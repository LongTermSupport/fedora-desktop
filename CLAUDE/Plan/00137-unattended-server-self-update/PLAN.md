# Plan 00137: unattended server self-update

**Status**: Not Started — waiting on the owner decisions below (D1–D4)
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

## Decisions for the owner

The options and trade-offs are in
[RESEARCH-existing-pieces.md § Decisions](RESEARCH-existing-pieces.md#decisions-the-owner-must-make).

- **D1 — restart mechanism**: restore sessions in place; or reboot through Plan 00135's
  restore; or reboot only when needed.
- **D2 — which plays**: `play-claude-yolo.yml` only; or an allowlist file; or everything
  the path diff maps to.
- **D3 — trust gate**: accept push access = root and document it; or signed commits from a
  pinned key; or green CI; or a release ref only the owner moves.
- **D4 — which checkout**: the shared `~/Projects/fedora-desktop`, or a deploy-only clone.
- **D5–D9**, with defaults proposed in the research doc and settled when Phase 1 starts:
  - D5: the sudo credential;
  - D6: the warning policy;
  - D7: the reporting-only boundary;
  - D8: failure behaviour;
  - D9: cadence and window.

## Tasks

### Phase 0: Decisions

- [ ] ⬜ **Task 0.1**: Record D1–D4 as Technical Decisions below, with the owner's reasons.
- [ ] ⬜ **Task 0.2**: Settle D5–D9 (proposed defaults, owner confirms) and record them.

### Phase 1: Safe update and change detection

- [ ] ⬜ **Task 1.1**: A tested helper that fetches, then refuses a dirty, diverged,
  detached or wrong-branch checkout, then fast-forwards (`merge --ff-only`). It prints the
  old and new SHAs and re-checks the Fedora version pin.
- [ ] ⬜ **Task 1.2**: The trust gate chosen in D3, applied to the new SHA before anything
  runs. Refuse, never warn and continue.
- [ ] ⬜ **Task 1.3**: `git diff --name-only OLD..NEW` mapped to plays: each play's own
  file, its `src:` files, `import_tasks`/`include_tasks`, the `vars/` it loads, and the
  `helpers/` it calls. The ledger's `judged` verdicts are layered on for GONE and
  UNEXPLAINED plays. The result is filtered by D2. TDD against fixture repos.
- [ ] ⬜ **Task 1.4**: A lock shared by the updater, `run.bash` single-play and `--run-play`.

### Phase 2: Unattended play runner

- [ ] ⬜ **Task 2.1**: A headless single-play path. It runs a sudo-only preflight, supports
  `--become-password-file`, never prompts, uses an explicit PATH, and exits non-zero on
  failure. It has none of the first-install behaviour of a full headless run.
- [ ] ⬜ **Task 2.2**: Fetch authentication that works from a timer: an HTTPS fetch URL for
  the public repo, or a documented key route.

### Phase 3: Sessions

- [ ] ⬜ **Task 3.1**: `ccy-sessions stop`. It ends every live session without removing its
  record, waits until no ccy container is left, and fails loudly if one survives. Add
  tests for record survival under SIGHUP and SIGTERM (only SIGKILL is tested today).
- [ ] ⬜ **Task 3.2**: A restore that cannot block on a prompt, plus a post-restore check:
  the pane is alive, the container is up, and `capture-pane` shows no prompt. Anything
  else is reported.
- [ ] ⬜ **Task 3.3**: Wire the warning (`notify going-down`), the wait, and D6's policy for
  a project without a daemon CLI.

### Phase 4: The cycle, its units and its play

- [ ] ⬜ **Task 4.1**: One orchestrator, tested under fakes: lock, update, gate, detect;
  then, only if something is to run: warn, wait, stop, run, restore, verify; then report.
  D8 decides what happens after a failed play.
- [ ] ⬜ **Task 4.2**: A systemd timer and service pair (D9 sets the cadence), deployed and
  enabled only on the server profile. Follow the `play-host-health-login-report.yml`
  pattern: separate reloads, and a read-back of the live dependency graph. Its place in
  the IaC graph follows D5 and D7.
- [ ] ⬜ **Task 4.3**: Results reach the host-health report, so a failed or skipped cycle
  shows up in the login snippet.
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
