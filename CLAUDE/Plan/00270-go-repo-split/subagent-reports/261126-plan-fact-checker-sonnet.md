# Fact check: Plan 00270 (PLAN.md, INVENTORY.md, CI.md, DESIGN-contained-mode.md)

Method: `go list` over `src-go` (255 packages) folded to units and compared row by row with
INVENTORY.md's Deps and Users columns; file, workflow and config greps against the current tree.

## REFUTED (most consequential first)

1. **PLAN.md Task 3.3 and Q5: the plays build teamchat "from the vendored, pinned module through
   the `tool` directive (offline, checked by `go.sum`)", "the same `tasks/build-go-command.yml`
   route as today".** Refuted. `src-go/go.mod` has no `tool` directive (it holds `go 1.26.0`
   and a toolchain line only). `tasks/build-go-command.yml` lines 24-25 and 95-103 build `src-go`
   commands offline from the committed `src-go/vendor` with `-mod=vendor`, but a command from
   another repository (`go_command_module`, lines 49-52) is built with `go install` from the Go
   module proxy under `-mod=readonly`, and `src-go/vendor` is not used for it. So today's route
   for an external pinned module is online and not vendored. The offline vendored route needs
   new work: a `tool` directive, a vendor tree and a changed task. Add it as a task.
2. **PLAN.md / INVENTORY.md: "the 68 project rules" and "rules (one package, 68 analyzers)".**
   Refuted. `src-go/qa/rules/rules_test.go:241` asserts `len(All()) == 67`.
3. **CI.md: "teamchat: the `busfailpoints`, `tuwunel` and `teamchatrelease` stages that
   `src-go/qa/go-qa.toml` holds today".** Refuted in part. The toml has the stages
   `teamchat-failpoints-lint`, `-vet`, `-test`, `project-rules-tagged` (tags busfailpoints,
   tuwunel, ccyteamstest, difftest) and `teamchat-cross-build`. `teamchatrelease` is a build tag
   (one file, `member/cli/release_test.go` and `failpoint` tests) and does not appear in
   `go-qa.toml`. `CLAUDE.md` in `src-go` says go-qa's own run covers it.
4. **INVENTORY.md: "`teams/*` imports 20 teamchat packages under `internal/`".** Refuted.
   Non-test imports from `internal/teams/...` name 16 distinct `internal/teamchat/*`
   packages, and 17 with test imports (`go list` Imports, TestImports, XTestImports). The
   list below the table (16 plus `clock`) is correct; the count 20 is wrong.

## Not refuted, but note

- Deps and Users columns match `go list` for every one of the 142 unit rows except two
  differences caused by other agents' UNCOMMITTED work in this shared worktree (so correct at
  HEAD): `sessions` -> `teams` (new untracked `internal/sessions/retire.go` imports
  `teams/membership`) and `conversation` -> `shellword` (modified `conversation/elsewhere.go`).
  Re-run the inventory before Phase 1 acts on it.
- PLAN.md names `play-podfreeze.yml` and `play-lxcfreeze.yml` (INVENTORY notes): they live at
  `playbooks/imports/optional/common/`, not `playbooks/imports/`. Name resolves, path implied
  only.
- `src-go/extracted-trees.tsv` does not exist: it is a Task 2.1 deliverable, fine.
- CI.md says `PRIVATE_TERMS` repository secret: no `PRIVATE_TERMS` in `.github` or `scripts`
  today. It is stated as the new-repository design, so not a false claim about the present.
- Not checked: the per-package counts of test files that read checkout paths (INVENTORY
  "test files reading checkout paths" row).

## Verified (summary)

| Claim                                                                                                                                                                                                                                                   | Evidence                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| 255 packages in `src-go`, 105 in `src-go/qa`                                                                                                                                                                                                            | `go list ./...` both modules                             |
| Every unit row exists, and no unit is missing                                                                                                                                                                                                           | fold script, "missing in doc []"                         |
| `stdinread` and `leakcheck` import `teamchat/clock`                                                                                                                                                                                                     | Deps column matches `go list`                            |
| ccy-imported teamchat packages are the 16 listed plus `clock`                                                                                                                                                                                           | import scan outside teamchat                             |
| `.github/workflows` has `qa.yml`, `arm64-install.yml` (`ubuntu-24.04-arm`), `release.yml`, `actions/release-key`                                                                                                                                        | files, grep                                              |
| `actions-qa-ci` job and gitleaks in `qa.yml`                                                                                                                                                                                                            | `qa.yml:442`, `:258`                                     |
| go-qa-ci pinned in `src-go/qa/go.mod` with a `tool` directive                                                                                                                                                                                           | `go.mod:6,22-23`                                         |
| `go-qa.toml` coverage `fail_under_statements = 90`, `teamchat-cross-build`                                                                                                                                                                              | toml lines 50, 125                                       |
| `scripts/qa-go-qa.bash`, `qa-gitleaks.bash`, `qa-private-names.bash` (`denylist FILE`, `<git dir>/private-terms`), `check-pinned-versions.bash`, `go-binary-suites.tsv`, `src-go/go-deps.allowlist`                                                     | exist; header of `qa-private-names.bash`                 |
| `main.RootDir` ldflags for `ccy-teams`, `cc-project-dir`; `main.Version` for `claude-yolo`                                                                                                                                                              | `play-claude-yolo.yml:188,384`, `ccy-teams/main_test.go` |
| qa units `bashlimits`, `godeps`, `toolchain`, `pushgate`, `branchhygiene`, `ciscope`, `branchdelete`, `privatenames`, `internal/prepush`, `ccyversionbase`, `lxcattach`, `nokill`, `teamchatseparate`, `internal/qatrees`, `qaverdict`, `qalock`        | directories exist under `src-go/qa`                      |
| `play-teamchat.yml`, `play-claude-yolo.yml`, `play-agent-bus.yml`, `docs/agent-bus-lxc-members.md`, `helpers/pingbus/matrix_clients.json`, `helpers/play_lock/lock.py`, `container-limits.bash`, `teams/lxcsession`, `scripts/qa-ccy-version-bump.bash` | exist                                                    |
| Repository license is MIT                                                                                                                                                                                                                               | `LICENSE`                                                |
