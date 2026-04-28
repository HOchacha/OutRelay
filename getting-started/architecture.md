# Architecture

OutRelay has three components. Each is its own binary; the relay and
agent live in their own repositories and import this repo as a Go
module.

## The components

| Component        | Repo                                                    | What it does                                                                 |
|------------------|---------------------------------------------------------|------------------------------------------------------------------------------|
| **controller**   | this repo (`cmd/outrelay-controller`)                   | Issues identities (PKI), keeps the service registry, owns policy and audit. |
| **relay**        | [`outrelay-relay`](https://github.com/boanlab/outrelay-relay) | Stateless QUIC relay that splices two outbound mTLS streams.               |
| **agent**        | [`outrelay-agent`](https://github.com/boanlab/outrelay-agent) | Per-workload sidecar / VM agent. Dials the relay outbound.                 |

The controller is the only component with persistent state (a single
SQLite database). Relays are stateless: they hold an in-memory copy
of the registry and policy snapshots, and rebuild it from the
controller on restart. Agents are also stateless across restarts.

## The data path

```
        ┌──────────────┐                                       ┌──────────────┐
        │  consumer    │ outbound QUIC :7443                   │  provider    │
        │  app         │  (mTLS, ALPN orp/1)                   │  app         │
        └──────┬───────┘                                       └──────┬───────┘
               │                                                      │
        ┌──────▼───────┐ stream open                  stream open ┌───▼──────┐
        │  agent (C)   │─────────────►   ┌─────────┐  ◄───────────│ agent (P)│
        │              │                  │  relay  │              │          │
        │              │◄─────── splice ──┤         ├── splice ───►│          │
        └──────────────┘                  └────┬────┘              └──────────┘
                                               │
                                               │ gRPC :7444
                                               ▼
                                       ┌────────────────┐
                                       │  controller    │
                                       │  (Registry,    │
                                       │   Policy,      │
                                       │   Audit, PKI)  │
                                       └────────────────┘
```

When the consumer app makes a request:

1. **Consumer agent** opens a fresh QUIC stream to the relay and
   writes an `OPEN_STREAM` frame naming the target service.
2. **Relay** asks the controller `Resolve(target)` to find the
   provider's relay binding; runs the cached policy snapshot to
   decide allow/deny; emits one audit `Record` to the controller.
3. **Relay** opens a new stream to the **provider agent** and writes
   `INCOMING_STREAM`; the provider answers `STREAM_ACCEPT` or
   `STREAM_REJECT`.
4. On accept, the relay enters **splice mode**: it stops parsing ORP
   frames on this stream pair and just copies bytes between the two
   QUIC streams in user space.

Both halves are outbound from the agent's point of view, so neither
side has to open an inbound port.

## The control path

The controller exposes three gRPC services on port 7444:

- **Registry** — agents announce the services they provide; relays
  resolve services on stream open and subscribe via `Watch` to be
  notified when bindings change.
- **Policy** — operators add / remove allow/deny rules; relays cache
  the full policy set in memory and re-evaluate on every
  `OPEN_STREAM`. The first message a relay sees after `Watch` is a
  full snapshot followed by a `SNAPSHOT_END` marker, after which the
  stream switches to live updates.
- **Audit** — the relay sends one `Record` per stream-open decision;
  operators query history with `outrelay-cli audit query`.

Identity (the controller's mini-CA) is implemented in `pkg/pki` but
is not yet exposed over gRPC. For now, agents bootstrap from a token
+ cert pair on disk; `make dev-pki` produces a usable bundle.

## Why these boundaries?

- **Stateless relay:** crashing or replacing a relay must be
  uneventful. State the relay needs (registry, policy) is pulled
  from the controller's `Watch` streams. State each in-flight stream
  needs to survive a relay restart (byte counters, ring buffer of
  unacked bytes) lives on the *agents*; the relay-side resume
  matcher just pairs two reconnecting agents on the same
  `stream_id`.

- **One controller, one SQLite file:** the controller is the source
  of truth and the only component that needs durable storage. SQLite
  keeps the deployment manifest a single Pod with a `Recreate`
  rollout (no rolling update — the SQLite write lock would race
  itself).

- **Identity is a URI, not a hostname:** `outrelay://<tenant>/agent/<uuid>`
  is the URI SAN on every agent's leaf certificate. Policy patterns
  match against the URI; the relay never trusts a self-reported name.

If this is the level of detail you needed, head to
[`local-cluster.md`](local-cluster.md) and run the system. Otherwise
[`concepts.md`](concepts.md) goes one layer deeper into ORP, resume,
and P2P promotion.
