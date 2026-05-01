package shunter_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	shunter "github.com/ponchione/shunter"
)

const (
	runtimeCrashChildEnv   = "SHUNTER_RUNTIME_GAUNTLET_CRASH_CHILD"
	runtimeCrashDataDirEnv = "SHUNTER_RUNTIME_GAUNTLET_DATA_DIR"
)

func TestRuntimeGauntletUncleanProcessCrashDurableProtocolCommitRecovers(t *testing.T) {
	dataDir := t.TempDir()
	runRuntimeCrashChild(t, dataDir, "durable-protocol-commit")

	rt := buildGauntletRuntime(t, dataDir)
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{1: "crash_durable"}}
	assertGauntletReadMatchesModel(t, rt, model, "unclean durable crash after restart")
	assertRuntimeCrashCanCommitQueryAndFanout(t, rt, &model, 2, "unclean durable crash after restart")
}

func TestRuntimeGauntletUncleanProcessCrashAfterProtocolAckRecoversConsistentState(t *testing.T) {
	dataDir := t.TempDir()
	runRuntimeCrashChild(t, dataDir, "protocol-ack-before-confirmed-durable")

	rt := buildGauntletRuntime(t, dataDir)
	defer rt.Close()

	recovered := readGauntletPlayers(t, rt, "unclean ack crash after restart")
	model := gauntletModel{players: map[uint64]string{}}
	nextID := uint64(1)
	switch {
	case len(recovered) == 0:
		// SPEC-003 allows caller-visible success to beat durable fsync.
	case len(recovered) == 1 && recovered[1] == "crash_ack":
		model.players[1] = "crash_ack"
		nextID = 2
	default:
		t.Fatalf("unclean ack crash recovered rows = %v, want empty or durable committed row", recovered)
	}
	if diff := diffGauntletPlayers(recovered, model.players); diff != "" {
		t.Fatalf("unclean ack crash recovered/model mismatch:\n%s", diff)
	}
	assertRuntimeCrashCanCommitQueryAndFanout(t, rt, &model, nextID, "unclean ack crash after restart")
}

func TestRuntimeGauntletUncleanProcessCrashRecoversScheduledWakeup(t *testing.T) {
	dataDir := t.TempDir()
	runRuntimeCrashChild(t, dataDir, "scheduled-wakeup")

	rt := buildGauntletRuntime(t, dataDir)
	defer rt.Close()

	model := gauntletModel{players: map[uint64]string{1: "crash_schedule_barrier"}}
	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	subscriber := dialGauntletProtocol(t, rt)
	defer subscriber.CloseNow()

	const queryID = uint32(13322)
	initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 13321, queryID)
	if got, ok := initialRows[1]; !ok || got != "crash_schedule_barrier" {
		t.Fatalf("unclean scheduled crash initial rows = %v, want durable barrier row", initialRows)
	}
	if got, ok := initialRows[10]; ok {
		if got != "crash_scheduled" {
			t.Fatalf("unclean scheduled crash row 10 = %q, want crash_scheduled", got)
		}
		model.players[10] = "crash_scheduled"
	} else {
		wantDelta := gauntletDelta{
			inserts: map[uint64]string{10: "crash_scheduled"},
			deletes: map[uint64]string{},
		}
		gotDelta := readGauntletTransactionUpdateLight(t, subscriber, queryID, "unclean scheduled crash recovered fire")
		assertGauntletDeltaEqual(t, gotDelta, wantDelta, "unclean scheduled crash recovered fire")
		model.players[10] = "crash_scheduled"
	}
	assertGauntletReadMatchesModel(t, rt, model, "unclean scheduled crash final")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, model, "unclean scheduled crash final")
}

func TestRuntimeGauntletCrashSubprocessDriver(t *testing.T) {
	scenario := os.Getenv(runtimeCrashChildEnv)
	if scenario == "" {
		return
	}
	dataDir := os.Getenv(runtimeCrashDataDirEnv)
	if dataDir == "" {
		t.Fatalf("%s is empty", runtimeCrashDataDirEnv)
	}

	switch scenario {
	case "durable-protocol-commit":
		runRuntimeCrashDurableProtocolCommitChild(t, dataDir)
	case "protocol-ack-before-confirmed-durable":
		runRuntimeCrashProtocolAckChild(t, dataDir)
	case "scheduled-wakeup":
		runRuntimeCrashScheduledWakeupChild(t, dataDir)
	default:
		t.Fatalf("unknown runtime crash child scenario %q", scenario)
	}
	os.Exit(0)
}

func runRuntimeCrashChild(t *testing.T, dataDir, scenario string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeGauntletCrashSubprocessDriver$")
	cmd.Env = append(os.Environ(),
		runtimeCrashChildEnv+"="+scenario,
		runtimeCrashDataDirEnv+"="+dataDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime crash child %s failed: %v\n%s", scenario, err, out)
	}
}

func runRuntimeCrashDurableProtocolCommitChild(t *testing.T, dataDir string) {
	t.Helper()
	rt := buildGauntletRuntime(t, dataDir)
	url := runtimeCrashProtocolURL(t, rt)

	model := gauntletModel{players: map[uint64]string{}}
	subscriber := dialGauntletProtocolURL(t, url, "unclean durable child subscriber")
	caller := dialGauntletProtocolURL(t, url, "unclean durable child caller")
	const queryID = uint32(13102)
	initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 13101, queryID)
	if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
		t.Fatalf("unclean durable child initial rows mismatch:\n%s", diff)
	}

	nextID := uint64(1)
	op := insertPlayerOp(&nextID, "crash_durable")
	wantDelta := gauntletAllRowsDeltaForOp(t, model, op)
	outcome := callGauntletProtocolReducer(t, caller, op, 13103, "unclean durable child protocol commit")
	advanceGauntletModel(t, &model, op, outcome, "unclean durable child protocol commit")
	gotDelta := readGauntletTransactionUpdateLight(t, subscriber, queryID, "unclean durable child confirmed fanout")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, "unclean durable child confirmed fanout")
	os.Exit(0)
}

func runRuntimeCrashProtocolAckChild(t *testing.T, dataDir string) {
	t.Helper()
	rt := buildGauntletRuntime(t, dataDir)
	url := runtimeCrashProtocolURL(t, rt)
	caller := dialGauntletProtocolURL(t, url, "unclean ack child caller")

	nextID := uint64(1)
	op := insertPlayerOp(&nextID, "crash_ack")
	outcome := callGauntletProtocolReducer(t, caller, op, 13201, "unclean ack child protocol commit")
	if outcome.status != op.wantStatus {
		t.Fatalf("unclean ack child status = %v, want %v", outcome.status, op.wantStatus)
	}
	os.Exit(0)
}

func runRuntimeCrashScheduledWakeupChild(t *testing.T, dataDir string) {
	t.Helper()
	rt := buildGauntletRuntime(t, dataDir)
	url := runtimeCrashProtocolURL(t, rt)

	model := gauntletModel{players: map[uint64]string{}}
	subscriber := dialGauntletProtocolURL(t, url, "unclean scheduled child subscriber")
	caller := dialGauntletProtocolURL(t, url, "unclean scheduled child caller")
	const queryID = uint32(13302)
	initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", 13301, queryID)
	if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
		t.Fatalf("unclean scheduled child initial rows mismatch:\n%s", diff)
	}

	scheduleGauntletInsertNext(t, rt, 10, "crash_scheduled", 2*time.Second, "unclean scheduled child schedule future fire")
	nextID := uint64(1)
	barrier := insertPlayerOp(&nextID, "crash_schedule_barrier")
	wantDelta := gauntletAllRowsDeltaForOp(t, model, barrier)
	outcome := callGauntletProtocolReducer(t, caller, barrier, 13303, "unclean scheduled child durable barrier")
	advanceGauntletModel(t, &model, barrier, outcome, "unclean scheduled child durable barrier")
	gotDelta := readGauntletTransactionUpdateLight(t, subscriber, queryID, "unclean scheduled child barrier fanout")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, "unclean scheduled child barrier fanout")
	os.Exit(0)
}

func runtimeCrashProtocolURL(t *testing.T, rt interface{ HTTPHandler() http.Handler }) string {
	t.Helper()
	srv := httptest.NewServer(rt.HTTPHandler())
	t.Cleanup(srv.Close)
	return strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
}

func assertRuntimeCrashCanCommitQueryAndFanout(t *testing.T, rt *shunter.Runtime, model *gauntletModel, nextID uint64, label string) {
	t.Helper()
	queryClient := dialGauntletProtocol(t, rt)
	defer queryClient.Close(websocket.StatusNormalClosure, "")
	subscriber := dialGauntletProtocol(t, rt)
	defer subscriber.Close(websocket.StatusNormalClosure, "")

	queryID := uint32(13400 + nextID)
	initialRows := subscribeGauntletProtocolPlayers(t, subscriber, "SELECT * FROM players", queryID-1, queryID)
	if diff := diffGauntletPlayers(initialRows, model.players); diff != "" {
		t.Fatalf("%s initial subscription mismatch:\n%s", label, diff)
	}
	assertGauntletProtocolQueriesMatchModel(t, queryClient, *model, label+" initial query")

	id := nextID
	op := insertPlayerOp(&id, fmt.Sprintf("%s_followup", strings.ReplaceAll(label, " ", "_")))
	wantDelta := gauntletAllRowsDeltaForOp(t, *model, op)
	runGauntletTrace(t, rt, model, []gauntletOp{op}, int(nextID), label+" follow-up commit")
	gotDelta := readGauntletTransactionUpdateLight(t, subscriber, queryID, label+" follow-up fanout")
	assertGauntletDeltaEqual(t, gotDelta, wantDelta, label+" follow-up fanout")
	assertGauntletProtocolQueriesMatchModel(t, queryClient, *model, label+" final query")
}
