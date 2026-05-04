// Package prometheus adapts Shunter's fixed metrics model to Prometheus.
package prometheus

import (
	"fmt"
	"net/http"
	"regexp"

	shunter "github.com/ponchione/shunter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const defaultNamespace = "shunter"

var (
	namespacePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	defaultDurationBuckets = []float64{
		0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
		0.1, 0.25, 0.5, 1, 2.5, 5, 10,
	}
)

// Config configures the Prometheus adapter for Shunter metrics.
type Config struct {
	// Namespace prefixes metric family names. Empty means "shunter".
	Namespace string

	// Registerer receives collectors. Nil creates a private registry.
	Registerer prometheus.Registerer

	// Gatherer backs Handler. Nil follows the SPEC-007 registerer selection
	// rules.
	Gatherer prometheus.Gatherer

	// ConstLabels are optional low-cardinality labels applied to every family.
	ConstLabels prometheus.Labels
}

// Adapter owns the Prometheus collectors registered for a Shunter metrics
// recorder.
type Adapter struct {
	recorder *recorder
	gatherer prometheus.Gatherer
	handler  http.Handler
}

// New creates and registers a Prometheus adapter for all SPEC-007 metric
// families.
func New(cfg Config) (*Adapter, error) {
	namespace, err := normalizeNamespace(cfg.Namespace)
	if err != nil {
		return nil, err
	}
	constLabels, err := copyConstLabels(cfg.ConstLabels)
	if err != nil {
		return nil, err
	}
	registerer, gatherer, err := selectRegistererAndGatherer(cfg)
	if err != nil {
		return nil, err
	}

	rec := &recorder{
		counters:   make(map[shunter.MetricName]counterCollector),
		gauges:     make(map[shunter.MetricName]gaugeCollector),
		histograms: make(map[shunter.MetricName]histogramCollector),
	}
	registered := make([]prometheus.Collector, 0, len(metricFamilies))
	for _, family := range metricFamilies {
		collector := buildCollector(namespace, constLabels, family, rec)
		if err := registerer.Register(collector); err != nil {
			for _, previous := range registered {
				registerer.Unregister(previous)
			}
			return nil, fmt.Errorf("register prometheus metric %s: %w", family.name, err)
		}
		registered = append(registered, collector)
	}

	return &Adapter{
		recorder: rec,
		gatherer: gatherer,
		handler:  promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}),
	}, nil
}

// Recorder returns the Shunter metrics recorder backed by this adapter.
func (a *Adapter) Recorder() shunter.MetricsRecorder {
	if a == nil {
		return nil
	}
	return a.recorder
}

// Handler returns an HTTP handler exposing the configured Prometheus gatherer
// in text format.
func (a *Adapter) Handler() http.Handler {
	if a == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "prometheus adapter is nil", http.StatusServiceUnavailable)
		})
	}
	return a.handler
}

func normalizeNamespace(namespace string) (string, error) {
	if namespace == "" {
		return defaultNamespace, nil
	}
	if !namespacePattern.MatchString(namespace) {
		return "", fmt.Errorf("prometheus namespace %q is invalid", namespace)
	}
	return namespace, nil
}

func copyConstLabels(labels prometheus.Labels) (prometheus.Labels, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	out := make(prometheus.Labels, len(labels))
	for name, value := range labels {
		if !labelNamePattern.MatchString(name) {
			return nil, fmt.Errorf("prometheus const label %q is invalid", name)
		}
		if reservedShunterLabels[name] {
			return nil, fmt.Errorf("prometheus const label %q duplicates a Shunter label", name)
		}
		out[name] = value
	}
	return out, nil
}

func selectRegistererAndGatherer(cfg Config) (prometheus.Registerer, prometheus.Gatherer, error) {
	if cfg.Registerer == nil {
		if cfg.Gatherer != nil {
			return nil, nil, fmt.Errorf("prometheus registerer is required when gatherer is supplied")
		}
		registry := prometheus.NewRegistry()
		return registry, registry, nil
	}
	if cfg.Gatherer != nil {
		return cfg.Registerer, cfg.Gatherer, nil
	}
	gatherer, ok := cfg.Registerer.(prometheus.Gatherer)
	if !ok {
		return nil, nil, fmt.Errorf("prometheus gatherer is required when registerer does not implement prometheus.Gatherer")
	}
	return cfg.Registerer, gatherer, nil
}

func buildCollector(namespace string, constLabels prometheus.Labels, family metricFamily, rec *recorder) prometheus.Collector {
	switch family.kind {
	case metricKindCounter:
		collector := prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        string(family.name),
			Help:        family.help,
			ConstLabels: constLabels,
		}, family.labels)
		rec.counters[family.name] = counterCollector{vec: collector, labels: family.labels}
		return collector
	case metricKindGauge:
		collector := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        string(family.name),
			Help:        family.help,
			ConstLabels: constLabels,
		}, family.labels)
		rec.gauges[family.name] = gaugeCollector{vec: collector, labels: family.labels}
		return collector
	case metricKindHistogram:
		collector := prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   namespace,
			Name:        string(family.name),
			Help:        family.help,
			ConstLabels: constLabels,
			Buckets:     append([]float64(nil), defaultDurationBuckets...),
		}, family.labels)
		rec.histograms[family.name] = histogramCollector{vec: collector, labels: family.labels}
		return collector
	default:
		panic("unknown metric kind")
	}
}

type recorder struct {
	counters   map[shunter.MetricName]counterCollector
	gauges     map[shunter.MetricName]gaugeCollector
	histograms map[shunter.MetricName]histogramCollector
}

func (r *recorder) AddCounter(name shunter.MetricName, labels shunter.MetricLabels, delta uint64) {
	if r == nil {
		return
	}
	collector, ok := r.counters[name]
	if !ok {
		return
	}
	collector.vec.WithLabelValues(labelValues(labels, collector.labels)...).Add(float64(delta))
}

func (r *recorder) SetGauge(name shunter.MetricName, labels shunter.MetricLabels, value float64) {
	if r == nil {
		return
	}
	collector, ok := r.gauges[name]
	if !ok {
		return
	}
	collector.vec.WithLabelValues(labelValues(labels, collector.labels)...).Set(value)
}

func (r *recorder) ObserveHistogram(name shunter.MetricName, labels shunter.MetricLabels, value float64) {
	if r == nil {
		return
	}
	collector, ok := r.histograms[name]
	if !ok {
		return
	}
	collector.vec.WithLabelValues(labelValues(labels, collector.labels)...).Observe(value)
}

type counterCollector struct {
	vec    *prometheus.CounterVec
	labels []string
}

type gaugeCollector struct {
	vec    *prometheus.GaugeVec
	labels []string
}

type histogramCollector struct {
	vec    *prometheus.HistogramVec
	labels []string
}

func labelValues(labels shunter.MetricLabels, names []string) []string {
	values := make([]string, 0, len(names))
	for _, name := range names {
		switch name {
		case "module":
			values = append(values, labels.Module)
		case "runtime":
			values = append(values, labels.Runtime)
		case "component":
			values = append(values, labels.Component)
		case "kind":
			values = append(values, labels.Kind)
		case "state":
			values = append(values, labels.State)
		case "result":
			values = append(values, labels.Result)
		case "reason":
			values = append(values, labels.Reason)
		case "direction":
			values = append(values, labels.Direction)
		case "reducer":
			values = append(values, labels.Reducer)
		default:
			values = append(values, "")
		}
	}
	return values
}

type metricKind uint8

const (
	metricKindCounter metricKind = iota
	metricKindGauge
	metricKindHistogram
)

type metricFamily struct {
	name   shunter.MetricName
	kind   metricKind
	labels []string
	help   string
}

var reservedShunterLabels = map[string]bool{
	"module":    true,
	"runtime":   true,
	"component": true,
	"kind":      true,
	"state":     true,
	"result":    true,
	"reason":    true,
	"direction": true,
	"reducer":   true,
}

var metricFamilies = []metricFamily{
	{
		name:   shunter.MetricRuntimeReady,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "1 iff RuntimeHealth.Ready is true, else 0.",
	},
	{
		name:   shunter.MetricRuntimeState,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime", "state"},
		help:   "One-hot lifecycle state gauge.",
	},
	{
		name:   shunter.MetricRuntimeDegraded,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "1 iff RuntimeHealth.Degraded is true, else 0.",
	},
	{
		name:   shunter.MetricRuntimeErrorsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "component", "reason"},
		help:   "Runtime or subsystem errors after reason mapping.",
	},
	{
		name:   shunter.MetricProtocolConnections,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Current active protocol connections.",
	},
	{
		name:   shunter.MetricProtocolConnectionsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "result"},
		help:   "Accepted or rejected connection attempts.",
	},
	{
		name:   shunter.MetricProtocolMessagesTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "kind", "result"},
		help:   "Client messages decoded and handled by the protocol layer.",
	},
	{
		name:   shunter.MetricProtocolBackpressureTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "direction"},
		help:   "Inbound/outbound backpressure signals.",
	},
	{
		name:   shunter.MetricExecutorCommandsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "kind", "result"},
		help:   "Executor command terminal outcomes, including submit-time rejections.",
	},
	{
		name:   shunter.MetricExecutorCommandDurationSeconds,
		kind:   metricKindHistogram,
		labels: []string{"module", "runtime", "kind", "result"},
		help:   "Time from executor dequeue to terminal command response.",
	},
	{
		name:   shunter.MetricExecutorInboxDepth,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Current executor queue depth.",
	},
	{
		name:   shunter.MetricExecutorFatal,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "1 iff executor fatal state is latched.",
	},
	{
		name:   shunter.MetricReducerCallsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "reducer", "result"},
		help:   "Reducer call outcomes.",
	},
	{
		name:   shunter.MetricReducerDurationSeconds,
		kind:   metricKindHistogram,
		labels: []string{"module", "runtime", "reducer", "result"},
		help:   "Reducer handler wall-clock duration.",
	},
	{
		name:   shunter.MetricDurabilityDurableTxID,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Latest durable transaction ID.",
	},
	{
		name:   shunter.MetricDurabilityQueueDepth,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Current durability queue depth.",
	},
	{
		name:   shunter.MetricDurabilityFailuresTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "reason"},
		help:   "Fatal durability failures.",
	},
	{
		name:   shunter.MetricSnapshotDurationSeconds,
		kind:   metricKindHistogram,
		labels: []string{"module", "runtime", "result"},
		help:   "Time spent creating commitlog snapshots.",
	},
	{
		name:   shunter.MetricSubscriptionActive,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Active subscription count.",
	},
	{
		name:   shunter.MetricSubscriptionEvalDurationSeconds,
		kind:   metricKindHistogram,
		labels: []string{"module", "runtime", "result"},
		help:   "Subscription evaluation duration after a committed transaction.",
	},
	{
		name:   shunter.MetricSubscriptionFanoutErrorsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "reason"},
		help:   "Fan-out delivery failures.",
	},
	{
		name:   shunter.MetricSubscriptionDroppedClientsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "reason"},
		help:   "Client drops initiated by subscription/fan-out backpressure.",
	},
	{
		name:   shunter.MetricRecoveryRunsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "result"},
		help:   "Recovery attempts at runtime build/open.",
	},
	{
		name:   shunter.MetricRecoveryRecoveredTxID,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Recovered transaction horizon from the latest recovery.",
	},
	{
		name:   shunter.MetricRecoveryDamagedTailSegments,
		kind:   metricKindGauge,
		labels: []string{"module", "runtime"},
		help:   "Damaged tail segment count from the latest recovery.",
	},
	{
		name:   shunter.MetricRecoverySkippedSnapshotsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "reason"},
		help:   "Skipped recovery snapshots.",
	},
	{
		name:   shunter.MetricRecoveryReplayDurationSeconds,
		kind:   metricKindHistogram,
		labels: []string{"module", "runtime", "result"},
		help:   "Time spent replaying commitlog records during recovery.",
	},
	{
		name:   shunter.MetricStoreCommitDurationSeconds,
		kind:   metricKindHistogram,
		labels: []string{"module", "runtime", "result"},
		help:   "Time spent applying store commits to committed state.",
	},
	{
		name:   shunter.MetricStoreReadRowsTotal,
		kind:   metricKindCounter,
		labels: []string{"module", "runtime", "kind"},
		help:   "Rows matched or delivered by committed store read paths.",
	},
}
