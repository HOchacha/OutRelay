// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package policy implements the controller-side policy registry.
// A rule is a tuple (caller_pattern, target_pattern,
// method_pattern, decision, expires, p2p_mode); the relay's policy
// engine evaluates these against every OPEN_STREAM.
//
// Relays subscribe via Watch and keep the full snapshot in memory:
// the server first replays one ADDED event per existing rule plus a
// SNAPSHOT_END marker, then streams ADDED / REMOVED events live.
package policy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

type Server struct {
	pb.UnimplementedPolicyServer

	store  *store.Store
	logger *slog.Logger

	mu        sync.Mutex
	watchers  map[*watcher]struct{}
	nextWatch atomic.Uint64
}

func New(s *store.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{store: s, logger: logger, watchers: map[*watcher]struct{}{}}
}

func (s *Server) AddPolicy(ctx context.Context, req *pb.AddPolicyRequest) (*pb.AddPolicyResponse, error) {
	if req.Rule == nil {
		s.logger.Warn("policy: add rejected (rule nil)")
		return nil, fmt.Errorf("policy: rule required")
	}
	r := req.Rule
	if r.Tenant == "" || r.TargetPattern == "" || r.CallerPattern == "" {
		s.logger.Warn("policy: add rejected (missing field)",
			"tenant", r.Tenant, "caller", r.CallerPattern, "target", r.TargetPattern)
		return nil, fmt.Errorf("policy: tenant, caller_pattern, and target_pattern are required")
	}
	if r.Decision == pb.Decision_DECISION_UNSPECIFIED {
		s.logger.Warn("policy: add rejected (decision unspecified)",
			"tenant", r.Tenant, "caller", r.CallerPattern, "target", r.TargetPattern)
		return nil, fmt.Errorf("policy: decision required")
	}
	id := r.Id
	if id == "" {
		id = uuid.New().String()
	}
	if err := s.store.AddPolicy(ctx, store.Policy{
		ID:            id,
		Tenant:        r.Tenant,
		CallerPattern: r.CallerPattern,
		TargetPattern: r.TargetPattern,
		MethodPattern: r.MethodPattern,
		Decision:      decisionString(r.Decision),
		ExpiresUnixMs: r.ExpiresUnixMs,
		P2PMode:       p2pModeString(r.P2PMode),
		RelayMode:     relayModeString(r.RelayMode),
	}); err != nil {
		s.logger.Warn("policy: add store failed",
			"tenant", r.Tenant, "rule_id", id, "err", err)
		return nil, err
	}
	r.Id = id
	s.logger.Info("policy: rule added",
		"tenant", r.Tenant, "rule_id", id,
		"caller", r.CallerPattern, "target", r.TargetPattern,
		"method", r.MethodPattern,
		"decision", decisionString(r.Decision),
		"p2p_mode", p2pModeString(r.P2PMode),
		"relay_mode", relayModeString(r.RelayMode))
	s.broadcast(&pb.PolicyEvent{
		Kind: pb.PolicyEvent_POLICY_KIND_ADDED,
		Rule: r,
	}, r.Tenant)
	return &pb.AddPolicyResponse{Id: id}, nil
}

func (s *Server) RemovePolicy(ctx context.Context, req *pb.RemovePolicyRequest) (*pb.RemovePolicyResponse, error) {
	removed, err := s.store.RemovePolicy(ctx, req.Tenant, req.Id)
	if err != nil {
		s.logger.Warn("policy: remove store failed",
			"tenant", req.Tenant, "rule_id", req.Id, "err", err)
		return nil, err
	}
	if removed {
		s.logger.Info("policy: rule removed",
			"tenant", req.Tenant, "rule_id", req.Id)
		s.broadcast(&pb.PolicyEvent{
			Kind:      pb.PolicyEvent_POLICY_KIND_REMOVED,
			RemovedId: req.Id,
		}, req.Tenant)
	} else {
		s.logger.Debug("policy: remove no-op (not found)",
			"tenant", req.Tenant, "rule_id", req.Id)
	}
	return &pb.RemovePolicyResponse{Removed: removed}, nil
}

func (s *Server) ListPolicies(ctx context.Context, req *pb.ListPoliciesRequest) (*pb.ListPoliciesResponse, error) {
	rows, err := s.store.ListPolicies(ctx, req.Tenant)
	if err != nil {
		s.logger.Warn("policy: list store failed", "tenant", req.Tenant, "err", err)
		return nil, err
	}
	out := make([]*pb.PolicyRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPB(r))
	}
	s.logger.Debug("policy: list served", "tenant", req.Tenant, "rule_count", len(out))
	return &pb.ListPoliciesResponse{Rules: out}, nil
}

func (s *Server) Watch(req *pb.WatchPoliciesRequest, stream pb.Policy_WatchServer) error {
	id := s.nextWatch.Add(1)
	// 1. Drain a snapshot of every existing policy in the tenant.
	rows, err := s.store.ListPolicies(stream.Context(), req.Tenant)
	if err != nil {
		s.logger.Warn("policy: watch snapshot list failed",
			"watcher_id", id, "tenant", req.Tenant, "err", err)
		return err
	}

	w := &watcher{
		id:     id,
		tenant: req.Tenant,
		ch:     make(chan *pb.PolicyEvent, 64),
	}
	// Register before sending the snapshot so we don't lose any update
	// that lands during the snapshot send.
	s.addWatcher(w)
	s.logger.Info("policy: watch started",
		"watcher_id", id, "tenant", req.Tenant, "rule_count", len(rows))
	defer func() {
		s.removeWatcher(w)
		s.logger.Debug("policy: watch ended", "watcher_id", id, "tenant", req.Tenant)
	}()

	for _, r := range rows {
		if err := stream.Send(&pb.PolicyEvent{
			Kind: pb.PolicyEvent_POLICY_KIND_ADDED,
			Rule: toPB(r),
		}); err != nil {
			s.logger.Warn("policy: snapshot send failed",
				"watcher_id", id, "tenant", req.Tenant, "rule_id", r.ID, "err", err)
			return err
		}
	}
	if err := stream.Send(&pb.PolicyEvent{
		Kind: pb.PolicyEvent_POLICY_KIND_SNAPSHOT_END,
	}); err != nil {
		s.logger.Warn("policy: snapshot_end send failed",
			"watcher_id", id, "tenant", req.Tenant, "err", err)
		return err
	}
	s.logger.Debug("policy: snapshot delivered",
		"watcher_id", id, "tenant", req.Tenant, "rule_count", len(rows))

	// 2. Stream live events.
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-w.ch:
			if !ok {
				s.logger.Warn("policy: watcher dropped (slow consumer)",
					"watcher_id", id, "tenant", req.Tenant)
				return fmt.Errorf("policy: watcher dropped (slow consumer)")
			}
			if err := stream.Send(ev); err != nil {
				s.logger.Warn("policy: live send failed",
					"watcher_id", id, "tenant", req.Tenant, "err", err)
				return err
			}
			s.logger.Debug("policy: live event sent",
				"watcher_id", id, "tenant", req.Tenant, "kind", ev.Kind.String())
		}
	}
}

type watcher struct {
	id     uint64
	tenant string
	ch     chan *pb.PolicyEvent
}

func (s *Server) addWatcher(w *watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watchers[w] = struct{}{}
}

func (s *Server) removeWatcher(w *watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.watchers[w]; ok {
		delete(s.watchers, w)
		close(w.ch)
	}
}

func (s *Server) broadcast(ev *pb.PolicyEvent, tenant string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for w := range s.watchers {
		if w.tenant != "" && w.tenant != tenant {
			continue
		}
		select {
		case w.ch <- ev:
		default:
			s.logger.Warn("policy: dropping slow watcher",
				"watcher_id", w.id, "tenant", w.tenant, "event", ev.Kind.String())
			delete(s.watchers, w)
			close(w.ch)
		}
	}
}

func decisionString(d pb.Decision) string {
	if d == pb.Decision_DECISION_DENY {
		return "deny"
	}
	return "allow"
}

func decisionPB(s string) pb.Decision {
	if s == "deny" {
		return pb.Decision_DECISION_DENY
	}
	return pb.Decision_DECISION_ALLOW
}

func toPB(r store.Policy) *pb.PolicyRule {
	return &pb.PolicyRule{
		Id:              r.ID,
		Tenant:          r.Tenant,
		CallerPattern:   r.CallerPattern,
		TargetPattern:   r.TargetPattern,
		MethodPattern:   r.MethodPattern,
		Decision:        decisionPB(r.Decision),
		ExpiresUnixMs:   r.ExpiresUnixMs,
		CreatedAtUnixMs: r.CreatedAtUnixMs,
		P2PMode:         p2pModePB(r.P2PMode),
		RelayMode:       relayModePB(r.RelayMode),
	}
}

func p2pModeString(m pb.P2PMode) string {
	switch m {
	case pb.P2PMode_P2P_FORBIDDEN:
		return "forbidden"
	case pb.P2PMode_P2P_REQUIRED:
		return "required"
	default:
		return "allowed"
	}
}

func p2pModePB(s string) pb.P2PMode {
	switch s {
	case "forbidden":
		return pb.P2PMode_P2P_FORBIDDEN
	case "required":
		return pb.P2PMode_P2P_REQUIRED
	default:
		return pb.P2PMode_P2P_ALLOWED
	}
}

func relayModeString(m pb.RelayMode) string {
	if m == pb.RelayMode_RELAY_MODE_FORWARD {
		return "forward"
	}
	return "splice"
}

func relayModePB(s string) pb.RelayMode {
	if s == "forward" {
		return pb.RelayMode_RELAY_MODE_FORWARD
	}
	return pb.RelayMode_RELAY_MODE_SPLICE
}
