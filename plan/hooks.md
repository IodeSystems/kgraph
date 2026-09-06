# Claude Code hooks — where kgraph gets enforcement instead of detection

> **Mostly historical as of 2026-08-30.** Four of the five hooks are gone:
> `guard-managed` and `guard-output` with generation, `lint-facts` and `what-if`
> with the fact files they triggered on. `report-stale` survives, repointed at
> standing queries.
>
> Their protection did not disappear — it moved INSIDE the write path, where
> `Apply` folds the log a write would join, builds the result, and refuses
> anything that would not load. That is strictly better than a hook: it cannot be
> bypassed by declining to install one. Read the rest as the argument for why
> that placement matters, not as a description of what is installed.

kgraph *detects* every failure mode it cares about. A hook is what turns
detection into prevention, and `~/CLAUDE.md` already states the rule: **if
something must hold 100% of the time, make it a hook, not a CLAUDE.md line.**

## Install

```sh
kg hook install          # merges into <repo>/.claude/settings.json
kg hook install PATH     # or a specific settings file
```

Idempotent, backs up an existing file first, refuses rather than clobbers if the
target is not valid JSON, and preserves foreign settings — it merges into a
shared file. Restart Claude Code (or `/hooks`) to pick them up.

Repo-local by default rather than `~/.claude`, so a graph-specific hook does not
fire in unrelated projects.

## The contract

Each hook reads the event JSON on stdin and answers with an exit code: **0** lets
the tool proceed and puts stdout in front of the model as context; **2** blocks
it and feeds stderr back so the model can correct.

Two rules run through all of them:

- **Only `guard-managed` and `lint-facts` block.** Everything else is advisory. A
  hook that blocks something a user legitimately does trains them to remove the
  hook, which costs more than the hook saves.
- **An internal failure fails open.** A broken hook must never wedge a session,
  so a parse error or a missing repo exits 0 silently. The sole exception is the
  check whose entire job is to refuse.

---

## `guard-managed` — PreToolUse · the one that matters most

Refuses any edit that lands at or below `<!-- kgraph:managed`.

A hand-edited managed block produces a **false-fresh** document: staleness
detection stops merely being wrong and starts actively lying, and nothing
downstream can tell. The self-hash detects it afterwards; this refuses it up
front.

```
kg: timeline.kgraph.md — that edit targets the generated managed block.

It records which facts the document was built from. Hand-editing it makes a stale
document report itself fresh. Run `kg attach timeline <path>...` instead.
```

Handles `Write` (compares the managed region the write would leave), `Edit`, and
`MultiEdit` (any edit whose `old_string` falls inside the managed region).
Editing the **authored** region of the same file is allowed.

**Why a hook and not a rule:** an agent asked to "mark the doc up to date" will
reach for the obvious edit. This is the one corruption the system cannot recover
from by itself, because the damaged state looks healthy.

## `lint-facts` — PostToolUse

Runs `kg scan` after a `*.kfacts.md` write and blocks on errors, so a mistake
surfaces on the edit that caused it rather than three turns later.

```
kg: 1 error(s) in the graph after that write:
  fence-dispute/posture.kfacts.md:106: error: edge requires references unknown node "g-does-not-exist"
```

Catches dangling references, inverted idioms (`question supports: claim`),
authored `disputed`, an alias denoting two things over the same period, and an
option with no `while`-scoped work. It blocks because every diagnostic it reports
is a genuine error the model can fix immediately.

## `what-if` — PreToolUse · advisory

Reconstructs what a pending `*.kfacts.md` write will contain, diffs it against
the current graph, and reports the blast radius **before** the fact lands.

```
kg: this change would move 2 document(s):
  fence-dispute/conflict-check.kgraph.md (3 change(s))
    · ours: count went 28 → 27 — every numeral and every "both"/"all three" is suspect
    · ours: Nolan Sinclair (Kestrel Vance) (kestrel-sinclair) left the set
  fence-dispute/settlement-posture.kgraph.md (2 change(s))
    · leads: count went 4 → 3 …
```

Never blocks. Its job is to make the cost visible at decision time rather than at
cleanup time. Output is bounded, and says what it elided rather than letting a
truncated list read as complete.

## `guard-output` — PreToolUse · advisory

If the target is a declared `## Outputs` entry, says what the edit will look like
afterwards. It deliberately does **not** refuse — hand-editing an output is
legitimate, and revision is required to preserve those edits. Turning this into a
block would put the tool in a fight with the user over their own document.

```
kg: fence-dispute/timeline.md is generated from fence-dispute/timeline.kgraph.md.
After this edit it will report `output-edited`, which is expected — regeneration is a
revise and preserves hand-edits. To make the edit the recorded state instead, run
`kg attach timeline fence-dispute/timeline.md`.
```

## `report-stale` — Stop

Reports which documents went stale, with the delta statements. This is the
failure the project exists to prevent: a session where a fact was corrected,
several documents silently went stale, and nobody found out until one was sent.

Skips `never-rendered` specs — those are not news at the end of a turn.

---

## What NOT to hook

- **Do not auto-run `kg attach`.** Attach asserts that an artifact came from a
  resolved state. Automating it re-creates exactly the false-fresh problem the
  self-hash exists to prevent, with extra steps.
- **Do not auto-regenerate documents.** kgraph never calls an LLM, and a hook
  that shells out to one puts a nondeterministic step inside a tool whose whole
  value is being reproducible.
- **Do not block on warnings.** `alias denotes 2 things` and `source attests
  nothing` are true, useful, and routinely fine. Blocking on them trains the user
  to disable the hook.

## Tests

`cmd/kg/hooks_test.go`. The ones worth knowing:

- `TestPendingContentReconstructsTheWrite` — what-if runs *before* the tool, so
  the post-write content has to be reconstructed from `Write` / `Edit` /
  `MultiEdit`, and an inapplicable edit must stay quiet rather than report
  nonsense.
- `TestHookInstallPreservesExistingSettings` — merging into a shared file must
  not drop someone else's hooks or settings.
- `TestInstallableNamesAreAllDispatched` — every installed command must be one the
  binary actually dispatches. A typo there produces a hook that silently does
  nothing on every tool call, which is worse than no hook.
