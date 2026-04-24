# Parity Decisions

This file keeps current implementation-facing parity decisions that are
still cited by code or tests. It is not a history log. For active status and
prioritization, use `TECH-DEBT.md` and the active root handoff.

Reference paths are grounding only. Do not copy or port source from
`reference/SpacetimeDB/`.

## Outcome Model

The reducer outcome protocol uses the reference-style heavy/light split.

Current contract:

- Callers observe reducer outcomes through `TransactionUpdate`.
- Non-callers with touched subscribed rows receive `TransactionUpdateLight`.
- Non-callers with no touched subscribed rows receive nothing.
- `ReducerCallResult` is removed from the protocol surface; its old tag is
  reserved and must not be reused.
- `TransactionUpdate` carries `UpdateStatus`, caller identity/connection,
  reducer call metadata, timestamp, execution duration, and energy.
- `UpdateStatus` has `Committed`, `Failed`, and `OutOfEnergy` arms.
- Rejections that happen before a transaction opens return a synthetic
  failed `TransactionUpdate` with `TxID == 0`.

Accepted deferrals:

- `EnergyQuantaUsed` is always zero because Shunter has no energy/quota
  subsystem.
- `OutOfEnergy` is wire-present but not emitted.
- Failure strings remain Shunter-specific. A future reducer-outcome parity
  slice can revisit the exact failure surface.

Authoritative pins:

- `protocol/parity_message_family_test.go`
- `protocol/parity_transaction_update_test.go`
- `executor/caller_metadata_test.go`
- `subscription/fanout_test.go`

## Protocol Rows Shape

The current row/update wire shape is a documented Shunter divergence, not a
reference byte-compatible wrapper chain.

Current contract:

- Applied envelopes and transaction updates use Shunter's flat row/update
  payloads.
- `SubscriptionUpdate` wire layout is:
  `query_id`, `table_name`, `inserts`, `deletes`.
- Inserts are encoded before deletes.
- The flat per-entry `QueryID` is client-facing correlation data.
- Row batches use Shunter's per-row length prefix format from
  `protocol/rowlist.go`, not reference `BsatnRowList` / `RowSizeHint`.
- The remaining wrapper-chain differences are consequences of that inner
  row-list divergence.

Accepted deferrals:

- `SubscribeRows`, `DatabaseUpdate`, `TableUpdate`, `QueryUpdate`,
  `CompressableQueryUpdate`, `BsatnRowList`, and `RowSizeHint` remain absent.
- `TableUpdate.num_rows` and deletes-before-inserts reference ordering are
  deferred with the full wrapper-chain rewrite.
- Removing the flat per-entry `QueryID` should happen only as part of a full
  wrapper-chain close.
- Inner compression remains deferred because Shunter already has outer
  envelope compression.

Reopen only with a concrete compatibility, migration, or bandwidth trigger.

Authoritative pins:

- `protocol/parity_rows_shape_test.go`
- `protocol/parity_applied_envelopes_test.go`
- `protocol/parity_message_family_test.go`
- `protocol/rowlist.go`
- `docs/decomposition/005-protocol/SPEC-005-protocol.md`

## Outbound Lag Policy

Current contract:

- `DefaultOutgoingBufferMessages` is `16 * 1024`.
- Outbound queue overflow disconnects the client.
- Fanout cleanup treats send-buffer overflow as a dropped-client path so
  subscription state is reclaimed.
- Incoming request backpressure is a Shunter-specific defensive limit and is
  not part of the outbound lag parity decision.

Accepted divergence:

- The reference lets the socket disappear without a clean close frame.
  Shunter sends WebSocket close code `1008` with reason `send buffer full`.

Reopen only if a real client or compatibility target requires the
reference's unclean close mechanism.

Authoritative pins:

- `protocol/options.go`
- `protocol/options_test.go`
- `protocol/parity_lag_policy_test.go`
- `protocol/backpressure_out_test.go`
- `subscription/fanout_worker.go`

## Commitlog Record Shape

The current commitlog wire is a canonical Shunter format, not a
reference byte-compatible on-disk format.

Current contract:

- Segment headers are 8 bytes: `SHNT`, version `1`, and zero reserved bytes.
- Shunter stores one physical record per transaction.
- Record headers are 14 bytes: `tx_id`, `record_type`, `flags`, and
  `data_len`.
- Record CRC is little-endian CRC32C over the record header plus payload.
- Payload is Shunter's versioned changeset format with deterministic table
  ordering.
- Reader/recovery tolerates an all-zero record-header tail as end of segment.
- Reserved bytes and record flags are strict.
- There is no epoch field, commit grouping, V0/V1 reference split, or
  reference records-buffer format.

Accepted deferrals:

- Reference byte-compatible segment magic and commit grouping.
- Reference epoch field and writer `set_epoch` API.
- Reference V0/V1 compatibility.
- Checksum-algorithm vocabulary alignment.
- Forked-offset detection.
- Full records-buffer parity, including reference transaction payload shape.
- Writer-side preallocation-friendly zero tails.

Reopen only with a concrete need to read reference-created logs, support
distributed epochs, or migrate the on-disk format.

Authoritative pins:

- `commitlog/wire_shape_test.go`
- `commitlog/replay_test.go`
- `commitlog/segment.go`
- `commitlog/changeset_codec.go`

## Scheduler Startup And Firing

Current contract:

- Startup replay scans `sys_scheduled`, enqueues past-due schedules, arms
  future schedules, and returns the maximum observed schedule id so
  post-restart allocation does not collide.
- Successful one-shot firings delete their schedule row.
- Successful interval firings advance from the intended fire time.
- Missing schedule rows during a cancel race are tolerated.
- Past-due replay preserves scan order rather than sorting by intended fire
  time.
- Failed one-shot firings leave the schedule row in place for retry.

Accepted deferrals:

- Reference-style `fn_start` clamping for the first repeated schedule time.
- Reference-style deletion of one-shot rows after panic.
- Sorting past-due startup replay by intended time.
- Reference commitlog workload labeling for scheduled firings.
- Startup ordering relative to lifecycle hooks.

Reopen only with workload evidence or a fresh parity regression.

Authoritative pins:

- `executor/scheduler_replay_test.go`
- `executor/scheduler_firing_test.go`
- `executor/scheduler_worker_test.go`
- `executor/sys_scheduled_test.go`
