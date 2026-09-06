package kgraph

import (
	"fmt"
	"sort"
	"strings"
)

// The delta is delivered as explicit statements, never as two lists for the
// model to compare. "count went 3 → 4" and "f-0155 is no longer the earliest"
// are what let a revision fix the wrong sentence instead of rewriting the page.

type NodeChange struct {
	ID     string `json:"id"`
	Class  string `json:"class"` // added|removed|changed|resolved|disputed|undisputed
	Detail string `json:"detail,omitempty"`
}

// SetDelta is one query's movement between a pin and the current resolution.
type SetDelta struct {
	Query       string       `json:"query"`
	Added       []string     `json:"added,omitempty"`
	Removed     []string     `json:"removed,omitempty"`
	Changed     []NodeChange `json:"changed,omitempty"`
	CountBefore int          `json:"count_before"`
	CountAfter  int          `json:"count_after"`
	Reordered   bool         `json:"reordered,omitempty"`
	Resolved    []string     `json:"resolved,omitempty"`  // holes that closed
	NewHoles    []string     `json:"new_holes,omitempty"` // holes that opened
	FirstMoved  [2]string    `json:"first_moved,omitempty"`
	LastMoved   [2]string    `json:"last_moved,omitempty"`
}

func (d SetDelta) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0 &&
		len(d.Resolved) == 0 && len(d.NewHoles) == 0 &&
		d.CountBefore == d.CountAfter && !d.Reordered &&
		d.FirstMoved == [2]string{} && d.LastMoved == [2]string{}
}

// SpecDelta is everything that moved under one document.
type SpecDelta struct {
	Spec  string     `json:"spec"`
	State []string   `json:"state"`
	Sets  []SetDelta `json:"sets,omitempty"`
}

func (d SpecDelta) Stale() bool {
	return !(len(d.State) == 1 && d.State[0] == "fresh")
}

// DiffPins compares a pinned resolution against a current one.
func (g *Graph) DiffPins(name string, before, after *Pin) SetDelta {
	d := SetDelta{Query: name}
	if before == nil {
		before = &Pin{}
	}
	if after == nil {
		after = &Pin{}
	}
	d.CountBefore, d.CountAfter = before.Count, after.Count

	oldHash, oldOrder := splitPin(before.Nodes)
	newHash, newOrder := splitPin(after.Nodes)

	for _, id := range newOrder {
		if _, was := oldHash[id]; !was {
			d.Added = append(d.Added, id)
		}
	}
	for _, id := range oldOrder {
		if _, still := newHash[id]; !still {
			d.Removed = append(d.Removed, id)
			continue
		}
		if oldHash[id] != newHash[id] {
			d.Changed = append(d.Changed, NodeChange{ID: id, Class: "changed", Detail: g.whatChanged(id)})
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)

	was, is := toSet(before.Unresolved), toSet(after.Unresolved)
	for _, q := range before.Unresolved {
		if !is[q] {
			d.Resolved = append(d.Resolved, q)
		}
	}
	for _, q := range after.Unresolved {
		if !was[q] {
			d.NewHoles = append(d.NewHoles, q)
		}
	}

	// ordering: same membership, different sequence. Narrative order and
	// "first"/"then" break even though nothing entered or left.
	if len(d.Added) == 0 && len(d.Removed) == 0 && !equal(oldOrder, newOrder) {
		d.Reordered = true
	}
	// Boundary movement only means something when both sides have a boundary. An
	// emptied or newly-filled set is already covered by the cardinality
	// statement, and "no longer the first; (nothing) is" is noise.
	if len(oldOrder) > 0 && len(newOrder) > 0 {
		if a, b := first(oldOrder), first(newOrder); a != b {
			d.FirstMoved = [2]string{a, b}
		}
		if a, b := last(oldOrder), last(newOrder); a != b {
			d.LastMoved = [2]string{a, b}
		}
	}
	return d
}

// whatChanged describes a node whose sem_hash moved. A pin records only the old
// hash, not the old field values, so this cannot say WHICH field changed — and
// must not pretend to. Reporting "now asserted" for a node that was already
// asserted (its hash moved because it gained an edge) is worse than saying
// nothing, because it sends the reader to the wrong sentence.
//
// So it reports only what stands on its own: a withdrawal, which is an event
// with a stated reason, and a live dispute.
func (g *Graph) whatChanged(id string) string {
	n := g.Nodes[id]
	if n == nil {
		return ""
	}
	var bits []string
	switch n.Status {
	case SWithdrawn:
		w := "withdrawn"
		if n.SupersedesReason != "" {
			w += ": " + oneLine(n.SupersedesReason)
		}
		bits = append(bits, w)
	case SFalse, SResolved, SDone:
		bits = append(bits, "now "+string(n.Status))
	}
	if t := g.tension(id, ""); t.Any() {
		bits = append(bits, t.Kind+" ("+t.Detail+")")
	}
	return strings.Join(bits, "; ")
}

// Statements renders the delta as sentences for the revision prompt.
func (d SetDelta) Statements(g *Graph) []string {
	var out []string
	say := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	name := func(id string) string {
		if g != nil {
			if n := g.Nodes[id]; n != nil && n.Body != "" {
				return fmt.Sprintf("%s (%s)", oneLine(n.Body), id)
			}
		}
		return id
	}
	if d.CountBefore != d.CountAfter {
		say("%s: count went %d → %d — every numeral and every \"both\"/\"all three\" in this section is suspect",
			d.Query, d.CountBefore, d.CountAfter)
	}
	for _, id := range d.Added {
		say("%s: %s entered the set", d.Query, name(id))
	}
	for _, id := range d.Removed {
		say("%s: %s left the set", d.Query, name(id))
	}
	for _, c := range d.Changed {
		if c.Detail != "" {
			say("%s: %s changed — %s", d.Query, name(c.ID), c.Detail)
		} else {
			say("%s: %s changed", d.Query, name(c.ID))
		}
	}
	for _, q := range d.Resolved {
		say("%s: %s is now answered — statements hedged on it can be made flatly", d.Query, name(q))
	}
	for _, q := range d.NewHoles {
		say("%s: %s is now open — this section rests on an unresolved question and must be hedged", d.Query, name(q))
	}
	if d.Reordered {
		say("%s: same members, different order — narrative sequence and any \"first\"/\"then\" claims break", d.Query)
	}
	if d.FirstMoved != [2]string{} {
		say("%s: %s is no longer the first; %s is — superlatives break",
			d.Query, orNone(d.FirstMoved[0]), orNone(d.FirstMoved[1]))
	}
	if d.LastMoved != [2]string{} {
		say("%s: %s is no longer the last; %s is", d.Query, orNone(d.LastMoved[0]), orNone(d.LastMoved[1]))
	}
	return out
}

func (d SpecDelta) Statements(g *Graph) []string {
	var out []string
	for _, s := range d.Sets {
		out = append(out, s.Statements(g)...)
	}
	return out
}

// ── what-if ────────────────────────────────────────────────────────────

// Clone produces an independent graph. `--what-if` needs a scratch copy that
// cannot corrupt the real one; in memory that is just a deep copy.
func (g *Graph) Clone() *Graph {
	out := &Graph{
		Nodes:    make(map[string]*Node, len(g.Nodes)),
		Edges:    append([]Edge(nil), g.Edges...),
		incident: make(map[string][]Edge, len(g.incident)),
		SemHash:  make(map[string]string, len(g.SemHash)),
	}
	for id, n := range g.Nodes {
		cp := *n
		if n.Source != nil {
			s := *n.Source
			cp.Source = &s
		}
		out.Nodes[id] = &cp
	}
	for id, es := range g.incident {
		out.incident[id] = append([]Edge(nil), es...)
	}
	for id, h := range g.SemHash {
		out.SemHash[id] = h
	}
	out.aliases = make(map[string][]aliasBinding, len(g.aliases))
	for name, bs := range g.aliases {
		out.aliases[name] = append([]aliasBinding(nil), bs...)
	}
	return out
}

// WhatIf applies a kfacts fragment to a scratch copy and returns the resulting
// graph, so the blast radius of a fact can be seen BEFORE it is committed.
func (g *Graph) WhatIf(fragment string) (*Graph, []Diag) {
	// A bare sequence of entries. It used to be wrapped in a ```kfacts fence to
	// reach the only parser entry point there was; `ParseEntries` is the format
	// without the file, so a proposed fact no longer has to pretend to be one.
	doc, diags := ParseEntries(legal, "--what-if", []byte(fragment))
	if len(Errors(diags)) > 0 {
		return nil, diags
	}
	scratch := g.Clone()
	for _, n := range doc.Nodes {
		cp := n
		scratch.Nodes[n.ID] = &cp
	}
	// drop edges the replaced nodes previously owned, then add the new ones
	replaced := map[string]bool{}
	for _, n := range doc.Nodes {
		replaced[n.ID] = true
	}
	var edges []Edge
	for _, e := range scratch.Edges {
		if !replaced[e.Src] {
			edges = append(edges, e)
		}
	}
	scratch.Edges = append(edges, doc.Edges...)

	scratch.incident = map[string][]Edge{}
	for _, e := range scratch.Edges {
		if scratch.Nodes[e.Src] == nil || scratch.Nodes[e.Dst] == nil {
			continue
		}
		scratch.incident[e.Src] = append(scratch.incident[e.Src], e)
		scratch.incident[e.Dst] = append(scratch.incident[e.Dst], e)
	}
	scratch.SemHash = map[string]string{}
	for id, n := range scratch.Nodes {
		scratch.SemHash[id] = SemHash(*n, scratch.incident[id])
	}
	return scratch, diags
}

// DiffGraphs compares one spec's resolution across two graph states — the shape
// `--what-if` needs, where there is no pin to compare against.
func DiffGraphs(before, after *Graph, s DeclaredSet, env Env) (*SpecDelta, error) {
	pb, err := before.Resolve(s, env)
	if err != nil {
		return nil, err
	}
	pa, err := after.Resolve(s, env)
	if err != nil {
		return nil, err
	}
	d := &SpecDelta{Spec: s.Path}
	names := make([]string, 0, len(pa))
	for name := range pa {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sd := after.DiffPins(name, pb[name], pa[name]); !sd.Empty() {
			d.Sets = append(d.Sets, sd)
		}
	}
	if len(d.Sets) > 0 {
		d.State = []string{"would-change"}
	} else {
		d.State = []string{"unaffected"}
	}
	return d, nil
}

// ── helpers ────────────────────────────────────────────────────────────

func splitPin(nodes []string) (map[string]string, []string) {
	h := make(map[string]string, len(nodes))
	order := make([]string, 0, len(nodes))
	for _, n := range nodes {
		id, hash, _ := strings.Cut(n, "@")
		h[id] = hash
		order = append(order, id)
	}
	return h, order
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func last(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[len(xs)-1]
}

func orNone(s string) string {
	if s == "" {
		return "(nothing)"
	}
	return s
}
