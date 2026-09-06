package kgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Human attestation: did the document actually say this?
//
// Extraction reads a document and writes facts. Nothing then checks that the
// document says what the fact claims — and for a scanned exhibit with no text
// layer, nothing CAN check it automatically. The corpus is mostly that: 89 PDFs
// and 117 images, read by eye or by OCR, with facts resting on them.
//
// So a person verifies, once, and the verdict is durable. This is oidio's
// contract in the other medium: the machine's reading is the raw observation and
// must survive being wrong, so corrections live in a SIDECAR rather than being
// written back over the facts. Re-extracting a document must not destroy what a
// person confirmed, and confirming must not freeze the facts against re-reading.
//
// The unit is the ATTESTS EDGE, not the fact and not the source. One source
// supports many facts and each needs its own proof, so (fact, source) is the key
// and the highlighted region is the proof.
//
// What this deliberately is NOT: a confidence score. The verdicts are
// categorical because `~/life`'s own rules forbid invented statistics, and
// because "the document does not say this" is not 0.3 of anything.
type Verdict string

// THE VOCABULARY IS raglit/attest's, AND THIS FILE IS THE ONE THAT MOVED.
//
// Four repos built human attestation separately — oidio over audio turns, raglit
// over page regions, this over (fact, source) edges — and only the LOCATOR ever
// differed. raglit/attest is the extraction and is canonical (see
// `raglit/plan/attest.md`); it had `confirmed` where this had `attested` and
// `unclear` where this had `illegible`. Same concepts, two spellings, which is
// drift arriving one word at a time.
//
// The old words are still READ. The logs are append-only by design — that is how
// two syncing machines merge without losing a verdict — so rewriting them to
// change a spelling would be the one edit the format exists to forbid. A reader
// normalises; a writer emits the converged word.
const (
	// VConfirmed — found it, and checked deliberately because something depends on
	// it. Region names where.
	//
	// THE ESCALATION, not the ordinary pass: see VAffirmed, which is what covers a
	// reviewer's ordinary progress through an asset. Getting these two the wrong
	// way round reports a careful review as a thin one.
	VConfirmed Verdict = "confirmed"
	// VAttested is the old spelling of VConfirmed. Accepted on read, never written.
	VAttested Verdict = "attested"
	// VAffirmed — the reviewer went through the asset and accepted this under the
	// standard they stated. THE ORDINARY PASS.
	//
	// New here, and it is the one this vocabulary was missing rather than
	// misspelling: without it there is no way to say "I read all of this and these
	// are the ones I changed", so every unreviewed unit and every unremarkable one
	// look identical.
	VAffirmed Verdict = "affirmed"
	// VCorrected — the document says something adjacent but not this. Note holds
	// what it actually says.
	VCorrected Verdict = "corrected"
	// VUnsupported — the document is SILENT on this. Not refuted: absent.
	//
	// This is the verdict with no prior home in the vocabulary. `contradicts` and
	// `undercut_by` already cover a document that says the opposite, and they feed
	// computed `disputed`. A miscitation is a different failure: the fact may be
	// perfectly true and simply hung on the wrong exhibit. So the FACT is left
	// alone and the CITATION dies — the edge stops counting as attestation and the
	// fact becomes unattested, needing a new source home.
	VUnsupported Verdict = "unsupported"
	// VUnclear — looked, cannot read it. A verdict on the SCAN, not the fact, and
	// it keeps a bad scan from being silently retried forever.
	VUnclear Verdict = "unclear"
	// VIllegible is the old spelling of VUnclear. Accepted on read, never written.
	VIllegible Verdict = "illegible"
)

// canonical folds a retired spelling onto the word this vocabulary now uses.
// Applied on READ so an existing log keeps resolving, and applied on WRITE so a
// caller passing the old constant still lands the new word in the file.
func (v Verdict) canonical() Verdict {
	switch v {
	case VAttested:
		return VConfirmed
	case VIllegible:
		return VUnclear
	}
	return v
}

func (v Verdict) valid() bool {
	switch v.canonical() {
	case VConfirmed, VAffirmed, VCorrected, VUnsupported, VUnclear:
		return true
	}
	return false
}

// Region is where on the page the proof is: a page number and a box in
// FRACTIONS of page width and height, so a re-render at a different DPI does not
// move it. Zero Page means the whole document.
type Region struct {
	Page int     `json:"page,omitempty"`
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	W    float64 `json:"w,omitempty"`
	H    float64 `json:"h,omitempty"`
}

// Attestation is one person's verdict on one (fact, source) pair.
type Attestation struct {
	Fact    string  `json:"fact"`
	Source  string  `json:"source"`
	Verdict Verdict `json:"verdict"`
	Region  Region  `json:"region,omitempty"`
	// Note carries the corrected reading for VCorrected, and the reason for
	// VUnsupported — "this paragraph is about the septic, not the pole".
	Note string `json:"note,omitempty"`
	// By is who or what verified: a person's name, or a machine identity like
	// `ocr-transcript` or `agent-vision`. Optional — an unsigned verdict is
	// treated exactly like a machine one, which is to say it still needs a human
	// to sign it off. See PendingHuman.
	//
	// The distinction that matters is not who signed but whether a PERSON did. A
	// machine can honestly report that a string is or is not in a text layer; it
	// cannot report that a document does not SUPPORT a proposition.
	By string `json:"by,omitempty"`
	At string `json:"at,omitempty"`
	// Excerpt is the passage the verdict was made OVER — the words as they read
	// at the moment somebody stood behind them — and ExcerptHash pins that text
	// against later edits to this record itself.
	//
	// WITHOUT IT A VERDICT OUTLIVES THE WORDS IT WAS ABOUT, and nothing could
	// tell. Two ways, both live:
	//
	//   - The TRANSCRIPTION changes under a standing verdict. A re-OCR, a better
	//     model, a re-scan at another DPI — and `HashDoc` cannot see any of it,
	//     because it digests the ORIGINAL FILE's bytes and the PDF did not
	//     change. Drift does not fire. This is the likeliest way words move and
	//     it was completely invisible.
	//   - The FACT's quotation is edited after attestation. An attestation is
	//     keyed on (fact, source) and survives any edit to either, so the verdict
	//     stands over words nobody attested.
	//
	// `CheckAttestedExcerpts` re-derives and compares in both directions. That is
	// what makes a citation checkable rather than merely formatted: a reference
	// that cannot be verified is authored confidence wearing a citation's
	// clothes, which is the thing backing, filing and grounding already refuse.
	//
	// Empty when the fact quotes nothing. A verdict about a document that is not
	// being quoted is still a verdict; there is simply no passage to pin.
	Excerpt     string `json:"excerpt,omitempty"`
	ExcerptHash string `json:"excerpt_hash,omitempty"`
}

// ExcerptOf is the passage an attestation of this fact would be made over: what
// the fact puts in quotation marks, joined.
//
// The same extraction `CheckQuotes` uses, so the two cannot disagree about what
// counts as a quotation — the editorial marks, the ellipsis splitting and the
// minimum length are one rule in one place.
func ExcerptOf(body string) string {
	qs := checkableQuotes(body)
	if len(qs) == 0 {
		return ""
	}
	return strings.Join(qs, " … ")
}

// HashExcerpt pins an excerpt's text. FOLDED, not raw: the comparison that
// matters is whether the same WORDS are there, and a transcription that changes
// its whitespace or its curly quotes has not moved the passage.
func HashExcerpt(excerpt string) string {
	f := foldText(excerpt)
	if f == "" {
		return ""
	}
	return hashString(f)
}

// machinePrefixes name verifiers that are processes, not people. A machine
// identity is not a lesser person — it answers a narrower question.
// `agent-` covers a model reading the document itself — vision over a scan is a
// machine reading, however good it is, and signing it with a person's name would
// put a verdict in their mouth.
var machinePrefixes = []string{"ocr-", "text-layer", "transcript", "raglit", "agent-"}

// MachineAttested reports whether this verdict came from a process rather than a
// person. Kept as a predicate rather than a rank: it is a fact about who looked,
// and what that is worth is a reader's call, not an invented statistic.
func (a Attestation) MachineAttested() bool { return IsMachineIdentity(a.By) }

// IsMachineIdentity reports whether a signature names a process rather than a
// person. Exported because the question is asked BEFORE there is an attestation
// to ask it of: a workbench has to know at startup whether the identity it is
// about to stamp on every verdict is one whose verdicts count.
func IsMachineIdentity(by string) bool {
	for _, p := range machinePrefixes {
		if strings.HasPrefix(by, p) {
			return true
		}
	}
	return false
}

func (a Attestation) key() string { return a.Fact + "\x00" + a.Source }

// AttestSet is the resolved verdicts, latest per (fact, source).
type AttestSet map[string]Attestation

// Of returns the verdict on one edge, if any.
func (s AttestSet) Of(fact, source string) (Attestation, bool) {
	a, ok := s[fact+"\x00"+source]
	return a, ok
}

// sorted returns every verdict in a stable order — fact, then source. A report
// built off a Go map reorders itself between two runs over an unchanged corpus,
// which is the one thing every report here is built not to do.
func (s AttestSet) sorted() []Attestation {
	out := make([]Attestation, 0, len(s))
	for _, a := range s {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Fact != out[j].Fact {
			return out[i].Fact < out[j].Fact
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// Subtracts reports whether this verdict removes the citation from the fact's
// evidence. It is the whole of what a verdict can DO to the graph, and exactly
// one verdict can do it.
//
// `unsupported` is the only verdict that subtracts. `unclear` deliberately does
// NOT: failing to read a scan is a fact about the scan, and treating it as
// disproof would let a bad photocopy silently strip a document of its evidence.
//
// AND ONLY A SIGNED PERSON MAY SUBTRACT. A machine can honestly report that a
// string is or is not in a text layer; it cannot report that a document does not
// SUPPORT a proposition. Absence from a text layer is not absence from the
// document, so a scan with no text layer would "prove" every fact it attests
// unsupported — and because `Miscited` is an ERROR at render and attach, one bad
// OCR pass could strip a corpus of its evidence and refuse to file. An UNSIGNED
// verdict is treated the same for the reason PendingHuman already gives: if
// nobody said who checked it, nobody checked it. That also closes the obvious
// bypass — a machine that simply omits its name is not thereby a person.
//
// Such a verdict is not an error to hold. It is kept, listed, and IGNORED, and
// `checkAttestVerdicts` says so, because the machine reading is still the
// candidate a person works from.
func (a Attestation) Subtracts() bool {
	return a.Verdict.canonical() == VUnsupported && a.By != "" && !a.MachineAttested()
}

// Supports reports whether this edge may still count as attestation. One place
// decides, so every downstream rule — conflict, disputed, the unattested warning,
// Miscited — follows from this predicate alone.
func (s AttestSet) Supports(fact, source string) bool {
	a, ok := s.Of(fact, source)
	return !ok || !a.Subtracts()
}

// attestName is the sidecar. JSONL, appended, last record per key wins.
//
// Append-only because `~/life` syncs over Syncthing, where two machines
// rewriting one JSON object is a merge conflict and a lost verdict; two machines
// appending lines is a merge a human can read. Same reason the facts are text.
const attestName = "attestations.jsonl"

// ReadAttestations loads the sidecar for an index. Missing file is not an error:
// a corpus nobody has verified yet is the normal starting state.
func ReadAttestations(root, dir string) (AttestSet, error) {
	b, err := os.ReadFile(filepath.Join(root, dir, attestName))
	if err != nil {
		if os.IsNotExist(err) {
			return AttestSet{}, nil
		}
		return nil, err
	}
	out := AttestSet{}
	for i, ln := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "//") {
			continue
		}
		var a Attestation
		if err := json.Unmarshal([]byte(ln), &a); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", attestName, i+1, err)
		}
		if !a.Verdict.valid() {
			return nil, fmt.Errorf("%s:%d: unknown verdict %q", attestName, i+1, a.Verdict)
		}
		// Normalised HERE and nowhere downstream, so nothing else in the codebase
		// has to know that two spellings ever existed.
		a.Verdict = a.Verdict.canonical()
		out[a.key()] = a // later line wins
	}
	return out, nil
}

// AppendAttestation records one verdict. The only writer.
func AppendAttestation(root, dir string, a Attestation) error {
	if !a.Verdict.valid() {
		return fmt.Errorf("unknown verdict %q", a.Verdict)
	}
	// The file only ever gains the converged word, whatever the caller passed.
	a.Verdict = a.Verdict.canonical()
	if a.Fact == "" || a.Source == "" {
		return fmt.Errorf("an attestation needs both a fact and a source")
	}
	// Refused at the door as well as ignored at the gate. Subtracts() already
	// declines to honour this, but writing it would leave a line in an
	// APPEND-ONLY log that reads like a ruling and does nothing — and the log is
	// the record of who checked what, so a verdict nobody may act on does not
	// belong in it.
	if a.Verdict.canonical() == VUnsupported && !(a.By != "" && !a.MachineAttested()) {
		who := "an unsigned verdict"
		if a.By != "" {
			who = fmt.Sprintf("%q is a machine identity and", a.By)
		}
		return fmt.Errorf("%s may not rule `unsupported`: absence from a text layer is not "+
			"absence from the document, and this is the one verdict that removes a fact's "+
			"evidence. Record what was actually observed — `unclear` if it could not be read, "+
			"`confirmed` if it could — or sign it with `--by <person>`", who)
	}
	line, err := json.Marshal(a)
	if err != nil {
		return err
	}
	p := filepath.Join(root, dir, attestName)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// LastSigner returns the `by` of the most recently appended verdict.
//
// This is oidio's name prompt without the prompt: it asks once and then keeps
// using the answer. Deriving it from the sidecar rather than a config file means
// there is no second place for identity to live and go stale, and the file the
// verdicts are in is the file that knows who has been making them.
func LastSigner(root, dir string) string {
	b, err := os.ReadFile(filepath.Join(root, dir, attestName))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var a Attestation
		if json.Unmarshal([]byte(lines[i]), &a) == nil && a.By != "" {
			return a.By
		}
	}
	return ""
}

// LastHumanSigner is the last PERSON to sign a verdict here, ignoring machines.
//
// `LastSigner` answers "who wrote the last line", and every caller that reaches
// for a default signature actually wants "who is the person working this
// corpus". Those differ the moment an agent runs, and the failure is silent and
// total: a workbench that defaults to `agent-vision` stamps it on every verdict,
// `PendingHuman` re-queues each one because `MachineAttested` is true, and a
// person's whole session of rulings evaporates back into the queue they were
// working. `unsupported` is refused outright, which at least says so.
func LastHumanSigner(root, dir string) string {
	b, err := os.ReadFile(filepath.Join(root, dir, attestName))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		var a Attestation
		if json.Unmarshal([]byte(lines[i]), &a) == nil && a.By != "" && !a.MachineAttested() {
			return a.By
		}
	}
	return ""
}

// Unsupported lists the edges a person has ruled miscitations, sorted, so
// `kg status` and the scan can report them in a stable order.
func (s AttestSet) Unsupported() []Attestation {
	var out []Attestation
	for _, a := range s {
		if a.Verdict == VUnsupported {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Fact != out[j].Fact {
			return out[i].Fact < out[j].Fact
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// checkAttestVerdicts reports a recorded verdict the graph declines to honour.
//
// The log is append-only, so a machine or unsigned `unsupported` written before
// the guard existed — or synced from a machine that still has an old binary —
// cannot be edited out. It is therefore IGNORED rather than obeyed, and ignoring
// it silently is the failure this warning exists to prevent: the sidecar would
// say a citation was ruled dead while the graph went on counting it, and nothing
// would reconcile the two.
//
// A warning, not an error. The reading is a real observation and usually a
// correct one; what it is not is a person's ruling, and the fix is for a person
// to look, not for the corpus to stop building.
func (g *Graph) checkAttestVerdicts() []Diag {
	var out []Diag
	for _, a := range g.Attest {
		if a.Verdict.canonical() != VUnsupported || a.Subtracts() {
			continue
		}
		who := "it is unsigned"
		if a.By != "" {
			who = fmt.Sprintf("%q is a machine identity", a.By)
		}
		file, line := attestName, 0
		if n, ok := g.Nodes[a.Fact]; ok {
			file, line = n.File, n.Line
		}
		out = append(out, Diag{File: file, Line: line, Severity: SevWarn, Msg: fmt.Sprintf(
			"%s is recorded `unsupported` against %s, but %s — the ruling is IGNORED and the "+
				"citation still counts. Only a person can say a document does not support a "+
				"proposition; re-record it signed if that is the finding", a.Fact, a.Source, who)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Msg < out[j].Msg
	})
	return out
}

// PendingHuman is the checklist: every citation a person still has to sign off.
//
// A machine reading — OCR, or a model over a scan — is a candidate, not a
// verification. It is enough to work from and not enough to file on, so those
// citations stay on the list until a person confirms them. An UNSIGNED verdict
// counts as pending too: if nobody said who checked it, nobody checked it.
//
// This is the artefact the work is actually for. Everything read by machine
// accumulates here, and the list only shrinks when a human works it.
func (g *Graph) PendingHuman() []Attestation {
	var out []Attestation
	for _, e := range g.Edges {
		if e.Type != EAttests || !g.isSource(e.Src) {
			continue
		}
		n := g.Nodes[e.Dst]
		if n == nil || n.Status == SWithdrawn {
			continue
		}
		a, ok := g.Attest.Of(e.Dst, e.Src)
		switch {
		case !ok:
			out = append(out, Attestation{Fact: e.Dst, Source: e.Src})
		case a.By == "" || a.MachineAttested():
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Fact != out[j].Fact {
			return out[i].Fact < out[j].Fact
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// Miscited reports pinned facts whose every citation a person has ruled
// `unsupported`. This is the "error if used materially" line.
//
// Unattested is a WARNING while a fact sits in the graph — most corpora have a
// backlog of facts not yet sourced, and blocking on those would block every
// render. This is narrower and stronger: the fact HAD citations, a person opened
// the documents, and none of them say it. Publishing that into a filing is a
// different act from leaving it in a working graph, so it is an error at render
// and attach and a warning everywhere else.
func (g *Graph) Miscited(pins map[string]*Pin) []string {
	if len(g.Attest) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range pins {
		for _, entry := range p.Nodes {
			id := entry
			if i := strings.LastIndex(entry, "@"); i > 0 {
				id = entry[:i]
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			// A withdrawn claim is exempt. This guard exists to stop a fact
			// resting on nothing from being ASSERTED into a filing; a withdrawn
			// claim is rendered as retracted, which is the opposite act. And the
			// commonest reason to withdraw one is precisely that a person opened
			// its documents and they did not say it — so without this exemption a
			// failed attestation permanently blocks the `revised:` sections that
			// exist to show the correction, and the graph cannot report its own
			// corrections.
			if n, ok := g.Nodes[id]; ok && n.Status == SWithdrawn {
				continue
			}
			var had, live bool
			for _, e := range g.incident[id] {
				if e.Type != EAttests || e.Dst != id || !g.isSource(e.Src) {
					continue
				}
				had = true
				if g.Attest.Supports(id, e.Src) {
					live = true
					break
				}
			}
			if had && !live {
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// FactsUsedByDeclarations maps each fact a DECLARED QUERY resolves to the
// declarations that resolve it.
//
// Renamed from `FactsUsedBySpecs` and repointed at `DeclaredSets` (2026-09-01),
// so it answers from standing queries once an index has them and from specs
// until it does. The question it answers has not changed.
//
// This is the difference between "verify the corpus" and "verify what we are
// about to file". A fact no document pins can be checked later; a fact three
// documents rest on is load-bearing now. On the fence-dispute corpus 404 citations
// await a human and only 217 are relied on by any spec — so the queue nearly
// halves, and what is left is what actually reaches paper.
func (p *Project) FactsUsedByDeclarations(env Env) map[string][]string {
	used := map[string][]string{}
	// The DECLARATION is what gets reported. For a standing query that is its own
	// name; for a spec it is the filename, which is what `kg attest --used` has
	// always printed so a person can see what a citation is holding up.
	byFile := len(p.Standing) == 0
	for _, s := range p.DeclaredSets() {
		for _, dq := range s.Queries {
			q, err := ParseQuery(dq.Text)
			if err != nil {
				continue // a query that will not parse cannot be relying on anything
			}
			ids, err := p.Graph.Eval(q, env)
			if err != nil {
				continue
			}
			name := dq.Name
			if byFile && s.Path != "" {
				name = filepath.Base(s.Path)
			}
			pin := p.Graph.pinOf(q, ids, env)
			// Rows, and then the sources those rows cite — a source is relied on
			// too, since its class and path are printed even though it is never a
			// row itself.
			for _, entry := range append(append([]string{}, pin.Nodes...), pin.Cites...) {
				id := entry
				if i := strings.LastIndex(entry, "@"); i > 0 {
					id = entry[:i]
				}
				if !containsString(used[id], name) {
					used[id] = append(used[id], name)
				}
			}
		}
	}
	for id := range used {
		sort.Strings(used[id])
	}
	return used
}

func containsString(hay []string, s string) bool {
	for _, h := range hay {
		if h == s {
			return true
		}
	}
	return false
}

// ── ranking the queue ──────────────────────────────────────────────────

// PendingRank says why a citation is worth a person's time NOW.
//
// The queue reached 1,197 on the live corpus and `--used`, the axis it was
// designed to narrow on, was keeping 1,106 of them — 92%, because 289 standing
// queries between them cover nearly every fact. An axis that keeps almost
// everything is not a filter, and the 14 citations where a machine read
// something the document does not say — the exact case the whole human-in-the-
// loop design exists for — were sitting at 1.2% density inside it.
type PendingRank struct {
	Attestation
	// Band is the reason, and lower sorts first.
	Band   int
	Reason string
}

// Priority bands, most consequential first.
const (
	// BandAbsentQuote: the source's transcription does not contain the fact's own
	// quoted words. Either the passage was dropped in transcription or the
	// citation is on the wrong document, and both are invisible from the fact
	// text alone. Nothing else in the queue is worth looking at first.
	BandAbsentQuote = iota
	// BandContested: the fact is disputed or underdetermined, so the conflict
	// machinery already reads this citation to decide what the corpus concludes.
	// A wrong verdict here changes an answer, not just a footnote.
	BandContested
	// BandSoleAndUsed: a declared query resolves the fact AND this is its only
	// citation — load-bearing and fragile at once. Rule it `unsupported` and
	// something the corpus answers with rests on nothing.
	BandSoleAndUsed
	// BandUsed: some declared query resolves the fact.
	BandUsed
	// BandSole: the fact's only citation, but nothing asks about the fact yet.
	//
	// RANKED BELOW `used` ON EVIDENCE, not on taste. It was placed second at
	// first and swallowed the queue: 780 of 1,197 on the live corpus, because
	// most facts there have exactly one citation. A band that holds two thirds of
	// the queue discriminates no better than the `--used` axis it replaced.
	BandSole
	// BandRoutine: everything else. Real, and not urgent.
	BandRoutine
)

var bandNames = map[int]string{
	BandAbsentQuote: "quoted words absent from the document",
	BandContested:   "the fact is contested, so conflict reads this",
	BandSoleAndUsed: "a query resolves the fact and this is its only citation",
	BandUsed:        "a declared query resolves the fact",
	BandSole:        "the fact's only citation",
	BandRoutine:     "routine",
}

// Urgent is the cut the queue shows by default.
//
// SET FROM THE MEASUREMENT, not from the shape of the list. Cutting at
// `BandSoleAndUsed` looked principled and left 769 of 1,197 showing, because 626
// facts in the live corpus are both resolved by a query and rest on a single
// citation. That is a fact about the CORPUS — thin sourcing — and no ranking can
// shrink it; only citing more documents can. Ranking's job is to put the 143
// where a wrong verdict changes what the corpus concludes in front of the 1,054
// where it does not.
const Urgent = BandContested

// BandName is the human label for a band.
func BandName(b int) string { return bandNames[b] }

// RankPending orders a queue by consequence.
//
// IN THE LIBRARY, not the CLI, so the shell, the daemon and any consumer agree
// about what "worth doing first" means — the same reason `Ask` is the one
// standard op for drift. A second ranker with its own idea of urgent is how two
// surfaces come to disagree about the same corpus.
func (g *Graph) RankPending(pending []Attestation, absent map[string]bool, used map[string][]string) []PendingRank {
	// How many live citations each fact has, so a sole one can be recognised.
	cites := map[string]int{}
	for _, e := range g.Edges {
		if e.Type == EAttests && g.isSource(e.Src) {
			cites[e.Dst]++
		}
	}
	out := make([]PendingRank, 0, len(pending))
	for _, a := range pending {
		r := PendingRank{Attestation: a, Band: BandRoutine}
		sole, resolved := cites[a.Fact] == 1, len(used[a.Fact]) > 0
		switch {
		case absent[a.Fact+"\x00"+a.Source]:
			r.Band = BandAbsentQuote
		case g.tension(a.Fact, "").Any():
			r.Band = BandContested
		case sole && resolved:
			r.Band = BandSoleAndUsed
		case resolved:
			r.Band = BandUsed
		case sole:
			r.Band = BandSole
		}
		r.Reason = bandNames[r.Band]
		out = append(out, r)
	}
	// Stable and total: band, then fact, then source. A queue that reorders
	// between runs cannot be worked down a screen at a time.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Band != out[j].Band {
			return out[i].Band < out[j].Band
		}
		if out[i].Fact != out[j].Fact {
			return out[i].Fact < out[j].Fact
		}
		return out[i].Source < out[j].Source
	})
	return out
}
