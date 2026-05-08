package shunter_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
	"github.com/ponchione/websocket"
)

const (
	rcExampleAppSeed         = uint64(0x7e57a991)
	rcExampleAppRuntimeLabel = "app=taskboard/auth=strict/table=tasks"
	rcExampleAppSigningKey   = "release-candidate-example-app-secret"
	rcExampleAppTasksTableID = schema.TableID(0)
)

func TestReleaseCandidateExampleAppWorkloadPublicRuntime(t *testing.T) {
	dataDir := t.TempDir()
	rt := buildRCExampleAppRuntime(t, dataDir)
	model := map[uint64]rcExampleTask{}

	callRCExampleReducer(t, rt, 0, "create_task", rcExampleCreateTaskArgs{ID: 1, Owner: "alice", Title: "ship gauntlet"})
	model[1] = rcExampleTask{Owner: "alice", Title: "ship gauntlet"}
	callRCExampleReducer(t, rt, 1, "create_task", rcExampleCreateTaskArgs{ID: 2, Owner: "bob", Title: "review release"})
	model[2] = rcExampleTask{Owner: "bob", Title: "review release"}
	callRCExampleReducer(t, rt, 2, "complete_task", rcExampleCompleteTaskArgs{ID: 1})
	model[1] = rcExampleTask{Owner: "alice", Title: "ship gauntlet", Done: true}
	callRCExampleReducer(t, rt, 3, "create_task", rcExampleCreateTaskArgs{ID: 3, Owner: "alice", Title: "publish notes"})
	model[3] = rcExampleTask{Owner: "alice", Title: "publish notes"}

	callRCExampleReducerExpectUserFailure(t, rt, 4, "create_task", rcExampleCreateTaskArgs{ID: 3, Owner: "mallory", Title: "duplicate"})
	callRCExampleReducerExpectPermissionDenied(t, rt, 5, "create_task", rcExampleCreateTaskArgs{ID: 4, Owner: "mallory", Title: "denied"})
	assertRCExampleDeclaredReadsDenied(t, rt, 6, "before-restart")
	assertRCExampleAllTasks(t, rt, model, 7, "before-restart-after-rejections")
	assertRCExampleOpenTasksQueryAndView(t, rt, model, 8, "before-restart-after-rejections")

	if err := rt.Close(); err != nil {
		t.Fatalf("seed=%#x op=9 runtime_config=%s operation=Close(before-restart) observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}

	rt = buildRCExampleAppRuntime(t, dataDir)
	defer rt.Close()
	assertRCExampleAllTasks(t, rt, model, 10, "after-restart")
	assertRCExampleDeclaredReadsDenied(t, rt, 11, "after-restart")

	callRCExampleReducerExpectUserFailure(t, rt, 12, "create_task", rcExampleCreateTaskArgs{ID: 3, Owner: "mallory", Title: "duplicate after restart"})
	callRCExampleReducerExpectPermissionDenied(t, rt, 13, "create_task", rcExampleCreateTaskArgs{ID: 4, Owner: "mallory", Title: "denied after restart"})
	assertRCExampleAllTasks(t, rt, model, 14, "after-restart-after-rejections")
	assertRCExampleOpenTasksQueryAndView(t, rt, model, 15, "after-restart-after-rejections")

	callRCExampleReducer(t, rt, 16, "complete_task", rcExampleCompleteTaskArgs{ID: 2})
	model[2] = rcExampleTask{Owner: "bob", Title: "review release", Done: true}
	assertRCExampleAllTasks(t, rt, model, 17, "after-restart-complete")
	assertRCExampleOpenTasksQueryAndView(t, rt, model, 18, "after-restart-complete")
}

func TestReleaseCandidateExampleAppProtocolWorkloadStrictAuth(t *testing.T) {
	dataDir := t.TempDir()
	rt := buildRCExampleAppRuntime(t, dataDir)
	model := map[uint64]rcExampleTask{}

	srv := httptest.NewServer(rt.HTTPHandler())
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
	caller := dialRCExampleProtocol(t, url, "protocol op 0 caller dial", "tasks:write", "tasks:read")
	subscriber := dialRCExampleProtocol(t, url, "protocol op 0 subscriber dial", "tasks:read")

	callRCExampleProtocolReducer(t, caller, 1, "create_task", rcExampleCreateTaskArgs{ID: 1, Owner: "alice", Title: "protocol ship"}, true)
	model[1] = rcExampleTask{Owner: "alice", Title: "protocol ship"}
	callRCExampleProtocolReducer(t, caller, 2, "create_task", rcExampleCreateTaskArgs{ID: 2, Owner: "bob", Title: "protocol review"}, true)
	model[2] = rcExampleTask{Owner: "bob", Title: "protocol review"}
	assertRCExampleAllTasks(t, rt, model, 3, "protocol before-restart commits")

	queryRows := queryRCExampleProtocolOpenTasks(t, caller, []byte("rc-protocol-open-before"), "protocol op 4 declared query before restart")
	if !maps.Equal(queryRows, openRCExampleTasks(model)) {
		t.Fatalf("seed=%#x op=4 runtime_config=%s operation=DeclaredQuery(protocol-before-restart) observed_rows=%v expected_rows=%v",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, queryRows, openRCExampleTasks(model))
	}
	const subscriberQueryID = uint32(7105)
	initialRows := subscribeRCExampleProtocolOpenTasks(t, subscriber, 7104, subscriberQueryID, "protocol op 5 declared view before restart")
	if !maps.Equal(initialRows, openRCExampleTasks(model)) {
		t.Fatalf("seed=%#x op=5 runtime_config=%s operation=SubscribeDeclaredView(protocol-before-restart) observed_rows=%v expected_rows=%v",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, initialRows, openRCExampleTasks(model))
	}

	callRCExampleProtocolReducer(t, caller, 6, "complete_task", rcExampleCompleteTaskArgs{ID: 1}, true)
	deleteDelta := readRCExampleProtocolTaskDelta(t, subscriber, subscriberQueryID, "protocol op 6 complete subscribed task")
	if len(deleteDelta.inserts) != 0 || !maps.Equal(deleteDelta.deletes, map[uint64]rcExampleTask{1: {Owner: "alice", Title: "protocol ship"}}) {
		t.Fatalf("seed=%#x op=6 runtime_config=%s operation=ReadDeclaredViewDelta(complete_task) observed_delta=%+v expected_deletes=task-1",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, deleteDelta)
	}
	model[1] = rcExampleTask{Owner: "alice", Title: "protocol ship", Done: true}

	callRCExampleProtocolReducer(t, caller, 7, "create_task", rcExampleCreateTaskArgs{ID: 2, Owner: "mallory", Title: "duplicate"}, false)
	assertNoGauntletProtocolMessageBeforeClose(t, subscriber, 50*time.Millisecond, "protocol op 7 duplicate create has no fanout")
	assertRCExampleAllTasks(t, rt, model, 8, "protocol after duplicate rejection")

	if err := caller.Close(websocket.StatusNormalClosure, "protocol before-restart caller complete"); err != nil {
		t.Fatalf("seed=%#x op=9 runtime_config=%s operation=CloseProtocolCaller observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}
	if err := subscriber.Close(websocket.StatusNormalClosure, "protocol before-restart subscriber complete"); err != nil {
		t.Fatalf("seed=%#x op=10 runtime_config=%s operation=CloseProtocolSubscriber observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}
	srv.Close()
	if err := rt.Close(); err != nil {
		t.Fatalf("seed=%#x op=11 runtime_config=%s operation=CloseRuntime(protocol-before-restart) observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}

	rt = buildRCExampleAppRuntime(t, dataDir)
	defer rt.Close()
	assertRCExampleAllTasks(t, rt, model, 12, "protocol after restart")
	restartedSrv := httptest.NewServer(rt.HTTPHandler())
	defer restartedSrv.Close()
	restartedURL := strings.Replace(restartedSrv.URL, "http://", "ws://", 1) + "/subscribe"
	restartedCaller := dialRCExampleProtocol(t, restartedURL, "protocol op 13 restarted caller dial", "tasks:write", "tasks:read")
	defer restartedCaller.CloseNow()
	restartedSubscriber := dialRCExampleProtocol(t, restartedURL, "protocol op 13 restarted subscriber dial", "tasks:read")
	defer restartedSubscriber.CloseNow()

	restartedRows := queryRCExampleProtocolOpenTasks(t, restartedCaller, []byte("rc-protocol-open-after"), "protocol op 14 declared query after restart")
	if !maps.Equal(restartedRows, openRCExampleTasks(model)) {
		t.Fatalf("seed=%#x op=14 runtime_config=%s operation=DeclaredQuery(protocol-after-restart) observed_rows=%v expected_rows=%v",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, restartedRows, openRCExampleTasks(model))
	}
	const restartedQueryID = uint32(7116)
	restartedInitial := subscribeRCExampleProtocolOpenTasks(t, restartedSubscriber, 7115, restartedQueryID, "protocol op 15 declared view after restart")
	if !maps.Equal(restartedInitial, openRCExampleTasks(model)) {
		t.Fatalf("seed=%#x op=15 runtime_config=%s operation=SubscribeDeclaredView(protocol-after-restart) observed_rows=%v expected_rows=%v",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, restartedInitial, openRCExampleTasks(model))
	}

	callRCExampleProtocolReducer(t, restartedCaller, 16, "create_task", rcExampleCreateTaskArgs{ID: 2, Owner: "mallory", Title: "duplicate after restart"}, false)
	assertRCExampleAllTasks(t, rt, model, 17, "protocol after-restart duplicate rejection")

	callRCExampleProtocolReducer(t, restartedCaller, 18, "create_task", rcExampleCreateTaskArgs{ID: 3, Owner: "alice", Title: "protocol after restart"}, true)
	insertDelta := readRCExampleProtocolTaskDelta(t, restartedSubscriber, restartedQueryID, "protocol op 18 create after restart")
	if !maps.Equal(insertDelta.inserts, map[uint64]rcExampleTask{3: {Owner: "alice", Title: "protocol after restart"}}) || len(insertDelta.deletes) != 0 {
		t.Fatalf("seed=%#x op=18 runtime_config=%s operation=ReadDeclaredViewDelta(create_task) observed_delta=%+v expected_inserts=task-3",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, insertDelta)
	}
	model[3] = rcExampleTask{Owner: "alice", Title: "protocol after restart"}
	assertRCExampleAllTasks(t, rt, model, 19, "protocol final")
}

type rcExampleTask struct {
	Owner string
	Title string
	Done  bool
}

type rcExampleCreateTaskArgs struct {
	ID    uint64 `json:"id"`
	Owner string `json:"owner"`
	Title string `json:"title"`
}

type rcExampleCompleteTaskArgs struct {
	ID uint64 `json:"id"`
}

func buildRCExampleAppRuntime(t *testing.T, dataDir string) *shunter.Runtime {
	t.Helper()
	mod := shunter.NewModule("taskboard").
		SchemaVersion(1).
		TableDef(rcExampleTasksTableDef(), schema.WithPrivateRead()).
		Reducer("create_task", rcExampleCreateTaskReducer, shunter.WithReducerPermissions(shunter.PermissionMetadata{Required: []string{"tasks:write"}})).
		Reducer("complete_task", rcExampleCompleteTaskReducer, shunter.WithReducerPermissions(shunter.PermissionMetadata{Required: []string{"tasks:write"}})).
		Query(shunter.QueryDeclaration{
			Name:        "open_tasks",
			SQL:         "SELECT * FROM tasks WHERE done = false",
			Permissions: shunter.PermissionMetadata{Required: []string{"tasks:read"}},
		}).
		View(shunter.ViewDeclaration{
			Name:        "open_tasks_live",
			SQL:         "SELECT * FROM tasks WHERE done = false",
			Permissions: shunter.PermissionMetadata{Required: []string{"tasks:read"}},
		})

	rt, err := shunter.Build(mod, shunter.Config{
		DataDir:        dataDir,
		EnableProtocol: true,
		AuthMode:       shunter.AuthModeStrict,
		AuthSigningKey: []byte(rcExampleAppSigningKey),
	})
	if err != nil {
		t.Fatalf("seed=%#x op=build runtime_config=%s operation=Build observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("seed=%#x op=start runtime_config=%s operation=Start observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}
	return rt
}

func rcExampleTasksTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "tasks",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "owner", Type: types.KindString},
			{Name: "title", Type: types.KindString},
			{Name: "done", Type: types.KindBool},
		},
	}
}

func rcExampleTasksTableSchema() *schema.TableSchema {
	def := rcExampleTasksTableDef()
	columns := make([]schema.ColumnSchema, len(def.Columns))
	for i, col := range def.Columns {
		columns[i] = schema.ColumnSchema{Index: i, Name: col.Name, Type: col.Type}
	}
	return &schema.TableSchema{
		ID:      rcExampleAppTasksTableID,
		Name:    def.Name,
		Columns: columns,
	}
}

func rcExampleCreateTaskReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	var input rcExampleCreateTaskArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	_, err := ctx.DB.Insert(uint32(rcExampleAppTasksTableID), types.ProductValue{
		types.NewUint64(input.ID),
		types.NewString(input.Owner),
		types.NewString(input.Title),
		types.NewBool(false),
	})
	return nil, err
}

func rcExampleCompleteTaskReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	var input rcExampleCompleteTaskArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	for rowID, row := range ctx.DB.ScanTable(uint32(rcExampleAppTasksTableID)) {
		if row[0].AsUint64() != input.ID {
			continue
		}
		_, err := ctx.DB.Update(uint32(rcExampleAppTasksTableID), rowID, types.ProductValue{
			types.NewUint64(input.ID),
			types.NewString(row[1].AsString()),
			types.NewString(row[2].AsString()),
			types.NewBool(true),
		})
		return nil, err
	}
	return nil, fmt.Errorf("task %d not found", input.ID)
}

func callRCExampleReducer(t *testing.T, rt *shunter.Runtime, op int, reducerName string, args any) {
	t.Helper()
	res := callRCExampleReducerResult(t, rt, op, reducerName, args, shunter.WithPermissions("tasks:write"))
	if res.Status != shunter.StatusCommitted {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallReducer(%s) observed_status=%v observed_error=%v expected_status=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, res.Status, res.Error, shunter.StatusCommitted)
	}
}

func callRCExampleReducerExpectUserFailure(t *testing.T, rt *shunter.Runtime, op int, reducerName string, args any) {
	t.Helper()
	res := callRCExampleReducerResult(t, rt, op, reducerName, args, shunter.WithPermissions("tasks:write"))
	if res.Status != shunter.StatusFailedUser || res.Error == nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallReducer(%s) observed_status=%v observed_error=%v expected_status=%v expected_error=non-nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, res.Status, res.Error, shunter.StatusFailedUser)
	}
}

func callRCExampleReducerExpectPermissionDenied(t *testing.T, rt *shunter.Runtime, op int, reducerName string, args any) {
	t.Helper()
	res := callRCExampleReducerResult(t, rt, op, reducerName, args)
	if res.Status != shunter.StatusFailedPermission || !errors.Is(res.Error, shunter.ErrPermissionDenied) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallReducer(%s) observed_status=%v observed_error=%v expected_status=%v expected_error=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, res.Status, res.Error, shunter.StatusFailedPermission, shunter.ErrPermissionDenied)
	}
}

func callRCExampleReducerResult(t *testing.T, rt *shunter.Runtime, op int, reducerName string, args any, opts ...shunter.ReducerCallOption) shunter.ReducerResult {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=Marshal(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	callOpts := append([]shunter.ReducerCallOption{shunter.WithRequestID(uint32(1000 + op))}, opts...)
	res, err := rt.CallReducer(ctx, reducerName, body, callOpts...)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallReducer(%s) observed_admission_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, err)
	}
	return res
}

func assertRCExampleDeclaredReadsDenied(t *testing.T, rt *shunter.Runtime, op int, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := rt.CallQuery(ctx, "open_tasks", shunter.WithDeclaredReadRequestID(uint32(2100+op))); !errors.Is(err, shunter.ErrPermissionDenied) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallQuery(%s) observed_error=%v expected_error=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err, shunter.ErrPermissionDenied)
	}
	if _, err := rt.SubscribeView(ctx, "open_tasks_live", uint32(3100+op), shunter.WithDeclaredReadRequestID(uint32(4100+op))); !errors.Is(err, shunter.ErrPermissionDenied) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=SubscribeView(%s) observed_error=%v expected_error=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err, shunter.ErrPermissionDenied)
	}
}

func dialRCExampleProtocol(t *testing.T, url string, label string, permissions ...string) *websocket.Conn {
	t.Helper()
	token := mintReadAuthGauntletToken(t, []byte(rcExampleAppSigningKey), "rc-taskboard", permissions...)
	client, _ := dialGauntletProtocolURLWithHeaders(t, url, gauntletBearerHeader(token), label)
	return client
}

func callRCExampleProtocolReducer(t *testing.T, client *websocket.Conn, op int, reducerName string, args any, wantCommitted bool) {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=MarshalProtocolReducer(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, err)
	}
	writeGauntletProtocolMessage(t, client, protocol.CallReducerMsg{
		ReducerName: reducerName,
		Args:        body,
		RequestID:   uint32(7000 + op),
		Flags:       protocol.CallReducerFlagsFullUpdate,
	}, fmt.Sprintf("rc protocol op %d call reducer %s", op, reducerName))

	tag, msg := readGauntletProtocolMessage(t, client, fmt.Sprintf("rc protocol op %d reducer response %s", op, reducerName))
	if tag != protocol.TagTransactionUpdate {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=ReadProtocolReducer(%s) observed_tag=%d expected_tag=%d",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, tag, protocol.TagTransactionUpdate)
	}
	update, ok := msg.(protocol.TransactionUpdate)
	if !ok {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=ReadProtocolReducer(%s) observed_type=%T expected_type=TransactionUpdate",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, msg)
	}
	if update.ReducerCall.RequestID != uint32(7000+op) || update.ReducerCall.ReducerName != reducerName || string(update.ReducerCall.Args) != string(body) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=ReducerCallInfo(%s) observed=%+v expected_request=%d expected_args=%s",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, update.ReducerCall, 7000+op, body)
	}
	switch status := update.Status.(type) {
	case protocol.StatusCommitted:
		if !wantCommitted {
			t.Fatalf("seed=%#x op=%d runtime_config=%s operation=ProtocolReducerStatus(%s) observed=committed expected=failed",
				rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName)
		}
	case protocol.StatusFailed:
		if wantCommitted || status.Error == "" {
			t.Fatalf("seed=%#x op=%d runtime_config=%s operation=ProtocolReducerStatus(%s) observed_failed_error=%q expected_committed=%v",
				rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, status.Error, wantCommitted)
		}
	default:
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=ProtocolReducerStatus(%s) observed_status=%T expected_committed=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, update.Status, wantCommitted)
	}
}

func queryRCExampleProtocolOpenTasks(t *testing.T, client *websocket.Conn, messageID []byte, label string) map[uint64]rcExampleTask {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: messageID,
		Name:      "open_tasks",
	}, label)
	resp := readGauntletOneOffQueryResponseWithLabel(t, client, messageID, label)
	if resp.Error != nil {
		t.Fatalf("seed=%#x runtime_config=%s operation=DeclaredQuery(%s) observed_error=%q expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != "tasks" {
		t.Fatalf("seed=%#x runtime_config=%s operation=DeclaredQuery(%s) observed_tables=%+v expected_single_table=tasks",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, resp.Tables)
	}
	return decodeRCExampleProtocolTasks(t, resp.Tables[0].Rows, label)
}

func subscribeRCExampleProtocolOpenTasks(t *testing.T, client *websocket.Conn, requestID, queryID uint32, label string) map[uint64]rcExampleTask {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: requestID,
		QueryID:   queryID,
		Name:      "open_tasks_live",
	}, label)
	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("seed=%#x runtime_config=%s operation=SubscribeDeclaredView(%s) observed_error=%q expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, subErr.Error)
	}
	applied, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok || applied.RequestID != requestID || applied.QueryID != queryID || applied.TableName != "tasks" {
		t.Fatalf("seed=%#x runtime_config=%s operation=SubscribeDeclaredView(%s) observed=(tag=%d msg=%+v) expected=request=%d query=%d table=tasks",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, tag, msg, requestID, queryID)
	}
	return decodeRCExampleProtocolTasks(t, applied.Rows, label)
}

type rcExampleProtocolDelta struct {
	inserts map[uint64]rcExampleTask
	deletes map[uint64]rcExampleTask
}

func readRCExampleProtocolTaskDelta(t *testing.T, client *websocket.Conn, queryID uint32, label string) rcExampleProtocolDelta {
	t.Helper()
	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("seed=%#x runtime_config=%s operation=ReadProtocolDelta(%s) observed_tag=%d expected_tag=%d",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, tag, protocol.TagTransactionUpdateLight)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("seed=%#x runtime_config=%s operation=ReadProtocolDelta(%s) observed_type=%T expected_type=TransactionUpdateLight",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, msg)
	}
	delta := rcExampleProtocolDelta{inserts: map[uint64]rcExampleTask{}, deletes: map[uint64]rcExampleTask{}}
	for i, entry := range update.Update {
		if entry.QueryID != queryID || entry.TableName != "tasks" {
			t.Fatalf("seed=%#x runtime_config=%s operation=ReadProtocolDelta(%s) observed_entry_%d=(query=%d table=%q) expected=(query=%d table=tasks)",
				rcExampleAppSeed, rcExampleAppRuntimeLabel, label, i, entry.QueryID, entry.TableName, queryID)
		}
		maps.Copy(delta.inserts, decodeRCExampleProtocolTasks(t, entry.Inserts, label+" inserts"))
		maps.Copy(delta.deletes, decodeRCExampleProtocolTasks(t, entry.Deletes, label+" deletes"))
	}
	return delta
}

func decodeRCExampleProtocolTasks(t *testing.T, encoded []byte, label string) map[uint64]rcExampleTask {
	t.Helper()
	rawRows, err := protocol.DecodeRowList(encoded)
	if err != nil {
		t.Fatalf("seed=%#x runtime_config=%s operation=DecodeRowList(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, err)
	}
	rows := make([]types.ProductValue, 0, len(rawRows))
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, rcExampleTasksTableSchema())
		if err != nil {
			t.Fatalf("seed=%#x runtime_config=%s operation=DecodeTaskRow(%s row=%d) observed_error=%v expected=nil",
				rcExampleAppSeed, rcExampleAppRuntimeLabel, label, i, err)
		}
		rows = append(rows, row)
	}
	got, err := rowsToRCExampleTasks(rows)
	if err != nil {
		t.Fatalf("seed=%#x runtime_config=%s operation=DecodeTaskRows(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, label, err)
	}
	return got
}

func assertRCExampleAllTasks(t *testing.T, rt *shunter.Runtime, want map[uint64]rcExampleTask, op int, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := rt.Read(ctx, func(view shunter.LocalReadView) error {
		if gotCount := view.RowCount(rcExampleAppTasksTableID); gotCount != len(want) {
			return fmt.Errorf("observed_count=%d expected_count=%d", gotCount, len(want))
		}
		got, err := collectRCExampleRows(view.TableScan(rcExampleAppTasksTableID))
		if err != nil {
			return err
		}
		if !maps.Equal(got, want) {
			return fmt.Errorf("observed_rows=%v expected_rows=%v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=Read(%s) observed_error=%v expected_rows=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err, want)
	}
}

func assertRCExampleOpenTasksQueryAndView(t *testing.T, rt *shunter.Runtime, model map[uint64]rcExampleTask, op int, label string) {
	t.Helper()
	want := openRCExampleTasks(model)
	connID := rcExampleDeclaredReadConnectionID(op)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query, err := rt.CallQuery(ctx, "open_tasks",
		shunter.WithDeclaredReadConnectionID(connID),
		shunter.WithDeclaredReadPermissions("tasks:read"),
		shunter.WithDeclaredReadRequestID(uint32(2000+op)),
	)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallQuery(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err)
	}
	if query.Name != "open_tasks" || query.TableName != "tasks" {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallQuery(%s) observed_identity=(%q,%q) expected=(open_tasks,tasks)",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, query.Name, query.TableName)
	}
	gotQuery, err := rowsToRCExampleTasks(query.Rows)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallQueryRows(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err)
	}
	if !maps.Equal(gotQuery, want) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallQuery(%s) observed_rows=%v expected_rows=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, gotQuery, want)
	}

	sub, err := rt.SubscribeView(ctx, "open_tasks_live", uint32(3000+op),
		shunter.WithDeclaredReadConnectionID(connID),
		shunter.WithDeclaredReadPermissions("tasks:read"),
		shunter.WithDeclaredReadRequestID(uint32(4000+op)),
	)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=SubscribeView(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err)
	}
	if sub.Name != "open_tasks_live" || sub.TableName != "tasks" || sub.QueryID != uint32(3000+op) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=SubscribeView(%s) observed_identity=(%q,%q,%d) expected=(open_tasks_live,tasks,%d)",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, sub.Name, sub.TableName, sub.QueryID, uint32(3000+op))
	}
	gotView, err := rowsToRCExampleTasks(sub.InitialRows)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=SubscribeViewRows(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, err)
	}
	if !maps.Equal(gotView, want) {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=SubscribeView(%s) observed_rows=%v expected_rows=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, label, gotView, want)
	}
}

func rcExampleDeclaredReadConnectionID(op int) types.ConnectionID {
	return types.ConnectionID{0x72, 0x63, byte(op >> 8), byte(op)}
}

func openRCExampleTasks(model map[uint64]rcExampleTask) map[uint64]rcExampleTask {
	open := map[uint64]rcExampleTask{}
	for id, task := range model {
		if !task.Done {
			open[id] = task
		}
	}
	return open
}

func collectRCExampleRows(rows iter.Seq2[types.RowID, types.ProductValue]) (map[uint64]rcExampleTask, error) {
	got := map[uint64]rcExampleTask{}
	for _, row := range rows {
		task, err := rcExampleTaskFromRow(row)
		if err != nil {
			return nil, err
		}
		id := row[0].AsUint64()
		if _, exists := got[id]; exists {
			return nil, fmt.Errorf("observed_duplicate_id=%d expected_unique_ids", id)
		}
		got[id] = task
	}
	return got, nil
}

func rowsToRCExampleTasks(rows []types.ProductValue) (map[uint64]rcExampleTask, error) {
	got := map[uint64]rcExampleTask{}
	for _, row := range rows {
		task, err := rcExampleTaskFromRow(row)
		if err != nil {
			return nil, err
		}
		id := row[0].AsUint64()
		if _, exists := got[id]; exists {
			return nil, fmt.Errorf("observed_duplicate_id=%d expected_unique_ids", id)
		}
		got[id] = task
	}
	return got, nil
}

func rcExampleTaskFromRow(row types.ProductValue) (rcExampleTask, error) {
	if len(row) != 4 {
		return rcExampleTask{}, fmt.Errorf("observed_row_width=%d expected=4", len(row))
	}
	if row[0].Kind() != types.KindUint64 || row[1].Kind() != types.KindString || row[2].Kind() != types.KindString || row[3].Kind() != types.KindBool {
		return rcExampleTask{}, fmt.Errorf("observed_row_kinds=(%s,%s,%s,%s) expected=(uint64,string,string,bool)",
			row[0].Kind(), row[1].Kind(), row[2].Kind(), row[3].Kind())
	}
	return rcExampleTask{
		Owner: row[1].AsString(),
		Title: row[2].AsString(),
		Done:  row[3].AsBool(),
	}, nil
}
