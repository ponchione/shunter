package commitlog

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type durabilityMetricObserver struct {
	mu        sync.Mutex
	depths    []int
	durable   []types.TxID
	failures  []string
	loggedErr []error
	snapshots []string
}

func (o *durabilityMetricObserver) LogDurabilityFailed(err error, reason string, txID types.TxID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.loggedErr = append(o.loggedErr, err)
	o.failures = append(o.failures, reason)
}

func (o *durabilityMetricObserver) RecordDurabilityQueueDepth(depth int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.depths = append(o.depths, depth)
}

func (o *durabilityMetricObserver) RecordDurabilityDurableTxID(txID types.TxID) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.durable = append(o.durable, txID)
}

func (o *durabilityMetricObserver) RecordSnapshotDuration(result string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.snapshots = append(o.snapshots, result)
}

func TestDurabilityMetricsQueueDepthAndDurableTxGauges(t *testing.T) {
	observer := &durabilityMetricObserver{}
	dw := &DurabilityWorker{
		ch:       make(chan durabilityItem, 1),
		closeCh:  make(chan struct{}),
		done:     make(chan struct{}),
		waiters:  make(map[uint64][]chan types.TxID),
		observer: observer,
	}
	dw.EnqueueCommitted(1, &store.Changeset{})
	observer.requireDepth(t, 1)

	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.Observer = observer
	realWorker, err := NewDurabilityWorker(dir, 1, opts)
	if err != nil {
		t.Fatalf("NewDurabilityWorker: %v", err)
	}
	realWorker.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	select {
	case <-realWorker.WaitUntilDurable(1):
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for durable tx")
	}
	observer.requireDepth(t, 0)
	observer.requireDurable(t, 1)
	if _, err := realWorker.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDurabilityMetricsFatalFailureMappedOnce(t *testing.T) {
	observer := &durabilityMetricObserver{}
	opts := DefaultCommitLogOptions()
	opts.Observer = observer
	opts.FsyncMode = FsyncMode(99)
	_, err := NewDurabilityWorker(t.TempDir(), 1, opts)
	if err == nil {
		t.Fatal("NewDurabilityWorker unexpectedly succeeded")
	}
	if !errors.Is(err, ErrUnknownFsyncMode) {
		t.Fatalf("error = %v, want ErrUnknownFsyncMode", err)
	}
	observer.requireFailure(t, "open_failed", 1)
}

func TestSnapshotMetricsRecordsCreateDurationResult(t *testing.T) {
	observer := &durabilityMetricObserver{}
	root := t.TempDir()
	committed, reg := buildSnapshotCommittedState(t)
	committed.SetCommittedTxID(1)

	writer := NewSnapshotWriterWithObserver(root, reg, observer)
	if err := writer.CreateSnapshot(committed, 1); err != nil {
		t.Fatalf("CreateSnapshot success path: %v", err)
	}
	observer.requireSnapshot(t, "ok", 1)

	if err := writer.CreateSnapshot(committed, 2); err == nil {
		t.Fatal("CreateSnapshot mismatch succeeded, want error")
	}
	observer.requireSnapshot(t, "error", 1)
}

func (o *durabilityMetricObserver) requireDepth(t *testing.T, want int) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.depths {
		if got == want {
			return
		}
	}
	t.Fatalf("missing queue depth %d in %v", want, o.depths)
}

func (o *durabilityMetricObserver) requireDurable(t *testing.T, want types.TxID) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, got := range o.durable {
		if got == want {
			return
		}
	}
	t.Fatalf("missing durable tx %d in %v", want, o.durable)
}

func (o *durabilityMetricObserver) requireFailure(t *testing.T, reason string, count int) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	var got int
	for _, failure := range o.failures {
		if failure == reason {
			got++
		}
	}
	if got != count {
		t.Fatalf("failure reason %q count = %d, want %d in %v", reason, got, count, o.failures)
	}
}

func (o *durabilityMetricObserver) requireSnapshot(t *testing.T, result string, count int) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	var got int
	for _, snapshot := range o.snapshots {
		if snapshot == result {
			got++
		}
	}
	if got != count {
		t.Fatalf("snapshot result %q count = %d, want %d in %v", result, got, count, o.snapshots)
	}
}
