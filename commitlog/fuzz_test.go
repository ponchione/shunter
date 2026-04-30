package commitlog

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
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

func FuzzReadSnapshot(f *testing.F) {
	for _, seed := range snapshotFuzzSeeds(f) {
		f.Add(seed)
	}

	const maxSnapshotBytes = 64 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxSnapshotBytes {
			t.Skip("snapshot fuzz input above bounded local limit")
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, snapshotFileName), data, 0o644); err != nil {
			t.Fatal(err)
		}

		snapshot, err := ReadSnapshot(dir)
		if err != nil {
			return
		}
		schemaByID := make(map[schema.TableID]schema.TableSchema, len(snapshot.Schema))
		for _, table := range snapshot.Schema {
			schemaByID[table.ID] = table
		}
		for _, tableData := range snapshot.Tables {
			tableSchema, ok := schemaByID[tableData.TableID]
			if !ok {
				t.Fatalf("snapshot table %d missing from schema", tableData.TableID)
			}
			for rowIdx, row := range tableData.Rows {
				if len(row) != len(tableSchema.Columns) {
					t.Fatalf("table %d row %d width = %d, want %d", tableData.TableID, rowIdx, len(row), len(tableSchema.Columns))
				}
			}
		}
	})
}

func FuzzOpenOffsetIndex(f *testing.F) {
	for _, seed := range offsetIndexFuzzSeeds() {
		f.Add(seed)
	}

	const maxIndexBytes = 64 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxIndexBytes {
			t.Skip("offset index fuzz input above bounded local limit")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, OffsetIndexFileName(1))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		idx, err := OpenOffsetIndex(path)
		if err != nil {
			return
		}
		defer idx.Close()

		entries, err := idx.Entries()
		if err != nil {
			t.Fatalf("read accepted offset index entries: %v", err)
		}
		if idx.NumEntries() != uint64(len(entries)) {
			t.Fatalf("NumEntries = %d, want %d", idx.NumEntries(), len(entries))
		}
		var last types.TxID
		for _, entry := range entries {
			if entry.TxID == 0 || entry.ByteOffset == 0 {
				t.Fatalf("accepted sentinel entry: %+v", entry)
			}
			if entry.TxID <= last {
				t.Fatalf("accepted non-monotonic entries: %+v", entries)
			}
			key, off, err := idx.KeyLookup(entry.TxID)
			if err != nil {
				t.Fatalf("lookup accepted entry %d: %v", entry.TxID, err)
			}
			if key != entry.TxID || off != entry.ByteOffset {
				t.Fatalf("lookup(%d) = (%d, %d), want (%d, %d)", entry.TxID, key, off, entry.TxID, entry.ByteOffset)
			}
			last = entry.TxID
		}
	})
}

func FuzzScanSingleSegment(f *testing.F) {
	for _, seed := range segmentFuzzSeeds(f) {
		f.Add(seed)
	}

	const maxSegmentBytes = 64 << 10
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxSegmentBytes {
			t.Skip("segment fuzz input above bounded local limit")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, SegmentFileName(1))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		segments, horizon, err := ScanSegments(dir)
		if err != nil {
			return
		}
		if len(segments) != 1 {
			t.Fatalf("accepted segment count = %d, want 1", len(segments))
		}
		seg := segments[0]
		if seg.Path != path || seg.StartTx != 1 || !seg.Valid {
			t.Fatalf("accepted segment info = %+v, want path %s start tx 1 valid", seg, path)
		}
		if seg.LastTx != horizon {
			t.Fatalf("horizon = %d, want segment LastTx %d", horizon, seg.LastTx)
		}
		if seg.LastTx > 0 && seg.LastTx < seg.StartTx {
			t.Fatalf("accepted segment has inverted non-empty range: %+v", seg)
		}
		if seg.AppendMode != AppendInPlace && seg.AppendMode != AppendByFreshNextSegment {
			t.Fatalf("accepted active segment append mode = %d, want in-place or fresh-next", seg.AppendMode)
		}

		reader, err := OpenSegment(path)
		if err != nil {
			return
		}
		defer reader.Close()
		for tx := types.TxID(1); tx <= seg.LastTx; tx++ {
			rec, err := reader.Next()
			if err != nil {
				t.Fatalf("read accepted record tx %d: %v", tx, err)
			}
			if types.TxID(rec.TxID) != tx {
				t.Fatalf("record tx = %d, want %d", rec.TxID, tx)
			}
		}
		if seg.AppendMode == AppendInPlace {
			if rec, err := reader.Next(); err == nil {
				t.Fatalf("append-in-place segment had extra record after horizon: %+v", rec)
			}
		}
	})
}

func FuzzOpenAndRecoverSnapshotSegmentBoundary(f *testing.F) {
	for _, seed := range recoveryBoundaryFuzzSeeds(f) {
		f.Add(seed.snapshot, seed.segment, seed.offsetIndex)
	}

	_, reg := testSchema()
	const maxArtifactBytes = 64 << 10
	f.Fuzz(func(t *testing.T, snapshotBytes []byte, segmentBytes []byte, offsetIndexBytes []byte) {
		if len(snapshotBytes) > maxArtifactBytes || len(segmentBytes) > maxArtifactBytes || len(offsetIndexBytes) > maxArtifactBytes {
			t.Skip("recovery fuzz input above bounded local limit")
		}

		root := t.TempDir()
		if snapshotBytes != nil {
			snapshotDir := filepath.Join(root, "snapshots", "1")
			if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(snapshotDir, snapshotFileName), snapshotBytes, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if segmentBytes != nil {
			if err := os.WriteFile(filepath.Join(root, SegmentFileName(1)), segmentBytes, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if offsetIndexBytes != nil {
			if err := os.WriteFile(filepath.Join(root, OffsetIndexFileName(1)), offsetIndexBytes, 0o644); err != nil {
				t.Fatal(err)
			}
		}

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			return
		}
		if recovered == nil {
			t.Fatalf("accepted recovery returned nil state: maxTxID=%d plan=%+v report=%+v", maxTxID, plan, report)
		}
		if got := recovered.CommittedTxID(); got != maxTxID {
			t.Fatalf("recovered committed txID = %d, want maxTxID %d (plan=%+v report=%+v)", got, maxTxID, plan, report)
		}
		if report.RecoveredTxID != maxTxID {
			t.Fatalf("report recovered txID = %d, want maxTxID %d (plan=%+v report=%+v)", report.RecoveredTxID, maxTxID, plan, report)
		}
		if report.ResumePlan != plan {
			t.Fatalf("report resume plan = %+v, want returned plan %+v", report.ResumePlan, plan)
		}
		if report.HasSelectedSnapshot && report.SelectedSnapshotTxID > maxTxID {
			t.Fatalf("selected snapshot txID %d exceeds recovered max txID %d (report=%+v)", report.SelectedSnapshotTxID, maxTxID, report)
		}
		if report.HasDurableLog && report.DurableLogHorizon < maxTxID && !report.HasSelectedSnapshot {
			t.Fatalf("durable log horizon %d below recovered max txID %d without snapshot (report=%+v)", report.DurableLogHorizon, maxTxID, report)
		}
		replayed := report.ReplayedTxRange
		if replayed != (RecoveryTxIDRange{}) {
			if replayed.Start == 0 || replayed.End == 0 || replayed.Start > replayed.End {
				t.Fatalf("invalid replay range %+v (report=%+v)", replayed, report)
			}
			if replayed.End > maxTxID {
				t.Fatalf("replay range %+v exceeds recovered max txID %d (report=%+v)", replayed, maxTxID, report)
			}
			if report.HasSelectedSnapshot && replayed.Start <= report.SelectedSnapshotTxID {
				t.Fatalf("replay range %+v overlaps selected snapshot txID %d (report=%+v)", replayed, report.SelectedSnapshotTxID, report)
			}
		}
	})
}

type recoveryBoundaryFuzzSeed struct {
	snapshot    []byte
	segment     []byte
	offsetIndex []byte
}

func recoveryBoundaryFuzzSeeds(t testing.TB) []recoveryBoundaryFuzzSeed {
	t.Helper()

	validSnapshot := validSnapshotSeed(t)
	corruptSnapshot := append([]byte(nil), validSnapshot...)
	corruptSnapshot[len(corruptSnapshot)-1] ^= 0xff

	validLog := recoveryBoundarySegmentSeed(t,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("one")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("two")}}},
	)
	snapshotBoundaryLog := recoveryBoundarySegmentSeed(t,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(99), types.NewString("skipped")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("after_snapshot")}}},
	)
	snapshotBoundaryZeroTail := append([]byte(nil), snapshotBoundaryLog...)
	snapshotBoundaryZeroTail = append(snapshotBoundaryZeroTail, make([]byte, RecordOverhead)...)
	snapshotBoundaryNonzeroTail := append([]byte(nil), snapshotBoundaryLog...)
	snapshotBoundaryNonzeroTail = append(snapshotBoundaryNonzeroTail, make([]byte, RecordHeaderSize)...)
	snapshotBoundaryNonzeroTail = append(snapshotBoundaryNonzeroTail, 1)
	snapshotBoundaryIndex := recoveryBoundaryOffsetIndexSeed(t, snapshotBoundaryLog)
	snapshotBoundaryCorruptIndex := appendOffsetIndexSeedEntry(nil, 2, 1)
	snapshotBoundaryPartialIndex := append([]byte(nil), snapshotBoundaryIndex...)
	snapshotBoundaryPartialIndex = appendOffsetIndexSeedKey(snapshotBoundaryPartialIndex, 99)
	snapshotBoundarySentinelIndex := append(make([]byte, 8), make([]byte, 8)...)
	binary.LittleEndian.PutUint64(snapshotBoundarySentinelIndex[8:], SegmentHeaderSize)
	firstTxMismatch := recoveryBoundarySegmentSeed(t,
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("first_mismatch")}}},
	)
	truncatedBoundary := append([]byte(nil), snapshotBoundaryLog...)
	truncatedBoundary = truncatedBoundary[:len(truncatedBoundary)-1]

	return []recoveryBoundaryFuzzSeed{
		{snapshot: nil, segment: nil},
		{snapshot: []byte{}, segment: nil},
		{snapshot: nil, segment: []byte{}},
		{snapshot: nil, segment: validLog},
		{snapshot: validSnapshot, segment: nil},
		{snapshot: validSnapshot, segment: snapshotBoundaryLog},
		{snapshot: validSnapshot, segment: snapshotBoundaryLog, offsetIndex: snapshotBoundaryIndex},
		{snapshot: validSnapshot, segment: snapshotBoundaryLog, offsetIndex: snapshotBoundaryCorruptIndex},
		{snapshot: validSnapshot, segment: snapshotBoundaryLog, offsetIndex: snapshotBoundaryPartialIndex},
		{snapshot: validSnapshot, segment: snapshotBoundaryLog, offsetIndex: snapshotBoundarySentinelIndex},
		{snapshot: validSnapshot, segment: snapshotBoundaryZeroTail},
		{snapshot: validSnapshot, segment: snapshotBoundaryZeroTail, offsetIndex: snapshotBoundaryIndex},
		{snapshot: validSnapshot, segment: snapshotBoundaryNonzeroTail},
		{snapshot: corruptSnapshot, segment: validLog},
		{snapshot: validSnapshot, segment: firstTxMismatch},
		{snapshot: validSnapshot, segment: truncatedBoundary},
	}
}

func recoveryBoundaryOffsetIndexSeed(t testing.TB, segment []byte) []byte {
	t.Helper()
	var index []byte
	for offset := uint64(SegmentHeaderSize); offset+RecordHeaderSize <= uint64(len(segment)); {
		txID := binary.LittleEndian.Uint64(segment[offset : offset+8])
		if txID == 0 {
			break
		}
		payloadLen := binary.LittleEndian.Uint32(segment[offset+10 : offset+14])
		next := offset + RecordOverhead + uint64(payloadLen)
		if next > uint64(len(segment)) {
			break
		}
		index = appendOffsetIndexSeedEntry(index, txID, offset)
		offset = next
	}
	if len(index) == 0 {
		t.Fatalf("segment produced empty offset-index seed")
	}
	return index
}

func recoveryBoundarySegmentSeed(t testing.TB, records ...replayRecord) []byte {
	t.Helper()
	encoded := make([]*Record, 0, len(records))
	for _, rec := range records {
		encoded = append(encoded, &Record{
			TxID:       rec.txID,
			RecordType: RecordTypeChangeset,
			Payload:    rapidEncodeReplayChangeset(t, rec),
		})
	}
	return segmentSeed(t, encoded...)
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
	partialNonZero := make([]byte, RecordHeaderSize-1)
	partialNonZero[len(partialNonZero)-1] = 1
	seeds = append(seeds, partialNonZero)
	zeroHeaderNonZeroTail := append(make([]byte, RecordHeaderSize), 1)
	seeds = append(seeds, zeroHeaderNonZeroTail)
	return seeds
}

func offsetIndexFuzzSeeds() [][]byte {
	var seeds [][]byte
	seeds = append(seeds, nil)
	seeds = append(seeds, make([]byte, OffsetIndexEntrySize))
	var valid []byte
	valid = appendOffsetIndexSeedEntry(valid, 1, SegmentHeaderSize)
	valid = appendOffsetIndexSeedEntry(valid, 3, SegmentHeaderSize+128)
	valid = append(valid, make([]byte, OffsetIndexEntrySize)...)
	seeds = append(seeds, valid)
	seeds = append(seeds, valid[:OffsetIndexEntrySize+8])

	keyOnlyTail := appendOffsetIndexSeedEntry(nil, 1, SegmentHeaderSize)
	keyOnlyTail = appendOffsetIndexSeedKey(keyOnlyTail, 2)
	keyOnlyTail = append(keyOnlyTail, make([]byte, 8)...)
	seeds = append(seeds, keyOnlyTail)

	var nonMonotonic []byte
	nonMonotonic = appendOffsetIndexSeedEntry(nonMonotonic, 2, SegmentHeaderSize)
	nonMonotonic = appendOffsetIndexSeedEntry(nonMonotonic, 1, SegmentHeaderSize+64)
	seeds = append(seeds, nonMonotonic)

	var zeroOffset []byte
	zeroOffset = appendOffsetIndexSeedEntry(zeroOffset, 1, 0)
	zeroOffset = appendOffsetIndexSeedEntry(zeroOffset, 2, SegmentHeaderSize+64)
	seeds = append(seeds, zeroOffset)
	return seeds
}

func segmentFuzzSeeds(t testing.TB) [][]byte {
	t.Helper()
	var seeds [][]byte
	seeds = append(seeds, nil)
	seeds = append(seeds, []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2]})
	headerOnly := segmentHeaderSeed(t)
	seeds = append(seeds, headerOnly)
	for _, mutate := range []func([]byte){
		func(seed []byte) { seed[0] ^= 0xff },
		func(seed []byte) { seed[4] = SegmentVersion + 1 },
		func(seed []byte) { seed[5] = 1 },
	} {
		corruptHeader := append([]byte(nil), headerOnly...)
		mutate(corruptHeader)
		seeds = append(seeds, corruptHeader)
	}

	validOne := segmentSeed(t, &Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("one")})
	seeds = append(seeds, validOne)
	seeds = append(seeds, validOne[:len(validOne)-1])
	zeroTail := append([]byte(nil), validOne...)
	zeroTail = append(zeroTail, make([]byte, RecordOverhead)...)
	seeds = append(seeds, zeroTail)
	zeroHeaderNonZeroTail := append([]byte(nil), validOne...)
	zeroHeaderNonZeroTail = append(zeroHeaderNonZeroTail, make([]byte, RecordHeaderSize)...)
	zeroHeaderNonZeroTail = append(zeroHeaderNonZeroTail, 1)
	seeds = append(seeds, zeroHeaderNonZeroTail)
	partialZeroTail := append([]byte(nil), validOne...)
	partialZeroTail = append(partialZeroTail, make([]byte, RecordHeaderSize-1)...)
	seeds = append(seeds, partialZeroTail)
	partialNonZeroTail := append([]byte(nil), validOne...)
	partialNonZero := make([]byte, RecordHeaderSize-1)
	partialNonZero[len(partialNonZero)-1] = 1
	partialNonZeroTail = append(partialNonZeroTail, partialNonZero...)
	seeds = append(seeds, partialNonZeroTail)

	validTwo := segmentSeed(t,
		&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("one")},
		&Record{TxID: 2, RecordType: RecordTypeChangeset, Payload: []byte("two")},
	)
	seeds = append(seeds, validTwo)

	corruptTail := segmentSeed(t,
		&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("one")},
		&Record{TxID: 2, RecordType: RecordTypeChangeset, Payload: []byte("two")},
		&Record{TxID: 3, RecordType: RecordTypeChangeset, Payload: []byte("three")},
	)
	corruptTail[len(corruptTail)-1] ^= 0xff
	seeds = append(seeds, corruptTail)

	gap := segmentSeed(t,
		&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("one")},
		&Record{TxID: 3, RecordType: RecordTypeChangeset, Payload: []byte("gap")},
	)
	seeds = append(seeds, gap)
	firstTxMismatch := segmentSeed(t, &Record{TxID: 2, RecordType: RecordTypeChangeset, Payload: []byte("mismatch")})
	seeds = append(seeds, firstTxMismatch)

	badFlags := segmentSeed(t, &Record{TxID: 1, RecordType: RecordTypeChangeset, Flags: 1, Payload: []byte("bad")})
	seeds = append(seeds, badFlags)
	unknownTypeAfterPrefix := segmentSeed(t,
		&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("one")},
		&Record{TxID: 2, RecordType: RecordTypeChangeset + 1, Payload: []byte("unknown")},
	)
	seeds = append(seeds, unknownTypeAfterPrefix)
	badFlagsAfterPrefix := segmentSeed(t,
		&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte("one")},
		&Record{TxID: 2, RecordType: RecordTypeChangeset, Flags: 1, Payload: []byte("bad")},
	)
	seeds = append(seeds, badFlagsAfterPrefix)
	return seeds
}

func segmentHeaderSeed(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func segmentSeed(t testing.TB, records ...*Record) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteSegmentHeader(&buf); err != nil {
		t.Fatal(err)
	}
	for _, rec := range records {
		if err := EncodeRecord(&buf, rec); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func appendOffsetIndexSeedEntry(dst []byte, key uint64, off uint64) []byte {
	dst = appendOffsetIndexSeedKey(dst, key)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], off)
	return append(dst, buf[:]...)
}

func appendOffsetIndexSeedKey(dst []byte, key uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], key)
	return append(dst, buf[:]...)
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
	seeds = append(seeds, append(append([]byte(nil), valid...), 0xde, 0xad, 0xbe, 0xef))
	rowShapeMismatch := &store.Changeset{
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   []types.ProductValue{{types.NewUint64(1)}},
			},
		},
	}
	seeds = append(seeds, encodeChangesetSeed(t, rowShapeMismatch))
	unknownTable := append([]byte(nil), []byte{changesetVersion, 1, 0, 0, 0, 99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	seeds = append(seeds, unknownTable)
	duplicateTable := []byte{changesetVersion}
	duplicateTable = appendUint32(duplicateTable, 2)
	for range 2 {
		duplicateTable = appendUint32(duplicateTable, 0)
		duplicateTable = appendUint32(duplicateTable, 0)
		duplicateTable = appendUint32(duplicateTable, 0)
	}
	seeds = append(seeds, duplicateTable)
	oversizedRow := []byte{changesetVersion}
	oversizedRow = appendUint32(oversizedRow, 1)
	oversizedRow = appendUint32(oversizedRow, 0)
	oversizedRow = appendUint32(oversizedRow, 1)
	oversizedRow = appendUint32(oversizedRow, DefaultCommitLogOptions().MaxRowBytes+1)
	oversizedRow = appendUint32(oversizedRow, 0)
	seeds = append(seeds, oversizedRow)
	return seeds
}

func snapshotFuzzSeeds(t testing.TB) [][]byte {
	t.Helper()
	var seeds [][]byte
	seeds = append(seeds, nil)
	seeds = append(seeds, make([]byte, SnapshotHeaderSize))
	valid := validSnapshotSeed(t)
	seeds = append(seeds, valid)
	seeds = append(seeds, valid[:SnapshotHeaderSize-1])
	seeds = append(seeds, valid[:len(valid)-1])

	hashCorrupt := append([]byte(nil), valid...)
	hashCorrupt[len(hashCorrupt)-1] ^= 0xff
	seeds = append(seeds, hashCorrupt)

	badMagic := append([]byte(nil), valid...)
	badMagic[0] ^= 0xff
	seeds = append(seeds, badMagic)

	badVersion := append([]byte(nil), valid...)
	badVersion[4] = SnapshotVersion + 1
	seeds = append(seeds, badVersion)

	badFlags := append([]byte(nil), valid...)
	badFlags[5] = 1
	seeds = append(seeds, badFlags)

	trailing := append([]byte(nil), valid...)
	trailing = append(trailing, 0)
	seeds = append(seeds, snapshotSeedWithRecomputedHash(t, trailing))

	oversizedSchema := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(oversizedSchema[SnapshotHeaderSize:SnapshotHeaderSize+4], DefaultCommitLogOptions().MaxRecordPayloadBytes+1)
	seeds = append(seeds, snapshotSeedWithRecomputedHash(t, oversizedSchema))

	singleTableSchema := encodeSingleTableSchemaSnapshot(t, false)
	var duplicateTables bytes.Buffer
	writeUint32(t, &duplicateTables, uint32(len(singleTableSchema)))
	duplicateTables.Write(singleTableSchema)
	writeUint32(t, &duplicateTables, 0) // sequence entries
	writeUint32(t, &duplicateTables, 1) // next ID entries
	writeUint32(t, &duplicateTables, 0)
	writeUint64(t, &duplicateTables, 1)
	writeUint32(t, &duplicateTables, 2) // table sections
	writeUint32(t, &duplicateTables, 0)
	writeUint32(t, &duplicateTables, 0)
	writeUint32(t, &duplicateTables, 0)
	writeUint32(t, &duplicateTables, 0)
	seeds = append(seeds, snapshotSeedFromBody(t, 1, duplicateTables.Bytes()))

	autoIncrementSchema := encodeSingleTableSchemaSnapshot(t, true)
	var duplicateSequence bytes.Buffer
	writeUint32(t, &duplicateSequence, uint32(len(autoIncrementSchema)))
	duplicateSequence.Write(autoIncrementSchema)
	writeUint32(t, &duplicateSequence, 2) // sequence entries
	writeUint32(t, &duplicateSequence, 0)
	writeUint64(t, &duplicateSequence, 1)
	writeUint32(t, &duplicateSequence, 0)
	writeUint64(t, &duplicateSequence, 2)
	writeUint32(t, &duplicateSequence, 1) // next ID entries
	writeUint32(t, &duplicateSequence, 0)
	writeUint64(t, &duplicateSequence, 1)
	writeUint32(t, &duplicateSequence, 1) // table sections
	writeUint32(t, &duplicateSequence, 0)
	writeUint32(t, &duplicateSequence, 0)
	seeds = append(seeds, snapshotSeedFromBody(t, 1, duplicateSequence.Bytes()))

	var oversizedRow bytes.Buffer
	writeUint32(t, &oversizedRow, uint32(len(singleTableSchema)))
	oversizedRow.Write(singleTableSchema)
	writeUint32(t, &oversizedRow, 0) // sequence entries
	writeUint32(t, &oversizedRow, 1) // next ID entries
	writeUint32(t, &oversizedRow, 0)
	writeUint64(t, &oversizedRow, 1)
	writeUint32(t, &oversizedRow, 1) // table sections
	writeUint32(t, &oversizedRow, 0)
	writeUint32(t, &oversizedRow, 1)
	writeUint32(t, &oversizedRow, DefaultCommitLogOptions().MaxRowBytes+1)
	seeds = append(seeds, snapshotSeedFromBody(t, 1, oversizedRow.Bytes()))
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

func validSnapshotSeed(t testing.TB) []byte {
	t.Helper()
	_, reg := testSchema()
	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, ok := reg.Table(tableID)
		if !ok {
			t.Fatalf("registry missing table %d", tableID)
		}
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	for _, row := range []types.ProductValue{
		{types.NewUint64(1), types.NewString("alice")},
		{types.NewUint64(2), types.NewString("bob")},
	} {
		if err := players.InsertRow(players.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 1)
	data, err := os.ReadFile(filepath.Join(root, "snapshots", "1", snapshotFileName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func snapshotSeedWithRecomputedHash(t testing.TB, data []byte) []byte {
	t.Helper()
	if len(data) < SnapshotHeaderSize {
		t.Fatalf("snapshot seed too short: %d", len(data))
	}
	hash := ComputeSnapshotHash(data[SnapshotHeaderSize:])
	copy(data[20:52], hash[:])
	return data
}

func snapshotSeedFromBody(t testing.TB, schemaVersion uint32, body []byte) []byte {
	t.Helper()
	var file bytes.Buffer
	file.Write(SnapshotMagic[:])
	file.Write([]byte{SnapshotVersion, 0, 0, 0})
	var txID [8]byte
	binary.LittleEndian.PutUint64(txID[:], 1)
	file.Write(txID[:])
	var version [4]byte
	binary.LittleEndian.PutUint32(version[:], schemaVersion)
	file.Write(version[:])
	hash := ComputeSnapshotHash(body)
	file.Write(hash[:])
	file.Write(body)
	return file.Bytes()
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
