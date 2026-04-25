package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestOI002JoinProjectionFromScout_RightTableResolutionPrecedesProjectionQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT x.* FROM t JOIN missing ON t.id = missing.id"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF0},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "no such table: `missing`. If the table exists, it may be marked private."
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002JoinProjectionFromScout_LeftTableResolutionPrecedesProjectionQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	sl := newMockSchema("s", 2,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	const sqlText = "SELECT x.* FROM missing JOIN s ON missing.id = s.id"
	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xF1},
		QueryString: sqlText,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	const want = "no such table: `missing`. If the table exists, it may be marked private."
	if result.Error == nil || *result.Error != want {
		if result.Error == nil {
			t.Fatalf("Error = nil, want %q", want)
		}
		t.Fatalf("Error = %q, want %q", *result.Error, want)
	}
}

func TestOI002JoinProjectionFromScout_SubscribeRightTableResolutionPrecedesProjectionQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("t", 1,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	const sqlText = "SELECT x.* FROM t JOIN missing ON t.id = missing.id"
	msg := &SubscribeSingleMsg{
		RequestID:   730,
		QueryID:     731,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}

func TestOI002JoinProjectionFromScout_SubscribeLeftTableResolutionPrecedesProjectionQualifier(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("s", 2,
		schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32},
	)

	const sqlText = "SELECT x.* FROM missing JOIN s ON missing.id = s.id"
	msg := &SubscribeSingleMsg{
		RequestID:   732,
		QueryID:     733,
		QueryString: sqlText,
	}
	handleSubscribeSingle(context.Background(), conn, msg, executor, sl)

	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want TagSubscriptionError", tag)
	}
	se := decoded.(SubscriptionError)
	want := "no such table: `missing`. If the table exists, it may be marked private., executing: `" + sqlText + "`"
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
}
