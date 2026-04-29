#!/usr/bin/env bash
# 03_relay_failover
#
# Cloud-side smoke for stream-resume on relay failover: stop the
# relay daemon, then bring it back, and confirm the consumer agent
# reconnects and resumes serving traffic. We're NOT measuring p99
# recovery time here — the Go-level e2e test pkg/edge/resume_e2e_test.go
# does that with tighter control. We *are* verifying that the resume
# path works end-to-end against real network conditions.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

control_iid=$(tf_out control_instance_id)
consumer_iid=$(tf_out consumer_instance_id)
url=$(tf_out primary_url)

# Sanity: traffic flows before we touch anything.
ssm_run "$consumer_iid" 30 <<EOF >/dev/null
curl --silent --show-error --fail --max-time 10 $url
EOF

# Bring the relay down, wait long enough that connection-state
# tracking expires the agent's outbound stream, then bring it back.
ssm_run "$control_iid" 60 <<'EOF' >/dev/null
sudo systemctl stop outrelay-relay.service
sleep 5
sudo systemctl start outrelay-relay.service
EOF

# Give the agent's reconnect-with-backoff a beat to pick up the new
# instance. Worst case backoff is 30s in the agent's session
# manager; we cap at 45.
deadline=$(( $(date +%s) + 45 ))
while :; do
  if ssm_run "$consumer_iid" 30 <<EOF >/dev/null 2>&1
curl --silent --show-error --fail --max-time 10 $url
EOF
  then
    break
  fi
  if [[ $(date +%s) -ge $deadline ]]; then
    fail "consumer never resumed after relay restart"
    exit 1
  fi
  sleep 2
done
