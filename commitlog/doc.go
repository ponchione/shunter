// Package commitlog implements Shunter's durable commit log, snapshot, replay,
// and compaction primitives.
//
// Commitlog is a runtime implementation package. Application code should
// prefer the root shunter runtime APIs and operations helpers unless it is
// intentionally working on Shunter internals.
package commitlog
