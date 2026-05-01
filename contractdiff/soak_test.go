package contractdiff

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestCompareAndPlanJSONConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc0a7d1ff)
		workers    = 6
		iterations = 128
	)

	seeds := contractDiffJSONFuzzSeeds(t)
	start := make(chan struct{})
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				seedIndex := (int(seed) + worker*11 + op*7) % len(seeds)
				input := seeds[seedIndex]
				if err := checkContractDiffJSONInput(input.old, input.current); err != nil {
					select {
					case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d corpus_index=%d failure=%v",
						seed, worker, op, workers, iterations, seedIndex, err):
					default:
					}
					return
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}

	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}
