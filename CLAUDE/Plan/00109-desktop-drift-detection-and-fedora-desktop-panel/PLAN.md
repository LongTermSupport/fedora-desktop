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
the core plays, so those get re-run whenever main is run. The **plays under
`playbooks/imports/optional/`** (46 today, one of them added by this plan) are run
by hand, once, and then forgotten — there is
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
  - [ ] ⬜ **HOST**: verify against a real run — unprovable in the container. Genesis
    plus one row per play; `--check` adds nothing; a second run appends
- [x] ✅ **Task 1.3**: Backfill — **none**, answered by a reporting rule instead: a play
  with no record has never been run here, and silence is correct for it

### Phase 2: Drift checks built on the ledger

> Complete. Verdict table, the three exit statuses and the two structural silences are in
> [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §§6–7; the offline-login decision in
> [DESIGN-host-health.md](DESIGN-host-health.md) §8; the pin axis in
> [DESIGN-version-pins.md](DESIGN-version-pins.md).

- [x] ✅ **Task 2.1**: Play-freshness — `freshness.py`, `git_history.py`,
  `check_freshness.py`. Clean-and-silent, findings, and **untrustworthy** are three
  different answers. An offline login is silent; a long silence is a finding. Reversing
  that ticked behaviour meant rewriting the test that asserted the opposite, not deleting it
- [x] ✅ **Task 2.2**: Installed-vs-pinned — the axis that actually failed. Passes both
  directions against the states the journal records, resolution is declared rather than
  guessed, and coverage has a floor: partial is a decision, zero is a check that cannot fail
- [x] ✅ **Task 2.3**: **Neither belongs in `qa-all.bash`.** Both ask "is this host what
  the repo says", which in a container finds nothing and exits 0 — two gates that cannot
  fail wherever CI runs them. Their tests are in `qa-all` via `qa-helper-tests.bash`,
  which is the part that does belong there

### Phase 3: Login-time health surfacing and Claude Code handoff

> Design: [DESIGN-host-health.md](DESIGN-host-health.md) §§1–11.

- [ ] 🔄 **Task 3.1**: Post-boot health probe — code done, HOST wiring pending
  - [x] ✅ `probe_results.py` (verdicts) and `probe.py` (the half that touches the
    machine); `host-health.service`, deployed by `play-host-health-login-report.yml`
  - [ ] ⬜ **HOST**: run the play, then assert the unit is actually *wanted* —
    `systemctl --user list-dependencies graphical-session.target` must name it. "The
    play succeeded" is a different claim
- [ ] 🔄 **Task 3.2**: Surface findings to the user — code done, HOST run pending
  - [x] ✅ `login_report.py` — one notification, silent when clean
  - [ ] ⬜ **HOST**: confirm a real notification arrives, and a clean login is silent
  - [ ] 🔄 **The server route.** Only the delivery was ever desktop-bound; the checks are
    profile-agnostic. Reasoning, the cadence derivation, the mutants and the two review
    findings are in [DESIGN-server-route.md](DESIGN-server-route.md)
    - [x] ✅ `status_document.py` (producer) and `login_message.py` (renderer)
    - [x] ✅ The delivery — the collection timer and the `~/.bashrc-includes` snippet,
      folded into `play-host-health-login-report.yml` (`scope: general`). Daily, derived
      from `STALE_AFTER_DAYS` (§1–2)
    - [x] ✅ The snippet prints **only for an interactive shell**, or it breaks `scp` to
      the host it reports on. 12 assertions, six mutants (§3)
    - [x] ✅ A fresh document can be about the **previous boot** — `render` reports a
      kernel mismatch in its own right, and demotes the one boot-scoped section rather
      than repeating its findings as present-tense faults (§4, §4.1)
    - [x] ✅ **A document the reader cannot interpret is reported, not read as clean.**
      Five malformed shapes were silent, including one whose own `state` said `findings`
      (§4.1a). The panel has the same gap from the other side — folded into the Task 4.2
      item below
    - [x] ✅ **A healthy server was never going to be silent** (qa-reviewer, 26-09-15).
      Two permanent findings, one root: no `dkms` on a server (§5). The first answer to
      the pin half silenced the founding incident on every desktop; the ledger now acts
      on one verdict, not on the population (§5.1). "No DKMS modules" is not "no DKMS" —
      the two are carried apart, or Task 0.2's own cleanup would go unreported (§5.2),
      and removing software on purpose is drift until the pin says otherwise (§5.3)
    - [x] ✅ Each branch of the merged play removes the other's artefacts, so correcting
      a mis-set profile does not leave both deliveries installed (§7)
    - [x] ✅ The VM lab runs this route end to end. `server-host-health-kernel-change`
      in `vars/vm-test-scenarios.yml` provisions a server guest with the play, and its
      fixture + checker make all fourteen claims below on a real boot (§8). A HOST run
      cannot make the kernel one on demand — it has to wait for a kernel update to
      arrive, where a guest can simply be given one
    - [x] ✅ `reboot_before_checks` is a scenario's answer, not a profile's; the CLI
      supplies only the mechanics of getting a guest back, and a profile it has no
      mechanics for is a refusal rather than a silent no-reboot (§8.1)
    - [ ] ⬜ **HOST**: run `play-vm-test-lab.yml` once, so the new scenario reaches the
      deployed allowlist and the two new guest scripts reach `~/.local/share/vmtest`.
      The bridge refuses an id that is only in the manifest — deliberately, and this is
      the only step an agent cannot do
    - [ ] ⬜ **VM**: `./scripts/vmtest-request.bash run-scenario server-host-health-kernel-change` — the timer arms, a document appears, **a clean
      server login is silent**, a live fault is reported as a fault, the guest reboots
      into a different kernel, the report names the boot mismatch, the previous boot's
      fault is demoted rather than repeated as current, and an `scp` through the guest's
      own `sshd` completes on both sides of the reboot
    - [ ] ⬜ **HOST**: confirm **this** checkout has a remote the timer can fetch
      **without an agent**, or the freshness axis reports "never reached the remote" for
      ever (§6). The VM proves the mechanism — its checker fetches with `SSH_AUTH_SOCK`
      unset — but a guest cloned over https says nothing about how this checkout's
      `origin` is configured, so this one stays a host fact
- [ ] 🔄 **Task 3.3**: Claude Code handoff — file and offer done
  - [x] ✅ `handoff.py`, mode `0600`; the wrong/not-looked-at split is carried in
    `Finding.checked`, not read from the prose
  - [ ] ⬜ The **one-click** offer — needs a surface that can receive a click, which is
    Phase 4's panel

### Phase 4: `fedora-desktop` GNOME panel extension

> Design: [DESIGN-panel.md](DESIGN-panel.md) §§1–10.

- [x] ✅ **Task 4.1**: Scaffold `extensions/fedora-desktop@fedora-desktop` —
  `metadata.json`, `statusDocument.js`, `sections/health.js`, `extension.js`,
  `stylesheet.css`, plus the producer `helpers/host_health/status_document.py` and the
  cross-language contract gate `helpers/gnome/check_panel_contract.py` in `qa-all.bash`
- [ ] 🔄 **Task 4.2**: Health section — renders Phase 3's three checks
  - [x] ✅ Registered and rendering; `unavailable` has its own icon, never the neutral one
  - [x] ✅ Renders the document's self-section reason, so an unreadable document says why
    rather than showing three derived "no such section" lines
  - [x] ✅ **The ledger's emptiness is now its own check**, `play-ledger`, not a
    reinterpretation of `play-freshness` — whose `EXIT_OK` on an empty ledger is correct
    for the question it asks, tested twice with reasoning, and has other callers.
    Emptiness is reported as a **fault**, not an unknown: `run.bash` ledgers every play
    and a play deploys the unit that runs this, so by the time anything reads it a record
    must exist. Silent while the `BROKEN` sentinel exists, which says the same absence
    with more detail. `helpers/play_ledger/ledger_presence.py`, 9 tests
  - [ ] ⬜ What a finding does when activated — a Task 3.3 decision
    ([DESIGN-panel.md](DESIGN-panel.md) §9)
  - [x] ✅ **The panel is boot-aware.** `statusDocument.isBootStale` is the same predicate
    as `status_document.is_boot_stale`, and `resolvedSection` is the ONE place the
    demotion happens — the menu and the icon read the same answer, or the icon would
    report a fault the menu had already explained away
  - [x] ✅ `state` is **derived** from the lists, as the producer derives it. A section
    saying `state: "ok"` over a populated `findings` list rendered "nothing to report"
    while the login report showed the fault (§4.3)
  - [x] ✅ A **malformed** document is reported, not read as a clean host: a group that
    is not a list, entries that are not strings, sections that cannot be read, and a
    document naming no checks at all (§4.1a, mirroring `unreadable_reasons`)
  - [x] ✅ Proven by `tests/extensions/test-panel-sections.mjs` — 17 tests importing the
    **shipped** `statusDocument.js` and `sections/health.js`, with a Node loader answering
    the `gi://` imports. Falsified on six mutants. **Not** the contract gate: that is a
    vocabulary check and `kernel` already satisfied it as an unused default
    ([DESIGN-server-route.md](DESIGN-server-route.md) §4.2) — though
    `BOOT_SCOPED_SECTION` is now in it too, since both readers must demote the same section
  - [ ] ⬜ **HOST**: the rendering itself — whether St shows the demoted lines legibly and
    whether the icon is the right thing to look at. Only a Wayland session can say, and
    the harness deliberately does not claim to
- [ ] ⬜ **Task 4.3**: Play/task runner — plays with their ledger state, launched in a
  visible terminal, never in the background. Which plays it lists needs the ledger's real
  contents from Task 1.2's HOST run
- [x] ✅ **Task 4.4**: Sections registered, not hardcoded — one array entry per section
- [ ] 🔄 **Task 4.5**: ESLint clean, deployed by its own play, Wayland-correct
  - [x] ✅ ESLint and compat gate green; `play-fedora-desktop-panel.yml` deploys it
  - [x] ✅ The contract gate compares a **derived** set — 7 constants, plus every key of
    a built document and every section id from the real seam — so a name added on the
    producer side cannot be one the gate forgot. Falsified on five mutants
  - [ ] ⬜ **HOST**: run the play, log out and back in, confirm the panel appears and
    renders the three sections

### Phase 5: Recover the desktop background after a monitor change

> Reasoning, evidence and the known limitation for this phase live in
> [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md) §Recovery.
> Task state stays here.

- [x] ✅ **Task 5.1**: Establish the real cost and the real bug — a **rendering**
  failure, not a texture failure, so image size is irrelevant. Converges on upstream
  `mutter#4767`; neither candidate is fixable here. The black-versus-blue-grey question
  that would have discriminated between the two candidates was **closed by the owner as
  resolved without a recorded answer**, so the `mutter#4767` attribution stands on the
  rendering evidence alone and was never confirmed on that axis
- [x] ❌ **Task 5.2**: ~~Scale the wallpaper~~ — cancelled, wrong problem
- [x] ❌ **Task 5.3**: ~~Per-monitor pre-scaled caching~~ — cancelled; GNOME ships it
- [x] ✅ **Task 5.4**: Recover the background after a monitor reconfiguration —
  `Action.REFRESH_BACKGROUND` in `helpers/displaylink_recovery/`, fired by the dock udev
  rule and the suspend service, after the wedge ladder and never while locked
  - ⚠️ **Known limitation — the resume path is effectively inert**: the screen is already
    locked when the suspend service runs, so the run correctly refuses. Dock/udev works
  - [ ] ⬜ **T5.4a**: Cover the unlock case. Needs something in the *user* session that
    reacts to unlock, not a root oneshot — Phase 4's panel is the natural owner
  - [ ] ⬜ **HOST**: deploy it, and separately confirm the refresh actually clears a
    black background when the symptom is present — exercised on a healthy desktop only

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
  silent about every play never run here
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
- Task 3.2's server route delivered: the collection timer and the interactive-only login
  snippet, folded into `play-host-health-login-report.yml` as its second delivery. The
  HOST run remains open, and the review found two ways it would never be silent there.
