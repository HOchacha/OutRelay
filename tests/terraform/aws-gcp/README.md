# aws-gcp/

Cross-cloud variant of the smoke topology. AWS NAT Gateway is
symmetric; GCP Cloud NAT can be configured with
`enable_endpoint_independent_mapping=true` (EIM). Putting one agent
behind each is the configuration the assertion suite uses to
characterise CSP-NAT behaviour against the agent's P2P-promotion
path.

## What it actually shows

The headline assertion — **08**: cross-cloud hole-punching from
symmetric NAT (consumer on AWS) toward GCP Cloud NAT with EIM
enabled (provider) — fails to hole-punch. The relay path remains
live and serves the request, but a direct dial against the
srflx-discovered GCP external endpoint times out cleanly.

The reason is on the GCP side, not the agent: GCP's EIM toggle
controls **mapping** but the **filtering** stays endpoint-dependent.
A blind inbound dial from a peer the host has never sent to is
dropped. Documentation that reads "EIM = full cone" is misleading.

This negative finding is itself a contribution and sets a hard
floor on the CSP-NAT cells in the planned cross-cloud measurement.

## Prerequisites

```bash
gcloud auth application-default login        # or set GOOGLE_APPLICATION_CREDENTIALS
gcloud config set project <project-id>
```

Run with `make smoke-aws-gcp`.

## Variants exercised

| Variant | Provider side | Hole-punch | Relay |
|---|---|---|---|
| B1 | GCP public IP | direct dial succeeds | ✓ |
| B2 | GCP Cloud NAT (EIM enabled) | drop (EDF filtering) | ✓ |

Both variants verify that the relay path remains live regardless of
the P2P outcome — the fallback property the architecture promises.
