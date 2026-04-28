// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package policy_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/pkg/policy"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

func startCtrl(t *testing.T) pb.PolicyClient {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	pb.RegisterPolicyServer(gs, policy.New(st))

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
	return pb.NewPolicyClient(cc)
}

func TestPolicyAddListRemove(t *testing.T) {
	t.Parallel()
	c := startCtrl(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	r1, err := c.AddPolicy(ctx, &pb.AddPolicyRequest{Rule: &pb.PolicyRule{
		Tenant: "acme", CallerPattern: "outrelay://acme/svc-billing",
		TargetPattern: "svc-payments", Decision: pb.Decision_DECISION_ALLOW,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Id == "" {
		t.Fatal("missing id")
	}

	list, err := c.ListPolicies(ctx, &pb.ListPoliciesRequest{Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Rules) != 1 || list.Rules[0].Id != r1.Id {
		t.Fatalf("unexpected list: %+v", list.Rules)
	}

	rm, err := c.RemovePolicy(ctx, &pb.RemovePolicyRequest{Tenant: "acme", Id: r1.Id})
	if err != nil {
		t.Fatal(err)
	}
	if !rm.Removed {
		t.Fatal("not removed")
	}
	list2, _ := c.ListPolicies(ctx, &pb.ListPoliciesRequest{Tenant: "acme"})
	if len(list2.Rules) != 0 {
		t.Fatal("not empty after remove")
	}
}

func TestPolicyWatchSnapshotAndUpdates(t *testing.T) {
	t.Parallel()
	c := startCtrl(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Pre-populate one rule so the snapshot has content.
	first, _ := c.AddPolicy(ctx, &pb.AddPolicyRequest{Rule: &pb.PolicyRule{
		Tenant: "acme", CallerPattern: "*", TargetPattern: "svc-x",
		Decision: pb.Decision_DECISION_ALLOW,
	}})

	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	stream, err := c.Watch(wctx, &pb.WatchPoliciesRequest{Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}

	// Snapshot: one ADDED then SNAPSHOT_END.
	ev1, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev1.Kind != pb.PolicyEvent_POLICY_KIND_ADDED || ev1.Rule.Id != first.Id {
		t.Fatalf("expected snapshot ADDED for %s, got %+v", first.Id, ev1)
	}
	end, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if end.Kind != pb.PolicyEvent_POLICY_KIND_SNAPSHOT_END {
		t.Fatalf("expected SNAPSHOT_END, got %v", end.Kind)
	}

	// Live: add another rule, observe ADDED.
	second, _ := c.AddPolicy(ctx, &pb.AddPolicyRequest{Rule: &pb.PolicyRule{
		Tenant: "acme", CallerPattern: "*", TargetPattern: "svc-y",
		Decision: pb.Decision_DECISION_DENY,
	}})
	ev2, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev2.Kind != pb.PolicyEvent_POLICY_KIND_ADDED || ev2.Rule.Id != second.Id {
		t.Fatalf("expected live ADDED for %s, got %+v", second.Id, ev2)
	}

	// Live: remove and observe REMOVED.
	if _, err := c.RemovePolicy(ctx, &pb.RemovePolicyRequest{Tenant: "acme", Id: first.Id}); err != nil {
		t.Fatal(err)
	}
	ev3, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev3.Kind != pb.PolicyEvent_POLICY_KIND_REMOVED || ev3.RemovedId != first.Id {
		t.Fatalf("expected live REMOVED for %s, got %+v", first.Id, ev3)
	}
}
