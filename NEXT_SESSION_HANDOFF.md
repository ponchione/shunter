# Next session handoff

Use this file to kick off the next agent on the next narrow Shunter parity slice.

## Copy-paste prompt

Continue Shunter from the latest completed parity work.

This session is a narrow implementation slice, not a broad audit or cleanup pass.

Primary objective
- Phase 2 Slice 1c and `P0-RECOVERY-002` are already landed and verified.
- `TD-142` remains materially incomplete relative to reference SpacetimeDB subscription SQL, with non-equality now closed.
- The next session should take the next narrow grounded slice: `OR` predicates.
- Keep `OneOffQuery.message_id`, the recovery sequence fix, the same-table-qualified-column slice, the mixed-case identifier slice, the alias/qualified-star slice, the ordered-comparison slice, and the non-equality slice intact unless a regression appears.
- Use `reference/SpacetimeDB/` as the authoritative parity source, but prefer finishing `OR` cleanly before widening into join-backed forms.

## What just landed

- `OneOffQueryMsg` now uses `MessageID []byte` instead of `RequestID uint32`.
- `OneOffQueryResult` now uses `MessageID []byte`.
- Client/server codecs now encode/decode one-off query ids as length-prefixed byte strings.
- `handleOneOffQuery` preserves the opaque message id through success and error replies.
- New parity pin added: `TestPhase2Slice1COneOffQueryMessageIDBytes`.
- Snapshot + replay recovery now preserves the recovered `TxID` horizon, restored `nextID`, and recovered auto-increment sequence state without letting explicit non-zero autoincrement rows incorrectly jump the recovered sequence.
- New recovery pin added: `TestOpenAndRecoverDetailedSnapshotReplayIgnoresExplicitAutoincrementRowsWhenRestoringSequence`.
- `query/sql.Parse` now accepts same-table qualified WHERE columns such as `SELECT * FROM users WHERE users.id = 1` and normalizes them back to the unqualified column name.
- Unquoted SQL table/column identifiers now resolve case-insensitively through the schema seam, so mixed-case forms like `SELECT * FROM USERS WHERE ID = 1 AND users.DISPLAY_NAME = 'alice'` compile against lowercase registered schema names.
- Single-table alias / qualified-star forms now parse and normalize correctly, including `SELECT item.* FROM users item` and `SELECT item.* FROM users AS item WHERE item.name = 'alice'`, and the same alias-aware query path now works through both subscribe and one-off query handling.
- Ordered single-column comparison operators `<`, `<=`, `>`, and `>=` now parse and normalize through both subscribe and one-off query handling, lowering into `subscription.ColRange` instead of being rejected as unsupported SQL.
- Non-equality comparison operators `<>` and `!=` now parse and normalize through both subscribe and one-off query handling, lowering into `subscription.ColNe` instead of being rejected as unsupported SQL.
- New SQL/query pins added:
  - `query/sql/parser_test.go::TestParseWhereQualifiedColumnsSameTable`
  - `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_QualifiedColumnsSameTable`
  - `schema/types_test.go::TestTableSchemaColumnLookupCaseInsensitive`
  - `schema/build_test.go::TestRegistryTableByNameCaseInsensitive`
  - `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_MixedCaseTableAndColumns`
  - `query/sql/parser_test.go::TestParseSelectQualifiedStarWithAlias`
  - `query/sql/parser_test.go::TestParseSelectQualifiedStarWithAsAliasAndQualifiedWhereColumns`
  - `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_QualifiedStarAlias`
  - `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_QualifiedStarAlias`
  - `query/sql/parser_test.go::TestParseWhereComparisonOperators`
  - `query/sql/parser_test.go::TestParseWhereNotEqualOperators`
  - `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_GreaterThanComparison`
  - `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_NotEqualComparison`
  - `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_LessThanOrEqualComparison`
  - `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_NotEqualComparison`
- Remaining mismatched qualified columns still reject (for example `SELECT * FROM users WHERE posts.id = 1`).
- Related tests and docs/debt entries were updated.
- Verification already passed:
  - `rtk go test ./query/sql ./protocol ./subscription -run 'TestParseWhereNotEqualOperators|TestHandleSubscribeSingle_NotEqualComparison|TestHandleOneOffQuery_NotEqualComparison|TestColNeTablesSingle|TestMatchRowColNe|TestQueryHashColNeDiffersFromColEq' -v`
  - `rtk go test ./query/sql ./protocol ./subscription -v`
  - `rtk go test ./schema ./protocol -run 'TestTableSchemaColumnLookupCaseInsensitive|TestRegistryTableByNameCaseInsensitive|TestHandleSubscribeSingle_MixedCaseTableAndColumns' -v`
  - `rtk go test ./schema ./protocol -v`
  - `rtk go test ./...`

## Current git state to expect

Modified:
- `TECH-DEBT.md`
- `NEXT_SESSION_HANDOFF.md`
- `commitlog/recovery.go`
- `commitlog/recovery_test.go`
- `docs/current-status.md`
- `docs/parity-phase0-ledger.md`
- `docs/spacetimedb-parity-roadmap.md`
- `protocol/handle_oneoff_test.go`
- `protocol/client_messages.go`
- `protocol/client_messages_test.go`
- `protocol/handle_oneoff.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- `protocol/parity_message_family_test.go`
- `protocol/send_responses_test.go`
- `protocol/server_messages.go`
- `protocol/server_messages_test.go`
- `query/sql/parser.go`
- `query/sql/parser_test.go`
- `schema/build_test.go`
- `schema/registry.go`
- `schema/types.go`
- `schema/types_test.go`
- `store/recovery.go`

Untracked:
- `.hermes/`
- `docs/phase-2-slice-1c-handoff.md`

## Important note

- There is a repo-local file at `.hermes/plans/2026-04-18_073534-phase1-wire-level-parity.md` because `protocol/phase1_audit_docs_test.go` expects it. Keep it unless you deliberately update that test and its contract.

## Required reading order

1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `docs/parity-phase0-ledger.md`
9. `TECH-DEBT.md`
10. `query/sql/parser.go`
11. `query/sql/parser_test.go`
12. any protocol/query tests that exercise SQL strings before changing code

## Grounded context

- `docs/parity-phase0-ledger.md` now says:
  - Phase 1 closed
  - Phase 1.5 closed except permanent energy deferral
  - Phase 2 Slice 2 closed
  - Phase 2 Slice 1c closed
  - `P0-RECOVERY-002` closed
  - `TD-142` has five landed narrow widenings: same-table qualified WHERE columns, case-insensitive unquoted SQL identifier resolution through the schema seam, single-table alias / qualified-star support, ordered single-column comparisons `<`, `<=`, `>`, `>=`, and non-equality comparisons `<>` / `!=`
- `TECH-DEBT.md` still treats `TD-142` as open because reference SpacetimeDB supports materially more subscription SQL than Shunter currently does.
- `reference/SpacetimeDB/` is the authoritative source for what still counts as incomplete parity work.
- Grounded remaining reference-backed gaps include:
  - `OR` predicates
  - subscription joins, including join `ON` clauses and join-qualified column behavior
- The current Shunter SQL grammar in `query/sql/parser.go` is still much smaller than the reference:
  - `SELECT ( * | qualifier.* ) FROM ident [ [AS] alias ]`
  - optional `WHERE colref op lit (AND colref op lit)*`
  - `op` may be `=`, `!=`, `<>`, `<`, `<=`, `>`, or `>=`
  - `qualifier` / `colref` qualifiers may match the `FROM` table or its alias only
  - unquoted table/column identifier resolution against the registered schema is case-insensitive
  - optional trailing semicolon
- `query/sql/parser_test.go` still pins rejected shapes that are accepted by the reference subscription surface, including `OR` and `JOIN`.

## Recommended execution strategy for the next TD-142 slice (`OR`)

1. Re-check `reference/SpacetimeDB/docs/docs/00300-resources/00200-reference/00400-sql-reference.md` and `reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs` to keep the `OR` target grounded.
2. Add focused failing tests first for parser acceptance of `OR` predicates.
3. Add paired subscribe and one-off protocol/runtime tests so accepted `OR` syntax is not parser-only.
4. Choose the smallest coherent contract that matches the reference-backed surface for this slice:
  - `WHERE a = 1 OR a = 2`
  - mixed boolean trees only if they are needed for the same coherent runtime path
5. Prefer a real predicate representation and normalization path over string-level hacks.
6. Keep already-landed behavior stable:
  - same-table qualified WHERE columns stay accepted
  - mixed-case unquoted identifier resolution stays accepted
  - single-table alias / qualified-star forms stay accepted
  - ordered single-column comparisons stay accepted
  - non-equality comparisons `<>` / `!=` stay accepted
7. Leave join-backed forms for the slice after `OR` unless `OR` implementation proves it must share infrastructure.
8. Run focused tests continuously, then re-run broader package coverage, then `rtk go test ./...`.
9. Update `TECH-DEBT.md`, `docs/parity-phase0-ledger.md`, and this handoff to reflect exactly what `OR` shapes landed and what join-backed gaps remain.

## Likely file surface

- `reference/SpacetimeDB/docs/docs/00300-resources/00200-reference/00400-sql-reference.md`
- `reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs`
- `reference/SpacetimeDB/crates/expr/src/check.rs`
- `query/sql/parser.go`
- `query/sql/parser_test.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_subscribe_test.go`
- `protocol/handle_oneoff.go`
- `protocol/handle_oneoff_test.go`
- `schema/registry.go`
- `schema/types.go`
- `docs/parity-phase0-ledger.md` / `TECH-DEBT.md` / `NEXT_SESSION_HANDOFF.md`

## Locked implementation decisions

- Do not reopen `OneOffQuery` unless you find a real failing regression.
- Do not reopen the `P0-RECOVERY-002` sequence fix unless a regression appears.
- Do not reopen the same-table-qualified-column widening or the mixed-case identifier widening except to keep them correct while broadening the remaining reference-backed surface.
- `reference/SpacetimeDB/` wins over prior defer-heavy local framing when deciding whether a SQL shape is still incomplete parity work.
- Favor complete, coherent support for the remaining reference-backed subscription SQL surface over adding new deferments.
- Do not land parser-only widenings that leave subscribe/one-off runtime behavior inconsistent with the accepted syntax.
- For the immediate next slice, prefer `OR` before join-backed SQL unless fresh evidence shows a dependency in the live implementation seams.
- If a shape proves larger than expected, record the exact blocker with evidence rather than silently reclassifying it as deferred.

## Suggested verification commands

- `rtk go test ./query/sql -run '<targeted parser test names>' -v`
- `rtk go test ./query/sql ./protocol -run '<targeted parser test names>|<paired protocol test names>' -v`
- `rtk go test ./schema ./query/sql ./protocol -v`
- `rtk go test ./...`

## Acceptance gate

- The remaining reference-backed `TD-142` SQL/query gaps have been investigated directly against `reference/SpacetimeDB/`.
- Every newly accepted shape is pinned by focused failing-then-passing tests.
- Parser coverage and externally visible protocol/runtime coverage both exist for the landed behavior.
- Previously landed accepted shapes still pass and previously still-rejected shapes only remain rejected when the reference does not require them.
- Full suite still passes.
- Docs/debt/parity ledger reflect the new truth without hand-waving deferral language.

## Stop / escalate if

- A remaining reference-backed SQL shape turns out to require a much larger subscription/query/executor redesign than the evidence suggested.
- Reference docs/parser/tests in `reference/SpacetimeDB/` disagree materially with each other and the intended parity target is unclear.
- A proposed change would make Shunter accept syntax that the reference subscription surface still rejects.
- The work starts drifting into unrelated lag-policy or non-SQL protocol cleanup before the known TD-142 parity gaps are resolved.

## Deliverables

- code + tests closing the remaining reference-backed `TD-142` SQL/query-surface gaps, or a grounded blocker report for any gap that proves larger than expected
- parser tests plus subscribe/one-off protocol tests for each newly accepted behavior
- minimal but accurate doc/debt/parity updates
- concise final note stating exactly which reference-backed SQL/query shapes were closed, what still remains if anything, and why
