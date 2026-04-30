# Hosted Runtime V2-E Current Execution Plan

Status: complete

Goal: turn passive v1.5 permission metadata into a narrow, testable
policy/auth enforcement foundation where real identity data supports it.

V2-E target:
- preserve dev-friendly local defaults
- define a small claims/permission model compatible with existing JWT identity
  validation
- enforce reducer permissions before widening to read permissions
- keep policy attached to reducers, queries, and views
- keep broad standalone policy frameworks out of scope

Task sequence:
1. Reconfirm auth, caller identity, reducer, declaration, and contract metadata
   surfaces.
2. Add failing tests for reducer permission enforcement and dev/strict behavior.
3. Implement the smallest permission claim extraction and enforcement path.
4. Extend enforcement to declared reads/raw SQL using the declared-read model.
5. Format and validate V2-E gates.

Scope boundaries:
- In scope: narrow permission tags, JWT claim extraction if needed, local-call
  and protocol reducer checks, contract/codegen metadata consistency.
- Out of scope: tenant framework, role database, external IdP integration
  beyond current JWT validation, broad policy language, multi-module scoping.

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V2-E plan as a live handoff; use
`docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.

## Completion Proof

- `auth.Claims` now carries optional `permissions` JWT claim tags while
  preserving existing identity, issuer, audience, expiry, and hex-identity
  validation.
- `types.CallerContext` carries permission tags and an explicit dev/anonymous
  all-permissions flag.
- Local calls in `AuthModeDev` keep a dev-friendly default by allowing all
  permissions unless the caller explicitly supplies tags with
  `WithPermissions(...)`.
- Local calls in `AuthModeStrict` require explicit supplied permission tags for
  protected reducers.
- Protocol upgrades copy validated claim permission tags; anonymous/dev
  protocol connections get the explicit all-permissions flag.
- `protocol.CallReducerRequest` and `executor.ProtocolInboxAdapter` forward
  permission context into reducer execution.
- `Build` copies reducer permission metadata into the runtime-owned executor
  reducer registry.
- The executor rejects external reducer calls missing required tags with
  `ErrPermissionDenied` and `StatusFailedPermission` before reducer user code
  or transaction creation.
- Permission metadata remains exported through canonical contracts and
  generated clients as before.
- Read permission enforcement is deferred: V2-D SQL-backed generated helpers
  still execute through raw SQL protocol routes, so correct read enforcement
  needs table/read-model policy rather than declaration-name checks alone.

## Validation

- `rtk go fmt . ./auth ./protocol ./executor ./codegen ./types`
- `rtk go test . -run 'Test.*(Permission|Auth|Reducer|Local|Network)' -count=1`
- `rtk go test ./auth ./protocol ./executor ./codegen ./types -count=1`
- `rtk go vet . ./auth ./protocol ./executor ./codegen ./types`
- `rtk go test ./... -count=1`
