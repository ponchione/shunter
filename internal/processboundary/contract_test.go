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

func TestValidateInvocationResponseAcceptsExpectedFailureClasses(t *testing.T) {
	tests := []InvocationResponse{
		{
			Status:      InvocationStatusCommitted,
			Transaction: UnsupportedTransaction("host transaction objects cannot cross the process boundary"),
		},
		{
			Status:      InvocationStatusUserError,
			Failure:     Failure{Class: FailureClassUser, Message: "reducer rejected input"},
			Transaction: UnsupportedTransaction("user reducer failure does not commit host state"),
		},
		{
			Status:      InvocationStatusPanic,
			Failure:     Failure{Class: FailureClassPanic, Message: "panic"},
			Transaction: UnsupportedTransaction("panic does not commit host state"),
		},
		{
			Status:      InvocationStatusPermission,
			Failure:     Failure{Class: FailureClassPermission, Message: "missing permission"},
			Transaction: UnsupportedTransaction("permission denial does not commit host state"),
		},
		{
			Status:      InvocationStatusInternalFailure,
			Failure:     Failure{Class: FailureClassInternal, Message: "internal error"},
			Transaction: UnsupportedTransaction("internal failure does not commit host state"),
		},
		{
			Status:      InvocationStatusBoundaryFailure,
			Failure:     Failure{Class: FailureClassBoundary, Message: "transport closed"},
			Transaction: UnsupportedTransaction("boundary failure occurs outside host transaction semantics"),
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.Status), func(t *testing.T) {
			if err := ValidateInvocationResponse(tt); err != nil {
				t.Fatalf("ValidateInvocationResponse returned error: %v", err)
			}
		})
	}
}

func FuzzValidateInvocationResponseJSON(f *testing.F) {
	for _, resp := range []InvocationResponse{
		{
			Status:      InvocationStatusCommitted,
			Output:      []byte{0x01, 0x02},
			TxID:        7,
			Transaction: UnsupportedTransaction("host transaction objects cannot cross the process boundary"),
		},
		UserFailure("reducer rejected input"),
		{
			Status:      InvocationStatusPanic,
			Error:       "panic",
			Failure:     Failure{Class: FailureClassPanic, Message: "panic"},
			Transaction: UnsupportedTransaction("panic does not commit host state"),
		},
		{
			Status:      InvocationStatusPermission,
			Error:       "missing permission",
			Failure:     Failure{Class: FailureClassPermission, Message: "missing permission"},
			Transaction: UnsupportedTransaction("permission denial does not commit host state"),
		},
		{
			Status:      InvocationStatusInternalFailure,
			Error:       "internal error",
			Failure:     Failure{Class: FailureClassInternal, Message: "internal error"},
			Transaction: UnsupportedTransaction("internal failure does not commit host state"),
		},
		BoundaryFailure("transport closed"),
	} {
		f.Add(mustBoundaryJSON(resp))
	}
	for _, data := range [][]byte{
		[]byte(`{`),
		[]byte(`{"status":"unknown","transaction":{"mode":"unsupported","decision":"unsupported"}}`),
		[]byte(`{"status":"committed","failure":{"class":"user"},"transaction":{"mode":"unsupported","decision":"unsupported"}}`),
		[]byte(`{"status":"user_error","failure":{"class":"boundary"},"transaction":{"mode":"unsupported","decision":"unsupported"}}`),
		[]byte(`{"status":"committed","transaction":{"mode":"unsupported","decision":"commit"}}`),
	} {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		var resp InvocationResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return
		}

		err := ValidateInvocationResponse(resp)
		if err != nil {
			if !isExpectedInvocationValidationError(err) {
				t.Fatalf("ValidateInvocationResponse returned uncategorized error: %v", err)
			}
			return
		}

		expectedFailure, ok := failureClassForInvocationStatus(resp.Status)
		if !ok {
			t.Fatalf("accepted response with unknown status %q", resp.Status)
		}
		if resp.Failure.Class != expectedFailure {
			t.Fatalf("accepted response failure class = %q, want %q for status %q", resp.Failure.Class, expectedFailure, resp.Status)
		}
		if resp.Transaction.Mode == "" || resp.Transaction.Decision == "" {
			t.Fatalf("accepted response without explicit transaction semantics: %#v", resp.Transaction)
		}
		if resp.Transaction.Mode == TransactionModeUnsupported && resp.Transaction.Decision != TransactionDecisionUnsupported {
			t.Fatalf("accepted unsupported transaction with decision %q", resp.Transaction.Decision)
		}

		roundTripData := mustBoundaryJSON(resp)
		var roundTrip InvocationResponse
		if err := json.Unmarshal(roundTripData, &roundTrip); err != nil {
			t.Fatalf("round-trip unmarshal failed: %v", err)
		}
		if !reflect.DeepEqual(roundTrip, resp) {
			t.Fatalf("response JSON round trip changed value:\nobserved=%#v\nexpected=%#v", roundTrip, resp)
		}
		if err := ValidateInvocationResponse(roundTrip); err != nil {
			t.Fatalf("round-tripped accepted response failed validation: %v", err)
		}
	})
}

func TestValidateInvocationResponseRejectsAmbiguousFailureClassification(t *testing.T) {
	tests := []struct {
		name string
		resp InvocationResponse
		want error
	}{
		{
			name: "unknown status",
			resp: InvocationResponse{
				Status:      InvocationStatus("unknown"),
				Transaction: UnsupportedTransaction("unknown status"),
			},
			want: ErrInvalidInvocationStatus,
		},
		{
			name: "committed response with failure class",
			resp: InvocationResponse{
				Status:      InvocationStatusCommitted,
				Failure:     Failure{Class: FailureClassUser, Message: "should not be classified"},
				Transaction: UnsupportedTransaction("host transaction objects cannot cross the process boundary"),
			},
			want: ErrInvalidFailureClassification,
		},
		{
			name: "user error with boundary class",
			resp: InvocationResponse{
				Status:      InvocationStatusUserError,
				Failure:     Failure{Class: FailureClassBoundary, Message: "wrong class"},
				Transaction: UnsupportedTransaction("user reducer failure does not commit host state"),
			},
			want: ErrInvalidFailureClassification,
		},
		{
			name: "permission denial without permission class",
			resp: InvocationResponse{
				Status:      InvocationStatusPermission,
				Transaction: UnsupportedTransaction("permission denial does not commit host state"),
			},
			want: ErrInvalidFailureClassification,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateInvocationResponse(tt.resp); !errors.Is(err, tt.want) {
				t.Fatalf("ValidateInvocationResponse error = %v, want %v", err, tt.want)
			}
		})
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

func FuzzValidateContractJSON(f *testing.F) {
	valid := DefaultContract()
	for _, contract := range []Contract{
		valid,
		{
			Decision: DecisionKept,
			Transactions: TransactionPolicy{
				Mode:               TransactionModeHostOwned,
				SupportedDecisions: []TransactionDecision{TransactionDecisionCommit, TransactionDecisionRollback},
				Reason:             "future host-owned process boundary",
			},
			Lifecycle:     valid.Lifecycle,
			Subscriptions: valid.Subscriptions,
			Reason:        "future contract shape",
		},
	} {
		f.Add(mustBoundaryJSON(contract))
	}
	for _, data := range [][]byte{
		[]byte(`{`),
		[]byte(`{}`),
		[]byte(`{"decision":"deferred","transactions":{"mode":"unsupported","supported_decisions":["commit"]},"subscriptions":{"update_source":"committed_state"},"lifecycle":{"OnConnect":{"ordering":["insert_client"],"failure_behavior":"reject_connection_rollback"},"OnDisconnect":{"ordering":["cleanup_client"],"failure_behavior":"cleanup_still_commits"}}}`),
		[]byte(`{"decision":"deferred","transactions":{"mode":"unsupported","supported_decisions":["unsupported"]},"subscriptions":{"update_source":"process_message","process_messages_may_broadcast":true},"lifecycle":{"OnConnect":{"ordering":["insert_client"],"failure_behavior":"reject_connection_rollback"},"OnDisconnect":{"ordering":["cleanup_client"],"failure_behavior":"cleanup_still_commits"}}}`),
	} {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192 {
			return
		}
		var contract Contract
		if err := json.Unmarshal(data, &contract); err != nil {
			return
		}

		err := ValidateContract(contract)
		if err != nil {
			if !isExpectedContractValidationError(err) {
				t.Fatalf("ValidateContract returned uncategorized error: %v", err)
			}
			return
		}

		if contract.Decision == "" {
			t.Fatal("accepted contract without decision")
		}
		if contract.Transactions.Mode == "" {
			t.Fatal("accepted contract without transaction mode")
		}
		if contract.Transactions.Mode == TransactionModeUnsupported &&
			!hasOnlyUnsupportedTransactionDecision(contract.Transactions.SupportedDecisions) {
			t.Fatalf("accepted unsupported transaction decisions: %#v", contract.Transactions.SupportedDecisions)
		}
		if contract.Subscriptions.UpdateSource != SubscriptionUpdateSourceCommittedState ||
			contract.Subscriptions.ProcessMessagesMayBroadcast {
			t.Fatalf("accepted process-driven subscription semantics: %#v", contract.Subscriptions)
		}
		for _, hook := range []LifecycleHook{LifecycleOnConnect, LifecycleOnDisconnect} {
			spec, ok := contract.Lifecycle[hook]
			if !ok {
				t.Fatalf("accepted contract missing %s lifecycle", hook)
			}
			if len(spec.Ordering) == 0 {
				t.Fatalf("accepted contract missing %s lifecycle ordering", hook)
			}
			if spec.FailureBehavior == "" {
				t.Fatalf("accepted contract missing %s lifecycle failure behavior", hook)
			}
		}

		roundTripData := mustBoundaryJSON(contract)
		var roundTrip Contract
		if err := json.Unmarshal(roundTripData, &roundTrip); err != nil {
			t.Fatalf("contract round-trip unmarshal failed: %v", err)
		}
		if !reflect.DeepEqual(roundTrip, contract) {
			t.Fatalf("contract JSON round trip changed value:\nobserved=%#v\nexpected=%#v", roundTrip, contract)
		}
		if err := ValidateContract(roundTrip); err != nil {
			t.Fatalf("round-tripped accepted contract failed validation: %v", err)
		}
	})
}

func assertLifecycleSteps(t *testing.T, hook LifecycleHook, got []LifecycleStep, want ...LifecycleStep) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s lifecycle steps = %#v, want %#v", hook, got, want)
	}
}

func mustBoundaryJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func isExpectedInvocationValidationError(err error) bool {
	return errors.Is(err, ErrInvalidInvocationStatus) ||
		errors.Is(err, ErrInvalidFailureClassification) ||
		errors.Is(err, ErrMissingTransactionSemantics) ||
		errors.Is(err, ErrUnsupportedTransactionDecision)
}

func isExpectedContractValidationError(err error) bool {
	return errors.Is(err, ErrInvalidContract) ||
		errors.Is(err, ErrMissingTransactionSemantics) ||
		errors.Is(err, ErrUnsupportedTransactionDecision) ||
		errors.Is(err, ErrUnsupportedSubscriptionSemantics)
}
