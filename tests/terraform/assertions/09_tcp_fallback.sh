#!/usr/bin/env bash
# 09_tcp_fallback
#
# Verifies the L2.5 TCP+TLS+yamux fallback path is wired end-to-end.
# The aws-only smoke deploys a second consumer (agent_consumer_tcp)
# whose --relay points at a guaranteed-to-fail .invalid hostname
# and whose --relay-tcp points at the real relay's TCP/443 endpoint.
# QUIC dial errors out fast (DNS NXDOMAIN per RFC 6761), the agent
# falls through to TCP, mTLS handshake completes over yamux, the
# session registers the consumed service, and the relay-mediated
# curl returns 200 the same as the QUIC consumer.
#
# Skipped in aws-gcp where no TCP-fallback consumer is deployed.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

if [[ -z "$(tf_out consumer_tcp_instance_id 2>/dev/null || true)" ]]; then
  log "skip: aws-gcp has no TCP-fallback consumer"
  exit 0
fi

iid=$(tf_out consumer_tcp_instance_id)
control_iid=$(tf_out control_instance_id)

# Diagnostic: on any failure below, dump consumer-tcp + relay logs
# so the operator can see what TCP fallback is actually doing.
dump_diagnostics() {
  log "consumer-tcp journal (last 80 lines):"
  ssm_run "$iid" 30 <<'EOF' >&2 || true
journalctl -u outrelay-consumer-tcp.service --no-pager -n 80
EOF
  log "relay journal (filtered for tcp/listen):"
  ssm_run "$control_iid" 30 <<'EOF' >&2 || true
journalctl -u outrelay-relay.service --no-pager -n 200 |
  grep -iE 'tcp|listen|fallback|443|yamux|hello' || true
EOF
}

# Relay-mediated round-trip via the TCP-fallback consumer's local
# listener. The consumer-tcp agent consumes svc-nat on 30002.
out=$(ssm_run "$iid" 30 <<'EOF'
curl --silent --show-error --fail --max-time 10 http://127.0.0.1:30002/ 2>&1
EOF
) || true
if [[ "$out" != ok\ from\ * ]]; then
  fail "tcp-fallback consumer unexpectedly returned: $out"
  dump_diagnostics
  exit 1
fi

# Confirm the agent actually took the TCP path (and not somehow QUIC).
log_text=$(ssm_run "$iid" 30 <<'EOF'
journalctl -u outrelay-consumer-tcp.service --no-pager -n 80
EOF
)
if ! grep -qiE 'TCP\+TLS fallback|relay-tcp dial' <<<"$log_text"; then
  fail "expected TCP fallback log on consumer-tcp; tail:"
  printf '%s\n' "$log_text" | tail -30 >&2
  exit 1
fi
