package commitlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"lukechampine.com/blake3"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

var SnapshotMagic = [4]byte{'S', 'H', 'S', 'N'}

const (
	SnapshotVersion        uint8 = 1
	SnapshotHeaderSize           = 52
	snapshotFileName             = "snapshot"
	snapshotTempFileName         = "snapshot.tmp"
	maxSnapshotStringBytes       = 1 << 20
)

type ErrSnapshotHashMismatch = SnapshotHashMismatchError

func ComputeSnapshotHash(data []byte) [32]byte {
	return blake3.Sum256(data)
}

func HasLockFile(snapshotDir string) bool {
	return snapshotMarkerExists(filepath.Join(snapshotDir, ".lock"))
}

func HasSnapshotTempFile(snapshotDir string) bool {
	return snapshotMarkerExists(filepath.Join(snapshotDir, snapshotTempFileName))
}

func snapshotMarkerExists(path string) bool {
	_, err := os.Lstat(path)
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
	if err := writeUint32Full(w, reg.Version()); err != nil {
		return err
	}
	if err := writeUint32Full(w, uint32(len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		ts, ok := reg.Table(id)
		if !ok {
			return fmt.Errorf("missing schema table %d", id)
		}
		if err := writeUint32Full(w, uint32(ts.ID)); err != nil {
			return err
		}
		if err := writeString(w, ts.Name); err != nil {
			return err
		}
		if err := writeUint32Full(w, uint32(len(ts.Columns))); err != nil {
			return err
		}
		for _, col := range ts.Columns {
			if err := writeUint32Full(w, uint32(col.Index)); err != nil {
				return err
			}
			if err := writeString(w, col.Name); err != nil {
				return err
			}
			if err := writeFull(w, []byte{byte(col.Type), boolByte(col.Nullable), boolByte(col.AutoIncrement)}); err != nil {
				return err
			}
		}
		if err := writeUint32Full(w, uint32(len(ts.Indexes))); err != nil {
			return err
		}
		for _, idx := range ts.Indexes {
			if err := writeString(w, idx.Name); err != nil {
				return err
			}
			if err := writeFull(w, []byte{boolByte(idx.Unique), boolByte(idx.Primary)}); err != nil {
				return err
			}
			if err := writeUint32Full(w, uint32(len(idx.Columns))); err != nil {
				return err
			}
			for _, colIdx := range idx.Columns {
				if err := writeUint32Full(w, uint32(colIdx)); err != nil {
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
	var tables []schema.TableSchema
	seenTables := map[schema.TableID]struct{}{}
	seenTableNames := map[string]struct{}{}
	for range tableCount {
		var tableID uint32
		if err := binary.Read(r, binary.LittleEndian, &tableID); err != nil {
			return nil, 0, err
		}
		schemaTableID := schema.TableID(tableID)
		if _, exists := seenTables[schemaTableID]; exists {
			return nil, 0, fmt.Errorf("%w: duplicate schema snapshot table ID %d", ErrSnapshot, tableID)
		}
		seenTables[schemaTableID] = struct{}{}
		name, err := readString(r)
		if err != nil {
			return nil, 0, err
		}
		if name == "" {
			return nil, 0, fmt.Errorf("%w: schema snapshot table %d has empty name", ErrSnapshot, tableID)
		}
		if _, exists := seenTableNames[name]; exists {
			return nil, 0, fmt.Errorf("%w: duplicate schema snapshot table name %q", ErrSnapshot, name)
		}
		seenTableNames[name] = struct{}{}
		var colCount uint32
		if err := binary.Read(r, binary.LittleEndian, &colCount); err != nil {
			return nil, 0, err
		}
		if colCount == 0 {
			return nil, 0, fmt.Errorf("%w: schema snapshot table %d has no columns", ErrSnapshot, tableID)
		}
		var cols []schema.ColumnSchema
		seenColumns := map[int]struct{}{}
		seenColumnNames := map[string]struct{}{}
		for range colCount {
			var colIdx uint32
			if err := binary.Read(r, binary.LittleEndian, &colIdx); err != nil {
				return nil, 0, err
			}
			if colIdx > math.MaxInt32 {
				return nil, 0, fmt.Errorf("%w: schema snapshot column index overflow: %d", ErrSnapshot, colIdx)
			}
			index := int(colIdx)
			if _, exists := seenColumns[index]; exists {
				return nil, 0, fmt.Errorf("%w: duplicate schema snapshot column index %d in table %d", ErrSnapshot, colIdx, tableID)
			}
			seenColumns[index] = struct{}{}
			colName, err := readString(r)
			if err != nil {
				return nil, 0, err
			}
			if colName == "" {
				return nil, 0, fmt.Errorf("%w: schema snapshot column %d in table %d has empty name", ErrSnapshot, colIdx, tableID)
			}
			if _, exists := seenColumnNames[colName]; exists {
				return nil, 0, fmt.Errorf("%w: duplicate schema snapshot column name %q in table %d", ErrSnapshot, colName, tableID)
			}
			seenColumnNames[colName] = struct{}{}
			flags := make([]byte, 3)
			if _, err := io.ReadFull(r, flags); err != nil {
				return nil, 0, err
			}
			kind := schema.ValueKind(flags[0])
			if !validSchemaSnapshotValueKind(kind) {
				return nil, 0, fmt.Errorf("%w: invalid schema snapshot column %q type %d in table %d", ErrSnapshot, colName, flags[0], tableID)
			}
			nullable, err := decodeSchemaSnapshotBool(flags[1], "column nullable")
			if err != nil {
				return nil, 0, err
			}
			autoIncrement, err := decodeSchemaSnapshotBool(flags[2], "column auto_increment")
			if err != nil {
				return nil, 0, err
			}
			if autoIncrement {
				if _, _, ok := schema.AutoIncrementBounds(kind); !ok {
					return nil, 0, fmt.Errorf("%w: schema snapshot column %q in table %d has invalid auto_increment type %s", ErrSnapshot, colName, tableID, kind)
				}
			}
			cols = append(cols, schema.ColumnSchema{Index: index, Name: colName, Type: kind, Nullable: nullable, AutoIncrement: autoIncrement})
		}
		var idxCount uint32
		if err := binary.Read(r, binary.LittleEndian, &idxCount); err != nil {
			return nil, 0, err
		}
		var indexes []schema.IndexSchema
		seenIndexNames := map[string]struct{}{}
		primaryIndexes := 0
		for idxID := uint32(0); idxID < idxCount; idxID++ {
			idxName, err := readString(r)
			if err != nil {
				return nil, 0, err
			}
			if idxName == "" {
				return nil, 0, fmt.Errorf("%w: schema snapshot index %d in table %d has empty name", ErrSnapshot, idxID, tableID)
			}
			if _, exists := seenIndexNames[idxName]; exists {
				return nil, 0, fmt.Errorf("%w: duplicate schema snapshot index name %q in table %d", ErrSnapshot, idxName, tableID)
			}
			seenIndexNames[idxName] = struct{}{}
			flags := make([]byte, 2)
			if _, err := io.ReadFull(r, flags); err != nil {
				return nil, 0, err
			}
			unique, err := decodeSchemaSnapshotBool(flags[0], "index unique")
			if err != nil {
				return nil, 0, err
			}
			primary, err := decodeSchemaSnapshotBool(flags[1], "index primary")
			if err != nil {
				return nil, 0, err
			}
			if primary && !unique {
				return nil, 0, fmt.Errorf("%w: schema snapshot primary index %q in table %d is not unique", ErrSnapshot, idxName, tableID)
			}
			if primary {
				primaryIndexes++
				if primaryIndexes > 1 {
					return nil, 0, fmt.Errorf("%w: schema snapshot table %d has multiple primary indexes", ErrSnapshot, tableID)
				}
			}
			var colsCount uint32
			if err := binary.Read(r, binary.LittleEndian, &colsCount); err != nil {
				return nil, 0, err
			}
			if colsCount == 0 {
				return nil, 0, fmt.Errorf("%w: schema snapshot index %q in table %d has no columns", ErrSnapshot, idxName, tableID)
			}
			var idxCols []int
			for range colsCount {
				var colIdx uint32
				if err := binary.Read(r, binary.LittleEndian, &colIdx); err != nil {
					return nil, 0, err
				}
				if colIdx > math.MaxInt32 {
					return nil, 0, fmt.Errorf("%w: schema snapshot column index overflow: %d", ErrSnapshot, colIdx)
				}
				if _, ok := seenColumns[int(colIdx)]; !ok {
					return nil, 0, fmt.Errorf("%w: schema snapshot index %q references unknown column index %d in table %d", ErrSnapshot, idxName, colIdx, tableID)
				}
				idxCols = append(idxCols, int(colIdx))
			}
			indexes = append(indexes, schema.IndexSchema{ID: schema.IndexID(idxID), Name: idxName, Columns: idxCols, Unique: unique, Primary: primary})
		}
		for _, col := range cols {
			if col.AutoIncrement && !snapshotSchemaHasUniqueSingleColumnIndex(indexes, col.Index) {
				return nil, 0, fmt.Errorf("%w: schema snapshot auto_increment column %q in table %d is not backed by a unique index", ErrSnapshot, col.Name, tableID)
			}
		}
		tables = append(tables, schema.TableSchema{ID: schemaTableID, Name: name, Columns: cols, Indexes: indexes})
	}
	if err := requireNoTrailingBytes(r, "trailing schema snapshot bytes"); err != nil {
		return nil, 0, err
	}
	return tables, version, nil
}

func validSchemaSnapshotValueKind(kind schema.ValueKind) bool {
	return kind >= schema.KindBool && kind <= schema.KindJSON
}

func snapshotSchemaHasUniqueSingleColumnIndex(indexes []schema.IndexSchema, columnIndex int) bool {
	for _, idx := range indexes {
		if idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == columnIndex {
			return true
		}
	}
	return false
}

func decodeSchemaSnapshotBool(v byte, field string) (bool, error) {
	switch v {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("%w: invalid schema snapshot %s flag %d", ErrSnapshot, field, v)
	}
}

type SnapshotWriter interface {
	CreateSnapshot(committed *store.CommittedState, txID types.TxID) error
}

type snapshotTempFile interface {
	io.Writer
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Close() error
}

type FileSnapshotWriter struct {
	baseDir       string
	reg           schema.SchemaRegistry
	mu            sync.Mutex
	inProgress    bool
	beforeWrite   chan struct{}
	continueWrite chan struct{}
	openTemp      func(string) (snapshotTempFile, error)
	rename        func(string, string) error
	syncDir       func(string) error
	removeLock    func(string) error
	observer      SnapshotObserver
}

func NewSnapshotWriter(baseDir string, reg schema.SchemaRegistry) SnapshotWriter {
	return NewSnapshotWriterWithObserver(baseDir, reg, nil)
}

func NewSnapshotWriterWithObserver(baseDir string, reg schema.SchemaRegistry, observer SnapshotObserver) SnapshotWriter {
	return NewFileSnapshotWriterWithObserver(baseDir, reg, observer)
}

func NewFileSnapshotWriterWithObserver(baseDir string, reg schema.SchemaRegistry, observer SnapshotObserver) *FileSnapshotWriter {
	return &FileSnapshotWriter{
		baseDir:    baseDir,
		reg:        reg,
		openTemp:   openSnapshotTempFile,
		rename:     os.Rename,
		syncDir:    syncDir,
		removeLock: RemoveLockFile,
		observer:   observer,
	}
}

func openSnapshotTempFile(path string) (snapshotTempFile, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
}

func (w *FileSnapshotWriter) CreateSnapshot(committed *store.CommittedState, txID types.TxID) (err error) {
	start := time.Now()
	defer func() {
		recordSnapshotDuration(w.observer, resultFromErr(err), time.Since(start))
	}()

	if err := w.beginSnapshot(); err != nil {
		return err
	}
	defer w.endSnapshot()

	body, err := w.captureSnapshotBody(committed, txID)
	if err != nil {
		return err
	}
	return w.createSnapshotFromBody(txID, body)
}

func (w *FileSnapshotWriter) CreateSnapshotAtCurrentHorizon(committed *store.CommittedState) (txID types.TxID, err error) {
	start := time.Now()
	defer func() {
		recordSnapshotDuration(w.observer, resultFromErr(err), time.Since(start))
	}()

	if err := w.beginSnapshot(); err != nil {
		return 0, err
	}
	defer w.endSnapshot()

	txID, body, err := w.captureSnapshotBodyAtCurrentHorizon(committed)
	if err != nil {
		return 0, err
	}
	if err := w.createSnapshotFromBody(txID, body); err != nil {
		return 0, err
	}
	return txID, nil
}

func (w *FileSnapshotWriter) beginSnapshot() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inProgress {
		return ErrSnapshotInProgress
	}
	w.inProgress = true
	return nil
}

func (w *FileSnapshotWriter) endSnapshot() {
	w.mu.Lock()
	w.inProgress = false
	w.mu.Unlock()
}

func (w *FileSnapshotWriter) createSnapshotFromBody(txID types.TxID, body snapshotBodyCapture) error {
	snapshotDir := filepath.Join(w.baseDir, strconv.FormatUint(uint64(txID), 10))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return &SnapshotCompletionError{Phase: "mkdir", Path: snapshotDir, Err: err}
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
	openTemp := w.openTemp
	if openTemp == nil {
		openTemp = openSnapshotTempFile
	}
	f, err := openTemp(tmpPath)
	if err != nil {
		return &SnapshotCompletionError{Phase: "open-temp", Path: tmpPath, Err: err}
	}
	if w.beforeWrite != nil {
		w.beforeWrite <- struct{}{}
	}
	if w.continueWrite != nil {
		<-w.continueWrite
	}

	if err := w.writeSnapshotFile(f, txID, body); err != nil {
		f.Close()
		return &SnapshotCompletionError{Phase: "write-temp", Path: tmpPath, Err: err}
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

func (w *FileSnapshotWriter) writeSnapshotFile(f snapshotTempFile, txID types.TxID, body snapshotBodyCapture) error {
	if err := writeFull(f, SnapshotMagic[:]); err != nil {
		return err
	}
	if err := writeFull(f, []byte{SnapshotVersion, 0, 0, 0}); err != nil {
		return err
	}
	if err := writeUint64Full(f, uint64(txID)); err != nil {
		return err
	}
	if err := writeUint32Full(f, w.reg.Version()); err != nil {
		return err
	}
	if err := writeFull(f, make([]byte, 32)); err != nil {
		return err
	}

	hasher := blake3.New(32, nil)
	bodyWriter := io.MultiWriter(f, hasher)
	if err := writeSnapshotBodyCapture(bodyWriter, w.reg, txID, body); err != nil {
		return err
	}
	var hash [32]byte
	copy(hash[:], hasher.Sum(nil))
	if err := writeAtFull(f, hash[:], 20); err != nil {
		return err
	}
	return nil
}

func writeSnapshotBodyCapture(dst io.Writer, reg schema.SchemaRegistry, txID types.TxID, body snapshotBodyCapture) error {
	if err := validateSnapshotBootstrapState(txID, snapshotBodyBootstrapSequences(body), snapshotBodyBootstrapNextIDs(body), snapshotBodyBootstrapRowCounts(body)); err != nil {
		return err
	}
	if err := writeUint32Full(dst, uint32(len(body.schema))); err != nil {
		return err
	}
	if err := writeFull(dst, body.schema); err != nil {
		return err
	}
	if err := writeUint32Full(dst, uint32(len(body.sequences))); err != nil {
		return err
	}
	for _, seq := range body.sequences {
		if err := writeUint32Full(dst, uint32(seq.tableID)); err != nil {
			return err
		}
		if err := writeUint64Full(dst, seq.value); err != nil {
			return err
		}
	}
	if err := writeUint32Full(dst, uint32(len(body.nextIDs))); err != nil {
		return err
	}
	for _, nextID := range body.nextIDs {
		if err := writeUint32Full(dst, uint32(nextID.tableID)); err != nil {
			return err
		}
		if err := writeUint64Full(dst, nextID.value); err != nil {
			return err
		}
	}
	if err := writeUint32Full(dst, uint32(len(body.tables))); err != nil {
		return err
	}
	rowBuf := make([]byte, 0, 1024)
	maxRowBytes := DefaultCommitLogOptions().MaxRowBytes
	for _, table := range body.tables {
		if err := writeUint32Full(dst, uint32(table.tableID)); err != nil {
			return err
		}
		if err := writeUint32Full(dst, uint32(len(table.rows))); err != nil {
			return err
		}
		for _, row := range table.rows {
			rowBuf = rowBuf[:0]
			var err error
			ts, ok := reg.Table(table.tableID)
			if !ok {
				return fmt.Errorf("%w: snapshot table section references unknown table %d", ErrSnapshot, table.tableID)
			}
			rowBuf, err = bsatn.AppendProductValueForSchema(rowBuf, row, ts)
			if err != nil {
				return err
			}
			rowLen := len(rowBuf)
			if err := validateSnapshotRowPayloadLen(rowLen, maxRowBytes); err != nil {
				return err
			}
			if err := writeUint32Full(dst, uint32(rowLen)); err != nil {
				return err
			}
			if err := writeFull(dst, rowBuf); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *FileSnapshotWriter) writeSnapshotBody(dst io.Writer, committed *store.CommittedState, txID types.TxID) error {
	body, err := w.captureSnapshotBody(committed, txID)
	if err != nil {
		return err
	}
	return writeSnapshotBodyCapture(dst, w.reg, txID, body)
}

func validateSnapshotRowPayloadLen(rowLen int, maxRowBytes uint32) error {
	if rowLen < 0 {
		return fmt.Errorf("%w: negative snapshot row payload size %d", ErrSnapshot, rowLen)
	}
	if uint64(rowLen) > math.MaxUint32 {
		return fmt.Errorf("%w: snapshot row payload %d exceeds uint32 length", ErrSnapshot, rowLen)
	}
	if maxRowBytes > 0 && uint64(rowLen) > uint64(maxRowBytes) {
		return snapshotSectionTooLarge("row", uint32(rowLen), maxRowBytes)
	}
	return nil
}

type snapshotBodyCapture struct {
	schema    []byte
	sequences []snapshotSequenceCapture
	nextIDs   []snapshotNextIDCapture
	tables    []snapshotTableCapture
}

type snapshotSequenceCapture struct {
	tableID schema.TableID
	value   uint64
}

type snapshotNextIDCapture struct {
	tableID schema.TableID
	value   uint64
}

type snapshotTableCapture struct {
	tableID schema.TableID
	rows    []types.ProductValue
}

func (w *FileSnapshotWriter) captureSnapshotBody(committed *store.CommittedState, txID types.TxID) (snapshotBodyCapture, error) {
	body, err := w.newSnapshotBodyCapture()
	if err != nil {
		return body, err
	}

	committed.RLock()
	defer committed.RUnlock()

	if committedTxID := committed.CommittedTxIDLocked(); committedTxID != txID {
		return body, &SnapshotHorizonMismatchError{
			SnapshotTxID:  txID,
			CommittedTxID: committedTxID,
		}
	}
	if err := validateSnapshotTxID(txID); err != nil {
		return body, err
	}
	if err := captureCommittedSnapshotBodyLocked(&body, committed); err != nil {
		return body, err
	}
	return body, nil
}

func (w *FileSnapshotWriter) captureSnapshotBodyAtCurrentHorizon(committed *store.CommittedState) (types.TxID, snapshotBodyCapture, error) {
	body, err := w.newSnapshotBodyCapture()
	if err != nil {
		return 0, body, err
	}

	committed.RLock()
	defer committed.RUnlock()

	txID := committed.CommittedTxIDLocked()
	if err := validateSnapshotTxID(txID); err != nil {
		return 0, body, err
	}
	if err := captureCommittedSnapshotBodyLocked(&body, committed); err != nil {
		return 0, body, err
	}
	return txID, body, nil
}

func (w *FileSnapshotWriter) newSnapshotBodyCapture() (snapshotBodyCapture, error) {
	var body snapshotBodyCapture
	var schemaBuf bytes.Buffer
	if err := EncodeSchemaSnapshot(&schemaBuf, w.reg); err != nil {
		return body, err
	}
	body.schema = append(body.schema, schemaBuf.Bytes()...)
	return body, nil
}

func validateSnapshotTxID(txID types.TxID) error {
	if txID == ^types.TxID(0) {
		return fmt.Errorf("%w: snapshot tx_id %d leaves no next tx_id", ErrSnapshot, txID)
	}
	return nil
}

func captureCommittedSnapshotBodyLocked(body *snapshotBodyCapture, committed *store.CommittedState) error {
	ids := committed.TableIDsLocked()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, tableID := range ids {
		table, _ := committed.TableLocked(tableID)
		if seq, ok := table.SequenceValue(); ok {
			body.sequences = append(body.sequences, snapshotSequenceCapture{tableID: tableID, value: seq})
		}
	}
	for _, tableID := range ids {
		table, _ := committed.TableLocked(tableID)
		body.nextIDs = append(body.nextIDs, snapshotNextIDCapture{
			tableID: tableID,
			value:   uint64(table.NextID()),
		})
	}
	for _, tableID := range ids {
		table, _ := committed.TableLocked(tableID)
		rows, err := deterministicRows(table)
		if err != nil {
			return err
		}
		body.tables = append(body.tables, snapshotTableCapture{tableID: tableID, rows: rows})
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
	path := filepath.Join(dir, snapshotFileName)
	if err := requireRegularSnapshotFile(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	txID, schemaVersion, expected, err := readSnapshotHeader(f)
	if err != nil {
		return nil, snapshotReadError(err)
	}
	if err := verifySnapshotPayloadHash(f, expected); err != nil {
		return nil, snapshotReadError(err)
	}

	tables, schemaSnapshotVersion, schemaByID, err := readSnapshotSchema(f)
	if err != nil {
		return nil, snapshotReadError(err)
	}
	sequences, err := readSnapshotSequences(f, schemaByID)
	if err != nil {
		return nil, snapshotReadError(err)
	}
	nextIDs, err := readSnapshotNextIDs(f, schemaByID)
	if err != nil {
		return nil, snapshotReadError(err)
	}
	snapshotTables, err := readSnapshotTables(f, schemaByID)
	if err != nil {
		return nil, snapshotReadError(err)
	}
	if err := validateSnapshotCompleteness(schemaByID, sequences, nextIDs, snapshotTables); err != nil {
		return nil, snapshotReadError(err)
	}
	if err := validateSnapshotAllocatorBounds(nextIDs, snapshotTables); err != nil {
		return nil, snapshotReadError(err)
	}
	if err := validateSnapshotSequenceBounds(sequences, schemaByID, snapshotTables); err != nil {
		return nil, snapshotReadError(err)
	}
	if err := validateSnapshotBootstrapState(types.TxID(txID), sequences, nextIDs, snapshotDataBootstrapRowCounts(snapshotTables)); err != nil {
		return nil, snapshotReadError(err)
	}
	if err := requireNoTrailingBytes(f, "trailing snapshot bytes"); err != nil {
		return nil, snapshotReadError(err)
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

func requireRegularSnapshotFile(path string) error {
	return requireRegularFilePath(path, ErrSnapshot, "snapshot file")
}

func snapshotReadError(err error) error {
	if err == nil || errors.Is(err, ErrSnapshot) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrSnapshot, err)
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
	if versionAndPad[1] != 0 || versionAndPad[2] != 0 || versionAndPad[3] != 0 {
		return 0, 0, [32]byte{}, ErrBadFlags
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

func requireNoTrailingBytes(r io.Reader, detail string) error {
	var trailing [1]byte
	n, err := r.Read(trailing[:])
	if n != 0 {
		return fmt.Errorf("%w: %s", ErrSnapshot, detail)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
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
	if max := DefaultCommitLogOptions().MaxRecordPayloadBytes; schemaLen > max {
		return nil, 0, nil, snapshotSectionTooLarge("schema", schemaLen, max)
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

func readSnapshotSequences(payload io.Reader, schemaByID map[schema.TableID]*schema.TableSchema) (map[schema.TableID]uint64, error) {
	return readSnapshotTableUint64Map(payload, "sequence", schemaByID)
}

func readSnapshotNextIDs(payload io.Reader, schemaByID map[schema.TableID]*schema.TableSchema) (map[schema.TableID]uint64, error) {
	return readSnapshotTableUint64Map(payload, "next_id", schemaByID)
}

func readSnapshotTableUint64Map(payload io.Reader, section string, schemaByID map[schema.TableID]*schema.TableSchema) (map[schema.TableID]uint64, error) {
	values := map[schema.TableID]uint64{}
	var count uint32
	if err := binary.Read(payload, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	for range count {
		var tableID uint32
		var next uint64
		if err := binary.Read(payload, binary.LittleEndian, &tableID); err != nil {
			return nil, err
		}
		if err := binary.Read(payload, binary.LittleEndian, &next); err != nil {
			return nil, err
		}
		id := schema.TableID(tableID)
		if _, exists := values[id]; exists {
			return nil, fmt.Errorf("%w: duplicate snapshot %s table ID %d", ErrSnapshot, section, tableID)
		}
		tableSchema, ok := schemaByID[id]
		if !ok {
			return nil, fmt.Errorf("%w: snapshot %s references unknown table %d", ErrSnapshot, section, tableID)
		}
		switch section {
		case "next_id":
			if next == 0 {
				return nil, fmt.Errorf("%w: snapshot next_id 0 for table %d is below initial row ID 1", ErrSnapshot, tableID)
			}
		case "sequence":
			if !snapshotSchemaHasAutoIncrement(tableSchema) {
				return nil, fmt.Errorf("%w: snapshot sequence references table %d without autoincrement column", ErrSnapshot, tableID)
			}
			if next == 0 {
				return nil, fmt.Errorf("%w: snapshot sequence 0 for table %d is below initial value 1", ErrSnapshot, tableID)
			}
		}
		values[id] = next
	}
	return values, nil
}

func snapshotSchemaHasAutoIncrement(tableSchema *schema.TableSchema) bool {
	for i := range tableSchema.Columns {
		if tableSchema.Columns[i].AutoIncrement {
			return true
		}
	}
	return false
}

func validateSnapshotCompleteness(schemaByID map[schema.TableID]*schema.TableSchema, sequences, nextIDs map[schema.TableID]uint64, tables []SnapshotTableData) error {
	tableSections := map[schema.TableID]struct{}{}
	for _, table := range tables {
		tableSections[table.TableID] = struct{}{}
	}
	for tableID, tableSchema := range schemaByID {
		if _, ok := nextIDs[tableID]; !ok {
			return fmt.Errorf("%w: snapshot missing next_id for table %d", ErrSnapshot, tableID)
		}
		if _, ok := tableSections[tableID]; !ok {
			return fmt.Errorf("%w: snapshot missing table section for table %d", ErrSnapshot, tableID)
		}
		if snapshotSchemaHasAutoIncrement(tableSchema) {
			if _, ok := sequences[tableID]; !ok {
				return fmt.Errorf("%w: snapshot missing sequence for autoincrement table %d", ErrSnapshot, tableID)
			}
		}
	}
	return nil
}

func validateSnapshotAllocatorBounds(nextIDs map[schema.TableID]uint64, tables []SnapshotTableData) error {
	for _, table := range tables {
		minNext := uint64(len(table.Rows)) + 1
		if nextIDs[table.TableID] < minNext {
			return &SnapshotAllocatorBoundsError{
				TableID: uint32(table.TableID),
				NextID:  nextIDs[table.TableID],
				MinNext: minNext,
			}
		}
	}
	return nil
}

func validateSnapshotSequenceBounds(sequences map[schema.TableID]uint64, schemaByID map[schema.TableID]*schema.TableSchema, tables []SnapshotTableData) error {
	for _, table := range tables {
		next, ok := sequences[table.TableID]
		if !ok {
			continue
		}
		tableSchema := schemaByID[table.TableID]
		sequenceCol := -1
		for i := range tableSchema.Columns {
			if tableSchema.Columns[i].AutoIncrement {
				sequenceCol = i
				break
			}
		}
		if sequenceCol < 0 {
			continue
		}
		maxSeen := uint64(0)
		for _, row := range table.Rows {
			value, ok := autoIncrementValueAsUint64(row[sequenceCol], tableSchema.Columns[sequenceCol].Type)
			if !ok {
				continue
			}
			if value > maxSeen {
				maxSeen = value
			}
		}
		minNext := maxSeen + 1
		if maxSeen == ^uint64(0) {
			minNext = maxSeen
		}
		if next < minNext {
			return &SnapshotSequenceBoundsError{
				TableID: uint32(table.TableID),
				Next:    next,
				MinNext: minNext,
			}
		}
	}
	return nil
}

func snapshotBodyBootstrapSequences(body snapshotBodyCapture) map[schema.TableID]uint64 {
	values := make(map[schema.TableID]uint64, len(body.sequences))
	for _, seq := range body.sequences {
		values[seq.tableID] = seq.value
	}
	return values
}

func snapshotBodyBootstrapNextIDs(body snapshotBodyCapture) map[schema.TableID]uint64 {
	values := make(map[schema.TableID]uint64, len(body.nextIDs))
	for _, nextID := range body.nextIDs {
		values[nextID.tableID] = nextID.value
	}
	return values
}

func snapshotBodyBootstrapRowCounts(body snapshotBodyCapture) map[schema.TableID]int {
	counts := make(map[schema.TableID]int, len(body.tables))
	for _, table := range body.tables {
		counts[table.tableID] = len(table.rows)
	}
	return counts
}

func snapshotDataBootstrapRowCounts(tables []SnapshotTableData) map[schema.TableID]int {
	counts := make(map[schema.TableID]int, len(tables))
	for _, table := range tables {
		counts[table.TableID] = len(table.Rows)
	}
	return counts
}

func validateSnapshotBootstrapState(txID types.TxID, sequences, nextIDs map[schema.TableID]uint64, rowCounts map[schema.TableID]int) error {
	if txID != 0 {
		return nil
	}
	tableIDs := sortedSnapshotBootstrapTableIDs(rowCounts)
	for _, tableID := range tableIDs {
		if rowCounts[tableID] != 0 {
			return fmt.Errorf("%w: snapshot at bootstrap tx 0 contains %d rows for table %d", ErrSnapshot, rowCounts[tableID], tableID)
		}
	}
	tableIDs = sortedSnapshotBootstrapTableIDs(nextIDs)
	for _, tableID := range tableIDs {
		if nextIDs[tableID] != 1 {
			return fmt.Errorf("%w: snapshot at bootstrap tx 0 has next_id %d for table %d", ErrSnapshot, nextIDs[tableID], tableID)
		}
	}
	tableIDs = sortedSnapshotBootstrapTableIDs(sequences)
	for _, tableID := range tableIDs {
		if sequences[tableID] != 1 {
			return fmt.Errorf("%w: snapshot at bootstrap tx 0 has sequence %d for table %d", ErrSnapshot, sequences[tableID], tableID)
		}
	}
	return nil
}

func sortedSnapshotBootstrapTableIDs[T any](values map[schema.TableID]T) []schema.TableID {
	tableIDs := make([]schema.TableID, 0, len(values))
	for tableID := range values {
		tableIDs = append(tableIDs, tableID)
	}
	sort.Slice(tableIDs, func(i, j int) bool { return tableIDs[i] < tableIDs[j] })
	return tableIDs
}

func readSnapshotTables(payload io.Reader, schemaByID map[schema.TableID]*schema.TableSchema) ([]SnapshotTableData, error) {
	var tableCount uint32
	if err := binary.Read(payload, binary.LittleEndian, &tableCount); err != nil {
		return nil, err
	}
	var tables []SnapshotTableData
	seenTables := map[schema.TableID]struct{}{}
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
		id := schema.TableID(tableID)
		if _, exists := seenTables[id]; exists {
			return nil, fmt.Errorf("%w: duplicate snapshot table section %d", ErrSnapshot, tableID)
		}
		seenTables[id] = struct{}{}
		snapshotTable := SnapshotTableData{TableID: id}
		ts, ok := schemaByID[id]
		if !ok {
			return nil, fmt.Errorf("%w: snapshot table section references unknown table %d", ErrSnapshot, tableID)
		}
		for range rowCount {
			var rowLen uint32
			if err := binary.Read(payload, binary.LittleEndian, &rowLen); err != nil {
				return nil, err
			}
			if max := DefaultCommitLogOptions().MaxRowBytes; rowLen > max {
				return nil, snapshotSectionTooLarge("row", rowLen, max)
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
		if strconv.FormatUint(txID, 10) != entry.Name() {
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
	if err := writeUint32Full(w, uint32(len(s))); err != nil {
		return err
	}
	return writeFull(w, []byte(s))
}

func writeUint32Full(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return writeFull(w, buf[:])
}

func writeUint64Full(w io.Writer, v uint64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return writeFull(w, buf[:])
}

func writeAtFull(f interface {
	WriteAt([]byte, int64) (int, error)
}, p []byte, off int64) error {
	if len(p) == 0 {
		return nil
	}
	n, err := f.WriteAt(p, off)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}

func readString(r io.Reader) (string, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n > maxSnapshotStringBytes {
		return "", snapshotSectionTooLarge("schema string", n, maxSnapshotStringBytes)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	if !utf8.Valid(buf) {
		return "", fmt.Errorf("%w: invalid UTF-8 schema string", ErrSnapshot)
	}
	return string(buf), nil
}

func snapshotSectionTooLarge(section string, size uint32, max uint32) error {
	return fmt.Errorf("%w: snapshot %s section %d exceeds max %d", ErrSnapshot, section, size, max)
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
