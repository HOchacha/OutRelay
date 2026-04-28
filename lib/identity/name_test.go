// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package identity_test

import (
	"errors"
	"testing"

	"github.com/boanlab/OutRelay/lib/identity"
	"github.com/google/uuid"
)

func TestNameRoundtrip(t *testing.T) {
	t.Parallel()
	n, err := identity.NewAgent("acme")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := identity.Parse(n.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != n {
		t.Fatalf("got %+v, want %+v", parsed, n)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		uri  string
		want error
	}{
		{"wrong scheme", "https://acme/agent/" + uuid.New().String(), identity.ErrInvalidScheme},
		{"empty tenant", "outrelay:///agent/" + uuid.New().String(), identity.ErrInvalidTenant},
		{"uppercase tenant", "outrelay://Acme/agent/" + uuid.New().String(), identity.ErrInvalidTenant},
		{"missing agent segment", "outrelay://acme/" + uuid.New().String(), identity.ErrInvalidPath},
		{"missing uuid", "outrelay://acme/agent/", identity.ErrInvalidPath},
		{"non-uuid", "outrelay://acme/agent/not-a-uuid", identity.ErrInvalidUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := identity.Parse(tc.uri)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewAgentRejectsBadTenant(t *testing.T) {
	t.Parallel()
	for _, tenant := range []string{"", "-leading-hyphen", "UPPER", "with space", "way-too-long-" + string(make([]byte, 60))} {
		if _, err := identity.NewAgent(tenant); !errors.Is(err, identity.ErrInvalidTenant) {
			t.Errorf("tenant %q: got %v, want ErrInvalidTenant", tenant, err)
		}
	}
}
