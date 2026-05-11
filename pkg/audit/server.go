// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package audit ingests stream-open decisions from relays and serves
// history queries to operators. The relay fires one Record per
// OPEN_STREAM evaluation; events persist in the same SQLite database
// that the registry and policy services share.
package audit

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/lib/observe"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

type Server struct {
	pb.UnimplementedAuditServer
	store     *store.Store
	logger    *slog.Logger
	eventsCnt *observe.Counter
}

// New constructs an Audit server. If reg is non-nil the server bumps
// the `audit_events_total` counter on every Record. A nil logger
// disables logging.
func New(s *store.Store, reg *observe.Registry, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	srv := &Server{store: s, logger: logger}
	if reg != nil {
		srv.eventsCnt = reg.Counter("audit_events_total")
	}
	return srv
}

func (s *Server) Record(ctx context.Context, req *pb.RecordAuditRequest) (*pb.RecordAuditResponse, error) {
	if req.Event == nil {
		s.logger.Warn("audit: record rejected (event nil)")
		return nil, fmt.Errorf("audit: event required")
	}
	ev := req.Event
	if err := s.store.RecordAudit(ctx, store.AuditEvent{
		TsUnixMs: ev.TsUnixMs,
		Tenant:   ev.Tenant,
		Caller:   ev.Caller,
		Target:   ev.Target,
		Method:   ev.Method,
		Decision: decisionString(ev.Decision),
		Reason:   ev.Reason,
		StreamID: ev.StreamId,
	}); err != nil {
		s.logger.Warn("audit: record store failed",
			"tenant", ev.Tenant, "caller", ev.Caller, "target", ev.Target,
			"stream_id", ev.StreamId, "err", err)
		return nil, err
	}
	if s.eventsCnt != nil {
		s.eventsCnt.Inc()
	}
	s.logger.Debug("audit: event recorded",
		"tenant", ev.Tenant, "caller", ev.Caller, "target", ev.Target,
		"method", ev.Method, "decision", decisionString(ev.Decision),
		"stream_id", ev.StreamId)
	return &pb.RecordAuditResponse{}, nil
}

func (s *Server) Query(req *pb.QueryAuditRequest, stream pb.Audit_QueryServer) error {
	rows, err := s.store.QueryAudit(stream.Context(), req.Tenant, req.Caller, req.Target, req.SinceUnixMs, int(req.Limit))
	if err != nil {
		s.logger.Warn("audit: query failed",
			"tenant", req.Tenant, "caller", req.Caller, "target", req.Target, "err", err)
		return err
	}
	s.logger.Debug("audit: query",
		"tenant", req.Tenant, "caller", req.Caller, "target", req.Target,
		"since_ms", req.SinceUnixMs, "limit", req.Limit, "rows", len(rows))
	for _, ev := range rows {
		if err := stream.Send(&pb.AuditEvent{
			TsUnixMs: ev.TsUnixMs,
			Tenant:   ev.Tenant,
			Caller:   ev.Caller,
			Target:   ev.Target,
			Method:   ev.Method,
			Decision: decisionPB(ev.Decision),
			Reason:   ev.Reason,
			StreamId: ev.StreamID,
		}); err != nil {
			s.logger.Warn("audit: query send failed",
				"tenant", req.Tenant, "err", err)
			return err
		}
	}
	return nil
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
