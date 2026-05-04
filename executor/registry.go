package executor

import (
	"fmt"
	"sync"
)

// ReducerRegistry holds registered reducers and lifecycle handlers.
type ReducerRegistry struct {
	mu        sync.RWMutex
	reducers  map[string]*RegisteredReducer
	lifecycle [LifecycleOnDisconnect + 1]*RegisteredReducer
	nextID    uint32
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
	rr.mu.Lock()
	defer rr.mu.Unlock()
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
	if rr.nextID == ^uint32(0) {
		return ErrReducerIDExhausted
	}

	r.RequiredPermissions = append([]string(nil), r.RequiredPermissions...)
	r.ID = rr.nextID
	rr.nextID++
	rr.reducers[r.Name] = &r
	if r.Lifecycle != LifecycleNone {
		rr.lifecycle[r.Lifecycle] = &r
	}
	return nil
}

func cloneRegisteredReducer(r *RegisteredReducer) *RegisteredReducer {
	if r == nil {
		return nil
	}
	clone := *r
	clone.RequiredPermissions = append([]string(nil), r.RequiredPermissions...)
	return &clone
}

// Lookup returns a registered reducer by name.
func (rr *ReducerRegistry) Lookup(name string) (*RegisteredReducer, bool) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	r, ok := rr.reducers[name]
	if !ok {
		return nil, false
	}
	return cloneRegisteredReducer(r), true
}

// LookupLifecycle returns a lifecycle reducer by kind.
func (rr *ReducerRegistry) LookupLifecycle(kind LifecycleKind) (*RegisteredReducer, bool) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	if kind <= LifecycleNone || int(kind) >= len(rr.lifecycle) {
		return nil, false
	}
	r := rr.lifecycle[kind]
	if r == nil {
		return nil, false
	}
	return cloneRegisteredReducer(r), true
}

// All returns all registered reducers.
func (rr *ReducerRegistry) All() []*RegisteredReducer {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	out := make([]*RegisteredReducer, 0, len(rr.reducers))
	for _, r := range rr.reducers {
		out = append(out, cloneRegisteredReducer(r))
	}
	return out
}

// Freeze prevents further registrations.
func (rr *ReducerRegistry) Freeze() {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.frozen = true
}

// IsFrozen returns whether the registry is frozen.
func (rr *ReducerRegistry) IsFrozen() bool {
	rr.mu.RLock()
	defer rr.mu.RUnlock()
	return rr.frozen
}
