package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE TEST THE WHOLE MIGRATION RESTS ON.
//
// A log folded into a graph must be INDISTINGUISHABLE from the same facts
// parsed out of a `*.kfacts.md`, and `SemHash` is what "indistinguishable"
// means here: it covers content, incident edges and a source's authored
// provenance, so if every node's hash matches, nothing downstream — staleness,
// drift, a standing query's pin — can tell which door the facts came through.
//
// Asserting that in a comment would be worthless. This computes it.
func TestFoldedLogMatchesParsedFactsBySemHash(t *testing.T) {
	src := "# f\n\n```kfacts\n" +
		"- id: s-deed\n  record: The 1984 deed\n  doc: deed.md\n  class: record\n" +
		"- id: c-one\n  claim: The strip is held by Halloway\n  status: asserted\n  attested_by: s-deed\n" +
		"- id: c-two\n  claim: The easement is terminable\n  status: proposed\n  supports: c-one\n" +
		"- id: q-open\n  question: Who surveyed the strip\n  status: open\n" +
		"```\n"
	parsed, ds := parseFixture("f.facts", []byte(src))
	if errs := Errors(ds); len(errs) > 0 {
		t.Fatalf("fixture does not parse: %v", errs)
	}

	// The same facts, asserted. Field-for-field the same vocabulary, which is the
	// point of the payload not being a typed struct.
	log := []Assertion{
		{Op: OpAssert, ID: "s-deed", By: "carl", At: "2026-08-30T10:00:00Z", Fields: map[string]any{
			"record": "The 1984 deed", "doc": "deed.md", "class": "record"}},
		{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T10:00:01Z", Fields: map[string]any{
			"claim": "The strip is held by Halloway", "status": "asserted", "attested_by": "s-deed"}},
		{Op: OpAssert, ID: "c-two", By: "carl", At: "2026-08-30T10:00:02Z", Fields: map[string]any{
			"claim": "The easement is terminable", "status": "proposed", "supports": "c-one"}},
		{Op: OpAssert, ID: "q-open", By: "carl", At: "2026-08-30T10:00:03Z", Fields: map[string]any{
			"question": "Who surveyed the strip", "status": "open"}},
	}
	folded, fds := FoldAssertions(legal, "facts.jsonl", log)
	if errs := Errors(fds); len(errs) > 0 {
		t.Fatalf("the fold does not parse: %v", errs)
	}

	a, da := BuildWith([]*Doc{parsed}, AttestSet{})
	b, db := BuildWith([]*Doc{folded}, AttestSet{})
	if errs := Errors(da); len(errs) > 0 {
		t.Fatalf("parsed graph: %v", errs)
	}
	if errs := Errors(db); len(errs) > 0 {
		t.Fatalf("folded graph: %v", errs)
	}
	if len(a.Nodes) != len(b.Nodes) {
		t.Fatalf("node counts differ: parsed %d, folded %d", len(a.Nodes), len(b.Nodes))
	}
	for id, want := range a.SemHash {
		got, ok := b.SemHash[id]
		if !ok {
			t.Errorf("%s is missing from the folded graph", id)
			continue
		}
		if got != want {
			t.Errorf("%s hashes differently:\n  parsed %s\n  folded %s", id, want, got)
		}
	}
	// Edges too. SemHash covers INCIDENT edges, so a mismatch there would already
	// show above — but a count check catches an edge duplicated on both ends,
	// which hashes the same and is still wrong.
	if len(a.Edges) != len(b.Edges) {
		t.Errorf("edge counts differ: parsed %d, folded %d", len(a.Edges), len(b.Edges))
	}
}

// The log is REPLAYED, not merely read: a withdrawal is a later line about an
// earlier fact, and the fold is what turns two lines into one node.
func TestWithdrawalIsALaterLineNotAnEdit(t *testing.T) {
	log := []Assertion{
		{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T10:00:00Z", Fields: map[string]any{
			"claim": "The strip is held by Halloway", "status": "asserted"}},
		{Op: OpWithdraw, ID: "c-one", By: "carl", At: "2026-09-02T09:12:00Z",
			Reason: "the deed describes the adjoining parcel"},
	}
	doc, ds := FoldAssertions(legal, "facts.jsonl", log)
	if errs := Errors(ds); len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	if len(doc.Nodes) != 1 {
		t.Fatalf("want one node, got %d", len(doc.Nodes))
	}
	n := doc.Nodes[0]
	if n.Status != SWithdrawn {
		t.Errorf("status is %q, want withdrawn", n.Status)
	}
	// The reason survives, and it is required: `withdrawn` on its own is a shrug,
	// and somebody else has to be able to check the correction.
	if !strings.Contains(n.Reason, "adjoining parcel") {
		t.Errorf("the withdrawal's reason was lost: %q", n.Reason)
	}
	// And the FACT is still there. `withdrawn` is "we were wrong", not "delete
	// it" — references to it keep resolving, which is why the log never removes.
	if n.Body == "" {
		t.Error("withdrawing emptied the node")
	}
}

// Two machines that appended in different orders must fold to the same graph, or
// this is not a store. Ordered by `At`, position breaking a tie — Syncthing
// merges by line, so file order is what both machines end up with.
func TestTheFoldIsOrderIndependent(t *testing.T) {
	early := Assertion{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T10:00:00Z",
		Fields: map[string]any{"claim": "first", "status": "asserted"}}
	late := Assertion{Op: OpAssert, ID: "c-one", By: "thorne", At: "2026-08-30T11:00:00Z",
		Fields: map[string]any{"claim": "second", "status": "asserted"}}

	forward, _ := FoldAssertions(legal, "facts.jsonl", []Assertion{early, late})
	reversed, _ := FoldAssertions(legal, "facts.jsonl", []Assertion{late, early})
	if len(forward.Nodes) != 1 || len(reversed.Nodes) != 1 {
		t.Fatal("want one node from each")
	}
	if forward.Nodes[0].Body != "second" {
		t.Errorf("last write must win, got %q", forward.Nodes[0].Body)
	}
	if forward.Nodes[0].Body != reversed.Nodes[0].Body {
		t.Errorf("file order changed the answer: %q vs %q",
			forward.Nodes[0].Body, reversed.Nodes[0].Body)
	}
}

// An assert REPLACES the node rather than merging into it. A field-by-field
// merge would make "this field is absent now" unsayable, and an absent field is
// how a fact stops claiming something.
func TestAssertReplacesRatherThanMerges(t *testing.T) {
	log := []Assertion{
		{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T10:00:00Z", Fields: map[string]any{
			"claim": "The strip is held by Halloway", "status": "asserted", "owner": "e-errol"}},
		{Op: OpAssert, ID: "c-one", By: "carl", At: "2026-08-30T11:00:00Z", Fields: map[string]any{
			"claim": "The strip is held by Halloway", "status": "asserted"}},
	}
	doc, ds := FoldAssertions(legal, "facts.jsonl", log)
	if errs := Errors(ds); len(errs) > 0 {
		t.Fatalf("%v", errs)
	}
	if doc.Nodes[0].Owner != "" {
		t.Errorf("the dropped field survived: owner=%q", doc.Nodes[0].Owner)
	}
}

// Every guard the store has, from the door.
func TestAnAssertionIsValidated(t *testing.T) {
	cases := []struct {
		name string
		a    Assertion
		want string
	}{
		{"unknown op", Assertion{Op: "delete", ID: "c-one", By: "carl"}, "unknown op"},
		{"no id", Assertion{Op: OpAssert, By: "carl"}, "needs an id"},
		{"unsigned", Assertion{Op: OpAssert, ID: "c-one",
			Fields: map[string]any{"claim": "x"}}, "unsigned"},
		{"empty assert", Assertion{Op: OpAssert, ID: "c-one", By: "carl"}, "says nothing"},
		{"withdraw with no reason", Assertion{Op: OpWithdraw, ID: "c-one", By: "carl"}, "needs a reason"},
		{"retire with no target", Assertion{Op: OpRetire, ID: "c-one", By: "carl"}, "folds into"},
		{"retire into itself", Assertion{Op: OpRetire, ID: "c-one", Into: "c-one", By: "carl"}, "itself"},
	}
	for _, c := range cases {
		err := c.a.validate()
		if err == nil {
			t.Errorf("%s must be refused", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q in %q", c.name, c.want, err)
		}
	}
}

// Append, read back, fold. The round trip is the store.
func TestTheLogRoundTrips(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	want := []Assertion{
		{Op: OpAssert, ID: "s-deed", By: "carl", Fields: map[string]any{
			"record": "The 1984 deed", "doc": "deed.md", "class": "record"},
			Note: "the auditor's certified copy, not the one inside Rowe's declaration"},
		{Op: OpAssert, ID: "c-one", By: "agent-scout", Fields: map[string]any{
			"claim": "The strip is held by Halloway", "status": "asserted", "attested_by": "s-deed"}},
	}
	for _, a := range want {
		if a.At == "" {
			a.At = "2026-08-30T10:00:00Z"
		}
		if err := st.Append(a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d assertions, want 2", len(got))
	}
	if got[0].At == "" {
		t.Error("the assertion lost its time")
	}
	// THE NOTE SURVIVES. It is the prose a *.kfacts.md carried in its markdown and
	// the one thing this store would otherwise have lost.
	if !strings.Contains(got[0].Note, "Rowe") {
		t.Errorf("the note was lost: %q", got[0].Note)
	}
	// A machine identity round-trips as itself, which is what routes a swarm's
	// output to the human queue rather than into the record unchallenged.
	if got[1].By != "agent-scout" {
		t.Errorf("the signer was lost: %q", got[1].By)
	}
	if !(Attestation{By: got[1].By}).MachineAttested() {
		t.Error("agent-scout must read as a machine identity")
	}
}

// A malformed line is an error, not a skipped line. Silently dropping one loses
// a fact while the corpus reports itself complete, which is the failure mode
// every store here is built to refuse.
func TestAMalformedLogLineIsAnError(t *testing.T) {
	root := t.TempDir()
	body := `{"op":"assert","id":"c-one","by":"carl","fields":{"claim":"ok"}}
{"op":"assert","id":"c-two"}
`
	if err := os.WriteFile(filepath.Join(root, assertName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadAssertionLog(filepath.Join(root, assertName))
	if err == nil {
		t.Fatal("an unsigned line must fail the read")
	}
	if !strings.Contains(err.Error(), ":2:") {
		t.Errorf("the error must name the line, got %q", err)
	}
}
