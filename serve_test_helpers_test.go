package shunter

import (
	"context"
	"net"
)

func (r *Runtime) serve(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.tryBeginServing() {
		_ = ln.Close()
		return ErrRuntimeServing
	}
	return r.serveStarted(ctx, ln)
}

func (h *Host) serve(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !h.tryBeginServing() {
		_ = ln.Close()
		return ErrHostServing
	}
	return h.serveStarted(ctx, ln)
}
