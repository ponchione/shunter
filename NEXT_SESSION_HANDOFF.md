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

Do not reopen `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033`, the reused-hash elision, the `SubscriptionError.table_id: None` closure, the compile-origin `WithSql` suffix, the SubscribeSingle initial-eval `WithSql` suffix, or the SubscribeMulti initial-eval canned-message closure without fresh failing regression evidence.

## Scout Prompts

Before starting implementation, answer one of these with live-code evidence:

- Is there a reference subscribe/unsubscribe error-path behavior Shunter still diverges on? Candidates remaining after the Single/Multi initial-eval closures: **UnsubscribeSingle** (`module_subscription_actor.rs:756`) WithSql-wraps the `evaluate_initial_subscription` result on the unsubscribe path, and **UnsubscribeMulti** (`:836`) uses a plain `return_on_err!` (raw err text, no suffix / no canned substitution) on `evaluate_queries` during unsubscribe. Shunter's `subscription/register_set.go::UnregisterSet` currently SWALLOWS these errors ("Final-delta errors are non-fatal — the subscription still drops"); reference BAILS and emits SubscriptionError before dropping. Closing either requires (a) surfacing the error from `UnregisterSet` and (b) for UnsubscribeSingle, storing the SQL text alongside `queryState` at register time so the unsubscribe path has it available (register-time SQLText flows through `RegisterSubscriptionSetRequest.SQLText` today but is not persisted into `queryState`). Structural — wider than the initial-eval slices that just closed. The duplicate-QID `"Subscription with id {query_id} already exists for client: {client_id}"` message shape (`module_subscription_manager.rs:989`, `:1055`) is likely fragile in Go because the reference text formats a Rust tuple via Debug `{:?}`; treat message-text parity as only bounded where reference uses fixed literals.
- Is there a bounded SQL surface shape latently admitted by parser+compile but not pinned end-to-end — check parser operator combinations that compile cleanly but have no one-off or subscribe-rejection pin? The current subscribe/one-off parity surface in `protocol/handle_subscribe_test.go` and `protocol/handle_oneoff_test.go` is heavily populated (40+ pins each); scout for specific operator combinations not yet covered rather than whole-shape audits.
- Is there a post-commit fanout ordering, QueryID stamping, or durability-gate edge case not yet pinned for a specific combined scenario (multi-subscription-per-connection with partial table overlap on actual overlapping tables, self-join aliased delta under concurrent update)? Disjoint-table multi-sub and same-connection reused-hash post-elision stamping are already pinned by `TestEvalMultipleTableUpdatesGrouped` and `TestEvalFanoutCarriesClientQueryIDForEachSubscription` respectively.

Pick the smallest bounded slice with a reference anchor and a concrete failing test you can write first.

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
