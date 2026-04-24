package protocol

import "github.com/ponchione/shunter/types"

// Query is the structured query sent in Subscribe / OneOffQuery
// messages (SPEC-005 §7.1.1).
type Query struct {
	TableName  string
	Predicates []Predicate
}

// Predicate is a single comparison filter inside a Query (SPEC-005
// §7.1.1). The Value side reuses the SPEC-001 Value representation so
// decoding feeds directly into the subscription predicate path.
type Predicate struct {
	Column string
	Op     string
	Value  types.Value
}

// SubscriptionUpdate carries per-table inserts/deletes for a client query
// inside transaction update and multi-applied server envelopes (SPEC-005 §8).
// QueryID is the client-chosen subscription-set identifier from the original
// SubscribeSingle / SubscribeMulti request; the subscription manager's
// internal SubscriptionID is intentionally not exposed on the wire. Inserts
// and Deletes are encoded RowList payloads. TableID is intentionally not on
// the wire; name is the client-side identifier.
type SubscriptionUpdate struct {
	QueryID   uint32
	TableName string
	Inserts   []byte
	Deletes   []byte
}
