# Troubleshooting

When OutRelay misbehaves, the symptom is almost always one of the
five cases below. Work through them in this order.

## 1. The controller pod won't come up

```bash
kubectl -n outrelay describe pod -l app.kubernetes.io/component=controller
kubectl -n outrelay logs -l app.kubernetes.io/component=controller
```

Common causes:

- `ImagePullBackOff` — the
  `docker.io/boanlab/outrelay-controller:latest` image isn't in the
  cluster's runtime. Re-run `kind load docker-image …` or the
  containerd `ctr import` step from
  [`local-cluster.md`](local-cluster.md).
- `permission denied` on `/data/outrelay-controller.db` — the pod
  runs as UID 65532, but the volume's mount mode disagrees. The
  default `emptyDir` works; if you swapped in a PVC, set `fsGroup:
  65532` on the pod spec (the manifest already does).

## 2. `make build-image` fails

- `protoc not found` during `make proto` — install `protoc` from
  your package manager. The Go plugins (`protoc-gen-go`,
  `protoc-gen-go-grpc`) install themselves on first run.
- `gofmt drift` — run `gofmt -w .` and re-run `make`. Don't bypass
  the gate; CI runs the same check.

## 3. The relay connects but every stream is denied

The controller starts **closed-world**: with no policy rules, every
`OPEN_STREAM` is denied. Add a rule (see
[`local-cluster.md`](local-cluster.md) §4) and confirm with
`outrelay-cli policy list --tenant=acme`.

If the rule is in place but streams are still denied, run
`outrelay-cli audit query` and look at the `reason` column on the
deny event — it tells you which pattern failed to match.

## 4. The consumer agent connects but immediately disconnects

Likely an mTLS or URI SAN mismatch. Check:

```bash
openssl x509 -in .dev-pki/agent-consumer.crt -noout -text \
  | grep -A1 'Subject Alternative Name'
```

The URI SAN must read `outrelay://<tenant>/agent/<uuid>` and match
the `--uri` flag the consumer manifest passes. If you regenerated
the PKI, re-apply `secrets.yaml` and bounce the agent pod.

## 5. Streams open but data never arrives

Something is wrong with the splice. Tail the relay log and look for
a single ID across both halves:

```bash
kubectl -n outrelay logs deploy/outrelay-relay -f \
  | jq -c 'select(.stream_id == "<id from consumer>")'
```

If you see only the consumer half, the matcher couldn't find the
provider — check `outrelay-cli policy list` (deny rule?) and
`Resolve` results (registered service?). If you see both halves but
no data, the splice is fine and the issue is in the application
above ORP.

For deeper debugging, run `tools/correlate` over JSONL dumps from
all three components — it groups events by `stream_id` so you can
see exactly when each side opened, accepted, and FIN'd.
