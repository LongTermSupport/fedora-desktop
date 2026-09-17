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
- [ ] 🔄 **Task 0.2**: Remove orphaned DKMS source trees — probe written, answer pending
  - [x] ✅ The probe is in `triage.bash` — every `/usr/src/evdi-*` tree, `rpm -qf` on
    each, what DKMS still has registered, and the Phase 3 login report
  - [ ] ⬜ **HOST**: run it. The cleanup cannot be written first: the two cases need
    opposite mechanisms — an rpm-owned tree goes by removing the package, an unowned
    one by deleting the directory — so guessing makes the play a no-op or a fight
    with the package manager
  - [ ] ⬜ Add cleanup to the owning play, gated on the tree being unregistered in DKMS
  - [ ] ⬜ Run QA, deploy on HOST, re-run `triage.bash` to confirm
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
- [ ] 🔄 **Task 1.2**: Write the ledger on every play run — `callback_plugins/play_ledger.py`
  - [x] ✅ **It had never worked on ansible-core 2.19** (issue #46). 2.19 removed
    `ansible_pos` and moved the play's source position into an `Origin` tag on the play
    itself, so every play became a recorded hole and the ledger marked itself `BROKEN` on
    every run. Both shapes are read now, new first, and the choice is tested
  - [x] ✅ **`store.clear_broken` had no caller anywhere** — a sentinel, once written, left
    the ledger permanently untrustworthy with no route back. `check_freshness --clear-broken` is that route, and it says the missing rows are not recovered
  - [x] ✅ **`qa-reviewer` over the commit** — 7 should-fixes, all acted on. Report:
    [subagent-reports/260915-qa-reviewer-00109-ledger-opus-5.md](subagent-reports/260915-qa-reviewer-00109-ledger-opus-5.md).
    The one that mattered: clearing the sentinel flipped `plays_run_here` from `None` to
    a **partial** set, silently suppressing every ABSENT pin verdict whose row was in the
    hole — the precise suppression that function's own docstring calls unacceptable. A
    `CLEARED` marker now outlives the sentinel, because the missing rows never come back
  - [x] ✅ **A gate now catches the next Ansible rename** —
    `tests/helpers/play_ledger/test_source_position_against_real_ansible.py` loads a real
    playbook through the real `Play.load` under the interpreter `ansible-playbook` itself
    runs, and asserts the production helper gets the file back for **both** the parsed
    play and the `copy()` a callback is actually handed. A fake origin cannot catch a
    rename in the thing it is faking, which is why the whole suite stayed green while the
    ledger recorded nothing for its entire life. Falsified against the pre-fix behaviour
  - [ ] ⬜ **HOST or VM**: verify against a real run — genesis plus one row per play,
    `--check` adds nothing, a second run appends. No guest checker reads the ledger today;
    that is the gap, not the machine ([DESIGN-host-health.md](DESIGN-host-health.md) §12).
    **This item would have caught both defects above on its first execution**, which is
    the argument for it rather than against it
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

- [ ] 🔄 **Task 3.1**: Post-boot health probe — code done, HOST wiring pending
  - [x] ✅ `probe_results.py` (verdicts) and `probe.py` (the half that touches the
    machine); `host-health.service`, deployed by `play-host-health-login-report.yml`
  - [ ] ⬜ **HOST or VM**: run the play, then assert the unit is actually *wanted* —
    `systemctl --user list-dependencies graphical-session.target` must name it. "The play
    succeeded" is a different claim. `desktop-fresh-install` has a graphical session, so
    the lab can settle this given the play in its `run_env`
    ([DESIGN-host-health.md](DESIGN-host-health.md) §12)
  - [x] ✅ **The distinction this sub-task insisted on was a real defect,** found the first
    time it was checked: the enable task's `daemon_reload:` runs *before* the enable, so it
    re-read a directory without the symlink it existed for. Reload split into its own task
    after the enable. Evidence: `JOURNAL/00109-Journal-26-09-17.md`
  - [ ] ⬜ **HOST**: re-run the play, confirm `list-dependencies` names the unit, log out and
    back in, re-run `acceptance.bash`. Checks [1]–[5], [11], [12], [15] cascade behind this
- [ ] 🔄 **Task 3.2**: Surface findings to the user — code done, HOST run pending
  - [x] ✅ `login_report.py` — one notification, silent when clean
  - [ ] ⬜ **HOST**: confirm a real notification arrives, and a clean login is silent
  - [ ] 🔄 **The server route.** Only the delivery was ever desktop-bound; the checks are
    profile-agnostic. Reasoning, the cadence derivation, the mutants and the two review
    findings are in [DESIGN-server-route.md](DESIGN-server-route.md)
    - [x] ✅ `status_document.py` (producer) and `login_message.py` (renderer)
    - [x] ✅ The delivery — collection timer plus `~/.bashrc-includes` snippet, folded
      into `play-host-health-login-report.yml` (`scope: general`); daily (§1–2), and each
      branch removes the other's artefacts (§7)
    - [x] ✅ The snippet prints **only for an interactive shell**, or it breaks `scp` to
      the host it reports on (§3)
    - [x] ✅ A fresh document can be about the **previous boot**: the mismatch is reported
      in its own right and the one boot-scoped section is demoted (§4, §4.1)
    - [x] ✅ **A document the reader cannot interpret is reported, not read as clean**
      (§4.1a). The panel had the same gap from the other side — Task 4.2 below
    - [x] ✅ **A healthy server was never going to be silent** (qa-reviewer, 26-09-15) —
      two permanent findings, one root: no `dkms` on a server (§5, §5.1–5.3)
    - [x] ✅ A scenario exists that **can** run this route end to end:
      `server-host-health-kernel-change` in `vars/vm-test-scenarios.yml`, with a fixture
      and a fifteen-check checker (§8). **It has never been executed**, and until it has,
      nothing below it is established
    - [x] ✅ **The kernel step had no executor** — now a function driven against stubs,
      proving which version is chosen, what is downloaded, and that every way of ending up
      with one kernel refuses. This proves the **decisions**, not dnf's real output
      format (§8.2)
    - [x] ✅ **Making it a function moved it out of `set -e`** (qa-reviewer, BLOCK): bash
      disables errexit inside a command substitution, so the package transaction's status
      was discarded. No case had ever failed the install. Every guest-changing command now
      carries its own refusal (§8.2, which also corrects an initial wrong diagnosis)
    - [x] ✅ `reboot_before_checks` is a scenario's answer, not a profile's, and a profile
      the CLI has no mechanics for is a refusal rather than a silent no-reboot (§8.1)
    - [ ] ⬜ **HOST**: run `play-vm-test-lab.yml` once, so the new scenario reaches the
      deployed allowlist and the two new guest scripts reach `~/.local/share/vmtest`.
      The bridge refuses an id that is only in the manifest — deliberately, and this is
      the only step an agent cannot do
    - [ ] ⬜ **VM — nothing in this task is established until this passes**:
      `./scripts/vmtest-request.bash run-scenario server-host-health-kernel-change`. The timer arms, a document appears, **a clean
      server login is silent**, a live fault is reported as a fault, the guest reboots
      into a different kernel, the report names the boot mismatch, the previous boot's
      fault is demoted rather than repeated as current, and an `scp` through the guest's
      own `sshd` completes on both sides of the reboot
    - [ ] ⬜ **HOST**: confirm **this** checkout has a remote the timer can fetch
      **without an agent**, or the freshness axis reports "never reached the remote" for
      ever. Stays HOST: a guest proves the mechanism, not this checkout's `origin` (§6,
      [DESIGN-host-health.md](DESIGN-host-health.md) §12)
- [x] ✅ **Task 3.3**: Claude Code handoff — file and offer done
  - [x] ✅ `handoff.py`, mode `0600`; the wrong/not-looked-at split is carried in
    `Finding.checked`, not read from the prose
  - [x] ✅ The **one-click** offer, in the panel's health section. It **copies** the
    command rather than launching it: `claude` reads the repository it starts in, and
    the panel knows no checkout path, so a launch would start it in the compositor's
    working directory where it cannot see the playbooks the diagnosis is about. Copying
    is also what `container-watch` does on this surface (§9a)
  - [x] ✅ The path reaches the panel through the status document, and `record_host_state`
    writes the handoff **before** the document that names it — a path recorded first is
    a button that fails in the user's hands. Falsified: computing the path instead of
    taking the write's result turns the ordering test red

### Phase 4: `fedora-desktop` GNOME panel extension

> Design: [DESIGN-panel.md](DESIGN-panel.md) §§1–11. A bare `§` below is a section of
> that file; anything owned elsewhere names its document.

- [x] ✅ **Task 4.1**: Scaffold `extensions/fedora-desktop@fedora-desktop` —
  `metadata.json`, `statusDocument.js`, `sections/health.js`, `extension.js`,
  `stylesheet.css`, plus the producer `helpers/host_health/status_document.py` and the
  cross-language contract gate `helpers/gnome/check_panel_contract.py` in `qa-all.bash`
- [ ] 🔄 **Task 4.2**: Health section — renders Phase 3's four checks
  - [x] ✅ Registered and rendering; `unavailable` has its own icon, never the neutral
    one — and this is now **tested**, in `tests/extensions/test-panel-indicator.mjs`,
    driving `enable()` on the shipped `extension.js`. It was asserted here and untested:
    the loader mapped the shell's `extension.js` import from the first commit while
    `gi-stubs.mjs` exported no `Extension`, so any test importing it failed on the
    import. Falsified on three mutants — sharing the neutral icon, dropping the
    `unavailable` colour, and starting neutral before the first read lands
  - [x] ✅ Renders the document's self-section reason, so an unreadable document says why
    rather than showing four derived "no such section" lines
  - [x] ✅ **The ledger's emptiness is now its own check**, `play-ledger`, not a
    reinterpretation of `play-freshness` — and emptiness is a **fault**, not an unknown.
    `helpers/play_ledger/ledger_presence.py`, 9 tests
    ([DESIGN-play-ledger.md](DESIGN-play-ledger.md) §8)
  - [x] ✅ What a finding does when activated: **nothing, and that is the answer**. One
    handoff file describes every finding, so a clickable row per finding would offer the
    same command N times while implying each had its own. The offer is section-level
    (§9a, Task 3.3)
  - [x] ✅ **The panel is boot-aware**, and `resolvedSection` is the ONE place the demotion
    happens, so the menu and the icon read the same answer (§11)
  - [x] ✅ `state` is **derived** from the lists, as the producer derives it (§11,
    [DESIGN-server-route.md](DESIGN-server-route.md) §4.3)
  - [x] ✅ A **malformed** document is reported, not read as a clean host (§11, mirroring
    `unreadable_reasons` — [DESIGN-server-route.md](DESIGN-server-route.md) §4.1a)
  - [x] ✅ Proven by `tests/extensions/test-panel-sections.mjs` — 27 tests importing the
    **shipped** files through a `gi://` loader, falsified on six mutants. **Not** the
    contract gate, which is a vocabulary check (§11,
    [DESIGN-server-route.md](DESIGN-server-route.md) §4.2)
  - [ ] ⬜ **HOST**: the rendering itself — whether St shows the demoted lines legibly and
    whether the icon is the right thing to look at. Only a Wayland session can say, and
    the harness deliberately does not claim to
- [ ] ⬜ **Task 4.3**: Play/task runner — plays with their ledger state, launched in a
  visible terminal, never in the background. Which plays it lists needs the ledger's real
  contents from Task 1.2's HOST run
- [x] ✅ **Task 4.4**: Sections registered, not hardcoded — one array entry per section
- [ ] 🔄 **Task 4.5**: ESLint clean, deployed by its own play, Wayland-correct
  - [x] ✅ ESLint and compat gate green; `play-fedora-desktop-panel.yml` deploys it
  - [x] ✅ The contract gate compares a **derived** set — 9 constants, plus every key of
    a built document and every section id from the real seam — so a name added on the
    producer side cannot be one the gate forgot. Falsified on five mutants
  - [ ] ⬜ **HOST**: run the play, log out and back in, confirm the panel appears and its
    health section renders all four checks

### Phase 5: Recover the desktop background after a monitor change

> Reasoning, evidence and the known limitation for this phase live in
> [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md) §Recovery.
> Task state stays here.

- [x] ✅ **Task 5.1**: Establish the real cost and the real bug — a **rendering**
  failure, not a texture failure, so image size is irrelevant. Converges on upstream
  `mutter#4767`, though **not confirmed on the axis that would have settled it** and not
  fixable here either way
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
    its own, applies no fix, and launches no play"*, which this would end; a user
    systemd unit keeps that contract intact but costs a long-running daemon whose only
    job is to watch one signal the shell already dispatches. Reasoning and the third
    option in [DESIGN-panel.md §12](DESIGN-panel.md). Implementable and unit-testable
    here once chosen; the HOST item below gates shipping it either way
  - [ ] ⬜ **HOST**: deploy it, and separately confirm the refresh actually clears a
    black background when the symptom is present — exercised on a healthy desktop only

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
`untracked/meta-deploy.bash` to run this plan alongside the others waiting.
`deploy.bash` runs four plays in a deliberate order, with `play-displaylink.yml` last
because it is the only one that can demand a MOK enrolment and a reboot; its change gate
says so before anything runs. `acceptance.bash` carries nineteen COVERAGE-registered
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
