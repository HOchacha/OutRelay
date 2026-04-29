#!/usr/bin/env bash
# 06_p2p_positive
#
# Verifies the P2P promotion code path runs end-to-end with a
# successful outcome:
#   1. consumer Promote → CANDIDATE_OFFER on ctrl
#   2. relay forwards to provider; provider's controlReader auto-
#      answers with its --p2p-advertise candidate (the EIP)
#   3. consumer's Engine.Check QUIC-handshakes the EIP host candidate
#   4. consumer's MigrateToDirect → STREAM_RESUME on direct stream
#   5. provider's AcceptDirect → SwapInner the responder side
#   6. mid-flight bytes start flowing over the direct path: the
#      previous inner's CancelRead unblocks the bridge's parked
#      Read so the swap takes effect immediately.
#
# Skipped in the aws-gcp configuration (no EIP provider).

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

if [[ -z "$(tf_out provider_eip_instance_id 2>/dev/null || true)" ]]; then
  log "skip: aws-gcp configuration has no EIP provider"
  exit 0
fi

consumer_iid=$(tf_out consumer_instance_id)

# Fire a slow request in the background so the stream stays open
# long enough for OFFER/ANSWER + connectivity check + migration.
ssm_run "$consumer_iid" 30 <<'EOF' >/dev/null
nohup curl --silent --max-time 30 \
  --output /tmp/slow.out \
  http://127.0.0.1:30001/slow </dev/null >/dev/null 2>&1 &
disown
EOF

# 1s tryPromote delay + ~1s OFFER/ANSWER round-trip + connectivity
# check at 500ms per pair + STREAM_RESUME write — give it 6s.
sleep 6

log=$(ssm_run "$consumer_iid" 30 <<'EOF'
journalctl -u outrelay-consumer.service --no-pager -n 200
EOF
)

if ! grep -qiE 'migrated to direct' <<<"$log"; then
  fail "expected 'migrated to direct' on consumer; tail:"
  printf '%s\n' "$log" | tail -40 >&2
  exit 1
fi

# Wait for the slow curl to finish and verify the body. With
# transport.Stream.CancelRead in place, SwapInner now actually
# unblocks the bridge's Read on the old relay-mediated stream,
# so response bytes after migration arrive over the direct path
# rather than the relay. The body comes back transparent to curl.
out=$(ssm_run "$consumer_iid" 30 <<'EOF'
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  if [[ -s /tmp/slow.out ]]; then
    cat /tmp/slow.out
    exit 0
  fi
  sleep 1
done
echo "TIMEOUT" >&2
exit 1
EOF
)

if [[ "$out" != "ok-slow from provider-eip" ]]; then
  fail "post-migration body unexpected: $out"
  exit 1
fi
