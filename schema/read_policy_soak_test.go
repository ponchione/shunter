package schema

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestReadPolicyJSONConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0x5c0a6e)
		workers    = 6
		iterations = 128
	)

	start := make(chan struct{})
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				inputIndex := (int(seed) + worker*11 + op*7) % len(readPolicyFuzzSeeds)
				input := readPolicyFuzzSeeds[inputIndex]
				if err := checkReadPolicyJSONInput(input); err != nil {
					select {
					case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=ReadPolicyJSON input=%q failure=%v",
						seed, worker, op, workers, iterations, input, err):
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
