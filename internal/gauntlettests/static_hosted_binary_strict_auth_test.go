package shunter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/protocolclient"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	hostedChatStrictAuthIssuer   = "hosted-chat-gauntlet"
	hostedChatStrictAuthAudience = "hosted-chat"
)

func TestStaticHostedBinaryGauntletHostedChatStrictAuth(t *testing.T) {
	signingKey := []byte("hosted-chat-gauntlet-strict-auth-signing-key")
	validToken := mintHostedChatGauntletToken(t, signingKey, hostedChatStrictAuthIssuer, hostedChatStrictAuthAudience)
	wrongIssuerToken := mintHostedChatGauntletToken(t, signingKey, "other-hosted-chat-issuer", hostedChatStrictAuthAudience)
	wrongAudienceToken := mintHostedChatGauntletToken(t, signingKey, hostedChatStrictAuthIssuer, "other-hosted-chat-audience")
	malformedToken := "malformed.jwt.token"
	tokenRedactions := []string{string(signingKey), validToken, wrongIssuerToken, wrongAudienceToken, malformedToken}

	bin := buildHostedChatBinary(t)
	dataDir := t.TempDir()
	addr := freeHostedChatListenAddr(t)
	server := startHostedChatBinary(t, bin, dataDir, addr, signingKey, tokenRedactions)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat server logs:\n%s", server.LogsForFailure())
		}
		server.StopCleanly(t)
	})

	waitForHostedChatProtocolReady(t, server, validToken)
	assertHostedChatDataDirBuildMetadata(t, dataDir)

	assertGauntletProtocolAuthClose(t, server.WebSocketURL(), nil, "hosted-chat strict missing token")
	assertGauntletProtocolAuthClose(t, server.WebSocketURL(), gauntletBearerHeader(malformedToken), "hosted-chat strict malformed token")
	assertGauntletProtocolAuthClose(t, server.WebSocketURL(), gauntletBearerHeader(wrongIssuerToken), "hosted-chat strict wrong issuer")
	assertGauntletProtocolAuthClose(t, server.WebSocketURL(), gauntletBearerHeader(wrongAudienceToken), "hosted-chat strict wrong audience")

	messageBody := "strict auth binary reducer"
	args := encodeHostedChatSendMessageArgs(t, "Ada", messageBody)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	identity, update, err := protocolclient.DialAndCallReducer(ctx, hostedChatProtocolOptions(server, validToken), protocolclient.ReducerCallRequest{
		Name:      "send_message",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("valid token send_message failed: %v", err)
	}
	if identity.Token != "" {
		t.Fatalf("strict auth identity included minted token length %d, want empty", len(identity.Token))
	}
	if _, ok := update.Status.(protocol.StatusCommitted); !ok {
		t.Fatalf("send_message status = %T, want StatusCommitted", update.Status)
	}

	_, response, err := protocolclient.DialAndExecuteDeclaredQuery(ctx, hostedChatProtocolOptions(server, validToken), protocolclient.DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if err != nil {
		t.Fatalf("valid token recent_messages failed: %v", err)
	}
	messages := decodeHostedChatMessages(t, response, "valid token recent_messages")
	if !hostedChatMessagesContain(messages, "Ada", messageBody) {
		t.Fatalf("recent_messages rows = %+v, want Ada message body %q", messages, messageBody)
	}

	assertHostedChatLogsDoNotContainTokenMaterial(t, server.logs.String(), tokenRedactions)
}

func TestStaticHostedBinaryGauntletHostedChatLiveSubscriptionAndCleanRestart(t *testing.T) {
	signingKey := []byte("hosted-chat-gauntlet-live-subscription-signing-key")
	validToken := mintHostedChatGauntletToken(t, signingKey, hostedChatStrictAuthIssuer, hostedChatStrictAuthAudience)
	tokenRedactions := []string{string(signingKey), validToken}

	bin := buildHostedChatBinary(t)
	dataDir := t.TempDir()
	addr := freeHostedChatListenAddr(t)
	server := startHostedChatBinary(t, bin, dataDir, addr, signingKey, tokenRedactions)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat first server logs:\n%s", server.LogsForFailure())
		}
		server.StopCleanly(t)
	})
	waitForHostedChatProtocolReady(t, server, validToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	subscriber, _, err := protocolclient.Dial(ctx, hostedChatProtocolOptions(server, validToken))
	if err != nil {
		t.Fatalf("dial live_messages subscriber: %v", err)
	}
	defer subscriber.Close(context.Background())

	const (
		subscribeRequestID = uint32(4101)
		subscribeQueryID   = uint32(4102)
	)
	if err := subscriber.Send(ctx, protocol.SubscribeDeclaredViewMsg{
		RequestID: subscribeRequestID,
		QueryID:   subscribeQueryID,
		Name:      "live_messages",
	}); err != nil {
		t.Fatalf("subscribe live_messages: %v", err)
	}
	initial := readHostedChatSubscribeApplied(t, subscriber, subscribeRequestID, subscribeQueryID, "live_messages initial")
	if len(initial) != 0 {
		t.Fatalf("live_messages initial rows = %+v, want empty", initial)
	}

	messageBody := "strict auth binary live subscription"
	args := encodeHostedChatSendMessageArgs(t, "Grace", messageBody)
	_, update, err := protocolclient.DialAndCallReducer(ctx, hostedChatProtocolOptions(server, validToken), protocolclient.ReducerCallRequest{
		Name:      "send_message",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("send_message for live subscription failed: %v", err)
	}
	if _, ok := update.Status.(protocol.StatusCommitted); !ok {
		t.Fatalf("send_message live status = %T, want StatusCommitted", update.Status)
	}

	delta := readHostedChatTransactionDelta(t, subscriber, subscribeQueryID, "live_messages delta")
	if !hostedChatMessagesContain(delta.inserts, "Grace", messageBody) {
		t.Fatalf("live_messages inserts = %+v, want Grace message body %q", delta.inserts, messageBody)
	}
	if len(delta.deletes) != 0 {
		t.Fatalf("live_messages deletes = %+v, want empty", delta.deletes)
	}
	if err := subscriber.Close(context.Background()); err != nil {
		t.Fatalf("close live_messages subscriber: %v", err)
	}

	server.StopCleanly(t)

	restartAddr := freeHostedChatListenAddr(t)
	restarted := startHostedChatBinary(t, bin, dataDir, restartAddr, signingKey, tokenRedactions)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat restarted server logs:\n%s", restarted.LogsForFailure())
		}
		restarted.StopCleanly(t)
	})
	waitForHostedChatProtocolReady(t, restarted, validToken)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	restartedSubscriber, _, err := protocolclient.Dial(ctx, hostedChatProtocolOptions(restarted, validToken))
	cancel()
	if err != nil {
		t.Fatalf("dial restarted live_messages subscriber: %v", err)
	}
	defer restartedSubscriber.Close(context.Background())
	const (
		restartedSubscribeRequestID = uint32(4201)
		restartedSubscribeQueryID   = uint32(4202)
	)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if err := restartedSubscriber.Send(ctx, protocol.SubscribeDeclaredViewMsg{
		RequestID: restartedSubscribeRequestID,
		QueryID:   restartedSubscribeQueryID,
		Name:      "live_messages",
	}); err != nil {
		cancel()
		t.Fatalf("subscribe restarted live_messages: %v", err)
	}
	cancel()
	restartedInitial := readHostedChatSubscribeApplied(t, restartedSubscriber, restartedSubscribeRequestID, restartedSubscribeQueryID, "live_messages restarted initial")
	if !hostedChatMessagesContain(restartedInitial, "Grace", messageBody) {
		t.Fatalf("live_messages restarted initial rows = %+v, want Grace message body %q", restartedInitial, messageBody)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, response, err := protocolclient.DialAndExecuteDeclaredQuery(ctx, hostedChatProtocolOptions(restarted, validToken), protocolclient.DeclaredQueryRequest{
		Name: "recent_messages",
	})
	if err != nil {
		t.Fatalf("recent_messages after clean restart failed: %v", err)
	}
	messages := decodeHostedChatMessages(t, response, "recent_messages after clean restart")
	if !hostedChatMessagesContain(messages, "Grace", messageBody) {
		t.Fatalf("recent_messages after restart rows = %+v, want Grace message body %q", messages, messageBody)
	}

	assertHostedChatLogsDoNotContainTokenMaterial(t, server.logs.String(), tokenRedactions)
	assertHostedChatLogsDoNotContainTokenMaterial(t, restarted.logs.String(), tokenRedactions)
}

func TestStaticHostedBinaryGauntletHostedChatRestoredDataDirRestart(t *testing.T) {
	signingKey := []byte("hosted-chat-gauntlet-restore-signing-key")
	validToken := mintHostedChatGauntletToken(t, signingKey, hostedChatStrictAuthIssuer, hostedChatStrictAuthAudience)
	tokenRedactions := []string{string(signingKey), validToken}

	bin := buildHostedChatBinary(t)
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backup")
	restoredDir := filepath.Join(root, "restored")

	addr := freeHostedChatListenAddr(t)
	server := startHostedChatBinary(t, bin, dataDir, addr, signingKey, tokenRedactions)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat backup source server logs:\n%s", server.LogsForFailure())
		}
		server.StopCleanly(t)
	})
	waitForHostedChatProtocolReady(t, server, validToken)

	messageBody := "strict auth binary restored data dir"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, update, err := protocolclient.DialAndCallReducer(ctx, hostedChatProtocolOptions(server, validToken), protocolclient.ReducerCallRequest{
		Name:      "send_message",
		Arguments: encodeHostedChatSendMessageArgs(t, "Katherine", messageBody),
	})
	cancel()
	if err != nil {
		t.Fatalf("send_message before backup failed: %v", err)
	}
	if _, ok := update.Status.(protocol.StatusCommitted); !ok {
		t.Fatalf("send_message before backup status = %T, want StatusCommitted", update.Status)
	}
	server.StopCleanly(t)

	if err := shunter.BackupDataDir(dataDir, backupDir); err != nil {
		t.Fatalf("BackupDataDir returned error: %v", err)
	}
	if err := shunter.RestoreDataDir(backupDir, restoredDir); err != nil {
		t.Fatalf("RestoreDataDir returned error: %v", err)
	}

	restoredAddr := freeHostedChatListenAddr(t)
	restored := startHostedChatBinary(t, bin, restoredDir, restoredAddr, signingKey, tokenRedactions)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat restored server logs:\n%s", restored.LogsForFailure())
		}
		restored.StopCleanly(t)
	})
	waitForHostedChatProtocolReady(t, restored, validToken)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	_, response, err := protocolclient.DialAndExecuteDeclaredQuery(ctx, hostedChatProtocolOptions(restored, validToken), protocolclient.DeclaredQueryRequest{
		Name: "recent_messages",
	})
	cancel()
	if err != nil {
		t.Fatalf("recent_messages after restore failed: %v", err)
	}
	messages := decodeHostedChatMessages(t, response, "recent_messages after restore")
	if !hostedChatMessagesContain(messages, "Katherine", messageBody) {
		t.Fatalf("recent_messages after restore rows = %+v, want Katherine message body %q", messages, messageBody)
	}

	assertHostedChatLogsDoNotContainTokenMaterial(t, server.logs.String(), tokenRedactions)
	assertHostedChatLogsDoNotContainTokenMaterial(t, restored.logs.String(), tokenRedactions)
}

func TestStaticHostedBinaryGauntletHostedChatUncleanCrashRecovery(t *testing.T) {
	signingKey := []byte("hosted-chat-gauntlet-unclean-crash-signing-key")
	validToken := mintHostedChatGauntletToken(t, signingKey, hostedChatStrictAuthIssuer, hostedChatStrictAuthAudience)
	tokenRedactions := []string{string(signingKey), validToken}

	bin := buildHostedChatBinary(t)
	dataDir := t.TempDir()

	addr := freeHostedChatListenAddr(t)
	server := startHostedChatBinary(t, bin, dataDir, addr, signingKey, tokenRedactions)
	serverKilled := false
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat unclean source server logs:\n%s", server.LogsForFailure())
		}
		if !serverKilled {
			server.StopCleanly(t)
		}
	})
	waitForHostedChatHealthReady(t, server)

	beforeConnectDurableTxID := hostedChatDurableTxID(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	caller, _, err := protocolclient.Dial(ctx, hostedChatProtocolOptions(server, validToken))
	cancel()
	if err != nil {
		t.Fatalf("dial unclean crash caller: %v", err)
	}
	defer func() {
		_ = caller.Close(context.Background())
	}()

	waitForHostedChatDurableTxID(t, server, beforeConnectDurableTxID+1)
	const (
		uncleanSubscribeRequestID = uint32(4301)
		uncleanSubscribeQueryID   = uint32(4302)
	)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if err := caller.Send(ctx, protocol.SubscribeDeclaredViewMsg{
		RequestID: uncleanSubscribeRequestID,
		QueryID:   uncleanSubscribeQueryID,
		Name:      "live_messages",
	}); err != nil {
		cancel()
		t.Fatalf("subscribe live_messages before unclean crash: %v", err)
	}
	cancel()
	initial := readHostedChatSubscribeApplied(t, caller, uncleanSubscribeRequestID, uncleanSubscribeQueryID, "live_messages unclean initial")
	if len(initial) != 0 {
		t.Fatalf("live_messages unclean initial rows = %+v, want empty", initial)
	}
	beforeReducerDurableTxID := hostedChatDurableTxID(t, server)

	messageBody := "strict auth binary unclean crash recovery"
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	update, err := caller.CallReducer(ctx, "send_message", encodeHostedChatSendMessageArgs(t, "Lin", messageBody))
	cancel()
	if err != nil {
		t.Fatalf("send_message before unclean kill failed: %v", err)
	}
	committed, ok := update.Status.(protocol.StatusCommitted)
	if !ok {
		t.Fatalf("send_message before unclean kill status = %T, want StatusCommitted", update.Status)
	}
	delta := decodeHostedChatProtocolUpdates(t, committed.Update, uncleanSubscribeQueryID, "live_messages unclean committed update")
	if !hostedChatMessagesContain(delta.inserts, "Lin", messageBody) {
		t.Fatalf("live_messages unclean committed inserts = %+v, want Lin message body %q", delta.inserts, messageBody)
	}
	waitForHostedChatDurableTxID(t, server, beforeReducerDurableTxID+1)

	server.KillUncleanly(t)
	serverKilled = true

	restartAddr := freeHostedChatListenAddr(t)
	restarted := startHostedChatBinary(t, bin, dataDir, restartAddr, signingKey, tokenRedactions)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hosted-chat unclean restarted server logs:\n%s", restarted.LogsForFailure())
		}
		restarted.StopCleanly(t)
	})
	waitForHostedChatProtocolReady(t, restarted, validToken)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	_, response, err := protocolclient.DialAndExecuteDeclaredQuery(ctx, hostedChatProtocolOptions(restarted, validToken), protocolclient.DeclaredQueryRequest{
		Name: "recent_messages",
	})
	cancel()
	if err != nil {
		t.Fatalf("recent_messages after unclean restart failed: %v", err)
	}
	messages := decodeHostedChatMessages(t, response, "recent_messages after unclean restart")
	if !hostedChatMessagesContain(messages, "Lin", messageBody) {
		t.Fatalf("recent_messages after unclean restart rows = %+v, want Lin message body %q", messages, messageBody)
	}

	assertHostedChatLogsDoNotContainTokenMaterial(t, server.logs.String(), tokenRedactions)
	assertHostedChatLogsDoNotContainTokenMaterial(t, restarted.logs.String(), tokenRedactions)
}

func buildHostedChatBinary(t *testing.T) string {
	t.Helper()
	repoRoot := hostedChatRepoRoot(t)
	bin := filepath.Join(t.TempDir(), "hosted-chat")
	cmd := exec.Command("go", "build", "-o", bin, "./examples/hosted-chat/cmd/hosted-chat")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build hosted-chat binary: %v\n%s", err, out)
	}
	return bin
}

func hostedChatRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func assertHostedChatDataDirBuildMetadata(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, "shunter.datadir.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hosted-chat data dir metadata: %v", err)
	}
	var metadata struct {
		FormatVersion   int `json:"format_version"`
		ContractVersion int `json:"contract_version"`
		Shunter         struct {
			Version string `json:"version"`
			Commit  string `json:"commit,omitempty"`
			Date    string `json:"date,omitempty"`
		} `json:"shunter"`
		Module struct {
			Name          string `json:"name"`
			Version       string `json:"version,omitempty"`
			SchemaVersion int    `json:"schema_version"`
		} `json:"module"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parse hosted-chat data dir metadata: %v", err)
	}
	wantVersion := strings.TrimSpace(string(mustReadHostedChatRepoFile(t, "VERSION")))
	if metadata.FormatVersion != 1 {
		t.Fatalf("data dir metadata format_version = %d, want 1", metadata.FormatVersion)
	}
	if metadata.ContractVersion != 1 {
		t.Fatalf("data dir metadata contract_version = %d, want 1", metadata.ContractVersion)
	}
	if metadata.Shunter.Version != wantVersion {
		t.Fatalf("data dir metadata shunter version = %q, want %q", metadata.Shunter.Version, wantVersion)
	}
	if metadata.Module.Name != "hosted_chat" {
		t.Fatalf("data dir metadata module name = %q, want hosted_chat", metadata.Module.Name)
	}
	if metadata.Module.Version != "v0.1.0" {
		t.Fatalf("data dir metadata module version = %q, want v0.1.0", metadata.Module.Version)
	}
	if metadata.Module.SchemaVersion != 1 {
		t.Fatalf("data dir metadata module schema_version = %d, want 1", metadata.Module.SchemaVersion)
	}
}

func mustReadHostedChatRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(hostedChatRepoRoot(t), rel))
	if err != nil {
		t.Fatalf("read repo file %s: %v", rel, err)
	}
	return data
}

func freeHostedChatListenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate hosted-chat listen address: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close hosted-chat listen probe: %v", err)
	}
	return addr
}

type hostedChatBinaryServer struct {
	cmd    *exec.Cmd
	logs   *lockedLogBuffer
	addr   string
	waitCh chan error
	redact []string

	mu      sync.Mutex
	exited  bool
	waitErr error
}

func startHostedChatBinary(t *testing.T, bin, dataDir, addr string, signingKey []byte, tokenRedactions []string) *hostedChatBinaryServer {
	t.Helper()
	logs := &lockedLogBuffer{}
	cmd := exec.Command(bin)
	cmd.Env = append(hostedChatBaseEnv(),
		"SHUNTER_DATA_DIR="+dataDir,
		"SHUNTER_LISTEN_ADDR="+addr,
		"SHUNTER_ENABLE_PROTOCOL=true",
		"SHUNTER_AUTH_MODE=strict",
		"SHUNTER_AUTH_SIGNING_KEY="+string(signingKey),
		"SHUNTER_AUTH_ISSUERS="+hostedChatStrictAuthIssuer,
		"SHUNTER_AUTH_AUDIENCES="+hostedChatStrictAuthAudience,
	)
	cmd.Stdout = logs
	cmd.Stderr = logs

	if err := cmd.Start(); err != nil {
		t.Fatalf("start hosted-chat binary: %v", err)
	}
	server := &hostedChatBinaryServer{
		cmd:    cmd,
		logs:   logs,
		addr:   addr,
		waitCh: make(chan error, 1),
		redact: append([]string(nil), tokenRedactions...),
	}
	go func() {
		server.waitCh <- cmd.Wait()
	}()
	return server
}

func hostedChatBaseEnv() []string {
	base := os.Environ()
	out := make([]string, 0, len(base))
	for _, entry := range base {
		if !strings.HasPrefix(entry, "SHUNTER_") {
			out = append(out, entry)
		}
	}
	return out
}

func (s *hostedChatBinaryServer) WebSocketURL() string {
	return "ws://" + s.addr + "/subscribe"
}

func (s *hostedChatBinaryServer) LogsForFailure() string {
	if s == nil || s.logs == nil {
		return ""
	}
	return s.logs.RedactedString(s.redact)
}

func (s *hostedChatBinaryServer) PollExit() (bool, error) {
	s.mu.Lock()
	if s.exited {
		err := s.waitErr
		s.mu.Unlock()
		return true, err
	}
	s.mu.Unlock()

	select {
	case err := <-s.waitCh:
		s.mu.Lock()
		s.exited = true
		s.waitErr = err
		s.mu.Unlock()
		return true, err
	default:
		return false, nil
	}
}

func (s *hostedChatBinaryServer) StopCleanly(t *testing.T) {
	t.Helper()
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	if exited, err := s.PollExit(); exited {
		if err != nil && !t.Failed() {
			t.Fatalf("hosted-chat exited before clean stop: %v\n%s", err, s.LogsForFailure())
		}
		return
	}

	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		if !t.Failed() {
			t.Fatalf("signal hosted-chat for clean stop: %v\n%s", err, s.LogsForFailure())
		}
		return
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-s.waitCh:
		s.mu.Lock()
		s.exited = true
		s.waitErr = err
		s.mu.Unlock()
		if err != nil && !t.Failed() {
			t.Fatalf("hosted-chat clean stop failed: %v\n%s", err, s.LogsForFailure())
		}
	case <-timer.C:
		_ = s.cmd.Process.Kill()
		err := <-s.waitCh
		s.mu.Lock()
		s.exited = true
		s.waitErr = err
		s.mu.Unlock()
		if !t.Failed() {
			t.Fatalf("hosted-chat did not stop cleanly within timeout\n%s", s.LogsForFailure())
		}
	}
}

func (s *hostedChatBinaryServer) KillUncleanly(t *testing.T) {
	t.Helper()
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	if exited, err := s.PollExit(); exited {
		if !t.Failed() {
			t.Fatalf("hosted-chat exited before unclean kill: %v\n%s", err, s.LogsForFailure())
		}
		return
	}

	if err := s.cmd.Process.Kill(); err != nil {
		if !t.Failed() {
			t.Fatalf("kill hosted-chat for unclean stop: %v\n%s", err, s.LogsForFailure())
		}
		return
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-s.waitCh:
		s.mu.Lock()
		s.exited = true
		s.waitErr = err
		s.mu.Unlock()
		if err == nil && !t.Failed() {
			t.Fatalf("hosted-chat unclean kill exited cleanly, want process kill\n%s", s.LogsForFailure())
		}
	case <-timer.C:
		if !t.Failed() {
			t.Fatalf("hosted-chat did not exit after unclean kill\n%s", s.LogsForFailure())
		}
	}
}

func waitForHostedChatProtocolReady(t *testing.T, server *hostedChatBinaryServer, token string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if exited, err := server.PollExit(); exited {
			t.Fatalf("hosted-chat exited before readiness: %v\n%s", err, server.LogsForFailure())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, response, err := protocolclient.DialAndExecuteDeclaredQuery(ctx, hostedChatProtocolOptions(server, token), protocolclient.DeclaredQueryRequest{
			Name: "recent_messages",
		})
		cancel()
		if err == nil && response.Error == nil {
			return
		}
		if err != nil {
			lastErr = err
		} else if response.Error != nil {
			lastErr = errors.New(*response.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("hosted-chat did not become protocol-ready: %v\n%s", lastErr, server.LogsForFailure())
}

func waitForHostedChatHealthReady(t *testing.T, server *hostedChatBinaryServer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if exited, err := server.PollExit(); exited {
			t.Fatalf("hosted-chat exited before health readiness: %v\n%s", err, server.LogsForFailure())
		}
		inspection, err := fetchHostedChatHealthInspection(server)
		if err == nil &&
			inspection.Status == shunter.HealthStatusOK &&
			inspection.Runtime.Ready &&
			inspection.Runtime.Protocol.Ready &&
			inspection.Runtime.Durability.Started {
			return
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf(
				"status=%s ready=%v protocol_ready=%v durability_started=%v",
				inspection.Status,
				inspection.Runtime.Ready,
				inspection.Runtime.Protocol.Ready,
				inspection.Runtime.Durability.Started,
			)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("hosted-chat did not become health-ready: %v\n%s", lastErr, server.LogsForFailure())
}

func hostedChatDurableTxID(t *testing.T, server *hostedChatBinaryServer) types.TxID {
	t.Helper()
	inspection, err := fetchHostedChatHealthInspection(server)
	if err != nil {
		t.Fatalf("get hosted-chat health: %v\n%s", err, server.LogsForFailure())
	}
	return inspection.Runtime.Durability.DurableTxID
}

func fetchHostedChatHealthInspection(server *hostedChatBinaryServer) (shunter.RuntimeHealthInspection, error) {
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + server.addr + "/healthz")
	if err != nil {
		return shunter.RuntimeHealthInspection{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return shunter.RuntimeHealthInspection{}, fmt.Errorf("hosted-chat health status = %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var inspection shunter.RuntimeHealthInspection
	if err := json.NewDecoder(resp.Body).Decode(&inspection); err != nil {
		return shunter.RuntimeHealthInspection{}, err
	}
	return inspection, nil
}

func waitForHostedChatDurableTxID(t *testing.T, server *hostedChatBinaryServer, txID types.TxID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last types.TxID
	for time.Now().Before(deadline) {
		if exited, err := server.PollExit(); exited {
			t.Fatalf("hosted-chat exited before durability reached tx %d: %v\n%s", txID, err, server.LogsForFailure())
		}
		last = hostedChatDurableTxID(t, server)
		if last >= txID {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("hosted-chat durable tx = %d, want at least %d\n%s", last, txID, server.LogsForFailure())
}

func hostedChatProtocolOptions(server *hostedChatBinaryServer, token string) protocolclient.Options {
	return protocolclient.Options{
		URL:   server.WebSocketURL(),
		Token: token,
	}
}

func mintHostedChatGauntletToken(t *testing.T, signingKey []byte, issuer, audience string) string {
	t.Helper()
	token, _, err := auth.MintAnonymousToken(&auth.MintConfig{
		Issuer:     issuer,
		Audience:   audience,
		SigningKey: signingKey,
		Expiry:     time.Minute,
	})
	if err != nil {
		t.Fatalf("mint hosted-chat gauntlet token: %v", err)
	}
	return token
}

func encodeHostedChatSendMessageArgs(t *testing.T, author, body string) []byte {
	t.Helper()
	out, err := bsatn.AppendProductValueForSchema(nil, types.ProductValue{
		types.NewString(author),
		types.NewString(body),
	}, hostedChatSendMessageArgsSchema())
	if err != nil {
		t.Fatalf("encode send_message args: %v", err)
	}
	return out
}

func hostedChatSendMessageArgsSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "author", Type: types.KindString},
			{Index: 1, Name: "body", Type: types.KindString},
		},
	}
}

func decodeHostedChatMessages(t *testing.T, response protocol.OneOffQueryResponse, label string) []hostedChatMessage {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("%s error = %q, want success", label, *response.Error)
	}
	if len(response.Tables) != 1 {
		t.Fatalf("%s returned %d tables, want 1", label, len(response.Tables))
	}
	if response.Tables[0].TableName != "messages" {
		t.Fatalf("%s table = %q, want messages", label, response.Tables[0].TableName)
	}

	rawRows, err := protocol.DecodeRowList(response.Tables[0].Rows)
	if err != nil {
		t.Fatalf("%s DecodeRowList: %v", label, err)
	}
	rows := make([]hostedChatMessage, 0, len(rawRows))
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, hostedChatMessagesTableSchema())
		if err != nil {
			t.Fatalf("%s decode row %d: %v", label, i, err)
		}
		rows = append(rows, hostedChatMessage{
			ID:     row[0].AsUint64(),
			Author: row[1].AsString(),
			Body:   row[2].AsString(),
		})
	}
	return rows
}

func readHostedChatSubscribeApplied(t *testing.T, client *protocolclient.Client, requestID, queryID uint32, label string) []hostedChatMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, msg, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("%s read failed: %v", label, err)
	}
	if tag != protocol.TagSubscribeSingleApplied {
		t.Fatalf("%s tag = %d, want SubscribeSingleApplied", label, tag)
	}
	applied, ok := msg.(protocol.SubscribeSingleApplied)
	if !ok {
		t.Fatalf("%s message = %T, want SubscribeSingleApplied", label, msg)
	}
	if applied.RequestID != requestID || applied.QueryID != queryID {
		t.Fatalf("%s request/query = %d/%d, want %d/%d", label, applied.RequestID, applied.QueryID, requestID, queryID)
	}
	if applied.TableName != "messages" {
		t.Fatalf("%s table = %q, want messages", label, applied.TableName)
	}
	return decodeHostedChatRows(t, applied.Rows, label)
}

type hostedChatDelta struct {
	inserts []hostedChatMessage
	deletes []hostedChatMessage
}

func readHostedChatTransactionDelta(t *testing.T, client *protocolclient.Client, queryID uint32, label string) hostedChatDelta {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, msg, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("%s read failed: %v", label, err)
	}
	if tag != protocol.TagTransactionUpdateLight {
		t.Fatalf("%s tag = %d, want TransactionUpdateLight", label, tag)
	}
	update, ok := msg.(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("%s message = %T, want TransactionUpdateLight", label, msg)
	}
	return decodeHostedChatProtocolUpdates(t, update.Update, queryID, label)
}

func decodeHostedChatProtocolUpdates(t *testing.T, updates []protocol.SubscriptionUpdate, queryID uint32, label string) hostedChatDelta {
	t.Helper()
	var delta hostedChatDelta
	found := false
	for i, entry := range updates {
		if entry.QueryID != queryID {
			t.Fatalf("%s update %d query ID = %d, want %d", label, i, entry.QueryID, queryID)
		}
		if entry.TableName != "messages" {
			t.Fatalf("%s update %d table = %q, want messages", label, i, entry.TableName)
		}
		delta.inserts = append(delta.inserts, decodeHostedChatRows(t, entry.Inserts, label+" inserts")...)
		delta.deletes = append(delta.deletes, decodeHostedChatRows(t, entry.Deletes, label+" deletes")...)
		found = true
	}
	if !found {
		t.Fatalf("%s did not include subscription updates", label)
	}
	return delta
}

func decodeHostedChatRows(t *testing.T, encoded []byte, label string) []hostedChatMessage {
	t.Helper()
	rawRows, err := protocol.DecodeRowList(encoded)
	if err != nil {
		t.Fatalf("%s DecodeRowList: %v", label, err)
	}
	rows := make([]hostedChatMessage, 0, len(rawRows))
	for i, raw := range rawRows {
		row, err := bsatn.DecodeProductValueFromBytes(raw, hostedChatMessagesTableSchema())
		if err != nil {
			t.Fatalf("%s decode row %d: %v", label, i, err)
		}
		rows = append(rows, hostedChatMessage{
			ID:     row[0].AsUint64(),
			Author: row[1].AsString(),
			Body:   row[2].AsString(),
		})
	}
	return rows
}

type hostedChatMessage struct {
	ID     uint64
	Author string
	Body   string
}

func hostedChatMessagesTableSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.ColumnSchema{
			{Index: 0, Name: "id", Type: types.KindUint64},
			{Index: 1, Name: "author", Type: types.KindString},
			{Index: 2, Name: "body", Type: types.KindString},
		},
	}
}

func hostedChatMessagesContain(messages []hostedChatMessage, author, body string) bool {
	for _, message := range messages {
		if message.Author == author && message.Body == body {
			return true
		}
	}
	return false
}

func assertHostedChatLogsDoNotContainTokenMaterial(t *testing.T, logs string, values []string) {
	t.Helper()
	for _, value := range values {
		if value == "" {
			continue
		}
		if strings.Contains(logs, value) {
			t.Fatalf("hosted-chat logs contain token material")
		}
	}
}

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedLogBuffer) RedactedString(values []string) string {
	out := b.String()
	for _, value := range values {
		if value != "" {
			out = strings.ReplaceAll(out, value, "[redacted-token-material]")
		}
	}
	return out
}
