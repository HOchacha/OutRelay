// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package pki_test

import (
	"errors"
	"testing"
	"time"

	"github.com/boanlab/OutRelay/pkg/pki"
)

func TestEnrollerIssueAndVerify(t *testing.T) {
	t.Parallel()
	e, err := pki.NewEnroller()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := e.Issue("acme", 0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tenant != "acme" {
		t.Fatalf("tenant=%q", c.Tenant)
	}
}

func TestEnrollerSingleUse(t *testing.T) {
	t.Parallel()
	e, _ := pki.NewEnroller()
	tok, _ := e.Issue("acme", 0)
	if _, err := e.Verify(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Verify(tok); !errors.Is(err, pki.ErrTokenReplay) {
		t.Fatalf("got %v, want ErrTokenReplay", err)
	}
}

func TestEnrollerRejectsExpired(t *testing.T) {
	t.Parallel()
	e, _ := pki.NewEnroller()
	tok, err := e.Issue("acme", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := e.Verify(tok); !errors.Is(err, pki.ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}

func TestEnrollerRejectsForeignKey(t *testing.T) {
	t.Parallel()
	a, _ := pki.NewEnroller()
	b, _ := pki.NewEnroller()
	tok, _ := a.Issue("acme", 0)
	// Token signed by a, verified with b's key. Must fail.
	if _, err := b.Verify(tok); !errors.Is(err, pki.ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}

func TestEnrollerRejectsBadTenant(t *testing.T) {
	t.Parallel()
	e, _ := pki.NewEnroller()
	if _, err := e.Issue("BadTenant", 0); err == nil {
		t.Fatal("expected invalid tenant error")
	}
}
