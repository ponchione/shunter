# V2-C Task 01: Reconfirm Migration Planning Prerequisites

Parent plan: `docs/hosted-runtime-planning/V2/V2-C/00-current-execution-plan.md`

Objective: verify V2-C can build on landed V1.5-E metadata and diff tooling.

Checks:
- `rtk go doc . MigrationMetadata`
- `rtk go doc . MigrationContract`
- `rtk go doc . Module.Migration`
- `rtk go doc . Module.TableMigration`
- `rtk go doc ./contractdiff Compare`
- `rtk go doc ./contractdiff CheckPolicy`
- `rtk go doc ./store CommittedState`
- `rtk go doc ./commitlog OpenAndRecoverDetailed`

Read only if needed:
- `runtime_migration_test.go`
- `runtime_contract.go`
- `contractdiff/`
- `store/`
- `commitlog/`

Prerequisite conclusions to record in Task 01:
- migration metadata is descriptive and exported
- `contractdiff` already classifies contract changes
- policy checks already warn about missing/risky metadata
- store and commitlog APIs must not be mutated by planning-only work
- runtime startup remains non-blocking for migration metadata

Recorded conclusions:
- `MigrationMetadata`, `MigrationContract`, `Module.Migration`, and
  `Module.TableMigration` are descriptive export surfaces only.
- `contractdiff.Compare` and `CheckPolicy` are sufficient inputs for
  contract-level planning and metadata-vs-diff mismatch warnings.
- `store.CommittedState` exposes live state locking/snapshot APIs, and
  `commitlog.OpenAndRecoverDetailed` performs recovery-oriented opening; V2-C
  therefore kept stored-state validation deferred rather than adding a
  planning path that could write commitlog/snapshot state.
- Runtime startup remains non-blocking for migration metadata; V2-C planning is
  an explicit artifact workflow over existing `ModuleContract` JSON files.

Stop if:
- V1.5-E contractdiff or migration metadata behavior is failing
- planning would require stored-state mutation to produce useful output
