# V3 Task 07: Subsystem Metrics

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 06 metrics core, lifecycle, and recovery

Objective: instrument protocol, executor, reducer, durability, subscription,
and fan-out metrics with exact SPEC-007 names, labels, and timing.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 5, 6,
  6.1, 6.2, 11, and 14
- `docs/features/V3/06-metrics-core-lifecycle-recovery.md`

Inspect:

```sh
rtk go doc ./protocol Server
rtk go doc ./protocol ConnManager
rtk go doc ./executor Executor
rtk go doc ./executor ReducerRegistry
rtk go doc ./commitlog DurabilityWorker
rtk go doc ./subscription Manager
rtk grep -n 'Submit|SubmitWithContext|Run\\(|CallReducer|HandleSubscribe|RegisterSet|UnregisterSet|EvalAndBroadcast|Dropped|Durability|queue|backpressure' executor protocol subscription commitlog *.go
```

## Target Behavior

Instrument protocol metrics:

- `protocol_connections`
- `protocol_connections_total`
- `protocol_messages_total`
- `protocol_backpressure_total`

Instrument executor and reducer metrics:

- `executor_commands_total`
- `executor_command_duration_seconds`
- `executor_inbox_depth`
- `executor_fatal`
- `reducer_calls_total`
- `reducer_duration_seconds`

Instrument durability and subscription metrics:

- `durability_durable_tx_id`
- `durability_queue_depth`
- `durability_failures_total`
- `subscription_active`
- `subscription_eval_duration_seconds`
- `subscription_fanout_errors_total`
- `subscription_dropped_clients_total`

Preserve exact timing rules:

- submit-time executor rejections increment command counters but do not record
  command duration histograms
- executor command duration excludes inbox wait time
- reducer duration measures only handler wall-clock time
- protocol connection accepted counters increment only after admission,
  connection registration, and identity token write succeed
- rejected connections map to exactly one rejection result
- queue gauges update after enqueue and dequeue
- fatal gauges latch
- durability failures that are fatal or that prevent startup/recovery from
  continuing increment one mapped `durability_failures_total` counter
- subscription dropped-client counters include discarded drop signals when the
  drop-signal channel is full

Reducer labels must use declared names by default and `"_all"` when
`ReducerLabelModeAggregate` is configured. Unknown reducer labels are allowed
only when an observation occurs before Shunter can resolve a declared reducer
name.

## Tests To Add First

Add focused failing tests for:

- protocol connection open/close updates active connection gauge and accepted
  counter
- connection rejection maps exactly one result and logs/records it
- auth failure increments rejected counter with `rejected_auth`
- malformed protocol message increments `protocol_messages_total` with
  `kind="unknown"` or decoded kind
- inbound and outbound backpressure increment the direction-specific counter
- executor submit rejection increments
  `executor_commands_total{result="rejected"}`
- executor command duration histogram records only dequeued commands
- executor inbox depth updates after enqueue/dequeue
- reducer committed/user error/panic/internal/permission outcomes increment
  distinct reducer result labels
- default reducer labels use the declared reducer name
- `ReducerLabelModeAggregate` emits reducer label `"_all"`
- durability queue depth and durable tx gauges update at the required points
- fatal durability failures increment mapped `durability_failures_total`
- subscription active gauge updates after register, unregister, disconnect
  cleanup, and runtime close
- subscription eval errors record
  `subscription_eval_duration_seconds{result="error"}` and log
  `subscription.eval_error` without inventing a separate eval-error counter
- fan-out errors and buffer-full drops increment the required reason counters

## Validation

Run at least:

```sh
rtk go fmt . ./commitlog ./executor ./protocol ./subscription
rtk go test ./protocol -run 'Test.*(Metrics|Connection|Protocol|Backpressure|Auth|Message)' -count=1
rtk go test ./executor -run 'Test.*(Metrics|Reducer|Command|Inbox|Fatal)' -count=1
rtk go test ./subscription -run 'Test.*(Metrics|Subscription|Fanout|Dropped|Eval)' -count=1
rtk go test ./commitlog -run 'Test.*(Durability|Metrics|Failure|Queue)' -count=1
rtk go test . -run 'Test.*(Metrics|Reducer|Protocol|Subscription|Durability)' -count=1
rtk go vet . ./commitlog ./executor ./protocol ./subscription
```

Expand to `rtk go test ./... -count=1` if subsystem interfaces change.

## Completion Notes

When complete, update this file with:

- metric families implemented by subsystem
- label/result/reason mappings covered by tests
- any metric intentionally deferred with reason
- validation commands run
