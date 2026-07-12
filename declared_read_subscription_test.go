package shunter

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
)

func TestDeclaredViewSubscriptionUnsubscribeReleasesOwnedStateIdempotently(t *testing.T) {
	rt := buildStartedOwnedViewRuntime(t, nil)
	defer rt.Close()
	sub, err := rt.SubscribeView(context.Background(), "live_messages", 41)
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 1 {
		t.Fatalf("active subscriptions = %d, want 1", active)
	}

	if err := sub.Unsubscribe(context.Background()); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("active subscriptions after cleanup = %d, want 0", active)
	}
}

func TestDeclaredViewSubscriptionConcurrentCloseRunsCleanupOnce(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	cleanup := &declaredViewSubscriptionCleanup{
		unsubscribeFn: func(context.Context) (bool, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return true, nil
		},
	}
	sub := DeclaredViewSubscription{cleanup: cleanup}
	const callers = 8
	errs := make(chan error, callers)
	for range callers {
		go func() { errs <- sub.Close() }()
	}
	<-started
	close(release)
	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Close: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestDeclaredViewSubscriptionCleanupErrorsAreRetryableOrTerminal(t *testing.T) {
	retryErr := errors.New("cleanup not admitted")
	var retryCalls int
	retrySub := DeclaredViewSubscription{cleanup: &declaredViewSubscriptionCleanup{
		unsubscribeFn: func(context.Context) (bool, error) {
			retryCalls++
			if retryCalls == 1 {
				return false, retryErr
			}
			return true, nil
		},
	}}
	if err := retrySub.Close(); !errors.Is(err, retryErr) {
		t.Fatalf("first retryable Close error = %v, want %v", err, retryErr)
	}
	if err := retrySub.Close(); err != nil {
		t.Fatalf("second retryable Close: %v", err)
	}
	if retryCalls != 2 {
		t.Fatalf("retryable cleanup calls = %d, want 2", retryCalls)
	}

	terminalErr := errors.New("removed with final evaluation error")
	var terminalCalls int
	terminalSub := DeclaredViewSubscription{cleanup: &declaredViewSubscriptionCleanup{
		unsubscribeFn: func(context.Context) (bool, error) {
			terminalCalls++
			return true, terminalErr
		},
	}}
	for i := 0; i < 2; i++ {
		if err := terminalSub.Close(); !errors.Is(err, terminalErr) {
			t.Fatalf("terminal Close %d error = %v, want %v", i+1, err, terminalErr)
		}
	}
	if terminalCalls != 1 {
		t.Fatalf("terminal cleanup calls = %d, want 1", terminalCalls)
	}
}

func TestSubscribeViewCancellationBeforeExecutorDispatchDoesNotLeak(t *testing.T) {
	blockStarted := make(chan struct{})
	releaseBlock := make(chan struct{})
	rt := buildStartedOwnedViewRuntime(t, func(*schema.ReducerContext, []byte) ([]byte, error) {
		close(blockStarted)
		<-releaseBlock
		return nil, nil
	})
	defer rt.Close()
	reducerDone := make(chan error, 1)
	go func() {
		_, err := rt.CallReducer(context.Background(), "block", nil)
		reducerDone <- err
	}()
	<-blockStarted

	ctx, cancel := context.WithCancel(context.Background())
	subscribeDone := make(chan error, 1)
	go func() {
		_, err := rt.SubscribeView(ctx, "live_messages", 42)
		subscribeDone <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for rt.executor.InboxDepth() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rt.executor.InboxDepth() == 0 {
		t.Fatal("SubscribeView command was not queued behind blocking reducer")
	}
	cancel()
	close(releaseBlock)
	if err := <-reducerDone; err != nil {
		t.Fatalf("blocking reducer: %v", err)
	}
	if err := <-subscribeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("SubscribeView error = %v, want context.Canceled", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("active subscriptions after pre-dispatch cancellation = %d, want 0", active)
	}
}

func TestDeclaredViewUnsubscribeCancellationBeforeDispatchCanRetry(t *testing.T) {
	blockStarted := make(chan struct{})
	releaseBlock := make(chan struct{})
	rt := buildStartedOwnedViewRuntime(t, func(*schema.ReducerContext, []byte) ([]byte, error) {
		close(blockStarted)
		<-releaseBlock
		return nil, nil
	})
	defer rt.Close()
	sub, err := rt.SubscribeView(context.Background(), "live_messages", 45)
	if err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	reducerDone := make(chan error, 1)
	go func() {
		_, err := rt.CallReducer(context.Background(), "block", nil)
		reducerDone <- err
	}()
	<-blockStarted

	ctx, cancel := context.WithCancel(context.Background())
	unsubscribeDone := make(chan error, 1)
	go func() { unsubscribeDone <- sub.Unsubscribe(ctx) }()
	waitForOwnedViewExecutorQueue(t, rt)
	cancel()
	close(releaseBlock)
	if err := <-reducerDone; err != nil {
		t.Fatalf("blocking reducer: %v", err)
	}
	if err := <-unsubscribeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Unsubscribe error = %v, want context.Canceled", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 1 {
		t.Fatalf("active subscriptions after canceled cleanup = %d, want 1", active)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("active subscriptions after retry = %d, want 0", active)
	}
}

func TestSubscribeViewCancellationConcurrentWithCompletedRegistrationReturnsOwner(t *testing.T) {
	rt := buildStartedOwnedViewRuntime(t, nil)
	defer rt.Close()
	replyStarted := make(chan struct{})
	releaseReply := make(chan struct{})
	previous := declaredViewRegisterReplyHook
	declaredViewRegisterReplyHook = func() {
		close(replyStarted)
		<-releaseReply
	}
	t.Cleanup(func() { declaredViewRegisterReplyHook = previous })

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan struct {
		sub DeclaredViewSubscription
		err error
	}, 1)
	go func() {
		sub, err := rt.SubscribeView(ctx, "live_messages", 43)
		resultCh <- struct {
			sub DeclaredViewSubscription
			err error
		}{sub: sub, err: err}
	}()
	<-replyStarted
	cancel()
	close(releaseReply)
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("SubscribeView after completed registration: %v", result.err)
	}
	if err := result.sub.Close(); err != nil {
		t.Fatalf("Close returned owner: %v", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("active subscriptions after returned owner cleanup = %d, want 0", active)
	}
}

func TestDeclaredViewSubscriptionDuplicateIDAndRuntimeShutdownOwnership(t *testing.T) {
	rt := buildStartedOwnedViewRuntime(t, nil)
	first, err := rt.SubscribeView(context.Background(), "live_messages", 44)
	if err != nil {
		t.Fatalf("first SubscribeView: %v", err)
	}
	if _, err := rt.SubscribeView(context.Background(), "live_messages", 44); !errors.Is(err, subscription.ErrQueryIDAlreadyLive) {
		t.Fatalf("duplicate SubscribeView error = %v, want ErrQueryIDAlreadyLive", err)
	}
	if active := rt.subscriptions.ActiveSubscriptionSets(); active != 1 {
		t.Fatalf("active subscriptions after duplicate = %d, want 1", active)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("subscription Close after runtime shutdown: %v", err)
	}
}

func buildStartedOwnedViewRuntime(t *testing.T, blocker schema.ReducerHandler) *Runtime {
	t.Helper()
	mod := validChatModule().View(ViewDeclaration{Name: "live_messages", SQL: "SELECT * FROM messages"})
	if blocker != nil {
		mod.Reducer("block", blocker)
	}
	return buildStartedDeclaredReadRuntime(t, mod)
}

func waitForOwnedViewExecutorQueue(t *testing.T, rt *Runtime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for rt.executor.InboxDepth() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rt.executor.InboxDepth() == 0 {
		t.Fatal("subscription command was not queued behind blocking reducer")
	}
}
