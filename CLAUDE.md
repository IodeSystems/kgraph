# kgraph — working instructions

Knowledge-graph CLI/daemon/MCP. Facts and their relations are the source of
truth; documents are **generated** from query-bound prompt templates. When a fact
moves, `kg diff` reports which query results changed and which documents need
revising.

Read first: `plan/format.md` (the format + query spec, including `.kg-index` and
dialects in §4) · `plan/plan.md` (current state, active work, settled decisions).
Global rules in `~/CLAUDE.md` apply.

## Invariants — do not relax these without saying so

These are load-bearing. Most are enforced in code; breaking one silently
corrupts staleness detection, which is the entire product.

- **One idiom per relation.** Two ways to write the same fact means two
  `sem_hash`es and a phantom delta. Inverse keys (`attested_by`, `members`,
  `options`, `undercut_by`, `answered_by`) are stored in **canonical direction**.
  The parser rejects inverted forms; do not "helpfully" accept both.
- **`disputed` is computed, never authored.** It comes from conflicting evidence.
- **No authored `confidence`.** A hand-typed `0.7` is an invented statistic and
  `~/life`'s own document rules forbid fake probabilities. Weight derives from
  the source's ordinal evidentiary class.
- **No polarity.** `not` is `status: false` plus the negative edge types
  (`prohibits`, `contradicts`). Deliberately cut; do not reintroduce.
- **`withdrawn` ≠ `false`.** `withdrawn` = we were wrong, flags documents.
  `false` = tested and rejected on purpose, kept so `prohibits` has a target,
  **never flags documents**. Conflating them churns every doc that renders a
  theory.
- **`SemHash` covers content, incident edges, and a source's AUTHORED
  provenance.** Never add file position or a document's byte-state (`DocHash`,
  drift) — a re-ingest or a re-exported PDF would then false-flag every document,
  which is what `source-changed` exists to report separately. Never drop incident
  edges — a relation change would then go unflagged. `doc:`, `class:`, `by:`,
  `medium:` ARE hashed: they are parsed from the fact text, so no re-ingest moves
  them, and every one of them is rendered into the artifact — `class` twice over,
  as the `[[id|class]]` token and as the rank that resolves a conflict. A moved
  corpus used to leave documents citing dead paths and reporting `fresh`.
- **A pin covers what a section RESTS ON, not just its rows.** `Unresolved` (open
  questions) and `Cites` (the sources its rows cite) are both folded into
  `SetHash`. A source is never a row, so without `Cites` its path and class could
  change under a finished document and nothing would notice.
- **Sequence is not dependency.** Groups carry `ordered`; forcing priority into
  `requires` invents blocking that isn't real.
- **`while` gates transitively.** A theory scoped to an unasserted premise must
  never surface in a `@now` query.
- **The managed region is never parsed back in.** Everything below
  `<!-- kgraph:managed` is generated output.

## Writing — who is allowed to

**Facts live in an assertion store**, not in files: `~/.kgraph/<index>.db`, one
append-only table, outside the corpus. Everything goes in through `kg assert` /
`withdraw` / `correct` / `answer` / `retire`, each of which folds the log it
would join, builds the result, and **refuses anything that would not load**. A
UI or an agent that could write a graph the CLI refuses to load is one that
corrupts a corpus. `kg export` is the text half and lives in the corpus.

**`by` is required on every assertion**, and a machine identity is recorded as
one — see `MachineAttested`. An agent's facts land in the human attestation
queue rather than in the record unchallenged, which is what makes an LLM-driven
producer safe to point at a legal corpus. See `plan/facts.md` and
`plan/split.md`.

## The generation loop — RETIRED 2026-08-29, kept as rationale

**kgraph never calls an LLM.** No API key, no model config, no network. It is
bookkeeping, not generation, and that is what makes `status`/`diff` reproducible
and testable. **This rule outlived generation and is the reason the split in
`plan/split.md` works**: it was written about kgraph, not about kgraph's
consumers, so a harness that calls models and writes THROUGH kgraph breaks
nothing — while a kgraph that could call one breaks everything downstream.

**Everything below describes render/attach, which are gone.** It is kept because
the reasoning is what the write path inherited, not because any of it still
runs.

```
kg render <spec>            resolved prompt + token, on stdout
   agent generates and writes the artifacts itself
kg attach <spec> <path>...  hash them, verify the token, seal the managed block
```

- **`extract` is render's inverse and every rule inverts with it.** It supplies the
  format rules (there is no author's prompt to resolve) and it does put the
  document path in the prompt (the document is the input). Neither contradicts the
  render rules below — they are the two ends of one pipe, and
  `TestExtractRulesDoNotLeakIntoRender` keeps them apart.
- **`render` adds no instructions of its own.** No role preamble, no emission
  format, no scaffolding. `TestRenderAddsNoScaffolding` enforces this. The
  prompt is the author's text plus resolved facts.
- **The corpus's house style is the one instruction block render interpolates,
  and it is the AUTHOR'S.** `.kgraph/style.md` holds the reference, tone and
  do-not-say rules every generated document in the corpus must obey; `render`
  appends it after the author's `## Prompt`. This does not relax the rule above,
  and the distinction is the whole of it: **kgraph ships no default style and
  writes no sentence of it.** A built-in style WOULD be scaffolding. Same shape as
  `.kgraph/queries.md` — one conventional path, absent is fine, named explicitly
  in the fingerprint because the walk skips `.kgraph/`.
  It exists because style rules copied per-spec are held only where somebody
  remembered them: a relief document printed "do not lead with this" and then
  argued against its own fact, and the same class of aside sat in eight more.
  **The style binds the render token and is recorded as `style_hash`**, so
  editing it reports `style-changed` on every document rather than `fresh`. A
  document that reports fresh against rules written after it is claiming to obey
  something it never saw. It lives on `Graph`, not `Env`, because `State` has to
  see it and `State` has no `Env` — same reason as `SourceDrift` and `Attest`.
- **Never hand-write a transcription sidecar. Extraction is raglit's job** —
  including OCR, page delineation, and describing diagrams and figures. Run
  `raglit index <path>` and let it write `<path>.raglit-transcription.md`. An
  agent that types out a document's text produces something with no `## Page N`
  markers, so `kg verify` cannot say WHERE in a 30-page scan a claim lives, and
  that page hint is the whole point of the sidecar. It also silently drops every
  figure, which on a survey or a plat is most of the evidence.
- **If raglit cannot read a format, add it to raglit FIRST.** Converting the file
  by hand to route around the gap yields a second original with no provenance,
  and leaves the next person to hit the same wall. As of 2026-07-29 raglit reads
  `.pdf` · `.jpg/.jpeg .png .tif/.tiff .webp .gif .bmp .heic/.heif` ·
  `.doc .docx .odt .rtf .pptx .epub .htm/.html` · `.xlsx .xls` · `.eml .mbox` ·
  `.md .txt .csv` — `.doc` via antiword, `.xls` via xls2csv, `.heic` via
  ImageMagick/libheif, `.xlsx` natively. **Check raglit's `ClassifyDoc` rather than
  reasoning about it.** kgweb kept a hand-mirrored copy of this set and was wrong
  in BOTH directions — `.doc` stayed refused after it worked, `.xlsx` was allowed
  while nothing read it. kgweb is gone; the lesson is not. Any second copy of this
  list will drift the same way, so ask raglit rather than keeping one.
- **Documents and images are raglit's. Audio is oidio's.** oidio does the
  recording, diarization and speaker attribution, and the EVIDENCE it produces is
  the rendered `*.verified.md` transcript — that file is what a fact cites. Do
  not point raglit at an audio file and do not transcribe one by hand.
- **The prompt never contains a source file path** — only `[[source|class]]`
  tokens. A model that sees a path invents a citation format.
- **kgraph is the only writer of the managed block.** An agent that edits it
  produces a *false-fresh* document, which is worse than a stale one, so the
  block carries a self-hash and a tampered block reports `managed-tampered`
  rather than being believed.
- **`attach` refuses an undeclared path and requires a token.** Required, not
  checked-if-supplied: omitting the flag used to pin an output to facts that had
  already moved and report it `fresh` — the false-fresh state the managed
  self-hash exists to prevent, reached without touching the managed block.
- **`attach` resolves `[[source-id]]` into citations, and an unknown id fails the
  attach.** The prompt carries opaque tokens so a model cannot invent a citation
  format, so something must resolve them afterwards and attach is the only moment
  both artifact and resolved graph are in hand. Citations resolve BEFORE hashing,
  or `status` reports `output-edited` against a file kgraph itself wrote. A
  document carrying an invented citation is worse than one carrying none, so an
  id that is unknown — or is a non-source, like citing a person — is an error.
- An agent with its own file access can bypass `attach`. That degrades to
  `output-edited` — detected, not silent. Do not add guards that make it silent.

## Hooks

`kg hook install` wires five Claude Code hooks; spec in `plan/hooks.md`.

- **`guard-managed` is the load-bearing one.** It refuses edits at or below the
  managed marker. Do not weaken it, and do not add a bypass: a hand-edited
  managed block makes a stale document report itself *fresh*, which is the one
  state nothing downstream can detect or recover.
- **Only the two hooks whose findings are always genuine errors block**
  (`guard-managed`, `lint-facts`). The rest are advisory on purpose — a hook that
  refuses legitimate work (hand-editing a generated document is legitimate) gets
  uninstalled, and then nothing is enforced.
- **Every hook fails open** on an internal error. A broken hook must never wedge
  a session. The only exception is the check whose entire job is to refuse.
- **Never auto-run `kg attach` from a hook.** Attach asserts an artifact came
  from a resolved state; automating it recreates the false-fresh problem the
  self-hash exists to prevent.

## Party identity — this repo is pushed

Real party, witness, firm and county names were scrubbed out of this repo on
2026-09-01 and replaced with invented ones. **Do not reintroduce them.** A
fixture, a comment, a plan note or a test that names a real person from `~/life`
goes to a remote the moment somebody pushes, and this repo HAS one and uses it.

- Names in `examples/` and in tests are INVENTED. Keep them that way; if you need
  another, invent one in the same register rather than reaching for the corpus.
- A path or host that names a matter belongs in an env var, not in a file:
  `$MATTER` in the annotation skill. `bin/dev` carried `$KGWEB_PUBLIC_HOST` for
  the same reason and retired with kgweb.
- **Never commit a built binary.** `kgweb` was tracked and pushed with the names
  in its embedded strings, where no text edit could reach them.
- The direction this is heading: party identity is ATTESTATION DATA and should
  arrive through raglit's attestation rather than be authored into a tool. See
  `raglit/plan/attest.md` § *Attestation PARTIES* and `plan/plan.md` § party
  names.

## Testing

`examples/` is the test corpus, not decoration — two hand-built graphs from real
`~/life` projects. Run `go test ./...`.

- **Never loosen a fixture to make a test pass.** The fixtures caught four format
  bugs, six reversed hops, and two evaluator bugs. If a fixture now fails, the
  code or the format changed and the fixture is telling you.
- **`TestNoDeclaredQueryResolvesEmpty` is the highest-value test.** Parsing and
  schema validation both pass a reversed hop; it resolves to zero rows and fails
  silently. Only resolution catches it. Any new declared query must return rows.
  Renamed from `TestNoSpecQueryResolvesEmpty` and widened to cover STANDING
  QUERIES as well as specs (2026-09-01), because specs are retiring and the value
  is a property of the declaration, not of the file it arrived in. It also
  refuses to pass having checked NOTHING — that vacuous pass becomes reachable
  the moment the specs are deleted and `standing.yaml` is not yet populated.
- **`TestSemHashStableAcrossEdgeOrderAndPosition`** pins the staleness contract
  from both sides. If you touch `SemHash`, this is the test that matters.
- **`TestInstallableNamesAreAllDispatched`** — every hook `kg hook install` writes
  must be one the binary dispatches. A typo there yields a hook that silently
  does nothing on every tool call, which is worse than no hook.

## Hardening — the failure mode this repo actually has

Three guards built on 2026-09-01 passed their unit tests and failed against the
real corpus. None was a missing test; each was **a test that could not fail**,
because the fixture was written by the same mind as the code and encoded the same
assumption.

- **`./bin/check` before calling anything done.** `go vet` is not enough: one
  retirement pass left eleven dead symbols behind and vet reported none.
  staticcheck catches unused funcs/vars/consts/fields; it does NOT catch an
  unused PARAMETER, which is what `unparam` is for.
- **Never put a test corpus's index at the repo root.** Use `newCorpus(t, name)`.
  A root-level index makes "the directory this index lives in" and "the root" the
  same string, so every function that DERIVES one from the other looks correct
  however it is written. That is exactly how the liveness check shipped unable to
  find its own watermark.
- **Test through the pipeline whenever a bug could be "called in the wrong
  place".** The rules test built a `[]Diag` and called `ApplyRules` on it, so it
  chose the position and passed while the real load applied rules before the last
  two checks ran. Demonstrated: reintroduce the bug and the unit test still
  passes; `TestARuleSilencesACheckThatRunsLastInTheLoad` catches it.
- **Every registered check must FIRE.** `testdata/allchecks/` exists to trigger
  each one, kept apart from `examples/` so exercising a checker never disturbs the
  standing-query pins there. A check nobody can trigger is a rule that cannot be
  tuned and a finding nobody can accept. State checks are exempt BY NAME and must
  name the test that covers them instead.
- **A derivation that cannot tell must say so.** `IndexDirOf` returned `""` for
  both "the default index, at the root" and "I could not tell", and that
  ambiguity was the liveness bug. `IndexDirKnown` returns the confidence.
- **The real corpus's SHAPE is in the repo, and its content is not.**
  `testdata/live-profile.json` is counts and distributions only — every string in
  it is a schema key or a kind name, nothing from the corpus — so it is
  publishable where the corpus never will be. `synth_test.go` generates a graph
  to it and runs the checkers at 1,850 nodes with the live ratios: 420
  unreferenced sources, 713 of 894 facts on a single citation, 98 contradictions.
  **Re-measure it when the corpus changes materially**; a stale profile makes
  this test agree with `examples/` again.
- **Still true, and worth keeping in view**: all three failures were found on the
  live corpus, two only by REPRODUCING the failure the feature was written to
  prevent. The profile closes the scale-and-distribution gap, not the gap between
  a generated corpus and a real one — synthesized facts are uniform where real
  ones are strange.

## Conventions

- Go 1.26, flat root package, `cmd/kg`, `internal/db` via sqlc + the metaquery
  plugin at `../sqlc-go-codegen-metaquery/bin/` (mirrors `raglit`).
- Node ids are **semantic slugs**, never `f-0142` — diffs have to stay readable.
  `cid` + `alias` absorb the churn when a slug is reworded.
- Scan reports **every** problem, never stops at the first. A dangling reference
  in a legal corpus must be caught at build, not discovered in a filed document.
- Storage: **SQLite is authoritative; the JSONL is its export** (2026-08-30,
  USER — this INVERTS the old rule, see `plan/facts.md`). One machine writes, so
  the file-sync argument for text stopped applying, and history stopped depending
  on git the moment `by`/`at` went on every assertion.
  **The database is not in the corpus by default** — `~/.kgraph/<index>.db`. That
  is a POLICY a caller may override (`--store`, `$KGRAPH_STORE`, `StoreRef`),
  because keying the location on the index NAME alone means two corpora spelling
  an index the same way share a store silently. The hazard below is unchanged; a
  caller that puts a database in a synced tree accepts it knowingly. `~/life` syncs
  via Syncthing and a live SQLite file there is a corruption hazard whether or
  not two machines write it: partial page writes, and `-wal`/`-shm` out of step.
  Single writer removed the write conflict, not the syncer. What lives in the
  corpus is `kg export`'s JSONL, which keeps the property the old rule was
  really protecting: what is in the corpus directory is text.
  **The `assertion` table IS the event log** (2026-09-01, USER: "in the sqlite db,
  or in the og table"). No `kgraph_events` table — a second copy of the log could
  drift from the log, which is the same reason there are no node/edge tables. The
  exported JSONL is kept for hand-over and offline loading but belongs OUT of
  git: `by`/`at` are on every assertion, so git was never the history, and the
  export is rewritten whole every time.
  Postgres is designed for and not built. Full rebuild on watch — at 10³–10⁴
  nodes incremental view maintenance is not worth building.
- The query evaluator is **in-memory and intended to stay that way**. CTE
  lowering would be work for no gain at this scale; the AST is the contract if
  that ever changes.

## Do not

- **Do not write into `~/life`.** `examples/` are fixtures derived from it. The
  real corpus holds live legal and medical work.
- Do not add a query field without adding it to `queryFields` in `eval.go` — an
  unknown field otherwise matches nothing silently.
- Do not commit generated outputs. Specs are committed; their outputs are not.
