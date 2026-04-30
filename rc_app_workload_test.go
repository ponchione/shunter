package shunter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"testing"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	rcExampleAppSeed         = uint64(0x7e57a991)
	rcExampleAppRuntimeLabel = "app=taskboard/auth=strict/table=tasks"
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

	assertRCExampleAllTasks(t, rt, model, 4, "before-restart")
	assertRCExampleOpenTasksQueryAndView(t, rt, model, 5, "before-restart")

	if err := rt.Close(); err != nil {
		t.Fatalf("seed=%#x op=6 runtime_config=%s operation=Close(before-restart) observed_error=%v expected=nil",
			rcExampleAppSeed, rcExampleAppRuntimeLabel, err)
	}

	rt = buildRCExampleAppRuntime(t, dataDir)
	defer rt.Close()
	assertRCExampleAllTasks(t, rt, model, 7, "after-restart")

	callRCExampleReducer(t, rt, 8, "complete_task", rcExampleCompleteTaskArgs{ID: 2})
	model[2] = rcExampleTask{Owner: "bob", Title: "review release", Done: true}
	assertRCExampleAllTasks(t, rt, model, 9, "after-restart-complete")
	assertRCExampleOpenTasksQueryAndView(t, rt, model, 10, "after-restart-complete")
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
		TableDef(schema.TableDefinition{
			Name: "tasks",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true},
				{Name: "owner", Type: types.KindString},
				{Name: "title", Type: types.KindString},
				{Name: "done", Type: types.KindBool},
			},
		}, schema.WithPrivateRead()).
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
		AuthMode:       shunter.AuthModeStrict,
		AuthSigningKey: []byte("release-candidate-example-app-secret"),
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
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=Marshal(%s) observed_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := rt.CallReducer(ctx, reducerName, body,
		shunter.WithPermissions("tasks:write"),
		shunter.WithRequestID(uint32(1000+op)),
	)
	if err != nil {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallReducer(%s) observed_admission_error=%v expected=nil",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, err)
	}
	if res.Status != shunter.StatusCommitted {
		t.Fatalf("seed=%#x op=%d runtime_config=%s operation=CallReducer(%s) observed_status=%v observed_error=%v expected_status=%v",
			rcExampleAppSeed, op, rcExampleAppRuntimeLabel, reducerName, res.Status, res.Error, shunter.StatusCommitted)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query, err := rt.CallQuery(ctx, "open_tasks",
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
