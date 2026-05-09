package protocol

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"sync"
)

var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

var gzipReaderPool sync.Pool

// Compression byte values (SPEC-005 §3.3, aligned with
// reference/SpacetimeDB
// crates/client-api-messages/src/websocket/common.rs
// SERVER_MSG_COMPRESSION_TAG_*).
const (
	CompressionNone   uint8 = 0x00
	CompressionBrotli uint8 = 0x01 // reserved; ErrBrotliUnsupported.
	CompressionGzip   uint8 = 0x02
)

// DefaultGzipMinBytes is the minimum body size that is gzipped when gzip is
// negotiated. Smaller bodies keep the compression envelope but use
// CompressionNone because gzip headers usually cost more than they save.
const DefaultGzipMinBytes = 256

// ErrUnknownCompressionTag is returned when the compression byte is
// not a recognized value.
var ErrUnknownCompressionTag = errors.New("protocol: unknown compression tag")

// ErrBrotliUnsupported is returned when a peer requests brotli
// compression. The tag is recognized, but Shunter does
// not implement brotli; callers should treat it as a distinct protocol
// deferral rather than an unknown tag.
var ErrBrotliUnsupported = errors.New("protocol: brotli compression unsupported")

// ErrDecompressionFailed is returned when gzip decompression fails.
var ErrDecompressionFailed = errors.New("protocol: decompression failed")

// EncodeFrame wraps a message for transport. When compressionEnabled
// is false the frame is `[tag][body]` with no compression byte, per
// SPEC-005 §3.3 when compression is negotiated as none at connection
// setup. When true, an explicit compression byte is always present;
// mode controls whether the body may be gzipped or passed through.
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
// `[compression][tag][maybe-gzip(body)]`. Gzip mode uses CompressionNone for
// bodies smaller than DefaultGzipMinBytes. Returns ErrUnknownCompressionTag if
// mode is not a known value.
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
		if len(body) < DefaultGzipMinBytes {
			return WrapCompressed(tag, body, CompressionNone)
		}
		var buf bytes.Buffer
		buf.WriteByte(CompressionGzip)
		buf.WriteByte(tag)
		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(&buf)
		if _, err := gw.Write(body); err != nil {
			gw.Reset(io.Discard)
			gzipWriterPool.Put(gw)
			return nil, fmt.Errorf("gzip write: %w", err)
		}
		if err := gw.Close(); err != nil {
			gw.Reset(io.Discard)
			gzipWriterPool.Put(gw)
			return nil, fmt.Errorf("gzip close: %w", err)
		}
		gw.Reset(io.Discard)
		gzipWriterPool.Put(gw)
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
	return unwrapCompressed(frame, 0)
}

// UnwrapCompressedWithLimit reads a compression-envelope frame and returns the
// tag + uncompressed body. maxMessageSize, when positive, limits the logical
// uncompressed message size (tag byte plus body), matching the transport read
// limit applied to uncompressed connections.
func UnwrapCompressedWithLimit(frame []byte, maxMessageSize int64) (uint8, []byte, error) {
	return unwrapCompressed(frame, maxMessageSize)
}

func unwrapCompressed(frame []byte, maxMessageSize int64) (uint8, []byte, error) {
	if len(frame) < 2 {
		return 0, nil, fmt.Errorf("%w: frame too short for compression envelope (len=%d)", ErrMalformedMessage, len(frame))
	}
	mode := frame[0]
	tag := frame[1]
	payload := frame[2:]
	switch mode {
	case CompressionNone:
		if err := checkUncompressedMessageSize(len(payload), maxMessageSize); err != nil {
			return 0, nil, err
		}
		return tag, payload, nil
	case CompressionBrotli:
		return 0, nil, ErrBrotliUnsupported
	case CompressionGzip:
		var (
			gr  *gzip.Reader
			err error
		)
		if pooled := gzipReaderPool.Get(); pooled != nil {
			gr = pooled.(*gzip.Reader)
			err = gr.Reset(bytes.NewReader(payload))
		} else {
			gr, err = gzip.NewReader(bytes.NewReader(payload))
		}
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %v", ErrDecompressionFailed, err)
		}
		maxBodySize, limited := bodyLimit(maxMessageSize)
		body, err := readGzipBody(gr, maxBodySize, limited)
		closeErr := gr.Close()
		gzipReaderPool.Put(gr)
		if err != nil {
			if errors.Is(err, ErrMessageTooLarge) {
				return 0, nil, err
			}
			return 0, nil, fmt.Errorf("%w: %v", ErrDecompressionFailed, err)
		}
		if closeErr != nil {
			return 0, nil, fmt.Errorf("%w: %v", ErrDecompressionFailed, closeErr)
		}
		return tag, body, nil
	default:
		return 0, nil, fmt.Errorf("%w: mode=%d", ErrUnknownCompressionTag, mode)
	}
}

func checkUncompressedMessageSize(bodyLen int, maxMessageSize int64) error {
	if maxMessageSize <= 0 {
		return nil
	}
	if int64(bodyLen)+1 > maxMessageSize {
		return fmt.Errorf("%w: uncompressed message size %d exceeds limit %d", ErrMessageTooLarge, int64(bodyLen)+1, maxMessageSize)
	}
	return nil
}

func bodyLimit(maxMessageSize int64) (int64, bool) {
	if maxMessageSize <= 0 {
		return 0, false
	}
	if maxMessageSize == 1 {
		return 0, true
	}
	return maxMessageSize - 1, true
}

func readGzipBody(gr *gzip.Reader, maxBodySize int64, limited bool) ([]byte, error) {
	if !limited {
		return io.ReadAll(gr)
	}
	lr := &io.LimitedReader{R: gr, N: maxBodySize + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBodySize {
		return nil, fmt.Errorf("%w: uncompressed message size exceeds limit", ErrMessageTooLarge)
	}
	return body, nil
}
