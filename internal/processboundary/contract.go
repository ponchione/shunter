package processboundary

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

var (
	ErrMissingTransactionSemantics      = errors.New("processboundary: missing transaction semantics")
	ErrUnsupportedTransactionMode       = errors.New("processboundary: unsupported transaction mode")
	ErrUnsupportedTransactionDecision   = errors.New("processboundary: unsupported transaction decision")
	ErrUnsupportedSubscriptionSemantics = errors.New("processboundary: unsupported subscription semantics")
	ErrInvalidContract                  = errors.New("processboundary: invalid contract")
	ErrInvalidInvocationStatus          = errors.New("processboundary: invalid invocation status")
	ErrInvalidFailureClassification     = errors.New("processboundary: invalid failure classification")
)

type Decision string

const (
	DecisionDeferred Decision = "deferred"
	DecisionKept     Decision = "kept"
	DecisionRejected Decision = "rejected"
)

type InvocationKind string

const (
	InvocationKindReducer   InvocationKind = "reducer"
	InvocationKindLifecycle InvocationKind = "lifecycle"
)

type InvocationRequest struct {
	Kind      InvocationKind `json:"kind"`
	Module    string         `json:"module"`
	Name      string         `json:"name"`
	Args      []byte         `json:"args"`
	RequestID uint32         `json:"request_id"`
	Caller    Caller         `json:"caller"`
}

type Caller struct {
	Identity            types.Identity     `json:"identity"`
	ConnectionID        types.ConnectionID `json:"connection_id"`
	Permissions         []string           `json:"permissions"`
	AllowAllPermissions bool               `json:"allow_all_permissions"`
}

type InvocationStatus string

const (
	InvocationStatusCommitted       InvocationStatus = "committed"
	InvocationStatusUserError       InvocationStatus = "user_error"
	InvocationStatusPanic           InvocationStatus = "panic"
	InvocationStatusPermission      InvocationStatus = "permission_denied"
	InvocationStatusInternalFailure InvocationStatus = "internal_failure"
	InvocationStatusBoundaryFailure InvocationStatus = "boundary_failure"
)

type FailureClass string

const (
	FailureClassNone       FailureClass = ""
	FailureClassUser       FailureClass = "user"
	FailureClassPanic      FailureClass = "panic"
	FailureClassPermission FailureClass = "permission"
	FailureClassInternal   FailureClass = "internal"
	FailureClassBoundary   FailureClass = "boundary"
)

type Failure struct {
	Class   FailureClass `json:"class"`
	Message string       `json:"message"`
}

type TransactionMode string

const (
	TransactionModeUnsupported TransactionMode = "unsupported"
	TransactionModeHostOwned   TransactionMode = "host_owned"
)

type TransactionDecision string

const (
	TransactionDecisionUnsupported TransactionDecision = "unsupported"
	TransactionDecisionCommit      TransactionDecision = "commit"
	TransactionDecisionRollback    TransactionDecision = "rollback"
)

type TransactionOutcome struct {
	Mode     TransactionMode     `json:"mode"`
	Decision TransactionDecision `json:"decision"`
	Reason   string              `json:"reason"`
}

type InvocationResponse struct {
	Status      InvocationStatus   `json:"status"`
	Output      []byte             `json:"output"`
	Error       string             `json:"error"`
	TxID        types.TxID         `json:"tx_id"`
	Failure     Failure            `json:"failure"`
	Transaction TransactionOutcome `json:"transaction"`
}

func UserFailure(message string) InvocationResponse {
	return InvocationResponse{
		Status: InvocationStatusUserError,
		Error:  message,
		Failure: Failure{
			Class:   FailureClassUser,
			Message: message,
		},
		Transaction: UnsupportedTransaction("user reducer failure does not commit host state"),
	}
}

func BoundaryFailure(message string) InvocationResponse {
	return InvocationResponse{
		Status: InvocationStatusBoundaryFailure,
		Error:  message,
		Failure: Failure{
			Class:   FailureClassBoundary,
			Message: message,
		},
		Transaction: UnsupportedTransaction("boundary failure occurs outside host transaction semantics"),
	}
}

func UnsupportedTransaction(reason string) TransactionOutcome {
	return TransactionOutcome{
		Mode:     TransactionModeUnsupported,
		Decision: TransactionDecisionUnsupported,
		Reason:   reason,
	}
}

func (r InvocationResponse) IsUserFailure() bool {
	return r.Status == InvocationStatusUserError && r.Failure.Class == FailureClassUser
}

func (r InvocationResponse) IsBoundaryFailure() bool {
	return r.Status == InvocationStatusBoundaryFailure && r.Failure.Class == FailureClassBoundary
}

func ValidateInvocationResponse(resp InvocationResponse) error {
	expectedFailure, ok := failureClassForInvocationStatus(resp.Status)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidInvocationStatus, resp.Status)
	}
	if resp.Failure.Class != expectedFailure {
		return fmt.Errorf(
			"%w: status %q has failure class %q, want %q",
			ErrInvalidFailureClassification,
			resp.Status,
			resp.Failure.Class,
			expectedFailure,
		)
	}
	return validateTransactionOutcome(resp.Transaction)
}

func failureClassForInvocationStatus(status InvocationStatus) (FailureClass, bool) {
	switch status {
	case InvocationStatusCommitted:
		return FailureClassNone, true
	case InvocationStatusUserError:
		return FailureClassUser, true
	case InvocationStatusPanic:
		return FailureClassPanic, true
	case InvocationStatusPermission:
		return FailureClassPermission, true
	case InvocationStatusInternalFailure:
		return FailureClassInternal, true
	case InvocationStatusBoundaryFailure:
		return FailureClassBoundary, true
	default:
		return FailureClassNone, false
	}
}

type Contract struct {
	Decision      Decision                            `json:"decision"`
	Transactions  TransactionPolicy                   `json:"transactions"`
	Lifecycle     map[LifecycleHook]LifecycleContract `json:"lifecycle"`
	Subscriptions SubscriptionPolicy                  `json:"subscriptions"`
	Reason        string                              `json:"reason"`
}

type TransactionPolicy struct {
	Mode               TransactionMode       `json:"mode"`
	SupportedDecisions []TransactionDecision `json:"supported_decisions"`
	Reason             string                `json:"reason"`
}

type LifecycleHook string

const (
	LifecycleOnConnect    LifecycleHook = "OnConnect"
	LifecycleOnDisconnect LifecycleHook = "OnDisconnect"
)

type LifecycleStep string

const (
	LifecycleStepInsertClient     LifecycleStep = "insert_client"
	LifecycleStepInvokeReducer    LifecycleStep = "invoke_reducer"
	LifecycleStepCommitOrRollback LifecycleStep = "commit_or_rollback"
	LifecycleStepCleanupClient    LifecycleStep = "cleanup_client"
	LifecycleStepCommitCleanup    LifecycleStep = "commit_cleanup"
)

type LifecycleFailureBehavior string

const (
	LifecycleFailureRejectConnectionRollback LifecycleFailureBehavior = "reject_connection_rollback"
	LifecycleFailureCleanupStillCommits      LifecycleFailureBehavior = "cleanup_still_commits"
)

type LifecycleContract struct {
	Ordering        []LifecycleStep          `json:"ordering"`
	FailureBehavior LifecycleFailureBehavior `json:"failure_behavior"`
}

type SubscriptionUpdateSource string

const (
	SubscriptionUpdateSourceCommittedState SubscriptionUpdateSource = "committed_state"
	SubscriptionUpdateSourceProcessMessage SubscriptionUpdateSource = "process_message"
)

type SubscriptionPolicy struct {
	UpdateSource                SubscriptionUpdateSource `json:"update_source"`
	ProcessMessagesMayBroadcast bool                     `json:"process_messages_may_broadcast"`
}

func DefaultContract() Contract {
	return Contract{
		Decision: DecisionDeferred,
		Transactions: TransactionPolicy{
			Mode:               TransactionModeUnsupported,
			SupportedDecisions: []TransactionDecision{TransactionDecisionUnsupported},
			Reason:             "store.Transaction and rollback semantics are host-local Go object semantics",
		},
		Lifecycle: map[LifecycleHook]LifecycleContract{
			LifecycleOnConnect: {
				Ordering: []LifecycleStep{
					LifecycleStepInsertClient,
					LifecycleStepInvokeReducer,
					LifecycleStepCommitOrRollback,
				},
				FailureBehavior: LifecycleFailureRejectConnectionRollback,
			},
			LifecycleOnDisconnect: {
				Ordering: []LifecycleStep{
					LifecycleStepInvokeReducer,
					LifecycleStepCleanupClient,
					LifecycleStepCommitCleanup,
				},
				FailureBehavior: LifecycleFailureCleanupStillCommits,
			},
		},
		Subscriptions: SubscriptionPolicy{
			UpdateSource: SubscriptionUpdateSourceCommittedState,
		},
		Reason: "out-of-process module execution is deferred until transaction mutation and subscription semantics have a dedicated design",
	}
}

func ValidateContract(contract Contract) error {
	if contract.Decision == "" {
		return fmt.Errorf("%w: decision is required", ErrInvalidContract)
	}
	if !isKnownDecision(contract.Decision) {
		return fmt.Errorf("%w: decision %q", ErrInvalidContract, contract.Decision)
	}
	if err := validateTransactionPolicy(contract.Transactions); err != nil {
		return err
	}
	if contract.Subscriptions.UpdateSource != SubscriptionUpdateSourceCommittedState ||
		contract.Subscriptions.ProcessMessagesMayBroadcast {
		return ErrUnsupportedSubscriptionSemantics
	}
	return validateLifecycle(contract.Lifecycle)
}

func validateLifecycle(lifecycle map[LifecycleHook]LifecycleContract) error {
	for hook := range lifecycle {
		if hook != LifecycleOnConnect && hook != LifecycleOnDisconnect {
			return fmt.Errorf("%w: lifecycle hook %q", ErrInvalidContract, hook)
		}
	}
	for _, hook := range []LifecycleHook{LifecycleOnConnect, LifecycleOnDisconnect} {
		spec, ok := lifecycle[hook]
		if !ok {
			return fmt.Errorf("%w: %s lifecycle contract missing", ErrInvalidContract, hook)
		}
		if len(spec.Ordering) == 0 {
			return fmt.Errorf("%w: %s lifecycle ordering missing", ErrInvalidContract, hook)
		}
		for _, step := range spec.Ordering {
			if !isKnownLifecycleStep(step) {
				return fmt.Errorf("%w: %s lifecycle step %q", ErrInvalidContract, hook, step)
			}
		}
		if spec.FailureBehavior == "" {
			return fmt.Errorf("%w: %s lifecycle failure behavior missing", ErrInvalidContract, hook)
		}
		if !isKnownLifecycleFailureBehavior(spec.FailureBehavior) {
			return fmt.Errorf("%w: %s lifecycle failure behavior %q", ErrInvalidContract, hook, spec.FailureBehavior)
		}
	}
	return nil
}

func validateTransactionOutcome(outcome TransactionOutcome) error {
	if outcome.Mode == "" || outcome.Decision == "" {
		return ErrMissingTransactionSemantics
	}
	switch outcome.Mode {
	case TransactionModeUnsupported:
		if outcome.Decision != TransactionDecisionUnsupported {
			return ErrUnsupportedTransactionDecision
		}
	case TransactionModeHostOwned:
		if outcome.Decision != TransactionDecisionCommit && outcome.Decision != TransactionDecisionRollback {
			return ErrUnsupportedTransactionDecision
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedTransactionMode, outcome.Mode)
	}
	return nil
}

func validateTransactionPolicy(policy TransactionPolicy) error {
	if policy.Mode == "" {
		return ErrMissingTransactionSemantics
	}
	switch policy.Mode {
	case TransactionModeUnsupported:
		if !hasOnlyUnsupportedTransactionDecision(policy.SupportedDecisions) {
			return ErrUnsupportedTransactionDecision
		}
	case TransactionModeHostOwned:
		if len(policy.SupportedDecisions) == 0 {
			return ErrMissingTransactionSemantics
		}
		for _, decision := range policy.SupportedDecisions {
			if decision != TransactionDecisionCommit && decision != TransactionDecisionRollback {
				return ErrUnsupportedTransactionDecision
			}
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedTransactionMode, policy.Mode)
	}
	return nil
}

func hasOnlyUnsupportedTransactionDecision(decisions []TransactionDecision) bool {
	return len(decisions) == 1 && decisions[0] == TransactionDecisionUnsupported
}

func isKnownDecision(decision Decision) bool {
	switch decision {
	case DecisionDeferred, DecisionKept, DecisionRejected:
		return true
	default:
		return false
	}
}

func isKnownLifecycleStep(step LifecycleStep) bool {
	switch step {
	case LifecycleStepInsertClient,
		LifecycleStepInvokeReducer,
		LifecycleStepCommitOrRollback,
		LifecycleStepCleanupClient,
		LifecycleStepCommitCleanup:
		return true
	default:
		return false
	}
}

func isKnownLifecycleFailureBehavior(behavior LifecycleFailureBehavior) bool {
	switch behavior {
	case LifecycleFailureRejectConnectionRollback, LifecycleFailureCleanupStillCommits:
		return true
	default:
		return false
	}
}
