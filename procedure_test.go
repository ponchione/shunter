package shunter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestCallProcedureRequiresReadyRuntime(t *testing.T) {
	rt := buildValidTestRuntime(t)

	_, err := rt.CallProcedure(context.Background(), "notify", nil)
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("CallProcedure before Start error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestCallProcedureWithCanceledContextReturnsContextError(t *testing.T) {
	rt := buildStartedRuntimeWithProcedure(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		t.Fatal("procedure should not run with canceled context")
		return nil, nil
	})
	defer rt.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.CallProcedure(ctx, "notify", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CallProcedure canceled context error = %v, want context.Canceled", err)
	}
}

func TestCallProcedureCopiesArgumentsAndReturn(t *testing.T) {
	var returned []byte
	rt := buildStartedRuntimeWithProcedure(t, "echo", func(_ *ProcedureContext, args []byte) ([]byte, error) {
		args[0] = 0xff
		returned = []byte{0x03, 0x04}
		return returned, nil
	})
	defer rt.Close()

	args := []byte{0x01, 0x02}
	got, err := rt.CallProcedure(context.Background(), "echo", args)
	if err != nil {
		t.Fatalf("CallProcedure returned error: %v", err)
	}
	if string(args) != string([]byte{0x01, 0x02}) {
		t.Fatalf("caller args = %x, want original 0102", args)
	}
	returned[0] = 0xee
	if string(got) != string([]byte{0x03, 0x04}) {
		t.Fatalf("procedure return = %x, want detached 0304", got)
	}
}

func TestCallProcedureWithAuthPrincipalCopiesClaimsThroughReducer(t *testing.T) {
	want := AuthPrincipal{
		Issuer:  "issuer",
		Subject: "alice",
		Claims: AuthClaims{Values: map[string]json.RawMessage{
			"email": []byte(`"alice@example.com"`),
		}},
	}
	var gotProcedure types.AuthPrincipal
	var gotProcedureAfterReducer types.AuthPrincipal
	var gotReducer types.AuthPrincipal
	rt, err := Build(validChatModule().
		Reducer("inspect_from_procedure", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
			gotReducer = ctx.Caller.Principal.Copy()
			ctx.Caller.Principal.Claims.Values["email"][1] = 'R'
			return nil, nil
		}).
		Procedure("inspect", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
			gotProcedure = ctx.Caller.Principal.Copy()
			res, err := ctx.CallReducer("inspect_from_procedure", nil)
			if err != nil {
				return nil, err
			}
			if res.Error != nil {
				return nil, res.Error
			}
			gotProcedureAfterReducer = ctx.Caller.Principal.Copy()
			return nil, nil
		}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	if _, err := rt.CallProcedure(context.Background(), "inspect", nil, WithProcedureAuthPrincipal(want)); err != nil {
		t.Fatalf("CallProcedure returned error: %v", err)
	}
	if claim, ok := gotProcedure.Claims.Get("email"); !ok || string(claim) != `"alice@example.com"` {
		t.Fatalf("procedure principal email claim = %s, %v; want copied email", claim, ok)
	}
	if claim, ok := gotReducer.Claims.Get("email"); !ok || string(claim) != `"alice@example.com"` {
		t.Fatalf("reducer principal email claim = %s, %v; want propagated email", claim, ok)
	}
	if string(want.Claims.Values["email"]) != `"alice@example.com"` {
		t.Fatalf("procedure/reducer claim mutation changed caller principal: %+v", want)
	}
	if claim, _ := gotProcedureAfterReducer.Claims.Get("email"); string(claim) != `"alice@example.com"` {
		t.Fatalf("reducer claim mutation changed procedure principal: %s", claim)
	}
}

func TestCallProcedurePanicsBecomeProcedureError(t *testing.T) {
	rt := buildStartedRuntimeWithProcedure(t, "panic", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		panic("boom")
	})
	defer rt.Close()

	_, err := rt.CallProcedure(context.Background(), "panic", nil)
	if !errors.Is(err, ErrProcedurePanicked) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("CallProcedure panic error = %v, want ErrProcedurePanicked with panic text", err)
	}
}

func TestCallProcedurePermissionDeniedBeforeHandlerExecution(t *testing.T) {
	called := false
	rt, err := Build(validChatModule().Procedure("notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		called = true
		return nil, nil
	}, WithProcedurePermissions(PermissionMetadata{Required: []string{"notify:send"}})), Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte("procedure-strict-secret-0123456789"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	_, err = rt.CallProcedure(context.Background(), "notify", nil)
	if !errors.Is(err, ErrProcedurePermissionDenied) {
		t.Fatalf("CallProcedure missing permission error = %v, want ErrProcedurePermissionDenied", err)
	}
	if called {
		t.Fatal("procedure handler ran despite missing permission")
	}

	_, err = rt.CallProcedure(context.Background(), "notify", nil, WithProcedureCallerPermissions("notify:send"))
	if err != nil {
		t.Fatalf("CallProcedure with permission returned error: %v", err)
	}
}

func TestCallProcedureDevLocalDefaultSatisfiesProcedurePermissions(t *testing.T) {
	rt, err := Build(validChatModule().Procedure("notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	}, WithProcedurePermissions(PermissionMetadata{Required: []string{"notify:send"}})), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	got, err := rt.CallProcedure(context.Background(), "notify", nil)
	if err != nil {
		t.Fatalf("CallProcedure dev default error = %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("CallProcedure return = %q, want ok", got)
	}
}

func TestProcedureCallReducerUsesSameCallerPermissions(t *testing.T) {
	const messagesTableID schema.TableID = 0
	rt, err := Build(validChatModule().
		Reducer("insert_message", func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
			_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{types.NewUint64(1), types.NewString("from procedure")})
			return nil, err
		}, WithReducerPermissions(PermissionMetadata{Required: []string{"messages:send"}})).
		Procedure("notify", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
			res, err := ctx.CallReducer("insert_message", nil)
			if err != nil {
				return nil, err
			}
			if res.Error != nil {
				return nil, res.Error
			}
			return []byte("ok"), nil
		}, WithProcedurePermissions(PermissionMetadata{Required: []string{"notify:send"}})), Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: []byte("procedure-strict-secret-0123456789"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	_, err = rt.CallProcedure(context.Background(), "notify", nil, WithProcedureCallerPermissions("notify:send"))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("CallProcedure missing reducer permission error = %v, want ErrPermissionDenied", err)
	}

	got, err := rt.CallProcedure(context.Background(), "notify", nil, WithProcedureCallerPermissions("notify:send", "messages:send"))
	if err != nil {
		t.Fatalf("CallProcedure with reducer permission error = %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("CallProcedure return = %q, want ok", got)
	}

	err = rt.Read(context.Background(), func(view LocalReadView) error {
		if got := view.RowCount(messagesTableID); got != 1 {
			return fmt.Errorf("row count = %d, want 1", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestProcedureContextCallReducerNilContextUsesBackground(t *testing.T) {
	rt := buildStartedRuntimeWithReducer(t, "send_message", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer rt.Close()

	pctx := &ProcedureContext{runtime: rt, active: true}
	res, err := pctx.CallReducer("send_message", nil)
	if err != nil {
		t.Fatalf("ProcedureContext.CallReducer returned admission error: %v", err)
	}
	if res.Status != StatusCommitted || string(res.ReturnBSATN) != "ok" {
		t.Fatalf("ProcedureContext.CallReducer result = (%v, %q, %v), want committed ok", res.Status, res.ReturnBSATN, res.Error)
	}
}

func TestProcedureContextExpiresAfterHandlerExit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*ProcedureContext) ([]byte, error)
		wantErr error
	}{
		{
			name: "success",
			handler: func(*ProcedureContext) ([]byte, error) {
				return []byte("ok"), nil
			},
		},
		{
			name: "error",
			handler: func(*ProcedureContext) ([]byte, error) {
				return nil, errors.New("handler failed")
			},
			wantErr: errors.New("handler failed"),
		},
		{
			name: "panic",
			handler: func(*ProcedureContext) ([]byte, error) {
				panic("handler panicked")
			},
			wantErr: ErrProcedurePanicked,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var escaped *ProcedureContext
			rt, err := Build(validChatModule().
				Reducer("after_return", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
					return nil, nil
				}).
				Procedure("escape", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
					escaped = ctx
					return tc.handler(ctx)
				}), Config{DataDir: t.TempDir()})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if err := rt.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer rt.Close()

			_, callErr := rt.CallProcedure(context.Background(), "escape", nil)
			if tc.wantErr == nil && callErr != nil {
				t.Fatalf("CallProcedure: %v", callErr)
			}
			if tc.wantErr != nil {
				if errors.Is(tc.wantErr, ErrProcedurePanicked) {
					if !errors.Is(callErr, ErrProcedurePanicked) {
						t.Fatalf("CallProcedure error = %v, want ErrProcedurePanicked", callErr)
					}
				} else if callErr == nil || callErr.Error() != tc.wantErr.Error() {
					t.Fatalf("CallProcedure error = %v, want %v", callErr, tc.wantErr)
				}
			}
			if escaped == nil {
				t.Fatal("procedure context was not captured")
			}
			if _, err := escaped.CallReducer("after_return", nil); !errors.Is(err, ErrProcedureContextExpired) {
				t.Fatalf("escaped CallReducer error = %v, want ErrProcedureContextExpired", err)
			}
		})
	}
}

func TestProcedureContextExpiresAfterCancellation(t *testing.T) {
	var escaped *ProcedureContext
	entered := make(chan struct{})
	rt := buildStartedRuntimeWithProcedure(t, "wait", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
		escaped = ctx
		close(entered)
		<-ctx.Context.Done()
		return nil, ctx.Context.Err()
	})
	defer rt.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := rt.CallProcedure(ctx, "wait", nil)
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("CallProcedure error = %v, want context.Canceled", err)
	}
	if _, err := escaped.CallReducer("missing", nil); !errors.Is(err, ErrProcedureContextExpired) {
		t.Fatalf("escaped CallReducer error = %v, want ErrProcedureContextExpired", err)
	}
}

func TestProcedureReturnWaitsForRacingCallReducer(t *testing.T) {
	reducerEntered := make(chan struct{})
	releaseReducer := make(chan struct{})
	reducerDone := make(chan error, 1)
	var escaped *ProcedureContext
	rt, err := Build(validChatModule().
		Reducer("racing", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
			close(reducerEntered)
			<-releaseReducer
			return nil, nil
		}).
		Procedure("race", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
			escaped = ctx
			go func() {
				res, err := ctx.CallReducer("racing", nil)
				if err == nil {
					err = res.Error
				}
				reducerDone <- err
			}()
			<-reducerEntered
			return []byte("ok"), nil
		}), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	procedureDone := make(chan error, 1)
	go func() {
		_, err := rt.CallProcedure(context.Background(), "race", nil)
		procedureDone <- err
	}()
	<-reducerEntered
	select {
	case err := <-procedureDone:
		t.Fatalf("CallProcedure returned before its racing reducer completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseReducer)
	if err := <-reducerDone; err != nil {
		t.Fatalf("racing CallReducer: %v", err)
	}
	if err := <-procedureDone; err != nil {
		t.Fatalf("CallProcedure: %v", err)
	}
	if _, err := escaped.CallReducer("racing", nil); !errors.Is(err, ErrProcedureContextExpired) {
		t.Fatalf("post-return CallReducer error = %v, want ErrProcedureContextExpired", err)
	}
}

func TestProtocolProcedureReducerProducesOneResponseThenCallerLightDelta(t *testing.T) {
	const messagesTableID schema.TableID = 0
	var nextID uint64
	insert := func(ctx *schema.ReducerContext, _ []byte) ([]byte, error) {
		nextID++
		_, err := ctx.DB.Insert(uint32(messagesTableID), types.ProductValue{
			types.NewUint64(nextID),
			types.NewString("from reducer"),
		})
		return nil, err
	}
	rt, err := Build(validChatModule().
		Reducer("insert_message", insert).
		Procedure("insert_from_procedure", func(ctx *ProcedureContext, _ []byte) ([]byte, error) {
			result, err := ctx.CallReducer("insert_message", nil)
			if err != nil {
				return nil, err
			}
			if result.Error != nil {
				return nil, result.Error
			}
			return []byte("inserted"), nil
		}), Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	server := httptest.NewServer(rt.HTTPHandler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := protocolclient.Dial(ctx, protocolclient.Options{
		URL:            "ws" + strings.TrimPrefix(server.URL, "http") + "/subscribe",
		AllowAnonymous: true,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close(ctx)

	if err := client.Send(ctx, protocol.SubscribeSingleMsg{
		RequestID:   71,
		QueryID:     72,
		QueryString: "SELECT * FROM messages",
	}); err != nil {
		t.Fatalf("send subscription: %v", err)
	}
	if tag, msg, err := client.Read(ctx); err != nil {
		t.Fatalf("read subscription applied: %v", err)
	} else if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("subscription response = tag %d %T, want SubscribeSingleApplied", tag, msg)
	}

	response, err := client.CallProcedure(ctx, "insert_from_procedure", nil)
	if err != nil {
		t.Fatalf("CallProcedure: %v", err)
	}
	if string(response.Result) != "inserted" {
		t.Fatalf("procedure result = %q, want inserted", response.Result)
	}

	tag, msg, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read procedure subscription delta: %v", err)
	}
	light, ok := msg.(protocol.TransactionUpdateLight)
	if tag != protocol.TagTransactionUpdateLight || !ok {
		t.Fatalf("procedure delta = tag %d %T, want TransactionUpdateLight", tag, msg)
	}
	if light.RequestID != 0 || len(light.Update) != 1 || light.Update[0].QueryID != 72 {
		t.Fatalf("procedure light delta = %+v, want request 0 query 72", light)
	}
	rows, err := protocol.DecodeRowList(light.Update[0].Inserts)
	if err != nil || len(rows) != 1 {
		t.Fatalf("procedure light inserts = %d rows, error %v; want one row", len(rows), err)
	}

	direct, err := client.CallReducer(ctx, "insert_message", nil)
	if err != nil {
		t.Fatalf("CallReducer after procedure: %v", err)
	}
	committed, ok := direct.Status.(protocol.StatusCommitted)
	if !ok || len(committed.Update) != 1 || committed.Update[0].QueryID != 72 {
		t.Fatalf("direct reducer update = %#v, want one heavy caller delta for query 72", direct.Status)
	}
}

func TestHandleCallProcedureUsesRuntimeClientSender(t *testing.T) {
	rt := buildStartedRuntimeWithProcedureAndConfig(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	}, Config{DataDir: t.TempDir(), EnableProtocol: true})
	defer rt.Close()

	sender := &declaredReadCapturingSender{}
	rt.mu.Lock()
	rt.protocolSender = sender
	rt.mu.Unlock()

	conn := newProcedureProtocolTestConn()
	rt.HandleCallProcedure(context.Background(), conn, &protocol.CallProcedureMsg{
		MessageID: []byte("procedure-message"),
		Name:      "notify",
	})

	sendCalls, gotConnID, gotMsg, heavyCalls, lightCalls := sender.snapshot()
	if sendCalls != 1 {
		t.Fatalf("sender Send calls = %d, want 1", sendCalls)
	}
	if heavyCalls != 0 || lightCalls != 0 {
		t.Fatalf("transaction sender calls = heavy:%d light:%d, want 0/0", heavyCalls, lightCalls)
	}
	if gotConnID != conn.ID {
		t.Fatalf("sender conn ID = %x, want %x", gotConnID[:], conn.ID[:])
	}
	if got := len(conn.OutboundCh); got != 0 {
		t.Fatalf("conn outbound queue length = %d, want 0; procedure responses must use ClientSender", got)
	}

	resp, ok := gotMsg.(protocol.ProcedureResponse)
	if !ok {
		t.Fatalf("sender msg = %T, want ProcedureResponse", gotMsg)
	}
	if string(resp.MessageID) != "procedure-message" {
		t.Fatalf("procedure response message ID = %q, want procedure-message", resp.MessageID)
	}
	if resp.Error != nil || string(resp.Result) != "ok" {
		t.Fatalf("procedure response = result %q error %v, want ok nil", resp.Result, resp.Error)
	}
	if resp.TotalHostExecutionDuration <= 0 {
		t.Fatalf("procedure duration = %d, want positive", resp.TotalHostExecutionDuration)
	}
}

func TestHandleCallProcedureReportsProcedureError(t *testing.T) {
	rt := buildStartedRuntimeWithProcedureAndConfig(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return nil, errors.New("notify failed")
	}, Config{DataDir: t.TempDir(), EnableProtocol: true})
	defer rt.Close()

	sender := &declaredReadCapturingSender{}
	rt.mu.Lock()
	rt.protocolSender = sender
	rt.mu.Unlock()

	rt.HandleCallProcedure(context.Background(), newProcedureProtocolTestConn(), &protocol.CallProcedureMsg{
		MessageID: []byte("procedure-error"),
		Name:      "notify",
	})

	_, _, gotMsg, _, _ := sender.snapshot()
	resp, ok := gotMsg.(protocol.ProcedureResponse)
	if !ok {
		t.Fatalf("sender msg = %T, want ProcedureResponse", gotMsg)
	}
	if resp.Error == nil || !strings.Contains(*resp.Error, "notify failed") {
		t.Fatalf("procedure response error = %v, want notify failed", resp.Error)
	}
	if len(resp.Result) != 0 {
		t.Fatalf("procedure response result = %x, want empty on error", resp.Result)
	}
}

func TestCallProcedureEnforcesConfiguredResultLimit(t *testing.T) {
	rt := buildStartedRuntimeWithProcedureAndConfig(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return []byte("too large"), nil
	}, Config{DataDir: t.TempDir(), ProcedureResultMaxBytes: 4})
	defer rt.Close()

	result, err := rt.CallProcedure(context.Background(), "notify", nil)
	if !errors.Is(err, ErrProcedureResultLimit) {
		t.Fatalf("CallProcedure error = %v, want ErrProcedureResultLimit", err)
	}
	if len(result) != 0 {
		t.Fatalf("CallProcedure result = %q, want empty on limit failure", result)
	}
}

func TestHandleCallProcedureConvertsOversizedWireResponseToCorrelatedError(t *testing.T) {
	logs := &recordingLogState{}
	metrics := &recordingMetricsRecorder{}
	rt := buildStartedRuntimeWithProcedureAndConfig(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return make([]byte, 1024), nil
	}, Config{
		DataDir:        t.TempDir(),
		EnableProtocol: true,
		Observability: ObservabilityConfig{
			Logger:  logs.logger(),
			Metrics: MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	defer rt.Close()

	opts := protocol.DefaultProtocolOptions()
	opts.MaxOutboundMessageSize = 128
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	mgr := protocol.NewConnManager()
	if err := mgr.Add(conn); err != nil {
		t.Fatalf("ConnManager.Add: %v", err)
	}
	rt.mu.Lock()
	rt.protocolSender = protocol.NewClientSender(mgr, nil)
	rt.mu.Unlock()
	messageID := []byte("procedure-size")

	rt.HandleCallProcedure(context.Background(), conn, &protocol.CallProcedureMsg{
		MessageID: messageID,
		Name:      "notify",
	})

	select {
	case frame := <-conn.OutboundCh:
		tag, msg, err := protocol.DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("DecodeServerMessage: %v", err)
		}
		response, ok := msg.(protocol.ProcedureResponse)
		if tag != protocol.TagProcedureResponse || !ok {
			t.Fatalf("response = tag %d %T, want ProcedureResponse", tag, msg)
		}
		if !bytes.Equal(response.MessageID, messageID) {
			t.Fatalf("response message ID = %x, want %x", response.MessageID, messageID)
		}
		if response.Error == nil || !strings.Contains(*response.Error, "outbound message limit") {
			t.Fatalf("response error = %v, want outbound message limit", response.Error)
		}
		if len(response.Result) != 0 {
			t.Fatalf("response result retained %d bytes", len(response.Result))
		}
	default:
		t.Fatal("procedure response was not delivered")
	}
	metrics.requireCounter(t, MetricProtocolMessagesTotal, MetricLabels{
		Module:  "chat",
		Runtime: "default",
		Kind:    "call_procedure",
		Result:  "response_too_large",
	}, 1)
	requireNoProtocolSendFailureLog(t, logs)
	requireNoProtocolMessageResult(t, metrics, "connection_closed")
}

func TestSendProtocolProcedureMessageNilConnNoops(t *testing.T) {
	var rt *Runtime
	if err := rt.sendProtocolProcedureMessage(nil, protocol.ProcedureResponse{}); err != nil {
		t.Fatalf("sendProtocolProcedureMessage nil conn error = %v, want nil", err)
	}
}

func TestSendProtocolProcedureMessageNilRuntimeReturnsNotReady(t *testing.T) {
	var rt *Runtime
	err := rt.sendProtocolProcedureMessage(newProcedureProtocolTestConn(), protocol.ProcedureResponse{})
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("sendProtocolProcedureMessage nil runtime error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestSendProtocolProcedureMessageRequiresReadyRuntime(t *testing.T) {
	rt := buildValidTestRuntime(t)

	err := rt.sendProtocolProcedureMessage(newProcedureProtocolTestConn(), protocol.ProcedureResponse{})
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("sendProtocolProcedureMessage before Start error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestSendProtocolProcedureMessageClosedRuntimeReturnsClosed(t *testing.T) {
	rt := buildStartedRuntimeWithProcedureAndConfig(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	}, Config{DataDir: t.TempDir(), EnableProtocol: true})
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := rt.sendProtocolProcedureMessage(newProcedureProtocolTestConn(), protocol.ProcedureResponse{})
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("sendProtocolProcedureMessage closed runtime error = %v, want ErrRuntimeClosed", err)
	}
}

func TestSendProtocolProcedureMessageMissingProtocolSenderReturnsNotReady(t *testing.T) {
	rt := buildStartedRuntimeWithProcedure(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer rt.Close()

	err := rt.sendProtocolProcedureMessage(newProcedureProtocolTestConn(), protocol.ProcedureResponse{})
	if !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("sendProtocolProcedureMessage missing sender error = %v, want ErrRuntimeNotReady", err)
	}
}

func TestSendProtocolProcedureMessageOverflowUsesClientSenderBackpressure(t *testing.T) {
	rt := buildStartedRuntimeWithProcedureAndConfig(t, "notify", func(_ *ProcedureContext, _ []byte) ([]byte, error) {
		return []byte("ok"), nil
	}, Config{DataDir: t.TempDir(), EnableProtocol: true})
	defer rt.Close()

	opts := protocol.DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	opts.DisconnectTimeout = 500 * time.Millisecond
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	mgr := protocol.NewConnManager()
	if err := mgr.Add(conn); err != nil {
		t.Fatalf("ConnManager.Add: %v", err)
	}
	inbox := &procedureProtocolInbox{}
	rt.mu.Lock()
	rt.protocolSender = protocol.NewClientSender(mgr, inbox)
	rt.mu.Unlock()

	conn.OutboundCh <- []byte{0xff}
	err := rt.sendProtocolProcedureMessage(conn, protocol.ProcedureResponse{
		MessageID: []byte("overflow"),
		Result:    []byte("ok"),
	})
	if !errors.Is(err, protocol.ErrClientBufferFull) {
		t.Fatalf("sendProtocolProcedureMessage overflow error = %v, want ErrClientBufferFull", err)
	}
	if got := len(conn.OutboundCh); got != 1 {
		t.Fatalf("overflow procedure response was enqueued; OutboundCh len = %d, want 1", got)
	}
}

func buildStartedRuntimeWithProcedure(t *testing.T, name string, handler ProcedureHandler) *Runtime {
	t.Helper()
	return buildStartedRuntimeWithProcedureAndConfig(t, name, handler, Config{DataDir: t.TempDir()})
}

func buildStartedRuntimeWithProcedureAndConfig(t *testing.T, name string, handler ProcedureHandler, cfg Config) *Runtime {
	t.Helper()
	rt, err := Build(validChatModule().Procedure(name, handler), cfg)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	return rt
}

func newProcedureProtocolTestConn() *protocol.Conn {
	opts := protocol.DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := protocol.NewConn(protocol.GenerateConnectionID(), types.Identity{1}, "", false, nil, &opts)
	conn.Permissions = []string{"notify:send", "messages:send"}
	conn.AllowAllPermissions = true
	conn.Principal = types.AuthPrincipal{Subject: "procedure-test"}
	return conn
}

type procedureProtocolInbox struct{}

func (procedureProtocolInbox) OnConnect(context.Context, types.ConnectionID, types.Identity, types.AuthPrincipal) error {
	return nil
}

func (procedureProtocolInbox) OnDisconnect(context.Context, types.ConnectionID, types.Identity, types.AuthPrincipal) error {
	return nil
}

func (procedureProtocolInbox) DisconnectClientSubscriptions(context.Context, types.ConnectionID) error {
	return nil
}

func (procedureProtocolInbox) RegisterSubscriptionSet(context.Context, protocol.RegisterSubscriptionSetRequest) error {
	return nil
}

func (procedureProtocolInbox) UnregisterSubscriptionSet(context.Context, protocol.UnregisterSubscriptionSetRequest) error {
	return nil
}

func (procedureProtocolInbox) CallReducer(context.Context, protocol.CallReducerRequest) error {
	return nil
}
