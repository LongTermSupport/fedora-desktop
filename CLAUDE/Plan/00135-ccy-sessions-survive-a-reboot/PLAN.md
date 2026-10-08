# Plan 00135: ccy sessions survive a reboot

**Status**: In Progress
**Created**: 2026-09-22
**Owner**: joseph
**Priority**: High

Implements [fedora-desktop#44](https://github.com/LongTermSupport/fedora-desktop/issues/44).

## Overview

A ccy session today dies with the machine and does not come back. Plan 00111 made a
session survive its *terminal* by moving it onto a tmux server under `systemd --user`;
this plan makes it survive a *reboot*. Two halves: warn every live session before the
machine goes down, and restart the recorded ones after it.

The warning half is a thin caller. The hooks-daemon already owns the signal channel and
its CLI shipped in v3.65.0, so nothing here composes a message — the helper names a
signal kind and an integer and nothing else. The restore half is the real work: ccy
writes no per-session state today, so a boot has nothing to read.

Restore is opt-in per machine. A laptop that is rebooted daily does not want four agents
resuming at login, and the default must stay exactly what it is now.

## Goals

- Every live ccy session is warned N minutes before a deliberate reboot, and again at one
  minute, through the daemon's existing signal CLI.
- A session running when the machine went down is running again after it, in the same
  project directory, resumed (`--continue`) and supervised.
- A machine that has not opted in behaves exactly as it does today.
- A project whose daemon CLI is missing is a loud refusal, never a silent skip.

## Non-Goals

- No message channel, no free text, no prompt path into a session. The helper may name a
  signal kind and an integer; that is the whole vocabulary.
- Not a crash-recovery feature. An unclean power loss leaves stale records by design —
  that is exactly how restore knows what was running.
- Not a scheduler. This plan reboots on request; it does not decide when to reboot.
- Does not change how `ccy` starts a session interactively, or the picker's behaviour.

## Context & Background

The facts established by reading before any code was written, each load-bearing for a
task below, are in [DECISIONS.md](DECISIONS.md#context--background).

## Tasks

### Phase 1: The session registry

> Independent of the daemon CLI and of restore. Lands first; nothing else needs it to
> exist to be useful, and it is what makes restore possible at all.

- [x] ✅ **Task 1.1**: Registry format and location. One file per session under
  `~/.local/state/ccy/sessions/`, named for the tmux session. Records: tmux session name,
  project directory, launcher, prefix, restore flag, and the launch arguments with one-shot
  arguments removed. `lib/session-registry.bash`; location decision in the 26-09-22 journal.

  **The one-shot set is already enumerated with file:line citations** in
  [research/launcher-facts.md](research/launcher-facts.md) §4 — use it rather than
  re-deriving. Three things from it change this task:

  - **`--prevent` is destructive to replay.** It writes `never` into
    `.claude/ccy/allowed-hostnames`, disabling ccy for that project. A restore that
    replayed argv verbatim would turn ccy off for the project it was restoring. This is
    the case that makes the filter load-bearing rather than tidy.
  - **`--continue` is not a ccy flag at all** — it is Claude Code's own and falls through
    to `CLAUDE_ARGS` (`claude-yolo:638-640`). The plan's "plus `--continue`" is a
    passthrough, not a ccy option.
  - **`--ssh-agent` needs a decision**: the agent socket differs after a reboot, so
    replaying it points at a socket that no longer exists.

- [x] ✅ **Task 1.2**: Write on start, delete on clean exit, both inside
  `ccy_tmux_insulate`. The write is there; the delete rides in the pane's trampoline
  (`ccy_registry_trampoline`), which runs when the launcher returns and not when the pane
  is killed. `ccy-sessions` Ctrl-X removes the record itself, since a `kill-session` never
  reaches the trampoline.

- [x] ✅ **Task 1.3**: `--no-restore` marking, consumed by the insulation; neither launcher
  sees it. `cc` strips it again on the paths where insulation does not apply.

- [x] ✅ **Task 1.4**: `scripts/test-ccy-session-registry.bash`, wired into `qa-all.bash`.
  Covers the write, the clean and failing exits, the **kill** (the real trampoline under
  `kill -KILL`), the one-shot filter case by case, `--no-restore`, a directory with spaces,
  and the malformed-record rejections.

### Phase 2: The restore service

- [x] ✅ **Task 2.1**: `ccy-sessions-restore.service`, `systemd --user`,
  `WantedBy=default.target`, running `ccy-sessions restore`. Each record is started through
  `ccy_tmux_start_detached` — the same function the interactive start now uses — with the
  recorded arguments plus `--continue`, and `--supervise` for `ccy` (not `cc`, which
  forwards its argv to `claude`).
- [x] ✅ **Task 2.2**: `ccy_restore_sessions` (`| default(false)`), declared in `host_vars`.
  Linger untouched; the play comment names its owner.
- [x] ✅ **Task 2.3**: In `play-claude-yolo.yml`: enable (or remove the wants-symlink when
  not opted in), reload as its own task, read back `list-dependencies default.target` and
  assert the live graph matches the opt-in either way.
- [x] ✅ **Task 2.4**: Decided (journal 26-09-22): vanished directory → error, record kept,
  run continues, unit ends failed; live name → skip and say so; unreadable live list →
  start nothing. Documented in `docs/ccy.md`.
- [x] ✅ **Task 2.5**: The translation is tested in `test-ccy-session-registry.bash` over a
  stubbed live list and a recording stub for the start; the executable's `restore`
  subcommand is exercised headless in `test-ccy-sessions-reboot.bash`.

### Phase 3: The reboot helper

- [x] ✅ **Task 3.1**: `ccy-sessions` is a dispatcher: bare = picker, `notify`, `reboot`,
  `restore`, `--help`. The TTY guard sits under the dispatch, on the picker path only.
  Usage mistakes exit 64, refusals exit 1.

- [x] ✅ **Task 3.2**: `notify reboot-warning --minutes N`, `notify shutdown-warning`,
  `notify reboot-cancelled`. Every live project is checked for a daemon CLI **before** any
  is signalled, so a refusal leaves no project half-warned. The CLI is run inside the
  container for a project with a ccy session, and asked for `signal --help` first
  (journal 26-09-25: a ccy-only project's host CLI had no venv).

- [x] ✅ **Task 3.3**: `reboot --in N [--dry-run]`: warn N, wait, warn 1, wait, `systemctl reboot`. `--in 1` warns once. Minutes are a positive integer or a usage error.

- [x] ✅ **Task 3.4**: `scripts/test-ccy-sessions-reboot.bash`, wired into `qa-all.bash`:
  the real executable under a fake `tmux`, a fake `systemctl` and a per-project logging
  stand-in for the daemon CLI, with the minute shortened to zero.

- [x] ✅ **Task 3.5**: `reboot-with-update [--in N]` (owner's request, 26-09-22):
  `shutdown-with-update` under a second name, symlinked by `play-basic-configs.yml`. Same
  updates, then `ccy-sessions notify` as the invoking user for the two warnings, then
  `systemctl reboot` as root (polkit refuses a plain user's reboot over SSH). Rehearses the
  warning with `--dry-run` before updating, so an unwarnable session refuses early.
  🧑 Not unit-tested: the body is dnf and firmware; Phase 5 proves it.

- [x] ✅ **Task 3.6**: `shutdown-with-update` warns too (owner's decision, 26-09-22). The
  rehearsal, the two warnings and the countdown run under both names; the name chooses the
  signal kind (`shutdown-warning` or `reboot-warning`) and the last step. Both now need
  `SUDO_USER`. Open decision 3 below is thereby reversed.

- [x] ✅ **Task 3.7**: review fixes to 3.6 (`subagent-reports/260923-t36-fixes-opus-5.md`):

  - The warning kind follows the **restore opt-in**, not the power action. The daemon's
    two texts encode "a restore will follow" and "NO restore, leave a handoff". So
    `ccy-sessions notify going-down` sends `reboot-warning` when this user's restore unit
    is enabled and `shutdown-warning` when it is not. `ccy-sessions reboot` and both
    names of `shutdown-with-update` use it.
  - **Known and not fixed here:** the same texts name the action. With restore on, a
    shutdown reads "will reboot"; with restore off, a reboot reads "will shut down". What
    the agent is asked to do is right; the verb is not. Fixing that is a hooks-daemon
    change: a kind for "restore follows" separate from the action. It has **not** been
    filed, because the tracker is public; that is the owner's call.
  - From the first warning on, any exit that is not the machine going down sends
    `reboot-cancelled` and exits non-zero. That covers a warning that fails part-way or
    at one minute, Ctrl-C, a blocked shutdown answered N, no terminal to ask on, and a
    forced poweroff or reboot request that fails.
  - A root shell (`SUDO_USER=root`) is refused before anything runs.
  - A dangling wants-symlink reads as restore off.
  - Each case above is driven under fakes in `scripts/test-ccy-sessions-reboot.bash`. A
    real `shutdown -h now` blocked by inhibitors is left to Task 5.7.

- [x] ✅ **Task 3.8**: fixes ported from the superseded PR #47's review
  (`subagent-reports/260923-pr47-ports-opus-5.md`), CCY 3.62.0:

  - `ccy-sessions reboot` withdraws its warning on every non-zero exit, as Task 3.7 made
    `shutdown-with-update` do. A withdrawal carries on past a project that refuses it.
  - Derived guards in `test-ccy-session-registry.bash`: every launcher flag has a replay
    decision, and every `read -p` prints a registered prompt or is listed as unreachable.
    The compose-stop and token-setup prompts are now registered.
  - A registry path that exists but cannot be listed fails the restore.

### Phase 4: Docs

- [x] ✅ **Task 4.1**: `docs/tmux-sessions.md`: the row stays "gone" for plain tmux
  sessions (true), and the sentence beneath it says which sessions are the exception and
  on which machines.
- [x] ✅ **Task 4.2**: `docs/ccy.md` "Sessions Survive a Reboot": registry, opt-in, the
  restore decision table, the replay filter, the prompt behaviour, the reboot helper and
  its refusal; command-reference and troubleshooting rows; `docs/ccy-changelog.md` 3.60.0.

### Phase 5: Proof on a real machine

> **HOST/VM ONLY — cannot be done in the CCY container.** Whoever executes this plan needs
> a machine they can reboot. This phase is the deliverable, not a formality: every task
> above can pass its unit tests and still not restore a session.

- [ ] 🧑 **Task 5.1**: Open two ccy sessions in different projects. Confirm two records
  exist. Exit one cleanly; confirm its record is gone and the other's remains.
- [ ] 🧑 **Task 5.2**: `ccy-sessions reboot --in 2`. Confirm the warning is visible **in
  each session**, and that the one-minute signal arrives.
- [ ] 🧑 **Task 5.3**: Let it reboot. After boot, without logging in if linger is the
  claim being tested, confirm the sessions are back, in the right directories, resumed and
  supervised.
- [ ] 🧑 **Task 5.4**: Confirm a machine with the opt-in **off** restores nothing.
- [ ] 🧑 **Task 5.5a**: Re-verify Plan 00111's terminal-death guarantee, which this plan
  restructured (the start and the attach are now two tmux calls): start a session, kill
  the terminal emulator, confirm the session survives and `ccy` in that directory offers it.
- [x] ✅ **Task 5.5b**: A restored `ccy` session runs with the supervisor ARMED
  (`--supervise`), which the original may not have. The owner confirmed on 2026-09-24
  that this is wanted, because a restored session runs with nobody watching it.
- [ ] 🧑 **Task 5.6**: `reboot-with-update --in 2` over SSH with two sessions open: the
  dry-run rehearsal passes, updates run, both warnings arrive, the machine reboots as root,
  the sessions come back.
- [ ] 🧑 **Task 5.7**: `shutdown-with-update --in 1` with a session open, once per opt-in
  state. Read the text the session actually receives, not just that a warning arrived:
  - restore **on**: it says a session restore will follow, the machine powers off, and
    the session is back after the next boot;
  - restore **off**: it says NO restore will follow and asks for a handoff, the agent
    leaves one, and nothing is restored.
- [ ] 🧑 **Task 5.5**: Put the evidence in the PR description.

### Phase 6: a restored session stops at the SSH key passphrase

The reboot on 2026-09-25 restored the session, and it stopped at ccy's key selection and
passphrase prompt. That is acceptable on the desktop, where the owner logs in anyway.
It is not acceptable on the server, where nobody is present. Four independent
brainstorms are in [`brainstorm-ssh-key-restore/`](brainstorm-ssh-key-restore/BRIEF.md).

- [x] ✅ **Task 6.1**: **Owner decision: the restore-only `SSH_ASKPASS`, alone.** The owner
  accepted the recommendation; the TPM seal and the lazy unlock are not built.
  Choose how a restored session on the server
  unlocks its key. Every brainstorm ranks the same answer first: a restore-only
  `SSH_ASKPASS` fed from the vault's `github_ssh_passphrase`, the same way
  `run.bash --headless` already unlocks it. The trade-off is that anyone who can read
  both the key and the vault password file on that disk can use the key. Two options add
  to it rather than replace it:
  - seal the passphrase to the TPM with `systemd-creds`;
  - leave the session's key locked until its first push, and report it as pending.
- [x] ✅ **Task 6.2**: Implement the decision, tests first. CCY 3.72.1, container 2.40.
  Applies where `provisioning_profile` is `server`, restore is on, and `github_accounts` is
  not empty. `play-claude-yolo.yml` writes the passphrase to a 0600 file, then adds a drop-in
  that names it to the restore unit. `ccy-sessions restore` checks the file before it starts
  anything, drops it from its environment so tmux never holds it, then hands its path to each
  `ccy` session's command. The host probe and the container entrypoint each `ssh-add` through
  their own askpass copy, and each copy is removed once its key is added, or by a trap if the
  launcher is killed mid-probe. The container gets only the stage's mount; the entrypoint sets
  `SSH_ASKPASS` for its own `ssh-add`, so `podman exec` never sees it.
  `scripts/test-ccy-restore-askpass.bash` tests this with the real ssh-agent. Choices, the
  ssh-add retry-loop finding and the review fixes: journal 26-10-02; the review is
  [`subagent-reports/261002-qa-reviewer-t62-opus.md`](subagent-reports/261002-qa-reviewer-t62-opus.md).
- [ ] 🚫 **Task 6.3**: 🧑 HOST, owner only: on the server, run `CLAUDE/Plan/meta-deploy.bash`,
  which runs `play-claude-yolo.yml`. Then open a `ccy` session that names its key, and reboot
  with `ccy-sessions reboot --in 2`. Without logging in, `ccy-sessions verify-restore --wait 300`
  must report `OK` with no `WAITING-AT-PROMPT ssh-passphrase`. Blocked until the owner has a
  reboot window on the server.
  - [x] ✅ The desktop half: the owner's meta-deploy run of `play-claude-yolo.yml` (CCY 3.72.1)
    passed. Both write tasks skipped and both "where it does not apply" removals reported
    `ok`, so no passphrase file or drop-in exists there.

### Phase 7: fedora-desktop#69, three restore defects

A planned restart of a host with seven recorded sessions found three defects (issue #69).
Report: [`subagent-reports/261006-issue69-fixes-opus.md`](subagent-reports/261006-issue69-fixes-opus.md).

- [x] ✅ **Task 7.1**: `verify-restore` fails on its first session with
  `can't find pane: =<session>`. Its `capture-pane -t "=${name}"` needs the pane form
  `=${name}:`. A sweep found no other pane command with an `=name` target. The fake tmux in
  `test-ccy-sessions-reboot.bash` stripped the `=`, which is why the suite passed; it now
  refuses an `=name` pane target without the colon, as real tmux does, and the suite's ten
  `verify-restore` cases were red against the old target, green after.
  - [ ] ⬜ The issue also asks for a test against a REAL tmux server. tmux is not in the CCY
    image, so that test cannot run in a session here. Adding tmux to the image (Dockerfile,
    container bump) is the route; it then runs only in sessions started on the new image.
- [x] ✅ **Task 7.2**: a key chosen at the SSH key menu goes into the session's record as
  `--ssh-key <file>`, so its restore never shows the menu. Only records that skip Quick
  Launch (`--token`, `--network`, `--no-network`) take it; Quick Launch holds the key for
  the rest. An agent or "no key" is not recorded, and `verify-restore` names that session
  `WAITING-AT-PROMPT ssh-key`. CCY 3.82.0. Tests: `test-ccy-session-registry.bash` ("the SSH
  key chosen at the prompt goes into the record", "the SSH key menu, as it is printed") and
  `test-ccy-sessions-take-over.bash` ("which CCY session this process runs in"). The
  launcher's call into the new code is covered only by an awk source-order check; the
  launcher cannot run in the container. Sessions started with no flags are restored through
  Quick Launch, whose saved settings now survive a ccy version change (Task 7.5).
- [x] ✅ **Task 7.3**: `ccy-sessions reboot` asks logind's `CanReboot` before it warns
  anyone, and refuses on any answer but `yes`, pointing to `sudo reboot-with-update`. Every
  session that cannot be warned is named in the same refusal, with its options. A failed
  `systemctl reboot` is reported as a failure. `reboot-with-update` and
  `shutdown-with-update` run as root and share only the session check, through their
  `--dry-run` rehearsal. Tests: `test-ccy-sessions-reboot.bash` ("everything that would stop
  it is found before anyone is warned", and "systemctl refusing the reboot is a failure").
- [ ] 🧑 **Task 7.4**: HOST: after the owner's meta-deploy run of `play-claude-yolo.yml`, the
  infra agent repeats the server reboot check and runs `ccy-sessions verify-restore --wait 300`.
  Task 7.1's fix ships in the same CCY 3.82.0. Deployed on the desktop: meta-deploy
  `20261006-134254` (`run.bash --changed`, `play-claude-yolo.yml` failed=0). The server
  needs the same play from F44. Expected: a session started with no launch flags is
  restored through Quick Launch without a prompt once the host runs CCY 3.86.3 or later,
  including the first reboot after that deploy: the file an earlier ccy saved is format 1
  and is kept (Task 7.5).
- [x] ✅ **Task 7.5**: OWNER decision (2026-10-07): option A, keep the saved Quick Launch
  settings across ccy versions when their shape has not changed. `load_launch_config`
  deleted them on any ccy version change. It now keeps them while the file's format
  (`SAVED_CONFIG_VERSION`, the existing `CONFIG_VERSION=1`, raised only when the keys or
  their meaning change) matches, and discards them when the format differs or is missing or
  a choice key is missing. Every existing file is format 1 with the same keys, so it is
  kept. CCY 3.86.3. Also unblocks Plan 00161's U20 prerequisite, which now checks the
  format, not the version. Tests: `test-ccy-teams.bash` ("Quick Launch across ccy versions").
  [report](subagent-reports/261007-quick-launch-across-versions-opus.md).

### Phase 8: fedora-desktop#88, a restored session is set going, and verify waits for it

The infra agent found three gaps on the restore path after unattended self-update reboots
(issue #88, body and its 2026-10-08 comment). A restored `--continue` session either starts
work on a cold prompt cache before anyone can compact it, or, with nothing queued, sits at an
empty prompt until a person types `continue`; and `fedora-desktop-self-update verify` gave up
after five minutes with five of six sessions still starting, leaving its unit failed.

- [x] ✅ **Task 8.1**: design, in this plan's journal, the restore-time context check: how
  `ccy-sessions restore` reads a restored session's context size once its prompt has drawn,
  and the floor at or above which it compacts. Grounded in what the pane or the transcript
  actually shows, not assumed. Journal 26-10-08: the prompt is Claude's framed `❯` input
  box (measured on 2.1.293), the size is the transcript's last main-thread usage or
  compaction boundary, the floor is `ccy_restore_compact_floor_tokens` (default 150000).
- [x] ✅ **Task 8.2**: `ccy-sessions restore` sets each restored session going once its prompt
  has drawn, before any work turn: at or above the floor it sends `/compact` (the
  supervisor's continue-after-compaction carries the session on); below it, `continue`. A
  session whose prompt cannot be read is left untouched and named in the output, never
  skipped silently. Built as `ccy-sessions set-going`, run by
  `ccy-sessions-set-going.service` after the restore (the restore's `Wants=`), so the wait
  never holds `default.target`; the manifest (format 2) records what each session got.
  CCY 3.87.0. Gated by `scripts/test-ccy-sessions-reboot.bash` and
  `scripts/test-ccy-session-registry.bash`.
- [x] ✅ **Task 8.3**: `verify-restore` reports a continuing session whose compaction (or
  `continue`) did not start within a short window, instead of counting it OK.
  `NOT-SET-GOING <compact|continue>-not-started` when the transcript shows no input within
  `CCY_SESSIONS_START_WINDOW` (120 s); `NOT-SET-GOING <reason>` for one left alone.
- [x] ✅ **Task 8.4**: `fedora-desktop-self-update verify` waits until the restore settles
  (every recorded session running, or definitely failed), not a fixed five minutes, and a
  passing `verify` clears its unit's failed state. The verify unit belongs to Plan 00137's
  self-update; the change is made here because the wait is on this plan's restore.
  `verify-restore --wait` stops once settled; the ceiling is 1500 s; a pass runs
  `systemctl reset-failed fedora-desktop-self-update-verify.service`. Gated by
  `tests/helpers/self_update/test_cycle.py` and `scripts/test-self-update-cycle.bash`.
- [ ] 🧑 **Task 8.5**: HOST: after an unattended self-update reboot with several sessions,
  every session is running and past its prompt, `verify` exits 0 and `systemctl --failed` is
  empty (the infra agent's acceptance on the server; the owner's meta-deploy on the desktop).

## Dependencies

- `claude-code-hooks-daemon` ≥ 3.65.0 for `hooks-daemon signal` (their #39, closed).
  **Verified present** in this checkout's installed clone.
- Plan 00111 (Completed) — the tmux server under `systemd --user`, `ccy-sessions`, and the
  re-attach offer. This plan extends all three.

## Open decisions — settled; reasoning in the 26-09-22 journal

1. **Bought here: the helper, not an inhibitor.** The helper owns the deliberate case and
   the countdown; a plain `systemctl reboot` warns nobody, and the docs say so. A
   `--what=shutdown` inhibitor that warns on every path is a separate change with its own
   argument (it delays every reboot, including unattended ones) and is not started here.
2. **Replay filtered argv.** The quick-launch path is a prompt, so it does not remove the
   interactivity, and it holds only token/ssh/network. A restored session runs in a real
   pane, so a launch that named its settings comes back unattended and one that answered
   prompts asks again, visibly.
3. **`shutdown-with-update` warns too** — reversed by the owner on 26-09-22 after Task 3.5
   made the two names one script (Task 3.6). The earlier position, that an updater which
   shuts down and a warner which reboots need not know about each other, stopped holding
   once they were the same file.

The questions as first asked, before they were settled, are in
[DECISIONS.md](DECISIONS.md#the-open-decisions-as-first-asked).

## Technical Decisions

- **Registry, not a shutdown hook.** A shutdown hook races the shutdown it is reacting to
  and an unclean power loss never runs one. A record written at start and deleted at clean
  exit means "still present at boot" == "was running when the machine went down", with no
  timing assumption at all.
- **`ccy_tmux_insulate` owns both the write and the delete.** Splitting them across two
  call sites is how they drift.
- **The helper never composes text.** A signal kind and an integer. This keeps a reboot
  notification from becoming an injection path into a running agent.
- **Restore is opt-in.** The default stays today's behaviour, so deploying this plan
  changes nothing until a machine asks for it.

## Success Criteria

- [ ] A session running at reboot is running after it, in the right directory, resumed.
- [ ] Each live session's project is signalled exactly once per warning, and again at one
  minute.
- [x] A project missing the daemon CLI causes a loud refusal and **no reboot**.
  `scripts/test-ccy-sessions-reboot.bash`: "a missing daemon CLI refuses the reboot", and
  the same inside a ccy container.
- [x] `--dry-run` signals nothing and reboots nothing. Same script: "dry run signals no
  daemon", "dry run invokes no systemctl".
- [ ] A machine with restore disabled behaves exactly as before this plan.
- [x] `./scripts/qa-all.bash` green (1142 files, run on F44 after `a8480fec`; ccy-sessions-reboot 156 passed).
- [x] ✅ `qa-reviewer` agent over the full plan diff, findings resolved (round 1 FIX-BEFORE-MERGE, round 2 PASS; journal 26-09-23).
- [x] No hostname, address, username or private path anywhere in the diff or the PR.
  The added lines and messages of all 42 commits naming the plan were scanned with the
  pre-commit hook's own `localhost.yml` denylist (no field matched) and for home paths,
  IPv4 addresses and emails (none outside placeholders). The work went straight to F44;
  there was no PR of its own.

## Out of Scope — tracked separately

**The firewalld / sshd-port item from the original brief is NOT in this plan**, and should
not be bolted onto it: different subsystem, different risk, and the brief's premise does
not survive reading the code.

`playbooks/imports/play-lxc-install-config.yml:127-132` enables firewalld's stock `ssh`
service, and the very next block (`:144-177`) already discovers sshd's real ports via the
tested `helpers/sshd_ports` helper and permits every one of them. The comment at `:134-138`
states the concern the brief raises, in its own words, as the reason that second block
exists.

So the stock enable is not an oversight — it is a deliberate anti-lockout guard for a
*first-ever* firewalld start on a remote headless VM, where the alternative to opening 22
is potentially losing the only access path. Removing it to honour a port variable would
delete that guard. What it actually costs today is an open port 22 that nothing listens
on: attack surface and noise, not a lockout.

That is worth fixing, but it is a different change with a different argument, and it needs
its own issue so the anti-lockout reasoning is weighed rather than silently dropped.

**A second site strengthens the case for that issue**: `files/usr/local/bin/ssh-suspend-guard:36`
still hardcodes port 22. So "SSH is on 22" is assumed in two independent places, and a
machine that moved sshd has one of them silently not doing its job. Also worth recording
in that issue: **no sshd port variable exists** in `vars/` or `environment/`, and that is
deliberate — `play-lxc-install-config.yml` *discovers* the ports from `sshd -T` through
`helpers/sshd_ports/cli.py` rather than declaring them. Adding a declared variable would
introduce a second source of truth that can disagree with the daemon's own answer. The
brief asked for exactly that variable, which is the part most worth re-examining before
anyone builds it.

## Delivery & Milestones

- Each phase commits and pushes separately, in order: registry → restore → helper → docs.
- Phases 1 and 2 do not depend on the daemon CLI and can land before Phase 3.
