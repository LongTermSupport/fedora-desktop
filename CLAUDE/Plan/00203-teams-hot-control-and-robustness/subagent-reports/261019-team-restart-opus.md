# Team restart and resume by id: interim report (Opus)

Branch `feat/teams-restart`, from local `work/add-session-and-restart-fixes` (F44 97537444
already merged into it). Tip 97d30e43. Not pushed. Stopped at a natural break because the
context budget ran out; the handoff for the next developer is
`untracked/scratch/261019-a70c26e520cdeb67d-handoff.md` in this worktree.

## Renumbered

F44's lxc-checkout task holds 3.8, so this branch's tasks moved up: add a running session is
3.9, the recorded conversation 3.10, restore and team restart by id 3.11, the agent view off
3.12. PLAN.md, the spec header and a journal correction entry (`--ref 10:09`) say so. The
earlier journal entries were left as written.

## Task 3.10: each session's conversation recorded (done)

- The pingbus SessionStart hook writes `conversation.json` (v, session, session_id, source,
  written_ms; mode 0600, atomic) into the session's report directory. It does this on every
  SessionStart: startup, resume, clear and compact.
- `internal/conversation.For` reads the file only when the record's launch carried the bus
  (Fable's M6 B1-class point). It resolves the transcript by the recorded id, never by taking
  the newest one in the folder.
- Tests, failing first: `ConversationRecordTest` (7 tests) and the plugin contract
  (`HooksJsonTest`) in Python; `TestSessionDirHoldsTheReport` and the `conversation` package
  tests in Go. All now pass, and the full helper suite passes (4851 tests).
- CCY 3.99.12.13 and container 2.77.9 are placeholders. The image carries the kit.

## Task 3.11, part 1: restore resumes by id (done)

`registry.ResumeArgs` replaces `RestoreArgs`. `ccy-sessions restore` resumes each session with
`--resume <recorded id>`. A record with no recorded id is reported as a failure, keeps its
record, and nothing is started.

Still to do: delete the uncalled bash `ccy_registry_restore` and `ccy_registry_restore_args`
(see the handoff), `ccy-teams restart` itself, the lifecycle plugin's restart reason, Task
3.12, and meta-deploy.

## Owner decisions

- D8, D9 and D11 are as in PLAN.md.
- D10 changes `ccy-sessions restore` for existing records. Before: every record was started
  with `--continue`. After: each is started with `--resume <id>`. A `--no-bus` record, a
  plain-`cc` record, or one whose hook never ran is not started, and is named with
  `ccy --resume` as the way back by hand.
- New D12: the plugin's hooks run only while a seat is held, so a ccy session on no team
  would never record its conversation. Under D10 it would then not be restored after a
  reboot. Its SessionStart now runs `pingbus hook conversation`, which only writes the record;
  every other event still runs nothing. This is the one exception to the inert contract.

## Second part: restart, bash restore removed, agent view (Opus, branch `feat/teams-restart-2`)

Branched from `feat/teams-restart` 0a13f8c4. F44 226a292d merged in last. Not pushed. Each
item was written test first.

1. **The bash restore is deleted.** A grep found no caller of `ccy_registry_restore`,
   `ccy_registry_restore_args` or `_ccy_restore_manifest_merge` (the last was used only by
   the restore). All three are deleted, and so are their tests. The bash-limits allowlist is
   tightened to match. A failure turned up on the way: `test-ccy-restore-askpass.bash` failed
   6 checks on the branch, because the Go restore now needs a recorded conversation. Its
   fixtures now hold the SessionStart record and a transcript, and the cc record has `--bus`.
   It now passes 90 of 90.
2. **`ccy-teams restart <team> [--yes]`** is in the new package `internal/teams/restart`.
   - **Running ccy session:** `membership.WriteRestart` writes `relaunch.json` with
     `"why":"restart"`. The job is done when the session's container has changed and its
     seats are held again, within 300 s. If that does not happen in time,
     `WithdrawRestart` removes the request, but only when its why is "restart".
   - **A first join's relaunch already waiting** is not overwritten and is not withdrawn.
   - **The lifecycle plugin (1.2.0)** now gives "the owner restarted its team" as the
     restart reason.
   - **Started with `--no-supervise`:** the session fails and is named.
   - **Running cc session:** left running and named (`-`). It is not a failure.
   - **Stopped session:** started through `registry.ResumeArgs` with `--resume <id>`. The job
     is done when its seats are held, within 180 s. With no recorded conversation it fails,
     is named, and nothing is started.
   - **Left out and named:** LXC members, and member or held seats that no record names. A
     member left out is not a failure.
   - **Reporting:** one ✓, ✗ or - line per session; the other sessions go on after a
     failure; the exit code is 1 if any session failed.
   - **Play lock:** nothing in restart runs a play, so it never waits for the play lock
     (B10). F44's Task 2.18 wait covers the commands that do run plays.
   - **Menu line:** "Restart the team's desktop sessions" is a visible line with no key,
     placed after Update and before Shut down and Take down. Its screens open with the
     heading `Teams › <team> › Restart`, and seats are named by their full handles. The
     help.txt paragraph is back, and usage.txt and docs/agent-bus.md describe the command.
   - **Tests, failing first:** membership (vet: undefined WriteRestart); the plugin
     (`test_a_team_restart_request_says_the_owner_restarted_the_team`); restart (the package
     did not build, then 4 expectations were corrected); picker (vet: p.Restart undefined);
     and app usage and dispatch. All now pass.
3. **D11:** both `CLAUDE_CODE_DISABLE_AGENT_VIEW` and `disableAgentView` are in the Claude
   Code 2.1.303 binary that this container runs. The image installs it from npm, unpinned.
   The binary checks the environment variable before the setting. I used the environment
   variable, as D11 says: `ENV CLAUDE_CODE_DISABLE_AGENT_VIEW=1` in the Dockerfile.
   Background tasks are left on. `test-ccy-restart-request.bash` checks that the variable is
   present and that no background-task switch is set; it failed (0, wanted 1) before the
   change and passes after it.
4. **Version placeholders:** CCY 3.99.12.17, container 2.77.13, not the .15 and .11 that
   were asked for. The pre-commit hook requires a new version in every commit that stages
   ccy or image files, and three of my commits did: the bash deletion, the agent view, and
   the merge of F44 de85f5f6. That merge brought the goodbye fix in at 3.99.12.14 /
   2.77.10. meta-deploy has one `play-agent-bus.yml` entry, then one `play-claude-yolo.yml`
   entry; each comment covers both Plan 00194's goodbye and this work. My changelog section
   is the top one and follows 3.99.12.14.

QA run, all targeted and with PINGBUS\_\* unset: go test for teams, sessions, registry and
conversation; golangci-lint; the project rules (vet tool); qa-hidden-bindings;
qa-bash-limits; test-ccy-teams-command, -session-registry, -lifecycle, -restart-request,
-restore-askpass, -network-disconnect, -sessions-reboot and -tmux-targets; and the pingbus
and ccy_lifecycle Python tests.

## Third part: merged over the member roles (Opus, branch `feat/teams-restart-3`)

Branched from `fix/teams-member-roles-3` 914e673f (Fable: MERGE), which goes into F44 just
before this. `feat/teams-restart-2` d1bed1b2 (Fable: MERGE, review copied in as
`261019-team-restart-review-fable.md`) merged with `--no-ff`. Not pushed.

1. **The team menu.** Both sides kept: member-roles' one role path, `memberActions`, the
   handle-less guard (no role and no Park on a row with no handle) and `lastDestructive`;
   restart's `actRestart`, the restart package and the line "Restart the team's desktop
   sessions", right after Update. Restart is not destructive (it asks first, ends nothing),
   so `lastDestructive` leaves it there and moves Shut down and Take down below help, just
   above Back. The team menu is now Update, Restart, help, Shut down, Take down.
2. **Tests changed for that order.** `TestATeamsMenuRestartsItsDesktopSessions` asked for
   Restart *just* above Shut down; help now sits between them, so it asks for Restart after
   Update and before Shut down (SPEC B1's words) and runs `checkOrder`, the destructive-last
   rule, on the team menu. The picker header test takes the merged key line: `restart ... via Enter`, then `? help`, then the two destructive lines.
3. **Versions:** CCY 3.99.12.16, container 2.77.12 (`CCY_VERSION` and its comment,
   `REQUIRED_CONTAINER_VERSION`, the Dockerfile label). The changelog's new top section
   "3.99.12.16 — container 2.77.12" follows 3.99.12.15 (container 2.77.11); the 3.99.12.15
   section is unchanged. `ENV CLAUDE_CODE_DISABLE_AGENT_VIEW=1` (D11) is in the Dockerfile.
   PLAN.md Task 3.12 names the new numbers; the stale Task 3.9 placeholder of the member-roles
   side is dropped in favour of this branch's Tasks 3.10-3.12.
4. **meta-deploy.list:** header lines 1-21 as on F44; one `play-agent-bus.yml` entry, then
   one `play-claude-yolo.yml` entry, each comment the union of Task 2.6, Task 3.8 and this
   branch's Tasks 3.10-3.12. The Plan 00194 Task 2.0b text is gone: F44 5f93ea93 records it
   deployed. The image line says 3.99.12.16 / 2.77.12 carries all of it.
5. **The rest:** journal 26-10-19 is the union of both sides in time order; the
   `test_hooks.py` conflict keeps both `RoleNoticeHookTest` and `ConversationRecordTest`; the
   bash-limits allowlist takes this branch's lower `session-registry.bash` row (865 36 871
   data), the member-roles side not touching it.

QA, targeted, PINGBUS\_\* unset: `go test -race` over `internal/teams/...`, `conversation`,
`registry` and `sessions` (all ok); golangci-lint (0 issues); test-ccy-teams (214/0),
test-ccy-teams-command (383/0), test-ccy-lifecycle (223/0), test-ccy-restart-request
(180/0), test-ccy-restore-askpass (90/0); the pingbus, agent_bus, agent_bus_lxc and
ccy_lifecycle Python tests (2496, OK); qa-bash-limits and qa-hidden-bindings (OK);
`ansible-playbook --syntax-check` on play-agent-bus.yml and play-claude-yolo.yml (OK).
qa-all not run.
