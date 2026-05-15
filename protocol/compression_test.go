package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestEncodeFrameCompressionDisabled(t *testing.T) {
	body := []byte{0x01, 0x02, 0x03}
	frame := EncodeFrame(TagSubscribeSingleApplied, body, false, CompressionNone)
	want := append([]byte{TagSubscribeSingleApplied}, body...)
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
	body := bytes.Repeat([]byte{0x42}, DefaultGzipMinBytes)
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

func TestEncodeFrameGzipSmallBodyUsesNoneEnvelope(t *testing.T) {
	body := []byte("small")
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionGzip)
	want := append([]byte{CompressionNone, TagTransactionUpdate}, body...)
	if !bytes.Equal(frame, want) {
		t.Fatalf("small gzip frame = % x, want % x", frame, want)
	}
}

func TestUnwrapCompressedNoneEnvelope(t *testing.T) {
	body := []byte{0x01, 0x02, 0x03}
	frame := []byte{CompressionNone, TagSubscribeSingleApplied, 0x01, 0x02, 0x03}
	tag, got, err := UnwrapCompressed(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscribeSingleApplied {
		t.Errorf("tag = %d, want TagSubscribeSingleApplied", tag)
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
	// 0x03 is out-of-range (0x00=none, 0x01=brotli-reserved, 0x02=gzip).
	frame := []byte{0x03, TagSubscribeSingleApplied, 0x01}
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

func TestUnwrapCompressedEmptyBodyGzipModeUsesNoneEnvelope(t *testing.T) {
	// Gzip mode uses the uncompressed envelope for bodies below the threshold.
	frame := EncodeFrame(TagSubscribeSingleApplied, nil, true, CompressionGzip)
	if frame[0] != CompressionNone {
		t.Fatalf("compression byte = %d, want CompressionNone", frame[0])
	}
	tag, body, err := UnwrapCompressed(frame)
	if err != nil {
		t.Fatal(err)
	}
	if tag != TagSubscribeSingleApplied {
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

func TestUnwrapCompressedWithLimitRejectsOversizeNoneEnvelope(t *testing.T) {
	body := []byte{0x01, 0x02, 0x03}
	frame := []byte{CompressionNone, TagTransactionUpdate, 0x01, 0x02, 0x03}

	_, _, err := UnwrapCompressedWithLimit(frame, int64(len(body)))
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge", err)
	}

	tag, got, err := UnwrapCompressedWithLimit(frame, int64(len(body)+1))
	if err != nil {
		t.Fatalf("at-limit unwrap: %v", err)
	}
	if tag != TagTransactionUpdate || !bytes.Equal(got, body) {
		t.Fatalf("unwrap = (%d, %x), want (%d, %x)", tag, got, TagTransactionUpdate, body)
	}
}

func TestUnwrapCompressedWithLimitRejectsGzipExpansion(t *testing.T) {
	body := bytes.Repeat([]byte{0x42}, 256)
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionGzip)

	_, _, err := UnwrapCompressedWithLimit(frame, 64)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge", err)
	}

	_, got, err := UnwrapCompressedWithLimit(frame, int64(len(body)+1))
	if err != nil {
		t.Fatalf("at-limit unwrap: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body len = %d, want %d", len(got), len(body))
	}
}

func TestUnwrapCompressedUsesDefaultLimitForGzipExpansion(t *testing.T) {
	maxMessageSize := DefaultProtocolOptions().MaxMessageSize
	body := bytes.Repeat([]byte{0x42}, int(maxMessageSize))
	frame := EncodeFrame(TagTransactionUpdate, body, true, CompressionGzip)
	if frame[0] != CompressionGzip {
		t.Fatalf("compression byte = %d, want CompressionGzip", frame[0])
	}

	_, _, err := UnwrapCompressed(frame)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge", err)
	}

	_, got, err := UnwrapCompressedWithLimit(frame, 0)
	if err != nil {
		t.Fatalf("explicit unlimited unwrap: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body len = %d, want %d", len(got), len(body))
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

func TestCompressionEnvelopeConcurrentRoundTripShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc0ffeed5)
		workers    = 6
		iterations = 96
	)
	var varied [257]byte
	for i := range varied {
		varied[i] = byte((int(seed) + i*31) & 0xff)
	}
	bodies := [][]byte{
		nil,
		[]byte("short-body"),
		bytes.Repeat([]byte{0x42}, 128),
		varied[:],
	}
	tags := []uint8{
		TagSubscribeSingleApplied,
		TagTransactionUpdate,
		TagOneOffQueryResponse,
		TagTransactionUpdateLight,
	}
	modes := []uint8{CompressionNone, CompressionGzip}

	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	failures := make(chan string, workers*iterations)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			for op := range iterations {
				tag := tags[(int(seed)+worker*11+op*7)%len(tags)]
				mode := modes[(int(seed)+worker*13+op*5)%len(modes)]
				bodySeed := bodies[(int(seed)+worker*17+op*3)%len(bodies)]
				body := append([]byte(nil), bodySeed...)
				bodyBefore := append([]byte(nil), body...)

				frame, err := WrapCompressed(tag, body, mode)
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=WrapCompressed observed_error=%v expected=nil",
						seed, worker, op, workers, iterations, mode, err)
					continue
				}
				if !bytes.Equal(body, bodyBefore) {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=WrapCompressed observed=mutated-body expected=unchanged",
						seed, worker, op, workers, iterations, mode)
					continue
				}

				frameBefore := append([]byte(nil), frame...)
				gotTag, gotBody, err := UnwrapCompressed(frame)
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=UnwrapCompressed observed_error=%v expected=nil",
						seed, worker, op, workers, iterations, mode, err)
					continue
				}
				if !bytes.Equal(frame, frameBefore) {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=UnwrapCompressed observed=mutated-frame expected=unchanged",
						seed, worker, op, workers, iterations, mode)
					continue
				}
				if gotTag != tag || !bytes.Equal(gotBody, body) {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=round-trip observed=(tag=%d body=%x) expected=(tag=%d body=%x)",
						seed, worker, op, workers, iterations, mode, gotTag, gotBody, tag, body)
					continue
				}

				againTag, againBody, err := UnwrapCompressed(frame)
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=UnwrapCompressedAgain observed_error=%v expected=nil",
						seed, worker, op, workers, iterations, mode, err)
					continue
				}
				if againTag != tag || !bytes.Equal(againBody, body) {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d mode=%d operation=determinism observed=(tag=%d body=%x) expected=(tag=%d body=%x)",
						seed, worker, op, workers, iterations, mode, againTag, againBody, tag, body)
				}
				if (int(seed)+worker+op)%7 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}

	waitForSignals(t, ready, workers, "seed=0xc0ffeed5 compression-envelope workers started")
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestDecodeClientMessagePartsMatchesFrameDecoder(t *testing.T) {
	frame, err := EncodeClientMessage(SubscribeSingleMsg{
		RequestID:   7,
		QueryID:     11,
		QueryString: "SELECT * FROM players",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTag, wantMsg, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	gotMsg, err := decodeClientMessageParts(frame[0], frame[1:])
	if err != nil {
		t.Fatal(err)
	}
	if wantTag != frame[0] {
		t.Fatalf("tag = %d, want %d", wantTag, frame[0])
	}
	if got, ok := gotMsg.(SubscribeSingleMsg); !ok {
		t.Fatalf("decoded type = %T, want SubscribeSingleMsg", gotMsg)
	} else if want := wantMsg.(SubscribeSingleMsg); got != want {
		t.Fatalf("decoded msg = %+v, want %+v", got, want)
	}
}

func BenchmarkWrapCompressedGzip(b *testing.B) {
	body := bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz012345"), 64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		frame, err := WrapCompressed(TagTransactionUpdate, body, CompressionGzip)
		if err != nil {
			b.Fatal(err)
		}
		if len(frame) == 0 {
			b.Fatal("empty frame")
		}
	}
}

func BenchmarkUnwrapCompressedGzip(b *testing.B) {
	body := bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz012345"), 64)
	frame, err := WrapCompressed(TagTransactionUpdate, body, CompressionGzip)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tag, got, err := UnwrapCompressed(frame)
		if err != nil {
			b.Fatal(err)
		}
		if tag != TagTransactionUpdate || len(got) != len(body) {
			b.Fatal("bad round-trip")
		}
	}
}
