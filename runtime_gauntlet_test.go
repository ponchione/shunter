package shunter_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const gauntletPlayersTableID schema.TableID = 0

func TestRuntimeGauntletSeededReducerReadModel(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			trace := buildGauntletTrace(seed, 48)
			model := gauntletModel{players: map[uint64]string{}}

			assertGauntletReadMatchesModel(t, rt, model, "initial")
			runGauntletTrace(t, rt, &model, trace, 0, fmt.Sprintf("seed %d", seed))
		})
	}
}

func TestRuntimeGauntletRestartEquivalence(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			const steps = 64
			trace := buildGauntletTrace(seed, steps)

			uninterruptedRT := buildGauntletRuntime(t, t.TempDir())
			uninterruptedModel := gauntletModel{players: map[uint64]string{}}
			runGauntletTrace(t, uninterruptedRT, &uninterruptedModel, trace, 0, fmt.Sprintf("seed %d uninterrupted", seed))
			uninterruptedPlayers := readGauntletPlayers(t, uninterruptedRT, fmt.Sprintf("seed %d uninterrupted final", seed))
			if err := uninterruptedRT.Close(); err != nil {
				t.Fatalf("seed %d uninterrupted Close returned error: %v", seed, err)
			}

			for _, restartAt := range []int{0, 1, 2, steps / 2, steps - 1, steps} {
				t.Run(fmt.Sprintf("restart_at_%02d", restartAt), func(t *testing.T) {
					restartDataDir := t.TempDir()
					restartedRT := buildGauntletRuntime(t, restartDataDir)
					restartedModel := gauntletModel{players: map[uint64]string{}}
					runGauntletTrace(t, restartedRT, &restartedModel, trace[:restartAt], 0, fmt.Sprintf("seed %d before restart at %d", seed, restartAt))
					if err := restartedRT.Close(); err != nil {
						t.Fatalf("seed %d restart at %d Close returned error: %v", seed, restartAt, err)
					}

					restartedRT = buildGauntletRuntime(t, restartDataDir)
					defer restartedRT.Close()
					assertGauntletReadMatchesModel(t, restartedRT, restartedModel, fmt.Sprintf("seed %d after restart at %d", seed, restartAt))
					runGauntletTrace(t, restartedRT, &restartedModel, trace[restartAt:], restartAt, fmt.Sprintf("seed %d after restart at %d", seed, restartAt))

					restartedPlayers := readGauntletPlayers(t, restartedRT, fmt.Sprintf("seed %d restarted final after restart at %d", seed, restartAt))
					if diff := diffGauntletPlayers(restartedPlayers, uninterruptedPlayers); diff != "" {
						t.Fatalf("seed %d restart at %d restarted/uninterrupted mismatch:\n%s", seed, restartAt, diff)
					}
				})
			}
		})
	}
}

func TestRuntimeGauntletProtocolCallReducerRestartEquivalence(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			const steps = 36
			trace := buildGauntletTrace(seed, steps)

			uninterruptedRT := buildGauntletRuntime(t, t.TempDir())
			uninterruptedClient := dialGauntletProtocol(t, uninterruptedRT)
			uninterruptedModel := gauntletModel{players: map[uint64]string{}}
			runGauntletProtocolTrace(t, uninterruptedRT, uninterruptedClient, &uninterruptedModel, trace, 0, 8200, fmt.Sprintf("seed %d protocol uninterrupted", seed))
			uninterruptedPlayers := readGauntletPlayers(t, uninterruptedRT, fmt.Sprintf("seed %d protocol uninterrupted final", seed))
			if err := uninterruptedClient.Close(websocket.StatusNormalClosure, "uninterrupted complete"); err != nil {
				t.Fatalf("seed %d close uninterrupted protocol client: %v", seed, err)
			}
			if err := uninterruptedRT.Close(); err != nil {
				t.Fatalf("seed %d protocol uninterrupted Close returned error: %v", seed, err)
			}

			for _, restartAt := range []int{0, steps / 2, steps} {
				t.Run(fmt.Sprintf("restart_at_%02d", restartAt), func(t *testing.T) {
					restartDataDir := t.TempDir()
					restartedRT := buildGauntletRuntime(t, restartDataDir)
					restartedClient := dialGauntletProtocol(t, restartedRT)
					restartedModel := gauntletModel{players: map[uint64]string{}}
					runGauntletProtocolTrace(t, restartedRT, restartedClient, &restartedModel, trace[:restartAt], 0, uint32(8300+restartAt*100), fmt.Sprintf("seed %d protocol before restart at %d", seed, restartAt))
					if err := restartedClient.Close(websocket.StatusNormalClosure, "restart prefix complete"); err != nil {
						t.Fatalf("seed %d restart at %d close prefix protocol client: %v", seed, restartAt, err)
					}
					if err := restartedRT.Close(); err != nil {
						t.Fatalf("seed %d protocol restart at %d Close returned error: %v", seed, restartAt, err)
					}

					restartedRT = buildGauntletRuntime(t, restartDataDir)
					defer restartedRT.Close()
					restartedClient = dialGauntletProtocol(t, restartedRT)
					defer restartedClient.Close(websocket.StatusNormalClosure, "")

					assertGauntletReadMatchesModel(t, restartedRT, restartedModel, fmt.Sprintf("seed %d protocol after restart at %d", seed, restartAt))
					runGauntletProtocolTrace(t, restartedRT, restartedClient, &restartedModel, trace[restartAt:], restartAt, uint32(8600+restartAt*100), fmt.Sprintf("seed %d protocol after restart at %d", seed, restartAt))

					restartedPlayers := readGauntletPlayers(t, restartedRT, fmt.Sprintf("seed %d protocol restarted final after restart at %d", seed, restartAt))
					if diff := diffGauntletPlayers(restartedPlayers, uninterruptedPlayers); diff != "" {
						t.Fatalf("seed %d protocol restart at %d restarted/uninterrupted mismatch:\n%s", seed, restartAt, diff)
					}
				})
			}
		})
	}
}

func TestRuntimeGauntletProtocolCloseWithLiveClientsRestart(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			dataDir := t.TempDir()
			rt := buildGauntletRuntime(t, dataDir)

			trace := buildGauntletTrace(seed, 24)
			model := gauntletModel{players: map[uint64]string{}}

			caller := dialGauntletProtocol(t, rt)
			defer caller.CloseNow()
			runGauntletProtocolTrace(t, rt, caller, &model, trace[:12], 0, uint32(8700), fmt.Sprintf("seed %d before live-client close", seed))

			subscriber := dialGauntletProtocol(t, rt)
			defer subscriber.CloseNow()
			initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 8713, 8714)
			if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
				t.Fatalf("seed %d live-client subscriber initial snapshot mismatch:\n%s", seed, diff)
			}

			queryClient := dialGauntletProtocol(t, rt)
			defer queryClient.CloseNow()
			queryRows := queryGauntletProtocolPlayers(t, queryClient, "SELECT * FROM players", []byte("before-live-client-close"))
			if diff := diffGauntletPlayers(queryRows, model.players); diff != "" {
				t.Fatalf("seed %d live-client one-off snapshot mismatch:\n%s", seed, diff)
			}

			if err := rt.Close(); err != nil {
				t.Fatalf("seed %d Close with live protocol clients returned error: %v", seed, err)
			}
			assertGauntletProtocolClosed(t, caller, fmt.Sprintf("seed %d caller after runtime close", seed))
			assertGauntletProtocolClosed(t, subscriber, fmt.Sprintf("seed %d subscriber after runtime close", seed))
			assertGauntletProtocolClosed(t, queryClient, fmt.Sprintf("seed %d query client after runtime close", seed))

			restartedRT := buildGauntletRuntime(t, dataDir)
			defer restartedRT.Close()
			assertGauntletReadMatchesModel(t, restartedRT, model, fmt.Sprintf("seed %d after live-client restart", seed))

			restartedClient := dialGauntletProtocol(t, restartedRT)
			defer restartedClient.Close(websocket.StatusNormalClosure, "")
			assertGauntletProtocolQueriesMatchModel(t, restartedClient, model, fmt.Sprintf("seed %d after live-client restart", seed))
			assertGauntletSubscribeInitialMatchesModel(t, restartedRT, model, fmt.Sprintf("seed %d after live-client restart", seed))

			runGauntletProtocolTrace(t, restartedRT, restartedClient, &model, trace[12:], 12, uint32(8800), fmt.Sprintf("seed %d after live-client restart", seed))
		})
	}
}

func TestRuntimeGauntletMixedSurfaceTrace(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			reducerClient := dialGauntletProtocol(t, rt)
			defer reducerClient.Close(websocket.StatusNormalClosure, "")
			queryClient := dialGauntletProtocol(t, rt)
			defer queryClient.Close(websocket.StatusNormalClosure, "")
			subscriber := dialGauntletProtocol(t, rt)
			defer subscriber.CloseNow()

			model := gauntletModel{players: map[uint64]string{}}
			subscribeRequestID := uint32(8901)
			queryID := uint32(8902)
			initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", subscribeRequestID, queryID)
			if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
				t.Fatalf("seed %d mixed-surface initial subscribe snapshot mismatch:\n%s", seed, diff)
			}

			trace := buildGauntletTrace(seed, 24)
			for step, op := range trace {
				label := fmt.Sprintf("seed %d mixed-surface step %d %s", seed, step, op)
				wantDelta := gauntletAllRowsDeltaForOp(t, model, op)

				var outcome gauntletReducerOutcome
				if step%3 == 0 {
					outcome = callGauntletProtocolReducer(t, reducerClient, op, uint32(9000+step), label)
				} else {
					res, err := rt.CallReducer(context.Background(), op.reducer, []byte(op.args))
					if err != nil {
						t.Fatalf("%s admission error: %v", label, err)
					}
					outcome = gauntletReducerOutcomeFromResult(res)
				}
				advanceGauntletModel(t, &model, op, outcome, label)
				assertGauntletReadMatchesModel(t, rt, model, label)

				if op.wantStatus == shunter.StatusCommitted {
					gotDelta := readGauntletTransactionUpdateLight(t, subscriber, queryID, label)
					assertGauntletDeltaEqual(t, gotDelta, wantDelta, label)
				}

				if step%5 == 4 {
					assertGauntletProtocolQueriesMatchModel(t, queryClient, model, label)
				}
				if step == 11 {
					finalRows := unsubscribeGauntletProtocolPlayers(t, subscriber, subscribeRequestID+1, queryID)
					if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
						t.Fatalf("%s unsubscribe final rows mismatch:\n%s", label, diff)
					}
					if err := subscriber.Close(websocket.StatusNormalClosure, "mixed-surface resubscribe"); err != nil {
						t.Fatalf("%s close unsubscribed protocol client: %v", label, err)
					}

					subscriber = dialGauntletProtocol(t, rt)
					defer subscriber.CloseNow()
					subscribeRequestID = 8911
					queryID = 8912
					initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", subscribeRequestID, queryID)
					if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
						t.Fatalf("%s resubscribe initial snapshot mismatch:\n%s", label, diff)
					}
				}
			}

			assertGauntletProtocolQueriesMatchModel(t, queryClient, model, fmt.Sprintf("seed %d mixed-surface final", seed))
			finalRows := unsubscribeGauntletProtocolPlayers(t, subscriber, subscribeRequestID+1, queryID)
			if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
				t.Fatalf("seed %d mixed-surface final unsubscribe rows mismatch:\n%s", seed, diff)
			}
		})
	}
}

func TestRuntimeGauntletProtocolMultiClientMixedWorkload(t *testing.T) {
	const (
		allRequestID          = uint32(8961)
		allQueryID            = uint32(8962)
		targetRequestID       = uint32(8963)
		targetQueryID         = uint32(8964)
		targetUnsubscribeID   = uint32(8965)
		resubscribeRequestID  = uint32(8966)
		resubscribeQueryID    = uint32(8967)
		resubscribeUnsubID    = uint32(8968)
		allUnsubscribeID      = uint32(8969)
		protocolRequestIDBase = uint32(8970)
		targetName            = "target"
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	caller := dialGauntletProtocol(t, rt)
	defer caller.Close(websocket.StatusNormalClosure, "")
	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	allSubscriber := dialGauntletProtocol(t, rt)
	defer allSubscriber.Close(websocket.StatusNormalClosure, "")
	targetSubscriber := dialGauntletProtocol(t, rt)
	defer targetSubscriber.CloseNow()

	model := gauntletModel{players: map[uint64]string{}}
	allInitial := subscribeGauntletProtocolPlayers(t, allSubscriber, "SELECT * FROM players", allRequestID, allQueryID)
	if diff := diffGauntletPlayers(allInitial, model.players); diff != "" {
		t.Fatalf("multi-client all subscriber initial snapshot mismatch:\n%s", diff)
	}
	targetInitial := subscribeGauntletProtocolPlayers(t, targetSubscriber, "SELECT * FROM players WHERE name = 'target'", targetRequestID, targetQueryID)
	if diff := diffGauntletPlayers(targetInitial, map[uint64]string{}); diff != "" {
		t.Fatalf("multi-client target subscriber initial snapshot mismatch:\n%s", diff)
	}
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "multi-client mixed initial")

	targetMatches := func(_ uint64, name string) bool { return name == targetName }
	targetActive := true
	currentTargetQueryID := targetQueryID

	callAndAssert := func(step int, op gauntletOp, viaProtocol bool) {
		t.Helper()
		label := fmt.Sprintf("multi-client mixed step %d %s", step, op)
		wantAllDelta := gauntletAllRowsDeltaForOp(t, model, op)
		wantTargetDelta := gauntletDeltaForOpMatching(t, model, op, targetMatches)

		var outcome gauntletReducerOutcome
		if viaProtocol {
			outcome = callGauntletProtocolReducer(t, caller, op, protocolRequestIDBase+uint32(step), label)
		} else {
			res, err := rt.CallReducer(context.Background(), op.reducer, []byte(op.args))
			if err != nil {
				t.Fatalf("%s admission error: %v", label, err)
			}
			outcome = gauntletReducerOutcomeFromResult(res)
		}
		advanceGauntletModel(t, &model, op, outcome, label)
		assertGauntletReadMatchesModel(t, rt, model, label)

		if op.wantStatus == shunter.StatusCommitted {
			gotAllDelta := readGauntletTransactionUpdateLight(t, allSubscriber, allQueryID, label+" all subscriber")
			assertGauntletDeltaEqual(t, gotAllDelta, wantAllDelta, label+" all subscriber")
			if targetActive && !gauntletDeltaIsEmpty(wantTargetDelta) {
				gotTargetDelta := readGauntletTransactionUpdateLight(t, targetSubscriber, currentTargetQueryID, label+" target subscriber")
				assertGauntletDeltaEqual(t, gotTargetDelta, wantTargetDelta, label+" target subscriber")
			}
		}
		assertGauntletProtocolQueriesMatchModel(t, queryClient, model, label)
	}

	nextID := uint64(1)
	callAndAssert(0, insertPlayerOp(&nextID, targetName), true)
	callAndAssert(1, insertPlayerOp(&nextID, "other"), false)
	callAndAssert(2, renamePlayerOp(2, targetName), true)

	targetFinalRows := unsubscribeGauntletProtocolPlayers(t, targetSubscriber, targetUnsubscribeID, targetQueryID)
	if diff := diffGauntletPlayers(targetFinalRows, filterGauntletPlayersByName(model.players, targetName)); diff != "" {
		t.Fatalf("multi-client target unsubscribe final rows mismatch:\n%s", diff)
	}
	targetActive = false
	if err := targetSubscriber.Close(websocket.StatusNormalClosure, "multi-client target unsubscribed"); err != nil {
		t.Fatalf("close multi-client unsubscribed target subscriber: %v", err)
	}

	callAndAssert(3, renamePlayerOp(1, "other"), false)

	targetSubscriber = dialGauntletProtocol(t, rt)
	defer targetSubscriber.CloseNow()
	currentTargetQueryID = resubscribeQueryID
	targetActive = true
	resubscribeInitial := subscribeGauntletProtocolPlayers(t, targetSubscriber, "SELECT * FROM players WHERE name = 'target'", resubscribeRequestID, currentTargetQueryID)
	if diff := diffGauntletPlayers(resubscribeInitial, filterGauntletPlayersByName(model.players, targetName)); diff != "" {
		t.Fatalf("multi-client target resubscribe initial snapshot mismatch:\n%s", diff)
	}

	callAndAssert(4, deletePlayerOp(2), true)

	resubscribeFinalRows := unsubscribeGauntletProtocolPlayers(t, targetSubscriber, resubscribeUnsubID, currentTargetQueryID)
	if diff := diffGauntletPlayers(resubscribeFinalRows, filterGauntletPlayersByName(model.players, targetName)); diff != "" {
		t.Fatalf("multi-client target resubscribe unsubscribe final rows mismatch:\n%s", diff)
	}
	targetActive = false

	allFinalRows := unsubscribeGauntletProtocolPlayers(t, allSubscriber, allUnsubscribeID, allQueryID)
	if diff := diffGauntletPlayers(allFinalRows, model.players); diff != "" {
		t.Fatalf("multi-client all unsubscribe final rows mismatch:\n%s", diff)
	}
	assertGauntletReadMatchesModel(t, rt, model, "multi-client mixed final")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "multi-client mixed final")
}

func TestRuntimeGauntletProtocolOneOffQueryModel(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			client := dialGauntletProtocol(t, rt)
			defer client.Close(websocket.StatusNormalClosure, "")

			trace := buildGauntletTrace(seed, 32)
			model := gauntletModel{players: map[uint64]string{}}

			assertGauntletProtocolQueriesMatchModel(t, client, model, fmt.Sprintf("seed %d initial", seed))
			for step, op := range trace {
				runGauntletTrace(t, rt, &model, trace[step:step+1], step, fmt.Sprintf("seed %d protocol", seed))
				assertGauntletProtocolQueriesMatchModel(t, client, model, fmt.Sprintf("seed %d after step %d %s", seed, step, op))
			}
		})
	}
}

func TestRuntimeGauntletProtocolOneOffQueryErrorIsIsolated(t *testing.T) {
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	prefix := []gauntletOp{
		insertPlayerOp(&nextID, "before_bad_one_off"),
	}
	runGauntletTrace(t, rt, &model, prefix, 0, "one-off error prefix")

	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	subClient := dialGauntletProtocol(t, rt)
	defer subClient.Close(websocket.StatusNormalClosure, "")

	initial := subscribeGauntletProtocolPlayers(t, subClient, "SELECT * FROM players", 8101, 8102)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("one-off error subscriber initial snapshot mismatch:\n%s", diff)
	}

	resp := queryGauntletProtocolExpectError(t, queryClient, "SELECT * FROM players WHERE missing = 1", []byte("bad-one-off"))
	if *resp.Error == "" {
		t.Fatal("one-off query error = empty")
	}
	if len(resp.Tables) != 0 {
		t.Fatalf("one-off query error returned %d tables, want 0", len(resp.Tables))
	}
	assertGauntletReadMatchesModel(t, rt, model, "after bad one-off query")
	assertNoGauntletProtocolMessageBeforeClose(t, subClient, 50*time.Millisecond, "bad one-off subscriber fanout")

	got := queryGauntletProtocolPlayers(t, queryClient, "SELECT * FROM players", []byte("after-bad-one-off"))
	if diff := diffGauntletPlayers(got, model.players); diff != "" {
		t.Fatalf("valid one-off after bad query mismatch:\n%s", diff)
	}
}

func TestRuntimeGauntletProtocolOneOffSameConnectionWithSubscription(t *testing.T) {
	const (
		subscribeRequestID   = uint32(8121)
		unsubscribeRequestID = uint32(8122)
		queryID              = uint32(8123)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	initial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", subscribeRequestID, queryID)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("same-connection one-off initial subscribe snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	insertOne := insertPlayerOp(&nextID, "same_conn_one")
	wantInsertOne := gauntletAllRowsDeltaForOp(t, model, insertOne)
	runGauntletTrace(t, rt, &model, []gauntletOp{insertOne}, 0, "same-connection one-off prefix")
	gotInsertOne := readGauntletTransactionUpdateLight(t, client, queryID, "same-connection one-off prefix")
	assertGauntletDeltaEqual(t, gotInsertOne, wantInsertOne, "same-connection one-off prefix")

	gotRows := queryGauntletProtocolPlayers(t, client, "SELECT * FROM players", []byte("same-conn-one-off"))
	if diff := diffGauntletPlayers(gotRows, model.players); diff != "" {
		t.Fatalf("same-connection one-off query mismatch:\n%s", diff)
	}

	resp := queryGauntletProtocolExpectError(t, client, "SELECT * FROM players WHERE missing = 1", []byte("same-conn-bad-one-off"))
	if *resp.Error == "" {
		t.Fatal("same-connection one-off query error = empty")
	}
	assertGauntletReadMatchesModel(t, rt, model, "same-connection after bad one-off")

	failedInsert := failAfterInsertOp(nextID, "same_conn_failed")
	runGauntletTrace(t, rt, &model, []gauntletOp{failedInsert}, 1, "same-connection failed reducer after bad one-off")

	insertTwo := insertPlayerOp(&nextID, "same_conn_two")
	wantInsertTwo := gauntletAllRowsDeltaForOp(t, model, insertTwo)
	runGauntletTrace(t, rt, &model, []gauntletOp{insertTwo}, 2, "same-connection one-off suffix")
	gotInsertTwo := readGauntletTransactionUpdateLight(t, client, queryID, "same-connection one-off suffix")
	assertGauntletDeltaEqual(t, gotInsertTwo, wantInsertTwo, "same-connection one-off suffix")

	gotOne := queryGauntletProtocolPlayers(t, client, "SELECT * FROM players WHERE id = 1", []byte("same-conn-one-off-id"))
	if diff := diffGauntletPlayers(gotOne, map[uint64]string{1: model.players[1]}); diff != "" {
		t.Fatalf("same-connection predicate one-off query mismatch:\n%s", diff)
	}

	finalRows := unsubscribeGauntletProtocolPlayers(t, client, unsubscribeRequestID, queryID)
	if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
		t.Fatalf("same-connection one-off unsubscribe final rows mismatch:\n%s", diff)
	}
}

func TestRuntimeGauntletProtocolSubscribeInitialMatchesOneOff(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			trace := buildGauntletTrace(seed, 24)
			model := gauntletModel{players: map[uint64]string{}}

			assertGauntletSubscribeInitialMatchesOneOff(t, rt, model, fmt.Sprintf("seed %d initial", seed))
			runGauntletTrace(t, rt, &model, trace[:8], 0, fmt.Sprintf("seed %d subscribe/one-off prefix", seed))
			assertGauntletSubscribeInitialMatchesOneOff(t, rt, model, fmt.Sprintf("seed %d after step 7", seed))
			runGauntletTrace(t, rt, &model, trace[8:], 8, fmt.Sprintf("seed %d subscribe/one-off suffix", seed))
			assertGauntletSubscribeInitialMatchesOneOff(t, rt, model, fmt.Sprintf("seed %d final", seed))
		})
	}
}

func TestRuntimeGauntletProtocolSubscribeInitialModel(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			trace := buildGauntletTrace(seed, 24)
			model := gauntletModel{players: map[uint64]string{}}

			assertGauntletSubscribeInitialMatchesModel(t, rt, model, fmt.Sprintf("seed %d initial", seed))
			runGauntletTrace(t, rt, &model, trace[:8], 0, fmt.Sprintf("seed %d subscribe prefix", seed))
			assertGauntletSubscribeInitialMatchesModel(t, rt, model, fmt.Sprintf("seed %d after step 7", seed))
			runGauntletTrace(t, rt, &model, trace[8:], 8, fmt.Sprintf("seed %d subscribe suffix", seed))
			assertGauntletSubscribeInitialMatchesModel(t, rt, model, fmt.Sprintf("seed %d final", seed))
		})
	}
}

func TestRuntimeGauntletProtocolSubscribeAllRowsDeltas(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			const (
				requestID = uint32(7001)
				queryID   = uint32(7002)
			)
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			client := dialGauntletProtocol(t, rt)
			defer client.Close(websocket.StatusNormalClosure, "")

			model := gauntletModel{players: map[uint64]string{}}
			initial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", requestID, queryID)
			if diff := diffGauntletPlayers(initial, model.players); diff != "" {
				t.Fatalf("seed %d initial subscribe snapshot mismatch:\n%s", seed, diff)
			}

			trace := buildGauntletTrace(seed, 32)
			for step, op := range trace {
				wantDelta := gauntletAllRowsDeltaForOp(t, model, op)
				runGauntletTrace(t, rt, &model, trace[step:step+1], step, fmt.Sprintf("seed %d subscribe delta", seed))
				if op.wantStatus == shunter.StatusCommitted {
					gotDelta := readGauntletTransactionUpdateLight(t, client, queryID, fmt.Sprintf("seed %d step %d %s", seed, step, op))
					assertGauntletDeltaEqual(t, gotDelta, wantDelta, fmt.Sprintf("seed %d step %d %s", seed, step, op))
				}
			}
			assertGauntletFailedReducerDoesNotFanout(t, rt, model, seed)
		})
	}
}

func TestRuntimeGauntletProtocolMultiSubscriberFanoutParity(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			const (
				primaryRequestID    = uint32(8921)
				primaryQueryID      = uint32(8922)
				mirrorRequestID     = uint32(8923)
				mirrorQueryID       = uint32(8924)
				mirrorUnsubscribeID = uint32(8925)
			)
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			primaryClient := dialGauntletProtocol(t, rt)
			defer primaryClient.Close(websocket.StatusNormalClosure, "")
			mirrorClient := dialGauntletProtocol(t, rt)

			model := gauntletModel{players: map[uint64]string{}}
			primaryInitial := subscribeGauntletProtocolPlayers(t, primaryClient, "SELECT * FROM players", primaryRequestID, primaryQueryID)
			mirrorInitial := subscribeGauntletProtocolPlayers(t, mirrorClient, "SELECT * FROM players", mirrorRequestID, mirrorQueryID)
			if diff := diffGauntletPlayers(primaryInitial, model.players); diff != "" {
				t.Fatalf("seed %d primary initial snapshot mismatch:\n%s", seed, diff)
			}
			if diff := diffGauntletPlayers(mirrorInitial, primaryInitial); diff != "" {
				t.Fatalf("seed %d mirror/primary initial snapshot mismatch:\n%s", seed, diff)
			}

			trace := buildGauntletTrace(seed, 20)
			for step, op := range trace[:12] {
				label := fmt.Sprintf("seed %d multi-subscriber step %d %s", seed, step, op)
				wantDelta := gauntletAllRowsDeltaForOp(t, model, op)
				runGauntletTrace(t, rt, &model, []gauntletOp{op}, step, "multi-subscriber fanout")
				if op.wantStatus != shunter.StatusCommitted {
					continue
				}

				primaryDelta := readGauntletTransactionUpdateLight(t, primaryClient, primaryQueryID, label+" primary")
				mirrorDelta := readGauntletTransactionUpdateLight(t, mirrorClient, mirrorQueryID, label+" mirror")
				assertGauntletDeltaEqual(t, primaryDelta, wantDelta, label+" primary")
				assertGauntletDeltaEqual(t, mirrorDelta, primaryDelta, label+" mirror/primary")
			}

			mirrorFinalRows := unsubscribeGauntletProtocolPlayers(t, mirrorClient, mirrorUnsubscribeID, mirrorQueryID)
			if diff := diffGauntletPlayers(mirrorFinalRows, model.players); diff != "" {
				t.Fatalf("seed %d mirror unsubscribe final rows mismatch:\n%s", seed, diff)
			}

			nextID := nextUnusedGauntletPlayerID(model)
			afterUnsubscribe := insertPlayerOp(&nextID, "after_mirror_unsubscribe")
			wantDelta := gauntletAllRowsDeltaForOp(t, model, afterUnsubscribe)
			runGauntletTrace(t, rt, &model, []gauntletOp{afterUnsubscribe}, 12, "multi-subscriber after mirror unsubscribe")
			primaryDelta := readGauntletTransactionUpdateLight(t, primaryClient, primaryQueryID, fmt.Sprintf("seed %d after mirror unsubscribe", seed))
			assertGauntletDeltaEqual(t, primaryDelta, wantDelta, fmt.Sprintf("seed %d after mirror unsubscribe primary", seed))
			assertNoGauntletProtocolMessageBeforeClose(t, mirrorClient, 50*time.Millisecond, fmt.Sprintf("seed %d after mirror unsubscribe", seed))
			if err := mirrorClient.Close(websocket.StatusNormalClosure, "mirror unsubscribed"); err != nil {
				t.Fatalf("seed %d close mirror client: %v", seed, err)
			}

			assertGauntletReadMatchesModel(t, rt, model, fmt.Sprintf("seed %d multi-subscriber final", seed))
		})
	}
}

func TestRuntimeGauntletProtocolSameConnectionSubscriptionMultiplex(t *testing.T) {
	const (
		allRequestID       = uint32(8941)
		allQueryID         = uint32(8942)
		targetRequestID    = uint32(8943)
		targetQueryID      = uint32(8944)
		targetUnsubRequest = uint32(8945)
		allUnsubRequest    = uint32(8946)
		targetName         = "target"
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	allInitial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", allRequestID, allQueryID)
	if diff := diffGauntletPlayers(allInitial, model.players); diff != "" {
		t.Fatalf("same-connection all initial snapshot mismatch:\n%s", diff)
	}
	targetInitial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players WHERE name = 'target'", targetRequestID, targetQueryID)
	if diff := diffGauntletPlayers(targetInitial, model.players); diff != "" {
		t.Fatalf("same-connection target initial snapshot mismatch:\n%s", diff)
	}

	targetMatches := func(_ uint64, name string) bool { return name == targetName }
	nextID := uint64(1)
	trace := []gauntletOp{
		insertPlayerOp(&nextID, targetName),
		renamePlayerOp(1, "other"),
		insertPlayerOp(&nextID, "other_two"),
		renamePlayerOp(2, targetName),
		deletePlayerOp(1),
		deletePlayerOp(2),
	}
	for step, op := range trace {
		label := fmt.Sprintf("same-connection multiplex step %d %s", step, op)
		want := map[uint32]gauntletDelta{
			allQueryID: gauntletAllRowsDeltaForOp(t, model, op),
		}
		if targetDelta := gauntletDeltaForOpMatching(t, model, op, targetMatches); !gauntletDeltaIsEmpty(targetDelta) {
			want[targetQueryID] = targetDelta
		}

		runGauntletTrace(t, rt, &model, []gauntletOp{op}, step, "same-connection multiplex")
		got := readGauntletTransactionUpdateLightByQuery(t, client, label)
		assertGauntletDeltasByQueryEqual(t, got, want, label)
	}

	targetFinalRows := unsubscribeGauntletProtocolPlayers(t, client, targetUnsubRequest, targetQueryID)
	if diff := diffGauntletPlayers(targetFinalRows, filterGauntletPlayersByName(model.players, targetName)); diff != "" {
		t.Fatalf("same-connection target unsubscribe final rows mismatch:\n%s", diff)
	}

	afterTargetUnsubscribe := insertPlayerOp(&nextID, targetName)
	wantAfterTargetUnsubscribe := gauntletAllRowsDeltaForOp(t, model, afterTargetUnsubscribe)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterTargetUnsubscribe}, len(trace), "same-connection after target unsubscribe")
	gotAfterTargetUnsubscribe := readGauntletTransactionUpdateLightByQuery(t, client, "same-connection after target unsubscribe")
	assertGauntletDeltasByQueryEqual(t, gotAfterTargetUnsubscribe, map[uint32]gauntletDelta{
		allQueryID: wantAfterTargetUnsubscribe,
	}, "same-connection after target unsubscribe")

	allFinalRows := unsubscribeGauntletProtocolPlayers(t, client, allUnsubRequest, allQueryID)
	if diff := diffGauntletPlayers(allFinalRows, model.players); diff != "" {
		t.Fatalf("same-connection all unsubscribe final rows mismatch:\n%s", diff)
	}

	afterAllUnsubscribe := insertPlayerOp(&nextID, "after_all_unsubscribe")
	runGauntletTrace(t, rt, &model, []gauntletOp{afterAllUnsubscribe}, len(trace)+1, "same-connection after all unsubscribe")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "same-connection after all unsubscribe")
	assertGauntletReadMatchesModel(t, rt, model, "same-connection multiplex final")
}

func TestRuntimeGauntletProtocolSubscribePredicateDeltas(t *testing.T) {
	const (
		idRequestID   = uint32(7401)
		idQueryID     = uint32(7402)
		nameRequestID = uint32(7403)
		nameQueryID   = uint32(7404)
		targetName    = "target"
	)

	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	idClient := dialGauntletProtocol(t, rt)
	defer idClient.Close(websocket.StatusNormalClosure, "")
	nameClient := dialGauntletProtocol(t, rt)
	defer nameClient.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	idMatches := func(id uint64, _ string) bool { return id == 1 }
	nameMatches := func(_ uint64, name string) bool { return name == targetName }

	idInitial := subscribeGauntletProtocolPlayers(t, idClient, "SELECT * FROM players WHERE id = 1", idRequestID, idQueryID)
	if diff := diffGauntletPlayers(idInitial, map[uint64]string{}); diff != "" {
		t.Fatalf("id predicate initial snapshot mismatch:\n%s", diff)
	}
	nameInitial := subscribeGauntletProtocolPlayers(t, nameClient, "SELECT * FROM players WHERE name = 'target'", nameRequestID, nameQueryID)
	if diff := diffGauntletPlayers(nameInitial, map[uint64]string{}); diff != "" {
		t.Fatalf("name predicate initial snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	trace := []gauntletOp{
		insertPlayerOp(&nextID, targetName),
		renamePlayerOp(1, "other"),
		renamePlayerOp(1, targetName),
		deletePlayerOp(1),
		insertPlayerOp(&nextID, targetName),
	}

	for step, op := range trace {
		label := fmt.Sprintf("predicate delta step %d %s", step, op)
		wantIDDelta := gauntletDeltaForOpMatching(t, model, op, idMatches)
		wantNameDelta := gauntletDeltaForOpMatching(t, model, op, nameMatches)

		runGauntletTrace(t, rt, &model, []gauntletOp{op}, step, "predicate delta")

		if gauntletDeltaIsEmpty(wantIDDelta) {
			if step != len(trace)-1 {
				t.Fatalf("%s produced empty id predicate delta before final no-op probe", label)
			}
		} else {
			gotIDDelta := readGauntletTransactionUpdateLight(t, idClient, idQueryID, label+" id predicate")
			assertGauntletDeltaEqual(t, gotIDDelta, wantIDDelta, label+" id predicate")
		}

		if gauntletDeltaIsEmpty(wantNameDelta) {
			t.Fatalf("%s produced empty name predicate delta", label)
		}
		gotNameDelta := readGauntletTransactionUpdateLight(t, nameClient, nameQueryID, label+" name predicate")
		assertGauntletDeltaEqual(t, gotNameDelta, wantNameDelta, label+" name predicate")
	}

	assertNoGauntletProtocolMessageBeforeClose(t, idClient, 50*time.Millisecond, "final non-matching id predicate insert")
	assertGauntletReadMatchesModel(t, rt, model, "predicate delta final")
}

func TestRuntimeGauntletProtocolRejectedSubscribeDoesNotRegister(t *testing.T) {
	const (
		requestID = uint32(7501)
		queryID   = uint32(7502)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	subErr := subscribeGauntletProtocolExpectError(t, client, "SELECT * FROM players WHERE missing = 1", requestID, queryID)
	if subErr.Error == "" {
		t.Fatal("rejected subscribe error = empty")
	}

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	op := insertPlayerOp(&nextID, "after_rejected_subscribe")
	runGauntletTrace(t, rt, &model, []gauntletOp{op}, 0, "rejected subscribe")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "rejected subscribe fanout probe")
	assertGauntletReadMatchesModel(t, rt, model, "rejected subscribe final")
}

func TestRuntimeGauntletProtocolRejectedSubscribeConnectionRecovery(t *testing.T) {
	const (
		rejectedRequestID  = uint32(7521)
		rejectedQueryID    = uint32(7522)
		validRequestID     = uint32(7523)
		validQueryID       = uint32(7524)
		unsubscribeRequest = uint32(7525)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	subErr := subscribeGauntletProtocolExpectError(t, client, "SELECT * FROM players WHERE missing = 1", rejectedRequestID, rejectedQueryID)
	if subErr.Error == "" {
		t.Fatal("rejected subscribe recovery error = empty")
	}

	model := gauntletModel{players: map[uint64]string{}}
	initial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", validRequestID, validQueryID)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("rejected subscribe recovery valid initial snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	op := insertPlayerOp(&nextID, "after_rejected_subscribe_recovery")
	wantDelta := gauntletAllRowsDeltaForOp(t, model, op)
	runGauntletTrace(t, rt, &model, []gauntletOp{op}, 0, "rejected subscribe recovery")
	gotDelta := readGauntletTransactionUpdateLight(t, client, validQueryID, "rejected subscribe recovery")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, "rejected subscribe recovery")

	gotRows := queryGauntletProtocolPlayers(t, client, "SELECT * FROM players", []byte("after-rejected-subscribe-recovery"))
	if diff := diffGauntletPlayers(gotRows, model.players); diff != "" {
		t.Fatalf("rejected subscribe recovery one-off mismatch:\n%s", diff)
	}

	finalRows := unsubscribeGauntletProtocolPlayers(t, client, unsubscribeRequest, validQueryID)
	if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
		t.Fatalf("rejected subscribe recovery unsubscribe final rows mismatch:\n%s", diff)
	}

	afterUnsubscribe := insertPlayerOp(&nextID, "after_rejected_subscribe_recovery_unsubscribe")
	runGauntletTrace(t, rt, &model, []gauntletOp{afterUnsubscribe}, 1, "after rejected subscribe recovery unsubscribe")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "after rejected subscribe recovery unsubscribe")
}

func TestRuntimeGauntletProtocolDisconnectReconnectDoesNotCorruptFanout(t *testing.T) {
	const (
		primaryRequestID   = uint32(7601)
		primaryQueryID     = uint32(7602)
		transientRequestID = uint32(7603)
		transientQueryID   = uint32(7604)
		reconnectRequestID = uint32(7605)
		reconnectQueryID   = uint32(7606)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	primaryClient := dialGauntletProtocol(t, rt)
	defer primaryClient.Close(websocket.StatusNormalClosure, "")
	transientClient := dialGauntletProtocol(t, rt)

	model := gauntletModel{players: map[uint64]string{}}
	primaryInitial := subscribeGauntletProtocolPlayers(t, primaryClient, "SELECT * FROM players", primaryRequestID, primaryQueryID)
	if diff := diffGauntletPlayers(primaryInitial, model.players); diff != "" {
		t.Fatalf("primary initial subscribe snapshot mismatch:\n%s", diff)
	}
	transientInitial := subscribeGauntletProtocolPlayers(t, transientClient, "SELECT * FROM players", transientRequestID, transientQueryID)
	if diff := diffGauntletPlayers(transientInitial, model.players); diff != "" {
		t.Fatalf("transient initial subscribe snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	beforeDisconnect := insertPlayerOp(&nextID, "before_disconnect")
	wantBeforeDisconnect := gauntletAllRowsDeltaForOp(t, model, beforeDisconnect)
	runGauntletTrace(t, rt, &model, []gauntletOp{beforeDisconnect}, 0, "disconnect before close")
	primaryBeforeDisconnect := readGauntletTransactionUpdateLight(t, primaryClient, primaryQueryID, "primary before disconnect")
	assertGauntletDeltaEqual(t, primaryBeforeDisconnect, wantBeforeDisconnect, "primary before disconnect")
	transientBeforeDisconnect := readGauntletTransactionUpdateLight(t, transientClient, transientQueryID, "transient before disconnect")
	assertGauntletDeltaEqual(t, transientBeforeDisconnect, wantBeforeDisconnect, "transient before disconnect")

	if err := transientClient.Close(websocket.StatusNormalClosure, "disconnect gauntlet"); err != nil {
		t.Fatalf("close transient protocol client: %v", err)
	}

	afterDisconnect := insertPlayerOp(&nextID, "after_disconnect")
	wantAfterDisconnect := gauntletAllRowsDeltaForOp(t, model, afterDisconnect)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterDisconnect}, 1, "disconnect after close")
	primaryAfterDisconnect := readGauntletTransactionUpdateLight(t, primaryClient, primaryQueryID, "primary after disconnect")
	assertGauntletDeltaEqual(t, primaryAfterDisconnect, wantAfterDisconnect, "primary after disconnect")

	reconnectClient := dialGauntletProtocol(t, rt)
	defer reconnectClient.Close(websocket.StatusNormalClosure, "")
	reconnectInitial := subscribeGauntletProtocolPlayers(t, reconnectClient, "SELECT * FROM players", reconnectRequestID, reconnectQueryID)
	if diff := diffGauntletPlayers(reconnectInitial, model.players); diff != "" {
		t.Fatalf("reconnect initial subscribe snapshot mismatch:\n%s", diff)
	}

	afterReconnect := renamePlayerOp(1, "after_reconnect")
	wantAfterReconnect := gauntletAllRowsDeltaForOp(t, model, afterReconnect)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterReconnect}, 2, "disconnect after reconnect")
	primaryAfterReconnect := readGauntletTransactionUpdateLight(t, primaryClient, primaryQueryID, "primary after reconnect")
	assertGauntletDeltaEqual(t, primaryAfterReconnect, wantAfterReconnect, "primary after reconnect")
	reconnectAfterReconnect := readGauntletTransactionUpdateLight(t, reconnectClient, reconnectQueryID, "reconnected after reconnect")
	assertGauntletDeltaEqual(t, reconnectAfterReconnect, wantAfterReconnect, "reconnected after reconnect")

	assertGauntletReadMatchesModel(t, rt, model, "disconnect/reconnect final")
}

func TestRuntimeGauntletProtocolSubscribeMultiUnsubscribeMulti(t *testing.T) {
	const (
		subscribeRequestID   = uint32(7701)
		unsubscribeRequestID = uint32(7702)
		queryID              = uint32(7703)
		targetName           = "target"
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	prefix := []gauntletOp{
		insertPlayerOp(&nextID, "one"),
		insertPlayerOp(&nextID, targetName),
	}
	runGauntletTrace(t, rt, &model, prefix, 0, "subscribe multi prefix")

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	matchesMulti := func(id uint64, name string) bool {
		return id == 1 || name == targetName
	}
	initial := subscribeMultiGauntletProtocolPlayers(t, client, []string{
		"SELECT * FROM players WHERE id = 1",
		"SELECT * FROM players WHERE name = 'target'",
	}, subscribeRequestID, queryID)
	wantInitial := gauntletDelta{
		inserts: filterGauntletPlayersMatching(model.players, matchesMulti),
		deletes: map[uint64]string{},
	}
	assertGauntletDeltaEqual(t, initial, wantInitial, "subscribe multi initial")

	leavePredicate := renamePlayerOp(2, "other")
	wantLeavePredicate := gauntletDeltaForOpMatching(t, model, leavePredicate, matchesMulti)
	runGauntletTrace(t, rt, &model, []gauntletOp{leavePredicate}, len(prefix), "subscribe multi live")
	gotLeavePredicate := readGauntletTransactionUpdateLight(t, client, queryID, "subscribe multi leave predicate")
	assertGauntletDeltaEqual(t, gotLeavePredicate, wantLeavePredicate, "subscribe multi leave predicate")

	final := unsubscribeMultiGauntletProtocolPlayers(t, client, unsubscribeRequestID, queryID)
	wantFinal := gauntletDelta{
		inserts: map[uint64]string{},
		deletes: filterGauntletPlayersMatching(model.players, matchesMulti),
	}
	assertGauntletDeltaEqual(t, final, wantFinal, "unsubscribe multi final")

	afterUnsubscribe := insertPlayerOp(&nextID, targetName)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterUnsubscribe}, len(prefix)+1, "subscribe multi after unsubscribe")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "subscribe multi after unsubscribe")
	assertGauntletReadMatchesModel(t, rt, model, "subscribe multi final")
}

func TestRuntimeGauntletProtocolSameConnectionSubscribeMultiAndSingleCoexist(t *testing.T) {
	const (
		multiRequestID      = uint32(7731)
		multiQueryID        = uint32(7732)
		singleRequestID     = uint32(7733)
		singleQueryID       = uint32(7734)
		multiUnsubscribeID  = uint32(7735)
		singleUnsubscribeID = uint32(7736)
		targetName          = "target"
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	prefix := []gauntletOp{
		insertPlayerOp(&nextID, "one"),
		insertPlayerOp(&nextID, targetName),
	}
	runGauntletTrace(t, rt, &model, prefix, 0, "subscribe multi plus single prefix")

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	matchesMulti := func(id uint64, name string) bool {
		return id == 1 || name == targetName
	}
	multiInitial := subscribeMultiGauntletProtocolPlayers(t, client, []string{
		"SELECT * FROM players WHERE id = 1",
		"SELECT * FROM players WHERE name = 'target'",
	}, multiRequestID, multiQueryID)
	assertGauntletDeltaEqual(t, multiInitial, gauntletDelta{
		inserts: filterGauntletPlayersMatching(model.players, matchesMulti),
		deletes: map[uint64]string{},
	}, "same-connection multi initial")

	singleInitial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", singleRequestID, singleQueryID)
	if diff := diffGauntletPlayers(singleInitial, model.players); diff != "" {
		t.Fatalf("same-connection single initial snapshot mismatch:\n%s", diff)
	}

	trace := []gauntletOp{
		insertPlayerOp(&nextID, targetName),
		renamePlayerOp(1, "one_renamed"),
		renamePlayerOp(2, "other"),
		insertPlayerOp(&nextID, "outside_multi"),
	}
	for step, op := range trace {
		label := fmt.Sprintf("same-connection multi plus single step %d %s", step, op)
		want := map[uint32]gauntletDelta{
			singleQueryID: gauntletAllRowsDeltaForOp(t, model, op),
		}
		if multiDelta := gauntletDeltaForOpMatching(t, model, op, matchesMulti); !gauntletDeltaIsEmpty(multiDelta) {
			want[multiQueryID] = multiDelta
		}

		runGauntletTrace(t, rt, &model, []gauntletOp{op}, len(prefix)+step, "subscribe multi plus single live")
		got := readGauntletTransactionUpdateLightByQuery(t, client, label)
		assertGauntletDeltasByQueryEqual(t, got, want, label)
	}

	multiFinal := unsubscribeMultiGauntletProtocolPlayers(t, client, multiUnsubscribeID, multiQueryID)
	assertGauntletDeltaEqual(t, multiFinal, gauntletDelta{
		inserts: map[uint64]string{},
		deletes: filterGauntletPlayersMatching(model.players, matchesMulti),
	}, "same-connection multi unsubscribe final")

	afterMultiUnsubscribe := insertPlayerOp(&nextID, targetName)
	wantAfterMultiUnsubscribe := gauntletAllRowsDeltaForOp(t, model, afterMultiUnsubscribe)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterMultiUnsubscribe}, len(prefix)+len(trace), "after same-connection multi unsubscribe")
	gotAfterMultiUnsubscribe := readGauntletTransactionUpdateLightByQuery(t, client, "after same-connection multi unsubscribe")
	assertGauntletDeltasByQueryEqual(t, gotAfterMultiUnsubscribe, map[uint32]gauntletDelta{
		singleQueryID: wantAfterMultiUnsubscribe,
	}, "after same-connection multi unsubscribe")

	singleFinal := unsubscribeGauntletProtocolPlayers(t, client, singleUnsubscribeID, singleQueryID)
	if diff := diffGauntletPlayers(singleFinal, model.players); diff != "" {
		t.Fatalf("same-connection single unsubscribe final rows mismatch:\n%s", diff)
	}

	afterSingleUnsubscribe := insertPlayerOp(&nextID, "after_single_unsubscribe")
	runGauntletTrace(t, rt, &model, []gauntletOp{afterSingleUnsubscribe}, len(prefix)+len(trace)+1, "after same-connection single unsubscribe")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "after same-connection single unsubscribe")
	assertGauntletReadMatchesModel(t, rt, model, "same-connection multi plus single final")
}

func TestRuntimeGauntletProtocolRejectedSubscribeMultiDoesNotRegisterAnyQuery(t *testing.T) {
	const (
		requestID = uint32(7751)
		queryID   = uint32(7752)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	subErr := subscribeMultiGauntletProtocolExpectError(t, client, []string{
		"SELECT * FROM players",
		"SELECT * FROM missing",
	}, requestID, queryID)
	if subErr.Error == "" {
		t.Fatal("rejected subscribe multi error = empty")
	}
	unsubErr := unsubscribeMultiGauntletProtocolExpectError(t, client, requestID+1, queryID)
	if unsubErr.Error == "" {
		t.Fatal("unsubscribe rejected subscribe multi error = empty")
	}

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	op := insertPlayerOp(&nextID, "after_rejected_subscribe_multi")
	runGauntletTrace(t, rt, &model, []gauntletOp{op}, 0, "rejected subscribe multi")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "rejected subscribe multi fanout probe")
	assertGauntletReadMatchesModel(t, rt, model, "rejected subscribe multi final")
}

func TestRuntimeGauntletProtocolRejectedSubscribeMultiConnectionRecovery(t *testing.T) {
	const (
		rejectedRequestID  = uint32(7761)
		rejectedQueryID    = uint32(7762)
		validRequestID     = uint32(7763)
		validQueryID       = uint32(7764)
		unsubscribeRequest = uint32(7765)
		targetName         = "target"
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	subErr := subscribeMultiGauntletProtocolExpectError(t, client, []string{
		"SELECT * FROM players",
		"SELECT * FROM missing",
	}, rejectedRequestID, rejectedQueryID)
	if subErr.Error == "" {
		t.Fatal("rejected subscribe multi recovery error = empty")
	}

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	prefix := []gauntletOp{
		insertPlayerOp(&nextID, "one"),
		insertPlayerOp(&nextID, targetName),
	}
	runGauntletTrace(t, rt, &model, prefix, 0, "rejected subscribe multi recovery prefix")

	matchesMulti := func(id uint64, name string) bool {
		return id == 1 || name == targetName
	}
	initial := subscribeMultiGauntletProtocolPlayers(t, client, []string{
		"SELECT * FROM players WHERE id = 1",
		"SELECT * FROM players WHERE name = 'target'",
	}, validRequestID, validQueryID)
	wantInitial := gauntletDelta{
		inserts: filterGauntletPlayersMatching(model.players, matchesMulti),
		deletes: map[uint64]string{},
	}
	assertGauntletDeltaEqual(t, initial, wantInitial, "rejected subscribe multi recovery valid initial")

	leavePredicate := renamePlayerOp(2, "other")
	wantLeavePredicate := gauntletDeltaForOpMatching(t, model, leavePredicate, matchesMulti)
	runGauntletTrace(t, rt, &model, []gauntletOp{leavePredicate}, len(prefix), "rejected subscribe multi recovery live")
	gotLeavePredicate := readGauntletTransactionUpdateLight(t, client, validQueryID, "rejected subscribe multi recovery live")
	assertGauntletDeltaEqual(t, gotLeavePredicate, wantLeavePredicate, "rejected subscribe multi recovery live")

	final := unsubscribeMultiGauntletProtocolPlayers(t, client, unsubscribeRequest, validQueryID)
	wantFinal := gauntletDelta{
		inserts: map[uint64]string{},
		deletes: filterGauntletPlayersMatching(model.players, matchesMulti),
	}
	assertGauntletDeltaEqual(t, final, wantFinal, "rejected subscribe multi recovery final")

	afterUnsubscribe := insertPlayerOp(&nextID, targetName)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterUnsubscribe}, len(prefix)+1, "after rejected subscribe multi recovery unsubscribe")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "after rejected subscribe multi recovery unsubscribe")
}

func TestRuntimeGauntletProtocolRepeatedSubscribeCyclesMatchLongLived(t *testing.T) {
	const (
		longRequestID = uint32(7801)
		longQueryID   = uint32(7802)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	longClient := dialGauntletProtocol(t, rt)
	defer longClient.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	longInitial := subscribeGauntletProtocolPlayers(t, longClient, "SELECT * FROM players", longRequestID, longQueryID)
	if diff := diffGauntletPlayers(longInitial, model.players); diff != "" {
		t.Fatalf("long-lived initial subscribe snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	cycleOps := []gauntletOp{
		insertPlayerOp(&nextID, "cycle_one"),
		renamePlayerOp(1, "cycle_one_renamed"),
		insertPlayerOp(&nextID, "cycle_two"),
		deletePlayerOp(1),
	}

	step := 0
	for cycle, op := range cycleOps {
		requestID := uint32(7810 + cycle*10)
		queryID := uint32(7811 + cycle*10)
		label := fmt.Sprintf("subscribe cycle %d %s", cycle, op)

		cycleClient := dialGauntletProtocol(t, rt)
		cycleInitial := subscribeGauntletProtocolPlayers(t, cycleClient, "SELECT * FROM players", requestID, queryID)
		if diff := diffGauntletPlayers(cycleInitial, model.players); diff != "" {
			t.Fatalf("%s initial subscribe snapshot mismatch:\n%s", label, diff)
		}

		wantDelta := gauntletAllRowsDeltaForOp(t, model, op)
		runGauntletTrace(t, rt, &model, []gauntletOp{op}, step, label)
		step++
		longDelta := readGauntletTransactionUpdateLight(t, longClient, longQueryID, label+" long-lived")
		assertGauntletDeltaEqual(t, longDelta, wantDelta, label+" long-lived")
		cycleDelta := readGauntletTransactionUpdateLight(t, cycleClient, queryID, label+" short-lived")
		assertGauntletDeltaEqual(t, cycleDelta, wantDelta, label+" short-lived")

		finalRows := unsubscribeGauntletProtocolPlayers(t, cycleClient, requestID+1, queryID)
		if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
			t.Fatalf("%s unsubscribe final rows mismatch:\n%s", label, diff)
		}

		probe := insertPlayerOp(&nextID, fmt.Sprintf("post_cycle_%d", cycle))
		wantProbeDelta := gauntletAllRowsDeltaForOp(t, model, probe)
		runGauntletTrace(t, rt, &model, []gauntletOp{probe}, step, label+" post-unsubscribe probe")
		step++
		longProbeDelta := readGauntletTransactionUpdateLight(t, longClient, longQueryID, label+" post-unsubscribe probe long-lived")
		assertGauntletDeltaEqual(t, longProbeDelta, wantProbeDelta, label+" post-unsubscribe probe long-lived")
		assertNoGauntletProtocolMessageBeforeClose(t, cycleClient, 50*time.Millisecond, label+" post-unsubscribe probe")
		if err := cycleClient.Close(websocket.StatusNormalClosure, "cycle complete"); err != nil {
			t.Fatalf("%s close cycle protocol client: %v", label, err)
		}
	}

	assertGauntletReadMatchesModel(t, rt, model, "repeated subscribe cycles final")
}

func TestRuntimeGauntletPanicReducerRollsBack(t *testing.T) {
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)

	runtimePanic := panicAfterInsertOp(nextID, "runtime_panic")
	res, err := rt.CallReducer(context.Background(), runtimePanic.reducer, []byte(runtimePanic.args))
	if err != nil {
		t.Fatalf("%s admission error: %v", runtimePanic, err)
	}
	advanceGauntletModel(t, &model, runtimePanic, gauntletReducerOutcomeFromResult(res), runtimePanic.String())
	assertGauntletReadMatchesModel(t, rt, model, "after runtime panic")

	afterRuntimePanic := insertPlayerOp(&nextID, "after_runtime_panic")
	runGauntletTrace(t, rt, &model, []gauntletOp{afterRuntimePanic}, 0, "after runtime panic")

	subscriber := dialGauntletProtocol(t, rt)
	subscribeInitial := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 7901, 7902)
	if diff := diffGauntletPlayers(subscribeInitial, model.players); diff != "" {
		t.Fatalf("panic subscriber initial snapshot mismatch:\n%s", diff)
	}

	caller := dialGauntletProtocol(t, rt)
	defer caller.Close(websocket.StatusNormalClosure, "")

	protocolPanic := panicAfterInsertOp(nextID, "protocol_panic")
	protocolOutcome := callGauntletProtocolReducer(t, caller, protocolPanic, 7903, "protocol panic")
	if protocolOutcome.status != shunter.StatusFailedUser {
		t.Fatalf("protocol panic status = %v, want collapsed protocol failure %v", protocolOutcome.status, shunter.StatusFailedUser)
	}
	if protocolOutcome.err == "" {
		t.Fatal("protocol panic error = empty")
	}
	assertGauntletReadMatchesModel(t, rt, model, "after protocol panic")
	assertNoGauntletProtocolMessageBeforeClose(t, subscriber, 50*time.Millisecond, "protocol panic subscriber fanout")
	if err := subscriber.Close(websocket.StatusNormalClosure, "panic probe complete"); err != nil {
		t.Fatalf("close panic subscriber: %v", err)
	}

	afterProtocolPanic := insertPlayerOp(&nextID, "after_protocol_panic")
	runGauntletTrace(t, rt, &model, []gauntletOp{afterProtocolPanic}, 1, "after protocol panic")
	assertGauntletReadMatchesModel(t, rt, model, "panic reducer final")
}

func TestRuntimeGauntletUnknownReducerDoesNotMutateOrFanout(t *testing.T) {
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	runRejectedReducerAdmissionGauntlet(t, rt, &model, &nextID, unknownReducerOp, 7921, "unknown reducer")
}

func TestRuntimeGauntletReservedLifecycleReducerDoesNotMutateOrFanout(t *testing.T) {
	for _, reducerName := range []string{"OnConnect", "OnDisconnect"} {
		t.Run(reducerName, func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			model := gauntletModel{players: map[uint64]string{}}
			nextID := uint64(1)
			makeOp := func(id uint64, name string) gauntletOp {
				return lifecycleReducerOp(reducerName, id, name)
			}
			runRejectedReducerAdmissionGauntlet(t, rt, &model, &nextID, makeOp, 7941, "reserved "+reducerName)
		})
	}
}

func TestRuntimeGauntletProtocolUnsubscribeStopsUpdates(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			const (
				subscribeRequestID   = uint32(7201)
				unsubscribeRequestID = uint32(7202)
				queryID              = uint32(7203)
			)
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			trace := buildGauntletTrace(seed, 12)
			model := gauntletModel{players: map[uint64]string{}}
			runGauntletTrace(t, rt, &model, trace, 0, fmt.Sprintf("seed %d unsubscribe prefix", seed))

			client := dialGauntletProtocol(t, rt)
			defer client.Close(websocket.StatusNormalClosure, "")

			initial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", subscribeRequestID, queryID)
			if diff := diffGauntletPlayers(initial, model.players); diff != "" {
				t.Fatalf("seed %d unsubscribe initial snapshot mismatch:\n%s", seed, diff)
			}

			nextID := nextUnusedGauntletPlayerID(model)
			beforeUnsubscribe := insertPlayerOp(&nextID, "before_unsubscribe")
			wantDelta := gauntletAllRowsDeltaForOp(t, model, beforeUnsubscribe)
			runGauntletTrace(t, rt, &model, []gauntletOp{beforeUnsubscribe}, len(trace), fmt.Sprintf("seed %d before unsubscribe", seed))
			gotDelta := readGauntletTransactionUpdateLight(t, client, queryID, fmt.Sprintf("seed %d before unsubscribe %s", seed, beforeUnsubscribe))
			assertGauntletDeltaEqual(t, gotDelta, wantDelta, fmt.Sprintf("seed %d before unsubscribe %s", seed, beforeUnsubscribe))

			finalRows := unsubscribeGauntletProtocolPlayers(t, client, unsubscribeRequestID, queryID)
			if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
				t.Fatalf("seed %d unsubscribe final rows mismatch:\n%s", seed, diff)
			}

			afterUnsubscribe := insertPlayerOp(&nextID, "after_unsubscribe")
			runGauntletTrace(t, rt, &model, []gauntletOp{afterUnsubscribe}, len(trace)+1, fmt.Sprintf("seed %d after unsubscribe", seed))
			assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, fmt.Sprintf("seed %d after unsubscribe %s", seed, afterUnsubscribe))
		})
	}
}

func TestRuntimeGauntletProtocolUnknownUnsubscribeDoesNotCorruptFanout(t *testing.T) {
	const (
		longRequestID       = uint32(8051)
		longQueryID         = uint32(8052)
		unknownRequestID    = uint32(8053)
		unknownQueryID      = uint32(8054)
		followupRequestID   = uint32(8055)
		followupQueryID     = uint32(8056)
		missingMultiReqID   = uint32(8057)
		missingMultiQueryID = uint32(8058)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{}}
	longClient := dialGauntletProtocol(t, rt)
	defer longClient.Close(websocket.StatusNormalClosure, "")
	initial := subscribeGauntletProtocolPlayers(t, longClient, "SELECT * FROM players", longRequestID, longQueryID)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("unknown unsubscribe long-lived initial snapshot mismatch:\n%s", diff)
	}

	errorClient := dialGauntletProtocol(t, rt)
	defer errorClient.Close(websocket.StatusNormalClosure, "")
	subErr := unsubscribeGauntletProtocolExpectError(t, errorClient, unknownRequestID, unknownQueryID)
	if subErr.Error == "" {
		t.Fatal("unknown unsubscribe error = empty")
	}

	nextID := uint64(1)
	afterSingleError := insertPlayerOp(&nextID, "after_unknown_unsubscribe")
	wantSingleDelta := gauntletAllRowsDeltaForOp(t, model, afterSingleError)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterSingleError}, 0, "after unknown unsubscribe")
	gotSingleDelta := readGauntletTransactionUpdateLight(t, longClient, longQueryID, "after unknown unsubscribe long-lived")
	assertGauntletDeltaEqual(t, gotSingleDelta, wantSingleDelta, "after unknown unsubscribe long-lived")

	followupRows := subscribeGauntletProtocolPlayers(t, errorClient, "SELECT * FROM players", followupRequestID, followupQueryID)
	if diff := diffGauntletPlayers(followupRows, model.players); diff != "" {
		t.Fatalf("unknown unsubscribe follow-up subscribe snapshot mismatch:\n%s", diff)
	}
	finalRows := unsubscribeGauntletProtocolPlayers(t, errorClient, followupRequestID+1, followupQueryID)
	if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
		t.Fatalf("unknown unsubscribe follow-up unsubscribe final rows mismatch:\n%s", diff)
	}

	multiErr := unsubscribeMultiGauntletProtocolExpectError(t, errorClient, missingMultiReqID, missingMultiQueryID)
	if multiErr.Error == "" {
		t.Fatal("unknown unsubscribe multi error = empty")
	}

	afterMultiError := insertPlayerOp(&nextID, "after_unknown_unsubscribe_multi")
	wantMultiDelta := gauntletAllRowsDeltaForOp(t, model, afterMultiError)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterMultiError}, 1, "after unknown unsubscribe multi")
	gotMultiDelta := readGauntletTransactionUpdateLight(t, longClient, longQueryID, "after unknown unsubscribe multi long-lived")
	assertGauntletDeltaEqual(t, gotMultiDelta, wantMultiDelta, "after unknown unsubscribe multi long-lived")
	assertGauntletReadMatchesModel(t, rt, model, "unknown unsubscribe final")
}

func TestRuntimeGauntletProtocolUnknownUnsubscribePreservesSameConnectionSubscription(t *testing.T) {
	const (
		subscribeRequestID   = uint32(8091)
		queryID              = uint32(8092)
		missingRequestID     = uint32(8093)
		missingQueryID       = uint32(8094)
		unsubscribeRequestID = uint32(8095)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	initial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", subscribeRequestID, queryID)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("same-connection unknown unsubscribe initial snapshot mismatch:\n%s", diff)
	}

	subErr := unsubscribeGauntletProtocolExpectError(t, client, missingRequestID, missingQueryID)
	if subErr.Error == "" {
		t.Fatal("same-connection unknown unsubscribe error = empty")
	}

	nextID := uint64(1)
	afterUnknown := insertPlayerOp(&nextID, "after_same_connection_unknown_unsubscribe")
	wantDelta := gauntletAllRowsDeltaForOp(t, model, afterUnknown)
	runGauntletTrace(t, rt, &model, []gauntletOp{afterUnknown}, 0, "after same-connection unknown unsubscribe")
	gotDelta := readGauntletTransactionUpdateLight(t, client, queryID, "after same-connection unknown unsubscribe")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, "after same-connection unknown unsubscribe")

	finalRows := unsubscribeGauntletProtocolPlayers(t, client, unsubscribeRequestID, queryID)
	if diff := diffGauntletPlayers(finalRows, model.players); diff != "" {
		t.Fatalf("same-connection unknown unsubscribe final rows mismatch:\n%s", diff)
	}

	afterUnsubscribe := insertPlayerOp(&nextID, "after_same_connection_unknown_unsubscribe_final")
	runGauntletTrace(t, rt, &model, []gauntletOp{afterUnsubscribe}, 1, "after same-connection unknown unsubscribe final")
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, "after same-connection unknown unsubscribe final")
}

func TestRuntimeGauntletProtocolCallReducerModel(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			client := dialGauntletProtocol(t, rt)
			defer client.Close(websocket.StatusNormalClosure, "")

			trace := buildGauntletTrace(seed, 32)
			model := gauntletModel{players: map[uint64]string{}}
			assertGauntletReadMatchesModel(t, rt, model, fmt.Sprintf("seed %d protocol call initial", seed))

			runGauntletProtocolTrace(t, rt, client, &model, trace, 0, 7300, fmt.Sprintf("seed %d protocol call", seed))
		})
	}
}

func TestRuntimeGauntletProtocolCallReducerOneOffReadYourWrites(t *testing.T) {
	for _, seed := range []int64{1, 17, 20260427} {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rt := buildGauntletRuntime(t, t.TempDir())
			defer rt.Close()

			client := dialGauntletProtocol(t, rt)
			defer client.Close(websocket.StatusNormalClosure, "")

			trace := buildGauntletTrace(seed, 24)
			model := gauntletModel{players: map[uint64]string{}}
			assertGauntletProtocolQueriesMatchModel(t, client, model, fmt.Sprintf("seed %d protocol call/read initial", seed))

			for step, op := range trace {
				label := fmt.Sprintf("seed %d protocol call/read step %d %s", seed, step, op)
				outcome := callGauntletProtocolReducer(t, client, op, uint32(7350+step), label)
				advanceGauntletModel(t, &model, op, outcome, label)
				assertGauntletReadMatchesModel(t, rt, model, label)
				assertGauntletProtocolQueriesMatchModel(t, client, model, label)
			}
		})
	}
}

func TestRuntimeGauntletProtocolCallReducerHeavyUpdateMatchesSubscribedDelta(t *testing.T) {
	const (
		subscribeRequestID = uint32(8061)
		queryID            = uint32(8062)
		successRequestID   = uint32(8063)
		failedRequestID    = uint32(8064)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	caller := dialGauntletProtocol(t, rt)
	defer caller.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	initial := subscribeGauntletProtocolPlayers(t, caller, "SELECT * FROM players", subscribeRequestID, queryID)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("heavy call reducer initial subscribe snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	successOp := insertPlayerOp(&nextID, "heavy_success")
	wantSuccessDelta := gauntletAllRowsDeltaForOp(t, model, successOp)
	successUpdate := callGauntletProtocolReducerUpdateWithFlags(t, caller, successOp, successRequestID, protocol.CallReducerFlagsFullUpdate, "heavy success")
	successStatus, ok := successUpdate.Status.(protocol.StatusCommitted)
	if !ok {
		t.Fatalf("heavy success status = %T, want StatusCommitted", successUpdate.Status)
	}
	gotSuccessDelta := decodeGauntletSubscriptionUpdates(t, successStatus.Update, queryID, "heavy success")
	assertGauntletDeltaEqual(t, gotSuccessDelta, wantSuccessDelta, "heavy success")
	advanceGauntletModel(t, &model, successOp, gauntletReducerOutcome{status: shunter.StatusCommitted}, "heavy success")
	assertGauntletReadMatchesModel(t, rt, model, "after heavy success")

	failedOp := failAfterInsertOp(nextID, "heavy_failure")
	failedOutcome := callGauntletProtocolReducerWithFlags(t, caller, failedOp, failedRequestID, protocol.CallReducerFlagsFullUpdate, "heavy failure")
	advanceGauntletModel(t, &model, failedOp, failedOutcome, "heavy failure")
	assertGauntletReadMatchesModel(t, rt, model, "after heavy failure")
	assertNoGauntletProtocolMessageBeforeClose(t, caller, 50*time.Millisecond, "heavy call reducer duplicate light update")
}

func TestRuntimeGauntletProtocolCallReducerHeavyUpdateMultiplex(t *testing.T) {
	const (
		allRequestID       = uint32(8071)
		allQueryID         = uint32(8072)
		targetRequestID    = uint32(8073)
		targetQueryID      = uint32(8074)
		targetUnsubRequest = uint32(8075)
		targetName         = "target"
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	caller := dialGauntletProtocol(t, rt)
	defer caller.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	allInitial := subscribeGauntletProtocolPlayers(t, caller, "SELECT * FROM players", allRequestID, allQueryID)
	if diff := diffGauntletPlayers(allInitial, model.players); diff != "" {
		t.Fatalf("heavy multiplex all initial snapshot mismatch:\n%s", diff)
	}
	targetInitial := subscribeGauntletProtocolPlayers(t, caller, "SELECT * FROM players WHERE name = 'target'", targetRequestID, targetQueryID)
	if diff := diffGauntletPlayers(targetInitial, model.players); diff != "" {
		t.Fatalf("heavy multiplex target initial snapshot mismatch:\n%s", diff)
	}

	targetMatches := func(_ uint64, name string) bool { return name == targetName }
	callAndAssert := func(step int, requestID uint32, op gauntletOp) {
		t.Helper()
		label := fmt.Sprintf("heavy multiplex step %d %s", step, op)
		want := map[uint32]gauntletDelta{
			allQueryID: gauntletAllRowsDeltaForOp(t, model, op),
		}
		if targetDelta := gauntletDeltaForOpMatching(t, model, op, targetMatches); !gauntletDeltaIsEmpty(targetDelta) {
			want[targetQueryID] = targetDelta
		}

		update := callGauntletProtocolReducerUpdateWithFlags(t, caller, op, requestID, protocol.CallReducerFlagsFullUpdate, label)
		status, ok := update.Status.(protocol.StatusCommitted)
		if !ok {
			t.Fatalf("%s status = %T, want StatusCommitted", label, update.Status)
		}
		got := decodeGauntletSubscriptionUpdatesByQuery(t, status.Update, label)
		assertGauntletDeltasByQueryEqual(t, got, want, label)
		advanceGauntletModel(t, &model, op, gauntletReducerOutcome{status: shunter.StatusCommitted}, label)
		assertGauntletReadMatchesModel(t, rt, model, label)
	}

	nextID := uint64(1)
	callAndAssert(0, 8076, insertPlayerOp(&nextID, targetName))
	callAndAssert(1, 8077, insertPlayerOp(&nextID, "other"))
	callAndAssert(2, 8078, renamePlayerOp(1, "other_renamed"))

	targetFinalRows := unsubscribeGauntletProtocolPlayers(t, caller, targetUnsubRequest, targetQueryID)
	if diff := diffGauntletPlayers(targetFinalRows, filterGauntletPlayersByName(model.players, targetName)); diff != "" {
		t.Fatalf("heavy multiplex target unsubscribe final rows mismatch:\n%s", diff)
	}

	afterTargetUnsubscribe := insertPlayerOp(&nextID, targetName)
	label := fmt.Sprintf("heavy multiplex after target unsubscribe %s", afterTargetUnsubscribe)
	wantAfterTargetUnsubscribe := map[uint32]gauntletDelta{
		allQueryID: gauntletAllRowsDeltaForOp(t, model, afterTargetUnsubscribe),
	}
	update := callGauntletProtocolReducerUpdateWithFlags(t, caller, afterTargetUnsubscribe, 8079, protocol.CallReducerFlagsFullUpdate, label)
	status, ok := update.Status.(protocol.StatusCommitted)
	if !ok {
		t.Fatalf("%s status = %T, want StatusCommitted", label, update.Status)
	}
	gotAfterTargetUnsubscribe := decodeGauntletSubscriptionUpdatesByQuery(t, status.Update, label)
	assertGauntletDeltasByQueryEqual(t, gotAfterTargetUnsubscribe, wantAfterTargetUnsubscribe, label)
	advanceGauntletModel(t, &model, afterTargetUnsubscribe, gauntletReducerOutcome{status: shunter.StatusCommitted}, label)
	assertGauntletReadMatchesModel(t, rt, model, label)
}

func TestRuntimeGauntletProtocolCallReducerNoSuccessNotify(t *testing.T) {
	const (
		subscribeRequestID = uint32(8001)
		queryID            = uint32(8002)
		successRequestID   = uint32(8003)
		failedRequestID    = uint32(8004)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	subscriber := dialGauntletProtocol(t, rt)
	defer subscriber.Close(websocket.StatusNormalClosure, "")
	initial := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", subscribeRequestID, queryID)
	model := gauntletModel{players: map[uint64]string{}}
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("no-success subscriber initial snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	successCaller := dialGauntletProtocol(t, rt)
	successOp := insertPlayerOp(&nextID, "no_success_notify")
	wantSuccessDelta := gauntletAllRowsDeltaForOp(t, model, successOp)
	writeGauntletProtocolReducerCall(t, successCaller, successOp, successRequestID, protocol.CallReducerFlagsNoSuccessNotify, "no-success notify success")
	gotSuccessDelta := readGauntletTransactionUpdateLight(t, subscriber, queryID, "no-success notify success subscriber")
	assertGauntletDeltaEqual(t, gotSuccessDelta, wantSuccessDelta, "no-success notify success subscriber")
	advanceGauntletModel(t, &model, successOp, gauntletReducerOutcome{status: shunter.StatusCommitted}, "no-success notify success")
	assertGauntletReadMatchesModel(t, rt, model, "after no-success notify success")
	assertNoGauntletProtocolMessageBeforeClose(t, successCaller, 50*time.Millisecond, "no-success notify success caller")
	if err := successCaller.Close(websocket.StatusNormalClosure, "no-success success complete"); err != nil {
		t.Fatalf("close no-success success caller: %v", err)
	}

	failedCaller := dialGauntletProtocol(t, rt)
	defer failedCaller.Close(websocket.StatusNormalClosure, "")
	failedOp := failAfterInsertOp(nextID, "no_success_failure")
	failedOutcome := callGauntletProtocolReducerWithFlags(t, failedCaller, failedOp, failedRequestID, protocol.CallReducerFlagsNoSuccessNotify, "no-success notify failure")
	advanceGauntletModel(t, &model, failedOp, failedOutcome, "no-success notify failure")
	assertGauntletReadMatchesModel(t, rt, model, "after no-success notify failure")
	assertNoGauntletProtocolMessageBeforeClose(t, subscriber, 50*time.Millisecond, "no-success notify failed reducer subscriber")
}

func TestRuntimeGauntletProtocolCallReducerNoSuccessNotifySubscribedCaller(t *testing.T) {
	const (
		callerSubscribeRequestID   = uint32(8011)
		callerQueryID              = uint32(8012)
		observerSubscribeRequestID = uint32(8013)
		observerQueryID            = uint32(8014)
		successRequestID           = uint32(8015)
		failedSubscribeRequestID   = uint32(8016)
		failedQueryID              = uint32(8017)
		failedRequestID            = uint32(8018)
	)
	rt := buildGauntletRuntime(t, t.TempDir())
	defer rt.Close()

	caller := dialGauntletProtocol(t, rt)
	observer := dialGauntletProtocol(t, rt)
	defer observer.Close(websocket.StatusNormalClosure, "")

	model := gauntletModel{players: map[uint64]string{}}
	callerInitial := subscribeGauntletProtocolPlayers(t, caller, "SELECT * FROM players", callerSubscribeRequestID, callerQueryID)
	if diff := diffGauntletPlayers(callerInitial, model.players); diff != "" {
		t.Fatalf("no-success subscribed caller initial snapshot mismatch:\n%s", diff)
	}
	observerInitial := subscribeGauntletProtocolPlayers(t, observer, "SELECT * FROM players", observerSubscribeRequestID, observerQueryID)
	if diff := diffGauntletPlayers(observerInitial, model.players); diff != "" {
		t.Fatalf("no-success observer initial snapshot mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	successOp := insertPlayerOp(&nextID, "no_success_subscribed")
	wantSuccessDelta := gauntletAllRowsDeltaForOp(t, model, successOp)
	writeGauntletProtocolReducerCall(t, caller, successOp, successRequestID, protocol.CallReducerFlagsNoSuccessNotify, "no-success subscribed caller success")
	gotObserverDelta := readGauntletTransactionUpdateLight(t, observer, observerQueryID, "no-success subscribed caller observer")
	assertGauntletDeltaEqual(t, gotObserverDelta, wantSuccessDelta, "no-success subscribed caller observer")
	advanceGauntletModel(t, &model, successOp, gauntletReducerOutcome{status: shunter.StatusCommitted}, "no-success subscribed caller success")
	assertGauntletReadMatchesModel(t, rt, model, "after no-success subscribed caller success")
	assertNoGauntletProtocolMessageBeforeClose(t, caller, 50*time.Millisecond, "no-success subscribed caller")
	if err := caller.Close(websocket.StatusNormalClosure, "no-success subscribed caller complete"); err != nil {
		t.Fatalf("close no-success subscribed caller: %v", err)
	}

	failedCaller := dialGauntletProtocol(t, rt)
	defer failedCaller.Close(websocket.StatusNormalClosure, "")
	failedInitial := subscribeGauntletProtocolPlayers(t, failedCaller, "SELECT * FROM players", failedSubscribeRequestID, failedQueryID)
	if diff := diffGauntletPlayers(failedInitial, model.players); diff != "" {
		t.Fatalf("no-success failed caller initial snapshot mismatch:\n%s", diff)
	}
	failedOp := failAfterInsertOp(nextID, "no_success_subscribed_failure")
	failedOutcome := callGauntletProtocolReducerWithFlags(t, failedCaller, failedOp, failedRequestID, protocol.CallReducerFlagsNoSuccessNotify, "no-success subscribed caller failure")
	advanceGauntletModel(t, &model, failedOp, failedOutcome, "no-success subscribed caller failure")
	assertGauntletReadMatchesModel(t, rt, model, "after no-success subscribed caller failure")
	assertNoGauntletProtocolMessageBeforeClose(t, observer, 50*time.Millisecond, "no-success subscribed caller failure observer")
}

type gauntletModel struct {
	players map[uint64]string
}

type gauntletOp struct {
	kind       string
	reducer    string
	args       string
	wantStatus shunter.ReducerStatus
	apply      func(*gauntletModel)
}

func (op gauntletOp) String() string {
	return fmt.Sprintf("%s(%s)", op.kind, op.args)
}

func buildGauntletTrace(seed int64, steps int) []gauntletOp {
	model := gauntletModel{players: map[uint64]string{}}
	rng := rand.New(rand.NewSource(seed))
	nextID := uint64(1)
	trace := make([]gauntletOp, 0, steps)
	for step := 0; step < steps; step++ {
		op := nextGauntletOp(rng, model, &nextID)
		trace = append(trace, op)
		if op.wantStatus == shunter.StatusCommitted {
			op.apply(&model)
		}
	}
	return trace
}

type gauntletReducerOutcome struct {
	status shunter.ReducerStatus
	err    string
}

func gauntletReducerOutcomeFromResult(res shunter.ReducerResult) gauntletReducerOutcome {
	outcome := gauntletReducerOutcome{status: res.Status}
	if res.Error != nil {
		outcome.err = res.Error.Error()
	}
	return outcome
}

func advanceGauntletModel(t *testing.T, model *gauntletModel, op gauntletOp, outcome gauntletReducerOutcome, label string) {
	t.Helper()
	if outcome.status != op.wantStatus {
		t.Fatalf("%s status = %v, want %v; reducer err = %s", label, outcome.status, op.wantStatus, outcome.err)
	}
	if op.wantStatus == shunter.StatusCommitted {
		op.apply(model)
	} else if outcome.err == "" {
		t.Fatalf("%s error = empty, want reducer failure detail", label)
	}
}

func runGauntletTrace(t *testing.T, rt *shunter.Runtime, model *gauntletModel, trace []gauntletOp, startStep int, label string) {
	t.Helper()
	for i, op := range trace {
		step := startStep + i
		res, err := rt.CallReducer(context.Background(), op.reducer, []byte(op.args))
		if err != nil {
			t.Fatalf("%s step %d %s admission error: %v", label, step, op, err)
		}
		advanceGauntletModel(t, model, op, gauntletReducerOutcomeFromResult(res), fmt.Sprintf("%s step %d %s", label, step, op))
		assertGauntletReadMatchesModel(t, rt, *model, fmt.Sprintf("%s after step %d %s", label, step, op))
	}
}

func runGauntletProtocolTrace(t *testing.T, rt *shunter.Runtime, client *websocket.Conn, model *gauntletModel, trace []gauntletOp, startStep int, requestIDBase uint32, label string) {
	t.Helper()
	for i, op := range trace {
		step := startStep + i
		stepLabel := fmt.Sprintf("%s step %d %s", label, step, op)
		outcome := callGauntletProtocolReducer(t, client, op, requestIDBase+uint32(step), stepLabel)
		advanceGauntletModel(t, model, op, outcome, stepLabel)
		assertGauntletReadMatchesModel(t, rt, *model, stepLabel)
	}
}

func nextGauntletOp(rng *rand.Rand, model gauntletModel, nextID *uint64) gauntletOp {
	if len(model.players) == 0 {
		if rng.Intn(4) == 0 {
			return failAfterInsertOp(*nextID, gauntletName(rng))
		}
		return insertPlayerOp(nextID, gauntletName(rng))
	}

	switch n := rng.Intn(100); {
	case n < 42:
		return insertPlayerOp(nextID, gauntletName(rng))
	case n < 68:
		id := gauntletExistingID(rng, model)
		return renamePlayerOp(id, gauntletName(rng))
	case n < 86:
		id := gauntletExistingID(rng, model)
		return deletePlayerOp(id)
	default:
		return failAfterInsertOp(*nextID, gauntletName(rng))
	}
}

func insertPlayerOp(nextID *uint64, name string) gauntletOp {
	id := *nextID
	*nextID++
	return gauntletOp{
		kind:       "insert",
		reducer:    "insert_player",
		args:       fmt.Sprintf("%d:%s", id, name),
		wantStatus: shunter.StatusCommitted,
		apply: func(model *gauntletModel) {
			model.players[id] = name
		},
	}
}

func renamePlayerOp(id uint64, name string) gauntletOp {
	return gauntletOp{
		kind:       "rename",
		reducer:    "rename_player",
		args:       fmt.Sprintf("%d:%s", id, name),
		wantStatus: shunter.StatusCommitted,
		apply: func(model *gauntletModel) {
			model.players[id] = name
		},
	}
}

func deletePlayerOp(id uint64) gauntletOp {
	return gauntletOp{
		kind:       "delete",
		reducer:    "delete_player",
		args:       strconv.FormatUint(id, 10),
		wantStatus: shunter.StatusCommitted,
		apply: func(model *gauntletModel) {
			delete(model.players, id)
		},
	}
}

func failAfterInsertOp(id uint64, name string) gauntletOp {
	return gauntletOp{
		kind:       "fail_after_insert",
		reducer:    "fail_after_insert",
		args:       fmt.Sprintf("%d:%s", id, name),
		wantStatus: shunter.StatusFailedUser,
		apply:      func(*gauntletModel) {},
	}
}

func panicAfterInsertOp(id uint64, name string) gauntletOp {
	return gauntletOp{
		kind:       "panic_after_insert",
		reducer:    "panic_after_insert",
		args:       fmt.Sprintf("%d:%s", id, name),
		wantStatus: shunter.StatusFailedPanic,
		apply:      func(*gauntletModel) {},
	}
}

func unknownReducerOp(id uint64, name string) gauntletOp {
	return gauntletOp{
		kind:       "unknown_reducer",
		reducer:    "missing_reducer",
		args:       fmt.Sprintf("%d:%s", id, name),
		wantStatus: shunter.StatusFailedInternal,
		apply:      func(*gauntletModel) {},
	}
}

func lifecycleReducerOp(reducer string, id uint64, name string) gauntletOp {
	return gauntletOp{
		kind:       "reserved_lifecycle",
		reducer:    reducer,
		args:       fmt.Sprintf("%d:%s", id, name),
		wantStatus: shunter.StatusFailedInternal,
		apply:      func(*gauntletModel) {},
	}
}

func gauntletExistingID(rng *rand.Rand, model gauntletModel) uint64 {
	ids := make([]uint64, 0, len(model.players))
	for id := range model.players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids[rng.Intn(len(ids))]
}

func gauntletName(rng *rand.Rand) string {
	return fmt.Sprintf("player_%03d", rng.Intn(1000))
}

func buildGauntletRuntime(t *testing.T, dataDir string) *shunter.Runtime {
	t.Helper()
	mod := shunter.NewModule("gauntlet").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "players",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true},
				{Name: "name", Type: types.KindString},
			},
		}).
		Reducer("insert_player", insertPlayerReducer).
		Reducer("rename_player", renamePlayerReducer).
		Reducer("delete_player", deletePlayerReducer).
		Reducer("fail_after_insert", failAfterInsertReducer).
		Reducer("panic_after_insert", panicAfterInsertReducer)

	rt, err := shunter.Build(mod, shunter.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	return rt
}

func dialGauntletProtocol(t *testing.T, rt *shunter.Runtime) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(rt.HTTPHandler())
	t.Cleanup(srv.Close)

	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{protocol.SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("protocol dial failed: %v (resp=%v)", err, resp)
	}

	_, msg := readGauntletProtocolMessage(t, client, "identity token")
	if _, ok := msg.(protocol.IdentityToken); !ok {
		t.Fatalf("first protocol message = %T, want IdentityToken", msg)
	}
	return client
}

func insertPlayerReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	id, name, err := parseGauntletPlayerArgs(args)
	if err != nil {
		return nil, err
	}
	_, err = ctx.DB.Insert(uint32(gauntletPlayersTableID), gauntletPlayerRow(id, name))
	return nil, err
}

func renamePlayerReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	id, name, err := parseGauntletPlayerArgs(args)
	if err != nil {
		return nil, err
	}
	rowID, _, ok := findGauntletPlayer(ctx, id)
	if !ok {
		return nil, fmt.Errorf("player %d not found", id)
	}
	_, err = ctx.DB.Update(uint32(gauntletPlayersTableID), rowID, gauntletPlayerRow(id, name))
	return nil, err
}

func deletePlayerReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	id, err := strconv.ParseUint(string(args), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse player id: %w", err)
	}
	rowID, _, ok := findGauntletPlayer(ctx, id)
	if !ok {
		return nil, fmt.Errorf("player %d not found", id)
	}
	return nil, ctx.DB.Delete(uint32(gauntletPlayersTableID), rowID)
}

func failAfterInsertReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	if _, err := insertPlayerReducer(ctx, args); err != nil {
		return nil, err
	}
	return nil, errors.New("intentional user failure after insert")
}

func panicAfterInsertReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	if _, err := insertPlayerReducer(ctx, args); err != nil {
		return nil, err
	}
	panic("intentional panic after insert")
}

func parseGauntletPlayerArgs(args []byte) (uint64, string, error) {
	idText, name, ok := strings.Cut(string(args), ":")
	if !ok || name == "" {
		return 0, "", fmt.Errorf("bad player args %q", args)
	}
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parse player id: %w", err)
	}
	return id, name, nil
}

func gauntletPlayerRow(id uint64, name string) types.ProductValue {
	return types.ProductValue{types.NewUint64(id), types.NewString(name)}
}

func findGauntletPlayer(ctx *schema.ReducerContext, id uint64) (types.RowID, types.ProductValue, bool) {
	for rowID, row := range ctx.DB.ScanTable(uint32(gauntletPlayersTableID)) {
		if row[0].AsUint64() == id {
			return rowID, row.Copy(), true
		}
	}
	return 0, nil, false
}

func assertGauntletProtocolQueriesMatchModel(t *testing.T, client *websocket.Conn, model gauntletModel, label string) {
	t.Helper()
	for _, query := range gauntletProtocolQueries(model) {
		got := queryGauntletProtocolPlayers(t, client, query.sql, []byte(query.id))
		if diff := diffGauntletPlayers(got, query.want); diff != "" {
			t.Fatalf("%s query %q protocol/model mismatch:\n%s", label, query.sql, diff)
		}
	}
}

func assertGauntletSubscribeInitialMatchesModel(t *testing.T, rt *shunter.Runtime, model gauntletModel, label string) {
	t.Helper()
	for _, query := range gauntletProtocolQueries(model) {
		client := dialGauntletProtocol(t, rt)
		got := subscribeGauntletProtocolPlayers(t, client, query.sql, gauntletRequestID(query.id), gauntletQueryID(query.id))
		if err := client.Close(websocket.StatusNormalClosure, ""); err != nil {
			t.Fatalf("%s query %q close protocol client: %v", label, query.sql, err)
		}
		if diff := diffGauntletPlayers(got, query.want); diff != "" {
			t.Fatalf("%s subscribe query %q initial snapshot/model mismatch:\n%s", label, query.sql, diff)
		}
	}
}

func assertGauntletSubscribeInitialMatchesOneOff(t *testing.T, rt *shunter.Runtime, model gauntletModel, label string) {
	t.Helper()
	oneOffClient := dialGauntletProtocol(t, rt)
	defer oneOffClient.Close(websocket.StatusNormalClosure, "")

	for _, query := range gauntletProtocolQueries(model) {
		oneOffRows := queryGauntletProtocolPlayers(t, oneOffClient, query.sql, []byte("oneoff-"+query.id))
		subClient := dialGauntletProtocol(t, rt)
		subRows := subscribeGauntletProtocolPlayers(t, subClient, query.sql, gauntletRequestID("sub-"+query.id), gauntletQueryID("sub-"+query.id))
		if err := subClient.Close(websocket.StatusNormalClosure, ""); err != nil {
			t.Fatalf("%s query %q close protocol client: %v", label, query.sql, err)
		}
		if diff := diffGauntletPlayers(subRows, oneOffRows); diff != "" {
			t.Fatalf("%s query %q subscribe initial/one-off mismatch:\n%s", label, query.sql, diff)
		}
		if diff := diffGauntletPlayers(oneOffRows, query.want); diff != "" {
			t.Fatalf("%s query %q one-off/model mismatch:\n%s", label, query.sql, diff)
		}
	}
}

func assertGauntletFailedReducerDoesNotFanout(t *testing.T, rt *shunter.Runtime, model gauntletModel, seed int64) {
	t.Helper()
	const (
		requestID = uint32(7101)
		queryID   = uint32(7102)
	)
	client := dialGauntletProtocol(t, rt)
	defer client.Close(websocket.StatusNormalClosure, "")

	initial := subscribeGauntletProtocolPlayers(t, client, "SELECT * FROM players", requestID, queryID)
	if diff := diffGauntletPlayers(initial, model.players); diff != "" {
		t.Fatalf("seed %d failed-reducer probe initial subscribe snapshot mismatch:\n%s", seed, diff)
	}

	op := failAfterInsertOp(nextUnusedGauntletPlayerID(model), "failed_probe")
	res, err := rt.CallReducer(context.Background(), op.reducer, []byte(op.args))
	if err != nil {
		t.Fatalf("seed %d failed-reducer probe %s admission error: %v", seed, op, err)
	}
	advanceGauntletModel(t, &model, op, gauntletReducerOutcomeFromResult(res), fmt.Sprintf("seed %d failed-reducer probe %s", seed, op))
	assertGauntletReadMatchesModel(t, rt, model, fmt.Sprintf("seed %d after failed-reducer probe", seed))
	assertNoGauntletProtocolMessageBeforeClose(t, client, 50*time.Millisecond, fmt.Sprintf("seed %d failed-reducer probe %s", seed, op))
}

func runRejectedReducerAdmissionGauntlet(t *testing.T, rt *shunter.Runtime, model *gauntletModel, nextID *uint64, makeOp func(uint64, string) gauntletOp, requestIDBase uint32, label string) {
	t.Helper()
	namePrefix := strings.ReplaceAll(strings.ToLower(label), " ", "_")

	runtimeSubscriber := dialGauntletProtocol(t, rt)
	runtimeInitial := subscribeGauntletProtocolPlayers(t, runtimeSubscriber, "SELECT * FROM players", requestIDBase, requestIDBase+1)
	if diff := diffGauntletPlayers(runtimeInitial, model.players); diff != "" {
		t.Fatalf("%s runtime subscriber initial snapshot mismatch:\n%s", label, diff)
	}

	runtimeRejected := makeOp(*nextID, "runtime_"+namePrefix)
	res, err := rt.CallReducer(context.Background(), runtimeRejected.reducer, []byte(runtimeRejected.args))
	if err != nil {
		t.Fatalf("%s admission error: %v", runtimeRejected, err)
	}
	advanceGauntletModel(t, model, runtimeRejected, gauntletReducerOutcomeFromResult(res), runtimeRejected.String())
	assertGauntletReadMatchesModel(t, rt, *model, "after runtime "+label)
	assertNoGauntletProtocolMessageBeforeClose(t, runtimeSubscriber, 50*time.Millisecond, "runtime "+label+" subscriber fanout")
	if err := runtimeSubscriber.Close(websocket.StatusNormalClosure, "runtime "+label+" complete"); err != nil {
		t.Fatalf("close runtime %s subscriber: %v", label, err)
	}

	afterRuntimeRejected := insertPlayerOp(nextID, "after_runtime_"+namePrefix)
	runGauntletTrace(t, rt, model, []gauntletOp{afterRuntimeRejected}, 0, "after runtime "+label)

	protocolSubscriber := dialGauntletProtocol(t, rt)
	protocolInitial := subscribeGauntletProtocolPlayers(t, protocolSubscriber, "SELECT * FROM players", requestIDBase+2, requestIDBase+3)
	if diff := diffGauntletPlayers(protocolInitial, model.players); diff != "" {
		t.Fatalf("%s protocol subscriber initial snapshot mismatch:\n%s", label, diff)
	}
	caller := dialGauntletProtocol(t, rt)
	defer caller.Close(websocket.StatusNormalClosure, "")

	protocolRejected := makeOp(*nextID, "protocol_"+namePrefix)
	protocolOutcome := callGauntletProtocolReducer(t, caller, protocolRejected, requestIDBase+4, "protocol "+label)
	if protocolOutcome.status != shunter.StatusFailedUser {
		t.Fatalf("protocol %s status = %v, want collapsed protocol failure %v", label, protocolOutcome.status, shunter.StatusFailedUser)
	}
	if protocolOutcome.err == "" {
		t.Fatalf("protocol %s error = empty", label)
	}
	assertGauntletReadMatchesModel(t, rt, *model, "after protocol "+label)
	assertNoGauntletProtocolMessageBeforeClose(t, protocolSubscriber, 50*time.Millisecond, "protocol "+label+" subscriber fanout")
	if err := protocolSubscriber.Close(websocket.StatusNormalClosure, "protocol "+label+" complete"); err != nil {
		t.Fatalf("close protocol %s subscriber: %v", label, err)
	}

	afterProtocolRejected := insertPlayerOp(nextID, "after_protocol_"+namePrefix)
	runGauntletTrace(t, rt, model, []gauntletOp{afterProtocolRejected}, 1, "after protocol "+label)
	assertGauntletReadMatchesModel(t, rt, *model, label+" final")
}

func nextUnusedGauntletPlayerID(model gauntletModel) uint64 {
	var maxID uint64
	for id := range model.players {
		if id > maxID {
			maxID = id
		}
	}
	return maxID + 1
}

type gauntletDelta struct {
	inserts map[uint64]string
	deletes map[uint64]string
}

func gauntletAllRowsDeltaForOp(t *testing.T, model gauntletModel, op gauntletOp) gauntletDelta {
	t.Helper()
	return gauntletDeltaForOpMatching(t, model, op, func(uint64, string) bool { return true })
}

func gauntletDeltaForOpMatching(t *testing.T, model gauntletModel, op gauntletOp, matches func(uint64, string) bool) gauntletDelta {
	t.Helper()
	before := filterGauntletPlayersMatching(model.players, matches)
	afterModel := gauntletModel{players: copyGauntletPlayers(model.players)}
	if op.wantStatus == shunter.StatusCommitted {
		op.apply(&afterModel)
	}
	after := filterGauntletPlayersMatching(afterModel.players, matches)
	return gauntletDeltaBetween(before, after)
}

func gauntletDeltaBetween(before, after map[uint64]string) gauntletDelta {
	delta := gauntletDelta{
		inserts: map[uint64]string{},
		deletes: map[uint64]string{},
	}
	for id, beforeName := range before {
		afterName, ok := after[id]
		if !ok || afterName != beforeName {
			delta.deletes[id] = beforeName
		}
	}
	for id, afterName := range after {
		beforeName, ok := before[id]
		if !ok || beforeName != afterName {
			delta.inserts[id] = afterName
		}
	}
	return delta
}

func gauntletDeltaIsEmpty(delta gauntletDelta) bool {
	return len(delta.inserts) == 0 && len(delta.deletes) == 0
}

type gauntletProtocolQuery struct {
	id   string
	sql  string
	want map[uint64]string
}

func gauntletProtocolQueries(model gauntletModel) []gauntletProtocolQuery {
	queries := []gauntletProtocolQuery{
		{
			id:   "all",
			sql:  "SELECT * FROM players",
			want: copyGauntletPlayers(model.players),
		},
	}
	if len(model.players) == 0 {
		return queries
	}

	id := firstGauntletPlayerID(model)
	name := model.players[id]
	queries = append(queries,
		gauntletProtocolQuery{
			id:   fmt.Sprintf("id-%d", id),
			sql:  fmt.Sprintf("SELECT * FROM players WHERE id = %d", id),
			want: map[uint64]string{id: name},
		},
		gauntletProtocolQuery{
			id:   "name-" + name,
			sql:  fmt.Sprintf("SELECT * FROM players WHERE name = '%s'", name),
			want: filterGauntletPlayersByName(model.players, name),
		},
	)
	return queries
}

func firstGauntletPlayerID(model gauntletModel) uint64 {
	ids := make([]uint64, 0, len(model.players))
	for id := range model.players {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids[0]
}

func copyGauntletPlayers(players map[uint64]string) map[uint64]string {
	copied := make(map[uint64]string, len(players))
	for id, name := range players {
		copied[id] = name
	}
	return copied
}

func filterGauntletPlayersByName(players map[uint64]string, name string) map[uint64]string {
	return filterGauntletPlayersMatching(players, func(_ uint64, playerName string) bool {
		return playerName == name
	})
}

func filterGauntletPlayersMatching(players map[uint64]string, matches func(uint64, string) bool) map[uint64]string {
	filtered := map[uint64]string{}
	for id, playerName := range players {
		if matches(id, playerName) {
			filtered[id] = playerName
		}
	}
	return filtered
}

func gauntletRequestID(id string) uint32 {
	return 1000 + gauntletStableID(id)%100000
}

func gauntletQueryID(id string) uint32 {
	return 2000 + gauntletStableID(id)%100000
}

func gauntletStableID(id string) uint32 {
	var n uint32
	for _, b := range []byte(id) {
		n = n*33 + uint32(b)
	}
	return n
}

func queryGauntletProtocolPlayers(t *testing.T, client *websocket.Conn, sql string, messageID []byte) map[uint64]string {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   messageID,
		QueryString: sql,
	}, "one-off query "+sql)

	resp := readGauntletOneOffQueryResponse(t, client, sql, messageID)
	if resp.Error != nil {
		t.Fatalf("one-off query %q error = %q, want success", sql, *resp.Error)
	}
	if len(resp.Tables) != 1 {
		t.Fatalf("one-off query %q returned %d tables, want 1", sql, len(resp.Tables))
	}
	if resp.Tables[0].TableName != "players" {
		t.Fatalf("one-off query %q table = %q, want players", sql, resp.Tables[0].TableName)
	}

	return decodeGauntletProtocolRows(t, resp.Tables[0].Rows, "one-off query "+sql)
}

func queryGauntletProtocolExpectError(t *testing.T, client *websocket.Conn, sql string, messageID []byte) protocol.OneOffQueryResponse {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   messageID,
		QueryString: sql,
	}, "one-off query "+sql)

	resp := readGauntletOneOffQueryResponse(t, client, sql, messageID)
	if resp.Error == nil {
		t.Fatalf("one-off query %q error = nil, want error", sql)
	}
	return resp
}

func readGauntletOneOffQueryResponse(t *testing.T, client *websocket.Conn, sql string, messageID []byte) protocol.OneOffQueryResponse {
	t.Helper()
	_, msg := readGauntletProtocolMessage(t, client, "one-off query "+sql)
	resp, ok := msg.(protocol.OneOffQueryResponse)
	if !ok {
		t.Fatalf("one-off query %q response = %T, want OneOffQueryResponse", sql, msg)
	}
	if !bytes.Equal(resp.MessageID, messageID) {
		t.Fatalf("one-off query %q message ID = %q, want %q", sql, resp.MessageID, messageID)
	}
	return resp
}

func subscribeGauntletProtocolPlayers(t *testing.T, client *websocket.Conn, sql string, requestID, queryID uint32) map[uint64]string {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeSingleMsg{
		RequestID:   requestID,
		QueryID:     queryID,
		QueryString: sql,
	}, "subscribe query "+sql)

	tag, msg := readGauntletProtocolMessage(t, client, "subscribe query "+sql)
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("subscribe query %q error = %q, want success", sql, subErr.Error)
	}
	applied, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok {
		t.Fatalf("subscribe query %q response = %T, want SubscribeSingleApplied", sql, msg)
	}
	if applied.RequestID != requestID {
		t.Fatalf("subscribe query %q request ID = %d, want %d", sql, applied.RequestID, requestID)
	}
	if applied.QueryID != queryID {
		t.Fatalf("subscribe query %q query ID = %d, want %d", sql, applied.QueryID, queryID)
	}
	if applied.TableName != "players" {
		t.Fatalf("subscribe query %q table = %q, want players", sql, applied.TableName)
	}
	return decodeGauntletProtocolRows(t, applied.Rows, "subscribe query "+sql)
}

func subscribeGauntletProtocolExpectError(t *testing.T, client *websocket.Conn, sql string, requestID, queryID uint32) protocol.SubscriptionError {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeSingleMsg{
		RequestID:   requestID,
		QueryID:     queryID,
		QueryString: sql,
	}, "rejected subscribe query "+sql)

	tag, msg := readGauntletProtocolMessage(t, client, "rejected subscribe query "+sql)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("rejected subscribe query %q tag = %d, want SubscriptionError", sql, tag)
	}
	subErr, ok := msg.(protocol.SubscriptionError)
	if !ok {
		t.Fatalf("rejected subscribe query %q response = %T, want SubscriptionError", sql, msg)
	}
	if subErr.RequestID == nil || *subErr.RequestID != requestID {
		t.Fatalf("rejected subscribe query %q request ID = %v, want %d", sql, subErr.RequestID, requestID)
	}
	if subErr.QueryID == nil || *subErr.QueryID != queryID {
		t.Fatalf("rejected subscribe query %q query ID = %v, want %d", sql, subErr.QueryID, queryID)
	}
	return subErr
}

func subscribeMultiGauntletProtocolPlayers(t *testing.T, client *websocket.Conn, sql []string, requestID, queryID uint32) gauntletDelta {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeMultiMsg{
		RequestID:    requestID,
		QueryID:      queryID,
		QueryStrings: sql,
	}, "subscribe multi query")

	tag, msg := readGauntletProtocolMessage(t, client, "subscribe multi query")
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("subscribe multi query error = %q, want success", subErr.Error)
	}
	applied, ok := msg.(protocol.SubscribeMultiApplied)
	if !ok {
		t.Fatalf("subscribe multi query response = %T, want SubscribeMultiApplied", msg)
	}
	if applied.RequestID != requestID {
		t.Fatalf("subscribe multi query request ID = %d, want %d", applied.RequestID, requestID)
	}
	if applied.QueryID != queryID {
		t.Fatalf("subscribe multi query query ID = %d, want %d", applied.QueryID, queryID)
	}
	return decodeGauntletSubscriptionUpdates(t, applied.Update, queryID, "subscribe multi query")
}

func subscribeMultiGauntletProtocolExpectError(t *testing.T, client *websocket.Conn, sql []string, requestID, queryID uint32) protocol.SubscriptionError {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeMultiMsg{
		RequestID:    requestID,
		QueryID:      queryID,
		QueryStrings: sql,
	}, "rejected subscribe multi query")

	tag, msg := readGauntletProtocolMessage(t, client, "rejected subscribe multi query")
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("rejected subscribe multi query tag = %d, want SubscriptionError", tag)
	}
	subErr, ok := msg.(protocol.SubscriptionError)
	if !ok {
		t.Fatalf("rejected subscribe multi query response = %T, want SubscriptionError", msg)
	}
	if subErr.RequestID == nil || *subErr.RequestID != requestID {
		t.Fatalf("rejected subscribe multi query request ID = %v, want %d", subErr.RequestID, requestID)
	}
	if subErr.QueryID == nil || *subErr.QueryID != queryID {
		t.Fatalf("rejected subscribe multi query query ID = %v, want %d", subErr.QueryID, queryID)
	}
	return subErr
}

func unsubscribeGauntletProtocolPlayers(t *testing.T, client *websocket.Conn, requestID, queryID uint32) map[uint64]string {
	t.Helper()
	label := fmt.Sprintf("unsubscribe query %d", queryID)
	writeGauntletProtocolMessage(t, client, protocol.UnsubscribeSingleMsg{
		RequestID: requestID,
		QueryID:   queryID,
	}, label)

	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("%s error = %q, want success", label, subErr.Error)
	}
	applied, ok := msg.(protocol.UnsubscribeSingleApplied)
	if !ok {
		t.Fatalf("%s response = %T, want UnsubscribeSingleApplied", label, msg)
	}
	if applied.RequestID != requestID {
		t.Fatalf("%s request ID = %d, want %d", label, applied.RequestID, requestID)
	}
	if applied.QueryID != queryID {
		t.Fatalf("%s query ID = %d, want %d", label, applied.QueryID, queryID)
	}
	if !applied.HasRows {
		return map[uint64]string{}
	}
	return decodeGauntletProtocolRows(t, applied.Rows, label)
}

func unsubscribeGauntletProtocolExpectError(t *testing.T, client *websocket.Conn, requestID, queryID uint32) protocol.SubscriptionError {
	t.Helper()
	label := fmt.Sprintf("rejected unsubscribe query %d", queryID)
	writeGauntletProtocolMessage(t, client, protocol.UnsubscribeSingleMsg{
		RequestID: requestID,
		QueryID:   queryID,
	}, label)

	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("%s tag = %d, want SubscriptionError", label, tag)
	}
	subErr, ok := msg.(protocol.SubscriptionError)
	if !ok {
		t.Fatalf("%s response = %T, want SubscriptionError", label, msg)
	}
	if subErr.RequestID == nil || *subErr.RequestID != requestID {
		t.Fatalf("%s request ID = %v, want %d", label, subErr.RequestID, requestID)
	}
	if subErr.QueryID == nil || *subErr.QueryID != queryID {
		t.Fatalf("%s query ID = %v, want %d", label, subErr.QueryID, queryID)
	}
	return subErr
}

func unsubscribeMultiGauntletProtocolPlayers(t *testing.T, client *websocket.Conn, requestID, queryID uint32) gauntletDelta {
	t.Helper()
	label := fmt.Sprintf("unsubscribe multi query %d", queryID)
	writeGauntletProtocolMessage(t, client, protocol.UnsubscribeMultiMsg{
		RequestID: requestID,
		QueryID:   queryID,
	}, label)

	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("%s error = %q, want success", label, subErr.Error)
	}
	applied, ok := msg.(protocol.UnsubscribeMultiApplied)
	if !ok {
		t.Fatalf("%s response = %T, want UnsubscribeMultiApplied", label, msg)
	}
	if applied.RequestID != requestID {
		t.Fatalf("%s request ID = %d, want %d", label, applied.RequestID, requestID)
	}
	if applied.QueryID != queryID {
		t.Fatalf("%s query ID = %d, want %d", label, applied.QueryID, queryID)
	}
	return decodeGauntletSubscriptionUpdates(t, applied.Update, queryID, label)
}

func unsubscribeMultiGauntletProtocolExpectError(t *testing.T, client *websocket.Conn, requestID, queryID uint32) protocol.SubscriptionError {
	t.Helper()
	label := fmt.Sprintf("rejected unsubscribe multi query %d", queryID)
	writeGauntletProtocolMessage(t, client, protocol.UnsubscribeMultiMsg{
		RequestID: requestID,
		QueryID:   queryID,
	}, label)

	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag != protocol.TagSubscriptionError {
		t.Fatalf("%s tag = %d, want SubscriptionError", label, tag)
	}
	subErr, ok := msg.(protocol.SubscriptionError)
	if !ok {
		t.Fatalf("%s response = %T, want SubscriptionError", label, msg)
	}
	if subErr.RequestID == nil || *subErr.RequestID != requestID {
		t.Fatalf("%s request ID = %v, want %d", label, subErr.RequestID, requestID)
	}
	if subErr.QueryID == nil || *subErr.QueryID != queryID {
		t.Fatalf("%s query ID = %v, want %d", label, subErr.QueryID, queryID)
	}
	return subErr
}

func callGauntletProtocolReducer(t *testing.T, client *websocket.Conn, op gauntletOp, requestID uint32, label string) gauntletReducerOutcome {
	t.Helper()
	return callGauntletProtocolReducerWithFlags(t, client, op, requestID, protocol.CallReducerFlagsFullUpdate, label)
}

func callGauntletProtocolReducerWithFlags(t *testing.T, client *websocket.Conn, op gauntletOp, requestID uint32, flags byte, label string) gauntletReducerOutcome {
	t.Helper()
	update := callGauntletProtocolReducerUpdateWithFlags(t, client, op, requestID, flags, label)
	return gauntletReducerOutcomeFromProtocolUpdate(t, update, label)
}

func callGauntletProtocolReducerUpdateWithFlags(t *testing.T, client *websocket.Conn, op gauntletOp, requestID uint32, flags byte, label string) protocol.TransactionUpdate {
	t.Helper()
	writeGauntletProtocolReducerCall(t, client, op, requestID, flags, label)

	tag, msg := readGauntletProtocolMessage(t, client, "call reducer "+label)
	if tag != protocol.TagTransactionUpdate {
		t.Fatalf("%s tag = %d, want TransactionUpdate", label, tag)
	}
	update, ok := msg.(protocol.TransactionUpdate)
	if !ok {
		t.Fatalf("%s response = %T, want TransactionUpdate", label, msg)
	}
	assertGauntletReducerCallInfo(t, update.ReducerCall, op, requestID, label)
	return update
}

func gauntletReducerOutcomeFromProtocolUpdate(t *testing.T, update protocol.TransactionUpdate, label string) gauntletReducerOutcome {
	t.Helper()
	switch status := update.Status.(type) {
	case protocol.StatusCommitted:
		return gauntletReducerOutcome{status: shunter.StatusCommitted}
	case protocol.StatusFailed:
		return gauntletReducerOutcome{status: shunter.StatusFailedUser, err: status.Error}
	default:
		t.Fatalf("%s status = %T, want StatusCommitted or StatusFailed", label, update.Status)
		return gauntletReducerOutcome{}
	}
}

func writeGauntletProtocolReducerCall(t *testing.T, client *websocket.Conn, op gauntletOp, requestID uint32, flags byte, label string) {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.CallReducerMsg{
		ReducerName: op.reducer,
		Args:        []byte(op.args),
		RequestID:   requestID,
		Flags:       flags,
	}, "call reducer "+label)
}

func assertGauntletReducerCallInfo(t *testing.T, got protocol.ReducerCallInfo, op gauntletOp, requestID uint32, label string) {
	t.Helper()
	if got.RequestID != requestID {
		t.Fatalf("%s request ID = %d, want %d", label, got.RequestID, requestID)
	}
	if got.ReducerName != op.reducer {
		t.Fatalf("%s reducer name = %q, want %q", label, got.ReducerName, op.reducer)
	}
	if !bytes.Equal(got.Args, []byte(op.args)) {
		t.Fatalf("%s reducer args = %q, want %q", label, got.Args, op.args)
	}
}

func writeGauntletProtocolMessage(t *testing.T, client *websocket.Conn, msg any, label string) {
	t.Helper()
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		t.Fatalf("encode %s: %v", label, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write %s: %v", label, err)
	}
}

func readGauntletTransactionUpdateLight(t *testing.T, client *websocket.Conn, queryID uint32, label string) gauntletDelta {
	t.Helper()
	tag, msg := readGauntletProtocolMessage(t, client, "transaction update "+label)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("%s tag = %d, want TransactionUpdateLight", label, tag)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("%s response = %T, want TransactionUpdateLight", label, msg)
	}
	return decodeGauntletSubscriptionUpdates(t, update.Update, queryID, label)
}

func readGauntletTransactionUpdateLightByQuery(t *testing.T, client *websocket.Conn, label string) map[uint32]gauntletDelta {
	t.Helper()
	tag, msg := readGauntletProtocolMessage(t, client, "transaction update "+label)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("%s tag = %d, want TransactionUpdateLight", label, tag)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("%s response = %T, want TransactionUpdateLight", label, msg)
	}
	return decodeGauntletSubscriptionUpdatesByQuery(t, update.Update, label)
}

func assertGauntletDeltasByQueryEqual(t *testing.T, got, want map[uint32]gauntletDelta, label string) {
	t.Helper()
	for queryID, wantDelta := range want {
		gotDelta, ok := got[queryID]
		if !ok {
			t.Fatalf("%s missing query %d delta", label, queryID)
		}
		assertGauntletDeltaEqual(t, gotDelta, wantDelta, fmt.Sprintf("%s query %d", label, queryID))
	}
	for queryID := range got {
		if _, ok := want[queryID]; !ok {
			t.Fatalf("%s unexpected query %d delta", label, queryID)
		}
	}
}

func decodeGauntletSubscriptionUpdatesByQuery(t *testing.T, updates []protocol.SubscriptionUpdate, label string) map[uint32]gauntletDelta {
	t.Helper()
	got := map[uint32]gauntletDelta{}
	for i, entry := range updates {
		entryLabel := fmt.Sprintf("%s update %d", label, i)
		if entry.TableName != "players" {
			t.Fatalf("%s table = %q, want players", entryLabel, entry.TableName)
		}
		delta, ok := got[entry.QueryID]
		if !ok {
			delta = gauntletDelta{inserts: map[uint64]string{}, deletes: map[uint64]string{}}
		}
		mergeGauntletRows(t, delta.inserts, decodeGauntletProtocolRows(t, entry.Inserts, entryLabel+" inserts"), entryLabel+" inserts")
		mergeGauntletRows(t, delta.deletes, decodeGauntletProtocolRows(t, entry.Deletes, entryLabel+" deletes"), entryLabel+" deletes")
		got[entry.QueryID] = delta
	}
	return got
}

func assertNoGauntletProtocolMessageBeforeClose(t *testing.T, client *websocket.Conn, wait time.Duration, label string) {
	t.Helper()
	type readResult struct {
		messageType websocket.MessageType
		data        []byte
		err         error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		messageType, data, err := client.Read(context.Background())
		resultCh <- readResult{messageType: messageType, data: data, err: err}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			return
		}
		tag, msg, err := protocol.DecodeServerMessage(result.data)
		if err != nil {
			t.Fatalf("%s received unexpected undecodable protocol message type=%v: %v", label, result.messageType, err)
		}
		t.Fatalf("%s received unexpected protocol message tag=%d type=%T", label, tag, msg)
	case <-time.After(wait):
		return
	}
}

func assertGauntletDeltaEqual(t *testing.T, got, want gauntletDelta, label string) {
	t.Helper()
	if diff := diffGauntletPlayers(got.inserts, want.inserts); diff != "" {
		t.Fatalf("%s insert delta mismatch:\n%s", label, diff)
	}
	if diff := diffGauntletPlayers(got.deletes, want.deletes); diff != "" {
		t.Fatalf("%s delete delta mismatch:\n%s", label, diff)
	}
}

func decodeGauntletProtocolRows(t *testing.T, encoded []byte, label string) map[uint64]string {
	t.Helper()
	rawRows, err := protocol.DecodeRowList(encoded)
	if err != nil {
		t.Fatalf("decode %s RowList: %v", label, err)
	}
	got := map[uint64]string{}
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, gauntletPlayerTableSchema())
		if err != nil {
			t.Fatalf("decode %s row %d: %v", label, i, err)
		}
		id := row[0].AsUint64()
		if _, exists := got[id]; exists {
			t.Fatalf("%s returned duplicate player id %d", label, id)
		}
		got[id] = row[1].AsString()
	}
	return got
}

func decodeGauntletSubscriptionUpdates(t *testing.T, updates []protocol.SubscriptionUpdate, queryID uint32, label string) gauntletDelta {
	t.Helper()
	delta := gauntletDelta{
		inserts: map[uint64]string{},
		deletes: map[uint64]string{},
	}
	for i, entry := range updates {
		entryLabel := fmt.Sprintf("%s update %d", label, i)
		if entry.QueryID != queryID {
			t.Fatalf("%s query ID = %d, want %d", entryLabel, entry.QueryID, queryID)
		}
		if entry.TableName != "players" {
			t.Fatalf("%s table = %q, want players", entryLabel, entry.TableName)
		}
		mergeGauntletRows(t, delta.inserts, decodeGauntletProtocolRows(t, entry.Inserts, entryLabel+" inserts"), entryLabel+" inserts")
		mergeGauntletRows(t, delta.deletes, decodeGauntletProtocolRows(t, entry.Deletes, entryLabel+" deletes"), entryLabel+" deletes")
	}
	return delta
}

func mergeGauntletRows(t *testing.T, dst, src map[uint64]string, label string) {
	t.Helper()
	for id, name := range src {
		if existing, exists := dst[id]; exists && existing != name {
			t.Fatalf("%s has conflicting row id %d: %q and %q", label, id, existing, name)
		}
		dst[id] = name
	}
}

func assertGauntletReadMatchesModel(t *testing.T, rt *shunter.Runtime, model gauntletModel, label string) {
	t.Helper()
	got := readGauntletPlayers(t, rt, label)
	if diff := diffGauntletPlayers(got, model.players); diff != "" {
		t.Fatalf("%s: runtime/model mismatch:\n%s", label, diff)
	}
}

func readGauntletPlayers(t *testing.T, rt *shunter.Runtime, label string) map[uint64]string {
	t.Helper()
	got := map[uint64]string{}
	err := rt.Read(context.Background(), func(view shunter.LocalReadView) error {
		rowCount := view.RowCount(gauntletPlayersTableID)
		for _, row := range view.TableScan(gauntletPlayersTableID) {
			id := row[0].AsUint64()
			name := row[1].AsString()
			if _, exists := got[id]; exists {
				return fmt.Errorf("duplicate player id %d", id)
			}
			got[id] = name
		}
		if rowCount != len(got) {
			return fmt.Errorf("row count = %d, scanned %d", rowCount, len(got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s: Read returned error: %v", label, err)
	}
	return got
}

func diffGauntletPlayers(got, want map[uint64]string) string {
	ids := make(map[uint64]struct{}, len(got)+len(want))
	for id := range got {
		ids[id] = struct{}{}
	}
	for id := range want {
		ids[id] = struct{}{}
	}
	sorted := make([]uint64, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var b strings.Builder
	for _, id := range sorted {
		g, gok := got[id]
		w, wok := want[id]
		switch {
		case !gok:
			fmt.Fprintf(&b, "- missing runtime id %d want %q\n", id, w)
		case !wok:
			fmt.Fprintf(&b, "- unexpected runtime id %d got %q\n", id, g)
		case g != w:
			fmt.Fprintf(&b, "- id %d got %q want %q\n", id, g, w)
		}
	}
	return b.String()
}

func readGauntletProtocolMessage(t *testing.T, client *websocket.Conn, label string) (uint8, any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	messageType, data, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("%s read: %v", label, err)
	}
	if messageType != websocket.MessageBinary {
		t.Fatalf("%s message type = %v, want MessageBinary", label, messageType)
	}
	tag, msg, err := protocol.DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("%s DecodeServerMessage: %v", label, err)
	}
	return tag, msg
}

func assertGauntletProtocolClosed(t *testing.T, client *websocket.Conn, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := client.Read(ctx)
	if err == nil {
		t.Fatalf("%s read succeeded, want closed protocol connection", label)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s protocol connection did not close before timeout", label)
	}
}

func gauntletPlayerTableSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   gauntletPlayersTableID,
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
			{Index: 1, Name: "name", Type: schema.KindString},
		},
	}
}
