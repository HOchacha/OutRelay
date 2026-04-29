#!/usr/bin/env bash
# 12_forward_plane
#
# Validates the L3 mini-TURN forward plane on real cloud
# infrastructure: the relay listens on UDP/9443 with the SG
# opened, and a single consumer EC2 stands up two UDP sockets
# (acting as "agent A" and "agent B" for the test). Each socket
# sends a registration packet to the relay; A then sends a data
# packet whose 4-byte prefix points at B's allocation; the relay
# strips the prefix and forwards the payload to B's endpoint.
#
# The full agent-side integration (read AllocGranted from the
# relay-mediated stream → open forwardConn → e2e QUIC handshake
# with peer) is staged for production work. This assertion
# validates the *plane's* behavior under real cloud SGs, NAT GW,
# and cross-zone routing — the parts that local unit tests can't
# exercise.
#
# Skipped when forward_endpoint output is empty.

set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/../scripts/lib.sh"

if [[ -z "$(tf_out forward_endpoint 2>/dev/null || echo "")" ]]; then
  log "skip: this config doesn't enable the forward plane"
  exit 0
fi

consumer_iid=$(tf_out consumer_instance_id)
fwd_endpoint=$(tf_out forward_endpoint)

# The script below runs on the consumer EC2 via SSM. It opens two
# UDP sockets locally (one acting as "A", the other as "B"),
# registers each with a distinct allocation id at the relay, then
# A sends a data packet addressed to B. Success = B's recvfrom
# returns the exact payload A sent.
#
# Allocation ids 0xA0A0A0A0 and 0xB0B0B0B0 are arbitrary
# constants high enough to avoid collision with any allocator-
# assigned ids during the smoke run.
out=$(ssm_run "$consumer_iid" 60 <<EOF
python3 - <<'PY'
import socket, struct, sys, time

RELAY = "$fwd_endpoint".rsplit(":", 1)
relay_addr = (RELAY[0], int(RELAY[1]))
ALLOC_A = 0xA0A0A0A0
ALLOC_B = 0xB0B0B0B0

a = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
b = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
a.settimeout(5.0)
b.settimeout(5.0)
# Bind explicitly so each socket has its own ephemeral port; the
# relay will record one entry per socket.
a.bind(("0.0.0.0", 0))
b.bind(("0.0.0.0", 0))

def register(s, alloc):
    pkt = struct.pack(">II", 0, alloc)  # peer_alloc=0 = registration
    s.sendto(pkt, relay_addr)

register(a, ALLOC_A)
register(b, ALLOC_B)

# Brief wait for the relay's read loop to record both registrations.
time.sleep(0.3)

# A → relay → B. payload is just bytes; the relay strips the
# 4-byte prefix before forwarding.
payload = b"hello-from-A-via-mini-TURN-plane"
pkt = struct.pack(">I", ALLOC_B) + payload
a.sendto(pkt, relay_addr)

try:
    data, _ = b.recvfrom(2048)
except socket.timeout:
    print("TIMEOUT: B never received forwarded packet", file=sys.stderr)
    sys.exit(1)
if data != payload:
    print(f"BAD: got {data!r}, want {payload!r}", file=sys.stderr)
    sys.exit(1)
print("OK")
PY
EOF
) || true

if [[ "$out" != *"OK"* ]]; then
  fail "forward plane round-trip failed: $out"
  exit 1
fi
