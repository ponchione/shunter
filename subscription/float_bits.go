package subscription

import "math"

func canonicalFloat32Bits(v float32) uint32 {
	if v == 0 {
		return 0
	}
	return math.Float32bits(v)
}

func canonicalFloat64Bits(v float64) uint64 {
	if v == 0 {
		return 0
	}
	return math.Float64bits(v)
}
