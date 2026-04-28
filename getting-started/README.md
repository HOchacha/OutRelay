# Getting started

Documents in this directory are aimed at someone meeting OutRelay for
the first time. Read them in this order:

1. [`overview.md`](overview.md) — what problem OutRelay solves and the
   shape of the solution.
2. [`architecture.md`](architecture.md) — the three components
   (controller, relay, agent), how they talk, and which repository
   each one lives in.
3. [`concepts.md`](concepts.md) — identity, ORP frames, policy,
   resume, and P2P promotion explained one at a time.
4. [`local-cluster.md`](local-cluster.md) — a step-by-step walk
   through running the full system on a local Kubernetes cluster
   (kind, k3d, or minikube).
5. [`troubleshooting.md`](troubleshooting.md) — common errors and how
   to read them.

If you just want to run the tests and poke at the controller without
a cluster, jump to [`../contribution/dev-loop.md`](../contribution/dev-loop.md).
