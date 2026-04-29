#!/usr/bin/env bash
# 05_p2p_negative
#
# With both consumer and provider-nat behind AWS NAT GW (symmetric),
# P2P promotion fires on every stream open — the agent runs
# OFFER/ANSWER and a connectivity check — but the symmetric NAT on
# both ends means there is no usable candidate pair and Engine.Check
# returns ErrNoPair. The stream stays on the relay.
#
# What we verify here:
#   (a) the request still succeeds — relay path remains live when
#       P2P fails (the fallback property the architecture promises).
#   (b) the consumer's structured log carries a "no candidate pair"
#       line, proving the Promote code path actually executed.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

if [[ -z "$(tf_out provider_nat_instance_id 2>/dev/null || echo "")" ]]; then
  log "skip: this config has no NAT-bound provider (no svc-nat to dial)"
  exit 0
fi

consumer_iid=$(tf_out consumer_instance_id)

# Trigger a stream against svc-nat. Promotion is implicit — the
# agent fires Promote() in the background after each Dial().
ssm_run "$consumer_iid" 30 <<'EOF' >/dev/null
curl --silent --show-error --fail --max-time 10 http://127.0.0.1:30002/
EOF

# Give the connectivity check time to exhaust its candidates.
# DefaultPerPairTimeout is 500ms (pkg/p2p/check.go:27); with empty
# remotes the check returns immediately, but we add slack for the
# OFFER/ANSWER relay round-trip.
sleep 5

log=$(ssm_run "$consumer_iid" 30 <<'EOF'
journalctl -u outrelay-consumer.service --no-pager -n 200
EOF
)

# main.go logs "p2p: stayed on relay" with err wrapping
# "p2p: no candidate pair succeeded" (from Engine.Check ErrNoPair).
if ! grep -qiE 'no candidate pair|stayed on relay' <<<"$log"; then
  fail "expected ErrNoPair-equivalent log line; consumer log tail:"
  printf '%s\n' "$log" | tail -40 >&2
  exit 1
fi

# Relay path must still work — that's the contract.
out=$(ssm_run "$consumer_iid" 30 <<'EOF'
curl --silent --show-error --fail --max-time 10 http://127.0.0.1:30002/
EOF
)
if [[ "$out" != "ok from provider-nat" ]]; then
  fail "relay path should still work after P2P fails; got: $out"
  exit 1
fi
