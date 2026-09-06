package kgraph

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bulkStore is a store with enough entries that a partial write is visible.
func bulkStore(t *testing.T, n int) Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for i := 0; i < n; i++ {
		if err := st.Append(Assertion{
			Op: OpAssert, ID: "c-" + strings.Repeat("x", 40) + itoa(i), By: "carl",
			At:     "2026-09-03T10:00:00Z",
			Fields: map[string]any{"claim": strings.Repeat("a long body ", 40), "status": "asserted"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// THE EXPORT IS WRITTEN WHOLE OR NOT AT ALL, and this asserts it DETERMINISTICALLY.
//
// It used to be `os.WriteFile`: truncate, then write. A reader arriving between
// the two sees a short log; a second writer arriving between them interleaves
// with the first. Neither is hypothetical — the file is a projection re-emitted
// after every write, and more than one process writes a corpus by design: a
// long-lived daemon and an ordinary CLI on one box, or an old and a new binary
// during a service upgrade. What it costs is not a retry: this is the portable
// form and the audit trail, so a truncated one is lost history that looks like
// history.
//
// THE FIRST VERSION OF THIS TEST RACED TWO WRITERS AGAINST A READER AND PROVED
// NOTHING. It passed against the OLD implementation, twice, because the write
// window is a sliver of each export — the time goes on marshalling — and a
// reader sampling once per round never landed in it. Growing the payload to
// several megabytes made it take 67 seconds and still never caught it. A
// probabilistic test for a rare interleaving is a test that goes green either
// way, which is worse than none.
//
// So this asserts the GUARANTEE instead, and the guarantee is exactly what a
// rename gives: a reader that opened the file keeps reading a COMPLETE log,
// because its descriptor holds the old inode until it lets go. Truncate-in-place
// cannot do that — the bytes under an open descriptor change while it reads.
func TestAReaderHoldingTheLogNeverSeesItTruncated(t *testing.T) {
	st := bulkStore(t, 20)
	path := filepath.Join(t.TempDir(), "corpus", "facts.jsonl")
	if _, err := ExportJSONL(st, path); err != nil {
		t.Fatal(err)
	}
	before, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}

	// A reader opens the log and is slow — it has the descriptor and has not
	// read yet, which is every consumer that opens a file and then does work.
	f, oerr := os.Open(path)
	if oerr != nil {
		t.Fatal(oerr)
	}
	defer func() { _ = f.Close() }()

	// The corpus moves on underneath it: another entry, another export.
	if err := st.Append(Assertion{Op: OpAssert, ID: "c-later", By: "carl",
		At: "2026-09-03T11:00:00Z", Fields: map[string]any{"claim": "x", "status": "asserted"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportJSONL(st, path); err != nil {
		t.Fatal(err)
	}

	got, gerr := io.ReadAll(f)
	if gerr != nil {
		t.Fatalf("the held descriptor stopped being readable: %v", gerr)
	}
	if string(got) != string(before) {
		t.Errorf("a reader holding the log saw it change underneath: %d bytes, was %d. "+
			"Truncate-in-place rewrites the bytes under an open descriptor; a rename leaves "+
			"them alone", len(got), len(before))
	}

	// And the file on disk is the NEW log, complete — the rename replaced it
	// rather than the reader having pinned a stale one forever.
	after, aerr := os.ReadFile(path)
	if aerr != nil {
		t.Fatal(aerr)
	}
	if len(after) <= len(before) {
		t.Errorf("the new log is %d bytes and the old was %d; the export did not land",
			len(after), len(before))
	}
	if !strings.Contains(string(after), "c-later") {
		t.Error("the new entry is not in the exported log")
	}
}

// NO TEMP FILE SURVIVES. The export runs after every write, so a leaked temp per
// export fills the corpus directory with debris git would then be asked about.
func TestTheExportLeavesNoTempBehind(t *testing.T) {
	st := bulkStore(t, 5)
	dir := t.TempDir()
	path := filepath.Join(dir, "facts.jsonl")
	for i := 0; i < 5; i++ {
		if _, err := ExportJSONL(st, path); err != nil {
			t.Fatal(err)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != "facts.jsonl" {
			t.Errorf("the export left %q behind", e.Name())
		}
	}
}

// THE TEMP IS IN THE TARGET'S DIRECTORY, because rename is only atomic within a
// filesystem. A temp under the system temp dir degrades to copy-and-truncate
// across a mount — silently, and exactly on the machines where the corpus is on
// its own volume.
func TestTheExportIsReadableWithTheModeItAlwaysHad(t *testing.T) {
	st := bulkStore(t, 3)
	path := filepath.Join(t.TempDir(), "facts.jsonl")
	if _, err := ExportJSONL(st, path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode is %v; a temp file's 0600 would make the export unreadable to anyone "+
			"else on the box", fi.Mode().Perm())
	}
}
