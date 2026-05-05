package protocol

import (
	"context"
)

var lifecycleReducerNames = map[string]bool{
	"OnConnect":    true,
	"OnDisconnect": true,
}

// handleCallReducer submits an external reducer call and sends the resulting
// TransactionUpdate to the caller.
func handleCallReducer(
	ctx context.Context,
	conn *Conn,
	msg *CallReducerMsg,
	executor ExecutorInbox,
) {
	if lifecycleReducerNames[msg.ReducerName] {
		sendSyntheticFailure(conn, msg, "lifecycle reducer cannot be called externally")
		recordProtocolMessage(conn.Observer, "call_reducer", "executor_rejected")
		return
	}

	respCh := make(chan TransactionUpdate, 1)
	if err := executor.CallReducer(ctx, CallReducerRequest{
		ConnID:              conn.ID,
		Identity:            conn.Identity,
		Principal:           conn.Principal.Copy(),
		RequestID:           msg.RequestID,
		ReducerName:         msg.ReducerName,
		Args:                msg.Args,
		Permissions:         append([]string(nil), conn.Permissions...),
		AllowAllPermissions: conn.AllowAllPermissions,
		Flags:               msg.Flags,
		ResponseCh:          respCh,
		Done:                conn.closed,
	}); err != nil {
		sendSyntheticFailure(conn, msg, "executor unavailable: "+err.Error())
		recordProtocolMessage(conn.Observer, "call_reducer", "executor_rejected")
		return
	}
	recordProtocolMessage(conn.Observer, "call_reducer", "ok")
	watchReducerResponse(conn, respCh)
}

// sendSyntheticFailure delivers a heavy `TransactionUpdate` with
// `StatusFailed` for pre-acceptance rejections that never reach the
// executor commit seam. TxID equivalent metadata is zero — no
// transaction was opened — and numeric fields follow the outcome-model stub
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
