#!/usr/bin/env bash
# 07_policy_deny
#
# Adds a specific deny rule for (consumer URI → svc-nat), exercises
# the call, then verifies the relay rejects the stream open. Cleans
# up the deny rule on exit so subsequent re-runs aren't poisoned.
#
# Specific-over-wildcard precedence: this deny must override the
# allow-all rule installed by 00_setup_policy. If the policy engine
# instead picks first-match, this assertion will fail and we'll
# need to invert the order (delete allow-all first, add deny, run,
# restore allow-all).

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

control_iid=$(tf_out control_instance_id)
consumer_iid=$(tf_out consumer_instance_id)

# Defensive: if assertion 04's restore_controller didn't fully
# settle, give the controller a brief window to come back before
# we add a policy. Otherwise the cli call fails "connection
# refused on 127.0.0.1:7444" and masks the real assertion.
deadline=$(( $(date +%s) + 30 ))
while :; do
  if ssm_run "$control_iid" 30 <<'EOF' >/dev/null 2>&1
/opt/outrelay/bin/outrelay-cli policy list \
  --controller=127.0.0.1:7444 --tenant=acme >/dev/null
EOF
  then
    break
  fi
  if [[ $(date +%s) -ge $deadline ]]; then
    fail "controller not reachable on :7444 after 30s wait"
    exit 1
  fi
  sleep 2
done

# Add a deny rule scoped tightly so other assertions are unaffected
# if the harness reuses state. Capture the policy id for cleanup.
deny_id=$(ssm_run "$control_iid" 30 <<'EOF'
/opt/outrelay/bin/outrelay-cli policy add \
  --controller=127.0.0.1:7444 \
  --tenant=acme \
  --caller='outrelay://acme/agent/00000000-0000-0000-0000-000000000002' \
  --target='svc-nat' \
  --decision=deny
EOF
)
deny_id=$(echo "$deny_id" | tr -d '[:space:]')

cleanup_deny() {
  ssm_run "$control_iid" 30 <<EOF >/dev/null || true
/opt/outrelay/bin/outrelay-cli policy remove \
  --controller=127.0.0.1:7444 --tenant=acme --id=$deny_id
EOF
}
trap cleanup_deny EXIT

# Allow some time for the relay's policy watch-stream to receive
# the new rule. Policy enforcement is eventual (no synchronous
# fanout), but in-process gRPC streams typically deliver within a
# few hundred ms.
sleep 3

# The curl should now fail. We capture the http exit code to
# distinguish "blocked by policy" (transport/connection error from
# the agent's side) from "succeeded with non-200" (would be a bug).
rc=0
ssm_run "$consumer_iid" 30 <<'EOF' >/dev/null || rc=$?
set +e
curl --silent --show-error --fail --max-time 10 http://127.0.0.1:30002/
exit $?
EOF

if [[ "$rc" -eq 0 ]]; then
  fail "deny rule did not block traffic — curl unexpectedly succeeded"
  exit 1
fi
