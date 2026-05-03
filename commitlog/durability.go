package commitlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type FsyncMode uint8

const (
	FsyncBatch FsyncMode = 0
	FsyncPerTx FsyncMode = 1
)

// CommitLogOptions configures the durability worker.
type CommitLogOptions struct {
	MaxSegmentSize        int64
	MaxRecordPayloadBytes uint32
	MaxRowBytes           uint32
	ChannelCapacity       int
	DrainBatchSize        int
	FsyncMode             FsyncMode
	SnapshotInterval      uint64
	// OffsetIndexIntervalBytes gates the per-segment offset index writer.
	// A pending candidate is flushed to the index once the bytes-since-last-
	// append counter crosses this threshold. Zero disables indexing (the
	// sidecar file is not created and the writer becomes a no-op).
	OffsetIndexIntervalBytes uint64
	// OffsetIndexCap bounds the number of entries a per-segment offset index
	// file preallocates. The sidecar file occupies OffsetIndexCap*16 bytes.
	// Zero disables indexing, same as OffsetIndexIntervalBytes == 0.
	OffsetIndexCap uint64
	// Observer receives runtime-scoped durability observations. Nil is a
	// no-op for package-level pre-runtime use.
	Observer Observer
}

// DefaultCommitLogOptions returns sensible defaults.
func DefaultCommitLogOptions() CommitLogOptions {
	return CommitLogOptions{
		MaxSegmentSize:           512 << 20, // 512 MiB
		MaxRecordPayloadBytes:    64 << 20,  // 64 MiB
		MaxRowBytes:              8 << 20,   // 8 MiB
		ChannelCapacity:          256,
		DrainBatchSize:           64,
		FsyncMode:                FsyncBatch,
		SnapshotInterval:         0,
		OffsetIndexIntervalBytes: 64 << 10, // 64 KiB
		OffsetIndexCap:           16384,
	}
}

type durabilityItem struct {
	txID      uint64
	changeset *store.Changeset
}

// DurabilityWorker persists committed transactions to the segment log.
type DurabilityWorker struct {
	ch         chan durabilityItem
	closeCh    chan struct{}
	durable    atomic.Uint64
	stateMu    sync.Mutex
	waiters    map[uint64][]chan types.TxID
	fatalErr   error
	closing    bool
	lastEnq    uint64
	sends      sync.WaitGroup
	done       chan struct{}
	closeOnce  sync.Once
	signalOnce sync.Once
	opts       CommitLogOptions
	dir        string
	seg        *SegmentWriter
	idx        *OffsetIndexWriter
	observer   Observer
}

// NewDurabilityWorker creates and starts the worker.
// If an active segment already exists for startTxID, it is reopened for appending.
// Otherwise a new segment is created.
func NewDurabilityWorker(dir string, startTxID uint64, opts CommitLogOptions) (*DurabilityWorker, error) {
	if err := validateCommitLogOptions(opts); err != nil {
		recordDurabilityFailed(opts.Observer, err, "open_failed", 0)
		return nil, err
	}
	seg, err := openOrCreateSegment(dir, startTxID)
	if err != nil {
		recordDurabilityFailed(opts.Observer, err, "open_failed", 0)
		return nil, err
	}
	return newDurabilityWorkerFromSegment(dir, seg, 0, opts), nil
}

func validateFsyncMode(mode FsyncMode) error {
	if mode != FsyncBatch {
		return fmt.Errorf("%w: %d", ErrUnknownFsyncMode, mode)
	}
	return nil
}

func validateCommitLogOptions(opts CommitLogOptions) error {
	if err := validateFsyncMode(opts.FsyncMode); err != nil {
		return err
	}
	if opts.ChannelCapacity < 0 {
		return fmt.Errorf("commitlog: channel capacity must be non-negative: %d", opts.ChannelCapacity)
	}
	return nil
}

// NewDurabilityWorkerWithResumePlan creates and starts the worker using the
// append strategy chosen during recovery.
func NewDurabilityWorkerWithResumePlan(dir string, plan RecoveryResumePlan, opts CommitLogOptions) (*DurabilityWorker, error) {
	if err := validateCommitLogOptions(opts); err != nil {
		recordDurabilityFailed(opts.Observer, err, "open_failed", 0)
		return nil, err
	}
	seg, durableTxID, err := openSegmentForResumePlan(dir, plan)
	if err != nil {
		recordDurabilityFailed(opts.Observer, err, "open_failed", uint64(plan.NextTxID))
		return nil, err
	}
	return newDurabilityWorkerFromSegment(dir, seg, durableTxID, opts), nil
}

func newDurabilityWorkerFromSegment(dir string, seg *SegmentWriter, durableTxID uint64, opts CommitLogOptions) *DurabilityWorker {
	dw := &DurabilityWorker{
		ch:       make(chan durabilityItem, opts.ChannelCapacity),
		closeCh:  make(chan struct{}),
		done:     make(chan struct{}),
		waiters:  make(map[uint64][]chan types.TxID),
		opts:     opts,
		dir:      dir,
		seg:      seg,
		observer: opts.Observer,
	}
	if durableTxID > 0 {
		dw.durable.Store(durableTxID)
		dw.lastEnq = durableTxID
	} else if seg.lastTx > 0 {
		dw.durable.Store(seg.lastTx)
		dw.lastEnq = seg.lastTx
	}
	dw.idx = initOffsetIndexForSegment(dir, seg, opts)
	go dw.run()
	return dw
}

// initOffsetIndexForSegment opens or creates the per-segment offset index
// sidecar next to seg and wraps it as a cadence writer. Indexing is disabled
// (nil returned) when options disable it or when any construction step fails.
// The index is advisory; failures do not bubble up or emit production logs.
func initOffsetIndexForSegment(dir string, seg *SegmentWriter, opts CommitLogOptions) *OffsetIndexWriter {
	if opts.OffsetIndexIntervalBytes == 0 || opts.OffsetIndexCap == 0 {
		return nil
	}
	if seg == nil {
		return nil
	}
	path := filepath.Join(dir, OffsetIndexFileName(seg.startTx))
	var head *OffsetIndexMut
	if _, err := os.Stat(path); err == nil {
		m, oerr := OpenOffsetIndexMut(path, opts.OffsetIndexCap)
		if oerr != nil {
			return nil
		}
		if terr := m.Truncate(types.TxID(seg.lastTx + 1)); terr != nil {
			_ = m.Close()
			return nil
		}
		head = m
	} else if errors.Is(err, os.ErrNotExist) {
		m, cerr := CreateOffsetIndex(path, opts.OffsetIndexCap)
		if cerr != nil {
			return nil
		}
		head = m
	} else {
		return nil
	}
	return NewOffsetIndexWriter(head, opts.OffsetIndexIntervalBytes)
}

func openOrCreateSegment(dir string, startTxID uint64) (*SegmentWriter, error) {
	seg, err := OpenSegmentForAppend(dir, startTxID)
	if err == nil {
		return seg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return CreateSegment(dir, startTxID)
	}
	return nil, err
}

func openSegmentForResumePlan(dir string, plan RecoveryResumePlan) (*SegmentWriter, uint64, error) {
	switch plan.AppendMode {
	case AppendInPlace:
		return openSegmentForAppendInPlaceResumePlan(dir, plan)
	case AppendByFreshNextSegment:
		if plan.SegmentStartTx == 0 || plan.NextTxID == 0 || plan.SegmentStartTx != plan.NextTxID {
			return nil, 0, fmt.Errorf("commitlog: invalid recovery resume plan: %+v", plan)
		}
		if err := removeEmptySegmentDirectoryArtifact(dir, uint64(plan.SegmentStartTx)); err != nil {
			return nil, 0, err
		}
		seg, err := CreateSegment(dir, uint64(plan.SegmentStartTx))
		if err != nil {
			return nil, 0, err
		}
		return seg, uint64(plan.NextTxID - 1), nil
	case AppendForbidden:
		return nil, 0, fmt.Errorf("commitlog: append forbidden for recovery resume plan")
	default:
		return nil, 0, fmt.Errorf("commitlog: unknown append mode %d", plan.AppendMode)
	}
}

func openSegmentForAppendInPlaceResumePlan(dir string, plan RecoveryResumePlan) (*SegmentWriter, uint64, error) {
	if plan.SegmentStartTx == 0 || plan.NextTxID == 0 {
		return nil, 0, fmt.Errorf("commitlog: invalid recovery resume plan: %+v", plan)
	}
	path := filepath.Join(dir, SegmentFileName(uint64(plan.SegmentStartTx)))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if plan.NextTxID != plan.SegmentStartTx {
			return nil, 0, fmt.Errorf("commitlog: invalid recovery resume plan: %+v", plan)
		}
		seg, err := CreateSegment(dir, uint64(plan.SegmentStartTx))
		if err != nil {
			return nil, 0, err
		}
		return seg, 0, nil
	}
	seg, err := OpenSegmentForAppend(dir, uint64(plan.SegmentStartTx))
	if err != nil {
		return nil, 0, err
	}
	expectedNextTxID := plan.SegmentStartTx
	if seg.hasLastRecord {
		expectedNextTxID = types.TxID(seg.lastTx + 1)
	}
	if plan.NextTxID != expectedNextTxID {
		closeErr := seg.Close()
		if closeErr != nil {
			return nil, 0, fmt.Errorf("commitlog: invalid recovery resume plan: %+v (close error: %w)", plan, closeErr)
		}
		return nil, 0, fmt.Errorf("commitlog: invalid recovery resume plan: %+v", plan)
	}
	return seg, seg.lastTx, nil
}

func removeEmptySegmentDirectoryArtifact(dir string, startTxID uint64) error {
	path := filepath.Join(dir, SegmentFileName(startTxID))
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("commitlog: remove rollover segment directory artifact %s: %w", path, err)
	}
	return nil
}

// EnqueueCommitted sends a committed changeset for durability.
// Panics if closed or fatally errored.
func (dw *DurabilityWorker) EnqueueCommitted(txID uint64, cs *store.Changeset) {
	dw.stateMu.Lock()
	if dw.fatalErr != nil {
		fatal := dw.fatalErr
		dw.stateMu.Unlock()
		panic(fmt.Errorf("%w: %w", ErrDurabilityFailed, fatal))
	}
	if dw.closing {
		dw.stateMu.Unlock()
		panic("commitlog: enqueue after close")
	}
	if txID <= dw.lastEnq {
		dw.stateMu.Unlock()
		panic(fmt.Sprintf("commitlog: enqueue tx %d after %d", txID, dw.lastEnq))
	}
	dw.lastEnq = txID
	dw.sends.Add(1)
	dw.stateMu.Unlock()
	defer dw.sends.Done()

	item := durabilityItem{txID: txID, changeset: cs}
	select {
	case dw.ch <- item:
		recordDurabilityQueueDepth(dw.observer, dw.QueueDepth())
		return
	case <-dw.closeCh:
		dw.stateMu.Lock()
		fatal := dw.fatalErr
		closing := dw.closing
		dw.stateMu.Unlock()
		if fatal != nil {
			panic(fmt.Errorf("%w: %w", ErrDurabilityFailed, fatal))
		}
		if closing {
			panic("commitlog: enqueue after close")
		}
		panic("commitlog: enqueue after worker stop")
	}
}

// DurableTxID returns the latest durably written TxID.
func (dw *DurabilityWorker) DurableTxID() uint64 {
	return dw.durable.Load()
}

// QueueDepth returns the current durability queue depth.
func (dw *DurabilityWorker) QueueDepth() int {
	if dw == nil {
		return 0
	}
	return len(dw.ch)
}

// QueueCapacity returns the configured durability queue capacity.
func (dw *DurabilityWorker) QueueCapacity() int {
	if dw == nil {
		return 0
	}
	return cap(dw.ch)
}

// FatalError returns the latched fatal worker error, if any.
func (dw *DurabilityWorker) FatalError() error {
	if dw == nil {
		return nil
	}
	dw.stateMu.Lock()
	defer dw.stateMu.Unlock()
	return dw.fatalErr
}

// WaitUntilDurable returns a readiness channel for txID. Already-durable txIDs
// return an already-ready channel.
func (dw *DurabilityWorker) WaitUntilDurable(txID types.TxID) <-chan types.TxID {
	ready := func(id types.TxID) <-chan types.TxID {
		ch := make(chan types.TxID, 1)
		ch <- id
		close(ch)
		return ch
	}
	if txID == 0 {
		return nil
	}
	dw.stateMu.Lock()
	defer dw.stateMu.Unlock()
	if dw.durable.Load() >= uint64(txID) {
		return ready(txID)
	}
	ch := make(chan types.TxID, 1)
	dw.waiters[uint64(txID)] = append(dw.waiters[uint64(txID)], ch)
	return ch
}

// Close stops the worker and returns the final durable TxID and any fatal error.
func (dw *DurabilityWorker) Close() (uint64, error) {
	dw.stateMu.Lock()
	dw.closing = true
	dw.stateMu.Unlock()
	dw.signalClose()
	dw.sends.Wait()
	dw.closeOnce.Do(func() { close(dw.ch) })
	<-dw.done

	var closeErr error
	if dw.seg != nil {
		dw.stateMu.Lock()
		fatal := dw.fatalErr
		dw.stateMu.Unlock()
		if fatal != nil {
			closeErr = errors.Join(closeErr, dw.seg.file.Close())
			if dw.idx != nil {
				closeErr = errors.Join(closeErr, dw.idx.Close())
				dw.idx = nil
			}
		} else {
			closeErr = errors.Join(closeErr, dw.seg.Close())
			if dw.idx != nil {
				closeErr = errors.Join(closeErr, dw.idx.Sync())
				closeErr = errors.Join(closeErr, dw.idx.Close())
				dw.idx = nil
			}
		}
		dw.seg = nil
	}

	dw.stateMu.Lock()
	defer dw.stateMu.Unlock()
	return dw.durable.Load(), errors.Join(dw.fatalErr, closeErr)
}

func (dw *DurabilityWorker) run() {
	defer close(dw.done)
	for {
		item, ok := <-dw.ch
		if !ok {
			return
		}
		recordDurabilityQueueDepth(dw.observer, dw.QueueDepth())
		batchCap := min(dw.opts.DrainBatchSize, 1024)
		if batchCap < 1 {
			batchCap = 1
		}
		batch := make([]durabilityItem, 0, batchCap)
		batch = append(batch, item)
	drain:
		for range dw.opts.DrainBatchSize - 1 {
			select {
			case it, ok := <-dw.ch:
				if !ok {
					break drain
				}
				batch = append(batch, it)
				recordDurabilityQueueDepth(dw.observer, dw.QueueDepth())
			default:
				break drain
			}
		}
		if err := dw.processBatch(batch); err != nil {
			dw.stateMu.Lock()
			if dw.fatalErr == nil {
				dw.fatalErr = err
			}
			dw.stateMu.Unlock()
			dw.signalClose()
			return
		}
	}
}

func (dw *DurabilityWorker) signalClose() {
	dw.signalOnce.Do(func() { close(dw.closeCh) })
}

func (dw *DurabilityWorker) processBatch(batch []durabilityItem) error {
	for _, item := range batch {
		payload, err := encodeChangesetWithLimits(item.changeset, dw.opts.MaxRowBytes, dw.opts.MaxRecordPayloadBytes)
		if err != nil {
			recordDurabilityFailed(dw.observer, err, "write_failed", item.txID)
			traceDurabilityBatch(dw.observer, item.txID, "error", err)
			return err
		}
		rec := &Record{
			TxID:       item.txID,
			RecordType: RecordTypeChangeset,
			Payload:    payload,
		}
		if err := dw.seg.Append(rec); err != nil {
			recordDurabilityFailed(dw.observer, err, "write_failed", item.txID)
			traceDurabilityBatch(dw.observer, item.txID, "error", err)
			return err
		}
		if dw.idx != nil {
			if off, ok := dw.seg.LastRecordByteOffset(); ok {
				recLen := uint64(RecordOverhead + len(rec.Payload))
				if err := dw.idx.AppendAfterCommit(types.TxID(rec.TxID), uint64(off), recLen); err != nil {
					_ = dw.idx.Close()
					dw.idx = nil
				}
			}
		}
	}
	if err := dw.seg.Sync(); err != nil {
		recordDurabilityFailed(dw.observer, err, "sync_failed", batch[len(batch)-1].txID)
		traceDurabilityBatch(dw.observer, batch[len(batch)-1].txID, "error", err)
		return err
	}
	if dw.idx != nil {
		if err := dw.idx.Sync(); err != nil {
			_ = dw.idx.Close()
			dw.idx = nil
		}
	}
	// Update durable TxID to last in batch.
	lastDurable := batch[len(batch)-1].txID
	dw.durable.Store(lastDurable)
	recordDurabilityDurableTxID(dw.observer, lastDurable)
	dw.releaseWaitersUpTo(lastDurable)

	// Check rotation.
	if dw.seg.Size() >= dw.opts.MaxSegmentSize {
		nextTx := batch[len(batch)-1].txID + 1
		if err := dw.seg.Close(); err != nil {
			recordDurabilityFailed(dw.observer, err, "segment_rotate_failed", batch[len(batch)-1].txID)
			traceDurabilityBatch(dw.observer, batch[len(batch)-1].txID, "error", err)
			return err
		}
		if dw.idx != nil {
			_ = dw.idx.Close()
			dw.idx = nil
		}
		seg, err := CreateSegment(dw.dir, nextTx)
		if err != nil {
			recordDurabilityFailed(dw.observer, err, "segment_rotate_failed", nextTx)
			traceDurabilityBatch(dw.observer, nextTx, "error", err)
			return err
		}
		dw.seg = seg
		dw.idx = initOffsetIndexForSegment(dw.dir, seg, dw.opts)
	}
	traceDurabilityBatch(dw.observer, lastDurable, "ok", nil)
	return nil
}

func (dw *DurabilityWorker) releaseWaitersUpTo(lastDurable uint64) {
	dw.stateMu.Lock()
	defer dw.stateMu.Unlock()
	for txID, waiters := range dw.waiters {
		if txID > lastDurable {
			continue
		}
		delete(dw.waiters, txID)
		for _, ch := range waiters {
			ch <- types.TxID(txID)
			close(ch)
		}
	}
}
