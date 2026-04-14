# Plan 031: Reliable Screen Sharing for Fedora Wayland WFH Devs

**Status**: In Progress — Phase 2 complete, awaiting user go-ahead for Phase 3
**Created**: 2026-04-14
**Owner**: User
**Priority**: High — MAJOR friction blocking WFH collaboration
**Type**: Research → Evaluation → Deployment

## Overview

Screen sharing on Fedora Wayland is currently unreliable for the WFH dev team:

- **Slack desktop screen sharing is fully disabled** (Slack hardcodes the
  PipeWire capturer off in `app.asar` — see Plan 028 for root cause).
- **Google Meet starts sharing then freezes within seconds** (matches a known
  mutter ScreenCast bug — also covered in Plan 028).
- This is causing **major friction** for collaboration — pairing, demos,
  debugging, code reviews — all blocked or painful.

Plan 028 takes the "fix the broken tool" route (upgrade mutter, install
Chromium, use Slack PWA). This plan takes the **complementary** route:
**find one or more reliable alternatives** so the team has a screen-sharing
solution that *works today* and is robust to upstream regressions.

The goal is not to replace Slack (it stays for chat). It's to give devs a
**reliable screen-sharing channel** they can use during pairing/debug/demo
sessions, even when the chat tool's video stack is broken.

This plan starts with parallel research across four very different solution
spaces, then converges on a short list to actually test on the team's
Fedora 43 GNOME Wayland machines.

## Goals

- Find at least 2 candidate solutions that demonstrably work on Fedora 43
  GNOME Wayland in April 2026 — without per-update patching or fragile flags.
- Solutions must support **screen sharing with audio** (or have a clean
  workaround for audio).
- Solutions must work over the open internet (WFH devs are on residential
  ISPs, behind NAT, no shared LAN).
- At least one candidate should be **self-hostable** so the team isn't
  dependent on a SaaS provider's pricing/availability.
- At least one candidate should be **zero-friction for ad-hoc use** (start a
  share in under 30 seconds, no logins for the viewer).
- Document a tested deployment path (Ansible playbook + brief usage doc) for
  the chosen solution(s).

## Non-Goals

- Replacing Slack as the team's chat platform.
- Fixing Slack desktop's screen sharing (covered by Plan 028 if anyone wants
  to pursue it).
- Fixing Google Meet specifically (also Plan 028).
- Building a custom screen-sharing tool from scratch.
- Solving low-latency game streaming for entertainment (latency target here
  is "good enough for pair programming", ~200ms is fine).
- Solving multi-party (>4 participants) video conferencing (focus is 1:1 and
  small-group dev collaboration).

## Context & Background

**Environment** (from repo + Plan 028):

- Fedora 43 GNOME Wayland, no X11 fallback available (`WaylandOnlyGNOME`).
- Two laptops in scope: primary laptop (`laptop-a`) and secondary laptop (`laptop-b`).
- PipeWire + WirePlumber for audio/video, `xdg-desktop-portal-{gnome,gtk}`
  installed.
- Firefox is the default browser (RPM, managed `policies.json`).
- No Chrome/Chromium currently installed by Ansible.
- RPM Fusion + multimedia codecs in place.
- NVIDIA driver is opt-in, not confirmed active.
- Devs are WFH — solutions must traverse NAT, work over residential
  internet, and not require shared LAN.

**Why "think outside the box" matters**: The default answers (Slack, Meet,
Zoom, Teams) all have known issues on Fedora Wayland in 2026. The team needs
solutions that are *designed for Linux* or that *avoid the Wayland portal
stack entirely* (e.g. browser-only, hardware capture, peer-to-peer).

**Anchor in Plan 028**: Plan 028 has the deep root-cause analysis of *why*
Slack and Meet are broken. This plan should not duplicate that — it should
build on it and explore *what else exists*.

## Research Tracks (Parallel)

Four independent research agents are dispatched in parallel, each writing a
self-contained markdown report into this plan's folder. They explore
complementary solution spaces so we get coverage without duplication.

| #   | File                                  | Track                                                                                                                                                   |
| --- | ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `research-1-self-hosted-platforms.md` | Self-hosted real-time collaboration platforms (Jitsi, BigBlueButton, Nextcloud Talk, Element Call, Mediasoup)                                           |
| 2   | `research-2-native-linux-tools.md`    | Native Linux peer-to-peer & remote-desktop tools (RustDesk, Sunshine/Moonlight, wayvnc + NoVNC, waypipe, raw PipeWire pipelines)                        |
| 3   | `research-3-saas-current-state.md`    | Current SaaS landscape — what *actually works* on Fedora Wayland in April 2026 (Discord, Zoom, Teams, Whereby, Around, Tuple, etc.)                     |
| 4   | `research-4-unconventional.md`        | Unconventional approaches — hardware capture, RTMP/HLS broadcast, code-collab tools (Live Share, Code With Me, tmate), tailscale-based, document camera |

Each agent will:

- Test feasibility specifically on **Fedora 43 GNOME Wayland**.
- Identify candidates that genuinely work (not just claim to).
- Note install complexity, audio support, NAT traversal, and dependencies.
- Compare candidates within their track on a small matrix.
- Recommend top 1–2 candidates from their track for live testing.

## Tasks

### Phase 1: Parallel Research

- [x] ✅ **Task 1.1**: Research track 1 — self-hosted platforms
  - Output: `research-1-self-hosted-platforms.md` (~2,800 words, 40+ sources)
  - Top picks: Galène (non-Electron native client), Jitsi (diagnostic)
- [x] ✅ **Task 1.2**: Research track 2 — native Linux tools
  - Output: `research-2-native-linux-tools.md` (~2,500 words, 26 sources)
  - Top picks: Sunshine + Moonlight (KMS bypass), gnome-remote-desktop (diagnostic)
- [x] ✅ **Task 1.3**: Research track 3 — SaaS current state
  - Output: `research-3-saas-current-state.md` (~2,200 words, 16 sources)
  - Top picks: Discord native RPM (works on Wayland since v0.0.76), Whereby
- [x] ✅ **Task 1.4**: Research track 4 — unconventional approaches
  - Output: `research-4-unconventional.md` (~2,600 words, 30 sources)
  - Top picks: OBS + v4l2loopback virtual webcam, VS Code Live Share + Tailscale

### Phase 2: Synthesis & Shortlist

- [x] ✅ **Task 2.1**: Read all four research reports.
- [x] ✅ **Task 2.2**: Build a unified comparison matrix in `shortlist.md`.
- [x] ✅ **Task 2.3**: Pick candidates for live testing (see Decision 2 below).
- [x] ✅ **Task 2.4**: Add Decision entries documenting the shortlist and rationale.

### Phase 3: Live Testing

- [ ] ⬜ **Task 3.1**: For each shortlisted candidate, write a **test
  protocol**: install steps, what to share, what to watch for, what
  "passes" looks like.
- [ ] ⬜ **Task 3.2**: Test on the primary laptop first, document results.
- [ ] ⬜ **Task 3.3**: Test against a second machine (the other laptop or
  a colleague) to validate cross-machine real-world use.
- [ ] ⬜ **Task 3.4**: Run a 30+ minute pair-programming session using the
  top candidate to surface real-world issues (audio drift, framerate drop,
  reconnect behaviour).

### Phase 4: Deployment & Documentation

- [ ] ⬜ **Task 4.1**: Write an Ansible playbook for the chosen tool(s) at
  `playbooks/imports/optional/common/play-screen-share.yml`.
- [ ] ⬜ **Task 4.2**: Write a brief usage doc at `docs/screen-sharing.md`
  covering how a dev starts/joins a session.
- [ ] ⬜ **Task 4.3**: If self-hosted: document the server-side deployment
  (separate playbook or external infra notes).
- [ ] ⬜ **Task 4.4**: Roll out to both laptops; run a real cross-machine
  collaboration session as final acceptance.

## Dependencies

- **Related**: Plan 028 (root-cause fixes for Slack/Meet) — complementary
  but independent.
- **Related**: `playbooks/imports/play-comms.yml` (where chat tools live).

## Technical Decisions

### Decision 1: Why a parallel-research approach instead of jumping to a tool

**Context**: The default temptation is "just use Zoom" or "just self-host Jitsi". But the user explicitly asked to "think outside the box" — meaning don't anchor on the obvious answers, and don't pick before exploring.
**Decision**: Spend research budget upfront across four very different solution spaces in parallel. The cost of an extra hour of research is far less than the cost of deploying a tool that turns out to fail the same way Slack/Meet do.
**Date**: 2026-04-14

### Decision 2: Phase 3 test ladder — three candidates + one diagnostic

**Context**: The four research tracks converged on three different *bypass classes* — bypass the portal (Sunshine = KMS capture), bypass the browser (Discord native, Galène native client), bypass the screen-share path entirely (OBS → virtual webcam). Each bypass class costs differently to set up.
**Decision**: Run a four-step test ladder, cheapest first:
0\. **gnome-remote-desktop** as a 10-minute diagnostic — tells us whether the bug is in Mutter (every browser-based candidate is doomed) or in Chromium (browser candidates open back up).

1. **Discord native RPM** — daily-driver candidate. Wayland-stable since v0.0.76 (Jan 2025), free, 5-minute install.
2. **OBS + v4l2loopback virtual webcam** — universal solve. Once it works, every meeting tool the team is forced to use becomes screen-share-capable via the unblockable webcam path.
3. **Sunshine + Moonlight + Tailscale** — escape hatch. Only KMS-bypass option; structurally cannot be affected by the portal bug.
   Stop at the first tier that fully solves the problem, but always run Test 0 because its diagnostic value also benefits Plan 028.
   **Date**: 2026-04-14
   **Detail**: see [shortlist.md](shortlist.md)

### Decision 3: Tools explicitly ruled out (do not test)

**Context**: Several oft-recommended tools turn out to be dead, Linux-hostile, or doomed by the same bug class.
**Decision**: Do not waste cycles on: Around (shut down March 2025), CoScreen, Pop, Tuple (no/beta Linux client), JetBrains Code With Me (sunset March 2026), Teams native (dead Dec 2022), Parsec (X11-only), wayvnc (wlroots-only, doesn't run on GNOME), waypipe (per-app, wrong problem), BigBlueButton (install complexity + 2025 regressions), Element Call (5-service stack), Zoom native desktop (same bug class as Meet), RustDesk (open Wayland issues + portal-path).
**Date**: 2026-04-14

## Success Criteria

- [ ] At least 2 candidate solutions pass live testing on Fedora 43 Wayland.
- [ ] Chosen solution(s) deployable via Ansible.
- [ ] A real 30+ minute pair-programming session completed successfully.
- [ ] Usage documented so any dev on the team can start a share in \<30s.
- [ ] Solution works WFH (over public internet, behind NAT, no shared LAN).

## Risks & Mitigations

| Risk                                                              | Impact | Probability | Mitigation                                                                              |
| ----------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------- |
| All candidates have the same Wayland portal issue as Slack/Meet   | High   | Medium      | Track 4 explicitly explores tools that bypass the portal stack (hardware capture, RTMP) |
| Self-hosted option requires server infra the team doesn't have    | Medium | Medium      | Track 3 covers SaaS fallbacks; Track 2 covers peer-to-peer that needs no central server |
| Chosen tool fits 1:1 but breaks for small-group sessions          | Low    | Medium      | Test protocol explicitly covers a 3-person session                                      |
| Audio support is broken even when video works (common Linux trap) | High   | Medium      | Each track must report audio support explicitly; not optional                           |
| Tool depends on a Chromium browser the team doesn't have          | Low    | Medium      | Plan 028 already adds Chrome/Chromium in its Phase 3 — coordinate                       |

## Notes & Updates

### 2026-04-14

- Plan created. Four parallel research agents dispatched. Each will write
  a report into this folder.
- Anchored against Plan 028: this plan looks for *alternatives*, not fixes.
- Phase 1 complete: all four research reports landed (~10k words total,
  100+ cited sources combined).
- Phase 2 complete: synthesis written to [shortlist.md](shortlist.md).
- Three converging insights across all four tracks:
  1. The bug is in the GNOME/Mutter portal stack — every browser-based tool
     using `getDisplayMedia()` hits the same freeze class.
  2. Working solutions are those that *physically can't* hit the broken
     portal: bypass-the-portal (Sunshine KMS), bypass-the-browser (Discord
     native, Galène native client), or bypass-the-screen-share-path (OBS
     virtual webcam → meeting tool's webcam picker).
  3. Cross-cutting rules: native RPM > Flatpak; Firefox > Chromium for
     in-browser screenshare on F43.
- Phase 3 test ladder defined in Decision 2: gnome-remote-desktop diagnostic
  → Discord RPM → OBS+v4l2loopback → Sunshine+Moonlight+Tailscale.
- **Awaiting user go-ahead** to begin live testing on the primary laptop.
