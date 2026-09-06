# Timeline — events, causation, and a deadline that moves with its basis

Events carry `at` with a `precision`, because "May 2024" and "2024-05-30" are
different assertions. `causes` records why one event followed another, and
`at_expr` derives a date from another node so a deadline recomputes instead of
going stale.

## Facts

```kfacts
- id: s-docket
  record: Court docket, CV-2025-00418
  doc: documents/docket.md
  at: 2026-03-01
  class: record

- id: s-client-note
  document: Client's own chronology
  doc: documents/chronology.md
  by: wren-bramble
  class: interested

- id: e-sale
  event: Lot 7 conveyed to Quill
  at: 2024-05-30
  status: asserted
  about: lot-7
  attested_by: s-docket
  causes: e-fence

- id: e-fence
  event: Quill erects a fence along the strip
  at: 2025-08
  precision: month
  status: asserted
  about: the-strip
  attested_by: s-client-note
  causes: e-complaint

- id: e-complaint
  event: Quill files suit
  at: 2025-11-14
  status: asserted
  about: the-matter
  attested_by: s-docket

- id: e-answer
  event: Brambles answer and assert affirmative defences
  at: 2025-12-19
  status: asserted
  about: the-matter
  attested_by: s-docket

- id: e-counsel-withdraws
  event: Defence counsel withdraws
  at: 2026-02-11
  status: asserted
  about: dace
  attested_by: s-docket
  causes: d-substitution

- id: d-substitution
  event: Deadline to substitute counsel or appear personally
  at: "= e-counsel-withdraws.at + 30d"
  status: asserted
  about: the-matter
  attested_by: i-substitution-window
  reason: >
    Derived rather than typed. If the withdrawal date is corrected the deadline
    moves with it, and every document that pinned it reports as stale.

- id: i-substitution-window
  inference: Thirty days from withdrawal, per the local rule
  derived_from: [e-counsel-withdraws]
  class: inference

- id: e-mediation
  event: Mediation scheduled
  at: 2026-06
  precision: month
  status: asserted
  about: the-matter
  attested_by: s-docket

# Year precision: the plat year is all the record gives, and rendering it as
# 1962-01-01 would invent a day nobody wrote down.
- id: e-plat-recorded
  event: The subdivision plat is recorded
  at: 1962
  precision: year
  status: asserted
  about: the-strip
  attested_by: s-plat-1962

# An APPROXIMATE date that is also DERIVED. Both qualifiers have to survive
# together: a reader needs to know the figure was computed and that it is soft,
# and dropping either turns an estimate into a deadline.
- id: d-limitations-horizon
  event: Approximate horizon for an adverse-possession claim measured from the conveyance
  at: "= e-sale.at + 3650d"
  precision: approx
  status: asserted
  about: the-strip
  attested_by: s-rule-14-18

```
