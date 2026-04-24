# Next Session Handoff

Use this file to start the next parity / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go doc ./subscription.Manager
rtk go list -json ./subscription ./protocol ./query/sql ./executor
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Scout for the next bounded OI-002 subscription/runtime residual. There is no queued target.

Recent closures removed the previously queued items:

- `P0-SUBSCRIPTION-033` (one-off `COUNT(*) [AS] alias` + `LIMIT` aggregate composition) closed in `700af6c`.
- Same-connection reused subscription-hash initial-snapshot elision closed in `febc389`; pinned by `subscription/register_set_test.go::TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate` and `TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot`.
- `SubscriptionError.table_id` always-`None` on subscribe/unsubscribe request-origin error paths closed in `d75d970`; pinned by `executor/protocol_inbox_adapter_test.go::TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleTableErrorEmitsNilTableID` alongside the pre-existing `TestProtocolInboxAdapter_RegisterSubscriptionSet_DuplicateErrorReply` multi-table nil assertion. Reference emit sites: `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_actor.rs:625`, `:731`, `:805`, `:1308`; `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:2014`.
- SubscribeSingle / SubscribeMulti compile-origin `SubscriptionError.Error` now carries the reference `DBError::WithSql` suffix `", executing: `{sql}`"` closed in `729860c`; pinned by `protocol/handle_subscribe_test.go::TestHandleSubscribe{Single,Multi}_ParityCompileErrorIncludesExecutingSqlSuffix`. Reference anchor: `reference/SpacetimeDB/crates/core/src/error.rs:140` (`DBError::WithSql { error, sql }` = `"{error}, executing: `{sql}`"`), emit sites `module_subscription_actor.rs:643` (SubscribeSingle `compile_query_with_hashes`) and `:1068` (SubscribeMulti per-SQL `compile_query_with_hashes`) via the `return_on_err_with_sql_bool!` macro. Helper: `protocol/handle_subscribe.go::wrapSubscribeCompileErrorSQL`.
- SubscribeSingle initial-eval `SubscriptionError.Error` now carries the same `DBError::WithSql` suffix closed in `d202669`; pinned by `executor/protocol_inbox_adapter_test.go::TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleInitialEvalErrorWrapsWithSql` (positive) and `_DuplicateErrorIsNotWrappedWithSql` (negative). Reference anchor: `module_subscription_actor.rs:672` (SubscribeSingle `evaluate_initial_subscription`) via `return_on_err_with_sql_bool!`. Seam: new `subscription.ErrInitialQuery` sentinel wraps the error returned by `Manager.initialQuery` inside `RegisterSet`; `protocol.RegisterSubscriptionSetRequest.SQLText` carries the original SQL into `executor/protocol_inbox_adapter.go::buildRegisterResponse`, which applies the suffix only when `errors.Is(replyErr, ErrInitialQuery) && SQLText != ""`.
- SubscribeMulti initial-eval `SubscriptionError.Error` now emits the canned `"Internal error evaluating queries"` text closed in `9d84609`; pinned by `_MultiInitialEvalErrorEmitsCannedMessage` (positive) and `_MultiDuplicateErrorNotCanned` (negative). Reference anchor: `module_subscription_actor.rs:1383` substitutes `evaluate_queries` failure text with that literal string on the v1 Multi path (no per-query detail, no WithSql suffix). Same `buildRegisterResponse` seam switches on `(ErrInitialQuery, Variant)` — Multi gets canned, Single gets WithSql.
- UnsubscribeSingle / UnsubscribeMulti final-eval `SubscriptionError.Error` parity closed in `db64604`. Reference anchors: `module_subscription_actor.rs:756` (UnsubscribeSingle via `return_on_err_with_sql!`) applies the `DBError::WithSql` suffix, and `:836` (UnsubscribeMulti via plain `return_on_err!`) emits raw err text with no suffix and no canned substitution — the canned-message substitution is admission-only (reference `:1383`) and does not apply to the unsubscribe path. Seam: new `subscription.ErrFinalQuery` sentinel wraps `Manager.initialQuery` errors inside `UnregisterSet`; the loop now evaluates final deltas first (short-circuit on first error) then drops every sid regardless, mirroring reference `remove_subscription`-before-eval ordering. `queryState.sqlText` is persisted at RegisterSet time from `SubscriptionSetRegisterRequest.SQLText` (Single only; Multi leaves it empty). `SubscriptionSetUnregisterResult.SQLText` carries the first-errored queryState's SQL back to the adapter, which applies the suffix only when `errors.Is(replyErr, ErrFinalQuery) && Variant == Single && SQLText != ""`. Pins: `subscription/register_set_test.go::TestUnregisterSetFinalEvalErrorWrapsErrFinalQueryAndDropsAll` (Single, SQLText surfaced, all sids dropped) and `TestUnregisterSetMultiFinalEvalErrorEmptySQLText` (Multi register leaves SQLText empty); `executor/protocol_inbox_adapter_test.go::TestProtocolInboxAdapter_UnregisterSubscriptionSet_{SingleFinalEvalErrorWrapsWithSql,MultiFinalEvalErrorRawText,NotFoundErrorIsNotWrappedWithSql}`.

Do not reopen `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033`, the reused-hash elision, the `SubscriptionError.table_id: None` closure, the compile-origin `WithSql` suffix, the Subscribe{Single,Multi} initial-eval error closures, or the Unsubscribe{Single,Multi} final-eval error closures without fresh failing regression evidence.

## Scout Prompts

Before starting implementation, answer one of these with live-code evidence:

- Is there a bounded SQL surface shape latently admitted by parser+compile but not pinned end-to-end — check parser operator combinations that compile cleanly but have no one-off or subscribe-rejection pin? The current subscribe/one-off parity surface in `protocol/handle_subscribe_test.go` and `protocol/handle_oneoff_test.go` is heavily populated (40+ pins each); scout for specific operator combinations not yet covered rather than whole-shape audits.
- Is there a post-commit fanout ordering, QueryID stamping, or durability-gate edge case not yet pinned for a specific combined scenario (multi-subscription-per-connection with partial table overlap on actual overlapping tables, self-join aliased delta under concurrent update)? Disjoint-table multi-sub and same-connection reused-hash post-elision stamping are already pinned by `TestEvalMultipleTableUpdatesGrouped` and `TestEvalFanoutCarriesClientQueryIDForEachSubscription` respectively.

Pick the smallest bounded slice with a reference anchor and a concrete failing test you can write first.

## Scouted Dead-Ends

Prior scouts that retired without a bounded slice. Do not rescout without fresh evidence.

- **Admission-error-text parity on subscribe/unsubscribe seam (ErrSubscriptionNotFound, ErrQueryIDAlreadyLive, Client not found, query-state-not-found).** Every reference emit site formats Rust tuples via Debug `{:?}`: `module_subscription_manager.rs:850`, `:857`, `:863`, `:875`, `:908`, `:915`, `:921`, `:928`, `:989`, `:1055`. The only fixed-literal admission emit is `module_subscription_actor.rs:750` `"Internal error"` on the UnsubscribeSingle duplicate-subscription path, but the reference comment at `:747` explicitly states "This whole function should be removed soon". Not a bounded parity surface.
- **`TotalHostExecutionDurationMicros` measurement-accuracy slice.** Reference captures `timer: Instant` at websocket-frame-receive in `client/message_handlers_v1.rs:20` and threads it into `module_host.rs:2302` (`timer.elapsed()`). Shunter captures at `Receipt` and measures via `elapsedMicros(receipt)` in the adapter. Both anchors are "elapsed-since-request-receipt"; any residual delta is implementation-detail noise, not observable. No failing test can pin a reference-precision delta under current code.
- **`"Estimated cardinality ({estimate} rows) exceeds limit ({limit} rows)"` row-limit literal (`estimation.rs:27-29`).** Reference rejects pre-execution via a cardinality-planner estimate; Shunter enforces via runtime actual-count in `subscription/register_set.go:61`,`:140` (`ErrInitialRowLimit`). Text-shape parity would emit a numerically divergent first integer on identical input (actual count vs planner estimate). Real parity requires a cardinality estimator — out of scope.
- **`"One-off queries are not allowed to modify the database"` fixed literal (`module_host.rs:2293`,`:2299`,`:2427`,`:2440`).** Decorates `execute_plan` errors only. Shunter one-off paths have no view-write failure mode that triggers it.
- **`"Subscribing to event tables requires WebSocket v2..."` fixed literal (`module_subscription_actor.rs:659-663`).** Shunter has no `returns_event_table()` equivalent; event-table feature not implemented.

## Out Of Scope

- SQL surface widening beyond what the parser already admits
- Fanout/QueryID correlation redesign (closed under `P0-SUBSCRIPTION-030`)
- Reopening closed parity rows without fresh failing evidence
- Non-OI-002 tech-debt

## Validation

```bash
rtk go test <touched packages> -count=1 -v
rtk go fmt <touched packages>
rtk go vet <touched packages>
rtk go test ./... -count=1
```

## Doc Follow-Through

After the implementation is green:
- update `TECH-DEBT.md::OI-002` summary only if the closure removes a risk listed there
- rewrite this handoff to the next live target, keeping startup reading minimal

## Working Tree Caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves (the `docs/hosted-runtime-planning/V1-*` → `V1/V1-*` rename set, and modifications to `AGENTS.md`, `CLAUDE.md`, `HOSTED_RUNTIME_PLANNING_HANDOFF.md`, `README.md`). Do not mix those into a TECH-DEBT / OI-002 implementation slice unless the user explicitly asks.
