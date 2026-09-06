# Variants — the same instrument, held more than once

> Status: **E is built; C is not.** Read `plan/format.md` for the format spec and
> `plan/plan.md` for current state. Global rules in `~/CLAUDE.md`.
>
> **Recovered 2026-08-29.** This doc spent a month untracked in an abandoned agent
> worktree, which `cef06ef chore: stop tracking .claude/worktrees` had made
> gitignored — one `git clean` from gone, while the work it designed landed under
> it. That is the reason it is committed rather than left beside the code it
> describes.
>
> The detection half lives in raglit and is working; this doc is about what kgraph
> should do with what it reports.
>
> **The user owns the decisions marked ❓.** They are evidentiary choices, not
> engineering ones, and guessing them is how a filing ends up citing the wrong
> copy of a deed.

## What has landed since this was written

**Option E is built** — `relations.go`, wired into scan at `scan.go:143`
(`eded9b2 feat(relations): read raglit's copy/version rulings, and say what they
mean`, `3461eac refactor(relations): ask raglit for its rulings instead of reading
its files`).

**It landed with one deliberate change from the proposal below: a RULING, not a
score.** E was drafted against `raglit similar --json`, i.e. similarity numbers.
What was built reads `raglit marks` — a person is shown the aligned pair with the
evidence attached and records COPY or VERSION, optionally naming which side
governs. That is strictly better here for the reason the doc already gives about
`overlap`: 195 of 257 pairs are `overlap`, and a legitimate quotation is
indistinguishable from a partial reproduction by score alone. A number could not
have driven a warning; a ruling can.

Two further consequences of that shape, both recorded in `relations.go`:

- **Rulings are ASKED FOR over raglit's HTTP client, never read off disk.** An
  earlier cut parsed `relations.jsonl`; raglit then moved rulings into a database
  projected from an audit trail, and the parser went on returning stale answers
  with no error. Parsing another tool's storage makes every storage change a
  silent break.
- **Unreachable is not empty.** With no raglit daemon, `Graph.Relations` stays
  nil, which means NOT LOADED and is silent — the same distinction `SourceDrift`
  makes. Reporting "no duplicates found" because a daemon was down would be the
  exact failure this doc exists to prevent.

**Two of E's three warnings exist. The third does not:**

| E's warning | state |
|---|---|
| two cited sources are copies of one instrument → phantom corroboration | ✅ `relations.go`, `RelationCopy`, `SevWarn` |
| two versions of one instrument, one superseded → facts rest on a version that no longer governs | ✅ `RelationVersion`; also warns when raglit recorded no `--supersedes` ruling |
| a fact cites a copy while a **higher-class** copy of the same instrument is held | ◻ **not built** — this is the `classRank` one |
| two copies **disagree on numbers** (`numeric_only_in_*`) | ◻ **not built** |

The two that landed key off raglit's ruling. The two that did not are the ones
needing kgraph's own evidentiary ladder, which is why they are the remainder.

**Three ❓ below were answered and are marked in place:**

- The `companions()` precedence bug — **fixed** by reordering, not by letting a
  source name its transcript (`79fb908 fix(extract): raglit's transcription beats
  a hand-written sidecar`). `transcriptSuffixes` now leads with
  `.raglit-transcription.md` (`extract.go:326`).
- `plan/format.md`'s worked example citing the fabricated `.ocr.md` as
  `class: record` — **the example was changed** (`b724a9a`), and `format.md:1129`
  records why rather than quietly deleting it.
- Phantom corroboration warns or errors — **warns** (`relations.go:208`), with the
  reasoning inline. Consistent with every other heuristic check here.

**Still open: E first or C first is now moot — E is in.** The live question is
whether C is worth authoring, and E was supposed to *find the population* it would
group. Nobody has read that output yet, which is the concrete next step.

## The problem

The same recorded instrument genuinely arrives many times: standalone from the
auditor, as an exhibit inside a declaration, reproduced in a title commitment,
photographed by a client, re-exported to PDF by a paralegal. A web upload
endpoint now takes files from clients, so the rate rises from here.

Measured on `fence-dispute-v-halloway` (233 indexed documents, 441 true pages) with
`raglit similar --all`: **257 overlapping pairs, of which 62 are strong** — 3
byte-identical, 23 duplicates, 36 one-inside-the-other. Fourteen of those 62 touch
a document the graph already cites. Two have **both sides cited**.

That second number is the one that matters, and it is what makes this an
evidentiary problem rather than housekeeping:

```
match-inside-probe  cov 0.07/0.75
  CITED  court/harrow-declaration-2023-09-05.pdf
  CITED  records/9404150061-1993-QCD-unified-bottling-to-halloway.pdf
```

A recorded quitclaim deed is reproduced inside a declaration, and the graph cites
**both**, as two independent sources. A brief that says "two sources establish X"
is then wrong about the record — it is one instrument, seen twice, and one of the
two sightings is a photocopy in an adverse party's filing. That is a
best-evidence problem and a candour problem, not a deduplication problem.

## What raglit now reports, and what it refuses to say

`raglit similar <path> --json` gives, per pair: `relation` (`identical` ·
`duplicate` · `probe-inside-match` · `match-inside-probe` · `overlap`), symmetric
`jaccard`, both `contain_*` directions, both `block_cover_*`, `matched_chars`, the
page `blocks` ("probe p1 = match p4"), the `gaps` where two copies differ in each
copy's own words, and `numeric_only_in_probe` / `numeric_only_in_match` — the
auditor file numbers, distances and bearings present on one side only.

Three things it deliberately does not decide:

- **Which copy is better.** That is `class`, and `class` is a legal judgment.
- **Whether a pair is one instrument or two similar ones.** It reports containment
  and disagreement; a lot certification that quotes a survey's legal description
  scores like a partial reproduction because it *is* one.
- **What to delete.** It has no opinion on retirement.

## The invariants that bite

- **`SemHash` covers content, incident edges, and a source's authored provenance
  — `doc:`, `class:`, `by:`, `medium:`, and `anchor:`** (`node.go:362`). So:
  - Repointing a source's `doc:` at a "better" copy changes its hash and stales
    every document citing every fact it attests. The comment on that invariant
    records a real scar (a moved corpus left documents citing dead paths and
    reporting `fresh`). **Any design here must leave the canonical source's
    hashed fields alone.**
  - Adding an edge is *also* a hash change, because incident edges are hashed.
    There is no free way to relate two source nodes. The question is how much
    churn, once, and whether it is churn that *should* happen.
- **One idiom per relation.** Two spellings of "these are the same instrument"
  means two hashes and a phantom delta. Whatever is chosen, exactly one spelling.
- **`attach` resolves `[[source-id]]` to exactly one thing**, and an unknown or
  non-source id fails the attach. A "variant" that is not itself a citable node
  cannot be cited — which sounds tidy until a fact needs to rest on the photocopy.
- **`withdrawn` ≠ `false`, and neither is about files.** Both are claim
  lifecycles. `withdrawn` flags documents; `false` does not. Retiring a redundant
  scan is neither, and reaching for `withdrawn` would flag every document that
  cited it for a housekeeping action.

## `anchor:` already covers more than expected

**Finding: "this deed as it appears on page 13 of Rowe's filing" needs nothing
new.** It is two source nodes today, and they are correctly *not* interchangeable:

```yaml
- id: s-cartwright-deed
  document: 1984 statutory warranty deed, Cartwright to Halloway (AF#8801200011)
  doc: documents/records/8801200011-1984-SWD-cartwright-to-halloway.pdf
  class: record            # recorded instrument; self-authenticating

- id: s-cartwright-deed-in-rowe
  document: The same deed as reproduced at Exhibit B of Rowe's declaration
  doc: documents/court/2023-02-lee-rowe-declaration-northwind-title-with-exhibits.pdf
  anchor: p13-14
  class: document          # a photocopy in a brief is not self-authenticating
```

Different `doc:`, different `anchor:`, different `class:` — and the differing
class is the *point*, not an inconvenience. `anchor:` is hashed, so a fact resting
on the exhibit is pinned to the exhibit.

So the sub-document problem is solved. **Three things are missing, and none of
them is a locator:**

1. Nothing says these two nodes are copies of one instrument, so they read as
   independent corroboration.
2. Nothing records that two copies **disagree** when they do.
3. Nothing helps an author notice they are citing the weaker copy.

## Options

### A — Nothing. Keep separate source nodes, distinguished by `doc:`/`anchor:`/`class:`

Zero code, zero churn, and already half right. Leaves all three gaps: phantom
corroboration stands, disagreements are unrecorded, and raglit's findings have
nowhere to land.

### B — A `variant_of` edge between source nodes

Rejected, for three reasons and any one would do.

The eleven edge types are a deliberately closed set; a twelfth costs a schema
change, a `queryFields` entry, and an evaluator change. It is **directional**,
so authoring it forces a canonical copy to be designated — which is exactly the
evidentiary judgment that should not be baked into a schema. And it is a *set*
relation wearing a pairwise costume: with four copies it is six edges, or three
plus a convention about which is canonical, which is two idioms.

### C — A group node per instrument ◻ recommended eventually, still not built

```yaml
- id: g-cartwright-deed-copies
  group: Copies held of the 1984 Cartwright statutory warranty deed (AF#8801200011)
  members: [s-cartwright-deed, s-cartwright-deed-in-rowe, s-cartwright-deed-photo]
```

Existing kind, existing edge, canonical direction (`members:` is already the
authored inverse of `member_of`). Nothing new in the schema. Checked: no rule
constrains `member_of` by node kind, so this parses today — **verify by scanning a
fixture before committing to it**, because "it should parse" is how the reversed
hops got in.

What it buys:

- "Which copies do we hold of AF#8801200011" becomes a query, not a convention.
- **The best copy is derived, never authored** — it is the member with the highest
  `classRank`. That respects "no authored `confidence`" and reuses the mechanism
  that already resolves conflicts.
- Phantom corroboration becomes checkable: a claim attested by two sources that
  are members of one group is attested by one instrument. A new advisory scan
  check, in the spirit of `CheckAlreadyHeld` — warn, never error.

**Churn, measured rather than guessed.** The corpus has 1,322 nodes and 129
`doc:` declarations over 117 distinct documents. Only **4 cited documents** are in
strong pairs where both sides are cited, so grouping today adds `member_of` to 4
source nodes. Their `SemHash` changes once; the facts they attest change; the
documents citing those facts go stale once.

That churn is *correct*. Recording that one of two "independent" sources is a
photocopy inside an adverse filing changes the evidentiary basis, and a document
that leaned on two sources should be revised. The cost grows only as uploads bring
in copies of things already cited — which is the population the upload endpoint
will produce, so it will grow.

### D — `variants:` as a list inside the canonical source node

The tempting one, and worth naming so it is rejected on the record.

One citable id, so `attach` stays unambiguous. But a fact that genuinely rests on
the photocopy — because that is what opposing counsel filed and impeaching it is
the argument — **cannot cite it**, and the class of the thing actually relied on
is lost. Worse for churn than C, not better: the variants sit inside the node, so
they are hashed into it, and every newly discovered copy restales every document
citing the canonical source. With a client upload endpoint that is a treadmill.

### E — Findings stay outside the graph, surfaced at scan time ✅ BUILT (two of three warnings)

`kg source variants` (or a scan check) reads `raglit similar --json` and warns:

- two **cited** sources are copies of one instrument → possible phantom
  corroboration, naming the claims that rest on both;
- a fact cites a copy while a **higher-class** copy of the same instrument is
  held → name the better one;
- two copies of one instrument **disagree on numbers** → name the tokens.

Zero churn, zero schema, and the knowledge lives where it is computed and stays
recomputable. It does not make variants queryable or renderable, and a human must
act on each warning.

## Recommendation

**E now. C when a filing needs to *render* the variant set.**

> E is in as of `eded9b2`/`3461eac`. The rest of this section is the reasoning
> that produced that order, kept because it is also the argument for C's timing.

E delivers all three missing things as warnings at no cost to staleness, and it
is reversible. C adds queryability and a derived best-copy, at a one-time churn of
4 source nodes today — a cost worth paying deliberately, once someone actually
needs "list every copy we hold of this instrument" inside a generated document.

Doing C first would spend staleness on 4 nodes to buy a query nobody has asked
for yet. Doing E first also *finds the population* C would have to group, which is
the right order: the check tells you which groups to author.

**Reject B and D outright.** B adds an edge type to a closed set to express a set
relation; D makes the copy that a fact may need to rest on uncitable.

## The evidentiary questions

### Which copy does a fact cite, and who decides?

**The copy the fact actually rests on, and a human decides at authoring time.** Not
the best copy automatically — a fact impeaching an exhibit must cite the exhibit,
and rewriting that citation to the certified copy would destroy the assertion.

The tool's job is to *notice*, not to choose: warn when a fact cites a lower-class
copy while a higher-class one is held, and name it. Advisory, because citing the
photocopy is sometimes precisely the point.

### Does a variant carry its own class, or inherit the canonical one?

**Its own, always.** This is the best-evidence rule and it is the reason variants
are not housekeeping. A certified copy from the auditor is `record`; the same
instrument photocopied into a brief is `document` (not self-authenticating); a
client's phone photograph is weaker still.

Inheriting would let a photocopy outrank a witness — the exact error `node.go`
records having already made once, when RCW 48.49.080 was classed `record` and sat
above every witness in the corpus. Inherited class would reintroduce it at scale.

### What does `kg verify` check against?

**The copy the fact cites** — that is what the fact asserts. And *additionally*
report when another held copy contains the quote and the cited one does not,
because that difference is a finding, not a failure: it means either the cited
copy is a bad read or the fact cites the wrong copy.

✅ **Answered 2026-08-03: the order was reversed** (`79fb908`), not the more
truthful "let a source name its transcript" — the cheaper fix won because the
precedence was the whole bug. `transcriptSuffixes` now leads with
`.raglit-transcription.md`. The description below is kept as written, because the
fabricated numbers it names are why the rule exists.

~~❓ This needs a decision, and there is a live bug behind it.~~
`companions()` (`extract.go:298`) returns the first hit from
`transcriptSuffixes = [".ocr.md", ".text.md", ".txt", ".raglit-transcription.md"]`,
so a hand-made `.ocr.md` **wins over raglit's own transcription**. For
`documents/evidence/2021-rrepsa-purchase-sale-agreement.pdf` all three exist, and
the `.ocr.md` that wins is substantially fabricated — it renders the operative
2008 record of survey's auditor file number `AF#201503110043` as
**`AF#20140415006`** (eleven digits, denoting nothing) in four places, and
`AF#201404150061` as `AF#201404050061`; it also writes "PLAT OF LARKSPUR" for
MONTBORNE and "Twp 17, Rge 5" for Twp 33 N, Rng 5E. raglit's transcription of the
same PDF has all three numbers right. `plan/format.md`'s own worked example cites
that `.ocr.md` with `class: record`.

That contradicts CLAUDE.md ("Never hand-write a transcription sidecar. Extraction
is raglit's job"), and it is a *precedence* fix, not a variant design: either
reverse the order so raglit's transcription wins, or let a source name its
transcript explicitly. Recorded here because it is the same failure this whole
doc is about, and because it is currently deciding what `kg verify` reads.

### What happens when the canonical copy turns out to be the bad one?

Neither `withdrawn` nor `false`, and the distinction is the whole answer.

- **The instrument is fine; the transcription is wrong.** The source node is not
  withdrawn — the deed exists and says what it says.
- Fixing the sidecar (re-running raglit) changes the file under the source, and
  `source-changed` already reports exactly that, flagging the documents that
  quoted it. Correct, and no new mechanism.
- What is missing is a way to say *"this transcription is known bad, do not verify
  against it."* That is a property of a **file**, not of the graph, so it belongs
  in raglit or in the `companions()` precedence above — not in a variant edge.
- **If a fact was wrong because it was read off a fabricated transcription**, then
  and only then: `withdrawn` + a corrected node + `supersedes`. That is what
  `withdrawn` is for ("we were wrong"), and flagging documents is the correct
  consequence.

### Retirement — variants are kept "until told we should delete them"

Retirement is a lifecycle on the **file**, and it must never borrow the claim
lifecycle. Three states are worth distinguishing:

| state | meaning | citable | flags documents |
|---|---|---|---|
| held | a copy we have | yes | — |
| superseded-as-evidence | a better copy arrived; this one stays | yes | no |
| deleted | the file is gone | no | via `Missing` |

- **`superseded-as-evidence` must not withdraw anything.** A worse copy stays
  citable because a citation in a *filed* document cannot be un-filed, and the
  record of what was relied on has to survive.
- **Deletion is already handled and already loud.** `doc:` dangles, and
  `have.go`'s `Missing` fires — which the comment there is emphatic about keeping
  distinct from "held".
- ❓ **Whether `superseded-as-evidence` needs to be recorded at all**, or whether
  a group plus `classRank` says it already. It probably does: the group's
  highest-class member *is* the best copy, so the state is derivable and
  authoring it would be a second idiom.

## Open decisions the user owns

- ✅ ~~**E first, or C first?**~~ Moot — E landed 2026-08-03. **The live question
  is whether to author C at all.** Its cost was measured at 4 source nodes of
  one-time `SemHash` churn, and E was meant to name which 4. Reading E's output on
  the fence-dispute corpus is the step that turns this back into a decision.
- ✅ ~~**The `companions()` precedence bug.**~~ Reversed (`79fb908`).
- ✅ ~~**`plan/format.md`'s worked example cites the fabricated `.ocr.md`.**~~ The
  example was changed and the reason recorded at `format.md:1129` (`b724a9a`).
- ✅ ~~**Does the phantom-corroboration check warn or error?**~~ **Warns**
  (`relations.go:208`). The candour argument for erroring is real but applies at
  *publication*, not in a working graph — the same line `Miscited` already draws,
  and if it is ever wanted it belongs there rather than in `scan`.
- ❓ **Whether `superseded-as-evidence` needs to be recorded at all** (see
  Retirement above). Unchanged: probably not, because a group plus `classRank`
  derives it and authoring it would be a second idiom.

## Risks and what is untested

- **`member_of` on source nodes parses but is unexercised.** No fixture uses it;
  `TestNoSpecQueryResolvesEmpty`'s lesson applies directly — schema validation
  passes things that resolve to nothing. Any group-of-sources design needs a spec
  query that returns rows before it is believed.
- **Group aggregate expressions are built for claims.** A group of sources with no
  aggregate expression may render oddly or be treated as a row; `SetHash` folds
  `Cites` in, and whether a source-group member counts as a row is unchecked.
- **raglit's `overlap` class is not evidence of anything.** 195 of the 257 pairs
  are `overlap`, and a legitimate quotation looks the same as a partial
  reproduction. Only the 62 strong pairs should ever drive a warning.
- **Detection has a floor.** Shingle matching fails below roughly one divergence
  per 49 characters of raw text (measured; see raglit's
  `TestDetectionFloorIsSharpBelowMinRunChars`). A copy worse than that — a bad
  phone photograph of a fax — is invisible to this and needs the exact-bytes check
  or a person. **A `similar` run reporting nothing is not proof of no duplicate.**

## Optional extensions, explicitly out of scope

- Rendering a variant set into a generated document ("we hold this instrument
  three times; they agree").
- Anything automatic in the upload flow. `attach`'s rule applies by analogy:
  automating a judgment recreates the false-fresh problem the self-hash exists to
  prevent. Uploads should *report*, and a person should decide.
- Image-level variant detection (two photographs of one page). raglit's image
  embeddings could do it, needs an embedder, and so cannot run in CI.
