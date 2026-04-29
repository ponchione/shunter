package types

import (
	"encoding/binary"
	"hash"
	"hash/fnv"
)

// ProductValue is an ordered, schema-aligned list of column values.
// Index i corresponds to column i in the table's ColumnSchema.
type ProductValue []Value

// Equal returns true if pv and other have the same length and element-wise equal values.
func (pv ProductValue) Equal(other ProductValue) bool {
	if len(pv) != len(other) {
		return false
	}
	for i := range pv {
		if !pv[i].Equal(other[i]) {
			return false
		}
	}
	return true
}

// Hash feeds a length-prefixed canonical representation of each column into h.
// Format per column: kind_byte + payload_len_u32 + payload_bytes.
// The length prefix prevents concatenation ambiguity (e.g. ("a","bc") vs ("ab","c")).
func (pv ProductValue) Hash(h hash.Hash64) {
	var buf [4]byte
	for _, v := range pv {
		h.Write([]byte{byte(v.kind)})
		binary.BigEndian.PutUint32(buf[:], v.payloadLen())
		h.Write(buf[:])
		v.writePayload(h)
	}
}

// Hash64 returns a hash using fnv64a.
func (pv ProductValue) Hash64() uint64 {
	h := fnv.New64a()
	pv.Hash(h)
	return h.Sum64()
}

// Copy returns a deep copy. Bytes values get their own slice; strings share
// underlying memory (Go strings are immutable).
func (pv ProductValue) Copy() ProductValue {
	if pv == nil {
		return nil
	}
	cp := make(ProductValue, len(pv))
	for i, v := range pv {
		if v.kind == KindBytes {
			cp[i] = NewBytes(v.buf)
		} else {
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
