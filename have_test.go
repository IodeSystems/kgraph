package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// haveProject builds the shape that produced the failure this file exists for:
// a document whose FILENAME lies about what it is, a document held but declared
// by nothing, and a source that cites a document nobody has.
func haveProject(t *testing.T) string {
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
	mk("documents/records/8801200011-1984-deed.pdf", "%PDF-1.4 binary")
	// The misfiled one. Its name says Form 34; its content says Form 35R.
	mk("documents/general-addendum-form-34.pdf", "%PDF-1.4 binary")
	mk("documents/general-addendum-form-34.ocr.md",
		"## Page 1\nForm 35R Inspection Response\n(C)Copyright 2021 Northwest MLS\n")
	// A document that merely CITES an instrument. It must never outrank the
	// instrument itself.
	mk("documents/letter.pdf", "%PDF-1.4 binary")
	mk("documents/letter.ocr.md",
		"## Page 1\nAs shown on the survey recorded under AF#201503110043, the strip...\n")
	mk("documents/records/201503110043-survey.pdf", "%PDF-1.4 binary")
	// An instrument nobody names by number. A client describes it — "the old
	// railroad right of way" — and the recorded deed writes the same thing with
	// different hyphens, which is exactly the pair the term tier exists to join.
	mk("documents/records/9404150061-quitclaim.pdf", "%PDF-1.4 binary")
	mk("documents/records/9404150061-quitclaim.ocr.md",
		"## Page 1\nTHE EASTERLY 25 FEET OF THE FOLLOWING:\n"+
			"That portion of the 100 foot wide railroad right-of-way commonly known as\n"+
			"the Northern Pacific Railway, as conveyed to the Seattle Lake Shore company.\n")
	// A stronger term match, filed LAST by path so ordering cannot pass by luck.
	mk("documents/zzz-surveyor-letter.pdf", "%PDF-1.4 binary")
	mk("documents/zzz-surveyor-letter.ocr.md",
		"## Page 1\nThe 25 foot strip of the old railroad right of way runs behind the driveway.\n")

	writeFactLog(t, root, "", ""+
		"- id: s-deed\n  record: Cartwright deed\n  doc: documents/records/8801200011-1984-deed.pdf\n  class: record\n"+
		"  aliases:\n    - as: AF 8801200011\n"+
		"- id: s-survey\n  record: 2008 Crestline survey 201503110043\n  doc: documents/records/201503110043-survey.pdf\n  class: record\n"+
		// Declared, but the document is genuinely absent. Reporting this as held
		// would tell someone to stop looking for a document nobody has.
		"- id: s-policy\n  record: Title policy\n  doc: documents/never-arrived.pdf\n  class: record\n"+
		"  aliases:\n    - as: PL77-1234\n"+
		"- id: c-one\n  claim: something\n  status: asserted\n  attested_by: s-deed\n"+
		"- id: q-get-the-deed\n  question: Obtain AF 8801200011 from the auditor\n  status: open\n  needs: evidence\n"+
		"- id: q-get-the-policy\n  question: Obtain PL77-1234 from the county\n  status: open\n  needs: evidence\n")
	return root
}

func haveIn(t *testing.T, root, q string) []Held {
	t.Helper()
	p := loadProject(t, root)
	h, err := Have(root, p.Index, p.Graph, q)
	if err != nil {
		t.Fatalf("Have(%q): %v", q, err)
	}
	return h
}

// A declared alias is the authoritative answer and must come first.
func TestHaveFindsByAlias(t *testing.T) {
	root := haveProject(t)
	got := haveIn(t, root, "AF 8801200011")
	if len(got) == 0 {
		t.Fatal("an aliased document was reported as not held")
	}
	if got[0].Via != "alias" || got[0].Source != "s-deed" {
		t.Errorf("want the alias match first, got via=%q source=%q", got[0].Via, got[0].Source)
	}
}

// The registry prefix is optional in the written form. `AF 201503110043` has to
// find a source titled `...survey 201503110043`, which carries no prefix — a
// bare-number filename is how most of this corpus is named.
func TestHaveMatchesWithAndWithoutRegistryPrefix(t *testing.T) {
	root := haveProject(t)
	for _, q := range []string{"AF 201503110043", "201503110043", "AF#201503110043"} {
		got := haveIn(t, root, q)
		if len(got) == 0 {
			t.Fatalf("%q found nothing", q)
		}
		if got[0].Doc != "documents/records/201503110043-survey.pdf" {
			t.Errorf("%q: want the survey first, got %q via %q", q, got[0].Doc, got[0].Via)
		}
	}
}

// The whole point of reading transcripts: a document whose name is wrong is
// still findable, because the content cannot be misfiled.
func TestHaveFindsAMisfiledDocumentByItsContent(t *testing.T) {
	root := haveProject(t)
	got := haveIn(t, root, "Form 35R")
	var found *Held
	for i := range got {
		if got[i].Doc == "documents/general-addendum-form-34.pdf" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatal("a document misfiled under another name was not found by its content")
	}
	if found.Via != "text" {
		t.Errorf("want a text match, got %q", found.Via)
	}
	if found.Declared {
		t.Error("nothing declares this document; saying otherwise hides that it needs a source")
	}
}

// A document that merely cites an instrument is not that instrument. This is
// the regression that ranked a purchase and sale agreement above the recorded
// survey it quoted.
func TestHaveRanksTheInstrumentAboveDocumentsCitingIt(t *testing.T) {
	root := haveProject(t)
	got := haveIn(t, root, "AF 201503110043")
	if got[0].Doc != "documents/records/201503110043-survey.pdf" {
		t.Fatalf("the instrument must outrank a document quoting it, got %q", got[0].Doc)
	}
	// The citing letter is still worth reporting, just not first.
	var sawLetter bool
	for _, h := range got {
		if h.Doc == "documents/letter.pdf" {
			sawLetter = true
			if h.Via != "text" {
				t.Errorf("a citing document should match on text, got %q", h.Via)
			}
		}
	}
	if !sawLetter {
		t.Error("a document discussing the instrument should still be listed")
	}
}

// THE TERM TIER, and the case that produced it. A client's account describes an
// instrument instead of naming it, and until this every tier matched only
// identifiers — so the deed the passage was about was unfindable from the
// passage.
//
// The hyphens are the point. The passage writes "25-foot" and "right of way";
// the recorded deed writes "100 foot" and "right-of-way". Joining punctuation
// out (which is right for `AF#201503110042`) turns those into `25foot` and
// `rightofway`, two tokens that cannot meet, and the real 1993 quitclaim scored
// one hit out of six terms against the sentence describing it.
func TestHaveFindsAnInstrumentThePassageOnlyDescribes(t *testing.T) {
	root := haveProject(t)
	got := haveIn(t, root, "The 25-foot strip the driveway sits on is the old railroad right of way.")
	var found bool
	for _, h := range got {
		if h.Doc == "documents/records/9404150061-quitclaim.pdf" {
			found = true
			if h.Via != "terms" {
				t.Errorf("a described instrument matches on terms, got %q", h.Via)
			}
			if h.Excerpt == "" {
				t.Error("a term match with no excerpt is unreadable")
			}
		}
		// The letter shares one content word ("strip") and nothing else. One
		// shared word is every document in a corpus.
		if h.Doc == "documents/letter.pdf" && h.Via == "terms" {
			t.Errorf("a single shared word was treated as a match: %q", h.Excerpt)
		}
	}
	if !found {
		t.Error("the instrument the passage describes was not found")
	}
}

// A search ordered by file path is a list somebody has to read all of.
func TestTermMatchesAreOrderedByStrength(t *testing.T) {
	root := haveProject(t)
	var terms []Held
	for _, h := range haveIn(t, root, "The 25-foot strip the driveway sits on is the old railroad right of way.") {
		if h.Via == "terms" {
			terms = append(terms, h)
		}
	}
	if len(terms) < 2 {
		t.Fatalf("expected at least two term matches, got %d", len(terms))
	}
	// Filed last by path, strongest by score.
	if terms[0].Doc != "documents/zzz-surveyor-letter.pdf" {
		t.Errorf("the strongest match must come first, got %q (%d terms)", terms[0].Doc, terms[0].Terms)
	}
	for i := 1; i < len(terms); i++ {
		if terms[i].Terms > terms[i-1].Terms {
			t.Errorf("term matches are out of order: %d before %d", terms[i-1].Terms, terms[i].Terms)
		}
	}
}

// A query that names an identifier and nothing else must run no term search.
//
// Its digits are not content: `PL 99-0479` splits to 99 and 0479, and searching
// those as words returns every line carrying both numbers — noise on a query the
// identifier tier already answered exactly. The first version of this scored
// zero terms against a threshold of zero, so `0 >= 0` reported EVERY transcript
// in the corpus as a term match with an empty excerpt.
func TestABareIdentifierQueryRunsNoTermSearch(t *testing.T) {
	root := haveProject(t)
	for _, h := range haveIn(t, root, "AF 8801200011") {
		if h.Via == "terms" {
			t.Errorf("an identifier query produced a term match: %s (%q)", h.Doc, h.Excerpt)
		}
	}
}

// ONE CONTENT WORD IS NOT A SUBJECT, and this was learned by watching it. A
// caret sitting in a markdown heading sent "# Bramble v." through the search; the
// only content word was a party name, and the panel filled with every transcript
// in the matter that mentions the client — presented as evidence for a heading.
func TestASingleContentWordRunsNoTermSearch(t *testing.T) {
	root := haveProject(t)
	// "Hollow" appears in the plat sheet; on its own it must not reach it.
	for _, q := range []string{"Hollow", "# Hollow —", "the plat"} {
		for _, h := range haveIn(t, root, q) {
			if h.Via == "terms" {
				t.Errorf("%q matched %s on terms (%q)", q, h.Doc, h.Excerpt)
			}
		}
	}
}

// A term match is NOT possession, and this is the tier's whole risk. A document
// that discusses a deed is not the deed, so treating a term hit as a holding
// closes an open question over a document nobody has.
//
// Asserted against StrongestOnDisk rather than through CheckAlreadyHeld,
// because CheckAlreadyHeld only ever queries identifiers it scraped out of the
// question — a describing question never reaches Have at all, so a test driven
// from there would pass without the rule existing.
func TestATermMatchIsNotPossession(t *testing.T) {
	root := haveProject(t)
	held := haveIn(t, root, "The 25-foot strip the driveway sits on is the old railroad right of way.")
	var any bool
	for _, h := range held {
		if h.Via == "terms" {
			any = true
		} else {
			t.Fatalf("fixture no longer isolates the term tier: %s matched on %s", h.Doc, h.Via)
		}
	}
	if !any {
		t.Fatal("no term matches, so this proves nothing")
	}
	if h, ok := StrongestOnDisk(held, true); ok {
		t.Errorf("a described-only match was reported as held: %s (%s)", h.Doc, h.Via)
	}
}

// Content is matched as whole identifiers, never as substrings. A digit run
// buried in an unrelated number is not a citation.
func TestHaveDoesNotSubstringMatchDocumentContent(t *testing.T) {
	root := haveProject(t)
	abs := filepath.Join(root, "documents/letter.ocr.md")
	if err := os.WriteFile(abs, []byte("## Page 1\nAccount 99888012000111234 was debited.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, h := range haveIn(t, root, "AF 8801200011") {
		if h.Doc == "documents/letter.pdf" {
			t.Error("a digit run inside a longer number was matched as an identifier")
		}
	}
}

// A source with no document on disk is NOT held. Reporting it as held told
// someone to close a question asking for a document that is genuinely missing,
// which destroys evidence rather than finding it.
func TestCheckAlreadyHeldIgnoresSourcesWithNoDocumentOnDisk(t *testing.T) {
	root := haveProject(t)
	p := loadProject(t, root)
	for _, d := range p.Graph.CheckAlreadyHeld(root, p.Index) {
		if strings.Contains(d.Msg, "q-get-the-policy") {
			t.Errorf("a cited-but-absent document was reported as held: %s", d.Msg)
		}
		if strings.Contains(d.Msg, "HELD at  ") || strings.HasSuffix(d.Msg, "HELD at ") {
			t.Errorf("reported a hold with no path: %s", d.Msg)
		}
	}
}

// The check that would have caught the original miss.
func TestCheckAlreadyHeldCatchesAQuestionAskingForAHeldDocument(t *testing.T) {
	root := haveProject(t)
	p := loadProject(t, root)
	var hit bool
	for _, d := range p.Graph.CheckAlreadyHeld(root, p.Index) {
		if strings.Contains(d.Msg, "q-get-the-deed") && strings.Contains(d.Msg, "ALREADY HELD") {
			hit = true
			if d.Severity != SevWarn {
				t.Errorf("a heuristic must warn, never error — got %v", d.Severity)
			}
		}
	}
	if !hit {
		t.Error("a question asking for a document already on disk was not reported")
	}
}

func TestAddAliasRecordsAndIsFoundByIt(t *testing.T) {
	root := haveProject(t)
	p := loadProject(t, root)
	st := storeFor(t, root)
	if err := AddAliasTo(st, legal, p.Graph, "s-survey", "the 2008 Crestline survey", "carl"); err != nil {
		t.Fatal(err)
	}
	if err := exportStore(t, st, root, ""); err != nil {
		t.Fatal(err)
	}
	got := haveIn(t, root, "Crestline survey")
	if len(got) == 0 || got[0].Source != "s-survey" {
		t.Fatalf("an alias just recorded did not resolve, got %+v", got)
	}
	if got[0].Via != "alias" {
		t.Errorf("want via=alias, got %q", got[0].Via)
	}
}

// An alias that already denotes something else is the ambiguity the alias system
// exists to surface. Creating it silently would make every reference unresolvable.
func TestAddAliasRefusesToCollideWithAnotherNode(t *testing.T) {
	root := haveProject(t)
	p := loadProject(t, root)
	if err := AddAliasTo(storeFor(t, root), legal, p.Graph, "s-survey", "AF 8801200011", "carl"); err == nil {
		t.Fatal("an alias already denoting another node was accepted")
	}
}

func TestAddAliasRefusesADuplicate(t *testing.T) {
	root := haveProject(t)
	p := loadProject(t, root)
	if err := AddAliasTo(storeFor(t, root), legal, p.Graph, "s-deed", "AF#8801200011", "carl"); err == nil {
		t.Fatal("a duplicate alias, differing only in punctuation, was appended")
	}
}

// ADDING AN ALIAS MUST NOT DISTURB ANYTHING ELSE.
//
// This replaces two tests about markdown mechanics — that the edit added exactly
// two lines, that every original line survived verbatim, that the alias landed
// inside the block declaring its source rather than a later one. All of that was
// real and load-bearing while the text was authoritative and hand-written,
// because a tool that reflows a file makes its own diffs unreviewable.
//
// The store makes those properties FREE: an assert is a new line, so nothing
// else can move. What is left worth testing is the invariant they were
// protecting — every other fact means exactly what it meant — and `SemHash` says
// that far more precisely than a line count did.
func TestAddingAnAliasChangesNothingElse(t *testing.T) {
	root := haveProject(t)
	p := loadProject(t, root)
	before := map[string]string{}
	for id, h := range p.Graph.SemHash {
		before[id] = h
	}

	st := storeFor(t, root)
	if err := AddAliasTo(st, legal, p.Graph, "s-survey", "the operative survey", "carl"); err != nil {
		t.Fatal(err)
	}
	if err := exportStore(t, st, root, ""); err != nil {
		t.Fatal(err)
	}

	after := loadProject(t, root)
	for id, want := range before {
		got, ok := after.Graph.SemHash[id]
		if !ok {
			t.Errorf("%s vanished", id)
			continue
		}
		if id == "s-survey" {
			// The aliased node MUST change: an alias is content and reaches the hash.
			if got == want {
				t.Error("the alias did not change the node it was added to")
			}
			continue
		}
		if got != want {
			t.Errorf("%s changed meaning when an unrelated source was aliased", id)
		}
	}
}

// A document that is ALREADY TEXT is its own readable form.
//
// The corpus keeps a court recording beside a `.verified.md` transcript, and no
// entry in transcriptSuffixes names that ending — so the content tier found no
// companion and skipped both files, and four hearings' worth of sworn testimony
// could not be found by content. The same gap hid every markdown and text
// exhibit in the corpus, whose words are on disk and need no transcription.
func TestHaveReadsATextDocumentWithNoCompanion(t *testing.T) {
	root := haveProject(t)
	mk := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The recording is the instrument; the verified transcript is its text.
	mk("documents/hearings/return-hearing.mp4", "\x00\x00\x00 binary")
	mk("documents/hearings/return-hearing.verified.md",
		"**Clerk** *(0:00)*\n\nSince you are a witness, not a party, "+
			"please take a seat in the gallery.\n")

	held := haveIn(t, root, "a witness, not a party, take a seat in the gallery")
	var found bool
	for _, h := range held {
		if strings.HasSuffix(h.Doc, "return-hearing.verified.md") {
			found = true
			if h.Excerpt == "" {
				t.Error("matched the transcript but reported no excerpt")
			}
		}
	}
	if !found {
		t.Fatalf("the verified transcript was not found by its own content; got %d hit(s): %+v", len(held), held)
	}
}

// An analysis is OUR reading of an instrument, never evidence of it. Reading
// text documents directly must not turn the corpus's own commentary into a hit.
func TestHaveDoesNotReturnAnAnalysisAsEvidence(t *testing.T) {
	root := haveProject(t)
	abs := filepath.Join(root, "documents/records/9404150061-quitclaim-analysis.md")
	if err := os.WriteFile(abs, []byte(
		"The easterly 25 feet of the 100 foot wide railroad right of way is the strip.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, h := range haveIn(t, root, "the easterly 25 feet of the old railroad right of way") {
		if strings.HasSuffix(h.Doc, analysisSuffix) {
			t.Errorf("an analysis was returned as evidence: %s", h.Doc)
		}
	}
}

// RAGLIT IS CONFIGURED PER PROJECT, and looking only at the corpus root is why
// every quotation in a live corpus read `unreadable`.
//
// The config sits at `<matter>/.raglit/config.json` and names the project,
// because that is the directory raglit indexes. kgraph is run from the corpus
// ROOT, found no `.raglit` there, returned an empty index name, and every text
// lookup short-circuited — silently, because "no raglit" is a legitimate state
// that degrades to "not loaded" rather than to an error.
//
// Measured on the live corpus: 192 of 303 quotations read `unreadable` under
// that, which looks exactly like evidence that was never transcribed. The
// transcriptions existed; nothing was asking for them. After: 76.
func TestTheRaglitConfigIsFoundInAProjectNotOnlyAtTheRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "a-matter", ".raglit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"project":"a-matter","default_index":"default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := raglitIndexName(root); got != "a-matter__default" {
		t.Errorf("index name from a project config = %q, want a-matter__default — "+
			"an empty name silently disables every text lookup", got)
	}

	// THE ROOT STILL WINS when it has one: a corpus that configures raglit at its
	// own root is not a corpus of matters, and must not have a subdirectory's
	// config chosen for it.
	rdir := filepath.Join(root, ".raglit")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "config.json"),
		[]byte(`{"project":"the-corpus","default_index":"default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := raglitIndexName(root); got != "the-corpus__default" {
		t.Errorf("the root's own config must win, got %q", got)
	}

	// And no config anywhere stays empty — raglit being absent is legitimate.
	if got := raglitIndexName(t.TempDir()); got != "" {
		t.Errorf("a corpus with no raglit config returned %q", got)
	}
}

// RAGLIT NOT HOLDING A DOCUMENT IS NOT THE DOCUMENT HAVING NO TEXT.
//
// `docText` asked raglit and returned empty when it had nothing, with the
// read-from-disk fallback sitting in the branch taken only when there was NO
// index name at all. That was invisible for as long as the name never resolved:
// every document fell through to the fallback, which did all the work. The
// moment the name started resolving, the fallback became unreachable and 76
// quotations lost their verification — case law kept as markdown, mostly, which
// a corpus's `include` patterns need not cover.
func TestADocumentRaglitDoesNotHoldIsStillReadFromDisk(t *testing.T) {
	root := t.TempDir()
	// A resolving raglit config, pointing at an index that holds nothing here.
	rdir := filepath.Join(root, ".raglit")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "config.json"),
		[]byte(`{"project":"p","default_index":"default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if raglitIndexName(root) == "" {
		t.Fatal("the fixture's config does not resolve — this test is checking nothing")
	}
	const body = "In the sale of real estate a broker has a duty to disclose material facts.\n"
	if err := os.WriteFile(filepath.Join(root, "opinion.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := docText(root, "opinion.md"); !strings.Contains(got, "duty to disclose") {
		t.Errorf("a text document raglit does not index was unreadable: %q", got)
	}
	// A binary the corpus cannot read stays unreadable — the fallback reads TEXT,
	// and must not start handing bytes to the quote checker.
	if err := os.WriteFile(filepath.Join(root, "scan.pdf"), []byte("%PDF-1.4\x00\x01binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := docText(root, "scan.pdf"); got != "" {
		t.Errorf("a binary with no transcription returned text: %q", got[:20])
	}
}

// A DESCRIPTION FINDS A SOURCE, and until this only a reference could.
//
// Every node tier matched the query as a PHRASE or as an identifier, so
// "the Cartwright deed" is a substring of the body "Cartwright deed" and finds
// it, while "Do you have the Cartwright deed?" is a substring of nothing and
// found it at no tier at all. The terms machinery already existed for exactly
// this case — a client's account DESCRIBES an instrument rather than naming it —
// and reached only documents on disk.
func TestADescribedSourceIsFoundByItsTerms(t *testing.T) {
	root := haveProject(t)

	// The reference form works and always did.
	var byPhrase bool
	for _, h := range haveIn(t, root, "Cartwright deed") {
		if h.Source == "s-deed" {
			byPhrase = true
		}
	}
	if !byPhrase {
		t.Fatal("the fixture no longer finds the source by its own words; this proves nothing")
	}

	// The question form is the one that found nothing.
	var byTerms *Held
	for _, h := range haveIn(t, root, "Do you have the Cartwright deed anywhere?") {
		if h.Source == "s-deed" {
			hit := h
			byTerms = &hit
		}
	}
	if byTerms == nil {
		t.Fatal("a source described rather than named is unfindable — which is the shape every " +
			"question a person actually asks takes")
	}
	if byTerms.Via != "terms" {
		t.Errorf("found via %q; a description is a TERM match and must report as one, because "+
			"the tier is what tells a reader how much the match is worth", byTerms.Via)
	}
	if byTerms.Terms < 1 {
		t.Errorf("Terms is %d; the count is what ranks one description above another", byTerms.Terms)
	}
}

// AND A DESCRIPTION IS STILL NOT POSSESSION.
//
// This is the half that must not move. `StrongestOnDisk` excludes `terms`
// always — a shared vocabulary is not holding a document, and this is the one
// answer that, wrong, destroys evidence by closing the question that would have
// found it. Making a description FINDABLE is a lead; making it count as
// possession would be the failure that rule exists to prevent.
func TestADescribedSourceIsStillNotPossession(t *testing.T) {
	root := haveProject(t)
	held := haveIn(t, root, "Do you have the Cartwright deed anywhere?")
	if len(held) == 0 {
		t.Fatal("the description finds nothing at all; the other test covers that")
	}
	if h, ok := StrongestOnDisk(held, false); ok {
		t.Errorf("a description reported as possession, via %q on %s. A caller asking whether we "+
			"HOLD something would close the question that would have found it", h.Via, h.Source)
	}
}
