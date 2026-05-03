package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func buildSnapshotCommittedState(t *testing.T) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	_, reg := testSchema()
	cs := buildEmptySnapshotCommittedState(t, reg)
	players, _ := cs.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")}); err != nil {
		t.Fatal(err)
	}
	return cs, reg
}

func buildEmptySnapshotCommittedState(t testing.TB, reg schema.SchemaRegistry) *store.CommittedState {
	t.Helper()
	cs := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, ok := reg.Table(tableID)
		if !ok {
			t.Fatalf("registry missing table %d", tableID)
		}
		cs.RegisterTable(tableID, store.NewTable(tableSchema))
	}
	return cs
}

func createSnapshotAt(t testing.TB, writer SnapshotWriter, cs *store.CommittedState, txID types.TxID) {
	t.Helper()
	cs.SetCommittedTxID(txID)
	if err := writer.CreateSnapshot(cs, txID); err != nil {
		t.Fatal(err)
	}
}

func rewriteSnapshotHeaderTxID(t testing.TB, snapshotDir string, txID types.TxID) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(snapshotDir, snapshotFileName), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(txID))
	if _, err := f.WriteAt(buf[:], 8); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndReadSnapshotAllowsEmptyBootstrapState(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	cs := buildEmptySnapshotCommittedState(t, reg)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	if err := writer.CreateSnapshot(cs, 0); err != nil {
		t.Fatal(err)
	}
	data, err := ReadSnapshot(filepath.Join(root, "snapshots", "0"))
	if err != nil {
		t.Fatal(err)
	}
	if data.TxID != 0 {
		t.Fatalf("snapshot txID = %d, want 0", data.TxID)
	}
	for _, table := range data.Tables {
		if len(table.Rows) != 0 {
			t.Fatalf("bootstrap snapshot table %d rows = %d, want empty", table.TableID, len(table.Rows))
		}
		if nextID := data.NextIDs[table.TableID]; nextID != 1 {
			t.Fatalf("bootstrap snapshot table %d next_id = %d, want 1", table.TableID, nextID)
		}
	}
}

func TestCreateSnapshotRejectsBootstrapAdvancedNextID(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	cs := buildEmptySnapshotCommittedState(t, reg)
	players, ok := cs.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	players.SetNextID(2)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	err := writer.CreateSnapshot(cs, 0)
	if err == nil {
		t.Fatal("expected bootstrap snapshot with advanced next_id to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("CreateSnapshot error = %v, want ErrSnapshot category", err)
	}
	for _, want := range []string{"bootstrap tx 0", "next_id 2", "table 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSnapshot error = %v, want detail %q", err, want)
		}
	}
}

func TestCreateSnapshotRejectsTerminalHorizonBeforeArtifacts(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	cs := buildEmptySnapshotCommittedState(t, reg)
	maxTxID := ^types.TxID(0)
	cs.SetCommittedTxID(maxTxID)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	err := writer.CreateSnapshot(cs, maxTxID)
	if err == nil {
		t.Fatal("expected terminal snapshot horizon to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("CreateSnapshot error = %v, want ErrSnapshot category", err)
	}
	for _, want := range []string{"leaves no next tx_id", "18446744073709551615"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSnapshot error = %v, want detail %q", err, want)
		}
	}
	snapshotDir := filepath.Join(root, "snapshots", strconv.FormatUint(uint64(maxTxID), 10))
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatalf("terminal snapshot dir stat err = %v, want not created", err)
	}
}

func TestCreateSnapshotRejectsBootstrapRows(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	cs.SetCommittedTxID(0)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	err := writer.CreateSnapshot(cs, 0)
	if err == nil {
		t.Fatal("expected bootstrap snapshot with rows to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("CreateSnapshot error = %v, want ErrSnapshot category", err)
	}
	for _, want := range []string{"bootstrap tx 0", "contains 2 rows"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSnapshot error = %v, want detail %q", err, want)
		}
	}
	snapshotDir := filepath.Join(root, "snapshots", "0")
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after bootstrap rejection")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("bootstrap snapshot file stat err = %v, want missing final snapshot", err)
	}
}

func TestReadSnapshotRejectsBootstrapAdvancedNextID(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	cs := buildEmptySnapshotCommittedState(t, reg)
	players, ok := cs.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	players.SetNextID(2)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 1)
	snapshotDir := filepath.Join(root, "snapshots", "1")
	rewriteSnapshotHeaderTxID(t, snapshotDir, 0)

	data, err := ReadSnapshot(snapshotDir)
	if err == nil {
		t.Fatalf("ReadSnapshot accepted bootstrap snapshot with advanced next_id: %+v", data)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	for _, want := range []string{"bootstrap tx 0", "next_id 2", "table 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ReadSnapshot error = %v, want detail %q", err, want)
		}
	}
}

func TestReadSnapshotRejectsZeroNextID(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 1, nil)
	rewriteSnapshotNextID(t, root, 1, 0, 0)

	data, err := ReadSnapshot(filepath.Join(root, "snapshots", "1"))
	if err == nil {
		t.Fatalf("ReadSnapshot accepted zero next_id: %+v", data)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "snapshot next_id 0 for table 0 is below initial row ID 1") {
		t.Fatalf("ReadSnapshot error = %v, want zero next_id detail", err)
	}
}

func TestReadSnapshotRejectsNextIDBelowRows(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 1, map[uint64]string{1: "alice", 2: "bob"})
	rewriteSnapshotNextID(t, root, 1, 0, 1)

	data, err := ReadSnapshot(filepath.Join(root, "snapshots", "1"))
	if err == nil {
		t.Fatalf("ReadSnapshot accepted regressed next_id: %+v", data)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	var allocatorErr *SnapshotAllocatorBoundsError
	if !errors.As(err, &allocatorErr) {
		t.Fatalf("ReadSnapshot error = %v, want SnapshotAllocatorBoundsError", err)
	}
	if allocatorErr.TableID != 0 || allocatorErr.NextID != 1 || allocatorErr.MinNext != 3 {
		t.Fatalf("allocator bounds error = %+v, want table 0 next 1 min 3", allocatorErr)
	}
}

func TestReadSnapshotRejectsZeroSequence(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryAutoIncrementRegistry(t)
	committed := buildEmptySnapshotCommittedState(t, reg)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 1)
	rewriteSnapshotSequence(t, root, 1, 0, 0)

	data, err := ReadSnapshot(filepath.Join(root, "snapshots", "1"))
	if err == nil {
		t.Fatalf("ReadSnapshot accepted zero sequence: %+v", data)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "snapshot sequence 0 for table 0 is below initial value 1") {
		t.Fatalf("ReadSnapshot error = %v, want zero sequence detail", err)
	}
}

func TestReadSnapshotRejectsSequenceBelowRows(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)
	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("seed-1")}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("seed-2")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(3)
	createSnapshotAt(t, NewSnapshotWriter(filepath.Join(root, "snapshots"), reg), committed, 1)
	rewriteSnapshotSequence(t, root, 1, 0, 1)

	data, err := ReadSnapshot(filepath.Join(root, "snapshots", "1"))
	if err == nil {
		t.Fatalf("ReadSnapshot accepted regressed sequence: %+v", data)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	var sequenceErr *SnapshotSequenceBoundsError
	if !errors.As(err, &sequenceErr) {
		t.Fatalf("ReadSnapshot error = %v, want SnapshotSequenceBoundsError", err)
	}
	if sequenceErr.TableID != 0 || sequenceErr.Next != 1 || sequenceErr.MinNext != 3 {
		t.Fatalf("sequence bounds error = %+v, want table 0 next 1 min 3", sequenceErr)
	}
}

func TestReadSnapshotUint8AutoIncrementSequenceBounds(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryUint8AutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)
	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint8(1), types.NewString("seed")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(2)
	createSnapshotAt(t, NewSnapshotWriter(filepath.Join(root, "snapshots"), reg), committed, 1)

	data, err := ReadSnapshot(filepath.Join(root, "snapshots", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if seq := data.Sequences[0]; seq != 2 {
		t.Fatalf("snapshot sequence = %d, want 2", seq)
	}
}

func TestReadSnapshotRejectsBootstrapRows(t *testing.T) {
	root := t.TempDir()
	cs, reg := buildSnapshotCommittedState(t)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 1)
	snapshotDir := filepath.Join(root, "snapshots", "1")
	rewriteSnapshotHeaderTxID(t, snapshotDir, 0)

	data, err := ReadSnapshot(snapshotDir)
	if err == nil {
		t.Fatalf("ReadSnapshot accepted bootstrap snapshot with rows: %+v", data)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	for _, want := range []string{"bootstrap tx 0", "contains 2 rows"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ReadSnapshot error = %v, want detail %q", err, want)
		}
	}
}

func TestSnapshotPublicAPIContractCompiles(t *testing.T) {
	var _ = SnapshotMagic
	var _ uint8 = SnapshotVersion
	var _ int = SnapshotHeaderSize
	_ = ComputeSnapshotHash
	_ = HasLockFile
	_ = CreateLockFile
	_ = RemoveLockFile
	_ = EncodeSchemaSnapshot
	_ = DecodeSchemaSnapshot
	_ = ReadSnapshot
	_ = ListSnapshots
}

func TestSnapshotHashAndLockHelpers(t *testing.T) {
	a := ComputeSnapshotHash([]byte("same"))
	b := ComputeSnapshotHash([]byte("same"))
	c := ComputeSnapshotHash([]byte("different"))
	if a != b {
		t.Fatal("same snapshot data should hash identically")
	}
	if a == c {
		t.Fatal("different snapshot data should hash differently")
	}

	dir := t.TempDir()
	if HasLockFile(dir) {
		t.Fatal("fresh dir should not have lockfile")
	}
	if err := CreateLockFile(dir); err != nil {
		t.Fatal(err)
	}
	if !HasLockFile(dir) {
		t.Fatal("CreateLockFile should create visible lock")
	}
	if err := RemoveLockFile(dir); err != nil {
		t.Fatal(err)
	}
	if HasLockFile(dir) {
		t.Fatal("RemoveLockFile should remove visible lock")
	}
}

func TestSchemaSnapshotCodecRoundTrip(t *testing.T) {
	_, reg := testSchema()
	var buf bytes.Buffer
	if err := EncodeSchemaSnapshot(&buf, reg); err != nil {
		t.Fatal(err)
	}
	tables, version, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if version != reg.Version() {
		t.Fatalf("decoded version = %d, want %d", version, reg.Version())
	}
	if len(tables) != len(reg.Tables()) {
		t.Fatalf("decoded tables = %d, want %d", len(tables), len(reg.Tables()))
	}
}

func TestDecodeSchemaSnapshotRejectsTrailingBytes(t *testing.T) {
	_, reg := testSchema()
	var buf bytes.Buffer
	if err := EncodeSchemaSnapshot(&buf, reg); err != nil {
		t.Fatal(err)
	}
	buf.Write([]byte{0xde, 0xad, 0xbe, 0xef})

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected trailing schema snapshot bytes to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("trailing schema snapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "trailing schema snapshot bytes") {
		t.Fatalf("trailing schema snapshot error = %v, want explicit trailing-bytes detail", err)
	}
}

func TestEncodeSchemaSnapshotRejectsShortWrite(t *testing.T) {
	_, reg := testSchema()
	err := EncodeSchemaSnapshot(shortWriteSink{}, reg)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("EncodeSchemaSnapshot short write error = %v, want io.ErrShortWrite", err)
	}
}

func TestWriteStringRejectsShortWrite(t *testing.T) {
	err := writeString(shortWriteSink{}, "players")
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeString short write error = %v, want io.ErrShortWrite", err)
	}
}

func TestSnapshotNumericWritesRejectShortWrite(t *testing.T) {
	if err := writeUint32Full(shortWriteSink{}, 1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeUint32Full short write error = %v, want io.ErrShortWrite", err)
	}
	if err := writeUint64Full(shortWriteSink{}, 1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeUint64Full short write error = %v, want io.ErrShortWrite", err)
	}
}

func TestDecodeSchemaSnapshotRejectsOversizedStringLength(t *testing.T) {
	var buf bytes.Buffer
	writeUint32(t, &buf, 1) // schema snapshot version
	writeUint32(t, &buf, 1) // table count
	writeUint32(t, &buf, 0) // table ID
	writeUint32(t, &buf, maxSnapshotStringBytes+1)

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected oversized schema string length to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("oversized schema string error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "schema string") {
		t.Fatalf("oversized schema string error = %v, want schema string detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsInvalidUTF8String(t *testing.T) {
	var buf bytes.Buffer
	writeUint32(t, &buf, 1) // schema snapshot version
	writeUint32(t, &buf, 1) // table count
	writeUint32(t, &buf, 0) // table ID
	writeUint32(t, &buf, 1) // table name length
	buf.WriteByte(0xff)

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if err == nil {
		t.Fatal("expected invalid UTF-8 schema string to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("invalid UTF-8 schema string error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "invalid UTF-8 schema string") {
		t.Fatalf("invalid UTF-8 schema string error = %v, want invalid UTF-8 detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsInvalidBoolFlags(t *testing.T) {
	validColumnFlags := [3]byte{byte(schema.KindUint64), 0, 0}
	validIndexFlags := [2]byte{1, 1}
	for _, tc := range []struct {
		name        string
		columnFlags [3]byte
		indexFlags  [2]byte
		wantDetail  string
	}{
		{
			name:        "column-nullable",
			columnFlags: [3]byte{byte(schema.KindUint64), 2, 0},
			indexFlags:  validIndexFlags,
			wantDetail:  "column nullable",
		},
		{
			name:        "column-auto-increment",
			columnFlags: [3]byte{byte(schema.KindUint64), 0, 2},
			indexFlags:  validIndexFlags,
			wantDetail:  "column auto_increment",
		},
		{
			name:        "index-unique",
			columnFlags: validColumnFlags,
			indexFlags:  [2]byte{2, 1},
			wantDetail:  "index unique",
		},
		{
			name:        "index-primary",
			columnFlags: validColumnFlags,
			indexFlags:  [2]byte{1, 2},
			wantDetail:  "index primary",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := encodeSchemaSnapshotWithFlags(t, tc.columnFlags, tc.indexFlags)
			_, _, err := DecodeSchemaSnapshot(bytes.NewReader(data))
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func TestDecodeSchemaSnapshotRejectsInvalidColumnType(t *testing.T) {
	data := encodeSchemaSnapshotWithFlags(t, [3]byte{byte(schema.KindUUID) + 1, 0, 0}, [2]byte{1, 1})
	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(data))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), `invalid schema snapshot column "id" type`) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want invalid column type detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsInvalidAutoIncrementType(t *testing.T) {
	data := encodeSchemaSnapshotWithFlags(t, [3]byte{byte(schema.KindString), 0, 1}, [2]byte{1, 1})
	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(data))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), `schema snapshot column "id" in table 0 has invalid auto_increment type String`) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want invalid auto_increment type detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsPrimaryIndexWithoutUniqueFlag(t *testing.T) {
	data := encodeSchemaSnapshotWithFlags(t, [3]byte{byte(schema.KindUint64), 0, 0}, [2]byte{0, 1})
	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(data))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), `schema snapshot primary index "primary" in table 0 is not unique`) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want primary-not-unique detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsInvalidKeyConstraints(t *testing.T) {
	for _, tc := range []struct {
		name       string
		writeData  func(*bytes.Buffer)
		wantDetail string
	}{
		{
			name: "auto-increment-without-unique-index",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "jobs"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 1) // column count
				writeUint32(t, buf, 0) // column index
				if err := writeString(buf, "id"); err != nil {
					t.Fatal(err)
				}
				buf.Write([]byte{byte(schema.KindUint64), 0, 1})
				writeUint32(t, buf, 0) // index count
			},
			wantDetail: `schema snapshot auto_increment column "id" in table 0 is not backed by a unique index`,
		},
		{
			name: "multiple-primary-indexes",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 2) // column count
				writeSchemaSnapshotColumn(t, buf, 0, "id")
				writeSchemaSnapshotColumn(t, buf, 1, "name")
				writeUint32(t, buf, 2) // index count
				writeSchemaSnapshotIndex(t, buf, "primary_id", 0)
				writeSchemaSnapshotIndex(t, buf, "primary_name", 1)
			},
			wantDetail: "schema snapshot table 0 has multiple primary indexes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.writeData(&buf)

			_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func TestDecodeSchemaSnapshotRejectsDuplicateTableIDs(t *testing.T) {
	var buf bytes.Buffer
	writeUint32(t, &buf, 1) // schema snapshot version
	writeUint32(t, &buf, 2) // table count
	writeMinimalSchemaSnapshotTable(t, &buf, 0, "players")
	writeMinimalSchemaSnapshotTable(t, &buf, 0, "players-again")

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "duplicate schema snapshot table ID 0") {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want duplicate table ID detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsDuplicateNames(t *testing.T) {
	for _, tc := range []struct {
		name       string
		writeData  func(*bytes.Buffer)
		wantDetail string
	}{
		{
			name: "table",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 2) // table count
				writeMinimalSchemaSnapshotTable(t, buf, 0, "players")
				writeMinimalSchemaSnapshotTable(t, buf, 1, "players")
			},
			wantDetail: `duplicate schema snapshot table name "players"`,
		},
		{
			name: "column",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 2) // column count
				writeSchemaSnapshotColumn(t, buf, 0, "id")
				writeSchemaSnapshotColumn(t, buf, 1, "id")
				writeUint32(t, buf, 0) // index count
			},
			wantDetail: `duplicate schema snapshot column name "id" in table 0`,
		},
		{
			name: "index",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 1) // column count
				writeSchemaSnapshotColumn(t, buf, 0, "id")
				writeUint32(t, buf, 2) // index count
				writeSchemaSnapshotIndex(t, buf, "primary", 0)
				writeSchemaSnapshotIndex(t, buf, "primary", 0)
			},
			wantDetail: `duplicate schema snapshot index name "primary" in table 0`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.writeData(&buf)

			_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func TestDecodeSchemaSnapshotRejectsDuplicateColumnIndexes(t *testing.T) {
	var buf bytes.Buffer
	writeUint32(t, &buf, 1) // schema snapshot version
	writeUint32(t, &buf, 1) // table count
	writeUint32(t, &buf, 0) // table ID
	if err := writeString(&buf, "players"); err != nil {
		t.Fatal(err)
	}
	writeUint32(t, &buf, 2) // column count
	writeSchemaSnapshotColumn(t, &buf, 0, "id")
	writeSchemaSnapshotColumn(t, &buf, 0, "id-again")
	writeUint32(t, &buf, 0) // index count

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "duplicate schema snapshot column index 0 in table 0") {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want duplicate column index detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsIndexUnknownColumn(t *testing.T) {
	var buf bytes.Buffer
	writeUint32(t, &buf, 1) // schema snapshot version
	writeUint32(t, &buf, 1) // table count
	writeUint32(t, &buf, 0) // table ID
	if err := writeString(&buf, "players"); err != nil {
		t.Fatal(err)
	}
	writeUint32(t, &buf, 1) // column count
	writeSchemaSnapshotColumn(t, &buf, 0, "id")
	writeUint32(t, &buf, 1) // index count
	if err := writeString(&buf, "primary"); err != nil {
		t.Fatal(err)
	}
	buf.Write([]byte{1, 1}) // unique, primary
	writeUint32(t, &buf, 1) // index column count
	writeUint32(t, &buf, 99)

	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), `schema snapshot index "primary" references unknown column index 99 in table 0`) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want unknown index column detail", err)
	}
}

func TestDecodeSchemaSnapshotRejectsEmptySchemaSections(t *testing.T) {
	for _, tc := range []struct {
		name       string
		writeData  func(*bytes.Buffer)
		wantDetail string
	}{
		{
			name: "table-name",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, ""); err != nil {
					t.Fatal(err)
				}
			},
			wantDetail: "schema snapshot table 0 has empty name",
		},
		{
			name: "columns",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 0) // column count
			},
			wantDetail: "schema snapshot table 0 has no columns",
		},
		{
			name: "column-name",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 1) // column count
				writeSchemaSnapshotColumn(t, buf, 0, "")
			},
			wantDetail: "schema snapshot column 0 in table 0 has empty name",
		},
		{
			name: "index-name",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 1) // column count
				writeSchemaSnapshotColumn(t, buf, 0, "id")
				writeUint32(t, buf, 1) // index count
				writeSchemaSnapshotIndex(t, buf, "", 0)
			},
			wantDetail: "schema snapshot index 0 in table 0 has empty name",
		},
		{
			name: "index-columns",
			writeData: func(buf *bytes.Buffer) {
				writeUint32(t, buf, 1) // schema snapshot version
				writeUint32(t, buf, 1) // table count
				writeUint32(t, buf, 0) // table ID
				if err := writeString(buf, "players"); err != nil {
					t.Fatal(err)
				}
				writeUint32(t, buf, 1) // column count
				writeSchemaSnapshotColumn(t, buf, 0, "id")
				writeUint32(t, buf, 1) // index count
				if err := writeString(buf, "primary"); err != nil {
					t.Fatal(err)
				}
				buf.Write([]byte{1, 1}) // unique, primary
				writeUint32(t, buf, 0)  // index column count
			},
			wantDetail: `schema snapshot index "primary" in table 0 has no columns`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.writeData(&buf)

			_, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes()))
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("DecodeSchemaSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func writeSchemaSnapshotColumn(t testing.TB, dst *bytes.Buffer, index uint32, name string) {
	t.Helper()
	writeUint32(t, dst, index)
	if err := writeString(dst, name); err != nil {
		t.Fatal(err)
	}
	dst.Write([]byte{byte(schema.KindUint64), 0, 0})
}

func writeSchemaSnapshotIndex(t testing.TB, dst *bytes.Buffer, name string, columns ...uint32) {
	t.Helper()
	if err := writeString(dst, name); err != nil {
		t.Fatal(err)
	}
	dst.Write([]byte{1, 1})
	writeUint32(t, dst, uint32(len(columns)))
	for _, col := range columns {
		writeUint32(t, dst, col)
	}
}

func writeMinimalSchemaSnapshotTable(t testing.TB, dst *bytes.Buffer, tableID uint32, name string) {
	t.Helper()
	writeUint32(t, dst, tableID)
	if err := writeString(dst, name); err != nil {
		t.Fatal(err)
	}
	writeUint32(t, dst, 1) // column count
	writeSchemaSnapshotColumn(t, dst, 0, "id")
	writeUint32(t, dst, 0) // index count
}

func encodeSchemaSnapshotWithFlags(t testing.TB, columnFlags [3]byte, indexFlags [2]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writeUint32(t, &buf, 1) // schema snapshot version
	writeUint32(t, &buf, 1) // table count
	writeUint32(t, &buf, 0) // table ID
	if err := writeString(&buf, "players"); err != nil {
		t.Fatal(err)
	}
	writeUint32(t, &buf, 1) // column count
	writeUint32(t, &buf, 0) // column index
	if err := writeString(&buf, "id"); err != nil {
		t.Fatal(err)
	}
	buf.Write(columnFlags[:])
	writeUint32(t, &buf, 1) // index count
	if err := writeString(&buf, "primary"); err != nil {
		t.Fatal(err)
	}
	buf.Write(indexFlags[:])
	writeUint32(t, &buf, 1) // index column count
	writeUint32(t, &buf, 0) // indexed column
	return buf.Bytes()
}

func TestDecodeSchemaSnapshotDoesNotPreallocateClaimedTableCount(t *testing.T) {
	var buf bytes.Buffer
	writeUint32(t, &buf, 1)
	writeUint32(t, &buf, ^uint32(0))

	if _, _, err := DecodeSchemaSnapshot(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected claimed table count without table bytes to fail")
	}
}

func TestCreateAndReadSnapshotRoundTrip(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 77)

	data, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "77"))
	if err != nil {
		t.Fatal(err)
	}
	if data.TxID != 77 {
		t.Fatalf("snapshot txID = %d, want 77", data.TxID)
	}
	if data.SchemaVersion != reg.Version() {
		t.Fatalf("snapshot schema version = %d, want %d", data.SchemaVersion, reg.Version())
	}
	playerRows := -1
	for _, table := range data.Tables {
		if table.TableID == 0 {
			playerRows = len(table.Rows)
			if len(table.Rows) >= 1 && table.Rows[0][1].AsString() != "alice" {
				t.Fatalf("first player snapshot row = %v, want alice", table.Rows[0])
			}
		}
	}
	if playerRows != 2 {
		t.Fatalf("players snapshot row count = %d, want 2 (tables=%+v)", playerRows, data.Tables)
	}
}

func TestReadSnapshotRejectsSymlinkSnapshotFile(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 77)
	replaceSnapshotFileWithSymlinkCandidate(t, baseDir, 77)

	_, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "77"))
	if err == nil {
		t.Fatal("expected symlink snapshot file to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("ReadSnapshot error = %v, want regular-file rejection detail", err)
	}
}

func TestReadSnapshotRejectsDirectorySnapshotFileWithoutRemoving(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 77)
	snapshotPath := filepath.Join(baseDir, "snapshots", "77", snapshotFileName)
	if err := os.Remove(snapshotPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(snapshotPath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "77"))
	if err == nil {
		t.Fatal("expected directory snapshot file artifact to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("ReadSnapshot error = %v, want regular-file rejection detail", err)
	}
	assertDirectoryArtifactExists(t, snapshotPath)
}

func TestSnapshotBodyRejectsShortWrite(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	cs.SetCommittedTxID(91)
	writer := NewSnapshotWriter(filepath.Join(t.TempDir(), "snapshots"), reg).(*FileSnapshotWriter)
	err := writer.writeSnapshotBody(shortWriteSink{}, cs, 91)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeSnapshotBody short write error = %v, want io.ErrShortWrite", err)
	}
}

func TestSnapshotOmitsSequenceEntriesForTablesWithoutAutoincrement(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 78)

	data, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "78"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data.Sequences[0]; ok {
		t.Fatalf("players table should not have a sequence entry: %+v", data.Sequences)
	}
	if _, ok := data.Sequences[1]; ok {
		t.Fatalf("sys_clients table should not have a sequence entry: %+v", data.Sequences)
	}
	if got, ok := data.Sequences[2]; !ok || got == 0 {
		t.Fatalf("autoincrement sys_scheduled table should keep its sequence entry, got %+v", data.Sequences)
	}
}

func TestListSnapshotsSkipsLockAndSortsNewestFirst(t *testing.T) {
	baseDir := t.TempDir()
	for _, txID := range []uint64{10, 20} {
		dir := filepath.Join(baseDir, txIDString(txID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "snapshot"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(baseDir, txIDString(30)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CreateLockFile(filepath.Join(baseDir, txIDString(30))); err != nil {
		t.Fatal(err)
	}

	ids, err := ListSnapshots(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 20 || ids[1] != 10 {
		t.Fatalf("ListSnapshots = %v, want [20 10]", ids)
	}
}

func TestListSnapshotsSkipsMalformedNamesAndMarkerDirectories(t *testing.T) {
	baseDir := t.TempDir()
	for _, txID := range []uint64{10, 20} {
		dir := filepath.Join(baseDir, txIDString(txID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, snapshotFileName), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"not-a-snapshot", "18446744073709551616", "00000000000000000030"} {
		if err := os.MkdirAll(filepath.Join(baseDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(baseDir, "50"), []byte("not a snapshot directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "30", ".lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "40", snapshotTempFileName), 0o755); err != nil {
		t.Fatal(err)
	}

	ids, err := ListSnapshots(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 20 || ids[1] != 10 {
		t.Fatalf("ListSnapshots = %v, want [20 10]", ids)
	}
}

func TestListSnapshotsSkipsMarkerSymlinksAndRegularTempArtifacts(t *testing.T) {
	baseDir := t.TempDir()
	for _, txID := range []uint64{10, 20} {
		dir := filepath.Join(baseDir, txIDString(txID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, snapshotFileName), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	targetDir := t.TempDir()
	lockTarget := filepath.Join(targetDir, "lock-target")
	tempTarget := filepath.Join(targetDir, "temp-target")
	lockBefore := []byte("external lock marker target")
	tempBefore := []byte("external temp marker target")
	if err := os.WriteFile(lockTarget, lockBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempTarget, tempBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(baseDir, "30")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, lockTarget, filepath.Join(lockDir, ".lock"))
	tempSymlinkDir := filepath.Join(baseDir, "40")
	if err := os.MkdirAll(tempSymlinkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, tempTarget, filepath.Join(tempSymlinkDir, snapshotTempFileName))
	tempRegularDir := filepath.Join(baseDir, "50")
	if err := os.MkdirAll(tempRegularDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempRegularDir, snapshotTempFileName), []byte("regular temp marker"), 0o644); err != nil {
		t.Fatal(err)
	}

	ids, err := ListSnapshots(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 20 || ids[1] != 10 {
		t.Fatalf("ListSnapshots = %v, want [20 10]", ids)
	}
	assertSymlinkExists(t, filepath.Join(lockDir, ".lock"))
	assertSymlinkExists(t, filepath.Join(tempSymlinkDir, snapshotTempFileName))
	assertFileBytes(t, lockTarget, lockBefore)
	assertFileBytes(t, tempTarget, tempBefore)
}

func TestReadSnapshotHashMismatch(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 88)
	path := filepath.Join(baseDir, "snapshots", "88", "snapshot")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "88"))
	var hashErr *SnapshotHashMismatchError
	if !errors.As(err, &hashErr) {
		t.Fatalf("expected SnapshotHashMismatchError, got %v", err)
	}
}

func TestReadSnapshotRejectsTrailingPayloadBytes(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 90)
	path := filepath.Join(baseDir, "snapshots", "90", snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, 0xde, 0xad, 0xbe, 0xef)
	hash := ComputeSnapshotHash(data[SnapshotHeaderSize:])
	copy(data[20:52], hash[:])
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "90"))
	if err == nil {
		t.Fatal("expected trailing snapshot bytes to fail")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("trailing snapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "trailing snapshot bytes") {
		t.Fatalf("trailing snapshot error = %v, want explicit trailing-bytes detail", err)
	}
}

func TestReadSnapshotRejectsOversizedSectionsBeforeAllocation(t *testing.T) {
	_, reg := testSchema()
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, reg); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		body       []byte
		wantDetail string
	}{
		{
			name: "schema-section",
			body: func() []byte {
				var body bytes.Buffer
				writeUint32(t, &body, DefaultCommitLogOptions().MaxRecordPayloadBytes+1)
				return body.Bytes()
			}(),
			wantDetail: "snapshot schema section",
		},
		{
			name: "row-section",
			body: func() []byte {
				var body bytes.Buffer
				writeUint32(t, &body, uint32(schemaBuf.Len()))
				body.Write(schemaBuf.Bytes())
				writeUint32(t, &body, 0) // sequence entries
				writeUint32(t, &body, 0) // next ID entries
				writeUint32(t, &body, 1) // table sections
				writeUint32(t, &body, 0) // players table
				writeUint32(t, &body, 1) // row count
				writeUint32(t, &body, DefaultCommitLogOptions().MaxRowBytes+1)
				return body.Bytes()
			}(),
			wantDetail: "snapshot row section",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			snapshotDir := filepath.Join(baseDir, "snapshots", "91")
			writeSnapshotBytes(t, snapshotDir, 91, reg.Version(), tc.body)

			_, err := ReadSnapshot(snapshotDir)
			if err == nil {
				t.Fatal("expected oversized snapshot section to fail")
			}
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("oversized snapshot section error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("oversized snapshot section error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func TestReadSnapshotDoesNotPreallocateClaimedTableCount(t *testing.T) {
	_, reg := testSchema()
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, reg); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writeUint32(t, &body, uint32(schemaBuf.Len()))
	body.Write(schemaBuf.Bytes())
	writeUint32(t, &body, 0) // sequence entries
	writeUint32(t, &body, 0) // next ID entries
	writeUint32(t, &body, ^uint32(0))

	baseDir := t.TempDir()
	snapshotDir := filepath.Join(baseDir, "snapshots", "92")
	writeSnapshotBytes(t, snapshotDir, 92, reg.Version(), body.Bytes())

	if _, err := ReadSnapshot(snapshotDir); err == nil {
		t.Fatal("expected claimed snapshot table count without table bytes to fail")
	}
}

func TestReadSnapshotRejectsDuplicateUint64MapTableIDs(t *testing.T) {
	_, reg := testSchema()
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, reg); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		writeMaps  func(*bytes.Buffer)
		wantDetail string
	}{
		{
			name: "sequence",
			writeMaps: func(body *bytes.Buffer) {
				writeUint32(t, body, 2)
				writeUint32(t, body, 2)
				writeUint64(t, body, 11)
				writeUint32(t, body, 2)
				writeUint64(t, body, 12)
				writeUint32(t, body, 0)
			},
			wantDetail: "duplicate snapshot sequence table ID 2",
		},
		{
			name: "next-id",
			writeMaps: func(body *bytes.Buffer) {
				writeUint32(t, body, 0)
				writeUint32(t, body, 2)
				writeUint32(t, body, 0)
				writeUint64(t, body, 21)
				writeUint32(t, body, 0)
				writeUint64(t, body, 22)
			},
			wantDetail: "duplicate snapshot next_id table ID 0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writeUint32(t, &body, uint32(schemaBuf.Len()))
			body.Write(schemaBuf.Bytes())
			tc.writeMaps(&body)
			writeUint32(t, &body, 0) // table sections

			baseDir := t.TempDir()
			snapshotDir := filepath.Join(baseDir, "snapshots", "93")
			writeSnapshotBytes(t, snapshotDir, 93, reg.Version(), body.Bytes())

			_, err := ReadSnapshot(snapshotDir)
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("ReadSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func TestReadSnapshotRejectsDuplicateTableSections(t *testing.T) {
	_, reg := testSchema()
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, reg); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writeUint32(t, &body, uint32(schemaBuf.Len()))
	body.Write(schemaBuf.Bytes())
	writeUint32(t, &body, 0) // sequence entries
	writeUint32(t, &body, 0) // next ID entries
	writeUint32(t, &body, 2) // table sections
	writeUint32(t, &body, 0) // players table
	writeUint32(t, &body, 0) // row count
	writeUint32(t, &body, 0) // duplicate players table
	writeUint32(t, &body, 0) // row count

	baseDir := t.TempDir()
	snapshotDir := filepath.Join(baseDir, "snapshots", "94")
	writeSnapshotBytes(t, snapshotDir, 94, reg.Version(), body.Bytes())

	_, err := ReadSnapshot(snapshotDir)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "duplicate snapshot table section 0") {
		t.Fatalf("ReadSnapshot error = %v, want duplicate table section detail", err)
	}
}

func TestReadSnapshotRejectsUnknownTableReferences(t *testing.T) {
	_, reg := testSchema()
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, reg); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		writeBody  func(*bytes.Buffer)
		wantDetail string
	}{
		{
			name: "sequence",
			writeBody: func(body *bytes.Buffer) {
				writeUint32(t, body, 1)
				writeUint32(t, body, 99)
				writeUint64(t, body, 11)
				writeUint32(t, body, 0)
				writeUint32(t, body, 0)
			},
			wantDetail: "snapshot sequence references unknown table 99",
		},
		{
			name: "next-id",
			writeBody: func(body *bytes.Buffer) {
				writeUint32(t, body, 0)
				writeUint32(t, body, 1)
				writeUint32(t, body, 99)
				writeUint64(t, body, 21)
				writeUint32(t, body, 0)
			},
			wantDetail: "snapshot next_id references unknown table 99",
		},
		{
			name: "table-section",
			writeBody: func(body *bytes.Buffer) {
				writeUint32(t, body, 0)
				writeUint32(t, body, 0)
				writeUint32(t, body, 1)
				writeUint32(t, body, 99)
				writeUint32(t, body, 0)
			},
			wantDetail: "snapshot table section references unknown table 99",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writeUint32(t, &body, uint32(schemaBuf.Len()))
			body.Write(schemaBuf.Bytes())
			tc.writeBody(&body)

			baseDir := t.TempDir()
			snapshotDir := filepath.Join(baseDir, "snapshots", "95")
			writeSnapshotBytes(t, snapshotDir, 95, reg.Version(), body.Bytes())

			_, err := ReadSnapshot(snapshotDir)
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("ReadSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func TestReadSnapshotRejectsSequenceForTableWithoutAutoIncrement(t *testing.T) {
	_, reg := testSchema()
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, reg); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writeUint32(t, &body, uint32(schemaBuf.Len()))
	body.Write(schemaBuf.Bytes())
	writeUint32(t, &body, 1)  // sequence entries
	writeUint32(t, &body, 0)  // players table has no autoincrement column
	writeUint64(t, &body, 11) // sequence value
	writeUint32(t, &body, 0)  // next ID entries
	writeUint32(t, &body, 0)  // table sections

	baseDir := t.TempDir()
	snapshotDir := filepath.Join(baseDir, "snapshots", "96")
	writeSnapshotBytes(t, snapshotDir, 96, reg.Version(), body.Bytes())

	_, err := ReadSnapshot(snapshotDir)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "snapshot sequence references table 0 without autoincrement column") {
		t.Fatalf("ReadSnapshot error = %v, want non-autoincrement sequence detail", err)
	}
}

func TestReadSnapshotRejectsMissingTableMetadata(t *testing.T) {
	for _, tc := range []struct {
		name          string
		autoIncrement bool
		writeMetadata func(*bytes.Buffer)
		wantDetail    string
	}{
		{
			name:          "next-id",
			autoIncrement: false,
			writeMetadata: func(body *bytes.Buffer) {
				writeUint32(t, body, 0) // sequence entries
				writeUint32(t, body, 0) // next ID entries
				writeUint32(t, body, 1) // table sections
				writeUint32(t, body, 0) // table ID
				writeUint32(t, body, 0) // row count
			},
			wantDetail: "snapshot missing next_id for table 0",
		},
		{
			name:          "table-section",
			autoIncrement: false,
			writeMetadata: func(body *bytes.Buffer) {
				writeUint32(t, body, 0) // sequence entries
				writeUint32(t, body, 1) // next ID entries
				writeUint32(t, body, 0) // table ID
				writeUint64(t, body, 1) // next row ID
				writeUint32(t, body, 0) // table sections
			},
			wantDetail: "snapshot missing table section for table 0",
		},
		{
			name:          "sequence",
			autoIncrement: true,
			writeMetadata: func(body *bytes.Buffer) {
				writeUint32(t, body, 0) // sequence entries
				writeUint32(t, body, 1) // next ID entries
				writeUint32(t, body, 0) // table ID
				writeUint64(t, body, 1) // next row ID
				writeUint32(t, body, 1) // table sections
				writeUint32(t, body, 0) // table ID
				writeUint32(t, body, 0) // row count
			},
			wantDetail: "snapshot missing sequence for autoincrement table 0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schemaBuf := encodeSingleTableSchemaSnapshot(t, tc.autoIncrement)
			var body bytes.Buffer
			writeUint32(t, &body, uint32(len(schemaBuf)))
			body.Write(schemaBuf)
			tc.writeMetadata(&body)

			baseDir := t.TempDir()
			snapshotDir := filepath.Join(baseDir, "snapshots", "97")
			writeSnapshotBytes(t, snapshotDir, 97, 1, body.Bytes())

			_, err := ReadSnapshot(snapshotDir)
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("ReadSnapshot error = %v, want ErrSnapshot category", err)
			}
			if !strings.Contains(err.Error(), tc.wantDetail) {
				t.Fatalf("ReadSnapshot error = %v, want %q detail", err, tc.wantDetail)
			}
		})
	}
}

func encodeSingleTableSchemaSnapshot(t testing.TB, autoIncrement bool) []byte {
	t.Helper()
	var schemaBuf bytes.Buffer
	writeUint32(t, &schemaBuf, 1) // schema snapshot version
	writeUint32(t, &schemaBuf, 1) // table count
	writeUint32(t, &schemaBuf, 0) // table ID
	if err := writeString(&schemaBuf, "players"); err != nil {
		t.Fatal(err)
	}
	writeUint32(t, &schemaBuf, 1) // column count
	writeUint32(t, &schemaBuf, 0) // column index
	if err := writeString(&schemaBuf, "id"); err != nil {
		t.Fatal(err)
	}
	schemaBuf.Write([]byte{byte(schema.KindUint64), 0, boolByte(autoIncrement)})
	if autoIncrement {
		writeUint32(t, &schemaBuf, 1) // index count
		writeSchemaSnapshotIndex(t, &schemaBuf, "primary", 0)
	} else {
		writeUint32(t, &schemaBuf, 0) // index count
	}
	return schemaBuf.Bytes()
}

func TestReadSnapshotRejectsHeaderVersionMismatch(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 89)
	path := filepath.Join(baseDir, "snapshots", "89", snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[4] = SnapshotVersion + 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "89"))
	var versionErr *BadVersionError
	if !errors.As(err, &versionErr) {
		t.Fatalf("expected BadVersionError, got %v", err)
	}
	if versionErr.Got != SnapshotVersion+1 {
		t.Fatalf("bad version got = %d, want %d", versionErr.Got, SnapshotVersion+1)
	}
}

func TestReadSnapshotRejectsNonZeroHeaderPadding(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 44)
	path := filepath.Join(baseDir, "snapshots", "44", snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[5] = 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = ReadSnapshot(filepath.Join(baseDir, "snapshots", "44"))
	if !errors.Is(err, ErrBadFlags) {
		t.Fatalf("ReadSnapshot non-zero padding error = %v, want ErrBadFlags", err)
	}
}

func TestConcurrentSnapshotReturnsInProgress(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	blocking := writer.(*FileSnapshotWriter)
	blocking.beforeWrite = make(chan struct{})
	blocking.continueWrite = make(chan struct{})

	errCh := make(chan error, 1)
	cs.SetCommittedTxID(91)
	go func() { errCh <- writer.CreateSnapshot(cs, 91) }()
	<-blocking.beforeWrite
	if err := writer.CreateSnapshot(cs, 92); !errors.Is(err, ErrSnapshotInProgress) {
		t.Fatalf("second snapshot error = %v, want ErrSnapshotInProgress", err)
	}
	close(blocking.continueWrite)
	if err := <-errCh; err != nil {
		t.Fatalf("first snapshot should complete successfully: %v", err)
	}
}

func TestCreateSnapshotUsesTempFileUntilRename(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	blocking := writer.(*FileSnapshotWriter)
	blocking.beforeWrite = make(chan struct{})
	blocking.continueWrite = make(chan struct{})

	errCh := make(chan error, 1)
	cs.SetCommittedTxID(91)
	go func() { errCh <- writer.CreateSnapshot(cs, 91) }()
	<-blocking.beforeWrite

	released := false
	release := func() {
		if !released {
			close(blocking.continueWrite)
			released = true
		}
	}
	defer release()

	snapshotDir := filepath.Join(baseDir, "snapshots", "91")
	if !HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should exist while temp file is pending")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); err != nil {
		t.Fatalf("snapshot temp file should exist before rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("final snapshot should not exist before rename, stat err=%v", err)
	}

	release()
	if err := <-errCh; err != nil {
		t.Fatalf("snapshot should complete successfully: %v", err)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after completion")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp file should be removed after completion, stat err=%v", err)
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("final snapshot should be readable: %v", err)
	}
}

func TestCreateSnapshotMkdirFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	snapshotBase := filepath.Join(baseDir, "snapshots")
	if err := os.MkdirAll(snapshotBase, 0o755); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(snapshotBase, "95")
	if err := os.WriteFile(snapshotDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	writer := NewSnapshotWriter(snapshotBase, reg)

	cs.SetCommittedTxID(95)
	err := writer.CreateSnapshot(cs, 95)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("mkdir error should be categorized as snapshot error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "mkdir" || completionErr.Path != snapshotDir {
		t.Fatalf("completion error = %+v, want mkdir phase and snapshot dir", completionErr)
	}
}

func TestCreateSnapshotParentSyncFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	snapshotBase := filepath.Join(baseDir, "snapshots")
	writer := NewSnapshotWriter(snapshotBase, reg).(*FileSnapshotWriter)
	syncErr := errors.New("sync parent failed")
	writer.syncDir = func(path string) error {
		if path == snapshotBase {
			return syncErr
		}
		return nil
	}

	cs.SetCommittedTxID(99)
	err := writer.CreateSnapshot(cs, 99)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("sync-parent error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("sync-parent error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-parent" || completionErr.Path != snapshotBase {
		t.Fatalf("completion error = %+v, want sync-parent phase and snapshot base path", completionErr)
	}
	snapshotDir := filepath.Join(snapshotBase, "99")
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should not be created after parent sync failure")
	}
	assertSnapshotPayloadArtifactsMissing(t, snapshotDir)
}

func TestCreateSnapshotLockCreateFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	snapshotBase := filepath.Join(baseDir, "snapshots")
	snapshotDir := filepath.Join(snapshotBase, "100")
	lockPath := filepath.Join(snapshotDir, ".lock")
	if err := os.MkdirAll(lockPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writer := NewSnapshotWriter(snapshotBase, reg)

	cs.SetCommittedTxID(100)
	err := writer.CreateSnapshot(cs, 100)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("create-lock error should be categorized as snapshot error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "create-lock" || completionErr.Path != lockPath {
		t.Fatalf("completion error = %+v, want create-lock phase and lock path", completionErr)
	}
	if !HasLockFile(snapshotDir) {
		t.Fatal("pre-existing lock artifact should remain after create-lock failure")
	}
	assertSnapshotPayloadArtifactsMissing(t, snapshotDir)
}

func TestCreateSnapshotOpenTempFailureReturnsSnapshotCompletionErrorAndCleansArtifacts(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	snapshotBase := filepath.Join(baseDir, "snapshots")
	snapshotDir := filepath.Join(snapshotBase, "96")
	tmpPath := filepath.Join(snapshotDir, snapshotTempFileName)
	if err := os.MkdirAll(tmpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writer := NewSnapshotWriter(snapshotBase, reg)

	cs.SetCommittedTxID(96)
	err := writer.CreateSnapshot(cs, 96)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("open-temp error should be categorized as snapshot error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "open-temp" || completionErr.Path != tmpPath {
		t.Fatalf("completion error = %+v, want open-temp phase and temp path", completionErr)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after temp open failure")
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp path should be removed after open failure, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("final snapshot should not exist after open failure, stat err=%v", err)
	}
}

func TestCreateSnapshotOpenTempRejectsSymlinkAndDoesNotTruncateTarget(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	snapshotBase := filepath.Join(baseDir, "snapshots")
	snapshotDir := filepath.Join(snapshotBase, "98")
	tmpPath := filepath.Join(snapshotDir, snapshotTempFileName)
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(baseDir, "external-temp-target")
	before := []byte("external snapshot temp target")
	if err := os.WriteFile(targetPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, targetPath, tmpPath)
	writer := NewSnapshotWriter(snapshotBase, reg)

	cs.SetCommittedTxID(98)
	err := writer.CreateSnapshot(cs, 98)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("open-temp symlink error should be categorized as snapshot error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "open-temp" || completionErr.Path != tmpPath {
		t.Fatalf("completion error = %+v, want open-temp phase and temp path", completionErr)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after temp symlink open failure")
	}
	if _, err := os.Lstat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp symlink should be removed after open failure, lstat err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("final snapshot should not exist after temp symlink open failure, lstat err=%v", err)
	}
	after, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("temp symlink target changed: got %q want %q", after, before)
	}
}

func TestCreateSnapshotTempFileFaultsReturnSnapshotCompletionErrorAndCleanArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		phase     string
		configure func(*faultingSnapshotTempFile, error)
	}{
		{
			name:  "write",
			phase: "write-temp",
			configure: func(f *faultingSnapshotTempFile, err error) {
				f.writeErr = err
			},
		},
		{
			name:  "write-at",
			phase: "write-temp",
			configure: func(f *faultingSnapshotTempFile, err error) {
				f.writeAtErr = err
			},
		},
		{
			name:  "sync",
			phase: "sync-temp",
			configure: func(f *faultingSnapshotTempFile, err error) {
				f.syncErr = err
			},
		},
		{
			name:  "close",
			phase: "close-temp",
			configure: func(f *faultingSnapshotTempFile, err error) {
				f.closeErr = err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, reg := buildSnapshotCommittedState(t)
			baseDir := t.TempDir()
			snapshotBase := filepath.Join(baseDir, "snapshots")
			snapshotDir := filepath.Join(snapshotBase, "97")
			tmpPath := filepath.Join(snapshotDir, snapshotTempFileName)
			finalPath := filepath.Join(snapshotDir, snapshotFileName)
			faultErr := errors.New(tc.name + " failed")
			writer := NewSnapshotWriter(snapshotBase, reg).(*FileSnapshotWriter)
			writer.openTemp = func(path string) (snapshotTempFile, error) {
				if path != tmpPath {
					t.Fatalf("open temp path = %q, want %q", path, tmpPath)
				}
				file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
				if err != nil {
					return nil, err
				}
				wrapped := &faultingSnapshotTempFile{File: file}
				tc.configure(wrapped, faultErr)
				return wrapped, nil
			}

			cs.SetCommittedTxID(97)
			err := writer.CreateSnapshot(cs, 97)
			if !errors.Is(err, ErrSnapshot) {
				t.Fatalf("snapshot temp %s error = %v, want ErrSnapshot category", tc.name, err)
			}
			if !errors.Is(err, faultErr) {
				t.Fatalf("snapshot temp %s error = %v, want wrapped injected fault", tc.name, err)
			}
			var completionErr *SnapshotCompletionError
			if !errors.As(err, &completionErr) {
				t.Fatalf("expected SnapshotCompletionError, got %v", err)
			}
			if completionErr.Phase != tc.phase || completionErr.Path != tmpPath {
				t.Fatalf("completion error = %+v, want %s on temp path", completionErr, tc.phase)
			}
			if HasLockFile(snapshotDir) {
				t.Fatal("snapshot lock should be removed after temp-file fault")
			}
			if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
				t.Fatalf("snapshot temp file should be removed after %s fault, stat err=%v", tc.name, err)
			}
			if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
				t.Fatalf("final snapshot should not exist after %s fault, stat err=%v", tc.name, err)
			}
			if _, err := ReadSnapshot(snapshotDir); err == nil {
				t.Fatalf("snapshot should not be readable after %s fault", tc.name)
			}
		})
	}
}

func TestCreateSnapshotRenameFailureReturnsSnapshotErrorAndCleansArtifacts(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	fileWriter := writer.(*FileSnapshotWriter)
	renameErr := errors.New("rename failed")
	fileWriter.rename = func(oldPath, newPath string) error {
		if filepath.Base(oldPath) != snapshotTempFileName || filepath.Base(newPath) != snapshotFileName {
			t.Fatalf("rename paths = (%q, %q), want temp to final snapshot", oldPath, newPath)
		}
		return renameErr
	}

	cs.SetCommittedTxID(92)
	err := writer.CreateSnapshot(cs, 92)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("rename error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, renameErr) {
		t.Fatalf("rename error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "rename" || completionErr.Path == "" {
		t.Fatalf("completion error = %+v, want rename phase and path", completionErr)
	}

	snapshotDir := filepath.Join(baseDir, "snapshots", "92")
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after rename failure")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp file should be removed after rename failure, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); !os.IsNotExist(err) {
		t.Fatalf("final snapshot should not exist after rename failure, stat err=%v", err)
	}
}

func TestCreateSnapshotDirectorySyncFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	fileWriter := writer.(*FileSnapshotWriter)
	syncErr := errors.New("sync failed")
	snapshotDir := filepath.Join(baseDir, "snapshots", "93")
	fileWriter.syncDir = func(path string) error {
		if path == snapshotDir {
			return syncErr
		}
		return nil
	}

	cs.SetCommittedTxID(93)
	err := writer.CreateSnapshot(cs, 93)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("sync error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("sync error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-snapshot" || completionErr.Path != snapshotDir {
		t.Fatalf("completion error = %+v, want sync-snapshot phase and snapshot dir", completionErr)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); err != nil {
		t.Fatalf("final snapshot should exist after rename before sync failure: %v", err)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed during sync-failure cleanup")
	}
}

func TestCreateSnapshotRemoveLockFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	fileWriter := writer.(*FileSnapshotWriter)
	removeErr := errors.New("remove lock failed")
	fileWriter.removeLock = func(string) error {
		return removeErr
	}

	cs.SetCommittedTxID(94)
	err := writer.CreateSnapshot(cs, 94)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("remove-lock error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, removeErr) {
		t.Fatalf("remove-lock error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "remove-lock" || filepath.Base(completionErr.Path) != ".lock" {
		t.Fatalf("completion error = %+v, want remove-lock phase and lock path", completionErr)
	}
	snapshotDir := filepath.Join(baseDir, "snapshots", "94")
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotFileName)); err != nil {
		t.Fatalf("final snapshot should exist before lock removal failure: %v", err)
	}
	if !HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should remain when lock removal fails")
	}
}

func TestCreateSnapshotSyncUnlockFailureReturnsSnapshotCompletionError(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	baseDir := t.TempDir()
	snapshotBase := filepath.Join(baseDir, "snapshots")
	snapshotDir := filepath.Join(snapshotBase, "105")
	writer := NewSnapshotWriter(snapshotBase, reg).(*FileSnapshotWriter)
	syncErr := errors.New("sync unlock failed")
	syncSnapshotCalls := 0
	writer.syncDir = func(path string) error {
		if path != snapshotBase && path != snapshotDir {
			t.Fatalf("syncDir path = %q, want snapshot base or tx directory", path)
		}
		if path == snapshotDir {
			syncSnapshotCalls++
			if syncSnapshotCalls == 2 {
				return syncErr
			}
		}
		return nil
	}

	cs.SetCommittedTxID(105)
	err := writer.CreateSnapshot(cs, 105)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("sync-unlock error should be categorized as snapshot error, got %v", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("sync-unlock error should wrap original error, got %v", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-unlock" || completionErr.Path != snapshotDir {
		t.Fatalf("completion error = %+v, want sync-unlock phase and snapshot dir", completionErr)
	}
	if syncSnapshotCalls != 2 {
		t.Fatalf("snapshot directory sync calls = %d, want 2", syncSnapshotCalls)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be absent after remove-lock before sync-unlock failure")
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("snapshot should be readable after unlock sync failure: %v", err)
	}
}

func TestCreateSnapshotRejectsTxIDThatDoesNotMatchCommittedHorizon(t *testing.T) {
	cs, reg := buildSnapshotCommittedState(t)
	cs.SetCommittedTxID(2)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)

	err := writer.CreateSnapshot(cs, 3)
	var mismatch *SnapshotHorizonMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected SnapshotHorizonMismatchError, got %v", err)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("expected snapshot category, got %v", err)
	}
	if mismatch.SnapshotTxID != 3 || mismatch.CommittedTxID != 2 {
		t.Fatalf("mismatch = %+v, want SnapshotTxID=3 CommittedTxID=2", mismatch)
	}
}

func assertSnapshotPayloadArtifactsMissing(t *testing.T, snapshotDir string) {
	t.Helper()
	for _, name := range []string{snapshotTempFileName, snapshotFileName} {
		if _, err := os.Stat(filepath.Join(snapshotDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be absent, stat err=%v", name, err)
		}
	}
}

type faultingSnapshotTempFile struct {
	*os.File
	shortWrite   bool
	shortWriteAt bool
	writeErr     error
	writeAtErr   error
	syncErr      error
	closeErr     error
}

func (f *faultingSnapshotTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.shortWrite && len(p) > 0 {
		return len(p) - 1, nil
	}
	return f.File.Write(p)
}

func (f *faultingSnapshotTempFile) WriteAt(p []byte, off int64) (int, error) {
	if f.writeAtErr != nil {
		return 0, f.writeAtErr
	}
	if f.shortWriteAt && len(p) > 0 {
		return len(p) - 1, nil
	}
	return f.File.WriteAt(p, off)
}

func (f *faultingSnapshotTempFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}

func (f *faultingSnapshotTempFile) Close() error {
	err := f.File.Close()
	if f.closeErr != nil {
		return f.closeErr
	}
	return err
}

func buildLargeSnapshotCommittedState(t testing.TB, rowCount int) (*store.CommittedState, schema.SchemaRegistry) {
	t.Helper()
	_, reg := testSchema()
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	players, ok := cs.Table(0)
	if !ok {
		t.Fatal("missing players table")
	}
	for i := 1; i <= rowCount; i++ {
		row := types.ProductValue{
			types.NewUint64(uint64(i)),
			types.NewString("player-" + strconv.Itoa(i)),
		}
		if err := players.InsertRow(players.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}
	return cs, reg
}

func TestSnapshotLargeRoundTripAndDeterministicBytes(t *testing.T) {
	cs, reg := buildLargeSnapshotCommittedState(t, 2048)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 101)
	createSnapshotAt(t, writer, cs, 102)

	data101, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "101"))
	if err != nil {
		t.Fatal(err)
	}
	data102, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "102"))
	if err != nil {
		t.Fatal(err)
	}

	playersRows101 := -1
	playersRows102 := -1
	for _, table := range data101.Tables {
		if table.TableID == 0 {
			playersRows101 = len(table.Rows)
			if got := table.Rows[len(table.Rows)-1][1].AsString(); got != "player-2048" {
				t.Fatalf("last player in snapshot 101 = %q, want player-2048", got)
			}
		}
	}
	for _, table := range data102.Tables {
		if table.TableID == 0 {
			playersRows102 = len(table.Rows)
		}
	}
	if playersRows101 != 2048 || playersRows102 != 2048 {
		t.Fatalf("player row counts = (%d, %d), want (2048, 2048)", playersRows101, playersRows102)
	}

	bytes101, err := os.ReadFile(filepath.Join(baseDir, "snapshots", "101", "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	bytes102, err := os.ReadFile(filepath.Join(baseDir, "snapshots", "102", "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes101[:8], bytes102[:8]) {
		t.Fatal("snapshot magic/version prefix differs for identical state")
	}
	if !bytes.Equal(bytes101[16:20], bytes102[16:20]) {
		t.Fatal("schema version bytes differ for identical state")
	}
	if !bytes.Equal(bytes101[20:52], bytes102[20:52]) {
		t.Fatal("hash bytes differ for identical state")
	}
	if !bytes.Equal(bytes101[52:], bytes102[52:]) {
		t.Fatal("snapshot payload differs for identical state")
	}
}

func TestReadSnapshotLargeRoundTrip(t *testing.T) {
	cs, reg := buildLargeSnapshotCommittedState(t, 4096)
	baseDir := t.TempDir()
	writer := NewSnapshotWriter(filepath.Join(baseDir, "snapshots"), reg)
	createSnapshotAt(t, writer, cs, 103)

	data, err := ReadSnapshot(filepath.Join(baseDir, "snapshots", "103"))
	if err != nil {
		t.Fatal(err)
	}
	if data.TxID != 103 {
		t.Fatalf("snapshot txID = %d, want 103", data.TxID)
	}
	playersRows := -1
	for _, table := range data.Tables {
		if table.TableID == 0 {
			playersRows = len(table.Rows)
			if got := table.Rows[0][1].AsString(); got != "player-1" {
				t.Fatalf("first player = %q, want player-1", got)
			}
			if got := table.Rows[len(table.Rows)-1][1].AsString(); got != "player-4096" {
				t.Fatalf("last player = %q, want player-4096", got)
			}
		}
	}
	if playersRows != 4096 {
		t.Fatalf("players rows = %d, want 4096", playersRows)
	}
}

func BenchmarkCreateSnapshotLarge(b *testing.B) {
	cs, reg := buildLargeSnapshotCommittedState(b, 4096)
	for i := 0; i < b.N; i++ {
		root := b.TempDir()
		writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
		createSnapshotAt(b, writer, cs, types.TxID(i+1))
	}
}

func txIDString(txID uint64) string {
	return fmt.Sprintf("%d", txID)
}

func writeSnapshotBytes(t testing.TB, dir string, txID types.TxID, schemaVersion uint32, body []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var file bytes.Buffer
	file.Write(SnapshotMagic[:])
	file.Write([]byte{SnapshotVersion, 0, 0, 0})
	writeUint64(t, &file, uint64(txID))
	writeUint32(t, &file, schemaVersion)
	hash := ComputeSnapshotHash(body)
	file.Write(hash[:])
	file.Write(body)
	if err := os.WriteFile(filepath.Join(dir, snapshotFileName), file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeUint32(t testing.TB, dst *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(dst, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}

func writeUint64(t testing.TB, dst *bytes.Buffer, value uint64) {
	t.Helper()
	if err := binary.Write(dst, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}
