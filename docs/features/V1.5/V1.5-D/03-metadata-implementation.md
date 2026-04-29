# V1.5-D Task 03: Implement Narrow Permission/Read-Model Metadata

Parent plan: `docs/features/V1.5/V1.5-D/00-current-execution-plan.md`

Objective: attach policy metadata to exported read/write surfaces without
expanding Shunter into a broad auth framework.

Implementation target:
- add a small permission/read-model metadata type
- allow reducers to carry metadata
- allow query declarations to carry metadata
- allow view declarations to carry metadata
- export metadata in the canonical contract
- keep metadata passive for tooling, docs, and generated clients

Metadata should answer small questions:
- who may call this reducer?
- who may read this query/view?
- what exported/client binding metadata is needed to represent that policy?

Representation guidance:
- prefer simple names/tags/requirements over a policy DSL
- make absent metadata explicit and deterministic
- keep metadata close to the declaration it annotates
- avoid runtime behavior changes unless separately tested and documented

Non-goals:
- broad standalone policy framework
- complex multi-tenant auth product
- runtime-blocking policy enforcement
- source-of-income or demographic access rules in docs/examples

