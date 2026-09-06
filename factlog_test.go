package kgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// THE CONVERSION HELPERS. A test corpus is written as a LOG now, not as
// markdown, because that is what kgraph reads.
//
// These take the same YAML entries a `*.kfacts.md` block held — the vocabulary
// did not change, only the file around it — so converting a test is swapping the
// wrapper, never rewriting the facts. That is the point of `parseEntries` being
// the format and the markdown being the file.

// writeFactLog writes YAML entries into a corpus as an exported assertion log,
// which `LoadIn` reads when no database is present.
func writeFactLog(t *testing.T, root, dir, entries string) {
	t.Helper()
	log := logFromYAML(t, entries)
	var b []byte
	for _, a := range log {
		line, err := yamlJSON(a)
		if err != nil {
			t.Fatal(err)
		}
		b = append(append(b, line...), '\n')
	}
	abs := filepath.Join(root, dir, assertName)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// logFromYAML turns a block of entries into assertions, one per entry, in the
// order written. The timestamps ascend so the fold's order is the authored one.
func logFromYAML(t *testing.T, entries string) []Assertion {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(entries), &doc); err != nil {
		t.Fatal(err)
	}
	seq, err := rawEntries(&doc)
	if err != nil {
		t.Fatal(err)
	}
	var out []Assertion
	for i, e := range seq {
		id, _ := e["id"].(string)
		if id == "" {
			t.Fatalf("entry %d has no id", i+1)
		}
		fields := map[string]any{}
		for k, v := range e {
			if k != "id" {
				fields[k] = v
			}
		}
		out = append(out, Assertion{
			Op: OpAssert, ID: id, Fields: fields, By: "carl",
			At: assertTime(i),
		})
	}
	return out
}

// assertTime gives each entry an ascending RFC3339 stamp, so a test's authored
// order is the fold's order without every test having to say so.
func assertTime(i int) string {
	const base = "2026-08-30T10:00:"
	return base + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10)) + "Z"
}

func yamlJSON(a Assertion) ([]byte, error) { return jsonMarshal(a) }

// storeFor opens a scratch store seeded from a corpus's exported log, so a test
// can exercise a WRITE path against a corpus it built as text.
func storeFor(t *testing.T, root string) Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	// Seed from whatever logs the corpus holds. A test corpus is small, so
	// walking for them is cheaper than threading the index name through.
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || fi.Name() != assertName {
			return nil
		}
		if _, ierr := ImportJSONL(st, p); ierr != nil {
			t.Fatal(ierr)
		}
		return nil
	})
	return st
}

// exportStore writes the store back out into the corpus, so a re-scan sees what
// a write op did.
func exportStore(t *testing.T, st Store, root, dir string) error {
	t.Helper()
	_, err := ExportJSONL(st, filepath.Join(root, dir, assertName))
	return err
}

// stripFence removes the markdown a fixture used to live inside, so a corpus
// string written for `*.kfacts.md` can be handed to writeFactLog unchanged.
//
// A conversion aid, not a format: it exists so the ENTRIES in a fixture stay
// byte-for-byte what they were, which is what makes a converted test still
// testing the same thing.
func stripFence(src string) string {
	out, in := []string{}, false
	for _, ln := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			in = !in
			continue
		}
		if in {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// parseFixture parses a fixture that was written as a markdown fact block.
//
// The mechanical half of the conversion: the ENTRIES are unchanged, and this
// strips the fence they used to live in before handing them to the format
// parser. Keeping the fixtures byte-identical is what makes a converted test
// still test the same thing — the alternative, retyping ninety corpora, is how
// coverage quietly changes meaning.
func parseFixture(path string, src []byte) (*Doc, []Diag) {
	return parseFixtureIn(legal, path, src)
}

func parseFixtureIn(dl Dialect, path string, src []byte) (*Doc, []Diag) {
	return ParseEntries(dl, path, []byte(stripFence(string(src))))
}

// writeLegacyFacts takes a fixture in its ORIGINAL markdown form and writes it
// as a log, deriving the directory from where the `*.kfacts.md` would have gone.
//
// The conversion tool for the bulk of the corpus. One rename per site, no
// rewriting of the fixture itself — which matters more than it sounds: a fixture
// retyped during a migration is a fixture nobody can diff against what it used
// to assert, and this corpus has caught four format bugs by being exactly what
// somebody wrote.
func writeLegacyFacts(t *testing.T, root, rel, md string) {
	t.Helper()
	writeFactLog(t, root, filepath.ToSlash(filepath.Dir(rel)), stripFence(md))
}

// writeDeclared writes a `standing.yaml` from spec-style `name: query` lines,
// all in one group.
//
// The conversion aid for tests that used to write a `*.kgraph.md` fixture: the
// QUERIES stay byte-for-byte what they were, which is what makes a converted
// test still test the same thing. Same role `stripFence` played when the fact
// files went.
func writeDeclared(t *testing.T, root, dir, block string) {
	t.Helper()
	var b strings.Builder
	for _, ln := range strings.Split(block, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		name, q, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "- name: %s\n  group: r\n  query: %s\n",
			strings.TrimSpace(name), strings.TrimSpace(q))
	}
	abs := filepath.Join(root, dir, standingName)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newCorpus makes a test corpus whose index lives in a SUBDIRECTORY, never at
// the root, and returns the root plus the index name.
//
// NOT A CONVENIENCE. A fixture with its index at the repo root makes "the
// directory this index lives in" and "the root" the same string, so every
// function that DERIVES one from the other looks correct however it is written.
// That is precisely how the liveness check shipped unable to find its own
// watermark: its test wrote the mark and read it in one directory, and the
// derivation it depended on never had a chance to be wrong.
func newCorpus(t *testing.T, index string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", index)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, IndexMarker),
		[]byte("index: "+index+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// corpusDir is where newCorpus put the index, repo-relative.
func corpusDir(index string) string { return filepath.ToSlash(filepath.Join("projects", index)) }
