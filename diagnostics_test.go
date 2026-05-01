package shunter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeDiagnosticsMountingAndHelperBehavior(t *testing.T) {
	rt := buildValidTestRuntime(t)

	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unmounted /healthz status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(rt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("helper /healthz status = %d, want 200", rec.Code)
	}
	var health runtimeDiagnosticsHealthPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode helper /healthz: %v", err)
	}
	if health.State != RuntimeStateBuilt || health.Ready || health.Degraded {
		t.Fatalf("helper /healthz payload = %+v, want built not-ready non-degraded", health)
	}
}

func TestRuntimeDiagnosticsMountedEndpointsAndProtocolRoute(t *testing.T) {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("metrics\n"))
	})
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Diagnostics: DiagnosticsConfig{
				MountHTTP:      true,
				MetricsHandler: metrics,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz?ignored=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("mounted /healthz status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("/healthz Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("/healthz Cache-Control = %q, want no-store", got)
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("built /readyz status = %d, want 503", rec.Code)
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD /healthz status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD /healthz body length = %d, want 0", rec.Body.Len())
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("POST /healthz Allow = %q, want GET, HEAD", got)
	}

	for _, path := range []string{"/healthz/", "/debug/shunter/runtime/extra"} {
		rec = httptest.NewRecorder()
		rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusAccepted || rec.Body.String() != "metrics\n" {
		t.Fatalf("/metrics response = (%d, %q), want accepted metrics", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscribe", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/subscribe before start status = %d, want 503", rec.Code)
	}
}

func TestRuntimeDiagnosticsReadyClosedNilAndRedaction(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Diagnostics: DiagnosticsConfig{MountHTTP: true},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready /readyz status = %d, want 200", rec.Code)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed /healthz status = %d, want 503", rec.Code)
	}

	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/shunter/runtime", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil debug status = %d, want 200", rec.Code)
	}
	var desc runtimeDiagnosticsDescriptionPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &desc); err != nil {
		t.Fatalf("decode nil debug payload: %v", err)
	}
	if desc.Module.Metadata == nil || desc.Health.State != RuntimeStateFailed || !desc.Health.Degraded {
		t.Fatalf("nil debug payload = %+v, want failed zero description with metadata map", desc)
	}

	failed := buildValidTestRuntime(t)
	failed.mu.Lock()
	failed.stateName = RuntimeStateFailed
	failed.lastErr = errors.New("token=secret")
	failed.mu.Unlock()
	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(failed).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed /healthz status = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "secret") || !strings.Contains(body, "[redacted]") {
		t.Fatalf("failed /healthz body = %q, want redacted error", body)
	}
}

func TestRuntimeDiagnosticsMetricsPanicRecovered(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Diagnostics: DiagnosticsConfig{
				MountHTTP:      true,
				MetricsHandler: panicHTTPHandler{},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic /metrics status = %d, want 500", rec.Code)
	}
}

type panicHTTPHandler struct{}

func (panicHTTPHandler) ServeHTTP(http.ResponseWriter, *http.Request) {
	panic("metrics handler failed")
}
