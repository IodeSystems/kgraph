package kgraph

import (
	"fmt"
	"sort"
	"strings"
)

// Graph is the assembled, validated graph across an index's assertions.
type Graph struct {
	// nearDupes are the pairs the near-duplicate checker found, kept so a triage
	// surface can group and rank them rather than a person reading warnings.
	nearDupes []DupPair
	Nodes     map[string]*Node
	Edges     []Edge
	incident  map[string][]Edge
	SemHash   map[string]string
	aliases   map[string][]aliasBinding // name -> the things it denotes
	Retired   map[string]string         // a merged-away id -> the node that absorbed it

	// SourceDrift is set after load by CheckSourceDrift, never by Build: a pure
	// Build must stay usable with no filesystem. Nil means NOT CHECKED, which is
	// not the same as nothing drifted, so nothing may read it as reassurance.
	SourceDrift map[string]SourceState

	// Attest holds human verdicts on attests edges, set after load for the same
	// reason as SourceDrift: it comes off disk. Nil means not loaded, which is not
	// the same as nothing ruled — so `Supports` on a nil set answers true, and an
	// unloaded graph never silently strips a fact of its evidence.
	Attest AttestSet

	// Relations holds raglit's rulings on which documents are copies or versions
	// of one another, set after load for the same reason as the two above: it
	// comes off disk. Nil means NOT LOADED, which is not the same as nothing
	// ruled — so the check is silent on nil rather than reporting a clean corpus.
	Relations RelationSet
	// results memoises standing queries for THIS graph state. Unexported and
	// never copied by Clone: a scratch graph must not answer from the real one's
	// cache. See resultCache.
	results *resultCache

	// dialect is the vocabulary this graph's index binds to. Unexported and
	// reached through Dialect(), for the reason the ladder itself is unexported: a
	// caller that can swap it can change what wins a conflict under a graph that
	// has already resolved one.
	//
	// The ZERO VALUE MEANS LEGAL rather than "not loaded", which is the opposite
	// of SourceDrift and Relations below and is deliberate. Those are absences —
	// nothing was asked, so nothing may be read as reassurance. A dialect is not
	// an absence: every graph is evaluated against SOME ladder, and a graph built
	// with no filesystem in hand is evaluated against the one every existing
	// corpus already uses.
	dialect Dialect

	// corpus is the unfiltered listing of files under the index, memoized for the
	// LIFETIME OF THIS GRAPH and no longer.
	//
	// The lifetime is the whole point. Every `Have` walked the tree itself, so one
	// audit of a real matter paid for seventeen full traversals of 1,872 files —
	// 228ms each, 2.4s of a 4s audit, to answer a question whose answer cannot
	// change while a graph is loaded. Hanging it here rather than in a package map
	// is what keeps it correct: the daemon builds a Graph per request precisely so
	// it sees file changes, and a cache that outlived the load would serve it
	// yesterday's corpus — the invalidation bug the no-cache rule exists to
	// prevent.
	//
	// Nil means NOT WALKED, which is not the same as an empty corpus, so a pure
	// Build stays usable with no filesystem exactly as SourceDrift requires.
	corpus    []string
	corpusErr error

	// Style is the corpus's house style for generated prose, from
	// `.kgraph/style.md`. Set after load, same reason as the two above: it comes
	// off disk and Build must stay usable with no filesystem. It lives here rather
	// than on Env because STALENESS depends on it — `State` has to see it to
	// report `style-changed`, and `State` has no Env.
	Style string
}

// Dialect is the vocabulary this graph is evaluated against, defaulting to legal
// for a graph built without an index in hand — Build stays usable with no
// filesystem, and every corpus that predates dialects declares none.
func (g *Graph) Dialect() Dialect {
	if g.dialect.name == "" {
		return legal
	}
	return g.dialect
}

// SetDialect binds a graph to a vocabulary. Called by the loader once the index
// is resolved; Build never calls it, because Build has no filesystem to resolve
// an index from.
func (g *Graph) SetDialect(d Dialect) { g.dialect = d }

type aliasBinding struct {
	Alias
	Node string
}

// Build assembles docs into a graph and runs every cross-file check. It returns
// a graph even when diagnostics contain errors, so a caller can report all
// problems at once instead of one per run.
func Build(docs []*Doc) (*Graph, []Diag) {
	return buildGraph(docs, nil)
}

func buildGraph(docs []*Doc, att AttestSet) (*Graph, []Diag) {
	g := &Graph{
		Nodes:    map[string]*Node{},
		incident: map[string][]Edge{},
		SemHash:  map[string]string{},
		Retired:  map[string]string{},
		Attest:   att,
	}
	var diags []Diag

	for _, d := range docs {
		for i := range d.Nodes {
			n := d.Nodes[i]
			if prev, dup := g.Nodes[n.ID]; dup {
				diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("duplicate id %q (also in %s:%d)", n.ID, prev.File, prev.Line)})
				continue
			}
			g.Nodes[n.ID] = &n
		}
	}
	for _, d := range docs {
		g.Edges = append(g.Edges, d.Edges...)
	}
	diags = append(diags, g.merge()...)
	edges, dupes := dedupeEdges(g.Edges)
	g.Edges = edges
	diags = append(diags, dupes...)

	diags = append(diags, g.checkRefs()...)

	for _, e := range g.Edges {
		if g.Nodes[e.Src] == nil || g.Nodes[e.Dst] == nil {
			continue // already reported; don't hash against phantoms
		}
		g.incident[e.Src] = append(g.incident[e.Src], e)
		g.incident[e.Dst] = append(g.incident[e.Dst], e)
	}
	for id, n := range g.Nodes {
		g.SemHash[id] = SemHash(*n, g.incident[id])
	}
	g.aliases = map[string][]aliasBinding{}
	for id, n := range g.Nodes {
		for _, a := range n.Aliases {
			g.aliases[a.As] = append(g.aliases[a.As], aliasBinding{a, id})
		}
	}
	for name := range g.aliases {
		sort.Slice(g.aliases[name], func(i, j int) bool {
			return g.aliases[name][i].From < g.aliases[name][j].From
		})
	}

	diags = append(diags, g.checkAliases()...)
	diags = append(diags, g.checkAnchors()...)
	diags = append(diags, g.checkQuestions()...)
	diags = append(diags, g.checkDuplicates()...)
	diags = append(diags, g.checkSameDocument()...)
	diags = append(diags, g.checkAttestation()...)
	diags = append(diags, g.checkAttestVerdicts()...)
	diags = append(diags, g.checkUnderdetermined()...)
	diags = append(diags, g.checkSupersession()...)
	diags = append(diags, g.checkInference()...)
	diags = append(diags, g.checkSources()...)
	diags = append(diags, g.checkBranches()...)
	diags = append(diags, g.checkGroups()...)

	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].File != diags[j].File {
			return diags[i].File < diags[j].File
		}
		return diags[i].Line < diags[j].Line
	})
	return g, diags
}

// dedupeEdges collapses a relation declared from both ends. `member_of` on the
// member and `members:` on the group are the SAME canonical edge, and keeping
// both would render the relation twice in a prompt and count it twice in a
// sem_hash digest. Saying it once is also the authoring rule, so the redundant
// declaration is reported.
func dedupeEdges(in []Edge) ([]Edge, []Diag) {
	type key struct {
		src, dst, from, until, while string
		typ                          EdgeType
	}
	seen := map[key]Edge{}
	var out []Edge
	var diags []Diag
	for _, e := range in {
		k := key{e.Src, e.Dst, e.ValidFrom, e.ValidUntil, e.While, e.Type}
		if first, dup := seen[k]; dup {
			diags = append(diags, Diag{File: e.File, Line: e.Line, Severity: SevWarn, Msg: fmt.Sprintf("`%s: %s -> %s` is already declared at %s:%d — state a relation once, from either end",
				e.Type, e.Src, e.Dst, first.File, first.Line)})
			continue
		}
		seen[k] = e
		out = append(out, e)
	}
	return out, diags
}

// checkRefs catches dangling references at build time. In a corpus of contested
// facts a dangling reference must never be discovered in a filed document.
func (g *Graph) checkRefs() []Diag {
	var out []Diag
	miss := func(n *Node, field, id string) {
		out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("%s of %q references unknown node %q", field, n.ID, id)})
	}
	for _, e := range g.Edges {
		if g.Nodes[e.Src] == nil {
			out = append(out, Diag{File: e.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("edge %s references unknown node %q", e.Type, e.Src)})
		}
		if g.Nodes[e.Dst] == nil {
			out = append(out, Diag{File: e.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("edge %s references unknown node %q", e.Type, e.Dst)})
		}
		if e.While != "" && g.Nodes[e.While] == nil {
			out = append(out, Diag{File: e.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("edge %s `while` references unknown node %q", e.Type, e.While)})
		}
	}
	for _, n := range g.Nodes {
		if n.While != "" && g.Nodes[n.While] == nil {
			miss(n, "while", n.While)
		}
		if n.Owner != "" && g.Nodes[n.Owner] == nil {
			miss(n, "owner", n.Owner)
		}
		if n.Source != nil {
			if n.Source.Speaker != "" && g.Nodes[n.Source.Speaker] == nil {
				miss(n, "by", n.Source.Speaker)
			}
			for _, p := range n.Source.Premises {
				if g.Nodes[p] == nil {
					miss(n, "derived_from", p)
				}
			}
		}
	}
	return out
}

// checkAliases catches the two ways a name goes wrong: colliding with a real
// node id, and denoting two things at once.
func (g *Graph) checkAliases() []Diag {
	var out []Diag
	names := make([]string, 0, len(g.aliases))
	for name := range g.aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bs := g.aliases[name]
		if n := g.Nodes[name]; n != nil {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("alias %q collides with a node id", name)})
		}
		if len(bs) < 2 {
			continue
		}
		for i := 0; i < len(bs); i++ {
			for j := i + 1; j < len(bs); j++ {
				if overlaps(bs[i].Alias, bs[j].Alias) {
					n := g.Nodes[bs[i].Node]
					out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("alias %q denotes both %q and %q over the same period — one of the windows is wrong",
						name, bs[i].Node, bs[j].Node)})
				}
			}
		}
		var who []string
		for _, b := range bs {
			who = append(who, b.Node)
		}
		n := g.Nodes[bs[0].Node]
		out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
			Check:    CheckAliasAmbiguous,
			Key:      findingKey(g, CheckAliasAmbiguous, who...),
			Subjects: who,
			Msg: fmt.Sprintf("alias %q denotes %d different things over time (%s) — a reference to it without a date is ambiguous",
				name, len(bs), strings.Join(who, ", "))})
	}
	return out
}

// overlaps is half-open [from, until): two bindings that merely touch do not
// collide, so `until: X` on one and `from: X` on the next is the clean handoff.
func overlaps(a, b Alias) bool {
	if a.Until != "" && b.From != "" && compare(a.Until, b.From) <= 0 {
		return false
	}
	if b.Until != "" && a.From != "" && compare(b.Until, a.From) <= 0 {
		return false
	}
	return true
}

// ResolveRef maps a reference to a node id: a literal id wins, then an id
// retired by `same_as`, otherwise the alias in force at `at`. An undated
// reference that could mean two things is an error rather than a silent pick.
func (g *Graph) ResolveRef(ref, at string) (string, error) {
	if _, ok := g.Nodes[ref]; ok {
		return ref, nil
	}
	// A retired id is still a literal id — it has appeared in documents already,
	// and a query naming it must find the fact rather than quietly return nothing.
	if to, ok := g.Retired[ref]; ok {
		return to, nil
	}
	var hits []aliasBinding
	for _, b := range g.aliases[ref] {
		if b.Holds(at) {
			hits = append(hits, b)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0].Node, nil
	case 0:
		return ref, nil // matches nothing; the caller reports an empty result
	}
	var who []string
	for _, h := range hits {
		who = append(who, h.Node)
	}
	sort.Strings(who)
	return "", fmt.Errorf("%q is ambiguous at %s — it denotes %s; add a date to the query",
		ref, orNow(at), strings.Join(who, " and "))
}

func orNow(at string) string {
	if at == "" {
		return "any time"
	}
	return at
}

// checkAnchors enforces the layering `about` actually has. Across the corpus the
// direction is one-way:
//
//	entity                          — a THING. Ground: it anchors to nothing.
//	event   about-> entity          — a HAPPENING, anchored to the things in it.
//	claim   about-> entity | event  — a FACT, anchored to things and happenings.
//	claim   about-> claim           — a statement ABOUT a statement (meta, allowed).
//
// An entity anchoring to something would mean a thing is "about" another thing,
// and that relationship is a CLAIM — `northwind-title-is-galeforce`, not an anchor.
// Allowing it makes the graph mushy in a way nothing downstream can detect.
func (g *Graph) checkAnchors() []Diag {
	var out []Diag
	for _, e := range g.Edges {
		if e.Type != EAbout {
			continue
		}
		src, dst := g.Nodes[e.Src], g.Nodes[e.Dst]
		if src == nil || dst == nil {
			continue
		}
		if src.Kind == KEntity {
			out = append(out, Diag{File: e.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf(
				"entity %q cannot be `about` anything — a thing is ground. A relationship between "+
					"two things is a claim (%q about %q), not an anchor", e.Src, e.Src, e.Dst)})
		}
		if src.Kind == KEvent && dst.Kind != KEntity && dst.Kind != KEvent {
			out = append(out, Diag{File: e.File, Line: e.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"event %q is `about` a %s — events anchor to things, not to facts. "+
					"If the fact is about the event, reverse it", e.Src, dst.Kind)})
		}
	}
	return out
}

// checkQuestions catches the two ways an open question becomes permanent.
//
// Without `needs`, nobody knows what closing it would look like, so it is a wish
// rather than a task. And a question with neither an `owner` nor an `about` is
// unactionable by construction: nobody is responsible and it is anchored to
// nothing — which is what "what is the consult fee?" was, asked once globally
// when the answer differs per firm. A question like that belongs in the document
// template that asks it, not in the graph.
// merge folds every `same_as:` node into the node it names, BEFORE edges are
// deduped or hashed, so the rest of the graph never sees the retired id.
//
// This is what makes the duplicate diagnostic actionable. Deleting one half of a
// duplicate breaks every reference to it, and rewriting those references by hand
// is how the second duplicate gets created. `same_as:` retires an id while
// keeping it resolvable: the survivor absorbs the incident edges — which
// correctly moves its sem_hash, since a merge IS a change to what it relates to.
func (g *Graph) merge() []Diag {
	var out []Diag
	// Resolve chains (c same_as b same_as a) to their end, refusing cycles.
	final := map[string]string{}
	for id, n := range g.Nodes {
		if n.SameAs == "" {
			continue
		}
		seen := map[string]bool{id: true}
		cur := n.SameAs
		for {
			if seen[cur] {
				out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("`same_as` of %q is a cycle — one of the ids in it has to be the survivor", id)})
				cur = ""
				break
			}
			seen[cur] = true
			next := g.Nodes[cur]
			if next == nil {
				out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("`same_as` of %q references unknown node %q", id, cur)})
				cur = ""
				break
			}
			if next.SameAs == "" {
				break
			}
			cur = next.SameAs
		}
		if cur == "" {
			continue
		}
		if survivor := g.Nodes[cur]; survivor.Kind != n.Kind {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("%q is `same_as` %q but one is a %s and the other a %s — "+
				"different kinds are not the same fact", id, cur, n.Kind, survivor.Kind)})
			continue
		}
		final[id] = cur
	}
	if len(final) == 0 {
		return out
	}
	resolve := func(id string) string {
		if to, ok := final[id]; ok {
			return to
		}
		return id
	}
	for i := range g.Edges {
		g.Edges[i].Src = resolve(g.Edges[i].Src)
		g.Edges[i].Dst = resolve(g.Edges[i].Dst)
		g.Edges[i].While = resolve(g.Edges[i].While)
	}
	for _, n := range g.Nodes {
		n.While = resolve(n.While)
		n.Owner = resolve(n.Owner)
	}
	for id, to := range final {
		// The survivor keeps the identifier aliases of what it absorbed, so an
		// address or parcel number recorded on the loser stays resolvable.
		g.Nodes[to].Aliases = append(g.Nodes[to].Aliases, g.Nodes[id].Aliases...)
		g.Retired[id] = to
		delete(g.Nodes, id)
	}
	// Two artifacts of rewriting, both dropped silently — neither is an authoring
	// mistake, so neither should be reported as one:
	//
	//   a self-edge, which is what a merged pair's own relation collapses to, and
	//   would otherwise render as a fact relating to itself;
	//
	//   an exact duplicate, which is what two copies attesting the same source
	//   collapse to. dedupeEdges warns on those, and that warning is only correct
	//   when a person declared the relation twice.
	seen := map[string]bool{}
	kept := g.Edges[:0]
	for _, e := range g.Edges {
		if e.Src == e.Dst {
			continue
		}
		k := strings.Join([]string{e.Src, e.Dst, string(e.Type), e.ValidFrom, e.ValidUntil, e.While}, "\x00")
		if seen[k] {
			continue
		}
		seen[k] = true
		kept = append(kept, e)
	}
	g.Edges = kept
	return out
}

// Lookup resolves an id through any `same_as` merge, so a reference written
// before the merge still finds the fact.
func (g *Graph) Lookup(id string) (*Node, bool) {
	if n, ok := g.Nodes[id]; ok {
		return n, true
	}
	if to, ok := g.Retired[id]; ok {
		n, ok := g.Nodes[to]
		return n, ok
	}
	return nil, false
}

// nearDuplicate is the token-overlap score above which two same-kind facts are
// reported as possibly the same fact. Tuned against the corpus: the real
// duplicate it was written to catch scores 0.88, and the highest-scoring
// deliberate pair — a sworn statement and the rebuttal that shares its whole
// vocabulary — scores 0.42.
const nearDuplicate = 0.72

// maxBucket and nearBudget bound the quadratic pass. A token shared by more than
// maxBucket nodes of one kind discriminates nothing, and nearBudget caps total
// work at roughly a quarter-second. Exceeding either is REPORTED — see
// checkDuplicates.
const (
	maxBucket  = 256
	nearBudget = 2_000_000
)

// maxNearReports caps the listed pairs. Bounding the work is not enough to bound
// the output: a few hundred mutually similar facts fit inside maxBucket and would
// still bury everything else in the scan.
const maxNearReports = 25

// indexTokens is how many of a node's rarest tokens it is indexed under. Three
// is enough that two copies of one fact always meet, and small enough that a
// node in a bucket of common words does not drag the whole vocabulary in.
const indexTokens = 3

// checkDuplicates finds one fact entered twice.
//
// This is the failure the format is least able to absorb. Two ids for one fact
// means two `sem_hash`es: answering it in one place leaves the other open, a
// document rendering each gets a different answer, and `kg diff` reports a
// delta for a fact that never moved. Nothing else in the graph is wrong, which
// is exactly why it survives review.
//
// An exact CID collision is an error — same kind, same words, twice. A near
// match is a warning, because only a person can say whether two similar
// sentences are one fact.
func (g *Graph) checkDuplicates() []Diag {
	var out []Diag

	// Exact: content-addressed, so a reworded id or a second source cannot hide it.
	byCID := map[string][]string{}
	for id, n := range g.Nodes {
		if n.Body == "" {
			continue
		}
		byCID[n.CID] = append(byCID[n.CID], id)
	}
	dup := map[string]bool{}
	for _, ids := range byCID {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		for _, id := range ids {
			dup[id] = true
		}
		out = append(out, Diag{File: g.Nodes[ids[0]].File, Line: g.Nodes[ids[0]].Line, Severity: SevError, Msg: fmt.Sprintf("%s is the same fact entered %d times (%s) — one fact, two sem_hashes: "+
			"resolving one leaves the others open. Keep one and give the rest `same_as: <id>`",
			g.Nodes[ids[0]].Kind, len(ids), strings.Join(ids, ", "))})
	}

	// Near: candidates are reached through a token index rather than by comparing
	// every pair. Each node is posted only under its RAREST tokens, which is what
	// keeps the buckets small — pruning by document frequency instead looks
	// equivalent and is not: in a domain corpus the CONTENT words are common too
	// ("parcel", "deed", "strip"), so a frequency prune quietly drops every
	// bucket and the check silently stops checking on exactly the large corpora
	// it was needed for.
	//
	// Two copies of one fact share their whole token set, so they share their
	// rarest tokens, so they always meet in a bucket.
	toks := map[string]map[string]bool{}
	var ids []string
	for id, n := range g.Nodes {
		if n.Body == "" || dup[id] {
			continue
		}
		toks[id] = bodyTokens(n.Body)
		ids = append(ids, id)
	}
	sort.Strings(ids)

	df := map[string]int{}
	for _, id := range ids {
		for w := range toks[id] {
			df[string(g.Nodes[id].Kind)+"\x00"+w]++
		}
	}
	posting := map[string][]string{}
	for _, id := range ids {
		kind := string(g.Nodes[id].Kind)
		keys := make([]string, 0, len(toks[id]))
		for w := range toks[id] {
			keys = append(keys, kind+"\x00"+w)
		}
		sort.Slice(keys, func(i, j int) bool {
			if df[keys[i]] != df[keys[j]] {
				return df[keys[i]] < df[keys[j]]
			}
			return keys[i] < keys[j]
		})
		if len(keys) > indexTokens {
			keys = keys[:indexTokens]
		}
		for _, k := range keys {
			posting[k] = append(posting[k], id)
		}
	}

	// Comparison is bounded, because near-duplicate detection is quadratic in the
	// worst case and that case is not exotic: 4000 line items differing only by a
	// number all land in the same buckets, and unbounded this pass produced eight
	// million warnings in 28s and 4GB. Three bounds, and the two that lose
	// coverage SAY SO — a check that silently stops checking is worse than one
	// that is absent, because the clean output is taken as proof.
	//
	// Buckets are processed smallest first, so the discriminating tokens are
	// compared before the budget can be spent on tokens that separate nothing.
	keys := make([]string, 0, len(posting))
	for k := range posting {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(posting[keys[i]]) != len(posting[keys[j]]) {
			return len(posting[keys[i]]) < len(posting[keys[j]])
		}
		return keys[i] < keys[j]
	})

	type match struct {
		other string
		score float64
	}
	best := map[string]match{}
	seen := map[string]bool{}
	var compared, skippedBuckets, unbudgeted int

	for _, k := range keys {
		cands := posting[k]
		if len(cands) > maxBucket {
			skippedBuckets++
			continue
		}
		if compared >= nearBudget {
			unbudgeted++
			continue
		}
		for i := 0; i < len(cands); i++ {
			for j := i + 1; j < len(cands); j++ {
				x, y := cands[i], cands[j]
				if x > y {
					x, y = y, x
				}
				pair := x + "\x00" + y
				if seen[pair] {
					continue
				}
				seen[pair] = true
				compared++
				score := jaccard(toks[x], toks[y])
				if score < nearDuplicate {
					continue
				}
				// Two facts joined by an edge are related on purpose. A claim and
				// its rebuttal share almost every word by design, and reporting
				// those would bury the accidents in noise.
				if g.related(x, y) {
					continue
				}
				// One warning per node, naming its closest match. A node that
				// resembles four hundred others is one problem to look at, not
				// four hundred, and listing them all is how a real duplicate
				// elsewhere becomes invisible.
				if m, ok := best[x]; !ok || score > m.score {
					best[x] = match{y, score}
				}
				if m, ok := best[y]; !ok || score > m.score {
					best[y] = match{x, score}
				}
			}
		}
	}

	// Report the most-similar pairs first and cap the list. Output has to be
	// bounded independently of the bucket cap: a few hundred mutually similar
	// facts fit under it and would otherwise emit a few hundred warnings, which
	// is the same unreadable wall by a different route.
	type pair struct {
		x, y  string
		score float64
	}
	var pairs []pair
	emitted := map[string]bool{}
	var owners []string
	for id := range best {
		owners = append(owners, id)
	}
	sort.Strings(owners)
	for _, id := range owners {
		x, y := id, best[id].other
		if x > y {
			x, y = y, x
		}
		if emitted[x+"\x00"+y] {
			continue
		}
		emitted[x+"\x00"+y] = true
		pairs = append(pairs, pair{x, y, best[id].score})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score > pairs[j].score
		}
		return pairs[i].x < pairs[j].x
	})
	shown := pairs
	if len(shown) > maxNearReports {
		shown = shown[:maxNearReports]
	}
	// KEPT AS DATA, not only as text. A corpus with fifty of these is worked by
	// grouping and ruling, not by reading fifty warnings — the message below has
	// said so since it was written, and `kg source dupes` is what it was asking
	// for.
	g.nearDupes = nil
	for _, pr := range pairs {
		g.nearDupes = append(g.nearDupes, DupPair{A: pr.x, B: pr.y, Score: pr.score})
	}
	for _, pr := range shown {
		out = append(out, Diag{File: g.Nodes[pr.x].File, Line: g.Nodes[pr.x].Line, Severity: SevWarn,
			Check:    CheckNearDuplicate,
			Key:      findingKey(g, CheckNearDuplicate, pr.x, pr.y),
			Subjects: []string{pr.x, pr.y},
			Msg: fmt.Sprintf("%s and %s may be the same %s entered twice — if so keep one and "+
				"`same_as:` the other; if not, relate them so it is on the record which is which",
				pr.x, pr.y, g.Nodes[pr.x].Kind)})
	}
	if len(pairs) > len(shown) {
		// DELIBERATELY KEYLESS. This is a notice about the LISTING, not a finding
		// about facts: there is nothing here a person could rule on, and its text
		// changes with every pair added or resolved. A key would let somebody
		// accept "there are more of these", which silences a count rather than a
		// claim. `kg source dupes` is where the pairs themselves are ruled on.
		out = append(out, Diag{File: "", Line: 0, Severity: SevWarn, Msg: fmt.Sprintf(
			"%d more possible duplicate pair(s) not listed — the %d most similar are above, "+
				"and a corpus with this many is better fixed by pattern than one at a time",
			len(pairs)-len(shown), len(shown))})
	}
	if skippedBuckets > 0 || unbudgeted > 0 {
		out = append(out, Diag{File: "", Line: 0, Severity: SevWarn, Msg: fmt.Sprintf(
			"near-duplicate detection did not compare everything: %d token group(s) too large "+
				"and %d left after the %d-comparison budget. Exact duplicates are still fully "+
				"checked; near ones in those groups are not",
			skippedBuckets, unbudgeted, nearBudget)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// related reports whether an edge joins the two in either direction.
// checkSameDocument reports two sources registered against one file.
//
// This is the exact half of duplicate detection, and it exists because the fuzzy
// half cannot see this shape at all. `nearDuplicate` is a token-overlap
// threshold, and a corpus that names its sources by WHAT THEY ARE produces two
// descriptions of one instrument that share about half their words, not three
// quarters: measured over 1438 nodes in fence-dispute, every duplicate found by hand
// across four sessions scored 0.50 to 0.62 — `s-sj-order`/`s-sj-order-full`, the
// two ids for the most-cited record in the matter, scored 0.56. Lowering the
// threshold to reach them is not the fix: at 0.45 the same run produces roughly
// sixty pairs, and most of them are designed parallels.
//
// The `doc:` path is not a similarity score. Two sources naming one file are
// registered against one file, whatever they call themselves, and the check has
// no threshold and no judgement in it. Run over the same corpus it found six
// collisions in one pass, five of them real duplicates nothing else had caught.
//
// It is a WARNING and not an error, because three of those six were legitimate:
// a declaration and its exhibits, or a certification packet, genuinely live in
// one PDF. The corpus already has the idiom for that — `anchor:` — so a group
// where every member carries its own anchor is silent, and a group where any
// member lacks one is asked for it by name.
func (g *Graph) checkSameDocument() []Diag {
	byDoc := map[string][]string{}
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath == "" {
			continue
		}
		byDoc[n.Source.DocPath] = append(byDoc[n.Source.DocPath], id)
	}

	var docs []string
	for d, ids := range byDoc {
		if len(ids) > 1 {
			docs = append(docs, d)
		}
	}
	sort.Strings(docs)

	var out []Diag
	for _, doc := range docs {
		ids := byDoc[doc]
		sort.Strings(ids)

		// Every member anchored, and no two to the same place: the convention is
		// being used correctly and there is nothing to report.
		var missing []string
		anchors := map[string]bool{}
		collided := false
		for _, id := range ids {
			a := g.Nodes[id].Source.Anchor
			if a == "" {
				missing = append(missing, id)
				continue
			}
			if anchors[a] {
				collided = true
			}
			anchors[a] = true
		}
		if len(missing) == 0 && !collided {
			continue
		}

		// Report at the earliest declaration, so the diagnostic sorts beside the
		// first of the pair rather than wherever the map happened to iterate.
		first := ids[0]
		for _, id := range ids {
			a, b := g.Nodes[id], g.Nodes[first]
			if a.File < b.File || (a.File == b.File && a.Line < b.Line) {
				first = id
			}
		}

		key := findingKey(g, CheckSameFileSources, ids...)
		msg := fmt.Sprintf("%d sources are registered against the same file %s (%s) — "+
			"the same document is entered more than once",
			len(ids), doc, strings.Join(ids, ", "))
		if len(missing) > 0 {
			msg += fmt.Sprintf(". If these are separate instruments inside one document, give "+
				"each an `anchor:` naming where it is; %s %s none",
				strings.Join(missing, ", "),
				map[bool]string{true: "carries", false: "carry"}[len(missing) == 1])
		} else {
			msg += ". Two of them carry the same `anchor:`, so the anchor is not telling them apart"
		}
		msg += ". If it is one instrument, keep one id and `same_as:` the rest"

		// The forms are what the two entries CLAIM the file is, and where they
		// disagree that has to be settled before either id is retired — a merge
		// would silently pick one answer. `record` and `document` are different
		// assertions about the same bytes: one says the corpus holds the
		// instrument, the other says it holds a copy of it.
		forms := map[string]bool{}
		var formList []string
		for _, id := range ids {
			f := g.Nodes[id].Source.Form
			if f != "" && !forms[f] {
				forms[f] = true
				formList = append(formList, f)
			}
		}
		if len(formList) > 1 {
			sort.Strings(formList)
			msg += fmt.Sprintf(" — but they disagree about what the file is (%s), and that is a "+
				"question to settle before either id is retired, not one a merge can answer",
				strings.Join(formList, " vs "))
		}

		out = append(out, Diag{File: g.Nodes[first].File, Line: g.Nodes[first].Line,
			Severity: SevWarn, Check: CheckSameFileSources, Key: key, Subjects: ids, Msg: msg})
	}
	return out
}

func (g *Graph) related(a, b string) bool {
	for _, e := range g.Edges {
		if (e.Src == a && e.Dst == b) || (e.Src == b && e.Dst == a) {
			return true
		}
	}
	return false
}

// checkAttestation reports a fact that rests on nothing.
//
// A conclusion is not exempt. That was my error: I read the unattested set as
// mostly legitimate because `defense-intact` and `root-cause` are reasoning
// rather than things read off a page. But a reasoned conclusion has premises, and
// a legal one has authority — which is exactly what the `inference` class and
// `derived_from` exist to record. `clinic-billing` already does it this way
// (`s-line-arithmetic` derives a figure by subtraction from four others); fence-dispute
// was simply inconsistent.
//
// An unattested fact costs three things:
//
//	nothing can audit what it rests on, so a wrong premise is invisible;
//	`DriftedSources` cannot walk through it, so a moved exhibit under a
//	conclusion never flags the document that renders the conclusion;
//	`conflict` cannot weigh it, so a contradiction against it computes as
//	one-sided and the fact reads as settled.
//
// Scoped to `asserted` and `proposed`. A `withdrawn` fact is one we already know
// we got wrong, and a `false` one is kept only so `prohibits` has a target;
// demanding provenance for either is bookkeeping about abandoned work.
// checkUnderdetermined nags while a claim is still declared not-one-fact.
//
// It is a PENDING DEFECT, not a stable state: something downstream has to either
// split it or stop relying on it. Left unreported it becomes a permanent excuse —
// every document renders the marker, nobody ever makes the split, and the corpus
// keeps a fact it has already admitted is two facts.
//
// Resolved the way every other correction is: two or more claims that supersede
// it, and this one goes `withdrawn`. That is the format's existing immutable-fix
// pattern, so the exit is already defined.
// checkSupersession catches a correction that never closed the old fact's window.
//
// `supersedes` says "this replaces that". If both are still live and neither is
// bounded, the graph asserts two incompatible things are true right now — and a
// `@now` query returns both, so a generated document states the corrected fact
// AND the thing it corrected, as equals.
//
// Two legitimate shapes, and the difference is exactly the one that matters in a
// legal corpus:
//
//	the old fact was WRONG        -> `withdrawn`, and documents citing it were wrong
//	the old fact WAS TRUE, then   -> `valid_until` on the old, `valid_from` on the
//	  was corrected as of a date     new; documents citing it in its window were RIGHT
//
// Collapsing the second into the first is what "not always was" guards against:
// a filing that relied on the record as it stood is not a mistake.
func (g *Graph) checkSupersession() []Diag {
	var out []Diag
	for _, e := range g.Edges {
		if e.Type != ESupersedes {
			continue
		}
		old, new_ := g.Nodes[e.Dst], g.Nodes[e.Src]
		if old == nil || new_ == nil {
			continue
		}
		if old.Status == SWithdrawn || old.Status == SFalse {
			continue // retired outright: it was wrong, not corrected
		}
		if old.ValidUntil != "" {
			continue // bounded: it was true, and stopped being
		}
		out = append(out, Diag{File: old.File, Line: old.Line, Severity: SevWarn, Msg: fmt.Sprintf(
			"%q is superseded by %q but is still open-ended — say which: `withdrawn` if it "+
				"was wrong, or `valid_until` if it was true until then. A `@now` query "+
				"currently returns both as equals", e.Dst, e.Src)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func (g *Graph) checkUnderdetermined() []Diag {
	var out []Diag
	for id, n := range g.Nodes {
		if n.Underdetermined == "" {
			continue
		}
		if n.Status == SWithdrawn || n.Status == SFalse {
			continue // already retired; the split happened or the claim was dropped
		}
		var replacements int
		for _, e := range g.incident[id] {
			if e.Type == ESupersedes && e.Dst == id {
				replacements++
			}
		}
		if replacements >= 2 {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"%q is superseded by %d claims but is still live — mark it `withdrawn` "+
					"so documents stop rendering the fact it has been split into", id, replacements)})
			continue
		}
		out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
			"%q is declared not one fact (%s) — split it into the facts it conflates, each "+
				"superseding it, then withdraw it. Until then no document can cite it precisely",
			id, strings.TrimSpace(n.Underdetermined))})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// checkInference enforces that an analysis is not a primary source.
//
// An `inference` is a conclusion we reached, not something we were told. That is
// why it ranks 0 — below every witness — but the rank is not enough on its own:
// a conclusion presented as provenance with no premises is an opinion wearing
// evidence's clothing, and the facts resting on it look sourced when nothing
// underneath them is.
//
// Three ways it goes wrong, all found in a real corpus:
//
//	no `derived_from`      unmoored: nothing says what it was concluded FROM
//	circular               it derives from a fact it also attests, so the fact
//	                       supports itself by way of the analysis
//	misclassed             the `doc:` is somebody's `-analysis.md` but the class
//	                       claims a rank that a reading does not earn
func (g *Graph) checkInference() []Diag {
	var out []Diag
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil {
			continue
		}
		isAnalysisDoc := strings.Contains(strings.ToLower(n.Source.DocPath), "-analysis.")
		if n.Source.Form != "inference" {
			if isAnalysisDoc && n.Source.Class != "inference" {
				out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
					"%q cites an analysis (%s) but is class `%s` — an analysis is a READING of a "+
						"document, not the document. Class it `inference` and name what it was "+
						"concluded from, or cite the underlying record instead",
					id, n.Source.DocPath, n.Source.Class)})
			}
			continue
		}
		var attests []string
		for _, e := range g.incident[id] {
			if e.Type == EAttests && e.Src == id {
				attests = append(attests, e.Dst)
			}
		}
		if len(n.Source.Premises) == 0 {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
				Check:    CheckInferenceNoPremise,
				Key:      findingKey(g, CheckInferenceNoPremise, id),
				Subjects: []string{id},
				Msg: fmt.Sprintf(
					"inference %q declares no `derived_from` — a conclusion with no premises is an "+
						"opinion wearing provenance, and the %d fact(s) resting on it only look sourced",
					id, len(attests))})
			continue
		}
		held := map[string]bool{}
		for _, a := range attests {
			held[a] = true
		}
		for _, prem := range n.Source.Premises {
			if held[prem] {
				out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf(
					"inference %q derives from %q and also attests it — the fact supports itself "+
						"through the analysis, so nothing underneath it is load-bearing",
					id, prem)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// BuildWith is Build plus human attestations. Build stays the pure entry point;
// this one exists because what counts as attestation depends on verdicts that
// live in a sidecar, and validation has to see them.
func BuildWith(docs []*Doc, att AttestSet) (*Graph, []Diag) {
	return buildGraph(docs, att)
}

func (g *Graph) checkAttestation() []Diag {
	var out []Diag
	for id, n := range g.Nodes {
		switch n.Kind {
		case KClaim, KEvent:
		default:
			continue
		}
		if n.Status != SAsserted && n.Status != SProposed {
			continue
		}
		// One place decides what counts as attestation, which is why a human ruling
		// an edge `unsupported` needs no other wiring: the fact falls back to
		// unattested and every downstream rule — this warning, conflict, disputed —
		// follows on its own.
		var attested, miscited bool
		for _, e := range g.incident[id] {
			if e.Type != EAttests || e.Dst != id || !g.isSource(e.Src) {
				continue
			}
			if !g.Attest.Supports(id, e.Src) {
				miscited = true
				continue
			}
			attested = true
			break
		}
		if attested {
			continue
		}
		if miscited {
			// Different advice: this fact HAD a citation and a person checked it.
			// Saying "name the document it was read from" would invite re-hanging it
			// on the exhibit that was just ruled silent.
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
				Check:    CheckUnsourcedFact,
				Key:      findingKey(g, CheckUnsourcedFact, id),
				Subjects: []string{id},
				Msg: fmt.Sprintf(
					"%s %q rests on no source: every citation it had was ruled `unsupported` — "+
						"a person read the document and it does not say this. The fact is kept, "+
						"because a miscitation is not a refutation, but it needs a new source "+
						"before it is used in anything generated", n.Kind, id)})
			continue
		}
		how := "name the document it was read from"
		if n.ValueExpr != "" || n.AtExpr != "" {
			how = "declare an `inference` source with `derived_from:` naming the operands"
		} else if len(g.premises(id)) > 0 {
			how = fmt.Sprintf("declare an `inference` source with `derived_from: [%s]`",
				strings.Join(g.premises(id), ", "))
		}
		out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
			Check:    CheckUnsourcedFact,
			Key:      findingKey(g, CheckUnsourcedFact, id),
			Subjects: []string{id},
			Msg: fmt.Sprintf(
				"%s %q rests on no source — %s. A conclusion is not exempt: its premises, "+
					"and any authority it relies on, are what `inference` and `derived_from` record",
				n.Kind, id, how)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// premises are the facts already pointing at this one through logical edges.
// They are the derived_from list a conclusion most likely wants — the reasoning
// is recorded as logic, just not yet as provenance.
func (g *Graph) premises(id string) []string {
	var out []string
	for _, e := range g.incident[id] {
		if e.Dst != id {
			continue
		}
		switch e.Type {
		case ESupports, ECauses, EImplies:
			out = append(out, e.Src)
		}
	}
	sort.Strings(out)
	return out
}

func (g *Graph) checkQuestions() []Diag {
	var out []Diag
	for id, n := range g.Nodes {
		if n.Kind != KQuestion || n.Status != SOpen {
			continue
		}
		if n.Needs == "" {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"open question %q declares no `needs` — say what would answer it "+
					"(evidence, decision, reply, analysis) or it sits open forever", id)})
		}
		anchored := n.Owner != ""
		if !anchored {
			for _, e := range g.incident[id] {
				if e.Type == EAbout && e.Src == id {
					anchored = true
					break
				}
			}
		}
		if !anchored {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"open question %q has neither an owner nor an `about` anchor — nobody can act "+
					"on it and it describes nothing in particular", id)})
		}
	}
	return out
}

// srcDescriptionMax is the ceiling on a source's description — the value of its
// `document:`/`utterance:`/`record:`/`observation:`/`inference:` key.
//
// The reason it needs one at all: that field is NOT metadata. Every document
// that cites the source prints it in the reference block, so it publishes to a
// reader the moment anything cites it, on a page whose own rules it was never
// checked against. `reason:` is the field that never reaches a reader; the
// description is the field that always does. So a description names the
// instrument — speaker, date, kind, subject — and anything past naming belongs
// in `reason:`.
//
// The failure that produced this: `/d/the-case` §14 obeyed its rule and stated
// none of the client's relief terms, and then the citation block printed all
// four of them, because the source's `utterance:` quoted them verbatim. The
// prose rule held and the reference list broke it.
//
// 240 is measured, not picked. Across the fence-dispute corpus's 264 sources the
// description runs a median of 78 characters; 240 flags 13 of them, about 5%,
// which is the right order of magnitude for a warning — enough to be worth
// reading, few enough that nobody learns to ignore it.
//
// `authority` is exempt on purpose. A statute or an opinion is law rather than
// evidence, and a description carrying the holding is that description doing its
// job: a reader meeting `a-adams-cullen` in a reference block wants "Unity of
// title and subsequent separation is an absolute requirement" there. Nine of the
// corpus's twelve longest descriptions are authorities and every one is correct
// as written, so without the exemption the check would be mostly false alarms.
const srcDescriptionMax = 240

func longSourceDescription(n *Node) string {
	if n.Source == nil || n.Source.Class == ClassAuthority {
		return ""
	}
	body := strings.Join(strings.Fields(n.Body), " ")
	if len([]rune(body)) <= srcDescriptionMax {
		return ""
	}
	return fmt.Sprintf(
		"source %q has a %d-character description, over %d — and a description is "+
			"CONTENT: it prints into the reference block of every document that cites "+
			"this source, including one whose own rules forbid what it says. Cut it to "+
			"what names the instrument and move the rest into `reason:`, which no "+
			"reader sees", n.ID, len([]rune(body)), srcDescriptionMax)
}

func (g *Graph) checkSources() []Diag {
	var out []Diag
	// A source earns its keep by attesting OR by undercutting: `undercut_by`
	// lowers to `contradicts`, and a source that only contradicts is still used.
	used := map[string]int{}
	for _, e := range g.Edges {
		used[e.Src]++
		if e.Type != EAttests {
			continue
		}
		if src := g.Nodes[e.Src]; src != nil && src.Kind != KSource {
			out = append(out, Diag{File: e.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("`attested_by: %s` — %q is a %s, not a source", e.Src, e.Src, src.Kind)})
		}
	}
	for id, n := range g.Nodes {
		if n.Kind != KSource {
			continue
		}
		if n.Source != nil && g.Dialect().SpeakerRequired(n.Source.Class) && n.Source.Speaker == "" {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
				Check:    CheckInterestedNoBy,
				Key:      findingKey(g, CheckInterestedNoBy, id),
				Subjects: []string{id},
				Msg: fmt.Sprintf(
					"%q is class `%s` but names no `by:` — both classes are claims about WHOSE "+
						"interest, and neither means anything without the speaker", id, n.Source.Class)})
		}
		if n.Source != nil && n.Source.Class == "" {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf("source %q has no class; weight is undefined", id)})
		}
		if d := longSourceDescription(n); d != "" {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
				Check:    CheckLongDescription,
				Key:      findingKey(g, CheckLongDescription, id),
				Subjects: []string{id},
				Msg:      d})
		}
		if used[id] == 0 && n.Status != SFalse {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn,
				Check:    CheckUnreferencedSource,
				Key:      findingKey(g, CheckUnreferencedSource, id),
				Subjects: []string{id},
				Msg:      fmt.Sprintf("source %q is referenced by nothing", id)})
		}
	}
	return out
}

// checkBranches enforces what the `options` declaration buys: exclusivity, and
// the unplanned-branch report — the most useful thing this tool can tell a
// planner, and a graph query rather than a judgment call.
func (g *Graph) checkBranches() []Diag {
	var out []Diag
	scoped := map[string]int{} // while-target -> count of work scoped to it
	for _, n := range g.Nodes {
		if n.While != "" {
			scoped[n.While]++
		}
	}
	for id, n := range g.Nodes {
		if len(n.Options) == 0 {
			continue
		}
		var asserted []string
		for _, o := range n.Options {
			on := g.Nodes[o]
			if on == nil {
				continue
			}
			if on.Status == SAsserted {
				asserted = append(asserted, o)
			}
			if scoped[o] == 0 {
				out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf("no plan for the branch where %q holds — nothing is scoped to it with `while`", o)})
			}
		}
		if len(asserted) > 1 {
			sort.Strings(asserted)
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("question %q declares exclusive options but %v are all asserted", id, asserted)})
		}
		if n.Exhaustive && n.Status == SResolved && len(asserted) == 0 {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("question %q is resolved and exhaustive but no declared option is asserted", id)})
		}
	}
	return out
}

func (g *Graph) checkGroups() []Diag {
	var out []Diag
	members := map[string]int{}
	for _, e := range g.Edges {
		if e.Type == EMemberOf {
			members[e.Dst]++
		}
	}
	for id, n := range g.Nodes {
		if n.Kind != KGroup {
			continue
		}
		if n.GroupExpr == "" {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("group %q has no aggregate expression", id)})
		}
		if members[id] == 0 {
			out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf("group %q has no members", id)})
		}
	}
	return out
}

// Incident returns the edges touching a node, for rendering and hashing.
func (g *Graph) Incident(id string) []Edge { return g.incident[id] }
