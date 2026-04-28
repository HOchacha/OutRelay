// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package pki

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/boanlab/OutRelay/lib/identity"
)

const (
	// DefaultEnrollTTL is the default lifetime of an enrollment token.
	DefaultEnrollTTL = 10 * time.Minute

	enrollIssuer   = "outrelay-controller"
	enrollAudience = "outrelay-agent"
)

var (
	ErrTokenReplay   = errors.New("pki: enrollment token already used")
	ErrTokenInvalid  = errors.New("pki: enrollment token invalid")
	ErrSigningMethod = errors.New("pki: unexpected jwt signing method")
)

// EnrollClaims is the JWT body the controller signs and the agent
// presents at first CSR. AgentID may be empty, meaning the agent picks
// its own UUID at enrollment time; if non-empty the controller's
// signed cert must match.
type EnrollClaims struct {
	Tenant  string `json:"ten"`
	AgentID string `json:"aid,omitempty"`
	jwt.RegisteredClaims
}

// Enroller issues and verifies one-shot enrollment tokens. Verification
// is single-use: a successfully verified token's jti is recorded and
// any future Verify of the same jti returns ErrTokenReplay.
type Enroller struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey

	mu   sync.Mutex
	used map[string]struct{}
}

// NewEnroller generates a fresh Ed25519 signing keypair.
func NewEnroller() (*Enroller, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: generate enroll key: %w", err)
	}
	return &Enroller{pub: pub, priv: priv, used: map[string]struct{}{}}, nil
}

// Issue mints a new enrollment token for tenant. If ttl <= 0,
// DefaultEnrollTTL is used.
func (e *Enroller) Issue(tenant string, ttl time.Duration) (string, error) {
	if !identity.IsValidTenant(tenant) {
		return "", identity.ErrInvalidTenant
	}
	if ttl <= 0 {
		ttl = DefaultEnrollTTL
	}
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := EnrollClaims{
		Tenant: tenant,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    enrollIssuer,
			Audience:  jwt.ClaimStrings{enrollAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        jti,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := tok.SignedString(e.priv)
	if err != nil {
		return "", fmt.Errorf("pki: sign token: %w", err)
	}
	return signed, nil
}

// Verify checks signature, expiry, audience, and prevents replay.
// On success, the token's jti is marked used and the claims returned.
func (e *Enroller) Verify(token string) (*EnrollClaims, error) {
	claims := &EnrollClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
			return nil, fmt.Errorf("%w: %s", ErrSigningMethod, t.Method.Alg())
		}
		return e.pub, nil
	},
		jwt.WithIssuer(enrollIssuer),
		jwt.WithAudience(enrollAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	if claims.ID == "" {
		return nil, fmt.Errorf("%w: missing jti", ErrTokenInvalid)
	}
	if !identity.IsValidTenant(claims.Tenant) {
		return nil, fmt.Errorf("%w: invalid tenant claim", ErrTokenInvalid)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, used := e.used[claims.ID]; used {
		return nil, ErrTokenReplay
	}
	e.used[claims.ID] = struct{}{}
	return claims, nil
}

func newJTI() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("pki: jti: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
