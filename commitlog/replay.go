package commitlog

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func shouldStopAfterRecord(segment SegmentInfo, txID types.TxID) bool {
	return segment.AppendMode == AppendByFreshNextSegment && txID >= segment.LastTx
}

// replayDecodeHook is a test-only instrumentation point fired once per record
// decoded by ReplayLog's segment read loop. Always nil in production.
var replayDecodeHook func(*Record)

// seekReplayReaderToHorizon positions reader past the resume horizon using
// the per-segment offset index when available. The index is advisory: any
// error (open, lookup, seek) degrades to a linear scan from the segment
// header. Returns without error in all non-fatal paths.
func seekReplayReaderToHorizon(reader *SegmentReader, segmentPath string, startTx, fromTxID types.TxID) {
	if fromTxID < startTx {
		return
	}
	dir := filepath.Dir(segmentPath)
	idxPath := filepath.Join(dir, OffsetIndexFileName(uint64(startTx)))
	if _, err := os.Stat(idxPath); err != nil {
		return
	}
	idx, err := OpenOffsetIndex(idxPath)
	if err != nil {
		log.Printf("commitlog: replay: opening offset index %s failed, falling back to linear scan: %v", idxPath, err)
		return
	}
	defer idx.Close()
	if err := reader.SeekToTxID(fromTxID+1, idx); err != nil {
		log.Printf("commitlog: replay: seek via offset index %s failed, falling back to linear scan: %v", idxPath, err)
		if _, seekErr := reader.file.Seek(SegmentHeaderSize, io.SeekStart); seekErr != nil {
			log.Printf("commitlog: replay: resetting segment reader after index seek failure failed for %s: %v", segmentPath, seekErr)
		}
		reader.lastTx = 0
	}
}

func ReplayLog(committed *store.CommittedState, segments []SegmentInfo, fromTxID types.TxID, reg schema.SchemaRegistry) (types.TxID, error) {
	maxAppliedTxID := fromTxID

	for _, segment := range segments {
		if isEmptyDamagedTail(segment) {
			continue
		}
		if segment.LastTx <= fromTxID {
			continue
		}

		reader, err := OpenSegment(segment.Path)
		if err != nil {
			return maxAppliedTxID, fmt.Errorf("commitlog: replay open segment %s: %w", segment.Path, err)
		}

		if segment.StartTx <= fromTxID {
			seekReplayReaderToHorizon(reader, segment.Path, segment.StartTx, fromTxID)
		}

		for {
			record, err := reader.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				closeErr := reader.Close()
				if closeErr != nil {
					return maxAppliedTxID, fmt.Errorf("commitlog: replay read segment %s: %w (close error: %v)", segment.Path, err, closeErr)
				}
				return maxAppliedTxID, fmt.Errorf("commitlog: replay read segment %s: %w", segment.Path, err)
			}
			if replayDecodeHook != nil {
				replayDecodeHook(record)
			}
			txID := types.TxID(record.TxID)
			if txID <= fromTxID {
				if shouldStopAfterRecord(segment, txID) {
					break
				}
				continue
			}

			changeset, err := DecodeChangeset(record.Payload, reg)
			if err != nil {
				closeErr := reader.Close()
				if closeErr != nil {
					return maxAppliedTxID, fmt.Errorf("commitlog: replay decode tx %d from segment %s: %w (close error: %v)", record.TxID, segment.Path, err, closeErr)
				}
				return maxAppliedTxID, fmt.Errorf("commitlog: replay decode tx %d from segment %s: %w", record.TxID, segment.Path, err)
			}
			if err := store.ApplyChangeset(committed, changeset); err != nil {
				closeErr := reader.Close()
				if closeErr != nil {
					return maxAppliedTxID, fmt.Errorf("commitlog: replay apply tx %d from segment %s: %w (close error: %v)", record.TxID, segment.Path, err, closeErr)
				}
				return maxAppliedTxID, fmt.Errorf("commitlog: replay apply tx %d from segment %s: %w", record.TxID, segment.Path, err)
			}
			if txID > maxAppliedTxID {
				maxAppliedTxID = txID
			}
			if shouldStopAfterRecord(segment, txID) {
				break
			}
		}

		if err := reader.Close(); err != nil {
			return maxAppliedTxID, fmt.Errorf("commitlog: replay close segment %s: %w", segment.Path, err)
		}
	}

	return maxAppliedTxID, nil
}
