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

	ErrOffsetIndexKeyNotFound = errors.New("commitlog: offset index key not found")
	ErrOffsetIndexFull        = errors.New("commitlog: offset index full")
	ErrOffsetIndexCorrupt     = errors.New("commitlog: offset index corrupt")
)

// Category sentinels group the errors above by admission seam. A caller can
// test category membership via errors.Is(err, ErrTraversal) etc. without
// enumerating every leaf. Leaf identity is preserved: errors.Is(err,
// ErrBadMagic), errors.As(err, &*BadVersionError{}) etc. continue to match
// after category wrapping.
var (
	// ErrTraversal categorizes iterator-time errors (record decode, segment
	// scan mid-record, changeset decode).
	ErrTraversal = errors.New("commitlog: traversal error")

	// ErrOpen categorizes errors surfaced while opening a segment or
	// enumerating segments, including commitlog-specific header validation
	// and recovery planning.
	ErrOpen = errors.New("commitlog: open error")

	// ErrDurability categorizes fatal durability-worker failures. Present
	// in the Unwrap chain of the value panicked by the worker.
	ErrDurability = errors.New("commitlog: durability error")

	// ErrSnapshot categorizes snapshot read/write/selection errors.
	ErrSnapshot = errors.New("commitlog: snapshot error")

	// ErrIndex categorizes per-segment offset index errors (read, write,
	// reopen).
	ErrIndex = errors.New("commitlog: offset index error")
)

// wrapCategory wraps leaf in an error whose Unwrap chain contains both cat
// and leaf. errors.Is matches both. Error() returns leaf.Error() unchanged so
// surface text, logs, and existing text-equality assertions do not churn.
func wrapCategory(cat, leaf error) error {
	if leaf == nil {
		return nil
	}
	if cat == nil {
		return leaf
	}
	return &categorizedError{cat: cat, leaf: leaf}
}

type categorizedError struct {
	cat  error
	leaf error
}

func (e *categorizedError) Error() string   { return e.leaf.Error() }
func (e *categorizedError) Unwrap() []error { return []error{e.leaf, e.cat} }

type OffsetIndexNonMonotonicError struct {
	Last uint64
	Got  uint64
}

func (e *OffsetIndexNonMonotonicError) Error() string {
	return fmt.Sprintf("commitlog: offset index non-monotonic: last=%d got=%d", e.Last, e.Got)
}

func (e *OffsetIndexNonMonotonicError) Is(target error) bool {
	return target == ErrIndex
}

type BadVersionError struct{ Got byte }

type ErrBadVersion = BadVersionError

func (e *BadVersionError) Error() string {
	return fmt.Sprintf("commitlog: bad version %d", e.Got)
}

func (e *BadVersionError) Is(target error) bool {
	return target == ErrOpen
}

type UnknownRecordTypeError struct{ Type byte }

type ErrUnknownRecordType = UnknownRecordTypeError

func (e *UnknownRecordTypeError) Error() string {
	return fmt.Sprintf("commitlog: unknown record type %d", e.Type)
}

func (e *UnknownRecordTypeError) Is(target error) bool {
	return target == ErrTraversal
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

func (e *ChecksumMismatchError) Is(target error) bool {
	return target == ErrTraversal
}

type RecordTooLargeError struct {
	Size uint32
	Max  uint32
}

type ErrRecordTooLarge = RecordTooLargeError

func (e *RecordTooLargeError) Error() string {
	return fmt.Sprintf("commitlog: record payload %d exceeds max %d", e.Size, e.Max)
}

func (e *RecordTooLargeError) Is(target error) bool {
	return target == ErrTraversal
}

type RowTooLargeError struct {
	Size uint32
	Max  uint32
}

func (e *RowTooLargeError) Error() string {
	return fmt.Sprintf("commitlog: row payload %d exceeds max %d", e.Size, e.Max)
}

func (e *RowTooLargeError) Is(target error) bool {
	return target == ErrTraversal
}

type SnapshotHashMismatchError struct {
	Expected [32]byte
	Got      [32]byte
}

func (e *SnapshotHashMismatchError) Error() string {
	return fmt.Sprintf("commitlog: snapshot hash mismatch: expected %x, got %x", e.Expected[:8], e.Got[:8])
}

func (e *SnapshotHashMismatchError) Is(target error) bool {
	return target == ErrSnapshot
}

type HistoryGapError struct {
	Expected uint64
	Got      uint64
	Segment  string
}

func (e *HistoryGapError) Error() string {
	return fmt.Sprintf("commitlog: history gap: expected tx %d, got %d in segment %s", e.Expected, e.Got, e.Segment)
}

func (e *HistoryGapError) Is(target error) bool {
	return target == ErrOpen
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

func (e *SchemaMismatchError) Is(target error) bool {
	return target == ErrSnapshot
}
