package types

// RowID uniquely identifies a row within a table.
// Allocated by a per-table monotonic counter; never reused after deletion.
type RowID uint64

// Identity is the 32-byte canonical client identifier.
type Identity [32]byte

// ColID is a zero-based column index within a table schema.
type ColID int

// ConnectionID is a 16-byte opaque identifier for a WebSocket connection.
// All-zeros is the zero value and represents no connection (internal callers).
type ConnectionID [16]byte

// TxID identifies a committed transaction.
// TxID(0) means "no committed transaction" / bootstrap.
type TxID uint64

// SubscriptionID is the server-internal uint32 allocated for one predicate
// registered inside a client-chosen query set. QueryID carries the client
// handle at protocol boundaries.
type SubscriptionID uint32
