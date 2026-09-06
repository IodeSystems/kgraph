package kgraph

import (
	"strings"
	"testing"
)

const splitMsg = "one end of a correction"

// The regression this guards is a document arguing a point the corpus had
// already retired. `the-case` said a certification's stated acreage was too
// small to hold a disputed strip for a week after a second certification showed
// the figure to be a form default. Both facts were in the corpus; only the dead
// one was in the set, and nothing said so.
func TestSplitContradictionIsWarned(t *testing.T) {
	root := splitFixture(t, "live: claim[id=old]")
	ds := loadDiags(t, root)
	if !hasMsg(ds, splitMsg) {
		t.Fatalf("a set carrying one end of a contradiction must be warned about:\n%s", allMsgs(ds))
	}
	if hasSev(ds, SevError) {
		t.Error("this is a warning — a document may omit a correction deliberately")
	}
}

// Carrying both ends is the whole point and must be silent.
func TestBothEndsIsNotWarned(t *testing.T) {
	root := splitFixture(t, "live: claim")
	if ds := loadDiags(t, root); hasMsg(ds, splitMsg) {
		t.Errorf("a set carrying both ends must be silent:\n%s", allMsgs(ds))
	}
}

// A `withdrawn` fact carries its status into the prose under house rule, so a
// reader can already see it is dead. Sets exist whose whole job is to collect
// them — `action-queue` has one named "retired" — and warning there says the
// opposite of what is meant.
func TestWithdrawnEndIsNotWarned(t *testing.T) {
	root := splitFixtureStatus(t, "live: claim[id=old]", "withdrawn", "asserted")
	if ds := loadDiags(t, root); hasMsg(ds, splitMsg) {
		t.Errorf("a withdrawn fact in the set must not be warned about:\n%s", allMsgs(ds))
	}
}

// And nothing is owed at the other end either: a document cannot be faulted for
// omitting a correction the corpus has itself retracted.
func TestWithdrawnCorrectionIsNotWarned(t *testing.T) {
	root := splitFixtureStatus(t, "live: claim[id=old]", "asserted", "withdrawn")
	if ds := loadDiags(t, root); hasMsg(ds, splitMsg) {
		t.Errorf("a withdrawn correction must not be demanded:\n%s", allMsgs(ds))
	}
}

// `undercut_by` is one-way: B beats A. Carrying B without A is fine, because a
// defeater does not need its victim on the page.
func TestOneWayUndercutterAloneIsNotWarned(t *testing.T) {
	root := splitFixtureRel(t, "live: claim[id=new]", "undercut_by")
	if ds := loadDiags(t, root); hasMsg(ds, splitMsg) {
		t.Errorf("an undercutter alone must be silent:\n%s", allMsgs(ds))
	}
}

// The defeated end alone is the failure it exists to catch.
func TestOneWayDefeatedAloneIsWarned(t *testing.T) {
	root := splitFixtureRel(t, "live: claim[id=old]", "undercut_by")
	if ds := loadDiags(t, root); !hasMsg(ds, splitMsg) {
		t.Errorf("a defeated claim alone must be warned about:\n%s", allMsgs(ds))
	}
}

// `supersedes` is one-way the other way round: the replacement alone is the
// correction landing, and that is correct.
func TestSupersedingFactAloneIsNotWarned(t *testing.T) {
	root := splitFixtureRel(t, "live: claim[id=new]", "supersedes")
	if ds := loadDiags(t, root); hasMsg(ds, splitMsg) {
		t.Errorf("a superseding fact alone must be silent:\n%s", allMsgs(ds))
	}
}

func splitFixture(t *testing.T, query string) string {
	return splitFixtureStatus(t, query, "asserted", "asserted")
}

func splitFixtureRel(t *testing.T, query, rel string) string {
	t.Helper()
	return splitRepo(t, query, "asserted", "asserted", rel)
}

func splitFixtureStatus(t *testing.T, query, oldStatus, newStatus string) string {
	t.Helper()
	return splitRepo(t, query, oldStatus, newStatus, "contradicts")
}

// splitRepo builds a two-claim repo joined by rel, and one spec selecting query.
// `contradicts` and `supersedes` are authored on the NEW claim pointing at the
// old; `undercut_by` is authored on the OLD claim naming what beats it.
func splitRepo(t *testing.T, query, oldStatus, newStatus, rel string) string {
	t.Helper()
	root := t.TempDir()
	writeTemp(t, root, ".kg-index", "fence-dispute\n")
	oldRel, newRel := "", "  "+rel+": old\n"
	if rel == "undercut_by" {
		oldRel, newRel = "  undercut_by: new\n", ""
	}
	writeFactLog(t, root, "",
		"- id: old\n  claim: The stated acreage limits it\n  status: "+oldStatus+"\n"+oldRel+
			"- id: new\n  claim: The figure is a form default\n  status: "+newStatus+"\n"+newRel)
	writeDeclared(t, root, "", query)
	return root
}

// A source that attests a fact the set already carries is IN the document: the
// render puts it under the fact and the reference block cites it. Counting only
// what Eval returns reported `s-precipitation-research` missing from a document
// that quotes it seven times.
func TestAttestingSourceCountsAsPresent(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, ".kg-index", "fence-dispute\n")
	writeFactLog(t, root, "",
		"- id: s1\n  document: A research note\n  doc: n.md\n  class: document\n"+
			"- id: old\n  claim: No flood event is logged\n  status: asserted\n  undercut_by: s1\n"+
			"- id: other\n  claim: The causation position is a regional event\n  status: asserted\n  attested_by: s1\n")
	writeDeclared(t, root, "", "live: claim")
	if ds := loadDiags(t, root); hasMsg(ds, splitMsg) {
		t.Errorf("a source attesting an in-set fact must count as present:\n%s", allMsgs(ds))
	}
}

// Shared by the scan tests. These lived in scan_publish_test.go, which went with
// the check it covered: CheckUnpublishedSpecs, retired when the viewer stopped
// publishing documents.

func loadDiags(t *testing.T, root string) []Diag {
	t.Helper()
	_, ds, err := LoadIn(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func hasMsg(ds []Diag, sub string) bool {
	for _, d := range ds {
		if strings.Contains(d.Msg, sub) {
			return true
		}
	}
	return false
}

func hasSev(ds []Diag, s Severity) bool {
	for _, d := range ds {
		if d.Severity == s {
			return true
		}
	}
	return false
}
func allMsgs(ds []Diag) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(d.File + ": " + d.Msg + "\n")
	}
	return b.String()
}
