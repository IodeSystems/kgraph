# Posture — what to do, in what order, and the branch nobody has chosen

`requires` records real blocking, not priority — sequence belongs to an
`ordered` group. `options` marks a decision the client owns; both branches carry
scoped work, so neither can be reported as unplanned.

## Facts

```kfacts
- id: s-strategy
  document: Strategy note
  doc: documents/strategy.md
  at: 2026-03-02
  class: document

# ── the decision, and its two planned branches ─────────────────────────
- id: q-settle-or-try
  question: Settle at mediation, or take the boundary to trial?
  status: open
  owner: wren-bramble
  options: [opt-settle, opt-try]
  exhaustive: true
  needs: decision
  attested_by: s-strategy

- id: opt-settle
  claim: Resolve at mediation with a recorded reciprocal easement
  status: proposed
  attested_by: s-strategy

- id: opt-try
  claim: Try the boundary and seek reformation
  status: proposed
  attested_by: s-strategy

- id: a-draft-easement
  action: Draft the reciprocal easement and a legal description for it
  status: asserted
  while: opt-settle
  owner: nettle
  attested_by: s-strategy

- id: a-retain-surveyor
  action: Retain an independent surveyor to plot the strip against both readings
  status: asserted
  while: opt-try
  owner: wren-bramble
  answers: q-geometry-proof
  attested_by: s-strategy

# ── work that is genuinely blocked ─────────────────────────────────────
# The edge carries its OWN window, in long form. Leave may be sought until the
# pleading cut-off and not after, and that expiry belongs on the dependency
# rather than on either node: the conflict check does not expire, and the motion
# does not stop existing — only the route between them closes.
- id: a-amend-answer
  action: Move to amend the answer to add counterclaims
  status: asserted
  owner: dace
  requires:
    - id: a-conflict-check
      valid_until: 2026-09-30
    # A boundary counterclaim cannot be pleaded before the geometry is settled,
    # so the motion inherits that question as a hole. This is what a pin has to
    # record: the plan is renderable, and it rests on something unanswered.
    - id: q-geometry-proof
  attested_by: s-strategy

- id: a-conflict-check
  action: Run a conflict check before new counsel is engaged
  status: asserted
  owner: wren-bramble
  requires: g-counsel-leads
  attested_by: s-strategy
  reason: >
    Two hops from a-amend-answer, deliberately: the closure tests need a chain
    that a single hop does not reach.

- id: g-counsel-leads
  group: ">= 1"
  members: [holt-pike, larkspur-llp, ferris-oak]
  attested_by: s-strategy
  reason: Candidate firms; at least one must respond before counsel is engaged.

- id: holt-pike
  entity: Holt & Pike (candidate counsel, no conflict)
  status: asserted
  attested_by: s-strategy

- id: larkspur-llp
  entity: Larkspur LLP (candidate counsel, declined for capacity)
  status: asserted
  attested_by: s-strategy

- id: ferris-oak
  entity: Ferris Oak (candidate counsel, awaiting callback)
  status: asserted
  attested_by: s-strategy

- id: a-serve-subpoena
  action: Subpoena the county for the 1962 plat file
  status: withdrawn
  owner: wren-bramble
  attested_by: s-strategy
  reason: >
    Withdrawn once a certified copy was available over the counter. Kept as a
    withdrawn action so the kind-alternation query has both a claim and an
    action to find.

- id: a-pull-plat
  action: Obtain a certified copy of the 1962 plat
  status: asserted
  owner: wren-bramble
  attested_by: s-strategy

# ── sequence is not dependency ─────────────────────────────────────────
- id: g-before-mediation
  group: all
  title: Everything that must be done before mediation, in order
  members: [a-conflict-check, a-pull-plat, a-retain-surveyor]
  ordered: true
  attested_by: s-strategy

- id: g-ready-to-amend
  group: all
  title: The amendment is ready when all of these hold
  members: [a-conflict-check, a-pull-plat]
  attested_by: s-strategy

# ── a superseded fact, kept resolvable ─────────────────────────────────
- id: mediation-in-april
  claim: Mediation is set for April 2026
  status: withdrawn
  attested_by: s-strategy
  reason: Rescheduled; kept so documents that cited April report as stale.

- id: mediation-in-june
  claim: Mediation is set for June 2026
  status: asserted
  supersedes: mediation-in-april
  attested_by: s-docket

# ── a tested-and-rejected position, kept so prohibits has a target ─────
- id: adverse-possession
  claim: The Brambles hold the strip by adverse possession
  status: false
  attested_by: s-strategy
  reason: >
    Use was shared and permissive, so it cannot ripen. Kept as `false` rather
    than deleted: it is the target of `prohibits` below, and documents that
    render the theory must not churn when a rejected branch is revisited.

- id: plead-shared-use
  claim: The defence pleads shared permissive use
  status: asserted
  prohibits: adverse-possession
  attested_by: s-strategy
```
