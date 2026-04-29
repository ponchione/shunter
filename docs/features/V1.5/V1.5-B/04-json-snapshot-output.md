# V1.5-B Task 04: Add Canonical JSON Snapshot Output

Parent plan: `docs/features/V1.5/V1.5-B/00-current-execution-plan.md`

Objective: provide deterministic JSON output suitable for generated artifacts,
review diffs, and optional committed snapshots.

Implementation target:
- add a JSON export helper for the full contract
- make JSON output deterministic enough for stable review diffs
- document `shunter.contract.json` as the recommended repo-committed path
- allow callers/tooling to choose another output path
- keep canonical JSON as the source of truth

Tests to add:
- repeated export of the same runtime produces byte-identical JSON
- JSON round trips back into the contract type
- default snapshot filename is documented or returned by a helper constant
- custom output paths are accepted by tooling/helper code if such code exists
- generated human-readable docs are not required

Implementation notes:
- if JSON is produced by a command, keep the command small and contract-focused
- if JSON is produced by a library helper only, leave CLI wiring to a later
  tooling slice unless it is needed for V1.5-C
- do not require every app to commit generated artifacts

