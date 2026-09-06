# Impeachment — where the other side's own record disagrees with itself

## Facts

```kfacts
- id: s-quill-depo
  utterance: Quill's deposition testimony on when the fence went up
  by: quill
  medium: deposition
  recorded: true
  doc: documents/quill-depo.pdf
  at: 2026-02-27
  class: admission

- id: quill-said-2024
  claim: Quill testified the fence went up in 2024
  status: asserted
  about: quill
  contradicts: e-fence
  attested_by: s-quill-depo
  reason: >
    The client chronology puts it in August 2025. Both sources are live, so the
    pair should surface under `claim[disputed]` rather than being silently
    resolved in favour of either.

- id: g-impeachment
  group: any
  title: Points where Quill's account conflicts with a document
  members: [quill-said-2024, exclusive-use-claimed]
  attested_by: s-quill-depo
```

## More contradictions, and the two shapes that are not the same

`disputed` is evidence pointing both ways at a claim. **Underdetermined** is a
claim resting on nothing strong enough to settle it. The remedies differ — weigh
versus go and find out — so a claim must never be reported as both.

```kfacts
- id: s-county-clerk
  record: County clerk's certified plat copy
  doc: documents/plat-certified.pdf
  at: 2026-03-04
  class: record

- id: s-rule-14-18
  document: County code 14.18, access standards
  doc: documents/code-14-18.md
  class: authority

- id: s-bramble-account
  document: Wren Bramble's own account of the strip's use
  doc: documents/chronology.md
  by: wren-bramble
  class: interested

- id: s-neighbour-hearsay
  utterance: A neighbour's recollection that the fence predates the sale
  by: quill
  medium: conversation
  doc: documents/neighbour-note.md
  class: interested
  undercut_by: s-county-clerk
  reason: >
    Undercut at the SOURCE, not by a contradicting claim. The hop
    `claim <-contradicts- claim` cannot see this; only the predicate can.

- id: fence-predates-sale
  claim: The fence predates the 2024 sale
  status: asserted
  about: the-strip
  attested_by: s-neighbour-hearsay
  # The undercut belongs on the FACT, not only on the source: `undercut_by` is
  # directional — a source contradicting this claim — and a source discredited in
  # general says nothing about which facts rest on it. Without this the claim
  # renders as flatly asserted while its only support is disputed.
  undercut_by: s-county-clerk

- id: strip-width-is-20ft
  claim: The strip is twenty feet wide
  status: asserted
  about: the-strip
  attested_by: s-county-clerk

- id: strip-width-is-16ft
  claim: The strip is sixteen feet wide
  status: asserted
  about: the-strip
  contradicts: strip-width-is-20ft
  attested_by: s-bramble-account

- id: access-width-required
  claim: County code requires a twenty-foot access easement
  status: asserted
  supports: strip-width-is-20ft
  attested_by: s-rule-14-18

- id: gate-was-locked
  claim: The gate across the strip was kept locked before the sale
  status: asserted
  about: the-strip
  attested_by: s-bramble-account
  underdetermined: >
    Rests only on an interested account with nothing on the other side. Nobody
    has contradicted it, so it is not disputed — but nothing settles it either,
    and the remedy is to go and find out rather than to weigh.

- id: gate-was-open
  claim: The gate across the strip stood open and unlatched before the sale
  status: asserted
  about: the-strip
  contradicts: gate-was-locked
  attested_by: s-neighbour-hearsay


# The mirror: a contradicts edge where one side cites NOTHING. The hop finds the
# pair, but there is no evidence pointing both ways — there is evidence on one
# side and an assertion on the other, and reporting that as disputed would flatter
# it. The gap is recorded as a question instead of being papered over.
- id: strip-never-conveyed
  claim: The strip was never conveyed to anyone and remains with the parent estate
  status: asserted
  about: the-strip
  contradicts: deed-omits-the-strip

- id: q-strip-chain
  question: Is there a recorded instrument conveying the strip, or is the chain simply silent?
  status: open
  needs: evidence
  about: the-strip
  attested_by: s-county-clerk

```
