package kgraph

import (
	"os"
	"path/filepath"
	"testing"
)

// corpus writes a one-source index and returns its root.
func repairCorpus(t *testing.T, docRel, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "matter")
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(docRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, docRel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, dir, "",
		"- id: s-doc\n  record: The exhibit\n  doc: "+docRel+"\n  class: record\n"+
			"- id: c-one\n  claim: Something the exhibit shows\n  status: asserted\n  attested_by: s-doc\n")
	return root
}

func lockedGraph(t *testing.T, root string) (*Graph, LockSet) {
	t.Helper()
	g, _, err := ScanFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LockAll(root, g); err != nil {
		t.Fatal(err)
	}
	g, _, err = ScanFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ReadLocks(root, g)
	if err != nil {
		t.Fatal(err)
	}
	return g, l
}

// A `doc:` is a path, and a path is not an identity. Reorganising a corpus
// breaks every citation into the directories that moved, and without this the
// graph cannot tell "this exhibit is gone" from "this exhibit is one directory
// over" — which is the difference between a lost document and a tidy-up.
func TestRelocationIsFoundByContentHash(t *testing.T) {
	root := repairCorpus(t, "evidence/exhibit.md", "# Exhibit\n\nThe contents.\n")
	g, l := lockedGraph(t, root)

	// Move it, exactly as a reorganisation would.
	os.MkdirAll(filepath.Join(root, "matter", "legacy"), 0o755)
	if err := os.Rename(filepath.Join(root, "matter/evidence/exhibit.md"),
		filepath.Join(root, "matter/legacy/exhibit.md")); err != nil {
		t.Fatal(err)
	}

	moved, amb, err := FindRelocations(root, g, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(amb) != 0 {
		t.Fatalf("nothing is ambiguous here: %+v", amb)
	}
	if len(moved) != 1 || moved[0].SourceID != "s-doc" {
		t.Fatalf("the moved exhibit was not found: %+v", moved)
	}
	if moved[0].Now != "matter/legacy/exhibit.md" {
		t.Fatalf("wrong destination: %+v", moved[0])
	}

	// Repaired through the STORE: the move is a re-assertion with a signer and a
	// note, not a text edit. Editing the old line would erase the fact that the
	// corpus once pointed somewhere else, which is the history the store keeps.
	st := storeFor(t, root)
	if n, err := ApplyRelocationsTo(st, legal, root, moved, "carl"); err != nil || n != 1 {
		t.Fatalf("apply: %d %v", n, err)
	}
	if err := exportStore(t, st, root, "matter"); err != nil {
		t.Fatal(err)
	}
	// And the graph must now resolve it — written relative to the DECLARING file,
	// which is how DocPathOf reads it back.
	g2, _, err := ScanFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	l2, _ := ReadLocks(root, g2)
	sts, _ := g2.SourceStatuses(root, l2)
	for _, s := range sts {
		if s.ID == "s-doc" && s.State == SrcVanished {
			t.Fatalf("still vanished after repair: %+v", s)
		}
	}
}

// A document whose bytes CHANGED is not a relocation. Matching it would repoint
// a citation at a different version of the exhibit and call it the same one,
// which is precisely the drift the lock exists to catch.
func TestChangedContentIsNotARelocation(t *testing.T) {
	root := repairCorpus(t, "evidence/exhibit.md", "# Exhibit\n\nThe contents.\n")
	g, l := lockedGraph(t, root)

	os.MkdirAll(filepath.Join(root, "matter", "legacy"), 0o755)
	os.Remove(filepath.Join(root, "matter/evidence/exhibit.md"))
	os.WriteFile(filepath.Join(root, "matter/legacy/exhibit.md"),
		[]byte("# Exhibit\n\nDifferent contents.\n"), 0o644)

	moved, _, err := FindRelocations(root, g, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Fatalf("a rewritten document is not the one that was cited: %+v", moved)
	}
}

// Two files with identical bytes are two exhibits. Picking one is a claim about
// which was cited that nothing in the graph supports, so both are reported and
// neither is applied.
func TestIdenticalCandidatesAreAmbiguousNotGuessed(t *testing.T) {
	root := repairCorpus(t, "evidence/exhibit.md", "# Exhibit\n\nThe contents.\n")
	g, l := lockedGraph(t, root)

	body, _ := os.ReadFile(filepath.Join(root, "matter/evidence/exhibit.md"))
	os.MkdirAll(filepath.Join(root, "matter", "a"), 0o755)
	os.MkdirAll(filepath.Join(root, "matter", "b"), 0o755)
	os.WriteFile(filepath.Join(root, "matter/a/exhibit.md"), body, 0o644)
	os.WriteFile(filepath.Join(root, "matter/b/copy.md"), body, 0o644)
	os.Remove(filepath.Join(root, "matter/evidence/exhibit.md"))

	moved, amb, err := FindRelocations(root, g, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Fatalf("must not choose between identical candidates: %+v", moved)
	}
	if len(amb) != 1 || len(amb[0].Candidates) != 2 {
		t.Fatalf("both candidates must be reported: %+v", amb)
	}
}

// An unlocked source has no recorded identity, so a missing document is just
// missing. Guessing from the filename would be exactly the path-is-identity
// assumption this feature exists to remove.
func TestUnlockedSourceIsNotRelocated(t *testing.T) {
	root := repairCorpus(t, "evidence/exhibit.md", "# Exhibit\n\nThe contents.\n")
	g, _, err := ScanFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := ReadLocks(root, g) // never locked

	os.MkdirAll(filepath.Join(root, "matter", "legacy"), 0o755)
	os.Rename(filepath.Join(root, "matter/evidence/exhibit.md"),
		filepath.Join(root, "matter/legacy/exhibit.md"))

	moved, amb, err := FindRelocations(root, g, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 || len(amb) != 0 {
		t.Fatalf("without a locked hash there is nothing to match on: %+v %+v", moved, amb)
	}
}
