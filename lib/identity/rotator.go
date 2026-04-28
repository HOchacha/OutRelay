// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package identity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultRotateAt is the fraction of cert TTL at which to renew.
// At TTL=1h and DefaultRotateAt=0.7 the rotator refreshes after 42m,
// leaving 18m headroom against the controller being briefly unreachable.
const DefaultRotateAt = 0.7

// Cert is the rotator's atomic snapshot of an active identity.
type Cert struct {
	TLS  *tls.Certificate
	X509 *x509.Certificate
}

// Signer fetches a fresh cert. Implementations typically build a CSR,
// authenticate to the controller (with an enrollment token on the
// first call, with the existing cert thereafter), and return the
// signed cert plus parsed metadata. Errors propagate so the rotator
// can apply backoff.
type Signer func(ctx context.Context) (*Cert, error)

// Rotator owns the agent's current identity and refreshes it before
// expiry. Goroutine-safe; readers obtain the current snapshot via
// Current() (lock-free atomic load).
type Rotator struct {
	signer   Signer
	rotateAt float64

	cur atomic.Pointer[Cert]

	mu   sync.Mutex
	subs []chan *Cert
}

// NewRotator returns a rotator that uses signer for both bootstrap
// and refresh. The rotator does no work until Bootstrap and Run are
// called.
func NewRotator(signer Signer) *Rotator {
	return &Rotator{signer: signer, rotateAt: DefaultRotateAt}
}

// SetRotateAt changes the renew threshold (must be in (0, 1)).
// Useful for tests that want fast-forward refresh.
func (r *Rotator) SetRotateAt(f float64) error {
	if f <= 0 || f >= 1 {
		return errors.New("identity: rotateAt must be in (0, 1)")
	}
	r.rotateAt = f
	return nil
}

// Bootstrap fetches the first cert. Must succeed before Run starts.
func (r *Rotator) Bootstrap(ctx context.Context) error {
	cert, err := r.signer(ctx)
	if err != nil {
		return fmt.Errorf("identity: bootstrap: %w", err)
	}
	r.cur.Store(cert)
	return nil
}

// Current returns the active cert, or nil if Bootstrap was not called.
func (r *Rotator) Current() *Cert { return r.cur.Load() }

// Run blocks until ctx cancels, refreshing cert as it approaches
// expiry. Refresh failures retry with a fixed 1-minute backoff.
func (r *Rotator) Run(ctx context.Context) error {
	cur := r.Current()
	if cur == nil {
		return errors.New("identity: rotator not bootstrapped")
	}
	for {
		until := r.timeUntilRefresh(cur.X509)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(until):
		}
		next, err := r.signer(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Minute):
				continue
			}
		}
		r.cur.Store(next)
		r.broadcast(next)
		cur = next
	}
}

// timeUntilRefresh returns the duration until the next refresh based
// on the cert's NotBefore/NotAfter and rotateAt fraction.
func (r *Rotator) timeUntilRefresh(c *x509.Certificate) time.Duration {
	total := c.NotAfter.Sub(c.NotBefore)
	deadline := c.NotBefore.Add(time.Duration(float64(total) * r.rotateAt))
	until := time.Until(deadline)
	if until < 0 {
		return 0
	}
	return until
}

// Subscribe returns a channel that receives a *Cert on every refresh.
// The channel is buffered (1) and dropped silently if the consumer is
// slow — consumers should always read the latest via Current().
func (r *Rotator) Subscribe() <-chan *Cert {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan *Cert, 1)
	r.subs = append(r.subs, ch)
	return ch
}

func (r *Rotator) broadcast(c *Cert) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.subs {
		select {
		case ch <- c:
		default:
		}
	}
}
