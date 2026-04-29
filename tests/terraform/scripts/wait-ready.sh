#!/usr/bin/env bash
# tests/terraform/scripts/wait-ready.sh
#
# Poll each EC2 instance until its outrelay systemd unit reports
# active. Times out at 5 minutes — boot + binary fetch + dev-pki
# unpack should finish well inside that.

set -euo pipefail

# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

deadline=$(( $(date +%s) + 300 ))

# Pull every instance ID from terraform output. The root modules
# expose a list whose elements are "<role>:<instance-id>".
mapfile -t targets < <(terraform output -json instance_targets | jq -r '.[]')

for entry in "${targets[@]}"; do
  # Two formats supported:
  #   "<role>:<iid>"            — single-region config, default $AWS_REGION
  #   "<role>:<iid>:<region>"   — multi-region config, explicit region
  IFS=':' read -r role iid region <<<"$entry"
  region="${region:-$AWS_REGION}"
  unit="outrelay-$role"
  log "wait-ready: $iid ($unit, $region)"

  while :; do
    status=$(ssm_run "$iid" 30 "$region" <<EOF || true
systemctl is-active --quiet $unit && echo active || echo inactive
EOF
)
    [[ "$status" == "active" ]] && { ok "$unit on $iid"; break; }
    if [[ $(date +%s) -ge $deadline ]]; then
      fail "wait-ready: $unit on $iid"
      # Dump journalctl + cloud-init log so the operator can see
      # *why* the unit didn't reach active before the run gets torn
      # down. Both are best-effort.
      ssm_run "$iid" 30 "$region" <<EOF >&2 || true
echo "=== systemctl status $unit ==="
systemctl status $unit --no-pager -l || true
echo "=== journalctl -u $unit (last 80 lines) ==="
journalctl -u $unit --no-pager -n 80 || true
echo "=== cloud-init-output.log (tail 200, progress lines stripped) ==="
sudo tail -200 /var/log/cloud-init-output.log 2>/dev/null |
  grep -v 'Completed [0-9]' || true
echo "=== ls /opt/outrelay ==="
ls -la /opt/outrelay /opt/outrelay/bin /opt/outrelay/pki 2>/dev/null || true
echo "=== /etc/systemd/system/$unit.service ==="
cat /etc/systemd/system/$unit.service 2>/dev/null || echo "(unit file missing)"
EOF
      exit 1
    fi
    sleep 5
  done
done
