# Plan 00109: desktop drift detection and fedora desktop panel

**Status**: In Progress
**Created**: 2026-09-11
**Owner**: joseph
**Priority**: High

## Overview

On 2026-09-11 the laptop rebooted into kernel 7.2.4 and both DisplayLink monitors
stayed dark. The cause was not subtle: `evdi` 1.14.16 cannot build against kernel
7.2 (the DRM atomic helpers changed `drm_atomic_state` to `drm_atomic_commit`), so
the DKMS autoinstall failed at boot and the module simply did not exist. The repo
had already pinned the fix — `evdi` 1.15.0, whose release notes say "preliminary
support for linux kernel 7.2" — in commit `588d1ae`, months earlier. The host had
never been told.

**Every automated check this repo owns was green throughout.**
`check-pinned-versions.bash` compares the repo pin against *upstream latest* and
correctly said "up to date". `qa-deployed-drift.bash` compares repo scripts against
*deployed scripts* under `~/.local/bin` and correctly said "in sync". Neither
compares the repo pin against **what is actually installed on this host**, which is
the axis that failed. Nothing watches that axis today.

The exposure is structural, not specific to DisplayLink. `playbook-main.yml` imports
the core plays, so those get re-run whenever main is run. The other **43 plays under
`playbooks/imports/optional/`** are run by hand, once, and then forgotten — there is
no record that they were ever run, at what commit, or whether they have changed
since. DisplayLink is simply the one that bit first, and it bit at the worst moment:
after a reboot, with no visible explanation.

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

- [x] ✅ **Task 0.1**: Restore DisplayLink on kernel 7.2.4
  - [x] ✅ Diagnose: `evdi` 1.14.16 DKMS build failure against 7.2 DRM API
  - [x] ✅ Confirm repo pin (1.15.0) already carries the upstream fix
  - [x] ✅ Run `play-displaylink.yml` on HOST; verify module built, signed, loaded
  - [x] ✅ Verify both DisplayLink heads enumerate (`card2-DVI-I-1`, `card3-DVI-I-2`)
- [ ] ⬜ **Task 0.2**: Remove orphaned DKMS source trees
  - [ ] ⬜ Establish whether `/usr/src/evdi-1.14.{10,11,12,16}` are reclaimable and
    who owns them (RPM-owned vs left behind) — probe goes in `triage.bash`
  - [ ] ⬜ Add cleanup to the owning play, gated on the tree being unregistered in DKMS
  - [ ] ⬜ Run QA, deploy on HOST, re-run `triage.bash` to confirm
- [ ] 🚫 **Task 0.3**: Fix group/world-readable vault password file permissions
  - **Blocked — human-only.** The path is protected by `secret_file_guard`; an agent
    cannot name it in a command, a script, or a playbook task, so this cannot be
    fixed via IaC by an agent. Flagged at session start by `secret_file_hygiene_checker`.
    Requires the user to set owner-only permissions by hand, or to lift the guard.

### Phase 1: Host play-run ledger

- [x] ✅ **Task 1.1**: Design the ledger record and its location —
  [DESIGN-play-ledger.md](DESIGN-play-ledger.md), Decision 2 below. One record per
  **play**, not per playbook run; append-only JSONL at
  `~/.local/state/fedora-desktop/play-ledger/runs.jsonl` (`0600`), so it survives a
  re-clone and cannot be committed. The hash's job is corrected there: git already
  answers "did this play change between two commits" — `play_sha256` is the
  **dirty-tree guard**, for the case where the commit is a lie
- [ ] ⬜ **Task 1.2**: Write the ledger on every play run
  - [x] ✅ Hook point: `callback_plugins/play_ledger.py`, enabled in `ansible.cfg`,
    which declared no `callback_plugins` path before this. Caught by `./run.bash`, by
    a playbook's shebang and by a bare `ansible-playbook` alike — but **defeated by
    `ANSIBLE_CONFIG`**, so Phase 2 may never call the ledger complete by construction
  - [x] ✅ Fail-fast in the only form available: Ansible **swallows** a callback's
    exception, so a write failure becomes a `BROKEN` sentinel plus a stderr
    `LEDGER-WRITE-FAILED`, which Phase 2 reads first and refuses to answer past.
    `--check` and `--list-*` runs record nothing — they applied nothing. Reasoning and
    the corrected outcome-folding: [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §3
  - [ ] ⬜ **HOST**: verify against a real run — unprovable in the container. Genesis
    plus one row per play; `--check` adds nothing; a second run appends
- [x] ✅ **Task 1.3**: Backfill — **none.** The day-one flood is answered by a
  reporting rule rather than invented history: a play with no record has never been
  run here, and silence is the correct output for it, so the 43 never-run plays say
  nothing instead of 43 wrong things. A `genesis` record at creation is what makes
  that silence unambiguous. `ledger.genesis_record` + `store.ensure_ledger`;
  reasoning in [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §4

### Phase 2: Drift checks built on the ledger

- [x] ✅ **Task 2.1**: Play-freshness check — `freshness.py` (verdicts),
  `git_history.py` (fetch-only git), `check_freshness.py` (executor), 47 tests.
  Findings name the commit subjects that touched each play, so the report says
  *what* changed. Three exit statuses: clean and silent, findings, and
  **untrustworthy** — because "nothing is stale" and "I cannot tell you" are
  different answers. Design, verdict table and the two structural silences:
  [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §6. Smoke-tested against this
  repo, both the findings path and the sentinel path
- [ ] 🔄 **Task 2.2**: Installed-vs-pinned check — the axis that failed
  - [x] ✅ The comparison: `helpers/version_pins/compare.py`, 30 tests. Five states,
    **only `MATCH` clean** — `AHEAD` is a finding (the repo no longer describes this
    host) and `UNDETERMINED` is a finding rather than a pass, which is this task's
    own fail-loudly requirement. Ordering is numeric and canonical; as strings
    `1.14.16` sorts *before* `1.14.9`
  - [x] ✅ **The gate this task demands**: a host a minor version behind its pin is a
    finding, *and* the state after the fix is clean — both asserted, so the check can
    fail against the failure it was built for and is not merely noisy
  - [ ] ⬜ Reuse the existing pin manifest in `check-pinned-versions.bash` rather
    than duplicating it (it already maps playbook→var→upstream repo). **It is a
    heredoc inside that script**, so reuse means extracting it to a declared file
    both read — the shape `vars/gnome-shell-extensions.yml` took in Plan 00112.
    That edits a live QA gate, so it needs its own control fixture
  - [ ] ⬜ Resolve what is *installed* per pin (rpm query, binary `--version`, DKMS
    status) — this is per-pin logic and cannot be fully generic; fail loudly on
    a pin whose install state cannot be determined rather than reporting a pass
  - [ ] ⬜ Gate: must report FAIL against the 2026-09-11 state (evdi 1.14.16
    installed, 1.15.0 pinned). A check that cannot fail against the incident it
    was built for is not a check.
- [x] ✅ **Task 2.3**: Wire both into the QA suite where appropriate — **decided:
  neither belongs in `qa-all.bash`.** Both ask "is this host what the repo says",
  which pre-commit is not asking and cannot act on; in a container both would find
  nothing, exit 0, and become two gates that cannot fail wherever CI runs them.
  Their home is Phase 3's login surface. Reasoning, and why
  `qa-deployed-drift.bash` is the exception rather than the counter-example:
  [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §7. Their **tests** are already in
  `qa-all` via `qa-helper-tests.bash`, which is the part that belongs there

### Phase 3: Login-time health surfacing and Claude Code handoff

- [ ] 🔄 **Task 3.1**: Post-boot health probe — probe done, login wiring pending
  - [x] ✅ `helpers/host_health/probe_results.py`, 29 tests. DKMS modules with no
    `installed` build **for the kernel that actually booted** — the incident's own
    shape, and why a non-empty `dkms status` fooled everyone — plus failed system and
    user units. A probe that could not run is a **finding**, not a skip. Phase 2's
    findings merge through one `extra` argument, so a broken host gets one
    notification rather than three. Detail: [DESIGN-host-health.md](DESIGN-host-health.md)
  - [x] ✅ Coordinated, not duplicated: neither Plan 00086 nor 00074 owns a reusable
    probe — both are fixes *inside* a play and inside `run.bash` — so there is nothing
    to call, and what is avoided is re-implementing their logic
  - [x] ✅ `helpers/host_health/probe.py`, 21 tests — the half that touches the
    machine. Every route out of `run_probe` ends in a `ProbeOutcome`, never a
    traceback, and **the classifier enforced that for `dkms` only**: `systemctl`
    was plain text, so one that could not run returned an empty unit list —
    identical to a healthy host. Both scopes now carry an outcome. Smoke-run in
    the container, which has neither `dkms` nor a systemd bus: 3 findings, 3
    lines, exit 1, and 2 of them are what that hole swallowed.
    [DESIGN-host-health.md](DESIGN-host-health.md) §6
  - [ ] ⬜ Running it at **end of login** rather than at boot. Lands with Task 3.2
    — a login unit whose output nothing surfaces is not a deliverable, and the
    open decision is 3.2's: [DESIGN-host-health.md](DESIGN-host-health.md) §7
- [ ] ⬜ **Task 3.2**: Surface findings to the user
  - [ ] ⬜ Desktop notification on findings; **silent when clean** (a health check
    that always speaks gets muted, and then it is not a health check)
- [ ] ⬜ **Task 3.3**: Claude Code handoff
  - [ ] ⬜ Write a findings/prompt file describing what broke and the evidence
  - [ ] ⬜ Offer to launch **CC** (not CCY) against the repo with an initial prompt
    to read that file and discuss — reproducing the 2026-09-11 session automatically
  - [ ] ⬜ Handoff is **offered**, never automatic

### Phase 4: `fedora-desktop` GNOME panel extension

- [ ] ⬜ **Task 4.1**: Scaffold `extensions/fedora-desktop@fedora-desktop`
  - [ ] ⬜ Follow the established pattern of the four existing extensions; reuse the
    extension→CLI-helper split proven by Plan 00041
  - [ ] ⬜ Single panel icon opening a generic, section-based panel
- [ ] ⬜ **Task 4.2**: Health section — surface Phase 3 findings, offer the handoff
- [ ] ⬜ **Task 4.3**: Play/task runner section — list plays, show ledger state
  (last run, stale or not), launch a run in a terminal
  - [ ] ⬜ Never run a play silently in the background; always in a visible terminal
- [ ] ⬜ **Task 4.4**: Keep the panel generic — sections are registered, not
  hardcoded, so quick-launch and other tools can be added without a rewrite
- [ ] ⬜ **Task 4.5**: ESLint clean (`cd extensions && node_modules/.bin/eslint`),
  deployed by a play, Wayland-correct

### Phase 5: Recover the desktop background after a monitor change

- [x] ✅ **Task 5.1**: Establish the real cost and the real bug.
  Evidence: [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md)
  - [x] ✅ Decode is **once, not per monitor** — a ~520 MiB claim made during
    triage was wrong — and is paid roughly **once per login**, not per dock cycle.
  - [x] ✅ **The symptom is a rendering failure, not a texture failure.** The
    wallpaper renders correctly in the Overview and is black only on the normal
    desktop; both draw the same `MetaBackground`, so a valid Overview render
    **proves the texture exists**, ruling out a cache miss, a slow decode and a
    NULL texture together.
  - [x] ✅ Therefore the cause is in the `MetaBackgroundContent` paint path for the
    desktop view. Three checks converge on **`mutter#4767`** (empty redraw clip)
    over the sticky `CHANGED_BACKGROUND` variant: no Cogl/framebuffer errors are
    logged this boot (the sticky variant stems from FBO allocation failure); the
    host runs `mutter-50.4`, which carries #4767 unfixed; and the monitors go
    *black*, whereas the sticky variant would paint the flat `primary-color`,
    here a dark slate blue. Convergent, not conclusive — that branch never logs.
    Both are upstream and open; neither is fixable here.
  - [ ] ⬜ **Confirm with the user**: literally black, or dark blue-grey? Blue-grey
    would overturn the above and point back at the flat-colour path.
- [x] ❌ **Task 5.2**: ~~Scale the wallpaper~~ — **cancelled, wrong problem.**
  Image size is irrelevant to this symptom (see 5.1), and a playbook is the wrong
  mechanism for wallpaper regardless. The rejected draft's two defects — a
  hardcoded `3840x2560`, and a setting that silently reverts — are recorded in
  the 12:05 journal entry as markers for their class.
- [x] ❌ **Task 5.3**: ~~Per-monitor pre-scaled caching~~ — **cancelled with 5.2.**
  GNOME already ships this (background `.xml` with `<size>`) and no third-party
  tool does; the finding survives in the research document.
- [x] ✅ **Task 5.4**: Recover the background after a monitor reconfiguration
  - Delivered in `9a79dd7`. `Action.REFRESH_BACKGROUND` in
    `helpers/displaylink_recovery/`, fired by the existing dock udev rule and
    suspend service, strictly after the wedge ladder and never while locked.
  - **Three faults found while adding it meant Plan 00056's recovery had never
    run on this host at all** — a deploy that always failed on a missing parent
    directory, a wedge signature that was always true (`getsize()` on sysfs
    returns 0), and dconf writes discarded because `sudo` strips the bus address.
    Each verified on HOST and fixed in `9a79dd7`; detail in the 13:05 journal
    entry.
  - ⚠️ **Known limitation — the resume path is effectively inert.** Measured on
    this host: `lock-enabled true`, `lock-delay 0`, so the screen is already
    locked when `displaylink-suspend.service` runs. `_session_locked()` then
    correctly refuses (the toggle leaks ~57 MB per monitor from a lock screen)
    and the run prints `action=none` — indistinguishable from "nothing needed".
    The **dock/udev path still works**, since a user moving monitors around is
    present and unlocked; the close-lid → reopen → unlock case is not covered.
  - [ ] ⬜ **T5.4a**: Cover the unlock case. Needs something in the *user*
    session that reacts to unlock, not a root oneshot — Phase 4's panel or a
    user systemd unit is the natural owner.
  - [ ] ⬜ **Still to confirm in the wild**: that the refresh actually clears the
    black background when the symptom is present. It has been exercised on a
    *healthy* desktop (runs clean, all three background keys unchanged) but not
    yet against the live fault.
  - **The one workaround that matches this failure**: force a real `bg-changed` by
    toggling `picture-uri`. That is the only signal that re-sets
    `CHANGED_BACKGROUND`; another `monitors-changed` does not recover it, and
    `updateResolution()` refreshes only the animation. Restarting gnome-shell also
    works but is not available under Wayland.
  - ⚠️ **Do not toggle while the session is locked** — `gnome-shell#9188` reports
    a ~57 MB per-monitor leak on that path. The recovery must check lock state and
    defer.
  - [ ] ⬜ Confirm the toggle actually recovers it on this host before building
    anything around it — one manual toggle, next time the symptom appears
  - [x] ✅ Decide the home — **extend** `helpers/displaylink_recovery/`, not a
    sibling: the compositor-layer failure shares the driver-layer one's trigger, so
    it reuses the existing udev rule and suspend service rather than inventing a
    trigger path. `Action.REFRESH_BACKGROUND` (`recovery.py:64`), dispatched at
    `run_recovery.py:411` (`9a79dd77`)
  - [x] ✅ Idempotence and loop-safety — `needs_background_refresh`
    (`recovery.py:101`) returns False once `attempted_background_refresh` is set, so
    the toggle cannot re-trigger on the key it writes, and False while
    `session_locked`, which is what keeps it off the `gnome-shell#9188` leak path
  - [x] ✅ Tests first, stdlib-only, mirroring the helper path
    (`tests/helpers/displaylink_recovery/test_recovery.py`); QA green
  - [ ] ⬜ **HOST**: deploy and verify. Distinct from the confirmation above — this
    is "the shipped code runs on the host", not "the toggle cures the fault"

## Dependencies

- Relates to Plan 00041 (remote desktop toggle) — extension→CLI-helper pattern to reuse
- Relates to Plan 00058 (version pins) — this plan adds the install-state axis it lacks
- Relates to Plan 00074 (boot preflight) — coordinate, do not duplicate
- Relates to Plan 00086 (kernel modules absent) — coordinate, do not duplicate

## Technical Decisions

### Decision 1: Detection and handoff, never unattended repair

**Context**: The failure mode is a host silently diverging from the repo. The
tempting fix is to auto-run stale plays.
**Options considered**: (A) auto-run stale plays on detection — self-healing, but a
play that prompts, reboots, or enrols a MOK key cannot run unattended, and an
unattended Ansible run on a desktop at login is a way to lose a working machine.
(B) detect, report, offer a one-click assisted fix.
**Decision**: B. The whole incident was survivable; what made it expensive was not
knowing *why*. Information is the deliverable.
**Date**: 2026-09-11

### Decision 2: The ledger is per-play host state, and its failures are recorded not raised

**Context**: every Phase 2 check compares against the ledger, so a silently wrong
ledger makes every check downstream silently wrong.
**Decision**: one record per **play**, append-only JSONL under
`$XDG_STATE_HOME/fedora-desktop/play-ledger/`, written by a callback plugin. Since
Ansible **swallows exceptions raised inside a callback**, a write failure cannot
fail the run — it leaves a `BROKEN` sentinel and Phase 2 reports FAIL while it
exists, turning an unfailable hook into a failable check. No backfill: a play with
no record has never been run here, and silence is the correct output for it.
**Reasoning, record shape, limits**: [DESIGN-play-ledger.md](DESIGN-play-ledger.md).
**Date**: 2026-09-14

## Success Criteria

- [ ] The installed-vs-pinned check **fails** when pointed at the 2026-09-11 state
  and passes now — demonstrated, not asserted
- [ ] The freshness check reports a play edited after its ledgered run, and stays
  silent about the 43 plays never run here
- [ ] A clean system produces **no notification at all** at login
- [ ] The panel opens from one icon and shows health plus play state
- [ ] `./scripts/qa-all.bash` passes; ESLint passes for the extension
- [ ] Host-only checks skip cleanly in the CCY container and in CI
- [ ] `qa-reviewer` agent run over the full plan diff, findings resolved

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
