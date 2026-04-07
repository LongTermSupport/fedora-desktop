## Plan 028: Fedora 43 GNOME Screen Sharing — Diagnose & Fix Slack/Meet

**Status**: Not Started
**Created**: 2026-04-07
**Owner**: User
**Priority**: High
**Type**: Bug Fix / Investigation

## Overview

Slack desktop screen sharing does not work at all on Fedora 43 GNOME, and Google
Meet screen sharing in the browser works briefly then freezes. The user needs a
reliable way to share their screen for collaboration. This plan applies the
fix order researched in `research.md`, working from cheapest to most invasive.

The deep web research (see `research.md`, ~40 cited sources) identified two
distinct root causes:

1. **Slack desktop is broken by design.** Slack's bundled Electron build
   hard-disables Chromium's `WebRTCPipeWireCapturer` feature inside `app.asar`,
   so the legacy `--enable-features=WebRTCPipeWireCapturer` workaround that
   most blog posts still recommend has been a no-op since Slack ~4.30. This
   affects both the RPM and the Flatpak. The reliable fix is to abandon the
   Slack desktop client and use Slack as a Chrome/Chromium PWA, which uses the
   working in-browser PipeWire path.

2. **The Meet "freeze after a few seconds" symptom matches a known mutter
   ScreenCast bug** fixed upstream in mutter 49.3 ("Fix reporing damage region
   in pipewire streams") and mutter 49.4 / backported to 49.5
   ("screen-cast-stream-src: Only specify framerate range if there is any").
   Fedora 43 shipped GA on `mutter-49.1.1` (October 2025) and has since
   updated to `mutter-49.3-1.fc43` (early February 2026) and
   `mutter-49.5-1.fc43` (mid-March 2026). If the user has not pulled
   updates recently, a `dnf upgrade --refresh` may resolve Meet for free.

**Critical constraint:** Fedora 43 has *no* GNOME-on-X11 fallback. The
`WaylandOnlyGNOME` change removed `gnome-session-xsession` from F43 repos
entirely. There is no "just switch to X11" escape hatch — any X11 fallback
requires a different desktop environment (KDE/Cinnamon/MATE).

See [research.md](research.md) for the full investigation, evidence, sources,
and tool comparison table.

## Goals

- Determine the user's exact mutter version and GPU vendor (the two unknowns
  that decide the fix path).
- Make Google Meet screen sharing stable for full-length meetings without
  freezing.
- Make Slack screen sharing work reliably without requiring per-update binary
  patching of `app.asar`.
- Capture the working configuration in Ansible playbooks so both `joseph-x1`
  and `joseph-p14` get the fix and survive reinstalls.
- Update the Fedora installation playbooks so future F43 installs do not hit
  these problems.

## Non-Goals

- Switching desktop environment away from GNOME (KDE/Cinnamon/MATE are
  out of scope unless every cheaper fix fails).
- Patching `app.asar` on every Slack update (fragile; documented as a
  last-resort workaround only).
- Replacing the team's chat platform (Slack stays — only the *client* changes
  if needed).
- Building a custom screen-sharing tool or self-hosted meeting server.
- Fixing Wayland screen sharing for unrelated apps (Zoom, Teams, Discord) —
  documented in research.md but not in scope unless the user requests them.

## Context & Background

**Repo facts** (from local exploration):

- Target OS: Fedora 43 (`vars/fedora-version.yml`).
- Hosts: `joseph-x1` and `joseph-p14` (two laptops, host_vars in
  `environment/localhost/host_vars/localhost.yml:63-67`).
- Slack is installed as a **Flatpak** (`com.slack.Slack`) via
  `playbooks/imports/play-comms.yml:1-17`. Version is not pinned.
- **Firefox** is installed as a native RPM via
  `playbooks/imports/play-firefox.yml:1-31` with managed `policies.json`.
- **Chrome / Chromium is NOT installed** by any current playbook. This is
  the biggest gap — the recommended Slack-PWA fix needs a Chromium-family
  browser.
- `xdg-desktop-portal-gtk` is installed by
  `playbooks/imports/optional/common/play-fast-file-manager.yml:62-93`.
  `xdg-desktop-portal-gnome` is pulled in by GNOME shell. The portal stack is
  in place.
- PipeWire/WirePlumber are heavily configured for HD audio
  (`playbooks/imports/optional/common/play-hd-audio.yml`) but no
  screen-cast-specific tuning exists.
- RPM Fusion + multimedia codecs + `intel-media-driver` are installed by
  `playbooks/imports/play-rpm-fusion.yml`.
- NVIDIA driver is **optional** (`playbooks/imports/optional/hardware-specific/play-nvidia.yml`)
  and not enabled by default — important to confirm whether the user is
  actually running it on either machine.
- No prior plans touch screen sharing, Wayland video, or PipeWire screen-cast.

**Open questions that gate the fix path** — answer these in Phase 1:

1. Which GPU vendor on each machine? (Intel iGPU / AMD / NVIDIA)
2. Is the proprietary NVIDIA driver actually loaded on either laptop?
3. What `mutter` version is currently installed? (`rpm -q mutter`)
4. When was the last `dnf upgrade`?
5. Has the user already tried `--enable-features=WebRTCPipeWireCapturer` for
   Slack? (If yes, that confirms the hardcoded-disable theory.)
6. Is the broken Slack the Flatpak (which is what the playbook installs) or a
   manually-installed RPM?

## Tasks

### Phase 1: Diagnose — gather facts before changing anything

- [ ] ⬜ **Task 1.1**: Capture current system state (run on whichever laptop
  is most affected, then the other)
  - [ ] ⬜ `rpm -q mutter gnome-shell xdg-desktop-portal xdg-desktop-portal-gnome xdg-desktop-portal-gtk pipewire wireplumber`
  - [ ] ⬜ `flatpak info com.slack.Slack | grep -E 'Version|Branch|Commit'`
  - [ ] ⬜ `flatpak list --columns=application,version | grep -i slack`
  - [ ] ⬜ `glxinfo -B | grep -E 'OpenGL renderer|vendor'` (GPU)
  - [ ] ⬜ `vainfo 2>&1 | head -30` (VA-API status)
  - [ ] ⬜ `lsmod | grep -E 'nvidia|nouveau|i915|amdgpu'` (which driver loaded)
  - [ ] ⬜ `echo $XDG_SESSION_TYPE` (confirm wayland)
  - [ ] ⬜ `dnf history list | head -5` (when was last upgrade)
  - [ ] ⬜ Save outputs to `/tmp/screen-share-diag-$(hostname).txt` and paste
        results into the plan's Notes & Updates
- [ ] ⬜ **Task 1.2**: Decide Phase 2 entry conditions based on Task 1.1
  - [ ] ⬜ If `mutter < 49.3-1.fc43` → Phase 2.1 (upgrade) is the prime
        suspect for Meet
  - [ ] ⬜ If NVIDIA proprietary driver loaded → flag the higher risk and
        consider disabling for the test (NVIDIA + Wayland screen-cast is
        still flaky as of late 2025 per research.md §8.3)
  - [ ] ⬜ If Slack version is < 4.30 → confirm whether `--enable-features`
        flag still works (unlikely, but cheap to check)

### Phase 2: Cheapest fix — system upgrade for the Meet freeze

- [ ] ⬜ **Task 2.1**: Pull all pending updates
  - [ ] ⬜ `sudo dnf upgrade --refresh` on the affected machine
  - [ ] ⬜ `flatpak update -y` (covers any Slack flatpak update)
  - [ ] ⬜ Reboot (mutter and gnome-shell upgrades require a full session
        restart, not just a logout)
- [ ] ⬜ **Task 2.2**: Re-test Google Meet
  - [ ] ⬜ Open `meet.google.com` in Firefox, start a test meeting alone
  - [ ] ⬜ Share the screen, leave it running for 10+ minutes, watch a video
        in another window so frames are actually changing
  - [ ] ⬜ Confirm whether the freeze still happens
  - [ ] ⬜ If still frozen: capture `journalctl --user -b -u pipewire -u wireplumber` and `journalctl -b _COMM=mutter`
        to a file in `/tmp/` and add to plan notes

### Phase 3: Add a Chromium-family browser, install Slack as PWA

This is the main fix for Slack. It also gives a fallback browser for Meet if
Firefox keeps misbehaving.

- [ ] ⬜ **Task 3.1**: Choose the Chromium-family browser
  - [ ] ⬜ Decide: Google Chrome (RPM via Google's repo) vs Chromium
        (Fedora RPM) vs ungoogled-chromium (Flatpak). research.md §3.3
        recommends **Google Chrome RPM** as the fastest path — it has the
        WebRTC PipeWire path enabled by default since Chrome 110 and is the
        most-tested combo.
  - [ ] ⬜ Document the choice in this plan with a Decision entry
- [ ] ⬜ **Task 3.2**: Create `playbooks/imports/optional/common/play-chrome.yml`
  - [ ] ⬜ Add the Google Chrome dnf repo (same idiom as RPM Fusion)
  - [ ] ⬜ Install `google-chrome-stable`
  - [ ] ⬜ Mark this playbook as opt-in via `playbook-main.yml` (do not
        deploy to all machines unless the user wants it)
- [ ] ⬜ **Task 3.3**: Run the playbook on the affected laptop and install
  Slack as a PWA
  - [ ] ⬜ `ansible-playbook playbooks/imports/optional/common/play-chrome.yml`
  - [ ] ⬜ Open Chrome → `https://app.slack.com` → log in → ⋮ menu →
        "Install Slack…" (creates a desktop launcher)
  - [ ] ⬜ Test screen sharing in a real Slack call
  - [ ] ⬜ Note: PWA does NOT replace the Flatpak Slack icon — keep both
        installed during the trial period
- [ ] ⬜ **Task 3.4**: Once PWA is confirmed working, optionally remove the
  Flatpak
  - [ ] ⬜ Comment out the `com.slack.Slack` install in
        `playbooks/imports/play-comms.yml` (do NOT delete the line —
        leave it commented with a reference to this plan)
  - [ ] ⬜ Run the playbook to remove the Flatpak
  - [ ] ⬜ Verify `flatpak list | grep -i slack` returns nothing

### Phase 4: If Phase 2 didn't fix Meet — hardware acceleration tweaks

Only run this phase if Meet is still freezing after Phase 2, and Slack PWA
*also* has freezes in Chrome (which would point at Mesa/VA-API rather than
Slack).

- [ ] ⬜ **Task 4.1**: Disable Chrome hardware acceleration
  - [ ] ⬜ `chrome://settings/system` → "Use graphics acceleration when
        available" → off → relaunch Chrome
  - [ ] ⬜ Re-test Meet and Slack PWA screen sharing
  - [ ] ⬜ If this fixes it: persist via Chrome enterprise policy in
        `/etc/opt/chrome/policies/managed/`
- [ ] ⬜ **Task 4.2**: Confirm Mesa version is current
  - [ ] ⬜ `rpm -q mesa-libGL mesa-vulkan-drivers mesa-va-drivers`
  - [ ] ⬜ Cross-reference against the Fedora Discussion threads cited in
        research.md §2.1 — if the user is on a known-bad Mesa version,
        wait for the next update rather than downgrading
- [ ] ⬜ **Task 4.3** (only if Task 4.1 didn't help): Test Firefox with
  `MOZ_ENABLE_WAYLAND=1` explicitly set and `media.webrtc.camera.allow-pipewire`
  in `about:config`
  - [ ] ⬜ Capture results

### Phase 5: Survival fixes — last resort options

Only invoke these if Phases 2–4 fail. Document each one in
research.md before applying.

- [ ] ⬜ **Task 5.1**: As a temporary bridge while Phases 2–4 are tested,
  evaluate switching team meetings to a more Linux-friendly tool for
  collaboration sessions (not chat — chat stays on Slack)
  - [ ] ⬜ Try **Jitsi Meet** at `meet.jit.si` in Firefox (works without flags
        per research.md §6)
  - [ ] ⬜ Try **Whereby** in Firefox
  - [ ] ⬜ Note: this is an interim measure, not a tool migration
- [ ] ⬜ **Task 5.2**: Slack `app.asar` patch (LAST RESORT only — fragile)
  - [ ] ⬜ Read research.md §3.2 for the exact patch and risks
  - [ ] ⬜ Do NOT apply unless user explicitly accepts the maintenance burden
        of re-patching after every Slack auto-update
  - [ ] ⬜ If applied: wrap in an Ansible handler that reapplies after
        upgrades AND a systemd path unit watching `~/.var/app/com.slack.Slack`
- [ ] ⬜ **Task 5.3**: OBS + virtual webcam workaround
  - [ ] ⬜ Document in research.md whether OBS Studio + the v4l2loopback
        kernel module can produce a "screen as webcam" stream that the
        Slack Flatpak's webcam-share path can consume
  - [ ] ⬜ Only useful if Phases 3 and 5.2 are both rejected

### Phase 6: Codify the fix into Ansible and document

- [ ] ⬜ **Task 6.1**: Make the working configuration reproducible
  - [ ] ⬜ Ensure `play-chrome.yml` (or whatever Phase 3.2 produced) is in
        `playbooks/imports/optional/common/`
  - [ ] ⬜ Add a brief README at the top of that file pointing at this plan
        ("see CLAUDE/Plan/028-fedora-screen-sharing/")
  - [ ] ⬜ Add the playbook to `playbook-main.yml` if and only if both
        machines are confirmed to need it
- [ ] ⬜ **Task 6.2**: Run the playbook on the *other* laptop (the one not
  used for the main fix) to validate idempotency and that the fix carries
  across machines
- [ ] ⬜ **Task 6.3**: Update plan status, mark complete, and add a
  "lessons learned" section to the plan notes

## Dependencies

- **Depends on**: nothing (this is a self-contained bug fix)
- **Blocks**: nothing
- **Related**: `playbooks/imports/play-comms.yml` (where Slack is installed),
  `playbooks/imports/play-firefox.yml` (current browser),
  `playbooks/imports/play-rpm-fusion.yml` (multimedia codecs already in place)

## Technical Decisions

### Decision 1: Browser choice for the Slack PWA fix

**Context**: The Slack PWA fix needs a Chromium-family browser. Three options
exist on Fedora 43.

**Options**:

1. **Google Chrome RPM** (from Google's signed dnf repo). Most-tested combo
   for WebRTC PipeWire. Closed-source, telemetry. Stable update channel.
2. **Fedora `chromium` RPM**. Open-source. Has had VA-API regressions on
   Mesa 25.x (research.md §2.1) but those affect *playback*, not screen
   capture, so likely irrelevant here.
3. **Flatpak `org.chromium.Chromium`**. Sandboxed, but the Flatpak version
   has historically had portal-handling quirks similar to Slack.

**Decision**: TBD — defer until Phase 3.1. Default lean: **Google Chrome RPM**
for lowest risk; revisit if the user objects to the privacy/closed-source
trade-off.

**Date**: 2026-04-07

### Decision 2: Whether to remove the Slack Flatpak after the PWA works

**Context**: Once the PWA is working, the Flatpak becomes redundant. But
removing it means no fallback if the PWA breaks.

**Options**:

1. Remove immediately (clean, less to maintain).
2. Keep both installed for a trial period, then remove.
3. Keep both indefinitely (the Flatpak is harmless if unused).

**Decision**: **Option 2** — keep the Flatpak for a 2-week trial then
re-evaluate. The Flatpak update channel is also a useful signal for whether
upstream Slack ever fixes their `app.asar` hardcode.

**Date**: 2026-04-07

### Decision 3: Whether to switch DE if everything else fails

**Context**: The "switch to KDE Plasma" nuclear option is the only way to
get an X11 session on F43, and X11 sidesteps the entire Wayland/PipeWire
screen-cast stack.

**Decision**: **Out of scope** for this plan. If Phases 2–5 all fail, that
becomes a separate plan with much wider implications (extension migration,
GNOME-specific tooling like dconf-editor, etc.). Document the failure path
in this plan's Notes & Updates and open a follow-up plan if reached.

**Date**: 2026-04-07

## Success Criteria

- [ ] Google Meet screen sharing runs for at least 30 minutes continuously
      with active screen content (e.g. a video playing) without freezing.
- [ ] Slack screen sharing works reliably from at least one launcher (PWA
      or Flatpak), confirmed in a real call with another participant.
- [ ] Both `joseph-x1` and `joseph-p14` have the fix applied and tested.
- [ ] The fix is captured in an Ansible playbook so a fresh F43 install
      gets it automatically (or via a documented optional playbook).
- [ ] research.md and PLAN.md updated with the actual root cause that
      applied to *this* user (which of the two suspects in §1) and the
      actual working fix.

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Phase 2 upgrade introduces *other* regressions (mutter is on the critical path for the entire desktop) | High | Low | Take a btrfs snapshot or note current `dnf history` ID before upgrading; rollback procedure documented in Phase 2 |
| User has NVIDIA proprietary driver, which has its own screen-cast issues that none of these fixes address | Medium | Unknown until Phase 1 | Phase 1 explicitly checks for this; if found, escalate to research mode before continuing |
| Google Chrome RPM repo introduces an unwanted update channel | Low | Low | Pin to stable, document removal procedure |
| Slack PWA UX is meaningfully worse than the desktop client (notifications, file uploads, deep links) | Medium | Medium | The 2-week trial in Decision 2 catches this; user can revert to Flatpak with one playbook run |
| Mutter 49.5 fixes don't actually resolve the Meet freeze (different bug) | Medium | Medium | Phase 4 has alternate diagnostics; research.md documents three independent root causes for Meet freezes |
| Patching `app.asar` (Phase 5.2) breaks Slack on next auto-update with no warning | High | High | Phase 5.2 is gated as last-resort and requires explicit user opt-in to the maintenance burden |

## Timeline

- Phase 1: Diagnostics (must complete before any fix)
- Phase 2: System upgrade (the cheapest test; may close the entire plan)
- Phase 3: Chrome + Slack PWA (the main Slack fix)
- Phase 4: Hardware-accel tweaks (only if Phase 2 didn't fix Meet)
- Phase 5: Last-resort fixes (only if Phases 2–4 fail)
- Phase 6: Ansible-ize and validate on second laptop

## Notes & Updates

### 2026-04-07
- Plan created from deep web research (~25 web searches, ~15 page fetches,
  ~40 cited sources). Full report at [research.md](research.md).
- Two distinct root causes identified: Slack hardcodes `WebRTCPipeWireCapturer`
  disable in `app.asar`; Meet freeze matches a known mutter ScreenCast bug
  fixed in 49.3 + 49.5.
- Critical constraint: Fedora 43 has no GNOME-on-X11 fallback (WaylandOnlyGNOME
  change removed `gnome-session-xsession` entirely).
- Repo audit: Slack is currently the Flatpak (`com.slack.Slack` in
  `play-comms.yml`); no Chrome/Chromium installed; portal stack already in
  place; NVIDIA playbook exists but is opt-in (need to confirm whether
  active on either of the user's two laptops).
- **Awaiting**: user confirmation of GPU vendor on both laptops, current
  mutter version, and last `dnf upgrade` date — these gate the Phase 2
  vs Phase 3 sequencing.
