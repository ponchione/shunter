# V1.5-C Task 01: Reconfirm Codegen Prerequisites

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`

Objective: verify client binding generation is built on the V1.5-B contract,
not hidden runtime state.

Checks:
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./schema SchemaExport`

Relevant docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md#12-client-code-generation-interface`
- `docs/decomposition/006-schema/epic-6-schema-export/story-6.3-codegen-tool-contract.md`
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`

Prerequisite conclusions to record in Task 01:
- codegen input is the canonical full module contract
- TypeScript is the documented first language target from schema export history
- generated reducer calls may remain raw-byte until typed reducer argument
  metadata exists
- generated query/view bindings should reflect V1.5-A declarations
- the generator must not depend on a live runtime process

