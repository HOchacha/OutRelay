# Releasing

OutRelay follows a tag-driven release flow. Pushing a `v*` tag to
`main` runs the release workflow, which gates on the same checks as
CI and pushes the controller image to Docker Hub.

## Workflow

[`.github/workflows/release.yml`](../.github/workflows/release.yml) runs:

1. `make gofmt`
2. `make golangci-lint`
3. `make gosec`
4. `make test`
5. **Push image** — `docker login` with the `DOCKERHUB_USERNAME` /
   `DOCKERHUB_TOKEN` repo secrets, then
   `make TAG=${{ github.ref_name }} push-image`.

The image lands at `docker.io/boanlab/outrelay-controller:<tag>`,
and the same build is also pushed as
`docker.io/boanlab/outrelay-controller:latest`. The git tag is
linked into the binary via `-ldflags '-X main.Version=...'`, so
`outrelay-controller --version` (or `outrelay-cli version`) prints
the exact release.

## Cutting a release

```bash
# 1. Make sure main is green and at the commit you want to ship.
git switch main && git pull --ff-only

# 2. Tag.
git tag -a v0.1.0 -m "v0.1.0 — first cut of the controller + shared lib"

# 3. Push the tag (this is what triggers the workflow).
git push origin v0.1.0
```

Watch the run in the Actions tab; if any gate fails, fix on `main`
and push a new patch tag (`v0.1.1`). Don't move tags.

## Versioning

We follow semver, but the project is pre-1.0:

- Breaking wire / API changes can land in `v0.x.y` minor bumps.
- Once we ship `v1.0.0`, breaking changes require a major bump and
  the previous major is supported for at least one cycle.

The `lib/` directory is the public Go API for the relay and agent
repositories. Field renames in `*.pb.go` are wire-breaking; adding
fields is wire-compatible. Removing a proto field number is never
allowed.

## What does NOT go in a release

- The `tools/dev-pki` output (`./.dev-pki/`) — dev only, never
  shipped.
- Local `bin/` artifacts — gitignored.
- CHANGELOG entries — we read `git log` for now. If you want a
  curated CHANGELOG, propose it in an issue first.

## Rolling back

If a release ships a regression, we ship a new patch release with
the fix. We don't yank tags.
