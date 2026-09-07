package shunter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type blockingMemoryMetricsRecorder struct {
	countingMetricsRecorder
	armed   atomic.Bool
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (r *blockingMemoryMetricsRecorder) SetGauge(name MetricName, _ MetricLabels, _ float64) {
	if name == MetricStoreMemoryBytes && r.armed.Load() {
		r.once.Do(func() {
			close(r.entered)
			<-r.release
		})
	}
}

func TestRuntimeMemoryMetricsDoNotBlockCommitsAndDrainOnClose(t *testing.T) {
	metrics := &blockingMemoryMetricsRecorder{entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(metrics.release) })
	rt, err := Build(dataDirBackupTestModule(), Config{
		DataDir:       t.TempDir(),
		Observability: ObservabilityConfig{Metrics: MetricsConfig{Enabled: true, Recorder: metrics}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	t.Cleanup(release)
	metrics.armed.Store(true)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-metrics.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not start memory sampling")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := rt.CallReducer(ctx, "insert_message", []byte("while sampling"))
	if err != nil || result.Status != StatusCommitted {
		t.Fatalf("commit during memory sampling = %+v, %v", result, err)
	}
	if err := rt.WaitUntilDurable(ctx, result.TxID); err != nil {
		t.Fatalf("durability during memory sampling: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- rt.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned with memory sampling in flight: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not drain memory sampling")
	}
}

func TestStoreMemoryMetricsSampleInsertsDeletesAndCancel(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	rt, err := Build(validChatModule(), Config{
		DataDir:       t.TempDir(),
		Observability: ObservabilityConfig{Metrics: MetricsConfig{Enabled: true, Recorder: metrics}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() {
			rt.runStoreMemoryMetrics(ctx)
			close(done)
		}()
		synctest.Wait()
		initialSamples := countMetricObservations(metrics, "gauge", MetricStoreMemoryBytes)
		tx := store.NewTransaction(rt.state, rt.registry)
		rowID, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("hello")})
		if err != nil {
			t.Fatal(err)
		}
		tx.Seal()
		if _, err := store.Commit(rt.state, tx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(storeMemorySampleInterval / 2)
		synctest.Wait()
		if got := countMetricObservations(metrics, "gauge", MetricStoreMemoryBytes); got != initialSamples {
			t.Fatalf("sampled before interval: %d observations, want %d", got, initialSamples)
		}
		requireLatestGauge(t, metrics, MetricStoreMemoryBytes, MetricLabels{
			Module: "chat", Runtime: "default", Kind: store.StoreMemoryKindTableRows, Table: "messages",
		}, 0)

		time.Sleep(storeMemorySampleInterval / 2)
		synctest.Wait()
		for _, usage := range rt.state.MemoryUsage() {
			requireLatestGauge(t, metrics, MetricStoreMemoryBytes, MetricLabels{
				Module: "chat", Runtime: "default", Kind: usage.Kind, Table: usage.TableName, Index: usage.IndexName,
			}, float64(usage.Bytes))
		}
		tx = store.NewTransaction(rt.state, rt.registry)
		if err := tx.Delete(0, rowID); err != nil {
			t.Fatal(err)
		}
		tx.Seal()
		if _, err := store.Commit(rt.state, tx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(storeMemorySampleInterval)
		synctest.Wait()
		for _, usage := range rt.state.MemoryUsage() {
			requireLatestGauge(t, metrics, MetricStoreMemoryBytes, MetricLabels{
				Module: "chat", Runtime: "default", Kind: usage.Kind, Table: usage.TableName, Index: usage.IndexName,
			}, float64(usage.Bytes))
		}
		cancel()
		<-done
		finalSamples := countMetricObservations(metrics, "gauge", MetricStoreMemoryBytes)
		time.Sleep(2 * storeMemorySampleInterval)
		if got := countMetricObservations(metrics, "gauge", MetricStoreMemoryBytes); got != finalSamples {
			t.Fatalf("sampled after cancellation: %d observations, want %d", got, finalSamples)
		}
	})
}

func TestSubsystemMetricsUseExactFamiliesAndLabels(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "rt-a",
		Metrics: MetricsConfig{
			Enabled:  true,
			Recorder: metrics,
		},
	})

	obs.RecordProtocolConnections(2)
	obs.RecordProtocolMessage("call_reducer", "ok")
	obs.LogProtocolBackpressure("inbound", "buffer_full")
	obs.RecordExecutorInboxDepth(3)
	obs.RecordExecutorCommand("call_reducer", "rejected")
	obs.RecordExecutorCommandDuration("call_reducer", "ok", 2*time.Millisecond)
	obs.RecordReducerCall("send_message", "committed")
	obs.RecordReducerDuration("send_message", "committed", 3*time.Millisecond)
	obs.RecordStoreCommitDuration("ok", 5*time.Millisecond)
	obs.RecordStoreMemoryUsage([]store.MemoryUsage{
		{Kind: store.StoreMemoryKindTableRows, TableName: "players", Bytes: 128},
		{Kind: store.StoreMemoryKindIndex, TableName: "players", IndexName: "pk", Bytes: 64},
	})
	obs.RecordDurabilityQueueDepth(4)
	obs.RecordDurabilityDurableTxID(types.TxID(9))
	obs.LogDurabilityFailed(assertErr("disk"), "sync_failed", types.TxID(9))
	obs.RecordSnapshotDuration("error", 6*time.Millisecond)
	obs.RecordSubscriptionActive(5)
	obs.RecordSubscriptionEvalDuration("error", 4*time.Millisecond)
	obs.RecordSubscriptionFanoutBlockedDuration(7 * time.Millisecond)
	obs.LogSubscriptionFanoutError("send_failed", nil, assertErr("send"))
	obs.LogSubscriptionClientDropped("buffer_full", nil)

	metrics.requireGauge(t, MetricProtocolConnections, MetricLabels{Module: "chat", Runtime: "rt-a"}, 2)
	metrics.requireCounter(t, MetricProtocolMessagesTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "call_reducer", Result: "ok"}, 1)
	metrics.requireCounter(t, MetricProtocolBackpressureTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Direction: "inbound"}, 1)
	metrics.requireGauge(t, MetricExecutorInboxDepth, MetricLabels{Module: "chat", Runtime: "rt-a"}, 3)
	metrics.requireCounter(t, MetricExecutorCommandsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "call_reducer", Result: "rejected"}, 1)
	requireHistogram(t, metrics, MetricExecutorCommandDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "call_reducer", Result: "ok"})
	metrics.requireCounter(t, MetricReducerCallsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "send_message", Result: "committed"}, 1)
	requireHistogram(t, metrics, MetricReducerDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "send_message", Result: "committed"})
	requireHistogram(t, metrics, MetricStoreCommitDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Result: "ok"})
	metrics.requireGauge(t, MetricStoreMemoryBytes, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "table_rows", Table: "players"}, 128)
	metrics.requireGauge(t, MetricStoreMemoryBytes, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "index", Table: "players", Index: "pk"}, 64)
	metrics.requireGauge(t, MetricDurabilityQueueDepth, MetricLabels{Module: "chat", Runtime: "rt-a"}, 4)
	metrics.requireGauge(t, MetricDurabilityDurableTxID, MetricLabels{Module: "chat", Runtime: "rt-a"}, 9)
	metrics.requireCounter(t, MetricDurabilityFailuresTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reason: "sync_failed"}, 1)
	requireHistogram(t, metrics, MetricSnapshotDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Result: "error"})
	metrics.requireGauge(t, MetricSubscriptionActive, MetricLabels{Module: "chat", Runtime: "rt-a"}, 5)
	requireHistogram(t, metrics, MetricSubscriptionEvalDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Result: "error"})
	requireHistogram(t, metrics, MetricSubscriptionFanoutBlockedSeconds, MetricLabels{Module: "chat", Runtime: "rt-a"})
	metrics.requireCounter(t, MetricSubscriptionFanoutErrorsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reason: "send_failed"}, 1)
	metrics.requireCounter(t, MetricSubscriptionDroppedClientsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reason: "buffer_full"}, 1)
}

func TestReducerMetricsAggregateLabelModeUsesAll(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "rt-a",
		Metrics: MetricsConfig{
			Enabled:          true,
			Recorder:         metrics,
			ReducerLabelMode: ReducerLabelModeAggregate,
		},
	})

	obs.RecordReducerCall("send_message", "committed")
	obs.RecordReducerDuration("send_message", "committed", time.Millisecond)

	metrics.requireCounter(t, MetricReducerCallsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "_all", Result: "committed"}, 1)
	requireHistogram(t, metrics, MetricReducerDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "_all", Result: "committed"})
}

func requireHistogram(t *testing.T, metrics *recordingMetricsRecorder, name MetricName, labels MetricLabels) {
	t.Helper()
	for _, observation := range metrics.snapshot() {
		if observation.kind == "histogram" && observation.name == name && observation.labels == labels && observation.value > 0 {
			return
		}
	}
	t.Fatalf("missing histogram %s labels=%+v in %+v", name, labels, metrics.snapshot())
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
