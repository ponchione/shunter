package bsatn

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

type wideValueWireVector struct {
	name  string
	value types.Value
	wire  string
}

// wideValueWireVectors deliberately spells out the on-wire bytes instead of
// deriving them through an encoder, decoder, or word-layout helper. Wide
// values are stored most-significant-word first in types.Value, while BSATN
// carries the least-significant word first and each word little-endian.
func wideValueWireVectors() []wideValueWireVector {
	return []wideValueWireVector{
		{"int128_zero", types.NewInt128(0, 0), "0d00000000000000000000000000000000"},
		{"int128_one", types.NewInt128(0, 1), "0d01000000000000000000000000000000"},
		{"int128_negative_one", types.NewInt128(-1, ^uint64(0)), "0dffffffffffffffffffffffffffffffff"},
		{"int128_min", types.NewInt128(math.MinInt64, 0), "0d00000000000000000000000000000080"},
		{"int128_max", types.NewInt128(math.MaxInt64, ^uint64(0)), "0dffffffffffffffffffffffffffffff7f"},
		{"int128_multiword", types.NewInt128(0x0102030405060708, 0x1112131415161718), "0d18171615141312110807060504030201"},

		{"uint128_zero", types.NewUint128(0, 0), "0e00000000000000000000000000000000"},
		{"uint128_one", types.NewUint128(0, 1), "0e01000000000000000000000000000000"},
		{"uint128_max", types.NewUint128(^uint64(0), ^uint64(0)), "0effffffffffffffffffffffffffffffff"},
		{"uint128_multiword", types.NewUint128(0x0102030405060708, 0x1112131415161718), "0e18171615141312110807060504030201"},

		{"int256_zero", types.NewInt256(0, 0, 0, 0), "0f0000000000000000000000000000000000000000000000000000000000000000"},
		{"int256_one", types.NewInt256(0, 0, 0, 1), "0f0100000000000000000000000000000000000000000000000000000000000000"},
		{"int256_negative_one", types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)), "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{"int256_min", types.NewInt256(math.MinInt64, 0, 0, 0), "0f0000000000000000000000000000000000000000000000000000000000000080"},
		{"int256_max", types.NewInt256(math.MaxInt64, ^uint64(0), ^uint64(0), ^uint64(0)), "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"},
		{"int256_multiword", types.NewInt256(0x0102030405060708, 0x1112131415161718, 0x2122232425262728, 0x3132333435363738), "0f3837363534333231282726252423222118171615141312110807060504030201"},

		{"uint256_zero", types.NewUint256(0, 0, 0, 0), "100000000000000000000000000000000000000000000000000000000000000000"},
		{"uint256_one", types.NewUint256(0, 0, 0, 1), "100100000000000000000000000000000000000000000000000000000000000000"},
		{"uint256_max", types.NewUint256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)), "10ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{"uint256_multiword", types.NewUint256(0x0102030405060708, 0x1112131415161718, 0x2122232425262728, 0x3132333435363738), "103837363534333231282726252423222118171615141312110807060504030201"},
		// 10^40 = 0x1d6329f1c35ca4bfabb9f5610000000000.
		{"uint256_ten_to_fortieth", types.NewUint256(0, 0x1d, 0x6329f1c35ca4bfab, 0xb9f5610000000000), "10000000000061f5b9abbfa45cc3f129631d000000000000000000000000000000"},
	}
}

func TestWideValueFixedWireVectors(t *testing.T) {
	for _, vector := range wideValueWireVectors() {
		t.Run(vector.name, func(t *testing.T) {
			want := mustHexBytes(t, vector.wire)
			got, err := AppendValue(nil, vector.value)
			if err != nil {
				t.Fatalf("AppendValue: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("AppendValue = %x, want %x", got, want)
			}

			decoded, err := DecodeValue(bytes.NewReader(want))
			if err != nil {
				t.Fatalf("DecodeValue: %v", err)
			}
			if !decoded.Equal(vector.value) {
				t.Fatalf("DecodeValue = %v, want %v", decoded, vector.value)
			}
		})
	}
}

func TestWideProductValueFixedWireVectors(t *testing.T) {
	ts := &schema.TableSchema{
		Name: "wide_values",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint128},
			{Index: 1, Name: "signed", Type: types.KindInt256, Nullable: true},
			{Index: 2, Name: "optional", Type: types.KindUint256, Nullable: true},
		},
	}
	vectors := []struct {
		name string
		row  types.ProductValue
		wire string
	}{
		{
			name: "present_signed_and_null_unsigned",
			row: types.ProductValue{
				types.NewUint128(0x0102030405060708, 0x1112131415161718),
				types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)),
				types.NewNull(types.KindUint256),
			},
			wire: "0e18171615141312110807060504030201" +
				"0f01ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" +
				"1000",
		},
		{
			name: "null_signed_and_present_scientific_unsigned",
			row: types.ProductValue{
				types.NewUint128(0, 1),
				types.NewNull(types.KindInt256),
				types.NewUint256(0, 0x1d, 0x6329f1c35ca4bfab, 0xb9f5610000000000),
			},
			wire: "0e01000000000000000000000000000000" +
				"0f00" +
				"1001000000000061f5b9abbfa45cc3f129631d000000000000000000000000000000",
		},
	}

	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			want := mustHexBytes(t, vector.wire)
			got, err := AppendProductValueForSchema(nil, vector.row, ts)
			if err != nil {
				t.Fatalf("AppendProductValueForSchema: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("AppendProductValueForSchema = %x, want %x", got, want)
			}

			decoded, err := DecodeProductValueFromBytes(want, ts)
			if err != nil {
				t.Fatalf("DecodeProductValueFromBytes: %v", err)
			}
			if !decoded.Equal(vector.row) {
				t.Fatalf("DecodeProductValueFromBytes = %v, want %v", decoded, vector.row)
			}
		})
	}
}

func handAuthoredWideValueFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	seeds := make([][]byte, 0, len(wideValueWireVectors()))
	for _, vector := range wideValueWireVectors() {
		seeds = append(seeds, mustHexBytes(tb, vector.wire))
	}
	return seeds
}

// handAuthoredWideProductFuzzSeed is valid for fuzzProductValueSchema. Its
// wide fields are Int128(-1) and Uint256(10^40); no production encoder is used
// to construct the seed.
func handAuthoredWideProductFuzzSeed(tb testing.TB) []byte {
	tb.Helper()
	return mustHexBytes(tb,
		"080100000000000000"+
			"0001"+
			"0b0100000061"+
			"0c02000000dead"+
			"12010000000100000078"+
			"0dffffffffffffffffffffffffffffffff"+
			"110200000000000000"+
			"10000000000061f5b9abbfa45cc3f129631d000000000000000000000000000000"+
			"1300112233445566778899aabbccddeeff"+
			"14ffffffffffffffff"+
			"15040000006e756c6c",
	)
}

func mustHexBytes(tb testing.TB, encoded string) []byte {
	tb.Helper()
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		tb.Fatalf("decode fixed wire vector %q: %v", encoded, err)
	}
	return decoded
}
