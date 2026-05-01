package codegen

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
)

func TestGeneratorAcceptsCanonicalContractJSON(t *testing.T) {
	data, err := contractFixture().MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	out, err := GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("GenerateFromJSON returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export interface MessagesRow {`)
	assertContains(t, ts, `id: bigint;`)
	assertContains(t, ts, `body: string;`)
	assertContains(t, ts, `sentAt: bigint;`)
	assertContains(t, ts, `payload: Uint8Array;`)
	assertContains(t, ts, `tags: string[];`)
	assertContains(t, ts, `export const reducers = {`)
	assertContains(t, ts, `sendMessage: "send_message",`)
	assertContains(t, ts, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `export const lifecycleReducers = {`)
	assertContains(t, ts, `OnConnect: "OnConnect",`)
	assertContains(t, ts, `export type QueryRunner = (sql: string) => Promise<Uint8Array>;`)
	assertContains(t, ts, `export type ViewSubscriber = (sql: string) => Promise<() => void>;`)
	assertContains(t, ts, `export type DeclaredQueryRunner = (name: string) => Promise<Uint8Array>;`)
	assertContains(t, ts, `export type DeclaredViewSubscriber = (name: string) => Promise<() => void>;`)
	assertContains(t, ts, `export const tableReadPolicies = {`)
	assertContains(t, ts, `messages: { access: "private", permissions: [] },`)
	assertContains(t, ts, `export const queries = {`)
	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `export const querySQL = {`)
	assertContains(t, ts, `recentMessages: "SELECT * FROM messages",`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export const views = {`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `export const viewSQL = {`)
	assertContains(t, ts, `liveMessages: "SELECT * FROM messages",`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_messages");`)
	assertNotContains(t, ts, `return runQuery("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return subscribeView("SELECT * FROM messages");`)
	assertContains(t, ts, `export const permissions = {`)
	assertContains(t, ts, `reducers: {`)
	assertContains(t, ts, `sendMessage: { required: ["messages:send"] },`)
	assertContains(t, ts, `queries: {`)
	assertContains(t, ts, `recentMessages: { required: ["messages:read"] },`)
	assertContains(t, ts, `views: {`)
	assertContains(t, ts, `liveMessages: { required: ["messages:subscribe"] },`)
	assertContains(t, ts, `export const readModels = {`)
	assertContains(t, ts, `recentMessages: { tables: ["messages"], tags: ["history"] },`)
	assertContains(t, ts, `liveMessages: { tables: ["messages"], tags: ["realtime"] },`)
}

func TestGeneratorAcceptsModuleContractWithoutRuntime(t *testing.T) {
	out, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	assertContains(t, string(out), `export interface MessagesRow {`)
	assertContains(t, string(out), `export function subscribeMessages(subscribeTable: TableSubscriber<MessagesRow>): Promise<() => void> {`)
}

func TestGeneratorRejectsUnsupportedLanguage(t *testing.T) {
	_, err := Generate(contractFixture(), Options{Language: "go"})
	if err == nil {
		t.Fatal("Generate returned nil error, want unsupported language error")
	}
	if !strings.Contains(err.Error(), `unsupported language "go"`) {
		t.Fatalf("Generate error = %v, want unsupported language", err)
	}
}

func TestGeneratorRejectsUnsupportedLanguageBeforeDecodingContractJSON(t *testing.T) {
	_, err := GenerateFromJSON([]byte(`{`), Options{Language: "go"})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want unsupported language error")
	}
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrUnsupportedLanguage", err)
	}
	if errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON decoded contract before rejecting language: %v", err)
	}
}

func TestGeneratorRejectsUnusableContractJSON(t *testing.T) {
	_, err := GenerateFromJSON([]byte(`{"version":1,"tables":[],"reducers":[]}`), Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !strings.Contains(err.Error(), "invalid module contract") {
		t.Fatalf("GenerateFromJSON error = %v, want invalid module contract", err)
	}
}

func TestGeneratorRejectsCodegenMetadataMismatchWithContext(t *testing.T) {
	contract := contractFixture()
	contract.Codegen.ContractFormat = "unexpected.format"
	contract.Codegen.ContractVersion = shunter.ModuleContractVersion + 1
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("GenerateFromJSON error = %v, want ErrInvalidContract", err)
	}
	if !strings.Contains(err.Error(), `codegen.contract_format = "unexpected.format"`) {
		t.Fatalf("GenerateFromJSON error = %v, want codegen format context", err)
	}
	if !strings.Contains(err.Error(), "codegen.contract_version") {
		t.Fatalf("GenerateFromJSON error = %v, want codegen version context", err)
	}
}

func TestGeneratorRejectsContractWithInvalidDeclarationSQL(t *testing.T) {
	contract := contractFixture()
	contract.Queries[0].SQL = "SELECT * FROM missing"
	data, err := contract.MarshalCanonicalJSON()
	if err != nil {
		t.Fatalf("MarshalCanonicalJSON returned error: %v", err)
	}

	_, err = GenerateFromJSON(data, Options{Language: LanguageTypeScript})
	if err == nil {
		t.Fatal("GenerateFromJSON returned nil error, want invalid contract error")
	}
	if !strings.Contains(err.Error(), "queries.recent_messages.sql") {
		t.Fatalf("GenerateFromJSON error = %v, want declaration SQL context", err)
	}
}

func TestTypeScriptGeneratorIsDeterministic(t *testing.T) {
	first, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	second, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("Generate was not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestTypeScriptGeneratorConcurrentDeterminismShortSoak(t *testing.T) {
	const (
		seed       = uint64(0xc0de6e17)
		workers    = 6
		iterations = 64
	)
	fixtures := []struct {
		name     string
		contract shunter.ModuleContract
	}{
		{name: "canonical", contract: contractFixture()},
		{name: "collision", contract: codegenIdentifierCollisionFixture()},
	}
	for i := range fixtures {
		fixtures[i].contract.Module.Metadata["soak"] = fixtures[i].name
	}

	expected := make([][]byte, len(fixtures))
	canonicalJSON := make([][]byte, len(fixtures))
	for i, fixture := range fixtures {
		out, err := Generate(fixture.contract, Options{Language: LanguageTypeScript})
		if err != nil {
			t.Fatalf("seed=%#x fixture=%s operation=GenerateExpected observed_error=%v expected=nil", seed, fixture.name, err)
		}
		expected[i] = out
		data, err := fixture.contract.MarshalCanonicalJSON()
		if err != nil {
			t.Fatalf("seed=%#x fixture=%s operation=MarshalCanonicalJSON observed_error=%v expected=nil", seed, fixture.name, err)
		}
		canonicalJSON[i] = data
	}

	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	failures := make(chan string, workers*iterations)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			for op := range iterations {
				fixtureIndex := (int(seed) + worker*11 + op*7) % len(fixtures)
				fixture := fixtures[fixtureIndex]
				var (
					out []byte
					err error
				)
				if (int(seed)+worker+op)%2 == 0 {
					out, err = Generate(fixture.contract, Options{Language: LanguageTypeScript})
				} else {
					out, err = GenerateFromJSON(canonicalJSON[fixtureIndex], Options{Language: LanguageTypeScript})
				}
				if err != nil {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d fixture=%s operation=Generate observed_error=%v expected=nil",
						seed, worker, op, workers, iterations, fixture.name, err)
					continue
				}
				if !bytes.Equal(out, expected[fixtureIndex]) {
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=workers=%d/iterations=%d fixture=%s operation=GenerateDeterminism observed_len=%d expected_len=%d",
						seed, worker, op, workers, iterations, fixture.name, len(out), len(expected[fixtureIndex]))
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}

	waitForCodegenSoakWorkers(t, ready, workers, "seed=0xc0de6e17 codegen workers started")
	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestTypeScriptGeneratorAvoidsTableViewSubscribeHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables, schema.TableExport{
		Name: "live_messages",
		Columns: []schema.ColumnExport{
			{Name: "id", Type: "uint64"},
		},
		Indexes: []schema.IndexExport{},
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export function subscribeLiveMessages(subscribeTable: TableSubscriber<LiveMessagesRow>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribeLiveMessages2(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
}

func TestTypeScriptGeneratorDisambiguatesReducerHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers, schema.ReducerExport{Name: "send-message"})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `sendMessage: "send_message",`)
	assertContains(t, ts, `sendMessage2: "send-message",`)
	assertContains(t, ts, `export function callSendMessage(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `return callReducer("send_message", args);`)
	assertContains(t, ts, `export function callSendMessage2(callReducer: ReducerCaller, args: Uint8Array): Promise<Uint8Array> {`)
	assertContains(t, ts, `return callReducer("send-message", args);`)
}

func TestTypeScriptGeneratorDisambiguatesLifecycleReducerIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers,
		schema.ReducerExport{Name: "on-connect", Lifecycle: true},
		schema.ReducerExport{Name: "default", Lifecycle: true},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const lifecycleReducers = {`)
	assertContains(t, ts, `OnConnect: "OnConnect",`)
	assertContains(t, ts, `OnConnect2: "on-connect",`)
	assertContains(t, ts, `Default: "default",`)
}

func TestTypeScriptGeneratorDisambiguatesFallbackAndReservedTableIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables,
		schema.TableExport{
			Name:    "!!!",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
		schema.TableExport{
			Name:    "_",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
		schema.TableExport{
			Name:    "class",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
		schema.TableExport{
			Name:    "class!",
			Columns: []schema.ColumnExport{{Name: "id", Type: "uint64"}},
		},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export interface _Row {`)
	assertContains(t, ts, `export interface _2Row {`)
	assertContains(t, ts, `_: "!!!",`)
	assertContains(t, ts, `_2: "_",`)
	assertContains(t, ts, `class_: "class",`)
	assertContains(t, ts, `class_2: "class!",`)
	assertContains(t, ts, `export function subscribe_(subscribeTable: TableSubscriber<_Row>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribe_2(subscribeTable: TableSubscriber<_2Row>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribeClass(subscribeTable: TableSubscriber<ClassRow>): Promise<() => void> {`)
	assertContains(t, ts, `export function subscribeClass2(subscribeTable: TableSubscriber<Class2Row>): Promise<() => void> {`)
}

func TestTypeScriptGeneratorDisambiguatesRowFieldIdentifierCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].Columns = []schema.ColumnExport{
		{Name: "body_text", Type: "string"},
		{Name: "body-text", Type: "string"},
		{Name: "class", Type: "uint64"},
		{Name: "class!", Type: "uint64"},
	}
	contract.Schema.Tables[0].Indexes = nil

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export interface MessagesRow {`)
	assertContains(t, ts, `bodyText: string;`)
	assertContains(t, ts, `bodyText2: string;`)
	assertContains(t, ts, `class_: bigint;`)
	assertContains(t, ts, `class_2: bigint;`)
}

func TestTypeScriptGeneratorDeclaredHelpersUseNamedCallbacksNotSQL(t *testing.T) {
	out, err := Generate(contractFixture(), Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export type QueryRunner = (sql: string) => Promise<Uint8Array>;`)
	assertContains(t, ts, `export type ViewSubscriber = (sql: string) => Promise<() => void>;`)
	assertContains(t, ts, `export type DeclaredQueryRunner = (name: string) => Promise<Uint8Array>;`)
	assertContains(t, ts, `export type DeclaredViewSubscriber = (name: string) => Promise<() => void>;`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_messages");`)
	assertNotContains(t, ts, `return runDeclaredQuery("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return subscribeDeclaredView("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return runQuery("SELECT * FROM messages");`)
	assertNotContains(t, ts, `return subscribeView("SELECT * FROM messages");`)
}

func TestTypeScriptGeneratorDisambiguatesDeclaredReadHelperNameCollisions(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{Name: "recent-messages", SQL: "SELECT * FROM messages"})
	contract.Views = append(contract.Views, shunter.ViewDescription{Name: "live messages", SQL: "SELECT * FROM messages"})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `recentMessages2: "recent-messages",`)
	assertContains(t, ts, `export function queryRecentMessages(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent_messages");`)
	assertContains(t, ts, `export function queryRecentMessages2(runDeclaredQuery: DeclaredQueryRunner): Promise<Uint8Array> {`)
	assertContains(t, ts, `return runDeclaredQuery("recent-messages");`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `liveMessages2: "live messages",`)
	assertContains(t, ts, `export function subscribeLiveMessages(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live_messages");`)
	assertContains(t, ts, `export function subscribeLiveMessages2(subscribeDeclaredView: DeclaredViewSubscriber): Promise<() => void> {`)
	assertContains(t, ts, `return subscribeDeclaredView("live messages");`)
}

func TestTypeScriptGeneratorDoesNotEmitExecutableHelpersForMetadataOnlyDeclarations(t *testing.T) {
	contract := contractFixture()
	contract.Queries[0].SQL = ""
	contract.Views[0].SQL = ""

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const queries = {`)
	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `export const querySQL = {`)
	assertNotContains(t, ts, `export function queryRecentMessages(`)
	assertContains(t, ts, `export const views = {`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `export const viewSQL = {`)
	assertNotContains(t, ts, `export function subscribeLiveMessages(`)
}

func TestTypeScriptGeneratorEmitsTableReadPolicyMetadata(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables[0].ReadPolicy = schema.ReadPolicy{
		Access:      schema.TableAccessPermissioned,
		Permissions: []string{"messages:read"},
	}

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const tableReadPolicies = {`)
	assertContains(t, ts, `messages: { access: "permissioned", permissions: ["messages:read"] },`)
}

func TestTypeScriptGeneratorDisambiguatesTableReadPolicyMetadataIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Tables = append(contract.Schema.Tables,
		schema.TableExport{
			Name: "audit_log",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
			Indexes: []schema.IndexExport{{Name: "audit_log_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
			ReadPolicy: schema.ReadPolicy{
				Access:      schema.TableAccessPermissioned,
				Permissions: []string{`audit:read"quoted`},
			},
		},
		schema.TableExport{
			Name: "audit-log",
			Columns: []schema.ColumnExport{
				{Name: "id", Type: "uint64"},
			},
			Indexes: []schema.IndexExport{{Name: "audit_log_2_pk", Columns: []string{"id"}, Unique: true, Primary: true}},
			ReadPolicy: schema.ReadPolicy{
				Access:      schema.TableAccessPermissioned,
				Permissions: []string{`audit\archive`},
			},
		},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const tableReadPolicies = {`)
	assertContains(t, ts, `auditLog: { access: "permissioned", permissions: ["audit:read\"quoted"] },`)
	assertContains(t, ts, `auditLog2: { access: "permissioned", permissions: ["audit\\archive"] },`)
}

func TestTypeScriptGeneratorEmitsVisibilityFilterMetadata(t *testing.T) {
	contract := contractFixture()
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{{
		Name:               "own_messages",
		SQL:                "SELECT * FROM messages WHERE body = :sender",
		ReturnTable:        "messages",
		ReturnTableID:      0,
		UsesCallerIdentity: true,
	}}

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const visibilityFilters = {`)
	assertContains(t, ts, `ownMessages: { sql: "SELECT * FROM messages WHERE body = :sender", returnTable: "messages", returnTableId: 0, usesCallerIdentity: true },`)
}

func TestTypeScriptGeneratorDisambiguatesVisibilityFilterMetadataIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.VisibilityFilters = []shunter.VisibilityFilterDescription{
		{
			Name:          "own_messages",
			SQL:           `SELECT * FROM messages WHERE body = 'said "hi"'`,
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
		{
			Name:          "own-messages",
			SQL:           `SELECT * FROM messages WHERE body = 'archived'`,
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
		{
			Name:          "class",
			SQL:           `SELECT * FROM messages WHERE body = 'reserved'`,
			ReturnTable:   "messages",
			ReturnTableID: 0,
		},
	}

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `export const visibilityFilters = {`)
	assertContains(t, ts, `ownMessages: { sql: "SELECT * FROM messages WHERE body = 'said \"hi\"'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`)
	assertContains(t, ts, `ownMessages2: { sql: "SELECT * FROM messages WHERE body = 'archived'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`)
	assertContains(t, ts, `class_: { sql: "SELECT * FROM messages WHERE body = 'reserved'", returnTable: "messages", returnTableId: 0, usesCallerIdentity: false },`)
}

func TestTypeScriptGeneratorDisambiguatesMetadataMapIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Queries = append(contract.Queries, shunter.QueryDescription{Name: "recent-messages", SQL: "SELECT * FROM messages"})
	contract.Views = append(contract.Views, shunter.ViewDescription{Name: "live messages", SQL: "SELECT * FROM messages"})
	contract.Permissions.Queries = append(contract.Permissions.Queries, shunter.PermissionContractDeclaration{
		Name:     "recent-messages",
		Required: []string{`messages:read"quoted`},
	})
	contract.Permissions.Views = append(contract.Permissions.Views, shunter.PermissionContractDeclaration{
		Name:     "live messages",
		Required: []string{`messages:subscribe\slash`},
	})
	contract.ReadModel.Declarations = append(contract.ReadModel.Declarations,
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceQuery,
			Name:    "recent-messages",
			Tables:  []string{"messages"},
			Tags:    []string{`audit"trail`},
		},
		shunter.ReadModelContractDeclaration{
			Surface: shunter.ReadModelSurfaceView,
			Name:    "live messages",
			Tables:  []string{"messages"},
			Tags:    []string{`live\feed`},
		},
	)

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `recentMessages: "recent_messages",`)
	assertContains(t, ts, `recentMessages2: "recent-messages",`)
	assertContains(t, ts, `liveMessages: "live_messages",`)
	assertContains(t, ts, `liveMessages2: "live messages",`)
	assertContains(t, ts, `recentMessages2: { required: ["messages:read\"quoted"] },`)
	assertContains(t, ts, `liveMessages2: { required: ["messages:subscribe\\slash"] },`)
	assertContains(t, ts, `recentMessages2: { tables: ["messages"], tags: ["audit\"trail"] },`)
	assertContains(t, ts, `liveMessages2: { tables: ["messages"], tags: ["live\\feed"] },`)
}

func TestTypeScriptGeneratorDisambiguatesReducerPermissionMetadataIdentifiers(t *testing.T) {
	contract := contractFixture()
	contract.Schema.Reducers = append(contract.Schema.Reducers, schema.ReducerExport{Name: "send-message"})
	contract.Permissions.Reducers = append(contract.Permissions.Reducers, shunter.PermissionContractDeclaration{
		Name:     "send-message",
		Required: []string{`messages:send-alt`},
	})

	out, err := Generate(contract, Options{Language: LanguageTypeScript})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	ts := string(out)

	assertContains(t, ts, `reducers: {`)
	assertContains(t, ts, `sendMessage: { required: ["messages:send"] },`)
	assertContains(t, ts, `sendMessage2: { required: ["messages:send-alt"] },`)
}

func contractFixture() shunter.ModuleContract {
	return shunter.ModuleContract{
		ContractVersion: shunter.ModuleContractVersion,
		Module: shunter.ModuleContractIdentity{
			Name:     "chat",
			Version:  "v1.2.3",
			Metadata: map[string]string{"team": "runtime"},
		},
		Schema: schema.SchemaExport{
			Version: 1,
			Tables: []schema.TableExport{
				{
					Name: "messages",
					Columns: []schema.ColumnExport{
						{Name: "id", Type: "uint64"},
						{Name: "body", Type: "string"},
						{Name: "sent_at", Type: "timestamp"},
						{Name: "payload", Type: "bytes"},
						{Name: "tags", Type: "arrayString"},
					},
					Indexes: []schema.IndexExport{
						{Name: "messages_pk", Columns: []string{"id"}, Unique: true, Primary: true},
					},
				},
			},
			Reducers: []schema.ReducerExport{
				{Name: "send_message", Lifecycle: false},
				{Name: "OnConnect", Lifecycle: true},
			},
		},
		Queries: []shunter.QueryDescription{
			{Name: "recent_messages", SQL: "SELECT * FROM messages"},
		},
		Views: []shunter.ViewDescription{
			{Name: "live_messages", SQL: "SELECT * FROM messages"},
		},
		Permissions: shunter.PermissionContract{
			Reducers: []shunter.PermissionContractDeclaration{
				{Name: "send_message", Required: []string{"messages:send"}},
			},
			Queries: []shunter.PermissionContractDeclaration{
				{Name: "recent_messages", Required: []string{"messages:read"}},
			},
			Views: []shunter.PermissionContractDeclaration{
				{Name: "live_messages", Required: []string{"messages:subscribe"}},
			},
		},
		ReadModel: shunter.ReadModelContract{
			Declarations: []shunter.ReadModelContractDeclaration{
				{
					Surface: shunter.ReadModelSurfaceQuery,
					Name:    "recent_messages",
					Tables:  []string{"messages"},
					Tags:    []string{"history"},
				},
				{
					Surface: shunter.ReadModelSurfaceView,
					Name:    "live_messages",
					Tables:  []string{"messages"},
					Tags:    []string{"realtime"},
				},
			},
		},
		Migrations: shunter.MigrationContract{
			Declarations: []shunter.MigrationContractDeclaration{},
		},
		Codegen: shunter.CodegenContractMetadata{
			ContractFormat:          shunter.ModuleContractFormat,
			ContractVersion:         shunter.ModuleContractVersion,
			DefaultSnapshotFilename: shunter.DefaultContractSnapshotFilename,
		},
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("generated TypeScript missing %q:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("generated TypeScript unexpectedly contains %q:\n%s", needle, haystack)
	}
}

func waitForCodegenSoakWorkers(t *testing.T, ch <-chan struct{}, want int, label string) {
	t.Helper()
	for i := 0; i < want; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: observed %d/%d workers", label, i, want)
		}
	}
}
