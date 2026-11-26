# Plan 00270: CI for each extracted repository

Every new repository has the same gates its code meets in `src-go/` today. None of them is
weaker for having moved.

## What fedora-desktop does today, to copy from

- `.github/workflows/qa.yml` picks the scope (full on a protected line, diff on `ci/<topic>`),
  runs `scripts/qa-all.bash` or `scripts/qa-changed.bash` in parts, and runs
  `LongTermSupport/actions-qa-ci` over the workflows and a gitleaks job
  (`scripts/qa-gitleaks.bash`).
- The Go gate is `scripts/qa-go-qa.bash`, which runs go-qa-ci's `go-qa`. It is pinned by the
  `tool` directive of `src-go/qa/go.mod`, configured by `src-go/qa/go-qa.toml` (the coverage
  floor, the record, the tagged stages), and given the project rules through `src-go/qa/driver`.
- `.github/workflows/arm64-install.yml` runs on `ubuntu-24.04-arm`.
- `.github/workflows/release.yml` and `.github/actions/release-key` make signed release
  commits and tags.
- `scripts/qa-private-names.bash` keeps the term list out of the repository, in
  `<git dir>/private-terms`. Its `denylist FILE` mode writes the terms as go-qa-ci's
  public-safety denylist, outside the repository.

## The per-repository pipeline

| Job                    | What it runs                                                                                                                                                                                              | Notes                                                                                                                                                         |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| go-qa (amd64)          | `go tool go-qa run --no-fix`: `go test -race -shuffle`, `go vet`, golangci-lint, govulncheck, the coverage floor (90 percent of statements, as now), the project rules through the repo's own `qa/driver` | `go-qa-ci` pinned in the repo's `qa/go.mod` exactly as here                                                                                                   |
| go-qa (arm64)          | the same on `ubuntu-24.04-arm`                                                                                                                                                                            | the seats run on both architectures                                                                                                                           |
| tagged builds          | teamchat: the `busfailpoints` and `tuwunel` stages that `src-go/qa/go-qa.toml` holds today, and the `teamchatrelease` build tag; ccy: `ccyteamstest`, `difftest`                                          | move with their code                                                                                                                                          |
| cross build            | `CGO_ENABLED=0` linux/amd64 and linux/arm64 for every command                                                                                                                                             | teamchat's `teamchat-cross-build` stage, generalised                                                                                                          |
| bash limits            | ccy only (it carries the bash launcher and libraries): the bashlimits gate with its own ratchet allowlist                                                                                                 | kit and teamchat hold no bash; a file that appears is caught by the same gate, which runs in every repo and passes when there is nothing to measure           |
| public safety          | gitleaks over the tree and the history; go-qa-ci's public-safety denylist built from the `PRIVATE_TERMS` repository secret at run time                                                                    | the term list never enters the repository or a log; a fork's pull request has no secret, so the job **fails** there rather than passing unchecked (fail fast) |
| workflows              | `LongTermSupport/actions-qa-ci`                                                                                                                                                                           | as here                                                                                                                                                       |
| dependencies           | the go-deps allowlist (vendored, pinned, a reason per module)                                                                                                                                             | the godeps gate, from go-qa-ci once upstreamed (INVENTORY.md)                                                                                                 |
| release (tag `vX.Y.Z`) | build every command for both architectures with `-trimpath -buildvcs=false`, `sha256sums.txt`, signed with the release key, attached to a GitHub release                                                  | fedora-desktop's plays pin the version **and** the checksum                                                                                                   |

**Branch flow:** the same as here. Work is pushed to `ci/<topic>`, and the protected `main`
moves only to a commit CI passed (branch protection: required checks, signed commits, no
force push).

**Agent tooling in each new repository, from its first commits:** a `CLAUDE.md` pointing at
the shared rules, and the full hooks-daemon setup: the daemon installed, its config and
handlers, and the planning workflow (`CLAUDE/Plan/`, `mkplan.bash`, plan QA). Its first plan
carries `HANDOVER.md` (PLAN.md Task 2.4). That is Task 2.3's checklist, so an agent working in
the new repository keeps the guards it has here.

**History:** none is carried (Q2). gitleaks and the private-names scan run over the tree
before the initial commit, then over the new repository's own history in CI from then on.
