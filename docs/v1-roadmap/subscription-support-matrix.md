# Subscription Support Matrix

Status: active Milestone A tracking

This matrix is subscription-specific. It complements
`docs/v1-roadmap/09-sql-read-scope.md` and the v1 read-surface matrix in
`docs/v1-compatibility.md`; those documents define the SQL surface, while this
file tracks live-read admission, initial rows, deltas, pruning, and lifecycle
coverage.

Legend:

- `done`: supported behavior has named tests.
- `partial`: accepted or implemented, but at least one required coverage axis
  is still tracked as missing.
- `reject`: admission rejects the shape before executor registration.
- `post-v1`: intentionally outside the v1 live-read contract.

## Surface Matrix

| Surface | Contract | Coverage owner refs |
| --- | --- | --- |
| Raw `SubscribeSingle` | Table-shaped live SQL only. Single-table `SELECT *` and join `SELECT table.*`/alias-qualified emitted relations are accepted when predicates and indexes are valid. Column projections, aggregates, `ORDER BY`, `LIMIT`, and `OFFSET` reject. | `protocol/handle_subscribe_test.go`, `protocol/auth_read_admission_test.go`, `subscription/manager_test.go`, `subscription/eval_test.go`, `subscription/property_test.go` |
| Raw `SubscribeMulti` | Same SQL contract as `SubscribeSingle`, applied atomically to every query string in the batch. One invalid query rejects the batch before registration. | `TestHandleSubscribeMultiSuccess`, `TestHandleSubscribeMulti_UnknownTable`, `TestHandleSubscribeMulti_ShunterCompileErrorIncludesExecutingSqlSuffix`, `TestHandleSubscribeMulti_AggregateRejectedAtomically` |
| Declared live views | Named live-read surface. Allows table-shaped reads, joins, projections over the emitted relation, single-table ordered/limited initial snapshots, and the supported aggregate subset. Post-commit delivery remains row deltas or aggregate replacement rows as tested. | `declared_read_test.go`, `declared_read_protocol_test.go`, `subscription/projection.go`, `subscription/aggregate.go` |
| Local/runtime subscription APIs | `Runtime.SubscribeView` uses declared view metadata and declaration permissions, returning initial rows and columns for local callers. There is no local ad hoc raw subscription SQL API for v1. | `TestSubscribeViewJoinAggregateCountInitialRows`, `TestSubscribeViewOrderByReturnsOrderedInitialRows`, `TestSubscribeViewOverPrivateBaseTableUsesDeclarationPermission` |
| Generated TypeScript live-read helpers | Not landed. SDK helpers should target declared live views and this matrix once the runtime exists. | missing |

## Query Shape Matrix

| Shape | Raw subscribe | Declared live view | Current coverage refs | Status / gaps |
| --- | --- | --- | --- | --- |
| Whole table | supported | supported | `TestHandleSubscribeSingleSuccess`, `TestHandleSubscribeMultiSuccess`, `TestRegisterReturnsInitialRows`, `TestProtocolDeclaredViewSucceedsWithDeclarationPermission` | done |
| Single-table equality filter | supported | supported | `TestHandleSubscribeSingleSuccess`, `TestEvalSingleTableColEqMatches`, `TestExecuteCompiledSQLQueryIndexedEqualityPredicateUsesIndexSeek` | done |
| Single-table range filter | supported | supported | `TestHandleSubscribeSingle_GreaterThanComparison`, `TestEvalSingleTableDeltaInserts`, `TestPlaceOrRangeBranchesUseRangeIndex` | done |
| Single-table `!=` / `<>` | supported through range-style pruning | supported through same predicate lowering | `TestHandleSubscribeSingle_NotEqualComparison`, `TestMatchRowColNe`, `TestCollectCandidatesMixedEqNeOrUsesIndexes` | done |
| `IS NULL` / `IS NOT NULL` | parser support exists; subscription runtime represents null inequality through `ColNe` | declared aggregate/query tests cover nullable semantics | `TestParseNullPredicates`, `TestMatchRowColNeNull`, `TestCollectCandidatesColNeNullRangeMatchAndMismatch`, `TestDeclaredQueryNullableAggregateSemantics` | partial: add raw subscribe protocol pins for `IS NULL`/`IS NOT NULL` |
| `AND`, `OR`, and parentheses | supported | supported | `TestHandleSubscribeSingle_OrComparison`, `TestParseWhereHarmlessParenthesizationMetamorphic`, `TestMatchRowAnd`, `TestMatchRowOr` | done |
| Mixed equality/range OR filters | supported when branches are indexable or conservatively placed | supported | `TestPlaceOrMixedEqRangeBranchesUseIndexes`, `TestCollectCandidatesMixedEqRangeOrPrunesMismatch`, `TestEvalOrWithMixedEqRangeBranchesUsesIndexes` | done |
| `:sender` filter | supported and caller-hashed | supported and permission-aware | `TestHandleSubscribeSingle_SenderParameterOnIdentityColumn`, `TestHandleSubscribeMulti_MixedLiteralAndSenderParameterCarriesPerPredicateHashIdentity`, `TestRegisterParameterizedHashUsesClientIdentity`, `TestProtocolDeclaredReadsApplyVisibilityToInitialRowsAndDeltas` | done |
| Visibility filters | supported before registration/evaluation | supported before initial rows and deltas | `TestAuthReadAdmissionSubscribePublicSucceeds`, `TestProtocolDeclaredReadsApplyVisibilityToInitialRowsAndDeltas`, `TestDeclaredViewMultiWayJoinAppliesVisibilityAfterPermissionSucceeds` | done |
| Two-table indexed join | supported when a join column is indexed | supported | `TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityAccepted`, `TestRegisterJoinBootstrapFallsBackToLeftIndex`, `TestEvalJoinSubscription`, `TestProtocolDeclaredViewMultiWayJoinSendsDeltas` | done |
| Two-table remote value filter | supported | supported | `TestCollectCandidatesJoinValueFilterEdgeUsesDeltaOppositeRows`, `TestPlaceJoinWithOppositeSideMixedOrFilterAddsValueAndRangeEdges` | done |
| Two-table remote range filter | supported | supported | `TestCollectCandidatesJoinRangeFilterEdgeUsesDeltaOppositeRows`, `TestCollectCandidatesJoinMixedOrRangeEdgePrunesMismatch` | done |
| Cross join | supported for table-shaped qualified projections; unsupported predicate forms reject before registration | supported, including aggregate views | raw: `TestHandleSubscribeSingle_CrossJoinProjection`, `TestHandleSubscribeSingle_CrossJoinWhereFalseStillRejected`; declared: `TestSubscribeViewCrossJoinAggregateInitialRows`, `TestProtocolDeclaredViewCrossJoinSumAggregateSendsInitialRowsAndDeltas` | partial: raw cross-join acceptance rules need one consolidated admission section |
| Multi-way key-preserving join | supported when indexed and covered | supported | `TestDeclaredViewMultiWayJoinSubscribes`, `TestMultiJoinRegisterInitialRowsAndDeltas`, `TestProtocolDeclaredViewMultiWayJoinSendsDeltas` | done |
| Multi-way non-key-preserving path | supported when indexed and within traversal limits | supported under same constraints | `TestJoinPathTraversalIndexAddLookup`, `TestJoinRangePathTraversalIndexAddLookup`, `TestCollectCandidatesMultiJoinAndWrappedSplitOrAllRemoteRangeBranchesUseSameTransactionRows` | partial: add matrix-linked upper-bound tests for `joinPathTraversalMaxHops` |
| Repeated table aliases | supported when aliases are explicit and covered | supported | `TestEvalSelfEquiJoinSubscription`, `TestEvalSelfEquiJoinWithAliasedWhere`, `TestQueryHashSelfJoinFilterChildOrderCanonicalized`, `TestDeclaredViewMultiWayJoinAppliesVisibilityAfterPermissionSucceeds` | done |
| Column equality filters | supported | supported | `TestMatchRowSideColEqColSameSide`, `TestMatchJoinPairColEqCol`, `TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityAccepted` | done |
| Projections | reject raw | supported over emitted relation | raw: `TestHandleSubscribeSingle_ShunterBareColumnProjectionRejected`; declared: `TestProtocolDeclaredViewColumnProjectionSendsProjectedInitialRowsAndDeltas` | done |
| Aggregate `COUNT`/`SUM` | reject raw | supported subset | raw: `TestHandleSubscribeSingle_ShunterCountAliasRejected`, `TestHandleSubscribeMulti_AggregateRejectedAtomically`; declared: `TestSubscribeViewJoinAggregateCountInitialRows`, `TestProtocolDeclaredViewAggregateSendsInitialRowsAndDeltas`, `TestProtocolDeclaredViewJoinSumAggregateSendsInitialRowsAndDeltas` | done for named subset |
| `ORDER BY` initial snapshot | reject raw | supported for declared initial snapshots | raw: `TestHandleSubscribeSingle_OrderByRejected`; declared: `TestSubscribeViewOrderByReturnsOrderedInitialRows`, `TestProtocolDeclaredViewOrderBySendsOrderedInitialRowsAndRowDeltas` | done |
| `LIMIT` initial snapshot | reject raw | supported for declared initial snapshots | raw: `TestHandleSubscribeSingle_ShunterLimitClauseRejected`; declared: `TestSubscribeViewLimitReturnsLimitedInitialRows`, `TestProtocolDeclaredViewLimitSendsLimitedInitialRowsAndRowDeltas` | done |
| `OFFSET` initial snapshot | reject raw | supported for declared initial snapshots | raw: `TestHandleSubscribeSingle_OffsetRejected`; declared: `TestSubscribeViewOffsetReturnsOffsetInitialRows`, `TestProtocolDeclaredViewOffsetSendsOffsetInitialRowsAndRowDeltas` | done |
| Grouped aggregate | post-v1 | post-v1 | `TestV1UnsupportedSQLNonGoalsRejected`, `TestHandleSubscribeSingle_ShunterSqlUnsupportedAggregateWithGroupByRejected`, `TestHandleSubscribeSingle_ShunterSqlInvalidEmptyGroupByRejected` | done rejection |
| Outer join | post-v1 | post-v1 | `TestV1UnsupportedSQLNonGoalsRejected`, `TestParseRejectsNonInnerJoinKeyword` | done rejection |
| Subquery | post-v1 | post-v1 | `TestV1UnsupportedSQLNonGoalsRejected`, `TestHandleSubscribeSingle_ShunterSubqueryInFromRejected` | done rejection |

## Missing Test Backlog

- Add raw `SubscribeSingle` protocol pins for `IS NULL` and `IS NOT NULL`
  once the intended predicate lowering is finalized for subscription SQL.
- Consolidate raw cross-join admission tests into one named table that separates
  accepted table-shaped column-equality joins from rejected unconstrained cross
  joins.
- Add generic traversal upper-bound tests at `joinPathTraversalMaxHops` and
  beyond-limit rejection/fallback tests.
- Fill generated TypeScript live-read helper rows after the SDK runtime lands.
