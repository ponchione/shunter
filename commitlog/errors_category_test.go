package commitlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// --- Pins 1-9: typed-struct category pins ---

func TestChecksumMismatchErrorCategory(t *testing.T) {
	err := error(&ChecksumMismatchError{Expected: 1, Got: 2, TxID: 7})
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false")
	}
	var cm *ChecksumMismatchError
	if !errors.As(err, &cm) {
		t.Fatalf("errors.As(*ChecksumMismatchError) = false")
	}
	if cm.Expected != 1 || cm.Got != 2 || cm.TxID != 7 {
		t.Fatalf("fields mismatch: %+v", cm)
	}
}

func TestBadVersionErrorCategory(t *testing.T) {
	err := error(&BadVersionError{Got: 2})
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false")
	}
	var bv *BadVersionError
	if !errors.As(err, &bv) {
		t.Fatalf("errors.As(*BadVersionError) = false")
	}
	if bv.Got != 2 {
		t.Fatalf("Got = %d, want 2", bv.Got)
	}
}

func TestUnknownRecordTypeErrorCategory(t *testing.T) {
	err := error(&UnknownRecordTypeError{Type: 42})
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false")
	}
	var u *UnknownRecordTypeError
	if !errors.As(err, &u) {
		t.Fatalf("errors.As(*UnknownRecordTypeError) = false")
	}
	if u.Type != 42 {
		t.Fatalf("Type = %d, want 42", u.Type)
	}
}

func TestRecordTooLargeErrorCategory(t *testing.T) {
	err := error(&RecordTooLargeError{Size: 1024, Max: 512})
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false")
	}
	var r *RecordTooLargeError
	if !errors.As(err, &r) {
		t.Fatalf("errors.As(*RecordTooLargeError) = false")
	}
	if r.Size != 1024 || r.Max != 512 {
		t.Fatalf("fields mismatch: %+v", r)
	}
}

func TestRowTooLargeErrorCategory(t *testing.T) {
	err := error(&RowTooLargeError{Size: 2048, Max: 1024})
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false")
	}
	var r *RowTooLargeError
	if !errors.As(err, &r) {
		t.Fatalf("errors.As(*RowTooLargeError) = false")
	}
	if r.Size != 2048 || r.Max != 1024 {
		t.Fatalf("fields mismatch: %+v", r)
	}
}

func TestHistoryGapErrorCategory(t *testing.T) {
	err := error(&HistoryGapError{Expected: 5, Got: 7, Segment: "/seg"})
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false")
	}
	var h *HistoryGapError
	if !errors.As(err, &h) {
		t.Fatalf("errors.As(*HistoryGapError) = false")
	}
	if h.Expected != 5 || h.Got != 7 || h.Segment != "/seg" {
		t.Fatalf("fields mismatch: %+v", h)
	}
}

func TestSchemaMismatchErrorCategory(t *testing.T) {
	cause := errors.New("nullable")
	err := error(&SchemaMismatchError{Detail: "mismatch", Cause: cause})
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("errors.Is(err, ErrSnapshot) = false")
	}
	var sm *SchemaMismatchError
	if !errors.As(err, &sm) {
		t.Fatalf("errors.As(*SchemaMismatchError) = false")
	}
	if sm.Detail != "mismatch" {
		t.Fatalf("Detail = %q, want mismatch", sm.Detail)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("existing Unwrap-to-Cause chain broken: errors.Is(err, cause) = false")
	}
}

func TestSnapshotHashMismatchErrorCategory(t *testing.T) {
	err := error(&SnapshotHashMismatchError{Expected: [32]byte{1}, Got: [32]byte{2}})
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("errors.Is(err, ErrSnapshot) = false")
	}
	var hm *SnapshotHashMismatchError
	if !errors.As(err, &hm) {
		t.Fatalf("errors.As(*SnapshotHashMismatchError) = false")
	}
	if hm.Expected[0] != 1 || hm.Got[0] != 2 {
		t.Fatalf("fields mismatch: %+v", hm)
	}
}

func TestOffsetIndexNonMonotonicErrorCategory(t *testing.T) {
	err := error(&OffsetIndexNonMonotonicError{Last: 9, Got: 5})
	if !errors.Is(err, ErrIndex) {
		t.Fatalf("errors.Is(err, ErrIndex) = false")
	}
	var nm *OffsetIndexNonMonotonicError
	if !errors.As(err, &nm) {
		t.Fatalf("errors.As(*OffsetIndexNonMonotonicError) = false")
	}
	if nm.Last != 9 || nm.Got != 5 {
		t.Fatalf("fields mismatch: %+v", nm)
	}
}

// --- Pins 10-12: sentinel singleton pins ---

func TestSentinelSingletonBadMagicCategory(t *testing.T) {
	if !errors.Is(ErrBadMagic, ErrBadMagic) {
		t.Fatalf("errors.Is(ErrBadMagic, ErrBadMagic) = false")
	}
	if !errors.Is(ErrBadMagic, ErrOpen) {
		t.Fatalf("errors.Is(ErrBadMagic, ErrOpen) = false")
	}
	if ErrBadMagic.Error() != "commitlog: bad magic bytes" {
		t.Fatalf("ErrBadMagic.Error() = %q", ErrBadMagic.Error())
	}
}

func TestSentinelSingletonTruncatedRecordCategory(t *testing.T) {
	if !errors.Is(ErrTruncatedRecord, ErrTruncatedRecord) {
		t.Fatalf("errors.Is(ErrTruncatedRecord, ErrTruncatedRecord) = false")
	}
	if !errors.Is(ErrTruncatedRecord, ErrTraversal) {
		t.Fatalf("errors.Is(ErrTruncatedRecord, ErrTraversal) = false")
	}
	if errors.Is(ErrTruncatedRecord, ErrOpen) {
		t.Fatalf("errors.Is(ErrTruncatedRecord, ErrOpen) = true")
	}
	if ErrTruncatedRecord.Error() != "commitlog: truncated record" {
		t.Fatalf("ErrTruncatedRecord.Error() = %q", ErrTruncatedRecord.Error())
	}
}

func TestSentinelSingletonCoversEveryBareSentinel(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
		category error
		text     string
	}{
		{"BadMagic", ErrBadMagic, ErrOpen, "commitlog: bad magic bytes"},
		{"BadFlags", ErrBadFlags, ErrTraversal, "commitlog: non-zero flags"},
		{"TruncatedRecord", ErrTruncatedRecord, ErrTraversal, "commitlog: truncated record"},
		{"DurabilityFailed", ErrDurabilityFailed, ErrDurability, "commitlog: durability worker failed"},
		{"SnapshotIncomplete", ErrSnapshotIncomplete, ErrSnapshot, "commitlog: snapshot has lock file (incomplete)"},
		{"SnapshotInProgress", ErrSnapshotInProgress, ErrSnapshot, "commitlog: snapshot write already in progress"},
		{"MissingBaseSnapshot", ErrMissingBaseSnapshot, ErrOpen, "commitlog: no valid base snapshot for log replay"},
		{"NoData", ErrNoData, ErrOpen, "commitlog: no snapshot or log data found"},
		{"UnknownFsyncMode", ErrUnknownFsyncMode, ErrOpen, "commitlog: unknown fsync mode"},
		{"OffsetIndexKeyNotFound", ErrOffsetIndexKeyNotFound, ErrIndex, "commitlog: offset index key not found"},
		{"OffsetIndexFull", ErrOffsetIndexFull, ErrIndex, "commitlog: offset index full"},
		{"OffsetIndexCorrupt", ErrOffsetIndexCorrupt, ErrIndex, "commitlog: offset index corrupt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.sentinel, tc.sentinel) {
				t.Fatalf("errors.Is(sentinel, sentinel) = false")
			}
			if !errors.Is(tc.sentinel, tc.category) {
				t.Fatalf("errors.Is(sentinel, category) = false")
			}
			if tc.sentinel.Error() != tc.text {
				t.Fatalf("sentinel.Error() = %q, want %q", tc.sentinel.Error(), tc.text)
			}
		})
	}
}

// --- Pins 13-25: end-to-end admission-seam pins ---

// Pin 13.
func TestSegmentHeaderBadMagicReturnsOpenCategory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SegmentFileName(1))
	// Wrong magic (first 4 bytes anything but "SHNT"), plausible rest.
	if err := os.WriteFile(path, []byte{'X', 'X', 'X', 'X', SegmentVersion, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := OpenSegment(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("errors.Is(err, ErrBadMagic) = false: %v", err)
	}
}

// Pin 14.
func TestDecodeRecordChecksumMismatchReturnsTraversalCategory(t *testing.T) {
	var buf bytes.Buffer
	rec := &Record{TxID: 42, RecordType: RecordTypeChangeset, Payload: []byte("payload-bytes")}
	if err := EncodeRecord(&buf, rec); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	// Flip the last byte of the 4-byte CRC trailer to guarantee mismatch.
	raw[len(raw)-1] ^= 0xFF

	_, err := DecodeRecord(bytes.NewReader(raw), 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
	var cm *ChecksumMismatchError
	if !errors.As(err, &cm) {
		t.Fatalf("errors.As(*ChecksumMismatchError) = false: %v", err)
	}
	if cm.TxID != 42 {
		t.Fatalf("cm.TxID = %d, want 42", cm.TxID)
	}
}

// Pin 15.
func TestScanSegmentsHistoryGapReturnsOpenCategory(t *testing.T) {
	dir := t.TempDir()
	// First segment with tx {1, 2}.
	writeReplaySegment(t, dir, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("b")}}},
	)
	// Second segment starts at 7 — expected 3 — creates a history gap.
	writeReplaySegment(t, dir, 7,
		replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(7), types.NewString("g")}}},
	)

	_, _, err := ScanSegments(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
	var gap *HistoryGapError
	if !errors.As(err, &gap) {
		t.Fatalf("errors.As(*HistoryGapError) = false: %v", err)
	}
	if gap.Expected != 3 || gap.Got != 7 {
		t.Fatalf("gap = %+v, want Expected=3 Got=7", gap)
	}
}

// Pin 16.
func TestReplayLogCorruptRecordReturnsTraversalCategory(t *testing.T) {
	root := t.TempDir()
	committed, reg := buildReplayCommittedState(t)
	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("b")}}},
	)
	// Flip a CRC-trailer byte of the second record so its decode fails mid-stream
	// rather than triggering a tail-truncated recognizer.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip the final byte — it's the last CRC byte of the last record.
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	segments := []SegmentInfo{{
		Path:       path,
		StartTx:    1,
		LastTx:     2,
		Valid:      true,
		AppendMode: AppendInPlace,
	}}
	_, err = ReplayLog(committed, segments, 0, reg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("errors.Is(err, ErrTraversal) = false: %v", err)
	}
}

// Pin 17.
func TestRecoveryNoDataReturnsOpenCategory(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	_, _, err := OpenAndRecover(dir, reg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("errors.Is(err, ErrNoData) = false: %v", err)
	}
}

// Pin 18.
func TestRecoveryMissingBaseSnapshotReturnsOpenCategory(t *testing.T) {
	dir := t.TempDir()
	_, reg := testSchema()
	// Log data present, but first segment starts above 1 (no base snapshot).
	writeReplaySegment(t, dir, 5,
		replayRecord{txID: 5, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("x")}}},
	)
	_, _, err := OpenAndRecover(dir, reg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
	if !errors.Is(err, ErrMissingBaseSnapshot) {
		t.Fatalf("errors.Is(err, ErrMissingBaseSnapshot) = false: %v", err)
	}
}

// Pin 19.
func TestSnapshotHashMismatchReturnsSnapshotCategory(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	corruptSelectionSnapshot(t, root, 5)

	_, err := ReadSnapshot(filepath.Join(root, "snapshots", "5"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("errors.Is(err, ErrSnapshot) = false: %v", err)
	}
	var hm *SnapshotHashMismatchError
	if !errors.As(err, &hm) {
		t.Fatalf("errors.As(*SnapshotHashMismatchError) = false: %v", err)
	}
}

func TestSnapshotHeaderFaultsReturnSnapshotCategory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bytes     []byte
		assertErr func(*testing.T, error)
	}{
		{
			name:  "bad-magic",
			bytes: []byte{'B', 'A', 'D', '!'},
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrBadMagic) {
					t.Fatalf("errors.Is(err, ErrBadMagic) = false: %v", err)
				}
			},
		},
		{
			name:  "bad-version",
			bytes: []byte{'S', 'H', 'S', 'N', SnapshotVersion + 1, 0, 0, 0},
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				var versionErr *BadVersionError
				if !errors.As(err, &versionErr) {
					t.Fatalf("errors.As(*BadVersionError) = false: %v", err)
				}
				if versionErr.Got != SnapshotVersion+1 {
					t.Fatalf("bad version = %d, want %d", versionErr.Got, SnapshotVersion+1)
				}
			},
		},
		{
			name:  "bad-flags",
			bytes: []byte{'S', 'H', 'S', 'N', SnapshotVersion, 1, 0, 0},
			assertErr: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrBadFlags) {
					t.Fatalf("errors.Is(err, ErrBadFlags) = false: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			snapshotDir := filepath.Join(root, "snapshots", "5")
			if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(snapshotDir, snapshotFileName), tc.bytes, 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := ReadSnapshot(snapshotDir)
			if err == nil {
				t.Fatal("expected snapshot header fault")
			}
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("errors.Is(err, ErrSnapshot) = false: %v", err)
			}
			tc.assertErr(t, err)
		})
	}
}

// Pin 20.
func TestSnapshotSelectSchemaMismatchReturnsSnapshotCategory(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writeSelectionSnapshot(t, root, reg, cs, 5)
	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Columns[1].Type = schema.KindUint64
		tables[0] = players
	})

	_, err := SelectSnapshot(root, 5, mismatchReg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("errors.Is(err, ErrSnapshot) = false: %v", err)
	}
	var sm *SchemaMismatchError
	if !errors.As(err, &sm) {
		t.Fatalf("errors.As(*SchemaMismatchError) = false: %v", err)
	}
}

// Pin 21.
func TestDurabilityWorkerFatalPanicsWithDurabilityCategory(t *testing.T) {
	dir := t.TempDir()
	dw, err := NewDurabilityWorker(dir, 1, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	fatalCause := errors.New("simulated disk failure")
	dw.stateMu.Lock()
	dw.fatalErr = fatalCause
	dw.stateMu.Unlock()

	var panicErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				e, _ := r.(error)
				panicErr = e
			}
		}()
		dw.EnqueueCommitted(1, makeDurabilityTestChangeset(1))
	}()
	// Cleanly tear down the worker regardless of outcome.
	_, _ = dw.Close()

	if panicErr == nil {
		t.Fatal("expected panic with error value")
	}
	if !errors.Is(panicErr, ErrDurability) {
		t.Fatalf("errors.Is(panicErr, ErrDurability) = false: %v", panicErr)
	}
	if !errors.Is(panicErr, ErrDurabilityFailed) {
		t.Fatalf("errors.Is(panicErr, ErrDurabilityFailed) = false: %v", panicErr)
	}
	if !errors.Is(panicErr, fatalCause) {
		t.Fatalf("errors.Is(panicErr, fatalCause) = false: %v", panicErr)
	}
}

// Pin 22.
func TestDurabilityCtorUnknownFsyncModeReturnsOpenCategory(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultCommitLogOptions()
	opts.FsyncMode = FsyncMode(99)
	_, err := NewDurabilityWorker(dir, 1, opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(err, ErrOpen) = false: %v", err)
	}
	if !errors.Is(err, ErrUnknownFsyncMode) {
		t.Fatalf("errors.Is(err, ErrUnknownFsyncMode) = false: %v", err)
	}
}

// Pin 23.
func TestOffsetIndexKeyNotFoundReturnsIndexCategory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	m, err := CreateOffsetIndex(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, _, err = m.KeyLookup(42)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrIndex) {
		t.Fatalf("errors.Is(err, ErrIndex) = false: %v", err)
	}
	if !errors.Is(err, ErrOffsetIndexKeyNotFound) {
		t.Fatalf("errors.Is(err, ErrOffsetIndexKeyNotFound) = false: %v", err)
	}
}

// Pin 24.
func TestOffsetIndexFullReturnsIndexCategory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	m, err := CreateOffsetIndex(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Append(types.TxID(1), uint64(SegmentHeaderSize)); err != nil {
		t.Fatal(err)
	}
	if err := m.Append(types.TxID(2), uint64(SegmentHeaderSize+1)); err != nil {
		t.Fatal(err)
	}
	err = m.Append(types.TxID(3), uint64(SegmentHeaderSize+2))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrIndex) {
		t.Fatalf("errors.Is(err, ErrIndex) = false: %v", err)
	}
	if !errors.Is(err, ErrOffsetIndexFull) {
		t.Fatalf("errors.Is(err, ErrOffsetIndexFull) = false: %v", err)
	}
}

// Pin 25.
func TestOffsetIndexNonMonotonicReturnsIndexCategory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OffsetIndexFileName(1))
	m, err := CreateOffsetIndex(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Append(types.TxID(10), uint64(SegmentHeaderSize)); err != nil {
		t.Fatal(err)
	}
	err = m.Append(types.TxID(5), uint64(SegmentHeaderSize+1))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrIndex) {
		t.Fatalf("errors.Is(err, ErrIndex) = false: %v", err)
	}
	var nm *OffsetIndexNonMonotonicError
	if !errors.As(err, &nm) {
		t.Fatalf("errors.As(*OffsetIndexNonMonotonicError) = false: %v", err)
	}
	if nm.Last != 10 || nm.Got != 5 {
		t.Fatalf("nm = %+v, want Last=10 Got=5", nm)
	}
}

// --- Pins 26-28: back-compat pins ---

func TestBackCompatSentinelIdentityPreserved(t *testing.T) {
	// Every sentinel reachable via a category wrap. Table-driven — errors.Is
	// on the leaf sentinel continues to match after wrapping.
	cases := []struct {
		name string
		cat  error
		leaf error
	}{
		{"BadMagic", ErrOpen, ErrBadMagic},
		{"BadFlags", ErrTraversal, ErrBadFlags},
		{"TruncatedRecord", ErrTraversal, ErrTruncatedRecord},
		{"NoData", ErrOpen, ErrNoData},
		{"MissingBaseSnapshot", ErrOpen, ErrMissingBaseSnapshot},
		{"SnapshotInProgress", ErrSnapshot, ErrSnapshotInProgress},
		{"SnapshotIncomplete", ErrSnapshot, ErrSnapshotIncomplete},
		{"OffsetIndexKeyNotFound", ErrIndex, ErrOffsetIndexKeyNotFound},
		{"OffsetIndexFull", ErrIndex, ErrOffsetIndexFull},
		{"OffsetIndexCorrupt", ErrIndex, ErrOffsetIndexCorrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.leaf, tc.leaf) {
				t.Fatalf("errors.Is(leaf, leaf) = false")
			}
			if !errors.Is(tc.leaf, tc.cat) {
				t.Fatalf("errors.Is(leaf, cat) = false")
			}
		})
	}
	// Sentinels raised via fmt.Errorf %w paths (durability).
	unknownFsync := fmt.Errorf("%w: %d", ErrUnknownFsyncMode, 99)
	if !errors.Is(unknownFsync, ErrUnknownFsyncMode) {
		t.Fatalf("fmt-wrapped ErrUnknownFsyncMode lost leaf identity")
	}
	if !errors.Is(unknownFsync, ErrOpen) {
		t.Fatalf("fmt-wrapped ErrUnknownFsyncMode lost category")
	}
	fatalPanic := fmt.Errorf("%w: %w", ErrDurabilityFailed, errors.New("x"))
	if !errors.Is(fatalPanic, ErrDurabilityFailed) {
		t.Fatalf("fmt-wrapped ErrDurabilityFailed lost leaf identity")
	}
	if !errors.Is(fatalPanic, ErrDurability) {
		t.Fatalf("fmt-wrapped ErrDurabilityFailed lost category")
	}
}

func TestBackCompatTypedStructErrorAsStillWorks(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		check func(t *testing.T, err error)
	}{
		{"BadVersion", &BadVersionError{Got: 2}, func(t *testing.T, e error) {
			var v *BadVersionError
			if !errors.As(e, &v) || v.Got != 2 {
				t.Fatalf("As failed or Got=%d", v.Got)
			}
		}},
		{"UnknownRecordType", &UnknownRecordTypeError{Type: 9}, func(t *testing.T, e error) {
			var u *UnknownRecordTypeError
			if !errors.As(e, &u) || u.Type != 9 {
				t.Fatalf("As failed or Type=%d", u.Type)
			}
		}},
		{"ChecksumMismatch", &ChecksumMismatchError{Expected: 1, Got: 2, TxID: 3}, func(t *testing.T, e error) {
			var c *ChecksumMismatchError
			if !errors.As(e, &c) || c.TxID != 3 {
				t.Fatalf("As failed or TxID=%d", c.TxID)
			}
		}},
		{"RecordTooLarge", &RecordTooLargeError{Size: 10, Max: 5}, func(t *testing.T, e error) {
			var r *RecordTooLargeError
			if !errors.As(e, &r) || r.Size != 10 {
				t.Fatalf("As failed or Size=%d", r.Size)
			}
		}},
		{"RowTooLarge", &RowTooLargeError{Size: 20, Max: 10}, func(t *testing.T, e error) {
			var r *RowTooLargeError
			if !errors.As(e, &r) || r.Size != 20 {
				t.Fatalf("As failed or Size=%d", r.Size)
			}
		}},
		{"HistoryGap", &HistoryGapError{Expected: 1, Got: 2, Segment: "s"}, func(t *testing.T, e error) {
			var h *HistoryGapError
			if !errors.As(e, &h) || h.Expected != 1 {
				t.Fatalf("As failed or Expected=%d", h.Expected)
			}
		}},
		{"SchemaMismatch", &SchemaMismatchError{Detail: "d"}, func(t *testing.T, e error) {
			var s *SchemaMismatchError
			if !errors.As(e, &s) || s.Detail != "d" {
				t.Fatalf("As failed or Detail=%q", s.Detail)
			}
		}},
		{"SnapshotHashMismatch", &SnapshotHashMismatchError{}, func(t *testing.T, e error) {
			var h *SnapshotHashMismatchError
			if !errors.As(e, &h) {
				t.Fatalf("As failed")
			}
		}},
		{"OffsetIndexNonMonotonic", &OffsetIndexNonMonotonicError{Last: 1, Got: 0}, func(t *testing.T, e error) {
			var n *OffsetIndexNonMonotonicError
			if !errors.As(e, &n) || n.Last != 1 {
				t.Fatalf("As failed or Last=%d", n.Last)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tc.check(t, tc.err) })
	}
}

func TestBackCompatErrorMessageUnchanged(t *testing.T) {
	// One representative per sentinel family. Error() surface text of a
	// categorized wrap equals the leaf's text — category appears only in the
	// Unwrap chain, never in the rendered message.
	cases := []struct {
		name string
		err  error
	}{
		{"BadMagic", ErrBadMagic},
		{"BadFlags", ErrBadFlags},
		{"TruncatedRecord", ErrTruncatedRecord},
		{"NoData", ErrNoData},
		{"MissingBaseSnapshot", ErrMissingBaseSnapshot},
		{"SnapshotInProgress", ErrSnapshotInProgress},
		{"OffsetIndexKeyNotFound", ErrOffsetIndexKeyNotFound},
		{"OffsetIndexFull", ErrOffsetIndexFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Leaf text is the part before " :" / descriptors — look for the leaf prefix.
			leafText := tc.err.Error()
			// Must start with "commitlog:" and must not contain any category string.
			if !strings.HasPrefix(leafText, "commitlog:") {
				t.Fatalf("Error() = %q, want commitlog: prefix", leafText)
			}
			for _, catText := range []string{
				"traversal error",
				"open error",
				"durability error",
				"snapshot error",
				"offset index error",
			} {
				if strings.Contains(leafText, catText) {
					t.Fatalf("Error() = %q contains category text %q", leafText, catText)
				}
			}
		})
	}
}
