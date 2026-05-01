package shunter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
)

func TestRuntimeHealthExpandedBeforeStartAndAfterClose(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:                 t.TempDir(),
		ExecutorQueueCapacity:   3,
		DurabilityQueueCapacity: 5,
		EnableProtocol:          true,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	health := rt.Health()
	if health.State != RuntimeStateBuilt || health.Ready || health.Degraded {
		t.Fatalf("built health = %+v, want built not-ready non-degraded", health)
	}
	if health.Executor.Started || health.Executor.AdmissionReady || health.Executor.InboxDepth != 0 || health.Executor.InboxCapacity != 3 {
		t.Fatalf("built executor health = %+v, want absent depth 0 capacity 3", health.Executor)
	}
	if health.Durability.Started || health.Durability.QueueDepth != 0 || health.Durability.QueueCapacity != 5 || health.Durability.DurableTxID != 0 {
		t.Fatalf("built durability health = %+v, want absent depth 0 capacity 5 tx 0", health.Durability)
	}
	if health.Subscriptions.Started || health.Subscriptions.FanoutQueueDepth != 0 || health.Subscriptions.FanoutQueueCapacity != 3 {
		t.Fatalf("built subscription health = %+v, want absent fanout depth 0 capacity 3", health.Subscriptions)
	}
	if !health.Protocol.Enabled || health.Protocol.Ready || health.Protocol.ActiveConnections != 0 {
		t.Fatalf("built protocol health = %+v, want enabled not-ready no active connections", health.Protocol)
	}
	if !health.Recovery.Ran || !health.Recovery.Succeeded || health.Recovery.RecoveredTxID != 0 {
		t.Fatalf("built recovery health = %+v, want successful fresh recovery tx 0", health.Recovery)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	connID := types.ConnectionID{7}
	rt.protocolConns.Add(&protocol.Conn{ID: connID})
	rt.protocolConns.Remove(connID)
	rt.protocolConns.RecordRejected()

	health = rt.Health()
	if !health.Ready || !health.Executor.Started || !health.Executor.AdmissionReady ||
		!health.Durability.Started || !health.Subscriptions.Started || !health.Protocol.Ready {
		t.Fatalf("ready health = %+v, want all started and ready", health)
	}
	if health.Protocol.AcceptedConnections != 1 || health.Protocol.RejectedConnections != 1 || health.Protocol.ActiveConnections != 0 {
		t.Fatalf("ready protocol counters = %+v, want accepted=1 rejected=1 active=0", health.Protocol)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	health = rt.Health()
	if health.State != RuntimeStateClosed || health.Ready || health.Degraded {
		t.Fatalf("closed health = %+v, want closed not-ready non-degraded", health)
	}
	if health.Executor.Started || health.Durability.Started || health.Subscriptions.Started {
		t.Fatalf("closed subsystem health = executor=%+v durability=%+v subscriptions=%+v, want stopped",
			health.Executor, health.Durability, health.Subscriptions)
	}
	if health.Executor.InboxDepth != 0 || health.Executor.InboxCapacity != 3 ||
		health.Durability.QueueDepth != 0 || health.Durability.QueueCapacity != 5 ||
		health.Subscriptions.FanoutQueueDepth != 0 || health.Subscriptions.FanoutQueueCapacity != 3 {
		t.Fatalf("closed capacities/depths = %+v, want retained capacities and zero depths", health)
	}
	if health.Protocol.AcceptedConnections != 1 || health.Protocol.RejectedConnections != 1 {
		t.Fatalf("closed protocol counters = %+v, want retained accepted/rejected counts", health.Protocol)
	}
}

func TestRuntimeHealthFreshBootstrapRecovery(t *testing.T) {
	rt := buildValidTestRuntime(t)

	health := rt.Health()
	if !health.Recovery.Ran || !health.Recovery.Succeeded || health.Recovery.RecoveredTxID != 0 {
		t.Fatalf("fresh recovery health = %+v, want ran successful recovered tx 0", health.Recovery)
	}
}

func TestRuntimeHealthRecoverySkippedSnapshotDegrades(t *testing.T) {
	dir := t.TempDir()
	initial, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	initial.state.SetCommittedTxID(1)
	if err := commitlog.NewSnapshotWriter(dir, initial.registry).CreateSnapshot(initial.state, 1); err != nil {
		t.Fatalf("create snapshot to corrupt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1", "snapshot"), []byte("corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt snapshot: %v", err)
	}

	rt, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("recovery Build returned error: %v", err)
	}
	health := rt.Health()
	if !health.Degraded || health.Recovery.SkippedSnapshots != 1 {
		t.Fatalf("recovery health = %+v, want degraded with one skipped snapshot", health)
	}
	if got := runtimePrimaryDegradedReason(health); got != runtimeDegradedReasonRecoverySkipped {
		t.Fatalf("degraded reason = %q, want %q", got, runtimeDegradedReasonRecoverySkipped)
	}
}

func TestRuntimeHealthDegradedReasonPriority(t *testing.T) {
	health := RuntimeHealth{
		State: RuntimeStateFailed,
		Ready: true,
		Executor: ExecutorHealth{
			Fatal: true,
		},
		Durability: DurabilityHealth{
			Fatal: true,
		},
		Protocol: ProtocolHealth{
			Enabled: true,
			Ready:   false,
		},
		Subscriptions: SubscriptionHealth{
			FanoutFatal: true,
		},
		Recovery: RecoveryHealth{
			Succeeded:           true,
			DamagedTailSegments: 1,
			SkippedSnapshots:    1,
		},
	}

	assertDegradedReason(t, health, runtimeDegradedReasonRuntimeFailed)
	health.State = RuntimeStateReady
	assertDegradedReason(t, health, runtimeDegradedReasonExecutorFatal)
	health.Executor.Fatal = false
	assertDegradedReason(t, health, runtimeDegradedReasonDurabilityFatal)
	health.Durability.Fatal = false
	assertDegradedReason(t, health, runtimeDegradedReasonFanoutFatal)
	health.Subscriptions.FanoutFatal = false
	assertDegradedReason(t, health, runtimeDegradedReasonRecoveryDamagedTail)
	health.Recovery.DamagedTailSegments = 0
	assertDegradedReason(t, health, runtimeDegradedReasonRecoverySkipped)
	health.Recovery.SkippedSnapshots = 0
	assertDegradedReason(t, health, runtimeDegradedReasonProtocolNotReady)
	health.Protocol.Ready = true
	assertDegradedReason(t, health, runtimeDegradedReasonNone)
}

func TestRuntimeHealthExecutorAndDurabilityFatalAppearWithoutBlocking(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Redaction: RedactionConfig{ErrorMessageMaxBytes: 64},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	fatalErr := errors.New("token=secret-value row=hidden " + strings.Repeat("x", 200))
	rt.mu.Lock()
	rt.executorFatalErr = fatalErr
	rt.durabilityFatalErr = fatalErr
	rt.fanoutFatalErr = fatalErr
	rt.lastErr = fatalErr
	rt.stateName = RuntimeStateFailed
	rt.mu.Unlock()

	done := make(chan RuntimeHealth, 1)
	go func() { done <- rt.Health() }()

	var health RuntimeHealth
	select {
	case health = <-done:
	case <-time.After(time.Second):
		t.Fatal("Health blocked with latched fatal facts")
	}

	if !health.Executor.Fatal || !health.Durability.Fatal || !health.Subscriptions.FanoutFatal || !health.Degraded {
		t.Fatalf("fatal health = %+v, want executor/durability/fanout fatal degraded", health)
	}
	assertHealthErrorRedactedAndBounded(t, health.LastError, 64)
	assertHealthErrorRedactedAndBounded(t, health.Executor.FatalError, 64)
	assertHealthErrorRedactedAndBounded(t, health.Durability.FatalError, 64)
	assertHealthErrorRedactedAndBounded(t, health.Subscriptions.FanoutFatalError, 64)
}

func TestRuntimeHealthProtocolDisabledReadyDoesNotDegrade(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	health := rt.Health()
	if !health.Ready || health.Degraded {
		t.Fatalf("disabled protocol runtime health = %+v, want ready non-degraded", health)
	}
	if health.Protocol.Enabled || health.Protocol.Ready {
		t.Fatalf("disabled protocol health = %+v, want enabled=false ready=false", health.Protocol)
	}
}

func TestHostHealthExpandedNilEmptyAggregateAndDetached(t *testing.T) {
	var nilHost *Host
	nilHealth := nilHost.Health()
	if nilHealth.Ready || !nilHealth.Degraded || nilHealth.Modules == nil || len(nilHealth.Modules) != 0 {
		t.Fatalf("nil host health = %+v, want not-ready degraded empty modules", nilHealth)
	}
	emptyHealth := (&Host{}).Health()
	if emptyHealth.Ready || !emptyHealth.Degraded || emptyHealth.Modules == nil || len(emptyHealth.Modules) != 0 {
		t.Fatalf("empty host health = %+v, want not-ready degraded empty modules", emptyHealth)
	}

	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	health := host.Health()
	if health.Ready || health.Degraded || len(health.Modules) != 2 {
		t.Fatalf("built host health = %+v, want not-ready non-degraded two modules", health)
	}
	health.Modules[0].Name = "mutated"
	if again := host.Health(); again.Modules[0].Name != "chat" {
		t.Fatalf("host health modules aliased: got %q, want chat", again.Modules[0].Name)
	}

	chat.mu.Lock()
	chat.executorFatalErr = errors.New("executor fatal")
	chat.mu.Unlock()
	health = host.Health()
	if !health.Degraded {
		t.Fatalf("host health = %+v, want degraded when a module is degraded", health)
	}
	chat.mu.Lock()
	chat.executorFatalErr = nil
	chat.mu.Unlock()

	if err := host.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })
	health = host.Health()
	if !health.Ready || health.Degraded {
		t.Fatalf("ready host health = %+v, want ready non-degraded", health)
	}
}

func TestDescribeReturnsDetachedExpandedHealth(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:               t.TempDir(),
		ExecutorQueueCapacity: 4,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	desc := rt.Describe()
	if desc.Health.Executor.InboxCapacity != 4 || !desc.Health.Recovery.Ran {
		t.Fatalf("runtime description health = %+v, want expanded health", desc.Health)
	}
	desc.Health.Executor.InboxCapacity = 99
	if again := rt.Describe(); again.Health.Executor.InboxCapacity != 4 {
		t.Fatalf("runtime description health aliased: got capacity %d, want 4", again.Health.Executor.InboxCapacity)
	}

	host, err := NewHost(HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: rt})
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}
	hostDesc := host.Describe()
	if hostDesc.Modules[0].Runtime.Health.Executor.InboxCapacity != 4 {
		t.Fatalf("host description runtime health = %+v, want expanded health", hostDesc.Modules[0].Runtime.Health)
	}
	hostDesc.Modules[0].Runtime.Health.Executor.InboxCapacity = 1
	if again := host.Describe(); again.Modules[0].Runtime.Health.Executor.InboxCapacity != 4 {
		t.Fatalf("host description health aliased: got capacity %d, want 4",
			again.Modules[0].Runtime.Health.Executor.InboxCapacity)
	}
}

func assertDegradedReason(t *testing.T, health RuntimeHealth, want runtimeDegradedReason) {
	t.Helper()
	if got := runtimePrimaryDegradedReason(health); got != want {
		t.Fatalf("runtimePrimaryDegradedReason(%+v) = %q, want %q", health, got, want)
	}
}

func assertHealthErrorRedactedAndBounded(t *testing.T, got string, maxBytes int) {
	t.Helper()
	if got == "" {
		t.Fatal("health error string is empty")
	}
	if len(got) > maxBytes {
		t.Fatalf("health error string length = %d, want <= %d: %q", len(got), maxBytes, got)
	}
	if strings.Contains(got, "secret-value") || strings.Contains(got, "hidden") {
		t.Fatalf("health error string was not redacted: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("health error string = %q, want redaction marker", got)
	}
}
