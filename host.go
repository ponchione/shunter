package shunter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrHostServing reports that a host serving loop is already active.
var ErrHostServing = errors.New("shunter: host is already serving")

// HostRuntime binds one built Runtime to the explicit module identity and HTTP
// route prefix used by a multi-module Host.
type HostRuntime struct {
	Name        string
	RoutePrefix string
	Runtime     *Runtime
}

// Host owns a fixed set of built single-module runtimes and mounts each under
// an explicit route prefix without merging their schemas, transactions, or
// contracts.
type Host struct {
	lifecycleMu sync.Mutex
	modules     []hostRuntimeMount
	byName      map[string]*Runtime
	serving     bool
}

type hostRuntimeMount struct {
	name        string
	routePrefix string
	dataDir     string
	runtime     *Runtime
}

// HostDescription is a detached multi-module host diagnostics snapshot.
type HostDescription struct {
	Modules []HostModuleDescription
}

// HostModuleDescription describes one runtime mounted in a Host.
type HostModuleDescription struct {
	Name        string
	RoutePrefix string
	DataDir     string
	Runtime     RuntimeDescription
}

// HostHealth is a detached per-module lifecycle/readiness snapshot.
type HostHealth struct {
	Ready    bool               `json:"ready"`
	Degraded bool               `json:"degraded"`
	Modules  []HostModuleHealth `json:"modules"`
}

// HostModuleHealth reports health for one runtime mounted in a Host.
type HostModuleHealth struct {
	Name        string        `json:"name"`
	RoutePrefix string        `json:"route_prefix"`
	Health      RuntimeHealth `json:"health"`
}

// NewHost validates and builds a multi-module host from already-built runtimes.
// Each runtime keeps its own schema, state, lifecycle resources, and canonical
// ModuleContract.
func NewHost(modules ...HostRuntime) (*Host, error) {
	host := &Host{
		modules: make([]hostRuntimeMount, 0, len(modules)),
		byName:  make(map[string]*Runtime, len(modules)),
	}
	routePrefixes := make([]string, 0, len(modules))
	dataDirs := make(map[string]string, len(modules))

	for _, module := range modules {
		if module.Runtime == nil {
			return nil, fmt.Errorf("host runtime %q must not be nil", module.Name)
		}
		if strings.TrimSpace(module.Name) == "" {
			return nil, fmt.Errorf("host module name must not be empty")
		}
		if runtimeName := module.Runtime.ModuleName(); module.Name != runtimeName {
			return nil, fmt.Errorf("host module %q does not match runtime module %q", module.Name, runtimeName)
		}
		if _, ok := host.byName[module.Name]; ok {
			return nil, fmt.Errorf("duplicate host module %q", module.Name)
		}

		routePrefix, err := normalizeHostRoutePrefix(module.RoutePrefix)
		if err != nil {
			return nil, fmt.Errorf("host module %q route prefix: %w", module.Name, err)
		}
		for _, existing := range routePrefixes {
			if hostRoutesOverlap(existing, routePrefix) {
				return nil, fmt.Errorf("host module %q route prefix %q conflicts with %q", module.Name, routePrefix, existing)
			}
		}

		dataDirKey, err := hostDataDirKey(module.Runtime.dataDir)
		if err != nil {
			return nil, fmt.Errorf("host module %q data dir: %w", module.Name, err)
		}
		if existing, ok := dataDirs[dataDirKey]; ok {
			return nil, fmt.Errorf("host module %q data dir conflicts with module %q", module.Name, existing)
		}

		host.modules = append(host.modules, hostRuntimeMount{
			name:        module.Name,
			routePrefix: routePrefix,
			dataDir:     module.Runtime.dataDir,
			runtime:     module.Runtime,
		})
		host.byName[module.Name] = module.Runtime
		routePrefixes = append(routePrefixes, routePrefix)
		dataDirs[dataDirKey] = module.Name
	}
	return host, nil
}

// Runtime returns the runtime registered for name.
func (h *Host) Runtime(name string) (*Runtime, bool) {
	if h == nil {
		return nil, false
	}
	rt, ok := h.byName[name]
	return rt, ok
}

// Start starts each hosted runtime in registration order. If any runtime fails
// to start, runtimes already started by this call are closed in reverse order.
func (h *Host) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	started := make([]hostRuntimeMount, 0, len(h.modules))
	for _, module := range h.modules {
		if err := module.runtime.Start(ctx); err != nil {
			cleanupErr := closeHostModulesReverse(started)
			return errors.Join(fmt.Errorf("start host module %q: %w", module.name, err), cleanupErr)
		}
		started = append(started, module)
	}
	return nil
}

// Close closes every hosted runtime in reverse registration order.
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	return closeHostModulesReverse(h.modules)
}

// HTTPHandler returns a composable handler that routes each module under its
// configured prefix. Call Start before serving protocol traffic.
func (h *Host) HTTPHandler() http.Handler {
	modules := h.snapshotModules()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		for _, module := range modules {
			if pathMatchesHostPrefix(req.URL.Path, module.routePrefix) {
				http.StripPrefix(module.routePrefix, module.runtime.HTTPHandler()).ServeHTTP(w, req)
				return
			}
		}
		http.NotFound(w, req)
	})
}

// ListenAndServe starts hosted runtimes if needed, serves Host.HTTPHandler on
// addr, and shuts serving plus hosted runtimes down when ctx is canceled.
func (h *Host) ListenAndServe(ctx context.Context, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !h.tryBeginServing() {
		return ErrHostServing
	}
	if strings.TrimSpace(addr) == "" {
		addr = defaultListenAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		h.endServing()
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	return h.serveStarted(ctx, ln)
}

func (h *Host) serve(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !h.tryBeginServing() {
		_ = ln.Close()
		return ErrHostServing
	}
	return h.serveStarted(ctx, ln)
}

func (h *Host) tryBeginServing() bool {
	if h == nil {
		return true
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.serving {
		return false
	}
	h.serving = true
	return true
}

func (h *Host) endServing() {
	if h == nil {
		return
	}
	h.lifecycleMu.Lock()
	h.serving = false
	h.lifecycleMu.Unlock()
}

func (h *Host) serveStarted(ctx context.Context, ln net.Listener) error {
	defer h.endServing()

	if err := h.Start(ctx); err != nil {
		_ = ln.Close()
		return err
	}

	httpServer := &http.Server{Handler: h.HTTPHandler()}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownCtx)
		closeErr := h.Close()
		serveErr := <-errCh
		if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
			return shutdownErr
		}
		if closeErr != nil {
			return closeErr
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return ctx.Err()
	case err := <-errCh:
		closeErr := h.Close()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return closeErr
	}
}

// Health returns detached health for every hosted runtime in registration order.
func (h *Host) Health() HostHealth {
	modules := h.snapshotModules()
	out := HostHealth{Modules: make([]HostModuleHealth, len(modules))}
	if len(modules) == 0 {
		out.Degraded = true
		return out
	}
	out.Ready = true
	for i, module := range modules {
		health := module.runtime.Health()
		out.Modules[i] = HostModuleHealth{
			Name:        module.name,
			RoutePrefix: module.routePrefix,
			Health:      health,
		}
		if !health.Ready {
			out.Ready = false
		}
		if health.Degraded {
			out.Degraded = true
		}
	}
	return out
}

// Describe returns detached diagnostics for every hosted runtime in registration order.
func (h *Host) Describe() HostDescription {
	modules := h.snapshotModules()
	out := HostDescription{Modules: make([]HostModuleDescription, len(modules))}
	for i, module := range modules {
		out.Modules[i] = HostModuleDescription{
			Name:        module.name,
			RoutePrefix: module.routePrefix,
			DataDir:     module.dataDir,
			Runtime:     module.runtime.Describe(),
		}
	}
	if out.Modules == nil {
		out.Modules = []HostModuleDescription{}
	}
	return out
}

func (h *Host) snapshotModules() []hostRuntimeMount {
	if h == nil || len(h.modules) == 0 {
		return []hostRuntimeMount{}
	}
	return slices.Clone(h.modules)
}

func closeHostModulesReverse(modules []hostRuntimeMount) error {
	var errs []error
	for i := len(modules) - 1; i >= 0; i-- {
		module := modules[i]
		if err := module.runtime.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close host module %q: %w", module.name, err))
		}
	}
	return errors.Join(errs...)
}

func normalizeHostRoutePrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if !strings.HasPrefix(prefix, "/") {
		return "", fmt.Errorf("must start with /")
	}
	cleaned := path.Clean(prefix)
	if cleaned == "/" {
		return "", fmt.Errorf("must not be /")
	}
	return cleaned, nil
}

func hostRoutesOverlap(a, b string) bool {
	return pathMatchesHostPrefix(a, b) || pathMatchesHostPrefix(b, a)
}

func pathMatchesHostPrefix(requestPath, prefix string) bool {
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}

func hostDataDirKey(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("must not be empty")
	}
	resolved, err := resolvePathForContainment(dataDir)
	if err != nil {
		return "", err
	}
	return resolved, nil
}
