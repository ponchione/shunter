package subscription

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"sync"

	"lukechampine.com/blake3"

	"github.com/ponchione/shunter/types"
)

// QueryHash is the 32-byte blake3 digest of a predicate's canonical form.
// It is used as the deduplication key across the subscription manager and
// pruning indexes.
type QueryHash [32]byte

// String returns the hex encoding of the hash.
func (h QueryHash) String() string { return hex.EncodeToString(h[:]) }

// Canonical serialization tags. These are internal — not a wire format.
// Only requirement is determinism within a single binary version.
const (
	tagColEq     byte = 0x01
	tagColNe     byte = 0x02
	tagColRange  byte = 0x03
	tagAnd       byte = 0x04
	tagAllRows   byte = 0x05
	tagJoin      byte = 0x06
	tagOr        byte = 0x07
	tagCrossJoin byte = 0x08
)

// Within a canonical Bound encoding.
const (
	boundUnbounded byte = 0x00
	boundExclusive byte = 0x01
	boundInclusive byte = 0x02
)

var encoderPool = sync.Pool{
	New: func() any { return &canonicalEncoder{} },
}

type canonicalEncoder struct {
	buf []byte
}

func acquireCanonicalEncoder() *canonicalEncoder {
	enc := encoderPool.Get().(*canonicalEncoder)
	if enc.buf == nil {
		enc.buf = acquirePooledBuffer()
	} else {
		enc.buf = enc.buf[:0]
	}
	return enc
}

func releaseCanonicalEncoder(enc *canonicalEncoder) {
	releasePooledBuffer(enc.buf)
	enc.buf = nil
	encoderPool.Put(enc)
}

func (e *canonicalEncoder) reset() { e.buf = e.buf[:0] }

func (e *canonicalEncoder) writeByte(b byte) { e.buf = append(e.buf, b) }

func (e *canonicalEncoder) writeU32(v uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	e.buf = append(e.buf, tmp[:]...)
}

func (e *canonicalEncoder) writeU64(v uint64) {
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], v)
	e.buf = append(e.buf, tmp[:]...)
}

func isAllRowsPredicate(pred Predicate) bool {
	_, ok := pred.(AllRows)
	return ok
}

func canonicalizePredicate(pred Predicate) Predicate {
	switch p := pred.(type) {
	case And:
		left := canonicalizePredicate(p.Left)
		right := canonicalizePredicate(p.Right)
		if left == nil || right == nil {
			return And{Left: left, Right: right}
		}
		if isAllRowsPredicate(left) {
			return right
		}
		if isAllRowsPredicate(right) {
			return left
		}
		return And{Left: left, Right: right}
	case Or:
		left := canonicalizePredicate(p.Left)
		right := canonicalizePredicate(p.Right)
		if left == nil || right == nil {
			return Or{Left: left, Right: right}
		}
		if isAllRowsPredicate(left) {
			return left
		}
		if isAllRowsPredicate(right) {
			return right
		}
		return Or{Left: left, Right: right}
	default:
		return pred
	}
}

// ComputeQueryHash returns the blake3-32 digest of the predicate's canonical
// form. When clientID is non-nil, the identity is appended so structurally
// identical predicates from different clients produce different hashes
// (parameterized form, SPEC-004 §3.4).
func ComputeQueryHash(pred Predicate, clientID *types.Identity) QueryHash {
	if pred == nil {
		panic("subscription: ComputeQueryHash on nil predicate")
	}
	pred = canonicalizePredicate(pred)
	enc := acquireCanonicalEncoder()
	defer releaseCanonicalEncoder(enc)
	encodePredicate(enc, pred)
	if clientID != nil {
		enc.buf = append(enc.buf, clientID[:]...)
	}
	h := QueryHash(blake3.Sum256(enc.buf))
	return h
}

func encodePredicate(e *canonicalEncoder, pred Predicate) {
	switch p := pred.(type) {
	case ColEq:
		e.writeByte(tagColEq)
		e.writeU32(uint32(p.Table))
		e.writeU32(uint32(p.Column))
		e.writeByte(p.Alias)
		encodeValue(e, p.Value)
	case ColNe:
		e.writeByte(tagColNe)
		e.writeU32(uint32(p.Table))
		e.writeU32(uint32(p.Column))
		e.writeByte(p.Alias)
		encodeValue(e, p.Value)
	case ColRange:
		e.writeByte(tagColRange)
		e.writeU32(uint32(p.Table))
		e.writeU32(uint32(p.Column))
		e.writeByte(p.Alias)
		encodeBound(e, p.Lower)
		encodeBound(e, p.Upper)
	case And:
		e.writeByte(tagAnd)
		encodePredicate(e, p.Left)
		encodePredicate(e, p.Right)
	case Or:
		e.writeByte(tagOr)
		encodePredicate(e, p.Left)
		encodePredicate(e, p.Right)
	case AllRows:
		e.writeByte(tagAllRows)
		e.writeU32(uint32(p.Table))
	case Join:
		e.writeByte(tagJoin)
		e.writeU32(uint32(p.Left))
		e.writeU32(uint32(p.Right))
		e.writeU32(uint32(p.LeftCol))
		e.writeU32(uint32(p.RightCol))
		e.writeByte(p.LeftAlias)
		e.writeByte(p.RightAlias)
		if p.ProjectRight {
			e.writeByte(1)
		} else {
			e.writeByte(0)
		}
		if p.Filter == nil {
			e.writeByte(0)
		} else {
			e.writeByte(1)
			encodePredicate(e, p.Filter)
		}
	case CrossJoin:
		e.writeByte(tagCrossJoin)
		e.writeU32(uint32(p.Left))
		e.writeU32(uint32(p.Right))
		e.writeByte(p.LeftAlias)
		e.writeByte(p.RightAlias)
		if p.ProjectRight {
			e.writeByte(1)
		} else {
			e.writeByte(0)
		}
	default:
		// Sealed interface — no external impls reach this point.
		panic("subscription: unknown predicate type")
	}
}

func encodeBound(e *canonicalEncoder, b Bound) {
	if b.Unbounded {
		e.writeByte(boundUnbounded)
		return
	}
	if b.Inclusive {
		e.writeByte(boundInclusive)
	} else {
		e.writeByte(boundExclusive)
	}
	encodeValue(e, b.Value)
}

func encodeValue(e *canonicalEncoder, v Value) {
	k := v.Kind()
	e.writeByte(byte(k))
	switch k {
	case types.KindBool:
		if v.AsBool() {
			e.writeByte(1)
		} else {
			e.writeByte(0)
		}
	case types.KindInt8:
		e.writeU64(uint64(int64(v.AsInt8())))
	case types.KindInt16:
		e.writeU64(uint64(int64(v.AsInt16())))
	case types.KindInt32:
		e.writeU64(uint64(int64(v.AsInt32())))
	case types.KindInt64:
		e.writeU64(uint64(v.AsInt64()))
	case types.KindUint8:
		e.writeU64(uint64(v.AsUint8()))
	case types.KindUint16:
		e.writeU64(uint64(v.AsUint16()))
	case types.KindUint32:
		e.writeU64(uint64(v.AsUint32()))
	case types.KindUint64:
		e.writeU64(v.AsUint64())
	case types.KindFloat32:
		e.writeU32(math.Float32bits(v.AsFloat32()))
	case types.KindFloat64:
		e.writeU64(math.Float64bits(v.AsFloat64()))
	case types.KindString:
		s := v.AsString()
		e.writeU32(uint32(len(s)))
		e.buf = append(e.buf, s...)
	case types.KindBytes:
		b := v.AsBytes()
		e.writeU32(uint32(len(b)))
		e.buf = append(e.buf, b...)
	case types.KindInt128:
		hi, lo := v.AsInt128()
		e.writeU64(uint64(hi))
		e.writeU64(lo)
	case types.KindUint128:
		hi, lo := v.AsUint128()
		e.writeU64(hi)
		e.writeU64(lo)
	case types.KindInt256:
		w0, w1, w2, w3 := v.AsInt256()
		e.writeU64(uint64(w0))
		e.writeU64(w1)
		e.writeU64(w2)
		e.writeU64(w3)
	case types.KindUint256:
		w0, w1, w2, w3 := v.AsUint256()
		e.writeU64(w0)
		e.writeU64(w1)
		e.writeU64(w2)
		e.writeU64(w3)
	case types.KindTimestamp:
		e.writeU64(uint64(v.AsTimestamp()))
	case types.KindArrayString:
		xs := v.AsArrayString()
		e.writeU32(uint32(len(xs)))
		for _, s := range xs {
			e.writeU32(uint32(len(s)))
			e.buf = append(e.buf, s...)
		}
	default:
		panic("subscription: encodeValue unhandled kind")
	}
}
