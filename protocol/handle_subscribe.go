package protocol

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// SchemaLookup resolves table names to their schema-level identifiers
// and column metadata. The host wires the concrete implementation;
// the protocol layer uses it during subscribe/query handling to
// validate table + column references before forwarding to the executor.
type SchemaLookup interface {
	TableByName(name string) (schema.TableID, *schema.TableSchema, bool)
}

// NormalizePredicates converts a slice of wire-level Predicate (column
// name + value) into a single subscription.Predicate tree suitable for
// the evaluator. Empty predicates produce AllRows; a single predicate
// produces ColEq; multiple predicates are folded left into nested And
// nodes.
func NormalizePredicates(
	tableID schema.TableID,
	ts *schema.TableSchema,
	preds []Predicate,
) (subscription.Predicate, error) {
	if len(preds) == 0 {
		return subscription.AllRows{Table: tableID}, nil
	}

	eqs := make([]subscription.Predicate, 0, len(preds))
	for _, p := range preds {
		col, ok := ts.Column(p.Column)
		if !ok {
			return nil, fmt.Errorf("unknown column %q on table %q", p.Column, ts.Name)
		}
		eqs = append(eqs, subscription.ColEq{
			Table:  tableID,
			Column: types.ColID(col.Index),
			Value:  p.Value,
		})
	}

	if len(eqs) == 1 {
		return eqs[0], nil
	}

	result := subscription.And{Left: eqs[0], Right: eqs[1]}
	for i := 2; i < len(eqs); i++ {
		result = subscription.And{Left: result, Right: eqs[i]}
	}
	return result, nil
}

// handleSubscribe processes an incoming SubscribeMsg: reserves the
// subscription id on the connection, resolves and validates the query
// against the schema, normalizes predicates, and submits the
// subscription to the executor. On any failure the subscription is
// released and a SubscriptionError is sent to the client.
func handleSubscribe(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	subID := msg.SubscriptionID

	if err := conn.Subscriptions.Reserve(subID); err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          err.Error(),
		})
		return
	}

	releaseOnError := true
	defer func() {
		if releaseOnError {
			_ = conn.Subscriptions.Remove(subID)
		}
	}()

	tableID, ts, ok := sl.TableByName(msg.Query.TableName)
	if !ok {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          fmt.Sprintf("unknown table %q", msg.Query.TableName),
		})
		return
	}

	pred, err := NormalizePredicates(tableID, ts, msg.Query.Predicates)
	if err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          err.Error(),
		})
		return
	}

	respCh := make(chan SubscriptionCommandResponse, 1)
	if err := executor.RegisterSubscription(ctx, RegisterSubscriptionRequest{
		ConnID:         conn.ID,
		SubscriptionID: subID,
		RequestID:      msg.RequestID,
		Predicate:      pred,
		ResponseCh:     respCh,
	}); err != nil {
		sendError(conn, SubscriptionError{
			RequestID:      msg.RequestID,
			SubscriptionID: subID,
			Error:          "executor unavailable: " + err.Error(),
		})
		return
	}

	releaseOnError = false
	watchSubscribeResponse(conn, respCh)
}
