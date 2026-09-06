# The repo split — where the orchestration goes

> Sketch. Status: **proposed, nothing moved.** Read `plan/plan.md` for current
> state. Global rules in `~/CLAUDE.md`.
>
> Prompted by BrainAPI (Lumen-Labs/brainapi2): a knowledge-graph memory layer
> whose four-agent pipeline — Scout, Observations, Architect, Janitor — writes
> the graph, with an event-centric append-only model and answers returned as
> traceable paths. The parts worth taking are the attributed append-only history
> and the swarm; the part that cannot come here is the swarm WRITING.

## Four layers, and the one rule that orders them

```
  harness           roles, a task DAG, when and why to run       ← NEW REPO
     │ imports
     ├── agentkit   the tool-call loop, compaction, lifting, backpressure
     ├── kgraph     the store, the query, the ladder, attestation
     └── raglit     retrieval, transcription, copy/version rulings  (over HTTP)
```

**The dependency arrow never reverses.** kgraph must not import agentkit, and the
reason is its hardest invariant: *kgraph never calls an LLM — no API key, no model
config, no network*, which is what makes `status` and `ask` reproducible and
testable. That rule was deliberately written about kgraph, not about kgraph's
consumers, so a harness that calls models and writes through kgraph breaks
nothing. A kgraph that could call one breaks everything downstream of it.

Each boundary already exists and is already load-bearing:

- **agentkit says so itself.** "What agentkit does not own is orchestration —
  roles, a task DAG, scheduling, when/why to run. That's your harness's job.
  agentkit gives it a `Session` to drive and a few small interfaces to
  implement." The new repo is exactly that harness.
- **kgraph↔raglit is already a client boundary**, and the scar is recorded in
  `relations.go`: an earlier cut parsed raglit's `relations.jsonl` directly,
  raglit moved rulings into a database, and the parser went on returning stale
  answers with no error. Rulings are ASKED FOR over HTTP now. Parsing another
  tool's storage makes every storage change a silent break.
- **agentkit↔raglit is already bridged.** `ragnotify` implements
  `agent.DocFinder` over an MCP retrieval server.
- **A consumer importing kgraph as a library already works.** caselit uses
  `ClassAuthority`, `ClassRank`, `ClassLadder`, `Have`, `Graph`, `ParseFacts`,
  `IndexAt`, `StrongestOnDisk`, the kinds and the statuses — and **zero** of
  render, attach or spec. The harness's import list will look much like it.

## What kgraph exports to the harness

Not a new API. This is the surface caselit already uses, plus what the
assertion store adds:

| the harness needs | kgraph gives |
|---|---|
| load a corpus | `LoadIn`, `IndexAt`, `IndexDirOf`, `DiscoverRoot` |
| ask a question | `ParseQuery`, `Ask`, `Answer`, `ReadStanding` |
| know what moved | `Answer.Drift`, `Answer.Delta`, `SourceDrift` |
| weigh a source | `DialectAt`, `Dialect.Rank`, `Dialect.Ladder` |
| write a fact | `Assert`/`Withdraw`/`Correct` — see `plan/facts.md` |
| know what is unread | `Backlog`, `Have`, `StrongestOnDisk` |
| record a verdict | `AppendAttestation`, `PendingHuman` |

**The write ops are the whole reason the split is now tractable.** Until
`plan/facts.md` lands there is no way for anything but a person with an editor to
put a fact in, so a swarm would have had to generate markdown for someone to
paste. An append-only assertion log with a required `by` is exactly the interface
a machine writer needs, and it is the same one the web asserter needs.

## What moves out of kgraph

- **`kg extract`'s PROMPT.** `extract.go` does two things: it says what a corpus
  holds and what has been read from each document, and it builds a prompt telling
  a model how to write facts. The first is a fact about the corpus and stays. The
  second is orchestration wearing a library's clothes — it exists only because
  there was no other way to get a model to produce facts, and the harness is that
  way. `TestExtractRulesDoNotLeakIntoRender` retires with it, having outlived
  render already.
- ~~**kgweb, eventually.**~~ **DONE 2026-09-04, by deletion rather than by
  moving.** The gate this entry set — "not yet, it is mid-rebuild, and moving a
  thing while rewriting it is how both go wrong" — cleared from the other side.
  Carl retired the last deployment, so the rebuild had no consumer left and a
  split repo would have been the same maintenance with more ceremony.

  Removed: `web.go` `webauth.go` `webnotes.go` `websearch.go` `webtree.go`
  `webuploads.go` `webversions.go`, their four test files, `cmd/kgweb/`,
  `webui/` (36 tracked files), `plan/web.md`, `.air.toml` and `bin/dev` —
  **7,333 lines of Go** plus the pnpm UI, 21% of the non-test tree.

  **It was NOT self-contained after all**, which is worth remembering the next
  time separability is asserted from a symbol grep: `sourceKind` lived in
  `web.go` and `verify.go` used it. No exported `Web*` name leaked, so the check
  that said "clean" was reading the wrong names. It now lives in
  `sourcekind.go`, which is where it belonged — `kg verify` asks the same
  question of the same files.

  **Recover with `git checkout kgweb-final -- web*.go cmd/kgweb webui`.** The tag
  is the pre-deletion commit and exists for exactly this.

  **What it leaves behind, and this is the part that is not code:**
  `~/.local/state/kgraph/web/fence-dispute/` still holds 42 reader notes — **7 of them
  unresolved corrections from a party to the matter** — 66 documents of version
  history, and 32 issued tokens. Plain JSON and markdown, so it is readable
  without the viewer, and `.claude/skills/resolve-annotations` was repointed at
  it. Nothing can WRITE it any more, and nothing new can arrive. The one upload
  was already filed into the corpus and matches byte for byte.

## What must NOT move

- **The ladder, attestation, and conflict.** These are the product. A Janitor
  agent that validates and dedupes is not a person signing that an exhibit says
  what a fact claims, and in a legal corpus that difference is everything.
- **`SemHash` and drift.** Reproducibility is the reason kgraph is separable at
  all. The moment staleness depends on a model's output it stops being testable.

## The consequence to design for, not around

**A swarm produces a reviewed backlog, not a finished graph.** `by` distinguishes
a person from a machine and `MachineAttested` is load-bearing: a machine can
honestly report that a string is or is not in a text layer; it cannot report that
a document SUPPORTS a proposition — and as of `aa6f6b1` it may not even try, since
only a signed person may rule a citation `unsupported`.

So every fact a Scout writes arrives machine-signed and lands in `PendingHuman`.
That is correct and it is the point: the swarm's output is a queue for a person,
and the measure of the harness is how much of that queue is worth a person's
time, not how few items are in it. A pipeline whose success metric is an empty
queue will find a way to sign its own work.

## Taken from BrainAPI

- ✅ **Attributed append-only history.** Already the design in `plan/facts.md`,
  arrived at independently and for the same reason.
- ✅ **The event hub / "triangle of attribution".** Already built: `ac81919` gave
  relations identity, a bag and a standalone form, and the `attempt` subtype is
  the pattern in miniature — an attempt is an EVENT because a nested list under
  its acquisition could not answer "what have we tried this month".
- ◻ **An answer carries its derivation.** BrainAPI returns the path that produced
  each answer. `Ask` returns rows and drift; the evidence tree exists per FACT
  and `Pin.Cites` per QUERY, but nothing hands back "here is how this answer was
  reached". Cheap on top of what exists, and it is what makes an LLM-orchestrated
  layer auditable rather than merely fast. **This is the one thing to steal.**

## Open

- ❓ **Does the harness get its own name and repo, or is it caselit grown up?**
  caselit already orchestrates over kgraph+raglit for one matter type. A general
  harness and a legal one may be the same program with a dialect, which is
  suspiciously close to the argument that made dialects data.
- ❓ **MCP or import?** kgraph ships an MCP server; agentkit has `mcpmgr`. The
  harness could drive kgraph as a tool rather than a library, which buys process
  isolation and costs the type system. caselit chose import. Recommend import for
  the harness's own logic and MCP only for what a MODEL drives directly.
