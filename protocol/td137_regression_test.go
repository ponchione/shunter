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
// run a second protocol-layer admission gate over manager-authored fanout.
// Admission is owned by subscription.Manager.querySets; by the time fanout
// reaches this transport helper, the update already carries the client QueryID
// selected at subscribe time.
func TestTD137_DeliverTransactionUpdateLightNoAdmissionGate(t *testing.T) {
	t.Parallel()

	conn, connID := testConn(false)
	mgr := NewConnManager()
	mgr.Add(conn)

	sender := &recordingUpdateSender{}
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		connID: {
			{
				QueryID:   9001,
				TableName: "users",
				Inserts:   []byte{0x01},
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
