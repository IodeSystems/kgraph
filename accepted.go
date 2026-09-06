package kgraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// THE FINDINGS LEDGER. "I have seen this, and it is fine."
//
// kgraph's detection is finished; its triage was not started. On the live corpus
// a scan produces 615 warnings, and a checker whose output nobody can read is
// not a guardrail, it is wallpaper. What was missing is the other half of every
// check: a signed record that a person looked at a finding and accepted it.
//
// THE PRECEDENT IS `sources.lock`, which is "I have seen this version of this
// document." An acceptance is the same act on a finding and takes the same
// shape — per-index, text, and keyed to content rather than to position.

const acceptedName = "accepted.jsonl"

// Check names — the identity of a RULE, stable across message rewording.
//
// A finding's key is built from the check plus its subjects, never from the
// message text: editing the wording of a warning must not silently retire every
// ruling somebody made about it.
const (
	CheckUnreferencedSource = "unreferenced-source"
	CheckSplitContradiction = "split-contradiction"
	CheckGraphShrank        = "graph-shrank"
	CheckNearDuplicate      = "near-duplicate"
	CheckSameFileSources    = "same-file-sources"
	CheckUnsourcedFact      = "unsourced-fact"
	CheckLongDescription    = "long-description"
	CheckInterestedNoBy     = "interested-no-speaker"
	CheckInferenceNoPremise = "inference-no-premises"
	CheckAlreadyHeld        = "already-held"
	CheckAttestedExcerpt    = "attested-excerpt-moved"
	CheckAliasAmbiguous     = "alias-ambiguous"
	CheckEmptyQuery         = "empty-query"
)

// knownChecks is every rule an index may tune, with what it is for.
//
// A REGISTRY RATHER THAN A FREE STRING, for the reason
// `TestInstallableNamesAreAllDispatched` exists: a rule naming a check that does
// not exist silently does nothing, and "I turned that off" then becomes a belief
// rather than a fact. A typo is refused at load, naming the file and listing
// what would be accepted.
//
// Only checks that emit WARNINGS appear here. An error is never tunable — a
// dangling reference in a legal corpus gets fixed, and the one severity that
// must be trusted stays non-negotiable. Same line the findings ledger draws.
var knownChecks = map[string]string{
	CheckUnreferencedSource: "a source nothing cites — evidence entered and never used",
	CheckSplitContradiction: "a set carries one end of a correction and nothing beside it carries the other",
	CheckGraphShrank:        "the index holds less than half the nodes it once held",
	CheckNearDuplicate:      "two nodes that may be one thing entered twice",
	CheckSameFileSources:    "several sources registered against one document",
	CheckUnsourcedFact:      "a live fact resting on no source at all",
	CheckLongDescription:    "a source description long enough to be argument rather than naming",
	CheckInterestedNoBy:     "a claim about whose interest it serves, with no `by:` naming whose",
	CheckInferenceNoPremise: "an inference declaring no `derived_from` — provenance with no premises",
	CheckAlreadyHeld:        "a question asking for a document the corpus already holds",
	CheckAttestedExcerpt:    "a verdict whose passage has moved under it",
	CheckEmptyQuery:         "a declared query resolving to no rows",
	CheckAliasAmbiguous:     "one alias denoting different things over time",
}

// CheckNames lists the tunable rules, sorted, for error messages and docs.
func CheckNames() []string {
	out := make([]string, 0, len(knownChecks))
	for n := range knownChecks {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// RuleLevel is what an index does with a check.
type RuleLevel string

const (
	// RuleError raises a warning to an error, so it fails the build. An index
	// that cannot tolerate unused evidence says so here.
	RuleError RuleLevel = "error"
	// RuleWarn is the default: reported, does not fail.
	RuleWarn RuleLevel = "warn"
	// RuleOff suppresses the check entirely.
	//
	// THIS IS THE CLASS-LEVEL DOOR, and it exists so the findings ledger is not
	// used as one. Accepting 407 findings one at a time is absurd; accepting them
	// as a class is disabling the check, and that should be declared out loud in
	// the index's own dialect where a reader sees it — not inferred from the
	// volume of a ledger.
	RuleOff RuleLevel = "off"
)

// Accepted is one person's ruling that a warning is tolerable.
//
// It carries the whole finding, not just the key. A ledger of opaque hashes is
// unreadable, un-reviewable and unmergeable by a human, and this file goes to
// git beside the corpus like every other record here.
type Accepted struct {
	Key     string `json:"key"`
	Check   string `json:"check"`
	Msg     string `json:"msg"`
	By      string `json:"by"`
	At      string `json:"at"`
	Reason  string `json:"reason,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// machineAccepted reports whether a process rather than a person accepted this.
//
// SAME PREDICATE, SAME REASON as `Attestation.MachineAttested`, one level up: an
// agent that can silence the corpus's own complaints is the hazard the whole
// human-in-the-loop design exists to prevent. A machine acceptance is kept and
// reported — never honoured.
func (a Accepted) machineAccepted() bool {
	for _, p := range machinePrefixes {
		if strings.HasPrefix(a.By, p) {
			return true
		}
	}
	return false
}

// Honoured reports whether this acceptance actually suppresses its finding.
func (a Accepted) Honoured() bool { return a.By != "" && !a.machineAccepted() }

// AcceptedSet is the ledger resolved: the latest entry per key.
type AcceptedSet map[string]Accepted

// ReadAccepted loads an index's ledger. Missing is not an error: a corpus nobody
// has triaged is the normal starting state.
//
// APPEND-ONLY, LAST ENTRY WINS — `attestations.jsonl`'s discipline, for its
// reason: two machines may record independently and a merge must not lose one.
func ReadAccepted(root, dir string) (AcceptedSet, error) {
	f, err := os.Open(filepath.Join(root, dir, acceptedName))
	if err != nil {
		if os.IsNotExist(err) {
			return AcceptedSet{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := AcceptedSet{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var a Accepted
		if err := json.Unmarshal([]byte(line), &a); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", acceptedName, n, err)
		}
		if a.Key == "" {
			return nil, fmt.Errorf("%s:%d: entry has no key", acceptedName, n)
		}
		out[a.Key] = a
	}
	return out, sc.Err()
}

// AppendAccepted records one acceptance.
func AppendAccepted(root, dir string, a Accepted) error {
	if a.By == "" {
		return fmt.Errorf("who is accepting? an unsigned acceptance says nobody looked")
	}
	line, err := json.Marshal(a)
	if err != nil {
		return err
	}
	abs := filepath.Join(root, dir, acceptedName)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if fi, serr := f.Stat(); serr == nil && fi.Size() == 0 {
		if _, err := f.WriteString(
			"// kgraph accepted findings — warnings a person has read and ruled tolerable.\n" +
				"// Keyed by CONTENT: the check, its subjects, and their sem_hashes. Edit a fact\n" +
				"// the finding names and the key changes, so the finding comes back on its own.\n"); err != nil {
			return err
		}
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Partition splits diagnostics into what still needs looking at and what has
// been accepted.
//
// ERRORS ARE NEVER ACCEPTABLE and are never partitioned away. A dangling
// reference in a legal corpus gets fixed; letting it be signed off would make
// the one severity that must be trusted negotiable.
func (set AcceptedSet) Partition(ds []Diag) (open, accepted []Diag) {
	for _, d := range ds {
		if d.Severity != SevWarn || d.Key == "" {
			open = append(open, d)
			continue
		}
		if a, ok := set[d.Key]; ok && a.Honoured() {
			accepted = append(accepted, d)
			continue
		}
		open = append(open, d)
	}
	return open, accepted
}

// Unhonoured returns acceptances that do NOT suppress anything, so a machine
// signature is reported rather than silently ignored.
func (set AcceptedSet) Unhonoured() []Accepted {
	var out []Accepted
	for _, a := range set {
		if !a.Honoured() {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// FindingKeyOf is findingKey for a caller outside this package — the CLI emits
// the empty-query finding, because only it holds the resolved declarations.
//
// A NAME rather than a node id is a legitimate subject: the finding is about a
// DECLARATION, and the declaration is the thing a person would rule on. It gets
// no sem_hash for the same reason — a query has no node to hash — so this key
// changes when the query is renamed and not when the corpus moves, which is
// correct: a query that has resolved to nothing since it was written is still
// resolving to nothing.
func FindingKeyOf(g *Graph, check string, subjects ...string) string {
	return findingKey(g, check, subjects...)
}

// findingKey identifies a finding by CONTENT, never by position.
//
// THE SEM_HASH OF EVERY SUBJECT GOES IN, and that is the whole safety property:
// accepting is accepting THIS SITUATION, so any edit to a fact the finding names
// yields a different key and the finding returns on its own. No separate
// staleness check and no stale acceptance — the discipline `sources.lock` uses
// for documents, reached the same way.
//
// A line number would be the obvious key and is exactly wrong: an unrelated edit
// higher in the file would silently retire somebody's ruling.
func findingKey(g *Graph, check string, subjects ...string) string {
	parts := append([]string{}, subjects...)
	sort.Strings(parts)
	var b strings.Builder
	b.WriteString(check)
	for _, id := range parts {
		b.WriteByte(0)
		b.WriteString(id)
		b.WriteByte('@')
		if g != nil {
			b.WriteString(g.SemHash[id])
		}
	}
	return hashString(b.String())
}

// Triage splits a project's diagnostics into what still wants looking at and
// what has been accepted.
//
// The single door, so the CLI, the daemon and any consumer agree about what
// "outstanding" means — the same reason `Ask` is the one standard op for drift.
func (p *Project) Triage(ds []Diag) (open, accepted []Diag) {
	return p.Accepted.Partition(ds)
}
