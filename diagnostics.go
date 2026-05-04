package shunter

import (
	"encoding/json"
	"net/http"
)

// HealthStatus is the stable health/readiness classification used by
// in-process inspection helpers and HTTP diagnostics payloads.
type HealthStatus string

const (
	// HealthStatusFailed reports a failed, closing, closed, or fatally degraded runtime.
	HealthStatusFailed HealthStatus = "failed"
	// HealthStatusDegraded reports a running but degraded runtime or host.
	HealthStatusDegraded HealthStatus = "degraded"
	// HealthStatusOK reports a ready, non-degraded runtime or host.
	HealthStatusOK HealthStatus = "ok"
	// HealthStatusNotReady reports a nonfailed runtime or host that is not ready.
	HealthStatusNotReady HealthStatus = "not_ready"
)

type diagnosticsStatus = HealthStatus

const (
	diagnosticsStatusFailed   = HealthStatusFailed
	diagnosticsStatusDegraded = HealthStatusDegraded
	diagnosticsStatusOK       = HealthStatusOK
	diagnosticsStatusNotReady = HealthStatusNotReady
)

// RuntimeDiagnosticsHandler returns an HTTP handler for runtime diagnostics.
// It serves diagnostics regardless of the runtime's MountHTTP setting.
func RuntimeDiagnosticsHandler(r *Runtime) http.Handler {
	routes := map[string]http.Handler{
		"/healthz":               runtimeHealthzHandler(r),
		"/readyz":                runtimeReadyzHandler(r),
		"/debug/shunter/runtime": runtimeDebugHandler(r),
	}
	if metrics := runtimeMetricsHandler(r); metrics != nil {
		routes["/metrics"] = metrics
	}
	return recoverDiagnosticsPanics(exactDiagnosticsRouter(routes))
}

// HostDiagnosticsHandler returns an HTTP handler for host diagnostics.
// It never serves runtime protocol routes such as /subscribe.
func HostDiagnosticsHandler(h *Host, metrics http.Handler) http.Handler {
	routes := map[string]http.Handler{
		"/healthz":            hostHealthzHandler(h),
		"/readyz":             hostReadyzHandler(h),
		"/debug/shunter/host": hostDebugHandler(h),
	}
	if metrics != nil {
		routes["/metrics"] = metrics
	}
	return recoverDiagnosticsPanics(exactDiagnosticsRouter(routes))
}

func runtimeHealthzHandler(r *Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		inspection := InspectRuntimeHealth(r)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/healthz", inspection.Status), inspection)
	}
}

func runtimeReadyzHandler(r *Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		inspection := InspectRuntimeHealth(r)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/readyz", inspection.Status), inspection)
	}
}

func runtimeDebugHandler(r *Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		writeDiagnosticsJSON(w, req, http.StatusOK, runtimeDiagnosticsDescription(r))
	}
}

func hostHealthzHandler(h *Host) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		inspection := InspectHostHealth(h)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/healthz", inspection.Status), inspection)
	}
}

func hostReadyzHandler(h *Host) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		inspection := InspectHostHealth(h)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/readyz", inspection.Status), inspection)
	}
}

func hostDebugHandler(h *Host) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		writeDiagnosticsJSON(w, req, http.StatusOK, hostDiagnosticsDescription(h))
	}
}

// RuntimeHealthInspection is a classified runtime health snapshot.
type RuntimeHealthInspection struct {
	Status  HealthStatus  `json:"status"`
	Runtime RuntimeHealth `json:"runtime"`
}

// HostHealthInspection is a classified host health snapshot.
type HostHealthInspection struct {
	Status HealthStatus `json:"status"`
	Host   HostHealth   `json:"host"`
}

type runtimeDiagnosticsHealthPayload = RuntimeHealthInspection
type hostDiagnosticsHealthPayload = HostHealthInspection

// InspectRuntimeHealth returns runtime health with the stable diagnostics status.
func InspectRuntimeHealth(r *Runtime) RuntimeHealthInspection {
	health := runtimeDiagnosticsHealth(r)
	return RuntimeHealthInspection{
		Status:  ClassifyRuntimeHealth(health),
		Runtime: health,
	}
}

// InspectHostHealth returns host health with the stable diagnostics status.
func InspectHostHealth(h *Host) HostHealthInspection {
	health := hostDiagnosticsHealth(h)
	return HostHealthInspection{
		Status: ClassifyHostHealth(health),
		Host:   health,
	}
}

func runtimeDiagnosticsHealth(r *Runtime) RuntimeHealth {
	if r == nil {
		return runtimeNotConfiguredHealth()
	}
	return r.Health()
}

func runtimeDiagnosticsDescription(r *Runtime) RuntimeDescription {
	if r == nil {
		return RuntimeDescription{
			Module: ModuleDescription{Metadata: map[string]string{}},
			Health: runtimeNotConfiguredHealth(),
		}
	}
	return r.Describe()
}

func hostDiagnosticsHealth(h *Host) HostHealth {
	return h.Health()
}

func hostDiagnosticsDescription(h *Host) HostDescription {
	return h.Describe()
}

// ClassifyRuntimeHealth maps a runtime health snapshot to its stable status.
func ClassifyRuntimeHealth(health RuntimeHealth) HealthStatus {
	if runtimeDiagnosticsFailed(health) {
		return HealthStatusFailed
	}
	if health.Degraded {
		return HealthStatusDegraded
	}
	if health.Ready {
		return HealthStatusOK
	}
	return HealthStatusNotReady
}

func runtimeDiagnosticsFailed(health RuntimeHealth) bool {
	switch health.State {
	case RuntimeStateFailed, RuntimeStateClosing, RuntimeStateClosed:
		return true
	default:
		return health.Executor.Fatal || health.Durability.Fatal || health.Subscriptions.FanoutFatal
	}
}

// ClassifyHostHealth maps a host health snapshot to its stable status.
func ClassifyHostHealth(health HostHealth) HealthStatus {
	if len(health.Modules) == 0 {
		return HealthStatusFailed
	}
	for _, module := range health.Modules {
		if ClassifyRuntimeHealth(module.Health) == HealthStatusFailed {
			return HealthStatusFailed
		}
	}
	if health.Degraded {
		return HealthStatusDegraded
	}
	if health.Ready {
		return HealthStatusOK
	}
	return HealthStatusNotReady
}

// HealthzStatusCode maps a health status to the /healthz HTTP status code.
func HealthzStatusCode(status HealthStatus) int {
	switch status {
	case HealthStatusOK, HealthStatusDegraded, HealthStatusNotReady:
		return http.StatusOK
	default:
		return http.StatusServiceUnavailable
	}
}

// ReadyzStatusCode maps a health status to the /readyz HTTP status code.
func ReadyzStatusCode(status HealthStatus) int {
	if status == HealthStatusOK {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}

func diagnosticsHTTPStatus(path string, classification HealthStatus) int {
	if path == "/healthz" {
		return HealthzStatusCode(classification)
	}
	if path == "/readyz" {
		return ReadyzStatusCode(classification)
	}
	return http.StatusServiceUnavailable
}

func runtimeMetricsHandler(r *Runtime) http.Handler {
	if r == nil {
		return nil
	}
	return r.buildConfig.Observability.Diagnostics.MetricsHandler
}

func diagnosticsMethodAllowed(w http.ResponseWriter, req *http.Request) bool {
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func writeDiagnosticsJSON(w http.ResponseWriter, req *http.Request, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "diagnostics JSON encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}

func recoverDiagnosticsPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if recover() != nil {
				http.Error(w, "diagnostics handler failed", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, req)
	})
}

func exactDiagnosticsRouter(routes map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if handler := routes[req.URL.Path]; handler != nil {
			handler.ServeHTTP(w, req)
			return
		}
		http.NotFound(w, req)
	})
}
