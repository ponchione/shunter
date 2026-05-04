package protocol

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ponchione/shunter/schema"
)

func TestProtocolV1ClientGoldenWireFixtures(t *testing.T) {
	fixtures := []protocolGoldenFixture{
		{
			name: "client/subscribe_single",
			msg: SubscribeSingleMsg{
				RequestID:   0x01020304,
				QueryID:     0x05060708,
				QueryString: "SELECT * FROM users",
			},
			wantHex: "0104030201080706051300000053454c454354202a2046524f4d207573657273",
		},
		{
			name:    "client/unsubscribe_single",
			msg:     UnsubscribeSingleMsg{RequestID: 0x11121314, QueryID: 0x21222324},
			wantHex: "021413121124232221",
		},
		{
			name: "client/call_reducer",
			msg: CallReducerMsg{
				ReducerName: "send",
				Args:        []byte{0xaa, 0xbb},
				RequestID:   0x31323334,
				Flags:       CallReducerFlagsNoSuccessNotify,
			},
			wantHex: "030400000073656e6402000000aabb3433323101",
		},
		{
			name: "client/one_off_query",
			msg: OneOffQueryMsg{
				MessageID:   []byte{0x01, 0x02, 0x03},
				QueryString: "SELECT id FROM users",
			},
			wantHex: "04030000000102031400000053454c4543542069642046524f4d207573657273",
		},
		{
			name: "client/subscribe_multi",
			msg: SubscribeMultiMsg{
				RequestID: 0x41424344,
				QueryID:   0x51525354,
				QueryStrings: []string{
					"SELECT * FROM users",
					"SELECT * FROM rooms WHERE id = 7",
				},
			},
			wantHex: "054443424154535251020000001300000053454c454354202a2046524f4d2075736572732000000053454c454354202a2046524f4d20726f6f6d73205748455245206964203d2037",
		},
		{
			name:    "client/unsubscribe_multi",
			msg:     UnsubscribeMultiMsg{RequestID: 0x61626364, QueryID: 0x71727374},
			wantHex: "066463626174737271",
		},
		{
			name:    "client/declared_query",
			msg:     DeclaredQueryMsg{MessageID: []byte{0x09, 0x08}, Name: "recent_users"},
			wantHex: "070200000009080c000000726563656e745f7573657273",
		},
		{
			name: "client/subscribe_declared_view",
			msg: SubscribeDeclaredViewMsg{
				RequestID: 0x81828384,
				QueryID:   0x91929394,
				Name:      "live_users",
			},
			wantHex: "0884838281949392910a0000006c6976655f7573657273",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frame, err := EncodeClientMessage(fixture.msg)
			if err != nil {
				t.Fatalf("EncodeClientMessage: %v", err)
			}
			assertGoldenFrame(t, fixture, frame)

			_, decoded, err := DecodeClientMessage(frame)
			if err != nil {
				t.Fatalf("DecodeClientMessage: %v", err)
			}
			assertGoldenDecoded(t, fixture.msg, decoded)
		})
	}
}

func TestProtocolV1ServerGoldenWireFixtures(t *testing.T) {
	rowList := EncodeRowList([][]byte{{0x01, 0x02}, {0x03}})
	update := []SubscriptionUpdate{{
		QueryID:   0x01020304,
		TableName: "users",
		Inserts:   rowList,
		Deletes:   []byte{0x04, 0x05},
	}}
	tableID := schema.TableID(0x61626364)
	requestID := uint32(0x41424344)
	queryID := uint32(0x51525354)
	oneOffErr := "bad query"

	fixtures := []protocolGoldenFixture{
		{
			name: "server/identity_token",
			msg: IdentityToken{
				Identity:     sequential32(0x00),
				Token:        "tok",
				ConnectionID: sequential16(0xf0),
			},
			wantHex: "01000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f03000000746f6bf0f1f2f3f4f5f6f7f8f9fafbfcfdfeff",
		},
		{
			name: "server/subscribe_single_applied",
			msg: SubscribeSingleApplied{
				RequestID:                        0x01020304,
				TotalHostExecutionDurationMicros: 0x0102030405060708,
				QueryID:                          0x11121314,
				TableName:                        "users",
				Rows:                             rowList,
			},
			wantHex: "02040302010807060504030201141312110500000075736572730f000000020000000200000001020100000003",
		},
		{
			name: "server/unsubscribe_single_applied",
			msg: UnsubscribeSingleApplied{
				RequestID:                        0x21222324,
				TotalHostExecutionDurationMicros: 0x1112131415161718,
				QueryID:                          0x31323334,
				HasRows:                          true,
				Rows:                             rowList,
			},
			wantHex: "0324232221181716151413121134333231010f000000020000000200000001020100000003",
		},
		{
			name: "server/subscription_error",
			msg: SubscriptionError{
				TotalHostExecutionDurationMicros: 0x0102030405060708,
				RequestID:                        &requestID,
				QueryID:                          &queryID,
				TableID:                          &tableID,
				Error:                            "denied",
			},
			wantHex: "0408070605040302010144434241015453525101646362610600000064656e696564",
		},
		{
			name: "server/transaction_update_committed",
			msg: TransactionUpdate{
				Status:                     StatusCommitted{Update: update},
				Timestamp:                  0x0102030405060708,
				CallerIdentity:             sequential32(0x20),
				CallerConnectionID:         sequential16(0xa0),
				ReducerCall:                ReducerCallInfo{ReducerName: "send", ReducerID: 0x11121314, Args: []byte{0xaa, 0xbb}, RequestID: 0x21222324},
				TotalHostExecutionDuration: 0x3132333435363738,
			},
			wantHex: "050001000000040302010500000075736572730f0000000200000002000000010201000000030200000004050807060504030201202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3fa0a1a2a3a4a5a6a7a8a9aaabacadaeaf0400000073656e641413121102000000aabb242322213837363534333231",
		},
		{
			name: "server/transaction_update_failed",
			msg: TransactionUpdate{
				Status:      StatusFailed{Error: "boom"},
				Timestamp:   0x1112131415161718,
				ReducerCall: ReducerCallInfo{ReducerName: "send", RequestID: 0x21222324},
			},
			wantHex: "050104000000626f6f6d18171615141312110000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000400000073656e640000000000000000242322210000000000000000",
		},
		{
			name:    "server/transaction_update_light",
			msg:     TransactionUpdateLight{RequestID: 0x31323334, Update: update},
			wantHex: "083433323101000000040302010500000075736572730f000000020000000200000001020100000003020000000405",
		},
		{
			name: "server/one_off_query_response_success",
			msg: OneOffQueryResponse{
				MessageID:                  []byte{0x01, 0x02},
				Tables:                     []OneOffTable{{TableName: "users", Rows: rowList}},
				TotalHostExecutionDuration: 0x1112131415161718,
			},
			wantHex: "0602000000010200010000000500000075736572730f0000000200000002000000010201000000031817161514131211",
		},
		{
			name: "server/one_off_query_response_error",
			msg: OneOffQueryResponse{
				MessageID:                  []byte{0x03, 0x04},
				Error:                      &oneOffErr,
				TotalHostExecutionDuration: 0x2122232425262728,
			},
			wantHex: "060200000003040109000000626164207175657279000000002827262524232221",
		},
		{
			name: "server/subscribe_multi_applied",
			msg: SubscribeMultiApplied{
				RequestID:                        0x41424344,
				TotalHostExecutionDurationMicros: 0x5152535455565758,
				QueryID:                          0x61626364,
				Update:                           update,
			},
			wantHex: "094443424158575655545352516463626101000000040302010500000075736572730f000000020000000200000001020100000003020000000405",
		},
		{
			name: "server/unsubscribe_multi_applied",
			msg: UnsubscribeMultiApplied{
				RequestID:                        0x71727374,
				TotalHostExecutionDurationMicros: 0x8182838485868788,
				QueryID:                          0x91929394,
				Update:                           update,
			},
			wantHex: "0a7473727188878685848382819493929101000000040302010500000075736572730f000000020000000200000001020100000003020000000405",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			frame, err := EncodeServerMessage(fixture.msg)
			if err != nil {
				t.Fatalf("EncodeServerMessage: %v", err)
			}
			assertGoldenFrame(t, fixture, frame)

			_, decoded, err := DecodeServerMessage(frame)
			if err != nil {
				t.Fatalf("DecodeServerMessage: %v", err)
			}
			assertGoldenDecoded(t, fixture.msg, decoded)
		})
	}
}

type protocolGoldenFixture struct {
	name    string
	msg     any
	wantHex string
}

func assertGoldenFrame(t *testing.T, fixture protocolGoldenFixture, frame []byte) {
	t.Helper()
	got := hex.EncodeToString(frame)
	if fixture.wantHex == "" {
		t.Fatalf("missing golden fixture for %s: %s", fixture.name, got)
	}
	if got != fixture.wantHex {
		t.Fatalf("%s golden frame mismatch\n got: %s\nwant: %s", fixture.name, got, fixture.wantHex)
	}
}

func assertGoldenDecoded(t *testing.T, want, got any) {
	t.Helper()
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		t.Fatalf("decoded type = %T, want %T", got, want)
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Fatalf("decoded value mismatch (-want +got):\n%s", diff)
	}
}

func sequential32(start byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}

func sequential16(start byte) [16]byte {
	var out [16]byte
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}
