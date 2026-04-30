# Hosted Runtime V2-D Current Execution Plan

Goal: reconcile v1.5 named query/view declarations with the live protocol SQL
read path before either surface grows larger.

Status: complete.

V2-D target:
- define how declared queries/views relate to executable protocol reads
- preserve existing OneOffQuery and Subscribe SQL behavior
- avoid permanent divergence between named declarations, generated clients, and
  raw SQL protocol usage
- keep SQL grammar expansion out of scope unless required by the chosen
  declaration model

Task sequence:
1. Reconfirm query/view declaration, protocol SQL, and subscription evaluator
   contracts.
2. Add failing tests that expose the current divergence between declarations
   and executable reads.
3. Implement the smallest declaration-to-execution relationship chosen for
   v2.
4. Update contract/codegen output only if the executable read model adds stable
   exported metadata.
5. Format and validate V2-D gates.

Scope boundaries:
- In scope: named read/view semantics, raw SQL coexistence rules, generated
  client expectations, protocol compatibility.
- Out of scope: broad SQL expansion, cross-language query runtime, policy
  enforcement, multi-module routing, process isolation.

Completed live proof:
- `QueryDeclaration.SQL` and `ViewDeclaration.SQL` define optional executable
  SQL targets for named read declarations.
- metadata-only declarations remain supported, but generated TypeScript clients
  no longer emit executable helpers unless SQL metadata is present.
- `Build` validates SQL-backed queries and views against the same protocol SQL
  compiler used by `OneOffQuery` and subscription admission.
- query SQL allows one-off query grammar, including projection and `LIMIT`;
  view SQL uses subscription grammar and rejects projection/`LIMIT`.
- `Runtime.ExportContract` and canonical JSON include declaration SQL metadata
  when present.
- `contractdiff.Compare` reports declaration SQL additions as additive and
  removals/changes as breaking.
- raw SQL protocol one-off and subscription behavior remains unchanged.
- no broad SQL grammar expansion, policy enforcement, multi-module routing, or
  process isolation was added.

Validation passed:
- `rtk go fmt . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test . -run 'Test.*(Declaration|Contract|Read|Query|View)' -count=1`
- `rtk go test ./protocol ./query/sql ./subscription -count=1`
- `rtk go test ./codegen ./contractdiff -count=1`
- `rtk go vet . ./protocol ./query/sql ./subscription ./codegen ./contractdiff`
- `rtk go test ./... -count=1`

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V2-D plan as a live handoff; use
`docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.
