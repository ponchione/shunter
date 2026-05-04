package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

type memoryStoreObserver struct {
	enabled bool
	usage   []MemoryUsage
}

func (o *memoryStoreObserver) LogStoreSnapshotLeaked(string)      {}
func (o *memoryStoreObserver) RecordStoreReadRows(string, uint64) {}
func (o *memoryStoreObserver) StoreMemoryUsageEnabled() bool      { return o.enabled }
func (o *memoryStoreObserver) RecordStoreMemoryUsage(usage []MemoryUsage) {
	o.usage = append([]MemoryUsage(nil), usage...)
}

func TestCommittedStateMemoryUsageTracksTablesAndIndexes(t *testing.T) {
	cs, pkIdx := seedMemoryUsageState(t)

	usage := cs.MemoryUsage()
	tableUsage := requireMemoryUsage(t, usage, StoreMemoryKindTableRows, "players", "")
	indexUsage := requireMemoryUsage(t, usage, StoreMemoryKindIndex, "players", "pk")
	if tableUsage.Bytes == 0 {
		t.Fatalf("table row memory = 0, want non-zero usage")
	}
	if indexUsage.Bytes == 0 {
		t.Fatalf("index memory = 0, want non-zero usage")
	}

	snap := cs.Snapshot()
	ids := snap.IndexSeek(0, pkIdx, NewIndexKey(types.NewUint64(1)))
	snap.Close()
	if len(ids) != 1 {
		t.Fatalf("seed index seek len = %d, want 1", len(ids))
	}
}

func TestCommittedStateRecordMemoryUsageHonorsObserverEnabled(t *testing.T) {
	cs, _ := seedMemoryUsageState(t)
	observer := &memoryStoreObserver{}
	cs.SetObserver(observer)
	cs.RecordMemoryUsage()
	if len(observer.usage) != 0 {
		t.Fatalf("disabled observer recorded memory usage: %+v", observer.usage)
	}

	observer.enabled = true
	cs.RecordMemoryUsage()
	requireMemoryUsage(t, observer.usage, StoreMemoryKindTableRows, "players", "")
	requireMemoryUsage(t, observer.usage, StoreMemoryKindIndex, "players", "pk")
}

func seedMemoryUsageState(t *testing.T) (*CommittedState, schema.IndexID) {
	t.Helper()
	ts := &schema.TableSchema{
		ID:   0,
		Name: "players",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "name", Type: types.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
		},
	}
	cs := NewCommittedState()
	table := NewTable(ts)
	cs.RegisterTable(0, table)
	for _, row := range []types.ProductValue{
		{types.NewUint64(1), types.NewString("alice")},
		{types.NewUint64(2), types.NewString("bob")},
	} {
		if err := table.InsertRow(table.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}
	return cs, 0
}

func requireMemoryUsage(t *testing.T, usage []MemoryUsage, kind, tableName, indexName string) MemoryUsage {
	t.Helper()
	for _, item := range usage {
		if item.Kind == kind && item.TableName == tableName && item.IndexName == indexName {
			return item
		}
	}
	t.Fatalf("missing memory usage kind=%q table=%q index=%q in %+v", kind, tableName, indexName, usage)
	return MemoryUsage{}
}
