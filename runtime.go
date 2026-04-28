package shunter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

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
	dataDir       string
	state         *store.CommittedState
	recoveredTxID types.TxID
	resumePlan    commitlog.RecoveryResumePlan
	reducers      *executor.ReducerRegistry

	mu              sync.Mutex
	closeMu         sync.Mutex
	stateName       RuntimeState
	ready           atomic.Bool
	lastErr         error
	lifecycleCancel context.CancelFunc
	fanOutCancel    context.CancelFunc
	schedulerWG     sync.WaitGroup
	fanOutWG        sync.WaitGroup
	durability      *commitlog.DurabilityWorker
	subscriptions   *subscription.Manager
	fanOutInbox     chan subscription.FanOutMessage
	fanOutWorker    *subscription.FanOutWorker
	fanOutSender    subscription.FanOutSender
	executor        *executor.Executor
	scheduler       *executor.Scheduler

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
	if mod == nil {
		return nil, fmt.Errorf("module must not be nil")
	}
	if strings.TrimSpace(mod.name) == "" {
		return nil, fmt.Errorf("module name must not be empty")
	}

	normalized, dataDir, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateModuleDeclarations(mod); err != nil {
		return nil, err
	}

	engine, err := mod.builder.Build(schema.EngineOptions{
		DataDir:                 dataDir,
		ExecutorQueueCapacity:   normalized.ExecutorQueueCapacity,
		DurabilityQueueCapacity: normalized.DurabilityQueueCapacity,
		EnableProtocol:          normalized.EnableProtocol,
	})
	if err != nil {
		return nil, fmt.Errorf("build hosted runtime schema: %w", err)
	}
	registry := engine.Registry()

	state, recoveredTxID, resumePlan, err := openOrBootstrapState(dataDir, registry)
	if err != nil {
		return nil, fmt.Errorf("build hosted runtime state: %w", err)
	}

	reducers, err := buildExecutorReducerRegistry(registry)
	if err != nil {
		return nil, fmt.Errorf("build hosted runtime reducers: %w", err)
	}

	return &Runtime{
		module:        newModuleSnapshot(mod),
		config:        cfg,
		buildConfig:   normalized,
		engine:        engine,
		registry:      registry,
		dataDir:       dataDir,
		state:         state,
		recoveredTxID: recoveredTxID,
		resumePlan:    resumePlan,
		reducers:      reducers,
		stateName:     RuntimeStateBuilt,
	}, nil
}

// ModuleName returns the name of the module used to build the runtime.
func (r *Runtime) ModuleName() string {
	return r.module.name
}

// Config returns the runtime configuration by value.
func (r *Runtime) Config() Config {
	return r.config
}
