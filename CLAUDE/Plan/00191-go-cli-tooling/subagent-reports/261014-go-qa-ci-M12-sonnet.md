# go-qa-ci M12: tools.lock, install-tools.sh, CI, declaration, CONFORMANCE, README

## Built

- `tools.lock` (sections go, release, checksums) pinning golangci-lint 2.14.0, ast-grep 0.50.0 (sha256 pinned, no checksum file is published), deadcode, govulncheck, gremlins.
- `scripts/install-tools.sh [--check] [tool...]` (shellcheck clean); verified a real install of golangci-lint and ast-grep into an empty GOBIN and `--check` afterwards.
- CI: job go (gofmt, vet, race tests, golangci-lint; installs golangci-lint and ast-grep from the lock so `tests/astgrep` no longer skips), job self-gate (fetch-depth 0, install-tools.sh, `go install ./cmd/go-qa`, `go-qa run --ci`), job workflows (actionlint, shellcheck). The old install-golangci-lint.sh and the inline tool versions are gone.
- `defence-before-fix.toml`: specs method 1.1.0, toolchain 0.2.0, detector 1.0.0; `[project]` and `[artefact]` graded separately, every known gap recorded with the file that holds the account.
- `docs/CONFORMANCE.md`: clause by clause for both levels; neither claims conformance.
- `internal/declaration` (strict TOML parse, page parse, Reconcile) and `tests/conformance`: fail when a Gap or Partial row has no declared gap, a declared gap is not Gap/Partial, a required clause has no row, spec versions differ, or a gap record file is missing.
- README rewritten in rust-qa-ci section order (What it runs, Writing rules, Project stages, Project record, Configuration, Installing, Public-safety guard); states defaults (6.4); no stale wording.

## Clauses served

9.1, 9.2 (declaration and separate levels), 9 (no conformance claim with gaps), 6.4 (defaults), 8.2 (the self-gate now installs every pinned tool and runs the full gate). The tool-pin gap recorded in docs/runners.md and docs/astgrep.md is closed.

## Tests added

tests/scripts/install_tools_test.go (lock content, unpinned tool exit 2, --check missing, ok, wrong version, whole lock), tests/ci/ci_test.go (job contents, fetch-depth, no tool pinned outside the lock), internal/declaration tests, tests/conformance tests.

## Deviations

- A missing ast-grep binary now fails the ast-grep step as an error (no skip), so 4.5 stays Met; no gap remains.
- mutants stays off in the self-gate: gremlins is pinned, but no mutation run has been held to a threshold; recorded under 8.2.
- Shared `writeStub` helper in tests/scripts/secret_scan_test.go so the existing G306 configuration exception (scope: that file) covers the new tests; no new exception.
- 8.1 is graded Partial (tests gate every push, but no release process refuses a tag when CI is red).
- Acceptance run: install-tools.sh, go install ./cmd/go-qa, go-qa run --ci exit 0 locally with all tools including ast-grep.

## Fix round: CI self-gate red (verifier problems 1 and 2)

- Cause: go.mod said go 1.26.0 and actions/setup-go (go-version-file: go.mod) reads only the go directive, so CI built with a vulnerable standard library. A toolchain line was tried first (commit 55b66e6) and did not change what setup-go installed.
- Fix (6432e67): the go directive is now 1.26.9 (single source for CI and consumers). tests/ci now asserts go.mod carries that directive and that every setup-go step reads go.mod. The proof of the clean-checkout clause is the CI self-gate job, green on 6432e67.
- Open, owner-side: the Public safety workflow's denylist scan fails because the repository secret GQACI_DENYLIST is not set.
