// Conflict, and how a node reads.
//
// THIS FILE WAS `render.go` AND WAS TWO FILES UNDER ONE NAME. The prose half —
// the prompt, the managed block, attach — is gone with generation. What is left
// was never about prose: `tension`, `conflict` and `impeachment` are what the
// QUERY evaluator answers `claim[disputed]` and `claim[impeached]` with, and
// `citedSources` is what a standing query's pin records.
//
// The distinction was invisible while both lived here, and it is the reason
// retiring render was a split rather than a deletion.

package kgraph

import (
	"fmt"
	"sort"
	"strings"
)

// citedSources is which sources a result set puts tokens for into the prompt,
// as "id@semhash", sorted and deduped.
//
// It must mirror what renderNode emits, and it lives next to sourceTokens for
// that reason: a source the prompt cites but the pin omits is a citation that
// can change under a finished document without anything noticing. Both arms
// below are one of the two `cite:` sites above — a source rendered as a row
// cites itself, and everything else cites what attests or undercuts it.
func (g *Graph) citedSources(ids []string) []string {
	seen := map[string]bool{}
	add := func(id string) {
		if n := g.Nodes[id]; n != nil && n.Kind == KSource {
			seen[id] = true
		}
	}
	for _, id := range ids {
		add(id)
		for _, e := range g.incident[id] {
			if e.Dst != id {
				continue
			}
			if e.Type == EAttests || e.Type == EContradicts {
				add(e.Src)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id+"@"+strings.TrimPrefix(g.SemHash[id], "sha256:"))
	}
	sort.Strings(out) // set membership, not order
	return out
}

// conflict reports evidence pointing both ways. Computed, never authored — and a
// document must render it AS disputed rather than silently picking a side.
//
// Two shapes count, and the first version only handled one:
//
//	undercut_by: s-x        a SOURCE contradicts this fact — directional, since
//	                        a source is not itself a claim that can be doubted
//	a contradicts b         two CLAIMS conflict — mutual, because the direction
//	                        of the edge is an authoring convenience and both
//	                        sides are equally in question
//
// Counting only the first made `disputed` nearly dead: across the corpus it fired
// twice, off the single `undercut_by`, while seven claim-to-claim contradictions
// produced nothing. That is the more common shape by far, and it is the important
// one — a deed saying the strip is Halloway's against a survey drawing putting it
// in Lot I is the whole case, and a document rendering either as flatly
// `asserted` is the exact failure this marker exists to prevent.
//
// The contradicting claim must itself be live and supported. A `status: false`
// strawman exists so `prohibits` has a target and must never flag anything, and
// an unattested assertion contradicting a record is not evidence both ways.
// Tension is what kind of trouble a fact is in. Three states, and the middle one
// is why this is not a boolean: the remedy differs completely.
type Tension struct {
	Kind   string // "" | "disputed" | "underdetermined"
	Detail string
}

func (t Tension) Any() bool { return t.Kind != "" }

// Label is what a rendered fact carries.
func (t Tension) Label() string {
	switch t.Kind {
	case "disputed":
		return "DISPUTED: " + t.Detail
	case "underdetermined":
		return "UNDERDETERMINED — not one fact: " + t.Detail
	case "impeached":
		return "IMPEACHED: " + t.Detail
	}
	return ""
}

// Impeachment is a contradiction with a WINNER. Two facts conflict, one rests on
// a source that outranks the other's, and the losing source has a speaker — so
// the finding is not "these disagree" but "this person said something the record
// refutes."
//
// Computed, not authored, and every ingredient was already in the graph: sources
// carry a `speaker`, `class` is ordinal, and `contradicts` says what conflicts.
// Nothing computed it, so a document could report a sworn paragraph and a county
// record as evenly disputed — which is the opposite of the point, because the
// whole value of an impeachment is that one side loses.
//
// It stayed invisible for a second reason too: `s-wren-decl` was classed `record`
// because a declaration is a court record as an ARTIFACT. But class is the
// evidentiary weight of the contents, and an interested party's assertion is
// `interested`. Ranked 6 instead of 1, every Wren paragraph tied with the record
// refuting it and no gap ever existed to find.
type Impeachment struct {
	Speaker string   // whose credibility is hit
	By      []string // the contradicting facts that outrank it
	Class   string   // the losing source's class
	Beat    string   // the best contradicting class
}

// impeachment reports whether this claim is outranked by something contradicting
// it. Strictly outranked: an equal-class conflict is a dispute to weigh on the
// merits, not an impeachment.
func (g *Graph) impeachment(id, at string) (Impeachment, bool) {
	n := g.Nodes[id]
	if n == nil || n.Kind == KSource {
		return Impeachment{}, false
	}
	var speaker, class string
	best := -1
	for _, e := range g.incident[id] {
		if e.Type != EAttests || e.Dst != id || !g.edgeLive(e, at) {
			continue
		}
		src := g.Nodes[e.Src]
		if src == nil || src.Source == nil || src.Source.Speaker == "" {
			continue
		}
		// The weakest speaker-bearing source is what gets impeached: a claim is
		// only as defensible as its worst support.
		if r, ok := legal.Rank(src.Source.Class); ok && (best < 0 || r < best) {
			best, speaker, class = r, src.Source.Speaker, src.Source.Class
		}
	}
	if speaker == "" {
		return Impeachment{}, false
	}
	out := Impeachment{Speaker: speaker, Class: class}
	beat := best
	for _, e := range g.incident[id] {
		if e.Type != EContradicts || !g.edgeLive(e, at) {
			continue
		}
		// Same asymmetry as `conflict`: the undercutter is not impeached by what
		// it undercuts.
		if e.OneWay && e.Src == id {
			continue
		}
		other := e.Src
		if other == id {
			other = e.Dst
		}
		if other == id || !g.contests(n, other, at) {
			continue
		}
		for _, oe := range g.incident[other] {
			if oe.Type != EAttests || oe.Dst != other || !g.edgeLive(oe, at) {
				continue
			}
			os := g.Nodes[oe.Src]
			if os == nil || os.Source == nil {
				continue
			}
			if r, ok := legal.Rank(os.Source.Class); ok && r > best {
				out.By = append(out.By, other)
				if r > beat {
					beat, out.Beat = r, os.Source.Class
				}
			}
		}
	}
	if len(out.By) == 0 {
		return Impeachment{}, false
	}
	sort.Strings(out.By)
	out.By = dedupe(out.By)
	return out, true
}

// tension classifies a fact. `underdetermined` wins over `disputed` and that
// ordering is load-bearing: once an author says the claim conflates two things,
// producing a for/against tally invites weighing sources that do not actually
// disagree, and the document then answers a question confidently instead of
// splitting it. A tally is worse than silence here.
func (g *Graph) tension(id, at string) Tension {
	n := g.Nodes[id]
	if n == nil || n.Kind == KSource {
		return Tension{}
	}
	// A retired fact is not in question. `withdrawn` means we already concluded we
	// were wrong and `false` means it was tested and rejected on purpose — in both
	// the argument is over, and reporting one as contested puts a settled matter
	// back in a document as though it were live.
	if n.Status == SWithdrawn || n.Status == SFalse {
		return Tension{}
	}
	if n.Underdetermined != "" {
		return Tension{"underdetermined", n.Underdetermined}
	}
	if d, yes := g.conflict(id, at); yes {
		// An impeachment is a dispute with a winner, and saying only "1 for, 1
		// against" throws that away — a document then hedges where it should
		// press.
		if imp, ok := g.impeachment(id, at); ok {
			return Tension{"impeached", fmt.Sprintf("%s (%s) is contradicted by %s — %s",
				g.short(imp.Speaker), imp.Class, strings.Join(imp.By, ", "), imp.Beat)}
		}
		return Tension{"disputed", d}
	}
	return Tension{}
}

// windowsOverlap reports whether two claims are ever true at the same time.
//
// A correction is not a conflict. When a surveyor files a corrected survey, the
// first one WAS the record until the second replaced it — the earlier fact is not
// refuted, it is superseded, and anyone who relied on it during its window relied
// correctly. Counting that as evidence pointing both ways tells a document to
// weigh a fact against its own correction, and produces "disputed" for something
// that simply changed.
//
// An unbounded claim overlaps everything: no window means "as far as we know,
// always", which is what an undated assertion actually says.
func windowsOverlap(a, b *Node) bool {
	if a.ValidUntil != "" && b.ValidFrom != "" && compare(a.ValidUntil, b.ValidFrom) <= 0 {
		return false
	}
	if b.ValidUntil != "" && a.ValidFrom != "" && compare(b.ValidUntil, a.ValidFrom) <= 0 {
		return false
	}
	return true
}

// contests reports whether `other` actually puts `n` in question: it has to be
// live, supported, and true at some of the same time.
func (g *Graph) contests(n *Node, other, at string) bool {
	o := g.Nodes[other]
	return o != nil && g.supportedClaim(other, at) && windowsOverlap(n, o)
}

func (g *Graph) conflict(id, at string) (string, bool) {
	n := g.Nodes[id]
	if n == nil || n.Kind == KSource {
		return "", false
	}
	var for_, against int
	bestFor, bestAgainst := "", ""
	rankFor, rankAgainst := -1, -1
	note := func(class string, r *int, best *string) {
		v, ranked := legal.Rank(class)
		if !ranked {
			// Authority supports without competing. Name it, but never let it win.
			if *best == "" {
				*best = class
			}
			return
		}
		if v > *r {
			*r, *best = v, class
		}
	}
	for _, e := range g.incident[id] {
		if !g.edgeLive(e, at) {
			// An attestation or a contradiction can lapse like any other relation.
			// Counting an expired one keeps a fact "supported" by evidence that no
			// longer speaks to it.
			continue
		}
		switch e.Type {
		case EAttests:
			if e.Dst == id && g.isSource(e.Src) {
				for_++
				note(g.Nodes[e.Src].Source.Class, &rankFor, &bestFor)
			}
		case EContradicts:
			if e.Dst == id && g.isSource(e.Src) {
				against++
				note(g.Nodes[e.Src].Source.Class, &rankAgainst, &bestAgainst)
				continue
			}
			// A one-way contradiction is read only from the end it defeats.
			// `undercut_by` says the other node beats this one; standing at the
			// other end and counting this one against it inverts the relation.
			if e.OneWay && e.Src == id {
				continue
			}
			other := e.Src
			if other == id {
				other = e.Dst
			}
			if other != id && g.contests(n, other, at) {
				against++
				for _, oe := range g.incident[other] {
					if oe.Type == EAttests && oe.Dst == other && g.isSource(oe.Src) && g.edgeLive(oe, at) {
						note(g.Nodes[oe.Src].Source.Class, &rankAgainst, &bestAgainst)
					}
				}
			}
		}
	}
	if for_ == 0 || against == 0 {
		return "", false
	}
	// The classes, not just the counts. "1 for, 1 against" reads as a tie and
	// invites a document to hedge, when a record against an interested party's
	// assertion is not close — the asymmetry IS the finding.
	return fmt.Sprintf("%d for (%s), %d against (%s)",
		for_, orUnclassed(bestFor), against, orUnclassed(bestAgainst)), true
}

func (g *Graph) isSource(id string) bool {
	n := g.Nodes[id]
	return n != nil && n.Kind == KSource
}

// supportedClaim reports whether a node can put another fact in question: it has
// to be live and to rest on at least one source of its own.
func (g *Graph) supportedClaim(id, at string) bool {
	n := g.Nodes[id]
	if n == nil || n.Kind == KSource {
		return false
	}
	if n.Status != SAsserted && n.Status != SProposed {
		return false
	}
	for _, e := range g.incident[id] {
		if e.Type == EAttests && e.Dst == id && g.isSource(e.Src) && g.edgeLive(e, at) {
			return true
		}
	}
	return false
}

func orUnclassed(c string) string {
	if c == "" {
		return "unclassed"
	}
	return c
}

// short is a node's body without the id, for inline prose.
func (g *Graph) short(id string) string {
	if n, ok := g.Lookup(id); ok && n.Body != "" {
		return n.Body
	}
	return id
}

func (g *Graph) label(id string) string {
	n := g.Nodes[id]
	switch {
	case n == nil:
		return id
	case n.Body != "":
		return fmt.Sprintf("%s (%s)", n.Body, id)
	case n.Kind == KGroup && n.Title != "":
		// THE NAME, when there is one. This read the RULE — `[group: >= 1]` — for
		// want of anywhere else to carry a name, which is exactly the pressure
		// that put prose in `group:` in six of nine authored groups.
		return fmt.Sprintf("%s (%s)", n.Title, id)
	case n.Kind == KGroup:
		return fmt.Sprintf("%s [group: %s]", id, n.GroupExpr)
	}
	return id
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// ── citations ──────────────────────────────────────────────────────────

// Label is one node in a line: its own words, or its id when it has none.
//
// It was `RenderOne`, which handed back the node AS A PROMPT WOULD SEE IT — the
// point being that a UI showed exactly what a document saw rather than a second,
// drifting representation. There is no prompt any more, so there is no second
// representation to drift from, and what a caller wants is the plain thing.
func (g *Graph) Label(id string) string { return g.label(id) }

// Induced returns the subgraph around a result set: the matched nodes, their
// neighbours out to depth, and every edge between the nodes returned.
//
// Bounded, and it says when it truncated — a graph view that silently drops
// nodes reads as "this is all there is", which is the same lie as an empty
// section in a generated document.
func (g *Graph) Induced(ids []string, depth, limit int, now string) ([]GraphNode, []GraphEdge, bool) {
	if depth < 0 {
		depth = 0
	}
	if limit <= 0 {
		limit = 250
	}
	matched := toSet(ids)
	keep := map[string]bool{}
	frontier := []string{}
	for _, id := range ids {
		if g.Nodes[id] != nil {
			keep[id] = true
			frontier = append(frontier, id)
		}
	}
	for d := 0; d < depth && len(keep) < limit; d++ {
		var next []string
		for _, id := range frontier {
			for _, e := range g.incident[id] {
				for _, side := range [2]string{e.Src, e.Dst} {
					if side == id || keep[side] || g.Nodes[side] == nil {
						continue
					}
					if len(keep) >= limit {
						continue
					}
					keep[side] = true
					next = append(next, side)
				}
			}
		}
		frontier = next
	}
	truncated := len(keep) >= limit

	nodes := make([]GraphNode, 0, len(keep))
	for _, id := range keys(keep) {
		n := g.Nodes[id]
		gn := GraphNode{ID: id, Kind: string(n.Kind), Status: string(n.Status),
			Label: firstNonEmpty(n.Body, id), Matched: matched[id]}
		if n.Source != nil {
			gn.Class = n.Source.Class
		}
		nodes = append(nodes, gn)
	}
	var edges []GraphEdge
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if !keep[e.Src] || !keep[e.Dst] {
			continue
		}
		k := e.Src + "\x00" + e.Dst + "\x00" + string(e.Type)
		if seen[k] {
			continue
		}
		seen[k] = true
		edges = append(edges, GraphEdge{Src: e.Src, Dst: e.Dst, Type: string(e.Type)})
	}
	return nodes, edges, truncated
}
