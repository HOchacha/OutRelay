// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Package identity defines the OutRelay agent identity URI and the
// client-side cert rotator. The controller's PKI (CA, enrollment) lives
// in pkg/pki and depends on this package.
//
// URI form: outrelay://<tenant>/agent/<uuid>
//
// The URI is encoded as a URI SAN in agent X.509 certs and is the
// caller identifier the relay's policy engine evaluates against.
package identity

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Scheme is the URI scheme for OutRelay identities.
const Scheme = "outrelay"

// Role distinguishes agent identities from inter-relay peer identities.
// Both share the same URI scheme so a single mTLS handshake can carry
// either; the role is encoded in the second URI path segment.
type Role int

const (
	RoleUnknown Role = iota
	RoleAgent        // outrelay://<tenant>/agent/<uuid>
	RoleRelay        // outrelay://<region>/relay/<id>
)

func (r Role) String() string {
	switch r {
	case RoleAgent:
		return "agent"
	case RoleRelay:
		return "relay"
	default:
		return "unknown"
	}
}

var (
	ErrInvalidScheme  = errors.New("identity: not an outrelay URI")
	ErrInvalidTenant  = errors.New("identity: invalid tenant")
	ErrInvalidPath    = errors.New("identity: invalid path component")
	ErrInvalidUUID    = errors.New("identity: invalid agent uuid")
	ErrInvalidRelayID = errors.New("identity: invalid relay id")
)

// tenantPattern: lowercase alphanumeric and hyphens, 1–63 chars,
// must start with alphanumeric. Mirrors DNS label rules so tenants
// can map to subdomains if ever needed.
var tenantPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// IsValidTenant reports whether s is a syntactically valid tenant.
func IsValidTenant(s string) bool { return tenantPattern.MatchString(s) }

// relayIDPattern: same DNS-label rules as tenant — keeps the URI
// structure regular and allows relay IDs to map to subdomains.
var relayIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// IsValidRelayID reports whether s is a syntactically valid relay id.
func IsValidRelayID(s string) bool { return relayIDPattern.MatchString(s) }

// Name is a parsed OutRelay identity (agent or relay).
//
// For RoleAgent: Tenant + AgentID are populated.
// For RoleRelay: Tenant + RelayID are populated.
type Name struct {
	Role    Role
	Tenant  string
	AgentID uuid.UUID
	RelayID string
}

// String returns the URI form.
func (n Name) String() string {
	switch n.Role {
	case RoleAgent:
		return fmt.Sprintf("%s://%s/agent/%s", Scheme, n.Tenant, n.AgentID.String())
	case RoleRelay:
		return fmt.Sprintf("%s://%s/relay/%s", Scheme, n.Tenant, n.RelayID)
	default:
		return ""
	}
}

// URL returns the URI as *url.URL for use as a URI SAN.
func (n Name) URL() *url.URL {
	u, _ := url.Parse(n.String())
	return u
}

// NewAgent creates an agent Name with a fresh random UUID.
func NewAgent(tenant string) (Name, error) {
	if !IsValidTenant(tenant) {
		return Name{}, ErrInvalidTenant
	}
	return Name{Role: RoleAgent, Tenant: tenant, AgentID: uuid.New()}, nil
}

// NewRelay creates a relay Name. region (Tenant slot) and id are both
// validated against the DNS-label regex.
func NewRelay(region, id string) (Name, error) {
	if !IsValidTenant(region) {
		return Name{}, ErrInvalidTenant
	}
	if !IsValidRelayID(id) {
		return Name{}, ErrInvalidRelayID
	}
	return Name{Role: RoleRelay, Tenant: region, RelayID: id}, nil
}

// Parse decodes a URI of either role.
func Parse(s string) (Name, error) {
	u, err := url.Parse(s)
	if err != nil {
		return Name{}, fmt.Errorf("identity: parse: %w", err)
	}
	return parseURL(u)
}

// FromURL is a convenience for parsing a URI SAN already as *url.URL.
func FromURL(u *url.URL) (Name, error) { return parseURL(u) }

func parseURL(u *url.URL) (Name, error) {
	if u.Scheme != Scheme {
		return Name{}, ErrInvalidScheme
	}
	if !IsValidTenant(u.Host) {
		return Name{}, ErrInvalidTenant
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 || parts[1] == "" {
		return Name{}, ErrInvalidPath
	}
	switch parts[0] {
	case "agent":
		id, err := uuid.Parse(parts[1])
		if err != nil {
			return Name{}, fmt.Errorf("%w: %v", ErrInvalidUUID, err)
		}
		return Name{Role: RoleAgent, Tenant: u.Host, AgentID: id}, nil
	case "relay":
		if !IsValidRelayID(parts[1]) {
			return Name{}, ErrInvalidRelayID
		}
		return Name{Role: RoleRelay, Tenant: u.Host, RelayID: parts[1]}, nil
	default:
		return Name{}, ErrInvalidPath
	}
}
