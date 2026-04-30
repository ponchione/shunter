package shunter_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	shunter "github.com/ponchione/shunter"
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
