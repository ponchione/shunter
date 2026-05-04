package executor

import (
	"errors"
	"testing"
)

func TestRegisterCachesLifecycleSlots(t *testing.T) {
	rr := NewReducerRegistry()
	if err := rr.Register(RegisteredReducer{Name: "OnConnect", Lifecycle: LifecycleOnConnect}); err != nil {
		t.Fatalf("Register(OnConnect) error = %v", err)
	}
	if err := rr.Register(RegisteredReducer{Name: "OnDisconnect", Lifecycle: LifecycleOnDisconnect}); err != nil {
		t.Fatalf("Register(OnDisconnect) error = %v", err)
	}

	if rr.lifecycle[LifecycleOnConnect] == nil {
		t.Fatal("lifecycle slot for OnConnect should be populated during Register")
	}
	if rr.lifecycle[LifecycleOnDisconnect] == nil {
		t.Fatal("lifecycle slot for OnDisconnect should be populated during Register")
	}

	if got, ok := rr.LookupLifecycle(LifecycleOnConnect); !ok || got.Name != "OnConnect" {
		t.Fatalf("LookupLifecycle(OnConnect) = (%v, %v), want OnConnect", got, ok)
	}
	if got, ok := rr.LookupLifecycle(LifecycleOnDisconnect); !ok || got.Name != "OnDisconnect" {
		t.Fatalf("LookupLifecycle(OnDisconnect) = (%v, %v), want OnDisconnect", got, ok)
	}
}

func TestRegisterRejectsReducerIDOverflowAtomically(t *testing.T) {
	rr := NewReducerRegistry()
	rr.nextID = ^uint32(0)

	err := rr.Register(RegisteredReducer{Name: "overflow"})
	if !errors.Is(err, ErrReducerIDExhausted) {
		t.Fatalf("Register error = %v, want ErrReducerIDExhausted", err)
	}
	if got := rr.nextID; got != ^uint32(0) {
		t.Fatalf("nextID = %d, want unchanged max uint32", got)
	}
	if _, ok := rr.Lookup("overflow"); ok {
		t.Fatal("overflow reducer was registered")
	}
}
