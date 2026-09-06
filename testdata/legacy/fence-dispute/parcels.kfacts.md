# Parcels — identifiers that changed meaning

An identifier is an **alias with a time window**, not a name. In this fixture
`P-1180` denotes different land before and after the 2024 split, which is why an
undated reference to it is ambiguous and the scan says so.

## Facts

```kfacts
- id: s-assessor
  record: County assessor parcel record
  doc: documents/assessor-P1180.pdf
  at: 2026-02-04
  class: record

- id: s-plat-1962
  record: Cedar Bluff plat, 1962
  doc: documents/plat-1962.pdf
  class: record

- id: parent-parcel
  entity: The pre-2024 Bramble parent parcel, covering Lots 7 and 8
  status: withdrawn
  valid_until: 2024-05-30
  aliases:
    - as: P-1180
      until: 2024-05-30
  attested_by: s-assessor
  reason: >
    Split at the 2024 sale. Instruments written before that date and referencing
    P-1180 mean this, not Lot 7 alone.

- id: lot-7
  entity: Lot 7 — the house lot sold to Quill
  status: asserted
  valid_from: 2024-05-30
  aliases:
    - as: P-1180
      from: 2024-05-30
    - as: 411 Cedar Bluff Road
  attested_by: s-assessor

- id: lot-8
  entity: Lot 8 — retained by the Brambles, no road frontage
  status: asserted
  attested_by: s-assessor

- id: the-strip
  entity: The 20-foot gravel strip along the south edge of Lot 7
  status: asserted
  attested_by: s-plat-1962

- id: strip-serves-both
  claim: The strip is the only vehicle access to both Lot 7 and Lot 8
  status: asserted
  about: the-strip
  attested_by: s-plat-1962

- id: lot-8-landlocked
  claim: Lot 8 has no access to a public road except across the strip
  status: asserted
  about: lot-8
  supports: implied-easement
  attested_by: i-landlock

- id: i-landlock
  inference: A retained parcel with no frontage depends on the strip it was served by
  derived_from: [strip-serves-both]
  class: inference

# A fact anchored to the OLD binding. Without one, the alias window is untestable
# from the query side: both dates would reach the same (empty) set and a broken
# time-binding would look identical to a working one.
- id: permit-against-parent
  claim: The 1998 driveway permit was written against the parent parcel, before any lot line existed
  status: asserted
  about: parent-parcel
  attested_by: s-county-clerk

```
