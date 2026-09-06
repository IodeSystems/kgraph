package kgraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A path typed into a box is not discoverable. Discovery is what makes the
// console usable without already knowing the answer.
func TestDiscoverFindsProjectRoots(t *testing.T) {
	cs, err := Discover("examples", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("want one project under examples/, got %v", cs)
	}
	c := cs[0]
	if !filepath.IsAbs(c.Root) {
		t.Errorf("root should be absolute: %q", c.Root)
	}
	// One log per index now, not ten fact files — the corpus is the same size,
	// the unit of discovery is not.
	if c.Facts < 2 || c.Specs < 10 {
		t.Errorf("counts look wrong: %d facts, %d specs", c.Facts, c.Specs)
	}
}

// Depth alone is a blunt instrument: a project one level deeper than the guess
// vanishes with nothing to say why. This is the case that caught me — the
// fixtures sit at examples/fence-dispute/, one below where I counted.
func TestDiscoverDepthIsGenerousByDefault(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(filepath.Dir(wd)) // two above the module
	deep, err := Discover(parent, 0)         // 0 ⇒ the default
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range deep {
		if filepath.Base(c.Root) == "examples" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the default depth must reach a project nested a few levels down: %v", deep)
	}
	// And a shallow limit genuinely excludes it, so the knob does something.
	shallow, _ := Discover(parent, 1)
	for _, c := range shallow {
		if filepath.Base(c.Root) == "examples" {
			t.Fatal("depth 1 should not reach it")
		}
	}
}

// The clock, not the depth, is what protects the console from a huge tree.
func TestDiscoverRespectsTimeBudget(t *testing.T) {
	start := time.Now()
	if _, err := DiscoverWithin("/", 30, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("walk ignored its budget: %v", el)
	}
}

func TestDiscoverSkipsNoise(t *testing.T) {
	dir := t.TempDir()
	for _, junk := range []string{"node_modules", ".hidden", "vendor"} {
		p := filepath.Join(dir, junk)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "x"+FactsSuffix), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs, err := Discover(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Fatalf("build and hidden directories are not projects: %v", cs)
	}
}

// THE DAEMON MUST RESOLVE THE SAME INDEX THE CLI DOES.
//
// `Registry.Get` called `Load(root)` — the DEFAULT index — while `kg` resolved
// the marker owning the working directory. So `kg daemon`, `kg serve` and
// `kg scopes` answered from a different graph than the CLI, in the same
// checkout: in a corpus whose facts all live under markers, an EMPTY one. The
// viewer and MCP are the surfaces people actually use, and a viewer serving a
// different graph than the CLI is the cross-matter confusion indexes exist to
// remove, wearing the daemon's hat.
//
// This fails against the old code by construction: every fact here is inside a
// marked index, so the default graph has nothing in it.
func TestRegistryResolvesTheIndexOwningTheDirectory(t *testing.T) {
	root := t.TempDir()
	for _, m := range []struct{ dir, index, id string }{
		{"matter-a", "alpha", "c-alpha-only"},
		{"matter-b", "beta", "c-beta-only"},
	} {
		d := filepath.Join(root, m.dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".kg-index"),
			[]byte("index: "+m.index+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFactLog(t, root, m.dir,
			"- id: "+m.id+"\n  claim: A fact in "+m.index+"\n  status: asserted\n")
	}

	r := NewRegistry()
	a, err := r.Get(filepath.Join(root, "matter-a"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Index != "alpha" {
		t.Fatalf("resolved index %q, want alpha — the daemon is loading the wrong graph", a.Index)
	}
	if _, ok := a.Project().Graph.Lookup("c-alpha-only"); !ok {
		t.Error("the scope does not hold its own index's facts")
	}
	if _, ok := a.Project().Graph.Lookup("c-beta-only"); ok {
		t.Error("the scope reached into another index — that is the entanglement indexes remove")
	}

	// TWO INDEXES UNDER ONE ROOT MUST NOT SHARE A CACHE ENTRY. The scope key was
	// (root, branch), so the second directory returned the first one's graph.
	b, err := r.Get(filepath.Join(root, "matter-b"))
	if err != nil {
		t.Fatal(err)
	}
	if b.Index != "beta" {
		t.Fatalf("second index resolved as %q — the cache key is too narrow", b.Index)
	}
	if _, ok := b.Project().Graph.Lookup("c-beta-only"); !ok {
		t.Error("the second scope came back with the first index's graph")
	}
	if a.Project() == b.Project() {
		t.Error("both directories share one loaded project")
	}
}
