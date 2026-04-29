#!/usr/bin/env bash
# tests/terraform/scripts/run.sh
#
# The harness. Always tears down on the way out — pass, fail, or
# kill. Reports per-assertion result and exits non-zero if any
# assertion failed.

set -euo pipefail

: "${CONFIG:?CONFIG must be aws-only or aws-gcp}"
: "${RUN_ID:?RUN_ID must be set by the Makefile}"
: "${EXPIRES_AFTER_HOURS:?}"
: "${AWS_REGION:?}"
: "${OWNER_TAG:?}"
: "${REPO_ROOT:?}"

ROOT="$REPO_ROOT/tests/terraform"
ROOT_MOD="$ROOT/$CONFIG"
# shellcheck disable=SC1091
source "$ROOT/scripts/lib.sh"

# Layer 1 of the multi-layer cleanup defense — destroy on any exit
# path. The trap fires even on SIGINT/SIGTERM, and the destroy is
# idempotent (no-op if apply never ran).
cleanup() {
  local rc=$?
  log "tearing down ($CONFIG)"
  # artifacts_dir must be passed even on destroy: the artifact-bucket
  # module's for_each is computed from fileset(), which Terraform
  # evaluates regardless of whether instances exist in state.
  ( cd "$ROOT_MOD" && terraform destroy -auto-approve \
      -var "run_id=$RUN_ID" \
      -var "owner_tag=$OWNER_TAG" \
      -var "aws_region=$AWS_REGION" \
      -var "expires_after_hours=$EXPIRES_AFTER_HOURS" \
      -var "artifacts_dir=$ROOT/.artifacts" ) || \
    fail "terraform destroy returned non-zero -- investigate manually"
  exit "$rc"
}
trap cleanup EXIT INT TERM

log "preflight"
"$ROOT/scripts/preflight.sh"

log "building linux/amd64 binaries + dev PKI"
"$ROOT/scripts/build-binaries.sh"

log "terraform apply ($CONFIG)"
cd "$ROOT_MOD"
terraform init -input=false
terraform apply -auto-approve \
  -var "run_id=$RUN_ID" \
  -var "owner_tag=$OWNER_TAG" \
  -var "aws_region=$AWS_REGION" \
  -var "expires_after_hours=$EXPIRES_AFTER_HOURS" \
  -var "artifacts_dir=$ROOT/.artifacts"

log "waiting for systemd units to come up"
"$ROOT/scripts/wait-ready.sh"

log "running assertions"
failed=0
shopt -s nullglob
for a in "$ROOT/assertions"/*.sh; do
  name=$(basename "$a" .sh)
  if "$a"; then
    ok "$name"
  else
    fail "$name"
    failed=$((failed + 1))
  fi
done

if [[ $failed -gt 0 ]]; then
  fail "$failed assertion(s) failed"
  exit 1
fi
ok "all assertions passed"
