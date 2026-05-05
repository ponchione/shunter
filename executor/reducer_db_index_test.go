package executor

import (
	"slices"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestReducerDBSeekCompositeIndexReturnsMatchingDetachedRows(t *testing.T) {
	cs := store.NewCommittedState()
	table := store.NewTable(reducerDBCompositeSchema())
	cs.RegisterTable(0, table)

	committedMatchID := table.AllocRowID()
	if err := table.InsertRow(committedMatchID, reducerDBCompositeRow(1, "red", 10, "committed-match")); err != nil {
		t.Fatal(err)
	}
	if err := table.InsertRow(table.AllocRowID(), reducerDBCompositeRow(2, "red", 20, "committed-other-score")); err != nil {
		t.Fatal(err)
	}
	if err := table.InsertRow(table.AllocRowID(), reducerDBCompositeRow(3, "blue", 10, "committed-other-guild")); err != nil {
		t.Fatal(err)
	}

	tx := store.NewTransaction(cs, nil)
	txMatchID, err := tx.Insert(0, reducerDBCompositeRow(4, "red", 10, "tx-match"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Insert(0, reducerDBCompositeRow(5, "red", 30, "tx-other-score")); err != nil {
		t.Fatal(err)
	}

	db := &reducerDBAdapter{tx: tx}
	var bodies []string
	for _, row := range db.SeekIndex(0, 1, types.NewString("red"), types.NewUint64(10)) {
		bodies = append(bodies, row[3].AsString())
		row[3] = types.NewString("mutated-by-reducer")
	}
	slices.Sort(bodies)
	wantBodies := []string{"committed-match", "tx-match"}
	if !slices.Equal(bodies, wantBodies) {
		t.Fatalf("composite SeekIndex bodies = %v, want %v", bodies, wantBodies)
	}

	if got, ok := table.GetRow(committedMatchID); !ok || got[3].AsString() != "committed-match" {
		t.Fatalf("committed row after reducer mutation = %v, %v; want original body", got, ok)
	}
	if got, ok := tx.GetRow(0, txMatchID); !ok || got[3].AsString() != "tx-match" {
		t.Fatalf("tx row after reducer mutation = %v, %v; want original body", got, ok)
	}
}

func reducerDBCompositeSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   0,
		Name: "scores",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
			{Index: 1, Name: "guild", Type: schema.KindString},
			{Index: 2, Name: "score", Type: schema.KindUint64},
			{Index: 3, Name: "body", Type: schema.KindString},
		},
		Indexes: []schema.IndexSchema{
			{ID: 0, Name: "pk", Columns: []int{0}, Unique: true, Primary: true},
			{ID: 1, Name: "guild_score", Columns: []int{1, 2}},
		},
	}
}

func reducerDBCompositeRow(id uint64, guild string, score uint64, body string) types.ProductValue {
	return types.ProductValue{
		types.NewUint64(id),
		types.NewString(guild),
		types.NewUint64(score),
		types.NewString(body),
	}
}
