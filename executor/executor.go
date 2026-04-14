package executor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// ExecutorConfig configures the executor.
type ExecutorConfig struct {
	InboxCapacity int
	RejectOnFull  bool
	// Durability receives every successful commit via the post-commit
	// pipeline (SPEC-003 §5.1). Nil defaults to a no-op handle so existing
	// unit tests that pre-date the pipeline can still construct an executor.
	Durability DurabilityHandle
	// Subscriptions evaluates every successful commit synchronously against
	// a committed read view (SPEC-003 §5.2–§5.3). Nil defaults to a no-op.
	Subscriptions SubscriptionManager
}

// Executor serializes all write-affecting operations.
type Executor struct {
	inbox      chan ExecutorCommand
	registry   *ReducerRegistry
	committed  *store.CommittedState
	schemaReg  schema.SchemaRegistry
	nextTxID   uint64
	fatal      bool
	rejectMode bool
	shutdown   bool
	// schedTableID is the cached schema.TableID for sys_scheduled,
	// resolved once at NewExecutor so per-reducer handle construction
	// avoids a registry lookup on every dispatch.
	schedTableID schema.TableID
	// schedSeq allocates monotonic ScheduleIDs across reducer
	// transactions. Rollback produces gaps, matching Postgres
	// sequence semantics. Story 6.5 resets this from the max existing
	// schedule_id at startup so replayed schedules keep their IDs.
	schedSeq   *store.Sequence
	durability DurabilityHandle
	subs       SubscriptionManager
	// snapshotFn acquires the committed read view used by post-commit
	// subscription evaluation. Defaults to e.committed.Snapshot(); tests
	// override it to inject a tracking wrapper.
	snapshotFn func() store.CommittedReadView
	done       chan struct{}
	closeOnce  sync.Once
}

// NewExecutor creates an executor. Registry must be frozen.
func NewExecutor(cfg ExecutorConfig, reg *ReducerRegistry, cs *store.CommittedState, schemaReg schema.SchemaRegistry, recoveredTxID uint64) *Executor {
	if !reg.IsFrozen() {
		panic("executor: registry must be frozen before creating executor")
	}
	cap := cfg.InboxCapacity
	if cap <= 0 {
		cap = 256
	}
	dur := cfg.Durability
	if dur == nil {
		dur = noopDurability{}
	}
	subs := cfg.Subscriptions
	if subs == nil {
		subs = noopSubs{}
	}
	schedTS, ok := SysScheduledTable(schemaReg)
	if !ok {
		panic("executor: sys_scheduled is not registered; every schema.Build call must register system tables")
	}
	e := &Executor{
		inbox:        make(chan ExecutorCommand, cap),
		registry:     reg,
		committed:    cs,
		schemaReg:    schemaReg,
		nextTxID:     recoveredTxID + 1,
		rejectMode:   cfg.RejectOnFull,
		schedTableID: schedTS.ID,
		schedSeq:     store.NewSequence(),
		durability:   dur,
		subs:         subs,
		done:         make(chan struct{}),
	}
	e.snapshotFn = func() store.CommittedReadView { return e.committed.Snapshot() }
	return e
}

// Run processes commands until context is cancelled or inbox is closed.
func (e *Executor) Run(ctx context.Context) {
	defer close(e.done)
	for {
		select {
		case <-ctx.Done():
			return
		case cmd, ok := <-e.inbox:
			if !ok {
				return
			}
			e.dispatchSafely(cmd)
		}
	}
}

// Submit sends a command to the executor inbox.
func (e *Executor) Submit(cmd ExecutorCommand) error {
	if e.fatal {
		return ErrExecutorFatal
	}
	if e.shutdown {
		return ErrExecutorShutdown
	}
	if e.rejectMode {
		select {
		case e.inbox <- cmd:
			return nil
		default:
			return ErrExecutorBusy
		}
	}
	e.inbox <- cmd
	return nil
}

// SubmitWithContext sends a command respecting a caller context.
func (e *Executor) SubmitWithContext(ctx context.Context, cmd ExecutorCommand) error {
	if e.fatal {
		return ErrExecutorFatal
	}
	if e.shutdown {
		return ErrExecutorShutdown
	}
	select {
	case e.inbox <- cmd:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown stops accepting new commands and waits for Run to finish.
func (e *Executor) Shutdown() {
	e.shutdown = true
	e.closeOnce.Do(func() { close(e.inbox) })
	<-e.done
}

func (e *Executor) dispatchSafely(cmd ExecutorCommand) {
	defer func() {
		if r := recover(); r != nil {
			e.handleDispatchPanic(cmd, r)
		}
	}()
	e.dispatch(cmd)
}

func (e *Executor) handleDispatchPanic(cmd ExecutorCommand, r any) {
	log.Printf("executor: panic during dispatch: %v\n%s", r, debug.Stack())
	// Send error response if possible.
	if c, ok := cmd.(CallReducerCmd); ok && c.ResponseCh != nil {
		c.ResponseCh <- ReducerResponse{
			Status: StatusFailedPanic,
			Error:  fmt.Errorf("reducer panicked: %v", r),
		}
	}
}

func (e *Executor) dispatch(cmd ExecutorCommand) {
	// Story 5.3: short-circuit write-affecting commands that were already in
	// the inbox when the executor latched into the fatal state. Submit
	// catches the common case; this catch covers the race window.
	if e.fatal {
		if c, ok := cmd.(CallReducerCmd); ok && c.ResponseCh != nil {
			c.ResponseCh <- ReducerResponse{
				Status: StatusFailedInternal,
				Error:  ErrExecutorFatal,
			}
		}
		return
	}
	switch c := cmd.(type) {
	case CallReducerCmd:
		e.handleCallReducer(c)
	case OnConnectCmd:
		e.handleOnConnect(c)
	case OnDisconnectCmd:
		e.handleOnDisconnect(c)
	default:
		log.Printf("executor: unknown command type %T", cmd)
	}
}

func (e *Executor) handleCallReducer(cmd CallReducerCmd) {
	req := cmd.Request
	if req.Source != CallSourceLifecycle {
		if _, reserved := lifecycleNames[req.ReducerName]; reserved {
			cmd.ResponseCh <- ReducerResponse{
				Status: StatusFailedInternal,
				Error:  ErrLifecycleReducer,
			}
			return
		}
	}

	// Lookup reducer.
	rr, ok := e.registry.Lookup(req.ReducerName)
	if !ok {
		cmd.ResponseCh <- ReducerResponse{
			Status: StatusFailedInternal,
			Error:  fmt.Errorf("%w: %s", ErrReducerNotFound, req.ReducerName),
		}
		return
	}

	// Begin: create transaction + context.
	caller := types.CallerContext{
		Identity:     req.Caller.Identity,
		ConnectionID: req.Caller.ConnectionID,
		Timestamp:    time.Now().UTC(),
	}
	tx := store.NewTransaction(e.committed, e.schemaReg)
	rctx := &types.ReducerContext{
		ReducerName: req.ReducerName,
		Caller:      caller,
		DB:          tx,
		Scheduler:   e.newSchedulerHandle(tx),
	}

	// Execute with panic recovery.
	var ret []byte
	var reducerErr error
	var panicked any
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = r
			}
		}()
		ret, reducerErr = rr.Handler(rctx, req.Args)
	}()

	// Decision routing.
	if panicked != nil {
		store.Rollback(tx)
		cmd.ResponseCh <- ReducerResponse{
			Status: StatusFailedPanic,
			Error:  fmt.Errorf("%v: %w", panicked, ErrReducerPanic),
		}
		return
	}

	if reducerErr != nil {
		store.Rollback(tx)
		cmd.ResponseCh <- ReducerResponse{
			Status: StatusFailedUser,
			Error:  reducerErr,
		}
		return
	}

	// Scheduled-reducer firing semantics (Story 6.4, SPEC-003 §9.4).
	// On success, atomically delete (one-shot) or advance (repeating)
	// the sys_scheduled row in the same transaction as the reducer's
	// writes. A missing row is acceptable: it means a concurrent
	// Cancel raced the firing — the reducer still commits
	// (at-least-once semantics).
	if req.Source == CallSourceScheduled {
		if err := e.advanceOrDeleteSchedule(tx, req.ScheduleID, req.IntendedFireAt); err != nil {
			store.Rollback(tx)
			cmd.ResponseCh <- ReducerResponse{
				Status: StatusFailedInternal,
				Error:  fmt.Errorf("schedule advance: %w", err),
			}
			return
		}
	}

	// Commit.
	changeset, err := store.Commit(e.committed, tx)
	if err != nil {
		store.Rollback(tx)
		status := StatusFailedInternal
		if isUserCommitError(err) {
			status = StatusFailedUser
		}
		cmd.ResponseCh <- ReducerResponse{
			Status: status,
			Error:  fmt.Errorf("commit: %w", err),
		}
		return
	}
	txID := types.TxID(e.nextTxID)
	e.nextTxID++
	changeset.TxID = txID

	e.postCommit(txID, changeset, ret, cmd.ResponseCh)
}

// postCommit runs the ordered post-commit pipeline (SPEC-003 §5.1–§5.4,
// Stories 5.1–5.3):
//
//  1. hand the committed changeset to durability (queue admission, not fsync)
//  2. acquire a stable committed read view
//  3. evaluate subscriptions synchronously against that view
//  4. release the read view
//  5. deliver the reducer response to the caller
//  6. non-blocking drain of dropped-client signals
//
// Crash-loss semantics: the response is acknowledged before fsync, so a crash
// between step 5 and durability persistence may lose the transaction on
// restart. This is an allowed state per SPEC-003 §5.1.
//
// Fatal-state semantics (Story 5.3, SPEC-003 §5.4): the transaction is
// already visible in memory once commit returns. Any panic in steps 1–6
// leaves committed state that was never handed off for durability or
// evaluated for subscribers. The executor therefore latches a fatal flag
// and rejects future write-affecting commands until restart.
func (e *Executor) postCommit(
	txID types.TxID,
	changeset *store.Changeset,
	ret []byte,
	responseCh chan<- ReducerResponse,
) {
	responded := false
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		e.fatal = true
		log.Printf("executor: post-commit panic (txID=%d): %v\n%s", txID, r, debug.Stack())
		if responded {
			return
		}
		responseCh <- ReducerResponse{
			Status: StatusFailedInternal,
			Error:  fmt.Errorf("%w: post-commit panic: %v", ErrExecutorFatal, r),
			TxID:   txID,
		}
	}()

	e.durability.EnqueueCommitted(txID, changeset)
	view := e.snapshotFn()
	e.subs.EvalAndBroadcast(txID, changeset, view)
	view.Close()

	responseCh <- ReducerResponse{
		Status:      StatusCommitted,
		ReturnBSATN: ret,
		TxID:        txID,
	}
	responded = true

	// Step 6 (Story 5.2): non-blocking drop-client drain. Runs after
	// response delivery, before the next command is dequeued. A failing
	// DisconnectClient is logged and drain continues — one failed cleanup
	// must not block the others.
	dropped := e.subs.DroppedClients()
	for {
		select {
		case connID := <-dropped:
			if err := e.subs.DisconnectClient(connID); err != nil {
				log.Printf("executor: DisconnectClient(%v) failed: %v", connID, err)
			}
		default:
			return
		}
	}
}

func isUserCommitError(err error) bool {
	return errors.Is(err, store.ErrPrimaryKeyViolation) ||
		errors.Is(err, store.ErrUniqueConstraintViolation) ||
		errors.Is(err, store.ErrDuplicateRow)
}

type noopDurability struct{}

func (noopDurability) EnqueueCommitted(types.TxID, *store.Changeset) {}

type noopSubs struct{}

func (noopSubs) EvalAndBroadcast(types.TxID, *store.Changeset, store.CommittedReadView) {}
func (noopSubs) DroppedClients() <-chan types.ConnectionID                              { return nil }
func (noopSubs) DisconnectClient(types.ConnectionID) error                              { return nil }
