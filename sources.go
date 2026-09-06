package kgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A fact is only as good as the document it was read from, and documents move:
// an exhibit is re-scanned, a statement is corrected, a PDF is re-exported with
// different pagination. Nothing in the graph notices, because the fact text did
// not change — and a filed document then cites a page that no longer says what
// it said.
//
// So the version of each document is recorded, and drift from that version is
// reported. Three things this deliberately is NOT:
//
// It is NOT part of `sem_hash`. Provenance in the semantic hash would mean
// re-exporting one PDF flags every document that renders any fact it attests,
// which is the false-flag storm the hash exists to avoid. Drift is a separate
// signal, folded into a spec's state where it belongs.
//
// It is NOT stored in the `.kfacts.md` files. Those are the authoritative fact
// text; kgraph writing into them to record a hash is the wrong direction, and a
// hand-typed hash is unusable anyway.
//
// It is NOT in SQLite, which is derived and disposable here and a corruption
// hazard on a Syncthing share. The lock is text, it commits, and its diff is the
// review event: "this exhibit changed" is exactly what a person needs to see.
//
// And it is NOT one file at the repo root, which is what it was first. A corpus
// has mixed sensitivity: `~/life` gitignores an entire project as "Medical PII —
// keep OUT of git history", and a root-level lock recording
// `projects/.../documents/eob-2026-05-27.pdf` would have put those filenames and
// dates into history anyway. One lock per directory of facts inherits that
// directory's git treatment automatically, which is the only arrangement that
// cannot leak by accident.
//
// Entries are keyed relative to the LOCK's own directory, so a lock reads exactly
// like the `doc:` lines beside it and keeps working if the project moves.
const lockName = "sources.lock"

// legacyLockRel is the old single root lock. Detected and reported, never read:
// silently ignoring a file the user believes is protecting them is worse than
// telling them it is stale.
const legacyLockRel = ".kgraph/sources.lock"

// LockDirOf is the directory whose lock records this source, which is the
// directory of the file that declares it.
func LockDirOf(n *Node) string {
	return filepath.ToSlash(filepath.Dir(n.File))
}

// SourceState is what we know about a declared document, right now.
type SourceState string

const (
	// SrcOK is locked and unchanged.
	SrcOK SourceState = "ok"
	// SrcChanged is the signal this whole file exists for: the document is not
	// the one the facts were read from.
	SrcChanged SourceState = "changed"
	// SrcVanished was locked with a hash and is now gone. The citation has no
	// referent.
	SrcVanished SourceState = "vanished"
	// SrcUnrecorded is present but never locked, so which version was read is
	// unknown. `kg source lock` settles it.
	SrcUnrecorded SourceState = "unrecorded"
	// SrcNotFound is declared, never locked, and not here. NOT a scan
	// diagnostic: a typo and an exhibit that lives on another machine look
	// identical from here, so reporting it as a problem would either flood a
	// corpus that keeps its evidence elsewhere or hide the typo in that flood.
	// `kg source status` lists these, which is where the typo shows up.
	SrcNotFound SourceState = "not-found"
	// SrcOutsideProject is a `doc:` path that escapes the project. Always an
	// error — an authored file must not aim the hasher at an arbitrary path.
	SrcOutsideProject SourceState = "outside-project"
)

// LockEntry is one recorded document version.
type LockEntry struct {
	Hash string
	Size int64
	At   string
}

// SourceLock maps a project-relative document path to the version recorded for
// it. Keyed by path rather than by source id: the path is what gets hashed, and
// two source nodes citing one document must not disagree about its version.
type SourceLock struct {
	Entries map[string]LockEntry
}

// SourceStatus is the resolved state of one source node's document.
type SourceStatus struct {
	ID    string      `json:"id"`
	Doc   string      `json:"doc"` // project-relative
	State SourceState `json:"state"`
	Hash  string      `json:"hash,omitempty"`
	Was   string      `json:"was,omitempty"` // the locked hash, when it drifted
	Facts int         `json:"facts"`         // how many facts rest on it
}

// DocPathOf resolves a source's `doc:` to a project-relative path.
//
// It resolves against the DECLARING FILE's directory, not the project root. A
// corpus keeps each project's evidence beside that project's facts, and the root
// is usually the repository above them all — resolving against the root would
// send every lookup to the wrong place, and moving a project would break every
// path in it.
func DocPathOf(root string, n *Node) (string, error) {
	if n.Source == nil || n.Source.DocPath == "" {
		return "", nil
	}
	doc := n.Source.DocPath
	if filepath.IsAbs(doc) {
		return "", fmt.Errorf("`doc:` of %q is an absolute path", n.ID)
	}
	// A trailing slash names a directory, and that is certain from the text alone
	// — no filesystem needed, so it is caught on every machine including one that
	// does not hold the evidence. A citation to a folder names no exhibit: it
	// cannot be hashed, and it cannot be cited in a filing.
	if strings.HasSuffix(doc, "/") {
		return "", fmt.Errorf("`doc:` of %q names a directory (%s) — cite the document, not the folder it is in", n.ID, doc)
	}
	rel := filepath.Clean(filepath.Join(filepath.Dir(n.File), doc))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("`doc:` of %q escapes the project", n.ID)
	}
	return filepath.ToSlash(rel), nil
}

// HashDoc hashes the document's raw bytes.
//
// Raw, not normalized. We cannot tell a cosmetic reflow from a substantive edit
// in an arbitrary file, and the two errors are not symmetric: a false flag costs
// one re-read, a missed change costs a document citing evidence that no longer
// says what was cited. `kg source lock` is the one-command way to accept drift
// once it has been looked at.
func HashDoc(abs string) (string, int64, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), n, nil
}

// ReadLock loads one directory's lock. An absent lock is not an error — it is a
// project that has not recorded its evidence yet, and every source in it reports
// `unrecorded`.
func ReadLock(root, dir string) (*SourceLock, error) {
	l := &SourceLock{Entries: map[string]LockEntry{}}
	rel := filepath.Join(dir, lockName)
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// A path may contain spaces — email exports routinely do — so a quoted
		// path is read whole. Whitespace splitting alone turned
		// `Fw_ Easement Agreement ... .txt` into a parse error.
		path, tail, err := splitLockPath(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", rel, i+1, err)
		}
		f := strings.Fields(tail)
		if len(f) < 2 {
			return nil, fmt.Errorf("%s:%d: want `path hash size [date]`", rel, i+1)
		}
		size, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: bad size %q", rel, i+1, f[1])
		}
		e := LockEntry{Hash: f[0], Size: size}
		if len(f) > 2 {
			e.At = f[2]
		}
		l.Entries[path] = e
	}
	return l, nil
}

// splitLockPath reads the leading path, quoted if it contains whitespace.
func splitLockPath(line string) (path, rest string, err error) {
	if !strings.HasPrefix(line, `"`) {
		p, r, _ := strings.Cut(line, " ")
		return p, r, nil
	}
	end := strings.Index(line[1:], `"`)
	if end < 0 {
		return "", "", fmt.Errorf("unterminated quoted path")
	}
	return line[1 : 1+end], line[2+end:], nil
}

// quoteLockPath quotes a path that would otherwise split.
func quoteLockPath(p string) string {
	if strings.ContainsAny(p, " \t\"") {
		return `"` + strings.ReplaceAll(p, `"`, "") + `"`
	}
	return p
}

// WriteLock writes one directory's lock, sorted, one document per line, so its
// diff reads as a list of exhibits that changed.
func WriteLock(root, dir string, l *SourceLock) error {
	var b strings.Builder
	b.WriteString("# kgraph source lock — the version of each document the facts here were read from.\n")
	b.WriteString("# Written by `kg source lock`. A diff here is a document to re-read, not a merge conflict.\n")
	b.WriteString("# Paths are relative to this file, so this lock shares the git treatment of the\n")
	b.WriteString("# facts beside it — a gitignored project's lock is gitignored with it.\n")
	b.WriteString("# path  hash  size  recorded\n")
	paths := make([]string, 0, len(l.Entries))
	for p := range l.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		e := l.Entries[p]
		fmt.Fprintf(&b, "%s %s %d %s\n", quoteLockPath(p), e.Hash, e.Size, e.At)
	}
	abs := filepath.Join(root, dir, lockName)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if len(l.Entries) == 0 {
		// Nothing to record: do not leave an empty file implying it was checked.
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(abs, []byte(b.String()), 0o644)
}

// LockSet holds one lock per directory of facts, keyed by that directory.
type LockSet map[string]*SourceLock

// ReadLocks loads the lock beside every directory that declares a source.
func ReadLocks(root string, g *Graph) (LockSet, error) {
	out := LockSet{}
	for _, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath == "" {
			continue
		}
		dir := LockDirOf(n)
		if _, done := out[dir]; done {
			continue
		}
		l, err := ReadLock(root, dir)
		if err != nil {
			return nil, err
		}
		out[dir] = l
	}
	return out, nil
}

// Dirs are the directories holding a lock, sorted.
func (ls LockSet) Dirs() []string {
	out := make([]string, 0, len(ls))
	for d := range ls {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// SourceStatuses resolves every source node against its directory's lock and the
// filesystem.
func (g *Graph) SourceStatuses(root string, locks LockSet) ([]SourceStatus, []Diag) {
	var out []SourceStatus
	var diags []Diag

	rests := map[string]int{}
	for _, e := range g.Edges {
		if e.Type == EAttests {
			rests[e.Src]++
		}
	}

	var ids []string
	for id, n := range g.Nodes {
		if n.Kind == KSource && n.Source != nil && n.Source.DocPath != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	for _, id := range ids {
		n := g.Nodes[id]
		st := SourceStatus{ID: id, Facts: rests[id]}
		rel, err := DocPathOf(root, n)
		if err != nil {
			st.State = SrcOutsideProject
			diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevError, Msg: err.Error()})
			out = append(out, st)
			continue
		}
		st.Doc = rel
		// Keyed relative to the lock's own directory, so the lock reads exactly
		// like the `doc:` lines beside it.
		dir := LockDirOf(n)
		key := lockKey(dir, rel)
		var locked LockEntry
		var isLocked bool
		if l := locks[dir]; l != nil {
			locked, isLocked = l.Entries[key]
		}
		st.Was = locked.Hash

		hash, _, herr := HashDoc(filepath.Join(root, rel))
		switch {
		case herr != nil && isLocked:
			st.State = SrcVanished
			diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"the document %q attests is gone (%s) — %d fact(s) cite something that "+
					"cannot be produced", id, rel, rests[id])})
		case herr != nil:
			st.State = SrcNotFound
		case !isLocked:
			st.State = SrcUnrecorded
			st.Hash = hash
			diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"%q has never been recorded — run `kg source lock` so a later change to %s "+
					"is detectable", id, rel)})
		case hash != locked.Hash:
			st.State = SrcChanged
			st.Hash = hash
			diags = append(diags, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"%s changed since %q was read from it — re-read it and check the %d fact(s) "+
					"resting on it, then `kg source lock`", rel, id, rests[id])})
		default:
			st.State = SrcOK
			st.Hash = hash
		}
		out = append(out, st)
	}
	return out, diags
}

// lockKey turns a root-relative document path into one relative to its lock.
func lockKey(dir, rel string) string {
	if dir == "." || dir == "" {
		return rel
	}
	if k, ok := strings.CutPrefix(rel, dir+"/"); ok {
		return k
	}
	return rel
}

// CheckSourceDrift records drift on the graph so specs can report it. Called
// after load, because Build is pure and must stay usable without a filesystem;
// a nil map means "not checked", never "nothing drifted".
func (g *Graph) CheckSourceDrift(root string) []Diag {
	locks, err := ReadLocks(root, g)
	if err != nil {
		return []Diag{{File: lockName, Line: 0, Severity: SevError, Msg: err.Error()}}
	}
	sts, diags := g.SourceStatuses(root, locks)
	// A stale root lock is reported, never read. Silently ignoring a file the
	// user believes is protecting them is worse than telling them it is dead.
	if _, err := os.Stat(filepath.Join(root, legacyLockRel)); err == nil {
		diags = append(diags, Diag{File: legacyLockRel, Line: 0, Severity: SevWarn, Msg: "this is the old single root lock and is no longer read — locks now live beside " +
			"the facts that declare them, so each inherits its project's git treatment. " +
			"Run `kg source lock`, then delete this file"})
	}
	g.SourceDrift = map[string]SourceState{}
	for _, st := range sts {
		switch st.State {
		case SrcChanged, SrcVanished:
			g.SourceDrift[st.ID] = st.State
		}
	}
	return diags
}

// DriftedSources returns the drifted sources a node's facts rest on, following
// `attests` and, for an inference, its premises — an inference is only as good
// as the facts under it, so a moved exhibit two hops down still reaches the
// document that rendered the conclusion.
func (g *Graph) DriftedSources(id string) []string {
	if len(g.SourceDrift) == 0 {
		return nil
	}
	seen := map[string]bool{}
	hits := map[string]bool{}
	var walk func(string, int)
	walk = func(cur string, depth int) {
		if seen[cur] || depth > 8 {
			return
		}
		seen[cur] = true
		for _, e := range g.incident[cur] {
			if e.Type != EAttests || e.Dst != cur {
				continue
			}
			if g.SourceDrift[e.Src] != "" {
				hits[e.Src] = true
			}
			// An inference's premises are facts; a document under one of them
			// having moved undercuts the inference too.
			if src := g.Nodes[e.Src]; src != nil && src.Source != nil {
				for _, p := range src.Source.Premises {
					walk(p, depth+1)
				}
			}
		}
	}
	walk(id, 0)
	out := make([]string, 0, len(hits))
	for s := range hits {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// LockAll records the current version of every present document. Absent ones are
// left out rather than recorded as absent: the lock states what we have read, and
// an entry claiming to have read nothing is worse than no entry.
// LockAll records the current version of every source that needs one.
//
// `only` NARROWS it to the named sources, and that is not a convenience — it is
// the fix for a defect that destroyed evidence twice. Locking one NEW document
// silently re-locked every `changed` one too, so a drift signal a person was
// midway through investigating vanished on an unrelated lock and had to be
// restored from git by hand. Empty `only` keeps the sweep, which is what an
// initial lock of a corpus wants.
func LockAll(root string, g *Graph, only ...string) (LockSet, []SourceStatus, error) {
	locks, err := ReadLocks(root, g)
	if err != nil {
		return nil, nil, err
	}
	dirOf := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == KSource && n.Source != nil && n.Source.DocPath != "" {
			dirOf[n.ID] = LockDirOf(n)
		}
	}
	sts, _ := g.SourceStatuses(root, locks)
	named := map[string]bool{}
	for _, id := range only {
		named[id] = true
	}
	now := time.Now().UTC().Format("2006-01-02")
	for _, st := range sts {
		if st.Doc == "" {
			continue
		}
		if len(named) > 0 && !named[st.ID] {
			continue
		}
		switch st.State {
		case SrcUnrecorded, SrcChanged:
			hash, size, herr := HashDoc(filepath.Join(root, st.Doc))
			if herr != nil {
				continue
			}
			dir := dirOf[st.ID]
			if locks[dir] == nil {
				locks[dir] = &SourceLock{Entries: map[string]LockEntry{}}
			}
			locks[dir].Entries[lockKey(dir, st.Doc)] = LockEntry{Hash: hash, Size: size, At: now}
		}
	}
	// A vanished document keeps its entry. Dropping it would silently turn "the
	// exhibit disappeared" into "we never had it".
	for _, dir := range locks.Dirs() {
		if err := WriteLock(root, dir, locks[dir]); err != nil {
			return nil, nil, err
		}
	}
	after, _ := g.SourceStatuses(root, locks)
	return locks, after, nil
}

// CheckIndexBoundary reports sources whose `doc:` resolves outside the index.
//
// An index is a closed graph, and its evidence should be too. `DocPathOf` only
// refuses a path that escapes the project ROOT, so a `doc:` reaching into a
// sibling directory — or into another matter — passes silently. On this corpus
// `../../checklists/action-queue.md` did exactly that: a living todo list from
// outside the index became the evidentiary source for ten facts, and because it
// changes constantly it kept the drift signal permanently tripped.
//
// A WARNING rather than an error, and the distinction is not timidity. Shared
// authority is the legitimate case: statutes, regulations and reported opinions
// belong to no single matter, and a firm-wide library of them under one root that
// several indexes cite is a reasonable arrangement. What is never reasonable is
// doing it by accident, which is what this reports.
func (g *Graph) CheckIndexBoundary(root string) []Diag {
	dir := indexDirOf(g)
	if dir == "" {
		return nil
	}
	base := filepath.Clean(filepath.Join(root, dir))
	var out []Diag
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath == "" {
			continue
		}
		rel, err := DocPathOf(root, n)
		if err != nil || rel == "" {
			continue
		}
		abs := filepath.Clean(filepath.Join(root, rel))
		if abs == base || strings.HasPrefix(abs, base+string(filepath.Separator)) {
			continue
		}
		out = append(out, Diag{File: n.File, Line: n.Line, Severity: SevWarn, Msg: fmt.Sprintf(
			"`doc:` of %q resolves outside this index (%s) — an index is a closed graph and "+
				"its evidence should be too. Shared authority (statutes, regulations, reported "+
				"opinions) legitimately lives outside a matter; anything else reaching out is "+
				"usually an accident, and a document that is not part of this matter will drift "+
				"under it", id, rel)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}
