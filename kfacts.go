package kgraph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity of a scan diagnostic.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
)

// Diag is one scan finding. A scan reports every problem rather than stopping at
// the first: a dangling reference in a legal corpus should be caught at build,
// not discovered in a filed document.
type Diag struct {
	File     string
	Line     int
	Severity Severity
	Msg      string
	// Check names the rule that produced this, and Key identifies the finding by
	// CONTENT so a person's acceptance of it survives an unrelated edit and does
	// NOT survive an edit to the facts it names. See `accepted.go`.
	//
	// Empty on purpose for anything that is not acceptable: parse failures,
	// scan-level errors, and every check that has not been given an identity yet.
	// A diagnostic with no Key is always shown, which is the safe default —
	// forgetting to set one costs noise, never silence.
	Check string
	Key   string
	// Subjects are the NODES this is about, and they are here because the
	// information existed and was being thrown away: `findingKey` already takes
	// the ids and hashes them into an opaque key, so every keyed finding knew
	// which nodes it named and told nobody.
	//
	// It cost a consumer. caselit's `already-held` findings came back with an
	// empty subject — ten of them on a real matter — so the question's id existed
	// only inside the prose message and an agent had to regex it out. Parsing
	// another package's message format is the coupling that project refuses, so
	// the fix belonged here.
	//
	// `TestAKeyedFindingNamesItsSubjects` pins the pairing: anything with a Key
	// computed from ids has to carry them.
	Subjects []string
}

func (d Diag) String() string {
	// A finding about the scan itself has no position to point at.
	if d.File == "" {
		return fmt.Sprintf("%s: %s", d.Severity, d.Msg)
	}
	return fmt.Sprintf("%s:%d: %s: %s", d.File, d.Line, d.Severity, d.Msg)
}

type block struct {
	body      string
	firstLine int // 1-based line of the block's first content line
}

// fencedBlocks extracts ```<lang> blocks, ignoring anything below the managed
// marker — that region is generated and must never be parsed back in.
func fencedBlocks(src []byte, lang string) []block {
	var out []block
	fence := "```" + lang
	lines := strings.Split(string(src), "\n")
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), managedMarker) {
			break
		}
		if strings.TrimSpace(lines[i]) != fence {
			continue
		}
		start := i + 1
		j := start
		for j < len(lines) && strings.TrimSpace(lines[j]) != "```" {
			j++
		}
		out = append(out, block{body: strings.Join(lines[start:j], "\n"), firstLine: start + 1})
		i = j
	}
	return out
}

const managedMarker = "<!-- kgraph:managed"

// ParseEntries parses a bare YAML sequence of entries — the FORMAT, with no file
// around it.
//
// This is the public form of the line described below, and it is what a caller
// that is not a markdown file wants: the fold uses it, and so does anything
// holding entries from somewhere else. `ParseFactsIn` is now just this with a
// markdown reader in front.
func ParseEntries(dl Dialect, path string, src []byte) (*Doc, []Diag) {
	diags := checkHashCommentIn(path, 1, string(src))
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return &Doc{Path: path}, append(diags, Diag{File: path, Line: 1, Severity: SevError, Msg: "invalid YAML: " + err.Error()})
	}
	doc := &Doc{Path: path}
	if len(root.Content) == 0 {
		return doc, diags
	}
	return doc, append(diags, parseEntries(dl, path, 1, root.Content[0], doc, map[string]int{})...)
}

// THE LINE BETWEEN THE FORMAT AND ITS FILE.
//
// `parseEntries` is the format: a YAML sequence of entries, each a node or a
// standalone relation, with every idiom check and every dialect rule. Above it
// sits whatever produced that sequence — a markdown block once, an assertion log
// now — and that half is a FILE FORMAT rather than the format.
//
// Splitting them is what lets the markdown reader be deleted without touching a
// single rule. It is also why `FoldAssertions` cannot drift from the parser: the
// fold calls THIS, not a re-implementation of it, and everything a fact file
// could mean is exactly what an assertion can.
func parseEntries(dl Dialect, path string, base int, root *yaml.Node, doc *Doc, seen map[string]int) []Diag {
	var diags []Diag
	if root.Kind != yaml.SequenceNode {
		return append(diags, Diag{File: path, Line: base, Severity: SevError, Msg: "facts must be a YAML list of entries"})
	}
	for _, item := range root.Content {
		line := base + item.Line - 1
		// A STANDALONE EDGE, dispatched before parseNode ever sees it.
		//
		// The tell is structural rather than a marker key: a node carries EXACTLY
		// ONE body key (`claim:`, `question:`, a source form …) and carrying none
		// is already an error, so an entry with no body key and both endpoints can
		// only be a relation. No previously-valid input changes meaning.
		//
		// That test matters more than it looks. `from:` and `to:` are also node
		// keys — an occurrence span — so once `is:` lands on nodes too, a claim
		// with a span would be indistinguishable from an edge by keys alone. The
		// body key is what keeps them apart, permanently.
		if isEdgeEntry(item) {
			e, ds := parseEdge(dl, path, line, item)
			diags = append(diags, ds...)
			if e != nil {
				doc.Edges = append(doc.Edges, *e)
			}
			continue
		}
		n, edges, ds := parseNode(dl, path, line, base, item)
		diags = append(diags, ds...)
		if n == nil {
			continue
		}
		if prev, dup := seen[n.ID]; dup {
			diags = append(diags, Diag{File: path, Line: n.Line, Severity: SevError, Msg: fmt.Sprintf("duplicate id %q (first declared at line %d)", n.ID, prev)})
			continue
		}
		seen[n.ID] = n.Line
		doc.Nodes = append(doc.Nodes, *n)
		doc.Edges = append(doc.Edges, edges...)
	}
	return diags
}

// scalarFields are the non-edge, non-body keys a node may carry.
var scalarFields = map[string]bool{
	"id": true, "status": true, "owner": true, "reason": true,
	"supersedes_reason": true, "while": true, "at": true,
	"valid_from": true, "valid_until": true, "from": true, "to": true,
	"precision": true, "value": true, "unit": true, "ordered": true,
	"exhaustive": true, "aliases": true, "needs": true, "same_as": true,
	"underdetermined": true,
	// group-only — see Node.Title
	"title": true,
	// source-only
	"by": true, "medium": true, "recorded": true, "doc": true,
	"anchor": true, "class": true, "derived_from": true,
}

func parseNode(dl Dialect, path string, line, base int, m *yaml.Node) (*Node, []Edge, []Diag) {
	var diags []Diag
	if m.Kind != yaml.MappingNode {
		return nil, nil, []Diag{{File: path, Line: line, Severity: SevError, Msg: "each list item must be a mapping"}}
	}
	n := &Node{File: path, Line: line, Precision: PDay}
	var edges []Edge
	var bodyKey string

	lineOf := func(y *yaml.Node) int { return base + y.Line - 1 }
	errf := func(y *yaml.Node, f string, a ...any) {
		diags = append(diags, Diag{File: path, Line: lineOf(y), Severity: SevError, Msg: fmt.Sprintf(f, a...)})
	}

	// A scalar field written twice is last-write-wins, and silently so. That is
	// how five nodes in the fence-dispute corpus lost authored text: somebody appended a
	// second `reason:` months after the first, saying what the answer turned out
	// to be, and the original account of why the work mattered was discarded with
	// no diagnostic anywhere. `kg scan` reported 0 errors on every one of them.
	//
	// EDGE keys are deliberately not covered. `attested_by:` twice means two
	// sources and `requires:` twice means two prerequisites — those accumulate,
	// they are used that way, and they are the reason this cannot simply be "no
	// key twice". The accumulating set is exactly forwardEdge and inverseEdge; the
	// scalar set is exactly scalarFields.
	scalarLine := map[string]int{}

	// `is:` and any subtype-governed key are resolved AFTER the loop, because
	// both depend on facts the loop is still gathering: a subtype declares which
	// core kind it must be, and the body key that sets the kind may appear below
	// the `is:` that needs it. Deferring is what makes key order irrelevant, which
	// it must be — nothing else in this format depends on it.
	var isNode *yaml.Node
	type pending struct{ k, v *yaml.Node }
	var deferred []pending

	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		key := k.Value

		if kind, ok := bodyKeys[key]; ok {
			if bodyKey != "" {
				errf(k, "node declares two kinds (%q and %q); exactly one is allowed", bodyKey, key)
				continue
			}
			bodyKey, n.Kind, n.Body = key, kind, v.Value
			if kind == KSource {
				// Preserve anything already parsed. `by`, `class`, `medium` and `doc`
				// construct the Source themselves when they come first, and
				// overwriting it here dropped them silently — a fact whose author
				// wrote `by:` above `record:` simply had no speaker, with no
				// diagnostic to say so.
				if n.Source == nil {
					n.Source = &Source{}
				}
				n.Source.Form = key
			} else if kind == KGroup {
				// REFUSED, NOT EVALUATED. An expression this evaluator does not
				// understand leaves the group permanently unsatisfied and says
				// nothing — indistinguishable from one whose members really are
				// unmet. `group: <a sentence>` is the way that happens in
				// practice: a consumer migrating entries into this format reaches
				// for `group:` to hold prose and gets no complaint.
				// REFUSED, and it shipped as a warning for one day because six
				// entries across two corpora were written this way and there was
				// nowhere else to put a name. `title:` is that place now, so the
				// severity is the one the defect deserves.
				//
				// TWO THINGS ARE WRONG WITH PROSE HERE and the second is worse.
				// It evaluates as NEVER SATISFIED, silently — OK is false
				// whatever the members do, indistinguishable from members that
				// are genuinely unmet. And `group: <a name>` reads as membership
				// by TAG, which a kgraph group is not: a group is an explicit set
				// with `member_of` edges, never a label match.
				if !ValidGroupExpr(v.Value) {
					diags = append(diags, Diag{File: path, Line: lineOf(v), Severity: SevError,
						Msg: fmt.Sprintf("group %q takes a RULE over its members: all, any, none, "+
							"a comparison like `>= 2`, or sum(<field>). %q is none of those — it "+
							"would evaluate as never satisfied whatever the members do. If it is "+
							"the group's NAME, that is `title:`",
							n.ID, v.Value)})
				}
				n.GroupExpr = v.Value
				n.Body = ""
			}
			continue
		}

		// `derived_from` is in forwardEdge so queries can hop it, but it is also a
		// source FIELD that SemHash reads. Let assignScalar keep the field; the
		// edges are emitted from Premises below. Without this the generic branch
		// swallows the key and every inference reports "declares no derived_from".
		if et, ok := forwardEdge[key]; ok && key != "derived_from" {
			for _, r := range edgeRefs(v) {
				edges = append(edges, r.edge(n.ID, r.id, et, path, lineOf(k)))
			}
			continue
		}
		// A DIALECT'S OWN RELATIONS, after the core table and never before it. They
		// parse into ordinary edges carrying the ordinary bag, and nothing in this
		// package reads them — that inertness is the licence for the set being
		// open. See Dialect.edges.
		if et, ok := dl.Edge(key); ok {
			for _, r := range edgeRefs(v) {
				edges = append(edges, r.edge(n.ID, r.id, et, path, lineOf(k)))
			}
			continue
		}
		if et, ok := inverseEdge[key]; ok {
			for _, r := range edgeRefs(v) {
				// stored canonically: the OTHER node is the edge source
				e := r.edge(r.id, n.ID, et, path, lineOf(k))
				e.OneWay = oneWayEdge[key]
				edges = append(edges, e)
			}
			if key == "options" {
				n.Options = idList(v)
			}
			continue
		}
		if key == "is" {
			isNode = v
			continue
		}
		if !scalarFields[key] {
			deferred = append(deferred, pending{k, v})
			continue
		}
		if first, dup := scalarLine[key]; dup {
			// Reported and then still assigned, so the parse result is unchanged
			// and only the diagnostic is new. Refusing the second value would
			// change what every existing corpus renders on the day this shipped.
			errf(k, "%q is written twice, at line %d and here — a scalar field holds one "+
				"value, so the second silently replaced the first. Keep whichever is right; "+
				"if the two disagree, that is a contradiction to resolve and not two values "+
				"to join", key, first)
		} else {
			scalarLine[key] = lineOf(k)
		}
		if d, ok := assignScalar(dl, n, key, v, path, lineOf(v)); !ok {
			diags = append(diags, d)
		}
	}

	if n.ID == "" {
		return nil, nil, append(diags, Diag{File: path, Line: line, Severity: SevError, Msg: "node has no id"})
	}
	if bodyKey == "" {
		return nil, nil, append(diags, Diag{File: path, Line: line, Severity: SevError, Msg: fmt.Sprintf("node %q declares no kind (expected one of claim/question/group/entity/event/action or a source form)", n.ID)})
	}

	// GROUPS ONLY, and now that the kind is known it can be said. Refused rather
	// than ignored, which is this format's standing rule for a key that does not
	// apply: an ignored one reads as accepted by whoever typed it.
	//
	// Every other kind's body IS its text — a claim is what it claims — so a
	// title on one would duplicate its own body. A group is a set plus a rule and
	// is the one kind with nowhere else to say what it is.
	if n.Title != "" && n.Kind != KGroup {
		diags = append(diags, Diag{File: path, Line: scalarLine["title"], Severity: SevError,
			Msg: fmt.Sprintf("`title:` names a GROUP, and %q is a %s — its body is already its "+
				"own text", n.ID, n.Kind)})
		n.Title = ""
	}

	// Now the kind is known, so `is:` can be checked against it and the deferred
	// keys can be routed.
	var subs nodeSubtypes
	if isNode != nil {
		var ds []Diag
		subs, ds = parseIs(dl, isNode, n.Kind, path, lineOf(isNode))
		diags = append(diags, ds...)
		n.Is = subs.names
	}
	for _, p := range deferred {
		key := p.k.Value
		if subs.governs(key) {
			if p.v.Kind != yaml.ScalarNode {
				errf(p.k, "subtype field %q must be a scalar; the bag holds values, not structure", key)
				continue
			}
			if n.Bag == nil {
				n.Bag = map[string]string{}
			}
			n.Bag[key] = p.v.Value
			continue
		}
		// A key TWO carried subtypes would answer to is the collision prefixing
		// exists for, and the message has to say so — "unknown field `where`" when
		// two of your subtypes declare `where` sends somebody looking for a typo.
		if c := subs.claimants(key); len(c) > 1 {
			errf(p.k, "%q is governed by more than one subtype (%s); prefix one of them in `is:` "+
				"and write the key prefixed", key, strings.Join(c, ", "))
			continue
		}
		errf(p.k, "unknown field %q", key)
	}
	// A conclusion's premises are a real relation, so emit one edge per premise —
	// `derived_from` takes a list because an inference can rest on several facts.
	// Premises stay on the Source struct too: SemHash reads them, and moving the
	// field would rewrite every inference's hash for no gain.
	if n.Source != nil {
		for _, p := range n.Source.Premises {
			edges = append(edges, Edge{Src: n.ID, Type: EDerivedFrom, Dst: p})
		}
	}

	// backfill: edges authored before `id` was read
	for i := range edges {
		if edges[i].Src == "" {
			edges[i].Src = n.ID
		}
		if edges[i].Dst == "" {
			edges[i].Dst = n.ID
		}
		edges[i].CID = EdgeCID(edges[i].Src, edges[i].Type, edges[i].Dst)
	}
	diags = append(diags, checkIdiom(*n, edges)...)
	// An unquoted `#` mid-value is a YAML comment, so `Rhoda Dorsey (Sewer District
	// #2)` silently becomes `Rhoda Dorsey (Sewer District`. Nothing downstream can
	// tell — the value parsed fine — so catch the tell: an unclosed bracket.
	if d, ok := checkTruncated(path, line, n); !ok {
		diags = append(diags, d)
	}
	n.CID = CID(n.Kind, n.Body)
	return n, edges, diags
}

func assignScalar(dl Dialect, n *Node, key string, v *yaml.Node, path string, line int) (Diag, bool) {
	bad := func(f string, a ...any) (Diag, bool) {
		return Diag{File: path, Line: line, Severity: SevError, Msg: fmt.Sprintf(f, a...)}, false
	}
	s := v.Value
	switch key {
	case "id":
		n.ID = s
	case "status":
		if !validStatus[Status(s)] {
			if s == "disputed" {
				return bad("`disputed` is computed from conflicting evidence, never authored")
			}
			if s == "underdetermined" {
				return bad("`underdetermined` is not a status — it says the claim is not one fact. " +
					"Use `underdetermined: <what it conflates>` and keep the real status")
			}
			return bad("unknown status %q", s)
		}
		n.Status = Status(s)
	case "owner":
		n.Owner = s
	case "reason":
		n.Reason = s
	case "title":
		// Stored here, CHECKED after the loop. The kind is set by whichever body
		// key appears, and a node that writes `title:` above `group:` would
		// otherwise be told it is not a group by a parser that has not read the
		// line saying it is.
		n.Title = s
	case "supersedes_reason":
		n.SupersedesReason = s
	case "while":
		n.While = s
	case "at":
		if strings.HasPrefix(s, "=") {
			n.AtExpr = s
		} else {
			n.At = s
			if p := inferPrecision(s); p != "" {
				n.Precision = p
			}
		}
	case "from":
		n.OccFrom = s
	case "to":
		n.OccTo = s
	case "valid_from":
		n.ValidFrom = s
	case "valid_until":
		n.ValidUntil = s
	case "precision":
		if !validPrecision[Precision(s)] {
			return bad("unknown precision %q", s)
		}
		n.Precision = Precision(s)
	case "value":
		if strings.HasPrefix(s, "=") {
			n.ValueExpr = s
			break
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return bad("value %q is not a number or a `= expr`", s)
		}
		n.Value, n.HasValue = f, true
	case "unit":
		n.Unit = s
	case "ordered":
		n.Ordered = s == "true"
	case "exhaustive":
		n.Exhaustive = s == "true"
	case "aliases":
		n.Aliases = parseAliases(v)
	case "underdetermined":
		// A bare `true` is a shrug. The value has to name what the claim conflates,
		// or nobody downstream can act on it and it never gets split.
		if s == "true" || s == "false" || s == "yes" || s == "no" {
			return bad("`underdetermined: %s` says nothing — give the reason it is not one fact", s)
		}
		n.Underdetermined = strings.Join(strings.Fields(s), " ")
	case "same_as":
		if s == n.ID {
			return bad("`same_as` of %q names itself", n.ID)
		}
		n.SameAs = s
	case "needs":
		if !validNeeds[Needs(s)] {
			return bad("unknown needs %q — one of evidence, decision, reply, analysis", s)
		}
		n.Needs = Needs(s)
	case "by", "medium", "recorded", "doc", "anchor", "class", "derived_from":
		if n.Source == nil {
			n.Source = &Source{}
		}
		switch key {
		case "by":
			n.Source.Speaker = s
		case "medium":
			n.Source.Medium = s
		case "recorded":
			n.Source.Recorded = s == "true"
		case "class":
			// AGAINST THE INDEX'S OWN LADDER. Refusing a class here against the
			// wrong vocabulary rejects a corpus's own words — the rung exists,
			// just not in the dialect the parser happened to be holding.
			if !dl.Valid(s) {
				return bad("unknown source class %q in the %s dialect; it has %s",
					s, dl.Name(), strings.Join(append(dl.Ladder(), dl.Governs()), ", "))
			}
			n.Source.Class = s
		case "derived_from":
			n.Source.Premises = idList(v)
		case "doc":
			// `#` is NOT an anchor separator here. It used to be, which silently
			// truncated `.../Record of survey file #201907220018 - ....txt` to
			// `.../Record of survey file ` — a real court filename, pointing at a
			// file that does not exist, and only caught because the lock could not
			// hash it. A path is a path; `anchor:` is the explicit field, and
			// nothing in either corpus ever used the implicit form.
			n.Source.DocPath = s
		case "anchor":
			n.Source.Anchor = s
		}
	}
	return Diag{}, true
}

// inferPrecision keeps "2026-07" from ever rendering as a specific day.
func inferPrecision(s string) Precision {
	switch len(s) {
	case 4:
		return PYear
	case 7:
		return PMonth
	}
	return ""
}

// checkHashCommentIn is the check itself, over any YAML source.
//
// IT BELONGS TO THE FORMAT, NOT TO THE FILE, and it took the conversion to
// notice: it lived on the markdown-block reader, so moving fixtures to the log
// silently dropped it. An unquoted `#` truncates a value in YAML wherever that
// YAML came from — a `doc:` path with a recording number in it is the case that
// motivated it, and nothing about that depends on a fence being nearby.
func checkHashCommentIn(path string, base int, body string) []Diag {
	var out []Diag
	for i, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := strings.Cut(trimmed, ": ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		if strings.HasPrefix(val, `"`) || strings.HasPrefix(val, "'") {
			continue
		}
		idx := strings.Index(val, " #")
		if idx < 0 {
			continue
		}
		out = append(out, Diag{File: path, Line: base + i, Severity: SevError, Msg: fmt.Sprintf(
			"unquoted `#` in %s — YAML reads it as a comment, so the value truncates to %q "+
				"and everything after is lost. Quote it.",
			strings.TrimPrefix(key, "- "), strings.TrimSpace(val[:idx]))})
	}
	return out
}

// checkTruncated flags a body that ends inside an unclosed bracket, which is
// almost always YAML having eaten an unquoted `#` and everything after it.
func checkTruncated(path string, line int, n *Node) (Diag, bool) {
	var depth int
	for _, r := range n.Body {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		}
	}
	if depth == 0 {
		return Diag{}, true
	}
	return Diag{File: path, Line: line, Severity: SevWarn, Msg: fmt.Sprintf(
		"%q ends inside an unclosed bracket — an unquoted `#` is a YAML comment, so `(Sewer District #2)` "+
			"truncates to `(Sewer District`. Quote the value.", n.Body)}, false
}

// checkIdiom enforces one spelling per relation. Two ways to write the same
// fact means two sem_hashes and a phantom delta, so the inverted forms are
// rejected rather than accepted.
func checkIdiom(n Node, edges []Edge) []Diag {
	var out []Diag
	for _, e := range edges {
		if e.Src != n.ID {
			continue // authored as an inverse; already canonical
		}
		switch {
		case n.Kind == KQuestion && e.Type == ESupports:
			out = append(out, Diag{File: n.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("a question cannot `supports` %q — write `requires: %s` on that node instead", e.Dst, n.ID)})
		case n.Kind == KQuestion && e.Type == EAnswers:
			out = append(out, Diag{File: n.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("`answers` points answer → question; %q is a question. Use `requires` to gate, or `about` to attach", n.ID)})
		case n.Kind != KSource && e.Type == EAttests:
			out = append(out, Diag{File: n.File, Line: e.Line, Severity: SevError, Msg: fmt.Sprintf("only a source can `attests`; write `attested_by:` on %q instead", n.ID)})
		}
	}
	return out
}

// edgeRef is one edge target plus any qualifiers authored in long form:
//
//	requires:
//	  - id: denial-letter-deficient
//	    valid_until: 2026-11-23
type edgeRef struct {
	id, validFrom, validUntil, while, because string
}

func (r edgeRef) edge(src, dst string, t EdgeType, path string, line int) Edge {
	return Edge{Src: src, Dst: dst, Type: t, ValidFrom: r.validFrom,
		ValidUntil: r.validUntil, While: r.while, Because: r.because, File: path, Line: line}
}

func edgeRefs(v *yaml.Node) []edgeRef {
	var out []edgeRef
	add := func(s string) {
		if s = strings.TrimPrefix(strings.TrimSpace(s), "#"); s != "" {
			out = append(out, edgeRef{id: s})
		}
	}
	switch v.Kind {
	case yaml.ScalarNode:
		add(v.Value)
	case yaml.SequenceNode:
		for _, c := range v.Content {
			if c.Kind != yaml.MappingNode {
				add(c.Value)
				continue
			}
			var r edgeRef
			for i := 0; i+1 < len(c.Content); i += 2 {
				val := strings.TrimPrefix(c.Content[i+1].Value, "#")
				switch c.Content[i].Value {
				case "id":
					r.id = val
				case "valid_from":
					r.validFrom = val
				case "valid_until":
					r.validUntil = val
				case "while":
					r.while = val
				case "because":
					// Not stripped of a leading '#': this is prose, not an id.
					r.because = strings.TrimSpace(c.Content[i+1].Value)
				}
			}
			if r.id != "" {
				out = append(out, r)
			}
		}
	}
	return out
}

// parseAliases reads `aliases:` as bare names or `{as, from, until}` records.
func parseAliases(v *yaml.Node) []Alias {
	var out []Alias
	switch v.Kind {
	case yaml.ScalarNode:
		if v.Value != "" {
			out = append(out, Alias{As: v.Value})
		}
	case yaml.SequenceNode:
		for _, c := range v.Content {
			if c.Kind != yaml.MappingNode {
				if c.Value != "" {
					out = append(out, Alias{As: c.Value})
				}
				continue
			}
			var a Alias
			for i := 0; i+1 < len(c.Content); i += 2 {
				switch c.Content[i].Value {
				case "as", "name":
					a.As = c.Content[i+1].Value
				case "from":
					a.From = c.Content[i+1].Value
				case "until":
					a.Until = c.Content[i+1].Value
				}
			}
			if a.As != "" {
				out = append(out, a)
			}
		}
	}
	return out
}

// idList accepts a bare scalar or a sequence, and tolerates a leading '#'.
func idList(v *yaml.Node) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimPrefix(strings.TrimSpace(s), "#"); s != "" {
			out = append(out, s)
		}
	}
	switch v.Kind {
	case yaml.ScalarNode:
		add(v.Value)
	case yaml.SequenceNode:
		for _, c := range v.Content {
			if c.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(c.Content); i += 2 {
					if c.Content[i].Value == "id" {
						add(c.Content[i+1].Value)
					}
				}
				continue
			}
			add(c.Value)
		}
	}
	return out
}

// isEdgeEntry reports a list item that is a STANDALONE RELATION rather than a
// node: no body key, and both endpoints named.
//
// Both halves are load-bearing. Requiring the endpoints stops a node whose kind
// was typo'd from being silently reinterpreted as an edge — it stays the "no
// kind" error it has always been. Requiring the ABSENCE of a body key is what
// keeps `from:`/`to:` unambiguous, because those are node keys too (an
// occurrence span) and will collide by name forever.
func isEdgeEntry(m *yaml.Node) bool {
	if m.Kind != yaml.MappingNode {
		return false
	}
	var from, to bool
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		if _, isBody := bodyKeys[k]; isBody {
			return false
		}
		switch k {
		case "from":
			from = true
		case "to":
			to = true
		}
	}
	return from && to
}

// parseEdge reads the standalone relation form:
//
//   - from: s-ros-2026
//     to: el-drawing-defect
//     is: attests
//     by: carl-taylor
//
// `is:` names the relation and is resolved the same way a node-authored key is —
// core table first, then the index's dialect — so a standalone edge can say
// nothing a short-form edge could not. It is a second SURFACE, not a second
// model.
func parseEdge(dl Dialect, path string, line int, m *yaml.Node) (*Edge, []Diag) {
	var diags []Diag
	bad := func(f string, a ...any) {
		diags = append(diags, Diag{File: path, Line: line, Severity: SevError, Msg: fmt.Sprintf(f, a...)})
	}
	e := &Edge{File: path, Line: line}
	var isKey string
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i].Value, m.Content[i+1]
		switch k {
		case "id":
			e.ID = v.Value
		case "from":
			e.Src = v.Value
		case "to":
			e.Dst = v.Value
		case "is":
			isKey = v.Value
		case "valid_from":
			e.ValidFrom = v.Value
		case "valid_until":
			e.ValidUntil = v.Value
		case "while":
			e.While = v.Value
		case "because":
			e.Because = v.Value
		case "one_way":
			e.OneWay = v.Value == "true"
		default:
			if v.Kind != yaml.ScalarNode {
				bad("relation annotation %q must be a scalar; the bag holds values, not structure", k)
				continue
			}
			if e.Bag == nil {
				e.Bag = map[string]string{}
			}
			e.Bag[k] = v.Value
		}
	}
	if isKey == "" {
		bad("a relation must say what it `is:` — the relation type, e.g. `is: attests`")
		return nil, diags
	}
	// Resolve exactly as the node-authored form does, and in the same order: the
	// core table wins, then the dialect. An inverse key is accepted and swapped
	// here rather than refused, because "which end do I author it on" is the
	// question the standalone form exists to make irrelevant.
	switch t, ok := forwardEdge[isKey]; {
	case ok:
		e.Type = t
	default:
		if t, ok := inverseEdge[isKey]; ok {
			e.Type, e.Src, e.Dst = t, e.Dst, e.Src
		} else if t, ok := dl.Edge(isKey); ok {
			e.Type = t
		} else {
			bad("unknown relation %q; not a core relation and not one %s declares", isKey, dl.Name())
			return nil, diags
		}
	}
	if e.Src == "" || e.Dst == "" {
		bad("a relation needs both `from:` and `to:`")
		return nil, diags
	}
	e.CID = EdgeCID(e.Src, e.Type, e.Dst)
	return e, diags
}

// nodeSubtypes is the resolved `is:` of one node: which subtypes it carries and,
// for each, the prefix its keys wear.
type nodeSubtypes struct {
	// byPrefix maps the authored prefix ("" for unprefixed) to the subtype.
	byPrefix map[string]Subtype
	names    []string // as authored, for diagnostics
}

// parseIs resolves a node's `is:` entries against the dialect.
//
// The form is `prefix:subtype`, matching the order the prefixed KEY then reads
// in — `acq:acquisition` licenses `acq:where` — so the declaration and its keys
// run the same way left to right.
func parseIs(dl Dialect, v *yaml.Node, kind Kind, path string, line int) (nodeSubtypes, []Diag) {
	var diags []Diag
	bad := func(f string, a ...any) {
		diags = append(diags, Diag{File: path, Line: line, Severity: SevError, Msg: fmt.Sprintf(f, a...)})
	}
	subs := nodeSubtypes{byPrefix: map[string]Subtype{}}
	for _, raw := range idList(v) {
		prefix, name := "", raw
		if i := strings.IndexByte(raw, ':'); i >= 0 {
			prefix, name = raw[:i], raw[i+1:]
		}
		st, ok := dl.Subtype(name)
		if !ok {
			bad("unknown subtype %q; %s declares %s", name, dl.Name(), strings.Join(dl.SubtypeNames(), ", "))
			continue
		}
		// The subtype names the core kind it must be. This is the check that keeps
		// `is:` honest: without it an `element` could be written on a question and
		// nothing downstream would know the difference until a derivation read it.
		if st.Kind() != kind {
			bad("subtype %q is a %s, but this node is a %s", name, st.Kind(), kind)
			continue
		}
		if _, dup := subs.byPrefix[prefix]; dup {
			bad("two subtypes share the prefix %q; give one of them a different prefix", prefix)
			continue
		}
		subs.byPrefix[prefix] = st
		subs.names = append(subs.names, raw)
	}
	return subs, diags
}

// governs routes an authored key to the subtype that declares it.
//
// An unprefixed key is looked up in the unprefixed subtype only. A key
// `acq:where` is looked up in the subtype registered under prefix `acq`. That is
// the whole resolution: no search across prefixes, no fallback, because a key
// that could resolve two ways is the ambiguity prefixing exists to remove.
func (s nodeSubtypes) governs(key string) bool {
	prefix, k := "", key
	if i := strings.IndexByte(key, ':'); i >= 0 {
		prefix, k = key[:i], key[i+1:]
	}
	st, ok := s.byPrefix[prefix]
	return ok && st.Governs(k)
}

// collision reports a bare key that more than one carried subtype would govern
// if they were all unprefixed. Only one subtype may be unprefixed, so the real
// collision is between the unprefixed subtype and a prefixed one that also
// declares the key — which is FINE, since the prefix distinguishes them. The
// case that is not fine is caught in parseIs: two subtypes sharing a prefix.
//
// Kept as a named concept because the error a person actually hits is "I wrote
// `where:` and two of my subtypes have one", and that message has to say prefix
// one of them rather than merely "unknown field".
func (s nodeSubtypes) claimants(key string) []string {
	var out []string
	for prefix, st := range s.byPrefix {
		if st.Governs(key) {
			if prefix == "" {
				out = append(out, "(unprefixed)")
			} else {
				out = append(out, prefix)
			}
		}
	}
	sort.Strings(out)
	return out
}
