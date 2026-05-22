package shunter

import (
	"context"
	"errors"
)

// Run builds mod with cfg, starts the runtime-owned HTTP/protocol server, and
// shuts the runtime down when ctx is canceled. It is the hosted-app convenience
// path for applications that want Shunter to be their backend server.
func Run(ctx context.Context, mod *Module, cfg Config) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rt, err := Build(mod, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	err = rt.ListenAndServe(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
