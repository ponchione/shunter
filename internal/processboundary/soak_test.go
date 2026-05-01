package processboundary

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
)

func TestBoundaryJSONConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xb0a5d17)
		workers    = 6
		iterations = 128
	)

	requests := invocationRequestJSONFuzzSeeds()
	responses := invocationResponseJSONFuzzSeeds()
	contracts := contractJSONFuzzSeeds()

	start := make(chan struct{})
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				switch (int(seed) + worker + op) % 3 {
				case 0:
					inputIndex := (int(seed) + worker*11 + op*7) % len(requests)
					input := requests[inputIndex]
					if err := checkInvocationRequestJSONInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=InvocationRequestJSON input=%s failure=%v",
							seed, worker, op, workers, iterations, boundarySoakInputLabel(input), err):
						default:
						}
						return
					}
				case 1:
					inputIndex := (int(seed) + worker*13 + op*5) % len(responses)
					input := responses[inputIndex]
					if err := checkInvocationResponseJSONInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=InvocationResponseJSON input=%s failure=%v",
							seed, worker, op, workers, iterations, boundarySoakInputLabel(input), err):
						default:
						}
						return
					}
				default:
					inputIndex := (int(seed) + worker*17 + op*3) % len(contracts)
					input := contracts[inputIndex]
					if err := checkContractJSONInput(input); err != nil {
						select {
						case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d operation=ContractJSON input=%s failure=%v",
							seed, worker, op, workers, iterations, boundarySoakInputLabel(input), err):
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

func boundarySoakInputLabel(data []byte) string {
	if len(data) <= 96 {
		return fmt.Sprintf("%q", data)
	}
	return fmt.Sprintf("%q...", data[:96])
}
