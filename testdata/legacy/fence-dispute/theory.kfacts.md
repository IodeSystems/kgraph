# Theory — the defect, the two paths, and what is not yet proven

The load-bearing distinction in this fixture: a claim can be **supported** and
still be **unproven**, because the thing that would prove it is a document
nobody has. `q-geometry-proof` is the hole, and `while` keeps any theory that
depends on it out of a `@now` query until it is answered.

## Facts

```kfacts
- id: s-vole-2026
  record: Corrective record of survey, Vole & Co.
  by: vole
  supersedes: s-vole-2023
  doc: documents/ros-2026.pdf
  at: 2026-01-08
  class: record

- id: s-deed-2024
  record: Statutory warranty deed, Bramble to Quill
  doc: documents/deed-2024.pdf
  at: 2024-05-30
  class: record

- id: s-quill-letter
  utterance: Quill's counsel concedes the strip was always shared
  doc: documents/nettle-letter-2026-02-19.pdf
  at: 2026-02-19
  by: nettle
  class: admission

# ── the defect ─────────────────────────────────────────────────────────
- id: drawing-conflicts-with-words
  claim: The 1962 plat drawing places the strip on Lot 7 while the written description conveys none of it
  status: asserted
  about: the-strip
  attested_by: s-vole-2026

- id: words-control
  claim: Where a recorded survey's drawing conflicts with its own written description, the description controls
  status: asserted
  supports: reformation
  attested_by: i-words-control

- id: i-words-control
  inference: Stated as the rule the reformation path relies on
  derived_from: [drawing-conflicts-with-words]
  class: inference

# ── path one: quiet title on the deeds ─────────────────────────────────
- id: quiet-title-on-deeds
  claim: Title to the strip can be quieted on the recorded instruments without expert evidence
  status: asserted
  attested_by: s-deed-2024

- id: deed-omits-the-strip
  claim: The 2024 deed conveys platted lot portions and no part of the strip
  status: asserted
  supports: quiet-title-on-deeds
  attested_by: s-deed-2024

# ── path two: reformation, and the hole in it ──────────────────────────
- id: reformation
  claim: The plat drawing should be reformed to match the written descriptions
  status: asserted
  while: q-geometry-proof
  requires: g-reformation-proof
  attested_by: s-vole-2026
  reason: >
    Scoped to the open question deliberately. The words side is verified; the
    geometry is asserted by a surveyor who worked both sides, so a @now query
    must not surface this as settled.

- id: q-geometry-proof
  question: Does an independent surveyor confirm the drawing is off, and in which direction?
  status: open
  needs: evidence
  owner: quill
  about: the-strip

- id: g-reformation-proof
  group: all
  title: What reformation needs before it can be pleaded as settled
  members: [drawing-conflicts-with-words, words-control, q-geometry-proof]
  ordered: false

# ── the concession, and what it does not reach ─────────────────────────
- id: shared-use-conceded
  claim: Quill's counsel concedes the strip was shared before the sale
  status: asserted
  supports: implied-easement
  attested_by: s-quill-letter

- id: implied-easement
  claim: Lot 8 holds an easement over the strip by implication from prior use
  status: asserted
  attested_by: i-implied

- id: i-implied
  inference: Prior apparent use plus a severance leaving the retained parcel landlocked
  derived_from: [strip-serves-both, lot-8-landlocked, shared-use-conceded]
  class: inference

- id: exclusive-use-claimed
  claim: Quill claims exclusive use of the strip
  status: asserted
  contradicts: shared-use-conceded
  attested_by: s-complaint
  reason: >
    Pleaded in the complaint and conceded away in the later letter. Both are
    live sources, so `claim[disputed]` should surface this pair.

# A CORRECTION, not a disagreement. Vole surveyed in 2023, then filed a
# corrective record in 2026. The two readings contradict each other and their
# windows are disjoint: the first WAS the record until the second replaced it,
# and anyone who relied on it during its window relied correctly.
#
# Counting this as evidence pointing both ways would tell a document to weigh a
# fact against its own correction and hedge about something that simply changed.
- id: s-vole-2023
  record: Record of survey, Vole & Co.
  by: vole
  doc: documents/ros-2023.pdf
  at: 2023-06-14
  # It WAS the record until the corrective survey replaced it, so `valid_until`
  # rather than `withdrawn`: nobody who relied on it before 2026-01-08 relied on
  # something wrong, and marking it withdrawn would flag every document that
  # cited it correctly at the time.
  valid_until: 2026-01-08
  class: record

- id: survey-2023-omits-strip
  claim: The 2023 survey shows no strip along the south edge of Lot 7
  status: asserted
  about: the-strip
  valid_until: 2026-01-08
  attested_by: s-vole-2023

- id: survey-2026-shows-strip
  claim: The corrective survey shows the strip bordering both lots
  status: asserted
  about: the-strip
  valid_from: 2026-01-08
  supersedes: survey-2023-omits-strip
  contradicts: survey-2023-omits-strip
  attested_by: s-vole-2026

```
