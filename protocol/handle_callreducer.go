package protocol

import (
	"context"
)

var lifecycleReducerNames = map[string]bool{
	"OnConnect":    true,
	"OnDisconnect": true,
}

// handleCallReducer admits an externally-issued CallReducer, hands it
// to the executor through the inbox seam, and waits for the resulting
// heavy `TransactionUpdate` before returning it to the caller's
// outbound channel. Pre-acceptance rejections (lifecycle-reducer-name
// collision, executor-unavailable) are synthesized as heavy
// `TransactionUpdate` envelopes with `StatusFailed` — see the Phase 1.5
// outcome-model decision doc.
func handleCallReducer(
	ctx context.Context,
	conn *Conn,
	msg *CallReducerMsg,
	executor ExecutorInbox,
) {
	if lifecycleReducerNames[msg.ReducerName] {
		sendSyntheticFailure(conn, msg, "lifecycle reducer cannot be called externally")
		return
	}

	respCh := make(chan TransactionUpdate, 1)
	if err := executor.CallReducer(ctx, CallReducerRequest{
		ConnID:      conn.ID,
		Identity:    conn.Identity,
		RequestID:   msg.RequestID,
		ReducerName: msg.ReducerName,
		Args:        msg.Args,
		ResponseCh:  respCh,
	}); err != nil {
		sendSyntheticFailure(conn, msg, "executor unavailable: "+err.Error())
		return
	}
	watchReducerResponse(conn, respCh)
}

// sendSyntheticFailure delivers a heavy `TransactionUpdate` with
// `StatusFailed` for pre-acceptance rejections that never reach the
// executor commit seam. TxID equivalent metadata is zero — no
// transaction was opened — and numeric fields follow the Phase 1.5 stub
// policy.
func sendSyntheticFailure(conn *Conn, msg *CallReducerMsg, errText string) {
	tu := TransactionUpdate{
		Status:             StatusFailed{Error: errText},
		CallerIdentity:     conn.Identity,
		CallerConnectionID: conn.ID,
		ReducerCall: ReducerCallInfo{
			ReducerName: msg.ReducerName,
			Args:        msg.Args,
			RequestID:   msg.RequestID,
		},
	}
	sender := connOnlySender{conn: conn}
	if err := sender.SendTransactionUpdate(conn.ID, &tu); err != nil {
		logReducerDeliveryError(conn, msg.RequestID, err)
	}
}
