# Plan 00109: desktop drift detection and fedora desktop panel

**Status**: In Progress
**Created**: 2026-09-11
**Owner**: joseph
**Priority**: High

## Overview

On 2026-09-11 the laptop rebooted into a new kernel and both DisplayLink monitors
stayed dark, because the repo had pinned the fix months earlier and the host had
never been told. **Every automated check this repo owns was green throughout** — each
was correct about the axis it watches, and none watches "repo pin vs what is
installed here". Diagnosis, evidence and the blow-by-blow:
[JOURNAL/00109-Journal-26-09-11.md](JOURNAL/00109-Journal-26-09-11.md).

The exposure is structural, not specific to DisplayLink. `playbook-main.yml` imports
the core plays, so those are re-run whenever main is run. The **plays under
`playbooks/imports/optional/`** (46 today outside `archived/`, two of them added by this
plan) are run by
hand, once, and then forgotten — nothing records that they were ever run, at what
commit, or whether they have changed since. DisplayLink is simply the one that bit
first, and it bit at the worst moment: after a reboot, with no visible explanation.

This plan closes that gap and then makes the result *usable*: a host-side ledger of
what has actually been run here, drift checks that compare against it, a login-time
health surface that tells the user in plain language when something broke, and a
single GNOME panel that fronts all of it.

## Goals

- Record, on this host, **which plays have been run and at what repo commit**.
- Detect and report plays that were run here and have **changed since** — and stay
  silent about plays never run, which are noise.
- Detect **repo-pinned version vs installed-on-host** drift (the axis that failed).
- Detect post-boot breakage (failed DKMS builds, failed units, missing modules)
  and surface it **at the end of login**, in language a human can act on.
- Offer a one-click handoff into Claude Code with a generated findings file, so
  diagnosing a break is not a manual archaeology session.
- Provide one `fedora-desktop` GNOME panel icon as the generic front end for the
  above, built so it can later host unrelated tools (task runner, quick launch).

## Non-Goals

- **Auto-applying fixes.** Detection and one-click handoff only. Nothing in this
  plan runs a playbook unattended; re-running a play is always a human decision.
- **Auto-pulling and merging the repo.** A freshness check may `git fetch` and
  compare, but it must never move the working tree.
- Replacing `check-pinned-versions.bash` or `qa-deployed-drift.bash` — this plan
  adds the missing third axis alongside them.
- Managing the user's choice of wallpaper image, or its size. Phase 5 began as
  wallpaper sizing and was cancelled once the evidence showed size is irrelevant
  to the symptom; it now recovers the background after a monitor change.
- Fixing the upstream mutter bugs. They are open; this plan works around them.

## Context & Background

The drift axes, and which are covered:

| Axis               | Compares                                       | Owned by                             | Covered |
| ------------------ | ---------------------------------------------- | ------------------------------------ | ------- |
| Pin freshness      | repo pin vs upstream latest                    | `scripts/check-pinned-versions.bash` | ✅      |
| Script deployment  | repo script vs deployed copy in `~/.local/bin` | `scripts/qa-deployed-drift.bash`     | ✅      |
| **Install state**  | **repo pin vs installed package on host**      | **nothing**                          | ❌      |
| **Play freshness** | **play at last run here vs play at HEAD**      | **nothing**                          | ❌      |

The bottom two are this plan's subject. They are different questions: install state
catches "the pin moved and the host never got it"; play freshness catches "the
play's *logic* changed and the host never got it" — a play can change materially
with no version pin involved at all.

Supporting detail as it is gathered goes in named documents in this folder, not
here. See `JOURNAL/` for the incident narrative and the blow-by-blow.

## Tasks

### Phase 0: Close out the 2026-09-11 incident

- [x] ✅ **Task 0.1**: Restore DisplayLink on kernel 7.2.4 — diagnosed, `play-displaylink.yml`
  run on HOST, module built and signed and loaded, both heads enumerating
  ([JOURNAL/00109-Journal-26-09-11.md](JOURNAL/00109-Journal-26-09-11.md))
- [ ] 🔄 **Task 0.2**: Remove orphaned DKMS source trees — cleanup written, deploy pending
  - [x] ✅ The probe is in `triage.bash` — every `/usr/src/evdi-*` tree, `rpm -qf` on
    each, what DKMS still has registered, and the Phase 3 login report
  - [x] ✅ **HOST**: answered on 2026-09-24. Five old trees are unowned and unregistered;
    only the current one is rpm-owned and registered. So the mechanism is deleting the
    directory, never removing a package
  - [x] ✅ `play-displaylink.yml` removes a tree only when no package owns it AND DKMS
    has no `/var/lib/dkms/evdi/<version>`; any other rpm failure stops the play.
    Reviewed PASS WITH NITS, all four resolved
    ([report](subagent-reports/260924-qa-reviewer-t02-evdi-opus-5.md))
  - [ ] ⬜ Deploy on HOST, re-run `triage.bash` to confirm only the current tree remains
- [ ] 🚫 **Task 0.3**: Fix group/world-readable vault password file permissions
  - **Blocked — human-only.** The path is protected by `secret_file_guard`; an agent
    cannot name it in a command, a script, or a playbook task, so this cannot be
    fixed via IaC by an agent. Flagged at session start by `secret_file_hygiene_checker`.
    Requires the user to set owner-only permissions by hand, or to lift the guard.

### Phase 1: Host play-run ledger

> Complete except one HOST item. Record shape, the hash's dirty-tree-guard job, the
> callback hook point and its `ANSIBLE_CONFIG` hole, the swallowed-exception fail-fast
> route, and the no-backfill decision are all in
> [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §§1–4.

- [x] ✅ **Task 1.1**: Design the ledger record and its location
- [x] ✅ **Task 1.2**: Write the ledger on every play run — `callback_plugins/play_ledger.py`.
  Detail (the 2.19 breakage, the sentinel fix, the qa-reviewer pass, the rename-detecting
  gate, HOST verification): [COMPLETED-TASKS-detail.md#task-12-write-the-ledger-on-every-play-run](COMPLETED-TASKS-detail.md#task-12-write-the-ledger-on-every-play-run)
- [x] ✅ **Task 1.3**: Backfill — **none**, answered by a reporting rule instead: a play
  with no record has never been run here, and silence is correct for it

### Phase 2: Drift checks built on the ledger

> Complete. Verdict table, the three exit statuses and the two structural silences are in
> [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §§6–7; the offline-login decision in
> [DESIGN-host-health.md](DESIGN-host-health.md) §8; the pin axis in
> [DESIGN-version-pins.md](DESIGN-version-pins.md).

- [x] ✅ **Task 2.1**: Play-freshness — `freshness.py`, `git_history.py`,
  `check_freshness.py`. Clean-and-silent, findings, and **untrustworthy** are three
  different answers, and an offline login is silent while a long silence is a finding
- [x] ✅ **Task 2.2**: Installed-vs-pinned — the axis that actually failed. Passes both
  directions against the states the journal records, resolution is declared rather than
  guessed, and coverage has a floor: partial is a decision, zero is a check that cannot fail
- [x] ✅ **Task 2.3**: **Neither belongs in `qa-all.bash`** — in a container both find
  nothing and exit 0. Their tests do belong there, via `qa-helper-tests.bash`

### Phase 3: Login-time health surfacing and Claude Code handoff

> Design: [DESIGN-host-health.md](DESIGN-host-health.md) §§1–11.

- [x] ✅ **Task 3.1**: Post-boot health probe — wired and running on the HOST.
  Detail (the `daemon_reload` ordering defect, the HOST re-run):
  [COMPLETED-TASKS-detail.md#task-31-post-boot-health-probe](COMPLETED-TASKS-detail.md#task-31-post-boot-health-probe)
- [ ] 🔄 **Task 3.2**: Surface findings to the user — code done, HOST run pending
  - [x] ✅ `login_report.py` — one notification, silent when clean
  - [ ] ⬜ **HOST**: confirm a real notification arrives, and a clean login is silent
  - [ ] 🔄 **The server route.** Only the delivery was ever desktop-bound; the checks are
    profile-agnostic. Reasoning, the cadence derivation, the mutants and the two review
    findings are in [DESIGN-server-route.md](DESIGN-server-route.md)
    - [x] ✅ Producer/renderer, delivery cadence and interactive-only guard, previous-boot
      handling, unreadable-document reporting, the healthy-server-silence fix, the VM
      scenario definition, its kernel-step executor and `set -e` fix, and the
      `reboot_before_checks` refusal are each finished. Detail:
      [COMPLETED-TASKS-detail.md#task-32-the-server-route](COMPLETED-TASKS-detail.md#task-32-the-server-route)
    - [x] ✅ **HOST**: `play-vm-test-lab.yml` run — check [16] confirms the scenario is in
      the deployed allowlist and both guest scripts are deployed executable, so the bridge
      will no longer refuse the id
    - [ ] ⬜ **VM — nothing in this task is established until this passes**:
      `./scripts/vmtest-request.bash run-scenario server-host-health-kernel-change`. The timer arms, a document appears, **a clean
      server login is silent**, a live fault is reported as a fault, the guest reboots
      into a different kernel, the report names the boot mismatch, the previous boot's
      fault is demoted rather than repeated as current, and an `scp` through the guest's
      own `sshd` completes on both sides of the reboot
      First run 2026-09-23: `error`, the fixture exited 1 at prepare. The reason is
      in the host transcript only; the harness now returns it (needs `play-vm-test-lab.yml`).
      Second run: the reason is dnf5 refusing dnf4's `repoquery --showduplicates`; fixed
      with a stub that models dnf5. Third run, 2026-09-24: dnf worked and prepare failed
      one step later. grubby read the entry it had just set back with `/boot` doubled.
      The fixture now accepts that spelling of the same entry, and only that one. Fourth
      run: 14 of 15 pass, including the kernel change. `clean-login-is-silent` fails on
      three things a clean login printed on stdout. Two were this repo's shell setup and
      are fixed: `ps1-prompt`'s title escape, and the SSH-agent block, which prompted with
      no terminal. The third was the pin check's coverage floor, "compared 0 of 1" on a
      server with no DKMS. The owner decided: not applicable, and silent
      ([DESIGN-server-route.md §5](DESIGN-server-route.md)). All three are fixed in code.
      A fifth run needs them pushed, because the guest provisions from the pushed commit
      (the run's `repo_commit` evidence).
    - [x] ✅ **HOST**: check [17] — `origin` resolves non-interactively, with a recorded
      successful fetch. The freshness axis will not report "never reached the remote"
- [x] ✅ **Task 3.3**: Claude Code handoff — file and offer done.
  Detail (the copy-not-launch reasoning, the write-ordering test):
  [COMPLETED-TASKS-detail.md#task-33-claude-code-handoff](COMPLETED-TASKS-detail.md#task-33-claude-code-handoff)

### Phase 4: `fedora-desktop` GNOME panel extension

> Design: [DESIGN-panel.md](DESIGN-panel.md) §§1–11. A bare `§` below is a section of
> that file; anything owned elsewhere names its document.

- [x] ✅ **Task 4.1**: Scaffold `extensions/fedora-desktop@fedora-desktop` —
  `metadata.json`, `statusDocument.js`, `sections/health.js`, `extension.js`,
  `stylesheet.css`, plus the producer `helpers/host_health/status_document.py` and the
  cross-language contract gate `helpers/gnome/check_panel_contract.py` in `qa-all.bash`
- [ ] 🔄 **Task 4.2**: Health section — renders Phase 3's four checks.
  Detail (indicator test coverage, self-section reasons, ledger-emptiness check,
  boot-awareness, malformed-document handling, the section-contract proof):
  [COMPLETED-TASKS-detail.md#task-42-health-section](COMPLETED-TASKS-detail.md#task-42-health-section)
  - [ ] ⬜ **HOST**: the rendering itself — whether St shows the demoted lines legibly and
    whether the icon is the right thing to look at. Only a Wayland session can say, and
    the harness deliberately does not claim to
- [ ] 🔄 **Task 4.3**: Play/task runner — plays with their ledger state, launched in a
  visible terminal, never in the background. Code done — HOST run pending. Ledger-seen
  plays via `host-status.json` `plays`; a click runs `fedora-desktop-health --run-play`.
  Decisions: [DESIGN-panel.md](DESIGN-panel.md) §9
  - [ ] ⬜ **HOST**: the rows read well in a live shell, a click opens the terminal via
    `xdg-terminal-exec`, `run.bash`'s sudo prompt works there, and the next report shows
    the play as fresh
- [x] ✅ **Task 4.4**: Sections registered, not hardcoded — one array entry per section
- [ ] 🔄 **Task 4.5**: ESLint clean, deployed by its own play, Wayland-correct
  - [x] ✅ ESLint clean, contract gate green, HOST play run confirmed. Detail:
    [COMPLETED-TASKS-detail.md#task-45-eslint-clean-deployed-by-its-own-play-wayland-correct](COMPLETED-TASKS-detail.md#task-45-eslint-clean-deployed-by-its-own-play-wayland-correct)
  - [ ] ⬜ **HOST — eyes only**: that the icon is *visibly* in the top bar. [13]–[15] are
    everything short of seeing it, and a gate cannot close that last step

### Phase 5: Recover the desktop background after a monitor change

> Reasoning, evidence and the known limitation for this phase live in
> [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md) §Recovery.
> Task state stays here.

- [x] ✅ **Task 5.1**: Establish the real cost and the real bug. Detail:
  [COMPLETED-TASKS-detail.md#task-51-establish-the-real-cost-and-the-real-bug](COMPLETED-TASKS-detail.md#task-51-establish-the-real-cost-and-the-real-bug)
- [x] ❌ **Task 5.2**: ~~Scale the wallpaper~~ — cancelled, wrong problem
- [x] ❌ **Task 5.3**: ~~Per-monitor pre-scaled caching~~ — cancelled; GNOME ships it
- [x] ✅ **Task 5.4**: Recover the background after a monitor reconfiguration —
  `Action.REFRESH_BACKGROUND` in `helpers/displaylink_recovery/`, fired by the dock udev
  rule and the suspend service, after the wedge ladder and never while locked
  - ⚠️ **Known limitation — the resume path is effectively inert**: the screen is already
    locked when the suspend service runs, so the run correctly refuses. Dock/udev works
  - [ ] 🚫 **T5.4a**: Cover the unlock case — **OWNER'S CALL, not code that is merely
    unwritten.** Nothing in the repo watches lock state today, and the two viable owners
    trade off against each other rather than one being determined:
    the panel gets `ActiveChanged` for free but its own header says it *"runs no check of
    its own and applies no fix"*, and launches only a terminal a person is looking at.
    An automatic background refresh would end that; a user
    systemd unit keeps that contract intact but costs a long-running daemon whose only
    job is to watch one signal the shell already dispatches. Reasoning and the third
    option in [DESIGN-panel.md §12](DESIGN-panel.md). Implementable and unit-testable
    here once chosen; the HOST item below gates shipping it either way
  - [x] ✅ **HOST**: deployed and armed — check \[18\]: recovery tree, udev rule and dock
    unit deployed, `displaylink-suspend.service` enabled
  - [ ] ⬜ **HOST**: that the refresh actually clears a black background. Needs the symptom
    present, and it is an upstream bug that cannot be induced on demand

## Dependencies

- Relates to Plan 00041 (remote desktop toggle) — extension→CLI-helper pattern to reuse
- Relates to Plan 00058 (version pins) — this plan adds the install-state axis it lacks
- Relates to Plan 00074 (boot preflight) — coordinate, do not duplicate
- Relates to Plan 00086 (kernel modules absent) — coordinate, do not duplicate

## Technical Decisions

Three, and the third is still open — full context, options and reasoning in
[DECISIONS.md](DECISIONS.md), extracted there because PLAN.md is read in full every
session and was approaching the size at which edits are blocked:

1. **Detection and handoff, never unattended repair.** Information is the deliverable.
2. **The ledger is per-play host state, and its failures are recorded not raised.**
3. **OPEN, owner's:** nothing detects that this plan's own opt-in plays were never run
   on a host, so a host that never enabled detection looks exactly like a clean one.

## Success Criteria

**The HOST items in the task tree are now two scripts, not a list of instructions.**
(No count here on purpose: the previous sentence gave one, it was the acceptance gate's
check count rather than the task tree's, and it went stale the moment a task was ticked.) Run `deploy.bash` then `acceptance.bash` in this folder — or
`CLAUDE/Plan/meta-deploy.bash` to run this plan alongside the others waiting.
`deploy.bash` runs four plays in a deliberate order, with `play-displaylink.yml` last
because it is the only one that can demand a MOK enrolment and a reboot. (That last point
is recorded in `deploy.bash`'s header comment, not announced at runtime: this said "its
change gate says so before anything runs", and there is neither a gate — R8 removed
`plan_gate_change` — nor any runtime warning.) `acceptance.bash` carries nineteen COVERAGE-registered
checks and prints what needs a Wayland session or your own eyes under FOR THE HUMAN,
never counting those as passed.

- [ ] The installed-vs-pinned check **fails** when pointed at the 2026-09-11 state
  and passes now — demonstrated, not asserted
- [ ] The freshness check reports a play edited after its ledgered run, and stays
  silent about every play never run here
- [ ] A clean system produces **no notification at all** at login
- [ ] The panel opens from one icon and shows health plus play state
- [x] `./scripts/qa-all.bash` passes (929 files, exit 0, three standing advisories —
  shellcheck's 172 informational issues, semgrep's 15 partially-parsed files, and the
  container's `deployed-drift` skip; "every gate green" would be the overclaim this plan
  is about); ESLint clean from
  `extensions/`, which is where its config lives — `eslint .` at the repo root finds no
  config at all and fails for that reason, which is not a finding about the code
- [ ] Host-only checks skip cleanly in the CCY container **and in CI** — the container
  half is done: `deployed-drift` reports `⚠ skipped (CCY container — no deployed copies to compare)`, an advisory rather than a pass, so a skipped check cannot be read as a
  passed one. The CI half is unverified from here and needs a green run on a pushed
  branch to claim
- [ ] `qa-reviewer` agent run over the full plan diff, findings resolved — **run**
  (`subagent-reports/260916-qa-reviewer-full-plan-diff-opus-5.md`, verdict BLOCK: 1
  blocking, 3 should-fix, 6 minor, 2 nits). Every finding actionable from a container is
  resolved, including the blocking one. Left open: the CI half above, and Decision 3,
  which is the owner's. Unticked until a re-run confirms it, since a review whose
  findings were actioned by the same agent that wrote them is not a second opinion

## Risks & Mitigations

| Risk                                              | Impact | Probability | Mitigation                                                                |
| ------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------- |
| Health check becomes noisy and gets muted         | H      | M           | Silent when clean; report only plays actually run here                    |
| Ledger hook bypassed by direct `ansible-playbook` | M      | H           | Hook at the Ansible callback layer, not in a wrapper script               |
| Per-pin install detection can't be generic        | M      | H           | Fail loudly on undeterminable pins rather than reporting a false pass     |
| Panel grows into an unmaintained catch-all        | M      | M           | Registered sections with a defined contract; each section earns its place |
| Auto-pull moves the working tree under the user   | H      | L           | `git fetch` only — never merge, never checkout                            |

## Delivery & Milestones

- Phase 0 Task 0.1 delivered: DisplayLink restored on kernel 7.2.4 (evdi 1.15.0)
- Task 3.2's server route delivered: the collection timer and the interactive-only login
  snippet, folded into `play-host-health-login-report.yml` as its second delivery. The
  HOST run remains open, and the review found two ways it would never be silent there.
