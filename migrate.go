package kgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MIGRATION IS A TRANSCRIPTION, NOT A TRANSLATION.
//
// The obvious implementation is to load the graph and write each `Node` back out
// as fields — and it is the wrong one, for the same reason `FoldAssertions` does
// not build nodes. A `Node` → fields serialiser is the parser run backwards, so
// it is a second implementation of the format, and the second implementation is
// the one that drifts: it would have to know every idiom, every inverse key,
// every dialect subtype, and it would silently drop the first one somebody adds.
//
// So this never touches `Node`. It reads the authored YAML entries out of the
// `kfacts` blocks and emits each one as an assertion's `Fields` verbatim. What
// goes into the log is exactly what was in the file, which is why the equality
// test can be `SemHash` over every node rather than a spot check.

// MigrateResult reports what a migration would do, or did.
type MigrateResult struct {
	// Files migrated, repo-relative.
	Files []string
	// Assertions written.
	Count int
	// Prose is the markdown that surrounded the fact blocks, per file, and it is
	// NOT migrated. See the comment on MigrateFacts — this is the honest half of
	// the report, and it is a count rather than a silence.
	Prose map[string]int
}

// MigrateFacts turns an index's `*.kfacts.md` into assertions.
//
// One `assert` per authored entry, signed by `by` and stamped `at`. Nothing is
// deleted and the fact files are left alone: the parser is the LAST thing to go
// (`plan/facts.md`), because a one-way door needs holding open until everything
// is through.
//
// WHAT IT DOES NOT CARRY, said plainly rather than discovered later: the prose
// around the blocks. A `*.kfacts.md` explains itself in markdown — why a fact
// matters, which reading of an ambiguous document was taken, what was nearly
// written instead — and that text belongs to a FILE, not to any one entry in it.
// Attaching it to an arbitrary assertion would invent an attribution; dropping
// it silently would lose the most valuable thing in the file. So it is counted
// and reported, and moving it is a decision per file that a person makes.
func MigrateFacts(st Store, root, index, by string, at string) (*MigrateResult, error) {
	if strings.TrimSpace(by) == "" {
		return nil, fmt.Errorf("a migration has to be signed — every assertion records who made it")
	}
	paths, err := FindFacts(root, index)
	if err != nil {
		return nil, err
	}
	res := &MigrateResult{Prose: map[string]int{}}
	var log []Assertion
	for _, rel := range paths {
		src, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			return nil, rerr
		}
		blocks := fencedBlocks(src, "kfacts")
		if len(blocks) == 0 {
			continue
		}
		for _, b := range blocks {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(b.body), &doc); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", rel, b.firstLine, err)
			}
			seq, serr := rawEntries(&doc)
			if serr != nil {
				return nil, fmt.Errorf("%s:%d: %w", rel, b.firstLine, serr)
			}
			for _, entry := range seq {
				id, _ := entry["id"].(string)
				if id == "" {
					return nil, fmt.Errorf("%s:%d: an entry has no id", rel, b.firstLine)
				}
				fields := map[string]any{}
				for k, v := range entry {
					if k == "id" {
						continue
					}
					fields[k] = v
				}
				log = append(log, Assertion{
					Op: OpAssert, ID: id, Fields: fields, By: by, At: at,
					// Provenance of the MIGRATION, not of the fact. A reader of the log
					// three years from now should be able to tell a fact somebody
					// asserted from one that arrived wholesale in a format change.
					Note: "migrated from " + rel,
				})
			}
		}
		if n := proseLines(src); n > 0 {
			res.Prose[rel] = n
		}
		res.Files = append(res.Files, rel)
	}
	res.Count = len(log)
	if len(log) == 0 {
		return res, nil
	}
	for _, a := range log {
		if err := st.Append(a); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// exportDir is where an index's JSONL export belongs: beside the facts, the same
// placement `sources.lock`, `attestations.jsonl` and `standing.yaml` already
// use, so a gitignored project's export is gitignored with it.
//
// The STORE is elsewhere — see StorePath. The export is text and belongs in the
// corpus; the database is not and does not.
func exportDir(root, index string) (string, error) {
	paths, err := FindFacts(root, index)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return filepath.ToSlash(filepath.Dir(paths[0])), nil
}

// proseLines counts the non-blank markdown outside the fact blocks.
//
// Reported so the one thing the migration cannot carry is a number rather than a
// silence. A file with 40 lines of reasoning around its YAML is a different
// migration decision from one with a heading.
func proseLines(src []byte) int {
	lines := strings.Split(string(src), "\n")
	in, n := false, 0
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			in = !in
			continue
		}
		if in || t == "" {
			continue
		}
		n++
	}
	return n
}

// rawEntries reads a `kfacts` block as authored TEXT, not as typed values.
//
// THIS IS THE BUG THE FIXTURE CORPUS CAUGHT, and it is worth the paragraph.
// Decoding a block into `[]map[string]any` lets yaml.v3 type-convert scalars, so
// `at: 2026-01-22` became a `time.Time` and re-marshalled as
// `2026-01-22T00:00:00Z`. That is a different date PRECISION, so the node hashed
// differently, so 24 of 99 fixture nodes "changed meaning" in a migration whose
// entire promise is that nothing does.
//
// A round trip through Go's type system is a translation, and the whole design
// of this file is that migration must be a transcription. So every scalar is
// kept as the text that was written. That is lossless by construction, and it is
// safe because the parser reads `v.Value` off the YAML node rather than decoding
// into a typed field — a quoted "2026-01-22" and a bare one are the same string
// to it, which is exactly why the text is the right thing to preserve.
func rawEntries(doc *yaml.Node) ([]map[string]any, error) {
	n := doc
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil, nil
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("a kfacts block must be a list of entries")
	}
	out := make([]map[string]any, 0, len(n.Content))
	for _, item := range n.Content {
		v := rawValue(item)
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("an entry is not a mapping")
		}
		out = append(out, m)
	}
	return out, nil
}

// rawValue converts a YAML node keeping every scalar as its authored text.
func rawValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = rawValue(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		xs := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			xs = append(xs, rawValue(c))
		}
		return xs
	case yaml.AliasNode:
		return rawValue(n.Alias)
	default:
		return n.Value
	}
}
