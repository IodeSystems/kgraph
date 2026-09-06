package kgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// synthesize builds a corpus matching a profile's SHAPE, with invented content.
//
// The live corpus cannot be committed — it is live legal and medical work and
// every id and description names a party. Its proportions can, and those are
// what the checkers actually behave differently under: 58% of sources cited by
// nothing, 713 of 894 facts resting on exactly one citation, 98 contradictions.
// `examples/` has none of those ratios at any scale.
//
// Deterministic: same profile, same corpus, so a failure is reproducible.
func synthesize(t *testing.T, root string, p Profile) {
	t.Helper()
	dir := filepath.Join(root, "projects", "synth")
	if err := os.MkdirAll(filepath.Join(dir, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, IndexMarker), []byte("index: synth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	n := 0
	line := func(id string, fields map[string]any) {
		n++
		rec := map[string]any{"op": "assert", "id": id, "fields": fields,
			"by": "carl", "at": fmt.Sprintf("2026-01-01T00:%02d:%02dZ", n/60%60, n%60)}
		j, _ := json.Marshal(rec)
		b.Write(j)
		b.WriteByte('\n')
	}

	// Sources, in the profile's class mix. Each gets a real file so drift and
	// quote checks have something to read.
	classes := make([]string, 0, len(p.SourceClasses))
	for c := range p.SourceClasses {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	var sources []string
	for _, c := range classes {
		for i := 0; i < p.SourceClasses[c]; i++ {
			id := fmt.Sprintf("s-%s-%03d", c, i)
			rel := filepath.Join("documents", id+".md")
			if err := os.WriteFile(filepath.Join(dir, rel),
				[]byte("body of "+id+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			line(id, map[string]any{
				"document": fmt.Sprintf("Instrument %s number %d", c, i),
				"doc":      filepath.ToSlash(rel), "class": c})
			sources = append(sources, id)
		}
	}
	if len(sources) == 0 {
		t.Fatal("the profile declares no sources — nothing to attach facts to")
	}

	// Claims, in the profile's CITATION DISTRIBUTION. Sources are handed out from
	// the front, so the tail of the list stays uncited — which is how the profile
	// reaches its unreferenced-source ratio without being told it directly.
	// THE TAIL IS RESERVED: only the first (len - unreferenced) sources are ever
	// cited, which is how the corpus reaches the profile's unreferenced ratio
	// without being told it directly.
	next := 0
	citable := len(sources) - p.UnreferencedSources
	if citable < 1 {
		citable = 1
	}
	take := func(k int) []string {
		var out []string
		for i := 0; i < k; i++ {
			out = append(out, sources[next%citable])
			next++
		}
		return out
	}
	claim := 0
	made := 0
	for _, bucket := range p.Buckets() {
		k, count := bucket[0], bucket[1]
		for i := 0; i < count; i++ {
			f := map[string]any{
				"claim":  fmt.Sprintf("Asserted proposition number %d", claim),
				"status": "asserted"}
			if k > 0 {
				f["attested_by"] = take(k)
			}
			// Contradictions, spread through the claims the profile asked for. The
			// contradicting end is `proposed` rather than `asserted` so a query over
			// asserted claims carries ONE END AND NOT THE OTHER — which is the
			// situation `split-contradiction` reports, and a set holding both ends
			// reports nothing.
			if made < p.Contradictions && claim%2 == 1 && claim > 0 {
				f["contradicts"] = fmt.Sprintf("c-%04d", claim-1)
				f["status"] = "proposed"
				made++
			}
			line(fmt.Sprintf("c-%04d", claim), f)
			claim++
		}
	}
	for i := 0; i < p.OpenQuestions; i++ {
		line(fmt.Sprintf("q-%04d", i), map[string]any{
			"question": fmt.Sprintf("Open question number %d", i),
			"status":   "open", "needs": "evidence"})
	}
	if err := os.WriteFile(filepath.Join(dir, assertName), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// STANDING QUERIES, so the set-shaped checks run too. Without them
	// `split-contradiction` never fires — and that is the check whose findings
	// silently stopped being tunable when the rules were applied in the wrong
	// place, so it is exactly the one that has to be exercised at scale.
	if err := os.WriteFile(filepath.Join(dir, standingName), []byte(
		// TWO SETS THAT SPLIT THE CONTRADICTIONS. A single query over every claim
		// puts both ends of each in one set and the check reports nothing, which
		// is how the first attempt at this fixture exercised it not at all.
		// SEPARATE GROUPS, and that is the point of the fixture. Both sets in ONE
		// group means a sibling set carries the other end and the check correctly
		// reports nothing — which is the grouping working, and exercises nothing.
		// A group that carries one end and no sibling reaching the other is the
		// situation the check exists for.
		"- name: asserted\n  group: live\n  query: claim[status=asserted] sort id\n"+
			"- name: proposed\n  group: draft\n  query: claim[status=proposed] sort id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE CHECKERS MUST BEHAVE AT THE REAL CORPUS'S SCALE AND SHAPE.
//
// This is the gap the other five hardening measures could not close. Three
// checker bugs in one day were invisible in `examples/` — ~100 nodes with a
// distribution nothing like the live index's — and were found only by running
// against 2,129 real ones. A profile is the shape without the content, so the
// difference the bugs lived in is now in the repo.
func TestCheckersBehaveAtLiveScaleAndShape(t *testing.T) {
	b, err := os.ReadFile("testdata/live-profile.json")
	if err != nil {
		t.Skip("no live profile recorded")
	}
	var prof Profile
	if err := json.Unmarshal(b, &prof); err != nil {
		t.Fatal(err)
	}
	if prof.Nodes < 1000 {
		t.Fatalf("the recorded profile is only %d nodes — it is meant to carry the LIVE "+
			"corpus's scale, and a small one makes this test agree with `examples/`", prof.Nodes)
	}

	root := t.TempDir()
	synthesize(t, root, prof)
	p, diags, err := LoadInWith(root, "synth", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(Errors(diags)) > 0 {
		t.Fatalf("a synthesized corpus must load clean: %v", Errors(diags)[:1])
	}

	// SCALE: within a factor of two of the real thing, or the generator has
	// drifted from the profile and this test is exercising something else.
	if got := len(p.Graph.Nodes); got*2 < prof.Nodes || got > prof.Nodes*2 {
		t.Errorf("synthesized %d nodes from a profile of %d", got, prof.Nodes)
	}

	// SHAPE: the ratio that made the attestation queue's `sole citation` band
	// useless must survive into the fixture, or the band looks discriminating
	// here and is not in life.
	got := ProfileOf(p.Graph)
	ratio := func(x Profile) float64 {
		if x.Kinds["claim"] == 0 {
			return 0
		}
		return float64(x.CitationsPerFact[1]) / float64(x.Kinds["claim"])
	}
	if a, b := ratio(got), ratio(prof); a < b-0.15 || a > b+0.15 {
		t.Errorf("single-citation ratio is %.2f, live is %.2f — the distribution did not survive", a, b)
	}
	if got.UnreferencedSources == 0 && prof.UnreferencedSources > 0 {
		t.Error("no unreferenced sources were generated — 58% of the live corpus's are")
	}

	// And the checkers actually run over it: findings, ranking and clustering all
	// have to survive real volume without falling over or going silent.
	fired := map[string]int{}
	for _, d := range diags {
		if d.Check != "" {
			fired[d.Check]++
		}
	}
	if fired[CheckUnreferencedSource] == 0 {
		t.Error("the unreferenced-source check went silent at scale")
	}
	if fired[CheckSplitContradiction] == 0 {
		t.Error("split-contradiction went silent at scale — it is the check whose findings " +
			"stopped being tunable, so it is the one that must fire here")
	}
	ranked := p.Graph.RankPending(p.Graph.PendingHuman(), map[string]bool{}, nil)
	if len(ranked) == 0 {
		t.Error("nothing pending at live scale — the queue this ranking exists for is empty")
	}
	for i := 1; i < len(ranked); i++ {
		if ranked[i-1].Band > ranked[i].Band {
			t.Fatalf("the ranking is not ordered at %d of %d", i, len(ranked))
		}
	}
	t.Logf("synthesized %d nodes, %d edges; findings: %v; pending: %d",
		len(p.Graph.Nodes), len(p.Graph.Edges), fired, len(ranked))
}
