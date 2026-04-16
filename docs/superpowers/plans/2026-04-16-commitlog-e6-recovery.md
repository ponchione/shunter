# Commit-Log E6: Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `OpenAndRecover` — full startup recovery from snapshots + commit log replay.

**Architecture:** Four files, one per story. `ScanSegments` validates segment file integrity and contiguity. `SelectSnapshot` picks the best usable snapshot with schema validation and corruption fallback. `ReplayLog` bridges SPEC-002 (commit log) and SPEC-001 (store) by decoding changesets and applying them to `CommittedState`. `OpenAndRecover` orchestrates all three into a single entry point for engine startup.

**Tech Stack:** Go, existing `commitlog` package infrastructure (segment reader, changeset codec, snapshot I/O, Blake3 hashing).

**Spec refs:** `docs/decomposition/002-commitlog/epic-6-recovery/`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `commitlog/segment_scan.go` | `AppendMode`, `SegmentInfo`, `ScanSegments()` |
| `commitlog/segment_scan_test.go` | All Story 6.1 acceptance tests |
| `commitlog/snapshot_select.go` | `SelectSnapshot()`, `compareSchemas()` |
| `commitlog/snapshot_select_test.go` | All Story 6.2 acceptance tests |
| `commitlog/replay.go` | `ReplayLog()` |
| `commitlog/replay_test.go` | All Story 6.3 acceptance tests |
| `commitlog/recovery.go` | `OpenAndRecover()`, `restoreFromSnapshot()` |
| `commitlog/recovery_test.go` | All Story 6.4 acceptance tests |
| `commitlog/errors.go` | Already complete — no changes needed |

**Existing deps used:**
- `segment.go`: `OpenSegment`, `SegmentReader`, `DecodeRecord`, `SegmentFileName`, `SegmentHeaderSize`, `RecordOverhead`
- `changeset_codec.go`: `DecodeChangeset`
- `snapshot_io.go`: `ListSnapshots`, `ReadSnapshot`, `SnapshotData`, `SnapshotTableData`, `NewSnapshotWriter`
- `errors.go`: `ErrHistoryGap`, `ErrSchemaMismatch`, `ErrMissingBaseSnapshot`, `ErrNoData`, `ErrTruncatedRecord`, `ErrChecksumMismatch`
- `store/recovery.go`: `ApplyChangeset`
- `store/committed_state.go`: `CommittedState`, `NewCommittedState`, `RegisterTable`
- `store/table.go`: `NewTable`, `SetNextID`, `SetSequenceValue`, `InsertRow`, `AllocRowID`
- `schema/registry.go`: `SchemaRegistry`, `Table()`, `Tables()`, `Version()`

---

## Task 1: ScanSegments — Segment Scanning & Validation

**Files:**
- Create: `commitlog/segment_scan.go`
- Create: `commitlog/segment_scan_test.go`

### Step 1: Write failing tests for ScanSegments

- [ ] **Step 1a: Create test file with helpers and first test**

```go
// commitlog/segment_scan_test.go
package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/types"
)

// writeTestSegment creates a segment file with N records starting at startTx.
// Records have consecutive TxIDs: startTx, startTx+1, ..., startTx+N-1.
func writeTestSegment(t *testing.T, dir string, startTx uint64, count int) {
	t.Helper()
	sw, err := CreateSegment(dir, startTx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range count {
		if err := sw.Append(&Record{
			TxID:       startTx + uint64(i),
			RecordType: RecordTypeChangeset,
			Payload:    []byte("payload"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestScanSegmentsThreeContiguous(t *testing.T) {
	dir := t.TempDir()
	writeTestSegment(t, dir, 1, 10)   // 1..10
	writeTestSegment(t, dir, 11, 10)  // 11..20
	writeTestSegment(t, dir, 21, 10)  // 21..30

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(segments))
	}
	if horizon != 30 {
		t.Fatalf("horizon = %d, want 30", horizon)
	}
	if segments[0].StartTx != 1 || segments[0].LastTx != 10 {
		t.Fatalf("seg[0] range = %d..%d, want 1..10", segments[0].StartTx, segments[0].LastTx)
	}
	if segments[2].StartTx != 21 || segments[2].LastTx != 30 {
		t.Fatalf("seg[2] range = %d..%d, want 21..30", segments[2].StartTx, segments[2].LastTx)
	}
	for _, seg := range segments {
		if !seg.Valid {
			t.Fatalf("segment %s should be valid", seg.Path)
		}
	}
	if segments[2].AppendMode != AppendInPlace {
		t.Fatalf("last segment append mode = %d, want AppendInPlace", segments[2].AppendMode)
	}
}

func TestScanSegmentsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 0 {
		t.Fatalf("segments = %d, want 0", len(segments))
	}
	if horizon != 0 {
		t.Fatalf("horizon = %d, want 0", horizon)
	}
}

func TestScanSegmentsSingleRecord(t *testing.T) {
	dir := t.TempDir()
	writeTestSegment(t, dir, 1, 1)

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || horizon != 1 {
		t.Fatalf("segments=%d horizon=%d, want 1/1", len(segments), horizon)
	}
}

func TestScanSegmentsHistoryGapMissingMiddle(t *testing.T) {
	dir := t.TempDir()
	writeTestSegment(t, dir, 1, 5)   // 1..5
	// skip 6..10
	writeTestSegment(t, dir, 11, 5)  // 11..15

	_, _, err := ScanSegments(dir)
	var gapErr *HistoryGapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("expected HistoryGapError, got %v", err)
	}
}

func TestScanSegmentsHistoryGapOverlap(t *testing.T) {
	dir := t.TempDir()
	writeTestSegment(t, dir, 1, 10)  // 1..10
	writeTestSegment(t, dir, 8, 5)   // 8..12 (overlaps)

	_, _, err := ScanSegments(dir)
	var gapErr *HistoryGapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("expected HistoryGapError, got %v", err)
	}
}

func TestScanSegmentsTruncatedTail(t *testing.T) {
	dir := t.TempDir()
	writeTestSegment(t, dir, 1, 5) // 1..5

	// Append garbage to end of segment file.
	path := filepath.Join(dir, SegmentFileName(1))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02})
	f.Close()

	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if horizon != 5 {
		t.Fatalf("horizon = %d, want 5", horizon)
	}
	if segments[0].AppendMode != AppendByFreshNextSegment {
		t.Fatalf("append mode = %d, want AppendByFreshNextSegment", segments[0].AppendMode)
	}
}

func TestScanSegmentsCorruptSealedSegment(t *testing.T) {
	dir := t.TempDir()
	writeTestSegment(t, dir, 1, 5)
	writeTestSegment(t, dir, 6, 5)

	// Corrupt a byte inside the first (sealed) segment's record data.
	path := filepath.Join(dir, SegmentFileName(1))
	data, _ := os.ReadFile(path)
	data[SegmentHeaderSize+10] ^= 0xFF // corrupt first record
	os.WriteFile(path, data, 0o644)

	_, _, err := ScanSegments(dir)
	if err == nil {
		t.Fatal("expected error for corrupt sealed segment")
	}
}

func TestScanSegmentsCorruptFirstRecordActiveSegment(t *testing.T) {
	dir := t.TempDir()

	// Create segment with valid header but corrupt first record.
	path := filepath.Join(dir, SegmentFileName(1))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	WriteSegmentHeader(f)
	f.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF}) // garbage where first record should be
	f.Close()

	_, _, err = ScanSegments(dir)
	if err == nil {
		t.Fatal("expected error for corrupt first record in active segment")
	}
}

func TestScanSegmentsOutOfOrderTxID(t *testing.T) {
	dir := t.TempDir()
	// Write segment where tx IDs go backwards.
	sw, err := CreateSegment(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	sw.Append(&Record{TxID: 5, RecordType: RecordTypeChangeset, Payload: []byte("x")})
	sw.Close()

	// Manually create a second segment that starts before the first ends.
	writeTestSegment(t, dir, 3, 2) // 3..4 overlaps with seg starting at 5

	_, _, err = ScanSegments(dir)
	var gapErr *HistoryGapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("expected HistoryGapError for out-of-order, got %v", err)
	}
}
```

- [ ] **Step 1b: Run tests to verify they fail**

Run: `rtk go test ./commitlog/ -run TestScanSegments -v`
Expected: FAIL — `ScanSegments` undefined

### Step 2: Implement ScanSegments

- [ ] **Step 2a: Create segment_scan.go**

```go
// commitlog/segment_scan.go
package commitlog

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ponchione/shunter/types"
)

// AppendMode indicates how the durability worker should resume writing.
type AppendMode uint8

const (
	// AppendInPlace means the last segment ended cleanly and can be reopened.
	AppendInPlace AppendMode = iota
	// AppendByFreshNextSegment means the last segment had a truncated tail;
	// future writes must go into a new segment file.
	AppendByFreshNextSegment
	// AppendForbidden means recovery cannot proceed (corrupt sealed segment).
	AppendForbidden
)

// SegmentInfo describes a validated segment file.
type SegmentInfo struct {
	Path       string
	StartTx    types.TxID
	LastTx     types.TxID
	Valid      bool
	AppendMode AppendMode
}

// ScanSegments lists and validates all segment files in dir.
// Returns the segment list (sorted by start TX), the durable replay horizon
// (highest contiguous valid TxID), and any error.
func ScanSegments(dir string) ([]SegmentInfo, types.TxID, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}

	// Collect .log files sorted by name (= sorted by start TX).
	var logFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".log" {
			logFiles = append(logFiles, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(logFiles)

	if len(logFiles) == 0 {
		return nil, 0, nil
	}

	segments := make([]SegmentInfo, 0, len(logFiles))
	for i, path := range logFiles {
		isLast := i == len(logFiles)-1
		info, err := scanOneSegment(path, isLast)
		if err != nil {
			return nil, 0, err
		}
		segments = append(segments, info)
	}

	// Validate cross-segment contiguity.
	for i := 1; i < len(segments); i++ {
		prev := segments[i-1]
		cur := segments[i]
		expected := prev.LastTx + 1
		if cur.StartTx != expected {
			return nil, 0, &HistoryGapError{
				Expected: uint64(expected),
				Got:      uint64(cur.StartTx),
				Segment:  cur.Path,
			}
		}
	}

	horizon := segments[len(segments)-1].LastTx
	return segments, horizon, nil
}

// scanOneSegment reads a single segment, validates all records, and returns info.
// isLast indicates whether this is the active (last) segment — truncated tails
// are tolerated only in the active segment.
func scanOneSegment(path string, isLast bool) (SegmentInfo, error) {
	sr, err := OpenSegment(path)
	if err != nil {
		return SegmentInfo{}, err
	}
	defer sr.Close()

	info := SegmentInfo{
		Path:    path,
		StartTx: types.TxID(sr.StartTxID()),
		Valid:   true,
	}

	var recordCount int
	var lastTx uint64
	var prevTx uint64

	for {
		rec, err := sr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// Record error — either truncation or corruption.
			if !isLast {
				// Sealed segment corruption is fatal.
				return SegmentInfo{}, err
			}
			if recordCount == 0 {
				// Corrupt first record in active segment with no valid prefix.
				return SegmentInfo{}, err
			}
			// Truncated/corrupt tail in active segment — stop at last valid.
			info.AppendMode = AppendByFreshNextSegment
			break
		}

		recordCount++

		// Check monotonicity within segment.
		if recordCount > 1 && rec.TxID <= prevTx {
			return SegmentInfo{}, &HistoryGapError{
				Expected: prevTx + 1,
				Got:      rec.TxID,
				Segment:  path,
			}
		}
		prevTx = rec.TxID
		lastTx = rec.TxID
	}

	if recordCount == 0 {
		// Valid header but no records — segment is empty.
		// Use StartTx as LastTx (no records to speak of).
		info.LastTx = info.StartTx
		info.Valid = false
	} else {
		info.LastTx = types.TxID(lastTx)
	}

	return info, nil
}
```

- [ ] **Step 2b: Run tests**

Run: `rtk go test ./commitlog/ -run TestScanSegments -v`
Expected: All PASS

- [ ] **Step 2c: Commit**

```bash
rtk git add commitlog/segment_scan.go commitlog/segment_scan_test.go
rtk git commit -m "feat(commitlog): ScanSegments for segment validation and contiguity (Story 6.1)"
```

---

## Task 2: SelectSnapshot — Snapshot Selection & Fallback

**Files:**
- Create: `commitlog/snapshot_select.go`
- Create: `commitlog/snapshot_select_test.go`

### Step 1: Write failing tests

- [ ] **Step 1a: Create test file**

```go
// commitlog/snapshot_select_test.go
package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func buildSelectTestState(t *testing.T) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	return buildSnapshotCommittedState(t)
}

func writeSnapshot(t *testing.T, baseDir string, reg schema.SchemaRegistry, cs *store.CommittedState, txID types.TxID) {
	t.Helper()
	writer := NewSnapshotWriter(baseDir, reg)
	if err := writer.CreateSnapshot(cs, txID); err != nil {
		t.Fatal(err)
	}
}

func TestSelectSnapshotNewestValid(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 100)
	writeSnapshot(t, snapDir, reg, cs, 200)

	snap, err := SelectSnapshot(snapDir, 200, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TxID != 200 {
		t.Fatalf("selected snapshot txID = %d, want 200", snap.TxID)
	}
}

func TestSelectSnapshotFallbackOnCorruption(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 100)
	writeSnapshot(t, snapDir, reg, cs, 200)

	// Corrupt snapshot 200.
	path := filepath.Join(snapDir, "200", "snapshot")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xFF
	os.WriteFile(path, data, 0o644)

	snap, err := SelectSnapshot(snapDir, 200, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TxID != 100 {
		t.Fatalf("should fall back to snapshot 100, got %d", snap.TxID)
	}
}

func TestSelectSnapshotSkipsBeyondHorizon(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 100)
	writeSnapshot(t, snapDir, reg, cs, 300)

	snap, err := SelectSnapshot(snapDir, 200, reg)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TxID != 100 {
		t.Fatalf("should skip snapshot 300, selected %d", snap.TxID)
	}
}

func TestSelectSnapshotAllCorruptLogAtTx1(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 50)

	// Corrupt the only snapshot.
	path := filepath.Join(snapDir, "50", "snapshot")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xFF
	os.WriteFile(path, data, 0o644)

	// logStartsTx1 = true → return nil (fresh start).
	snap, err := SelectSnapshot(snapDir, 100, reg, withLogStart(1))
	if err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		t.Fatal("expected nil snapshot for fresh start")
	}
}

func TestSelectSnapshotAllCorruptLogNotAtTx1(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 50)

	// Corrupt the only snapshot.
	path := filepath.Join(snapDir, "50", "snapshot")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xFF
	os.WriteFile(path, data, 0o644)

	// logStartsTx > 1 → error.
	_, err := SelectSnapshot(snapDir, 100, reg, withLogStart(5))
	if !errors.Is(err, ErrMissingBaseSnapshot) {
		t.Fatalf("expected ErrMissingBaseSnapshot, got %v", err)
	}
}

func TestSelectSnapshotSchemaMismatchColumnType(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 100)

	// Build a different registry with different column type.
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindString, PrimaryKey: true}, // was Uint64
			{Name: "name", Type: types.KindString},
		},
	})
	e, _ := b.Build(schema.EngineOptions{})
	differentReg := e.Registry()

	_, err := SelectSnapshot(snapDir, 100, differentReg, withLogStart(5))
	var schemaErr *SchemaMismatchError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected SchemaMismatchError, got %v", err)
	}
}

func TestSelectSnapshotSchemaMismatchMissingTable(t *testing.T) {
	cs, reg := buildSelectTestState(t)
	snapDir := t.TempDir()
	writeSnapshot(t, snapDir, reg, cs, 100)

	// Build a registry with an extra table.
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "players",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "name", Type: types.KindString},
		},
	})
	b.TableDef(schema.TableDefinition{
		Name: "scores",
		Columns: []schema.ColumnDefinition{
			{Name: "val", Type: types.KindInt64},
		},
	})
	e, _ := b.Build(schema.EngineOptions{})
	differentReg := e.Registry()

	_, err := SelectSnapshot(snapDir, 100, differentReg, withLogStart(5))
	var schemaErr *SchemaMismatchError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected SchemaMismatchError, got %v", err)
	}
}

func TestSelectSnapshotNoSnapshots(t *testing.T) {
	snapDir := t.TempDir()
	_, reg := buildSelectTestState(t)

	snap, err := SelectSnapshot(snapDir, 100, reg, withLogStart(1))
	if err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		t.Fatal("expected nil when no snapshots exist")
	}
}
```

- [ ] **Step 1b: Run tests to verify they fail**

Run: `rtk go test ./commitlog/ -run TestSelectSnapshot -v`
Expected: FAIL — `SelectSnapshot` undefined

### Step 2: Implement SelectSnapshot

- [ ] **Step 2a: Create snapshot_select.go**

```go
// commitlog/snapshot_select.go
package commitlog

import (
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// selectOptions configures SelectSnapshot behavior.
type selectOptions struct {
	logStartTx types.TxID
}

// SelectOption configures snapshot selection.
type SelectOption func(*selectOptions)

// withLogStart sets the first TxID in the log. If all snapshots are unusable
// and logStartTx == 1, SelectSnapshot returns nil (fresh start). Otherwise
// it returns ErrMissingBaseSnapshot.
func withLogStart(txID types.TxID) SelectOption {
	return func(o *selectOptions) { o.logStartTx = txID }
}

// SelectSnapshot picks the best usable snapshot for recovery.
// Falls back through older snapshots on corruption. Validates schema match.
func SelectSnapshot(baseDir string, durableHorizon types.TxID, reg schema.SchemaRegistry, opts ...SelectOption) (*SnapshotData, error) {
	var cfg selectOptions
	for _, opt := range opts {
		opt(&cfg)
	}

	ids, err := ListSnapshots(baseDir)
	if err != nil {
		return nil, err
	}

	for _, txID := range ids {
		if txID > durableHorizon {
			continue
		}

		snapDir := fmt.Sprintf("%s/%d", baseDir, txID)
		data, err := ReadSnapshot(snapDir)
		if err != nil {
			// Corrupt snapshot — try next.
			continue
		}

		if err := compareSchemas(data.Schema, reg); err != nil {
			return nil, err
		}

		return data, nil
	}

	// No usable snapshot found.
	if cfg.logStartTx <= 1 {
		return nil, nil
	}
	return nil, ErrMissingBaseSnapshot
}

// compareSchemas validates that snapshot schema matches the registered schema.
func compareSchemas(snapshotTables []schema.TableSchema, reg schema.SchemaRegistry) error {
	regTableIDs := reg.Tables()

	// Check that system tables from the registry (which are auto-added by the
	// builder) are excluded from comparison — snapshots only contain the user
	// tables that were registered at snapshot time.  Build a lookup of snapshot
	// table IDs so we can compare only what the snapshot covers.
	snapByID := make(map[schema.TableID]*schema.TableSchema, len(snapshotTables))
	for i := range snapshotTables {
		snapByID[snapshotTables[i].ID] = &snapshotTables[i]
	}

	// Every registered table must be in the snapshot.
	for _, id := range regTableIDs {
		regTable, _ := reg.Table(id)
		snapTable, ok := snapByID[id]
		if !ok {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q (id=%d) missing from snapshot", regTable.Name, id)}
		}

		if err := compareTableSchemas(regTable, snapTable); err != nil {
			return err
		}
		delete(snapByID, id)
	}

	// Any leftover snapshot tables have no match in the registry.
	for _, st := range snapByID {
		return &SchemaMismatchError{Detail: fmt.Sprintf("snapshot has extra table %q (id=%d)", st.Name, st.ID)}
	}

	return nil
}

func compareTableSchemas(reg, snap *schema.TableSchema) error {
	if reg.Name != snap.Name {
		return &SchemaMismatchError{Detail: fmt.Sprintf("table %d name: registry=%q snapshot=%q", reg.ID, reg.Name, snap.Name)}
	}

	if len(reg.Columns) != len(snap.Columns) {
		return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column count: registry=%d snapshot=%d", reg.Name, len(reg.Columns), len(snap.Columns))}
	}

	for i := range reg.Columns {
		rc, sc := reg.Columns[i], snap.Columns[i]
		if rc.Index != sc.Index || rc.Name != sc.Name || rc.Type != sc.Type || rc.Nullable != sc.Nullable || rc.AutoIncrement != sc.AutoIncrement {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %q differs", reg.Name, rc.Name)}
		}
	}

	if len(reg.Indexes) != len(snap.Indexes) {
		return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index count: registry=%d snapshot=%d", reg.Name, len(reg.Indexes), len(snap.Indexes))}
	}

	for i := range reg.Indexes {
		ri, si := reg.Indexes[i], snap.Indexes[i]
		if ri.Name != si.Name || ri.Unique != si.Unique || ri.Primary != si.Primary {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %q differs", reg.Name, ri.Name)}
		}
		if len(ri.Columns) != len(si.Columns) {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %q column count differs", reg.Name, ri.Name)}
		}
		for j := range ri.Columns {
			if ri.Columns[j] != si.Columns[j] {
				return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %q column[%d] differs", reg.Name, ri.Name, j)}
			}
		}
	}

	return nil
}
```

- [ ] **Step 2b: Run tests**

Run: `rtk go test ./commitlog/ -run TestSelectSnapshot -v`
Expected: All PASS

- [ ] **Step 2c: Commit**

```bash
rtk git add commitlog/snapshot_select.go commitlog/snapshot_select_test.go
rtk git commit -m "feat(commitlog): SelectSnapshot with fallback and schema validation (Story 6.2)"
```

---

## Task 3: ReplayLog — Log Replay

**Files:**
- Create: `commitlog/replay.go`
- Create: `commitlog/replay_test.go`

### Step 1: Write failing tests

- [ ] **Step 1a: Create test file**

```go
// commitlog/replay_test.go
package commitlog

import (
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// writeChangesetSegment writes a segment of encoded changesets.
// Each changeset inserts one row: (txID, "player_{txID}") into table 0 ("players").
func writeChangesetSegment(t *testing.T, dir string, reg schema.SchemaRegistry, startTx uint64, count int) {
	t.Helper()
	sw, err := CreateSegment(dir, startTx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range count {
		txID := startTx + uint64(i)
		cs := &store.Changeset{
			TxID: types.TxID(txID),
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "players",
					Inserts: []types.ProductValue{
						{types.NewUint64(txID), types.NewString("player")},
					},
				},
			},
		}
		data, err := EncodeChangeset(cs)
		if err != nil {
			t.Fatal(err)
		}
		if err := sw.Append(&Record{TxID: txID, RecordType: RecordTypeChangeset, Payload: data}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
}

func buildReplayState(t *testing.T, reg schema.SchemaRegistry) *store.CommittedState {
	t.Helper()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	return cs
}

func TestReplayLogBasic(t *testing.T) {
	_, reg := testSchema()
	dir := t.TempDir()
	writeChangesetSegment(t, dir, reg, 1, 10)

	segments, _, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}

	cs := buildReplayState(t, reg)
	maxTx, err := ReplayLog(cs, segments, 0, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 10 {
		t.Fatalf("maxTx = %d, want 10", maxTx)
	}

	table, _ := cs.Table(0)
	if table.RowCount() != 10 {
		t.Fatalf("row count = %d, want 10", table.RowCount())
	}
}

func TestReplayLogSkipRecords(t *testing.T) {
	_, reg := testSchema()
	dir := t.TempDir()
	writeChangesetSegment(t, dir, reg, 1, 10)

	segments, _, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}

	cs := buildReplayState(t, reg)
	maxTx, err := ReplayLog(cs, segments, 5, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 10 {
		t.Fatalf("maxTx = %d, want 10", maxTx)
	}

	table, _ := cs.Table(0)
	if table.RowCount() != 5 {
		t.Fatalf("row count = %d, want 5 (skipped 1..5)", table.RowCount())
	}
}

func TestReplayLogMultipleSegments(t *testing.T) {
	_, reg := testSchema()
	dir := t.TempDir()
	writeChangesetSegment(t, dir, reg, 1, 5)
	writeChangesetSegment(t, dir, reg, 6, 5)

	segments, _, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}

	cs := buildReplayState(t, reg)
	maxTx, err := ReplayLog(cs, segments, 0, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 10 {
		t.Fatalf("maxTx = %d, want 10", maxTx)
	}
	table, _ := cs.Table(0)
	if table.RowCount() != 10 {
		t.Fatalf("row count = %d, want 10", table.RowCount())
	}
}

func TestReplayLogEmptyReplay(t *testing.T) {
	_, reg := testSchema()
	dir := t.TempDir()
	writeChangesetSegment(t, dir, reg, 1, 5)

	segments, _, err := ScanSegments(dir)
	if err != nil {
		t.Fatal(err)
	}

	cs := buildReplayState(t, reg)
	maxTx, err := ReplayLog(cs, segments, 10, reg) // all records ≤ fromTxID
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 10 {
		t.Fatalf("maxTx = %d, want 10 (fromTxID passthrough)", maxTx)
	}

	table, _ := cs.Table(0)
	if table.RowCount() != 0 {
		t.Fatalf("row count = %d, want 0", table.RowCount())
	}
}

func TestReplayLogDecodeError(t *testing.T) {
	_, reg := testSchema()
	dir := t.TempDir()

	// Write segment with garbage payload.
	sw, err := CreateSegment(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	sw.Append(&Record{TxID: 1, RecordType: RecordTypeChangeset, Payload: []byte{0xFF}})
	sw.Close()

	segments := []SegmentInfo{{
		Path:    filepath.Join(dir, SegmentFileName(1)),
		StartTx: 1,
		LastTx:  1,
		Valid:   true,
	}}

	cs := buildReplayState(t, reg)
	_, err = ReplayLog(cs, segments, 0, reg)
	if err == nil {
		t.Fatal("expected decode error")
	}
}
```

- [ ] **Step 1b: Run tests to verify they fail**

Run: `rtk go test ./commitlog/ -run TestReplayLog -v`
Expected: FAIL — `ReplayLog` undefined

### Step 2: Implement ReplayLog

- [ ] **Step 2a: Create replay.go**

```go
// commitlog/replay.go
package commitlog

import (
	"fmt"
	"io"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// ReplayLog decodes and applies log records from segments into committed state.
// Skips records with tx_id ≤ fromTxID. Returns the highest applied TxID.
func ReplayLog(committed *store.CommittedState, segments []SegmentInfo, fromTxID types.TxID, reg schema.SchemaRegistry) (types.TxID, error) {
	maxApplied := fromTxID

	for _, seg := range segments {
		// Skip entire segment if all records are ≤ fromTxID.
		if seg.LastTx <= fromTxID {
			continue
		}

		sr, err := OpenSegment(seg.Path)
		if err != nil {
			return 0, fmt.Errorf("replay: open segment %s: %w", seg.Path, err)
		}

		for {
			rec, err := sr.Next()
			if err != nil {
				sr.Close()
				if err == io.EOF {
					break
				}
				return 0, fmt.Errorf("replay: read record in %s: %w", seg.Path, err)
			}

			txID := types.TxID(rec.TxID)
			if txID <= fromTxID {
				continue
			}

			cs, err := DecodeChangeset(rec.Payload, reg)
			if err != nil {
				sr.Close()
				return 0, fmt.Errorf("replay: decode changeset at tx %d in %s: %w", txID, seg.Path, err)
			}

			if err := store.ApplyChangeset(committed, cs); err != nil {
				sr.Close()
				return 0, fmt.Errorf("replay: apply changeset at tx %d in %s: %w", txID, seg.Path, err)
			}

			maxApplied = txID
		}
		sr.Close()
	}

	return maxApplied, nil
}
```

- [ ] **Step 2b: Run tests**

Run: `rtk go test ./commitlog/ -run TestReplayLog -v`
Expected: All PASS

- [ ] **Step 2c: Commit**

```bash
rtk git add commitlog/replay.go commitlog/replay_test.go
rtk git commit -m "feat(commitlog): ReplayLog for crash recovery log replay (Story 6.3)"
```

---

## Task 4: OpenAndRecover — Full Recovery Orchestration

**Files:**
- Create: `commitlog/recovery.go`
- Create: `commitlog/recovery_test.go`

**Note:** `store/recovery.go` already exists — this is `commitlog/recovery.go`, a different package.

### Step 1: Write failing tests

- [ ] **Step 1a: Create test file**

```go
// commitlog/recovery_test.go
package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// setupRecoveryDir creates a dir with segments/ and snapshots/ subdirs.
func setupRecoveryDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenAndRecoverSnapshotPlusLog(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	// Create committed state, insert 2 rows, snapshot at tx 2.
	cs := buildReplayState(t, reg)
	table, _ := cs.Table(0)
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")})
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")})

	writer := NewSnapshotWriter(filepath.Join(dir, "snapshots"), reg)
	if err := writer.CreateSnapshot(cs, 2); err != nil {
		t.Fatal(err)
	}

	// Write log 3..5 with changeset inserts.
	writeChangesetSegment(t, dir, reg, 3, 3)

	recovered, maxTx, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 5 {
		t.Fatalf("maxTx = %d, want 5", maxTx)
	}

	recTable, _ := recovered.Table(0)
	if recTable.RowCount() != 5 {
		t.Fatalf("row count = %d, want 5 (2 from snapshot + 3 from log)", recTable.RowCount())
	}
}

func TestOpenAndRecoverLogOnly(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	writeChangesetSegment(t, dir, reg, 1, 5)

	recovered, maxTx, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 5 {
		t.Fatalf("maxTx = %d, want 5", maxTx)
	}

	recTable, _ := recovered.Table(0)
	if recTable.RowCount() != 5 {
		t.Fatalf("row count = %d, want 5", recTable.RowCount())
	}
}

func TestOpenAndRecoverLogNotAtTx1NoSnapshot(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	writeChangesetSegment(t, dir, reg, 5, 3) // starts at 5, not 1

	_, _, err := OpenAndRecover(dir, reg)
	if !errors.Is(err, ErrMissingBaseSnapshot) {
		t.Fatalf("expected ErrMissingBaseSnapshot, got %v", err)
	}
}

func TestOpenAndRecoverNoSegmentsNoSnapshots(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	_, _, err := OpenAndRecover(dir, reg)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("expected ErrNoData, got %v", err)
	}
}

func TestOpenAndRecoverSnapshotOnlyNoLog(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	cs := buildReplayState(t, reg)
	table, _ := cs.Table(0)
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")})

	writer := NewSnapshotWriter(filepath.Join(dir, "snapshots"), reg)
	if err := writer.CreateSnapshot(cs, 10); err != nil {
		t.Fatal(err)
	}

	recovered, maxTx, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 10 {
		t.Fatalf("maxTx = %d, want 10", maxTx)
	}
	recTable, _ := recovered.Table(0)
	if recTable.RowCount() != 1 {
		t.Fatalf("row count = %d, want 1", recTable.RowCount())
	}
}

func TestOpenAndRecoverCorruptNewestSnapshotFallback(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	cs := buildReplayState(t, reg)
	table, _ := cs.Table(0)
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")})

	snapDir := filepath.Join(dir, "snapshots")
	writer := NewSnapshotWriter(snapDir, reg)
	writer.CreateSnapshot(cs, 5)

	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")})
	writer2 := NewSnapshotWriter(snapDir, reg)
	writer2.CreateSnapshot(cs, 10)

	// Corrupt snapshot 10.
	path := filepath.Join(snapDir, "10", "snapshot")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 0xFF
	os.WriteFile(path, data, 0o644)

	// Log from 6..8 (replays on top of snapshot 5).
	writeChangesetSegment(t, dir, reg, 6, 3)

	recovered, maxTx, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 8 {
		t.Fatalf("maxTx = %d, want 8", maxTx)
	}

	recTable, _ := recovered.Table(0)
	// Snapshot 5 has 1 row (alice), log adds 3 rows.
	if recTable.RowCount() != 4 {
		t.Fatalf("row count = %d, want 4", recTable.RowCount())
	}
}

func TestOpenAndRecoverSequencesAndNextID(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	cs := buildReplayState(t, reg)
	table, _ := cs.Table(0)
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")})
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")})

	writer := NewSnapshotWriter(filepath.Join(dir, "snapshots"), reg)
	writer.CreateSnapshot(cs, 5)

	recovered, _, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}

	recTable, _ := recovered.Table(0)
	if recTable.NextID() < 3 {
		t.Fatalf("NextID = %d, want >= 3 (2 rows inserted)", recTable.NextID())
	}
}

func TestOpenAndRecoverIndexesRebuilt(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	cs := buildReplayState(t, reg)
	table, _ := cs.Table(0)
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")})
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")})

	writer := NewSnapshotWriter(filepath.Join(dir, "snapshots"), reg)
	writer.CreateSnapshot(cs, 5)

	recovered, _, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}

	recTable, _ := recovered.Table(0)
	pk := recTable.PrimaryIndex()
	if pk == nil {
		t.Fatal("primary index should be rebuilt after recovery")
	}
	// Verify index can find rows.
	key := pk.ExtractKey(types.ProductValue{types.NewUint64(1), types.NewString("alice")})
	rids := pk.Seek(key)
	if len(rids) != 1 {
		t.Fatalf("PK seek for id=1 returned %d results, want 1", len(rids))
	}
}

func TestOpenAndRecoverLockedSnapshotSkipped(t *testing.T) {
	_, reg := testSchema()
	dir := setupRecoveryDir(t)

	cs := buildReplayState(t, reg)
	table, _ := cs.Table(0)
	table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")})

	snapDir := filepath.Join(dir, "snapshots")
	writer := NewSnapshotWriter(snapDir, reg)
	writer.CreateSnapshot(cs, 5)

	// Create a locked (in-progress) snapshot at tx 10.
	os.MkdirAll(filepath.Join(snapDir, "10"), 0o755)
	CreateLockFile(filepath.Join(snapDir, "10"))

	// Log from 6..8.
	writeChangesetSegment(t, dir, reg, 6, 3)

	recovered, maxTx, err := OpenAndRecover(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTx != 8 {
		t.Fatalf("maxTx = %d, want 8 (locked snapshot 10 should be skipped)", maxTx)
	}
	recTable, _ := recovered.Table(0)
	if recTable.RowCount() != 4 {
		t.Fatalf("row count = %d, want 4", recTable.RowCount())
	}
}
```

- [ ] **Step 1b: Run tests to verify they fail**

Run: `rtk go test ./commitlog/ -run TestOpenAndRecover -v`
Expected: FAIL — `OpenAndRecover` undefined

### Step 2: Implement OpenAndRecover

- [ ] **Step 2a: Create recovery.go**

```go
// commitlog/recovery.go
package commitlog

import (
	"fmt"
	"path/filepath"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// OpenAndRecover performs full startup recovery.
// Scans segments, selects a snapshot, restores state, replays the log.
// Returns the recovered committed state and the highest applied TxID.
func OpenAndRecover(dir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, error) {
	segments, horizon, err := ScanSegments(dir)
	if err != nil {
		return nil, 0, err
	}

	// Check for append-forbidden in sealed segments (already caught by ScanSegments).
	// Check active segment append mode.
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if last.AppendMode == AppendForbidden {
			return nil, 0, fmt.Errorf("commitlog: recovery cannot proceed: active segment corrupt")
		}
	}

	snapDir := filepath.Join(dir, "snapshots")
	var logStartTx types.TxID
	if len(segments) > 0 {
		logStartTx = segments[0].StartTx
	}

	snap, err := SelectSnapshot(snapDir, horizon, reg, withLogStart(logStartTx))
	if err != nil {
		return nil, 0, err
	}

	if snap == nil && len(segments) == 0 {
		return nil, 0, ErrNoData
	}

	committed := store.NewCommittedState()
	var snapshotTxID types.TxID

	if snap != nil {
		if err := restoreFromSnapshot(committed, snap, reg); err != nil {
			return nil, 0, fmt.Errorf("commitlog: restore snapshot: %w", err)
		}
		snapshotTxID = snap.TxID
	} else {
		// Fresh start — register empty tables from schema.
		for _, tid := range reg.Tables() {
			ts, _ := reg.Table(tid)
			committed.RegisterTable(tid, store.NewTable(ts))
		}
	}

	if len(segments) == 0 {
		// Snapshot only, no log to replay.
		return committed, snapshotTxID, nil
	}

	maxTx, err := ReplayLog(committed, segments, snapshotTxID, reg)
	if err != nil {
		return nil, 0, err
	}

	return committed, maxTx, nil
}

// restoreFromSnapshot populates committed state from snapshot data.
// Registers tables, inserts rows, restores sequences and nextID, rebuilds indexes.
func restoreFromSnapshot(committed *store.CommittedState, snap *SnapshotData, reg schema.SchemaRegistry) error {
	// Register tables from the live schema (not the snapshot schema — the live
	// schema is authoritative, and SelectSnapshot already verified they match).
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		committed.RegisterTable(tid, store.NewTable(ts))
	}

	// Insert snapshot rows.
	for _, st := range snap.Tables {
		table, ok := committed.Table(st.TableID)
		if !ok {
			return fmt.Errorf("snapshot references unregistered table %d", st.TableID)
		}
		for _, row := range st.Rows {
			id := table.AllocRowID()
			if err := table.InsertRow(id, row); err != nil {
				return fmt.Errorf("restore table %d row: %w", st.TableID, err)
			}
		}
	}

	// Restore nextID from snapshot (overrides whatever AllocRowID set).
	for tid, nextID := range snap.NextIDs {
		table, ok := committed.Table(tid)
		if !ok {
			continue
		}
		table.SetNextID(types.RowID(nextID))
	}

	// Restore sequence values.
	for tid, seqVal := range snap.Sequences {
		table, ok := committed.Table(tid)
		if !ok {
			continue
		}
		table.SetSequenceValue(seqVal)
	}

	return nil
}
```

- [ ] **Step 2b: Run tests**

Run: `rtk go test ./commitlog/ -run TestOpenAndRecover -v`
Expected: All PASS

- [ ] **Step 2c: Run full commitlog test suite**

Run: `rtk go test ./commitlog/ -v`
Expected: All existing + new tests PASS

- [ ] **Step 2d: Commit**

```bash
rtk git add commitlog/recovery.go commitlog/recovery_test.go
rtk git commit -m "feat(commitlog): OpenAndRecover full startup recovery (Story 6.4)"
```

---

## Task 5: Update REMAINING.md

**Files:**
- Modify: `REMAINING.md`

- [ ] **Step 1: Update E6 status in REMAINING.md**

Change E6 row status from "Not implemented (error types only)" to **Done** with commit range.

- [ ] **Step 2: Update dependency chain section**

Remove E6 from "can start now" — only E7 remains.

- [ ] **Step 3: Commit**

```bash
rtk git add REMAINING.md
rtk git commit -m "docs: mark commitlog E6 (Recovery) complete in REMAINING.md"
```
