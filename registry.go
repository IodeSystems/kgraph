package kgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// One daemon serves many projects. A scope is a (root, branch) pair: the same
// repository on two branches is two different graphs, and a document generated
// on one branch must not be reported fresh on the other.
type Scope struct {
	Root   string `json:"root"`
	Branch string `json:"branch"`
	// Index is the closed graph this scope holds, resolved from the caller's own
	// directory the way the CLI resolves it. Reported because a viewer serving a
	// different index than `kg` answers from is the failure this field exists to
	// make visible.
	Index    string `json:"index"`
	Nodes    int    `json:"nodes"`
	Edges    int    `json:"edges"`
	Specs    int    `json:"specs"`
	Errors   int    `json:"errors"`
	LoadedAt string `json:"loaded_at"`

	project     *Project
	diags       []Diag
	fingerprint string
	// docs are the evidence paths this load declared, so the NEXT fingerprint can
	// stat them. A document not yet declared cannot be watched, which is why a
	// newly declared one is picked up by the fact-file change that declared it.
	docs []string
}

// Registry caches loaded projects and reloads only when the files under a scope
// actually change. A full rebuild is cheap at this scale, so invalidation is a
// stat sweep rather than incremental view maintenance.
type Registry struct {
	mu       sync.Mutex
	scopes   map[string]*Scope
	watchers map[string]*watcher
}

func NewRegistry() *Registry { return &Registry{scopes: map[string]*Scope{}} }

// scopeKey identifies one loaded graph. An INDEX is part of it, not only a root
// and a branch: one checkout holds several closed graphs, and keying without the
// index made two indexes under one root share a cache entry — the first one
// loaded answered for both.
func scopeKey(root, branch, index string) string {
	return root + "\x00" + branch + "\x00" + index
}

// DiscoverRoot walks up from dir for a project marker: `.kgraph/` first, then a
// git root. Mirrors raglit's DiscoverHome so routing is implicit from cwd.
//
// The home directory is never a root, however it is marked. The daemon keeps its
// own state in `~/.kgraph/`, which is the exact string this looks for — so a
// project anywhere under $HOME resolved to $HOME and every request then walked
// the entire home directory. That was self-inflicted and severe: it is slow, it
// reads far outside the corpus, and it fails on the first unreadable directory
// anywhere in it. Daemon state now lives elsewhere (see statePath), and this
// refuses $HOME regardless, because a corpus rooted at the home directory is not
// a thing anyone means.
func DiscoverRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	for _, marker := range []string{".kgraph", ".git"} {
		d := abs
		for {
			if d != home {
				if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
					return d, nil
				}
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	return abs, nil
}

// Branch reads the checked-out branch. A detached HEAD keys by its sha, so a
// bisect or a detached review still gets its own scope rather than silently
// reusing another branch's pins.
func Branch(root string) string {
	b, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err != nil {
		return "-"
	}
	s := strings.TrimSpace(string(b))
	if ref, ok := strings.CutPrefix(s, "ref: refs/heads/"); ok {
		return ref
	}
	if len(s) >= 12 {
		return "detached-" + s[:12]
	}
	return "-"
}

// fingerprint stats every file whose content can change an answer. Only paths,
// sizes and mtimes are read, so an unchanged scope costs a directory walk and
// nothing more.
//
// `extra` carries the paths that are not `*.kfacts.md`/`*.kgraph.md` but still
// decide what the daemon reports: the evidence documents, and the two files under
// `.kgraph/`. Walking for those is not an option — the walk deliberately skips
// `.kgraph/`, and the documents are ordinary files anywhere in the tree that only
// the loaded graph can identify.
//
// Missing this is a specific and quiet failure: an exhibit is re-scanned, every
// fact still reads the same, the fingerprint does not move, and the daemon keeps
// answering `fresh` for a document that is not. The CLI reloads every run and
// would have looked correct throughout.
func fingerprint(root string, extra []string) (string, error) {
	h := sha256.New()
	var paths []string
	var markers []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory must not fail the whole scope. A real corpus
			// contains permission-denied corners, and a fingerprint that errors out
			// makes every request 400 rather than degrading.
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".kgraph", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		n := d.Name()
		if strings.HasSuffix(n, FactsSuffix) || strings.HasSuffix(n, SpecSuffix) || n == lockName {
			paths = append(paths, p)
		}
		// A marker both declares an index and locates the dialects that index owns,
		// which live under a `.kgraph/` the walk is about to skip. Collected here
		// because this is the only pass that knows where the projects are.
		if n == IndexMarker {
			paths = append(paths, p)
			markers = append(markers, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	// The named-query library changes answers and lives under the directory the
	// walk skips. Lock files sit beside the facts and are picked up by the walk.
	paths = append(paths, filepath.Join(root, namedQueriesRel))
	// The house style is instructions, so editing it changes every generated
	// document. Same directory, same reason it has to be named by hand.
	paths = append(paths, filepath.Join(root, styleRel))
	// An authored dialect decides which of two conflicting sources wins and how
	// the document says so, so editing one changes answers.
	// Every project owns dialects too, under its own `.kgraph/`. Both the
	// corpus-wide directory and each project's are stat'd as a DIRECTORY as well
	// as file by file: adding a dialect is as much a change as editing one, and a
	// stat of the directory catches both.
	for _, dir := range append([]string{root}, markers...) {
		dd := filepath.Join(dir, dialectsDir)
		paths = append(paths, dd)
		if ents, derr := os.ReadDir(dd); derr == nil {
			for _, e := range ents {
				if !e.IsDir() {
					paths = append(paths, filepath.Join(dd, e.Name()))
				}
			}
		}
	}
	for _, rel := range extra {
		paths = append(paths, filepath.Join(root, rel))
	}
	sort.Strings(paths)
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			// Absence is part of the fingerprint: a document appearing or
			// vanishing changes what we report about it.
			fmt.Fprintf(h, "%s\x00absent\n", p)
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, st.Size(), st.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// Get returns the scope for a directory, reloading only if something changed.
func (r *Registry) Get(dir string) (*Scope, error) {
	root, err := DiscoverRoot(dir)
	if err != nil {
		return nil, err
	}
	branch := Branch(root)
	// THE INDEX OWNING THE CALLER'S DIRECTORY, exactly as the CLI resolves it.
	//
	// This called `Load(root)` — the DEFAULT index — while `kg` resolved the
	// marker, so `kg daemon`, `kg serve` and `kg scopes` answered from a
	// different graph than the CLI did, in the same checkout. In a corpus whose
	// facts all live under markers that graph is EMPTY, and the viewer reported a
	// matter with nothing in it rather than reporting an error.
	//
	// A marker that will not parse is an error rather than a fallback: falling
	// back to the default index here is precisely what produced a confident wrong
	// answer.
	index, err := IndexAt(root, dir)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	key := scopeKey(root, branch, index)
	var known []string
	if sc, ok := r.scopes[key]; ok {
		known = sc.docs
	}
	fp, err := fingerprint(root, known)
	if err != nil {
		return nil, err
	}
	if sc, ok := r.scopes[key]; ok && sc.fingerprint == fp {
		return sc, nil
	}
	p, diags, err := LoadIn(root, index)
	if err != nil {
		return nil, err
	}
	sc := &Scope{
		Root: root, Branch: branch, Index: index,
		Nodes: len(p.Graph.Nodes), Edges: len(p.Graph.Edges), Specs: declaredCount(p),
		Errors:   len(Errors(diags)),
		LoadedAt: time.Now().UTC().Format(time.RFC3339),
		project:  p, diags: diags, fingerprint: fp,
		docs: declaredDocs(root, p.Graph),
	}
	// The declared set may have grown during this load, so re-fingerprint with it.
	// Otherwise a newly declared document is unwatched until the next change.
	if fp2, err := fingerprint(root, sc.docs); err == nil {
		sc.fingerprint = fp2
	}
	r.scopes[key] = sc
	go r.Remember(root)
	return sc, nil
}

// declaredCount is how many declared queries a project holds, from whichever
// door declared them — spec queries while specs exist, standing queries after.
func declaredCount(p *Project) int {
	n := 0
	for _, s := range p.DeclaredSets() {
		n += len(s.Queries)
	}
	return n
}

// declaredDocs lists the project-relative evidence paths the graph declares.
func declaredDocs(root string, g *Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath == "" {
			continue
		}
		if rel, err := DocPathOf(root, n); err == nil && rel != "" {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// Scopes lists what the daemon currently holds.
func (r *Registry) Scopes() []*Scope {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Scope, 0, len(r.scopes))
	for _, sc := range r.scopes {
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Root != out[j].Root {
			return out[i].Root < out[j].Root
		}
		return out[i].Branch < out[j].Branch
	})
	return out
}

// Project exposes the loaded project. Callers must not mutate it; `--what-if`
// clones first.
func (s *Scope) Project() *Project { return s.project }

// Diags are the scan findings for this scope.
func (s *Scope) Diags() []Diag { return s.diags }

// Err reports a scope that cannot be trusted for anything but reporting.
func (s *Scope) Err() error {
	if s.Errors == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s@%s has %d error(s):\n", s.Root, s.Branch, s.Errors)
	for _, d := range Errors(s.diags) {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	return fmt.Errorf("%s", b.String())
}

// ── discovery ──────────────────────────────────────────────────────────

// Candidate is a project the console can offer without the user knowing a path.
type Candidate struct {
	Root   string `json:"root"`
	Branch string `json:"branch"`
	Facts  int    `json:"facts"`
	Specs  int    `json:"specs"`
	Loaded bool   `json:"loaded"`
}

// Discover walks under a directory for kgraph projects: any directory holding a
// `*.kfacts.md`, resolved to its project root and deduped.
//
// A path typed into a box is not a discoverable interface. The daemon already
// knows how to resolve a root, so it can answer "what is here" instead of
// making someone guess.
func Discover(under string, maxDepth int) ([]Candidate, error) {
	return DiscoverWithin(under, maxDepth, 4*time.Second)
}

// DiscoverWithin bounds the walk by BOTH depth and elapsed time. Depth alone is
// a blunt instrument — a project nested one level deeper than the guess simply
// vanishes, with nothing to say why — so the depth default is generous and the
// clock is what actually protects the console from a huge tree.
func DiscoverWithin(under string, maxDepth int, budget time.Duration) ([]Candidate, error) {
	if maxDepth <= 0 {
		maxDepth = 8
	}
	deadline := time.Now().Add(budget)
	abs, err := filepath.Abs(under)
	if err != nil {
		return nil, err
	}
	base := strings.Count(filepath.Clean(abs), string(filepath.Separator))
	roots := map[string]*Candidate{}

	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, do not fail the whole walk
		}
		if time.Now().After(deadline) {
			return fs.SkipAll
		}
		if d.IsDir() {
			n := d.Name()
			if n != "." && strings.HasPrefix(n, ".") && n != ".kgraph" {
				return fs.SkipDir
			}
			switch n {
			case "node_modules", "vendor", ".git", "target", "dist", "build",
				"Library", "Applications", "snap", "go":
				return fs.SkipDir
			}
			if strings.Count(filepath.Clean(p), string(filepath.Separator))-base > maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		// A project is found by its FACTS or its DECLARATIONS, and both have moved
		// house: a migrated project's facts are its log, and its declarations are
		// `standing.yaml` now that `*.kgraph.md` is retired. Looking only for
		// `*.kfacts.md` made every migrated corpus report zero facts —
		// discoverable by its specs, and then wrong about what was in it. Dropping
		// the standing file would be the same mistake in the other half.
		if !strings.HasSuffix(d.Name(), FactsSuffix) && !strings.HasSuffix(d.Name(), SpecSuffix) &&
			d.Name() != assertName && d.Name() != standingName {
			return nil
		}
		root, rerr := DiscoverRoot(filepath.Dir(p))
		if rerr != nil {
			return nil
		}
		c := roots[root]
		if c == nil {
			c = &Candidate{Root: root, Branch: Branch(root)}
			roots[root] = c
		}
		switch {
		case strings.HasSuffix(d.Name(), FactsSuffix), d.Name() == assertName:
			c.Facts++
		case d.Name() == standingName:
			// One file holds every standing query, so count the DECLARATIONS in it
			// rather than the file. A count of 1 would say nothing about a corpus
			// with 289 questions in it.
			if set, rerr := ReadStanding(root, strings.TrimPrefix(
				strings.TrimPrefix(filepath.Dir(p), root), string(filepath.Separator))); rerr == nil {
				c.Specs += len(set)
			}
		default:
			c.Specs++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(roots))
	for _, c := range roots {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out, nil
}

// knownPath is where the daemon remembers projects between restarts, so the
// console is not empty every time it comes up.
func knownPath() string { return statePath("known.json") }

// statePath is where the daemon keeps its own state. Deliberately NOT
// `~/.kgraph/`: that is the project marker DiscoverRoot searches for, so putting
// state there made the home directory look like a project root.
func statePath(name string) string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "kgraph", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "kgraph", name)
}

// Remember records a root. Best-effort: failing to persist must never fail a
// request.
func (r *Registry) Remember(root string) {
	p := knownPath()
	if p == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := readKnown(p)
	for _, k := range seen {
		if k == root {
			return
		}
	}
	seen = append(seen, root)
	sort.Strings(seen)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	if b, err := json.Marshal(seen); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

// Known lists remembered roots that still exist.
func (r *Registry) Known() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, k := range readKnown(knownPath()) {
		if st, err := os.Stat(k); err == nil && st.IsDir() {
			out = append(out, k)
		}
	}
	return out
}

func readKnown(p string) []string {
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal(b, &out)
	return out
}
