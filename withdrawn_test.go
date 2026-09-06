package kgraph

import (
	"path/filepath"
	"testing"
)

// A CORPUS WITH NO INDEX HAS NO LEDGER, and that is an answer rather than a
// failure. Every test corpus in this repo is a temp directory nobody ingested,
// and so is a fresh clone — if this errored, every consumer would have to
// special-case the ordinary case.
func TestNoIndexIsNotAFailureToRead(t *testing.T) {
	got, err := Withdrawn(t.TempDir())
	if err != nil {
		t.Fatalf("Withdrawn on an unindexed corpus: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil — nothing was asked, so nothing is known", got)
	}
}

// The index name comes from the corpus's OWN config, not from its directory
// name, and a malformed one reads as "no index" rather than as a daemon that
// could not be reached. Same rule `raglitIndexName` documents: "" is a real
// answer.
func TestAMalformedConfigReadsAsNoIndex(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".raglit", "config.json"), "{not json")
	got, err := Withdrawn(root)
	if err != nil {
		t.Fatalf("Withdrawn: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
