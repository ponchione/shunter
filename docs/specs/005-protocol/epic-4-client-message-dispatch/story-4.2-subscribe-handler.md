# Story 4.2: Subscribe Handler

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §7.1, §7.1.1, §9.1
**Depends on:** Story 4.1
**Blocks:** Epic 5 (subscription state for routing)

**Cross-spec:** SPEC-003 (executor inbox: `RegisterSubscriptionSetCmd`), SPEC-004 (predicate model: `AllRows`, `ColEq`, `And`)

> **Updated 2026-04-24 (QueryID cleanup).** Subscribe handling is
> `SubscribeSingleMsg` / `SubscribeMultiMsg` keyed by client `QueryID` /
> wire `query_id`. There is no protocol-local tracker; duplicate
> pending/active query IDs are rejected by the manager as
> `ErrQueryIDAlreadyLive`.

---

## Summary

Parse and validate `SubscribeSingle` / `SubscribeMulti` messages. Parse SQL query strings into SPEC-004 predicates and route manager-authoritative QueryID registration to the executor.

## Deliverables

- `func handleSubscribeSingle(conn *Conn, msg *SubscribeSingleMsg, executor ExecutorInbox, schema SchemaLookup)` and the matching multi-query path:
  1. Parse each SQL `query_string` with `query/sql.Parse`
  2. Validate referenced table/columns and supported subscription shape → send `SubscriptionError` if invalid
  3. Lower each accepted SQL query to SPEC-004 predicates (`AllRows`, `ColEq` / ranges / supported `And` and join forms)
  4. Send `RegisterSubscriptionSetCmd` to the executor with `ConnID`, client `QueryID`, `RequestID`, and the predicate list
  5. Let the subscription manager enforce duplicate pending/active `(ConnID, QueryID)` state and surface `ErrQueryIDAlreadyLive` as `SubscriptionError`

- `SchemaLookup` interface — resolves table names to IDs and column names to IDs:
  ```go
  type SchemaLookup interface {
      TableByName(name string) (TableID, *TableSchema, bool)
  }
  ```

- Predicate normalization helpers:
  ```go
  func NormalizePredicate(tableID TableID, schema *TableSchema, preds []Predicate) (spec004.Predicate, error)
  ```

## Acceptance Criteria

- [ ] Valid SubscribeSingle / SubscribeMulti → `RegisterSubscriptionSetCmd` sent with the client `QueryID`
- [ ] Duplicate active `query_id` → `SubscriptionError` with `ErrQueryIDAlreadyLive`
- [ ] Duplicate pending `query_id` → `SubscriptionError` with `ErrQueryIDAlreadyLive`
- [ ] Unknown table → `SubscriptionError`
- [ ] Unknown column → `SubscriptionError`
- [ ] Empty predicates → normalized to `AllRows`
- [ ] Single predicate → normalized to `ColEq`
- [ ] Three predicates `[P1, P2, P3]` → `And{And{ColEq(P1), ColEq(P2)}, ColEq(P3)}`
- [ ] Range predicate → `SubscriptionError` (not v1)
- [ ] `RegisterSubscriptionSetCmd` sent to executor on success
- [ ] Executor/manager responds with error → `SubscriptionError` sent; the failed `query_id` is reusable once not registered

## Design Notes

- The v1 wire protocol uses string table/column names. The protocol layer resolves these to internal IDs via `SchemaLookup` before constructing SPEC-004 predicates. This decouples wire format from internal representation.
- Predicate normalization is owned by the shared SQL compile path; accepted shapes lower into the SPEC-004 predicate tree used by subscription registration.
- `SchemaLookup` is read-only. The protocol layer never mutates schema.
