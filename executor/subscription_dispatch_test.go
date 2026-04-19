package executor

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type registerDispatchSubs struct {
	registerReq      subscription.SubscriptionRegisterRequest
	registerView     store.CommittedReadView
	registerResult   subscription.SubscriptionRegisterResult
	registerErr      error
	unregisterConnID types.ConnectionID
	unregisterSubID  types.SubscriptionID
	unregisterErr    error
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

func (f *registerDispatchSubs) Register(req subscription.SubscriptionRegisterRequest, view store.CommittedReadView) (subscription.SubscriptionRegisterResult, error) {
	f.registerReq = req
	f.registerView = view
	return f.registerResult, f.registerErr
}
func (f *registerDispatchSubs) Unregister(connID types.ConnectionID, subscriptionID types.SubscriptionID) error {
	f.unregisterConnID = connID
	f.unregisterSubID = subscriptionID
	return f.unregisterErr
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

func TestRegisterSubscriptionDispatchUsesSnapshotAndClosesIt(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{
		registerResult: subscription.SubscriptionRegisterResult{SubscriptionID: 9, InitialRows: []types.ProductValue{mkReducerRow(1, "alice")}},
	}
	exec.subs = fakeSubs

	var tracked *trackingSnapshot
	exec.snapshotFn = func() store.CommittedReadView {
		tracked = &trackingSnapshot{CommittedReadView: exec.committed.Snapshot()}
		return tracked
	}

	respCh := make(chan SubscriptionRegisterResult, 1)
	req := subscription.SubscriptionRegisterRequest{ConnID: types.ConnectionID{1}, SubscriptionID: 9}
	exec.dispatch(RegisterSubscriptionCmd{Request: req, ResponseCh: respCh})

	resp := <-respCh
	if resp.SubscriptionID != 9 {
		t.Fatalf("register response id = %d, want 9", resp.SubscriptionID)
	}
	if fakeSubs.registerReq != req {
		t.Fatalf("register request = %+v, want %+v", fakeSubs.registerReq, req)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("register snapshot should be closed")
	}
	if fakeSubs.registerView != tracked {
		t.Fatal("register should receive the acquired snapshot view")
	}
}

func TestRegisterSubscriptionDispatchClosesSnapshotOnError(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{registerErr: errors.New("boom")}
	exec.subs = fakeSubs

	var tracked *trackingSnapshot
	exec.snapshotFn = func() store.CommittedReadView {
		tracked = &trackingSnapshot{CommittedReadView: exec.committed.Snapshot()}
		return tracked
	}

	respCh := make(chan SubscriptionRegisterResult, 1)
	exec.dispatch(RegisterSubscriptionCmd{Request: subscription.SubscriptionRegisterRequest{}, ResponseCh: respCh})

	resp := <-respCh
	if resp.SubscriptionID != 0 || len(resp.InitialRows) != 0 {
		t.Fatalf("error register response should be zero, got %+v", resp)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("snapshot should close on register error")
	}
}

func TestUnregisterAndDisconnectSubscriptionDispatchDelegate(t *testing.T) {
	exec, _ := setupExecutor()
	fakeSubs := &registerDispatchSubs{}
	exec.subs = fakeSubs

	unregCh := make(chan error, 1)
	exec.dispatch(UnregisterSubscriptionCmd{
		ConnID:         types.ConnectionID{2},
		SubscriptionID: 7,
		ResponseCh:     unregCh,
	})
	if err := <-unregCh; err != nil {
		t.Fatalf("unregister error = %v", err)
	}
	if fakeSubs.unregisterConnID != (types.ConnectionID{2}) || fakeSubs.unregisterSubID != 7 {
		t.Fatalf("unregister delegation = (%v, %d)", fakeSubs.unregisterConnID, fakeSubs.unregisterSubID)
	}

	discCh := make(chan error, 1)
	exec.dispatch(DisconnectClientSubscriptionsCmd{ConnID: types.ConnectionID{3}, ResponseCh: discCh})
	if err := <-discCh; err != nil {
		t.Fatalf("disconnect error = %v", err)
	}
	if fakeSubs.disconnectConnID != (types.ConnectionID{3}) {
		t.Fatalf("disconnect delegation connID = %v, want 3", fakeSubs.disconnectConnID)
	}
}

func mkReducerRow(id uint64, name string) types.ProductValue {
	return types.ProductValue{types.NewUint64(id), types.NewString(name)}
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
	respCh := make(chan subscription.SubscriptionSetRegisterResult, 1)
	exec.dispatch(RegisterSubscriptionSetCmd{Request: req, ResponseCh: respCh})

	resp := <-respCh
	if resp.QueryID != 42 {
		t.Fatalf("resp.QueryID = %d, want 42", resp.QueryID)
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

	respCh := make(chan UnregisterSubscriptionSetResponse, 1)
	exec.dispatch(UnregisterSubscriptionSetCmd{
		ConnID:     types.ConnectionID{9},
		QueryID:    42,
		ResponseCh: respCh,
	})

	resp := <-respCh
	if resp.Err != nil {
		t.Fatalf("unexpected err: %v", resp.Err)
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

	respCh := make(chan UnregisterSubscriptionSetResponse, 1)
	exec.dispatch(UnregisterSubscriptionSetCmd{
		ConnID:     types.ConnectionID{9},
		QueryID:    42,
		ResponseCh: respCh,
	})
	resp := <-respCh
	if resp.Err == nil || resp.Err.Error() != "synthetic" {
		t.Fatalf("resp.Err = %v, want synthetic", resp.Err)
	}
	if tracked == nil || !tracked.closed {
		t.Fatal("snapshot should be closed even on error path")
	}
}
