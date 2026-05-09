package shunter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
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

func TestCallReducerRejectsWhenDurabilityFatalLatched(t *testing.T) {
	rt := buildStartedRuntimeWithReducer(t, "send_message", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		t.Fatal("reducer should not run after durability fatal")
		return nil, nil
	})
	defer rt.Close()

	rt.mu.Lock()
	rt.durabilityFatalErr = errors.New("durability failed")
	rt.mu.Unlock()

	_, err := rt.CallReducer(context.Background(), "send_message", nil)
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("CallReducer error = %v, want ErrRuntimeNotReady", err)
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

func TestCallReducerContextCancelsWhileExecutorInboxFull(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	rt, err := Build(validChatModule().Reducer("slow", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return []byte("ok"), nil
	}), Config{DataDir: t.TempDir(), ExecutorQueueCapacity: 1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	firstResp := make(chan executor.ReducerResponse, 1)
	if err := rt.executor.Submit(executor.CallReducerCmd{
		Request:    executor.ReducerRequest{ReducerName: "slow", Source: executor.CallSourceExternal},
		ResponseCh: firstResp,
	}); err != nil {
		t.Fatalf("submit first slow reducer: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow reducer did not start")
	}
	if err := rt.executor.Submit(executor.CallReducerCmd{
		Request: executor.ReducerRequest{ReducerName: "slow", Source: executor.CallSourceExternal},
	}); err != nil {
		t.Fatalf("submit queued slow reducer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = rt.CallReducer(ctx, "slow", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallReducer with full inbox err=%v, want context deadline exceeded", err)
	}

	close(release)
	select {
	case <-firstResp:
	case <-time.After(2 * time.Second):
		t.Fatal("first slow reducer did not finish after release")
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

func TestCallReducerWithAuthPrincipal(t *testing.T) {
	want := AuthPrincipal{
		Issuer:      "issuer",
		Subject:     "alice",
		Audience:    []string{"shunter-api"},
		Permissions: []string{"principal:permission"},
	}
	var got types.AuthPrincipal
	rt := buildStartedRuntimeWithReducer(t, "inspect_principal", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
		got = ctx.Caller.Principal.Copy()
		return nil, nil
	})
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "inspect_principal", nil, WithAuthPrincipal(want))
	if err != nil {
		t.Fatalf("CallReducer admission error = %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("status = %v, want committed; reducer err = %v", res.Status, res.Error)
	}
	if got.Issuer != want.Issuer || got.Subject != want.Subject ||
		len(got.Audience) != 1 || got.Audience[0] != "shunter-api" ||
		len(got.Permissions) != 1 || got.Permissions[0] != "principal:permission" {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
}

func TestCallReducerDetachesArgsBeforeQueuedExecution(t *testing.T) {
	blockStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	seenArgs := make(chan []byte, 1)

	rt, err := Build(validChatModule().
		Reducer("block_executor", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
			close(blockStarted)
			<-releaseBlocker
			return nil, nil
		}).
		Reducer("capture_args", func(_ *schema.ReducerContext, args []byte) ([]byte, error) {
			seenArgs <- append([]byte(nil), args...)
			return nil, nil
		}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	blockDone := make(chan error, 1)
	go func() {
		res, err := rt.CallReducer(context.Background(), "block_executor", nil)
		if err != nil {
			blockDone <- err
			return
		}
		if res.Status != StatusCommitted {
			blockDone <- fmt.Errorf("block_executor status = %v, err = %v", res.Status, res.Error)
			return
		}
		blockDone <- nil
	}()

	select {
	case <-blockStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking reducer did not start")
	}

	args := []byte{0x01, 0x02, 0x03}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := rt.CallReducer(ctx, "capture_args", args); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued capture_args err = %v, want context deadline exceeded", err)
	}

	args[0] = 0xff
	close(releaseBlocker)

	select {
	case got := <-seenArgs:
		if string(got) != string([]byte{0x01, 0x02, 0x03}) {
			t.Fatalf("queued reducer args = %x, want original 010203", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capture_args reducer did not run")
	}
	if err := <-blockDone; err != nil {
		t.Fatalf("block_executor: %v", err)
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

func TestCallReducerDevLocalDefaultSatisfiesReducerPermissions(t *testing.T) {
	rt, err := Build(validChatModule().Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "send_message", nil)
	if err != nil {
		t.Fatalf("CallReducer admission error = %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("status = %v, want committed; err = %v", res.Status, res.Error)
	}
}

func TestCallReducerPermissionDeniedBeforeReducerExecution(t *testing.T) {
	called := false
	rt, err := Build(validChatModule().Reducer("send_message", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
		called = true
		_, err := ctx.DB.Insert(0, types.ProductValue{types.NewUint64(1), types.NewString("blocked")})
		return nil, err
	}, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "send_message", nil, WithPermissions("messages:read"))
	if err != nil {
		t.Fatalf("CallReducer admission error = %v", err)
	}
	if res.Status != StatusFailedPermission {
		t.Fatalf("status = %v, want permission failure; err = %v", res.Status, res.Error)
	}
	if !errors.Is(res.Error, ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", res.Error)
	}
	if called {
		t.Fatal("reducer handler ran despite missing permission")
	}

	err = rt.Read(context.Background(), func(view LocalReadView) error {
		if got := view.RowCount(0); got != 0 {
			return fmt.Errorf("row count = %d, want 0", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestCallReducerStrictLocalRequiresExplicitPermissions(t *testing.T) {
	rt, err := Build(validChatModule().Reducer("send_message", noopReducer, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})), Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte("strict-local-secret"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "send_message", nil)
	if err != nil {
		t.Fatalf("CallReducer admission error = %v", err)
	}
	if res.Status != StatusFailedPermission || !errors.Is(res.Error, ErrPermissionDenied) {
		t.Fatalf("strict default result = (%v, %v), want permission denied", res.Status, res.Error)
	}

	res, err = rt.CallReducer(context.Background(), "send_message", nil, WithAuthPrincipal(AuthPrincipal{Permissions: []string{"messages:send"}}))
	if err != nil {
		t.Fatalf("CallReducer with principal permissions admission error = %v", err)
	}
	if res.Status != StatusFailedPermission || !errors.Is(res.Error, ErrPermissionDenied) {
		t.Fatalf("strict principal-permissions result = (%v, %v), want permission denied", res.Status, res.Error)
	}

	res, err = rt.CallReducer(context.Background(), "send_message", nil, WithPermissions("messages:send"))
	if err != nil {
		t.Fatalf("CallReducer with permissions admission error = %v", err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("status with permissions = %v, want committed; err = %v", res.Status, res.Error)
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

func TestLocalIndexedReadsAndDurableWait(t *testing.T) {
	const messagesTableID schema.TableID = 0
	rt := buildStartedRuntimeWithReducer(t, "upsert_message", func(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
		for rowID := range ctx.DB.SeekIndex(uint32(messagesTableID), uint32(0), types.NewUint64(1)) {
			_, err := ctx.DB.Update(uint32(messagesTableID), rowID, types.ProductValue{types.NewUint64(1), types.NewString(string(args))})
			return nil, err
		}
		_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{types.NewUint64(1), types.NewString(string(args))})
		return nil, err
	})
	defer rt.Close()

	res, err := rt.CallReducer(context.Background(), "upsert_message", []byte("hello"))
	if err != nil || res.Status != StatusCommitted {
		t.Fatalf("CallReducer insert result = (%v, %v), want committed", res, err)
	}
	if err := rt.WaitUntilDurable(context.Background(), res.TxID); err != nil {
		t.Fatalf("WaitUntilDurable: %v", err)
	}
	res, err = rt.CallReducer(context.Background(), "upsert_message", []byte("updated"))
	if err != nil || res.Status != StatusCommitted {
		t.Fatalf("CallReducer update result = (%v, %v), want committed", res, err)
	}

	err = rt.Read(context.Background(), func(view LocalReadView) error {
		var bodies []string
		for _, row := range view.SeekIndex(messagesTableID, 0, types.NewUint64(1)) {
			bodies = append(bodies, row[1].AsString())
		}
		if len(bodies) != 1 || bodies[0] != "updated" {
			return fmt.Errorf("SeekIndex bodies = %v, want [updated]", bodies)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestWaitUntilDurableClosedWorkerDoesNotReportSuccess(t *testing.T) {
	opts := commitlog.DefaultCommitLogOptions()
	opts.OffsetIndexIntervalBytes = 0
	opts.OffsetIndexCap = 0
	dw, err := commitlog.NewDurabilityWorker(t.TempDir(), 1, opts)
	if err != nil {
		t.Fatalf("NewDurabilityWorker: %v", err)
	}
	if _, err := dw.Close(); err != nil {
		t.Fatalf("Close durability worker: %v", err)
	}

	rt := &Runtime{
		stateName:  RuntimeStateReady,
		durability: dw,
	}
	rt.ready.Store(true)

	err = rt.WaitUntilDurable(context.Background(), 1)
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("WaitUntilDurable closed worker error = %v, want ErrRuntimeNotReady", err)
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
