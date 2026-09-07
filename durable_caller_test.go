package shunter

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
)

func TestProtocolDurableSuccessSurvivesAbruptExit(t *testing.T) {
	const childDirEnv = "SHUNTER_DURABLE_CALLER_CHILD_DIR"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	mod := validChatModule().Reducer("insert_message", insertMessageReducer)
	if dir := os.Getenv(childDirEnv); dir != "" {
		rt, err := Build(mod, Config{DataDir: dir, EnableProtocol: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.Start(ctx); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(rt.HTTPHandler())
		client, _, err := protocolclient.Dial(ctx, protocolclient.Options{
			URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/subscribe", AllowAnonymous: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		for id := byte(1); id <= 3; id++ {
			if err := client.Send(ctx, protocol.CallReducerMsg{
				ReducerName: "insert_message", Args: []byte{id}, RequestID: uint32(id), Flags: protocol.CallReducerFlagsDurableSuccess,
			}); err != nil {
				t.Fatal(err)
			}
			_, msg, err := client.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			update, ok := msg.(protocol.TransactionUpdate)
			if !ok || update.ReducerCall.RequestID != uint32(id) {
				t.Fatalf("reply = %+v, want request %d", msg, id)
			}
			if _, ok := update.Status.(protocol.StatusCommitted); !ok {
				t.Fatalf("reply status = %+v", update.Status)
			}
			if rt.durability.DurableTxID() < uint64(rt.state.CommittedTxID()) {
				t.Fatal("remote acknowledgement preceded persistence")
			}
		}
		os.Exit(42) // No runtime, HTTP, client, or durability shutdown/flush.
	}
	dataDir := t.TempDir()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProtocolDurableSuccessSurvivesAbruptExit$")
	child.Env = append(os.Environ(), childDirEnv+"="+dataDir)
	output, err := child.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Fatalf("child did not reach abrupt exit: %v\n%s", err, output)
	}
	rt, err := Build(mod, Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rt.Read(ctx, func(view LocalReadView) error {
		if view.RowCount(0) != 3 {
			return fmt.Errorf("recovered %d messages, want 3 acknowledged messages", view.RowCount(0))
		}
		for _, row := range view.TableScan(0) {
			id := row[0].AsUint64()
			if id < 1 || id > 3 || row[1].AsString() != string([]byte{byte(id)}) {
				return fmt.Errorf("unexpected recovered row: %v", row)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
