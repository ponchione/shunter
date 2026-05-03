package protocol

// SubscriptionUpdate carries encoded row inserts/deletes for a client query.
// QueryID is client-chosen; internal SubscriptionID is not exposed.
type SubscriptionUpdate struct {
	QueryID   uint32
	TableName string
	Inserts   []byte
	Deletes   []byte
}
