// Package store implements Shunter's in-process committed-state, transaction,
// table, and index storage.
//
// Store is a runtime implementation package. Application code should prefer the
// root shunter runtime APIs, contract JSON, generated clients, or protocol
// surfaces unless it is intentionally working on Shunter internals.
package store
