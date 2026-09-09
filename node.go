package kgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Kind is a node kind. Group operators and temporal words are NOT kinds —
// groups carry an aggregate expression, temporal words are qualifiers.
type Kind string

const (
	KClaim    Kind = "claim"
	KQuestion Kind = "question"
	KGroup    Kind = "group"
	KEntity   Kind = "entity"
	KEvent    Kind = "event"
	KAction   Kind = "action"
	KSource   Kind = "source"
)

// bodyKeys maps the authored key that declares a node's kind to that kind.
// Exactly one must be present on a node.
var bodyKeys = map[string]Kind{
	"claim":    KClaim,
	"question": KQuestion,
	"group":    KGroup,
	"entity":   KEntity,
	"event":    KEvent,
	"action":   KAction,
	// source forms — all produce KSource, and set Source.Form
	"document":    KSource,
	"utterance":   KSource,
	"record":      KSource,
	"observation": KSource,
	"inference":   KSource,
}

// Status is the epistemic state. `disputed` is absent on purpose: it is
// computed from conflicting evidence, never authored.
type Status string

const (
	SAsserted  Status = "asserted"
	SProposed  Status = "proposed"
	SOpen      Status = "open"
	SResolved  Status = "resolved" // questions
	SDone      Status = "done"     // actions
	SWithdrawn Status = "withdrawn"
	SFalse     Status = "false"
)

var validStatus = map[Status]bool{
	SAsserted: true, SProposed: true, SOpen: true,
	SResolved: true, SDone: true, SWithdrawn: true, SFalse: true,
}

// Needs is the kind of thing that would close a question. Four kinds, each
// implying a different next action:
//
//	evidence  a document exists; obtain and read it
//	decision  someone must choose; no amount of evidence settles it
//	reply     an external party must respond; the ball is not ours
//	analysis  reasoning over facts and law, not a document to fetch
//
// A question with no `needs` is a wish. It sits open because nobody knows what
// closing it would even look like.
type Needs string

const (
	NeedsEvidence Needs = "evidence"
	NeedsDecision Needs = "decision"
	NeedsReply    Needs = "reply"
	NeedsAnalysis Needs = "analysis"
)

var validNeeds = map[Needs]bool{
	NeedsEvidence: true, NeedsDecision: true, NeedsReply: true, NeedsAnalysis: true,
}

// Precision guards against rendering "2026-07" as a specific day.
type Precision string

const (
	PDay    Precision = "day"
	PMonth  Precision = "month"
	PYear   Precision = "year"
	PApprox Precision = "approx"
)

var validPrecision = map[Precision]bool{PDay: true, PMonth: true, PYear: true, PApprox: true}

// EdgeType is one of the twelve binary relations. Nothing else is an edge.
type EdgeType string

const (
	ECauses      EdgeType = "causes"
	EImplies     EdgeType = "implies"
	ERequires    EdgeType = "requires"
	EProhibits   EdgeType = "prohibits"
	ESupports    EdgeType = "supports"
	EContradicts EdgeType = "contradicts"
	EAnswers     EdgeType = "answers"
	ESupersedes  EdgeType = "supersedes"
	EMemberOf    EdgeType = "member_of"
	EAbout       EdgeType = "about"
	EAttests     EdgeType = "attests"
	// EDerivedFrom points from an `inference` source to each premise it was
	// concluded from. It was a plain field for a long time — validated, hashed,
	// and unwalkable — which meant the one question worth asking of a conclusion
	// ("what does this rest on, and is any of it weak?") could not be asked in
	// the query language at all. A conclusion's premises ARE a binary relation;
	// modelling them as one lets `-derived_from->` find the soft ground.
	EDerivedFrom EdgeType = "derived_from"
)

// forwardEdge: authored on the source node, points at the destination.
var forwardEdge = map[string]EdgeType{
	"causes":       ECauses,
	"implies":      EImplies,
	"requires":     ERequires,
	"prohibits":    EProhibits,
	"supports":     ESupports,
	"contradicts":  EContradicts,
	"answers":      EAnswers,
	"supersedes":   ESupersedes,
	"member_of":    EMemberOf,
	"about":        EAbout,
	"attests":      EAttests,
	"derived_from": EDerivedFrom,
}

// inverseEdge: authored on the DESTINATION node. The key names the inverse, and
// the edge is stored in canonical direction so one relation has one sem_hash.
var inverseEdge = map[string]EdgeType{
	"attested_by": EAttests,     // source attests this node
	"answered_by": EAnswers,     // that node answers this question
	"undercut_by": EContradicts, // that source contradicts this node — one-way, see Edge.OneWay
	"members":     EMemberOf,    // those nodes are members of this group
	"options":     EAnswers,     // declared cases of this question
}

// oneWayEdge names the keys whose contradiction defeats rather than conflicts.
// See Edge.OneWay.
var oneWayEdge = map[string]bool{"undercut_by": true}

// Node is a fact, question, group, entity, event, action, or source.
type Node struct {
	ID     string
	CID    string // content-addressed fallback; absorbs slug churn
	Kind   Kind
	Body   string
	Status Status
	Owner  string // entity id — responsible, not topical

	ValidFrom, ValidUntil string // truth window of the CLAIM
	At                    string // occurrence: a point in time
	OccFrom, OccTo        string // occurrence: a span
	AtExpr                string // "= e-eob.at + 180d" — derived, moves with its basis
	Precision             Precision

	While string // holds only in the branch where this node is satisfied

	Value     float64
	HasValue  bool
	ValueExpr string // "= bill-total.value - g-fair-value.value"
	Unit      string

	GroupExpr string // "all" | "any" | "none" | ">= 2" | "== 1" | "sum(value)"
	// Title names a GROUP, and only a group.
	//
	// Every other kind's body IS its text: a claim is what it claims, a question
	// is what it asks. A group is a SET plus a RULE over it and has no natural
	// text, which is why `group:` — alone among the eleven body keys — takes the
	// rule as its value instead of a body.
	//
	// So a group had nowhere to say what it IS, and six of the nine authored
	// groups in this workspace put prose in `group:` instead. That does not
	// merely evaluate as never-satisfied; it reads as `group: <a label>`, which
	// is membership by TAG — and a kgraph group is an explicit set with
	// `member_of` edges, never a label match. The wrong mental model is the worse
	// half of that bug.
	//
	// Group-only on purpose. A general `title:` invites every kind to grow a
	// field that duplicates its own body.
	Title   string
	Ordered bool // member order is a sequence, not a set

	Options    []string // declared cases of a question
	Exhaustive bool

	// Underdetermined declares that this is NOT ONE FACT: the sources that appear
	// to conflict are describing different things, so weighing them is the error
	// and the remedy is to split the claim.
	//
	// Authored, unlike `disputed`, and the asymmetry is the point. `disputed` is
	// computed because it IS derivable — evidence pointing both ways. Whether a
	// claim is ambiguous is a statement about what the claim MEANS, which no
	// amount of evidence reveals. Same reasoning as `needs`.
	//
	// The value is the reason, and it is required: "underdetermined: true" is a
	// shrug, while "the EOB bills the professional balance and the statement may
	// add a facility line" is the split waiting to be made.
	Underdetermined string

	// SameAs retires this id in favour of another node: the same fact was entered
	// twice and this is the copy being folded away. References to this id keep
	// resolving, which is the point — deleting the loser outright breaks them.
	SameAs string
	// Needs declares what would ANSWER a question. Not derivable — whether an
	// answer comes from a document or from someone deciding is knowledge about
	// the question — and without it "what can I act on" is unanswerable.
	Needs Needs

	// Aliases are the names a thing is KNOWN BY, each optionally scoped in time.
	// This is denotation, not identity churn: the same identifier can denote two
	// different things in two different periods, and reading one as the other is
	// how a filed document ends up wrong.
	Aliases []Alias

	// Is are the dialect subtypes this node carries, as authored — either a bare
	// name or `prefix:subtype`. Multiple are allowed because one thing can be two
	// things, and the prefix is how their keys are kept apart.
	Is []string
	// Bag holds subtype-governed keys, under the name they were authored with:
	// bare when the subtype was unprefixed, `prefix:key` when it was not. The
	// authored form is kept rather than resolved so a diff shows what a person
	// typed, and so two prefixings of one subtype cannot collide.
	//
	// Core scalar fields never land here — a subtype governing one is refused at
	// dialect construction, so the two sets cannot overlap.
	Bag map[string]string

	Reason           string
	SupersedesReason string

	Source *Source // non-nil iff Kind == KSource

	File string
	Line int
}

// Alias binds a name to a node over an interval. `P74736` denoted the pre-2021
// parent parcel and now denotes Lot I; an undated reference to it is ambiguous
// and the scan says so.
type Alias struct {
	As    string
	From  string
	Until string
}

// Holds reports whether the binding is in force at a point in time. An undated
// query sees every binding, which is what makes ambiguity detectable.
func (a Alias) Holds(at string) bool {
	if at == "" {
		return true
	}
	if a.From != "" && compare(at, a.From) < 0 {
		return false
	}
	if a.Until != "" && compare(at, a.Until) >= 0 {
		return false
	}
	return true
}

// Source is provenance as a node: queryable, weighted, and invalidated when the
// underlying document moves.
type Source struct {
	Form     string // document|utterance|record|observation|inference
	Class    string // ordinal evidentiary weight — see ClassRank
	Speaker  string // entity id
	Medium   string
	Recorded bool
	DocPath  string
	DocHash  string
	Anchor   string
	Premises []string // inference only: derived_from
}

// ClassAuthority is law, not evidence, and is deliberately OUTSIDE the ordinal
// ladder below.
//
// A statute or an opinion does not testify to a fact — it governs. Ranking it
// alongside witnesses means it can "impeach" one, which is a category error: a
// statute never refutes testimony, it decides what the testimony is worth. It was
// worse than theoretical here, where RCW 48.49.080 was classed `record` and
// therefore sat at the TOP of the evidentiary ladder, outranking every witness in
// the corpus.
//
// Having no rank is the whole mechanism: `conflict` and `impeachment` look up a
// rank and skip what has none, so authority supports a claim without ever winning
// or losing an evidentiary contest. ClassRank reports it unranked for that reason,
// and a caller that substitutes zero for "no rank" reintroduces the category error.
const ClassAuthority = "authority"

// The package-level accessors below are the LEGAL dialect, and they are the
// migration rather than a redesign.
//
// Every one of them used to own its data. They now delegate to `legal` in
// dialect.go, so there is one ladder in the binary instead of one here and a
// copy there — and callers that have no index in hand keep working unchanged
// while the value is threaded to the ones that do. caselit consumes ClassRank
// and ClassLadder and derives its `backed` floor from `document`'s rank; both
// were deliberately shaped as FUNCTIONS rather than a table so this substitution
// would not be a breaking change, and it is not.

// ClassRank reports a source class's ordinal evidentiary weight under the LEGAL
// dialect, and whether the class has one at all. ok is false for ClassAuthority
// and for any class the dialect does not know, which are different reasons to
// have no rank but the same instruction to a caller: skip it, do not rank it
// zero.
//
// It exists because a consumer that derives anything from evidentiary weight —
// caselit derives whether a legal element is backed or thin — otherwise copies
// this table by hand, and a copy that goes stale against a reweight silently
// promotes weak support to strong. That is the one answer such a tool exists to
// give.
//
// INDEX-SCOPED IS Dialect.Rank. This answers for `legal` with no index in hand,
// which is right for a caller that has none and wrong for one that does.
func ClassRank(class string) (int, bool) { return legal.Rank(class) }

// ClassLadder returns the LEGAL dialect's ranked classes, strongest first.
// ClassAuthority is absent by construction, not by omission.
//
// A consumer needs the set, not just a lookup: caselit's floor for "backed" is a
// named rung, and a class kgraph ADDS is one caselit will rank without ever
// having decided which side of that floor it belongs on. Enumerating is what
// lets a test there notice.
func ClassLadder() []string { return legal.Ladder() }

// validClass is what may be authored under the legal dialect.
func validClass(c string) bool { return legal.Valid(c) }

// speakerRequired marks the classes that are assertions ABOUT someone's
// interest.
func speakerRequired(c string) bool { return legal.SpeakerRequired(c) }

// Edge is binary and directed. contradicts is symmetric. An edge carries its own
// temporal qualifiers: a requirement that lapses on a date must stop being
// traversable, or a closure walks a path that no longer exists.
type Edge struct {
	// ID is the OPTIONAL authored name of this relation, and CID is its content
	// address — exactly the pair a node carries, for exactly the same reason.
	//
	// AN EDGE WITHOUT AN IDENTITY CANNOT BE SPOKEN ABOUT, and that was the limit.
	// "The other side disputes that this document attests that element" is a
	// statement whose subject is a RELATION, and there was nowhere to point it:
	// an edge was reachable only by walking from one of its endpoints, so
	// annotation could only ever be inline and only from the closed set below.
	//
	// CID is derived from `(Src, Type, Dst)` and NOTHING ELSE — deliberately, and
	// the same way `CID(kind, body)` excludes the attesting document. Two authors
	// who record the same relation with different `Because` are describing ONE
	// relation twice, and folding the annotation into the identity would give
	// them different addresses, defeating the merge this exists to find.
	ID  string
	CID string

	Src, Dst   string
	Type       EdgeType
	ValidFrom  string
	ValidUntil string
	While      string
	File       string
	Line       int

	// Bag holds annotation outside the typed fields — `by:` is the first, and the
	// reason the map exists rather than another field.
	//
	// The typed fields above are NOT bag entries and never move into it: every
	// one of them drives a computation (`While` gates on a branch, `ValidFrom`
	// bounds a truth window, `OneWay` decides direction), and a computation
	// reading an untyped map is how a typo becomes silently false rather than
	// refused. The bag is for what the graph carries and does not act on.
	Bag map[string]string

	// Because is the per-MEMBERSHIP justification, distinct from the group's own
	// `reason:`. A group says what membership MEANS — one criterion, shared by
	// every member. Because says how THIS item meets it, and those are different
	// sentences: two sources can sit in `g-set-aside` for unrelated reasons, and
	// a group reason cannot carry either. Authored on the mapping form of any
	// relation key, alongside `valid_from`/`valid_until`/`while`.
	//
	// Named `because` and not `why` deliberately. A node's `reason:` already
	// renders into a prompt as `why:`, so two different things would have worn one
	// word in the same output — the node's context and the membership's
	// justification. The vocabulary is small on purpose and a collision in it
	// costs more than a longer key.
	//
	// It renders with the relation and is inside SemHash, so revising a
	// justification flags the documents that render the fact — which is the point.
	// An unexplained exclusion is the failure mode this field exists to prevent.
	Because string

	// OneWay marks a contradiction that DEFEATS rather than one that is mutually
	// incompatible. `contradicts:` is symmetric — two claims that cannot both be
	// true each put the other in question, and reading that edge from either end
	// is correct. `undercut_by:` is not: "B undercuts A" says B beats A, and A
	// does not thereby put B in question.
	//
	// They lowered to the same edge type, so the direction was lost, and
	// `conflict` counted the undercutter as contradicted by the thing it
	// undercut. In the fence-dispute corpus that made `sj-denied-septic-negligent` — a
	// summary-judgment denial read off the court's own order, the strongest fact
	// in the matter — render `DISPUTED: 1 for (record), 1 against (inference)`,
	// off one `undercut_by` authored on a defence theory the order defeats. House
	// rule 10 makes a status travel with the claim, so two documents going to
	// counsel hedged a court order.
	//
	// It never bit for a SOURCE undercutting a claim, which is what `undercut_by`
	// was designed for, because `conflict` returns early on a source. The defect
	// only appears when the undercutter is a claim.
	OneWay bool
}

// Doc is one parsed *.kfacts.md file.
type Doc struct {
	Path  string
	Nodes []Node
	Edges []Edge
}

// SemHash covers everything a rendered fact commits to, plus a digest of the
// node's incident edges — an edge change around a pinned node must flag the
// documents that consume it, because the rendering includes relations.
//
// It deliberately EXCLUDES provenance metadata and file position: otherwise a
// re-ingest or a line shift false-flags every document.
func SemHash(n Node, incident []Edge) string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	w(string(n.Kind), n.Body, string(n.Status), n.Owner)
	w(n.ValidFrom, n.ValidUntil, n.At, n.OccFrom, n.OccTo, n.AtExpr, string(n.Precision))
	w(n.While, n.GroupExpr, n.ValueExpr, n.Unit)
	if n.HasValue {
		w(fmt.Sprintf("%g", n.Value))
	} else {
		w("")
	}
	w(fmt.Sprintf("%t|%t", n.Ordered, n.Exhaustive), string(n.Needs), n.Underdetermined)
	// An alias change alters what a document's references denote, so it is part
	// of the staleness contract.
	als := make([]string, 0, len(n.Aliases))
	for _, a := range n.Aliases {
		als = append(als, a.As+"\x00"+a.From+"\x00"+a.Until)
	}
	sort.Strings(als)
	for _, a := range als {
		w(a)
	}

	// A source's AUTHORED provenance is rendered into the document, so it is part
	// of the staleness contract — and none of it was covered.
	//
	// `class` decides the `[[id|class]]` token in the prompt AND the ordinal rank
	// that resolves a conflict, so reclassifying `record` -> `hearsay` can flip
	// which claim comes out `disputed`. `doc:` is printed verbatim in the citation
	// line, so a reorganised corpus leaves every document citing a path that no
	// longer exists. Both reported `fresh`.
	//
	// This does not contradict TestSourceDriftIsNotInSemHash. That invariant is
	// about the document's BYTES: re-exporting a PDF must not flag every fact it
	// attests, and it still doesn't — drift stays a separate signal. Every field
	// below is parsed from the fact text and changes only when someone edits it,
	// so no re-ingest can move them. Authored, and rendered: hashed.
	//
	// DocHash is deliberately absent. It is the byte-state, which is drift's job.
	if s := n.Source; s != nil {
		w(s.Form, s.Class, s.Speaker, s.Medium, s.DocPath, s.Anchor)
		w(fmt.Sprintf("%t", s.Recorded))
		prem := append([]string(nil), s.Premises...)
		sort.Strings(prem) // premise order is not meaning, same as edges
		for _, p := range prem {
			w(p)
		}
	}

	digests := make([]string, 0, len(incident))
	for _, e := range incident {
		d := strings.Join([]string{
			string(e.Type), e.Src, e.Dst, e.ValidFrom, e.ValidUntil, e.While}, "\x00")
		// Appended only when set, so every hash in every corpus without a
		// one-way edge is byte-identical to what it was before this field
		// existed. The nodes that DO move are exactly the ones whose disputed
		// status this changes, which is what staleness is for: switching a
		// relation between `contradicts:` and `undercut_by:` is an authored
		// change to what the graph asserts, and it has to flag the documents.
		if e.OneWay {
			d += "\x001way"
		}
		// Same append-only discipline as OneWay above, and for the same reason: a
		// corpus with no `why:` hashes exactly as it did before the field existed.
		// The nodes that move are the ones whose justification was authored or
		// revised — and a changed justification MUST flag the documents, because a
		// membership whose stated reason has quietly changed is the failure this
		// field exists to prevent.
		if e.Because != "" {
			d += "\x00because=" + e.Because
		}
		digests = append(digests, d)
	}
	sort.Strings(digests) // edge order must not affect the hash
	for _, d := range digests {
		w(d)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// CID is the content address of the fact itself: its kind and its normalized
// body, and nothing else.
//
// It deliberately does NOT include the attesting document, which the first
// version did. Provenance is an edge now, so the same claim reached from two
// sources is ONE fact with two `attested_by` edges — and folding the document
// into the identity would give those two different CIDs, defeating exactly the
// merge this exists to find.
func CID(kind Kind, body string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + NormalizeBody(body)))
	return hex.EncodeToString(sum[:])[:16]
}

// EdgeCID is the content address of a RELATION: its two endpoints and its type.
//
// Endpoint ids are taken verbatim rather than normalized, because they are
// identifiers and not prose — `NormalizeBody` folds case and punctuation, which
// is right for a sentence and wrong for `el-drawing-defect`.
//
// Annotation is excluded for the reason given on Edge.CID: the same relation
// recorded twice with different justifications is one relation, and two
// addresses for it would defeat the merge.
func EdgeCID(src string, typ EdgeType, dst string) string {
	sum := sha256.Sum256([]byte(src + "\x00" + string(typ) + "\x00" + dst))
	return hex.EncodeToString(sum[:])[:16]
}

// NormalizeBody folds the differences that do not change what a fact says:
// case, whitespace, and trailing punctuation. It stops there — dropping words
// would start merging facts that differ in meaning.
func NormalizeBody(body string) string {
	f := strings.Fields(strings.ToLower(body))
	for i, w := range f {
		f[i] = strings.Trim(w, ".,;:!?\"'()[]")
	}
	return strings.Join(f, " ")
}

// bodyTokens is the token set used for near-duplicate scoring.
func bodyTokens(body string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(NormalizeBody(body)) {
		if len(w) > 1 {
			out[w] = true
		}
	}
	return out
}

// Similarity scores two pieces of prose for being the same thing said twice.
//
// Set overlap over the union of their content tokens, after NormalizeBody folds
// case and punctuation: 1 for identical wording, 0 for nothing in common. Word
// ORDER is deliberately ignored — "when did it start" and "it started when"
// are one question — and single-character tokens are dropped.
//
// IT IS A SCORE AND NOT A VERDICT. Compare it against NearDuplicate rather than
// a literal, so that retuning the threshold reaches every caller. See the note
// there for why this is exported at all.
func Similarity(a, b string) float64 { return jaccard(bodyTokens(a), bodyTokens(b)) }

// jaccard is set overlap over union: 1 for identical token sets, 0 for disjoint.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var inter int
	for w := range a {
		if b[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}
