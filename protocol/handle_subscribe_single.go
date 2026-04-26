package protocol

import (
	"context"
	"time"

	"github.com/ponchione/shunter/types"
)

// handleSubscribeSingle processes an incoming SubscribeSingleMsg. It
// resolves and validates the wire query against the schema, normalizes
// predicates, and submits the subscription to the executor via the
// set-based seam (len(Predicates)==1). The executor invokes the Reply
// closure synchronously on its own goroutine; the closure enqueues
// either a SubscribeSingleApplied or a SubscriptionError onto the
// connection's outbound channel. Synchronous dispatch here is what
// enforces ADR §9.4 FIFO between Applied and any subsequent fan-out.
//
// Receipt-timestamp seam: `receipt` is captured at handler entry so
// every `TotalHostExecutionDurationMicros` field emitted on this path
// reflects the full admission duration. The receipt is passed to the
// executor via RegisterSubscriptionSetRequest.Receipt for the
// success/executor-error path, and used locally for handler-short-circuit
// paths (compile failure, executor-submit failure).
func handleSubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	receipt := time.Now()
	compiled, err := compileSQLQueryString(msg.QueryString, sl, &conn.Identity, false, false)
	if err != nil {
		sendSubscribeCompileError(conn, receipt, msg.RequestID, msg.QueryID, err, msg.QueryString)
		return
	}
	pred := compiled.Predicate

	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:                  conn.ID,
		QueryID:                 msg.QueryID,
		RequestID:               msg.RequestID,
		Variant:                 SubscriptionSetVariantSingle,
		Predicates:              []any{pred},
		PredicateHashIdentities: []*types.Identity{callerHashIdentity(conn, compiled)},
		Reply:                   makeSubscribeSetReply(conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantSingle),
		Receipt:                 receipt,
		SQLText:                 msg.QueryString,
	}); submitErr != nil {
		sendExecutorUnavailableError(conn, receipt, msg.RequestID, msg.QueryID, submitErr)
		return
	}
}

// elapsedMicros reports the non-zero microsecond delta since receipt.
// A zero measurement is bumped to 1 so downstream consumers can
// distinguish "measured" from the deferred-measurement sentinel (0)
// that error-origin SubscriptionError emitters used before the seam.
func elapsedMicros(receipt time.Time) uint64 {
	us := uint64(time.Since(receipt).Microseconds())
	if us == 0 {
		return 1
	}
	return us
}
