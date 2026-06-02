package shunter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"path/filepath"
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
	typeIndexCanaryTableID      schema.TableID = 0
	typeIndexCanaryTableName                   = "flat_values"
	typeIndexCanaryRuntimeLabel                = "type-index-canary/protocol=enabled/table=flat_values"
)

type typeIndexCanaryInput struct {
	ID     uint64  `json:"id"`
	Label  string  `json:"label"`
	Bucket string  `json:"bucket"`
	Seq    uint64  `json:"seq"`
	Note   *string `json:"note"`
}

func TestRuntimeGauntletFlatTypeIndexCanary(t *testing.T) {
	assertTypeIndexCanaryFloatBoundaries(t)

	dataDir := t.TempDir()
	rt := buildTypeIndexCanaryRuntime(t, dataDir)
	t.Cleanup(func() {
		if rt != nil {
			_ = rt.Close()
		}
	})

	alphaNote := "alpha note"
	betaNote := "beta note"
	alpha := typeIndexCanaryInput{ID: 1, Label: "alpha", Bucket: "active", Seq: 10, Note: &alphaNote}
	beta := typeIndexCanaryInput{ID: 2, Label: "beta", Bucket: "inactive", Seq: 20, Note: &betaNote}
	want := map[uint64]types.ProductValue{}

	callTypeIndexCanaryReducer(t, rt, 1, "insert_flat_value", alpha, shunter.StatusCommitted)
	want[alpha.ID] = typeIndexCanaryRow(t, alpha)
	callTypeIndexCanaryReducer(t, rt, 2, "insert_flat_value", beta, shunter.StatusCommitted)
	want[beta.ID] = typeIndexCanaryRow(t, beta)
	callTypeIndexCanaryReducer(t, rt, 3, "insert_flat_value",
		typeIndexCanaryInput{ID: 99, Label: "alpha", Bucket: "duplicate", Seq: 99},
		shunter.StatusFailedUser)

	assertTypeIndexCanaryLocalRead(t, rt, want, []string{"missing"}, "after initial reducer writes")
	assertTypeIndexCanaryDeclaredQueryAndView(t, rt, want, 10, "after initial reducer writes")

	client := dialGauntletProtocol(t, rt)
	defer client.CloseNow()
	assertTypeIndexCanaryOneOffProtocolRows(t, client, []byte("raw-alpha"), "SELECT * FROM flat_values WHERE label = 'alpha'", map[uint64]types.ProductValue{
		alpha.ID: want[alpha.ID],
	}, "raw SQL label query")
	assertTypeIndexCanaryDeclaredProtocolRows(t, client, []byte("declared-active"), activeTypeIndexCanaryRows(want), "declared active query")
	subRows := subscribeTypeIndexCanaryProtocolView(t, client, 100, 101, activeTypeIndexCanaryRows(want), "declared active view initial")
	mutateTypeIndexCanaryDecodedRowBuffers(subRows)
	assertTypeIndexCanaryLocalRead(t, rt, want, []string{"missing"}, "after mutating decoded protocol rows")

	gamma := typeIndexCanaryInput{ID: 3, Label: "gamma", Bucket: "active", Seq: 30}
	callTypeIndexCanaryReducer(t, rt, 4, "insert_flat_value", gamma, shunter.StatusCommitted)
	want[gamma.ID] = typeIndexCanaryRow(t, gamma)
	readTypeIndexCanaryProtocolDelta(t, client, 101, map[uint64]types.ProductValue{
		gamma.ID: want[gamma.ID],
	}, nil, "active view insert delta")

	betaActiveNote := "beta active note"
	betaActive := typeIndexCanaryInput{ID: 2, Label: "beta_active", Bucket: "active", Seq: 15, Note: &betaActiveNote}
	callTypeIndexCanaryReducer(t, rt, 5, "update_flat_value", betaActive, shunter.StatusCommitted)
	want[betaActive.ID] = typeIndexCanaryRow(t, betaActive)
	readTypeIndexCanaryProtocolDelta(t, client, 101, map[uint64]types.ProductValue{
		betaActive.ID: want[betaActive.ID],
	}, nil, "active view update-into-window delta")

	callTypeIndexCanaryReducer(t, rt, 6, "delete_flat_value", typeIndexCanaryInput{ID: alpha.ID}, shunter.StatusCommitted)
	delete(want, alpha.ID)
	readTypeIndexCanaryProtocolDelta(t, client, 101, nil, map[uint64]types.ProductValue{
		alpha.ID: typeIndexCanaryRow(t, alpha),
	}, "active view delete delta")

	assertTypeIndexCanaryLocalRead(t, rt, want, []string{"alpha", "beta"}, "after insert update delete")
	assertTypeIndexCanaryDeclaredQueryAndView(t, rt, want, 20, "after insert update delete")

	if err := client.Close(websocket.StatusNormalClosure, "type-index canary complete"); err != nil {
		t.Fatalf("runtime_config=%s operation=CloseProtocolClient observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("runtime_config=%s operation=Close(before restart) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, err)
	}
	rt = nil

	rt = buildTypeIndexCanaryRuntime(t, dataDir)
	assertTypeIndexCanaryLocalRead(t, rt, want, []string{"alpha", "beta"}, "after restart")
	assertTypeIndexCanaryDeclaredQueryAndView(t, rt, want, 30, "after restart")
}

func TestRuntimeGauntletFlatTypeIndexCanaryBackupRestore(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backup")
	restoreDir := filepath.Join(root, "restored")

	rt := buildTypeIndexCanaryRuntime(t, dataDir)
	t.Cleanup(func() {
		if rt != nil {
			_ = rt.Close()
		}
	})

	alphaNote := "alpha restore note"
	gammaNote := "gamma restore note"
	rows := []typeIndexCanaryInput{
		{ID: 1, Label: "restore_alpha", Bucket: "active", Seq: 10, Note: &alphaNote},
		{ID: 2, Label: "restore_beta", Bucket: "inactive", Seq: 20},
		{ID: 3, Label: "restore_gamma", Bucket: "active", Seq: 30, Note: &gammaNote},
	}
	want := map[uint64]types.ProductValue{}
	var last shunter.ReducerResult
	for i, row := range rows {
		last = callTypeIndexCanaryReducer(t, rt, 100+i, "insert_flat_value", row, shunter.StatusCommitted)
		want[row.ID] = typeIndexCanaryRow(t, row)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := rt.WaitUntilDurable(ctx, last.TxID); err != nil {
		cancel()
		t.Fatalf("runtime_config=%s operation=WaitUntilDurable(before backup) tx_id=%d observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, last.TxID, err)
	}
	cancel()
	if err := rt.Close(); err != nil {
		t.Fatalf("runtime_config=%s operation=Close(before backup) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, err)
	}
	rt = nil

	if err := shunter.BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("runtime_config=%s operation=BackupDataDir observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, err)
	}
	if err := shunter.RestoreDataDir(backupDir, restoreDir); err != nil {
		t.Fatalf("runtime_config=%s operation=RestoreDataDir observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, err)
	}

	rt = buildTypeIndexCanaryRuntime(t, restoreDir)
	assertTypeIndexCanaryLocalRead(t, rt, want, []string{"missing", "restore_missing"}, "after backup restore")
	assertTypeIndexCanaryDeclaredQueryAndView(t, rt, want, 110, "after backup restore")
}

func buildTypeIndexCanaryRuntime(t *testing.T, dataDir string) *shunter.Runtime {
	t.Helper()
	mod := shunter.NewModule("type_index_canary").
		SchemaVersion(1).
		TableDef(typeIndexCanaryTableDef(), schema.WithPublicRead()).
		Reducer("insert_flat_value", insertTypeIndexCanaryReducer).
		Reducer("update_flat_value", updateTypeIndexCanaryReducer).
		Reducer("delete_flat_value", deleteTypeIndexCanaryReducer).
		Query(shunter.QueryDeclaration{
			Name: "active_flat_values",
			SQL:  "SELECT * FROM flat_values WHERE bucket = 'active' ORDER BY seq LIMIT 10",
		}).
		View(shunter.ViewDeclaration{
			Name: "active_flat_values_live",
			SQL:  "SELECT * FROM flat_values WHERE bucket = 'active' ORDER BY seq LIMIT 10",
		})

	rt, err := shunter.Build(mod, shunter.Config{DataDir: dataDir, EnableProtocol: true})
	if err != nil {
		t.Fatalf("runtime_config=%s operation=Build observed_error=%v expected=nil", typeIndexCanaryRuntimeLabel, err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("runtime_config=%s operation=Start observed_error=%v expected=nil", typeIndexCanaryRuntimeLabel, err)
	}
	return rt
}

func typeIndexCanaryTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: typeIndexCanaryTableName,
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "label", Type: types.KindString},
			{Name: "bucket", Type: types.KindString},
			{Name: "seq", Type: types.KindUint64},
			{Name: "flag", Type: types.KindBool},
			{Name: "i8", Type: types.KindInt8},
			{Name: "u8", Type: types.KindUint8},
			{Name: "i16", Type: types.KindInt16},
			{Name: "u16", Type: types.KindUint16},
			{Name: "i32", Type: types.KindInt32},
			{Name: "u32", Type: types.KindUint32},
			{Name: "i64", Type: types.KindInt64},
			{Name: "u64", Type: types.KindUint64},
			{Name: "i128", Type: types.KindInt128},
			{Name: "u128", Type: types.KindUint128},
			{Name: "i256", Type: types.KindInt256},
			{Name: "u256", Type: types.KindUint256},
			{Name: "f32", Type: types.KindFloat32},
			{Name: "f64", Type: types.KindFloat64},
			{Name: "created_at", Type: types.KindTimestamp},
			{Name: "ttl", Type: types.KindDuration},
			{Name: "uuid", Type: types.KindUUID},
			{Name: "blob", Type: types.KindBytes},
			{Name: "metadata", Type: types.KindJSON},
			{Name: "tags", Type: types.KindArrayString},
			{Name: "optional_note", Type: types.KindString, Nullable: true},
		},
		Indexes: []schema.IndexDefinition{
			{Name: "label_uniq", Columns: []string{"label"}, Unique: true},
			{Name: "bucket_idx", Columns: []string{"bucket"}},
			{Name: "seq_idx", Columns: []string{"seq"}},
		},
	}
}

func typeIndexCanaryTableSchema() *schema.TableSchema {
	def := typeIndexCanaryTableDef()
	columns := make([]schema.ColumnSchema, len(def.Columns))
	for i, col := range def.Columns {
		columns[i] = schema.ColumnSchema{Index: i, Name: col.Name, Type: col.Type, Nullable: col.Nullable}
	}
	return &schema.TableSchema{ID: typeIndexCanaryTableID, Name: def.Name, Columns: columns}
}

func insertTypeIndexCanaryReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	input, err := parseTypeIndexCanaryInput(args)
	if err != nil {
		return nil, err
	}
	_, err = ctx.DB.Insert(uint32(typeIndexCanaryTableID), mustTypeIndexCanaryRow(input))
	return nil, err
}

func updateTypeIndexCanaryReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	input, err := parseTypeIndexCanaryInput(args)
	if err != nil {
		return nil, err
	}
	rowID, _, ok := findTypeIndexCanaryRow(ctx, input.ID)
	if !ok {
		return nil, fmt.Errorf("flat value %d not found", input.ID)
	}
	_, err = ctx.DB.Update(uint32(typeIndexCanaryTableID), rowID, mustTypeIndexCanaryRow(input))
	return nil, err
}

func deleteTypeIndexCanaryReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	input, err := parseTypeIndexCanaryInput(args)
	if err != nil {
		return nil, err
	}
	rowID, _, ok := findTypeIndexCanaryRow(ctx, input.ID)
	if !ok {
		return nil, fmt.Errorf("flat value %d not found", input.ID)
	}
	return nil, ctx.DB.Delete(uint32(typeIndexCanaryTableID), rowID)
}

func parseTypeIndexCanaryInput(args []byte) (typeIndexCanaryInput, error) {
	var input typeIndexCanaryInput
	if err := json.Unmarshal(args, &input); err != nil {
		return typeIndexCanaryInput{}, err
	}
	return input, nil
}

func findTypeIndexCanaryRow(ctx *schema.ReducerContext, id uint64) (types.RowID, types.ProductValue, bool) {
	for rowID, row := range ctx.DB.SeekIndex(uint32(typeIndexCanaryTableID), 0, types.NewUint64(id)) {
		return rowID, row, true
	}
	return 0, nil, false
}

func callTypeIndexCanaryReducer(t *testing.T, rt *shunter.Runtime, op int, reducer string, input typeIndexCanaryInput, wantStatus shunter.ReducerStatus) shunter.ReducerResult {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("runtime_config=%s op=%d operation=Marshal(%s) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, op, reducer, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := rt.CallReducer(ctx, reducer, body, shunter.WithRequestID(uint32(5000+op)))
	if err != nil {
		t.Fatalf("runtime_config=%s op=%d operation=CallReducer(%s) observed_admission_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, op, reducer, err)
	}
	if res.Status != wantStatus {
		t.Fatalf("runtime_config=%s op=%d operation=CallReducer(%s) observed_status=%v observed_error=%v expected_status=%v",
			typeIndexCanaryRuntimeLabel, op, reducer, res.Status, res.Error, wantStatus)
	}
	if wantStatus == shunter.StatusCommitted && res.TxID == 0 {
		t.Fatalf("runtime_config=%s op=%d operation=CallReducer(%s) observed_txid=0 expected_nonzero",
			typeIndexCanaryRuntimeLabel, op, reducer)
	}
	return res
}

func typeIndexCanaryRow(t *testing.T, input typeIndexCanaryInput) types.ProductValue {
	t.Helper()
	row, err := buildTypeIndexCanaryRow(input)
	if err != nil {
		t.Fatalf("runtime_config=%s operation=BuildExpectedRow(%d) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, input.ID, err)
	}
	return row
}

func mustTypeIndexCanaryRow(input typeIndexCanaryInput) types.ProductValue {
	row, err := buildTypeIndexCanaryRow(input)
	if err != nil {
		panic(err)
	}
	return row
}

func buildTypeIndexCanaryRow(input typeIndexCanaryInput) (types.ProductValue, error) {
	f32, err := types.NewFloat32(float32(input.ID) + 0.25)
	if err != nil {
		return nil, err
	}
	f64, err := types.NewFloat64(float64(input.ID) + 0.5)
	if err != nil {
		return nil, err
	}
	metadata, err := types.NewJSON([]byte(fmt.Sprintf(`{"bucket":%q,"id":%d,"label":%q}`, input.Bucket, input.ID, input.Label)))
	if err != nil {
		return nil, err
	}
	note := types.NewNull(types.KindString)
	if input.Note != nil {
		note = types.NewString(*input.Note)
	}
	return types.ProductValue{
		types.NewUint64(input.ID),
		types.NewString(input.Label),
		types.NewString(input.Bucket),
		types.NewUint64(input.Seq),
		types.NewBool(input.ID%2 == 1),
		types.NewInt8(-int8(input.ID)),
		types.NewUint8(uint8(input.ID + 10)),
		types.NewInt16(-int16(input.ID * 2)),
		types.NewUint16(uint16(input.ID*2 + 10)),
		types.NewInt32(-int32(input.ID * 3)),
		types.NewUint32(uint32(input.ID*3 + 10)),
		types.NewInt64(-int64(input.ID * 4)),
		types.NewUint64(input.ID*4 + 10),
		types.NewInt128(int64(input.ID), input.Seq+100),
		types.NewUint128(input.ID+200, input.Seq+200),
		types.NewInt256(int64(input.ID), input.Seq+300, input.ID+300, input.Seq+301),
		types.NewUint256(input.ID+400, input.Seq+400, input.ID+401, input.Seq+401),
		f32,
		f64,
		types.NewTimestamp(1_700_000_000_000_000 + int64(input.Seq)),
		types.NewDuration(int64(input.Seq) * 1_000),
		typeIndexCanaryUUID(input.ID),
		types.NewBytes([]byte{byte(input.ID), byte(input.Seq), 0xA5}),
		metadata,
		types.NewArrayString([]string{input.Bucket, input.Label, fmt.Sprintf("seq-%d", input.Seq)}),
		note,
	}, nil
}

func typeIndexCanaryUUID(id uint64) types.Value {
	var uuid [16]byte
	copy(uuid[:], []byte{0x10, 0x20, 0x30, 0x40})
	uuid[8] = byte(id >> 24)
	uuid[9] = byte(id >> 16)
	uuid[10] = byte(id >> 8)
	uuid[11] = byte(id)
	uuid[15] = byte(id + 0x40)
	return types.NewUUID(uuid)
}

func assertTypeIndexCanaryFloatBoundaries(t *testing.T) {
	t.Helper()
	if _, err := types.NewFloat32(float32(math.NaN())); !errors.Is(err, types.ErrInvalidFloat) {
		t.Fatalf("operation=NewFloat32(NaN) observed_error=%v expected=%v", err, types.ErrInvalidFloat)
	}
	if _, err := types.NewFloat64(math.NaN()); !errors.Is(err, types.ErrInvalidFloat) {
		t.Fatalf("operation=NewFloat64(NaN) observed_error=%v expected=%v", err, types.ErrInvalidFloat)
	}
}

func assertTypeIndexCanaryLocalRead(t *testing.T, rt *shunter.Runtime, want map[uint64]types.ProductValue, absentLabels []string, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := rt.Read(ctx, func(view shunter.LocalReadView) error {
		if gotCount := view.RowCount(typeIndexCanaryTableID); gotCount != len(want) {
			return fmt.Errorf("row_count=%d want=%d", gotCount, len(want))
		}
		got, err := collectTypeIndexCanaryRows(view.TableScan(typeIndexCanaryTableID))
		if err != nil {
			return err
		}
		if err := compareTypeIndexCanaryRows(got, want); err != nil {
			return err
		}
		if err := assertTypeIndexCanaryIndexRows(rt, view, want, absentLabels); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runtime_config=%s operation=Read(%s) observed_error=%v expected_rows=%s",
			typeIndexCanaryRuntimeLabel, label, err, formatTypeIndexCanaryRowIDs(want))
	}
}

func assertTypeIndexCanaryIndexRows(rt *shunter.Runtime, view shunter.LocalReadView, want map[uint64]types.ProductValue, absentLabels []string) error {
	primaryIndex := schema.IndexID(0)
	labelIndex := typeIndexCanaryIndexID(rt, "label_uniq")
	bucketIndex := typeIndexCanaryIndexID(rt, "bucket_idx")
	seqIndex := typeIndexCanaryIndexID(rt, "seq_idx")
	for id, row := range want {
		gotPrimary, err := collectTypeIndexCanaryRows(view.SeekIndex(typeIndexCanaryTableID, primaryIndex, row[0]))
		if err != nil {
			return fmt.Errorf("primary index id=%d: %w", id, err)
		}
		if err := compareTypeIndexCanaryRows(gotPrimary, map[uint64]types.ProductValue{id: row}); err != nil {
			return fmt.Errorf("primary index id=%d: %w", id, err)
		}
		got, err := collectTypeIndexCanaryRows(view.SeekIndex(typeIndexCanaryTableID, labelIndex, row[1]))
		if err != nil {
			return fmt.Errorf("label index id=%d: %w", id, err)
		}
		if err := compareTypeIndexCanaryRows(got, map[uint64]types.ProductValue{id: row}); err != nil {
			return fmt.Errorf("label index id=%d: %w", id, err)
		}
	}
	for _, label := range absentLabels {
		got, err := collectTypeIndexCanaryRows(view.SeekIndex(typeIndexCanaryTableID, labelIndex, types.NewString(label)))
		if err != nil {
			return fmt.Errorf("absent label index %q: %w", label, err)
		}
		if len(got) != 0 {
			return fmt.Errorf("absent label index %q returned %s", label, formatTypeIndexCanaryRowIDs(got))
		}
	}
	activeRows := activeTypeIndexCanaryRows(want)
	gotActive, err := collectTypeIndexCanaryRows(view.SeekIndex(typeIndexCanaryTableID, bucketIndex, types.NewString("active")))
	if err != nil {
		return fmt.Errorf("bucket index: %w", err)
	}
	if err := compareTypeIndexCanaryRows(gotActive, activeRows); err != nil {
		return fmt.Errorf("bucket index: %w", err)
	}
	gotRange, err := collectTypeIndexCanaryRows(view.SeekIndexRange(typeIndexCanaryTableID, seqIndex, shunter.Inclusive(types.NewUint64(0)), shunter.Inclusive(types.NewUint64(100))))
	if err != nil {
		return fmt.Errorf("seq range index: %w", err)
	}
	if err := compareTypeIndexCanaryRows(gotRange, want); err != nil {
		return fmt.Errorf("seq range index: %w", err)
	}
	return nil
}

func typeIndexCanaryIndexID(rt *shunter.Runtime, name string) schema.IndexID {
	export := rt.ExportSchema()
	for _, table := range export.Tables {
		if table.Name != typeIndexCanaryTableName {
			continue
		}
		for _, index := range table.Indexes {
			if index.Name == name {
				return index.ID
			}
		}
	}
	panic(fmt.Sprintf("type-index canary index %q not found", name))
}

func collectTypeIndexCanaryRows(rows iter.Seq2[types.RowID, types.ProductValue]) (map[uint64]types.ProductValue, error) {
	got := map[uint64]types.ProductValue{}
	for _, row := range rows {
		if len(row) != len(typeIndexCanaryTableDef().Columns) {
			return nil, fmt.Errorf("row_width=%d want=%d", len(row), len(typeIndexCanaryTableDef().Columns))
		}
		id := row[0].AsUint64()
		if _, exists := got[id]; exists {
			return nil, fmt.Errorf("duplicate id %d", id)
		}
		got[id] = row
	}
	return got, nil
}

func assertTypeIndexCanaryDeclaredQueryAndView(t *testing.T, rt *shunter.Runtime, want map[uint64]types.ProductValue, op int, label string) {
	t.Helper()
	wantActive := activeTypeIndexCanaryRows(want)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query, err := rt.CallQuery(ctx, "active_flat_values", shunter.WithDeclaredReadRequestID(uint32(6000+op)))
	if err != nil {
		t.Fatalf("runtime_config=%s op=%d operation=CallQuery(%s) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, op, label, err)
	}
	if query.Name != "active_flat_values" || query.TableName != typeIndexCanaryTableName {
		t.Fatalf("runtime_config=%s op=%d operation=CallQuery(%s) observed_identity=(%q,%q) expected=(active_flat_values,%s)",
			typeIndexCanaryRuntimeLabel, op, label, query.Name, query.TableName, typeIndexCanaryTableName)
	}
	assertTypeIndexCanaryRows(t, rowsToTypeIndexCanaryMap(t, query.Rows, label+" query rows"), wantActive, label+" query rows")

	sub, err := rt.SubscribeView(ctx, "active_flat_values_live", uint32(7000+op),
		shunter.WithDeclaredReadConnectionID(typeIndexCanaryConnectionID(op)),
		shunter.WithDeclaredReadRequestID(uint32(8000+op)),
	)
	if err != nil {
		t.Fatalf("runtime_config=%s op=%d operation=SubscribeView(%s) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, op, label, err)
	}
	if sub.Name != "active_flat_values_live" || sub.TableName != typeIndexCanaryTableName || sub.QueryID != uint32(7000+op) {
		t.Fatalf("runtime_config=%s op=%d operation=SubscribeView(%s) observed_identity=(%q,%q,%d) expected=(active_flat_values_live,%s,%d)",
			typeIndexCanaryRuntimeLabel, op, label, sub.Name, sub.TableName, sub.QueryID, typeIndexCanaryTableName, uint32(7000+op))
	}
	assertTypeIndexCanaryRows(t, rowsToTypeIndexCanaryMap(t, sub.InitialRows, label+" view rows"), wantActive, label+" view rows")
}

func typeIndexCanaryConnectionID(op int) types.ConnectionID {
	return types.ConnectionID{0x54, 0x49, byte(op >> 8), byte(op)}
}

func assertTypeIndexCanaryOneOffProtocolRows(t *testing.T, client *websocket.Conn, messageID []byte, sql string, want map[uint64]types.ProductValue, label string) {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   messageID,
		QueryString: sql,
	}, label)
	resp := readGauntletOneOffQueryResponseWithLabel(t, client, messageID, label)
	if resp.Error != nil {
		t.Fatalf("runtime_config=%s operation=OneOffQuery(%s) observed_error=%q expected=nil",
			typeIndexCanaryRuntimeLabel, label, *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != typeIndexCanaryTableName {
		t.Fatalf("runtime_config=%s operation=OneOffQuery(%s) observed_tables=%+v expected_single_table=%s",
			typeIndexCanaryRuntimeLabel, label, resp.Tables, typeIndexCanaryTableName)
	}
	got := decodeTypeIndexCanaryProtocolRowsDetached(t, resp.Tables[0].Rows, label)
	assertTypeIndexCanaryRows(t, rowsToTypeIndexCanaryMap(t, got, label), want, label)
}

func assertTypeIndexCanaryDeclaredProtocolRows(t *testing.T, client *websocket.Conn, messageID []byte, want map[uint64]types.ProductValue, label string) {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.DeclaredQueryMsg{
		MessageID: messageID,
		Name:      "active_flat_values",
	}, label)
	resp := readGauntletOneOffQueryResponseWithLabel(t, client, messageID, label)
	if resp.Error != nil {
		t.Fatalf("runtime_config=%s operation=DeclaredQuery(%s) observed_error=%q expected=nil",
			typeIndexCanaryRuntimeLabel, label, *resp.Error)
	}
	if len(resp.Tables) != 1 || resp.Tables[0].TableName != typeIndexCanaryTableName {
		t.Fatalf("runtime_config=%s operation=DeclaredQuery(%s) observed_tables=%+v expected_single_table=%s",
			typeIndexCanaryRuntimeLabel, label, resp.Tables, typeIndexCanaryTableName)
	}
	got := decodeTypeIndexCanaryProtocolRowsDetached(t, resp.Tables[0].Rows, label)
	assertTypeIndexCanaryRows(t, rowsToTypeIndexCanaryMap(t, got, label), want, label)
}

func subscribeTypeIndexCanaryProtocolView(t *testing.T, client *websocket.Conn, requestID, queryID uint32, want map[uint64]types.ProductValue, label string) []types.ProductValue {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeDeclaredViewMsg{
		RequestID: requestID,
		QueryID:   queryID,
		Name:      "active_flat_values_live",
	}, label)
	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("runtime_config=%s operation=SubscribeDeclaredView(%s) observed_error=%q expected=nil",
			typeIndexCanaryRuntimeLabel, label, subErr.Error)
	}
	applied, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok || applied.RequestID != requestID || applied.QueryID != queryID || applied.TableName != typeIndexCanaryTableName {
		t.Fatalf("runtime_config=%s operation=SubscribeDeclaredView(%s) observed=(tag=%d msg=%+v) expected=request=%d query=%d table=%s",
			typeIndexCanaryRuntimeLabel, label, tag, msg, requestID, queryID, typeIndexCanaryTableName)
	}
	got := decodeTypeIndexCanaryProtocolRowsDetached(t, applied.Rows, label)
	assertTypeIndexCanaryRows(t, rowsToTypeIndexCanaryMap(t, got, label), want, label)
	return got
}

func readTypeIndexCanaryProtocolDelta(t *testing.T, client *websocket.Conn, queryID uint32, wantInserts, wantDeletes map[uint64]types.ProductValue, label string) {
	t.Helper()
	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("runtime_config=%s operation=ReadProtocolDelta(%s) observed_tag=%d expected_tag=%d",
			typeIndexCanaryRuntimeLabel, label, tag, protocol.TagTransactionUpdateLight)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("runtime_config=%s operation=ReadProtocolDelta(%s) observed_type=%T expected=TransactionUpdateLight",
			typeIndexCanaryRuntimeLabel, label, msg)
	}
	inserts := map[uint64]types.ProductValue{}
	deletes := map[uint64]types.ProductValue{}
	for i, entry := range update.Update {
		if entry.QueryID != queryID || entry.TableName != typeIndexCanaryTableName {
			t.Fatalf("runtime_config=%s operation=ReadProtocolDelta(%s) observed_entry_%d=(query=%d table=%q) expected=(query=%d table=%s)",
				typeIndexCanaryRuntimeLabel, label, i, entry.QueryID, entry.TableName, queryID, typeIndexCanaryTableName)
		}
		mergeTypeIndexCanaryRows(t, inserts, decodeTypeIndexCanaryProtocolRowsDetached(t, entry.Inserts, label+" inserts"), label+" inserts")
		mergeTypeIndexCanaryRows(t, deletes, decodeTypeIndexCanaryProtocolRowsDetached(t, entry.Deletes, label+" deletes"), label+" deletes")
	}
	assertTypeIndexCanaryRows(t, inserts, wantInserts, label+" inserts")
	assertTypeIndexCanaryRows(t, deletes, wantDeletes, label+" deletes")
}

func decodeTypeIndexCanaryProtocolRowsDetached(t *testing.T, encoded []byte, label string) []types.ProductValue {
	t.Helper()
	rawRows, err := protocol.DecodeRowList(encoded)
	if err != nil {
		t.Fatalf("runtime_config=%s operation=DecodeRowList(%s) observed_error=%v expected=nil",
			typeIndexCanaryRuntimeLabel, label, err)
	}
	rows := make([]types.ProductValue, 0, len(rawRows))
	table := typeIndexCanaryTableSchema()
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, table)
		if err != nil {
			t.Fatalf("runtime_config=%s operation=DecodeRow(%s row=%d) observed_error=%v expected=nil",
				typeIndexCanaryRuntimeLabel, label, i, err)
		}
		rows = append(rows, row)
	}
	for i := range encoded {
		encoded[i] ^= 0xFF
	}
	return rows
}

func mergeTypeIndexCanaryRows(t *testing.T, dst map[uint64]types.ProductValue, rows []types.ProductValue, label string) {
	t.Helper()
	for _, row := range rows {
		id := row[0].AsUint64()
		if _, exists := dst[id]; exists {
			t.Fatalf("runtime_config=%s operation=MergeRows(%s) observed_duplicate_id=%d expected_unique",
				typeIndexCanaryRuntimeLabel, label, id)
		}
		dst[id] = row
	}
}

func rowsToTypeIndexCanaryMap(t *testing.T, rows []types.ProductValue, label string) map[uint64]types.ProductValue {
	t.Helper()
	got := map[uint64]types.ProductValue{}
	mergeTypeIndexCanaryRows(t, got, rows, label)
	return got
}

func activeTypeIndexCanaryRows(rows map[uint64]types.ProductValue) map[uint64]types.ProductValue {
	active := map[uint64]types.ProductValue{}
	for id, row := range rows {
		if row[2].AsString() == "active" {
			active[id] = row
		}
	}
	return active
}

func assertTypeIndexCanaryRows(t *testing.T, got, want map[uint64]types.ProductValue, label string) {
	t.Helper()
	if err := compareTypeIndexCanaryRows(got, want); err != nil {
		t.Fatalf("runtime_config=%s operation=CompareRows(%s) observed_error=%v observed_ids=%s expected_ids=%s",
			typeIndexCanaryRuntimeLabel, label, err, formatTypeIndexCanaryRowIDs(got), formatTypeIndexCanaryRowIDs(want))
	}
}

func compareTypeIndexCanaryRows(got, want map[uint64]types.ProductValue) error {
	if len(got) != len(want) {
		return fmt.Errorf("row_count=%d want=%d", len(got), len(want))
	}
	for id, wantRow := range want {
		gotRow, ok := got[id]
		if !ok {
			return fmt.Errorf("missing id %d", id)
		}
		if !gotRow.Equal(wantRow) {
			return fmt.Errorf("row id %d mismatch got=%#v want=%#v", id, gotRow, wantRow)
		}
	}
	return nil
}

func formatTypeIndexCanaryRowIDs(rows map[uint64]types.ProductValue) string {
	if len(rows) == 0 {
		return "[]"
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true
	for id := range rows {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&buf, "%d", id)
	}
	buf.WriteByte(']')
	return buf.String()
}

func mutateTypeIndexCanaryDecodedRowBuffers(rows []types.ProductValue) {
	for _, row := range rows {
		if len(row) < 25 {
			continue
		}
		if blob := row[22].BytesView(); len(blob) > 0 {
			blob[0] ^= 0xFF
		}
		if metadata := row[23].JSONView(); len(metadata) > 0 {
			metadata[0] ^= 0xFF
		}
		if tags := row[24].ArrayStringView(); len(tags) > 0 {
			tags[0] = "mutated"
		}
	}
}
