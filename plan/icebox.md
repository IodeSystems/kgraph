# Icebox — deferred, opt-in

Nothing here is a bug. (`underdetermined` graduated out of here on 2026-07-26.) Each is a real capability that was considered and left out,
with the reason. Pull one out by asking for it.

## FTS5

`~ "text"` is substring matching. Fine at 10³–10⁴ nodes. FTS5 is the only real
argument for keeping the SQLite scaffolding.

## Query engine v2

Path variables and multi-binding returns (today the leftmost atom is the result),
and `group by`. The AST is the contract if these land.

## Write operations from the console

The console is read-only: no assert, resolve, render, or attach from the UI. The
daemon can write (`/attach`), so this is a UI gap rather than an API one.
Deliberate order — a read-only console cannot corrupt a corpus while the format
is still moving.

## A managed "review surface" block in `*.kfacts.md`

Mentioned in `CLAUDE.md`. Would put computed state (disputed, taint, unattested)
back into the fact files as a generated region. Not built: the fact files are the
authoritative input, and `kg scan` already answers the same questions without
kgraph writing into them.

## `.bak` on attach

Considered and rejected. Specs are in git and `WriteSpec` is already atomic, so a
backup file duplicates what version control holds. The hook installer has one
because `~/.claude/settings.json` is not in git.

## Automatic duplicate merging

`checkDuplicates` reports; `same_as:` merges by hand. Automatic merging was not
built because only a person can say whether two similar sentences are one fact —
and a wrong merge silently destroys a distinction.

## `= expr` on `valid_from` / `valid_until`

Only `at` resolves expressions (`resolveAt`, and it checks `m[2] != "at"`). Truth
windows are literals.

The gap shows up on limitations clocks. The natural model is one claim — "this
claim is available" — whose `valid_until` is the bar date derived from its accrual
anchor, so a corrected accrual date moves the window and every document citing it
flags. That cannot be written, so a clock instead needs a derived **event** for
the bar date plus a literal `valid_until` on the claim, and the literal can drift
from the event it is supposed to mirror. Accrual is the least certain part of any
limitations analysis, which is exactly when a stale literal is most likely.

Not built yet because `@now` and `@<date>` already filter on the literal window,
so the corpus is queryable today, and the drift is caught by reading the two
fields together. Worth doing when a corpus carries enough clocks that reading
them together stops being reliable.

## Day counts on a resolved date, in the renderer

`kg render` already resolves and formats derived dates — `[event · 2026-08-04
(derived)]`, and `about …` when precision is approx. What it does not do is say
how far away that is, and for a deadline that is the number a reader wants.

The Bramble matter works around it with `build-clock-days.sh`: the prompt emits
`<!--clock: <id>-->`, the script reads the date back out of `kg render` and
substitutes a count. That works and keeps arithmetic away from the model, which is
the important part — a model asked "how many days until 2026-09-11" is usually
right, and a bar date is not a place for usually.

But it is a shim in the wrong layer. It post-processes the model's OUTPUT, so it
only works for documents that opt in by emitting markers, and it depends on the
model reproducing an id correctly. Rendering `[event · 2026-08-04 (derived, in 6
days)]` at interpolation time would serve every spec with no markers, no opt-in,
and nothing for a prompt to get wrong. `--now` already exists, so it is testable
at any date, including across a bar date.

Deferred rather than built because the output format is read by prompts that are
already written, and widening it is a corpus-wide change worth doing deliberately.
When it lands, delete the sidecar and the marker convention with it.

## Separate "source names no document" from `unreadable`

`kg quotes` reports `unreadable` for two unrelated conditions: a document that
could not be read, and a source that names no document at all. After the
2026-09-03 fixes the entire remaining `unreadable` band (16 of 303) is the
second kind, so the state now means the opposite of what its name suggests.

Worth its own diagnostic — a source carrying quotations but no `doc:` is a
corpus defect a person can act on, while `unreadable` reads as a transcription
backlog and sends them to raglit for nothing.

Deferred: it is a naming problem, not a correctness one. Nothing is being
reported fresh that is not.
