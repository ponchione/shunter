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
| Generated TypeScript live-read helpers | Generated helpers target declared live views by name, import the shared `@shunter/client` runtime types, and return the runtime `SubscriptionUnsubscribe` type. Raw SQL remains outside the generated helper path. | `TestGeneratorAcceptsCanonicalContractJSON`, `TestV1CompatibilityTypeScriptDeclaredReadResultShapeSurface`, `typescript/client/test/generated-runtime-usage.ts` |

## Query Shape Matrix

| Shape | Raw subscribe | Declared live view | Current coverage refs | Status / gaps |
| --- | --- | --- | --- | --- |
| Whole table | supported | supported | `TestHandleSubscribeSingleSuccess`, `TestHandleSubscribeMultiSuccess`, `TestRegisterReturnsInitialRows`, `TestProtocolDeclaredViewSucceedsWithDeclarationPermission` | done |
| Single-table equality filter | supported | supported | `TestHandleSubscribeSingleSuccess`, `TestEvalSingleTableColEqMatches`, `TestExecuteCompiledSQLQueryIndexedEqualityPredicateUsesIndexSeek` | done |
| Single-table range filter | supported | supported | `TestHandleSubscribeSingle_GreaterThanComparison`, `TestEvalSingleTableDeltaInserts`, `TestPlaceOrRangeBranchesUseRangeIndex` | done |
| Single-table `!=` / `<>` | supported through range-style pruning | supported through same predicate lowering | `TestHandleSubscribeSingle_NotEqualComparison`, `TestMatchRowColNe`, `TestCollectCandidatesMixedEqNeOrUsesIndexes` | done |
| `IS NULL` / `IS NOT NULL` | supported; raw admission lowers to typed null predicates | declared aggregate/query tests cover nullable semantics | `TestHandleSubscribeSingle_NullPredicates`, `TestParseNullPredicates`, `TestMatchRowColNeNull`, `TestCollectCandidatesColNeNullRangeMatchAndMismatch`, `TestDeclaredQueryNullableAggregateSemantics` | done |
| `AND`, `OR`, and parentheses | supported | supported | `TestHandleSubscribeSingle_OrComparison`, `TestParseWhereHarmlessParenthesizationMetamorphic`, `TestMatchRowAnd`, `TestMatchRowOr` | done |
| Mixed equality/range OR filters | supported when branches are indexable or conservatively placed | supported | `TestPlaceOrMixedEqRangeBranchesUseIndexes`, `TestCollectCandidatesMixedEqRangeOrPrunesMismatch`, `TestEvalOrWithMixedEqRangeBranchesUsesIndexes` | done |
| `:sender` filter | supported and caller-hashed | supported and permission-aware | `TestHandleSubscribeSingle_SenderParameterOnIdentityColumn`, `TestHandleSubscribeMulti_MixedLiteralAndSenderParameterCarriesPerPredicateHashIdentity`, `TestRegisterParameterizedHashUsesClientIdentity`, `TestProtocolDeclaredReadsApplyVisibilityToInitialRowsAndDeltas` | done |
| Visibility filters | supported before registration/evaluation | supported before initial rows and deltas | `TestAuthReadAdmissionSubscribePublicSucceeds`, `TestProtocolDeclaredReadsApplyVisibilityToInitialRowsAndDeltas`, `TestDeclaredViewMultiWayJoinAppliesVisibilityAfterPermissionSucceeds` | done |
| Two-table indexed join | supported when a join column is indexed | supported | `TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityAccepted`, `TestRegisterJoinBootstrapFallsBackToLeftIndex`, `TestEvalJoinSubscription`, `TestProtocolDeclaredViewMultiWayJoinSendsDeltas` | done |
| Two-table remote value filter | supported | supported | `TestCollectCandidatesJoinValueFilterEdgeUsesDeltaOppositeRows`, `TestPlaceJoinWithOppositeSideMixedOrFilterAddsValueAndRangeEdges` | done |
| Two-table remote range filter | supported | supported | `TestCollectCandidatesJoinRangeFilterEdgeUsesDeltaOppositeRows`, `TestCollectCandidatesJoinMixedOrRangeEdgePrunesMismatch` | done |
| Cross join | supported for qualified table-shaped projections and qualified column-equality `WHERE`; unsupported raw shapes reject before registration | supported, including aggregate views | raw: `TestHandleSubscribeSingle_CrossJoinAdmissionMatrix`; declared: `TestSubscribeViewCrossJoinAggregateInitialRows`, `TestProtocolDeclaredViewCrossJoinSumAggregateSendsInitialRowsAndDeltas` | done |
| Multi-way key-preserving join | supported when indexed and covered | supported | `TestDeclaredViewMultiWayJoinSubscribes`, `TestMultiJoinRegisterInitialRowsAndDeltas`, `TestProtocolDeclaredViewMultiWayJoinSendsDeltas` | done |
| Multi-way non-key-preserving path | supported when indexed and within traversal limits | supported under same constraints | `TestJoinPathTraversalIndexAddLookup`, `TestJoinRangePathTraversalIndexAddLookup`, `TestMultiJoinPlacementSplitOrMaxHopUsesGenericPathEdges`, `TestMultiJoinPlacementSplitOrBeyondMaxHopFallsBackToExistenceEdge`, `TestCollectCandidatesMultiJoinAndWrappedSplitOrAllRemoteRangeBranchesUseSameTransactionRows` | done |
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

No subscription support matrix gaps are currently tracked. Add rows here when
new live-read surfaces or unsupported-shape diagnostics are identified.
