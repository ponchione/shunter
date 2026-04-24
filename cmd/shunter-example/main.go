// Command shunter-example is the normal hosted-runtime hello-world example.
//
// It defines a tiny Shunter module through the top-level shunter API, builds a
// runtime, and serves the runtime WebSocket surface at /subscribe. Lower-level
// kernel packages remain available for advanced/internal work, but normal app
// code should not assemble the schema/commitlog/executor/subscription/protocol
// graph by hand.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/ponchione/shunter"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func main() {
	var (
		addr    = flag.String("addr", ":8080", "HTTP listen address")
		dataDir = flag.String("data", "./shunter-data", "Shunter data directory")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *dataDir); err != nil {
		log.Fatalf("shunter-example: %v", err)
	}
}

func run(ctx context.Context, addr, dataDir string) error {
	rt, err := buildHelloRuntime(dataDir, addr)
	if err != nil {
		return err
	}
	if err := rt.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func buildHelloRuntime(dataDir, addr string) (*shunter.Runtime, error) {
	return shunter.Build(newHelloModule(), shunter.Config{
		DataDir:        dataDir,
		ListenAddr:     addr,
		AuthMode:       shunter.AuthModeDev,
		EnableProtocol: true,
	})
}

func newHelloModule() *shunter.Module {
	return shunter.NewModule("hello").
		SchemaVersion(1).
		TableDef(schema.TableDefinition{
			Name: "greetings",
			Columns: []schema.ColumnDefinition{
				{Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
				{Name: "message", Type: types.KindString},
			},
		}).
		Reducer("say_hello", sayHello)
}

func sayHello(ctx *schema.ReducerContext, args []byte) ([]byte, error) {
	msg := string(args)
	if msg == "" {
		msg = "hello, world"
	}
	const greetingsTableID uint32 = 0
	_, err := ctx.DB.Insert(greetingsTableID, types.ProductValue{
		types.NewUint64(0),
		types.NewString(msg),
	})
	return nil, err
}
