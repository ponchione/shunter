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
	// StatusFailedPermission means the caller lacked required reducer permissions.
	StatusFailedPermission = executor.StatusFailedPermission
)

// ReducerResult is the result of a local reducer execution.
type ReducerResult = executor.ReducerResponse

// ReducerDB is the reducer-facing transactional database surface.
type ReducerDB = types.ReducerDB

// Value is a Shunter cell value.
type Value = types.Value

// ProductValue is an ordered row value.
type ProductValue = types.ProductValue

// AuthPrincipal carries normalized external-auth claim data for reducer calls.
type AuthPrincipal = types.AuthPrincipal

// RowID identifies a row within a table.
type RowID = types.RowID

// TxID identifies a committed transaction.
type TxID = types.TxID

// IndexBound represents one endpoint of an indexed range read.
type IndexBound = store.Bound

// IndexKey is an ordered tuple of Values used as an index key.
type IndexKey = store.IndexKey

// NewIndexKey constructs an IndexKey from parts.
func NewIndexKey(parts ...types.Value) IndexKey {
	return store.NewIndexKey(parts...)
}

// UnboundedLow constructs an unbounded lower endpoint for an indexed range.
func UnboundedLow() IndexBound {
	return store.UnboundedLow()
}

// UnboundedHigh constructs an unbounded upper endpoint for an indexed range.
func UnboundedHigh() IndexBound {
	return store.UnboundedHigh()
}

// Inclusive constructs an inclusive index range endpoint.
func Inclusive(value types.Value) IndexBound {
	return store.Inclusive(value)
}

// Exclusive constructs an exclusive index range endpoint.
func Exclusive(value types.Value) IndexBound {
	return store.Exclusive(value)
}

var (
	// ErrLocalReadNilCallback reports that Runtime.Read was called without a read callback.
	ErrLocalReadNilCallback = errors.New("shunter: local read callback must not be nil")
	// ErrPermissionDenied reports that a caller lacks required permissions.
	ErrPermissionDenied = executor.ErrPermissionDenied
)

// LocalReadView is the callback-scoped read-only view exposed by Runtime.Read.
type LocalReadView interface {
	TableScan(id schema.TableID) iter.Seq2[types.RowID, types.ProductValue]
	GetRow(tableID schema.TableID, rowID types.RowID) (types.ProductValue, bool)
	SeekIndex(tableID schema.TableID, indexID schema.IndexID, key ...types.Value) iter.Seq2[types.RowID, types.ProductValue]
	SeekIndexRange(tableID schema.TableID, indexID schema.IndexID, lower, upper IndexBound) iter.Seq2[types.RowID, types.ProductValue]
	RowCount(tableID schema.TableID) int
}

// ReducerCallOption configures a local reducer call.
type ReducerCallOption func(*reducerCallOptions)

type reducerCallOptions struct {
	caller         types.CallerContext
	requestID      uint32
	permissionsSet bool
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

// WithAuthPrincipal sets generic external-auth principal data for the local
// reducer call without requiring a raw JWT.
func WithAuthPrincipal(principal types.AuthPrincipal) ReducerCallOption {
	return func(opts *reducerCallOptions) {
		opts.caller.Principal = principal.Copy()
	}
}

// WithPermissions sets the local caller permission tags for the reducer call.
func WithPermissions(permissions ...string) ReducerCallOption {
	return func(opts *reducerCallOptions) {
		opts.caller.Permissions = append([]string(nil), permissions...)
		opts.caller.AllowAllPermissions = false
		opts.permissionsSet = true
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
	if !callOpts.permissionsSet && r.buildConfig.AuthMode == AuthModeDev {
		callOpts.caller.AllowAllPermissions = true
	}

	responseCh := make(chan executor.ReducerResponse, 1)
	cmd := executor.CallReducerCmd{
		Request: executor.ReducerRequest{
			ReducerName: reducerName,
			Args:        append([]byte(nil), args...),
			Caller:      callOpts.caller.Copy(),
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

// WaitUntilDurable blocks until txID is confirmed durable on disk.
func (r *Runtime) WaitUntilDurable(ctx context.Context, txID types.TxID) error {
	if txID == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	if r.durableTxID >= txID {
		r.mu.Unlock()
		return nil
	}
	if err := r.readyLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	durability := r.durability
	if durability == nil {
		r.mu.Unlock()
		return ErrRuntimeNotReady
	}
	wait := durability.WaitUntilDurable(txID)
	r.mu.Unlock()

	select {
	case <-wait:
		if err := durability.FatalError(); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
	if r.durabilityFatalErr != nil {
		return ErrRuntimeNotReady
	}
	if r.durability != nil {
		if err := r.durability.FatalError(); err != nil {
			r.durabilityFatalErr = err
			return ErrRuntimeNotReady
		}
	}
	if r.executorFatal || r.executorFatalErr != nil || (r.executor != nil && r.executor.Fatal()) {
		return ErrRuntimeNotReady
	}
	if r.stateName != RuntimeStateReady || !r.ready.Load() {
		return ErrRuntimeNotReady
	}
	return nil
}
