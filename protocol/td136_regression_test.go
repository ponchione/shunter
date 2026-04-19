package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

type recordedSend struct {
	connID types.ConnectionID
	msg    any
}

type recordingSender struct {
	sent []recordedSend
}

func (s *recordingSender) Send(connID types.ConnectionID, msg any) error {
	s.sent = append(s.sent, recordedSend{connID: connID, msg: msg})
	return nil
}

func (s *recordingSender) SendTransactionUpdate(_ types.ConnectionID, _ *TransactionUpdate) error {
	return nil
}

func (s *recordingSender) SendTransactionUpdateLight(_ types.ConnectionID, _ *TransactionUpdateLight) error {
	return nil
}

// TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed pins the
// C1 regression from TD-140/TD-136: SendSubscribeSingleApplied must deliver
// the Applied envelope on a fresh Conn with no prior tracker Reserve seed.
// Pre-fix, the IsPending guard silently dropped the message in production
// because handleSubscribeSingle no longer Reserves the QueryID.
func TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed(t *testing.T) {
	t.Parallel()

	conn, _ := testConn(false)
	sender := &recordingSender{}
	msg := &SubscribeSingleApplied{
		RequestID: 42,
		QueryID:   7,
		TableName: "users",
		Rows:      []byte{0x01, 0x02},
	}

	if err := SendSubscribeSingleApplied(sender, conn, msg); err != nil {
		t.Fatalf("SendSubscribeSingleApplied returned error: %v", err)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("expected one Send call, got %d", len(sender.sent))
	}
	got, ok := sender.sent[0].msg.(SubscribeSingleApplied)
	if !ok {
		t.Fatalf("sender.sent[0].msg type = %T, want SubscribeSingleApplied", sender.sent[0].msg)
	}
	if got.QueryID != msg.QueryID {
		t.Fatalf("QueryID mismatch: got %d, want %d", got.QueryID, msg.QueryID)
	}
}
