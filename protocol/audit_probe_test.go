package protocol

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCloseAllReturnsOnContextWhileLateAdmissionCleansUp(t *testing.T) {
	inbox := &blockedAdmissionInbox{
		fakeInbox: &fakeInbox{},
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	mgr := NewConnManager()
	conn := testConnDirect(nil)
	lifecycleDone := make(chan error, 1)
	go func() {
		lifecycleDone <- conn.RunLifecycle(context.Background(), inbox, mgr)
	}()
	select {
	case <-inbox.started:
	case <-time.After(time.Second):
		t.Fatal("admission did not block")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	closeDone := make(chan struct{})
	go func() {
		mgr.CloseAll(ctx, inbox)
		close(closeDone)
	}()
	select {
	case <-closeDone:
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("CloseAll returned before deadline: %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not respect its context deadline")
	}
	select {
	case <-conn.readCtx.Done():
	default:
		t.Fatal("reserved transport remained open after CloseAll returned")
	}
	select {
	case err := <-lifecycleDone:
		t.Fatalf("admission completed before release: %v", err)
	default:
	}
	if got := mgr.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount before late admission completion = %d, want 0", got)
	}
	if got := mgr.AcceptedCount(); got != 0 {
		t.Fatalf("AcceptedCount before late admission completion = %d, want 0", got)
	}

	close(inbox.release)
	select {
	case err := <-lifecycleDone:
		if !errors.Is(err, ErrConnectionManagerClosed) {
			t.Fatalf("late RunLifecycle error = %v, want ErrConnectionManagerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("late admission did not complete")
	}
	if got := mgr.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount after late admission completion = %d, want 0", got)
	}
	if got := mgr.AcceptedCount(); got != 0 {
		t.Fatalf("AcceptedCount after late admission completion = %d, want 0", got)
	}
	onDisconnect, disconnectSubs, events := inbox.disconnectSnapshot()
	if disconnectSubs != 1 || onDisconnect != 1 {
		t.Fatalf("late cleanup calls = (subs=%d,onDisconnect=%d), want (1,1)", disconnectSubs, onDisconnect)
	}
	if want := []string{"DisconnectClientSubscriptions", "OnDisconnect"}; fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("late cleanup order = %v, want %v", events, want)
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("late admission cleanup did not close the connection")
	}
	mgr.CloseAll(context.Background(), inbox)
	onDisconnect, disconnectSubs, _ = inbox.disconnectSnapshot()
	if disconnectSubs != 1 || onDisconnect != 1 {
		t.Fatalf("cleanup calls after repeated CloseAll = (subs=%d,onDisconnect=%d), want (1,1)", disconnectSubs, onDisconnect)
	}
}
