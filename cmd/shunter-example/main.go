// Command shunter-example is a minimal embedding of the Shunter engine.
//
// It demonstrates the end-to-end wiring surface covered by OI-008: schema →
// committed state → commit-log durability → executor → protocol server, with
// graceful shutdown on SIGINT/SIGTERM. See docs/embedding.md for the
// embedder-facing walkthrough.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// fanOutInboxCapacity bounds the buffered hand-off between the executor's
// EvalAndBroadcast call and the fan-out worker goroutine. Sized to match
// the executor inbox default so a commit backlog does not rendezvous-stall
// the executor.
const fanOutInboxCapacity = 256

func main() {
	var (
		addr    = flag.String("addr", ":8080", "HTTP listen address")
		dataDir = flag.String("data", "./shunter-data", "commit-log / snapshot directory")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *dataDir); err != nil {
		log.Fatalf("shunter-example: %v", err)
	}
}

// run wires the engine graph and serves HTTP until ctx is cancelled. It is
// separated from main so the smoke test can drive it.
func run(ctx context.Context, addr, dataDir string) error {
	engine, err := buildEngine(ctx, dataDir)
	if err != nil {
		return err
	}
	defer engine.shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", engine.server.HandleSubscribe)
	httpSrv := &http.Server{Addr: addr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("shunter-example: listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http serve: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	return nil
}

// engineGraph bundles the wired subsystems for both main and test callers.
type engineGraph struct {
	server   *protocol.Server
	exec     *executor.Executor
	dw       *commitlog.DurabilityWorker
	subs     *subscription.Manager
	shutdown func()
}

// buildEngine wires schema → committed state → durability → executor →
// protocol server. The returned shutdown closes them in reverse order.
func buildEngine(ctx context.Context, dataDir string) (*engineGraph, error) {
	reg, err := buildSchema()
	if err != nil {
		return nil, fmt.Errorf("build schema: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data: %w", err)
	}

	committed, maxTxID, plan, err := openOrBootstrap(dataDir, reg)
	if err != nil {
		return nil, fmt.Errorf("open data dir: %w", err)
	}

	dw, err := commitlog.NewDurabilityWorkerWithResumePlan(dataDir, plan, commitlog.DefaultCommitLogOptions())
	if err != nil {
		return nil, fmt.Errorf("start durability: %w", err)
	}

	rr, err := buildReducerRegistry()
	if err != nil {
		dw.Close()
		return nil, fmt.Errorf("build reducers: %w", err)
	}

	// Subscription fan-out graph. The Manager consumes reducer commits
	// synchronously on the executor goroutine (EvalAndBroadcast) and hands
	// computed per-connection deltas to the FanOutWorker over fanOutInbox.
	// The worker drives protocol.FanOutSenderAdapter, which encodes each
	// delta and enqueues it on the target connection's OutboundCh.
	fanOutInbox := make(chan subscription.FanOutMessage, fanOutInboxCapacity)
	subs := subscription.NewManager(
		schemaLookupAdapter{reg},
		reg,
		subscription.WithFanOutInbox(fanOutInbox),
	)

	exec := executor.NewExecutor(executor.ExecutorConfig{
		Durability:    durabilityAdapter{dw},
		Subscriptions: subs,
	}, rr, committed, reg, uint64(maxTxID))

	// Scheduler replays sys_scheduled past-due rows into the executor inbox
	// during Startup. schedCtx is cancelled by shutdown() so sched.Run exits
	// before exec.Shutdown closes the inbox — otherwise a late scheduled
	// reducer send could race a closed channel.
	sched := exec.SchedulerFor()
	if err := exec.Startup(ctx, sched); err != nil {
		dw.Close()
		return nil, fmt.Errorf("executor startup: %w", err)
	}

	schedCtx, schedCancel := context.WithCancel(ctx)
	schedDone := make(chan struct{})
	go func() {
		sched.Run(schedCtx)
		close(schedDone)
	}()

	runDone := make(chan struct{})
	go func() {
		exec.Run(ctx)
		close(runDone)
	}()

	// ConnManager is shared between the protocol.Server (admission) and the
	// fan-out ClientSender (delivery) so resolved ConnectionIDs point at
	// the same *Conn in both directions.
	conns := protocol.NewConnManager()
	inboxAdapter := executor.NewProtocolInboxAdapter(exec)
	clientSender := protocol.NewClientSender(conns, inboxAdapter)
	fanOutSender := protocol.NewFanOutSenderAdapter(clientSender)
	worker := subscription.NewFanOutWorker(fanOutInbox, fanOutSender, subs.DroppedChanSend())
	workerDone := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(workerDone)
	}()

	server := buildProtocolServer(inboxAdapter, conns, reg, committed)

	return &engineGraph{
		server: server,
		exec:   exec,
		dw:     dw,
		subs:   subs,
		shutdown: func() {
			// Stop the scheduler first so no in-flight scheduled-reducer
			// enqueue races exec.Shutdown closing the inbox.
			schedCancel()
			<-schedDone
			exec.Shutdown()
			<-runDone
			// Closing the fan-out inbox drains the worker so it does not
			// sit on a cancelled ctx waiting for late sends after the
			// executor has already stopped publishing. Order matters:
			// worker exit first, then durability close, so any in-flight
			// delivery completes before the commit-log goes away.
			close(fanOutInbox)
			<-workerDone
			dw.Close()
		},
	}, nil
}

func buildSchema() (schema.SchemaRegistry, error) {
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "greetings",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "message", Type: types.KindString},
		},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		return nil, err
	}
	return eng.Registry(), nil
}

func buildReducerRegistry() (*executor.ReducerRegistry, error) {
	rr := executor.NewReducerRegistry()
	if err := rr.Register(executor.RegisteredReducer{
		Name:    "say_hello",
		Handler: sayHello,
	}); err != nil {
		return nil, err
	}
	rr.Freeze()
	return rr, nil
}

func sayHello(rctx *types.ReducerContext, args []byte) ([]byte, error) {
	// Minimal reducer: insert a row with the caller-supplied message. The
	// real embedder would BSATN-decode args into a typed struct.
	msg := string(args)
	if msg == "" {
		msg = "hello, world"
	}
	const greetingsTableID uint32 = 0
	if _, err := rctx.DB.Insert(greetingsTableID, types.ProductValue{
		types.NewUint64(0),
		types.NewString(msg),
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

func openOrBootstrap(dir string, reg schema.SchemaRegistry) (*store.CommittedState, types.TxID, commitlog.RecoveryResumePlan, error) {
	committed, maxTxID, plan, err := commitlog.OpenAndRecoverDetailed(dir, reg)
	if err == nil {
		return committed, maxTxID, plan, nil
	}
	if !errors.Is(err, commitlog.ErrNoData) {
		return nil, 0, commitlog.RecoveryResumePlan{}, err
	}

	// First boot: seed empty committed state and write an initial snapshot
	// so subsequent recovery finds a valid base.
	fresh := store.NewCommittedState()
	for _, tid := range reg.Tables() {
		ts, ok := reg.Table(tid)
		if !ok {
			return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("registry missing table %d", tid)
		}
		fresh.RegisterTable(tid, store.NewTable(ts))
	}
	if err := commitlog.NewSnapshotWriter(dir, reg).CreateSnapshot(fresh, 0); err != nil {
		return nil, 0, commitlog.RecoveryResumePlan{}, fmt.Errorf("initial snapshot: %w", err)
	}
	return commitlog.OpenAndRecoverDetailed(dir, reg)
}

func buildProtocolServer(inbox *executor.ProtocolInboxAdapter, conns *protocol.ConnManager, reg schema.SchemaRegistry, cs *store.CommittedState) *protocol.Server {
	signingKey := []byte("shunter-example-signing-key-change-me")
	return &protocol.Server{
		JWT: &auth.JWTConfig{
			SigningKey: signingKey,
			AuthMode:   auth.AuthModeAnonymous,
		},
		Mint: &auth.MintConfig{
			Issuer:     "shunter-example",
			Audience:   "shunter-example",
			SigningKey: signingKey,
			Expiry:     24 * time.Hour,
		},
		Options:  protocol.DefaultProtocolOptions(),
		Executor: inbox,
		Conns:    conns,
		Schema:   reg,
		State:    stateAdapter{cs},
	}
}

// stateAdapter lifts *store.CommittedState.Snapshot()'s concrete return type
// to the interface required by protocol.CommittedStateAccess.
type stateAdapter struct{ cs *store.CommittedState }

func (a stateAdapter) Snapshot() store.CommittedReadView { return a.cs.Snapshot() }

// schemaLookupAdapter widens schema.SchemaRegistry to satisfy
// subscription.SchemaLookup. The subscription layer needs ColumnCount
// (used by the join evaluator to project concatenated LHS++RHS rows onto
// one side) which schema.SchemaRegistry itself does not expose.
type schemaLookupAdapter struct{ reg schema.SchemaRegistry }

func (a schemaLookupAdapter) TableExists(t subscription.TableID) bool {
	return a.reg.TableExists(t)
}
func (a schemaLookupAdapter) ColumnExists(t subscription.TableID, c subscription.ColID) bool {
	return a.reg.ColumnExists(t, c)
}
func (a schemaLookupAdapter) ColumnType(t subscription.TableID, c subscription.ColID) subscription.ValueKind {
	return a.reg.ColumnType(t, c)
}
func (a schemaLookupAdapter) HasIndex(t subscription.TableID, c subscription.ColID) bool {
	return a.reg.HasIndex(t, c)
}
func (a schemaLookupAdapter) TableName(t subscription.TableID) string {
	return a.reg.TableName(t)
}
func (a schemaLookupAdapter) ColumnCount(t subscription.TableID) int {
	ts, ok := a.reg.Table(t)
	if !ok {
		return 0
	}
	return len(ts.Columns)
}

// durabilityAdapter bridges commitlog.DurabilityWorker to executor.DurabilityHandle.
// The method signatures differ only on the txID scalar type.
type durabilityAdapter struct{ dw *commitlog.DurabilityWorker }

func (a durabilityAdapter) EnqueueCommitted(txID types.TxID, cs *store.Changeset) {
	a.dw.EnqueueCommitted(uint64(txID), cs)
}

func (a durabilityAdapter) WaitUntilDurable(txID types.TxID) <-chan types.TxID {
	return a.dw.WaitUntilDurable(txID)
}
