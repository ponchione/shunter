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
