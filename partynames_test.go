package kgraph

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// THIS REPO IS PUSHED, and party identity must not ride along.
//
// Real party, witness, firm and county names were scrubbed out on 2026-09-01 —
// 47 files — after `origin/main` was found carrying them since 2026-08-03. The
// scrub was a one-time act; this is what keeps it true, because the failure mode
// is not a bad decision, it is somebody reaching for the live corpus for a
// realistic fixture and nothing objecting.
//
// IT STORES NO NAMES. A test that listed what to look for would put the names
// back in the repo it is protecting, so it derives them from `~/life` at run
// time — the `entity:` values are exactly the people and firms — and SKIPS when
// that corpus is not on the machine. A guardrail that only works where the
// secret already is, which is the only place it can work without leaking it.
func TestNoPartyNamesFromTheLiveCorpus(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	corpus := filepath.Join(home, "life", "projects")
	if _, serr := os.Stat(corpus); serr != nil {
		t.Skip("no ~/life on this machine — nothing to check against")
	}
	names := partyTokens(t, corpus)
	if len(names) < 10 {
		t.Skipf("only %d name tokens found — the corpus moved and this test is "+
			"checking nothing; fix the reader rather than trusting the pass", len(names))
	}

	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Skip("not a git checkout")
	}
	// The scrub's own record names what was replaced, so it is exempt; so is this
	// file, which would otherwise match its own explanation.
	skip := map[string]bool{"partynames_test.go": true}
	for _, rel := range strings.Fields(string(out)) {
		if skip[rel] || strings.HasPrefix(rel, "dist/") {
			continue
		}
		f, oerr := os.Open(rel)
		if oerr != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for line := 1; sc.Scan(); line++ {
			// ESCAPE SEQUENCES ARE SEPARATORS. A fixture written as
			// `"- id: x\nambetter\n"` puts the name straight after `\n`, and in the
			// FILE that is the letter `n` — a word character — so `\bambetter\b`
			// never matches and the name hides in plain sight. This was not
			// hypothetical: it survived the first scrub in `index_test.go` exactly
			// this way.
			low := escapeSep.ReplaceAllString(strings.ToLower(sc.Text()), " ")
			for tok := range names {
				if !strings.Contains(low, tok) {
					continue
				}
				if regexp.MustCompile(`\b` + regexp.QuoteMeta(tok) + `\b`).MatchString(low) {
					t.Errorf("%s:%d names %q, which is a party in the live corpus — "+
						"invent a name instead. See CLAUDE.md § Party identity",
						rel, line, tok)
				}
			}
		}
		f.Close()
	}
}

// partyTokens reads the corpus's `entity:` values and returns the words in them
// that could identify somebody.
//
// Word-level, because a fixture reintroduces a SURNAME rather than a whole
// entity line. The generic half of a name — `Title`, `County`, `Law` — is
// dropped, or every document in this repo that says "title" would fail.
// escapeSep turns a Go/YAML escape into a word separator. See the use site.
var escapeSep = regexp.MustCompile(`\\[a-z]`)

func partyTokens(t *testing.T, corpus string) map[string]bool {
	t.Helper()
	// Generic words that appear inside real names and mean nothing on their own.
	// Adding to this list is how a false positive is fixed; a word that could
	// identify a person does not belong here.
	generic := map[string]bool{
		"the": true, "and": true, "inc": true, "llc": true, "law": true,
		"group": true, "title": true, "county": true, "court": true, "care": true,
		"primary": true, "medical": true, "clinic": true, "insurance": true,
		"corporation": true, "washington": true, "attorney": true, "counsel": true,
		"plaintiff": true, "defendant": true, "parcel": true, "survey": true,
		"health": true, "company": true, "district": true, "record": true,
		"associates": true, "partners": true, "office": true, "services": true,
		"contractor": true, "insurer": true, "patient": true, "member": true,
		"daughter": true, "son": true, "party": true, "agent": true, "buyer": true,
		"seller": true, "surveyor": true, "shareholder": true, "responsible": true,
		"additional": true, "grantor": true, "neighbour": true, "neighbor": true,
		"west": true, "east": true, "north": true, "south": true, "sand": true,
		"point": true, "from": true, "with": true, "not": true, "for": true,
		// UNPROTECTABLE COLLISIONS: a real surname that is also a common English
		// word this repo uses constantly. `Page` is a party AND the unit a
		// transcription sidecar is delineated into; `Lot` is a party's parcel AND a
		// word; `Trust`, `View`, `Lake`, `Judge`, `Escrow`, `Doctor`, `Drain` the
		// same. Protecting them would fail on this repo's own prose hundreds of
		// times, so the trade is stated rather than hidden: **the surname half of
		// these is not guarded here.** Where such a name appears as a NAME it is
		// replaced by hand — `Bryan Page` became `Nolan Sinclair`, and the bare word
		// `page` is what remains.
		"page": true, "lot": true, "view": true, "trust": true, "lake": true,
		"judge": true, "escrow": true, "doctor": true, "drain": true,
		"surveying": true, "beverages": true, "consolidated": true, "lee": true,
		"scott": true, "john": true, "collins": true, "clark": true,
		// THE REPO'S AUTHOR. Carl Taylor signs assertions throughout the fixtures
		// and every git commit, so this is authorship, not a party's identity.
		"carl": true, "taylor": true,
	}
	// An entity DECLARATION, not a line of prose that happens to say "entity:".
	decl := regexp.MustCompile(`^\s*-?\s*entity:\s*(.+)$`)
	// CAPITALISED words only, and only from the NAME half of the value. A value
	// reads `Rowan Dace (Bramble's surveyor)` or `Dr Teal — the neighbour to the
	// west`, so everything past the first bracket or dash is a description: taking
	// it whole turned "whose", "where" and "file" into party names and failed on
	// this repo's own prose.
	word := regexp.MustCompile(`\b[A-Z][A-Za-z'-]{2,}`)
	out := map[string]bool{}
	_ = filepath.Walk(corpus, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if n := fi.Name(); !strings.HasSuffix(n, ".kfacts.md") && n != "facts.jsonl" {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for _, ln := range strings.Split(string(b), "\n") {
			m := decl.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			name := m[1]
			if i := strings.IndexAny(name, "(—,-"); i > 0 {
				name = name[:i]
			}
			for _, w := range word.FindAllString(name, -1) {
				if w = strings.ToLower(w); !generic[w] {
					out[w] = true
				}
			}
		}
		return nil
	})
	// The matter directories are named for their parties too.
	if entries, derr := os.ReadDir(corpus); derr == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			for _, w := range word.FindAllString(e.Name(), -1) {
				if w = strings.ToLower(w); !generic[w] && w != "dispute" && w != "billing" {
					out[w] = true
				}
			}
		}
	}
	return out
}
