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
	"slices"
	"time"
)

var ErrInvalidFloat = errors.New("invalid float value")

// ErrInvalidUUID identifies UUID text that is not canonical lowercase
// RFC 4122 hyphenated form.
var ErrInvalidUUID = errors.New("invalid UUID")

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
	KindInt256
	KindUint256
	KindTimestamp
	KindArrayString
	KindUUID
	KindDuration
	KindJSON
)

var kindNames = [...]string{
	KindBool:        "Bool",
	KindInt8:        "Int8",
	KindUint8:       "Uint8",
	KindInt16:       "Int16",
	KindUint16:      "Uint16",
	KindInt32:       "Int32",
	KindUint32:      "Uint32",
	KindInt64:       "Int64",
	KindUint64:      "Uint64",
	KindFloat32:     "Float32",
	KindFloat64:     "Float64",
	KindString:      "String",
	KindBytes:       "Bytes",
	KindInt128:      "Int128",
	KindUint128:     "Uint128",
	KindInt256:      "Int256",
	KindUint256:     "Uint256",
	KindTimestamp:   "Timestamp",
	KindArrayString: "ArrayString",
	KindUUID:        "UUID",
	KindDuration:    "Duration",
	KindJSON:        "JSON",
}

func (k ValueKind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("ValueKind(%d)", int(k))
}

// Value is a tagged union of supported column types.
// Wide integers store fixed-width words in big-endian order.
type Value struct {
	kind   ValueKind
	isNull bool
	b      bool
	i64    int64
	u64    uint64
	f32    float32
	f64    float64
	str    string
	buf    []byte
	hi128  uint64
	lo128  uint64
	w256   [4]uint64
	strArr []string
	uuid   [16]byte
}

// Kind returns the ValueKind of this Value.
func (v Value) Kind() ValueKind { return v.kind }

// IsNull reports whether this Value is the null sentinel for its kind.
func (v Value) IsNull() bool { return v.isNull }

// --- Constructors ---

func NewNull(kind ValueKind) Value {
	return Value{kind: kind, isNull: true}
}

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

// NewBytesOwned builds a Bytes value by taking ownership of x.
// Callers must not mutate x after passing it here.
func NewBytesOwned(x []byte) Value {
	return Value{kind: KindBytes, buf: x}
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

// NewInt256 builds a 256-bit signed value. w0 is the signed most-significant
// word; w1, w2, w3 are unsigned words in descending significance (w3 is the
// least-significant word).
func NewInt256(w0 int64, w1, w2, w3 uint64) Value {
	return Value{kind: KindInt256, w256: [4]uint64{uint64(w0), w1, w2, w3}}
}

// NewInt256FromInt64 widens an int64 into an Int256 with sign extension.
func NewInt256FromInt64(x int64) Value {
	var ext uint64
	if x < 0 {
		ext = ^uint64(0)
	}
	return Value{kind: KindInt256, w256: [4]uint64{ext, ext, ext, uint64(x)}}
}

// NewUint256 builds a 256-bit unsigned value from its four words (w0 is the
// most-significant word, w3 the least-significant).
func NewUint256(w0, w1, w2, w3 uint64) Value {
	return Value{kind: KindUint256, w256: [4]uint64{w0, w1, w2, w3}}
}

// NewUint256FromUint64 widens a uint64 into a Uint256 with zero-extension.
func NewUint256FromUint64(x uint64) Value {
	return Value{kind: KindUint256, w256: [4]uint64{0, 0, 0, x}}
}

// NewTimestamp builds a Timestamp value. micros is microseconds since the
// Unix epoch (negative values denote times before the epoch); storage reuses
// the i64 slot.
func NewTimestamp(micros int64) Value {
	return Value{kind: KindTimestamp, i64: micros}
}

// NewTimestampFromTime builds a Timestamp value from a Go time.Time. Precision
// is truncated to microseconds to match Shunter's timestamp storage.
func NewTimestampFromTime(t time.Time) Value {
	return NewTimestamp(t.UTC().UnixMicro())
}

// NewDuration builds a Duration value. micros is signed microseconds; storage
// reuses the i64 slot.
func NewDuration(micros int64) Value {
	return Value{kind: KindDuration, i64: micros}
}

// NewDurationFromTime builds a Duration value from a Go time.Duration.
// Precision is truncated to microseconds to match Shunter's duration storage.
func NewDurationFromTime(d time.Duration) Value {
	return NewDuration(d.Microseconds())
}

// NewArrayString builds an ArrayString value from a slice of strings.
// The input slice is copied defensively so the resulting Value does not
// alias caller storage.
func NewArrayString(xs []string) Value {
	cp := make([]string, len(xs))
	copy(cp, xs)
	return Value{kind: KindArrayString, strArr: cp}
}

// NewArrayStringOwned builds an ArrayString value by taking ownership of xs.
// Callers must not mutate xs after passing it here.
func NewArrayStringOwned(xs []string) Value {
	return Value{kind: KindArrayString, strArr: xs}
}

// NewUUID builds a UUID value from its canonical 16-byte representation.
func NewUUID(x [16]byte) Value {
	return Value{kind: KindUUID, uuid: x}
}

// ParseUUID parses canonical lowercase RFC 4122 hyphenated UUID text.
func ParseUUID(s string) (Value, error) {
	var out [16]byte
	if len(s) != 36 {
		return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
	}
	j := 0
	for i := 0; i < len(s); {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
			}
			i++
			continue
		}
		if i+1 >= len(s) {
			return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
		}
		hi, ok := lowerHexValue(s[i])
		if !ok {
			return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
		}
		lo, ok := lowerHexValue(s[i+1])
		if !ok {
			return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
		}
		if j >= len(out) {
			return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
		}
		out[j] = hi<<4 | lo
		j++
		i += 2
	}
	if j != len(out) {
		return Value{}, fmt.Errorf("shunter: %w: %q", ErrInvalidUUID, s)
	}
	return NewUUID(out), nil
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

// BytesView returns the Bytes payload without copying.
// The returned slice is read-only by convention; mutating it would mutate v.
func (v Value) BytesView() []byte {
	v.mustKind(KindBytes)
	return v.buf
}

// AsUUID returns the canonical 16-byte UUID payload.
func (v Value) AsUUID() [16]byte {
	v.mustKind(KindUUID)
	return v.uuid
}

// UUIDString returns canonical lowercase RFC 4122 hyphenated UUID text.
func (v Value) UUIDString() string {
	u := v.AsUUID()
	var out [36]byte
	j := 0
	for i, b := range u {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}
		out[j] = lowerHexDigits[b>>4]
		out[j+1] = lowerHexDigits[b&0x0f]
		j += 2
	}
	return string(out[:])
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

// AsInt256 returns the signed most-significant word and the three remaining
// unsigned words of an Int256 (w3 is the least-significant word).
func (v Value) AsInt256() (w0 int64, w1, w2, w3 uint64) {
	v.mustKind(KindInt256)
	return int64(v.w256[0]), v.w256[1], v.w256[2], v.w256[3]
}

// AsUint256 returns the four words of a Uint256 (w0 most significant, w3
// least significant).
func (v Value) AsUint256() (w0, w1, w2, w3 uint64) {
	v.mustKind(KindUint256)
	return v.w256[0], v.w256[1], v.w256[2], v.w256[3]
}

// AsTimestamp returns the Timestamp in microseconds since the Unix epoch.
func (v Value) AsTimestamp() int64 {
	v.mustKind(KindTimestamp)
	return v.i64
}

// AsTime returns the Timestamp as a UTC Go time.Time.
func (v Value) AsTime() time.Time {
	return time.UnixMicro(v.AsTimestamp()).UTC()
}

// AsDurationMicros returns the Duration in signed microseconds.
func (v Value) AsDurationMicros() int64 {
	v.mustKind(KindDuration)
	return v.i64
}

// AsDuration returns the Duration as a Go time.Duration.
func (v Value) AsDuration() time.Duration {
	return time.Duration(v.AsDurationMicros()) * time.Microsecond
}

// AsArrayString returns a defensive copy of the string-array payload.
func (v Value) AsArrayString() []string {
	v.mustKind(KindArrayString)
	cp := make([]string, len(v.strArr))
	copy(cp, v.strArr)
	return cp
}

// ArrayStringView returns the ArrayString payload without copying.
// The returned slice is read-only by convention; mutating it would mutate v.
func (v Value) ArrayStringView() []string {
	v.mustKind(KindArrayString)
	return v.strArr
}

// AsJSON returns a defensive copy of the canonical JSON payload.
func (v Value) AsJSON() []byte {
	v.mustKind(KindJSON)
	cp := make([]byte, len(v.buf))
	copy(cp, v.buf)
	return cp
}

// JSONView returns the canonical JSON payload without copying.
// The returned slice is read-only by convention; mutating it would mutate v.
func (v Value) JSONView() []byte {
	v.mustKind(KindJSON)
	return v.buf
}

func (v Value) mustKind(want ValueKind) {
	if v.kind != want {
		panic(fmt.Sprintf("shunter: Value.As%s called on %s value", want, v.kind))
	}
	if v.isNull {
		panic(fmt.Sprintf("shunter: Value.As%s called on null %s value", want, v.kind))
	}
}

// --- Equality (Story 1.2) ---

// Equal returns true if v and other have the same kind and value.
// No cross-kind coercion.
func (v Value) Equal(other Value) bool {
	if v.kind != other.kind {
		return false
	}
	if v.isNull || other.isNull {
		return v.isNull && other.isNull
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
	case KindBytes, KindJSON:
		return bytes.Equal(v.buf, other.buf)
	case KindInt128, KindUint128:
		return v.hi128 == other.hi128 && v.lo128 == other.lo128
	case KindInt256, KindUint256:
		return v.w256 == other.w256
	case KindTimestamp, KindDuration:
		return v.i64 == other.i64
	case KindArrayString:
		return slices.Equal(v.strArr, other.strArr)
	case KindUUID:
		return v.uuid == other.uuid
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
	if v.isNull || other.isNull {
		switch {
		case v.isNull && other.isNull:
			return 0
		case v.isNull:
			return -1
		default:
			return 1
		}
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
		return cmp.Compare(v.str, other.str)
	case KindBytes, KindJSON:
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
	case KindInt256:
		if c := cmp.Compare(int64(v.w256[0]), int64(other.w256[0])); c != 0 {
			return c
		}
		for i := 1; i < 4; i++ {
			if c := cmp.Compare(v.w256[i], other.w256[i]); c != 0 {
				return c
			}
		}
		return 0
	case KindUint256:
		for i := range 4 {
			if c := cmp.Compare(v.w256[i], other.w256[i]); c != 0 {
				return c
			}
		}
		return 0
	case KindTimestamp, KindDuration:
		return cmp.Compare(v.i64, other.i64)
	case KindArrayString:
		return slices.Compare(v.strArr, other.strArr)
	case KindUUID:
		return bytes.Compare(v.uuid[:], other.uuid[:])
	default:
		panic(fmt.Sprintf("shunter: unhandled ValueKind %d", int(v.kind)))
	}
}

// --- Hashing (Story 1.4) ---

// Hash feeds the canonical hash representation of v into h.
// Format: kind byte, null marker, then canonical payload bytes when present.
func (v Value) Hash(h hash.Hash64) {
	h.Write([]byte{byte(v.kind)})
	if v.isNull {
		h.Write([]byte{0})
		return
	}
	h.Write([]byte{1})
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
	if v.isNull {
		return
	}
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
		bits := uint32(0)
		if v.f32 != 0 {
			bits = math.Float32bits(v.f32)
		}
		binary.BigEndian.PutUint32(buf[:4], bits)
		h.Write(buf[:4])
	case KindFloat64:
		bits := uint64(0)
		if v.f64 != 0 {
			bits = math.Float64bits(v.f64)
		}
		binary.BigEndian.PutUint64(buf[:], bits)
		h.Write(buf[:])
	case KindString:
		h.Write([]byte(v.str))
	case KindBytes, KindJSON:
		h.Write(v.buf)
	case KindInt128, KindUint128:
		binary.BigEndian.PutUint64(buf[:], v.hi128)
		h.Write(buf[:])
		binary.BigEndian.PutUint64(buf[:], v.lo128)
		h.Write(buf[:])
	case KindInt256, KindUint256:
		for i := range 4 {
			binary.BigEndian.PutUint64(buf[:], v.w256[i])
			h.Write(buf[:])
		}
	case KindTimestamp, KindDuration:
		binary.BigEndian.PutUint64(buf[:], uint64(v.i64))
		h.Write(buf[:])
	case KindArrayString:
		binary.BigEndian.PutUint32(buf[:4], uint32(len(v.strArr)))
		h.Write(buf[:4])
		for _, s := range v.strArr {
			binary.BigEndian.PutUint32(buf[:4], uint32(len(s)))
			h.Write(buf[:4])
			h.Write([]byte(s))
		}
	case KindUUID:
		h.Write(v.uuid[:])
	}
}

// payloadLen returns the byte length of the canonical payload.
func (v Value) payloadLen() uint32 {
	if v.isNull {
		return 0
	}
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
	case KindBytes, KindJSON:
		return uint32(len(v.buf))
	case KindInt128, KindUint128:
		return 16
	case KindInt256, KindUint256:
		return 32
	case KindTimestamp, KindDuration:
		return 8
	case KindArrayString:
		n := uint32(4)
		for _, s := range v.strArr {
			n += 4 + uint32(len(s))
		}
		return n
	case KindUUID:
		return 16
	default:
		return 0
	}
}

var lowerHexDigits = [...]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

func lowerHexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	default:
		return 0, false
	}
}
