# Plan 00137: unattended server self-update

**Status**: In Progress
**Created**: 2026-09-23
**Owner**: joseph
**Priority**: Medium

## Overview

An always-on headless server hosting ccy sessions has no-one at it to notice that the
fedora-desktop checkout has moved on and the launchers it deployed are out of date. This
plan adds a timer-driven cycle that brings the checkout up to date safely, works out which
plays the new commits affect, warns and stops the running ccy/cc sessions, runs those
plays unattended, and brings the sessions back with a check that they really resumed.
`play-claude-yolo.yml` is the main candidate, since it deploys both launchers.

Most of the parts exist and several do not fit as they are. The research
([RESEARCH-existing-pieces.md](RESEARCH-existing-pieces.md)) found:

- `run.bash`'s update is a bare `git pull` that merges when the branch has diverged.
- Headless single-play mode is refused.
- The play ledger watches only each play's own file, so a change to the ccy launcher or
  its lib reads as "nothing to run".
- `ccy-sessions` has no stop that keeps the session records.
- A restore can stop at an interactive menu and still look like it succeeded.
- Push access to the tracked branch becomes root on the server. The ccy agents running
  there hold GitHub tokens.

Each of those is a task below.

## Goals

- A timer runs the whole cycle with no human present, and a failure at any step stops the
  cycle with a clear reason. It never runs a play from a checkout it could not verify.
- "What to run" is computed from the paths the new commits changed, not only from play
  files, so a lib or launcher change triggers `play-claude-yolo.yml`.
- Every session that was running before the cycle is running after it, in the same
  directory, resumed. A session that could not be brought back is reported, never
  silently dropped.
- A human running a play, or the panel's `--run-play`, can never overlap the timer.

## Non-Goals

- Desktop hosts. The cycle is for the server profile, where no-one is logged in to decide.
- Package and kernel updates. `shutdown-with-update` / `reboot-with-update` already own
  those. `play-AB-dnf-upgrade.yml` is out of scope unless D2 brings it in.
- First-install provisioning. That is Plan 00063's headless `run.bash`.

## Context & Background

- Detection and reporting: Plan 00109 (play ledger, `check_freshness`, the server
  collection timer).
- Session warning and boot-time restore: Plan 00135. Its Phase 5 (real-machine proof) is
  still open, and this plan depends on it.
- Single-play runs: Plan 00114. Headless run.bash: Plan 00063.
- Plan 00109 says re-running a play is always a human decision
  (`play-host-health-login-report.yml:28-31`). This plan is the first automatic play
  runner, so D5 has to settle how that statement changes.

## Technical Decisions

The options and trade-offs for each are in
[RESEARCH-existing-pieces.md § Decisions](RESEARCH-existing-pieces.md#decisions-the-owner-must-make).
D1–D4 are the owner's choices, made 2026-09-23.

- **D1 — restart: reboot through Plan 00135.** Pull, run the plays, warn with `ccy-sessions notify going-down`, then reboot. The boot-time restore brings the sessions back. This
  keeps one restore path instead of two, and needs no in-place stop, whose kill path is
  unproven. The machine reboots only when there is something to run. This makes Plan
  00135's Phase 5 a hard dependency, and `ccy_restore_sessions` must be true on the
  server.
- **D2 — plays: a tracked allowlist**, seeded with `play-claude-yolo.yml`. A changed play
  that is not on the list is reported, not run.
- **D3 — trust: the tip must be signed by the owner's pinned key.** Every commit cannot be
  signed, because the agents write most of them and must never hold the key. The cycle
  therefore runs a commit only if it is signed by the pinned key, and that signature
  vouches for the whole range from the last deployed commit to it. Unsigned commits
  above it wait for the next signed one. Signing is not set up anywhere yet (Plan
  00035 Phase 6 was research only), so this plan sets it up: SSH signing, and on the
  server an `allowedSignersFile` holding only the owner's key.
- **D4 — checkout: a deploy-only clone**, never mounted into a ccy container, updated only
  by the timer.
- **D5 — privilege: a root-owned sbin entry point, with scoped sudo.** The owner's
  pattern: `/usr/local/sbin/fedora-desktop-self-update` is owned by root and not writable
  (or readable) by the user. The timer is a system unit, so it runs the script as root
  directly. A sudoers drop-in lets the owner run that one script manually without a
  password, and nothing else. The limit is that Ansible's `become` cannot be scoped: it
  runs `sudo … /bin/sh -c <python>`, which is arbitrary, and the plays cannot run as root
  outright, because their `systemctl --user` tasks need the user's own manager. So the
  script runs the plays as the user and hands Ansible the become password from a
  root-only 0600 file, on an inherited file descriptor. The user's shell and the ccy
  containers can never read the file, and no `NOPASSWD:ALL` exists. The file is
  provisioned through vault, never hardcoded.
  - **Amended after the IaC review:** a process running as the user can still alter the
    user's Ansible code or read the running controller's memory. The owner chose to
    treat the user account as trusted and add two cheap hardenings:

    - the cycle runs a root-owned system `ansible-core` with a root-owned collections
      path, so no user-writable code is on the path;
    - the server sets `kernel.yama.ptrace_scope=1`, so a process can read another's
      memory only if it launched it.

    ccy agents are already outside this. Rootless podman gives them their own PID
    namespace, and they see only their project mount. Running the controller as root
    was rejected: every play assumes it starts as the user.
- **D6 — warning: 3 minutes, configurable.** A session can only fail to be warned when
  its project has no hooks-daemon CLI (`.claude/hooks-daemon/bin/hooks-daemon`), for
  example a project that does not use the daemon. `notify` refuses rather than reboot
  over it. Default: skip the cycle and alert (D8's channel), then retry next run.
- **D7 — boundary: its own opt-in play**, enabled per server by a `host_vars` flag. Plan
  00109's "re-running a play is a human decision" still holds everywhere the flag is off.
- **D8 — failure: no reboot, and an alert through a real channel.** Every failure alerts,
  whether a play failed, the gate refused, or a session could not be warned, and so does
  a completed cycle's summary. The sinks are pluggable: a Slack webhook (URL in vault)
  and/or a GitHub issue in a **private** repo. This repo is public, so an alert here
  could expose install details. The host-health report also carries the last result.
- **D9 — cadence: nightly** (around 03:30, `RandomizedDelaySec` up to 30 minutes,
  `Persistent`). There is no "someone is watching" check: the owner judged it not worth
  including.

## Tasks

### Phase 0: Decisions

- [x] ✅ **Task 0.1**: Record D1–D4 as Technical Decisions above.
- [x] ✅ **Task 0.2**: Settle D5–D9 and record them. The owner answered D5, D6, D8 and D9;
  D7 is the stated default.
- [ ] ⬜ **Task 0.4**: Owner picks the alert sink(s) for D8 (Slack webhook, private-repo
  GitHub issue, or both). This does not block Phases 1–3.
- [x] ✅ **Task 0.3**: Signing IaC: an SSH signing key for the owner on the desktop, git
  configured to sign, and the public key published through a `host_vars` placeholder,
  never hardcoded. The deploy clone's `gpg.ssh.allowedSignersFile` holds only that key.

### Phase 1: Safe update and change detection

- [x] ✅ **Task 1.1**: A tested helper that fetches, then refuses a dirty, diverged,
  detached or wrong-branch checkout, then fast-forwards (`merge --ff-only`). It prints the
  old and new SHAs and re-checks the Fedora version pin.
- [x] ✅ **Task 1.2**: The trust gate (D3). Fast-forward only to the newest commit on the
  branch that `git verify-commit` accepts against the pinned signer. Anything unsigned
  above it waits. No signed commit beyond the deployed one means nothing to do. A bad
  signature refuses the cycle; it never warns and continues.
- [x] ✅ **Task 1.3**: `git diff --name-only OLD..NEW` mapped to plays: each play's own
  file, its `src:` files, `import_tasks`/`include_tasks`, the `vars/` it loads, and the
  `helpers/` it calls. The ledger's `judged` verdicts are layered on for GONE and
  UNEXPLAINED plays. The result is filtered by D2. TDD against fixture repos.
- [x] ✅ **Task 1.4**: A lock shared by the updater, `run.bash` single-play and `--run-play`.

### Phase 2: Unattended play runner

- [x] ✅ **Task 2.1**: A headless single-play path. It runs a sudo-only preflight, supports
  `--become-password-file`, never prompts, uses an explicit PATH, and exits non-zero on
  failure. It has none of the first-install behaviour of a full headless run.
- [x] ✅ **Task 2.2**: Fetch authentication that works from a timer: an HTTPS fetch URL for
  the public repo, or a documented key route.

### Phase 3: Sessions

- [ ] ❌ **Task 3.1**: ~~`ccy-sessions stop` that keeps the records~~. Cancelled by D1:
  the reboot ends the sessions, and Plan 00135 proves that path keeps their records.
- [x] ✅ **Task 3.2**: A boot-time restore that cannot block on a prompt, plus a
  post-restore check: the pane is alive, the container is up, and `capture-pane` shows no
  prompt. Anything else is reported. Blocking menus include a zombie container, an
  existing container, and a token or network prompt.
- [x] ✅ **Task 3.3**: The warn-then-reboot step. Reuse `reboot-with-update`'s warning and
  countdown, with its withdraw-on-failure behaviour, but skip its package updates. Apply
  D6's policy for a project without a daemon CLI.
- [x] ✅ **Task 3.4**: Record the cycle's own state across the reboot: what it deployed and
  that a restore check is owed. The post-boot check then reports against it.

### Phase 4: The cycle, its units and its play

- [x] ✅ **Task 4.1**: One orchestrator, tested under fakes: lock, update, gate, detect;
  then, only if something is to run: run the plays, warn, reboot. After boot: restore,
  verify, report. D8 decides what happens after a failed play: reboot anyway, or leave
  the sessions running and alert.
- [x] ✅ **Task 4.2**: A systemd timer and service pair (D9 sets the cadence), deployed and
  enabled only on the server profile. Follow the `play-host-health-login-report.yml`
  pattern: separate reloads, and a read-back of the live dependency graph. Its place in
  the IaC graph follows D5 and D7.
- [x] ✅ **Task 4.3**: Results reach the host-health report, so a failed or skipped cycle
  shows up in the login snippet. Contract: DESIGN-cycle.md, "The published copy".
- [ ] ⬜ **Task 4.5**: The alert sinks from D8/Task 0.4. The secret lives in vault. The
  message carries no hostname, username or path (public-repo rule), and a sink that
  fails to deliver is itself reported.
- [x] ✅ **Task 4.7**: The D5 hardening in `play-self-update.yml`:
  - system `ansible-core` from dnf, plus the collections the plays need in a root-owned
    `ANSIBLE_COLLECTIONS_PATH`;
  - a `sysctl.d` drop-in setting `kernel.yama.ptrace_scope=1`, applied and read back;
  - the orchestrator uses only that `ansible-playbook` and that collections path.
- [x] ✅ **Task 4.6**: The root sbin script, its sudoers drop-in (that one command only;
  validated with `visudo -c` before install), and the root-only become-password file
  provisioned from vault.
- [ ] ⬜ **Task 4.4**: `deploy.bash` and `acceptance.bash` for this plan (host-only). The
  acceptance script prints a coverage line and names anything it cannot establish.

### Phase 5: Close

- [ ] ⬜ **Task 5.1**: Docs: what the cycle does, the trust model, how to pause it, and how
  to read its log.
- [ ] ⬜ **Task 5.2**: `./scripts/qa-all.bash`, then the `qa-reviewer` agent over the full
  diff, with findings resolved.
- [ ] ⬜ **Task 5.3**: HOST: one full cycle on a server with two live sessions, triggered by
  a real commit that touches `lib/`.

## Success Criteria

- [ ] A commit touching only `files/var/local/claude-yolo/lib/` makes the cycle run
  `play-claude-yolo.yml`. A commit touching nothing deployed runs nothing and stops no
  session.
- [ ] A dirty, diverged or unverified checkout stops the cycle before any session is
  warned.
- [ ] Two sessions running before the cycle are running after it, resumed, and the check
  in Task 3.2 passes for both.
- [ ] A failed play is reported, and the sessions end up in the state D8 chose.
- [ ] A manual play run started during a cycle is refused by the lock, and the reverse.
- [ ] `./scripts/qa-all.bash` green; `qa-reviewer` findings resolved.

## Delivery & Milestones

- Research: [RESEARCH-existing-pieces.md](RESEARCH-existing-pieces.md)
