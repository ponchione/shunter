package protocol

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/ponchione/shunter/types"
)

type compressionCorpusFixture struct {
	name  string
	tag   uint8
	body  []byte
	plain []byte
}

type compressionBenchCodec struct {
	name   string
	wrap   func(tag uint8, body []byte) ([]byte, error)
	unwrap func(frame []byte) (uint8, []byte, error)
	decode func(frame []byte) (uint8, any, error)
}

var (
	compressionBenchFrameSink []byte
	compressionBenchTagSink   uint8
	compressionBenchMsgSink   any
)

func TestCompressionCorpusCodecRoundTrip(t *testing.T) {
	for _, fixture := range newCompressionCorpusFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			for _, codec := range newCompressionBenchCodecs() {
				t.Run(codec.name, func(t *testing.T) {
					frame, err := codec.wrap(fixture.tag, fixture.body)
					if err != nil {
						t.Fatalf("wrap: %v", err)
					}
					tag, body, err := codec.unwrap(frame)
					if err != nil {
						t.Fatalf("unwrap: %v", err)
					}
					if tag != fixture.tag {
						t.Fatalf("tag = %d, want %d", tag, fixture.tag)
					}
					if !bytes.Equal(body, fixture.body) {
						t.Fatalf("body mismatch after round-trip: got len %d, want %d", len(body), len(fixture.body))
					}
				})
			}
		})
	}
}

func BenchmarkCompressionCorpusEncode(b *testing.B) {
	fixtures := newCompressionCorpusFixtures(b)
	codecs := newCompressionBenchCodecs()

	for _, fixture := range fixtures {
		fixture := fixture
		for _, codec := range codecs {
			codec := codec
			b.Run(fixture.name+"/"+codec.name, func(b *testing.B) {
				frame, err := codec.wrap(fixture.tag, fixture.body)
				if err != nil {
					b.Fatalf("metric wrap: %v", err)
				}
				plainLen := len(fixture.plain)
				wireLen := len(frame)
				b.SetBytes(int64(len(fixture.plain)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					frame, err := codec.wrap(fixture.tag, fixture.body)
					if err != nil {
						b.Fatalf("wrap: %v", err)
					}
					if len(frame) == 0 {
						b.Fatal("empty frame")
					}
					compressionBenchFrameSink = frame
				}
				b.StopTimer()
				reportCompressionCorpusMetrics(b, plainLen, wireLen)
			})
		}
	}
}

func BenchmarkCompressionCorpusDecode(b *testing.B) {
	fixtures := newCompressionCorpusFixtures(b)
	codecs := newCompressionBenchCodecs()

	for _, fixture := range fixtures {
		fixture := fixture
		for _, codec := range codecs {
			codec := codec
			frame, err := codec.wrap(fixture.tag, fixture.body)
			if err != nil {
				b.Fatalf("%s/%s setup wrap: %v", fixture.name, codec.name, err)
			}
			b.Run(fixture.name+"/"+codec.name, func(b *testing.B) {
				plainLen := len(fixture.plain)
				wireLen := len(frame)
				b.SetBytes(int64(len(fixture.plain)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					tag, msg, err := codec.decode(frame)
					if err != nil {
						b.Fatalf("decode: %v", err)
					}
					if tag != fixture.tag || msg == nil {
						b.Fatalf("decoded tag/msg = (%d, %T), want tag %d", tag, msg, fixture.tag)
					}
					compressionBenchTagSink = tag
					compressionBenchMsgSink = msg
				}
				b.StopTimer()
				reportCompressionCorpusMetrics(b, plainLen, wireLen)
			})
		}
	}
}

func newCompressionBenchCodecs() []compressionBenchCodec {
	return []compressionBenchCodec{
		{
			name:   "plain",
			wrap:   wrapCompressionBenchPlain,
			unwrap: unwrapCompressionBenchPlain,
			decode: decodeCompressionBenchPlain,
		},
		{
			name:   "none_envelope",
			wrap:   wrapCompressionBenchNone,
			unwrap: UnwrapCompressed,
			decode: decodeCompressionBenchEnvelope,
		},
		{
			name:   "gzip",
			wrap:   wrapCompressionBenchGzip,
			unwrap: UnwrapCompressed,
			decode: decodeCompressionBenchEnvelope,
		},
		{
			name:   "brotli_q1",
			wrap:   wrapCompressionBenchBrotli(1),
			unwrap: unwrapCompressionBenchBrotli,
			decode: decodeCompressionBenchBrotli,
		},
		{
			name:   "brotli_q4",
			wrap:   wrapCompressionBenchBrotli(4),
			unwrap: unwrapCompressionBenchBrotli,
			decode: decodeCompressionBenchBrotli,
		},
	}
}

func wrapCompressionBenchPlain(tag uint8, body []byte) ([]byte, error) {
	return EncodeFrame(tag, body, false, CompressionNone), nil
}

func unwrapCompressionBenchPlain(frame []byte) (uint8, []byte, error) {
	if len(frame) < 1 {
		return 0, nil, fmt.Errorf("%w: empty plain frame", ErrMalformedMessage)
	}
	return frame[0], frame[1:], nil
}

func decodeCompressionBenchPlain(frame []byte) (uint8, any, error) {
	return DecodeServerMessage(frame)
}

func wrapCompressionBenchNone(tag uint8, body []byte) ([]byte, error) {
	return WrapCompressed(tag, body, CompressionNone)
}

func wrapCompressionBenchGzip(tag uint8, body []byte) ([]byte, error) {
	return WrapCompressed(tag, body, CompressionGzip)
}

func wrapCompressionBenchBrotli(quality int) func(tag uint8, body []byte) ([]byte, error) {
	return func(tag uint8, body []byte) ([]byte, error) {
		var buf bytes.Buffer
		buf.Grow(2 + len(body))
		buf.WriteByte(CompressionBrotli)
		buf.WriteByte(tag)
		w := brotli.NewWriterOptions(&buf, brotli.WriterOptions{Quality: quality})
		if _, err := w.Write(body); err != nil {
			_ = w.Close()
			return nil, fmt.Errorf("brotli q%d write: %w", quality, err)
		}
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("brotli q%d close: %w", quality, err)
		}
		return buf.Bytes(), nil
	}
}

func unwrapCompressionBenchBrotli(frame []byte) (uint8, []byte, error) {
	if len(frame) < 2 {
		return 0, nil, fmt.Errorf("%w: frame too short for brotli envelope (len=%d)", ErrMalformedMessage, len(frame))
	}
	if frame[0] != CompressionBrotli {
		return 0, nil, fmt.Errorf("%w: brotli candidate frame mode=%d", ErrUnknownCompressionTag, frame[0])
	}
	body, err := io.ReadAll(brotli.NewReader(bytes.NewReader(frame[2:])))
	if err != nil {
		return 0, nil, fmt.Errorf("brotli read: %w", err)
	}
	return frame[1], body, nil
}

func decodeCompressionBenchEnvelope(frame []byte) (uint8, any, error) {
	tag, body, err := UnwrapCompressed(frame)
	if err != nil {
		return 0, nil, err
	}
	return decodeCompressionBenchUnwrapped(tag, body)
}

func decodeCompressionBenchBrotli(frame []byte) (uint8, any, error) {
	tag, body, err := unwrapCompressionBenchBrotli(frame)
	if err != nil {
		return 0, nil, err
	}
	return decodeCompressionBenchUnwrapped(tag, body)
}

func decodeCompressionBenchUnwrapped(tag uint8, body []byte) (uint8, any, error) {
	frame := make([]byte, 1+len(body))
	frame[0] = tag
	copy(frame[1:], body)
	return DecodeServerMessage(frame)
}

func reportCompressionCorpusMetrics(b *testing.B, plainLen, wireLen int) {
	b.Helper()
	wirePct := float64(wireLen) * 100 / float64(plainLen)
	b.ReportMetric(float64(wireLen), "wire_B/op")
	b.ReportMetric(wirePct, "wire_pct")
	b.ReportMetric(100-wirePct, "saved_pct")
}

func newCompressionCorpusFixtures(tb testing.TB) []compressionCorpusFixture {
	tb.Helper()

	repetitiveRows := EncodeRowList([][]byte{
		bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz012345"), 64),
	})
	largeInitialRows := mustCompressionProductRows(tb, "subscribe_single_large_initial", compressionMixedRows(384, 0))
	multiUsers := mustCompressionProductRows(tb, "subscribe_multi_users", compressionMixedRows(96, 1_000))
	multiOrders := mustCompressionProductRows(tb, "subscribe_multi_orders", compressionMixedRows(128, 2_000))
	multiMessages := mustCompressionProductRows(tb, "subscribe_multi_messages", compressionStringRows(72, 3_000))
	lightInsertA := mustCompressionProductRows(tb, "tx_light_insert_a", compressionMixedRows(72, 4_000))
	lightDeleteA := mustCompressionProductRows(tb, "tx_light_delete_a", compressionMixedRows(48, 5_000))
	lightInsertB := mustCompressionProductRows(tb, "tx_light_insert_b", compressionStringRows(64, 6_000))
	lightDeleteB := mustCompressionProductRows(tb, "tx_light_delete_b", compressionStringRows(32, 7_000))
	heavyRows := mustCompressionProductRows(tb, "tx_heavy_rows", compressionMixedRows(96, 8_000))
	heavyDeletes := mustCompressionProductRows(tb, "tx_heavy_deletes", compressionMixedRows(24, 9_000))
	oneOffUsers := mustCompressionProductRows(tb, "oneoff_users", compressionMixedRows(80, 10_000))
	oneOffOrders := mustCompressionProductRows(tb, "oneoff_orders", compressionMixedRows(112, 11_000))
	oneOffMessages := mustCompressionProductRows(tb, "oneoff_messages", compressionStringRows(64, 12_000))
	oneOffBlobs := mustCompressionProductRows(tb, "oneoff_blobs", compressionBytesRows(48, 48, 13_000, 0x0ff1ce))
	stringRows := mustCompressionProductRows(tb, "string_heavy_rows", compressionStringRows(192, 14_000))
	mixedRows := mustCompressionProductRows(tb, "mixed_rows", compressionMixedRows(256, 15_000))
	randomRows := mustCompressionProductRows(tb, "random_bytes_rows", compressionBytesRows(128, 96, 16_000, 0xbadc0ffee))

	heavy := TransactionUpdate{
		Status: StatusCommitted{Update: []SubscriptionUpdate{
			{QueryID: 801, TableName: "orders", Inserts: heavyRows, Deletes: heavyDeletes},
			{QueryID: 802, TableName: "order_events", Inserts: lightInsertB},
		}},
		Timestamp: 1_700_000_123_456_789,
		ReducerCall: ReducerCallInfo{
			ReducerName: "submit_order_batch_with_metadata",
			ReducerID:   42,
			Args:        compressionReducerArgs(3_584),
			RequestID:   9001,
		},
		TotalHostExecutionDuration: 12_345,
	}
	for i := range heavy.CallerIdentity {
		heavy.CallerIdentity[i] = byte(0x40 + i%29)
	}
	for i := range heavy.CallerConnectionID {
		heavy.CallerConnectionID[i] = byte(0xa0 + i)
	}

	cases := []struct {
		name string
		msg  any
	}{
		{
			name: "tiny_unsubscribe_single",
			msg: UnsubscribeSingleApplied{
				RequestID:                        1,
				TotalHostExecutionDurationMicros: 7,
				QueryID:                          2,
			},
		},
		{
			name: "repetitive_2kib_light_update",
			msg: TransactionUpdateLight{
				RequestID: 100,
				Update: []SubscriptionUpdate{
					{QueryID: 101, TableName: "synthetic_repetitive", Inserts: repetitiveRows},
				},
			},
		},
		{
			name: "subscribe_single_large_initial",
			msg: SubscribeSingleApplied{
				RequestID:                        200,
				TotalHostExecutionDurationMicros: 1_250,
				QueryID:                          201,
				TableName:                        "orders",
				Rows:                             largeInitialRows,
			},
		},
		{
			name: "subscribe_multi_multi_table",
			msg: SubscribeMultiApplied{
				RequestID:                        300,
				TotalHostExecutionDurationMicros: 1_750,
				QueryID:                          301,
				Update: []SubscriptionUpdate{
					{QueryID: 302, TableName: "users", Inserts: multiUsers},
					{QueryID: 303, TableName: "orders", Inserts: multiOrders},
					{QueryID: 304, TableName: "messages", Inserts: multiMessages},
				},
			},
		},
		{
			name: "tx_light_many_changes",
			msg: TransactionUpdateLight{
				RequestID: 400,
				Update: []SubscriptionUpdate{
					{QueryID: 401, TableName: "orders", Inserts: lightInsertA, Deletes: lightDeleteA},
					{QueryID: 402, TableName: "messages", Inserts: lightInsertB, Deletes: lightDeleteB},
					{QueryID: 403, TableName: "inventory", Inserts: multiOrders, Deletes: multiUsers},
				},
			},
		},
		{
			name: "tx_heavy_reducer_args",
			msg:  heavy,
		},
		{
			name: "oneoff_several_tables",
			msg: OneOffQueryResponse{
				MessageID: []byte("oneoff-compression-corpus-001"),
				Tables: []OneOffTable{
					{TableName: "users", Rows: oneOffUsers},
					{TableName: "orders", Rows: oneOffOrders},
					{TableName: "messages", Rows: oneOffMessages},
					{TableName: "attachments", Rows: oneOffBlobs},
				},
				TotalHostExecutionDuration: 2_400,
			},
		},
		{
			name: "string_heavy_rows",
			msg: SubscribeSingleApplied{
				RequestID:                        500,
				TotalHostExecutionDurationMicros: 2_750,
				QueryID:                          501,
				TableName:                        "chat_messages",
				Rows:                             stringRows,
			},
		},
		{
			name: "mixed_rows",
			msg: SubscribeSingleApplied{
				RequestID:                        600,
				TotalHostExecutionDurationMicros: 3_125,
				QueryID:                          601,
				TableName:                        "order_projection",
				Rows:                             mixedRows,
			},
		},
		{
			name: "random_bytes_rows",
			msg: SubscribeSingleApplied{
				RequestID:                        700,
				TotalHostExecutionDurationMicros: 3_500,
				QueryID:                          701,
				TableName:                        "attachment_chunks",
				Rows:                             randomRows,
			},
		},
	}

	fixtures := make([]compressionCorpusFixture, 0, len(cases))
	for _, tc := range cases {
		frame, err := EncodeServerMessage(tc.msg)
		if err != nil {
			tb.Fatalf("%s EncodeServerMessage: %v", tc.name, err)
		}
		if len(frame) == 0 {
			tb.Fatalf("%s EncodeServerMessage returned empty frame", tc.name)
		}
		if _, _, err := DecodeServerMessage(frame); err != nil {
			tb.Fatalf("%s DecodeServerMessage: %v", tc.name, err)
		}
		fixtures = append(fixtures, compressionCorpusFixture{
			name:  tc.name,
			tag:   frame[0],
			body:  append([]byte(nil), frame[1:]...),
			plain: append([]byte(nil), frame...),
		})
	}

	if len(fixtures[0].body) >= DefaultGzipMinBytes {
		tb.Fatalf("%s body len = %d, want below DefaultGzipMinBytes=%d", fixtures[0].name, len(fixtures[0].body), DefaultGzipMinBytes)
	}
	return fixtures
}

func mustCompressionProductRows(tb testing.TB, name string, rows []types.ProductValue) []byte {
	tb.Helper()
	encoded, err := EncodeProductRows(rows)
	if err != nil {
		tb.Fatalf("%s EncodeProductRows: %v", name, err)
	}
	return encoded
}

func compressionStringRows(n, start int) []types.ProductValue {
	subjects := []string{"status update", "order ready", "joined room", "payment authorized", "delivery scheduled"}
	rows := make([]types.ProductValue, n)
	for i := range rows {
		id := start + i + 1
		rows[i] = types.ProductValue{
			types.NewUint64(uint64(id)),
			types.NewString(fmt.Sprintf("tenant-%02d", id%8)),
			types.NewString(fmt.Sprintf("channel-%02d", id%16)),
			types.NewString(subjects[id%len(subjects)]),
			types.NewString(compressionLongText(id)),
			types.NewArrayString([]string{
				fmt.Sprintf("tag-%02d", id%7),
				"visible",
				fmt.Sprintf("bucket-%02d", id%5),
			}),
		}
	}
	return rows
}

func compressionMixedRows(n, start int) []types.ProductValue {
	statuses := []string{"open", "paid", "packed", "shipped", "settled", "archived"}
	rows := make([]types.ProductValue, n)
	for i := range rows {
		id := start + i + 1
		rows[i] = types.ProductValue{
			types.NewUint64(uint64(id)),
			types.NewUint64(uint64(id%97 + 1)),
			types.NewBool(id%5 != 0),
			types.NewUint32(uint32((id%113 + 1) * 10)),
			types.NewString(statuses[id%len(statuses)]),
			types.NewString(fmt.Sprintf("owner-%03d", id%64)),
			types.NewBytesOwned(compressionPatternBytes(24, id)),
			types.NewTimestamp(1_700_000_000_000_000 + int64(id)*1_000),
			types.NewDuration(int64(id%300) * 1_000_000),
		}
	}
	return rows
}

func compressionBytesRows(n, width, start int, seed uint64) []types.ProductValue {
	rows := make([]types.ProductValue, n)
	for i := range rows {
		id := start + i + 1
		rows[i] = types.ProductValue{
			types.NewUint64(uint64(id)),
			types.NewBytesOwned(compressionDeterministicBytes(width, seed+uint64(i)*0x9e3779b97f4a7c15)),
			types.NewBytesOwned(compressionDeterministicBytes(width/2, seed^uint64(id)*0xbf58476d1ce4e5b9)),
		}
	}
	return rows
}

func compressionLongText(id int) string {
	base := fmt.Sprintf("entity=%03d state=%02d shard=%02d event=subscription_delta ", id%128, id%11, id%9)
	return strings.Repeat(base, 2+id%4)
}

func compressionPatternBytes(n, id int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte((id + i%8) & 0xff)
	}
	return out
}

func compressionDeterministicBytes(n int, seed uint64) []byte {
	out := make([]byte, n)
	x := seed
	for i := range out {
		x = x*6364136223846793005 + 1442695040888963407
		out[i] = byte(x >> 56)
	}
	return out
}

func compressionReducerArgs(targetBytes int) []byte {
	var b strings.Builder
	b.Grow(targetBytes + 128)
	b.WriteString(`{"reducer":"submit_order_batch","orders":[`)
	for i := 0; b.Len() < targetBytes; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf(`{"sku":"sku-%03d","qty":%d,"region":"r-%02d","note":"repeatable reducer metadata"}`,
			i%64, i%9+1, i%12))
	}
	b.WriteString(`],"source":"compression-corpus"}`)
	return []byte(b.String())
}
