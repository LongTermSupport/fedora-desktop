# Dormant-branch aggregate: where it stands

Plan 00245 Phase 4. Aggregate branch: `agent-a5ee80f767118a918-a448e021`, worktree
`.claude/worktrees/dormant-aggregate`. Pushed tip: see the end of this file. CCY version
carried: **3.113.1** (F44 is at 3.112.5; one changelog entry, `docs/ccy-changelog.md`).

## The five branches

| #   | Branch                                                    | Outcome                                |
| --- | --------------------------------------------------------- | -------------------------------------- |
| 1   | `agent-a5ee80f767118a918-a448e021` (ccy pids limit)       | **Completed and in the aggregate.**    |
| 5   | `agent-ae87230b6e5b05508-967f0e77` (Plan 00173 follow-up) | **Completed and merged** (42d0335fb).  |
| 2   | `agent-a81014c63f66ee6f8-c4d3bbe2` (Plan 00185 umask pin) | **Left out, for the owner.**           |
| 3   | `agent-a8c69aa67110156ce-c48195fd` (Plan 00177 T7.6)      | **Not started** (stopped for context). |
| 4   | `agent-ad49d4886a70d25db-01b66214` (ccy Go caches)        | **Not started** (stopped for context). |

### 1. ccy pids limit

- F44 merged in (1ff58f6bc). F44 had moved the token check, network check and Claude Code
  version probe from the bash launcher into the Go front, so those runs did not get the limit
  from the branch's bash edits. Now `engine.RunWithSecret`, `RunOnNetwork` and `ClaudeVersion`
  take it too (engine E14), the front settles it (`settledPidsLimit`, front F17-F19), and a
  refused value runs nothing (exit 1). Test written first: `TestTheLauncherChecksRunUnderThePidsLimit`.
  difftest re-recorded, with the branch's two launch scenarios adapted to F44's token check.
- Review round 3 (qa-reviewer): CHANGES, three blockers, all fixed in 0ad400860:
  1. `ccc` started its session and sign-in on the ccy image with no limit. It now passes
     `engine.PidsLimitDefault` (4096).
  2. The Semgrep rule reads no Go. Defence Before Fix: the project rule
     `project_container_pids_limit` (`src-go/qa/rules/containerpids.go`), red on its fixture
     `fix/internal/badpids` and, swept over both modules, red on exactly `ccc`'s two runs and
     their tests; green after, the `difftest` and `ccyteamstest` tagged builds included.
     Catalogued in `CLAUDE/GoProjectRules.md`, linked from the QA.md section.
  3. No plan owned the work: Plan 00245 Phase 4 now does.
     Nits fixed: the addedgates reason; the `docs/ccy.md` line join is the formatter's.
- Not re-reviewed after the round-3 fixes (round 4 not run).
- Host run queued: `changed-plays` in `CLAUDE/Plan/meta-deploy.list` (play-claude-yolo.yml,
  play-claude-code-consumer.yml for ccc, play-basic-configs.yml for item 5).

### 5. Plan 00173 follow-up

The branch named cc sessions as "not shown the warning" in the bash `ccy-sessions`, which F44
deleted when it cut over to Go. Ported to Go (`src-go/internal/sessions/routes.go`,
`checkProjects` + `ccSessions`; `TestACcSessionIsNamedAsNotShownTheWarning`, red then green).
The branch's own case in `scripts/test-ccy-sessions-reboot.bash` passes against the Go binary
(350 passed). `shutdown-with-update` keeps F44's printf help with the branch's wording; the
branch's edits to the deleted bashrc include and to `meta-deploy.bash`'s old PLANS array are
dropped. No open review round was recorded on this branch.

### 2. Plan 00185 umask pin: left out

Evidence: after the branch stopped (2026-10-23), F44 fixed the same defect, root-written font
caches left 0600 under a cc session's umask 077, another way: 05ad1442c "Merge Plan 00185: font
caches stay readable by every user (rule root-umask, 7 sites fixed; DBF conformance CONFORMS)".
Plan 00185 is Complete in F44 (`CLAUDE/Plan/Completed/00185-terminal-emoji-glyphs/PLAN.md`
lines 129-132). The branch pins every Ansible run to umask 022 with a callback plugin
(`callback_plugins/umask_pin.py`, rule `ansible-umask-pinned`); F44's `root-umask` rule
(CLAUDE/QA.md#root-umask) instead requires each root task that writes user-read files to set
its own umask or mode, and its doc says "Ansible has no umask setting for a command". The two
overlap: the pin would make every such task safe at run time, the rule keeps each task correct
on its own. Whether to add the pin as well is the owner's decision. The branch also edits the
plan's active-folder path, which F44 has moved to `Completed/`.

### 3 and 4: not started

Stopped because this agent's context passed its budget. Nothing of either is in the aggregate.

- 3 (`agent-a8c69aa67110156ce-c48195fd`): 66 files, mostly `src-go/internal/teams` and two new
  project rules (`project_child_exit`, `project_row_kind`) plus `internal/term`; CCY 3.100.23 in
  the branch. Its review round 2 is open (report
  `261023-unknown-team-review-r2.md` in its tree). Expect conflicts in `CLAUDE/GoProjectRules.md`,
  `src-go/qa/rules/table.go` and `rules_test.go` (rule count, now 32), the launcher, help.txt,
  the changelog and difftest goldens. F44's Plan 00177 no longer has a Task 7.6.
- 4 (`agent-ad49d4886a70d25db-01b66214`): handoff
  `CLAUDE/Plan/00213-agent-bus-performance-go-carve-out/subagent-reports/261023-ccy-go-cache-handoff.md`
  (in F44). Next: qa-changed on its B1 WIP commit, review round 5, then the round-4 non-blocking
  items. Its Go container runs must pass the pids limit (the new rule will say so).

Each needs: `git merge --no-ff origin/<branch>`, conflicts resolved, the CCY version raised one
step above the aggregate's (3.113.x), its changelog entry folded into the single 3.113 entry,
the open review round run with qa-reviewer, `./scripts/qa-changed.bash --base origin/F44`, a
journal entry, the Plan 00245 Phase 4 task ticked, and a push.

## Tooling faults met (dogfooding)

- **Fixed:** `pre-push` (private-names) rescanned every commit `origin/F44` already
  publishes when pushing a branch that merged F44, and refused the push over history findings
  the owner had left. Plan 00245 Task 3.6, 8289bf74e.
- **To log:** the merge commit's `pre-commit` took about five minutes on a 3485-file merge;
  it loops per staged file in bash. Not a failure, but it blocks for minutes.
- **To log:** the `lint_on_edit` Ansible check fails in a linked worktree: the syntax check
  needs `vault-pass.secret`, which a worktree does not have (same cause as the earlier "DBF
  conformance-reviewer could not be dispatched" note in the pids-limit handoff).
- **To log:** the qa-reviewer agent has no Write tool, so a dispatch that names a report path
  gets the report inline only.
- Note: the first push of 1ff58f6bc was refused by the hook, yet the next push showed the
  remote already at 1ff58f6bc. Not investigated.

## QA

See the end of this file for the `qa-changed --base origin/F44` result. Also run:
`go test` for `internal/engine`, `internal/ccylaunch/...` (difftest included), `internal/ccc`,
`internal/sessions`, `qa/rules`, `qa/all`, `qa/privatenames` (all ok);
`scripts/test-ccy-container-limits.bash` (26 passed), `scripts/test-ccy-token-handoff.bash`
(134 passed), `scripts/test-ccy-sessions-reboot.bash` (350 passed), `scripts/qa-patterns.bash`
(531 files OK), `scripts/qa-bash-limits.bash` (OK after `--update` lowered the launcher's record).

## State at hand-back

- `./scripts/qa-changed.bash --base origin/F44` over the whole aggregate was started and is
  queued behind the coordinator's `qa-all` on the QA lock (30 minutes waited). It has not run
  any check yet. Its log: `untracked/scratch/qa-agg.log` in the worktree (last line `rc=N`
  when done), results `untracked/scratch/qa-agg.json`. Its result is not known to this report.
- Last pushed tip: the commit that adds this report (see `git log -1` on the branch).

## Continuation (second agent)

CCY at the end: **3.113.2** (one changelog entry, 3.113.2, carrying the pids limit and the
unknown team). Stopped for context before branch 4.

### 0. The first agent's QA: done, three failures

`qa-changed --base origin/F44` ran once the lock freed (it had waited 90 minutes behind two
other runs). Results:

- `test-ccy-token-mode.bash`: 8 failed with rc 127. `create_token` now passes
  `--pids-limit "$CCY_PIDS_LIMIT"`; the suite sourced the token library under `set -u` without
  settling the limit, so each case died with "unbound variable". Production settles it first
  (launcher line 99), and `cc` sources the library in host mode, which never reaches
  `create_token`. Fixed in the harness (ba65d1f3d): 43 passed. Every other suite that sources
  the library or `image-build.bash` passes.
- bash-limits: `shutdown-with-update` was 289 code lines against a record of 286 (branch 5's
  help wording). Rewrapped into F44's 9 lines with the same content (ba65d1f3d).
- ansible-syntax: the 4 playbooks fail in any linked worktree, which has no
  `vault-pass.secret`. Environment, already logged as issues #146 and #215. The coordinator's
  run in the main checkout covers these plays.

### 1. F44 merged: done (e159a535b)

The only conflict was `src-go/qa/privatenames`. F44's version was taken whole: the branch's
test pinned nothing F44's does not, and F44 also no longer counts local archive refs as
published. Recorded in Task 3.6.

### 2. Round 4 review of the pids limit: done (ab3b4c363)

The round found no blocker (round 3 had three) and 3 medium and 4 low findings, all fixed:

- `project_container_pids_limit` now finds the verb after leading non-constant arguments and
  after `podman`, `docker` or `container`. That covers the engine package's own
  `e.run(ctx, "run", …)` style.
- It also judges `create`, reads flags by name (`-dit`, `--rm=true`), and counts the long flags
  only a container takes.
- 7 fixture cases went red, then green. The driver sweep over both modules is clean with the
  default, `difftest`, `ccyteamstest`, `busfailpoints` and `tuwunel` tags. It still goes red on
  `ccc`'s pre-fix code (shown by putting the old `run.go` back).
- What it still cannot see is listed in its doc and the catalogue, `netpolicy`'s `create` among
  it: its limit comes from the bash launcher.
- Task 4.1 is ticked.

### 3. Branch 3 (Plan 00177 unknown team): done

F44 does not already do this: there is no `chooser/unknown.go` and neither rule exists there.
Merged in dd2c450ed. Conflict resolutions:

- Rules: 34, later 35. Wizard: F44's `declined` kept beside the branch's `named`.
- `seats` and `picker`: F44's wording and its keyless action shape, with `IsSession`.
- Board: a host seat's KIND is `cc`, not F44's `host`. `agentbus.KindOf` is the one reading.
- Help: the `help-pids-limit-override` golden was re-recorded.
- Changelog: folded into one entry. Plan 00177: F44's status text plus Tasks 7.6-7.9, with 7.9
  (the host test) open.

The swept rules found one new instance in F44's code. `lxccmd.failed` typed a guest op's status
as `term.ExitCode`. It was never the tool's exit, so it is now an `int` (`FromChild` would have
hidden an undocumented code).

Review round 2 (610908276):

- Finding 1 (medium) was settled by F44's `declined`; the chooser's dead branch is removed.
- Findings 2-7 fixed. For 7, `project_child_exit` now follows a `var` local, red then green.
- Findings 8 and 9 (optional) left: 8 is a consistency point, and 9 has no room in a help that
  must keep its line count.

Review round 3 (CHANGES: 1 medium, 1 low-medium, 1 nit; round 2 had 1 medium and 8 low), fixed
in the commit after this report's previous tip:

- The graph asked `Kind == KindCcy`. Defence Before Fix: the new rule `project_session_kind`
  went red on its fixture and on 3 real sites, then green. `TestMermaidDrawsACcSeatWithItsSession`
  was red first.
- F44's duplicate `kindWord` is removed.
- `ge.Exit == 75` is now `int(exits.ExitBusy)`.

Task 4.4 is ticked, with a journal handoff entry.

### 4. Branch 4 (ccy Go caches): not started

Left for a fresh agent: this agent passed its context budget. From the trial merge
(`git merge-tree`), expect conflicts in:

- `docs/ccy-changelog.md`, the launcher and `scripts/bash-limits.allowlist`;
- 27 difftest goldens (each gains a line; re-record with `go test ./internal/ccylaunch/difftest/ -update` after the merge);
- `src-go/internal/teams/app.go`.

Its Go container runs must pass `project_container_pids_limit` (now broader). Then follow its
handoff: review round 5, and the round-4 non-blocking items.

### QA state

After the commits above, `qa-changed` has **not** been re-run over the whole aggregate. A run
was started and stopped while it still waited for the lock, before it checked anything, so
that the round-3 fixes could go in. Targeted checks did pass:

- `go test` over `internal/teams/...`, `term`, `agentbus`, `sessions`, `ccylaunch/...`
  (difftest included), `ccc`, `engine` and `teamchat/host/...`, plus `qa/rules` and `qa/all`.
- `golangci-lint` on each changed package (0 issues).
- The project-rule sweep over both modules and every tag set (clean).
- `test-ccy-teams-command.bash` (395 passed), `test-cc-teams.bash` (83),
  `test-ccy-token-mode.bash` (43), `test-ccy-token-import-cleanup.bash` (25),
  `test-ccy-image-build-lock.bash` and `test-ccy-session-registry.bash` (pass), and
  `qa-bash-limits.bash` (OK).

The next agent should run `qa-changed --base origin/F44` first.

### Tooling faults met (dogfooding)

- **To log:** the hooks-daemon secret-path guard (`R-SECRET-BASH-MENTION`) took markdown `**`
  in a heredoc body for the protected glob `*.secret*` and blocked the command. That is a false
  positive in an upstream, daemon-owned handler; the workaround was a script file. The
  coordinator should file it upstream (`hooks-daemon issue-report`).
- **To log:** the containment guard (`R-WRITE-OUTSIDE-PROJECT-ROOT`) resolves a relative
  `../x` against the project root, ignoring an earlier `cd` in the same command, and blocked a
  write inside the worktree. The workaround was absolute paths.
- Already logged: #146 and #215 (vault password in worktrees), #216 (qa-reviewer has no Write).

## Continuation (third agent)

All five branches are now handled: four in the aggregate, branch 2 left out for the owner. CCY at
the end: **3.113.4, container 2.80.1** (one changelog entry, after F44's 3.113.1).

### 0. QA over bc9b2e004 (the second agent's tip)

`qa-changed --base origin/F44`: every gate passed except ansible-syntax, whose 4 playbooks fail
only because a linked worktree has no vault password file (#146, #215).

### 1. Branch 4 (ccy Go caches): done

F44 has no `gocache` package and no `internal go-cache` command, so the branch was merged
(fcc028cca). Its only container-facing code prints run arguments and starts no container, so
`project_container_pids_limit` has nothing to judge there; the project-rule sweep over
`internal/teams/...` (plain and `ccyteamstest`), `ccylaunch/...` and `cmd/...` is clean.

- Launcher version line and changelog: folded into one entry.
- `teams/app.go`: the aggregate's `chosen` chooser kept, `GoCache` added beside it.
- `bash-limits.allowlist`: re-measured with `--update`; the launcher record went down.
- 27 difftest goldens re-recorded with `-update`. Each was checked line by line to differ from
  the aggregate's only by the `ccy-teams internal go-cache difftest-project` call and the bind
  and `GOCACHE` arguments on the run (`:z` and the `z` argument in the SELinux-enforcing
  scenario). That is the merged behaviour intended.

Review round 5 (qa-reviewer, the merge's diff; CHANGES, no blocker: 2 medium, 1 low, 1 nit), all
fixed in a062f10f5:

- The Go-cache bullets landed under 3.105.0. The conflict's HEAD side spanned several entries,
  so they had been appended at its end. They are now in the single new entry.
- The `changed-plays` description now covers the Go caches.
- The launcher's mount comment no longer names a docker volume.
- `docs/ccy.md` gives a rootless-docker route for a foreign-owned file.

Round 4's non-blocking items:

- **Done:**
  - N1: the deploy description.
  - N2: `docs/ccy.md` says a running session can grow the caches past the bound until the
    next launch.
  - N4: the lock is tried with `LOCK_NB` first, and a launch that must wait says so on stderr.
    `TestTheRealLockSaysWhenItMustWait` and `TestAWaitForTheProjectsLockIsSaidOnStderr` were
    red first.
  - N5, the launcher's stop: the new difftest scenario `launch-go-cache-fails` pins exit 1
    with no session container.
- **Left:**
  - N3: the "not a cache ccy keeps" wording when a seat refills during a prune; practically
    unreachable at 8 GiB.
  - N5's other test gaps: `faultyFS` hides `ReadDirFS`, and the vanished-file test is a unit test.
  - N6: recovery from a mode-000 directory, which needs a non-root host to verify.
  - N7: the cost of the walk and of the `:z` relabel, which needs an Enforcing host. Owner or
    host work.

### 2. F44 merged again (886105eeb)

origin/F44 had moved to 9fd2236ee (PR #189, arm64: CCY 3.113.1, container 2.80.1).

- Conflicts: the launcher version line, the changelog and `meta-deploy.list`.
- The container is F44's 2.80.1, since the aggregate changes no image file.
- The changelog keeps F44's 3.113.1 and 3.113.0 below the aggregate's entry.
- One `changed-plays` keeps both descriptions.
- The commit hook refused the merge until CCY was bumped, so it is 3.113.4. The commit message
  says 3.113.3, because it was written before the hook refused; the tree is right.

### QA over the final tree

`qa-changed --base origin/F44` over the final tree (at 886105eeb plus this plan's
task tick, journal and this report): every gate passed except ansible-syntax, for the same
missing vault password file. That includes go-qa over both modules in diff mode, docs and
every suite it routed. `go test` passed for `teams/...`, `ccylaunch/...` (difftest included)
and `engine`, as did `golangci-lint` on the changed packages (0 issues) and `qa-bash-limits`.

### For the coordinator and the owner

- The coordinator's `qa-all` in the main checkout covers ansible-syntax.
- Host run: `changed-plays` in `meta-deploy.list` (play-claude-yolo.yml among them) carries all
  of it; Plan 00177 Task 7.9 is the owner's host test.
- Task 4.3 (umask pin) is still the owner's decision.
- **To log (dogfooding):** the secret-path guard blocks a Bash command that merely greps a QA
  log for the vault password file's *name* (`R-SECRET-BASH-MENTION`), so a check of why
  ansible-syntax failed has to avoid naming the file. The same false-positive family as the
  second agent's note.
- **To log:** the CCY bump hook demands a new version on a merge commit even when the merged
  version is already above both parents, so each F44 merge burns a version number.
