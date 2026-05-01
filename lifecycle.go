package shunter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// RuntimeState names the public lifecycle state reported by Runtime.Health.
type RuntimeState string

const (
	// RuntimeStateBuilt means Build completed but Start has not yet made the runtime ready.
	RuntimeStateBuilt RuntimeState = "built"
	// RuntimeStateStarting means one goroutine is currently running Start.
	RuntimeStateStarting RuntimeState = "starting"
	// RuntimeStateReady means Start completed and runtime-owned workers are running.
	RuntimeStateReady RuntimeState = "ready"
	// RuntimeStateClosing means Close is shutting down runtime-owned workers.
	RuntimeStateClosing RuntimeState = "closing"
	// RuntimeStateClosed means the runtime has been closed and cannot be restarted.
	RuntimeStateClosed RuntimeState = "closed"
	// RuntimeStateFailed means the last lifecycle operation failed.
	RuntimeStateFailed RuntimeState = "failed"
)

var (
	// ErrRuntimeStarting reports that another goroutine is already starting the runtime.
	ErrRuntimeStarting = errors.New("shunter: runtime is starting")
	// ErrRuntimeClosed reports that the runtime has already been closed.
	ErrRuntimeClosed = errors.New("shunter: runtime is closed")
)

var runtimeStartAfterDurabilityHook func(*Runtime) error

// Ready reports whether Start has completed and runtime-owned workers are running.
func (r *Runtime) Ready() bool {
	return r.ready.Load()
}

// Start performs runtime startup and returns once background ownership is ready.
// The supplied context is a startup/cancellation context only; canceling it
// after Start returns does not stop the runtime. Use Close for shutdown.
func (r *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	startedAt := time.Now()

	r.mu.Lock()
	switch r.stateName {
	case RuntimeStateReady:
		r.mu.Unlock()
		return nil
	case RuntimeStateStarting:
		r.mu.Unlock()
		return ErrRuntimeStarting
	case RuntimeStateClosing, RuntimeStateClosed:
		r.mu.Unlock()
		return ErrRuntimeClosed
	}
	r.stateName = RuntimeStateStarting
	r.lastErr = nil
	r.ready.Store(false)
	r.mu.Unlock()
	r.recordRuntimeMetrics()

	if err := ctx.Err(); err != nil {
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}
	if err := r.engine.Start(ctx); err != nil {
		err = fmt.Errorf("start schema engine: %w", err)
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}
	if _, _, err := buildAuthConfig(r.config); err != nil {
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}
	if _, err := buildProtocolOptions(r.config.Protocol); err != nil {
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}

	durabilityOptions := commitlog.DefaultCommitLogOptions()
	durabilityOptions.ChannelCapacity = r.buildConfig.DurabilityQueueCapacity
	durabilityOptions.Observer = r.observability
	durability, err := commitlog.NewDurabilityWorkerWithResumePlan(r.dataDir, r.resumePlan, durabilityOptions)
	if err != nil {
		err = fmt.Errorf("start durability worker: %w", err)
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}
	cleanupDurability := true
	defer func() {
		if cleanupDurability {
			_, _ = durability.Close()
		}
	}()

	if runtimeStartAfterDurabilityHook != nil {
		if err := runtimeStartAfterDurabilityHook(r); err != nil {
			err = fmt.Errorf("start runtime lifecycle: %w", err)
			r.recordStartFailure(ctx, err, time.Since(startedAt))
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}

	fanOutCapacity := r.buildConfig.ExecutorQueueCapacity
	fanOutInbox := make(chan subscription.FanOutMessage, fanOutCapacity)
	subscriptions := subscription.NewManager(
		r.registry,
		r.registry,
		subscription.WithFanOutInbox(fanOutInbox),
		subscription.WithObserver(r.observability),
	)
	exec := executor.NewExecutor(executor.ExecutorConfig{
		InboxCapacity: r.buildConfig.ExecutorQueueCapacity,
		Durability:    durabilityHandle{worker: durability},
		Subscriptions: subscriptions,
		RejectOnFull:  false,
		Observer:      r.observability,
	}, r.reducers, r.state, r.registry, uint64(r.recoveredTxID))
	scheduler := exec.SchedulerFor()
	if err := exec.Startup(ctx, scheduler); err != nil {
		err = fmt.Errorf("startup executor: %w", err)
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}

	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	fanOutCtx, fanOutCancel := context.WithCancel(context.Background())
	fanOutSender := newSwappableFanOutSender(noopFanOutSender{})
	fanOutWorker := subscription.NewFanOutWorkerWithObserver(
		fanOutInbox,
		fanOutSender,
		subscriptions.DroppedChanSend(),
		subscriptions.RecordDroppedClient,
		r.observability,
	)

	r.mu.Lock()
	if r.stateName == RuntimeStateClosed || r.stateName == RuntimeStateClosing {
		r.mu.Unlock()
		lifecycleCancel()
		fanOutCancel()
		r.recordStartFailure(ctx, ErrRuntimeClosed, time.Since(startedAt))
		return ErrRuntimeClosed
	}
	r.lifecycleCancel = lifecycleCancel
	r.fanOutCancel = fanOutCancel
	r.durability = durability
	r.subscriptions = subscriptions
	r.fanOutInbox = fanOutInbox
	r.fanOutWorker = fanOutWorker
	r.fanOutSender = fanOutSender
	r.executor = exec
	r.scheduler = scheduler
	if err := r.ensureProtocolGraphLocked(); err != nil {
		r.lifecycleCancel = nil
		r.fanOutCancel = nil
		r.durability = nil
		r.subscriptions = nil
		r.fanOutInbox = nil
		r.fanOutWorker = nil
		r.fanOutSender = nil
		r.executor = nil
		r.scheduler = nil
		r.protocolConns = nil
		r.protocolInbox = nil
		r.protocolSender = nil
		r.protocolServer = nil
		r.mu.Unlock()
		lifecycleCancel()
		fanOutCancel()
		r.recordStartFailure(ctx, err, time.Since(startedAt))
		return err
	}
	r.schedulerWG.Add(1)
	r.fanOutWG.Add(1)
	r.stateName = RuntimeStateReady
	r.ready.Store(true)
	r.mu.Unlock()

	go exec.Run(context.Background())
	go func() {
		defer r.schedulerWG.Done()
		scheduler.Run(lifecycleCtx)
	}()
	go func() {
		defer r.fanOutWG.Done()
		fanOutWorker.Run(fanOutCtx)
	}()

	cleanupDurability = false
	r.recordStartReady(ctx, time.Since(startedAt))
	return nil
}

// Close idempotently shuts down runtime-owned background workers.
func (r *Runtime) Close() error {
	startedAt := time.Now()
	r.closeMu.Lock()
	defer r.closeMu.Unlock()

	r.mu.Lock()
	if r.stateName == RuntimeStateClosed {
		r.mu.Unlock()
		return nil
	}
	if r.stateName == RuntimeStateBuilt || r.stateName == RuntimeStateFailed {
		r.stateName = RuntimeStateClosed
		r.ready.Store(false)
		r.mu.Unlock()
		r.recordClosed(time.Since(startedAt))
		return nil
	}
	r.stateName = RuntimeStateClosing
	r.ready.Store(false)
	lifecycleCancel := r.lifecycleCancel
	fanOutCancel := r.fanOutCancel
	exec := r.executor
	durability := r.durability
	subscriptions := r.subscriptions
	protocolConns := r.protocolConns
	protocolInbox := r.protocolInbox
	r.mu.Unlock()
	r.recordRuntimeMetrics()

	r.closeProtocolGraph(protocolConns, protocolInbox)
	if lifecycleCancel != nil {
		lifecycleCancel()
	}
	if fanOutCancel != nil {
		fanOutCancel()
	}
	r.schedulerWG.Wait()
	r.fanOutWG.Wait()
	executorFatal := false
	if exec != nil {
		exec.Shutdown()
		executorFatal = exec.Fatal()
	}
	var (
		closeErr            error
		finalDurableTxID    uint64
		acceptedConnections uint64
		rejectedConnections uint64
		droppedClients      uint64
	)
	if protocolConns != nil {
		acceptedConnections = protocolConns.AcceptedCount()
		rejectedConnections = protocolConns.RejectedCount()
	}
	if subscriptions != nil {
		droppedClients = subscriptions.DroppedClientCount()
		r.observability.RecordSubscriptionActive(0)
	}
	if durability != nil {
		finalDurableTxID, closeErr = durability.Close()
	}

	r.mu.Lock()
	if finalDurableTxID > 0 || durability != nil {
		r.durableTxID = types.TxID(finalDurableTxID)
	}
	if closeErr != nil {
		r.durabilityFatalErr = closeErr
	}
	if executorFatal {
		r.executorFatal = true
	}
	if acceptedConnections > r.protocolAcceptedConnections {
		r.protocolAcceptedConnections = acceptedConnections
	}
	if rejectedConnections > r.protocolRejectedConnections {
		r.protocolRejectedConnections = rejectedConnections
	}
	if droppedClients > r.subscriptionDroppedClients {
		r.subscriptionDroppedClients = droppedClients
	}
	r.lifecycleCancel = nil
	r.fanOutCancel = nil
	r.durability = nil
	r.subscriptions = nil
	r.fanOutInbox = nil
	r.fanOutWorker = nil
	r.fanOutSender = nil
	r.executor = nil
	r.scheduler = nil
	r.protocolConns = nil
	r.protocolInbox = nil
	r.protocolSender = nil
	r.protocolServer = nil
	r.lastErr = closeErr
	r.stateName = RuntimeStateClosed
	r.ready.Store(false)
	r.mu.Unlock()
	if closeErr != nil {
		r.recordCloseFailure(closeErr, time.Since(startedAt))
	} else {
		r.recordClosed(time.Since(startedAt))
	}
	return closeErr
}

func (r *Runtime) recordStartFailure(ctx context.Context, err error, duration time.Duration) {
	transitionedFailed := false
	r.mu.Lock()
	r.lastErr = err
	r.ready.Store(false)
	if r.stateName != RuntimeStateClosed && r.stateName != RuntimeStateClosing {
		r.stateName = RuntimeStateFailed
		transitionedFailed = true
	}
	r.mu.Unlock()
	health := r.Health()
	r.observability.recordRuntimeHealthMetrics(health)
	if transitionedFailed {
		r.observability.recordRuntimeError("start_failed")
	}
	r.observability.recordRuntimeStartFailed(ctx, err, duration)
	r.observability.recordRuntimeHealthDegraded(health, runtimePrimaryDegradedReason(health))
}

func (r *Runtime) recordStartReady(ctx context.Context, duration time.Duration) {
	health := r.Health()
	r.observability.recordRuntimeHealthMetrics(health)
	r.observability.recordRuntimeReady(ctx, health, duration)
	r.observability.recordRuntimeHealthDegraded(health, runtimePrimaryDegradedReason(health))
}

func (r *Runtime) recordCloseFailure(err error, duration time.Duration) {
	if err == nil {
		return
	}
	health := r.Health()
	r.observability.recordRuntimeHealthMetrics(health)
	r.observability.recordRuntimeError("close_failed")
	r.observability.recordRuntimeCloseFailed(err, duration)
	r.observability.recordRuntimeHealthDegraded(health, runtimePrimaryDegradedReason(health))
}

func (r *Runtime) recordClosed(duration time.Duration) {
	r.recordRuntimeMetrics()
	r.observability.recordRuntimeClosed(RuntimeStateClosed, duration)
}

func (r *Runtime) recordRuntimeMetrics() {
	r.observability.recordRuntimeHealthMetrics(r.Health())
}

type durabilityHandle struct {
	worker *commitlog.DurabilityWorker
}

func (h durabilityHandle) EnqueueCommitted(txID types.TxID, changeset *store.Changeset) {
	h.worker.EnqueueCommitted(uint64(txID), changeset)
}

func (h durabilityHandle) WaitUntilDurable(txID types.TxID) <-chan types.TxID {
	return h.worker.WaitUntilDurable(txID)
}

// noopFanOutSender is V1-D-only internal delivery plumbing. V1-E replaces or
// wraps this with protocol-backed fan-out delivery when network serving exists.
type noopFanOutSender struct{}

func (noopFanOutSender) SendTransactionUpdateHeavy(types.ConnectionID, subscription.CallerOutcome, []subscription.SubscriptionUpdate, *subscription.EncodingMemo) error {
	return nil
}

func (noopFanOutSender) SendTransactionUpdateLight(types.ConnectionID, uint32, []subscription.SubscriptionUpdate, *subscription.EncodingMemo) error {
	return nil
}

func (noopFanOutSender) SendSubscriptionError(types.ConnectionID, subscription.SubscriptionError) error {
	return nil
}
