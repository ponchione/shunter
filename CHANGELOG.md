# Changelog

Shunter uses source versions from `VERSION` and release tags named `vX.Y.Z`.

## v0.1.0-dev

- Current development line.
- Gzip-negotiated protocol connections now gzip-compress post-handshake server messages while keeping client messages uncompressed.
- Added backup/restore guidance to dry-run contract migration plans for blocking or data-rewrite changes.
- Added reusable offline `DataDir` backup and restore helpers for app-owned binaries.
- Added generic CLI commands for offline runtime `DataDir` backup and restore.
- Added runtime snapshot creation and commit log compaction helpers for app-owned maintenance workflows.
- Added reusable runtime and host health/readiness inspection helpers.
- Added `Host.ListenAndServe` for app-owned serving of multi-module hosts.
- Added app-owned contract export and runtime-to-codegen file helpers in `contractworkflow`.
- Added narrow `OFFSET <unsigned-integer>` support for one-off SQL and declared queries.
- Added narrow `ORDER BY` support for unique projection output names in one-off SQL and declared queries.
- Added multi-column `ORDER BY` support for one-off SQL and executable declared queries.
- Added aggregate output alias `ORDER BY` support for one-off SQL and executable declared queries.
- Added narrow `COUNT(<column>)` support for one-off SQL and declared queries.
- Added narrow `SUM(<numeric-column>)` support for one-off SQL and declared queries.
- Reject commits before state mutation when the executor TxID allocator is exhausted.
- Reject reducer registration before reducer ID allocation can wrap.
- Fixed store index keys so caller-owned `bytes` and `arrayString` values cannot mutate committed index entries after insert.
