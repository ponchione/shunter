# V1.5-B Task 03: Implement Contract Export Assembly

Parent plan: `docs/features/V1.5/V1.5-B/00-current-execution-plan.md`

Objective: assemble a detached full module contract from existing module,
schema, reducer, query, and view metadata.

Implementation target:
- add a root-package `ModuleContract` or equivalent exported contract type
- add contract export from built runtimes
- keep schema export intact for lower-level consumers
- normalize field ordering before JSON serialization
- include explicit version fields for module version and schema/contract version
- include reserved permission and migration sections without implementing their
  full semantics yet
- include codegen/export metadata that records the artifact format/version

Suggested method shape:
- `Runtime.ExportContract() ModuleContract`

If a different name is chosen, update this plan and the V1.5 docs in the same
slice.

Contract model guidance:
- canonical contract should be the full module artifact, not only a public
  client surface
- JSON tags should be stable and review-friendly
- empty slices should prefer deterministic empty arrays where useful for review
  diffs
- maps should be copied and serialized deterministically by construction or by
  canonical JSON output

Non-goals:
- client binding generation
- runtime migration enforcement
- permission evaluation
- server/module implementation generation

