package commitlog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"lukechampine.com/blake3"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

var SnapshotMagic = [4]byte{'S', 'H', 'S', 'N'}

const (
	SnapshotVersion      uint8 = 1
	SnapshotHeaderSize         = 52
	snapshotFileName           = "snapshot"
	snapshotTempFileName       = "snapshot.tmp"
)

type ErrSnapshotHashMismatch = SnapshotHashMismatchError

func ComputeSnapshotHash(data []byte) [32]byte {
	return blake3.Sum256(data)
}

func HasLockFile(snapshotDir string) bool {
	_, err := os.Stat(filepath.Join(snapshotDir, ".lock"))
	return err == nil
}

func HasSnapshotTempFile(snapshotDir string) bool {
	_, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName))
	return err == nil
}

func CreateLockFile(snapshotDir string) error {
	f, err := os.OpenFile(filepath.Join(snapshotDir, ".lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func RemoveLockFile(snapshotDir string) error {
	err := os.Remove(filepath.Join(snapshotDir, ".lock"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func EncodeSchemaSnapshot(w io.Writer, reg schema.SchemaRegistry) error {
	ids := reg.Tables()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err := binary.Write(w, binary.LittleEndian, reg.Version()); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		ts, ok := reg.Table(id)
		if !ok {
			return fmt.Errorf("missing schema table %d", id)
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(ts.ID)); err != nil {
			return err
		}
		if err := writeString(w, ts.Name); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(ts.Columns))); err != nil {
			return err
		}
		for _, col := range ts.Columns {
			if err := binary.Write(w, binary.LittleEndian, uint32(col.Index)); err != nil {
				return err
			}
			if err := writeString(w, col.Name); err != nil {
				return err
			}
			if _, err := w.Write([]byte{byte(col.Type), boolByte(col.Nullable), boolByte(col.AutoIncrement)}); err != nil {
				return err
			}
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(ts.Indexes))); err != nil {
			return err
		}
		for _, idx := range ts.Indexes {
			if err := writeString(w, idx.Name); err != nil {
				return err
			}
			if _, err := w.Write([]byte{boolByte(idx.Unique), boolByte(idx.Primary)}); err != nil {
				return err
			}
			if err := binary.Write(w, binary.LittleEndian, uint32(len(idx.Columns))); err != nil {
				return err
			}
			for _, colIdx := range idx.Columns {
				if err := binary.Write(w, binary.LittleEndian, uint32(colIdx)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func DecodeSchemaSnapshot(r io.Reader) ([]schema.TableSchema, uint32, error) {
	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, 0, err
	}
	var tableCount uint32
	if err := binary.Read(r, binary.LittleEndian, &tableCount); err != nil {
		return nil, 0, err
	}
	tables := make([]schema.TableSchema, 0, tableCount)
	for range tableCount {
		var tableID uint32
		if err := binary.Read(r, binary.LittleEndian, &tableID); err != nil {
			return nil, 0, err
		}
		name, err := readString(r)
		if err != nil {
			return nil, 0, err
		}
		var colCount uint32
		if err := binary.Read(r, binary.LittleEndian, &colCount); err != nil {
			return nil, 0, err
		}
		cols := make([]schema.ColumnSchema, 0, colCount)
		for range colCount {
			var colIdx uint32
			if err := binary.Read(r, binary.LittleEndian, &colIdx); err != nil {
				return nil, 0, err
			}
			if colIdx > math.MaxInt32 {
				return nil, 0, fmt.Errorf("column index overflow: %d", colIdx)
			}
			colName, err := readString(r)
			if err != nil {
				return nil, 0, err
			}
			flags := make([]byte, 3)
			if _, err := io.ReadFull(r, flags); err != nil {
				return nil, 0, err
			}
			cols = append(cols, schema.ColumnSchema{Index: int(colIdx), Name: colName, Type: schema.ValueKind(flags[0]), Nullable: flags[1] == 1, AutoIncrement: flags[2] == 1})
		}
		var idxCount uint32
		if err := binary.Read(r, binary.LittleEndian, &idxCount); err != nil {
			return nil, 0, err
		}
		indexes := make([]schema.IndexSchema, 0, idxCount)
		for idxID := uint32(0); idxID < idxCount; idxID++ {
			idxName, err := readString(r)
			if err != nil {
				return nil, 0, err
			}
			flags := make([]byte, 2)
			if _, err := io.ReadFull(r, flags); err != nil {
				return nil, 0, err
			}
			var colsCount uint32
			if err := binary.Read(r, binary.LittleEndian, &colsCount); err != nil {
				return nil, 0, err
			}
			idxCols := make([]int, colsCount)
			for i := range idxCols {
				var colIdx uint32
				if err := binary.Read(r, binary.LittleEndian, &colIdx); err != nil {
					return nil, 0, err
				}
				if colIdx > math.MaxInt32 {
					return nil, 0, fmt.Errorf("column index overflow: %d", colIdx)
				}
				idxCols[i] = int(colIdx)
			}
			indexes = append(indexes, schema.IndexSchema{ID: schema.IndexID(idxID), Name: idxName, Columns: idxCols, Unique: flags[0] == 1, Primary: flags[1] == 1})
		}
		tables = append(tables, schema.TableSchema{ID: schema.TableID(tableID), Name: name, Columns: cols, Indexes: indexes})
	}
	return tables, version, nil
}

type SnapshotWriter interface {
	CreateSnapshot(committed *store.CommittedState, txID types.TxID) error
}

type FileSnapshotWriter struct {
	baseDir       string
	reg           schema.SchemaRegistry
	mu            sync.Mutex
	inProgress    bool
	beforeWrite   chan struct{}
	continueWrite chan struct{}
	rename        func(string, string) error
	syncDir       func(string) error
	removeLock    func(string) error
}

func NewSnapshotWriter(baseDir string, reg schema.SchemaRegistry) SnapshotWriter {
	return &FileSnapshotWriter{baseDir: baseDir, reg: reg, rename: os.Rename, syncDir: syncDir, removeLock: RemoveLockFile}
}

func (w *FileSnapshotWriter) CreateSnapshot(committed *store.CommittedState, txID types.TxID) error {
	w.mu.Lock()
	if w.inProgress {
		w.mu.Unlock()
		return ErrSnapshotInProgress
	}
	w.inProgress = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.inProgress = false
		w.mu.Unlock()
	}()

	if err := validateSnapshotHorizon(committed, txID); err != nil {
		return err
	}

	snapshotDir := filepath.Join(w.baseDir, strconv.FormatUint(uint64(txID), 10))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return err
	}
	if err := w.syncDir(w.baseDir); err != nil {
		return &SnapshotCompletionError{Phase: "sync-parent", Path: w.baseDir, Err: err}
	}
	if err := CreateLockFile(snapshotDir); err != nil {
		return &SnapshotCompletionError{Phase: "create-lock", Path: filepath.Join(snapshotDir, ".lock"), Err: err}
	}
	defer func() {
		_ = w.removeLock(snapshotDir)
	}()

	tmpPath := filepath.Join(snapshotDir, snapshotTempFileName)
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(tmpPath)
		}
	}()
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if w.beforeWrite != nil {
		w.beforeWrite <- struct{}{}
	}
	if w.continueWrite != nil {
		<-w.continueWrite
	}

	if err := w.writeSnapshotFile(f, committed, txID); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return &SnapshotCompletionError{Phase: "sync-temp", Path: tmpPath, Err: err}
	}
	if err := f.Close(); err != nil {
		return &SnapshotCompletionError{Phase: "close-temp", Path: tmpPath, Err: err}
	}
	finalPath := filepath.Join(snapshotDir, snapshotFileName)
	if err := w.rename(tmpPath, finalPath); err != nil {
		return &SnapshotCompletionError{Phase: "rename", Path: finalPath, Err: err}
	}
	completed = true
	if err := w.syncDir(snapshotDir); err != nil {
		return &SnapshotCompletionError{Phase: "sync-snapshot", Path: snapshotDir, Err: err}
	}
	lockPath := filepath.Join(snapshotDir, ".lock")
	if err := w.removeLock(snapshotDir); err != nil {
		return &SnapshotCompletionError{Phase: "remove-lock", Path: lockPath, Err: err}
	}
	if err := w.syncDir(snapshotDir); err != nil {
		return &SnapshotCompletionError{Phase: "sync-unlock", Path: snapshotDir, Err: err}
	}
	return nil
}

func validateSnapshotHorizon(committed *store.CommittedState, txID types.TxID) error {
	committedTxID := committed.CommittedTxID()
	if committedTxID != txID {
		return &SnapshotHorizonMismatchError{
			SnapshotTxID:  txID,
			CommittedTxID: committedTxID,
		}
	}
	return nil
}

func (w *FileSnapshotWriter) writeSnapshotFile(f *os.File, committed *store.CommittedState, txID types.TxID) error {
	if _, err := f.Write(SnapshotMagic[:]); err != nil {
		return err
	}
	if _, err := f.Write([]byte{SnapshotVersion, 0, 0, 0}); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint64(txID)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, w.reg.Version()); err != nil {
		return err
	}
	if _, err := f.Write(make([]byte, 32)); err != nil {
		return err
	}

	hasher := blake3.New(32, nil)
	bodyWriter := io.MultiWriter(f, hasher)
	if err := w.writeSnapshotBody(bodyWriter, committed, txID); err != nil {
		return err
	}
	var hash [32]byte
	copy(hash[:], hasher.Sum(nil))
	if _, err := f.WriteAt(hash[:], 20); err != nil {
		return err
	}
	return nil
}

func (w *FileSnapshotWriter) writeSnapshotBody(dst io.Writer, committed *store.CommittedState, txID types.TxID) error {
	committed.RLock()
	defer committed.RUnlock()

	if committedTxID := committed.CommittedTxIDLocked(); committedTxID != txID {
		return &SnapshotHorizonMismatchError{
			SnapshotTxID:  txID,
			CommittedTxID: committedTxID,
		}
	}

	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, w.reg); err != nil {
		return err
	}
	if err := binary.Write(dst, binary.LittleEndian, uint32(schemaBuf.Len())); err != nil {
		return err
	}
	if _, err := dst.Write(schemaBuf.Bytes()); err != nil {
		return err
	}
	ids := committed.TableIDs()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	sequenceTableIDs := make([]schema.TableID, 0, len(ids))
	for _, tableID := range ids {
		table, _ := committed.Table(tableID)
		if _, ok := table.SequenceValue(); ok {
			sequenceTableIDs = append(sequenceTableIDs, tableID)
		}
	}
	if err := binary.Write(dst, binary.LittleEndian, uint32(len(sequenceTableIDs))); err != nil {
		return err
	}
	for _, tableID := range sequenceTableIDs {
		table, _ := committed.Table(tableID)
		seq, _ := table.SequenceValue()
		if err := binary.Write(dst, binary.LittleEndian, uint32(tableID)); err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, seq); err != nil {
			return err
		}
	}
	if err := binary.Write(dst, binary.LittleEndian, uint32(len(ids))); err != nil {
		return err
	}
	for _, tableID := range ids {
		table, _ := committed.Table(tableID)
		if err := binary.Write(dst, binary.LittleEndian, uint32(tableID)); err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, uint64(table.NextID())); err != nil {
			return err
		}
	}
	if err := binary.Write(dst, binary.LittleEndian, uint32(len(ids))); err != nil {
		return err
	}
	var rowBuf bytes.Buffer
	for _, tableID := range ids {
		table, _ := committed.Table(tableID)
		rows, err := deterministicRows(table)
		if err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, uint32(tableID)); err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, uint32(len(rows))); err != nil {
			return err
		}
		for _, row := range rows {
			rowBuf.Reset()
			if err := bsatn.EncodeProductValue(&rowBuf, row); err != nil {
				return err
			}
			if err := binary.Write(dst, binary.LittleEndian, uint32(rowBuf.Len())); err != nil {
				return err
			}
			if _, err := dst.Write(rowBuf.Bytes()); err != nil {
				return err
			}
		}
	}
	return nil
}

type SnapshotData struct {
	TxID                  types.TxID
	SchemaVersion         uint32
	SchemaSnapshotVersion uint32
	Tables                []SnapshotTableData
	Sequences             map[schema.TableID]uint64
	NextIDs               map[schema.TableID]uint64
	Schema                []schema.TableSchema
}

type SnapshotTableData struct {
	TableID schema.TableID
	Rows    []types.ProductValue
}

func ReadSnapshot(dir string) (*SnapshotData, error) {
	f, err := os.Open(filepath.Join(dir, snapshotFileName))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	txID, schemaVersion, expected, err := readSnapshotHeader(f)
	if err != nil {
		return nil, err
	}
	if err := verifySnapshotPayloadHash(f, expected); err != nil {
		return nil, err
	}

	tables, schemaSnapshotVersion, schemaByID, err := readSnapshotSchema(f)
	if err != nil {
		return nil, err
	}
	sequences, err := readSnapshotSequences(f)
	if err != nil {
		return nil, err
	}
	nextIDs, err := readSnapshotNextIDs(f)
	if err != nil {
		return nil, err
	}
	snapshotTables, err := readSnapshotTables(f, schemaByID)
	if err != nil {
		return nil, err
	}

	return &SnapshotData{
		TxID:                  types.TxID(txID),
		SchemaVersion:         schemaVersion,
		SchemaSnapshotVersion: schemaSnapshotVersion,
		Tables:                snapshotTables,
		Sequences:             sequences,
		NextIDs:               nextIDs,
		Schema:                tables,
	}, nil
}

func readSnapshotHeader(f *os.File) (uint64, uint32, [32]byte, error) {
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return 0, 0, [32]byte{}, err
	}
	if magic != SnapshotMagic {
		return 0, 0, [32]byte{}, ErrBadMagic
	}

	var versionAndPad [4]byte
	if _, err := io.ReadFull(f, versionAndPad[:]); err != nil {
		return 0, 0, [32]byte{}, err
	}
	if versionAndPad[0] != SnapshotVersion {
		return 0, 0, [32]byte{}, &BadVersionError{Got: versionAndPad[0]}
	}

	var txID uint64
	var schemaVersion uint32
	if err := binary.Read(f, binary.LittleEndian, &txID); err != nil {
		return 0, 0, [32]byte{}, err
	}
	if err := binary.Read(f, binary.LittleEndian, &schemaVersion); err != nil {
		return 0, 0, [32]byte{}, err
	}

	var expected [32]byte
	if _, err := io.ReadFull(f, expected[:]); err != nil {
		return 0, 0, [32]byte{}, err
	}
	return txID, schemaVersion, expected, nil
}

func verifySnapshotPayloadHash(f *os.File, expected [32]byte) error {
	hasher := blake3.New(32, nil)
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}
	var got [32]byte
	copy(got[:], hasher.Sum(nil))
	if got != expected {
		return &SnapshotHashMismatchError{Expected: expected, Got: got}
	}
	_, err := f.Seek(SnapshotHeaderSize, io.SeekStart)
	return err
}

func readSnapshotSchema(payload io.Reader) ([]schema.TableSchema, uint32, map[schema.TableID]*schema.TableSchema, error) {
	var schemaLen uint32
	if err := binary.Read(payload, binary.LittleEndian, &schemaLen); err != nil {
		return nil, 0, nil, err
	}
	schemaBytes := make([]byte, schemaLen)
	if _, err := io.ReadFull(payload, schemaBytes); err != nil {
		return nil, 0, nil, err
	}
	tables, schemaSnapshotVersion, err := DecodeSchemaSnapshot(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, 0, nil, err
	}
	schemaByID := make(map[schema.TableID]*schema.TableSchema, len(tables))
	for i := range tables {
		ts := &tables[i]
		schemaByID[ts.ID] = ts
	}
	return tables, schemaSnapshotVersion, schemaByID, nil
}

func readSnapshotSequences(payload io.Reader) (map[schema.TableID]uint64, error) {
	sequences := map[schema.TableID]uint64{}
	var seqCount uint32
	if err := binary.Read(payload, binary.LittleEndian, &seqCount); err != nil {
		return nil, err
	}
	for range seqCount {
		var tableID uint32
		var next uint64
		if err := binary.Read(payload, binary.LittleEndian, &tableID); err != nil {
			return nil, err
		}
		if err := binary.Read(payload, binary.LittleEndian, &next); err != nil {
			return nil, err
		}
		sequences[schema.TableID(tableID)] = next
	}
	return sequences, nil
}

func readSnapshotNextIDs(payload io.Reader) (map[schema.TableID]uint64, error) {
	nextIDs := map[schema.TableID]uint64{}
	var nextIDCount uint32
	if err := binary.Read(payload, binary.LittleEndian, &nextIDCount); err != nil {
		return nil, err
	}
	for range nextIDCount {
		var tableID uint32
		var next uint64
		if err := binary.Read(payload, binary.LittleEndian, &tableID); err != nil {
			return nil, err
		}
		if err := binary.Read(payload, binary.LittleEndian, &next); err != nil {
			return nil, err
		}
		nextIDs[schema.TableID(tableID)] = next
	}
	return nextIDs, nil
}

func readSnapshotTables(payload io.Reader, schemaByID map[schema.TableID]*schema.TableSchema) ([]SnapshotTableData, error) {
	var tableCount uint32
	if err := binary.Read(payload, binary.LittleEndian, &tableCount); err != nil {
		return nil, err
	}
	tables := make([]SnapshotTableData, 0, tableCount)
	var rowBuf []byte
	for range tableCount {
		var tableID uint32
		var rowCount uint32
		if err := binary.Read(payload, binary.LittleEndian, &tableID); err != nil {
			return nil, err
		}
		if err := binary.Read(payload, binary.LittleEndian, &rowCount); err != nil {
			return nil, err
		}
		snapshotTable := SnapshotTableData{TableID: schema.TableID(tableID)}
		ts, ok := schemaByID[schema.TableID(tableID)]
		if !ok {
			return nil, fmt.Errorf("snapshot references unknown table %d", tableID)
		}
		for range rowCount {
			var rowLen uint32
			if err := binary.Read(payload, binary.LittleEndian, &rowLen); err != nil {
				return nil, err
			}
			if cap(rowBuf) < int(rowLen) {
				rowBuf = make([]byte, rowLen)
			}
			rowBytes := rowBuf[:rowLen]
			if _, err := io.ReadFull(payload, rowBytes); err != nil {
				return nil, err
			}
			row, err := bsatn.DecodeProductValueFromBytes(rowBytes, ts)
			if err != nil {
				return nil, err
			}
			snapshotTable.Rows = append(snapshotTable.Rows, row)
		}
		tables = append(tables, snapshotTable)
	}
	return tables, nil
}

func ListSnapshots(baseDir string) ([]types.TxID, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []types.TxID
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		txID, err := strconv.ParseUint(entry.Name(), 10, 64)
		if err != nil {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		if HasLockFile(dir) || HasSnapshotTempFile(dir) {
			continue
		}
		ids = append(ids, types.TxID(txID))
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	return ids, nil
}

func deterministicRows(table *store.Table) ([]types.ProductValue, error) {
	if pk := table.PrimaryIndex(); pk != nil {
		var rows []types.ProductValue
		for rid := range pk.BTree().Scan() {
			row, ok := table.GetRow(rid)
			if ok {
				rows = append(rows, row)
			}
		}
		return rows, nil
	}
	type pair struct {
		id  types.RowID
		row types.ProductValue
	}
	var pairs []pair
	for id, row := range table.Scan() {
		pairs = append(pairs, pair{id: id, row: row})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].id < pairs[j].id })
	rows := make([]types.ProductValue, len(pairs))
	for i, p := range pairs {
		rows[i] = p.row
	}
	return rows, nil
}

func writeString(w io.Writer, s string) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

func readString(r io.Reader) (string, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
