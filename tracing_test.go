package shunter

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func TestTracingDisabledWithTracerRecordsNoSpans(t *testing.T) {
	tracer := &recordingTracer{}
	rt, err := Build(validChatModule().Reducer("insert_message", insertMessageReducer), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Enabled: false,
				Tracer:  tracer,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()
	if res, err := rt.CallReducer(context.Background(), "insert_message", []byte("hello")); err != nil || res.Status != StatusCommitted {
		t.Fatalf("CallReducer = (%v, %v), want committed", res, err)
	}
	if got := tracer.spanCount(); got != 0 {
		t.Fatalf("disabled tracing recorded %d spans: %+v", got, tracer.snapshot())
	}
}

func TestTracingRecordsRequiredSpansAndAttributes(t *testing.T) {
	tracer := &recordingTracer{}
	cfg := declaredReadProtocolConfig(t)
	cfg.Observability = ObservabilityConfig{
		RuntimeLabel: "trace-runtime",
		Tracing: TracingConfig{
			Enabled: true,
			Tracer:  tracer,
		},
	}
	rt, err := Build(validChatModule().
		Reducer("insert_message", insertMessageReducer).
		View(ViewDeclaration{Name: "live_messages", SQL: "SELECT * FROM messages"}), cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	if res, err := rt.CallReducer(context.Background(), "insert_message", []byte("hello")); err != nil || res.Status != StatusCommitted {
		t.Fatalf("CallReducer = (%v, %v), want committed", res, err)
	}
	if _, err := rt.SubscribeView(context.Background(), "live_messages", 17); err != nil {
		t.Fatalf("SubscribeView: %v", err)
	}
	unregisterRuntimeSubscription(t, rt, types.ConnectionID{}, 17)

	client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "trace-client"))
	writeDeclaredReadProtocolMessage(t, client, protocol.OneOffQueryMsg{
		MessageID:   []byte("trace-query"),
		QueryString: "SELECT * FROM messages WHERE body = 'token=secret'",
	})
	requireDeclaredReadOneOffError(t, client, "no such table")

	required := []string{
		"shunter.runtime.start",
		"shunter.recovery.open",
		"shunter.protocol.message",
		"shunter.reducer.call",
		"shunter.store.commit",
		"shunter.durability.batch",
		"shunter.subscription.eval",
		"shunter.subscription.fanout",
		"shunter.query.one_off",
		"shunter.subscription.register",
		"shunter.subscription.unregister",
	}
	for _, name := range required {
		tracer.waitForSpan(t, name)
	}

	for _, span := range tracer.snapshot() {
		if span.attrs["component"] == "" {
			t.Fatalf("span %s missing component attr: %+v", span.name, span.attrs)
		}
		if span.attrs["module"] != "chat" {
			t.Fatalf("span %s module = %#v, want chat", span.name, span.attrs["module"])
		}
		if span.attrs["runtime"] != "trace-runtime" {
			t.Fatalf("span %s runtime = %#v, want trace-runtime", span.name, span.attrs["runtime"])
		}
	}

	requireTraceAttr(t, tracer, "shunter.runtime.start", "state", string(RuntimeStateReady))
	requireTraceAttr(t, tracer, "shunter.recovery.open", "result", "success")
	requireTraceAttrPresent(t, tracer, "shunter.recovery.open", "tx_id")
	requireTraceAttr(t, tracer, "shunter.protocol.message", "kind", "one_off_query")
	requireTraceAttr(t, tracer, "shunter.protocol.message", "result", "validation_error")
	requireTraceAttr(t, tracer, "shunter.reducer.call", "reducer", "insert_message")
	requireTraceAttr(t, tracer, "shunter.reducer.call", "result", "committed")
	requireTraceAttr(t, tracer, "shunter.store.commit", "result", "ok")
	requireTraceAttrPresent(t, tracer, "shunter.store.commit", "tx_id")
	requireTraceAttr(t, tracer, "shunter.durability.batch", "result", "ok")
	requireTraceAttrPresent(t, tracer, "shunter.durability.batch", "tx_id")
	requireTraceAttr(t, tracer, "shunter.subscription.eval", "result", "ok")
	requireTraceAttrPresent(t, tracer, "shunter.subscription.eval", "tx_id")
	requireTraceAttr(t, tracer, "shunter.subscription.fanout", "result", "error")
	requireTraceAttr(t, tracer, "shunter.subscription.fanout", "reason", "connection_closed")
	requireTraceAttr(t, tracer, "shunter.query.one_off", "result", "validation_error")
	requireTraceAttr(t, tracer, "shunter.subscription.register", "result", "ok")
	requireTraceAttr(t, tracer, "shunter.subscription.unregister", "result", "ok")
}

func TestTracingRedactsFailureErrorsAndExcludesSensitiveAttributes(t *testing.T) {
	tracer := &recordingTracer{}
	rt, err := Build(validChatModule().Reducer("fail_secret", func(_ *schema.ReducerContext, _ []byte) ([]byte, error) {
		return nil, errors.New("authorization=Bearer abc.def args=raw-args row={secret-row} token=raw-token signing_key=raw-key sql=select * from messages where token='raw-token'; trailing text")
	}), Config{
		DataDir:        t.TempDir(),
		AuthSigningKey: []byte("raw-signing-key"),
		Observability: ObservabilityConfig{
			Redaction: RedactionConfig{ErrorMessageMaxBytes: 96},
			Tracing: TracingConfig{
				Enabled: true,
				Tracer:  tracer,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rt.Close()

	args := []byte("args=raw-args row={secret-row} token=raw-token signing_key=raw-key sql=select * from messages")
	res, err := rt.CallReducer(context.Background(), "fail_secret", args)
	if err != nil {
		t.Fatalf("CallReducer admission: %v", err)
	}
	if res.Status != StatusFailedUser {
		t.Fatalf("CallReducer status = %v, want failed user", res.Status)
	}

	span := tracer.waitForSpan(t, "shunter.reducer.call")
	if got := fmt.Sprint(span.endErr); strings.Contains(got, "raw-token") ||
		strings.Contains(got, "raw-key") ||
		strings.Contains(got, "raw-args") ||
		strings.Contains(got, "secret-row") ||
		strings.Contains(got, "select * from messages") {
		t.Fatalf("span end error leaked sensitive text: %q", got)
	}
	if got := fmt.Sprint(span.endErr); len(got) > 96 {
		t.Fatalf("span end error length = %d, want <= 96: %q", len(got), got)
	}
	for _, span := range tracer.snapshot() {
		for key, value := range span.attrs {
			got := fmt.Sprint(value)
			if strings.Contains(got, "raw-token") ||
				strings.Contains(got, "raw-key") ||
				strings.Contains(got, "raw-args") ||
				strings.Contains(got, "secret-row") ||
				strings.Contains(got, "select * from messages") {
				t.Fatalf("span %s attr %s leaked sensitive text: %#v", span.name, key, value)
			}
		}
	}
}

func TestTracingPanicsAndNilSpansDoNotChangeRuntimeResults(t *testing.T) {
	for _, tt := range []struct {
		name   string
		tracer Tracer
	}{
		{name: "start span panic", tracer: panicStartTracer{}},
		{name: "add event panic", tracer: addEventPanicTracer{}},
		{name: "end panic", tracer: endPanicTracer{}},
		{name: "nil span", tracer: nilSpanTracer{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := declaredReadProtocolConfig(t)
			cfg.Observability = ObservabilityConfig{
				Tracing: TracingConfig{
					Enabled: true,
					Tracer:  tt.tracer,
				},
			}
			rt, err := Build(validChatModule().
				Reducer("insert_message", insertMessageReducer).
				View(ViewDeclaration{Name: "live_messages", SQL: "SELECT * FROM messages"}), cfg)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if err := rt.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer rt.Close()

			res, err := rt.CallReducer(context.Background(), "insert_message", []byte("hello"))
			if err != nil {
				t.Fatalf("CallReducer admission: %v", err)
			}
			if res.Status != StatusCommitted {
				t.Fatalf("CallReducer status = %v, want committed; err=%v", res.Status, res.Error)
			}
			if _, err := rt.SubscribeView(context.Background(), "live_messages", 19); err != nil {
				t.Fatalf("SubscribeView: %v", err)
			}
			unregisterRuntimeSubscription(t, rt, types.ConnectionID{}, 19)

			client := dialDeclaredReadProtocol(t, rt, mintDeclaredReadProtocolToken(t, "panic-client"))
			writeDeclaredReadProtocolMessage(t, client, protocol.OneOffQueryMsg{
				MessageID:   []byte("panic-query"),
				QueryString: "SELECT * FROM messages",
			})
			requireDeclaredReadOneOffError(t, client, "no such table")
		})
	}
}

func unregisterRuntimeSubscription(t *testing.T, rt *Runtime, connID types.ConnectionID, queryID uint32) {
	t.Helper()
	exec, err := rt.readyExecutor()
	if err != nil {
		t.Fatalf("readyExecutor: %v", err)
	}
	reply := make(chan error, 1)
	cmd := executor.UnregisterSubscriptionSetCmd{
		ConnID:  connID,
		QueryID: queryID,
		Reply: func(_ subscription.SubscriptionSetUnregisterResult, err error) {
			reply <- err
		},
		Context: context.Background(),
	}
	if err := exec.SubmitWithContext(context.Background(), cmd); err != nil {
		t.Fatalf("submit unregister: %v", err)
	}
	select {
	case err := <-reply:
		if err != nil {
			t.Fatalf("unregister subscription: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for unregister reply")
	}
}

type recordedTraceSpan struct {
	name   string
	attrs  map[string]any
	events []recordedTraceEvent
	endErr error
	ended  bool
}

type recordedTraceEvent struct {
	name  string
	attrs map[string]any
}

type recordingTracer struct {
	mu    sync.Mutex
	spans []*recordingSpan
}

func (t *recordingTracer) StartSpan(ctx context.Context, name string, attrs ...TraceAttr) (context.Context, Span) {
	span := &recordingSpan{name: name, attrs: traceAttrsMap(attrs)}
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
	return ctx, span
}

func (t *recordingTracer) spanCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.spans)
}

func (t *recordingTracer) snapshot() []recordedTraceSpan {
	t.mu.Lock()
	spans := slices.Clone(t.spans)
	t.mu.Unlock()
	out := make([]recordedTraceSpan, 0, len(spans))
	for _, span := range spans {
		out = append(out, span.snapshot())
	}
	return out
}

func (t *recordingTracer) waitForSpan(tb testing.TB, name string) recordedTraceSpan {
	tb.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, span := range t.snapshot() {
			if span.name == name && span.ended {
				return span
			}
		}
		if time.Now().After(deadline) {
			tb.Fatalf("missing ended trace span %q in %+v", name, t.snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type recordingSpan struct {
	mu     sync.Mutex
	name   string
	attrs  map[string]any
	events []recordedTraceEvent
	endErr error
	ended  bool
}

func (s *recordingSpan) AddEvent(name string, attrs ...TraceAttr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, recordedTraceEvent{name: name, attrs: traceAttrsMap(attrs)})
}

func (s *recordingSpan) End(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endErr = err
	s.ended = true
}

func (s *recordingSpan) snapshot() recordedTraceSpan {
	s.mu.Lock()
	defer s.mu.Unlock()
	attrs := make(map[string]any, len(s.attrs))
	for key, value := range s.attrs {
		attrs[key] = value
	}
	events := slices.Clone(s.events)
	return recordedTraceSpan{
		name:   s.name,
		attrs:  attrs,
		events: events,
		endErr: s.endErr,
		ended:  s.ended,
	}
}

func traceAttrsMap(attrs []TraceAttr) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		out[attr.Key] = attr.Value
	}
	return out
}

func requireTraceAttr(t *testing.T, tracer *recordingTracer, spanName, key string, want any) {
	t.Helper()
	for _, span := range tracer.snapshot() {
		if span.name != spanName {
			continue
		}
		if got, ok := span.attrs[key]; ok && fmt.Sprint(got) == fmt.Sprint(want) {
			return
		}
	}
	t.Fatalf("missing trace attr %s=%v on %s in %+v", key, want, spanName, tracer.snapshot())
}

func requireTraceAttrPresent(t *testing.T, tracer *recordingTracer, spanName, key string) {
	t.Helper()
	for _, span := range tracer.snapshot() {
		if span.name != spanName {
			continue
		}
		if _, ok := span.attrs[key]; ok {
			return
		}
	}
	t.Fatalf("missing trace attr %s on %s in %+v", key, spanName, tracer.snapshot())
}

type panicStartTracer struct{}

func (panicStartTracer) StartSpan(context.Context, string, ...TraceAttr) (context.Context, Span) {
	panic("tracer start panic token=secret")
}

type nilSpanTracer struct{}

func (nilSpanTracer) StartSpan(ctx context.Context, _ string, _ ...TraceAttr) (context.Context, Span) {
	return ctx, nil
}

type addEventPanicTracer struct{}

func (addEventPanicTracer) StartSpan(ctx context.Context, _ string, _ ...TraceAttr) (context.Context, Span) {
	return ctx, addEventPanicSpan{}
}

type addEventPanicSpan struct{}

func (addEventPanicSpan) AddEvent(string, ...TraceAttr) { panic("span add event panic token=secret") }
func (addEventPanicSpan) End(error)                     {}

type endPanicTracer struct{}

func (endPanicTracer) StartSpan(ctx context.Context, _ string, _ ...TraceAttr) (context.Context, Span) {
	return ctx, endPanicSpan{}
}

type endPanicSpan struct{}

func (endPanicSpan) AddEvent(string, ...TraceAttr) {}
func (endPanicSpan) End(error)                     { panic("span end panic token=secret") }
