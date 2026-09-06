# Case builder — moved

This design lives in its own repository now: **`iodesystems/caselit`**.

- `caselit/docs/design.md` — what it is, why, the pieces, the risk, sequencing,
  and the 2026-08-03 addendum on the construction model and the approval loop.
- `caselit/docs/architecture.md` — components, transports, the write gate,
  storage, roles and deployment.

Kept as a pointer rather than deleted because kgraph commits reference this
path, and because the reason caselit is a separate product is a fact about
kgraph: story-first and graph-is-truth are opposite directions of travel, and
putting both in this tool would break a live matter that depends on the
current one.
