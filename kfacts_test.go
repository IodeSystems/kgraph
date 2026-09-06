package kgraph

import (
	"strings"
	"testing"
)

// The hand-built fixtures are the test corpus: two real projects of different
// shape (a legal-theory graph and a billing dispute).
func TestScanFixtures(t *testing.T) {
	g, diags, err := ScanFactsIn("examples", "fence-dispute")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) == 0 {
		t.Fatal("no nodes parsed")
	}
	t.Logf("%d nodes, %d edges, %d diagnostics", len(g.Nodes), len(g.Edges), len(diags))
	for _, d := range diags {
		t.Log(d)
	}
	if errs := Errors(diags); len(errs) > 0 {
		t.Errorf("%d errors in fixtures", len(errs))
	}
}

func parseOne(t *testing.T, body string) (*Doc, []Diag) {
	t.Helper()
	return parseFixture("t.facts", []byte("```kfacts\n"+body+"\n```\n"))
}

func TestKindAndEdges(t *testing.T) {
	doc, diags := parseOne(t, `
- id: a
  claim: A holds
  status: asserted
  requires: b
- id: b
  question: Does B hold?
  status: open
`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if len(doc.Nodes) != 2 || doc.Nodes[0].Kind != KClaim || doc.Nodes[1].Kind != KQuestion {
		t.Fatalf("bad kinds: %+v", doc.Nodes)
	}
	if len(doc.Edges) != 1 || doc.Edges[0].Src != "a" || doc.Edges[0].Dst != "b" {
		t.Fatalf("bad edges: %+v", doc.Edges)
	}
}

// Inverse keys must be stored in canonical direction, or the same relation
// authored from either end produces two different sem_hashes.
func TestInverseEdgesAreCanonical(t *testing.T) {
	doc, _ := parseOne(t, `
- id: s
  document: A packet
  class: record
- id: c
  claim: Something
  attested_by: s
`)
	if len(doc.Edges) != 1 {
		t.Fatalf("want 1 edge, got %+v", doc.Edges)
	}
	e := doc.Edges[0]
	if e.Type != EAttests || e.Src != "s" || e.Dst != "c" {
		t.Fatalf("attested_by not canonicalized: %+v", e)
	}
}

func TestOneIdiomPerRelation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"question supports claim", `
- id: q
  question: Is it so?
  supports: c
- id: c
  claim: It is so
`, "requires"},
		{"question answers claim", `
- id: q
  question: Is it so?
  answers: c
- id: c
  claim: It is so
`, "answer → question"},
		{"non-source attests", `
- id: c
  claim: It is so
  attests: d
- id: d
  claim: Other
`, "only a source can `attests`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := parseOne(t, tc.body)
			var found bool
			for _, d := range diags {
				if strings.Contains(d.Msg, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want a diagnostic containing %q, got %v", tc.want, diags)
			}
		})
	}
}

func TestDisputedIsNotAuthorable(t *testing.T) {
	_, diags := parseOne(t, "- id: c\n  claim: X\n  status: disputed\n")
	if len(diags) == 0 || !strings.Contains(diags[0].Msg, "computed") {
		t.Fatalf("authored `disputed` must be rejected, got %v", diags)
	}
}

// Printing "2026-07" as a specific day is a misstatement, so month-granularity
// dates must carry month precision without the author saying so.
func TestPrecisionInferredFromDate(t *testing.T) {
	doc, _ := parseOne(t, "- id: e\n  event: Denial\n  at: 2026-07\n")
	if doc.Nodes[0].Precision != PMonth {
		t.Fatalf("want month precision, got %q", doc.Nodes[0].Precision)
	}
	doc, _ = parseOne(t, "- id: e2\n  event: Service\n  at: 2026-05-04\n")
	if doc.Nodes[0].Precision != PDay {
		t.Fatalf("want day precision, got %q", doc.Nodes[0].Precision)
	}
}

func TestComputedValueAndDate(t *testing.T) {
	doc, diags := parseOne(t, `
- id: v
  claim: Gap
  value: "= a.value - b.value"
  unit: USD
- id: d
  event: Deadline
  at: "= e-eob.at + 180d"
`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if doc.Nodes[0].ValueExpr == "" || doc.Nodes[0].HasValue {
		t.Fatalf("computed value not recognized: %+v", doc.Nodes[0])
	}
	if doc.Nodes[1].AtExpr == "" || doc.Nodes[1].At != "" {
		t.Fatalf("derived date not recognized: %+v", doc.Nodes[1])
	}
}

// sem_hash must ignore edge authoring order and file position, or a re-ingest
// false-flags every document that pins the node.
func TestSemHashStableAcrossEdgeOrderAndPosition(t *testing.T) {
	n := Node{Kind: KClaim, Body: "X", Status: SAsserted, File: "a.md", Line: 3}
	e1 := Edge{Src: "n", Dst: "p", Type: ERequires}
	e2 := Edge{Src: "n", Dst: "q", Type: ESupports}
	if SemHash(n, []Edge{e1, e2}) != SemHash(n, []Edge{e2, e1}) {
		t.Fatal("sem_hash depends on edge order")
	}
	moved := n
	moved.File, moved.Line = "b.md", 99
	if SemHash(n, []Edge{e1}) != SemHash(moved, []Edge{e1}) {
		t.Fatal("sem_hash depends on file position")
	}
	changed := n
	changed.Status = SWithdrawn
	if SemHash(n, []Edge{e1}) == SemHash(changed, []Edge{e1}) {
		t.Fatal("sem_hash ignores status")
	}
	if SemHash(n, []Edge{e1}) == SemHash(n, []Edge{e1, e2}) {
		t.Fatal("sem_hash ignores incident edges")
	}
}

// TestManagedBlockIsNotParsedBack retired 2026-08-30. It pinned that a fact file
// stopped parsing at `<!-- kgraph:managed`, so a generated block could never be
// read back in as authored input. There are no generated blocks — render went in
// 8127461 — and there are no fact files either. The rule it protected has no
// surface left to protect.

func TestDanglingRefsAreBuildErrors(t *testing.T) {
	doc, _ := parseOne(t, "- id: a\n  claim: X\n  requires: nope\n  while: alsonope\n")
	_, diags := Build([]*Doc{doc})
	var refs int
	for _, d := range diags {
		if strings.Contains(d.Msg, "nope") {
			refs++
		}
	}
	if refs != 2 {
		t.Fatalf("want 2 dangling-ref diagnostics, got %v", diags)
	}
}

// "There is no plan for the branch where X holds" is the most useful thing the
// tool can tell a planner, and it is a graph query rather than a judgment call.
func TestUnplannedBranchIsReported(t *testing.T) {
	doc, _ := parseOne(t, `
- id: q
  question: Which way?
  status: open
  options: [left, right]
  exhaustive: true
- id: left
  claim: It went left
  status: proposed
- id: right
  claim: It went right
  status: proposed
- id: plan-left
  action: Do the left thing
  status: open
  while: left
`)
	_, diags := Build([]*Doc{doc})
	var found string
	for _, d := range diags {
		if strings.Contains(d.Msg, "no plan for the branch") {
			found += d.Msg
		}
	}
	if !strings.Contains(found, `"right"`) {
		t.Fatalf("want an unplanned-branch warning for `right`, got %v", diags)
	}
	if strings.Contains(found, `"left"`) {
		t.Fatalf("`left` is planned; must not warn: %v", diags)
	}
}

func TestExclusiveOptionsCannotBothHold(t *testing.T) {
	doc, _ := parseOne(t, `
- id: q
  question: Which way?
  status: resolved
  options: [left, right]
- id: left
  claim: Left
  status: asserted
  while: q
- id: right
  claim: Right
  status: asserted
  while: q
`)
	_, diags := Build([]*Doc{doc})
	for _, d := range diags {
		if strings.Contains(d.Msg, "all asserted") {
			return
		}
	}
	t.Fatalf("want an exclusivity error, got %v", diags)
}

// A citation is not just an id — `citation()` prints the document path and the
// evidentiary class into the artifact, and `class` additionally decides the
// `[[id|class]]` token and the ordinal rank that resolves a conflict. None of it
// was hashed, so reorganising a corpus, or reclassifying a record as hearsay,
// changed what every document says while every document reported `fresh`.
//
// The counterpart is TestSourceDriftIsNotInSemHash: the document's BYTES stay
// out. These fields are authored — a re-ingest cannot move them.
func TestAuthoredProvenanceIsInSemHash(t *testing.T) {
	base := Node{Kind: KSource, Body: "The exhibit", Status: SAsserted,
		Source: &Source{Form: "document", Class: "record", DocPath: "evidence/x.md"}}
	h := SemHash(base, nil)

	for _, tc := range []struct {
		what string
		mut  func(*Source)
	}{
		{"the document moved", func(s *Source) { s.DocPath = "legacy/x.md" }},
		{"a record was reclassified as hearsay", func(s *Source) { s.Class = "hearsay" }},
		{"the form changed", func(s *Source) { s.Form = "utterance" }},
		{"someone else is now credited", func(s *Source) { s.Speaker = "p-other" }},
		{"the medium changed", func(s *Source) { s.Medium = "phone" }},
		{"it became a recording", func(s *Source) { s.Recorded = true }},
		{"the anchor moved", func(s *Source) { s.Anchor = "p12" }},
		{"an inference rests on a new premise", func(s *Source) { s.Premises = []string{"c-new"} }},
	} {
		cp := *base.Source
		n := base
		n.Source = &cp
		tc.mut(n.Source)
		if SemHash(n, nil) == h {
			t.Errorf("%s: rendered into the document, but sem_hash did not move", tc.what)
		}
	}

	// Premise ORDER is not meaning, same as edge order.
	a, b := base, base
	sa, sb := *base.Source, *base.Source
	sa.Premises, sb.Premises = []string{"c-1", "c-2"}, []string{"c-2", "c-1"}
	a.Source, b.Source = &sa, &sb
	if SemHash(a, nil) != SemHash(b, nil) {
		t.Error("sem_hash depends on premise order")
	}
}

// A scalar field written twice is last-write-wins, and it was silent. Five nodes
// in the fence-dispute corpus lost authored text this way — an original `reason:` saying
// why the work mattered, then a second `reason:` appended months later saying
// what the answer was, with the first discarded and `kg scan` reporting 0 errors.
func TestRepeatedScalarFieldIsReported(t *testing.T) {
	src := "```kfacts\n" +
		"- id: q1\n  question: Obtain the certification\n  status: open\n" +
		"  reason: Why the work matters.\n" +
		"  reason: What the answer turned out to be.\n" +
		"```\n"
	_, diags := parseFixture("a.facts", []byte(src))
	var found *Diag
	for i, d := range diags {
		if strings.Contains(d.Msg, "written twice") {
			found = &diags[i]
		}
	}
	if found == nil {
		t.Fatalf("a repeated `reason:` discards the first block silently: %v", diags)
	}
	if found.Severity != SevError {
		t.Errorf("losing authored text is an error, not a warning")
	}
	if !strings.Contains(found.Msg, `"reason"`) || !strings.Contains(found.Msg, "line 4") {
		t.Errorf("both lines have to be findable from the message: %q", found.Msg)
	}
	// A merge is not always the fix and the message must not propose one: two
	// `status:` values that disagree is a contradiction, not a concatenation.
	if strings.Contains(found.Msg, "combine") || strings.Contains(found.Msg, "merge") {
		t.Errorf("the message proposes joining the two values: %q", found.Msg)
	}
}

// The repeated-key idiom that IS intended, and the reason this cannot be "no key
// twice". `attested_by:` twice means two sources; `requires:` twice means two
// prerequisites. Both are edge keys and both accumulate.
func TestRepeatedEdgeFieldsStillAccumulate(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s1\n  record: One\n  class: record\n" +
		"- id: s2\n  record: Two\n  class: record\n" +
		"- id: c1\n  claim: A fact resting on two documents\n  status: asserted\n" +
		"  attested_by: s1\n  attested_by: s2\n" +
		"```\n"
	d, diags := parseFixture("a.facts", []byte(src))
	for _, x := range diags {
		if strings.Contains(x.Msg, "written twice") {
			t.Fatalf("two sources is the intended idiom, not a defect: %q", x.Msg)
		}
	}
	var n int
	for _, e := range d.Edges {
		if (e.Src == "c1" && (e.Dst == "s1" || e.Dst == "s2")) ||
			(e.Dst == "c1" && (e.Src == "s1" || e.Src == "s2")) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("both attestations must survive the parse, got %d", n)
	}
}

// Reporting must not change what the corpus renders on the day it ships: the
// second value still wins, exactly as before. Only the diagnostic is new.
func TestRepeatedScalarStillAssignsTheLastValue(t *testing.T) {
	src := "```kfacts\n" +
		"- id: c1\n  claim: A fact\n  status: asserted\n  status: withdrawn\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	if len(d.Nodes) != 1 || d.Nodes[0].Status != "withdrawn" {
		t.Fatalf("parse result changed: %+v", d.Nodes)
	}
}

// A RELATION CAN BE NAMED, ANNOTATED, AND SPOKEN ABOUT.
//
// Three things land together because none is useful alone. An edge had no
// identity, so nothing could reference one; no bag, so annotation was limited to
// a closed five-key set; and no standalone form, so it was always a field on one
// of its endpoints — which is how caselit ended up authoring ONE relation twice,
// as `satisfied_by:` on the element and `turns_on:` on the acquisition, with
// nothing reconciling them.
func TestAStandaloneRelationCarriesIdentityAndAnnotation(t *testing.T) {
	const src = "```kfacts\n" +
		"- id: el-drawing-defect\n  claim: The plat drawing is defective\n  status: asserted\n" +
		"- id: s-ros-2026\n  record: Corrective record of survey\n  class: record\n" +
		"- id: e-ros-attests-drawing\n" +
		"  from: s-ros-2026\n" +
		"  to: el-drawing-defect\n" +
		"  is: attests\n" +
		"  by: carl-taylor\n" +
		"  because: the surveyor's certificate, not the cover letter\n" +
		"```\n"

	doc, ds := parseFixture("f.facts", []byte(src))
	for _, d := range ds {
		if d.Severity == SevError {
			t.Fatalf("unexpected error: %s", d.Msg)
		}
	}
	var e *Edge
	for i := range doc.Edges {
		if doc.Edges[i].Type == EAttests {
			e = &doc.Edges[i]
		}
	}
	if e == nil {
		t.Fatal("the standalone relation did not become an edge")
	}
	if e.Src != "s-ros-2026" || e.Dst != "el-drawing-defect" {
		t.Errorf("endpoints are %s -> %s", e.Src, e.Dst)
	}
	if e.ID != "e-ros-attests-drawing" {
		t.Errorf("authored id lost: %q", e.ID)
	}
	// The bag holds what the typed fields do not. `by` is the first such key and
	// the reason the bag exists: who asserted a relation is worth recording in an
	// audited corpus and was previously unrecordable.
	if e.Bag["by"] != "carl-taylor" {
		t.Errorf("bag lost `by`: %+v", e.Bag)
	}
	// `because` is TYPED and must not fall into the bag — it drives rendering and
	// sits inside SemHash.
	if e.Because == "" || e.Bag["because"] != "" {
		t.Errorf("because must be typed, not bagged: %q / %+v", e.Because, e.Bag)
	}
	// Content address, so a relation is addressable with nothing authored.
	if e.CID != EdgeCID("s-ros-2026", EAttests, "el-drawing-defect") {
		t.Errorf("CID is not the content address of (src, type, dst): %q", e.CID)
	}
}

// THE SHORT FORM AND THE STANDALONE FORM ARE ONE RELATION, NOT TWO.
//
// A second authoring surface that produced a different edge would be the same
// two-models failure this is meant to end. Same endpoints and type must reach
// the same content address however they were written.
func TestBothAuthoringFormsAgreeOnIdentity(t *testing.T) {
	short := "```kfacts\n" +
		"- id: el-x\n  claim: A claim\n  attested_by: s-y\n" +
		"- id: s-y\n  record: A record\n  class: record\n```\n"
	long := "```kfacts\n" +
		"- id: el-x\n  claim: A claim\n" +
		"- id: s-y\n  record: A record\n  class: record\n" +
		"- from: s-y\n  to: el-x\n  is: attests\n```\n"

	pick := func(src string) Edge {
		t.Helper()
		doc, ds := parseFixture("f.facts", []byte(src))
		for _, d := range ds {
			if d.Severity == SevError {
				t.Fatalf("unexpected error: %s", d.Msg)
			}
		}
		for _, e := range doc.Edges {
			if e.Type == EAttests {
				return e
			}
		}
		t.Fatal("no attests edge")
		return Edge{}
	}
	a, b := pick(short), pick(long)
	if a.CID != b.CID {
		t.Errorf("the two forms disagree: short %s(%s->%s) vs standalone %s(%s->%s)",
			a.CID, a.Src, a.Dst, b.CID, b.Src, b.Dst)
	}
}

// A TYPO'D KIND STAYS THE ERROR IT ALWAYS WAS, and is not reinterpreted as a
// relation. This is why isEdgeEntry requires BOTH endpoints rather than merely
// the absence of a body key.
func TestATypodKindIsNotReadAsARelation(t *testing.T) {
	const src = "```kfacts\n- id: el-x\n  claimm: A claim with a typo'd kind\n```\n"
	_, ds := parseFixture("f.facts", []byte(src))
	var sawKindError bool
	for _, d := range ds {
		if d.Severity == SevError && strings.Contains(d.Msg, "declares no kind") {
			sawKindError = true
		}
	}
	if !sawKindError {
		t.Errorf("a typo'd body key must stay a 'declares no kind' error; got %v", ds)
	}
}

// TestAWithdrawnRelationIsNotARelation.
//
// `OpWithdraw` folds to `status: withdrawn` on the entry. For a NODE that is
// right: the claim stays visible with its status and its reason, so a reader can
// see we were wrong and why. An EDGE has no status to carry and should not grow
// one — a relation is not something anybody reasons about the truth of, it holds
// or it does not.
//
// Before this, withdrawing an edge wrote a correct line to the log, folded it,
// and left the edge standing. The log said the relation was retracted and the
// graph went on computing with it. caselit found it while building `unbind`,
// which is the act its own auditor had been instructing for as long as the
// provenance rules have existed.
func TestAWithdrawnRelationIsNotARelation(t *testing.T) {
	const src = "- id: c-one\n  claim: The strip is held by Halloway\n  status: asserted\n" +
		"- id: c-two\n  claim: The fence was moved in 2014\n  status: asserted\n" +
		"- id: e-live\n  from: c-two\n  to: c-one\n  is: supports\n" +
		"- id: e-gone\n  from: c-one\n  to: c-two\n  is: supports\n  status: withdrawn\n"
	doc, diags := ParseEntries(Dialect{}, "t.md", []byte(src))
	for _, d := range diags {
		if d.Severity == SevError {
			t.Fatalf("unexpected error: %s", d.Msg)
		}
	}
	var live, gone bool
	for _, e := range doc.Edges {
		if e.Src == "c-two" && e.Dst == "c-one" {
			live = true
		}
		if e.Src == "c-one" && e.Dst == "c-two" {
			gone = true
		}
	}
	if !live {
		t.Error("the live relation did not survive; withdrawal took the wrong one")
	}
	if gone {
		t.Error("a relation marked withdrawn still produced an edge — the log says it was " +
			"retracted and the graph is still computing with it")
	}
}
