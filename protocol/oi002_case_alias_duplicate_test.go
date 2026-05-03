package protocol

import (
	"context"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// TestOI002CaseAlias_CaseDistinctQuotedAliasesDoNotCollide pins byte-exact
// alias collision checks.
func TestOI002CaseAlias_CaseDistinctQuotedAliasesDoNotCollide(t *testing.T) {
	conn := testConnDirect(nil)
	b := schema.NewBuilder().SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name:    "t",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	b.TableDef(schema.TableDefinition{
		Name:    "s",
		Columns: []schema.ColumnDefinition{{Name: "u32", Type: schema.KindUint32}},
	})
	eng, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatalf("Build schema = %v", err)
	}
	sl := registrySchemaLookup{reg: eng.Registry()}
	snap := &mockSnapshot{rows: map[schema.TableID][]types.ProductValue{}}
	stateAccess := &mockStateAccess{snap: snap}

	msg := &OneOffQueryMsg{
		MessageID:   []byte{0xC1},
		QueryString: `SELECT t.* FROM t AS "R" JOIN s AS r ON "R".u32 = r.u32`,
	}
	handleOneOffQuery(context.Background(), conn, msg, stateAccess, sl)

	result := drainOneOff(t, conn)
	if result.Error == nil {
		return
	}
	if strings.Contains(*result.Error, "Duplicate name") {
		t.Fatalf("Error = %q, must not be a Duplicate name collision (case-distinct aliases `\"R\"` and `r` are byte-distinct in reference SqlIdent)", *result.Error)
	}
}
