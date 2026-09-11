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
- Managing the user's choice of wallpaper image. Phase 5 manages *size and
  delivery*, not aesthetics.

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

- [ ] ⬜ **Task 1.1**: Design the ledger record and its location
  - [ ] ⬜ Decide record shape: play path, repo commit at run, play file hash, run
    outcome, timestamp. Hash matters because commit alone cannot tell you
    whether *this play* changed.
  - [ ] ⬜ Decide storage location and ownership (host state, not repo state — it
    must not be committed, and must survive a re-clone)
  - [ ] ⬜ Record the decision in `## Technical Decisions`
- [ ] ⬜ **Task 1.2**: Write the ledger on every play run
  - [ ] ⬜ Establish the hook point that cannot be bypassed by running
    `ansible-playbook` directly — a callback plugin is the candidate; confirm
    whether `ansible.cfg` already loads one
  - [ ] ⬜ Fail-fast: a ledger write failure must not silently produce a blank ledger
- [ ] ⬜ **Task 1.3**: Backfill what is already known
  - [ ] ⬜ A fresh ledger claims nothing has ever been run, which would report all 43
    optional plays as stale on day one. Decide how the first run seeds itself
    without either lying or flooding.

### Phase 2: Drift checks built on the ledger

- [ ] ⬜ **Task 2.1**: Play-freshness check
  - [ ] ⬜ `git fetch` only; compare each ledgered play against its state at HEAD
  - [ ] ⬜ Report only plays **run here** that have since changed; never mention
    plays never run
  - [ ] ⬜ Report *what* changed (commit subjects touching that play), not just that
    it did
- [ ] ⬜ **Task 2.2**: Installed-vs-pinned check — the axis that failed
  - [ ] ⬜ Reuse the existing pin manifest in `check-pinned-versions.bash` rather
    than duplicating it (it already maps playbook→var→upstream repo)
  - [ ] ⬜ Resolve what is *installed* per pin (rpm query, binary `--version`, DKMS
    status) — this is per-pin logic and cannot be fully generic; fail loudly on
    a pin whose install state cannot be determined rather than reporting a pass
  - [ ] ⬜ Gate: must report FAIL against the 2026-09-11 state (evdi 1.14.16
    installed, 1.15.0 pinned). A check that cannot fail against the incident it
    was built for is not a check.
- [ ] ⬜ **Task 2.3**: Wire both into the QA suite where appropriate
  - [ ] ⬜ Decide which belong in `qa-all.bash` (host-only checks must skip cleanly
    in CCY and CI, as `qa-deployed-drift.bash` already does)

### Phase 3: Login-time health surfacing and Claude Code handoff

- [ ] ⬜ **Task 3.1**: Post-boot health probe
  - [ ] ⬜ Detect: failed DKMS builds, failed/degraded systemd units (system and
    user), kernel modules expected-but-absent, plus Phase 2 drift
  - [ ] ⬜ Coordinate with Plan 00086 (kernel modules absent enumeration) and
    Plan 00074 (boot preflight) rather than duplicating their logic
  - [ ] ⬜ Run at **end of login**, not at boot, so the user is present to see it
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

### Phase 5: Wallpaper sizing and management

- [x] ✅ **Task 5.1**: Establish the real cost and the real bug.
  Evidence and citations: [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md)
  - [x] ✅ Decode is **once, not per monitor** — a ~520 MiB claim made during
    triage was wrong. The lever is decode *latency* (342–466 ms vs 73–75 ms), not
    memory, and it is paid roughly **once per login**, not per dock cycle.
  - [x] ✅ Mechanism: `_updateBackgrounds()` destroys every background manager
    before rebuilding while the image cache holds only a **weak** reference, so
    survival across a hotplug is GJS GC timing. Losers paint the flat colour.
  - [x] ✅ **Two open upstream bugs cause the same symptom independently**
    (`mutter#4767`, `mutter#4935`), plus an apparently unreported sticky variant.
    **Scaling cannot fix those** — Phase 5 is at best a mitigation, and the
    discriminator under 5.2 decides whether it is even that.
- [ ] 🚫 **Task 5.2**: Scale the wallpaper — *mechanism undecided, play approach rejected*
  - **Current state**: wallpaper is a 7008x4672, 27 MB camera original at
    `~/.config/background`, unmanaged. A pre-scaled 3840x2560 copy exists at
    `~/.config/background-scaled.jpg`, **not applied**.
  - 🚫 **A playbook is the wrong mechanism, and a first draft proved it twice.**
    Ansible converges once; wallpaper selection is a recurring user action. A play
    that scales whatever is at the source path and repoints `picture-uri` loses
    its setting the moment the user picks a new wallpaper in GNOME Settings —
    GNOME repoints `picture-uri` at *their* choice, the managed copy is
    dereferenced, and the scaling only returns if someone re-runs Ansible. A
    setting that silently reverts is worse than no setting.
  - 🚫 The same draft hardcoded `wallpaper_max_geometry: "3840x2560"` — a fact
    about one laptop's panel, written into a public repo that provisions
    arbitrary hardware. See the 12:05 journal entry; the marker for this class is
    that the justifying comment named the machine.
  - **Design constraints carried forward**, whichever mechanism wins:
    - Target geometry is **discovered on the host at install time**, never a
      literal. A configurable default may exist; when discovered exceeds it, the
      target grows to the discovered size plus padding.
    - Discovery loops over `/sys/class/drm/*/status` + `modes`, so per
      `playbooks/CLAUDE.md` it belongs in a stdlib-only TDD'd helper under
      `helpers/`, invoked via `command:` + `argv:`.
    - `-auto-orient` before `-strip`, so dropping the EXIF orientation tag cannot
      rotate the result.
  - [ ] ⬜ **Run the free discriminator FIRST — it can cancel this whole task.**
    Next time the backgrounds go black, **open the Overview**. If they come back,
    the cause is the upstream clip bug (`mutter#4767`) and image size is
    irrelevant — scaling would buy nothing and 5.2 should be dropped. If they stay
    black, it is the GC-race or the sticky NULL-texture variant, and only then
    does shrinking the decode window have a point. Costs nothing, needs no code,
    and no mechanism should be chosen before it has been run.
  - **Frequency caveat, load-bearing**: the ~350 ms decode is paid roughly once
    per login, *not* per dock cycle — so this is not an ongoing performance cost
    and must not be justified as one. Its only value is shrinking the GC-race
    window. How often that race is actually lost here is **unmeasured**.
  - [ ] ⬜ **Then decide the mechanism**, if the discriminator did not cancel it:
    1. Event-driven — systemd user service watching `picture-uri`, rescaling on
       change. The only option that survives the user changing wallpaper; needs
       loop-protection against reacting to its own write. Phase 4's panel could
       own it.
    2. One-shot user tool in `~/.local/bin` — "scale this and set it". Honest and
       tiny, but manual and easy to forget.
    3. Drop it — see Task 5.3 on what scaling can and cannot fix.
  - [ ] ⬜ Only then: implement, QA, deploy on HOST, verify
- [ ] ⬜ **Task 5.3**: Decide whether per-monitor pre-scaled caching is worth it
  - [x] ✅ Gated on 5.1, now answered: a per-monitor cache does **not** buy what
    it appeared to. There is no per-monitor decode to eliminate and no ~520 MiB
    to reclaim — only decode latency, which one correctly-sized image already
    captures.
  - [x] ✅ "Surely somebody has built this already" — **GNOME has**, shipped and
    unused: a background `.xml` with multiple `<size>` entries. No third-party
    tool does it; they all composite one spanned image. Detail and the two
    adoption caveats are in [RESEARCH-wallpaper-and-backgrounds.md](RESEARCH-wallpaper-and-backgrounds.md).
  - [ ] ⬜ **Decision deferred.** Only worth it if 5.2 alone does not stop the
    black backgrounds recurring — and it costs the shared-background fast path.
    Re-evaluate after 5.2 ships and survives some dock cycles.

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
