package main

import (
	"reflect"
	"testing"
)

// STRUCTURE IS THE ONLY THING THE PARSE MAY ADD.
//
// stdin was a plain string, so `attested_by` could only ever be set to one id —
// the command whose purpose is "change one field, carrying the rest forward"
// could not add a citation. But parsing scalars too would retype them on a legal
// corpus, so a scalar keeps its exact bytes.
func TestAmendTakesStructureAndLeavesScalarsAlone(t *testing.T) {
	// Scalars keep their bytes. These are the ones that would silently retype.
	for _, s := range []string{
		"s-1993-quitclaim", "yes", "no", "null-ish", "2024-06-06",
		"0640", "1.10", "on", "off", "~", "true",
	} {
		if s == "null" {
			continue
		}
		got := amendValue(s)
		if got != any(s) {
			t.Errorf("amendValue(%q) = %#v (%T), want the string unchanged", s, got, got)
		}
	}
	// `null` still drops the field.
	if amendValue("null") != nil {
		t.Errorf("null no longer drops the field")
	}
	// A list comes through as a list.
	got := amendValue("[s-a, s-b]")
	if !reflect.DeepEqual(got, []any{"s-a", "s-b"}) {
		t.Errorf("a list did not survive: %#v", got)
	}
	// And the mapping form, which is what carries the per-edge justification.
	got = amendValue("- id: s-bn-1979-license\n  because: grants the railroad-grade access\n")
	l, ok := got.([]any)
	if !ok || len(l) != 1 {
		t.Fatalf("mapping form did not parse as a list: %#v", got)
	}
	m, ok := l[0].(map[string]any)
	if !ok || m["id"] != "s-bn-1979-license" || m["because"] != "grants the railroad-grade access" {
		t.Errorf("the edge lost its id or its because: %#v", l[0])
	}
}

// A GLOBAL FLAG AFTER THE SUBCOMMAND MUST BE REFUSED, NOT IGNORED.
//
// `flag.Parse` stops at the first positional, so a global flag written after the
// subcommand arrived untouched and was silently dropped. `kg scan --json`
// printed text. Worse, `kg attest <f> <s> confirmed --by dana` recorded the
// verdict UNSIGNED and joined "--by dana" into the note, because attest takes
// its note from the trailing words — a silently unattributed row in the one
// table that records who checked what.
func TestGlobalFlagAfterSubcommandIsRefused(t *testing.T) {
	var o opts
	globals := globalFlagNames(newFlagSet(&o))
	for _, name := range []string{"by", "json", "root", "index", "store", "note", "now"} {
		if !globals[name] {
			t.Errorf("--%s is no longer guarded", name)
		}
	}
	// `verify` and `daemon` parse --addr themselves and their documented usage
	// puts it after the subcommand, so guarding it would refuse valid commands.
	if globals["addr"] {
		t.Errorf("--addr is guarded, which breaks `kg verify --addr HOST:PORT`")
	}
}
