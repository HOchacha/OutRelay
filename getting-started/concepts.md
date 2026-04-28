# Concepts

This page introduces the building blocks one at a time. It assumes
you have already read [`architecture.md`](architecture.md).

## Identity

Every agent and every relay has a stable URI:

```
outrelay://<tenant>/agent/<uuid>
outrelay://<region>/relay/<id>
```

The URI is encoded as a **URI SAN** on the leaf X.509 certificate the
controller signs. The relay does mTLS, extracts the URI from the
peer's certificate, and uses it as the caller / target identity for
every downstream decision.

Agents (and relays) bootstrap with a one-shot **enrollment token** —
a short-lived JWT signed by the controller's enrollment key. The
agent presents the token on first connect, sends a CSR carrying its
URI SAN, and receives a leaf cert. After bootstrap, certs renew
automatically via `lib/identity.Rotator` before they expire.

See `lib/identity/name.go` for the URI parser, `pkg/pki/ca.go` for
the signing logic, and `pkg/pki/enroll.go` for token issuance.

## ORP — the wire protocol

ORP (OutRelay Protocol) rides on QUIC.

- **Stream 0** carries control frames (HELLO, REGISTER,
  OPEN_STREAM, …).
- **Streams N>0** carry one application stream each. The first frame
  is a control frame (`STREAM_INIT` / `INCOMING_STREAM`), then the
  relay enters splice mode and copies raw bytes.

Each frame is an 8-byte big-endian header followed by a
protobuf-encoded payload:

```
bytes [0:2]  Version(3) | Reserved(2) | Type(11)
bytes [2:4]  Flags
bytes [4:8]  Length (payload bytes, max 2^24)
bytes [8:..] Payload (protobuf message for the given Type)
```

The codec lives in `lib/orp/frame.go`. Payload schemas are in
`api/orp/v1/orp.proto`.

## Stream lifecycle (FSM)

A stream walks a small state machine, implemented in `lib/orp/stream.go`:

```
Init  --Open---->  Opening
Opening   --Accept-->  Accepted   --Fin--> Closing
Accepted  --Splice-->  Spliced    --Fin--> Closing
Spliced               --Fin--> Closing
Closing   --Fin--> Closed
(any)     --Reset--> Closed
```

The state transitions are driven by the frames that arrive on the
control stream and the splicing decision the relay makes after
`STREAM_ACCEPT`.

## Service registry

When an agent provides a service, it sends `REGISTER` to its relay.
The relay calls `Registry.RegisterService` on the controller, which
persists the binding `(tenant, name) -> (agent_uri, relay_id)`. On
the consumer side, the relay calls `Registry.Resolve` for every
incoming `OPEN_STREAM` to find the provider's relay binding.

Relays subscribe via `Registry.Watch` so they cache binding changes
in memory and avoid blocking calls on the hot path. The Watch stream
is **slow-consumer-tolerant**: if a watcher's queue fills up, the
controller closes its stream so the relay knows to reconnect and
re-list.

## Policy

Policy rules are tuples
`(caller_pattern, target_pattern, method_pattern, decision, expires, p2p_mode)`.
The relay caches the full policy set in memory and evaluates it on
every `OPEN_STREAM`.

The relay subscribes via `Policy.Watch`. The first messages on the
stream are one `ADDED` event per existing rule, terminated by a
`SNAPSHOT_END` marker; after that, the stream carries live `ADDED`
and `REMOVED` events.

Default is **closed-world**: with no rules in place, every
`OPEN_STREAM` is denied. The dev quickstart adds a wildcard ALLOW
rule so the demo can talk.

## Audit

The relay calls `Audit.Record` once per stream-open decision (allow
or deny). Events are persisted in the same SQLite database used by
the registry and policy. Operators query history with
`outrelay-cli audit query`.

## Resume

Relays are stateless, so a relay restart would normally tear down
every in-flight stream. ORP avoids that with two control frames
(`lib/resume/`):

- `STREAM_CHECKPOINT` — sent every 100 ms by each agent, carrying
  `(stream_id, my_position, peer_ack_position)`. The peer uses
  `peer_ack_position` to free its ring buffer.
- `STREAM_RESUME` — sent on a fresh stream after both agents
  reconnect. The relay buckets two halves of the same `stream_id`
  inside a 5-second window, splices them back together, and each
  agent retransmits the gap from its ring buffer.

The ring buffer (`lib/resume/ringbuffer.go`) keeps the last N bytes
written, with grow/shrink hysteresis (grow when the next write would
overflow, shrink when occupancy falls below cap/4) so memory is
proportional to in-flight bytes.

## P2P promotion (opportunistic)

When both agents can reach each other directly (e.g. same VPC, or
NAT mapping is benign), ORP can opportunistically migrate a stream
off the relay onto a direct QUIC connection. The relay acts as a
built-in STUN: each agent sends `OBSERVED_ADDR_QUERY`, the relay
replies with the agent's NAT-mapped src, and the agents trade ICE-
style `CANDIDATE_OFFER` / `CANDIDATE_ANSWER`. After a connectivity
check, both agents send `MIGRATE_TO_P2P` simultaneously and the
in-flight stream resumes on the new path.

P2P is governed by the rule's `p2p_mode`:

- `P2P_ALLOWED` (default) — opportunistic; on failure, stay on the relay.
- `P2P_FORBIDDEN` — never attempt; all bytes flow through the relay
  so audit and metering can observe them.
- `P2P_REQUIRED` — open is rejected if P2P cannot be established.
  Useful for channels where the relay seeing plaintext is a security
  concern (e.g. plaintext key exchange).

See `api/orp/v1/orp.proto` for the frame definitions.

## Observability

The shared `lib/observe` package provides counters, gauges, and
bucketed histograms with `Snapshot` → JSONL output. The controller
exposes `/debug/metrics` (snapshot as JSON) and `/debug/pprof/*` on a
**localhost-only** debug port. The agent / relay binaries reuse the
same library so a single `tools/correlate` invocation can stitch
together events across all three components by `stream_id`.

There is intentionally no Prometheus / OpenTelemetry exporter. The
JSONL dump and `correlate` are designed to be reproducible without a
running TSDB.
