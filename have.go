package kgraph

import (
	"fmt"
	"github.com/iodesystems/raglit/client"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Do we already have this document?
//
// Three times in one corpus an open question asked someone to go obtain an
// instrument that was sitting in `documents/` the whole time. Once the file was
// misfiled under another document's name (a Form 35R filed as "Form 34 General
// Addendum"), once it had simply never been connected to the question that
// wanted it, and once a search by filename was reported as proof of absence.
//
// Nothing in kgraph could answer the question. `source status` tracks documents
// already CITED, so it is blind to anything not yet a source — which is exactly
// the population a "go obtain it" question is drawn from. `extract` lists what is
// unread, which is a different question again.
//
// So this looks for a document by any name it might be known by, in the four
// places a corpus actually records that knowledge, and reports HOW it matched
// because that changes what you do next:
//
//   - alias — a source declares this name. Authoritative; someone said so.
//   - source — the source's own title contains it.
//   - path — the filename contains it. Weakest: filenames in this corpus have
//     lied three times, documented.
//   - text — a transcript beside the document contains it. This is the tier that
//     finds a misfiled document, because the sidecar is derived from the CONTENT
//     and the content cannot be misfiled.
//
// The text tier is why aliases are not sufficient on their own: aliases only
// cover what someone already thought to write down, and the documents that go
// missing are precisely the ones nobody catalogued.
type Held struct {
	Doc    string `json:"doc,omitempty"`
	Source string `json:"source,omitempty"`
	// Anchor locates a sub-document. One PDF released by a county records office
	// held a 1999 certification AND the 1972 contract that was the root of title;
	// they are different evidence from different decades and only `anchor:`
	// separates them.
	Anchor  string `json:"anchor,omitempty"`
	Via     string `json:"via"`
	Excerpt string `json:"excerpt,omitempty"`
	// Declared reports that a source node cites this document. An undeclared
	// document is held but invisible to the graph — findable here, citable by
	// nothing, which is its own thing to fix.
	Declared bool `json:"declared"`
	// Missing reports a `doc:` that names a file which is not there. Declared and
	// held are DIFFERENT facts and conflating them is the one error this must
	// never make: a citation to a document nobody has is precisely what an
	// "obtain it" question is for, and reporting it as held tells someone to
	// close that question and stop looking.
	Missing bool `json:"missing,omitempty"`
	// Terms is how many distinct query terms the excerpt carries, and is zero
	// outside the `terms` tier. The identifier tiers need no score — a document
	// either names the instrument or it does not — but a term match is a SEARCH,
	// and a search ordered by file path is a list somebody has to read all of.
	Terms int `json:"terms,omitempty"`
	// Caveat is raglit's one-line caution about the TEXT this hit was found in:
	// a model's description of an image rather than a transcription, or a read
	// nobody measured. Empty for ordinary transcription, and empty for the
	// identifier tiers, which match a NAME rather than a body.
	//
	// Carried because of what happens downstream. caselit binds a hit to an
	// element and DERIVES backing from the bind, so a passage a model invented
	// about a photograph and a passage transcribed off a recorded instrument
	// arrive at that decision indistinguishable unless this travels with them.
	Caveat string `json:"caveat,omitempty"`
	// Trust is the same fact structured — method, level, described percentage,
	// per-facet confidences, who ruled. Nil when raglit recorded nothing about
	// how the document was read, which is not a claim that it is reliable.
	Trust *client.HitTrust `json:"trust,omitempty"`
}

// viaRank orders the tiers by how much they can be trusted, most first.
//
// `text` ranks LAST, below even a filename, and the reason is worth stating: a
// document whose BODY contains an identifier is usually a document that CITES
// that instrument, not the instrument itself. A purchase and sale agreement
// quoting "AF#201503110043" is not the survey. Ranked above `path` it displaced
// the real recorded survey — the correct answer was in the corpus, and this
// pointed somewhere else, which is worse than pointing nowhere.
//
// `terms` ranks BELOW even that, and deliberately so. The tiers above assert
// that a document IS the thing asked for; `terms` says only that it discusses the
// same subject, which is what makes it useful for finding evidence and useless
// for deciding possession. Every caller that treats a hit as "we hold this"
// must exclude it, exactly as they already exclude `text`.
// `sounds` sits below every EXACT tier and above free text: a phonetic match is
// weaker evidence than any spelling that actually agrees, and stronger than a
// document that merely shares vocabulary.
var viaRank = map[string]int{"alias": 0, "source": 1, "path": 2, "sounds": 3, "text": 4, "terms": 5}

// normalize strips a name to what is stable across the ways people write it.
// `AF#201503110042`, `AF 201503110042` and `201503110042` are one identifier;
// `PL99-0479` and `PL990479` are one certification. Punctuation and case carry no
// information in a document identifier and cost matches when they are honoured.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// registryPrefixes are the tokens people put in front of a document number.
// The same instrument is written "AF#201503110043", "AF 201503110043" and bare
// "201503110043", and a source titled "2008 Crestline survey 201503110043" carries
// no prefix at all. Requiring one made the query miss the very document it
// named.
var registryPrefixes = []string{"af", "afn", "afno", "pl", "form", "rec", "vol"}

// queryForms is the ways one identifier may be written, normalized. Matching on
// any of them is deliberate: a false negative here sends someone to a county
// records desk for a document already on disk, which is the whole failure.
func queryForms(query string) []string {
	q := normalize(query)
	out := []string{q}
	for _, p := range registryPrefixes {
		if bare, ok := strings.CutPrefix(q, p); ok && len(bare) >= minQuery {
			out = append(out, bare)
		}
	}
	return out
}

// matchesAny reports whether any written form of the query appears in s.
func matchesAny(s string, forms []string) bool {
	n := normalize(s)
	for _, f := range forms {
		if strings.Contains(n, f) {
			return true
		}
	}
	return false
}

// minQuery is the shortest normalized query that can be searched. Below this a
// substring match is noise — "35r" alone would hit any document containing that
// run of characters anywhere in a page of OCR.
const minQuery = 4

// Have reports where an identifier can be found in the corpus. Results are
// ordered by how authoritative the match is, and deduplicated per document so a
// file that matches four ways is reported once, by its strongest tier.
func Have(root, index string, g *Graph, query string) ([]Held, error) {
	if len(normalize(query)) < minQuery {
		return nil, fmt.Errorf("%q is too short to search for — give at least %d letters or digits", query, minQuery)
	}
	forms := queryForms(query)

	best := map[string]Held{} // doc path (or source id, when there is no path) -> best match
	keep := func(h Held) {
		k := h.Doc
		if k == "" {
			k = h.Source
		}
		if prev, ok := best[k]; ok && viaRank[prev.Via] <= viaRank[h.Via] {
			// Keep the stronger tier, but let it inherit an excerpt it does not
			// have — knowing WHERE in the file the name appears is useful even
			// when the match was made by alias.
			if prev.Excerpt == "" && h.Excerpt != "" {
				prev.Excerpt = h.Excerpt
				best[k] = prev
			}
			return
		}
		best[k] = h
	}

	// THE QUERY'S CONTENT WORDS, hoisted above the node tiers because they are
	// wanted there too. They used to be computed just before the document tiers,
	// which is where they were first needed and is why they never reached a node.
	want := identifierSet(query)
	terms := contentTerms(query, want)

	// Tiers 1 and 2: what the graph itself declares.
	docOfSource := map[string]string{}
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil {
			continue
		}
		rel, err := DocPathOf(root, n)
		if err != nil {
			rel = ""
		}
		docOfSource[id] = rel
		missing := rel == ""
		if rel != "" {
			if _, serr := os.Stat(filepath.Join(root, rel)); serr != nil {
				missing = true
			}
		}
		h := Held{Doc: rel, Source: id, Anchor: n.Source.Anchor, Declared: true, Missing: missing}
		matched := false
		for _, a := range n.Aliases {
			if matchesAny(a.As, forms) {
				h.Via = "alias"
				h.Excerpt = a.As
				keep(h)
				matched = true
				break
			}
		}
		// PHONETIC, and only after every exact tier has failed. A surname
		// survives OCR badly and predictably — doubled letters collapse, vowels
		// wander — so two spellings of one person become two entries and the
		// second is invisible to anyone searching the first. Reported as its own
		// tier so a reader can see the match was made by SOUND and check it.
		//
		// Never reached by an identifier: `soundsLike` refuses anything with a
		// digit in it, because a near miss on an auditor's file number is a
		// different instrument.
		if !matched {
			for _, a := range n.Aliases {
				if soundsAlikeIn(a.As, query) {
					h.Via = "sounds"
					h.Excerpt = a.As
					keep(h)
					matched = true
					break
				}
			}
		}
		if matchesAny(n.Body, forms) {
			h.Via = "source"
			h.Excerpt = firstLine(n.Body)
			keep(h)
			matched = true
		}
		if !matched && soundsAlikeIn(firstLine(n.Body), query) {
			h.Via = "sounds"
			h.Excerpt = firstLine(n.Body)
			keep(h)
			matched = true
		}
		// A DESCRIPTION FINDS A SOURCE, and until this only a reference could.
		//
		// Every tier above matches the query as a PHRASE or as an identifier, so
		// "the 1962 plat" is a substring of the body "The 1962 plat of Hollow
		// Ridge" and finds it, while "Do you have the 1962 plat?" is a substring
		// of nothing and finds it at no tier at all. The terms machinery already
		// existed for exactly this — a client's account DESCRIBES an instrument
		// rather than naming it — and reached only DOCUMENTS.
		//
		// It cost a caselit feature: the interview's check for "are you asking
		// for something we already hold" hands a whole QUESTION to this function,
		// and a question is a sentence, so it never fired on the input it
		// receives. The prompt carries 242 titles to compensate.
		//
		// Consistent with the asymmetry the note below draws: a source's body is
		// a short line somebody wrote deliberately, like an alias, and matching
		// it loosely is safe in a way that matching a page of OCR is not.
		if !matched && len(terms) > 0 {
			if hits, ok := bodyCoveredBy(n.Body, terms); ok {
				h.Via = "terms"
				h.Excerpt = firstLine(n.Body)
				h.Terms = hits
				keep(h)
			}
		}
	}

	// Tiers 3 and 4: what is on disk, whether or not the graph knows about it.
	//
	// These match by EXTRACTED IDENTIFIER, never by substring. A filename or a
	// page of OCR is long, uncurated text; asking whether it CONTAINS a string
	// answered yes for every document that merely cites the instrument, and
	// ranked a purchase and sale agreement above the recorded survey it quoted.
	// Pulling identifiers out and comparing them as tokens has the same reach and
	// none of that: "AF#201503110043" in a body is found because it is an auditor
	// file number, not because the characters happen to appear.
	//
	// Aliases above are matched loosely on purpose, and the asymmetry is the
	// design. An alias is a short line somebody wrote deliberately, so a partial
	// name — "Cartwright" — should find it. Document content is neither short nor
	// deliberate.
	declared := map[string]bool{}
	for _, rel := range docOfSource {
		if rel != "" {
			declared[rel] = true
		}
	}
	docs, err := g.corpusListing(root, index)
	if err != nil {
		return nil, err
	}

	// THE CONTENT TIER IS RAGLIT'S ANSWER, not a walk of sidecars. raglit holds
	// the text of the whole corpus — .docx extracted natively, scans OCR'd, audio
	// transcribed — so it can answer "which documents use these words" for every
	// document, including the ones no sidecar was ever written for. havesearch.go
	// states why, and why an unreachable daemon is an error rather than silence.
	idx := raglitIndexName(root)
	found, err := contentHits(root, idx, query, want, terms, 25)
	if err != nil {
		return nil, err
	}

	for _, rel := range docs {
		if hit, ok := sharesIdentifier(rel, want); ok {
			keep(Held{Doc: rel, Via: "path", Excerpt: hit, Declared: declared[rel],
				Source: sourceFor(docOfSource, rel)})
		}
		if h, ok := found[rel]; ok {
			keep(Held{Doc: rel, Via: h.via, Excerpt: h.excerpt, Terms: h.terms,
				Caveat: h.caveat, Trust: h.trust,
				Declared: declared[rel], Source: sourceFor(docOfSource, rel)})
			continue
		}
		// No raglit index: nothing has been ingested, so there is no daemon to be
		// down and the only text available is whatever is on disk. This is the
		// path a test's temp corpus takes, and a tree nobody has indexed.
		if idx != "" {
			continue
		}
		tr, _ := companions(root, rel)
		if tr == "" {
			if !isTextDoc(rel) {
				continue
			}
			tr = rel
		}
		if line, via, hits, ok := grepTranscript(filepath.Join(root, tr), want, terms); ok {
			keep(Held{Doc: rel, Via: via, Excerpt: line, Terms: hits, Declared: declared[rel],
				Source: sourceFor(docOfSource, rel)})
		}
	}

	out := make([]Held, 0, len(best))
	for _, h := range best {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if viaRank[out[i].Via] != viaRank[out[j].Via] {
			return viaRank[out[i].Via] < viaRank[out[j].Via]
		}
		// Within the search tier, most terms first. Everything else stays sorted
		// by path, which is what makes the report diffable; Terms is zero there,
		// so this comparison does nothing to it.
		if out[i].Terms != out[j].Terms {
			return out[i].Terms > out[j].Terms
		}
		return out[i].Doc < out[j].Doc
	})
	return out, nil
}

// StrongestOnDisk picks the best match that is an actual FILE.
//
// A source declared with no `doc:` is not a document we hold — it is a citation
// to something nobody has. Reporting it as "ALREADY HELD at " (with an empty
// path, which is exactly what it printed) tells someone to close a question
// asking for a document that is genuinely missing. That is the one failure this
// check must never produce, because it destroys evidence rather than finding it.
//
// EXPORTED because it is the possession DECISION, and a caller that reads
// `Have`'s tiers itself will get it wrong. caselit's `already-held` audit rule
// did exactly that: it hand-rolled the loop, skipped `text` and forgot `terms`,
// and raised an ERROR saying four documents were "already in the corpus" against
// settlement letters that shared a word with the request. Every caller deciding
// whether somebody HOLDS something calls this; `Have` itself is for listing
// leads, where a term hit is the point.
func StrongestOnDisk(held []Held, allowText bool) (Held, bool) {
	for _, h := range held {
		if h.Doc == "" || h.Missing {
			continue
		}
		// `terms` is EXCLUDED ALWAYS, not gated by allowText. A shared vocabulary
		// is not possession, and this function decides whether to tell somebody
		// they already hold a document — the one answer that, wrong, destroys
		// evidence by closing the question that would have found it.
		if h.Via == "terms" {
			continue
		}
		if h.Via == "text" && !allowText {
			continue
		}
		return h, true
	}
	return Held{}, false
}

func sourceFor(docOfSource map[string]string, rel string) string {
	for id, d := range docOfSource {
		if d == rel {
			return id
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// identifierSet is every registry identifier written in a string, normalized.
// Empty when the string carries none — a plain name like "the Cartwright
// contract" is not an identifier and is matched against aliases only.
func identifierSet(s string) map[string]bool {
	out := map[string]bool{}
	// EVERY pattern below requires at least one digit, so a string carrying none
	// cannot match any of them. Checked first because it is the common case by a
	// wide margin — most lines of prose and most filenames have no digits — and
	// because the alternative is running three backtracking regexes to discover
	// the same thing.
	//
	// This is not a heuristic: it is only ever a fast path to the answer the
	// regexes would have given. TestFastPathMatchesTheSlowPath compares them.
	if !hasDigitASCII(s) {
		return out
	}
	for _, pat := range identifierPatterns {
		// Same reasoning one level down: `AF\s*#?\s*\d{8,14}` cannot match a
		// string with no "af" in it, and a substring scan is orders of magnitude
		// cheaper than the backtracking the regex does to find that out.
		if pat.lit != "" && !containsFoldASCII(s, pat.lit) {
			continue
		}
		for _, m := range pat.re.FindAllString(s, -1) {
			for _, f := range queryForms(m) {
				out[f] = true
			}
		}
	}
	return out
}

// sharesIdentifier reports whether s carries any of the wanted identifiers,
// compared as whole tokens rather than as substrings.
func sharesIdentifier(s string, want map[string]bool) (string, bool) {
	if len(want) == 0 {
		return "", false
	}
	for id := range identifierSet(s) {
		if want[id] {
			return id, true
		}
	}
	// A filename routinely writes a bare number with no registry prefix —
	// `8801200011-1984-SWD-cartwright-to-halloway.pdf`. That is still the
	// identifier, so bare digit runs long enough to be one are compared too.
	for _, tok := range bareNumbers(s) {
		if want[tok] {
			return tok, true
		}
	}
	return "", false
}

// bareNumberRe is a digit run long enough to be a registry number and not a
// date, a page count, or a dollar figure.
var bareNumberRe = regexp.MustCompile(`\d{8,14}`)

func bareNumbers(s string) []string {
	if !hasDigitASCII(s) {
		return nil
	}
	return bareNumberRe.FindAllString(s, -1)
}

// hasDigitASCII reports whether s contains an ASCII digit, without allocating.
func hasDigitASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// containsFoldASCII reports whether s contains lit, folding ASCII case. lit must
// be lowercase ASCII.
//
// Hand-written rather than strings.Contains(strings.ToLower(s), lit) because
// this runs once per corpus file and once per transcript LINE, and ToLower
// allocates a copy of every one of them.
func containsFoldASCII(s, lit string) bool {
	n := len(lit)
	if n == 0 {
		return true
	}
	if len(s) < n {
		return false
	}
	lo, up := lit[0], lit[0]-('a'-'A')
	for i := 0; i+n <= len(s); i++ {
		if c := s[i]; c != lo && c != up {
			continue
		}
		match := true
		for j := 1; j < n; j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != lit[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// grepTranscript scans a transcript once for both an identifier and a run of
// content terms, and reports which tier matched.
//
// An identifier ANYWHERE in the file beats the best term line in it: naming an
// instrument is stronger evidence that a document is about it than sharing
// vocabulary with it, and the ranking has to hold within a file as well as
// across the corpus.
func grepTranscript(abs string, want map[string]bool, terms []string) (string, string, int, bool) {
	if len(want) == 0 && len(terms) == 0 {
		return "", "", 0, false
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", "", 0, false
	}
	need := minTermHits(terms)
	bestLine, bestHits := "", 0
	for _, ln := range strings.Split(string(b), "\n") {
		if len(want) > 0 {
			if _, ok := sharesIdentifier(ln, want); ok {
				return excerpt(ln), "text", 0, true
			}
		}
		if need == 0 {
			continue
		}
		// The line carrying the MOST of the query's terms, not the first to
		// clear the bar: a page that shares four content words with the passage
		// is a better excerpt than one that shares three, and the excerpt is
		// what a person reads before deciding whether to cite it.
		if n := termHits(ln, terms); n > bestHits {
			bestLine, bestHits = ln, n
		}
	}
	// `need > 0` is not redundant with the loop above. A query that names only an
	// identifier has no content terms at all, and `0 >= 0` reported every
	// transcript in the corpus as a term match with an empty excerpt.
	if need > 0 && bestHits >= need {
		return excerpt(bestLine), "terms", bestHits, true
	}
	return "", "", 0, false
}

func excerpt(ln string) string {
	ln = strings.TrimSpace(ln)
	if len([]rune(ln)) > 160 {
		return string([]rune(ln)[:160]) + "\u2026"
	}
	return ln
}

// identifierPatterns are the document identifiers this corpus writes in prose.
// Deliberately narrow: an identifier that is nearly all digits, or carries a
// registry prefix, denotes ONE instrument. Matching looser phrases ("the survey",
// "the listing agreement") would warn constantly and be ignored, which is worse
// than not warning.
var identifierPatterns = []struct {
	re *regexp.Regexp
	// unique marks an identifier that names exactly ONE instrument in a public
	// registry. Only these are trusted to a body-text match: an auditor file
	// number appearing inside a document is usually that document, or a document
	// citing it, and either is worth a look.
	//
	// A form number is not unique. "Form 17" appears in every letter that
	// discusses a Form 17, and matching those produced warnings pointing at a
	// settlement-response .docx as though it were the disclosure statement. So a
	// form is matched by alias, title or filename only — the tiers that assert
	// the document IS the thing rather than mentions it.
	unique bool
	// lit is a lowercase literal the pattern cannot match without. It is a
	// PRE-FILTER, never the match: identifierSet skips the regex when lit is
	// absent, which is almost always, and the regex still decides every hit.
	lit string
}{
	{regexp.MustCompile(`(?i)\bAF\s*#?\s*\d{8,14}\b`), true, "af"},         // auditor file number
	{regexp.MustCompile(`(?i)\bPL\s*\d{2}\s*-\s*\d{4}\b`), true, "pl"},     // county lot certification
	{regexp.MustCompile(`(?i)\bForm\s+\d{1,3}\s*[A-Z]?\b`), false, "form"}, // NWMLS form
}

// CheckAlreadyHeld reports open questions and actions that ask someone to obtain
// a document the corpus already holds.
//
// This is a heuristic on purpose, and it earns that by being the only tier that
// would have caught all three misses. An explicit "what does this question seek"
// field would be precise, and nobody wrote one on any of the three occasions it
// mattered — a field that must be remembered does not help with the failure mode
// of forgetting. So the prose is scraped instead.
//
// Warns, never errors. A false positive here costs one glance; a hard failure on
// a heuristic would get the check disabled, and then it protects nothing.
func (g *Graph) CheckAlreadyHeld(root, index string) []Diag {
	var diags []Diag
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Status != SOpen || (n.Kind != KQuestion && n.Kind != KAction) {
			continue
		}
		// The reason field explains WHY the question is open and routinely names
		// documents already in hand as context. Scanning it produces warnings
		// about documents nobody asked for, so only the ask itself is scraped.
		for _, pat := range identifierPatterns {
			for _, m := range pat.re.FindAllString(n.Body, -1) {
				key := n.ID + "\x00" + normalize(m)
				if seen[key] {
					continue
				}
				seen[key] = true
				held, err := Have(root, index, g, m)
				if err != nil {
					continue
				}
				h, ok := StrongestOnDisk(held, pat.unique)
				if !ok {
					continue
				}
				where := h.Doc
				if h.Anchor != "" {
					where += " (" + h.Anchor + ")"
				}
				undeclared := ""
				if !h.Declared {
					undeclared = ", and no source cites it"
				}
				// A body-text hit is a WEAKER claim and is worded as one. It says
				// some document in the corpus discusses this identifier, which is a
				// lead, not a holding. Overstating it teaches people to ignore the
				// check.
				// KEYED ON THE QUESTION AND THE IDENTIFIER, not on the document that
				// answers it: the same question can match several identifiers, and each
				// is a separate thing to rule on.
				fkey := hashString(fmt.Sprintf("%s\x00%s\x00%s\x00%s",
					CheckAlreadyHeld, n.ID, m, g.SemHash[n.ID]))
				if h.Via == "text" {
					diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
						Check: CheckAlreadyHeld, Key: fkey, Subjects: []string{n.ID}, Msg: fmt.Sprintf(
							"%q asks for %s, and %s already mentions it%s — check whether we hold it before requesting it",
							n.ID, m, where, undeclared)})
					continue
				}
				diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
					Check: CheckAlreadyHeld, Key: fkey, Subjects: []string{n.ID}, Msg: fmt.Sprintf(
						"%q asks for %s, which is ALREADY HELD at %s%s — close it, or say what is still missing",
						n.ID, m, where, undeclared)})
			}
		}
	}
	return diags
}

// Identifiers is every registry identifier written in a string, as written and
// deduplicated.
//
// A document named `8801200011-1984-SWD-cartwright-to-halloway.pdf` already
// declares its auditor file number; copying that into an `aliases:` list would
// duplicate a fact the filename carries and let the two drift. So identifiers
// are DERIVED for display and lookup, and the alias list is reserved for the
// names nothing can derive — "the Cartwright fulfillment deed", "the 1999 parent
// parcel certification" — which is the knowledge a catalogue is actually for.
func Identifiers(s string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.Join(strings.Fields(v), " ")
		if k := normalize(v); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	for _, pat := range identifierPatterns {
		for _, m := range pat.re.FindAllString(s, -1) {
			add(m)
		}
	}
	// A bare digit run is shown as itself. Prefixing it "AF " labelled a title
	// commitment number 620047455 as an auditor file number, which is a claim
	// about which registry it belongs to that the filename does not make.
	for _, m := range bareNumbers(s) {
		if !seen[normalize(m)] {
			add(m)
		}
	}
	return out
}

// contentTerms are the words in a query worth searching a transcript for.
//
// Short words and stopwords are dropped because a line matching "the" and "of"
// matches every line. What is left is the vocabulary that makes a passage about
// something: in "the 25-foot strip the driveway sits on is the old railroad
// right of way" that is 25, foot, strip, driveway, sits, railroad, right. The
// bare number is kept because a number in a legal description is usually
// load-bearing.
//
// AN IDENTIFIER'S OWN PIECES ARE NOT CONTENT. `PL 99-0479` tokenizes to 99 and
// 0479, and searching for those as words returns every line that happens to
// carry both numbers — noise on a query whose identifier tier already answered
// it exactly. So tokens belonging to an identifier the query names are dropped,
// which also means a bare identifier query runs no term pass at all and keeps
// the fast path it had. A query that names an identifier AND describes
// something still gets both tiers; only the digits are spent.
func contentTerms(query string, ids map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range termTokens(query) {
		if seen[t] || termStop[t] || partOfIdentifier(t, ids) {
			continue
		}
		// Digits count from two characters: "25" in "25-foot" is the number
		// that matters. Words need four, below which they are almost all
		// function words the stoplist does not happen to name.
		if allDigits(t) {
			if len(t) < 2 {
				continue
			}
		} else if len(t) < 4 {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// partOfIdentifier reports whether a token is a piece of an identifier the
// query already names. The forms in ids are normalized — `pl990479`, `990479` —
// so containment is the test.
func partOfIdentifier(t string, ids map[string]bool) bool {
	for id := range ids {
		if strings.Contains(id, t) {
			return true
		}
	}
	return false
}

// termTokens splits prose into the alphanumeric runs inside it.
//
// It is NOT normalize, and the difference is the whole reason the term tier
// found nothing at first. `normalize` deletes punctuation and JOINS what it
// separated, which is right for an identifier — `AF#201503110042` and
// `AF 201503110042` are one number — and wrong for prose, because the two sides
// of a search hyphenate differently and never recover:
//
//	query    "the 25-foot strip …"     -> 25foot
//	document "the 100 foot wide railroad right-of-way …" -> rightofway
//
// The passage and the instrument it describes then share zero tokens while
// sharing four words. That is exactly what happened to the 1993 quitclaim: its
// best line scored one hit out of six terms, and the one it scored was
// `railroad`, the only content word in the line that nobody had hyphenated.
//
// So a hyphenated compound contributes its PARTS. `right-of-way` is right, of,
// way; `25-foot` is 25, foot. The parts are ordinary words, and precision is
// held by minTermHits requiring three of them at once rather than by pretending
// a compound is one rare token.
func termTokens(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// minTermHits is how many distinct query terms a line must carry.
//
// THREE, and the number is the whole precision knob. One is every page that
// says "driveway". Requiring all of them finds nothing, because a transcript
// does not repeat a sentence written by somebody who never read it. Three
// distinct content words co-occurring on one line is the point at which a match
// is usually about the same thing.
//
// A two-term query must match both: matching on one word would be a substring
// search.
//
// A ONE-TERM QUERY RUNS NO TERM SEARCH AT ALL, which is not the same rule and
// was learned by watching it. A caret sitting in a markdown heading sent
// "# Bramble v." through this; the only content word was a party name, need came
// out 1, and the panel filled with every transcript in the matter that mentions
// the client — presented as evidence for a heading. One word is a subject only
// by accident. Naming an instrument is what the identifier tiers are for.
// bodyCoveredBy reports whether a query's terms cover a node's whole body.
//
// THE TEST RUNS THE OTHER WAY ROUND HERE, and that is the point. `minTermHits`
// asks how many of the QUERY's terms a line holds, and needs three for a longer
// query — right for a page of OCR, where three shared words is the difference
// between a match and a coincidence. A source's body is a short line somebody
// wrote deliberately: "Cartwright deed" holds two content words in total, so a
// question that names both still failed a threshold of three and the source was
// unfindable by description.
//
// So the question is how much of the BODY the query covers, not how much of the
// query the body covers. Every content word of the body has to appear, which is
// what makes "Do you have the Cartwright deed anywhere?" find the deed and
// "What did you have for lunch?" find nothing.
//
// It is the weakest tier and it is excluded from POSSESSION always — see
// StrongestOnDisk. A loose match here is a lead somebody may ignore; a loose
// match there closes the question that would have found the document.
func bodyCoveredBy(body string, terms []string) (int, bool) {
	own := contentTerms(body, nil)
	if len(own) == 0 {
		return 0, false
	}
	have := map[string]bool{}
	for _, t := range terms {
		have[t] = true
	}
	for _, t := range own {
		if !have[t] {
			return 0, false
		}
	}
	return len(own), true
}

func minTermHits(terms []string) int {
	if len(terms) < 2 {
		return 0
	}
	if len(terms) == 2 {
		return 2
	}
	return 3
}

// termHits counts how many distinct terms appear in a line, as whole tokens.
//
// Whole tokens, not substrings: "way" must not match "always", and a
// substring pass over an OCR transcript matches almost everything.
func termHits(ln string, terms []string) int {
	if len(terms) == 0 || strings.TrimSpace(ln) == "" {
		return 0
	}
	tok := map[string]bool{}
	for _, t := range termTokens(ln) {
		tok[t] = true
	}
	n := 0
	for _, t := range terms {
		if tok[t] {
			n++
		}
	}
	return n
}

// termStop are words that carry no subject. Only four letters and over are
// listed, because contentTerms drops anything shorter anyway.
var termStop = map[string]bool{
	"about": true, "after": true, "again": true, "against": true, "also": true,
	"back": true, "because": true, "been": true, "before": true, "being": true,
	"both": true, "came": true, "come": true, "could": true, "did": true,
	"does": true, "down": true, "each": true, "even": true, "ever": true,
	"every": true, "from": true, "gave": true, "give": true, "goes": true,
	"gone": true, "had": true, "has": true, "have": true, "here": true,
	"how": true, "into": true, "just": true, "know": true, "like": true,
	"made": true, "make": true, "many": true, "more": true, "most": true,
	"much": true, "must": true, "near": true, "need": true, "never": true,
	"next": true, "not": true, "now": true, "only": true, "onto": true,
	"other": true, "our": true, "out": true, "over": true, "own": true,
	"said": true, "same": true, "says": true, "she": true, "should": true,
	"since": true, "some": true, "such": true, "take": true, "than": true,
	"that": true, "the": true, "their": true, "them": true, "then": true,
	"there": true, "these": true, "they": true, "this": true, "those": true,
	"through": true, "time": true, "under": true, "upon": true, "very": true,
	"was": true, "well": true, "went": true, "were": true, "what": true,
	"when": true, "where": true, "which": true, "while": true, "who": true,
	"will": true, "with": true, "would": true, "your": true,
}

// AddAliasTo records an alias through the STORE: a re-assertion carrying the
// enlarged list.
//
// `AddAlias` edits the declaring file and takes care to touch exactly one line,
// because the text was authoritative and hand-written and a tool that reflows it
// makes its own diffs unreviewable. That care is no longer needed and the
// property it protected is now free: an assert is a new line, so nothing else in
// the corpus moves by construction.
func AddAliasTo(st Store, dl Dialect, g *Graph, target, alias, by string) error {
	n, ok := g.Nodes[target]
	if !ok {
		return fmt.Errorf("no node %q in this index", target)
	}
	if strings.TrimSpace(alias) == "" {
		return fmt.Errorf("an empty alias names nothing")
	}
	// Refusing a duplicate rather than appending it: two identical aliases are
	// not an error worth failing a script over, but silently growing the list on
	// every re-run is how a curated catalogue turns into noise.
	for _, a := range n.Aliases {
		if normalize(a.As) == normalize(alias) {
			return fmt.Errorf("%q already carries the alias %q", target, a.As)
		}
	}
	// An alias that already denotes something ELSE is the ambiguity the alias
	// system exists to surface, not to create. `P74736` meant the parent parcel
	// before 2021 and Lot I after; two undated bindings make every reference to
	// it unresolvable, so this refuses and says where the other one is.
	for _, b := range g.aliases[alias] {
		if b.Node != target {
			return fmt.Errorf("%q already denotes %q — if both are true, date them with `from:`/`until:` by hand",
				alias, b.Node)
		}
	}
	fields, found, err := LastFields(st, target)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no assertion of %q to amend", target)
	}
	var list []any
	if prev, ok := fields["aliases"].([]any); ok {
		list = append(list, prev...)
	}
	list = append(list, map[string]any{"as": alias})
	_, err = Amend(st, dl, target, map[string]any{"aliases": list}, by, "alias added: "+alias)
	return err
}
