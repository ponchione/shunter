# Continued-Development Recommendations

Status: optional proposals plus completed historical records

These recommendations consolidate the codebase assessment, reference-runtime
comparison, hosted-backend direction, and operational-use discussion completed
in July 2026. They are optional, trigger-driven ideas, not a sequential roadmap,
and do not authorize speculative implementation.

A recommendation becomes active only when supported by at least one concrete
trigger:

- an explicit user goal
- a reproducible bug or limitation
- code or test evidence
- concrete integration pressure
- a specifically authorized release or distribution decision

Promote a triggered proposal into `../actionable/` or another owned plan only
when its concrete implementation goal is authorized. File numbers are stable
references, not priority. In particular, completing qualification does not make
the product operating envelope or any other item the automatic next task.
Release, production-operability, synthetic benchmark, and productization work
must not become the default merely because it appears first. Live code and tests
remain more authoritative than these notes.

## Completed And Historical

- [Qualify the current development line](01-current-release-qualification.md) -
  completed on 2026-07-10; qualification and release-owner review are preserved,
  no release was cut, and release preparation is dormant pending explicit
  authorization.

## Optional Proposals

- [Establish a product operating envelope](02-product-operating-envelope.md) -
  measured capacity and recovery expectations when real workload pressure
  requires them.
- [Operationalize durability maintenance](03-durability-maintenance-policy.md) -
  a repeatable snapshot, compaction, and backup policy.
- [Reduce recovery cost](04-recovery-efficiency-refactor.md) - lower replay
  memory and latency without format drift.
- [Define reliable integration patterns](05-enterprise-integration-reliability.md) -
  safe coordination with systems of record.
- [Harden operational authorization](06-operational-authorization-model.md) -
  enterprise identity-to-permission and scope behavior.
- [Make reconnect state explicit](07-client-connectivity-resilience.md) - honest
  operator UX through network interruption.
- [Set live-query admission policy](08-live-query-admission-policy.md) -
  evidence-backed protection from expensive views.
- [Add an operational audit pattern](09-operational-audit-trail.md) - app-facing
  action history without exposing commitlog internals.
- [Settle TypeScript distribution](10-typescript-client-distribution.md) - a
  governed public or intentionally private SDK workflow.
- [Deepen the type system](11-type-system-depth.md) - richer domain contracts
  before broad SQL work.

## Standing Non-Goals

None of these recommendations should introduce, by accident:

- reference-runtime wire, storage, client, or source compatibility
- a managed control plane or dynamic module upload
- distributed transactions or a multi-region database
- broad SQL or PostgreSQL compatibility
- raw telemetry, analytics-warehouse, blob-store, PLC, or SCADA behavior
- a second in-repo reference application that duplicates product canaries
