# Plan 00123: ccy session registry and reboot restore

**Status**: In Progress
**Created**: 2026-09-15
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00111 made a CCY session survive its terminal: the launcher re-executes itself inside
a tmux session on CCY's own server (`tmux -L ccy`), so a dead emulator detaches a session
instead of destroying it. `docs/tmux-sessions.md` then states the remaining limit honestly
— **"Host reboots → gone. Nothing restarts them after a reboot."** This plan is that
capability, for the machine where some sessions are meant to run permanently.

Two halves, and they are independent. The first is a **session registry**: `ccy` records a
session at the moment its container is about to start, and removes the record when the
launcher exits. Anything still recorded at boot was running when the machine went down —
exact, where a shutdown hook is racy and a timer has a window. The second is a
`systemd --user` **restore service** that brings each surviving record back detached, with
`--supervise --continue`, so the conversation resumes and the supervisor nudges it back to
work.

The issue's third piece — warn every running session N minutes before a reboot — depends on
a signal kind and CLI that live in another repository
(`Edmonds-Commerce-Limited/claude-code-hooks-daemon#39`) and **do not exist yet**; verified,
see [FACTS-ccy-mechanics.md](FACTS-ccy-mechanics.md) F7. What does not need that signal is
built (session enumeration, the per-project daemon-CLI audit, `--dry-run`, the countdown and
reboot sequencing). The one call that raises the signal is a single function that fails fast
naming the upstream issue. No local substitute is invented.

## Goals

- `ccy` writes a session record at launch and removes it on exit, surviving a partial write
  by construction (atomic `rename`, plus a terminator line the reader requires).
- A `systemd --user` restore service brings surviving sessions back detached with
  `--supervise --continue`, and **cannot loop**: it consumes each record before starting it.
- Every record the restore service does not restore is **retired with a named reason** and
  reported — never silently dropped.
- `ccy` launched unattended can never park on a prompt: any prompt reached without a human
  is a fatal, named error, not a hang.
- `ccy-sessions restore-status` reports installation state and registry contents on **separate
  axes**, never multiplied into one verdict: six installation answers, of which
  `installed-state-unknown` and `enabled-linger-unknown` exist purely so "could not tell" is
  never reported as "will not run"; and independently of those, how many sessions are recorded
  and what became of the ones that were not restored.
- Restore is opt-in per machine through the play; the default is today's behaviour.
- `ccy-sessions reboot --dry-run` reports what a reboot would kill and whether each project
  is ready to be warned. The live form refuses, naming the upstream dependency.

## Non-Goals

- **Raising the `reboot-warning` signal.** Blocked on
  `Edmonds-Commerce-Limited/claude-code-hooks-daemon#39`. See Task 4.3.
- **`ccy-sessions notify` as a working command.** Same dependency; it exists only to fail
  fast with the dependency named rather than as "unknown option".
- **Restoring `cc-*` sessions.** The host `cc` wrapper is not in this repository (F5), so
  nothing here can write its records. Only `ccy` sessions are registered.
- **Validating Claude's conversation state.** `--continue` resumes whatever Claude Code
  stored; its format is Claude Code's, not ours. The failure is bounded and surfaced
  (D6), not prevented.
- **Restoring a session that never reached its container.** A record is written at the point
  of no return, so a launcher killed mid-start leaves nothing (D3). That is the intended
  answer, not a gap.
- Reviving the retired ctrl+z patch, or any change to how the supervisor is armed.

## Context & Background

- **Predecessor**: Plan 00111 (Completed) — terminal-death insulation, `lib/tmux-session.bash`,
  `ccy-sessions`, CCY 3.53.3.
- **Verified facts, and where the issue is wrong**:
  [FACTS-ccy-mechanics.md](FACTS-ccy-mechanics.md). Three of the issue's premises are
  inaccurate (F1 one-session-per-project, F4 verbatim argument passthrough, F6 the
  unattended-prompt hazard it does not mention). The design accounts for all three.
- **Failure-mode analysis and the decisions taken**:
  [DESIGN-failure-modes.md](DESIGN-failure-modes.md) — D1…D9.
- **CCY layering**: launcher + `lib/*.bash` + `entrypoint.sh` + `Dockerfile`
  (`CLAUDE/ContainerRules.md`). This plan touches the launcher and adds one library, so a
  `CCY_VERSION` bump is mandatory. The `Dockerfile` is untouched, so
  `REQUIRED_CONTAINER_VERSION` is not bumped.

## Tasks

### Phase 1: Facts and plan

- [x] ✅ **Task 1.1**: Read issue 44 in full and verify every premise against the code
  - [x] ✅ Record the verification, including the wrong premises, in `FACTS-ccy-mechanics.md`
- [x] ✅ **Task 1.2**: Analyse the unattended failure modes and record the decisions
  - [x] ✅ `DESIGN-failure-modes.md` D1…D9
- [x] ✅ **Task 1.3**: Scaffold the plan, add the `CLAUDE/Plan/README.md` row

### Phase 2: The registry, and an unattended-safe launcher

- [x] ✅ **Task 2.1**: New library `files/var/local/claude-yolo/lib/session-registry.bash`
  - [x] ✅ Record write via temp-file + `rename`, terminator line required on read (D3)
  - [x] ✅ Reader validates schema, required keys and terminator; a bad record is *quarantined*, never skipped
  - [x] ✅ Record the **resolved** launch configuration by value, not argv (F4, D5)
  - [x] ✅ `ccy_registry_restore_flags` reconstructs the launch flags from a record
  - [x] ✅ `ccy_registry_fingerprint` — the project's root commit, for the reused-directory check (D7)
- [x] ✅ **Task 2.2**: Unit-test the library — `scripts/test-ccy-session-registry.bash`
  - [x] ✅ Round-trip, partial write, missing terminator, unknown schema, path with spaces
  - [x] ✅ **The flag-classification guard**: derive every `ccy` flag from the launcher's own
    parser and fail when one is unclassified, so a future flag cannot be silently
    dropped from restored sessions (D5)
  - [x] ✅ Wire into `scripts/qa-all.bash`
- [x] ✅ **Task 2.3**: Wire the registry into `claude-yolo`
  - [x] ✅ Write the record immediately before `container_cmd run`; remove it in the existing
    `cleanup` EXIT trap (there is already one — extend it, do not add a second)
  - [x] ✅ New `--no-restore` flag, and its entry in `--help` and the `ccy_flags` validator list
- [x] ✅ **Task 2.4**: Make an unattended launch incapable of hanging (D6)
  - [x] ✅ `CCY_UNATTENDED=1` + a `read()` shadow in the launcher: a `read` **with `-p`** (a
    human prompt) is fatal and names the prompt; every other `read` passes through to
    the builtin. One seam covers every prompt site, present and future.
  - [x] ✅ Unattended defaults where a default is honest: accept the saved quick-launch
    configuration (it *is* the recorded one), decline compose start/stop — both printed
- [x] ✅ **Task 2.5**: Bump `CCY_VERSION` (minor — new feature, backward compatible) and add
  a `docs/ccy-changelog.md` entry
- [x] ✅ **Task 2.6**: Run QA — see "QA in a worktree" below for the one stage that cannot run here

### Phase 3: The restore service

- [x] ✅ **Task 3.1**: `files/home/.local/bin/ccy-sessions-restore` — non-interactive, fail-fast
  - [x] ✅ Skip records from the **current** boot as live (D1) — the second brake beside D2
  - [x] ✅ Consume the record into `attempted/` *before* starting, so no retry loop can exist (D2)
  - [x] ✅ Retire with a named reason: `no-restore`, `directory-gone`, `not-a-git-checkout`,
    `different-project`, `stale`, `malformed` (D4, D7, D8)
  - [x] ✅ Start under `systemd-run --user --scope --collect`, or a oneshot's cgroup teardown
    kills the tmux server it just started (D9)
  - [x] ✅ Gather semantics: process every record, report every failure, exit non-zero
    (`CLAUDE/PlanScriptStandards.md` R7's read-only leg semantics, applied to a service)
  - [x] ✅ Unit-test it — `scripts/test-ccy-session-restore.bash`, wired into `qa-all.bash`.
    Drives the **real** script through every retirement reason against real git repositories,
    asserting both the outcome and that the reason was recorded. Boot-time code nobody watches
    cannot be verified by reading, so `CCY_LIB`/`CCY_LAUNCHER` are overridable for this and
    stated to be so at the definition.
- [x] ✅ **Task 3.2**: `files/home/.config/systemd/user/ccy-sessions-restore.service`
  - [x] ✅ `Type=oneshot`, `WantedBy=default.target`, no `Restart=`
- [x] ✅ **Task 3.3**: Deploy it from `playbooks/imports/play-claude-yolo.yml`, opt-in
  - [x] ✅ `ccy_restore_sessions` (default `false`) in `vars/container-defaults.yml`
  - [x] ✅ When true: install + enable the unit, and depend on the linger that
    `play-systemd-user-tweaks.yml` already establishes
  - [x] ✅ When false: the unit is **absent**, so `systemctl --user is-enabled` is the honest
    source of truth rather than a second one
- [x] ✅ **Task 3.4**: Run QA: `./scripts/qa-all.bash`

### Phase 4: `ccy-sessions` subcommands

- [x] ✅ **Task 4.1**: Give `ccy-sessions` subcommands without changing the no-argument picker
- [x] ✅ **Task 4.2**: `ccy-sessions restore-status` — six distinct installation answers, two of
  them "could not tell", reported on an axis of their own from the registry contents (D8).
  Driven through every state by `scripts/test-ccy-sessions-status.bash`
- [x] ✅ **Task 4.3**: `ccy-sessions reboot` / `notify` — everything except the signal
  - [x] ✅ `reboot --dry-run`: enumerate live sessions, audit each project for the daemon CLI,
    print what would be signalled, and **refuse if any project lacks it** (no silent skip)
  - [x] 🚫 `reboot --in N` and `notify`: **BLOCKED** on
    `Edmonds-Commerce-Limited/claude-code-hooks-daemon#39`. One function,
    `ccy_reboot_raise_signal`, fails fast naming the issue. Nothing is signalled and
    nothing is rebooted, so the command cannot claim to have warned anyone.
- [x] ✅ **Task 4.4**: Run QA: `./scripts/qa-all.bash`

### Phase 5: Documentation

- [x] ✅ **Task 5.1**: `docs/tmux-sessions.md` — a host-reboot row for restore enabled
- [x] ✅ **Task 5.2**: `docs/ccy.md` — the registry, the restore service, the subcommands, and
  what is blocked
- [x] ✅ **Task 5.3**: `docs/ccy-changelog.md` entry (with Task 2.5)

### Phase 6: Review and hand-off

- [x] ✅ **Task 6.1**: every QA stage green bar the one named above; all four `test-ccy-*`
  suites for this plan pass (registry 100, restore 44, status 42)
- [x] ✅ **Task 6.2**: `qa-reviewer` agent over the full branch diff; every finding acted on.
  Reports under `subagent-reports/`, narrative in `JOURNAL/`
  - [x] ✅ Round 1 — 21 findings, all resolved (CCY 3.59.0)
  - [x] ✅ Round 2 — all 21 verified fixed, none relocated; 13 new, all resolved (CCY 3.59.1)
  - [x] ✅ Round 3 — 13 verified (3 partial); 4 new, all resolved (CCY 3.59.2). Two were tests
    of mine that passed without exercising anything, which is the defect class this plan
    keeps meeting; both suites now drive the branches they claimed
- [ ] ⬜ **Task 6.3**: Open the PR. **Do not merge** — the owner reviews and merges
- [ ] ⬜ **Task 6.4**: (HOST, owner) deploy and verify — see the HOST tasks below

## QA in a worktree — one stage cannot run here

`./scripts/qa-all.bash` runs green in this worktree **except** `qa-ansible-syntax.bash`: it
needs a vault password file that no clean checkout has and that a worktree is deliberately not
seeded with, so the mandatory pre-commit gate cannot pass in any worktree of this repo. Not
fixed here — that file sits behind the daemon's `secret_file_guard`, which says only a human may
lift it. Playbook changes were verified with the form the guard permits.

Full reasoning, the verification command, the three options for the owner, and the two smaller
worktree gaps that *were* fixed (one a real public-repo leak hazard in `.claude/.gitignore`):
[WORKTREE-QA-GAP.md](WORKTREE-QA-GAP.md).

## HOST tasks — for the owner, not for a container

Nothing below can run in a CCY container (`CLAUDE/ContainerRules.md`). Run each from the
repository root on the host.

1. Deploy the launcher, the library, the helper and the unit:
   `./playbooks/imports/play-claude-yolo.yml`
2. Opt in to restore on this machine, if wanted:
   `./playbooks/imports/play-claude-yolo.yml -e ccy_restore_sessions=true`
3. Confirm the status is honest, before and after opting in:
   `ccy-sessions restore-status`
4. Start a session, check a record appeared, exit it, check the record went:
   `ccy` in a project → `ccy-sessions restore-status` → `/exit` → `ccy-sessions restore-status`
5. Dry-run the reboot audit: `ccy-sessions reboot --dry-run`
6. The real test: start a session, reboot, then after login confirm the session is back
   detached and holds its conversation — `ccy-sessions`, attach it.
7. Negative test, because this is the part that bites: start a session, rename its project
   directory, reboot, and confirm the record retires as `directory-gone` with the reason in
   `journalctl --user -u ccy-sessions-restore --no-pager`.

## Dependencies

- Depends on: Plan 00111 (Completed) — the tmux server, `lib/tmux-session.bash`, `ccy-sessions`.
- Depends on: `play-systemd-user-tweaks.yml` for linger and a running user manager. Already
  in `playbook-main.yml`, `scope: general`, so it holds on a headless host too.
- **Blocked on (Task 4.3 only)**: `Edmonds-Commerce-Limited/claude-code-hooks-daemon#39` —
  the `reboot-warning` signal kind and the CLI that raises it. Verified absent from the
  installed daemon's command list (F7). The registry and the restore service do not depend
  on it and land first, exactly as the issue says.

## Technical Decisions

Full reasoning, alternatives and evidence for each is in
[DESIGN-failure-modes.md](DESIGN-failure-modes.md). In brief:

| #   | Decision                                                                                                           |
| --- | ------------------------------------------------------------------------------------------------------------------ |
| D1  | A record carries its boot id; one matching the current boot is live, not restorable                                |
| D2  | The restore consumes a record before starting it, so a retry loop cannot exist                                     |
| D3  | Atomic `rename` plus a required terminator line; a `.tmp` file is never a record                                   |
| D4  | Every non-restore is a *retirement with a reason*, kept as evidence, never a silent drop                           |
| D5  | The record stores the resolved configuration by value, not argv; a flag-classification test stops silent staleness |
| D6  | `CCY_UNATTENDED=1` plus a `read()` shadow keyed on `-p`: one seam makes every prompt fatal instead of a hang       |
| D7  | The project's root commit is the fingerprint; a reused directory retires as `different-project`                    |
| D8  | Enablement and record count are reported independently, so "cannot tell" is never "nothing to do"                  |
| D9  | The restore starts tmux under `systemd-run --user --scope`, or the oneshot's cgroup teardown kills it              |

## Success Criteria

- [ ] `./scripts/qa-all.bash` passes
- [ ] `scripts/test-ccy-session-registry.bash` passes, including the flag-classification guard
- [ ] `CCY_VERSION` bumped; the pre-commit CCY gate accepts the commit
- [ ] A truncated record present in the directory is quarantined, not read (unit-tested)
- [ ] The `restore-status` answers are distinct strings, not one "nothing to do"
- [ ] No prompt site in the launcher can hang an unattended launch — the `read()` shadow is
  keyed on `-p`, so it covers sites this plan never enumerated
- [ ] `reboot --in N` refuses with the upstream issue named, and reboots nothing
- [ ] `qa-reviewer` findings all resolved
- [ ] (HOST, owner) a session recorded before a reboot is detached and attachable after it

## Risks & Mitigations

| Risk                                                                                      | Impact | Probability | Mitigation                                                                                            |
| ----------------------------------------------------------------------------------------- | ------ | ----------- | ----------------------------------------------------------------------------------------------------- |
| An unattended launch parks on a prompt and looks restored                                 | H      | H           | D6 — a `-p` prompt under `CCY_UNATTENDED` is fatal and names itself; not an enumeration               |
| A restored session crashes and is restored again for ever                                 | H      | M           | D2 — the record is consumed before the start, so there is nothing left to retry                       |
| A record is read while half-written                                                       | M      | L           | D3 — `rename` is atomic and `*.record` never matches the temp name; the terminator is belt-and-braces |
| A directory is reused for another project and `--continue` resumes the wrong conversation | M      | L           | D7 — a root-commit fingerprint mismatch retires the record                                            |
| The tmux server the service starts is killed when the oneshot exits                       | H      | H           | D9 — `systemd-run --user --scope --collect`, the mechanism `ccy_tmux_insulate` already uses           |
| `ccy` gains a durable flag and restored sessions silently lose it                         | M      | M           | D5 — the classification test derives flags from the parser and fails on an unclassified one           |
| Restore appears enabled but never runs because linger is off                              | M      | M           | D8 — `restore-status` reports that as its own answer                                                  |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00123-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Facts established against the code; three of the issue's premises corrected
  </content>
