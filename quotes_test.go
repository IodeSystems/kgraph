package kgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quotesFacts is the fixture corpus, as authored entries. The vocabulary is
// unchanged from the `*.kfacts.md` it replaced; only the file around it moved.
var quotesFacts = "" +
	"- id: s-survey\n  record: Survey\n  doc: documents/survey.pdf\n  class: record\n" +
	"- id: s-deed\n  record: Deed\n  doc: documents/deed.pdf\n  class: record\n" +
	"- id: s-pleading\n  record: Pleading\n  doc: documents/pleading.pdf\n  class: record\n" +
	"- id: s-scan\n  record: Scan\n  doc: documents/scan.pdf\n  class: record\n" +
	"- id: c-dropped\n  claim: The survey says \"THAT LIES WESTERLY OF THE CENTERLINE OF SAID RIGHT-OF-WAY\"\n  status: asserted\n  attested_by: s-survey\n" +
	"- id: c-wrapped\n  claim: The deed says \"extensions of both the Northwesterly and Southeasterly lines of Lot 2 of said plat\"\n  status: asserted\n  attested_by: s-deed\n" +
	"- id: c-pleading\n  claim: The order says \"Defendants shall cooperate as reasonably necessary to determine the location\"\n  status: asserted\n  attested_by: s-pleading\n" +
	"- id: c-unreadable\n  claim: The scan says \"something nobody can mechanically verify here at all\"\n  status: asserted\n  attested_by: s-scan\n" +
	"- id: c-split\n  claim: Together they say \"THAT LIES WESTERLY OF THE CENTERLINE OF SAID RIGHT-OF-WAY\"\n  status: asserted\n  attested_by: [s-survey, s-deed]\n" +
	"- id: c-editorial\n  claim: He wrote \"Northwind tile [sic] stated they do not write easements but will record them\"\n  status: asserted\n  attested_by: s-deed\n" +
	"- id: c-short\n  claim: It names him as \"Applicant\"; her signature is in the \"In presence of\" column\n  status: asserted\n  attested_by: s-deed\n"

// quotesProject builds the shapes that made this check hard to get right: a
// transcription that dropped a passage, one that has it wrapped across a line,
// a pleading that numbers its lines, and a source with no transcription at all.
func quotesProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("documents/survey.pdf", "%PDF-1.4")
	// The real failure: a tidy transcription with the legal description gone.
	mk("documents/survey.ocr.md", "## Page 1\nRECORD OF SURVEY\n[FIGURE: plat map of the parcels.]\n")

	mk("documents/deed.pdf", "%PDF-1.4")
	// Correct, but wrapped mid-phrase — "lines of Lot\n2 of said plat".
	mk("documents/deed.ocr.md",
		"## Page 1\nthat lies Westerly of the centerline of said right-of-way\n"+
			"and between the Northeasterly extensions of both the Northwesterly\n"+
			"and Southeasterly lines of Lot\n2 of said plat;\n")

	mk("documents/pleading.pdf", "%PDF-1.4")
	mk("documents/pleading.ocr.md", "## Page 1\n"+numberedPleading())

	mk("documents/scan.pdf", "%PDF-1.4") // no transcription anywhere

	writeFactLog(t, root, "", quotesFacts)
	return root
}

// numberedPleading is a court filing's margin numbering: the number, then the
// text, on every line.
func numberedPleading() string {
	lines := []string{
		"1 IN THE SUPERIOR COURT OF THE STATE OF WASHINGTON",
		"2 IN AND FOR THE COUNTY OF CASCADE",
		"3 ORDER GRANTING PARTIAL SUMMARY JUDGMENT",
		"4 THIS MATTER came on for hearing before the undersigned",
		"5 judge, and the court having considered the pleadings,",
		"6 now therefore it is ORDERED that Defendants shall",
		"7 cooperate as reasonably necessary to determine the location",
		"8 of the sewer line along or near the shared boundary.",
		"9 DATED this day of April, 2023.",
		"10 JUDGE OF THE SUPERIOR COURT",
	}
	return strings.Join(lines, "\n") + "\n"
}

func checksBy(t *testing.T, root string) map[string]string {
	t.Helper()
	p := loadProject(t, root)
	out := map[string]string{}
	for _, c := range p.Graph.CheckQuotes(root) {
		// Last write wins is fine: each fixture fact has one checkable quote.
		out[c.Fact] = c.State
	}
	return out
}

// The failure the whole check exists for.
func TestCheckQuotesFindsAPassageDroppedFromTheTranscription(t *testing.T) {
	got := checksBy(t, quotesProject(t))
	if got["c-dropped"] != "absent" {
		t.Errorf("a quote missing from its source read as %q, want absent", got["c-dropped"])
	}
}

// A quotation that spans a line break is present. Folding whitespace away is
// what makes this a check about content.
func TestCheckQuotesMatchesAcrossLineWraps(t *testing.T) {
	got := checksBy(t, quotesProject(t))
	if got["c-wrapped"] != "present" {
		t.Errorf("a wrapped quote read as %q, want present — the line break is not a missing passage", got["c-wrapped"])
	}
}

// Pleadings number their lines and the numbers interleave with the text, so a
// quotation spanning a line never matches without stripping them.
func TestCheckQuotesIgnoresPleadingLineNumbers(t *testing.T) {
	got := checksBy(t, quotesProject(t))
	if got["c-pleading"] != "present" {
		t.Errorf("a quote spanning a numbered line read as %q, want present", got["c-pleading"])
	}
}

// ...but only where the document actually numbers its lines. Unconditional
// stripping ate the "2" from "lines of Lot\n2 of said plat" and reported a
// CORRECT transcription as missing the clause this check exists to protect.
func TestStripPleadingLineNumbersLeavesADeedAlone(t *testing.T) {
	deed := "and Southeasterly lines of Lot\n2 of said plat;\n"
	if got := stripPleadingLineNumbers(deed); got != deed {
		t.Errorf("a deed's wrapped text was mangled as line numbering:\n%q", got)
	}
	if got := stripPleadingLineNumbers(numberedPleading()); strings.Contains(got, "\n7 cooperate") {
		t.Error("a pleading's line numbers were not stripped")
	}
}

// A source nobody can read mechanically is not evidence against the fact.
func TestCheckQuotesReportsUnreadableRatherThanAbsent(t *testing.T) {
	got := checksBy(t, quotesProject(t))
	if got["c-unreadable"] != "unreadable" {
		t.Errorf("a source with no transcription read as %q, want unreadable", got["c-unreadable"])
	}
}

// A quote need appear in only ONE of a fact's sources.
func TestCheckQuotesAcceptsAQuoteFoundInAnyCitedSource(t *testing.T) {
	got := checksBy(t, quotesProject(t))
	if got["c-split"] != "present" {
		t.Errorf("a quote present in the second of two sources read as %q, want present", got["c-split"])
	}
}

// `[sic]` is the author's commentary on the document, not text from it.
func TestCheckQuotesStripsEditorialMarks(t *testing.T) {
	if qs := checkableQuotes(`He wrote "Northwind tile [sic] stated they do not write easements but will record them"`); len(qs) != 1 {
		t.Fatalf("want one checkable quote, got %d: %q", len(qs), qs)
	} else if strings.Contains(qs[0], "sic") {
		t.Errorf("the editorial mark was kept: %q", qs[0])
	}
}

// Straight quotes must pair by POSITION. A length-filtered regex skips a short
// pair and then pairs its closing quote with the next opening one, checking the
// prose BETWEEN two quotations as though the document had claimed it.
func TestCheckableQuotesPairsStraightQuotesByPosition(t *testing.T) {
	text := `names him alone as "Applicant"; her signature appears only in the "In presence of" witness column`
	for _, q := range quotedSpans(text) {
		if strings.HasPrefix(q, ";") {
			t.Fatalf("paired a closing quote with the next opening one: %q", q)
		}
	}
	// And the short ones are below the length floor, so nothing is checked here.
	if qs := checkableQuotes(text); len(qs) != 0 {
		t.Errorf("short quotations should not be checked, got %q", qs)
	}
}

// A withdrawn claim is not a live citation and must not appear in the queue.
func TestCheckQuotesSkipsWithdrawnClaims(t *testing.T) {
	root := quotesProject(t)
	// Withdrawn by a LATER LINE, which is what a withdrawal is now. Rewriting the
	// earlier one is the in-place edit the store exists to make impossible.
	writeFactLog(t, root, "", strings.Replace(quotesFacts,
		"- id: c-dropped\n  claim: The survey says \"THAT LIES WESTERLY OF THE CENTERLINE OF SAID RIGHT-OF-WAY\"\n  status: asserted\n",
		"- id: c-dropped\n  claim: The survey says \"THAT LIES WESTERLY OF THE CENTERLINE OF SAID RIGHT-OF-WAY\"\n  status: withdrawn\n", 1))
	if got := checksBy(t, root); got["c-dropped"] != "" {
		t.Errorf("a withdrawn claim was still checked, got %q", got["c-dropped"])
	}
}

// AbsentQuotes is what ranks the attestation queue, keyed on the edge.
func TestAbsentQuotesKeysOnTheFactSourcePair(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	absent := p.Graph.AbsentQuotes(root)
	if !absent["c-dropped\x00s-survey"] {
		t.Error("the dropped-passage citation is not marked for a human to look at first")
	}
	if absent["c-wrapped\x00s-deed"] {
		t.Error("a correct citation was marked as needing attention")
	}
}

// A located quote is what makes verification cheap. Without it "present in a
// 30-page scan" still costs ten minutes of searching, which is the reason the
// transcription carries page markers in the first place.
func TestLocateReportsPageAndLine(t *testing.T) {
	doc := "## Page 1\nalpha beta\n\n## Page 7\ngamma\ndelta epsilon\n"
	off := strings.Index(doc, "delta")
	page, line := locate(doc, off)
	if page != 7 {
		t.Errorf("page = %d, want 7 — the marker's own number, not a count of markers", page)
	}
	if line != 6 {
		t.Errorf("line = %d, want 6 — counted from the start of the file", line)
	}
}

// A transcription with no page markers still locates by line. Emails and plain
// text have no pages, and reporting page 0 is honest where inventing page 1
// would not be.
func TestLocateWithoutPageMarkers(t *testing.T) {
	doc := "first\nsecond\nthird\n"
	page, line := locate(doc, strings.Index(doc, "third"))
	if page != 0 {
		t.Errorf("page = %d, want 0 — this document has no pages", page)
	}
	if line != 3 {
		t.Errorf("line = %d, want 3", line)
	}
}

// The index must agree with the fold byte for byte. The quote is folded by one
// function and the document by the other, so a divergence is a match that
// silently never happens.
func TestFoldTextIdxAgreesWithFoldText(t *testing.T) {
	for _, s := range []string{
		"", "plain ascii text",
		"“smart quotes” and — dashes",
		"MiXeD Case 123 with\nnewlines\tand tabs",
		"accented: café naïve ÅNGSTRÖM",
		"THE EASTERLY 25 FEET OF THE FOLLOWING:",
	} {
		f, offs := foldTextIdx(s)
		if got := foldText(s); got != f {
			t.Errorf("foldText(%q) = %q, foldTextIdx = %q — they must not diverge", s, got, f)
		}
		if len(offs) != len(f) {
			t.Errorf("foldTextIdx(%q): %d offsets for %d folded bytes", s, len(offs), len(f))
		}
		for i, o := range offs {
			if o < 0 || o >= len(s) {
				t.Errorf("foldTextIdx(%q): offset[%d] = %d out of range", s, i, o)
			}
		}
	}
}

// The offset must point at the quoted words in the ORIGINAL, which is the whole
// contract: a hit in the folded text is useless unless it maps back.
func TestFoldTextIdxMapsBackToTheOriginal(t *testing.T) {
	orig := "Note 1 of the survey states:\n  “THAT LIES WESTERLY OF THE CENTERLINE”\nand nothing else.\n"
	f, offs := foldTextIdx(orig)
	k := strings.Index(f, foldText("THAT LIES WESTERLY"))
	if k < 0 {
		t.Fatal("folded quote not found in folded original")
	}
	off := offs[k]
	if !strings.HasPrefix(orig[off:], "THAT LIES WESTERLY") {
		t.Errorf("offset %d lands on %q, want the start of the quoted words", off, orig[off:min(off+18, len(orig))])
	}
	if _, line := locate(orig, off); line != 2 {
		t.Errorf("line = %d, want 2", line)
	}
}

// STAR PAGINATION IS NOT CONTENT, and treating it as content was a FALSE
// NEGATIVE — the expensive direction.
//
// A reported opinion carries the printed page boundary inside the sentence:
// "a broker or seller has a *177 duty to disclose all material facts". The fold
// keeps digits, so `*177` became `177` in the middle of the text and every quote
// spanning a page reported ABSENT with the words plainly there — sending a
// person to verify a citation that was correct. Found on the live corpus, where
// one of 15 absent quotes was this and 59 sources are reported opinions.
func TestAQuoteSpanningAPrintedPageBoundaryIsFound(t *testing.T) {
	root := newCorpus(t, "law")
	dir := filepath.Join(root, corpusDir("law"))
	const opinion = "The court held that in the sale of real estate, a broker or " +
		"seller has a *177 duty to disclose all material facts not reasonably " +
		"ascertainable to the buyer. See the cases cited.\n"
	if err := os.WriteFile(filepath.Join(dir, "opinion.md"), []byte(opinion), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, root, corpusDir("law"),
		"- id: a-case\n  document: A reported opinion\n  doc: opinion.md\n  class: authority\n"+
			"- id: c-duty\n  claim: The rule is that \"a broker or seller has a duty to "+
			"disclose all material facts\" in a sale\n  status: asserted\n  attested_by: a-case\n")

	p, _, err := LoadInWith(root, "law", nil)
	if err != nil {
		t.Fatal(err)
	}
	checks := p.Graph.CheckQuotes(root)
	if len(checks) == 0 {
		t.Fatal("no quote was checked — this test is checking nothing")
	}
	for _, c := range checks {
		if c.Fact == "c-duty" && c.State != "present" {
			t.Errorf("a quote spanning a printed page boundary reported %q — the words are "+
				"in the document and only a page marker sits inside them", c.State)
		}
	}

	// And the masking must not change any OFFSET: a present quote still has to
	// locate, or the fix trades a false absent for a useless hit.
	for _, c := range checks {
		if c.Fact == "c-duty" && c.State == "present" && c.Line == 0 {
			t.Error("the quote was found but not located — masking broke the offset mapping")
		}
	}
}

// AN ABSENT QUOTE IS LOOKED FOR ELSEWHERE BEFORE IT IS REPORTED.
//
// Measured on the live corpus: of 15 absent quotes, 4 cited the wrong document
// and the words were verbatim in a sibling file. The tool said only "absent",
// leaving a person to grep a gigabyte of evidence — a search the corpus can do
// itself, and the one part of that queue that was never a judgement call.
func TestAnAbsentQuoteNamesTheDocumentThatDoesContainIt(t *testing.T) {
	root := newCorpus(t, "matter")
	dir := filepath.Join(root, corpusDir("matter"))
	if err := os.WriteFile(filepath.Join(dir, "cited.md"),
		[]byte("This letter is about something else entirely.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sibling.md"),
		[]byte("We have replaced the legal from the title company with your documents.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, root, corpusDir("matter"),
		"- id: s-cited\n  document: The letter cited\n  doc: cited.md\n  class: document\n"+
			"- id: s-sibling\n  document: The letter next to it\n  doc: sibling.md\n  class: document\n"+
			"- id: c-legal\n  claim: The agent wrote \"we have replaced the legal from the "+
			"title company with your documents\" in the thread\n  status: asserted\n"+
			"  attested_by: s-cited\n")

	p, _, err := LoadInWith(root, "matter", nil)
	if err != nil {
		t.Fatal(err)
	}
	checks := p.Graph.CheckQuotes(root)
	var got *QuoteCheck
	for i := range checks {
		if checks[i].Fact == "c-legal" {
			got = &checks[i]
		}
	}
	if got == nil {
		t.Fatal("the quotation was not checked — this test is checking nothing")
	}
	if got.State != "absent" {
		t.Fatalf("the cited document does not contain the words, so this is %q not absent", got.State)
	}
	if got.FoundIn != "s-sibling" {
		t.Errorf("the words are in s-sibling and the tool did not say so: FoundIn=%q", got.FoundIn)
	}

	// A CANDIDATE, NOT A CORRECTION — the citation is untouched. Two instruments
	// can legitimately quote one passage, so only a person moves the citation.
	if got.Source != "s-cited" {
		t.Errorf("the reported citation moved on its own: %q", got.Source)
	}
}

// A PLEADING FOLLOWED BY EXHIBITS is the shape a whole-document ratio cannot see.
//
// A declaration is six numbered pages and thirty unnumbered ones of attachments.
// Judged over the file, the exhibits outvote the pleading — measured on a real
// filing, 97 numbered lines against 1635 — the ratio fails, nothing is stripped,
// and every quotation spanning a line break in the numbered part reports a near
// miss. Line numbering is a property of a PAGE.
func TestPleadingLineNumbersAreStrippedDespiteUnnumberedExhibits(t *testing.T) {
	var b strings.Builder
	// A numbered page, with a sentence wrapped across two of its lines.
	for i := 1; i <= 24; i++ {
		switch i {
		case 11:
			b.WriteString("11     17. Specifically, the area outlined in black on the survey and the area\n")
		case 12:
			b.WriteString("12 outlined in red did not accurately reflect the boundary.\n")
		default:
			b.WriteString(fmt.Sprintf("%d some line of the declaration body here\n", i))
		}
	}
	// ...and a long unnumbered exhibit after it.
	for i := 0; i < 300; i++ {
		b.WriteString("an exhibit line carrying no margin numbering at all\n")
	}
	got := stripPleadingLineNumbers(b.String())
	if !strings.Contains(foldText(got), foldText("the area outlined in red did not accurately")) {
		t.Errorf("a sentence wrapped across two numbered lines did not survive stripping")
	}
	if strings.Contains(got, "\n12 outlined") {
		t.Errorf("the margin number is still in the text")
	}
	// The exhibit is untouched — nothing there looks like a numbered page.
	if !strings.Contains(got, "an exhibit line carrying no margin numbering at all") {
		t.Errorf("the unnumbered exhibit was altered")
	}
}

// NON-BREAKING SPACE is what a layout-aware VLM indents a numbered line with,
// and U+00A0 is not in Go's `\s`. An ASCII-only class matched only the
// continuation lines, leaving every paragraph-opening number to land
// mid-sentence once the page was folded.
func TestPleadingLineNumbersSurviveNonBreakingIndent(t *testing.T) {
	if !lineNo.MatchString("1      11. The First Dace Survey has a note") {
		t.Errorf("a line indented with U+00A0 was not recognised as numbered")
	}
}

// A RECITED PASSAGE IS NOT A MISCITATION, AND THE COUNT IS WHAT SAYS SO.
//
// `findQuoted` returned the first match in sorted id order and the CLI printed
// "the words ARE in <x> — cite that, or say why not". When a passage is recited
// — and in a boundary dispute recitation is the normal state of the material,
// one plat certification's language measured in 22 of the index's documents —
// that names the alphabetically first of many and reads as a correction.
func TestFindQuotedCountsEveryHomeNotJustTheFirst(t *testing.T) {
	root := t.TempDir()
	const recited = "that lies westerly of the centerline of said right of way"
	const unique = "the inlet baffle was replaced on a saturday"
	for name, body := range map[string]string{
		"deed.txt":       "conveying the land " + recited + " to the grantee",
		"survey.txt":     "the survey shows land " + recited + " as parcel a",
		"commitment.txt": "schedule b excepts the strip " + recited + " from coverage",
		"invoice.txt":    "note: " + unique + " and the lid was reset",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := &Graph{Nodes: map[string]*Node{}}
	for _, n := range []struct{ id, doc string }{
		{"s-deed", "deed.txt"}, {"s-survey", "survey.txt"},
		{"s-commitment", "commitment.txt"}, {"s-invoice", "invoice.txt"},
	} {
		g.Nodes[n.id] = &Node{ID: n.id, Kind: KSource, Source: &Source{DocPath: n.doc, Class: "record"}}
	}
	cache := map[string]foldedDoc{}
	read := func(rel string) foldedDoc {
		if d, ok := cache[rel]; ok {
			return d
		}
		d := foldedDoc{body: foldText(docText(root, rel))}
		cache[rel] = d
		return d
	}

	_, _, n := g.findQuoted(root, foldText(recited), read, nil)
	if n != 3 {
		t.Errorf("a passage in three documents counted %d — the count is what tells a "+
			"person containment cannot decide this", n)
	}
	id, _, n := g.findQuoted(root, foldText(unique), read, nil)
	if n != 1 || id != "s-invoice" {
		t.Errorf("a passage with one home reported id=%q count=%d, want s-invoice/1", id, n)
	}
	// Nothing anywhere is zero, not a stray first match.
	if id, _, n := g.findQuoted(root, foldText("no document says this at all"), read, nil); n != 0 || id != "" {
		t.Errorf("an absent passage reported id=%q count=%d", id, n)
	}
}
