package protocol

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
)

// Compression byte values (SPEC-005 §3.3, parity-aligned with
// reference/SpacetimeDB
// crates/client-api-messages/src/websocket/common.rs
// SERVER_MSG_COMPRESSION_TAG_*).
const (
	CompressionNone   uint8 = 0x00
	CompressionBrotli uint8 = 0x01 // reserved; ErrBrotliUnsupported.
	CompressionGzip   uint8 = 0x02
)

// ErrUnknownCompressionTag is returned when the compression byte is
// not a recognized value.
var ErrUnknownCompressionTag = errors.New("protocol: unknown compression tag")

// ErrBrotliUnsupported is returned when a peer requests brotli
// compression. The tag is recognized (Phase 1 parity) but Shunter does
// not implement brotli; callers should treat it as a distinct protocol
// deferral rather than an unknown tag.
var ErrBrotliUnsupported = errors.New("protocol: brotli compression unsupported")

// ErrDecompressionFailed is returned when gzip decompression fails.
var ErrDecompressionFailed = errors.New("protocol: decompression failed")

// EncodeFrame wraps a message for transport. When compressionEnabled
// is false the frame is `[tag][body]` with no compression byte, per
// SPEC-005 §3.3 when compression is negotiated as none at connection
// setup. When true, an explicit compression byte is always present;
// mode controls whether the body is gzipped or passed through.
func EncodeFrame(tag uint8, body []byte, compressionEnabled bool, mode uint8) []byte {
	if !compressionEnabled {
		out := make([]byte, 1+len(body))
		out[0] = tag
		copy(out[1:], body)
		return out
	}
	frame, err := WrapCompressed(tag, body, mode)
	if err != nil {
		// WrapCompressed only errors on unknown mode; caller is
		// expected to pass a valid constant. Fall back to
		// uncompressed envelope so we never panic in delivery.
		fallback := make([]byte, 2+len(body))
		fallback[0] = CompressionNone
		fallback[1] = tag
		copy(fallback[2:], body)
		return fallback
	}
	return frame
}

// WrapCompressed applies the compression envelope:
// `[compression][tag][maybe-gzip(body)]`. Returns
// ErrUnknownCompressionTag if mode is not a known value.
func WrapCompressed(tag uint8, body []byte, mode uint8) ([]byte, error) {
	switch mode {
	case CompressionNone:
		out := make([]byte, 2+len(body))
		out[0] = CompressionNone
		out[1] = tag
		copy(out[2:], body)
		return out, nil
	case CompressionBrotli:
		return nil, ErrBrotliUnsupported
	case CompressionGzip:
		var buf bytes.Buffer
		buf.WriteByte(CompressionGzip)
		buf.WriteByte(tag)
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(body); err != nil {
			return nil, fmt.Errorf("gzip write: %w", err)
		}
		if err := gw.Close(); err != nil {
			return nil, fmt.Errorf("gzip close: %w", err)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("%w: mode=%d", ErrUnknownCompressionTag, mode)
	}
}

// UnwrapCompressed reads a compression-envelope frame and returns the
// tag + uncompressed body. Unknown compression byte maps to
// ErrUnknownCompressionTag; gzip failure maps to
// ErrDecompressionFailed.
func UnwrapCompressed(frame []byte) (uint8, []byte, error) {
	if len(frame) < 2 {
		return 0, nil, fmt.Errorf("%w: frame too short for compression envelope (len=%d)", ErrMalformedMessage, len(frame))
	}
	mode := frame[0]
	tag := frame[1]
	payload := frame[2:]
	switch mode {
	case CompressionNone:
		return tag, payload, nil
	case CompressionBrotli:
		return 0, nil, ErrBrotliUnsupported
	case CompressionGzip:
		gr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %v", ErrDecompressionFailed, err)
		}
		defer gr.Close()
		body, err := io.ReadAll(gr)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %v", ErrDecompressionFailed, err)
		}
		return tag, body, nil
	default:
		return 0, nil, fmt.Errorf("%w: mode=%d", ErrUnknownCompressionTag, mode)
	}
}
