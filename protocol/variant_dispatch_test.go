package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
)

func TestHandleSubscribeSingleSetsSingleVariant(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1, schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32})

	handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
		RequestID: 1,
		QueryID:   2,
		Query:     Query{TableName: "users"},
	}, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("missing register request")
	}
	if req.Variant != SubscriptionSetVariantSingle {
		t.Fatalf("Variant = %v, want SubscriptionSetVariantSingle", req.Variant)
	}
}

func TestHandleSubscribeMultiSetsMultiVariant(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := newMockSchema("users", 1, schema.ColumnSchema{Index: 0, Name: "id", Type: schema.KindUint32})

	handleSubscribeMulti(context.Background(), conn, &SubscribeMultiMsg{
		RequestID: 1,
		QueryID:   2,
		Queries:   []Query{{TableName: "users"}},
	}, executor, sl)

	req := executor.getRegisterSetReq()
	if req == nil {
		t.Fatal("missing register request")
	}
	if req.Variant != SubscriptionSetVariantMulti {
		t.Fatalf("Variant = %v, want SubscriptionSetVariantMulti", req.Variant)
	}
}

func TestHandleUnsubscribeSingleSetsSingleVariant(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockDispatchExecutor{}

	handleUnsubscribeSingle(context.Background(), conn, &UnsubscribeSingleMsg{RequestID: 1, QueryID: 2}, executor)

	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.unregisterSetReq == nil {
		t.Fatal("missing unregister request")
	}
	if executor.unregisterSetReq.Variant != SubscriptionSetVariantSingle {
		t.Fatalf("Variant = %v, want SubscriptionSetVariantSingle", executor.unregisterSetReq.Variant)
	}
}

func TestHandleUnsubscribeMultiSetsMultiVariant(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockDispatchExecutor{}

	handleUnsubscribeMulti(context.Background(), conn, &UnsubscribeMultiMsg{RequestID: 1, QueryID: 2}, executor)

	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.unregisterSetReq == nil {
		t.Fatal("missing unregister request")
	}
	if executor.unregisterSetReq.Variant != SubscriptionSetVariantMulti {
		t.Fatalf("Variant = %v, want SubscriptionSetVariantMulti", executor.unregisterSetReq.Variant)
	}
}
