# Shared query library

Referenced from a spec as `@name`. Written once here rather than restated in
every document that needs the same hop chain.

```kgraph
# everything gating readiness for the Sept–Oct wave
blockers:    action[status=open] -requires*-> #g-ready-for-wave @now sort id
# counsel still in play
live-leads:  entity[status=asserted] -member_of-> #g-live-leads @now sort valid_from, id
# anything a record backs, which is what a filing can lean on
record-backed: claim[status=asserted] <-attests- source[class=record] @now sort id
# work scoped to a branch we are not in
contingent:  claim[while] sort id
```
