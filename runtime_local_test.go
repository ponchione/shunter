package shunter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestCallReducerRequiresReadyRuntime(t *testing.T) {
	rt := buildValidTestRuntime(t)

	_, err := rt.CallReducer(context.Background(), "send_message", nil)
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("CallReducer before Start error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestCallReducerPreservesStartingState(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.mu.Lock()
	rt.stateName = RuntimeStateStarting
	rt.ready.Store(false)
	rt.mu.Unlock()

	_, err := rt.CallReducer(context.Background(), "send_message", nil)
	if !errors.Is(err, ErrRuntimeStarting) {
		t.Fatalf("CallReducer while starting error = %v, want ErrRuntimeStarting", err)
	}
}

func TestCallReducerAfterCloseReturnsRuntimeClosed(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := rt.CallReducer(context.Background(), "send_message", nil)
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("CallReducer after Close error = %v, want ErrRuntimeClosed", err)
	}
}

func TestCallReducerWithCanceledContextReturnsContextError(t *testing.T) {
	rt := buildStartedRuntimeWithReducer(t, "send_message", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer rt.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.CallReducer(ctx, "send_message", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CallReducer canceled context error = %v, want context.Canceled", err)
	}
}

func TestCallReducerInvokesReducerThroughExecutor(t *testing.T) {
	rt := buildStartedRuntimeWithReducer(t, "send_message", func(_ *schema.ReducerContext, args []byte) ([]byte, error) {
		if string(args) != "hello" {
			return nil, fmt.Errorf("bad args: %q", args)
		}
		return []byte("ok"), nil
	})
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "send_message", []byte("hello"), WithRequestID(7))
	if err != nil {
		t.Fatalf("CallReducer returned admission error: %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("status = %v, want committed; reducer err = %v", res.Status, res.Error)
	}
	if string(res.ReturnBSATN) != "ok" {
		t.Fatalf("return = %q, want ok", res.ReturnBSATN)
	}
	if res.TxID == 0 {
		t.Fatal("expected non-zero committed tx id")
	}
}

func TestCallReducerUserErrorIsResultNotAdmissionError(t *testing.T) {
	rt := buildStartedRuntimeWithReducer(t, "fail", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		return nil, errors.New("user failed")
	})
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "fail", nil)
	if err != nil {
		t.Fatalf("CallReducer admission error = %v, want nil", err)
	}
	if res.Status != StatusFailedUser {
		t.Fatalf("status = %v, want user failure", res.Status)
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "user failed") {
		t.Fatalf("result error = %v, want user failed", res.Error)
	}
}

func TestReadRejectsNilCallbackBeforeReadinessCheck(t *testing.T) {
	rt := buildValidTestRuntime(t)

	err := rt.Read(context.Background(), nil)
	if !errors.Is(err, ErrLocalReadNilCallback) {
		t.Fatalf("Read with nil callback error = %v, want ErrLocalReadNilCallback", err)
	}
}

func TestReadRequiresReadyRuntime(t *testing.T) {
	rt := buildValidTestRuntime(t)

	err := rt.Read(context.Background(), func(LocalReadView) error { return nil })
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("Read before Start error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestReadPreservesStartingAndClosedState(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.mu.Lock()
	rt.stateName = RuntimeStateStarting
	rt.ready.Store(false)
	rt.mu.Unlock()

	err := rt.Read(context.Background(), func(LocalReadView) error { return nil })
	if !errors.Is(err, ErrRuntimeStarting) {
		t.Fatalf("Read while starting error = %v, want ErrRuntimeStarting", err)
	}

	rt.mu.Lock()
	rt.stateName = RuntimeStateClosed
	rt.mu.Unlock()
	err = rt.Read(context.Background(), func(LocalReadView) error { return nil })
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("Read after close error = %v, want ErrRuntimeClosed", err)
	}
}

func TestReadExposesCommittedRowsAndClosesSnapshot(t *testing.T) {
	const messagesTableID schema.TableID = 0
	rt := buildStartedRuntimeWithReducer(t, "insert_message", func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
		id := uint64(1)
		if string(args) == "after-read" {
			id = 2
		}
		_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{types.NewUint64(id), types.NewString(string(args))})
		return nil, err
	})
	defer rt.Close()

	if res, err := rt.CallReducer(context.Background(), "insert_message", []byte("hello")); err != nil || res.Status != StatusCommitted {
		t.Fatalf("CallReducer insert result = (%v, %v), want committed", res, err)
	}

	var scannedRows int
	err := rt.Read(context.Background(), func(view LocalReadView) error {
		if got := view.RowCount(messagesTableID); got != 1 {
			return fmt.Errorf("RowCount = %d, want 1", got)
		}
		for rowID, row := range view.TableScan(messagesTableID) {
			scannedRows++
			got, ok := view.GetRow(messagesTableID, rowID)
			if !ok {
				return fmt.Errorf("GetRow(%d) not found", rowID)
			}
			if fmt.Sprint(got) != fmt.Sprint(row) {
				return fmt.Errorf("GetRow(%d) = %v, want scanned row %v", rowID, got, row)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if scannedRows != 1 {
		t.Fatalf("scanned rows = %d, want 1", scannedRows)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if res, err := rt.CallReducer(ctx, "insert_message", []byte("after-read")); err != nil || res.Status != StatusCommitted {
		t.Fatalf("CallReducer after Read result = (%v, %v), want committed; leaked snapshots can block this", res, err)
	}
}

func buildStartedRuntimeWithReducer(t *testing.T, name string, handler schema.ReducerHandler) *Runtime {
	t.Helper()
	rt, err := Build(validChatModule().Reducer(name, handler), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	return rt
}
