package kgraph

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Extraction is the other direction from render, and the difference matters.
//
// `render` resolves an AUTHOR's prompt and adds nothing of its own —
// TestRenderAddsNoScaffolding enforces that, because scaffolding in a generation
// prompt is how a document ends up in a shape nobody asked for. Extraction has no
// author's prompt to resolve: the input is a document and the wanted output is
// `kfacts` obeying rules the model cannot infer from the document. So kgraph does
// supply the format rules here, and only here.
//
// The other inverted rule: a render prompt must never contain a document path,
// because a model that sees a path invents a citation format. In extraction the
// document IS the input, so the path and the body belong in the prompt. These are
// not in conflict — they are the two ends of the same pipe.
//
// kgraph still never calls a model. Same three steps as render/attach:
//
//	kg extract              what has not been read yet
//	kg extract <doc>        the extraction prompt, on stdout
//	  agent writes facts into a *.kfacts.md
//	kg scan                 validates them; duplicates are caught by cid
//	kg source lock          records the version that was read
type Extraction struct {
	Doc        string   `json:"doc"`
	Source     string   `json:"source,omitempty"` // existing source node, if any
	Class      string   `json:"class,omitempty"`
	Known      int      `json:"known"` // facts already read from this document
	Prompt     string   `json:"prompt"`
	Inlined    bool     `json:"inlined"`              // body included, vs path only
	Transcript string   `json:"transcript,omitempty"` // sibling text used in place of a scan
	Analysis   string   `json:"analysis,omitempty"`   // sibling reading, mentioned not inlined
	Questions  []string `json:"questions,omitempty"`
}

// DocStatus is one document's place in the extraction backlog.
type DocStatus struct {
	Doc    string `json:"doc"`
	Source string `json:"source,omitempty"`
	// Title labels a source that has no path to print. Empty for real documents,
	// where Doc is the label.
	Title string `json:"title,omitempty"`
	Facts int    `json:"facts"`
	State string `json:"state"` // unread | thin | undocumented | missing | read
	// Ready reports that machine-readable text exists for this document — a
	// transcription or text layer — so it can actually be extracted now.
	//
	// This is the difference between a backlog and a work queue. 500 unread
	// documents is a number nobody can act on; the subset that is unread AND
	// readable is the list to work today, and it grows on its own as raglit
	// transcribes. Derived from what is on disk rather than from watching for
	// events, so nothing is missed and nothing needs resyncing.
	Ready bool `json:"ready,omitempty"`
}

// ignoreName is a per-directory list of what is not evidence.
//
// A real corpus contains files that are inputs to an exhibit rather than
// exhibits: `_source-tiffs/harrow-declaration/Scan 1.tiff` through `Scan 33.tiff`
// are the page scans behind one PDF that is already in the corpus. Counted as
// documents they were 33 of the 58 unread court filings, and a backlog that is
// 57% noise is one nobody reads.
//
// Nothing in the filesystem distinguishes them, and kgraph must not guess — an
// underscore prefix means "staging" in one corpus and nothing in the next. So the
// corpus says. Not `.gitignore`, which answers a different question: an entire
// project here is gitignored for holding medical PII and is very much evidence.
const ignoreName = ".kgraphignore"

// ignoreRules is a set of globs, resolved relative to the file that declares them.
type ignoreRules struct {
	dir  string
	pats []string
}

func loadIgnores(root string) []ignoreRules {
	var out []ignoreRules
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != ignoreName {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		r := ignoreRules{dir: filepath.ToSlash(rel)}
		for _, ln := range strings.Split(string(b), "\n") {
			if ln = strings.TrimSpace(ln); ln != "" && !strings.HasPrefix(ln, "#") {
				r.pats = append(r.pats, ln)
			}
		}
		if len(r.pats) > 0 {
			out = append(out, r)
		}
		return nil
	})
	return out
}

// ignored reports whether a project-relative path is excluded. A pattern ending
// in `/` excludes a whole subtree; otherwise it is matched as a glob against both
// the path relative to the declaring file and the base name.
func ignored(rules []ignoreRules, rel string) bool {
	for _, r := range rules {
		sub := rel
		if r.dir != "." && r.dir != "" {
			var ok bool
			if sub, ok = strings.CutPrefix(rel, r.dir+"/"); !ok {
				continue
			}
		}
		for _, pat := range r.pats {
			if strings.HasSuffix(pat, "/") {
				if strings.HasPrefix(sub, pat) || strings.Contains(sub, "/"+pat) {
					return true
				}
				continue
			}
			if ok, _ := filepath.Match(pat, sub); ok {
				return true
			}
			if ok, _ := filepath.Match(pat, filepath.Base(sub)); ok {
				return true
			}
		}
	}
	return false
}

// thinFacts is the count below which a cited document looks under-read. A record
// that yielded one fact usually had more in it; this is a prompt to look again,
// not a rule about how many facts a document owes.
const thinFacts = 2

// Backlog lists documents in the project and what has been read from each.
//
// The valuable half is the documents NO source cites. Those are invisible to
// every other command — the graph cannot report a gap it has no node for — and in
// a corpus assembled by hand they are where the unread evidence actually is.
// The backlog is per-INDEX, not per-root. An index is a closed graph, so a
// document in another matter is not this matter's unread evidence — listing it
// invites reading one person's records into another's graph, which is the exact
// crossing the index scheme exists to prevent. On `~/life` this reported 805
// unread of which 253 were other matters and Syncthing's `.stversions` archive.
//
// Filtering per file rather than walking one directory is deliberate: markers
// nest and nearest wins, so an index is not always one contiguous subtree.
func Backlog(root, index string, g *Graph) ([]DocStatus, error) {
	bySource := map[string]string{} // doc path -> source id
	factCount := map[string]int{}   // doc path -> facts attested
	attested := map[string]int{}    // source id -> facts
	for _, e := range g.Edges {
		if e.Type == EAttests {
			attested[e.Src]++
		}
	}
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath == "" {
			continue
		}
		rel, err := DocPathOf(root, n)
		if err != nil || rel == "" {
			continue
		}
		bySource[rel] = id
		factCount[rel] += attested[id]
	}

	// Nothing is generated any more — render retired in 8127461 — so there are no
	// outputs of kgraph's own to exclude from the backlog. Kept as an empty set
	// rather than threaded away, because the walk below reads it in three places
	// and an empty map says "nothing to skip" more plainly than three deletions.
	generated := map[string]bool{}

	var out []DocStatus
	// A declared source is always listed, even where a rule would exclude it:
	// silently dropping something the graph cites would hide a real gap.
	cited := func(rel string) bool { return bySource[rel] != "" }
	files, err := walkCorpus(root, index, generated, cited)
	seen := map[string]bool{}
	// READY means raglit can read it, asked once for the whole corpus. It used to
	// mean "a sidecar exists", under which nine Word declarations were permanently
	// unready while their text sat in the index.
	readable := docsWithText(root)
	for _, rel := range files {
		seen[rel] = true
		ready := false
		if readable != nil {
			ready = readable[rel]
		} else {
			tr, _ := companions(root, rel)
			ready = tr != ""
		}
		out = append(out, DocStatus{Doc: rel, Source: bySource[rel], Ready: ready,
			Facts: factCount[rel], State: docState(bySource[rel], factCount[rel])})
	}
	if err != nil {
		return nil, err
	}
	// A cited document that is not on disk is still backlog: it is declared, and
	// whatever was read from it cannot be checked.
	for rel, id := range bySource {
		if !seen[rel] {
			out = append(out, DocStatus{Doc: rel, Source: id, Facts: factCount[rel],
				State: "missing"})
		}
	}

	// A source with NO `doc:` at all is the quieter version of the same problem,
	// and nothing reported it. Facts rest on it, `kg source status` cannot see it
	// because there is no path to hash, and the backlog never listed it because
	// the backlog walks the disk. So the graph asserts what a deed or a survey
	// says while holding no deed and no survey.
	//
	// `inference` is exempt and is the whole reason this is not simply an error:
	// an inference is our own conclusion, and `derived_from` names its premises.
	// It has no document because there is no document to have.
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath != "" {
			continue
		}
		if n.Source.Class == "inference" || n.Source.Form == "inference" {
			continue
		}
		out = append(out, DocStatus{Source: id, Title: n.Body, Facts: attested[id],
			State: "undocumented"})
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := stateRank(out[i].State), stateRank(out[j].State)
		if ri != rj {
			return ri < rj
		}
		return out[i].Doc < out[j].Doc
	})
	return out, nil
}

func docState(source string, facts int) string {
	switch {
	case source == "":
		return "unread"
	case facts < thinFacts:
		return "thin"
	}
	return "read"
}

// stateRank sorts the backlog by how much attention each state wants.
func stateRank(s string) int {
	switch s {
	case "unread":
		return 0
	case "thin":
		return 1
	case "missing":
		return 2
	case "undocumented":
		return 3
	}
	return 4
}

// transcriptSuffixes are sibling files holding the TEXT OF THE DOCUMENT: an OCR
// pass or an extracted text layer. Preferred over the binary, because that text
// is what a reader actually reads.
//
// `-analysis.md` is deliberately NOT here. An analysis is someone's READING of a
// document, and inlining it as the document would launder an interpretation into
// the fact record — the extracted facts would cite the exhibit while actually
// resting on a prior conclusion about it. It is mentioned so it can be consulted
// on purpose, never presented as the source text.
// `.raglit-transcription.md` is raglit's page-delineated transcription, written
// beside a document during ingest. It is listed here rather than ignored on
// purpose: it is not a second exhibit, it is THIS document in readable form —
// derived, with the real source right next to it — which is exactly what a
// transcript is. Treating it as a transcript means the backlog does not
// double-count it AND an extraction prompt can use the readable text while still
// citing the document it came from.
// ORDER IS PRECEDENCE, and the whole-file transcription comes FIRST.
//
// It was last, which meant `.ocr.md` beat it whenever both existed — and `companions`
// returns the first match, so that is the text `kg verify` and `kg quotes` actually
// read.
//
// `.ocr.md` is NOT a hand-typed file, which is what an earlier version of this comment
// claimed. Six of the nine in the live corpus carry the header "OCR text extracted by
// raglit (per-page)": it is raglit's PER-PAGE fallback, used when a whole-file request
// blows the vision model's context, roughly past six pages. So this is not
// human-versus-machine, it is two pathways through the same model — and the per-page
// one is the one that pads. Measured on the live corpus:
// `2021-rrepsa-purchase-sale-agreement.ocr.md` renders the operative record of survey
// as `AF#20140415006`, an eleven-digit number that denotes nothing, where raglit's
// transcription of the same PDF has `AF#201503110043` correct. So the one tool whose
// entire job is checking whether a source contains the words a fact quotes was
// checking them against invention, and `plan/format.md` cites that very file as
// `class: record`.
//
// The corpus states the same rule in `documents/TRANSCRIPTION-SIDECARS.md` —
// "raglit is canonical", meaning the whole-file transcription — and the code was
// contradicting its own project's written policy. `.ocr.md` remains a FALLBACK for a
// document with no whole-file pass; it simply stops overriding one.
var transcriptSuffixes = []string{".raglit-transcription.md", ".ocr.md", ".text.md", ".txt"}

// TranscriptSuffixes are the endings that mark a DERIVED reading of a document
// rather than a document.
//
// Exported because a consumer that has to know "is this file evidence, or is it
// the machine's reading of evidence" otherwise copies the list — and a copy that
// goes stale declares a transcription as a source, which puts the same
// photograph in the corpus twice and lets `Have` find both. caselit hit exactly
// that filing a directory of 27 photographs that came with 27 transcriptions.
//
// A FUNCTION, not the slice, for the reason ClassLadder is one: a caller cannot
// mutate the rule under the package that applies it, and the shape survives this
// becoming dialect-scoped.
func TranscriptSuffixes() []string {
	return append([]string(nil), transcriptSuffixes...)
}

const analysisSuffix = "-analysis.md"

// textDocExts are documents whose own bytes are already the readable form.
//
// A PDF or a scan needs a transcript because searching its bytes finds nothing.
// These need no transcript, and treating "has no companion" as "cannot be read"
// hid them: a hearing's `.verified.md`, an email thread saved as text, a
// witness's notes. The list is deliberately short. Anything whose readable form
// is DERIVED belongs in transcriptSuffixes instead, so that the hit is reported
// against the instrument rather than against the machine's reading of it.
var textDocExts = map[string]bool{".md": true, ".txt": true}

// isTextDoc reports whether a document can be read directly, with no companion.
//
// `-analysis.md` is excluded, and that exclusion is the point rather than an
// omission. An analysis is OUR reading of an instrument, and a search that
// returned it as evidence would let the fact tree grow from our own notes —
// the corpus keeps analyses beside their sources precisely so that a citation
// names the source and never the commentary.
func isTextDoc(rel string) bool {
	if strings.HasSuffix(rel, analysisSuffix) {
		return false
	}
	return textDocExts[strings.ToLower(filepath.Ext(rel))]
}

// companions finds the transcript and the analysis sitting beside a document.
func companions(root, rel string) (transcript, analysis string) {
	stem := strings.TrimSuffix(rel, filepath.Ext(rel))
	// Two naming conventions, both real. `X.ocr.md` replaces the extension;
	// `X.pdf.raglit-transcription.md` appends to the whole name, because raglit
	// derives it from the full path. Checking only the first missed every raglit
	// transcription.
	for _, suf := range transcriptSuffixes {
		for _, c := range transcriptCandidates(rel, stem, suf) {
			if c == rel {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, c)); err == nil {
				transcript = c
				break
			}
		}
		if transcript != "" {
			break
		}
	}
	if a := stem + analysisSuffix; a != rel {
		if _, err := os.Stat(filepath.Join(root, a)); err == nil {
			analysis = a
		}
	}
	return transcript, analysis
}

// siblingTextDirs are directory pairs where a corpus keeps a document and its
// readable form side by side rather than in one place — `correspondence/eml/x.eml`
// with `correspondence/text/x.txt`.
//
// Without this the same 49 emails counted twice: once as the .eml and once as its
// own text. Ignoring the text directory would have "fixed" the count by hiding
// the only readable form of every email in the corpus, which is the opposite of
// what a transcript is for.
var siblingTextDirs = [][2]string{{"eml", "text"}, {"raw", "text"}, {"scans", "text"}}

// transcriptCandidates is where a document's readable form might live: replacing
// its extension (`X.ocr.md`), appended to the whole name
// (`X.pdf.raglit-transcription.md`), or the same basename in a sibling directory.
func transcriptCandidates(rel, stem, suf string) []string {
	out := []string{stem + suf, rel + suf}
	dir, base := filepath.Split(stem)
	for _, pair := range siblingTextDirs {
		d := filepath.Clean(dir)
		if filepath.Base(d) != pair[0] {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(filepath.Dir(d), pair[1], base+suf)))
	}
	return out
}

// isTranscriptOf reports whether rel is the transcript of a document that is
// itself in the corpus. A stray `.ocr.md` with no original is real evidence and
// stays in the backlog.
func isTranscriptOf(root, rel string) bool {
	// The sibling-directory convention, in reverse: is THIS the text of a document
	// one directory over?
	for _, suf := range transcriptSuffixes {
		if stem, ok := strings.CutSuffix(rel, suf); ok {
			dir, base := filepath.Split(stem)
			for _, pair := range siblingTextDirs {
				d := filepath.Clean(dir)
				if filepath.Base(d) != pair[1] {
					continue
				}
				matches, _ := filepath.Glob(filepath.Join(root,
					filepath.Dir(d), pair[0], base+".*"))
				if len(matches) > 0 {
					return true
				}
			}
		}
	}
	for _, suf := range transcriptSuffixes {
		stem, ok := strings.CutSuffix(rel, suf)
		if !ok {
			continue
		}
		exts := []string{".pdf", ".tiff", ".tif", ".jpg", ".jpeg", ".png", ".docx"}
		for _, ext := range exts {
			if _, err := os.Stat(filepath.Join(root, stem+ext)); err == nil {
				return true
			}
			// The stem may already CARRY its extension: `X.docx.txt` extracts
			// `X.docx`, whose sibling is the stem itself, not stem+ext. Only
			// stem+ext was checked, so every `.docx.txt` in the corpus counted as a
			// second exhibit — 67 of them, against 10 originals.
			if strings.EqualFold(filepath.Ext(stem), ext) {
				if _, err := os.Stat(filepath.Join(root, stem)); err == nil {
					return true
				}
			}
		}
	}
	return false
}

// maxInline caps the document body put into a prompt. Past this the path is given
// instead, because a truncated exhibit is worse than a pointer to a whole one —
// facts read from the visible half look complete.
const maxInline = 200 << 10

// Extract builds the extraction prompt for one document.
func (g *Graph) Extract(root, rel string, env Env) (*Extraction, error) {
	rel = filepath.ToSlash(rel)
	x := &Extraction{Doc: rel}

	var factIDs []string
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil {
			continue
		}
		if r, err := DocPathOf(root, n); err != nil || r != rel {
			continue
		}
		x.Source = id
		x.Class = n.Source.Class
		for _, e := range g.incident[id] {
			if e.Type == EAttests && e.Src == id {
				factIDs = append(factIDs, e.Dst)
			}
		}
	}
	sort.Strings(factIDs)
	x.Known = len(factIDs)

	var b strings.Builder
	raw, readErr := os.ReadFile(filepath.Join(root, rel))
	transcript, analysis := companions(root, rel)

	// A transcript beside a scan is the same document in readable form. Using it
	// keeps the prompt self-contained instead of sending the reader off to open a
	// PDF page by page.
	if !utf8.Valid(raw) && transcript != "" {
		if tb, terr := os.ReadFile(filepath.Join(root, transcript)); terr == nil && utf8.Valid(tb) && len(tb) <= maxInline {
			x.Inlined = true
			x.Transcript = transcript
			fmt.Fprintf(&b, "# Document: %s\n\nRead here from its transcript, %s. Cite the document, not the transcript.\n\n%s\n",
				rel, transcript, strings.TrimRight(string(tb), "\n"))
			raw = nil
		}
	}
	switch {
	case x.Inlined:
		// already written from the transcript
	case readErr != nil:
		return nil, fmt.Errorf("cannot read %s: %w", rel, readErr)
	case !utf8.Valid(raw):
		fmt.Fprintf(&b, "# Document (not text — read it yourself)\n\n%s\n", rel)
	case len(raw) > maxInline:
		fmt.Fprintf(&b, "# Document (%d bytes, too large to inline — read it yourself)\n\n%s\n",
			len(raw), rel)
	default:
		x.Inlined = true
		fmt.Fprintf(&b, "# Document: %s\n\n%s\n", rel, strings.TrimRight(string(raw), "\n"))
	}

	if analysis != "" {
		fmt.Fprintf(&b, "\nA prior analysis of this document exists at %s. It is somebody's READING, "+
			"not the document — consult it if useful, but extract from the text above and attest "+
			"to the document.\n", analysis)
		x.Analysis = analysis
	}

	// What is already held, so a re-read adds rather than duplicates. Without this
	// the second pass over a document restates the first pass under new ids, and
	// `cid` then catches it as an error after the fact — better to not write it.
	b.WriteString("\n# Already read from this document\n\n")
	if len(factIDs) == 0 {
		b.WriteString("Nothing yet.\n")
	} else {
		for _, id := range factIDs {
			n := g.Nodes[id]
			fmt.Fprintf(&b, "- %s [%s · %s] id: %s\n", n.Body, n.Kind, n.Status, id)
		}
		b.WriteString("\nDo not restate these. Add what they miss, or contradict them with a\n")
		b.WriteString("`contradicts` edge if the document does not support them.\n")
	}

	// The highest-value framing available: not "extract facts" but "close these".
	likely, other := g.evidenceQuestions(factIDs)
	x.Questions = append(append([]string{}, likely...), other...)
	if len(x.Questions) > 0 {
		b.WriteString("\n# Open questions this document may answer\n\n")
		b.WriteString("Each is open and needs EVIDENCE. If the document answers one, say so with an\n")
		b.WriteString("`answers:` edge from the new fact to the question id.\n\n")
		g.writeQuestions(&b, likely, "Anchored to something this document already touches")
		g.writeQuestions(&b, other, "Other open evidence questions")
	}

	b.WriteString(extractRules)
	if x.Source != "" {
		fmt.Fprintf(&b, "\nThis document is already declared as source `%s` (class: %s).\n"+
			"Use `attested_by: %s`. Do not declare a second source node for it.\n",
			x.Source, orDeclared(x.Class), x.Source)
	} else {
		fmt.Fprintf(&b, "\nNo source node declares this document yet. Declare one, with `doc: %s`\n"+
			"and the evidentiary class the document actually warrants.\n", rel)
	}
	x.Prompt = b.String()
	return x, nil
}

func orDeclared(s string) string {
	if s == "" {
		return "not declared"
	}
	return s
}

// evidenceQuestions splits open evidence questions into those anchored to
// something this document already touches, and the rest.
func (g *Graph) evidenceQuestions(factIDs []string) (likely, other []string) {
	touches := map[string]bool{}
	for _, id := range factIDs {
		for _, e := range g.incident[id] {
			if e.Type == EAbout && e.Src == id {
				touches[e.Dst] = true
			}
		}
	}
	var ids []string
	for id, n := range g.Nodes {
		if n.Kind == KQuestion && n.Status == SOpen && n.Needs == NeedsEvidence {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		var hit bool
		for _, e := range g.incident[id] {
			if e.Type == EAbout && e.Src == id && touches[e.Dst] {
				hit = true
				break
			}
		}
		if hit {
			likely = append(likely, id)
		} else {
			other = append(other, id)
		}
	}
	// Cap the unranked tail. A wall of every open question in the corpus reads as
	// noise and the ranked ones stop being noticed.
	if len(other) > 12 {
		other = other[:12]
	}
	return likely, other
}

func (g *Graph) writeQuestions(b *strings.Builder, ids []string, heading string) {
	if len(ids) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", heading)
	for _, id := range ids {
		n := g.Nodes[id]
		fmt.Fprintf(b, "- %s\n  id: %s\n", n.Body, id)
		if n.Reason != "" {
			fmt.Fprintf(b, "  why it matters: %s\n", strings.TrimSpace(n.Reason))
		}
	}
	b.WriteString("\n")
}

// extractRules is the format contract. Every line is here because getting it
// wrong produces something that parses and is wrong — a silent defect, not an
// error the scan can catch.
const extractRules = `
# Write kfacts

Emit one ` + "```kfacts" + ` block. Each entry is a node; one of these keys names
its kind and carries the statement:

    claim  question  event  action  entity  group
    document  utterance  record  observation  inference   (these are sources)

Rules that a document cannot tell you, and that produce a parseable but wrong
graph when broken:

- One idiom per relation. Write a relation once, from the canonical end. Do not
  also declare its inverse from the other end.
- No ` + "`confidence`" + `. A hand-typed number is an invented statistic. Weight
  comes from the source's ` + "`class`" + `, which is ordinal:
  record > admission > document > observation > statement > interested > inference.
- No ` + "`disputed`" + `. It is computed from conflicting evidence, never written.
- No negation. There is no ` + "`not`" + `: use ` + "`status: false`" + ` plus the
  negative edges (` + "`prohibits`, `contradicts`" + `).
- ` + "`status: withdrawn`" + ` means we were wrong. ` + "`status: false`" + ` means
  tested and rejected on purpose. They are not interchangeable.
- Every claim and event needs an ` + "`about:`" + ` anchor — the thing it is about.
  A fact anchored to nothing cannot be found by anything.
- Every open question needs ` + "`needs:`" + ` (evidence, decision, reply, analysis)
  and either an ` + "`owner:`" + ` or an ` + "`about:`" + `, or it sits open forever.
- Dates: ` + "`at:`" + ` is when something OCCURRED. ` + "`valid_from`/`valid_until`" + `
  is when a claim is TRUE. Use ` + "`precision: month|year|approx`" + ` rather than
  inventing a day.
- Ids are semantic slugs, never numbers: ` + "`strip-held-by-halloway`" + `, not
  ` + "`f-0142`" + `. They appear in diffs and have to stay readable.
- Quote any value containing ` + "`#`" + `, or YAML silently truncates it at that
  character.

Write only what the document supports. A fact the document does not carry is
worse than a missing one, because the citation makes it look established.
`

// walkCorpus lists the candidate documents of one index, applying the exclusions
// that have nothing to do with the graph: tooling directories, fact and spec
// files, documents belonging to another matter, ignore rules, and transcripts of
// documents already listed.
//
// Extracted from Backlog so `kg have` searches exactly the population the backlog
// counts. They had drifted apart in the obvious way once already — a lookup that
// silently skips a directory the backlog includes will answer "not held" about a
// document sitting right there, which is the failure `have` exists to prevent.
//
// `keep` forces inclusion of a path a rule would drop. Backlog passes "is it
// cited", because dropping something a fact rests on would report it `missing`
// and hide a real gap. A caller with no such need passes nil.
func walkCorpus(root, index string, generated map[string]bool, keep func(rel string) bool) ([]string, error) {
	if keep == nil {
		keep = func(string) bool { return false }
	}
	rules := loadIgnores(root)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".kgraph", "node_modules", "vendor":
				return fs.SkipDir
			}
			// Any dot-directory. Dot-FILES were already skipped below, but the
			// directories were not, which is how Syncthing's `.stversions` — an
			// archive of every superseded version of every file — became 9 backlog
			// entries, and `.claude/` and `.raglit/` alongside it. A dot-directory
			// is tooling state, never evidence.
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return fs.SkipDir
			}
			return nil
		}
		n := d.Name()
		if strings.HasSuffix(n, FactsSuffix) || strings.HasSuffix(n, SpecSuffix) || strings.HasPrefix(n, ".") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if generated[rel] || n == ignoreName {
			return nil
		}
		// Whose evidence is this? A document belongs to the index of its nearest
		// marker, and only this index's documents are this index's population.
		if own, ierr := indexOfFile(root, rel); ierr != nil || own != index {
			return nil
		}
		// A transcript is the same document in readable form, not another exhibit.
		if isTranscriptOf(root, rel) && !keep(rel) {
			return nil
		}
		if ignored(rules, rel) && !keep(rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	return out, err
}

// corpusListing is walkCorpus's unfiltered result, computed once per graph.
//
// Only the UNFILTERED walk is memoized. extract.go's own caller passes a
// `generated` set and a `cited` predicate that differ per call, and caching a
// filtered answer under a key that does not mention the filter is how a cache
// starts returning somebody else's question. The expensive part is the traversal
// and it is the same traversal either way.
//
// A nil receiver walks without caching rather than panicking: `Have` accepts a
// graph and callers have passed one built without a filesystem.
func (g *Graph) corpusListing(root, index string) ([]string, error) {
	if g == nil {
		return walkCorpus(root, index, nil, nil)
	}
	if g.corpus == nil && g.corpusErr == nil {
		g.corpus, g.corpusErr = walkCorpus(root, index, nil, nil)
		// A walk that found nothing must not re-walk on every call. Nil is the
		// "not walked" signal, so an empty result is stored as an empty non-nil
		// slice — otherwise a corpus with no documents pays the full traversal
		// every time, which is the exact case a matter starts in.
		if g.corpus == nil && g.corpusErr == nil {
			g.corpus = []string{}
		}
	}
	return g.corpus, g.corpusErr
}
