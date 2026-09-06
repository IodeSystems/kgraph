package kgraph

import "sort"

// A dialect is the VOCABULARY an index binds to, and it exists because kgraph is
// not agnostic and pretending otherwise is how the wrong ladder gets applied to
// a corpus.
//
// The ladder below — record, admission, document, observation, statement,
// interested, inference — is a LEGAL ontology. It was written for a matter and
// it is right for one; it is not right for an engineering corpus where
// derivation is the majority case, and it is not right for a clinical one where
// "observation" outranks nearly everything. Baking it into the binary made every
// index a legal index by default and left no way to say otherwise.
//
// WHAT IS DIALECT AND WHAT IS PHYSICS — the split this file exists to hold:
//
//	dialect   the rung NAMES and their order, which class governs rather than
//	          testifies, which rungs must name a speaker, what the invalidation
//	          flag is called
//	physics   that an ordinal ladder resolves conflicts at all, that the governing
//	          class has NO rank so it can neither win nor lose, that `disputed` is
//	          computed and never authored, that `derived_from` makes a conclusion
//	          invalidate when a premise moves
//
// The mechanic is core. Only the word is dialect-scoped. A dialect that could
// change the mechanic would be a dialect that can silently corrupt conflict
// resolution, which is why the list is CLOSED IN THE BINARY and not
// user-pluggable: a hand-authored ladder with its rungs in the wrong order
// produces no error, just confidently wrong `disputed`.

// Dialect names a vocabulary and the rules that bind under it.
//
// The fields are unexported and reached through methods for the reason
// `classRank` was unexported: a caller that can mutate the ladder can change
// what wins a conflict under a graph that has already resolved one.
type Dialect struct {
	name string
	// rank is the ordinal ladder. Ordinal, not a probability — a hand-typed 0.7
	// is an invented statistic; this ranks and is defensible.
	rank map[string]int
	// governs is the out-of-ladder class: law, not evidence. It has no rank, and
	// that absence IS the mechanism — conflict and impeachment look up a rank and
	// skip what has none, so authority supports a claim without ever winning or
	// losing an evidentiary contest.
	governs string
	// speaker is the rung set whose members are assertions ABOUT someone's
	// interest, and mean nothing without naming who.
	speaker map[string]bool
	// rules is what this index does with each tunable check: error, warn or off.
	// Absent means the check keeps its natural level.
	rules map[string]RuleLevel
	// invalidated is what this dialect calls a source that can no longer be
	// relied on — impeached, retracted, revoked. The mechanic is core; the word
	// is not.
	invalidated string
	// edges are relation keys this dialect adds, mapped to how they read when
	// followed backwards. They become walkable edges carrying the same bag as any
	// other — valid_from, valid_until, while, because.
	//
	// THEY DRIVE NOTHING, AND THAT IS THE WHOLE LICENCE FOR THEM BEING OPEN.
	//
	// The core table is closed because every type in it participates in a
	// computation: `attests` in evidence, `contradicts` in `disputed`,
	// `supersedes` in which fact wins, `member_of` in group evaluation,
	// `derived_from` in staleness, `requires` in taint, `supports`/`causes`/
	// `implies` in premises, `about` in the anchor rule. A dialect that could
	// redefine one of those could change what a graph concludes, silently, which
	// is the same failure a hand-authored ladder is kept out of the binary for.
	//
	// A dialect edge is the opposite: it exists to be WALKED. It is how a
	// construction layer says "these things are waiting on this document" without
	// the core growing a relation it does not use — which is what a plain field
	// was doing instead, validated, hashed, and unwalkable.
	edges map[string]string
	// hash identifies an AUTHORED dialect's content, and is empty for a built-in.
	// It is what makes `dialect-changed` possible without false-flagging every
	// document a corpus rendered before dialects were data. See DialectHash.
	hash string
	// subtypes are the node types this dialect adds over the core kinds.
	//
	// A subtype does NOT create a kind. `element` is a `claim` — the core table
	// stays closed for the same reason the core EDGE table does — and the subtype
	// says which core kind it must be plus which keys it may carry. That is the
	// whole of it: the schematic layer sits on top of an arbitrary graph rather
	// than growing it.
	subtypes map[string]Subtype
}

// Subtype is a dialect's node type: the core kind it must be, and the keys it
// governs beyond the core scalar set.
//
// Keys are what makes prefixing necessary. Two subtypes on one node can both
// want `state`, and rather than pick a winner — precedence being the silent
// corruption this file refuses everywhere else — the author disambiguates by
// prefixing one of them, and an unresolved collision is an ERROR that names the
// fix.
type Subtype struct {
	kind Kind
	keys map[string]bool
}

// Kind is the core kind a node must be to carry this subtype.
func (s Subtype) Kind() Kind { return s.kind }

// Governs reports whether this subtype declares the key.
func (s Subtype) Governs(key string) bool { return s.keys[key] }

// Keys lists what the subtype declares, sorted.
func (s Subtype) Keys() []string {
	out := make([]string, 0, len(s.keys))
	for k := range s.keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Subtype resolves a node type this dialect adds.
func (d Dialect) Subtype(name string) (Subtype, bool) {
	s, ok := d.subtypes[name]
	return s, ok
}

// SubtypeNames lists the node types this dialect adds, sorted.
func (d Dialect) SubtypeNames() []string {
	out := make([]string, 0, len(d.subtypes))
	for k := range d.subtypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Name is the dialect's identifier, as an index declares it.
func (d Dialect) Name() string { return d.name }

// Governs is the class that governs rather than testifies.
func (d Dialect) Governs() string { return d.governs }

// Invalidated is what this dialect calls an invalidated source.
func (d Dialect) Invalidated() string { return d.invalidated }

// Rank reports a class's ordinal weight and whether it has one at all.
//
// ok is false for the governing class and for any class this dialect does not
// know — different reasons to have no rank, but the same instruction to a
// caller: skip it, do not rank it zero. A caller that substitutes zero
// reintroduces the category error the governing class exists to avoid.
func (d Dialect) Rank(class string) (int, bool) {
	r, ok := d.rank[class]
	return r, ok
}

// Ladder returns the ranked classes, strongest first. The governing class is
// absent by construction, not by omission.
func (d Dialect) Ladder() []string {
	out := make([]string, 0, len(d.rank))
	for c := range d.rank {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return d.rank[out[i]] > d.rank[out[j]] })
	return out
}

// Valid reports what may be authored: the ladder plus the governing class.
func (d Dialect) Valid(class string) bool {
	if class == d.governs {
		return true
	}
	_, ok := d.rank[class]
	return ok
}

// SpeakerRequired reports a class that means nothing without naming who spoke.
func (d Dialect) SpeakerRequired(class string) bool { return d.speaker[class] }

// Edge resolves a relation key this dialect adds.
//
// A CORE TYPE ALWAYS WINS and is never consulted here — see checkDialects, which
// refuses a dialect that names one at construction rather than letting the
// precedence be a thing somebody has to remember.
func (d Dialect) Edge(key string) (EdgeType, bool) {
	if _, ok := d.edges[key]; !ok {
		return "", false
	}
	return EdgeType(key), true
}

// EdgePhrase is how a dialect edge reads when followed backwards, for the same
// reason inversePhrase exists: "waits_on by" is not English and this text goes
// into a prompt.
func (d Dialect) EdgePhrase(t EdgeType) (string, bool) {
	p, ok := d.edges[string(t)]
	return p, ok
}

// EdgeKeys lists the relation keys this dialect adds, sorted.
func (d Dialect) EdgeKeys() []string {
	out := make([]string, 0, len(d.edges))
	for k := range d.edges {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// legal is the dialect this graph was written for, and the default for an index
// that declares none.
//
// DEFAULTING RATHER THAN REFUSING is deliberate and is the migration: every
// index in existence predates dialects and declares nothing, and an unmarked
// index that stopped loading would be a tool that broke every corpus to gain a
// feature none of them asked for yet.
var legal = Dialect{
	name: "legal",
	rank: map[string]int{
		"record":      6,
		"admission":   5, // against the speaker's own interest
		"document":    4,
		"observation": 3,
		"statement":   2,
		"interested":  1, // a party asserting what benefits them
		"inference":   0, // we concluded, we were not told
	},
	governs: ClassAuthority,
	// An admission is against the speaker's own interest; an interested source is
	// a party asserting what benefits them. Neither means anything without who.
	speaker:     map[string]bool{"admission": true, "interested": true},
	invalidated: "impeached",
	edges: map[string]string{
		// `serves` is what a document that does not exist yet does for the thing
		// waiting on it. Authored on the acquisition and pointing at what waits,
		// so it reads off the entry: this acquisition serves el-record-title.
		//
		// It promises NOTHING about the outcome, deliberately — the document may
		// arrive and settle nothing, which `enables` or `supports` would both
		// imply it will, and `supports` additionally collides with what actually
		// backs a claim.
		//
		// It exists because caselit had this relation as a plain field on its
		// construction entries, in two spellings (`turns_on:` for elements,
		// `answers:` for questions) split by what happened to be on the other
		// end — and unwalkable in both.
		"serves": "served by",
		// The rest of the construction's relations, landed 2026-08-29 when the
		// vocabulary moved here rather than into a file caselit vendored.
		//
		// WHY HERE AND NOT IN A VENDORED `caselit.legal`. This dialect IS the
		// construction layer's vocabulary — `serves` above says so in its own
		// comment, and `element`, `acquisition` and `attempt` were declared for
		// that consumer. Half a vocabulary in the binary and half in a file is a
		// worse drift than a copied ladder, and a corpus-wide file cannot extend a
		// built-in, only restate it. So the ladder stays uncopied and these live
		// beside the three that were already here.
		//
		// Each drives NOTHING, which is the licence for this set being open, and
		// each exists to be WALKED — every one of them was a plain field on a
		// construction entry, validated and unwalkable.

		// A theory targets the outcome it would win, and counters the opposing
		// theory. `counters` is not `contradicts`: two theories are arguments
		// about the same facts, not evidence pointing opposite ways, and routing
		// them through `contradicts` would make `disputed` fire on the pleadings.
		"targets":  "targeted by",
		"counters": "countered by",
		// An attempt pursues its acquisition, authored on the ATTEMPT so that
		// recording one appends a node and touches nothing else. Deliberately not
		// `serves`: one word for two different pairs is how `turns_on` came to
		// mean two things.
		"pursues": "pursued by",
		// A production produced the sources that came with it — "what came in
		// volume three" stops being a scan over every source in the corpus.
		"produced": "produced in",
		// An outcome names the claim recording the cause AS PLEADED — the entry in
		// the complaint, not our restatement of it. Without it an outcome restates
		// a cause by name and nothing connects the two, so nothing checks an
		// element list against what was actually filed.
		"pleaded_as": "pleaded by",
	},
	// Started at two, deliberately: an element and the acquisition that serves
	// it, enough to prove the mechanism before the migration had been tried.
	//
	// COMPLETED 2026-08-29. The rest of the construction is here — outcome,
	// theory, production, negative, and the keys the ledger needs — because this
	// dialect is what the construction layer speaks. A subtype does not create a
	// kind: an outcome is a `group`, a theory is a `claim`, a production is an
	// `event`. The core table is untouched, which is the whole point of a
	// schematic layer over an arbitrary graph.
	subtypes: map[string]Subtype{
		// An element is a CLAIM: a thing asserted, which must be made out. It
		// governs no keys of its own — what makes it an element is the threshold
		// it must clear, and that is read off this dialect's ladder rather than
		// authored per node.
		"element": {kind: KClaim},
		// An acquisition is a QUESTION whose answer is a document, and the ledger
		// is the reason subtypes carry keys at all: `where`, `who` and `cost` are
		// real data with nowhere to live on a core node.
		// `state` is DERIVED from the attempts under it and is authored only
		// alongside `state_because` — the log cannot know the clerk said it burned
		// in 1974, but it may not be overridden silently either.
		//
		// `arrived` is the document that landed, and is NOT spelled `doc:`: that
		// key is source-only and constructs a Source on whatever carries it, so an
		// acquisition written with it would come out source-shaped. Same trap as
		// `by:` on an attempt, below.
		//
		// `raised_file`/`raised_sentence` are the prose that raised the entry. A
		// sentence is not a graph node — it lives in a markdown story file — so
		// this is a pair of keys and not a relation, and it is a RAISED-BY rather
		// than a support: asking for a document grounds nothing.
		"acquisition": {kind: KQuestion, keys: map[string]bool{
			"where": true, "who": true, "cost": true,
			"state": true, "state_because": true, "arrived": true,
			"raised_file": true, "raised_sentence": true,
		}},
		// An outcome is a GROUP, which is what it has always been: satisfied when
		// its elements are, and `all` is the core group expression that says so.
		// `party` is whose outcome it is — the side reads off it.
		//
		// `deadline`/`deadline_why` are a DATE THAT RUNS OUT against the cause,
		// and they are here rather than in the consumer because the fact format
		// is the asset: a limitation recorded in a file beside the tool is a
		// limitation nothing can check, and two formats for one date is the drift
		// this dialect exists to prevent.
		//
		// AUTHORED, and deliberately the exception. Backing, filing and clearance
		// are all derived because an authored answer outlives what made it true;
		// a statutory deadline is the opposite — no amount of reading a corpus
		// reveals it, and nothing in the graph can compute one. What is derived is
		// whether it has RUN, which the consumer answers against its own clock on
		// every read, so an order extending a date cannot leave a stored "expired"
		// saying the opposite forever.
		//
		// `deadline_why` names what SETS the date — a statute, an order, a
		// stipulation. It is not required here and cannot be: a subtype declares
		// which keys may appear and has no way to insist on one. The consumer
		// makes it an audit ERROR, for the same reason a theory's `counters` is
		// checked there — and because the date is the urgent half, so losing it
		// to a validation error would be worse than holding it unsourced.
		//
		// ONLY ON AN OUTCOME, for now. A cause of action is the thing limitations
		// run against. A court rule running against a filing, or a stipulation
		// against a production, are real and are not this: each would need its own
		// subtype key, and adding them speculatively would declare a vocabulary
		// nobody has used.
		"outcome": {kind: KGroup, keys: map[string]bool{
			"party": true, "deadline": true, "deadline_why": true,
		}},
		// A theory is a CLAIM owned by a party. Claims serve theories and inherit
		// the side, so a two-edged fact never has to pick one or be entered twice.
		//
		// A theory's `counters` is REQUIRED by the consumer, and cannot be
		// required here: a subtype declares which keys may appear and has no way
		// to insist on a relation. That check stays where it can be made — an
		// audit ERROR, because an absent counter means the adversarial pass has
		// not been run over that position.
		"theory": {kind: KClaim, keys: map[string]bool{"party": true}},
		// A production is an EVENT: what changed hands, when, from whom, and under
		// what designation. The date is core `at:`. Whether any of it is safe to
		// FILE is derived from these and is refused as an authored field, for the
		// same reason `disputed` is: an authored answer survives the clawback
		// notice that made it false.
		"production": {kind: KEvent, keys: map[string]bool{
			"party": true, "volume": true, "bates": true,
			"privilege": true, "confidentiality": true, "clawed_back": true,
		}},
		// A negative is an asserted ABSENCE — the accusation, answered. It governs
		// no keys because its answer is derived from the acquisitions covering the
		// places evidence would live if it were true, and a derived answer with an
		// authored field beside it is the failure this whole layer is built to
		// avoid.
		"negative": {kind: KClaim},
		// An attempt is an EVENT: somebody tried, on a date, and something came
		// back. It was a nested list under the acquisition, which meant the log
		// could only be read by walking to that one entry — "what have we tried
		// this month, across every acquisition" was not a question the graph could
		// answer.
		//
		// `actor` and not `by`: `by:` is source-only and constructs a Source on
		// whatever node carries it, so an attempt written with `by:` would come out
		// source-shaped. The init guard refuses a subtype governing it for the same
		// reason, and that is the guard working rather than an obstacle.
		// `note` carries what a closed set cannot: a request number, which clerk,
		// what the objection said. "Subpoena, nothing" with no reference is not
		// something anybody can follow up or testify from.
		"attempt": {kind: KEvent, keys: map[string]bool{
			"actor": true, "how": true, "found": true, "note": true,
		}},
	},
}

// engineering is the vocabulary of a codebase: what was measured, what was
// observed, what somebody concluded, and — separately — what was DECIDED.
//
// It exists because the legal ladder is wrong here in a specific way. `record`,
// `admission`, `document`, `interested` are the categories of a matter, where
// the question is who said a thing and against whose interest. In a codebase
// the question is how a thing was found out, and the strongest answer is a
// number somebody measured.
//
// DECIDED GOVERNS RATHER THAN RANKING, and that is the load-bearing choice. A
// user's preference — "the menu should be a hamburger with a drawer" — is not a
// claim about the world that better evidence could overturn. It is a rule, in
// the same sense `authority` is a rule in the legal dialect, and it belongs off
// the ladder for the same mechanical reason: conflict looks up a rank and skips
// what has none, so a decision settles a question without ever having to beat a
// measurement in an evidentiary contest. Putting it on the ladder would let a
// benchmark outrank a preference, which is backwards — the benchmark is about
// the world and the preference is about what is wanted.
//
// The ladder underneath is about WARRANT, not about artefacts:
//
//   - `measured` outranks `observed` because a number carries its method;
//     "it repaints every second" and "472 B/s over 90 seconds" are not the same
//     kind of knowing, and the second is the one that survives an argument.
//   - `signed-off` outranks `accepted` because the two are a real distinction and
//     not a formality: accepted means the party that set the criteria believes
//     they were met, signed-off means the person the work is for agrees. A graph
//     that flattened them would lose the whole point of having a reviewer.
//   - `reported` is a claim nobody has checked yet, which is exactly where a task
//     session's hand-back starts.
//   - `inference` is the floor, as it is in legal: we concluded, we were not told.
//
// SPEAKER REQUIRED ON THE THREE REVIEW RUNGS. "Accepted" means nothing without
// by whom — an orchestrator accepting its own work is the failure a review layer
// exists to prevent, and a graph that could not name the reviewer could not tell
// that case from a real one.
//
// `stale` RATHER THAN `impeached`. What happens to an engineering finding is not
// that somebody discredited it; it is that the code moved and nobody noticed.
// The mechanic is identical — a source that can no longer be relied on — and the
// word has to say which failure it was, because they call for different
// responses: an impeached source is argued about, a stale one is re-measured.
//
// ONE SUBTYPE, deliberately, for the reason `legal` started at two: a subtype's
// keys are a guess until a corpus has been written under them. `measurement` is
// the case where the guess is safe, because a number with no method and no date
// is not a measurement in any trade.
//
// KNOWN GAP, and it is the one this dialect most wants: `inference` is rank 0
// with no notion of DEPTH, so a conclusion drawn from a measurement and one
// drawn from three chained conclusions weigh the same. The plan already names
// this as tolerable in legal, where the chain is argued in front of someone, and
// not in a corpus where derivation is the majority case. In engineering it IS
// the majority case. Until the depth rule lands, this dialect ranks derived
// knowledge more generously than it deserves.
var engineering = Dialect{
	name: "engineering",
	rank: map[string]int{
		"measured":   5,
		"observed":   4,
		"signed-off": 3,
		"accepted":   2,
		"reported":   1,
		"inference":  0,
	},
	governs:     "decided",
	speaker:     map[string]bool{"reported": true, "accepted": true, "signed-off": true},
	invalidated: "stale",
	subtypes: map[string]Subtype{
		// A measurement is a CLAIM whose warrant is its method. `how` and `when`
		// are the two things that make a number re-checkable, and neither has
		// anywhere to live on a core node — which is the same argument that gave
		// `acquisition` its ledger keys.
		// NOT `value`: a claim already carries one, and the init guard refuses a
		// subtype governing a core scalar. That is the guard working rather than
		// an obstacle, and this comment's own next sentence conceded it — `how`
		// and `when` are the two that have nowhere to live, which is another way
		// of saying `value` already does. A subtype that shadowed it would be
		// ambiguous on any node that did not prefix the key, and whether the
		// author remembered is not a thing the format may depend on.
		"measurement": {kind: KClaim, keys: map[string]bool{
			"how": true, "when": true,
		}},
	},
}

// dialects are the DEFAULTS this binary ships. A corpus authors its own in
// `.kgraph/dialects/*.yaml` and a project in its own — see dialectfile.go, which
// holds what replaced the closed map and why. An entry here is a reviewed
// addition; the guard below runs over both, so a built-in gets no laxer a check
// than a file.
var dialects = map[string]Dialect{
	legal.name:       legal,
	engineering.name: engineering,
}

// A DIALECT MAY NOT REDEFINE A CORE RELATION. Checked at init rather than left to
// precedence, because "the core wins" is a rule somebody has to know, and a
// dialect that quietly failed to take effect would be worse than one refused.
//
// Panicking is right here and nowhere else: the dialect list is closed in the
// binary, so this can only fire for a dialect somebody is adding, at the moment
// they add it, never in front of a user.
func init() {
	for name, d := range dialects {
		if err := validateDialect(d); err != nil {
			panic("built-in " + name + ": " + err.Error())
		}
	}
}

// DialectFor resolves an index's declared dialect.
//
// An empty name is the LEGAL default, not an error, for the reason on `legal`.
// An unknown name IS an error: it is a typo or an index built for a kgraph that
// knows something this one does not, and guessing which would apply the wrong
// ladder to somebody's corpus — the precise failure this type exists to prevent.
func DialectFor(name string) (Dialect, bool) {
	if name == "" {
		return legal, true
	}
	d, ok := dialects[name]
	return d, ok
}

// DialectNames lists what this binary knows, sorted. For `kg indexes` and for
// the error message when a declared dialect is unknown — a refusal that does not
// say what WOULD have been accepted makes somebody go and read the source.
func DialectNames() []string {
	out := make([]string, 0, len(dialects))
	for n := range dialects {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// LevelOf is what this dialect does with a check. Unset and unknown both mean
// `warn`, which is every check's natural level and what a corpus declaring no
// rules has always had.
func (d Dialect) LevelOf(check string) RuleLevel {
	if l, ok := d.rules[check]; ok {
		return l
	}
	return RuleWarn
}

// ApplyRules re-levels diagnostics according to the index's declared rules.
//
// AN ERROR IS NEVER TOUCHED. A rule may raise a warning to an error or silence
// it, and may never lower an error: a dangling reference gets fixed, and letting
// an index declare its way out of one would make the severity that must be
// trusted negotiable. Same line the findings ledger draws for acceptance, and
// for the same reason — the two are the class-level and per-finding halves of
// one mechanism.
func (d Dialect) ApplyRules(ds []Diag) []Diag {
	if len(d.rules) == 0 {
		return ds
	}
	out := ds[:0:0]
	for _, x := range ds {
		if x.Severity == SevError || x.Check == "" {
			out = append(out, x)
			continue
		}
		switch d.LevelOf(x.Check) {
		case RuleOff:
			continue
		case RuleError:
			x.Severity = SevError
		}
		out = append(out, x)
	}
	return out
}

// withRules layers an index's own rule levels over the dialect's.
//
// A COPY, never a mutation: a built-in dialect is a package-level value shared
// by every index that binds it, and writing one index's policy into it would
// silence a check corpus-wide. The same reason the ladder is unexported.
func (d Dialect) withRules(over map[string]RuleLevel) Dialect {
	if len(over) == 0 {
		return d
	}
	merged := make(map[string]RuleLevel, len(d.rules)+len(over))
	for k, v := range d.rules {
		merged[k] = v
	}
	for k, v := range over {
		merged[k] = v
	}
	d.rules = merged
	return d
}
