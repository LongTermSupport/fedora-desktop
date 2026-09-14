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
`playbooks/imports/optional/`** (45 today, and this plan added one of them) are run
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
  run here, and silence is the correct output for it, so every never-run play says
  nothing instead of saying something wrong. A `genesis` record at creation is what makes
  that silence unambiguous. `ledger.genesis_record` + `store.ensure_ledger`;
  reasoning in [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §4

### Phase 2: Drift checks built on the ledger

- [x] ✅ **Task 2.1**: Play-freshness check — `freshness.py` (verdicts),
  `git_history.py` (fetch-only git, bounded by a timeout because it is the one
  network call on the login path), `check_freshness.py` (executor).
  Findings name the commit subjects that touched each play, so the report says
  *what* changed. Three exit statuses: clean and silent, findings, and
  **untrustworthy** — because "nothing is stale" and "I cannot tell you" are
  different answers. Design, verdict table and the two structural silences:
  [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §6. Smoke-tested against this
  repo, both the findings path and the sentinel path
  - [x] ✅ **An offline login is silent, and a long silence is a finding.** A failed
    `git fetch` is no longer untrustworthy on its own: "can I reach the remote now"
    is a fact about the network, "how long since I last could" is a fact about this
    host. `fetch_clock` stamps each success; past the declared bound it reports the
    gap, and never-fetched is its own message. The decision, and why neither simple
    answer was right: [DESIGN-host-health.md](DESIGN-host-health.md) §8. This
    **reversed** a ticked behaviour, so the test that asserted the opposite was
    rewritten to pin the new rule rather than deleted
- [x] ✅ **Task 2.2**: Installed-vs-pinned check — the axis that failed. Detail:
  [DESIGN-version-pins.md](DESIGN-version-pins.md)
  - [x] ✅ `compare.py` (5 states, only `MATCH` clean), `manifest.py` +
    `vars/version-pins.yml` (the declaration both consumers read),
    `check_pins.py` (per-pin resolution), `scripts/qa-version-pins.bash`
  - [x] ✅ **The gate this task demands, both directions**, against the states the
    journal records: `evdi/1.14.16` against a `1.15.0` pin is a finding, and
    `evdi/1.15.0-1.github_evdi` is clean — release suffix and all. A check that
    cannot fail against its incident is not a check; one that cannot pass is noise
  - [x] ✅ **Resolution is declared, never guessed**, and **coverage has a floor.** A
    pin declares `installed:` or is rejected; 1 of 9 is tracked today and the gate
    prints that split. Partial coverage is a decision, but zero is a check that
    cannot fail — so the gate fails at zero and the login report says
    `compared 0 of N`
- [x] ✅ **Task 2.3**: Wire both into the QA suite — **decided: neither belongs in
  `qa-all.bash`.** Both ask "is this host what the repo says", which in a container
  finds nothing and exits 0: two gates that cannot fail wherever CI runs them. Their
  home is Phase 3's login surface; their **tests** are already in `qa-all` via
  `qa-helper-tests.bash`, which is the part that belongs there. Reasoning, and why
  `qa-deployed-drift.bash` is the exception:
  [DESIGN-play-ledger.md](DESIGN-play-ledger.md) §7

### Phase 3: Login-time health surfacing and Claude Code handoff

- [ ] 🔄 **Task 3.1**: Post-boot health probe — probe done, login wiring pending
  - [x] ✅ `helpers/host_health/probe_results.py`. DKMS modules with no `installed`
    build **for the kernel that actually booted** — the incident's own shape, and why a
    non-empty `dkms status` fooled everyone — plus failed system and user units. A probe
    that could not run is a **finding**, not a skip.
    [DESIGN-host-health.md](DESIGN-host-health.md) §§1–3
  - [x] ✅ Coordinated, not duplicated: neither Plan 00086 nor 00074 owns a reusable
    probe — both are fixes *inside* a play and inside `run.bash`.
    [DESIGN-host-health.md](DESIGN-host-health.md) §4
  - [x] ✅ `helpers/host_health/probe.py` — the half that touches the machine. Every
    route out of `run_probe` ends in a `ProbeOutcome`, never a traceback, and **the
    classifier enforced that for `dkms` only** — a `systemctl` that could not run
    returned an empty unit list, identical to a healthy host.
    [DESIGN-host-health.md](DESIGN-host-health.md) §6
  - [x] ✅ Running it at **end of login** rather than at boot:
    `host-health.service`, `After=graphical-session.target`, deployed by
    `play-host-health-login-report.yml`. `WorkingDirectory` is templated from
    `root_dir` — no checkout path reaches the repo. `SuccessExitStatus=0 1`, because
    exit 1 means "there are findings", and a drifted host must not also register as
    a broken service: two alarms for one fact is how both get ignored
  - [ ] ⬜ **HOST**: run the play, then assert the unit is actually *wanted* —
    `systemctl --user list-dependencies graphical-session.target` must name it. Not
    "the play succeeded": `WantedBy=graphical-session.target` is new in this repo
    (`play-container-watch.yml` is timer-activated and has no `[Install]` at all), and
    deployed-but-not-enabled already shipped once today looking exactly like success
- [ ] 🔄 **Task 3.2**: Surface findings to the user — code done, HOST run pending
  - [x] ✅ `helpers/host_health/login_report.py`. **One** notification listing
    everything, not three; **silent when clean**; host-health findings first,
    because something broken now outranks something that merely drifted
  - [x] ✅ **The freshness seam keeps its two channels apart** — what it found about
    the host, and what happened to it while looking. One sink made every diagnostic a
    finding. [DESIGN-host-health.md](DESIGN-host-health.md) §9
  - [x] ✅ **Merged, not chained**, and **the notification is not the only channel** —
    a raising check names itself instead of silencing the others, and a bus that is not
    there does not discard the findings. [DESIGN-host-health.md](DESIGN-host-health.md) §10
  - [ ] ⬜ **HOST**: confirm a real notification arrives, and that a clean login is
    genuinely silent
  - [ ] 🔄 **A server profile gets no drift detection at all.** The play is
    `scope: gnome`, so it ends the play there — correctly for the *delivery*
    (`notify-send` needs a session bus, and `graphical-session.target` never
    activates on a server), but the ledger, play-freshness and installed-vs-pinned
    checks are profile-agnostic and a server is where unattended drift matters most.
    Needs a second delivery route, not a scope change. Raised by the user
    - [x] ✅ The half both routes share is built: `helpers/host_health/status_document.py`
      writes the checks' verdict to a file, so *running the checks* and *telling the
      user* stop being one step. That is what makes a server route affordable — a
      synchronous `git fetch` at every SSH login would add latency to every login and
      can hang, while printing a cached document costs nothing
    - [ ] ⬜ The route itself: which schedule writes it, and what prints it at login.
      Unowned decision
- [ ] 🔄 **Task 3.3**: Claude Code handoff — file and offer done, one-click is Phase 4
  - [x] ✅ `helpers/host_health/handoff.py`. The prompt file separates *"this is
    wrong"* from *"this was not looked at"*, and says of the second that these are
    **not** clean results. Mode `0600`: it records what is broken about this host
  - [x] ✅ **The split is carried in the data, not guessed from the prose** —
    `Finding.checked`, the producing check's own answer. Substring matching misfiled
    **6 of 13**; measured again after, **0 of 13**.
    [DESIGN-host-health.md](DESIGN-host-health.md) §11
  - [x] ✅ The prompt asks for a **diagnosis and a discussion**, and says in terms
    not to apply a fix or run a playbook. Strict IaC and the plan's own Non-Goals
    both say re-running a play is the operator's decision
  - [x] ✅ Offered, never automatic: `offer()` returns a **string** naming the file
    and the `claude` command — `claude`, not `ccy`, because diagnosing a broken host
    from inside a container cannot see the host. A test exists so that growing a
    subprocess call here gets noticed
  - [ ] ⬜ The **one-click** offer. A printed command is the offer today; a clickable
    one needs a surface that can receive a click, which is Phase 4's panel

### Phase 4: `fedora-desktop` GNOME panel extension

- [ ] 🔄 **Task 4.1**: Scaffold `extensions/fedora-desktop@fedora-desktop`
  - [x] ✅ Design settled in writing first — [DESIGN-panel.md](DESIGN-panel.md) — covering
    the two things the existing extension pattern does not: one aggregate status document
    rather than one file per producer, and the three states `ok`/`findings`/**`unavailable`**,
    because an absent document is ignorance and must not render as health
  - [x] ✅ **The producer, before either consumer**: `helpers/host_health/status_document.py`.
    Three states distinct in the data, `unavailable` read from `Finding.checked` rather
    than the wording, and an absent or unparseable or unknown-schema document reported as
    `unavailable` — never as an empty one, which renders as health. Its atomicity test
    survived a mutant that wrote straight to the destination; the test now pins what a
    *failed* write leaves behind, which is the only thing that tells the two apart
  - [ ] ⬜ The scaffold, landing with 4.2's section as its first consumer — a registry
    with nothing registered cannot be exercised
- [ ] ⬜ **Task 4.2**: Health section — surface Phase 3 findings, offer the handoff
  - [ ] ⬜ **Nothing checks that the ledger has any content**, and the panel is where it
    shows. A smoke run against an empty state directory publishes
    `play-freshness: ok` — correct for the question that check asks (no ledgered play
    has drifted, because there are none), and a green tick on a host with no ledger.
    Not a defect in `check_freshness`: an empty ledger has no holes in it, the
    `EXIT_OK` is tested and reasoned, and it has other callers whose contract that
    exit code is. The gap is a **missing check** — at login the case is reachable only
    one way, because the unit is deployed by a play and `run.bash` ledgers every play,
    so an empty ledger at login means it was lost. Needs its own section, not a
    reinterpretation of this one
- [ ] ⬜ **Task 4.3**: Play/task runner — plays with their ledger state, launched in a
  visible terminal, never in the background
- [ ] ⬜ **Task 4.4**: Sections registered, not hardcoded, so quick-launch and other
  tools can be added without a rewrite
- [ ] ⬜ **Task 4.5**: ESLint clean (`cd extensions && node_modules/.bin/eslint`),
  deployed by its own play, Wayland-correct

### Phase 5: Recover the desktop background after a monitor change

> Reasoning, evidence and the known limitation for this phase live in
> [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md) §Recovery.
> Task state stays here.

- [x] ✅ **Task 5.1**: Establish the real cost and the real bug — **a rendering
  failure, not a texture failure**, so image size is irrelevant. Converges on
  upstream `mutter#4767`; neither candidate is fixable here
  - [ ] 🚫 **Blocked on the user, one question**: are the monitors literally black,
    or dark blue-grey? Blue-grey is the flat `primary-color` and would overturn the
    above, pointing back at the other candidate. Nothing here can answer it
- [x] ❌ **Task 5.2**: ~~Scale the wallpaper~~ — **cancelled, wrong problem.** Image
  size is irrelevant (5.1), and a playbook is the wrong mechanism for wallpaper. The
  rejected draft's two defects are in the 12:05 journal entry as markers for the class
- [x] ❌ **Task 5.3**: ~~Per-monitor pre-scaled caching~~ — **cancelled with 5.2.**
  GNOME already ships it (background `.xml` with `<size>`) and no third-party tool does
- [x] ✅ **Task 5.4**: Recover the background after a monitor reconfiguration —
  delivered in `9a79dd7`, `Action.REFRESH_BACKGROUND` in
  `helpers/displaylink_recovery/`, fired by the dock udev rule and the suspend
  service, after the wedge ladder and never while locked. Adding it found **three
  faults meaning Plan 00056's recovery had never run on this host at all**
  - ⚠️ **Known limitation — the resume path is effectively inert**, because the
    screen is already locked when the suspend service runs, so the run correctly
    refuses and prints `action=none`. The dock/udev path still works
  - [ ] ⬜ **T5.4a**: Cover the unlock case. Needs something in the *user* session
    that reacts to unlock, not a root oneshot — Phase 4's panel is the natural owner
  - [ ] ⬜ **Still to confirm in the wild**: that the refresh clears the black
    background when the symptom is present. Exercised on a *healthy* desktop only
  - [x] ✅ Decide the home — **extend** `helpers/displaylink_recovery/`, not a
    sibling: the compositor-layer failure shares the driver-layer one's trigger, so it
    reuses the existing udev rule and suspend service rather than inventing one
  - [x] ✅ Idempotence and loop-safety — `needs_background_refresh` returns False once
    `attempted_background_refresh` is set, so the toggle cannot re-trigger on the key
    it writes, and False while locked, keeping it off the `gnome-shell#9188` leak path
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
