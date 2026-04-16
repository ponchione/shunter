package commitlog

import (
	"errors"
	"fmt"
	"io"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func ReplayLog(committed *store.CommittedState, segments []SegmentInfo, fromTxID types.TxID, reg schema.SchemaRegistry) (types.TxID, error) {
	maxAppliedTxID := fromTxID

	for _, segment := range segments {
		reader, err := OpenSegment(segment.Path)
		if err != nil {
			return maxAppliedTxID, fmt.Errorf("commitlog: replay open segment %s: %w", segment.Path, err)
		}

		for {
			record, err := reader.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				if segment.AppendMode == AppendByFreshNextSegment && maxAppliedTxID >= segment.LastTx {
					break
				}
				closeErr := reader.Close()
				if closeErr != nil {
					return maxAppliedTxID, fmt.Errorf("commitlog: replay read segment %s: %w (close error: %v)", segment.Path, err, closeErr)
				}
				return maxAppliedTxID, fmt.Errorf("commitlog: replay read segment %s: %w", segment.Path, err)
			}
			if types.TxID(record.TxID) <= fromTxID {
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
			if txID := types.TxID(record.TxID); txID > maxAppliedTxID {
				maxAppliedTxID = txID
			}
		}

		if err := reader.Close(); err != nil {
			return maxAppliedTxID, fmt.Errorf("commitlog: replay close segment %s: %w", segment.Path, err)
		}
	}

	return maxAppliedTxID, nil
}
