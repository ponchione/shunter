package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestCreateSnapshotDurabilityFailurePropagatesWithoutCaptureOrFalseSuccess(t *testing.T) {
	h := newPipelineHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.exec.Run(ctx)

	if response := submit(t, h.exec, "InsertPlayer"); response.Status != StatusCommitted || response.TxID != 1 {
		t.Fatalf("seed reducer response = %+v, want committed tx 1", response)
	}

	// A durability worker can stop after an ordinary commit is visible but
	// before a later maintenance snapshot reaches its required durable barrier.
	stopped := make(chan types.TxID)
	waitCalled := make(chan types.TxID, 1)
	durabilityErr := errors.New("injected snapshot fsync failure")
	h.dur.waitCh = stopped
	h.dur.waitCalled = waitCalled

	captured := false
	responseCh := make(chan CreateSnapshotResult, 1)
	if err := h.exec.Submit(CreateSnapshotCmd{
		Capture: func(*store.CommittedState, types.TxID) error {
			captured = true
			return nil
		},
		ResponseCh: responseCh,
	}); err != nil {
		t.Fatalf("Submit snapshot: %v", err)
	}
	if txID := <-waitCalled; txID != 1 {
		t.Fatalf("snapshot durability wait tx = %d, want 1", txID)
	}
	// Publishing the fatal cause before closing the readiness channel models
	// the durability worker recording its I/O failure before it stops waiters.
	h.dur.fatalErr = durabilityErr
	close(stopped)

	result := <-responseCh
	if result.TxID != 0 {
		t.Fatalf("failed snapshot TxID = %d, want zero (no false success horizon)", result.TxID)
	}
	if !errors.Is(result.Err, ErrExecutorFatal) || !errors.Is(result.Err, durabilityErr) {
		t.Fatalf("failed snapshot error = %v, want ErrExecutorFatal wrapping injected durability failure", result.Err)
	}
	if captured {
		t.Fatal("snapshot capture ran after durability barrier failed")
	}
	if err := h.exec.Submit(OnConnectCmd{}); !errors.Is(err, ErrExecutorFatal) {
		t.Fatalf("Submit after snapshot durability failure = %v, want ErrExecutorFatal", err)
	}
}
