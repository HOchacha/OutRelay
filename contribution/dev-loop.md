# Development loop

Day-to-day commands for working on the controller and shared library
without a cluster.

## Toolchain

- Go 1.25+ (`go version` to confirm)
- `protoc` for `make proto` (the Go plugins install themselves)
- `docker` (or compatible) for image targets
- `kubectl` only when you need an end-to-end demo
  (see [`../getting-started/local-cluster.md`](../getting-started/local-cluster.md))

`golangci-lint` and `gosec` are installed automatically by their
respective `make` targets on first run; they end up in `$(go env GOPATH)/bin`.

## Common commands

| Command          | What it does                                                                 |
|------------------|------------------------------------------------------------------------------|
| `make`           | Default target: proto regen → gofmt check → golangci-lint → gosec → `go build` |
| `make test`      | `go test -race -count=1 ./...` (all tests, no caching, race detector on)    |
| `make gofmt`     | Fail on `gofmt` drift; print the offending files. Fix with `gofmt -w .`.    |
| `make golangci-lint` | Run `golangci-lint`. Settings in [`.golangci.yml`](../.golangci.yml).    |
| `make gosec`     | Static security scan via `gosec`.                                           |
| `make proto`     | Regenerate `lib/{orp,control}/v1/*.pb.go` from `api/`.                      |
| `make build-image` | `docker build` → `$(IMAGE):$(TAG)` *and* `$(IMAGE):latest`. Defaults: `IMAGE=docker.io/boanlab/outrelay-controller`, `TAG=v0.1.0`. |
| `make dev-pki`   | One-shot CA + leaf certs in `./.dev-pki/` (do **not** commit).              |
| `make clean`     | Remove `bin/`, `dist/`, `.dev-pki/`, generated `*.pb.go`.                   |
| `make help`      | Print the target list with descriptions.                                    |

## A typical change

```bash
# 1. Branch off main.
git switch -c feat/policy-method-glob

# 2. Make the change. Edit code, add or update tests.

# 3. Iterate fast. Run only the package you changed.
go test -race -count=1 ./pkg/policy/...

# 4. Before committing, run the full gate.
make            # fmt + lint + sec + build
make test       # full test suite

# 5. Commit and push. See commits-and-prs.md for the format.
```

## Running the controller without a cluster

You can run the controller binary against an in-memory SQLite
database for end-to-end manual testing on your workstation:

```bash
make build
./bin/outrelay-controller \
  --listen=127.0.0.1:7444 \
  --db=:memory: \
  --debug-listen=127.0.0.1:9101 \
  --log-format=json
```

In another shell, exercise it with the CLI:

```bash
./bin/outrelay-cli policy add \
  --controller=127.0.0.1:7444 \
  --tenant=acme --caller='*' --target='*' --decision=allow

./bin/outrelay-cli policy list --controller=127.0.0.1:7444 --tenant=acme

./bin/outrelay-cli audit query --controller=127.0.0.1:7444 --tenant=acme
```

The debug HTTP endpoint exposes:

- `GET /debug/metrics` — full registry snapshot as JSON
- `GET /debug/pprof/*` — standard `net/http/pprof` handlers

It binds to `127.0.0.1:9101` by default and is **never** exposed
inside a cluster (the manifest sets `--debug-listen=` to disable it
in production).

## Working on the proto schemas

After editing anything under `api/`, regenerate the Go bindings:

```bash
make proto
```

The output ends up in `lib/orp/v1/` and `lib/control/v1/`. Generated
files are committed (so consumers don't need `protoc`); make sure
your PR includes the regen alongside the schema change.

The license-header workflow ignores generated `.pb.go` files
([`.licenserc.yaml`](../.licenserc.yaml) → `paths-ignore`), so you
don't need to add the SPDX header to them by hand.

## Shipping an image

The `release.yml` workflow builds and pushes on tag push. To dry-run
a release locally:

```bash
make build-image TAG=v0.1.0-rc1
make push-image  TAG=v0.1.0-rc1         # requires docker login first
```

Both targets always produce `:$(TAG)` *and* `:latest`. The
`-ldflags '-X main.Version=$(TAG)'` baked in by `GO_BUILD` (and the
`--build-arg VERSION=$(TAG)` to docker) means
`outrelay-controller --version` and `outrelay-cli version` print the
exact tag the binary was built from.

See [`releasing.md`](releasing.md) for the cut process.
