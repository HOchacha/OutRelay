// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package store is the controller's persistence layer. It owns the
// schema for agents, services, relay instances, policies, and audit
// events, and exposes typed Go methods over a single SQLite database.
//
// The driver is modernc.org/sqlite (pure Go) so the controller stays
// CGO-free and ships as a fully static distroless image.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned by lookups when no row matches.
var ErrNotFound = errors.New("store: not found")

// Schema is applied idempotently at Open. The five tables — agents,
// services, relay_instances, policies, audit_events — back every
// gRPC service the controller hosts.
const Schema = `
CREATE TABLE IF NOT EXISTS agents (
  uri               TEXT PRIMARY KEY,
  tenant            TEXT NOT NULL,
  last_seen_unix_ms INTEGER NOT NULL,
  metadata          TEXT
);
CREATE INDEX IF NOT EXISTS idx_agents_tenant ON agents(tenant);

CREATE TABLE IF NOT EXISTS services (
  id                 TEXT PRIMARY KEY,
  tenant             TEXT NOT NULL,
  name               TEXT NOT NULL,
  agent_uri          TEXT NOT NULL,
  relay_id           TEXT NOT NULL,
  local_addr         TEXT,
  health             TEXT NOT NULL DEFAULT 'healthy',
  updated_at_unix_ms INTEGER NOT NULL,
  UNIQUE(tenant, name)
);
CREATE INDEX IF NOT EXISTS idx_services_agent ON services(tenant, agent_uri);
CREATE INDEX IF NOT EXISTS idx_services_relay ON services(relay_id);

CREATE TABLE IF NOT EXISTS relay_instances (
  id                     TEXT PRIMARY KEY,
  region                 TEXT,
  endpoint               TEXT NOT NULL,
  last_heartbeat_unix_ms INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS policies (
  id                  TEXT PRIMARY KEY,
  tenant              TEXT NOT NULL,
  caller_pattern      TEXT NOT NULL,
  target_pattern      TEXT NOT NULL,
  method_pattern      TEXT NOT NULL DEFAULT '',
  decision            TEXT NOT NULL CHECK(decision IN ('allow','deny')),
  expires_unix_ms     INTEGER NOT NULL DEFAULT 0,
  created_at_unix_ms  INTEGER NOT NULL,
  p2p_mode            TEXT NOT NULL DEFAULT 'allowed' CHECK(p2p_mode IN ('allowed','forbidden','required')),
  relay_mode          TEXT NOT NULL DEFAULT 'splice' CHECK(relay_mode IN ('splice','forward'))
);
CREATE INDEX IF NOT EXISTS idx_policies_tenant ON policies(tenant);

CREATE TABLE IF NOT EXISTS audit_events (
  ts_unix_ms INTEGER NOT NULL,
  tenant     TEXT NOT NULL,
  caller     TEXT NOT NULL,
  target     TEXT NOT NULL,
  method     TEXT NOT NULL DEFAULT '',
  decision   TEXT NOT NULL,
  reason     TEXT NOT NULL DEFAULT '',
  stream_id  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_caller ON audit_events(tenant, caller, ts_unix_ms);
CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_events(tenant, target, ts_unix_ms);
`

// Service is a registered service row.
//
// RelayEndpoint is set by ResolveService via JOIN with relay_instances
// — empty when the relay holding this service hasn't called
// UpsertRelay (most likely a misconfiguration).
type Service struct {
	ID              string
	Tenant          string
	Name            string
	AgentURI        string
	RelayID         string
	RelayEndpoint   string
	LocalAddr       string
	Health          string
	UpdatedAtUnixMs int64
}

// Store wraps the SQLite database with the registry-shaped operations.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at dsn. Use ":memory:" for
// tests. The schema is applied idempotently.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// modernc.org/sqlite is single-writer. Set conservative pool sizes
	// so we don't spawn many connections fighting for the write lock.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(ctx, Schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// UpsertAgent records that agentURI is connected (or refreshes its
// last_seen). Tenant must be present so we keep agents partitioned.
func (s *Store) UpsertAgent(ctx context.Context, tenant, agentURI string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents(uri, tenant, last_seen_unix_ms)
		VALUES(?, ?, ?)
		ON CONFLICT(uri) DO UPDATE SET
		  tenant = excluded.tenant,
		  last_seen_unix_ms = excluded.last_seen_unix_ms`,
		agentURI, tenant, now)
	if err != nil {
		return fmt.Errorf("store: upsert agent: %w", err)
	}
	return nil
}

// RegisterService binds (tenant, name) -> (agent_uri, relay_id). Same
// (tenant, name) pair is updated in place so an agent can re-register
// idempotently after reconnect.
func (s *Store) RegisterService(ctx context.Context, svc Service) (string, error) {
	id := svc.ID
	if id == "" {
		id = svc.Tenant + "/" + svc.Name
	}
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO services(id, tenant, name, agent_uri, relay_id, local_addr, health, updated_at_unix_ms)
		VALUES(?, ?, ?, ?, ?, ?, 'healthy', ?)
		ON CONFLICT(tenant, name) DO UPDATE SET
		  agent_uri = excluded.agent_uri,
		  relay_id  = excluded.relay_id,
		  local_addr = excluded.local_addr,
		  health = 'healthy',
		  updated_at_unix_ms = excluded.updated_at_unix_ms`,
		id, svc.Tenant, svc.Name, svc.AgentURI, svc.RelayID, svc.LocalAddr, now)
	if err != nil {
		return "", fmt.Errorf("store: register service: %w", err)
	}
	return id, nil
}

// ResolveService returns the provider for (tenant, name) or
// ErrNotFound. The result includes the relay's advertised endpoint
// via a LEFT JOIN with relay_instances; an empty RelayEndpoint
// means the relay holding this service has not called UpsertRelay.
//
// callerRegion: when non-empty, providers whose relay is in the
// same region are preferred — multi-region deployments use this
// to avoid an unnecessary cross-region inter-relay hop. Empty
// disables the preference (single-relay back-compat).
func (s *Store) ResolveService(ctx context.Context, tenant, name, callerRegion string) (Service, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.tenant, s.name, s.agent_uri, s.relay_id,
		       COALESCE(s.local_addr, ''), s.health, s.updated_at_unix_ms,
		       COALESCE(r.endpoint, '')
		FROM services s
		LEFT JOIN relay_instances r ON r.id = s.relay_id
		WHERE s.tenant = ? AND s.name = ?
		ORDER BY
		  CASE WHEN ? != '' AND r.region = ? THEN 0 ELSE 1 END,
		  s.updated_at_unix_ms DESC
		LIMIT 1`,
		tenant, name, callerRegion, callerRegion)
	var svc Service
	err := row.Scan(&svc.ID, &svc.Tenant, &svc.Name, &svc.AgentURI, &svc.RelayID,
		&svc.LocalAddr, &svc.Health, &svc.UpdatedAtUnixMs, &svc.RelayEndpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return Service{}, ErrNotFound
	}
	if err != nil {
		return Service{}, fmt.Errorf("store: resolve: %w", err)
	}
	return svc, nil
}

// DeregisterAgent removes every service belonging to agentURI on
// relayID. Returns the names of services that were removed so the
// caller (registry server) can broadcast Watch DEREGISTER events.
func (s *Store) DeregisterAgent(ctx context.Context, tenant, agentURI, relayID string) ([]Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant, name, agent_uri, relay_id, COALESCE(local_addr, ''), health, updated_at_unix_ms
		FROM services
		WHERE tenant = ? AND agent_uri = ? AND relay_id = ?`,
		tenant, agentURI, relayID)
	if err != nil {
		return nil, fmt.Errorf("store: list before deregister: %w", err)
	}
	var removed []Service
	for rows.Next() {
		var svc Service
		if err := rows.Scan(&svc.ID, &svc.Tenant, &svc.Name, &svc.AgentURI, &svc.RelayID, &svc.LocalAddr, &svc.Health, &svc.UpdatedAtUnixMs); err != nil {
			_ = rows.Close()
			return nil, err
		}
		removed = append(removed, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM services
		WHERE tenant = ? AND agent_uri = ? AND relay_id = ?`,
		tenant, agentURI, relayID); err != nil {
		return nil, fmt.Errorf("store: delete services: %w", err)
	}
	return removed, nil
}

// UpsertRelay records (or refreshes the heartbeat of) a relay instance.
func (s *Store) UpsertRelay(ctx context.Context, id, region, endpoint string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO relay_instances(id, region, endpoint, last_heartbeat_unix_ms)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  region = excluded.region,
		  endpoint = excluded.endpoint,
		  last_heartbeat_unix_ms = excluded.last_heartbeat_unix_ms`,
		id, region, endpoint, now)
	if err != nil {
		return fmt.Errorf("store: upsert relay: %w", err)
	}
	return nil
}

// CountServices returns the row count of the services table (helper
// for tests and metrics).
func (s *Store) CountServices(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM services`).Scan(&n)
	return n, err
}

// Policy is one row of the policies table.
type Policy struct {
	ID              string
	Tenant          string
	CallerPattern   string
	TargetPattern   string
	MethodPattern   string
	Decision        string // "allow" | "deny"
	ExpiresUnixMs   int64  // 0 = no expiry
	CreatedAtUnixMs int64
	P2PMode         string // "allowed" | "forbidden" | "required" (default "allowed")
	RelayMode       string // "splice" | "forward" (default "splice")
}

// AddPolicy inserts a new policy. id may be empty; the caller usually
// generates a stable id (e.g. uuid) before calling.
func (s *Store) AddPolicy(ctx context.Context, p Policy) error {
	if p.CreatedAtUnixMs == 0 {
		p.CreatedAtUnixMs = time.Now().UnixMilli()
	}
	if p.P2PMode == "" {
		p.P2PMode = "allowed"
	}
	if p.RelayMode == "" {
		p.RelayMode = "splice"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO policies(id, tenant, caller_pattern, target_pattern, method_pattern, decision, expires_unix_ms, created_at_unix_ms, p2p_mode, relay_mode)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Tenant, p.CallerPattern, p.TargetPattern, p.MethodPattern, p.Decision, p.ExpiresUnixMs, p.CreatedAtUnixMs, p.P2PMode, p.RelayMode)
	if err != nil {
		return fmt.Errorf("store: add policy: %w", err)
	}
	return nil
}

// RemovePolicy deletes a policy by id (tenant must also match to avoid
// cross-tenant deletes). Returns whether a row was actually removed.
func (s *Store) RemovePolicy(ctx context.Context, tenant, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM policies WHERE tenant = ? AND id = ?`, tenant, id)
	if err != nil {
		return false, fmt.Errorf("store: remove policy: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListPolicies returns every policy in tenant. There is no
// pagination — tenants are expected to hold <10k policies, and
// relays consume the full list at startup via Watch.
func (s *Store) ListPolicies(ctx context.Context, tenant string) ([]Policy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant, caller_pattern, target_pattern, method_pattern, decision, expires_unix_ms, created_at_unix_ms, p2p_mode, relay_mode
		FROM policies
		WHERE tenant = ?
		ORDER BY created_at_unix_ms`,
		tenant)
	if err != nil {
		return nil, fmt.Errorf("store: list policies: %w", err)
	}
	defer rows.Close()
	var out []Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.ID, &p.Tenant, &p.CallerPattern, &p.TargetPattern, &p.MethodPattern, &p.Decision, &p.ExpiresUnixMs, &p.CreatedAtUnixMs, &p.P2PMode, &p.RelayMode); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AuditEvent is one row of the audit_events table.
type AuditEvent struct {
	TsUnixMs int64
	Tenant   string
	Caller   string
	Target   string
	Method   string
	Decision string
	Reason   string
	StreamID string
}

// RecordAudit appends one event.
func (s *Store) RecordAudit(ctx context.Context, ev AuditEvent) error {
	if ev.TsUnixMs == 0 {
		ev.TsUnixMs = time.Now().UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events(ts_unix_ms, tenant, caller, target, method, decision, reason, stream_id)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.TsUnixMs, ev.Tenant, ev.Caller, ev.Target, ev.Method, ev.Decision, ev.Reason, ev.StreamID)
	if err != nil {
		return fmt.Errorf("store: record audit: %w", err)
	}
	return nil
}

// QueryAudit returns events filtered by tenant and optional caller /
// target. sinceUnixMs is inclusive; 0 means "no lower bound". limit
// caps the result set; 0 means "no cap".
func (s *Store) QueryAudit(ctx context.Context, tenant, caller, target string, sinceUnixMs int64, limit int) ([]AuditEvent, error) {
	q := `SELECT ts_unix_ms, tenant, caller, target, method, decision, reason, stream_id FROM audit_events WHERE tenant = ?`
	args := []any{tenant}
	if caller != "" {
		q += ` AND caller = ?`
		args = append(args, caller)
	}
	if target != "" {
		q += ` AND target = ?`
		args = append(args, target)
	}
	if sinceUnixMs > 0 {
		q += ` AND ts_unix_ms >= ?`
		args = append(args, sinceUnixMs)
	}
	q += ` ORDER BY ts_unix_ms DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query audit: %w", err)
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		if err := rows.Scan(&ev.TsUnixMs, &ev.Tenant, &ev.Caller, &ev.Target, &ev.Method, &ev.Decision, &ev.Reason, &ev.StreamID); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
