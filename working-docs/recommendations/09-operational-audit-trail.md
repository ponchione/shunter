# Define An Operational Audit-Trail Pattern

Status: recommended for multi-user operational workflows

Promotion trigger: a real application must answer who changed an operational
case, what action they took, when it happened, and what business reason or
correlation accompanied it.

Owners: root reducer context, module patterns, contracts, codegen, operator
docs

## Why

The commit log is a durability implementation, not an app-facing audit API.
Operational systems need intentional, reviewable history tied to business
actions and caller identity. Each application should not invent incompatible
or incomplete audit rows.

## Outcome

A documented app-owned append-only history pattern that is written in the same
transaction as the state change and can be queried through declared reads.

## Proposed Record

The promoted app should determine the final schema, but common fields include:

- audit event identifier
- entity type and entity identifier
- action name and reducer name
- caller identity and connection identifier where appropriate
- event timestamp
- correlation or integration identifier
- previous and new lifecycle state when safe to retain
- bounded reason or decision code
- non-sensitive metadata needed for review

## Work

1. Prove the schema in a real operational workflow.
2. Provide a reducer-side helper only after repeated use demonstrates a stable
   shape.
3. Write the audit row in the same transaction as the corresponding state
   change.
4. Define retention, export, redaction, and visibility policy explicitly.
5. Prevent normal application reducers from mutating or deleting prior audit
   rows by module convention or a narrowly justified schema capability.
6. Keep detailed payloads bounded and avoid copying secrets, tokens, or raw
   third-party responses.

## Non-Goals

- exposing commit-log records as business history
- claiming tamper-evident or regulatory compliance without an external control
  design
- logging every read
- storing unrestricted before/after payloads
- replacing source-system audit trails

## Completion Evidence

- state transition and audit row commit or roll back together
- authorization tests protect sensitive history
- retention/export procedure is documented
- integration correlation survives retry and restart
- app can reconstruct the reviewed business-action timeline without reading
  runtime internals
