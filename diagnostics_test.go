package shunter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusNotReady, RuntimeStateBuilt)
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
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusNotReady, RuntimeStateBuilt)
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("/healthz Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("/healthz Cache-Control = %q, want no-store", got)
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusNotReady, RuntimeStateBuilt)

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD /healthz status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD /healthz body length = %d, want 0", rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("HEAD /healthz Content-Type = %q, want application/json", got)
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

func TestRuntimeDiagnosticsHealthzAndReadyzStatusSemantics(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir:        t.TempDir(),
		EnableProtocol: true,
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
	t.Cleanup(func() { _ = rt.Close() })

	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusOK, RuntimeStateReady)
	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusOK, RuntimeStateReady)

	notReady := buildValidTestRuntime(t)
	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(notReady).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusNotReady, RuntimeStateBuilt)
	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(notReady).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusNotReady, RuntimeStateBuilt)

	degraded, err := Build(validChatModule(), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build degraded runtime returned error: %v", err)
	}
	if err := degraded.Start(context.Background()); err != nil {
		t.Fatalf("Start degraded runtime returned error: %v", err)
	}
	t.Cleanup(func() { _ = degraded.Close() })
	degraded.mu.Lock()
	degraded.protocolServer = nil
	degraded.mu.Unlock()
	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(degraded).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusDegraded, RuntimeStateReady)
	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(degraded).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusDegraded, RuntimeStateReady)

	for _, state := range []RuntimeState{RuntimeStateFailed, RuntimeStateClosing, RuntimeStateClosed} {
		failed := buildValidTestRuntime(t)
		failed.mu.Lock()
		failed.stateName = state
		failed.ready.Store(false)
		failed.mu.Unlock()
		rec = httptest.NewRecorder()
		RuntimeDiagnosticsHandler(failed).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, state)
		rec = httptest.NewRecorder()
		RuntimeDiagnosticsHandler(failed).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, state)
	}
}

func TestRuntimeDiagnosticsNilDebugAndRedaction(t *testing.T) {
	rec := httptest.NewRecorder()
	RuntimeDiagnosticsHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, RuntimeStateFailed)

	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertRuntimeDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, RuntimeStateFailed)

	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/shunter/runtime", nil))
	assertRuntimeDiagnosticsDebugPayload(t, rec, RuntimeDescription{
		Module: ModuleDescription{Metadata: map[string]string{}},
		Health: runtimeNotConfiguredHealth(),
	})

	rt, err := Build(validChatModule(), Config{
		DataDir:        t.TempDir(),
		AuthSigningKey: []byte("super-secret-signing-key-material-12345"),
		Observability: ObservabilityConfig{
			Diagnostics: DiagnosticsConfig{MountHTTP: true},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	rt.mu.Lock()
	rt.lastErr = errors.New("token=secret")
	rt.mu.Unlock()

	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(rt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/shunter/runtime?ignored=true", nil))
	assertRuntimeDiagnosticsDebugPayload(t, rec, rt.Describe())
	body := rec.Body.String()
	if strings.Contains(body, "secret") || strings.Contains(body, "super-secret-signing-key-material") ||
		!strings.Contains(body, "[redacted]") {
		t.Fatalf("debug body = %q, want redacted health error and no signing key", body)
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

func TestDiagnosticsMethodHeadPathAndMetricsRules(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Diagnostics: DiagnosticsConfig{
				MountHTTP: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/debug/shunter/runtime", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST debug status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("POST debug Allow = %q, want GET, HEAD", got)
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HEAD /readyz status = %d, want 503", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD /readyz body length = %d, want 0", rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("HEAD /readyz Content-Type = %q, want application/json", got)
	}

	for _, path := range []string{"/healthz/", "/readyz/", "/debug/shunter/runtime/", "/debug/shunter/runtime/extra", "/metrics/"} {
		rec = httptest.NewRecorder()
		rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
	}

	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("runtime /metrics without handler status = %d, want 404", rec.Code)
	}

	unmountedMetrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	rt, err = Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Diagnostics: DiagnosticsConfig{MetricsHandler: unmountedMetrics},
		},
	})
	if err != nil {
		t.Fatalf("Build unmounted metrics runtime returned error: %v", err)
	}
	rec = httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unmounted runtime /metrics status = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	RuntimeDiagnosticsHandler(rt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("runtime helper /metrics status = %d, want delegated 202", rec.Code)
	}
}

func TestDiagnosticsMetricsPanicRecovered(t *testing.T) {
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

	host := &Host{}
	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, panicHTTPHandler{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("host panic /metrics status = %d, want 500", rec.Code)
	}
}

func TestHostDiagnosticsHandlerEndpoints(t *testing.T) {
	var nilHost *Host
	rec := httptest.NewRecorder()
	HostDiagnosticsHandler(nilHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, 0)

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(&Host{}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, 0)

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(nilHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/shunter/host?ignored=true", nil))
	assertHostDiagnosticsDebugPayload(t, rec, HostDescription{Modules: []HostModuleDescription{}})

	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusNotReady, 2)

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscribe", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("host /subscribe status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("host /metrics without handler status = %d, want 404", rec.Code)
	}

	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("host metrics\n"))
	})
	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, metrics).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusAccepted || rec.Body.String() != "host metrics\n" {
		t.Fatalf("host /metrics response = (%d, %q), want delegated metrics", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/debug/shunter/host", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("host POST debug status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("host POST debug Allow = %q, want GET, HEAD", got)
	}

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("host HEAD /readyz status = %d, want 503", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("host HEAD /readyz body length = %d, want 0", rec.Body.Len())
	}

	for _, path := range []string{"/healthz/", "/readyz/", "/debug/shunter/host/", "/debug/shunter/host/extra", "/metrics/"} {
		rec = httptest.NewRecorder()
		HostDiagnosticsHandler(host, metrics).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("host %s status = %d, want 404", path, rec.Code)
		}
	}

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(host, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/shunter/host", nil))
	assertHostDiagnosticsDebugPayload(t, rec, host.Describe())
}

func TestHostDiagnosticsStatusSemantics(t *testing.T) {
	readyRuntime := buildHostTestRuntime(t, "ready", t.TempDir())
	if err := readyRuntime.Start(context.Background()); err != nil {
		t.Fatalf("Start ready runtime returned error: %v", err)
	}
	readyHost, err := NewHost(HostRuntime{Name: "ready", RoutePrefix: "/ready", Runtime: readyRuntime})
	if err != nil {
		t.Fatalf("NewHost ready returned error: %v", err)
	}
	t.Cleanup(func() { _ = readyHost.Close() })

	rec := httptest.NewRecorder()
	HostDiagnosticsHandler(readyHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusOK, 1)
	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(readyHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusOK, 1)

	degradedRuntime, err := Build(NewModule("degraded").SchemaVersion(1).TableDef(messagesTableDef()), Config{
		DataDir:        t.TempDir(),
		EnableProtocol: true,
	})
	if err != nil {
		t.Fatalf("Build degraded runtime returned error: %v", err)
	}
	if err := degradedRuntime.Start(context.Background()); err != nil {
		t.Fatalf("Start degraded runtime returned error: %v", err)
	}
	degradedRuntime.mu.Lock()
	degradedRuntime.protocolServer = nil
	degradedRuntime.mu.Unlock()
	degradedHost, err := NewHost(HostRuntime{Name: "degraded", RoutePrefix: "/degraded", Runtime: degradedRuntime})
	if err != nil {
		t.Fatalf("NewHost degraded returned error: %v", err)
	}
	t.Cleanup(func() { _ = degradedHost.Close() })

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(degradedHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusOK, diagnosticsStatusDegraded, 1)
	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(degradedHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusDegraded, 1)

	failedRuntime := buildHostTestRuntime(t, "failed", t.TempDir())
	failedRuntime.mu.Lock()
	failedRuntime.stateName = RuntimeStateFailed
	failedRuntime.executorFatalErr = errors.New("executor fatal")
	failedRuntime.mu.Unlock()
	failedHost, err := NewHost(HostRuntime{Name: "failed", RoutePrefix: "/failed", Runtime: failedRuntime})
	if err != nil {
		t.Fatalf("NewHost failed returned error: %v", err)
	}

	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(failedHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, 1)
	rec = httptest.NewRecorder()
	HostDiagnosticsHandler(failedHost, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assertHostDiagnosticsHealthPayload(t, rec, http.StatusServiceUnavailable, diagnosticsStatusFailed, 1)
}

func assertRuntimeDiagnosticsHealthPayload(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantStatus diagnosticsStatus, wantState RuntimeState) {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("runtime diagnostics status code = %d, want %d; body=%q", rec.Code, wantCode, rec.Body.String())
	}
	var payload runtimeDiagnosticsHealthPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime diagnostics payload: %v", err)
	}
	if payload.Status != wantStatus || payload.Runtime.State != wantState {
		t.Fatalf("runtime diagnostics payload = %+v, want status=%q state=%q", payload, wantStatus, wantState)
	}
}

func assertHostDiagnosticsHealthPayload(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantStatus diagnosticsStatus, wantModules int) {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("host diagnostics status code = %d, want %d; body=%q", rec.Code, wantCode, rec.Body.String())
	}
	var payload hostDiagnosticsHealthPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode host diagnostics payload: %v", err)
	}
	if payload.Status != wantStatus || payload.Host.Modules == nil || len(payload.Host.Modules) != wantModules {
		t.Fatalf("host diagnostics payload = %+v, want status=%q modules=%d non-nil", payload, wantStatus, wantModules)
	}
}

func assertRuntimeDiagnosticsDebugPayload(t *testing.T, rec *httptest.ResponseRecorder, want RuntimeDescription) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime debug status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var got RuntimeDescription
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode runtime debug payload: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime debug payload = %#v, want %#v", got, want)
	}
}

func assertHostDiagnosticsDebugPayload(t *testing.T, rec *httptest.ResponseRecorder, want HostDescription) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("host debug status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var got HostDescription
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode host debug payload: %v", err)
	}
	if got.Modules == nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("host debug payload = %#v, want %#v with non-nil modules", got, want)
	}
}

type panicHTTPHandler struct{}

func (panicHTTPHandler) ServeHTTP(http.ResponseWriter, *http.Request) {
	panic("metrics handler failed")
}
