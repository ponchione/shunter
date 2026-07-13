package protocol

import (
	"context"
)

// handleSubscribeMulti validates all SQL queries and submits them atomically.
// The receipt timestamp covers local validation and executor admission.
func handleSubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	handleSubscribeMultiWithVisibility(ctx, conn, msg, executor, sl, nil)
}

func handleSubscribeMultiWithVisibility(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
) {
	handleSubscribeMultiWithVisibilityAndLimit(ctx, conn, msg, executor, sl, visibilityFilters, DefaultSubscriptionMaxQueriesPerSet)
}

func handleSubscribeMultiWithVisibilityAndLimit(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
	visibilityFilters []VisibilityFilter,
	maxQueriesPerSet int,
) {
	handleSubscribeSetWithVisibilityAndLimit(ctx, conn, msg.RequestID, msg.QueryID, SubscriptionSetVariantMulti, msg.QueryStrings, "", executor, sl, visibilityFilters, maxQueriesPerSet)
}
