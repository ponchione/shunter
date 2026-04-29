# V2-E Task 01: Reconfirm Policy/Auth Prerequisites

Parent plan: `docs/features/V2/V2-E/00-current-execution-plan.md`

Objective: verify the current identity and passive metadata surfaces before
adding enforcement.

Checks:
- `rtk go doc ./auth Claims`
- `rtk go doc ./auth ValidateJWT`
- `rtk go doc ./types CallerContext`
- `rtk go doc ./types ReducerContext`
- `rtk go doc . PermissionMetadata`
- `rtk go doc . WithReducerPermissions`
- `rtk go doc . QueryDeclaration`
- `rtk go doc . ViewDeclaration`
- `rtk go doc . Config`
- `rtk go doc . Runtime.CallReducer`
- `rtk go doc ./protocol Server`

Read only if needed:
- `auth/jwt.go`
- `runtime_network.go`
- `runtime_local.go`
- `executor/`
- `protocol/handle_call_reducer.go`
- `module_declarations.go`
- `runtime_contract.go`

Prerequisite conclusions to record in Task 01:
- `auth.Claims` currently carries identity-oriented JWT fields, not permission
  tags
- local calls currently provide identity/connection context but no permission
  claims
- reducer/query/view permission metadata is passive and exported
- strict auth already requires signing material
- V2-E must define default local/dev behavior explicitly

Stop if:
- current auth or reducer call tests are failing
- V2-D left read execution semantics unresolved and the slice attempts read
  enforcement anyway

## Completion Notes

- `auth.Claims` previously carried identity-oriented JWT fields only; V2-E
  added permission tags from a narrow `permissions` claim.
- Local calls previously supplied identity/connection context only; V2-E added
  explicit local permission options plus a dev default.
- Reducer/query/view permission metadata was passive and remains exported in
  contracts/codegen.
- Strict auth already required signing material; V2-E keeps strict protocol
  permission data tied to validated claims.
- Current read execution uses raw SQL protocol paths for generated helpers, so
  declaration-level read enforcement is deferred rather than guessed.
