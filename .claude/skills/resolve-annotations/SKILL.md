---
name: resolve-annotations
description: "Work the reader-annotation queue on a kgraph matter viewer: verify each correction, question, added-context note, dispute and rephrase against the evidence, change the graph where it is wrong, answer where it is right, and record what was done. Activate when asked to resolve, triage or work through annotations, corrections, or the note queue. Built to be driven iteratively with /loop, one document or one fact per iteration."
---

# Resolving reader annotations

Readers of a published matter send back corrections, questions, added context,
disputes and rephrasings. This is how they get dealt with.

## Where you are, and where the work is

Three different places, and conflating them is the first mistake:

| what | where |
|---|---|
| the tool — `kg` | `~/local/src/iodesystems/kgraph/dist/` — **not on PATH** |
| the viewer config | `~/life/projects/<matter>/kgraph-web.yaml` |
| the facts you will edit | `~/life/projects/<matter>/*.kfacts.md` |
| the corpus root | `~/life` — `DiscoverRoot` walks up to `.git`, and that is where it lands |
| **the directory to actually work in** | `~/life/projects/<matter>/` — see the trap below |
| the note and upload stores | `~/.local/state/kgraph/web/<index>/` — never in the corpus, because it syncs |

**`cd` into the matter's project directory and stay there.** `kg` resolves against
the index that OWNS THE WORKING DIRECTORY, and this is the trap: run `kg query` from
`~/life` and it returns `0 row(s)` — not an error, not a warning. A triage pass run
from one directory up concludes that facts do not exist and starts authoring
duplicates. Measured: `kg query 'question'` gives 0 rows from `~/life` and 40-odd
from the matter's own directory.

Commit fact edits in **`~/life`**, which is its own git repo — separate from the
kgraph checkout the tools came from. Two repos are in play and a commit in the wrong
one loses the work.

Build the tools first if `dist/` is stale — `bin/build` writes both.

`$MATTER` is the matter's directory under `~/life/projects/`. **Set it from the
matter you are working; this file does not name one** — a matter directory is
named for its parties, and party identity does not belong in a repository. See
`plan/plan.md` § party names.

```
export MATTER=~/life/projects/<the matter>
export KG=~/local/src/iodesystems/kgraph/dist/kg
export NOTES=~/.local/state/kgraph/web/<index>/notes.json
cd $MATTER
```

## The operations

Today these are CLI calls. Prefer `--json` for anything you parse: the text form is
for a person and will break on the first note body containing a newline, which is
most of them.

**kgweb was retired 2026-09-04 and `notes.list` now reads the store directly.**
That store is plain JSON and nothing else ever wrote it, so losing the viewer lost
the collection of new notes, not the reading of existing ones. No new notes can
arrive: the reader-facing viewer is gone.

| operation | today |
|---|---|
| `notes.list` | `jq '[.[]\|select(.resolved\|not)]' $NOTES` — unresolved first; the file IS the store |
| `notes.reply` | **RETIRED with the viewer.** A reply existed to be READ by the person who wrote the note, in a page that no longer serves. Writing one now puts a message where nobody will see it |
| `notes.resolve` | **RETIRED with the viewer.** Record the outcome where the work landed — the fact, the document, the matter's log — not in a store nothing renders |
| `notes.reopen` | **RETIRED with the viewer**, same reason |
| `uploads.list` | `jq . ~/.local/state/kgraph/web/<index>/uploads/index.json` — blobs are beside it under `uploads/blob/` |
| `uploads.accept` | file the blob into the corpus by hand, then verify by hash. The one upload in the fence-dispute store was already filed this way and matches byte for byte |
| `uploads.reject` | **RETIRED with the viewer** — there is nobody to tell |
| `sources.search` | `$KG have <identifier>` — aliases, titles, filenames, transcript text, and it reports WHICH tier matched |
| `facts.get` | `$KG query '#<id>'` — there is no `kg show` |
| `facts.query` | `$KG query '<dsl>'` |
| `quotes.check` | `$KG quotes` — does the transcription contain the words a fact puts in quotation marks? Triage, not a verdict |
| `attest.record` | `$KG attest <fact> <source> attested\|corrected\|unsupported\|illegible` |
| `docs.status` | `$KG status` · `$KG diff` — what moved, and which documents went stale |
| `questions.blast` | `$KG variants` — which open questions would change a document, per answer |
| `render` / `attach` | `$KG render <spec>` then `$KG attach <spec> <path>...` |

`kg` ALREADY serves MCP on stdio — `kg serve`, proxied to the daemon. If the triage
side ever wants MCP tools, that is where they belong now.

## The one failure that matters

**Never resolve a note you did not act on.** A resolved note LEAVES the document for
every reader — deliberately, so one correction is not made twice — which means
resolving to clear the queue silently hides a live problem. It is the note-queue
equivalent of a false-fresh document: nothing downstream detects it, and the reader
has been told their correction was handled.

If you cannot verify something, reply and leave it open. An open note is honest; a
resolved one is a claim.

## A note targets a document OR a fact

A note carries four locators — `document`, `anchor`, `fact`, `source` — and at
least one, enforced at submission: a note about nothing is reachable from no view
and is lost while its author believes it was filed.

**Which one it carries decides how this skill batches it**, and the two halves
differ only in what a batch IS:

| the note is on | batch by | verify against |
|---|---|---|
| a document (`document`/`anchor`) | that document | the rendered artifact, matching `quoted` |
| a fact or a source (`fact`/`source`) | that node | the graph and the instrument, no artifact involved |

Everything below about deciding, answering, asking and recording applies
unchanged to both. **Only the regeneration step is document-only** — a
fact-targeted note has no artifact to regenerate, so the pass ends at the fact
edit and the commit.

Generated documents are being retired (`plan/web.md`), so the document half of
this table is the half with an end date. When it goes, the regeneration rule and
the `quoted`-matching rule go with it and nothing else here changes.

## Each iteration

One document or one fact, at most 8 notes. Both bounds are so the commit stays
reviewable — a single pass over 27 notes produces a diff nobody can check.

**1. Read the queue.** Take **reopened notes first** — a reply carrying `reopened` is
someone saying a fix did not fix it, which outranks anything new. Otherwise take the
target with the most open notes, document or fact alike.

**2. Read all of that target's notes before changing anything.** Several routinely
sit on one passage or one claim and contradict each other; deciding them one at a
time produces edits that fight, and the second silently undoes the first.

**DECIDE THE WHOLE DOCUMENT BEFORE YOU REGENERATE IT — once, at the end.** A note
stores the passage it was about as `quoted`, and the highlighter locates it by
matching that text against the rendered document. Regenerating a 435-line artifact to
fix one paragraph therefore unmoors every other note on it: 26 notes go `drifted` or
`lost`, and the readers who wrote them see their annotations slide off the passages
they were about. Fact edits are safe to make as you go — they only change what a
future render produces. The render itself is the destructive step, so batch it.

This means most notes come out of an iteration REPLIED-TO AND STILL OPEN, with the
verification recorded, waiting on one regeneration. That is the correct outcome, not
an incomplete one.

Then order them: **corrections and disputes → context → rephrase**. Rephrasing a
sentence a correction is about to rewrite is wasted work.

**3. Choose one outcome per note. Prefer ASK when unsure.**

- **EDIT** — the graph is wrong or thin, or the prose is. See the recipe below; this is
  the majority outcome.
- **ANSWER** — the document is right, says it clearly, and the reader simply had not
  found it. Reply, then resolve. **This is rarer than it looks** — read the recipe
  before reaching for it.
- **ASK** — you cannot verify it, or acting needs a document the file does not hold.
  Reply with the specific question and **do not resolve**.

### The recipe — what "fixing a note" almost always means

Most notes, whatever kind their author gave them, resolve the same way. A reader who
asks "what four instruments?" is not requesting a private answer; they are telling you
the sentence does not say. Answering in a reply and resolving leaves the document
exactly as unclear as it was, and the next reader asks the same question — except now
the note that would have told you is closed and off the page. That mistake has already
been made once here; `--reopen` exists because of it.

In order:

1. **Rephrase — de-ai it.** Generated prose has a characteristic badness: "everything
   turns on it", "it is worth noting that", a chain of title introduced as "the chain
   of title question". Say what a person would say. This is the fix far more often than
   any factual change.
2. **Attribute it, and put the detail THERE.** If naming the thing inline makes the
   sentence cumbersome, that is what citations are for. **Excessive detail belongs in
   the attributions, not in the prose** — the text reads clean and the four recording
   numbers live in the footnote where a reader who wants them can find them. A sentence
   carrying its own evidence in parentheses is a sentence nobody finishes.
3. **Create the fact, if it can be sourced or inferred.** A number that is right in six
   `reason:` fields and stated nowhere as a claim is a number a generated document will
   eventually contradict — there is nothing for it to be stale against. If the thing is
   arithmetic across two events, that is an `inference` with both premises named.

A `question` is therefore usually an EDIT, not an ANSWER. So is most `context`: "we
should be citing our sources here" is step 2, and "this reads badly" is step 1.

Do not relabel the author's note to match. The KIND is theirs — what changes is what
triage does about it.

**4. Record it.** The resolution is shown to the author, so name what changed and
where — "corrected in the briefing, §The septic claim" — not "fixed". A resolution
with no account of what happened is indistinguishable from a dismissal.

**5. Verify and commit.**

- `$KG scan` — read all of it. It reports every problem rather than stopping at the
  first, by design: a dangling reference in a legal corpus has to be caught at build,
  not discovered in a filed document.
- `$KG diff` — what moved. This is how you learn which OTHER documents your edit made
  stale; a correction to one fact routinely restales three specs, and leaving them
  stale is how a reader is shown a document that disagrees with the graph.
- `$KG quotes` for anything touching a quotation. An absent quote means the
  transcription dropped the passage or the citation is on the wrong document — both
  worth knowing before you tell the reader it is fixed.
- `go test ./...` in the kgraph checkout only if you changed code there.

Then one commit **in `~/life`** for that document's fact edits.

## Hard rules

- **A note is not evidence.** A party writing "the date was X" is a `statement`- or
  `interested`-class assertion whose source is the note itself, never a `record`. If
  the claim needs an instrument the file does not hold, that is an open question, not
  a fact.
- **A note quoting another AI is not authority.** This queue contains pasted analysis
  from other models, with case-law citations and statutory claims. Fabricated
  citations are a known failure mode of every model, this one included, and this
  corpus feeds legal filings. Before anything from such a note is relied on: confirm
  the authority EXISTS, confirm it says what the note claims, and record it as
  `authority` with its own source. An unverified legal theory is at most an open
  question. Say so in the reply — the reader deserves to know their source was not
  taken at face value.
- **A `rephrase` changes wording only.** If the edit would change what is CLAIMED,
  stop — that is a correction and needs a basis. Rephrase deliberately requires no
  basis, so it is the one kind that can smuggle an unsourced assertion in.
- **Never edit at or below `<!-- kgraph:managed`.** Everything there is generated, and
  a hand-edited managed block makes a stale document report itself fresh.
- **Never hand-write a transcription sidecar.** Extraction is raglit's job, OCR and
  figures included: `raglit index <path>`.
- **Never author `confidence` or `disputed`.** Weight comes from the source's class;
  `disputed` is computed from conflicting evidence.
- **`withdrawn` ≠ `false`.** `withdrawn` means we were wrong, and flags documents.
  `false` means tested and rejected on purpose, and never flags documents.

## Uploaded evidence

A note may arrive with a file. It is in quarantine, not the corpus. `uploads.accept`
records where a file went; **it does not copy anything.** Move it yourself and run
`raglit index` over it — a document in the corpus with no transcription sidecar is
invisible to `kg verify`, which is worse than one honestly still in the inbox.

Do not resolve a note whose evidence is still pending. Nothing rests on a document
that has not arrived.

## Before you start: is anything else writing the corpus?

`cd ~/life && git log --format='%h %ad %s' --date=format:'%H:%M' -3` and check the
times. If something committed in the last few minutes, STOP and say so. Concurrent
sessions do work on this corpus, and an uncommitted fact edit sitting in the tree
when another session runs `git add -A` is swept into ITS commit, under a message
describing different work. That happened on the first real run of this skill. In a
corpus that backs legal filings, a commit whose message does not describe its
contents is a defect in the audit trail, not an inconvenience.

## Stopping

Stop when no open notes remain, or when every remaining one is an ASK. Do not
re-examine notes already replied to and awaiting an answer — that is a loop that
never converges.

Report: what was resolved and how, what waits on a reader, what waits on a document,
and **anything found that contradicts the record beyond the note that raised it**.
That last category is the most valuable thing a triage pass produces and the easiest
to leave behind in your head.
