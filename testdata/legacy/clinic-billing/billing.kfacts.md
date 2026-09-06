# Clinic billing — a second matter, sharing a person with the first

This exists to make the index rule concrete. **Wren Bramble is the patient here
and a defendant in `../fence-dispute`.** Same human, modelled once per matter,
because the two graphs answer different questions and must not contaminate each
other's computed state.

Before indexes, one graph over `examples/` meant a contradiction in one matter
could mark a claim disputed in the other, and editing one could move the other's
`sem_hash`. Each directory now carries a `.kg-index`, so verification is closed
over the matter it belongs to.

## Facts

```kfacts
- id: s-eob
  record: Explanation of benefits, Northwind Health
  doc: documents/eob-2026-01.pdf
  at: 2026-01-22
  class: record

- id: s-statement
  record: Clinic statement
  doc: documents/statement-2026-02.pdf
  at: 2026-02-15
  class: record

- id: s-call-note
  utterance: Northwind representative on the network status of the provider
  by: northwind
  medium: phone
  doc: documents/call-2026-02-20.md
  at: 2026-02-20
  class: admission

# ── the parties ────────────────────────────────────────────────────────
- id: wren-bramble
  entity: Wren Bramble (patient)
  status: asserted
  attested_by: s-eob
  reason: >
    The same person as `wren-bramble` in the fence-dispute index. Deliberately a
    separate node: an id is only unique within its index, and merging the two
    would let one matter's evidence bear on the other's conclusions.

- id: clinic
  entity: Lakeshore Clinic (provider)
  status: asserted
  attested_by: s-statement

- id: northwind
  entity: Northwind Health (insurer)
  status: asserted
  attested_by: s-eob

# ── the numbers ────────────────────────────────────────────────────────
- id: billed-amount
  claim: The clinic billed 1240.00 for the visit
  status: asserted
  value: 1240.00
  unit: USD
  about: clinic
  attested_by: s-statement

- id: allowed-amount
  claim: Northwind allowed 380.00
  status: asserted
  value: 380.00
  unit: USD
  about: northwind
  attested_by: s-eob

- id: balance
  claim: The balance billed to the patient
  status: asserted
  value: "= billed-amount.value - allowed-amount.value"
  unit: USD
  about: wren-bramble
  attested_by: i-balance
  reason: >
    Derived, not typed. Correct either input and the balance recomputes, and any
    document that pinned the figure reports as stale.

- id: i-balance
  inference: Billed less allowed is what was passed to the patient
  derived_from: [billed-amount, allowed-amount]
  class: inference

# ── the dispute ────────────────────────────────────────────────────────
- id: provider-out-of-network
  claim: The provider was out of network on the date of service
  status: asserted
  about: clinic
  attested_by: s-eob

- id: rep-said-in-network
  claim: A Northwind representative stated the provider was in network
  status: asserted
  about: northwind
  contradicts: provider-out-of-network
  attested_by: s-call-note

- id: q-network-status
  question: Was the provider in network on the date of service?
  status: open
  needs: evidence
  owner: wren-bramble
  about: clinic

- id: e-eob-issued
  event: Northwind issues the explanation of benefits
  at: 2026-01-22
  status: asserted
  about: northwind
  attested_by: s-eob
  causes: d-appeal-deadline

- id: d-appeal-deadline
  event: Internal appeal deadline
  at: "= e-eob-issued.at + 180d"
  status: asserted
  about: northwind
  attested_by: i-appeal-window

# A date derived from a DERIVED date. The deadline is itself computed from the
# EOB, so this exercises the chain rather than a single hop off an authored date
# — the case where a stale intermediate would silently poison everything after it.
- id: d-collections
  event: The clinic refers the balance to collections
  at: "= d-appeal-deadline.at + 30d"
  status: asserted
  about: clinic
  attested_by: s-statement

- id: d-escalate-warning
  event: Last date to escalate before the balance is referred
  at: "= d-collections.at - 14d"
  status: asserted
  about: wren-bramble
  attested_by: s-statement

- id: i-appeal-window
  inference: One hundred and eighty days from the EOB, per the plan documents
  derived_from: [e-eob-issued]
  class: inference

- id: a-file-appeal
  action: File the internal appeal
  status: asserted
  owner: wren-bramble
  requires: a-get-network-roster
  attested_by: s-eob

- id: a-get-network-roster
  action: Obtain the network roster as it stood on the date of service
  status: asserted
  owner: wren-bramble
  answers: q-network-status
  attested_by: s-eob
```
