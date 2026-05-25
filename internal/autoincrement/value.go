// Package autoincrement contains shared helpers for schema-owned
// autoincrement column behavior.
package autoincrement

import (
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// ValueAsUint64 returns the non-negative integer represented by v for an
// autoincrement column kind.
func ValueAsUint64(v types.Value, kind schema.ValueKind) (uint64, bool) {
	if v.IsNull() {
		return 0, false
	}
	switch kind {
	case schema.KindInt8:
		return signedValueAsUint64(int64(v.AsInt8()))
	case schema.KindInt16:
		return signedValueAsUint64(int64(v.AsInt16()))
	case schema.KindInt32:
		return signedValueAsUint64(int64(v.AsInt32()))
	case schema.KindInt64:
		return signedValueAsUint64(v.AsInt64())
	case schema.KindUint8:
		return uint64(v.AsUint8()), true
	case schema.KindUint16:
		return uint64(v.AsUint16()), true
	case schema.KindUint32:
		return uint64(v.AsUint32()), true
	case schema.KindUint64:
		return v.AsUint64(), true
	default:
		return 0, false
	}
}

func signedValueAsUint64(n int64) (uint64, bool) {
	if n < 0 {
		return 0, false
	}
	return uint64(n), true
}
