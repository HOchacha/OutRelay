# tests/terraform

Cloud smoke tests. Provisions a real AWS topology, runs assertions
that cannot be exercised locally, and tears everything down on the
way out — pass or fail. Cost per run is below USD 0.10 at typical
AWS pricing (≤ 15 minutes of t3 spot + one NAT GW hour).

## Scope

These tests cover only the things local Go tests cannot:

- real outbound-only enforcement at the cloud network layer (VPC SG
  with no inbound rule)
- real AWS NAT Gateway behaviour (symmetric NAT) against P2P
  promotion
- EC2 systemd VM-daemon deployment shape
- multi-VPC = multi-cluster realism that local netns cannot fake

ORP frame correctness, stream-resume algorithm correctness, policy
matching, and resume internals are already covered by Go unit and
package integration tests in the three sibling repos. We do not
re-validate those here.

## Three configurations

| Path | Coverage | When |
|---|---|---|
| `aws-only/` | full ORP path + outbound-only + relay failover + control-plane outage + P2P negative + P2P direct-dial positive + policy deny + TCP/443 fallback + mini-TURN forward plane | runnable today |
| `aws-gcp/` | full ORP path + true hole-punch through symmetric ↔ GCP Cloud NAT (EIM and EDM variants) | requires GCP credentials |
| `aws-multi-region/` | multi-relay + nearest-relay routing + region-aware Resolve + inter-relay forwarding | runnable today |

The AWS-only path is the day-to-day smoke (cheapest, most coverage).
`aws-gcp` is the only configuration that exercises hole-punching
through CSP NAT — including the GCP Cloud NAT EIM filtering finding
where `endpoint_independent_mapping=true` does *not* deliver
inbound full-cone filtering. `aws-multi-region` is the cross-region
multi-relay test (two relays in different regions plus inter-relay
forwarding).

## Binary distribution

Each daemon is a static Go binary (`CGO_ENABLED=0`). The harness
cross-compiles `linux/amd64` locally, uploads them to a per-run S3
bucket alongside the dev PKI output, and EC2 instances pull from
that bucket via an IAM instance profile scoped to the bucket only.
SSM Session Manager handles all remote shell — no SSH keys, no
bastion, no inbound port. The bucket has `force_destroy = true` and
a 1-day lifecycle rule as a fallback.

## Auto-destroy (multi-layer defense)

1. **Trap in `run.sh`** — `terraform destroy -auto-approve` runs on
   EXIT/INT/TERM whether assertions passed or failed.
2. **Tag-based lifecycle** — every resource carries
   `owner=outrelay-smoke` + `expires-at=<unix-ts>`.
   `scripts/cleanup-stale.sh` (run from cron on a separate machine)
   reaps anything past `expires-at`.
3. **Pre-flight stale check** — `run.sh` aborts if it sees prior
   `owner=outrelay-smoke` resources, forcing a manual review.
4. **AWS Budgets** (operator's responsibility) — set a daily cap
   alarm on the AWS account used here.

## Prerequisites

- AWS credentials with EC2/VPC/IAM/S3/SSM permissions
- Terraform ≥ 1.6
- Go ≥ 1.25 (for the local cross-compile step)
- `aws` CLI ≥ 2.x
- `jq`

## Usage

```bash
# Build, apply, run assertions, destroy. Roughly 13 minutes.
make smoke-aws

# Same for the AWS+GCP topology (requires GOOGLE_APPLICATION_CREDENTIALS).
make smoke-aws-gcp

# Manual escape hatches.
make destroy-aws
make destroy-aws-gcp
make cleanup-stale          # remove tagged resources past expires-at

# Static checks without standing anything up.
make validate
make fmt
```

## What gets verified

`assertions/` contains one shell script per check, run via
`aws ssm send-command` against the deployed EC2s. The harness
reports per-assertion pass/fail and exits non-zero if any failed.

| # | Assertion | aws-only | aws-gcp | aws-multi-region |
|---|---|---|---|---|
| 00 | controller starts closed-world; add allow-all policy | ✓ | ✓ | ✓ |
| 01 | consumer → relay → provider HTTP 200 | ✓ | ✓ | — |
| 02 | provider SG inbound deny → still reachable via relay | ✓ | ✓ | — |
| 03 | relay killed mid-stream → stream resumes on reconnect | ✓ | ✓ | — |
| 04 | controller killed → in-flight stream survives | ✓ | ✓ | — |
| 05 | both behind NAT GW → P2P fails → stays on relay | ✓ | ✓ | — |
| 06 | provider has EIP → P2P direct-dial succeeds → MIGRATE_TO_P2P | ✓ | — | — |
| 07 | policy `decision=deny` → stream rejected | ✓ | ✓ | — |
| 08 | true hole-punch through symmetric ↔ EIM NAT | — | ✓ | — |
| 09 | TCP/443 fallback when QUIC dial fails | ✓ | — | — |
| 11 | inter-relay forwarding: provider on a peer relay (cross-region) | — | — | ✓ |
| 12 | mini-TURN forwarding plane round-trip (`relay_mode=forward`) | ✓ | — | — |

## Layout

```
tests/terraform/
├── README.md                  # this file
├── Makefile                   # smoke-aws, smoke-aws-gcp, validate, ...
├── scripts/
│   ├── run.sh                 # trap-based harness
│   ├── lib.sh                 # SSM send-command helpers
│   ├── preflight.sh           # creds + stale check
│   ├── build-binaries.sh      # cross-compile linux/amd64
│   ├── wait-ready.sh          # wait for systemd units healthy
│   └── cleanup-stale.sh       # Layer 2 safety net
├── modules/
│   ├── vpc/                   # public-only OR public+private+NAT
│   ├── control-host/          # EC2 running controller + relay
│   ├── agent-host/            # EC2 running outrelay-agent
│   └── artifact-bucket/       # per-run S3 bucket + uploads
├── assertions/
│   └── *.sh                   # one per check
├── aws-only/                  # root module — runnable today
├── aws-gcp/                   # root module — GCP credentials required
└── aws-multi-region/          # root module — two AWS regions, two relays
```
