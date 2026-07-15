package protocol

import (
	"errors"
	"fmt"
)

const (
	// DefaultSQLQueryMaxRows bounds rows returned by hosted one-off and declared
	// queries when the host does not provide a narrower limit.
	DefaultSQLQueryMaxRows = 100_000
	// DefaultSQLQueryMaxBytes bounds the encoded RowList payload returned by
	// hosted one-off and declared queries.
	DefaultSQLQueryMaxBytes = 64 << 20
	// DefaultSQLQueryMaxWork bounds candidate rows and index probes performed
	// by one-off and declared multi-way joins.
	DefaultSQLQueryMaxWork = 1_000_000
)

// ErrSQLQueryResultLimit reports that a query exceeded a host-controlled result
// boundary. Client-supplied LIMIT clauses do not override this boundary.
var ErrSQLQueryResultLimit = errors.New("protocol: SQL query result limit exceeded")

// ErrSQLQueryWorkLimit reports that a query exhausted its host-controlled
// execution-work boundary before producing a result.
var ErrSQLQueryWorkLimit = errors.New("protocol: SQL query work limit exceeded")

// SQLQueryLimits bounds one-off and declared SQL query results and multi-way
// join execution work. Zero values use the hosted defaults when passed through
// NormalizeSQLQueryLimits.
type SQLQueryLimits struct {
	MaxRows  int
	MaxBytes int
	MaxWork  int

	responseMaxBytes  int
	responseMessageID string
}

// BindSQLQueryResponseLimit reserves the exact OneOffQueryResponse envelope
// overhead from the query's RowList budget. It is used by protocol handlers;
// local query callers retain the configured payload-only limit.
func BindSQLQueryResponseLimit(limits SQLQueryLimits, conn *Conn, messageID []byte) SQLQueryLimits {
	if conn == nil || conn.opts == nil || conn.opts.MaxOutboundMessageSize <= 0 {
		return limits
	}
	limits.responseMaxBytes = conn.opts.MaxOutboundMessageSize
	limits.responseMessageID = string(messageID)
	return limits
}

// NormalizeSQLQueryLimits validates limits and fills zero values with hosted
// defaults.
func NormalizeSQLQueryLimits(limits SQLQueryLimits) (SQLQueryLimits, error) {
	if limits.MaxRows < 0 {
		return SQLQueryLimits{}, fmt.Errorf("SQL query max rows must not be negative")
	}
	if limits.MaxBytes < 0 {
		return SQLQueryLimits{}, fmt.Errorf("SQL query max bytes must not be negative")
	}
	if limits.MaxWork < 0 {
		return SQLQueryLimits{}, fmt.Errorf("SQL query max work must not be negative")
	}
	if limits.MaxRows == 0 {
		limits.MaxRows = DefaultSQLQueryMaxRows
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = DefaultSQLQueryMaxBytes
	}
	if limits.MaxWork == 0 {
		limits.MaxWork = DefaultSQLQueryMaxWork
	}
	return limits, nil
}

func applyOneOffResponseBudget(limits SQLQueryLimits, tableName string) (SQLQueryLimits, error) {
	if limits.responseMaxBytes <= 0 {
		return limits, nil
	}
	base := OneOffQueryResponse{
		MessageID: []byte(limits.responseMessageID),
		Tables:    []OneOffTable{{TableName: tableName}},
	}
	baseSize, err := ValidateServerMessageSize(base, 0)
	if err != nil {
		return SQLQueryLimits{}, err
	}
	rowListBytes := limits.responseMaxBytes - baseSize
	if rowListBytes < 4 {
		return SQLQueryLimits{}, fmt.Errorf(
			"%w: response_bytes=%d cap=%d",
			ErrSQLQueryResultLimit,
			baseSize+4,
			limits.responseMaxBytes,
		)
	}
	if limits.MaxBytes > rowListBytes {
		limits.MaxBytes = rowListBytes
	}
	return limits, nil
}

func validateSQLQueryResultRowLimit(result SQLQueryResult, limits SQLQueryLimits) error {
	if limits.MaxRows > 0 && len(result.Rows) > limits.MaxRows {
		return fmt.Errorf("%w: rows=%d cap=%d", ErrSQLQueryResultLimit, len(result.Rows), limits.MaxRows)
	}
	return nil
}
