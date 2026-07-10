# Continued-Development Recommendations

Status: proposed slices awaiting explicit promotion

These recommendations consolidate the codebase assessment, reference-runtime
comparison, hosted-product direction, and operational-use discussion completed
in July 2026. They are not an active roadmap and do not authorize speculative
implementation.

Promote one file at a time into `../actionable/` or a task-specific plan when a
release decision, product workload, reproducible failure, or approved
integration supplies its promotion trigger. Live code and tests remain more
authoritative than these notes.

## Suggested Order

| Order | Recommendation | Primary outcome |
| ---: | --- | --- |
| 1 | [Qualify the current development line](01-current-release-qualification.md) | A release decision tied to current evidence |
| 2 | [Establish a product operating envelope](02-product-operating-envelope.md) | Measured capacity and recovery expectations |
| 3 | [Operationalize durability maintenance](03-durability-maintenance-policy.md) | A repeatable snapshot, compaction, and backup policy |
| 4 | [Reduce recovery cost](04-recovery-efficiency-refactor.md) | Lower replay memory and latency without format drift |
| 5 | [Define reliable integration patterns](05-enterprise-integration-reliability.md) | Safe coordination with systems of record |
| 6 | [Harden operational authorization](06-operational-authorization-model.md) | Enterprise identity-to-permission and scope behavior |
| 7 | [Make reconnect state explicit](07-client-connectivity-resilience.md) | Honest operator UX through network interruption |
| 8 | [Set live-query admission policy](08-live-query-admission-policy.md) | Evidence-backed protection from expensive views |
| 9 | [Add an operational audit pattern](09-operational-audit-trail.md) | App-facing action history without exposing commitlog internals |
| 10 | [Settle TypeScript distribution](10-typescript-client-distribution.md) | A governed public or intentionally private SDK workflow |
| 11 | [Deepen the type system](11-type-system-depth.md) | Richer domain contracts before broad SQL work |

Items 1 through 4 are qualification and operability work for capabilities that
already exist. Items 5 through 9 arise from using Shunter as a live operational
coordination backend. Items 10 and 11 are productization and developer-
experience investments that should follow real adoption pressure.

## Standing Non-Goals

None of these recommendations should introduce, by accident:

- reference-runtime wire, storage, client, or source compatibility
- a managed control plane or dynamic module upload
- distributed transactions or a multi-region database
- broad SQL or PostgreSQL compatibility
- raw telemetry, analytics-warehouse, blob-store, PLC, or SCADA behavior
- a second in-repo reference application that duplicates product canaries
