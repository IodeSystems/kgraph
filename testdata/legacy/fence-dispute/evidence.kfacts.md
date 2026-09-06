# Evidence — what is in hand, and what it proves

## Facts

```kfacts
- id: s-photos
  record: Dated photographs of the strip in use
  doc: documents/photos/strip-in-use-2025-07-11.jpg
  at: 2025-07-11
  class: record

- id: strip-used-by-both
  claim: Photographs show both households using the strip before the sale
  status: asserted
  about: the-strip
  supports: implied-easement
  attested_by: s-photos

- id: g-implied-easement-proof
  group: all
  title: What the implied easement rests on
  members: [strip-serves-both, lot-8-landlocked, strip-used-by-both, shared-use-conceded]
  attested_by: s-photos

- id: q-photo-dates
  question: Can the photograph dates be authenticated from their metadata?
  status: open
  needs: evidence
  owner: wren-bramble
  about: s-photos

# Deliberately UNOWNED. Nobody has picked this up, and that is the third bucket:
# `!question[owner=us]` silently swallows it alongside the other side's work, so
# without an unowned question the guard against that spelling cannot be tested.
- id: q-plat-original
  question: Does the county still hold the original 1962 plat mylar, or only the scan?
  status: open
  needs: evidence
  about: the-strip
  attested_by: s-county-clerk


- id: fence-line-follows-old-posts
  claim: The 2025 fence was set on the line of the older post holes, not on the deed line
  status: asserted
  about: e-fence
  attested_by: s-photos

# `needs` says what would MOVE a question, and the four kinds are not
# interchangeable: a question waiting on a document is not one waiting on a
# decision somebody has to make. Partitioning by it is how a plan separates
# "chase this" from "choose this".
- id: q-post-hole-dating
  question: Can the older post holes be dated well enough to predate the sale?
  status: open
  needs: analysis
  about: the-strip
  attested_by: s-photos

- id: q-nettle-reply
  question: Will Quill's counsel respond to the reciprocal-easement proposal before mediation?
  status: open
  needs: reply
  about: nettle
  attested_by: s-strategy

```
