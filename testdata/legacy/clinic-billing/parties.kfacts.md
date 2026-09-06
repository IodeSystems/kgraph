# Parties — and the shared person that motivates indexes

## Facts

```kfacts
- id: s-plan-doc
  document: Northwind plan certificate
  doc: documents/plan-certificate.pdf
  class: document

- id: provider-npi
  claim: The clinic bills under a single group NPI
  status: asserted
  about: clinic
  attested_by: s-plan-doc

- id: plan-is-ppo
  claim: The plan is a PPO with out-of-network benefits
  status: asserted
  about: northwind
  supports: partial-coverage-owed
  attested_by: s-plan-doc

- id: partial-coverage-owed
  claim: Some out-of-network benefit is owed even if the provider was out of network
  status: asserted
  attested_by: i-ppo
  reason: Holds on either answer to the network question, so it is not gated on it.

- id: i-ppo
  inference: A PPO pays out-of-network at a reduced rate rather than nothing
  derived_from: [plan-is-ppo]
  class: inference
```
