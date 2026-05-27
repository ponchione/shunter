package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/protocol"
)

type serverInfo struct {
	URL string `json:"url"`
}

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fatalf("get working directory: %v", err)
	}

	protocolServer := &protocol.Server{
		JWT: &auth.JWTConfig{
			SigningKey: []byte("browser-integration-strict-auth-signing-key"),
			AuthMode:   auth.AuthModeStrict,
		},
		Options: protocol.DefaultProtocolOptions(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", protocolServer.HandleSubscribe)
	mux.HandleFunc("/client/index.js", serveFile(filepath.Join(repoRoot, "typescript", "client", "dist", "index.js"), "application/javascript; charset=utf-8"))
	mux.HandleFunc("/client/index.js.map", serveFile(filepath.Join(repoRoot, "typescript", "client", "dist", "index.js.map"), "application/json; charset=utf-8"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>Shunter browser integration</title>")
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatalf("listen: %v", err)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	info, err := json.Marshal(serverInfo{URL: "http://" + listener.Addr().String() + "/"})
	if err != nil {
		fatalf("marshal server info: %v", err)
	}
	fmt.Println(string(info))

	waitForExit(server, serveErr)
}

func serveFile(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, path)
	}
}

func waitForExit(server *http.Server, serveErr <-chan error) {
	stdinClosed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stdinClosed)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatalf("serve: %v", err)
		}
		return
	case <-stdinClosed:
	case <-signals:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		fatalf("shutdown: %v", err)
	}

	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatalf("serve: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Printf("error: "+format+"\n", args...)
	os.Exit(1)
}
