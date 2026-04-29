# Hosted Runtime V1 Feature Docs

This directory is historical implementation context. V1-A through V1-G have
landed in the root package, and current hosted-runtime status is tracked in
`HOSTED_RUNTIME_PLANNING_HANDOFF.md`.

Use these files to audit the original V1 contracts only. Do not treat older
implplan statements such as "root package absent" or "no Go files" as current
repo facts. When a task file and live code disagree, verify with `rtk go doc`
and the root package tests before opening a new implementation target.

Known historical artifacts:
- `V1-G/02-module-and-runtime-export-tests.md`
- `V1-G/03-runtime-export-implementation.md`
- `V1-G/04-detachment-and-status-snapshots.md`
- `V1-G/05-format-and-validate.md`

Those files are superseded by the current V1-G task files and retained only so
older implementation notes remain traceable.
