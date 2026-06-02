package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	typeIndexCanaryTableID   schema.TableID = 0
	typeIndexCanaryTableName                = "flat_values"
)

type typeIndexCanaryInput struct {
	ID     uint64  `json:"id"`
	Label  string  `json:"label"`
	Bucket string  `json:"bucket"`
	Seq    uint64  `json:"seq"`
	Note   *string `json:"note"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hosted type-index canary: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.MkdirTemp("", "shunter-hosted-type-index-canary-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(root)

	rt, err := buildRuntime(filepath.Join(root, "data"))
	if err != nil {
		return err
	}
	runtimeClosed := false
	closeRuntime := func() error {
		if runtimeClosed {
			return nil
		}
		runtimeClosed = true
		return rt.Close()
	}
	defer closeRuntime()

	if err := rt.Start(context.Background()); err != nil {
		return fmt.Errorf("start runtime: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{
		Handler:           rt.HTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(ln)
	}()

	ready := struct {
		URL string `json:"url"`
	}{
		URL: "ws://" + ln.Addr().String() + "/subscribe",
	}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
		return fmt.Errorf("write ready message: %w", err)
	}

	stdinClosed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stdinClosed)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	var earlyServeErr error
	select {
	case <-stdinClosed:
	case <-stop:
	case err := <-serveErr:
		earlyServeErr = err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	shutdownErr := server.Shutdown(ctx)
	cancel()
	runtimeErr := closeRuntime()
	if earlyServeErr == nil {
		earlyServeErr = <-serveErr
	}
	if errors.Is(earlyServeErr, http.ErrServerClosed) {
		earlyServeErr = nil
	}
	if errors.Is(shutdownErr, http.ErrServerClosed) {
		shutdownErr = nil
	}
	return errors.Join(earlyServeErr, shutdownErr, runtimeErr)
}

func buildRuntime(dataDir string) (*shunter.Runtime, error) {
	mod := shunter.NewModule("type_index_canary").
		SchemaVersion(1).
		TableDef(typeIndexCanaryTableDef(), schema.WithPublicRead()).
		Reducer("insert_flat_value", insertTypeIndexCanaryReducer).
		Reducer("update_flat_value", updateTypeIndexCanaryReducer).
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
		return nil, fmt.Errorf("build runtime: %w", err)
	}
	return rt, nil
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
	rowID, ok := findTypeIndexCanaryRow(ctx, input.ID)
	if !ok {
		return nil, fmt.Errorf("flat value %d not found", input.ID)
	}
	_, err = ctx.DB.Update(uint32(typeIndexCanaryTableID), rowID, mustTypeIndexCanaryRow(input))
	return nil, err
}

func parseTypeIndexCanaryInput(args []byte) (typeIndexCanaryInput, error) {
	var input typeIndexCanaryInput
	if err := json.Unmarshal(args, &input); err != nil {
		return typeIndexCanaryInput{}, err
	}
	return input, nil
}

func findTypeIndexCanaryRow(ctx *schema.ReducerContext, id uint64) (types.RowID, bool) {
	for rowID := range ctx.DB.SeekIndex(uint32(typeIndexCanaryTableID), 0, types.NewUint64(id)) {
		return rowID, true
	}
	return 0, false
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
