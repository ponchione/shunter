package commitlog

import (
	"errors"
	"fmt"

	"github.com/ponchione/shunter/types"
)

var (
	ErrBadMagic            error = badMagicError{}
	ErrBadFlags            error = badFlagsError{}
	ErrTruncatedRecord     error = truncatedRecordError{}
	ErrDurabilityFailed    error = durabilityFailedError{}
	ErrSnapshotIncomplete  error = snapshotIncompleteError{}
	ErrSnapshotInProgress  error = snapshotInProgressError{}
	ErrMissingBaseSnapshot error = missingBaseSnapshotError{}
	ErrNoData              error = noDataError{}
	ErrUnknownFsyncMode    error = unknownFsyncModeError{}

	ErrOffsetIndexKeyNotFound error = offsetIndexKeyNotFoundError{}
	ErrOffsetIndexFull        error = offsetIndexFullError{}
	ErrOffsetIndexCorrupt     error = offsetIndexCorruptError{}
)

// Category sentinels group the errors above by admission seam. A caller can
// test category membership via errors.Is(err, ErrTraversal) etc. without
// enumerating every leaf. Converted singleton sentinels now carry their own
// category via Is. For the two historically split leaves (ErrBadFlags and
// ErrTruncatedRecord), the singleton category is ErrTraversal; open-time sites
// still return the same leaf text/identity, but callers wanting the old split
// must check the leaf directly.
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

type badMagicError struct{}

func (badMagicError) Error() string        { return "commitlog: bad magic bytes" }
func (badMagicError) Is(target error) bool { return target == ErrOpen }

type badFlagsError struct{}

func (badFlagsError) Error() string        { return "commitlog: non-zero flags" }
func (badFlagsError) Is(target error) bool { return target == ErrTraversal }

type truncatedRecordError struct{}

func (truncatedRecordError) Error() string        { return "commitlog: truncated record" }
func (truncatedRecordError) Is(target error) bool { return target == ErrTraversal }

type durabilityFailedError struct{}

func (durabilityFailedError) Error() string        { return "commitlog: durability worker failed" }
func (durabilityFailedError) Is(target error) bool { return target == ErrDurability }

type snapshotIncompleteError struct{}

func (snapshotIncompleteError) Error() string {
	return "commitlog: snapshot has lock file (incomplete)"
}
func (snapshotIncompleteError) Is(target error) bool { return target == ErrSnapshot }

type snapshotInProgressError struct{}

func (snapshotInProgressError) Error() string {
	return "commitlog: snapshot write already in progress"
}
func (snapshotInProgressError) Is(target error) bool { return target == ErrSnapshot }

type missingBaseSnapshotError struct{}

func (missingBaseSnapshotError) Error() string {
	return "commitlog: no valid base snapshot for log replay"
}
func (missingBaseSnapshotError) Is(target error) bool { return target == ErrOpen }

type noDataError struct{}

func (noDataError) Error() string        { return "commitlog: no snapshot or log data found" }
func (noDataError) Is(target error) bool { return target == ErrOpen }

type unknownFsyncModeError struct{}

func (unknownFsyncModeError) Error() string        { return "commitlog: unknown fsync mode" }
func (unknownFsyncModeError) Is(target error) bool { return target == ErrOpen }

type offsetIndexKeyNotFoundError struct{}

func (offsetIndexKeyNotFoundError) Error() string {
	return "commitlog: offset index key not found"
}
func (offsetIndexKeyNotFoundError) Is(target error) bool { return target == ErrIndex }

type offsetIndexFullError struct{}

func (offsetIndexFullError) Error() string        { return "commitlog: offset index full" }
func (offsetIndexFullError) Is(target error) bool { return target == ErrIndex }

type offsetIndexCorruptError struct{}

func (offsetIndexCorruptError) Error() string        { return "commitlog: offset index corrupt" }
func (offsetIndexCorruptError) Is(target error) bool { return target == ErrIndex }

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

// SnapshotHorizonMismatchError reports a snapshot write whose requested txID
// does not match the committed state's recorded horizon.
type SnapshotHorizonMismatchError struct {
	SnapshotTxID  types.TxID // SnapshotTxID is the txID requested for the snapshot.
	CommittedTxID types.TxID // CommittedTxID is the horizon recorded on the source state.
}

func (e *SnapshotHorizonMismatchError) Error() string {
	return fmt.Sprintf("commitlog: snapshot txID %d does not match committed txID %d", e.SnapshotTxID, e.CommittedTxID)
}

func (e *SnapshotHorizonMismatchError) Is(target error) bool {
	return target == ErrSnapshot
}

// SnapshotCompletionError reports a filesystem failure while making a snapshot
// selectable after its body has been written.
type SnapshotCompletionError struct {
	Phase string // Phase names the completion step that failed.
	Path  string // Path names the affected artifact or directory when available.
	Err   error  // Err is the wrapped underlying filesystem error.
}

func (e *SnapshotCompletionError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("commitlog: snapshot %s: %v", e.Phase, e.Err)
	}
	return fmt.Sprintf("commitlog: snapshot %s %s: %v", e.Phase, e.Path, e.Err)
}

func (e *SnapshotCompletionError) Unwrap() error {
	return e.Err
}

func (e *SnapshotCompletionError) Is(target error) bool {
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
