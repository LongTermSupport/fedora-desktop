# Handoff: Plan 00266 Phase A (after Tasks A.1 to A.3)

The first builder of Phase A stopped at its context budget after committing Tasks A.1 to A.3
(ccy 3.124.103). This is what is left, and where things are.

## Done (in the commit that adds this file)

- **A.1.** The class is "host launcher applies container-writable checkout config".
  - Bash half: the semgrep rule `ccy-host-applies-checkout-config` in
    `.semgrep/bash-conventions.yml`, with its fixture in `.semgrep/bash-conventions.bash`. It is
    blocking in `qa-patterns`. It was red on exactly `lib/common.bash:425` and
    `lib/network-management.bash:941`.
  - Go half: `project_checkout_config_pin` in `src-go/qa/rules/checkoutpin.go`, with fixtures in
    `testdata/checkoutpin`. It was red on `internal/sessions/restore.go:220` only.
  - Both are documented in CLAUDE/QA.md and CLAUDE/GoProjectRules.md.
- **A.2 and A.3.** `src-go/internal/checkoutpin` (package doc K1-K4, command doc V1-V3).
  - It is wired in `internal/teams/app.go` as:
    - `ccy-teams approve mounts|compose [<checkout>]`
    - `ccy-teams internal project-mounts`
    - `ccy-teams internal checkout-pin compose true|false`
  - The bash callers are `load_project_mounts` (common.bash) and `ccy_compose_up`
    (network-management.bash). The bash validator `ccy_validate_mount_line` is deleted.
  - The difftest fake answers `internal project-mounts`, and its goldens are re-recorded.
- **Sweep findings**, sent to the coordinator and not fixed:
  - `.claude/ccy/.last-launch.conf` is applied by a headless launch (its SSH key and network).
  - A host `cc` in a checkout that a container also mounts runs that checkout's
    `.claude/settings.json` hooks.
  - Host git in a mounted checkout reads that checkout's `.git/config`.

## Left

1. **A.4** (`src-go/internal/sessions/restore.go`, `record()`, the
   `command = append(append(command, rec.Launcher), args...)` line).
   - Exec a launcher from a fixed table keyed by prefix: `ccy` →
     `/var/local/claude-yolo/claude-yolo`, `cc` → `/var/local/claude-code/cc`.
   - Refuse any other prefix in `planRestore`, so `restore --check` and `declare` refuse it too.
   - Tests need a seam: an App field that defaults to the table.
   - The bash suites record `/launch/ccy` and `/launch/cc` and grep for them:
     - `scripts/test-ccy-restore-askpass.bash:482-520`
     - `scripts/test-ccy-compose-restore.bash:193-206`
     - Go tests in `sessions/*_test.go` (`Launcher: "/launch/ccy"`)
   - Decide how the suites reach a fake. One way is an env override of the table's directory,
     which is host-only env. Say in the plan why it is safe.
2. **A.5** (`src-go/internal/registry/replay.go` `ReplayArgs`; `restore.go` `FlagClass`).
   - At restore, refuse a record whose args hold an unknown flag (`ClassUnknown`) or any word
     after `--`. Apply this to cc above all: every cc word reaches a host claude.
   - cc's own words are `--teams <v>`, `--teams=<v>` and `--bus`.
   - Validate `withoutSelectors(args)` in `planRestore`, so `--check` and `declare` agree.
   - Refuse, naming the word and how to declare the session again. Do not drop the word
     silently.
3. Optional: `judgeRestore` could report a ccy record whose mounts or compose are unapproved
   (`checkoutpin.Store.Verify`), so `restore --check` names them before the next boot.
4. **The independent security review** (the coordinator's bar).
   - Dispatch `qa-reviewer` with the code, not these conclusions.
   - Ask it to break the mounts allowlist (`checkoutpin/mounts.go`), the hash approval
     (`pin.go`, `command.go`, and the terminal check in `teams/app.go` `newPin`), and the replay
     filter.
   - Fix what it finds.
   - Name its report path in PLAN.md and in the commit message.
5. Tick A.4 and A.5 in PLAN.md. Bump CCY_VERSION if the launcher's bash changes. The changelog
   and meta-deploy.list need a line only if the deploy changes.

## How the commits were made

- Temp index: `GIT_INDEX_FILE=/workspace/untracked/scratch/p266a.index`, then `git read-tree HEAD`,
  then `git add` only your own paths.
- The shared files (GoProjectRules.md, ccy-changelog.md, meta-deploy.list, table.go,
  rules_test.go) hold edits from other agents. Each one is staged as HEAD plus this phase's
  lines only.
