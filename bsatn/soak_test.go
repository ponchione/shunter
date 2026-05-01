package bsatn

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestDecodeConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xb5a7d5)
		workers    = 6
		iterations = 128
	)

	valueInputs := decodeValueFuzzSeeds(t)
	productInputs := decodeProductValueFuzzSeeds(t)

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
					inputIndex := (int(seed) + worker*11 + op*7) % len(valueInputs)
					input := valueInputs[inputIndex]
					if err := checkDecodeValueFuzzInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=DecodeValue input=%x failure=%v",
							seed, worker, op, workers, iterations, input, err):
						default:
						}
						return
					}
				} else {
					inputIndex := (int(seed) + worker*13 + op*5) % len(productInputs)
					input := productInputs[inputIndex]
					if err := checkDecodeProductValueFuzzInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=DecodeProductValue input=%x failure=%v",
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
