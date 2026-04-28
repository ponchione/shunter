package shunter

import (
	"context"
	"crypto/sha256"
	"errors"
	"iter"

	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// ReducerStatus describes the outcome of a local reducer execution.
type ReducerStatus = executor.ReducerStatus

const (
	// StatusCommitted means the reducer committed successfully.
	StatusCommitted = executor.StatusCommitted
	// StatusFailedUser means the reducer returned a user error or failed a user-level commit constraint.
	StatusFailedUser = executor.StatusFailedUser
	// StatusFailedPanic means the reducer panicked and the transaction was rolled back.
	StatusFailedPanic = executor.StatusFailedPanic
	// StatusFailedInternal means the executor failed the call before user commit semantics completed.
	StatusFailedInternal = executor.StatusFailedInternal
)

// ReducerResult is the result of a local reducer execution.
type ReducerResult = executor.ReducerResponse

// ErrLocalReadNilCallback reports that Runtime.Read was called without a read callback.
var ErrLocalReadNilCallback = errors.New("shunter: local read callback must not be nil")

// LocalReadView is the callback-scoped read-only view exposed by Runtime.Read.
type LocalReadView interface {
	TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue]
	GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool)
	RowCount(tableID schema.TableID) int
}

// ReducerCallOption configures a local reducer call.
type ReducerCallOption func(*reducerCallOptions)

type reducerCallOptions struct {
	caller    types.CallerContext
	requestID uint32
}

var defaultLocalIdentity = types.Identity(sha256.Sum256([]byte("shunter local runtime caller")))

// WithRequestID attaches a caller-chosen request identifier to the local reducer call.
func WithRequestID(requestID uint32) ReducerCallOption {
	return func(opts *reducerCallOptions) {
		opts.requestID = requestID
	}
}

// WithIdentity sets the local caller identity for the reducer call.
func WithIdentity(identity types.Identity) ReducerCallOption {
	return func(opts *reducerCallOptions) {
		opts.caller.Identity = identity
	}
}

// WithConnectionID sets the local caller connection identifier for the reducer call.
func WithConnectionID(connID types.ConnectionID) ReducerCallOption {
	return func(opts *reducerCallOptions) {
		opts.caller.ConnectionID = connID
	}
}

// CallReducer invokes a reducer through the runtime-owned executor and waits for its result.
func (r *Runtime) CallReducer(ctx context.Context, reducerName string, args []byte, opts ...ReducerCallOption) (ReducerResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ReducerResult{}, err
	}

	exec, err := r.readyExecutor()
	if err != nil {
		return ReducerResult{}, err
	}

	callOpts := reducerCallOptions{
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

	responseCh := make(chan executor.ReducerResponse, 1)
	cmd := executor.CallReducerCmd{
		Request: executor.ReducerRequest{
			ReducerName: reducerName,
			Args:        args,
			Caller:      callOpts.caller,
			RequestID:   callOpts.requestID,
			Source:      executor.CallSourceExternal,
		},
		ResponseCh: responseCh,
	}
	if err := exec.SubmitWithContext(ctx, cmd); err != nil {
		return ReducerResult{}, err
	}

	select {
	case res := <-responseCh:
		return res, nil
	case <-ctx.Done():
		return ReducerResult{}, ctx.Err()
	}
}

func (r *Runtime) readyExecutor() (*executor.Executor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.readyLocked(); err != nil {
		return nil, err
	}
	if r.executor == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.executor, nil
}

// Read acquires a committed snapshot, passes a callback-scoped read view to fn,
// and closes the snapshot before returning.
func (r *Runtime) Read(ctx context.Context, fn func(LocalReadView) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return ErrLocalReadNilCallback
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	state, err := r.readyState()
	if err != nil {
		return err
	}
	snapshot := state.Snapshot()
	defer snapshot.Close()

	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(snapshot)
}

func (r *Runtime) readyState() (*store.CommittedState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.readyLocked(); err != nil {
		return nil, err
	}
	if r.state == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.state, nil
}

func (r *Runtime) readyLocked() error {
	if r.stateName == RuntimeStateStarting {
		return ErrRuntimeStarting
	}
	if r.stateName == RuntimeStateClosing || r.stateName == RuntimeStateClosed {
		return ErrRuntimeClosed
	}
	if r.stateName != RuntimeStateReady || !r.ready.Load() {
		return ErrRuntimeNotReady
	}
	return nil
}
