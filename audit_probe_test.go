package shunter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
)

func TestOversizedChangesetRejectedBeforeCommit(t *testing.T) {
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
	if res.Status == StatusCommitted {
		t.Fatalf("oversized status=%v tx=%d err=%v, want rejection", res.Status, res.TxID, res.Error)
	}
	if res.TxID != 0 {
		t.Fatalf("oversized tx id=%d, want 0", res.TxID)
	}
	var rowErr *commitlog.RowTooLargeError
	if !errors.As(res.Error, &rowErr) {
		t.Fatalf("oversized error=%v, want RowTooLargeError", res.Error)
	}
	assertDataDirRuntimeStateMessageBodies(t, rt, nil)
	health := rt.Health()
	if !health.Durability.Started || health.Durability.Fatal || health.Durability.FatalError != "" {
		t.Fatalf("durability health after rejected transaction=%+v, want healthy", health.Durability)
	}

	valid, err := rt.CallReducer(context.Background(), "insert_message", []byte("durable"))
	if err != nil {
		t.Fatalf("valid CallReducer admission: %v", err)
	}
	if valid.Status != StatusCommitted || valid.TxID != 1 {
		t.Fatalf("valid result=%+v, want committed tx 1", valid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.WaitUntilDurable(ctx, valid.TxID); err != nil {
		t.Fatalf("WaitUntilDurable(%d): %v", valid.TxID, err)
	}
	assertDataDirRuntimeStateMessageBodies(t, rt, []string{"durable"})
	health = rt.Health()
	if health.Durability.Fatal || health.Durability.DurableTxID < valid.TxID {
		t.Fatalf("durability health after valid transaction=%+v, want durable tx %d", health.Durability, valid.TxID)
	}
}
