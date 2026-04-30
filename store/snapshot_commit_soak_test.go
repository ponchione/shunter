package store

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestCommittedSnapshotConcurrentCommitShortSoak(t *testing.T) {
	const (
		seed       = uint64(0x5107c0de)
		writerOps  = 64
		readers    = 4
		iterations = 128
	)
	cs, reg := buildTestState()
	start := make(chan struct{})
	failures := make(chan string, writerOps+(readers*iterations))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for op := range writerOps {
			pk := uint64(op + 1)
			tx := NewTransaction(cs, reg)
			if _, err := tx.Insert(0, mkRow(pk, fmt.Sprintf("player-%03d", pk))); err != nil {
				failures <- fmt.Sprintf("seed=%#x writer_op=%d runtime_config=writer_ops=%d/readers=%d/iterations=%d operation=Insert(%d) observed_error=%v expected=nil",
					seed, op, writerOps, readers, iterations, pk, err)
				return
			}
			if _, err := Commit(cs, tx); err != nil {
				failures <- fmt.Sprintf("seed=%#x writer_op=%d runtime_config=writer_ops=%d/readers=%d/iterations=%d operation=Commit(%d) observed_error=%v expected=nil",
					seed, op, writerOps, readers, iterations, pk, err)
				return
			}
			if (int(seed)+op)%7 == 0 {
				runtime.Gosched()
			}
		}
	}()

	for reader := range readers {
		wg.Add(1)
		go func(reader int) {
			defer wg.Done()
			<-start
			for op := range iterations {
				snap := cs.Snapshot()
				rowCount := snap.RowCount(0)
				rows := make(map[uint64]string, rowCount)
				for _, row := range snap.TableScan(0) {
					pk := row[0].AsUint64()
					name := row[1].AsString()
					if _, exists := rows[pk]; exists {
						failures <- fmt.Sprintf("seed=%#x reader=%d op=%d runtime_config=writer_ops=%d/readers=%d/iterations=%d operation=TableScan observed=duplicate-pk-%d expected=unique-prefix",
							seed, reader, op, writerOps, readers, iterations, pk)
						snap.Close()
						return
					}
					rows[pk] = name
				}
				snap.Close()

				if len(rows) != rowCount {
					failures <- fmt.Sprintf("seed=%#x reader=%d op=%d runtime_config=writer_ops=%d/readers=%d/iterations=%d operation=RowCount+TableScan observed=%d expected=%d",
						seed, reader, op, writerOps, readers, iterations, len(rows), rowCount)
					return
				}
				for pk := uint64(1); pk <= uint64(rowCount); pk++ {
					want := fmt.Sprintf("player-%03d", pk)
					if got, ok := rows[pk]; !ok || got != want {
						failures <- fmt.Sprintf("seed=%#x reader=%d op=%d runtime_config=writer_ops=%d/readers=%d/iterations=%d operation=validate-prefix observed=(pk=%d ok=%v name=%q) expected=(ok=true name=%q)",
							seed, reader, op, writerOps, readers, iterations, pk, ok, got, want)
						return
					}
				}
				if (int(seed)+reader+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(reader)
	}

	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("seed=%#x op=wait runtime_config=writer_ops=%d/readers=%d/iterations=%d observed=timeout expected=all-workers-finished", seed, writerOps, readers, iterations)
	}
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}

	snap := cs.Snapshot()
	defer snap.Close()
	if got := snap.RowCount(0); got != writerOps {
		t.Fatalf("seed=%#x op=final-count runtime_config=writer_ops=%d/readers=%d/iterations=%d observed=%d expected=%d",
			seed, writerOps, readers, iterations, got, writerOps)
	}
}
