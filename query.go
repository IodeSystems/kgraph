package kgraph

import (
	"fmt"
	"strings"
	"unicode"
)

// The surface DSL is what humans and doc headers read. It lowers to this AST,
// which is JSON-serializable and is what the MCP tool accepts directly — an
// agent can emit the AST schema-constrained and never produce a syntax error.
// One evaluator serves both.

type Query interface{ isQuery() }

type Union struct {
	Terms []Query `json:"union"`
}
type Inter struct {
	Terms []Query `json:"inter"`
}
type Not struct {
	X Query `json:"not"`
}
type Ref struct {
	Name string `json:"ref"`
}

// Pattern is a chain of atoms joined by hops. The LEFTMOST atom is the result
// set; flip with an inverse hop to return the other end. Path variables and
// multi-binding returns are deliberately v2.
type Pattern struct {
	Atoms []Atom    `json:"atoms"`
	Hops  []Hop     `json:"hops,omitempty"`
	At    *Temporal `json:"at,omitempty"`
	Sort  []SortKey `json:"sort,omitempty"`
}

type Atom struct {
	Kinds []Kind `json:"kinds,omitempty"`
	ID    string `json:"id,omitempty"`
	Preds []Pred `json:"preds,omitempty"`
	Text  string `json:"text,omitempty"` // ~ "…" full-text
}

type Hop struct {
	Type EdgeType `json:"type"`
	Dir  int      `json:"dir"`  // 1 forward, -1 inverse, 0 undirected
	Star bool     `json:"star"` // transitive closure
}

// Pred is `key op value`, `key in (…)`, or a bare key meaning "is present".
type Pred struct {
	Key  string   `json:"key"`
	Op   string   `json:"op"` // = != < > <= >= in has
	Vals []string `json:"vals,omitempty"`
}

type Temporal struct {
	Now  bool   `json:"now,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type SortKey struct {
	Key  string `json:"key"`
	Desc bool   `json:"desc,omitempty"`
}

// Scoped applies a temporal context and a sort to a whole set expression.
// `@now` and `sort` are otherwise suffixes on a PATTERN, so in `A | B @now` the
// temporal binds to B alone — which over-collects silently. Parenthesise to mean
// the expression: `(A | B) @now`.
//
// The temporal pushes DOWN into every pattern that does not carry its own, so an
// inner `@2020-06-01` still wins. The sort applies to the final set.
type Scoped struct {
	X    Query     `json:"scoped"`
	At   *Temporal `json:"at,omitempty"`
	Sort []SortKey `json:"sort,omitempty"`
}

func (Scoped) isQuery()  {}
func (Union) isQuery()   {}
func (Inter) isQuery()   {}
func (Not) isQuery()     {}
func (Ref) isQuery()     {}
func (Pattern) isQuery() {}

// ── lexer ──────────────────────────────────────────────────────────────

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tValue // starts with a digit: 2026-10-31, 773.00, 180d
	tStr
	tPunct // ( ) [ ] # @ ~ & | ! , * . = != < > <= >= - -> <-
)

type tok struct {
	kind tokKind
	s    string
	pos  int
}

// scanWord consumes an identifier, slug id, or date. A '-' is part of the word
// only when an alphanumeric follows it: slug ids carry dashes (`g-live-leads`,
// `2026-10-31`) but a hop's closing dash must not be swallowed by the edge name
// in `-member_of->` or `<-requires-`.
func scanWord(src string, i int) int {
	j := i
	for j < len(src) {
		c := rune(src[j])
		switch {
		case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '.':
			j++
		case c == '-' && j+1 < len(src) && (unicode.IsLetter(rune(src[j+1])) || unicode.IsDigit(rune(src[j+1]))):
			j++
		default:
			return j
		}
	}
	return j
}

func lex(src string) ([]tok, error) {
	var out []tok
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string at %d", i)
			}
			out = append(out, tok{tStr, src[i+1 : j], i})
			i = j + 1
		case unicode.IsDigit(rune(c)):
			j := scanWord(src, i)
			out = append(out, tok{tValue, src[i:j], i})
			i = j
		case unicode.IsLetter(rune(c)) || c == '_':
			j := scanWord(src, i)
			out = append(out, tok{tIdent, src[i:j], i})
			i = j
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "->", "<-", "!=", "<=", ">=":
				out = append(out, tok{tPunct, two, i})
				i += 2
				continue
			}
			if strings.ContainsRune("()[]#@~&|!,*=<>-", rune(c)) {
				out = append(out, tok{tPunct, string(c), i})
				i++
				continue
			}
			return nil, fmt.Errorf("unexpected character %q at %d", c, i)
		}
	}
	return append(out, tok{tEOF, "", len(src)}), nil
}

// ── parser ─────────────────────────────────────────────────────────────

type parser struct {
	toks []tok
	i    int
}

// ParseQuery parses the surface DSL into the AST.
func ParseQuery(src string) (Query, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q, err := p.union()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, fmt.Errorf("unexpected %q at %d", p.cur().s, p.cur().pos)
	}
	return q, nil
}

func (p *parser) cur() tok  { return p.toks[p.i] }
func (p *parser) next() tok { t := p.toks[p.i]; p.i++; return t }
func (p *parser) isP(s string) bool {
	return p.cur().kind == tPunct && p.cur().s == s
}
func (p *parser) accept(s string) bool {
	if p.isP(s) {
		p.i++
		return true
	}
	return false
}
func (p *parser) expect(s string) error {
	if !p.accept(s) {
		return fmt.Errorf("expected %q, got %q at %d", s, p.cur().s, p.cur().pos)
	}
	return nil
}

func (p *parser) union() (Query, error) {
	first, err := p.inter()
	if err != nil {
		return nil, err
	}
	terms := []Query{first}
	for p.accept("|") {
		t, err := p.inter()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	if len(terms) == 1 {
		return first, nil
	}
	return Union{terms}, nil
}

func (p *parser) inter() (Query, error) {
	first, err := p.unary()
	if err != nil {
		return nil, err
	}
	terms := []Query{first}
	for p.accept("&") {
		t, err := p.unary()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	if len(terms) == 1 {
		return first, nil
	}
	return Inter{terms}, nil
}

func (p *parser) unary() (Query, error) {
	if p.accept("!") {
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Not{x}, nil
	}
	return p.primary()
}

func (p *parser) primary() (Query, error) {
	// `@name` is a named-query reference; `@now` / `@date` are temporal and only
	// ever appear after a pattern, so position disambiguates.
	if p.isP("@") && p.toks[p.i+1].kind == tIdent && p.toks[p.i+1].s != "now" {
		p.i++
		return Ref{p.next().s}, nil
	}
	if p.isP("(") && !p.looksLikeKindList() {
		p.i++
		q, err := p.union()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		// A parenthesised group may carry the suffixes, and then they mean the
		// whole expression rather than the nearest pattern.
		sc := Scoped{X: q}
		if p.isP("@") {
			p.i++
			t, err := p.temporal()
			if err != nil {
				return nil, err
			}
			sc.At = t
		}
		if p.cur().kind == tIdent && p.cur().s == "sort" {
			p.i++
			sk, err := p.sortKeys()
			if err != nil {
				return nil, err
			}
			sc.Sort = sk
		}
		if sc.At == nil && sc.Sort == nil {
			return q, nil
		}
		return sc, nil
	}
	return p.pattern()
}

// looksLikeKindList distinguishes `(claim|action)` — a kind alternation — from
// `(…)` grouping a subquery.
func (p *parser) looksLikeKindList() bool {
	j := p.i + 1
	for {
		if p.toks[j].kind != tIdent || !isKind(p.toks[j].s) {
			return false
		}
		j++
		if p.toks[j].kind == tPunct && p.toks[j].s == "|" {
			j++
			continue
		}
		return p.toks[j].kind == tPunct && p.toks[j].s == ")"
	}
}

func isKind(s string) bool {
	switch Kind(s) {
	case KClaim, KQuestion, KGroup, KEntity, KEvent, KAction, KSource:
		return true
	}
	return false
}

func (p *parser) pattern() (Query, error) {
	a, err := p.atom()
	if err != nil {
		return nil, err
	}
	pat := Pattern{Atoms: []Atom{a}}
	for {
		h, ok, err := p.hop()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		nxt, err := p.atom()
		if err != nil {
			return nil, err
		}
		pat.Hops = append(pat.Hops, h)
		pat.Atoms = append(pat.Atoms, nxt)
	}
	if p.isP("@") {
		p.i++
		t, err := p.temporal()
		if err != nil {
			return nil, err
		}
		pat.At = t
	}
	if p.cur().kind == tIdent && p.cur().s == "sort" {
		p.i++
		sk, err := p.sortKeys()
		if err != nil {
			return nil, err
		}
		pat.Sort = sk
	}
	return pat, nil
}

func (p *parser) hop() (Hop, bool, error) {
	dir := 0
	switch {
	case p.isP("<-"):
		dir = -1
	case p.isP("-"):
		dir = 1
	default:
		return Hop{}, false, nil
	}
	p.i++
	if p.cur().kind != tIdent {
		return Hop{}, false, fmt.Errorf("expected an edge type at %d", p.cur().pos)
	}
	name := p.next().s
	et, ok := forwardEdge[name]
	if !ok {
		return Hop{}, false, fmt.Errorf("unknown edge type %q", name)
	}
	star := p.accept("*")
	// closing side: `->` means forward, a bare `-` means undirected (or closes
	// an inverse hop opened with `<-`).
	switch {
	case p.accept("->"):
		if dir == -1 {
			return Hop{}, false, fmt.Errorf("`<-%s->` is not a direction", name)
		}
		dir = 1
	case p.accept("-"):
		if dir == 1 {
			dir = 0 // -type- is undirected
		}
	default:
		return Hop{}, false, fmt.Errorf("unterminated hop at %d", p.cur().pos)
	}
	return Hop{Type: et, Dir: dir, Star: star}, true, nil
}

func (p *parser) atom() (Atom, error) {
	var a Atom
	if p.accept("(") {
		for {
			if p.cur().kind != tIdent {
				return a, fmt.Errorf("expected a kind at %d", p.cur().pos)
			}
			a.Kinds = append(a.Kinds, Kind(p.next().s))
			if p.accept("|") {
				continue
			}
			break
		}
		if err := p.expect(")"); err != nil {
			return a, err
		}
	} else if p.cur().kind == tIdent && isKind(p.cur().s) {
		a.Kinds = []Kind{Kind(p.next().s)}
	}
	if p.accept("#") {
		// Quoted so an alias containing spaces can be referenced directly:
		// #"1440 North Northlea Road".
		switch p.cur().kind {
		case tIdent, tValue, tStr:
			a.ID = p.next().s
		default:
			return a, fmt.Errorf("expected a node id or quoted alias after # at %d", p.cur().pos)
		}
	}
	if p.accept("[") {
		for {
			pr, err := p.pred()
			if err != nil {
				return a, err
			}
			a.Preds = append(a.Preds, pr)
			if p.accept(",") {
				continue
			}
			break
		}
		if err := p.expect("]"); err != nil {
			return a, err
		}
	}
	if p.accept("~") {
		if p.cur().kind != tStr {
			return a, fmt.Errorf("expected a quoted string after ~ at %d", p.cur().pos)
		}
		a.Text = p.next().s
	}
	if len(a.Kinds) == 0 && a.ID == "" && len(a.Preds) == 0 && a.Text == "" {
		return a, fmt.Errorf("empty atom at %d", p.cur().pos)
	}
	return a, nil
}

func (p *parser) pred() (Pred, error) {
	if p.cur().kind != tIdent {
		return Pred{}, fmt.Errorf("expected a field name at %d", p.cur().pos)
	}
	key := p.next().s
	if p.cur().kind == tIdent && p.cur().s == "in" {
		p.i++
		if err := p.expect("("); err != nil {
			return Pred{}, err
		}
		pr := Pred{Key: key, Op: "in"}
		for {
			pr.Vals = append(pr.Vals, p.next().s)
			if p.accept(",") {
				continue
			}
			break
		}
		return pr, p.expect(")")
	}
	for _, op := range []string{"!=", "<=", ">=", "=", "<", ">"} {
		if p.isP(op) {
			p.i++
			pr := Pred{Key: key, Op: op}
			for {
				pr.Vals = append(pr.Vals, p.next().s)
				if p.accept("|") { // value alternation: status=asserted|open
					continue
				}
				break
			}
			if len(pr.Vals) > 1 && pr.Op == "=" {
				pr.Op = "in"
			}
			return pr, nil
		}
	}
	return Pred{Key: key, Op: "has"}, nil // bare key = present
}

func (p *parser) temporal() (*Temporal, error) {
	if p.cur().kind == tIdent && p.cur().s == "now" {
		p.i++
		return &Temporal{Now: true}, nil
	}
	if p.accept("[") {
		from := p.next().s
		if err := p.expect(","); err != nil {
			return nil, err
		}
		to := p.next().s
		return &Temporal{From: from, To: to}, p.expect("]")
	}
	if p.cur().kind != tValue {
		return nil, fmt.Errorf("expected now, a date, or [from,to] at %d", p.cur().pos)
	}
	d := p.next().s
	return &Temporal{From: d, To: d}, nil
}

func (p *parser) sortKeys() ([]SortKey, error) {
	var out []SortKey
	for {
		if p.cur().kind != tIdent {
			return nil, fmt.Errorf("expected a sort field at %d", p.cur().pos)
		}
		k := SortKey{Key: p.next().s}
		if p.cur().kind == tIdent {
			switch p.cur().s {
			case "desc":
				k.Desc = true
				p.i++
			case "asc":
				p.i++
			}
		}
		out = append(out, k)
		if p.accept(",") {
			continue
		}
		return out, nil
	}
}
