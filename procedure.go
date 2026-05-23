package shunter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
)

var (
	// ErrProcedureNotFound reports a call to an undeclared procedure.
	ErrProcedureNotFound = errors.New("shunter: procedure not found")
	// ErrProcedurePermissionDenied reports that a caller lacks required procedure permissions.
	ErrProcedurePermissionDenied = errors.New("shunter: procedure permission denied")
	// ErrProcedurePanicked reports that a procedure panicked.
	ErrProcedurePanicked = errors.New("shunter: procedure panicked")
)

// ProcedureContext is the execution context passed to a procedure. It remains
// valid until the procedure returns or Context is canceled.
type ProcedureContext struct {
	Context       context.Context
	ProcedureName string
	Caller        types.CallerContext
	runtime       *Runtime
}

// CallReducer invokes a reducer as the same caller. The reducer runs on the
// serialized executor only for the duration of that reducer call.
func (c *ProcedureContext) CallReducer(name string, args []byte) (ReducerResult, error) {
	if c == nil || c.runtime == nil {
		return ReducerResult{}, ErrRuntimeNotReady
	}
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return c.runtime.callReducerWithCaller(ctx, name, args, c.Caller)
}

// ProcedureCallOption configures a local procedure call.
type ProcedureCallOption func(*procedureCallOptions)

type procedureCallOptions struct {
	caller         types.CallerContext
	permissionsSet bool
}

// WithProcedureIdentity sets the local caller identity for the procedure call.
func WithProcedureIdentity(identity types.Identity) ProcedureCallOption {
	return func(opts *procedureCallOptions) {
		opts.caller.Identity = identity
	}
}

// WithProcedureConnectionID sets the local caller connection identifier.
func WithProcedureConnectionID(connID types.ConnectionID) ProcedureCallOption {
	return func(opts *procedureCallOptions) {
		opts.caller.ConnectionID = connID
	}
}

// WithProcedureAuthPrincipal sets generic external-auth principal data.
func WithProcedureAuthPrincipal(principal types.AuthPrincipal) ProcedureCallOption {
	return func(opts *procedureCallOptions) {
		opts.caller.Principal = principal.Copy()
	}
}

// WithProcedureCallerPermissions sets local caller permission tags.
func WithProcedureCallerPermissions(permissions ...string) ProcedureCallOption {
	return func(opts *procedureCallOptions) {
		opts.caller.Permissions = append([]string(nil), permissions...)
		opts.caller.AllowAllPermissions = false
		opts.permissionsSet = true
	}
}

// CallProcedure invokes a procedure without entering the reducer executor.
func (r *Runtime) CallProcedure(ctx context.Context, name string, args []byte, opts ...ProcedureCallOption) ([]byte, error) {
	callOpts := procedureCallOptions{
		caller: types.CallerContext{Identity: defaultLocalIdentity},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&callOpts)
		}
	}
	if callOpts.caller.Identity.IsZero() {
		callOpts.caller.Identity = defaultLocalIdentity
	}
	if !callOpts.permissionsSet && r != nil && r.buildConfig.AuthMode == AuthModeDev {
		callOpts.caller.AllowAllPermissions = true
	}
	return r.callProcedureWithCaller(ctx, name, args, callOpts.caller)
}

func (r *Runtime) callProcedureWithCaller(ctx context.Context, name string, args []byte, caller types.CallerContext) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.Lock()
	if err := r.readyLocked(); err != nil {
		r.mu.Unlock()
		return nil, err
	}
	procedure, ok := r.module.lookupProcedure(name)
	r.mu.Unlock()
	if !ok || procedure.Handler == nil {
		return nil, fmt.Errorf("%w: %s", ErrProcedureNotFound, name)
	}
	if missing, denied := types.MissingRequiredPermission(caller, procedure.Permissions.Required); denied {
		return nil, fmt.Errorf("%w: procedure %q missing permission %q", ErrProcedurePermissionDenied, procedure.Name, missing)
	}
	caller = caller.Copy()
	caller.Timestamp = time.Now().UTC()
	pctx := &ProcedureContext{
		Context:       ctx,
		ProcedureName: procedure.Name,
		Caller:        caller,
		runtime:       r,
	}
	var ret []byte
	var err error
	var panicked any
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicked = recovered
			}
		}()
		ret, err = procedure.Handler(pctx, append([]byte(nil), args...))
	}()
	if panicked != nil {
		return nil, fmt.Errorf("%w: %v", ErrProcedurePanicked, panicked)
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), ret...), nil
}

// HandleCallProcedure implements the protocol procedure seam.
func (r *Runtime) HandleCallProcedure(ctx context.Context, conn *protocol.Conn, msg *protocol.CallProcedureMsg) {
	if conn == nil || msg == nil {
		return
	}
	start := time.Now()
	caller := types.CallerContext{
		Identity:            conn.Identity,
		ConnectionID:        conn.ID,
		Principal:           conn.Principal.Copy(),
		Permissions:         append([]string(nil), conn.Permissions...),
		AllowAllPermissions: conn.AllowAllPermissions,
	}
	result, err := r.callProcedureWithCaller(ctx, msg.Name, msg.Args, caller)
	response := protocol.ProcedureResponse{
		MessageID:                  append([]byte(nil), msg.MessageID...),
		Result:                     result,
		TotalHostExecutionDuration: time.Since(start).Microseconds(),
	}
	if response.TotalHostExecutionDuration <= 0 {
		response.TotalHostExecutionDuration = 1
	}
	if err != nil {
		errText := err.Error()
		response.Error = &errText
		response.Result = nil
	}
	if sendErr := r.sendProtocolProcedureMessage(conn, response); sendErr != nil {
		r.logProtocolProcedureSendError(sendErr)
	}
}

func (r *Runtime) sendProtocolProcedureMessage(conn *protocol.Conn, msg protocol.ProcedureResponse) error {
	if conn == nil {
		return nil
	}
	sender, err := r.readyProtocolSender()
	if err != nil {
		return err
	}
	return sender.Send(conn.ID, msg)
}

func (r *Runtime) logProtocolProcedureSendError(err error) {
	r.observability.LogProtocolProtocolError("call_procedure", "send_failed", err)
}
