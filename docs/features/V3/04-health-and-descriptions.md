# V3 Task 04: Health And Description Expansion

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 03 build and recovery observations

Objective: expand runtime and host health snapshots and descriptions to the
SPEC-007 section 9 shapes with cheap, detached, redacted, deterministic data.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 9, 9.1,
  9.2, 9.3, 10.3, and 14
- `docs/features/V3/03-build-recovery-observations.md`

Inspect:

```sh
rtk go doc . RuntimeHealth
rtk go doc . HostHealth
rtk go doc . Runtime.Describe
rtk go doc . Host.Describe
rtk go doc ./commitlog RecoveryReport
rtk grep -n 'RuntimeHealth|HostHealth|RuntimeDescription|HostDescription|Health\\(|Describe\\(' *.go
```

## Target Behavior

Expand `Runtime.Health()` to return the exact SPEC-007 public health model:

- `RuntimeHealth`
- `ExecutorHealth`
- `DurabilityHealth`
- `ProtocolHealth`
- `SubscriptionHealth`
- `RecoveryHealth`

Expand `Host.Health()` and host module health:

- `HostHealth.Ready`
- `HostHealth.Degraded`
- non-nil `HostHealth.Modules`
- detached `HostModuleHealth` entries with per-runtime health

Update `Runtime.Describe()` to include the expanded `RuntimeHealth`.
`Host.Describe()` must expose expanded health indirectly through each
`RuntimeDescription`, matching SPEC-007 section 9, without aliasing
runtime-owned mutable state.

Health snapshots must:

- avoid table scans, durability waits, goroutine waits, network I/O, and
  unbounded allocation
- report configured capacities even when workers/channels are absent or torn
  down
- report queue depths as `0` when the underlying queue is absent
- retain fatal facts and cumulative counters across close
- redact and bound `LastError` and `FatalError` strings
- classify `Ready` and `Degraded` using SPEC-007 sections 9.1 and 9.2
- choose the primary degraded reason with SPEC-007 section 9.3 priority

This task may add the degraded-reason helper used by later logging, but the
`runtime.health_degraded` event itself belongs to task 05.

## Tests To Add First

Add focused failing tests for:

- health before start and after close reports configured capacities, absent
  depths as zero, retained counters, and latched fatal facts
- fresh bootstrap health reports `Recovery.Ran=true`, `Succeeded=true`, and
  recovered transaction ID `0`
- recovery damaged tails or skipped snapshots set `Degraded=true`
- multiple degraded conditions choose the section 9.3 primary reason
  deterministically
- executor fatal and durability fatal state appear without blocking
- protocol disabled reports `Enabled=false`, `Ready=false`, and does not
  degrade the runtime by itself
- nil or empty host health returns `Ready=false`, `Degraded=true`, and
  `Modules=[]`
- host health aggregates ready/degraded across modules and returns detached
  slices
- `Runtime.Describe()` and `Host.Describe()` return detached expanded health
  data
- health error strings are redacted and bounded

## Validation

Run at least:

```sh
rtk go fmt .
rtk go test . -run 'Test.*(Health|Describe|Host|Runtime|Recovery|Degraded|Protocol)' -count=1
rtk go vet .
```

Expand to `rtk go test ./... -count=1` if exported health or description JSON
changes affect contracts, codegen, or protocol tests.

## Completion Notes

When complete, update this file with:

- exported health type changes
- degraded reason helper behavior
- description payload changes
- compatibility concerns for existing tests or users
- validation commands run
