package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

type recordingUpdateSender struct {
	sent []recordedSend
}

func (s *recordingUpdateSender) Send(connID types.ConnectionID, msg any) error {
	s.sent = append(s.sent, recordedSend{connID: connID, msg: msg})
	return nil
}

func (s *recordingUpdateSender) SendTransactionUpdate(connID types.ConnectionID, msg *TransactionUpdate) error {
	s.sent = append(s.sent, recordedSend{connID: connID, msg: *msg})
	return nil
}

func (s *recordingUpdateSender) SendTransactionUpdateLight(connID types.ConnectionID, msg *TransactionUpdateLight) error {
	s.sent = append(s.sent, recordedSend{connID: connID, msg: *msg})
	return nil
}

// TestTD137_DeliverTransactionUpdateLightNoAdmissionGate pins the C2
// regression from TD-140/TD-137: DeliverTransactionUpdateLight must NOT
// reject a SubscriptionUpdate whose SubscriptionID is a manager-allocated
// internal id. Pre-fix, validateActiveSubscriptionUpdates compared this
// against the wire-QueryID tracker map and returned ErrSubscriptionNotActive
// for every fan-out delivery.
func TestTD137_DeliverTransactionUpdateLightNoAdmissionGate(t *testing.T) {
	t.Parallel()

	conn, connID := testConn(false)
	mgr := NewConnManager()
	mgr.Add(conn)

	sender := &recordingUpdateSender{}
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		connID: {
			{
				SubscriptionID: 9001,
				TableName:      "users",
				Inserts:        []byte{0x01},
			},
		},
	}

	errs := DeliverTransactionUpdateLight(sender, mgr, 0, fanout)

	if len(errs) != 0 {
		t.Fatalf("DeliverTransactionUpdateLight returned %d errors, want 0: %+v", len(errs), errs)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one Send call, got %d", len(sender.sent))
	}
}
