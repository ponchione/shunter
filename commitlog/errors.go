package commitlog

import (
	"errors"
	"fmt"
)

var (
	ErrBadMagic            = errors.New("commitlog: bad magic bytes")
	ErrBadFlags            = errors.New("commitlog: non-zero flags")
	ErrTruncatedRecord     = errors.New("commitlog: truncated record")
	ErrDurabilityFailed    = errors.New("commitlog: durability worker failed")
	ErrSnapshotIncomplete  = errors.New("commitlog: snapshot has lock file (incomplete)")
	ErrSnapshotInProgress  = errors.New("commitlog: snapshot write already in progress")
	ErrMissingBaseSnapshot = errors.New("commitlog: no valid base snapshot for log replay")
	ErrNoData              = errors.New("commitlog: no snapshot or log data found")
	ErrUnknownFsyncMode    = errors.New("commitlog: unknown fsync mode")
)

type BadVersionError struct{ Got byte }

type ErrBadVersion = BadVersionError

func (e *BadVersionError) Error() string {
	return fmt.Sprintf("commitlog: bad version %d", e.Got)
}

type UnknownRecordTypeError struct{ Type byte }

type ErrUnknownRecordType = UnknownRecordTypeError

func (e *UnknownRecordTypeError) Error() string {
	return fmt.Sprintf("commitlog: unknown record type %d", e.Type)
}

type ChecksumMismatchError struct {
	Expected uint32
	Got      uint32
	TxID     uint64
}

type ErrChecksumMismatch = ChecksumMismatchError

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("commitlog: checksum mismatch on tx %d: expected %08x, got %08x", e.TxID, e.Expected, e.Got)
}

type RecordTooLargeError struct {
	Size uint32
	Max  uint32
}

type ErrRecordTooLarge = RecordTooLargeError

func (e *RecordTooLargeError) Error() string {
	return fmt.Sprintf("commitlog: record payload %d exceeds max %d", e.Size, e.Max)
}

type RowTooLargeError struct {
	Size uint32
	Max  uint32
}

func (e *RowTooLargeError) Error() string {
	return fmt.Sprintf("commitlog: row payload %d exceeds max %d", e.Size, e.Max)
}

type SnapshotHashMismatchError struct {
	Expected [32]byte
	Got      [32]byte
}

func (e *SnapshotHashMismatchError) Error() string {
	return fmt.Sprintf("commitlog: snapshot hash mismatch: expected %x, got %x", e.Expected[:8], e.Got[:8])
}

type HistoryGapError struct {
	Expected uint64
	Got      uint64
	Segment  string
}

func (e *HistoryGapError) Error() string {
	return fmt.Sprintf("commitlog: history gap: expected tx %d, got %d in segment %s", e.Expected, e.Got, e.Segment)
}

type SchemaMismatchError struct {
	Detail string
	Cause  error
}

func (e *SchemaMismatchError) Error() string {
	return fmt.Sprintf("commitlog: schema mismatch: %s", e.Detail)
}

func (e *SchemaMismatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
