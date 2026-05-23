package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/websocket"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestCallCommandInvokesRunningAppReducerJSON(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	received := make(chan protocol.CallReducerMsg, 1)
	srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertRunningAppAuth(t, r, "operator-token")
		ws := acceptRunningAppProtocolConn(t, w, r)
		defer ws.CloseNow()
		writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read reducer call: %v", err)
			return
		}
		_, msg, err := protocol.DecodeClientMessage(frame)
		if err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		call, ok := msg.(protocol.CallReducerMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.CallReducerMsg", msg)
			return
		}
		received <- call
		writeRunningAppServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})
		readRunningAppClientClose(t, r, ws)
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"call",
		"--url", srv.httpURL(),
		"--contract", contractPath,
		"--token", "operator-token",
		"--format", "json",
		"send_message",
		`{"author":"Ada","body":"hello"}`,
	})
	if code != 0 {
		t.Fatalf("call exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("call stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status    string `json:"status"`
		Command   string `json:"command"`
		TargetURL string `json:"target_url"`
		Surface   string `json:"surface"`
		TxStatus  string `json:"tx_status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode call JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "ok" || report.Command != "call" || report.Surface != "send_message" || report.TxStatus != "committed" {
		t.Fatalf("call report = %+v", report)
	}
	if !strings.HasPrefix(report.TargetURL, "ws://") || !strings.HasSuffix(report.TargetURL, "/subscribe") {
		t.Fatalf("target URL = %q, want normalized ws /subscribe URL", report.TargetURL)
	}

	select {
	case call := <-received:
		if call.ReducerName != "send_message" || call.RequestID != 1 || call.Flags != protocol.CallReducerFlagsFullUpdate {
			t.Fatalf("call = %+v", call)
		}
		if len(call.Args) == 0 {
			t.Fatal("call args are empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive reducer call")
	}
}

func TestQueryCommandInvokesRunningAppDeclaredQueryAndDecodesRows(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	received := make(chan protocol.DeclaredQueryMsg, 1)
	rows := encodeRunningAppMessageRows(t, types.ProductValue{
		types.NewUint64(1),
		types.NewString("Ada"),
		types.NewString("hello"),
	})
	srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertRunningAppAuth(t, r, "env-token")
		ws := acceptRunningAppProtocolConn(t, w, r)
		defer ws.CloseNow()
		writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{3}, ConnectionID: [16]byte{4}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read query: %v", err)
			return
		}
		_, msg, err := protocol.DecodeClientMessage(frame)
		if err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		query, ok := msg.(protocol.DeclaredQueryMsg)
		if !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryMsg", msg)
			return
		}
		received <- query
		writeRunningAppServerMessage(t, ws, protocol.OneOffQueryResponse{
			MessageID: query.MessageID,
			Tables:    []protocol.OneOffTable{{TableName: "messages", Rows: rows}},
		})
		readRunningAppClientClose(t, r, ws)
	})
	t.Setenv("SHUNTER_TOKEN", "env-token")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"query",
		"--url", srv.wsURL(),
		"--contract", contractPath,
		"--format", "json",
		"recent_messages",
	})
	if code != 0 {
		t.Fatalf("query exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("query stderr = %s, want empty", stderr.String())
	}
	var report struct {
		Status  string `json:"status"`
		Command string `json:"command"`
		Surface string `json:"surface"`
		Result  struct {
			Name      string `json:"name"`
			TableName string `json:"table_name"`
			Rows      []struct {
				ID     string `json:"id"`
				Author string `json:"author"`
				Body   string `json:"body"`
			} `json:"rows"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode query JSON: %v\n%s", err, stdout.String())
	}
	if report.Status != "ok" || report.Command != "query" || report.Surface != "recent_messages" {
		t.Fatalf("query report = %+v", report)
	}
	if report.Result.Name != "recent_messages" || report.Result.TableName != "messages" || len(report.Result.Rows) != 1 {
		t.Fatalf("query result = %+v", report.Result)
	}
	if report.Result.Rows[0].ID != "1" || report.Result.Rows[0].Author != "Ada" || report.Result.Rows[0].Body != "hello" {
		t.Fatalf("query row = %+v", report.Result.Rows[0])
	}

	select {
	case query := <-received:
		if query.Name != "recent_messages" || !bytes.Equal(query.MessageID, []byte{1, 0, 0, 0}) {
			t.Fatalf("query = %+v", query)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive declared query")
	}
}

func TestRunningAppCommandsRejectLocalMisuseBeforeNetwork(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	called := make(chan struct{}, 1)
	srv := runningAppProtocolTestServer(t, func(http.ResponseWriter, *http.Request) {
		called <- struct{}{}
	})

	for _, tc := range []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "call-missing-token",
			args:       []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "send_message", `{"author":"Ada","body":"hello"}`},
			wantCode:   2,
			wantStderr: "token is required",
		},
		{
			name:       "call-unknown-reducer",
			args:       []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "missing", `{}`},
			wantCode:   2,
			wantStderr: "contract surface not found",
		},
		{
			name:       "call-malformed-json",
			args:       []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "send_message", `{`},
			wantCode:   2,
			wantStderr: "invalid argument JSON",
		},
		{
			name:       "query-unexpected-args",
			args:       []string{"query", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "recent_messages", `{"unexpected":true}`},
			wantCode:   2,
			wantStderr: "does not accept arguments",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != tc.wantCode {
				t.Fatalf("%s exit code = %d, stderr = %s", tc.name, code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("%s stdout = %s, want empty", tc.name, stdout.String())
			}
			assertContains(t, stderr.String(), tc.wantStderr)
			select {
			case <-called:
				t.Fatalf("%s called server before rejecting local misuse", tc.name)
			default:
			}
		})
	}
}

func TestRunningAppCommandJSONErrorOutput(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"call",
		"--url", "http://127.0.0.1:1",
		"--contract", contractPath,
		"--format", "json",
		"send_message",
		`{"author":"Ada","body":"hello"}`,
	})
	if code != 2 {
		t.Fatalf("call missing token exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %s, want empty", stdout.String())
	}
	var report runningAppError
	if err := json.Unmarshal(stderr.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON error: %v\n%s", err, stderr.String())
	}
	if report.Status != "error" || report.Scope != "running_app" || report.Command != "call" || report.Code != "missing_token" || report.Surface != "send_message" {
		t.Fatalf("error report = %+v", report)
	}
}

func TestRunningAppCommandTokenFileAndArgsFile(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	tokenPath := filepath.Join(dir, "token.txt")
	argsPath := filepath.Join(dir, "args.json")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o666); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := os.WriteFile(argsPath, []byte(`{"author":"Ada","body":"from file"}`), 0o666); err != nil {
		t.Fatalf("write args: %v", err)
	}
	srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertRunningAppAuth(t, r, "file-token")
		ws := acceptRunningAppProtocolConn(t, w, r)
		defer ws.CloseNow()
		writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read reducer call: %v", err)
			return
		}
		_, msg, err := protocol.DecodeClientMessage(frame)
		if err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		call := msg.(protocol.CallReducerMsg)
		writeRunningAppServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})
		readRunningAppClientClose(t, r, ws)
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"call",
		"--url", srv.httpURL(),
		"--contract", contractPath,
		"--token-file", tokenPath,
		"--args-file", argsPath,
		"send_message",
	})
	if code != 0 {
		t.Fatalf("call with files exit code = %d, stderr = %s", code, stderr.String())
	}
	assertContains(t, stdout.String(), "Status: ok")
}

func TestRunningAppCommandTokenPrecedenceAndDevAnonymous(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	tokenPath := filepath.Join(dir, "token.txt")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o666); err != nil {
		t.Fatalf("write token: %v", err)
	}

	for _, tc := range []struct {
		name      string
		envToken  string
		args      []string
		wantToken string
	}{
		{
			name:     "token-flag-beats-file-and-env",
			envToken: "env-token",
			args: []string{
				"--token", "flag-token",
				"--token-file", tokenPath,
			},
			wantToken: "flag-token",
		},
		{
			name:     "token-file-beats-env",
			envToken: "env-token",
			args: []string{
				"--token-file", tokenPath,
			},
			wantToken: "file-token",
		},
		{
			name:      "env-beats-dev-anonymous",
			envToken:  "env-token",
			args:      []string{"--allow-dev-anonymous"},
			wantToken: "env-token",
		},
		{
			name: "token-flag-beats-dev-anonymous",
			args: []string{
				"--token", "flag-token",
				"--allow-dev-anonymous",
			},
			wantToken: "flag-token",
		},
		{
			name:      "dev-anonymous-without-token",
			args:      []string{"--allow-dev-anonymous"},
			wantToken: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHUNTER_TOKEN", tc.envToken)
			srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.wantToken == "" {
					if got := r.Header.Get("Authorization"); got != "" {
						t.Fatalf("Authorization = %q, want empty", got)
					}
				} else {
					assertRunningAppAuth(t, r, tc.wantToken)
				}
				ws := acceptRunningAppProtocolConn(t, w, r)
				defer ws.CloseNow()
				writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, Token: "minted-token", ConnectionID: [16]byte{2}})
				_, frame, err := ws.Read(r.Context())
				if err != nil {
					t.Errorf("server read query: %v", err)
					return
				}
				_, msg, err := protocol.DecodeClientMessage(frame)
				if err != nil {
					t.Errorf("DecodeClientMessage: %v", err)
					return
				}
				query := msg.(protocol.DeclaredQueryMsg)
				writeRunningAppServerMessage(t, ws, protocol.OneOffQueryResponse{
					MessageID: query.MessageID,
					Tables:    []protocol.OneOffTable{{TableName: "messages", Rows: encodeRunningAppMessageRows(t)}},
				})
				readRunningAppClientClose(t, r, ws)
			})

			args := []string{
				"query",
				"--url", srv.httpURL(),
				"--contract", contractPath,
			}
			args = append(args, tc.args...)
			args = append(args, "recent_messages")
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, args)
			if code != 0 {
				t.Fatalf("query exit code = %d, stderr = %s", code, stderr.String())
			}
			assertContains(t, stdout.String(), "Status: ok")
		})
	}
}

func TestRunningAppCommandArgsSourcesAreExclusiveBeforeNetwork(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	argsPath := filepath.Join(dir, "args.json")
	if err := os.WriteFile(argsPath, []byte(`{"author":"Ada","body":"from file"}`), 0o666); err != nil {
		t.Fatalf("write args: %v", err)
	}
	called := make(chan struct{}, 1)
	srv := runningAppProtocolTestServer(t, func(http.ResponseWriter, *http.Request) {
		called <- struct{}{}
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "args-and-positional",
			args: []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "--args", `{"author":"Ada","body":"flag"}`, "send_message", `{"author":"Ada","body":"positional"}`},
		},
		{
			name: "args-file-and-positional",
			args: []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "--args-file", argsPath, "send_message", `{"author":"Ada","body":"positional"}`},
		},
		{
			name: "args-hex-and-positional",
			args: []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "--args-hex", "0102", "send_message", `{"author":"Ada","body":"positional"}`},
		},
		{
			name: "args-and-args-file",
			args: []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "--args", `{"author":"Ada","body":"flag"}`, "--args-file", argsPath, "send_message"},
		},
		{
			name: "malformed-args-hex",
			args: []string{"call", "--url", srv.httpURL(), "--contract", contractPath, "--token", "operator-token", "--args-hex", "not-hex", "send_message"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(&stdout, &stderr, tc.args)
			if code != 2 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
			}
			assertContains(t, stderr.String(), "invalid")
			select {
			case <-called:
				t.Fatal("server was called before rejecting invalid argument sources")
			default:
			}
		})
	}
}

func TestRunningAppCommandArgsHexSendsRawBytes(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	received := make(chan []byte, 1)
	srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertRunningAppAuth(t, r, "operator-token")
		ws := acceptRunningAppProtocolConn(t, w, r)
		defer ws.CloseNow()
		writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read reducer call: %v", err)
			return
		}
		_, msg, err := protocol.DecodeClientMessage(frame)
		if err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		call := msg.(protocol.CallReducerMsg)
		received <- append([]byte(nil), call.Args...)
		writeRunningAppServerMessage(t, ws, protocol.TransactionUpdate{
			Status:      protocol.StatusCommitted{},
			ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
		})
		readRunningAppClientClose(t, r, ws)
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"call",
		"--url", srv.httpURL(),
		"--contract", contractPath,
		"--token", "operator-token",
		"--args-hex", "000102ff",
		"send_message",
	})
	if code != 0 {
		t.Fatalf("call with args-hex exit code = %d, stderr = %s", code, stderr.String())
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, []byte{0, 1, 2, 255}) {
			t.Fatalf("raw args = %x", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive reducer call")
	}
}

func TestRunningAppCommandRuntimeFailuresExitOne(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())

	t.Run("reducer-failure", func(t *testing.T) {
		srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assertRunningAppAuth(t, r, "operator-token")
			ws := acceptRunningAppProtocolConn(t, w, r)
			defer ws.CloseNow()
			writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
			_, frame, err := ws.Read(r.Context())
			if err != nil {
				t.Errorf("server read reducer call: %v", err)
				return
			}
			_, msg, err := protocol.DecodeClientMessage(frame)
			if err != nil {
				t.Errorf("DecodeClientMessage: %v", err)
				return
			}
			call := msg.(protocol.CallReducerMsg)
			writeRunningAppServerMessage(t, ws, protocol.TransactionUpdate{
				Status:      protocol.StatusFailed{Error: "boom"},
				ReducerCall: protocol.ReducerCallInfo{ReducerName: call.ReducerName, RequestID: call.RequestID},
			})
			readRunningAppClientClose(t, r, ws)
		})

		var stdout, stderr bytes.Buffer
		code := run(&stdout, &stderr, []string{
			"call",
			"--url", srv.httpURL(),
			"--contract", contractPath,
			"--token", "operator-token",
			"--format", "json",
			"send_message",
			`{"author":"Ada","body":"hello"}`,
		})
		if code != 1 {
			t.Fatalf("call exit code = %d, stderr = %s", code, stderr.String())
		}
		assertRunningAppJSONErrorCode(t, stderr.Bytes(), "reducer_error")
	})

	t.Run("query-failure", func(t *testing.T) {
		srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assertRunningAppAuth(t, r, "operator-token")
			ws := acceptRunningAppProtocolConn(t, w, r)
			defer ws.CloseNow()
			writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
			_, frame, err := ws.Read(r.Context())
			if err != nil {
				t.Errorf("server read query: %v", err)
				return
			}
			_, msg, err := protocol.DecodeClientMessage(frame)
			if err != nil {
				t.Errorf("DecodeClientMessage: %v", err)
				return
			}
			query := msg.(protocol.DeclaredQueryMsg)
			queryErr := "bad query"
			writeRunningAppServerMessage(t, ws, protocol.OneOffQueryResponse{
				MessageID: query.MessageID,
				Error:     &queryErr,
			})
			readRunningAppClientClose(t, r, ws)
		})

		var stdout, stderr bytes.Buffer
		code := run(&stdout, &stderr, []string{
			"query",
			"--url", srv.httpURL(),
			"--contract", contractPath,
			"--token", "operator-token",
			"--format", "json",
			"recent_messages",
		})
		if code != 1 {
			t.Fatalf("query exit code = %d, stderr = %s", code, stderr.String())
		}
		assertRunningAppJSONErrorCode(t, stderr.Bytes(), "query_error")
	})
}

func TestRunningAppCommandMalformedServerResponseIsProtocolError(t *testing.T) {
	dir := t.TempDir()
	contractPath := writeCLIContract(t, dir, "contract.json", runningAppContractFixture())
	srv := runningAppProtocolTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertRunningAppAuth(t, r, "operator-token")
		ws := acceptRunningAppProtocolConn(t, w, r)
		defer ws.CloseNow()
		writeRunningAppServerMessage(t, ws, protocol.IdentityToken{Identity: [32]byte{1}, ConnectionID: [16]byte{2}})
		_, frame, err := ws.Read(r.Context())
		if err != nil {
			t.Errorf("server read query: %v", err)
			return
		}
		_, msg, err := protocol.DecodeClientMessage(frame)
		if err != nil {
			t.Errorf("DecodeClientMessage: %v", err)
			return
		}
		if _, ok := msg.(protocol.DeclaredQueryMsg); !ok {
			t.Errorf("client message = %T, want protocol.DeclaredQueryMsg", msg)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := ws.Write(ctx, websocket.MessageBinary, []byte{protocol.TagOneOffQueryResponse}); err != nil {
			t.Fatalf("server write malformed response: %v", err)
		}
		readRunningAppClientClose(t, r, ws)
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"query",
		"--url", srv.httpURL(),
		"--contract", contractPath,
		"--token", "operator-token",
		"--format", "json",
		"recent_messages",
	})
	if code != 1 {
		t.Fatalf("query exit code = %d, stderr = %s", code, stderr.String())
	}
	assertRunningAppJSONErrorCode(t, stderr.Bytes(), "protocol_error")
}

func runningAppContractFixture() shunter.ModuleContract {
	contract := cliContractFixture()
	contract.Schema.Tables[0].Columns = []schema.ColumnExport{
		{Name: "id", Type: "uint64"},
		{Name: "author", Type: "string"},
		{Name: "body", Type: "string"},
	}
	contract.Schema.Reducers[0].Args = &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
		{Name: "author", Type: "string"},
		{Name: "body", Type: "string"},
	}}
	contract.Queries = []shunter.QueryDescription{{
		Name: "recent_messages",
		SQL:  "SELECT * FROM messages ORDER BY id DESC LIMIT 20",
		ResultShape: &shunter.ReadResultShape{
			Kind:  shunter.ReadResultShapeTable,
			Table: "messages",
		},
		RowSchema: &schema.ProductSchemaExport{Columns: []schema.ProductColumnExport{
			{Name: "id", Type: "uint64"},
			{Name: "author", Type: "string"},
			{Name: "body", Type: "string"},
		}},
	}}
	return contract
}

func encodeRunningAppMessageRows(t *testing.T, rows ...types.ProductValue) []byte {
	t.Helper()
	columns := []schema.ColumnSchema{
		{Index: 0, Name: "id", Type: types.KindUint64},
		{Index: 1, Name: "author", Type: types.KindString},
		{Index: 2, Name: "body", Type: types.KindString},
	}
	out, err := protocol.EncodeProductRowsForColumns(rows, columns)
	if err != nil {
		t.Fatalf("EncodeProductRowsForColumns: %v", err)
	}
	return out
}

type runningAppProtocolHTTPServer struct {
	*httptest.Server
}

func runningAppProtocolTestServer(t *testing.T, handler http.HandlerFunc) *runningAppProtocolHTTPServer {
	t.Helper()
	srv := &runningAppProtocolHTTPServer{Server: httptest.NewServer(handler)}
	t.Cleanup(srv.Close)
	return srv
}

func (s *runningAppProtocolHTTPServer) httpURL() string {
	return s.URL
}

func (s *runningAppProtocolHTTPServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.URL, "http") + "/subscribe"
}

func acceptRunningAppProtocolConn(t *testing.T, w http.ResponseWriter, r *http.Request) *websocket.Conn {
	t.Helper()
	if r.URL.Path != "/subscribe" {
		t.Fatalf("request path = %q, want /subscribe", r.URL.Path)
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: protocol.SupportedSubprotocols(),
	})
	if err != nil {
		t.Fatalf("websocket.Accept: %v", err)
	}
	return ws
}

func writeRunningAppServerMessage(t *testing.T, ws *websocket.Conn, msg any) {
	t.Helper()
	frame, err := protocol.EncodeServerMessage(msg)
	if err != nil {
		t.Fatalf("EncodeServerMessage(%T): %v", msg, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("server write %T: %v", msg, err)
	}
}

func readRunningAppClientClose(t *testing.T, r *http.Request, ws *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, _, _ = ws.Read(ctx)
}

func assertRunningAppAuth(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
}

func assertRunningAppJSONErrorCode(t *testing.T, data []byte, want string) {
	t.Helper()
	var report runningAppError
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode JSON error: %v\n%s", err, string(data))
	}
	if report.Status != "error" || report.Scope != "running_app" || report.Code != want {
		t.Fatalf("error report = %+v, want code %q", report, want)
	}
}
