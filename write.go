package kgraph

import (
	"fmt"
	"strings"
	"time"
)

// THE WRITE OPS. One door, and everything goes through it.
//
// They take a `Store` rather than a path, which is the seam the storage swap
// went through: these were written against "read the log, append to the log"
// before there was a database, so making that an interface changed the
// signatures and nothing else.
//
// A UI that can write a graph the CLI refuses to load is a UI that corrupts a
// corpus, so a write is not "append a line" — it is: fold the log this line
// would join, build the graph that results, and append only if it still builds.
// The alternative is a store that accepts anything and a `kg scan` that reports
// the damage afterwards, which is the same shape as a document reporting itself
// fresh: detected too late to be worth detecting.
//
// ERRORS REFUSE, WARNINGS DO NOT. A corpus carries warnings by design — thirty
// facts resting on no source is a backlog, not a fault — and a writer that
// blocked on those would block every assertion in a real matter. The line is the
// same one `load` draws: errors are things the graph cannot mean.

// WriteResult is what a write did, and what the graph now says about itself.
type WriteResult struct {
	// Applied is the assertions actually appended.
	Applied []Assertion
	// Warnings are the diagnostics the resulting graph carries. Returned rather
	// than swallowed: a caller that just asserted a fact resting on nothing should
	// be told so at the moment they can still fix it.
	Warnings []Diag
}

// Apply validates assertions against the log they would join and appends them
// only if the resulting graph builds.
//
// ALL OR NOTHING. `Correct` is two lines — a withdrawal and its replacement —
// and a half-applied correction leaves a fact withdrawn with nothing standing in
// its place, which is strictly worse than the wrong fact it was fixing. So the
// whole batch is validated together and written together.
func Apply(st Store, dl Dialect, as ...Assertion) (*WriteResult, error) {
	if len(as) == 0 {
		return &WriteResult{}, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range as {
		if as[i].At == "" {
			as[i].At = now
		}
		if err := as[i].validate(); err != nil {
			return nil, err
		}
	}
	existing, err := st.All()
	if err != nil {
		return nil, err
	}
	doc, fds := FoldAssertions(dl, assertName, append(existing, as...))
	if errs := Errors(fds); len(errs) > 0 {
		return nil, fmt.Errorf("that would not load:\n%s", diagLines(errs))
	}
	g, bds := BuildWith([]*Doc{doc}, AttestSet{})
	_ = g
	all := append(fds, bds...)
	if errs := Errors(all); len(errs) > 0 {
		return nil, fmt.Errorf("that would not load:\n%s", diagLines(errs))
	}
	for _, a := range as {
		if err := st.Append(a); err != nil {
			// A partial write here is the one failure this cannot fully prevent —
			// the file is append-only and the second line may fail on I/O after the
			// first landed. Reported loudly rather than hidden, because the fold is
			// deterministic and a person can see exactly what is there.
			return nil, fmt.Errorf("wrote %d of %d assertions, then: %w", len(as), len(as), err)
		}
	}
	return &WriteResult{Applied: as, Warnings: warningsOf(all)}, nil
}

// Assert states a fact, or restates one. The fields are the authored vocabulary
// a `*.kfacts.md` entry uses; see Assertion.Fields.
func Assert(st Store, dl Dialect, id string, fields map[string]any, by, note string) (*WriteResult, error) {
	return Apply(st, dl, Assertion{
		Op: OpAssert, ID: id, Fields: fields, By: by, Note: note})
}

// Withdraw says WE WERE WRONG about a fact.
//
// Not `false`, which is tested-and-rejected and kept so `prohibits` has a
// target. The two are different and conflating them churns every answer that
// rests on a theory. The reason is required: `withdrawn` on its own is a shrug,
// and somebody else has to be able to check the correction.
func Withdraw(st Store, dl Dialect, id, reason, by, note string) (*WriteResult, error) {
	return Apply(st, dl, Assertion{
		Op: OpWithdraw, ID: id, Reason: reason, By: by, Note: note})
}

// Correct is the whole correction, atomically: the old fact withdrawn, and a new
// one asserted in its place carrying `supersedes`.
//
// TWO LINES, ONE ACT. This is the operation the format has always described —
// "a correction is a new node plus `supersedes` plus a reason, old node
// withdrawn, never edited in place" — and until now it was a thing a person did
// by hand in three edits, any of which could be forgotten. The commonest
// forgetting was the `supersedes`, which leaves the corpus holding both facts
// with nothing saying which won.
func Correct(st Store, dl Dialect, oldID, newID string, fields map[string]any, reason, by, note string) (*WriteResult, error) {
	if oldID == newID {
		return nil, fmt.Errorf("a correction needs a NEW id — %q cannot supersede itself, and "+
			"reusing the id is the in-place edit this exists to avoid", oldID)
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("a correction needs a reason — it is what the withdrawal records " +
			"and what somebody else checks it against")
	}
	next := map[string]any{}
	for k, v := range fields {
		next[k] = v
	}
	// Written by the op rather than left to the caller. A correction whose
	// replacement does not say what it replaces is the failure mode this
	// operation exists to remove, so it is not a field somebody can omit.
	next["supersedes"] = oldID
	// The withdrawal is stamped a tick earlier so the fold sees it before the
	// replacement regardless of how the two lines end up ordered on disk.
	now := time.Now().UTC()
	return Apply(st, dl,
		Assertion{Op: OpWithdraw, ID: oldID, Reason: reason, By: by, Note: note,
			At: now.Format(time.RFC3339)},
		Assertion{Op: OpAssert, ID: newID, Fields: next, By: by, Note: note,
			At: now.Add(time.Second).Format(time.RFC3339)})
}

// AnswerQuestion records an answer to an open question.
//
// Named for the question rather than the answer because `Answer` is already a
// standing query's resolved result, and two things called Answer in one package
// is the sort of collision that gets resolved by whichever one somebody imports
// first.
//
// A node with `answers:` FIRST — what closes a question is a fact that can
// itself be weighed, sourced and withdrawn, and a question flipped to `resolved`
// with nothing attached is an answer nobody can check.
//
// AND THE QUESTION IS CLOSED IN THE SAME ACT (2026-09-01). Writing only the edge
// left `status: open` on an answered question forever, so `question[status=open]`
// went on matching it and every open-questions query over-reported — silently,
// and in every consumer. Reported from the `aw4` corpus and confirmed here.
//
// This does not relax the rule above, it satisfies it: the objection is to a
// status flipped with NOTHING ATTACHED, and here the answer is attached in the
// same `Apply` — one act, validated together, refused together. The codebase
// already modelled it this way and only the write was missing: `taint.go`
// simulates answering as the option asserted AND the question `SResolved`, so
// `kg variants` was predicting a state that answering never produced.
//
// The question's other fields come along because an assert REPLACES rather than
// merges — the same reason `Amend` exists. It is not reused here because it
// applies separately, and two acts can half-succeed.
func AnswerQuestion(st Store, dl Dialect, questionID, id string, fields map[string]any, by, note string) (*WriteResult, error) {
	next := map[string]any{}
	for k, v := range fields {
		next[k] = v
	}
	next["answers"] = questionID
	as := []Assertion{{Op: OpAssert, ID: id, Fields: next, By: by, Note: note}}

	// Closing it is best-effort by design. A question the store has never
	// asserted — one still living in an imported export — cannot be amended
	// without inventing its fields, and refusing the answer over that would make
	// the op unusable on exactly the corpora that most need answering.
	q, ok, err := LastFields(st, questionID)
	if err != nil {
		return nil, err
	}
	if _, isQuestion := q["question"]; ok && isQuestion && q["status"] != string(SResolved) {
		q["status"] = string(SResolved)
		as = append(as, Assertion{Op: OpAssert, ID: questionID, Fields: q, By: by,
			Note: note})
	}
	return Apply(st, dl, as...)
}

// Retire folds a duplicate id into the node that absorbed it.
//
// References to the retired id keep resolving, which is the point: deleting the
// loser outright breaks every fact that cited it, and only a person can say that
// two similar sentences are one fact.
func Retire(st Store, dl Dialect, id, into, by, note string) (*WriteResult, error) {
	return Apply(st, dl, Assertion{
		Op: OpRetire, ID: id, Into: into, By: by, Note: note})
}

func warningsOf(ds []Diag) []Diag {
	var out []Diag
	for _, d := range ds {
		if d.Severity == SevWarn {
			out = append(out, d)
		}
	}
	return out
}

func diagLines(ds []Diag) string {
	var b strings.Builder
	for i, d := range ds {
		if i == 6 {
			fmt.Fprintf(&b, "  … and %d more\n", len(ds)-i)
			break
		}
		fmt.Fprintf(&b, "  %s\n", d)
	}
	return b.String()
}
