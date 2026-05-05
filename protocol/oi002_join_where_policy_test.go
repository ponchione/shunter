package protocol

import (
	"context"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestHandleOneOffQuery_InnerJoinWhereColumnComparisonFiltersPairs(t *testing.T) {
	conn := testConnDirect(nil)
	sl := exactIdentifierJoinSchema()
	stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(10)},
			{types.NewUint32(2), types.NewUint32(20)},
			{types.NewUint32(3), types.NewUint32(20)},
		},
		2: {
			{types.NewUint32(1), types.NewUint32(10)},
			{types.NewUint32(99), types.NewUint32(20)},
			{types.NewUint32(3), types.NewUint32(20)},
		},
	}}}

	const sqlText = "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id"
	handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
		MessageID:   []byte{0xB0},
		QueryString: sqlText,
	}, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error != nil {
		t.Fatalf("one-off error = %q, want nil", *result.Error)
	}
	if len(result.Tables) != 1 || result.Tables[0].TableName != "t" {
		t.Fatalf("tables = %#v, want one t result table", result.Tables)
	}
	_, tSchema, ok := sl.TableByName("t")
	if !ok {
		t.Fatal("missing t schema")
	}
	rows := decodeRows(t, firstTableRows(result), tSchema)
	assertProductRowsEqual(t, rows, []types.ProductValue{
		{types.NewUint32(1), types.NewUint32(10)},
		{types.NewUint32(3), types.NewUint32(20)},
	})
}

func TestHandleSubscribeSingle_InnerJoinWhereColumnComparisonRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := exactIdentifierJoinSchema()

	const sqlText = "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id"
	handleSubscribeSingle(context.Background(), conn, &SubscribeSingleMsg{
		RequestID:   768,
		QueryID:     769,
		QueryString: sqlText,
	}, executor, sl)

	requireSubscriptionError(t, conn, 768, 769, "join WHERE column comparisons not supported, executing: `"+sqlText+"`")
	requireNoSubscriptionRegistration(t, executor)
}

func TestHandleSubscribeMulti_InnerJoinWhereColumnComparisonRejected(t *testing.T) {
	conn := testConnDirect(nil)
	executor := &mockSubExecutor{}
	sl := exactIdentifierJoinSchema()

	const sqlText = "SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE t.id = s.id"
	handleSubscribeMulti(context.Background(), conn, &SubscribeMultiMsg{
		RequestID:    770,
		QueryID:      771,
		QueryStrings: []string{"SELECT * FROM t WHERE id = 1", sqlText},
	}, executor, sl)

	requireSubscriptionError(t, conn, 770, 771, "join WHERE column comparisons not supported, executing: `"+sqlText+"`")
	requireNoSubscriptionRegistration(t, executor)
}

func TestHandleOneOffQuery_CrossJoinWhereBoolExpressionsRejected(t *testing.T) {
	cases := []string{
		"SELECT t.* FROM t JOIN s WHERE FALSE",
		"SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 OR t.id = 1",
	}
	for _, sqlText := range cases {
		t.Run(sqlText, func(t *testing.T) {
			conn := testConnDirect(nil)
			sl := exactIdentifierJoinSchema()
			stateAccess := &mockStateAccess{snap: &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}}

			handleOneOffQuery(context.Background(), conn, &OneOffQueryMsg{
				MessageID:   []byte{0xB1},
				QueryString: sqlText,
			}, stateAccess, sl)

			requireOneOffError(t, conn, "cross join WHERE only supports qualified column equality")
		})
	}
}
