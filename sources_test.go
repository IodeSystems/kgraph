package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// driftProject writes a project whose one fact rests on one document, renders a
// spec against it, and attaches, so the spec starts out `fresh`.
func driftProject(t *testing.T, docBody string) (root string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("documents/exhibit.md", docBody)
	writeLegacyFacts(t, root, "facts.kfacts.md", "# f\n\n## Facts\n\n```kfacts\n"+
		"- id: s-exhibit\n  record: An exhibit\n  doc: documents/exhibit.md\n  class: record\n"+
		"- id: c1\n  claim: The corners were visible\n  status: asserted\n  attested_by: s-exhibit\n"+
		"```\n")
	write("report.kgraph.md", "# r\n\n## Purpose\np\n\n## Query\n```kgraph\n"+
		"vis: claim[status=asserted]\n```\n\n## Prompt\n{{vis}}\n")
	write("report.md", "generated\n")
	return root
}

func loadProject(t *testing.T, root string) *Project {
	t.Helper()
	p, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The load-bearing invariant. Provenance in the semantic hash would mean
// re-exporting one PDF flags every document rendering any fact it attests — the
// false-flag storm the hash exists to prevent. Drift is a separate signal.
func TestSourceDriftIsNotInSemHash(t *testing.T) {
	root := driftProject(t, "v1\n")
	p := loadProject(t, root)
	if _, _, err := LockAll(root, p.Graph); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	before := map[string]string{}
	for id, h := range p.Graph.SemHash {
		before[id] = h
	}
	if err := os.WriteFile(filepath.Join(root, "documents/exhibit.md"), []byte("v2 entirely different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	for id, h := range p.Graph.SemHash {
		if before[id] != h {
			t.Errorf("%s: sem_hash moved because a document was re-exported", id)
		}
	}
	if len(p.Graph.SourceDrift) == 0 {
		t.Fatal("the drift itself must still be recorded, just not in the hash")
	}
}

// A nil map means NOT CHECKED. Nothing may read it as "nothing drifted", or a
// graph built without a filesystem would silently report every document fresh.
func TestUncheckedDriftIsNotReportedAsClean(t *testing.T) {
	d, _ := parseFixture("f.facts", []byte("```kfacts\n- id: a\n  claim: X\n  status: asserted\n```\n"))
	g, _ := Build([]*Doc{d})
	if g.SourceDrift != nil {
		t.Fatal("Build must not claim to have checked the filesystem")
	}
	if got := g.DriftedSources("a"); got != nil {
		t.Fatalf("an unchecked graph must not answer the drift question, got %v", got)
	}
}

// A document only renders the conclusion; the exhibit is two hops away, under an
// inference's premise. An inference is only as good as the facts beneath it.
func TestDriftReachesThroughAnInferencePremise(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("documents/deed.md", "original\n")
	writeLegacyFacts(t, root, "facts.kfacts.md", "# f\n\n## Facts\n\n```kfacts\n"+
		"- id: s-deed\n  record: The deed\n  doc: documents/deed.md\n  class: record\n"+
		"- id: held-by-halloway\n  claim: The strip is held by Halloway\n  status: asserted\n  attested_by: s-deed\n"+
		"- id: s-concl\n  inference: Therefore no fee interest\n  derived_from: [held-by-halloway]\n  class: inference\n"+
		"- id: no-fee\n  claim: Bramble holds no fee interest\n  status: asserted\n  attested_by: s-concl\n"+
		"```\n")
	p := loadProject(t, root)
	if _, _, err := LockAll(root, p.Graph); err != nil {
		t.Fatal(err)
	}
	w("documents/deed.md", "AMENDED\n")
	p = loadProject(t, root)
	if got := p.Graph.DriftedSources("no-fee"); len(got) == 0 {
		t.Fatal("a conclusion whose premise rests on a moved deed was not reached")
	}
}

// `doc:` resolves against the DECLARING FILE's directory. A corpus keeps each
// project's evidence beside its facts while the root is the repository above them
// all, so resolving against the root would send every lookup to the wrong place.
func TestDocPathResolvesAgainstTheDeclaringFile(t *testing.T) {
	n := &Node{ID: "s", Kind: KSource, File: "projects/fence-dispute/parcels.kfacts.md",
		Source: &Source{DocPath: "documents/party-contacts.md"}}
	got, err := DocPathOf("/root", n)
	if err != nil {
		t.Fatal(err)
	}
	if got != "projects/fence-dispute/documents/party-contacts.md" {
		t.Fatalf("got %q", got)
	}
}

// An authored file must not aim the hasher at an arbitrary path, and a citation
// to a folder names no exhibit.
func TestBadDocPathsAreRejected(t *testing.T) {
	cases := []struct{ doc, want string }{
		{"/etc/passwd", "absolute"},
		{"../../../../etc/passwd", "escapes"},
		{"documents/court/", "names a directory"},
	}
	for _, c := range cases {
		n := &Node{ID: "s", Kind: KSource, File: "a/facts.kfacts.md", Source: &Source{DocPath: c.doc}}
		_, err := DocPathOf("/root", n)
		if err == nil {
			t.Errorf("%q was accepted", c.doc)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: want %q, got %v", c.doc, c.want, err)
		}
	}
}

// A trailing slash is certain from the text alone, so it is caught on a machine
// that does not hold the evidence — which is most of them.
func TestDirectoryDocIsCaughtWithoutTheFilePresent(t *testing.T) {
	root := t.TempDir()
	writeLegacyFacts(t, root, "f.kfacts.md",
		"```kfacts\n- id: s\n  record: R\n  doc: documents/court/\n  class: record\n"+
			"- id: c\n  claim: X\n  status: asserted\n  attested_by: s\n```\n")
	_, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range Errors(diags) {
		if strings.Contains(d.Msg, "names a directory") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a directory error, got %v", diags)
	}
}

// Dropping a vanished document's entry would silently turn "the exhibit
// disappeared" into "we never had it" — the one reading that hides the problem.
func TestVanishedDocumentKeepsItsLockEntry(t *testing.T) {
	root := driftProject(t, "v1\n")
	p := loadProject(t, root)
	if _, _, err := LockAll(root, p.Graph); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "documents/exhibit.md")); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	if _, _, err := LockAll(root, p.Graph); err != nil {
		t.Fatal(err)
	}
	l, err := ReadLocks(root, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	// The facts sit at the root of this fixture, so the lock is the root one and
	// keys are relative to it.
	if lk := l["."]; lk == nil {
		t.Fatal("no lock beside the facts")
	} else if _, ok := lk.Entries["documents/exhibit.md"]; !ok {
		t.Fatal("`kg source lock` erased the record of a document that vanished")
	}
	p = loadProject(t, root)
	sts, _ := p.Graph.SourceStatuses(root, l)
	if len(sts) != 1 || sts[0].State != SrcVanished {
		t.Fatalf("want vanished, got %+v", sts)
	}
}

// A declared document that was never here cannot be distinguished from a typo, so
// it must not be a scan diagnostic — the fixture corpus declares 32 of them and
// would otherwise never be clean.
func TestNeverSeenDocumentIsNotAScanError(t *testing.T) {
	root := t.TempDir()
	writeLegacyFacts(t, root, "f.kfacts.md",
		"```kfacts\n- id: s\n  record: R\n  doc: documents/elsewhere.pdf\n  class: record\n"+
			"- id: c\n  claim: X\n  status: asserted\n  attested_by: s\n```\n")
	p, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Errorf("a document that lives on another machine produced %v", d)
	}
	l, _ := ReadLocks(root, p.Graph)
	sts, _ := p.Graph.SourceStatuses(root, l)
	if len(sts) != 1 || sts[0].State != SrcNotFound {
		t.Fatalf("want not-found, got %+v", sts)
	}
}

// A present but unlocked document means we do not know which version was read.
// That has to be visible, or the first `source-changed` never fires.
func TestUnrecordedDocumentIsReported(t *testing.T) {
	root := driftProject(t, "v1\n")
	_, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range diags {
		if strings.Contains(d.Msg, "never been recorded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an unrecorded warning, got %v", diags)
	}
}

func TestLockRoundTrips(t *testing.T) {
	root := t.TempDir()
	in := &SourceLock{Entries: map[string]LockEntry{
		"b/second.md": {Hash: "sha256:bbbb", Size: 22, At: "2026-07-26"},
		"a/first.pdf": {Hash: "sha256:aaaa", Size: 11, At: "2026-07-25"},
	}}
	if err := WriteLock(root, "proj", in); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "proj", lockName))
	if err != nil {
		t.Fatal(err)
	}
	// Sorted, so the diff of this file reads as a list of exhibits that changed.
	if i, j := strings.Index(string(b), "a/first"), strings.Index(string(b), "b/second"); i > j {
		t.Error("entries are not sorted")
	}
	out, err := ReadLock(root, "proj")
	if err != nil {
		t.Fatal(err)
	}
	for p, e := range in.Entries {
		if out.Entries[p] != e {
			t.Errorf("%s: %+v != %+v", p, out.Entries[p], e)
		}
	}
}

// Every source in the corpus declares a resolvable, hashable path. This is what
// caught two nodes citing `documents/court/` — the docket folder, which names no
// exhibit and cannot be cited in a filing.
func TestCorpusDocPathsAreAllWellFormed(t *testing.T) {
	g := fixtureGraph(t)
	var n int
	for id, node := range g.Nodes {
		if node.Kind != KSource || node.Source == nil || node.Source.DocPath == "" {
			continue
		}
		n++
		if _, err := DocPathOf("examples", node); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if n < 15 {
		t.Fatalf("only %d sources declare a document — this test is not covering the corpus", n)
	}
}

// The daemon caches a scope until its fingerprint moves, and the fingerprint
// walk only sees `*.kfacts.md`/`*.kgraph.md`. An exhibit is re-scanned, every
// fact still reads the same, the fingerprint does not move — and the daemon keeps
// answering `fresh` for a document that is not. The CLI reloads every run and
// would have looked correct the whole time.
func TestChangedEvidenceInvalidatesTheDaemonCache(t *testing.T) {
	root := driftProject(t, "v1\n")
	r := NewRegistry()
	sc, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LockAll(root, sc.Project().Graph); err != nil {
		t.Fatal(err)
	}
	// Locking must itself invalidate: otherwise the daemon reports drift that has
	// already been reviewed and recorded.
	sc2, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if sc2 == sc {
		t.Fatal("`kg source lock` did not invalidate the cached scope")
	}
	if len(sc2.Project().Graph.SourceDrift) != 0 {
		t.Fatalf("want no drift right after locking, got %v", sc2.Project().Graph.SourceDrift)
	}

	touchLater(t, filepath.Join(root, "documents/exhibit.md"), "v2 says the opposite\n")
	sc3, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if sc3 == sc2 {
		t.Fatal("a re-scanned exhibit did not invalidate the cached scope")
	}
	if len(sc3.Project().Graph.SourceDrift) == 0 {
		t.Fatal("the reloaded scope does not report the drift")
	}
}

// A document appearing or vanishing changes what we report about it, so absence
// has to be part of the fingerprint too.
func TestVanishedEvidenceInvalidatesTheDaemonCache(t *testing.T) {
	root := driftProject(t, "v1\n")
	r := NewRegistry()
	sc, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LockAll(root, sc.Project().Graph); err != nil {
		t.Fatal(err)
	}
	sc, err = r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "documents/exhibit.md")); err != nil {
		t.Fatal(err)
	}
	sc2, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if sc2 == sc {
		t.Fatal("a vanished exhibit did not invalidate the cached scope")
	}
}

// `.kgraph/queries.md` decides how every spec referencing it resolves, and it
// lives under the directory the fingerprint walk skips.
func TestNamedQueryLibraryInvalidatesTheDaemonCache(t *testing.T) {
	root := driftProject(t, "v1\n")
	if err := os.MkdirAll(filepath.Join(root, ".kgraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	sc, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	touchLater(t, filepath.Join(root, ".kgraph/queries.md"),
		"# q\n\n```kgraph\nvisible: claim[status=asserted]\n```\n")
	sc2, err := r.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if sc2 == sc {
		t.Fatal("editing the named query library did not invalidate the cached scope")
	}
	if len(sc2.Project().Named) == 0 {
		t.Fatal("the reloaded scope did not pick up the library")
	}
}

// mtime granularity can be coarse enough that a same-size rewrite inside one tick
// is invisible. Push the mtime forward so the test is measuring the fingerprint's
// design and not the filesystem's clock.
func touchLater(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

// The lock must inherit its project's git treatment. `~/life` gitignores a whole
// project as "Medical PII — keep OUT of git history", and a single root-level lock
// recording `projects/.../documents/eob-2026-05-27.pdf` would have put those
// filenames and dates into history anyway. One lock per directory of facts is the
// only arrangement that cannot leak by accident.
func TestLockLivesBesideTheFactsThatDeclareIt(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"open/documents", "private/documents"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("open/documents/deed.md", "a deed\n")
	w("private/documents/eob.pdf", "an eob\n")
	writeLegacyFacts(t, root, "open/f.kfacts.md", "```kfacts\n"+
		"- id: t1\n  entity: A thing\n  status: asserted\n"+
		"- id: s-deed\n  record: The deed\n  doc: documents/deed.md\n  class: record\n"+
		"- id: c1\n  claim: X\n  status: asserted\n  about: t1\n  attested_by: s-deed\n```\n")
	writeLegacyFacts(t, root, "private/f.kfacts.md", "```kfacts\n"+
		"- id: t2\n  entity: A bill\n  status: asserted\n"+
		"- id: s-eob\n  record: The EOB\n  doc: documents/eob.pdf\n  class: record\n"+
		"- id: c2\n  claim: Y\n  status: asserted\n  about: t2\n  attested_by: s-eob\n```\n")

	p := loadProject(t, root)
	if _, _, err := LockAll(root, p.Graph); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"open", "private"} {
		if _, err := os.Stat(filepath.Join(root, dir, lockName)); err != nil {
			t.Errorf("no lock beside %s's facts: %v", dir, err)
		}
	}
	// Nothing at the root, which is the whole point: a root lock is outside every
	// project's gitignore.
	if _, err := os.Stat(filepath.Join(root, lockName)); err == nil {
		t.Error("a root-level lock was written")
	}
	// And the private lock must not name anything from the open project, or the
	// separation is cosmetic.
	b, err := os.ReadFile(filepath.Join(root, "private", lockName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "deed") || strings.Contains(string(b), "open/") {
		t.Errorf("the private lock names another project's documents:\n%s", b)
	}
	// Keys are relative to the lock, so it reads like the `doc:` lines beside it
	// and survives the project being moved.
	if !strings.Contains(string(b), "documents/eob.pdf") {
		t.Errorf("want a self-relative key, got:\n%s", b)
	}
	if strings.Contains(string(b), "private/documents") {
		t.Errorf("key is not relative to the lock:\n%s", b)
	}
}

// Drift still has to work once the locks are split.
func TestDriftWorksWithPerDirectoryLocks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proj", "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("proj/documents/x.md", "v1\n")
	writeLegacyFacts(t, root, "proj/f.kfacts.md", "```kfacts\n"+
		"- id: t\n  entity: A thing\n  status: asserted\n"+
		"- id: s\n  record: X\n  doc: documents/x.md\n  class: record\n"+
		"- id: c\n  claim: X says v1\n  status: asserted\n  about: t\n  attested_by: s\n```\n")
	p := loadProject(t, root)
	if _, _, err := LockAll(root, p.Graph); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	if len(p.Graph.SourceDrift) != 0 {
		t.Fatalf("clean after locking, got %v", p.Graph.SourceDrift)
	}
	w("proj/documents/x.md", "v2 says something else\n")
	p = loadProject(t, root)
	if p.Graph.SourceDrift["s"] != SrcChanged {
		t.Fatalf("drift not detected through a per-directory lock: %v", p.Graph.SourceDrift)
	}
}

// A file the user believes is protecting them must not be silently ignored.
func TestLegacyRootLockIsReported(t *testing.T) {
	root := driftProject(t, "v1\n")
	if err := os.MkdirAll(filepath.Join(root, ".kgraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, legacyLockRel),
		[]byte("# old\ndocuments/exhibit.md sha256:dead 3 2026-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range diags {
		if strings.Contains(d.Msg, "no longer read") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a stale root lock was silently ignored: %v", diags)
	}
}

// An index is a closed graph and its evidence should be too. `DocPathOf` only
// refuses a path escaping the project ROOT, so a `doc:` reaching into a sibling
// directory passed silently — which is how a living checklist from outside the
// matter became the source for ten facts and kept the drift signal permanently
// tripped.
func TestDocOutsideTheIndexIsReported(t *testing.T) {
	root := t.TempDir()
	idx := filepath.Join(root, "matter")
	if err := os.MkdirAll(filepath.Join(idx, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "checklists"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"matter/documents/deed.md", "checklists/todo.md"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	facts := "# f\n\n```kfacts\n" +
		"- id: s-inside\n  record: In the matter\n  doc: documents/deed.md\n  class: record\n" +
		"- id: s-outside\n  document: A checklist from elsewhere\n  doc: ../checklists/todo.md\n  class: document\n" +
		"```\n"
	writeLegacyFacts(t, idx, "f.kfacts.md", facts)
	g, _, err := ScanFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	ds := g.CheckIndexBoundary(root)
	var named []string
	for _, d := range ds {
		named = append(named, d.Msg)
		if d.Severity != SevWarn {
			t.Errorf("must WARN, not error — shared authority legitimately lives outside a matter: %v", d.Severity)
		}
	}
	if len(ds) != 1 {
		t.Fatalf("want exactly the outside source reported, got %d: %v", len(ds), named)
	}
	if !containsSub(named[0], "s-outside") {
		t.Errorf("wrong source reported: %s", named[0])
	}
	if containsSub(named[0], "s-inside") {
		t.Error("a document inside the index must not be reported")
	}
}

// LOCKING ONE SOURCE MUST NOT RE-LOCK THE REST.
//
// `kg source lock` was all-or-nothing, so recording a NEW document silently
// re-locked every `changed` one — and a `changed` source is a drift signal
// somebody is midway through investigating. It swallowed one twice in a single
// session and had to be restored from git by hand both times.
func TestLockingOneSourceLeavesOtherDriftAlone(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("documents/watched.md", "the version somebody is investigating")
	write("documents/fresh.md", "a document nobody has recorded yet")
	writeFactLog(t, root, "",
		"- id: s-watched\n  document: A watched exhibit\n  doc: documents/watched.md\n  class: document\n"+
			"- id: s-fresh\n  document: A new exhibit\n  doc: documents/fresh.md\n  class: document\n"+
			"- id: c-one\n  claim: Rests on the watched one\n  status: asserted\n  attested_by: s-watched\n"+
			"- id: c-two\n  claim: Rests on the new one\n  status: asserted\n  attested_by: s-fresh\n")

	g, _, err := ScanFactsIn(root, DefaultIndex)
	if err != nil {
		t.Fatal(err)
	}
	// Record both, then move one on disk so it reads as `changed`.
	if _, _, err := LockAll(root, g); err != nil {
		t.Fatal(err)
	}
	write("documents/watched.md", "EDITED — this is the drift under investigation")

	stateOf := func(id string) SourceState {
		t.Helper()
		locks, lerr := ReadLocks(root, g)
		if lerr != nil {
			t.Fatal(lerr)
		}
		sts, _ := g.SourceStatuses(root, locks)
		for _, st := range sts {
			if st.ID == id {
				return st.State
			}
		}
		t.Fatalf("no status for %s", id)
		return ""
	}
	if got := stateOf("s-watched"); got != SrcChanged {
		t.Fatalf("the edited document should read as changed, got %q", got)
	}

	// Lock ONLY the other one. The drift must survive.
	if _, _, err := LockAll(root, g, "s-fresh"); err != nil {
		t.Fatal(err)
	}
	if got := stateOf("s-watched"); got != SrcChanged {
		t.Errorf("locking s-fresh erased the drift on s-watched: now %q", got)
	}
}
