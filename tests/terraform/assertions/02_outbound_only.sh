#!/usr/bin/env bash
# 02_outbound_only
#
# Provider-nat sits in a private subnet behind a NAT GW with a
# security group that allows zero inbound traffic. If the consumer
# can still reach svc-nat (a different service exposed by that
# provider), we've shown that the relay-mediated path works without
# *any* ingress to the provider's host. That's the whole point of
# the outbound-only model — and the gap to Submariner / Skupper.
#
# Two checks:
#   (a) the SG genuinely has no ingress rules
#   (b) the curl through the consumer succeeds anyway

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

provider_nat_iid=$(tf_out provider_nat_instance_id 2>/dev/null || echo "")
if [[ -z "$provider_nat_iid" ]]; then
  log "skip: this config has no NAT-bound provider (only meaningful when an agent is behind a NAT GW)"
  exit 0
fi
consumer_iid=$(tf_out consumer_instance_id)

# (a) Inspect the SG attached to provider-nat. We expect the
# IpPermissions array to be empty.
sg_id=$(aws ec2 describe-instances \
  --region "$AWS_REGION" \
  --instance-ids "$provider_nat_iid" \
  --query 'Reservations[0].Instances[0].SecurityGroups[0].GroupId' \
  --output text)
ingress_count=$(aws ec2 describe-security-groups \
  --region "$AWS_REGION" \
  --group-ids "$sg_id" \
  --query 'SecurityGroups[0].IpPermissions | length(@)' \
  --output text)
if [[ "$ingress_count" != "0" ]]; then
  fail "provider-nat SG has $ingress_count ingress rules; expected 0"
  exit 1
fi

# (b) Reach svc-nat via the relay path.
out=$(ssm_run "$consumer_iid" 30 <<'EOF'
curl --silent --show-error --fail --max-time 10 http://127.0.0.1:30002/
EOF
)

if [[ "$out" != "ok from provider-nat" ]]; then
  fail "unexpected response: $out"
  exit 1
fi
