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

			for step, op := range trace {
				label := fmt.Sprintf("seed %d protocol call step %d %s", seed, step, op)
				outcome := callGauntletProtocolReducer(t, client, op, uint32(7300+step), label)
				advanceGauntletModel(t, &model, op, outcome, label)
				assertGauntletReadMatchesModel(t, rt, model, label)
			}
		})
	}
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
		Reducer("fail_after_insert", failAfterInsertReducer)

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

	_, msg := readGauntletProtocolMessage(t, client, "one-off query "+sql)
	resp, ok := msg.(protocol.OneOffQueryResponse)
	if !ok {
		t.Fatalf("one-off query %q response = %T, want OneOffQueryResponse", sql, msg)
	}
	if !bytes.Equal(resp.MessageID, messageID) {
		t.Fatalf("one-off query %q message ID = %q, want %q", sql, resp.MessageID, messageID)
	}
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

func callGauntletProtocolReducer(t *testing.T, client *websocket.Conn, op gauntletOp, requestID uint32, label string) gauntletReducerOutcome {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.CallReducerMsg{
		ReducerName: op.reducer,
		Args:        []byte(op.args),
		RequestID:   requestID,
		Flags:       protocol.CallReducerFlagsFullUpdate,
	}, "call reducer "+label)

	tag, msg := readGauntletProtocolMessage(t, client, "call reducer "+label)
	if tag != protocol.TagTransactionUpdate {
		t.Fatalf("%s tag = %d, want TransactionUpdate", label, tag)
	}
	update, ok := msg.(protocol.TransactionUpdate)
	if !ok {
		t.Fatalf("%s response = %T, want TransactionUpdate", label, msg)
	}
	if update.ReducerCall.RequestID != requestID {
		t.Fatalf("%s request ID = %d, want %d", label, update.ReducerCall.RequestID, requestID)
	}
	if update.ReducerCall.ReducerName != op.reducer {
		t.Fatalf("%s reducer name = %q, want %q", label, update.ReducerCall.ReducerName, op.reducer)
	}
	if !bytes.Equal(update.ReducerCall.Args, []byte(op.args)) {
		t.Fatalf("%s reducer args = %q, want %q", label, update.ReducerCall.Args, op.args)
	}

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
