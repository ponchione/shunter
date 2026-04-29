# Hosted Runtime V2.5 Planning

Status: complete
Scope: read authorization completion for hosted-runtime V2.

Primary authority:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`

Supporting completed slices:
- `docs/features/V2/V2-D/` for declared read SQL convergence
- `docs/features/V2/V2-E/` for permission claims and reducer enforcement
- `docs/features/V2/V2-F/` for hosted runtime/protocol wiring

V2.5 turned the V2 read-authorization design into worker-sized implementation
tasks. The completed end state is that Shunter enforces external read
authorization with table policy, named declared read execution,
generated-client alignment, and row-level visibility.

## Phase Order

1. `V2.5-A`: table policy plus raw SQL enforcement
   - tasks 01-04
   - delivers immediate protection for raw one-off and subscription SQL
2. `V2.5-B`: declared read surfaces
   - tasks 05-06
   - makes `QueryDeclaration` and `ViewDeclaration` runtime-owned read
     endpoints and changes generated clients to use them
3. `V2.5-C`: row-level visibility
   - tasks 07-08
   - attaches caller-specific row filtering to tables and applies it before
     query evaluation can leak rows through joins
4. Cross-cutting closeout
   - tasks 09-10
   - ensures contracts, diffs, migration policy, gauntlet tests, and validation
     prove the whole feature

Do not skip phase A. Named declarations and row-level visibility both depend on
the same caller context and read-admission discipline.

## Task Files

1. `00-current-execution-plan.md`
2. `01-stack-prerequisites.md`
3. `02-schema-table-read-policy.md`
4. `03-shared-permission-checker.md`
5. `04-auth-aware-raw-sql-admission.md`
6. `05-declared-read-catalog-and-runtime-api.md`
7. `06-protocol-and-codegen-declared-reads.md`
8. `07-visibility-filter-declarations.md`
9. `08-read-plan-visibility-expansion.md`
10. `09-contract-diff-and-migration-policy.md`
11. `10-gauntlet-and-validation.md`

## Boundary Rules

V2.5 must:
- preserve existing reducer permission behavior
- preserve existing raw SQL error text where tests pin it
- enforce raw SQL table policy before subscription registration and before
  one-off execution
- keep subscription manager from becoming the first line of SQL
  authorization
- make generated declaration helpers call named read endpoints, not raw SQL
  text
- treat row-level visibility as part of query evaluation, not as a final
  projected-row filter

V2.5 must not:
- infer declaration permissions by matching raw SQL text
- authorize only the projected table
- silently grant strict-auth callers admin privileges
- accept unsupported SQL forms without enforcing table and row policy
- copy source or structure from `reference/SpacetimeDB`

## Completion Definition

V2.5 is complete when:
- default-private table policy is implemented and exported
- permissioned table reads are enforced for raw SQL
- raw SQL cannot leak unauthorized tables through joins
- declared reads are invokable by name and enforce declaration permissions
- generated declaration helpers use named read callbacks
- row-level visibility applies to one-off reads, subscription initial state,
  and subscription deltas
- contracts and diffs expose policy changes
- the V2.5 gauntlet and full validation gates pass

## Accepted Closure Caveats

These are not V2.5 blockers:

- The final runtime gauntlet intentionally does not duplicate every
  package/tooling edge case. Generated declared-read callbacks, malformed
  metadata validation, contract diff and migration classification, raw SQL
  parse/type/error ordering, reducer permission behavior, and protocol
  lifecycle behavior remain pinned by focused package tests.
- Generated TypeScript still exposes table-level raw SQL helper surfaces for
  tables whose read policy may be private or permissioned. That is metadata and
  client ergonomics, not execution authority; server-side raw SQL admission
  still enforces table policy and visibility.

Reopen V2.5 only for a confirmed read-authorization behavior regression.
Client API polish or broader gauntlet duplication should be tracked as a
separate follow-up.
