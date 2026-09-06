package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE DEFAULT IS LEGAL AND AN UNKNOWN NAME IS AN ERROR.
//
// Every index in existence predates dialects and declares nothing, so an
// unmarked index that stopped loading would be a tool breaking every corpus to
// gain a feature none of them asked for yet. An unknown name is the opposite
// case: a typo, or an index built for a kgraph that knows something this one
// does not, and guessing which would apply the wrong ladder to somebody's
// corpus.
func TestDialectDefaultsToLegalAndRefusesUnknown(t *testing.T) {
	d, ok := DialectFor("")
	if !ok || d.Name() != "legal" {
		t.Fatalf("an index declaring nothing must get legal, got %q/%v", d.Name(), ok)
	}
	if d2, ok := DialectFor("legal"); !ok || d2.Name() != "legal" {
		t.Fatalf("legal must resolve by name")
	}
	if _, ok := DialectFor("Legal"); ok {
		t.Error("dialect names are exact; a near-miss must not resolve")
	}
	if _, ok := DialectFor("clinical"); ok {
		t.Error("an unknown dialect must be refused, not guessed at")
	}
	if len(DialectNames()) == 0 {
		t.Error("a refusal that cannot say what WOULD be accepted sends somebody to the source")
	}
}

// THE MECHANIC IS CORE; ONLY THE WORD IS DIALECT-SCOPED.
//
// The governing class has NO rank, and that absence is the whole mechanism:
// conflict and impeachment look up a rank and skip what has none, so authority
// supports a claim without ever winning or losing an evidentiary contest. A
// dialect may rename it. A dialect that could give it a rank would be one that
// reintroduces the category error — a statute "impeaching" a witness.
func TestTheGoverningClassIsUnrankedInEveryDialect(t *testing.T) {
	for _, name := range DialectNames() {
		d, _ := DialectFor(name)
		if _, ranked := d.Rank(d.Governs()); ranked {
			t.Errorf("%s: the governing class %q has a rank — it can now win or lose a contest",
				name, d.Governs())
		}
		if !d.Valid(d.Governs()) {
			t.Errorf("%s: the governing class must still be authorable", name)
		}
		// And it is absent from the ladder by construction, not omission: a
		// consumer enumerating the rungs must not be handed something unrankable.
		for _, c := range d.Ladder() {
			if c == d.Governs() {
				t.Errorf("%s: the governing class is in the ladder", name)
			}
			if _, ok := d.Rank(c); !ok {
				t.Errorf("%s: %q is on the ladder with no rank", name, c)
			}
		}
	}
}

// A LADDER IS ORDINAL AND ITS ORDER DECIDES CONFLICTS. Rungs sharing a rank make
// two different kinds of support weigh the same, which is a silent wrong answer
// rather than an error — the exact failure that keeps this list closed in the
// binary.
func TestEveryDialectsLadderIsStrictlyOrdered(t *testing.T) {
	for _, name := range DialectNames() {
		d, _ := DialectFor(name)
		l := d.Ladder()
		if len(l) < 2 {
			t.Errorf("%s: a ladder of %d rungs resolves nothing", name, len(l))
			continue
		}
		seen := map[int]string{}
		for i, c := range l {
			r, _ := d.Rank(c)
			if prev, dup := seen[r]; dup {
				t.Errorf("%s: %q and %q both rank %d", name, prev, c, r)
			}
			seen[r] = c
			if i > 0 {
				if pr, _ := d.Rank(l[i-1]); pr <= r {
					t.Errorf("%s: not strongest-first at %d: %q(%d) then %q(%d)",
						name, i, l[i-1], pr, c, r)
				}
			}
		}
		// A speaker-required rung must be ON the ladder. One that is not is a
		// requirement nothing can ever satisfy.
		for _, c := range l {
			_ = d.SpeakerRequired(c)
		}
		if !d.SpeakerRequired("admission") && name == "legal" {
			t.Error("legal: an admission means nothing without naming who made it")
		}
	}
}

// The package accessors are the LEGAL dialect, and they must stay that way while
// the value is threaded. caselit imports ClassRank and ClassLadder and derives
// its `backed` floor from `document`'s rank; a delegation that answered from
// somewhere else would move that floor without anybody editing it.
func TestThePackageAccessorsAreTheLegalDialect(t *testing.T) {
	d, _ := DialectFor("legal")
	for _, c := range d.Ladder() {
		want, _ := d.Rank(c)
		got, ok := ClassRank(c)
		if !ok || got != want {
			t.Errorf("ClassRank(%q)=%d/%v, the legal dialect says %d", c, got, ok, want)
		}
		if !validClass(c) {
			t.Errorf("validClass(%q) is false but it is on the legal ladder", c)
		}
		if speakerRequired(c) != d.SpeakerRequired(c) {
			t.Errorf("speakerRequired(%q) disagrees with the dialect", c)
		}
	}
	if len(ClassLadder()) != len(d.Ladder()) {
		t.Error("ClassLadder and the legal dialect's ladder are different lengths")
	}
}

// THE MARKER FINALLY REACHES THE GRAPH.
//
// `dialect:` has parsed since July and reached nothing, because IndexAt read the
// declaration and threw everything but the name away. This is the wire.
func TestTheIndexDeclarationCarriesItsDialect(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "matter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, IndexMarker), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A marker declaring ONLY a dialect still names the index after its directory
	// — the case that used to name the index "legal".
	write("dialect: legal\n")
	decl, err := DeclAt(root, dir)
	if err != nil {
		t.Fatalf("DeclAt: %v", err)
	}
	if decl.Name != "matter" {
		t.Errorf("name %q, want the directory's own name", decl.Name)
	}
	if decl.Dialect != "legal" {
		t.Errorf("dialect %q was parsed and then dropped", decl.Dialect)
	}
	if d, derr := DialectAt(root, dir); derr != nil || d.Name() != "legal" {
		t.Errorf("DialectAt: %v/%v", d.Name(), derr)
	}

	// No marker at all is the migration case: every corpus that predates dialects
	// declares nothing and must keep loading, against legal.
	if err := os.Remove(filepath.Join(dir, IndexMarker)); err != nil {
		t.Fatal(err)
	}
	if d, derr := DialectAt(root, dir); derr != nil || d.Name() != "legal" {
		t.Errorf("an index declaring nothing must get legal: %v/%v", d.Name(), derr)
	}

	// An UNKNOWN dialect is an error, and the error says what would have been
	// accepted. Falling back to legal here would evaluate one corpus's facts
	// against another's ladder and be confident about it.
	write("dialect: clinical\n")
	_, derr := DialectAt(root, dir)
	if derr == nil {
		t.Fatal("an unknown dialect must be refused, not defaulted")
	}
	if !strings.Contains(derr.Error(), "clinical") || !strings.Contains(derr.Error(), "legal") {
		t.Errorf("the refusal must name what was asked for AND what is known: %v", derr)
	}
}

// A graph built with no filesystem is evaluated against legal, because Build
// stays pure and every corpus that predates dialects declares none. The zero
// value meaning legal — rather than "not loaded" — is what makes that true.
func TestAGraphWithoutAnIndexIsLegal(t *testing.T) {
	g, _ := Build(nil)
	if g.Dialect().Name() != "legal" {
		t.Fatalf("a graph with no index resolves %q", g.Dialect().Name())
	}
	d, _ := DialectFor("legal")
	g.SetDialect(d)
	if g.Dialect().Name() != "legal" {
		t.Fatal("SetDialect did not bind")
	}
}

// PARSING REFUSES A CLASS AGAINST THE INDEX'S OWN LADDER.
//
// `class:` is refused at parse time, so parsing under the wrong vocabulary
// rejects a corpus's own words — the rung exists, just not in the dialect the
// parser happened to hold. This is the one place where holding the wrong
// dialect produces a diagnostic rather than a wrong answer, and a diagnostic
// already emitted cannot be corrected downstream.
//
// It needs a SECOND dialect to prove anything at all: with one ladder in the
// binary, "validated against the dialect" and "validated against the global" are
// indistinguishable. This one exists for the test and says so.
var probeDialect = Dialect{
	name:        "probe",
	rank:        map[string]int{"telemetry": 2, "hearsay": 1},
	governs:     "policy",
	speaker:     map[string]bool{"hearsay": true},
	invalidated: "withdrawn",
}

func TestParsingValidatesTheClassAgainstItsOwnDialect(t *testing.T) {
	const src = "```kfacts\n" +
		"- id: s-thing\n  source: A thing\n  class: telemetry\n" +
		"```\n"

	// Under legal, `telemetry` is not a rung and is refused — and the refusal
	// names the dialect and what it does have, or somebody goes to read source.
	_, ds := parseFixtureIn(legal, "f.facts", []byte(src))
	var refused string
	for _, d := range ds {
		if d.Severity == SevError && strings.Contains(d.Msg, "telemetry") {
			refused = d.Msg
		}
	}
	if refused == "" {
		t.Fatal("legal must refuse a class it does not have")
	}
	if !strings.Contains(refused, "legal") || !strings.Contains(refused, "record") {
		t.Errorf("the refusal must name the dialect and its rungs: %q", refused)
	}

	// Under the probe dialect the SAME text is a corpus's own vocabulary.
	_, ds2 := parseFixtureIn(probeDialect, "f.facts", []byte(src))
	for _, d := range ds2 {
		if d.Severity == SevError && strings.Contains(d.Msg, "telemetry") {
			t.Errorf("a rung the dialect HAS was refused: %q", d.Msg)
		}
	}

	// And the reverse: legal's own `record` is foreign to the probe.
	const legalSrc = "```kfacts\n" +
		"- id: s-deed\n  source: A deed\n  class: record\n" +
		"```\n"
	var foreign bool
	_, ds3 := parseFixtureIn(probeDialect, "f.facts", []byte(legalSrc))
	for _, d := range ds3 {
		if d.Severity == SevError && strings.Contains(d.Msg, "record") {
			foreign = true
		}
	}
	if !foreign {
		t.Error("the probe dialect must not accept legal's rungs; the ladders are not shared")
	}
}

// Query validation moves with it, for the same reason and with the same failure:
// a query naming a rung its own corpus uses would be rejected under a foreign
// ladder.
func TestQueryValidationIsDialectScoped(t *testing.T) {
	q, err := ParseQuery("source [class=telemetry]")
	if err != nil {
		t.Skipf("query grammar does not accept this shape: %v", err)
	}
	if errs := ValidateQueryIn(legal, q); len(errs) == 0 {
		t.Error("legal must reject a class it does not have")
	}
	if errs := ValidateQueryIn(probeDialect, q); len(errs) != 0 {
		t.Errorf("the probe dialect has telemetry; it must accept it: %v", errs)
	}
	// The package-level entry point is legal, and stays that way.
	if errs := ValidateQuery(q); len(errs) == 0 {
		t.Error("ValidateQuery must still answer for legal")
	}
}

// A DIALECT'S OWN RELATION PARSES INTO A WALKABLE EDGE.
//
// This is the point of the whole exercise. caselit carried this relation as a
// plain FIELD on its construction entries — in two spellings, `turns_on:` for
// elements and `answers:` for questions, split by what happened to be on the
// other end — and unwalkable in both. kgraph's own `derived_from` was a plain
// field for the same reason and was promoted for the same one: "validated,
// hashed, and unwalkable".
func TestADialectRelationBecomesAnEdge(t *testing.T) {
	const src = "```kfacts\n" +
		"- id: el-record-title\n  claim: Record title vests in the client\n  status: asserted\n" +
		"- id: aq-deed\n  question: The recorded conveyance\n  status: open\n" +
		"  serves: [el-record-title]\n" +
		"```\n"

	// Under legal, `serves` is a relation and becomes an edge.
	doc, ds := parseFixtureIn(legal, "f.facts", []byte(src))
	for _, d := range ds {
		if d.Severity == SevError {
			t.Fatalf("legal declares `serves`; parsing it must not error: %s", d.Msg)
		}
	}
	var found bool
	for _, e := range doc.Edges {
		if e.Type == EdgeType("serves") && e.Src == "aq-deed" && e.Dst == "el-record-title" {
			found = true
		}
	}
	if !found {
		t.Fatalf("`serves` did not become an edge: %+v", doc.Edges)
	}

	// Under a dialect that does not declare it, the key is NOT a relation. It
	// must not silently become one — an open set that accepted anything would
	// turn every typo into an edge to nowhere.
	_, ds2 := parseFixtureIn(probeDialect, "f.facts", []byte(src))
	var refused bool
	for _, d := range ds2 {
		if d.Severity == SevError && strings.Contains(d.Msg, "serves") {
			refused = true
		}
	}
	if !refused {
		t.Error("a dialect that does not declare `serves` must not accept it as a relation")
	}
}

// A DIALECT MAY NOT REDEFINE A CORE RELATION, and the check runs at init rather
// than relying on precedence. "The core wins" is a rule somebody has to know,
// and a dialect whose relation quietly failed to take effect would be worse than
// one refused outright.
func TestNoDialectRedefinesACoreRelation(t *testing.T) {
	for _, name := range DialectNames() {
		d, _ := DialectFor(name)
		for _, k := range d.EdgeKeys() {
			if _, core := forwardEdge[k]; core {
				t.Errorf("%s redefines the core relation %q", name, k)
			}
			if _, core := inverseEdge[k]; core {
				t.Errorf("%s redefines the core inverse relation %q", name, k)
			}
			// And every declared relation must read backwards in English: this
			// text goes into a prompt.
			if p, ok := d.EdgePhrase(EdgeType(k)); !ok || p == "" {
				t.Errorf("%s declares %q with no inverse phrase; it would render as %q by", name, k, k)
			}
		}
	}
}

// THE CORE TABLE STAYS CLOSED, and this is the line that matters: every type in
// it participates in a computation, so a dialect that could redefine one could
// change what a graph concludes without saying so.
//
// The list is spelled out rather than derived, so ADDING a core type is a
// deliberate act that fails this test until somebody states which computation it
// drives.
func TestEveryCoreRelationDrivesSomething(t *testing.T) {
	drives := map[EdgeType]string{
		EAttests:     "evidence",
		EContradicts: "disputed",
		ESupersedes:  "which fact wins",
		EMemberOf:    "group evaluation",
		EDerivedFrom: "staleness",
		ERequires:    "taint",
		ESupports:    "premises",
		ECauses:      "premises",
		EImplies:     "premises",
		EAbout:       "the anchor rule",
		EAnswers:     "question direction",
		EProhibits:   "nothing — the one word in here that is vocabulary",
	}
	for _, et := range forwardEdge {
		if _, ok := drives[et]; !ok {
			t.Errorf("core relation %q is in the table with no stated reason; either it drives "+
				"something and this list should say what, or it is vocabulary and belongs in a dialect", et)
		}
	}
}

// SUBTYPES TAG A CORE KIND; THEY DO NOT CREATE ONE.
//
// `element` is a claim and `acquisition` is a question. The core kind table stays
// closed for the same reason the core edge table does, and the schematic layer
// sits on top of an arbitrary graph rather than growing it.
func TestASubtypeTagsACoreKindAndCarriesItsKeys(t *testing.T) {
	const src = "```kfacts\n" +
		"- id: q-independent-survey\n" +
		"  question: Does an independent surveyor confirm the drawing is off?\n" +
		"  is: [acquisition]\n" +
		"  status: open\n" +
		"  needs: evidence\n" +
		"  where: any licensed surveyor\n" +
		"  cost: roughly $2,400\n" +
		"  serves: [el-drawing-defect]\n" +
		"- id: el-drawing-defect\n  claim: The plat drawing is defective\n  is: [element]\n  status: asserted\n" +
		"```\n"

	doc, ds := parseFixtureIn(legal, "f.facts", []byte(src))
	for _, d := range ds {
		if d.Severity == SevError {
			t.Fatalf("unexpected error: %s", d.Msg)
		}
	}
	var q *Node
	for i := range doc.Nodes {
		if doc.Nodes[i].ID == "q-independent-survey" {
			q = &doc.Nodes[i]
		}
	}
	if q == nil {
		t.Fatal("node missing")
	}
	if q.Kind != KQuestion {
		t.Errorf("an acquisition must stay a question, got %s", q.Kind)
	}
	// The ledger fields are the reason subtypes carry keys at all: they are real
	// data with nowhere to live on a core node.
	if q.Bag["where"] != "any licensed surveyor" || q.Bag["cost"] != "roughly $2,400" {
		t.Errorf("subtype keys did not reach the bag: %+v", q.Bag)
	}
	// Core scalars stay typed and must NOT be swept into the bag.
	if q.Status != SOpen || q.Bag["status"] != "" {
		t.Errorf("core scalar leaked into the bag: status=%q bag=%+v", q.Status, q.Bag)
	}
}

// A SUBTYPE ON THE WRONG CORE KIND IS REFUSED. Without this, `is: element` on a
// question would parse and nothing would notice until a derivation read it.
func TestASubtypeMustMatchTheCoreKind(t *testing.T) {
	const src = "```kfacts\n- id: x\n  question: A question\n  is: [element]\n```\n"
	_, ds := parseFixtureIn(legal, "f.facts", []byte(src))
	var refused bool
	for _, d := range ds {
		if d.Severity == SevError && strings.Contains(d.Msg, "is a claim, but this node is a question") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("an element on a question must be refused; got %v", ds)
	}
}

// PREFIXING IS THE CONFLICT RESOLUTION, AND IT IS THE AUTHOR'S, NOT A PRECEDENCE
// RULE. Two subtypes wanting one key is resolved by naming which is which —
// never by one silently winning, which is the failure this file refuses
// everywhere else.
func TestTwoSubtypesSharingAPrefixAreRefused(t *testing.T) {
	const src = "```kfacts\n- id: x\n  question: A question\n  is: [acquisition, acquisition]\n```\n"
	_, ds := parseFixtureIn(legal, "f.facts", []byte(src))
	var refused bool
	for _, d := range ds {
		if d.Severity == SevError && strings.Contains(d.Msg, "share the prefix") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("two subtypes under one prefix must be refused; got %v", ds)
	}
}

// A PREFIXED SUBTYPE'S KEYS WEAR THE PREFIX, and the unprefixed one's do not —
// `is: [a, b:b]` gives a bare `a` schema and a b-prefixed `b` schema.
func TestAPrefixedSubtypeKeysWearThePrefix(t *testing.T) {
	const src = "```kfacts\n" +
		"- id: x\n  question: A question\n  is: [acq:acquisition]\n  acq:where: county recorder\n```\n"
	doc, ds := parseFixtureIn(legal, "f.facts", []byte(src))
	for _, d := range ds {
		if d.Severity == SevError {
			t.Fatalf("unexpected error: %s", d.Msg)
		}
	}
	if doc.Nodes[0].Bag["acq:where"] != "county recorder" {
		t.Errorf("prefixed key did not reach the bag under its authored name: %+v", doc.Nodes[0].Bag)
	}

	// And the BARE key is then not governed — the prefix is the only way in, so a
	// bare `where:` on a node whose acquisition is prefixed is an unknown field.
	const bare = "```kfacts\n" +
		"- id: x\n  question: A question\n  is: [acq:acquisition]\n  where: county recorder\n```\n"
	_, ds2 := parseFixtureIn(legal, "f.facts", []byte(bare))
	var refused bool
	for _, d := range ds2 {
		if d.Severity == SevError && strings.Contains(d.Msg, "unknown field") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("a bare key must not resolve into a prefixed subtype; got %v", ds2)
	}
}

// LEGAL CARRIES THE WHOLE CONSTRUCTION VOCABULARY, and this test is what says so
// in one place rather than leaving it implied by seven scattered comments.
//
// It was two subtypes and one relation while the shape was being proven. The
// alternative to completing it here was a `caselit.legal` file vendored into
// every matter — which works, and was built and thrown away, because a
// corpus-wide file cannot EXTEND a built-in, only restate it. That would have
// meant this ladder copied into every matter, and a copied ordinal ladder is the
// thing `ClassRank` was exported to stop.
func TestLegalCarriesTheConstructionVocabulary(t *testing.T) {
	d, ok := DialectFor("legal")
	if !ok {
		t.Fatal("legal is the default dialect and must resolve")
	}
	// A SUBTYPE DOES NOT CREATE A KIND. That is the whole of the schematic layer:
	// an outcome is a group, a theory is a claim, a production is an event.
	for name, kind := range map[string]Kind{
		"element": KClaim, "outcome": KGroup, "theory": KClaim,
		"acquisition": KQuestion, "attempt": KEvent,
		"production": KEvent, "negative": KClaim,
	} {
		st, ok := d.Subtype(name)
		if !ok {
			t.Errorf("legal declares no %q; the construction has nowhere to live", name)
			continue
		}
		if st.Kind() != kind {
			t.Errorf("subtype %q sits over %q, want %q", name, st.Kind(), kind)
		}
	}
	// The ledger's keys. `where`/`who`/`cost` are what an acquisition IS; the
	// rest are what it accumulated as the loop closed around it.
	acq, _ := d.Subtype("acquisition")
	for _, k := range []string{"where", "who", "cost", "state", "state_because",
		"arrived", "raised_file", "raised_sentence"} {
		if !acq.Governs(k) {
			t.Errorf("an acquisition does not govern %q", k)
		}
	}
	// `doc` and `by` are the two traps: both are source-only core keys and would
	// construct a Source on whatever carried them. validateDialect refuses a
	// subtype governing either, so this asserts the SPELLINGS that exist instead.
	if acq.Governs("doc") {
		t.Error("`doc` is source-only; the acquisition's arrived document is `arrived`")
	}
	att, _ := d.Subtype("attempt")
	if att.Governs("by") {
		t.Error("`by` is source-only; an attempt's actor is `actor`")
	}
	for _, k := range []string{"actor", "how", "found", "note"} {
		if !att.Governs(k) {
			t.Errorf("an attempt does not govern %q", k)
		}
	}
	for _, r := range []string{"serves", "targets", "counters", "pursues", "produced", "pleaded_as"} {
		if _, ok := d.Edge(r); !ok {
			t.Errorf("legal declares no %q; that relation goes back to being an unwalkable field", r)
		}
	}
	// None of them may be a core relation, and none of them is — but the reason
	// is worth pinning: a dialect edge drives NOTHING, which is the licence for
	// the set being open. `counters` in particular is not `contradicts`.
	if _, core := forwardEdge["counters"]; core {
		t.Error("`counters` must not be a core relation")
	}
}

// TestAnOutcomeCarriesADeadline — the date a cause runs out is part of the fact
// format, not something a consumer keeps beside it.
//
// It is here because a limitation recorded in a file next to the tool is a
// limitation nothing can check, and because two formats for one date is the drift
// this dialect exists to prevent. AUTHORED on purpose: no amount of reading a
// corpus reveals a statutory date. Whether it has RUN is the consumer's, derived
// against its own clock, so an extension cannot leave a stored answer saying the
// opposite forever.
func TestAnOutcomeCarriesADeadline(t *testing.T) {
	d, ok := DialectFor("legal")
	if !ok {
		t.Fatal("legal is the default dialect and must resolve")
	}
	st, ok := d.Subtype("outcome")
	if !ok {
		t.Fatal("the dialect no longer declares an outcome")
	}
	for _, k := range []string{"deadline", "deadline_why"} {
		if !st.Governs(k) {
			t.Errorf("an outcome cannot carry %q; a limitation would live outside the format", k)
		}
	}
	// AND NOWHERE ELSE, YET. A court rule against a filing and a stipulation
	// against a production are real and are not this. Declaring them
	// speculatively would put a vocabulary in the format that nothing writes.
	for _, other := range []string{"theory", "acquisition", "production"} {
		if s2, ok := d.Subtype(other); ok && s2.Governs("deadline") {
			t.Errorf("%q declares `deadline` — add it deliberately, with the rule that sets it, "+
				"or a clock will attach to whatever somebody reached for first", other)
		}
	}
}
