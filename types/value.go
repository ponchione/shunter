package types

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"math"
	"strings"
)

var ErrInvalidFloat = errors.New("invalid float value")

// ValueKind identifies the type of a column value.
type ValueKind int

const (
	KindBool ValueKind = iota
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
	KindInt128
	KindUint128
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
	KindInt128:  "Int128",
	KindUint128: "Uint128",
}

func (k ValueKind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("ValueKind(%d)", int(k))
}

// Value is a tagged union of all v1 column types.
// Fields not used by the current kind are zero values.
//
// 128-bit kinds use hi128/lo128 (two's complement; hi is the signed high word
// for Int128, unsigned for Uint128). Existing primitive slots are untouched so
// the blast radius on unrelated code paths stays zero.
type Value struct {
	kind  ValueKind
	b     bool
	i64   int64
	u64   uint64
	f32   float32
	f64   float64
	str   string
	buf   []byte
	hi128 uint64
	lo128 uint64
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
		return Value{}, fmt.Errorf("shunter: NaN is not a valid Float32 value: %w", ErrInvalidFloat)
	}
	return Value{kind: KindFloat32, f32: x}, nil
}

func NewFloat64(x float64) (Value, error) {
	if math.IsNaN(x) {
		return Value{}, fmt.Errorf("shunter: NaN is not a valid Float64 value: %w", ErrInvalidFloat)
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

// NewInt128 builds a 128-bit signed value from its high (signed) and low
// words. hi is the signed high word; lo is the unsigned low word.
func NewInt128(hi int64, lo uint64) Value {
	return Value{kind: KindInt128, hi128: uint64(hi), lo128: lo}
}

// NewInt128FromInt64 widens an int64 into an Int128 with sign extension.
func NewInt128FromInt64(x int64) Value {
	var hi uint64
	if x < 0 {
		hi = ^uint64(0)
	}
	return Value{kind: KindInt128, hi128: hi, lo128: uint64(x)}
}

// NewUint128 builds a 128-bit unsigned value from its high and low words.
func NewUint128(hi, lo uint64) Value {
	return Value{kind: KindUint128, hi128: hi, lo128: lo}
}

// NewUint128FromUint64 widens a uint64 into a Uint128 with zero-extension.
func NewUint128FromUint64(x uint64) Value {
	return Value{kind: KindUint128, hi128: 0, lo128: x}
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

// AsInt128 returns the signed high word and unsigned low word of an Int128.
func (v Value) AsInt128() (hi int64, lo uint64) {
	v.mustKind(KindInt128)
	return int64(v.hi128), v.lo128
}

// AsUint128 returns the high and low words of a Uint128.
func (v Value) AsUint128() (hi, lo uint64) {
	v.mustKind(KindUint128)
	return v.hi128, v.lo128
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
	case KindInt128, KindUint128:
		return v.hi128 == other.hi128 && v.lo128 == other.lo128
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
	case KindInt128:
		if c := cmp.Compare(int64(v.hi128), int64(other.hi128)); c != 0 {
			return c
		}
		return cmp.Compare(v.lo128, other.lo128)
	case KindUint128:
		if c := cmp.Compare(v.hi128, other.hi128); c != 0 {
			return c
		}
		return cmp.Compare(v.lo128, other.lo128)
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
	case KindInt128, KindUint128:
		binary.BigEndian.PutUint64(buf[:], v.hi128)
		h.Write(buf[:])
		binary.BigEndian.PutUint64(buf[:], v.lo128)
		h.Write(buf[:])
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
	case KindInt128, KindUint128:
		return 16
	default:
		return 0
	}
}
