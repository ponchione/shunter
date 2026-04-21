package executor

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type registerDispatchSubs struct {
	disconnectConnID types.ConnectionID
	disconnectErr    error

	registerSetCalled    bool
	registerSetReq       subscription.SubscriptionSetRegisterRequest
	registerSetView      store.CommittedReadView
	registerSetResult    subscription.SubscriptionSetRegisterResult
	registerSetErr       error
	unregisterSetCalled  bool
	unregisterSetConn    types.ConnectionID
	unregisterSetQueryID uint32
	unregisterSetView    store.CommittedReadView
	unregisterSetResult  subscription.SubscriptionSetUnregisterResult
	unregisterSetErr     error
}

func (f *registerDispatchSubs) RegisterSet(req subscription.SubscriptionSetRegisterRequest, view store.CommittedReadView) (subscription.SubscriptionSetRegisterResult, error) {
	f.registerSetCalled = true
	f.registerSetReq = req
	f.registerSetView = view
	return f.registerSetResult, f.registerSetErr
}
func (f *registerDispatchSubs) UnregisterSet(connID types.ConnectionID, queryID uint32, view store.CommittedReadView) (subscription.SubscriptionSetUnregisterResult, error) {
	f.unregisterSetCalled = true
	f.unregisterSetConn = connID
	f.unregisterSetQueryID = queryID
	f.unregisterSetView = view
	return f.unregisterSetResult, f.unregisterSetErr
}
func (f *registerDispatchSubs) DisconnectClient(connID types.ConnectionID) error {
	f.disconnectConnID = connID
	return f.disconnectErr
}
func (f *registerDispatchSubs) EvalAndBroadcast(types.TxID, *store.Changeset, store.CommittedReadView, subscription.PostCommitMeta) {
}
func (f *registerDispatchSubs) DroppedClients() <-chan types.ConnectionID { return nil }

type trackingSnapshot struct {
	store.CommittedReadView
	closed bool
}

func (s *trackingSnapshot) Close() {
	s.closed = true
	s.CommittedReadView.Close()
}

func TestDisconnectSubscriptionDispatchDelegates(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{}
	exec.subs = fakeSubs

	discCh := make(chan error, 1)
	exec.dispatch(DisconnectClientSubscriptionsCmd{ConnID: types.ConnectionID{3}, ResponseCh: discCh})
	if err := <-discCh; err != nil {
		t.Fatalf("disconnect error = %v", err)
	}
	if fakeSubs.disconnectConnID != (types.ConnectionID{3}) {
		t.Fatalf("disconnect delegation connID = %v, want 3", fakeSubs.disconnectConnID)
	}
}

func TestDispatchRegisterSubscriptionSet(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{
		registerSetResult: subscription.SubscriptionSetRegisterResult{QueryID: 42},
	}
	exec.subs = fakeSubs

	var tracked *trackingSnapshot
	exec.snapshotFn = func() store.CommittedReadView {
		tracked = &trackingSnapshot{CommittedReadView: exec.committed.Snapshot()}
		return tracked
	}

	req := subscription.SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{9},
		QueryID: 42,
		Predicates: []subscription.Predicate{
			subscription.AllRows{Table: 1},
		},
	}
	var (
		gotResult subscription.SubscriptionSetRegisterResult
		gotErr    error
		called    bool
	)
	exec.dispatch(RegisterSubscriptionSetCmd{
		Request: req,
		Reply: func(r subscription.SubscriptionSetRegisterResult, err error) {
			called = true
			gotResult = r
			gotErr = err
		},
	})

	if !called {
		t.Fatal("Reply was not invoked")
	}
	if gotErr != nil {
		t.Fatalf("Reply got err = %v, want nil", gotErr)
	}
	if gotResult.QueryID != 42 {
		t.Fatalf("Reply got QueryID = %d, want 42", gotResult.QueryID)
	}
	if gotResult.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("Reply got zero TotalHostExecutionDurationMicros, want non-zero measured duration")
	}
	if !fakeSubs.registerSetCalled {
		t.Fatal("fakeSubs.RegisterSet was not called")
	}
	if fakeSubs.registerSetReq.QueryID != 42 || len(fakeSubs.registerSetReq.Predicates) != 1 {
		t.Fatalf("fakeSubs.registerSetReq = %+v", fakeSubs.registerSetReq)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("register-set snapshot should be closed")
	}
	if fakeSubs.registerSetView != tracked {
		t.Fatal("register-set should receive the acquired snapshot view")
	}
}

func TestDispatchUnregisterSubscriptionSet(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{}
	exec.subs = fakeSubs

	var tracked *trackingSnapshot
	exec.snapshotFn = func() store.CommittedReadView {
		tracked = &trackingSnapshot{CommittedReadView: exec.committed.Snapshot()}
		return tracked
	}

	var (
		gotResult subscription.SubscriptionSetUnregisterResult
		gotErr    error
		called    bool
	)
	exec.dispatch(UnregisterSubscriptionSetCmd{
		ConnID:  types.ConnectionID{9},
		QueryID: 42,
		Reply: func(r subscription.SubscriptionSetUnregisterResult, err error) {
			called = true
			gotResult = r
			gotErr = err
		},
	})

	if !called {
		t.Fatal("Reply was not invoked")
	}
	if gotErr != nil {
		t.Fatalf("unexpected err: %v", gotErr)
	}
	if !fakeSubs.unregisterSetCalled {
		t.Fatal("fakeSubs.UnregisterSet was not called")
	}
	if fakeSubs.unregisterSetConn != (types.ConnectionID{9}) || fakeSubs.unregisterSetQueryID != 42 {
		t.Fatalf("fakeSubs got conn=%x query=%d", fakeSubs.unregisterSetConn, fakeSubs.unregisterSetQueryID)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("unregister-set snapshot should be closed")
	}
	if fakeSubs.unregisterSetView != tracked {
		t.Fatal("unregister-set should receive the acquired snapshot view")
	}
	if gotResult.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("UnregisterSet result should carry non-zero TotalHostExecutionDurationMicros")
	}
}

func TestDispatchRegisterSubscriptionSetClosesSnapshotOnError(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{
		registerSetErr: errors.New("synthetic"),
	}
	exec.subs = fakeSubs

	var tracked *trackingSnapshot
	exec.snapshotFn = func() store.CommittedReadView {
		tracked = &trackingSnapshot{CommittedReadView: exec.committed.Snapshot()}
		return tracked
	}

	var (
		gotResult subscription.SubscriptionSetRegisterResult
		gotErr    error
		called    bool
	)
	exec.dispatch(RegisterSubscriptionSetCmd{
		Request: subscription.SubscriptionSetRegisterRequest{
			ConnID:     types.ConnectionID{9},
			QueryID:    42,
			Predicates: []subscription.Predicate{subscription.AllRows{Table: 1}},
		},
		Reply: func(r subscription.SubscriptionSetRegisterResult, err error) {
			called = true
			gotResult = r
			gotErr = err
		},
	})
	if !called {
		t.Fatal("Reply was not invoked on error path")
	}
	// On error, handler hands a zero-value result to Reply together
	// with the underlying error.
	if gotErr == nil || gotErr.Error() != "synthetic" {
		t.Fatalf("Reply err = %v, want synthetic", gotErr)
	}
	if gotResult.QueryID != 0 {
		t.Fatalf("error response should be zero-value, got %+v", gotResult)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("snapshot should be closed on error path")
	}
}

func TestDispatchUnregisterSubscriptionSetCarriesError(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{
		unregisterSetErr: errors.New("synthetic"),
	}
	exec.subs = fakeSubs

	var tracked *trackingSnapshot
	exec.snapshotFn = func() store.CommittedReadView {
		tracked = &trackingSnapshot{CommittedReadView: exec.committed.Snapshot()}
		return tracked
	}

	var (
		gotErr error
		called bool
	)
	exec.dispatch(UnregisterSubscriptionSetCmd{
		ConnID:  types.ConnectionID{9},
		QueryID: 42,
		Reply: func(_ subscription.SubscriptionSetUnregisterResult, err error) {
			called = true
			gotErr = err
		},
	})
	if !called {
		t.Fatal("Reply was not invoked on error path")
	}
	if gotErr == nil || gotErr.Error() != "synthetic" {
		t.Fatalf("Reply err = %v, want synthetic", gotErr)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("snapshot should be closed even on error path")
	}
}
