package subscription

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestQueryHashConcurrentDeterministicFixedSeeds(t *testing.T) {
	clientA := types.Identity{0x10, 0x20, 0x30}
	clientB := types.Identity{0x99, 0x88, 0x77}
	seeds := []struct {
		name   string
		pred   Predicate
		client *types.Identity
	}{
		{
			name: "same-table-and",
			pred: And{
				Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)},
				Right: ColRange{Table: 1, Column: 1, Lower: Bound{Value: types.NewTimestamp(1_739_202_330_000_000), Inclusive: true}, Upper: Bound{Unbounded: true}},
			},
		},
		{
			name: "same-table-or",
			pred: Or{
				Left:  ColNe{Table: 2, Column: 0, Value: types.NewString("north")},
				Right: ColEq{Table: 2, Column: 1, Value: types.NewArrayString([]string{"alpha", "beta"})},
			},
		},
		{
			name: "self-join-filter",
			pred: Join{
				Left:       3,
				Right:      3,
				LeftCol:    0,
				RightCol:   2,
				LeftAlias:  0,
				RightAlias: 1,
				Filter: And{
					Left:  ColEq{Table: 3, Column: 1, Alias: 0, Value: types.NewUint32(7)},
					Right: ColNe{Table: 3, Column: 1, Alias: 1, Value: types.NewUint32(9)},
				},
			},
			client: &clientA,
		},
		{
			name: "cross-join-filter",
			pred: CrossJoin{
				Left:         4,
				Right:        5,
				ProjectRight: true,
				Filter: Or{
					Left:  ColEq{Table: 4, Column: 0, Value: types.NewBytes([]byte{1, 2, 3})},
					Right: ColEq{Table: 5, Column: 1, Value: types.NewInt128(-1, ^uint64(0))},
				},
			},
			client: &clientB,
		},
		{
			name: "wide-values",
			pred: And{
				Left:  ColEq{Table: 6, Column: 0, Value: types.NewUint256(1, 2, 3, 4)},
				Right: ColNe{Table: 6, Column: 1, Value: types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0))},
			},
		},
	}

	expected := make([]QueryHash, len(seeds))
	for i, seed := range seeds {
		expected[i] = ComputeQueryHash(seed.pred, seed.client)
	}

	const workers = 8
	const iterations = 256
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				seedIndex := (worker*31 + iter*17) % len(seeds)
				got := ComputeQueryHash(seeds[seedIndex].pred, seeds[seedIndex].client)
				if got != expected[seedIndex] {
					select {
					case failures <- fmt.Sprintf("worker=%d iter=%d seed=%s index=%d got=%s want=%s", worker, iter, seeds[seedIndex].name, seedIndex, got, expected[seedIndex]):
					default:
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}
