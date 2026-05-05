package subscription

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// Property tests cover IVM equivalence, pruning safety, and registration cleanup.

// ---------- shared helpers ----------

// bagEqual reports multiset equality on slices of ProductValue using the
// value-level Equal method. Order-insensitive; duplicates count.
func bagEqual(a, b []types.ProductValue) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, pa := range a {
		found := false
		for j, pb := range b {
			if used[j] {
				continue
			}
			if pa.Equal(pb) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// applyDelta returns (base ∪ inserts) \ deletes using multiset semantics: each
// delete removes at most one occurrence of a matching row in base.
func applyDelta(base, inserts, deletes []types.ProductValue) []types.ProductValue {
	out := make([]types.ProductValue, 0, len(base)+len(inserts))
	out = append(out, base...)
	for _, d := range deletes {
		for i, r := range out {
			if r.Equal(d) {
				out = append(out[:i], out[i+1:]...)
				break
			}
		}
	}
	out = append(out, inserts...)
	return out
}

// randomRow builds a 2-column ProductValue (uint64 id, string label). Label is
// derived from id so rows with the same id compare equal.
func randomRow(r *rand.Rand, idMax int) types.ProductValue {
	id := uint64(r.IntN(idMax))
	return types.ProductValue{types.NewUint64(id), types.NewString(fmt.Sprintf("r%d", id))}
}

func randomInserts(r *rand.Rand, n, idMax int) []types.ProductValue {
	out := make([]types.ProductValue, n)
	for i := range out {
		out[i] = randomRow(r, idMax)
	}
	return out
}

// pickRandomRows returns up to n entries from existing without replacement so
// we never pick the same physical row twice per transaction.
func pickRandomRows(r *rand.Rand, existing []types.ProductValue, n int) []types.ProductValue {
	if len(existing) == 0 || n == 0 {
		return nil
	}
	pool := make([]types.ProductValue, len(existing))
	copy(pool, existing)
	r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

// propSchema is the common schema used by property tests. One table with
// (uint64, string) columns and an index on column 0.
func propSchema() *fakeSchema {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	return s
}

func cloneRows(in []types.ProductValue) []types.ProductValue {
	out := make([]types.ProductValue, len(in))
	for i, r := range in {
		copyRow := make(types.ProductValue, len(r))
		copy(copyRow, r)
		out[i] = copyRow
	}
	return out
}

// ---------- IVM invariant (A4.2) ----------

// TestIVMInvariantPropertySingleTable exercises Story 5.4 §13.2 IVM invariant
// for single-table ColEq subscriptions: accumulated incremental deltas on top
// of the pre-commit snapshot must equal a fresh evaluation of the same
// predicate against the post-commit snapshot.
func TestIVMInvariantPropertySingleTable(t *testing.T) {
	const seeds = 50
	for seed := uint64(1); seed <= seeds; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runIVMInvariantIteration(t, seed)
		})
	}
}

func runIVMInvariantIteration(t *testing.T, seed uint64) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))

	s := propSchema()
	idMax := 16

	initRows := randomInserts(r, r.IntN(20)+1, idMax)
	sPre := buildMockCommitted(s, map[TableID][]types.ProductValue{1: initRows})

	target := uint64(r.IntN(idMax))
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(target)}

	inbox := make(chan FanOutMessage, 4)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, sPre)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var rPreRows []types.ProductValue
	for _, u := range res.Update {
		rPreRows = append(rPreRows, u.Inserts...)
	}
	rPre := cloneRows(rPreRows)

	insN := r.IntN(5)
	delN := r.IntN(3)
	inserts := randomInserts(r, insN, idMax)
	deletes := pickRandomRows(r, initRows, delN)
	if len(inserts)+len(deletes) == 0 {
		inserts = randomInserts(r, 1, idMax)
	}
	cs := simpleChangeset(1, inserts, deletes)

	postRows := applyDelta(initRows, inserts, deletes)
	sPost := buildMockCommitted(s, map[TableID][]types.ProductValue{1: postRows})

	mgr.EvalAndBroadcast(types.TxID(2), cs, sPost, PostCommitMeta{})

	var dIns, dDel []types.ProductValue
	select {
	case msg := <-inbox:
		for _, u := range msg.Fanout[types.ConnectionID{1}] {
			dIns = append(dIns, u.Inserts...)
			dDel = append(dDel, u.Deletes...)
		}
	default:
		t.Fatal("expected fanout message, got none")
	}

	incremental := applyDelta(rPre, dIns, dDel)

	mgr2 := NewManager(s, s)
	res2, err := mgr2.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 20, Predicates: []Predicate{pred},
	}, sPost)
	if err != nil {
		t.Fatalf("fresh RegisterSet: %v", err)
	}
	var rFreshRows []types.ProductValue
	for _, u := range res2.Update {
		rFreshRows = append(rFreshRows, u.Inserts...)
	}
	rFresh := cloneRows(rFreshRows)

	if !bagEqual(incremental, rFresh) {
		t.Fatalf("IVM invariant violated\nseed=%d target=%d\ninitRows=%v\ninserts=%v deletes=%v\nrPre=%v\ndIns=%v dDel=%v\nincremental=%v rFresh=%v",
			seed, target, initRows, inserts, deletes, rPre, dIns, dDel, incremental, rFresh)
	}
}

// ---------- Pruning safety (A4.3) ----------

// TestPruningSafetyProperty exercises Story 5.4 §13.2: for a random mix of
// ColEq / ColRange / AllRows subscriptions and a random tx, the manager's
// pruned evaluation must produce the same per-query deltas as a baseline that
// ignores the pruning indexes and evaluates every registered query directly.
func TestPruningSafetyProperty(t *testing.T) {
	const seeds = 30
	for seed := uint64(1); seed <= seeds; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runPruningSafetyIteration(t, seed)
		})
	}
}

func runPruningSafetyIteration(t *testing.T, seed uint64) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, seed^0xC6BC279692B5C323))
	s := propSchema()
	idMax := 16

	initRows := randomInserts(r, r.IntN(20)+5, idMax)
	sPre := buildMockCommitted(s, map[TableID][]types.ProductValue{1: initRows})

	numSubs := r.IntN(8) + 2
	type subEntry struct {
		queryID uint32
		pred    Predicate
	}
	subs := make([]subEntry, numSubs)
	for i := 0; i < numSubs; i++ {
		var p Predicate
		switch r.IntN(3) {
		case 0:
			p = ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(r.IntN(idMax)))}
		case 1:
			lo := uint64(r.IntN(idMax))
			hi := lo + uint64(r.IntN(idMax))
			p = ColRange{Table: 1, Column: 0,
				Lower: Bound{Value: types.NewUint64(lo), Inclusive: true},
				Upper: Bound{Value: types.NewUint64(hi), Inclusive: true}}
		case 2:
			p = AllRows{Table: 1}
		}
		subs[i] = subEntry{
			queryID: uint32(100 + i),
			pred:    p,
		}
	}

	inbox := make(chan FanOutMessage, 1)
	conn := types.ConnectionID{1}
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	for _, e := range subs {
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: conn, QueryID: e.queryID, Predicates: []Predicate{e.pred},
		}, sPre); err != nil {
			t.Fatalf("RegisterSet(%v): %v", e.pred, err)
		}
	}

	inserts := randomInserts(r, r.IntN(4)+1, idMax)
	deletes := pickRandomRows(r, initRows, r.IntN(3))
	cs := simpleChangeset(1, inserts, deletes)
	postRows := applyDelta(initRows, inserts, deletes)
	sPost := buildMockCommitted(s, map[TableID][]types.ProductValue{1: postRows})

	// Pruned path: normal EvalAndBroadcast.
	mgr.EvalAndBroadcast(types.TxID(2), cs, sPost, PostCommitMeta{})
	msg := <-inbox
	pruned := groupPerQueryDeltas(mgr, msg.Fanout[conn])

	// Baseline path: evaluate every registered query, no pruning.
	baseline := evalAllQueries(mgr, cs, sPost)

	if !perQueryDeltasEqual(pruned, baseline) {
		t.Fatalf("pruning safety violated\nseed=%d\ninit=%v\ninserts=%v deletes=%v\npruned=%v\nbaseline=%v",
			seed, initRows, inserts, deletes, pruned, baseline)
	}
}

type deltaBag struct {
	inserts []types.ProductValue
	deletes []types.ProductValue
}

// groupPerQueryDeltas picks one subscriber delta per query hash so fan-out
// duplication does not inflate the baseline comparison.
func groupPerQueryDeltas(mgr *Manager, updates []SubscriptionUpdate) map[QueryHash]deltaBag {
	bySub := map[types.SubscriptionID]deltaBag{}
	for _, u := range updates {
		b := bySub[u.SubscriptionID]
		b.inserts = append(b.inserts, u.Inserts...)
		b.deletes = append(b.deletes, u.Deletes...)
		bySub[u.SubscriptionID] = b
	}
	pickedSub := map[QueryHash]types.SubscriptionID{}
	for subID := range bySub {
		h, ok := hashForSubscriptionID(mgr, subID)
		if !ok {
			continue
		}
		if existing, exists := pickedSub[h]; !exists || subID < existing {
			pickedSub[h] = subID
		}
	}
	out := map[QueryHash]deltaBag{}
	for h, sub := range pickedSub {
		out[h] = bySub[sub]
	}
	return out
}

func hashForSubscriptionID(mgr *Manager, subID types.SubscriptionID) (QueryHash, bool) {
	for h, qs := range mgr.registry.byHash {
		for _, perConn := range qs.subscribers {
			if _, ok := perConn[subID]; ok {
				return h, true
			}
		}
	}
	return QueryHash{}, false
}

// evalAllQueries recomputes per-query deltas without pruning: the candidate
// set is every registered query. Builds the DeltaView once and drives the
// manager's internal evalQuery path, so the only dimension varying from the
// pruned path is candidate selection.
func evalAllQueries(mgr *Manager, cs *store.Changeset, view store.CommittedReadView) map[QueryHash]deltaBag {
	activeCols := mgr.collectDeltaIndexColumns()
	dv := NewDeltaView(view, cs, activeCols)
	out := map[QueryHash]deltaBag{}
	for h, qs := range mgr.registry.byHash {
		updates := mgr.evalQuery(qs, dv)
		var bag deltaBag
		for _, u := range updates {
			bag.inserts = append(bag.inserts, u.Inserts...)
			bag.deletes = append(bag.deletes, u.Deletes...)
		}
		if len(bag.inserts) > 0 || len(bag.deletes) > 0 {
			out[h] = bag
		}
	}
	return out
}

func perQueryDeltasEqual(a, b map[QueryHash]deltaBag) bool {
	if len(a) != len(b) {
		return false
	}
	for h, ba := range a {
		bb, ok := b[h]
		if !ok {
			return false
		}
		if !bagEqual(ba.inserts, bb.inserts) || !bagEqual(ba.deletes, bb.deletes) {
			return false
		}
	}
	return true
}

// ---------- Registration/deregistration symmetry (A4.4) ----------

// TestRegistrationSymmetryProperty exercises Story 5.4 §13.2: after every
// subscription registered in a random sequence is unregistered, the manager's
// query registry and pruning indexes must be fully empty.
func TestRegistrationSymmetryProperty(t *testing.T) {
	const seeds = 30
	for seed := uint64(1); seed <= seeds; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runRegistrationSymmetryIteration(t, seed)
		})
	}
}

func runRegistrationSymmetryIteration(t *testing.T, seed uint64) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, seed^0xD1B54A32D192ED03))
	s := propSchema()
	mgr := NewManager(s, s)

	idMax := 16
	n := r.IntN(12) + 1
	type entry struct {
		connID  types.ConnectionID
		queryID uint32
		pred    Predicate
	}
	entries := make([]entry, n)
	// (connID, queryID) must be unique per connection; use i as the key so
	// RegisterSet cannot collide even when the same connID is reused.
	for i := range entries {
		var p Predicate
		switch r.IntN(3) {
		case 0:
			p = ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(r.IntN(idMax)))}
		case 1:
			lo := uint64(r.IntN(idMax))
			hi := lo + uint64(r.IntN(idMax))
			p = ColRange{Table: 1, Column: 0,
				Lower: Bound{Value: types.NewUint64(lo), Inclusive: true},
				Upper: Bound{Value: types.NewUint64(hi), Inclusive: true}}
		case 2:
			p = AllRows{Table: 1}
		}
		entries[i] = entry{
			connID:  types.ConnectionID{byte(r.IntN(4)), byte(i)},
			queryID: uint32(1000 + i),
			pred:    p,
		}
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: entries[i].connID, QueryID: entries[i].queryID, Predicates: []Predicate{entries[i].pred},
		}, nil); err != nil {
			t.Fatalf("RegisterSet: %v", err)
		}
	}

	order := r.Perm(len(entries))
	for _, idx := range order {
		e := entries[idx]
		if _, err := mgr.UnregisterSet(e.connID, e.queryID, nil); err != nil {
			t.Fatalf("UnregisterSet(%v,%v): %v", e.connID, e.queryID, err)
		}
	}

	if len(mgr.registry.byHash) != 0 {
		t.Fatalf("registry.byHash not empty: %v", mgr.registry.byHash)
	}
	if len(mgr.registry.bySub) != 0 {
		t.Fatalf("registry.bySub not empty: %v", mgr.registry.bySub)
	}
	if len(mgr.registry.byConn) != 0 {
		t.Fatalf("registry.byConn not empty: %v", mgr.registry.byConn)
	}
	if mgr.registry.hasActive() {
		t.Fatalf("registry still active after all unregisters")
	}
	if !pruningIndexesEmpty(mgr.indexes) {
		t.Fatalf("pruning indexes non-empty after all unregisters: %+v", mgr.indexes)
	}
}

// pruningIndexesEmpty peeks into each tier's internal state. All tiers must
// contain zero entries for registration symmetry to hold.
func pruningIndexesEmpty(idx *PruningIndexes) bool {
	if len(idx.Value.args) != 0 || len(idx.Value.cols) != 0 {
		return false
	}
	if len(idx.Range.ranges) != 0 || len(idx.Range.cols) != 0 {
		return false
	}
	if len(idx.JoinEdge.edges) != 0 || len(idx.JoinEdge.byTable) != 0 {
		return false
	}
	if len(idx.Table.tables) != 0 {
		return false
	}
	return true
}
