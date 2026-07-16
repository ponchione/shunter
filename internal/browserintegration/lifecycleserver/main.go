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
	"strconv"
	"strings"
	"syscall"
	"time"

	shunter "github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const messagesTableID uint32 = 0

type serverInfo struct {
	URL  string `json:"url"`
	Addr string `json:"addr"`
}

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fatalf("get working directory: %v", err)
	}
	dataDir := os.Getenv("SHUNTER_BROWSER_LIFECYCLE_DATA_DIR")
	if dataDir == "" {
		fatalf("SHUNTER_BROWSER_LIFECYCLE_DATA_DIR is required")
	}
	addr := os.Getenv("SHUNTER_BROWSER_LIFECYCLE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	mod := shunter.NewModule("browser_lifecycle").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "messages",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true},
				{Name: "body", Type: types.KindString},
			},
		}, schema.WithPublicRead()).
		Reducer("insert_message", insertMessageReducer)
	rt, err := shunter.Build(mod, shunter.Config{
		DataDir:        dataDir,
		EnableProtocol: true,
		AuthMode:       shunter.AuthModeDev,
	})
	if err != nil {
		fatalf("build runtime: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		_ = rt.Close()
		fatalf("start runtime: %v", err)
	}

	if os.Getenv("SHUNTER_BROWSER_LIFECYCLE_SEED") == "1" {
		if err := callDurableInsert(rt, 1, "initial"); err != nil {
			_ = rt.Close()
			fatalf("seed initial row: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/subscribe", rt.HTTPHandler())
	mux.HandleFunc("POST /control/insert", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
		if err != nil || id == 0 {
			http.Error(w, "id must be a positive uint64", http.StatusBadRequest)
			return
		}
		body := r.URL.Query().Get("body")
		if err := callDurableInsert(rt, id, body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "id": id, "body": body})
	})
	mux.HandleFunc("/client/index.js", serveFile(filepath.Join(repoRoot, "typescript", "client", "dist", "index.js"), "application/javascript; charset=utf-8"))
	mux.HandleFunc("/client/index.js.map", serveFile(filepath.Join(repoRoot, "typescript", "client", "dist", "index.js.map"), "application/json; charset=utf-8"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>Shunter lifecycle browser integration</title>")
	})

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		_ = rt.Close()
		fatalf("listen %s: %v", addr, err)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	info, err := json.Marshal(serverInfo{
		URL:  "http://" + listener.Addr().String() + "/",
		Addr: listener.Addr().String(),
	})
	if err != nil {
		_ = server.Close()
		_ = rt.Close()
		fatalf("marshal server info: %v", err)
	}
	fmt.Println(string(info))

	waitForExit(server, rt, serveErr)
}

func insertMessageReducer(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	idText, body, ok := strings.Cut(string(args), ":")
	if !ok {
		return nil, errors.New("insert_message args must be id:body")
	}
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("insert_message id %q must be a positive uint64", idText)
	}
	_, err = ctx.DB.Insert(messagesTableID, types.ProductValue{
		types.NewUint64(id),
		types.NewString(body),
	})
	return nil, err
}

func callDurableInsert(rt *shunter.Runtime, id uint64, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := rt.CallReducer(ctx, "insert_message", []byte(fmt.Sprintf("%d:%s", id, body)))
	if err != nil {
		return err
	}
	if result.Status != shunter.StatusCommitted {
		return fmt.Errorf("insert_message status %d: %v", result.Status, result.Error)
	}
	return rt.WaitUntilDurable(ctx, result.TxID)
}

func serveFile(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, path)
	}
}

func waitForExit(server *http.Server, rt *shunter.Runtime, serveErr <-chan error) {
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
	shutdownErr := server.Shutdown(ctx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	runtimeErr := rt.Close()
	serveCloseErr := <-serveErr
	if errors.Is(serveCloseErr, http.ErrServerClosed) {
		serveCloseErr = nil
	}
	if err := errors.Join(shutdownErr, runtimeErr, serveCloseErr); err != nil {
		fatalf("shutdown: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Printf("error: "+format+"\n", args...)
	os.Exit(1)
}
