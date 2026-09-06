package kgraph

import (
	"os"
	"path/filepath"
	"testing"
)

// A withdrawn claim is exempt from the miscited refusal.
//
// The guard stops a fact resting on nothing from being ASSERTED into a filing.
// A withdrawn claim is rendered as retracted, which is the opposite act — and
// the commonest reason to withdraw one is precisely that a person opened its
// documents and found they do not say it. Without the exemption a failed
// attestation permanently blocks the `revised:` sections that exist to show the
// correction, so the graph could never report its own corrections.
func TestWithdrawnClaimIsExemptFromMiscited(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "documents/deed.md"), []byte("a deed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFactLog(t, root, "",
		"- id: s-deed\n  record: The deed\n  doc: documents/deed.md\n  class: record\n"+
			"- id: c-live\n  claim: Still asserted, cites the deed\n  status: asserted\n  attested_by: s-deed\n"+
			"- id: c-gone\n  claim: Retracted, cited the deed\n  status: withdrawn\n  attested_by: s-deed\n")
	if err := os.WriteFile(filepath.Join(root, standingName),
		[]byte("- name: all\n  query: claim sort id\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Both claims lose their only citation to the same verdict.
	for _, id := range []string{"c-live", "c-gone"} {
		if err := AppendAttestation(root, "", Attestation{
			Fact: id, Source: "s-deed", Verdict: VUnsupported, By: "carl"}); err != nil {
			t.Fatal(err)
		}
	}

	p := loadProject(t, root)
	pins, err := p.Graph.Resolve(p.DeclaredSets()[0], Env{Now: "2026-07-28"})
	if err != nil {
		t.Fatal(err)
	}
	bad := p.Graph.Miscited(pins)
	if len(bad) != 1 || bad[0] != "c-live" {
		t.Fatalf("only the ASSERTED claim should be refused, got %v", bad)
	}
}
