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
