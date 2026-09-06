# Retiring authored fact files

> Status: **BUILT (2026-08-30).** All seven steps done; `plan/plan.md` carries
> what is left.
> Read `plan/plan.md` for current state and `plan/format.md` for the format spec.
> Global rules in `~/CLAUDE.md`.
>
> Decision: **no hand-authored fact files.** The graph's store becomes primary
> and kgraph is its only writer. `*.kfacts.md` goes.
>
> Storage settled 2026-08-30: **SQLite primary, JSONL export, Postgres designed
> for and not built.** This INVERTS the "text authoritative, SQLite derived"
> invariant — see the first section, and fix anything still repeating the old
> one.

## The objection, and why it does not survive contact

The settled invariant was "text files authoritative, SQLite derived and
disposable", for two stated reasons: `~/life` syncs over Syncthing where a live
SQLite file is a corruption hazard, and text gives `git blame` on a contested
fact, which matters in a legal corpus.

Both reasons are about a DATABASE replacing a text file. Neither is about
authoring. The decision above does not require a database — it requires that
kgraph, rather than a person with an editor, be what writes.

**So the store stays text, stays in git, and stops being authored.**

## The store is a DATABASE; the log is its export (USER, 2026-08-30)

Superseding the section below, which argued for JSONL as the store. The argument
was right about append-only and wrong about the file, and the question that
settled it was not storage but WRITERS.

**JSONL's advantages over a table were four, and three stopped applying.**
`git blame`, human repair, file-sync survival, no binary corruption. Dropping git
history removes the first. The other two are both the same point — surviving a
FILE SYNCER — and that point only earns its keep when more than one machine
writes the same index. **It is one machine.** So JSONL was a worse database that
survives Syncthing, and SQLite is a better one that does not need to.

Note what the design had already done to the git argument on its own: **history
stopped depending on git the moment `by`/`at` went on every assertion.** The log
carries its own provenance and `note` carries the why. Dropping git costs the
diff view, not the history.

- **SQLite is primary**, for dev, test and the real corpus.
- **JSONL is the export** — audit, backup, portability, and the thing that goes
  in git if anybody wants it there. `kg export`.
- **Postgres is an upgrade option. Designed for, NOT built** (USER).
- **THE INVARIANT IS INVERTED AND THAT MUST BE SAID OUT LOUD.** "Text files
  authoritative, SQLite derived and disposable" was settled and is now false. The
  database is authoritative; the text is the export. Anything still saying the old
  thing — `CLAUDE.md`, `plan/plan.md`, `sql/schema.sql`'s own header — is wrong
  and has to be corrected in the same pass, or the next person will believe it.

### What designing for Postgres costs, and why it is nearly free

**Because the store holds the LOG, not the graph.** The dead `sql/schema.sql`
projected nodes, edges, conflicts and sources into tables — that was a derived
index, and porting THAT to two engines would be real work. The assertion store is
one append-only table, and the query evaluator stays in memory where it already
is by decision. Portability is therefore a small set of rules rather than a
layer:

- **One table, `assertion`**, columns `seq`, `op`, `id`, `fields`, `reason`,
  `into`, `note`, `by`, `at`. `fields` is TEXT holding JSON in both engines —
  never `jsonb`, never `json_extract`. Nothing queries into it in SQL, because
  the evaluator is in memory and folding is what turns the log into a graph.
- **Times are TEXT in RFC3339**, never a native timestamp type. The two engines
  disagree about timezones and the fold's ordering must not depend on which one
  is underneath.
- **No SQLite-isms**: no `AUTOINCREMENT`, no `INSERT OR REPLACE`, no `rowid`.
  `ON CONFLICT ... DO UPDATE` is the one upsert both speak.
- **Placeholders differ** (`?` vs `$1`) and that is the only genuinely engine-
  specific thing. One rebind at the driver seam, not per query.
- **`sqlc.yaml` is configured `engine: sqlite`** and would need a second config
  for Postgres. Worth knowing before it is written the first time.

### Where the database lives — and it is NOT in the synced tree

`~/life` syncs over Syncthing. **A live SQLite file there is a corruption hazard
whether or not two machines WRITE it** — partial page writes, and the `-wal`
and `-shm` files syncing out of step with the database. "Single machine" removes
the write conflict; it does not remove the syncer.

So the database lives outside the synced tree — `~/.kgraph/<index>.db` — and what
sits in `~/life` is the JSONL export. That keeps the property the old invariant
was really protecting: **what is in the corpus directory is text.** It just stops
pretending the text is the thing being written.

**AMENDED 2026-09-01: that path is the DEFAULT, not the only answer.** The
hazard above is unchanged and so is the recommendation; what changes is who
decides. A caller names its own store — see "The store is a LOCATION and an
ENGINE the caller names" in `plan/plan.md` — because keying the location on the
index NAME alone means two corpora that spell an index the same way share a
database silently, and because a consumer keeping one index per project cannot
be told by this library where its data has to live. A caller that puts a
database in a synced tree is accepting this hazard knowingly, which is a
different thing from the library not having thought about it.

## The append-only argument, unchanged

Everything below stands: append-only is right, and it is right for reasons that
have nothing to do with the file format. Read it as the argument for the SHAPE of
the store, with the medium now settled above.

## The store: an append-only assertion log

`.kgraph/facts.jsonl`, per index, one JSON object per line, written only by
kgraph.

**THIS IS NOT A COMPROMISE. IT IS A BETTER FIT THAN WHAT IT REPLACES.** `*.kfacts.md`
is a MUTABLE FILE HOLDING AN IMMUTABLE DATA MODEL, and that mismatch has been
paid for the whole time. The format already says: facts are immutable, a
correction is a new node plus `supersedes` plus a reason, the old node moves to
`withdrawn`, never edit in place. An append-only log is that model written down.
A markdown file that anyone can retype is the model with a hole in it.

Every property the objection was defending survives, and two are improved:

| | `*.kfacts.md` | `facts.jsonl` |
|---|---|---|
| in git, `git blame` works | yes | yes |
| Syncthing-safe | a merge conflict inside a YAML block | two machines appending lines is a merge a person can read |
| matches the immutability rule | no — the file is edited in place | yes — the model IS append-only |
| who may write | anyone with an editor | kgraph |
| a fact's history | whatever git happens to hold | in the store, by construction |

**The precedent is already in the codebase and was chosen for this exact
reason.** `attestations.jsonl` is append-only because "two syncing machines
rewriting one JSON object is a merge conflict and a lost verdict; two machines
appending lines is a merge a human can read". A verdict and an assertion have
the same shape of problem. `standing.yaml` is a whole-file rewrite for the
opposite and equally deliberate reason — an answer is REPLACED, not accumulated.
Facts accumulate. They get the log.

**SQLite stays derived and disposable.** Nothing about this decision touches
that, and the Syncthing hazard the plan named never arises.

## What a record is

One line per ASSERTION, not per node. A node is the fold of the assertions about
it, which is what makes the log a log rather than a rewritten file.

```jsonl
{"op":"assert","id":"c-strip-held","kind":"claim","body":"The strip is held by Halloway","status":"asserted","attested_by":["s-deed"],"by":"carl","at":"2026-08-30T11:04:22Z"}
{"op":"withdraw","id":"c-strip-held","reason":"the deed describes the adjoining parcel","by":"carl","at":"2026-09-02T09:12:00Z"}
{"op":"assert","id":"c-strip-held-2","kind":"claim","body":"The strip is held by Bramble","supersedes":"c-strip-held","by":"carl","at":"2026-09-02T09:12:01Z"}
```

- **`by` is required**, and for the reason `kg attest` already requires it: an
  unsigned record says nobody did it. The web asserter signs with the token's
  label, the CLI with `--by`.
- **`at` is the wall clock, not the fact's date.** A fact's own time is `at:` in
  its content; this is when the assertion was made, and the two are different
  questions that a single field has confused before.
- **The ops are the operations the editor already offers** (`plan/web.md`):
  `assert`, `withdraw`, `correct` (an `assert` carrying `supersedes` plus the
  matching `withdraw`), `answer`, `retire` (`same_as`). Nothing new is invented
  for the store — if the UI cannot express it, it is not an op.

## What has to hold

- **Reading is a fold, and it must be deterministic.** Last write per `(id,
  field)` wins, ordered by `at` then by line. Two machines that appended in
  different orders must fold to the same graph or the store is not a store.
- **`SemHash` is unchanged.** It covers content, incident edges and a source's
  authored provenance. It must NOT gain the assertion's `by` or `at`: who typed
  a fact and when is not what the fact SAYS, and folding it in would make every
  re-assertion of an identical fact a phantom delta — the precise failure the
  hash's own comment records.
- **The parser is the last thing to go.** `*.kfacts.md` must keep loading while
  a corpus is migrated, and `kg migrate` writes the log from the files rather
  than the other way round. A one-way door needs the door held open until
  everything is through.
- **A hand-edited log is detectable.** The store is the source of truth, so this
  is the false-fresh problem in a new place. Same answer as the managed block:
  the log carries a running hash, and a break in the chain is reported rather
  than believed. NOT a lock — a person WILL edit it to fix something, and the
  requirement is that kgraph notices, not that it refuses.

## Order of work

1. ✅ **The record type, the writer, and the fold** (`assert.go`). Additive —
   nothing loads from it yet.
   **The fold does not build nodes.** It merges the log into authored field maps
   and hands them to `ParseFactsIn` — the same parser, the same dialect, the same
   guards. A fold that constructed `Node` values would be a second implementation
   of the format, and the second implementation is the one that drifts: a dialect
   adds a relation or a subtype key and the log silently cannot carry it. That is
   also why `Assertion.Fields` is `map[string]any` rather than a struct.
   `TestFoldedLogMatchesParsedFactsBySemHash` is the proof rather than the claim,
   and it is verified non-vacuous — perturbing one id in the fold fails it.
2. ✅ **`kg migrate`** (`migrate.go`). Deletes nothing — the fact files stay so
   the migration can be checked against its source rather than trusted.
   **It never touches `Node`.** Writing each node back out as fields would be the
   parser run backwards, i.e. the same second implementation the fold refuses. It
   reads the authored YAML entries and emits them verbatim.
   **The fixture corpus caught a real bug on the first run**: decoding a block
   into `[]map[string]any` let yaml.v3 type-convert scalars, so `at: 2026-01-22`
   became a `time.Time` and re-marshalled as `2026-01-22T00:00:00Z` — a different
   date PRECISION, so 24 of 99 nodes hashed differently in a migration whose
   entire promise is that none do. Every scalar is now kept as authored TEXT,
   which is lossless by construction and safe because the parser reads `v.Value`
   rather than decoding into a typed field.
   Verified on both fixture indexes: 99 and 30 nodes, every `SemHash` identical,
   edge counts unchanged.
   **The prose is reported, not silently dropped** — 39 lines across
   fence-dispute's 7 files. It belongs to a FILE rather than to any entry in it,
   so attaching it to one would invent an attribution; `note:` is where it goes
   and moving it is a decision per file.
3. ◐ **The write ops** — `Assert`/`Withdraw`/`Correct`/`AnswerQuestion`/`Retire`
   ✅ in the library (`write.go`). A write folds the log it would join, builds the
   result, and appends only if it still builds: a UI that can write a graph the
   CLI refuses to load is a UI that corrupts a corpus. Errors refuse, warnings do
   not — a corpus carries warnings by design and blocking on them would block
   every assertion in a real matter.
   `Correct` is two lines and one act, with `supersedes` written by the op rather
   than left to the caller; forgetting it was the commonest hand-edit failure and
   leaves the corpus holding both facts with nothing saying which won.
   **next**: the CLI, then the web asserter (`plan/web.md` step 6).
4. ◐ **The SQLite store** (`store.go`). ✅ `Store` interface, `sqlStore`, one
   `assertion` table, `ExportJSONL`/`ImportJSONL`, `StorePath` at
   `~/.kgraph/<index>.db` — outside the synced tree, 0700/0600 like the token
   store and for a stronger reason: this is the PRIMARY copy of a live legal and
   medical corpus now, not a derived index that could be rebuilt.
   `TestTheExportRebuildsTheStore` is the property that makes the inversion safe:
   store → export → fresh store → identical graph by `SemHash`. A store that
   cannot be rebuilt from its text is a store whose text is decoration, and the
   text is what lives in the corpus.
   ✅ The seam is repointed: `Apply` and the five ops take a `Store` rather than
   a path, `MigrateFacts` writes into one, and `kg export` / `kg import` are the
   text half. `ReadAssertions`/`AppendAssertion` are gone — one reader,
   `ReadAssertionLog`, because two ways to read the log is the two-idioms
   problem this format refuses everywhere else, and the second one survived only
   as long as the JSONL was the store.
   Verified end to end: 99 assertions migrated out of the fence-dispute fixture,
   exported, and replayed into the store.
5. ✅ **Load from the store** (`scan.go`, `storedFacts`). A NON-EMPTY STORE WINS
   OUTRIGHT and the now-inert fact files are NAMED in a warning.
   The failure to design against is not ambiguity, it is SILENCE — somebody edits
   a `*.kfacts.md` after migrating, nothing happens, and the corpus reports
   itself clean. Merging instead would be worse twice over: `kg migrate` copies
   every id so everything would collide, and a per-id precedence rule is a thing
   somebody has to remember, which this format refuses everywhere else.
   Nil from the store means NO STORE, never an empty graph — confusing them would
   make an unmigrated corpus load as no facts at all. And scanning must not
   CREATE one: a read command that writes to `~/.kgraph` as a side effect is how
   a corpus acquires a store nobody asked for. Both pinned.
   ✅ **AMENDED 2026-09-01: an EMPTY store falls through to the export**, where it
   used to answer "no facts" and stop. Safe precisely because the table is
   append-only: a log ever written to cannot come back empty, so empty means never
   written rather than emptied, and reading the text resurrects nothing. It fixes
   a live failure rather than a hypothetical one — `OpenStore` CREATES its file,
   so the first `kg export`, `kg import` or `kg assert` in a corpus that loads
   from its exported log left an empty database behind, and every later scan then
   reported that corpus EMPTY. `TestAnEmptyStoreDoesNotHideTheExport` pins it.
   ✅ **WHERE the store is comes from the caller** (2026-09-01). `storedFacts`
   took the default path itself, which was hardcoded policy in the middle of a
   library function; it now takes a `Store` plus a flag asking for the policy,
   and only the bare entry points pass the flag. See `ScanFactsInWith` /
   `LoadInWith`, and the store-location tree in `plan/plan.md`.
6. ✅ **Delete the parser.** `ParseFacts`, `ParseFactsIn` and `Graph.WithFile`
   are gone; nothing loads a `*.kfacts.md`. `fencedBlocks` survives for specs,
   and `migrate.go` keeps its own block reading — migration is a one-way import
   and has to read the format it imports FROM.
   `examples/` is `facts.jsonl` now, migrated from its own files and committed;
   the originals are `testdata/legacy/`, which is the migration's sample rather
   than a second corpus.
   ✅ **`parseEntries` is the format; `ParseFactsIn` is the file.** The first is a
   YAML sequence of entries with every idiom check and every dialect rule; the
   second is markdown around it. `FoldAssertions` calls `parseEntries` directly
   now — it used to wrap its fields in a ```kfacts fence to reach the only entry
   point that existed, which was the tell. Deleting the markdown reader now
   touches no rule.
   ✅ **An index is discoverable and loadable by its EXPORT.** Discovery keyed off
   `*.kfacts.md` alone, so a migrated index VANISHED — it had nothing left to be
   found by. `FindFactLogs`, `Indexes` and `ScanFactsIn` now accept either, and
   `TestACorpusLoadsFromItsExportAlone` deletes the fact files and proves the
   graph is identical by `SemHash`. This also makes the export more than a
   backup: a corpus handed to somebody with no database still loads.
   Converted in batches with the suite green between each, and the conversion
   paid for itself five times — every one of these was silent and none was
   findable by reading:
   - the folded doc's path lost its directory, so every `doc:` resolved against
     the repo root instead of the index
   - `ApplyRelocations` and `AddAlias` were text edits, which the store cannot
     do; both became re-assertions, which is the better answer
   - exports folded one-per-index moved every `sources.lock` to the index root,
     undoing the per-directory placement that keeps a medical project's document
     filenames out of a shared git history
   - `checkHashComment` hung off the markdown reader, so the unquoted-`#` check
     silently stopped covering anything
   - `registry.go` counted `*.kfacts.md`, so a migrated corpus reported ZERO
     facts while remaining discoverable by its specs
7. ✅ **Correct the inverted invariant** — `CLAUDE.md`'s Conventions,
   `plan/plan.md`'s Decisions (struck through rather than deleted, because it was
   settled for reasons that STOPPED APPLYING rather than being overruled, and
   that distinction is worth keeping), `plan/format.md`'s source-lock rationale,
   and `sql/schema.sql`'s header. The last is left in place carrying a note that
   it is unused and wrong, because a schema nothing reads is exactly the kind of
   file somebody trusts.

- **risks**
  - **`examples/` is 10 `*.kfacts.md` files and is the test corpus, not
    decoration.** They migrate at step 2 and the `SemHash` equality test is what
    proves the migration rather than asserting it.
  - **`kg extract` writes facts for a person to paste.** It becomes a producer of
    `assert` ops, which is a smaller change than it sounds and removes the paste.
  - **A corpus mid-migration has two sources of truth.** Step 4's warning is not
    a nicety; without it a fact edited in the old file silently does not exist.
- ✅ **`note` on the assertion** (USER, 2026-08-30). The prose a `*.kfacts.md`
  carries in the markdown around its YAML has nowhere else to go, and it is
  routinely the most valuable thing in the file. Same shape as an attestation's
  `Note`. It is deliberately NOT `reason:` on the node: `reason:` is content and
  reaches `SemHash`, while a note is about the ACT of asserting and must not, or
  an editorial aside would flag every answer resting on the fact.
