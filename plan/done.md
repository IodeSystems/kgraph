# Done — completed work, archived

Moved here as each tree finished. `plan/plan.md` holds only current state and
active work; this is the record of how it got there. Design rationale lives in
`plan/format.md`.

Everything below was complete and green as of 2026-07-26:
**372 nodes · 467 edges · 11 specs · 0 errors**, `go vet` and `go test -race` clean.

### ✅ Parser: `*.kfacts.md`
`node.go` (types, `SemHash`, `CID`) · `kfacts.go` (block extraction, YAML → nodes
+ edges, idiom enforcement) · `graph.go` (assembly, cross-file checks) ·
`scan.go` (glob discovery). Clean across both fixtures; `go vet` clean.

Enforced at parse: inverse keys (`attested_by`, `members`, `options`, …) stored
in **canonical direction** so one relation has one `sem_hash`; inverted idioms
rejected; authored `disputed` rejected; date precision inferred from granularity;
the managed region never parsed back in. Enforced at build: dangling refs,
non-source `attests`, unreferenced sources, empty groups, option exclusivity,
**unplanned branches**.

- **risks**: the `SemHash` field set is the whole staleness contract. Locked to
  content + incident edges, excluding file position and provenance metadata —
  tested, but only a real re-ingest proves it.

### ✅ Fixtures: two hand-built graphs
237 nodes, 270 edges, 8 doc specs, 0 diagnostics. The parser test corpus.

- `examples/fence-dispute/` (178 nodes, 6 specs) — posture · title-theory · parties &
  conflicts · timeline · financing. Reimplements the real project's key documents
  as query-bound specs: **defense-plan · timeline · conflict-check ·
  attorney-packet (6 outputs) · financing-options · settlement-posture.**
  Every fact carries a source node with an evidentiary class.
- `examples/clinic-billing/` (59 nodes, 2 specs) — strategy · timeline. Money,
  derived deadlines, parallel tracks. Its Letter C is the proof case: a
  ready-to-send document whose premise was disproven, still sitting in
  `letters-ready.md`.

Notable modelling wins, both from the real corpus:
- **Underdetermined, not disputed.** The packet dates partial SJ 2023-04-24, the
  docket 2023-04-25. Not a conflict — the ruling and the order entry are two
  events wearing one fact. Split, not weighed.
- **The structural conflict.** Northwind Title (the closing agent that dropped the
  easement) is a Galeforce company; plaintiff's counsel is Galeforce's litigation arm. Two
  sourced facts, one derived claim, feeding both the landmines section and the
  settlement-leverage argument.

### ✅ Query engine
`query.go` (lexer, parser, JSON-serializable AST) · `eval.go` (in-memory
evaluator over `*Graph`, satisfaction, computed values and dates,
`ValidateQuery`). 26 tests green, `go vet` clean.

**No SQL lowering.** At 10³–10⁴ nodes an in-memory evaluator is the correct
permanent choice, not a stopgap — CTE lowering would be work for no gain. The AST
stays the contract if that ever changes.

Implemented: kind alternation `(claim|action)`, id atoms, predicates
(`= != < > <= >= in has`, value alternation), FTS-ish `~ "text"`, hops in three
directions with `*` closure, `@now` / `@date` / `@[from,to]`, declared `sort`
with missing-sorts-last, `& | !` (complement over the universe), `@named` refs
with a recursion guard, group aggregates (`all any none >=K ==K sum(value)`)
returning `GroupStat{Total,Satisfied,Blocking,OK}`, and `= expr` resolution for
values and chained dates.

**Edge temporal qualifiers land.** Long-form edge refs parse
(`requires: [{id, valid_until, while}]`), an expired edge truncates a closure,
and a lapsed `member_of` changes a group's cardinality — which is what generated
prose commits to. Edge `while` is dangling-checked.

- **risks**: nothing outstanding that a fixture exercises.
- **deferred to v2, deliberately**: path variables and multi-binding returns
  (leftmost atom is the result), `group by`.

### ✅ Resolution check: no spec query may return zero rows
Parsing and schema validation cannot see a **reversed hop** — it resolves to
nothing and fails silently as an empty section in a generated document. Only
resolution catches it. `TestNoSpecQueryResolvesEmpty` runs all 66 spec queries
against the fixture; it caught six reversed hops and one real evaluator bug
(`now` inside a predicate was string-compared as the literal word). This check
belongs in `kg status` too, not just the test suite.

### ✅ Spec parser + managed-block contract
`spec.go` — `ParseSpec` (purpose · queries · prompt · outputs · build), `Resolve`
(a `Pin` per query: set hash, count, declared sort, ordered `id@semhash`),
`RenderToken`, `State`, and the sealed managed block.

The managed region is never authored input and never trusted on sight: it
carries a **self-hash** over a canonical payload, so a hand-edited block reports
`managed-tampered` instead of producing a false-fresh document. `spec_hash`
covers the authored region only, trailing whitespace normalized, so adding the
managed block does not itself report `spec-changed`.

- **next**: `kg diff` consumes the pins directly.

### ✅ Render / attach
`render.go` — `Render` (resolve → interpolate → prompt + token) and `Attach`
(verify → hash artifacts → seal), plus `WriteSpec` (atomic rename, authored
region untouched).

**`attach`, not `apply`.** The agent writes artifacts however it likes; kgraph
records that they came from a given resolved state. That removes the need for
any emission-format boilerplate in the prompt — no "emit fenced blocks tagged
with path" — which matters most for the 6-output attorney packet.

`Render` adds **nothing of its own**: no role preamble, no scaffolding, no
generation instructions. The prompt is the author's `## Prompt` with `{{query}}`
and `{{query.count}}` replaced by resolved facts. Facts render with status,
precision-correct dates, group satisfaction (`0/3 satisfied`), relations in
readable inverse phrasing, computed `DISPUTED: n for, m against`, and opaque
`[[source|class]]` tokens — the model never sees a file path, so it cannot
invent a citation format.

`Attach` refuses an undeclared output path, refuses a token that no longer
matches the graph, reports declared-but-unattached as `output-missing`, and
reseals.

- **next**: citation substitution (`[[source-id]]` → per-output style) still
  belongs here and is not built. `## Build` is recorded but not executed.

### ✅ `kg diff`
`diff.go` — pin-vs-current set diffing into `+ − ~` plus the set-level classes
(`cardinality`, `ordering`, `boundary`), rendered as **statements** rather than
two lists. `Clone` + `WhatIf` give a scratch overlay, so blast radius is visible
before a fact is committed.

### ✅ CLI — `cmd/kg`
`scan · status · diff [--what-if] · query · render · attach · daemon · serve ·
scopes`. `render` puts the prompt on stdout and metadata on stderr, so it pipes.
`scan` reports zero-row spec queries, which parsing and validation cannot see.

### ✅ Daemon — singleton, multi-project, branch-aware
`registry.go` + `daemon.go`. **One process serves every project**; a request
carries `dir` and the daemon resolves it to a `(root, branch)` scope, so routing
is implicit from cwd — raglit's shape. Reload is gated on a stat-sweep
fingerprint, so an unchanged scope costs a directory walk.

**Branch is part of the scope key.** The same repo on two branches is two graphs,
and a document generated on one must not read as fresh on the other. A detached
HEAD keys by sha.

Surfaces come from one huma handler set registered through **gat**: REST,
GraphQL (`/api/graphql`), gRPC (`/api/grpc`), and `/openapi.json`. Verified
end-to-end against the fixtures.

- **two upstream gotchas, both chased down**:
  - gat could not render a `map` field — fixed upstream and released as
    **gwag v1.2.1**; `map[string]string` is back on the render response and the
    descriptor now carries a proper `CurrentEntry` map entry.
  - **Parameters on an unexported field are invisible to huma.** The rule is
    *exportedness*, not embedding: huma skips `!f.IsExported()` before reading a
    tag, and an embedded field takes its name from its type — so embedding a
    lowercase `scope` drops `dir`/`now` from the OpenAPI document *and* the
    runtime binding, silently. Exporting fixes it; `Target` here is exported on
    purpose and embedded normally.

    I got this wrong twice. First as "huma ignores embedded structs" — which
    produced a real but silent bug here: REST fell back to the daemon's cwd,
    which happened to resolve to the same project, so my verification was a
    false positive. Then I shipped that wrong diagnosis upstream as a gwag check
    that rejected *working* exported embeds. Corrected in **gwag v1.3.1**;
    **v1.3.2** then fixed gat's binder, which advertised the promoted argument in
    the GraphQL/proto schema and never wrote it to the field. Both pinned by
    tests that fail without the fix, and written up in gwag's `docs/gat.md` —
    "Trap: parameters on an unexported field".

### ✅ MCP — a thin client of the daemon
`mcp.go` speaks JSON-RPC on stdio and forwards every call over HTTP. It does not
read the filesystem: **one process owns scope resolution, branch keying and the
reload cache.** Two independent readers would eventually disagree about what is
fresh, and the freshness answer is the whole product.

The shim knows only its own cwd and passes it as `dir`; the daemon resolves the
project and branch. If nothing is listening it starts a daemon (`os.Executable`
→ `kg daemon`) and waits for it, so an agent is never told to boot one.

Nine tools — `status · diff · variants · what_if · query · render · attach ·
scan · scopes` — and **nothing that writes a file**, so an agent driving kgraph
through MCP cannot write an output without going through attach. huma problem
documents are unwrapped to plain sentences (*"P74736 is ambiguous at any
time…"*), and tool errors return as content with `isError` rather than as
transport failures, so the agent reads and corrects.

Verified end-to-end over stdio: auto-start, scope listing, suffix spec matching,
render token, undeclared-path refusal, what-if blast radius, dated alias
resolution.

- **risks**: an agent with its own file access can bypass attach. That degrades
  to `output-edited` — detected, not silent. Do not add guards that make it
  silent.

### ✅ Anchored things and temporal aliases
Facts anchor to entities via `about`, so "everything we know about this parcel"
is one hop. Identifiers are `aliases:` with optional `from`/`until`, because the
same name denotes different things in different periods.

`P74736` is the corpus's own example: the pre-2021 parent parcel, then Bramble's
Lot I. The real tracker records that this exact confusion already produced a
wrong line. Now `#P74736 @2020-06-01` and `#P74736 @2022-06-01` reach different
facts, an **undated** reference to it is an error rather than a silent pick,
overlapping windows are an error, and aliases are inside `SemHash` because
changing one changes what a document's references denote. Quoted refs
(`#"1440 North Northlea Road"`) let addresses be aliases too.

### ✅ Unresolved questions: taint and variants
`taint.go`. Two distinct jobs, previously conflated:

- **`while` REMOVES** work belonging to a branch we are not in.
- **Taint KEEPS** a fact in the result and marks what it rests on. A fact can be
  fully attested and still not settled, and the document has to talk about it —
  just not as though it were decided.

Taint flows transitively over `requires`, and a group inherits its members'
holes. It deliberately does **not** flow across membership downward: being in a
group with an unsettled sibling does not make a fact conditional. (That
direction was the first implementation and it wrongly marked `drawing-error` as
resting on `q-surveyor-spend`.)

Surfaces in three places: `UNRESOLVED — conditional on: …` in the rendered
prompt; `unresolved:` in the pin, and inside `set_hash`, so **answering a
question registers as a delta even when no member of the set moved**; and as
`is now answered` / `is now open` statements in `kg diff`.

**Variants** enumerate a question's declared `options`, resolving the spec once
per branch. `kg variants` reports whether the answers *diverge* — "the answer
changes what the document says" — or merely block — "either answer yields the
same text". Only the first is a reason to wait before writing. Reuses `Clone`,
so it costs nothing new.

### ✅ Web console
`ui.html` (embedded via `go:embed`) + `ui.go`, mounted at `/` by the daemon.
Self-contained: no CDN, no build step, no second toolchain — the force layout is
thirty lines of vanilla JS for the same reason.

Query IDE with the named-query library as one-click buttons, results table, node
inspector showing **exactly what a prompt would see** (`RenderOne` calls the same
`renderNode`, so the view cannot drift from the document), click-through on
edges, plus Documents (status + delta) and Problems (scan) tabs.

Two endpoints back it. `/graph` returns the subgraph induced by a query with a
depth of neighbourhood; `/node` returns one node rendered, its edges, its open
holes, and **which documents pin it**. `Induced` is bounded and reports
truncation — a map that silently drops nodes reads as "this is all there is",
the same lie as an empty section in a generated document.

**Discoverable, not a path box.** `/projects` merges remembered roots (persisted
to `~/.kgraph/known.json` as scopes load), currently-open scopes, and a walk under
a given path — so the console offers what exists instead of requiring a path
nobody can guess. Bounded by **time as well as depth**: depth alone silently
loses a project nested one level deeper than the guess, which is exactly what
happened (the fixtures live at `examples/fence-dispute/`, one below where I counted).

**Explorable.** Pan and zoom by viewBox — wheel, drag, `+ − fit` — which composes
with the running simulation because the layout works in its own coordinate space.
The inspector groups edges by direction and type, every id is a link, and
"explore from here" re-centres the query on that node at depth 2, so the graph is
walkable rather than a dead end. It also says when a fact is pinned by **no**
document: nothing goes stale when it changes.

**Live.** `/events` is an SSE stream; the page re-runs its query and refreshes the
visible tab when the scope's files move. Without it the console keeps showing a
resolution that is no longer true — the exact failure the tool exists to prevent,
in the tool's own console.

Polled at 1s, not inotify: the fingerprint is already a cheap stat sweep, an
editor save is not latency-critical, and polling has no descriptor limits, no
missed-event races on rename, and behaves the same on the network mounts `~/life`
syncs over. One poller per scope fans out to every subscriber, so ten tabs cost
one sweep; it stops when the last one leaves. Sends are dropped rather than
blocking — the message is "something changed", and a client that missed one
learns it from the next.

**Exposure is opt-in and loud.** Default stays loopback. `kg daemon 0.0.0.0:PORT`
warns that `/attach` writes files with no auth, and prints the LAN URL.

### ✅ Claude hooks
`cmd/kg/hooks.go` + `kg hook install`. Five hooks, spec in `plan/hooks.md`.

Two blocking, three advisory. **`guard-managed`** (PreToolUse) refuses any edit
at or below the managed marker — the one corruption the system cannot self-heal,
since false-fresh looks healthy — and allows edits to the authored region of the
same file. **`lint-facts`** (PostToolUse) blocks on scan errors so a dangling ref
surfaces on the edit that caused it. **`what-if`** reconstructs the pending write
and reports blast radius before the fact lands; **`guard-output`** notes that
editing a generated doc will read as `output-edited` without refusing;
**`report-stale`** (Stop) is the end-of-turn catch for silently stale documents.

`install` is idempotent, backs up, refuses invalid JSON rather than clobbering,
preserves foreign settings, and defaults to the repo's `.claude/settings.json`
so graph hooks do not fire elsewhere.

- **design rules, both load-bearing**: only the two checks whose findings are
  always genuine errors block — an advisory hook that refuses legitimate work
  gets uninstalled. And every hook fails **open** on an internal error, except
  the one whose job is to refuse.
- **tested**: `cmd/kg/hooks_test.go`, including that every installed command is
  one the binary dispatches — a typo there yields a hook that silently does
  nothing forever.

### ✅ Source lock — versioning the evidence
`sources.go`. `.kgraph/sources.lock` records each declared document's hash; drift
surfaces as a new spec state, `source-changed`. Deliberately **not** in `sem_hash`
(re-exporting one PDF would flag every document rendering any fact it attests),
not in the `.kfacts.md` files (those are authoritative input), and not in SQLite
(derived, disposable, a Syncthing hazard). The lock is text and its diff *is* the
review event.

`not-found` is intentionally silent — a typo and an exhibit on another machine are
indistinguishable from here — and surfaces in `kg source status`, which is where it
caught two nodes citing `documents/court/`, the docket *folder*. Drift reaches
through an inference's premises. `kg source lock` is the only writer.

Found while verifying: the daemon's fingerprint stats only `*.kfacts.md` /
`*.kgraph.md` and skipped `.kgraph/`, so a changed exhibit never invalidated the
cache and `.kgraph/queries.md` never reloaded either. Also fixed a pre-existing
watcher race whose test helper had been a no-op.

### ✅ Citations resolve at attach
`[[source-id]]` → markdown footnotes plus a generated definition block carrying
body, speaker, class, document path, and a precision-aware date. Resolution happens
**before** hashing, or `status` reports `output-edited` against a file kgraph wrote.
An unknown id — or a non-source, like citing a person — fails the attach. Binary
artifacts are never rewritten. First version derived the cited set only from
`[[id]]` tokens, so a second attach deleted the citations it had written; `[^id]`
refs are now read back.

### ✅ The attach token is required
Was `token != "" && token != now` — one omitted flag defeated it, recording
`fresh` for an artifact written against a graph that had already moved. Making it
mandatory immediately caught a test that had been relying on the bypass.

### ✅ Daemon auth
Bearer token required from off-box peers; the gate is on the **peer**, not the
listen address, so binding `0.0.0.0` does not start demanding a token from the
local MCP shim. `?token=` is exchanged for an HttpOnly cookie and redirected away.
Token at `~/.kgraph/daemon-token`, 0600, never in the project. Verified live:
loopback 200, off-box 401, off-box+token 200, off-box `POST /attach` 401.

### ✅ Duplicate detection — `cid` and `same_as:`
Two ids for one fact means two `sem_hash`es: answering one leaves the other open.
`cid` is now kind + normalized body and **not** the source document — folding the
document in gave the same claim from two sources two CIDs, defeating the merge.
Exact collision is an error; high token overlap is a warning, exempting pairs
already joined by an edge (a claim and its rebuttal share their whole vocabulary by
design). `same_as:` retires an id while keeping it resolvable.

Found two real duplicates in the corpus. Bounded after the quadratic case produced
8M warnings in 28s/4GB; both bounds that lose coverage report it.

### ✅ `needs` — what would close a question
`evidence | decision | reply | analysis`, because each implies a different next
action. Partitions the open set. Two ways a question becomes permanent are now
caught: no `needs`, and neither an `owner` nor an `about`. That second check
retired `q-consult-fee`, which was per-interaction capture masquerading as a case
fact and could never have been answered.

### ✅ Extraction — the read direction
`extract.go`. Render's inverse: input is a document, output is `kfacts`, kgraph
supplies the format rules, and the document path belongs *in* the prompt.
`TestExtractRulesDoNotLeakIntoRender` keeps the two ends apart. `kg extract` bare
is the backlog — a document no source cites is invisible to every other command.

### ✅ `claim[disputed]` and the computation behind it
`conflict()` counted only source undercuts, so `disputed` fired **twice** across
the corpus while seven claim-to-claim contradictions produced nothing. Now both
count, with source undercut directional and claim-to-claim mutual, requiring the
contradicting side to be live and attested. 2 → 38 renders over 11 facts.

The predicate joins `computed` as a computed flag — and exposed that
`[computed=false]` had always matched nothing rather than the complement.

### ✅ Citations are content — the false-fresh a moved corpus produced
`SemHash` covered node content and incident edges but nothing from `Source`,
while `citation()` prints `doc:` and `class:` into the artifact at attach time.
Repairing a moved document and re-locking put every source back at `ok` and left
citing documents `fresh` with a dead path in their citation list. `class:` was the
worse instance — it drives the `[[id|class]]` token, the citation line, *and*
`classRank`, so a reclassification could flip which claim comes out `disputed`.

Hashing the authored `Source` fields was not sufficient: a source is never a query
row, so a spec pinning `claim[...]` never had it in the pin set. Hence
`Pin.Cites`, folded into `SetHash` alongside `Unresolved`, and written into the
managed block so the staleness is explainable and not merely detected. The
byte-state stays out — a re-exported PDF still reports `source-changed`.

Proven live: changing a cited source's `derived_from` flipped
`document-queue.kgraph.md` from `fresh` to `facts-changed`.

### ✅ Diarization handed back to oidio
`hearings.go`, `hearings.html`, the `/verify` route and its banner line, and eight
tests removed. kgraph consumes the rendered transcript as a source document and
needs to know nothing about speakers. `Registry` stayed — it is daemon-wide.

---

## Archived 2026-08-29

Everything below finished between 2026-07-27 and 2026-08-29 and was still sitting
in `plan/plan.md`'s Active work, which had not been reconciled since `a243aec`
(2026-08-03).

### ✅ Indexes — membership and closure
Built 2026-07-27. `.kg-index` resolution (`IndexAt`, nearest wins, an empty marker
names itself after its directory), discovery filtered by index so a graph is only
ever built from one index's documents, `Project.Index`, `LoadIn`/`ScanFactsIn`,
`--index`, cwd-implicit routing, and `kg indexes`. Six tests in `index_test.go`,
including the regression that two matters' same-id nodes must not collapse.
Verified on `~/life`: 713 → 661 nodes, 11 → 10 specs, and the attorney-packet
render stopped containing the other matter's facts.

**`## Index` in a spec documents and enforces; it never redirects.** A spec
resolves against the index owning its directory, and a declaration that disagrees
is an **error** naming both sides. Letting the header redirect would hand a spec
reach into another matter by editing a heading. Optional, both marker forms
(`name` / `index: name`) accepted, three tests, verified in the negative.

Still open, in `plan.md`: the daemon/registry path, and cross-index query.

### ✅ Dialects — the ladder becomes a value, and the index declares it
Built 2026-08-03 (`49162c3`, `a27e753`, `15c8d3d`, `b4fee7f`), extended 2026-08-29
(`dbdff52`, `ac81919`).

`classRank`/`validClass`/`speakerRequired` moved from package globals onto a
`Dialect` value; the `.kg-index` marker reaches the graph (`Graph.SetDialect`,
`g.Dialect()` falling back to `legal`); parse (`ParseFactsIn`) and query
validation (`ValidateQueryIn`) both check against the index's own ladder. A
dialect may **add** relations while the core edge table stays closed. Node
subtypes are a schematic layer over an arbitrary graph, and relations gained
identity, a bag, and a standalone form.

**The marker parser was the bug worth recording.** `indexName` took the first
non-blank line and `strings.Cut` on `":"`, so a marker containing only
`dialect: legal` named the index **"legal"** — silently, an index name being
validated against nothing. Replaced by `IndexDecl` + `parseIndexDecl` with a
**closed key set**: the fix is rejecting unknown keys, not special-casing
`dialect`, because every typo had that shape. `spec.go`'s `## Index` was
duplicating the logic and the bug; it now shares the parser and rejects
`dialect:` outright, since a spec that could name one would pick its own
evidentiary ladder by editing a heading.

**Invariant / policy split.** Never toggleable: in-index closure, `SemHash`
inputs, one-idiom-per-relation, the managed self-hash, `disputed`
computed-never-authored, and **`derived_from` on every `inference`** — that last
is not a completeness check but the edge that makes a conclusion invalidate when
a premise moves (`sources.go:426`), so waiving it breaks staleness rather than
relaxing a standard. Toggleable per index and parameterizable by kind:
`checkAttestation`, `speakerRequired`, citation conventions, scan warnings. **A
waiver must print**, or `0 errors` means something different in each index.

**Dialect must not enter `SemHash`.** A class *rename* is a content change on the
source node and should flag; the declaration itself must not, or switching
dialect re-flags every document in the matter.

**`engagement` sketch** (second dialect, unvalidated, no corpus yet). Out-of-ladder
`signed` (SOW, executed change order). Ladder: `written-confirmation` (the
client's own email — the `admission` analogue) › `measured` › `meeting-note` ›
`verbal` › `assumed`. Its invalidation flag is `unconfirmed` — a scope claim
resting only on `verbal` or below — which is `checkAttestation` with a rank
threshold and a kind filter, not a new mechanic. The draw: `supersedes` on a
requirement is change-order detection with an audit trail, and an estimate
modelled as an `inference` over named assumptions invalidates when the client
moves one. Optional later ladders — `clinical`, `audit`, `engineering`,
`historical` — all have real ordinal systems behind them and none has a corpus
here; journalism and due-diligence collapse into `legal` plus rungs, research
into `clinical`.

Remaining work is in `plan.md`: render's four hard-coded `legal.Rank` sites.

### ✅ Attestation — did the document actually say it?
Built 2026-07-28 through 2026-08-29.

Extraction writes facts from a document; nothing then checked that the document
says what the fact claims, and for a scan with no text layer nothing CAN. So a
person verifies once and the verdict is durable — oidio's contract in the other
medium, in a SIDECAR so re-extraction never destroys what a person confirmed and
confirming never freezes the facts.

**The unit is the `attests` EDGE**, not the fact and not the source: one source
supports many facts and each needs its own proof, so `(fact, source)` is the key.
`attest.go`, `attestations.jsonl` (append-only, last line per key wins — a
rewritten JSON object is a Syncthing merge conflict and a lost verdict),
`kg attest`, and the render/attach refusal.

**`unsupported` is the word this vocabulary was missing** (USER). `contradicts` /
`undercut_by` already cover a document that says the OPPOSITE and feed computed
`disputed`. A miscitation is a different failure: the source is SILENT, and the
fact may be perfectly true and hung on the wrong exhibit. So the fact is untouched
and the CITATION dies — the edge stops counting in one place and every downstream
rule follows on its own. `unclear` deliberately does NOT subtract: failing to read
a scan is a fact about the scan, and treating it as disproof would let a bad
photocopy strip a document of its evidence.

**Warning to author, error to publish** (USER). Unattested stays a warning in a
working graph; the escalation is narrower — the fact HAD citations, a person
opened them, none say it. `Graph.Miscited` refuses that at render and attach only.

**Signed verdicts** (2026-07-28, USER). `by` distinguishes a person's name from a
machine identity (`ocr-transcript`, `raglit`, `text-layer`). `MachineAttested` is
a predicate, not a rank — what a machine reading is worth is a reader's call, not
an invented statistic. `--by` is asked once and reused, derived from the last
signer in the sidecar rather than a config file, so identity has no second place
to go stale.

**The vocabulary converged onto raglit/attest's** (`850531a`). Four repos built
human attestation separately and only the LOCATOR ever differed: `confirmed` for
`attested`, `unclear` for `illegible`, plus `affirmed` — the ordinary pass, which
was genuinely missing, and without which every unreviewed unit and every
unremarkable one look identical. Old words are still READ; the log is append-only,
so rewriting spellings would be the one edit the format forbids.

**The workbench** (`verify.go`, `kg verify`). `kg attest --todo` listed 407
citations on fence-dispute, and a list is not a tool. One citation on screen, the
document beside it, drag a box over the proof, one key to rule. Regions are stored
in page FRACTIONS so a re-render at another DPI does not move the box; PDFs
rasterize server-side with `pdftoppm` at a 150dpi floor (USER — poppler is already
a de facto dependency and it keeps the "no CDN, no build step" rule). Serves on
the LAN because the browser is not on the dev box, and deliberately NOT part of
`kg daemon`: the daemon is a token-gated singleton across every project, and this
is a focused per-index session someone opens, works, and closes.

**✅ May a MACHINE verdict be `unsupported`? No — answered 2026-08-29** (`aa6f6b1`).
This sat open, and it was a live hole: `Supports` decided on the verdict alone,
so `--by ocr-transcript unsupported` was accepted and honoured, and because
`Miscited` is an ERROR at render and attach, one bad OCR pass could strip a corpus
of its evidence and then refuse to file. Absence from a text layer is not absence
from the document. `Attestation.Subtracts()` is now the single predicate:
`unsupported`, signed, by a person. Guarded at both ends because the log is
append-only — `AppendAttestation` refuses to write one, and `checkAttestVerdicts`
warns on one already recorded rather than honouring it silently. UNSIGNED counts
as machine, which closes the bypass of simply omitting `--by`.

- **still open (USER)**: does an `attested` verdict belong in the rendered
  citation as a pin-cite ("p. 3 ¶5")? If yes it is content and must reach
  staleness; if no the region stays a working aid. This is the one that decides
  whether attestation touches `SetHash`. Kept here rather than in `plan.md`
  because nothing is blocked on it.
- **risk carried forward**: a region is recorded against a rasterization. If the
  *document* is re-exported the box may no longer bound the same text —
  `source-changed` catches the drift, but the verdict's region silently becomes
  approximate. Consider invalidating a region (not the verdict) on drift.
- **loose end**: `Source.Anchor` exists, is in `SemHash`, and is unused.
  Per-source, so it is the wrong granularity for per-fact proof — decide whether
  it is retired or given a meaning before two overlapping ideas of "where in the
  document" exist.

### ✅ kgraph-web — the per-matter viewer
Designed in `plan/web.md`, built 2026-07-29 through 2026-08-02. Go + gat +
React/MUI/TanStack, fronted by `hz`. Auth with per-address lockout, reader
cohorts and privilege closure, annotations on the passage with a rail of all of
them, notes triage from the CLI plus a `/resolve-annotations` skill, version
history and compare, uploads sniffed by format rather than family, PDF plates,
search, print rules, and a citation model where a citation that closes a sentence
must touch it.

The auth risk the design named turned out right: gat surfaces one handler on
three transports, so auth had to be a gwag transform — HTTP middleware would have
left `/api/graphql` open.

Three decisions the design listed as the user's (evidence scope, `internalOnly`,
one-binary-per-matter) were settled in the building; `plan/web.md` is current and
is the reference.

### ✅ Variants — Option E, from raglit's rulings
`plan/variants.md` holds the design and the reconciliation. E landed as `eded9b2`
+ `3461eac`: `relations.go` asks raglit for COPY/VERSION **rulings** over its HTTP
client — not similarity scores, because 195 of 257 pairs are `overlap` where a
legitimate quotation is indistinguishable from a partial reproduction. Wired into
scan; warns on phantom corroboration and on a fact resting on a superseded
version. Unreachable is NOT empty: with no daemon `Relations` stays nil, meaning
NOT LOADED, silent — the same distinction `SourceDrift` makes.

The doc itself spent a month untracked in an abandoned agent worktree and was
recovered in `59640dc`.

### ✅ The real graph — the first conversions and the corrections they found
2026-07-27 through 2026-07-28. The loop ran end to end for the first time
(`document-queue.kgraph.md` → `generated/document-queue.md`, one citation
resolved, `fresh`) and immediately caught a migration defect: output paths resolve
relative to the **spec file**, so specs moved into `queries/` were declaring
`generated/…` and would have written to `queries/generated/`. All 10 repointed.

`document-queue` and `conflict-check` generated; 19 facts read from *Plaintiffs'
First Requests for Admission*; the MPSJ package started against scans with no text
layer; the Dorsey declaration and the 2015 baffle invoice; then a seven-agent
extraction fan-out over the SJ order, register of actions, Wren/Harrow
declarations, complaint, answer and MSJ memorandum, reconciled serially. 661 → 750
nodes, 0 errors, 44 documents read. The architecture held: agents read and
returned candidates, the reconciler assigned ids, resolved anchors and deduped —
which is what caught the survey identity below.

Eleven corrections, every one found by reading a primary against a roster entry:

- **Harrow, Brand & Teal is PLAINTIFFS' firm**, not defence counsel — read off
  the RFA document's FOOTER, which is plaintiff's letterhead because the set is
  plaintiff's own pleading with answers inserted. It had reached the conflict
  sheet under "Our side": the exact inversion a conflict check exists to prevent.
- **Milo Sedge is Sedge Septic, not Northwind Title** — his own sworn
  declaration against the party list.
- **Ives is co-counsel of record for plaintiffs**, WSBA #22500, with his own
  firm; the list had "CC'd on correspondence".
- **Only the DRIVEWAY easement is terminable.** The order uses "terminable" once,
  in the driveway ruling, and grants the sewer easement "as a concept" with
  location reserved. Withdrawn and superseded.
- **"Ha-Eun Suh Survey" and "Crestline Survey" are one document** — Suh was the
  surveyor AT Crestline, both names attach to AF 201503110043. The corpus carried
  them as two and had built a contradiction on the difference.
- **Harrow does NOT silently supersede his own survey.** He names both, explains
  the difference, and says the Quills retained him to check it.
- **The answer never responded to the Easement cause (paras 8.1-8.8).** Under
  CR 8(d) that may be an admission by omission, and it would explain why the court
  recorded the easement motion as uncontested.
- `s-dorsey-decl` and `s-sedge-decl` had no `doc:` — the graph asserted what they
  said without ever having read them. Both now point at the filed PDFs.
- **Dorsey paras 5 and 12 contradict inside one document**: para 12 opines the
  property is unlivable without a working septic, para 5 disclaims any knowledge
  of whether the septic works. Recorded as a contradiction, not an attack — both
  are sworn the same day.
- **Dorsey para 8**: the District cannot site a connection without an engineer AND a
  developer extension agreement *between Bramble and the Quills*. Plaintiff's
  own witness makes cooperation a precondition — a settlement lever.
- **`s-septic-2015-baffle` pointed at a client SUMMARY of the invoice and was
  classed `record`** — top of the ladder for a party's reading of a document.
  Repointed at the invoice PDF, which was in the corpus all along; the summary is
  now its own `interested` source carrying the two clarifications not on the
  invoice.

**✅ A checklist is not evidence** (2026-07-28, USER). `s-action-queue` cited a
living todo list outside this index as the source for ten facts. Five were actions
and two were questions, neither of which needs attestation; the three claims were
our own decisions and an observation about when firms answer the phone. Nothing
had been read from anywhere. Cost: seven items in the human attestation queue that
did not belong there, our judgement dressed as evidence, and — because the
checklist changes constantly — ten facts permanently reporting `source-changed`,
hand-restored after every `source lock` all session. Re-sourced to an `inference`
and an `observation`.

### ✅ Dialects are data, and a project owns its own
2026-08-29. `05fa1e2` (corpus-wide), `6a14b6f` (project-scoped), `38264ed`
(`kg indexes`), `0ef7da9` (`format.md` §4).

The list was closed in the binary. With `legal` as both the only entry and the
default, the feature had **no user-visible effect at all** — and "dialect"
appeared in zero of README.md, `plan/format.md`, `CLAUDE.md` and the CLI, so a
key on a file with a CLOSED key set was undiscoverable: guessing it wrong was a
hard error with nothing to guess from.

**What replaced the closed map**, since the closure was doing real work and had
to be paid for three times over:

- **`validateDialect` is one implementation**, called from `init()` for the
  built-ins and from the file parser for everything else, so the two cannot
  drift. It also checks what the built-ins never needed: a non-empty
  duplicate-free ladder, the governing class NOT on the ladder, `speaker_required`
  as a subset of the ladder, a subtype's kind being a core kind.
- **A corpus-wide file may not redefine a built-in.** `legal` would otherwise
  mean something different per checkout.
- **`dialect-changed`.** `DialectHash` binds into the render token and the managed
  block. A built-in hashes to the empty string, so nothing already rendered
  false-flags — the `StyleHash` rule, for the `StyleHash` reason.

**Project dialects are qualified by their index** — `engagement.yaml` under
`acme/` is `acme.engagement` — and that qualification is what makes the two
shadowing rules able to differ. A bare `dialect: engagement` finds the project's
if it has one and the corpus-wide one otherwise; the qualified form resolves too.
Two matters can each mean their own thing by one word and neither can change what
the other resolves, so a project may specialise even `legal` as `acme.legal`,
explicitly, with the built-in untouched elsewhere.

`IndexDecl` gained `Dir` — the only thing that knows where a project starts is
the walk that found its marker, and `DeclAt` was discarding it. The fingerprint
now collects `.kg-index` markers during the walk it already does and stats each
project's dialect directory, without which a project dialect could be edited
under a running daemon and nothing would move: the walk skips every `.kgraph/`,
which is where they live.

**The limit, stated rather than buried**: whether a ladder's ORDER is right for
the trade is not checkable and never was, including while the list was closed.
`format.md` §4 says so in the document a corpus author reads.

Two bugs found in passing. `LoadDialects` must stat before reading, because
`.kgraph` is a project marker `DiscoverRoot` accepts as a plain FILE — reading
through one yields ENOTDIR, which `os.IsNotExist` does not cover, and one fixture
does exactly that. And `format.md`'s States table listed four states while the
code emitted eleven.

`TestTheDocumentedDialectExampleParses` reads the worked example OUT of
`format.md` and loads it, so the document and the parser cannot drift — the same
class of failure this repo already records, where a spec exemplar pointed at a
hand-typed `.ocr.md` labelled `class: record`.

### ✅ Generation retired, and facts became a store
2026-08-29/30. `plan/facts.md` holds the design and the seven steps; this is the
one-line record.

`render`, `attach` and `build` are gone — CLI, daemon routes, MCP tools, the
prose helpers, `guard-managed`, `guard-output`, and 30 tests. `render.go` became
`conflict.go`: it was TWO FILES UNDER ONE NAME, and the half that stayed was
never about prose — `tension`/`conflict`/`impeachment` are what the query
evaluator answers `claim[disputed]` with.

`*.kfacts.md` is gone too. Facts live in `~/.kgraph/<index>.db`, one append-only
`assertion` table outside the synced tree, written only through ops that fold the
log they would join and refuse anything that would not load. `kg export` is the
text half and loads on its own.

**`dialect_hash` moved rather than died**, and that is the shape of the whole
change. It lived in a managed block because the ladder decides what the prose
said about a conflict; the dependency outlived the prose, because
`claim[disputed]` resolves through the same machinery — an edited ladder changes
WHICH ROWS come back while the query text and every fact stand still. It is on
`Standing` now.

Status, diff and `report-stale` are over standing queries for the same reason:
they asked which DOCUMENTS were stale, and the question outlived its subject.

#### What the store is, as shipped

`*.kfacts.md` is retired. Facts live in `~/.kgraph/<index>.db`, one append-only
`assertion` table, written only by kgraph through `Assert`/`Withdraw`/`Correct`/
`AnswerQuestion`/`Retire` — each of which folds the log it would join, builds the
result, and refuses anything that would not load. `kg export`/`kg import` are the
text half, and the export is loadable on its own, so a corpus handed to somebody
with no database still works. `examples/` is an export; `testdata/legacy/` is the
sample the migration imports from.

- ◻ **The web asserter** (`plan/web.md` step 6) is the one consumer still to
  write, and the door it needs already exists.
- ◻ **`kg extract` still emits a prompt for a person to paste.** It should
  produce `assert` ops — smaller than it sounds now, and it is the seam the
  harness needs (`plan/split.md`).
- **risk**: `sql/schema.sql` and `sqlc.yaml` are still the dead render-era
  scaffolding, now doubly wrong — they describe a derived index of a format that
  no longer exists. The live schema is `storeSchema` in `store.go`. Delete them.

### ◐ Finish retiring generation — the spec's dead half

`render`, `attach` and `build` are gone (2026-08-29): the CLI commands, the
daemon routes, the MCP tools, `Render`/`Attach`/`RunBuild`/`WriteSpec`, the
prose helpers, `guard-managed` and `guard-output`, and 30 tests. `render.go`
became `conflict.go` — it was two files under one name, and the half that
stayed was never about prose: `tension`/`conflict`/`impeachment` are what the
QUERY evaluator answers `claim[disputed]` with.

**`dialect_hash` moved rather than died**, and that is the shape of what is
left. It lived in a managed block because the ladder decides what the prose said
about a conflict; the dependency survives the prose, because `claim[disputed]`
resolves through the same machinery — an edited ladder can change WHICH ROWS
come back while the query text and every fact stand still. It is on `Standing`
now and `Ask` reports `dialect-changed`.

- **next**: `spec.go` still parses `## Prompt`, `## Outputs`, `## Build` and the
  managed block, and still carries `Managed`, `RenderToken`, `StyleHash` and the
  document half of `State`. Nothing calls any of it. Go does not complain about
  an unused function, so this will sit there looking load-bearing until somebody
  deletes it.
- ❓ **What a `*.kgraph.md` IS now** (USER). Stripped of prompt and outputs it is
  a named query with a purpose — which is a second way to declare what
  `standing.yaml` declares, and two idioms for one thing is the failure this
  format refuses everywhere else. Either specs become the file-based
  declaration and `standing.yaml` holds only the recorded ANSWERS (clean, and it
  puts the question in git where `git blame` works), or specs go entirely and
  `kg ask add` is the only door. **Recommend the first.**
- **risks**: `examples/` fixtures are `*.kgraph.md`, and
  `TestNoSpecQueryResolvesEmpty` — named in `CLAUDE.md` as the highest-value test
  — is a spec test. Its VALUE is "a declared query must return rows", which
  applies identically to a standing query. It must be repointed, never deleted.
- **also gone**: `.kgraph/style.md` now feeds nothing. It was the corpus's house
  style for GENERATED PROSE and there is none. `Graph.Style` and `StyleHash` are
  dead with the rest.

### ✅ Thread the dialect into render — resolved by deletion

Render is gone, and `render.go:507,540,626` went with it. `webtree.go:147` still
calls the package-level `ClassRank`, which answers for `legal` — that is now the
only hard-coded ladder left, and it is a UI display concern rather than a
conflict-resolution one. Recorded under kgweb's risks in `plan/web.md`.

- **next**: `render.go:507` (weakest-link wording), `render.go:540` (conflict
  resolution) and `render.go:626` all call `legal.Rank` directly; `webtree.go:147`
  calls the package-level `ClassRank`. `g.Dialect()` already exists and
  `graph.go:1225` uses it — this is passing it four more places.
- **risks**: these are the sites that decide which of two conflicting sources
  wins and what the document says about it. A dialect whose rungs are ordered
  wrongly corrupts that **silently** — no error, just confidently wrong
  `disputed`. That is now a LIVE risk rather than a hypothetical one, because a
  corpus can author its own ladder; the mitigations are that everything checkable
  is checked at load, that `kg indexes` names the bound dialect, and that editing
  one reports `dialect-changed`. None of them checks the order, and none can.
  Until render is threaded the risk is narrower than it reads — render still
  applies `legal` to every index, which is wrong in the other direction.
- **`ClassRank`/`ClassLadder` stay as the `legal` default on purpose.** caselit
  consumes them and derives its `backed` floor from `document`'s rank, so they
  answer with no index in hand. Giving dialects an index-scoped form is the
  migration; retiring these is not part of it.
- **blocking decisions (USER)**:
  1. **Inference chain depth.** `inference` is rank 0, the floor, so a conclusion
     drawn from a `record` and one drawn from three chained conclusions weigh the
     same. Tolerable in legal, where the chain is argued in front of someone; not
     in an engagement corpus where derivation is the majority case. Add a depth
     rule the dialect enables, or propagate the weakest premise's rank? Recommend
     the rule — changing rank propagation touches conflict and impeachment in the
     legal index, which currently works.
  2. **Does a dialect eventually carry render rules?** Journalism says yes — an
     anonymous source must be withheld from output while staying in the graph.
     Don't build it; don't design the type so it is forbidden.
  3. **`engagement` needs a corpus** before its ladder is more than a guess. Own
     client work is the candidate, and carries no `~/life` write restriction. It
     is now the worked example in `format.md` §4 and is parsed by a test, so the
     SHAPE is proven; what is unproven is the order of its rungs, which is exactly
     the part no validator can check.
- **explicitly out of scope**: intelligence-style two-dimensional reliability ×
  credibility (Admiralty). The ladder is `map[string]int` and cannot express it;
  wanting it is a conflict-resolution redesign, not a table swap.



### ✅ The store is a LOCATION the caller names (2026-09-01)

Asked for by caselit, and it was blocking that layer from writing facts at all.
`OpenStore(path)` always took any path, so a consumer could WRITE anywhere; what
it could not do was make this package READ from there. `StorePath(index)` had two
callers and the one on the LOAD path was hardcoded policy in the middle of a
library function, so a consumer keeping its store beside its own corpus got a
graph loaded from somebody else's database, or from none.

That was a LIVE hazard, not a future one: `~/.kgraph/<index>.db` is keyed by
index NAME and nothing else, so two corpora spelling an index the same way share
a store silently and the loser is whichever is read second. caselit's suite had
to point `HOME` at a temp directory to stop a developer's own database shadowing
a committed fixture — a workaround living in a downstream package, which is where
one should never have to live.

- **`StoreRef` + `OpenStoreRef` + `DefaultStoreRef`** (`store.go`). DSN is `""`
  for the policy, a path, `sqlite:<path>`, `postgres://…`, or `none`. The scheme
  picks the engine; a bare path is SQLite because that is what every existing
  caller passes. A Postgres DSN is REFUSED BY NAME rather than opened as a file
  with a colon in it — designed for, not built. `StoreRef.Index`, the `idx`
  discriminator, is in the shape and refused while it does nothing: a field that
  errors is louder than a field that is missing.
- **`ScanFactsInWith` / `LoadInWith`** (`scan.go`), the shape `BuildWith` already
  had. The bare forms keep today's behaviour exactly, so no existing caller
  changed. `storedFacts` takes the store plus a flag asking for the policy, and
  only the bare entry points pass the flag.
- **A NIL STORE MEANS LOG ONLY** — do not look for a database. That is exactly
  what a corpus handed to somebody else is, and before this it was reachable only
  by the file happening not to exist, which is a state nobody could ask for and no
  test could assert.
- **An EMPTY store falls through to the export**, where it used to answer "no
  facts" and stop. Safe because the table is append-only: a log ever written to
  cannot come back empty, so empty means never written rather than emptied. It
  fixed a live failure — `OpenStore` CREATES its file, so the first `kg export`,
  `import` or `assert` in a corpus loading from its exported log left an empty
  database behind and every later scan reported that corpus EMPTY.
- **Shell**: one global `--store <dsn>`, `$KGRAPH_STORE` beneath it, the
  per-index default beneath that. No second index flag — `--index` IS the
  discriminator. `--store none` loads from the export and makes every command
  needing a database REFUSE, saying that it refused and naming the default it
  would otherwise have used: a write that silently went to a default database
  while the operator believed they were working from a log is the failure worth
  spending an error message on.
- **`kg indexes` prints the store each index resolved to**, and whether a
  database is actually there. Two locations for one index is a shadowing failure
  that exists whether or not anybody names a store; naming it only helps if the
  answer is visible without guessing.
- **Deliberately NOT a `.kgraph/store` marker file.** A DSN in a corpus file is a
  credential in a repository. If a per-corpus declaration is ever wanted it names
  an ENV VAR to read, never an inline password.
- Tests: `TestStoreRefResolvesEngineAndLocation`,
  `TestTheCallersStoreWinsOverTheDefaultLocation` (which asserts the shadowing
  first, so it fails loudly if it stops testing anything),
  `TestANilStoreReadsTheExportAndNeverTheDatabase`,
  `TestAnEmptyStoreDoesNotHideTheExport`,
  `TestStoreResolutionPrefersTheFlagThenTheEnv`,
  `TestStoreNoneRefusesEveryWriteAndSaysWhy`.
- **left open, in `plan.md`**: the `idx` discriminator and the Postgres engine —
  neither is needed for the file case, and the discriminator carries a migration
  that can go silently wrong.


### ✅ `kg answer` closes the question (2026-09-01)

Reported from the `aw4` corpus: answering left `status: open`, so
`question[status=open]` kept matching an answered question and every
open-questions query over-reported, silently, in every consumer.

**It was half-deliberate, not an oversight**, and that mattered to the fix. The
doc comment argued a question must not be flipped to `resolved` with nothing
attached, because that is an answer nobody can check — correct, and it stays.
What it never did was close the question when an answer WAS attached. So
`AnswerQuestion` now writes both lines in ONE `Apply`: the answering fact with
its `answers:` edge, and the question re-asserted as `resolved`. The rule is
satisfied rather than relaxed — the fact is attached in the same act, validated
together and refused together.

The codebase had already modelled it this way and only the write was missing:
`taint.go:145` simulates answering as the option asserted AND the question
`SResolved`, so `kg variants` was predicting a state that answering never
produced. `SResolved` existed in the vocabulary, documented "questions", written
by nothing.

- The question's other fields come along, because an assert REPLACES rather than
  merges — same reason `Amend` exists. Not reused: `Amend` applies separately and
  two acts can half-succeed.
- `TestAnsweringAttachesAFactRatherThanFlippingAStatus` pinned the OLD contract
  and was repointed, not loosened: it still requires the answer to be attached —
  the real invariant — and now requires the close, the question's body and
  `needs` to survive it, and both assertions to be in the log.
- Answering a question the corpus does not hold stays REFUSED and writes nothing:
  `Apply` rejects the dangling `answers` edge, so the close cannot smuggle a
  question into existence. `TestAnsweringAQuestionTheCorpusDoesNotHoldIsRefused`.
- Verified through the CLI end to end: `question[status=open]` 1 row → 0 rows.

### ✅ The party-name guardrail (2026-09-01)

`TestNoPartyNamesFromTheLiveCorpus`. The scrub was one act; this is what keeps it
true, because the failure mode is not a bad decision — it is somebody reaching
into the live corpus for a realistic fixture with nothing objecting.

**It stores no names.** A test listing what to look for would put the names back
into the repo it protects, so it derives them from `~/life`'s `entity:` values at
run time and SKIPS where that corpus is absent. It also skips, loudly, if it
finds too few tokens to be checking anything.

It paid for itself immediately: **twelve real names the manual scrub missed** —
four surnames, four given names, two firms and two brokerages, across
`README.md`, five tests, three plan docs, `cmd/kgweb` and `webui/`. They are not
listed here, and that is not coyness: the first draft of this paragraph named all
twelve and the guardrail failed the build over it, which is the whole mechanism
working. And it found the reason one survived: **in a Go string literal a name
written straight after `\n` has no word boundary** — the character before it in
the FILE is the letter `n` — so `\b<name>\b` never matched and it hid in plain
sight. The guardrail now treats an escape as a separator.

- **Stated trade**: a surname that is also a common word this repo uses —
  `Page`, `Lot`, `View`, `Trust`, `Judge`, `Escrow`, `Drain`, `Doctor` — is not
  guarded, because guarding it fails on the repo's own prose hundreds of times.
  Where such a name appears AS a name it was replaced by hand — the invented
  half of one such pair is `Nolan Sinclair` — and the bare common word is what
  remains.
- The repo's own author (`Carl Taylor`) is exempt: that is authorship, recorded
  on every assertion and every commit, not a party's identity.


### ✅ The live corpus is migrated and loads again (2026-09-01)

It had been DARK. 65 `*.kfacts.md`, no export, no store — so from the moment the
parser was deleted, `kg scan` reported 64 unread fact files, built an EMPTY
graph, and every one of the 21 specs resolved to 0 rows. The plan carried
*750 nodes · 0 errors* from 2026-07-28, which was not a stale reading but a false
one.

**Both live indexes are across**: the litigation one, 2,127 assertions from 64
files; the billing one, 52 from 1. Stores at `~/.kgraph/<index>.db`, exports
written into each project. Neither is named here — an index is named for its
parties, which is the rule the guardrail enforced on the first draft of this very
paragraph.

    2116 nodes · 3853 edges · 21 specs · 0 error(s), 619 warning(s)

Larger than the last recorded reading by a factor of three — the corpus grew a
lot while it was unloadable. 894 claims · 729 sources · 252 questions (214 open)
· 78 events · 63 entities · 56 actions · 44 groups.

**Verified in both directions before anything was deleted, and nothing was:**

- **The store says what the files say.** Re-derived the graph from the 64 fact
  files into a fresh store and compared to the live one node by node: 2,116 nodes
  and 3,853 edges, identical by `SemHash`.
- **The export rebuilds the store.** Replayed `facts.jsonl` into a third store
  and compared again — identical. The text that now lives in the corpus is a
  faithful copy, not decoration.

**The 64 fact files are still there, on purpose.** Migration carried the fenced
blocks; **754 lines of prose around them were NOT carried**, and that prose
belongs to a FILE rather than to any entry, so moving it is a decision per file —
`note:` on an assertion is where it would go. Deleting the files before that
decision loses the explanation, and `git` holding it is not the same as the
corpus holding it. USER's call.

Side effect worth recording: `source status` reports **587 sources ok · 0 changed
· 0 unrecorded**, so `sources.lock` was already current and the migration
introduced no drift.


### ✅ The daemon resolves the index owning the caller's directory (2026-09-01)

`Registry.Get` called `Load(root)` — the DEFAULT index — while `kg` resolved the
marker owning the working directory. So `kg daemon`, `kg serve` and `kg scopes`
answered from a different graph than the CLI in the same checkout, and in a
corpus whose facts all live under markers that graph is EMPTY: the viewer
reported a matter with nothing in it rather than reporting an error. The web
viewer and MCP are the surfaces people actually use, so this was the
cross-matter confusion indexes exist to remove, wearing the daemon's hat.

- `Registry.Get` resolves `IndexAt(root, dir)` and loads through `LoadIn`. A
  marker that will not parse is an ERROR, not a fallback — falling back to the
  default index is exactly what produced a confident wrong answer.
- **The scope key was too narrow.** `(root, branch)` meant two indexes under one
  checkout shared a cache entry and the first one loaded answered for both. Now
  `(root, branch, index)`, and `Scope.Index` is reported so which graph is being
  served is visible rather than assumed.
- The watcher is keyed and threaded the same way — one watcher per index, or a
  subscriber watching one index is told about another's changes. `Registry.Docs`
  takes the index with it.
- `TestRegistryResolvesTheIndexOwningTheDirectory` covers both halves and was
  checked against the OLD code: reverting either the load or the key fails it.
  The suite passed before the fix, so nothing had been covering this.
- Verified against the live corpus end to end: the daemon now reports
  `index: <the matter> · 2116 nodes · 3853 edges · 21 specs · 0 errors`, which is
  what `kg scan` says.

**Known and accepted**: `fingerprint` still walks the whole root, so editing one
index invalidates the sibling index's scope too. That is over-invalidation — a
reload, never a wrong answer — and narrowing it is not worth the complexity at
this scale.


### ✅ `*.kgraph.md` is retired (2026-09-01)

The format is gone. `spec.go` deleted; `Pin` and `Resolve` moved to `declared.go`
because they were never about documents — they are about a set somebody
committed to.

- **`DeclaredSet` / `DeclaredQuery` are the currency.** `Resolve`, `DiffGraphs`,
  `Variants`, `CheckSetProportion` and `CheckSplitContradiction` all take a set
  of declared queries rather than a `*Spec`, so a standing group and a spec
  document were the same thing to everything downstream and deleting the format
  changed one producer.
- **Six consumers repointed**: `Backlog` (its `specs` parameter was DEAD — unread
  since render retired), `FactsUsedBySpecs` → `FactsUsedByDeclarations`,
  `Project.Spec` → `Project.Declared`, five daemon endpoints, `Scope.Specs` →
  `declaredCount`, and discovery, which now counts the DECLARATIONS in a
  `standing.yaml` rather than the file — a count of 1 says nothing about a corpus
  with 289 questions in it.
- **Converted with rows compared through both doors, never text carried across.**
  `examples/`: 10 specs → 34 standing queries. The live matter: 21 specs → 289.
  **On the live corpus, all 119 contradiction findings are IDENTICAL — zero lost,
  zero gained** — measured by comparing the findings themselves rather than
  counts, which is what caught the two traps below.
- **Trap 1, caught by the comparison**: the first conversion copied each spec's
  DOCUMENT purpose onto every one of its queries. `Purpose` raises the proportion
  check's threshold from 75% to 90%, so an 87% set silently stopped being
  flagged. `Standing.Purpose` is the QUERY's own.
- **Trap 2**: without `Standing.Group` every query became a group of one and the
  contradiction check got strictly noisier — 19 findings where the specs gave 10.
- **Tests moved, not loosened.** `TestSpecQueriesParse` was deleted as strictly
  subsumed by `TestNoDeclaredQueryResolvesEmpty`, which parses, resolves, requires
  rows, and refuses to pass having checked nothing. Four `## Index` header tests
  went with the header they tested; the one whose value survives —
  a declaration cannot reach another index — is now
  `TestDeclarationsAreScopedToTheirIndex`, scoped by LOCATION since
  `standing.yaml` lives inside the index and no header can redirect it.
- **Still on disk, deliberately**: the 22 `*.kgraph.md` in the live corpus are
  inert but not deleted. Their `## Prompt` sections are authored prose that
  standing queries do not carry — the same reason the migrated `*.kfacts.md` are
  still there. USER's call, per file.

### ✅ `kg source lock <id>` (2026-09-01)

Locking one NEW document silently re-locked every `changed` one — and a
`changed` source is a drift signal somebody is midway through investigating. It
swallowed one twice in a single session and had to be restored from git by hand
both times. `LockAll` takes `only ...string`; empty keeps the sweep, which is
what an initial lock of a corpus wants. The CLI refuses an id the index does not
hold, and reports only what was asked for — otherwise the sweep it declined to do
reads as one it performed.
`TestLockingOneSourceLeavesOtherDriftAlone`, checked against the old behaviour.


### ✅ The `idx` discriminator, and `kgraph_query` (2026-09-01)

**One database can hold several closed graphs.** `assertion` gains `idx`, keyed
`PRIMARY KEY (idx, seq)`, and every read and write is scoped by it. `seq` is
numbered per index, so one matter's volume never advances another's.

- **The migration is where this goes silently wrong, and it has no error in
  it.** `ADD COLUMN idx TEXT NOT NULL DEFAULT ''` leaves every existing row under
  the EMPTY index; a scoped read of a named index then returns nothing and the
  corpus reports itself empty. So `migrateIdx` STAMPS existing rows with the
  index the FILE was named for.
- **I built that exact failure and caught it in the same pass**: the stamp used
  the filename while `OpenStore` still read with `""`. `IndexOfStorePath` is now
  the single source of the derivation, so the stamp and the read cannot disagree.
  `TestAPreIdxStoreIsStampedWithItsFilesIndex` builds a store in the old shape and
  proves the pre-migration row is still reachable.
- The schema is split around the migration: `CREATE INDEX ... ON assertion(idx,
  id)` cannot run against a store written before the column existed, and
  `CREATE TABLE IF NOT EXISTS` is a no-op there.
- `PRIMARY KEY (idx, seq)` makes a lost `MAX(seq)+1` race a constraint violation
  rather than a silent overwrite — never reached under SQLite's single
  connection, and the outcome worth having under a shared Postgres.
- Verified on the live store: 2,127 rows stamped, graph unchanged at 2116 · 3853
  · 0 errors.

**`kgraph_query` is the projection consumers asked for.** What was asked, the
answer on record, and whether it still holds — in SQL, without linking kgraph,
folding the log and re-evaluating in memory.

- **Safe where node/edge tables were not, and the difference is authorship**: a
  pin is somebody's accepted answer, signed, not a second copy of the fold. The
  one computed column can lag the log but cannot disagree with it.
- **Drift REASONS, not a boolean.** `Ask` distinguishes five states and
  collapsing them throws away the only part a consumer can act on. `computed_at`
  records when the flag was decided, so a consumer reading against a newer log
  treats it as unknown rather than fresh.
- **A whole-index replace, written only by `kg ask`, and only when every query
  was asked** — a partial ask would silently retire the rows it did not visit.
  Fails soft: a store that cannot hold it must not fail the ask, and says so on
  stderr rather than skipping silently.
- Verified end to end on the live corpus: 289 rows; asserting one fact flipped
  exactly 3 queries to `results-changed`.
- **That probe was a write into a live legal matter and it was the wrong place to
  run it.** The store was restored from a pre-probe backup — 2,127 assertions, no
  probe rows, 0 errors. A scratch copy was the right tool and I used the corpus.


### ✅ Hardening the checkers (2026-09-01)

Prompted by three guards built the same day that passed their unit tests and
failed against the live corpus: rules applied mid-load (silently exempting the
second-largest check class), liveness unable to find its own watermark when the
graph was empty, and `kg source dupes` shipping with a top band that could never
fire. **None was a missing test. Each was a test that could not fail.**

1. **Reachability** — `TestEveryRegisteredCheckFiresOnTheFixtures`. It found 3 of
   4 registered checks never fired on `examples/`, including
   `unreferenced-source`, the largest class in the live corpus.
   `testdata/allchecks/` now triggers each, kept apart from `examples/` so
   exercising a checker never disturbs its standing pins. State checks are exempt
   BY NAME and must name the test covering them.
2. **Shape, not text** — `TestTheDiagnosticShapeIsStable` pins per check: count,
   severity, and how many carry a KEY. That last column catches what text cannot:
   a check that quietly stops identifying its findings prints identically while
   becoming untunable and unacceptable.
3. **Non-degenerate fixtures** — `newCorpus(t, name)` puts the index in a
   subdirectory. A root-level index makes the derived directory and the root the
   same string, so a wrong derivation looks right.
4. **`IndexDirKnown`** — `IndexDirOf` returned `""` for both "the default index at
   root" and "I could not tell", and that ambiguity WAS the liveness bug.
5. **Pipeline tests** — `TestARuleSilencesACheckThatRunsLastInTheLoad` goes
   through `LoadIn` and silences a check that runs LAST, which is the one a
   misplaced call misses. **Demonstrated**: reintroducing the original bug leaves
   the unit test passing and fails this one.
6. **`./bin/check`** — gofmt, vet, staticcheck, unparam (advisory), tests. It
   found 18 dead symbols on first run, eleven of them left by that day's own
   retirement, and caught an unused variable in the hardening work itself. vet
   reported none of them.

**Stated limit, in `CLAUDE.md`**: none of this substitutes for the real corpus.
All three failures were found there and two only by deliberately reproducing the
failure the feature was written to prevent.


### ✅ The live corpus's SHAPE, in the repo (2026-09-01)

The gap the other six hardening measures could not close: `examples/` is ~100
nodes and the live index is 2,129 with a completely different distribution, and
three checker bugs in one day lived in that difference.

**A profile is the shape without the content.** `ProfileOf` measures counts and
distributions — kinds, source classes, citations-per-fact, unreferenced sources,
contradictions, open questions. **Every string in the recorded file is a schema
key or a kind name; nothing from the corpus is in it**, which is what makes it
publishable where the corpus never will be, and consistent with the party-name
rule rather than an exception to it.

`synth_test.go` generates a corpus to that profile and runs the checkers over it:
1,850 nodes, 420 unreferenced sources (the live figure exactly), 713 of 894 facts
on a single citation, 98 contradictions. All three content checks fire, and the
attestation ranking is exercised against 1,091 pending citations rather than the
handful `examples/` produces.

**Three attempts before the fixture exercised anything**, and each failure was
instructive rather than fiddly:

1. No standing queries — the set-shaped checks never ran.
2. A query over EVERY asserted claim put both ends of every contradiction in one
   set, so the check correctly reported nothing.
3. Two sets in ONE group meant a sibling carried the other end — the grouping
   working exactly as designed, and exercising nothing.

A fixture that produces no findings looks identical to a checker that works.

**What it does not close**: synthesized facts are uniform where real ones are
strange. This buys scale and distribution, not weirdness — and the ordering bug
is still caught by the pipeline test rather than by this.


### ✅ Star pagination is not content — a checker FALSE NEGATIVE (2026-09-01)

Found by asking whether the 15 absent quotes shared a cause instead of working
them by hand. A reported opinion carries the printed page boundary INSIDE the
sentence:

    "a broker or seller has a *177 duty to disclose all material facts"

`foldText` keeps letters AND DIGITS, so `*177` folded to `177` in the middle of
the text and any quote spanning a printed page reported ABSENT with the words
plainly there. **A false negative, which is the expensive direction** — it sends
a person to verify a citation that was correct. 59 sources in the live corpus are
reported opinions, so it recurs.

Skipped inline in `foldTextIdx`, the one function both the quote and the document
go through — the rule that file already states, that two implementations of the
fold diverge and the divergence is a match that silently fails to happen.
`TestAQuoteSpanningAPrintedPageBoundaryIsFound`, checked against the unfixed
code. Live corpus: 15 absent → 14.

**Two process notes, both mine:**

- The first implementation pre-masked with a regex behind a
  `strings.Contains(s, "*")` guard. Markdown is full of asterisks, so the guard
  would never fire and the regex would run over every megabyte of every document.
  Replaced with an inline O(n) skip.
- I then measured the suite at 4m41s and blamed that regex. **It was not the
  cause** — a leftover debug test of mine was walking a 1 GB documents tree per
  quote, and a `pkill` that matched its own shell had killed the command that
  would have deleted it. The suite is 16s either way. The inline skip is still
  the better implementation and was kept; the reasoning first written into its
  comment was wrong and is corrected.
