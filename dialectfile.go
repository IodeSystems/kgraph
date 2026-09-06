package kgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DIALECTS ARE DATA. A corpus may write its own, in `.kgraph/dialects/*.yaml`.
//
// The list used to be closed in the binary, and the reason given was that a
// hand-authored ladder with its rungs in the wrong order produces no error, just
// confidently wrong `disputed`. That argument was weaker than it read. The
// CHECKABLE part is checked — a ladder must be strictly ordered and free of
// duplicates, the governing class must have no rank, a dialect may not redefine
// a core relation or shadow a core key — and closing the map never checked the
// part that cannot be checked, which is whether `measured` truly outranks
// `meeting-note` in somebody's trade. It only limited who was allowed to get it
// wrong, at the cost of making the feature unusable by the corpora it exists for.
//
// What holds instead, and what each piece buys:
//
//   - **Validation is load-time and total.** Every guard the built-ins got at
//     init() now runs over a file too, as an ERROR naming the file rather than a
//     panic. `validateDialect` is the one implementation; the built-ins are
//     checked through it at init so the two can never drift.
//   - **A file may not shadow a built-in.** A name means one thing. Same shape as
//     the `## Index` rule: a declaration that disagrees is an error naming both
//     sides, never a silent redirect. Otherwise `legal` means something different
//     per checkout and a document's ladder depends on which clone you are in.
//   - **Editing one flags the documents written under it** — `DialectHash`, bound
//     into the render token and recorded in the managed block, reported as
//     `dialect-changed`. Same argument as `.kgraph/style.md`, and it is not the
//     thing the settled decision forbids: the dialect still does NOT enter
//     `SemHash`, so switching one does not re-flag every FACT. What it does is
//     stop a DOCUMENT claiming freshness against a ladder written after it, and
//     the ladder decides which of two conflicting sources wins and how the
//     document says so.
//   - **A built-in hashes to the empty string.** A corpus that declares no dialect
//     hashes and records exactly as it did before this existed, so nothing that
//     has already been rendered false-flags. Identical to `StyleHash`'s rule and
//     for the identical reason: a built-in cannot be edited under a finished
//     document; a file can.
//
// Same shape as `.kgraph/style.md` and `.kgraph/queries.md` throughout: one
// conventional path, absent is fine, and named explicitly by the fingerprint
// because the walk skips `.kgraph/`.
const dialectsDir = ".kgraph/dialects"

// dialectFile is the authored shape. Deliberately flat: every field is one of
// the five things a dialect carries, and nothing here is a mechanic.
type dialectFile struct {
	// Name is optional. Present, it must agree with the file's own name — two
	// places to say one thing, so disagreeing is an error rather than a
	// precedence rule somebody has to remember.
	Name string `yaml:"name"`
	// Ladder is the ordinal evidentiary ladder, STRONGEST FIRST. A list and not a
	// map of numbers: the numbers are an implementation detail, and authoring them
	// invites gaps that read like meaning.
	Ladder []string `yaml:"ladder"`
	// Governs is the out-of-ladder class — law, not evidence. It must not appear
	// in the ladder: having no rank is the mechanism, not an omission.
	Governs string `yaml:"governs"`
	// Invalidated is what this dialect calls a source that can no longer be relied
	// on. The mechanic is core; the word is not.
	Invalidated string `yaml:"invalidated"`
	// SpeakerRequired names the rungs that assert something about someone's
	// interest and mean nothing without saying whose.
	SpeakerRequired []string `yaml:"speaker_required"`
	// Edges are relation keys this dialect adds, mapped to how each reads when
	// followed backwards. They are walkable and drive nothing — see Dialect.edges.
	Edges map[string]string `yaml:"edges"`
	// Subtypes are node types over the core kinds. A subtype does not create a
	// kind.
	Subtypes map[string]subtypeFile `yaml:"subtypes"`
	// Rules tunes what this index does with each check: `error`, `warn` or `off`.
	// Absent means every check keeps its natural level, which is what every
	// corpus that predates this gets.
	Rules map[string]string `yaml:"rules"`
}

type subtypeFile struct {
	Kind string   `yaml:"kind"`
	Keys []string `yaml:"keys"`
}

// parseDialectFile builds a Dialect from one authored file. rel is used only for
// error messages, and every error names it: a dialect is loaded far from where
// its effect is felt, so an error that does not say which file is a hunt.
func parseDialectFile(rel string, src []byte) (Dialect, error) {
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	var f dialectFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return Dialect{}, fmt.Errorf("%s: %w", rel, err)
	}
	if f.Name != "" && f.Name != base {
		return Dialect{}, fmt.Errorf("%s: declares name %q but is filed as %q — "+
			"the file name is the dialect's name; drop the `name:` or rename the file",
			rel, f.Name, base)
	}
	if base == "" {
		return Dialect{}, fmt.Errorf("%s: a dialect file needs a name", rel)
	}

	d := Dialect{
		name:        base,
		governs:     f.Governs,
		invalidated: f.Invalidated,
		rank:        map[string]int{},
		speaker:     map[string]bool{},
		rules:       map[string]RuleLevel{},
		hash:        hashString(strings.TrimSpace(string(src))),
	}
	// RULES ARE VALIDATED TOTALLY, at load, naming the file. A rule pointing at a
	// check that does not exist does nothing at all, and "I turned that off"
	// becomes a belief instead of a fact — the same failure a mistyped hook name
	// produces, which is why that has its own test.
	for name, level := range f.Rules {
		if _, ok := knownChecks[name]; !ok {
			return Dialect{}, fmt.Errorf("%s: rule %q names no check — this binary knows: %s",
				rel, name, strings.Join(CheckNames(), ", "))
		}
		switch RuleLevel(level) {
		case RuleError, RuleWarn, RuleOff:
			d.rules[name] = RuleLevel(level)
		default:
			return Dialect{}, fmt.Errorf("%s: rule %q is %q — it must be error, warn or off",
				rel, name, level)
		}
	}
	// Strongest first in the file, highest rank in the map. Authoring positions
	// rather than numbers means the ladder cannot carry a gap that reads like a
	// missing rung.
	for i, c := range f.Ladder {
		c = strings.TrimSpace(c)
		if c == "" {
			return Dialect{}, fmt.Errorf("%s: the ladder has an empty rung at position %d", rel, i+1)
		}
		if _, dup := d.rank[c]; dup {
			return Dialect{}, fmt.Errorf("%s: %q appears twice in the ladder — a class has one rank "+
				"or the ordering is not an ordering", rel, c)
		}
		d.rank[c] = len(f.Ladder) - 1 - i
	}
	for _, c := range f.SpeakerRequired {
		d.speaker[strings.TrimSpace(c)] = true
	}
	if len(f.Edges) > 0 {
		d.edges = map[string]string{}
		for k, phrase := range f.Edges {
			d.edges[k] = phrase
		}
	}
	if len(f.Subtypes) > 0 {
		d.subtypes = map[string]Subtype{}
		for name, sf := range f.Subtypes {
			kind := Kind(sf.Kind)
			if !coreKinds[kind] {
				return Dialect{}, fmt.Errorf("%s: subtype %q is declared over kind %q, which is not a core kind; "+
					"the core kinds are %s", rel, name, sf.Kind, strings.Join(coreKindNames(), ", "))
			}
			st := Subtype{kind: kind, keys: map[string]bool{}}
			for _, k := range sf.Keys {
				st.keys[strings.TrimSpace(k)] = true
			}
			d.subtypes[name] = st
		}
	}
	if err := validateDialect(d); err != nil {
		return Dialect{}, fmt.Errorf("%s: %w", rel, err)
	}
	return d, nil
}

// coreKinds is the closed set a subtype may sit over, derived from bodyKeys so
// the two cannot drift.
var coreKinds = func() map[Kind]bool {
	out := map[Kind]bool{}
	for _, k := range bodyKeys {
		out[k] = true
	}
	return out
}()

func coreKindNames() []string {
	out := make([]string, 0, len(coreKinds))
	for k := range coreKinds {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// validateDialect is every guard a dialect must pass, whether it was written in
// Go or read off disk. ONE implementation, called from init() for the built-ins
// and from parseDialectFile for the rest — two copies would drift, and the copy
// that drifts is the one guarding the files somebody actually writes.
func validateDialect(d Dialect) error {
	if len(d.rank) == 0 {
		return fmt.Errorf("dialect %q has no ladder — an ordinal ladder is what resolves a conflict", d.name)
	}
	if d.governs == "" {
		return fmt.Errorf("dialect %q names no governing class; it is the out-of-ladder class that "+
			"states law rather than testifying", d.name)
	}
	// The governing class having no rank IS the mechanism: conflict and
	// impeachment look up a rank and skip what has none, so authority supports a
	// claim without ever winning or losing an evidentiary contest. Ranking it
	// would let a statute beat a witness, which is a category error.
	if _, ranked := d.rank[d.governs]; ranked {
		return fmt.Errorf("dialect %q puts its governing class %q on the ladder — the governing class "+
			"has no rank by construction, so that it can neither win nor lose an evidentiary contest",
			d.name, d.governs)
	}
	if d.invalidated == "" {
		return fmt.Errorf("dialect %q names no invalidation flag — impeached, retracted, revoked; "+
			"the mechanic is core but the word is the dialect's", d.name)
	}
	for c := range d.speaker {
		if _, ok := d.rank[c]; !ok {
			return fmt.Errorf("dialect %q requires a speaker for %q, which is not on its ladder", d.name, c)
		}
	}
	// A DIALECT MAY NOT REDEFINE A CORE RELATION, because every type in the core
	// table participates in a computation — `attests` in evidence, `contradicts`
	// in `disputed`, `supersedes` in which fact wins, `derived_from` in staleness.
	// A dialect that could redefine one could change what a graph concludes,
	// silently.
	for _, k := range d.EdgeKeys() {
		if _, core := forwardEdge[k]; core {
			return fmt.Errorf("dialect %q redefines the core relation %q", d.name, k)
		}
		if _, core := inverseEdge[k]; core {
			return fmt.Errorf("dialect %q redefines the core inverse relation %q", d.name, k)
		}
	}
	// A SUBTYPE MAY NOT SHADOW A CORE KEY, and prefixing does not rescue it:
	// `acq:status` is unambiguous, but a subtype declaring bare `status` is
	// ambiguous on any node that did not prefix it, and whether the author
	// remembered is not a thing the format should depend on.
	for _, sname := range d.SubtypeNames() {
		st, _ := d.Subtype(sname)
		if _, core := bodyKeys[sname]; core {
			return fmt.Errorf("dialect %q subtype %q shadows the core kind key %q", d.name, sname, sname)
		}
		for _, k := range st.Keys() {
			if scalarFields[k] {
				return fmt.Errorf("dialect %q subtype %q governs the core scalar field %q", d.name, sname, k)
			}
			if _, core := forwardEdge[k]; core {
				return fmt.Errorf("dialect %q subtype %q governs the core relation %q", d.name, sname, k)
			}
			if _, core := inverseEdge[k]; core {
				return fmt.Errorf("dialect %q subtype %q governs the core inverse relation %q", d.name, sname, k)
			}
		}
	}
	return nil
}

// LoadDialects reads the CORPUS-WIDE `.kgraph/dialects/*.yaml`. Absent is fine —
// a corpus with no authored dialect resolves exactly as it did before this
// existed.
//
// A file that will not parse or will not validate is an ERROR, not a warning.
// Every other optional load here degrades to "not loaded, and silent about it";
// this one cannot, because continuing means evaluating a corpus's facts against
// a ladder nobody has checked and reporting a confident answer about it.
func LoadDialects(root string) (map[string]Dialect, error) {
	return loadDialectDir(filepath.Join(root, dialectsDir), dialectsDir, "")
}

// LoadProjectDialects reads the dialects a PROJECT owns, from
// `<index-dir>/.kgraph/dialects/*.yaml`, and qualifies each by the index that
// owns it: `engagement.yaml` under `acme/` is `acme.engagement`.
//
// QUALIFYING IS WHAT MAKES PROJECT DIALECTS SAFE. A corpus-wide file may not
// shadow a built-in, because `legal` would then mean something different per
// checkout. A project's may share a name with anything, because its identity
// carries the project in it and the fallback below is explicit about which was
// found. Two matters can each mean their own thing by `engagement` without
// either one being able to change what the other resolves.
//
// The default index has no marker and therefore no directory of its own, so it
// owns no dialects — the corpus-wide directory is exactly its scope.
func LoadProjectDialects(root string, decl IndexDecl) (map[string]Dialect, error) {
	if decl.Dir == "" {
		return nil, nil
	}
	rel := filepath.Join(decl.Dir, dialectsDir)
	return loadDialectDir(filepath.Join(root, rel), rel, decl.Name)
}

// loadDialectDir reads one directory of authored dialects. project is the index
// that owns them, or empty for the corpus-wide set.
func loadDialectDir(dir, rel, project string) (map[string]Dialect, error) {
	// Statted rather than read straight, because `.kgraph` is a project marker
	// that DiscoverRoot accepts as a plain FILE as well as a directory. Reading
	// through one yields ENOTDIR, which `os.IsNotExist` does not cover, and a
	// corpus that marks its root with a file would have failed to load at all.
	// Either way the answer is the same: nothing here authored a dialect.
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".yaml" || ext == ".yml" {
			names = append(names, e.Name())
		}
	}
	// Sorted so a corpus with two broken files reports the same one first every
	// run. A diagnostic that moves between runs is a diagnostic nobody trusts.
	sort.Strings(names)
	out := map[string]Dialect{}
	for _, n := range names {
		relFile := filepath.Join(rel, n)
		b, rerr := os.ReadFile(filepath.Join(dir, n))
		if rerr != nil {
			return nil, rerr
		}
		d, perr := parseDialectFile(relFile, b)
		if perr != nil {
			return nil, perr
		}
		// A CORPUS-WIDE name means one thing. Shadowing a built-in would make
		// `legal` depend on which checkout you are in, and a document's ladder is
		// not a thing that may vary by clone. A PROJECT's dialect is exempt because
		// it is qualified — see LoadProjectDialects.
		if project == "" {
			if _, built := dialects[d.Name()]; built {
				return nil, fmt.Errorf("%s: %q is a built-in dialect and may not be redefined; "+
					"rename this one", relFile, d.Name())
			}
		} else {
			d.name = project + "." + d.name
		}
		if prev, dup := out[d.Name()]; dup {
			return nil, fmt.Errorf("%s: %q is already defined by another file in %s",
				relFile, prev.Name(), rel)
		}
		out[d.Name()] = d
	}
	return out, nil
}

// dialectFrom resolves a declared name. An empty name is the default, not an
// error, for the reason on `legal`: every index that predates dialects declares
// nothing.
//
// PROJECT FIRST, THEN CORPUS-WIDE, THEN BUILT-IN. A bare `dialect: engagement`
// in acme's marker finds `acme.engagement` if acme wrote one and the corpus-wide
// `engagement` otherwise, which is what lets a matter specialise a shared
// vocabulary without forking the name. The fully qualified form is also
// accepted, so a marker can say `acme.engagement` and mean exactly that.
//
// The order is a real precedence rule rather than a lookup detail, so it is
// stated here and tested: nearest wins, the same rule `.kg-index` resolution
// itself uses.
func dialectFrom(name, project string, projectDialects, global map[string]Dialect) (Dialect, bool) {
	if name == "" {
		return legal, true
	}
	if project != "" {
		if d, ok := projectDialects[project+"."+name]; ok {
			return d, true
		}
	}
	// An already-qualified declaration, which is the internal identity written
	// out in full.
	if d, ok := projectDialects[name]; ok {
		return d, true
	}
	if d, ok := global[name]; ok {
		return d, true
	}
	return DialectFor(name)
}

// knownDialectNames is what a refusal lists. It includes the corpus's own and
// the project's, or the message would tell somebody their dialect does not exist
// while it sits in the directory next to the one they are editing.
func knownDialectNames(projectDialects, global map[string]Dialect) []string {
	out := DialectNames()
	for n := range global {
		out = append(out, n)
	}
	for n := range projectDialects {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// DialectHash identifies an AUTHORED dialect's content, and is the empty string
// for a built-in.
//
// Empty for built-ins so a corpus that declares none records exactly as it did
// before dialects were data — otherwise every document already rendered would
// report `dialect-changed` on the next status, which is the false-flag the
// staleness rules exist to prevent. A built-in also cannot be edited under a
// finished document; a file can, which is the whole difference.
func DialectHash(d Dialect) string { return d.hash }
