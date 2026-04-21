# P1-07 protocol forwarding cancel-safe slice

Goal: continue P1-07 with one narrow follow-up slice that reduces response-channel blocking assumptions in the protocol inbox adapter without changing executor reply semantics.

Chosen sub-slice:
- Harden `executor/protocol_inbox_adapter.go` so `forwardReducerResponse` does not block forever on `req.ResponseCh` after the caller context is canceled.

Scope:
- Add targeted tests in `executor/protocol_inbox_adapter_test.go` for blocked outbound reducer-response forwarding with context cancellation.
- Update `executor/protocol_inbox_adapter.go` to send reducer updates to `req.ResponseCh` via a context-aware helper.
- Preserve existing successful delivery behavior when the response channel is ready.
- Do not change executor-side blocking semantics in `sendReducerResponse` / `sendProtocolCallReducerResponse` in this slice.

Behavioral invariants:
- Executor still replies into its own response channels exactly as before.
- Protocol adapter still forwards heavy/light reducer results when the caller response channel is ready.
- If the caller context is canceled while the protocol adapter is blocked on forwarding, the forwarding goroutine exits instead of waiting forever.

Validation:
- `rtk go fmt ./executor`
- targeted protocol adapter tests for cancel-safe forwarding
- touched-package tests: `rtk go test ./executor -count=1`
