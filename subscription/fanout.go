package subscription

import "github.com/ponchione/shunter/types"

// FanOutMessage is the handoff payload between the executor's evaluation
// loop and the fan-out worker (SPEC-004 §8.1 / Story 6.1).
//
// This file is the minimal E6.1 "contract slice" that Story 5.1 depends on.
// The fan-out worker, backpressure policy, and actual protocol delivery live
// in Epic 6's remaining stories and Phase 8 of the execution plan — none of
// them land here.
type FanOutMessage struct {
	// TxID identifies the transaction this payload came from.
	TxID types.TxID

	// TxDurable becomes ready when the transaction is durable. The fan-out
	// worker consumes readiness when the recipient requires confirmed reads.
	// Nil is allowed and means "treat as already durable" (fast reads only).
	TxDurable <-chan types.TxID

	// Fanout holds the per-connection subscription updates produced for
	// this commit.
	Fanout CommitFanout

	// Errors holds per-connection subscription evaluation failures that should
	// be delivered as SubscriptionError messages before cleanup. Delivery wiring
	// is owned by the deferred Epic 6 / SPEC-005 integration path.
	Errors map[types.ConnectionID][]SubscriptionError

	// CallerConnID and CallerResult are optional metadata for reducer-
	// originated commits. When present, the caller connection's update
	// slice is routed into the ReducerCallResult path in the delivery
	// layer instead of emitting a standalone TransactionUpdate.
	CallerConnID *types.ConnectionID
	CallerResult *ReducerCallResult
}

// SubscriptionError is the protocol-facing evaluation-failure payload queued
// for clients affected by a broken subscription. SPEC-005 owns wire encoding;
// this package only carries the semantic content across the fan-out seam.
type SubscriptionError struct {
	QueryHash QueryHash
	Predicate string
	Message   string
}

// ReducerCallResult is the caller-side response envelope used by fan-out
// delivery. This forward declaration matches the protocol-owned shape from
// SPEC-005 §8.7 closely enough for the subscription/protocol seam.
type ReducerCallResult struct {
	RequestID         uint32
	Status            uint8
	TxID              types.TxID
	Error             string
	Energy            uint64
	TransactionUpdate []SubscriptionUpdate
}
