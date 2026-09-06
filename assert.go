package kgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// THE ASSERTION LOG. Facts as they are put in, not as they end up.
//
// `*.kfacts.md` is a MUTABLE FILE HOLDING AN IMMUTABLE DATA MODEL, and that
// mismatch has been paid for the whole time. The format already says facts are
// immutable: a correction is a new node plus `supersedes` plus a reason, the old
// node moves to `withdrawn`, never edited in place. An append-only log is that
// model written down. A markdown file anyone can retype is the same model with a
// hole in it.
//
// See `plan/facts.md` for the decision and what it replaces. In short: the
// objection to leaving text was about a DATABASE replacing a text file —
// Syncthing corruption, and `git blame` on a contested fact. Neither is about
// authoring. So the store stays text, stays in git, and stops being authored.
//
// The precedent is `attestations.jsonl`, chosen append-only for this exact
// reason: two syncing machines rewriting one JSON object is a merge conflict and
// a lost verdict, while two appending lines is a merge a person can read.
// `standing.yaml` rewrites for the opposite and equally deliberate reason — an
// answer is REPLACED, not accumulated. Facts accumulate.

// AssertOp is what one line does.
type AssertOp string

const (
	// OpAssert states a fact. The whole node, in the same field vocabulary a
	// `*.kfacts.md` entry uses — see Assertion.Fields.
	OpAssert AssertOp = "assert"
	// OpWithdraw says WE WERE WRONG. Not `false`, which is tested-and-rejected and
	// kept so `prohibits` has a target; the two are different and conflating them
	// churns every document that renders a theory.
	OpWithdraw AssertOp = "withdraw"
	// OpRetire folds a duplicate away into another id, the `same_as:` idiom.
	// References to the retired id keep resolving, which is the point — deleting
	// the loser outright breaks them.
	OpRetire AssertOp = "retire"
)

func (o AssertOp) valid() bool {
	switch o {
	case OpAssert, OpWithdraw, OpRetire:
		return true
	}
	return false
}

// Assertion is one line of the log: somebody put something in, and this is what
// and when and who.
type Assertion struct {
	Op AssertOp `json:"op"`
	ID string   `json:"id"`

	// Fields is the node's authored content, in THE SAME VOCABULARY as a
	// `*.kfacts.md` entry — `claim:`, `status:`, `attested_by:`, `doc:`, `class:`,
	// a dialect's own keys and subtypes, all of it.
	//
	// DELIBERATELY NOT A TYPED STRUCT, and this is the load-bearing choice in the
	// whole file. A typed record would be a SECOND PARSER, and the second parser
	// is the one that drifts: a dialect adds a relation or a subtype key and the
	// log silently cannot carry it. Keeping the payload in the authored vocabulary
	// means the fold below hands it to the parser that already exists, so an
	// assertion cannot mean something a fact file could not.
	Fields map[string]any `json:"fields,omitempty"`

	// Reason is required by OpWithdraw and OpRetire. "withdrawn: true" is a shrug;
	// "the deed describes the adjoining parcel" is the correction somebody else
	// can check. Same argument as `Underdetermined`, which is also required prose.
	Reason string `json:"reason,omitempty"`

	// Into is the surviving id for OpRetire.
	Into string `json:"into,omitempty"`

	// Note is the prose that has nowhere else to go.
	//
	// THIS IS THE ONE REAL REGRESSION THE STORE WOULD OTHERWISE HAVE. A
	// `*.kfacts.md` carries explanation in the markdown around its YAML — why this
	// fact matters, what was nearly written instead, which reading of an ambiguous
	// document was taken — and that prose is routinely the most valuable thing in
	// the file. JSONL has nowhere to put it, so this is where it goes. Same shape
	// and same purpose as an attestation's `Note`.
	//
	// It is NOT `reason:` on the node. `reason:` is content and reaches SemHash; a
	// note is about the ACT of asserting and must not, or an editorial aside would
	// flag every answer resting on the fact.
	Note string `json:"note,omitempty"`

	// By is who asserted this, and it is REQUIRED for the reason `kg attest`
	// requires it: an unsigned record says nobody did it. A person's name, or a
	// machine identity — see MachineAttested, which is what routes a swarm's
	// output to the human queue rather than into the record unchallenged.
	By string `json:"by"`
	// At is WALL CLOCK: when the assertion was made. Not the fact's own date,
	// which lives in `Fields["at"]`. The two are different questions and a single
	// field has confused them before.
	At string `json:"at"`
}

const assertName = "facts.jsonl"

// jsonMarshal is json.Marshal, named so tests can emit a log line without
// importing encoding/json in every file that builds a corpus.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// ReadAssertionLog reads an exported log file.
//
// ONE reader, used by `ImportJSONL` and by anything that wants to look at an
// export without a store. There was briefly a second — `ReadAssertions` walking
// the same lines with its own loop — which is exactly the two-idioms-for-one-
// thing this format refuses everywhere else, and it survived only as long as the
// JSONL was the store.
//
// A malformed line is an ERROR naming its line, not a skipped line. Silently
// dropping one loses a fact while the corpus reports itself complete.
func ReadAssertionLog(path string) ([]Assertion, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Assertion
	for i, ln := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		var a Assertion
		if err := json.Unmarshal([]byte(t), &a); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", filepath.Base(path), i+1, err)
		}
		if err := a.validate(); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", filepath.Base(path), i+1, err)
		}
		out = append(out, a)
	}
	return out, nil
}

func (a Assertion) validate() error {
	if !a.Op.valid() {
		return fmt.Errorf("unknown op %q", a.Op)
	}
	if a.ID == "" {
		return fmt.Errorf("an assertion needs an id")
	}
	if strings.TrimSpace(a.By) == "" {
		return fmt.Errorf("%s of %q is unsigned — an unsigned record says nobody did it", a.Op, a.ID)
	}
	switch a.Op {
	case OpAssert:
		if len(a.Fields) == 0 {
			return fmt.Errorf("assert of %q says nothing", a.ID)
		}
	case OpWithdraw:
		if strings.TrimSpace(a.Reason) == "" {
			return fmt.Errorf("withdrawing %q needs a reason — `withdrawn` on its own is a shrug, "+
				"and somebody else has to be able to check the correction", a.ID)
		}
	case OpRetire:
		if a.Into == "" {
			return fmt.Errorf("retiring %q needs the id it folds into", a.ID)
		}
		if a.Into == a.ID {
			return fmt.Errorf("%q cannot be retired into itself", a.ID)
		}
	}
	return nil
}

// ── the fold ───────────────────────────────────────────────────────────

// FoldAssertions replays a log into the document a parser would have produced.
//
// IT DOES NOT BUILD NODES. It folds the log into the authored field maps and
// hands them to `ParseFactsIn` — the same parser, the same dialect, the same
// guards. A fold that constructed `Node` values directly would be a second
// implementation of the format, and it is the second implementation that drifts:
// every idiom check, every inverse-key rejection, every subtype rule would have
// to be remembered here, and one of them would not be.
//
// So the log's payload is authored vocabulary and this function is a merge, not
// a compiler. That is also what makes the migration provable: `kg migrate` writes
// the log FROM the files, and if the folded graph's SemHashes match the parsed
// one's, nothing downstream can tell the difference.
//
// LAST WRITE PER ID WINS, ordered by `At` then by position. Two machines that
// appended in different orders must fold to the same graph or this is not a
// store. Position breaks a tie because two assertions can share a timestamp —
// Syncthing merges by line, so the file order is what both machines end up with.
func FoldAssertions(dl Dialect, path string, log []Assertion) (*Doc, []Diag) {
	type slot struct {
		fields map[string]any
		at     string
		pos    int
		live   bool
	}
	order := []string{}
	byID := map[string]*slot{}

	idx := make([]int, 0, len(log))
	for i := range log {
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool {
		if log[idx[a]].At != log[idx[b]].At {
			return log[idx[a]].At < log[idx[b]].At
		}
		return idx[a] < idx[b]
	})

	var diags []Diag
	for _, i := range idx {
		a := log[i]
		s, ok := byID[a.ID]
		if !ok {
			s = &slot{fields: map[string]any{}, pos: len(order)}
			byID[a.ID] = s
			order = append(order, a.ID)
		}
		switch a.Op {
		case OpAssert:
			// The whole node, replaced. NOT merged field-by-field: a partial update
			// would make "this field is absent now" unsayable, and an absent field is
			// how a fact stops claiming something.
			next := map[string]any{}
			for k, v := range a.Fields {
				next[k] = v
			}
			next["id"] = a.ID
			s.fields, s.at, s.live = next, a.At, true
		case OpWithdraw:
			if !s.live {
				diags = append(diags, Diag{File: path, Line: 0, Severity: SevWarn, Msg: fmt.Sprintf(
					"%s withdraws %q, which was never asserted here", a.At, a.ID)})
				continue
			}
			s.fields["status"] = string(SWithdrawn)
			s.fields["reason"] = a.Reason
		case OpRetire:
			if !s.live {
				diags = append(diags, Diag{File: path, Line: 0, Severity: SevWarn, Msg: fmt.Sprintf(
					"%s retires %q, which was never asserted here", a.At, a.ID)})
				continue
			}
			s.fields["same_as"] = a.Into
		}
	}

	seq := make([]map[string]any, 0, len(order))
	for _, id := range order {
		if s := byID[id]; s.live {
			seq = append(seq, s.fields)
		}
	}
	if len(seq) == 0 {
		return &Doc{Path: path}, diags
	}
	// Marshalled to YAML and handed to the FORMAT parser, not to the file parser.
	//
	// It used to wrap this in a ```kfacts fence and call `ParseFactsIn`, which
	// worked and was a tell: the fold was pretending to be a markdown file to
	// reach the only entry point that existed. `parseEntries` is that entry point
	// without the costume, and drawing that line is what let the markdown reader
	// be deleted without touching a single rule.
	body, err := yaml.Marshal(seq)
	if err != nil {
		return &Doc{Path: path}, append(diags, Diag{File: path, Line: 0, Severity: SevError, Msg: err.Error()})
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return &Doc{Path: path}, append(diags, Diag{File: path, Line: 0, Severity: SevError, Msg: err.Error()})
	}
	if len(root.Content) == 0 {
		return &Doc{Path: path}, diags
	}
	doc := &Doc{Path: path}
	ds := parseEntries(dl, path, 1, root.Content[0], doc, map[string]int{})
	return doc, append(diags, ds...)
}
