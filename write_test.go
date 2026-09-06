package kgraph

import (
	"path/filepath"
	"strings"
	"testing"
)

func writeCorpus(t *testing.T) Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := Assert(st, legal, "s-deed", map[string]any{
		"record": "The 1984 deed", "doc": "deed.md", "class": "record"}, "carl", ""); err != nil {
		t.Fatal(err)
	}
	return st
}

// THE GUARD THIS FILE EXISTS FOR. A write that would not load is refused BEFORE
// it lands, not reported by a scan afterwards.
//
// A UI that can write a graph the CLI refuses to load is a UI that corrupts a
// corpus, and detecting it later is the same shape as a document reporting
// itself fresh: too late to be worth detecting.
func TestAWriteThatWouldNotLoadIsRefused(t *testing.T) {
	st := writeCorpus(t)
	_, err := Assert(st, legal, "c-one", map[string]any{
		"claim": "The strip is held by Halloway", "status": "asserted",
		"attested_by": "s-nonexistent"}, "carl", "")
	if err == nil {
		t.Fatal("an assertion citing an unknown source must be refused")
	}
	if !strings.Contains(err.Error(), "would not load") {
		t.Errorf("the refusal must say why, got %q", err)
	}
	// And it must not have landed. A refused write that appended anyway is worse
	// than one that succeeded, because the store now disagrees with what the
	// caller was told.
	log, rerr := st.All()
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, a := range log {
		if a.ID == "c-one" {
			t.Fatal("the refused assertion was written anyway")
		}
	}
}

// Warnings do not refuse. A corpus carries them by design — thirty facts resting
// on no source is a backlog, not a fault — and a writer that blocked on those
// would block every assertion in a real matter.
func TestAWarningDoesNotRefuseTheWrite(t *testing.T) {
	st := writeCorpus(t)
	res, err := Assert(st, legal, "c-unsourced", map[string]any{
		"claim": "Something nobody has sourced yet", "status": "asserted"}, "carl", "")
	if err != nil {
		t.Fatalf("an unsourced fact is a warning, not an error: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("the caller must be told, at the moment they can still fix it")
	}
}

// A correction is TWO LINES, ONE ACT, and the `supersedes` is written by the op
// rather than left to the caller — forgetting it leaves the corpus holding both
// facts with nothing saying which won.
func TestCorrectWithdrawsAndSupersedesTogether(t *testing.T) {
	st := writeCorpus(t)
	if _, err := Assert(st, legal, "c-one", map[string]any{
		"claim": "The strip is held by Halloway", "status": "asserted",
		"attested_by": "s-deed"}, "carl", ""); err != nil {
		t.Fatal(err)
	}
	res, err := Correct(st, legal, "c-one", "c-one-b", map[string]any{
		"claim": "The strip is held by Bramble", "status": "asserted", "attested_by": "s-deed"},
		"the deed describes the adjoining parcel", "carl", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 {
		t.Fatalf("a correction is two lines, got %d", len(res.Applied))
	}
	log, _ := st.All()
	doc, ds := FoldAssertions(legal, "facts.jsonl", log)
	if errs := Errors(ds); len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	g, _ := BuildWith([]*Doc{doc}, AttestSet{})
	old, ok := g.Nodes["c-one"]
	if !ok || old.Status != SWithdrawn {
		t.Fatalf("the old fact is %v", old)
	}
	if !strings.Contains(old.Reason, "adjoining parcel") {
		t.Errorf("the withdrawal lost its reason: %q", old.Reason)
	}
	// The replacement says what it replaces, and the op wrote that, not the test.
	var superseded bool
	for _, e := range g.Edges {
		if e.Src == "c-one-b" && e.Dst == "c-one" && e.Type == ESupersedes {
			superseded = true
		}
	}
	if !superseded {
		t.Error("the replacement does not supersede what it replaced")
	}
}

// Reusing the id IS the in-place edit this whole store exists to avoid.
func TestCorrectRefusesToReuseTheId(t *testing.T) {
	st := writeCorpus(t)
	if _, err := Correct(st, legal, "c-one", "c-one", map[string]any{"claim": "x"},
		"because", "carl", ""); err == nil {
		t.Fatal("a correction into the same id must be refused")
	}
	if _, err := Correct(st, legal, "c-one", "c-two", map[string]any{"claim": "x"},
		"  ", "carl", ""); err == nil {
		t.Fatal("a correction with no reason must be refused")
	}
}

// ALL OR NOTHING. A half-applied correction leaves a fact withdrawn with nothing
// standing in its place, which is worse than the wrong fact it was fixing.
func TestARefusedCorrectionWithdrawsNothing(t *testing.T) {
	st := writeCorpus(t)
	if _, err := Assert(st, legal, "c-one", map[string]any{
		"claim": "The strip is held by Halloway", "status": "asserted",
		"attested_by": "s-deed"}, "carl", ""); err != nil {
		t.Fatal(err)
	}
	// The replacement cites a source that does not exist, so the whole act fails.
	if _, err := Correct(st, legal, "c-one", "c-one-b", map[string]any{
		"claim": "The strip is held by Bramble", "status": "asserted",
		"attested_by": "s-nonexistent"}, "wrong parcel", "carl", ""); err == nil {
		t.Fatal("the correction should have been refused")
	}
	log, _ := st.All()
	doc, _ := FoldAssertions(legal, "facts.jsonl", log)
	g, _ := BuildWith([]*Doc{doc}, AttestSet{})
	if n := g.Nodes["c-one"]; n == nil || n.Status != SAsserted {
		t.Fatalf("the original was withdrawn by a correction that failed: %v", n)
	}
}

// An answer is a FACT with `answers:` AND it closes the question, in one act.
//
// The first half is the invariant and has not moved: a question marked resolved
// with nothing attached is an answer nobody can check. The second half changed
// on 2026-09-01 — this test previously required the question to stay `open`,
// which meant `question[status=open]` matched an answered question forever and
// every open-questions query over-reported. Attaching the fact in the SAME
// `Apply` is what earns the status change, so the rule is satisfied rather than
// relaxed.
func TestAnsweringAttachesAFactAndClosesTheQuestion(t *testing.T) {
	st := writeCorpus(t)
	if _, err := Assert(st, legal, "q-who", map[string]any{
		"question": "Who surveyed the strip", "status": "open",
		"needs": "evidence"}, "carl", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(st, legal, "q-who", "c-crestline", map[string]any{
		"claim": "Crestline surveyed it in 2008", "status": "asserted",
		"attested_by": "s-deed"}, "carl", ""); err != nil {
		t.Fatal(err)
	}
	log, _ := st.All()
	doc, _ := FoldAssertions(legal, "facts.jsonl", log)
	g, _ := BuildWith([]*Doc{doc}, AttestSet{})
	var answered bool
	for _, e := range g.Edges {
		if e.Src == "c-crestline" && e.Dst == "q-who" && e.Type == EAnswers {
			answered = true
		}
	}
	if !answered {
		t.Error("the answer is not attached to the question")
	}
	n := g.Nodes["q-who"]
	if n == nil {
		t.Fatal("the question is gone")
	}
	if n.Status != SResolved {
		t.Errorf("an answered question is still %q — `question[status=open]` will "+
			"keep matching it", n.Status)
	}
	// THE AMEND CARRIED THE REST. An assert REPLACES rather than merges, so
	// closing the question by re-asserting it drops every field that was not
	// carried over — the question's body and its `needs` among them.
	if n.Body != "Who surveyed the strip" {
		t.Errorf("closing the question rewrote it: %q", n.Body)
	}
	if n.Needs != "evidence" {
		t.Errorf("closing the question dropped `needs`: %q", n.Needs)
	}
	// Both lines are in the log. The close is a new assertion, not an edit, so
	// what the corpus used to say survives.
	var asked int
	for _, a := range log {
		if a.ID == "q-who" && a.Op == OpAssert {
			asked++
		}
	}
	if asked != 2 {
		t.Errorf("the question has %d assertions, want 2 — asked, then closed", asked)
	}
}

// Answering a question the corpus does not hold is REFUSED, and nothing is
// written — not the answer, and not an invented question to hang it on.
//
// The refusal comes from `Apply`, which builds the graph the write would produce
// and rejects a dangling `answers` edge, so the close added above cannot smuggle
// a question into existence: both lines fail together or neither lands.
func TestAnsweringAQuestionTheCorpusDoesNotHoldIsRefused(t *testing.T) {
	st := writeCorpus(t)
	before, _ := st.All()
	_, err := AnswerQuestion(st, legal, "q-absent", "c-late", map[string]any{
		"claim": "Answered anyway", "status": "asserted",
		"attested_by": "s-deed"}, "carl", "")
	if err == nil {
		t.Fatal("an answer to a question nobody asked was accepted")
	}
	if !strings.Contains(err.Error(), "q-absent") {
		t.Errorf("the refusal must name what is missing: %v", err)
	}
	after, _ := st.All()
	if len(after) != len(before) {
		t.Errorf("a refused answer still wrote %d line(s)", len(after)-len(before))
	}
}
