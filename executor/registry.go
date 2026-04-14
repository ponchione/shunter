package executor

import "fmt"

// ReducerRegistry holds registered reducers and lifecycle handlers.
type ReducerRegistry struct {
	reducers  map[string]*RegisteredReducer
	frozen    bool
}

var lifecycleNames = map[string]LifecycleKind{
	"OnConnect":    LifecycleOnConnect,
	"OnDisconnect": LifecycleOnDisconnect,
}

// NewReducerRegistry creates an empty registry.
func NewReducerRegistry() *ReducerRegistry {
	return &ReducerRegistry{
		reducers: make(map[string]*RegisteredReducer),
	}
}

// Register adds a reducer. Returns error on duplicates, reserved names, or if frozen.
func (rr *ReducerRegistry) Register(r RegisteredReducer) error {
	if rr.frozen {
		return fmt.Errorf("executor: registry is frozen")
	}
	if r.Name == "" {
		return fmt.Errorf("executor: reducer name must not be empty")
	}

	// Reserved name check.
	if expectedKind, reserved := lifecycleNames[r.Name]; reserved {
		if r.Lifecycle != expectedKind {
			return fmt.Errorf("executor: reducer name %q is reserved for lifecycle %v", r.Name, expectedKind)
		}
	} else if r.Lifecycle != LifecycleNone {
		return fmt.Errorf("executor: lifecycle reducer %q must use reserved name", r.Name)
	}

	// Duplicate check.
	if _, exists := rr.reducers[r.Name]; exists {
		if r.Lifecycle != LifecycleNone {
			return fmt.Errorf("executor: duplicate lifecycle reducer %q", r.Name)
		}
		return fmt.Errorf("executor: duplicate reducer %q", r.Name)
	}

	rr.reducers[r.Name] = &r
	return nil
}

// Lookup returns a registered reducer by name.
func (rr *ReducerRegistry) Lookup(name string) (*RegisteredReducer, bool) {
	r, ok := rr.reducers[name]
	return r, ok
}

// LookupLifecycle returns a lifecycle reducer by kind.
func (rr *ReducerRegistry) LookupLifecycle(kind LifecycleKind) (*RegisteredReducer, bool) {
	for _, r := range rr.reducers {
		if r.Lifecycle == kind {
			return r, true
		}
	}
	return nil, false
}

// All returns all registered reducers.
func (rr *ReducerRegistry) All() []*RegisteredReducer {
	out := make([]*RegisteredReducer, 0, len(rr.reducers))
	for _, r := range rr.reducers {
		out = append(out, r)
	}
	return out
}

// Freeze prevents further registrations.
func (rr *ReducerRegistry) Freeze() {
	rr.frozen = true
}

// IsFrozen returns whether the registry is frozen.
func (rr *ReducerRegistry) IsFrozen() bool {
	return rr.frozen
}
