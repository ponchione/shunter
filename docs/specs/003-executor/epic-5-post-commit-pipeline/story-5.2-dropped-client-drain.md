# Story 5.2: Dropped Client Drain

**Epic:** [Epic 5 — Post-Commit Pipeline](EPIC.md)  
**Spec ref:** SPEC-003 §5  
**Depends on:** Story 5.1  
**Blocks:** Nothing

---

## Summary

After each commit's post-commit pipeline completes, non-blocking drain of the DroppedClients channel. Disconnect each dropped client's subscriptions.

## Deliverables

- Drain loop at the end of `postCommit`:
  ```go
  for {
      select {
      case connID := <-e.subs.DroppedClients():
          e.subs.DisconnectClient(connID)
      default:
          return
      }
  }
  ```

- Non-blocking: if no dropped clients are pending, loop exits immediately via `default`.

## Acceptance Criteria

- [ ] Dropped clients drained after response delivery
- [ ] Each dropped client triggers DisconnectClient call
- [ ] If no dropped clients, drain is a no-op (no blocking)
- [ ] Multiple dropped clients in channel all processed in one drain
- [ ] Drain happens before next command is dequeued

## Design Notes

- The fan-out worker (SPEC-004/SPEC-005) signals dropped connections by writing to the DroppedClients channel. The executor drains this channel after each commit to clean up subscriptions promptly.
- This is not in the critical path before response delivery — it runs after the response is sent. A slow DisconnectClient call doesn't delay the caller's response.
- If DisconnectClient itself fails, log the error and continue draining. Don't let one failed cleanup block others.
