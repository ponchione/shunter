package protocol

import "context"

var lifecycleReducerNames = map[string]bool{
	"OnConnect":    true,
	"OnDisconnect": true,
}

func handleCallReducer(
	ctx context.Context,
	conn *Conn,
	msg *CallReducerMsg,
	executor ExecutorInbox,
) {
	if lifecycleReducerNames[msg.ReducerName] {
		sendError(conn, ReducerCallResult{
			RequestID: msg.RequestID,
			Status:    3, // not_found
			Error:     "lifecycle reducer cannot be called externally",
		})
		return
	}

	respCh := make(chan ReducerCallResult, 1)
	if err := executor.CallReducer(ctx, CallReducerRequest{
		ConnID:      conn.ID,
		Identity:    conn.Identity,
		RequestID:   msg.RequestID,
		ReducerName: msg.ReducerName,
		Args:        msg.Args,
		ResponseCh:  respCh,
	}); err != nil {
		sendError(conn, ReducerCallResult{
			RequestID: msg.RequestID,
			Status:    3,
			Error:     "executor unavailable: " + err.Error(),
		})
		return
	}
}
