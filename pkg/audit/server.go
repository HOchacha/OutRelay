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

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/lib/observe"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

type Server struct {
	pb.UnimplementedAuditServer
	store     *store.Store
	eventsCnt *observe.Counter
}

// New constructs an Audit server. If reg is non-nil the server bumps
// the `audit_events_total` counter on every Record.
func New(s *store.Store, reg *observe.Registry) *Server {
	srv := &Server{store: s}
	if reg != nil {
		srv.eventsCnt = reg.Counter("audit_events_total")
	}
	return srv
}

func (s *Server) Record(ctx context.Context, req *pb.RecordAuditRequest) (*pb.RecordAuditResponse, error) {
	if req.Event == nil {
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
		return nil, err
	}
	if s.eventsCnt != nil {
		s.eventsCnt.Inc()
	}
	return &pb.RecordAuditResponse{}, nil
}

func (s *Server) Query(req *pb.QueryAuditRequest, stream pb.Audit_QueryServer) error {
	rows, err := s.store.QueryAudit(stream.Context(), req.Tenant, req.Caller, req.Target, req.SinceUnixMs, int(req.Limit))
	if err != nil {
		return err
	}
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
