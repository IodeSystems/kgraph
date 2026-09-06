# kgraph — living plan

> How this plan works: see the Planning section of `~/CLAUDE.md`. This file holds
> current state, active work, and decisions only. Completed trees move to
> `plan/done.md`; deferred opt-ins move to `plan/icebox.md`.
> Format design lives in `plan/format.md`; the viewer in `plan/web.md`; hooks in
> `plan/hooks.md`; the same-instrument-held-twice design in `plan/variants.md`;
> retiring authored fact files in `plan/facts.md`; where the orchestration goes
> in `plan/split.md`.

## What this is

A knowledge-graph daemon/CLI/MCP over project corpora. Facts, questions, and
their logical relations are the source of truth; documents are **generated** from
query-bound prompt templates. When a fact changes or an open question resolves,
`kg diff` reports which query results moved and which documents therefore need
revising — with a delta specific enough to fix the wrong sentence rather than
rewrite the page.

Driving corpus: `~/life` (12 projects; `fence-dispute-v-halloway` is the hard one —
200+ docs, already raglit-indexed).

## Decisions (settled)

- **Graph is source of truth. Docs are outputs.** Regeneration is always a
  *revise* — the LLM gets old doc + old prompt + new prompt + fact delta.
- ~~**Files authoritative, SQLite derived.**~~ **INVERTED 2026-08-30 (USER):
  SQLite is authoritative, the JSONL export is text.** Kept struck through rather
  than deleted, because it was settled for two stated reasons and both stopped
  applying rather than being overruled: `git blame` went with git history — and
  history had already stopped depending on git the moment `by`/`at` went on every
  assertion — while surviving a FILE SYNCER only earns its keep when more than
  one machine writes an index, and it is one machine. The Syncthing hazard is
  unchanged and is why the DEFAULT database location is `~/.kgraph/<index>.db`,
  outside the corpus, with the export inside it. See `plan/facts.md`.
  **AMENDED 2026-09-01: the default is a policy, not the only answer** — a caller
  names its own store (`--store`, `$KGRAPH_STORE`, `StoreRef`), and the hazard
  becomes a thing a caller accepts knowingly rather than one the library decides
  for everybody. Built; see `done.md`, and the engine half still open under Active
  work. Full rebuild on watch — at 10³–10⁴ nodes, incremental view maintenance is
  not worth building.
- **The corpus owns its prose rules; kgraph owns the plumbing.** `.kgraph/style.md`
  is interpolated into every render prompt in the index. A default style shipped
  in the binary would be the scaffolding the render invariant forbids, so there
  is none — absent means render behaves exactly as it did before the feature.
- **Glob discovery, no registry.** `*.kfacts.md` (nodes) and `*.kgraph.md`
  (specs) anywhere; one derived `.kgraph/index.sqlite` at repo root.
- **One graph per repo root, namespaced by project.** Entities cross projects, so
  per-project graphs would forbid the edges that matter.
- **Eleven binary edge types.** `causes implies requires prohibits supports
  contradicts answers supersedes member_of about attests`. Group operators are a
  single *aggregate expression* on a group node (`all`, `>= 2`, `sum(value)`);
  temporal words are *qualifiers*. Both were arity-wrong as edges, and folding
  them in is what would have made queries free-form again.
- **No polarity.** `not` is `status: false` plus the negative edge types
  (`prohibits` = negative `requires`, `contradicts` = negative `supports`). A
  second spelling costs correctness and buys nothing the fixtures needed.
- **Sequence is not dependency.** Groups carry `ordered`; forcing priority into
  `requires` invents blocking that isn't real.
- **Immutable facts.** Correction = new node + `supersedes` + `reason`, old node
  → `withdrawn`. Never edit in place. `withdrawn` (we were wrong) and `false`
  (tested and rejected, kept so `prohibits` has a target) are different: only
  the first flags documents.
- **One prompt → N outputs.** Static sites, sources + resources. Binary targets
  (PDF) come from an optional `## Build` step, since an LLM emits text only.
- **Generated outputs are not committed.** Separate scope.
- **Surface DSL lowers to a JSON AST.** Humans and doc headers read the DSL;
  the MCP tool accepts the AST schema-constrained so an agent cannot emit a
  syntax error. One evaluator, in-memory — CTE lowering would be work for no
  gain at this scale.
- **Sets are pinned, not just members.** Prose commits to counts, order, and
  group cardinalities; the pin records all three so the delta can name the broken
  sentence. Default sort `id` so re-resolution never reorders spuriously.
- **Go, matching raglit conventions.** Flat root package, `cmd/kg`, `plan/`
  topic docs. SQLite/sqlc is scaffolded but unused and may not survive — the
  in-memory evaluator made it nearly redundant.
- **Sources are nodes, not a `src:` string.** Provenance is queryable, weighted
  by an ordinal evidentiary class (`record` … `interested` … `inference`), and
  hashed — a re-OCR'd document flags every fact it attests as *re-verify*.
- **No authored `confidence`.** A hand-typed `0.7` is an invented statistic —
  and `~/life`'s own document rules forbid fake probabilities. Weight derives
  from source class.
- **`disputed` is computed, never authored.** Asked with `claim[disputed]`; both
  a source `undercut_by` and a claim-to-claim `contradicts` count, and the
  contradicting side must itself be live and attested. The *underdetermined* half
  of this decision was never built — see icebox.
- **`contradicts` is mutual; `undercut_by` defeats.** They lower to one edge
  type, so `Edge.OneWay` records which key wrote it and `conflict` reads a
  one-way edge only from the end it defeats. Without it the undercutter came out
  disputed by what it undercuts, and a court order rendered `DISPUTED` in two
  documents going to counsel. `plan/format.md` has the sweep.
- **A source's description is published prose, not metadata.** The
  `document:`/`utterance:`/`record:` value prints into the reference block of
  every document that cites the source, including one whose own rules forbid what
  it says. So it names the instrument and anything past naming goes in `reason:`,
  which no reader sees. `kg scan` warns over 240 characters; `authority` is
  exempt, because a description carrying the holding is what a reader wants there.
- **Citation is an attach concern.** Prompts carry opaque `[[source-id]]` tokens;
  `kg attach` resolves them into footnotes, before hashing. Ref placement, role-on-first-mention,
  and `-analysis.md` companion handling stop being rules an LLM can violate.
- **One idiom per relation, enforced by the parser.** Two ways to write the same
  fact means two `sem_hash`es and a phantom delta.
- **kgraph never calls an LLM.** It is a bookkeeping tool, not a generation
  tool: no API key, no model config, no network. `kg render` prints a prompt and
  a token; the agent generates; `kg attach` takes the artifacts back. `kg extract`
  is the same contract in the read direction. Everything kgraph
  does is a reproducible function of files on disk, so `status` and `diff` are
  testable and trustworthy — the one nondeterministic step lives outside.
  Unattended use is a shell pipeline (`kg render | claude -p | ... | kg attach`), which
  matches how `~/life/automations/` already works.
- **kgraph is the only writer; the agent is the only generator.** The LLM returns
  content and never writes an output file or the managed block. Both writes
  happen inside `kg attach`, together. Three guards make this hold: the managed
  block carries a **self-hash** (a hand-edited block is disbelieved, not
  believed — a false-fresh document is worse than a stale one); `kg attach`
  verifies a **render token** binding the content to the graph state it was
  written from; and undeclared output paths are refused.
- **The daemon does not regenerate.** It watches, rebuilds the graph, and
  reports what went stale. A human or agent decides what to do about it.
- **Semantic slug ids, not `f-0142`.** Diffs stay readable. `cid` is the content
  address (kind + normalized body, NOT the source document) and `same_as:` retires
  a duplicate id while keeping it resolvable.
- **Every live fact rests on a source, conclusions included.** A conclusion's
  premises and authority are what `inference` + `derived_from` record. Unattested
  means unauditable, unreachable by drift, and unweighable by `conflict`.
- **Only a person may rule a document silent** (2026-08-29). `unsupported` is
  the only verdict that SUBTRACTS a citation, and `Miscited` makes a fact whose
  every citation died an error at render and attach. A machine can honestly
  report that a string is or is not in a text layer; it cannot report that a
  document does not SUPPORT a proposition, and absence from a text layer is not
  absence from the document — so an unsigned or machine `unsupported` is kept,
  reported, and never honoured. `Attestation.Subtracts()` is the single place
  that decides.
- **The evidence itself is versioned.** `.kgraph/sources.lock` records each
  document's hash; drift is a spec state, deliberately NOT part of `sem_hash`.
- **Auth is per-peer, not per-listen-address.** Off-box callers need a bearer
  token; loopback does not, so local `kg` and the MCP shim are unaffected.
- **An index is a closed graph; `.kg-index` declares one** (2026-07-27). A corpus
  discovered by walking up to a `.git` root takes in every sibling project, and a
  monorepo of unrelated matters then becomes one graph. That corrupts computed
  state rather than merely adding noise: `disputed` is computed from conflicting
  evidence, so a foreign matter can mark a claim disputed; and `sem_hash` covers
  incident edges, so a person appearing in two matters can carry edges across the
  seam and produce phantom deltas. **Verification is in-index, always.** Nearest
  marker wins; an empty marker takes its directory's name; unmarked files form the
  default index, so a corpus that predates this keeps working. Rejected: a
  path-boundary marker, which stops the *walk* but not the *entanglement*, since
  the shared entity legitimately belongs to both subtrees.
- **Closed computation, open query — but crossing must be explicit** (2026-07-27,
  USER). Reading across indexes is wanted and is safe, because a query result is
  an observation that never feeds back into computed state. The safety is in the
  explicitness: an unqualified query resolves in its own index and cannot reach
  outside it. The failure that motivated all of this was exactly an unqualified
  query — `event[at>2026-03-01]`, no scope — silently spanning two matters and
  putting one person's medical facts into another matter's attorney packet.
- **A dialect is an index's vocabulary plus a set of rules; `legal` is the
  default** (2026-07-27, USER). The legal-specific surface was ~50 lines —
  `classRank`, `ClassAuthority`, `speakerRequired`, then in `node.go`, now fields
  on a `Dialect` value — plus `impeached`. Everything else is domain-neutral, so
  naming that layer costs little and makes "general engine, legal defaults"
  checkable rather than rhetorical. The four-part shape (ordinal ladder · one
  out-of-ladder *governs* class · a self-interested rung needing an attributed
  party · a source-invalidation flag) held unchanged across clinical, audit,
  engineering and historical evidence models, so a dialect is **data**, not code.
  Rules are opt-in/out per index at `error`/`warn`/`off` — the same split as the
  hooks: only checks whose findings are always genuine errors are non-negotiable.
  `legal` is the default so no existing corpus migrates.
- **A corpus authors its own dialects; the binary ships defaults** (2026-08-29,
  USER). The list was closed in the binary, and the reason given was that a
  hand-authored ladder with its rungs in the wrong order corrupts `disputed`
  silently. That argument did not survive being looked at: closing the map never
  checked the uncheckable part — whether `measured` truly outranks `meeting-note`
  in somebody's trade — it only limited who was allowed to get it wrong, at the
  cost of a feature with **no user-visible effect at all**, since `legal` was both
  the only entry and the default. Authored in `.kgraph/dialects/*.yaml`
  corpus-wide, and in `<index-dir>/.kgraph/dialects/*.yaml` per project where they
  are **qualified by their index** (`acme.engagement`). Resolution is nearest
  wins, the rule the marker already uses. Every guard the built-ins get runs over
  a file, as an error naming it. What replaced the closed map is not one thing but
  three: total load-time validation, a name that means one thing corpus-wide, and
  the fact that editing one is VISIBLE.
- **`*.kgraph.md` is retired** (2026-09-01, USER). Stripped of prompt, outputs and
  its managed block, a spec was `Path · Purpose · Index · Queries[] · SpecHash` —
  a named query with a purpose, which is a second way to declare what
  `standing.yaml` declares. Two idioms for one thing is the failure this format
  refuses everywhere else, so the spec goes and `kg ask` is the only door. The
  survey says this is safe downstream: **caselit touches no spec symbol at all**
  (`Spec`, `ParseSpec`, `FindSpecs`, `SpecSuffix`, `Pin`, `SetHash` — zero
  references). It is NOT free in the corpus: 21 fence-dispute specs and 1 billing spec
  become standing queries, and `aw4` had planned to bind its `README.md` and
  `plan/plan.md` to specs.
- **No materialised node/edge tables. A `kgraph_query` table, yes** (2026-09-01,
  USER). The reasoning that killed `kgraph_node`/`kgraph_edge` does not reach
  this one, and the difference is what makes it safe: a node table is a
  PROJECTION OF THE FOLD and can drift from it silently, whereas a standing
  query's pin is AUTHORED — somebody acknowledged this answer, at this time, and
  signed it — so the table is a record, not a derivation. The one computed
  column, whether it is invalidated, is cheap and self-healing: recomputing it
  cannot disagree with the fold, only lag it. It exists for DOWNSTREAM USERS:
  caselit and aw4 both want "is this answer still good?" without folding the log
  and re-evaluating in memory.
- **Editing a dialect flags documents, and it stays out of `SemHash`**
  (2026-08-29). `dialect_hash` in the render token and the managed block, reported
  as `dialect-changed` — the `style.md` mechanism, for the `style.md` reason. A
  class *rename* is a content change on the source node and should flag in
  `SemHash`; the DECLARATION must not, or switching dialect re-flags every fact in
  the matter. A built-in hashes to the empty string, so a corpus that declares
  none records exactly as it did before dialects were data.

## Active work

Reconciled 2026-09-01 (second pass, after a long session). Everything finished
is in `plan/done.md` — including, from this session: the caller-named store, the
`idx` discriminator and `kgraph_query`, the retirement of `*.kgraph.md`, the
daemon's index resolution, `kg answer` closing its question, per-source locking,
the party-name scrub and its guardrail, and the migration of the live corpus.
Earlier work is there too — parser, query
engine, spec/managed contract, render/attach, diff, CLI, daemon, MCP, console,
hooks, source lock, citations, auth, extraction, indexes, dialects (now authored
as data, `format.md` §4), attestation, the web viewer, variants, and the
caller-named store location. `plan/icebox.md` holds the deferred opt-ins.

Corpus (`~/life`, the litigation index), measured 2026-09-01 after migrating it:
**2116 nodes · 3853 edges · 289 standing queries · 0 errors · 619 warnings.**
It had been DARK since the parser was deleted — 64 unread fact files, an empty
graph, every declared query resolving to 0 rows — and the *750 nodes* this line
used to carry was a false reading, not a stale one. Both live indexes are
migrated, verified in both directions, and their specs converted; see `done.md`.
The `*.kfacts.md` and `*.kgraph.md` files are still on disk because prose around
their blocks did not come across.

Consumers, surveyed 2026-09-01: **caselit** (library, 50 symbols — touches no
spec symbol, so the retirement did not reach it), **aw4** (CLI only,
`engineering` dialect; its workflow doc still plans to bind documents to specs
and needs telling), `.claude/skills/resolve-annotations`. `go vet` and `go test`
clean 2026-09-01. **kgweb/`webui` was the fourth consumer and was retired
2026-09-04** — see `plan/split.md`.

### ✅ THE TRIAGE PHASE — all six items built (2026-09-01/02)

**Outcome, measured on the live corpus: 615 findings → 208 outstanding**, every
one of them nameable, tunable and acceptable; the attestation queue ranked from
1,197 to 143; a corpus that goes dark now fails instead of reporting success.
Detail per item below; everything here is done.

Framed 2026-09-01 after the live corpus came back, and it is a change of
direction rather than another feature: **kgraph's detection is finished; its
triage was never started.** Every checker works — that is what this whole session
kept confirming. What does not exist is any notion of *"I have seen this and it
is fine"*, and without it a check stops being a guardrail and becomes wallpaper.

Measured on the live corpus, 2026-09-01:

| signal | count | reading |
|---|---|---|
| scan warnings | **615** | 425 are `source referenced by nothing` — **425 of 729 sources, 58%** |
| citations awaiting sign-off | **1,197** | `--used` narrows to 1,106: **92%**, so the narrowing filter no longer narrows |
| of those, quote-mismatch `!` | **14** | the highest-value signal in the corpus, at 1.2% density |
| duplicate pairs | **~47** | the checker's own advice: "better fixed by pattern than one at a time" |
| suppression mechanism | **none** | no accepted state anywhere |

**Why this is the priority and not more checks.** The design's central safety
property is that an agent's facts land in a HUMAN attestation queue rather than
in the record unchallenged — that is what makes an LLM-driven producer safe to
point at a legal corpus. A queue of 1,197 a person must sign is not a queue, it
is a wall, and a safety property nobody can work is decorative. The 14
quote-mismatches are precisely the cases where a machine read something the
document does not say; they are the reason the mechanism exists, and they are
buried.

#### ✅ 1. A findings ledger — `accepted.jsonl` (built 2026-09-01)

**The precedent is already here**: `sources.lock` is "I have seen this version of
this document." An acceptance is the same act on a finding, and takes the same
shape.

- **Identity is a CONTENT hash, and the sem_hash of every node it names goes
  in.** That is what makes acceptance safe: accepting is accepting THIS
  SITUATION, so any edit to the facts involved yields a different key and the
  finding returns on its own. No separate staleness check, no stale acceptance —
  the same discipline as source drift, reached the same way.
- **Per-index, beside `sources.lock` and `attestations.jsonl`.** Findings name
  node ids and sometimes document paths, so the lock-placement rule applies
  unchanged: a project gitignored as medical PII must not have its filenames
  recorded in a shared history.
- **Append-only, last entry per key wins** — `attestations.jsonl`'s discipline,
  for its reason: two machines may record independently and neither may be lost.
- **Only a PERSON may accept, and a machine acceptance is kept, reported and
  never honoured.** Exactly `Attestation.Subtracts()`, one level up: an agent
  that could silence the corpus's own complaints is the whole hazard.
- **Errors can never be accepted.** A dangling reference gets fixed. Only
  warnings are acceptable, and that line is not negotiable.
- ✅ **BUILT 2026-09-01.** `accepted.go`, `Diag.Check`/`Diag.Key`,
  `Project.Triage`, `kg accept <key>`, `kg scan --all`.
  **526 of 615 live findings now carry a key** — `unreferenced-source` 407 and
  `split-contradiction` 119. A diag with no `Check` is simply not acceptable and
  always shows, so forgetting to give a check an identity costs noise, never
  silence.
  Keyed on the two ENDS of a contradiction rather than the set carrying one, so
  renaming a query or regrouping declarations does not retire a ruling about the
  same pair of facts.
  Verified end to end on the live corpus: accepting one took it 615 → 614 with
  `1 accepted (--all to see them)`.
  `TestAnAcceptanceDiesWhenTheFactItNamesChanges` is the one that matters — it
  accepts a finding, edits the fact it names, and requires the finding BACK.
  Also pinned: a machine acceptance is kept, reported and never honoured; an
  error can never be accepted; an unsigned acceptance is refused.
- ✅ **Every finding is identified (2026-09-02): 614 of 615 carry a key.** Nine
  more checks named and keyed — `same-file-sources`, `unsourced-fact`,
  `long-description`, `interested-no-speaker`, `inference-no-premises`,
  `already-held`, `alias-ambiguous`, `empty-query`, plus the near-duplicate pairs.
  Until this, 64 findings could be neither accepted nor tuned: they printed on
  every scan forever with nothing anyone could do about them, which is the
  wallpaper problem surviving in the tenth of the checks that never got
  identified.
  **The one remaining keyless finding is correct and documented**: the
  near-duplicate OVERFLOW notice ("22 more not listed") is about the LISTING, not
  about facts. A key would let somebody accept "there are more of these", which
  silences a count rather than a claim.
  Both guards earned themselves again while doing it — the registry test demanded
  a check the CLI emits (it scanned only the root package), and the reachability
  test found FIVE newly-named checks that no fixture triggered.
  **Two exemptions, each naming its covering test**: `empty-query` and
  `already-held` are emitted by the CLI rather than by a load — the first because
  resolving a declaration needs a clock and `LoadIn` has none, the second because
  it searches transcript TEXT and running it in `LoadIn` would make every
  `kg query` pay for a full-text sweep.

#### ✅ 2. Per-index rule levels — the class-level half (built 2026-09-01)

`error` / `warn` / `off`, and it is what the ledger must NOT be used for:
accepting 407 findings one at a time is absurd, and accepting them as a class is
disabling the check, which belongs in the index's own declaration where a reader
sees it rather than inferred from the volume of a ledger.

**Two places, and they layer rather than compete.** A dialect carries the
sensible default for its domain (`rules:` in `.kgraph/dialects/*.yaml`); an index
says what it does differently (`rule.<check>: off` in `.kg-index`). Nearest wins,
the rule the marker already uses.

**The marker half was not in the original design and it had to be.** Rules were
built on the dialect first, and the live corpus immediately showed the problem:
it binds the BUILT-IN `legal` dialect, so tuning one check would have meant
forking an entire evidentiary ladder to hang it on — and a forked ladder that
drifts from the built-in is exactly the silent corruption the closed dialect map
existed to prevent.

- **Deliberately NOT in `dialect_hash`.** The ladder is hashed because it decides
  which of two conflicting sources wins and therefore which rows come back. A
  rule level changes what is REPORTED and no row anywhere, so flagging every
  document over it would be a false alarm.
- **An error is never lowered.** A rule may raise a warning or silence it, never
  declare its way out of an error — the same line the ledger draws, and the two
  are the class-level and per-finding halves of one mechanism.
- **A rule naming no check is refused at load**, listing what would be accepted.
  A mistyped rule otherwise does nothing at all and "I turned that off" becomes a
  belief rather than a fact — the mistyped-hook failure, which has its own test
  for the same reason. `TestEveryEmittedCheckNameIsRegistered` guards the
  registry in BOTH directions: a check nothing registers cannot be tuned, and a
  registered check nothing emits is a rule that can never match.
- **Applied LAST in the load path**, where every diagnostic has converged. The
  first attempt applied them mid-way and silently exempted `split-contradiction`,
  the second-largest class in the corpus.
- Verified on the live corpus: `off` took it 614 → 208 warnings; `error` turned
  the same 407 into errors and made `kg scan` exit 1; a typo was refused by name.

#### ✅ 3. Rank the attestation queue by consequence (built 2026-09-01)

`--used` was the narrowing axis and it died the moment 289 standing queries
covered the corpus — it kept 1,106 of 1,197. `Graph.RankPending` replaces it with
bands, **in the library so the shell, the daemon and any consumer agree about
what "worth doing first" means** — a second ranker with its own idea of urgent is
how two surfaces come to disagree about one corpus.

    14   quoted words absent from the document        ← start here
    129  the fact is contested, so conflict reads it
    626  a query resolves it and it has ONE citation
    337  a declared query resolves the fact
    75   the fact's only citation
    16   routine

**`kg attest --todo` now shows 143 instead of 1,197**, ordered, with the rest
counted and one `--all` away.

- **Two orderings were wrong before this one, and the corpus said so.** `sole
  citation` placed second held 780 of 1,197 — most facts here have exactly one
  citation, so it discriminated no better than the axis it replaced. Then cutting
  at `sole-and-used` left 769 showing. **Both were fixed from measurement rather
  than from the shape of the list.**
- **The residue is a fact about the CORPUS, not the filter**: 626 facts are
  resolved by a query AND rest on a single citation. No ranking shrinks that —
  only citing more documents does. Worth naming as a corpus-health number in its
  own right.
- **next**: the same treatment for `kg quotes`, which is where the 14 go.

#### ✅ 5. Quote verification could not see two thirds of the corpus — 2026-09-03

`kg quotes` reported `unreadable` for 192 of 303 quotations, which looks exactly
like a corpus whose evidence was never transcribed. The transcriptions existed.
Three separate defects, each hiding the next:

| | | present | unreadable |
|---|---|---|---|
| start | | 97 | 192 |
| raglit config found one level down | corpora configure raglit per PROJECT | 181 | 76 |
| a document raglit does not hold is read from disk | the fallback had gone unreachable | 238 | 16 |
| pleading line numbers judged per page | see below | **245** | **16** |

- **The second was mine, caused by fixing the first.** The disk fallback sat in
  the branch taken only when a corpus had NO index name. That was invisible while
  the name never resolved — every document fell through and the fallback did all
  the work. Resolving the name made it unreachable and cost 76 quotations their
  verification, mostly case law kept as markdown, which a corpus's `include`
  patterns need not cover. Pinned by
  `TestADocumentRaglitDoesNotHoldIsStillReadFromDisk`.
- **The pleading stripper failed on a whole-document ratio.** A filing is six
  numbered pages of declaration and thirty unnumbered ones of exhibits; the
  exhibits outvote the pleading (97 numbered lines against 1635) and nothing is
  stripped. Line numbering is a property of a PAGE — judged over a page-sized
  window now. Its whitespace class was also ASCII, and a layout-aware VLM indents
  with U+00A0.
- **`near` means LOOK, not FIX, and that is the finding worth keeping.** Of 19
  near misses: ~5 were pleading line numbers, ~5 OCR misreads where the FACT is
  right and the transcription is wrong (`rotted`→`potted`, `asbuilt`→`asphalt`,
  `cert`→`cut`), ~5 genuine one-word typos in the fact, ~4 needing eyes.
  **Batch-correcting facts to match the document would have corrupted a legal
  corpus** — `lot-12-was-contracted-for-but-never-conveyed` reads `Lots 12 to 16`
  against a document's `13 to 16`, and the PDF says 12. The fact is right. No
  looser matcher may hide that, which is why the fix went to page geometry rather
  than to the string rules.
- **residue**: the last 16 `unreadable` cite sources with NO document at all, so
  there is nothing to check against. Honest, but it is a different condition from
  "have a document, cannot read it" — see icebox.

#### ✅ 6. The workbench became usable — 2026-09-03

`kg verify` was a ruling surface with three defects that each hid the next.

- **It signed as a machine.** `LastSigner` is "who wrote the last line", and one
  agent run left the human workbench signing `agent-vision`. Every verdict would
  have come straight back through `PendingHuman`, and `unsupported` refused
  outright — a session that discards itself. `LastHumanSigner` now, and
  `ServeVerify` refuses a machine identity rather than collect work it will throw
  away.
- **It could not scroll.** `body` declared grid columns and no rows, so the
  implicit `auto` row grew to content: 1538px in a 429px window, clipped by
  `overflow:hidden`, keyboard legend at y=1462. `minmax(0,1fr)` plus `min-height:0`.
  Also had no viewport meta at all.
- **36% of the queue had no viewable evidence.** `/api/doc` rasterised a PDF and
  handed everything else to an `<img>` — 337 of 926 held documents, every .md,
  .docx, .txt, .eml and .csv. Non-images come back as text from `docText` now,
  and the pane is chosen from `VerifyItem.Kind` decided by the SERVER, so the two
  halves cannot disagree about what a .docx is.

**Phone use is a hard constraint, not a nicety** (USER, 2026-09-03): raising the
soft keyboard costs half the screen, and the half it takes is the page image. So
`attested`, `illegible` and `unsupported` are thumb buttons and only `corrected`
may summon a keyboard — its note IS the correction, so it cannot be keyboard-free
by definition. `prompt()` is gone; the sheet is capped in `dvh`, which tracks the
keyboard where `vh` does not. Marking the proof moved to pointer events (it was
mouse-only, so it did not exist on a touchscreen) with an explicit mark mode,
because a finger drag is also how you scroll and the two cannot both own it.

#### ✅ 7. `Supported by` — the write path to a model that was already there

**USER, 2026-09-03: "There should be multiple edges to evidence, and the edge
should carry the rational."** It already can. Edges have identity, and `Because`
is the per-membership justification, inside `SemHash`.

**979 facts carry `attested_by`. Zero use the mapping form. Zero carry a
`because`.** Not because nobody had reasons — because nothing could author one:
the workbench wrote verdicts only, and `kg amend` read stdin as a plain string,
so the field could only ever be set to ONE id.

- `kg amend` now takes YAML, **falling back to the raw string on a scalar**. That
  fallback is load-bearing on a legal corpus: parsing scalars turns `0640` into
  416, `2024-06-06` into a timestamp and `~` into nil. Structure is the only
  thing a string cannot already say, so structure is the only thing the parse may
  add.
- `POST /api/cite` merges citations, carrying existing ones forward EXACTLY as
  authored — rewriting an untouched bare id into mapping form would move its
  sem_hash and flag every document that renders the fact.
- **Citing creates review rather than skipping it**: each new pair returns
  through `PendingHuman` for its own ruling.
- **next**: registering a NEW document as a source, in the WORKBENCH. `kg assert`
  already does it from the CLI — the earlier note here saying two documents
  "cannot be attached yet" was wrong.

#### ◻ The 26 `absent` quotations are two structural decisions, not 26 errors

Examined 2026-09-03. `absent` is the strongest band — words in quotation marks
that are not in the cited document — and 18 of the 26 have the words in
ANOTHER source the corpus holds, which reads like 18 mechanical recitations. It
is not.

- **6 point at `s-icloud-bundle`**, which is a whole export, not an instrument.
  The cited instruments are a certification inside the 38-page Kessler filing, a
  `.jpeg` scan, and a PSA envelope — the text is readable in the bundle's copy
  and not in the instrument's own transcription. Reciting a recorded plat
  certification to "the iCloud bundle" would be worse practice than leaving it.
  **These are the `anchor:` backlog re-reported through a different check.**
- **~5 are one thing recorded twice.** `s-po-renewal-hearing` and
  `s-hearing-transcript-—-h19-440-renewal` are the same hearing as
  `.verified.md` and `-transcript.md` — and `~/CLAUDE.md` says the `.verified.md`
  IS the evidence. `s-broker-log-2021-legal` against
  `s-communications-sally-seller's-agent` is the parked broker-log decision:
  three of these are exactly the "split the messages out, or cite the bulk
  export" call.
- **~7 look like genuine miscitations** — `s-summit-survey` quoted for language
  that is in `s-af-399591` and in `s-1993-quitclaim`.
- **8 are nowhere at all.** Three of those cite documents that DO have readable
  text (.md, .txt), so the quotation itself is wrong or is a paraphrase.

- **✅ 2026-09-03: `found_in` was misdirection, and that was the systematic
  half.** It returned the alphabetically FIRST document containing the words and
  the CLI printed "cite that, or say why not". Recitation is the normal state of
  this material — measured containment: one plat certification's language is in
  **22** of the index's documents, and five separate facts were each pointed at
  one arbitrary member of that set. `FoundCount` now reports how many, and the
  band splits **5 with exactly one home · 13 recited**.
- **The near-miss summary was telling a person to corrupt the corpus**: "correct
  the quotation to what the document says", when most near misses here are OCR
  defects and the FACT is the accurate one. Both summary lines are findings now,
  not instructions.
- **The hearing pair is not a judgement call.** `~/CLAUDE.md` already says
  oidio's `*.verified.md` IS the evidence, so `s-po-renewal-hearing` is
  canonical and the hand `-transcript.md` source is referenced by nothing. But
  the fix is NOT a recitation: the fact quotes the hand transcript
  (`there is the anti harassment`) while the audio says `there's the
  anti-harassment`, and the verified rendering carries its own ASR glitch
  (`anti-harassment. order`). Fact and evidence are both wrong, and the evidence
  half is oidio's to correct.
- **next**: the 5 single-home absents, of which 2 are the `s-icloud-bundle`
  degraded-artifact case and 1 is the hearing above — so roughly 2 are genuine
  candidates. The remaining 13 need no action.

#### ✅ 4. Liveness — built 2026-09-01, and it catches the original failure

`highwater.json` per index, `Graph.CheckHighWater`, written by `kg scan` after a
CLEAN scan and by no load. Reproduced the historical failure — a corpus with a
mark, no store and no export — and it now reports an error and exits 1 where it
previously scanned clean at 0 nodes.

- **The mark lives in the CORPUS, not the store**, and that placement is the
  whole design: the commonest way to load nothing is to find no store, so a
  watermark kept inside one would be missing in exactly the case it exists to
  detect.
- **It only goes up.** A count that could fall would record the collapse it is
  meant to report, one run later, and agree with it forever.
- **Zero is an ERROR and is deliberately neither tunable nor acceptable** — no
  `Check`, no `Key`. Every other check describes the corpus; this one says the
  corpus is not being READ, so every other answer in the run is worthless. A
  warning would sit in a list of 614 others.
- **A partial drop (>half) is a tunable warning**, because `kg retire` folding
  duplicates and a matter shedding a theory both do this honestly.
- **Written only after a clean scan.** Recording a count from a run with errors
  would pin the corpus at whatever a broken load produced.
- **The bug worth remembering**: the first version located its mark with
  `IndexDirOf`, which derives the directory from the NODES' file paths — so an
  index that loaded nothing reported the repo root and never found its own
  watermark. **The check failed in exactly the case it was written for**, and
  only trying it against a reproduced dark corpus showed that; the unit tests all
  passed. `IndexHomeOf` walks for the marker instead.
  `TestLivenessFindsItsMarkFromTheMarkerNotTheGraph` pins it.

#### ✅ 5a. Batch duplicate triage — `kg source dupes` (2026-09-01, corrected 09-02)

The near-duplicate checker's own overflow message has asked for this since it was
written: *"a corpus with this many is better fixed by pattern than one at a
time."* Clustering is what turns a list of pairs into a list of decisions.

**On the live corpus: 47 pairs → 44 clusters over 99 nodes**, ranked, each
printing the `kg retire` line that would resolve it.

- **Transitive**, because a person rules on a THING rather than on a pair: three
  entries for one survey produce three pairs, and asking three times whether the
  same document is the same document is how a triage pass stops being done. The
  largest live cluster covers 5 entries in one decision.
- **Two sources of pairs, folded together, and that is not tidying.** The
  near-duplicate checker compares TEXT; a separate check finds sources registered
  against the SAME FILE — one instrument entered more than once, no judgement
  required. They lived in different checks in different places, so **the cheapest
  decisions in the corpus were scattered through the hardest ones.** Ranked
  together, the 7 certain ones come first. Before folding them in, the
  top-ranked band never fired at all.
- **Exact duplicates stay out**: identical text is a hard ERROR, and an error
  gets fixed rather than triaged.
- **Suggests keeping the most-cited member**, because that is the id references
  already resolve through — a suggestion, since only a person can say two records
  are one instrument.
- **CORRECTED 2026-09-02, and the correction matters more than the feature.**
  Same-file clusters were called "the unambiguous case — one instrument entered
  twice" and printed `kg retire` lines for all of them. Working the queue showed
  the opposite: of seven, **five are COMPOUND DOCUMENTS** — a declaration with its
  exhibits, an e-sign envelope holding Form 21 and Form 35, a scan holding a
  contract and its certification. One held a 1947 record, a 2008 plat
  certification and a 2023 declaration, and the tool advised merging them.
  The signal that separates the two readings the underlying checker has ALWAYS
  offered: two entries for one instrument agree about WHEN and WHAT it is; a
  filing and its exhibit cannot. Differing date or class ⇒ compound ⇒ the advice
  becomes `anchor:`, never merge.
  **Empty is not disagreement**: one dated entry and one undated is a missing
  value, and treating it as a difference marked the corpus's only genuine
  duplicate as compound.
- **A cluster ruled NOT duplicates is accepted through the ledger**
  (`CheckNearDuplicate` has a key), so it stops reappearing until the facts move.

**This also un-parks variants option C**: the measurement that parked it was ~47
pairs, and what it actually needed was 44 decisions with the certain ones first.
Work the queue down, then count the genuine same-instrument groups that remain —
that count is what C was always waiting for.

#### ✅ 5b. Postgres — built and verified against a server (2026-09-02)

`OpenPostgres(dsn, idx)`, wired into `OpenStoreRef` for `postgres://`. Nearly
free, as designed: the store holds the LOG rather than the graph, so there is one
append-only table and the evaluator stays in memory. **The schema needed no
change at all** — the portability rules on it (TEXT holding JSON, RFC3339 strings
rather than native timestamps, an explicit `seq` rather than AUTOINCREMENT, no
upsert) were written for exactly this and held.

- **Everything engine-specific is at one seam**: the driver, `pgRebind` turning
  `?` into `$1`, and the fact that a server has no file permissions to set.
  `migrateIdx` is SQLite-only — `PRAGMA table_info` has no Postgres equivalent,
  and a Postgres table is created with `idx` from the start.
- **`MAX(seq)+1` is a RACE here and is not one under SQLite**, whose store caps
  itself at one connection — that cap is what made the read-then-write exact.
  `PRIMARY KEY (idx, seq)` turns a lost race into a constraint violation rather
  than a silent overwrite, and `Append` turns the violation into a re-read.
  **Verified by removing the retry: 8 concurrent writers then lose assertions.**
- **The index must be TOLD.** Unlike a file store there is no filename to derive
  it from, so a shared database given no index would have one matter silently
  reading another's log.
- **Tested against a real server, not a mock.** A whole fixture corpus imported
  into Postgres, loaded, and compared to the same corpus loaded from its text:
  99 nodes, identical by `SemHash`. Set `KGRAPH_TEST_POSTGRES` to a DSN to run
  them; without it they SKIP, saying so, because a green suite must not be
  mistaken for a tested engine.

### ✅ Phonetic matching in `kg have`, and where BM25 does NOT go (2026-09-02)

**BM25 was tried in near-duplicate detection and MEASURED AS A WASH**, so it was
reverted rather than shipped. On the live corpus, IDF-weighted Jaccard against
plain Jaccard: 22 of 25 pairs identical, and the 3-for-3 swap cut both ways — it
correctly dropped a memorandum/reply pair sharing boilerplate, and wrongly
dropped a `-2`-suffixed pair that is almost certainly one document.
**The reason is specific and worth keeping**: these descriptions are short and
templated, so what distinguishes two entries is often a VERSION NUMBER — a rare
token. Raw IDF gives that one differing token enormous weight in the denominator,
so a near-identical pair scores LOW. That is backwards for duplicate detection.
Compressing the weights (sqrt) recovered most of it and still measured neutral.

**And free-text search is not kgraph's to rank.** `contentHits` delegates to
raglit (`client.Search`); kgraph only re-scores the returned snippets. BM25
belongs there, not here.

**What IS kgraph's own data is names**, so that is where the work went: a
`sounds` tier in `kg have`, below every exact tier and above free text.

- **Scoped to names and NEVER to identifiers.** `soundsLike` refuses anything
  containing a digit, because a near miss on an auditor's file number is a
  DIFFERENT INSTRUMENT.
- **The phonetic key is a candidate FILTER, not the verdict.** A consonant
  skeleton collides by construction — `Barlow` and `Borley` both code to `b64`
  however long the key — so a per-word edit-distance floor of 2 confirms. Caught
  by its own test, which had asserted the opposite.
- **It slides over a phrase**, because that is the only form real data takes: an
  alias is `Bruce Harrow (the surveyor)`, not a bare name. Before this the tier
  never fired on the live corpus AT ALL — a misspelled surname returned nothing
  against a corpus holding that surname.
- Verified live: a misspelling now reaches the correctly-spelled instruments,
  marked `sounds` so a reader can see the match was made by ear; three different
  surnames and a near-miss file number still return nothing.

### ◻ Cross-index query — the three semantics calls are answered; nothing is built

An unqualified query is confined by construction: the graph has nothing else in
it. What does not exist is a way to DELIBERATELY cross. Resolved 2026-09-01 —
each answer follows from an invariant already settled rather than from taste.

1. **Syntax: `@across(a, b)`, a query-level qualifier.** The spec-header option
   (`indexes: [a, b]`) died with `*.kgraph.md`, and a per-token prefix
   (`index:other/entity`) is wrong for the same reason a spec header was: it lets
   crossing be acquired by editing one token deep inside an expression, where a
   reader skims past it. The absence of crossing is the safety property, so the
   ask must be at the top and must read aloud — "across a and b".
2. **A crossed result is a TAGGED TUPLE `(index, node)`, never merged nodes.**
   Merging recreates the entanglement the whole scheme removes: two matters'
   facts in one bag, and `disputed` computed across them. Tagging keeps the
   observation an observation.
3. **Sameness is ASSERTED, never inferred.** The open question was what makes two
   nodes "the same subject" when ids are index-local and entity text differs
   (`Imogen — a party` vs `Imogen — patient`). String similarity was the obvious
   trap and it invents facts. The answer is already this format's answer
   everywhere: **only a person may rule**, and there is **no authored
   confidence**. So a cross-index identity is an assertion somebody signs, with
   `by` and `at` like every other, and two nodes nobody has linked are simply two
   nodes. A verifier may PROPOSE candidates; it may never establish one.

- **next**: none of this is built. `@across` in the parser, a tagged result type
  through the evaluator, and a signed cross-index identity assertion.
- **risks**: flags from a cross-index comparison must never reach `sem_hash` or
  staleness. If they do, editing one matter starts flagging another's
  documents — the original bug in a new hat.
- **Cross-dialect crossing is refused outright**: ranks from two ladders are not
  comparable, and a comparison that silently ranks one corpus's `record` against
  another's is worse than no comparison.
- **why it is not urgent**: nobody has asked to cross yet. The confinement works;
  this is the door, not a fix.

### ◐ The real graph — the fence-dispute corpus, now that documents are gone

`examples/` are fixtures; the fence-dispute index is the actual work. The goal WAS that
every derived document be generated from facts. There are no generated
documents, so the goal is now that every question worth asking of that corpus is
a standing query somebody has accepted an answer to — the same aim, minus the
prose that used to carry it.

- ✅ **Migrated and converted 2026-09-01** — 2,127 assertions from 64 files, 21
  specs into 289 standing queries, verified in both directions, fact files and
  specs deleted. See `done.md`.
- **The queues are partly a TOOLING problem after all** — measured 2026-09-01 by
  classifying every absent quote rather than assuming. Of 15:
  **1 was a checker false negative** (star pagination, now fixed — see `done.md`),
  **4 cite the wrong document** and the correct one is findable mechanically,
  **10 are genuinely absent** and want a person. So a third of that queue was not
  corpus work at all.
  ✅ **BUILT**: an absent quote is now looked for elsewhere in the index BEFORE
  being reported, and the finding names the document that does contain the words.
  Live corpus: 4 of the 14 absent are located, matching the manual
  classification exactly. Scoped to the index's declared SOURCES rather than a
  filesystem walk — a walk reaches another matter's evidence, which is the
  crossing this format refuses, and `documents/` is a gigabyte. A CANDIDATE, never
  a correction: two instruments can legitimately quote one passage, so the
  citation is untouched and a person moves it.
- ✅ **17 standing queries re-pinned 2026-09-02.** Every one of their deltas was
  verified to consist of ONLY the 13 questions added the day before — nothing
  else moved, checked as sets rather than counted — which makes re-pinning
  bookkeeping rather than judgement. Signed `agent-triage`, a machine identity,
  so the record says plainly that a machine re-pinned them. `kg ask ack` now
  REQUIRES `--by`; it was the one write here that did not.
- ❓ **The 4 located quotes are TWO decisions, and none of them is "move the
  citation".** Investigating each rather than applying the tool's suggestion is
  what showed it — the design's "a candidate, never a correction" earned itself
  on first contact.
  0. ✅ **And a third cause turned up when I stopped assuming: 5 of the 14 are
     NEAR MISSES, not absent.** A quotation one word out — `impute` where the
     opinion says `imputes`, `save` where a transcript says `saved`, an added
     "NO." in a recording reference — reported identically to a passage the
     document does not contain at all. Opposite findings with opposite fixes, and
     both sent a person to read a thirty-page scan. `kg quotes` now reports
     `near`, prints the passage, and names the differing words. **14 absent → 9
     absent + 5 near, each a one-line correction.**
  1. **Three facts cite an individual broker-log message while the words are in a
     bulk thread export.** The cited file is a real message from the same thread
     on the same day; the quoted message was never split out into its own file.
     So this is ONE decision about corpus structure covering three facts —
     finish splitting the thread, or cite the export — and not three
     investigations. **USER's call.**
  2. **One must NOT be moved.** It cites the `.verified.md` transcript, which is
     what `CLAUDE.md` says a fact must cite, and the words are only in the RAW
     one. The difference is a contraction: the verified rendering says "there's
     the anti-harassment" where the quotation says "there is the anti
     harassment". So the fact's quotation was normalised when authored, and
     "fixing" it by moving the citation would swap a human-verified transcript
     for an unverified one — the opposite of the rule. **The quotation should be
     made verbatim instead.**
     Worth knowing beyond this fact: oidio's verified rendering can differ in
     WORDING from the raw transcript, so any quotation taken from the raw text
     will fail against the file the corpus requires citing.
- **and then the rest of the queues**: work what the tooling produces. `kg attest --todo` shows 143 where a wrong verdict changes what the
  corpus concludes, 14 of them citations whose quoted words are absent from the
  document. `kg source dupes` shows 44 clusters, 7 unambiguous. 17 standing
  queries report `results-changed` and want a human read then `kg ask ack`. And
  ✅ **ANSWERED 2026-09-02 (USER): the matter catalogues its documents**, so a
  source nothing cites is the normal state there, not a defect —
  `rule.unreferenced-source: off` in its marker. **614 warnings → 208.**
  Declared rather than accepted one at a time, and that distinction is the point:
  it says "this check does not apply to this corpus", in the index's own
  declaration where a reader sees it, instead of silencing 407 separate claims in
  a ledger and leaving the reason to be inferred from the volume. This is what
  the class-level door was built for.
  **This is the shift worth naming: for most of this work the TOOL was the
  constraint. It is not any more.**
- **pattern worth naming**: three sources carried `doc:` pointing at a *reading*
  of a document rather than the document, or carried no `doc:` while facts rested
  on them. `kg extract` reports the second case as `thin`; it cannot see the
  first.
- **lesson**: `about: the-case` is a catch-all and behaves like one. Anchoring
  substantive admissions to it leaked them into a conflict-check whose prompt
  forbade case theory. Anchor a fact to its actual subject.
- **risks**:
  - 30 facts rest on no source (warnings, not errors). The warnings are the
    backlog, not noise.
  - `q-where-does-rfa-54-come-from` is still open:
    `rfa-54-never-refused-utility` carries the whole no-prescription argument,
    and the served set contains no RFA 12. Recorded `unsupported` against
    `s-rfa-responses`.
  - 38 markdown links between documents were broken before any of this and stay
    broken. `kg source repair` fixes `doc:` citations, not prose links.
- ✅ **party names — SCRUBBED 2026-09-01 (USER: scrub, then keep pushing).**
  The entry this replaces claimed *"the repo has no remote so nothing has
  leaked"*. Both halves were false: `origin` is
  `git@github.com:IodeSystems/kgraph.git`, `main` tracks it, and `origin/main`
  sat at `a243aec` **pushed 2026-08-03** with ~30 name-carrying files in it. The
  repo is PRIVATE (unauthenticated API returns 404), so this was off-box, not
  public. The count was also low — 29 tracked files, not 10.
  **What was done**: every real party, witness, firm and county name replaced
  with an invented one in the fixtures' own register — 47 files, 259 lines, tests
  green. `kgweb`, a 28 MB compiled binary that was TRACKED and pushed with the
  names in its embedded strings, is untracked and gitignored: a binary cannot be
  scrubbed by editing text.
  **The two that a rename alone got wrong**, because renaming them produced
  something plausible and broken rather than something private:
  `.claude/skills/resolve-annotations/SKILL.md` named a real matter directory and
  now takes `$MATTER` from the operator; `bin/dev` named a real host and now
  takes `$KGWEB_PUBLIC_HOST`, printing the hint only when it is set. Neither was
  ever dialled — both were message text — so nothing behaves differently.
  **NOT done, and it is deliberate**: the pushed history is untouched. Rewriting
  it is a force-push over a shared remote and its own decision; the old names
  remain in commits up to `a243aec` until somebody makes it.
  **The direction underneath stays settled**: party identity is attestation data
  and belongs in raglit — `raglit/plan/attest.md` § *Attestation PARTIES*. The
  scrub buys a pushable repo now; it does not remove the reason the concept has
  to exist there.
  **Enforced, not remembered**: `TestNoPartyNamesFromTheLiveCorpus` derives the
  names from `~/life` at run time and skips where that corpus is absent, so it
  stores none. It found twelve the manual pass missed. Details in `done.md`.

### ◐ Provenance backlog — 13 unsourced facts, now TRACKED as questions

**Re-measured 2026-09-01 on the migrated corpus: 13, not 36** — the corpus grew
and largely fixed this itself while it was unloadable. Earlier: 18 were fixed by
recording reasoning as `inference` + `derived_from`; drift reach went 106 → 124.

**2026-09-01 (USER): the 13 are now questions in the graph, and the facts stay
linted.** Both, deliberately. A scan warning is transient output nobody owns; a
question is IN the graph — ownable, answerable, and it shows up in
`question[status=open]` where the work actually gets picked up. The lint stays
because the fact is still unsourced and should keep saying so; the question is
the ledger entry, not a substitute.

Each is anchored `about:` the fact it concerns rather than at a catch-all — the
lesson `about: the-case` taught. Split by what would close it:

- **6 `needs: analysis`** (`q-authority-*`) — legal propositions wanting
  authority: the two insurance-coverage rules, survey-as-professional-reliance,
  the no-adverse-tie standard, the pro-se fallback (which NAMES RCW 2.44.060 in
  its text while no source node carries it), and the effect of a global release.
  **I must not invent citations**, which is why these are questions rather than
  guesses. The naming follows the corpus's own established `q-authority-*`
  pattern — seven already existed.
- **7 `needs: evidence`** (`q-source-*`) — observations wanting the document they
  were read from. One is sharper than the rest: the power pole rests on RFA 12,
  and the served set contains no RFA 12 — the same gap
  `q-where-does-rfa-54-come-from` already records from the other end.

**The loop demonstrated itself on the way through.** Adding them moved
`attorney-packet.open` 214 → 227 rows, and `kg ask` reported
`results-changed` on 17 standing queries, naming each entering row and warning
that *"every numeral and every 'both'/'all three' in this section is suspect"*.
`kgraph_query` carries the same 17 with their reasons.

- **next**: the 17 flagged answers want a human read, then `kg ask ack`.
  **Not automatic, ever** — an answer that re-pins itself on being read reports
  fresh forever, and a tool that clears its own alarm has none.
- **risks**: none to the code. This is corpus completeness and it is now VISIBLE
  in two places rather than one.

### ⏸ Variants — option C is PARKED on a measurement that came back wrong

`plan/variants.md` has the design. Option E (findings surfaced at scan time from
raglit's rulings) is built; option C (a group node per instrument) is not, and
the open call was "is C worth its measured one-time churn of **4 source nodes'
`SemHash`**".

**Measured 2026-09-01 on the live corpus, and the number is not 4 — it is ~47.**
25 possible duplicate pairs listed plus 22 more, alongside separate "same file
registered twice" findings. E was supposed to NAME the handful of sources C would
group; instead it named a backlog.

- **The decision is therefore answered, in the direction of not yet.** C is a
  structure for a handful of known variants, not a way to work through fifty
  unreviewed pairs — and the checker itself says so: *"a corpus with this many is
  better fixed by pattern than one at a time."*
- **resume condition**: the duplicate backlog is triaged — `kg source dupes`
  now exists for exactly this and puts the 7 certain clusters first — and the
  surviving genuine same-instrument groups are counted. If that count comes back small, C
  is decided on its original terms. If it stays large, C was the wrong shape.
- **capacity it waits on**: mine, and it is a corpus-authoring job rather than a
  code one — each pair is a human judgement about whether two entries are one
  instrument.

### ✅ Wired to `~/life` — both halves answered (2026-09-01)

The ask was "pick ONE project that is not the litigation matter, because that one
shaped the format and surfaces format gaps rather than workflow gaps."

**Already done, and the plan had not noticed.** The billing dispute is a declared
index, migrated, and loads clean: **52 nodes · 76 edges · 7 standing queries · 0
errors · 0 warnings** — the only index in either corpus with no warnings at all,
which is what a small well-formed corpus looks like.

**And the second half is answered by practice**: kgraph's files live in `~/life`
itself, not a sibling repo. `.kg-index`, `standing.yaml`, `sources.lock`,
`attestations.jsonl` and the JSONL export all sit with the project they describe;
only the database is outside, at `~/.kgraph/<index>.db`, for the Syncthing reason.
`facts.jsonl` is gitignored — the store is authoritative and carries `by`/`at`, so
git was never the history.

**Next candidates, when a third is wanted**: eleven other projects exist and none
is declared. `get-a-job` and `property-management` are the two with live
decision-making in them.

## Assumptions made

- Repo-relative paths throughout; repo root = nearest ancestor with `.kgraph/`
  (raglit's `DiscoverHome` pattern), else the git root.
- `~/life` is the first consumer but nothing in the design is life-specific.
- SQLite via `database/sql`, matching raglit.
