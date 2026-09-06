package kgraph

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// pgDSN is the Postgres this test runs against, or "" to skip.
//
// SUPPLIED, never invented. A test that spins up its own database hides the
// thing worth knowing — whether the schema this package writes is accepted by a
// real server — behind a container that may not resemble one. Set
// KGRAPH_TEST_POSTGRES to a DSN and these run; leave it and they skip, loudly
// enough that a green suite is not mistaken for a tested engine.
func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("KGRAPH_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set KGRAPH_TEST_POSTGRES to a DSN to exercise the Postgres engine")
	}
	return dsn
}

// pgClean drops this test's rows so a shared database stays usable.
func pgFresh(t *testing.T, idx string) Store {
	t.Helper()
	st, err := OpenPostgres(pgDSN(t), idx)
	if err != nil {
		t.Fatalf("opening postgres: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if s, ok := st.(*sqlStore); ok {
		if _, err := s.db.Exec(s.rebind(`DELETE FROM assertion WHERE idx = ?`), idx); err != nil {
			t.Fatalf("clearing %s: %v", idx, err)
		}
	}
	return st
}

// THE SAME LOG, THROUGH A DIFFERENT ENGINE. The fold, the ops and the graph do
// not change: the store holds the LOG rather than the graph, which is why
// designing for Postgres was nearly free.
func TestPostgresHoldsTheSameLog(t *testing.T) {
	st := pgFresh(t, "engine-test")
	for i, body := range []string{"first", "second", "third"} {
		if err := st.Append(Assertion{Op: OpAssert, ID: fmt.Sprintf("c-%d", i),
			By: "carl", At: fmt.Sprintf("2026-09-02T10:00:0%dZ", i),
			Fields: map[string]any{"claim": body, "status": "asserted"}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "c-0" || got[2].ID != "c-2" {
		t.Fatalf("the log came back wrong: %+v", got)
	}
	// And it FOLDS — the placeholder rebinding must not have mangled a field.
	doc, ds := FoldAssertions(legal, "facts.jsonl", got)
	if errs := Errors(ds); len(errs) > 0 {
		t.Fatalf("the postgres log does not fold: %v", errs)
	}
	if len(doc.Nodes) != 3 {
		t.Fatalf("folded to %d nodes", len(doc.Nodes))
	}
}

// THE DISCRIMINATOR IS WHAT MAKES ONE DATABASE SERVE A WORKSPACE. Two matters in
// one Postgres must not see each other's log — and unlike a file store there is
// no filename to derive the index from, so being told is the only way.
func TestPostgresScopesByIndex(t *testing.T) {
	a := pgFresh(t, "matter-a")
	b := pgFresh(t, "matter-b")
	if err := a.Append(Assertion{Op: OpAssert, ID: "c-a", By: "carl",
		At: "2026-09-02T10:00:00Z", Fields: map[string]any{"claim": "in a"}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Assertion{Op: OpAssert, ID: "c-b", By: "carl",
		At: "2026-09-02T10:00:00Z", Fields: map[string]any{"claim": "in b"}}); err != nil {
		t.Fatal(err)
	}
	rows, err := b.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "c-b" {
		t.Fatalf("matter-b sees %d row(s) — the discriminator is not scoping: %+v", len(rows), rows)
	}
}

// `MAX(seq)+1` IS A RACE HERE, and it is not one under SQLite: that store caps
// itself at one connection, which is what made the read-then-write exact. A
// shared Postgres has concurrent writers, two appends read the same MAX, and
// losing an assertion is the one outcome an append-only log may not have.
//
// PRIMARY KEY (idx, seq) turns the loss into a constraint violation, and Append
// turns the violation into a re-read.
func TestPostgresConcurrentAppendsAllSurvive(t *testing.T) {
	dsn := pgDSN(t)
	const idx, writers = "race-test", 8
	if st, err := OpenPostgres(dsn, idx); err == nil {
		if s, ok := st.(*sqlStore); ok {
			s.db.Exec(s.rebind(`DELETE FROM assertion WHERE idx = ?`), idx)
		}
		st.Close()
	}
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			st, err := OpenPostgres(dsn, idx)
			if err != nil {
				errs[n] = err
				return
			}
			defer st.Close()
			errs[n] = st.Append(Assertion{Op: OpAssert, ID: fmt.Sprintf("c-%d", n),
				By: "carl", At: "2026-09-02T10:00:00Z",
				Fields: map[string]any{"claim": fmt.Sprintf("written by %d", n)}})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d lost its assertion: %v", i, err)
		}
	}
	st, err := OpenPostgres(dsn, idx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != writers {
		t.Fatalf("%d concurrent appends left %d rows — an append-only log lost one",
			writers, len(rows))
	}
	seqs := map[string]bool{}
	for _, r := range rows {
		if seqs[r.ID] {
			t.Errorf("%s appears twice", r.ID)
		}
		seqs[r.ID] = true
	}
}

// The placeholder is the one engine-specific thing in every query.
func TestPgRebindNumbersPlaceholdersInOrder(t *testing.T) {
	got := pgRebind("INSERT INTO t (a, b, c) VALUES (?, ?, ?) WHERE d = ?")
	want := "INSERT INTO t (a, b, c) VALUES ($1, $2, $3) WHERE d = $4"
	if got != want {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "?") {
		t.Error("a placeholder survived")
	}
}

// A whole corpus through Postgres: import the fixture's exported log, load the
// graph from it, and compare to the same corpus loaded from its text.
// A WHOLE CORPUS THROUGH POSTGRES, compared to the same corpus loaded from its
// text. This is the proof that the engine swap is transparent: the store holds
// the LOG, the fold turns it into a graph, and neither knows which database it
// came out of.
func TestPostgresRoundTripsAFixtureCorpus(t *testing.T) {
	st := pgFresh(t, "fence-dispute")
	n, err := ImportJSONL(st, "examples/fence-dispute/facts.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	viaText, _, err := LoadInWith("examples", "fence-dispute", nil)
	if err != nil {
		t.Fatal(err)
	}
	viaPg, _, err := LoadInWith("examples", "fence-dispute", st)
	if err != nil {
		t.Fatal(err)
	}
	if len(viaPg.Graph.Nodes) != len(viaText.Graph.Nodes) {
		t.Fatalf("postgres gave %d nodes, the text gave %d",
			len(viaPg.Graph.Nodes), len(viaText.Graph.Nodes))
	}
	for id, want := range viaText.Graph.SemHash {
		if viaPg.Graph.SemHash[id] != want {
			t.Fatalf("%s differs between engines", id)
		}
	}
	t.Logf("%d assertions → %d nodes, identical by SemHash through Postgres", n, len(viaPg.Graph.Nodes))
}
