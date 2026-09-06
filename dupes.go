package kgraph

import "sort"

// BATCH DUPLICATE TRIAGE. Fifty possible duplicates is not fifty problems.
//
// The near-duplicate checker has said so in its own overflow message since it
// was written — "a corpus with this many is better fixed by pattern than one at
// a time" — and there was no surface for doing that. On the live corpus it finds
// ~47 pairs, which is also the measurement that parked variants option C: that
// option was sized for grouping a handful of known variants, and what the corpus
// actually has is a backlog.

// DupPair is two nodes that may be one thing, and how alike they are.
type DupPair struct {
	A, B  string
	Score float64
}

// DupCluster is a set of nodes that may all be one thing.
//
// TRANSITIVE, because a person rules on a THING rather than on a pair: three
// entries for one survey produce three pairs, and asking three times whether the
// same document is the same document is how a triage pass stops being done.
type DupCluster struct {
	IDs []string
	// SameFile is true when every member points at the same document.
	//
	// NOT AUTOMATICALLY A DUPLICATE, and calling it one was wrong. A court filing
	// is a declaration WITH ITS EXHIBITS; an e-sign envelope holds several forms;
	// a scan holds a contract and the certification stapled to it. Measured on the
	// live corpus: of seven same-file clusters, six were compound documents and
	// exactly one was a duplicate. Retiring on that advice would have merged a
	// 1947 record into a 2023 declaration.
	SameFile bool
	// Compound is true when the members disagree about WHEN or WHAT they are —
	// different dates, or different evidentiary classes. Two entries for one
	// instrument agree about both; a declaration and the 1947 deed attached to it
	// cannot. This is the signal that separates the two readings the checker has
	// always offered, and which `kg source dupes` had flattened away.
	Compound bool
	// Why names the disagreement, so the reader can judge it rather than trust it.
	Why string
	// Doc is the shared document path when SameFile, else empty.
	Doc string
	// Cites is how many facts rest on the cluster's members in total. A cluster
	// nothing cites is cheap to resolve and cheap to leave; one holding up forty
	// facts is neither.
	Cites int
	// Keep is the member this suggests keeping: the most-cited, tie-broken by id
	// so the suggestion is stable between runs. A SUGGESTION — only a person can
	// say two records are one instrument.
	Keep string
}

// DuplicateClusters groups everything that may be one thing into things to rule
// on.
//
// TWO SOURCES OF PAIRS, and folding them together is not tidying. The
// near-duplicate checker compares TEXT; a separate check finds sources
// registered against the SAME FILE, which is the unambiguous case — one
// instrument entered more than once, no judgement required. They were reported
// by different checks in different places, so the cheapest decisions in the
// corpus were scattered through the hardest ones. Ranked together, the certain
// ones come first.
func (g *Graph) DuplicateClusters() []DupCluster {
	pairs := append([]DupPair{}, g.nearDupes...)
	// Sources sharing a document, paired up. A shared file is a score of 1: they
	// are not merely similar, they are the same bytes.
	byDoc := map[string][]string{}
	for id, n := range g.Nodes {
		if n.Kind == KSource && n.Source != nil && n.Source.DocPath != "" {
			byDoc[n.Source.DocPath] = append(byDoc[n.Source.DocPath], id)
		}
	}
	for _, ids := range byDoc {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		for i := 1; i < len(ids); i++ {
			pairs = append(pairs, DupPair{A: ids[0], B: ids[i], Score: 1})
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	// Union-find over the pairs.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if parent[x] == "" || parent[x] == x {
			parent[x] = x
			return x
		}
		r := find(parent[x])
		parent[x] = r
		return r
	}
	for _, p := range pairs {
		a, b := find(p.A), find(p.B)
		if a != b {
			parent[a] = b
		}
	}
	groups := map[string][]string{}
	for _, p := range pairs {
		for _, id := range []string{p.A, p.B} {
			r := find(id)
			if !containsString(groups[r], id) {
				groups[r] = append(groups[r], id)
			}
		}
	}

	cites := map[string]int{}
	for _, e := range g.Edges {
		if e.Type == EAttests {
			cites[e.Src]++
		}
	}
	var out []DupCluster
	for _, ids := range groups {
		sort.Strings(ids)
		c := DupCluster{IDs: ids}
		doc, same := "", true
		dates, classes := map[string]bool{}, map[string]bool{}
		for _, id := range ids {
			c.Cites += cites[id]
			n := g.Nodes[id]
			d := ""
			if n != nil && n.Source != nil {
				d = n.Source.DocPath
				// EMPTY IS NOT A DISAGREEMENT. One entry dated and one undated is a
				// missing value, not two instruments — treating it as one marked the
				// corpus's only genuine duplicate as a compound document.
				if n.Source.Class != "" {
					classes[n.Source.Class] = true
				}
				if n.At != "" {
					dates[n.At] = true
				}
			}
			switch {
			case doc == "" && d != "":
				doc = d
			case d != doc:
				same = false
			}
			if c.Keep == "" || cites[id] > cites[c.Keep] {
				c.Keep = id
			}
		}
		if same && doc != "" {
			c.SameFile, c.Doc = true, doc
			// A COMPOUND DOCUMENT, not a duplicate. Two entries for one instrument
			// agree about its date and its evidentiary class; a filing and the
			// exhibit attached to it cannot.
			switch {
			case len(dates) > 1 && len(classes) > 1:
				c.Compound, c.Why = true, "they disagree about both date and class"
			case len(dates) > 1:
				c.Compound, c.Why = true, "they carry different dates"
			case len(classes) > 1:
				c.Compound, c.Why = true, "they are different evidentiary classes"
			}
		}
		out = append(out, c)
	}
	// Ranked by what makes a decision cheap and consequential: the unambiguous
	// same-file clusters first, then by how much rests on them.
	sort.Slice(out, func(i, j int) bool {
		if out[i].SameFile != out[j].SameFile {
			return out[i].SameFile
		}
		if out[i].Cites != out[j].Cites {
			return out[i].Cites > out[j].Cites
		}
		return out[i].IDs[0] < out[j].IDs[0]
	})
	return out
}
