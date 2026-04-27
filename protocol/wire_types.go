package protocol

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
