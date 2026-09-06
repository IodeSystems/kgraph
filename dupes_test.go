package kgraph

import (
	"os"
	"path/filepath"
	"testing"
)

// FIFTY POSSIBLE DUPLICATES IS NOT FIFTY PROBLEMS.
//
// The near-duplicate checker's own overflow message has said so since it was
// written — "a corpus with this many is better fixed by pattern than one at a
// time" — and there was no surface for doing that. Clustering is what turns a
// list of pairs into a list of decisions.
func TestDuplicatesClusterTransitivelyAndRankCertainOnesFirst(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"order.pdf", "a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFactLog(t, root, "",
		// Two sources on ONE file: the unambiguous case, and no judgement needed.
		"- id: s-order-one\n  document: The order\n  doc: order.pdf\n  class: document\n"+
			"- id: s-order-two\n  document: The order, again\n  doc: order.pdf\n  class: document\n"+
			// Three NEAR-identical descriptions across three files. Identical text is a
			// different check and a hard error — exact duplicates get fixed, not
			// triaged — so these differ by a word, which is what the near-duplicate
			// checker is for.
			"- id: s-survey-a\n  document: The 2008 boundary survey of lot seven north\n  doc: a.md\n  class: record\n"+
			"- id: s-survey-b\n  document: The 2008 boundary survey of lot seven south\n  doc: b.md\n  class: record\n"+
			"- id: s-survey-c\n  document: The 2008 boundary survey of lot seven west\n  doc: c.md\n  class: record\n"+
			"- id: c-one\n  claim: Rests on the order\n  status: asserted\n  attested_by: s-order-one\n")

	g, _, err := ScanFactsIn(root, DefaultIndex)
	if err != nil {
		t.Fatal(err)
	}
	cs := g.DuplicateClusters()
	if len(cs) == 0 {
		t.Fatal("no clusters — this test is checking nothing")
	}

	// THE CERTAIN ONE COMES FIRST. Same-file duplicates were found by a different
	// check in a different place, so the cheapest decisions in a corpus were
	// scattered through the hardest ones.
	if !cs[0].SameFile {
		t.Errorf("the unambiguous same-file cluster must rank first, got %+v", cs[0])
	}
	if cs[0].Doc != "order.pdf" || len(cs[0].IDs) != 2 {
		t.Errorf("the same-file cluster is wrong: %+v", cs[0])
	}
	// It suggests keeping the CITED one, because that is the id references
	// already resolve through.
	if cs[0].Keep != "s-order-one" {
		t.Errorf("want the cited member kept, got %q", cs[0].Keep)
	}

	// TRANSITIVE. Three entries for one survey are one decision, not three.
	var survey *DupCluster
	for i := range cs {
		if containsString(cs[i].IDs, "s-survey-a") {
			survey = &cs[i]
		}
	}
	if survey == nil {
		t.Fatal("the near-duplicate survey entries did not cluster")
	}
	if len(survey.IDs) != 3 {
		t.Errorf("three entries for one thing became %d cluster member(s): %v",
			len(survey.IDs), survey.IDs)
	}
}

// SAME FILE IS NOT SAME INSTRUMENT, and calling it one gave dangerous advice.
//
// A court filing is a declaration WITH ITS EXHIBITS; an e-sign envelope holds
// several forms; a scan holds a contract and the certification stapled to it.
// Measured on the live corpus: of seven same-file clusters, FIVE were compound
// documents and two were duplicates — and the tool was printing `kg retire` for
// all seven. Following it would have merged a 1947 record into a 2023
// declaration.
//
// Two entries for one instrument agree about when it is and what it is. A filing
// and the exhibit attached to it cannot.
func TestSeveralInstrumentsInOneDocumentAreNotADuplicate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "matter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, IndexMarker), []byte("index: matter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"filing.pdf", "register.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFactLog(t, root, "projects/matter",
		// A declaration attaching a much older record: different date AND class.
		"- id: s-decl\n  utterance: The 2023 declaration\n  doc: filing.pdf\n"+
			"  class: interested\n  at: 2023-02-10\n  by: e-witness\n"+
			"- id: s-exhibit\n  record: The 1947 plat attached to it\n  doc: filing.pdf\n"+
			"  class: record\n  at: 1947-01-02\n"+
			"- id: e-witness\n  entity: A witness\n  status: asserted\n"+
			// One document entered twice: same class, one simply undated.
			"- id: s-reg-a\n  record: Register of actions\n  doc: register.md\n"+
			"  class: record\n  at: 2026-07-13\n"+
			"- id: s-reg-b\n  record: Docket register\n  doc: register.md\n  class: record\n")

	g, _, err := ScanFactsIn(root, "matter")
	if err != nil {
		t.Fatal(err)
	}
	byMember := map[string]DupCluster{}
	for _, c := range g.DuplicateClusters() {
		for _, id := range c.IDs {
			byMember[id] = c
		}
	}
	decl, ok := byMember["s-decl"]
	if !ok {
		t.Fatal("the filing's entries did not cluster — this test is checking nothing")
	}
	if !decl.Compound {
		t.Error("a declaration and the 1947 exhibit attached to it were called a duplicate — " +
			"merging them would erase the exhibit")
	}
	if decl.Why == "" {
		t.Error("compound reported without saying what disagrees, so a reader must trust it")
	}

	// AND A REAL DUPLICATE MUST STILL BE ONE. An entry with no date is a MISSING
	// value, not a second instrument — treating empty as a disagreement marked
	// the live corpus's only genuine duplicate as compound.
	reg, ok := byMember["s-reg-a"]
	if !ok {
		t.Fatal("the register entries did not cluster")
	}
	if reg.Compound {
		t.Errorf("one dated entry and one undated were called separate instruments (%q) — "+
			"absence is not disagreement", reg.Why)
	}
	if reg.Keep == "" {
		t.Error("a genuine duplicate should still suggest which to keep")
	}
}
