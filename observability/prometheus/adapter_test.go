package prometheus

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	shunter "github.com/ponchione/shunter"
	client "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

var errInjectedRegisterFailure = errors.New("injected register failure")

var expectedMetricFamilies = []shunter.MetricName{
	shunter.MetricRuntimeReady,
	shunter.MetricRuntimeState,
	shunter.MetricRuntimeDegraded,
	shunter.MetricRuntimeErrorsTotal,
	shunter.MetricProtocolConnections,
	shunter.MetricProtocolConnectionsTotal,
	shunter.MetricProtocolMessagesTotal,
	shunter.MetricProtocolBackpressureTotal,
	shunter.MetricExecutorCommandsTotal,
	shunter.MetricExecutorCommandDurationSeconds,
	shunter.MetricExecutorInboxDepth,
	shunter.MetricExecutorFatal,
	shunter.MetricReducerCallsTotal,
	shunter.MetricReducerDurationSeconds,
	shunter.MetricDurabilityDurableTxID,
	shunter.MetricDurabilityQueueDepth,
	shunter.MetricDurabilityFailuresTotal,
	shunter.MetricSnapshotDurationSeconds,
	shunter.MetricSubscriptionActive,
	shunter.MetricSubscriptionEvalDurationSeconds,
	shunter.MetricSubscriptionFanoutErrorsTotal,
	shunter.MetricSubscriptionDroppedClientsTotal,
	shunter.MetricRecoveryRunsTotal,
	shunter.MetricRecoveryRecoveredTxID,
	shunter.MetricRecoveryDamagedTailSegments,
	shunter.MetricRecoverySkippedSnapshotsTotal,
	shunter.MetricRecoveryReplayDurationSeconds,
	shunter.MetricStoreCommitDurationSeconds,
	shunter.MetricStoreReadRowsTotal,
}

var expectedMetricTypes = map[shunter.MetricName]string{
	shunter.MetricRuntimeReady:                    "GAUGE",
	shunter.MetricRuntimeState:                    "GAUGE",
	shunter.MetricRuntimeDegraded:                 "GAUGE",
	shunter.MetricRuntimeErrorsTotal:              "COUNTER",
	shunter.MetricProtocolConnections:             "GAUGE",
	shunter.MetricProtocolConnectionsTotal:        "COUNTER",
	shunter.MetricProtocolMessagesTotal:           "COUNTER",
	shunter.MetricProtocolBackpressureTotal:       "COUNTER",
	shunter.MetricExecutorCommandsTotal:           "COUNTER",
	shunter.MetricExecutorCommandDurationSeconds:  "HISTOGRAM",
	shunter.MetricExecutorInboxDepth:              "GAUGE",
	shunter.MetricExecutorFatal:                   "GAUGE",
	shunter.MetricReducerCallsTotal:               "COUNTER",
	shunter.MetricReducerDurationSeconds:          "HISTOGRAM",
	shunter.MetricDurabilityDurableTxID:           "GAUGE",
	shunter.MetricDurabilityQueueDepth:            "GAUGE",
	shunter.MetricDurabilityFailuresTotal:         "COUNTER",
	shunter.MetricSnapshotDurationSeconds:         "HISTOGRAM",
	shunter.MetricSubscriptionActive:              "GAUGE",
	shunter.MetricSubscriptionEvalDurationSeconds: "HISTOGRAM",
	shunter.MetricSubscriptionFanoutErrorsTotal:   "COUNTER",
	shunter.MetricSubscriptionDroppedClientsTotal: "COUNTER",
	shunter.MetricRecoveryRunsTotal:               "COUNTER",
	shunter.MetricRecoveryRecoveredTxID:           "GAUGE",
	shunter.MetricRecoveryDamagedTailSegments:     "GAUGE",
	shunter.MetricRecoverySkippedSnapshotsTotal:   "COUNTER",
	shunter.MetricRecoveryReplayDurationSeconds:   "HISTOGRAM",
	shunter.MetricStoreCommitDurationSeconds:      "HISTOGRAM",
	shunter.MetricStoreReadRowsTotal:              "COUNTER",
}

var expectedMetricLabels = map[shunter.MetricName][]string{
	shunter.MetricRuntimeReady:                    {"module", "runtime"},
	shunter.MetricRuntimeState:                    {"module", "runtime", "state"},
	shunter.MetricRuntimeDegraded:                 {"module", "runtime"},
	shunter.MetricRuntimeErrorsTotal:              {"module", "runtime", "component", "reason"},
	shunter.MetricProtocolConnections:             {"module", "runtime"},
	shunter.MetricProtocolConnectionsTotal:        {"module", "runtime", "result"},
	shunter.MetricProtocolMessagesTotal:           {"module", "runtime", "kind", "result"},
	shunter.MetricProtocolBackpressureTotal:       {"module", "runtime", "direction"},
	shunter.MetricExecutorCommandsTotal:           {"module", "runtime", "kind", "result"},
	shunter.MetricExecutorCommandDurationSeconds:  {"module", "runtime", "kind", "result"},
	shunter.MetricExecutorInboxDepth:              {"module", "runtime"},
	shunter.MetricExecutorFatal:                   {"module", "runtime"},
	shunter.MetricReducerCallsTotal:               {"module", "runtime", "reducer", "result"},
	shunter.MetricReducerDurationSeconds:          {"module", "runtime", "reducer", "result"},
	shunter.MetricDurabilityDurableTxID:           {"module", "runtime"},
	shunter.MetricDurabilityQueueDepth:            {"module", "runtime"},
	shunter.MetricDurabilityFailuresTotal:         {"module", "runtime", "reason"},
	shunter.MetricSnapshotDurationSeconds:         {"module", "runtime", "result"},
	shunter.MetricSubscriptionActive:              {"module", "runtime"},
	shunter.MetricSubscriptionEvalDurationSeconds: {"module", "runtime", "result"},
	shunter.MetricSubscriptionFanoutErrorsTotal:   {"module", "runtime", "reason"},
	shunter.MetricSubscriptionDroppedClientsTotal: {"module", "runtime", "reason"},
	shunter.MetricRecoveryRunsTotal:               {"module", "runtime", "result"},
	shunter.MetricRecoveryRecoveredTxID:           {"module", "runtime"},
	shunter.MetricRecoveryDamagedTailSegments:     {"module", "runtime"},
	shunter.MetricRecoverySkippedSnapshotsTotal:   {"module", "runtime", "reason"},
	shunter.MetricRecoveryReplayDurationSeconds:   {"module", "runtime", "result"},
	shunter.MetricStoreCommitDurationSeconds:      {"module", "runtime", "result"},
	shunter.MetricStoreReadRowsTotal:              {"module", "runtime", "kind"},
}

var specDurationBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
	0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

func TestNilRegistererUsesPrivateRegistryAndDoesNotPolluteGlobal(t *testing.T) {
	adapter, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter.Recorder().SetGauge(shunter.MetricRuntimeReady, shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
	}, 1)

	privateFamilies := gatherFamilies(t, adapter.gatherer)
	if _, ok := privateFamilies[publicFamilyName("shunter", shunter.MetricRuntimeReady)]; !ok {
		t.Fatalf("private gatherer missing runtime_ready family: %v", sortedFamilyNames(privateFamilies))
	}

	globalFamilies := gatherFamilies(t, client.DefaultGatherer)
	for _, family := range expectedMetricFamilies {
		name := publicFamilyName("shunter", family)
		if _, ok := globalFamilies[name]; ok {
			t.Fatalf("nil registerer polluted default gatherer with %s", name)
		}
	}
}

func TestCustomRegistryRegistersAllFamiliesWithDefaultNamespace(t *testing.T) {
	registry := client.NewRegistry()
	adapter, err := New(Config{Registerer: registry})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordAllFamilies(adapter.Recorder())

	families := gatherFamilies(t, registry)
	requireExactFamilySet(t, families, "shunter")
}

func TestNonNilRegistererWithoutUsableGathererIsRejected(t *testing.T) {
	registerer := &registerOnly{}

	adapter, err := New(Config{Registerer: registerer})
	if err == nil {
		t.Fatalf("New() error = nil, want gatherer rejection with adapter %#v", adapter)
	}
	if len(registerer.collectors) != 0 {
		t.Fatalf("registered collectors before rejecting gatherer: %d", len(registerer.collectors))
	}
}

func TestGathererWithoutRegistererIsRejected(t *testing.T) {
	adapter, err := New(Config{Gatherer: client.NewRegistry()})
	if err == nil {
		t.Fatalf("New() error = nil, want gatherer-only rejection with adapter %#v", adapter)
	}
}

func TestRegisterFailureUnregistersPreviouslyRegisteredCollectors(t *testing.T) {
	registerer := &failingRegisterer{failAfter: 3}

	adapter, err := New(Config{
		Registerer: registerer,
		Gatherer:   client.NewRegistry(),
	})
	if err == nil {
		t.Fatalf("New() error = nil, want injected register failure with adapter %#v", adapter)
	}
	if !errors.Is(err, errInjectedRegisterFailure) {
		t.Fatalf("New() error = %v, want wrapped injected failure", err)
	}
	if len(registerer.registered) != registerer.failAfter {
		t.Fatalf("registered collectors = %d, want %d", len(registerer.registered), registerer.failAfter)
	}
	if !reflect.DeepEqual(registerer.unregistered, registerer.registered) {
		t.Fatalf("unregistered collectors = %#v, want previous registered collectors %#v", registerer.unregistered, registerer.registered)
	}
	if !strings.Contains(err.Error(), string(expectedMetricFamilies[registerer.failAfter])) {
		t.Fatalf("New() error = %v, want failing metric family context %s", err, expectedMetricFamilies[registerer.failAfter])
	}
}

func TestCustomNamespacePrefixesEveryFamily(t *testing.T) {
	registry := client.NewRegistry()
	adapter, err := New(Config{
		Namespace:  "app",
		Registerer: registry,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordAllFamilies(adapter.Recorder())

	families := gatherFamilies(t, registry)
	requireExactFamilySet(t, families, "app")
	for name := range families {
		if strings.HasPrefix(name, "shunter_") {
			t.Fatalf("custom namespace emitted default family %s", name)
		}
	}
}

func TestInvalidNamespaceIsRejected(t *testing.T) {
	for _, namespace := range []string{"9bad", "bad-name", "bad name"} {
		t.Run(namespace, func(t *testing.T) {
			adapter, err := New(Config{Namespace: namespace})
			if err == nil {
				t.Fatalf("New() error = nil, want invalid namespace rejection with adapter %#v", adapter)
			}
		})
	}
}

func TestInvalidConstLabelNamesAreRejected(t *testing.T) {
	adapter, err := New(Config{ConstLabels: client.Labels{"bad-name": "x"}})
	if err == nil {
		t.Fatalf("New() error = nil, want invalid const label rejection with adapter %#v", adapter)
	}
}

func TestConstLabelsDuplicatingReservedShunterLabelsAreRejected(t *testing.T) {
	for _, label := range []string{"module", "runtime", "component", "kind", "state", "result", "reason", "direction", "reducer"} {
		t.Run(label, func(t *testing.T) {
			adapter, err := New(Config{ConstLabels: client.Labels{label: "x"}})
			if err == nil {
				t.Fatalf("New() error = nil, want reserved label rejection with adapter %#v", adapter)
			}
		})
	}
}

func TestConstLabelsAreCopiedAtConstruction(t *testing.T) {
	registry := client.NewRegistry()
	constLabels := client.Labels{"env": "test"}
	adapter, err := New(Config{
		Registerer:  registry,
		ConstLabels: constLabels,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	constLabels["env"] = "prod"

	adapter.Recorder().SetGauge(shunter.MetricRuntimeReady, shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
	}, 1)

	family := gatherFamilies(t, registry)[publicFamilyName("shunter", shunter.MetricRuntimeReady)]
	metric := requireMetricWithLabels(t, family, map[string]string{
		"env":     "test",
		"module":  "chat",
		"runtime": "rt-a",
	})
	if got := metric.GetGauge().GetValue(); got != 1 {
		t.Fatalf("runtime_ready gauge = %v, want 1", got)
	}
}

func TestDurationHistogramsExposeExactSpecBucketBoundaries(t *testing.T) {
	registry := client.NewRegistry()
	adapter, err := New(Config{Registerer: registry})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := adapter.Recorder()
	recorder.ObserveHistogram(shunter.MetricExecutorCommandDurationSeconds, sampleLabelsFor(shunter.MetricExecutorCommandDurationSeconds), 0.003)
	recorder.ObserveHistogram(shunter.MetricReducerDurationSeconds, sampleLabelsFor(shunter.MetricReducerDurationSeconds), 0.003)
	recorder.ObserveHistogram(shunter.MetricSnapshotDurationSeconds, sampleLabelsFor(shunter.MetricSnapshotDurationSeconds), 0.003)
	recorder.ObserveHistogram(shunter.MetricSubscriptionEvalDurationSeconds, sampleLabelsFor(shunter.MetricSubscriptionEvalDurationSeconds), 0.003)
	recorder.ObserveHistogram(shunter.MetricRecoveryReplayDurationSeconds, sampleLabelsFor(shunter.MetricRecoveryReplayDurationSeconds), 0.003)
	recorder.ObserveHistogram(shunter.MetricStoreCommitDurationSeconds, sampleLabelsFor(shunter.MetricStoreCommitDurationSeconds), 0.003)

	families := gatherFamilies(t, registry)
	for _, name := range []shunter.MetricName{
		shunter.MetricExecutorCommandDurationSeconds,
		shunter.MetricReducerDurationSeconds,
		shunter.MetricSnapshotDurationSeconds,
		shunter.MetricSubscriptionEvalDurationSeconds,
		shunter.MetricRecoveryReplayDurationSeconds,
		shunter.MetricStoreCommitDurationSeconds,
	} {
		family := families[publicFamilyName("shunter", name)]
		if family == nil {
			t.Fatalf("missing histogram family %s", name)
		}
		if got := family.GetType().String(); got != "HISTOGRAM" {
			t.Fatalf("%s type = %s, want HISTOGRAM", family.GetName(), got)
		}
		if len(family.GetMetric()) != 1 {
			t.Fatalf("%s metric count = %d, want 1", family.GetName(), len(family.GetMetric()))
		}
		var got []float64
		for _, bucket := range family.GetMetric()[0].GetHistogram().GetBucket() {
			got = append(got, bucket.GetUpperBound())
		}
		if !reflect.DeepEqual(got, specDurationBuckets) {
			t.Fatalf("%s buckets = %v, want %v", family.GetName(), got, specDurationBuckets)
		}
	}
}

func TestRecorderIgnoresUnknownMetricNames(t *testing.T) {
	adapter, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter.Recorder().AddCounter(shunter.MetricName("unknown_total"), shunter.MetricLabels{Module: "chat", Runtime: "rt-a"}, 1)
	adapter.Recorder().SetGauge(shunter.MetricName("unknown_gauge"), shunter.MetricLabels{Module: "chat", Runtime: "rt-a"}, 1)
	adapter.Recorder().ObserveHistogram(shunter.MetricName("unknown_seconds"), shunter.MetricLabels{Module: "chat", Runtime: "rt-a"}, 1)

	families := gatherFamilies(t, adapter.gatherer)
	if len(families) != 0 {
		t.Fatalf("unknown metrics produced families: %v", sortedFamilyNames(families))
	}
}

func TestRecorderWritesCountersGaugesAndHistogramsWithExpectedLabels(t *testing.T) {
	registry := client.NewRegistry()
	adapter, err := New(Config{Registerer: registry})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	adapter.Recorder().AddCounter(shunter.MetricProtocolMessagesTotal, shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
		Kind:    "call_reducer",
		Result:  "ok",
	}, 3)
	adapter.Recorder().SetGauge(shunter.MetricRuntimeReady, shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
	}, 1)
	adapter.Recorder().ObserveHistogram(shunter.MetricReducerDurationSeconds, shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
		Reducer: "send_message",
		Result:  "committed",
	}, 0.002)

	families := gatherFamilies(t, registry)
	counter := requireMetricWithLabels(t, families[publicFamilyName("shunter", shunter.MetricProtocolMessagesTotal)], map[string]string{
		"kind":    "call_reducer",
		"module":  "chat",
		"result":  "ok",
		"runtime": "rt-a",
	})
	if got := counter.GetCounter().GetValue(); got != 3 {
		t.Fatalf("protocol_messages_total = %v, want 3", got)
	}
	gauge := requireMetricWithLabels(t, families[publicFamilyName("shunter", shunter.MetricRuntimeReady)], map[string]string{
		"module":  "chat",
		"runtime": "rt-a",
	})
	if got := gauge.GetGauge().GetValue(); got != 1 {
		t.Fatalf("runtime_ready = %v, want 1", got)
	}
	histogram := requireMetricWithLabels(t, families[publicFamilyName("shunter", shunter.MetricReducerDurationSeconds)], map[string]string{
		"module":  "chat",
		"reducer": "send_message",
		"result":  "committed",
		"runtime": "rt-a",
	})
	if got := histogram.GetHistogram().GetSampleCount(); got != 1 {
		t.Fatalf("reducer_duration sample count = %d, want 1", got)
	}
}

func TestHandlerExposesGatheredMetricsInPrometheusTextFormat(t *testing.T) {
	registry := client.NewRegistry()
	adapter, err := New(Config{Registerer: registry})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	adapter.Recorder().SetGauge(shunter.MetricRuntimeReady, shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
	}, 1)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	if contentType := res.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q, want Prometheus text", contentType)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "# TYPE shunter_runtime_ready gauge") {
		t.Fatalf("metrics body missing TYPE line:\n%s", text)
	}
	if !strings.Contains(text, `shunter_runtime_ready{module="chat",runtime="rt-a"} 1`) {
		t.Fatalf("metrics body missing runtime_ready sample:\n%s", text)
	}
}

func TestRecorderGatherAndHandlerConcurrentShortSoak(t *testing.T) {
	const (
		seed             = uint64(0x91e7100d)
		workers          = 6
		iterations       = 128
		scrapers         = 3
		scrapeIterations = 64
	)

	registry := client.NewRegistry()
	adapter, err := New(Config{Registerer: registry})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	recorder := adapter.Recorder()
	recordAllFamilies(recorder)
	handler := adapter.Handler()

	start := make(chan struct{})
	failures := make(chan string, workers+scrapers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				familyIndex := (int(seed) + worker*11 + op*7) % len(expectedMetricFamilies)
				name := expectedMetricFamilies[familyIndex]
				recordMetricFamily(recorder, name, worker+op+1)
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}
	for scraper := range scrapers {
		wg.Add(1)
		go func(scraper int) {
			defer wg.Done()
			<-start
			for op := range scrapeIterations {
				if err := gatherAndScrapePrometheus(registry, handler); err != nil {
					select {
					case failures <- fmt.Sprintf("seed=%#x scraper=%d op=%d runtime_config=workers=%d/iterations=%d/scrapers=%d/scrape_iterations=%d failure=%v",
						seed, scraper, op, workers, iterations, scrapers, scrapeIterations, err):
					default:
					}
					return
				}
				if (int(seed)+scraper+op)%3 == 0 {
					runtime.Gosched()
				}
			}
		}(scraper)
	}

	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}

	families := gatherFamilies(t, registry)
	requireExactFamilySet(t, families, "shunter")
}

func recordAllFamilies(recorder shunter.MetricsRecorder) {
	for _, name := range expectedMetricFamilies {
		recordMetricFamily(recorder, name, 1)
	}
}

func recordMetricFamily(recorder shunter.MetricsRecorder, name shunter.MetricName, value int) {
	labels := sampleLabelsFor(name)
	switch expectedMetricTypes[name] {
	case "COUNTER":
		recorder.AddCounter(name, labels, 1)
	case "GAUGE":
		recorder.SetGauge(name, labels, float64(value))
	case "HISTOGRAM":
		recorder.ObserveHistogram(name, labels, 0.001+float64(value%5)*0.001)
	}
}

func gatherAndScrapePrometheus(gatherer client.Gatherer, handler http.Handler) error {
	if _, err := gatherer.Gather(); err != nil {
		return fmt.Errorf("Gather: %w", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		return fmt.Errorf("handler status=%d body=%s", res.Code, res.Body.String())
	}
	if contentType := res.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		return fmt.Errorf("handler content_type=%q want text/plain", contentType)
	}
	return nil
}

func sampleLabelsFor(name shunter.MetricName) shunter.MetricLabels {
	labels := shunter.MetricLabels{
		Module:  "chat",
		Runtime: "rt-a",
	}
	switch name {
	case shunter.MetricRuntimeState:
		labels.State = "ready"
	case shunter.MetricRuntimeErrorsTotal:
		labels.Component = "runtime"
		labels.Reason = "build_failed"
	case shunter.MetricProtocolConnectionsTotal:
		labels.Result = "accepted"
	case shunter.MetricProtocolMessagesTotal:
		labels.Kind = "call_reducer"
		labels.Result = "ok"
	case shunter.MetricProtocolBackpressureTotal:
		labels.Direction = "inbound"
	case shunter.MetricExecutorCommandsTotal, shunter.MetricExecutorCommandDurationSeconds:
		labels.Kind = "call_reducer"
		labels.Result = "ok"
	case shunter.MetricReducerCallsTotal, shunter.MetricReducerDurationSeconds:
		labels.Reducer = "send_message"
		labels.Result = "committed"
	case shunter.MetricDurabilityFailuresTotal:
		labels.Reason = "sync_failed"
	case shunter.MetricSnapshotDurationSeconds:
		labels.Result = "ok"
	case shunter.MetricSubscriptionEvalDurationSeconds:
		labels.Result = "ok"
	case shunter.MetricSubscriptionFanoutErrorsTotal:
		labels.Reason = "send_failed"
	case shunter.MetricSubscriptionDroppedClientsTotal:
		labels.Reason = "buffer_full"
	case shunter.MetricRecoveryRunsTotal:
		labels.Result = "success"
	case shunter.MetricRecoverySkippedSnapshotsTotal:
		labels.Reason = "read_failed"
	case shunter.MetricRecoveryReplayDurationSeconds:
		labels.Result = "ok"
	case shunter.MetricStoreCommitDurationSeconds:
		labels.Result = "ok"
	case shunter.MetricStoreReadRowsTotal:
		labels.Kind = "table_scan"
	}
	return labels
}

func gatherFamilies(t *testing.T, gatherer client.Gatherer) map[string]*dto.MetricFamily {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	out := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		out[family.GetName()] = family
	}
	return out
}

func requireExactFamilySet(t *testing.T, families map[string]*dto.MetricFamily, namespace string) {
	t.Helper()
	expected := make([]string, 0, len(expectedMetricFamilies))
	for _, family := range expectedMetricFamilies {
		expected = append(expected, publicFamilyName(namespace, family))
	}
	sort.Strings(expected)

	got := sortedFamilyNames(families)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("families = %v, want %v", got, expected)
	}
	for _, family := range expectedMetricFamilies {
		public := publicFamilyName(namespace, family)
		if gotType := families[public].GetType().String(); gotType != expectedMetricTypes[family] {
			t.Fatalf("%s type = %s, want %s", public, gotType, expectedMetricTypes[family])
		}
		if gotCount := len(families[public].GetMetric()); gotCount != 1 {
			t.Fatalf("%s metric count = %d, want 1", public, gotCount)
		}
		gotLabels := sortedLabelNames(families[public].GetMetric()[0])
		wantLabels := append([]string(nil), expectedMetricLabels[family]...)
		sort.Strings(wantLabels)
		if !reflect.DeepEqual(gotLabels, wantLabels) {
			t.Fatalf("%s labels = %v, want %v", public, gotLabels, wantLabels)
		}
	}
}

func requireMetricWithLabels(t *testing.T, family *dto.MetricFamily, want map[string]string) *dto.Metric {
	t.Helper()
	if family == nil {
		t.Fatalf("missing metric family for labels %v", want)
	}
	for _, metric := range family.GetMetric() {
		if reflect.DeepEqual(metricLabelMap(metric), want) {
			return metric
		}
	}
	t.Fatalf("family %s missing labels %v in %v", family.GetName(), want, family.GetMetric())
	return nil
}

func metricLabelMap(metric *dto.Metric) map[string]string {
	out := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		out[label.GetName()] = label.GetValue()
	}
	return out
}

func sortedLabelNames(metric *dto.Metric) []string {
	names := make([]string, 0, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		names = append(names, label.GetName())
	}
	sort.Strings(names)
	return names
}

func sortedFamilyNames(families map[string]*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func publicFamilyName(namespace string, name shunter.MetricName) string {
	return namespace + "_" + string(name)
}

type registerOnly struct {
	collectors []client.Collector
}

func (r *registerOnly) Register(collector client.Collector) error {
	r.collectors = append(r.collectors, collector)
	return nil
}

func (r *registerOnly) MustRegister(collectors ...client.Collector) {
	for _, collector := range collectors {
		if err := r.Register(collector); err != nil {
			panic(err)
		}
	}
}

func (r *registerOnly) Unregister(client.Collector) bool {
	return false
}

type failingRegisterer struct {
	failAfter    int
	registered   []client.Collector
	unregistered []client.Collector
}

func (r *failingRegisterer) Register(collector client.Collector) error {
	if len(r.registered) == r.failAfter {
		return errInjectedRegisterFailure
	}
	r.registered = append(r.registered, collector)
	return nil
}

func (r *failingRegisterer) MustRegister(collectors ...client.Collector) {
	for _, collector := range collectors {
		if err := r.Register(collector); err != nil {
			panic(err)
		}
	}
}

func (r *failingRegisterer) Unregister(collector client.Collector) bool {
	r.unregistered = append(r.unregistered, collector)
	return true
}
