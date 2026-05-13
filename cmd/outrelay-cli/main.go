// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// outrelay-cli is the operator CLI for the controller. It exposes
// `policy add/list/remove` and `audit query` over the controller's
// gRPC API. The dev manifests run it inline via `kubectl run` for
// bootstrap; production operators install it on a workstation.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
)

// Version is stamped at link time via -ldflags '-X main.Version=...'.
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "policy":
		policyMain(args)
	case "audit":
		auditMain(args)
	case "version", "--version", "-v":
		fmt.Println(Version)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `outrelay-cli — operator interface to outrelay-controller

  outrelay-cli policy add    --tenant T --caller P --target P [--method P] --decision allow|deny [--expires DURATION] [--p2p-mode allowed|forbidden|required] [--relay-mode splice|forward]
  outrelay-cli policy list   --tenant T
  outrelay-cli policy remove --tenant T --id ID
  outrelay-cli audit  query  --tenant T [--caller P] [--target P] [--since DUR] [--limit N]

Common flags:
  --controller HOST:PORT   default 127.0.0.1:7444
`)
}

func policyMain(args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		policyAdd(args[1:])
	case "list":
		policyList(args[1:])
	case "remove":
		policyRemove(args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func policyAdd(args []string) {
	fs := flag.NewFlagSet("policy add", flag.ExitOnError)
	addr := fs.String("controller", "127.0.0.1:7444", "controller address")
	tenant := fs.String("tenant", "", "tenant")
	caller := fs.String("caller", "", "caller pattern")
	target := fs.String("target", "", "target pattern")
	method := fs.String("method", "", "method pattern (optional)")
	decision := fs.String("decision", "", "allow | deny")
	expires := fs.Duration("expires", 0, "TTL from now (e.g. 24h); 0 = never")
	p2pMode := fs.String("p2p-mode", "", "allowed | forbidden | required (default: allowed)")
	relayMode := fs.String("relay-mode", "", "splice | forward (default: splice)")
	_ = fs.Parse(args)

	if *tenant == "" || *caller == "" || *target == "" || *decision == "" {
		fmt.Fprintln(os.Stderr, "policy add: missing required flag")
		os.Exit(2)
	}
	dec := pb.Decision_DECISION_ALLOW
	if strings.EqualFold(*decision, "deny") {
		dec = pb.Decision_DECISION_DENY
	}
	p2p, ok := parseP2PMode(*p2pMode)
	if !ok {
		fmt.Fprintln(os.Stderr, "policy add: invalid --p2p-mode (allowed|forbidden|required)")
		os.Exit(2)
	}
	rm, ok := parseRelayMode(*relayMode)
	if !ok {
		fmt.Fprintln(os.Stderr, "policy add: invalid --relay-mode (splice|forward)")
		os.Exit(2)
	}
	var expiresMs int64
	if *expires > 0 {
		expiresMs = time.Now().Add(*expires).UnixMilli()
	}

	cc := dial(*addr)
	defer cc.Close()
	client := pb.NewPolicyClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.AddPolicy(ctx, &pb.AddPolicyRequest{
		Rule: &pb.PolicyRule{
			Tenant:        *tenant,
			CallerPattern: *caller,
			TargetPattern: *target,
			MethodPattern: *method,
			Decision:      dec,
			ExpiresUnixMs: expiresMs,
			P2PMode:       p2p,
			RelayMode:     rm,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(resp.Id)
}

func policyList(args []string) {
	fs := flag.NewFlagSet("policy list", flag.ExitOnError)
	addr := fs.String("controller", "127.0.0.1:7444", "controller address")
	tenant := fs.String("tenant", "", "tenant")
	_ = fs.Parse(args)
	if *tenant == "" {
		fmt.Fprintln(os.Stderr, "policy list: --tenant required")
		os.Exit(2)
	}
	cc := dial(*addr)
	defer cc.Close()
	client := pb.NewPolicyClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.ListPolicies(ctx, &pb.ListPoliciesRequest{Tenant: *tenant})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	for _, r := range resp.Rules {
		decision := "allow"
		if r.Decision == pb.Decision_DECISION_DENY {
			decision = "deny"
		}
		expires := "-"
		if r.ExpiresUnixMs > 0 {
			expires = time.UnixMilli(r.ExpiresUnixMs).Format(time.RFC3339)
		}
		fmt.Printf("%s\t%s -> %s (%s)\tmethod=%q\tp2p=%s\trelay=%s\texpires=%s\n",
			r.Id, r.CallerPattern, r.TargetPattern, decision, r.MethodPattern, p2pModeString(r.P2PMode), relayModeString(r.RelayMode), expires)
	}
}

func policyRemove(args []string) {
	fs := flag.NewFlagSet("policy remove", flag.ExitOnError)
	addr := fs.String("controller", "127.0.0.1:7444", "controller address")
	tenant := fs.String("tenant", "", "tenant")
	id := fs.String("id", "", "policy id")
	_ = fs.Parse(args)
	if *tenant == "" || *id == "" {
		fmt.Fprintln(os.Stderr, "policy remove: --tenant and --id required")
		os.Exit(2)
	}
	cc := dial(*addr)
	defer cc.Close()
	client := pb.NewPolicyClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.RemovePolicy(ctx, &pb.RemovePolicyRequest{Tenant: *tenant, Id: *id})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if !resp.Removed {
		fmt.Fprintln(os.Stderr, "no such policy")
		os.Exit(1)
	}
	fmt.Println("removed")
}

func auditMain(args []string) {
	if len(args) == 0 || args[0] != "query" {
		usage()
		os.Exit(2)
	}
	fs := flag.NewFlagSet("audit query", flag.ExitOnError)
	addr := fs.String("controller", "127.0.0.1:7444", "controller address")
	tenant := fs.String("tenant", "", "tenant")
	caller := fs.String("caller", "", "filter by caller (exact)")
	target := fs.String("target", "", "filter by target (exact)")
	since := fs.Duration("since", 0, "filter to events newer than now-DUR")
	limit := fs.Int("limit", 100, "max events")
	_ = fs.Parse(args[1:])
	if *tenant == "" {
		fmt.Fprintln(os.Stderr, "audit query: --tenant required")
		os.Exit(2)
	}
	cc := dial(*addr)
	defer cc.Close()
	client := pb.NewAuditClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sinceMs int64
	if *since > 0 {
		sinceMs = time.Now().Add(-*since).UnixMilli()
	}
	cappedLimit := min(*limit, math.MaxInt32)
	stream, err := client.Query(ctx, &pb.QueryAuditRequest{
		Tenant: *tenant, Caller: *caller, Target: *target, SinceUnixMs: sinceMs, Limit: int32(cappedLimit), // #nosec G115 -- clamped above
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "stream:", err)
			os.Exit(1)
		}
		decision := "allow"
		if ev.Decision == pb.Decision_DECISION_DENY {
			decision = "deny"
		}
		fmt.Printf("%s\t%s\t%s -> %s %q\t%s\n",
			time.UnixMilli(ev.TsUnixMs).Format(time.RFC3339),
			decision, ev.Caller, ev.Target, ev.Method, ev.Reason)
	}
}

func dial(addr string) *grpc.ClientConn {
	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	return cc
}

func parseP2PMode(s string) (pb.P2PMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "allowed", "allow", "p2p_allowed":
		return pb.P2PMode_P2P_ALLOWED, true
	case "forbidden", "forbid", "deny", "p2p_forbidden":
		return pb.P2PMode_P2P_FORBIDDEN, true
	case "required", "require", "p2p_required":
		return pb.P2PMode_P2P_REQUIRED, true
	}
	return pb.P2PMode_P2P_ALLOWED, false
}

func p2pModeString(m pb.P2PMode) string {
	switch m {
	case pb.P2PMode_P2P_FORBIDDEN:
		return "forbidden"
	case pb.P2PMode_P2P_REQUIRED:
		return "required"
	}
	return "allowed"
}

func parseRelayMode(s string) (pb.RelayMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "splice", "relay_mode_splice":
		return pb.RelayMode_RELAY_MODE_SPLICE, true
	case "forward", "relay_mode_forward":
		return pb.RelayMode_RELAY_MODE_FORWARD, true
	}
	return pb.RelayMode_RELAY_MODE_SPLICE, false
}

func relayModeString(m pb.RelayMode) string {
	if m == pb.RelayMode_RELAY_MODE_FORWARD {
		return "forward"
	}
	return "splice"
}
