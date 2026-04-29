# Story 3.5: Shutdown

**Epic:** [Epic 3 — Executor Core](EPIC.md)  
**Spec ref:** SPEC-003 §2.2, §7, §13.2  
**Depends on:** Stories 3.3, 3.4  
**Blocks:** Nothing

---

## Summary

Graceful executor shutdown: stop accepting new commands, drain remaining, terminate run loop, and preserve the required ordering against durability teardown.

## Deliverables

- ```go
  func (e *Executor) Shutdown()
  ```
  - Acquire `e.submitMu.Lock()` before flipping the shutdown flag and before closing `e.inbox`; release only after the close is visible to Submit paths
  - Set shutdown flag (prevents new Submit calls)
  - Close `e.inbox` channel
  - Wait for `Run` goroutine to finish (via done channel or WaitGroup)
  - Executor must stop accepting new writes BEFORE durability subsystem is torn down
  - Shutdown API must make the stop-admit → drain → durability-close ordering explicit to its caller

- Shutdown ordering contract:
  1. `Shutdown()` called
  2. Submit returns `ErrExecutorShutdown` for any new commands
  3. `Run` drains remaining commands in inbox
  4. `Run` returns
  5. Only after step 4 may engine shutdown code call `DurabilityHandle.Close()`
  6. Engine shutdown surfaces final durable TxID / latched durability error through its chosen API

## Acceptance Criteria

- [ ] After Shutdown, Submit returns `ErrExecutorShutdown`
- [ ] Run processes remaining queued commands before returning
- [ ] Shutdown blocks until Run exits
- [ ] No commands lost between shutdown signal and drain completion
- [ ] Closing inbox terminates run loop cleanly (no spin, no panic)
- [ ] Double Shutdown is safe (idempotent)
- [ ] Durability teardown is ordered strictly after stop-admit and drain completion
- [ ] Engine shutdown path surfaces the final durable TxID and any latched durability error through its chosen API

## Design Notes

- The executor story owns the shutdown-ordering contract even if the final `DurabilityHandle.Close()` call lives in a higher-level engine lifecycle object. Otherwise the executor decomposition leaves §7 / §13.2 without a clear implementation owner.
- One acceptable implementation shape is: executor `Shutdown()` handles stop-admit + drain, and engine `Close()` then calls `DurabilityHandle.Close()` and returns the final durable TxID / latched error. Another is a single higher-level close method that performs all three phases. The contract matters more than the exact public method split.
