package kgraph

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Finding a source that moved.
//
// `doc:` is a path, and a path is not an identity. Reorganising a corpus — which
// happens, and should — breaks every citation into the directories that moved,
// and the graph reports each one as `not-found` with no way to tell "this
// document is gone" from "this document is one directory over". A legal corpus
// gets reorganised precisely when it grows, so the failure arrives exactly when
// the citations matter most.
//
// The lock already records a sha256 of every document's bytes. That IS an
// identity, and it survives the move. So a not-found citation whose locked hash
// still exists somewhere in the index is not a broken reference — it is a
// reference to a file that was moved, and kgraph can say so.
//
// What it must NOT do is guess. A repair rewrites a fact file, and a citation
// silently repointed at the wrong exhibit is worse than one that is visibly
// broken. So: exact hash, unique match, inside the index, or no proposal.

// Relocation is one moved document kgraph can account for.
type Relocation struct {
	SourceID string `json:"source_id"`
	Was      string `json:"was"`  // the dead `doc:`, project-relative
	Now      string `json:"now"`  // where the bytes actually are
	Hash     string `json:"hash"` // what proves they are the same document
	File     string `json:"file"` // the kfacts file to edit
}

// Ambiguity is a moved document with more than one candidate. Reported rather
// than resolved: two files with identical bytes are two exhibits, and picking
// one is a claim about which was cited that nothing in the graph supports.
type Ambiguity struct {
	SourceID   string   `json:"source_id"`
	Was        string   `json:"was"`
	Candidates []string `json:"candidates"`
}

// FindRelocations matches not-found sources against the index by content hash.
//
// Only files INSIDE the index are considered. An index is a closed graph, so a
// document that turns up in a sibling project is not this project's exhibit —
// it is a different matter that happens to hold the same bytes, and citing
// across that seam is the thing indexes exist to prevent.
func FindRelocations(root string, g *Graph, locks LockSet) ([]Relocation, []Ambiguity, error) {
	sts, _ := g.SourceStatuses(root, locks)

	// What are we looking for? Sources whose document is missing AND that were
	// recorded — without a locked hash there is nothing to match on, and the
	// honest answer is that the document is simply gone.
	//
	// `vanished` is the interesting state and the common one: it means the bytes
	// WERE recorded and the path no longer resolves, which is exactly what a move
	// looks like. `not-found` is included because a citation can be broken and
	// locked in either order.
	want := map[string][]SourceStatus{}
	for _, s := range sts {
		if s.State != SrcVanished && s.State != SrcNotFound {
			continue
		}
		// `Was` is the hash the lock recorded — SourceStatuses already resolved it,
		// and duplicating that lookup here got the keying wrong once already.
		if s.Was != "" {
			want[s.Was] = append(want[s.Was], s)
		}
	}
	if len(want) == 0 {
		return nil, nil, nil
	}

	// One walk, hashing only files whose SIZE could match — hashing a corpus of
	// scans to find four moved documents is minutes of IO for nothing.
	sizes := map[int64]bool{}
	for _, cands := range want {
		for _, s := range cands {
			if n, ok := g.Nodes[s.ID]; ok {
				if l := locks[LockDirOf(n)]; l != nil {
					if e, ok := l.Entries[lockKey(LockDirOf(n), s.Doc)]; ok {
						sizes[e.Size] = true
					}
				}
			}
		}
	}

	base := filepath.Join(root, indexDirOf(g))
	found := map[string][]string{}
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil || !sizes[fi.Size()] {
			return nil
		}
		h, _, err := HashDoc(p)
		if err != nil {
			return nil
		}
		if _, wanted := want[h]; wanted {
			rel, err := filepath.Rel(root, p)
			if err == nil {
				found[h] = append(found[h], filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	var moved []Relocation
	var amb []Ambiguity
	for h, cands := range want {
		hits := found[h]
		sort.Strings(hits)
		for _, s := range cands {
			switch len(hits) {
			case 0:
				// Genuinely gone. Not a relocation, and not reported as one.
			case 1:
				moved = append(moved, Relocation{SourceID: s.ID, Was: s.Doc, Now: hits[0],
					Hash: h, File: fileOfSource(g, s.ID)})
			default:
				amb = append(amb, Ambiguity{SourceID: s.ID, Was: s.Doc, Candidates: hits})
			}
		}
	}
	sort.Slice(moved, func(i, j int) bool { return moved[i].SourceID < moved[j].SourceID })
	sort.Slice(amb, func(i, j int) bool { return amb[i].SourceID < amb[j].SourceID })
	return moved, amb, nil
}

// ApplyRelocations rewrites each `doc:` to where the bytes now are.
//
// The new value is written RELATIVE TO THE DECLARING FILE, matching how
// DocPathOf resolves it — writing a root-relative path here would produce a
// citation that scans clean on this machine and resolves somewhere else on any
// other layout.
// ApplyRelocationsTo repairs through the STORE: each moved document becomes a
// re-assertion of its source carrying the corrected `doc:`.
//
// A repair used to be a text edit, rewriting `doc:` inside the declaring file.
// That is no longer available and it should not be: the store is append-only,
// and "the exhibit moved" is a thing that HAPPENED, with a time and a signer.
// Editing the old line would erase that the corpus once pointed somewhere else,
// which is exactly the history the store exists to keep.
func ApplyRelocationsTo(st Store, dl Dialect, root string, moved []Relocation, by string) (int, error) {
	n := 0
	for _, m := range moved {
		rel, err := filepath.Rel(filepath.Dir(filepath.Join(root, m.File)), filepath.Join(root, m.Now))
		if err != nil {
			return n, err
		}
		if _, err := Amend(st, dl, m.SourceID,
			map[string]any{"doc": filepath.ToSlash(rel)}, by,
			"document moved: "+m.Was+" → "+m.Now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ApplyRelocations rewrites `doc:` in the declaring FILE.
//
// The legacy path, for a corpus that has not migrated. `ApplyRelocationsTo` is
// the one that repairs a store.
func ApplyRelocations(root string, moved []Relocation) (int, error) {
	byFile := map[string][]Relocation{}
	for _, m := range moved {
		if m.File == "" {
			return 0, fmt.Errorf("relocation for %q names no declaring file", m.SourceID)
		}
		byFile[m.File] = append(byFile[m.File], m)
	}
	n := 0
	for file, rs := range byFile {
		abs := filepath.Join(root, file)
		b, err := os.ReadFile(abs)
		if err != nil {
			return n, err
		}
		txt := string(b)
		for _, m := range rs {
			rel, err := filepath.Rel(filepath.Dir(filepath.Join(root, file)), filepath.Join(root, m.Now))
			if err != nil {
				return n, err
			}
			rel = filepath.ToSlash(rel)
			// Anchor on the id so a shared filename cannot rewrite the wrong node.
			idx := strings.Index(txt, "- id: "+m.SourceID+"\n")
			if idx < 0 {
				return n, fmt.Errorf("%s: no node %q", file, m.SourceID)
			}
			end := strings.Index(txt[idx:], "\n- id: ")
			if end < 0 {
				end = len(txt) - idx
			}
			block := txt[idx : idx+end]
			old := "doc: " + m.Was
			// `doc:` is stored relative to the declaring file, so match on the tail.
			replaced := false
			for _, cand := range []string{old, "doc: " + filepath.Base(m.Was)} {
				if strings.Contains(block, cand) {
					txt = txt[:idx] + strings.Replace(block, cand, "doc: "+rel, 1) + txt[idx+end:]
					replaced = true
					break
				}
			}
			if !replaced {
				// Fall back to whatever `doc:` the block actually carries.
				di := strings.Index(block, "doc: ")
				if di < 0 {
					return n, fmt.Errorf("%s: node %q has no `doc:`", file, m.SourceID)
				}
				eol := strings.Index(block[di:], "\n")
				if eol < 0 {
					eol = len(block) - di
				}
				txt = txt[:idx] + block[:di] + "doc: " + rel + block[di+eol:] + txt[idx+end:]
			}
			n++
		}
		if err := os.WriteFile(abs, []byte(txt), 0o644); err != nil {
			return n, err
		}
	}
	return n, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".raglit", "node_modules", ".kg":
		return true
	}
	return false
}

// IndexDirOf is indexDirOf, exported for the CLI: the sidecar lives beside the
// facts, so a caller outside the package needs to know where that is.
func IndexDirOf(g *Graph) string { return indexDirOf(g) }

// IndexDirKnown is IndexDirOf with the answer's CONFIDENCE, and callers that
// cannot tolerate a guess must use it.
//
// `IndexDirOf` returns "" for two different things: the default index, which
// legitimately lives at the repo root, and "I could not tell, because there are
// no nodes to tell from". That ambiguity is not theoretical — it is exactly how
// the liveness check came to look for its watermark in the wrong directory and
// fail in the one case it was written for, an index that loaded NOTHING.
//
// ok is false when the graph has no positioned node, which is when the answer is
// a fallback rather than a derivation.
func IndexDirKnown(g *Graph) (string, bool) {
	for _, n := range g.Nodes {
		if n.File != "" {
			return indexDirOf(g), true
		}
	}
	return "", false
}

// indexDirOf finds the directory the index's facts live in, so the search stays
// inside the closed graph rather than sweeping the whole root.
func indexDirOf(g *Graph) string {
	best := ""
	for _, n := range g.Nodes {
		if n.File == "" {
			continue
		}
		d := filepath.Dir(n.File)
		if best == "" || len(d) < len(best) {
			best = d
		}
	}
	return best
}

func fileOfSource(g *Graph, id string) string {
	if n, ok := g.Nodes[id]; ok {
		return n.File
	}
	return ""
}
