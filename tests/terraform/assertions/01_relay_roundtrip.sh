#!/usr/bin/env bash
# 01_relay_roundtrip
#
# The simplest possible end-to-end check: the consumer agent dials
# its localhost listener, traffic flows consumer-agent → relay →
# provider-agent → echo HTTP server. Pass means the four-phase ORP
# round-trip works on real EC2 with real network paths.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

consumer_iid=$(tf_out consumer_instance_id)
url=$(tf_out primary_url)

# Each config picks its own primary_url to avoid hard-coding a
# provider role: aws-only uses the EIP provider on 30001; aws-gcp
# uses the NAT-bound provider on 30002. Body check stays role-
# agnostic ("ok from <role>") so both configs match.
out=$(ssm_run "$consumer_iid" 30 <<EOF
curl --silent --show-error --fail --max-time 10 $url
EOF
)

if [[ "$out" != ok\ from\ * ]]; then
  fail "unexpected response: $out"
  exit 1
fi
