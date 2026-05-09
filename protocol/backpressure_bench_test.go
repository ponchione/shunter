package protocol

import (
	"errors"
	"testing"
)

func BenchmarkClientSenderBackpressureFullBuffer(b *testing.B) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := testConnDirect(&opts)
	mgr := NewConnManager()
	if err := mgr.Add(conn); err != nil {
		b.Fatalf("register connection: %v", err)
	}
	sender := NewClientSender(mgr, nil)

	rows := EncodeRowList([][]byte{{0x01, 0x02, 0x03, 0x04}})
	update := &TransactionUpdateLight{
		RequestID: 1,
		Update: []SubscriptionUpdate{{
			QueryID:   1,
			TableName: "orders",
			Inserts:   rows,
			Deletes:   EncodeRowList(nil),
		}},
	}

	conn.OutboundCh <- []byte{TagTransactionUpdateLight}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := sender.SendTransactionUpdateLight(conn.ID, update)
		if !errors.Is(err, ErrClientBufferFull) {
			b.Fatalf("SendTransactionUpdateLight error = %v, want ErrClientBufferFull", err)
		}
	}
}
