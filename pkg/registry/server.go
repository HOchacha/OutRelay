// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package registry implements the controller-side authoritative
// service registry. It persists state via pkg/registry/store and
// serves the gRPC API defined in api/control/v1/registry.proto.
package registry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	pb "github.com/boanlab/OutRelay/lib/control/v1"
	"github.com/boanlab/OutRelay/pkg/registry/store"
)

// Server implements pb.RegistryServer.
type Server struct {
	pb.UnimplementedRegistryServer

	store  *store.Store
	logger *slog.Logger

	mu        sync.Mutex
	watchers  map[*watcher]struct{}
	nextWatch atomic.Uint64
}

// New constructs a Server backed by the given store. A nil logger
// disables logging.
func New(s *store.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		store:    s,
		logger:   logger,
		watchers: map[*watcher]struct{}{},
	}
}

// RegisterService persists the binding and broadcasts a REGISTER event
// to active Watch streams for the matching tenant.
func (s *Server) RegisterService(ctx context.Context, req *pb.RegisterServiceRequest) (*pb.RegisterServiceResponse, error) {
	if req.Tenant == "" || req.ServiceName == "" || req.AgentUri == "" || req.RelayId == "" {
		s.logger.Warn("registry: register rejected (missing field)",
			"tenant", req.Tenant, "service", req.ServiceName,
			"agent", req.AgentUri, "relay", req.RelayId)
		return nil, fmt.Errorf("registry: missing required field")
	}
	if err := s.store.UpsertAgent(ctx, req.Tenant, req.AgentUri); err != nil {
		s.logger.Warn("registry: upsert agent failed",
			"tenant", req.Tenant, "agent", req.AgentUri, "err", err)
		return nil, err
	}
	id, err := s.store.RegisterService(ctx, store.Service{
		Tenant:    req.Tenant,
		Name:      req.ServiceName,
		AgentURI:  req.AgentUri,
		RelayID:   req.RelayId,
		LocalAddr: req.LocalAddr,
	})
	if err != nil {
		s.logger.Warn("registry: register service failed",
			"tenant", req.Tenant, "service", req.ServiceName,
			"agent", req.AgentUri, "relay", req.RelayId, "err", err)
		return nil, err
	}
	s.logger.Info("registry: service registered",
		"tenant", req.Tenant, "service", req.ServiceName,
		"agent", req.AgentUri, "relay", req.RelayId, "service_id", id)
	s.broadcast(&pb.WatchEvent{
		Kind:        pb.EventKind_EVENT_KIND_REGISTER,
		ServiceName: req.ServiceName,
		Provider: &pb.Provider{
			ServiceId: id,
			AgentUri:  req.AgentUri,
			RelayId:   req.RelayId,
			LocalAddr: req.LocalAddr,
		},
	}, req.Tenant)
	return &pb.RegisterServiceResponse{ServiceId: id}, nil
}

// DeregisterAgent removes all of agent's services and broadcasts
// DEREGISTER events.
func (s *Server) DeregisterAgent(ctx context.Context, req *pb.DeregisterAgentRequest) (*pb.DeregisterAgentResponse, error) {
	removed, err := s.store.DeregisterAgent(ctx, req.Tenant, req.AgentUri, req.RelayId)
	if err != nil {
		s.logger.Warn("registry: deregister agent failed",
			"tenant", req.Tenant, "agent", req.AgentUri, "relay", req.RelayId, "err", err)
		return nil, err
	}
	s.logger.Info("registry: agent deregistered",
		"tenant", req.Tenant, "agent", req.AgentUri, "relay", req.RelayId,
		"removed_count", len(removed))
	for _, svc := range removed {
		s.logger.Debug("registry: service removed by deregister",
			"tenant", req.Tenant, "service", svc.Name, "service_id", svc.ID)
		s.broadcast(&pb.WatchEvent{
			Kind:        pb.EventKind_EVENT_KIND_DEREGISTER,
			ServiceName: svc.Name,
			Provider: &pb.Provider{
				ServiceId: svc.ID,
				AgentUri:  svc.AgentURI,
				RelayId:   svc.RelayID,
			},
		}, req.Tenant)
	}
	return &pb.DeregisterAgentResponse{}, nil
}

// Resolve returns the provider for (tenant, service_name). When
// req.CallerRegion is non-empty the store prefers a provider whose
// relay sits in the same region — keeps cross-region traffic inside
// the inter-relay path it's already on rather than forcing the
// caller's relay to forward through a far-region relay.
func (s *Server) Resolve(ctx context.Context, req *pb.ResolveRequest) (*pb.ResolveResponse, error) {
	svc, err := s.store.ResolveService(ctx, req.Tenant, req.ServiceName, req.CallerRegion)
	if err != nil {
		// Translate ErrNotFound into an empty result; callers distinguish
		// "no provider" vs "infra failure" by err vs len(providers)==0.
		if err == store.ErrNotFound {
			return &pb.ResolveResponse{}, nil
		}
		return nil, err
	}
	return &pb.ResolveResponse{
		Providers: []*pb.Provider{{
			ServiceId:       svc.ID,
			AgentUri:        svc.AgentURI,
			RelayId:         svc.RelayID,
			RelayEndpoint:   svc.RelayEndpoint,
			LocalAddr:       svc.LocalAddr,
			UpdatedAtUnixMs: svc.UpdatedAtUnixMs,
		}},
	}, nil
}

// UpsertRelay records the relay's presence (or refreshes its heartbeat).
func (s *Server) UpsertRelay(ctx context.Context, req *pb.UpsertRelayRequest) (*pb.UpsertRelayResponse, error) {
	if req.Id == "" || req.Endpoint == "" {
		s.logger.Warn("registry: upsert relay rejected (missing field)",
			"relay_id", req.Id, "endpoint", req.Endpoint)
		return nil, fmt.Errorf("registry: relay id and endpoint required")
	}
	if err := s.store.UpsertRelay(ctx, req.Id, req.Region, req.Endpoint); err != nil {
		s.logger.Warn("registry: upsert relay failed",
			"relay_id", req.Id, "region", req.Region, "endpoint", req.Endpoint, "err", err)
		return nil, err
	}
	s.logger.Debug("registry: relay upserted",
		"relay_id", req.Id, "region", req.Region, "endpoint", req.Endpoint)
	return &pb.UpsertRelayResponse{}, nil
}

// Watch streams change events. The stream stays open until the client
// cancels (server-side stream close on ctx cancel). Unbuffered drops
// are not silent: if a watcher's queue fills up, the server closes
// the stream so the client knows to reconnect and re-list.
func (s *Server) Watch(req *pb.WatchRequest, stream pb.Registry_WatchServer) error {
	id := s.nextWatch.Add(1)
	w := &watcher{
		id:     id,
		tenant: req.Tenant,
		filter: nameSet(req.ServiceNames),
		ch:     make(chan *pb.WatchEvent, 64),
		ctx:    stream.Context(),
	}
	s.addWatcher(w)
	s.logger.Debug("registry: watcher added",
		"watcher_id", id, "tenant", req.Tenant, "filter_count", len(w.filter))
	defer func() {
		s.removeWatcher(w)
		s.logger.Debug("registry: watcher removed",
			"watcher_id", id, "tenant", req.Tenant)
	}()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-w.ch:
			if !ok {
				s.logger.Warn("registry: watcher dropped (slow consumer)",
					"watcher_id", id, "tenant", req.Tenant)
				return fmt.Errorf("registry: watcher dropped (slow consumer)")
			}
			if err := stream.Send(ev); err != nil {
				s.logger.Warn("registry: watch send failed",
					"watcher_id", id, "tenant", req.Tenant, "err", err)
				return err
			}
		}
	}
}

type watcher struct {
	id     uint64
	tenant string
	filter map[string]struct{} // empty == all
	ch     chan *pb.WatchEvent
	ctx    context.Context
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

func (s *Server) broadcast(ev *pb.WatchEvent, tenant string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for w := range s.watchers {
		if w.tenant != "" && w.tenant != tenant {
			continue
		}
		if len(w.filter) > 0 {
			if _, ok := w.filter[ev.ServiceName]; !ok {
				continue
			}
		}
		select {
		case w.ch <- ev:
		default:
			// Slow consumer: drop and close to signal the client to
			// reconnect-and-relist.
			s.logger.Warn("registry: dropping slow watcher",
				"watcher_id", w.id, "tenant", w.tenant, "event", ev.Kind.String())
			delete(s.watchers, w)
			close(w.ch)
		}
	}
}

func nameSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}
