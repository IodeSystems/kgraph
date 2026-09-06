package kgraph

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Does the document contain the words the fact puts in quotation marks?
//
// `attest` asks a person that question and records a durable verdict, which is
// right: for a scanned exhibit with no text layer, nothing else CAN answer it.
// But where a transcription exists, the narrow mechanical part of the question —
// do these exact words appear — is answerable now, and answering it changes what
// the person should look at first.
//
// This is triage, never a verdict. A match is not attestation: the words can be
// present and the fact still misread them, and only a human can say so. So this
// records nothing and writes nothing. It ranks.
//
// What makes it worth having is the failure it catches. A transcription that is
// silently INCOMPLETE reads exactly like a complete one — a survey looped during
// OCR, the retry returned tidy prose that had dropped the entire legal
// description, and the sidecar looked finished. Nothing downstream could tell.
// The graph could: a fact quoted the missing clause and cited that document, so
// the clause could be looked for. It was not there.
//
// An absent quote therefore means one of two things, and both are worth a
// person's time: the transcription dropped the passage, or the citation is on
// the wrong document.
type QuoteCheck struct {
	Fact   string `json:"fact"`
	Source string `json:"source"`
	Doc    string `json:"doc,omitempty"`
	// Transcript is the readable form actually searched. Empty means the source
	// has none, and the check is reported as UNREADABLE rather than as a miss —
	// silence about a document nobody can read is not evidence against the fact.
	Transcript string `json:"transcript,omitempty"`
	Quote      string `json:"quote"`
	State      string `json:"state"` // present | absent | unreadable
	// Page and Line locate a PRESENT quote inside the transcription, so a person
	// verifying it can open the document at the right place instead of searching
	// a thirty-page scan. Page is the `## Page N` marker raglit writes, and is 0
	// when the transcription has none (an email, a plain text file). Line is
	// 1-based within the transcription and is always set for a present quote.
	//
	// Both are zero for absent and unreadable — there is no position for text
	// that was not found.
	// FoundIn names another source in the index whose document DOES contain the
	// quoted words, when the cited one does not.
	//
	// Measured on the live corpus: of 15 absent quotes, 4 cite the wrong document
	// and the right one is a sibling file. Without this the tool says only
	// "absent" and leaves a person to grep a gigabyte of evidence by hand — which
	// is a search the corpus can do itself, and the one part of that queue that
	// was never a judgement call.
	//
	// A CANDIDATE, never a correction. Two instruments can legitimately quote the
	// same passage, so this says where the words are and a person says which
	// document the fact rests on.
	FoundIn  string `json:"found_in,omitempty"`
	FoundDoc string `json:"found_doc,omitempty"`
	// FoundCount is HOW MANY of the index's documents contain the words, and it
	// is what makes FoundIn safe to read.
	//
	// FoundIn returns the alphabetically first match, which is an arbitrary pick
	// when the passage is recited — and in a boundary dispute recitation is the
	// normal state of the material: one plat certification's language sits in the
	// deed, the survey, the title commitment, the complaint and every
	// declaration. Reported alone it named one of fourteen documents and read as
	// "the document you should have cited", which sent a person to correct a
	// citation that was right.
	//
	// 1 says the words have exactly one home and the citation is worth a look.
	// A large number says the passage is recited and containment cannot decide
	// anything — a fact cites the instrument that GOVERNS, not one that repeats
	// the language.
	FoundCount int `json:"found_count,omitempty"`
	// Near is the passage in the CITED document that the quotation nearly
	// matches, and NearRatio how alike they are.
	//
	// A QUOTE CAN BE WRONG WITHOUT BEING INVENTED, and `absent` was reporting
	// both. On the live corpus several absent quotations differ from their
	// document by one word — `impute` where the opinion says `imputes` — while
	// others are not there at all. Opposite findings, opposite fixes: correct the
	// quotation, versus find out what the document really says.
	Near      string  `json:"near,omitempty"`
	NearRatio float64 `json:"near_ratio,omitempty"`
	Page      int     `json:"page,omitempty"`
	Line      int     `json:"line,omitempty"`
}

// Locator renders Page/Line the way a citation wants them, and is empty when
// the quote was never located.
func (q QuoteCheck) Locator() string {
	switch {
	case q.Line == 0:
		return ""
	case q.Page > 0:
		return fmt.Sprintf("p. %d, line %d", q.Page, q.Line)
	default:
		return fmt.Sprintf("line %d", q.Line)
	}
}

// curlyQuote matches a properly paired typographic quotation.
//
// Straight quotes get no regex at all, and that is deliberate: they are not
// directional, so a length-filtered pattern skips a short pair and then pairs its
// CLOSING quote with the next OPENING one. Measured on a real corpus, that
// reported the prose BETWEEN two quotations as missing from a document that never
// claimed to contain it. Straight quotes are paired by position instead.
var curlyQuote = regexp.MustCompile(`“([^“”]{4,400})”`)

// minQuoteChars is how much normalized text a span needs before it is worth
// checking. Short quotations — a single word like "Applicant" — carry too little
// signal: they match by accident, and their absence says nothing.
const minQuoteChars = 40

// quotedSpans is every run the author marked as quoted, in order.
func quotedSpans(text string) []string {
	var out []string
	for _, m := range curlyQuote.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	var pos []int
	for i, r := range text {
		if r == '"' {
			pos = append(pos, i)
		}
	}
	// Pair by position: first with second, third with fourth. A trailing unpaired
	// quote is unbalanced text and is dropped rather than run to the end.
	for i := 0; i+1 < len(pos); i += 2 {
		out = append(out, text[pos[i]+1:pos[i+1]])
	}
	return out
}

var (
	editorial = regexp.MustCompile(`\[[^\]]{0,20}\]`)
	ellipsis  = regexp.MustCompile(`\s*(?:\.\.\.|…)\s*`)
	// RE2 has no lookahead, so the character after the number is CAPTURED and put
	// back. Written with (?=\S) this compiled nowhere and panicked at package
	// init, which broke every kg command, not just this one.
	// `\p{Zs}`, not `[ \t]`. A layout-aware VLM indents the numbered line with
	// NON-BREAKING spaces — "1\u00a0\u00a0\u00a0\u00a011. The First Dace Survey..." — and U+00A0
	// is not in Go's `\s`, so an ASCII-only class matched only the lines that
	// happened to be continuations. Measured on one pleading page: 13 of the 23
	// numbered lines matched, which is worse than none of them. It cleared the
	// guard below, stripped the continuation numbers, and left every
	// paragraph-opening number in the text — landing them MID-SENTENCE once the
	// page was folded, which is precisely the artifact this exists to remove.
	lineNo = regexp.MustCompile(`(?m)^[\t\p{Zs}]{0,4}\d{1,2}[\t\p{Zs}]+(\S)`)
)

// checkableQuotes are the spans long enough to mean something, with the author's
// own marks removed.
//
// `[sic]` is commentary on the document, not text from it — "Northwind tile [sic]
// stated" is the author flagging a typo the document itself does not contain. An
// ellipsis marks an elision, so the halves are checkable and the whole is not.
func checkableQuotes(text string) []string {
	var out []string
	for _, raw := range quotedSpans(text) {
		q := strings.Join(strings.Fields(editorial.ReplaceAllString(raw, " ")), " ")
		for _, part := range ellipsis.Split(q, -1) {
			if len(foldText(part)) >= minQuoteChars {
				out = append(out, strings.TrimSpace(part))
			}
		}
	}
	return out
}

// stripPleadingLineNumbers removes the numbers printed down a pleading's margin,
// but ONLY from a document that actually numbers its lines.
//
// They interleave with the text — "Defendants shall\n7 cooperate as reasonably
// necessary" — so a quotation spanning a line break never matches. Stripping them
// unconditionally is worse than not stripping them at all: a deed wraps as
// "...lines of Lot\n2 of said plat", and removing that "2" turned a CORRECT
// transcription into a reported omission of the exact clause this check exists to
// protect.
func stripPleadingLineNumbers(s string) string {
	lines := strings.Split(s, "\n")
	num := make([]bool, len(lines))
	for i, ln := range lines {
		num[i] = strings.TrimSpace(ln) != "" && lineNo.MatchString(ln+" ")
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		if num[i] && numberedNeighbourhood(lines, num, i) {
			ln = lineNo.ReplaceAllString(ln, " $1")
		}
		out[i] = ln
	}
	return strings.Join(out, "\n")
}

// numberedNeighbourhood says whether line i sits INSIDE a numbered page.
//
// The guard used to be a whole-document ratio, and a court filing is the one
// shape that defeats it: a numbered six-page declaration followed by thirty
// unnumbered pages of exhibits. Measured on one — 97 numbered lines against
// 1635 — the exhibits outvoted the pleading, the ratio failed, and NOTHING was
// stripped, so every quotation spanning a line break in the part that actually
// is a pleading reported a near miss.
//
// Line numbering is a property of a PAGE. Judging it over a window of about a
// page keeps the original thresholds honest where they were right — a deed that
// wraps as "...lines of Lot\n2 of said plat" has no dense run anywhere, so its
// "2" survives exactly as before — while letting a numbered section be
// recognised inside a document that is mostly not one.
const pleadingWindow = 20

func numberedNeighbourhood(lines []string, num []bool, i int) bool {
	lo, hi := i-pleadingWindow, i+pleadingWindow
	if lo < 0 {
		lo = 0
	}
	if hi > len(lines)-1 {
		hi = len(lines) - 1
	}
	var n, total int
	for j := lo; j <= hi; j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		total++
		if num[j] {
			n++
		}
	}
	return n >= 8 && n*5 >= total
}

// foldText reduces text to letters and digits.
//
// OCR and an author disagree about hyphens, smart quotes, line breaks and
// spacing constantly, and none of that is a missing passage. Folding it away is
// what keeps this about CONTENT.
// It is defined as foldTextIdx without the index, deliberately: the quote is
// folded by one of these and the document by the other, so any divergence
// between them is a match that silently fails to happen. One implementation
// cannot diverge from itself.
func foldText(s string) string {
	f, _ := foldTextIdx(s)
	return f
}

// foldTextIdx folds exactly as foldText does, and records where each kept byte
// came from.
//
// The fold is what makes matching survive OCR, so a hit is an offset into the
// FOLDED text and means nothing to a human. offs[i] is the byte offset in the
// original of the i'th folded byte, which is what turns a match back into a
// place in the document. Kept bytes are ASCII letters and digits, so a folded
// byte index is also a folded rune index and the two can be used
// interchangeably.
func foldTextIdx(s string) (string, []int) {
	// STAR PAGINATION IS NOT CONTENT. A reported opinion carries the printed
	// page boundary inside the sentence — "a broker or seller has a *177 duty to
	// disclose" — and the fold keeps DIGITS, so `*177` became `177` in the middle
	// of the text and every quote spanning a page reported ABSENT with the words
	// plainly there. That is a false negative, which is the expensive direction:
	// it sends a person to verify a citation that was correct.
	//
	// Masked here, in the one function both the quote and the document go
	// through, for the reason the comment above gives — two implementations of
	// the fold diverge and the divergence is a match that silently fails to
	// happen. Masked with SPACES rather than deleted so every byte offset still
	// points where it did, which is what keeps `offs` able to locate a hit.
	var b strings.Builder
	offs := make([]int, 0, len(s))
	skipTo := -1
	for i, r := range s {
		// STAR PAGINATION IS NOT CONTENT, and is skipped here rather than stripped
		// in a pre-pass, so the quote and the document get it identically — the
		// rule the comment above states, that one implementation cannot diverge
		// from itself.
		//
		// A reported opinion carries the printed page boundary INSIDE the
		// sentence: "a broker or seller has a *177 duty to disclose". The fold
		// keeps digits, so `*177` became `177` in the middle of the text and every
		// quote spanning a page reported ABSENT with the words plainly there — a
		// false negative, which is the expensive direction: it sends a person to
		// verify a citation that was correct.
		//
		// Skipped inline rather than by regex because this runs over every
		// document body, and markdown is full of asterisks — a `Contains(s, "*")`
		// guard would never fire. O(n), no allocation.
		if i < skipTo {
			continue
		}
		if r == '*' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j > i+1 {
				skipTo = j
				continue
			}
		}
		l := unicode.ToLower(r)
		if unicode.IsLetter(l) || unicode.IsDigit(l) {
			// ToLower can change a rune's encoded width, and a multi-byte rune
			// folds to one entry per byte written. Record the source offset for
			// each byte so the mapping stays aligned.
			n := b.Len()
			b.WriteRune(l)
			for range b.Len() - n {
				offs = append(offs, i)
			}
		}
	}
	return b.String(), offs
}

// pageMark is raglit's page delineation in a transcription sidecar. Without it
// a quote from a thirty-page scan can be reported present and still take ten
// minutes to find, which is the whole reason the sidecar carries page markers.
var pageMark = regexp.MustCompile(`(?m)^#{1,6}\s*Page\s+(\d+)\b`)

// locate turns a byte offset in the transcription into a page and line.
//
// Page is the last `## Page N` marker at or before the offset — its NUMBER as
// written, not a count of markers seen, because a sidecar may start at a page
// other than one. Line is counted from the start of the file, not from the page
// marker: an editor's line number is what a person actually navigates with.
func locate(text string, off int) (page, line int) {
	if off < 0 || off > len(text) {
		return 0, 0
	}
	head := text[:off]
	line = strings.Count(head, "\n") + 1
	if m := pageMark.FindAllStringSubmatch(head, -1); len(m) > 0 {
		if n, err := strconv.Atoi(m[len(m)-1][1]); err == nil {
			page = n
		}
	}
	return page, line
}

// CheckQuotes looks for each fact's quoted text in the sources it cites.
//
// A quote need appear in only ONE of a fact's sources. Requiring it in every one
// flagged a fact that quotes a 1972 contract AND the 1984 deed that fulfilled it
// — correct about both, with the halves simply living in different documents.
// foldedDoc is a document's stripped original beside its fold, so a match can be
// turned back into a page and line. Package-level rather than local to
// CheckQuotes because `findQuoted` shares the caller's cache — folding a
// gigabyte of evidence once per fact is the difference between a search and a
// stall.
type foldedDoc struct {
	text, body string
	offs       []int
}

// readableSource is one source whose document could actually be read.
type readableSource struct {
	src, doc, tr string
	d            foldedDoc
}

func (g *Graph) CheckQuotes(root string) []QuoteCheck {
	cache := map[string]foldedDoc{}
	// Text comes from raglit, keyed by the DOCUMENT rather than by a sidecar path.
	// A quote is checked against what the instrument says, and a document raglit
	// reads natively — a .docx declaration, a recording — has no file beside it to
	// open. Under the old rule those quotes were simply never checked.
	read := func(rel string) foldedDoc {
		if _, ok := cache[rel]; !ok {
			raw := docText(root, rel)
			if raw == "" {
				cache[rel] = foldedDoc{}
			} else {
				t := stripPleadingLineNumbers(raw)
				f, offs := foldTextIdx(t)
				cache[rel] = foldedDoc{text: t, body: f, offs: offs}
			}
		}
		return cache[rel]
	}

	bySrc := map[string][]string{} // fact -> source ids
	for _, e := range g.Edges {
		if e.Type == EAttests && g.isSource(e.Src) {
			bySrc[e.Dst] = append(bySrc[e.Dst], e.Src)
		}
	}

	var out []QuoteCheck
	for fid, srcs := range bySrc {
		n := g.Nodes[fid]
		if n == nil || n.Kind != KClaim || n.Status == SWithdrawn {
			continue
		}
		quotes := checkableQuotes(n.Body)
		if len(quotes) == 0 {
			continue
		}
		var rs []readableSource
		for _, sid := range srcs {
			sn := g.Nodes[sid]
			if sn == nil || sn.Source == nil {
				continue
			}
			rel, err := DocPathOf(root, sn)
			if err != nil || rel == "" {
				continue
			}
			if d := read(rel); d.body != "" {
				rs = append(rs, readableSource{sid, rel, rel, d})
			}
		}
		for _, q := range quotes {
			if len(rs) == 0 {
				// Nothing readable to look in. Reported so the count is honest —
				// "we checked and found nothing" and "we could not check" are
				// different answers and only one of them is about the fact.
				out = append(out, QuoteCheck{Fact: fid, Source: srcs[0], Quote: q, State: "unreadable"})
				continue
			}
			fq := foldText(q)
			hit, off := -1, -1
			for i, r := range rs {
				if k := strings.Index(r.d.body, fq); k >= 0 {
					hit, off = i, k
					break
				}
			}
			state, at := "absent", rs[0]
			var page, line int
			var foundIn, foundDoc, near string
			var foundCount int
			var nearRatio float64
			if hit >= 0 {
				state, at = "present", rs[hit]
				if off < len(at.d.offs) {
					page, line = locate(at.d.text, at.d.offs[off])
				}
			} else {
				// ABSENT FROM ITS OWN DOCUMENT. Two things can still be true and they
				// want opposite fixes, so both are looked for before reporting:
				// the words may be in ANOTHER document (the citation is wrong), or
				// the cited document may say NEARLY this (the quotation is wrong).
				//
				// Both searches run only here, on the rare path — 14 of 615 findings
				// on the live corpus — so the common case pays nothing.
				foundIn, foundDoc, foundCount = g.findQuoted(root, fq, read, rs)
				if foundIn == "" {
					if p, r := nearMiss(q, at.d.text); p != "" {
						state, near, nearRatio = "near", p, r
					}
				}
			}
			out = append(out, QuoteCheck{Fact: fid, Source: at.src, Doc: at.doc,
				Transcript: at.tr, Quote: q, State: state, Page: page, Line: line,
				FoundIn: foundIn, FoundDoc: foundDoc, FoundCount: foundCount,
				Near: near, NearRatio: nearRatio})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		// `near` sorts just after `absent`: a wrong quotation is a smaller problem
		// than a missing one, and a bigger one than a citation that checks out.
		rank := map[string]int{"absent": 0, "near": 1, "unreadable": 2, "present": 3}
		if rank[out[i].State] != rank[out[j].State] {
			return rank[out[i].State] < rank[out[j].State]
		}
		if out[i].Fact != out[j].Fact {
			return out[i].Fact < out[j].Fact
		}
		return out[i].Quote < out[j].Quote
	})
	return out
}

// findQuoted looks for a folded quotation across the index's OTHER sources.
//
// Scoped to sources this index declares, not to a filesystem walk. A walk would
// reach the whole corpus — including another matter's evidence — and naming a
// document from a different index as the home of a quotation is the crossing
// this format refuses everywhere else. It is also how the first version of this
// search took minutes: `documents/` is a gigabyte.
//
// `read` is the caller's cache, so a document already folded for one fact is not
// folded again for the next.
//
// COUNTS, and does not stop at the first. The count is the whole difference
// between a candidate and a misdirection: in a boundary dispute the same legal
// description is recited across the deed, the survey, the title commitment, the
// complaint and every declaration, so "the words are in s-x" picked the
// alphabetically first of fourteen documents and read as an answer. Measured on
// the live corpus, five absent quotations of one plat certification each had
// double-figure candidates. A fact cites the instrument that GOVERNS, not any
// instrument that repeats the language, and no containment test can tell those
// apart — but the COUNT tells a person which question they are looking at.
func (g *Graph) findQuoted(root, fq string, read func(string) foldedDoc, skip []readableSource) (string, string, int) {
	seen := map[string]bool{}
	for _, r := range skip {
		seen[r.doc] = true
	}
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	// Sorted, so the candidate offered is the same one on every run — a
	// suggestion that moves between runs is one nobody can act on.
	sort.Strings(ids)
	var firstID, firstRel string
	var hits int
	for _, id := range ids {
		n := g.Nodes[id]
		if n.Kind != KSource || n.Source == nil {
			continue
		}
		rel, err := DocPathOf(root, n)
		if err != nil || rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		if d := read(rel); d.body != "" && strings.Contains(d.body, fq) {
			hits++
			if firstID == "" {
				firstID, firstRel = id, rel
			}
		}
	}
	return firstID, firstRel, hits
}

// AbsentQuotes is the (fact, source) pairs with at least one quotation the
// transcription does not contain — the subset of the attestation queue a person
// should work first.
func (g *Graph) AbsentQuotes(root string) map[string]bool {
	out := map[string]bool{}
	for _, q := range g.CheckQuotes(root) {
		if q.State == "absent" {
			out[q.Fact+"\x00"+q.Source] = true
		}
	}
	return out
}
