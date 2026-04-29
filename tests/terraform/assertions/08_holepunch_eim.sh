#!/usr/bin/env bash
# 08_holepunch_eim
#
# Cross-cloud P2P promotion. Consumer (AWS, behind NAT GW) → relay
# (AWS) → provider-gcp (GCP, ephemeral public IP). Same end-to-end
# shape as assertion 06 against svc-eip, but the provider lives on
# a different CSP — proves the system works across clouds, GCS-
# based binary distribution, Debian runtime.
#
# B1 caveat (intentional): the GCP provider has a public IP, NOT
# Cloud NAT with EIM. True symmetric ↔ EIM hole-punching depends on
# the GCP-side filtering, which the cloud smoke shows is endpoint-
# dependent — covered separately in the B2 variant. With a public
# IP the connectivity check succeeds via the host candidate that
# srflx-style auto-detection emits from the eth0 interface.
#
# In aws-only this skips because there is no GCP provider.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

if ! gcp_iid=$(tf_out provider_gcp_instance_id 2>/dev/null); then
  log "skip: aws-only configuration has no GCP provider"
  exit 0
fi
if [[ -z "$gcp_iid" ]]; then
  log "skip: aws-only configuration has no GCP provider"
  exit 0
fi

consumer_iid=$(tf_out consumer_instance_id)

# wait-ready.sh polls only AWS units via SSM (we don't run an SSM-
# equivalent on GCP). Block here until the GCP provider is actually
# serving requests — exposed indirectly through a fast curl from
# the consumer. Budget 5 minutes for VM boot + GCS pull + python3
# install + service register.
log "waiting for svc-gcp to be reachable through the relay"
deadline=$(( $(date +%s) + 300 ))
while :; do
  if out=$(ssm_run "$consumer_iid" 30 <<'EOF'
curl --silent --show-error --fail --max-time 5 http://127.0.0.1:30003/
EOF
  ) && [[ "$out" == "ok from provider-gcp" ]]; then
    break
  fi
  if [[ $(date +%s) -ge $deadline ]]; then
    fail "svc-gcp never came up within 5 minutes"
    exit 1
  fi
  sleep 10
done

# Now exercise the long-lived path so promotion has time to fire.
ssm_run "$consumer_iid" 30 <<'EOF' >/dev/null
nohup curl --silent --max-time 30 \
  --output /tmp/slow-gcp.out \
  http://127.0.0.1:30003/slow </dev/null >/dev/null 2>&1 &
disown
EOF

sleep 6

log=$(ssm_run "$consumer_iid" 30 <<'EOF'
journalctl -u outrelay-consumer.service --no-pager -n 200
EOF
)

# Match either an explicit svc-gcp migrate line or the generic
# "migrated to direct". The agent's log keys "svc" alongside the
# message, so a tighter match is safe.
if ! grep -qiE 'migrated to direct.*svc-gcp|svc.*svc-gcp.*migrated' <<<"$log"; then
  fail "expected 'migrated to direct' for svc-gcp; tail:"
  printf '%s\n' "$log" | tail -40 >&2
  exit 1
fi

# Body confirms post-migration bytes flowed.
out=$(ssm_run "$consumer_iid" 30 <<'EOF'
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  if [[ -s /tmp/slow-gcp.out ]]; then
    cat /tmp/slow-gcp.out
    exit 0
  fi
  sleep 1
done
echo "TIMEOUT" >&2
exit 1
EOF
)

if [[ "$out" != "ok-slow from provider-gcp" ]]; then
  fail "post-migration body unexpected: $out"
  exit 1
fi
