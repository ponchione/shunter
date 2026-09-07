package shunter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type snapshotBarrierMetricsRecorder struct {
	countingMetricsRecorder
	armed   atomic.Bool
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (o *snapshotBarrierMetricsRecorder) ObserveHistogram(name MetricName, _ MetricLabels, _ float64) {
	if name == MetricStoreCommitDurationSeconds && o.armed.Load() {
		o.once.Do(func() {
			close(o.entered)
			<-o.release
		})
	}
}

func TestRuntimeCreateSnapshotWritesCommittedHorizon(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.state.SetCommittedTxID(7)

	txID, err := rt.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot returned error: %v", err)
	}
	if txID != 7 {
		t.Fatalf("CreateSnapshot txID = %d, want 7", txID)
	}

	snapshots, err := commitlog.ListSnapshots(rt.dataDir)
	if err != nil {
		t.Fatalf("ListSnapshots returned error: %v", err)
	}
	if !runtimeSnapshotListed(snapshots, txID) {
		t.Fatalf("snapshots = %v, want tx %d", snapshots, txID)
	}
}

func TestRuntimeCreateSnapshotNilRuntimeReturnsNotReady(t *testing.T) {
	var rt *Runtime

	_, err := rt.CreateSnapshot()
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("CreateSnapshot nil runtime error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestRuntimeCompactCommitLogNilRuntimeReturnsNotReady(t *testing.T) {
	var rt *Runtime

	err := rt.CompactCommitLog(1)
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("CompactCommitLog nil runtime error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestRuntimeCreateSnapshotFaultKeepsRuntimeUsable(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.state.SetCommittedTxID(7)
	snapshotPath := filepath.Join(rt.dataDir, "7")
	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o755); err != nil {
		t.Fatalf("create snapshot parent: %v", err)
	}
	if err := os.WriteFile(snapshotPath, []byte("not a snapshot directory"), 0o644); err != nil {
		t.Fatalf("create snapshot obstruction: %v", err)
	}

	_, err := rt.CreateSnapshot()
	if !errors.Is(err, commitlog.ErrSnapshot) {
		t.Fatalf("CreateSnapshot fault error = %v, want ErrSnapshot category", err)
	}
	var completionErr *commitlog.SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("CreateSnapshot fault error = %v, want SnapshotCompletionError", err)
	}
	if completionErr.Phase != "mkdir" || completionErr.Path != snapshotPath {
		t.Fatalf("snapshot completion error = %+v, want mkdir on obstruction", completionErr)
	}
	runtimeAssertFileExists(t, snapshotPath)

	if err := os.Remove(snapshotPath); err != nil {
		t.Fatalf("remove snapshot obstruction: %v", err)
	}
	txID, err := rt.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot after clearing fault returned error: %v", err)
	}
	if txID != 7 {
		t.Fatalf("CreateSnapshot txID after clearing fault = %d, want 7", txID)
	}
}

func TestRuntimeCreateSnapshotSerializesCommitDurabilityAndRecovery(t *testing.T) {
	dir := t.TempDir()
	observer := &snapshotBarrierMetricsRecorder{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	release := sync.OnceFunc(func() { close(observer.release) })
	rt, err := Build(dataDirBackupTestModule(), Config{
		DataDir:       dir,
		Observability: ObservabilityConfig{Metrics: MetricsConfig{Enabled: true, Recorder: observer}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	t.Cleanup(release)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	first, err := rt.CallReducer(context.Background(), "insert_message", []byte("first"))
	if err != nil || first.Status != StatusCommitted {
		t.Fatalf("first reducer = %+v, %v; want committed", first, err)
	}
	if err := rt.WaitUntilDurable(context.Background(), first.TxID); err != nil {
		t.Fatalf("WaitUntilDurable(%d): %v", first.TxID, err)
	}

	observer.armed.Store(true)
	type reducerCallResult struct {
		result ReducerResult
		err    error
	}
	reducerDone := make(chan reducerCallResult, 1)
	go func() {
		result, err := rt.CallReducer(context.Background(), "insert_message", []byte("second"))
		reducerDone <- reducerCallResult{result: result, err: err}
	}()
	select {
	case <-observer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for commit duration observation")
	}
	if got := rt.state.CommittedTxID(); got != first.TxID+1 {
		t.Fatalf("committed horizon while observation paused = %d, want %d", got, first.TxID+1)
	}

	type snapshotResult struct {
		txID types.TxID
		err  error
	}
	snapshotDone := make(chan snapshotResult, 1)
	go func() {
		txID, err := rt.CreateSnapshot()
		snapshotDone <- snapshotResult{txID: txID, err: err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for rt.executor.InboxDepth() == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if rt.executor.InboxDepth() == 0 {
		t.Fatal("snapshot maintenance command was not queued behind paused commit")
	}
	select {
	case result := <-snapshotDone:
		t.Fatalf("snapshot completed before commit durability enqueue: %+v", result)
	default:
	}

	release()
	second := <-reducerDone
	if second.err != nil || second.result.Status != StatusCommitted {
		t.Fatalf("second reducer = %+v, %v; want committed", second.result, second.err)
	}
	snapshot := <-snapshotDone
	if snapshot.err != nil {
		t.Fatalf("CreateSnapshot: %v", snapshot.err)
	}
	if snapshot.txID != second.result.TxID {
		t.Fatalf("snapshot txID = %d, want committed tx %d", snapshot.txID, second.result.TxID)
	}
	if durable := types.TxID(rt.durability.DurableTxID()); durable < snapshot.txID {
		t.Fatalf("snapshot txID = %d published ahead of durable horizon %d", snapshot.txID, durable)
	}

	data, err := commitlog.ReadSnapshot(filepath.Join(dir, strconv.FormatUint(uint64(snapshot.txID), 10)))
	if err != nil {
		t.Fatalf("ReadSnapshot(%d): %v", snapshot.txID, err)
	}
	if len(data.Tables) == 0 || data.Tables[0].TableID != 0 || len(data.Tables[0].Rows) != 2 {
		t.Fatalf("snapshot tables = %#v, want two message rows", data.Tables)
	}
	tail, err := rt.CallReducer(context.Background(), "insert_message", []byte("tail"))
	if err != nil || tail.Status != StatusCommitted {
		t.Fatalf("tail reducer = %+v, %v; want committed", tail, err)
	}
	if err := rt.WaitUntilDurable(context.Background(), tail.TxID); err != nil {
		t.Fatalf("WaitUntilDurable(%d): %v", tail.TxID, err)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rebuilt, err := Build(dataDirBackupTestModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("Build from snapshot and log tail: %v", err)
	}
	defer rebuilt.Close()
	stateSnapshot := rebuilt.state.Snapshot()
	defer stateSnapshot.Close()
	var got []string
	for _, row := range stateSnapshot.TableScan(0) {
		got = append(got, row[1].AsString())
	}
	slices.Sort(got)
	want := []string{"first", "second", "tail"}
	if !slices.Equal(got, want) {
		t.Fatalf("rebuilt message bodies = %#v, want %#v", got, want)
	}
}

func TestRuntimeSnapshotPublicationAllowsCommitsAndDrainsOnClose(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run("running="+strconv.FormatBool(running)+"/fail="+strconv.FormatBool(fail), func(t *testing.T) {
				dir := t.TempDir()
				rt, err := Build(dataDirBackupTestModule(), Config{DataDir: dir})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = rt.Close() })
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				horizon := types.TxID(7)
				if running {
					if err := rt.Start(ctx); err != nil {
						t.Fatal(err)
					}
					result, err := rt.CallReducer(ctx, "insert_message", []byte("captured"))
					if err != nil || result.Status != StatusCommitted {
						t.Fatalf("first reducer = %+v, %v", result, err)
					}
					horizon = result.TxID
				} else {
					rt.state.SetCommittedTxID(horizon)
				}

				entered, resume := make(chan struct{}), make(chan struct{})
				release := sync.OnceFunc(func() { close(resume) })
				original := captureRuntimeSnapshot
				var captures atomic.Int32
				secondCaptureErr := errors.New("second capture")
				captureRuntimeSnapshot = func(w *commitlog.FileSnapshotWriter, state *store.CommittedState, txID types.TxID) (func() error, error) {
					if captures.Add(1) > 1 {
						return nil, secondCaptureErr
					}
					publish, err := original(w, state, txID)
					if err != nil {
						return nil, err
					}
					return func() error {
						close(entered)
						<-resume
						return publish()
					}, nil
				}
				t.Cleanup(func() {
					release()
					_ = rt.Close()
					captureRuntimeSnapshot = original
				})
				type snapshotResult struct {
					txID types.TxID
					err  error
				}
				done := make(chan snapshotResult, 1)
				go func() {
					txID, err := rt.CreateSnapshot()
					done <- snapshotResult{txID, err}
				}()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("snapshot did not reach publication")
				}
				secondSnapshot := make(chan error, 1)
				go func() { _, err := rt.CreateSnapshot(); secondSnapshot <- err }()
				compacted := make(chan error, 1)
				go func() { compacted <- rt.CompactCommitLog(horizon) }()
				if running {
					result, err := rt.CallReducer(ctx, "insert_message", []byte("tail"))
					if err != nil || result.Status != StatusCommitted || result.TxID != horizon+1 {
						t.Fatalf("reducer during paused publication = %+v, %v", result, err)
					}
				}
				snapshotDir := filepath.Join(dir, strconv.FormatUint(uint64(horizon), 10))
				if fail {
					if err := os.WriteFile(snapshotDir, []byte("obstruction"), 0o600); err != nil {
						t.Fatal(err)
					}
				}

				closed := make(chan error, 1)
				go func() { closed <- rt.Close() }()
				if rt.closeMu.TryLock() {
					rt.closeMu.Unlock()
					t.Fatal("publication did not retain the storage maintenance lock")
				}
				select {
				case err := <-secondSnapshot:
					t.Fatalf("second snapshot returned during publication: %v", err)
				case err := <-compacted:
					t.Fatalf("compaction returned during publication: %v", err)
				case err := <-closed:
					t.Fatalf("Close returned during publication: %v", err)
				default:
				}
				if other, err := Build(dataDirBackupTestModule(), Config{DataDir: dir}); !errors.Is(err, ErrDataDirInUse) {
					if other != nil {
						_ = other.Close()
					}
					t.Fatalf("Build during publication = %v, want ErrDataDirInUse", err)
				}
				release()
				select {
				case result := <-done:
					if fail {
						if result.txID != 0 || !errors.Is(result.err, commitlog.ErrSnapshot) {
							t.Fatalf("failed publication = %+v", result)
						}
						if err := commitlog.RunCompaction(dir, horizon, rt.registry); err == nil {
							t.Fatal("compaction accepted failed publication")
						}
					} else if result.err != nil || result.txID != horizon {
						t.Fatalf("snapshot = %+v, want tx %d", result, horizon)
					}
				case <-ctx.Done():
					t.Fatal("publication did not finish")
				}
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("Close did not drain publication")
				}
				select {
				case err := <-secondSnapshot:
					if !errors.Is(err, ErrRuntimeClosed) && !errors.Is(err, secondCaptureErr) {
						t.Fatalf("second snapshot = %v", err)
					}
				case <-ctx.Done():
					t.Fatal("second snapshot did not finish")
				}
				select {
				case err := <-compacted:
					if fail && err == nil {
						t.Fatal("compaction accepted failed publication")
					}
					if !fail && err != nil && !errors.Is(err, ErrRuntimeClosed) {
						t.Fatalf("compaction = %v", err)
					}
				case <-ctx.Done():
					t.Fatal("compaction did not finish")
				}
				if !fail {
					data, err := commitlog.ReadSnapshot(snapshotDir)
					if err != nil {
						t.Fatal(err)
					}
					wantRows := 0
					if running {
						wantRows = 1
					}
					if data.TxID != horizon || len(data.Tables[0].Rows) != wantRows {
						t.Fatalf("snapshot = %+v, want captured horizon only", data)
					}
				}
				rebuilt, err := Build(dataDirBackupTestModule(), Config{DataDir: dir})
				if err != nil {
					t.Fatal(err)
				}
				defer rebuilt.Close()
				if !fail {
					report := rebuilt.recovery.report
					if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != horizon {
						t.Fatalf("recovery did not select captured snapshot: %+v", report)
					}
					if running && report.ReplayedTxRange != (commitlog.RecoveryTxIDRange{Start: horizon + 1, End: horizon + 1}) {
						t.Fatalf("recovery replay = %+v, want only the log tail", report.ReplayedTxRange)
					}
				}
				if running {
					snapshot := rebuilt.state.Snapshot()
					defer snapshot.Close()
					var rows []string
					for _, row := range snapshot.TableScan(0) {
						rows = append(rows, row[1].AsString())
					}
					slices.Sort(rows)
					if !slices.Equal(rows, []string{"captured", "tail"}) {
						t.Fatalf("recovery = %v, want captured row and log tail", rows)
					}
				}
			})
		}
	}
}

func TestRuntimeCompactCommitLogDeletesCoveredSegments(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.state.SetCommittedTxID(2)
	snapshotTxID, err := rt.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot returned error: %v", err)
	}

	covered := makeRuntimeCompactionSegment(t, rt.dataDir, 1, 1, 2)
	active := makeRuntimeCompactionSegment(t, rt.dataDir, 3, 3)

	if err := rt.CompactCommitLog(snapshotTxID); err != nil {
		t.Fatalf("CompactCommitLog returned error: %v", err)
	}
	runtimeAssertFileMissing(t, covered)
	runtimeAssertFileExists(t, active)
}

func TestRuntimeCompactCommitLogRetriesAfterCoveredSegmentDeletedBeforeSidecar(t *testing.T) {
	rt := buildValidTestRuntime(t)
	rt.state.SetCommittedTxID(3)
	snapshotTxID, err := rt.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot returned error: %v", err)
	}

	covered := makeRuntimeCompactionSegment(t, rt.dataDir, 1, 1, 2, 3)
	active := makeRuntimeCompactionSegment(t, rt.dataDir, 4, 4)
	coveredIndex := filepath.Join(rt.dataDir, commitlog.OffsetIndexFileName(1))
	activeIndex := filepath.Join(rt.dataDir, commitlog.OffsetIndexFileName(4))
	for _, path := range []string{coveredIndex, activeIndex} {
		idx, err := commitlog.CreateOffsetIndex(path, 4)
		if err != nil {
			t.Fatalf("CreateOffsetIndex(%s): %v", path, err)
		}
		if err := idx.Close(); err != nil {
			t.Fatalf("Close offset index %s: %v", path, err)
		}
	}

	if err := os.Remove(covered); err != nil {
		t.Fatalf("remove covered segment before compaction retry: %v", err)
	}
	if err := rt.CompactCommitLog(snapshotTxID); err != nil {
		t.Fatalf("CompactCommitLog retry returned error: %v", err)
	}
	runtimeAssertFileMissing(t, covered)
	runtimeAssertFileMissing(t, coveredIndex)
	runtimeAssertFileExists(t, active)
	runtimeAssertFileExists(t, activeIndex)
}

func TestRuntimeCompactCommitLogRequiresCompletedSnapshot(t *testing.T) {
	rt := buildValidTestRuntime(t)

	err := rt.CompactCommitLog(99)
	if !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("CompactCommitLog error = %v, want ErrSnapshotNotFound", err)
	}
}

func TestRuntimeStartStorageFaultBlockedFreshSegmentFailsWithoutReadiness(t *testing.T) {
	rt := buildValidTestRuntime(t)
	blockedSegment := filepath.Join(rt.dataDir, commitlog.SegmentFileName(1))
	if err := os.Mkdir(blockedSegment, 0o755); err != nil {
		t.Fatalf("create blocked segment artifact: %v", err)
	}
	leftover := filepath.Join(blockedSegment, "leftover")
	if err := os.WriteFile(leftover, []byte("unsafe artifact"), 0o644); err != nil {
		t.Fatalf("write blocked segment artifact: %v", err)
	}

	err := rt.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded with blocked fresh segment artifact")
	}
	if !strings.Contains(err.Error(), "start durability worker") ||
		!strings.Contains(err.Error(), "remove rollover segment directory artifact") {
		t.Fatalf("Start error = %v, want durability startup artifact context", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after storage-fault startup failure")
	}
	health := rt.Health()
	if health.State == RuntimeStateReady {
		t.Fatalf("runtime state = %q, want non-ready failure state", health.State)
	}
	if health.LastError == "" {
		t.Fatal("LastError not recorded after storage-fault startup failure")
	}
	if rt.durability != nil || rt.executor != nil || rt.scheduler != nil || rt.fanOutWorker != nil || rt.subscriptions != nil {
		t.Fatalf("partial resources retained after storage-fault startup failure: health=%+v", health)
	}
	if _, statErr := os.Stat(leftover); statErr != nil {
		t.Fatalf("blocked segment artifact should be preserved after failed Start: %v", statErr)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close after storage-fault startup failure: %v", err)
	}
}

func runtimeSnapshotListed(snapshots []types.TxID, want types.TxID) bool {
	for _, got := range snapshots {
		if got == want {
			return true
		}
	}
	return false
}

func makeRuntimeCompactionSegment(t *testing.T, dir string, startTx uint64, txs ...uint64) string {
	t.Helper()

	sw, err := commitlog.CreateSegment(dir, startTx)
	if err != nil {
		t.Fatalf("CreateSegment returned error: %v", err)
	}
	for _, tx := range txs {
		rec := &commitlog.Record{
			TxID:       tx,
			RecordType: commitlog.RecordTypeChangeset,
			Payload:    []byte{byte(tx)},
		}
		if err := sw.Append(rec); err != nil {
			t.Fatalf("Append(%d) returned error: %v", tx, err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("Close segment returned error: %v", err)
	}
	return filepath.Join(dir, commitlog.SegmentFileName(startTx))
}

func runtimeAssertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err=%v", filepath.Base(path), err)
	}
}

func runtimeAssertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", filepath.Base(path), err)
	}
}
