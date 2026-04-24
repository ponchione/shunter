package shunter

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeInitialHealthIsBuiltAndNotReady(t *testing.T) {
	rt := buildValidTestRuntime(t)

	if rt.Ready() {
		t.Fatal("new runtime is ready before Start")
	}
	health := rt.Health()
	if health.State != RuntimeStateBuilt {
		t.Fatalf("state = %q, want %q", health.State, RuntimeStateBuilt)
	}
	if health.Ready {
		t.Fatal("health reports ready before Start")
	}
	if health.LastError != nil {
		t.Fatalf("unexpected last error: %v", health.LastError)
	}
}

func TestRuntimeStartAndCloseOwnLifecycle(t *testing.T) {
	rt := buildValidTestRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	cancel()
	if !rt.Ready() {
		t.Fatal("runtime not ready after Start")
	}
	health := rt.Health()
	if health.State != RuntimeStateReady || !health.Ready {
		t.Fatalf("health after Start = %+v", health)
	}
	if rt.durability == nil {
		t.Fatal("durability worker not created")
	}
	if rt.executor == nil {
		t.Fatal("executor not created")
	}
	if rt.scheduler == nil {
		t.Fatal("scheduler not created")
	}
	if rt.subscriptions == nil {
		t.Fatal("subscription manager not created")
	}
	if rt.fanOutWorker == nil {
		t.Fatal("fan-out worker not created")
	}
	if !rt.Ready() {
		t.Fatal("canceling Start context after readiness stopped runtime; want startup-only context")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after Close")
	}
	if got := rt.Health().State; got != RuntimeStateClosed {
		t.Fatalf("state after Close = %q, want %q", got, RuntimeStateClosed)
	}
}

func TestRuntimeStartIsIdempotentAfterReady(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("second Start on ready runtime: %v", err)
	}
}

func TestRuntimeCloseIsIdempotent(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRuntimeCloseBeforeStartClosesRuntime(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Close(); err != nil {
		t.Fatalf("Close before Start: %v", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after Close before Start")
	}
	if got := rt.Health().State; got != RuntimeStateClosed {
		t.Fatalf("state after Close before Start = %q, want %q", got, RuntimeStateClosed)
	}
	if err := rt.Start(context.Background()); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Start after Close error = %v, want ErrRuntimeClosed", err)
	}
}

func TestRuntimeStartWithCanceledContextFailsWithoutReadiness(t *testing.T) {
	rt := buildValidTestRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rt.Start(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after canceled Start")
	}
	health := rt.Health()
	if health.State == RuntimeStateReady {
		t.Fatalf("state after canceled Start = ready")
	}
	if health.LastError == nil {
		t.Fatal("LastError not recorded after canceled Start")
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("retry Start after canceled startup: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close after retry: %v", err)
	}
}

func TestRuntimeStartFailureCleansPartialResources(t *testing.T) {
	rt := buildValidTestRuntime(t)
	injected := errors.New("injected lifecycle failure")
	oldHook := runtimeStartAfterDurabilityHook
	runtimeStartAfterDurabilityHook = func(*Runtime) error { return injected }
	defer func() { runtimeStartAfterDurabilityHook = oldHook }()

	err := rt.Start(context.Background())
	if err == nil || !errors.Is(err, injected) {
		t.Fatalf("Start error = %v, want injected failure", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after failed Start")
	}
	if rt.durability != nil || rt.executor != nil || rt.scheduler != nil || rt.fanOutWorker != nil || rt.subscriptions != nil {
		t.Fatalf("partial resources not cleaned up: health=%+v", rt.Health())
	}
	if rt.Health().LastError == nil {
		t.Fatal("LastError not recorded after failed Start")
	}

	runtimeStartAfterDurabilityHook = oldHook
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("retry Start after injected failure: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close after retry: %v", err)
	}
}

func buildValidTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	return rt
}
