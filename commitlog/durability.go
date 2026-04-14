package commitlog

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ponchione/shunter/store"
)

// CommitLogOptions configures the durability worker.
type CommitLogOptions struct {
	MaxSegmentSize        int64
	MaxRecordPayloadBytes uint32
	MaxRowBytes           uint32
	ChannelCapacity       int
	DrainBatchSize        int
}

// DefaultCommitLogOptions returns sensible defaults.
func DefaultCommitLogOptions() CommitLogOptions {
	return CommitLogOptions{
		MaxSegmentSize:        512 << 20, // 512 MiB
		MaxRecordPayloadBytes: 64 << 20,  // 64 MiB
		MaxRowBytes:           8 << 20,   // 8 MiB
		ChannelCapacity:       256,
		DrainBatchSize:        64,
	}
}

type durabilityItem struct {
	txID      uint64
	changeset *store.Changeset
}

// DurabilityWorker persists committed transactions to the segment log.
type DurabilityWorker struct {
	ch       chan durabilityItem
	durable  atomic.Uint64
	stateMu  sync.Mutex
	fatalErr error
	closing  bool
	lastEnq  uint64
	done     chan struct{}
	opts     CommitLogOptions
	dir      string
	seg      *SegmentWriter
}

// NewDurabilityWorker creates and starts the worker.
func NewDurabilityWorker(dir string, startTxID uint64, opts CommitLogOptions) (*DurabilityWorker, error) {
	seg, err := CreateSegment(dir, startTxID)
	if err != nil {
		return nil, err
	}
	dw := &DurabilityWorker{
		ch:   make(chan durabilityItem, opts.ChannelCapacity),
		done: make(chan struct{}),
		opts: opts,
		dir:  dir,
		seg:  seg,
	}
	go dw.run()
	return dw, nil
}

// EnqueueCommitted sends a committed changeset for durability.
// Panics if closed or fatally errored.
func (dw *DurabilityWorker) EnqueueCommitted(txID uint64, cs *store.Changeset) {
	dw.stateMu.Lock()
	fatal := dw.fatalErr
	closing := dw.closing
	if fatal == nil && !closing {
		if txID <= dw.lastEnq {
			dw.stateMu.Unlock()
			panic(fmt.Sprintf("commitlog: enqueue tx %d after %d", txID, dw.lastEnq))
		}
		dw.lastEnq = txID
	}
	dw.stateMu.Unlock()
	if fatal != nil {
		panic(fmt.Errorf("%w: %w", ErrDurabilityFailed, fatal))
	}
	if closing {
		panic("commitlog: enqueue after close")
	}
	dw.ch <- durabilityItem{txID: txID, changeset: cs}
}

// DurableTxID returns the latest durably written TxID.
func (dw *DurabilityWorker) DurableTxID() uint64 {
	return dw.durable.Load()
}

// Close stops the worker and returns the final durable TxID and any fatal error.
func (dw *DurabilityWorker) Close() (uint64, error) {
	dw.stateMu.Lock()
	dw.closing = true
	dw.stateMu.Unlock()
	close(dw.ch)
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
		// Non-blocking drain.
		for range dw.opts.DrainBatchSize - 1 {
			select {
			case it, ok := <-dw.ch:
				if !ok {
					break
				}
				batch = append(batch, it)
			default:
				goto process
			}
		}
	process:
		if err := dw.processBatch(batch); err != nil {
			dw.stateMu.Lock()
			if dw.fatalErr == nil {
				dw.fatalErr = err
			}
			dw.stateMu.Unlock()
			return
		}
	}
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
	dw.durable.Store(batch[len(batch)-1].txID)

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
