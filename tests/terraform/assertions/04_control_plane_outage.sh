#!/usr/bin/env bash
# 04_control_plane_outage
#
# Verifies the data-plane recovers cleanly after the control plane
# is killed and brought back. The contract is that *in-flight*
# data-plane traffic survives a control-plane outage; brand-new
# stream opens *during* the outage need a Registry lookup against
# the controller and fail by definition.
#
# This assertion verifies the weaker but still meaningful property:
# control-plane restart doesn't permanently break the data plane.
# New streams work both before and after. Verifying that a single
# long-running stream survives the outage end-to-end needs a
# long-lived fixture and is left to the package-level integration
# tests.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

control_iid=$(tf_out control_instance_id)
consumer_iid=$(tf_out consumer_instance_id)
url=$(tf_out primary_url)

# Baseline: curl works.
out=$(ssm_run "$consumer_iid" 30 <<EOF
curl --silent --show-error --fail --max-time 10 $url
EOF
)
if [[ "$out" != ok\ from\ * ]]; then
  fail "baseline curl failed: $out"
  exit 1
fi

# SIGKILL controller and tell systemd not to auto-restart for now.
ssm_run "$control_iid" 60 <<'EOF' >/dev/null
sudo systemctl kill -s KILL outrelay-controller.service || true
sudo systemctl stop outrelay-controller.service || true
EOF

# Restart on the way out (always — even on assertion failure).
restore_controller() {
  ssm_run "$control_iid" 60 <<'EOF' >/dev/null || true
sudo systemctl start outrelay-controller.service
EOF
  local deadline=$(( $(date +%s) + 30 ))
  while :; do
    if ssm_run "$control_iid" 30 <<'EOF' >/dev/null 2>&1
/opt/outrelay/bin/outrelay-cli policy list \
  --controller=127.0.0.1:7444 --tenant=acme >/dev/null
EOF
    then
      return 0
    fi
    [[ $(date +%s) -ge $deadline ]] && return 1
    sleep 2
  done
}
trap restore_controller EXIT

# Bring it back and confirm the data plane recovers — same curl
# must succeed after restart.
restore_controller || { fail "controller never came back"; exit 1; }

# Give the relay's policy watch a beat to re-establish.
sleep 5

out=$(ssm_run "$consumer_iid" 30 <<EOF
curl --silent --show-error --fail --max-time 10 $url
EOF
)
if [[ "$out" != ok\ from\ * ]]; then
  fail "post-restart curl failed: $out"
  exit 1
fi
