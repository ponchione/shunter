package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeFrameCompressionDisabled(t *testing.T) {
	body := []byte{0x01, 0x02, 0x03}
	frame := EncodeFrame(TagSubscribeApplied, body, false, CompressionNone)
	want := append([]byte{TagSubscribeApplied}, body...)
	if !bytes.Equal(frame, want) {
		t.Errorf("disabled frame = % x, want % x", frame, want)
	}
}

func TestEncodeFrameEnabledModeNone(t *testing.T) {
	body := []byte{0xaa, 0xbb}
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionNone)
	want := []byte{CompressionNone, TagTransactionUpdate, 0xaa, 0xbb}
	if !bytes.Equal(frame, want) {
		t.Errorf("enabled+none = % x, want % x", frame, want)
	}
}

func TestEncodeFrameEnabledModeGzip(t *testing.T) {
	body := bytes.Repeat([]byte{0x42}, 256)
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionGzip)
	if frame[0] != CompressionGzip {
		t.Errorf("compression byte = %d, want CompressionGzip", frame[0])
	}
	if frame[1] != TagTransactionUpdate {
		t.Errorf("tag byte = %d, want TagTransactionUpdate", frame[1])
	}
	// gzip should have reduced the repetitive body meaningfully.
	if len(frame) >= 1+1+256 {
		t.Errorf("gzip frame too large: %d bytes for 256-byte repetitive body", len(frame))
	}
}

func TestUnwrapCompressedNoneEnvelope(t *testing.T) {
	body := []byte{0x01, 0x02, 0x03}
	frame := []byte{CompressionNone, TagSubscribeApplied, 0x01, 0x02, 0x03}
	tag, got, err := UnwrapCompressed(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscribeApplied {
		t.Errorf("tag = %d, want TagSubscribeApplied", tag)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body = % x, want % x", got, body)
	}
}

func TestUnwrapCompressedGzipRoundTrip(t *testing.T) {
	body := bytes.Repeat([]byte{0x55}, 1024)
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionGzip)
	tag, got, err := UnwrapCompressed(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagTransactionUpdate {
		t.Errorf("tag mismatch")
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body round-trip failed")
	}
}

func TestUnwrapCompressedUnknownByte(t *testing.T) {
	frame := []byte{0x02, TagSubscribeApplied, 0x01}
	_, _, err := UnwrapCompressed(frame)
	if !errors.Is(err, ErrUnknownCompressionTag) {
		t.Errorf("got %v, want ErrUnknownCompressionTag", err)
	}
}

func TestUnwrapCompressedGzipInvalid(t *testing.T) {
	// Valid compression byte + tag + invalid gzip payload.
	frame := []byte{CompressionGzip, TagTransactionUpdate, 0x00, 0x01, 0x02}
	_, _, err := UnwrapCompressed(frame)
	if !errors.Is(err, ErrDecompressionFailed) {
		t.Errorf("got %v, want ErrDecompressionFailed", err)
	}
}

func TestUnwrapCompressedEmptyBodyGzip(t *testing.T) {
	// gzip of empty body round-trips.
	frame := EncodeFrame(TagSubscribeApplied, nil, true, CompressionGzip)
	tag, body, err := UnwrapCompressed(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscribeApplied {
		t.Errorf("tag mismatch")
	}
	if len(body) != 0 {
		t.Errorf("body should be empty, got len %d", len(body))
	}
}

func TestUnwrapCompressedLargeBody(t *testing.T) {
	body := make([]byte, 1<<20)
	for i := range body {
		body[i] = byte(i % 256)
	}
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionGzip)
	_, got, err := UnwrapCompressed(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("1 MiB round-trip failed")
	}
}

func TestUnwrapCompressedTruncated(t *testing.T) {
	_, _, err := UnwrapCompressed(nil)
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("nil frame: got %v, want ErrMalformedMessage", err)
	}
	// Only compression byte, no tag.
	_, _, err = UnwrapCompressed([]byte{CompressionNone})
	if !errors.Is(err, ErrMalformedMessage) {
		t.Errorf("tag-less frame: got %v, want ErrMalformedMessage", err)
	}
}
