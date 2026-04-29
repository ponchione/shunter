# V2-E Task 04: Extend Enforcement To Reads

Parent plan: `docs/hosted-runtime-planning/V2/V2-E/00-current-execution-plan.md`

Objective: apply permission enforcement to reads using the declared-read/raw-SQL
relationship.

Possible enforcement points:
- named query declarations
- named view declarations
- raw SQL one-off queries
- raw SQL subscriptions
- generated client helper metadata

Decision constraints:
- raw SQL reads may need table/read-model policy, not only named declaration
  policy
- generated clients must reflect the runtime behavior
- permission errors must preserve existing protocol error-shape expectations as
  much as possible
- read enforcement must not bypass subscription manager validation

If read semantics remain metadata-only:
- record that read permission enforcement is deferred
- keep reducer enforcement complete and tested

## Completion Decision

Read permission enforcement is deferred.

Reason:
- V2-D made `QueryDeclaration.SQL` and `ViewDeclaration.SQL` executable through
  generated helpers, but those helpers still call raw SQL one-off/subscription
  protocol paths.
- The runtime does not receive a named query/view declaration when a raw SQL
  read arrives, so declaration-only permission checks would be incomplete and
  bypassable.
- Correct raw SQL read enforcement needs a table/read-model policy layer that
  can evaluate the compiled SQL surface. That is outside V2-E's narrow reducer
  enforcement foundation.

Reducer enforcement is complete and tested.
