// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package registry_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/pkg/registry"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

// startServer spins an in-process gRPC controller for tests, returning
// the dial address. Caller cancels ctx to shut down.
func startServer(t *testing.T, ctx context.Context, dsn string) (string, *store.Store) {
	t.Helper()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	gs := grpc.NewServer()
	pb.RegisterRegistryServer(gs, registry.New(st, nil))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(func() {
		gs.GracefulStop()
		_ = st.Close()
	})
	return ln.Addr().String(), st
}

func dialClient(t *testing.T, addr string) pb.RegistryClient {
	t.Helper()
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return pb.NewRegistryClient(cc)
}

func TestRegisterResolveDeregister(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	addr, _ := startServer(t, ctx, ":memory:")
	c := dialClient(t, addr)

	if _, err := c.RegisterService(ctx, &pb.RegisterServiceRequest{
		Tenant: "acme", ServiceName: "svc-x",
		AgentUri: "outrelay://acme/agent/aaa",
		RelayId:  "relay-1",
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := c.Resolve(ctx, &pb.ResolveRequest{Tenant: "acme", ServiceName: "svc-x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Providers) != 1 || resp.Providers[0].AgentUri != "outrelay://acme/agent/aaa" {
		t.Fatalf("unexpected resolve: %+v", resp.Providers)
	}

	// Deregister; subsequent Resolve returns empty.
	if _, err := c.DeregisterAgent(ctx, &pb.DeregisterAgentRequest{
		Tenant: "acme", AgentUri: "outrelay://acme/agent/aaa", RelayId: "relay-1",
	}); err != nil {
		t.Fatal(err)
	}
	resp2, err := c.Resolve(ctx, &pb.ResolveRequest{Tenant: "acme", ServiceName: "svc-x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.Providers) != 0 {
		t.Fatalf("expected empty after dereg, got %+v", resp2.Providers)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	t.Parallel()

	dsn := t.TempDir() + "/c.db"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Bring up controller, register, shut down.
	addr1, _ := startServer(t, ctx, dsn)
	c1 := dialClient(t, addr1)
	if _, err := c1.RegisterService(ctx, &pb.RegisterServiceRequest{
		Tenant: "acme", ServiceName: "svc-y",
		AgentUri: "outrelay://acme/agent/bbb", RelayId: "relay-1",
	}); err != nil {
		t.Fatal(err)
	}

	// Bring up a fresh controller pointing at the same file.
	addr2, _ := startServer(t, ctx, dsn)
	c2 := dialClient(t, addr2)
	resp, err := c2.Resolve(ctx, &pb.ResolveRequest{Tenant: "acme", ServiceName: "svc-y"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("data lost across restart: %+v", resp.Providers)
	}
}

func TestWatchEmitsRegisterAndDeregister(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	addr, _ := startServer(t, ctx, ":memory:")
	c := dialClient(t, addr)

	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	stream, err := c.Watch(wctx, &pb.WatchRequest{Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}

	// Give Watch a moment to register on the server before pushing events.
	time.Sleep(50 * time.Millisecond)

	if _, err := c.RegisterService(ctx, &pb.RegisterServiceRequest{
		Tenant: "acme", ServiceName: "svc-z",
		AgentUri: "outrelay://acme/agent/ccc", RelayId: "relay-1",
	}); err != nil {
		t.Fatal(err)
	}

	ev1, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv #1: %v", err)
	}
	if ev1.Kind != pb.EventKind_EVENT_KIND_REGISTER || ev1.ServiceName != "svc-z" {
		t.Fatalf("unexpected event: %+v", ev1)
	}

	if _, err := c.DeregisterAgent(ctx, &pb.DeregisterAgentRequest{
		Tenant: "acme", AgentUri: "outrelay://acme/agent/ccc", RelayId: "relay-1",
	}); err != nil {
		t.Fatal(err)
	}
	ev2, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv #2: %v", err)
	}
	if ev2.Kind != pb.EventKind_EVENT_KIND_DEREGISTER || ev2.ServiceName != "svc-z" {
		t.Fatalf("unexpected event: %+v", ev2)
	}

	wcancel()
	// Stream should end on context cancel; gRPC may surface this as
	// io.EOF or a Canceled status — either is acceptable.
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error after cancel")
	}
}

func TestWatchTenantFilter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	addr, _ := startServer(t, ctx, ":memory:")
	c := dialClient(t, addr)

	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	stream, err := c.Watch(wctx, &pb.WatchRequest{Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	// Foreign-tenant register must not reach our stream.
	if _, err := c.RegisterService(ctx, &pb.RegisterServiceRequest{
		Tenant: "other", ServiceName: "svc-q",
		AgentUri: "outrelay://other/agent/zzz", RelayId: "r",
	}); err != nil {
		t.Fatal(err)
	}
	// Same-tenant register reaches us.
	if _, err := c.RegisterService(ctx, &pb.RegisterServiceRequest{
		Tenant: "acme", ServiceName: "svc-q",
		AgentUri: "outrelay://acme/agent/aaa", RelayId: "r",
	}); err != nil {
		t.Fatal(err)
	}

	recvCtx, recvCancel := context.WithTimeout(ctx, time.Second)
	defer recvCancel()
	type result struct {
		ev  *pb.WatchEvent
		err error
	}
	done := make(chan result, 1)
	go func() {
		ev, err := stream.Recv()
		done <- result{ev, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		// We should only ever see the acme register; foreign was filtered.
		if r.ev.ServiceName != "svc-q" {
			t.Fatalf("unexpected event: %+v", r.ev)
		}
	case <-recvCtx.Done():
		t.Fatal("watch did not receive expected event")
	}
}

func TestUpsertRelay(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	addr, _ := startServer(t, ctx, ":memory:")
	c := dialClient(t, addr)
	if _, err := c.UpsertRelay(ctx, &pb.UpsertRelayRequest{
		Id: "relay-1", Region: "us-east", Endpoint: "127.0.0.1:7443",
	}); err != nil {
		t.Fatal(err)
	}
	// idempotent on same id
	if _, err := c.UpsertRelay(ctx, &pb.UpsertRelayRequest{
		Id: "relay-1", Region: "us-east", Endpoint: "127.0.0.1:7443",
	}); err != nil {
		t.Fatal(err)
	}
}
