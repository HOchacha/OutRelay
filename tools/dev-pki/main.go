// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

// dev-pki bootstraps a fresh CA and a small fixed set of leaf certs
// (one relay + two agents) for local cluster validation.
//
// THIS IS A DEVELOPMENT-ONLY TOOL. The agent UUIDs are fixed so
// manifests can pre-bake `--uri` flags; the leaf TTL defaults to 30
// days (vs. production's 1 h with rotation). Do not deploy these
// secrets anywhere that matters.
//
// Run:
//
//	go run ./tools/dev-pki -out ./.dev-pki
//
// Output:
//
//	.dev-pki/
//	  ca.crt        — distribute (RootCAs / ClientCAs)
//	  ca.key        — keep local; needed if you re-issue
//	  relay-r1.{crt,key}
//	  agent-provider.{crt,key}
//	  agent-consumer.{crt,key}
//	  secrets.yaml  — kubectl apply -f .dev-pki/secrets.yaml
package main

import (
	"crypto/ecdsa"
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

const (
	devLeafTTL = 30 * 24 * time.Hour

	// Fixed UUIDs so that manifests can hard-code --uri values without
	// a separate template step. Provider gets ...001, consumer ...002.
	devProviderUUID = "00000000-0000-0000-0000-000000000001"
	devConsumerUUID = "00000000-0000-0000-0000-000000000002"
)

// Version is stamped at link time via -ldflags '-X main.Version=...'.
var Version = "dev"

func main() {
	out := flag.String("out", ".dev-pki", "output directory")
	tenant := flag.String("tenant", "acme", "tenant / region label")
	relayID := flag.String("relay-id", "r1", "relay id (used in URI: outrelay://<tenant>/relay/<id>)")
	namespace := flag.String("namespace", "outrelay", "K8s namespace for emitted Secret manifests")
	flag.Parse()

	// 0o700: dev-pki output contains private keys; restrict to the
	// running user.
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
	if err := write(*out, "ca.crt", ca.CertPEM()); err != nil {
		log.Fatal(err)
	}
	if err := write(*out, "ca.key", caKeyPEM); err != nil {
		log.Fatal(err)
	}

	relayName, err := identity.NewRelay(*tenant, *relayID)
	if err != nil {
		log.Fatal(err)
	}
	provName := identity.Name{Role: identity.RoleAgent, Tenant: *tenant, AgentID: uuid.MustParse(devProviderUUID)}
	consName := identity.Name{Role: identity.RoleAgent, Tenant: *tenant, AgentID: uuid.MustParse(devConsumerUUID)}

	type leaf struct {
		name       identity.Name
		filePrefix string
		secretName string
	}
	leaves := []leaf{
		{relayName, "relay-" + *relayID, "outrelay-relay-tls"},
		{provName, "agent-provider", "outrelay-agent-provider-tls"},
		{consName, "agent-consumer", "outrelay-agent-consumer-tls"},
	}

	type issued struct {
		leaf
		certPEM []byte
		keyPEM  []byte
	}
	out2 := make([]issued, 0, len(leaves))
	for _, l := range leaves {
		certPEM, keyPEM, err := issueLeaf(ca, l.name)
		if err != nil {
			log.Fatalf("issue %s: %v", l.name, err)
		}
		if err := write(*out, l.filePrefix+".crt", certPEM); err != nil {
			log.Fatal(err)
		}
		if err := write(*out, l.filePrefix+".key", keyPEM); err != nil {
			log.Fatal(err)
		}
		out2 = append(out2, issued{leaf: l, certPEM: certPEM, keyPEM: keyPEM})
	}

	// Emit a single multi-document YAML with a Secret per leaf. Each
	// Secret carries tls.crt / tls.key / ca.crt; manifests reference
	// them via subPath or volumeMounts.
	var b strings.Builder
	for i, x := range out2 {
		if i > 0 {
			b.WriteString("---\n")
		}
		writeSecret(&b, *namespace, x.secretName, x.name, x.certPEM, x.keyPEM, ca.CertPEM())
	}
	if err := write(*out, "secrets.yaml", []byte(b.String())); err != nil {
		log.Fatal(err)
	}

	fmt.Fprintln(os.Stderr, "dev-pki", Version, "→", *out)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "agent URIs (use as --uri in manifests):")
	fmt.Fprintln(os.Stderr, "  provider:", provName.String())
	fmt.Fprintln(os.Stderr, "  consumer:", consName.String())
	fmt.Fprintln(os.Stderr, "relay URI:", relayName.String())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "apply secrets with:")
	fmt.Fprintf(os.Stderr, "  kubectl apply -f %s/secrets.yaml\n", *out)
}

func issueLeaf(ca *pki.CA, name identity.Name) (certPEM, keyPEM []byte, err error) {
	csrDER, key, err := pki.NewCSR(name)
	if err != nil {
		return nil, nil, err
	}
	leafDER, err := ca.Sign(csrDER, name, devLeafTTL)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM, err = encodeKey(key)
	return certPEM, keyPEM, err
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func write(dir, name string, data []byte) error {
	mode := os.FileMode(0o644)
	if strings.HasSuffix(name, ".key") {
		mode = 0o600
	}
	return os.WriteFile(filepath.Join(dir, name), data, mode)
}

// writeSecret emits a v1.Secret in stringData form. URI SAN of the
// leaf cert is added as an annotation so operators can verify the
// secret matches the expected agent / relay identity at a glance.
func writeSecret(b *strings.Builder, namespace, name string, ident identity.Name, certPEM, keyPEM, caPEM []byte) {
	fmt.Fprintf(b, "apiVersion: v1\n")
	fmt.Fprintf(b, "kind: Secret\n")
	fmt.Fprintf(b, "metadata:\n")
	fmt.Fprintf(b, "  name: %s\n", name)
	fmt.Fprintf(b, "  namespace: %s\n", namespace)
	fmt.Fprintf(b, "  labels:\n")
	fmt.Fprintf(b, "    app.kubernetes.io/part-of: outrelay\n")
	fmt.Fprintf(b, "    app.kubernetes.io/managed-by: outrelay\n")
	fmt.Fprintf(b, "  annotations:\n")
	fmt.Fprintf(b, "    outrelay.dev/uri: %q\n", ident.String())
	fmt.Fprintf(b, "type: Opaque\n")
	fmt.Fprintf(b, "stringData:\n")
	fmt.Fprintf(b, "  tls.crt: |\n%s", indent(certPEM, "    "))
	fmt.Fprintf(b, "  tls.key: |\n%s", indent(keyPEM, "    "))
	fmt.Fprintf(b, "  ca.crt: |\n%s", indent(caPEM, "    "))
}

func indent(data []byte, prefix string) string {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(prefix)
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
