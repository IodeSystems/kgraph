package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// standingCorpus is two asserted claims, one of which cites a source.
func standingCorpus(t *testing.T, twoAsserted bool) (string, *Graph) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "deed.md"), []byte("a deed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := "proposed"
	if twoAsserted {
		second = "asserted"
	}
	writeFactLog(t, root, "",
		"- id: s-deed\n  record: The deed\n  doc: deed.md\n  class: record\n"+
			"- id: c-one\n  claim: The strip is held by Halloway\n  status: asserted\n  attested_by: s-deed\n"+
			"- id: c-two\n  claim: The easement is terminable\n  status: "+second+"\n")
	p, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, p.Graph
}

func askOnce(t *testing.T, g *Graph, st *Standing) *Answer {
	t.Helper()
	a, err := g.Ask(st, Env{Now: "2026-08-29"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// The four states, each on its own. They are separated because they want
// different responses: an edited question is not a moved corpus, and a moved
// document under unchanged rows is neither.
func TestAskReportsWhyTheAnswerMoved(t *testing.T) {
	_, g := standingCorpus(t, false)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}

	// NEVER-ASKED is not an empty result and must not read as a settled question.
	a := askOnce(t, g, st)
	if !contains(a.Drift, "never-asked") {
		t.Fatalf("an unasked query must say so, got %v", a.Drift)
	}
	if len(a.Rows) != 1 || a.Rows[0] != "c-one" {
		t.Fatalf("rows: %v", a.Rows)
	}
	st.Acknowledge(a, "2026-08-29", "carl")
	if a2 := askOnce(t, g, st); a2.Stale() {
		t.Fatalf("straight after acknowledging, nothing has moved: %v", a2.Drift)
	}

	// RESULTS-CHANGED: same question, the corpus moved.
	_, moved := standingCorpus(t, true)
	a = askOnce(t, moved, st)
	if !contains(a.Drift, "results-changed") {
		t.Fatalf("want results-changed, got %v", a.Drift)
	}
	if contains(a.Drift, "query-changed") {
		t.Error("the question was not edited")
	}
	// And it says WHAT moved, not merely that something did.
	if a.Delta.Empty() {
		t.Error("a moved result must carry a delta")
	}

	// QUERY-CHANGED: the question was edited, reported on its own so it does not
	// read as the corpus moving.
	edited := &Standing{Name: "held", Query: "claim", QueryHash: st.QueryHash, Pin: st.Pin}
	a = askOnce(t, g, edited)
	if !contains(a.Drift, "query-changed") {
		t.Fatalf("want query-changed, got %v", a.Drift)
	}
}

// THE CASE NOTHING ELSE CAN SEE. Every row still matches, every sem_hash is
// identical, and a document underneath was re-scanned. `results-changed`
// structurally cannot report this, which is why source drift is tracked apart
// from the semantic hash.
func TestSourceDriftIsReportedWithUnchangedRows(t *testing.T) {
	_, g := standingCorpus(t, false)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}
	a := askOnce(t, g, st)
	st.Acknowledge(a, "2026-08-29", "carl")

	g.SourceDrift = map[string]SourceState{"s-deed": "changed"}
	a = askOnce(t, g, st)
	if !contains(a.Drift, "source-changed") {
		t.Fatalf("want source-changed, got %v", a.Drift)
	}
	if contains(a.Drift, "results-changed") {
		t.Error("no row moved; reporting the set as changed would send somebody looking for one")
	}
	if a.Pin.SetHash != st.Pin.SetHash {
		t.Error("the set hash must be identical — that is the whole point of this case")
	}

	// Drift on a source this query never rested on must NOT flag it. A matter
	// re-OCRs unrelated exhibits constantly.
	g.SourceDrift = map[string]SourceState{"s-elsewhere": "changed"}
	if a := askOnce(t, g, st); a.Stale() {
		t.Errorf("drift under an uncited source must not flag: %v", a.Drift)
	}
}

// Acknowledging is separate from asking and never automatic. An answer that
// re-pins itself the moment it is read reports unchanged forever — the delta
// exists to be seen by somebody, and a tool that clears its own alarm has none.
func TestAskingDoesNotAcknowledge(t *testing.T) {
	_, g := standingCorpus(t, false)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}
	for i := 0; i < 3; i++ {
		if a := askOnce(t, g, st); !contains(a.Drift, "never-asked") {
			t.Fatalf("ask %d cleared its own alarm: %v", i, a.Drift)
		}
	}
	if st.Pin != nil {
		t.Fatal("Ask wrote a pin")
	}
}

// The file is the record of what was asked and what came back, and it must
// survive a round trip exactly — a baseline that changes shape on read is not a
// baseline.
func TestStandingRoundTrips(t *testing.T) {
	root, g := standingCorpus(t, false)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}
	st.Acknowledge(askOnce(t, g, st), "2026-08-29", "carl")
	set := StandingSet{"held": st}
	if err := WriteStanding(root, "", set); err != nil {
		t.Fatal(err)
	}
	back, err := ReadStanding(root, "")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := back["held"]
	if !ok {
		t.Fatal("the query did not survive")
	}
	if got.Query != st.Query || got.QueryHash != st.QueryHash || got.By != "carl" {
		t.Fatalf("round trip lost something: %+v", got)
	}
	if got.Pin == nil || got.Pin.SetHash != st.Pin.SetHash {
		t.Fatal("the answer did not survive")
	}
	// And re-asking against the loaded copy reports nothing moved.
	if a := askOnce(t, g, got); a.Stale() {
		t.Fatalf("a round-tripped baseline must still match: %v", a.Drift)
	}
	// Writing an empty set removes the file rather than leaving one that implies
	// the corpus was checked and had nothing.
	if err := WriteStanding(root, "", StandingSet{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, standingName)); !os.IsNotExist(err) {
		t.Error("an empty set must not leave a file behind")
	}
}

// A malformed store is an error, not a silent empty set: resolving to "nothing
// was ever asked" would report every standing query as new and lose every
// baseline in one read.
func TestAMalformedStandingFileIsAnError(t *testing.T) {
	root := t.TempDir()
	for _, body := range []string{
		"- name: held\n",   // no query
		"- query: claim\n", // no name
		"- name: a\n  query: claim\n- name: a\n  query: claim[status=asserted]\n", // duplicate
	} {
		if err := os.WriteFile(filepath.Join(root, standingName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadStanding(root, ""); err == nil {
			t.Errorf("must be refused: %q", body)
		}
	}
}

// The cache exists to make asking cheap, and the invalidation is deliberately
// asymmetric: a node the result does not mention may still BELONG in it now, and
// no amount of looking at the old rows can discover that.
func TestTheResultCacheIsScrappedConservatively(t *testing.T) {
	_, g := standingCorpus(t, false)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}
	askOnce(t, g, st)
	if g.results == nil || len(g.results.rows) != 1 {
		t.Fatal("the answer was not cached")
	}

	// A node the cached result MENTIONS drops that entry.
	g.Invalidate("c-one")
	if len(g.results.rows) != 0 {
		t.Error("a change to a row in the result must scrap it")
	}

	askOnce(t, g, st)
	// A known node the result does not mention leaves it alone.
	g.Invalidate("c-two")
	if len(g.results.rows) != 1 {
		t.Error("a change elsewhere in the graph need not scrap an unrelated result")
	}

	// An UNKNOWN id scraps everything: it may be a node that now matches, and the
	// old rows cannot say. Wrong in the cheap direction on purpose.
	g.Invalidate("c-three")
	if len(g.results.rows) != 0 {
		t.Error("an unknown node must scrap the cache — it could match anything")
	}

	// A scratch graph must never answer from the real one's cache.
	askOnce(t, g, st)
	if c := g.Clone(); c.results != nil {
		t.Error("Clone carried the cache into a scratch graph")
	}
}

// `@now` makes one query text two questions on two days. A cache that ignored
// the clock would answer a limitations query with yesterday's window.
func TestTheCacheIsKeyedByTheClock(t *testing.T) {
	_, g := standingCorpus(t, false)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}
	if _, err := g.Ask(st, Env{Now: "2026-08-29"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Ask(st, Env{Now: "2027-01-01"}); err != nil {
		t.Fatal(err)
	}
	if len(g.results.rows) != 2 {
		t.Fatalf("want one cache entry per clock, got %d", len(g.results.rows))
	}
}

// Two spellings that resolve identically today are still two questions, and one
// may stop being the other when the evaluator grows. Only whitespace is
// normalised, because the DSL has none that is significant.
func TestQueryHashNormalisesWhitespaceAndNothingElse(t *testing.T) {
	if QueryHashOf("claim[status=asserted]") != QueryHashOf("  claim[status=asserted]\n") {
		t.Error("whitespace must not be a different question")
	}
	if QueryHashOf("claim[status=asserted]") == QueryHashOf("claim[status = asserted]") {
		t.Log("note: inner spacing normalises to the same question")
	}
	if QueryHashOf("claim") == QueryHashOf("event") {
		t.Fatal("different questions must not share a hash")
	}
}

func TestAskRefusesAQueryThatDoesNotParse(t *testing.T) {
	_, g := standingCorpus(t, false)
	_, err := g.Ask(&Standing{Name: "bad", Query: "claim[["}, Env{Now: "2026-08-29"})
	if err == nil {
		t.Fatal("a query that does not parse must be an error, not an empty answer")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("the error must name the standing query, got %q", err)
	}
}

// A GROUP IS WHAT A SPEC WAS, and this pins that the two set-shaped checks
// survive the retirement of `*.kgraph.md`.
//
// `CheckSplitContradiction` reports that a set carries one end of a correction
// and NO OTHER SET BESIDE IT carries the other — a statement about a group. On
// the live corpus 119 of 619 warnings are that check, so a flat standing query
// with no notion of "beside it" would have dropped a fifth of what the corpus
// says, quietly. `Standing.Group` is what keeps it.
func TestAGroupOfStandingQueriesChecksLikeASpecDid(t *testing.T) {
	p, _, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Standing) == 0 {
		t.Fatal("no standing queries — this test is checking nothing")
	}

	// Grouped as authored: siblings can see each other, so only genuinely
	// one-sided sets are reported.
	grouped := len(p.CheckSplitContradiction())

	// Ungrouped: every query becomes a document of one, so every contradiction
	// with one end in the set is reported. It must be strictly noisier — that
	// difference IS the value of the field.
	for _, st := range p.Standing {
		st.Group = ""
	}
	alone := len(p.CheckSplitContradiction())
	if alone <= grouped {
		t.Errorf("grouping changed nothing: %d grouped vs %d ungrouped — "+
			"either the field is not reaching the check, or the fixture no longer "+
			"has two queries in a group that cover both ends of a correction",
			grouped, alone)
	}
}

// The proportion check's threshold rises from 75%% to 90%% when a query states
// its own purpose, so PURPOSE MUST BE THE QUERY'S, never the group's.
//
// The conversion first copied each spec's DOCUMENT purpose onto every one of its
// queries, which silently raised the bar for all of them: an 87% set stopped
// being flagged. Caught by comparing findings before and after, not by any test
// then existing.
func TestAGroupsPurposeIsNotEveryQuerysPurpose(t *testing.T) {
	p, _, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	before := len(p.CheckSetProportion())
	if before == 0 {
		t.Fatal("no proportion warnings in the fixture — this test is checking nothing")
	}
	for _, st := range p.Standing {
		st.Purpose = "a document-level purpose, wrongly copied onto every member"
	}
	if after := len(p.CheckSetProportion()); after >= before {
		t.Errorf("a purpose on every query did not relax the check (%d → %d) — "+
			"the threshold is not reading Purpose", before, after)
	}
}

// contains is the small helper the drift assertions read with. It lived in
// `spec_test.go` and outlived it — the file went with `*.kgraph.md`, its users
// did not.
func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
