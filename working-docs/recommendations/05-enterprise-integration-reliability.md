# Define Reliable Enterprise Integration Patterns

Status: recommended operational-app foundation

Promotion trigger: an approved product integration must coordinate Shunter
state with an ERP, dispatch, telematics, maintenance, messaging, or other
system of record.

Owners: root procedure/reducer surfaces, app shell, scheduler, contracts,
operator docs

## Why

Operational coordination apps live between systems. Procedures permit external
I/O and reducer calls, but a durable integration needs explicit idempotency,
retry, correlation, and failure ownership. These semantics should not be left
to ad hoc procedure code in every application.

## Outcome

A documented and tested application pattern for inbound events and outbound
commands with at-least-once delivery, idempotent effects, bounded retries, and
operator-visible dead letters.

## Proposed Pattern

- app-owned inbound endpoint verifies and normalizes a source event
- an inbox reducer deduplicates by source plus idempotency key
- the same transaction records the operational state change
- outbound intent is written to an app-owned outbox table in the same reducer
- a worker/procedure performs external I/O outside the reducer executor
- completion or failure is recorded through a reducer
- retry scheduling uses bounded exponential policy and explicit terminal state
- correlation identifiers connect source event, reducer transaction, outbound
  attempt, and operational case

## Work

1. Prove the pattern in a real app integration before adding runtime helpers.
2. Specify ownership and uniqueness rules for inbox and outbox identifiers.
3. Define retryable, permanent, and operator-actionable failure classes.
4. Provide a reusable helper only when two real integrations repeat the same
   code and semantics.
5. Add diagnostics for backlog depth, oldest pending work, attempt count, and
   terminal failures.
6. Document credential handling, request timeouts, payload bounds, and
   redaction.

## Non-Goals

- exactly-once network delivery
- a generic message broker or ESB inside Shunter
- storing raw telemetry firehoses
- performing external I/O while a reducer transaction is open
- replacing source-system audit or transaction records

## Completion Evidence

- integration tests covering duplicate inbound events, timeout, retry,
  external success with lost acknowledgement, permanent failure, and restart
- no duplicate committed operational effect for one idempotency key
- bounded queues, payloads, attempts, and timeouts
- operator-visible pending and failed work
- security review for the concrete integration
