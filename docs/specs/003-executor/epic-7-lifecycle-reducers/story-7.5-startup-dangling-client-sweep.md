# Story 7.5: Startup Dangling-Client Sweep

**Epic:** [Epic 7 — Lifecycle Reducers & Client Management](EPIC.md)  
**Spec ref:** SPEC-003 §10.6  
**Depends on:** Stories 7.1, 7.3, Epic 6 Story 6.5, Epic 3 Story 3.6  
**Blocks:** first external command acceptance

---

## Summary

After recovery, before the engine accepts new external commands, sweep any surviving `sys_clients` rows so crash-leftover clients do not remain visible forever.

## Deliverables

- Startup sweep flow:
  1. Acquire a committed read view after recovery completes
  2. Scan surviving `sys_clients` rows
  3. For each row, invoke the OnDisconnect cleanup path (or its equivalent internal helper) so the row is deleted using the same semantics as Story 7.3
  4. Finish the full sweep before external reducer calls or subscription-registration commands are admitted

- Interaction contract:
  - scheduler replay (Story 6.5) runs before this sweep
  - this sweep runs before scheduler/executor first accept (Story 3.6)
  - the sweep is restart-only maintenance; it is not part of the steady-state protocol disconnect path

## Acceptance Criteria

- [ ] Surviving `sys_clients` rows after recovery are detected and cleaned up before first accept
- [ ] Sweep reuses Story 7.3 cleanup semantics rather than inventing a second row-deletion contract
- [ ] No external reducer or subscription-registration command may interleave ahead of the sweep
- [ ] Empty `sys_clients` table is a no-op
- [ ] Sweep ordering is documented relative to scheduler replay and executor startup

## Design Notes

- This story is the recovery-side complement to Story 7.3's guaranteed cleanup rule. Crash-only leftovers should not require manual operator cleanup.
- The sweep is intentionally defined in Epic 7 rather than SPEC-002 recovery: the durable state reconstruction belongs to SPEC-002, but the meaning of a leftover `sys_clients` row and the correct cleanup action belong to the lifecycle subsystem.
- If the sweep itself encounters the same reducer failure behavior as a normal OnDisconnect path, it follows Story 7.3's existing contracts rather than inventing a restart-specific error model.
