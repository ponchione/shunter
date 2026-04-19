package protocol

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestPhase1ParityCompressionTagByteValues pins the reference byte
// numbering: 0x00 none, 0x01 brotli (reserved, unsupported), 0x02
// gzip. Reference outcome matched:
// crates/client-api-messages/src/websocket/common.rs
// SERVER_MSG_COMPRESSION_TAG_{NONE,BROTLI,GZIP}.
func TestPhase1ParityCompressionTagByteValues(t *testing.T) {
	if CompressionNone != 0x00 {
		t.Errorf("CompressionNone = 0x%02x, want 0x00", CompressionNone)
	}
	if CompressionBrotli != 0x01 {
		t.Errorf("CompressionBrotli = 0x%02x, want 0x01",
			CompressionBrotli)
	}
	if CompressionGzip != 0x02 {
		t.Errorf("CompressionGzip = 0x%02x, want 0x02", CompressionGzip)
	}
}

// TestPhase1ParityCompressionGzipEnvelopeByte pins the over-the-wire
// byte sequence so a reference-compatible client sees gzip signaled as
// 0x02.
func TestPhase1ParityCompressionGzipEnvelopeByte(t *testing.T) {
	frame, err := WrapCompressed(TagTransactionUpdate, []byte("body"),
		CompressionGzip)
	if err != nil {
		t.Fatalf("WrapCompressed gzip: %v", err)
	}
	if len(frame) < 2 {
		t.Fatalf("frame too short: %d", len(frame))
	}
	if frame[0] != 0x02 {
		t.Errorf("compression byte = 0x%02x, want 0x02 (gzip)", frame[0])
	}
	if frame[1] != TagTransactionUpdate {
		t.Errorf("tag byte = 0x%02x, want 0x%02x",
			frame[1], TagTransactionUpdate)
	}
	gr, err := gzip.NewReader(bytes.NewReader(frame[2:]))
	if err != nil {
		t.Fatalf("gzip decode: %v", err)
	}
	defer gr.Close()
}

// TestPhase1ParityCompressionBrotliReservedRejected pins the deferral:
// brotli is recognized as a known tag but Shunter does not implement
// it. Server-side emit must reject it; decode must return a dedicated
// ErrBrotliUnsupported (distinct from ErrUnknownCompressionTag) so
// callers can distinguish "reserved-but-unimplemented" from "bogus
// byte".
func TestPhase1ParityCompressionBrotliReservedRejected(t *testing.T) {
	_, err := WrapCompressed(TagTransactionUpdate, []byte("body"),
		CompressionBrotli)
	if err == nil {
		t.Fatal("WrapCompressed brotli: want error, got nil")
	}
	if !errors.Is(err, ErrBrotliUnsupported) {
		t.Errorf("err = %v, want ErrBrotliUnsupported", err)
	}

	// A frame arriving with 0x01 from a peer must decode to the same
	// reserved-unsupported error so the dispatch loop can close with
	// a specific reason.
	frame := []byte{CompressionBrotli, TagSubscribeSingle, 0xAA}
	_, _, derr := UnwrapCompressed(frame)
	if !errors.Is(derr, ErrBrotliUnsupported) {
		t.Errorf("UnwrapCompressed brotli err = %v, want ErrBrotliUnsupported",
			derr)
	}
}

// TestPhase1ParityBrotliFrameClosesWithReason drives a brotli-tagged
// frame into the dispatch loop and asserts the connection is closed
// with code 1002 and a reason string containing "brotli". Mirrors the
// pattern of TestUnknownCompressionTag_Closes1002 in close_test.go.
func TestPhase1ParityBrotliFrameClosesWithReason(t *testing.T) {
	opts := DefaultProtocolOptions()
	conn, clientWS := testConnPair(t, &opts)
	conn.Compression = true // enable compression path

	handlers := &MessageHandlers{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send binary frame with brotli compression byte (0x01).
	wCtx, wCancel := context.WithTimeout(ctx, time.Second)
	_ = clientWS.Write(wCtx, websocket.MessageBinary, []byte{CompressionBrotli, TagSubscribeSingle, 0x00})
	wCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on brotli compression tag")
	}

	readCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rCancel()
	_, _, err := clientWS.Read(readCtx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CloseError, got %v (%T)", err, err)
	}
	if ce.Code != websocket.StatusProtocolError {
		t.Errorf("close code = %d, want %d (1002)", ce.Code, websocket.StatusProtocolError)
	}
	if !strings.Contains(ce.Reason, "brotli") {
		t.Errorf("close reason = %q, want contains %q", ce.Reason, "brotli")
	}
}
