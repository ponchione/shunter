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

Stop if:
- V1.5-E contractdiff or migration metadata behavior is failing
- planning would require stored-state mutation to produce useful output
