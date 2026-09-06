package kgraph

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Result is one query's resolved set. Sets are pinned, not just members: prose
// commits to counts and order, so both are part of the answer.
type Result struct {
	Query string   `json:"query"`
	IDs   []string `json:"ids"`
	Count int      `json:"count"`
	Sort  []string `json:"sort"`
}

// queryFields are the fields a predicate or sort key may name. A typo here would
// otherwise match nothing silently, which in a spec means a section of a
// generated document quietly empties.
var queryFields = map[string]bool{
	"id": true, "kind": true, "body": true, "status": true, "owner": true,
	"while": true, "unit": true, "precision": true, "valid_from": true,
	"valid_until": true, "from": true, "to": true, "computed": true,
	"class": true, "at": true, "value": true, "speaker": true, "medium": true,
	"needs": true, "disputed": true, "underdetermined": true, "impeached": true,
}

// boolFields are computed, not authored: they take no value other than
// true/false, and `[disputed=maybe]` would otherwise validate and match nothing.
var boolFields = map[string]bool{"computed": true, "disputed": true, "impeached": true}

// ValidateQuery checks a parsed query against the schema. Syntax is not enough:
// `status=retracted` parses cleanly and matches nothing, so a spec written
// against an older vocabulary would silently produce an empty section forever.
func ValidateQuery(q Query) []error { return ValidateQueryIn(legal, q) }

// ValidateQueryIn checks a query against a stated vocabulary.
//
// `class=` is the only key a dialect moves, and it is the one that matters: a
// query naming a rung its own corpus uses would be rejected under a foreign
// ladder, and one naming a rung that ladder happens to share would be accepted
// where it means something else.
//
// CROSS-DIALECT QUERY NEEDS NO GUARD HERE, and it is worth saying why rather
// than leaving the absence to look like an oversight. A query runs within one
// index — in-index closure, scan.go — so there is no way to build one that spans
// two ladders. If querying ever crosses an index boundary, THIS is where the
// refusal belongs: ranks from two ladders are not comparable, and a verifier
// that compares them invents facts.
func ValidateQueryIn(dl Dialect, q Query) []error {
	var errs []error
	var walk func(Query)
	walk = func(q Query) {
		switch t := q.(type) {
		case Union:
			for _, x := range t.Terms {
				walk(x)
			}
		case Inter:
			for _, x := range t.Terms {
				walk(x)
			}
		case Not:
			walk(t.X)
		case Scoped:
			walk(t.X)
			for _, s := range t.Sort {
				if !queryFields[s.Key] {
					errs = append(errs, fmt.Errorf("unknown sort field %q", s.Key))
				}
			}
		case Pattern:
			for _, a := range t.Atoms {
				for _, k := range a.Kinds {
					if !isKind(string(k)) {
						errs = append(errs, fmt.Errorf("unknown kind %q", k))
					}
				}
				for _, p := range a.Preds {
					if !queryFields[p.Key] {
						errs = append(errs, fmt.Errorf("unknown field %q", p.Key))
						continue
					}
					if p.Key == "status" {
						for _, v := range p.Vals {
							if !validStatus[Status(v)] {
								errs = append(errs, fmt.Errorf("unknown status %q", v))
							}
						}
					}
					if p.Key == "needs" {
						for _, v := range p.Vals {
							if !validNeeds[Needs(v)] {
								errs = append(errs, fmt.Errorf("unknown needs %q", v))
							}
						}
					}
					if p.Key == "class" {
						for _, v := range p.Vals {
							if !dl.Valid(v) {
								errs = append(errs, fmt.Errorf("unknown source class %q in the %s dialect",
									v, dl.Name()))
							}
						}
					}
					// A typed field compared against a free-form word parses and
					// validates but matches nothing — the silent-empty-section bug.
					if dateFields[p.Key] {
						for _, v := range p.Vals {
							if !isDateLiteral(v) {
								errs = append(errs, fmt.Errorf("%s expects a date or `now`, got %q", p.Key, v))
							}
						}
					}
					if boolFields[p.Key] && p.Op != "has" {
						if p.Op != "=" && p.Op != "!=" {
							errs = append(errs, fmt.Errorf("%s is a computed flag; use `[%s]`, `[%s=true]` or `!...[%s]`",
								p.Key, p.Key, p.Key, p.Key))
						}
						for _, v := range p.Vals {
							if v != "true" && v != "false" {
								errs = append(errs, fmt.Errorf("%s expects true or false, got %q", p.Key, v))
							}
						}
					}
					if p.Key == "value" && p.Op != "has" {
						for _, v := range p.Vals {
							if _, err := strconv.ParseFloat(v, 64); err != nil {
								errs = append(errs, fmt.Errorf("value expects a number, got %q", v))
							}
						}
					}
				}
			}
			for _, s := range t.Sort {
				if !queryFields[s.Key] {
					errs = append(errs, fmt.Errorf("unknown sort field %q", s.Key))
				}
			}
		}
	}
	walk(q)
	return errs
}

var dateFields = map[string]bool{
	"at": true, "valid_from": true, "valid_until": true, "from": true, "to": true,
}

var dateLiteral = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?$`)

func isDateLiteral(s string) bool { return s == "now" || dateLiteral.MatchString(s) }

// Env carries evaluation context: the clock, and the named-query library.
type Env struct {
	Now   string // ISO date; "" means temporal filters pass
	Named map[string]Query
}

// Eval resolves a query against the graph. Order is the pattern's declared sort
// — set operations fall back to id, which is the documented stable default.
func (g *Graph) Eval(q Query, env Env) ([]string, error) {
	return g.eval(q, env, map[string]bool{}, nil)
}

// inherit is the temporal context pushed down from an enclosing Scoped. A
// pattern with its own `@` overrides it.
func (g *Graph) eval(q Query, env Env, seen map[string]bool, inherit *Temporal) ([]string, error) {
	switch t := q.(type) {
	case Scoped:
		at := t.At
		if at == nil {
			at = inherit
		}
		ids, err := g.eval(t.X, env, seen, at)
		if err != nil {
			return nil, err
		}
		if len(t.Sort) > 0 {
			// Sorting can read a computed flag, so it needs the same temporal
			// context the match did.
			g.sortIDs(ids, t.Sort, resolveAtFor(at, env))
		}
		return ids, nil
	case Pattern:
		if t.At == nil && inherit != nil {
			t.At = inherit
		}
		return g.evalPattern(t, env)
	case Ref:
		if seen[t.Name] {
			return nil, fmt.Errorf("named query %q is recursive", t.Name)
		}
		sub, ok := env.Named[t.Name]
		if !ok {
			return nil, fmt.Errorf("unknown named query @%s", t.Name)
		}
		seen[t.Name] = true
		defer delete(seen, t.Name)
		return g.eval(sub, env, seen, inherit)
	case Not:
		// Set complement over the result universe — total and cheap, with none
		// of the stratification a negation-as-failure semantics would need.
		in, err := g.eval(t.X, env, seen, inherit)
		if err != nil {
			return nil, err
		}
		excl := toSet(in)
		var out []string
		for id := range g.Nodes {
			if !excl[id] {
				out = append(out, id)
			}
		}
		return out, nil
	case Inter:
		var acc map[string]bool
		for _, term := range t.Terms {
			ids, err := g.eval(term, env, seen, inherit)
			if err != nil {
				return nil, err
			}
			s := toSet(ids)
			if acc == nil {
				acc = s
				continue
			}
			for id := range acc {
				if !s[id] {
					delete(acc, id)
				}
			}
		}
		return keys(acc), nil
	case Union:
		acc := map[string]bool{}
		for _, term := range t.Terms {
			ids, err := g.eval(term, env, seen, inherit)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				acc[id] = true
			}
		}
		return keys(acc), nil
	}
	return nil, fmt.Errorf("unknown query node %T", q)
}

// evalPattern binds the LEFTMOST atom and requires an existential path through
// the hops. Sorting is applied last, and defaults to id so a re-resolve never
// reorders spuriously.
func (g *Graph) evalPattern(p Pattern, env Env) ([]string, error) {
	at := ""
	if p.At != nil {
		at = p.At.From
		if p.At.Now {
			at = env.Now
		}
	}
	// Resolve every #ref through the alias index at this query's date, so
	// `#P74736 @2020-01-01` and `#P74736 @2022-01-01` reach different things.
	atoms := make([]Atom, len(p.Atoms))
	copy(atoms, p.Atoms)
	for i := range atoms {
		if atoms[i].ID == "" {
			continue
		}
		id, err := g.ResolveRef(atoms[i].ID, at)
		if err != nil {
			return nil, err
		}
		atoms[i].ID = id
	}
	p.Atoms = atoms

	// `@[from,to]` is a WINDOW, not an as-of date. The `to` half used to be
	// parsed and then dropped on the floor, so a windowed query silently
	// degraded to "as of `from`" and matched the whole graph: the fence-dispute
	// timeline's `event @[2021-01-01,2021-12-31]` returned 46 of 48 events, and
	// every section of the chronology rendered the entire chronology. A filter
	// that silently matches everything is worse than one that errors, because
	// the document it produces looks well-formed.
	//
	// A node is in the window when its resolved date is. Dates are ISO-8601, so
	// string comparison is chronological; a node with no date is not in any
	// window (it cannot be placed on a timeline).
	windowed := p.At != nil && !p.At.Now && p.At.To != "" && p.At.To != p.At.From

	var out []string
	for id, n := range g.Nodes {
		if !g.matchAtom(p.Atoms[0], n, at, env.Now) {
			continue
		}
		if windowed {
			d, ok := g.ResolveAt(id)
			if !ok || !dateOverlapsWindow(d, p.At.From, p.At.To) {
				continue
			}
		}
		if ok, err := g.walk(id, p, at, env.Now); err != nil {
			return nil, err
		} else if ok {
			out = append(out, id)
		}
	}
	g.sortIDs(out, p.Sort, at)
	return out, nil
}

func (g *Graph) walk(start string, p Pattern, at, now string) (bool, error) {
	cur := []string{start}
	for i, h := range p.Hops {
		next := map[string]bool{}
		for _, from := range cur {
			for _, to := range g.hopTargets(from, h, at) {
				n := g.Nodes[to]
				if n != nil && g.matchAtom(p.Atoms[i+1], n, at, now) {
					next[to] = true
				}
			}
		}
		if len(next) == 0 {
			return false, nil
		}
		cur = keys(next)
	}
	return true, nil
}

// hopTargets follows one hop, or its transitive closure when `*` is set. Edges
// carry their own validity, and an expired edge truncates the path: a closure
// must not walk a requirement that has lapsed.
func (g *Graph) hopTargets(from string, h Hop, at string) []string {
	step := func(id string) []string {
		var out []string
		for _, e := range g.incident[id] {
			if e.Type != h.Type || !g.edgeLive(e, at) {
				continue
			}
			switch h.Dir {
			case 1:
				if e.Src == id {
					out = append(out, e.Dst)
				}
			case -1:
				if e.Dst == id {
					out = append(out, e.Src)
				}
			default:
				if e.Src == id {
					out = append(out, e.Dst)
				} else if e.Dst == id {
					out = append(out, e.Src)
				}
			}
		}
		return out
	}
	if !h.Star {
		return step(from)
	}
	seen, queue := map[string]bool{}, step(from)
	var out []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		queue = append(queue, step(id)...)
	}
	return out
}

// edgeLive reports whether an edge is traversable at a point in time. With no
// temporal context every edge is live, so an untimed query still sees the whole
// graph.
func (g *Graph) edgeLive(e Edge, at string) bool {
	if at == "" {
		return true
	}
	if e.ValidFrom != "" && compare(at, e.ValidFrom) < 0 {
		return false
	}
	if e.ValidUntil != "" && compare(at, e.ValidUntil) > 0 {
		return false
	}
	if e.While != "" && !g.Satisfied(e.While, at) {
		return false
	}
	return true
}

func (g *Graph) matchAtom(a Atom, n *Node, at, now string) bool {
	if a.ID != "" && n.ID != a.ID {
		return false
	}
	if len(a.Kinds) > 0 {
		var ok bool
		for _, k := range a.Kinds {
			if n.Kind == k {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if a.Text != "" && !strings.Contains(strings.ToLower(n.Body), strings.ToLower(a.Text)) {
		return false
	}
	for _, p := range a.Preds {
		if !g.matchPred(p, n, at, now) {
			return false
		}
	}
	if at != "" {
		if !validAt(n, at) {
			return false
		}
		// `while` gates transitively: a Plan-B theory hanging off an unasserted
		// premise is a theory in a world we are not in, and must not surface as
		// live work.
		if n.While != "" && !g.Satisfied(n.While, at) {
			return false
		}
	}
	return true
}

func (g *Graph) matchPred(p Pred, n *Node, at, now string) bool {
	// A computed flag is modelled as present-or-absent, which the ordinary path
	// cannot compare against: absence short-circuits to false below, so
	// `[disputed=false]` and `[computed=false]` matched NOTHING rather than the
	// complement. Silently — the query parses, validates, and returns an empty
	// section.
	if boolFields[p.Key] {
		_, yes := g.field(n, p.Key, at)
		switch p.Op {
		case "has":
			return yes
		case "=":
			return (p.Vals[0] == "true") == yes
		case "!=":
			return (p.Vals[0] == "true") != yes
		}
		return false
	}
	got, present := g.field(n, p.Key, at)
	if p.Op == "has" {
		return present
	}
	if !present {
		return false
	}
	// `now` is a clock reference wherever it appears, not the literal word —
	// `event[at>now]` otherwise string-compares against "now" and matches nothing.
	val := func(i int) string {
		if p.Vals[i] == "now" {
			return now
		}
		return p.Vals[i]
	}
	switch p.Op {
	case "in":
		for i := range p.Vals {
			if got == val(i) {
				return true
			}
		}
		return false
	case "=":
		return got == val(0)
	case "!=":
		return got != val(0)
	}
	c := compare(got, val(0))
	switch p.Op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	}
	return false
}

// field resolves a queryable field, resolving computed `= expr` values and dates
// so a derived deadline compares like an authored one.
func (g *Graph) field(n *Node, key, at string) (string, bool) {
	switch key {
	case "id":
		return n.ID, true
	case "kind":
		return string(n.Kind), true
	case "body":
		return n.Body, n.Body != ""
	case "status":
		return string(n.Status), n.Status != ""
	case "owner":
		return n.Owner, n.Owner != ""
	case "while":
		return n.While, n.While != ""
	case "unit":
		return n.Unit, n.Unit != ""
	case "precision":
		return string(n.Precision), true
	case "valid_from":
		return n.ValidFrom, n.ValidFrom != ""
	case "valid_until":
		return n.ValidUntil, n.ValidUntil != ""
	case "from":
		return n.OccFrom, n.OccFrom != ""
	case "to":
		return n.OccTo, n.OccTo != ""
	case "computed":
		return "true", n.AtExpr != "" || n.ValueExpr != ""
	case "underdetermined":
		// Authored, so the reason is the value; presence is what `[underdetermined]`
		// asks about.
		return n.Underdetermined, n.Underdetermined != ""
	case "disputed":
		// Computed, never authored — the parser rejects `disputed` as a status for
		// exactly this reason, so it can only ever be asked about, not asserted.
		// Present iff the evidence points both ways; `!claim[disputed]` is how you
		// ask for the uncontested ones.
		// A claim declared not-one-fact is NOT disputed. Asking for disputed facts
		// must not return facts whose sources were never in conflict.
		return "true", g.tension(n.ID, at).Kind == "disputed"
	case "impeached":
		// A dispute the record wins. Disjoint from `disputed`, which is the ones
		// still to be weighed.
		return "true", g.tension(n.ID, at).Kind == "impeached"
	case "class":
		if n.Source == nil {
			return "", false
		}
		return n.Source.Class, n.Source.Class != ""
	case "needs":
		return string(n.Needs), n.Needs != ""
	case "speaker":
		// Who said it. Needed to ask the question an impeachment matrix is built
		// on: which declarant has sworn two things that cannot both be true.
		if n.Source == nil {
			return "", false
		}
		return n.Source.Speaker, n.Source.Speaker != ""
	case "medium":
		if n.Source == nil {
			return "", false
		}
		return n.Source.Medium, n.Source.Medium != ""
	case "at":
		if v, ok := g.ResolveAt(n.ID); ok {
			return v, true
		}
		return "", false
	case "value":
		if v, ok := g.ResolveValue(n.ID); ok {
			return strconv.FormatFloat(v, 'f', -1, 64), true
		}
		return "", false
	}
	return "", false
}

func compare(a, b string) int {
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	if ea == nil && eb == nil {
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
}

func validAt(n *Node, at string) bool {
	if n.ValidFrom != "" && compare(at, n.ValidFrom) < 0 {
		return false
	}
	if n.ValidUntil != "" && compare(at, n.ValidUntil) > 0 {
		return false
	}
	return true
}

// resolveAtFor collapses a Temporal to the date a field lookup should use.
func resolveAtFor(t *Temporal, env Env) string {
	if t == nil {
		return ""
	}
	if t.Now {
		return env.Now
	}
	return t.From
}

func (g *Graph) sortIDs(ids []string, keysSpec []SortKey, at string) {
	if len(keysSpec) == 0 {
		keysSpec = []SortKey{{Key: "id"}}
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := g.Nodes[ids[i]], g.Nodes[ids[j]]
		for _, k := range keysSpec {
			av, aok := g.field(a, k.Key, at)
			bv, bok := g.field(b, k.Key, at)
			// missing sorts last regardless of direction, so absent dates never
			// masquerade as the earliest
			if aok != bok {
				return aok
			}
			if c := compare(av, bv); c != 0 {
				if k.Desc {
					return c > 0
				}
				return c < 0
			}
		}
		return ids[i] < ids[j]
	})
}

// ── satisfaction ───────────────────────────────────────────────────────

var groupCmp = regexp.MustCompile(`^(>=|<=|==|>|<)\s*(\d+)$`)

// ValidGroupExpr reports whether a string is a group expression this evaluator
// understands.
//
// IT EXISTS BECAUSE ANYTHING ELSE EVALUATES SILENTLY. `groupState`'s default arm
// leaves `OK` false when nothing matches, so `group: <a sentence>` produces a
// group that is simply never satisfied, with no error anywhere — the same shape
// as a claim nobody wrote, and indistinguishable from one whose members really
// are unmet. A consumer migrating construction entries into this format reaches
// for `group:` to hold prose, gets no complaint, and finds out when an outcome
// silently stops being reachable.
//
// The set is exactly what `groupState` branches on, written once here so the two
// cannot drift: adding a form to the evaluator and forgetting this would refuse
// the form it just learned.
func ValidGroupExpr(expr string) bool {
	e := strings.TrimSpace(expr)
	switch e {
	case "all", "any", "none":
		return true
	}
	if strings.HasPrefix(e, "sum(") && strings.HasSuffix(e, ")") {
		return true
	}
	return groupCmp.MatchString(e)
}

// GroupStat is what documents actually render for a group: how much of it is
// done and what is holding it up.
type GroupStat struct {
	Total     int      `json:"total"`
	Satisfied int      `json:"satisfied"`
	Blocking  []string `json:"blocking,omitempty"`
	OK        bool     `json:"ok"`
}

// Satisfied reports whether a node holds at a point in time, recursing through
// groups and `while` chains.
func (g *Graph) Satisfied(id, at string) bool {
	return g.satisfied(id, at, map[string]bool{})
}

func (g *Graph) satisfied(id, at string, seen map[string]bool) bool {
	n := g.Nodes[id]
	if n == nil || seen[id] {
		return false // a cycle cannot establish its own truth
	}
	seen[id] = true
	defer delete(seen, id)

	if at != "" && !validAt(n, at) {
		return false
	}
	if n.While != "" && !g.satisfied(n.While, at, seen) {
		return false
	}
	if n.Kind == KGroup {
		return g.groupStat(id, at, seen).OK
	}
	switch n.Status {
	case SAsserted, SDone, SResolved:
		return true
	}
	return false
}

// GroupStat evaluates a group's aggregate over its members.
func (g *Graph) GroupStat(id, at string) GroupStat {
	return g.groupStat(id, at, map[string]bool{})
}

func (g *Graph) groupStat(id, at string, seen map[string]bool) GroupStat {
	n := g.Nodes[id]
	var st GroupStat
	if n == nil {
		return st
	}
	for _, m := range g.membersAt(id, at) {
		st.Total++
		if g.satisfied(m, at, seen) {
			st.Satisfied++
		} else {
			st.Blocking = append(st.Blocking, m)
		}
	}
	sort.Strings(st.Blocking)

	expr := strings.TrimSpace(n.GroupExpr)
	switch {
	case expr == "all":
		st.OK = st.Total > 0 && st.Satisfied == st.Total
	case expr == "any":
		st.OK = st.Satisfied >= 1
	case expr == "none":
		st.OK = st.Satisfied == 0
	case strings.HasPrefix(expr, "sum("):
		// a summing group is a quantity, not a proposition
		st.OK = st.Total > 0
	default:
		if m := groupCmp.FindStringSubmatch(expr); m != nil {
			k, _ := strconv.Atoi(m[2])
			switch m[1] {
			case ">=":
				st.OK = st.Satisfied >= k
			case ">":
				st.OK = st.Satisfied > k
			case "<=":
				st.OK = st.Satisfied <= k
			case "<":
				st.OK = st.Satisfied < k
			case "==":
				st.OK = st.Satisfied == k
			}
		}
	}
	return st
}

func (g *Graph) members(id string) []string { return g.membersAt(id, "") }

// membersAt honours edge validity: a membership that has lapsed changes the
// group's cardinality, which is precisely what generated prose commits to.
func (g *Graph) membersAt(id, at string) []string {
	var out []string
	for _, e := range g.incident[id] {
		if e.Type == EMemberOf && e.Dst == id && g.edgeLive(e, at) {
			out = append(out, e.Src)
		}
	}
	sort.Strings(out)
	return out
}

// ── computed values and dates ──────────────────────────────────────────

var refTerm = regexp.MustCompile(`^([A-Za-z_][\w-]*)\.(value|at)$`)

// ResolveValue resolves `value`, following a `= expr` and summing groups.
func (g *Graph) ResolveValue(id string) (float64, bool) {
	return g.resolveValue(id, map[string]bool{})
}

func (g *Graph) resolveValue(id string, seen map[string]bool) (float64, bool) {
	n := g.Nodes[id]
	if n == nil || seen[id] {
		return 0, false
	}
	seen[id] = true
	defer delete(seen, id)

	if n.HasValue {
		return n.Value, true
	}
	if n.Kind == KGroup && strings.HasPrefix(strings.TrimSpace(n.GroupExpr), "sum(") {
		var sum float64
		var any bool
		for _, m := range g.members(id) {
			if v, ok := g.resolveValue(m, seen); ok {
				sum += v
				any = true
			}
		}
		return sum, any
	}
	if n.ValueExpr == "" {
		return 0, false
	}
	terms, err := splitExpr(n.ValueExpr)
	if err != nil {
		return 0, false
	}
	var acc float64
	for i, t := range terms {
		v, ok := g.termValue(t.operand, seen)
		if !ok {
			return 0, false
		}
		if i > 0 && t.minus {
			acc -= v
		} else {
			acc += v
		}
	}
	return acc, true
}

func (g *Graph) termValue(s string, seen map[string]bool) (float64, bool) {
	if m := refTerm.FindStringSubmatch(s); m != nil && m[2] == "value" {
		return g.resolveValue(m[1], seen)
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// ResolveAt resolves an occurrence date, following `= <id>.at ± Nd`. A deadline
// derived from its basis moves when the basis is corrected; a literal would not.
func (g *Graph) ResolveAt(id string) (string, bool) {
	return g.resolveAt(id, map[string]bool{})
}

func (g *Graph) resolveAt(id string, seen map[string]bool) (string, bool) {
	n := g.Nodes[id]
	if n == nil || seen[id] {
		return "", false
	}
	seen[id] = true
	defer delete(seen, id)

	if n.At != "" {
		return n.At, true
	}
	if n.AtExpr == "" {
		return "", false
	}
	terms, err := splitExpr(n.AtExpr)
	if err != nil || len(terms) == 0 {
		return "", false
	}
	m := refTerm.FindStringSubmatch(terms[0].operand)
	if m == nil || m[2] != "at" {
		return "", false
	}
	base, ok := g.resolveAt(m[1], seen)
	if !ok {
		return "", false
	}
	t, err := time.Parse("2006-01-02", base)
	if err != nil {
		return "", false
	}
	for _, term := range terms[1:] {
		years, months, days, ok := parseOffset(term.operand)
		if !ok {
			return "", false
		}
		if term.minus {
			years, months, days = -years, -months, -days
		}
		t = t.AddDate(years, months, days)
	}
	return t.Format("2006-01-02"), true
}

// parseOffset reads one offset term — `180d`, `3y`, `4mo` — into AddDate's
// arguments.
//
// Years and months are calendar units, not multiples of a day, and for a
// limitations period that distinction is the whole point. A three-year statute
// running from 2023-08-04 bars on 2026-08-04; written as `1095d` it bars on
// 2026-08-03, because 2024 was a leap year. A bar date that is one day early is
// not a rounding error, so `y` and `mo` delegate to AddDate rather than
// multiplying out to days.
//
// `mo`, not `m`: `m` reads as minutes to anyone who has used Go durations, and
// the ambiguity would be silent.
func parseOffset(s string) (years, months, days int, ok bool) {
	for _, u := range []struct {
		suffix string
		field  *int
	}{
		{"mo", &months},
		{"y", &years},
		{"d", &days},
	} {
		num, found := strings.CutSuffix(s, u.suffix)
		if !found {
			continue
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return 0, 0, 0, false
		}
		*u.field = v
		return years, months, days, true
	}
	return 0, 0, 0, false
}

type exprTerm struct {
	operand string
	minus   bool
}

// splitExpr breaks `= a.value - b.value` into signed terms. Deliberately flat:
// computed fields exist to keep a total from drifting from its parts, not to be
// a spreadsheet.
func splitExpr(s string) ([]exprTerm, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "="))
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	out := []exprTerm{{operand: fields[0]}}
	for i := 1; i < len(fields); i += 2 {
		if i+1 >= len(fields) {
			return nil, fmt.Errorf("dangling operator in %q", s)
		}
		switch fields[i] {
		case "+":
			out = append(out, exprTerm{operand: fields[i+1]})
		case "-":
			out = append(out, exprTerm{operand: fields[i+1], minus: true})
		default:
			return nil, fmt.Errorf("unsupported operator %q", fields[i])
		}
	}
	return out, nil
}

func toSet(ids []string) map[string]bool {
	s := make(map[string]bool, len(ids))
	for _, id := range ids {
		s[id] = true
	}
	return s
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dateFloor and dateCeil expand a possibly-partial ISO date to the first and
// last instant it denotes: `2022` is 2022-01-01..2022-12-31, `2022-08` is
// 2022-08-01..2022-08-31.
//
// The ceiling uses -31 for every month rather than the real month length. These
// values are only ever compared lexically against other ISO dates, and no real
// date sorts above its own month's -31, so a fake 2022-02-31 is a correct upper
// bound and a real calendar is not worth carrying here.
func dateFloor(d string) string {
	switch len(d) {
	case 4:
		return d + "-01-01"
	case 7:
		return d + "-01"
	}
	return d
}

func dateCeil(d string) string {
	switch len(d) {
	case 4:
		return d + "-12-31"
	case 7:
		return d + "-31"
	}
	return d
}

// dateOverlapsWindow reports whether a date falls in `@[from,to]`.
//
// A date of lower precision is an INTERVAL, not a point, so this is an overlap
// test rather than a comparison. Point containment silently dropped every
// coarse date: `e-suit-filed` is dated `2022`, and "2022" < "2022-01-01"
// lexically, so the filing of the lawsuit vanished from a 2022–2023 window —
// the single most important row in that section of the timeline.
//
// The window's own bounds may be partial too, so `@[2022,2023]` means all of
// both years.
func dateOverlapsWindow(d, from, to string) bool {
	return dateCeil(d) >= dateFloor(from) && dateFloor(d) <= dateCeil(to)
}
