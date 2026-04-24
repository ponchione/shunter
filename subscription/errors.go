package subscription

import (
	"errors"

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
	// ErrInitialRowLimit — initial snapshot exceeded the configured row limit.
	ErrInitialRowLimit = errors.New("subscription: initial row limit exceeded")
	// ErrInitialQuery — wraps any error surfaced by the initial-snapshot
	// evaluation inside RegisterSet so the protocol-side adapter can
	// discriminate it from admission-time (validation / duplicate) errors
	// and mirror the reference `DBError::WithSql` suffix shape
	// (`reference/SpacetimeDB/crates/core/src/error.rs:140`,
	// `module_subscription_actor.rs:672` SubscribeSingle initial-eval).
	// Concrete cause (e.g. ErrInitialRowLimit) remains `errors.Is`-unwrappable.
	ErrInitialQuery = errors.New("initial query")
	// ErrFinalQuery — wraps any error surfaced by the final-delta
	// evaluation inside UnregisterSet so the protocol-side adapter can
	// apply the reference `DBError::WithSql` suffix on the
	// UnsubscribeSingle path (`module_subscription_actor.rs:756` via
	// `return_on_err_with_sql!`) while keeping the UnsubscribeMulti path
	// raw (`:836` via `return_on_err!`). Concrete cause (e.g.
	// ErrInitialRowLimit) remains `errors.Is`-unwrappable.
	ErrFinalQuery = errors.New("final query")
	// ErrSubscriptionNotFound — unknown query ID/subscription set for unregister.
	ErrSubscriptionNotFound = errors.New("subscription: subscription not found")
	// ErrJoinIndexUnresolved — validation confirmed a join-side index exists
	// but the runtime IndexResolver could not produce an IndexID for it (or
	// the manager was constructed without a resolver). This is a contract
	// violation between schema and resolver, not a user-facing predicate
	// error. Bootstrap must hard-fail here rather than return silent empty
	// rows (SPEC-004 §4.1; PHASE-5-DEFERRED §D).
	ErrJoinIndexUnresolved = errors.New("subscription: join index unresolved by resolver")
)

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
)
