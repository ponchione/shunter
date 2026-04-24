# V1-H Task 03: Add the end-to-end WebSocket proof test

Parent plan: `docs/hosted-runtime-planning/V1-H/2026-04-23_214356-hosted-runtime-v1h-hello-world-replacement-v1-proof-implplan.md`

Objective: preserve the behavioral proof while replacing the old manual architecture.

Files:
- Modify `cmd/.../main_test.go` for the hosted example

Test coverage to add or preserve:
- cold boot then recovery against the same data directory
- anonymous/dev WebSocket admission and token handshake
- subscription to `SELECT * FROM greetings`
- reducer call to `say_hello` over protocol messages
- non-caller subscriber receives `TransactionUpdateLight` with greeting inserts
- context cancellation shuts the example down cleanly

Implementation guidance:
- use existing protocol encode/decode helpers instead of inventing new test protocols
- the external proof path must stay WebSocket-first, not local-call-only
