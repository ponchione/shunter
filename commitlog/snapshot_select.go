package commitlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func SelectSnapshot(baseDir string, durableHorizon types.TxID, reg schema.SchemaRegistry) (*SnapshotData, error) {
	snapshot, _, err := selectSnapshotWithReport(baseDir, durableHorizon, reg)
	return snapshot, err
}

// ValidateSnapshotForCompaction requires one exact, completed snapshot that is
// safe to use as the durable base for deleting covered commit-log segments.
func ValidateSnapshotForCompaction(baseDir string, txID types.TxID, reg schema.SchemaRegistry) error {
	snapshotDir, _ := resolveSnapshotAndLogDirs(baseDir)
	dir := filepath.Join(snapshotDir, fmt.Sprintf("%d", txID))
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("%w: snapshot tx_id %d: %w", ErrSnapshot, txID, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: snapshot tx_id %d path is not a directory", ErrSnapshot, txID)
	}
	if HasLockFile(dir) || HasSnapshotTempFile(dir) {
		return fmt.Errorf("%w: snapshot tx_id %d is incomplete", ErrSnapshot, txID)
	}

	snapshot, err := readSnapshotWithExpectedTxID(snapshotDir, txID)
	if err != nil {
		return err
	}
	return compareSnapshotSchema(snapshot, reg)
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
		snapshot, skip, err := readSnapshotCandidate(snapshotDir, txID, reg)
		if err != nil {
			return nil, skipped, err
		}
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
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

func readSnapshotCandidate(snapshotDir string, txID types.TxID, reg schema.SchemaRegistry) (*SnapshotData, *SkippedSnapshotReport, error) {
	snapshot, err := readSnapshotWithExpectedTxID(snapshotDir, txID)
	if err != nil {
		if isUnsafeSnapshotSelectionError(err) {
			return nil, nil, err
		}
		return nil, &SkippedSnapshotReport{
			TxID:   txID,
			Reason: SnapshotSkipReadFailed,
			Detail: err.Error(),
		}, nil
	}
	if err := compareSnapshotSchema(snapshot, reg); err != nil {
		return nil, nil, err
	}
	return snapshot, nil, nil
}

func readSnapshotWithExpectedTxID(snapshotDir string, txID types.TxID) (*SnapshotData, error) {
	snapshot, err := ReadSnapshot(filepath.Join(snapshotDir, fmt.Sprintf("%d", txID)))
	if err != nil {
		return nil, err
	}
	if snapshot.TxID != txID {
		return nil, fmt.Errorf("%w: snapshot tx_id mismatch: directory=%d header=%d", ErrSnapshot, txID, snapshot.TxID)
	}
	return snapshot, nil
}

func compareSnapshotSchema(snapshot *SnapshotData, reg schema.SchemaRegistry) error {
	report := analyzeSnapshotSchema(snapshot, reg)
	if report.Compatible {
		return nil
	}
	return &SchemaMismatchError{Detail: report.MismatchDetail(), Report: report}
}

func analyzeSnapshotSchema(snapshot *SnapshotData, reg schema.SchemaRegistry) schema.SchemaCompatibilityReport {
	var snapshotVersion uint32
	if snapshot != nil {
		snapshotVersion = snapshot.SchemaSnapshotVersion
		if snapshotVersion == 0 {
			snapshotVersion = snapshot.SchemaVersion
		}
	}
	_, report := schema.ReconcileRegistryForSnapshot(reg, &schema.SnapshotSchema{
		Version: snapshotVersion,
		Tables:  snapshot.Schema,
	})
	return report
}

func reconcileSnapshotRegistry(snapshot *SnapshotData, reg schema.SchemaRegistry) schema.SchemaRegistry {
	if snapshot == nil {
		return reg
	}
	var snapshotVersion uint32
	snapshotVersion = snapshot.SchemaSnapshotVersion
	if snapshotVersion == 0 {
		snapshotVersion = snapshot.SchemaVersion
	}
	reconciled, _ := schema.ReconcileRegistryForSnapshot(reg, &schema.SnapshotSchema{
		Version: snapshotVersion,
		Tables:  snapshot.Schema,
	})
	return reconciled
}

func isUnsafeSnapshotSelectionError(err error) bool {
	var allocatorErr *SnapshotAllocatorBoundsError
	if errors.As(err, &allocatorErr) {
		return true
	}
	var sequenceErr *SnapshotSequenceBoundsError
	return errors.As(err, &sequenceErr)
}
