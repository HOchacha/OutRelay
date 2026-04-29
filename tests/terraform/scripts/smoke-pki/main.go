// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// Command smoke-pki bootstraps a fresh CA plus one relay leaf and
// three agent leaves (provider-eip, provider-nat, consumer) for the
// AWS smoke test. dev-pki only ships two agent leaves with hardcoded
// UUIDs, but the smoke topology needs three distinct agent identities
// so we issue them ourselves here. Layout matches dev-pki's so the
// existing identity / transport code finds the files unchanged.
package main

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/boanlab/OutRelay/lib/identity"
	"github.com/boanlab/OutRelay/pkg/pki"
)

const leafTTL = 30 * 24 * time.Hour

// Fixed UUIDs so cloud-init templates can hard-code the agent --uri
// flags. ...001/...002 match dev-pki for compatibility; ...003/...004
// are smoke-test extensions. aws-only uses 001/002/003; aws-gcp uses
// 002/003/004 (the EIP provider 001 is replaced by the GCP one).
// Each leaf is always emitted; unused ones are harmless extras.
const (
	providerEIPUUID = "00000000-0000-0000-0000-000000000001"
	consumerUUID    = "00000000-0000-0000-0000-000000000002"
	providerNATUUID = "00000000-0000-0000-0000-000000000003"
	providerGCPUUID = "00000000-0000-0000-0000-000000000004"
	consumerTCPUUID = "00000000-0000-0000-0000-000000000005"
)

func main() {
	out := flag.String("out", ".smoke-pki", "output directory")
	tenant := flag.String("tenant", "acme", "tenant label")
	relayIDs := flag.String("relay-ids", "r1", "comma-separated relay ids; one leaf cert per id is emitted as relay-<id>.{crt,key}")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o700); err != nil {
		log.Fatal(err)
	}

	ca, err := pki.NewCA()
	if err != nil {
		log.Fatal(err)
	}
	caKeyPEM, err := ca.KeyPEM()
	if err != nil {
		log.Fatal(err)
	}
	must(write(*out, "ca.crt", ca.CertPEM()))
	must(write(*out, "ca.key", caKeyPEM))

	type leafSpec struct {
		name   identity.Name
		prefix string
	}
	var leaves []leafSpec
	for _, id := range strings.Split(*relayIDs, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		relayName, err := identity.NewRelay(*tenant, id)
		if err != nil {
			log.Fatal(err)
		}
		leaves = append(leaves, leafSpec{relayName, "relay-" + id})
	}
	leaves = append(leaves,
		leafSpec{agentName(*tenant, providerEIPUUID), "agent-provider-eip"},
		leafSpec{agentName(*tenant, providerNATUUID), "agent-provider-nat"},
		leafSpec{agentName(*tenant, providerGCPUUID), "agent-provider-gcp"},
		leafSpec{agentName(*tenant, consumerUUID), "agent-consumer"},
		leafSpec{agentName(*tenant, consumerTCPUUID), "agent-consumer-tcp"},
	)

	for _, l := range leaves {
		certPEM, keyPEM, err := issueLeaf(ca, l.name)
		if err != nil {
			log.Fatalf("issue %s: %v", l.prefix, err)
		}
		must(write(*out, l.prefix+".crt", certPEM))
		must(write(*out, l.prefix+".key", keyPEM))
	}

	fmt.Fprintf(os.Stderr, "smoke-pki: wrote %d leaves to %s\n", len(leaves), *out)
}

func agentName(tenant, u string) identity.Name {
	return identity.Name{
		Role:    identity.RoleAgent,
		Tenant:  tenant,
		AgentID: uuid.MustParse(u),
	}
}

func issueLeaf(ca *pki.CA, name identity.Name) (certPEM, keyPEM []byte, err error) {
	csrDER, key, err := pki.NewCSR(name)
	if err != nil {
		return nil, nil, err
	}
	leafDER, err := ca.Sign(csrDER, name, leafTTL)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func write(dir, name string, b []byte) error {
	return os.WriteFile(filepath.Join(dir, name), b, 0o600)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
