package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ponchione/opsboard-canary/internal/app"
	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

type capacityEvent struct {
	ticket uint64
	stamp  int64
}

type finalTable struct {
	ID   schema.TableID
	Rows map[types.RowID]types.ProductValue
}

func finalCapture(tb testing.TB, dir string) map[string]finalTable {
	tb.Helper()
	tables := map[string]finalTable{}
	_, err := shunter.RunDataDirMigrations(context.Background(), app.NewModule(), app.StrictAuthConfig(dir, ""), func(_ context.Context, mc *shunter.MigrationContext) error {
		for _, id := range mc.Schema().Tables() {
			rows := map[types.RowID]types.ProductValue{}
			for rid, row := range mc.Transaction().ScanTable(id) {
				rows[rid] = row
			}
			tables[mc.Schema().TableName(id)] = finalTable{id, rows}
		}
		return nil
	})
	capacityOK(tb, err)
	return tables
}

// Runs only after server shutdown, timing, allocation and retained-heap capture.
func capacityVerifyComplete(b *testing.B, dir, fixture string, sent [][]capacityEvent, reads [][]float64) {
	seed := filepath.Join(b.TempDir(), "seed")
	capacityOK(b, shunter.RestoreDataDir(fixture, seed))
	before, after := finalCapture(b, seed), finalCapture(b, dir)
	expected := map[capacityEvent]types.ProductValue{}
	actor := auth.DeriveIdentity(app.StrictAuthIssuer, "alice").Hex()
	choices := sha256.New()
	perClient := make([][2]int, len(sent))
	for i, events := range sent {
		perClient[i] = [2]int{len(events), len(reads[i])}
		rng := rand.New(rand.NewPCG(capacitySeed, uint64(i+1)))
		for range len(events) + len(reads[i]) {
			fmt.Fprintf(choices, "%d:%t;", i, rng.IntN(100) < 50)
		}
		for j, e := range events {
			status, action := "closed", app.ReducerCloseTicket
			if j%2 == 1 {
				status, action = "open", app.ReducerReopenTicket
			}
			if _, ok := expected[e]; ok {
				b.Fatal("duplicate request event")
			}
			expected[e] = types.ProductValue{types.NewUint64(0), types.NewString(actor), types.NewString(action), types.NewString("tickets"), types.NewUint64(e.ticket), types.NewString("set ticket status to " + status), types.NewInt64(e.stamp)}
		}
		if len(events) > 0 {
			last := events[len(events)-1]
			for _, row := range before["tickets"].Rows {
				if row[0].AsUint64() == last.ticket {
					status := "open"
					if len(events)%2 == 1 {
						status = "closed"
					}
					row[4] = types.NewString(status)
					row[9] = types.NewInt64(last.stamp)
				}
			}
		}
	}
	var high uint64
	for _, row := range before["audit_log"].Rows {
		high = max(high, row[0].AsUint64())
	}
	originalHigh := high
	newIDs := map[uint64]bool{}
	for rid, row := range after["audit_log"].Rows {
		if _, old := before["audit_log"].Rows[rid]; old {
			continue
		}
		e := capacityEvent{row[4].AsUint64(), row[6].AsInt64()}
		want, ok := expected[e]
		if !ok {
			b.Fatal("unexpected audit", row)
		}
		id := row[0].AsUint64()
		want[0] = types.NewUint64(id)
		if id <= originalHigh || newIDs[id] || !reflect.DeepEqual(row, want) {
			b.Fatal("invalid audit", row, want)
		}
		high = max(high, id)
		newIDs[id] = true
		delete(expected, e)
		delete(after["audit_log"].Rows, rid)
	}
	if len(expected) != 0 || high != originalHigh+uint64(len(newIDs)) {
		b.Fatal("missing audit or ID gap")
	}
	// Updated rows are compared by primary key; physical IDs stay within
	// allocator bounds. Migration separately requires exact original physical IDs.
	normalized := map[types.RowID]types.ProductValue{}
	originalByPK := map[uint64]types.RowID{}
	var maxOriginal types.RowID
	for rid, row := range before["tickets"].Rows {
		originalByPK[row[0].AsUint64()] = rid
		maxOriginal = max(maxOriginal, rid)
	}
	touched := map[uint64]bool{}
	for _, events := range sent {
		if len(events) > 0 {
			touched[events[0].ticket] = true
		}
	}
	for rid, row := range after["tickets"].Rows {
		pk := row[0].AsUint64()
		original, ok := originalByPK[pk]
		if !ok || normalized[original] != nil {
			b.Fatal("unexpected or duplicate ticket")
		}
		if touched[pk] {
			if rid == 0 || rid > maxOriginal+types.RowID(len(newIDs)) {
				b.Fatal("ticket physical ID bound")
			}
		} else if rid != original {
			b.Fatal("untouched ticket physical ID changed")
		}
		normalized[original] = row
	}
	after["tickets"] = finalTable{after["tickets"].ID, normalized}
	for name, table := range before {
		if name == "audit_allocator" {
			continue
		}
		if !reflect.DeepEqual(table, after[name]) {
			b.Fatal("changed complete original table/physical IDs", name)
		}
	}
	if counter, ok := after["audit_allocator"]; ok {
		if len(counter.Rows) != 1 {
			b.Fatal("counter count")
		}
		for _, row := range counter.Rows {
			if row[0].AsUint64() != 1 || row[1].AsUint64() != high {
				b.Fatal("counter bound", row, high)
			}
		}
	}
	data, err := json.Marshal(map[string]any{"clients_writes_reads": perClient, "operation_choices_sha256": fmt.Sprintf("%x", choices.Sum(nil)), "audits_added": len(newIDs), "audit_high": high, "complete_values_and_physical_id_bounds": true, "new_audits_match_request_timestamps": true, "ordered_deliveries": true})
	capacityOK(b, err)
	b.Log("FINAL_CHECK " + string(data))
}
