package commitlog

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// CommitLogOptions configures the durability worker.
type CommitLogOptions struct {
	MaxSegmentSize        int64
	MaxRecordPayloadBytes uint32
	MaxRowBytes           uint32
	ChannelCapacity       int
	DrainBatchSize        int
	SnapshotInterval      uint64
}

// DefaultCommitLogOptions returns sensible defaults.
func DefaultCommitLogOptions() CommitLogOptions {
	return CommitLogOptions{
		MaxSegmentSize:        512 << 20, // 512 MiB
		MaxRecordPayloadBytes: 64 << 20,  // 64 MiB
		MaxRowBytes:           8 << 20,   // 8 MiB
		ChannelCapacity:       256,
		DrainBatchSize:        64,
		SnapshotInterval:      0,
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
}

// NewDurabilityWorker creates and starts the worker.
// If an active segment already exists for startTxID, it is reopened for appending.
// Otherwise a new segment is created.
func NewDurabilityWorker(dir string, startTxID uint64, opts CommitLogOptions) (*DurabilityWorker, error) {
	return NewDurabilityWorkerWithResumePlan(dir, RecoveryResumePlan{
		SegmentStartTx: types.TxID(startTxID),
		NextTxID:       types.TxID(startTxID),
		AppendMode:     AppendInPlace,
	}, opts)
}

// NewDurabilityWorkerWithResumePlan creates and starts the worker using the
// append strategy chosen during recovery.
func NewDurabilityWorkerWithResumePlan(dir string, plan RecoveryResumePlan, opts CommitLogOptions) (*DurabilityWorker, error) {
	seg, durableTxID, err := openSegmentForResumePlan(dir, plan)
	if err != nil {
		return nil, err
	}
	dw := &DurabilityWorker{
		ch:      make(chan durabilityItem, opts.ChannelCapacity),
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
		waiters: make(map[uint64][]chan types.TxID),
		opts:    opts,
		dir:     dir,
		seg:     seg,
	}
	if durableTxID > 0 {
		dw.durable.Store(durableTxID)
		dw.lastEnq = durableTxID
	} else if seg.lastTx > 0 {
		dw.durable.Store(seg.lastTx)
		dw.lastEnq = seg.lastTx
	}
	go dw.run()
	return dw, nil
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
		seg, err := openOrCreateSegment(dir, uint64(plan.SegmentStartTx))
		if err != nil {
			return nil, 0, err
		}
		return seg, seg.lastTx, nil
	case AppendByFreshNextSegment:
		if plan.SegmentStartTx == 0 || plan.NextTxID == 0 {
			return nil, 0, fmt.Errorf("commitlog: invalid recovery resume plan: %+v", plan)
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

	if dw.seg != nil {
		dw.stateMu.Lock()
		fatal := dw.fatalErr
		dw.stateMu.Unlock()
		if fatal != nil {
			_ = dw.seg.file.Close()
		} else {
			_ = dw.seg.Close()
		}
	}

	dw.stateMu.Lock()
	defer dw.stateMu.Unlock()
	return dw.durable.Load(), dw.fatalErr
}

func (dw *DurabilityWorker) run() {
	defer close(dw.done)
	for {
		item, ok := <-dw.ch
		if !ok {
			return
		}
		batch := []durabilityItem{item}
	drain:
		for range dw.opts.DrainBatchSize - 1 {
			select {
			case it, ok := <-dw.ch:
				if !ok {
					break drain
				}
				batch = append(batch, it)
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
		payload, err := EncodeChangeset(item.changeset)
		if err != nil {
			return err
		}
		rec := &Record{
			TxID:       item.txID,
			RecordType: RecordTypeChangeset,
			Payload:    payload,
		}
		if err := dw.seg.Append(rec); err != nil {
			return err
		}
	}
	if err := dw.seg.Sync(); err != nil {
		return err
	}
	// Update durable TxID to last in batch.
	lastDurable := batch[len(batch)-1].txID
	dw.durable.Store(lastDurable)
	dw.releaseWaitersUpTo(lastDurable)

	// Check rotation.
	if dw.seg.Size() >= dw.opts.MaxSegmentSize {
		nextTx := batch[len(batch)-1].txID + 1
		if err := dw.seg.Close(); err != nil {
			return err
		}
		seg, err := CreateSegment(dw.dir, nextTx)
		if err != nil {
			return err
		}
		dw.seg = seg
	}
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
