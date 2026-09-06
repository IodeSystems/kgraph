package kgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// A STANDING QUERY is a query kgraph keeps: the text, and the keys it last
// resolved to. It is the whole product without the document layer.
//
// The generated-document pipeline answers "which documents need revising" by
// pinning a resolution inside each artifact's managed block. That works, and it
// makes the answer available only to something that generates artifacts. The
// question underneath it is smaller and stands on its own:
//
//	I asked this. Is the answer still the answer?
//
// A standing query records the QUERY and the RESULT KEYS, so three different
// things can be told apart, which a single "it changed" cannot:
//
//	query-changed     the question was edited; of course the answer moved
//	results-changed   the same question now returns a different set
//	source-changed    the same rows, but a document under them moved on disk
//
// The third is the one nothing else can see. Every row still matches, every
// `sem_hash` is identical, and the evidence has been re-scanned underneath —
// which is exactly the case `results-changed` structurally cannot report and the
// reason source drift is tracked apart from the semantic hash.
//
// The keys are a `Pin`, unchanged and deliberately so: it already records the
// rows in resolved order with their hashes, the open questions the set RESTS on,
// and the sources its rows cite. Those last two are not members of the set and
// were both learned the hard way — a set can be fully attested and still not
// settled, and reorganising a corpus can change what a result means while every
// pinned row still matches.
type Standing struct {
	Name string `yaml:"name"`
	// Group is the set of questions this one is asked ALONGSIDE, and it is
	// load-bearing rather than filing.
	//
	// A spec was a GROUP of queries with a purpose, and two checks depend on the
	// grouping — most of all `CheckSplitContradiction`, which reports that a set
	// carries one end of a correction and NO OTHER SET BESIDE IT carries the
	// other. That is a statement about a group; a flat standing query has no
	// sibling to reach. On the live corpus 119 of 619 warnings are that check, so
	// retiring specs without this field would have dropped a fifth of what the
	// corpus says, quietly.
	//
	// Empty is a group of one, which is honest: a question asked on its own has
	// nothing beside it, and the check says so rather than pretending otherwise.
	Group string `yaml:"group,omitempty"`
	// Purpose is why the question is asked, in the author's words. It carries no
	// machinery and is deliberately not hashed — editing it must not report the
	// answer as moved.
	//
	// It came across from `*.kgraph.md`, which is retiring (2026-09-01, USER). A
	// spec was `Purpose` plus named queries, and this was the only field of it a
	// standing query could not already express — the difference between a query
	// somebody MEANT and a query somebody typed. Losing it in the retirement
	// would have been the one real cost of that decision.
	Purpose string `yaml:"purpose,omitempty"`
	// Query is the DSL text, kept verbatim. Stored rather than referenced because
	// the point is to notice when it is EDITED, and a reference to a file that
	// changed underneath cannot report that.
	Query string `yaml:"query"`
	// QueryHash is what the pin below was taken under. Without it an edited query
	// reports `results-changed`, which reads as "the corpus moved" when the truth
	// is "you asked something else".
	QueryHash string `yaml:"query_hash"`
	// Pin is the answer as last acknowledged. Nil means never resolved, which is
	// not the same as an empty result and must never be reported as one.
	Pin *Pin `yaml:"pin,omitempty"`
	// DialectHash is the AUTHORED dialect the answer was accepted under, empty for
	// a built-in.
	//
	// It moved here when render retired. It used to live in a document's managed
	// block, because the ladder decides which of two conflicting sources wins and
	// therefore what the prose said about it. The dependency did not go away with
	// the prose: `claim[disputed]` and `claim[impeached]` resolve through the same
	// conflict machinery, so an edited ladder can change WHICH ROWS COME BACK
	// while the query text and every fact stand still.
	DialectHash string `yaml:"dialect_hash,omitempty"`
	// At and By are when the answer was accepted and by whom. An unacknowledged
	// standing query is a question nobody has looked at, and saying so is worth
	// more than a timestamp nobody wrote.
	At string `yaml:"at,omitempty"`
	By string `yaml:"by,omitempty"`
}

// StandingSet is a corpus's standing queries, by name.
type StandingSet map[string]*Standing

// QueryHashOf identifies a query by its TEXT, normalised for whitespace only.
//
// Not by its parsed AST: two spellings that resolve identically today are still
// two questions, and one of them may stop being the other when the evaluator
// grows. Normalising whitespace is safe because the DSL has no significant
// indentation; normalising anything else would start deciding that two questions
// are the same question, which is not a thing a hash may decide.
func QueryHashOf(q string) string {
	return hashString(strings.Join(strings.Fields(q), " "))
}

const standingName = "standing.yaml"

// ReadStanding loads an index's standing queries. Missing is not an error: a
// corpus nobody has asked anything of is the normal starting state.
func ReadStanding(root, dir string) (StandingSet, error) {
	b, err := os.ReadFile(filepath.Join(root, dir, standingName))
	if err != nil {
		if os.IsNotExist(err) {
			return StandingSet{}, nil
		}
		return nil, err
	}
	var list []*Standing
	if err := yaml.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", standingName, err)
	}
	out := StandingSet{}
	for i, st := range list {
		if st == nil || st.Name == "" {
			return nil, fmt.Errorf("%s: entry %d has no name", standingName, i+1)
		}
		if _, dup := out[st.Name]; dup {
			return nil, fmt.Errorf("%s: %q is defined twice", standingName, st.Name)
		}
		if st.Query == "" {
			return nil, fmt.Errorf("%s: %q has no query", standingName, st.Name)
		}
		out[st.Name] = st
	}
	return out, nil
}

// ReadStandingIn loads an index's standing queries by INDEX NAME rather than by
// directory, for callers that have not loaded the graph — `kg indexes` lists
// every index including ones it never builds.
func ReadStandingIn(root, index string) (StandingSet, error) {
	logs, err := FindFactLogs(root, index)
	if err != nil {
		return nil, err
	}
	dir := indexDirOfPaths(logs)
	return ReadStanding(root, dir)
}

// WriteStanding rewrites the file, sorted by name.
//
// A whole-file rewrite rather than the append-only form `attestations.jsonl`
// uses, and the difference is real: an attestation is a person's verdict that
// two machines may record independently and must never lose, so appending is how
// a Syncthing merge stays readable. A standing query is EDITED — its answer is
// replaced, not accumulated — so an append-only log of it would grow one entry
// per acknowledgement and mean nothing but the last line. Same treatment and
// same reasoning as `sources.lock`: a diff here is a question to re-ask.
func WriteStanding(root, dir string, set StandingSet) error {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	list := make([]*Standing, 0, len(names))
	for _, n := range names {
		list = append(list, set[n])
	}
	abs := filepath.Join(root, dir, standingName)
	if len(list) == 0 {
		// Nothing recorded: do not leave an empty file implying it was checked.
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	body, err := yaml.Marshal(list)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# kgraph standing queries — what was asked, and the answer on record.\n")
	b.WriteString("# A diff here is a question to re-ask, not a merge conflict.\n")
	b.Write(body)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(b.String()), 0o644)
}

// ── the standard op ────────────────────────────────────────────────────

// Answer is one standing query resolved NOW, with every reason the answer is not
// the one on record.
//
// Rows and drift come back TOGETHER and from one call on purpose. Two calls
// means two resolutions, and between them the graph may be rebuilt — so a caller
// could report drift computed against rows it never showed. It also means every
// consumer re-implements the pairing, and they will not agree.
type Answer struct {
	Name  string
	Query string
	// Rows is the result, in the query's declared order.
	Rows []string
	// Pin is the answer as it would be recorded if acknowledged now.
	Pin *Pin
	// Drift is empty when the answer on record is still the answer.
	Drift []string
	// DialectHash is the ladder this answer was resolved under, so acknowledging
	// records what it was actually computed with rather than re-reading it.
	DialectHash string
	// Delta says WHAT moved, not merely that something did. Zero-valued when
	// there is nothing on record to compare against.
	Delta SetDelta
}

// Stale reports whether anything moved.
func (a *Answer) Stale() bool { return len(a.Drift) > 0 }

// Ask resolves a standing query and reports both the rows and the drift.
//
// THIS IS THE STANDARD OP. Everything that wants to know whether an answer moved
// goes through it — the CLI, the daemon, the MCP surface — so that "changed"
// means one thing across all of them.
//
// `never-asked` is reported instead of a delta when nothing is on record, and it
// is not the same as an empty result: a query that has never run and a query that
// legitimately returns nothing are different states, and collapsing them lets an
// unasked question read as a settled one.
func (g *Graph) Ask(st *Standing, env Env) (*Answer, error) {
	q, err := ParseQuery(st.Query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", st.Name, err)
	}
	ids, err := g.evalCached(st.Query, q, env)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", st.Name, err)
	}
	a := &Answer{Name: st.Name, Query: st.Query, Rows: ids, Pin: g.pinOf(q, ids, env),
		DialectHash: DialectHash(g.Dialect())}

	if st.Pin == nil {
		a.Drift = []string{"never-asked"}
		return a, nil
	}
	// An edited question first, and on its own. Reporting `results-changed` for a
	// rewritten query reads as "the corpus moved" when the truth is "you asked
	// something else", and the two want opposite responses.
	if st.QueryHash != "" && st.QueryHash != QueryHashOf(st.Query) {
		a.Drift = append(a.Drift, "query-changed")
	}
	if st.Pin.SetHash != a.Pin.SetHash {
		a.Drift = append(a.Drift, "results-changed")
	}
	// An edited ladder, reported on its own. It usually shows up as
	// `results-changed` too — a row entering or leaving a `[disputed]` query — but
	// not always, and when it does not the set hash says nothing moved while the
	// rule that produced it did.
	if st.DialectHash != DialectHash(g.Dialect()) {
		a.Drift = append(a.Drift, "dialect-changed")
	}
	// The case the set hash structurally cannot see: every row still matches and
	// a document underneath was re-scanned. Checked against the CITED sources
	// rather than the whole corpus, so a matter re-OCRing an unrelated exhibit
	// does not flag a query that never rested on it.
	if len(g.SourceDrift) > 0 {
		for _, c := range a.Pin.Cites {
			if g.SourceDrift[idOfPinned(c)] != "" {
				a.Drift = append(a.Drift, "source-changed")
				break
			}
		}
	}
	a.Delta = g.DiffPins(st.Name, st.Pin, a.Pin)
	return a, nil
}

// Acknowledge records the current answer as the answer. The next Ask compares
// against this one.
//
// Separate from Ask, and never automatic. An answer that re-pins itself the
// moment it is read reports `fresh` forever: the delta exists to be SEEN by
// somebody, and a tool that clears its own alarm is a tool that has none. Same
// reason `kg attach` is never run from a hook.
func (st *Standing) Acknowledge(a *Answer, at, by string) {
	st.Pin = a.Pin
	st.QueryHash = QueryHashOf(st.Query)
	st.DialectHash = a.DialectHash
	st.At = at
	st.By = by
}

// idOfPinned strips the "@semhash" a pinned entry carries.
func idOfPinned(s string) string {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i]
	}
	return s
}

// pinOf builds the recordable answer. Identical to what a spec pins, because it
// is the same question — the rows, what they rest on, and what they cite.
func (g *Graph) pinOf(q Query, ids []string, env Env) *Pin {
	p := &Pin{Count: len(ids)}
	if pat, ok := q.(Pattern); ok {
		for _, k := range pat.Sort {
			p.Sort = append(p.Sort, k.Key)
		}
	}
	if len(p.Sort) == 0 {
		p.Sort = []string{"id"}
	}
	for _, id := range ids {
		p.Nodes = append(p.Nodes, id+"@"+strings.TrimPrefix(g.SemHash[id], "sha256:"))
	}
	p.Unresolved = g.TaintedSet(ids, env.Now)
	p.Cites = g.citedSources(ids)
	joined := append([]string{}, p.Nodes...)
	joined = append(joined, p.Unresolved...)
	joined = append(joined, p.Cites...)
	p.SetHash = hashString(strings.Join(joined, "\n"))
	return p
}

// ── the result cache ───────────────────────────────────────────────────

// resultCache memoises a standing query's rows for one graph state.
//
// The graph is REBUILT on change rather than mutated — a fresh Graph starts with
// an empty cache, which is the whole invalidation story for the watch path and
// is why this is a memo rather than a view to maintain. The mutating paths are
// the scratch ones (`WhatIf`, `WithFile`), and they Clone, which deliberately
// does not carry the cache across.
//
// Guarded because the daemon serves concurrent requests off one Scope's graph.
type resultCache struct {
	mu   sync.Mutex
	rows map[string][]string
}

// evalCached resolves a query, reusing the answer for this graph state.
//
// Keyed by the query TEXT and the clock, because `@now` makes the same text a
// different question on a different day, and a cache that ignored the clock
// would answer a limitations query with yesterday's window.
func (g *Graph) evalCached(text string, q Query, env Env) ([]string, error) {
	key := QueryHashOf(text) + "@" + env.Now
	if g.results != nil {
		g.results.mu.Lock()
		rows, ok := g.results.rows[key]
		g.results.mu.Unlock()
		if ok {
			return append([]string(nil), rows...), nil
		}
	}
	rows, err := g.Eval(q, env)
	if err != nil {
		return nil, err
	}
	if g.results == nil {
		g.results = &resultCache{}
	}
	g.results.mu.Lock()
	if g.results.rows == nil {
		g.results.rows = map[string][]string{}
	}
	g.results.rows[key] = append([]string(nil), rows...)
	g.results.mu.Unlock()
	return rows, nil
}

// Invalidate scraps cached results that the named nodes could have moved.
//
// CONSERVATIVE BY CONSTRUCTION, and the asymmetry is deliberate. A cached result
// that MENTIONS a changed node is obviously stale. A changed node the result does
// not mention may still belong in it now — a claim whose status flipped to
// `asserted` newly matches a query it was previously excluded from — and no
// amount of looking at the OLD rows can discover that. So a node this graph did
// not already have, or any node whose identity is unknown here, scraps
// everything.
//
// Being wrong in the cheap direction costs a re-resolution over an in-memory
// graph of 10³–10⁴ nodes. Being wrong in the other direction reports a stale
// answer as current, which is the one thing this package exists not to do.
func (g *Graph) Invalidate(changed ...string) {
	if g.results == nil {
		return
	}
	g.results.mu.Lock()
	defer g.results.mu.Unlock()
	if len(g.results.rows) == 0 {
		return
	}
	for _, id := range changed {
		if _, known := g.Nodes[id]; !known {
			g.results.rows = nil
			return
		}
	}
	touched := map[string]bool{}
	for _, id := range changed {
		touched[id] = true
	}
	for key, rows := range g.results.rows {
		for _, r := range rows {
			if touched[r] {
				delete(g.results.rows, key)
				break
			}
		}
	}
}
