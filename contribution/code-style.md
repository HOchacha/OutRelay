# Code style

The mechanical rules are enforced by `make`. The judgement calls
below aren't, but they make review go faster.

## Mechanical rules (enforced)

- **`gofmt`** — `make gofmt` must pass. Use `gofmt -w .` to fix.
- **`golangci-lint`** — settings in [`.golangci.yml`](../.golangci.yml).
  Errcheck is relaxed for `Close()`-style cleanup (where checking
  the error gives no signal) and for `fmt.Fprint*` writes; everything
  else is on with default severity.
- **`gosec`** — static security scan. If you have to suppress, leave
  a `// #nosec G<NNN> -- <one-line reason>` comment. Don't suppress
  silently.
- **License header** — every Go, Makefile, and Dockerfile source
  file starts with:
  ```
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 BoanLab @ Dankook University
  ```
  Generated `*.pb.go` files are exempt (see
  [`.licenserc.yaml`](../.licenserc.yaml)).

## Conventions we ask reviewers to enforce

### Comments

- Comment **why**, not what. The code already says what it does.
- Lead with one sentence. If you need more, follow with a short
  paragraph.
- Don't reference external design-doc section numbers — link to a
  function, file, or concept the reader can actually open.
- Don't narrate the change history (`// added for X`, `// was Y
  before`). That belongs in the commit message and rots in code.
- Don't write multi-paragraph docstrings on internal helpers.

### Naming

- Exported names use Go conventions (`UpperCamelCase`,
  receiver-as-method-name). No Hungarian notation.
- Errors are `ErrSomething`; sentinel errors get `errors.New(...)`,
  contextual errors get `fmt.Errorf("pkg: ...: %w", err)`.
- Test functions follow `TestThing_Behaviour` only when there's a
  natural prefix. `TestEnrollerSingleUse` is fine.

### Errors

- Wrap with `%w` so callers can `errors.Is` your sentinels.
- Always include a package prefix in the message: `"identity: parse: ..."`.
- Don't swallow errors. If you have to (best-effort cleanup), use
  `_ = thing.Close()` so the lint suppression is visible.

### Concurrency

- Public types document their concurrency contract in the type
  doc-comment (`goroutine-safe`, `not goroutine-safe — caller
  serializes`, `safe for one writer + many readers`).
- Use `sync/atomic` for hot counters. Use `sync.Mutex` for
  multi-field invariants.
- Don't add new goroutines without a clear shutdown path tied to a
  `context.Context`.

### Tests

- Use `t.Parallel()` unless the test mutates global state.
- Prefer table-driven tests for cases that vary only by input.
- Use `t.Context()` (not `context.Background()`) so the test's
  context is cancelled automatically on completion.
- Integration-flavoured tests (full mTLS handshake, real SQLite file)
  belong in `*_test.go` files alongside the package; we don't have a
  separate integration suite.

### Public API

- Generated `.pb.go` files are the wire-level public API. Renaming a
  field is a breaking change; adding a field is fine. Removing a
  field number is forbidden.
- Anything in `lib/` is imported by the relay and agent
  repositories. Adding a method is fine; renaming or removing one is
  a coordinated change that needs to land in this repo first.

## Things we deliberately don't do

- We don't pull in OpenTelemetry, Prometheus client libs, or any
  observability stack — `lib/observe` is the surface, by design.
- We don't add abstractions for "future flexibility." When the
  second consumer arrives, refactor.
- We don't write multi-paragraph package doc-comments. One
  paragraph that says what the package is for is enough.
