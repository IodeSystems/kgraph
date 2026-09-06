package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func attestCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "documents/deed.md"), []byte("a deed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, root, "",
		"- id: s-deed\n  record: The deed\n  doc: documents/deed.md\n  class: record\n"+
			"- id: s-other\n  record: Another record\n  doc: documents/deed.md\n  class: record\n"+
			"- id: c-one\n  claim: The strip is held by Halloway\n  status: asserted\n  attested_by: s-deed\n"+
			"- id: c-two\n  claim: Something with two citations\n  status: asserted\n  attested_by: [s-deed, s-other]\n")
	return root
}

// The whole point of `unsupported`: the document is SILENT, not contradicting.
// The fact may be true and hung on the wrong exhibit, so it must survive — but it
// loses that citation and falls back to unattested, which is the existing rule
// doing the work rather than a new status being invented.
func TestUnsupportedKillsTheCitationNotTheFact(t *testing.T) {
	root := attestCorpus(t)
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VUnsupported, By: "carl", Note: "this page is about the septic, not the strip"}); err != nil {
		t.Fatal(err)
	}
	p, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := p.Graph.Lookup("c-one")
	if !ok {
		t.Fatal("the fact was deleted; a miscitation is not a refutation")
	}
	if n.Status != SAsserted {
		t.Errorf("the fact's own status must not move, got %q", n.Status)
	}
	var warned bool
	for _, d := range diags {
		if d.Severity == SevWarn && contains([]string{d.Msg}, d.Msg) &&
			d.Msg != "" && n.ID == "c-one" {
			if containsSub(d.Msg, "unsupported") && containsSub(d.Msg, "c-one") {
				warned = true
			}
		}
	}
	if !warned {
		t.Error("a fact whose only citation was ruled unsupported must be reported as unattested")
	}
}

// A fact with two citations keeps standing when only one is ruled out. The
// verdict is per EDGE, which is why the key is (fact, source).
func TestOneBadCitationDoesNotUnseatAFactWithAnother(t *testing.T) {
	root := attestCorpus(t)
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-two", Source: "s-deed", Verdict: VUnsupported, By: "carl"}); err != nil {
		t.Fatal(err)
	}
	_, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if containsSub(d.Msg, "c-two") && containsSub(d.Msg, "rests on no source") {
			t.Fatalf("c-two still has s-other and must not be reported unattested: %s", d.Msg)
		}
	}
}

// `illegible` is a verdict on the SCAN. Treating it as disproof would let a bad
// photocopy silently strip a document of its evidence.
func TestIllegibleDoesNotSubtractEvidence(t *testing.T) {
	root := attestCorpus(t)
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VIllegible, By: "carl"}); err != nil {
		t.Fatal(err)
	}
	_, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if containsSub(d.Msg, "c-one") && containsSub(d.Msg, "rests on no source") {
			t.Fatal("an unreadable scan is not a refutation")
		}
	}
}

// Last line wins, so a verdict can be revised without rewriting the file — which
// is what makes append-only safe under Syncthing.
func TestLaterVerdictSupersedesEarlier(t *testing.T) {
	root := attestCorpus(t)
	for _, v := range []Verdict{VUnsupported, VConfirmed} {
		if err := AppendAttestation(root, "", Attestation{
			Fact: "c-one", Source: "s-deed", Verdict: v, By: "carl"}); err != nil {
			t.Fatal(err)
		}
	}
	att, err := ReadAttestations(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := att.Of("c-one", "s-deed"); a.Verdict != VConfirmed {
		t.Fatalf("the later verdict must win, got %q", a.Verdict)
	}
	if !att.Supports("c-one", "s-deed") {
		t.Error("a reinstated citation must count again")
	}
}

// A nil set means NOT LOADED, not "nothing ruled". Answering false there would
// let any caller that skipped the sidecar silently strip every fact of evidence.
func TestUnloadedAttestationsNeverSubtract(t *testing.T) {
	var none AttestSet
	if !none.Supports("c-one", "s-deed") {
		t.Fatal("an unloaded attestation set must not disbelieve anything")
	}
}

func containsSub(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfSub(s, sub) >= 0)
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// The "error if used materially" line. Unattested is a warning in a working
// graph — most corpora carry a backlog of unsourced facts and blocking on those
// would block every render. This is narrower: the fact HAD citations, a person
// opened the documents, and none of them say it. Publishing that is a different
// act from leaving it in the graph.
func TestMiscitedFactIsRefusedAtRenderButOthersAreNot(t *testing.T) {
	root := attestCorpus(t)
	// A declared query over every asserted claim. A standing query since
	// 2026-09-01; it was a spec file, and the declaration is all this test ever
	// needed from one.
	if err := os.WriteFile(filepath.Join(root, standingName),
		[]byte("- name: all\n  query: claim[status=asserted] sort id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{Now: "2026-07-28"}

	// Before any verdict, nothing is refused.
	p := loadProject(t, root)
	pins, err := p.Graph.Resolve(p.DeclaredSets()[0], env)
	if err != nil {
		t.Fatal(err)
	}
	if bad := p.Graph.Miscited(pins); len(bad) != 0 {
		t.Fatalf("nothing has been ruled on yet: %v", bad)
	}

	// c-one's only citation is ruled unsupported; c-two keeps s-other.
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VUnsupported, By: "carl"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-two", Source: "s-deed", Verdict: VUnsupported, By: "carl"}); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	pins, err = p.Graph.Resolve(p.DeclaredSets()[0], env)
	if err != nil {
		t.Fatal(err)
	}
	bad := p.Graph.Miscited(pins)
	if len(bad) != 1 || bad[0] != "c-one" {
		t.Fatalf("only the fact with NO surviving citation may be refused, got %v", bad)
	}
}

// A fact that never had a citation at all is a warning, not a render error —
// otherwise a corpus with a normal unsourced backlog could never generate
// anything. The escalation is about a verdict, not about absence.
func TestNeverSourcedFactDoesNotBlockRender(t *testing.T) {
	root := t.TempDir()
	writeFactLog(t, root, ".",
		"- id: c-bare\n  claim: Nobody has sourced this yet\n  status: asserted\n")
	if err := os.WriteFile(filepath.Join(root, standingName),
		[]byte("- name: all\n  query: claim[status=asserted]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := loadProject(t, root)
	pins, err := p.Graph.Resolve(p.DeclaredSets()[0], Env{Now: "2026-07-28"})
	if err != nil {
		t.Fatal(err)
	}
	if bad := p.Graph.Miscited(pins); len(bad) != 0 {
		t.Fatalf("an unsourced fact is a warning, not a publish error: %v", bad)
	}
}

// `by` is optional (USER). An unsigned verdict is accepted and then treated
// exactly like a machine one: it stays on the human checklist, because if nobody
// said who checked it, nobody checked it. The safe default is "still needs you".
func TestUnsignedVerdictIsAcceptedButStaysPending(t *testing.T) {
	root := attestCorpus(t)
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VAttested}); err != nil {
		t.Fatalf("an unsigned verdict must be accepted: %v", err)
	}
	p := loadProject(t, root)
	var found bool
	for _, a := range p.Graph.PendingHuman() {
		if a.Fact == "c-one" && a.Source == "s-deed" {
			found = true
		}
	}
	if !found {
		t.Error("an unsigned verdict must stay on the human checklist")
	}
}

// The checklist is the artefact the work is for. A machine reading is a
// candidate, not a verification — good enough to work from, not good enough to
// file on — so it stays listed until a person signs it off, and a human verdict
// takes it off.
func TestPendingHumanIsTheChecklist(t *testing.T) {
	root := attestCorpus(t)
	p := loadProject(t, root)
	// Nothing verified yet: every attests edge is pending.
	if n := len(p.Graph.PendingHuman()); n != 3 {
		t.Fatalf("every unverified citation must be pending, got %d", n)
	}
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VAttested, By: "agent-vision"}); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	if n := len(p.Graph.PendingHuman()); n != 3 {
		t.Fatalf("a machine reading does not discharge the checklist, got %d", n)
	}
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VAttested, By: "Carl Taylor"}); err != nil {
		t.Fatal(err)
	}
	p = loadProject(t, root)
	for _, a := range p.Graph.PendingHuman() {
		if a.Fact == "c-one" && a.Source == "s-deed" {
			t.Error("a human sign-off must take the citation off the checklist")
		}
	}
}

// A machine identity answers a narrower question than a person does: OCR can say
// a string is absent from a text layer, not that a document fails to support a
// proposition. The predicate exists so a reader can tell which they are looking
// at; it deliberately carries no rank.
func TestMachineAndHumanVerifiersAreDistinguishable(t *testing.T) {
	for by, machine := range map[string]bool{
		"ocr-transcript": true, "raglit": true, "text-layer": true,
		"Carl Taylor": false, "carl": false,
	} {
		if got := (Attestation{By: by}).MachineAttested(); got != machine {
			t.Errorf("%q: machine=%v, want %v", by, got, machine)
		}
	}
}

// THE OLD SPELLINGS STILL READ, AND NOTHING DOWNSTREAM LEARNS THEY EXISTED.
//
// The vocabulary converged onto raglit/attest's — `attested` became `confirmed`,
// `illegible` became `unclear` — and the logs it has to keep reading are
// APPEND-ONLY by design, which is how two syncing machines merge without losing a
// verdict. Rewriting them to change a spelling would be the one edit the format
// exists to forbid, so a reader normalises instead. A live corpus had 14
// `attested` lines when this landed.
func TestARetiredSpellingStillResolves(t *testing.T) {
	root := attestCorpus(t)
	// Written by hand in the OLD vocabulary, exactly as a file synced from a
	// machine that predates the convergence would arrive.
	old := `{"fact":"c-one","source":"s-deed","verdict":"attested","by":"carl"}
{"fact":"c-two","source":"s-deed","verdict":"illegible","by":"carl"}
`
	if err := os.WriteFile(filepath.Join(root, attestName), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	att, err := ReadAttestations(root, "")
	if err != nil {
		t.Fatalf("a log in the old vocabulary must still load: %v", err)
	}
	if a, _ := att.Of("c-one", "s-deed"); a.Verdict != VConfirmed {
		t.Errorf("`attested` must resolve as %q, got %q — a consumer switching on the\n"+
			"    verdict would silently miss every pre-convergence ruling", VConfirmed, a.Verdict)
	}
	if a, _ := att.Of("c-two", "s-deed"); a.Verdict != VUnclear {
		t.Errorf("`illegible` must resolve as %q, got %q", VUnclear, a.Verdict)
	}
	// And `unclear` still does not subtract: failing to read a scan is a fact
	// about the scan, not disproof of what it holds.
	if !att.Supports("c-two", "s-deed") {
		t.Error("`unclear` must not strip a document of its evidence")
	}
}

// A caller holding the old constant must not be able to put the old word back in
// the file. The migration is one-way or it is not a migration.
func TestWritingTheOldConstantLandsTheNewWord(t *testing.T) {
	root := attestCorpus(t)
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VAttested, By: "carl"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, attestName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"attested"`) {
		t.Errorf("the retired spelling reached the file:\n%s", b)
	}
	if !strings.Contains(string(b), `"confirmed"`) {
		t.Errorf("the converged word is not in the file:\n%s", b)
	}
}

// THE GUARD. A machine cannot rule a document silent.
//
// Absence from a text layer is not absence from the document, so a scan with no
// text layer would "prove" every fact it attests unsupported — and `Miscited` is
// an ERROR at render and attach, so one bad OCR pass could strip a corpus of its
// evidence and then refuse to file. Pinned from both ends: the write is refused,
// and a line already in an append-only log is not honoured.
func TestMachineMayNotRuleUnsupported(t *testing.T) {
	root := attestCorpus(t)
	for _, by := range []string{"ocr-transcript", "raglit", "agent-vision", ""} {
		err := AppendAttestation(root, "", Attestation{
			Fact: "c-one", Source: "s-deed", Verdict: VUnsupported, By: by})
		if err == nil {
			t.Fatalf("by=%q: a machine or unsigned `unsupported` must be refused — "+
				"it is the one verdict that removes evidence", by)
		}
		if !containsSub(err.Error(), "unsupported") {
			t.Errorf("by=%q: the error must name the verdict it refuses, got %q", by, err)
		}
	}
	// Omitting the signature is the obvious bypass, and it must not work: an
	// unsigned verdict is not thereby a person's.
	if err := AppendAttestation(root, "", Attestation{
		Fact: "c-one", Source: "s-deed", Verdict: VUnsupported, By: "carl"}); err != nil {
		t.Fatalf("a signed person must still be able to rule `unsupported`: %v", err)
	}
	att, err := ReadAttestations(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if att.Supports("c-one", "s-deed") {
		t.Fatal("a PERSON's `unsupported` must still kill the citation — the guard is " +
			"about who ruled, not about disabling the verdict")
	}
}

// A log written before the guard existed, or synced from a machine still running
// an old binary, cannot be edited out: the format is append-only on purpose. So
// the ruling is ignored — and saying so is the point, because a sidecar that
// reads as "this citation is dead" while the graph goes on counting it is two
// records disagreeing with nobody reconciling them.
func TestRecordedMachineUnsupportedIsIgnoredAndReported(t *testing.T) {
	root := attestCorpus(t)
	log := `{"fact":"c-one","source":"s-deed","verdict":"unsupported","by":"ocr-transcript"}
{"fact":"c-two","source":"s-other","verdict":"unsupported"}
`
	if err := os.WriteFile(filepath.Join(root, attestName), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	p, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Graph.Attest.Supports("c-one", "s-deed") {
		t.Error("a machine's recorded `unsupported` must not subtract")
	}
	if !p.Graph.Attest.Supports("c-two", "s-other") {
		t.Error("an unsigned recorded `unsupported` must not subtract")
	}
	// It is kept and listed — the machine reading is still the candidate a person
	// works from, and dropping it would discard real work.
	if n := len(p.Graph.Attest.Unsupported()); n != 2 {
		t.Errorf("both rulings must stay in the set, got %d", n)
	}
	var warned int
	for _, d := range diags {
		if d.Severity == SevWarn && containsSub(d.Msg, "IGNORED") {
			warned++
		}
		if d.Severity == SevError {
			t.Errorf("an unhonoured verdict must not stop the corpus building: %s", d.Msg)
		}
	}
	if warned != 2 {
		t.Errorf("each ignored ruling must be reported, got %d warnings", warned)
	}
	// And the fact is NOT reported as miscited — that error is reserved for a
	// person's finding, and reaching it by machine is the whole failure.
	for _, id := range p.Graph.Miscited(map[string]*Pin{
		"d": {Nodes: []string{"c-one", "c-two"}},
	}) {
		t.Errorf("%s was ruled miscited on a machine verdict", id)
	}
}

// THE QUEUE IS RANKED BY CONSEQUENCE, and the ordering is the product.
//
// `--used` was the axis this replaces and it had stopped discriminating: 1,106
// of 1,197 on the live corpus, because 289 standing queries between them cover
// nearly every fact. The 14 citations where a machine read something the
// document does not say — the exact case the human-in-the-loop design exists
// for — were at 1.2% density inside it.
func TestPendingIsRankedByConsequence(t *testing.T) {
	root := t.TempDir()
	writeFactLog(t, root, "",
		"- id: s-a\n  document: A\n  doc: a.md\n  class: document\n"+
			"- id: s-b\n  document: B\n  doc: b.md\n  class: document\n"+
			// contested: two claims that contradict each other
			"- id: c-hot\n  claim: The gate was open\n  status: asserted\n  attested_by: s-a\n"+
			"  contradicts: c-cold\n"+
			"- id: c-cold\n  claim: The gate was locked\n  status: asserted\n  attested_by: s-b\n"+
			// sole citation, and a query resolves it
			"- id: c-sole\n  claim: Rests on one document\n  status: asserted\n  attested_by: s-a\n"+
			// sole citation, nothing asks about it
			"- id: c-quiet\n  claim: Nobody asks about this\n  status: asserted\n  attested_by: s-b\n")
	g, _, err := ScanFactsIn(root, DefaultIndex)
	if err != nil {
		t.Fatal(err)
	}
	pending := g.PendingHuman()
	if len(pending) == 0 {
		t.Fatal("nothing pending — this test is checking nothing")
	}
	absent := map[string]bool{"c-quiet\x00s-b": true}
	used := map[string][]string{"c-sole": {"a-query"}, "c-hot": {"a-query"}}

	ranked := g.RankPending(pending, absent, used)
	band := map[string]int{}
	for _, r := range ranked {
		band[r.Fact] = r.Band
	}
	// An absent quote outranks everything, INCLUDING the fact being otherwise
	// unremarkable: either the passage was dropped in transcription or the
	// citation is on the wrong document, and neither is visible from the fact.
	if band["c-quiet"] != BandAbsentQuote {
		t.Errorf("an absent quote is band %d, want %d", band["c-quiet"], BandAbsentQuote)
	}
	if band["c-hot"] != BandContested {
		t.Errorf("a contested fact is band %d, want %d", band["c-hot"], BandContested)
	}
	if band["c-sole"] != BandSoleAndUsed {
		t.Errorf("a queried single-cited fact is band %d, want %d", band["c-sole"], BandSoleAndUsed)
	}
	// SORTED, and totally: a queue that reorders between runs cannot be worked
	// down a screen at a time.
	for i := 1; i < len(ranked); i++ {
		if ranked[i-1].Band > ranked[i].Band {
			t.Fatalf("the queue is not in band order at %d", i)
		}
	}

	// `sole` ranks BELOW `used`, and that was set from measurement: placed second
	// it swallowed two thirds of the live queue, because most facts there have
	// exactly one citation.
	if BandSole < BandUsed {
		t.Error("a band that holds most of the queue must not outrank a narrower one")
	}
	if Urgent != BandContested {
		t.Errorf("the default cut moved to %d — it is set from what a wrong verdict "+
			"CHANGES, not from the shape of the list", Urgent)
	}
}

// A HUMAN SURFACE MUST NOT DEFAULT TO A MACHINE SIGNATURE.
//
// `LastSigner` is "who wrote the last line", and one agent run is enough to make
// that a machine. Every caller reaching for a default signature wants "who is
// the person working this corpus", and the difference is not cosmetic: a verdict
// signed by a machine is re-queued by `PendingHuman`, so a person's whole
// session of rulings comes straight back as pending.
func TestLastHumanSignerSkipsMachineIdentities(t *testing.T) {
	root := t.TempDir()
	dir := "idx"
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Attestation{
		{Fact: "f1", Source: "s1", Verdict: VConfirmed, By: "dana@example.test"},
		{Fact: "f2", Source: "s1", Verdict: VConfirmed, By: "agent-vision"},
		{Fact: "f3", Source: "s1", Verdict: VConfirmed, By: "ocr-transcript"},
	} {
		if err := AppendAttestation(root, dir, a); err != nil {
			t.Fatal(err)
		}
	}
	if got := LastSigner(root, dir); got != "ocr-transcript" {
		t.Fatalf("fixture is not exercising the case: LastSigner = %q, want the machine", got)
	}
	if got := LastHumanSigner(root, dir); got != "dana@example.test" {
		t.Errorf("LastHumanSigner = %q, want the person behind the machine runs", got)
	}
}

// The workbench refuses to start rather than discard the session it is about to
// collect. Signing every verdict with a machine identity is not a lesser
// recording, it is no recording: `PendingHuman` hands each one straight back.
func TestVerifyWorkbenchRefusesAMachineSigner(t *testing.T) {
	err := ServeVerify(t.TempDir(), "idx", "127.0.0.1:0", "agent-vision", StoreRef{DSN: StoreNone})
	if err == nil {
		t.Fatal("the workbench started signing as a machine")
	}
	if !strings.Contains(err.Error(), "agent-vision") || !strings.Contains(err.Error(), "--by") {
		t.Errorf("the refusal does not say who or what to do: %v", err)
	}
}
