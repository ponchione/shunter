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
