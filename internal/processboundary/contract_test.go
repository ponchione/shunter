package processboundary

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestInvocationRequestAndResponseRepresentReducerCall(t *testing.T) {
	var identity types.Identity
	identity[0] = 0x42
	var connID types.ConnectionID
	connID[0] = 0x24

	req := InvocationRequest{
		Kind:      InvocationKindReducer,
		Module:    "chat",
		Name:      "send_message",
		Args:      []byte{0x01, 0x02},
		RequestID: 7,
		Caller: Caller{
			Identity:            identity,
			ConnectionID:        connID,
			Permissions:         []string{"messages:send"},
			AllowAllPermissions: false,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal request returned error: %v", err)
	}
	var decodedReq InvocationRequest
	if err := json.Unmarshal(data, &decodedReq); err != nil {
		t.Fatalf("Unmarshal request returned error: %v", err)
	}
	if !reflect.DeepEqual(decodedReq, req) {
		t.Fatalf("decoded request = %#v, want %#v", decodedReq, req)
	}

	resp := InvocationResponse{
		Status: InvocationStatusCommitted,
		Output: []byte{0x03, 0x04},
		Transaction: TransactionOutcome{
			Mode:     TransactionModeUnsupported,
			Decision: TransactionDecisionUnsupported,
			Reason:   "host transaction objects cannot cross the process boundary",
		},
	}
	if err := ValidateInvocationResponse(resp); err != nil {
		t.Fatalf("ValidateInvocationResponse returned error: %v", err)
	}
}

func TestInvocationFailuresDistinguishUserAndBoundaryFailures(t *testing.T) {
	user := UserFailure("reducer rejected input")
	if user.Status != InvocationStatusUserError {
		t.Fatalf("user status = %q, want %q", user.Status, InvocationStatusUserError)
	}
	if user.Failure.Class != FailureClassUser {
		t.Fatalf("user failure class = %q, want %q", user.Failure.Class, FailureClassUser)
	}
	if !user.IsUserFailure() {
		t.Fatal("user failure was not classified as user failure")
	}
	if user.IsBoundaryFailure() {
		t.Fatal("user failure was classified as boundary failure")
	}

	boundary := BoundaryFailure("transport closed")
	if boundary.Status != InvocationStatusBoundaryFailure {
		t.Fatalf("boundary status = %q, want %q", boundary.Status, InvocationStatusBoundaryFailure)
	}
	if boundary.Failure.Class != FailureClassBoundary {
		t.Fatalf("boundary failure class = %q, want %q", boundary.Failure.Class, FailureClassBoundary)
	}
	if !boundary.IsBoundaryFailure() {
		t.Fatal("boundary failure was not classified as boundary failure")
	}
	if boundary.IsUserFailure() {
		t.Fatal("boundary failure was classified as user failure")
	}
}

func TestValidateInvocationResponseRequiresExplicitTransactionSemantics(t *testing.T) {
	err := ValidateInvocationResponse(InvocationResponse{
		Status: InvocationStatusCommitted,
		Output: []byte{0x01},
	})
	if !errors.Is(err, ErrMissingTransactionSemantics) {
		t.Fatalf("ValidateInvocationResponse error = %v, want ErrMissingTransactionSemantics", err)
	}

	err = ValidateInvocationResponse(InvocationResponse{
		Status: InvocationStatusCommitted,
		Transaction: TransactionOutcome{
			Mode:     TransactionModeUnsupported,
			Decision: TransactionDecisionCommit,
		},
	})
	if !errors.Is(err, ErrUnsupportedTransactionDecision) {
		t.Fatalf("ValidateInvocationResponse error = %v, want ErrUnsupportedTransactionDecision", err)
	}
}

func TestDefaultContractDeclaresLifecycleAndSubscriptionSemantics(t *testing.T) {
	contract := DefaultContract()

	if contract.Decision != DecisionDeferred {
		t.Fatalf("decision = %q, want %q", contract.Decision, DecisionDeferred)
	}
	if contract.Transactions.Mode != TransactionModeUnsupported {
		t.Fatalf("transaction mode = %q, want %q", contract.Transactions.Mode, TransactionModeUnsupported)
	}
	if contract.Subscriptions.UpdateSource != SubscriptionUpdateSourceCommittedState {
		t.Fatalf("subscription update source = %q, want %q", contract.Subscriptions.UpdateSource, SubscriptionUpdateSourceCommittedState)
	}
	if contract.Subscriptions.ProcessMessagesMayBroadcast {
		t.Fatal("process messages were allowed to broadcast subscription updates")
	}

	onConnect, ok := contract.Lifecycle[LifecycleOnConnect]
	if !ok {
		t.Fatal("OnConnect lifecycle contract missing")
	}
	assertLifecycleSteps(t, LifecycleOnConnect, onConnect.Ordering,
		LifecycleStepInsertClient,
		LifecycleStepInvokeReducer,
		LifecycleStepCommitOrRollback,
	)
	if onConnect.FailureBehavior != LifecycleFailureRejectConnectionRollback {
		t.Fatalf("OnConnect failure behavior = %q, want %q", onConnect.FailureBehavior, LifecycleFailureRejectConnectionRollback)
	}

	onDisconnect, ok := contract.Lifecycle[LifecycleOnDisconnect]
	if !ok {
		t.Fatal("OnDisconnect lifecycle contract missing")
	}
	assertLifecycleSteps(t, LifecycleOnDisconnect, onDisconnect.Ordering,
		LifecycleStepInvokeReducer,
		LifecycleStepCleanupClient,
		LifecycleStepCommitCleanup,
	)
	if onDisconnect.FailureBehavior != LifecycleFailureCleanupStillCommits {
		t.Fatalf("OnDisconnect failure behavior = %q, want %q", onDisconnect.FailureBehavior, LifecycleFailureCleanupStillCommits)
	}

	if err := ValidateContract(contract); err != nil {
		t.Fatalf("ValidateContract returned error: %v", err)
	}
}

func TestValidateContractRejectsProcessDrivenSubscriptionUpdates(t *testing.T) {
	contract := DefaultContract()
	contract.Subscriptions.UpdateSource = SubscriptionUpdateSourceProcessMessage
	contract.Subscriptions.ProcessMessagesMayBroadcast = true

	err := ValidateContract(contract)
	if !errors.Is(err, ErrUnsupportedSubscriptionSemantics) {
		t.Fatalf("ValidateContract error = %v, want ErrUnsupportedSubscriptionSemantics", err)
	}
}

func assertLifecycleSteps(t *testing.T, hook LifecycleHook, got []LifecycleStep, want ...LifecycleStep) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s lifecycle steps = %#v, want %#v", hook, got, want)
	}
}
