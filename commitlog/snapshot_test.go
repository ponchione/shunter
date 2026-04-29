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
	cs := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, _ := reg.Table(tid)
		cs.RegisterTable(tid, store.NewTable(ts))
	}
	players, _ := cs.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("bob")}); err != nil {
		t.Fatal(err)
	}
	return cs, reg
}

func createSnapshotAt(t testing.TB, writer SnapshotWriter, cs *store.CommittedState, txID types.TxID) {
	t.Helper()
	cs.SetCommittedTxID(txID)
	if err := writer.CreateSnapshot(cs, txID); err != nil {
		t.Fatal(err)
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
	data := encodeSchemaSnapshotWithFlags(t, [3]byte{byte(schema.KindArrayString) + 1, 0, 0}, [2]byte{1, 1})
	_, _, err := DecodeSchemaSnapshot(bytes.NewReader(data))
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), `invalid schema snapshot column "id" type`) {
		t.Fatalf("DecodeSchemaSnapshot error = %v, want invalid column type detail", err)
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
	writeUint32(t, &schemaBuf, 0) // index count
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
