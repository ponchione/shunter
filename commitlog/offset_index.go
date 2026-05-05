package commitlog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ponchione/shunter/types"
)

// OffsetIndexEntrySize is the wire size of one (txID, byteOffset) record.
const OffsetIndexEntrySize = 16

// OffsetIndexFileName returns the sidecar filename for a segment whose
// starting TxID is startTxID.
func OffsetIndexFileName(startTxID uint64) string {
	return fmt.Sprintf("%020d.idx", startTxID)
}

const (
	offsetIndexKeyOff = 0
	offsetIndexValOff = 8

	maxOffsetIndexCap = uint64((1<<63 - 1) / OffsetIndexEntrySize)
)

// OffsetIndexEntry is one (tx id, segment byte offset) pair.
type OffsetIndexEntry struct {
	TxID       types.TxID
	ByteOffset uint64
}

// OffsetIndexMut is a writable, pre-allocated, sparse per-segment offset
// index. On-disk layout: cap * 16 bytes, each entry two little-endian uint64
// (key, value). Key `0` is the reserved empty-slot sentinel; appends with
// txID <= lastKey (including 0) are rejected as non-monotonic. Mutable reopen
// clears any ignored tail so later appends cannot resurrect stale entries.
type OffsetIndexMut struct {
	f          *os.File
	cap        uint64
	numEntries uint64
	lastKey    uint64
}

// CreateOffsetIndex creates a new index file preallocated to cap entries.
// It fails if path already exists. Every slot is zero-initialised, which the
// sentinel rule treats as absent.
func CreateOffsetIndex(path string, cap uint64) (*OffsetIndexMut, error) {
	size, err := offsetIndexFileSize(cap)
	if err != nil {
		return nil, err
	}
	if err := requireCreatableOffsetIndexPath(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return &OffsetIndexMut{f: f, cap: cap}, nil
}

// OpenOffsetIndexMut opens an existing index file for append. Scans the
// leading valid prefix (until first zero-key or non-monotonic key), clears the
// ignored tail, and leaves the file at the requested capacity for append.
func OpenOffsetIndexMut(path string, cap uint64) (*OffsetIndexMut, error) {
	want, err := offsetIndexFileSize(cap)
	if err != nil {
		return nil, err
	}
	if err := requireRegularOffsetIndexPath(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() != want {
		if err := f.Truncate(want); err != nil {
			f.Close()
			return nil, err
		}
	}
	n, last, err := scanOffsetIndexPrefix(f, cap)
	if err != nil {
		f.Close()
		return nil, err
	}
	if n < cap {
		if err := f.Truncate(int64(n * OffsetIndexEntrySize)); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Truncate(want); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &OffsetIndexMut{f: f, cap: cap, numEntries: n, lastKey: last}, nil
}

func requireCreatableOffsetIndexPath(path string) error {
	return requireCreatableRegularFilePath(path, ErrOpen, "offset index file")
}

func requireRegularOffsetIndexPath(path string) error {
	return requireRegularFilePath(path, ErrOpen, "offset index file")
}

func offsetIndexFileSize(cap uint64) (int64, error) {
	if cap == 0 {
		return 0, fmt.Errorf("commitlog: offset index cap must be > 0")
	}
	if cap > maxOffsetIndexCap {
		return 0, fmt.Errorf("commitlog: offset index cap %d too large", cap)
	}
	return int64(cap * OffsetIndexEntrySize), nil
}

// Append writes (txID, byteOffset) as the next entry.
//
// Errors:
//   - *OffsetIndexNonMonotonicError when txID == 0 or txID <= lastKey.
//   - ErrOffsetIndexFull when the index is at capacity.
func (o *OffsetIndexMut) Append(txID types.TxID, byteOffset uint64) error {
	key := uint64(txID)
	if o.numEntries >= o.cap {
		return ErrOffsetIndexFull
	}
	if key == 0 || key <= o.lastKey {
		return &OffsetIndexNonMonotonicError{Last: o.lastKey, Got: key}
	}
	var buf [OffsetIndexEntrySize]byte
	binary.LittleEndian.PutUint64(buf[offsetIndexKeyOff:], key)
	binary.LittleEndian.PutUint64(buf[offsetIndexValOff:], byteOffset)
	if err := writeAtFull(o.f, buf[:], int64(o.numEntries*OffsetIndexEntrySize)); err != nil {
		return err
	}
	o.numEntries++
	o.lastKey = key
	return nil
}

// KeyLookup returns the entry with the largest key <= target. Returns
// ErrOffsetIndexKeyNotFound when target is below the first key or the index
// is empty.
func (o *OffsetIndexMut) KeyLookup(target types.TxID) (types.TxID, uint64, error) {
	return offsetIndexLookup(o.f, o.numEntries, target)
}

// Entries returns a snapshot of every valid entry in ascending key order.
func (o *OffsetIndexMut) Entries() ([]OffsetIndexEntry, error) {
	return readOffsetIndexEntries(o.f, o.numEntries)
}

// NumEntries returns the count of valid entries.
func (o *OffsetIndexMut) NumEntries() uint64 {
	return o.numEntries
}

// Cap returns the file's preallocated entry capacity.
func (o *OffsetIndexMut) Cap() uint64 {
	return o.cap
}

// Truncate drops all entries with key >= target, zero-filling the tail.
// A target at or below the first entry empties the index.
func (o *OffsetIndexMut) Truncate(target types.TxID) error {
	key := uint64(target)
	n := o.numEntries
	// Upper-bound binary search: lo is the count of entries with key < target.
	lo, hi := uint64(0), n
	for lo < hi {
		mid := (lo + hi) / 2
		midKey, _, err := readOffsetIndexEntryAt(o.f, mid)
		if err != nil {
			return err
		}
		if midKey < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	drop := lo
	if drop >= n {
		return nil
	}
	zero := make([]byte, OffsetIndexEntrySize*(n-drop))
	if err := writeAtFull(o.f, zero, int64(drop*OffsetIndexEntrySize)); err != nil {
		return err
	}
	o.numEntries = drop
	if drop == 0 {
		o.lastKey = 0
	} else {
		last, _, err := readOffsetIndexEntryAt(o.f, drop-1)
		if err != nil {
			return err
		}
		o.lastKey = last
	}
	return nil
}

// Sync fsyncs the backing file so index durability can be ordered against
// segment durability from above.
func (o *OffsetIndexMut) Sync() error {
	if o == nil || o.f == nil {
		return nil
	}
	return o.f.Sync()
}

// Close releases the file handle without fsyncing; callers that need
// durability must Sync explicitly.
func (o *OffsetIndexMut) Close() error {
	if o == nil || o.f == nil {
		return nil
	}
	err := o.f.Close()
	o.f = nil
	return err
}

// OffsetIndex is a read-only view over an on-disk sparse offset index.
type OffsetIndex struct {
	f          *os.File
	numEntries uint64
}

// OpenOffsetIndex opens an existing index file read-only.
func OpenOffsetIndex(path string) (*OffsetIndex, error) {
	if err := requireRegularOffsetIndexPath(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	cap := uint64(info.Size() / OffsetIndexEntrySize)
	n, _, err := scanOffsetIndexPrefix(f, cap)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &OffsetIndex{f: f, numEntries: n}, nil
}

// KeyLookup returns the entry with the largest key <= target. Returns
// ErrOffsetIndexKeyNotFound when target is below the first key or the index
// is empty.
func (o *OffsetIndex) KeyLookup(target types.TxID) (types.TxID, uint64, error) {
	return offsetIndexLookup(o.f, o.numEntries, target)
}

// Entries returns a snapshot of every valid entry in ascending key order.
func (o *OffsetIndex) Entries() ([]OffsetIndexEntry, error) {
	return readOffsetIndexEntries(o.f, o.numEntries)
}

// NumEntries returns the count of valid entries.
func (o *OffsetIndex) NumEntries() uint64 {
	return o.numEntries
}

// Close releases the read-only file handle.
func (o *OffsetIndex) Close() error {
	if o == nil || o.f == nil {
		return nil
	}
	err := o.f.Close()
	o.f = nil
	return err
}

func scanOffsetIndexPrefix(f *os.File, cap uint64) (uint64, uint64, error) {
	var buf [OffsetIndexEntrySize]byte
	var last uint64
	for i := uint64(0); i < cap; i++ {
		_, err := f.ReadAt(buf[:], int64(i*OffsetIndexEntrySize))
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return i, last, nil
		}
		if err != nil {
			return 0, 0, err
		}
		key := binary.LittleEndian.Uint64(buf[offsetIndexKeyOff:])
		val := binary.LittleEndian.Uint64(buf[offsetIndexValOff:])
		if key == 0 {
			return i, last, nil
		}
		// Real record byte offsets are always >= SegmentHeaderSize. A
		// value of zero indicates a partial write where the key half
		// landed but the value half did not (pre-allocated zeros), so
		// treat it as the end of the valid prefix.
		if val == 0 {
			return i, last, nil
		}
		if i > 0 && key <= last {
			return i, last, nil
		}
		last = key
	}
	return cap, last, nil
}

func offsetIndexLookup(f *os.File, n uint64, target types.TxID) (types.TxID, uint64, error) {
	if n == 0 {
		return 0, 0, ErrOffsetIndexKeyNotFound
	}
	key := uint64(target)
	lo, hi := uint64(0), n
	for lo < hi {
		mid := (lo + hi) / 2
		midKey, _, err := readOffsetIndexEntryAt(f, mid)
		if err != nil {
			return 0, 0, err
		}
		if midKey <= key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0, 0, ErrOffsetIndexKeyNotFound
	}
	foundKey, val, err := readOffsetIndexEntryAt(f, lo-1)
	if err != nil {
		return 0, 0, err
	}
	return types.TxID(foundKey), val, nil
}

func readOffsetIndexEntryAt(f *os.File, idx uint64) (uint64, uint64, error) {
	var buf [OffsetIndexEntrySize]byte
	if _, err := f.ReadAt(buf[:], int64(idx*OffsetIndexEntrySize)); err != nil {
		return 0, 0, err
	}
	key := binary.LittleEndian.Uint64(buf[offsetIndexKeyOff:])
	val := binary.LittleEndian.Uint64(buf[offsetIndexValOff:])
	return key, val, nil
}

func readOffsetIndexEntries(f *os.File, n uint64) ([]OffsetIndexEntry, error) {
	if n == 0 {
		return nil, nil
	}
	var out []OffsetIndexEntry
	for i := uint64(0); i < n; i++ {
		key, val, err := readOffsetIndexEntryAt(f, i)
		if err != nil {
			return nil, err
		}
		out = append(out, OffsetIndexEntry{TxID: types.TxID(key), ByteOffset: val})
	}
	return out, nil
}

// OffsetIndexWriter wraps an OffsetIndexMut with bytes-since-last-append
// cadence. The earliest (lowest tx id) commit in each cadence window becomes
// the candidate; when the running byte counter crosses the interval, the
// candidate is flushed and the new commit becomes the next candidate.
// Sync flushes the pending candidate.
type OffsetIndexWriter struct {
	head                  *OffsetIndexMut
	minWriteIntervalBytes uint64
	bytesSinceLastAppend  uint64
	candidateTxID         types.TxID
	candidateByteOffset   uint64
	haveCandidate         bool
	full                  bool
}

// NewOffsetIndexWriter wraps head with the given cadence threshold.
func NewOffsetIndexWriter(head *OffsetIndexMut, minWriteIntervalBytes uint64) *OffsetIndexWriter {
	return &OffsetIndexWriter{head: head, minWriteIntervalBytes: minWriteIntervalBytes}
}

// AppendAfterCommit buffers (txID, byteOffset) as the current candidate and
// flushes it when the running byte counter crosses the cadence threshold.
// recordLen is the byte length of the just-committed record.
//
// Once the underlying index reports ErrOffsetIndexFull, the writer stops
// buffering; subsequent calls are no-ops.
func (w *OffsetIndexWriter) AppendAfterCommit(txID types.TxID, byteOffset uint64, recordLen uint64) error {
	if w.full {
		return nil
	}
	w.bytesSinceLastAppend += recordLen
	if !w.haveCandidate {
		w.candidateTxID = txID
		w.candidateByteOffset = byteOffset
		w.haveCandidate = true
		return nil
	}
	if w.bytesSinceLastAppend >= w.minWriteIntervalBytes {
		if err := w.head.Append(w.candidateTxID, w.candidateByteOffset); err != nil {
			if errors.Is(err, ErrOffsetIndexFull) {
				w.full = true
				w.haveCandidate = false
				w.bytesSinceLastAppend = 0
				return nil
			}
			return err
		}
		w.candidateTxID = txID
		w.candidateByteOffset = byteOffset
		w.bytesSinceLastAppend = 0
		return nil
	}
	return nil
}

// Sync flushes the pending candidate (if any) and fsyncs the backing index.
// Must be called after the segment's own fsync so the index cannot reference
// a byte offset that is not yet durable.
func (w *OffsetIndexWriter) Sync() error {
	if w.haveCandidate {
		if err := w.head.Append(w.candidateTxID, w.candidateByteOffset); err != nil {
			if errors.Is(err, ErrOffsetIndexFull) {
				w.full = true
			} else {
				return err
			}
		}
		w.haveCandidate = false
		w.bytesSinceLastAppend = 0
	}
	return w.head.Sync()
}

// Truncate drops all index entries with key >= target and discards the
// pending candidate if it falls at or above the boundary.
func (w *OffsetIndexWriter) Truncate(target types.TxID) error {
	if w.haveCandidate && w.candidateTxID >= target {
		w.haveCandidate = false
		w.bytesSinceLastAppend = 0
	}
	return w.head.Truncate(target)
}

// Close releases the backing index file. Any unflushed candidate is lost;
// callers that need durability must Sync first.
func (w *OffsetIndexWriter) Close() error {
	return w.head.Close()
}
