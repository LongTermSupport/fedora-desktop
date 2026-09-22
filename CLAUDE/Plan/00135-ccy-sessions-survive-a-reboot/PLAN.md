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

Established by reading, before any code was written. Each is load-bearing for a task
below.

- **`ccy-sessions` already exists** (`files/home/.local/bin/ccy-sessions`, 152 lines) and
  is **picker-only**: its argument parsing accepts `""` or `-h|--help` and rejects
  everything else with exit 64. It also **hard-exits without a TTY** (`[[ ! -t 0 || ! -t 1 ]]`,
  line 42) and refuses to run inside a CCY container. `notify` will be called by an
  automated patch cycle with no terminal, so the TTY guard has to move below dispatch —
  see Task 3.1. This is a restructure of an existing tool, not a new one.
- **`lib/tmux-session.bash`** (339 lines) already exposes `ccy_tmux_list` →
  `<name> <attached-count> <directory>` per line, plus `ccy_tmux_project_sessions`,
  `ccy_tmux_next_name` and `ccy_tmux_is_detached`. Listing live sessions is solved; do not
  re-implement it.
- **`ccy_tmux_insulate <project> <command> [args...]`** (line 248) is the launcher's
  single entry point for creating a session. It is the one choke point where a registry
  write and its matching delete both belong. Anywhere else and they drift apart.
- **No per-session state is written today.** The `.claude/ccy/sessions/` paths in the
  launcher are Claude's own transcripts, not a ccy registry. The registry is genuinely
  new.
- **The daemon dependency has landed and is verified present** in the installed clone:
  `hooks-daemon signal {reboot-warning,shutdown-warning,reboot-cancelled} [--minutes N] [--all-sessions] [--project-root PATH]`.
  `--project-root` matters: the helper runs from outside each project.
- **The `systemd --user` pattern to copy** is `play-container-watch.yml` — unit file under
  `files/home/.config/systemd/user/`, explicit loop, uid resolved via `getent` + assert,
  `scope: user` plus `XDG_RUNTIME_DIR`. **Linger is not its job**:
  `play-systemd-user-tweaks.yml:23-50` already owns that.
  `play-host-health-login-report.yml` is worth reading alongside it for one trap: the
  `ansible.builtin.systemd` module runs `daemon_reload` **before** it changes the enabled
  state, so a reload requested on the enable task re-reads a directory that does not yet
  contain the symlink the enable is about to create. That play splits the reload into its
  own task *after* the enable and reads back `list-dependencies` rather than trusting
  `is-enabled`. Task 2.3 must do the same.
- **The registry path needs deciding, not assuming.** The issue says
  `~/.local/state/ccy/sessions/`, but this repo's own XDG convention is
  `$XDG_STATE_HOME/fedora-desktop` (`helpers/play_ledger/ledger.py:48-81`). Pick one
  deliberately; do not end up with two conventions because the issue named a path.
- **`docs/tmux-sessions.md:27-34`** is the table to amend, and `:36` says in prose
  "Nothing restarts them after a reboot". Both contradict this plan once it lands, so both
  change together — a table row without the sentence leaves the page arguing with itself.
- **Test harness**: `scripts/test-*.bash`, run by `scripts/qa-all.bash`. Seven
  `test-ccy-*.bash` scripts already exist to model on. There is **no** existing test for
  `tmux-session.bash`.

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
  is signalled, so a refusal leaves no project half-warned.
- [x] ✅ **Task 3.3**: `reboot --in N [--dry-run]`: warn N, wait, warn 1, wait, `systemctl reboot`. `--in 1` warns once. Minutes are a positive integer or a usage error.
- [x] ✅ **Task 3.4**: `scripts/test-ccy-sessions-reboot.bash`, wired into `qa-all.bash`:
  the real executable under a fake `tmux`, a fake `systemctl` and a per-project logging
  stand-in for the daemon CLI, with the minute shortened to zero.

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
- [ ] 🧑 **Task 5.5**: Put the evidence in the PR description.

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
3. **`shutdown-with-update` is left as it is.** It is an updater that then shuts down; the
   helper is a warner that then reboots. Making one call the other is an integration this
   plan does not need for its goals; noted for whoever next touches either.

Both of the first two came from [research/launcher-facts.md](research/launcher-facts.md)
and neither was answered by the issue. The original questions, for the record:

**1. The helper only warns when the helper is used.** The issue names "an automated patch
cycle" as a reason to want this, but a patch cycle runs `systemctl reboot`, not
`ccy-sessions reboot` — so it would warn nobody. This repo already ships the pattern that
would catch every path: `files/usr/local/bin/ssh-suspend-guard` +
`playbooks/imports/play-prevent-ssh-suspend.yml` hold a `systemd-inhibit --what=sleep`
lock in a loop as a system unit, and `--what=shutdown` is its sibling. An inhibitor would
warn on *any* reboot; the helper warns on one. The helper is still worth having (it owns
the countdown and the deliberate case), but if the patch-cycle case matters, the inhibitor
is the mechanism that actually covers it. Decide which is being bought here.

**2. Replay argv, or reuse the quick-launch path?** `--token`, `--ssh-key` and `--network`
are **already persisted per project** in `.last-launch.conf` (`claude-yolo:2880`), with a
quick-launch path at `:911-915`. Restoring through that path rather than replaying a
filtered argv would make the whole one-shot filter above unnecessary — the persisted set
contains no one-shot flags by construction. That is a materially simpler design than the
issue proposes. It needs checking that the quick-launch path covers everything a restore
needs, but it should be checked before the filter is built.

**3. `shutdown-with-update` already exists** (`files/usr/local/bin/shutdown-with-update`,
119 lines) as a user-invoked pre-shutdown updater. It is not a hook on `systemctl reboot`
— nothing intercepts a plain reboot today — but it is the existing "operator deliberately
brings the machine down" path, and two commands that both mean that should know about each
other rather than diverge.

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
- [ ] A project missing the daemon CLI causes a loud refusal and **no reboot**.
- [ ] `--dry-run` signals nothing and reboots nothing.
- [ ] A machine with restore disabled behaves exactly as before this plan.
- [ ] `./scripts/qa-all.bash` green.
- [ ] `qa-reviewer` agent over the full plan diff, findings resolved.
- [ ] No hostname, address, username or private path anywhere in the diff or the PR.

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
