# Contributing to OutRelay

We welcome bug reports, design discussions, and code contributions.
This document is the short version of how we work.

## Before you start

1. **Open an issue first** for anything beyond a typo or a one-line
   fix. A short description of the problem and the change you want to
   make saves both of us from wasted work.
2. **Check the scope.** This repository is the controller and the
   shared library. Relay-specific or agent-specific changes belong in
   the [`outrelay-relay`](https://github.com/boanlab/outrelay-relay)
   or [`outrelay-agent`](https://github.com/boanlab/outrelay-agent)
   repository.
3. **Skim [`code-style.md`](code-style.md)** so the lint / format
   gates don't bounce your first PR.

## License and DCO

All code in this repository is Apache 2.0 — see [`../LICENSE`](../LICENSE).

Every source file must carry the SPDX header (the
`make` targets don't add it for you):

```
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University
```

The license header CI job
([`.github/workflows/license.yml`](../.github/workflows/license.yml))
uses `apache/skywalking-eyes` to enforce this; configuration is in
[`.licenserc.yaml`](../.licenserc.yaml).

By submitting a pull request you assert that you have the right to
license your contribution under Apache 2.0.

## What a good change looks like

- **One concern per PR.** Refactors and behaviour changes ride on
  separate commits, ideally separate PRs.
- **Tests.** New behaviour comes with a test that fails before the
  change and passes after. Bug fixes come with a test that locks the
  bug down. The test commands you'll run are in
  [`dev-loop.md`](dev-loop.md).
- **No drive-by reformatting.** Touch only the lines your change
  needs.
- **No drive-by dependencies.** Adding a Go module is a separate
  conversation; open an issue first.

## Reporting bugs

For a confirmed bug, open an issue with:

- the version (commit hash) you're running;
- what you did, what you expected, what actually happened;
- enough log output to identify the failing path — JSONL output from
  `--log-format=json` and the relevant `stream_id` are ideal;
- a minimum reproducer if you have one.

If the issue is a security vulnerability, do **not** open a public
issue. Email the maintainers (see the repo's `CODEOWNERS`-equivalent
or the BoanLab homepage) with the same content. We'll coordinate a
fix and a coordinated disclosure window.

## Reporting a vulnerability you found by accident

If you found something while running the system, please give us at
least 14 days to ship a fix before disclosing. The PKI, audit, and
policy paths are the highest-risk surface; please call those out
explicitly.

## Code of conduct

Be kind, be specific, assume good faith. We follow the
[Contributor Covenant 2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/);
maintainers will moderate accordingly.
