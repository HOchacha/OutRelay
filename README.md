# OutRelay

**OutRelay** is a platform-agnostic, outbound-only relay that connects
services across clouds and runtimes without inbound port openings, VPN
tunnels, or a Kubernetes dependency on every endpoint. Agents dial out
to a stateless relay over mTLS QUIC, and the relay splices the two
halves of each call together in user space.

This repository hosts the **controller**, the **operator CLI**, and
the **shared library** that the runtime data plane links against. The
runtime is split across two sibling repositories:

- [`outrelay-relay`](https://github.com/boanlab/outrelay-relay) —
  stateless QUIC relay, zero per-tenant state.
- [`outrelay-agent`](https://github.com/boanlab/outrelay-agent) —
  per-workload sidecar / VM agent.

To get going, read [`getting-started/`](getting-started/). To
contribute, read [`contribution/`](contribution/).

## How OutRelay works (one paragraph)

A consumer agent and a provider agent both dial out to the same relay
on QUIC port 7443 with their own X.509 leaf cert. The relay
authenticates each agent against its `outrelay://<tenant>/agent/<uuid>`
URI SAN, calls the controller to register the provider's service, and
matches incoming OPEN_STREAM frames to a registered provider. Once the
provider accepts, the relay enters splice mode and copies bytes
between the two QUIC streams without parsing them. Policies, audit
events, and identity issuance live on the controller; the relay only
knows what the controller's gRPC API tells it.

```
            ┌───────────────────┐
            │   controller      │  gRPC :7444
            │ Registry / Policy │   (Registry, Policy, Audit)
            │   Audit / PKI     │   SQLite-backed
            └─────▲─────────▲───┘
                  │ Watch   │ Resolve / Record
                  │         │
            ┌─────┴─────────┴───┐
            │      relay        │   QUIC :7443  (mTLS, ALPN orp/1)
            │ stateless splice  │
            └─────▲─────────▲───┘
       outbound  │         │  outbound
              ┌──┴──┐    ┌─┴──┐
              │agent│    │agent│
              │  C  │    │  P  │
              └─────┘    └─────┘
```

## Repository layout

```
api/                       # protobuf sources (generated into lib/*/v1)
  orp/v1/                  # ORP wire frames (control + data)
  control/v1/              # Registry, Policy, Audit gRPC services

lib/                       # shared library, imported by relay and agent
  orp/                     # ORP frame codec + per-stream FSM
  transport/               # QUIC abstraction (mTLS, ALPN orp/1, keepalives)
  identity/                # outrelay:// URI parsing + cert rotator
  observe/                 # in-process metrics + JSONL dump + /debug/*
  resume/                  # stream id, ring buffer, per-stream state

pkg/                       # controller-only
  registry/                # Registry gRPC server, slow-consumer-tolerant Watch
  registry/store/          # SQLite schema + queries (modernc.org/sqlite)
  policy/                  # Policy gRPC server (add/remove/list/Watch)
  audit/                   # Audit gRPC server (Record/Query)
  pki/                     # self-signed mini-CA + enrollment-token issuer

cmd/
  outrelay-controller/     # gRPC server hosting Registry, Policy, Audit
  outrelay-cli/            # operator CLI: policy add/list/remove, audit query

tools/
  correlate/               # JSONL log correlator: groups events by stream_id
  dev-pki/                 # one-shot CA + leaf certs + Secret YAML (dev only)

deployments/               # Namespace + controller manifests for Kubernetes
getting-started/           # quickstart + concepts for first-time users
contribution/              # how to build, test, and submit changes
```

## Quickstart

The deployment under `deployments/` ships a single controller in the
`outrelay` namespace. The relay and agent manifests live in their own
repositories — see [`getting-started/local-cluster.md`](getting-started/local-cluster.md)
for the full walk-through.

```bash
# 1. Build the controller image and load it into the cluster runtime.
#    Produces both docker.io/boanlab/outrelay-controller:$(TAG) and :latest.
make build-image                     # default: TAG=v0.1.0

# 2. Generate dev PKI (CA + relay leaf + 2 agent leaves + Secret YAML).
make dev-pki                         # writes ./.dev-pki/ — never commit

# 3. Apply manifests.
kubectl apply -f deployments/00-namespace.yaml
kubectl apply -f .dev-pki/secrets.yaml
kubectl apply -f deployments/10-outrelay-controller.yaml

# 4. Add a wildcard ALLOW policy (controller starts closed-world).
kubectl -n outrelay run cli --rm -i --restart=Never \
  --image=docker.io/boanlab/outrelay-controller:latest \
  --image-pull-policy=IfNotPresent \
  --command -- /usr/local/bin/outrelay-cli policy add \
    --controller=outrelay-controller.outrelay.svc.cluster.local:7444 \
    --tenant=acme --caller='*' --target='*' --decision=allow
```

## Build

```bash
make                # default: proto -> gofmt -> golangci-lint -> gosec -> go build
make test           # go test -race -count=1 ./...
make build-image    # docker build -> $(IMAGE)
make push-image     # docker push $(IMAGE)
make proto          # regenerate lib/{orp,control}/v1/*.pb.go
make dev-pki        # generate ./.dev-pki/ for local cluster validation
make help           # quick reference
```

`golangci-lint` and `gosec` install themselves on first run. The
image registry and tag are overridable:
`make build-image TAG=v0.1.0` or
`IMAGE_REGISTRY=mirror.example/boanlab make push-image`. Every
`build-image` produces both `:$(TAG)` (defaults to `v0.1.0`) and
`:latest`; `push-image` pushes both.

### Required toolchain

- Go 1.25+
- `docker` (or compatible) for image targets
- `protoc` for `make proto` (the Go plugins are installed automatically)

## License

Apache 2.0 — see [`LICENSE`](LICENSE).
