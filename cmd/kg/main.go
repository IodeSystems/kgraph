// Command kg is the kgraph CLI.
//
// kgraph never calls an LLM. `render` prints a prompt; your agent generates and
// writes the artifacts; `attach` records that they came from that resolved
// state. Every command here is a pure function of files on disk.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/kgraph"
	"gopkg.in/yaml.v3"
)

const usage = `kg — knowledge graph with query-bound documents

  kg scan [--all]                parse everything, report problems (accepted hidden)
  kg accept <key>... --by NAME   rule a warning tolerable; keys from kg scan --json
  kg amend <id> <field> --by NAME change one field, carrying the rest forward
  kg status [spec]               which answers have moved, and why
  kg diff [spec]                 what moved, as statements
  kg diff --what-if -            blast radius of a fact read from stdin
  kg query '<dsl>'               resolve a query
                                 --build also runs the spec's ## Build step
  kg variants [spec]             open questions that would change a document, per answer
  kg extract                     documents nothing has been read from yet
  kg extract <doc>               the extraction prompt for one document
  kg extract --ready             unread documents that have readable text NOW
  kg quotes                      does the source's transcription contain the words a
                                 fact puts in quotation marks? triage, not a verdict
  kg attest                      human verdicts on citations: does the document say it?
  kg attest <fact> <source> <v>  record one: confirmed|affirmed|corrected|unsupported|unclear
                                 unsupported needs --by <person> — only a person can
                                 say a document does not support a proposition
  kg attest --todo               citations still awaiting a human sign-off
  kg attest --todo --used        only those a spec actually renders
  kg verify [--addr HOST:PORT]   the attestation workbench, in a browser
  kg have <identifier>           do we already hold this document? searches source
                                 aliases, titles, filenames and transcript text
  kg source status               which evidence documents moved since they were read
  kg source list                 every source, its document and its aliases — grep this
  kg source alias <id> <name>    record another name this document is known by
  kg source lock                 record the current version of each (the only writer)
  kg daemon [--addr A]           singleton daemon: REST + GraphQL + gRPC, all projects
  kg serve                       MCP on stdio, proxied to the daemon
  kg indexes                     indexes declared under the root, and their sizes
  kg scopes                      projects and branches the daemon holds
  kg hook <name>                 Claude Code hooks (kg hook install wires them up)

Flags: --root DIR  --index NAME  --now YYYY-MM-DD  --json  --token T  --addr HOST:PORT
       --by NAME (for attest; asked once, then reused)
       --store DSN  path | sqlite:<path> | postgres://… | none   ($KGRAPH_STORE)

An index is a closed graph. Facts, specs and everything computed from them —
disputed, supersedes, sem_hash — resolve strictly within one index, so an
unrelated matter in the same checkout can never mark a claim disputed or move a
document's hash. Declare one with a .kg-index file; commands default to the index
owning the working directory.

The store is a LOCATION you may name. It defaults to ~/.kgraph/<index>.db, which
is keyed by index name alone — two corpora naming an index the same thing share
it. --store puts it where you want; --store none loads from the exported log and
refuses every write, saying that it refused.
`

type opts struct {
	root  string
	index string
	// indexSet records whether --index was given at all, so an explicit
	// `--index ""` can select the default index rather than being mistaken for
	// "not specified" and re-derived from cwd.
	indexSet bool
	now      string
	asJSON   bool
	token    string
	addr     string
	by       string
	// store is WHERE the assertion store is: a path, `sqlite:<path>`,
	// `postgres://…`, or `none`. Empty takes the default policy.
	store string
	// note is prose about the ACT of asserting, not about the fact. It is where
	// the explanation a *.kfacts.md carried in its markdown goes.
	note string
}

func main() {
	var o opts
	fs := newFlagSet(&o)

	args := os.Args[1:]
	if err := fs.Parse(args); err != nil {
		fatal("%v\n\n%s", err, usage)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Print(usage)
		os.Exit(2)
	}
	// A GLOBAL FLAG AFTER THE SUBCOMMAND IS REFUSED, not ignored.
	//
	// `flag.Parse` stops at the first positional, so everything after the
	// subcommand arrives untouched and every global flag written there was
	// silently dropped. Two ways that went wrong, both of which cost real work:
	//
	//   kg scan --json                 printed text, not JSON
	//   kg attest <f> <s> confirmed --by dana
	//                                  recorded the verdict UNSIGNED, and joined
	//                                  "--by dana" into the note, because attest
	//                                  takes its note from the trailing words
	//
	// The second is the serious one. `attestations.jsonl` is the record of who
	// checked what, and that is the one table a silently unattributed row must
	// never reach. It is also exactly how the usage text reads, so the natural
	// invocation was the broken one.
	//
	// `addr` is excluded because `verify` and `daemon` scan for it themselves and
	// their documented usage puts it after the subcommand.
	globals := globalFlagNames(fs)
	for _, a := range rest[1:] {
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if globals[name] {
			why := "it is taken as an argument and the command carries on without it"
			if rest[0] == "attest" && name == "by" {
				why = "`attest` takes its note from the trailing words, so this became " +
					"the NOTE and the verdict was recorded signed by nobody"
			}
			fatal("--%s is a global flag and has to come BEFORE the subcommand:\n"+
				"    kg --%s ... %s ...\n"+
				"written after it, %s", name, name, rest[0], why)
		}
	}

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "index" {
			o.indexSet = true
		}
	})
	if o.root == "" {
		o.root = discoverRoot()
	}
	if !o.indexSet {
		// Routing is implicit from cwd, the same way root discovery is: working
		// inside a project should operate on that project without being told.
		if cwd, err := os.Getwd(); err == nil {
			idx, ierr := kgraph.IndexAt(o.root, cwd)
			if ierr != nil {
				// Falling back to the default index here would run the command against
				// the wrong graph without saying so.
				fatal("%v", ierr)
			}
			o.index = idx
		}
	}

	var err error
	switch cmd := rest[0]; cmd {
	case "scan":
		err = cmdScan(o, rest[1:])
	case "accept":
		err = cmdAccept(o, rest[1:])
	case "amend":
		err = cmdAmend(o, rest[1:])
	case "status":
		err = cmdStatus(o, rest[1:])
	case "diff":
		err = cmdDiff(o, rest[1:], fs)
	case "query":
		err = cmdQuery(o, rest[1:])
	case "ask":
		err = cmdAsk(o, rest[1:])
	case "migrate":
		err = cmdMigrate(o, rest[1:])
	case "assert", "withdraw", "correct", "answer", "retire":
		err = cmdWrite(o, cmd, rest[1:])
	case "export":
		err = cmdExport(o, rest[1:])
	case "import":
		err = cmdImport(o, rest[1:])
	case "variants":
		err = cmdVariants(o, rest[1:])
	case "daemon":
		addr := o.addr
		if len(rest) > 1 {
			addr = rest[1]
		}
		err = kgraph.NewDaemon().Serve(addr)
	case "serve":
		// The shim forwards its cwd; the daemon resolves the project and branch.
		cwd, _ := os.Getwd()
		err = kgraph.ServeMCP(o.addr, cwd, o.now, os.Stdin, os.Stdout)
	case "indexes":
		err = cmdIndexes(o)
	case "scopes":
		err = cmdScopes(o)
	case "hook":
		err = cmdHook(o, rest[1:])
	case "source":
		err = cmdSource(o, rest[1:])
	case "have":
		err = cmdHave(o, rest[1:])
	case "extract":
		err = cmdExtract(o, rest[1:])
	case "quotes":
		err = cmdQuotes(o, rest[1:])
	case "attest":
		err = cmdAttest(o, rest[1:])
	case "verify":
		err = cmdVerify(o, rest[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fatal("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fatal("%v", err)
	}
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "kg: "+f+"\n", a...)
	os.Exit(1)
}

// discoverRoot walks up for .kgraph/, then for .git — raglit's pattern.
func discoverRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for _, marker := range []string{".kgraph", ".git"} {
		d := dir
		for {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	return dir
}

// cmdIndexes lists what exists. Without it the scheme is a trap: marking a
// subtree removes its facts from the default index, and a user who does not
// already know about indexes would see them simply disappear.
func cmdIndexes(o opts) error {
	names, err := kgraph.Indexes(o.root)
	if err != nil {
		return err
	}
	type row struct {
		Index string `json:"index"`
		Facts int    `json:"facts"`
		Specs int    `json:"specs"`
		// Dialect is the evidentiary ladder and rule set this index binds to. Shown
		// because this is the only command that talks about indexes, and a feature
		// declared in a file with a CLOSED key set is undiscoverable if nothing ever
		// names it: guessing the key wrong is a hard error, and there was nothing to
		// guess from.
		Dialect string `json:"dialect"`
		// Store is WHERE this index's facts were read from. Shown because two
		// locations for one index — a stale ~/.kgraph/<index>.db beside a corpus's
		// own store — is a shadowing failure that exists whether or not anybody
		// names a store, and naming it is only an improvement if the answer is
		// visible without guessing.
		Store   string `json:"store"`
		Current bool   `json:"current"`
	}
	ref, named := storeRefOf(o)
	var rows []row
	for _, n := range names {
		// Counted from the LOGS, not the fact files. A migrated index has none of
		// the latter, and reporting "0 fact file(s)" for a corpus holding ninety-
		// nine facts is the kind of true-but-useless number somebody acts on.
		facts, err := kgraph.FindFactLogs(o.root, n)
		if err != nil {
			return err
		}
		if len(facts) == 0 {
			if legacy, lerr := kgraph.FindFacts(o.root, n); lerr == nil {
				facts = legacy
			}
		}
		// Declarations are standing queries now. Counted from the index's own
		// `standing.yaml` rather than from files, since one file holds them all
		// and a count of 1 would say nothing about a corpus with 289 questions.
		var specs []string
		if set, serr := kgraph.ReadStandingIn(o.root, n); serr == nil {
			specs = make([]string, 0, len(set))
			for name := range set {
				specs = append(specs, name)
			}
		}
		// Resolved from a file the index owns, since an index is named by its
		// marker and reached through its members. An unknown dialect is reported in
		// the row and does not abort the listing: this is the command somebody runs
		// BECAUSE a marker is wrong, and failing it would hide every other index.
		dialect := "?"
		var dir string
		switch {
		case len(facts) > 0:
			dir = filepath.Dir(filepath.Join(o.root, facts[0]))
		case len(specs) > 0:
			dir = filepath.Dir(filepath.Join(o.root, specs[0]))
		}
		if dir != "" {
			if d, derr := kgraph.DialectAt(o.root, dir); derr != nil {
				fmt.Fprintln(os.Stderr, "kg:", derr)
			} else {
				dialect = d.Name()
			}
		}
		// A named store is one location for whichever index is being read; an
		// unnamed one is the per-index default policy, so it differs per row.
		at := kgraph.DefaultStoreRef(n)
		if named {
			at = ref
		}
		store := at.Display()
		if at.IsNone() {
			store += " — reading the exported log, and every write refused"
		} else if path, ok := at.Path(); ok {
			if _, serr := os.Stat(path); serr != nil {
				store += " (no database there — reading the exported log)"
			}
		}
		rows = append(rows, row{Index: n, Facts: len(facts), Specs: len(specs),
			Dialect: dialect, Store: store, Current: n == o.index})
	}
	if o.asJSON {
		return emit(o, rows)
	}
	for _, r := range rows {
		name := r.Index
		if name == kgraph.DefaultIndex {
			name = "(default — unmarked files)"
		}
		marker := " "
		if r.Current {
			marker = "*"
		}
		fmt.Printf("%s %-34s %d fact store(s) · %d spec(s) · %s\n", marker, name, r.Facts, r.Specs, r.Dialect)
		fmt.Printf("  %-34s store: %s\n", "", r.Store)
	}
	if len(rows) > 1 {
		fmt.Println("\n* is the index owning the working directory. Queries resolve within one index;")
		fmt.Println("crossing indexes has to be asked for explicitly.")
	}
	// The default is asked for rather than spelled, so this line cannot drift from
	// what the resolver actually does.
	fallback, _ := kgraph.DialectFor("")
	fmt.Printf("\nAn index binds a DIALECT — its evidentiary ladder and rule set — declared as\n"+
		"`dialect: <name>` in its .kg-index marker, alongside `index: <name>`. Unmarked\n"+
		"files and a marker naming none take %s. This binary knows: %s.\n",
		fallback.Name(), strings.Join(kgraph.DialectNames(), ", "))
	return nil
}

func load(o opts) (*kgraph.Project, error) {
	p, diags, err := loadIn(o)
	if err != nil {
		return nil, err
	}
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d)
	}
	if errs := kgraph.Errors(diags); len(errs) > 0 {
		return nil, fmt.Errorf("%d error(s) in the graph — fix them before continuing", len(errs))
	}
	return p, nil
}

// env carries the clock and the shared named-query library.
func env(o opts, p *kgraph.Project) kgraph.Env {
	return kgraph.Env{Now: o.now, Named: p.Named}
}

func emit(o opts, v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ── scan ───────────────────────────────────────────────────────────────

// cmdExtract is the read direction: kgraph emits a prompt, an agent writes the
// facts, `kg scan` validates them. kgraph does not call a model here either.
func cmdExtract(o opts, args []string) error {
	p, _, err := loadIn(o)
	if err != nil {
		return err
	}
	// `--ready` is the work queue: unread AND readable. The rest of the backlog is
	// real but not actionable until something transcribes it.
	ready := len(args) == 1 && (args[0] == "--ready" || args[0] == "ready")
	if ready {
		args = nil
	}
	if len(args) == 0 {
		docs, err := kgraph.Backlog(o.root, p.Index, p.Graph)
		if err != nil {
			return err
		}
		if o.asJSON {
			return emit(o, docs)
		}
		counts := map[string]int{}
		nReady := 0
		for _, d := range docs {
			counts[d.State]++
			if d.State == "unread" && d.Ready {
				nReady++
			}
			if d.State == "read" {
				continue
			}
			if ready && !(d.State == "unread" && d.Ready) {
				continue
			}
			label := d.Doc
			if label == "" {
				label = d.Title // an undocumented source has no path to print
			}
			fmt.Printf("%-12s %-52s", d.State, label)
			if d.Source != "" {
				fmt.Printf(" %s (%d fact(s))", d.Source, d.Facts)
			}
			fmt.Println()
		}
		if ready {
			fmt.Printf("\n%d document(s) unread AND readable — extractable right now.\n", nReady)
			fmt.Println("`kg extract <doc>` prints the prompt for one.")
			return nil
		}
		fmt.Printf("%d unread (%d readable now) · %d thin · %d undocumented · %d missing · %d read\n",
			counts["unread"], nReady, counts["thin"], counts["undocumented"],
			counts["missing"], counts["read"])
		if counts["unread"] > 0 {
			fmt.Println("\nUnread means no source node cites it — invisible to every other command.")
			fmt.Println("`kg extract <doc>` prints the prompt for one.")
		}
		if counts["undocumented"] > 0 {
			fmt.Printf("\n%d source(s) cite a document the corpus does not hold: facts rest on them,\n",
				counts["undocumented"])
			fmt.Println("but there is no file to hash, to re-read, or to attach to a filing. Find the")
			fmt.Println("document and add `doc:`, or record an action to pull it.")
		}
		return nil
	}
	x, err := p.Graph.Extract(o.root, args[0], env(o, p))
	if err != nil {
		return err
	}
	if o.asJSON {
		return emit(o, x)
	}
	// Prompt on stdout, metadata on stderr, same split as `kg render`, so the
	// prompt can be piped without the header contaminating it.
	fmt.Fprintf(os.Stderr, "document: %s\nsource:   %s\nknown:    %d fact(s)\ninlined:  %t\n",
		x.Doc, orDash(x.Source), x.Known, x.Inlined)
	fmt.Print(x.Prompt)
	return nil
}

// trimTo caps a printed passage.
func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func orDash(s string) string {
	if s == "" {
		return "— not declared"
	}
	return s
}

// cmdSource reports and records the version of each document the facts were read
// from. `status` never writes; `lock` is the only thing that does.
func cmdSource(o opts, args []string) error {
	sub, apply := "status", false
	// Positional arguments, flags removed. `alias` needs two of them, and the
	// previous loop kept only the first — it was written when every subcommand
	// took none.
	var pos []string
	for _, a := range args {
		if a == "--apply" {
			apply = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) > 0 {
		sub = pos[0]
	}
	g, _, err := scanFactsIn(o)
	if err != nil {
		return err
	}
	switch sub {
	case "status":
		l, err := kgraph.ReadLocks(o.root, g)
		if err != nil {
			return err
		}
		sts, _ := g.SourceStatuses(o.root, l)
		return printSources(o, sts)
	case "repair":
		// A `doc:` is a path, and a path is not an identity. The lock's sha256 is,
		// and it survives a move — so a not-found citation whose recorded bytes
		// still exist in the index is a relocation, not a broken reference.
		l, err := kgraph.ReadLocks(o.root, g)
		if err != nil {
			return err
		}
		moved, amb, err := kgraph.FindRelocations(o.root, g, l)
		if err != nil {
			return err
		}
		if o.asJSON {
			return emit(o, map[string]any{"moved": moved, "ambiguous": amb})
		}
		for _, a := range amb {
			fmt.Printf("ambiguous  %-28s %s\n           candidates: %s\n",
				a.SourceID, a.Was, strings.Join(a.Candidates, ", "))
		}
		for _, m := range moved {
			fmt.Printf("moved      %-28s %s\n           -> %s\n", m.SourceID, m.Was, m.Now)
		}
		if len(moved) == 0 && len(amb) == 0 {
			fmt.Println("no relocated sources — every `doc:` resolves, or its bytes are gone")
			return nil
		}
		if !apply {
			fmt.Printf("\n%d relocation(s), %d ambiguous. Re-run with --apply to rewrite `doc:`.\n",
				len(moved), len(amb))
			return nil
		}
		n, err := kgraph.ApplyRelocations(o.root, moved)
		if err != nil {
			return err
		}
		// The rewrite changes what every citing document SAYS — `attach` prints the
		// path into the citation line — so those documents are now stale and will
		// report `facts-changed`. Saying so here matters: re-locking puts the
		// sources back at `ok`, which reads like the job is finished.
		fmt.Printf("\nrewrote %d `doc:` path(s) — run `kg source lock` to re-record them,\n"+
			"then `kg status`: documents citing them now read the old path and need regenerating\n", n)
		return nil
	case "lock":
		// NAMED IDS LOCK ONLY THEMSELVES. Without this, locking one new document
		// re-locked every `changed` one — a drift signal somebody was midway
		// through investigating, erased by an unrelated lock, twice in one session.
		ids := pos[1:]
		for _, id := range ids {
			if _, ok := g.Lookup(id); !ok {
				return fmt.Errorf("no source %q in this index — `kg source list` to find it", id)
			}
		}
		_, after, err := kgraph.LockAll(o.root, g, ids...)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			// Report only what was asked for, or the sweep it refused to do reads
			// as one it performed.
			var shown []kgraph.SourceStatus
			for _, st := range after {
				for _, id := range ids {
					if st.ID == id {
						shown = append(shown, st)
					}
				}
			}
			after = shown
		}
		return printSources(o, after)
	case "list":
		// One grep-able line per source. `kg have` does matching in Go, which is
		// right for the transcript tier but wrong as the only way in: a flat
		// listing composes with grep, sort, awk and everything else, and the
		// question is usually "is this number anywhere in the catalogue" — which
		// is a grep, not a feature.
		type row struct {
			ID      string   `json:"id"`
			Class   string   `json:"class"`
			At      string   `json:"at"`
			Doc     string   `json:"doc,omitempty"`
			OnDisk  bool     `json:"on_disk"`
			Anchor  string   `json:"anchor,omitempty"`
			Aliases []string `json:"aliases,omitempty"`
			// Derived are identifiers read off the filename, not declared. Kept in
			// a separate field so a caller can tell what somebody asserted from
			// what the path happens to say.
			Derived []string `json:"derived,omitempty"`
			Title   string   `json:"title"`
		}
		var rows []row
		for id, n := range g.Nodes {
			if n.Kind != kgraph.KSource || n.Source == nil {
				continue
			}
			rel, _ := kgraph.DocPathOf(o.root, n)
			on := false
			if rel != "" {
				if _, err := os.Stat(filepath.Join(o.root, rel)); err == nil {
					on = true
				}
			}
			var al []string
			for _, a := range n.Aliases {
				al = append(al, a.As)
			}
			// What is this document called when nobody has named it? Its filename
			// already carries the registry number, and showing that is the whole
			// job — an empty `aka` column on a file called `8801200011-...pdf` is
			// the listing failing to report what it can plainly see.
			derived := kgraph.Identifiers(rel)
			rows = append(rows, row{ID: id, Class: n.Source.Class, At: n.At, Doc: rel,
				OnDisk: on, Anchor: n.Source.Anchor, Aliases: al, Derived: derived,
				Title: firstLineOf(n.Body)})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		if o.asJSON {
			return emit(o, rows)
		}
		for _, r := range rows {
			state := "ok"
			switch {
			case r.Doc == "":
				state = "no-doc"
			case !r.OnDisk:
				state = "MISSING"
			}
			line := fmt.Sprintf("%-34s %-10s %-7s %-10s %s", r.ID, r.Class, state, r.At, r.Doc)
			if r.Anchor != "" {
				line += " [" + r.Anchor + "]"
			}
			if len(r.Aliases) > 0 {
				line += "  aka " + strings.Join(r.Aliases, " · ")
			}
			if len(r.Derived) > 0 {
				line += "  [" + strings.Join(r.Derived, " · ") + "]"
			}
			fmt.Println(line + "  — " + r.Title)
		}
		return nil
	case "alias":
		// Naming a document is how the lookup stops guessing. Two arguments, so
		// it costs less than opening the file — which is the point: the catalogue
		// is only maintained if maintaining it is trivial.
		if len(pos) < 3 {
			return fmt.Errorf("kg source alias <source-id> <alias> — the name this document is also known by")
		}
		name := strings.Join(pos[2:], " ")
		by := o.by
		if by == "" {
			by = kgraph.LastHumanSigner(o.root, kgraph.IndexDirOf(g))
		}
		if by == "" {
			return fmt.Errorf("who is naming it? pass --by <you> — an alias is an assertion " +
				"about what a document is called, and it is recorded like one")
		}
		st, serr := openStore(o)
		if serr != nil {
			return serr
		}
		defer st.Close()
		if err := kgraph.AddAliasTo(st, g.Dialect(), g, pos[1], name, by); err != nil {
			return err
		}
		fmt.Printf("%s is now also known as %q\n", pos[1], name)
		return nil
	case "dupes":
		// The overflow message has asked for this since it was written: "a corpus
		// with this many is better fixed by pattern than one at a time."
		clusters := g.DuplicateClusters()
		if o.asJSON {
			return emit(o, clusters)
		}
		if len(clusters) == 0 {
			fmt.Println("no possible duplicates. `kg scan` reports them as they appear.")
			return nil
		}
		same, compound := 0, 0
		for _, c := range clusters {
			mark := " "
			switch {
			case c.Compound:
				mark = "+"
				compound++
			case c.SameFile:
				mark = "="
				same++
			}
			fmt.Printf("%s %d entr(ies), %d citation(s)\n", mark, len(c.IDs), c.Cites)
			if c.SameFile {
				fmt.Printf("    all point at %s\n", c.Doc)
			}
			if c.Compound {
				// A COMPOUND DOCUMENT WANTS AN ANCHOR, NOT A MERGE. Suggesting
				// `retire` here would erase one instrument into another — on the live
				// corpus, a 1947 record into a 2023 declaration.
				fmt.Printf("    SEPARATE INSTRUMENTS in one document — %s. Give each an "+
					"`anchor:` naming where it is; do NOT merge them.\n", c.Why)
				for _, id := range c.IDs {
					fmt.Printf("      %s\n", id)
				}
				continue
			}
			fmt.Printf("    keep %s?\n", c.Keep)
			for _, id := range c.IDs {
				if id == c.Keep {
					continue
				}
				fmt.Printf("    kg retire %s %s --by <you>\n", id, c.Keep)
			}
		}
		fmt.Printf("\n%d cluster(s) from %d node(s): %d marked + are SEPARATE INSTRUMENTS "+
			"sharing one document and want an `anchor:`, %d marked = are one document entered "+
			"twice.\n", len(clusters), countIDs(clusters), compound, same)
		fmt.Println("`kg retire <dup> <keep>` folds one into the other; references to the retired " +
			"id keep resolving. If they are NOT duplicates, `kg accept <key>` from `kg scan --json`.")
		return nil
	default:
		return fmt.Errorf("unknown `source` subcommand %q — status, list, lock, repair, alias or dupes", sub)
	}
}

func printSources(o opts, sts []kgraph.SourceStatus) error {
	if o.asJSON {
		return json.NewEncoder(os.Stdout).Encode(sts)
	}
	counts := map[kgraph.SourceState]int{}
	for _, st := range sts {
		counts[st.State]++
		// `ok` and `not-found` are the quiet states. Listing every unchanged
		// exhibit buries the one that moved.
		if st.State == kgraph.SrcOK {
			continue
		}
		fmt.Printf("%-12s %-28s %s", st.State, st.ID, st.Doc)
		if st.Facts > 0 {
			fmt.Printf("  (%d fact(s))", st.Facts)
		}
		fmt.Println()
	}
	fmt.Printf("%d ok · %d changed · %d vanished · %d unrecorded · %d not found here\n",
		counts[kgraph.SrcOK], counts[kgraph.SrcChanged], counts[kgraph.SrcVanished],
		counts[kgraph.SrcUnrecorded], counts[kgraph.SrcNotFound])
	if counts[kgraph.SrcChanged] > 0 {
		fmt.Println("\nA changed document means the facts read from it may no longer say what they say.")
		fmt.Println("Re-read it, fix or confirm those facts, then `kg source lock`.")
	}
	return nil
}

func cmdScan(o opts, args []string) error {
	all := false
	for _, a := range args {
		if a == "--all" || a == "all" {
			all = true
		}
	}
	p, diags, err := loadIn(o)
	if err != nil {
		return err
	}
	// An open question asking for a document the corpus already holds. Needs the
	// filesystem, so it cannot live with the pure-graph checks in Build.
	diags = append(diags, p.Graph.CheckAlreadyHeld(o.root, p.Index)...)
	// A verdict whose passage has moved under it. Needs the filesystem for the
	// same reason and is here for the same reason: a re-reading of an unchanged
	// scan moves the words and leaves the bytes alone, so nothing else can see
	// it.
	if set, aerr := kgraph.ReadAttestations(o.root, kgraph.IndexDirOf(p.Graph)); aerr == nil {
		diags = append(diags, kgraph.ExcerptDiags(p.Graph,
			p.Graph.CheckAttestedExcerpts(o.root, set))...)
	}
	// A spec query that resolves to nothing is almost always a reversed hop; it
	// fails silently as an empty section, so scan reports it.
	for _, s := range p.DeclaredSets() {
		pins, err := p.Graph.Resolve(s, env(o, p))
		if err != nil {
			diags = append(diags, kgraph.Diag{File: s.Path, Line: 1, Severity: kgraph.SevError, Msg: err.Error()})
			continue
		}
		for name, pin := range pins {
			if pin.Count == 0 {
				diags = append(diags, kgraph.Diag{File: s.Path, Line: 1, Severity: kgraph.SevWarn,
					Check: kgraph.CheckEmptyQuery,
					Key:   kgraph.FindingKeyOf(p.Graph, kgraph.CheckEmptyQuery, s.Name+"."+name),
					Msg:   fmt.Sprintf("query %q resolves to 0 rows — check the hop direction", name)})
			}
		}
	}
	if o.asJSON {
		return emit(o, diags)
	}
	// ACCEPTED FINDINGS ARE HIDDEN, AND THEIR COUNT IS NOT. A check whose output
	// nobody can read is wallpaper; a check that silently drops findings is worse.
	// So the scan shows what is outstanding and says how much has been ruled on,
	// and `--all` shows everything.
	open, accepted := p.Triage(diags)
	shown := diags
	if !all {
		shown = open
	}
	for _, d := range shown {
		fmt.Println(d)
	}
	n := len(kgraph.Errors(diags))
	// The count matches what was PRINTED. Reporting the outstanding number under
	// a full listing is the kind of small lie that makes a person stop trusting
	// the summary line.
	fmt.Printf("%d nodes · %d edges · %d standing quer(ies) · %d error(s), %d warning(s)",
		len(p.Graph.Nodes), len(p.Graph.Edges), len(p.Standing), n, len(shown)-n)
	if len(accepted) > 0 && !all {
		fmt.Printf(" · %d accepted (--all to see them)", len(accepted))
	}
	fmt.Println()
	// An acceptance nobody may honour is REPORTED rather than ignored: a machine
	// that could silence the corpus's own complaints is the hazard the whole
	// human-in-the-loop design exists to prevent.
	for _, a := range p.Accepted.Unhonoured() {
		fmt.Fprintf(os.Stderr,
			"kg: acceptance of %s is signed %q, a machine identity — it is NOT honoured, "+
				"and the finding still stands. Re-accept it signed by a person.\n", a.Check, a.By)
	}
	if n > 0 {
		os.Exit(1)
	}
	// THE WATERMARK IS RAISED ONLY AFTER A CLEAN SCAN. Recording a count from a
	// run that had errors in it would pin the corpus at whatever a broken load
	// happened to produce, and the mark exists to be compared against.
	if err := kgraph.WriteHighWater(o.root, kgraph.IndexDirOf(p.Graph), p.Graph, o.now); err != nil {
		fmt.Fprintf(os.Stderr, "kg: the liveness mark was not updated: %v\n", err)
	}
	return nil
}

// ── status ─────────────────────────────────────────────────────────────

// cmdStatus reports which standing answers have moved.
//
// It reported which DOCUMENTS were stale. Same question, and the thing it is
// asked about outlived the thing it used to be asked about.
func cmdStatus(o opts, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: kg status [name]")
	}
	only := ""
	if len(args) == 1 {
		only = args[0]
	}
	p, err := load(o)
	if err != nil {
		return err
	}
	set, err := kgraph.ReadStanding(o.root, kgraph.IndexDirOf(p.Graph))
	if err != nil {
		return err
	}
	type row struct {
		Name  string   `json:"name"`
		Rows  int      `json:"rows"`
		Drift []string `json:"drift,omitempty"`
	}
	var names []string
	for n := range set {
		if only != "" && n != only {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	var rows []row
	for _, n := range names {
		a, aerr := p.Graph.Ask(set[n], env(o, p))
		if aerr != nil {
			return aerr
		}
		rows = append(rows, row{n, a.Pin.Count, a.Drift})
	}
	if o.asJSON {
		return emit(o, rows)
	}
	if len(rows) == 0 {
		fmt.Println("nothing asked yet. `kg ask add <name> '<dsl>'`")
		return nil
	}
	for _, r := range rows {
		state := "unchanged"
		if len(r.Drift) > 0 {
			state = strings.Join(r.Drift, ", ")
		}
		fmt.Printf("%-28s %3d row(s)  %s\n", r.Name, r.Rows, state)
	}
	return nil
}

// ── diff ───────────────────────────────────────────────────────────────

func cmdDiff(o opts, args []string, root *flag.FlagSet) error {
	var whatIf string
	sub := flag.NewFlagSet("diff", flag.ContinueOnError)
	sub.SetOutput(io.Discard)
	sub.StringVar(&whatIf, "what-if", "", "a fragment of entries, or - to read stdin")
	if err := sub.Parse(args); err != nil {
		return fmt.Errorf("%v\n\n%s", err, usage)
	}
	p, err := load(o)
	if err != nil {
		return err
	}
	specs, err := pick(p, sub.Args())
	if err != nil {
		return err
	}

	var deltas []*kgraph.SpecDelta
	if whatIf != "" {
		frag := whatIf
		if frag == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			frag = string(b)
		}
		scratch, ds := p.Graph.WhatIf(frag)
		if scratch == nil {
			for _, d := range ds {
				fmt.Fprintln(os.Stderr, d)
			}
			return fmt.Errorf("the fragment does not parse")
		}
		for _, s := range specs {
			d, err := kgraph.DiffGraphs(p.Graph, scratch, s, env(o, p))
			if err != nil {
				return err
			}
			deltas = append(deltas, d)
		}
	} else {
		// Against the ACCEPTED answers, not against a document's sealed pins. The
		// two are the same comparison — a recorded resolution against a current one
		// — and only the thing holding the record changed.
		set, serr := kgraph.ReadStanding(o.root, kgraph.IndexDirOf(p.Graph))
		if serr != nil {
			return serr
		}
		var names []string
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a, aerr := p.Graph.Ask(set[n], env(o, p))
			if aerr != nil {
				return aerr
			}
			deltas = append(deltas, &kgraph.SpecDelta{
				Spec: a.Name, State: a.Drift, Sets: []kgraph.SetDelta{a.Delta}})
		}
	}

	if o.asJSON {
		return emit(o, deltas)
	}
	var any bool
	for _, d := range deltas {
		lines := d.Statements(p.Graph)
		if len(lines) == 0 && !d.Stale() {
			continue
		}
		any = true
		fmt.Printf("\n%s  [%s]\n", d.Spec, strings.Join(d.State, ", "))
		for _, l := range lines {
			fmt.Printf("  · %s\n", l)
		}
	}
	if !any {
		fmt.Println("nothing moved")
	}
	return nil
}

// ── query ──────────────────────────────────────────────────────────────

func cmdQuery(o opts, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kg query '<dsl>'")
	}
	p, err := load(o)
	if err != nil {
		return err
	}
	q, err := kgraph.ParseQuery(strings.Join(args, " "))
	if err != nil {
		return err
	}
	// Validated IN the index's dialect, not the default one. `kg scan` has always
	// resolved the marker's dialect, so an index could hold facts this command
	// then refused to ask about: a query naming a dialect edge or a dialect's
	// source class came back "unknown edge type" / "unknown source class … in the
	// legal dialect" while the very same corpus scanned clean.
	if errs := kgraph.ValidateQueryIn(p.Graph.Dialect(), q); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "kg:", e)
		}
		return fmt.Errorf("query does not validate")
	}
	ids, err := p.Graph.Eval(q, env(o, p))
	if err != nil {
		return err
	}
	if o.asJSON {
		return emit(o, ids)
	}
	for _, id := range ids {
		n := p.Graph.Nodes[id]
		fmt.Printf("%-34s %-9s %s\n", id, n.Kind, n.Body)
	}
	fmt.Fprintf(os.Stderr, "%d row(s)\n", len(ids))
	return nil
}

// ── render ─────────────────────────────────────────────────────────────

// ── attach ─────────────────────────────────────────────────────────────

// ── helpers ────────────────────────────────────────────────────────────

func pick(p *kgraph.Project, args []string) ([]kgraph.DeclaredSet, error) {
	if len(args) == 0 {
		return p.DeclaredSets(), nil
	}
	var out []kgraph.DeclaredSet
	for _, a := range args {
		s, err := p.Declared(a)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func cmdScopes(o opts) error {
	sc, err := kgraph.NewRegistry().Get(o.root)
	if err != nil {
		return err
	}
	if o.asJSON {
		return emit(o, sc)
	}
	fmt.Printf("%s\n  branch %s · %d nodes · %d edges · %d specs · %d error(s)\n",
		sc.Root, sc.Branch, sc.Nodes, sc.Edges, sc.Specs, sc.Errors)
	return nil
}

// cmdVariants answers "what is this document waiting on, and what would each
// answer change" — the difference between a stale document and one that cannot
// be finished yet.
func cmdVariants(o opts, args []string) error {
	p, err := load(o)
	if err != nil {
		return err
	}
	specs, err := pick(p, args)
	if err != nil {
		return err
	}
	var all []kgraph.VariantReport
	for _, s := range specs {
		rs, err := p.Graph.Variants(s, env(o, p))
		if err != nil {
			return err
		}
		all = append(all, rs...)
	}
	if o.asJSON {
		return emit(o, all)
	}
	if len(all) == 0 {
		fmt.Println("no open question would change a document")
		return nil
	}
	for _, r := range all {
		note := "blocks this section; either answer yields the same text"
		if r.Diverges {
			note = "the answer CHANGES what the document says"
		}
		fmt.Printf("\n%s\n  ? %s (%s)\n    → %s\n", r.Spec, r.Body, r.Question, note)
		if !r.Diverges {
			continue
		}
		for _, v := range r.Variants {
			label := v.Body
			if label == "" {
				label = v.Option
			}
			fmt.Printf("    if %s:\n", label)
			if len(v.Delta) == 0 {
				fmt.Printf("      (no change)\n")
			}
			for _, l := range v.Delta {
				fmt.Printf("      · %s\n", l)
			}
		}
	}
	return nil
}

// cmdAttest records and reports human verdicts on citations.
//
// The CLI comes before the viewer deliberately: the verdict is the durable
// thing, and the page-region viewer is a faster way to reach the same record.
// Anything the viewer will write, this writes too.
func cmdAttest(o opts, args []string) error {
	p, err := load(o)
	if err != nil {
		return err
	}
	dir := kgraph.IndexDirOf(p.Graph)
	usedOnly, attestAll := false, false
	for _, a := range args {
		if a == "--used" {
			usedOnly = true
		}
		if a == "--all" {
			attestAll = true
		}
	}
	args = dropFlag(args, "--all")
	if usedOnly {
		out := args[:0]
		for _, a := range args {
			if a != "--used" {
				out = append(out, a)
			}
		}
		args = out
	}
	if len(args) == 1 && (args[0] == "--todo" || args[0] == "todo") {
		// The checklist. A machine reading is a candidate, not a verification: good
		// enough to work from, not good enough to file on. An UNSIGNED verdict counts
		// as pending too — if nobody said who checked it, nobody checked it.
		pending := p.Graph.PendingHuman()
		used := p.FactsUsedByDeclarations(env(o, p))
		if usedOnly {
			keep := pending[:0]
			for _, a := range pending {
				if len(used[a.Fact]) > 0 {
					keep = append(keep, a)
				}
			}
			pending = keep
		}
		// RANKED BY CONSEQUENCE, in the library so every surface agrees. Which of
		// these has a quotation the transcription does not contain? That is where
		// a person's time is worth most: either the passage was dropped from the
		// transcription or the citation is on the wrong document, and both are
		// invisible from the fact text alone.
		absent := p.Graph.AbsentQuotes(o.root)
		ranked := p.Graph.RankPending(pending, absent, used)
		if o.asJSON {
			return emit(o, ranked)
		}
		// The routine band is COUNTED, not printed. A queue of 1,197 lines is not
		// a queue, and printing it in priority order still leaves 1,197 lines —
		// the ordering only helps if the tail stops competing for the screen.
		shown := ranked
		if !attestAll {
			shown = shown[:0]
			for _, r := range ranked {
				if r.Band <= kgraph.Urgent {
					shown = append(shown, r)
				}
			}
		}
		lastBand := -1
		for _, r := range shown {
			if r.Band != lastBand {
				fmt.Printf("\n── %s\n", kgraph.BandName(r.Band))
				lastBand = r.Band
			}
			a := r.Attestation
			who := a.By
			switch {
			case who == "" && a.Verdict == "":
				who = "never checked"
			case who == "":
				who = "unsigned"
			default:
				who += " (machine)"
			}
			if u := used[a.Fact]; len(u) > 0 {
				who += "  → " + strings.Join(u, ", ")
			}
			mark := "  "
			if absent[a.Fact+"\x00"+a.Source] {
				mark = "! "
			}
			fmt.Printf("%s%-38s %-26s %s\n", mark, a.Fact, a.Source, who)
		}
		byBand := map[int]int{}
		for _, r := range ranked {
			byBand[r.Band]++
		}
		fmt.Printf("\n%d citation(s) awaiting your sign-off", len(ranked))
		if n := len(ranked) - len(shown); n > 0 && !attestAll {
			fmt.Printf(" — %d where a wrong verdict changes what the corpus CONCLUDES, "+
				"%d ranked behind them (--all)", len(shown), n)
		}
		fmt.Println(".")
		for b := 0; b <= kgraph.BandRoutine; b++ {
			if byBand[b] > 0 {
				fmt.Printf("  %5d  %s\n", byBand[b], kgraph.BandName(b))
			}
		}
		if byBand[kgraph.BandAbsentQuote] > 0 {
			fmt.Println("\nStart at the top: `kg quotes` says which words are missing, and whether " +
				"the document or the citation is wrong.")
		}
		if len(pending) > 0 {
			fmt.Println("`kg attest <fact> <source> confirmed|affirmed|corrected|unsupported|unclear --by <you>`")
		}
		return nil
	}
	if len(args) == 0 {
		att, err := kgraph.ReadAttestations(o.root, dir)
		if err != nil {
			return err
		}
		if o.asJSON {
			return emit(o, att)
		}
		if len(att) == 0 {
			fmt.Println("no citations verified yet.")
			fmt.Println("`kg attest <fact> <source> confirmed|affirmed|corrected|unsupported|unclear --by <you> [note]`")
			return nil
		}
		counts := map[kgraph.Verdict]int{}
		for _, a := range att {
			counts[a.Verdict]++
		}
		for _, a := range att.Unsupported() {
			who := a.By
			if a.MachineAttested() {
				who += " (machine)"
			}
			fmt.Printf("unsupported  %-34s cited to %-22s by %s\n", a.Fact, a.Source, who)
			if a.Note != "" {
				fmt.Printf("             %s\n", a.Note)
			}
		}
		// Counted on the CONVERGED words only. A log carrying the retired spellings
		// still resolves — ReadAttestations normalises — so tallying the old
		// constants here would report zero for a corpus full of rulings.
		//
		// `affirmed` is reported beside `confirmed` and never folded into it: a low
		// confirmed count next to a high affirmed one is the normal shape of a
		// careful pass, and adding them together destroys the only signal that says
		// which claims were load-bearing enough to be checked twice.
		fmt.Printf("%d confirmed · %d affirmed · %d corrected · %d unsupported · %d unclear\n",
			counts[kgraph.VConfirmed], counts[kgraph.VAffirmed], counts[kgraph.VCorrected],
			counts[kgraph.VUnsupported], counts[kgraph.VUnclear])
		return nil
	}
	if len(args) < 3 {
		return fmt.Errorf("usage: kg attest <fact> <source> <confirmed|affirmed|corrected|unsupported|unclear> [--by <you>] [note]")
	}
	fact, src, v := args[0], args[1], kgraph.Verdict(args[2])
	// An attestation about a fact or a source that does not exist is a typo, and
	// silently recording it would produce a verdict nothing ever reads.
	if _, ok := p.Graph.Lookup(fact); !ok {
		return fmt.Errorf("no such fact %q", fact)
	}
	if n, ok := p.Graph.Lookup(src); !ok || n.Kind != kgraph.KSource {
		return fmt.Errorf("no such source %q", src)
	}
	// Ask once, then keep using the answer — oidio's name prompt, without a second
	// place for identity to live. Optional: an unsigned verdict stays on the
	// human checklist, which is the safe default.
	by := o.by
	if by == "" {
		by = kgraph.LastHumanSigner(o.root, dir)
	}
	a := kgraph.Attestation{Fact: fact, Source: src, Verdict: v, At: o.now, By: by,
		Note: strings.Join(args[3:], " ")}
	// THE WORDS THIS VERDICT IS ABOUT, recorded with it. Without them the verdict
	// outlives its passage and nothing can tell — a re-OCR of the same scan
	// changes the text and not the file, so `HashDoc` sees nothing. See
	// Attestation.Excerpt and `kg excerpts`.
	if n, ok := p.Graph.Lookup(fact); ok {
		if ex := kgraph.ExcerptOf(n.Body); ex != "" {
			a.Excerpt, a.ExcerptHash = ex, kgraph.HashExcerpt(ex)
		}
	}
	if err := kgraph.AppendAttestation(o.root, dir, a); err != nil {
		return err
	}
	fmt.Printf("recorded %s by %s: %s cited to %s\n", v, by, fact, src)
	if v == kgraph.VUnsupported {
		fmt.Println("the fact is kept — a miscitation is not a refutation — but it now rests on")
		fmt.Println("one fewer source.")
	}
	return nil
}

// cmdVerify serves the attestation workbench. Separate from `kg daemon` on
// purpose: the daemon is a singleton across every project behind a token gate,
// and this is a focused per-index session a person opens, works and closes.
func cmdVerify(o opts, args []string) error {
	addr := "0.0.0.0:7391"
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--addr" || args[i] == "--listen" {
			addr = args[i+1]
		}
	}
	if !strings.Contains(addr, ":") {
		addr += ":7391" // `--listen 192.168.1.50` should just work
	}
	by := o.by
	if by == "" {
		p, err := load(o)
		if err == nil {
			by = kgraph.LastHumanSigner(o.root, kgraph.IndexDirOf(p.Graph))
		}
	}
	ref, _ := storeRefOf(o)
	return kgraph.ServeVerify(o.root, o.index, addr, by, ref)
}

// cmdHave answers "do we already have this?" before someone goes and gets it
// again. Prints the strongest match per document, and says which tier matched —
// a hit found only in transcript text means the FILENAME is wrong, which is a
// second thing to fix and the reason the tier is reported rather than hidden.
func cmdHave(o opts, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("kg have <identifier> — an AF number, a certification number, a form number, or a name")
	}
	p, _, err := loadIn(o)
	if err != nil {
		return err
	}
	held, err := kgraph.Have(o.root, p.Index, p.Graph, strings.Join(args, " "))
	if err != nil {
		return err
	}
	if o.asJSON {
		return emit(o, held)
	}
	if len(held) == 0 {
		// Said plainly, because the alternative is someone reading silence as a
		// filesystem error and searching by hand anyway.
		fmt.Printf("NOT HELD — nothing in this index matches %q\n", strings.Join(args, " "))
		return nil
	}
	for _, h := range held {
		where := h.Doc
		if where == "" {
			where = "(no document path)"
		}
		// Declared and held are different facts. A `doc:` naming a file that is
		// not there is exactly what an "obtain it" question is for.
		label := "HELD "
		if h.Missing {
			label = "CITED"
		}
		fmt.Printf("%s %s\n", label, where)
		if h.Missing {
			fmt.Printf("      ^ declared, but this file is NOT on disk\n")
		}
		if h.Anchor != "" {
			fmt.Printf("      at %s\n", h.Anchor)
		}
		// The term count is the reason one result is above another, so it is
		// printed rather than left implicit in the ordering.
		on := "matched on " + h.Via
		if h.Via == "terms" {
			on = fmt.Sprintf("%s (%d words)", on, h.Terms)
		}
		switch {
		case h.Source != "":
			fmt.Printf("      source %s · %s\n", h.Source, on)
		default:
			fmt.Printf("      no source cites it · %s\n", on)
		}
		switch h.Via {
		case "text":
			fmt.Printf("      ^ the filename does not carry this identifier\n")
		case "terms":
			fmt.Printf("      ^ MATCHED ON WORDS, not on an identifier — this document " +
				"discusses the same subject and is not evidence that we hold the thing asked for\n")
		}
		if h.Excerpt != "" {
			fmt.Printf("      %s\n", h.Excerpt)
		}
		// AFTER the excerpt, because the reader has just read the words and this
		// is what they need before treating them as the record. The tier note
		// above says how the document was FOUND; this says who wrote the text
		// they are looking at — and on this corpus the top hit for a survey
		// query is a sheet 88% of which is a model's account of a drawing.
		if h.Caveat != "" {
			fmt.Printf("      ! %s\n", h.Caveat)
		}
	}
	return nil
}

// firstLineOf is a source's title without its body.
func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// cmdQuotes reports whether each fact's quoted text is in the document it cites.
//
// Prints nothing that could be mistaken for a verdict. `present` means the words
// are there, not that the fact read them correctly — only `kg attest` records
// that, and only a person can.
func cmdQuotes(o opts, args []string) error {
	p, err := load(o)
	if err != nil {
		return err
	}
	checks := p.Graph.CheckQuotes(o.root)
	only := ""
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			only = strings.TrimPrefix(a, "--")
		}
	}
	if only != "" {
		keep := checks[:0]
		for _, c := range checks {
			if c.State == only {
				keep = append(keep, c)
			}
		}
		checks = keep
	}
	if o.asJSON {
		return emit(o, checks)
	}
	var absent, unreadable, present, near int
	for _, c := range checks {
		switch c.State {
		case "absent":
			absent++
		case "near":
			near++
		case "unreadable":
			unreadable++
		default:
			present++
		}
	}
	state := ""
	for _, c := range checks {
		if c.State != state {
			state = c.State
			fmt.Printf("\n=== %s ===\n", state)
		}
		fmt.Printf("  %s  ← %s\n", c.Fact, c.Source)
		if c.Doc != "" {
			fmt.Printf("      %s\n", c.Doc)
		}
		q := c.Quote
		if len(q) > 140 {
			q = q[:140] + "…"
		}
		fmt.Printf("      %q\n", q)
		// Where to look. A present quote a person cannot find is barely better
		// than an absent one.
		if loc := c.Locator(); loc != "" {
			fmt.Printf("      %s of %s\n", loc, c.Transcript)
		}
		// Absent from its own document, but the words are in the index. The
		// commonest live cause of an absent quote is a citation on a sibling file,
		// and finding that is a search the corpus can do itself.
		// A wrong quotation, not a missing one — show WHAT differs, so the fix is a
		// one-line comparison rather than a document to go and read.
		if c.Near != "" {
			fmt.Printf("      the document says: %q\n", trimTo(c.Near, 140))
			if d := kgraph.QuoteDiff(c.Quote, c.Near); d != "" {
				fmt.Printf("      difference (-yours +document): %s\n", d)
			}
		}
		// THE COUNT DECIDES WHAT THIS MEANS. One home is a candidate; many is a
		// recited passage, and telling somebody to "cite that" then points at the
		// alphabetically first of twenty-two documents.
		switch {
		case c.FoundCount == 1:
			fmt.Printf("      → these words have exactly one home in the index: %s (%s)\n"+
				"        worth checking whether the fact rests on that instrument\n",
				c.FoundIn, c.FoundDoc)
		case c.FoundCount > 1:
			fmt.Printf("      → these words are in %d of the index's documents (e.g. %s)\n"+
				"        RECITED language — containment cannot say which instrument the fact\n"+
				"        rests on, so read this as \"the words exist\", not as a correction\n",
				c.FoundCount, c.FoundIn)
		}
	}
	located, recited := 0, 0
	for _, c := range checks {
		if c.State != "absent" || c.FoundIn == "" {
			continue
		}
		if c.FoundCount == 1 {
			located++
		} else {
			recited++
		}
	}
	fmt.Printf("\n%d quoted span(s): %d present · %d ABSENT · %d near · %d unreadable (no transcription)\n",
		len(checks), present, absent, near, unreadable)
	// NEITHER OF THESE IS AN INSTRUCTION, and both used to be.
	//
	// "correct the quotation to what the document says" is wrong more often than
	// it is right: on the live corpus most near misses are transcription
	// defects — `rotted` read as `potted`, `asbuilt` as `asphalt` — so the fact
	// is accurate and the document's OCR is not. Following it edits a correct
	// quotation to match a machine's misreading, in a corpus that feeds filings.
	if near > 0 {
		fmt.Printf("%d are NEAR: the document says ALMOST this. Either the quotation is wrong "+
			"or the transcription is — read the diff before deciding, and expect OCR to be "+
			"the culprit as often as the quote.\n", near)
	}
	if located > 0 {
		fmt.Printf("%d of the absent have their words in EXACTLY ONE other document — the "+
			"strongest signal here, and worth opening.\n", located)
	}
	if recited > 0 {
		fmt.Printf("%d more are RECITED language, present in several documents at once. "+
			"Containment cannot say which instrument a fact rests on: a legal description "+
			"lives in the deed, the survey, the commitment and every filing that quotes "+
			"them.\n", recited)
	}
	if absent > 0 {
		fmt.Println("An absent quote means the transcription dropped the passage OR the citation is on the wrong document.")
	}
	return nil
}

// cmdAsk is the standing-query surface: register a question, and ask whether
// its answer is still the answer.
//
// One command rather than a verb per state, because the states are not separate
// workflows — you ask, you look at what moved, you accept it. Splitting that
// into `kg standing list` / `kg standing check` / `kg standing diff` would make
// the common case three commands and hide the drift behind the one nobody runs.
func cmdAsk(o opts, args []string) error {
	p, err := load(o)
	if err != nil {
		return err
	}
	dir := kgraph.IndexDirOf(p.Graph)
	set, err := kgraph.ReadStanding(o.root, dir)
	if err != nil {
		return err
	}
	e := env(o, p)

	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "add":
		if len(args) < 3 {
			return fmt.Errorf("usage: kg ask add <name> '<dsl>'")
		}
		name, text := args[1], strings.Join(args[2:], " ")
		q, perr := kgraph.ParseQuery(text)
		if perr != nil {
			return perr
		}
		// Validated at REGISTRATION, not at the first ask. A standing query that
		// does not validate resolves to nothing and reports a stable empty answer
		// forever, which reads exactly like a settled question.
		if errs := kgraph.ValidateQueryIn(p.Graph.Dialect(), q); len(errs) > 0 {
			for _, verr := range errs {
				fmt.Fprintln(os.Stderr, "kg:", verr)
			}
			return fmt.Errorf("query does not validate")
		}
		if prev, dup := set[name]; dup && prev.Query != text {
			// Replacing the text is legitimate — it is how a question gets sharpened
			// — but the answer on record was taken under the OLD one, so it stops
			// being a baseline. Kept, not dropped: the next ask reports
			// `query-changed` and shows what moved, which is the useful thing.
			fmt.Printf("replacing the query for %q; the recorded answer stays until you ack\n", name)
		}
		next := &kgraph.Standing{Name: name, Query: text}
		if prev, ok := set[name]; ok {
			// The old answer survives a re-registration, and the old QueryHash with
			// it. That pairing is what lets the next ask say `query-changed` rather
			// than silently treating a rewritten question as a moved corpus.
			next.Pin, next.QueryHash, next.At, next.By = prev.Pin, prev.QueryHash, prev.At, prev.By
		}
		set[name] = next
		if werr := kgraph.WriteStanding(o.root, dir, set); werr != nil {
			return werr
		}
		fmt.Printf("asked %s — `kg ask %s` to see the answer, `kg ask ack %s` to accept it\n",
			name, name, name)
		return nil

	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: kg ask rm <name>")
		}
		if _, ok := set[args[1]]; !ok {
			return fmt.Errorf("no standing query %q", args[1])
		}
		delete(set, args[1])
		return kgraph.WriteStanding(o.root, dir, set)

	case "ack":
		if len(args) != 2 {
			return fmt.Errorf("usage: kg ask ack <name>")
		}
		st, ok := set[args[1]]
		if !ok {
			return fmt.Errorf("no standing query %q", args[1])
		}
		// SIGNED, like every other write here. An acknowledgement says a person
		// looked at what moved and accepted it as the answer; an unsigned one says
		// nobody did, and the next reader cannot tell the two apart.
		if o.by == "" {
			return fmt.Errorf("who is accepting this answer? pass --by <you> — an "+
				"acknowledgement is a record that somebody read the delta on %q and "+
				"accepted it, and it is the baseline every later change is measured against",
				st.Name)
		}
		a, aerr := p.Graph.Ask(st, e)
		if aerr != nil {
			return aerr
		}
		st.Acknowledge(a, o.now, o.by)
		if werr := kgraph.WriteStanding(o.root, dir, set); werr != nil {
			return werr
		}
		fmt.Printf("%s: %d row(s) accepted as the answer\n", st.Name, a.Pin.Count)
		return nil
	}

	// No subcommand: ask one, or all of them.
	var names []string
	switch {
	case sub != "":
		if _, ok := set[sub]; !ok {
			return fmt.Errorf("no standing query %q — `kg ask add %s '<dsl>'` registers one", sub, sub)
		}
		names = []string{sub}
	default:
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		fmt.Println("nothing asked yet. `kg ask add <name> '<dsl>'`")
		return nil
	}
	var answers []*kgraph.Answer
	for _, n := range names {
		a, aerr := p.Graph.Ask(set[n], e)
		if aerr != nil {
			return aerr
		}
		answers = append(answers, a)
	}
	// THE PROJECTION IS WRITTEN HERE AND NOWHERE ELSE. `kg ask` is the one place
	// that has both the declarations and a resolved answer in hand, which is the
	// same reason `kg attach` was the only place citations could resolve. Only
	// written when every query was asked — a partial ask would silently retire
	// the rows it did not visit, since the write is a whole-index replace.
	if sub == "" && len(args) == 0 {
		if werr := syncQueryProjection(o, set, answers); werr != nil {
			fmt.Fprintf(os.Stderr, "kg: the standing-query projection was not updated: %v\n", werr)
		}
	}
	if o.asJSON {
		return emit(o, answers)
	}
	stale := 0
	for _, a := range answers {
		state := "unchanged"
		if a.Stale() {
			state = strings.Join(a.Drift, " · ")
			stale++
		}
		fmt.Printf("%-28s %3d row(s)  %s\n", a.Name, a.Pin.Count, state)
		if len(names) == 1 || a.Stale() {
			for _, line := range a.Delta.Statements(p.Graph) {
				fmt.Printf("    %s\n", line)
			}
		}
		if len(names) == 1 {
			for _, id := range a.Rows {
				fmt.Printf("    %s\n", id)
			}
		}
	}
	if stale > 0 {
		fmt.Printf("\n%d of %d need looking at. `kg ask ack <name>` accepts an answer as the new baseline.\n",
			stale, len(answers))
	}
	return nil
}

// cmdMigrate turns an index's `*.kfacts.md` into the assertion log.
//
// It does NOT delete the fact files. The parser is the last thing to go
// (`plan/facts.md`): a one-way door needs holding open until everything is
// through, and a migration you cannot check against its source is one you have
// to trust rather than verify.
func cmdMigrate(o opts, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: kg migrate [--by <you>] — migrates the current index")
	}
	// DELIBERATELY NOT `load`. An unmigrated index reports its fact files as an
	// error — they are no longer read, and saying so is the whole point — so
	// migration cannot require the very load it exists to make possible. It needs
	// the fact files and a signer, both of which it can get without a graph.
	by := o.by
	if by == "" {
		by = kgraph.LastHumanSigner(o.root, o.index)
	}
	if by == "" {
		return fmt.Errorf("who is migrating? pass --by <you> — every assertion records who made it")
	}
	st, serr := openStore(o)
	if serr != nil {
		return serr
	}
	defer st.Close()
	res, err := kgraph.MigrateFacts(st, o.root, o.index, by, o.now)
	if err != nil {
		return err
	}
	if o.asJSON {
		return emit(o, res)
	}
	if res.Count == 0 {
		fmt.Println("nothing to migrate — this index has no fact files.")
		return nil
	}
	fmt.Printf("%d assertion(s) from %d file(s), signed %s.\n", res.Count, len(res.Files), by)
	// The prose is the one thing this cannot carry, so it is a number rather than
	// a silence. Attaching a file's explanation to an arbitrary entry in it would
	// invent an attribution; dropping it quietly would lose the most valuable
	// thing in the file.
	total := 0
	for _, n := range res.Prose {
		total += n
	}
	if total > 0 {
		fmt.Printf("\n%d line(s) of prose around those blocks were NOT carried:\n", total)
		names := make([]string, 0, len(res.Prose))
		for f := range res.Prose {
			names = append(names, f)
		}
		sort.Strings(names)
		for _, f := range names {
			fmt.Printf("  %-52s %d line(s)\n", f, res.Prose[f])
		}
		fmt.Println("\nIt belongs to a FILE rather than to any one entry, so moving it is a")
		fmt.Println("decision per file. `note:` on an assertion is where it goes.")
	}
	fmt.Println("\nThe fact files are untouched. Compare before deleting anything.")
	return nil
}

// cmdWrite is every write op: assert, withdraw, correct, answer, retire.
//
// ONE FUNCTION because they share everything that matters — who is writing, the
// fields on stdin, and the report afterwards. Splitting them would put five
// copies of the signer rule in the file and one of them would drift.
func cmdWrite(o opts, op string, args []string) error {
	p, err := load(o)
	if err != nil {
		return err
	}
	by := o.by
	if by == "" {
		by = kgraph.LastHumanSigner(o.root, kgraph.IndexDirOf(p.Graph))
	}
	if by == "" {
		return fmt.Errorf("who is asserting? pass --by <you> — every assertion records who made it, " +
			"and an unsigned record says nobody did")
	}
	dl := p.Graph.Dialect()
	st, serr := openStore(o)
	if serr != nil {
		return serr
	}
	defer st.Close()

	// Fields arrive as a YAML fragment on stdin, in the same vocabulary a fact
	// file uses. Not a pile of --flags: the vocabulary is open — a dialect adds
	// relations and subtype keys — so a flag per field would be a second, always
	// incomplete spelling of the format.
	fields := func() (map[string]any, error) {
		b, rerr := io.ReadAll(os.Stdin)
		if rerr != nil {
			return nil, rerr
		}
		if len(strings.TrimSpace(string(b))) == 0 {
			return nil, fmt.Errorf("no fields on stdin — pipe the entry's YAML in, e.g.\n" +
				"  printf 'claim: ...\\nstatus: asserted\\n' | kg assert c-one --by you")
		}
		var m map[string]any
		if uerr := yaml.Unmarshal(b, &m); uerr != nil {
			return nil, fmt.Errorf("stdin is not YAML: %w", uerr)
		}
		delete(m, "id") // the id is the argument; two places to say it is one too many
		return m, nil
	}

	var res *kgraph.WriteResult
	switch op {
	case "assert":
		if len(args) != 1 {
			return fmt.Errorf("usage: kg assert <id> --by <you>   (fields as YAML on stdin)")
		}
		f, ferr := fields()
		if ferr != nil {
			return ferr
		}
		res, err = kgraph.Assert(st, dl, args[0], f, by, o.note)
	case "withdraw":
		if len(args) < 2 {
			return fmt.Errorf("usage: kg withdraw <id> <reason> --by <you>\n" +
				"the reason is required: `withdrawn` on its own is a shrug, and somebody\n" +
				"else has to be able to check the correction")
		}
		res, err = kgraph.Withdraw(st, dl, args[0], strings.Join(args[1:], " "), by, o.note)
	case "correct":
		if len(args) < 3 {
			return fmt.Errorf("usage: kg correct <old-id> <new-id> <reason> --by <you>   " +
				"(the replacement's fields as YAML on stdin)")
		}
		f, ferr := fields()
		if ferr != nil {
			return ferr
		}
		res, err = kgraph.Correct(st, dl, args[0], args[1], f,
			strings.Join(args[2:], " "), by, o.note)
	case "answer":
		if len(args) != 2 {
			return fmt.Errorf("usage: kg answer <question-id> <new-id> --by <you>   " +
				"(the answering fact's fields as YAML on stdin)")
		}
		f, ferr := fields()
		if ferr != nil {
			return ferr
		}
		res, err = kgraph.AnswerQuestion(st, dl, args[0], args[1], f, by, o.note)
	case "retire":
		if len(args) != 2 {
			return fmt.Errorf("usage: kg retire <id> <into-id> --by <you>")
		}
		res, err = kgraph.Retire(st, dl, args[0], args[1], by, o.note)
	}
	if err != nil {
		return err
	}
	if o.asJSON {
		return emit(o, res)
	}
	for _, a := range res.Applied {
		fmt.Printf("%s %s, signed %s\n", a.Op, a.ID, a.By)
	}
	// Warnings do not refuse the write, but they are told to the person who just
	// made it — that is the moment they can still act on them.
	if len(res.Warnings) > 0 {
		fmt.Printf("\n%d warning(s) on the graph this produced:\n", len(res.Warnings))
		for i, d := range res.Warnings {
			if i == 8 {
				fmt.Printf("  … and %d more — `kg scan` for all of them\n", len(res.Warnings)-i)
				break
			}
			fmt.Printf("  %s\n", d)
		}
	}
	return nil
}

// storeRefOf resolves WHERE this invocation's store is, and whether anybody
// said so.
//
// `--store` beats `$KGRAPH_STORE` beats `~/.kgraph/<index>.db`. The second
// return is what distinguishes "the default policy" from "a location somebody
// named": the default path is resolved lazily inside the library, so an
// unnamed store keeps the exact behaviour every existing caller has.
func storeRefOf(o opts) (kgraph.StoreRef, bool) {
	if o.store != "" {
		return kgraph.StoreRef{DSN: o.store}, true
	}
	if v := os.Getenv("KGRAPH_STORE"); v != "" {
		return kgraph.StoreRef{DSN: v}, true
	}
	return kgraph.DefaultStoreRef(o.index), false
}

// syncQueryProjection mirrors the standing queries into `kgraph_query`.
//
// FAILS SOFT, and that is deliberate: the projection is a convenience for
// consumers and `standing.yaml` plus the fold remain the truth, so a store that
// cannot hold it — one opened with `--store none`, or an engine that does not
// implement the optional half — must not fail the ask. It says so on stderr
// rather than silently skipping, because a consumer reading a stale table has no
// way to tell.
func syncQueryProjection(o opts, set kgraph.StandingSet, answers []*kgraph.Answer) error {
	ref, _ := storeRefOf(o)
	if ref.IsNone() {
		return nil
	}
	st, err := kgraph.OpenStoreRef(ref)
	if err != nil || st == nil {
		return err
	}
	defer st.Close()
	w, ok := st.(kgraph.QueryWriter)
	if !ok {
		return nil
	}
	byName := map[string]*kgraph.Answer{}
	for _, a := range answers {
		byName[a.Name] = a
	}
	rows := make([]kgraph.StandingRow, 0, len(set))
	for name, q := range set {
		r := kgraph.StandingRow{
			Name: name, Group: q.Group, Purpose: q.Purpose, Query: q.Query,
			QueryHash: q.QueryHash, DialectHash: q.DialectHash, At: q.At, By: q.By,
		}
		if q.Pin != nil {
			r.SetHash, r.Count = q.Pin.SetHash, q.Pin.Count
			if b, merr := json.Marshal(q.Pin); merr == nil {
				r.Pin = string(b)
			}
		}
		if a := byName[name]; a != nil {
			r.Drift, r.Invalidated = a.Drift, a.Stale()
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return w.WriteQueries(rows, time.Now().UTC().Format(time.RFC3339))
}

// cmdAmend changes ONE field of a fact and carries the rest forward.
//
// The library has had `Amend` since the repair paths needed it, and no door to
// it: `kg assert` REPLACES, so correcting one word of a claim meant retyping
// every field it had and losing whichever one you forgot. `kg correct` is the
// wrong shape — that supersedes a fact because it was WRONG, withdrawing the
// old one; a misquotation is the same fact with a transcription error in it, and
// withdrawing it would flag every document resting on it as retracted.
//
// The value comes from stdin, because the thing most worth amending is a claim's
// body and those carry quotation marks, apostrophes and dashes that a shell eats.
func cmdAmend(o opts, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: kg amend <id> <field> --by <you>   (the new value on stdin)")
	}
	if o.by == "" {
		return fmt.Errorf("who is amending? pass --by <you> — every assertion records who " +
			"made it, and an amendment is an assertion")
	}
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	val := strings.TrimRight(string(body), "\n")
	if strings.TrimSpace(val) == "" {
		return fmt.Errorf("no value on stdin — to DROP a field, `kg amend %s %s` with the "+
			"literal word `null`", args[0], args[1])
	}
	p, err := load(o)
	if err != nil {
		return err
	}
	st, serr := openStore(o)
	if serr != nil {
		return serr
	}
	defer st.Close()
	change := amendValue(val)

	res, err := kgraph.Amend(st, p.Graph.Dialect(), args[0],
		map[string]any{args[1]: change}, o.by, o.note)
	if err != nil {
		return err
	}
	for _, a := range res.Applied {
		fmt.Printf("%s %s.%s, signed %s\n", a.Op, a.ID, args[1], a.By)
	}
	return nil
}

// cmdAccept records that a person has read a warning and ruled it tolerable.
//
// BY KEY, not by line number or by message. A key is a content hash of the check
// and the facts it names, so an acceptance survives an unrelated edit and does
// NOT survive an edit to those facts — the finding comes back on its own, which
// is the whole reason this is safe to have.
func cmdAccept(o opts, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: kg accept <key>... --by <you> [--note <why>]   " +
			"(keys come from `kg scan --json`)")
	}
	if o.by == "" {
		return fmt.Errorf("who is accepting? pass --by <you> — an acceptance says a PERSON " +
			"looked at this and ruled it tolerable, and an unsigned one says nobody did")
	}
	p, diags, err := loadIn(o)
	if err != nil {
		return err
	}
	byKey := map[string]kgraph.Diag{}
	for _, d := range diags {
		if d.Key != "" {
			byKey[d.Key] = d
		}
	}
	dir := kgraph.IndexDirOf(p.Graph)
	for _, key := range args {
		d, ok := byKey[key]
		if !ok {
			// REFUSED rather than recorded. Accepting a key nothing produced is
			// either a typo or a finding that has already moved, and writing it
			// would leave a ruling in the ledger that suppresses nothing and that
			// nobody can trace back to anything.
			return fmt.Errorf("no outstanding finding with key %q — `kg scan --json` lists them; "+
				"if the fact changed, the finding has a new key and wants looking at again", key)
		}
		if d.Severity != kgraph.SevWarn {
			return fmt.Errorf("%s is an error, not a warning — errors are not acceptable, "+
				"they get fixed: %s", key, d.Msg)
		}
		if err := kgraph.AppendAccepted(o.root, dir, kgraph.Accepted{
			Key: key, Check: d.Check, Msg: d.Msg, By: o.by, At: o.now, Reason: o.note,
		}); err != nil {
			return err
		}
		fmt.Printf("accepted %s (%s), signed %s\n", key[:12], d.Check, o.by)
	}
	return nil
}

// countIDs totals the nodes across clusters.
func countIDs(cs []kgraph.DupCluster) int {
	n := 0
	for _, c := range cs {
		n += len(c.IDs)
	}
	return n
}

// dropFlag removes a flag from a positional argument list.
func dropFlag(args []string, flag string) []string {
	out := args[:0:0]
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}

// openStore opens the store this invocation writes to.
//
// The default database is NOT in the corpus — `~/.kgraph/<index>.db`, outside
// any synced tree. See StorePath: `~/life` syncs over Syncthing, and a live
// SQLite file there is a corruption hazard whether or not two machines write it.
// That is a POLICY, and `--store` is how a caller accepts a different one
// knowingly.
func openStore(o opts) (kgraph.Store, error) {
	ref, _ := storeRefOf(o)
	st, err := kgraph.OpenStoreRef(ref)
	if err != nil {
		return nil, err
	}
	if st == nil {
		// REFUSED, AND IT SAYS WHY IT REFUSED. A write that silently went to a
		// default database while the operator believed they were working from a log
		// is the failure worth spending an error message on.
		return nil, fmt.Errorf("no store: --store %s names the absence of one, so this index "+
			"loads from its exported log and any command that needs a database is refused. "+
			"Name one with --store <path> or $KGRAPH_STORE, or drop the flag for %s",
			kgraph.StoreNone, kgraph.DefaultStoreRef(o.index).Display())
	}
	return st, nil
}

// loadIn and scanFactsIn are the read path through whichever store this
// invocation names. Unnamed goes through the bare library entry points, so the
// default policy stays in one place.
func loadIn(o opts) (*kgraph.Project, []kgraph.Diag, error) {
	ref, named := storeRefOf(o)
	if !named {
		return kgraph.LoadIn(o.root, o.index)
	}
	st, err := kgraph.OpenStoreRef(ref)
	if err != nil {
		return nil, nil, err
	}
	if st != nil {
		defer st.Close()
	}
	return kgraph.LoadInWith(o.root, o.index, st)
}

func scanFactsIn(o opts) (*kgraph.Graph, []kgraph.Diag, error) {
	ref, named := storeRefOf(o)
	if !named {
		return kgraph.ScanFactsIn(o.root, o.index)
	}
	st, err := kgraph.OpenStoreRef(ref)
	if err != nil {
		return nil, nil, err
	}
	if st != nil {
		defer st.Close()
	}
	return kgraph.ScanFactsInWith(o.root, o.index, st)
}

// cmdExport writes the store out as JSONL, and cmdImport replays one back in.
//
// The export is what lives in the corpus and what goes to git: an audit trail, a
// backup, and the portable form. The import exists because an export you cannot
// replay is not a backup — and the pair is what makes it safe for the database
// to be primary.
func cmdExport(o opts, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: kg export [path]")
	}
	st, err := openStore(o)
	if err != nil {
		return err
	}
	defer st.Close()
	out := "facts.jsonl"
	if len(args) == 1 {
		out = args[0]
	} else if p, lerr := load(o); lerr == nil {
		out = filepath.Join(o.root, kgraph.IndexDirOf(p.Graph), "facts.jsonl")
	}
	n, err := kgraph.ExportJSONL(st, out)
	if err != nil {
		return err
	}
	fmt.Printf("%d assertion(s) → %s\n", n, out)
	return nil
}

func cmdImport(o opts, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kg import <path.jsonl>")
	}
	st, err := openStore(o)
	if err != nil {
		return err
	}
	defer st.Close()
	// APPENDS rather than replacing, so importing into a non-empty store is a
	// merge. That is what recovering half a corpus from a backup actually looks
	// like, and a silent replace would discard whatever was asserted since.
	before, err := st.All()
	if err != nil {
		return err
	}
	n, err := kgraph.ImportJSONL(st, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("%d assertion(s) imported; the store held %d before.\n", n, len(before))
	if len(before) > 0 {
		fmt.Println("This APPENDED — a re-import of the same log double-records it.")
	}
	return nil
}

// YAML, falling back to the RAW STRING when it parses to a scalar.
//
// stdin was taken as a plain string, so the one command whose stated purpose
// is "change one field, carrying the rest forward" could not express the most
// common change a fact ever gets: adding a second citation. `attested_by` is
// a list, and setting it to a string collapsed it to one. Measured on a live
// corpus, 979 facts carry `attested_by` and NOT ONE used the mapping form or
// carried a `because` — the model has supported per-edge justification all
// along and nothing could author it.
//
// The fallback is not a nicety. Parsing scalars would silently retype them on
// a legal corpus: bare `yes` becomes a boolean, `2024-06-06` a timestamp, and
// a source id like `s-1993-quitclaim` is fine only by luck. Structure is the
// only thing a string cannot already say, so structure is the only thing the
// parse is allowed to add.
func amendValue(val string) any {
	var change any = val
	switch {
	case val == "null":
		change = nil // a nil value DROPS the field; see Amend
	default:
		var parsed any
		if yaml.Unmarshal([]byte(val), &parsed) == nil {
			switch parsed.(type) {
			case []any, map[string]any:
				change = parsed
			}
		}
	}
	return change
}

// newFlagSet registers every global flag. One place, so the guard below cannot
// drift from what is actually parsed — a flag added here is guarded by having
// been added here, rather than by somebody remembering a second list.
func newFlagSet(o *opts) *flag.FlagSet {
	fs := flag.NewFlagSet("kg", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.root, "root", "", "repo root (default: nearest ancestor with .kgraph/, else the git root)")
	fs.StringVar(&o.now, "now", time.Now().Format("2006-01-02"), "clock for @now")
	fs.BoolVar(&o.asJSON, "json", false, "machine-readable output")
	fs.StringVar(&o.token, "token", "", "render token, for attach")
	fs.StringVar(&o.addr, "addr", kgraph.DefaultAddr, "daemon address")
	fs.StringVar(&o.index, "index", "", "index to operate on (default: the one owning cwd, per .kg-index)")
	fs.StringVar(&o.by, "by", "", "who is attesting or asserting: a name, or a machine identity such as ocr-transcript")
	fs.StringVar(&o.note, "note", "", "prose about the ACT of asserting — why, what was nearly written instead")
	fs.StringVar(&o.store, "store", "", "where the assertion store is: path | sqlite:<path> | postgres://… | none (default: ~/.kgraph/<index>.db, or $KGRAPH_STORE)")
	return fs
}

// globalFlagNames are the flags that must precede the subcommand. DERIVED from
// the flag set, so a new global is guarded the moment it is registered.
//
// `addr` is excluded deliberately: `verify` and `daemon` scan for it themselves
// and their documented usage puts it after the subcommand.
func globalFlagNames(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { out[f.Name] = true })
	delete(out, "addr")
	return out
}
