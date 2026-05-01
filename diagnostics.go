package shunter

import (
	"encoding/json"
	"net/http"
)

type diagnosticsStatus string

const (
	diagnosticsStatusFailed   diagnosticsStatus = "failed"
	diagnosticsStatusDegraded diagnosticsStatus = "degraded"
	diagnosticsStatusOK       diagnosticsStatus = "ok"
	diagnosticsStatusNotReady diagnosticsStatus = "not_ready"
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
		health := runtimeDiagnosticsHealth(r)
		classification := classifyRuntimeDiagnostics(health)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/healthz", classification), runtimeDiagnosticsHealthPayload{
			Status:  classification,
			Runtime: health,
		})
	}
}

func runtimeReadyzHandler(r *Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		health := runtimeDiagnosticsHealth(r)
		classification := classifyRuntimeDiagnostics(health)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/readyz", classification), runtimeDiagnosticsHealthPayload{
			Status:  classification,
			Runtime: health,
		})
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
		health := hostDiagnosticsHealth(h)
		classification := classifyHostDiagnostics(health)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/healthz", classification), hostDiagnosticsHealthPayload{
			Status: classification,
			Host:   health,
		})
	}
}

func hostReadyzHandler(h *Host) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		health := hostDiagnosticsHealth(h)
		classification := classifyHostDiagnostics(health)
		writeDiagnosticsJSON(w, req, diagnosticsHTTPStatus("/readyz", classification), hostDiagnosticsHealthPayload{
			Status: classification,
			Host:   health,
		})
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

type runtimeDiagnosticsHealthPayload struct {
	Status  diagnosticsStatus `json:"status"`
	Runtime RuntimeHealth     `json:"runtime"`
}

type hostDiagnosticsHealthPayload struct {
	Status diagnosticsStatus `json:"status"`
	Host   HostHealth        `json:"host"`
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

func classifyRuntimeDiagnostics(health RuntimeHealth) diagnosticsStatus {
	if runtimeDiagnosticsFailed(health) {
		return diagnosticsStatusFailed
	}
	if health.Degraded {
		return diagnosticsStatusDegraded
	}
	if health.Ready {
		return diagnosticsStatusOK
	}
	return diagnosticsStatusNotReady
}

func runtimeDiagnosticsFailed(health RuntimeHealth) bool {
	switch health.State {
	case RuntimeStateFailed, RuntimeStateClosing, RuntimeStateClosed:
		return true
	default:
		return health.Executor.Fatal || health.Durability.Fatal || health.Subscriptions.FanoutFatal
	}
}

func classifyHostDiagnostics(health HostHealth) diagnosticsStatus {
	if len(health.Modules) == 0 {
		return diagnosticsStatusFailed
	}
	for _, module := range health.Modules {
		if classifyRuntimeDiagnostics(module.Health) == diagnosticsStatusFailed {
			return diagnosticsStatusFailed
		}
	}
	if health.Degraded {
		return diagnosticsStatusDegraded
	}
	if health.Ready {
		return diagnosticsStatusOK
	}
	return diagnosticsStatusNotReady
}

func diagnosticsHTTPStatus(path string, classification diagnosticsStatus) int {
	if path == "/healthz" && classification != diagnosticsStatusFailed {
		return http.StatusOK
	}
	if path == "/readyz" && classification == diagnosticsStatusOK {
		return http.StatusOK
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
