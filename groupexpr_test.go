package kgraph

import (
	"strings"
	"testing"
)

// A GROUP TAKES A RULE, AND PROSE THERE IS AN ERROR.
//
// `group:` holds the aggregate expression — the parser assigns it and blanks the
// body — and `groupStat`'s default arm leaves OK false when nothing matches. So a
// name written there produced a group that reported unsatisfied whatever its
// members did, indistinguishable from members that are genuinely unmet. There was
// already an error for an EMPTY expression and none for a wrong one.
//
// Found on this repo's own examples/fence-dispute — five groups, three of them
// with every member satisfied:
//
//	g-counsel-leads          3/3  OK=true   ">= 1"
//	g-before-mediation       2/3  OK=false  "Everything that must be done before mediation, in order"
//	g-reformation-proof      2/3  OK=false  "What reformation needs before it can be pleaded as settled"
//	g-impeachment            2/2  OK=false  "Points where Quill's account conflicts with a document"
//	g-ready-to-amend         2/2  OK=false  "The amendment is ready when all of these hold"
//	g-implied-easement-proof 4/4  OK=false  "What the implied easement rests on"
//
// `g-ready-to-amend` said in its own title that it is ready when all of these
// hold. All of them held. It reported not ready.
//
// IT SHIPPED AS A WARNING FOR ONE DAY, because six entries across two committed
// corpora were written that way and the format had nowhere else for a group's
// name. `title:` is that place now — see Node.Title — so this is an error, which
// is the severity the second half of the defect deserves: `group: <a name>` reads
// as membership by TAG, and a kgraph group is an explicit set with `member_of`
// edges, never a label match.
func TestAGroupTakesARuleAndProseIsRefused(t *testing.T) {
	for _, ok := range []string{"all", "any", "none", ">= 2", "<=10", "== 1", "sum(value)"} {
		if !ValidGroupExpr(ok) {
			t.Errorf("%q is a group expression and was refused", ok)
		}
	}
	for _, bad := range []string{
		"What the implied easement rests on",
		"The amendment is ready when all of these hold",
		"", "ALL", ">= two", "sum(",
	} {
		if ValidGroupExpr(bad) {
			t.Errorf("%q is not a group expression and was accepted", bad)
		}
	}

	// THE CORPUS IS CLEAN NOW, so the refusal is exercised on an entry written
	// here rather than on a fixture that happens to still carry the defect. A
	// test asserting "the examples produce this error" goes quiet the moment
	// somebody fixes the examples, which is the wrong way round.
	if _, _, err := LoadIn("examples", "fence-dispute"); err != nil {
		t.Fatalf("the committed corpus no longer loads: %v", err)
	}

	root := t.TempDir()
	writeFactLog(t, root, "", "- id: g-named\n  group: What the easement rests on\n  members: [c-one]\n"+
		"- id: c-one\n  claim: something\n  status: asserted\n")
	// AN ERROR STOPS THE LOAD, which is what an error is for: the corpus does not
	// fold, and the caller is told why rather than handed a graph with a group in
	// it that can never be satisfied.
	_, _, lerr := Load(root)
	if lerr == nil {
		t.Fatal("a group whose expression is prose was accepted")
	}
	if !strings.Contains(lerr.Error(), "takes a RULE over its members") {
		t.Errorf("refused for the wrong reason: %v", lerr)
	}
	if !strings.Contains(lerr.Error(), "title:") {
		t.Errorf("the refusal does not name where a group's NAME goes, which is the whole reason "+
			"prose ended up in `group:`: %v", lerr)
	}
}

// A TITLE IS FOR A GROUP AND NOTHING ELSE.
//
// Every other kind's body IS its text — a claim is what it claims — so a title
// there would duplicate it, and a general `title:` invites nine kinds to grow
// one. Refused rather than ignored: an ignored key reads as accepted by whoever
// typed it.
func TestATitleBelongsToAGroupAlone(t *testing.T) {
	root := t.TempDir()
	writeFactLog(t, root, "", "- id: c-one\n  claim: something\n  status: asserted\n"+
		"  title: A name this claim does not need\n")
	_, _, err := Load(root)
	if err == nil {
		t.Fatal("a claim was given a title")
	}
	if !strings.Contains(err.Error(), "names a GROUP") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// AND THE ORDER OF THE KEYS DOES NOT DECIDE IT.
//
// The kind is set by whichever body key appears, so a node writing `title:` above
// `group:` would otherwise be told it is not a group by a parser that has not yet
// read the line saying it is. The check runs after the loop for that reason.
func TestATitleAboveItsGroupIsStillAGroup(t *testing.T) {
	root := t.TempDir()
	writeFactLog(t, root, "", "- id: g-named\n  title: What the easement rests on\n  group: all\n"+
		"  members: [c-one]\n"+
		"- id: c-one\n  claim: something\n  status: asserted\n")
	p, _, err := Load(root)
	if err != nil {
		t.Fatalf("a title written above its group was refused: %v", err)
	}
	n, ok := p.Graph.Lookup("g-named")
	if !ok {
		t.Fatal("the group is missing")
	}
	if n.Title != "What the easement rests on" {
		t.Errorf("title is %q", n.Title)
	}
	if n.GroupExpr != "all" {
		t.Errorf("rule is %q", n.GroupExpr)
	}
}

// THE FOLD'S WARNINGS REACH A CALLER, and they did not.
//
// `storedFacts` scanned the fold's diagnostics for errors and discarded the
// rest. The markdown fact reader was deleted, so the fold is the only path a
// corpus loads by — which means every parse-time WARNING in the format had
// silently stopped being reported, not one class of them but all of them.
//
// Found by adding one and watching it fire in the tests that fold explicitly
// while `LoadIn` — the entry point everything else uses — reported nothing.
func TestAFoldsWarningsReachTheCaller(t *testing.T) {
	_, diags, err := LoadIn("examples", "fence-dispute")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var fromFold int
	for _, d := range diags {
		if strings.HasSuffix(d.File, assertName) && d.Severity == SevWarn {
			fromFold++
		}
	}
	if fromFold == 0 {
		t.Error("no warning from the fold reached the caller. Every diagnostic the format parser " +
			"produces about a fact arrives this way, and dropping them is invisible: a corpus " +
			"with problems loads clean")
	}
}
