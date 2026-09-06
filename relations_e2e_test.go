package kgraph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func graphWithSources(t *testing.T, facts string) *Graph {
	t.Helper()
	root := t.TempDir()
	writeFactLog(t, root, "", stripFence(facts))
	g, _, err := ScanFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

const twoDeeds = "# case\n\n## Facts\n\n```kfacts\n" + `- id: s-old-deed
  document: 1993 quit claim deed
  doc: records/1993-qcd.pdf
  class: record
- id: s-new-deed
  document: 2008 deed
  doc: records/2008-deed.pdf
  class: record
` + "```\n"

// A ruling raglit made must reach the graph as a statement about SOURCE IDS —
// an id is what a person reading a fact actually sees.
func TestASupersededVersionWarnsAtTheFactsThatRestOnIt(t *testing.T) {
	g := graphWithSources(t, twoDeeds)
	g.Relations = RelationSet{
		relKey("records/1993-qcd.pdf", "records/2008-deed.pdf"): {
			A: "records/1993-qcd.pdf", B: "records/2008-deed.pdf",
			Kind: RelationVersion, Supersedes: "records/2008-deed.pdf",
		},
	}
	var got string
	for _, d := range g.checkRelations() {
		if strings.Contains(d.Msg, "SUPERSEDED") {
			got = d.Msg
		}
	}
	if got == "" {
		t.Fatal("a superseded version produced no warning")
	}
	if !strings.Contains(got, "s-old-deed") {
		t.Errorf("must name the superseded SOURCE ID: %s", got)
	}
	if !strings.Contains(got, "s-new-deed") {
		t.Errorf("must name what governs instead: %s", got)
	}
}

// Two ids for one instrument are not two pieces of evidence.
func TestACopyWarnsThatTheTwoDoNotCorroborate(t *testing.T) {
	g := graphWithSources(t, twoDeeds)
	g.Relations = RelationSet{
		relKey("records/1993-qcd.pdf", "records/2008-deed.pdf"): {
			A: "records/1993-qcd.pdf", B: "records/2008-deed.pdf", Kind: RelationCopy,
		},
	}
	for _, d := range g.checkRelations() {
		if strings.Contains(d.Msg, "do not corroborate") {
			return
		}
	}
	t.Fatal("a copy ruling produced no corroboration warning")
}

// Nil means NOT LOADED, and an unloaded set must never read as reassurance.
// A ruling about documents this graph does not cite is raglit's business.
func TestSilentWhenNotLoadedOrWhenTheDocsAreNotCited(t *testing.T) {
	g := graphWithSources(t, twoDeeds)
	if ds := g.checkRelations(); len(ds) != 0 {
		t.Fatalf("nil Relations produced %d diagnostic(s)", len(ds))
	}
	g.Relations = RelationSet{
		relKey("nowhere/p.pdf", "nowhere/q.pdf"): {
			A: "nowhere/p.pdf", B: "nowhere/q.pdf",
			Kind: RelationVersion, Supersedes: "nowhere/q.pdf",
		},
	}
	if ds := g.checkRelations(); len(ds) != 0 {
		t.Fatalf("warned about documents the graph does not cite: %v", ds)
	}
}

// The contract that keeps a scan working on a machine with no daemon: asking
// must fail as UNAVAILABLE, distinctly, so the caller can stay silent instead of
// reporting "nothing ruled".
func TestFetchReportsUnavailableRatherThanEmpty(t *testing.T) {
	t.Setenv("RAGLIT_DAEMON", "http://127.0.0.1:1") // nothing listens on port 1
	rels, err := FetchRelations(context.Background(), t.TempDir())
	if err == nil {
		t.Fatalf("want an error when raglit is unreachable, got %d relations", len(rels))
	}
	if !errors.Is(err, ErrRaglitUnavailable) {
		t.Errorf("want ErrRaglitUnavailable so the caller can tell it from 'nothing ruled', got %v", err)
	}
}

// And a scan must still succeed with no daemon running.
func TestAScanSucceedsWithNoDaemon(t *testing.T) {
	t.Setenv("RAGLIT_DAEMON", "http://127.0.0.1:1")
	root := t.TempDir()
	writeFactLog(t, root, "", stripFence(twoDeeds))
	g, _, err := ScanFacts(root)
	if err != nil {
		t.Fatalf("a scan must not fail because raglit is down: %v", err)
	}
	if g.Relations != nil {
		t.Error("Relations should stay nil (NOT LOADED) when raglit could not be asked")
	}
}
