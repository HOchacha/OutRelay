#!/usr/bin/env bash
# 11_inter_relay
#
# Cross-region inter-relay forwarding. The aws-multi-region smoke
# splits consumer/provider across two regions: provider registers
# svc-remote with relay-r2 (us-east-1); consumer in ap-northeast-2
# has relay-r1 + relay-r2 in its --relay list and DialAnyHappy
# picks the lower-RTT r1. When consumer dials svc-remote, the
# stream open on r1 resolves to a provider on r2, returns
# ErrProviderRemote, and r1 forwards via intra.Pool to r2.
# End-to-end success proves multi-region routing.
#
# Skipped in aws-only / aws-gcp where there's no remote provider.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

if [[ -z "$(tf_out provider_remote_instance_id 2>/dev/null || true)" ]]; then
  log "skip: not a multi-region config"
  exit 0
fi

consumer_iid=$(tf_out consumer_instance_id)

out=$(ssm_run "$consumer_iid" 30 <<'EOF'
curl --silent --show-error --fail --max-time 15 http://127.0.0.1:30001/
EOF
)

if [[ "$out" != "ok from provider-remote" ]]; then
  fail "cross-region curl unexpectedly returned: $out"
  exit 1
fi

# Confirm the consumer actually picked the local relay (r1 in
# primary region) — DialAnyHappy log line carries the chosen addr.
log_text=$(ssm_run "$consumer_iid" 30 <<'EOF'
journalctl -u outrelay-consumer.service --no-pager -n 80
EOF
)
if ! grep -qiE 'nearest-relay selected' <<<"$log_text"; then
  log "warning: consumer didn't log nearest-relay selection (may have only one healthy endpoint)"
fi
