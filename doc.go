// Package shunter provides a Go-native hosted runtime for stateful realtime
// applications.
//
// Applications define a Module with tables, reducers, lifecycle hooks, declared
// reads, visibility filters, and metadata. Build validates the module and
// returns a Runtime that owns committed state, durability, reducer execution,
// subscription fanout, protocol serving, and contract/schema export.
//
// The root package is the app-facing integration surface. Lower-level packages
// such as schema, store, executor, subscription, protocol, auth, and commitlog
// implement the runtime subsystems and are available when callers need more
// specialized control.
package shunter
