# The appeal track

## Facts

```kfacts
- id: s-appeal-form
  record: Northwind internal appeal form
  doc: documents/appeal-form.pdf
  class: record

- id: g-appeal-ready
  group: all
  title: The appeal is ready to file when all of these hold
  members: [a-get-network-roster, a-get-itemised-bill]
  attested_by: s-appeal-form

- id: a-get-itemised-bill
  action: Request an itemised bill from the clinic
  status: asserted
  owner: wren-bramble
  attested_by: s-appeal-form

- id: q-external-review
  question: Is external review available if the internal appeal fails?
  status: open
  needs: analysis
  owner: wren-bramble
  about: northwind

- id: escalate-to-regulator
  claim: The state insurance regulator accepts complaints after internal appeal
  status: proposed
  answers: q-external-review
  attested_by: s-plan-doc
```
