package types

import (
	"iter"
	"time"
)

// ScheduleID identifies a scheduled reducer entry.
type ScheduleID uint64

// ReducerDB is the reducer-facing transactional database surface.
// It exposes the operations reducers may perform synchronously during
// execution without leaking the concrete store implementation through
// `ReducerContext`.
type ReducerDB interface {
	Insert(tableID uint32, row ProductValue) (RowID, error)
	Delete(tableID uint32, rowID RowID) error
	Update(tableID uint32, rowID RowID, newRow ProductValue) (RowID, error)
	GetRow(tableID uint32, rowID RowID) (ProductValue, bool)
	ScanTable(tableID uint32) iter.Seq2[RowID, ProductValue]
	SeekIndex(tableID uint32, indexID uint32, key ...Value) iter.Seq2[RowID, ProductValue]
	SeekIndexRange(tableID uint32, indexID uint32, lower, upper IndexBound) iter.Seq2[RowID, ProductValue]
	Underlying() any
}

// ReducerScheduler is the reducer-facing scheduling surface.
type ReducerScheduler interface {
	Schedule(reducerName string, args []byte, at time.Time) (ScheduleID, error)
	ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error)
	Cancel(id ScheduleID) (bool, error)
}

// ReducerHandler is the raw runtime signature for all reducers.
type ReducerHandler func(ctx *ReducerContext, argBSATN []byte) ([]byte, error)

// ReducerContext is the execution context passed to a reducer.
// Valid only during synchronous reducer invocation on the executor goroutine.
// Reducers must not retain it after return, use it from another goroutine, or
// perform blocking network/disk/RPC I/O while holding the executor.
type ReducerContext struct {
	ReducerName string
	Caller      CallerContext
	DB          ReducerDB
	Scheduler   ReducerScheduler
}

// AuthPrincipal carries normalized external-auth claim data for a reducer
// invocation. Shunter identity remains the stable runtime identity; this
// principal is optional application context derived from a validated token or
// supplied by an app-owned local adapter.
type AuthPrincipal struct {
	Issuer      string
	Subject     string
	Audience    []string
	Permissions []string
}

// Copy returns a detached copy of p.
func (p AuthPrincipal) Copy() AuthPrincipal {
	p.Audience = append([]string(nil), p.Audience...)
	p.Permissions = append([]string(nil), p.Permissions...)
	return p
}

// CallerContext captures the identity and timing of a reducer invocation.
type CallerContext struct {
	Identity            Identity
	ConnectionID        ConnectionID
	Principal           AuthPrincipal
	Timestamp           time.Time
	Permissions         []string
	AllowAllPermissions bool
}

// Copy returns a detached copy of c.
func (c CallerContext) Copy() CallerContext {
	c.Principal = c.Principal.Copy()
	c.Permissions = append([]string(nil), c.Permissions...)
	return c
}
