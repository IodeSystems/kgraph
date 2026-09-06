package kgraph

import (
	"strings"
	"testing"
)

// `#` in a `doc:` is part of the filename, not an anchor separator. Court
// exhibits are named with recording numbers — `Record of survey file
// #201907220018 - ....txt` — and splitting on `#` truncated the path to a file
// that does not exist. `anchor:` is the explicit field and always was.
func TestHashInDocPathIsPartOfTheFilename(t *testing.T) {
	d, diags := parseFixture("f.facts", []byte("```kfacts\n- id: x\n  record: A\n"+
		"  doc: \"docs/Record of survey file #201907220018 - x.txt\"\n  class: record\n```\n"))
	if errs := Errors(diags); len(errs) > 0 {
		t.Fatal(errs)
	}
	got := d.Nodes[0].Source
	if got.DocPath != "docs/Record of survey file #201907220018 - x.txt" {
		t.Errorf("path was truncated: %q", got.DocPath)
	}
	if got.Anchor != "" {
		t.Errorf("an anchor was invented from the filename: %q", got.Anchor)
	}
	// The explicit field still works.
	d2, _ := parseFixture("f.facts", []byte("```kfacts\n- id: y\n  record: B\n"+
		"  doc: a.pdf\n  anchor: page-3\n  class: record\n```\n"))
	if d2.Nodes[0].Source.Anchor != "page-3" {
		t.Errorf("explicit anchor lost: %q", d2.Nodes[0].Source.Anchor)
	}
}

// An unquoted `#` mid-value is a YAML comment. `checkTruncated` only sees an
// unclosed bracket, so a path with no bracket lost everything after the `#` and
// nothing noticed.
func TestUnquotedHashInAnyValueIsCaught(t *testing.T) {
	_, diags := parseFixture("f.facts", []byte("```kfacts\n- id: x\n  record: A\n"+
		"  doc: docs/Record of survey file #201907220018 - x.txt\n  class: record\n```\n"))
	var found bool
	for _, d := range Errors(diags) {
		if strings.Contains(d.Msg, "unquoted `#`") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an unquoted # in a doc path was not caught: %v", diags)
	}
	// A leading `#` is a real comment and must stay allowed.
	_, clean := parseFixture("f.facts", []byte("```kfacts\n# a comment\n- id: x\n"+
		"  claim: Y\n  status: asserted\n```\n"))
	for _, d := range Errors(clean) {
		t.Errorf("a real comment was rejected: %v", d)
	}
}
