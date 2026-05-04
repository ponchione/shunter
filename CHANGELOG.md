# Changelog

Shunter uses source versions from `VERSION` and release tags named `vX.Y.Z`.

## v0.1.0-dev

- Current development line.
- Gzip-negotiated protocol connections now gzip-compress post-handshake server messages while keeping client messages uncompressed.
- Added narrow `OFFSET <unsigned-integer>` support for one-off SQL and declared queries.
- Added narrow `ORDER BY` support for unique projection output names in one-off SQL and declared queries.
- Added multi-column `ORDER BY` support for one-off SQL and executable declared queries.
- Added narrow `COUNT(<column>)` support for one-off SQL and declared queries.
- Added narrow `SUM(<numeric-column>)` support for one-off SQL and declared queries.
- Reject commits before state mutation when the executor TxID allocator is exhausted.
- Reject reducer registration before reducer ID allocation can wrap.
- Fixed store index keys so caller-owned `bytes` and `arrayString` values cannot mutate committed index entries after insert.
