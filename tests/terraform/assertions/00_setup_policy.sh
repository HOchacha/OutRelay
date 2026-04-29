#!/usr/bin/env bash
# 00_setup_policy
#
# Adds an allow-all policy so the subsequent assertions can run
# without each setting up their own. Assertion 07 explicitly
# overrides this with a more specific deny.
#
# The controller starts closed-world; we add the allow-all here,
# after wait-ready confirms both daemons are up, rather than baking
# it into cloud-init so a stale policy doesn't survive across runs.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

control_iid=$(tf_out control_instance_id)

ssm_run "$control_iid" 30 <<'EOF' >/dev/null
/opt/outrelay/bin/outrelay-cli policy add \
  --controller=127.0.0.1:7444 \
  --tenant=acme --caller='*' --target='*' --decision=allow
EOF
