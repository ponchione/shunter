package shunter

import (
	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// RuntimeHealth is a detached lifecycle, readiness, and subsystem snapshot.
type RuntimeHealth struct {
	State     RuntimeState `json:"state"`
	Ready     bool         `json:"ready"`
	Degraded  bool         `json:"degraded"`
	LastError string       `json:"last_error,omitempty"`

	Executor      ExecutorHealth     `json:"executor"`
	Durability    DurabilityHealth   `json:"durability"`
	Protocol      ProtocolHealth     `json:"protocol"`
	Subscriptions SubscriptionHealth `json:"subscriptions"`
	Recovery      RecoveryHealth     `json:"recovery"`
}

// ExecutorHealth reports executor admission and queue facts.
type ExecutorHealth struct {
	Started        bool   `json:"started"`
	AdmissionReady bool   `json:"admission_ready"`
	InboxDepth     int    `json:"inbox_depth"`
	InboxCapacity  int    `json:"inbox_capacity"`
	Fatal          bool   `json:"fatal"`
	FatalError     string `json:"fatal_error,omitempty"`
}

// DurabilityHealth reports commit-log durability worker facts.
type DurabilityHealth struct {
	Started       bool       `json:"started"`
	DurableTxID   types.TxID `json:"durable_tx_id"`
	QueueDepth    int        `json:"queue_depth"`
	QueueCapacity int        `json:"queue_capacity"`
	Fatal         bool       `json:"fatal"`
	FatalError    string     `json:"fatal_error,omitempty"`
}

// ProtocolHealth reports protocol serving graph and connection facts.
type ProtocolHealth struct {
	Enabled             bool   `json:"enabled"`
	Ready               bool   `json:"ready"`
	ActiveConnections   int    `json:"active_connections"`
	AcceptedConnections uint64 `json:"accepted_connections"`
	RejectedConnections uint64 `json:"rejected_connections"`
	LastError           string `json:"last_error,omitempty"`
}

// SubscriptionHealth reports subscription manager and fan-out facts.
type SubscriptionHealth struct {
	Started             bool   `json:"started"`
	ActiveSubscriptions int    `json:"active_subscriptions"`
	DroppedClients      uint64 `json:"dropped_clients"`
	FanoutQueueDepth    int    `json:"fanout_queue_depth"`
	FanoutQueueCapacity int    `json:"fanout_queue_capacity"`
	FanoutFatal         bool   `json:"fanout_fatal"`
	FanoutFatalError    string `json:"fanout_fatal_error,omitempty"`
}

// RecoveryHealth reports durable-state recovery facts observed during Build.
type RecoveryHealth struct {
	Ran                  bool       `json:"ran"`
	Succeeded            bool       `json:"succeeded"`
	HasSelectedSnapshot  bool       `json:"has_selected_snapshot"`
	SelectedSnapshotTxID types.TxID `json:"selected_snapshot_tx_id"`
	HasDurableLog        bool       `json:"has_durable_log"`
	DurableLogHorizon    types.TxID `json:"durable_log_horizon"`
	ReplayedFromTxID     types.TxID `json:"replayed_from_tx_id"`
	ReplayedToTxID       types.TxID `json:"replayed_to_tx_id"`
	RecoveredTxID        types.TxID `json:"recovered_tx_id"`
	DamagedTailSegments  int        `json:"damaged_tail_segments"`
	SkippedSnapshots     int        `json:"skipped_snapshots"`
	LastError            string     `json:"last_error,omitempty"`
}

type runtimeDegradedReason string

const (
	runtimeDegradedReasonNone                runtimeDegradedReason = ""
	runtimeDegradedReasonRuntimeFailed       runtimeDegradedReason = "runtime_failed"
	runtimeDegradedReasonExecutorFatal       runtimeDegradedReason = "executor_fatal"
	runtimeDegradedReasonDurabilityFatal     runtimeDegradedReason = "durability_fatal"
	runtimeDegradedReasonFanoutFatal         runtimeDegradedReason = "fanout_fatal"
	runtimeDegradedReasonRecoveryDamagedTail runtimeDegradedReason = "recovery_damaged_tail"
	runtimeDegradedReasonRecoverySkipped     runtimeDegradedReason = "recovery_skipped_snapshot"
	runtimeDegradedReasonProtocolNotReady    runtimeDegradedReason = "protocol_not_ready"
)

type runtimeHealthSnapshot struct {
	state          RuntimeState
	ready          bool
	lastErr        error
	buildConfig    Config
	observability  *runtimeObservability
	durableTxID    types.TxID
	recovery       runtimeRecoveryFacts
	executor       *executor.Executor
	durability     *commitlog.DurabilityWorker
	subscriptions  *subscription.Manager
	fanOutInbox    chan subscription.FanOutMessage
	fanOutWorker   *subscription.FanOutWorker
	protocolConns  *protocol.ConnManager
	protocolInbox  *executor.ProtocolInboxAdapter
	protocolServer *protocol.Server

	executorFatal              bool
	executorFatalErr           error
	durabilityFatalErr         error
	protocolLastErr            error
	fanoutFatalErr             error
	protocolAcceptedConnection uint64
	protocolRejectedConnection uint64
	subscriptionDroppedClients uint64
}

// Health returns a detached lifecycle/readiness snapshot.
func (r *Runtime) Health() RuntimeHealth {
	if r == nil {
		return runtimeNotConfiguredHealth()
	}
	snap := r.healthSnapshot()
	health := buildRuntimeHealth(snap)
	health.Degraded = runtimePrimaryDegradedReason(health) != runtimeDegradedReasonNone
	return health
}

func (r *Runtime) healthSnapshot() runtimeHealthSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return runtimeHealthSnapshot{
		state:                      r.stateName,
		ready:                      r.ready.Load(),
		lastErr:                    r.lastErr,
		buildConfig:                r.buildConfig,
		observability:              r.observability,
		durableTxID:                r.durableTxID,
		recovery:                   r.recovery,
		executor:                   r.executor,
		durability:                 r.durability,
		subscriptions:              r.subscriptions,
		fanOutInbox:                r.fanOutInbox,
		fanOutWorker:               r.fanOutWorker,
		protocolConns:              r.protocolConns,
		protocolInbox:              r.protocolInbox,
		protocolServer:             r.protocolServer,
		executorFatal:              r.executorFatal,
		executorFatalErr:           r.executorFatalErr,
		durabilityFatalErr:         r.durabilityFatalErr,
		protocolLastErr:            r.protocolLastErr,
		fanoutFatalErr:             r.fanoutFatalErr,
		protocolAcceptedConnection: r.protocolAcceptedConnections,
		protocolRejectedConnection: r.protocolRejectedConnections,
		subscriptionDroppedClients: r.subscriptionDroppedClients,
	}
}

func buildRuntimeHealth(snap runtimeHealthSnapshot) RuntimeHealth {
	recovery := buildRecoveryHealth(snap.recovery, snap.observability)
	durableTxID := snap.durableTxID
	if durableTxID == 0 {
		durableTxID = recovery.RecoveredTxID
	}

	durabilityFatalErr := snap.durabilityFatalErr
	durabilityDepth := 0
	if snap.durability != nil {
		if current := types.TxID(snap.durability.DurableTxID()); current > durableTxID {
			durableTxID = current
		}
		durabilityDepth = snap.durability.QueueDepth()
		if err := snap.durability.FatalError(); err != nil {
			durabilityFatalErr = err
		}
	}

	executorFatal := snap.executorFatal || snap.executorFatalErr != nil
	executorDepth := 0
	executorCanAdmit := false
	if snap.executor != nil {
		executorDepth = snap.executor.InboxDepth()
		if snap.executor.Fatal() {
			executorFatal = true
		}
		executorCanAdmit = snap.executor.ExternalReady() && !snap.executor.ShutdownStarted() && !executorFatal
	}

	fanoutFatal := snap.fanoutFatalErr != nil
	coreReady := snap.state == RuntimeStateReady &&
		snap.ready &&
		executorCanAdmit &&
		snap.durability != nil &&
		durabilityFatalErr == nil &&
		snap.subscriptions != nil &&
		snap.fanOutWorker != nil &&
		!fanoutFatal

	protocolActive := 0
	protocolAccepted := snap.protocolAcceptedConnection
	protocolRejected := snap.protocolRejectedConnection
	if snap.protocolConns != nil {
		protocolActive = snap.protocolConns.ActiveCount()
		if accepted := snap.protocolConns.AcceptedCount(); accepted > protocolAccepted {
			protocolAccepted = accepted
		}
		if rejected := snap.protocolConns.RejectedCount(); rejected > protocolRejected {
			protocolRejected = rejected
		}
	}

	activeSubscriptions := 0
	droppedClients := snap.subscriptionDroppedClients
	if snap.subscriptions != nil {
		activeSubscriptions = snap.subscriptions.ActiveSubscriptionSets()
		if dropped := snap.subscriptions.DroppedClientCount(); dropped > droppedClients {
			droppedClients = dropped
		}
	}

	fanoutDepth := 0
	if snap.fanOutInbox != nil {
		fanoutDepth = len(snap.fanOutInbox)
	}

	return RuntimeHealth{
		State:     snap.state,
		Ready:     coreReady,
		LastError: redactHealthError(snap.observability, snap.lastErr),
		Executor: ExecutorHealth{
			Started:        snap.state == RuntimeStateReady && snap.executor != nil,
			AdmissionReady: coreReady,
			InboxDepth:     executorDepth,
			InboxCapacity:  snap.buildConfig.ExecutorQueueCapacity,
			Fatal:          executorFatal,
			FatalError:     redactHealthError(snap.observability, snap.executorFatalErr),
		},
		Durability: DurabilityHealth{
			Started:       snap.state == RuntimeStateReady && snap.durability != nil,
			DurableTxID:   durableTxID,
			QueueDepth:    durabilityDepth,
			QueueCapacity: snap.buildConfig.DurabilityQueueCapacity,
			Fatal:         durabilityFatalErr != nil,
			FatalError:    redactHealthError(snap.observability, durabilityFatalErr),
		},
		Protocol: ProtocolHealth{
			Enabled:             snap.buildConfig.EnableProtocol,
			Ready:               snap.buildConfig.EnableProtocol && coreReady && snap.protocolConns != nil && snap.protocolInbox != nil && snap.protocolServer != nil,
			ActiveConnections:   protocolActive,
			AcceptedConnections: protocolAccepted,
			RejectedConnections: protocolRejected,
			LastError:           redactHealthError(snap.observability, snap.protocolLastErr),
		},
		Subscriptions: SubscriptionHealth{
			Started:             snap.state == RuntimeStateReady && snap.subscriptions != nil,
			ActiveSubscriptions: activeSubscriptions,
			DroppedClients:      droppedClients,
			FanoutQueueDepth:    fanoutDepth,
			FanoutQueueCapacity: snap.buildConfig.ExecutorQueueCapacity,
			FanoutFatal:         fanoutFatal,
			FanoutFatalError:    redactHealthError(snap.observability, snap.fanoutFatalErr),
		},
		Recovery: recovery,
	}
}

func buildRecoveryHealth(facts runtimeRecoveryFacts, observability *runtimeObservability) RecoveryHealth {
	report := facts.report
	return RecoveryHealth{
		Ran:                  facts.ran,
		Succeeded:            facts.succeeded,
		HasSelectedSnapshot:  report.HasSelectedSnapshot,
		SelectedSnapshotTxID: report.SelectedSnapshotTxID,
		HasDurableLog:        report.HasDurableLog,
		DurableLogHorizon:    report.DurableLogHorizon,
		ReplayedFromTxID:     report.ReplayedTxRange.Start,
		ReplayedToTxID:       report.ReplayedTxRange.End,
		RecoveredTxID:        report.RecoveredTxID,
		DamagedTailSegments:  len(report.DamagedTailSegments),
		SkippedSnapshots:     len(report.SkippedSnapshots),
		LastError:            redactHealthError(observability, facts.lastErr),
	}
}

func runtimeNotConfiguredHealth() RuntimeHealth {
	health := RuntimeHealth{
		State:     RuntimeStateFailed,
		Ready:     false,
		LastError: "runtime is not configured",
	}
	health.Degraded = runtimePrimaryDegradedReason(health) != runtimeDegradedReasonNone
	return health
}

func redactHealthError(observability *runtimeObservability, err error) string {
	if err == nil {
		return ""
	}
	return observability.redactError(err)
}

func runtimePrimaryDegradedReason(health RuntimeHealth) runtimeDegradedReason {
	switch {
	case health.State == RuntimeStateFailed:
		return runtimeDegradedReasonRuntimeFailed
	case health.Executor.Fatal:
		return runtimeDegradedReasonExecutorFatal
	case health.Durability.Fatal:
		return runtimeDegradedReasonDurabilityFatal
	case health.Subscriptions.FanoutFatal:
		return runtimeDegradedReasonFanoutFatal
	case health.Recovery.Succeeded && health.Recovery.DamagedTailSegments > 0:
		return runtimeDegradedReasonRecoveryDamagedTail
	case health.Recovery.Succeeded && health.Recovery.SkippedSnapshots > 0:
		return runtimeDegradedReasonRecoverySkipped
	case health.Ready && health.Protocol.Enabled && !health.Protocol.Ready:
		return runtimeDegradedReasonProtocolNotReady
	default:
		return runtimeDegradedReasonNone
	}
}
