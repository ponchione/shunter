package main

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"testing"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestContractReadCommandsConcurrentShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc11c0a7)
		workers    = 6
		iterations = 128
	)

	cases := newCLIReadSoakCases(t)
	start := make(chan struct{})
	failures := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				caseIndex := (int(seed) + worker*11 + op*7) % len(cases)
				tc := cases[caseIndex]
				got := runCLIReadSoakCommand(tc.args)
				if got.code != tc.want.code || got.stdout != tc.want.stdout || got.stderr != tc.want.stderr {
					select {
					case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d command=%s observed=%#v expected=%#v",
						seed, worker, op, workers, iterations, tc.name, got, tc.want):
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

type cliReadSoakCase struct {
	name string
	args []string
	want cliReadSoakResult
}

type cliReadSoakResult struct {
	code   int
	stdout string
	stderr string
}

func newCLIReadSoakCases(t *testing.T) []cliReadSoakCase {
	t.Helper()

	dir := t.TempDir()
	previousPath := writeCLIContract(t, dir, "previous.json", cliContractFixture())
	current := cliContractFixture()
	current.Module.Version = "v1.1.0"
	current.Schema.Tables[0].Columns = append(current.Schema.Tables[0].Columns, schema.ColumnExport{Name: "sent_at", Type: "timestamp"})
	current.Queries = append(current.Queries, shunter.QueryDescription{Name: "recent_messages"})
	currentPath := writeCLIContract(t, dir, "current.json", current)

	cases := []cliReadSoakCase{
		{
			name: "diff-json",
			args: []string{
				"contract", "diff",
				"--previous", previousPath,
				"--current", currentPath,
				"--format", "json",
			},
		},
		{
			name: "policy-strict-json",
			args: []string{
				"contract", "policy",
				"--previous", previousPath,
				"--current", currentPath,
				"--strict",
				"--require-previous-version",
				"--format", "json",
			},
		},
		{
			name: "plan-json",
			args: []string{
				"contract", "plan",
				"--previous", previousPath,
				"--current", currentPath,
				"--require-previous-version",
				"--format", "json",
			},
		},
	}
	for i := range cases {
		cases[i].want = runCLIReadSoakCommand(cases[i].args)
	}
	return cases
}

func runCLIReadSoakCommand(args []string) cliReadSoakResult {
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, args)
	return cliReadSoakResult{
		code:   code,
		stdout: stdout.String(),
		stderr: stderr.String(),
	}
}
