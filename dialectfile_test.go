package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDialect puts one authored dialect in a corpus.
func writeDialect(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, dialectsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const engagement = `ladder:
  - written-confirmation
  - measured
  - meeting-note
  - verbal
  - assumed
governs: signed
invalidated: unconfirmed
speaker_required: [verbal, meeting-note]
`

// The point of the whole change: a corpus writes its own vocabulary and an index
// binds it. Strongest-first in the file, so the ladder reads the way somebody
// would say it out loud.
func TestAnAuthoredDialectLoadsAndAnIndexBindsIt(t *testing.T) {
	root := t.TempDir()
	writeDialect(t, root, "engagement.yaml", engagement)
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "acme", ".kg-index"),
		[]byte("index: acme\ndialect: engagement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := DialectAt(root, filepath.Join(root, "acme"))
	if err != nil {
		t.Fatalf("an authored dialect must resolve: %v", err)
	}
	if d.Name() != "engagement" {
		t.Fatalf("bound the wrong dialect: %q", d.Name())
	}
	// Strongest first in the file means highest rank in the ladder.
	top, ok := d.Rank("written-confirmation")
	if !ok {
		t.Fatal("the first rung must be ranked")
	}
	bottom, ok := d.Rank("assumed")
	if !ok {
		t.Fatal("the last rung must be ranked")
	}
	if top <= bottom {
		t.Fatalf("the ladder is inverted: written-confirmation=%d assumed=%d", top, bottom)
	}
	// The governing class is unranked BY CONSTRUCTION — that absence is what lets
	// it support a claim without winning or losing an evidentiary contest.
	if _, ranked := d.Rank(d.Governs()); ranked {
		t.Error("the governing class must have no rank")
	}
	if !d.Valid("signed") || !d.Valid("measured") || d.Valid("record") {
		t.Error("Valid must accept this dialect's ladder plus its governing class, and nothing else")
	}
	if !d.SpeakerRequired("verbal") || d.SpeakerRequired("measured") {
		t.Error("speaker_required did not reach the dialect")
	}
	if d.Invalidated() != "unconfirmed" {
		t.Errorf("the invalidation flag is the dialect's word, got %q", d.Invalidated())
	}
}

// A name means one thing. Otherwise `legal` depends on which checkout you are
// in, and a document's ladder is not a thing that may vary by clone.
func TestAnAuthoredDialectMayNotShadowABuiltIn(t *testing.T) {
	root := t.TempDir()
	writeDialect(t, root, "legal.yaml", engagement)
	_, err := LoadDialects(root)
	if err == nil {
		t.Fatal("a file redefining a built-in must be refused")
	}
	if !strings.Contains(err.Error(), "legal") || !strings.Contains(err.Error(), "built-in") {
		t.Errorf("the refusal must name the collision, got %q", err)
	}
}

// Every guard the built-ins get at init() must run over a file too. These are
// the checkable failures — the ones closing the map never actually checked.
func TestAnAuthoredDialectIsValidated(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{{
		name: "duplicate rung",
		body: "ladder: [a, b, a]\ngoverns: g\ninvalidated: x\n",
		want: "twice in the ladder",
	}, {
		name: "governing class on the ladder",
		body: "ladder: [a, g]\ngoverns: g\ninvalidated: x\n",
		want: "no rank by construction",
	}, {
		name: "no ladder",
		body: "governs: g\ninvalidated: x\n",
		want: "no ladder",
	}, {
		name: "no governing class",
		body: "ladder: [a, b]\ninvalidated: x\n",
		want: "no governing class",
	}, {
		name: "no invalidation flag",
		body: "ladder: [a, b]\ngoverns: g\n",
		want: "no invalidation flag",
	}, {
		name: "speaker required off the ladder",
		body: "ladder: [a, b]\ngoverns: g\ninvalidated: x\nspeaker_required: [zzz]\n",
		want: "not on its ladder",
	}, {
		name: "redefines a core relation",
		body: "ladder: [a]\ngoverns: g\ninvalidated: x\nedges:\n  supersedes: superseded by\n",
		want: "redefines the core relation",
	}, {
		name: "subtype over a non-core kind",
		body: "ladder: [a]\ngoverns: g\ninvalidated: x\nsubtypes:\n  widget:\n    kind: gadget\n",
		want: "not a core kind",
	}, {
		name: "subtype governs a core scalar",
		body: "ladder: [a]\ngoverns: g\ninvalidated: x\nsubtypes:\n  widget:\n    kind: claim\n    keys: [status]\n",
		want: "core scalar field",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseDialectFile(".kgraph/dialects/d.yaml", []byte(c.body))
			if err == nil {
				t.Fatalf("%s must be refused", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error must say why; want %q in %q", c.want, err)
			}
			// And it must say WHICH file. A dialect is loaded far from where its
			// effect is felt.
			if !strings.Contains(err.Error(), "d.yaml") {
				t.Errorf("the error must name the file, got %q", err)
			}
		})
	}
}

// Two places to say one thing, so disagreeing is an error rather than a
// precedence rule somebody has to remember.
func TestADialectFileNameIsItsName(t *testing.T) {
	if _, err := parseDialectFile(".kgraph/dialects/engagement.yaml",
		[]byte("name: clinical\n"+engagement)); err == nil {
		t.Fatal("a `name:` disagreeing with the file must be refused")
	}
	d, err := parseDialectFile(".kgraph/dialects/engagement.yaml",
		[]byte("name: engagement\n"+engagement))
	if err != nil {
		t.Fatalf("a `name:` that agrees is fine: %v", err)
	}
	if d.Name() != "engagement" {
		t.Fatalf("got %q", d.Name())
	}
}

// An unknown name must be refused rather than defaulted, and the refusal has to
// list the corpus's OWN dialects — otherwise it tells somebody their dialect
// does not exist while it sits in the directory next to the one they edited.
func TestAnUnknownDialectNamesWhatWouldHaveBeenAccepted(t *testing.T) {
	root := t.TempDir()
	writeDialect(t, root, "engagement.yaml", engagement)
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "acme", ".kg-index"),
		[]byte("index: acme\ndialect: maritime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := DialectAt(root, filepath.Join(root, "acme"))
	if err == nil {
		t.Fatal("an unknown dialect must be refused, not defaulted to legal")
	}
	if !strings.Contains(err.Error(), "legal") || !strings.Contains(err.Error(), "engagement") {
		t.Errorf("the refusal must list built-in AND authored dialects, got %q", err)
	}
}

// The staleness contract, from both sides.
//
// A BUILT-IN hashes to the empty string, so a corpus that declares no dialect
// records exactly as it did before this existed — otherwise every document
// already rendered would report `dialect-changed` on the next status, which is
// the false-flag the staleness rules exist to prevent.
//
// An AUTHORED one hashes, so editing the ladder flags the documents written
// under it. The ladder is not the prompt, but it decides which of two
// conflicting sources wins and how the document says so.
func TestEditingAnAuthoredDialectFlagsDocumentsAndABuiltInDoesNot(t *testing.T) {
	if h := DialectHash(legal); h != "" {
		t.Fatalf("a built-in must hash to the empty string, got %q", h)
	}
	d, err := parseDialectFile(".kgraph/dialects/engagement.yaml", []byte(engagement))
	if err != nil {
		t.Fatal(err)
	}
	if DialectHash(d) == "" {
		t.Fatal("an authored dialect must hash, or editing it can never be noticed")
	}
	edited, err := parseDialectFile(".kgraph/dialects/engagement.yaml",
		[]byte(strings.Replace(engagement, "  - measured\n", "", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if DialectHash(edited) == DialectHash(d) {
		t.Fatal("removing a rung must change the hash — that edit reorders the ladder")
	}

	// End to end, on a STANDING QUERY. It was a document attach until render
	// retired; the dependency is the same and the carrier changed. An edited
	// ladder can change which rows a `[disputed]` query returns while the query
	// text and every fact stand still, so the answer records what it was resolved
	// under.
	_, g := standingCorpus(t, false)
	g.SetDialect(d)
	st := &Standing{Name: "held", Query: "claim[status=asserted]"}
	a := askOnce(t, g, st)
	st.Acknowledge(a, "2026-08-29", "carl")
	if st.DialectHash == "" {
		t.Fatal("the acknowledged answer did not record the dialect it was resolved under")
	}
	if a2 := askOnce(t, g, st); a2.Stale() {
		t.Fatalf("nothing moved: %v", a2.Drift)
	}
	g.SetDialect(edited)
	if a3 := askOnce(t, g, st); !contains(a3.Drift, "dialect-changed") {
		t.Fatalf("want dialect-changed after moving a rung, got %v", a3.Drift)
	}
}

// A RULE NAMING NO CHECK IS REFUSED AT LOAD.
//
// The failure it prevents is silence, not error: a mistyped rule does nothing
// at all, and "I turned that check off" becomes a belief rather than a fact.
// Same shape as a mistyped hook name, which has its own test for the same
// reason.
func TestARuleNamingAnUnknownCheckIsRefused(t *testing.T) {
	_, err := parseDialectFile("engineering.yaml", []byte(
		"ladder: [measured, reported]\nrules:\n  unreferenced-sauce: off\n"))
	if err == nil {
		t.Fatal("a rule naming no check was accepted")
	}
	// The message must say what WOULD be accepted, or the author's next move is
	// to guess.
	if !strings.Contains(err.Error(), "unreferenced-source") {
		t.Errorf("the refusal must list the known checks: %v", err)
	}
	if _, err := parseDialectFile("engineering.yaml", []byte(
		"ladder: [measured]\nrules:\n  unreferenced-source: quiet\n")); err == nil {
		t.Fatal("an unknown level was accepted")
	}
}

// `off` is the CLASS-LEVEL door, and it exists so the findings ledger is not
// used as one: accepting 407 findings individually is absurd, and accepting
// them as a class is disabling the check — which belongs in the index's own
// declaration where a reader sees it.
func TestRuleLevelsSilenceAndRaise(t *testing.T) {
	d, err := parseDialectFile("engineering.yaml", []byte(
		"ladder: [measured, reported]\ngoverns: standard\ninvalidated: retracted\nrules:\n"+
			"  unreferenced-source: off\n  split-contradiction: error\n"))
	if err != nil {
		t.Fatal(err)
	}
	in := []Diag{
		{Severity: SevWarn, Check: CheckUnreferencedSource, Msg: "silenced"},
		{Severity: SevWarn, Check: CheckSplitContradiction, Msg: "raised"},
		{Severity: SevWarn, Msg: "no check — always shown"},
		{Severity: SevError, Check: CheckUnreferencedSource, Msg: "an error"},
	}
	out := d.ApplyRules(in)
	if len(out) != 3 {
		t.Fatalf("want 3 survivors, got %d: %+v", len(out), out)
	}
	if out[0].Severity != SevError || out[0].Msg != "raised" {
		t.Errorf("the raised check is %v", out[0])
	}
	if out[1].Msg != "no check — always shown" {
		t.Error("a diagnostic with no check must never be silenced by a rule")
	}
	// AN ERROR IS NEVER LOWERED. `off` on a check that emitted an error must not
	// let an index declare its way out of it — the severity that must be trusted
	// stays non-negotiable, which is the same line the ledger draws.
	if out[2].Severity != SevError {
		t.Error("a rule silenced an ERROR")
	}
}
