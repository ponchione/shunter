# P1-07 executor submit-time response-channel validation

Goal: execute one narrow executor-side follow-up slice for P1-07 by preventing public Submit/SubmitWithContext callers from stalling the executor with unread unbuffered reply channels.

Chosen sub-slice:
- Keep executor reply sends blocking once a command is admitted.
- Reject unbuffered public reply channels at submission time for executor-owned channel-based commands.

Scope:
- Add targeted tests in `executor/executor_test.go` for `Submit`/`SubmitWithContext` returning a new error when given unbuffered reply channels.
- Add a new exported executor error in `executor/errors.go`.
- Add a small private validator in `executor/executor.go` that checks channel-based commands before enqueue.
- Do not alter post-commit ordering, panic/fatal response behavior, or protocol wire delivery.

Behavioral invariants:
- Admitted commands still use blocking reply sends exactly as before.
- Buffered reply channels remain accepted.
- Nil reply channels remain accepted.
- Public callers handing unbuffered executor reply channels get an immediate error instead of a latent executor stall.

Validation:
- `rtk go fmt ./executor`
- targeted tests for new submit-time rejection behavior
- touched-package tests: `rtk go test ./executor -count=1`
