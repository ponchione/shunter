package types

import (
	"encoding/binary"
	"hash"
	"slices"
)

// ProductValue is an ordered, schema-aligned list of column values.
// Index i corresponds to column i in the table's ColumnSchema.
type ProductValue []Value

// Equal returns true if pv and other have the same length and element-wise equal values.
func (pv ProductValue) Equal(other ProductValue) bool {
	return slices.EqualFunc(pv, other, Value.Equal)
}

// Hash feeds a length-prefixed canonical representation of each column into h.
// Format per column: kind_byte + null_marker + payload_len_u32 + payload_bytes.
// The length prefix prevents concatenation ambiguity (e.g. ("a","bc") vs ("ab","c")).
func (pv ProductValue) Hash(h hash.Hash64) {
	var buf [4]byte
	for _, v := range pv {
		h.Write([]byte{byte(v.kind)})
		if v.isNull {
			h.Write([]byte{0})
			binary.BigEndian.PutUint32(buf[:], 0)
			h.Write(buf[:])
			continue
		}
		h.Write([]byte{1})
		binary.BigEndian.PutUint32(buf[:], v.payloadLen())
		h.Write(buf[:])
		v.writePayload(h)
	}
}

// Hash64 returns a hash using fnv64a.
func (pv ProductValue) Hash64() uint64 {
	sum := fnv64aOffset
	for _, v := range pv {
		sum = fnv64aWriteByte(sum, byte(v.kind))
		if v.isNull {
			sum = fnv64aWriteByte(sum, 0)
			sum = fnv64aWriteUint32BE(sum, 0)
			continue
		}
		sum = fnv64aWriteByte(sum, 1)
		sum = fnv64aWriteUint32BE(sum, v.payloadLen())
		sum = v.hashPayload64Into(sum)
	}
	return sum
}

// Copy returns a deep copy. Slice-backed values get their own slices; strings
// share underlying memory (Go strings are immutable).
func (pv ProductValue) Copy() ProductValue {
	if pv == nil {
		return nil
	}
	cp := make(ProductValue, len(pv))
	for i, v := range pv {
		if v.IsNull() {
			cp[i] = v
			continue
		}
		switch v.kind {
		case KindBytes:
			cp[i] = NewBytes(v.buf)
		case KindJSON:
			cp[i] = Value{kind: KindJSON, buf: slices.Clone(v.buf)}
		case KindArrayString:
			cp[i] = NewArrayString(v.strArr)
		default:
			cp[i] = v
		}
	}
	return cp
}

// CopyProductValues returns detached copies of every row in rows.
func CopyProductValues(rows []ProductValue) []ProductValue {
	if len(rows) == 0 {
		return nil
	}
	out := make([]ProductValue, len(rows))
	for i, row := range rows {
		out[i] = row.Copy()
	}
	return out
}
