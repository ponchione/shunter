package subscription

import "github.com/ponchione/shunter/types"

// queryState holds bookkeeping for one registered query. Shared by every
// subscriber with the same QueryHash (SPEC-004 §7.4 deduplication).
type queryState struct {
	hash       QueryHash
	predicate  Predicate // v1 executable plan == the validated predicate itself
	projection []ProjectionColumn
	aggregate  *Aggregate
	orderBy    []OrderByColumn
	limit      *uint64
	offset     *uint64
	// sqlText is the original Single-subscribe SQL used for final-eval errors.
	sqlText string
	// subscribers is keyed first by connection and then by subscription ID so
	// one connection may hold multiple independent subscriptions to the same
	// query hash.
	subscribers map[types.ConnectionID]map[types.SubscriptionID]subscriptionDeliveryMeta
	// refCount counts total attached subscriptions, not distinct connections.
	refCount int
}

type subscriptionDeliveryMeta struct {
	RequestID uint32
	// QueryID is the client-chosen subscription-set identifier to stamp on
	// fanout updates for this internal SubscriptionID.
	QueryID uint32
}

type subscriptionRef struct {
	connID types.ConnectionID
	subID  types.SubscriptionID
}

// queryRegistry maintains the three-way index (hash → state, sub → hash,
// conn → subs) needed for O(1) lookups on register/unregister/disconnect.
type queryRegistry struct {
	byHash map[QueryHash]*queryState
	bySub  map[subscriptionRef]QueryHash
	byConn map[types.ConnectionID][]types.SubscriptionID
}

func newQueryRegistry() *queryRegistry {
	return &queryRegistry{
		byHash: make(map[QueryHash]*queryState),
		bySub:  make(map[subscriptionRef]QueryHash),
		byConn: make(map[types.ConnectionID][]types.SubscriptionID),
	}
}

// createQueryState creates a new query state entry. Panics if hash already exists.
func (r *queryRegistry) createQueryState(hash QueryHash, pred Predicate, projection []ProjectionColumn, aggregate *Aggregate, orderBy []OrderByColumn, limit *uint64, offset *uint64) *queryState {
	if _, ok := r.byHash[hash]; ok {
		panic("subscription: createQueryState on existing hash")
	}
	qs := &queryState{
		hash:        hash,
		predicate:   pred,
		projection:  copySlice(projection),
		aggregate:   copyAggregate(aggregate),
		orderBy:     copySlice(orderBy),
		limit:       copyRowLimit(limit),
		offset:      copyRowOffset(offset),
		subscribers: make(map[types.ConnectionID]map[types.SubscriptionID]subscriptionDeliveryMeta),
	}
	r.byHash[hash] = qs
	return qs
}

// removeQueryState removes the query entry. Callers must have ensured all
// subscribers are gone first.
func (r *queryRegistry) removeQueryState(hash QueryHash) {
	delete(r.byHash, hash)
}

// addSubscriber attaches a client to an existing query state.
func (r *queryRegistry) addSubscriber(hash QueryHash, connID types.ConnectionID, subID types.SubscriptionID, requestID uint32, queryID uint32) {
	qs, ok := r.byHash[hash]
	if !ok {
		panic("subscription: addSubscriber on unknown hash")
	}
	ref := subscriptionRef{connID: connID, subID: subID}
	if _, exists := r.bySub[ref]; exists {
		return
	}
	perConn, ok := qs.subscribers[connID]
	if !ok {
		perConn = make(map[types.SubscriptionID]subscriptionDeliveryMeta)
		qs.subscribers[connID] = perConn
	}
	perConn[subID] = subscriptionDeliveryMeta{RequestID: requestID, QueryID: queryID}
	r.bySub[ref] = hash
	r.byConn[connID] = append(r.byConn[connID], subID)
	qs.refCount++
}

// removeSubscriber detaches a client from a subscription.
// Returns the query hash it was attached to and whether this was the last
// subscriber for that query. ok=false when the subID is unknown.
func (r *queryRegistry) removeSubscriber(connID types.ConnectionID, subID types.SubscriptionID) (hash QueryHash, last bool, ok bool) {
	ref := subscriptionRef{connID: connID, subID: subID}
	h, ok := r.bySub[ref]
	if !ok {
		return QueryHash{}, false, false
	}
	qs := r.byHash[h]
	if qs == nil {
		return QueryHash{}, false, false
	}
	if perConn, ok := qs.subscribers[connID]; ok {
		delete(perConn, subID)
		if len(perConn) == 0 {
			delete(qs.subscribers, connID)
		}
	}
	if qs.refCount > 0 {
		qs.refCount--
	}
	delete(r.bySub, ref)

	// Trim the byConn slice.
	if subs, ok := r.byConn[connID]; ok {
		for i, s := range subs {
			if s == subID {
				r.byConn[connID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(r.byConn[connID]) == 0 {
			delete(r.byConn, connID)
		}
	}

	return h, qs.refCount == 0, true
}

// getQuery returns the query state, or nil if not present.
func (r *queryRegistry) getQuery(hash QueryHash) *queryState {
	return r.byHash[hash]
}

// subscriptionsForConn returns a copy of the subscription IDs for the given
// connection. Safe to iterate while removing entries.
func (r *queryRegistry) subscriptionsForConn(connID types.ConnectionID) []types.SubscriptionID {
	subs := r.byConn[connID]
	if len(subs) == 0 {
		return nil
	}
	out := make([]types.SubscriptionID, len(subs))
	copy(out, subs)
	return out
}

// hasActive reports whether any query is registered.
func (r *queryRegistry) hasActive() bool { return len(r.byHash) > 0 }

// hashForSub returns the QueryHash a given (conn, sub) is attached to.
func (r *queryRegistry) hashForSub(connID types.ConnectionID, subID types.SubscriptionID) (QueryHash, bool) {
	h, ok := r.bySub[subscriptionRef{connID: connID, subID: subID}]
	return h, ok
}
