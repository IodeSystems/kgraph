package kgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// EVERY REGISTERED CHECK MUST ACTUALLY FIRE SOMEWHERE.
//
// This codebase's characteristic bug is code that LOOKS alive: `Backlog` carried
// a `specs` parameter nothing read, `web.go` built a `bySpec` map nothing read,
// and `kg source dupes` shipped with a top-ranked band that could never fire
// because same-file duplicates came from a different check. None of those was a
// missing test — each was a test that could not fail.
//
// A check nobody can trigger is a rule that cannot be tuned, a finding that
// cannot be accepted, and a guarantee nobody has.
func TestEveryRegisteredCheckFiresOnTheFixtures(t *testing.T) {
	fired := map[string]bool{}
	// A corpus built to trigger every CONTENT check, kept apart from `examples/`
	// so exercising a checker never disturbs the standing-query pins there.
	for _, root := range []string{"testdata/allchecks", "examples"} {
		indexes, err := Indexes(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, idx := range indexes {
			_, diags, err := LoadInWith(root, idx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range diags {
				if d.Check != "" {
					fired[d.Check] = true
				}
			}
		}
	}
	if len(fired) == 0 {
		t.Fatal("no check fired on the fixtures — this test is checking nothing")
	}
	for name := range knownChecks {
		if stateChecks[name] != "" {
			continue
		}
		if !fired[name] {
			t.Errorf("check %q never fires on the fixtures — add a corpus that triggers it, "+
				"or it is a rule nobody can tune and a finding nobody can accept", name)
		}
	}
	// A STATE check cannot live in a committed corpus — `graph-shrank` needs a
	// watermark recorded from a LARGER past, which no fixture can hold without
	// permanently claiming to have shrunk. Exempt by name, and only with the test
	// that does cover it named here, so the exemption cannot become a place to
	// hide an unexercised check.
	for name, by := range stateChecks {
		if _, ok := knownChecks[name]; !ok {
			t.Errorf("%q is exempted but is not a registered check", name)
		}
		if !testExists(t, by) {
			t.Errorf("check %q is exempted from the fixtures because %s covers it, "+
				"and that test does not exist", name, by)
		}
	}
}

// stateChecks are the checks a corpus fixture cannot reach through `LoadIn`, and
// the test that covers each instead.
//
// Two reasons appear here and both are named at the entry: a check about the
// corpus's HISTORY, which no committed fixture can hold; and a check the CLI
// runs rather than the load, because it costs too much to run on every command.
// An entry with no covering test fails, so this cannot become a place to park an
// unexercised check.
var stateChecks = map[string]string{
	CheckGraphShrank: "TestACorpusThatStopsLoadingIsAnErrorNotASilentSuccess",
	// Emitted by the CLI, not by a load: resolving a declaration needs an Env
	// (a clock) and `LoadIn` has none. So it cannot fire through this test's
	// door, and the corpus fixture cannot reach it.
	CheckEmptyQuery: "TestNoDeclaredQueryResolvesEmpty",
	// Also CLI-only, and for cost rather than for shape: it searches transcript
	// TEXT, and putting it in `LoadIn` would make every `kg query` pay for a
	// full-text sweep of the corpus. `kg scan` is the command that asks for
	// everything, so it is the one that runs it.
	CheckAlreadyHeld: "TestCheckAlreadyHeldCatchesAQuestionAskingForAHeldDocument",
	// CLI-only for the same reason as the one above, and the reason is worth
	// repeating because it is what the check is FOR: it reads the document's
	// transcription. Putting it in `LoadIn` would make every command pay to
	// re-read the corpus, and `kg scan` is the one that asks for everything.
	CheckAttestedExcerpt: "TestARereadDocumentIsCaughtUnderAStandingVerdict",
}

// testExists reports whether a test function of that name is in the package.
func testExists(t *testing.T, name string) bool {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		if rerr == nil && strings.Contains(string(b), "func "+name+"(") {
			return true
		}
	}
	return false
}

// THE SHAPE OF WHAT THE CHECKS PRODUCE, pinned.
//
// Not the message text — that must be free to be reworded. What is pinned is
// per check: how many findings, at what severity, and HOW MANY CARRY A KEY. That
// last column is the one that catches the failure text cannot: a check that
// quietly stops identifying its findings still prints exactly as before, while
// silently becoming untunable and unacceptable. `split-contradiction` did
// exactly that for one build, because the rules were applied before it ran.
func TestTheDiagnosticShapeIsStable(t *testing.T) {
	type shape struct {
		count, keyed, errors int
	}
	got := map[string]*shape{}
	indexes, err := Indexes("examples")
	if err != nil {
		t.Fatal(err)
	}
	for _, idx := range indexes {
		_, diags, err := LoadIn("examples", idx)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range diags {
			name := d.Check
			if name == "" {
				name = "(unidentified)"
			}
			s := got[name]
			if s == nil {
				s = &shape{}
				got[name] = s
			}
			s.count++
			if d.Key != "" {
				s.keyed++
			}
			if d.Severity == SevError {
				s.errors++
			}
		}
	}
	// Every IDENTIFIED finding must carry a key, or it cannot be accepted. The
	// two travel together by construction and a divergence means one was added
	// without the other.
	for name, s := range got {
		if name == "(unidentified)" {
			continue
		}
		if s.keyed != s.count {
			t.Errorf("check %q produced %d finding(s) but only %d carry a key — "+
				"an identified finding that cannot be accepted is half a mechanism",
				name, s.count, s.keyed)
		}
		if s.errors > 0 {
			t.Errorf("check %q emitted %d error(s) — only warnings are tunable and "+
				"acceptable, so an error must not carry a check name", name, s.errors)
		}
	}
	var lines []string
	for name, s := range got {
		lines = append(lines, fmt.Sprintf("%s=%d", name, s.count))
	}
	sort.Strings(lines)
	t.Logf("fixture diagnostic shape: %s", strings.Join(lines, " "))
}

// RULES MUST BE TESTED THROUGH THE PIPELINE, not by calling ApplyRules.
//
// If a bug can be "the function was called in the wrong place", the test must
// not get to choose the place. `TestRuleLevelsSilenceAndRaise` builds a slice
// and calls `ApplyRules` on it, so it passed while the real load applied rules
// BEFORE `CheckSplitContradiction` ran — leaving the second-largest check class
// in the corpus silently unconfigurable.
//
// This goes through `LoadIn`, and it deliberately silences a check that runs
// LAST in the pipeline, because that is the one a misplaced call misses.
func TestARuleSilencesACheckThatRunsLastInTheLoad(t *testing.T) {
	const root = "testdata/allchecks"
	_, diags, err := LoadInWith(root, "allchecks", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := 0
	for _, d := range diags {
		if d.Check == CheckSplitContradiction {
			before++
		}
	}
	if before == 0 {
		t.Fatal("the fixture no longer produces a split-contradiction — this test is checking nothing")
	}

	// Silence it through the MARKER, exactly as an operator would.
	marker := filepath.Join(root, "matter", IndexMarker)
	orig, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.WriteFile(marker, orig, 0o644) })
	if err := os.WriteFile(marker,
		append(orig, []byte("rule."+CheckSplitContradiction+": off\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	_, diags, err = LoadInWith(root, "allchecks", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Check == CheckSplitContradiction {
			t.Fatalf("a rule did not reach a check that runs last in the load: %s", d.Msg)
		}
	}
}
