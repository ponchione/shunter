package shunter_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/types"
)

func TestRuntimeGauntletRejectedReducerRestartNoGhostRows(t *testing.T) {
	dataDir := t.TempDir()
	rt := buildGauntletRuntime(t, dataDir)

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)

	baselineObserver := dialGauntletProtocol(t, rt)
	baselineInitial := subscribeGauntletProtocolPlayers(t, baselineObserver, "SELECT * FROM players", 9911, 9912)
	if diff := diffGauntletPlayers(baselineInitial, model.players); diff != "" {
		t.Fatalf("restart ghost-row baseline initial snapshot mismatch:\n%s", diff)
	}

	baseline := insertPlayerOp(&nextID, "restart_baseline")
	baselineDelta := gauntletAllRowsDeltaForOp(t, model, baseline)
	runGauntletTrace(t, rt, &model, []gauntletOp{baseline}, 0, "restart ghost-row baseline")
	gotBaselineDelta := readGauntletTransactionUpdateLight(t, baselineObserver, 9912, "restart ghost-row baseline")
	assertGauntletDeltaEqual(t, gotBaselineDelta, baselineDelta, "restart ghost-row baseline")
	if err := baselineObserver.Close(websocket.StatusNormalClosure, "restart ghost-row baseline complete"); err != nil {
		t.Fatalf("restart ghost-row close baseline observer: %v", err)
	}

	failedObserver := dialGauntletProtocol(t, rt)
	failedInitial := subscribeGauntletProtocolPlayers(t, failedObserver, "SELECT * FROM players", 9921, 9922)
	if diff := diffGauntletPlayers(failedInitial, model.players); diff != "" {
		t.Fatalf("restart ghost-row failed observer initial snapshot mismatch:\n%s", diff)
	}
	failed := failAfterInsertOp(nextID, "runtime_failed_before_restart")
	failedOutcome := callGauntletRuntimeReducer(t, rt, failed, "restart ghost-row runtime failed reducer")
	advanceGauntletModel(t, &model, failed, failedOutcome, "restart ghost-row runtime failed reducer")
	assertGauntletReadMatchesModel(t, rt, model, "restart ghost-row after runtime failed reducer")
	assertNoGauntletProtocolMessageBeforeClose(t, failedObserver, 50*time.Millisecond, "restart ghost-row runtime failed reducer")
	if err := failedObserver.Close(websocket.StatusNormalClosure, "restart ghost-row failed observer complete"); err != nil {
		t.Fatalf("restart ghost-row close failed observer: %v", err)
	}

	panicObserver := dialGauntletProtocol(t, rt)
	panicInitial := subscribeGauntletProtocolPlayers(t, panicObserver, "SELECT * FROM players", 9931, 9932)
	if diff := diffGauntletPlayers(panicInitial, model.players); diff != "" {
		t.Fatalf("restart ghost-row panic observer initial snapshot mismatch:\n%s", diff)
	}
	caller := dialGauntletProtocol(t, rt)
	protocolPanic := panicAfterInsertOp(nextID, "protocol_panic_before_restart")
	protocolOutcome := callGauntletProtocolReducer(t, caller, protocolPanic, 9933, "restart ghost-row protocol panic reducer")
	if protocolOutcome.status != shunter.StatusFailedUser {
		t.Fatalf("restart ghost-row protocol panic status = %v, want collapsed protocol failure %v", protocolOutcome.status, shunter.StatusFailedUser)
	}
	if protocolOutcome.err == "" {
		t.Fatal("restart ghost-row protocol panic error = empty")
	}
	assertGauntletReadMatchesModel(t, rt, model, "restart ghost-row after protocol panic reducer")
	assertNoGauntletProtocolMessageBeforeClose(t, panicObserver, 50*time.Millisecond, "restart ghost-row protocol panic reducer")
	if err := panicObserver.Close(websocket.StatusNormalClosure, "restart ghost-row panic observer complete"); err != nil {
		t.Fatalf("restart ghost-row close panic observer: %v", err)
	}
	if err := caller.Close(websocket.StatusNormalClosure, "restart ghost-row caller complete"); err != nil {
		t.Fatalf("restart ghost-row close caller: %v", err)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("restart ghost-row Close before restart returned error: %v", err)
	}

	rt = buildGauntletRuntime(t, dataDir)
	defer rt.Close()
	assertGauntletReadMatchesModel(t, rt, model, "restart ghost-row after restart")

	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "restart ghost-row after restart")

	subscriber := dialGauntletProtocol(t, rt)
	defer subscriber.Close(websocket.StatusNormalClosure, "")
	restartedInitial := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 9941, 9942)
	if diff := diffGauntletPlayers(restartedInitial, model.players); diff != "" {
		t.Fatalf("restart ghost-row restarted initial snapshot mismatch:\n%s", diff)
	}

	afterRestart := insertPlayerOp(&nextID, "after_restart_reuses_rejected_id")
	wantDelta := gauntletAllRowsDeltaForOp(t, model, afterRestart)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterRestart}, 1, "restart ghost-row after restart")
	gotDelta := readGauntletTransactionUpdateLight(t, subscriber, 9942, "restart ghost-row after restart")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, "restart ghost-row after restart")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "restart ghost-row final")
}

func TestRuntimeGauntletRejectedProtocolControlPlaneRestartRecovery(t *testing.T) {
	dataDir := t.TempDir()
	rt := buildGauntletRuntime(t, dataDir)

	rejectedClient := dialGauntletProtocol(t, rt)

	queryResp := queryGauntletProtocolExpectErrorWithLabel(t, rejectedClient, "SELECT * FROM players WHERE missing = 1", []byte("bad-query-before-restart"), "rejected control-plane one-off before restart")
	if queryResp.Error == nil || *queryResp.Error == "" {
		t.Fatal("rejected control-plane one-off error = empty")
	}

	singleErr := subscribeGauntletProtocolExpectErrorWithLabel(t, rejectedClient, "SELECT * FROM players WHERE missing = 1", 9951, 9952, "rejected control-plane single subscribe before restart")
	if singleErr.Error == "" {
		t.Fatal("rejected control-plane single subscribe error = empty")
	}

	multiErr := subscribeMultiGauntletProtocolExpectErrorWithLabel(t, rejectedClient, []string{
		"SELECT * FROM players",
		"SELECT * FROM missing",
	}, 9953, 9954, "rejected control-plane multi subscribe before restart")
	if multiErr.Error == "" {
		t.Fatal("rejected control-plane multi subscribe error = empty")
	}

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	beforeRestart := insertPlayerOp(&nextID, "after_rejected_control_plane")
	runGauntletTrace(t, rt, &model, []gauntletOp{beforeRestart}, 0, "rejected control-plane before restart")
	assertNoGauntletProtocolMessageBeforeClose(t, rejectedClient, 50*time.Millisecond, "rejected control-plane before restart fanout")

	if err := rt.Close(); err != nil {
		t.Fatalf("rejected control-plane Close before restart returned error: %v", err)
	}

	rt = buildGauntletRuntime(t, dataDir)
	defer rt.Close()
	assertGauntletReadMatchesModel(t, rt, model, "rejected control-plane after restart")

	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "rejected control-plane after restart")

	subscriber := dialGauntletProtocol(t, rt)
	defer subscriber.Close(websocket.StatusNormalClosure, "")
	initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 9955, 9956)
	if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
		t.Fatalf("rejected control-plane restarted initial snapshot mismatch:\n%s", diff)
	}

	afterRestart := insertPlayerOp(&nextID, "after_rejected_control_plane_restart")
	wantDelta := gauntletAllRowsDeltaForOp(t, model, afterRestart)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterRestart}, 1, "rejected control-plane after restart")
	gotDelta := readGauntletTransactionUpdateLight(t, subscriber, 9956, "rejected control-plane after restart")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, "rejected control-plane after restart")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "rejected control-plane final")
}

func TestRuntimeGauntletRejectedUnsubscribeRestartRecovery(t *testing.T) {
	dataDir := t.TempDir()
	rt := buildGauntletRuntime(t, dataDir)

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	subscriber := dialGauntletProtocol(t, rt)
	const liveQueryID = uint32(9972)
	initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 9971, liveQueryID)
	if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
		t.Fatalf("rejected unsubscribe op 0 initial subscriber snapshot mismatch:\n%s", diff)
	}

	singleErr := unsubscribeGauntletProtocolExpectErrorWithLabel(t, subscriber, 9973, 9974, "rejected unsubscribe op 1 unknown single unsubscribe")
	if singleErr.Error == "" {
		t.Fatal("rejected unsubscribe op 1 single unsubscribe error = empty")
	}
	multiErr := unsubscribeMultiGauntletProtocolExpectErrorWithLabel(t, subscriber, 9975, 9976, "rejected unsubscribe op 2 unknown multi unsubscribe")
	if multiErr.Error == "" {
		t.Fatal("rejected unsubscribe op 2 multi unsubscribe error = empty")
	}

	beforeRestart := insertPlayerOp(&nextID, "after_rejected_unsubscribe")
	beforeDelta := gauntletAllRowsDeltaForOp(t, model, beforeRestart)
	runGauntletTrace(t, rt, &model, []gauntletOp{beforeRestart}, 0, "rejected unsubscribe op 3 before restart")
	gotBeforeDelta := readGauntletTransactionUpdateLight(t, subscriber, liveQueryID, "rejected unsubscribe op 3 before restart")
	assertGauntletDeltaEqual(t, gotBeforeDelta, beforeDelta, "rejected unsubscribe op 3 before restart")
	if err := subscriber.Close(websocket.StatusNormalClosure, "rejected unsubscribe op 4 subscriber complete"); err != nil {
		t.Fatalf("rejected unsubscribe op 4 close subscriber: %v", err)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("rejected unsubscribe op 5 Close before restart returned error: %v", err)
	}

	rt = buildGauntletRuntime(t, dataDir)
	defer rt.Close()
	assertGauntletReadMatchesModel(t, rt, model, "rejected unsubscribe op 6 after restart")

	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "rejected unsubscribe op 6 after restart")

	restartedSubscriber := dialGauntletProtocol(t, rt)
	defer restartedSubscriber.Close(websocket.StatusNormalClosure, "")
	const restartedQueryID = uint32(9978)
	restartedInitial := subscribeGauntletProtocolPlayers(t, restartedSubscriber, "SELECT * FROM players", 9977, restartedQueryID)
	if diff := diffGauntletPlayers(restartedInitial, model.players); diff != "" {
		t.Fatalf("rejected unsubscribe op 7 restarted subscriber snapshot mismatch:\n%s", diff)
	}

	afterRestart := insertPlayerOp(&nextID, "after_rejected_unsubscribe_restart")
	afterDelta := gauntletAllRowsDeltaForOp(t, model, afterRestart)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterRestart}, 1, "rejected unsubscribe op 8 after restart")
	gotAfterDelta := readGauntletTransactionUpdateLight(t, restartedSubscriber, restartedQueryID, "rejected unsubscribe op 8 after restart")
	assertGauntletDeltaEqual(t, gotAfterDelta, afterDelta, "rejected unsubscribe op 8 after restart")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "rejected unsubscribe op 8 final")
}

func TestRuntimeGauntletDevTokenReconnectAfterCleanRestart(t *testing.T) {
	dataDir := t.TempDir()
	cfg := shunter.Config{
		DataDir:        dataDir,
		AuthMode:       shunter.AuthModeDev,
		AuthSigningKey: []byte("gauntlet-dev-token-restart-key"),
	}
	rt := buildGauntletRuntimeWithConfig(t, cfg, true)

	srv := httptest.NewServer(rt.HTTPHandler())
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
	wantConnID := types.ConnectionID{0xDE, 0xAD, 0xBE, 0xEF}
	reconnectURL := gauntletURLWithConnectionID(url, wantConnID)

	first, firstIdentity := dialGauntletProtocolURLWithHeaders(t, reconnectURL, nil, "dev-token restart op 0 first dial")
	if firstIdentity.ConnectionID != wantConnID {
		t.Fatalf("dev-token restart op 0 connection ID = %x, want %x", firstIdentity.ConnectionID, wantConnID)
	}
	if firstIdentity.Identity == (types.Identity{}) || firstIdentity.Token == "" {
		t.Fatalf("dev-token restart op 0 identity=%x token length=%d, want anonymous identity and minted token", firstIdentity.Identity, len(firstIdentity.Token))
	}

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	const firstQueryID = uint32(9962)
	initialRows := subscribeGauntletProtocolPlayers(t, first, "SELECT * FROM players", 9961, firstQueryID)
	if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
		t.Fatalf("dev-token restart op 0 initial subscriber snapshot mismatch:\n%s", diff)
	}
	if err := first.Close(websocket.StatusNormalClosure, "dev-token restart op 0 complete"); err != nil {
		t.Fatalf("dev-token restart op 0 close first client: %v", err)
	}

	caller, callerIdentity := dialGauntletProtocolURLWithHeaders(t, url, gauntletBearerHeader(firstIdentity.Token), "dev-token restart op 1 bearer caller")
	if callerIdentity.Identity != firstIdentity.Identity {
		t.Fatalf("dev-token restart op 1 caller identity = %x, want %x", callerIdentity.Identity, firstIdentity.Identity)
	}
	if callerIdentity.Token != "" {
		t.Fatalf("dev-token restart op 1 caller token = %q, want empty for bearer reconnect", callerIdentity.Token)
	}
	beforeRestart := insertPlayerOp(&nextID, "dev_token_before_restart")
	beforeOutcome := callGauntletProtocolReducer(t, caller, beforeRestart, 9963, "dev-token restart op 2 before restart commit")
	advanceGauntletModel(t, &model, beforeRestart, beforeOutcome, "dev-token restart op 2 before restart commit")
	assertGauntletReadMatchesModel(t, rt, model, "dev-token restart op 2 before restart commit")
	if err := caller.Close(websocket.StatusNormalClosure, "dev-token restart op 2 caller complete"); err != nil {
		t.Fatalf("dev-token restart op 2 close caller: %v", err)
	}

	srv.Close()
	if err := rt.Close(); err != nil {
		t.Fatalf("dev-token restart op 3 Close before restart returned error: %v", err)
	}

	rt = buildGauntletRuntimeWithConfig(t, cfg, true)
	defer rt.Close()
	assertGauntletReadMatchesModel(t, rt, model, "dev-token restart op 4 after restart")
	restartedSrv := httptest.NewServer(rt.HTTPHandler())
	defer restartedSrv.Close()
	restartedURL := strings.Replace(restartedSrv.URL, "http://", "ws://", 1) + "/subscribe"
	restartedReconnectURL := gauntletURLWithConnectionID(restartedURL, wantConnID)

	idle, idleIdentity := dialGauntletProtocolURLWithHeaders(t, restartedReconnectURL, gauntletBearerHeader(firstIdentity.Token), "dev-token restart op 5 idle reconnect")
	if idleIdentity.ConnectionID != wantConnID || idleIdentity.Identity != firstIdentity.Identity {
		t.Fatalf("dev-token restart op 5 identity token = {identity=%x conn=%x}, want {identity=%x conn=%x}", idleIdentity.Identity, idleIdentity.ConnectionID, firstIdentity.Identity, wantConnID)
	}
	if idleIdentity.Token != "" {
		t.Fatalf("dev-token restart op 5 idle token = %q, want empty for bearer reconnect", idleIdentity.Token)
	}

	restartedCaller, restartedCallerIdentity := dialGauntletProtocolURLWithHeaders(t, restartedURL, gauntletBearerHeader(firstIdentity.Token), "dev-token restart op 6 restarted caller")
	defer restartedCaller.CloseNow()
	if restartedCallerIdentity.Identity != firstIdentity.Identity || restartedCallerIdentity.Token != "" {
		t.Fatalf("dev-token restart op 6 caller identity token = {identity=%x token=%q}, want identity %x and empty token", restartedCallerIdentity.Identity, restartedCallerIdentity.Token, firstIdentity.Identity)
	}

	afterIdleReconnect := insertPlayerOp(&nextID, "dev_token_after_idle_reconnect")
	afterIdleOutcome := callGauntletProtocolReducer(t, restartedCaller, afterIdleReconnect, 9964, "dev-token restart op 7 after idle reconnect commit")
	advanceGauntletModel(t, &model, afterIdleReconnect, afterIdleOutcome, "dev-token restart op 7 after idle reconnect commit")
	assertNoGauntletProtocolMessageBeforeClose(t, idle, 50*time.Millisecond, "dev-token restart op 7 idle reconnect has no recovered subscription")
	if err := idle.Close(websocket.StatusNormalClosure, "dev-token restart op 7 idle complete"); err != nil {
		t.Fatalf("dev-token restart op 7 close idle reconnect: %v", err)
	}
	assertGauntletReadMatchesModel(t, rt, model, "dev-token restart op 7 after idle reconnect commit")

	subscriber, subscriberIdentity := dialGauntletProtocolURLWithHeaders(t, restartedReconnectURL, gauntletBearerHeader(firstIdentity.Token), "dev-token restart op 8 subscriber reconnect")
	defer subscriber.CloseNow()
	if subscriberIdentity.ConnectionID != wantConnID || subscriberIdentity.Identity != firstIdentity.Identity || subscriberIdentity.Token != "" {
		t.Fatalf("dev-token restart op 8 subscriber identity token = {identity=%x conn=%x token=%q}, want identity %x conn %x empty token", subscriberIdentity.Identity, subscriberIdentity.ConnectionID, subscriberIdentity.Token, firstIdentity.Identity, wantConnID)
	}
	const subscriberQueryID = uint32(9966)
	restartedInitial := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 9965, subscriberQueryID)
	if diff := diffGauntletPlayers(restartedInitial, model.players); diff != "" {
		t.Fatalf("dev-token restart op 8 restarted subscriber snapshot mismatch:\n%s", diff)
	}

	afterSubscribe := insertPlayerOp(&nextID, "dev_token_after_resubscribe")
	afterSubscribeDelta := gauntletAllRowsDeltaForOp(t, model, afterSubscribe)
	afterSubscribeOutcome := callGauntletProtocolReducer(t, restartedCaller, afterSubscribe, 9967, "dev-token restart op 9 after resubscribe commit")
	advanceGauntletModel(t, &model, afterSubscribe, afterSubscribeOutcome, "dev-token restart op 9 after resubscribe commit")
	gotAfterSubscribeDelta := readGauntletTransactionUpdateLight(t, subscriber, subscriberQueryID, "dev-token restart op 9 after resubscribe commit")
	assertGauntletDeltaEqual(t, gotAfterSubscribeDelta, afterSubscribeDelta, "dev-token restart op 9 after resubscribe commit")
	assertGauntletReadMatchesModel(t, rt, model, "dev-token restart op 9 after resubscribe commit")
}

func TestRuntimeGauntletDeterministicConcurrentReadShortSoak(t *testing.T) {
	const (
		steps       = 18
		readerCount = 3
	)

	for _, seed := range []int64{20260430, 20260501} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			trace := buildGauntletTrace(seed, steps)
			model := gauntletModel{players: map[uint64]string{}}
			startReaders := make(chan struct{})
			readerErrs := make(chan error, readerCount)
			var readers sync.WaitGroup

			for readerID := 0; readerID < readerCount; readerID++ {
				readerID := readerID
				readers.Add(1)
				go func() {
					defer readers.Done()
					<-startReaders
					for iter := 0; iter < steps*2; iter++ {
						err := rt.Read(context.Background(), func(view shunter.LocalReadView) error {
							_, err := collectGauntletPlayersFromReadView(view)
							return err
						})
						if err != nil {
							readerErrs <- fmt.Errorf("seed %d reader %d iter %02d: %w", seed, readerID, iter, err)
							return
						}
						time.Sleep(time.Duration((readerID+iter)%3) * time.Millisecond)
					}
				}()
			}
			close(startReaders)

			queryClient := dialGauntletProtocol(t, rt)
			defer queryClient.Close(websocket.StatusNormalClosure, "")

			for step, op := range trace {
				select {
				case err := <-readerErrs:
					t.Fatal(err)
				default:
				}

				label := fmt.Sprintf("seed %d concurrent soak", seed)
				runGauntletTrace(t, rt, &model, []gauntletOp{op}, step, label)
				if step%3 == 2 {
					assertGauntletProtocolQueriesMatchModel(t, queryClient, model, fmt.Sprintf("%s step %02d protocol probe", label, step))
				}
			}

			readers.Wait()
			close(readerErrs)
			for err := range readerErrs {
				t.Fatal(err)
			}
			assertGauntletReadMatchesModel(t, rt, model, fmt.Sprintf("seed %d concurrent soak final", seed))
			assertGauntletProtocolQueriesMatchModel(t, queryClient, model, fmt.Sprintf("seed %d concurrent soak final", seed))
		})
	}
}

func TestRuntimeGauntletProtocolRestartLoopShortSoak(t *testing.T) {
	const (
		steps        = 18
		restartEvery = 3
	)

	for _, seed := range []int64{20260502, 20260503} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			dataDir := t.TempDir()
			rt := buildGauntletRuntime(t, dataDir)
			t.Cleanup(func() {
				if rt != nil {
					_ = rt.Close()
				}
			})

			trace := buildGauntletTrace(seed, steps)
			model := gauntletModel{players: map[uint64]string{}}

			for chunkStart := 0; chunkStart < steps; chunkStart += restartEvery {
				chunkEnd := chunkStart + restartEvery
				if chunkEnd > steps {
					chunkEnd = steps
				}
				label := fmt.Sprintf("seed %d restart-loop chunk %02d-%02d", seed, chunkStart, chunkEnd)

				caller := dialGauntletProtocol(t, rt)
				runGauntletProtocolTrace(t, rt, caller, &model, trace[chunkStart:chunkEnd], chunkStart, uint32(10000+chunkStart*10), label)
				if err := caller.Close(websocket.StatusNormalClosure, label+" caller complete"); err != nil {
					t.Fatalf("%s close caller: %v", label, err)
				}

				queryClient := dialGauntletProtocol(t, rt)
				assertGauntletProtocolQueriesMatchModel(t, queryClient, model, label+" pre-restart probe")
				if err := queryClient.Close(websocket.StatusNormalClosure, label+" query complete"); err != nil {
					t.Fatalf("%s close query client: %v", label, err)
				}
				assertGauntletReadMatchesModel(t, rt, model, label+" pre-restart local read")

				if chunkEnd == steps {
					continue
				}
				if err := rt.Close(); err != nil {
					t.Fatalf("%s Close before restart returned error: %v", label, err)
				}
				rt = buildGauntletRuntime(t, dataDir)
				afterRestartLabel := fmt.Sprintf("seed %d restart-loop after restart at %02d", seed, chunkEnd)
				assertGauntletReadMatchesModel(t, rt, model, afterRestartLabel)
				assertGauntletSubscribeInitialMatchesModel(t, rt, model, afterRestartLabel)
			}

			finalLabel := fmt.Sprintf("seed %d restart-loop final", seed)
			assertGauntletReadMatchesModel(t, rt, model, finalLabel)
			finalQueryClient := dialGauntletProtocol(t, rt)
			defer finalQueryClient.Close(websocket.StatusNormalClosure, finalLabel)
			assertGauntletProtocolQueriesMatchModel(t, finalQueryClient, model, finalLabel)
		})
	}
}
