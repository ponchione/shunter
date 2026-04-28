package executor

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
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
	fatal      atomic.Bool
	rejectMode bool
	shutdown   atomic.Bool
	// externalReady guards SubmitWithContext — the spec-declared external
	// admission entrypoint (SPEC-003 §10.6, §13.5, Story 3.6). Flipped true
	// by Startup after scheduler replay and the dangling-client sweep
	// finish; until then external protocol traffic is rejected with
	// ErrExecutorNotStarted. Submit (the in-process/test entrypoint) is
	// deliberately ungated — embedder-direct callers own their ordering.
	externalReady atomic.Bool
	// startupOnce ensures Startup runs its replay + sweep sequence once.
	// Subsequent calls return nil without re-entering the sweep.
	startupOnce sync.Once
	submitMu    sync.RWMutex
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

type postCommitOptions struct {
	source          CallSource
	callerConnID    *types.ConnectionID
	callerRequestID uint32
	callerFlags     byte
	// Caller metadata populated for CallSourceExternal so the heavy
	// TransactionUpdate envelope can carry the reference
	// CallerIdentity / ReducerCallInfo fields. startTime captures the
	// reducer-dispatch instant — postCommit derives both the µs-since-Unix
	// `Timestamp` (reference `Timestamp` at sats/timestamp.rs:11-13) and
	// the wall-clock µs `TotalHostExecutionDuration` (reference
	// `TimeDuration` at sats/time_duration.rs:17-19) from it.
	callerIdentity types.Identity
	reducerName    string
	reducerID      uint32
	args           []byte
	startTime      time.Time
}

// NewExecutor creates an executor. Registry must be frozen.
func NewExecutor(cfg ExecutorConfig, reg *ReducerRegistry, cs *store.CommittedState, schemaReg schema.SchemaRegistry, recoveredTxID uint64) *Executor {
	if !reg.IsFrozen() {
		panic("executor: registry must be frozen before creating executor")
	}
	cs.SetCommittedTxID(types.TxID(recoveredTxID))
	capacity := cfg.InboxCapacity
	if capacity <= 0 {
		capacity = 256
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
	schedSeq := store.NewSequence()
	// Story 6.5: replayed sys_scheduled rows may already use
	// schedule_ids > 0. Reset the in-memory sequence past the max
	// existing id so post-restart Schedule() calls don't collide.
	if maxID := maxScheduleID(cs, schedTS.ID); maxID > 0 {
		schedSeq.Reset(uint64(maxID) + 1)
	}
	e := &Executor{
		inbox:        make(chan ExecutorCommand, capacity),
		registry:     reg,
		committed:    cs,
		schemaReg:    schemaReg,
		nextTxID:     recoveredTxID + 1,
		rejectMode:   cfg.RejectOnFull,
		schedTableID: schedTS.ID,
		schedSeq:     schedSeq,
		durability:   dur,
		subs:         subs,
		done:         make(chan struct{}),
	}
	e.snapshotFn = func() store.CommittedReadView { return e.committed.Snapshot() }
	return e
}

// Startup runs the executor-side startup sequence after recovery (SPEC-003
// §10.6, §13.5; Story 3.6 owner / Story 7.5 sweep):
//
//  1. scheduler.ReplayFromCommitted — repopulates the in-memory wakeup cache
//     from sys_scheduled and enqueues any past-due rows into the executor
//     inbox so they fire promptly once Run begins consuming it. Pass a nil
//     scheduler in tests / deployments that skip sys_scheduled replay.
//  2. dangling-client sweep — every surviving sys_clients row left by a
//     previous crash is deleted via a fresh cleanup transaction, reusing
//     Story 7.3's cleanup-only semantics (no OnDisconnect reducer is run;
//     the cleanup commit still fans out via the post-commit pipeline so
//     subscribers observe the sys_clients delete).
//  3. flip externalReady so SubmitWithContext starts admitting external
//     reducer / subscription-registration traffic from the protocol layer.
//
// Startup MUST complete before the caller starts Scheduler.Run / Executor.Run
// and before the protocol layer begins accepting connections. The full engine
// boot ordering is: recovery → NewExecutor → Startup → go Scheduler.Run →
// go Executor.Run → first protocol accept.
//
// Startup is idempotent: later calls are no-ops (first-call wins via
// sync.Once). If the sweep's post-commit pipeline latches executor-fatal
// mid-sequence, Startup returns the error and leaves externalReady false.
func (e *Executor) Startup(ctx context.Context, scheduler *Scheduler) error {
	var startupErr error
	e.startupOnce.Do(func() {
		if scheduler != nil {
			scheduler.ReplayFromCommitted()
		}
		if err := e.sweepDanglingClients(ctx); err != nil {
			startupErr = err
			return
		}
		e.externalReady.Store(true)
	})
	return startupErr
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
	e.submitMu.RLock()
	defer e.submitMu.RUnlock()
	if e.fatal.Load() {
		return ErrExecutorFatal
	}
	if e.shutdown.Load() {
		return ErrExecutorShutdown
	}
	if err := validateResponseChannels(cmd); err != nil {
		return err
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
//
// This is the external admission entrypoint used by the protocol adapter
// (SPEC-003 §10.6, §13.5, Story 3.6 / Story 7.5). The call is rejected with
// ErrExecutorNotStarted until Startup has finished scheduler replay and the
// dangling-client sweep — external reducer / subscription-registration work
// is not allowed to race ahead of either.
func (e *Executor) SubmitWithContext(ctx context.Context, cmd ExecutorCommand) error {
	e.submitMu.RLock()
	defer e.submitMu.RUnlock()
	if e.fatal.Load() {
		return ErrExecutorFatal
	}
	if e.shutdown.Load() {
		return ErrExecutorShutdown
	}
	if !e.externalReady.Load() {
		return ErrExecutorNotStarted
	}
	if err := validateResponseChannels(cmd); err != nil {
		return err
	}
	if e.rejectMode {
		select {
		case e.inbox <- cmd:
			return nil
		default:
			return ErrExecutorBusy
		}
	}
	select {
	case e.inbox <- cmd:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateResponseChannels(cmd ExecutorCommand) error {
	switch c := cmd.(type) {
	case CallReducerCmd:
		if isUnbufferedReducerResponseChannel(c.ResponseCh) || isUnbufferedProtocolReducerResponseChannel(c.ProtocolResponseCh) {
			return ErrExecutorUnbufferedResponseChannel
		}
	case DisconnectClientSubscriptionsCmd:
		if isUnbufferedErrorChannel(c.ResponseCh) {
			return ErrExecutorUnbufferedResponseChannel
		}
	case OnConnectCmd:
		if isUnbufferedReducerResponseChannel(c.ResponseCh) {
			return ErrExecutorUnbufferedResponseChannel
		}
	case OnDisconnectCmd:
		if isUnbufferedReducerResponseChannel(c.ResponseCh) {
			return ErrExecutorUnbufferedResponseChannel
		}
	}
	return nil
}

func isUnbufferedReducerResponseChannel(ch chan<- ReducerResponse) bool {
	return ch != nil && cap(ch) == 0
}

func isUnbufferedProtocolReducerResponseChannel(ch chan<- ProtocolCallReducerResponse) bool {
	return ch != nil && cap(ch) == 0
}

func isUnbufferedErrorChannel(ch chan<- error) bool {
	return ch != nil && cap(ch) == 0
}

// Shutdown stops accepting new commands and waits for Run to finish.
func (e *Executor) Shutdown() {
	e.shutdown.Store(true)
	e.submitMu.Lock()
	e.closeOnce.Do(func() { close(e.inbox) })
	e.submitMu.Unlock()
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
	if c, ok := cmd.(CallReducerCmd); ok {
		sendCallReducerResponse(c, ReducerResponse{
			Status: StatusFailedPanic,
			Error:  fmt.Errorf("reducer panicked: %v", r),
		}, nil)
	}
}

func (e *Executor) dispatch(cmd ExecutorCommand) {
	// Story 5.3: short-circuit write-affecting commands that were already in
	// the inbox when the executor latched into the fatal state. Submit
	// catches the common case; this catch covers the race window.
	if e.fatal.Load() {
		switch c := cmd.(type) {
		case CallReducerCmd:
			sendCallReducerResponse(c, ReducerResponse{Status: StatusFailedInternal, Error: ErrExecutorFatal}, nil)
		}
		return
	}
	switch c := cmd.(type) {
	case CallReducerCmd:
		e.handleCallReducer(c)
	case RegisterSubscriptionSetCmd:
		e.handleRegisterSubscriptionSet(c)
	case UnregisterSubscriptionSetCmd:
		e.handleUnregisterSubscriptionSet(c)
	case DisconnectClientSubscriptionsCmd:
		e.handleDisconnectClientSubscriptions(c)
	case OnConnectCmd:
		e.handleOnConnect(c)
	case OnDisconnectCmd:
		e.handleOnDisconnect(c)
	default:
		log.Printf("executor: unknown command type %T", cmd)
	}
}

func (e *Executor) handleRegisterSubscriptionSet(cmd RegisterSubscriptionSetCmd) {
	view := e.snapshotFn()
	defer view.Close()
	start := cmd.Receipt
	if start.IsZero() {
		start = time.Now()
	}
	req := cmd.Request
	if req.Context == nil {
		req.Context = context.Background()
	}
	res, err := e.subs.RegisterSet(req, view)
	durationMicros := uint64(time.Since(start).Microseconds())
	if durationMicros == 0 {
		durationMicros = 1
	}
	res.TotalHostExecutionDurationMicros = durationMicros
	if err != nil {
		log.Printf("executor: RegisterSubscriptionSet failed: %v", err)
	}
	if cmd.Reply != nil {
		// Synchronous invocation on the executor goroutine so the
		// caller's Applied/Error enqueue strictly precedes any
		// subsequent fan-out for the same connection (ADR §9.4).
		cmd.Reply(res, err)
	}
}

func (e *Executor) handleUnregisterSubscriptionSet(cmd UnregisterSubscriptionSetCmd) {
	view := e.snapshotFn()
	defer view.Close()
	start := cmd.Receipt
	if start.IsZero() {
		start = time.Now()
	}
	ctx := cmd.Context
	if ctx == nil {
		ctx = context.Background()
	}
	type unregisterSetContexter interface {
		UnregisterSetContext(context.Context, types.ConnectionID, uint32, store.CommittedReadView) (subscription.SubscriptionSetUnregisterResult, error)
	}
	var res subscription.SubscriptionSetUnregisterResult
	var err error
	if subs, ok := e.subs.(unregisterSetContexter); ok {
		res, err = subs.UnregisterSetContext(ctx, cmd.ConnID, cmd.QueryID, view)
	} else {
		res, err = e.subs.UnregisterSet(cmd.ConnID, cmd.QueryID, view)
	}
	durationMicros := uint64(time.Since(start).Microseconds())
	if durationMicros == 0 {
		durationMicros = 1
	}
	res.TotalHostExecutionDurationMicros = durationMicros
	if cmd.Reply != nil {
		// Synchronous invocation on the executor goroutine so the
		// caller's Applied/Error enqueue strictly precedes any
		// subsequent fan-out for the same connection (ADR §9.4).
		cmd.Reply(res, err)
	}
}

func (e *Executor) handleDisconnectClientSubscriptions(cmd DisconnectClientSubscriptionsCmd) {
	cmd.ResponseCh <- e.subs.DisconnectClient(cmd.ConnID)
}

type reducerDBAdapter struct {
	mu     sync.Mutex
	closed bool
	tx     *store.Transaction
}

func (d *reducerDBAdapter) close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
}

func (d *reducerDBAdapter) checkOpenLocked() error {
	if d.closed || d.tx == nil {
		return store.ErrTransactionClosed
	}
	return nil
}

func (d *reducerDBAdapter) Insert(tableID uint32, row types.ProductValue) (types.RowID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkOpenLocked(); err != nil {
		return 0, err
	}
	return d.tx.Insert(schema.TableID(tableID), row)
}

func (d *reducerDBAdapter) Delete(tableID uint32, rowID types.RowID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkOpenLocked(); err != nil {
		return err
	}
	return d.tx.Delete(schema.TableID(tableID), rowID)
}

func (d *reducerDBAdapter) Update(tableID uint32, rowID types.RowID, newRow types.ProductValue) (types.RowID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkOpenLocked(); err != nil {
		return 0, err
	}
	return d.tx.Update(schema.TableID(tableID), rowID, newRow)
}

func (d *reducerDBAdapter) GetRow(tableID uint32, rowID types.RowID) (types.ProductValue, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkOpenLocked(); err != nil {
		return nil, false
	}
	return d.tx.GetRow(schema.TableID(tableID), rowID)
}

func (d *reducerDBAdapter) ScanTable(tableID uint32) iter.Seq2[types.RowID, types.ProductValue] {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkOpenLocked(); err != nil {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	type entry struct {
		id  types.RowID
		row types.ProductValue
	}
	var rows []entry
	for id, row := range d.tx.ScanTable(schema.TableID(tableID)) {
		rows = append(rows, entry{id: id, row: row})
	}
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for _, row := range rows {
			if !yield(row.id, row.row) {
				return
			}
		}
	}
}

func (d *reducerDBAdapter) Underlying() any {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkOpenLocked(); err != nil {
		return nil
	}
	return d.tx
}

func sendReducerResponse(ch chan<- ReducerResponse, resp ReducerResponse) bool {
	if ch == nil {
		return true
	}
	ch <- resp
	return true
}

func sendProtocolCallReducerResponse(ch chan<- ProtocolCallReducerResponse, resp ProtocolCallReducerResponse) bool {
	if ch == nil {
		return true
	}
	ch <- resp
	return true
}

func sendCallReducerResponse(cmd CallReducerCmd, resp ReducerResponse, committed *CommittedCallerPayload) bool {
	responded := sendReducerResponse(cmd.ResponseCh, resp)
	protocolResponded := sendProtocolCallReducerResponse(cmd.ProtocolResponseCh, ProtocolCallReducerResponse{
		Reducer:   resp,
		Committed: committed,
	})
	return responded && protocolResponded
}

func (e *Executor) handleCallReducer(cmd CallReducerCmd) {
	req := cmd.Request
	start := time.Now()
	if req.Source != CallSourceLifecycle {
		if _, reserved := lifecycleNames[req.ReducerName]; reserved {
			sendCallReducerResponse(cmd, ReducerResponse{
				Status: StatusFailedInternal,
				Error:  ErrLifecycleReducer,
			}, nil)
			return
		}
	}

	// Lookup reducer.
	rr, ok := e.registry.Lookup(req.ReducerName)
	if !ok {
		sendCallReducerResponse(cmd, ReducerResponse{
			Status: StatusFailedInternal,
			Error:  fmt.Errorf("%w: %s", ErrReducerNotFound, req.ReducerName),
		}, nil)
		return
	}

	// Begin: create transaction + context.
	caller := types.CallerContext{
		Identity:     req.Caller.Identity,
		ConnectionID: req.Caller.ConnectionID,
		Timestamp:    time.Now().UTC(),
	}
	tx := store.NewTransaction(e.committed, e.schemaReg)
	db := &reducerDBAdapter{tx: tx}
	scheduler := e.newSchedulerHandle(tx)
	rctx := &types.ReducerContext{
		ReducerName: req.ReducerName,
		Caller:      caller,
		DB:          db,
		Scheduler:   scheduler,
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
	db.close()
	scheduler.close()

	// Decision routing.
	if panicked != nil {
		store.Rollback(tx)
		panicErr := func(r any) error {
			if err, ok := r.(error); ok {
				return errors.Join(ErrReducerPanic, err)
			}
			return fmt.Errorf("%v: %w", r, ErrReducerPanic)
		}(panicked)
		sendCallReducerResponse(cmd, ReducerResponse{
			Status: StatusFailedPanic,
			Error:  panicErr,
		}, nil)
		return
	}

	if reducerErr != nil {
		store.Rollback(tx)
		sendCallReducerResponse(cmd, ReducerResponse{
			Status: StatusFailedUser,
			Error:  reducerErr,
		}, nil)
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
			sendCallReducerResponse(cmd, ReducerResponse{
				Status: StatusFailedInternal,
				Error:  fmt.Errorf("schedule advance: %w", err),
			}, nil)
			return
		}
	}

	// Commit.
	tx.Seal()
	changeset, err := store.Commit(e.committed, tx)
	if err != nil {
		store.Rollback(tx)
		status := StatusFailedInternal
		if isUserCommitError(err) {
			status = StatusFailedUser
		}
		sendCallReducerResponse(cmd, ReducerResponse{
			Status: status,
			Error:  fmt.Errorf("commit: %w", err),
		}, nil)
		return
	}
	txID := types.TxID(e.nextTxID)
	e.nextTxID++
	changeset.TxID = txID
	e.committed.SetCommittedTxID(txID)

	var opts postCommitOptions
	opts.source = req.Source
	if req.Source == CallSourceExternal {
		connID := req.Caller.ConnectionID
		opts.callerConnID = &connID
		opts.callerRequestID = req.RequestID
		opts.callerFlags = req.Flags
		opts.callerIdentity = req.Caller.Identity
		opts.reducerName = req.ReducerName
		opts.reducerID = rr.ID
		opts.args = req.Args
		opts.startTime = start
	}
	e.postCommit(txID, changeset, ret, cmd, opts)
}

// postCommit runs the ordered post-commit pipeline (SPEC-003 §5.1–§5.4,
// Stories 5.1–5.3):
//
//  1. hand the committed changeset to durability (queue admission, not fsync)
//  2. acquire a stable committed read view and defer its release
//  3. evaluate subscriptions synchronously against that view
//  4. deliver the reducer response to the caller
//  5. non-blocking drain of dropped-client signals
//
// Crash-loss semantics: the response is acknowledged before fsync, so a crash
// after response delivery but before durability persistence may lose the
// transaction on restart. This is an allowed state per SPEC-003 §5.1.
//
// Fatal-state semantics (Story 5.3, SPEC-003 §5.4): the transaction is
// already visible in memory once commit returns. Any panic in the post-commit
// pipeline leaves committed state that may not have been durably handed off or
// evaluated for subscribers. The executor therefore latches a fatal flag
// and rejects future write-affecting commands until restart.
func (e *Executor) postCommit(
	txID types.TxID,
	changeset *store.Changeset,
	ret []byte,
	cmd CallReducerCmd,
	opts postCommitOptions,
) {
	responded := cmd.ResponseCh == nil && cmd.ProtocolResponseCh == nil
	var committedPayload *CommittedCallerPayload
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		e.fatal.Store(true)
		log.Printf("executor: post-commit panic (txID=%d): %v\n%s", txID, r, debug.Stack())
		if responded {
			return
		}
		sendCallReducerResponse(cmd, ReducerResponse{
			Status: StatusFailedInternal,
			Error:  fmt.Errorf("%w: post-commit panic: %v", ErrExecutorFatal, r),
			TxID:   txID,
		}, nil)
	}()

	e.durability.EnqueueCommitted(txID, changeset)
	view := e.snapshotFn()
	defer view.Close()
	meta := subscription.PostCommitMeta{TxDurable: e.durability.WaitUntilDurable(txID)}
	if opts.source == CallSourceExternal && opts.callerConnID != nil {
		callerConnID := *opts.callerConnID
		callerOutcome := subscription.CallerOutcome{
			Kind:                       subscription.CallerOutcomeCommitted,
			RequestID:                  opts.callerRequestID,
			Flags:                      opts.callerFlags,
			CallerIdentity:             opts.callerIdentity,
			ReducerName:                opts.reducerName,
			ReducerID:                  opts.reducerID,
			Args:                       opts.args,
			Timestamp:                  opts.startTime.UnixMicro(),
			TotalHostExecutionDuration: time.Since(opts.startTime).Microseconds(),
		}
		if cmd.ProtocolResponseCh != nil {
			// For protocol-originated reducer calls the protocol inbox adapter owns
			// the caller-visible heavy reply. Keep the caller out of light fan-out,
			// capture its authoritative update slice from evaluation, but do not
			// export CallerOutcome into the fan-out worker or it will deliver a
			// second heavy envelope for the same commit.
			meta.CallerConnID = &callerConnID
			committedPayload = &CommittedCallerPayload{Outcome: callerOutcome}
			meta.CaptureCallerUpdates = func(updates []subscription.SubscriptionUpdate) {
				committedPayload.Updates = append([]subscription.SubscriptionUpdate(nil), updates...)
			}
		} else {
			// Non-protocol external callers keep the original fan-out-owned caller
			// heavy delivery path.
			meta.CallerConnID = &callerConnID
			meta.CallerOutcome = &callerOutcome
		}
	}
	e.subs.EvalAndBroadcast(txID, changeset, view, meta)

	responded = sendCallReducerResponse(cmd, ReducerResponse{
		Status:      StatusCommitted,
		ReturnBSATN: ret,
		TxID:        txID,
	}, committedPayload)

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
func (noopDurability) WaitUntilDurable(types.TxID) <-chan types.TxID { return nil }

type noopSubs struct{}

func (noopSubs) RegisterSet(subscription.SubscriptionSetRegisterRequest, store.CommittedReadView) (subscription.SubscriptionSetRegisterResult, error) {
	return subscription.SubscriptionSetRegisterResult{}, nil
}
func (noopSubs) UnregisterSet(types.ConnectionID, uint32, store.CommittedReadView) (subscription.SubscriptionSetUnregisterResult, error) {
	return subscription.SubscriptionSetUnregisterResult{}, nil
}
func (noopSubs) EvalAndBroadcast(types.TxID, *store.Changeset, store.CommittedReadView, subscription.PostCommitMeta) {
}
func (noopSubs) DroppedClients() <-chan types.ConnectionID { return nil }
func (noopSubs) DisconnectClient(types.ConnectionID) error { return nil }
