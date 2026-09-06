# kgraph

A knowledge graph for projects whose facts keep changing, and documents that know
when they've gone stale.

**Status: early.** The library is real and tested against two corpora — parser,
graph, query engine, spec parser, render, and attach. What's missing is the
`kg` binary that wraps them, `kg diff`, and the daemon/MCP layer. The commands
below describe the shape those will take; today they are Go calls.

## The problem

A long-running project accumulates facts, open questions, plans, and timelines,
scattered across documents that all restate each other. When one fact is
corrected or a question resolves, every document repeating it is silently wrong,
and nothing tells you which ones.

A worked example, from the corpus this was built against: a demand letter sat
finished and ready to send in `letters-ready.md`. Its central premise — "the
provider directory returns no result" — had been disproven two weeks earlier. The
only thing standing between it and the mailbox was a `⛔ DEAD` note someone had to
remember to read.

## The approach

The graph is the source of truth. Documents are outputs.

A document is a **spec**: a purpose, a set of queries, and a prompt template.
Rendering resolves the queries, fills the prompt, and pins the result — every
fact id with its semantic hash, plus the set's count, order, and grouping.

When the graph changes, re-resolving and diffing against those pins says exactly
what moved, and regeneration is a **revision** — the model gets the old document,
the old prompt, the new prompt, and the delta. Not "rewrite this," but "the count
went 3 → 4 and `f-0155` is no longer the earliest."

## The loop

kgraph never calls an LLM — no API key, no model config, no network. It is
bookkeeping; your agent is the generator. That keeps every step a reproducible
function of files on disk, so staleness detection is testable.

```sh
kg status                    # which documents went stale, and why
kg diff                      # "count 3 → 4", "f-0155 is no longer the earliest"
kg render defense-plan       # the resolved prompt, and a token. no network.
#   … your agent writes the artifacts …
kg attach defense-plan defense-plan.md    # hash them, verify the token, seal
```

`render` contributes nothing of its own — no role preamble, no emission format,
no scaffolding. The prompt is your `## Prompt` section with facts interpolated.

`attach` records that those artifacts came from that resolved state. It refuses
an undeclared path, and refuses a token that no longer matches — a stale token
means the graph moved while the model was writing, and the artifact would be
pinned to facts that had already changed.

The managed block carries a self-hash, so an agent that "helpfully" edits it to
mark a document fresh is detected rather than believed. A false-fresh document is
worse than a stale one.

**`--token` is a global flag, so it goes BEFORE the subcommand**, and putting it
anywhere else fails with an error about the wrong thing:

```sh
kg --token $TOKEN attach defense-plan defense-plan.md   # right
kg attach --token $TOKEN defense-plan defense-plan.md   # WRONG
kg attach defense-plan defense-plan.md --token $TOKEN   # WRONG
```

Both wrong forms report that a path is **"not declared in `## Outputs`"** — the
flag and its value have been taken as positional arguments, so `attach` is
looking for an output called `--token`. The message names a real rule and the
wrong cause, which costs a while if the spec's `## Outputs` section is where you
start looking.

The output path is also literal: give it exactly as `## Outputs` resolved it,
repo-relative from the kgraph root. Neither a path relative to the project
directory nor an absolute one is accepted.

## Two file kinds, found by glob

No registry, no index to keep in sync.

```
<anywhere>/*.kfacts.md    facts and relations   (authoritative)
<anywhere>/*.kgraph.md    document specs
.kgraph/index.sqlite      derived, disposable
```

Text is authoritative and SQLite is a rebuildable cache — so `git blame` works on
a contested fact, and nothing breaks when the directory syncs across machines.

### Facts

````markdown
```kfacts
- id: s-nettle-call
  utterance: Settlement call with Ivo Nettle (plaintiff's counsel)
  by: ivo-nettle
  medium: call
  recorded: true
  at: 2026-03-14
  class: admission          # a statement against the speaker's own interest

- id: aug4-cliff
  claim: Aug 4 is a filing cliff
  status: withdrawn         # we were wrong — this flags every document
  attested_by: s-nettle-call

- id: no-pending-deadline
  claim: No pending deadline is set against the Quills
  status: asserted
  supersedes: aug4-cliff
  requires: q-written-rep
  attested_by: s-nettle-call
  undercut_by: s-no-written-confirmation   # ⇒ computed `disputed`
```
````

Facts are immutable: a correction is a new node plus `supersedes`, never an edit
in place.

**Provenance is a node, not a string.** "Opposing counsel conceded it on a
recorded line" and "she recalls it that way" are both sources, and a generated
document must not word them alike. Sources carry an ordinal evidentiary class —
`record`, `admission`, `document`, `observation`, `statement`, `interested`,
`inference` — which ranks and is defensible, unlike a hand-typed `0.7`. They're
also hashed, so a re-OCR'd document flags every fact it attests as *re-verify*,
which is not the same as *the fact changed*.

### Specs

Four sections, and they mean four different things. **`## Purpose` states what
the output must achieve** — the durable one, and the only part of a spec that has
to survive a rewrite. **`## Query` gathers the facts. `## Prompt` says how to
write them up** and is expected to be replaced as the corpus improves. **`##
Outputs` names the files.** Purpose and Prompt both reach the model, Purpose
first; a prompt rewrite therefore cannot lose the intent it was serving.

````markdown
## Purpose
One table saying which piece of ground each parcel letter denotes, on which
sheet, held by whom, under which instrument. A lookup, not an argument.

## Query
```kgraph
blockers: action[status=open] -requires*-> #g-ready-for-wave @now sort valid_until, id
gating:   question[status=open] <-requires- (claim|action) @now sort id
clocks:   event[at>now] sort at
```

## Prompt
Blocking items ({{blockers.count}}):
{{blockers}}
…

## Outputs
- defense-plan.md
````

Citations render themselves. The prompt carries opaque `[[source-id]]` tokens and
the renderer substitutes per output — a markdown ref, an anchor link, a LaTeX
footnote. Reference placement and role-on-first-mention stop being rules a model
can violate.

## The vocabulary is small on purpose

Eleven binary edges: `causes` `implies` `requires` `prohibits` `supports`
`contradicts` `answers` `supersedes` `member_of` `about` `attests`.

Everything else that looks like it wants to be an edge isn't:

- **Group operators** — `all`, `any`, `>= 2`, `sum(value)` — are one aggregate
  expression on a group node. `and`/`or`/`not`/`k-of-n` fall out; no arity-wrong
  edges.
- **Temporal words** — `until`, `while`, `between` — are qualifiers on nodes and
  edges. An edge that lapses truncates the paths through it.
- **Switch** is a question with declared `options`, which buys exclusivity
  checking and the report that matters most to a planner: *there is no plan for
  the branch where X holds.*

Keeping the vocabulary closed is what keeps queries closed-form.

## Sets, not just members

Prose commits to facts *about* a set — "three items block settlement", "the
earliest is the ALTA commitment". Those belong to no single node and go wrong
when the set changes. So the pin records the count, the declared sort, and the
group cardinalities, yielding delta classes beyond added/removed/changed:

| class | what breaks |
|---|---|
| `cardinality` | every numeral, every "both" / "all three" |
| `ordering` | narrative sequence, "first" / "then" |
| `grouping` | document sections appear or vanish |
| `boundary` | superlatives — "the earliest", "the biggest risk" |

Quantities and dates can be **computed** rather than restated: `value: "=
bill-total.value - g-fair-value.value"`, `at: "= e-eob.at + 180d"`. Correct the
EOB date and every downstream deadline moves, and every document citing one
flags. A literal wouldn't.

## Examples

`examples/` holds two hand-built graphs — 129 nodes, 193 edges, 10 document
specs — and doubles as the test corpus. They're deliberately different in shape:
one is a boundary dispute (theories, evidence, impeachment, contingent plans),
the other a billing appeal (money, derived deadlines, parallel tracks). Neither
bent the other's design.

**Both are invented.** Every person, firm, parcel and dollar figure is made up.
They were rebuilt as fixtures precisely so the repository stops carrying real
matters: a test corpus is read by anyone who clones this, and the people in a
live legal or medical file did not agree to that. The shapes are drawn from real
work; nothing identifying survived the rewrite.

Each is a separate index (`.kg-index`), so nothing resolves across the seam —
which is itself the fixture for that feature: the same person appears in both,
and must not become one node.

### A moved document is not a lost one

`doc:` is a path, and a path is not an identity. Reorganising a corpus breaks
every citation into the directories that moved — and the graph cannot tell *this
exhibit is gone* from *this exhibit is one directory over*, which is the
difference between a lost document and a tidy-up. The failure arrives exactly
when a corpus grows, which is when its citations matter most.

The lock already records a sha256 of every document's bytes, and that survives
the move:

```sh
kg source repair            # what moved, and where it went
kg source repair --apply    # rewrite the `doc:` paths, then `kg source lock`
```

It refuses to guess. A repair edits a fact file, and a citation silently
repointed at the wrong exhibit is worse than one that is visibly broken — so the
hash must match exactly, the match must be unique, and the file must be inside
the same index. Two files with identical bytes are reported as ambiguous and
left alone; a document whose contents changed is not a relocation at all, since
matching it would call a different version the same exhibit.

**A repair makes documents stale, and that is the point.** `attach` prints the
document path into the citation line, so a corpus that reorganised leaves every
generated document citing a path that no longer exists. Re-locking puts the
sources back at `ok`, which reads like the job is done — so the moved path is
part of the source's `sem_hash`, and `kg status` reports the citing documents
`facts-changed` until they are regenerated.

This is not the same signal as `source-changed`. That one is about the
document's *bytes* — re-exporting a PDF must not flag every fact it attests, and
still doesn't. What is hashed is the *authored* provenance: `doc:`, `class:`,
`by:`, `medium:`. All of it is rendered into the artifact, and none of it moves
on a re-ingest. Reclassifying a `record` as `hearsay` is the same class of
change: it alters the `[[id|class]]` token in the prompt and the ordinal rank
that decides which claim comes out `disputed`.

## Build

```sh
go test ./...
```

Go 1.26. See `plan/format.md` for the full format and query-language spec, and
`plan/plan.md` for current state.
