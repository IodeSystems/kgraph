package kgraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Discovery is by glob, not by a registry: there is no index to keep in sync,
// and a fact file is found wherever its subject lives.
const (
	FactsSuffix = ".kfacts.md"
	SpecSuffix  = ".kgraph.md"
)

// findFilesBySuffix walks root, skipping VCS and derived directories. It is the
// unfiltered sweep; callers that build a graph must narrow it to one index.
func findFilesBySuffix(root, suffix string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".kgraph", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), suffix) {
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				rel = p
			}
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// FindFacts returns the *.kfacts.md belonging to index. Filtering happens here,
// at discovery, rather than after Build: a graph is only ever constructed from
// one index's documents, so no cross-index edge can exist to be reasoned over.
// That is what closes `disputed` and `SemHash` over the index.
func FindFacts(root, index string) ([]string, error) {
	return findInIndex(root, FactsSuffix, index)
}

func findInIndex(root, suffix, index string) ([]string, error) {
	all, err := findFilesBySuffix(root, suffix)
	if err != nil {
		return nil, err
	}
	out := all[:0:0]
	var bad []error
	for _, rel := range all {
		idx, ierr := indexOfFile(root, rel)
		if ierr != nil {
			// A marker that will not parse leaves membership unknown, and a file of
			// unknown membership cannot be silently dropped or silently included.
			bad = append(bad, ierr)
			continue
		}
		if idx == index {
			out = append(out, rel)
		}
	}
	if len(bad) > 0 {
		return nil, joinUnique(bad)
	}
	return out, nil
}

// ScanFacts builds the graph for the default index. Corpora that predate indexes
// are entirely in the default index, so this keeps working unchanged.
func ScanFacts(root string) (*Graph, []Diag, error) {
	return ScanFactsIn(root, DefaultIndex)
}

// FindFactLogs returns the exported assertion logs belonging to index.
//
// AN INDEX IS DISCOVERABLE BY ITS EXPORT, not only by its fact files. Discovery
// keyed off `*.kfacts.md` alone, which was invisible until a corpus had
// migrated: with the files deleted the index had nothing to be found BY, so it
// vanished rather than loading from its own log. That is the coupling that has
// to go before the parser can.
func FindFactLogs(root, index string) ([]string, error) {
	return findInIndex(root, assertName, index)
}

// ScanFactsIn parses one index's facts and builds its graph, reading the store
// the DEFAULT POLICY names — `~/.kgraph/<index>.db` if it exists, else the
// exported logs in the corpus. Every existing caller keeps exactly this.
func ScanFactsIn(root, index string) (*Graph, []Diag, error) {
	return scanFactsIn(root, index, nil, true)
}

// ScanFactsInWith builds an index's graph from a store the CALLER names.
//
// The location is a policy, not the only answer, and this is the seam that says
// so: `StorePath` had exactly two callers and the one on this path was hardcoded
// policy in the middle of a library function, so a consumer keeping its store
// beside its own corpus got a graph loaded from somebody else's database.
//
// A NIL STORE MEANS LOG ONLY — do not look for a database — which is what a
// corpus handed to somebody else is. See `StoreNone`.
func ScanFactsInWith(root, index string, st Store) (*Graph, []Diag, error) {
	return scanFactsIn(root, index, st, false)
}

func scanFactsIn(root, index string, st Store, useDefault bool) (*Graph, []Diag, error) {
	paths, err := FindFacts(root, index)
	if err != nil {
		return nil, nil, err
	}
	logs, err := FindFactLogs(root, index)
	if err != nil {
		return nil, nil, err
	}
	var docs []*Doc
	var diags []Diag
	// THE DIALECT IS RESOLVED BEFORE PARSING, not after. `class:` is refused at
	// parse time against the ladder, so parsing under the wrong vocabulary
	// rejects a corpus's own words — the rung exists, just not in the dialect the
	// parser happened to hold. Every later consumer can be corrected; a diagnostic
	// already emitted cannot.
	//
	// JOINED TO ROOT, and the bug this fixes was invisible in every test.
	// `indexDirOfPaths` answers in ROOT-RELATIVE paths — `ReadAttestations` and
	// `FetchRelations` below both join root before using it — while `DialectAt`
	// takes a real directory and resolves it against the PROCESS WORKING
	// DIRECTORY. So a scan run from anywhere but the corpus root walked up from
	// `$PWD/<index>`, found no marker, and fell back to the default index and
	// therefore to the built-in dialect. Silently: the declared dialect's own
	// subtypes then come back as `unknown subtype`, naming the dialect the corpus
	// did not ask for. Tests never saw it because a test's working directory is
	// the package under test and its corpora are absolute temp dirs, which
	// happens to make the wrong resolution unreachable.
	// The index's directory comes from whatever it HAS: its fact files while it
	// has them, its export once it does not.
	home := indexDirOfPaths(paths)
	if len(paths) == 0 && len(logs) > 0 {
		home = indexDirOfPaths(logs)
	}
	dl, derr := DialectAt(root, filepath.Join(root, home))
	if derr != nil {
		return nil, nil, derr
	}
	// THE STORE IS THE SOURCE OF TRUTH WHEN IT HAS ANYTHING IN IT.
	//
	// A corpus mid-migration has two of them, and the failure to design against is
	// not ambiguity — it is SILENCE. Somebody edits a `*.kfacts.md` after
	// migrating, nothing happens, and the corpus reports itself clean. So the rule
	// is stated rather than merged: a non-empty store wins outright, and the fact
	// files that are now inert are NAMED in a warning.
	//
	// Merging the two instead would be worse in the obvious way — `kg migrate`
	// copies every id, so every node would collide — and worse in a subtle one: a
	// precedence rule per id is a thing somebody has to remember, and this format
	// refuses those everywhere else.
	stored, sdiags, serr := storedFacts(dl, root, index, home, logs, st, useDefault)
	if serr != nil {
		return nil, nil, serr
	}
	// THE FOLD'S WARNINGS REACH THE CALLER. They were dropped here, and with the
	// markdown reader gone this is the only path a corpus loads by.
	diags = append(diags, sdiags...)
	if len(stored) > 0 {
		docs = append(docs, stored...)
		if len(paths) > 0 {
			diags = append(diags, Diag{File: paths[0], Line: 1, Severity: SevWarn, Msg: fmt.Sprintf(
				"this index is loaded from its assertion store, so %d fact file(s) are no longer "+
					"read — edits to them do nothing. Delete them once you have compared, or "+
					"`kg export` and diff. Files: %s", len(paths), strings.Join(paths, ", "))})
		}
	} else if len(paths) > 0 {
		// FACT FILES ARE NO LONGER READ. They are reported, once, with what to do
		// about them — an index whose facts sit in markdown loads as empty, and
		// that would otherwise be silent.
		diags = append(diags, Diag{File: paths[0], Line: 1, Severity: SevError, Msg: fmt.Sprintf(
			"%d fact file(s) in this index are no longer read — facts live in the "+
				"assertion store now. Run `kg migrate --by <you>` to bring them across; "+
				"nothing is deleted and you can compare before removing them. Files: %s",
			len(paths), strings.Join(paths, ", "))})
	}
	// Attestations come off disk and must be in hand BEFORE Build validates, or
	// `checkAttestation` would warn about facts whose citations a person has
	// already ruled on. Build itself stays pure — this only fills the field.
	att, aerr := ReadAttestations(root, indexDirOfPaths(paths))
	if aerr != nil {
		return nil, nil, aerr
	}
	g, bd := BuildWith(docs, att)
	diags = append(diags, bd...)

	// THE INDEX OWNS THE DIALECT, and this is where the marker finally reaches the
	// graph. Build stays pure — it has no filesystem and cannot resolve an index —
	// so the vocabulary is bound here, alongside attestations and relations, for
	// the same reason they are.
	//
	// An unknown dialect is a hard error rather than a diagnostic. Every other
	// load failure here degrades to "not loaded, and silent about it"; this one
	// cannot, because continuing means evaluating one corpus's facts against
	// another's evidentiary ladder and reporting a confident answer about it.
	g.SetDialect(dl)

	// raglit's rulings on which documents are copies or versions of one another.
	// ASKED FOR after Build, unlike attestations: nothing in Build depends on
	// them, and they turn a document-level fact into a statement about source
	// ids, which needs the built graph to say at all.
	//
	// raglit being unreachable leaves Relations nil, which means NOT LOADED and
	// is silent. That is deliberate and it is not a failure: kgraph has to scan a
	// corpus on a machine with no daemon running, and reporting "no duplicates"
	// because nothing was asked would be worse than reporting nothing at all.
	if rel, rerr := FetchRelations(context.Background(), filepath.Join(root, indexDirOfPaths(paths))); rerr == nil {
		g.Relations = rel
		diags = append(diags, g.checkRelations()...)
	} else if !errors.Is(rerr, ErrRaglitUnavailable) {
		return nil, nil, rerr
	}

	return g, diags, nil
}

// indexDirOfPaths finds the directory an index's facts live in. Same answer as
// indexDirOf, from the paths rather than from a built graph, because the sidecar
// has to be read before the graph exists.
func indexDirOfPaths(paths []string) string {
	best := ""
	for _, rel := range paths {
		d := filepath.Dir(rel)
		if best == "" || len(d) < len(best) {
			best = d
		}
	}
	return best
}

// Errors filters diagnostics down to failures.
func Errors(ds []Diag) []Diag {
	var out []Diag
	for _, d := range ds {
		if d.Severity == SevError {
			out = append(out, d)
		}
	}
	return out
}

// Warnings filters diagnostics down to the ones that do not stop a load.
//
// It exists because `storedFacts` dropped them. Every diagnostic the FORMAT
// parser produced during a fold was scanned for errors and the rest discarded —
// so with the markdown reader deleted, and every corpus loading through the
// fold, every parse-time WARNING in the format had silently stopped being
// reported. Not one class of them: all of them.
func Warnings(ds []Diag) []Diag {
	var out []Diag
	for _, d := range ds {
		if d.Severity != SevError {
			out = append(out, d)
		}
	}
	return out
}

// Project is a scanned repo: the graph plus every document spec. Both the CLI
// and the MCP server load through here so they cannot drift.
type Project struct {
	Root string
	// Index is the one index this project's graph was built from. Every fact,
	// spec and computed field in it belongs to this index and no other.
	Index string
	Graph *Graph
	// Named holds the shared query library from `.kgraph/queries.md`, so a hop
	// chain used by five documents is written once.
	Named map[string]Query
	// Style holds the corpus's house style for generated prose, from
	// `.kgraph/style.md`. Empty when the corpus declares none.
	Style string
	// Accepted holds the findings a person has read and ruled tolerable, from
	// `accepted.jsonl`. Applied by `Triage`, never by `Load`.
	Accepted AcceptedSet
	// HighWater is the largest this index has been seen to be. Read here,
	// written only by a command — see WriteHighWater.
	HighWater HighWater
	// Standing holds the index's standing queries, from `standing.yaml`. This is
	// where declarations are heading — `*.kgraph.md` is retiring — so the
	// set-shaped checks read them through `DeclaredSets` rather than the specs.
	Standing StandingSet
}

// Load scans facts and specs. It returns the project even when diagnostics
// contain errors: a scan reports every problem rather than stopping at the
// first, because a dangling reference must be caught at build time.
func Load(root string) (*Project, []Diag, error) {
	return LoadIn(root, DefaultIndex)
}

// LoadIn scans one index's facts and specs, from the store the default policy
// names.
func LoadIn(root, index string) (*Project, []Diag, error) {
	return loadIn(root, index, nil, true)
}

// LoadInWith scans one index from a store the CALLER names. A nil store means
// log only — see ScanFactsInWith.
func LoadInWith(root, index string, st Store) (*Project, []Diag, error) {
	return loadIn(root, index, st, false)
}

func loadIn(root, index string, st Store, useDefault bool) (*Project, []Diag, error) {
	g, diags, err := scanFactsIn(root, index, st, useDefault)
	if err != nil {
		return nil, nil, err
	}
	diags = append(diags, g.CheckSourceDrift(root)...)
	diags = append(diags, g.CheckIndexBoundary(root)...)
	p := &Project{Root: root, Index: index, Graph: g, Named: map[string]Query{}}
	// Read before the specs, because the checks below prefer them and an unread
	// set would silently fall back to the spec half.
	stand, serr := ReadStanding(root, IndexDirOf(g))
	if serr != nil {
		return nil, nil, serr
	}
	p.Standing = stand
	// The findings ledger. Loaded but NOT applied here: `Load` returns every
	// diagnostic, and deciding which of them a person has already ruled on is a
	// presentation concern. A library caller that wants the full picture must not
	// have it quietly filtered — see `Project.Triage`.
	acc, aerr := ReadAccepted(root, IndexDirOf(g))
	if aerr != nil {
		return nil, nil, aerr
	}
	p.Accepted = acc
	// LIVENESS, checked on every load and recorded by no load. A corpus that has
	// stopped being read reports success over an empty graph, which is how the
	// driving corpus stayed dark for weeks.
	// FOUND FROM THE MARKER, not from the graph. `IndexDirOf` reads the nodes'
	// file paths, so an index that loaded NOTHING reports the repo root — and the
	// check written to catch an empty corpus could not find its own watermark in
	// exactly that case. Measured: a dark corpus scanned clean at 0 nodes.
	hwDir, known := IndexDirKnown(g)
	if !known {
		// The graph cannot say where it lives, so ask the MARKER. This is the
		// whole liveness case: an index that loaded nothing has no node to derive
		// a directory from, and deriving one anyway returns the repo root.
		if h, herr := IndexHomeOf(root, index); herr == nil {
			hwDir = h
		}
	}
	hw, herr := ReadHighWater(root, hwDir)
	if herr != nil {
		return nil, nil, herr
	}
	p.HighWater = hw
	diags = append(diags, g.CheckHighWater(hw)...)
	named, ndiags := loadNamedQueries(root)
	p.Named = named
	g.Style = loadStyle(root)
	p.Style = g.Style
	diags = append(diags, ndiags...)
	// After every spec is parsed, because the question is about the set: which of
	// them a manifest leaves out.
	diags = append(diags, p.CheckSetProportion()...)
	diags = append(diags, p.CheckSplitContradiction()...)
	// THE INDEX'S RULES ARE APPLIED LAST, once, where every diagnostic has
	// converged. Applying them earlier silently exempts whatever runs after —
	// which it did on the first attempt, leaving `split-contradiction`, the
	// second-largest class in the corpus, unconfigurable.
	return p, g.Dialect().ApplyRules(diags), nil
}

// namedQueriesRel is the shared query library. It lives under `.kgraph/`, which
// the fingerprint walk skips, so it is stated once here and named explicitly by
// the fingerprint — otherwise editing it never reloads the daemon.
const namedQueriesRel = ".kgraph/queries.md"

// styleRel is the corpus's house style for generated prose: the reference,
// tone and do-not-say rules every document in it must obey.
//
// It exists because those rules were being copied into individual specs and
// therefore held in some and not others. The failure that produced it was a
// relief document that printed "do not lead with this" and then argued against
// its own fact — a drafter's note in a deliverable — and the same class of aside
// was found in eight further documents. A rule enforced per-spec is a rule
// enforced where somebody remembered it.
//
// It is the AUTHOR'S text, not kgraph's. `render` still adds no words of its
// own; it interpolates the corpus's own rules the same way it interpolates the
// corpus's own facts, and shipping a default here would be exactly the
// scaffolding the render invariant forbids. Same shape as the named-query
// library: one conventional path, absent is fine, and named explicitly by the
// fingerprint because `.kgraph/` is skipped by the walk.
const styleRel = ".kgraph/style.md"

// loadStyle reads `.kgraph/style.md`. Absent is fine — a corpus with no declared
// house style renders exactly as it did before this existed.
func loadStyle(root string) string {
	src, err := os.ReadFile(filepath.Join(root, styleRel))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(src))
}

// loadNamedQueries reads `.kgraph/queries.md`. Absent is fine; a broken one is
// not, because every spec referencing it would otherwise fail with an unhelpful
// "unknown named query".
func loadNamedQueries(root string) (map[string]Query, []Diag) {
	rel := namedQueriesRel
	src, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return map[string]Query{}, nil
	}
	out := map[string]Query{}
	var diags []Diag
	for _, b := range fencedBlocks(src, "kgraph") {
		for i, ln := range strings.Split(b.body, "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "#") {
				continue
			}
			name, text, ok := strings.Cut(ln, ":")
			if !ok {
				diags = append(diags, Diag{File: rel, Line: b.firstLine + i, Severity: SevError, Msg: fmt.Sprintf("not a `name: query` line: %q", ln)})
				continue
			}
			name, text = strings.TrimSpace(name), strings.TrimSpace(text)
			q, perr := ParseQuery(text)
			if perr != nil {
				diags = append(diags, Diag{File: rel, Line: b.firstLine + i, Severity: SevError, Msg: fmt.Sprintf("@%s: %v", name, perr)})
				continue
			}
			// LEGAL, and this is a real limit rather than an oversight. Named
			// queries are PROJECT-scoped — one file at the root, usable by specs
			// in any index — while `class=` is INDEX-scoped. Under a second
			// dialect a named query naming a rung one ladder has and another
			// does not is valid in one index and not the other, and there is no
			// single dialect to validate it against here.
			//
			// It does not bite while `legal` is the only dialect. When a second
			// lands, the choice is to validate a named query per USE rather than
			// per definition, or to scope named queries to an index.
			for _, e := range ValidateQuery(q) {
				diags = append(diags, Diag{File: rel, Line: b.firstLine + i, Severity: SevError, Msg: fmt.Sprintf("@%s: %v", name, e)})
			}
			if _, dup := out[name]; dup {
				diags = append(diags, Diag{File: rel, Line: b.firstLine + i, Severity: SevError, Msg: fmt.Sprintf("named query @%s is defined twice", name)})
				continue
			}
			out[name] = q
		}
	}
	return out, diags
}

// Declared finds a declared set by name — a standing group, or a spec's
// basename. Replaces `Project.Spec`, which could only ever find the second.
func (p *Project) Declared(name string) (DeclaredSet, error) {
	all := p.DeclaredSets()
	for _, s := range all {
		if s.Name == name || s.Path == name ||
			strings.TrimSuffix(filepath.Base(s.Path), SpecSuffix) == name {
			return s, nil
		}
	}
	known := make([]string, 0, len(all))
	for _, s := range all {
		known = append(known, s.Name)
	}
	sort.Strings(known)
	return DeclaredSet{}, fmt.Errorf("no declared set %q — known: %s",
		name, strings.Join(known, ", "))
}

// CheckSetProportion warns where a spec's set takes most of its kind.
//
// A selector can be a SELECTION or a DEFAULT and the spec cannot tell them
// apart. `records: source[class=record]` was authored to mean "the recorded
// instruments this document rests on" and resolved to 68 of the corpus's 69
// records — every one — which six documents then printed. Nothing was wrong with
// the query; it answered exactly what it asked. What was missing is that nobody
// could see it had stopped selecting.
//
// The warning is deliberately not an error and deliberately not clever. Some
// sets SHOULD take everything: an appendix whose stated job is to show what a
// class contains is correct at 100%. That is why a set carrying a `purpose:` is
// held to a higher bar before it is flagged — the author has said what it is
// for, so the burden shifts to the reader.
// DeclaredQuery is one declared query, from whichever door declared it.
//
// Exported because it is the currency now: `Resolve`, `DiffGraphs` and
// `Variants` all take a set of these rather than a `*Spec`, so a standing group
// and a spec document are the same thing to everything downstream and retiring
// `*.kgraph.md` changes one producer.
type DeclaredQuery struct {
	Name    string
	Text    string
	Purpose string
	Line    int
}

// DeclaredSet is a GROUP of declared queries — a spec's document, or a standing
// group. The two set-shaped checks below run over this rather than over specs,
// so retiring `*.kgraph.md` changes the producer and not the checks.
type DeclaredSet struct {
	// Name is how a person asks for this set: a spec's basename, or a standing
	// group. Empty for a group of one, whose Queries[0].Name is the whole of it.
	Name    string
	Path    string // where a diagnostic is reported
	Queries []DeclaredQuery
}

// DeclaredSets is every group of declared queries in the project.
//
// STANDING QUERIES WIN OUTRIGHT where an index has any, and the specs are not
// also read. Same rule and same reasoning as the assertion store winning over
// fact files: a corpus mid-conversion holds both, and the failure to design
// against is not ambiguity but DOUBLE-REPORTING — every warning twice, from two
// spellings of one declaration.
func (p *Project) DeclaredSets() []DeclaredSet {
	if len(p.Standing) > 0 {
		byGroup := map[string][]DeclaredQuery{}
		for _, st := range p.Standing {
			// An ungrouped query is a group of one, keyed by its own name so it
			// cannot pool with every other ungrouped question in the index.
			g := st.Group
			if g == "" {
				g = "\x00" + st.Name
			}
			byGroup[g] = append(byGroup[g], DeclaredQuery{
				Name: st.Name, Text: st.Query, Purpose: st.Purpose})
		}
		names := make([]string, 0, len(byGroup))
		for g := range byGroup {
			names = append(names, g)
		}
		sort.Strings(names)
		out := make([]DeclaredSet, 0, len(names))
		for _, g := range names {
			qs := byGroup[g]
			sort.Slice(qs, func(i, j int) bool { return qs[i].Name < qs[j].Name })
			name := g
			if strings.HasPrefix(name, "\x00") {
				name = qs[0].Name
			}
			out = append(out, DeclaredSet{
				Name:    name,
				Path:    filepath.ToSlash(filepath.Join(IndexDirOf(p.Graph), standingName)),
				Queries: qs,
			})
		}
		return out
	}
	return nil
}

func (p *Project) CheckSetProportion() []Diag {
	if p.Graph == nil {
		return nil
	}
	// Totals per kind, computed once.
	total := map[Kind]int{}
	for _, n := range p.Graph.Nodes {
		total[n.Kind]++
	}
	var out []Diag
	for _, s := range p.DeclaredSets() {
		for _, q := range s.Queries {
			parsed, err := ParseQuery(q.Text)
			if err != nil {
				continue // already reported at parse time
			}
			kinds := leftmostKinds(parsed)
			if len(kinds) != 1 {
				// A union of kinds has no single denominator, and comparing against
				// the corpus would flag every well-scoped chronology.
				continue
			}
			denom := total[kinds[0]]
			if denom < 20 {
				continue // a small kind makes the ratio meaningless
			}
			got, err := p.Graph.Eval(parsed, Env{Named: p.Named})
			if err != nil {
				continue
			}
			pct := len(got) * 100 / denom
			threshold := 75
			if q.Purpose != "" {
				threshold = 90 // the author has stated intent; flag only the extreme
			}
			if pct < threshold {
				continue
			}
			msg := fmt.Sprintf("set %q takes %d of %d %s (%d%%) — a selection, or a default? name what it is for with an indented `purpose:`",
				q.Name, len(got), denom, kinds[0], pct)
			if q.Purpose != "" {
				msg = fmt.Sprintf("set %q takes %d of %d %s (%d%%) and states its purpose as %q — check the expression still serves it",
					q.Name, len(got), denom, kinds[0], pct, q.Purpose)
			}
			out = append(out, Diag{File: s.Path, Line: q.Line, Severity: SevWarn, Msg: msg})
		}
	}
	return out
}

// leftmostKinds is the kind of the set a query RETURNS. The leftmost atom of a
// pattern is the result, and a union returns whatever its terms agree on — so a
// union of differing kinds has no single denominator and reports none.
func leftmostKinds(q Query) []Kind {
	switch t := q.(type) {
	case Pattern:
		if len(t.Atoms) == 0 {
			return nil
		}
		return t.Atoms[0].Kinds
	case Scoped:
		return leftmostKinds(t.X)
	case Inter:
		// An intersection is constrained by every term; the first that names a
		// kind is the denominator.
		for _, x := range t.Terms {
			if k := leftmostKinds(x); len(k) == 1 {
				return k
			}
		}
	case Union:
		var first []Kind
		for _, x := range t.Terms {
			k := leftmostKinds(x)
			if len(k) != 1 {
				return nil
			}
			if first == nil {
				first = k
			} else if first[0] != k[0] {
				return nil
			}
		}
		return first
	}
	return nil
}

// CheckSplitContradiction warns where a document's sets carry one end of a
// contradiction and not the other.
//
// A corrected fact does not delete the fact it corrects — it sits beside it,
// joined by `contradicts`, `undercut_by` or `supersedes`, and the graph is
// right to keep both. What a DOCUMENT must not do is print one without the
// other, because a reader of the document cannot see the edge. The set is the
// only thing standing between the corpus and that outcome, and a hand-enumerated
// set has no way to know that a fact it names was retired by a fact it does not.
//
// This is not hypothetical. `the-case` argued for a week that a certification's
// stated "approximately .2 acres" was too small to hold a disputed strip, after
// a second certification from the same batch had already shown the figure to be
// a form default. Both facts were in the corpus. Only the dead one was in the
// set. The document had no way to know, and neither did anyone reading it.
//
// Direction matters, and follows Edge.OneWay:
//
//   - `contradicts` is symmetric, so either end alone is a warning.
//   - `undercut_by` is not: B beats A. Carrying A without B is a warning;
//     carrying B without A is fine, because B does not need its victim.
//   - `supersedes` is not: A replaces B. Carrying B without A is a warning;
//     carrying A without B is fine, because that is the correction landing.
//
// It is a warning and never an error. A document is entitled to omit a
// correction it has no room for — a chronology need not carry every dispute
// about every entry — and the author is the one who decides. What the author is
// not entitled to is not knowing.
func (p *Project) CheckSplitContradiction() []Diag {
	if p.Graph == nil {
		return nil
	}
	type rel struct {
		present string   // the end that IS in the set
		absent  string   // the end that is not
		kind    EdgeType // what joins them
		oneway  bool
	}
	var out []Diag
	for _, s := range p.DeclaredSets() {
		// Resolve every set once, remembering which query first pulled each node
		// so the warning lands on a line the author can act from.
		at := map[string]*DeclaredQuery{}
		for i := range s.Queries {
			q := &s.Queries[i]
			parsed, err := ParseQuery(q.Text)
			if err != nil {
				continue // reported at parse time
			}
			got, err := p.Graph.Eval(parsed, Env{Named: p.Named})
			if err != nil {
				continue
			}
			for _, id := range got {
				if _, seen := at[id]; !seen {
					at[id] = q
				}
			}
		}
		if len(at) == 0 {
			continue
		}
		// A source that attests an in-set fact is IN the document: the render
		// carries it under the fact and the reference block cites it, so a reader
		// can see it. Counting only what Eval returns made every such source read
		// as absent — `s-precipitation-research` undercuts the no-flood-event
		// fact, appears seven times in the-case's prompt, and was reported missing
		// from a document that quotes it. Attribute it to the query that pulled
		// the fact it attests.
		for _, e := range p.Graph.Edges {
			if e.Type != EAttests {
				continue
			}
			if q, ok := at[e.Dst]; ok {
				if _, seen := at[e.Src]; !seen {
					at[e.Src] = q
				}
			}
		}
		var split []rel
		for _, e := range p.Graph.Edges {
			if e.Type != EContradicts && e.Type != ESupersedes {
				continue
			}
			_, hasSrc := at[e.Src]
			_, hasDst := at[e.Dst]
			if hasSrc == hasDst {
				continue // both in, or both out — nothing to say
			}
			// A node the corpus has already killed is not the failure mode, at
			// either end. House rule: a status travels with the claim into the
			// prose, so a reader can see a `withdrawn` or `false` fact is dead
			// without needing the edge — and sets exist whose whole job is to
			// collect them, `action-queue` has one named "retired". At the other
			// end there is simply nothing to carry: a document cannot be faulted
			// for omitting a correction the corpus has itself retracted. The
			// defect is one LIVE fact whose live correction the reader cannot see.
			if dead(p.Graph, e.Src) || dead(p.Graph, e.Dst) {
				continue
			}
			switch {
			case e.Type == ESupersedes:
				// A forward edge: Src supersedes Dst. Only the superseded end alone
				// is a problem — the replacement standing alone is the correction
				// landing, which is what is wanted.
				if hasDst {
					split = append(split, rel{e.Dst, e.Src, e.Type, true})
				}
			case e.OneWay:
				// `undercut_by` is an inverseEdge: authored on the node being
				// beaten, stored canonically as Src=defeater, Dst=defeated. Only
				// the defeated end alone is a problem — a defeater does not need
				// its victim on the page.
				if hasDst {
					split = append(split, rel{e.Dst, e.Src, e.Type, true})
				}
			default:
				// Symmetric: whichever end is present is missing its counterpart.
				if hasSrc {
					split = append(split, rel{e.Src, e.Dst, e.Type, false})
				} else {
					split = append(split, rel{e.Dst, e.Src, e.Type, false})
				}
			}
		}
		sort.Slice(split, func(i, j int) bool {
			if split[i].present != split[j].present {
				return split[i].present < split[j].present
			}
			return split[i].absent < split[j].absent
		})
		for _, r := range split {
			q := at[r.present]
			verb := "contradicted by"
			fix := "carry both, or say in the prompt why this document does not"
			if r.oneway {
				if r.kind == ESupersedes {
					verb = "superseded by"
					fix = "carry the superseding fact, or drop the superseded one"
				} else {
					verb = "undercut by"
					fix = "carry the fact that defeats it, or drop it"
				}
			}
			out = append(out, Diag{File: s.Path, Line: q.Line, Severity: SevWarn,
				Check: CheckSplitContradiction,
				// The two ENDS identify it, not the set that happened to carry one:
				// renaming a query or regrouping the declarations must not retire a
				// ruling about the same pair of facts.
				Key:      findingKey(p.Graph, CheckSplitContradiction, r.present, r.absent),
				Subjects: []string{r.present, r.absent},
				Msg: fmt.Sprintf(
					"set %q carries %q, which is %s %q — and no set in this document reaches it, so the document states one end of a correction and the reader cannot see the other. %s",
					q.Name, r.present, verb, r.absent, fix)})
		}
	}
	return out
}

// dead reports whether a node is one the corpus has already retired, so a
// document carrying it is not making a live claim on it.
func dead(g *Graph, id string) bool {
	n, ok := g.Lookup(id)
	if !ok {
		return false
	}
	return n.Status == SWithdrawn || n.Status == SFalse
}

// storedFacts folds an index's assertion store, or returns nil when it holds
// nothing.
//
// NIL MEANS "NO STORE", NOT "AN EMPTY GRAPH", and the two must not be confused:
// the first is a corpus that has not migrated and whose fact files are still
// authoritative, the second is a corpus somebody has emptied. Answering the same
// way for both would make an unmigrated corpus load as no facts at all.
//
// A store that will not open is NOT an error here. Most corpora have none, and
// `OpenStore` would otherwise create one as a side effect of scanning — which is
// how a read command ends up writing to `~/.kgraph`.
//
// WHERE the store is comes from the caller. `useDefault` asks for the policy —
// `~/.kgraph/<index>.db` if that file exists — and is what the bare entry points
// pass; otherwise `st` is the caller's, and a nil `st` means LOG ONLY.
func storedFacts(dl Dialect, root, index, home string, logs []string, st Store, useDefault bool) ([]*Doc, []Diag, error) {
	var warn []Diag
	// THE DATABASE FIRST, and it is one store per index.
	if useDefault {
		path, err := StorePath(index)
		if err != nil {
			return nil, nil, err
		}
		if _, serr := os.Stat(path); serr == nil {
			opened, oerr := OpenStore(path)
			if oerr != nil {
				return nil, nil, oerr
			}
			defer opened.Close()
			st = opened
		}
	}
	if st != nil {
		log, lerr := st.All()
		if lerr != nil {
			return nil, nil, lerr
		}
		// AN EMPTY STORE FALLS THROUGH TO THE EXPORTS, and it is safe precisely
		// because the table is append-only: a log that has ever been written to can
		// never come back empty, so empty means never written rather than emptied,
		// and nothing is being resurrected against anything. Returning "no facts"
		// here instead is how a corpus that loads from its exported log reports
		// itself EMPTY the moment any command creates the default database as a side
		// effect of opening it.
		if len(log) > 0 {
			doc, ds := FoldAssertions(dl, filepath.ToSlash(filepath.Join(home, assertName)), log)
			if errs := Errors(ds); len(errs) > 0 {
				return nil, nil, fmt.Errorf("the assertion store does not fold:\n%s", diagLines(errs))
			}
			return []*Doc{doc}, append(warn, Warnings(ds)...), nil
		}
	}

	// NO DATABASE, BUT EXPORTS: load them, ONE DOC PER LOG.
	//
	// Per-directory rather than per-index, mirroring how fact files worked, and
	// the reason is not symmetry. A node's declaring file decides where its
	// `sources.lock` goes, and locks are per-directory ON PURPOSE: `~/life`
	// gitignores an entire project as medical PII, and a lock recording that
	// project's document filenames anywhere else would put them into a shared
	// history. Folding every log in an index into one doc moved every lock to the
	// index root — which the lock-placement test caught on the first run.
	//
	// The export is also not only a backup. A corpus handed to somebody with no
	// database still loads, which is what makes the text worth keeping.
	var out []*Doc
	for _, rel := range logs {
		log, lerr := ReadAssertionLog(filepath.Join(root, rel))
		if lerr != nil {
			return nil, nil, lerr
		}
		if len(log) == 0 {
			continue
		}
		doc, ds := FoldAssertions(dl, rel, log)
		if errs := Errors(ds); len(errs) > 0 {
			return nil, nil, fmt.Errorf("%s does not fold:\n%s", rel, diagLines(errs))
		}
		warn = append(warn, Warnings(ds)...)
		out = append(out, doc)
	}
	return out, warn, nil
}
