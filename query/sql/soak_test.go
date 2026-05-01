package sql

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestParseAndCoerceConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0x501c0de)
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
				if (int(seed)+worker+op)%2 == 0 {
					inputIndex := (int(seed) + worker*11 + op*7) % len(parseFuzzSeeds)
					input := parseFuzzSeeds[inputIndex]
					if err := checkParseFuzzInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=Parse input=%q failure=%v",
							seed, worker, op, workers, iterations, input, err):
						default:
						}
						return
					}
				} else {
					inputIndex := (int(seed) + worker*13 + op*5) % len(coerceFuzzSeeds)
					input := coerceFuzzSeeds[inputIndex]
					if err := checkCoerceFuzzInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=CoerceWithCaller input=%x failure=%v",
							seed, worker, op, workers, iterations, input, err):
						default:
						}
						return
					}
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
