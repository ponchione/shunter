package app

import (
	"context"
	"testing"

	"github.com/ponchione/shunter"
)

func TestHostedChatModuleReducerAndLiveView(t *testing.T) {
	rt, err := shunter.Build(Module(), shunter.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	args, err := EncodeSendMessageArgs("Ada", "hello from hosted chat")
	if err != nil {
		t.Fatalf("EncodeSendMessageArgs returned error: %v", err)
	}
	res, err := rt.CallReducer(context.Background(), "send_message", args)
	if err != nil {
		t.Fatalf("CallReducer returned error: %v", err)
	}
	if res.Status != shunter.StatusCommitted {
		t.Fatalf("reducer status = %v, error=%v", res.Status, res.Error)
	}
	if err := rt.Read(context.Background(), func(view shunter.LocalReadView) error {
		rows := 0
		for range view.TableScan(messagesTableID) {
			rows++
		}
		if rows != 1 {
			t.Fatalf("local read rows after reducer = %d, want 1", rows)
		}
		return nil
	}); err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	after, err := rt.SubscribeView(context.Background(), "live_messages", 2)
	if err != nil {
		t.Fatalf("SubscribeView after reducer returned error: %v", err)
	}
	if len(after.InitialRows) != 1 {
		t.Fatalf("initial live rows after reducer = %d, want 1", len(after.InitialRows))
	}
	row := after.InitialRows[0]
	if row[1].AsString() != "Ada" || row[2].AsString() != "hello from hosted chat" {
		t.Fatalf("live row = %#v", row)
	}
}

func TestHostedChatModuleRejectsBlankMessage(t *testing.T) {
	rt, err := shunter.Build(Module(), shunter.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	args, err := EncodeSendMessageArgs("Ada", " ")
	if err != nil {
		t.Fatalf("EncodeSendMessageArgs returned error: %v", err)
	}
	res, err := rt.CallReducer(context.Background(), "send_message", args)
	if err != nil {
		t.Fatalf("CallReducer returned error: %v", err)
	}
	if res.Status == shunter.StatusCommitted {
		t.Fatal("blank message committed; want reducer failure")
	}
}
