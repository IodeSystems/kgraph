package kgraph

import (
	"strings"
	"testing"
)

// billingGraph is the other corpus. Quantities and derived dates live there —
// fence-dispute has no `value:` and no `= expr` date — so a test for those must
// load it rather than assert against a graph that cannot express them.
func billingGraph(t *testing.T) *Graph {
	t.Helper()
	g, diags, err := ScanFactsIn("examples", "clinic-billing")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Severity == SevError {
			t.Fatalf("fixture has errors: %v", diags)
		}
	}
	return g
}

func fixtureGraph(t *testing.T) *Graph {
	t.Helper()
	g, diags, err := ScanFactsIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatalf("fixture errors: %v", errs)
	}
	return g
}

func run(t *testing.T, g *Graph, src string) []string {
	t.Helper()
	q, err := ParseQuery(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	ids, err := g.Eval(q, Env{Now: "2026-07-25"})
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return ids
}

func has(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestSpecQueriesParse was deleted with the spec fixtures (2026-09-01). It
// checked that every query in `examples/*.kgraph.md` PARSES;
// `TestNoDeclaredQueryResolvesEmpty` parses every declared query, resolves it,
// requires rows, and refuses to pass having checked nothing — strictly more, on
// the declarations that now exist.

func TestKindAndPredicateFilters(t *testing.T) {
	g := fixtureGraph(t)
	ids := run(t, g, `question[status=open]`)
	if !has(ids, "q-geometry-proof") || !has(ids, "q-photo-dates") {
		t.Fatalf("open questions missing: %v", ids)
	}
	for _, id := range ids {
		if g.Nodes[id].Kind != KQuestion || g.Nodes[id].Status != SOpen {
			t.Fatalf("%s leaked into question[status=open]", id)
		}
	}
	alt := run(t, g, `(claim|action)[status=withdrawn]`)
	if !has(alt, "mediation-in-april") || !has(alt, "a-serve-subpoena") {
		t.Fatalf("kind alternation missed cross-project results: %v", alt)
	}
}

func TestTransitiveClosureAndDirection(t *testing.T) {
	g := fixtureGraph(t)
	// file-counterclaims requires retain-counsel requires g-live-leads
	direct := run(t, g, `action -requires-> #g-counsel-leads`)
	if !has(direct, "a-conflict-check") || has(direct, "a-amend-answer") {
		t.Fatalf("one-hop should reach only retain-counsel: %v", direct)
	}
	star := run(t, g, `action -requires*-> #g-counsel-leads`)
	if !has(star, "a-conflict-check") || !has(star, "a-amend-answer") {
		t.Fatalf("closure should reach both: %v", star)
	}
	inv := run(t, g, `entity <-member_of- #g-counsel-leads`)
	if len(inv) != 0 {
		t.Fatalf("inverse direction is wrong way round: %v", inv)
	}
	fwd := run(t, g, `entity -member_of-> #g-counsel-leads`)
	if len(fwd) != 3 {
		t.Fatalf("want 3 candidate firms, got %v", fwd)
	}
}

// `while` scopes a theory to a branch we are not in. It must never surface as
// live work — that is the whole point of the qualifier.
func TestWhileGatesLiveQueries(t *testing.T) {
	g := fixtureGraph(t)
	// Looked up, not indexed: a missing node used to panic here, which hid the
	// "fixture changed" message this guard exists to print.
	premise, ok := g.Lookup("q-geometry-proof")
	if !ok || premise.Status != SOpen {
		t.Fatal("fixture changed: q-geometry-proof should be an open question")
	}
	live := run(t, g, `claim @now`)
	if has(live, "reformation") {
		t.Fatal("a theory scoped to an unresolved premise surfaced as live")
	}
	all := run(t, g, `claim`)
	if !has(all, "reformation") {
		t.Fatal("without @now the theory should still be visible")
	}
}

func TestTemporalWindow(t *testing.T) {
	g := fixtureGraph(t)
	// dace is counsel of record until 2026-02-11, when they withdraw.
	q, _ := ParseQuery(`entity @now`)
	before, _ := g.Eval(q, Env{Now: "2026-01-05"})
	after, _ := g.Eval(q, Env{Now: "2026-07-25"})
	if !has(before, "dace") {
		t.Fatal("window should be open on 01-05")
	}
	if has(after, "dace") {
		t.Fatal("window should be closed on 07-25")
	}
}

func TestSetOperations(t *testing.T) {
	g := fixtureGraph(t)
	all := run(t, g, `claim`)
	asserted := run(t, g, `claim[status=asserted]`)
	notAsserted := run(t, g, `claim & !claim[status=asserted]`)
	if len(asserted)+len(notAsserted) != len(all) {
		t.Fatalf("complement is not total: %d + %d != %d", len(asserted), len(notAsserted), len(all))
	}
	union := run(t, g, `claim[status=withdrawn] | claim[status=false]`)
	if !has(union, "mediation-in-april") || !has(union, "adverse-possession") {
		t.Fatalf("union missed a term: %v", union)
	}
}

func TestNamedQueryRefAndRecursionGuard(t *testing.T) {
	g := fixtureGraph(t)
	blockers, _ := ParseQuery(`question[status=open]`)
	env := Env{Now: "2026-07-25", Named: map[string]Query{"blockers": blockers}}

	q, _ := ParseQuery(`@blockers`)
	ids, err := g.Eval(q, env)
	if err != nil || len(ids) == 0 {
		t.Fatalf("named ref failed: %v %v", ids, err)
	}
	self, _ := ParseQuery(`@loop`)
	env.Named["loop"] = self
	if _, err := g.Eval(self, env); err == nil || !strings.Contains(err.Error(), "recursive") {
		t.Fatalf("want a recursion error, got %v", err)
	}
}

// Sorting must be declared and stable, and a missing key must sort last so an
// absent date never masquerades as the earliest.
func TestSortIsDeclaredAndMissingSortsLast(t *testing.T) {
	g := fixtureGraph(t)
	ids := run(t, g, `entity -member_of-> #g-counsel-leads sort valid_from, id`)
	var last string
	for _, id := range ids {
		vf := g.Nodes[id].ValidFrom
		if last != "" && vf < last {
			t.Fatalf("not sorted by valid_from: %v", ids)
		}
		last = vf
	}
	def := run(t, g, `entity -member_of-> #g-counsel-leads`)
	for i := 1; i < len(def); i++ {
		if def[i-1] > def[i] {
			t.Fatalf("default sort must be id: %v", def)
		}
	}
}

// Group satisfaction is what documents render: "0/3 satisfied, blocked on …".
func TestGroupAggregates(t *testing.T) {
	g := fixtureGraph(t)
	ready := g.GroupStat("g-before-mediation", "2026-07-25") // group: all
	if ready.Total != 3 {
		t.Fatalf("g-before-mediation has three members: %+v", ready)
	}
	leads := g.GroupStat("g-counsel-leads", "2026-07-25") // group: ">= 1"
	if !leads.OK || leads.Satisfied == 0 {
		t.Fatalf("g-counsel-leads has asserted members and needs >= 1: %+v", leads)
	}
	// The bag's cardinality is what prose commits to, so it is pinned.
	if leads.Total != 3 {
		t.Fatalf("want 3 members, got %d", leads.Total)
	}
}

func TestComputedValue(t *testing.T) {
	g := billingGraph(t)
	billed, ok := g.ResolveValue("billed-amount")
	if !ok || billed != 1240.00 {
		t.Fatalf("billed: %v %v", billed, ok)
	}
	allowed, ok := g.ResolveValue("allowed-amount")
	if !ok || allowed != 380.00 {
		t.Fatalf("allowed: %v %v", allowed, ok)
	}
	// balance is `= billed-amount.value - allowed-amount.value`, so it must track
	// the operands rather than carry a number somebody typed.
	bal, ok := g.ResolveValue("balance")
	if !ok || bal < 859.99 || bal > 860.01 {
		t.Fatalf("balance should be computed as 860.00, got %v %v", bal, ok)
	}
	if bal != billed-allowed {
		t.Fatalf("computed value drifted from its operands: %v != %v", bal, billed-allowed)
	}
}

// A derived deadline moves when its basis moves; a literal would not.
func TestDerivedDates(t *testing.T) {
	g := billingGraph(t)
	got, ok := g.ResolveAt("d-appeal-deadline") // = e-eob-issued.at + 180d, EOB 2026-01-22
	if !ok || got != "2026-07-21" {
		t.Fatalf("appeal deadline: got %q ok=%v, want 2026-07-21", got, ok)
	}
	coll, ok := g.ResolveAt("d-collections") // = d-appeal-deadline.at + 30d
	if !ok || coll != "2026-08-20" {
		t.Fatalf("collections: got %q, want 2026-08-20", coll)
	}
	warn, ok := g.ResolveAt("d-escalate-warning") // = d-collections.at - 14d, chained off a DERIVED date
	if !ok || warn != "2026-08-06" {
		t.Fatalf("escalate warning: got %q, want 2026-08-06", warn)
	}
	// and it is queryable like an authored date
	ids := run(t, g, `event[at>2026-07-01]`)
	if !has(ids, "d-appeal-deadline") {
		t.Fatalf("derived date not comparable in a predicate: %v", ids)
	}
}

func TestFullTextAndIDAtom(t *testing.T) {
	g := billingGraph(t)
	if ids := run(t, g, `claim ~ "out-of-network"`); len(ids) == 0 {
		t.Fatal("full-text found nothing")
	}
	ids := run(t, g, `#plan-is-ppo`)
	if len(ids) != 1 || ids[0] != "plan-is-ppo" {
		t.Fatalf("id atom: %v", ids)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		`claim -nosuchedge-> #x`,
		`claim -requires`,
		`claim[`,
		`<-requires- claim`,
		`claim ~ unquoted`,
	} {
		if _, err := ParseQuery(bad); err == nil {
			t.Errorf("expected a parse error for %q", bad)
		}
	}
}

func TestValidateCatchesStaleVocabulary(t *testing.T) {
	for _, bad := range []struct{ q, want string }{
		{`claim[status=retracted]`, "unknown status"},
		{`event[at=computed]`, ""},
		{`claim[nosuchfield=1]`, "unknown field"},
		{`claim sort nosuchfield`, "unknown sort field"},
		{`source[class=gossip]`, "unknown source class"},
	} {
		if bad.want == "" {
			continue
		}
		q, err := ParseQuery(bad.q)
		if err != nil {
			t.Fatalf("%q should parse: %v", bad.q, err)
		}
		errs := ValidateQuery(q)
		if len(errs) == 0 || !strings.Contains(errs[0].Error(), bad.want) {
			t.Errorf("%q: want %q, got %v", bad.q, bad.want, errs)
		}
	}
}

// An edge carries its own validity, and an expired requirement must truncate
// the path — otherwise a closure walks a route that no longer exists. Tested on
// a synthetic pair so node-level `while` gating cannot mask the edge check.
func TestEdgeTemporalTruncatesPaths(t *testing.T) {
	doc, diags := parseOne(t, `
- id: gate
  claim: The gate
  status: asserted
- id: dep
  action: Depends on the gate until it lapses
  status: open
  requires:
    - id: gate
      valid_until: 2026-11-23
`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	g, _ := Build([]*Doc{doc})
	if e := findEdge(t, g, "dep", "gate", ERequires); e.ValidUntil != "2026-11-23" {
		t.Fatalf("long-form edge qualifier not parsed: %+v", e)
	}
	q, err := ParseQuery(`action -requires-> #gate @now`)
	if err != nil {
		t.Fatal(err)
	}
	if open, _ := g.Eval(q, Env{Now: "2026-09-01"}); !has(open, "dep") {
		t.Fatal("edge should be live before its valid_until")
	}
	if closed, _ := g.Eval(q, Env{Now: "2026-12-01"}); has(closed, "dep") {
		t.Fatal("closure traversed an edge that had lapsed")
	}
	if untimed, _ := g.Eval(q, Env{}); !has(untimed, "dep") {
		t.Fatal("an untimed query must still see the whole graph")
	}
}

// The real fixture carries a long-form edge; parsing it is the regression guard.
func TestFixtureUsesLongFormEdge(t *testing.T) {
	g := fixtureGraph(t)
	e := findEdge(t, g, "a-amend-answer", "a-conflict-check", ERequires)
	if e.ValidUntil != "2026-09-30" {
		t.Fatalf("fixture long-form edge lost its window: %+v", e)
	}
	// and the action stays out of live work, because its branch is unrealized:
	// a-draft-easement exists only `while opt-settle`, which is merely proposed.
	if live := run(t, g, `action @now`); has(live, "a-draft-easement") {
		t.Fatal("work scoped to an unrealized branch surfaced as live")
	}
}

// A lapsed membership changes a group's cardinality, which is exactly what
// generated prose commits to.
func TestEdgeValidityAffectsGroupCardinality(t *testing.T) {
	g := fixtureGraph(t)
	before := g.GroupStat("g-counsel-leads", "2026-07-25")
	for i := range g.Edges {
		if g.Edges[i].Type == EMemberOf && g.Edges[i].Src == "holt-pike" {
			g.Edges[i].ValidUntil = "2026-08-01"
		}
	}
	g2, _ := Build([]*Doc{{Path: "x", Nodes: nodesOf(g), Edges: g.Edges}})
	after := g2.GroupStat("g-counsel-leads", "2026-09-01")
	if after.Total != before.Total-1 {
		t.Fatalf("lapsed membership should drop cardinality: %d -> %d", before.Total, after.Total)
	}
}

// Both declared branches carry scoped work, so neither may be reported unplanned.
func TestDeclaredOptionsAreBothPlanned(t *testing.T) {
	g, diags, err := ScanFactsIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	_ = g
	for _, d := range diags {
		if strings.Contains(d.Msg, "no plan for the branch") {
			t.Errorf("both branches are planned, but got: %v", d)
		}
	}
}

func findEdge(t *testing.T, g *Graph, src, dst string, typ EdgeType) Edge {
	t.Helper()
	for _, e := range g.Edges {
		if e.Src == src && e.Dst == dst && e.Type == typ {
			return e
		}
	}
	t.Fatalf("edge %s -%s-> %s not found", src, typ, dst)
	return Edge{}
}

func nodesOf(g *Graph) []Node {
	out := make([]Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, *n)
	}
	return out
}

// A DECLARED QUERY that resolves to nothing is almost always a reversed hop or a
// stale id, and it fails silently. Parsing and schema validation both pass a
// reversed hop; only resolution catches it.
//
// Renamed from `TestNoSpecQueryResolvesEmpty` and widened to cover STANDING
// QUERIES as well as specs (2026-09-01). Specs are retiring, and this test is
// named in `CLAUDE.md` as the highest-value one in the repo — its value is "a
// declared query must return rows", which is a property of the declaration and
// not of the file it arrived in. Repointed BEFORE the conversion rather than
// after, so the conversion runs covered; when specs go, the spec half is deleted
// and the test keeps working.
func TestNoDeclaredQueryResolvesEmpty(t *testing.T) {
	// Per index, not one graph for the whole tree. A spec resolves against the
	// index that owns it, so checking a clinic-billing query against the
	// fence-dispute graph would report every one of them empty — and the real
	// signal, a hop that resolves to nothing in its OWN corpus, would be lost in
	// the noise.
	indexes, err := Indexes("examples")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, idx := range indexes {
		g, diags, err := ScanFactsIn("examples", idx)
		if err != nil {
			t.Fatal(err)
		}
		if errs := Errors(diags); len(errs) > 0 {
			t.Fatalf("index %q: %v", idx, errs)
		}
		// Standing queries are the only declarations now — `*.kgraph.md` retired
		// 2026-09-01 and the spec half of this test went with the fixtures.
		set, serr := ReadStanding("examples", IndexDirOf(g))
		if serr != nil {
			t.Fatalf("index %q: %v", idx, serr)
		}
		for name, st := range set {
			q, perr := ParseQuery(st.Query)
			if perr != nil {
				t.Errorf("standing %q: %v", name, perr)
				continue
			}
			ids, eerr := g.Eval(q, Env{Now: "2026-07-25"})
			if eerr != nil {
				t.Errorf("standing %q: %v", name, eerr)
				continue
			}
			if len(ids) == 0 {
				t.Errorf("standing query %q resolves to 0 rows: %s", name, st.Query)
			}
			checked++
		}
	}
	// A TEST THAT CHECKS NOTHING PASSES, which is the same silent-empty failure
	// this test exists to catch, one level up. It becomes reachable during the
	// spec retirement: delete the specs, do not populate `standing.yaml`, and
	// this goes green over an empty corpus.
	if checked == 0 {
		t.Fatal("no declared queries were resolved — this test checked nothing")
	}
}

// The same identifier can denote different things in different periods. In the
// real corpus P74736 was the pre-2021 parent parcel and is now Bramble's Lot I —
// a confusion that already put a wrong line into the tracker.
func TestAliasResolvesByDate(t *testing.T) {
	g := fixtureGraph(t)
	before, err := g.ResolveRef("P-1180", "2020-06-01")
	if err != nil || before != "parent-parcel" {
		t.Fatalf("pre-split P-1180 should be the parent parcel, got %q %v", before, err)
	}
	after, err := g.ResolveRef("P-1180", "2025-06-01")
	if err != nil || after != "lot-7" {
		t.Fatalf("post-split P-1180 should be Lot 7, got %q %v", after, err)
	}
	// the handoff is half-open: the boundary date belongs to the new binding
	on, err := g.ResolveRef("P-1180", "2024-05-30")
	if err != nil || on != "lot-7" {
		t.Fatalf("boundary date should resolve forward, got %q %v", on, err)
	}
}

// An undated reference that could mean two things must fail loudly. Picking one
// silently is how the wrong parcel ends up in a filed document.
func TestUndatedAmbiguousAliasIsAnError(t *testing.T) {
	g := fixtureGraph(t)
	if _, err := g.ResolveRef("P-1180", ""); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want an ambiguity error, got %v", err)
	}
	q, _ := ParseQuery(`claim -about-> #P-1180`)
	if _, err := g.Eval(q, Env{Now: "2026-07-25"}); err == nil {
		t.Fatal("an undated query through an ambiguous alias must not resolve")
	}
	// unambiguous aliases still resolve without a date
	if id, err := g.ResolveRef("CV-2025-00418", ""); err != nil || id != "the-matter" {
		t.Fatalf("unambiguous alias should resolve undated, got %q %v", id, err)
	}
}

func TestAliasQueriesReachDifferentFacts(t *testing.T) {
	g := fixtureGraph(t)
	old := run(t, g, `claim -about-> #P-1180 @2020-06-01`)
	now := run(t, g, `claim -about-> #P-1180 @2025-06-01`)
	if !has(old, "permit-against-parent") {
		t.Fatalf("the access permit is about the parent parcel: %v", old)
	}
	if has(now, "permit-against-parent") {
		t.Fatalf("the permit must not surface as a fact about Lot 7: %v", now)
	}
	// addresses are aliases too, and they contain spaces
	byAddress := run(t, g, `claim -about-> #"411 Cedar Bluff Road" @2025-06-01`)
	if !equal(byAddress, now) {
		t.Fatalf("the address and the parcel number should reach the same facts: %v vs %v", byAddress, now)
	}
	if id, err := g.ResolveRef("411 Cedar Bluff Road", "2025-01-01"); err != nil || id != "lot-7" {
		t.Fatalf("address alias should resolve to Lot 7, got %q %v", id, err)
	}
}

// Overlapping windows mean one of them is wrong, and that is an error rather
// than a warning: a query would then silently pick.
func TestOverlappingAliasWindowsAreAnError(t *testing.T) {
	doc, _ := parseOne(t, `
- id: thing-a
  entity: A
  aliases:
    - as: SHARED
      until: 2022-01-01
- id: thing-b
  entity: B
  aliases:
    - as: SHARED
      from: 2021-01-01
`)
	_, diags := Build([]*Doc{doc})
	for _, d := range diags {
		if d.Severity == SevError && strings.Contains(d.Msg, "same period") {
			return
		}
	}
	t.Fatalf("want an overlap error, got %v", diags)
}

func TestAliasChangeMovesSemHash(t *testing.T) {
	n := Node{Kind: KEntity, Body: "A parcel", Status: SAsserted}
	withAlias := n
	withAlias.Aliases = []Alias{{As: "P1", From: "2021-01-01"}}
	moved := n
	moved.Aliases = []Alias{{As: "P1", From: "2022-01-01"}}
	if SemHash(n, nil) == SemHash(withAlias, nil) {
		t.Fatal("adding an alias must move sem_hash")
	}
	if SemHash(withAlias, nil) == SemHash(moved, nil) {
		t.Fatal("changing an alias window must move sem_hash — it changes what a reference denotes")
	}
}

// A fact can be fully attested and still not settled. Taint keeps it in the
// result and marks what it rests on — unlike `while`, which removes work
// belonging to a branch we are not in.
func TestTaintPropagatesThroughRequires(t *testing.T) {
	g := fixtureGraph(t)
	// membership must NOT flow: a sibling's open question is not this fact's
	if h := g.Taint("drawing-conflicts-with-words", "2026-07-25"); len(h) != 0 {
		t.Fatalf("this claim is attested and requires nothing open: %v", h)
	}
	// a group IS as unsettled as its members
	gh := g.Taint("g-reformation-proof", "2026-07-25")
	if !has(gh, "q-geometry-proof") {
		t.Fatalf("a group inherits its members' holes: %v", gh)
	}
	// transitively: reformation requires the group, which holds the open question
	rh := g.Taint("reformation", "2026-07-25")
	if !has(rh, "q-geometry-proof") {
		t.Fatalf("taint must be transitive: %v", rh)
	}
}

func TestTaintedSetAndPin(t *testing.T) {
	g := fixtureGraph(t)
	p, _, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	set, err := p.Declared("next-steps")
	if err != nil {
		t.Fatal(err)
	}
	pins, err := g.Resolve(set, Env{Now: "2026-07-25"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range pins {
		if len(p.Unresolved) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("the defense plan rests on open questions; no pin recorded them")
	}
}

// Answering a question is a change even when no member of the set moved.
func TestResolvingAHoleIsADelta(t *testing.T) {
	g := fixtureGraph(t)
	before := &Pin{Count: 1, Nodes: []string{"x@aaa"}, Unresolved: []string{"q-split-instrument"}}
	after := &Pin{Count: 1, Nodes: []string{"x@aaa"}}
	d := g.DiffPins("theory", before, after)
	if d.Empty() {
		t.Fatal("a closed hole must register as a change")
	}
	var saw bool
	for _, l := range d.Statements(g) {
		if strings.Contains(l, "is now answered") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("want an 'is now answered' statement: %v", d.Statements(g))
	}
}

// Variants answer "what is this waiting on, and what would each answer change".
func TestVariantsEnumerateDeclaredOptions(t *testing.T) {
	doc, diags := parseOne(t, `
- id: q
  question: Which way?
  status: open
  options: [went-left, went-right]
  exhaustive: true
- id: went-left
  claim: It went left
  status: proposed
- id: went-right
  claim: It went right
  status: proposed
- id: conclusion
  claim: The conclusion
  status: asserted
  requires: q
- id: left-only
  claim: Only true if it went left
  status: asserted
  while: went-left
`)
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	g, _ := Build([]*Doc{doc})
	s := DeclaredSet{
		Name:    "t",
		Queries: []DeclaredQuery{{Name: "facts", Text: "claim @now sort id"}},
	}
	reports, err := g.Variants(s, Env{Now: "2026-07-25"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Question != "q" {
		t.Fatalf("want one report for q: %+v", reports)
	}
	r := reports[0]
	if !r.Diverges {
		t.Fatalf("the answers reach different documents, so this must diverge: %+v", r)
	}
	if len(r.Variants) != 2 {
		t.Fatalf("want a variant per declared option: %+v", r.Variants)
	}
	var leftBrings bool
	for _, v := range r.Variants {
		if v.Option == "went-left" {
			for _, l := range v.Delta {
				if strings.Contains(l, "left-only") {
					leftBrings = true
				}
			}
		}
	}
	if !leftBrings {
		t.Fatalf("the left branch should bring its scoped work into the set: %+v", r.Variants)
	}
}

// An impeachment matrix turns on finding the same person on both sides of a
// contradiction, which needs the speaker of a source to be queryable.
func TestSpeakerIsQueryable(t *testing.T) {
	g := fixtureGraph(t)
	byVole := run(t, g, `source[speaker=vole] sort id`)
	if len(byVole) != 3 {
		t.Fatalf("Vole authored both surveys and the declaration: %v", byVole)
	}
	byQuill := run(t, g, `source[speaker=quill] sort id`)
	if len(byQuill) != 2 {
		t.Fatalf("Quill appears as deponent and as the neighbour account: %v", byQuill)
	}
	// His two positions are BOTH established, and that is the point — so they must
	// not be modelled as contradicting. Both being true simultaneously means
	// `contradicts` is the wrong edge, and marking it made both compute as
	// DISPUTED, which tells a document to hedge about facts that are settled.
	for _, id := range []string{"vole-both-sides", "vole-not-our-witness"} {
		if t2 := g.tension(id, ""); t2.Any() {
			t.Errorf("%s is established, not %s — a document told to hedge here loses the impeachment", id, t2.Kind)
		}
	}
	// The matrix finds them by their shared anchor instead.
	byAnchor := run(t, g, `claim -about-> #vole sort id`)
	for _, id := range []string{"vole-both-sides", "vole-not-our-witness"} {
		if !has(byAnchor, id) {
			t.Errorf("%s is not reachable by its anchor, so nothing finds it: %v", id, byAnchor)
		}
	}
	// And what the declaration actually asserts — the real contradiction — is
	// recorded as an open question rather than invented.
	if _, ok := g.Nodes["q-vole-declaration-substance"]; !ok {
		t.Error("the gap left by removing the wrong edge must stay recorded")
	}
}

// An unquoted `#` is a YAML comment, so a value silently truncates and nothing
// downstream can tell — the value parsed fine.
func TestUnquotedHashTruncationIsCaught(t *testing.T) {
	_, diags := parseOne(t, "- id: x\n  entity: Rhoda Dorsey (Sewer District #2)\n")
	var found bool
	for _, d := range diags {
		if strings.Contains(d.Msg, "unclosed bracket") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a truncation warning, got %v", diags)
	}
	// A balanced body must not warn.
	_, ok := parseOne(t, "- id: y\n  entity: \"Rhoda Dorsey (Sewer District #2)\"\n")
	for _, d := range ok {
		if strings.Contains(d.Msg, "unclosed bracket") {
			t.Fatalf("a quoted value must not warn: %v", d)
		}
	}
}

// `about` is the anchor, and it has a direction: things are ground, events
// anchor to things, facts anchor to things and events. An entity anchoring to
// anything would mean a thing is "about" another thing — and that relationship
// is a claim, not an anchor.
func TestEntitiesAnchorNothing(t *testing.T) {
	g := fixtureGraph(t)
	for _, e := range g.Edges {
		if e.Type != EAbout {
			continue
		}
		if src := g.Nodes[e.Src]; src != nil && src.Kind == KEntity {
			t.Errorf("entity %q is `about` %q — a thing is ground", e.Src, e.Dst)
		}
	}
	// and the check catches it
	doc, _ := parseOne(t, `
- id: a
  entity: A thing
  about: b
- id: b
  entity: Another thing
`)
	_, diags := Build([]*Doc{doc})
	var found bool
	for _, d := range diags {
		if d.Severity == SevError && strings.Contains(d.Msg, "a thing is ground") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an anchor-layering error, got %v", diags)
	}
}

// The layering that actually holds in the corpus, pinned so it stays true.
func TestAnchorLayering(t *testing.T) {
	g := fixtureGraph(t)
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if e.Type != EAbout {
			continue
		}
		src, dst := g.Nodes[e.Src], g.Nodes[e.Dst]
		if src == nil || dst == nil {
			continue
		}
		seen[string(src.Kind)+"->"+string(dst.Kind)] = true
		if src.Kind == KEvent && dst.Kind != KEntity && dst.Kind != KEvent {
			t.Errorf("event %q anchors to a %s; events anchor to things", e.Src, dst.Kind)
		}
	}
	for _, want := range []string{"claim->entity", "claim->event", "event->entity"} {
		if !seen[want] {
			t.Errorf("the corpus should exercise %s", want)
		}
	}
}

// A chase queue is "whose ball is it", and `owner` carries that — including when
// the owner is the other party. The three buckets must partition the open
// questions, or something silently falls out of the log.
func TestOwnershipPartitionsOpenQuestions(t *testing.T) {
	g := fixtureGraph(t)
	all := run(t, g, `question[status=open]`)
	ours := run(t, g, `question[status=open, owner=wren-bramble]`)
	theirs := run(t, g, `question[status=open, owner] & !question[owner=wren-bramble]`)
	unowned := run(t, g, `question[status=open] & !question[owner]`)

	if n := len(ours) + len(theirs) + len(unowned); n != len(all) {
		t.Fatalf("buckets must partition: %d+%d+%d != %d", len(ours), len(theirs), len(unowned), len(all))
	}
	// External owners are the point: a chase is waiting on someone else.
	if !has(theirs, "q-geometry-proof") {
		t.Errorf("q-geometry-proof is Quill's to answer: %v", theirs)
	}
	for _, ourOwn := range []string{"q-photo-dates", "q-settle-or-try"} {
		if has(theirs, ourOwn) {
			t.Errorf("%s is ours; it must not read as someone else's ball", ourOwn)
		}
	}
	if !has(unowned, "q-plat-original") {
		t.Errorf("an unowned question belongs in its own bucket: %v", unowned)
	}
	// The naive spelling swallows unowned questions too, which is the bug this
	// pins: `!question[owner=carl-taylor]` is not "theirs".
	naive := run(t, g, `question[status=open] & !question[owner=wren-bramble]`)
	if len(naive) == len(theirs) {
		t.Fatal("expected the naive query to over-collect; the guard is pointless if not")
	}
}

// `@now` and `sort` are suffixes on a PATTERN, not on a set expression. So
// `claim & !claim[while] @now` scopes the NEGATED term, not the result — which
// reads as "live claims minus contingent ones" and is not that. Pinned because
// it is a silent difference, not an error.
func TestTemporalBindsToPatternNotExpression(t *testing.T) {
	g := fixtureGraph(t)
	trap := run(t, g, `claim & !claim[while] @now`)
	intended := run(t, g, `claim @now & !claim[while] @now`)
	if len(trap) == len(intended) {
		t.Fatal("expected the two spellings to differ; the trap is what makes the doc necessary")
	}
	if len(trap) <= len(intended) {
		t.Fatalf("the trap over-collects: %d vs %d", len(trap), len(intended))
	}
	// A temporal suffix cannot appear mid-pattern.
	if _, err := ParseQuery(`claim @now -requires-> #x`); err == nil {
		t.Fatal("@now mid-pattern must not parse")
	}
	// Parenthesising is the fix: it scopes the suffix to the whole expression.
	scoped := run(t, g, `(claim & !claim[while]) @now`)
	if len(scoped) != len(intended) {
		t.Fatalf("a scoped suffix must equal the per-pattern spelling: %d vs %d", len(scoped), len(intended))
	}
}

// A temporal on a group pushes DOWN into patterns that lack their own, and an
// inner temporal still wins — otherwise scoping a group would silently rewrite
// a deliberately dated term.
func TestScopedTemporalPushesDownButInnerWins(t *testing.T) {
	g := fixtureGraph(t)
	dated := run(t, g, `claim -about-> #P-1180 @2020-06-01`)
	if len(dated) == 0 {
		t.Fatal("fixture should have pre-2021 parcel claims")
	}
	both := run(t, g, `(claim -about-> #P-1180 @2020-06-01 | claim[status=false]) @now`)
	for _, id := range dated {
		if !has(both, id) {
			t.Fatalf("the inner @2020-06-01 must survive the outer @now: %s missing", id)
		}
	}
	// and the undated term did get the outer context
	undated := run(t, g, `claim[status=false] @now`)
	for _, id := range undated {
		if !has(both, id) {
			t.Fatalf("the undated term should inherit @now: %s missing", id)
		}
	}
}

func TestScopedSortOrdersTheWholeSet(t *testing.T) {
	g := fixtureGraph(t)
	ids := run(t, g, `(event[at>2026-08-01] | event[computed]) @now sort at`)
	if len(ids) < 2 {
		t.Fatalf("need at least two to check ordering: %v", ids)
	}
	var last string
	for _, id := range ids {
		at, _ := g.field(g.Nodes[id], "at", "")
		if last != "" && at < last {
			t.Fatalf("not sorted across the union: %v", ids)
		}
		last = at
	}
}

// The shared library: written once, referenced from many specs.
func TestNamedQueryLibraryLoads(t *testing.T) {
	p, diags, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	for _, want := range []string{"blockers", "live-leads", "record-backed", "contingent"} {
		if _, ok := p.Named[want]; !ok {
			t.Errorf("@%s missing from the library: %v", want, p.Named)
		}
	}
	q, err := ParseQuery(`@record-backed & !@contingent`)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := p.Graph.Eval(q, Env{Now: "2026-07-26", Named: p.Named})
	if err != nil || len(ids) == 0 {
		t.Fatalf("a composed named query must resolve: %d %v", len(ids), err)
	}
	// Without the library the same query is an error, not an empty set.
	if _, err := p.Graph.Eval(q, Env{Now: "2026-07-26"}); err == nil {
		t.Fatal("an unknown named query must error rather than resolve empty")
	}
}

// An open question is only actionable if you know what would close it. The four
// kinds imply completely different next actions — read something, decide
// something, chase someone, or reason it through — and they must partition the
// open set or a document silently omits a category.
func TestNeedsPartitionsOpenQuestions(t *testing.T) {
	g := fixtureGraph(t)
	all := run(t, g, `question[status=open]`)
	total := 0
	for _, k := range []string{"evidence", "decision", "reply", "analysis"} {
		got := run(t, g, `question[status=open, needs=`+k+`]`)
		if len(got) == 0 {
			t.Errorf("no open question needs %s; the corpus should exercise all four", k)
		}
		total += len(got)
	}
	if total != len(all) {
		t.Fatalf("needs must partition: %d classified vs %d open", total, len(all))
	}
	if _, err := ParseQuery(`question[needs=vibes]`); err != nil {
		t.Fatal(err)
	}
	q, _ := ParseQuery(`question[needs=vibes]`)
	if errs := ValidateQuery(q); len(errs) == 0 {
		t.Fatal("an unknown needs must not validate")
	}
}

// Two ways an open question becomes permanent, both now caught.
func TestUnactionableQuestionsAreFlagged(t *testing.T) {
	doc, _ := parseOne(t, `
- id: q-wish
  question: What is the consult fee?
  status: open
- id: q-anchored
  question: Does the letter say so?
  status: open
  needs: evidence
  owner: someone
- id: someone
  entity: Someone
  status: asserted
`)
	_, diags := Build([]*Doc{doc})
	var noNeeds, noAnchor bool
	for _, d := range diags {
		if !strings.Contains(d.Msg, "q-wish") {
			if strings.Contains(d.Msg, "q-anchored") {
				t.Errorf("a question with needs and an owner must not warn: %v", d)
			}
			continue
		}
		if strings.Contains(d.Msg, "no `needs`") {
			noNeeds = true
		}
		if strings.Contains(d.Msg, "neither an owner nor an `about`") {
			noAnchor = true
		}
	}
	if !noNeeds || !noAnchor {
		t.Fatalf("want both warnings for the un-anchored wish: %v", diags)
	}
}

// A resolved question needs neither: the checks are about what is still open.
func TestResolvedQuestionsAreNotNagged(t *testing.T) {
	doc, _ := parseOne(t, "- id: q\n  question: Settled?\n  status: resolved\n")
	_, diags := Build([]*Doc{doc})
	for _, d := range diags {
		if strings.Contains(d.Msg, "needs") || strings.Contains(d.Msg, "anchor") {
			t.Fatalf("a resolved question must not be nagged: %v", d)
		}
	}
}

// A conclusion's premises are a real relation and must be walkable. They were a
// plain field for a long time — validated, hashed, unwalkable — so the one
// question worth asking of a conclusion ("what does this rest on, and is any of
// it weak?") could not be asked in the query language at all.
//
// derived_from takes a LIST: one inference can rest on several facts, and each
// gets its own edge.
func TestDerivedFromIsWalkableAndMultiEdge(t *testing.T) {
	g := fixtureGraph(t)

	// Every premise of every inference is reachable in one hop.
	prem := run(t, g, "claim <-derived_from- source[class=inference]")
	if len(prem) == 0 {
		t.Fatal("no premises reachable through derived_from — the edge is not being emitted")
	}

	// The field survives alongside the edge: SemHash reads Premises, so losing it
	// would silently rewrite every inference's hash.
	var withPremises int
	for _, n := range g.Nodes {
		if n.Source != nil && len(n.Source.Premises) > 0 {
			withPremises++
		}
	}
	if withPremises == 0 {
		t.Error("derived_from was consumed as an edge and the Source.Premises field was lost")
	}

	// An inference resting on several premises emits several edges — the whole
	// point of the list form.
	multi := 0
	for id, n := range g.Nodes {
		if n.Source == nil || len(n.Source.Premises) < 2 {
			continue
		}
		var got int
		for _, e := range g.Edges {
			if e.Src == id && e.Type == EDerivedFrom {
				got++
			}
		}
		if got != len(n.Source.Premises) {
			t.Errorf("%s: %d premises but %d derived_from edges", id, len(n.Source.Premises), got)
		}
		multi++
	}
	if multi == 0 {
		t.Skip("fixture has no multi-premise inference to check")
	}
}

// A statutory period is calendar arithmetic, not a multiple of a day. Three years
// from 2023-08-04 bars on 2026-08-04; expressed as 1095d it would bar on
// 2026-08-03, because 2024 was a leap year, and a bar date one day early is a
// missed claim rather than a rounding error.
//
// Built directly rather than scanned: ResolveAt reads At and AtExpr and nothing
// else, so routing through a corpus fixture would test the scanner instead.
func TestDerivedDatesCalendarUnits(t *testing.T) {
	g := &Graph{Nodes: map[string]*Node{
		"e-routing": {ID: "e-routing", At: "2023-08-04"},
		"e-death":   {ID: "e-death", At: "2026-03-15"},

		"d-three-years":  {ID: "d-three-years", AtExpr: "= e-routing.at + 3y"},
		"d-same-in-days": {ID: "d-same-in-days", AtExpr: "= e-routing.at + 1095d"},
		"d-creditor-bar": {ID: "d-creditor-bar", AtExpr: "= e-death.at + 4mo"},
		"d-outer-bound":  {ID: "d-outer-bound", AtExpr: "= e-death.at + 24mo"},
		"d-chained":      {ID: "d-chained", AtExpr: "= d-creditor-bar.at - 1mo"},
		"d-mixed":        {ID: "d-mixed", AtExpr: "= e-death.at + 1y + 10d"},

		"d-bad-unit": {ID: "d-bad-unit", AtExpr: "= e-death.at + 3w"},
		"d-bad-num":  {ID: "d-bad-num", AtExpr: "= e-death.at + xy"},
	}}

	for _, c := range []struct{ id, want string }{
		{"d-three-years", "2026-08-04"},  // calendar: the same day, three years on
		{"d-same-in-days", "2026-08-03"}, // the leap-year error this exists to avoid
		{"d-creditor-bar", "2026-07-15"}, // RCW 11.40.051, four months
		{"d-outer-bound", "2028-03-15"},  // months past twelve keep counting
		{"d-chained", "2026-06-15"},      // minus, off a derived date
		{"d-mixed", "2027-03-25"},        // terms of different units compose
	} {
		got, ok := g.ResolveAt(c.id)
		if !ok || got != c.want {
			t.Errorf("%s (%s): got %q ok=%v, want %q",
				c.id, g.Nodes[c.id].AtExpr, got, ok, c.want)
		}
	}

	// An unrecognised unit must refuse to resolve. Silently reading "3w" as three
	// days would put a bar date three weeks early and nothing would say so.
	for _, id := range []string{"d-bad-unit", "d-bad-num"} {
		if got, ok := g.ResolveAt(id); ok {
			t.Errorf("%s (%s): resolved to %q; an unparseable offset must not resolve",
				id, g.Nodes[id].AtExpr, got)
		}
	}
}
