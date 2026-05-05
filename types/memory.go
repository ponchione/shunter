package types

import "unsafe"

// ApproxMemoryBytes returns a deterministic approximation of the in-memory
// bytes held by v, including dynamic payloads owned by the value.
func (v Value) ApproxMemoryBytes() uint64 {
	n := uint64(unsafe.Sizeof(v))
	switch v.kind {
	case KindString:
		n += uint64(len(v.str))
	case KindBytes, KindJSON:
		n += uint64(cap(v.buf))
	case KindArrayString:
		n += uint64(cap(v.strArr)) * uint64(unsafe.Sizeof(""))
		for _, s := range v.strArr {
			n += uint64(len(s))
		}
	}
	return n
}

// ApproxMemoryBytes returns a deterministic approximation of the in-memory
// bytes held by pv, including dynamic payloads owned by its values.
func (pv ProductValue) ApproxMemoryBytes() uint64 {
	n := uint64(unsafe.Sizeof(pv))
	for _, v := range pv {
		n += v.ApproxMemoryBytes()
	}
	return n
}
