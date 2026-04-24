# Phase 2 Slice 3 — lag / slow-client policy

Status: closed for the current parity target, with one explicit
mechanism divergence.

This is now a compact decision record. The older source-reading and
session-plan detail was removed because the behavior is pinned in live
protocol tests and summarized in `docs/parity-phase0-ledger.md`.

## Current contract

- `DefaultOutgoingBufferMessages` is `16 * 1024`, matching the
  reference per-client outbound channel capacity.
- Outbound queue overflow disconnects the client.
- Fan-out cleanup treats send-buffer overflow as a dropped-client path
  so subscription state is reclaimed.
- Incoming request backpressure remains a Shunter-specific defensive
  limit and is not part of this outbound lag parity slice.

Authoritative pins:

- `protocol/options.go`
- `protocol/options_test.go`
- `protocol/parity_lag_policy_test.go`
- `protocol/backpressure_out_test.go`
- `subscription/fanout_worker.go`

## Accepted divergence

The reference aborts the per-client actor and lets the socket disappear
without a clean WebSocket close frame. Shunter sends an explicit
WebSocket `1008` policy-violation close with reason `send buffer full`.

The externally visible outcome is still disconnect-on-lag with server
side cleanup. Reopen this only if a real client or compatibility target
needs the reference's unclean close mechanism.

## Reading rule

Use this document only for the outbound lag-policy decision. For
current scenario status, use `docs/parity-phase0-ledger.md`.
