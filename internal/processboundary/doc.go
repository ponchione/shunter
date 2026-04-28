// Package processboundary records the experimental invocation contract that an
// out-of-process module runner would have to satisfy.
//
// It deliberately does not start, supervise, or route to child processes.
// V2-G uses this package as a gate: the boundary can describe calls, failures,
// lifecycle ordering, and unsupported transaction semantics without replacing
// the in-process runtime model.
package processboundary
