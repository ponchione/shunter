package types

import "testing"

var valueEqualSink bool

func BenchmarkValueEqual(b *testing.B) {
	cases := valueEqualBenchmarkCases()
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var equal bool
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				equal = tc.left.Equal(tc.right)
			}
			valueEqualSink = equal
		})
	}
}

func BenchmarkEqualValues(b *testing.B) {
	cases := valueEqualBenchmarkCases()
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			left := &tc.left
			right := &tc.right
			var equal bool
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				equal = EqualValues(left, right)
			}
			valueEqualSink = equal
		})
	}
}

func valueEqualBenchmarkCases() []struct {
	name        string
	left, right Value
} {
	return []struct {
		name        string
		left, right Value
	}{
		{name: "uint64_equal", left: NewUint64(42), right: NewUint64(42)},
		{name: "uint64_unequal", left: NewUint64(42), right: NewUint64(43)},
		{name: "string_equal", left: NewString("one-off-join-key"), right: NewString("one-off-join-key")},
		{name: "string_unequal", left: NewString("one-off-join-key"), right: NewString("other-join-key")},
		{name: "uint256_equal", left: NewUint256(1, 2, 3, 4), right: NewUint256(1, 2, 3, 4)},
		{name: "uint256_unequal", left: NewUint256(1, 2, 3, 4), right: NewUint256(1, 2, 3, 5)},
		{name: "null_equal", left: NewNull(KindUint64), right: NewNull(KindUint64)},
		{name: "bytes_equal", left: NewBytes([]byte("one-off-join-key")), right: NewBytes([]byte("one-off-join-key"))},
		{name: "bytes_unequal", left: NewBytes([]byte("one-off-join-key")), right: NewBytes([]byte("other-join-key"))},
	}
}
