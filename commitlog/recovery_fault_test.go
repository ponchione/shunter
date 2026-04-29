package commitlog

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestOpenAndRecoverDurabilityBoundaryFaultMatrix(t *testing.T) {
	type faultCase struct {
		name   string
		setup  func(t *testing.T, root string, reg schema.SchemaRegistry)
		assert func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error)
	}

	cases := []faultCase{
		{
			name: "damaged-snapshot-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				corruptSelectionSnapshot(t, root, 2)
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "missing-snapshot-file-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				createMissingSnapshotCandidate(t, root, 2)
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "locked-snapshot-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{99: "unsafe-locked"})
				markSnapshotLocked(t, root, 2)
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				if report.HasSelectedSnapshot || len(report.SkippedSnapshots) != 0 {
					t.Fatalf("snapshot report = selected(%v, %d) skipped=%+v, want locked candidate ignored", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, report.SkippedSnapshots)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "temp-snapshot-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{99: "unsafe-temp"})
				markSnapshotTemp(t, root, 2)
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				if report.HasSelectedSnapshot || len(report.SkippedSnapshots) != 0 {
					t.Fatalf("snapshot report = selected(%v, %d) skipped=%+v, want temp candidate ignored", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, report.SkippedSnapshots)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "truncated-snapshot-file-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{99: "unsafe-truncated"})
				truncateSnapshotFile(t, root, 2, SnapshotHeaderSize-1)
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "missing-newest-snapshot-file-falls-back-to-older-snapshot-and-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				createMissingSnapshotCandidate(t, root, 7)
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 7 {
					t.Fatalf("maxTxID = %d, want 7", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 7, SnapshotSkipReadFailed)
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 7}) {
					t.Fatalf("replayed range = %+v, want 6..7", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 8 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 8", plan)
				}
			},
		},
		{
			name: "truncated-newest-snapshot-file-falls-back-to-older-snapshot-and-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				writeFaultSnapshot(t, root, reg, 7, map[uint64]string{99: "unsafe-truncated"})
				truncateSnapshotFile(t, root, 7, SnapshotHeaderSize-1)
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 7 {
					t.Fatalf("maxTxID = %d, want 7", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 7, SnapshotSkipReadFailed)
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 7}) {
					t.Fatalf("replayed range = %+v, want 6..7", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 8 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 8", plan)
				}
			},
		},
		{
			name: "locked-newest-snapshot-falls-back-to-older-snapshot-and-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				writeFaultSnapshot(t, root, reg, 7, map[uint64]string{99: "unsafe-locked"})
				markSnapshotLocked(t, root, 7)
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 7 {
					t.Fatalf("maxTxID = %d, want 7", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if len(report.SkippedSnapshots) != 0 {
					t.Fatalf("skipped snapshots = %+v, want locked candidate ignored before selection", report.SkippedSnapshots)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 7}) {
					t.Fatalf("replayed range = %+v, want 6..7", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 8 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 8", plan)
				}
			},
		},
		{
			name: "temp-newest-snapshot-falls-back-to-older-snapshot-and-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				writeFaultSnapshot(t, root, reg, 7, map[uint64]string{99: "unsafe-temp"})
				markSnapshotTemp(t, root, 7)
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 7 {
					t.Fatalf("maxTxID = %d, want 7", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if len(report.SkippedSnapshots) != 0 {
					t.Fatalf("skipped snapshots = %+v, want temp candidate ignored before selection", report.SkippedSnapshots)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 7}) {
					t.Fatalf("replayed range = %+v, want 6..7", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 8 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 8", plan)
				}
			},
		},
		{
			name: "missing-snapshot-file-with-log-after-base-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				createMissingSnapshotCandidate(t, root, 2)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected missing base snapshot error")
				}
				if !errors.Is(err, ErrMissingBaseSnapshot) {
					t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
			},
		},
		{
			name: "truncated-snapshot-file-with-log-after-base-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				truncateSnapshotFile(t, root, 2, SnapshotHeaderSize-1)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected missing base snapshot error")
				}
				if !errors.Is(err, ErrMissingBaseSnapshot) {
					t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.HasSelectedSnapshot || report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) {
					t.Fatalf("report = %+v, want no selected snapshot, recovered tx, or resume plan", report)
				}
			},
		},
		{
			name: "temp-snapshot-with-log-after-base-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				markSnapshotTemp(t, root, 2)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected missing base snapshot error")
				}
				if !errors.Is(err, ErrMissingBaseSnapshot) {
					t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				if len(report.SkippedSnapshots) != 0 || report.HasSelectedSnapshot {
					t.Fatalf("snapshot report = selected(%v, %d) skipped=%+v, want temp candidate ignored", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, report.SkippedSnapshots)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
			},
		},
		{
			name: "locked-snapshot-with-log-after-base-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				markSnapshotLocked(t, root, 2)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected missing base snapshot error")
				}
				if !errors.Is(err, ErrMissingBaseSnapshot) {
					t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				if len(report.SkippedSnapshots) != 0 || report.HasSelectedSnapshot {
					t.Fatalf("snapshot report = selected(%v, %d) skipped=%+v, want locked candidate ignored", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, report.SkippedSnapshots)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
			},
		},
		{
			name: "missing-middle-log-segment-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				writeReplaySegment(t, root, 4,
					replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected history gap error")
				}
				var gapErr *HistoryGapError
				if !errors.As(err, &gapErr) {
					t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
				}
				if gapErr.Expected != 3 || gapErr.Got != 4 {
					t.Fatalf("HistoryGapError = %+v, want Expected=3 Got=4", gapErr)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("history gap error = %v, want ErrOpen category", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertZeroRecoveryReport(t, report)
			},
		},
		{
			name: "snapshot-covered-truncated-rollover-first-record-recovers-snapshot",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				path := writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial-carol")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
					t.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
				}
				if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].StartTx != 3 || report.DamagedTailSegments[0].LastTx != 2 {
					t.Fatalf("damaged tail report = %+v, want empty damaged segment 3..2", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
				}
			},
		},
		{
			name: "snapshot-covered-header-only-rollover-recovers-snapshot",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				createHeaderOnlySegment(t, root, 3)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
					t.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 2 {
					t.Fatalf("durable log report = (%v, %d), want (true, 2)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 3 || report.SegmentCoverage[0].MaxTxID != 2 {
					t.Fatalf("segment coverage = %+v, want empty rollover segment 3..2", report.SegmentCoverage)
				}
				if len(report.DamagedTailSegments) != 0 {
					t.Fatalf("damaged tail report = %+v, want none for header-only segment", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 3 at tx 3", plan)
				}
			},
		},
		{
			name: "snapshot-log-gap-to-truncated-rollover-first-record-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				path := writeReplaySegment(t, root, 4,
					replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("partial-dave")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected snapshot/log boundary gap error")
				}
				var gapErr *HistoryGapError
				if !errors.As(err, &gapErr) {
					t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
				}
				if gapErr.Expected != 3 || gapErr.Got != 4 {
					t.Fatalf("HistoryGapError = %+v, want Expected=3 Got=4", gapErr)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("boundary gap error = %v, want ErrOpen category", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].StartTx != 4 || report.DamagedTailSegments[0].LastTx != 3 {
					t.Fatalf("damaged tail report = %+v, want empty damaged segment 4..3", report.DamagedTailSegments)
				}
				if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) {
					t.Fatalf("report = %+v, want no recovered tx or resume plan", report)
				}
			},
		},
		{
			name: "snapshot-log-gap-to-header-only-rollover-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				createHeaderOnlySegment(t, root, 4)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected snapshot/log boundary gap error")
				}
				var gapErr *HistoryGapError
				if !errors.As(err, &gapErr) {
					t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
				}
				if gapErr.Expected != 3 || gapErr.Got != 4 {
					t.Fatalf("HistoryGapError = %+v, want Expected=3 Got=4", gapErr)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("boundary gap error = %v, want ErrOpen category", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if len(report.DamagedTailSegments) != 0 {
					t.Fatalf("damaged tail report = %+v, want none for header-only gap", report.DamagedTailSegments)
				}
				if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 4 || report.SegmentCoverage[0].MaxTxID != 3 {
					t.Fatalf("segment coverage = %+v, want empty rollover segment 4..3", report.SegmentCoverage)
				}
				if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) {
					t.Fatalf("report = %+v, want no recovered tx or resume plan", report)
				}
			},
		},
		{
			name: "full-log-prefix-with-truncated-rollover-first-record-recovers-prefix",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				path := writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial-carol")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 2}) {
					t.Fatalf("replayed range = %+v, want 1..2", report.ReplayedTxRange)
				}
				if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].StartTx != 3 || report.DamagedTailSegments[0].LastTx != 2 {
					t.Fatalf("damaged tail report = %+v, want empty damaged segment 3..2", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
				}
			},
		},
		{
			name: "damaged-sealed-segment-tail-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				sealedPath := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
				corruptScanTestRecordPayloadByte(t, sealedPath, 1, 0)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected history gap error")
				}
				var gapErr *HistoryGapError
				if !errors.As(err, &gapErr) {
					t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
				}
				if gapErr.Expected != 2 || gapErr.Got != 3 {
					t.Fatalf("HistoryGapError = %+v, want Expected=2 Got=3", gapErr)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertZeroRecoveryReport(t, report)
			},
		},
		{
			name: "truncated-first-record-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				path := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected truncated first record error")
				}
				if !errors.Is(err, ErrTruncatedRecord) {
					t.Fatalf("error = %v, want ErrTruncatedRecord", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 0 {
					t.Fatalf("durable log report = (%v, %d), want (true, 0)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].StartTx != 1 || report.DamagedTailSegments[0].LastTx != 0 {
					t.Fatalf("damaged tail report = %+v, want empty damaged segment 1..0", report.DamagedTailSegments)
				}
				if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 1 || report.SegmentCoverage[0].MaxTxID != 0 {
					t.Fatalf("segment coverage = %+v, want one empty 1..0 segment", report.SegmentCoverage)
				}
			},
		},
		{
			name: "truncated-active-tail-header-recovers-valid-prefix",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				path := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial-carol")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(scanTestRecordOffset(t, path, 2)+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].LastTx != 2 {
					t.Fatalf("damaged tail report = %+v, want one segment with LastTx 2", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
				}
			},
		},
		{
			name: "zero-filled-active-tail-recovers-valid-prefix",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				path := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				appendZeroTail(t, path, RecordOverhead*2)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if len(report.DamagedTailSegments) != 0 {
					t.Fatalf("damaged tail report = %+v, want none for zero-filled tail", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 3", plan)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, reg := testSchema()
			tc.setup(t, root, reg)

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
			tc.assert(t, recovered, maxTxID, plan, report, err)
		})
	}
}

func TestOpenAndRecoverSegmentHeaderFaultsFailLoudly(t *testing.T) {
	cases := []struct {
		name   string
		bytes  []byte
		assert func(t *testing.T, err error)
	}{
		{
			name:  "bad-magic",
			bytes: []byte{'X', 'X', 'X', 'X', SegmentVersion, 0, 0, 0},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrBadMagic) {
					t.Fatalf("error = %v, want ErrBadMagic", err)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("error = %v, want ErrOpen category", err)
				}
			},
		},
		{
			name:  "bad-version",
			bytes: []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2], SegmentMagic[3], SegmentVersion + 1, 0, 0, 0},
			assert: func(t *testing.T, err error) {
				var versionErr *BadVersionError
				if !errors.As(err, &versionErr) {
					t.Fatalf("expected BadVersionError, got %T (%v)", err, err)
				}
				if versionErr.Got != SegmentVersion+1 {
					t.Fatalf("bad version got = %d, want %d", versionErr.Got, SegmentVersion+1)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("error = %v, want ErrOpen category", err)
				}
			},
		},
		{
			name:  "bad-flags",
			bytes: []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2], SegmentMagic[3], SegmentVersion, 1, 0, 0},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrBadFlags) {
					t.Fatalf("error = %v, want ErrBadFlags", err)
				}
			},
		},
		{
			name:  "truncated-header",
			bytes: []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2]},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("error = %v, want io.ErrUnexpectedEOF", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, reg := testSchema()
			path := filepath.Join(root, SegmentFileName(1))
			if err := os.WriteFile(path, tc.bytes, 0o644); err != nil {
				t.Fatal(err)
			}

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
			if err == nil {
				t.Fatal("expected recovery to fail loudly")
			}
			if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
				t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
			}
			assertZeroRecoveryReport(t, report)
			tc.assert(t, err)
		})
	}
}

func TestOpenAndRecoverSnapshotSchemaMismatchFailsLoudly(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
	writeReplaySegment(t, root, 6,
		replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)

	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Columns[1].Type = schema.KindUint64
		tables[0] = players
	})

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, mismatchReg)
	if err == nil {
		t.Fatal("expected schema mismatch error")
	}
	var mismatchErr *SchemaMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected SchemaMismatchError, got %T (%v)", err, err)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("error = %v, want ErrSnapshot category", err)
	}
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 6 {
		t.Fatalf("durable log report = (%v, %d), want (true, 6)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if report.HasSelectedSnapshot || len(report.SkippedSnapshots) != 0 || report.RecoveredTxID != 0 {
		t.Fatalf("report = %+v, want no selected/skipped snapshot and no recovered tx", report)
	}
}

func TestOpenAndRecoverSnapshotPastDurableHorizonMatrix(t *testing.T) {
	type horizonCase struct {
		name   string
		setup  func(t *testing.T, root string, reg schema.SchemaRegistry)
		assert func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error)
	}

	cases := []horizonCase{
		{
			name: "newest-snapshot-past-horizon-falls-back-to-older-snapshot",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				writeFaultSnapshot(t, root, reg, 10, map[uint64]string{99: "too-new"})
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
					replayRecord{txID: 8, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 8 {
					t.Fatalf("maxTxID = %d, want 8", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol", 4: "dave"})
				assertSkippedSnapshot(t, report, 10, SnapshotSkipPastDurableHorizon)
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 8 {
					t.Fatalf("durable log report = (%v, %d), want (true, 8)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 8}) {
					t.Fatalf("replayed range = %+v, want 6..8", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 9 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 9", plan)
				}
			},
		},
		{
			name: "only-snapshot-past-horizon-recovers-complete-base-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{99: "too-new"})
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 5, SnapshotSkipPastDurableHorizon)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "only-snapshot-past-horizon-with-missing-base-log-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{99: "too-new"})
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
					replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected missing base snapshot error")
				}
				if !errors.Is(err, ErrMissingBaseSnapshot) {
					t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertSkippedSnapshot(t, report, 5, SnapshotSkipPastDurableHorizon)
				if !report.HasDurableLog || report.DurableLogHorizon != 4 {
					t.Fatalf("durable log report = (%v, %d), want (true, 4)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.HasSelectedSnapshot || report.RecoveredTxID != 0 {
					t.Fatalf("report = %+v, want no selected snapshot or recovered tx", report)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, reg := testSchema()
			tc.setup(t, root, reg)

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
			tc.assert(t, recovered, maxTxID, plan, report, err)
		})
	}
}

func TestOpenAndRecoverHeaderOnlySegmentBoundaries(t *testing.T) {
	t.Run("first-segment-before-first-record-recovers-empty-state", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		createHeaderOnlySegment(t, root, 1)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatal(err)
		}
		if maxTxID != 0 {
			t.Fatalf("maxTxID = %d, want 0", maxTxID)
		}
		assertReplayPlayerRows(t, recovered, map[uint64]string{})
		if !report.HasDurableLog || report.DurableLogHorizon != 0 {
			t.Fatalf("durable log report = (%v, %d), want (true, 0)", report.HasDurableLog, report.DurableLogHorizon)
		}
		if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 1 || report.SegmentCoverage[0].MaxTxID != 0 {
			t.Fatalf("segment coverage = %+v, want one empty 1..0 segment", report.SegmentCoverage)
		}
		if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 1 {
			t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 1", plan)
		}
	})

	t.Run("rollover-segment-before-first-record-recovers-valid-prefix", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		)
		createHeaderOnlySegment(t, root, 3)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatal(err)
		}
		if maxTxID != 2 {
			t.Fatalf("maxTxID = %d, want 2", maxTxID)
		}
		assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
		if !report.HasDurableLog || report.DurableLogHorizon != 2 {
			t.Fatalf("durable log report = (%v, %d), want (true, 2)", report.HasDurableLog, report.DurableLogHorizon)
		}
		if len(report.SegmentCoverage) != 2 || report.SegmentCoverage[1].MinTxID != 3 || report.SegmentCoverage[1].MaxTxID != 2 {
			t.Fatalf("segment coverage = %+v, want empty active rollover segment 3..2", report.SegmentCoverage)
		}
		if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
			t.Fatalf("resume plan = %+v, want append-in-place on segment 3 at tx 3", plan)
		}
	})

	t.Run("header-only-segment-after-gap-fails-loudly", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		createHeaderOnlySegment(t, root, 3)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected missing base snapshot error")
		}
		if !errors.Is(err, ErrMissingBaseSnapshot) {
			t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
		}
		if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
			t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
		}
		if !report.HasDurableLog || report.DurableLogHorizon != 2 {
			t.Fatalf("durable log report = (%v, %d), want (true, 2)", report.HasDurableLog, report.DurableLogHorizon)
		}
	})
}

func TestOpenAndRecoverLogicalReplayFaultsFailLoudly(t *testing.T) {
	t.Run("valid-record-with-invalid-changeset-payload", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: []byte{0xff}},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected invalid changeset payload to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "changeset too short") {
			t.Fatalf("replay decode error %q missing payload failure detail", err)
		}
	})

	t.Run("valid-record-with-unsupported-changeset-version", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: []byte{changesetVersion + 1, 0, 0, 0, 0}},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected unsupported changeset version to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "unsupported changeset version") {
			t.Fatalf("replay decode error %q missing version failure detail", err)
		}
	})

	t.Run("valid-record-with-unsafe-duplicate-primary-key", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice-again")}}},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected duplicate primary key payload to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		var pkErr *store.PrimaryKeyViolationError
		if !errors.As(err, &pkErr) {
			t.Fatalf("expected PrimaryKeyViolationError, got %T (%v)", err, err)
		}
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay apply error %q missing tx or segment context", err)
		}
	})

	t.Run("valid-record-with-delete-of-missing-row", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, deletes: []types.ProductValue{{types.NewUint64(2), types.NewString("missing-bob")}}},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected missing-row delete to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !errors.Is(err, store.ErrRowNotFound) {
			t.Fatalf("expected ErrRowNotFound, got %T (%v)", err, err)
		}
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay apply error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "replay delete row not found") {
			t.Fatalf("replay apply error %q missing delete failure detail", err)
		}
	})

	t.Run("valid-record-with-unknown-table-changeset", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload := []byte{changesetVersion}
		payload = appendUint32(payload, 1)
		payload = appendUint32(payload, 99)
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected unknown table changeset to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "unknown table ID 99") {
			t.Fatalf("replay decode error %q missing unknown table detail", err)
		}
	})

	t.Run("valid-record-with-duplicate-table-changeset", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload := []byte{changesetVersion}
		payload = appendUint32(payload, 2)
		for range 2 {
			payload = appendUint32(payload, 0)
			payload = appendUint32(payload, 0)
			payload = appendUint32(payload, 0)
		}
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected duplicate table changeset to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "duplicate table ID 0") {
			t.Fatalf("replay decode error %q missing duplicate table detail", err)
		}
	})

	t.Run("valid-record-with-row-shape-mismatch", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload, err := EncodeChangeset(&store.Changeset{
			TxID: 2,
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "players",
					Inserts:   []types.ProductValue{{types.NewUint64(2)}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected row shape mismatch to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "row shape mismatch") {
			t.Fatalf("replay decode error %q missing row shape detail", err)
		}
	})

	t.Run("valid-record-with-row-type-tag-mismatch", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload, err := EncodeChangeset(&store.Changeset{
			TxID: 2,
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "players",
					Inserts:   []types.ProductValue{{types.NewString("not-a-uint64"), types.NewString("bob")}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected row type tag mismatch to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "type tag mismatch") {
			t.Fatalf("replay decode error %q missing type tag detail", err)
		}
	})

	t.Run("valid-record-with-oversized-row-payload", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload := []byte{changesetVersion}
		payload = appendUint32(payload, 1)
		payload = appendUint32(payload, 0)
		payload = appendUint32(payload, 1)
		payload = appendUint32(payload, DefaultCommitLogOptions().MaxRowBytes+1)
		payload = appendUint32(payload, 0)
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected oversized row payload to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		var rowErr *RowTooLargeError
		if !errors.As(err, &rowErr) {
			t.Fatalf("expected RowTooLargeError, got %T (%v)", err, err)
		}
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
	})

	t.Run("valid-record-with-trailing-changeset-bytes", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload, err := EncodeChangeset(&store.Changeset{
			TxID: 2,
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "players",
					Inserts:   []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, 0xde, 0xad, 0xbe, 0xef)
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected trailing changeset bytes to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "trailing changeset bytes") {
			t.Fatalf("replay decode error %q missing trailing-bytes detail", err)
		}
	})
}

func TestOpenAndRecoverSnapshotNextIDBelowRestoredRowsFailsLoudly(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
	rewriteSnapshotNextID(t, root, 2, 0, 1)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err == nil {
		t.Fatal("expected regressed snapshot next_id to fail recovery")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "snapshot next_id 1 for table 0 is below restored next row ID 3") {
		t.Fatalf("error = %v, want explicit next_id regression detail", err)
	}
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) {
		t.Fatalf("report = %+v, want no recovered tx or resume plan", report)
	}
}

func TestOpenAndRecoverSnapshotSequenceBelowRestoredRowsFailsLoudly(t *testing.T) {
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
	committed.SetCommittedTxID(2)
	createSnapshotAt(t, NewSnapshotWriter(filepath.Join(root, "snapshots"), reg), committed, 2)
	rewriteSnapshotSequence(t, root, 2, 0, 1)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err == nil {
		t.Fatal("expected regressed snapshot sequence to fail recovery")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "snapshot sequence 1 for table 0 is below restored next sequence value 3") {
		t.Fatalf("error = %v, want explicit sequence regression detail", err)
	}
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) {
		t.Fatalf("report = %+v, want no recovered tx or resume plan", report)
	}
}

func TestOpenAndRecoverSnapshotSequenceBelowPositiveSignedRowsFailsWithNegativeRowsPresent(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoverySignedAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)
	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewInt64(-7), types.NewString("explicit-negative")}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewInt64(42), types.NewString("explicit-positive")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(43)
	committed.SetCommittedTxID(2)
	createSnapshotAt(t, NewSnapshotWriter(filepath.Join(root, "snapshots"), reg), committed, 2)
	rewriteSnapshotSequence(t, root, 2, 0, 1)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err == nil {
		t.Fatal("expected regressed signed snapshot sequence to fail recovery")
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("error = %v, want ErrSnapshot category", err)
	}
	if !strings.Contains(err.Error(), "snapshot sequence 1 for table 0 is below restored next sequence value 43") {
		t.Fatalf("error = %v, want explicit sequence regression detail", err)
	}
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) {
		t.Fatalf("report = %+v, want no recovered tx or resume plan", report)
	}
}

func TestOpenAndRecoverFallsBackWhenOffsetIndexPointsInsideSegmentHeader(t *testing.T) {
	root := t.TempDir()
	const horizon = types.TxID(512)
	const lastTx = uint64(1024)

	committed, reg := buildLargeSnapshotCommittedState(t, int(horizon))
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, horizon)

	_, entries := writeDenseReplaySegment(t, root, 1, lastTx)
	idxPath := filepath.Join(root, OffsetIndexFileName(1))
	idx := populateSparseIndex(t, idxPath, 4, []OffsetIndexEntry{
		{TxID: horizon, ByteOffset: 1},
		entries[768],
	})
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != types.TxID(lastTx) {
		t.Fatalf("maxTxID = %d, want %d", maxTxID, lastTx)
	}
	players, ok := recovered.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if players.RowCount() != int(lastTx) {
		t.Fatalf("players row count = %d, want %d", players.RowCount(), lastTx)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != types.TxID(lastTx+1) {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx %d", plan, lastTx+1)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != horizon {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, %d)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, horizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: horizon + 1, End: types.TxID(lastTx)}) {
		t.Fatalf("replayed range = %+v, want %d..%d", report.ReplayedTxRange, horizon+1, lastTx)
	}
}

func TestOpenAndRecoverOrphanOffsetIndexDoesNotStandInForMissingLog(t *testing.T) {
	t.Run("snapshot-recovers-and-resumes-at-next-tx", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
		createOrphanOffsetIndex(t, root, 3, OffsetIndexEntry{TxID: 3, ByteOffset: SegmentHeaderSize})

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatal(err)
		}
		if maxTxID != 2 {
			t.Fatalf("maxTxID = %d, want 2", maxTxID)
		}
		assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
		if report.HasDurableLog || report.DurableLogHorizon != 0 || len(report.SegmentCoverage) != 0 {
			t.Fatalf("durable log report = %+v, want no log coverage from orphan index", report)
		}
		if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
			t.Fatalf("selected snapshot report = (%v, %d), want (true, 2)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
		}
		if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
			t.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
		}
		if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
			t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
		}
	})

	t.Run("without-base-snapshot-fails-as-no-data", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		createOrphanOffsetIndex(t, root, 1, OffsetIndexEntry{TxID: 1, ByteOffset: SegmentHeaderSize})

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected orphan offset index without snapshot or log to fail")
		}
		if !errors.Is(err, ErrNoData) {
			t.Fatalf("error = %v, want ErrNoData", err)
		}
		if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
			t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
		}
		assertZeroRecoveryReport(t, report)
	})
}

func TestCreateSnapshotParentSyncFailureLeavesNoSelectableArtifacts(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	committed.SetCommittedTxID(9)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	syncErr := errors.New("parent sync failed")
	writer.syncDir = func(path string) error {
		if path == writer.baseDir {
			return syncErr
		}
		return nil
	}

	err := writer.CreateSnapshot(committed, 9)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped sync failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-parent" || completionErr.Path != writer.baseDir {
		t.Fatalf("completion error = %+v, want sync-parent on base dir", completionErr)
	}

	snapshotDir := filepath.Join(writer.baseDir, "9")
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should not exist when parent sync fails before lock creation")
	}
	for _, name := range []string{snapshotTempFileName, snapshotFileName} {
		if _, err := os.Stat(filepath.Join(snapshotDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after parent sync failure, stat err=%v", name, err)
		}
	}
	ids, listErr := ListSnapshots(writer.baseDir)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("ListSnapshots = %v, want incomplete directory to remain discoverable for read-failure reporting", ids)
	}
	_, readErr := ReadSnapshot(snapshotDir)
	if readErr == nil {
		t.Fatal("incomplete snapshot directory should not be readable")
	}
}

func TestCreateSnapshotUnlockSyncFailureFailsLoudlyAndLeavesRecoverableSnapshot(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	committed.SetCommittedTxID(9)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	snapshotDir := filepath.Join(writer.baseDir, "9")
	syncErr := errors.New("unlock sync failed")
	var snapshotDirSyncs int
	writer.syncDir = func(path string) error {
		if path == snapshotDir {
			snapshotDirSyncs++
			if snapshotDirSyncs == 2 {
				return syncErr
			}
		}
		return nil
	}

	err := writer.CreateSnapshot(committed, 9)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped sync failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-unlock" || completionErr.Path != snapshotDir {
		t.Fatalf("completion error = %+v, want sync-unlock on snapshot dir", completionErr)
	}
	if snapshotDirSyncs != 2 {
		t.Fatalf("snapshot dir sync count = %d, want 2", snapshotDirSyncs)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed before sync-unlock failure is reported")
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("snapshot should be readable after sync-unlock failure in current filesystem state: %v", err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 9 {
		t.Fatalf("maxTxID = %d, want 9", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 9 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 9)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
		t.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 10 || plan.NextTxID != 10 {
		t.Fatalf("resume plan = %+v, want fresh segment at tx 10", plan)
	}
}

func TestCreateSnapshotDirectorySyncFailureFailsLoudlyAndLeavesRecoverableSnapshot(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	committed.SetCommittedTxID(9)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	snapshotDir := filepath.Join(writer.baseDir, "9")
	syncErr := errors.New("snapshot dir sync failed")
	writer.syncDir = func(path string) error {
		if path == snapshotDir {
			return syncErr
		}
		return nil
	}

	err := writer.CreateSnapshot(committed, 9)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped sync failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-snapshot" || completionErr.Path != snapshotDir {
		t.Fatalf("completion error = %+v, want sync-snapshot on snapshot dir", completionErr)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed during sync-snapshot failure cleanup")
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("snapshot should be readable after sync-snapshot failure in current filesystem state: %v", err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 9 {
		t.Fatalf("maxTxID = %d, want 9", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 9 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 9)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
		t.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 10 || plan.NextTxID != 10 {
		t.Fatalf("resume plan = %+v, want fresh segment at tx 10", plan)
	}
}

func TestCreateSnapshotRenameAfterMoveFailureFailsLoudlyAndLeavesRecoverableSnapshot(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	committed.SetCommittedTxID(9)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	snapshotDir := filepath.Join(writer.baseDir, "9")
	finalPath := filepath.Join(snapshotDir, snapshotFileName)
	renameErr := errors.New("rename moved file then failed")
	writer.rename = func(oldPath, newPath string) error {
		if filepath.Base(oldPath) != snapshotTempFileName || newPath != finalPath {
			t.Fatalf("rename paths = (%q, %q), want temp to final snapshot", oldPath, newPath)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		return renameErr
	}

	err := writer.CreateSnapshot(committed, 9)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, renameErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped rename failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "rename" || completionErr.Path != finalPath {
		t.Fatalf("completion error = %+v, want rename on final snapshot path", completionErr)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after rename failure cleanup")
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, snapshotTempFileName)); !os.IsNotExist(err) {
		t.Fatalf("snapshot temp should not remain after moved rename failure, stat err=%v", err)
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("snapshot should be readable after post-move rename failure in current filesystem state: %v", err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 9 {
		t.Fatalf("maxTxID = %d, want 9", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 9 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 9)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
		t.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 10 || plan.NextTxID != 10 {
		t.Fatalf("resume plan = %+v, want fresh segment at tx 10", plan)
	}
}

func TestCreateSnapshotRemoveLockFailureLeavesLockedSnapshotIgnoredAndCompleteLogRecovers(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
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
	committed.SetCommittedTxID(2)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	removeErr := errors.New("remove lock failed")
	writer.removeLock = func(string) error {
		return removeErr
	}

	err := writer.CreateSnapshot(committed, 2)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, removeErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped remove-lock failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "remove-lock" || filepath.Base(completionErr.Path) != ".lock" {
		t.Fatalf("completion error = %+v, want remove-lock on lock path", completionErr)
	}
	snapshotDir := filepath.Join(writer.baseDir, "2")
	if !HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should remain after remove-lock failure")
	}
	if _, err := ReadSnapshot(snapshotDir); err != nil {
		t.Fatalf("final snapshot payload should be readable even while locked: %v", err)
	}

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	if report.HasSelectedSnapshot || len(report.SkippedSnapshots) != 0 {
		t.Fatalf("snapshot report = selected(%v, %d) skipped=%+v, want locked candidate ignored", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, report.SkippedSnapshots)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 3 {
		t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
		t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
	}
}

func TestCreateSnapshotTempWriteFailureLeavesNoSelectableSnapshotAndCompleteLogRecovers(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
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
	committed.SetCommittedTxID(2)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	snapshotDir := filepath.Join(writer.baseDir, "2")
	tmpPath := filepath.Join(snapshotDir, snapshotTempFileName)
	writeErr := errors.New("temp write failed")
	writer.openTemp = func(path string) (snapshotTempFile, error) {
		if path != tmpPath {
			t.Fatalf("open temp path = %q, want %q", path, tmpPath)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		return &faultingSnapshotTempFile{File: file, writeErr: writeErr}, nil
	}

	err := writer.CreateSnapshot(committed, 2)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, writeErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped write failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "write-temp" || completionErr.Path != tmpPath {
		t.Fatalf("completion error = %+v, want write-temp on temp path", completionErr)
	}
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should be removed after temp write failure")
	}
	for _, name := range []string{snapshotTempFileName, snapshotFileName} {
		if _, err := os.Stat(filepath.Join(snapshotDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after temp write failure, stat err=%v", name, err)
		}
	}

	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
	if report.HasSelectedSnapshot {
		t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
		t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
	}
}

func TestOpenAndRecoverAfterCompactionDeletesCoveredLogPrefix(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 3, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	covered := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	tail := writeReplaySegment(t, root, 4,
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
		replayRecord{txID: 5, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("eve")}}},
	)

	if err := RunCompaction(root, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(covered); !os.IsNotExist(err) {
		t.Fatalf("covered segment should be removed after compaction, stat err=%v", err)
	}
	if _, err := os.Stat(tail); err != nil {
		t.Fatalf("tail segment should remain after compaction: %v", err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 5 {
		t.Fatalf("maxTxID = %d, want 5", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol", 4: "dave", 5: "eve"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 3 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 3)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 5 {
		t.Fatalf("durable log report = (%v, %d), want (true, 5)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 4, End: 5}) {
		t.Fatalf("replayed range = %+v, want 4..5", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 4 || plan.NextTxID != 6 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 4 at tx 6", plan)
	}
}

func TestOpenAndRecoverAfterCompactionSyncFailureUsesSnapshotAndRemainingTail(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 3, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	covered := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	tail := writeReplaySegment(t, root, 4,
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
		replayRecord{txID: 5, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("eve")}}},
	)

	syncErr := errors.New("compaction sync failed")
	originalSyncDir := syncDir
	syncDir = func(path string) error {
		if path != root {
			t.Fatalf("syncDir path = %q, want %q", path, root)
		}
		return syncErr
	}
	err := RunCompaction(root, 3)
	syncDir = originalSyncDir
	defer func() { syncDir = originalSyncDir }()
	if !errors.Is(err, syncErr) {
		t.Fatalf("compaction error = %v, want wrapped sync failure", err)
	}
	if _, err := os.Stat(covered); !os.IsNotExist(err) {
		t.Fatalf("covered segment should be removed before sync failure is reported, stat err=%v", err)
	}
	if _, err := os.Stat(tail); err != nil {
		t.Fatalf("tail segment should remain after compaction sync failure: %v", err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 5 {
		t.Fatalf("maxTxID = %d, want 5", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol", 4: "dave", 5: "eve"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 3 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 3)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 5 {
		t.Fatalf("durable log report = (%v, %d), want (true, 5)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 4, End: 5}) {
		t.Fatalf("replayed range = %+v, want 4..5", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 4 || plan.NextTxID != 6 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 4 at tx 6", plan)
	}
}

func TestOpenAndRecoverIgnoresCoveredOrphanIndexesAfterCompactedPrefix(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 6, map[uint64]string{
		1: "alice",
		2: "bob",
		3: "carol",
		4: "dave",
		5: "eve",
		6: "frank",
	})
	createOrphanOffsetIndex(t, root, 1, OffsetIndexEntry{TxID: 1, ByteOffset: SegmentHeaderSize})
	createOrphanOffsetIndex(t, root, 4, OffsetIndexEntry{TxID: 4, ByteOffset: SegmentHeaderSize})
	writeReplaySegment(t, root, 7,
		replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(7), types.NewString("grace")}}},
		replayRecord{txID: 8, inserts: []types.ProductValue{{types.NewUint64(8), types.NewString("heidi")}}},
	)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 8 {
		t.Fatalf("maxTxID = %d, want 8", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{
		1: "alice",
		2: "bob",
		3: "carol",
		4: "dave",
		5: "eve",
		6: "frank",
		7: "grace",
		8: "heidi",
	})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 6 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 6)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 8 {
		t.Fatalf("durable log report = (%v, %d), want (true, 8)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 7 || report.SegmentCoverage[0].MaxTxID != 8 {
		t.Fatalf("segment coverage = %+v, want only live tail segment 7..8", report.SegmentCoverage)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 7, End: 8}) {
		t.Fatalf("replayed range = %+v, want 7..8", report.ReplayedTxRange)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 7 || plan.NextTxID != 9 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 7 at tx 9", plan)
	}
}

func assertNoRecoveredStateAfterReplayFault(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, durableHorizon types.TxID) {
	t.Helper()
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != durableHorizon {
		t.Fatalf("durable log report = (%v, %d), want (true, %d)", report.HasDurableLog, report.DurableLogHorizon, durableHorizon)
	}
	if report.HasSelectedSnapshot || report.RecoveredTxID != 0 || report.ReplayedTxRange != (RecoveryTxIDRange{}) || report.ResumePlan != (RecoveryResumePlan{}) {
		t.Fatalf("report = %+v, want no selected snapshot, recovered tx, replay range, or resume plan", report)
	}
}

func appendUint32(dst []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(dst, buf[:]...)
}

func writeFaultSnapshot(t *testing.T, root string, reg schema.SchemaRegistry, txID types.TxID, rows map[uint64]string) {
	t.Helper()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	for id, name := range rows {
		if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(id), types.NewString(name)}); err != nil {
			t.Fatal(err)
		}
	}
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, txID)
}

func createHeaderOnlySegment(t *testing.T, root string, startTxID uint64) {
	t.Helper()
	seg, err := CreateSegment(root, startTxID)
	if err != nil {
		t.Fatal(err)
	}
	if err := seg.Close(); err != nil {
		t.Fatal(err)
	}
}

func createMissingSnapshotCandidate(t *testing.T, root string, txID types.TxID) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "snapshots", txIDString(uint64(txID))), 0o755); err != nil {
		t.Fatal(err)
	}
}

func markSnapshotLocked(t *testing.T, root string, txID types.TxID) {
	t.Helper()
	if err := CreateLockFile(filepath.Join(root, "snapshots", txIDString(uint64(txID)))); err != nil {
		t.Fatal(err)
	}
}

func markSnapshotTemp(t *testing.T, root string, txID types.TxID) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "snapshots", txIDString(uint64(txID)), snapshotTempFileName), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func truncateSnapshotFile(t *testing.T, root string, txID types.TxID, size int64) {
	t.Helper()
	if err := os.Truncate(filepath.Join(root, "snapshots", txIDString(uint64(txID)), snapshotFileName), size); err != nil {
		t.Fatal(err)
	}
}

func createOrphanOffsetIndex(t *testing.T, root string, startTx uint64, entries ...OffsetIndexEntry) {
	t.Helper()
	idx, err := CreateOffsetIndex(filepath.Join(root, OffsetIndexFileName(startTx)), uint64(len(entries)+1))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := idx.Append(entry.TxID, entry.ByteOffset); err != nil {
			_ = idx.Close()
			t.Fatal(err)
		}
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
}

func rewriteSnapshotNextID(t *testing.T, root string, txID types.TxID, tableID schema.TableID, nextID uint64) {
	t.Helper()
	rewriteSnapshotUint64Map(t, root, txID, tableID, nextID, false)
}

func rewriteSnapshotSequence(t *testing.T, root string, txID types.TxID, tableID schema.TableID, sequence uint64) {
	t.Helper()
	rewriteSnapshotUint64Map(t, root, txID, tableID, sequence, true)
}

func rewriteSnapshotUint64Map(t *testing.T, root string, txID types.TxID, tableID schema.TableID, value uint64, sequence bool) {
	t.Helper()
	path := filepath.Join(root, "snapshots", txIDString(uint64(txID)), snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < SnapshotHeaderSize {
		t.Fatalf("snapshot %s is too short: %d bytes", path, len(data))
	}
	body := data[SnapshotHeaderSize:]
	offset := 0
	readUint32 := func(section string) uint32 {
		t.Helper()
		if offset+4 > len(body) {
			t.Fatalf("snapshot %s ended before %s at body offset %d", path, section, offset)
		}
		v := binary.LittleEndian.Uint32(body[offset : offset+4])
		offset += 4
		return v
	}

	schemaLen := int(readUint32("schema length"))
	if offset+schemaLen > len(body) {
		t.Fatalf("snapshot %s has invalid schema length %d at body offset %d", path, schemaLen, offset)
	}
	offset += schemaLen
	sequenceCount := int(readUint32("sequence count"))
	if sequence {
		rewriteSnapshotUint64MapEntry(t, path, data, body, &offset, sequenceCount, tableID, value, "sequence")
		return
	}
	offset += sequenceCount * 12
	if offset > len(body) {
		t.Fatalf("snapshot %s ended inside sequence entries", path)
	}
	nextIDCount := int(readUint32("next_id count"))
	rewriteSnapshotUint64MapEntry(t, path, data, body, &offset, nextIDCount, tableID, value, "next_id")
}

func rewriteSnapshotUint64MapEntry(t *testing.T, path string, data []byte, body []byte, offset *int, count int, tableID schema.TableID, value uint64, section string) {
	t.Helper()
	for range count {
		if *offset+12 > len(body) {
			t.Fatalf("snapshot %s ended inside %s entries", path, section)
		}
		gotTableID := binary.LittleEndian.Uint32(body[*offset : *offset+4])
		if gotTableID == uint32(tableID) {
			binary.LittleEndian.PutUint64(body[*offset+4:*offset+12], value)
			hash := ComputeSnapshotHash(body)
			copy(data[20:52], hash[:])
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		*offset += 12
	}
	t.Fatalf("snapshot %s has no %s entry for table %d", path, section, tableID)
}

func appendZeroTail(t *testing.T, path string, byteCount int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, byteCount)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertSkippedSnapshot(t *testing.T, report RecoveryReport, txID types.TxID, reason SnapshotSkipReason) {
	t.Helper()
	if len(report.SkippedSnapshots) != 1 {
		t.Fatalf("skipped snapshots = %+v, want one skipped snapshot", report.SkippedSnapshots)
	}
	skipped := report.SkippedSnapshots[0]
	if skipped.TxID != txID || skipped.Reason != reason {
		t.Fatalf("skipped snapshot = %+v, want tx %d %s with detail", skipped, txID, reason)
	}
	if reason == SnapshotSkipReadFailed && skipped.Detail == "" {
		t.Fatalf("skipped snapshot = %+v, want read failure detail", skipped)
	}
}

func assertZeroRecoveryReport(t *testing.T, report RecoveryReport) {
	t.Helper()
	if report.HasSelectedSnapshot ||
		report.SelectedSnapshotTxID != 0 ||
		report.HasDurableLog ||
		report.DurableLogHorizon != 0 ||
		report.ReplayedTxRange != (RecoveryTxIDRange{}) ||
		report.RecoveredTxID != 0 ||
		report.ResumePlan != (RecoveryResumePlan{}) ||
		len(report.SkippedSnapshots) != 0 ||
		len(report.DamagedTailSegments) != 0 ||
		len(report.SegmentCoverage) != 0 {
		t.Fatalf("recovery report = %+v, want zero report", report)
	}
}
