package schema

import (
	"math"
)

var exportStrings = [...]string{
	KindBool:        "bool",
	KindInt8:        "int8",
	KindUint8:       "uint8",
	KindInt16:       "int16",
	KindUint16:      "uint16",
	KindInt32:       "int32",
	KindUint32:      "uint32",
	KindInt64:       "int64",
	KindUint64:      "uint64",
	KindFloat32:     "float32",
	KindFloat64:     "float64",
	KindString:      "string",
	KindBytes:       "bytes",
	KindInt128:      "int128",
	KindUint128:     "uint128",
	KindInt256:      "int256",
	KindUint256:     "uint256",
	KindTimestamp:   "timestamp",
	KindArrayString: "arrayString",
	KindUUID:        "uuid",
}

// ValueKindExportString returns the lowercase export name for a ValueKind.
func ValueKindExportString(k ValueKind) string {
	if k >= 0 && int(k) < len(exportStrings) {
		return exportStrings[k]
	}
	return ""
}

type intBounds struct {
	min int64
	max uint64
}

var autoIncrBounds = map[ValueKind]intBounds{
	KindInt8:   {min: math.MinInt8, max: math.MaxInt8},
	KindUint8:  {min: 0, max: math.MaxUint8},
	KindInt16:  {min: math.MinInt16, max: math.MaxInt16},
	KindUint16: {min: 0, max: math.MaxUint16},
	KindInt32:  {min: math.MinInt32, max: math.MaxInt32},
	KindUint32: {min: 0, max: math.MaxUint32},
	KindInt64:  {min: math.MinInt64, max: math.MaxInt64},
	KindUint64: {min: 0, max: math.MaxUint64},
}

// AutoIncrementBounds reports whether a ValueKind is eligible for auto-increment
// and returns the representable integer bounds. Non-integer kinds return ok=false.
func AutoIncrementBounds(k ValueKind) (min int64, max uint64, ok bool) {
	b, ok := autoIncrBounds[k]
	if !ok {
		return 0, 0, false
	}
	return b.min, b.max, true
}
