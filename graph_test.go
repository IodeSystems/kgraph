package kgraph

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The corpus must not contain one fact under two ids. This is the failure the
// format is least able to absorb: nothing looks wrong, both copies render, and
// answering one leaves the other open.
func TestNoDuplicateFactsInCorpus(t *testing.T) {
	g := fixtureGraph(t)
	if d := g.checkDuplicates(); len(d) != 0 {
		for _, x := range d {
			t.Errorf("%v", x)
		}
	}
}

// A claim and the rebuttal that shares its whole vocabulary are a DESIGNED pair,
// not an accident. If the near-duplicate check reported those, its output would
// be all noise and the real duplicates would be invisible in it — so the check
// is worth nothing without this property.
func TestDeliberateNearPairsAreNotFlagged(t *testing.T) {
	g := fixtureGraph(t)
	pairs := [][2]string{
		// Near-identical wording, deliberately: two readings of the same measurement
		// that differ only in the number. They are related by `contradicts`, and
		// that edge is the only thing keeping the duplicate detector quiet.
		{"strip-width-is-16ft", "strip-width-is-20ft"},
	}
	for _, p := range pairs {
		a, aok := g.Lookup(p[0])
		b, bok := g.Lookup(p[1])
		if !aok || !bok {
			t.Fatalf("fixture pair missing: %v", p)
		}
		if !g.related(p[0], p[1]) {
			t.Fatalf("%s and %s must be related for the exemption to apply", p[0], p[1])
		}
		// The exemption must be load-bearing, i.e. the pair must actually score
		// high enough that only the edge keeps it quiet. Otherwise this proves
		// nothing about the threshold.
		if s := jaccard(bodyTokens(a.Body), bodyTokens(b.Body)); s < 0.3 {
			t.Logf("%s/%s score %.2f — below the interesting range", p[0], p[1], s)
		}
	}
}

// The near-duplicate threshold has to separate the real duplicate this check was
// written for from the highest-scoring intentional pair. If those ever cross,
// the constant is wrong and no amount of exemptions will save it.
func TestNearDuplicateThresholdSeparatesRealFromDesigned(t *testing.T) {
	dup := jaccard(
		bodyTokens("Which retained lot is the barn on?"),
		bodyTokens("Which retained lot is the barn actually on?"))
	designed := jaccard(
		bodyTokens("Wren swore the driveway and property corners were not clearly visible"),
		bodyTokens("The driveway and the property corners were clearly visible, and the deeded 25-ft strip bisects the driveway at its true location"))
	if dup < nearDuplicate {
		t.Errorf("the duplicate this check exists for scores %.2f, under the %.2f threshold", dup, nearDuplicate)
	}
	if designed >= nearDuplicate {
		t.Errorf("a designed rebuttal pair scores %.2f, at or over the %.2f threshold", designed, nearDuplicate)
	}
}

// CID is the content address of the FACT, not of the file it was read from.
// Including the document would give the same claim reached from two sources two
// different CIDs, defeating exactly the merge the CID exists to find.
func TestCIDIsIndependentOfSourceAndFormatting(t *testing.T) {
	a := CID(KClaim, "The strip is held by Halloway by recorded fee deed")
	b := CID(KClaim, "  the STRIP is held by Halloway by recorded fee deed.  ")
	if a != b {
		t.Error("case, whitespace and trailing punctuation must not change a CID")
	}
	if a == CID(KQuestion, "The strip is held by Halloway by recorded fee deed") {
		t.Error("kind must be part of the content address")
	}
}

// A `same_as` merge must move the survivor's sem_hash — it absorbed relations,
// which is a real change to what the fact rests on — while leaving every
// reference to the retired id resolvable.
func TestSameAsMergesAndKeepsTheRetiredIdResolvable(t *testing.T) {
	base := "```kfacts\n" +
		"- id: s1\n  record: A record\n  class: record\n" +
		"- id: keep\n  claim: The barn is on Lot G\n  status: asserted\n" +
		"- id: other\n  claim: Something else entirely\n  status: asserted\n" +
		"```\n"
	d0, diags := parseFixture("a.facts", []byte(base))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	before, _ := Build([]*Doc{d0})
	baseHash := before.SemHash["keep"]

	merged := "```kfacts\n" +
		"- id: s1\n  record: A record\n  class: record\n" +
		"- id: keep\n  claim: The barn is on Lot G\n  status: asserted\n" +
		"- id: other\n  claim: Something else entirely\n  status: asserted\n" +
		"- id: dup\n  same_as: keep\n  claim: The barn is on Lot G\n  status: asserted\n" +
		"  attested_by: s1\n  supports: other\n" +
		"```\n"
	d1, diags := parseFixture("a.facts", []byte(merged))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d1})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, live := g.Nodes["dup"]; live {
		t.Error("a retired id must not remain a node of its own")
	}
	n, ok := g.Lookup("dup")
	if !ok || n.ID != "keep" {
		t.Fatalf("a retired id must still resolve, got %v %v", n, ok)
	}
	if id, err := g.ResolveRef("dup", ""); err != nil || id != "keep" {
		t.Fatalf("a query naming a retired id must find the fact: %q %v", id, err)
	}
	if g.SemHash["keep"] == baseHash {
		t.Error("absorbing relations must move the survivor's sem_hash")
	}
	var supports int
	for _, e := range g.Edges {
		if e.Src == "keep" && e.Dst == "other" && e.Type == ESupports {
			supports++
		}
		if e.Src == "dup" || e.Dst == "dup" {
			t.Errorf("edge still names the retired id: %+v", e)
		}
	}
	if supports != 1 {
		t.Errorf("the absorbed relation must survive exactly once, got %d", supports)
	}
}

// Merging must not manufacture diagnostics nobody could act on: the pair's own
// relation to each other becomes a self-edge, and a source both copies cited
// becomes a duplicate edge. dedupeEdges reports duplicates as an authoring
// mistake, and here no author made one.
func TestMergeArtifactsAreNotReportedAsMistakes(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s1\n  record: A record\n  class: record\n" +
		"- id: keep\n  claim: One fact\n  status: asserted\n  attested_by: s1\n" +
		"- id: dup\n  same_as: keep\n  claim: One fact\n  status: asserted\n" +
		"  attested_by: s1\n  supports: keep\n" +
		"```\n"
	d, diags := parseFixture("a.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, diags := Build([]*Doc{d})
	for _, x := range diags {
		t.Errorf("merge produced a diagnostic: %v", x)
	}
	for _, e := range g.Edges {
		if e.Src == e.Dst {
			t.Errorf("self-edge survived the merge: %+v", e)
		}
	}
	var attests int
	for _, e := range g.Edges {
		if e.Type == EAttests {
			attests++
		}
	}
	if attests != 1 {
		t.Errorf("the shared source must collapse to one edge, got %d", attests)
	}
}

// `same_as` across kinds is not a merge, it is a mistake, and folding a question
// into an event would erase the question.
func TestSameAsAcrossKindsIsRejected(t *testing.T) {
	src := "```kfacts\n" +
		"- id: e1\n  event: A thing happened\n  at: 2026-01-01\n  status: asserted\n" +
		"- id: q1\n  same_as: e1\n  question: A thing happened\n  status: open\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	g, diags := Build([]*Doc{d})
	var found bool
	for _, x := range Errors(diags) {
		if strings.Contains(x.Msg, "different kinds") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a kind-mismatch error, got %v", diags)
	}
	if _, ok := g.Nodes["q1"]; !ok {
		t.Error("a rejected merge must leave the node in place, not swallow it")
	}
}

func TestSameAsCycleIsRejected(t *testing.T) {
	src := "```kfacts\n" +
		"- id: a\n  same_as: b\n  claim: X\n  status: asserted\n" +
		"- id: b\n  same_as: a\n  claim: X\n  status: asserted\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	_, diags := Build([]*Doc{d})
	var found bool
	for _, x := range Errors(diags) {
		if strings.Contains(x.Msg, "cycle") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a cycle error, got %v", diags)
	}
}

// A chain must land on the end of the chain, not one hop along it, or the
// survivor is whichever node the map happened to visit first.
func TestSameAsChainResolvesToTheEnd(t *testing.T) {
	src := "```kfacts\n" +
		"- id: a\n  claim: X\n  status: asserted\n" +
		"- id: b\n  same_as: a\n  claim: X\n  status: asserted\n" +
		"- id: c\n  same_as: b\n  claim: X\n  status: asserted\n" +
		"- id: d\n  claim: Y\n  status: asserted\n  supports: c\n" +
		"```\n"
	doc, _ := parseFixture("a.facts", []byte(src))
	g, diags := Build([]*Doc{doc})
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("want a and d only, got %d nodes", len(g.Nodes))
	}
	for _, ref := range []string{"b", "c"} {
		if id, err := g.ResolveRef(ref, ""); err != nil || id != "a" {
			t.Errorf("%s must resolve to a, got %q %v", ref, id, err)
		}
	}
	for _, e := range g.Edges {
		if e.Type == ESupports && e.Dst != "a" {
			t.Errorf("a relation through a chain must land on the survivor: %+v", e)
		}
	}
}

// TestNoDuplicateFactsInCorpus passing proves nothing unless the check can still
// fire. A detector that silently stopped detecting reads exactly like a clean
// corpus.
func TestDuplicateCheckStillFires(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{{
		"exact",
		"- id: a\n  question: Which retained lot is the barn on?\n  status: open\n" +
			"- id: b\n  question: which RETAINED lot is the barn on\n  status: open\n",
		"same fact entered 2 times",
	}, {
		"near",
		"- id: a\n  question: Which retained lot is the barn on?\n  status: open\n" +
			"- id: b\n  question: Which retained lot is the barn actually on?\n  status: open\n",
		"may be the same question entered twice",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, _ := parseFixture("a.facts", []byte("```kfacts\n"+c.src+"```\n"))
			g, _ := Build([]*Doc{d})
			var found bool
			for _, x := range g.checkDuplicates() {
				if strings.Contains(x.Msg, c.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %q, got %v", c.want, g.checkDuplicates())
			}
		})
	}
}

// Relating two near-duplicates must silence the warning — that is the escape
// hatch the message offers, and if it does not work the check is unactionable
// noise on any corpus with intentional near-pairs.
func TestRelatingNearDuplicatesSilencesTheWarning(t *testing.T) {
	src := "```kfacts\n" +
		"- id: a\n  question: Which retained lot is the barn on?\n  status: open\n" +
		"- id: b\n  question: Which retained lot is the barn actually on?\n  status: open\n" +
		"  supersedes: a\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	if d := g.checkDuplicates(); len(d) != 0 {
		t.Fatalf("an explicit relation must silence the warning, got %v", d)
	}
}

// Unbounded, this pass produced eight million warnings in 28s and 4GB on 4000
// near-identical facts — not an exotic corpus, just line items differing by a
// number. It must stay fast, stay quiet, and admit what it skipped.
func TestNearDuplicatePassIsBoundedAndSaysSo(t *testing.T) {
	var b strings.Builder
	b.WriteString("```kfacts\n")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "- id: n%d\n  claim: The deed strip parcel is held by Halloway variant%d\n  status: asserted\n", i, i)
	}
	b.WriteString("```\n")
	d, _ := parseFixture("big.facts", []byte(b.String()))
	g, _ := Build([]*Doc{d})

	start := time.Now()
	diags := g.checkDuplicates()
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("the bounded pass took %s", el)
	}
	if len(diags) > 50 {
		t.Errorf("%d diagnostics — one per node is unreadable and buries real findings", len(diags))
	}
	var admitted bool
	for _, x := range diags {
		if strings.Contains(x.Msg, "did not compare everything") {
			admitted = true
		}
	}
	if !admitted {
		t.Error("coverage was bounded and the scan did not say so — a silent cap reads as a clean corpus")
	}
}

// A node resembling hundreds of others is ONE thing to look at. Listing every
// pair is how a real duplicate elsewhere in the output becomes invisible.
func TestNearDuplicatesReportOnePairPerNode(t *testing.T) {
	var b strings.Builder
	b.WriteString("```kfacts\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, "- id: n%d\n  claim: Which retained lot is the barn on number%d\n  status: asserted\n", i, i)
	}
	b.WriteString("```\n")
	d, _ := parseFixture("a.facts", []byte(b.String()))
	g, _ := Build([]*Doc{d})
	diags := g.checkDuplicates()
	// 8 mutually similar nodes are 28 pairs. Only each node's closest match is
	// reported, so the list is about one per node rather than one per pair.
	if len(diags) > 8 {
		t.Errorf("8 mutually similar nodes produced %d diagnostics, want <= 8 (28 pairs exist)", len(diags))
	}
	if len(diags) == 0 {
		t.Error("mutually similar nodes must still be reported at least once")
	}
}

// The rarest-token index is what makes the bucketing sound. Indexing under
// common tokens instead would put a duplicate in a bucket too large to compare,
// where the size bound would then skip it.
func TestDuplicateFoundDespiteACommonVocabulary(t *testing.T) {
	var b strings.Builder
	b.WriteString("```kfacts\n")
	words := []string{"deed", "strip", "parcel", "driveway", "easement", "survey"}
	for i := 0; i < 1200; i++ {
		fmt.Fprintf(&b, "- id: n%d\n  claim: The %s at position %d\n  status: asserted\n",
			i, strings.Join(words, " "), i)
	}
	fmt.Fprintf(&b, "- id: twin-a\n  claim: Which retained lot holds the barn\n  status: asserted\n")
	fmt.Fprintf(&b, "- id: twin-b\n  claim: Which retained lot holds the barn now\n  status: asserted\n")
	b.WriteString("```\n")
	d, _ := parseFixture("a.facts", []byte(b.String()))
	g, _ := Build([]*Doc{d})
	var found bool
	for _, x := range g.checkDuplicates() {
		if strings.Contains(x.Msg, "twin-a") && strings.Contains(x.Msg, "twin-b") {
			found = true
		}
	}
	if !found {
		t.Error("a duplicate buried in a corpus of common words was missed")
	}
}

// Bounding the comparisons does not bound the output: a few hundred mutually
// similar facts fit inside maxBucket. The list has to be capped too, and the
// remainder counted.
func TestNearDuplicateReportsAreCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("```kfacts\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "- id: n%d\n  claim: Which retained lot is the barn on number%d\n  status: asserted\n", i, i)
	}
	b.WriteString("```\n")
	d, _ := parseFixture("a.facts", []byte(b.String()))
	g, _ := Build([]*Doc{d})
	diags := g.checkDuplicates()
	var pairs, notice int
	for _, x := range diags {
		switch {
		case strings.Contains(x.Msg, "not listed"):
			notice++
		case strings.Contains(x.Msg, "may be the same"):
			pairs++
		}
	}
	if pairs > maxNearReports {
		t.Errorf("listed %d pairs, cap is %d", pairs, maxNearReports)
	}
	if notice != 1 {
		t.Errorf("the unlisted remainder must be counted, got %d notices", notice)
	}
}

// A conclusion is not exempt from needing a source. That was my error: I read the
// unattested set as mostly legitimate because `defense-intact` and `root-cause`
// are reasoning rather than things read off a page. But a reasoned conclusion has
// premises, and a legal one has authority — which is exactly what `inference` and
// `derived_from` record, and what clinic-billing already did while fence-dispute did
// not.
func TestUnattestedFactsAreReported(t *testing.T) {
	src := "```kfacts\n" +
		"- id: thing\n  entity: A parcel\n  status: asserted\n" +
		"- id: s-deed\n  record: The deed\n  class: record\n" +
		"- id: sourced\n  claim: Read from the deed\n  status: asserted\n" +
		"  about: thing\n  attested_by: s-deed\n" +
		"- id: concluded\n  claim: Therefore we win\n  status: asserted\n" +
		"  about: thing\n" +
		"- id: abandoned\n  claim: We were wrong\n  status: withdrawn\n  about: thing\n" +
		"- id: strawman\n  claim: Tested and rejected\n  status: false\n  about: thing\n" +
		"```\n"
	d, _ := parseFixture("f.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	got := map[string]bool{}
	for _, x := range g.checkAttestation() {
		for _, id := range []string{"sourced", "concluded", "abandoned", "strawman"} {
			if strings.Contains(x.Msg, "\""+id+"\"") {
				got[id] = true
			}
		}
	}
	if !got["concluded"] {
		t.Error("an unattested conclusion was not reported")
	}
	if got["sourced"] {
		t.Error("an attested fact was reported")
	}
	// A withdrawn fact is one we already know we got wrong; a `false` one is kept
	// only so `prohibits` has a target. Demanding provenance for either is
	// bookkeeping about abandoned work.
	for _, id := range []string{"abandoned", "strawman"} {
		if got[id] {
			t.Errorf("%s is not live and must not be nagged", id)
		}
	}
}

// The suggestion has to name the premises the graph already knows, or the fix is
// guesswork and the warning gets ignored.
func TestUnattestedSuggestionNamesKnownPremises(t *testing.T) {
	src := "```kfacts\n" +
		"- id: thing\n  entity: A parcel\n  status: asserted\n" +
		"- id: s-deed\n  record: The deed\n  class: record\n" +
		"- id: p1\n  claim: Premise one\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-deed\n  supports: concluded\n" +
		"- id: p2\n  claim: Premise two\n  status: asserted\n  about: thing\n" +
		"  attested_by: s-deed\n  causes: concluded\n" +
		"- id: concluded\n  claim: Therefore we win\n  status: asserted\n  about: thing\n" +
		"```\n"
	d, _ := parseFixture("f.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	var msg string
	for _, x := range g.checkAttestation() {
		if strings.Contains(x.Msg, "\"concluded\"") {
			msg = x.Msg
		}
	}
	if !strings.Contains(msg, "derived_from: [p1, p2]") {
		t.Errorf("the suggestion does not name the premises already in the graph: %s", msg)
	}
}

// Recording a conclusion's premises is what lets source drift reach it: a moved
// exhibit two hops under a conclusion must flag the document that renders the
// conclusion, not stop at the premise.
func TestRecordedReasoningExtendsDriftReach(t *testing.T) {
	g := fixtureGraph(t)
	g.SourceDrift = map[string]SourceState{}
	for id, n := range g.Nodes {
		if n.Kind == KSource {
			g.SourceDrift[id] = SrcChanged
		}
	}
	var reach, total int
	for id, n := range g.Nodes {
		if n.Kind != KClaim && n.Kind != KEvent {
			continue
		}
		total++
		if len(g.DriftedSources(id)) > 0 {
			reach++
		}
	}
	// A ratio, not a count: the property is "almost nothing is unattested", and an
	// absolute floor only states that for the corpus it was written against. Before
	// the reasoning was recorded this was 106/172 — 62%. Every unattested fact is a
	// fact no exhibit change can ever flag.
	if total == 0 || reach*100 < total*70 {
		t.Errorf("only %d of %d facts are reachable from any source", reach, total)
	}
}

// The exact half of duplicate detection: two sources naming one file.
//
// The fuzzy half cannot reach this shape. Measured over the fence-dispute corpus, the
// real duplicates score 0.50 to 0.62 against a 0.72 threshold, because a corpus
// that names sources by what they ARE describes one instrument two ways and two
// descriptions of one thing share about half their words. The `doc:` path is not
// a similarity score, so this check has no threshold in it.
func TestTwoSourcesOnOneFileAreReported(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s-order\n  record: Order granting partial summary judgment\n  class: record\n  doc: court/44-order.pdf\n" +
		"- id: s-order-full\n  record: Order granting in part and denying in part\n  class: record\n  doc: court/44-order.pdf\n" +
		"```\n"
	d, diags := parseFixture("a.facts", []byte(src))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	g, _ := Build([]*Doc{d})

	// It is invisible to the similarity pass, which is the whole reason it exists.
	for _, x := range g.checkDuplicates() {
		if strings.Contains(x.Msg, "s-order") {
			t.Fatalf("the near-duplicate pass was not supposed to see this: %q", x.Msg)
		}
	}

	got := g.checkSameDocument()
	if len(got) != 1 {
		t.Fatalf("want one diagnostic, got %v", got)
	}
	if got[0].Severity != SevWarn {
		t.Errorf("this is a warning, not an error — several instruments genuinely live in one PDF")
	}
	for _, want := range []string{"court/44-order.pdf", "s-order", "s-order-full", "`anchor:`", "`same_as:`"} {
		if !strings.Contains(got[0].Msg, want) {
			t.Errorf("message does not name %q: %q", want, got[0].Msg)
		}
	}
}

// The corpus's own idiom for two instruments in one document, used correctly, is
// not a finding. A declaration and its exhibits genuinely share a PDF.
func TestDistinctAnchorsOnOneFileAreSilent(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s-decl\n  document: The declaration\n  class: interested\n  by: e1\n  doc: court/decl.pdf\n  anchor: pages 1-8\n" +
		"- id: s-exhibit-b\n  record: Exhibit B, the certification\n  class: record\n  doc: court/decl.pdf\n  anchor: pages 29-33\n" +
		"- id: e1\n  entity: A declarant\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	if got := g.checkSameDocument(); len(got) != 0 {
		t.Fatalf("both are anchored and they point at different pages: %v", got)
	}
}

// Two entries on one file that disagree about what the file IS are not a merge
// waiting to happen. `record` says the corpus holds the instrument; `document`
// says it holds a copy. Retiring either id picks one answer silently.
func TestDisagreeingFormsOnOneFileSayMergeIsNotTheAnswer(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s-docket-entries\n  record: Register of actions\n  class: record\n  doc: court/register.md\n" +
		"- id: s-docket-register\n  document: Docket register and ordered filings\n  class: record\n  doc: court/register.md\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	got := g.checkSameDocument()
	if len(got) != 1 {
		t.Fatalf("want one diagnostic, got %v", got)
	}
	if !strings.Contains(got[0].Msg, "disagree about what the file is") {
		t.Errorf("the form disagreement is the point and the message does not carry it: %q", got[0].Msg)
	}
	if !strings.Contains(got[0].Msg, "document vs record") {
		t.Errorf("both forms have to be named: %q", got[0].Msg)
	}
}

// Anchors that are present and identical tell the two entries apart no better
// than no anchors at all.
func TestSameAnchorTwiceOnOneFileIsReported(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s-a\n  record: One reading\n  class: record\n  doc: r/x.pdf\n  anchor: page 2\n" +
		"- id: s-b\n  record: Another reading\n  class: record\n  doc: r/x.pdf\n  anchor: page 2\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	got := g.checkSameDocument()
	if len(got) != 1 || !strings.Contains(got[0].Msg, "same `anchor:`") {
		t.Fatalf("want the identical-anchor message, got %v", got)
	}
}

// A source with no `doc:` is a citation rather than a held file, and any number
// of those may coexist.
func TestSourcesWithoutADocAreNotCompared(t *testing.T) {
	src := "```kfacts\n" +
		"- id: s-a\n  record: Cited only\n  class: record\n" +
		"- id: s-b\n  record: Also cited only\n  class: record\n" +
		"```\n"
	d, _ := parseFixture("a.facts", []byte(src))
	g, _ := Build([]*Doc{d})
	if got := g.checkSameDocument(); len(got) != 0 {
		t.Fatalf("neither names a file: %v", got)
	}
}
