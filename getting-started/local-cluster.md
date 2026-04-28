# Local cluster walk-through

This guide deploys the controller, one relay, and two agents
(provider + consumer) onto a local Kubernetes cluster and watches a
single stream round-trip end-to-end.

You will need:

- a local Kubernetes cluster (kind, k3d, minikube, or any existing
  cluster that can pull local images);
- `kubectl` pointing at it;
- `make`, `docker`, and Go 1.25+ on your workstation.

The relay and agent live in sibling repositories. Clone all three
next to each other so the `go.mod` `replace` directives in the
relay / agent can pick up your local controller checkout:

```bash
mkdir -p ~/src/boanlab && cd ~/src/boanlab
git clone https://github.com/boanlab/OutRelay.git
git clone https://github.com/boanlab/outrelay-relay.git
git clone https://github.com/boanlab/outrelay-agent.git
```

## 1. Build the images

Each `make build-image` tags both `:$(TAG)` and `:latest`; the
deployment manifests pull `:latest` so you don't have to template
the tag. Default `TAG` is `v0.1.0`.

```bash
make -C OutRelay         build-image    # docker.io/boanlab/outrelay-controller:{v0.1.0,latest}
make -C outrelay-relay   build-image    # docker.io/boanlab/outrelay-relay:{v0.1.0,latest}
make -C outrelay-agent   build-image    # docker.io/boanlab/outrelay-agent:{v0.1.0,latest}
```

Load them into the cluster's runtime. For containerd:

```bash
for img in docker.io/boanlab/outrelay-{controller,relay,agent}:latest; do
  docker save "$img" | sudo ctr -n=k8s.io images import -
done
```

For kind: `kind load docker-image docker.io/boanlab/outrelay-controller:latest`
(repeat per image).

## 2. Generate dev PKI

```bash
make -C OutRelay dev-pki
```

This produces `./.dev-pki/` with:

- a fresh self-signed CA (10-year TTL, ECDSA P-256);
- a relay leaf cert and two agent leaf certs (30-day TTL — dev
  default, vs. 1 h with rotation in production);
- `secrets.yaml`, a multi-document `v1.Secret` manifest carrying
  `tls.crt` / `tls.key` / `ca.crt` for each leaf, namespaced to
  `outrelay` (override with `go run ./tools/dev-pki -namespace=...`).

The agent UUIDs are fixed
(`00000000-0000-0000-0000-000000000001` for provider,
`...0002` for consumer) so the manifests can pre-bake `--uri`
flags without templating.

> The `.dev-pki/` directory contains private keys. It is gitignored
> and must not be committed.

## 3. Apply the manifests

```bash
kubectl apply -f OutRelay/deployments/00-namespace.yaml
kubectl apply -f .dev-pki/secrets.yaml
kubectl apply -f OutRelay/deployments/10-outrelay-controller.yaml
kubectl apply -f outrelay-relay/deployments/20-relay.yaml
kubectl apply -f outrelay-agent/deployments/30-provider.yaml
kubectl apply -f outrelay-agent/deployments/40-consumer.yaml
```

Confirm everything is `Running`:

```bash
kubectl -n outrelay get pods
```

## 4. Add a wildcard ALLOW policy

The controller starts closed-world. Add one rule so the demo
consumer can reach the demo provider:

```bash
kubectl -n outrelay run cli --rm -i --restart=Never \
  --image=docker.io/boanlab/outrelay-controller:latest \
  --image-pull-policy=IfNotPresent \
  --command -- /usr/local/bin/outrelay-cli policy add \
    --controller=outrelay-controller.outrelay.svc.cluster.local:7444 \
    --tenant=acme --caller='*' --target='*' --decision=allow
```

You should see a policy id printed.

## 5. Watch a stream round-trip

Tail the consumer's app sidecar:

```bash
kubectl -n outrelay logs deploy/outrelay-agent-consumer -c app -f
```

You should see one round-trip per `echo` cycle. To inspect what the
controller saw:

```bash
kubectl -n outrelay run cli --rm -i --restart=Never \
  --image=docker.io/boanlab/outrelay-controller:latest \
  --image-pull-policy=IfNotPresent \
  --command -- /usr/local/bin/outrelay-cli audit query \
    --controller=outrelay-controller.outrelay.svc.cluster.local:7444 \
    --tenant=acme --limit=20
```

Each row is one `OPEN_STREAM` decision (caller URI, target service,
decision, reason, stream id).

## Troubleshooting

If anything stalls, see [`troubleshooting.md`](troubleshooting.md).
