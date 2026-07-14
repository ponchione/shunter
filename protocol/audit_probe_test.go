package protocol

import (
	"context"
	"testing"
	"time"
)

func TestAuditProbeCloseAllIgnoresExpiredContextWhileAdmissionBlocked(t *testing.T) {
	inbox := &blockedAdmissionInbox{
		fakeInbox: &fakeInbox{},
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	mgr := NewConnManager()
	conn := NewConn(testConnectionID(99), testIdentity(99), "", false, nil, nil)
	lifecycleDone := make(chan error, 1)
	go func() {
		lifecycleDone <- conn.RunLifecycle(context.Background(), inbox, mgr)
	}()
	select {
	case <-inbox.started:
	case <-time.After(time.Second):
		t.Fatal("admission did not block")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	closeDone := make(chan struct{})
	go func() {
		mgr.CloseAll(ctx, inbox)
		close(closeDone)
	}()
	<-ctx.Done()
	select {
	case <-closeDone:
		t.Fatal("CloseAll returned at its context deadline; probe no longer reproduces")
	case <-time.After(50 * time.Millisecond):
		t.Log("CloseAll remained blocked after its context expired")
	}

	close(inbox.release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not return after admission released")
	}
	<-lifecycleDone
}
