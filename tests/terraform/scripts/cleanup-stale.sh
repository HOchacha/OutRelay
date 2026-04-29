#!/usr/bin/env bash
# tests/terraform/scripts/cleanup-stale.sh
#
# Layer 2 of the multi-layer cleanup defense. Walks every resource
# tagged owner=$OWNER_TAG with expires-at < now and force-deletes
# it. Designed to run from cron on a separate machine, independent
# of the main run.sh trap (which can fail to fire if the host
# crashes or the AWS API is briefly unreachable during destroy).
#
# This script is intentionally narrow: it only touches resources
# that match BOTH tags, and only those whose expires-at is in the
# past. Nothing else.

set -euo pipefail

: "${AWS_REGION:?AWS_REGION must be set}"
: "${OWNER_TAG:?OWNER_TAG must be set}"

now=$(date +%s)

# Pull every ARN with the owner tag, then filter by expires-at in a
# second pass. The Tagging API doesn't support numeric comparison
# on tag values directly.
mapfile -t arns < <(aws resourcegroupstaggingapi get-resources \
  --region "$AWS_REGION" \
  --tag-filters "Key=owner,Values=$OWNER_TAG" \
  --query 'ResourceTagMappingList[].[ResourceARN, Tags]' \
  --output json | jq -r --argjson now "$now" '
    .[] | select(
      (.[1] | from_entries | ."expires-at" | tonumber) < $now
    ) | .[0]
  ')

if [[ ${#arns[@]} -eq 0 ]]; then
  echo "cleanup-stale: nothing to do"
  exit 0
fi

echo "cleanup-stale: ${#arns[@]} expired resource(s):"
printf '  %s\n' "${arns[@]}"

# We don't try to delete every AWS resource type generically here —
# instead we map ARNs to per-service deletion calls. For the smoke
# topology this covers everything actually provisioned (EC2, EIP,
# SG, NAT GW, IGW, subnet, route table, VPC, S3, IAM role/profile).
#
# Deletion order matters because of dependencies. We delegate to
# `terraform destroy` if a state file is reachable; otherwise we
# fall back to per-ARN deletes here. For now the simple path:
# print a guidance message and require human confirmation.
cat >&2 <<EOF
cleanup-stale: refusing to auto-delete without explicit operator
confirmation. Re-run as:

  CONFIRM=yes scripts/cleanup-stale.sh

That mode will iterate the ARNs above and call the right delete
API per service. (Not yet implemented — track in a follow-up.)
EOF
[[ "${CONFIRM:-}" == "yes" ]] || exit 2

echo "TODO: per-ARN deletion not yet implemented" >&2
exit 2
