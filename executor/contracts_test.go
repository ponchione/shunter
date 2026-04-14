package executor

import (
	"reflect"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func TestFoundationTypesMatchPhase1dContracts(t *testing.T) {
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

func TestReducerContractsMatchPhase1dSpec(t *testing.T) {
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
		DB:          nil,
	}

	if request.ReducerName != "CreatePlayer" || request.Source != CallSourceExternal {
		t.Fatalf("ReducerRequest fields incorrect: %+v", request)
	}
	if response.Status != StatusCommitted || string(response.ReturnBSATN) != "reply" || response.TxID != 7 {
		t.Fatalf("ReducerResponse fields incorrect: %+v", response)
	}
	_ = ctx
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
	if !scheduler.Cancel(2) {
		t.Fatal("Cancel() should return true")
	}
}

type stubScheduler struct{}

func (stubScheduler) Schedule(string, []byte, time.Time) (ScheduleID, error) { return 1, nil }
func (stubScheduler) ScheduleRepeat(string, []byte, time.Duration) (ScheduleID, error) {
	return 2, nil
}
func (stubScheduler) Cancel(ScheduleID) bool { return true }
