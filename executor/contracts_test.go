package executor

import (
	"errors"
	"iter"
	"reflect"
	"testing"
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func TestFoundationTypesMatchContracts(t *testing.T) {
	var zero types.TxID
	if zero != types.TxID(0) {
		t.Fatalf("TxID zero value = %d, want 0", zero)
	}

	callSources := map[CallSource]bool{
		CallSourceExternal:  true,
		CallSourceScheduled: true,
		CallSourceLifecycle: true,
	}
	if len(callSources) != 3 {
		t.Fatal("CallSource values must be distinct")
	}

	statuses := map[ReducerStatus]bool{
		StatusCommitted:      true,
		StatusFailedUser:     true,
		StatusFailedPanic:    true,
		StatusFailedInternal: true,
	}
	if len(statuses) != 4 {
		t.Fatal("ReducerStatus values must be distinct")
	}

	lifecycles := map[LifecycleKind]bool{
		LifecycleNone:         true,
		LifecycleOnConnect:    true,
		LifecycleOnDisconnect: true,
	}
	if len(lifecycles) != 3 {
		t.Fatal("LifecycleKind values must be distinct")
	}

	if reflect.TypeFor[SubscriptionID]().Kind() != reflect.Uint32 {
		t.Fatalf("SubscriptionID kind = %s, want uint32", reflect.TypeFor[SubscriptionID]().Kind())
	}
}

func TestReducerContractsMatchSpec(t *testing.T) {
	h := types.ReducerHandler(func(ctx *types.ReducerContext, argBSATN []byte) ([]byte, error) {
		if ctx == nil {
			t.Fatal("ReducerHandler should receive ReducerContext")
		}
		if string(argBSATN) != "args" {
			t.Fatalf("argBSATN = %q, want args", string(argBSATN))
		}
		return []byte("ok"), nil
	})

	respBytes, err := h(&types.ReducerContext{}, []byte("args"))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if string(respBytes) != "ok" {
		t.Fatalf("handler returned %q, want ok", string(respBytes))
	}

	rr := RegisteredReducer{Name: "CreatePlayer", Handler: h, Lifecycle: LifecycleOnConnect}
	if rr.Name != "CreatePlayer" || rr.Handler == nil || rr.Lifecycle != LifecycleOnConnect {
		t.Fatalf("RegisteredReducer not populated correctly: %+v", rr)
	}

	var internal types.CallerContext
	if internal.ConnectionID != (types.ConnectionID{}) {
		t.Fatal("internal callers should use zero-value ConnectionID")
	}

	now := time.Now().UTC()
	request := ReducerRequest{
		ReducerName: "CreatePlayer",
		Args:        []byte("args"),
		Caller: types.CallerContext{
			Identity:     types.Identity{1},
			ConnectionID: types.ConnectionID{2},
			Timestamp:    now,
		},
		Source: CallSourceExternal,
	}
	response := ReducerResponse{
		Status:      StatusCommitted,
		Error:       nil,
		ReturnBSATN: []byte("reply"),
		TxID:        types.TxID(7),
	}
	ctx := types.ReducerContext{
		ReducerName: request.ReducerName,
		Caller:      request.Caller,
		DB:          stubReducerDB{},
		Scheduler:   stubReducerScheduler{},
	}

	if request.ReducerName != "CreatePlayer" || request.Source != CallSourceExternal {
		t.Fatalf("ReducerRequest fields incorrect: %+v", request)
	}
	if response.Status != StatusCommitted || string(response.ReturnBSATN) != "reply" || response.TxID != 7 {
		t.Fatalf("ReducerResponse fields incorrect: %+v", response)
	}
	_, _ = ctx.DB.Insert(0, nil)
	_, _ = ctx.Scheduler.Cancel(1)
}

func TestSchedulerHandleMinimalContract(t *testing.T) {
	var scheduler SchedulerHandle = stubScheduler{}
	id, err := scheduler.Schedule("job", []byte("a"), time.Unix(10, 0))
	if err != nil || id != 1 {
		t.Fatalf("Schedule() = (%d, %v), want (1, nil)", id, err)
	}
	id, err = scheduler.ScheduleRepeat("job", []byte("a"), time.Second)
	if err != nil || id != 2 {
		t.Fatalf("ScheduleRepeat() = (%d, %v), want (2, nil)", id, err)
	}
	deleted, err := scheduler.Cancel(2)
	if err != nil {
		t.Fatalf("Cancel() error = %v, want nil", err)
	}
	if !deleted {
		t.Fatal("Cancel() should return true")
	}
}

func TestEpic1CommandTypesImplementExecutorCommand(t *testing.T) {
	var cmd ExecutorCommand

	cmd = CallReducerCmd{}
	if _, ok := cmd.(CallReducerCmd); !ok {
		t.Fatal("CallReducerCmd should satisfy ExecutorCommand")
	}

	cmd = DisconnectClientSubscriptionsCmd{}
	if _, ok := cmd.(DisconnectClientSubscriptionsCmd); !ok {
		t.Fatal("DisconnectClientSubscriptionsCmd should satisfy ExecutorCommand")
	}

	cmd = RegisterSubscriptionSetCmd{}
	if _, ok := cmd.(RegisterSubscriptionSetCmd); !ok {
		t.Fatal("RegisterSubscriptionSetCmd should satisfy ExecutorCommand")
	}

	cmd = UnregisterSubscriptionSetCmd{}
	if _, ok := cmd.(UnregisterSubscriptionSetCmd); !ok {
		t.Fatal("UnregisterSubscriptionSetCmd should satisfy ExecutorCommand")
	}
}

func TestEpic1CommandShapesMatchSpec(t *testing.T) {
	regReq := subscription.SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 2,
	}
	regCmd := RegisterSubscriptionSetCmd{
		Request: regReq,
		Reply:   func(subscription.SubscriptionSetRegisterResult, error) {},
	}
	if regCmd.Request.ConnID != regReq.ConnID || regCmd.Request.QueryID != regReq.QueryID {
		t.Fatalf("RegisterSubscriptionSetCmd.Request = %+v, want %+v", regCmd.Request, regReq)
	}
	if regCmd.Reply == nil {
		t.Fatal("RegisterSubscriptionSetCmd.Reply should be non-nil")
	}

	unregCmd := UnregisterSubscriptionSetCmd{
		ConnID:  types.ConnectionID{3},
		QueryID: 4,
		Reply:   func(subscription.SubscriptionSetUnregisterResult, error) {},
	}
	if unregCmd.ConnID != (types.ConnectionID{3}) || unregCmd.QueryID != 4 {
		t.Fatalf("UnregisterSubscriptionSetCmd = %+v", unregCmd)
	}
	if unregCmd.Reply == nil {
		t.Fatal("UnregisterSubscriptionSetCmd.Reply should be non-nil")
	}

	disconnectCmd := DisconnectClientSubscriptionsCmd{
		ConnID:     types.ConnectionID{5},
		ResponseCh: make(chan error, 1),
	}
	if disconnectCmd.ConnID != (types.ConnectionID{5}) {
		t.Fatalf("DisconnectClientSubscriptionsCmd = %+v", disconnectCmd)
	}
}

func TestEpic1InterfacesAndErrorsExist(t *testing.T) {
	var _ DurabilityHandle = stubDurability{}
	var _ SubscriptionManager = stubSubscriptionManager{}

	base := []error{
		ErrReducerNotFound,
		ErrLifecycleReducer,
		ErrExecutorBusy,
		ErrExecutorShutdown,
		ErrReducerPanic,
		ErrCommitFailed,
		ErrExecutorFatal,
		ErrExecutorUnbufferedResponseChannel,
		ErrInvalidScheduleInterval,
	}
	for _, err := range base {
		if !errors.Is(err, err) {
			t.Fatalf("errors.Is(%v, %v) = false", err, err)
		}
	}
}

type stubReducerDB struct{}

func (stubReducerDB) Insert(uint32, types.ProductValue) (types.RowID, error) { return 0, nil }
func (stubReducerDB) Delete(uint32, types.RowID) error                       { return nil }
func (stubReducerDB) Update(uint32, types.RowID, types.ProductValue) (types.RowID, error) {
	return 0, nil
}
func (stubReducerDB) GetRow(uint32, types.RowID) (types.ProductValue, bool) { return nil, false }
func (stubReducerDB) ScanTable(uint32) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {}
}
func (stubReducerDB) SeekIndex(uint32, uint32, ...types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {}
}
func (stubReducerDB) SeekIndexRange(uint32, uint32, types.IndexBound, types.IndexBound) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {}
}
func (stubReducerDB) Underlying() any { return nil }

type stubReducerScheduler struct{}

func (stubReducerScheduler) Schedule(string, []byte, time.Time) (types.ScheduleID, error) {
	return 1, nil
}
func (stubReducerScheduler) ScheduleRepeat(string, []byte, time.Duration) (types.ScheduleID, error) {
	return 2, nil
}
func (stubReducerScheduler) Cancel(types.ScheduleID) (bool, error) { return true, nil }

type stubScheduler struct{}

func (stubScheduler) Schedule(string, []byte, time.Time) (types.ScheduleID, error) { return 1, nil }
func (stubScheduler) ScheduleRepeat(string, []byte, time.Duration) (types.ScheduleID, error) {
	return 2, nil
}
func (stubScheduler) Cancel(types.ScheduleID) (bool, error) { return true, nil }

type stubDurability struct{}

func (stubDurability) EnqueueCommitted(types.TxID, *store.Changeset) {}
func (stubDurability) WaitUntilDurable(types.TxID) <-chan types.TxID { return nil }
func (stubDurability) FatalError() error                             { return nil }

type stubSubscriptionManager struct{}

func (stubSubscriptionManager) RegisterSet(subscription.SubscriptionSetRegisterRequest, store.CommittedReadView) (subscription.SubscriptionSetRegisterResult, error) {
	return subscription.SubscriptionSetRegisterResult{}, nil
}
func (stubSubscriptionManager) UnregisterSet(types.ConnectionID, uint32, store.CommittedReadView) (subscription.SubscriptionSetUnregisterResult, error) {
	return subscription.SubscriptionSetUnregisterResult{}, nil
}
func (stubSubscriptionManager) EvalAndBroadcast(types.TxID, *store.Changeset, store.CommittedReadView, subscription.PostCommitMeta) {
}
func (stubSubscriptionManager) DrainDroppedClients() []types.ConnectionID { return nil }
func (stubSubscriptionManager) DisconnectClient(types.ConnectionID) error { return nil }
