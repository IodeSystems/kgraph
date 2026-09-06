package kgraph

import (
	"testing"
)

// Source qualifiers must survive whatever order they are written in. The body
// key used to replace the Source wholesale, so `by:` above `record:` was dropped
// without a diagnostic — the fact parsed, validated and queried cleanly, and
// simply had no speaker. A silently missing attribution in an evidence graph is
// worse than a parse error.
func TestSourceQualifiersSurviveKeyOrder(t *testing.T) {
	dir := t.TempDir()
	writeFactLog(t, dir, "",
		"- id: s-before\n  by: vole\n  class: record\n  record: Declaration\n"+
			"- id: s-after\n  record: Declaration\n  by: vole\n  class: record\n")
	g, _, err := ScanFacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"s-before", "s-after"} {
		n, ok := g.Nodes[id]
		if !ok || n.Source == nil {
			t.Fatalf("%s did not parse as a source", id)
		}
		if n.Source.Speaker != "vole" {
			t.Errorf("%s lost its speaker to key order: %+v", id, n.Source)
		}
		if n.Source.Class != "record" {
			t.Errorf("%s lost its class to key order: %+v", id, n.Source)
		}
	}
}
