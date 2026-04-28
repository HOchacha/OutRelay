# Overview

## What problem does OutRelay solve?

Most service-to-service connectivity stories assume that *somebody*
can open an inbound port: a Kubernetes Service backed by a
LoadBalancer, an internal ALB, a VPN gateway, a service mesh sidecar
that listens on a pod IP. That assumption breaks down when:

- the provider runs on a workstation, a CI runner, an edge gateway,
  or behind a CGNAT;
- the consumer is in a different cloud or VPC than the provider, with
  no VPN between them;
- the security review forbids opening any inbound port at all on the
  provider's network;
- the team does not (and will not) run a Kubernetes cluster on the
  provider side.

In all those cases the only reliable channel is **outbound**. OutRelay
is built around that fact: every component dials *out* to a relay,
nothing accepts inbound traffic except the relay itself.

## What is OutRelay, in one sentence?

OutRelay is a small **stateless QUIC relay** that splices two
outbound mTLS connections — one from a consumer-side agent and one
from a provider-side agent — into a single bidirectional byte stream,
governed by an out-of-band **controller** that owns identity, service
registry, policy, and audit.

## What it is NOT

- **Not a service mesh.** No sidecar mTLS to your existing pods, no
  L7 retries, no traffic mirroring.
- **Not a tunnel like WireGuard or Tailscale.** OutRelay is a
  per-stream relay, not an L3/L4 tunnel; there is no network
  interface in your namespace.
- **Not a managed product.** This repository is the open-source
  reference implementation; you run all three components.
- **Not a load balancer.** Each registered service has one provider
  agent at a time today; replication is a future-work item.

## When is OutRelay the wrong tool?

- You already have flat L3 between consumer and provider — use plain
  Kubernetes Services or a service mesh.
- You need true peer-to-peer with NAT traversal that is bulletproof
  on day one — OutRelay opportunistically promotes streams to P2P
  but always keeps the relay path available, so you pay for the
  relay even on the happy path.
- You need fan-out / pub-sub semantics — OutRelay streams are
  point-to-point.

If none of those apply, [`architecture.md`](architecture.md) walks
through what each component actually does.
