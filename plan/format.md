# kgraph file formats

Two authored file kinds, both discovered by glob from the repo root. No registry,
no index to keep in sync. One derived SQLite index at `.kgraph/index.sqlite`
(gitignored, rebuilt on watch).

```
<anywhere>/*.kfacts.md     nodes + edges          (source of truth)
<anywhere>/*.kgraph.md     document specs         (query + prompt + managed state)
<anywhere>/.kg-index       index marker           (membership + dialect)   — see §4
.kgraph/queries.md         named/shared queries
.kgraph/style.md           the corpus's house style, interpolated into every prompt
.kgraph/dialects/*.yaml    authored dialects                                — see §4
.kgraph/index.sqlite       derived
```

Everything under `.kgraph/` is **named by hand** in the daemon's fingerprint. The
walk that finds `*.kfacts.md` deliberately skips that directory, so a file living
there which changes answers has to be listed explicitly or the daemon keeps
reporting `fresh` after it is edited.

Facts scope by **subject**, not by document — they live under the project that
owns them, but the glob makes them globally queryable, so cross-project edges
work. Generated outputs are **not** committed (separate scope).

---

## 1. `*.kfacts.md` — nodes and edges

Authored section is compact YAML blocks. The managed section is the **review
surface**: the tool renders each fact and each logical statement in readable
form, resolves relations, and flags unresolved/contradictory structure.

````markdown
# Closing timeline — Bramble v. Halloway

## Facts

```kfacts
- id: s-larkspur-packet
  document: Larkspur closing packet
  doc: documents/evidence/larkspur-closing-packet.md
  class: document

- id: closing-aug-14
  claim: Closing occurred 2021-08-14
  status: withdrawn
  attested_by: s-larkspur-packet

- id: closing-aug-19
  claim: Closing occurred 2021-08-19
  status: asserted
  valid_from: 2021-08-19
  supersedes: closing-aug-14
  attested_by: s-rrepsa

- id: q-disclosure-timing
  question: Did Larkspur disclose the easement before signing?
  status: open

- id: g-settlement-ready
  group: all
  members: [closing-aug-19, title-commitment-discloses, q-disclosure-timing]
  requires: settlement
```

<!-- kgraph:managed — generated, do not edit -->
## Rendered

f-0142  claim · asserted · valid from 2021-08-19
  "Closing occurred 2021-08-19"
  supersedes f-0087 ("Closing occurred 2021-08-14")
  ref documents/evidence/2021-rrepsa.pdf#p14
  consumed by settlement-posture, closing-chronology

q-0031  question · OPEN
  "Did Larkspur disclose the easement before signing?"
  blocks g-0004 → #settlement
  consumed by settlement-posture

g-0004  group(all) → requires → #settlement
  ├ f-0142  asserted   Closing occurred 2021-08-19
  ├ f-0155  asserted   Title commitment discloses easement
  └ q-0031  OPEN       Did Larkspur disclose before signing?    ← blocks

## Attention
- 1 unresolved node inside an all-group that gates #settlement (q-0031)
- 0 live contradictions
- 0 orphans

```yaml
file_hash: sha256:1c40…
rendered_at: 2026-07-25T14:11Z
nodes: 4
```
````

### Node fields

| field | applies to | notes |
|---|---|---|
| `id` | all | stable, required |
| `claim` / `question` / `event` / `action` / `entity` | all | presence picks the kind; value is the body |
| `group` | groups | one aggregate over members: `all` \| `any` \| `none` \| `>= K` \| `== K` \| `sum(value)` |
| `members` | groups | ids; ordered when `ordered: true` |
| `owner` | actions, questions | entity id — who is responsible, distinct from `about` |
| `options` / `exhaustive` | questions | declared cases; enables exclusivity and unplanned-branch checks |
| `status` | all | epistemic: `asserted` \| `proposed` \| `open` \| `resolved` \| `false` \| `withdrawn`. **`disputed` is computed, never authored** — see §3 |
| `valid_from` / `valid_until` | all | world time; null = unbounded. This is `between` / `until` |
| `while` | all | node id — holds only while that node is satisfied; gates transitively |
| `attested_by` | all | source node id(s) — see §3 |
| `value` / `unit` | all | typed quantity, or a `= expr` computed from other nodes |
| `at` | events, sources | point in time; `= expr` for derived dates |
| `precision` | anything with a date | `day` (default) \| `month` \| `year` \| `approx` |
| `ordered` | groups | member order is a sequence, not a set |
| `reason` | all | why asserted/withdrawn; feeds the revision prompt |

### Quantities

A dispute is often *about a number*, and prose-only facts mean documents restate
figures that have since moved. Nodes carry `value` + `unit`, and the group
aggregate extends from counting members to summing them — the same mechanism,
not a new one:

```yaml
- id: g-fair-value
  group: sum(value)
  unit: USD
- id: fair-value-gap
  value: "= bill-total.value - g-fair-value.value"
  unit: USD
```

`= expr` marks a **computed** field. Computed values are never authored, so a
settlement target cannot drift from the line items it sums.

### Derived dates

The same applies to time, and it matters more. A deadline is almost always
*derived* — 180 days from the EOB, 100 days from the verification — and writing
the literal `2026-11-23` produces a date that silently survives a correction to
its basis.

```yaml
- id: d-appeal-deadline
  event: Billing appeal deadline
  at: "= e-eob.at + 180d"
  precision: approx
```

Correct the EOB date and every deadline downstream moves, and every document
citing one flags. A literal would not have.

Offsets take `d`, `mo`, or `y`, and terms of different units compose
(`+ 1y + 10d`). **`y` and `mo` are calendar units, not multiples of a day**, and
for a statutory period that is the whole point:

```yaml
- id: d-harrow-bar
  event: Professional-negligence period expires (RCW 4.16.080(2))
  at: "= e-routing-forwarded.at + 3y"     # 2023-08-04 → 2026-08-04
```

Written as `1095d` the same period lands on 2026-08-03, because 2024 was a leap
year. A bar date one day early is a missed claim, not a rounding error, so
`y`/`mo` delegate to calendar arithmetic instead of multiplying out. An
unrecognised unit **refuses to resolve** rather than falling back to days — a
silent reinterpretation of `3w` as three days would move a deadline three weeks
and nothing would say so. `mo` rather than `m`, because `m` reads as minutes to
anyone who has used Go durations.

⚠ **`valid_from` / `valid_until` do not take `= expr`** — only `at` does. So a
limitations clock models the bar date as a derived **event**, which moves with its
accrual anchor, and the availability window on the claim is a literal. Expression
support on truth windows is in `icebox.md`.

### Approximate dates

`precision` exists because real corpora are full of "denied around July" and
"~48.5–50 years of permissive use". Rendering `2026-07` as a specific day is a
misrepresentation, and in a legal document a costly one. The renderer is required
to honor precision: `month` renders "July 2026", `approx` renders "about
2026-11-23".

### Ordering is not dependency

`retain-surveyor` does not require `file-counterclaims`, but the strategy still
says do them in that order. Priority and sequence are not the `requires` graph,
and forcing them into it invents dependencies that then block things falsely.

Groups carry it instead: `ordered: true` means member order is the sequence
(fence-dispute's five-step course of action), `ordered: false` means a parallel bag
(clinic-billing's three tracks). No new edge type.

### `about` is an anchor, and it is layered

A census of the corpus found `about` running one way and only one way:

```
entity                          a THING. Ground — it anchors to nothing.
event   about-> entity          a HAPPENING, anchored to the things in it.
claim   about-> entity | event  a FACT, anchored to things and happenings.
claim   about-> claim           a statement ABOUT a statement (meta, allowed).
question about-> claim | entity | action
```

Zero entity anchors across 300+ nodes, so the layering is observed rather than
imposed — and now enforced. An entity `about` something would mean one thing is
"about" another, and **that relationship is a claim**
(`northwind-title-is-galeforce`), not an anchor. Allowing it makes the graph mushy in a
way nothing downstream can detect.

The anchor is separate from the relations (`requires`, `supports`, …): those say
how facts relate to each other, `about` says what a fact is *of*. That is why it
renders even under `relations: none` — a claim whose body does not name its
subject is unusable without it.

### Conditionals: three edges that are not interchangeable

`implies`, `requires`, and `while` all read as "if/then" in English and an author
will mix them unless the distinction is stated:

| | means | used for |
|---|---|---|
| `A implies B` | A is true ⇒ B is also true | inference — deriving facts |
| `B requires A` | B cannot hold unless A holds | planning — gating and blocking |
| `B while A` | B exists only in the branch where A holds | scoping — counterfactuals, Plan B |

`easement-by-necessity while: deeded-access-lost` is a theory that exists only in
a world we are not in. It must not appear in a "what is live now" query, and
`while` gates satisfaction transitively so it doesn't.

### An analysis is not a primary source

`inference` ranks 0 — below every witness — but rank alone was not enough. Three
shapes let a reading act as evidence, and a real corpus had all three:

**Unmoored.** An `inference` with no `derived_from` is a conclusion presented as
provenance. Nothing says what it was concluded *from*, so the facts resting on it
look sourced when nothing underneath them is. Now a warning; an inference must
name its premises.

**Circular.** `s-wren-analysis` derived from `wren-p46-culvert-medic` *and* attested
it — the fact supported itself by way of the analysis. Five pairs like this
existed, all from the same mistake: the `derived_from` lists named what the
analysis **concluded** rather than what it was built from. Now an error.

**Misclassed.** A source whose `doc:` is somebody's `-analysis.md` must be class
`inference`, or an interpretation outranks the record it reads.

The fix is always the same shape: **attest the fact to the primary source, and let
the analysis derive from it.** `permissive-use` was attested by the analysis that
concluded it; its real basis is the Quills' own discovery response — RFA 12,
*"always said yes to the power pole"* — which is an `admission` and outranks every
reading of it. Two client-prepared compilations classed `inference` became
`interested` documents: something we wrote is a low-ranked source for what we
asserted, not a derivation.

`kg extract` enforces the same line on the way in — a sibling `-analysis.md` is
mentioned, never inlined as the document.

### Transcripts beside a scan

A corpus that has been through an OCR pass usually has the text somewhere. `~/life`
did — raglit held indexed text for 59 of fence-dispute's PDFs, including the answer, the
SJ order and the Wren declaration, while `kg extract` was reporting them as
"not text — read it yourself" and I was reading page images.

`kg extract` now prefers a sibling transcript and says which document to cite.
Precedence is `.raglit-transcription.md` › `.ocr.md` › `.text.md` › `.txt`
(`extract.go:326`) — raglit's own transcription leads, because a hand-made
`.ocr.md` used to win over it and one of those was substantially fabricated. The text is **materialised beside the document**, not read out
of another tool's index, because a fact read from OCR rests on that OCR: the lock
should hash the text that was actually read, or a re-OCR changes the reading and
nothing notices.

**`-analysis.md` is deliberately not a transcript.** An analysis is somebody's
*reading* of a document, and inlining it as the document would launder an
interpretation into the fact record — the extracted facts would cite the exhibit
while actually resting on a prior conclusion about it. It is mentioned so it can be
consulted on purpose, never presented as the source text.

### Extraction — the read direction

Render's inverse, and every rule points the other way:

| | render | extract |
|---|---|---|
| input | the author's prompt | a document |
| output | a document | `kfacts` |
| kgraph adds | **nothing** (`TestRenderAddsNoScaffolding`) | the format rules |
| document path in prompt | **never** (invites invented citations) | **always** (it is the input) |

Both differences are load-bearing, not inconsistencies. Extraction has no author's
prompt to resolve, and the rules it needs — no authored `confidence`, `withdrawn`
≠ `false`, canonical edge direction, `about` anchoring, semantic slugs, the
unquoted-`#` YAML trap — cannot be inferred from a deed. Every one of them
produces something that *parses and is wrong* when broken.

kgraph still never calls a model:

```
kg extract              the backlog: what nothing has been read from
kg extract <doc>        the prompt, on stdout
  agent writes facts into a *.kfacts.md
kg scan                 validates; cid catches duplicates
kg source lock          records the version that was read
```

Three things make the prompt more than the document pasted into a box:

- **What is already held from this document.** Without it a second pass restates
  the first under new ids, and `cid` catches it as an error *after* it is written.
- **The open questions it might close**, ranked so the ones anchored to something
  this document already touches come first. Only `needs: evidence` questions — a
  decision is not waiting on a document, and offering it invites a fabricated
  answer.
- **A body that is inlined or explicitly not.** A binary exhibit, or one over
  200KB, gets its path instead: a truncated exhibit is worse than a pointer to a
  whole one, because facts read from the visible half look complete.

`kg extract` with no argument is the half that matters most. A document **no
source cites is invisible to every other command** — the graph cannot report a gap
it has no node for — and in a hand-assembled corpus that is exactly where unread
evidence sits. Generated spec outputs are excluded, or the backlog would grow every
time you used it.

### `underdetermined` — when the remedy is to split, not to weigh

Two sources disagreeing has two completely different remedies, and `conflict()`
always assumed the first:

| | means | remedy |
|---|---|---|
| **disputed** | a determinate question, conflicting evidence | **weigh** — at most one side is true |
| **underdetermined** | the claim conflates two things | **split** — both sides are true, of different facts |

`underdetermined: <reason>` is **authored**, and the asymmetry with `disputed` is
the point. `disputed` is computed because it *is* derivable — evidence pointing
both ways. Whether a claim is ambiguous is a statement about what the claim
**means**, which no amount of evidence reveals. Same reasoning as `needs`.

The value is the reason and it is required: `underdetermined: true` is rejected as
a shrug. It also is not a status — the claim still has a real one.

**Underdetermined wins over disputed, and that ordering is load-bearing.** Once an
author says a claim conflates two things, producing a `1 for, 1 against` tally
invites weighing sources that never disagreed, and the document then answers the
wrong question *confidently*. A tally is worse than silence there. The two query
sets are disjoint by construction.

It is a **pending defect**, not a stable state, so scan nags while it is live —
otherwise it becomes a permanent excuse: every document renders the marker and
nobody ever makes the split. The exit is the format's existing immutable-fix
pattern: two or more claims supersede it, then it goes `withdrawn`. A
superseded-but-still-live claim gets told so.

The corpus case is clinic-billing's headline number. `bill-total` is the EOB's
*professional balance*; the mailed statement may carry a separate facility line and
subtract a courtesy. **Both figures can be right** — "what is owed" is two
quantities. Weighing a record against a record there produces a confident answer to
the wrong question. `q-current-balance` was reframed at the same time: it used to
ask "what is the actual current balance", which presumes one number exists — the
same defect as `q-consult-fee`.

### The four time ranges, and which honour them

| carrier | fields | means |
|---|---|---|
| a **claim** | `valid_from` / `valid_until` | when the claim is **true** |
| a **claim or event** | `at`, or `from` / `to` | when it **occurred** |
| **any edge**, including `attests` | `valid_from` / `valid_until` / `while` | when the relation **holds** |
| an **alias** | `from` / `until` | when the name **denotes** that thing |

Truth window and occurrence are deliberately separate, and **occurrence is not a
truth window**: an event having happened stays true forever. So two records
disagreeing about *when* something happened is a genuine conflict, not a
succession — which is why `windowsOverlap` reads `valid_*` only. The partial-SJ
pair, 04-24 against 04-25, is exactly that case.

**Attestation windows are the ones that were quietly ignored.** `edgeLive` was
honoured by query hops and group membership but not by `conflict`,
`impeachment`, or `supportedClaim` — so an expired attestation kept a fact
"supported" by evidence that no longer spoke to it, and a lapsed declaration went
on impeaching forever. The whole family now takes the query's date:

```
g.impeachment("sworn", "2022-06-01")   ->  impeached, the declaration was live
g.impeachment("sworn", "2024-06-01")   ->  nothing, the attestation lapsed
```

An undated question sees every attestation, which is what a bare query means.

### A correction is not a conflict — "not always was"

The two Harrow surveys are the case: same surveyor, both class `record`, the first
omitting the railroad-strip legal and the second including it. Neither outranks the
other, so no impeachment computes — correct. But they are not simply *in dispute*
either. The first **was** the recorded depiction from March until the second
replaced it in May, and anyone who relied on the record in that window relied
correctly.

So `conflict()` is now time-aware: **two claims whose validity windows are disjoint
are never in evidentiary conflict.** Counting them as evidence pointing both ways
tells a document to weigh a fact against its own correction, and reports `disputed`
for something that simply changed. An unbounded claim overlaps everything — no
window means "as far as we know, always", which is what an undated assertion says.

Two legitimate shapes for a superseded fact, and the difference is load-bearing in
a legal corpus:

| | means | effect on documents |
|---|---|---|
| `withdrawn` | it was **wrong** | flags every document that cited it |
| `valid_until` + `supersedes` | it **was true**, then was corrected | documents citing it *in its window were right* |

Collapsing the second into the first is what "not always was" guards against: a
filing that relied on the record as it stood is not a mistake.

`checkSupersession` catches the incoherent middle — a `supersedes` whose target is
still live *and* unbounded. That asserts two incompatible things are true right
now, and a `@now` query returns both, so a generated document states the corrected
fact and the thing it corrected as equals.

The corpus now models it, and it is time-addressable:

```
@2022-04-01  the recorded survey omits the railroad-strip legal description
@2026-07-26  the recorded survey shows the strip bordering both lots
```

### The class audit — `class` is the weight of the CONTENTS

Auditing all 71 sources after `s-wren-decl` turned out to be miscategorised. The
rule that settles every case: **`class` is the evidentiary weight of what the
source says, not the filing status of the object it is written on.**

**Three real findings.**

`s-rcw-48-49-080` — a **statute** classed `record`, which put it at the *top* of
the evidentiary ladder, outranking every witness in the corpus. Law is not
evidence: a statute does not testify to a fact, it decides what testimony is
worth. Ranked alongside witnesses it can "impeach" one, which is a category
error. Now `authority`, deliberately **outside** the ladder — having no rank is
the mechanism, since `conflict` and `impeachment` look up a rank and skip what has
none. It still attests, so a legal conclusion resting on it is not unsourced.

`s-dorsey-decl` — classed `statement`, but the one claim it attests is *"Dorsey
admitted he never inspected the septic"*. That is an **admission**, against the
speaker's own interest, and `statement` undersold it by three ranks.

`s-carl-renewal` — an observation by a party of his own diligence, unattributed
and classed neutral `observation`, so it outranked a third party's statement.
Now `interested`, with a `by:`.

**Four false positives, worth naming so nobody "fixes" them.**
`s-septic-2020-pump`, `s-kestrel-workorders`, and the two Harrow surveys all carry
a speaker *and* class `record`. That is correct: a contemporaneous county or
business record is a record however it is authored, and the speaker is there so
the graph can ask which declarant authored two conflicting things. Demoting them
would destroy the impeachments that depend on the class gap. Pinned by a test.

**The check this implies.** `admission` and `interested` are both claims about
*whose* interest, so neither means anything without a `by:` — now a warning. A
`record` needs no speaker: it has an author, not an interest.

### `impeached` — a contradiction with a winner

A dispute where one side's source strictly outranks the other's, and the losing
source has a `speaker`. The finding is then not "these disagree" but **"this
person said something the record refutes."**

Computed, not authored, and every ingredient was already present: sources carry a
`speaker`, `class` is ordinal, `contradicts` says what conflicts. Nothing put them
together, so a document could report a sworn paragraph and a county record as
evenly disputed — the opposite of the point, since the whole value of an
impeachment is that one side *loses*.

It also could not have computed, because **`s-wren-decl` was classed `record`.**
A sworn declaration is a court record as an *artifact* and an interested party's
assertion as *evidence*; `class` is the second thing. Ranked 6 instead of 1, every
Wren paragraph tied with the record refuting it and no gap existed to find.
Reclassing it to `interested` turned four paragraphs into computed impeachments.

Strictly outranked, deliberately: an equal-class conflict is a dispute to weigh on
the merits. And a claim is impeached on its **weakest** support, since a claim is
only as defensible as that — otherwise adding one strong citation would hide a
weak one.

The tally now names classes for the same reason: `1 for, 1 against` reads as a tie
and invites hedging, while `1 for (record), 1 against (interested)` is not close.

The four states are disjoint and ordered — **underdetermined** (split it) beats
**impeached** (press it) beats **disputed** (weigh it) beats settled. Each is a
different instruction to the document.

Where impeachment needs something the graph does not have, it stays a question.
`s-sedge-decl` (statement) and `s-septic-2020-pump` (record) share a speaker, so
the machinery is ready — but nothing yet says what his declaration *asserts*, and
`q-sedge-declaration-substance` records that rather than inventing testimony.

### A third case: `contradicts` used for something that is not a contradiction

`claim[disputed]` exposed this. `sedge-own-finding-acceptable` was marked as
contradicting `sedge-declared-for-tess`, but **both are true**: he did declare for
the plaintiff, and his own 2020 finding did record the system as acceptable. Marking
them contradictory made both compute as DISPUTED, telling a document to hedge — and
the entire value there is that both are *established*. Hedging loses the
impeachment.

The edge was simply wrong; the impeachment matrix finds the pair by their shared
`about` anchor. The real contradiction is between what the declaration **asserts**
about the septic and his own record — and that is not in the graph, so
`q-sedge-declaration-substance` records it rather than inventing testimony.

The general trap: a claim naming a *speech act* ("Wren swore X") is true as a
speech act, so it cannot contradict the record — X can. Where a claim is written
about the filing rather than about what the filing says, `contradicts` looks right
and is not.

### Asking for contested facts — `claim[disputed]`

`disputed` cannot be authored (the parser rejects it as a status), so a query is
the only way to ask. It joins `computed` as a **computed flag**: `[disputed]`,
`[disputed=true]`, `[disputed=false]`, `[disputed!=true]`, and
`!claim[disputed]`. Ordering operators and any other value are rejected —
`[disputed=maybe]` would otherwise validate and match nothing.

Fixing this surfaced a latent bug in the older flag. A computed flag is modelled
as present-or-absent, and absence short-circuited the comparison path, so
**`[computed=false]` had always matched nothing** rather than the complement — 141
rows, silently reported as 0. Both now partition: 11 + 132 = 143 claims.

**It is not the same question as `claim -contradicts- claim`,** and each catches
what the other cannot:

- `no-pending-deadline` is undercut by a **source** (`undercut_by`). The
  claim-to-claim hop is structurally blind to it, so a document written against
  the hop never flags it.
- `strip-held-by-halloway` has a `contradicts` edge but **no source of its own**.
  One side having evidence is not evidence pointing both ways, so the hop finds
  the pair and the predicate correctly does not.

That second case is a real corpus gap the predicate exposed: the central claim of
Path ① — quiet title on the recorded deeds — cites no instrument. Rather than
attest it to whichever deed was nearest, the gap is now recorded as
`q-strip-instrument` (`needs: evidence`).

### A conclusion is not exempt from needing a source

I got this wrong first: I read the unattested set as mostly legitimate because
`defense-intact`, `root-cause` and `prefer-settlement` are reasoning rather than
things read off a page, and concluded "every claim needs a source" should not be a
rule.

Backwards. A reasoned conclusion has premises, and a legal one has authority —
which is exactly what the `inference` class and `derived_from` exist to record.
`clinic-billing` already did it that way (`s-line-arithmetic` derives a figure by
subtraction from four others); fence-dispute was simply inconsistent.

An unattested fact costs three specific things:

- nothing can audit what it rests on, so a wrong premise is invisible;
- `DriftedSources` cannot walk through it, so a moved exhibit under a conclusion
  never flags the document that renders the conclusion;
- `conflict` cannot weigh it, so a contradiction against it computes as one-sided
  and the fact reads as settled — which is what hid `strip-held-by-halloway`.

`checkAttestation` now reports every live `claim` or `event` resting on nothing,
and names the premises the graph already knows: incoming `supports` / `causes` /
`implies` edges are the `derived_from` list the conclusion wants, and for a
computed value or date the operands are. Scoped to `asserted` and `proposed` —
a `withdrawn` fact is one we know we got wrong and a `false` one is kept only so
`prohibits` has a target, so demanding provenance for either is bookkeeping about
abandoned work.

54 facts were unattested. 18 are now recorded, and the measurable payoff is drift
reach: facts any exhibit change can flag went **106 → 124 of 172**. The remaining
36 need a document or an authority that cannot be named without reading the real
corpus, and that is the backlog the check exists to surface.

### `disputed` was nearly dead

Found while testing extraction. `conflict()` counted only contradictions from
*source* nodes, so across the whole corpus `disputed` fired **twice**, off a single
`undercut_by`, while seven claim-to-claim contradictions produced nothing.

That is the more common shape by far and the important one: a deed saying the strip
is Halloway's against a survey drawing putting it in Lot I *is* the case. Rendering
either as flatly `asserted` is the exact failure the marker exists to prevent.

Now both shapes count, with the right symmetry — a source undercut is
**directional** (a source is not a claim that can be doubted), a claim-to-claim
contradiction is **mutual** (edge direction is an authoring convenience, and both
sides are equally in question). The contradicting claim must itself be live and
attested: a `status: false` strawman exists so `prohibits` has a target and must
never flag anything, and an unsourced assertion against a record is not evidence
both ways. 2 → 38 renders, over 11 distinct facts — all the impeachment pairs.

#### The mutual rule was right for `contradicts:` and wrong for `undercut_by:`

"Edge direction is an authoring convenience" holds for the key it was written
about. It does not hold for `undercut_by`, which lowers to the same edge type and
means something else: `contradicts` says two claims cannot both be true, so each
puts the other in question; `undercut_by` says the other node **beats** this one,
and the winner is not thereby in question. One edge type carried both relations,
so the direction was lost and `conflict` read the defeat relation from both ends.

Found in fence-dispute, where `sj-denied-septic-negligent` — a summary-judgment denial
read off the court's own order, the strongest fact in the matter — rendered
`DISPUTED: 1 for (record), 1 against (inference)` off one `undercut_by` authored
on the defence theory the order defeats. House rule 10 makes a status travel with
the claim, so two documents going to counsel hedged a court order.

`Edge.OneWay` now carries the distinction, set by the parser from the key and
honoured in `conflict` and `impeachment`. It is in `SemHash`, appended only when
set — corpora with no one-way edge hash exactly as before, and the nodes that do
move are the ones whose disputed status this changes, which is what staleness is
for. Swapping a relation between the two keys is an authored change to what the
graph asserts.

It never bit for the shape `undercut_by` was designed for, a source undercutting
a claim, because `conflict` returns early on a source. Only a claim-valued
`undercut_by` reached the symmetric branch — 11 of fence-dispute's 13.

Sweep over fence-dispute: **five claims came off the disputed list**, and none is a loss.
`sj-denied-septic-negligent` (the order), `quiet-title-on-deeds` (Path ①'s
doctrine), `admit-refused-to-sign-harrow-corrected-deed` (an admission),
`francis-road-in-clear-lake-was-under-water` and
`the-argument-is-demonstrated-non-cooperation-not-character`. Nothing became
disputed that was not before.

### Citations — `[[source-id]]` resolves at attach

The prompt never contains a document path, only `[[id|class]]` tokens: a model
shown a path invents a citation format. So the tokens survive into what the model
writes, and **attach** — the only moment both the artifact and the resolved graph
are in hand — turns them into markdown footnotes plus a generated definition
block:

```
The strip is held by Halloway by recorded fee deed [^s-deed].

<!-- kgraph:citations — generated, do not edit -->

[^s-deed]: Statutory warranty deed, AF 201503110044 · record · documents/deed.md · 2008-07-08
```

Four things that had to be right:

- **Resolve before hashing.** Hashing first records the unresolved artifact, and
  every later `kg status` reports `output-edited` against a file kgraph wrote.
- **An unknown id fails the attach.** A document carrying an invented citation is
  worse than one carrying none. Citing a *non-source* fails too — `[[errol]]`,
  a person, is the same defect as a nonexistent id.
- **Already-resolved refs count.** The first version derived the cited set only
  from `[[id]]` tokens, so a second attach found none, regenerated an empty list,
  and **deleted the citations it had just written**. `[^id]` references are now
  read back. An unknown `[^x]` is left alone, though — that is ordinary markdown
  footnote syntax, and an author's own footnote is not an invented citation.
- **Binary artifacts are never rewritten.** A PDF has no citations to resolve.

Hand-editing the citation block is *detected* (`output-edited`) and not blocked.
Hand-editing a generated document is legitimate work, and a hook that refuses
legitimate work gets uninstalled.

### `doc:` and the source lock — when the evidence moves

A fact is only as good as the document it was read from, and documents move: an
exhibit is re-scanned, a statement is corrected, a PDF is re-exported with
different pagination. **Nothing in the graph notices**, because the fact text did
not change — and a filed document then cites a page that no longer says what it
said.

So a `sources.lock` beside each directory of facts records the hash of each
declared document, and drift from it is reported. Four deliberate non-choices:

- **Not in `sem_hash`.** Provenance in the semantic hash would mean re-exporting
  one PDF flags every document rendering any fact it attests — the false-flag
  storm the hash exists to prevent. Drift is a separate signal, folded into a
  spec's state. `TestSourceDriftIsNotInSemHash` pins this.
- **Not in the facts themselves.** kgraph writing hashes into a fact is the wrong
  direction, and a hand-typed hash is unusable anyway. (Written when
  `*.kfacts.md` was the authoritative fact text. The store has since become the
  database — `plan/facts.md` — and the argument is unchanged: a lock belongs
  beside the evidence, not inside the assertion.)
- **Not in the fact store**, which is a corruption hazard on a Syncthing share
  and therefore lives outside the corpus entirely. The lock is text, it commits,
  and **its diff is the review event** — "this exhibit changed" is what a person
  needs to see. That is also why the lock did NOT move into the database when the
  facts did: a diff nobody can read is not a review event.
- **Not one file at the repo root**, which is what it was first. A corpus has
  mixed sensitivity: `~/life` gitignores an entire project as *"Medical PII — keep
  OUT of git history"*, and a root lock recording
  `projects/.../documents/eob-2026-05-27.pdf` would have put those filenames and
  dates into history anyway. **One lock per directory of facts inherits that
  directory's git treatment automatically** — verified with `git check-ignore` —
  which is the only arrangement that cannot leak by accident. Entries are keyed
  relative to the lock, so it reads like the `doc:` lines beside it and survives
  the project being moved. A stale root lock is reported, never silently ignored.

States, and why the last one is quiet:

| state | meaning |
|---|---|
| `ok` | locked, unchanged |
| `changed` | **the signal.** Not the document the facts were read from |
| `vanished` | was locked, now gone — the citation has no referent |
| `unrecorded` | present but never locked; which version was read is unknown |
| `not-found` | declared, never locked, not here |

`not-found` is **not a scan diagnostic**. A typo and an exhibit that lives on
another machine are indistinguishable from here, so reporting it would either
flood a corpus that keeps evidence elsewhere or bury the typo in that flood. It
shows up in `kg source status`, which is where a typo is actually visible — and
that is how two nodes citing `documents/court/`, the *docket folder*, were found.
A trailing slash names a directory, which is certain from the text alone, so it is
an error on every machine including one holding no evidence at all.

`doc:` resolves against the **declaring file's directory**, not the root. Each
project keeps its evidence beside its facts while the root is the repository above
them all; resolving against the root would send every lookup to the wrong place
and moving a project would break every path in it. Absolute and escaping paths
are refused — an authored file must not aim the hasher at an arbitrary path.

Hashes are over **raw bytes**. We cannot tell a cosmetic reflow from a substantive
edit in an arbitrary file, and the errors are not symmetric: a false flag costs
one re-read, a missed change costs a document citing evidence that no longer says
what was cited.

**Drift reaches through an inference's premises.** A document rendering only a
conclusion is flagged when an exhibit two hops beneath it moves — an inference is
only as good as the facts under it.

`kg source lock` is the **only writer**, and deliberately so: recording a hash
asserts the document was re-read. The daemon exposes `/sources` read-only, because
no remote caller should assert that on the author's behalf.

### Two ids, one file — and which keys accumulate

Two ways a duplicate hides that the near-duplicate pass cannot see, both found by
measuring the fence-dispute corpus rather than by the scanner.

**Two sources on one `doc:` path.** The similarity pass is a token-overlap
threshold at `nearDuplicate = 0.72`, and every duplicate found by hand in that
corpus across four sessions scored **0.50 to 0.62** — `s-sj-order` against
`s-sj-order-full`, two ids for the most-cited record in the matter, scored 0.56.
The cause is not a badly tuned constant. A corpus that names its sources by what
they *are* describes one instrument two ways, and two descriptions of one thing
share about half their words, not three quarters. Lowering the threshold does not
recover them either: at 0.45 the same run produces roughly sixty pairs, most of
them designed parallels (`d-bar-broker-cpa` against `d-bar-escrow-cpa`,
`s-alta-supplement-2` against `-3`).

So the check that finds them is exact and free: **two sources whose `doc:` paths
are identical.** No threshold, no judgement, and zero false positives by
construction. It is a **warning**, because several instruments genuinely live in
one PDF — a declaration and its exhibits, a certification packet. The idiom for
that is already in the format: give each source an `anchor:` naming where inside
the document it is. A group where every member is anchored, to a different place,
is silent.

⚠ **A collision is not always a merge waiting to happen.** Where the two entries
declare different source FORMS — one `record:`, one `document:` — they disagree
about what the file is: whether the corpus holds the instrument or a copy of it.
Retiring one id picks an answer silently, so the message says so and names both
forms.

**Which keys may be written twice.** A node's **edge** keys accumulate:
`attested_by:` twice means two sources, `requires:` twice means two prerequisites,
and that is used correctly and on purpose. Every key in `scalarFields` — `reason`,
`status`, `at`, `class`, `owner`, `same_as`, `doc`, `anchor` and the rest — holds
one value, and writing one twice is **last-write-wins**.

That was silent, and it cost authored text. Eleven nodes in fence-dispute repeated a key;
five were losing content, four of them in the same shape — an original `reason:`
saying why the work mattered, then a second `reason:` appended months later saying
what the answer turned out to be, with the first discarded. `kg scan` reported 0
errors on every one. A repeated scalar is now an **error** naming both lines.

⚠ The message does not propose joining the two values, and must not. Two
`status:` values that disagree is a contradiction to resolve, not a
concatenation.

### One fact, two ids — and `same_as:`

The worst thing that can happen to this graph quietly. Nothing looks wrong: both
copies parse, both validate, both render. But two ids mean two `sem_hash`es, so
answering one leaves the other open, a document rendering each gets a different
answer, and `kg diff` reports a delta for a fact that never moved.

`cid` is the content address of the fact — **kind plus normalized body, and
nothing else**. The first version folded in the attesting document, which was
wrong for exactly this purpose: the same claim reached from two sources would get
two different CIDs, defeating the merge the CID exists to find. Provenance is an
edge, not part of identity.

Detection has two tiers, because only one of them can be certain:

- **Exact CID collision → error.** Same kind, same words, twice.
- **High token overlap → warning.** Only a person can say whether two similar
  sentences are one fact. Pairs already joined by an edge are exempt: a claim and
  the rebuttal that shares its whole vocabulary are a *designed* pair, and
  reporting those would bury the accidents in noise.

`same_as: <id>` retires an id in favour of another node. The survivor absorbs the
incident edges — which correctly moves its `sem_hash`, since a merge is a real
change to what the fact rests on — and the retired id **stays resolvable**, in
queries and in references. That last part is the whole point: deleting one half of
a duplicate breaks every reference to it, and rewriting those by hand is how the
second duplicate got created. An id that has already appeared in generated output
must keep resolving.

Rejected, because they are mistakes rather than merges: a cycle, a self-reference,
and a merge across kinds (folding a question into an event erases the question).

**Two real duplicates were in this corpus.** `sol-first-pass` / `e-sol-first-pass`
— the same event in two files, and the copy also used `valid_from` where an
occurrence needs `at`. And `q-barn-parcel` / `q-which-parcel-has-barn` — the same
question asked twice, both rendering into documents as separate open items.

Near-duplicate detection is quadratic in the worst case, and that case is not
exotic — 4000 line items differing by a number. Unbounded it produced eight
million warnings in 28s and 4GB. Three bounds now apply (bucket size, a
comparison budget, and a cap on the listed pairs), and **the two that lose
coverage say so in the scan output**. A check that silently stops checking is
worse than an absent one, because the clean output gets taken as proof.

### `needs` — what would actually close a question

An open question is only actionable if you know what would answer it, and that is
not derivable from the graph: whether an answer comes from a document or from
someone deciding is knowledge *about* the question.

| `needs` | means | next action |
|---|---|---|
| `evidence` | a document exists | obtain and read it |
| `decision` | someone must choose | no amount of evidence settles it |
| `reply` | an external party must respond | chase, do not research |
| `analysis` | reasoning over facts and law | not a document to fetch |

The four are why a flat list of open questions goes unread: they imply completely
different next actions, and lumping them together makes the list unusable. The
distribution across the corpus is 14 evidence · 4 decision · 8 reply · 4 analysis.

### Two ways an open question becomes permanent

Both are now scan warnings.

**No `needs`** — nobody knows what closing it would even look like, so it is a
wish, not a task.

**Neither an `owner` nor an `about`** — nobody is responsible and it is anchored
to nothing. This caught a real one: *"What is the consult fee and the next step?"*
asked once, globally. The fee differs per firm, so a single node could never be
answered. **Per-interaction capture is not a case fact.** It belongs in the
document template that asks for it — the runsheet's EXTRACT block — and becomes a
fact once captured ("Kestrel's consult fee is $X").

The same reasoning promoted the three call screens from global questions to ones
with an owner and an anchor: "does the firm represent title companies?" is a real
question whose answer changes what we do, so it stays — but it had to say whose
job it is and what it bears on.

### Switch: a question with declared options

Exclusive branching was expressible but not *checkable* — a question with several
answer claims, each scoping a branch by `while`, with nothing enforcing that the
cases are exclusive or that every case has a plan.

A question may declare its cases:

```yaml
- id: q-denial-letter-content
  question: Does the denial letter contain the required content?
  status: open
  options: [denial-letter-clean, denial-letter-deficient]
  exhaustive: true
```

Each option is a claim that `answers` the question, and downstream work scopes to
it with `while`. The scan can then check what prose never does:

- **exclusivity** — more than one option asserted is an error
- **exhaustiveness** — an open question with `exhaustive: true` and an
  unenumerated outcome
- **unplanned branch** — an option with no `while`-scoped work hanging off it.
  *"There is no plan for the branch where the facility claim also came back
  out-of-network"* is the most useful thing this tool can say to a planner, and
  it is a graph query, not a judgment call.

### Actions need `done` and `owner`

Two planning fields the fixtures papered over:

- `status: done` for actions. `resolved` is the question vocabulary; using it for
  a completed action conflates "we answered something" with "we did something".
- `owner:` — an entity id. Both trackers separate *my* next actions from
  **decisions the other party owns** (fence-dispute: "Decisions the Quills own";
  clinic-billing: "Blocking decisions / info I need from you"). Ownership is what makes
  the decision memo — `question[status=open, owner=…]` — a two-line spec instead
  of a hand-curated list. `about:` cannot carry it; that is topical, not
  responsible.

### Occurrence time is not truth time

`at` on an event is **when it happened**. `valid_from`/`valid_until` is **when
the claim is true**. These were conflated in the first fixture pass and they are
not the same: the 2021-08-19 closing *occurred* on one day and is *true forever
after*, while "no pending deadline is set" became true on 2026-03-14 and may stop
being true.

Events that span use `from`/`to` rather than `at` — the Sept–Oct deposition wave
is an interval occurrence, not a claim with a truth window.

`status` was originally one field doing three jobs. Split:

| value | means | flags docs? |
|---|---|---|
| `asserted` | believed true | — |
| `proposed` | a theory, not yet believed (`easement-by-necessity`) | — |
| `open` | question awaiting an answer | — |
| `resolved` | question answered; see the `answers` edge | yes |
| `withdrawn` | **we were wrong** — belief revised, `supersedes` follows (`aug4-cliff`) | yes |
| `false` | **recorded as false on purpose** — a strawman kept so `prohibits` has a target (`prescriptive-easement`) | **no** |

`false` must never flag a document. A strawman is stable by design; treating it
as a retraction churns every doc that renders the theory.

Note there is **no authored `confidence`**. Writing `confidence: 0.7` by hand is
inventing a statistic. Weight is derived from the evidentiary class of the
attesting sources (§3), which is ordinal and defensible.

### Edges

Inline on the node, one key per edge type. Value is an id or list. When authored
on the *target* of an edge the key names the inverse (`attested_by`, `answered_by`).

`causes` · `implies` · `requires` · `prohibits` · `supports` · `contradicts` ·
`answers` · `supersedes` · `member_of` · `about` · `attests`

Eleven types, all binary and directed (`contradicts` is symmetric). Group
operators are node kinds, not edges. Temporal words are qualifiers, not edges.

**One idiom per relation — this is a hard rule, not style.** The same fact
written two ways produces two `sem_hash`es and a phantom delta. In particular:

- A claim or action waiting on a question is **always** `requires: <question>`.
  Never `question supports: claim`, never `question answers: claim`.
- `answers` points **answer → question** only.
- Provenance is **always** `attested_by: <source>`, never a bare path.

The parser rejects the inverted forms rather than accepting both.

An edge may carry its own qualifiers via long form:

```yaml
  requires:
    - id: "#settlement"
      valid_until: 2026-09-01
      confidence: 0.8
```

### Immutability

A corrected fact is **never edited in place**. Assert a new node, set
`supersedes` + `reason`, flip the old one to `status: retracted`. That gives the
revision prompt the causal history it needs and makes `git blame` meaningful on
contested facts.

---

## 2. `*.kgraph.md` — document spec

Default output = strip `.kgraph` (`settlement-posture.kgraph.md` →
`settlement-posture.md`). `## Outputs` is only needed when it is not 1:1.

One prompt, N outputs — a static site, a file plus generated resources, or
sources for a build step. The LLM emits text; anything binary (PDF) comes from
`## Build`.

````markdown
# Settlement posture

## Purpose
Where settlement stands, what still blocks it, what we're waiting on.
For Carl before an attorney call — not client-facing.

## Query
```kgraph
blockers: claim[status=asserted] -requires*-> #settlement @now
open:     question[status=open] -about-> #settlement
risks:    claim -contradicts- claim @now
```

## Prompt
Write a one-screen posture note.

Blocking items:
{{blockers}}

Open questions:
{{open}}

Contradictions to flag:
{{risks}}

Lead with the single thing most likely to move first. Sources at end of
paragraph, never inline. No process narration.

## Outputs
- settlement-posture.md
- site/index.html
- site/style.css

## Build
./build-attorney-packet-pdf.sh

<!-- kgraph:managed — generated, do not edit -->
```yaml
status: facts-changed
spec_hash: sha256:4f1a…          # purpose + query + prompt + outputs + build
rendered_at: 2026-07-19T10:02Z
prompt_hash: sha256:9c2d…        # prompt with facts interpolated
build: {ran_at: 2026-07-19T10:03Z, exit: 0}
outputs:
  settlement-posture.md: sha256:aa71…
  site/index.html: sha256:31bc…
  site/style.css: sha256:0d92…
facts:
  blockers:
    - f-0087@a3f1
    - f-0142@7b09
  open:
    - q-0031@9c2d
  risks: []
```
````

The managed block is **visible YAML, not an HTML comment**: sorted `id@hash`
lines mean `git diff` on the spec *is* the fact delta. Bloat buys the signal.

### `## Build`

A spec may declare a build step for targets an LLM cannot emit — a PDF, a bundled
site. It runs from the repo root and **only when explicitly asked**
(`kg attach --build`), never because the file says so: a spec is a file an agent
writes, so an unconditional `## Build` would let anything that can edit a spec run
anything.

It refuses a partial attach — building from an incomplete set of artifacts
produces a binary corresponding to no recorded state — and a failure is recorded
as `build-failed`, which keeps a packet whose PDF never built from reporting
`fresh`.

### `## Options`

- `relations: none` — omit incident-edge rendering. A fact's relations carry the
  **bodies** of nodes the query did not select, which in a document shared before
  engagement is a confidentiality leak. The `about` anchor still renders: it says
  what a fact is *about*, which is identity rather than a relation between facts,
  and a claim whose body does not name its subject ("Reachable at 555-010-2288")
  is unusable without it.
- `publish: internal` — this spec's outputs are working material and no viewer
  manifest is meant to list them. It silences the `no viewer publishes …`
  warning, which otherwise fires on every spec that has outputs and is absent
  from the `kgraph-web.yaml` governing its directory.

  The warning exists because the failure is invisible: `kg attach` writes the
  artifact, `kg status` says `fresh`, `kg scan` reports no error, and the page is
  a 404, because kgweb served only the specs its manifest listed and read that
  list once at start-up. Two documents in the fence-dispute corpus were written, cited
  from a work queue as though they could be opened, and could not be opened by
  anyone.

  It is a warning rather than an error because a queue, a call runsheet or a
  purchasing list is right to be unpublished. Those say so here, in one greppable
  string, next to the spec — which is also how you find them: `grep -rn "publish:
  internal" queries/`.

### Multi-output emission

The LLM emits one fenced block per declared output:

````
```out path=site/index.html
<!doctype html>…
```
````

`kg attach` refuses to write an undeclared path and warns on a declared path that
was not emitted.

### Semantic hash

`sem_hash` for a node covers `kind, body, status, owner, value, unit, at,
precision, valid_from, valid_until, occurrence, while, group_expr` **plus a digest of its incident
edges** — an edge change around a pinned node must flag consuming docs, because
the fact rendering includes relations. It excludes provenance metadata, or every
re-ingest false-flags every document.

### States

Three independent checks — `spec_hash` vs file, pinned `facts` vs re-resolved
query, `outputs` hashes vs disk — combine into the state that picks the prompt:

| state | meaning | prompt |
|---|---|---|
| `fresh` | all match | none |
| `never-rendered` | no managed block yet | full render |
| `facts-changed` | resolution differs | old doc + fact diff → **revise** |
| `spec-changed` | purpose/query/prompt edited | old prompt + new prompt + old doc → **revise** |
| `style-changed` | `.kgraph/style.md` edited | the document did not obey a rule written after it |
| `dialect-changed` | the authored dialect moved — see §4 | the ladder that decides conflicts is not the one it was written under |
| `source-changed` | a cited document's bytes moved | **re-verify**, distinct from *the fact changed* |
| `output-edited` | output hash ≠ disk | hand-edits present; revise must preserve them |
| `output-missing` | a declared output is gone | full render |
| `build-failed` | `## Build` did not succeed | fix the build, not the prose |
| `managed-tampered` | the managed block's self-hash does not match | **believe nothing here** |

`facts-changed + output-edited` is the common case and the reason regeneration is
always a revise, never a regen.

The last one is not like the others. Every other state says a document is out of
date; `managed-tampered` says the record of what it was generated FROM has been
edited by hand, so a stale document can report itself fresh. That is the one
state nothing downstream can detect or recover, which is why the block carries a
self-hash and why `guard-managed` refuses the edit at source.

---

## 3. Provenance and conflict

### Sources are nodes, not a string field

`src: path/to/doc.md` fails for the same reason group-kinds-as-an-enum failed:
provenance is itself something you reason and query over. "Opposing counsel
conceded it on a recorded line" and "Sadie recalls annotating the map" are both
"a source", and a generated document must not word them the same way.

```yaml
- id: s-nettle-call
  utterance: Settlement call with Ivo Nettle (plaintiff's counsel)
  by: ivo-nettle
  medium: call
  recorded: true
  at: 2026-03-14
  doc: documents/calls/2026-03-14-nettle-settlement-call-transcript.md
  class: admission          # statement against the speaker's own interest

- id: s-rrepsa
  document: 2021 RREPSA purchase & sale agreement
  # `doc:` names the INSTRUMENT, not a transcription of it. The transcript is found
  # beside it by `companions()`, which prefers raglit's `.raglit-transcription.md`.
  #
  # This example pointed at `...ocr.md` and taught the wrong thing. That file, in the
  # live corpus, renders the operative record of survey as `AF#20140415006` — eleven
  # digits, denoting nothing — where raglit's transcription of the same PDF has
  # `AF#201503110043` correct. A spec exemplar labelled `class: record` pointing at a
  # hand-typed sidecar is how a fabrication becomes self-authenticating.
  doc: documents/evidence/2021-rrepsa-purchase-sale-agreement.pdf
  anchor: p14
  class: record             # recorded instrument; self-authenticating

- id: s-sol-analysis
  inference: Statute-of-limitations first pass
  derived_from: [tess-claims-timely, sol-tort-barred]
  at: 2026-03-14
  class: inference
```

Consumed from the claim side:

```yaml
- id: no-pending-deadline
  claim: No pending deadline or hearing is set against the Quills
  status: asserted
  attested_by: s-nettle-call
  undercut_by: s-no-written-confirmation
```

### Evidentiary class is ordinal, not a probability

| class | example in the fixture |
|---|---|
| `record` | recorded instrument, court order, filed docket document |
| `admission` | statement against the speaker's own interest — Nettle conceding easement-by-conveyance; Harrow, Bramble's own surveyor, backing a reciprocal easement |
| `document` | a writing that is not self-authenticating |
| `statement` | ordinary assertion by a party |
| `interested` | a party asserting what benefits them — Sadie on the map annotation |
| `observation` | direct observation |
| `inference` | derived by us; `derived_from` names the nodes |

This ranks and it is defensible, which a hand-typed `0.7` is not. It also drives
wording: a brief should write an `admission` as "Nettle conceded" and an
`interested` as "Sadie recalls" — the prompt gets the class, so the LLM does
not have to guess how hard to lean.

`inference` sources matter most: they are the line between *we were told* and
*we concluded*, and only they carry `derived_from`, so a conclusion invalidates
automatically when its premises move.

### Source documents are hashed

Every `doc:` path is hashed at scan. When the underlying file changes — the
fence-dispute PDFs are still draining through OCR in the background — every fact
attested by it flags `source-changed`, meaning *re-verify*, distinct from
*the fact changed*. Without this, a re-OCR silently invalidates facts nobody
re-checks.

### References render themselves

Because a source is a node with structured fields, **citation is a render
concern, not a prompt concern** — which matters, because an LLM is unreliable at
citation discipline and a renderer is not.

The prompt interpolates facts carrying opaque tokens; the model is told only to
keep the token and put it at the end of the statement. `kg attach` substitutes.

```
prompt sees:   No pending deadline is set against the Quills [[s-nettle-call]]
.md output:    …against the Quills. — *ref: documents/calls/2026-03-14-nettle-settlement-call-transcript.md*
site output:   …against the Quills.<a href="…#p1" class="ref">[3]</a>
pdf source:    …against the Quills.\footnote{Nettle settlement call, 2026-03-14 (recorded).}
```

One graph, one token, three citation styles — chosen per output, not per prompt.

Three of the `~/life` client-facing document rules stop being instructions the
model can violate:

| rule | becomes |
|---|---|
| refs go at the **end** of the statement, never mid-sentence | token placement, enforced at substitution |
| **first mention of a person states their role** | `by:` points at an entity node whose body carries the role; the renderer tracks first mention per output and expands once, then contracts |
| cite the source, not its `-analysis.md` companion | companion is derived from `doc_path` by naming convention and linked, never cited in its place |

A source node referenced by no claim, or a `by:` pointing at a missing entity, is
a scan error — dangling references are caught at build, not discovered in a
document.

### Conflict: disputed vs. underdetermined

A claim carrying both supporting and contradicting evidence is **computed**
`disputed`. Never author it. But conflicting evidence means one of two very
different things, and confusing them is how a graph rots:

**Disputed** — sources disagree about a well-formed claim with a determinate
answer. "Closing was 8/14" vs "closing was 8/19". Exactly one is true.
*Resolution: weigh the evidence, then `supersedes`.*

**Underdetermined** — sources only *appear* to disagree because the claim is
ambiguous and is really two claims sharing one id. "Access to the northern lots"
— vehicular or utility? deeded or prescriptive? The sources are answering
different questions. *Resolution: split the node. Weighing evidence here produces
a confident wrong answer.*

Heuristic the tool can apply: if the conflicting sources differ in scope — date
range, subject qualifier, `about` target — suspect underdetermined and propose
a split; if they scope identically, it is disputed. This is a flag for review,
not an automatic rewrite.

A `disputed` claim renders in documents **as disputed**, with both sources and
their classes. Silently picking a side is the failure mode this whole tool exists
to prevent.

---

## 4. Indexes and dialects

An **index** is a closed graph. A **dialect** is the vocabulary and rule set that
index binds to. One file declares both.

### `.kg-index` — the marker

```
acme/.kg-index
```
```yaml
index: acme
dialect: engagement
```

Nearest marker wins. A marker that declares no name takes its **directory's**
name, so the common case is an empty file. Files under no marker form the
**default index**, which is why a corpus that predates all of this keeps working.

**The key set is CLOSED, and closing it is the point.** The first parser took the
first non-blank line and split it on `":"`, treating whatever followed as the
name — so a marker whose only line was `dialect: legal` named the index
**"legal"**, silently, because an index name is validated against nothing and a
wrongly-named index simply resolves to an empty graph. The fix was rejecting
unknown keys, not special-casing `dialect`: every typo had that same shape. A
marker that will not parse is an **error** at every entry point, reported once
per marker rather than once per file underneath it.

| key | meaning |
|---|---|
| `index` | the index's name. Optional; the directory's name is the default. |
| `dialect` | the vocabulary to bind. Optional; `legal` is the default. |

Both name forms parse: a bare `acme` on its own line means `index: acme`.

**Verification is in-index, always.** A corpus discovered by walking up to a
`.git` root takes in every sibling project, and a monorepo of unrelated matters
then becomes one graph — which corrupts *computed* state rather than merely
adding noise. `disputed` is computed from conflicting evidence, so a foreign
matter can mark a claim disputed; `sem_hash` covers incident edges, so a person
appearing in two matters can carry edges across the seam and produce phantom
deltas. The failure that motivated this was an unqualified `event[at>2026-03-01]`
silently spanning two matters and putting one person's medical facts into
another matter's attorney packet.

**`## Index` in a spec documents and enforces; it never redirects.** A spec
resolves against the index owning its directory, and a header that disagrees is
an error naming both sides. Letting it redirect would hand a spec reach into
another matter by editing a heading. A spec may not name a `dialect:` at all —
the index owns that, and a spec that could name one would pick its own
evidentiary ladder by editing a heading.

### What a dialect is, and what it is not

A dialect carries five things, and only the first four are words:

| | |
|---|---|
| the ordinal ladder | the rung names and their order |
| the governing class | the out-of-ladder class that states law rather than testifying |
| the speaker-required rungs | those that assert something about someone's interest |
| the invalidation flag | `impeached` · `retracted` · `revoked` |
| additions | relations it adds, and node subtypes over the core kinds |

Everything else is **physics** and is never dialect-scoped:

- that an ordinal ladder resolves conflicts at all
- that the governing class has **no rank**, so it can neither win nor lose an
  evidentiary contest — see §3
- that `disputed` is computed and never authored
- that `derived_from` makes a conclusion invalidate when a premise moves
- in-index closure, the `SemHash` field set, one-idiom-per-relation, the managed
  self-hash

**A dialect may not redefine a core relation.** Every type in the core table
participates in a computation — `attests` in evidence, `contradicts` in
`disputed`, `supersedes` in which fact wins, `derived_from` in staleness — so a
dialect that could redefine one could change what a graph concludes, silently. A
dialect's own relations are the opposite: they exist to be **walked** and drive
nothing, which is the entire licence for that set being open.

**A subtype does not create a kind.** It says which core kind it must be and
which keys it may carry, so the schematic layer sits on top of an arbitrary graph
rather than growing it. A subtype may not shadow a core key, and prefixing does
not rescue it: `acq:status` is unambiguous, but a subtype declaring bare `status`
is ambiguous on any node that did not prefix it, and whether the author
remembered is not a thing the format should depend on.

### Where a dialect comes from

Three places, resolved **nearest first** — the same rule the marker itself uses:

```
acme/.kgraph/dialects/engagement.yaml   →  acme.engagement    project-owned
.kgraph/dialects/engagement.yaml        →  engagement         corpus-wide
(compiled in)                           →  legal              built-in
```

A project's dialects are **qualified by the index that owns them**, and that
qualification is what makes them safe. A bare `dialect: engagement` in acme's
marker finds `acme.engagement` if acme wrote one and the corpus-wide
`engagement` otherwise; the qualified form resolves too, so a marker can say
`acme.engagement` and mean exactly that.

The two shadowing rules therefore differ, on purpose:

- **A corpus-wide file may not redefine a built-in.** `legal` would otherwise
  mean something different per checkout, and a document's ladder is not a thing
  that may vary by clone. Refused, naming both sides.
- **A project's may share a name with anything**, because its identity carries
  the project in it. Two matters can each mean their own thing by `engagement`
  and neither can change what the other resolves. A project may specialise
  `legal` as `acme.legal` — explicit rather than a silent override, and the
  built-in is untouched everywhere else.

An **unknown** dialect is an error, never a fall back to `legal`: continuing
means evaluating one corpus's facts against another's ladder and reporting a
confident answer about it. The refusal lists what would have been accepted,
built-in and authored, so it cannot tell somebody their dialect does not exist
while it sits in the directory next to the one they are editing.

`kg indexes` names each index's dialect and the marker key. It is the discovery
path: with a closed key set, guessing the key wrong is a hard error and there
would otherwise be nothing to guess from.

### Authoring one

```yaml
# .kgraph/dialects/engagement.yaml   — the file name IS the dialect's name
ladder:                    # STRONGEST FIRST
  - written-confirmation   # the client's own email — the `admission` analogue
  - measured
  - meeting-note
  - verbal
  - assumed
governs: signed            # SOW, executed change order. Law, not evidence.
invalidated: unconfirmed   # a scope claim resting only on `verbal` or below
speaker_required: [verbal, meeting-note]
edges:
  serves: served by        # walkable, drives nothing
subtypes:
  deliverable:
    kind: claim            # a subtype does not create a kind
    keys: [accepted_on, invoice]
```

The ladder is authored as an ordered **list**, never as a map of numbers. The
numbers are an implementation detail, and authoring them invites gaps that read
like meaning. An optional `name:` must agree with the file name — two places to
say one thing, so disagreeing is an error rather than a precedence rule somebody
has to remember.

Every guard the compiled-in dialects get is run over an authored one, as an error
naming the file:

| refused | because |
|---|---|
| an empty or duplicated ladder | an ordinal ladder is what resolves a conflict, and a class has one rank |
| the governing class **on** the ladder | having no rank is the mechanism, not an omission |
| no invalidation flag | the mechanic is core; the word is the dialect's |
| `speaker_required` naming a rung that is not on the ladder | it would silently require nothing |
| a relation that is already core, forward or inverse | it could change what a graph concludes |
| a subtype over a kind that is not core | the core kind table is closed |
| a subtype governing a core scalar or relation | it would shadow a key, and prefixing does not rescue it |

**What is still not checkable** — and never was, including when the list was
closed in the binary — is whether the ORDER is right for the trade. A ladder with
its rungs semantically misplaced produces no error, just confidently wrong
`disputed`. Closing the list only limited who was allowed to get that wrong; it
never checked it. The mitigations are that the dialect is named in `kg indexes`,
that everything checkable is checked, and that editing one is visible:

### Editing a dialect flags the documents written under it

An authored dialect is hashed, bound into the render token, recorded in the
managed block as `dialect_hash`, and reported as **`dialect-changed`**.

This does **not** put the dialect in `SemHash`. A class *rename* is a content
change on the source node and should flag there; the declaration must not, or
switching dialect would re-flag every fact in the matter. What `dialect-changed`
catches is narrower and it is about DOCUMENTS: the ladder decides which of two
conflicting sources wins and how the prose says so, and a document reporting
`fresh` against a ladder written after it is claiming to obey something it never
saw. Same argument, same mechanism, and the same conventional directory as
`.kgraph/style.md`.

**A built-in hashes to the empty string.** A corpus that declares no dialect
records exactly as it did before dialects were data — otherwise every document
already rendered would report `dialect-changed` on the next status, which is the
false-flag the staleness rules exist to prevent. A built-in also cannot be edited
under a finished document; a file can, and that is the whole of the difference.

---

## 5. Query language

Surface DSL for humans and doc headers; lowers to a JSON AST that the MCP tool
accepts directly (schema-constrained, so an agent can skip the parser and never
emit a syntax error). Both compile to the same recursive SQLite CTEs.

```
query    := union
union    := inter ('|' inter)*
inter    := unary ('&' unary)*
unary    := '!' unary | primary
primary  := '(' query ')' | '@' NAME | pattern
pattern  := atom (hop atom)* temporal? sort?
sort     := 'sort' KEY ('asc'|'desc')? (',' KEY ('asc'|'desc')?)*
atom     := KIND? ('#' ID)? ('[' preds ']')? ('~' STRING)?
hop      := '-' EDGE '*'? '->'      # forward, '*' = transitive closure
          | '<-' EDGE '*'? '-'      # inverse
          | '-' EDGE '*'? '-'       # undirected
preds    := pred (',' pred)*
pred     := KEY OP VALUE | KEY 'in' '(' VALUE (',' VALUE)* ')'
temporal := '@' ('now' | DATE | '[' DATE ',' DATE ']')
```

- `!` is **set complement over the result universe**, not negation-as-failure.
  Total, cheap, no stratification.
- `~ "text"` is FTS5 over node bodies.
- Group nodes expand during traversal per their `kind`, so `all`/`any`/`none`
  never appear in query syntax.
- **The leftmost atom is the result set.** Flip with `<-` to return the other
  end. Path variables and multi-binding returns are v2.

```
claim[status in (asserted,disputed)] -requires*-> #settlement @now
question[status=open] <-about- entity#rafe-wren
claim -contradicts- claim @now
!(claim -answers-> question#q-0031)
@blockers & claim[confidence<0.5]
claim ~ "closing date" @[2021-01-01,2022-01-01]
```

Deliberately v2: aggregation (`count`, `group by`), path variables, embedding
similarity.

### Lineage

Not one language — a composite, taking the part of each that fits:

| from | what |
|---|---|
| **Cypher** | the path grammar. `-[:REQUIRES*]->` became `-requires*->`, with `<-` for the inverse |
| **CSS / XPath** | attribute predicates. `claim[status=open]` rather than Cypher's `{prop: val}` or a `WHERE` clause |
| **Datalog** | named rules — derived predicates as `@name`, the one idea worth stealing without taking the engine |
| **set algebra** | `& \| !` as infix operators over result sets. Cypher has `UNION` but no infix intersection |
| **SQL:2011** | the temporal suffix. `@2020-06-01` is `FOR SYSTEM_TIME AS OF` in three characters |
| **SQL** | `sort`, straight from `ORDER BY` |

Three things were deliberately *not* taken. Cypher's **variable bindings and
`RETURN` projection** — the leftmost atom is the result instead, which costs
expressiveness and buys a grammar an LLM writes correctly on the first try.
Datalog's **negation-as-failure** — `!` is set complement over the universe,
total and cheap, with none of the stratification. And SPARQL, entirely.

### Trap: `@now` and `sort` bind to a pattern, not an expression

Both are suffixes on a *pattern*. In a set expression they attach to the nearest
pattern, which is rarely what the line looks like it says:

```
claim & !claim[while] @now        # @now scopes the NEGATED term
claim @now & !claim[while] @now   # what that line looks like it means
```

The first over-collects and does not error. `@now` also cannot appear
mid-pattern — `claim @now -requires-> #x` is a parse error, which is the one
place the grammar is loud about it.

---

## 6. Sets: ordering, counts, groupings

A query returns a **set**, but the prose the LLM writes from it bakes in facts
*about* the set — "three items block settlement", "the earliest is the ALTA
commitment", "all of them turn on the easement". Those are derived, belong to no
single node, and go wrong when the set changes. Pinning node ids alone flags the
document correctly but leaves the revision prompt unable to say *which sentence*
is now false.

So the pin records the realized set, not just its membership.

### Ordering must be declared, never incidental

Default sort is `id` — stable, so a re-resolve never reorders spuriously. When
narrative order is load-bearing, declare it:

```
blockers: claim[status=asserted] -requires*-> #settlement @now sort valid_from, id
```

Undeclared order that happens to come out of SQL is a bug generator: the diff
would show churn that means nothing, and real reordering would hide in the noise.

### Two kinds of grouping — keep them apart

**Semantic grouping is a fact.** If "these three blockers are the easement
cluster" is a claim about the world that could be *wrong*, it is a `group` node
in the graph — reviewable, supersede-able, and diffed like any other fact. It is
not a query-time bucket.

**Presentational grouping is a template concern.** `{{blockers | group_by:
status}}` buckets for layout only. Cheap, but the realized buckets get pinned,
because the document's section structure now depends on them.

The test: if the grouping changing would mean *we learned something*, it is a
group node. If it would only mean *the page looks different*, it is a template
filter.

### What the managed block pins per query

```yaml
facts:
  blockers:
    set_hash: sha256:c19e…        # ordered ids + sem_hashes — fast fresh check
    count: 3
    sort: [valid_from, id]
    groups: {easement: 2, disclosure: 1}
    nodes:
      - f-0142@7b09
      - f-0155@2a44
      - f-0087@a3f1
```

### Delta classes over sets

Beyond per-node `+ − ~`, the set itself changes in ways the prose cares about:

| class | trigger | why prose breaks |
|---|---|---|
| `cardinality` | `count` differs | every numeral and every "both/all three" is suspect |
| `ordering` | same members, different sequence | narrative sequence and "first/last" claims break |
| `grouping` | a group key appeared or vanished | document sections appear or disappear |
| `boundary` | first or last element changed | superlatives ("the earliest", "the biggest risk") break |

These are reported to the revision prompt as explicit statements — "count went
3 → 4", "f-0155 is no longer the earliest" — not left for the LLM to infer by
comparing two lists. That is the difference between a revision that reliably
fixes the wrong sentence and one that rewrites the whole document.

### Identity stability

Sets only diff meaningfully if node ids survive re-extraction. If an agent
re-reads a source document and re-emits facts, ids must not churn or every diff
is total. Nodes therefore carry a content-addressed fallback id derived from
`(src, normalized body)`, and an explicit `alias:` field to merge a
newly-extracted node into an existing one. Unmatched new nodes are reported for
review rather than silently added — a silent add is indistinguishable from a
real discovery.

---

## 7. `kg diff`

The centerpiece. Change or add a fact → which queries changed results, and which
specs therefore went stale.

```
kg diff                          # working tree vs pinned resolutions
kg diff <file.kfacts.md>         # scoped to edits in one fact file
kg diff --what-if '<yaml node>'  # blast radius BEFORE committing the fact
```

No reverse index. Snapshot the index, apply the change to a scratch overlay,
re-evaluate every named query and every spec query, set-diff each. At 10³–10⁴
nodes that is milliseconds — incremental view maintenance is not worth building.

Delta classes per query:

- `+` node entered the result set
- `−` node left the result set
- `~` node stayed, `sem_hash` changed (body, status, validity, or incident edges)
- `?→!` question resolved (gained an `answers` edge or flipped to `resolved`)
- `⚠` new live contradiction: two `contradicts`-linked nodes both asserted with
  overlapping validity
- `⚖` claim became `disputed` — gained conflicting evidence — or stopped being
- `📄` `source-changed`: an attesting document's hash moved. **Re-verify**, which
  is not the same as *the fact changed* — the fact may be fine, but nobody has
  looked since the source moved under it
- `∴` an `inference` source's `derived_from` premises changed, so the conclusion
  is stale even though nothing about the conclusion itself was edited

Output maps query deltas → affected specs → suggested action.
