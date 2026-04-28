# V2-D Task 01: Reconfirm Declared Read And SQL Prerequisites

Parent plan: `docs/hosted-runtime-planning/V2/V2-D/00-current-execution-plan.md`

Objective: verify the live read surfaces before designing convergence.

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

Stop if:
- protocol SQL tests are failing
- query/view contract export is unstable
