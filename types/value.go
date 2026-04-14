package types

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
	"math"
	"strings"
)

// ValueKind identifies the type of a column value.
type ValueKind int

const (
	KindBool    ValueKind = iota
	KindInt8
	KindUint8
	KindInt16
	KindUint16
	KindInt32
	KindUint32
	KindInt64
	KindUint64
	KindFloat32
	KindFloat64
	KindString
	KindBytes
)

var kindNames = [...]string{
	KindBool:    "Bool",
	KindInt8:    "Int8",
	KindUint8:   "Uint8",
	KindInt16:   "Int16",
	KindUint16:  "Uint16",
	KindInt32:   "Int32",
	KindUint32:  "Uint32",
	KindInt64:   "Int64",
	KindUint64:  "Uint64",
	KindFloat32: "Float32",
	KindFloat64: "Float64",
	KindString:  "String",
	KindBytes:   "Bytes",
}

func (k ValueKind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("ValueKind(%d)", int(k))
}

// Value is a tagged union of all v1 column types.
// Fields not used by the current kind are zero values.
type Value struct {
	kind ValueKind
	b    bool
	i64  int64
	u64  uint64
	f32  float32
	f64  float64
	str  string
	buf  []byte
}

// Kind returns the ValueKind of this Value.
func (v Value) Kind() ValueKind { return v.kind }

// --- Constructors ---

func NewBool(x bool) Value {
	return Value{kind: KindBool, b: x}
}

func NewInt8(x int8) Value {
	return Value{kind: KindInt8, i64: int64(x)}
}

func NewUint8(x uint8) Value {
	return Value{kind: KindUint8, u64: uint64(x)}
}

func NewInt16(x int16) Value {
	return Value{kind: KindInt16, i64: int64(x)}
}

func NewUint16(x uint16) Value {
	return Value{kind: KindUint16, u64: uint64(x)}
}

func NewInt32(x int32) Value {
	return Value{kind: KindInt32, i64: int64(x)}
}

func NewUint32(x uint32) Value {
	return Value{kind: KindUint32, u64: uint64(x)}
}

func NewInt64(x int64) Value {
	return Value{kind: KindInt64, i64: int64(x)}
}

func NewUint64(x uint64) Value {
	return Value{kind: KindUint64, u64: uint64(x)}
}

func NewFloat32(x float32) (Value, error) {
	if math.IsNaN(float64(x)) {
		return Value{}, fmt.Errorf("shunter: NaN is not a valid Float32 value")
	}
	return Value{kind: KindFloat32, f32: x}, nil
}

func NewFloat64(x float64) (Value, error) {
	if math.IsNaN(x) {
		return Value{}, fmt.Errorf("shunter: NaN is not a valid Float64 value")
	}
	return Value{kind: KindFloat64, f64: x}, nil
}

func NewString(x string) Value {
	return Value{kind: KindString, str: x}
}

func NewBytes(x []byte) Value {
	cp := make([]byte, len(x))
	copy(cp, x)
	return Value{kind: KindBytes, buf: cp}
}

// --- Accessors ---

func (v Value) AsBool() bool {
	v.mustKind(KindBool)
	return v.b
}

func (v Value) AsInt8() int8 {
	v.mustKind(KindInt8)
	return int8(v.i64)
}

func (v Value) AsUint8() uint8 {
	v.mustKind(KindUint8)
	return uint8(v.u64)
}

func (v Value) AsInt16() int16 {
	v.mustKind(KindInt16)
	return int16(v.i64)
}

func (v Value) AsUint16() uint16 {
	v.mustKind(KindUint16)
	return uint16(v.u64)
}

func (v Value) AsInt32() int32 {
	v.mustKind(KindInt32)
	return int32(v.i64)
}

func (v Value) AsUint32() uint32 {
	v.mustKind(KindUint32)
	return uint32(v.u64)
}

func (v Value) AsInt64() int64 {
	v.mustKind(KindInt64)
	return v.i64
}

func (v Value) AsUint64() uint64 {
	v.mustKind(KindUint64)
	return v.u64
}

func (v Value) AsFloat32() float32 {
	v.mustKind(KindFloat32)
	return v.f32
}

func (v Value) AsFloat64() float64 {
	v.mustKind(KindFloat64)
	return v.f64
}

func (v Value) AsString() string {
	v.mustKind(KindString)
	return v.str
}

func (v Value) AsBytes() []byte {
	v.mustKind(KindBytes)
	cp := make([]byte, len(v.buf))
	copy(cp, v.buf)
	return cp
}

func (v Value) mustKind(want ValueKind) {
	if v.kind != want {
		panic(fmt.Sprintf("shunter: Value.As%s called on %s value", want, v.kind))
	}
}

// --- Equality (Story 1.2) ---

// Equal returns true if v and other have the same kind and value.
// No cross-kind coercion.
func (v Value) Equal(other Value) bool {
	if v.kind != other.kind {
		return false
	}
	switch v.kind {
	case KindBool:
		return v.b == other.b
	case KindInt8, KindInt16, KindInt32, KindInt64:
		return v.i64 == other.i64
	case KindUint8, KindUint16, KindUint32, KindUint64:
		return v.u64 == other.u64
	case KindFloat32:
		return v.f32 == other.f32
	case KindFloat64:
		return v.f64 == other.f64
	case KindString:
		return v.str == other.str
	case KindBytes:
		return bytes.Equal(v.buf, other.buf)
	default:
		panic(fmt.Sprintf("shunter: unhandled ValueKind %d", int(v.kind)))
	}
}

// --- Ordering (Story 1.3) ---

// Compare returns -1, 0, or +1. Panics on cross-kind comparison.
func (v Value) Compare(other Value) int {
	if v.kind != other.kind {
		panic(fmt.Sprintf("shunter: Value.Compare across kinds: %s vs %s", v.kind, other.kind))
	}
	switch v.kind {
	case KindBool:
		if v.b == other.b {
			return 0
		}
		if !v.b {
			return -1
		}
		return 1
	case KindInt8, KindInt16, KindInt32, KindInt64:
		return cmp.Compare(v.i64, other.i64)
	case KindUint8, KindUint16, KindUint32, KindUint64:
		return cmp.Compare(v.u64, other.u64)
	case KindFloat32:
		return cmp.Compare(v.f32, other.f32)
	case KindFloat64:
		return cmp.Compare(v.f64, other.f64)
	case KindString:
		return strings.Compare(v.str, other.str)
	case KindBytes:
		return bytes.Compare(v.buf, other.buf)
	default:
		panic(fmt.Sprintf("shunter: unhandled ValueKind %d", int(v.kind)))
	}
}

// --- Hashing (Story 1.4) ---

// Hash feeds the canonical hash representation of v into h.
// Format: kind byte followed by canonical payload bytes.
func (v Value) Hash(h hash.Hash64) {
	h.Write([]byte{byte(v.kind)})
	v.writePayload(h)
}

// Hash64 returns a hash using fnv64a.
func (v Value) Hash64() uint64 {
	h := fnv.New64a()
	v.Hash(h)
	return h.Sum64()
}

// writePayload writes canonical payload bytes (without kind) into h.
func (v Value) writePayload(h hash.Hash64) {
	var buf [8]byte
	switch v.kind {
	case KindBool:
		if v.b {
			buf[0] = 1
		}
		h.Write(buf[:1])
	case KindInt8, KindInt16, KindInt32, KindInt64:
		binary.BigEndian.PutUint64(buf[:], uint64(v.i64))
		h.Write(buf[:])
	case KindUint8, KindUint16, KindUint32, KindUint64:
		binary.BigEndian.PutUint64(buf[:], v.u64)
		h.Write(buf[:])
	case KindFloat32:
		binary.BigEndian.PutUint32(buf[:4], math.Float32bits(v.f32))
		h.Write(buf[:4])
	case KindFloat64:
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(v.f64))
		h.Write(buf[:])
	case KindString:
		h.Write([]byte(v.str))
	case KindBytes:
		h.Write(v.buf)
	}
}

// payloadLen returns the byte length of the canonical payload.
func (v Value) payloadLen() uint32 {
	switch v.kind {
	case KindBool:
		return 1
	case KindInt8, KindInt16, KindInt32, KindInt64:
		return 8
	case KindUint8, KindUint16, KindUint32, KindUint64:
		return 8
	case KindFloat32:
		return 4
	case KindFloat64:
		return 8
	case KindString:
		return uint32(len(v.str))
	case KindBytes:
		return uint32(len(v.buf))
	default:
		return 0
	}
}
