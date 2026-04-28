# Commits and pull requests

## Commit messages

Use a short imperative subject and a body that says **why**.

```
policy: validate caller_pattern at AddPolicy

Previously the controller accepted any non-empty string as
caller_pattern, which led to silent denials at the relay because the
pattern compiled to a regex that matched nothing. Validate at write
time so operators get an error from `outrelay-cli policy add` instead
of debugging an empty audit trail.
```

Rules of thumb:

- Subject under 70 characters, no trailing period.
- Prefix with the package or area when natural: `policy:`, `pki:`,
  `observe:`, `docs:`, `ci:`.
- Wrap the body at 72 chars. Reference the issue ID at the end if
  you have one (`Fixes #123.`).
- One concern per commit. If the diff doesn't fit one subject line,
  split it.

## Pull requests

A good PR description has three parts:

1. **What changed.** One paragraph, plain English.
2. **Why.** What problem this solves, or what design choice it
   makes. Link the issue if any.
3. **How to verify.** The commands a reviewer can run on their
   workstation. `make test` is rarely enough on its own — say
   "added a test that fails before and passes after," or "ran the
   local-cluster walk-through and confirmed audit shows the new
   reason field."

Keep the diff small. PRs over ~400 lines of change should be split
unless the change is mostly mechanical.

## Review

- Reviewers will run `make` and `make test` locally if anything
  looks off; CI will run the same gates plus `gosec` and the license
  header check.
- Reviewers can request changes; please push fixups to the same
  branch (don't force-push during active review unless asked, so the
  reviewer can see incremental diffs).
- Once everyone's happy, a maintainer squash-merges. The squashed
  message uses the PR title as subject and the PR body as body — so
  treat the PR description as your final commit message.

## CI gates

These run on every PR ([`.github/workflows/ci.yml`](../.github/workflows/ci.yml)):

- `make gofmt`
- `make golangci-lint`
- `make gosec`
- `make test`
- `make build-image` (with `TAG=tmp` on PRs, `TAG=latest` on `main`)
- License header check ([`.github/workflows/license.yml`](../.github/workflows/license.yml))

If a gate fails, please fix the underlying issue rather than
suppressing the warning. If you genuinely need a suppression, leave
a `// #nosec G<NNN> -- <reason>` comment that a reviewer can defend.

## Backporting and hotfixes

Hotfixes go straight to `main`; we tag and release from there. There
are no maintenance branches today.
