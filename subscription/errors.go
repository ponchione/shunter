package subscription

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/schema"
)

// Validation errors (Story 1.2).
var (
	// ErrTooManyTables — predicate references more than two tables.
	ErrTooManyTables = errors.New("subscription: predicate references more than two tables")
	// ErrUnindexedJoin — Join predicate join columns lack an index on either side.
	ErrUnindexedJoin = errors.New("subscription: join column has no index on either side")
	// ErrInvalidPredicate — type mismatch, non-literal reference, or structural error.
	ErrInvalidPredicate = errors.New("subscription: invalid predicate")
	// ErrTableNotFound — predicate references a table that is not registered.
	ErrTableNotFound = errors.New("subscription: table not found")
	// ErrColumnNotFound is re-exported from SPEC-006 §13 so subscription
	// predicate validation and the store integrity path share one sentinel
	// value for errors.Is across package boundaries.
	ErrColumnNotFound = schema.ErrColumnNotFound
)

// Registration errors (Story 4.2 / 4.5).
var (
	// ErrSubscriptionQuota classifies admission and snapshot resource-limit
	// rejections separately from predicate and runtime failures.
	ErrSubscriptionQuota = errors.New("subscription: quota exceeded")
	// ErrSubscriptionQueryLimit reports too many predicates in one set.
	ErrSubscriptionQueryLimit = errors.New("subscription: query count limit exceeded")
	// ErrSubscriptionSetLimit reports too many active sets on one connection.
	ErrSubscriptionSetLimit = errors.New("subscription: active set limit exceeded")
	// ErrSubscriptionCountLimit reports too many active internal subscriptions
	// on one connection.
	ErrSubscriptionCountLimit = errors.New("subscription: active subscription limit exceeded")
	// ErrInitialRowLimit — initial snapshot exceeded the configured row limit.
	ErrInitialRowLimit = errors.New("subscription: initial row limit exceeded")
	// ErrSnapshotByteLimit reports an initial or final snapshot whose encoded
	// row-list data exceeds the configured aggregate byte limit.
	ErrSnapshotByteLimit = errors.New("subscription: snapshot byte limit exceeded")
	// ErrMultiJoinLimit — live multi-way join exceeds configured production limits.
	ErrMultiJoinLimit = errors.New("subscription: multi-way join limit exceeded")
	// ErrInitialQuery wraps initial-snapshot evaluation failures.
	ErrInitialQuery = errors.New("initial query")
	// ErrFinalQuery wraps final-delta evaluation failures.
	ErrFinalQuery = errors.New("final query")
	// ErrSubscriptionNotFound — unknown query ID/subscription set for unregister.
	ErrSubscriptionNotFound = errors.New("subscription: subscription not found")
	// ErrSubscriptionIDOverflow — the manager exhausted its internal
	// per-process SubscriptionID space.
	ErrSubscriptionIDOverflow = errors.New("subscription: subscription id overflow")
	// ErrJoinIndexUnresolved — validation confirmed a join-side index exists
	// but the runtime IndexResolver could not produce an IndexID for it (or
	// the manager was constructed without a resolver). This is a contract
	// violation between schema and resolver, not a user-facing predicate
	// error. Bootstrap must hard-fail here rather than return silent empty
	// rows (SPEC-004 §4.1; PHASE-5-DEFERRED §D).
	ErrJoinIndexUnresolved = errors.New("subscription: join index unresolved by resolver")
)

type quotaError struct {
	cause    error
	resource string
	used     int
	limit    int
}

func (e *quotaError) Error() string {
	return fmt.Sprintf("%s: %s=%d cap=%d", e.cause, e.resource, e.used, e.limit)
}

func (e *quotaError) Unwrap() []error {
	return []error{ErrSubscriptionQuota, e.cause}
}

// NewQuotaError returns a stable, classifiable subscription quota error.
func NewQuotaError(cause error, resource string, used, limit int) error {
	if cause == nil {
		cause = ErrSubscriptionQuota
	}
	return &quotaError{cause: cause, resource: resource, used: used, limit: limit}
}

// Evaluation errors (Story 4.5 / 5.1).
var (
	// ErrSubscriptionEval — evaluation failure for a subscription
	// (corrupted index, type mismatch, etc.).
	ErrSubscriptionEval = errors.New("subscription: evaluation failed")
)

// Delivery errors (Story 6.1 / 6.3).
var (
	// ErrSendBufferFull — client outbound buffer is full, client should be dropped.
	ErrSendBufferFull = errors.New("subscription: client send buffer full")
	// ErrSendConnGone — connection not found, client already disconnected.
	ErrSendConnGone = errors.New("subscription: connection not found for delivery")
	// ErrSendEncodeFailed — delivery failed before send while encoding a
	// protocol payload.
	ErrSendEncodeFailed = errors.New("subscription: encode failed for delivery")
)
