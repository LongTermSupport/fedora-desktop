I didn't write the report file. My role forbids creating files, including through Bash, so please save the report below to `/workspace/CLAUDE/Plan/00191-go-cli-tooling/subagent-reports/261014-qa-reviewer-whole-plan.md` yourself. I made no changes to the repository.

## QA Review — Plan 00191 as a whole (Task 5.1), 83 commits, base `fd159607`..`edc2c083`

**Verdict**: FIX-BEFORE-MERGE

Nothing breaks other users, loses data or leaks private information. There are six should-fix items: one QA gap, one previous fix left incomplete, one misplaced play, and plan, docs and push drift.

### Should fix

1. **`qa-changed.bash` skips the Go contract tests when the files they check change.**
   - Several Go tests run the real bash library or the real Python helper and compare their output with the Go version:
     - `session-registry.bash` / `common-pure.bash`: `src-go/internal/registry/registry_test.go:348`, `internal/restore/manifest_test.go:458-461`, `internal/screen/screen_test.go:104,135`, `internal/transcript/transcript_test.go:246`
     - `helpers.agent_bus.declare`: `internal/declare/integration_test.go:23`, and the `internal/teams` tests
   - The interim gate only runs for `src-go/*` (`scripts/qa-changed.bash:228-233`). I asked `qa-changed.bash --plan --files` about `helpers/agent_bus/declare.py`, `lib/session-registry.bash` and `lib/common-pure.bash`. Each picks shell suites but never `check go`.
   - So a change to the bash record format or to `declare.py` that breaks the Go side passes per-commit QA, and only `qa-all` catches it at push.
   - Fix: set `NEED_GO` for the files the Go tests read, and add a case to `scripts/test-qa-changed.bash`.

2. **The earlier dead-code fix (cut-over review item 2) missed one chain.**
   - `ccy_tmux_take_over` calls `ccy_tmux_client_words`, which calls `ccy_tmux_idle_words` (`files/var/local/claude-yolo/lib/tmux-session.bash:155,173,204`). Nothing outside that file calls the chain: I ran `git grep` over `files helpers playbooks src-go`, and a sweep of every lib function found `ccy_tmux_take_over` as the only one with no caller at all.
   - The comment at `tmux-session.bash:25` still says "ccy-sessions can take one over (ccy_tmux_take_over)". That is false now: the Go binary uses `internal/tmuxccy`.
   - `scripts/test-ccy-sessions-take-over.bash:243-262` and `CLAUDE/QA.md:102` keep testing it. No Go test uses it as a parity reference, which was the stated reason for keeping the other functions.
   - Fix: delete the three functions and their bash cases, and bump CCY (this changes `lib/`). Or, if they stay as a reference, give the reason and make a Go test actually use them.

3. **`play-golang.yml` is now always installed but still filed as optional.**
   - Two core plays import it: `playbooks/imports/play-python.yml:6` and `play-claude-yolo.yml:5`. No other core play imports anything from `optional/` (`git grep 'import_playbook: optional'` returns only these two).
   - It is still listed in `playbooks/imports/optional/server-recommended.bundle:7`, and `docs/playbooks.md:1111` documents it under "Optional Playbooks" (heading at :763).
   - The `play-claude-yolo.yml` section of `docs/playbooks.md` (:468-512) says nothing about installing `golang` or building `ccy-teams`/`ccy-sessions` from `src-go/`.
   - Fix: move it to `playbooks/imports/play-golang.yml` and add the old path to `helpers/play_ledger/retired-plays.json`. Drop it from the bundle, and update both doc sections.

4. **The task text in `PLAN.md` is out of date.**
   - Task 1.2 still says `play-golang.yml` "installs `golang`, `gcc` … and golangci-lint at the `.qa-versions` pin". Commit `4a358878` moved gcc and golangci-lint to `play-python.yml`. The play (`play-golang.yml:13-16`) and the 00:55 journal entry both say Go only.
   - Task 3.2 says "Milestones M0-M13", but M14 is done (commit `199a2bfe`, journal 04:42).

5. **Agents get no guidance for the new language.**
   - There is no `.claude/rules/*.md` with `paths:` for `src-go/**` or `*.go`. `source-dirs.md` covers `src/**` only.
   - The path-triggered table in `CLAUDE.md` and its QA bullet mention ESLint but not Go.
   - An agent editing Go is never pointed at `qa-go.bash`, the coverage floor, the rule that suite build flags must match the play's, or `GO_BINARY_SUITES`.
   - Fix: add a thin `go-sources.md` rule that points at `CLAUDE/QA.md`, plus a row in `CLAUDE.md`.

6. **Three commits are not pushed, and the push gate cannot pass in this container.**
   - `git log origin/F44..HEAD` shows `edc2c083`, `856105ca`, `199a2bfe`, including the Task 1.4 coverage floor.
   - `scripts/qa-toolchain.bash` exits 1 here: "golangci-lint is not installed (expected 2.14.0)". `go` is present only as a symlink dated 23:15 on 13 October, and Task 1.1 records it as "installed in the running container now". That is the manual container install the Missing Dependencies rule forbids.
   - The tracked repo does not depend on it (`git grep untracked/tools` has no hits). But a container rebuild (`ccy --rebuild`) is needed before `qa-all` can pass and these commits can be pushed. Tell the owner.

### Nits

7. **One suite check passes even if its tests never run.** `scripts/test-ccy-tmux-targets.bash:298-299` treats any output starting with `ok` as a pass. If `TestRealTmux*` is renamed, `go test` still prints `ok … [no tests to run]` and the check passes. Assert on `--- PASS: TestRealTmux` with `-v`, or count the passes.
8. **The `qa-go.bash` pass line gives no total.** It counts `^ok` lines and prints "19 package(s)". A package without tests would drop out of that count silently. Print "n of m", where m comes from `go list ./...` (it is 19 today).
9. **The `CLAUDE/Plan/meta-deploy.bash:59-67` comment reads out of order.** The CCY 3.95.8 clause sits between "ccy-teams is now the Go binary" and "and ccy-sessions is too".
10. **Comment slips in `playbooks/imports/play-claude-yolo.yml`.**
    - `:3` says only "ccy-teams is a Go binary built here"; ccy-sessions is built there too.
    - `:838-839` and `:888-889` were edited without re-wrapping, so the lines run long.

### Checked and clean

- **The seven earlier findings did not regress.**
  - `-buildvcs=false` is on both play builds (`:856`, `:894`) and on every suite build.
  - The six dead functions are gone; `git grep` finds them only in the changelog.
  - `play-golang.yml` installs only `golang`.
  - `RootDir == ""` is refused (`cmd/ccy-teams/main.go:23-28`).
  - The `qa-all` template comment is fixed, and the `CCY_VERSION` comment is updated (3.95.8).
  - `test-qa-changed.bash:197-206` asserts that every `src-go/cmd/*` has a suite entry, with a control case.
- **Play placement:** both builds live in the play that deploys the binaries, run as the user with `GOTOOLCHAIN=local` and `-mod=readonly`, and install with `copy`, so the install only reports changed when the checksum differs. `go.mod` has no dependencies, so no network is needed on the unattended path. `play-python.yml` is imported before `play-claude-yolo.yml` (`playbook-main.yml:34,36`).
- **Fail-fast in Go:** the only discarded value in non-test Go code is `strings.Cut` (`restore/manifest.go:141`). Error branches wrap and return their errors. The "No server" empty result is an explicit, documented case (`tmuxccy/panes.go:23`).
- **Secrets:** the Element password goes to `wl-copy` on stdin, never in argv (`internal/teams/password.go:111-113`), and is shown on stderr only.
- **This repo does not depend on go-qa-ci:** it is mentioned only in comments (`scripts/qa-go.bash:6`, `CLAUDE/QA.md:50`).
- **Toolchain:** `.qa-versions` (`GOLANGCI_LINT=2.14.0`), the Dockerfile, `play-python.yml`, CI (checksum-verified) and `qa-toolchain.bash` all agree. Go is treated as a minimum version taken from `go.mod`.
- **CI:** the latest run, 38509621838, succeeded with `GO-OK 19`, `ccy-tmux-targets 20` and `ccy-teams-command 323`.
- **Coverage:** every package is at least 50% (`cmd/ccy-sessions`, a 2-line main); every `internal/` package is at least 89.6%; the total is 94.2%, matching the plan and journal.
- **Public-repo safety:** only placeholders (`/home/u`, `192.0.2.10`, `example-org`). The `Owner: joseph` line follows the repo convention (145 plans).
- **CCY versions:** each of the three lib/launcher commits bumps (3.95.6, 3.95.7, 3.95.8). The `.claude/ccy/Dockerfile` belongs to this repo's own dev image, so no `REQUIRED_CONTAINER_VERSION` bump is needed.
- **No stale references:** `git grep` finds no live mention of the deleted `ccy-teams.j2`, `files/home/.local/bin/ccy-sessions` or `test-ccy-session-network.bash` outside plans and the changelog. Every suite `qa-all` calls exists.
- **Plan index:** the README row is present, and the In Progress status matches open Tasks 3.3, 4.2 and 5.1.

### Mechanical gates

- **qa-all.bash:** not run, as instructed. Run separately, `qa-toolchain.bash` fails here because golangci-lint is missing (item 6).
- **qa-go.bash** (with `untracked/tools/bin` on PATH): `GO-OK 19 package(s): fmt, vet, test -race, coverage 94.2% (floor 90%), golangci-lint clean`.
- **plan-qa --sweep:** 15 findings, 0 block, none for 00191.
- **syntax-check:** passes for `play-claude-yolo.yml`, `play-python.yml` and `play-golang.yml`.
- **qa-helper-tests:** not triggered by recent commits; the earlier review ran it for the `tests/helpers` change.
- **Extension gates:** not triggered; nothing under `extensions/` changed.
- **Not re-verified:** whether builds are byte-for-byte reproducible. Checking would mean writing two build outputs, which my role doesn't allow.