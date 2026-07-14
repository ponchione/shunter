package shunter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
)

func TestAuditProbeOversizeCommittedRowPoisonsDurability(t *testing.T) {
	rt, err := Build(dataDirBackupTestModule(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	body := strings.Repeat("x", int(commitlog.DefaultCommitLogOptions().MaxRowBytes)+1)
	res, err := rt.CallReducer(context.Background(), "insert_message", []byte(body))
	if err != nil {
		t.Fatalf("CallReducer admission: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	durableErr := rt.WaitUntilDurable(ctx, res.TxID)
	t.Logf("status=%v tx=%d result_err=%v durable_err=%v health=%+v", res.Status, res.TxID, res.Error, durableErr, rt.Health())
	if res.Status != StatusCommitted {
		t.Fatalf("status=%v, want committed to demonstrate late validation", res.Status)
	}
	if durableErr == nil {
		t.Fatal("WaitUntilDurable returned nil for an oversized committed row")
	}
}
