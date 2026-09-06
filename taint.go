package kgraph

import (
	"fmt"
	"sort"
)

// A fact can be perfectly well-attested and still not settled, because
// something it rests on is an open question. That conditionality is transitive:
// if A requires B and B requires an open question, A is conditional too.
//
// Gating and taint are different jobs. `while` REMOVES work that belongs to a
// branch we are not in. Taint KEEPS a fact in the result and marks it as
// resting on a hole — because the document still has to talk about it, just not
// as though it were settled.

// Taint returns the open questions a node transitively rests on, via `requires`
// and group membership.
func (g *Graph) Taint(id, at string) []string {
	seen := map[string]bool{}
	holes := map[string]bool{}
	g.taint(id, at, seen, holes)
	out := make([]string, 0, len(holes))
	for q := range holes {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

func (g *Graph) taint(id, at string, seen, holes map[string]bool) {
	if seen[id] {
		return
	}
	seen[id] = true
	n := g.Nodes[id]
	if n == nil {
		return
	}
	if n.Kind == KQuestion && n.Status == SOpen {
		holes[id] = true
		// an open question is itself a hole, but what IT waits on is not the
		// caller's conditionality — stop here
		return
	}
	for _, e := range g.incident[id] {
		if e.Src != id || !g.edgeLive(e, at) {
			continue
		}
		// `requires` only. Membership does NOT flow this way: being in a group
		// with an unsettled sibling does not make this fact conditional — the
		// GROUP is conditional on its members, not the other way round.
		if e.Type == ERequires {
			g.taint(e.Dst, at, seen, holes)
		}
	}
	// a group is only as settled as its members
	if n.Kind == KGroup {
		for _, m := range g.membersAt(id, at) {
			g.taint(m, at, seen, holes)
		}
	}
	if n.While != "" {
		g.taint(n.While, at, seen, holes)
	}
}

// TaintedSet reports the open questions a whole result set rests on. A section
// of prose is conditional if any fact in it is.
func (g *Graph) TaintedSet(ids []string, at string) []string {
	holes := map[string]bool{}
	for _, id := range ids {
		for _, q := range g.Taint(id, at) {
			holes[q] = true
		}
	}
	out := make([]string, 0, len(holes))
	for q := range holes {
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

// ── variants ───────────────────────────────────────────────────────────

// Variant is one way an open question could resolve, and what the document
// would then say.
type Variant struct {
	Option string   `json:"option"`
	Body   string   `json:"body,omitempty"`
	Delta  []string `json:"delta"`
}

// VariantReport is a question that would change a document, enumerated over its
// declared options.
type VariantReport struct {
	Spec     string    `json:"spec"`
	Question string    `json:"question"`
	Body     string    `json:"body,omitempty"`
	Variants []Variant `json:"variants"`
	// Diverges distinguishes two very different situations: the answer changes
	// what the document says, or the question merely blocks a section and either
	// answer yields the same text. Both are worth reporting; only the first is a
	// reason to wait before writing.
	Diverges bool `json:"diverges"`
}

// Variants enumerates how a spec's resolution would differ under each declared
// option of each open question it rests on.
//
// This is the difference between "this document is stale" and "this document
// cannot be finished until you answer Q, and here is what each answer would
// change". Only questions that declare `options` can be enumerated; an open
// question without them is a hole with unknown shape, reported as taint alone.
func (g *Graph) Variants(s DeclaredSet, env Env) ([]VariantReport, error) {
	pins, err := g.Resolve(s, env)
	if err != nil {
		return nil, err
	}
	var all []string
	for _, p := range pins {
		for _, node := range p.Nodes {
			id, _, _ := splitOne(node)
			all = append(all, id)
		}
	}
	var out []VariantReport
	for _, qid := range g.TaintedSet(all, env.Now) {
		q := g.Nodes[qid]
		if q == nil || len(q.Options) == 0 {
			continue
		}
		rep := VariantReport{Spec: s.Path, Question: qid, Body: q.Body}
		for _, opt := range q.Options {
			scratch := g.Clone()
			// resolve the question the way this option says
			if on := scratch.Nodes[opt]; on != nil {
				cp := *on
				cp.Status = SAsserted
				scratch.Nodes[opt] = &cp
			}
			if qn := scratch.Nodes[qid]; qn != nil {
				cp := *qn
				cp.Status = SResolved
				scratch.Nodes[qid] = &cp
			}
			scratch.reindex()
			d, err := DiffGraphs(g, scratch, s, env)
			if err != nil {
				return nil, err
			}
			v := Variant{Option: opt, Delta: d.Statements(scratch)}
			if on := g.Nodes[opt]; on != nil {
				v.Body = on.Body
			}
			rep.Variants = append(rep.Variants, v)
		}
		rep.Diverges = divergent(rep.Variants)
		if rep.Diverges || anyDelta(rep.Variants) {
			out = append(out, rep)
		}
	}
	return out, nil
}

// divergent drops questions whose answers all produce the same document — those
// are open, but not load-bearing here, and reporting them is noise.
func divergent(vs []Variant) bool {
	if len(vs) < 2 {
		return false
	}
	first := fmt.Sprint(vs[0].Delta)
	for _, v := range vs[1:] {
		if fmt.Sprint(v.Delta) != first {
			return true
		}
	}
	return false
}

func anyDelta(vs []Variant) bool {
	for _, v := range vs {
		if len(v.Delta) > 0 {
			return true
		}
	}
	return false
}

// reindex rebuilds derived structures after a scratch mutation.
func (g *Graph) reindex() {
	g.incident = map[string][]Edge{}
	for _, e := range g.Edges {
		if g.Nodes[e.Src] == nil || g.Nodes[e.Dst] == nil {
			continue
		}
		g.incident[e.Src] = append(g.incident[e.Src], e)
		g.incident[e.Dst] = append(g.incident[e.Dst], e)
	}
	g.SemHash = map[string]string{}
	for id, n := range g.Nodes {
		g.SemHash[id] = SemHash(*n, g.incident[id])
	}
	g.aliases = map[string][]aliasBinding{}
	for id, n := range g.Nodes {
		for _, a := range n.Aliases {
			g.aliases[a.As] = append(g.aliases[a.As], aliasBinding{a, id})
		}
	}
}

func splitOne(s string) (string, string, bool) {
	for i := range s {
		if s[i] == '@' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
