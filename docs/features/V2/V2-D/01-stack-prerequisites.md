# V2-D Task 01: Reconfirm Declared Read And SQL Prerequisites

Parent plan: `docs/features/V2/V2-D/00-current-execution-plan.md`

Objective: verify the live read surfaces before designing convergence.

Status: complete.

Checks:
- `rtk go doc . QueryDeclaration`
- `rtk go doc . ViewDeclaration`
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./query/sql`
- `rtk go doc ./protocol OneOffQueryMsg`
- `rtk go doc ./protocol SubscribeSingleMsg`
- `rtk go doc ./protocol SubscribeMultiMsg`
- `rtk go doc ./subscription Manager`
- `rtk go doc ./subscription Predicate`

Read only if needed:
- `module_declarations.go`
- `runtime_contract.go`
- `protocol/handle_oneoff.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `query/sql/`
- `subscription/`

Prerequisite conclusions to record in Task 01:
- query/view declarations currently export names and metadata only
- protocol one-off and subscription reads execute SQL strings
- the SQL grammar is intentionally minimum viable
- subscription registration/evaluation is already substantial and should not be
  bypassed casually
- V2-D should define source-of-truth rules before adding features

Recorded conclusions:
- live `QueryDeclaration` and `ViewDeclaration` previously carried only names,
  passive permission/read-model metadata, and migration metadata.
- protocol `OneOffQuery`, `SubscribeSingle`, and `SubscribeMulti` were already
  SQL-string execution surfaces through `query/sql` and the protocol compiler.
- subscription registration and evaluation remain the runtime execution path for
  live views; V2-D reuses its admission compiler instead of adding a parallel
  evaluator.
- the chosen source-of-truth rule is optional declaration SQL metadata: present
  SQL is executable and validated at build time; absent SQL remains
  metadata-only.

Stop if:
- protocol SQL tests are failing
- query/view contract export is unstable
