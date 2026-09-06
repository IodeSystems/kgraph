package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A QUOTE CAN BE WRONG WITHOUT BEING INVENTED, and `absent` was reporting both.
//
// Measured on the live corpus: of 14 absent quotations, 5 differ from their
// document by a word or two — `impute` where the opinion says `imputes`, `save`
// where a transcript says `saved` — and 9 are genuinely not there. Opposite
// findings with opposite fixes: correct the quotation, versus find out what the
// document really says. Reported identically, both sent a person to read a
// thirty-page scan.
func TestANearlyRightQuotationIsNearNotAbsent(t *testing.T) {
	root := newCorpus(t, "matter")
	dir := filepath.Join(root, corpusDir("matter"))
	const opinion = "The court will not impute knowledge as to her contract with the " +
		"seller; it has no bearing on her contract with the title company at all.\n"
	if err := os.WriteFile(filepath.Join(dir, "opinion.md"), []byte(opinion), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"),
		[]byte("An unrelated letter about a fence and a survey.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, root, corpusDir("matter"),
		"- id: a-case\n  document: A reported opinion\n  doc: opinion.md\n  class: authority\n"+
			"- id: s-other\n  document: An unrelated letter\n  doc: other.md\n  class: document\n"+
			// One WORD out: "imputes" for "impute".
			"- id: c-near\n  claim: The opinion says it will not \"imputes knowledge as to her "+
			"contract with the seller it has no bearing on her contract\"\n  status: asserted\n"+
			"  attested_by: a-case\n"+
			// Nothing like it: genuinely absent.
			"- id: c-absent\n  claim: The opinion says \"the buyer waived every objection to "+
			"the boundary and accepted the fence as built\"\n  status: asserted\n"+
			"  attested_by: a-case\n")

	p, _, err := LoadInWith(root, "matter", nil)
	if err != nil {
		t.Fatal(err)
	}
	byFact := map[string]QuoteCheck{}
	for _, c := range p.Graph.CheckQuotes(root) {
		byFact[c.Fact] = c
	}
	if len(byFact) == 0 {
		t.Fatal("no quotation was checked — this test is checking nothing")
	}

	near, ok := byFact["c-near"]
	if !ok {
		t.Fatal("the near-miss quotation was not checked")
	}
	if near.State != "near" {
		t.Errorf("a quotation one word out reported %q — the fix is to correct the "+
			"quotation, and %q sends a person to read the document instead",
			near.State, near.State)
	}
	if near.Near == "" {
		t.Error("near reported without the passage it nearly matches, which is the whole point")
	}
	// The difference must be SHOWN, or this is still a document-reading task.
	if d := QuoteDiff(near.Quote, near.Near); !strings.Contains(d, "imputes") {
		t.Errorf("the difference does not name the word that differs: %q", d)
	}

	// AND THE GENUINELY ABSENT ONE MUST STAY ABSENT. A threshold loose enough to
	// call this "near" would make the state meaningless — every quotation shares
	// "the" and "her" with every document.
	if gone, ok := byFact["c-absent"]; !ok {
		t.Fatal("the absent quotation was not checked")
	} else if gone.State != "absent" {
		t.Errorf("a quotation the document does not contain reported %q (ratio %.2f)",
			gone.State, gone.NearRatio)
	}
}

// The measure is ORDER-SENSITIVE. A bag of words would call a paraphrase built
// from the same vocabulary an exact hit, and word order is most of what makes a
// quotation a quotation.
func TestTheNearMeasureIsOrderSensitive(t *testing.T) {
	a := []string{"the", "gate", "was", "open", "on", "the", "morning", "of", "the", "sale"}
	b := []string{"the", "sale", "of", "the", "morning", "on", "open", "was", "gate", "the"}
	if r := wordRatio(a, b); r >= nearThreshold {
		t.Errorf("the same words in reverse scored %.2f — that is a bag of words, not a quotation", r)
	}
	if r := wordRatio(a, a); r != 1 {
		t.Errorf("a passage against itself scored %.2f", r)
	}
}
