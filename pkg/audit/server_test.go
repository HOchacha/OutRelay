// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package audit_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/pkg/audit"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

func startSrv(t *testing.T) pb.AuditClient {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	pb.RegisterAuditServer(gs, audit.New(st, nil, nil))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gs.Serve(ln) }()
	t.Cleanup(func() { gs.GracefulStop(); _ = st.Close() })

	cc, err := grpc.NewClient(ln.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return pb.NewAuditClient(cc)
}

func TestAuditRecordAndQuery(t *testing.T) {
	t.Parallel()
	c := startSrv(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for i, dec := range []pb.Decision{
		pb.Decision_DECISION_ALLOW,
		pb.Decision_DECISION_DENY,
		pb.Decision_DECISION_ALLOW,
	} {
		_, err := c.Record(ctx, &pb.RecordAuditRequest{Event: &pb.AuditEvent{
			TsUnixMs: time.Now().UnixMilli() + int64(i),
			Tenant:   "acme",
			Caller:   "outrelay://acme/svc-billing",
			Target:   "svc-payments",
			Method:   "POST /charge",
			Decision: dec,
			Reason:   "rule-x",
		}})
		if err != nil {
			t.Fatal(err)
		}
	}

	stream, err := c.Query(ctx, &pb.QueryAuditRequest{Tenant: "acme", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var got []*pb.AuditEvent
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ev)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}

	// Filter to deny only.
	stream2, err := c.Query(ctx, &pb.QueryAuditRequest{Tenant: "acme", Limit: 10, Caller: "outrelay://acme/svc-billing"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		_, err := stream2.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("filter caller exact: want 3, got %d", count)
	}
}
