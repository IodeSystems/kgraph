package kgraph

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// acceptCorpus builds a corpus with one unreferenced source, which is the
// commonest finding in the real one — 407 of 615.
func acceptCorpus(t *testing.T) string {
	t.Helper()
	// A SUBDIRECTORY, never the root — see newCorpus for why that matters.
	root := newCorpus(t, "demo")
	if err := os.WriteFile(filepath.Join(root, corpusDir("demo"), "spare.md"),
		[]byte("a statute\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, root, corpusDir("demo"),
		"- id: s-spare\n  document: A statute nothing cites yet\n  doc: spare.md\n  class: authority\n"+
			"- id: c-one\n  claim: Something else entirely\n  status: asserted\n")
	return root
}

func findingOf(t *testing.T, root, check string) (Diag, *Project) {
	t.Helper()
	p, diags, err := LoadInWith(root, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Check == check {
			return d, p
		}
	}
	t.Fatalf("no %s finding in the fixture — this test is checking nothing", check)
	return Diag{}, nil
}

// AN ACCEPTANCE IS OF A SITUATION, NOT OF A MESSAGE.
//
// The key carries the sem_hash of every node the finding names, so editing one
// of those facts yields a different key and the finding comes back on its own.
// That is what makes accepting safe to have at all: there is no separate
// staleness check to forget, and no way to sign off a warning and then quietly
// change what it was about.
func TestAnAcceptanceDiesWhenTheFactItNamesChanges(t *testing.T) {
	root := acceptCorpus(t)
	d, _ := findingOf(t, root, CheckUnreferencedSource)

	if err := AppendAccepted(root, corpusDir("demo"), Accepted{
		Key: d.Key, Check: d.Check, Msg: d.Msg, By: "carl", At: "2026-09-01"}); err != nil {
		t.Fatal(err)
	}
	p, diags, err := LoadInWith(root, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	open, accepted := p.Triage(diags)
	if len(accepted) != 1 {
		t.Fatalf("the acceptance did not suppress its finding: %d accepted", len(accepted))
	}
	for _, o := range open {
		if o.Check == CheckUnreferencedSource {
			t.Fatal("the finding is still outstanding")
		}
	}

	// Now change the fact the finding names. The ruling was about the OLD one.
	st := storeFor(t, root)
	if _, err := Amend(st, legal, "s-spare",
		map[string]any{"document": "A statute nothing cites yet — reworded"},
		"carl", "the description changed"); err != nil {
		t.Fatal(err)
	}
	if err := exportStore(t, st, root, corpusDir("demo")); err != nil {
		t.Fatal(err)
	}
	p2, diags2, err := LoadInWith(root, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	open2, accepted2 := p2.Triage(diags2)
	if len(accepted2) != 0 {
		t.Errorf("a stale acceptance survived an edit to the fact it named")
	}
	var back bool
	for _, o := range open2 {
		if o.Check == CheckUnreferencedSource {
			back = true
		}
	}
	if !back {
		t.Error("the finding did not come back after the fact changed")
	}
}

// ONLY A PERSON MAY ACCEPT. An agent that can silence the corpus's own
// complaints is the hazard the human-in-the-loop design exists to prevent, so a
// machine signature is kept, REPORTED, and never honoured — exactly what
// `Attestation.Subtracts()` does for `unsupported`, one level up.
func TestAMachineAcceptanceIsKeptAndNeverHonoured(t *testing.T) {
	root := acceptCorpus(t)
	d, _ := findingOf(t, root, CheckUnreferencedSource)
	if err := AppendAccepted(root, corpusDir("demo"), Accepted{
		Key: d.Key, Check: d.Check, Msg: d.Msg, By: "agent-triage", At: "2026-09-01"}); err != nil {
		t.Fatal(err)
	}
	p2, diags, err := LoadInWith(root, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, accepted := p2.Triage(diags)
	if len(accepted) != 0 {
		t.Error("a machine silenced a finding")
	}
	// Kept and reportable, not dropped: the operator has to be able to see that
	// something tried.
	if un := p2.Accepted.Unhonoured(); len(un) != 1 || un[0].By != "agent-triage" {
		t.Errorf("the machine acceptance was not reported: %+v", un)
	}
}

// Errors are never acceptable. A dangling reference gets fixed; letting one be
// signed off would make the one severity that must be trusted negotiable.
func TestAnErrorCannotBeAccepted(t *testing.T) {
	set := AcceptedSet{"k": Accepted{Key: "k", By: "carl"}}
	open, accepted := set.Partition([]Diag{
		{Severity: SevError, Check: "x", Key: "k", Msg: "a dangling reference"},
	})
	if len(accepted) != 0 || len(open) != 1 {
		t.Fatal("an error was accepted")
	}
}

// An unsigned acceptance says nobody looked, so it is refused at the door.
func TestAnUnsignedAcceptanceIsRefused(t *testing.T) {
	root := t.TempDir()
	err := AppendAccepted(root, "", Accepted{Key: "k", Check: "x"})
	if err == nil {
		t.Fatal("an unsigned acceptance was recorded")
	}
	if !strings.Contains(err.Error(), "who is accepting") {
		t.Errorf("the refusal should say what is missing: %v", err)
	}
}

// EVERY CHECK NAME THE CODE EMITS MUST BE IN THE REGISTRY.
//
// Same shape and same reason as `TestInstallableNamesAreAllDispatched`: a check
// that emits a name the registry does not know cannot be tuned by a rule and
// cannot be accepted by key with a meaningful label — and nothing would say so.
// The registry is also what an error message lists when a rule is mistyped, so a
// missing entry makes that message wrong too.
func TestEveryEmittedCheckNameIsRegistered(t *testing.T) {
	emitted := map[string]bool{}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// The CLI emits one too — `empty-query`, because only it holds the resolved
	// declarations — and a registry test that scanned only this package would
	// call it unemitted and demand its removal.
	cli, err := filepath.Glob("cmd/kg/*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, cli...)
	re := regexp.MustCompile(`Check:\s*(?:kgraph\.)?(Check[A-Za-z]+)`)
	byConst := map[string]string{
		"CheckUnreferencedSource": CheckUnreferencedSource,
		"CheckSplitContradiction": CheckSplitContradiction,
		"CheckGraphShrank":        CheckGraphShrank,
		"CheckNearDuplicate":      CheckNearDuplicate,
		"CheckSameFileSources":    CheckSameFileSources,
		"CheckUnsourcedFact":      CheckUnsourcedFact,
		"CheckLongDescription":    CheckLongDescription,
		"CheckInterestedNoBy":     CheckInterestedNoBy,
		"CheckInferenceNoPremise": CheckInferenceNoPremise,
		"CheckAlreadyHeld":        CheckAlreadyHeld,
		"CheckAliasAmbiguous":     CheckAliasAmbiguous,
		"CheckEmptyQuery":         CheckEmptyQuery,
		"CheckAttestedExcerpt":    CheckAttestedExcerpt,
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			name, ok := byConst[m[1]]
			if !ok {
				t.Errorf("%s emits %s, which this test does not know — add it to byConst "+
					"and to knownChecks", f, m[1])
				continue
			}
			emitted[name] = true
		}
	}
	if len(emitted) == 0 {
		t.Fatal("no check names found in the source — this test is checking nothing")
	}
	for name := range emitted {
		if _, ok := knownChecks[name]; !ok {
			t.Errorf("check %q is emitted but not registered — no rule can tune it", name)
		}
	}
	// And the other direction: a registered check nothing emits is a rule an
	// author can write that will never match anything.
	for name := range knownChecks {
		if !emitted[name] {
			t.Errorf("check %q is registered but nothing emits it", name)
		}
	}
}

// THE FAILURE THIS EXISTS FOR, reproduced.
//
// The driving corpus was dark for weeks: the parser was deleted, its fact files
// stopped being read, every load built an empty graph, and every command
// reported success over it. Nothing was broken enough to fail. The plan carried
// a confident "750 nodes · 0 errors" the whole time.
func TestACorpusThatStopsLoadingIsAnErrorNotASilentSuccess(t *testing.T) {
	root := t.TempDir()
	writeFactLog(t, root, "",
		"- id: c-one\n  claim: A fact\n  status: asserted\n"+
			"- id: c-two\n  claim: Another\n  status: asserted\n")
	g, _, err := ScanFactsIn(root, DefaultIndex)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteHighWater(root, "", g, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	// Before: a healthy corpus says nothing.
	hw, _ := ReadHighWater(root, "")
	if hw.Nodes != len(g.Nodes) {
		t.Fatalf("the mark did not record: %+v", hw)
	}
	if ds := g.CheckHighWater(hw); len(ds) != 0 {
		t.Fatalf("a healthy corpus must be silent: %v", ds)
	}

	// Now the corpus stops being read — the store moves, the index is wrong, a
	// migration never ran. The graph is EMPTY and nothing else has failed.
	empty := &Graph{Nodes: map[string]*Node{}}
	ds := empty.CheckHighWater(hw)
	if len(ds) != 1 || ds[0].Severity != SevError {
		t.Fatalf("an empty load must be an ERROR, got %v", ds)
	}
	// NOT TUNABLE AND NOT ACCEPTABLE. Every other check describes the corpus;
	// this one says the corpus is not being read, so every other answer in the
	// run is worthless. A `Check` would make it silenceable by a rule, and a
	// `Key` would make it acceptable — it must be neither.
	if ds[0].Check != "" || ds[0].Key != "" {
		t.Error("the empty-graph error must not be silenceable or acceptable")
	}
	// And it must say what to actually do.
	for _, want := range []string{"ZERO", "store", "--index", highWaterName} {
		if !strings.Contains(ds[0].Msg, want) {
			t.Errorf("the message must mention %q: %s", want, ds[0].Msg)
		}
	}

	// A partial drop is a WARNING: retiring duplicates and dropping a theory both
	// do this honestly.
	half := &Graph{Nodes: map[string]*Node{"c-one": {ID: "c-one"}}}
	half.Nodes = map[string]*Node{}
	small := HighWater{Nodes: 100, At: "2026-09-01"}
	ds = (&Graph{Nodes: map[string]*Node{"a": {}, "b": {}}}).CheckHighWater(small)
	if len(ds) != 1 || ds[0].Severity != SevWarn || ds[0].Check != CheckGraphShrank {
		t.Fatalf("a partial drop must be a tunable warning, got %v", ds)
	}
	_ = half
}

// The mark only ever goes UP. A count that could fall would record the collapse
// it is meant to report, one run later, and then agree with it forever.
func TestTheWatermarkNeverFalls(t *testing.T) {
	root := t.TempDir()
	big := &Graph{Nodes: map[string]*Node{"a": {}, "b": {}, "c": {}}}
	if err := WriteHighWater(root, "", big, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	small := &Graph{Nodes: map[string]*Node{"a": {}}}
	if err := WriteHighWater(root, "", small, "2026-09-02"); err != nil {
		t.Fatal(err)
	}
	hw, _ := ReadHighWater(root, "")
	if hw.Nodes != 3 {
		t.Errorf("the watermark fell to %d — a shrunken corpus would then agree with itself forever", hw.Nodes)
	}
}

// THE LIVENESS CHECK MUST FIND ITS MARK WHEN THE GRAPH IS EMPTY — which is the
// only time it matters.
//
// `IndexDirOf` derives the directory from the nodes' file paths, so an index
// that loaded NOTHING reported the repo root and the watermark beside its marker
// was never found. Measured before the fix: a genuinely dark corpus scanned
// clean at 0 nodes and exited 0, which is the exact failure the check exists to
// catch, reproduced by the check itself.
func TestLivenessFindsItsMarkFromTheMarkerNotTheGraph(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "matter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kg-index"), []byte("acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A mark beside the marker, and no facts at all: the dark state.
	if err := os.WriteFile(filepath.Join(dir, highWaterName),
		[]byte(`{"nodes":2129,"edges":3866,"at":"2026-09-01"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	home, err := IndexHomeOf(root, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if home != "matter" {
		t.Fatalf("the index home resolved to %q, so the mark is looked for in the wrong place", home)
	}
	_, diags, err := LoadInWith(root, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(Errors(diags)) == 0 {
		t.Fatal("a dark corpus scanned clean — the liveness check did not find its mark")
	}
}

// A KEYED FINDING NAMES ITS SUBJECTS.
//
// `findingKey` takes the node ids and hashes them into an opaque key, so every
// keyed diagnostic already knew which nodes it was about and told nobody. That
// cost a consumer: caselit's `already-held` findings came back with an empty
// subject — ten of them on a real matter — so the question's id existed only
// inside the prose message and an agent had to regex it out. Parsing another
// package's message format is the coupling that project refuses, which is why
// the fix belongs here.
//
// The pairing is what this pins. A Key computed from ids and no Subjects is the
// same information thrown away again, and it would be thrown away silently.
func TestAKeyedFindingNamesItsSubjects(t *testing.T) {
	var checked int
	for _, index := range []string{"fence-dispute", "clinic-billing"} {
		p, diags, err := LoadIn("examples", index)
		if err != nil {
			t.Fatalf("%s: %v", index, err)
		}
		diags = append(diags, p.Graph.CheckAlreadyHeld("examples", index)...)
		for _, d := range diags {
			if d.Key == "" {
				continue
			}
			checked++
			if len(d.Subjects) == 0 {
				t.Errorf("%s: %q carries a Key and names no subject — the ids went into the "+
					"hash and nowhere else:\n  %s", index, d.Check, d.Msg)
			}
			for _, id := range d.Subjects {
				if _, ok := p.Graph.Lookup(id); !ok {
					t.Errorf("%s: %q names subject %q, which is not a node", index, d.Check, id)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no keyed finding in either corpus; this proves nothing")
	}
}
