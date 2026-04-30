package commitlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func SelectSnapshot(baseDir string, durableHorizon types.TxID, reg schema.SchemaRegistry) (*SnapshotData, error) {
	snapshot, _, err := selectSnapshotWithReport(baseDir, durableHorizon, reg)
	return snapshot, err
}

func selectSnapshotWithReport(baseDir string, durableHorizon types.TxID, reg schema.SchemaRegistry) (*SnapshotData, []SkippedSnapshotReport, error) {
	snapshotDir, logDir := resolveSnapshotAndLogDirs(baseDir)

	ids, err := ListSnapshots(snapshotDir)
	if err != nil {
		return nil, nil, err
	}
	var skipped []SkippedSnapshotReport
	for _, txID := range ids {
		if txID > durableHorizon {
			skipped = append(skipped, SkippedSnapshotReport{
				TxID:   txID,
				Reason: SnapshotSkipPastDurableHorizon,
			})
			continue
		}
		snapshot, err := ReadSnapshot(filepath.Join(snapshotDir, fmt.Sprintf("%d", txID)))
		if err != nil {
			if isUnsafeSnapshotSelectionError(err) {
				return nil, skipped, err
			}
			skipped = append(skipped, SkippedSnapshotReport{
				TxID:   txID,
				Reason: SnapshotSkipReadFailed,
				Detail: err.Error(),
			})
			continue
		}
		if snapshot.TxID != txID {
			err := fmt.Errorf("%w: snapshot tx_id mismatch: directory=%d header=%d", ErrSnapshot, txID, snapshot.TxID)
			skipped = append(skipped, SkippedSnapshotReport{
				TxID:   txID,
				Reason: SnapshotSkipReadFailed,
				Detail: err.Error(),
			})
			continue
		}
		if err := compareSnapshotSchema(snapshot, reg); err != nil {
			return nil, skipped, err
		}
		return snapshot, skipped, nil
	}

	segments, _, err := ScanSegments(logDir)
	if err != nil {
		return nil, skipped, err
	}
	if len(segments) == 0 || segments[0].StartTx <= 1 {
		return nil, skipped, nil
	}
	return nil, skipped, ErrMissingBaseSnapshot
}

func resolveSnapshotAndLogDirs(baseDir string) (string, string) {
	snapshotDir := baseDir
	logDir := baseDir
	if info, err := os.Stat(filepath.Join(baseDir, "snapshots")); err == nil && info.IsDir() {
		return filepath.Join(baseDir, "snapshots"), logDir
	}
	if filepath.Base(baseDir) == "snapshots" {
		return snapshotDir, filepath.Dir(baseDir)
	}
	return snapshotDir, logDir
}

func compareSnapshotSchema(snapshot *SnapshotData, reg schema.SchemaRegistry) error {
	if snapshot.SchemaVersion != reg.Version() {
		return &SchemaMismatchError{Detail: fmt.Sprintf("schema version mismatch: snapshot=%d registry=%d", snapshot.SchemaVersion, reg.Version())}
	}
	if snapshot.SchemaSnapshotVersion != reg.Version() {
		return &SchemaMismatchError{Detail: fmt.Sprintf("schema snapshot version mismatch: snapshot=%d registry=%d", snapshot.SchemaSnapshotVersion, reg.Version())}
	}

	snapshotByID := make(map[schema.TableID]schema.TableSchema, len(snapshot.Schema))
	for _, table := range snapshot.Schema {
		snapshotByID[table.ID] = table
	}

	for _, tableID := range reg.Tables() {
		registered, ok := reg.Table(tableID)
		if !ok {
			return &SchemaMismatchError{Detail: fmt.Sprintf("registry missing table %d", tableID)}
		}
		stored, ok := snapshotByID[tableID]
		if !ok {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q (id=%d) missing from snapshot", registered.Name, tableID)}
		}
		if err := compareTableSchema(*registered, stored); err != nil {
			return err
		}
		delete(snapshotByID, tableID)
	}

	for _, extra := range snapshotByID {
		return &SchemaMismatchError{Detail: fmt.Sprintf("snapshot has extra table %q (id=%d)", extra.Name, extra.ID)}
	}

	return nil
}

func isUnsafeSnapshotSelectionError(err error) bool {
	var allocatorErr *SnapshotAllocatorBoundsError
	return errors.As(err, &allocatorErr)
}

func compareTableSchema(registered schema.TableSchema, snapshot schema.TableSchema) error {
	if registered.Name != snapshot.Name {
		return &SchemaMismatchError{Detail: fmt.Sprintf("table id %d name mismatch: snapshot=%q registry=%q", registered.ID, snapshot.Name, registered.Name)}
	}
	if len(registered.Columns) != len(snapshot.Columns) {
		return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column count mismatch: snapshot=%d registry=%d", registered.Name, len(snapshot.Columns), len(registered.Columns))}
	}
	for i := range registered.Columns {
		regCol := registered.Columns[i]
		snapCol := snapshot.Columns[i]
		if regCol.Index != snapCol.Index {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %d index mismatch: snapshot=%d registry=%d", registered.Name, i, snapCol.Index, regCol.Index)}
		}
		if regCol.Name != snapCol.Name {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %d name mismatch: snapshot=%q registry=%q", registered.Name, i, snapCol.Name, regCol.Name)}
		}
		if regCol.Type != snapCol.Type {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %q type mismatch: snapshot=%v registry=%v", registered.Name, regCol.Name, snapCol.Type, regCol.Type)}
		}
		if snapCol.Nullable {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %q nullable must be false in v1 snapshots", registered.Name, regCol.Name), Cause: schema.ErrNullableColumn}
		}
		if regCol.Nullable != snapCol.Nullable {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %q nullable mismatch: snapshot=%t registry=%t", registered.Name, regCol.Name, snapCol.Nullable, regCol.Nullable)}
		}
		if regCol.AutoIncrement != snapCol.AutoIncrement {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q column %q auto_increment mismatch: snapshot=%t registry=%t", registered.Name, regCol.Name, snapCol.AutoIncrement, regCol.AutoIncrement)}
		}
	}
	if len(registered.Indexes) != len(snapshot.Indexes) {
		return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index count mismatch: snapshot=%d registry=%d", registered.Name, len(snapshot.Indexes), len(registered.Indexes))}
	}
	for i := range registered.Indexes {
		regIdx := registered.Indexes[i]
		snapIdx := snapshot.Indexes[i]
		if regIdx.Name != snapIdx.Name {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %d name mismatch: snapshot=%q registry=%q", registered.Name, i, snapIdx.Name, regIdx.Name)}
		}
		if !slices.Equal(regIdx.Columns, snapIdx.Columns) {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %q columns mismatch: snapshot=%v registry=%v", registered.Name, regIdx.Name, snapIdx.Columns, regIdx.Columns)}
		}
		if regIdx.Unique != snapIdx.Unique {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %q unique mismatch: snapshot=%t registry=%t", registered.Name, regIdx.Name, snapIdx.Unique, regIdx.Unique)}
		}
		if regIdx.Primary != snapIdx.Primary {
			return &SchemaMismatchError{Detail: fmt.Sprintf("table %q index %q primary mismatch: snapshot=%t registry=%t", registered.Name, regIdx.Name, snapIdx.Primary, regIdx.Primary)}
		}
	}
	return nil
}
