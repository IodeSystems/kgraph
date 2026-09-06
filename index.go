package kgraph

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IndexMarker names the file that declares which index a subtree belongs to.
//
// The problem it solves is not tidiness. A corpus discovered by walking up to a
// `.git` root takes in every sibling project in a monorepo, and kgraph then
// treats them as one graph. Two consequences follow, and both corrupt computed
// state rather than merely adding noise:
//
//   - `disputed` is computed from conflicting evidence. Facts from an unrelated
//     matter can contradict one another across the seam and mark a claim
//     disputed on evidence that has nothing to do with it.
//   - `SemHash` covers content plus incident edges. A person who genuinely
//     appears in two matters — the same human, modelled once per matter — can
//     acquire edges spanning both, so editing one matter produces a phantom
//     delta in the other's documents.
//
// A path boundary would stop the walk but not the entanglement, because the
// shared entity legitimately belongs to both subtrees. Membership has to be
// declared, and computation has to be closed over it.
//
// Closed computation, open query. The two planes are deliberately separate:
//
//   - **Verification is in-index, always.** `disputed`, contradiction detection,
//     `supersedes`, `same_as` and `SemHash` resolve strictly within one index.
//     Nothing outside it can mark a claim disputed or move a document's hash.
//     This is the invariant; it is what makes staleness reproducible.
//   - **Query may cross indexes, but only when it says so.** Reading across
//     matters is useful and safe, because a query result is an observation and
//     never feeds back into computed state. The safety is in the explicitness:
//     an unqualified query resolves in its own index and cannot reach outside it,
//     and a spec that draws on another index declares that at the top so a
//     generated document can never silently carry foreign facts. The failure this
//     scheme exists to prevent was exactly an unqualified query — `event[at>...]`
//     with no scope — quietly spanning two unrelated matters.
//
// Cross-index comparison therefore reports **flags** — two indexes asserting
// incompatible things about the same subject — which is a distinct concept from
// `disputed` and must never be conflated with it. `disputed` is a property of one
// graph's evidence; a flag is a relationship between two graphs, and only a human
// can say which of them is wrong.
const IndexMarker = ".kg-index"

// DefaultIndex is the index of files with no marker above them. It is a real
// index, not "everything": a corpus that has never heard of indexes keeps
// working unchanged, and a marked subtree is excluded from it.
const DefaultIndex = ""

// IndexDecl is a parsed index declaration — the `.kg-index` marker, and the
// `## Index` spec header that mirrors its grammar. One parser for both: the
// header used to duplicate the marker's line-splitting, and duplicated its bug
// along with it.
type IndexDecl struct {
	// Name is the declared index. Empty means the declaration did not state one,
	// which for a marker means "name yourself after your directory".
	Name string
	// Dialect names the vocabulary and rule set the index binds to. Resolved by
	// DialectAt, which refuses an unknown name rather than falling back to legal,
	// and surfaced to a user by `kg indexes` — the only place the key is named.
	Dialect string
	// Rules are this index's overrides of the dialect's rule levels, from
	// `rule.<check>: error|warn|off`. Nearest wins, as everything here does.
	Rules map[string]RuleLevel
	// Dir is the repo-relative directory holding the marker, and it is empty for
	// the default index, which has none. Carried because a project OWNS dialects
	// — `<Dir>/.kgraph/dialects/*.yaml` — and the only thing that knows where a
	// project starts is the walk that found its marker.
	Dir string
}

// declKeys is the closed set of keys a declaration may carry, and closing it is
// the point rather than a nicety.
//
// The first parser took the first non-blank, non-comment line and split it on
// ":", treating whatever followed as the index name. That made *every*
// `key: value` line a name declaration, so a marker whose only line was
//
//	dialect: legal
//
// named the index "legal" — silently, because an index name is validated
// against nothing and a wrongly-named index simply resolves to an empty graph.
// Any unrecognised key has that same shape, so the fix is not to special-case
// `dialect` but to reject what is not a known key. A typo must not be able to
// name an index.
var declKeys = map[string]bool{"index": true, "dialect": true}

// rulePrefix declares what this index does with one check: `rule.<check>: off`.
//
// PER-INDEX, and it OVERRIDES the dialect's own rule for that check. A dialect
// carries the sensible default for its domain; an index says what it does
// differently. Same layering as everything else here, and the alternative was
// worse in a specific way: tuning one check would have meant forking a whole
// evidentiary ladder to hang it on, and a forked ladder that drifts from the
// built-in is exactly the silent corruption the closed dialect map was
// protecting against.
//
// It is deliberately NOT part of `dialect_hash`. The ladder is hashed because it
// decides which of two conflicting sources wins and therefore which rows come
// back; a rule level changes what is REPORTED and no row anywhere, so flagging
// every document over it would be a false alarm.
const rulePrefix = "rule."

// parseIndexDecl reads the declaration grammar: blank lines and `#` comments are
// skipped, `key: value` sets that key, and a lone bare word is the index name.
// Both name forms are accepted because both read naturally and no invariant
// prefers one.
func parseIndexDecl(text string) (IndexDecl, error) {
	var d IndexDecl
	var named bool
	setName := func(v string) error {
		if named {
			return fmt.Errorf("index declared twice (%q then %q)", d.Name, v)
		}
		d.Name, named = v, true
		return nil
	}
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		key, val, keyed := strings.Cut(ln, ":")
		if !keyed {
			if err := setName(ln); err != nil {
				return d, err
			}
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if strings.HasPrefix(key, rulePrefix) {
			check := strings.TrimPrefix(key, rulePrefix)
			if _, known := knownChecks[check]; !known {
				return d, fmt.Errorf("`%s` names no check — this binary knows: %s",
					key, strings.Join(CheckNames(), ", "))
			}
			switch RuleLevel(val) {
			case RuleError, RuleWarn, RuleOff:
			default:
				return d, fmt.Errorf("`%s: %s` — a rule must be error, warn or off", key, val)
			}
			if d.Rules == nil {
				d.Rules = map[string]RuleLevel{}
			}
			if _, dup := d.Rules[check]; dup {
				return d, fmt.Errorf("rule for %q declared twice", check)
			}
			d.Rules[check] = RuleLevel(val)
			continue
		}
		if !declKeys[key] {
			return d, fmt.Errorf("unknown key %q — want `index:`, `dialect:` or `rule.<check>:`, "+
				"or a bare index name", key)
		}
		if val == "" {
			return d, fmt.Errorf("`%s:` has no value", key)
		}
		switch key {
		case "index":
			if err := setName(val); err != nil {
				return d, err
			}
		case "dialect":
			if d.Dialect != "" {
				return d, fmt.Errorf("dialect declared twice (%q then %q)", d.Dialect, val)
			}
			d.Dialect = val
		}
	}
	return d, nil
}

// IndexAt resolves the index owning dir by walking up to root inclusive and
// taking the first marker found. Nearest wins, so a subtree can carve itself out
// of an enclosing index.
//
// An empty marker — or one that declares only a dialect — names the index after
// its own directory, which is the common case and saves stating a name twice.
//
// A marker that will not parse is an error, never a fallback: guessing
// membership is how foreign facts end up in a graph.
func IndexAt(root, dir string) (string, error) {
	decl, err := DeclAt(root, dir)
	return decl.Name, err
}

// DeclAt resolves the whole declaration owning dir, not just its name.
//
// IndexAt parsed the marker and threw everything but the name away, which is why
// `dialect:` has parsed since July and reached nothing: the one function that
// reads a marker discarded the field. A caller that needs the vocabulary an
// index binds to had no way to ask for it.
//
// Name is always filled — the directory's own name when the marker declares
// none, which is the common case and saves stating it twice.
func DeclAt(root, dir string) (IndexDecl, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return IndexDecl{Name: DefaultIndex}, err
	}
	d, err := filepath.Abs(dir)
	if err != nil {
		return IndexDecl{Name: DefaultIndex}, err
	}
	for {
		marker := filepath.Join(d, IndexMarker)
		b, rerr := os.ReadFile(marker)
		if rerr == nil {
			decl, perr := parseIndexDecl(string(b))
			if perr != nil {
				return IndexDecl{Name: DefaultIndex}, fmt.Errorf("%s: %w", marker, perr)
			}
			if decl.Name == "" {
				decl.Name = filepath.Base(d)
			}
			if rel, rerr := filepath.Rel(absRoot, d); rerr == nil && rel != "." {
				decl.Dir = rel
			}
			return decl, nil
		}
		if d == absRoot {
			return IndexDecl{Name: DefaultIndex}, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			// dir was not under root. Refusing to guess is deliberate: silently
			// returning the default index would put foreign facts in a graph.
			return IndexDecl{Name: DefaultIndex}, nil
		}
		d = parent
	}
}

// DialectAt resolves the vocabulary an index binds to.
//
// An UNKNOWN name is an error and the error names what would have been accepted.
// Falling back to legal would apply one corpus's evidentiary ladder to another's
// facts and report a confident answer about it — the failure the whole type
// exists to prevent, arriving silently.
// IndexHomeOf finds the directory an index's MARKER sits in, by walking for
// markers rather than by asking the graph.
//
// `IndexDirOf` derives the directory from the nodes' file paths, which is right
// everywhere except the one case that matters here: an index that loaded NOTHING
// has no nodes, so it reports the repo root and the liveness mark beside the
// marker is never found. The check that exists to catch an empty corpus was
// unable to locate its own watermark precisely when the corpus was empty.
//
// Empty means the default index, which has no marker and no home of its own.
func IndexHomeOf(root, index string) (string, error) {
	if index == DefaultIndex {
		return "", nil
	}
	var home string
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || fi.Name() != IndexMarker || home != "" {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		decl, derr := parseIndexDecl(string(b))
		if derr != nil {
			return nil // a broken marker is reported by the scan, not here
		}
		name := decl.Name
		if name == "" {
			name = filepath.Base(filepath.Dir(p))
		}
		if name == index {
			rel, rerr := filepath.Rel(root, filepath.Dir(p))
			if rerr == nil {
				home = filepath.ToSlash(rel)
			}
		}
		return nil
	})
	return home, err
}

func DialectAt(root, dir string) (Dialect, error) {
	decl, err := DeclAt(root, dir)
	if err != nil {
		return legal, err
	}
	// Authored dialects are read before the name is resolved, because a corpus's
	// own vocabulary has to be in hand to say whether the name is unknown. A file
	// that will not parse or validate fails HERE rather than downstream: the
	// alternative is resolving to the built-in of the same intent and reporting a
	// confident answer under a ladder nobody checked.
	global, gerr := LoadDialects(root)
	if gerr != nil {
		return legal, gerr
	}
	project, perr := LoadProjectDialects(root, decl)
	if perr != nil {
		return legal, perr
	}
	d, ok := dialectFrom(decl.Dialect, decl.Name, project, global)
	if !ok {
		return legal, fmt.Errorf("index %q declares dialect %q, which this kgraph does not know; "+
			"it knows %s", decl.Name, decl.Dialect,
			strings.Join(knownDialectNames(project, global), ", "))
	}
	return d.withRules(decl.Rules), nil
}

// indexOfFile resolves the index owning a repo-relative file path.
func indexOfFile(root, rel string) (string, error) {
	return IndexAt(root, filepath.Dir(filepath.Join(root, rel)))
}

// joinUnique collapses the same marker error reported once per file underneath
// it. One broken marker should read as one problem, and every broken marker
// should be reported — scan never stops at the first.
func joinUnique(errs []error) error {
	seen := map[string]bool{}
	out := errs[:0:0]
	for _, e := range errs {
		if s := e.Error(); !seen[s] {
			seen[s] = true
			out = append(out, e)
		}
	}
	return errors.Join(out...)
}

// Indexes lists every index declared under root, including DefaultIndex when any
// unmarked fact file exists. Sorted, deduplicated. This is what makes the scheme
// discoverable: without it a user who marks a subtree sees their facts vanish
// from the default index with nothing telling them where they went.
func Indexes(root string) ([]string, error) {
	paths, err := findFilesBySuffix(root, FactsSuffix)
	if err != nil {
		return nil, err
	}
	specs, err := findFilesBySuffix(root, SpecSuffix)
	if err != nil {
		return nil, err
	}
	// An index that has migrated has no fact files, so it has to be findable by
	// its export or it disappears from `kg indexes` while still loading fine
	// everywhere else — a discrepancy nobody would think to look for.
	logs, err := findFilesBySuffix(root, assertName)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var bad []error
	for _, rel := range append(append(paths, specs...), logs...) {
		idx, ierr := indexOfFile(root, rel)
		if ierr != nil {
			bad = append(bad, ierr)
			continue
		}
		seen[idx] = true
	}
	if len(bad) > 0 {
		return nil, joinUnique(bad)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}
