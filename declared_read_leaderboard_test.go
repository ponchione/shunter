package shunter

import (
	"context"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

const (
	leaderboardMatchesTableID uint32 = iota
	leaderboardRowsTableID
	leaderboardStageScoresTableID
)

const (
	leaderboardMatchA = "match-a"
	leaderboardMatchB = "match-b"
)

func TestSubscribeViewKickbrassLikeFlattenedLeaderboardInitialRows(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, leaderboardPilotModule())
	defer rt.Close()

	empty, err := rt.SubscribeView(context.Background(), "live_flattened_leaderboard", 201,
		WithDeclaredReadConnectionID(types.ConnectionID{0xA1}),
		WithDeclaredReadPermissions("leaderboard:subscribe"),
		WithDeclaredReadParameters(types.ProductValue{types.NewString(leaderboardMatchA)}),
	)
	if err != nil {
		t.Fatalf("SubscribeView empty: %v", err)
	}
	assertLeaderboardProjectionColumns(t, empty.Columns)
	if len(empty.InitialRows) != 0 {
		t.Fatalf("empty initial rows = %#v, want none", empty.InitialRows)
	}

	callLeaderboardReducer(t, rt, "seed_leaderboard_match_a")
	seeded, err := rt.SubscribeView(context.Background(), "live_flattened_leaderboard", 202,
		WithDeclaredReadConnectionID(types.ConnectionID{0xA2}),
		WithDeclaredReadPermissions("leaderboard:subscribe"),
		WithDeclaredReadParameters(types.ProductValue{types.NewString(leaderboardMatchA)}),
	)
	if err != nil {
		t.Fatalf("SubscribeView seeded: %v", err)
	}
	assertLeaderboardProjectionColumns(t, seeded.Columns)
	assertProductValueBag(t, seeded.InitialRows, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 1, 88, "A-1", "v-a"),
	}, "seeded initial rows")
}

func TestSubscribeViewKickbrassLikeFlattenedLeaderboardDeltas(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, leaderboardPilotModule())
	defer rt.Close()
	callLeaderboardReducer(t, rt, "seed_leaderboard_match_a")

	capture := newDeclaredReadLocalFanOutSender(8)
	installDeclaredReadLocalFanOutSender(t, rt, capture)
	connID := types.ConnectionID{0xD1}
	rt.fanOutWorker.SetConfirmedReads(connID, false)

	sub, err := rt.SubscribeView(context.Background(), "live_flattened_leaderboard", 301,
		WithDeclaredReadConnectionID(connID),
		WithDeclaredReadPermissions("leaderboard:subscribe"),
		WithDeclaredReadParameters(types.ProductValue{types.NewString(leaderboardMatchA)}),
	)
	if err != nil {
		t.Fatalf("SubscribeView seeded: %v", err)
	}
	assertProductValueBag(t, sub.InitialRows, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 1, 88, "A-1", "v-a"),
	}, "subscription initial rows")

	callLeaderboardReducer(t, rt, "insert_leaderboard_stage_two")
	update := requireLeaderboardLightUpdate(t, capture, connID, 301)
	assertProductValueBag(t, update.Inserts, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 2, 92, "A-2", "v-a"),
	}, "stage insert delta inserts")
	assertProductValueBag(t, update.Deletes, nil, "stage insert delta deletes")

	callLeaderboardReducer(t, rt, "update_leaderboard_stage_two")
	update = requireLeaderboardLightUpdate(t, capture, connID, 301)
	assertProductValueBag(t, update.Inserts, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 2, 96, "A-2-updated", "v-a"),
	}, "stage update delta inserts")
	assertProductValueBag(t, update.Deletes, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 2, 92, "A-2", "v-a"),
	}, "stage update delta deletes")

	callLeaderboardReducer(t, rt, "delete_leaderboard_stage_two")
	update = requireLeaderboardLightUpdate(t, capture, connID, 301)
	assertProductValueBag(t, update.Inserts, nil, "stage delete delta inserts")
	assertProductValueBag(t, update.Deletes, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 2, 96, "A-2-updated", "v-a"),
	}, "stage delete delta deletes")

	callLeaderboardReducer(t, rt, "move_leaderboard_row_out")
	update = requireLeaderboardLightUpdate(t, capture, connID, 301)
	assertProductValueBag(t, update.Inserts, nil, "row move-out delta inserts")
	assertProductValueBag(t, update.Deletes, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 1, 88, "A-1", "v-a"),
	}, "row move-out delta deletes")

	callLeaderboardReducer(t, rt, "move_leaderboard_row_in")
	update = requireLeaderboardLightUpdate(t, capture, connID, 301)
	assertProductValueBag(t, update.Inserts, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 1, 88, "A-1", "v-a"),
	}, "row move-in delta inserts")
	assertProductValueBag(t, update.Deletes, nil, "row move-in delta deletes")

	callLeaderboardReducer(t, rt, "update_leaderboard_source_and_stage")
	update = requireLeaderboardLightUpdate(t, capture, connID, 301)
	assertProductValueBag(t, update.Inserts, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 1, 93, "A-1-source-stage", "v-a2"),
	}, "source and stage update delta inserts")
	assertProductValueBag(t, update.Deletes, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchA, 1, 88, "A-1", "v-a"),
	}, "source and stage update delta deletes")
}

func TestSubscribeViewKickbrassLikeFlattenedLeaderboardParameterIsolation(t *testing.T) {
	rt := buildStartedDeclaredReadRuntime(t, leaderboardPilotModule())
	defer rt.Close()
	callLeaderboardReducer(t, rt, "seed_leaderboard_match_a")

	capture := newDeclaredReadLocalFanOutSender(4)
	installDeclaredReadLocalFanOutSender(t, rt, capture)
	alphaConn := types.ConnectionID{0xE1}
	bravoConn := types.ConnectionID{0xE2}
	rt.fanOutWorker.SetConfirmedReads(alphaConn, false)
	rt.fanOutWorker.SetConfirmedReads(bravoConn, false)

	if _, err := rt.SubscribeView(context.Background(), "live_flattened_leaderboard", 401,
		WithDeclaredReadConnectionID(alphaConn),
		WithDeclaredReadPermissions("leaderboard:subscribe"),
		WithDeclaredReadParameters(types.ProductValue{types.NewString(leaderboardMatchA)}),
	); err != nil {
		t.Fatalf("SubscribeView match-a: %v", err)
	}
	if _, err := rt.SubscribeView(context.Background(), "live_flattened_leaderboard", 402,
		WithDeclaredReadConnectionID(bravoConn),
		WithDeclaredReadPermissions("leaderboard:subscribe"),
		WithDeclaredReadParameters(types.ProductValue{types.NewString(leaderboardMatchB)}),
	); err != nil {
		t.Fatalf("SubscribeView match-b: %v", err)
	}

	callLeaderboardReducer(t, rt, "insert_leaderboard_match_b")
	update := requireLeaderboardLightUpdate(t, capture, bravoConn, 402)
	assertProductValueBag(t, update.Inserts, []types.ProductValue{
		leaderboardProjectedRow(leaderboardMatchB, 1, 77, "B-1", "v-b"),
	}, "match-b isolated delta inserts")
	assertProductValueBag(t, update.Deletes, nil, "match-b isolated delta deletes")
	capture.requireNoLight(t)
}

func leaderboardPilotModule() *Module {
	return NewModule("leaderboard_pilot").
		SchemaVersion(1).
		TableDef(leaderboardMatchesTableDef()).
		TableDef(leaderboardRowsTableDef()).
		TableDef(leaderboardStageScoresTableDef()).
		Reducer("seed_leaderboard_match_a", seedLeaderboardMatchAReducer).
		Reducer("insert_leaderboard_stage_two", insertLeaderboardStageTwoReducer).
		Reducer("update_leaderboard_stage_two", updateLeaderboardStageTwoReducer).
		Reducer("delete_leaderboard_stage_two", deleteLeaderboardStageTwoReducer).
		Reducer("move_leaderboard_row_out", moveLeaderboardRowOutReducer).
		Reducer("move_leaderboard_row_in", moveLeaderboardRowInReducer).
		Reducer("update_leaderboard_source_and_stage", updateLeaderboardSourceAndStageReducer).
		Reducer("insert_leaderboard_match_b", insertLeaderboardMatchBReducer).
		View(ViewDeclaration{
			Name:        "live_flattened_leaderboard",
			SQL:         leaderboardFlattenedSQL(),
			Permissions: PermissionMetadata{Required: []string{"leaderboard:subscribe"}},
		}, WithViewParameters(ProductSchema{Columns: []ProductColumn{
			{Name: "match_id", Type: "string"},
		}}))
}

func leaderboardFlattenedSQL() string {
	return `SELECT
  r.match_id AS match_id,
  r.account_id AS account_id,
  r.first_name AS first_name,
  r.last_name AS last_name,
  r.division AS division,
  r.class AS class,
  r.rank AS rank,
  r.total_points AS total_points,
  r.match_percentage AS match_percentage,
  s.stage_id AS stage_id,
  s.stage_name AS stage_name,
  s.stage_order AS stage_order,
  s.match_points AS match_points,
  s.stage_rank AS stage_rank,
  s.raw_points AS raw_points,
  s.total_time AS total_time,
  s.hit_factor AS hit_factor,
  s.score_data AS score_data,
  m.source_version AS source_version,
  r.updated_at AS updated_at
FROM leaderboard_rows AS r
JOIN leaderboard_matches AS m ON r.match_id = m.match_id
JOIN leaderboard_stage_scores AS s ON r.match_id = s.match_id
WHERE r.match_id = :match_id AND r.account_id = s.account_id`
}

func leaderboardMatchesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "leaderboard_matches",
		Columns: []schema.ColumnDefinition{
			{Name: "match_id", Type: types.KindString, PrimaryKey: true},
			{Name: "source_version", Type: types.KindString},
		},
	}
}

func leaderboardRowsTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "leaderboard_rows",
		Columns: []schema.ColumnDefinition{
			{Name: "match_id", Type: types.KindString},
			{Name: "account_id", Type: types.KindUint64},
			{Name: "first_name", Type: types.KindString},
			{Name: "last_name", Type: types.KindString},
			{Name: "division", Type: types.KindString},
			{Name: "class", Type: types.KindString},
			{Name: "rank", Type: types.KindUint64},
			{Name: "total_points", Type: types.KindUint64},
			{Name: "match_percentage", Type: types.KindUint64},
			{Name: "updated_at", Type: types.KindString},
		},
		Indexes: []schema.IndexDefinition{
			{Name: "idx_leaderboard_rows_match_id", Columns: []string{"match_id"}},
			{Name: "idx_leaderboard_rows_account_id", Columns: []string{"account_id"}},
		},
	}
}

func leaderboardStageScoresTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "leaderboard_stage_scores",
		Columns: []schema.ColumnDefinition{
			{Name: "match_id", Type: types.KindString},
			{Name: "account_id", Type: types.KindUint64},
			{Name: "stage_id", Type: types.KindUint64},
			{Name: "stage_name", Type: types.KindString},
			{Name: "stage_order", Type: types.KindUint64},
			{Name: "match_points", Type: types.KindUint64},
			{Name: "stage_rank", Type: types.KindUint64},
			{Name: "raw_points", Type: types.KindUint64},
			{Name: "total_time", Type: types.KindUint64},
			{Name: "hit_factor", Type: types.KindUint64},
			{Name: "score_data", Type: types.KindString},
		},
		Indexes: []schema.IndexDefinition{
			{Name: "idx_leaderboard_stage_scores_match_id", Columns: []string{"match_id"}},
			{Name: "idx_leaderboard_stage_scores_account_id", Columns: []string{"account_id"}},
		},
	}
}

func seedLeaderboardMatchAReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	if err := insertLeaderboardBase(ctx, leaderboardMatchA, "v-a"); err != nil {
		return nil, err
	}
	_, err := ctx.DB.Insert(leaderboardStageScoresTableID, leaderboardStageScoreRow(leaderboardMatchA, 1, 88, "A-1"))
	return nil, err
}

func insertLeaderboardStageTwoReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	_, err := ctx.DB.Insert(leaderboardStageScoresTableID, leaderboardStageScoreRow(leaderboardMatchA, 2, 92, "A-2"))
	return nil, err
}

func updateLeaderboardStageTwoReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	return nil, updateLeaderboardStageScore(ctx, leaderboardMatchA, 2, leaderboardStageScoreRow(leaderboardMatchA, 2, 96, "A-2-updated"))
}

func deleteLeaderboardStageTwoReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	return nil, deleteLeaderboardStageScore(ctx, leaderboardMatchA, 2)
}

func moveLeaderboardRowOutReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	return nil, updateLeaderboardRowMatch(ctx, leaderboardMatchA, "archived-match")
}

func moveLeaderboardRowInReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	return nil, updateLeaderboardRowMatch(ctx, "archived-match", leaderboardMatchA)
}

func updateLeaderboardSourceAndStageReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	if err := updateLeaderboardMatchSource(ctx, leaderboardMatchA, "v-a2"); err != nil {
		return nil, err
	}
	return nil, updateLeaderboardStageScore(ctx, leaderboardMatchA, 1, leaderboardStageScoreRow(leaderboardMatchA, 1, 93, "A-1-source-stage"))
}

func insertLeaderboardMatchBReducer(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
	if err := insertLeaderboardBase(ctx, leaderboardMatchB, "v-b"); err != nil {
		return nil, err
	}
	_, err := ctx.DB.Insert(leaderboardStageScoresTableID, leaderboardStageScoreRow(leaderboardMatchB, 1, 77, "B-1"))
	return nil, err
}

func insertLeaderboardBase(ctx *schema.ReducerContext, matchID, sourceVersion string) error {
	if _, err := ctx.DB.Insert(leaderboardMatchesTableID, types.ProductValue{
		types.NewString(matchID),
		types.NewString(sourceVersion),
	}); err != nil {
		return err
	}
	_, err := ctx.DB.Insert(leaderboardRowsTableID, leaderboardBaseRow(matchID))
	return err
}

func updateLeaderboardStageScore(ctx *schema.ReducerContext, matchID string, stageID uint64, row types.ProductValue) error {
	for rowID, existing := range ctx.DB.ScanTable(leaderboardStageScoresTableID) {
		if existing[0].AsString() == matchID && existing[2].AsUint64() == stageID {
			_, err := ctx.DB.Update(leaderboardStageScoresTableID, rowID, row)
			return err
		}
	}
	return fmt.Errorf("stage score %s/%d not found", matchID, stageID)
}

func updateLeaderboardMatchSource(ctx *schema.ReducerContext, matchID string, sourceVersion string) error {
	for rowID, row := range ctx.DB.ScanTable(leaderboardMatchesTableID) {
		if row[0].AsString() == matchID {
			next := row.Copy()
			next[1] = types.NewString(sourceVersion)
			_, err := ctx.DB.Update(leaderboardMatchesTableID, rowID, next)
			return err
		}
	}
	return fmt.Errorf("leaderboard match %s not found", matchID)
}

func deleteLeaderboardStageScore(ctx *schema.ReducerContext, matchID string, stageID uint64) error {
	for rowID, row := range ctx.DB.ScanTable(leaderboardStageScoresTableID) {
		if row[0].AsString() == matchID && row[2].AsUint64() == stageID {
			return ctx.DB.Delete(leaderboardStageScoresTableID, rowID)
		}
	}
	return fmt.Errorf("stage score %s/%d not found", matchID, stageID)
}

func updateLeaderboardRowMatch(ctx *schema.ReducerContext, fromMatchID, toMatchID string) error {
	for rowID, row := range ctx.DB.ScanTable(leaderboardRowsTableID) {
		if row[0].AsString() == fromMatchID && row[1].AsUint64() == 7 {
			next := row.Copy()
			next[0] = types.NewString(toMatchID)
			_, err := ctx.DB.Update(leaderboardRowsTableID, rowID, next)
			return err
		}
	}
	return fmt.Errorf("leaderboard row %s not found", fromMatchID)
}

func leaderboardBaseRow(matchID string) types.ProductValue {
	return types.ProductValue{
		types.NewString(matchID),
		types.NewUint64(7),
		types.NewString("Alice"),
		types.NewString("Zephyr"),
		types.NewString("Open"),
		types.NewString("A"),
		types.NewUint64(1),
		types.NewUint64(188),
		types.NewUint64(100),
		types.NewString("2026-05-25T12:00:00Z"),
	}
}

func leaderboardStageScoreRow(matchID string, stageID uint64, points uint64, scoreData string) types.ProductValue {
	return types.ProductValue{
		types.NewString(matchID),
		types.NewUint64(7),
		types.NewUint64(stageID),
		types.NewString(fmt.Sprintf("Stage %d", stageID)),
		types.NewUint64(stageID),
		types.NewUint64(points),
		types.NewUint64(stageID),
		types.NewUint64(points + 5),
		types.NewUint64(30 + stageID),
		types.NewUint64(10 + stageID),
		types.NewString(scoreData),
	}
}

func leaderboardProjectedRow(matchID string, stageID uint64, points uint64, scoreData string, sourceVersion string) types.ProductValue {
	stage := leaderboardStageScoreRow(matchID, stageID, points, scoreData)
	return types.ProductValue{
		types.NewString(matchID),
		types.NewUint64(7),
		types.NewString("Alice"),
		types.NewString("Zephyr"),
		types.NewString("Open"),
		types.NewString("A"),
		types.NewUint64(1),
		types.NewUint64(188),
		types.NewUint64(100),
		stage[2],
		stage[3],
		stage[4],
		stage[5],
		stage[6],
		stage[7],
		stage[8],
		stage[9],
		stage[10],
		types.NewString(sourceVersion),
		types.NewString("2026-05-25T12:00:00Z"),
	}
}

func callLeaderboardReducer(t *testing.T, rt *Runtime, name string) {
	t.Helper()
	res, err := rt.CallReducer(context.Background(), name, nil)
	if err != nil {
		t.Fatalf("%s admission: %v", name, err)
	}
	if res.Status != StatusCommitted {
		t.Fatalf("%s status = %v, err = %v, want committed", name, res.Status, res.Error)
	}
}

func requireLeaderboardLightUpdate(t *testing.T, capture *declaredReadLocalFanOutSender, connID types.ConnectionID, queryID uint32) subscription.SubscriptionUpdate {
	t.Helper()
	got := capture.requireLight(t)
	if got.connID != connID {
		t.Fatalf("fan-out connID = %x, want %x", got.connID, connID)
	}
	if len(got.updates) != 1 {
		t.Fatalf("fan-out updates = %+v, want one", got.updates)
	}
	update := got.updates[0]
	if update.QueryID != queryID {
		t.Fatalf("fan-out QueryID = %d, want %d", update.QueryID, queryID)
	}
	if update.TableName != "leaderboard_rows" {
		t.Fatalf("fan-out table = %q, want leaderboard_rows", update.TableName)
	}
	assertLeaderboardProjectionColumns(t, update.Columns)
	return update
}

func assertLeaderboardProjectionColumns(t *testing.T, columns []schema.ColumnSchema) {
	t.Helper()
	want := []string{
		"match_id",
		"account_id",
		"first_name",
		"last_name",
		"division",
		"class",
		"rank",
		"total_points",
		"match_percentage",
		"stage_id",
		"stage_name",
		"stage_order",
		"match_points",
		"stage_rank",
		"raw_points",
		"total_time",
		"hit_factor",
		"score_data",
		"source_version",
		"updated_at",
	}
	if len(columns) != len(want) {
		t.Fatalf("projection columns len = %d, want %d (%#v)", len(columns), len(want), columns)
	}
	for i, name := range want {
		if columns[i].Name != name {
			t.Fatalf("projection column %d = %q, want %q; all=%#v", i, columns[i].Name, name, columns)
		}
	}
}

func assertProductValueBag(t *testing.T, got, want []types.ProductValue, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d; got=%#v want=%#v", label, len(got), len(want), got, want)
	}
	used := make([]bool, len(got))
	for _, wantRow := range want {
		found := false
		for i, gotRow := range got {
			if used[i] || !gotRow.Equal(wantRow) {
				continue
			}
			used[i] = true
			found = true
			break
		}
		if !found {
			t.Fatalf("%s missing row %#v in %#v", label, wantRow, got)
		}
	}
}
