package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestExportSchemaIncludesTablesReducersAndLifecycle(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(5)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: KindString, Nullable: true},
		},
		Indexes: []IndexDefinition{{Name: "name_idx", Columns: []string{"name"}, Unique: true}},
	})
	b.Reducer("CreatePlayer", func(*ReducerContext, []byte) ([]byte, error) { return nil, nil })
	b.OnConnect(func(*ReducerContext) error { return nil })

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	export := e.ExportSchema()
	if export.Version != 5 {
		t.Fatalf("ExportSchema version = %d, want 5", export.Version)
	}
	if len(export.Tables) != 3 {
		t.Fatalf("ExportSchema tables = %d, want 3 (user + system tables)", len(export.Tables))
	}
	if export.Tables[0].Name != "players" {
		t.Fatalf("first exported table = %q, want players", export.Tables[0].Name)
	}
	if export.Tables[0].ID != 0 {
		t.Fatalf("players table id = %d, want 0", export.Tables[0].ID)
	}
	if export.Tables[0].Columns[0].Type != "uint64" || export.Tables[0].Columns[1].Type != "string" {
		t.Fatalf("column export types = %+v", export.Tables[0].Columns)
	}
	if export.Tables[0].Columns[0].Index != 0 || export.Tables[0].Columns[1].Index != 1 {
		t.Fatalf("column export indexes = %+v, want indexes 0 and 1", export.Tables[0].Columns)
	}
	if !export.Tables[0].Columns[0].AutoIncrement {
		t.Fatalf("id column auto_increment = false, want true")
	}
	if !export.Tables[0].Columns[1].Nullable {
		t.Fatalf("nullable column export = %+v, want nullable name", export.Tables[0].Columns[1])
	}
	if export.Tables[0].Indexes[0].Columns[0] != "id" {
		t.Fatalf("primary index column export = %v, want [id]", export.Tables[0].Indexes[0].Columns)
	}
	if export.Tables[0].Indexes[0].ID != 0 || !reflect.DeepEqual(export.Tables[0].Indexes[0].ColumnOrdinals, []int{0}) {
		t.Fatalf("primary index durable identity = %+v, want id 0 column_ordinals [0]", export.Tables[0].Indexes[0])
	}
	if !export.Tables[0].Indexes[0].Primary || !export.Tables[0].Indexes[0].Unique {
		t.Fatalf("primary index export flags = %+v", export.Tables[0].Indexes[0])
	}
	if export.Tables[0].Indexes[1].ID != 1 || !reflect.DeepEqual(export.Tables[0].Indexes[1].ColumnOrdinals, []int{1}) {
		t.Fatalf("secondary index durable identity = %+v, want id 1 column_ordinals [1]", export.Tables[0].Indexes[1])
	}
	if export.Reducers[0] != (ReducerExport{Name: "CreatePlayer", Lifecycle: false}) {
		t.Fatalf("first reducer export = %+v", export.Reducers[0])
	}
	if export.Reducers[1] != (ReducerExport{Name: "OnConnect", Lifecycle: true}) {
		t.Fatalf("lifecycle reducer export = %+v", export.Reducers[1])
	}
}

func TestExportSchemaIncludesExtendedColumnKinds(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "extended",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "i128", Type: KindInt128},
			{Name: "u128", Type: KindUint128},
			{Name: "i256", Type: KindInt256},
			{Name: "u256", Type: KindUint256},
			{Name: "created_at", Type: KindTimestamp},
			{Name: "tags", Type: KindArrayString},
			{Name: "uuid", Type: KindUUID},
			{Name: "ttl", Type: KindDuration},
			{Name: "metadata", Type: KindJSON},
		},
	})

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	export := e.ExportSchema()
	got := map[string]string{}
	for _, column := range export.Tables[0].Columns {
		got[column.Name] = column.Type
	}
	want := map[string]string{
		"id":         "uint64",
		"i128":       "int128",
		"u128":       "uint128",
		"i256":       "int256",
		"u256":       "uint256",
		"created_at": "timestamp",
		"tags":       "arrayString",
		"uuid":       "uuid",
		"ttl":        "duration",
		"metadata":   "json",
	}
	for name, wantType := range want {
		if got[name] != wantType {
			t.Fatalf("exported column %q type = %q, want %q; all columns = %#v", name, got[name], wantType, export.Tables[0].Columns)
		}
	}
}

func TestExportSchemaIncludesTableReadPolicy(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "messages",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
		},
	}, WithReadPermissions("messages:read"))

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	export := e.ExportSchema()
	if len(export.Tables) == 0 {
		t.Fatal("ExportSchema returned no tables")
	}
	policy := export.Tables[0].ReadPolicy
	if policy.Access != TableAccessPermissioned {
		t.Fatalf("export read access = %s, want permissioned", policy.Access)
	}
	if len(policy.Permissions) != 1 || policy.Permissions[0] != "messages:read" {
		t.Fatalf("export read permissions = %#v, want [messages:read]", policy.Permissions)
	}

	export.Tables[0].ReadPolicy.Permissions[0] = "mutated"
	again := e.ExportSchema()
	if got := again.Tables[0].ReadPolicy.Permissions[0]; got != "messages:read" {
		t.Fatalf("second export read permission = %q, want detached copy", got)
	}
}

func TestSchemaExportJSONRoundTripIncludesTableReadPolicy(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "messages",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
		},
	}, WithPublicRead())

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	data, err := json.Marshal(e.ExportSchema())
	if err != nil {
		t.Fatalf("Marshal export: %v", err)
	}
	var decoded SchemaExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal export: %v", err)
	}
	if decoded.Tables[0].ReadPolicy.Access != TableAccessPublic {
		t.Fatalf("decoded read access = %s, want public; json=%s", decoded.Tables[0].ReadPolicy.Access, data)
	}
}

func TestReadPolicyJSONRejectsNullPolicyAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "policy-null", data: []byte(`null`)},
		{name: "policy-whitespace-null", data: []byte(" \n null \t")},
		{name: "empty-object", data: []byte(`{}`)},
		{name: "access-only", data: []byte(`{"access":"private"}`)},
		{name: "permissions-only", data: []byte(`{"permissions":[]}`)},
		{name: "permissions-null", data: []byte(`{"access":"private","permissions":null}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var policy ReadPolicy
			err := json.Unmarshal(tc.data, &policy)
			if !errors.Is(err, ErrInvalidTableReadPolicy) {
				t.Fatalf("Unmarshal ReadPolicy error = %v, want ErrInvalidTableReadPolicy", err)
			}
		})
	}
}

func TestExportSchemaReturnsDetachedSnapshot(t *testing.T) {
	e, err := validBuilder().Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	export := e.ExportSchema()
	export.Tables[0].Name = "mutated"
	export.Tables[0].Columns[0].Name = "mutated_col"
	export.Tables[0].Indexes[0].Columns[0] = "mutated_idx_col"
	export.Tables[0].Indexes[0].ColumnOrdinals[0] = 99
	export.Reducers = append(export.Reducers, ReducerExport{Name: "Mutated", Lifecycle: false})

	again := e.ExportSchema()
	if again.Tables[0].Name != "players" {
		t.Fatalf("detached table name = %q, want players", again.Tables[0].Name)
	}
	if again.Tables[0].Columns[0].Name != "id" {
		t.Fatalf("detached column name = %q, want id", again.Tables[0].Columns[0].Name)
	}
	if again.Tables[0].Indexes[0].Columns[0] != "id" {
		t.Fatalf("detached index columns = %v, want [id]", again.Tables[0].Indexes[0].Columns)
	}
	if again.Tables[0].Indexes[0].ColumnOrdinals[0] != 0 {
		t.Fatalf("detached index column ordinals = %v, want [0]", again.Tables[0].Indexes[0].ColumnOrdinals)
	}
}

func TestSchemaRegistryConcurrentReadExportShortSoak(t *testing.T) {
	const seed = uint64(0x5c4e6a)
	const workers = 8
	const iterations = 64

	b := NewBuilder()
	b.SchemaVersion(7)
	b.TableDef(TableDefinition{
		Name: "players",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "name", Type: KindString},
		},
	}, WithPublicRead())
	b.TableDef(TableDefinition{
		Name: "messages",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "player_id", Type: KindUint64},
			{Name: "body", Type: KindString},
		},
	}, WithReadPermissions("messages:read"))
	b.Reducer("CreateMessage", func(*ReducerContext, []byte) ([]byte, error) { return nil, nil })
	b.OnConnect(func(*ReducerContext) error { return nil })

	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	reg := e.Registry()
	baseline := e.ExportSchema()

	start := make(chan struct{})
	failures := make(chan string, workers*iterations)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for iteration := range iterations {
				opIndex := worker*iterations + iteration
				if ((uint64(opIndex) ^ seed) & 3) == 0 {
					runtime.Gosched()
				}
				switch (uint64(opIndex) ^ seed) % 6 {
				case 0:
					ids := reg.Tables()
					if len(ids) != 4 || ids[0] != 0 || ids[1] != 1 {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=Tables observed=%v expected_prefix=[0 1] expected_len=4",
							seed, opIndex, worker, workers, iterations, ids)
						return
					}
					ids[0] = 99
					if again := reg.Tables(); again[0] != 0 {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=Tables-detached observed=%v expected_first=0",
							seed, opIndex, worker, workers, iterations, again)
						return
					}
				case 1:
					id, table, ok := reg.TableByName("Players")
					if !ok || id != 0 || table.Name != "players" || len(table.Columns) != 2 {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=TableByName observed=(%d,%+v,%v) expected=(0,players,true)",
							seed, opIndex, worker, workers, iterations, id, table, ok)
						return
					}
					table.Columns[0].Name = "mutated"
					_, again, ok := reg.TableByName("players")
					if !ok || again.Columns[0].Name != "id" {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=TableByName-detached observed=(%+v,%v) expected_first_column=id",
							seed, opIndex, worker, workers, iterations, again, ok)
						return
					}
				case 2:
					table, ok := reg.Table(1)
					if !ok || table.Name != "messages" || table.ReadPolicy.Access != TableAccessPermissioned || len(table.ReadPolicy.Permissions) != 1 {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=Table observed=(%+v,%v) expected=messages permissioned",
							seed, opIndex, worker, workers, iterations, table, ok)
						return
					}
					table.ReadPolicy.Permissions[0] = "mutated"
					again, ok := reg.Table(1)
					if !ok || again.ReadPolicy.Permissions[0] != "messages:read" {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=Table-detached observed=(%+v,%v) expected_permission=messages:read",
							seed, opIndex, worker, workers, iterations, again, ok)
						return
					}
				case 3:
					names := reg.Reducers()
					if len(names) != 1 || names[0] != "CreateMessage" || reg.OnConnect() == nil || reg.OnDisconnect() != nil {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=Reducers observed_names=%v on_connect_nil=%v on_disconnect_nil=%v expected=[CreateMessage],false,true",
							seed, opIndex, worker, workers, iterations, names, reg.OnConnect() == nil, reg.OnDisconnect() == nil)
						return
					}
					names[0] = "mutated"
					if again := reg.Reducers(); again[0] != "CreateMessage" {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=Reducers-detached observed=%v expected=[CreateMessage]",
							seed, opIndex, worker, workers, iterations, again)
						return
					}
				case 4:
					got := e.ExportSchema()
					if !reflect.DeepEqual(got, baseline) {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ExportSchema observed=%#v expected=%#v",
							seed, opIndex, worker, workers, iterations, got, baseline)
						return
					}
					got.Tables[0].Name = "mutated"
					got.Tables[0].Columns[0].Name = "mutated"
					got.Tables[1].ReadPolicy.Permissions[0] = "mutated"
					if again := e.ExportSchema(); !reflect.DeepEqual(again, baseline) {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=ExportSchema-detached observed=%#v expected=%#v",
							seed, opIndex, worker, workers, iterations, again, baseline)
						return
					}
				case 5:
					if reg.TableName(1) != "messages" || !reg.ColumnExists(1, 2) || reg.ColumnType(1, 2) != KindString || !reg.HasIndex(1, 0) {
						failures <- fmt.Sprintf("seed=%#x op_index=%d worker=%d runtime_config=workers=%d,iterations=%d operation=lookup-primitives observed_table=%q column_exists=%v column_type=%v has_index=%v expected=messages,true,string,true",
							seed, opIndex, worker, workers, iterations, reg.TableName(1), reg.ColumnExists(1, 2), reg.ColumnType(1, 2), reg.HasIndex(1, 0))
						return
					}
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestSchemaExportJSONRoundTrip(t *testing.T) {
	b := NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(TableDefinition{
		Name: "files",
		Columns: []ColumnDefinition{
			{Name: "id", Type: KindUint64, PrimaryKey: true},
			{Name: "payload", Type: KindBytes},
		},
	})
	b.Reducer("Upload", func(*ReducerContext, []byte) ([]byte, error) { return []byte("ok"), nil })
	e, err := b.Build(EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(e.ExportSchema())
	if err != nil {
		t.Fatalf("Marshal export: %v", err)
	}
	var decoded SchemaExport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal export: %v", err)
	}
	if decoded.Version != 1 {
		t.Fatalf("decoded version = %d, want 1", decoded.Version)
	}
	if decoded.Tables[0].Columns[1].Type != ValueKindExportString(types.KindBytes) {
		t.Fatalf("decoded bytes column type = %q, want bytes", decoded.Tables[0].Columns[1].Type)
	}
}
