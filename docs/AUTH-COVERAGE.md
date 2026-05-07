# Auth Coverage Audit

Status: current v1 strict-auth coverage audit
Scope: dev and strict auth, protocol upgrade, local reducers, local and
protocol reads, permissions, and visibility filters.

This audit records the current code reality behind the strict-auth roadmap.
`docs/authentication.md` remains the operator and app-author contract; this file
tracks where that contract is proved.

## Current Contract

- Strict protocol auth requires `AuthModeStrict` and a configured
  `AuthSigningKey`.
- Strict tokens are HS256 JWTs with required `iss` and `sub` claims.
- `AuthIssuers` and `AuthAudiences` are allowlists when configured.
- Expired, future-issued, not-yet-valid, malformed, wrong-algorithm,
  bad-signature, audience-mismatched, issuer-mismatched, and missing-claim
  tokens fail before WebSocket upgrade.
- Permission admission uses runtime caller permissions. Protocol strict-mode
  callers receive permissions from the token `permissions` claim; local callers
  must supply permission options explicitly.
- Visibility filters use the caller identity through `:sender`.

## Covered Surfaces

- JWT validation and strict config: `auth/*_test.go`, `network_test.go`, and
  `root_validation_test.go`.
- Protocol upgrade and principal propagation: `protocol/upgrade_test.go`,
  `protocol/handle_callreducer_test.go`, and `protocol/lifecycle_test.go`.
- Local reducer admission: `local_test.go`.
- Local declared queries, declared views, table-read permissions, and
  visibility filters: `declared_read_test.go`.
- Protocol declared-read admission: `declared_read_protocol_test.go`.
- Raw one-off SQL read authorization and visibility expansion:
  `protocol/handle_oneoff_test.go`, `protocol/visibility_expansion_test.go`,
  and `read_auth_gauntlet_test.go`.
- Raw subscription visibility and deltas:
  `protocol/visibility_expansion_test.go` and `read_auth_gauntlet_test.go`.
- Strict-auth public-runtime workload: `rc_app_workload_test.go`.

## Remaining v1 Gap

The remaining auth work is not another isolated auth primitive. It is a
maintained reference-app example that uses realistic non-dev tokens and proves
the recommended production setup through the normal app, protocol, and client
workflow.
