// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package observe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// DebugMux returns an http.Handler that serves:
// - /debug/metrics: JSON snapshot of the registry
// - /debug/pprof/*: standard net/http/pprof handlers
//
// Mount on a localhost-only listener so it's not reachable from the
// outside.
func DebugMux(reg *Registry) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.Snapshot())
	})

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return mux
}

// ServeDebug binds to addr and serves DebugMux(reg) until ctx cancels.
// Blocks until the server stops.
func ServeDebug(ctx context.Context, addr string, reg *Registry) error {
	if addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           DebugMux(reg),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Background in the inner shutdown context is deliberate: by the
	// time we reach <-ctx.Done(), ctx is already cancelled, so reusing
	// it would give Shutdown an already-expired deadline. We want a
	// fresh 2 s grace window instead.
	go func() { // #nosec G118
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
