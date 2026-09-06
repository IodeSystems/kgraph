package kgraph

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTempStore(t *testing.T) Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// Everything an assertion carries has to survive the database, including the
// two fields that exist to be read by a person years later: `by` and `note`.
func TestTheStoreRoundTripsAnAssertion(t *testing.T) {
	st := openTempStore(t)
	want := Assertion{
		Op: OpAssert, ID: "s-deed", By: "carl", At: "2026-08-30T10:00:00Z",
		Fields: map[string]any{"record": "The 1984 deed", "doc": "deed.md", "class": "record"},
		Note:   "the auditor's certified copy, not the one inside Rowe's declaration",
	}
	if err := st.Append(want); err != nil {
		t.Fatal(err)
	}
	got, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d assertions", len(got))
	}
	g := got[0]
	if g.Op != want.Op || g.ID != want.ID || g.By != want.By || g.At != want.At {
		t.Errorf("header lost: %+v", g)
	}
	if g.Note != want.Note {
		t.Errorf("the note was lost: %q", g.Note)
	}
	for k, v := range want.Fields {
		if g.Fields[k] != v {
			t.Errorf("field %q is %v, want %v", k, g.Fields[k], v)
		}
	}
}

// Fold order is by `at` then `seq`, and the store returns it that way so the
// store and the fold cannot disagree about what "the log" means.
func TestTheStoreReturnsFoldOrder(t *testing.T) {
	st := openTempStore(t)
	// Appended out of order, and two sharing a timestamp so `seq` is what breaks
	// the tie.
	for _, a := range []Assertion{
		{Op: OpAssert, ID: "c", By: "carl", At: "2026-08-30T12:00:00Z", Fields: map[string]any{"claim": "third"}},
		{Op: OpAssert, ID: "a", By: "carl", At: "2026-08-30T10:00:00Z", Fields: map[string]any{"claim": "first"}},
		{Op: OpAssert, ID: "b", By: "carl", At: "2026-08-30T10:00:00Z", Fields: map[string]any{"claim": "second"}},
	} {
		if err := st.Append(a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range got {
		ids = append(ids, a.ID)
	}
	// a and b share a timestamp; a was appended first, so it keeps that order.
	if strings.Join(ids, ",") != "a,b,c" {
		t.Errorf("fold order is %v", ids)
	}
}

// THE EXPORT HAS TO BE A REAL PROJECTION. A store that cannot be rebuilt from
// its text is a store whose text is decoration — and the text is what lives in
// the corpus, so this is the property that makes the whole inversion safe.
func TestTheExportRebuildsTheStore(t *testing.T) {
	dir := t.TempDir()
	src, err := OpenStore(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	for _, a := range []Assertion{
		{Op: OpAssert, ID: "s-deed", By: "carl", At: "2026-08-30T10:00:00Z",
			Fields: map[string]any{"record": "The 1984 deed", "doc": "deed.md", "class": "record"}},
		{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T10:00:01Z",
			Fields: map[string]any{"claim": "The strip is held by Halloway", "status": "asserted",
				"attested_by": "s-deed"}, Note: "read off the deed itself"},
		{Op: OpWithdraw, ID: "c-one", By: "carl", At: "2026-09-02T09:00:00Z",
			Reason: "the deed describes the adjoining parcel"},
	} {
		if err := src.Append(a); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(dir, "facts.jsonl")
	n, err := ExportJSONL(src, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("exported %d lines", n)
	}

	dst, err := OpenStore(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := ImportJSONL(dst, out); err != nil {
		t.Fatal(err)
	}

	// Not "the same lines" — the same GRAPH. That is what a rebuild has to mean,
	// and SemHash is the only statement of it that downstream agrees with.
	fold := func(st Store) *Graph {
		t.Helper()
		log, lerr := st.All()
		if lerr != nil {
			t.Fatal(lerr)
		}
		doc, ds := FoldAssertions(legal, "facts.jsonl", log)
		if errs := Errors(ds); len(errs) > 0 {
			t.Fatalf("%v", errs)
		}
		g, _ := BuildWith([]*Doc{doc}, AttestSet{})
		return g
	}
	a, b := fold(src), fold(dst)
	if len(a.Nodes) != len(b.Nodes) {
		t.Fatalf("node counts differ: %d vs %d", len(a.Nodes), len(b.Nodes))
	}
	for id, want := range a.SemHash {
		if b.SemHash[id] != want {
			t.Errorf("%s differs after a round trip through the export", id)
		}
	}
	// And the prose survived, which is the field with no other home.
	back, _ := dst.All()
	var noted bool
	for _, x := range back {
		if strings.Contains(x.Note, "read off the deed") {
			noted = true
		}
	}
	if !noted {
		t.Error("the note did not survive the export")
	}
}

// An index name reaches a filename, so it may not reach out of the directory.
func TestStorePathRefusesAPathishIndexName(t *testing.T) {
	if _, err := StorePath("../../etc/passwd"); err == nil {
		t.Fatal("an index name containing a separator must be refused")
	}
	p, err := StorePath(DefaultIndex)
	if err != nil {
		t.Fatal(err)
	}
	// The default index gets a NAME, not a bare dot — a file called `.db` is
	// invisible in exactly the situation somebody is looking for it.
	if filepath.Base(p) != "default.db" {
		t.Errorf("the default index's store is %q", filepath.Base(p))
	}
	// And it is not in the corpus. That is the whole reason this function exists:
	// a live SQLite file in a Syncthing tree corrupts whether or not two machines
	// write it.
	if !strings.Contains(p, ".kgraph") {
		t.Errorf("the store is not under ~/.kgraph: %q", p)
	}
}

// The append-only rule, from the store's side: there is no update and no delete.
func TestTheStoreOnlyAppends(t *testing.T) {
	st := openTempStore(t)
	first := Assertion{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T10:00:00Z",
		Fields: map[string]any{"claim": "first", "status": "asserted"}}
	second := first
	second.At = "2026-08-30T11:00:00Z"
	second.Fields = map[string]any{"claim": "second", "status": "asserted"}
	for _, a := range []Assertion{first, second} {
		if err := st.Append(a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	// BOTH lines are there. Re-asserting an id does not overwrite the earlier
	// line — the fold decides what the node says, and the history of how it got
	// there is the thing the store exists to keep.
	if len(got) != 2 {
		t.Fatalf("re-asserting overwrote the earlier line: %d rows", len(got))
	}
	doc, _ := FoldAssertions(legal, "facts.jsonl", got)
	if doc.Nodes[0].Body != "second" {
		t.Errorf("the fold took the wrong line: %q", doc.Nodes[0].Body)
	}
}

// The store holds a live legal and medical corpus, and it is now the PRIMARY
// copy rather than a derived index that could be deleted and rebuilt. SQLite
// creates its file 0644 if left alone.
func TestTheFactStoreIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "facts.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Append(Assertion{Op: OpAssert, ID: "c-one", By: "carl",
		At: "2026-08-30T10:00:00Z", Fields: map[string]any{"claim": "x"}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("fact store mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("store directory mode = %o, want 700", perm)
	}
}

// ── WHERE the store is ─────────────────────────────────────────────────

// A ref resolves an ENGINE and a LOCATION, and the two failure shapes that are
// worth a name are `none` — which is an answer, not an error — and a DSN for an
// engine nothing wires yet, which must be refused rather than opened as a file
// with a colon in its name.
func TestStoreRefResolvesEngineAndLocation(t *testing.T) {
	dir := t.TempDir()

	// `none` is the sayable absence: no store, and no error either.
	st, err := OpenStoreRef(StoreRef{DSN: StoreNone})
	if err != nil {
		t.Fatalf("--store none is an answer, not a failure: %v", err)
	}
	if st != nil {
		t.Fatal("--store none opened a store")
	}
	if !(StoreRef{DSN: StoreNone}).IsNone() {
		t.Error("IsNone did not recognise its own constant")
	}

	// A bare path and an explicit `sqlite:` reach the same file.
	path := filepath.Join(dir, "beside-the-corpus.db")
	for _, dsn := range []string{path, "sqlite:" + path} {
		st, err := OpenStoreRef(StoreRef{DSN: dsn})
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		st.Close()
		if _, serr := os.Stat(path); serr != nil {
			t.Fatalf("%s did not reach %s", dsn, path)
		}
	}

	// DESIGNED FOR, NOT BUILT. Opening it as a filesystem path would create a
	// directory called `postgres:` and report success.
	if _, err := OpenStoreRef(StoreRef{DSN: "postgres://localhost/kg"}); err == nil {
		t.Fatal("a postgres DSN opened something")
	}

	// THE DISCRIMINATOR SCOPES A SHARED DATABASE. Two indexes in one file must
	// not see each other's log.
	shared := filepath.Join(dir, "workspace.db")
	a, err := OpenStoreIn(shared, "matter-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Append(Assertion{Op: OpAssert, ID: "c-a", By: "carl",
		At: "2026-09-01T10:00:00Z", Fields: map[string]any{"claim": "in a"}}); err != nil {
		t.Fatal(err)
	}
	a.Close()
	b, err := OpenStoreRef(StoreRef{DSN: shared, Index: "matter-b"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Append(Assertion{Op: OpAssert, ID: "c-b", By: "carl",
		At: "2026-09-01T10:00:00Z", Fields: map[string]any{"claim": "in b"}}); err != nil {
		t.Fatal(err)
	}
	got, err := b.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "c-b" {
		t.Fatalf("matter-b sees %d row(s) — the discriminator is not scoping reads: %+v",
			len(got), got)
	}
	// And the other index still holds exactly its own, with `seq` numbered per
	// index so one matter's volume never advances another's.
	a2, err := OpenStoreIn(shared, "matter-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	back, err := a2.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].ID != "c-a" {
		t.Fatalf("matter-a lost its own row: %+v", back)
	}
}

// THE SHADOWING FAILURE, stated as a test. `~/.kgraph/<index>.db` is keyed by
// index NAME and nothing else, so a corpus keeping its own store must be able to
// say so — otherwise it gets a graph loaded from whatever database happens to
// share its index name on that machine.
func TestTheCallersStoreWinsOverTheDefaultLocation(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "examples", root)

	// A default-location store for the SAME index name, holding somebody else's
	// facts. This is the developer's own database shadowing a committed fixture.
	home := t.TempDir()
	t.Setenv("HOME", home)
	shadowPath, err := StorePath("fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := OpenStore(shadowPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := shadow.Append(Assertion{Op: OpAssert, ID: "c-not-ours", By: "somebody",
		At: "2026-09-01T10:00:00Z", Fields: map[string]any{"claim": "a foreign matter"}}); err != nil {
		t.Fatal(err)
	}
	shadow.Close()

	// The bare entry point takes the policy, and the policy is what shadows.
	shadowed, _, err := LoadIn(root, "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shadowed.Graph.Lookup("c-not-ours"); !ok {
		t.Fatal("the default location did not shadow — this test is no longer testing anything")
	}

	// Named, it reads the store it was handed.
	mine, err := OpenStoreRef(StoreRef{DSN: filepath.Join(root, "fence-dispute", "facts.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Close()
	if _, err := ImportJSONL(mine, filepath.Join(root, "fence-dispute", "facts.jsonl")); err != nil {
		t.Fatal(err)
	}
	p, _, err := LoadInWith(root, "fence-dispute", mine)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Graph.Lookup("c-not-ours"); ok {
		t.Error("a named store still read the default location's database")
	}
	if _, ok := p.Graph.Lookup("s-photos"); !ok {
		t.Error("a named store did not read its own facts")
	}
}

// A NIL STORE MEANS LOG ONLY, and it has to be sayable: that is exactly what a
// corpus handed to somebody with no database is, and it was reachable before
// only by the file happening not to exist.
func TestANilStoreReadsTheExportAndNeverTheDatabase(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "examples", root)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := StorePath("fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Append(Assertion{Op: OpAssert, ID: "c-not-ours", By: "somebody",
		At: "2026-09-01T10:00:00Z", Fields: map[string]any{"claim": "a foreign matter"}}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	p, _, err := LoadInWith(root, "fence-dispute", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Graph.Lookup("c-not-ours"); ok {
		t.Error("a nil store went looking for a database anyway")
	}
	if _, ok := p.Graph.Lookup("s-photos"); !ok {
		t.Error("a nil store did not read the exported log")
	}
}

// AN EMPTY STORE FALLS THROUGH TO THE EXPORT. The table is append-only, so a log
// that has ever been written to cannot come back empty: empty means never
// written rather than emptied, and nothing is resurrected by reading the text.
// Answering "no facts" here instead is how a corpus that loads from its export
// reports itself EMPTY the moment any command creates the database as a side
// effect of opening it.
func TestAnEmptyStoreDoesNotHideTheExport(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "examples", root)
	empty, err := OpenStore(filepath.Join(t.TempDir(), "fence-dispute.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	p, _, err := LoadInWith(root, "fence-dispute", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Graph.Lookup("s-photos"); !ok {
		t.Error("an empty store hid the corpus's own exported log")
	}
}

// A STORE WRITTEN BEFORE `idx` EXISTED MUST NOT COME BACK EMPTY.
//
// This is the one way the discriminator goes wrong with no error in it. `ADD
// COLUMN idx TEXT NOT NULL DEFAULT ”` leaves every existing row under the EMPTY
// index; a scoped read of a named index then returns nothing and the corpus
// reports itself empty — no error, no missing file, just a graph with no nodes.
// So the migration STAMPS existing rows with the index the FILE was named for,
// and `IndexOfStorePath` is the single source of that derivation so the stamp
// and the read cannot disagree.
func TestAPreIdxStoreIsStampedWithItsFilesIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matter-x.db")

	// A store in the OLD shape: no idx column at all.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE assertion (
	  seq INTEGER NOT NULL PRIMARY KEY, op TEXT NOT NULL, id TEXT NOT NULL,
	  fields TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
	  into_id TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
	  by_who TEXT NOT NULL, at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO assertion (seq, op, id, fields, by_who, at) VALUES (1, 'assert', 'c-old', ?, 'carl', '2026-08-01T10:00:00Z')`,
		`{"claim":"written before the discriminator"}`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "c-old" {
		t.Fatalf("the pre-idx row is unreachable after migration: %+v — this is the "+
			"silent-empty failure, and it has no error in it", got)
	}
	// A new append lands under the same index and both are visible together.
	if err := st.Append(Assertion{Op: OpAssert, ID: "c-new", By: "carl",
		At: "2026-09-01T10:00:00Z", Fields: map[string]any{"claim": "after"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.All(); len(got) != 2 {
		t.Fatalf("old and new rows are in different indexes: %+v", got)
	}
}

// `kgraph_query` is the projection consumers asked for: what was asked, the
// answer on record, and whether it still holds — readable in SQL without
// linking kgraph, folding the log and re-evaluating in memory.
//
// It is safe to have where node/edge tables were not, and the difference is
// authorship: a pin is somebody's accepted answer, signed, not a second copy of
// the fold. The one computed column can lag the log but cannot disagree with it.
func TestTheStandingProjectionCarriesDriftReasonsNotJustAFlag(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "proj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w, ok := st.(QueryWriter)
	if !ok {
		t.Fatal("a sqlite store must be able to hold the projection")
	}
	rows := []StandingRow{
		{Name: "settled", Group: "g", Query: "claim", SetHash: "sha256:aa", Count: 3},
		{Name: "moved", Group: "g", Query: "claim[disputed]", SetHash: "sha256:bb",
			Count: 1, Invalidated: true, Drift: []string{"results-changed", "source-changed"}},
	}
	if err := w.WriteQueries(rows, "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	back, err := st.(interface{ ReadQueries() ([]StandingRow, error) }).ReadQueries()
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("wrote 2 rows, read %d", len(back))
	}
	// FIVE STATES, NOT A BOOLEAN. Collapsing never-asked, query-changed,
	// results-changed, dialect-changed and source-changed into one flag throws
	// away the only part a consumer can act on.
	moved := back[0]
	if moved.Name != "moved" {
		moved = back[1]
	}
	if !moved.Invalidated {
		t.Error("the moved query is not flagged")
	}
	if len(moved.Drift) != 2 || moved.Drift[0] != "results-changed" {
		t.Errorf("the drift reasons did not survive: %v", moved.Drift)
	}

	// A WHOLE-INDEX REPLACE. A standing query somebody DELETED must leave the
	// table, or a consumer keeps filtering on an answer to a question nobody
	// asks any more.
	if err := w.WriteQueries(rows[:1], "2026-09-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	back, _ = st.(interface{ ReadQueries() ([]StandingRow, error) }).ReadQueries()
	if len(back) != 1 || back[0].Name != "settled" {
		t.Errorf("a deleted query survived the rewrite: %+v", back)
	}
}
