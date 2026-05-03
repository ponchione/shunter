package shunter_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	readAuthMessagesTableID schema.TableID = iota
	readAuthSecretsTableID
	readAuthAuditLogsTableID
	readAuthProfilesTableID
)

func TestRuntimeGauntletReadAuthorizationEndToEnd(t *testing.T) {
	signingKey := []byte("read-auth-gauntlet-signing-key")
	rt := buildReadAuthGauntletRuntime(t, shunter.Config{
		DataDir:        t.TempDir(),
		AuthMode:       shunter.AuthModeStrict,
		AuthSigningKey: signingKey,
	})
	defer rt.Close()

	srv := httptest.NewServer(rt.HTTPHandler())
	defer srv.Close()
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"

	alice, aliceIdentity := dialReadAuthGauntletProtocol(t, url, signingKey, "alice", "secrets:read", "secrets:subscribe")
	defer alice.CloseNow()
	bob, bobIdentity := dialReadAuthGauntletProtocol(t, url, signingKey, "bob")
	defer bob.CloseNow()
	auditor, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "auditor", "audit:read")
	defer auditor.CloseNow()
	missingPerm, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "missing")
	defer missingPerm.CloseNow()

	aliceOwner := types.Identity(aliceIdentity.Identity).Hex()
	bobOwner := types.Identity(bobIdentity.Identity).Hex()
	seedReadAuthGauntletRows(t, rt, aliceOwner, bobOwner)
	assertReadAuthGauntletContractMetadata(t, rt)

	assertReadAuthOneOffErrorContains(t, alice, "SELECT * FROM secrets", []byte("raw-private"), "no such table: `secrets`. If the table exists, it may be marked private.", "raw private one-off")
	assertReadAuthSubscribeErrorContains(t, alice, "SELECT * FROM secrets", 101, 201, "no such table: `secrets`. If the table exists, it may be marked private.", "raw private subscribe")
	assertReadAuthOneOffErrorContains(t, missingPerm, "SELECT * FROM audit_logs", []byte("audit-denied"), "no such table: `audit_logs`. If the table exists, it may be marked private.", "permissioned table denied")
	assertReadAuthOneOffErrorContains(t, alice, "SELECT profiles.* FROM profiles JOIN secrets ON profiles.owner = secrets.owner", []byte("join-private"), "no such table: `secrets`. If the table exists, it may be marked private.", "private non-projected join side")

	aliceRows := queryReadAuthMessages(t, alice, "SELECT * FROM messages", []byte("alice-messages"), "alice raw messages")
	assertReadAuthMessageRowsEqual(t, aliceRows, []readAuthMessageRow{
		{ID: 2, Owner: aliceOwner, Channel: "shared", Body: "alice shared"},
		{ID: 3, Owner: "public", Channel: "public", Body: "public notice"},
	}, "alice raw messages")
	bobRows := queryReadAuthMessages(t, bob, "SELECT * FROM messages", []byte("bob-messages"), "bob raw messages")
	assertReadAuthMessageRowsEqual(t, bobRows, []readAuthMessageRow{
		{ID: 1, Owner: bobOwner, Channel: "shared", Body: "bob shared"},
		{ID: 3, Owner: "public", Channel: "public", Body: "public notice"},
	}, "bob raw messages")

	auditRows := queryReadAuthAuditLogs(t, auditor, "SELECT * FROM audit_logs", []byte("audit-allowed"), "permissioned table allowed")
	assertReadAuthAuditRowsEqual(t, auditRows, []readAuthAuditRow{{ID: 1, Body: "audit seed"}}, "permissioned table allowed")

	aggregateRows := queryReadAuthRows(t, alice, "SELECT COUNT(*) AS n FROM messages", []byte("visible-count"), readAuthAggregateSchema(), "visible aggregate count")
	assertReadAuthProductRowsEqual(t, aggregateRows, []types.ProductValue{{types.NewUint64(2)}}, "visible aggregate count")
	limitRows := queryReadAuthMessages(t, alice, "SELECT * FROM messages LIMIT 1", []byte("visible-limit"), "visible limit")
	if len(limitRows) != 1 || (limitRows[0].Owner != aliceOwner && limitRows[0].Owner != "public") {
		t.Fatalf("visible limit rows = %#v, want one caller-visible row", limitRows)
	}

	selfJoinRows := queryReadAuthMessages(t, alice, "SELECT a.* FROM messages AS a JOIN messages AS b ON a.channel = b.channel", []byte("self-join"), "visibility self join")
	assertReadAuthMessageRowsEqual(t, selfJoinRows, []readAuthMessageRow{
		{ID: 2, Owner: aliceOwner, Channel: "shared", Body: "alice shared"},
		{ID: 3, Owner: "public", Channel: "public", Body: "public notice"},
	}, "visibility self join")
	profileRows := queryReadAuthProfiles(t, alice, "SELECT profiles.* FROM profiles JOIN messages ON profiles.id = messages.id", []byte("filtered-join"), "filtered non-projected join side")
	assertReadAuthProfileRowsEqual(t, profileRows, []readAuthProfileRow{
		{ID: 2, Owner: aliceOwner, Label: "alice profile"},
		{ID: 3, Owner: "public", Label: "public profile"},
	}, "filtered non-projected join side")

	atomicClient, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "atomic", "secrets:read")
	assertReadAuthSubscribeMultiErrorContains(t, atomicClient, []string{"SELECT * FROM messages", "SELECT * FROM secrets"}, 102, 202, "no such table: `secrets`. If the table exists, it may be marked private.", "subscribe multi atomic rejection")
	callReadAuthReducer(t, rt, "insert_message", "4", aliceOwner, "post_atomic", "alice after rejected multi")
	assertNoGauntletProtocolMessageBeforeClose(t, atomicClient, 50*time.Millisecond, "subscribe multi atomic rejection")
	closeReadAuthClient(t, atomicClient, "subscribe multi atomic rejection")

	hiddenMessageSub, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "alice", "secrets:read", "secrets:subscribe")
	hiddenInitial := subscribeReadAuthMessages(t, hiddenMessageSub, "SELECT * FROM messages", 103, 203, "message hidden delta initial")
	assertReadAuthMessageRowsEqual(t, hiddenInitial, []readAuthMessageRow{
		{ID: 2, Owner: aliceOwner, Channel: "shared", Body: "alice shared"},
		{ID: 3, Owner: "public", Channel: "public", Body: "public notice"},
		{ID: 4, Owner: aliceOwner, Channel: "post_atomic", Body: "alice after rejected multi"},
	}, "message hidden delta initial")
	callReadAuthReducer(t, rt, "insert_message", "5", bobOwner, "hidden", "bob hidden")
	assertNoGauntletProtocolMessageBeforeClose(t, hiddenMessageSub, 50*time.Millisecond, "message hidden delta")
	closeReadAuthClient(t, hiddenMessageSub, "message hidden delta")

	visibleMessageSub, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "alice", "secrets:read", "secrets:subscribe")
	subRows := subscribeReadAuthMessages(t, visibleMessageSub, "SELECT * FROM messages", 104, 204, "message visible delta initial")
	assertReadAuthMessageRowsEqual(t, subRows, []readAuthMessageRow{
		{ID: 2, Owner: aliceOwner, Channel: "shared", Body: "alice shared"},
		{ID: 3, Owner: "public", Channel: "public", Body: "public notice"},
		{ID: 4, Owner: aliceOwner, Channel: "post_atomic", Body: "alice after rejected multi"},
	}, "message visible delta initial")
	callReadAuthReducer(t, rt, "insert_message", "6", aliceOwner, "visible", "alice visible delta")
	messageDelta := readAuthSubscriptionDelta(t, visibleMessageSub, 204, "messages", readAuthMessagesSchema(), "message visible delta")
	assertReadAuthMessageRowsEqual(t, readAuthMessageRowsFromValues(messageDelta.inserts), []readAuthMessageRow{
		{ID: 6, Owner: aliceOwner, Channel: "visible", Body: "alice visible delta"},
	}, "message visible delta inserts")
	if len(messageDelta.deletes) != 0 {
		t.Fatalf("message visible delta deletes = %#v, want none", messageDelta.deletes)
	}
	closeReadAuthClient(t, visibleMessageSub, "message visible delta")

	writeGauntletProtocolMessage(t, alice, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-secrets"),
		Name:      "my_secrets",
	}, "declared query private table")
	declaredSecrets := readAuthOneOffRows(t, alice, []byte("declared-secrets"), "secrets", readAuthSecretsSchema(), "declared query private table")
	assertReadAuthSecretRowsEqual(t, readAuthSecretRowsFromValues(declaredSecrets), []readAuthSecretRow{
		{ID: 2, Owner: aliceOwner, Body: "alice secret"},
	}, "declared query private table")
	writeGauntletProtocolMessage(t, bob, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-missing-permission"),
		Name:      "my_secrets",
	}, "declared query missing permission")
	assertReadAuthDeclaredOneOffErrorContains(t, bob, []byte("declared-missing-permission"), "permission denied", "declared query missing permission")
	writeGauntletProtocolMessage(t, alice, protocol.DeclaredQueryMsg{
		MessageID: []byte("declared-unknown"),
		Name:      "SELECT * FROM secrets",
	}, "declared query unknown name")
	assertReadAuthDeclaredOneOffErrorContains(t, alice, []byte("declared-unknown"), "unknown declared read", "declared query unknown name")

	hiddenSecretSub, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "alice", "secrets:read", "secrets:subscribe")
	writeGauntletProtocolMessage(t, hiddenSecretSub, protocol.SubscribeDeclaredViewMsg{
		RequestID: 105,
		QueryID:   205,
		Name:      "my_secret_stream",
	}, "declared view hidden delta initial")
	hiddenSecretInitial := readAuthSubscribeRows(t, hiddenSecretSub, 105, 205, "secrets", readAuthSecretsSchema(), "declared view hidden delta initial")
	assertReadAuthSecretRowsEqual(t, readAuthSecretRowsFromValues(hiddenSecretInitial), []readAuthSecretRow{
		{ID: 2, Owner: aliceOwner, Body: "alice secret"},
	}, "declared view hidden delta initial")
	callReadAuthReducer(t, rt, "insert_secret", "3", bobOwner, "bob hidden secret")
	assertNoGauntletProtocolMessageBeforeClose(t, hiddenSecretSub, 50*time.Millisecond, "declared view hidden delta")
	closeReadAuthClient(t, hiddenSecretSub, "declared view hidden delta")

	visibleSecretSub, _ := dialReadAuthGauntletProtocol(t, url, signingKey, "alice", "secrets:read", "secrets:subscribe")
	writeGauntletProtocolMessage(t, visibleSecretSub, protocol.SubscribeDeclaredViewMsg{
		RequestID: 106,
		QueryID:   206,
		Name:      "my_secret_stream",
	}, "declared view visible delta initial")
	visibleSecretInitial := readAuthSubscribeRows(t, visibleSecretSub, 106, 206, "secrets", readAuthSecretsSchema(), "declared view visible delta initial")
	assertReadAuthSecretRowsEqual(t, readAuthSecretRowsFromValues(visibleSecretInitial), []readAuthSecretRow{
		{ID: 2, Owner: aliceOwner, Body: "alice secret"},
	}, "declared view visible delta initial")
	callReadAuthReducer(t, rt, "insert_secret", "4", aliceOwner, "alice visible secret")
	secretDelta := readAuthSubscriptionDelta(t, visibleSecretSub, 206, "secrets", readAuthSecretsSchema(), "declared view visible delta")
	assertReadAuthSecretRowsEqual(t, readAuthSecretRowsFromValues(secretDelta.inserts), []readAuthSecretRow{
		{ID: 4, Owner: aliceOwner, Body: "alice visible secret"},
	}, "declared view visible delta inserts")
	if len(secretDelta.deletes) != 0 {
		t.Fatalf("declared view visible delta deletes = %#v, want none", secretDelta.deletes)
	}
	closeReadAuthClient(t, visibleSecretSub, "declared view visible delta")
}

func TestRuntimeGauntletReadAuthorizationAllowAllBypassesPolicyAndVisibility(t *testing.T) {
	rt := buildReadAuthGauntletRuntime(t, shunter.Config{DataDir: t.TempDir()})
	defer rt.Close()
	callReadAuthReducer(t, rt, "insert_secret", "1", "first-owner", "first secret")
	callReadAuthReducer(t, rt, "insert_secret", "2", "second-owner", "second secret")
	callReadAuthReducer(t, rt, "insert_audit", "1", "audit seed")

	client := dialGauntletProtocol(t, rt)
	defer client.CloseNow()
	secrets := queryReadAuthSecrets(t, client, "SELECT * FROM secrets", []byte("allow-all-private"), "allow-all private table")
	assertReadAuthSecretRowsEqual(t, secrets, []readAuthSecretRow{
		{ID: 1, Owner: "first-owner", Body: "first secret"},
		{ID: 2, Owner: "second-owner", Body: "second secret"},
	}, "allow-all private table")
	auditRows := queryReadAuthAuditLogs(t, client, "SELECT * FROM audit_logs", []byte("allow-all-permissioned"), "allow-all permissioned table")
	assertReadAuthAuditRowsEqual(t, auditRows, []readAuthAuditRow{{ID: 1, Body: "audit seed"}}, "allow-all permissioned table")
}

func buildReadAuthGauntletRuntime(t *testing.T, cfg shunter.Config) *shunter.Runtime {
	t.Helper()
	cfg.EnableProtocol = true
	mod := shunter.NewModule("read_auth_gauntlet").
		SchemaVersion(1).
		TableDef(readAuthMessagesTableDef(), schema.WithPublicRead()).
		TableDef(readAuthSecretsTableDef()).
		TableDef(readAuthAuditLogsTableDef(), schema.WithReadPermissions("audit:read")).
		TableDef(readAuthProfilesTableDef(), schema.WithPublicRead()).
		VisibilityFilter(shunter.VisibilityFilterDeclaration{
			Name: "own_messages",
			SQL:  "SELECT * FROM messages WHERE owner = :sender",
		}).
		VisibilityFilter(shunter.VisibilityFilterDeclaration{
			Name: "public_messages",
			SQL:  "SELECT * FROM messages WHERE owner = 'public'",
		}).
		VisibilityFilter(shunter.VisibilityFilterDeclaration{
			Name: "own_secrets",
			SQL:  "SELECT * FROM secrets WHERE owner = :sender",
		}).
		Query(shunter.QueryDeclaration{
			Name:        "my_secrets",
			SQL:         "SELECT * FROM secrets",
			Permissions: shunter.PermissionMetadata{Required: []string{"secrets:read"}},
		}).
		View(shunter.ViewDeclaration{
			Name:        "my_secret_stream",
			SQL:         "SELECT * FROM secrets",
			Permissions: shunter.PermissionMetadata{Required: []string{"secrets:subscribe"}},
		}).
		Reducer("insert_message", readAuthInsertMessageReducer).
		Reducer("insert_secret", readAuthInsertSecretReducer).
		Reducer("insert_audit", readAuthInsertAuditReducer).
		Reducer("insert_profile", readAuthInsertProfileReducer)

	rt, err := shunter.Build(mod, cfg)
	if err != nil {
		t.Fatalf("Build read-auth gauntlet runtime: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start read-auth gauntlet runtime: %v", err)
	}
	return rt
}

func seedReadAuthGauntletRows(t *testing.T, rt *shunter.Runtime, aliceOwner, bobOwner string) {
	t.Helper()
	callReadAuthReducer(t, rt, "insert_message", "1", bobOwner, "shared", "bob shared")
	callReadAuthReducer(t, rt, "insert_message", "2", aliceOwner, "shared", "alice shared")
	callReadAuthReducer(t, rt, "insert_message", "3", "public", "public", "public notice")
	callReadAuthReducer(t, rt, "insert_secret", "1", bobOwner, "bob secret")
	callReadAuthReducer(t, rt, "insert_secret", "2", aliceOwner, "alice secret")
	callReadAuthReducer(t, rt, "insert_audit", "1", "audit seed")
	callReadAuthReducer(t, rt, "insert_profile", "1", bobOwner, "bob profile")
	callReadAuthReducer(t, rt, "insert_profile", "2", aliceOwner, "alice profile")
	callReadAuthReducer(t, rt, "insert_profile", "3", "public", "public profile")
}

func assertReadAuthGauntletContractMetadata(t *testing.T, rt *shunter.Runtime) {
	t.Helper()
	contract := rt.ExportContract()
	if policy := readAuthContractTablePolicy(t, contract, "messages"); policy.Access != schema.TableAccessPublic {
		t.Fatalf("messages read policy = %+v, want public", policy)
	}
	if policy := readAuthContractTablePolicy(t, contract, "secrets"); policy.Access != schema.TableAccessPrivate {
		t.Fatalf("secrets read policy = %+v, want private", policy)
	}
	if policy := readAuthContractTablePolicy(t, contract, "audit_logs"); policy.Access != schema.TableAccessPermissioned || !reflect.DeepEqual(policy.Permissions, []string{"audit:read"}) {
		t.Fatalf("audit_logs read policy = %+v, want permissioned audit:read", policy)
	}
	assertReadAuthPermissionContract(t, contract.Permissions.Queries, "my_secrets", []string{"secrets:read"}, "query permissions")
	assertReadAuthPermissionContract(t, contract.Permissions.Views, "my_secret_stream", []string{"secrets:subscribe"}, "view permissions")
	if !readAuthHasVisibilityFilter(contract.VisibilityFilters, "own_messages", "messages", true) ||
		!readAuthHasVisibilityFilter(contract.VisibilityFilters, "public_messages", "messages", false) ||
		!readAuthHasVisibilityFilter(contract.VisibilityFilters, "own_secrets", "secrets", true) {
		t.Fatalf("visibility filters = %+v, want messages and secrets policy metadata", contract.VisibilityFilters)
	}
}

func readAuthContractTablePolicy(t *testing.T, contract shunter.ModuleContract, tableName string) schema.ReadPolicy {
	t.Helper()
	for _, table := range contract.Schema.Tables {
		if table.Name == tableName {
			return table.ReadPolicy
		}
	}
	t.Fatalf("contract missing table %q", tableName)
	return schema.ReadPolicy{}
}

func assertReadAuthPermissionContract(t *testing.T, declarations []shunter.PermissionContractDeclaration, name string, want []string, label string) {
	t.Helper()
	for _, decl := range declarations {
		if decl.Name == name {
			if !reflect.DeepEqual(decl.Required, want) {
				t.Fatalf("%s for %q = %#v, want %#v", label, name, decl.Required, want)
			}
			return
		}
	}
	t.Fatalf("%s missing declaration %q", label, name)
}

func readAuthHasVisibilityFilter(filters []shunter.VisibilityFilterDescription, name, table string, usesCaller bool) bool {
	for _, filter := range filters {
		if filter.Name == name && filter.ReturnTable == table && filter.UsesCallerIdentity == usesCaller {
			return true
		}
	}
	return false
}

func dialReadAuthGauntletProtocol(t *testing.T, url string, signingKey []byte, subject string, permissions ...string) (*websocket.Conn, protocol.IdentityToken) {
	t.Helper()
	token := mintReadAuthGauntletToken(t, signingKey, subject, permissions...)
	return dialGauntletProtocolURLWithHeaders(t, url, gauntletBearerHeader(token), "read-auth "+subject+" dial")
}

func mintReadAuthGauntletToken(t *testing.T, signingKey []byte, subject string, permissions ...string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": "read-auth-gauntlet",
		"sub": subject,
		"iat": time.Now().Unix(),
	}
	if len(permissions) > 0 {
		claims["permissions"] = permissions
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("mint read-auth token: %v", err)
	}
	return signed
}

func readAuthMessagesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "messages",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "owner", Type: types.KindString},
			{Name: "channel", Type: types.KindString},
			{Name: "body", Type: types.KindString},
		},
	}
}

func readAuthSecretsTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "secrets",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "owner", Type: types.KindString},
			{Name: "body", Type: types.KindString},
		},
	}
}

func readAuthAuditLogsTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "audit_logs",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "body", Type: types.KindString},
		},
	}
}

func readAuthProfilesTableDef() schema.TableDefinition {
	return schema.TableDefinition{
		Name: "profiles",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: types.KindUint64, PrimaryKey: true},
			{Name: "owner", Type: types.KindString},
			{Name: "label", Type: types.KindString},
		},
	}
}

func readAuthMessagesSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   readAuthMessagesTableID,
		Name: "messages",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
			{Index: 1, Name: "owner", Type: schema.KindString},
			{Index: 2, Name: "channel", Type: schema.KindString},
			{Index: 3, Name: "body", Type: schema.KindString},
		},
	}
}

func readAuthSecretsSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   readAuthSecretsTableID,
		Name: "secrets",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
			{Index: 1, Name: "owner", Type: schema.KindString},
			{Index: 2, Name: "body", Type: schema.KindString},
		},
	}
}

func readAuthAuditLogsSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   readAuthAuditLogsTableID,
		Name: "audit_logs",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
			{Index: 1, Name: "body", Type: schema.KindString},
		},
	}
}

func readAuthProfilesSchema() *schema.TableSchema {
	return &schema.TableSchema{
		ID:   readAuthProfilesTableID,
		Name: "profiles",
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: schema.KindUint64},
			{Index: 1, Name: "owner", Type: schema.KindString},
			{Index: 2, Name: "label", Type: schema.KindString},
		},
	}
}

func readAuthAggregateSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Name:    "messages",
		Columns: []schema.ColumnSchema{{Index: 0, Name: "n", Type: schema.KindUint64}},
	}
}

func readAuthInsertMessageReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	parts, err := readAuthParseArgs(args, 4)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse message id: %w", err)
	}
	_, err = ctx.DB.Insert(uint32(readAuthMessagesTableID), types.ProductValue{
		types.NewUint64(id),
		types.NewString(parts[1]),
		types.NewString(parts[2]),
		types.NewString(parts[3]),
	})
	return nil, err
}

func readAuthInsertSecretReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	parts, err := readAuthParseArgs(args, 3)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse secret id: %w", err)
	}
	_, err = ctx.DB.Insert(uint32(readAuthSecretsTableID), types.ProductValue{
		types.NewUint64(id),
		types.NewString(parts[1]),
		types.NewString(parts[2]),
	})
	return nil, err
}

func readAuthInsertAuditReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	parts, err := readAuthParseArgs(args, 2)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse audit id: %w", err)
	}
	_, err = ctx.DB.Insert(uint32(readAuthAuditLogsTableID), types.ProductValue{
		types.NewUint64(id),
		types.NewString(parts[1]),
	})
	return nil, err
}

func readAuthInsertProfileReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	parts, err := readAuthParseArgs(args, 3)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse profile id: %w", err)
	}
	_, err = ctx.DB.Insert(uint32(readAuthProfilesTableID), types.ProductValue{
		types.NewUint64(id),
		types.NewString(parts[1]),
		types.NewString(parts[2]),
	})
	return nil, err
}

func readAuthParseArgs(args []byte, want int) ([]string, error) {
	parts := strings.Split(string(args), "|")
	if len(parts) != want {
		return nil, fmt.Errorf("got %d args, want %d", len(parts), want)
	}
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("arg %d is empty", i)
		}
	}
	return parts, nil
}

func callReadAuthReducer(t *testing.T, rt *shunter.Runtime, reducer string, args ...string) {
	t.Helper()
	res, err := rt.CallReducer(context.Background(), reducer, []byte(strings.Join(args, "|")))
	if err != nil {
		t.Fatalf("%s reducer admission error: %v", reducer, err)
	}
	if res.Status != shunter.StatusCommitted {
		t.Fatalf("%s reducer status = %v err=%v, want committed", reducer, res.Status, res.Error)
	}
}

func queryReadAuthMessages(t *testing.T, client *websocket.Conn, sql string, messageID []byte, label string) []readAuthMessageRow {
	t.Helper()
	return readAuthMessageRowsFromValues(queryReadAuthRows(t, client, sql, messageID, readAuthMessagesSchema(), label))
}

func queryReadAuthSecrets(t *testing.T, client *websocket.Conn, sql string, messageID []byte, label string) []readAuthSecretRow {
	t.Helper()
	return readAuthSecretRowsFromValues(queryReadAuthRows(t, client, sql, messageID, readAuthSecretsSchema(), label))
}

func queryReadAuthAuditLogs(t *testing.T, client *websocket.Conn, sql string, messageID []byte, label string) []readAuthAuditRow {
	t.Helper()
	return readAuthAuditRowsFromValues(queryReadAuthRows(t, client, sql, messageID, readAuthAuditLogsSchema(), label))
}

func queryReadAuthProfiles(t *testing.T, client *websocket.Conn, sql string, messageID []byte, label string) []readAuthProfileRow {
	t.Helper()
	return readAuthProfileRowsFromValues(queryReadAuthRows(t, client, sql, messageID, readAuthProfilesSchema(), label))
}

func queryReadAuthRows(t *testing.T, client *websocket.Conn, sql string, messageID []byte, ts *schema.TableSchema, label string) []types.ProductValue {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   messageID,
		QueryString: sql,
	}, label)
	return readAuthOneOffRows(t, client, messageID, ts.Name, ts, label)
}

func readAuthOneOffRows(t *testing.T, client *websocket.Conn, messageID []byte, tableName string, ts *schema.TableSchema, label string) []types.ProductValue {
	t.Helper()
	resp := readGauntletOneOffQueryResponseWithLabel(t, client, messageID, label)
	if resp.Error != nil {
		t.Fatalf("%s error = %q, want success", label, *resp.Error)
	}
	if len(resp.Tables) != 1 {
		t.Fatalf("%s returned %d tables, want 1", label, len(resp.Tables))
	}
	if resp.Tables[0].TableName != tableName {
		t.Fatalf("%s table = %q, want %q", label, resp.Tables[0].TableName, tableName)
	}
	return decodeReadAuthRows(t, resp.Tables[0].Rows, ts, label)
}

func assertReadAuthOneOffErrorContains(t *testing.T, client *websocket.Conn, sql string, messageID []byte, want string, label string) {
	t.Helper()
	resp := queryGauntletProtocolExpectErrorWithLabel(t, client, sql, messageID, label)
	if resp.Error == nil || !strings.Contains(*resp.Error, want) {
		t.Fatalf("%s error = %v, want substring %q", label, resp.Error, want)
	}
}

func assertReadAuthDeclaredOneOffErrorContains(t *testing.T, client *websocket.Conn, messageID []byte, want string, label string) {
	t.Helper()
	resp := readGauntletOneOffQueryResponseWithLabel(t, client, messageID, label)
	if resp.Error == nil || !strings.Contains(*resp.Error, want) {
		t.Fatalf("%s error = %v, want substring %q", label, resp.Error, want)
	}
}

func subscribeReadAuthMessages(t *testing.T, client *websocket.Conn, sql string, requestID, queryID uint32, label string) []readAuthMessageRow {
	t.Helper()
	return readAuthMessageRowsFromValues(subscribeReadAuthRows(t, client, sql, requestID, queryID, "messages", readAuthMessagesSchema(), label))
}

func subscribeReadAuthRows(t *testing.T, client *websocket.Conn, sql string, requestID, queryID uint32, tableName string, ts *schema.TableSchema, label string) []types.ProductValue {
	t.Helper()
	writeGauntletProtocolMessage(t, client, protocol.SubscribeSingleMsg{
		RequestID:   requestID,
		QueryID:     queryID,
		QueryString: sql,
	}, label)
	return readAuthSubscribeRows(t, client, requestID, queryID, tableName, ts, label)
}

func readAuthSubscribeRows(t *testing.T, client *websocket.Conn, requestID, queryID uint32, tableName string, ts *schema.TableSchema, label string) []types.ProductValue {
	t.Helper()
	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag == protocol.TagSubscriptionError {
		subErr := msg.(protocol.SubscriptionError)
		t.Fatalf("%s error = %q, want success", label, subErr.Error)
	}
	applied, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok {
		t.Fatalf("%s response = %T, want SubscribeSingleApplied", label, msg)
	}
	if applied.RequestID != requestID || applied.QueryID != queryID {
		t.Fatalf("%s applied identity = request %d query %d, want request %d query %d", label, applied.RequestID, applied.QueryID, requestID, queryID)
	}
	if applied.TableName != tableName {
		t.Fatalf("%s table = %q, want %q", label, applied.TableName, tableName)
	}
	return decodeReadAuthRows(t, applied.Rows, ts, label)
}

func assertReadAuthSubscribeErrorContains(t *testing.T, client *websocket.Conn, sql string, requestID, queryID uint32, want string, label string) {
	t.Helper()
	subErr := subscribeGauntletProtocolExpectErrorWithLabel(t, client, sql, requestID, queryID, label)
	if !strings.Contains(subErr.Error, want) {
		t.Fatalf("%s error = %q, want substring %q", label, subErr.Error, want)
	}
	if !strings.Contains(subErr.Error, "executing: `"+sql+"`") {
		t.Fatalf("%s error = %q, want SQL wrapper for %q", label, subErr.Error, sql)
	}
}

func assertReadAuthSubscribeMultiErrorContains(t *testing.T, client *websocket.Conn, sql []string, requestID, queryID uint32, want string, label string) {
	t.Helper()
	subErr := subscribeMultiGauntletProtocolExpectErrorWithLabel(t, client, sql, requestID, queryID, label)
	if !strings.Contains(subErr.Error, want) {
		t.Fatalf("%s error = %q, want substring %q", label, subErr.Error, want)
	}
}

type readAuthDelta struct {
	inserts []types.ProductValue
	deletes []types.ProductValue
}

func readAuthSubscriptionDelta(t *testing.T, client *websocket.Conn, queryID uint32, tableName string, ts *schema.TableSchema, label string) readAuthDelta {
	t.Helper()
	tag, msg := readGauntletProtocolMessage(t, client, label)
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("%s tag = %d, want TransactionUpdateLight", label, tag)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("%s response = %T, want TransactionUpdateLight", label, msg)
	}
	var delta readAuthDelta
	found := false
	for i, entry := range update.Update {
		if entry.QueryID != queryID || entry.TableName != tableName {
			t.Fatalf("%s update %d identity = query %d table %q, want query %d table %q", label, i, entry.QueryID, entry.TableName, queryID, tableName)
		}
		delta.inserts = append(delta.inserts, decodeReadAuthRows(t, entry.Inserts, ts, fmt.Sprintf("%s inserts %d", label, i))...)
		delta.deletes = append(delta.deletes, decodeReadAuthRows(t, entry.Deletes, ts, fmt.Sprintf("%s deletes %d", label, i))...)
		found = true
	}
	if !found {
		t.Fatalf("%s has no subscription updates", label)
	}
	return delta
}

func decodeReadAuthRows(t *testing.T, encoded []byte, ts *schema.TableSchema, label string) []types.ProductValue {
	t.Helper()
	rawRows, err := protocol.DecodeRowList(encoded)
	if err != nil {
		t.Fatalf("%s DecodeRowList: %v", label, err)
	}
	rows := make([]types.ProductValue, 0, len(rawRows))
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, ts)
		if err != nil {
			t.Fatalf("%s decode row %d: %v", label, i, err)
		}
		rows = append(rows, row)
	}
	return rows
}

type readAuthMessageRow struct {
	ID      uint64
	Owner   string
	Channel string
	Body    string
}

type readAuthSecretRow struct {
	ID    uint64
	Owner string
	Body  string
}

type readAuthAuditRow struct {
	ID   uint64
	Body string
}

type readAuthProfileRow struct {
	ID    uint64
	Owner string
	Label string
}

func readAuthMessageRowsFromValues(rows []types.ProductValue) []readAuthMessageRow {
	out := make([]readAuthMessageRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, readAuthMessageRow{
			ID:      row[0].AsUint64(),
			Owner:   row[1].AsString(),
			Channel: row[2].AsString(),
			Body:    row[3].AsString(),
		})
	}
	return out
}

func readAuthSecretRowsFromValues(rows []types.ProductValue) []readAuthSecretRow {
	out := make([]readAuthSecretRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, readAuthSecretRow{
			ID:    row[0].AsUint64(),
			Owner: row[1].AsString(),
			Body:  row[2].AsString(),
		})
	}
	return out
}

func readAuthAuditRowsFromValues(rows []types.ProductValue) []readAuthAuditRow {
	out := make([]readAuthAuditRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, readAuthAuditRow{
			ID:   row[0].AsUint64(),
			Body: row[1].AsString(),
		})
	}
	return out
}

func readAuthProfileRowsFromValues(rows []types.ProductValue) []readAuthProfileRow {
	out := make([]readAuthProfileRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, readAuthProfileRow{
			ID:    row[0].AsUint64(),
			Owner: row[1].AsString(),
			Label: row[2].AsString(),
		})
	}
	return out
}

func assertReadAuthMessageRowsEqual(t *testing.T, got, want []readAuthMessageRow, label string) {
	t.Helper()
	got = sortedReadAuthMessageRows(got)
	want = sortedReadAuthMessageRows(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s rows = %#v, want %#v", label, got, want)
	}
}

func assertReadAuthSecretRowsEqual(t *testing.T, got, want []readAuthSecretRow, label string) {
	t.Helper()
	got = sortedReadAuthSecretRows(got)
	want = sortedReadAuthSecretRows(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s rows = %#v, want %#v", label, got, want)
	}
}

func assertReadAuthAuditRowsEqual(t *testing.T, got, want []readAuthAuditRow, label string) {
	t.Helper()
	got = sortedReadAuthAuditRows(got)
	want = sortedReadAuthAuditRows(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s rows = %#v, want %#v", label, got, want)
	}
}

func assertReadAuthProfileRowsEqual(t *testing.T, got, want []readAuthProfileRow, label string) {
	t.Helper()
	got = sortedReadAuthProfileRows(got)
	want = sortedReadAuthProfileRows(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s rows = %#v, want %#v", label, got, want)
	}
}

func sortedReadAuthMessageRows(in []readAuthMessageRow) []readAuthMessageRow {
	out := append([]readAuthMessageRow(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].Body < out[j].Body
	})
	return out
}

func sortedReadAuthSecretRows(in []readAuthSecretRow) []readAuthSecretRow {
	out := append([]readAuthSecretRow(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Body < out[j].Body
	})
	return out
}

func sortedReadAuthAuditRows(in []readAuthAuditRow) []readAuthAuditRow {
	out := append([]readAuthAuditRow(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Body < out[j].Body
	})
	return out
}

func sortedReadAuthProfileRows(in []readAuthProfileRow) []readAuthProfileRow {
	out := append([]readAuthProfileRow(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func assertReadAuthProductRowsEqual(t *testing.T, got, want []types.ProductValue, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s rows = %#v, want %#v", label, got, want)
	}
	for i := range got {
		if !got[i].Equal(want[i]) {
			t.Fatalf("%s row %d = %#v, want %#v", label, i, got[i], want[i])
		}
	}
}

func closeReadAuthClient(t *testing.T, client *websocket.Conn, label string) {
	t.Helper()
	if err := client.Close(websocket.StatusNormalClosure, label); err != nil {
		t.Fatalf("%s close client: %v", label, err)
	}
}
