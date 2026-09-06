package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// attestOver records a verdict carrying the passage it was made over, the way
// `kg attest` does.
func attestOver(t *testing.T, g *Graph, set AttestSet, fact, source string) {
	t.Helper()
	n, ok := g.Lookup(fact)
	if !ok {
		t.Fatalf("no fact %q", fact)
	}
	ex := ExcerptOf(n.Body)
	if ex == "" {
		t.Fatalf("%s quotes nothing, so there is no excerpt to attest over", fact)
	}
	set[fact+"\x00"+source] = Attestation{
		Fact: fact, Source: source, Verdict: VConfirmed, By: "carl", At: "2026-09-03",
		Excerpt: ex, ExcerptHash: HashExcerpt(ex),
	}
}

// A VERDICT MUST NOT OUTLIVE THE WORDS IT WAS ABOUT, and the transcription
// moving under it was invisible to everything.
//
// `HashDoc` digests the ORIGINAL FILE's bytes, so a re-OCR of the same scan — a
// better model, another DPI, a retry that returned tidier prose — changes the
// words anybody reads and quotes while leaving the PDF untouched. Source drift
// cannot fire. That is the likeliest way a passage moves and nothing reported it.
func TestARereadDocumentIsCaughtUnderAStandingVerdict(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	set := AttestSet{}
	attestOver(t, p.Graph, set, "c-wrapped", "s-deed")

	// Intact to begin with, or the test proves nothing about the change.
	before := p.Graph.CheckAttestedExcerpts(root, set)
	if len(before) != 1 || before[0].State != ExcerptIntact {
		t.Fatalf("the verdict does not start intact: %+v", before)
	}

	// THE FILE IS NOT TOUCHED. Only its reading is — which is exactly the case
	// no existing check can see.
	if err := os.WriteFile(filepath.Join(root, "documents/deed.ocr.md"),
		[]byte("## Page 1\nRECORD OF DEED\n[FIGURE: the legal description did not survive this pass.]\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	after := p.Graph.CheckAttestedExcerpts(root, set)
	if len(after) != 1 {
		t.Fatalf("got %d checks, want 1", len(after))
	}
	if after[0].State != ExcerptDocMoved {
		t.Fatalf("state is %q, want %q — the words the verdict was given over are gone and the "+
			"document's bytes never changed", after[0].State, ExcerptDocMoved)
	}
	// AND IT IS AN ERROR, because a citation that resolves to words nobody
	// attested is the thing this whole mechanism exists to prevent.
	ds := ExcerptDiags(p.Graph, after)
	if len(ds) != 1 || ds[0].Severity != SevError {
		t.Fatalf("diags are %+v", ds)
	}
	if !strings.Contains(ds[0].Msg, "c-wrapped") || !strings.Contains(ds[0].Msg, "s-deed") {
		t.Errorf("the finding does not name both ends: %s", ds[0].Msg)
	}
	if len(ds[0].Subjects) != 2 {
		t.Errorf("the finding names %v, and an agent needs both the fact and the source", ds[0].Subjects)
	}
}

// THE OTHER DIRECTION: the fact is reworded after attestation.
//
// An attestation is keyed on (fact, source) and survives an edit to either, so a
// quotation changed afterwards carries a verdict nobody gave about those words.
// It is a different problem from a moved transcription and gets a different
// state, for the reason `not-held` and `not-looked` are two states: they send
// somebody to fix opposite ends.
func TestAFactRewordedAfterAttestationIsCaught(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	set := AttestSet{}
	attestOver(t, p.Graph, set, "c-wrapped", "s-deed")

	n, _ := p.Graph.Lookup("c-wrapped")
	n.Body = `The deed says "and between the Northeasterly extensions of both the Northwesterly"`

	got := p.Graph.CheckAttestedExcerpts(root, set)
	if len(got) != 1 || got[0].State != ExcerptFactMoved {
		t.Fatalf("got %+v, want one %s", got, ExcerptFactMoved)
	}
	if !strings.Contains(got[0].Why, "no longer uses") {
		t.Errorf("the reason does not say what changed: %s", got[0].Why)
	}
}

// UNREADABLE IS NOT MOVED. Silence about a document nobody can read is not
// evidence that its words changed, and reporting it as a mismatch sends somebody
// to re-verify a verdict that is fine.
func TestADocumentNothingCanReadIsNotAMismatch(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	set := AttestSet{}
	attestOver(t, p.Graph, set, "c-unreadable", "s-scan")

	got := p.Graph.CheckAttestedExcerpts(root, set)
	if len(got) != 1 || got[0].State != ExcerptUnreadable {
		t.Fatalf("got %+v, want one %s", got, ExcerptUnreadable)
	}
	// A WARNING, NOT AN ERROR, and not silence either: the verdict cannot be
	// checked, which a person should know and should not be paged about.
	ds := ExcerptDiags(p.Graph, got)
	if len(ds) != 1 || ds[0].Severity != SevWarn {
		t.Fatalf("diags are %+v", ds)
	}
}

// AN INTACT VERDICT IS NOT REPORTED. A report that lists what is fine teaches
// people to skim it, which is how the one line that mattered gets skimmed too.
func TestAnIntactExcerptSaysNothing(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	set := AttestSet{}
	attestOver(t, p.Graph, set, "c-wrapped", "s-deed")

	got := p.Graph.CheckAttestedExcerpts(root, set)
	if len(got) != 1 || got[0].State != ExcerptIntact {
		t.Fatalf("got %+v, want one %s", got, ExcerptIntact)
	}
	if ds := ExcerptDiags(p.Graph, got); len(ds) != 0 {
		t.Errorf("an intact verdict produced %d finding(s)", len(ds))
	}
}

// A VERDICT WITH NO EXCERPT IS LEFT ALONE. Every attestation recorded before
// this existed has none, and treating that as a mismatch would report the whole
// backlog as broken on the day the feature landed.
func TestAVerdictWithNoExcerptIsNotAFinding(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	set := AttestSet{"c-wrapped\x00s-deed": {
		Fact: "c-wrapped", Source: "s-deed", Verdict: VConfirmed, By: "carl", At: "2026-09-03",
	}}
	if got := p.Graph.CheckAttestedExcerpts(root, set); len(got) != 0 {
		t.Errorf("an attestation predating excerpts produced %+v", got)
	}
}

// THE EXCERPT IS FOLDED BEFORE IT IS COMPARED. A transcription that rewraps a
// line or straightens a curly quote has not moved the passage, and reporting
// that as a changed document is the false flag that makes a check unusable.
func TestWhitespaceAndPunctuationAreNotAChangedDocument(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)
	set := AttestSet{}
	attestOver(t, p.Graph, set, "c-wrapped", "s-deed")

	src, err := os.ReadFile(filepath.Join(root, "documents/deed.ocr.md"))
	if err != nil {
		t.Fatal(err)
	}
	rewrapped := strings.ReplaceAll(strings.ReplaceAll(string(src), "\n", " "), "  ", " ")
	if err := os.WriteFile(filepath.Join(root, "documents/deed.ocr.md"), []byte(rewrapped), 0o644); err != nil {
		t.Fatal(err)
	}

	got := p.Graph.CheckAttestedExcerpts(root, set)
	if len(got) != 1 || got[0].State != ExcerptIntact {
		t.Fatalf("a rewrapped transcription reported as %+v", got)
	}
}

// AND THE EXISTING CHECKS DO NOT SEE IT, which is the whole reason this one
// exists. Asserted rather than assumed: if source drift ever starts catching a
// re-read, the argument for excerpts is weaker and somebody should know.
func TestSourceDriftCannotSeeARereadDocument(t *testing.T) {
	root := quotesProject(t)
	p := loadProject(t, root)

	// No lock is written on purpose: the point is what drift can SEE, and it
	// compares the document's bytes either way. Start from a clean report.
	p.Graph.CheckSourceDrift(root)
	if len(p.Graph.SourceDrift) != 0 {
		t.Fatalf("the corpus does not start clean: %v", p.Graph.SourceDrift)
	}

	// Replace the READING. The PDF is untouched.
	if err := os.WriteFile(filepath.Join(root, "documents/deed.ocr.md"),
		[]byte("## Page 1\nnothing that was there before.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2 := loadProject(t, root)
	p2.Graph.CheckSourceDrift(root)
	if len(p2.Graph.SourceDrift) != 0 {
		t.Errorf("source drift reported %v for a document whose bytes did not change — if it "+
			"can see a re-read now, this package has two answers to one question",
			p2.Graph.SourceDrift)
	}
}

// QUOTEIN IS THE PRIMITIVE, and it is exported so no consumer reimplements the
// folding. Whether a quotation is present is not a substring test: pleading line
// numbers interleave with the text, transcriptions wrap mid-phrase, and quotes
// and whitespace differ between readings. A second copy of those rules is the
// drift this package tells its consumers to avoid.
func TestQuoteInFoldsBothSidesAndSeparatesUnreadable(t *testing.T) {
	root := quotesProject(t)

	// Wrapped mid-phrase in the transcription — "lines of Lot\n2 of said plat".
	if present, readable := QuoteIn(root, "documents/deed.pdf",
		"extensions of both the Northwesterly and Southeasterly lines of Lot 2 of said plat"); !present || !readable {
		t.Errorf("a quote wrapped across a line read as present=%v readable=%v", present, readable)
	}
	// Genuinely absent: the transcription dropped the legal description.
	if present, readable := QuoteIn(root, "documents/survey.pdf",
		"THAT LIES WESTERLY OF THE CENTERLINE OF SAID RIGHT-OF-WAY"); present || !readable {
		t.Errorf("a dropped passage read as present=%v readable=%v", present, readable)
	}
	// UNREADABLE IS NOT ABSENT. Reporting a scan nobody has read as a miscitation
	// is the false flag that makes the check unusable.
	if _, readable := QuoteIn(root, "documents/scan.pdf", "anything at all"); readable {
		t.Error("a document with no transcription reported as readable")
	}
}
