package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// twoMatters lays out the shape that motivated indexes: one checkout, two
// unrelated matters, and a person who genuinely appears in both.
func twoMatters(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "projects/case-a/.kg-index"), "case-a\n")
	writeFactLog(t, root, "projects/case-a", "- id: imogen\n  entity: Imogen — a party\n  status: asserted\n")
	write(t, filepath.Join(root, "projects/case-b/.kg-index"), "case-b\n")
	writeFactLog(t, root, "projects/case-b", "- id: imogen\n  entity: Imogen — a patient\n  status: asserted\n")
	writeFactLog(t, root, "", "- id: x\n  entity: X\n  status: asserted\n")
	return root
}

func TestIndexAtNearestMarkerWins(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a/.kg-index"), "outer\n")
	write(t, filepath.Join(root, "a/b/.kg-index"), "inner\n")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := IndexAt(root, deep)
	if err != nil {
		t.Fatal(err)
	}
	if got != "inner" {
		t.Fatalf("nearest marker should win, got %q", got)
	}
	if got, _ := IndexAt(root, filepath.Join(root, "a")); got != "outer" {
		t.Fatalf("outer = %q", got)
	}
	if got, _ := IndexAt(root, root); got != DefaultIndex {
		t.Fatalf("unmarked root should be the default index, got %q", got)
	}
}

// An empty marker names the index after its directory, so the common case does
// not require stating the name twice.
func TestEmptyMarkerNamesItselfAfterItsDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "fence-dispute/.kg-index"), "")
	got, err := IndexAt(root, filepath.Join(root, "fence-dispute"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "fence-dispute" {
		t.Fatalf("got %q, want the directory name", got)
	}
}

func TestMarkerAcceptsKeyedOrBareName(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "k/.kg-index"), "# a comment\n\nindex: keyed\n")
	write(t, filepath.Join(root, "b/.kg-index"), "\nbare\n")
	if got, _ := IndexAt(root, filepath.Join(root, "k")); got != "keyed" {
		t.Fatalf("keyed form: %q", got)
	}
	if got, _ := IndexAt(root, filepath.Join(root, "b")); got != "bare" {
		t.Fatalf("bare form: %q", got)
	}
}

// The bug the closed key set exists for. The original parser split the first
// non-blank line on ":" and took the right-hand side as the name, so a marker
// declaring only a dialect named the index after the DIALECT — silently, since
// an index name is validated against nothing.
func TestDialectOnlyMarkerDoesNotNameTheIndex(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "fence-dispute/.kg-index"), "dialect: legal\n")
	got, err := IndexAt(root, filepath.Join(root, "fence-dispute"))
	if err != nil {
		t.Fatal(err)
	}
	if got == "legal" {
		t.Fatal("the dialect named the index")
	}
	if got != "fence-dispute" {
		t.Fatalf("got %q, want the directory name", got)
	}
}

func TestMarkerParsesNameAndDialectTogether(t *testing.T) {
	for _, body := range []string{
		"index: billing\ndialect: legal\n",
		"dialect: legal\nindex: billing\n",
		"# both forms mix\nbilling\ndialect: legal\n",
	} {
		decl, err := parseIndexDecl(body)
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if decl.Name != "billing" || decl.Dialect != "legal" {
			t.Fatalf("%q parsed as %+v", body, decl)
		}
	}
}

// An unknown key must not fall through to the bare-name branch: that is the
// original bug wearing a typo. A misspelled key would otherwise name the index.
func TestMarkerRejectsMalformedDeclarations(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"dialct: legal\n", "unknown key"},
		{"index:\n", "no value"},
		{"index: a\nindex: b\n", "declared twice"},
		{"a\nb\n", "declared twice"},
		{"dialect: legal\ndialect: audit\n", "dialect declared twice"},
	} {
		_, err := parseIndexDecl(tc.body)
		if err == nil {
			t.Fatalf("%q: want an error", tc.body)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: error %q should mention %q", tc.body, err, tc.want)
		}
	}
}

// A marker that will not parse leaves membership unknown. Resolving it to the
// default index would quietly build a graph out of files that may not belong to
// it — the entanglement indexes exist to prevent.
func TestUnparseableMarkerIsAnErrorNotADefault(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "m/.kg-index"), "dialct: legal\n")
	writeFactLog(t, root, "m", "- id: x\n  entity: X\n  status: asserted\n")

	if _, err := IndexAt(root, filepath.Join(root, "m")); err == nil {
		t.Fatal("IndexAt should refuse an unparseable marker")
	} else if !strings.Contains(err.Error(), IndexMarker) {
		t.Fatalf("error should name the marker: %v", err)
	}
	// Discovery over the LOGS now, since that is what a corpus has. The rule is
	// unchanged: a marker that will not parse leaves membership unknown, and
	// guessing is how a foreign matter's facts end up in a graph.
	if _, err := FindFactLogs(root, DefaultIndex); err == nil {
		t.Fatal("discovery should refuse rather than guess membership")
	}
	if _, err := Indexes(root); err == nil {
		t.Fatal("Indexes should refuse rather than guess membership")
	}
}

// One broken marker is one problem, however many files sit under it.
func TestMarkerErrorReportedOncePerMarker(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "m/.kg-index"), "dialct: legal\n")
	for _, n := range []string{"a", "b", "c"} {
		writeFactLog(t, root, filepath.Join("m", n), "- id: "+n+"\n  entity: N\n  status: asserted\n")
	}
	_, err := FindFactLogs(root, DefaultIndex)
	if err == nil {
		t.Fatal("want an error")
	}
	if got := strings.Count(err.Error(), "unknown key"); got != 1 {
		t.Fatalf("marker error reported %d times, want 1: %v", got, err)
	}
}

// The regression this whole scheme exists for. Before indexes, a graph rooted at
// the checkout absorbed every sibling matter, so an unqualified query resolved
// across them and a document generated for one could carry facts from another.
func TestGraphIsClosedOverItsIndex(t *testing.T) {
	root := twoMatters(t)

	a, diags, err := ScanFactsIn(root, "case-a")
	if err != nil {
		t.Fatal(err)
	}
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatalf("case-a: %v", errs)
	}
	if _, ok := a.Nodes["imogen"]; !ok {
		t.Fatal("case-a should contain its own imogen")
	}
	if got := len(a.Nodes); got != 1 {
		t.Fatalf("case-a graph has %d nodes; a sibling matter leaked in", got)
	}

	b, _, err := ScanFactsIn(root, "case-b")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(b.Nodes); got != 1 {
		t.Fatalf("case-b graph has %d nodes", got)
	}

	// Same id, two indexes, two distinct facts. Neither may see the other.
	if a.Nodes["imogen"].Body == b.Nodes["imogen"].Body {
		t.Fatal("the two matters' Imogen nodes collapsed into one")
	}
}

// Marked subtrees leave the default index. Unmarked files stay in it, so a
// corpus that predates indexes keeps working unchanged.
func TestDefaultIndexHoldsOnlyUnmarkedFiles(t *testing.T) {
	root := twoMatters(t)
	facts, err := FindFactLogs(root, DefaultIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0] != assertName {
		t.Fatalf("default index = %v, want only the unmarked corpus", facts)
	}
}

// A DECLARATION resolves against its own index, so one matter's questions can
// never be asked of another's facts. `standing.yaml` lives inside the index
// directory, so the scoping is by LOCATION and there is no header that could
// redirect it — which is what the four `## Index` header tests deleted alongside
// this one were guarding, back when a spec could name an index it did not live
// in.
func TestDeclarationsAreScopedToTheirIndex(t *testing.T) {
	root := twoMatters(t)
	writeStandingFor(t, root, "projects/case-a", "a-question", "entity")
	writeStandingFor(t, root, "projects/case-b", "b-question", "entity")

	p, _, err := LoadIn(root, "case-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Standing["a-question"]; !ok {
		t.Fatal("case-a cannot see its own declaration")
	}
	if _, ok := p.Standing["b-question"]; ok {
		t.Fatal("case-a reached case-b's declarations")
	}
	if p.Index != "case-a" {
		t.Fatalf("project index = %q", p.Index)
	}
}

// writeStandingFor drops one standing query into an index's directory.
func writeStandingFor(t *testing.T, root, dir, name, query string) {
	t.Helper()
	write(t, filepath.Join(root, dir, standingName),
		"- name: "+name+"\n  query: "+query+"\n")
}

func TestIndexesListsEveryDeclaredIndex(t *testing.T) {
	root := twoMatters(t)
	got, err := Indexes(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{DefaultIndex, "case-a", "case-b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
