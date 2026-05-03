package shunter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHostRegistersMultipleRuntimesWithDistinctModuleNames(t *testing.T) {
	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())

	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	desc := host.Describe()
	if got, want := hostDescriptionNames(desc), []string{"chat", "ops"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("host module names = %#v, want %#v", got, want)
	}
	if desc.Modules[0].RoutePrefix != "/chat" || desc.Modules[1].RoutePrefix != "/ops" {
		t.Fatalf("route prefixes = %#v, want /chat then /ops", desc.Modules)
	}
}

func TestHostRejectsDuplicateModuleNames(t *testing.T) {
	_, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: buildHostTestRuntime(t, "chat", t.TempDir())},
		HostRuntime{Name: "chat", RoutePrefix: "/ops", Runtime: buildHostTestRuntime(t, "chat", t.TempDir())},
	)
	if err == nil {
		t.Fatal("NewHost succeeded with duplicate module names")
	}
	assertErrorMentions(t, err, "duplicate")
	assertErrorMentions(t, err, "chat")
}

func TestHostRejectsNilRuntime(t *testing.T) {
	_, err := NewHost(HostRuntime{Name: "chat", RoutePrefix: "/chat"})
	if err == nil {
		t.Fatal("NewHost succeeded with nil runtime")
	}
	assertErrorMentions(t, err, "runtime")
	assertErrorMentions(t, err, "nil")
}

func TestHostRejectsBlankModuleName(t *testing.T) {
	_, err := NewHost(HostRuntime{Name: "   ", RoutePrefix: "/chat", Runtime: buildHostTestRuntime(t, "chat", t.TempDir())})
	if err == nil {
		t.Fatal("NewHost succeeded with blank module name")
	}
	assertErrorMentions(t, err, "name")
	assertErrorMentions(t, err, "empty")
}

func TestHostRejectsModuleRuntimeIdentityMismatch(t *testing.T) {
	_, err := NewHost(HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: buildHostTestRuntime(t, "chat", t.TempDir())})
	if err == nil {
		t.Fatal("NewHost succeeded with mismatched module and runtime names")
	}
	assertErrorMentions(t, err, "ops")
	assertErrorMentions(t, err, "chat")
}

func TestHostRejectsOverlappingRoutePrefixes(t *testing.T) {
	_, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: buildHostTestRuntime(t, "chat", t.TempDir())},
		HostRuntime{Name: "ops", RoutePrefix: "/chat/admin", Runtime: buildHostTestRuntime(t, "ops", t.TempDir())},
	)
	if err == nil {
		t.Fatal("NewHost succeeded with overlapping route prefixes")
	}
	assertErrorMentions(t, err, "prefix")
	assertErrorMentions(t, err, "conflicts")
}

func TestHostRejectsSharedRuntimeDataDir(t *testing.T) {
	dir := t.TempDir()
	chat := buildHostTestRuntime(t, "chat", dir)
	ops := buildHostTestRuntime(t, "ops", dir)

	_, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err == nil {
		t.Fatal("NewHost succeeded with shared runtime data dir")
	}
	assertErrorMentions(t, err, "data")
}

func TestHostStartsInRegistrationOrderAndCleansPartialStart(t *testing.T) {
	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	injected := errors.New("injected host start failure")
	var order []string
	oldHook := runtimeStartAfterDurabilityHook
	runtimeStartAfterDurabilityHook = func(rt *Runtime) error {
		order = append(order, rt.ModuleName())
		if rt.ModuleName() == "ops" {
			return injected
		}
		return nil
	}
	defer func() { runtimeStartAfterDurabilityHook = oldHook }()

	err = host.Start(context.Background())
	if err == nil || !errors.Is(err, injected) {
		t.Fatalf("Start error = %v, want injected failure", err)
	}
	if got, want := order, []string{"chat", "ops"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %#v, want %#v", got, want)
	}
	if got := chat.Health().State; got != RuntimeStateClosed {
		t.Fatalf("first runtime state after partial start cleanup = %q, want %q", got, RuntimeStateClosed)
	}
	if ops.Ready() {
		t.Fatal("failing runtime ready after failed host start")
	}
}

func TestHostCloseClosesEveryStartedRuntime(t *testing.T) {
	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}
	if err := host.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if err := host.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := chat.Health().State; got != RuntimeStateClosed {
		t.Fatalf("chat state = %q, want closed", got)
	}
	if got := ops.Health().State; got != RuntimeStateClosed {
		t.Fatalf("ops state = %q, want closed", got)
	}
}

func TestHostHTTPHandlerRoutesByModulePrefix(t *testing.T) {
	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	if err := chat.Start(context.Background()); err != nil {
		t.Fatalf("chat Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = chat.Close() })

	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/chat/subscribe", nil)
	rec := httptest.NewRecorder()
	host.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("/chat/subscribe status = %d, want protocol rejection 400", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/ops/subscribe", nil)
	rec = httptest.NewRecorder()
	host.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ops/subscribe status = %d, want runtime-not-ready 503", rec.Code)
	}
}

func TestHostHealthReportsPerModuleState(t *testing.T) {
	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	health := host.Health()
	assertHostModuleHealth(t, health, "chat", RuntimeStateBuilt, false)
	assertHostModuleHealth(t, health, "ops", RuntimeStateBuilt, false)

	if err := host.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	health = host.Health()
	assertHostModuleHealth(t, health, "chat", RuntimeStateReady, true)
	assertHostModuleHealth(t, health, "ops", RuntimeStateReady, true)
}

func TestHostPreservesPerModuleContractsAndDetachedDescription(t *testing.T) {
	chat := buildHostTestRuntime(t, "chat", t.TempDir())
	ops := buildHostTestRuntime(t, "ops", t.TempDir())
	chatContract := chat.ExportContract()
	opsContract := ops.ExportContract()

	host, err := NewHost(
		HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chat},
		HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: ops},
	)
	if err != nil {
		t.Fatalf("NewHost returned error: %v", err)
	}

	desc := host.Describe()
	desc.Modules[0].Name = "mutated"
	desc.Modules[0].Runtime.Module.Name = "mutated-runtime"
	desc.Modules[0].Runtime.Module.Metadata["mutated"] = "true"

	again := host.Describe()
	if again.Modules[0].Name != "chat" {
		t.Fatalf("description module name = %q, want chat", again.Modules[0].Name)
	}
	if again.Modules[0].Runtime.Module.Name != "chat" {
		t.Fatalf("runtime description module name = %q, want chat", again.Modules[0].Runtime.Module.Name)
	}
	if _, ok := again.Modules[0].Runtime.Module.Metadata["mutated"]; ok {
		t.Fatal("host description was not detached")
	}
	if got := chat.ExportContract(); !reflect.DeepEqual(got, chatContract) {
		t.Fatalf("chat contract changed after host registration:\n got: %#v\nwant: %#v", got, chatContract)
	}
	if got := ops.ExportContract(); !reflect.DeepEqual(got, opsContract) {
		t.Fatalf("ops contract changed after host registration:\n got: %#v\nwant: %#v", got, opsContract)
	}
}

func buildHostTestRuntime(t *testing.T, name, dataDir string) *Runtime {
	t.Helper()
	rt, err := Build(NewModule(name).SchemaVersion(1).TableDef(messagesTableDef()), Config{DataDir: dataDir, EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build(%q) returned error: %v", name, err)
	}
	return rt
}

func hostDescriptionNames(desc HostDescription) []string {
	names := make([]string, len(desc.Modules))
	for i, module := range desc.Modules {
		names[i] = module.Name
	}
	return names
}

func assertHostModuleHealth(t *testing.T, health HostHealth, name string, state RuntimeState, ready bool) {
	t.Helper()
	for _, module := range health.Modules {
		if module.Name != name {
			continue
		}
		if module.Health.State != state || module.Health.Ready != ready {
			t.Fatalf("module %q health = %+v, want state=%q ready=%v", name, module.Health, state, ready)
		}
		return
	}
	t.Fatalf("host health modules = %#v, want %q", health.Modules, name)
}
