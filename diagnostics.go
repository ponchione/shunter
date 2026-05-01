package shunter

import (
	"encoding/json"
	"net/http"
)

// RuntimeDiagnosticsHandler returns an HTTP handler for runtime diagnostics.
// It serves diagnostics regardless of the runtime's MountHTTP setting.
func RuntimeDiagnosticsHandler(r *Runtime) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", runtimeHealthzHandler(r))
	mux.HandleFunc("/readyz", runtimeReadyzHandler(r))
	mux.HandleFunc("/debug/shunter/runtime", runtimeDebugHandler(r))
	if metrics := runtimeMetricsHandler(r); metrics != nil {
		mux.Handle("/metrics", metrics)
	}
	return recoverDiagnosticsPanics(mux)
}

func runtimeHealthzHandler(r *Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		health := runtimeDiagnosticsHealth(r)
		status := http.StatusOK
		if runtimeDiagnosticsFailed(health.State) {
			status = http.StatusServiceUnavailable
		}
		writeDiagnosticsJSON(w, req, status, health)
	}
}

func runtimeReadyzHandler(r *Runtime) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !diagnosticsMethodAllowed(w, req) {
			return
		}
		health := runtimeDiagnosticsHealth(r)
		status := http.StatusOK
		if !health.Ready || health.Degraded || runtimeDiagnosticsFailed(health.State) {
			status = http.StatusServiceUnavailable
		}
		writeDiagnosticsJSON(w, req, status, health)
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

type runtimeDiagnosticsHealthPayload = RuntimeHealth

type runtimeDiagnosticsDescriptionPayload struct {
	Module ModuleDescription `json:"module"`
	Health RuntimeHealth     `json:"health"`
}

func runtimeDiagnosticsHealth(r *Runtime) RuntimeHealth {
	if r == nil {
		return runtimeNotConfiguredHealth()
	}
	return r.Health()
}

func runtimeDiagnosticsDescription(r *Runtime) runtimeDiagnosticsDescriptionPayload {
	if r == nil {
		return runtimeDiagnosticsDescriptionPayload{
			Module: ModuleDescription{Metadata: map[string]string{}},
			Health: runtimeNotConfiguredHealth(),
		}
	}
	desc := r.Describe()
	return runtimeDiagnosticsDescriptionPayload{
		Module: desc.Module,
		Health: runtimeDiagnosticsHealth(r),
	}
}

func runtimeDiagnosticsFailed(state RuntimeState) bool {
	switch state {
	case RuntimeStateFailed, RuntimeStateClosing, RuntimeStateClosed:
		return true
	default:
		return false
	}
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if req.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
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
