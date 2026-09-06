package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// migrateInto opens a scratch store and migrates an index into it.
//
// The store lives outside the corpus by design, so a test cannot let it default
// to `~/.kgraph` — that is the real one.
func migrateInto(t *testing.T, root, index, by, at string) (Store, *MigrateResult) {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), index+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	res, err := MigrateFacts(st, root, index, by, at)
	if err != nil {
		t.Fatal(err)
	}
	return st, res
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		out := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// THIS TEST IS THE MIGRATION. Everything else about it is a claim.
//
// Run over the REAL fixture corpora rather than a hand-built one, because a
// fixture written for this test would only exercise what its author remembered.
// `examples/` is two hand-built graphs from real `~/life` projects and is the
// test corpus, not decoration — it has caught four format bugs, six reversed
// hops and two evaluator bugs, and if a migration breaks any idiom it is where
// that shows.
//
// The assertion is `SemHash` equality across every node. That hash covers
// content, incident edges and a source's authored provenance, so if every node
// matches, nothing downstream — staleness, source drift, a standing query's pin
// — can tell which door the facts came through. Which is what "migrated" has to
// mean.
func TestMigratingTheFixtureCorpusChangesNothing(t *testing.T) {
	for _, index := range []string{"fence-dispute", "clinic-billing"} {
		t.Run(index, func(t *testing.T) {
			// BEFORE is the committed export in `examples/`; AFTER is that same
			// corpus migrated from its original `*.kfacts.md` in `testdata/legacy`.
			// So this pins two things at once: that migration preserves meaning, and
			// that the fixture checked into `examples/` is genuinely what its source
			// produces rather than something regenerated once and trusted since.
			before, ds, err := LoadIn("examples", index)
			if err != nil {
				t.Fatal(err)
			}
			if errs := Errors(ds); len(errs) > 0 {
				t.Fatalf("the committed export does not load: %v", errs)
			}
			if len(before.Graph.Nodes) == 0 {
				t.Fatal("no nodes — the test is not exercising anything")
			}

			root := t.TempDir()
			copyTree(t, "testdata/legacy", root)
			st, res := migrateInto(t, root, index, "carl", "2026-08-30T12:00:00Z")
			if res.Count != len(before.Graph.Nodes) {
				t.Fatalf("migrated %d assertions for %d nodes", res.Count, len(before.Graph.Nodes))
			}

			log, err := st.All()
			if err != nil {
				t.Fatal(err)
			}
			dir, err := exportDir(root, index)
			if err != nil {
				t.Fatal(err)
			}
			dl, err := DialectAt(root, filepath.Join(root, dir))
			if err != nil {
				t.Fatal(err)
			}
			doc, fds := FoldAssertions(dl, "facts.jsonl", log)
			if errs := Errors(fds); len(errs) > 0 {
				t.Fatalf("the folded log does not parse: %v", errs)
			}
			after, bds := BuildWith([]*Doc{doc}, AttestSet{})
			if errs := Errors(bds); len(errs) > 0 {
				t.Fatalf("the folded graph does not build: %v", errs)
			}

			if len(after.Nodes) != len(before.Graph.Nodes) {
				t.Fatalf("node count moved: %d → %d", len(before.Graph.Nodes), len(after.Nodes))
			}
			if len(after.Edges) != len(before.Graph.Edges) {
				t.Errorf("edge count moved: %d → %d", len(before.Graph.Edges), len(after.Edges))
			}
			bad := 0
			for id, want := range before.Graph.SemHash {
				got, ok := after.SemHash[id]
				if !ok {
					t.Errorf("%s vanished in migration", id)
					bad++
					continue
				}
				if got != want {
					if bad < 5 {
						t.Errorf("%s hashes differently:\n  file %s\n  log  %s", id, want, got)
					}
					bad++
				}
			}
			if bad > 0 {
				t.Fatalf("%d of %d nodes changed meaning", bad, len(before.Graph.SemHash))
			}
		})
	}
}

// The prose is the one thing a migration cannot carry, and it must be a NUMBER
// rather than a silence.
//
// A `*.kfacts.md` explains itself in markdown, and that text belongs to a file
// rather than to any one entry in it — attaching it to an arbitrary assertion
// would invent an attribution, and dropping it quietly would lose the most
// valuable thing in the file. So the migration counts it and says so.
func TestMigrationReportsTheProseItCannotCarry(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/legacy", root)
	_, res := migrateInto(t, root, "fence-dispute", "carl", "2026-08-30T12:00:00Z")
	if len(res.Files) == 0 {
		t.Fatal("nothing migrated")
	}
	total := 0
	for _, n := range res.Prose {
		total += n
	}
	if total == 0 {
		t.Fatal("the fixture corpus has prose around its blocks; a zero count means " +
			"the counter is broken, not that the files are bare")
	}
	t.Logf("%d file(s), %d assertion(s), %d line(s) of prose not carried",
		len(res.Files), res.Count, total)
}

// Every assertion says it arrived in a format change rather than being asserted
// by somebody. A reader of the log three years from now must be able to tell
// those apart.
func TestMigratedAssertionsAreMarkedAsMigrated(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/legacy", root)
	st, _ := migrateInto(t, root, "fence-dispute", "carl", "2026-08-30T12:00:00Z")
	log, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range log {
		if !strings.HasPrefix(a.Note, "migrated from ") {
			t.Fatalf("%s does not say where it came from: %q", a.ID, a.Note)
		}
		if a.By != "carl" {
			t.Fatalf("%s is signed %q", a.ID, a.By)
		}
		if a.Op != OpAssert {
			t.Fatalf("%s migrated as %q", a.ID, a.Op)
		}
	}
}

// A migration is a record of who did it, so it cannot be anonymous.
func TestMigrationMustBeSigned(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/legacy", root)
	st, err := OpenStore(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := MigrateFacts(st, root, "fence-dispute", "  ", "2026-08-30T12:00:00Z"); err == nil {
		t.Fatal("an unsigned migration must be refused")
	}
}

// LOADING AFTER A MIGRATION: the store wins, and the inert files are NAMED.
//
// The failure to design against is not ambiguity, it is SILENCE. Somebody edits
// a `*.kfacts.md` after migrating, nothing happens, and the corpus reports
// itself clean. So a non-empty store wins outright and the files that no longer
// matter are said out loud.
func TestAfterMigratingTheStoreWinsAndTheFilesAreNamed(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/legacy", root)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The committed export is the reference: migrating the legacy corpus must
	// produce the same graph, whichever door it comes through.
	before, _, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}

	path, err := StorePath("fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, home) {
		t.Fatalf("the store escaped the test's HOME: %s", path)
	}
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateFacts(st, root, "fence-dispute", "carl", "2026-08-30T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	after, ds, err := LoadIn(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	// Same graph, different door.
	if len(after.Graph.Nodes) != len(before.Graph.Nodes) {
		t.Fatalf("node count moved: %d → %d", len(before.Graph.Nodes), len(after.Graph.Nodes))
	}
	for id, want := range before.Graph.SemHash {
		if after.Graph.SemHash[id] != want {
			t.Errorf("%s differs when loaded from the store", id)
		}
	}
	// And the now-inert files are named, so an edit to one is not silent.
	var told bool
	for _, d := range ds {
		if strings.Contains(d.Msg, "no longer") && strings.Contains(d.Msg, "kfacts.md") {
			told = true
		}
	}
	if !told {
		t.Error("the fact files are inert and nothing said so")
	}
}

// An unmigrated corpus is unaffected: no store, so the files are still read.
// Nil from the store must mean "no store", never "an empty graph".
func TestAnUnmigratedCorpusIsReportedNotSilentlyEmpty(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/legacy", root)
	t.Setenv("HOME", t.TempDir())
	// An unmigrated corpus loads as EMPTY, and that must be an error naming the
	// files rather than a silent nothing. Silence is the whole failure mode: a
	// corpus that reports itself clean while holding none of its facts.
	_, ds, err := LoadIn(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	var told bool
	for _, d := range Errors(ds) {
		if strings.Contains(d.Msg, "no longer read") && strings.Contains(d.Msg, "kg migrate") {
			told = true
		}
	}
	if !told {
		t.Fatalf("an unmigrated corpus loaded empty and said nothing: %v", ds)
	}
}

// Scanning must not CREATE a store. A read command that writes to `~/.kgraph`
// as a side effect is how a corpus acquires one nobody asked for.
func TestScanningDoesNotCreateAStore(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "examples", root)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, _, err := LoadIn(root, "fence-dispute"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kgraph")); !os.IsNotExist(err) {
		t.Error("scanning created a store directory")
	}
}

// A CORPUS WITH ONLY AN EXPORT LOADS. No database, no fact files — the
// committed text is enough.
//
// This is the property that makes the export more than a backup, and it is the
// prerequisite for deleting the parser: an index has to be discoverable and
// loadable by its own log, or migrating one makes it vanish.
func TestACorpusLoadsFromItsExportAlone(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/legacy", root)
	t.Setenv("HOME", t.TempDir())

	before, _, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}

	st, _ := migrateInto(t, root, "fence-dispute", "carl", "2026-08-30T12:00:00Z")
	dir, err := exportDir(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExportJSONL(st, filepath.Join(root, dir, assertName)); err != nil {
		t.Fatal(err)
	}
	// The fact files go. What is left is the export and the index marker.
	paths, err := FindFacts(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range paths {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}

	// Still discoverable...
	names, err := Indexes(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range names {
		if n == "fence-dispute" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a migrated index vanished from Indexes(): %v", names)
	}

	// ...and still the same graph.
	after, ds, err := LoadIn(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if errs := Errors(ds); len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	if len(after.Graph.Nodes) != len(before.Graph.Nodes) {
		t.Fatalf("node count moved: %d → %d", len(before.Graph.Nodes), len(after.Graph.Nodes))
	}
	for id, want := range before.Graph.SemHash {
		if after.Graph.SemHash[id] != want {
			t.Errorf("%s differs when loaded from the export alone", id)
		}
	}
	// Nothing to warn about: there are no inert files left.
	for _, d := range ds {
		if strings.Contains(d.Msg, "no longer") {
			t.Errorf("warned about files that are gone: %s", d.Msg)
		}
	}
}
