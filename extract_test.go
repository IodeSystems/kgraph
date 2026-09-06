package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extractProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("documents/deed.md", "Deed AF 201503110044: Halloway conveys Lot I.\n")
	w("documents/surveyor.md", "The barn sits on Lot G. The strip is drawn within Lot I.\n")
	w("documents/scan.jpg", "\xff\xd8\xff\xe0not text at all\x00\x01")
	w("report.md", "generated output, not evidence\n")
	writeLegacyFacts(t, root, "facts.kfacts.md", "# f\n\n## Facts\n\n```kfacts\n"+
		"- id: carl\n  entity: Carl Taylor\n  status: asserted\n"+
		"- id: strip\n  entity: The disputed strip\n  status: asserted\n"+
		"- id: retained\n  entity: The retained parcel\n  status: asserted\n"+
		"- id: s-deed\n  record: Statutory warranty deed\n  doc: documents/deed.md\n"+
		"  at: 2008-07-08\n  class: record\n"+
		"- id: held-by-halloway\n  claim: The strip is held by Halloway\n  status: asserted\n"+
		"  about: strip\n  attested_by: s-deed\n"+
		"- id: q-drawing\n  question: Does the survey draw the strip on Lot I?\n  status: open\n"+
		"  needs: evidence\n  about: strip\n"+
		"- id: q-barn\n  question: Which lot is the barn on?\n  status: open\n"+
		"  needs: evidence\n  about: retained\n"+
		"- id: q-floor\n  question: What is the settlement floor?\n  status: open\n"+
		"  needs: decision\n  owner: carl\n"+
		"```\n")
	w("r.kgraph.md", "# r\n\n## Purpose\np\n\n## Query\n```kgraph\nc: claim\n```\n\n"+
		"## Outputs\n- report.md\n\n## Prompt\n{{c}}\n")
	return root
}

// The valuable half of the backlog. A document no source cites is invisible to
// every other command — the graph cannot report a gap it has no node for — and in
// a hand-assembled corpus that is where the unread evidence actually is.
func TestBacklogFindsUncitedDocuments(t *testing.T) {
	root := extractProject(t)
	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for _, d := range docs {
		state[d.Doc] = d.State
	}
	if state["documents/surveyor.md"] != "unread" {
		t.Errorf("an uncited document must be unread, got %q", state["documents/surveyor.md"])
	}
	if state["documents/scan.jpg"] != "unread" {
		t.Errorf("a binary exhibit is still evidence, got %q", state["documents/scan.jpg"])
	}
	// A document with one fact read from it usually had more in it.
	if state["documents/deed.md"] != "thin" {
		t.Errorf("one fact from a deed should read as thin, got %q", state["documents/deed.md"])
	}
	// The generated-output exclusion retired 2026-08-30: nothing is generated, so
	// there is nothing kgraph wrote to keep out of its own backlog. `report.md`
	// in this fixture is now an ordinary unread file, which is the honest answer —
	// it is a markdown document nobody has read anything from.
	// Unread sorts first, because that is the work.
	if docs[0].State != "unread" {
		t.Errorf("backlog is not ordered by attention, first is %q", docs[0].State)
	}
}

// A cited document that is not on disk is still backlog: whatever was read from
// it cannot be checked.
func TestBacklogReportsCitedButAbsentDocuments(t *testing.T) {
	root := extractProject(t)
	if err := os.Remove(filepath.Join(root, "documents/deed.md")); err != nil {
		t.Fatal(err)
	}
	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.Doc == "documents/deed.md" {
			if d.State != "missing" {
				t.Errorf("want missing, got %q", d.State)
			}
			if d.Source != "s-deed" {
				t.Errorf("the declaring source must be named, got %q", d.Source)
			}
			return
		}
	}
	t.Fatal("a cited but absent document vanished from the backlog entirely")
}

func TestExtractInlinesTextAndPointsAtTheRest(t *testing.T) {
	root := extractProject(t)
	p := loadProject(t, root)
	env := Env{Now: "2026-07-26"}

	x, err := p.Graph.Extract(root, "documents/surveyor.md", env)
	if err != nil {
		t.Fatal(err)
	}
	if !x.Inlined || !strings.Contains(x.Prompt, "The barn sits on Lot G") {
		t.Error("a text document must be inlined so the prompt is self-contained")
	}
	// The render rule is inverted here on purpose: in extraction the document IS
	// the input, so its path belongs in the prompt.
	if !strings.Contains(x.Prompt, "documents/surveyor.md") {
		t.Error("the extraction prompt must name the document")
	}
	// No source declares it yet, and the prompt has to say so — otherwise the
	// agent invents a second source node for a document that already has one.
	if !strings.Contains(x.Prompt, "No source node declares this document yet") {
		t.Error("the prompt does not say the source is undeclared")
	}

	// A truncated exhibit is worse than a pointer to a whole one: facts read from
	// the visible half look complete.
	b, err := p.Graph.Extract(root, "documents/scan.jpg", env)
	if err != nil {
		t.Fatal(err)
	}
	if b.Inlined {
		t.Error("a binary exhibit must not be inlined")
	}
	if !strings.Contains(b.Prompt, "not text") {
		t.Errorf("the prompt must say why the body is absent:\n%s", b.Prompt)
	}
}

// Without this the second pass over a document restates the first pass under new
// ids, and `cid` catches it as an error after the fact — better not to write it.
func TestExtractListsWhatIsAlreadyKnown(t *testing.T) {
	root := extractProject(t)
	p := loadProject(t, root)
	x, err := p.Graph.Extract(root, "documents/deed.md", Env{Now: "2026-07-26"})
	if err != nil {
		t.Fatal(err)
	}
	if x.Known != 1 || x.Source != "s-deed" {
		t.Fatalf("want 1 known fact from s-deed, got %d from %q", x.Known, x.Source)
	}
	for _, want := range []string{
		"The strip is held by Halloway", "Do not restate these",
		"already declared as source `s-deed`", "Do not declare a second source",
	} {
		if !strings.Contains(x.Prompt, want) {
			t.Errorf("missing %q in the prompt", want)
		}
	}
}

// The highest-value framing available: not "extract facts" but "close these".
// Only EVIDENCE questions belong — a decision is not waiting on a document, and
// offering it invites a fabricated answer.
func TestExtractOffersOnlyEvidenceQuestionsRanked(t *testing.T) {
	root := extractProject(t)
	p := loadProject(t, root)
	x, err := p.Graph.Extract(root, "documents/deed.md", Env{Now: "2026-07-26"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(x.Prompt, "q-floor") {
		t.Error("a `needs: decision` question was offered as answerable by a document")
	}
	for _, want := range []string{"q-drawing", "q-barn"} {
		if !strings.Contains(x.Prompt, want) {
			t.Errorf("open evidence question %s was not offered", want)
		}
	}
	// The deed's one fact is anchored to `strip`, so the question anchored to
	// `strip` is the likely one and must be ranked above the rest.
	likelyAt := strings.Index(x.Prompt, "Anchored to something this document already touches")
	otherAt := strings.Index(x.Prompt, "Other open evidence questions")
	if likelyAt < 0 || otherAt < 0 || likelyAt > otherAt {
		t.Fatalf("questions are not ranked: likely at %d, other at %d", likelyAt, otherAt)
	}
	if di, bi := strings.Index(x.Prompt, "q-drawing"), strings.Index(x.Prompt, "q-barn"); di > bi {
		t.Error("the question anchored to what this document touches must come first")
	}
}

// Extraction is the one place kgraph supplies instructions, because there is no
// author's prompt to resolve and these rules cannot be inferred from a document.
// Each produces something that PARSES and is wrong when broken.
func TestExtractStatesTheRulesADocumentCannotTeach(t *testing.T) {
	root := extractProject(t)
	p := loadProject(t, root)
	x, err := p.Graph.Extract(root, "documents/surveyor.md", Env{Now: "2026-07-26"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"No `confidence`",         // a hand-typed number is an invented statistic
		"No `disputed`",           // computed from conflicting evidence
		"No negation",             // polarity was deliberately cut
		"not interchangeable",     // withdrawn vs false
		"`about:` anchor",         // a fact anchored to nothing is unfindable
		"needs:",                  // or a question sits open forever
		"semantic slugs",          // ids appear in diffs
		"YAML silently truncates", // the unquoted `#` trap
		"record > admission",      // class is ordinal, not a probability
	} {
		if !strings.Contains(x.Prompt, want) {
			t.Errorf("the extraction rules omit %q", want)
		}
	}
}

// ── the dispute computation ────────────────────────────────────────────

func conflictGraph(t *testing.T, extra string) *Graph {
	t.Helper()
	src := "```kfacts\n" +
		"- id: thing\n  entity: A parcel\n  status: asserted\n" +
		"- id: s-deed\n  record: The deed\n  class: record\n" +
		"- id: s-survey\n  record: The survey\n  class: record\n" +
		"- id: a\n  claim: The strip is Halloway's\n  status: asserted\n" +
		"  about: thing\n  attested_by: s-deed\n" + extra + "```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	return g
}

// Counting only source-undercuts made `disputed` nearly dead: across the corpus
// it fired twice off a single `undercut_by` while seven claim-to-claim
// contradictions produced nothing — and that is the more common shape by far, and
// the important one. A deed against a survey drawing IS the dispute.
func TestClaimContradictionDisputesBothSides(t *testing.T) {
	g := conflictGraph(t,
		"- id: b\n  claim: The survey draws it in Lot I\n  status: asserted\n"+
			"  about: thing\n  attested_by: s-survey\n  contradicts: a\n")
	for _, id := range []string{"a", "b"} {
		if _, disputed := g.conflict(id, ""); !disputed {
			t.Errorf("%s is in a two-sided evidentiary conflict and is not marked disputed", id)
		}
	}
}

// A source undercutting a claim is directional: a source is not itself a claim
// that can be in doubt.
func TestSourceUndercutIsDirectional(t *testing.T) {
	g := conflictGraph(t, "")
	g.Nodes["a"].Status = SAsserted
	d, _ := parseFixture("g.facts", []byte("```kfacts\n"+
		"- id: thing\n  entity: A parcel\n  status: asserted\n"+
		"- id: s-deed\n  record: The deed\n  class: record\n"+
		"- id: s-doubt\n  record: A contrary record\n  class: record\n"+
		"- id: a\n  claim: The strip is Halloway's\n  status: asserted\n"+
		"  about: thing\n  attested_by: s-deed\n  undercut_by: s-doubt\n```\n"))
	g2, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, disputed := g2.conflict("a", ""); !disputed {
		t.Error("an undercut claim must be disputed")
	}
	if _, disputed := g2.conflict("s-doubt", ""); disputed {
		t.Error("a source must never be reported as disputed")
	}
}

// A `status: false` strawman exists so `prohibits` has a target and must never
// flag anything. If it disputed its target, every rejected theory would put a
// live fact in question.
func TestStrawmanDoesNotDispute(t *testing.T) {
	g := conflictGraph(t,
		"- id: b\n  claim: The strip is Bramble's\n  status: false\n"+
			"  about: thing\n  attested_by: s-survey\n  contradicts: a\n")
	if _, disputed := g.conflict("a", ""); disputed {
		t.Error("a rejected strawman put a live fact in question")
	}
}

func TestWithdrawnClaimDoesNotDispute(t *testing.T) {
	g := conflictGraph(t,
		"- id: b\n  claim: The strip is Bramble's\n  status: withdrawn\n"+
			"  about: thing\n  attested_by: s-survey\n  contradicts: a\n")
	if _, disputed := g.conflict("a", ""); disputed {
		t.Error("a withdrawn claim — one we know we got wrong — still disputed its target")
	}
}

// "Evidence pointing both ways" means both ways have evidence. An unsourced
// assertion contradicting a record is not a dispute, it is an unsupported claim.
func TestUnattestedClaimDoesNotDispute(t *testing.T) {
	g := conflictGraph(t,
		"- id: b\n  claim: The strip is Bramble's\n  status: asserted\n"+
			"  about: thing\n  contradicts: a\n")
	if _, disputed := g.conflict("a", ""); disputed {
		t.Error("an unattested claim disputed a record-backed one")
	}
}

// An unsupported claim is not "disputed", it is unsupported — the marker means
// the evidence conflicts.
func TestUnsupportedClaimIsNotDisputed(t *testing.T) {
	d, _ := parseFixture("f.facts", []byte("```kfacts\n"+
		"- id: thing\n  entity: A parcel\n  status: asserted\n"+
		"- id: s-survey\n  record: The survey\n  class: record\n"+
		"- id: a\n  claim: Unsourced\n  status: asserted\n  about: thing\n"+
		"- id: b\n  claim: Contrary\n  status: asserted\n  about: thing\n"+
		"  attested_by: s-survey\n  contradicts: a\n```\n"))
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, disputed := g.conflict("a", ""); disputed {
		t.Error("a claim with no support of its own was reported as disputed")
	}
}

// The corpus is built on contradictions — the impeachment matrix is nothing but
// paired conflicts — so if this drops to near zero the computation has broken.
func TestCorpusDisputesAreComputed(t *testing.T) {
	g := fixtureGraph(t)
	var n int
	for id := range g.Nodes {
		if _, disputed := g.conflict(id, ""); disputed {
			n++
		}
	}
	// Scaled to the corpus, not to a remembered number. The guard is against the
	// computation collapsing, which shows up as zero, not as a count one short.
	if n < 5 {
		t.Errorf("only %d disputed facts in a corpus built on contradictions", n)
	}
}

// ── the disputed predicate ─────────────────────────────────────────────

// `disputed` is computed, so it cannot be authored — the parser rejects it as a
// status. This is the only way to ask about it, and a document rendering contested
// facts as settled is the failure the whole marker exists to prevent.
func TestDisputedPredicatePartitionsClaims(t *testing.T) {
	g := fixtureGraph(t)
	env := Env{Now: "2026-07-26"}
	count := func(src string) int {
		q, err := ParseQuery(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if errs := ValidateQuery(q); len(errs) > 0 {
			t.Fatalf("%s: %v", src, errs)
		}
		rows, err := g.Eval(q, env)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return len(rows)
	}
	all := count("claim")
	yes := count("claim[disputed]")
	no := count("claim[disputed=false]")
	if yes == 0 {
		t.Fatal("no disputed claims in a corpus built on contradictions")
	}
	if yes+no != all {
		t.Errorf("the predicate does not partition: %d + %d != %d", yes, no, all)
	}
	// Every spelling of the same question must agree, or a spec picks the wrong
	// one and silently renders a different set.
	for _, same := range []string{"claim[disputed=true]", "claim[disputed]"} {
		if count(same) != yes {
			t.Errorf("%s disagrees with claim[disputed]", same)
		}
	}
	for _, same := range []string{"claim[disputed=false]", "claim[disputed!=true]", "claim & !claim[disputed]"} {
		if count(same) != no {
			t.Errorf("%s disagrees with claim[disputed=false]: %d vs %d", same, count(same), no)
		}
	}
}

// A computed flag is modelled as present-or-absent, and absence short-circuited
// the comparison path — so `[disputed=false]` and `[computed=false]` matched
// NOTHING rather than the complement. Silently: the query parsed, validated, and
// returned an empty section.
func TestComputedFlagFalseMatchesTheComplement(t *testing.T) {
	g := fixtureGraph(t)
	env := Env{Now: "2026-07-26"}
	for _, f := range []string{"disputed", "computed"} {
		n := func(src string) int {
			q, err := ParseQuery(src)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := g.Eval(q, env)
			if err != nil {
				t.Fatal(err)
			}
			return len(rows)
		}
		all, yes, no := n("claim"), n("claim["+f+"]"), n("claim["+f+"=false]")
		if no == 0 {
			t.Errorf("claim[%s=false] matched nothing", f)
		}
		if yes+no != all {
			t.Errorf("%s: %d + %d != %d", f, yes, no, all)
		}
	}
}

// A computed flag takes no value but true/false, and no ordering operator.
// `[disputed=maybe]` would otherwise validate and match nothing.
func TestComputedFlagRejectsNonBooleanComparisons(t *testing.T) {
	for _, bad := range []string{
		"claim[disputed=maybe]", "claim[disputed>1]",
		"claim[computed=yes]", "claim[computed<2]",
	} {
		q, err := ParseQuery(bad)
		if err != nil {
			continue // rejected earlier, also fine
		}
		if errs := ValidateQuery(q); len(errs) == 0 {
			t.Errorf("%s validated", bad)
		}
	}
}

// The computed predicate and the structural hop are NOT the same question, and
// each catches what the other cannot. Pinning both directions, because collapsing
// them would look like a simplification and lose real findings.
func TestDisputedAndContradictsHopDisagreeForGoodReasons(t *testing.T) {
	g := fixtureGraph(t)
	env := Env{Now: "2026-07-26"}
	set := func(src string) map[string]bool {
		q, err := ParseQuery(src)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := g.Eval(q, env)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, r := range rows {
			out[r] = true
		}
		return out
	}
	hop := set("claim -contradicts- claim")
	computed := set("claim[disputed]")

	// A source undercut is invisible to a claim-to-claim hop.
	// `fence-predates-sale` rests only on `s-neighbour-hearsay`, which the
	// county clerk undercuts, so a document written against the hop never flags it.
	if !computed["fence-predates-sale"] || hop["fence-predates-sale"] {
		t.Error("the predicate must catch a source undercut that the hop cannot see")
	}

	// The reverse: a `contradicts` edge where one side has no source of its own is
	// not evidence pointing both ways. `strip-never-conveyed` cites no
	// instrument — a real gap in the corpus, recorded as q-strip-chain — so
	// the hop finds the pair and the predicate correctly does not.
	if !hop["strip-never-conveyed"] || computed["strip-never-conveyed"] {
		t.Error("an unattested claim must not be reported as having evidence both ways")
	}
	if _, ok := g.Nodes["q-strip-chain"]; !ok {
		t.Error("the gap the predicate exposed must stay recorded as an open question")
	}
}

// ── underdetermined vs disputed ────────────────────────────────────────

func tensionGraph(t *testing.T, extra string) *Graph {
	t.Helper()
	src := "```kfacts\n" +
		"- id: thing\n  entity: A parcel\n  status: asserted\n" +
		"- id: s-eob\n  record: The EOB\n  class: record\n" +
		"- id: s-stmt\n  record: The statement\n  class: record\n" + extra + "```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	return g
}

// The whole distinction: `disputed` means weigh, `underdetermined` means split.
// Producing a for/against tally for a claim whose sources never disagreed makes a
// document answer the wrong question confidently.
func TestUnderdeterminedSuppressesTheDisputedTally(t *testing.T) {
	both := "- id: a\n  claim: What is owed\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-eob\n" +
		"  underdetermined: two quantities, the professional balance and the billed total\n" +
		"- id: b\n  claim: The statement shows more\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-stmt\n  contradicts: a\n"
	g := tensionGraph(t, both)
	got := g.tension("a", "")
	if got.Kind != "underdetermined" {
		t.Fatalf("want underdetermined to win over a live contradiction, got %q", got.Kind)
	}
	if strings.Contains(got.Label(), "for,") {
		t.Errorf("a tally invites weighing sources that never disagreed: %s", got.Label())
	}
	if !strings.Contains(got.Label(), "not one fact") {
		t.Errorf("the label must name the remedy: %s", got.Label())
	}
	// Without the marker the same shape IS a dispute.
	plain := strings.Replace(both,
		"  underdetermined: two quantities, the professional balance and the billed total\n", "", 1)
	if k := tensionGraph(t, plain).tension("a", "").Kind; k != "disputed" {
		t.Errorf("a genuine two-sided conflict must still be disputed, got %q", k)
	}
}

// Asking for contested facts must not return facts whose sources were never in
// conflict — the two sets are disjoint by construction.
func TestDisputedAndUnderdeterminedAreDisjoint(t *testing.T) {
	g := fixtureGraph(t)
	env := Env{Now: "2026-07-26"}
	n := func(src string) int {
		q, err := ParseQuery(src)
		if err != nil {
			t.Fatal(err)
		}
		if errs := ValidateQuery(q); len(errs) > 0 {
			t.Fatal(errs)
		}
		rows, err := g.Eval(q, env)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	if u := n("claim[underdetermined]"); u == 0 {
		t.Fatal("the corpus has no underdetermined claim, so this proves nothing")
	}
	if both := n("claim[disputed] & claim[underdetermined]"); both != 0 {
		t.Errorf("%d claims are reported as both — weigh and split are different remedies", both)
	}
}

// Authored, unlike `disputed`, and the asymmetry is the point: whether a claim is
// ambiguous is a statement about what it MEANS, which no evidence reveals.
func TestUnderdeterminedIsNotAStatus(t *testing.T) {
	_, diags := parseFixture("f.facts", []byte(
		"```kfacts\n- id: a\n  claim: X\n  status: underdetermined\n```\n"))
	var found bool
	for _, d := range Errors(diags) {
		if strings.Contains(d.Msg, "not a status") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a status rejection, got %v", diags)
	}
}

// A bare `true` is a shrug. The value has to name what the claim conflates or
// nobody downstream can act on it, and it never gets split.
func TestUnderdeterminedRequiresAReason(t *testing.T) {
	for _, v := range []string{"true", "yes", "false", "no"} {
		_, diags := parseFixture("f.facts", []byte(
			"```kfacts\n- id: a\n  claim: X\n  status: asserted\n  underdetermined: "+v+"\n```\n"))
		var found bool
		for _, d := range Errors(diags) {
			if strings.Contains(d.Msg, "says nothing") {
				found = true
			}
		}
		if !found {
			t.Errorf("`underdetermined: %s` was accepted: %v", v, diags)
		}
	}
}

// It is a PENDING DEFECT, not a stable state. Unreported it becomes a permanent
// excuse: every document renders the marker and nobody ever makes the split.
func TestUnderdeterminedIsNaggedUntilSplit(t *testing.T) {
	live := "- id: a\n  claim: What is owed\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-eob\n  underdetermined: two quantities\n"
	var msgs []string
	for _, d := range tensionGraph(t, live).checkUnderdetermined() {
		msgs = append(msgs, d.Msg)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "split it") {
		t.Fatalf("a live underdetermined claim must be nagged: %v", msgs)
	}

	// Resolved the way every correction is: two claims supersede it and it goes
	// withdrawn. The exit is the format's existing immutable-fix pattern.
	split := live +
		"- id: a1\n  claim: The EOB professional balance\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-eob\n  supersedes: a\n" +
		"- id: a2\n  claim: The billed total\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-stmt\n  supersedes: a\n"
	if d := tensionGraph(t, split).checkUnderdetermined(); len(d) != 1 ||
		!strings.Contains(d[0].Msg, "still live") {
		t.Fatalf("a superseded-but-live claim must be told to withdraw: %v", d)
	}
	done := strings.Replace(split, "- id: a\n  claim: What is owed\n  status: asserted",
		"- id: a\n  claim: What is owed\n  status: withdrawn", 1)
	if d := tensionGraph(t, done).checkUnderdetermined(); len(d) != 0 {
		t.Errorf("a withdrawn, split claim must stop being nagged: %v", d)
	}
}

// It changes what a document says, so it belongs in the staleness contract.
func TestUnderdeterminedIsInSemHash(t *testing.T) {
	base := "- id: a\n  claim: X\n  status: asserted\n  about: thing\n  attested_by: s-eob\n"
	plain := tensionGraph(t, base).SemHash["a"]
	marked := tensionGraph(t, base+"  underdetermined: two quantities\n").SemHash["a"]
	if plain == marked {
		t.Error("declaring a fact not-one-fact must move its sem_hash")
	}
}

// ── impeachment ────────────────────────────────────────────────────────

// An impeachment is a contradiction with a WINNER, and reporting only "1 for, 1
// against" throws that away — a document then hedges where it should press. Every
// ingredient was already in the graph: sources carry a speaker, class is ordinal,
// and `contradicts` says what conflicts. Nothing computed it.
func TestRecordImpeachesSwornStatement(t *testing.T) {
	src := "```kfacts\n" +
		"- id: wren\n  entity: Rafe Wren\n  status: asserted\n" +
		"- id: thing\n  entity: The septic\n  status: asserted\n" +
		"- id: s-decl\n  utterance: Wren declaration\n  by: wren\n  class: interested\n" +
		"- id: s-repair\n  record: County repair record\n  class: record\n" +
		"- id: sworn\n  claim: Wren swore it was serviced in 2020\n  status: asserted\n" +
		"  about: thing\n  attested_by: s-decl\n" +
		"- id: rec\n  claim: The repair was in 2015\n  status: asserted\n" +
		"  about: thing\n  attested_by: s-repair\n  contradicts: sworn\n" +
		"```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}

	imp, ok := g.impeachment("sworn", "")
	if !ok {
		t.Fatal("a record contradicting an interested party's sworn statement is an impeachment")
	}
	if imp.Speaker != "wren" || imp.Class != "interested" || imp.Beat != "record" {
		t.Errorf("got %+v", imp)
	}
	if ten := g.tension("sworn", ""); ten.Kind != "impeached" {
		t.Errorf("the losing side must read as impeached, got %q", ten.Kind)
	}
	// The winning side is contested but not impeached — nothing outranks it.
	if ten := g.tension("rec", ""); ten.Kind != "disputed" {
		t.Errorf("the record must not be impeached by an interested source: %q", ten.Kind)
	}
	// And its label must not read as a tie.
	if lbl := g.tension("rec", "").Label(); !strings.Contains(lbl, "record") || !strings.Contains(lbl, "interested") {
		t.Errorf("the tally must name the classes, or a document reads it as even: %s", lbl)
	}
}

// Equal class is a dispute to weigh on the merits. Only a strictly higher class
// impeaches, or every mutual contradiction would claim a winner.
func TestEqualClassConflictIsNotImpeachment(t *testing.T) {
	src := "```kfacts\n" +
		"- id: a1\n  entity: Someone\n  status: asserted\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s1\n  record: Record one\n  by: a1\n  class: record\n" +
		"- id: s2\n  record: Record two\n  class: record\n" +
		"- id: x\n  claim: X\n  status: asserted\n  about: thing\n  attested_by: s1\n" +
		"- id: y\n  claim: Not X\n  status: asserted\n  about: thing\n  attested_by: s2\n" +
		"  contradicts: x\n```\n"
	d, _ := parseFixture("f.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	if _, ok := g.impeachment("x", ""); ok {
		t.Error("two records of equal weight are a dispute, not an impeachment")
	}
	if k := g.tension("x", "").Kind; k != "disputed" {
		t.Errorf("want disputed, got %q", k)
	}
}

// A sworn declaration is a court record as an ARTIFACT and an interested party's
// assertion as EVIDENCE. Classed `record` it tied with every record refuting it,
// so no gap existed and impeachment could never compute — which is exactly what
// the corpus did.
func TestCorpusImpeachmentsAreComputed(t *testing.T) {
	g := fixtureGraph(t)
	env := Env{Now: "2026-07-26"}
	q, err := ParseQuery("claim[impeached] sort id")
	if err != nil {
		t.Fatal(err)
	}
	if errs := ValidateQuery(q); len(errs) > 0 {
		t.Fatal(errs)
	}
	rows, err := g.Eval(q, env)
	if err != nil {
		t.Fatal(err)
	}
	// Scaled to the corpus: fence-dispute is deliberately smaller than the corpus
	// this threshold was first written against, and its impeachment matrix is two
	// pairs. What matters is that the computation produces them, not the count —
	// a drop to zero is the regression this guards.
	if len(rows) < 2 {
		t.Fatalf("the corpus is built on impeachment and only %d compute: %v", len(rows), rows)
	}
	// Disjoint from plain disputes: those are the ones still to be weighed.
	dq, _ := ParseQuery("claim[impeached] & claim[disputed]")
	both, err := g.Eval(dq, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 0 {
		t.Errorf("%d claims are both impeached and merely disputed", len(both))
	}
	// The declaration must not be classed as a record, or the gap closes again.
	if n, ok := g.Lookup("s-bramble-account"); ok && n.Source.Class == "record" {
		t.Error("a party's declaration classed `record` outranks nothing and is outranked by nothing")
	}
}

// A claim is only as defensible as its weakest support, so that is what gets
// impeached — otherwise adding one strong citation would hide the weak one.
func TestWeakestSupportIsWhatIsImpeached(t *testing.T) {
	src := "```kfacts\n" +
		"- id: wren\n  entity: Rafe Wren\n  status: asserted\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s-decl\n  utterance: Wren declaration\n  by: wren\n  class: interested\n" +
		"- id: s-obs\n  observation: A walkthrough\n  by: wren\n  class: observation\n" +
		"- id: s-rec\n  record: The record\n  class: record\n" +
		"- id: sworn\n  claim: X\n  status: asserted\n  about: thing\n" +
		"  attested_by: [s-decl, s-obs]\n" +
		"- id: rec\n  claim: Not X\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-rec\n  contradicts: sworn\n```\n"
	d, _ := parseFixture("f.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	imp, ok := g.impeachment("sworn", "")
	if !ok {
		t.Fatal("no impeachment computed")
	}
	if imp.Class != "interested" {
		t.Errorf("the weakest support is what is impeached, got %q", imp.Class)
	}
}

// ── evidentiary class ──────────────────────────────────────────────────

// Law is not evidence. A statute does not testify to a fact, it decides what
// testimony is worth — so ranking it alongside witnesses lets it "impeach" one,
// which is a category error. RCW 48.49.080 was classed `record` and therefore sat
// at the TOP of the ladder, outranking every witness in the corpus.
func TestAuthorityIsOutsideTheEvidentiaryLadder(t *testing.T) {
	if _, ranked := legal.Rank(ClassAuthority); ranked {
		t.Fatal("authority must have no rank — having none is the whole mechanism")
	}
	if !validClass(ClassAuthority) {
		t.Fatal("authority must be authorable")
	}
	src := "```kfacts\n" +
		"- id: someone\n  entity: A witness\n  status: asserted\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s-law\n  document: RCW 48.49.080\n  class: authority\n" +
		"- id: s-said\n  utterance: What the witness said\n  by: someone\n  class: statement\n" +
		"- id: legal\n  claim: The statute requires passive disclosure only\n  status: asserted\n" +
		"  about: thing\n  attested_by: s-law\n" +
		"- id: told\n  claim: They told me otherwise\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-said\n  contradicts: legal\n```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	// Authority supports without competing: it can neither impeach nor be impeached.
	if _, ok := g.impeachment("told", ""); ok {
		t.Error("a statute impeached a witness — law decides what testimony is worth, it does not refute it")
	}
	if _, ok := g.impeachment("legal", ""); ok {
		t.Error("a witness impeached a statute")
	}
	// But it still attests, so a legal conclusion resting on it is not unsourced.
	for _, d := range g.checkAttestation() {
		if strings.Contains(d.Msg, "\"legal\"") {
			t.Error("a claim resting on authority was reported as resting on nothing")
		}
	}
	// And it reads as itself rather than as "unclassed".
	if lbl := g.tension("legal", "").Label(); lbl != "" && !strings.Contains(lbl, "authority") {
		t.Errorf("authority must be named in the tally: %s", lbl)
	}
}

// ClassRank and ClassLadder are the ladder's public surface, and they exist so a
// consumer deriving anything from evidentiary weight stops hand-copying the table.
// caselit is the first: it reads whether a legal element is backed or thin off
// these rungs, and a copy that went stale against a reweight promoted weak support
// to strong without a word.
//
// What this pins is the contract, not the numbers: authority stays unranked
// through the exported form (a caller reading zero for "no rank" reintroduces the
// category error above), the ladder agrees with the lookup, and the returned slice
// is a copy — a consumer must not be able to reorder the ladder the graph resolves
// conflicts with.
func TestExportedClassLadderIsTheInternalOne(t *testing.T) {
	if _, ok := ClassRank(ClassAuthority); ok {
		t.Error("ClassRank ranked authority — outside the ladder is the whole mechanism")
	}
	if _, ok := ClassRank("hearsay"); ok {
		t.Error("ClassRank ranked a class kgraph does not know")
	}
	ladder := ClassLadder()
	// Against the LEGAL dialect's own table, which is where the ladder now lives.
	// The package accessors delegate to it, and this is what pins that they still
	// agree — a delegation that silently answered from somewhere else would be
	// exactly the stale copy ClassRank exists to prevent.
	if len(ladder) != len(legal.rank) {
		t.Fatalf("ClassLadder has %d rungs, the legal dialect has %d", len(ladder), len(legal.rank))
	}
	for i, c := range ladder {
		r, ok := ClassRank(c)
		if !ok || r != legal.rank[c] {
			t.Errorf("ClassLadder[%d]=%q ranks %d/%v, the dialect says %d", i, c, r, ok, legal.rank[c])
		}
		if i > 0 {
			if prev, _ := ClassRank(ladder[i-1]); prev <= r {
				t.Errorf("ClassLadder is not strongest-first at %d: %q(%d) then %q(%d)",
					i, ladder[i-1], prev, c, r)
			}
		}
	}
	ladder[0] = "vibes"
	if again := ClassLadder(); again[0] == "vibes" {
		t.Error("ClassLadder handed out the table itself; a consumer can reorder the ladder")
	}
}

// An admission is against the speaker's own interest and an interested source is a
// party asserting what benefits them. Both are claims about WHOSE interest, so
// neither means anything without naming who.
func TestInterestClassesRequireASpeaker(t *testing.T) {
	for _, class := range []string{"admission", "interested"} {
		src := "```kfacts\n" +
			"- id: thing\n  entity: A thing\n  status: asserted\n" +
			"- id: s\n  utterance: Something said\n  class: " + class + "\n" +
			"- id: c\n  claim: X\n  status: asserted\n  about: thing\n  attested_by: s\n```\n"
		d, _ := parseFixture("f.facts", []byte(src))
		g, _ := Build([]*Doc{d})
		var found bool
		for _, x := range g.checkSources() {
			if strings.Contains(x.Msg, "names no `by:`") {
				found = true
			}
		}
		if !found {
			t.Errorf("class %s with no speaker was not reported", class)
		}
	}
	// A neutral class needs no speaker — a county record has an author, not an
	// interest.
	src := "```kfacts\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s\n  record: A county record\n  class: record\n" +
		"- id: c\n  claim: X\n  status: asserted\n  about: thing\n  attested_by: s\n```\n"
	d, _ := parseFixture("f.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	for _, x := range g.checkSources() {
		if strings.Contains(x.Msg, "names no `by:`") {
			t.Error("a record was asked for a speaker it does not need")
		}
	}
}

// The audit's own conclusions, pinned. A contemporaneous record authored by a
// named person is still a record — that is why these carry a speaker AND a top
// class, and "fixing" them would destroy the impeachments that depend on the gap.
func TestCorpusClassesSurviveTheAudit(t *testing.T) {
	g := fixtureGraph(t)
	want := map[string]string{
		// ordinary-course records that happen to name their author
		"s-plat-1962":   "record",
		"s-docket":      "record",
		"s-vole-2026":   "record",
		"s-survey-decl": "record",
		// a party's agent asserting what benefits them
		"s-bramble-account": "interested",
		// the one claim it attests is a concession against his own side
		"s-quill-depo": "admission",
		// law, not evidence
		"s-rule-14-18": ClassAuthority,
		// a party's account of their own diligence
		"s-client-note": "interested",
	}
	for id, class := range want {
		n, ok := g.Lookup(id)
		if !ok {
			t.Errorf("%s is gone", id)
			continue
		}
		if n.Source == nil || n.Source.Class != class {
			got := ""
			if n.Source != nil {
				got = n.Source.Class
			}
			t.Errorf("%s: want class %q, got %q", id, class, got)
		}
	}
}

// ── correction vs conflict ─────────────────────────────────────────────

func correctionGraph(t *testing.T, oldExtra, newExtra string) *Graph {
	t.Helper()
	// `status:` is written once. It used to be hard-coded here AND supplied by
	// callers through oldExtra, which made every `status: withdrawn` case a node
	// with the key twice — last-write-wins, so the test passed for the right
	// reason by accident. parseNode now reports that, which is how this surfaced.
	oldStatus := "  status: asserted\n"
	if strings.Contains(oldExtra, "status:") {
		oldStatus = ""
	}
	src := "```kfacts\n" +
		"- id: thing\n  entity: A parcel\n  status: asserted\n" +
		"- id: s1\n  record: First survey\n  class: record\n" +
		"- id: s2\n  record: Corrected survey\n  class: record\n" +
		"- id: old\n  claim: The survey omits the strip\n" + oldStatus +
		"  about: thing\n  attested_by: s1\n" + oldExtra +
		"- id: new\n  claim: The survey shows the strip\n  status: asserted\n" +
		"  about: thing\n  attested_by: s2\n  contradicts: old\n" + newExtra +
		"```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	return g
}

// A correction is not a conflict. The first survey WAS the record until the
// second replaced it — the earlier fact is not refuted, it is superseded, and
// anyone who relied on it in its window relied correctly. Weighing a fact against
// its own correction reports "disputed" for something that simply changed.
func TestDisjointWindowsAreNotAConflict(t *testing.T) {
	g := correctionGraph(t,
		"  valid_from: 2022-03-01\n  valid_until: 2022-05-23\n",
		"  valid_from: 2022-05-23\n  supersedes: old\n")
	for _, id := range []string{"old", "new"} {
		if ten := g.tension(id, ""); ten.Any() {
			t.Errorf("%s reads as %s, but the two are never true at the same time", id, ten.Kind)
		}
	}
}

// Overlapping windows still conflict, or the fix would silence real disputes.
func TestOverlappingWindowsStillConflict(t *testing.T) {
	g := correctionGraph(t,
		"  valid_from: 2022-03-01\n  valid_until: 2022-12-31\n",
		"  valid_from: 2022-05-23\n")
	if ten := g.tension("old", ""); ten.Kind != "disputed" {
		t.Errorf("windows overlap from May to December, so this is a real dispute: %q", ten.Kind)
	}
}

// No window means "as far as we know, always" — which is what an undated
// assertion actually says, so it overlaps everything.
func TestUnboundedClaimOverlapsEverything(t *testing.T) {
	g := correctionGraph(t, "", "  valid_from: 2022-05-23\n")
	if ten := g.tension("old", ""); ten.Kind != "disputed" {
		t.Errorf("an unbounded claim must still be contestable: %q", ten.Kind)
	}
}

// `supersedes` says "this replaces that". If both are live and unbounded, a `@now`
// query returns both and a document states the corrected fact AND the thing it
// corrected, as equals.
func TestOpenEndedSupersessionIsReported(t *testing.T) {
	fires := func(g *Graph) bool {
		for _, d := range g.checkSupersession() {
			if strings.Contains(d.Msg, "still open-ended") {
				return true
			}
		}
		return false
	}
	if !fires(correctionGraph(t, "", "  supersedes: old\n")) {
		t.Error("an unbounded superseded fact was not reported")
	}
	// The two legitimate shapes, and the difference is the one that matters:
	// `withdrawn` says we were wrong and flags every document that cited it;
	// a closed window says it was true then, so those documents were RIGHT.
	if fires(correctionGraph(t, "  status: withdrawn\n", "  supersedes: old\n")) {
		t.Error("a withdrawn fact is retired outright and needs no window")
	}
	if fires(correctionGraph(t, "  valid_until: 2022-05-23\n", "  supersedes: old\n")) {
		t.Error("a bounded fact has already said when it stopped being true")
	}
}

// "Not always was": the corrected record is what a query sees now, and the
// original is what a query sees at a date inside its window. A filing that relied
// on the record as it stood was correct.
func TestCorrectedRecordIsTimeAddressable(t *testing.T) {
	g := fixtureGraph(t)
	at := func(date string) []string {
		q, err := ParseQuery("claim -about-> #the-strip @" + date)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := g.Eval(q, Env{Now: date})
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if got := at("2024-04-01"); !has(got, "survey-2023-omits-strip") ||
		has(got, "survey-2026-shows-strip") {
		t.Errorf("in April 2024 the omission was the record: %v", got)
	}
	if got := at("2026-07-26"); has(got, "survey-2023-omits-strip") ||
		!has(got, "survey-2026-shows-strip") {
		t.Errorf("today the correction is the record: %v", got)
	}
	// And neither is disputed, though they contradict: same surveyor, both records,
	// disjoint windows.
	for _, id := range []string{"survey-2023-omits-strip", "survey-2026-shows-strip"} {
		if ten := g.tension(id, ""); ten.Any() {
			t.Errorf("%s reads as %s; a recorded correction is not a standing disagreement", id, ten.Kind)
		}
	}
}

// ── time ranges on attestations ────────────────────────────────────────

// An attestation can lapse like any other relation. `edgeLive` was honoured by
// query hops and group membership but not by `conflict`, `impeachment` or
// `supportedClaim` — so an expired attestation kept a fact "supported" by
// evidence that no longer speaks to it, and a withdrawn declaration went on
// impeaching forever.
func TestLapsedAttestationStopsCounting(t *testing.T) {
	src := "```kfacts\n" +
		"- id: wren\n  entity: Rafe Wren\n  status: asserted\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s-decl\n  utterance: Wren declaration\n  by: wren\n  class: interested\n" +
		"- id: s-rec\n  record: The record\n  class: record\n" +
		"- id: sworn\n  claim: Wren swore X\n  status: asserted\n  about: thing\n" +
		"  attested_by: [{id: s-decl, valid_until: 2023-01-01}]\n" +
		"- id: rec\n  claim: Not X\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-rec\n  contradicts: sworn\n```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	// Inside the window the impeachment stands.
	if _, ok := g.impeachment("sworn", "2022-06-01"); !ok {
		t.Error("the attestation was live in 2022 and should impeach")
	}
	// After it lapses the claim rests on nothing, so there is nothing to impeach.
	if _, ok := g.impeachment("sworn", "2024-06-01"); ok {
		t.Error("a lapsed attestation went on impeaching")
	}
	if g.supportedClaim("sworn", "2024-06-01") {
		t.Error("a lapsed attestation still counts as support")
	}
	if !g.supportedClaim("sworn", "2022-06-01") {
		t.Error("a live attestation must count")
	}
	// Undated asks about the graph as authored, which is what a bare query means.
	if !g.supportedClaim("sworn", "") {
		t.Error("an undated question must see every attestation")
	}
}

// An expired `contradicts` must stop disputing, for the same reason.
func TestLapsedContradictionStopsDisputing(t *testing.T) {
	src := "```kfacts\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s1\n  record: One\n  class: record\n" +
		"- id: s2\n  record: Two\n  class: record\n" +
		"- id: a\n  claim: X\n  status: asserted\n  about: thing\n  attested_by: s1\n" +
		"- id: b\n  claim: Not X\n  status: asserted\n  about: thing\n  attested_by: s2\n" +
		"  contradicts: [{id: a, valid_until: 2023-01-01}]\n```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, _ := Build([]*Doc{d})
	if k := g.tension("a", "2022-06-01").Kind; k != "disputed" {
		t.Errorf("live contradiction, want disputed, got %q", k)
	}
	if k := g.tension("a", "2024-06-01").Kind; k != "" {
		t.Errorf("expired contradiction still reads as %q", k)
	}
}

// An event's `at` is when it OCCURRED, and having occurred stays true forever —
// so occurrence must not be treated as a truth window. Two contradictory events
// at different dates are a real conflict about what happened, not a succession.
func TestOccurrenceIsNotATruthWindow(t *testing.T) {
	src := "```kfacts\n" +
		"- id: thing\n  entity: A docket\n  status: asserted\n" +
		"- id: s1\n  record: The packet\n  class: record\n" +
		"- id: s2\n  record: The docket\n  class: record\n" +
		"- id: e1\n  event: Partial SJ entered\n  at: 2023-04-24\n  status: asserted\n" +
		"  about: thing\n  attested_by: s1\n" +
		"- id: e2\n  event: Partial SJ entered\n  at: 2023-04-25\n  status: asserted\n" +
		"  about: thing\n  attested_by: s2\n  contradicts: e1\n```\n"
	d, _ := parseFixture("f.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	if k := g.tension("e1", "").Kind; k != "disputed" {
		t.Errorf("two records disagreeing about when something happened is a conflict, got %q", k)
	}
}

// ── root resolution ────────────────────────────────────────────────────

// The home directory is never a project root, however it is marked. The daemon
// used to keep its state in `~/.kgraph/` — the exact string DiscoverRoot searches
// for — so a project anywhere under $HOME resolved to $HOME and every request
// walked the entire home directory. Slow, far outside the corpus, and it failed
// on the first unreadable directory in it.
func TestHomeIsNeverAProjectRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".kgraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "life")
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(proj, "projects", "a-case")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	if got == home {
		t.Fatal("resolved to $HOME — every request would walk the whole home directory")
	}
	if got != proj {
		t.Errorf("want the project's own git root %q, got %q", proj, got)
	}
}

// Daemon state must not sit in the directory that marks a project.
func TestDaemonStateIsNotAProjectMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	p := statePath("known.json")
	if strings.Contains(p, string(filepath.Separator)+".kgraph"+string(filepath.Separator)) {
		t.Errorf("state lives in the project marker directory: %s", p)
	}
	if !strings.HasPrefix(p, home) {
		t.Errorf("state escaped the home directory: %s", p)
	}
}

// A real corpus contains permission-denied corners. A fingerprint that errors out
// makes every request fail rather than degrading.
func TestUnreadableDirectoryDoesNotFailTheScope(t *testing.T) {
	root := t.TempDir()
	writeLegacyFacts(t, root, "f.kfacts.md",
		"```kfacts\n- id: a\n  entity: A thing\n  status: asserted\n```\n")
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	fp, err := fingerprint(root, nil)
	if err != nil {
		t.Fatalf("one unreadable directory failed the whole fingerprint: %v", err)
	}
	if fp == "" {
		t.Error("no fingerprint produced")
	}
}

// A scan with a transcript beside it should be read from the transcript — the
// same document in readable form — instead of sending the reader to open a PDF
// page by page.
func TestTranscriptIsPreferredOverAScan(t *testing.T) {
	root := t.TempDir()
	w := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeLegacyFacts(t, root, "f.kfacts.md", "```kfacts\n- id: t\n  entity: A case\n  status: asserted\n```\n")
	w("answer.pdf", "%PDF-1.4\n\x00\x01\xff\xfe binary\n")
	w("answer.ocr.md", "COME NOW the Defendants. 2.4 Admitted.\n")
	w("answer-analysis.md", "My read: the admission at 2.4 is the knowledge element.\n")

	p := loadProject(t, root)
	x, err := p.Graph.Extract(root, "answer.pdf", Env{Now: "2026-07-26"})
	if err != nil {
		t.Fatal(err)
	}
	if !x.Inlined || x.Transcript != "answer.ocr.md" {
		t.Fatalf("the transcript was not used: inlined=%v transcript=%q", x.Inlined, x.Transcript)
	}
	if !strings.Contains(x.Prompt, "COME NOW the Defendants") {
		t.Error("transcript text is missing from the prompt")
	}
	if !strings.Contains(x.Prompt, "Cite the document, not the transcript") {
		t.Error("the prompt must say which of the two to cite")
	}

	// An analysis is someone's READING. Inlining it as the document would launder
	// an interpretation into the fact record — facts would cite the exhibit while
	// actually resting on a prior conclusion about it.
	if strings.Contains(x.Prompt, "My read:") {
		t.Error("an analysis was inlined as the document")
	}
	if x.Analysis != "answer-analysis.md" || !strings.Contains(x.Prompt, "not the document") {
		t.Error("the analysis must be mentioned as a reading, not silently dropped")
	}
}

// ── an analysis is not a primary source ────────────────────────────────

func inferenceGraph(t *testing.T, body string) *Graph {
	t.Helper()
	d, diags := parseFixture("f.facts", []byte("```kfacts\n"+
		"- id: thing\n  entity: A thing\n  status: asserted\n"+
		"- id: s-rec\n  record: A record\n  class: record\n"+body+"```\n"))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, _ := Build([]*Doc{d})
	return g
}

func inferenceDiags(g *Graph, want string) bool {
	for _, d := range g.checkInference() {
		if strings.Contains(d.Msg, want) {
			return true
		}
	}
	return false
}

// A conclusion presented as provenance with no premises is an opinion wearing
// evidence's clothing: the facts resting on it look sourced when nothing
// underneath them is.
func TestUnmooredInferenceIsReported(t *testing.T) {
	g := inferenceGraph(t,
		"- id: s-analysis\n  inference: My read of the case\n  class: inference\n"+
			"- id: c\n  claim: Therefore we win\n  status: asserted\n  about: thing\n"+
			"  attested_by: s-analysis\n")
	if !inferenceDiags(g, "declares no `derived_from`") {
		t.Fatal("an inference with no premises was accepted as a source")
	}
	ok := inferenceGraph(t,
		"- id: base\n  claim: A fact\n  status: asserted\n  about: thing\n  attested_by: s-rec\n"+
			"- id: s-analysis\n  inference: My read\n  derived_from: [base]\n  class: inference\n"+
			"- id: c\n  claim: Therefore\n  status: asserted\n  about: thing\n  attested_by: s-analysis\n")
	if inferenceDiags(ok, "declares no `derived_from`") {
		t.Error("an inference that names its premises must be accepted")
	}
}

// The worst shape: the analysis derives from a fact it also attests, so the fact
// supports itself by way of the analysis and nothing underneath is load-bearing.
func TestCircularInferenceIsAnError(t *testing.T) {
	g := inferenceGraph(t,
		"- id: c\n  claim: Use was permissive\n  status: asserted\n  about: thing\n"+
			"  attested_by: s-analysis\n"+
			"- id: s-analysis\n  inference: Driveway analysis\n  derived_from: [c]\n  class: inference\n")
	var found bool
	for _, d := range g.checkInference() {
		if d.Severity == SevError && strings.Contains(d.Msg, "also attests it") {
			found = true
		}
	}
	if !found {
		t.Fatalf("circular provenance was not an error: %v", g.checkInference())
	}
}

// An analysis is a READING of a document, not the document. Citing one at a class
// a reading has not earned lets an interpretation outrank the record it reads.
func TestAnalysisDocumentMustBeClassedAsInference(t *testing.T) {
	g := inferenceGraph(t,
		"- id: s-bad\n  record: Wren declaration analysis\n"+
			"  doc: documents/wren-declaration-analysis.md\n  class: record\n"+
			"- id: c\n  claim: X\n  status: asserted\n  about: thing\n  attested_by: s-bad\n")
	if !inferenceDiags(g, "cites an analysis") {
		t.Fatal("an analysis classed as a record was accepted")
	}
}

// The corpus must stay clean of all three, or the checks are decoration.
func TestCorpusInferencesAreGrounded(t *testing.T) {
	g := fixtureGraph(t)
	for _, d := range g.checkInference() {
		t.Errorf("%v", d)
	}
}

// A transcript is the same document in readable form, not another exhibit.
// Materialising seven of them grew the court backlog from 25 to 30, which reads
// as more work appearing because work was done.
func TestTranscriptIsNotSeparateBacklog(t *testing.T) {
	root := t.TempDir()
	w := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeLegacyFacts(t, root, "f.kfacts.md", "```kfacts\n- id: t\n  entity: A case\n  status: asserted\n```\n")
	w("order.pdf", "%PDF binary\n")
	w("order.ocr.md", "the order text\n")
	w("orphan.ocr.md", "text with no original — real evidence\n")

	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range docs {
		seen[d.Doc] = true
	}
	if !seen["order.pdf"] {
		t.Error("the document itself must stay in the backlog")
	}
	if seen["order.ocr.md"] {
		t.Error("a transcript was listed as separate work")
	}
	// A stray transcript with no original IS the only copy, so it is real evidence.
	if !seen["orphan.ocr.md"] {
		t.Error("a transcript with no original must stay listed")
	}
}

// A retired fact is not in question. `withdrawn` means we already concluded we
// were wrong; reporting it as contested puts a settled matter back into a
// document as though it were live. Four corrected Wren paragraphs went on
// reporting as impeached after being withdrawn.
func TestRetiredFactsAreNotInTension(t *testing.T) {
	src := "```kfacts\n" +
		"- id: thing\n  entity: A thing\n  status: asserted\n" +
		"- id: s-decl\n  utterance: A declaration\n  by: thing\n  class: interested\n" +
		"- id: s-rec\n  record: A record\n  class: record\n" +
		"- id: rec\n  claim: The record says X\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-rec\n" +
		"- id: live\n  claim: Sworn not-X\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-decl\n  contradicts: rec\n" +
		"- id: retired\n  claim: Sworn not-X, recorded wrongly\n  status: withdrawn\n" +
		"  about: thing\n  attested_by: s-decl\n  contradicts: rec\n" +
		"- id: strawman\n  claim: Tested and rejected\n  status: false\n  about: thing\n" +
		"  attested_by: s-decl\n  contradicts: rec\n```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, _ := Build([]*Doc{d})
	if k := g.tension("live", "").Kind; k != "impeached" {
		t.Errorf("a live contradicted claim must still be impeached, got %q", k)
	}
	for _, id := range []string{"retired", "strawman"} {
		if ten := g.tension(id, ""); ten.Any() {
			t.Errorf("%s is retired and reads as %s", id, ten.Kind)
		}
	}
}

// The backlog is per-index, and an index is a closed graph. Another matter's
// evidence is not this matter's unread work: offering it as something to read
// invites reading one person's records into another's graph, which is the exact
// crossing the index scheme exists to prevent. On `~/life` this reported 805
// unread of which 253 were other matters.
//
// It must also skip dot-DIRECTORIES. Dot-files were already skipped, dot-dirs
// were not, so Syncthing's `.stversions` — an archive of every superseded
// version of every file in the tree — arrived as real backlog entries.
func TestBacklogIsScopedToItsIndexAndSkipsDotDirs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Two matters, each a declared index, each with one uncited document.
	write("mine/.kg-index", "mine\n")
	writeLegacyFacts(t, root, "mine/f.kfacts.md", "# f\n\n```kfacts\n- id: c\n  claim: Something\n  status: asserted\n```\n")
	write("mine/documents/exhibit.md", "my evidence\n")
	write("theirs/.kg-index", "theirs\n")
	writeLegacyFacts(t, root, "theirs/f.kfacts.md", "# f\n\n```kfacts\n- id: c\n  claim: Theirs\n  status: asserted\n```\n")
	write("theirs/documents/medical.md", "someone else's records\n")
	// Syncthing's archive, holding a superseded copy of my own exhibit.
	write(".stversions/mine/documents/exhibit.md", "an older version\n")

	p, _, err := LoadIn(root, "mine")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range docs {
		got = append(got, d.Doc)
	}
	for _, want := range []string{"mine/documents/exhibit.md"} {
		if !containsStr(got, want) {
			t.Errorf("this index's own evidence is missing from its backlog: %v", got)
		}
	}
	for _, never := range []string{
		"theirs/documents/medical.md",           // another matter
		"theirs/f.kfacts.md",                    // ditto
		".stversions/mine/documents/exhibit.md", // Syncthing's archive
	} {
		if containsStr(got, never) {
			t.Errorf("%q must not be in this index's backlog: %v", never, got)
		}
	}
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// A text extraction whose stem already carries its own extension. `X.docx.txt`
// is the text OF `X.docx`; the sibling is the stem itself, not stem+ext, and only
// stem+ext was checked — so 67 `.docx.txt` files in the corpus counted as 67
// additional exhibits, making reading one document look like two.
func TestTextExtractionOfAnAlreadyExtensionedStemIsNotASecondExhibit(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "documents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"documents/answer.docx", "documents/answer.docx.txt"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !isTranscriptOf(root, "documents/answer.docx.txt") {
		t.Error("`answer.docx.txt` is the text of `answer.docx`, not another exhibit")
	}
	// The original itself is of course still an exhibit.
	if isTranscriptOf(root, "documents/answer.docx") {
		t.Error("the document must not be mistaken for its own transcript")
	}
	// And a .txt with no such original stands on its own.
	if err := os.WriteFile(filepath.Join(root, "documents/note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isTranscriptOf(root, "documents/note.txt") {
		t.Error("a .txt with no original is its own document")
	}
}

// A source with no `doc:` is the quiet failure. Facts rest on it, `kg source
// status` cannot see it because there is no path to hash, and the disk walk
// never lists it because there is no file — so the graph asserts what a deed
// says while holding no deed. In the fence-dispute corpus that was 17 sources,
// including four recorded instruments cited by auditor file number.
//
// `inference` is exempt: it is our own conclusion, `derived_from` names its
// premises, and there is no document to have.
func TestUndocumentedSourcesAreReportedButInferenceIsExempt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "documents/held.md"), []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	facts := "# f\n\n```kfacts\n" +
		"- id: s-held\n  record: A document we hold\n  doc: documents/held.md\n  class: record\n" +
		"- id: s-deed\n  record: A deed cited by file number, never obtained\n  class: record\n" +
		"- id: s-why\n  inference: We concluded it ourselves\n  class: inference\n" +
		"- id: c1\n  claim: Something the deed says\n  status: asserted\n  attested_by: s-deed\n" +
		"```\n"
	writeLegacyFacts(t, root, "f.kfacts.md", facts)
	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for _, d := range docs {
		if d.Source != "" {
			state[d.Source] = d.State
		}
	}
	if state["s-deed"] != "undocumented" {
		t.Errorf("a source citing a document nobody holds must be reported, got %q", state["s-deed"])
	}
	if _, listed := state["s-why"]; listed {
		t.Error("an inference has no document to have and must not be reported as missing one")
	}
	if state["s-held"] == "undocumented" {
		t.Error("a source whose document is on disk is not undocumented")
	}
	// It must carry the fact count, or there is no way to rank what to chase.
	for _, d := range docs {
		if d.Source == "s-deed" && d.Facts != 1 {
			t.Errorf("undocumented source must report how many facts rest on it, got %d", d.Facts)
		}
	}
}

// A raglit transcription is the document in readable form, not a second exhibit.
//
// It must be recognised as a TRANSCRIPT so the backlog does not double-count it
// and an extraction prompt can use the readable text while still citing the
// document it came from. Ignoring it instead would hide it from extraction too,
// losing the text that makes a scan usable.
//
// The naming differs from the older sidecars: `X.ocr.md` replaces the extension,
// `X.pdf.raglit-transcription.md` appends to the whole name.
func TestRaglitTranscriptionIsATranscriptNotAnExhibit(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "documents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "documents/order.pdf"
	tr := doc + ".raglit-transcription.md"
	for _, rel := range []string{doc, tr} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !isTranscriptOf(root, tr) {
		t.Error("a raglit transcription of a document in the corpus is not separate evidence")
	}
	gotTr, _ := companions(root, doc)
	if gotTr != tr {
		t.Errorf("the document should find its transcription, got %q", gotTr)
	}
	// It must not appear as its own backlog entry.
	facts := "# f\n\n```kfacts\n- id: c\n  claim: Something\n  status: asserted\n```\n"
	writeLegacyFacts(t, root, "f.kfacts.md", facts)
	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.Doc == tr {
			t.Error("the transcription must not be listed as its own document")
		}
	}
	// A stray transcription with no original IS evidence and stays listed.
	stray := "documents/orphan.pdf.raglit-transcription.md"
	if err := os.WriteFile(filepath.Join(root, stray), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isTranscriptOf(root, stray) {
		t.Error("a transcription whose original is absent is not a transcript of anything held")
	}
}

// The difference between a backlog and a work queue.
//
// 500 unread documents is a number nobody can act on. The subset that is unread
// AND has machine-readable text is the list to work today, and it grows on its
// own as raglit transcribes — derived from what is on disk, so no event can be
// missed and nothing needs resyncing.
func TestReadyMarksOnlyDocumentsThatCanBeExtractedNow(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(rel string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("documents/scanned.pdf")                             // no text anywhere
	w("documents/transcribed.pdf")                         // has a raglit transcription
	w("documents/transcribed.pdf.raglit-transcription.md") //
	w("documents/older.pdf")                               // has a legacy sidecar
	w("documents/older.ocr.md")                            //
	w("f.kfacts.md")

	writeLegacyFacts(t, root, "f.kfacts.md",
		"# f\n\n```kfacts\n- id: c\n  claim: x\n  status: asserted\n```\n")
	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	ready := map[string]bool{}
	for _, d := range docs {
		ready[d.Doc] = d.Ready
	}
	if ready["documents/scanned.pdf"] {
		t.Error("a scan with no text is not extractable yet — listing it as ready is a lie")
	}
	if !ready["documents/transcribed.pdf"] {
		t.Error("a document with a raglit transcription is extractable now")
	}
	if !ready["documents/older.pdf"] {
		t.Error("a legacy .ocr.md sidecar also makes a document readable")
	}
}

// A corpus may keep a document and its readable form in sibling directories —
// `correspondence/eml/x.eml` beside `correspondence/text/x.txt` — rather than in
// one place. Without this the same 49 emails counted twice.
//
// Ignoring the text directory would have "fixed" the count by hiding the only
// readable form of every email in the corpus, which is exactly backwards.
func TestSiblingDirectoryTranscriptIsPaired(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"correspondence/eml", "correspondence/text"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	name := "Access Easement - 2022-08-18 1130"
	eml := "correspondence/eml/" + name + ".eml"
	txt := "correspondence/text/" + name + ".txt"
	for _, rel := range []string{eml, txt} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := companions(root, eml); got != txt {
		t.Errorf("the .eml should find its text one directory over, got %q", got)
	}
	if !isTranscriptOf(root, txt) {
		t.Error("the text is the readable form of the .eml, not a second document")
	}
	// And it must not fire when the original is absent — a stray text file is
	// evidence in its own right.
	stray := "correspondence/text/orphan.txt"
	if err := os.WriteFile(filepath.Join(root, stray), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isTranscriptOf(root, stray) {
		t.Error("a text file with no counterpart is not a transcript of anything")
	}
}

// A document the graph CITES is always listed, even when it looks like a
// transcript. Skipping it before checking made it vanish from the walk and get
// reported `missing` — a real gap hidden by a convenience rule. The ignore path
// already guarded this; the transcript path did not.
func TestACitedTranscriptIsStillListed(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"correspondence/eml", "correspondence/text"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"correspondence/eml/note.eml", "correspondence/text/note.txt"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A fact rests on the TEXT form, which is also a transcript of the .eml.
	facts := "# f\n\n```kfacts\n" +
		"- id: s-note\n  document: The note\n  doc: correspondence/text/note.txt\n  class: document\n" +
		"- id: c1\n  claim: Something the note says\n  status: asserted\n  attested_by: s-note\n```\n"
	writeLegacyFacts(t, root, "f.kfacts.md", facts)
	p := loadProject(t, root)
	docs, err := Backlog(root, p.Index, p.Graph)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.Doc == "correspondence/text/note.txt" {
			if d.State == "missing" {
				t.Error("a cited document that exists must never be reported missing")
			}
			return
		}
	}
	t.Error("a cited document must appear in the backlog even if it looks like a transcript")
}

// raglit's WHOLE-FILE transcription wins over its per-page `.ocr.md` when both exist.
//
// Both come from raglit: `.ocr.md` is the per-page fallback for documents too big for
// one request. Not human-versus-machine — two pathways through the same model, and the
// per-page one is the one that pads.
//
// It used to lose, because `.ocr.md` sorted first and `companions` returns the first
// match. Measured on the live corpus before the fix:
// `2021-rrepsa-purchase-sale-agreement.ocr.md` gives the operative record of survey as
// `AF#20140415006`, eleven digits denoting nothing, where raglit's transcription of the
// same PDF has `AF#201503110043` right. The tool whose only job is checking a quotation
// against its source was checking it against invention.
func TestWholeFileTranscriptionBeatsThePerPageOne(t *testing.T) {
	root := t.TempDir()
	rel := "documents/deed.pdf"
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"documents/deed.pdf":                         "%PDF-1.4",
		"documents/deed.ocr.md":                      "AF#20140415006 typed by hand",
		"documents/deed.pdf.raglit-transcription.md": "## Page 1\n\nAF#201503110043",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr, _ := companions(root, rel)
	if !strings.HasSuffix(tr, ".raglit-transcription.md") {
		t.Fatalf("companions chose %q — a hand-written sidecar is overriding raglit's", tr)
	}
	// And the page markers come with it, which is the other half: without them
	// `kg verify` cannot say where in a scan a claim lives.
	b, err := os.ReadFile(filepath.Join(root, tr))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "## Page 1") {
		t.Error("the chosen transcript carries no page markers")
	}
}

// The per-page sidecar is still the fallback. Plenty of documents have one and no
// whole-file pass; demoting it must not mean ignoring it.
func TestThePerPageSidecarIsStillUsedWhenItIsTheOnlyOne(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"documents/deed.pdf":    "%PDF-1.4",
		"documents/deed.ocr.md": "the only transcript there is",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if tr, _ := companions(root, "documents/deed.pdf"); !strings.HasSuffix(tr, ".ocr.md") {
		t.Errorf("companions found %q; the only available transcript must still be used", tr)
	}
}

// `undercut_by` DEFEATS; `contradicts` CONFLICTS. They lower to the same edge
// type, and reading the defeat relation from the wrong end inverted it: the
// undercutter came out disputed by the thing it undercuts.
//
// The corpus case this was found in: a summary-judgment denial read off the
// court's own order carried one `undercut_by` authored on the defence theory the
// order defeats, and rendered `DISPUTED: 1 for (record), 1 against (inference)`.
// A status travels with the claim, so two documents going to counsel hedged a
// court order.
func TestUndercutByIsDirectionalAndContradictsIsNot(t *testing.T) {
	oneWay := "- id: theory\n  claim: The defence reaches only the intentional count\n" +
		"  status: asserted\n  about: thing\n  attested_by: s-stmt\n" +
		"  undercut_by: order\n" +
		"- id: order\n  claim: Summary judgment was denied on the negligent count\n" +
		"  status: asserted\n  about: thing\n  attested_by: s-eob\n"
	g := tensionGraph(t, oneWay)
	if k := g.tension("order", "").Kind; k != "" {
		t.Errorf("the undercutter is not put in question by what it undercuts, got %q: %s",
			k, g.tension("order", "").Label())
	}
	if k := g.tension("theory", "").Kind; k == "" {
		t.Error("the undercut node must still be contested — the edge does something")
	}

	// The symmetric key keeps reading both ways: two claims that cannot both be
	// true each put the other in question.
	both := "- id: theory\n  claim: The defence reaches only the intentional count\n" +
		"  status: asserted\n  about: thing\n  attested_by: s-stmt\n" +
		"  contradicts: order\n" +
		"- id: order\n  claim: Summary judgment was denied on the negligent count\n" +
		"  status: asserted\n  about: thing\n  attested_by: s-eob\n"
	gb := tensionGraph(t, both)
	for _, id := range []string{"order", "theory"} {
		if k := gb.tension(id, "").Kind; k == "" {
			t.Errorf("`contradicts` is symmetric; %s must be contested", id)
		}
	}
}

// A one-way edge is an authored distinction, so switching a relation between the
// two keys has to move the hash — otherwise a change to what the graph asserts
// leaves every document consuming it reporting `fresh`.
func TestOneWayIsInSemHash(t *testing.T) {
	mk := func(key string) string {
		d, diags := parseFixture("f.facts", []byte(
			"```kfacts\n- id: a\n  claim: A\n  status: asserted\n"+
				"- id: b\n  claim: B\n  status: asserted\n  "+key+": a\n```\n"))
		if errs := Errors(diags); len(errs) > 0 {
			t.Fatal(errs)
		}
		g, diags := Build([]*Doc{d})
		if errs := Errors(diags); len(errs) > 0 {
			t.Fatal(errs)
		}
		return SemHash(*g.Nodes["b"], g.incident["b"])
	}
	// `contradicts: a` on b stores a-->b? No: forward, b-->a. `undercut_by: a`
	// stores a-->b. Compare the two shapes that DO share Src/Dst/Type.
	if mk("undercut_by") == mk("contradicts") {
		t.Error("undercut_by and contradicts must not hash alike; the direction is authored")
	}
}

// A source's description is not metadata. It prints into the reference block of
// every document that cites the source, so it is published prose and has to name
// the instrument rather than carry its contents.
func TestALongSourceDescriptionIsFlaggedButAuthorityIsExempt(t *testing.T) {
	long := strings.Repeat("the client's own terms, quoted at length, ", 8)
	src := "```kfacts\n" +
		"- id: thing\n  entity: A parcel\n  status: asserted\n" +
		"- id: s-verbose\n  utterance: " + long + "\n  by: thing\n  class: interested\n" +
		"- id: a-verbose\n  document: " + long + "\n  class: authority\n" +
		"- id: c\n  claim: A claim\n  status: asserted\n  about: thing\n" +
		"  attested_by: [s-verbose, a-verbose]\n```\n"
	d, diags := parseFixture("f.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	_, bd := Build([]*Doc{d})
	var flagged []string
	for _, x := range append(diags, bd...) {
		if strings.Contains(x.Msg, "-character description") {
			flagged = append(flagged, x.Msg)
		}
	}
	if len(flagged) != 1 {
		t.Fatalf("want exactly the evidence source flagged, got %d: %v", len(flagged), flagged)
	}
	if !strings.Contains(flagged[0], "s-verbose") {
		t.Errorf("authority carrying its holding is the description doing its job: %s", flagged[0])
	}
	if !strings.Contains(flagged[0], "`reason:`") {
		t.Errorf("the warning must name where the text belongs instead: %s", flagged[0])
	}
}
