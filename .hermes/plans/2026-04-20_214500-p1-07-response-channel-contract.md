# P1-07 response-channel contract slice

Goal: execute the next bounded Shunter performance-audit slice by pinning and documenting the executor response-channel contract without changing broader executor scheduling or protocol semantics.

Chosen slice: P1-07 is still the best next slice after inspection. It is narrower and lower-risk than the remaining larger P1 items, and the live code confirms the sharp edge is real: executor response helpers perform unconditional blocking sends while the protocol adapter currently relies on buffered size-1 channels.

Scope:
- Modify `executor/command.go` to document the required response-channel contract for executor-owned reply channels.
- Add targeted contract tests in `executor/executor_test.go` or the existing executor test surface to prove unbuffered response sends block until a receiver is ready.
- Extend `executor/protocol_inbox_adapter_test.go` to pin that the adapter submits buffered size-1 response channels for lifecycle and reducer calls.
- Do not change unrelated behavior, scheduler flow, or broader protocol delivery.

Behavioral invariants:
- Executor reducer-response delivery remains blocking when a non-nil response channel is provided.
- ProtocolInboxAdapter continues to protect executor-owned reply paths by supplying buffered size-1 channels.
- No wire-format, transaction, or post-commit behavior changes.

Validation:
- `rtk go fmt ./executor`
- targeted tests for the new contract cases
- touched-package tests: `rtk go test ./executor -count=1`
