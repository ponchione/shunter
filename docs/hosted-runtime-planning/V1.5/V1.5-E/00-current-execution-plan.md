# Hosted Runtime V1.5-E Current Execution Plan

Goal: make schema/module evolution visible and reviewable without executing
migrations.

Task sequence:
1. Reconfirm contract, metadata, and snapshot surfaces.
2. Add failing tests for descriptive migration metadata.
3. Implement module-level and declaration-level migration metadata.
4. Add contract-diff tooling that compares current export to a previous
   `shunter.contract.json`.
5. Add warning/CI-oriented policy checks for missing metadata, risky changes,
   and declared-vs-inferred mismatches.
6. Format and validate V1.5-E gates.

Task progress:
- Task 01 pending.
- Task 02 pending.
- Task 03 pending.
- Task 04 pending.
- Task 05 pending.
- Task 06 pending.

V1.5-E target:
- descriptive/exported metadata first
- no executable migration runner
- module-level version/compatibility summary
- optional declaration-level change metadata
- author-declared intent plus tool-inferred contract diffs
- runtime startup remains non-blocking for migration metadata

V1.5-E completes the initial V1.5 plan.

