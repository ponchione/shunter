package commitlog

import (
	"bytes"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func FuzzDecodeRecord(f *testing.F) {
	for _, seed := range recordFuzzSeeds(f) {
		f.Add(seed)
	}

	const maxPayload = uint32(4096)
	f.Fuzz(func(t *testing.T, data []byte) {
		rec, err := DecodeRecord(bytes.NewReader(data), maxPayload)
		if err != nil {
			return
		}
		if rec.RecordType != RecordTypeChangeset {
			t.Fatalf("decoded record type = %d, want changeset", rec.RecordType)
		}
		if rec.Flags != 0 {
			t.Fatalf("decoded flags = %d, want 0", rec.Flags)
		}
		if len(rec.Payload) > int(maxPayload) {
			t.Fatalf("decoded payload len = %d, want <= %d", len(rec.Payload), maxPayload)
		}

		var encoded bytes.Buffer
		if err := EncodeRecord(&encoded, rec); err != nil {
			t.Fatalf("re-encode decoded record: %v", err)
		}
		roundTrip, err := DecodeRecord(bytes.NewReader(encoded.Bytes()), maxPayload)
		if err != nil {
			t.Fatalf("decode re-encoded record: %v", err)
		}
		if roundTrip.TxID != rec.TxID || roundTrip.RecordType != rec.RecordType || roundTrip.Flags != rec.Flags || !bytes.Equal(roundTrip.Payload, rec.Payload) {
			t.Fatalf("record round-trip mismatch: before=%+v after=%+v", rec, roundTrip)
		}
	})
}

func FuzzDecodeChangeset(f *testing.F) {
	_, reg := testSchema()
	for _, seed := range changesetFuzzSeeds(f) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		cs, err := DecodeChangeset(data, reg)
		if err != nil {
			return
		}
		encoded, err := EncodeChangeset(cs)
		if err != nil {
			t.Fatalf("re-encode decoded changeset: %v", err)
		}
		roundTrip, err := DecodeChangeset(encoded, reg)
		if err != nil {
			t.Fatalf("decode re-encoded changeset: %v", err)
		}
		assertChangesetsEquivalent(t, cs, roundTrip)
	})
}

func recordFuzzSeeds(t testing.TB) [][]byte {
	t.Helper()
	var seeds [][]byte
	seeds = append(seeds, nil)
	seeds = append(seeds, make([]byte, RecordHeaderSize))

	valid := encodeRecordSeed(t, &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("payload")})
	seeds = append(seeds, valid)
	seeds = append(seeds, valid[:RecordHeaderSize-1])
	seeds = append(seeds, valid[:len(valid)-1])

	corrupt := append([]byte(nil), valid...)
	corrupt[len(corrupt)-1] ^= 0xff
	seeds = append(seeds, corrupt)

	tooLarge := append([]byte(nil), valid[:RecordHeaderSize]...)
	tooLarge[10] = 0xff
	tooLarge[11] = 0xff
	tooLarge[12] = 0xff
	tooLarge[13] = 0x7f
	seeds = append(seeds, tooLarge)

	unknownType := encodeRecordSeed(t, &Record{TxID: 2, RecordType: RecordTypeChangeset + 1, Payload: []byte("x")})
	seeds = append(seeds, unknownType)
	badFlags := encodeRecordSeed(t, &Record{TxID: 3, RecordType: RecordTypeChangeset, Flags: 1, Payload: []byte("x")})
	seeds = append(seeds, badFlags)
	return seeds
}

func changesetFuzzSeeds(t testing.TB) [][]byte {
	t.Helper()
	var seeds [][]byte
	seeds = append(seeds, nil)
	seeds = append(seeds, []byte{changesetVersion})
	seeds = append(seeds, []byte{changesetVersion, 0, 0, 0, 0})
	seeds = append(seeds, []byte{changesetVersion + 1, 0, 0, 0, 0})

	empty := &store.Changeset{Tables: map[schema.TableID]*store.TableChangeset{}}
	seeds = append(seeds, encodeChangesetSeed(t, empty))
	withRows := &store.Changeset{
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}},
				Deletes:   []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}},
			},
		},
	}
	valid := encodeChangesetSeed(t, withRows)
	seeds = append(seeds, valid)
	seeds = append(seeds, valid[:len(valid)-1])
	unknownTable := append([]byte(nil), []byte{changesetVersion, 1, 0, 0, 0, 99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	seeds = append(seeds, unknownTable)
	return seeds
}

func encodeRecordSeed(t testing.TB, rec *Record) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeChangesetSeed(t testing.TB, cs *store.Changeset) []byte {
	t.Helper()
	data, err := EncodeChangeset(cs)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertChangesetsEquivalent(t *testing.T, a, b *store.Changeset) {
	t.Helper()
	if len(a.Tables) != len(b.Tables) {
		t.Fatalf("table count = %d, want %d", len(b.Tables), len(a.Tables))
	}
	for tableID, aTable := range a.Tables {
		bTable, ok := b.Tables[tableID]
		if !ok {
			t.Fatalf("table %d missing after round-trip", tableID)
		}
		if aTable.TableName != bTable.TableName {
			t.Fatalf("table %d name = %q, want %q", tableID, bTable.TableName, aTable.TableName)
		}
		assertRowsEquivalent(t, "inserts", aTable.Inserts, bTable.Inserts)
		assertRowsEquivalent(t, "deletes", aTable.Deletes, bTable.Deletes)
	}
}

func assertRowsEquivalent(t *testing.T, label string, a, b []types.ProductValue) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s row count = %d, want %d", label, len(b), len(a))
	}
	for i := range a {
		if !productValuesEqual(a[i], b[i]) {
			t.Fatalf("%s[%d] = %v, want %v", label, i, b[i], a[i])
		}
	}
}

func productValuesEqual(a, b types.ProductValue) bool {
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
