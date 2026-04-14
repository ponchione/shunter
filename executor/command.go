package executor

import "github.com/ponchione/shunter/types"

// ExecutorCommand is the interface for all executor inbox commands.
type ExecutorCommand interface {
	isExecutorCommand()
}

// CallReducerCmd requests a reducer invocation.
type CallReducerCmd struct {
	Request    ReducerRequest
	ResponseCh chan<- ReducerResponse
}

func (CallReducerCmd) isExecutorCommand() {}

// OnConnectCmd is an internal command delivered by the protocol layer when a
// client connection is being admitted (SPEC-003 §10.3). It inserts the
// `sys_clients` row and runs the optional OnConnect lifecycle reducer inside
// a single transaction. A failed reducer rolls back the entire transaction.
type OnConnectCmd struct {
	ConnID     types.ConnectionID
	Identity   types.Identity
	ResponseCh chan<- ReducerResponse
}

func (OnConnectCmd) isExecutorCommand() {}

// OnDisconnectCmd is an internal command delivered by the protocol layer after
// the client is considered gone (SPEC-003 §10.4). Disconnect cannot be
// vetoed: if the OnDisconnect reducer fails, a cleanup transaction still
// deletes the `sys_clients` row.
type OnDisconnectCmd struct {
	ConnID     types.ConnectionID
	Identity   types.Identity
	ResponseCh chan<- ReducerResponse
}

func (OnDisconnectCmd) isExecutorCommand() {}
