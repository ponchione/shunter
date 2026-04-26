package protocol

import (
	"context"
	"strings"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// TestOI002Scout_CaseDistinctQuotedAliasesDoNotCollide pins the reference
// `SqlIdent` byte-equal alias semantics: a join whose two relations are
// aliased with case-distinct identifiers (`"R"` and `r`) must NOT be
// rejected as a `DuplicateName` collision. Reference path: `type_from`
// (expr/src/lib.rs:88-89) inserts each alias into a HashSet keyed by
// `Relvars`; `Relvars` is a byte-equal `SqlIdent`, so `"R"` and `r` are
// distinct keys and the second insert does not collide. This pin keeps the
// collision check exact so case-distinct aliases cannot regress into a
// `DuplicateName` rejection.
//
// Scope: this scout pins ONLY the collision-detection seam. Downstream
// parser/protocol alias routing for the same case-distinct shape is pinned by
// `TestParseCaseDistinctRelationAliasesResolveIndependently`,
// `TestHandleOneOffQuery_CaseDistinctRelationAliasesRouteJoinSides`, and
// `TestHandleSubscribeSingle_CaseDistinctRelationAliasesRouteJoinSides`.
func TestOI002Scout_CaseDistinctQuotedAliasesDoNotCollide(t *testing.T) {
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
