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

The full text of every completed task is in [DONE-DETAIL.md](DONE-DETAIL.md), by phase and
task number. Here a completed task keeps one line.

### Phase 1: The session registry

- [x] ✅ **Task 1.1**: Registry format and location, one file per session. [detail](DONE-DETAIL.md#task-11)
- [x] ✅ **Task 1.2**: Write on start, delete on clean exit, inside `ccy_tmux_insulate`. [detail](DONE-DETAIL.md#task-12)
- [x] ✅ **Task 1.3**: `--no-restore` marking, consumed by the insulation. [detail](DONE-DETAIL.md#task-13)
- [x] ✅ **Task 1.4**: `scripts/test-ccy-session-registry.bash`, wired into `qa-all.bash`. [detail](DONE-DETAIL.md#task-14)

### Phase 2: The restore service

- [x] ✅ **Task 2.1**: `ccy-sessions-restore.service` replays each record through `ccy_tmux_start_detached`. [detail](DONE-DETAIL.md#task-21)
- [x] ✅ **Task 2.2**: `ccy_restore_sessions` opt-in variable, default false. [detail](DONE-DETAIL.md#task-22)
- [x] ✅ **Task 2.3**: `play-claude-yolo.yml` enables or removes the unit and asserts the live graph. [detail](DONE-DETAIL.md#task-23)
- [x] ✅ **Task 2.4**: Restore edge cases decided (vanished directory, live name, unreadable list). [detail](DONE-DETAIL.md#task-24)
- [x] ✅ **Task 2.5**: Restore translation tested over stubs; `restore` exercised headless. [detail](DONE-DETAIL.md#task-25)

### Phase 3: The reboot helper

- [x] ✅ **Task 3.1**: `ccy-sessions` dispatcher (picker, `notify`, `reboot`, `restore`). [detail](DONE-DETAIL.md#task-31)
- [x] ✅ **Task 3.2**: `notify` kinds; every project checked for a daemon CLI before any is signalled. [detail](DONE-DETAIL.md#task-32)
- [x] ✅ **Task 3.3**: `reboot --in N [--dry-run]`. [detail](DONE-DETAIL.md#task-33)
- [x] ✅ **Task 3.4**: `scripts/test-ccy-sessions-reboot.bash` under fakes, wired into `qa-all.bash`. [detail](DONE-DETAIL.md#task-34)
- [x] ✅ **Task 3.5**: `reboot-with-update`, `shutdown-with-update` under a second name. 🧑 Phase 5 proves it. [detail](DONE-DETAIL.md#task-35)
- [x] ✅ **Task 3.6**: `shutdown-with-update` warns too (reverses open decision 3). [detail](DONE-DETAIL.md#task-36)
- [x] ✅ **Task 3.7**: Review fixes: warning kind follows the restore opt-in; cancel on any non-going-down exit. [detail](DONE-DETAIL.md#task-37)
- [x] ✅ **Task 3.8**: Fixes ported from superseded PR #47, CCY 3.62.0. [detail](DONE-DETAIL.md#task-38)

### Phase 4: Docs

- [x] ✅ **Task 4.1**: `docs/tmux-sessions.md` names the exception. [detail](DONE-DETAIL.md#task-41)
- [x] ✅ **Task 4.2**: `docs/ccy.md` "Sessions Survive a Reboot", changelog 3.60.0. [detail](DONE-DETAIL.md#task-42)

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
- [x] ✅ **Task 5.5b**: Restored `ccy` sessions run supervised; owner confirmed 2026-09-24. [detail](DONE-DETAIL.md#task-55b)
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

- [x] ✅ **Task 6.1**: Owner decision: the restore-only `SSH_ASKPASS` alone; TPM seal and lazy unlock not built. [detail](DONE-DETAIL.md#task-61)
- [x] ✅ **Task 6.2**: Restore-only askpass on `server` profiles, CCY 3.72.1; review in `subagent-reports/261002-qa-reviewer-t62-opus.md`. [detail](DONE-DETAIL.md#task-62)
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

- [x] ✅ **Task 7.1**: `verify-restore` pane target needed `=name:`; fake tmux now refuses the bare form. [detail](DONE-DETAIL.md#task-71)
  - [ ] 🔄 The issue also asks for a test against a REAL tmux server. Built:
    `test-ccy-tmux-targets.bash`, with tmux added to `.claude/ccy/Dockerfile`. Not yet run:
    this container has no tmux until ccy restarts and rebuilds the project image. Its QA
    wiring (the `qa-all.bash` gate, the `CLAUDE/QA.md` row, CI's tmux install) is kept as
    [`tmux-qa-wiring.patch`](tmux-qa-wiring.patch) so QA stays green meanwhile: in the first
    session with tmux, run the test, `git apply` the patch, run QA, commit, delete the patch.
    Journal 26-10-09.
- [x] ✅ **Task 7.2**: A key chosen at the SSH key menu is recorded as `--ssh-key <file>`; CCY 3.82.0. [detail](DONE-DETAIL.md#task-72)
- [x] ✅ **Task 7.3**: `ccy-sessions reboot` asks logind's `CanReboot` before warning anyone. [detail](DONE-DETAIL.md#task-73)
- [ ] 🧑 **Task 7.4**: HOST: after the owner's meta-deploy run of `play-claude-yolo.yml`, the
  infra agent repeats the server reboot check and runs `ccy-sessions verify-restore --wait 300`.
  Task 7.1's fix ships in the same CCY 3.82.0. Deployed on the desktop: meta-deploy
  `20261006-134254` (`run.bash --changed`, `play-claude-yolo.yml` failed=0). The server
  needs the same play from F44. Expected: a session started with no launch flags is
  restored through Quick Launch without a prompt once the host runs CCY 3.86.3 or later,
  including the first reboot after that deploy: the file an earlier ccy saved is format 1
  and is kept (Task 7.5).
- [x] ✅ **Task 7.5**: Owner decision 2026-10-07: keep saved Quick Launch settings across ccy versions while the format matches; CCY 3.86.3. [detail](DONE-DETAIL.md#task-75)

### Phase 8: fedora-desktop#88, a restored session is set going, and verify waits for it

The infra agent found three gaps on the restore path after unattended self-update reboots
(issue #88, body and its 2026-10-08 comment). A restored `--continue` session either starts
work on a cold prompt cache before anyone can compact it, or, with nothing queued, sits at an
empty prompt until a person types `continue`; and `fedora-desktop-self-update verify` gave up
after five minutes with five of six sessions still starting, leaving its unit failed.

- [x] ✅ **Task 8.1**: Restore-time context check designed (floor `ccy_restore_compact_floor_tokens`, default 150000). [detail](DONE-DETAIL.md#task-81)
- [x] ✅ **Task 8.2**: `ccy-sessions set-going` compacts or sends `continue` once the prompt draws; CCY 3.88.0. [detail](DONE-DETAIL.md#task-82)
- [x] ✅ **Task 8.3**: `verify-restore` reports `NOT-SET-GOING` for a session that did not start. [detail](DONE-DETAIL.md#task-83)
- [x] ✅ **Task 8.4**: `fedora-desktop-self-update verify` waits for the restore to settle (ceiling 1500 s). [detail](DONE-DETAIL.md#task-84)
- [ ] 🧑 **Task 8.5**: HOST: after an unattended self-update reboot with several sessions,
  every session is running and past its prompt, `verify` exits 0 and `systemctl --failed` is
  empty (the infra agent's acceptance on the server; the owner's meta-deploy on the desktop).

### Phase 9: fedora-desktop#87, a restored session never waits at the compose question

A restore found a project's compose network with its containers stopped (`Exited (0)`) and
the session waited at `Start services with podman-compose up -d?` with nobody there. The
owner's design is the issue's last comment: the record keeps how the services stood, the
restore replays it with no person and no flag the operator passes, and no launch question
is read where nobody can answer it.

- [x] ✅ **Task 9.1**: The record keeps `compose=started|declined|running` (optional key,
  format stays 1); every rewrite carries it; the restore replays it as `--compose start|skip`;
  CCY 3.89.0. Tests: `test-ccy-session-registry.bash`, `test-ccy-compose-restore.bash`.
- [x] ✅ **Task 9.2**: Every launch question with nobody to answer (`--headless`, no
  terminal) takes its safe answer or refuses by name; a restore and a restart take the same
  safe answers. Inventory in the 26-10-09 journal.
- [ ] 🔄 **Task 9.3**: PR review and merge (owner).
- [ ] 🧑 **Task 9.4**: HOST: after the owner's meta-deploy run of `play-claude-yolo.yml` with
  3.89.0, start a session in a project with a compose stack, reboot, and
  `ccy-sessions verify-restore --wait 300` reports it `OK`, its stack up. A session started
  on an older ccy has no recorded answer and still asks once.

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
- [x] A project missing the daemon CLI causes a loud refusal and **no reboot**. [evidence](DONE-DETAIL.md#success-criteria-evidence)
- [x] `--dry-run` signals nothing and reboots nothing. [evidence](DONE-DETAIL.md#success-criteria-evidence)
- [ ] A machine with restore disabled behaves exactly as before this plan.
- [x] `./scripts/qa-all.bash` green on F44. [evidence](DONE-DETAIL.md#success-criteria-evidence)
- [x] ✅ `qa-reviewer` over the full plan diff, findings resolved (round 2 PASS). [evidence](DONE-DETAIL.md#success-criteria-evidence)
- [x] No hostname, address, username or private path anywhere in the diff or the PR. [evidence](DONE-DETAIL.md#success-criteria-evidence)

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
