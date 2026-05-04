package protocol

import (
	"bytes"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestRowListEncodingDeterministicAndReadOnly(t *testing.T) {
	rows := [][]byte{{0x01, 0x02}, {}, {0x03, 0x04, 0x05}}
	before := cloneByteRows(rows)

	first := EncodeRowList(rows)
	second := EncodeRowList(rows)
	if !bytes.Equal(first, second) {
		t.Fatalf("EncodeRowList is not deterministic\nfirst:  % x\nsecond: % x", first, second)
	}
	if !byteRowsEqual(rows, before) {
		t.Fatalf("EncodeRowList mutated caller rows\n got: % x\nwant: % x", rows, before)
	}

	rows[0][0] = 0xff
	decoded, err := DecodeRowList(first)
	if err != nil {
		t.Fatalf("DecodeRowList(first): %v", err)
	}
	if !byteRowsEqual(decoded, before) {
		t.Fatalf("encoded RowList aliased caller rows after mutation\n got: % x\nwant: % x", decoded, before)
	}
}

func TestDecodeRowListReturnsDetachedRows(t *testing.T) {
	frame := EncodeRowList([][]byte{{0x0a, 0x0b}, {0x0c}})
	decoded, err := DecodeRowList(frame)
	if err != nil {
		t.Fatalf("DecodeRowList: %v", err)
	}

	frame[len(frame)-1] = 0xff
	if got := decoded[1][0]; got != 0x0c {
		t.Fatalf("decoded row aliases source frame: got 0x%02x, want 0x0c", got)
	}

	decoded[0][0] = 0xee
	again, err := DecodeRowList(EncodeRowList([][]byte{{0x0a, 0x0b}, {0x0c}}))
	if err != nil {
		t.Fatalf("DecodeRowList again: %v", err)
	}
	if got := again[0][0]; got != 0x0a {
		t.Fatalf("decoded row mutation leaked across decodes: got 0x%02x, want 0x0a", got)
	}
}

func TestEncodeProductRowsDeterministicAndReadOnly(t *testing.T) {
	rows := []types.ProductValue{
		{types.NewUint32(7), types.NewString("alice"), types.NewBytes([]byte{0x01, 0x02})},
		{types.NewUint32(8), types.NewString("bob"), types.NewArrayString([]string{"red", "blue"})},
	}
	before := types.CopyProductValues(rows)

	first, err := EncodeProductRows(rows)
	if err != nil {
		t.Fatalf("EncodeProductRows first: %v", err)
	}
	second, err := EncodeProductRows(rows)
	if err != nil {
		t.Fatalf("EncodeProductRows second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("EncodeProductRows is not deterministic\nfirst:  % x\nsecond: % x", first, second)
	}
	if !productRowsEqual(rows, before) {
		t.Fatalf("EncodeProductRows mutated caller rows")
	}

	first[0] = 0xff
	third, err := EncodeProductRows(rows)
	if err != nil {
		t.Fatalf("EncodeProductRows third: %v", err)
	}
	if !bytes.Equal(second, third) {
		t.Fatalf("mutating encoded output changed later encoding\nsecond: % x\nthird:  % x", second, third)
	}
}

func TestSubscriptionUpdateEncodingDeterministicAndReadOnly(t *testing.T) {
	update := []SubscriptionUpdate{
		{
			QueryID:   0x01020304,
			TableName: "users",
			Inserts:   EncodeRowList([][]byte{{0x01, 0x02}}),
			Deletes:   EncodeRowList([][]byte{{0x03}}),
		},
		{
			QueryID:   0x11121314,
			TableName: "rooms",
			Inserts:   EncodeRowList(nil),
			Deletes:   []byte{0xaa, 0xbb},
		},
	}
	before := cloneSubscriptionUpdates(update)

	var first bytes.Buffer
	writeSubscriptionUpdates(&first, update)
	var second bytes.Buffer
	writeSubscriptionUpdates(&second, update)

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("writeSubscriptionUpdates is not deterministic\nfirst:  % x\nsecond: % x", first.Bytes(), second.Bytes())
	}
	if !subscriptionUpdatesEqual(update, before) {
		t.Fatalf("writeSubscriptionUpdates mutated caller updates")
	}
}

func cloneByteRows(rows [][]byte) [][]byte {
	out := make([][]byte, len(rows))
	for i, row := range rows {
		out[i] = append([]byte(nil), row...)
	}
	return out
}

func byteRowsEqual(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func productRowsEqual(a, b []types.ProductValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func cloneSubscriptionUpdates(update []SubscriptionUpdate) []SubscriptionUpdate {
	out := make([]SubscriptionUpdate, len(update))
	for i, u := range update {
		out[i] = SubscriptionUpdate{
			QueryID:   u.QueryID,
			TableName: u.TableName,
			Inserts:   append([]byte(nil), u.Inserts...),
			Deletes:   append([]byte(nil), u.Deletes...),
		}
	}
	return out
}

func subscriptionUpdatesEqual(a, b []SubscriptionUpdate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].QueryID != b[i].QueryID ||
			a[i].TableName != b[i].TableName ||
			!bytes.Equal(a[i].Inserts, b[i].Inserts) ||
			!bytes.Equal(a[i].Deletes, b[i].Deletes) {
			return false
		}
	}
	return true
}
