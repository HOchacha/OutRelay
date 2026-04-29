#!/usr/bin/env bash
# tests/terraform/scripts/build-binaries.sh
#
# Cross-compiles controller / relay / agent / cli for linux/amd64
# from the three sibling repos. Output goes to ./.artifacts/ which
# the Terraform artifact-bucket module uploads to S3. Runs the
# dev-pki tool too, so the bucket also carries the CA + leaf certs.

set -euo pipefail

: "${REPO_ROOT:?REPO_ROOT must be set by the Makefile}"

ART="$REPO_ROOT/tests/terraform/.artifacts"
mkdir -p "$ART/bin" "$ART/pki"

export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

build() {
  local repo="$1" pkg="$2" out="$3"
  echo "build: $out"
  ( cd "$REPO_ROOT/../$repo" && \
    go build -trimpath -ldflags '-s -w' -o "$ART/bin/$out" "$pkg" )
}

# REPO_ROOT here points at OutRelay (the controller repo). The relay
# and agent are siblings — see getting-started/local-cluster.md.
build "OutRelay"        ./cmd/outrelay-controller outrelay-controller
build "OutRelay"        ./cmd/outrelay-cli        outrelay-cli
build "outrelay-relay"  ./cmd/outrelay-relay      outrelay-relay
build "outrelay-agent"  ./cmd/outrelay-agent      outrelay-agent

# Smoke PKI: CA, relay leaf, three agent leaves (provider-eip,
# provider-nat, consumer). Lives next door so we don't touch the
# main dev-pki tool, which only emits two agent UUIDs.
echo "smoke-pki: regenerating $ART/pki"
rm -rf "$ART/pki"
# Always emit r1+r2 leaves so the same artifact bundle works for
# every smoke variant. Unused leaves are harmless extras for the
# single-relay configs (aws-only, aws-gcp).
( cd "$REPO_ROOT" && \
  go run ./tests/terraform/scripts/smoke-pki -out "$ART/pki" -relay-ids "r1,r2" )

echo "build-binaries: ok"
ls -lh "$ART/bin"
