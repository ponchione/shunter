package executor

import (
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type commitMemoryObserver struct {
	enabled bool
	samples int
}

func (*commitMemoryObserver) LogStoreSnapshotLeaked(string)      {}
func (*commitMemoryObserver) RecordStoreReadRows(string, uint64) {}
func (o *commitMemoryObserver) StoreMemoryUsageEnabled() bool    { return o.enabled }
func (o *commitMemoryObserver) RecordStoreMemoryUsage([]store.MemoryUsage) {
	o.samples++
}

func TestCommitDoesNotSampleStoreMemory(t *testing.T) {
	exec, reg := setupExecutorWithObserver(nil, ExecutorConfig{})
	observer := &commitMemoryObserver{enabled: true}
	exec.committed.SetObserver(observer)
	tx := store.NewTransaction(exec.committed, reg)
	if _, err := tx.Insert(0, types.ProductValue{types.NewUint64(1), types.NewString("small")}); err != nil {
		t.Fatal(err)
	}
	tx.Seal()
	if _, err := exec.commitTransaction(tx, 1); err != nil {
		t.Fatal(err)
	}
	if observer.samples != 0 {
		t.Fatalf("commit sampled whole-store memory %d times", observer.samples)
	}
}

// Only the unrelated indexed table grows; each measured commit updates one row.
func BenchmarkCommitMemoryMetrics(b *testing.B) {
	for _, rows := range []int{0, 1000, 10000} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("unrelated_rows=%d/metrics=%t", rows, enabled), func(b *testing.B) {
				exec, reg := setupExecutorWithObserver(nil, ExecutorConfig{})
				unrelated := store.NewTable(&schema.TableSchema{
					ID: 1, Name: "unrelated",
					Columns: []schema.ColumnSchema{
						{Index: 0, Name: "id", Type: types.KindUint64},
						{Index: 1, Name: "body", Type: types.KindString},
					},
					Indexes: []schema.IndexSchema{
						{ID: 1, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
					},
				})
				exec.committed.RegisterTable(1, unrelated)
				for i := range rows {
					if err := unrelated.InsertRow(unrelated.AllocRowID(), types.ProductValue{
						types.NewUint64(uint64(i)), types.NewString("unrelated indexed row"),
					}); err != nil {
						b.Fatal(err)
					}
				}
				row := types.ProductValue{types.NewUint64(1), types.NewString("small")}
				updates := [2]types.ProductValue{row, {types.NewUint64(1), types.NewString("other")}}
				tx := store.NewTransaction(exec.committed, reg)
				rowID, err := tx.Insert(0, row)
				if err != nil {
					b.Fatal(err)
				}
				tx.Seal()
				if _, err := exec.commitTransaction(tx, 1); err != nil {
					b.Fatal(err)
				}
				exec.committed.SetObserver(&commitMemoryObserver{enabled: enabled})
				txID := types.TxID(1)
				b.ReportAllocs()
				for b.Loop() {
					tx := store.NewTransaction(exec.committed, reg)
					rowID, err = tx.Update(0, rowID, updates[txID%2])
					if err != nil {
						b.Fatal(err)
					}
					tx.Seal()
					txID++
					if _, err := exec.commitTransaction(tx, txID); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
