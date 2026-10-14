# qa-bash port (Plan 00192 Task 3.3), branch go-port/qa-bash

Spec: [SPEC-qa-bash.md](../SPEC-qa-bash.md), expectations S1-S15, departures B1-B12. Code:
`src-go/qa/bash` and `src-go/qa/cmd/qa-bash`; `scripts/qa-bash.bash` is a build-and-run wrapper and
its allowlist row is gone.

- Engines stay: `bash -n` (one process per file, merged output, status 2 is a syntax error, any
  other status is exit 2) and one shellcheck process for all files.
- shellcheck is pinned: `--norc --external-sources --format json --`, an empty `SHELLCHECK_OPTS`,
  run from the root with relative paths. This closes the same class of hole as qa-python's ruff
  `--config`: a nested `.shellcheckrc` or the environment could disable checks.
- Scope is the `shell` list of `qa/discovery`; the old coverage guard stays as the `tracked-shell`
  yardstick (exit 2 naming the missed scripts).
- Gating is error and warning; info and style are the advisory line. A file with a gating finding
  is now a failure with its codes (the bash left `failures: []`), and the failure count is files.
- qa-changed: new `bash` check (tool, discovery, wrapper or trees table change), its per-file
  shellcheck given the same `--norc` and empty `SHELLCHECK_OPTS` (SPEC-qa-changed B35; 18 golden
  deviations re-pinned).
- Live tree: same 433 files as the old bash, exit 0 both; 167 against 162 shellcheck findings, the
  5 missing being SC1091 (info) resolved by `--external-sources`; all else identical.

Open point for the reviewer: the unpinned shellcheck version is left to `qa-toolchain` (S4 states
the limit); `.qa-versions` already holds `SHELLCHECK=`.
