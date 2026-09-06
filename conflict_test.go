package kgraph

import (
	"os"
	"path/filepath"
	"testing"
)

// The conflict machinery and the graph view. What was `render_test.go` covered
// the prompt, the managed block and attach, and went with them; these two are
// what was never about prose.

// The graph view must never silently drop nodes: a truncated map reads as "this
// is all there is", the same lie as an empty section in a generated document.
func TestInducedBoundsAndReportsTruncation(t *testing.T) {
	g := fixtureGraph(t)
	ids := run(t, g, `claim -contradicts- claim @now`)
	if len(ids) < 5 {
		t.Fatalf("fixture should have contradiction pairs: %v", ids)
	}

	n0, e0, tr0 := g.Induced(ids, 0, 250, "2026-07-26")
	if len(n0) != len(ids) || tr0 {
		t.Fatalf("depth 0 is the result set itself: %d vs %d", len(n0), len(ids))
	}
	for _, n := range n0 {
		if !n.Matched {
			t.Fatalf("%s is in the result and must be marked matched", n.ID)
		}
	}

	n1, e1, _ := g.Induced(ids, 1, 250, "2026-07-26")
	if len(n1) <= len(n0) || len(e1) < len(e0) {
		t.Fatalf("depth 1 must add neighbours: %d→%d nodes, %d→%d edges",
			len(n0), len(n1), len(e0), len(e1))
	}
	var neighbours int
	for _, n := range n1 {
		if !n.Matched {
			neighbours++
		}
	}
	if neighbours == 0 {
		t.Fatal("neighbours must be distinguishable from matches")
	}

	_, _, tr := g.Induced(ids, 2, 8, "2026-07-26")
	if !tr {
		t.Fatal("a bounded induction must say it truncated")
	}

	// Every edge returned must connect two returned nodes, or the view draws a
	// line to nothing.
	in := map[string]bool{}
	for _, n := range n1 {
		in[n.ID] = true
	}
	for _, e := range e1 {
		if !in[e.Src] || !in[e.Dst] {
			t.Fatalf("dangling edge %s -> %s", e.Src, e.Dst)
		}
	}
}

func writeTemp(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
