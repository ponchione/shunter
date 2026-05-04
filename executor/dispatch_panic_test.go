package executor

import (
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

type fatalPanicDurability struct{}

func (fatalPanicDurability) EnqueueCommitted(types.TxID, *store.Changeset) {}
func (fatalPanicDurability) WaitUntilDurable(types.TxID) <-chan types.TxID { return nil }
func (fatalPanicDurability) FatalError() error                             { panic("fatal probe") }

func TestDispatchPanicRepliesToAllCommandTypes(t *testing.T) {
	t.Run("call reducer", func(t *testing.T) {
		exec, _ := setupExecutor()
		exec.durability = fatalPanicDurability{}
		ch := make(chan ReducerResponse, 1)

		exec.dispatchSafely(CallReducerCmd{
			Request:    ReducerRequest{ReducerName: "InsertPlayer", Source: CallSourceExternal},
			ResponseCh: ch,
		})

		resp := receiveReducerResponse(t, ch)
		if resp.Status != StatusFailedPanic {
			t.Fatalf("status = %d, want StatusFailedPanic", resp.Status)
		}
		if resp.Error == nil || !strings.Contains(resp.Error.Error(), "fatal probe") {
			t.Fatalf("error = %v, want recovered panic text", resp.Error)
		}
	})

	t.Run("register subscription", func(t *testing.T) {
		exec, _ := setupExecutor()
		exec.durability = fatalPanicDurability{}
		errCh := make(chan error, 1)

		exec.dispatchSafely(RegisterSubscriptionSetCmd{
			Request: subscription.SubscriptionSetRegisterRequest{
				ConnID:     types.ConnectionID{1},
				QueryID:    10,
				Predicates: []subscription.Predicate{subscription.AllRows{Table: 0}},
			},
			Reply: func(_ subscription.SubscriptionSetRegisterResult, err error) {
				errCh <- err
			},
		})

		expectDispatchPanicError(t, "register subscription", errCh)
	})

	t.Run("unregister subscription", func(t *testing.T) {
		exec, _ := setupExecutor()
		exec.durability = fatalPanicDurability{}
		errCh := make(chan error, 1)

		exec.dispatchSafely(UnregisterSubscriptionSetCmd{
			ConnID:  types.ConnectionID{1},
			QueryID: 10,
			Reply: func(_ subscription.SubscriptionSetUnregisterResult, err error) {
				errCh <- err
			},
		})

		expectDispatchPanicError(t, "unregister subscription", errCh)
	})

	t.Run("disconnect subscriptions", func(t *testing.T) {
		exec, _ := setupExecutor()
		exec.durability = fatalPanicDurability{}
		errCh := make(chan error, 1)

		exec.dispatchSafely(DisconnectClientSubscriptionsCmd{
			ConnID:     types.ConnectionID{1},
			ResponseCh: errCh,
		})

		expectDispatchPanicError(t, "disconnect subscriptions", errCh)
	})

	t.Run("on connect", func(t *testing.T) {
		exec, _ := setupExecutor()
		exec.durability = fatalPanicDurability{}
		ch := make(chan ReducerResponse, 1)

		exec.dispatchSafely(OnConnectCmd{
			ConnID:     types.ConnectionID{1},
			Identity:   types.Identity{2},
			ResponseCh: ch,
		})

		expectLifecycleDispatchPanicResponse(t, "on connect", ch)
	})

	t.Run("on disconnect", func(t *testing.T) {
		exec, _ := setupExecutor()
		exec.durability = fatalPanicDurability{}
		ch := make(chan ReducerResponse, 1)

		exec.dispatchSafely(OnDisconnectCmd{
			ConnID:     types.ConnectionID{1},
			Identity:   types.Identity{2},
			ResponseCh: ch,
		})

		expectLifecycleDispatchPanicResponse(t, "on disconnect", ch)
	})
}

func expectDispatchPanicError(t *testing.T, label string, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		if err == nil || !strings.Contains(err.Error(), "executor dispatch panic: fatal probe") {
			t.Fatalf("%s error = %v, want recovered dispatch panic", label, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s reply timed out", label)
	}
}

func expectLifecycleDispatchPanicResponse(t *testing.T, label string, ch <-chan ReducerResponse) {
	t.Helper()
	select {
	case resp := <-ch:
		if resp.Status != StatusFailedInternal {
			t.Fatalf("%s status = %d, want StatusFailedInternal", label, resp.Status)
		}
		if resp.Error == nil || !strings.Contains(resp.Error.Error(), "executor dispatch panic: fatal probe") {
			t.Fatalf("%s error = %v, want recovered dispatch panic", label, resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s reply timed out", label)
	}
}
