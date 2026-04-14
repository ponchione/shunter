package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestRowListEncodeEmpty(t *testing.T) {
	got := EncodeRowList(nil)
	want := []byte{0, 0, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeRowList(nil) = % x, want % x", got, want)
	}

	got = EncodeRowList([][]byte{})
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeRowList([]) = % x, want % x", got, want)
	}
}

func TestRowListDecodeEmpty(t *testing.T) {
	rows, err := DecodeRowList([]byte{0, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestRowListRoundTripSingle(t *testing.T) {
	in := [][]byte{{0xaa, 0xbb, 0xcc}}
	enc := EncodeRowList(in)
	out, err := DecodeRowList(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d rows, want 1", len(out))
	}
	if !bytes.Equal(out[0], in[0]) {
		t.Errorf("row mismatch: got % x, want % x", out[0], in[0])
	}
}

func TestRowListRoundTripManyRows(t *testing.T) {
	in := make([][]byte, 100)
	for i := range in {
		in[i] = []byte{byte(i), byte(i + 1), byte(i + 2)}
	}
	enc := EncodeRowList(in)
	out, err := DecodeRowList(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 100 {
		t.Fatalf("got %d rows, want 100", len(out))
	}
	for i := range in {
		if !bytes.Equal(out[i], in[i]) {
			t.Errorf("row[%d]: got % x, want % x", i, out[i], in[i])
		}
	}
}

func TestRowListRoundTripVariableLength(t *testing.T) {
	in := [][]byte{
		{},
		{0x01},
		{0x02, 0x03},
		{},
		{0x04, 0x05, 0x06, 0x07, 0x08},
	}
	enc := EncodeRowList(in)
	out, err := DecodeRowList(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d rows, want %d", len(out), len(in))
	}
	for i := range in {
		if !bytes.Equal(out[i], in[i]) {
			t.Errorf("row[%d]: got % x, want % x", i, out[i], in[i])
		}
	}
}

func TestRowListDecodeTruncatedHeader(t *testing.T) {
	// <4 bytes — can't even read row count.
	for _, n := range []int{0, 1, 2, 3} {
		_, err := DecodeRowList(make([]byte, n))
		if !errors.Is(err, ErrMalformedMessage) {
			t.Errorf("DecodeRowList(%d bytes): got %v, want ErrMalformedMessage", n, err)
		}
	}
}

func TestRowListDecodeTruncatedRowLength(t *testing.T) {
	// Claim 1 row, but payload has no len prefix.
	data := []byte{1, 0, 0, 0} // count=1, no row header
	_, err := DecodeRowList(data)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("got %v, want ErrMalformedMessage", err)
	}

	// Claim 1 row, partial len prefix (2 bytes).
	data = []byte{1, 0, 0, 0, 5, 0}
	_, err = DecodeRowList(data)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("partial len: got %v, want ErrMalformedMessage", err)
	}
}

func TestRowListDecodeRowLengthExceedsRemaining(t *testing.T) {
	// Claim row len=10 but only 3 bytes available.
	data := []byte{1, 0, 0, 0, 10, 0, 0, 0, 0xaa, 0xbb, 0xcc}
	_, err := DecodeRowList(data)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("got %v, want ErrMalformedMessage", err)
	}
}

func TestRowListDecodeCountExceedsAvailable(t *testing.T) {
	// Claim 100 rows but only payload for 1.
	data := []byte{100, 0, 0, 0, 1, 0, 0, 0, 0x42}
	_, err := DecodeRowList(data)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("got %v, want ErrMalformedMessage", err)
	}
}
