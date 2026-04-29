#!/usr/bin/env bash
# tests/terraform/scripts/preflight.sh
#
# Refuses to start a smoke run if the operator's environment is not
# ready or if a prior run left billable resources behind. Better to
# fail loud here than to silently pile up cloud cost.

set -euo pipefail

: "${AWS_REGION:?AWS_REGION must be set}"
: "${OWNER_TAG:?OWNER_TAG must be set}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "preflight: missing required tool: $1" >&2
    exit 1
  }
}
require terraform
require aws
require jq
require go

# AWS credential sanity. sts get-caller-identity is the cheapest way
# to confirm both that creds exist and that they reach AWS.
if ! aws sts get-caller-identity --region "$AWS_REGION" >/dev/null 2>&1; then
  echo "preflight: AWS credentials not usable in $AWS_REGION" >&2
  exit 1
fi

# Stale-resource check.
#
# We deliberately query each service in a state-filtered way rather
# than calling the unified Tagging API: terminated EC2 instances
# and deleted NAT Gateways stay visible in the Tagging API for ~1
# hour for audit, so a tag-only check would flag back-to-back runs
# as stale even when nothing is actually billing.
#
# Anything below is a real cost-incurring leftover and warrants
# human review before the next apply.

stale=()

while IFS= read -r line; do
  [[ -n "$line" ]] && stale+=("ec2: $line")
done < <(aws ec2 describe-instances \
  --region "$AWS_REGION" \
  --filters "Name=tag:owner,Values=$OWNER_TAG" \
            "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[].Instances[].InstanceId' \
  --output text 2>/dev/null | tr '\t' '\n')

while IFS= read -r line; do
  [[ -n "$line" ]] && stale+=("nat-gw: $line")
done < <(aws ec2 describe-nat-gateways \
  --region "$AWS_REGION" \
  --filter "Name=tag:owner,Values=$OWNER_TAG" \
           "Name=state,Values=pending,available" \
  --query 'NatGateways[].NatGatewayId' \
  --output text 2>/dev/null | tr '\t' '\n')

while IFS= read -r line; do
  [[ -n "$line" ]] && stale+=("eip: $line")
done < <(aws ec2 describe-addresses \
  --region "$AWS_REGION" \
  --filters "Name=tag:owner,Values=$OWNER_TAG" \
  --query 'Addresses[].AllocationId' \
  --output text 2>/dev/null | tr '\t' '\n')

while IFS= read -r line; do
  [[ -n "$line" ]] && stale+=("vpc: $line")
done < <(aws ec2 describe-vpcs \
  --region "$AWS_REGION" \
  --filters "Name=tag:owner,Values=$OWNER_TAG" \
  --query 'Vpcs[].VpcId' \
  --output text 2>/dev/null | tr '\t' '\n')

# S3 bucket names match a known prefix; the tag check needs a
# separate API call so list-buckets + filter by name is faster.
while IFS= read -r b; do
  [[ -n "$b" ]] && stale+=("s3: $b")
done < <(aws s3api list-buckets \
  --query "Buckets[?starts_with(Name, 'outrelay-smoke-')].Name" \
  --output text 2>/dev/null | tr '\t' '\n')

if [[ ${#stale[@]} -gt 0 ]]; then
  cat >&2 <<EOF
preflight: found existing billable resources tagged owner=$OWNER_TAG
in $AWS_REGION:

$(printf '  %s\n' "${stale[@]}")

Investigate before re-running. To force-clean, run:
  make destroy-aws         # if a prior terraform state still exists
  make cleanup-stale       # otherwise (per-ARN delete — TODO)
EOF
  exit 1
fi

echo "preflight: ok"
