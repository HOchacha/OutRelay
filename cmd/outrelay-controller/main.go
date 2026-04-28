// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// outrelay-controller is the control-plane process. A single gRPC
// server hosts three services backed by one SQLite database:
//
//   - Registry: service registration, resolve, change-watch, relay
//     self-register.
//   - Policy:   add/remove/list rules plus a snapshot+live Watch stream
//     relays subscribe to.
//   - Audit:    record stream-open decisions and serve history queries.
//
// The PKI primitives in pkg/pki (CA, enrollment-token issuer) are
// linked in but not yet exposed over gRPC. For now, agents bootstrap
// from a token/cert pair provisioned on disk.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/lib/observe"
	"github.com/boanlab/OutRelay/pkg/audit"
	"github.com/boanlab/OutRelay/pkg/policy"
	"github.com/boanlab/OutRelay/pkg/registry"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

// Version is stamped at link time via -ldflags '-X main.Version=...'.
var Version = "dev"

func main() {
	var (
		listen      = flag.String("listen", "127.0.0.1:7444", "gRPC listen address")
		dsn         = flag.String("db", "outrelay-controller.db", "SQLite DSN (use ':memory:' for tests)")
		debugListen = flag.String("debug-listen", "127.0.0.1:9101", "localhost-only debug HTTP (/debug/metrics, /debug/pprof). Empty disables.")
		logFormat   = flag.String("log-format", "text", "log format: text or json")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		return
	}

	logger := newLogger(*logFormat)

	ctx, cancel := signalContext()
	defer cancel()

	st, err := store.Open(ctx, *dsn)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	obsReg := observe.NewRegistry()
	if *debugListen != "" {
		go func() {
			if err := observe.ServeDebug(ctx, *debugListen, obsReg); err != nil {
				logger.Warn("debug http", "err", err)
			}
		}()
	}

	srv := grpc.NewServer()
	pb.RegisterRegistryServer(srv, registry.New(st))
	pb.RegisterPolicyServer(srv, policy.New(st))
	pb.RegisterAuditServer(srv, audit.New(st, obsReg))

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}
	logger.Info("controller listening", "addr", ln.Addr(), "db", *dsn, "version", Version)

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()
	if err := srv.Serve(ln); err != nil {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
}

func newLogger(format string) *slog.Logger {
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		h = slog.NewTextHandler(os.Stderr, nil)
	}
	return slog.New(h)
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigC
		cancel()
	}()
	return ctx, cancel
}
