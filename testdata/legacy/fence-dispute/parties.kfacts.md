# Parties — who is involved, and in what capacity

A worked fixture, not a real matter. Every person, firm and parcel here is
invented; see `../README.md`.

Roles are entities so a document can ask *what do we know about this person*, and
so an adverse witness and the party who called them are separable. Contact
details are ordinary claims `about` an entity rather than an `attrs:` bag — a
phone number read from a source is a fact like any other, and should be sourced,
diffable and correctable through `supersedes`.

## Facts

```kfacts
# ── sources ────────────────────────────────────────────────────────────
- id: s-roster
  document: Party roster kept by the client
  doc: documents/roster.md
  at: 2026-03-02
  class: document

- id: s-complaint
  record: Complaint, Quill v. Bramble
  doc: documents/complaint.pdf
  at: 2025-11-14
  class: record

- id: s-survey-decl
  record: Declaration of T. Vole, licensed surveyor
  by: vole
  doc: documents/vole-declaration.pdf
  at: 2026-01-20
  class: record

# ── the matter ─────────────────────────────────────────────────────────
- id: the-matter
  entity: Quill v. Bramble — boundary and access dispute
  status: asserted
  aliases:
    - as: CV-2025-00418
  attested_by: s-complaint

# ── people ─────────────────────────────────────────────────────────────
- id: quill
  entity: Ada Quill (plaintiff, bought Lot 7 in 2024)
  status: asserted
  attested_by: s-complaint

- id: wren-bramble
  entity: Wren Bramble (defendant, seller of Lot 7)
  status: asserted
  attested_by: s-complaint

- id: sam-bramble
  entity: Sam Bramble (defendant, spouse and co-owner)
  status: asserted
  attested_by: s-complaint

- id: vole
  entity: Terrence Vole (licensed surveyor, retained by both sides at different times)
  status: asserted
  attested_by: s-survey-decl

- id: dace
  entity: Owen Dace (counsel for the Brambles, withdrew 2026-02)
  status: asserted
  valid_until: 2026-02-11
  attested_by: s-roster

- id: nettle
  entity: Marta Nettle (counsel for Quill)
  status: asserted
  attested_by: s-complaint

# ── contact details, as sourced claims ─────────────────────────────────
- id: vole-phone
  claim: Vole's office line is 555-0143
  status: asserted
  about: vole
  attested_by: s-roster

# ── the conflict the index scheme exists to illustrate ─────────────────
# Wren Bramble is also the patient in examples/clinic-billing. Same human, two
# matters, deliberately modelled once per matter. Before indexes, a single graph
# over examples/ let edges cross that seam.
- id: bramble-is-shared
  claim: Wren Bramble appears in an unrelated billing matter under a separate index
  status: asserted
  about: wren-bramble
  attested_by: s-roster
  reason: >
    Recorded so the cross-matter overlap is visible from inside this index
    without reaching into the other one. Nothing here resolves against
    clinic-billing; the note is prose, not an edge.

# ── the surveyor worked both sides ─────────────────────────────────────
- id: vole-both-sides
  claim: Vole surveyed for the Brambles in 2023 and declared for Quill in 2026
  status: asserted
  about: vole
  attested_by: s-survey-decl

- id: vole-not-our-witness
  claim: Vole should not be called as a defence witness
  status: asserted
  about: vole
  attested_by: i-vole-conflicted

- id: i-vole-conflicted
  inference: A surveyor who declared for the opposing party is not a defence witness
  derived_from: [vole-both-sides]
  class: inference

# The gap left by NOT drawing `contradicts` between Vole's two positions. Both
# are true — he did survey for one side and later declare for the other — so an
# edge asserting they conflict would make both compute as disputed and tell a
# document to hedge about settled facts. What is actually unknown is narrower,
# and it is recorded as a question instead of being smuggled in as an edge.
- id: q-vole-declaration-substance
  question: Where exactly does Vole's 2026 declaration depart from his own 2023 survey?
  status: open
  needs: analysis
  about: vole
  attested_by: s-vole-2026

```
