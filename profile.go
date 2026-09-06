package kgraph

import "sort"

// A CORPUS PROFILE IS ITS SHAPE WITHOUT ITS CONTENT.
//
// The fixtures are ~100 nodes; the live index is 2,129 with a completely
// different distribution — 58% of its sources cited by nothing, 626 facts both
// resolved by a query and resting on a single citation, 47 near-duplicate pairs.
// Three checker bugs in one day lived in that difference, and none of them could
// have been found in `examples/`.
//
// The corpus itself cannot be committed: it is live legal and medical work, and
// its every id and description names a party. Its SHAPE can — a profile is
// counts and distributions, with no string from the corpus in it — so a
// generated corpus can be built to the real thing's proportions and the checkers
// exercised at the scale they actually run at.
type Profile struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
	// Kinds counts nodes per kind, so a generated corpus is not all claims.
	Kinds map[string]int `json:"kinds"`
	// SourceClasses counts sources per evidentiary class. The ladder decides
	// conflicts, so a corpus that is all one class exercises none of that.
	SourceClasses map[string]int `json:"source_classes"`
	// CitationsPerFact is how many facts have exactly N citations, N capped at
	// 5+. THE DISTRIBUTION IS THE POINT: a corpus where most facts have exactly
	// one citation behaves differently from one where most have three, and it was
	// this number that made the attestation queue's "sole citation" band useless.
	CitationsPerFact map[int]int `json:"citations_per_fact"`
	// UnreferencedSources is how many sources nothing cites.
	UnreferencedSources int `json:"unreferenced_sources"`
	// Contradictions counts contradicts/supersedes edges — what the conflict
	// machinery reads.
	Contradictions int `json:"contradictions"`
	// OpenQuestions and Withdrawn keep the status mix honest.
	OpenQuestions int `json:"open_questions"`
	Withdrawn     int `json:"withdrawn"`
}

// ProfileOf measures a graph's shape.
//
// EVERY FIELD IS A COUNT. If a string from the corpus ever ends up in here, this
// stops being publishable and the whole point of it is gone.
func ProfileOf(g *Graph) Profile {
	p := Profile{
		Nodes: len(g.Nodes), Edges: len(g.Edges),
		Kinds: map[string]int{}, SourceClasses: map[string]int{},
		CitationsPerFact: map[int]int{},
	}
	cites := map[string]int{}
	used := map[string]bool{}
	for _, e := range g.Edges {
		switch e.Type {
		case EAttests:
			cites[e.Dst]++
			used[e.Src] = true
		case EContradicts, ESupersedes:
			p.Contradictions++
		}
	}
	for id, n := range g.Nodes {
		p.Kinds[string(n.Kind)]++
		switch {
		case n.Kind == KSource:
			if n.Source != nil {
				p.SourceClasses[n.Source.Class]++
			}
			if !used[id] && n.Status != SFalse {
				p.UnreferencedSources++
			}
		case n.Kind == KQuestion:
			if n.Status == SOpen {
				p.OpenQuestions++
			}
		}
		if n.Status == SWithdrawn {
			p.Withdrawn++
		}
		if n.Kind == KClaim {
			c := cites[id]
			if c > 5 {
				c = 5
			}
			p.CitationsPerFact[c]++
		}
	}
	return p
}

// Buckets returns the citation distribution in a stable order.
func (p Profile) Buckets() [][2]int {
	keys := make([]int, 0, len(p.CitationsPerFact))
	for k := range p.CitationsPerFact {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([][2]int, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]int{k, p.CitationsPerFact[k]})
	}
	return out
}
