package subscription

import (
	"math"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestQueryHashDeterministic(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	h1 := ComputeQueryHash(p, nil)
	h2 := ComputeQueryHash(p, nil)
	if h1 != h2 {
		t.Fatalf("deterministic: %v != %v", h1, h2)
	}
}

func TestQueryHashValueDifferent(t *testing.T) {
	p1 := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	p2 := ColEq{Table: 1, Column: 0, Value: types.NewUint64(43)}
	if ComputeQueryHash(p1, nil) == ComputeQueryHash(p2, nil) {
		t.Fatal("different values should produce different hashes")
	}
}

func TestQueryHashUUIDUsesCanonicalBytes(t *testing.T) {
	u, err := types.ParseUUID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	raw := u.AsUUID()
	uHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: u}, nil)
	bytesHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: types.NewBytes(raw[:])}, nil)
	if uHash == bytesHash {
		t.Fatal("UUID hash should include kind tag and not collapse to raw bytes")
	}
}

func TestQueryHashDurationUsesDistinctKind(t *testing.T) {
	durationHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: types.NewDuration(42)}, nil)
	intHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: types.NewInt64(42)}, nil)
	if durationHash == intHash {
		t.Fatal("Duration hash should include kind tag and not collapse to Int64")
	}
}

func TestQueryHashJSONUsesCanonicalBytesAndKind(t *testing.T) {
	j1, err := types.NewJSON([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	j2, err := types.NewJSON([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	jsonHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: j1}, nil)
	canonicalHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: j2}, nil)
	if jsonHash != canonicalHash {
		t.Fatal("JSON hash should use canonical bytes")
	}
	bytesHash := ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: types.NewBytes(j1.JSONView())}, nil)
	if jsonHash == bytesHash {
		t.Fatal("JSON hash should include kind tag and not collapse to raw bytes")
	}
}

func TestQueryHashCanonicalizesFloatZero(t *testing.T) {
	neg32, err := types.NewFloat32(float32(math.Copysign(0, -1)))
	if err != nil {
		t.Fatal(err)
	}
	pos32, err := types.NewFloat32(0)
	if err != nil {
		t.Fatal(err)
	}
	if ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: neg32}, nil) != ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: pos32}, nil) {
		t.Fatal("Float32 -0 and +0 compare equal and must hash equally")
	}

	neg64, err := types.NewFloat64(math.Copysign(0, -1))
	if err != nil {
		t.Fatal(err)
	}
	pos64, err := types.NewFloat64(0)
	if err != nil {
		t.Fatal(err)
	}
	if ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: neg64}, nil) != ComputeQueryHash(ColEq{Table: 1, Column: 0, Value: pos64}, nil) {
		t.Fatal("Float64 -0 and +0 compare equal and must hash equally")
	}
}

func TestQueryHashColNeDiffersFromColEq(t *testing.T) {
	eq := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	ne := ColNe{Table: 1, Column: 0, Value: types.NewUint64(42)}
	if ComputeQueryHash(eq, nil) == ComputeQueryHash(ne, nil) {
		t.Fatal("ColEq and ColNe should hash differently")
	}
}

func TestQueryHashColEqColDiffersByColumn(t *testing.T) {
	left := ColEqCol{LeftTable: 1, LeftColumn: 0, RightTable: 2, RightColumn: 0}
	right := ColEqCol{LeftTable: 1, LeftColumn: 1, RightTable: 2, RightColumn: 0}
	if ComputeQueryHash(left, nil) == ComputeQueryHash(right, nil) {
		t.Fatal("ColEqCol hash should include both column references")
	}
}

func TestQueryShapeHashIncludesAggregateMetadata(t *testing.T) {
	pred := AllRows{Table: 1}
	result := schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64}
	countStar := &Aggregate{Func: AggregateCount, ResultColumn: result}
	countBody := &Aggregate{
		Func:         AggregateCount,
		ResultColumn: result,
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 1, Name: "body", Type: types.KindString},
			Table:  1,
			Column: 1,
		},
	}
	countDistinctBody := copyAggregate(countBody)
	countDistinctBody.Distinct = true
	renamed := &Aggregate{Func: AggregateCount, ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64}}
	sumID := &Aggregate{
		Func:         AggregateSum,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  1,
			Column: 0,
		},
	}
	projection := []ProjectionColumn{{Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64}, Table: 1, Column: 0}}

	tableHash := ComputeQueryPlanHash(pred, nil, nil)
	projectionHash := ComputeQueryPlanHash(pred, projection, nil)
	countStarHash := ComputeQueryShapeHash(pred, nil, countStar, nil)
	countBodyHash := ComputeQueryShapeHash(pred, nil, countBody, nil)
	countDistinctBodyHash := ComputeQueryShapeHash(pred, nil, countDistinctBody, nil)
	renamedHash := ComputeQueryShapeHash(pred, nil, renamed, nil)
	sumHash := ComputeQueryShapeHash(pred, nil, sumID, nil)

	if countStarHash == tableHash {
		t.Fatal("aggregate query shape should not collapse to table-shaped hash")
	}
	if countStarHash == projectionHash {
		t.Fatal("aggregate query shape should not collapse to projection hash")
	}
	if countStarHash == countBodyHash {
		t.Fatal("COUNT(*) and COUNT(column) should hash differently")
	}
	if countBodyHash == countDistinctBodyHash {
		t.Fatal("COUNT(column) and COUNT(DISTINCT column) should hash differently")
	}
	if countStarHash == renamedHash {
		t.Fatal("aggregate result column schema should affect query identity")
	}
	if countStarHash == sumHash {
		t.Fatal("COUNT and SUM should hash differently")
	}
}

func TestQueryOrderedShapeHashIncludesOrderByMetadata(t *testing.T) {
	pred := AllRows{Table: 1}
	idOrder := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}
	idDesc := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		Table:  1,
		Column: 0,
		Desc:   true,
	}}
	bodyThenID := []OrderByColumn{
		{Schema: schema.ColumnSchema{Index: 1, Name: "body", Type: types.KindString}, Table: 1, Column: 1},
		{Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64}, Table: 1, Column: 0},
	}
	idThenBody := []OrderByColumn{
		{Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64}, Table: 1, Column: 0},
		{Schema: schema.ColumnSchema{Index: 1, Name: "body", Type: types.KindString}, Table: 1, Column: 1},
	}

	unordered := ComputeQueryShapeHash(pred, nil, nil, nil)
	ordered := ComputeQueryOrderedShapeHash(pred, nil, nil, idOrder, nil)
	if unordered == ordered {
		t.Fatal("ORDER BY metadata should change query identity from unordered shape")
	}
	if ordered == ComputeQueryOrderedShapeHash(pred, nil, nil, idDesc, nil) {
		t.Fatal("ORDER BY ASC and DESC should hash differently")
	}
	if ComputeQueryOrderedShapeHash(pred, nil, nil, bodyThenID, nil) == ComputeQueryOrderedShapeHash(pred, nil, nil, idThenBody, nil) {
		t.Fatal("multi-column ORDER BY term order should affect query identity")
	}
}

func TestQueryLimitedShapeHashIncludesLimitMetadata(t *testing.T) {
	pred := AllRows{Table: 1}
	limitTwo := uint64(2)
	limitThree := uint64(3)
	orderBy := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}

	unlimited := ComputeQueryOrderedShapeHash(pred, nil, nil, orderBy, nil)
	limitedTwo := ComputeQueryLimitedShapeHash(pred, nil, nil, orderBy, &limitTwo, nil)
	if unlimited == limitedTwo {
		t.Fatal("LIMIT metadata should change query identity from unlimited ordered shape")
	}
	if limitedTwo == ComputeQueryLimitedShapeHash(pred, nil, nil, orderBy, &limitThree, nil) {
		t.Fatal("different LIMIT values should hash differently")
	}
}

func TestQueryWindowedShapeHashIncludesOffsetMetadata(t *testing.T) {
	pred := AllRows{Table: 1}
	limitTwo := uint64(2)
	offsetOne := uint64(1)
	offsetTwo := uint64(2)
	orderBy := []OrderByColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}

	withoutOffset := ComputeQueryLimitedShapeHash(pred, nil, nil, orderBy, &limitTwo, nil)
	withOffset := ComputeQueryWindowedShapeHash(pred, nil, nil, orderBy, &limitTwo, &offsetOne, nil)
	if withoutOffset == withOffset {
		t.Fatal("OFFSET metadata should change query identity from limited shape")
	}
	if withOffset == ComputeQueryWindowedShapeHash(pred, nil, nil, orderBy, &limitTwo, &offsetTwo, nil) {
		t.Fatal("different OFFSET values should hash differently")
	}
}

func TestQueryHashOrDiffersFromAnd(t *testing.T) {
	left := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	right := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	if ComputeQueryHash(And{Left: left, Right: right}, nil) == ComputeQueryHash(Or{Left: left, Right: right}, nil) {
		t.Fatal("And and Or should hash differently")
	}
}

func TestQueryHashSameClient(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	id := types.Identity{1, 2, 3}
	h1 := ComputeQueryHash(p, &id)
	h2 := ComputeQueryHash(p, &id)
	if h1 != h2 {
		t.Fatalf("same client: %v != %v", h1, h2)
	}
}

func TestQueryHashDifferentClients(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	a := types.Identity{1}
	b := types.Identity{2}
	if ComputeQueryHash(p, &a) == ComputeQueryHash(p, &b) {
		t.Fatal("different clients should produce different parameterized hashes")
	}
}

func TestQueryHashNoClientVsClient(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	id := types.Identity{1}
	if ComputeQueryHash(p, nil) == ComputeQueryHash(p, &id) {
		t.Fatal("non-parameterized vs parameterized should differ")
	}
}

func TestQueryHashSameTableAndChildOrderCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	p1 := And{Left: a, Right: b}
	p2 := And{Left: b, Right: a}
	if ComputeQueryHash(p1, nil) != ComputeQueryHash(p2, nil) {
		t.Fatal("same-table And child order should not change canonical hash")
	}
}

func TestQueryHashSameTableOrChildOrderCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)}
	p1 := Or{Left: a, Right: b}
	p2 := Or{Left: b, Right: a}
	if ComputeQueryHash(p1, nil) != ComputeQueryHash(p2, nil) {
		t.Fatal("same-table Or child order should not change canonical hash")
	}
}

func TestQueryHashSameTableAndAssociativeGroupingCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	c := ColEq{Table: 1, Column: 2, Value: types.NewUint64(30)}
	leftGrouped := And{Left: And{Left: a, Right: b}, Right: c}
	rightGrouped := And{Left: a, Right: And{Left: b, Right: c}}
	if ComputeQueryHash(leftGrouped, nil) != ComputeQueryHash(rightGrouped, nil) {
		t.Fatal("same-table And grouping should not change canonical hash")
	}
}

func TestQueryHashSameTableOrAssociativeGroupingCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)}
	c := ColEq{Table: 1, Column: 0, Value: types.NewUint64(3)}
	leftGrouped := Or{Left: Or{Left: a, Right: b}, Right: c}
	rightGrouped := Or{Left: a, Right: Or{Left: b, Right: c}}
	if ComputeQueryHash(leftGrouped, nil) != ComputeQueryHash(rightGrouped, nil) {
		t.Fatal("same-table Or grouping should not change canonical hash")
	}
}

func TestQueryHashSameTableDuplicateAndCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	duplicated := And{Left: a, Right: a}
	if ComputeQueryHash(a, nil) != ComputeQueryHash(duplicated, nil) {
		t.Fatal("same-table duplicate And leaf should share canonical hash with single leaf")
	}
}

func TestQueryHashSameTableDuplicateOrCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	duplicated := Or{Left: a, Right: a}
	if ComputeQueryHash(a, nil) != ComputeQueryHash(duplicated, nil) {
		t.Fatal("same-table duplicate Or leaf should share canonical hash with single leaf")
	}
}

func TestQueryHashSameTableOrAbsorptionCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	absorbed := Or{Left: a, Right: And{Left: a, Right: b}}
	if ComputeQueryHash(a, nil) != ComputeQueryHash(absorbed, nil) {
		t.Fatal("same-table Or absorption should share canonical hash with absorbed leaf")
	}
}

func TestQueryHashSameTableAndAbsorptionCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	absorbed := And{Left: a, Right: Or{Left: a, Right: b}}
	if ComputeQueryHash(a, nil) != ComputeQueryHash(absorbed, nil) {
		t.Fatal("same-table And absorption should share canonical hash with absorbed leaf")
	}
}

func TestQueryHashMultiTableAndOrderMatters(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 2, Column: 0, Value: types.NewUint64(2)}
	p1 := And{Left: a, Right: b}
	p2 := And{Left: b, Right: a}
	if ComputeQueryHash(p1, nil) == ComputeQueryHash(p2, nil) {
		t.Fatal("multi-table And order should still matter")
	}
}

func TestQueryHashJoinCompoundOrderMatters(t *testing.T) {
	join := Join{Left: 1, Right: 1, LeftCol: 0, RightCol: 0, LeftAlias: 0, RightAlias: 1}
	leaf := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	p1 := And{Left: join, Right: leaf}
	p2 := And{Left: leaf, Right: join}
	if ComputeQueryHash(p1, nil) == ComputeQueryHash(p2, nil) {
		t.Fatal("join-containing compound order should still matter")
	}
}

func TestQueryHashTrueAndComparisonMatchesComparison(t *testing.T) {
	comparison := ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)}
	withTrue := And{Left: AllRows{Table: 1}, Right: comparison}
	if ComputeQueryHash(withTrue, nil) != ComputeQueryHash(comparison, nil) {
		t.Fatal("TRUE AND comparison should share canonical hash with comparison")
	}
}

func TestQueryHashNoRowsStable(t *testing.T) {
	pred := NoRows{Table: 1}
	hashA := ComputeQueryHash(pred, nil)
	hashB := ComputeQueryHash(pred, nil)
	if hashA != hashB {
		t.Fatal("NoRows hash should be deterministic")
	}
}

func TestQueryHashSameTableFalseOrComparisonCanonicalized(t *testing.T) {
	comparison := ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)}
	withFalse := Or{Left: NoRows{Table: 1}, Right: comparison}
	if ComputeQueryHash(withFalse, nil) != ComputeQueryHash(comparison, nil) {
		t.Fatal("FALSE OR comparison should share canonical hash with comparison")
	}
}

func TestQueryHashMixedTableFalseOrComparisonNotCanonicalized(t *testing.T) {
	mixed := Or{
		Left:  NoRows{Table: 1},
		Right: ColEq{Table: 2, Column: 0, Value: types.NewUint64(7)},
	}
	right := ColEq{Table: 2, Column: 0, Value: types.NewUint64(7)}
	if ComputeQueryHash(mixed, nil) == ComputeQueryHash(right, nil) {
		t.Fatal("mixed-table FALSE OR comparison should not collapse to the right child")
	}
}

func TestQueryHashJoinFilterChildOrderCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	joinA := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: And{Left: a, Right: b}}
	joinB := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: And{Left: b, Right: a}}
	if ComputeQueryHash(joinA, nil) != ComputeQueryHash(joinB, nil) {
		t.Fatal("distinct-table join filter child order should not change canonical hash")
	}

	projectionDrift := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, ProjectRight: true, Filter: And{Left: a, Right: b}}
	if ComputeQueryHash(joinA, nil) == ComputeQueryHash(projectionDrift, nil) {
		t.Fatal("join projection side must still change canonical hash")
	}
}

func TestQueryHashSelfJoinFilterChildOrderCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	joinA := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: b}}
	joinB := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: b, Right: a}}
	if ComputeQueryHash(joinA, nil) != ComputeQueryHash(joinB, nil) {
		t.Fatal("self-join filter child order should not change canonical hash")
	}

	aliasDrift := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: ColEq{Table: 1, Column: 0, Alias: 1, Value: types.NewUint32(1)}, Right: b}}
	if ComputeQueryHash(joinA, nil) == ComputeQueryHash(aliasDrift, nil) {
		t.Fatal("self-join filter alias identity must still change canonical hash")
	}
}

func TestQueryHashSelfJoinFilterAssociativeGroupingCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	c := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Unbounded: true}, Upper: Bound{Value: types.NewUint32(2), Inclusive: false}}
	leftGrouped := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: And{Left: a, Right: b}, Right: c}}
	rightGrouped := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: And{Left: b, Right: c}}}
	if ComputeQueryHash(leftGrouped, nil) != ComputeQueryHash(rightGrouped, nil) {
		t.Fatal("self-join filter grouping should not change canonical hash")
	}

	aliasDrift := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: And{Left: ColEq{Table: 1, Column: 0, Alias: 1, Value: types.NewUint32(1)}, Right: b}, Right: c}}
	if ComputeQueryHash(leftGrouped, nil) == ComputeQueryHash(aliasDrift, nil) {
		t.Fatal("self-join filter alias identity must still change canonical hash")
	}
}

func TestQueryHashSelfJoinFilterDuplicateLeafCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	single := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: a}
	duplicateAnd := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: a}}
	duplicateOr := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: Or{Left: a, Right: a}}
	if ComputeQueryHash(single, nil) != ComputeQueryHash(duplicateAnd, nil) {
		t.Fatal("self-join duplicate And leaf should share canonical hash with single leaf")
	}
	if ComputeQueryHash(single, nil) != ComputeQueryHash(duplicateOr, nil) {
		t.Fatal("self-join duplicate Or leaf should share canonical hash with single leaf")
	}

	aliasDrift := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: ColEq{Table: 1, Column: 0, Alias: 1, Value: types.NewUint32(1)}}}
	if ComputeQueryHash(single, nil) == ComputeQueryHash(aliasDrift, nil) {
		t.Fatal("self-join filter alias identity must still change canonical hash")
	}
}

func TestQueryHashSelfJoinFilterAbsorptionCanonicalized(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	single := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: a}
	absorbedOr := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: Or{Left: a, Right: And{Left: a, Right: b}}}
	absorbedAnd := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: Or{Left: a, Right: b}}}
	if ComputeQueryHash(single, nil) != ComputeQueryHash(absorbedOr, nil) {
		t.Fatal("self-join absorbed Or shape should share canonical hash with single leaf")
	}
	if ComputeQueryHash(single, nil) != ComputeQueryHash(absorbedAnd, nil) {
		t.Fatal("self-join absorbed And shape should share canonical hash with single leaf")
	}

	aliasDrift := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: ColEq{Table: 1, Column: 0, Alias: 1, Value: types.NewUint32(1)}}
	if ComputeQueryHash(single, nil) == ComputeQueryHash(aliasDrift, nil) {
		t.Fatal("self-join filter alias identity must still change canonical hash")
	}
}

func TestQueryHashJoinFilterDiffers(t *testing.T) {
	withoutF := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: nil}
	withF := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: ColEq{Table: 2, Column: 1, Value: types.NewInt32(7)}}
	if ComputeQueryHash(withoutF, nil) == ComputeQueryHash(withF, nil) {
		t.Fatal("Join with vs without filter should differ")
	}
}

// self-join projection contract: ProjectRight is part of the canonical identity because
// `SELECT lhs.*` and `SELECT rhs.*` produce rows of different shape and are
// distinct queries. Same Join sides must hash differently for the two
// projections so the registry does not collapse them.
func TestQueryHashJoinProjectionDiffers(t *testing.T) {
	left := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, ProjectRight: false}
	right := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, ProjectRight: true}
	if ComputeQueryHash(left, nil) == ComputeQueryHash(right, nil) {
		t.Fatal("Join projection side must change canonical hash")
	}
}

func TestQueryPlanHashProjectionColumnsDiffer(t *testing.T) {
	pred := AllRows{Table: 1}
	idProjection := []ProjectionColumn{{
		Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
		Table:  1,
		Column: 0,
	}}
	bodyProjection := []ProjectionColumn{{
		Schema: schema.ColumnSchema{Index: 1, Name: "text", Type: types.KindString},
		Table:  1,
		Column: 1,
	}}
	if ComputeQueryPlanHash(pred, idProjection, nil) == ComputeQueryPlanHash(pred, bodyProjection, nil) {
		t.Fatal("query plan hash must include live projection column identity")
	}
	if ComputeQueryHash(pred, nil) != ComputeQueryPlanHash(pred, nil, nil) {
		t.Fatal("empty query plan projection must preserve predicate hash")
	}
}

func TestQueryHashCrossJoinProjectionAndAliasesDiffer(t *testing.T) {
	left := CrossJoin{Left: 1, Right: 1, LeftAlias: 0, RightAlias: 1, ProjectRight: false}
	right := CrossJoin{Left: 1, Right: 1, LeftAlias: 0, RightAlias: 1, ProjectRight: true}
	aliasDrift := CrossJoin{Left: 1, Right: 1, LeftAlias: 2, RightAlias: 3, ProjectRight: false}
	if ComputeQueryHash(left, nil) == ComputeQueryHash(right, nil) {
		t.Fatal("CrossJoin projection side must change canonical hash")
	}
	if ComputeQueryHash(left, nil) == ComputeQueryHash(aliasDrift, nil) {
		t.Fatal("CrossJoin alias identity must change canonical hash")
	}
}

func TestQueryHashStringIs64Hex(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	h := ComputeQueryHash(p, nil)
	s := h.String()
	if len(s) != 64 {
		t.Fatalf("hex len = %d, want 64", len(s))
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			t.Fatalf("non-hex char %q in %s", c, s)
		}
	}
}

func TestQueryHashAllKindsRoundTrip(t *testing.T) {
	// Ensure all kinds can be hashed without panicking.
	f32, _ := types.NewFloat32(1.5)
	f64, _ := types.NewFloat64(2.25)
	cases := []Value{
		types.NewBool(true),
		types.NewInt8(-1),
		types.NewUint8(1),
		types.NewInt16(-1),
		types.NewUint16(1),
		types.NewInt32(-1),
		types.NewUint32(1),
		types.NewInt64(-1),
		types.NewUint64(1),
		f32,
		f64,
		types.NewString("hi"),
		types.NewBytes([]byte{1, 2, 3}),
		types.NewInt128(0, 127),
		types.NewInt128(-1, ^uint64(0)),
		types.NewUint128(0, 127),
		types.NewUint128(^uint64(0), ^uint64(0)),
		types.NewInt256(0, 0, 0, 127),
		types.NewInt256(-1, ^uint64(0), ^uint64(0), ^uint64(0)),
		types.NewUint256(0, 0, 0, 127),
		types.NewUint256(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)),
		types.NewTimestamp(0),
		types.NewTimestamp(-1),
		types.NewTimestamp(1_739_202_330_000_000),
		types.NewArrayString(nil),
		types.NewArrayString([]string{"alpha"}),
		types.NewArrayString([]string{"alpha", "beta"}),
	}
	for _, v := range cases {
		p := ColEq{Table: 1, Column: 0, Value: v}
		h := ComputeQueryHash(p, nil)
		if h == (QueryHash{}) {
			t.Fatalf("zero hash for kind %s", v.Kind())
		}
	}
}

// TestQueryHashArrayStringVsString pins that an ArrayString with a single
// element and a scalar String with the same payload hash to different digests
// (kind tag + length prefix separate them).
func TestQueryHashArrayStringVsString(t *testing.T) {
	arr := ColEq{Table: 1, Column: 0, Value: types.NewArrayString([]string{"alpha"})}
	str := ColEq{Table: 1, Column: 0, Value: types.NewString("alpha")}
	if ComputeQueryHash(arr, nil) == ComputeQueryHash(str, nil) {
		t.Fatal("ArrayString{'alpha'} and String 'alpha' should hash differently")
	}
}

// TestQueryHashArrayStringDiffersByPayload pins that element-level payload
// differences (length, ordering, content) each perturb the canonical hash.
func TestQueryHashArrayStringDiffersByPayload(t *testing.T) {
	empty := ColEq{Table: 1, Column: 0, Value: types.NewArrayString(nil)}
	single := ColEq{Table: 1, Column: 0, Value: types.NewArrayString([]string{"alpha"})}
	pair := ColEq{Table: 1, Column: 0, Value: types.NewArrayString([]string{"alpha", "beta"})}
	reversed := ColEq{Table: 1, Column: 0, Value: types.NewArrayString([]string{"beta", "alpha"})}
	h1 := ComputeQueryHash(empty, nil)
	h2 := ComputeQueryHash(single, nil)
	h3 := ComputeQueryHash(pair, nil)
	h4 := ComputeQueryHash(reversed, nil)
	if h1 == h2 || h1 == h3 || h2 == h3 || h3 == h4 {
		t.Fatalf("distinct ArrayString payloads hashed to equal: %v %v %v %v", h1, h2, h3, h4)
	}
}

// TestQueryHashInt128VsUint128 pins that distinct 128-bit kinds with the same
// payload produce different canonical hashes (tag byte separates them).
func TestQueryHashInt128VsUint128(t *testing.T) {
	iv := ColEq{Table: 1, Column: 0, Value: types.NewInt128(0, 127)}
	uv := ColEq{Table: 1, Column: 0, Value: types.NewUint128(0, 127)}
	if ComputeQueryHash(iv, nil) == ComputeQueryHash(uv, nil) {
		t.Fatal("Int128 and Uint128 with same payload should produce different hashes")
	}
}

// TestQueryHashInt256VsUint256 pins that distinct 256-bit kinds with the same
// payload produce different canonical hashes (tag byte separates them).
func TestQueryHashInt256VsUint256(t *testing.T) {
	iv := ColEq{Table: 1, Column: 0, Value: types.NewInt256(0, 0, 0, 127)}
	uv := ColEq{Table: 1, Column: 0, Value: types.NewUint256(0, 0, 0, 127)}
	if ComputeQueryHash(iv, nil) == ComputeQueryHash(uv, nil) {
		t.Fatal("Int256 and Uint256 with same payload should produce different hashes")
	}
}

// TestQueryHashInt256DiffersByPayload pins that different 256-bit payloads
// produce different canonical hashes across every word slot.
func TestQueryHashInt256DiffersByPayload(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewInt256(0, 0, 0, 127)}
	b := ColEq{Table: 1, Column: 0, Value: types.NewInt256(0, 0, 0, 128)}
	c := ColEq{Table: 1, Column: 0, Value: types.NewInt256(0, 0, 1, 127)}
	d := ColEq{Table: 1, Column: 0, Value: types.NewInt256(1, 0, 0, 127)}
	h1 := ComputeQueryHash(a, nil)
	h2 := ComputeQueryHash(b, nil)
	h3 := ComputeQueryHash(c, nil)
	h4 := ComputeQueryHash(d, nil)
	if h1 == h2 || h1 == h3 || h1 == h4 || h2 == h3 || h2 == h4 || h3 == h4 {
		t.Fatalf("distinct Int256 payloads hashed to equal: %v %v %v %v", h1, h2, h3, h4)
	}
}

// TestQueryHashInt128DiffersByPayload pins that different 128-bit payloads
// produce different canonical hashes.
func TestQueryHashInt128DiffersByPayload(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewInt128(0, 127)}
	b := ColEq{Table: 1, Column: 0, Value: types.NewInt128(0, 128)}
	c := ColEq{Table: 1, Column: 0, Value: types.NewInt128(1, 127)}
	h1 := ComputeQueryHash(a, nil)
	h2 := ComputeQueryHash(b, nil)
	h3 := ComputeQueryHash(c, nil)
	if h1 == h2 || h1 == h3 || h2 == h3 {
		t.Fatalf("distinct Int128 payloads hashed to equal: %v %v %v", h1, h2, h3)
	}
}

// TestQueryHashTimestampVsInt64 pins that a Timestamp with the same raw i64
// payload as an Int64 produces a different canonical hash (the kind tag byte
// separates them).
func TestQueryHashTimestampVsInt64(t *testing.T) {
	ts := ColEq{Table: 1, Column: 0, Value: types.NewTimestamp(1_739_202_330_000_000)}
	i64 := ColEq{Table: 1, Column: 0, Value: types.NewInt64(1_739_202_330_000_000)}
	if ComputeQueryHash(ts, nil) == ComputeQueryHash(i64, nil) {
		t.Fatal("Timestamp and Int64 with same i64 payload should produce different hashes")
	}
}

// TestQueryHashTimestampDiffersByPayload pins that different timestamp micros
// produce different canonical hashes.
func TestQueryHashTimestampDiffersByPayload(t *testing.T) {
	a := ColEq{Table: 1, Column: 0, Value: types.NewTimestamp(1_739_202_330_000_000)}
	b := ColEq{Table: 1, Column: 0, Value: types.NewTimestamp(1_739_202_330_000_001)}
	if ComputeQueryHash(a, nil) == ComputeQueryHash(b, nil) {
		t.Fatal("distinct Timestamp micros should hash differently")
	}
}

func TestQueryHashRangeBoundDiffers(t *testing.T) {
	inclusive := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(0), Inclusive: true},
		Upper: Bound{Unbounded: true}}
	exclusive := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(0), Inclusive: false},
		Upper: Bound{Unbounded: true}}
	if ComputeQueryHash(inclusive, nil) == ComputeQueryHash(exclusive, nil) {
		t.Fatal("inclusive vs exclusive lower bound should differ")
	}
}
