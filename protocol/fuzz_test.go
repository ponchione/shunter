package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func FuzzDecodeRowList(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0, 0, 0, 0},
		{1, 0, 0, 0},
		{1, 0, 0, 0, 3, 0, 0, 0, 0xaa, 0xbb, 0xcc},
		{2, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0x42},
		{100, 0, 0, 0, 1, 0, 0, 0, 0x42},
		{0xff, 0xff, 0xff, 0xff},
		{1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
	} {
		f.Add(seed)
	}

	const maxRowListFuzzBytes = 64 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxRowListFuzzBytes {
			t.Skip("rowlist fuzz input above bounded local limit")
		}

		rows, err := DecodeRowList(data)
		if err != nil {
			if !errors.Is(err, ErrMalformedMessage) {
				t.Fatalf("DecodeRowList(%x) error = %v, want ErrMalformedMessage category", data, err)
			}
			return
		}

		encoded := EncodeRowList(rows)
		roundTrip, err := DecodeRowList(encoded)
		if err != nil {
			t.Fatalf("DecodeRowList(EncodeRowList(rows)) after accepting %x: %v", data, err)
		}
		if len(roundTrip) != len(rows) {
			t.Fatalf("row count after canonical round trip = %d, want %d for input %x", len(roundTrip), len(rows), data)
		}
		for i := range rows {
			if !bytes.Equal(roundTrip[i], rows[i]) {
				t.Fatalf("row %d after canonical round trip = %x, want %x for input %x", i, roundTrip[i], rows[i], data)
			}
		}
	})
}
