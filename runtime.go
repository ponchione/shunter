package shunter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// Runtime owns a built module, its recovered committed state, lifecycle-owned
// workers, and the protocol serving graph exposed by the hosted runtime.
type Runtime struct {
	module        moduleSnapshot
	config        Config
	buildConfig   Config
	engine        *schema.Engine
	registry      schema.SchemaRegistry
	readCatalog   *declaredReadCatalog
	dataDir       string
	state         *store.CommittedState
	recoveredTxID types.TxID
	resumePlan    commitlog.RecoveryResumePlan
	recovery      runtimeRecoveryFacts
	reducers      *executor.ReducerRegistry
	observability *runtimeObservability

	mu                          sync.Mutex
	closeMu                     sync.Mutex
	stateName                   RuntimeState
	ready                       atomic.Bool
	lastErr                     error
	startRetryBlockedErr        error
	durableTxID                 types.TxID
	executorFatal               bool
	executorFatalErr            error
	durabilityFatalErr          error
	protocolLastErr             error
	fanoutFatalErr              error
	protocolAcceptedConnections uint64
	protocolRejectedConnections uint64
	subscriptionDroppedClients  uint64
	lifecycleCancel             context.CancelFunc
	fanOutCancel                context.CancelFunc
	schedulerWG                 sync.WaitGroup
	fanOutWG                    sync.WaitGroup
	durability                  *commitlog.DurabilityWorker
	subscriptions               *subscription.Manager
	fanOutInbox                 chan subscription.FanOutMessage
	fanOutWorker                *subscription.FanOutWorker
	fanOutSender                subscription.FanOutSender
	executor                    *executor.Executor
	scheduler                   *executor.Scheduler

	protocolConns  *protocol.ConnManager
	protocolInbox  *executor.ProtocolInboxAdapter
	protocolSender protocol.ClientSender
	protocolServer *protocol.Server
	serving        bool
}

// Build validates the root hosted-runtime boundary and builds the module's
// schema, durable-state foundation, and reducer registry without starting
// runtime services.
func Build(mod *Module, cfg Config) (*Runtime, error) {
	observability, _ := newBuildObservability("unknown", cfg.Observability)
	fail := func(err error) (*Runtime, error) {
		observability.recordBuildFailed(err)
		return nil, err
	}

	if mod == nil {
		return fail(fmt.Errorf("module must not be nil"))
	}
	if strings.TrimSpace(mod.name) == "" {
		return fail(fmt.Errorf("module name must not be empty"))
	}
	observability.setModuleName(mod.name)

	preview, err := previewRuntimeBuild(mod, cfg)
	if err != nil {
		return fail(err)
	}
	observability = newRuntimeObservability(mod.name, preview.normalized.Observability)

	engine, err := mod.builder.Build(preview.schemaOpts)
	if err != nil {
		return fail(fmt.Errorf("build hosted runtime schema: %w", err))
	}
	registry := engine.Registry()
	readCatalog, err := newDeclaredReadCatalog(mod.queries, mod.views, registry)
	if err != nil {
		return fail(fmt.Errorf("build hosted runtime declared reads: %w", err))
	}
	if err := validateDataDirMetadata(preview.dataDir, mod, registry); err != nil {
		return fail(fmt.Errorf("build hosted runtime state: %w", err))
	}

	recoveryStart := time.Now()
	state, recoveredTxID, resumePlan, recoveryReport, err := openOrBootstrapState(preview.dataDir, registry)
	if err != nil {
		err = fmt.Errorf("build hosted runtime state: %w", err)
		observability.recordRecoveryFailed(err, time.Since(recoveryStart))
		return fail(err)
	}
	observability.recordRecoveryCompleted(recoveryReport, time.Since(recoveryStart))
	state.SetObserver(observability)
	state.RecordMemoryUsage()

	reducers, err := buildExecutorReducerRegistry(registry, mod.reducers)
	if err != nil {
		return fail(fmt.Errorf("build hosted runtime reducers: %w", err))
	}
	if err := writeDataDirMetadata(preview.dataDir, mod, registry); err != nil {
		return fail(fmt.Errorf("write data dir metadata: %w", err))
	}

	rt := &Runtime{
		module:        newModuleSnapshot(mod, preview.visibilityFilters),
		config:        copyConfig(cfg),
		buildConfig:   preview.normalized,
		engine:        engine,
		registry:      registry,
		readCatalog:   readCatalog,
		dataDir:       preview.dataDir,
		state:         state,
		recoveredTxID: recoveredTxID,
		resumePlan:    resumePlan,
		recovery:      newSuccessfulRuntimeRecoveryFacts(recoveryReport),
		reducers:      reducers,
		observability: observability,
		stateName:     RuntimeStateBuilt,
		durableTxID:   recoveredTxID,
	}
	rt.recordRuntimeMetrics()
	return rt, nil
}

type runtimeRecoveryFacts struct {
	ran       bool
	succeeded bool
	report    commitlog.RecoveryReport
	lastErr   error
}

func newSuccessfulRuntimeRecoveryFacts(report commitlog.RecoveryReport) runtimeRecoveryFacts {
	return runtimeRecoveryFacts{
		ran:       true,
		succeeded: true,
		report:    copyRecoveryReport(report),
	}
}

func (f runtimeRecoveryFacts) degraded() bool {
	return f.succeeded && (len(f.report.DamagedTailSegments) > 0 || len(f.report.SkippedSnapshots) > 0)
}

func copyRecoveryReport(report commitlog.RecoveryReport) commitlog.RecoveryReport {
	out := report
	out.SkippedSnapshots = append([]commitlog.SkippedSnapshotReport(nil), report.SkippedSnapshots...)
	out.DamagedTailSegments = append([]commitlog.SegmentInfo(nil), report.DamagedTailSegments...)
	out.SegmentCoverage = append([]commitlog.SegmentRange(nil), report.SegmentCoverage...)
	return out
}

// ModuleName returns the name of the module used to build the runtime.
func (r *Runtime) ModuleName() string {
	return r.module.name
}

// Config returns the runtime configuration by value.
func (r *Runtime) Config() Config {
	return copyConfig(r.config)
}
