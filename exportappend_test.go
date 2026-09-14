package kgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A LINE ALREADY IN THE LOG IS NEVER REWRITTEN BY THE NEXT EXPORT.
//
// Found in a consumer: one restatement in a matter whose log another hand had
// written rewrote 22 lines nobody touched. Three had `&` turned into a unicode escape by
// `json.Marshal`, and the rest had their keys re-ordered into the struct's order.
// The export runs after every write, so every write carried that diff.
//
// The hand-written log below is spelled the way the encoder would NOT spell it —
// keys alphabetised, a raw `&` — so a re-encode of either line shows up as a
// changed byte.
func TestTheExportChangesOnlyTheLinesTheStoreAdded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "facts.jsonl")
	written := "// a log another hand wrote\n" +
		`{"at":"2026-09-03T00:00:00Z","by":"carl","fields":{"claim":"Quill & Bramble shared the strip","status":"asserted"},"id":"c-shared","op":"assert"}` + "\n" +
		`{"at":"2026-09-03T00:00:01Z","by":"carl","fields":{"status":"asserted","claim":"the fence stood before either"},"id":"c-fence","op":"assert"}` + "\n"
	if err := os.WriteFile(path, []byte(written), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := OpenStore(filepath.Join(dir, "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := ImportJSONL(st, path); err != nil {
		t.Fatal(err)
	}

	// Exporting a store that holds exactly what the file holds changes nothing.
	if _, err := ExportJSONL(st, path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != written {
		t.Fatalf("exporting an unchanged store rewrote the log:\n%s", got)
	}

	if err := st.Append(Assertion{Op: OpAssert, ID: "c-later", By: "carl", At: "2026-09-03T11:00:00Z",
		Fields: map[string]any{"claim": "Lot 7 & Lot 8 were assessed apart", "status": "asserted"}}); err != nil {
		t.Fatal(err)
	}
	n, err := ExportJSONL(st, path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("the export reports %d assertions, want 3", n)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), written) {
		t.Fatalf("the lines already in the log were rewritten.\nwas:\n%s\nnow:\n%s", written, got)
	}
	added := strings.TrimSuffix(strings.TrimPrefix(string(got), written), "\n")
	if strings.Count(added, "\n") != 0 || !strings.Contains(added, `"id":"c-later"`) {
		t.Errorf("want exactly one new line for c-later, got:\n%s", added)
	}
	// The six-character unicode escape for `&`, built from bytes so the sequence is
	// never spelled in this file: a tool that decodes escapes would turn it into the
	// very character it stands for, and the check would fail every correct line.
	escaped := string([]byte{92, 'u', '0', '0', '2', '6'})
	if strings.Contains(added, escaped) || !strings.Contains(added, "Lot 7 & Lot 8") {
		t.Errorf("the new line escaped `&`; the log is read by people and git, not a browser:\n%s", added)
	}

	// And the log still rebuilds the store it came from.
	back, err := ReadAssertionLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Errorf("the log reads back %d assertions, want 3", len(back))
	}
}

// A FILE THAT IS NOT A PREFIX OF THE STORE IS REPLACED BY THE STORE. The store is
// what the export projects; a log that disagrees with it — edited by hand, or left
// over from another store — must not survive by being appended to.
func TestAnExportOverADifferentLogWritesTheStore(t *testing.T) {
	st := bulkStore(t, 2)
	path := filepath.Join(t.TempDir(), "facts.jsonl")
	stale := `{"op":"assert","id":"c-somebody-else","fields":{"claim":"x","status":"asserted"},"by":"carl","at":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportJSONL(st, path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "c-somebody-else") {
		t.Errorf("a log that is not a prefix of the store survived the export:\n%s", got)
	}
	back, err := ReadAssertionLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Errorf("the log reads back %d assertions, want the store's 2", len(back))
	}
}
