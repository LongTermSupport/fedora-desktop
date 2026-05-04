# Plan 00038: MusicCast Controller (Now-Playing + Qobuz Browse/Search/Play)

**Status**: Awaiting decision-gate review (research complete, see [DECISION.md](DECISION.md))
**Created**: 2026-05-02
**Owner**: joseph
**Priority**: Medium
**Type**: Feature Research → Decision Gate → Implementation
**Estimated Effort**: TBD post-research

## Decision Gate

→ **[DECISION.md](DECISION.md)** ← read this before reviewing the research files.

The decision gate recommends **Path B (Python + Textual + `aiomusiccast`)** with a parallel low-cost license experiment on KsanStone/MusicCastDesktop as Path A.

## Overview

Build (or adopt) a MusicCast controller for Linux that gives full UX:

- **See what's playing** — current track / artist / album / artwork on the receiver
- **Browse Qobuz** — primary streaming target; navigate genres, playlists, albums
- **Search** — Qobuz search-as-you-type
- **Select & play** — push a track or album to the receiver and start playback
- **Now-playing transport controls** — play/pause/skip/volume/source

The controller runs on a Fedora desktop. Two MusicCast receivers are confirmed
on the LAN at `192.0.2.44` and `192.0.2.74` (UPnP MediaRenderers; YXC API
endpoint to be confirmed via Research Track A).

This plan supersedes the lightweight `play-musiccast.yml` (which now only
installs SSDP diagnostics + an opt-in stale gyrc GUI). ymc was tried and
removed — upstream dormant since 2023, panics on empty speaker list.

## Goals

- Decide whether to **build** a controller, **adopt** an existing one, or
  **wrap** an existing platform (Home Assistant) — backed by evidence from
  parallel research tracks, not gut feel.
- If building: pick a stack (Go / Rust / Python; TUI vs GUI vs web).
- If adopting: pick the project, fork if needed, plan the deployment.
- Land the decision in a Decision Gate before any implementation work.

## Non-Goals

- Multi-user / multi-tenant control. Single-user desktop.
- Mobile-app parity with Yamaha's official MusicCast app.
- Spotify / Tidal / Apple Music integration. Qobuz only — those are
  separate streaming-service authentication problems.
- Hardware control beyond the YXC API surface (no IR, no RS-232).

## Context & Background

### Failed approaches (already explored)

- **ymc** (Go TUI, atamanroman/ymc) — panics on first input when the
  speaker list is empty. Upstream dormant since May 2023, 0 issues filed.
  Not viable.
- **gyrc** (GTK GUI, balinbob/yrc) — GUI-only, dormant since 2020,
  depends on stale `pymusiccast`. Available as opt-in via the existing
  playbook but not a primary candidate for the rich UX we want.
- **MCcontrol** (FrankEberle/MCcontrol) — turns out to be a CGI script
  for OpenWRT routers (JS/HTML/Haserl). Wrong fit for a Fedora desktop.

### Working ground truth

- LAN interface: `wlp0s20f3` (192.0.2.58/24).
- gssdp-discover finds MediaRenderers when given `-i wlp0s20f3`.
- gupnp-tools is installed via play-musiccast.yml.
- Qobuz CLI tools are already installed via `play-qobuz-cli.yml` —
  `qobuz-player` (web UI on :9888) and `hifi-rs`. **Significant** because
  qobuz-player potentially exposes a DLNA / MPRIS / web API that could
  be bridged to MusicCast.

### Key architectural unknowns (RESOLVED via research)

1. **Does MusicCast natively browse/search/play Qobuz on these receivers?**
   ✅ **Yes** — confirmed live by Track A. The WXA-50 (192.0.2.74) was
   actively playing Qobuz during probes (`netusb/getPlayInfo` returned
   `input: "qobuz"` with full metadata). Qobuz is a first-class `netusb`
   input on both receivers (the May 2019 Yamaha-Qobuz firmware wave covered
   the RX-A 80 and WXA series). Browse via `netusb/getListInfo`, search via
   POST `netusb/setSearchString`, transport + presets all work.
2. **DLNA fallback (qobuz-player → DLNA → MusicCast)?** Not needed —
   Path 1 confirmed. Track D ranked DLNA as the fallback if native Qobuz
   failed; it didn't.
3. **Wrap Home Assistant?** Track C showed HA wraps `aiomusiccast` but
   doesn't surface receiver-side search. The library itself is the better
   dependency than HA. Path discounted.
4. **YXC library maintenance state?** `vigonotion/aiomusiccast` is mature,
   MIT, used by HA, and covers everything we need including `setSearchString`
   that HA hides. `pymusiccast` is dormant; `aiomusiccast` is its successor.

## Research Phase — 5 Parallel Tracks

Each track produces one Markdown file in this directory. Repos cloned for
deeper inspection live under `untracked/00038-musiccast-controller/<repo>`
(gitignored).

### Track A — YXC API Surface ✅

- File: [`research-yxc-api.md`](research-yxc-api.md) (510 lines)
- Yamaha YXC spec PDFs saved alongside: `yxc-api-spec-basic.pdf`, `yxc-api-spec-advanced.pdf`
- Headline: full now-playing / browse / search / push surface documented; live data captured from both receivers; Qobuz confirmed as a first-class netusb input on the WXA-50.

### Track B — OSS Controller Landscape ✅

- File: [`research-oss-landscape.md`](research-oss-landscape.md) (192 lines)
- ~80 repos surveyed; 14 cloned to `untracked/00038-musiccast-controller/`
- Headline: only `KsanStone/MusicCastDesktop` (Tauri+Vue+Rust) matches the spec — but has no license. Best library for build-from-scratch is `vigonotion/aiomusiccast`.

### Track C — Home Assistant Integration Deep Dive ✅

- File: [`research-home-assistant.md`](research-home-assistant.md) (334 lines)
- HA `yamaha_musiccast` source + `aiomusiccast` cloned to `untracked/00038-musiccast-controller/`
- Headline: HA wraps `aiomusiccast` but doesn't surface receiver-side search. Wrapping HA buys nothing the library doesn't already give us. Use `aiomusiccast` directly.

### Track D — Qobuz on MusicCast ✅

- File: [`research-qobuz-integration.md`](research-qobuz-integration.md) (220 lines)
- Headline: native Qobuz over YXC is the top path and confirmed working on the WXA-50; DLNA via upmpdcli is the fallback if YXC native ever fails; AirPlay capped at 16/44.1 (dealbreaker for hi-res); Qobuz Connect not on Yamaha hardware as of May 2026.

### Track E — Build-From-Scratch Stack Options ✅

- File: [`research-stack-options.md`](research-stack-options.md) (385 lines)
- Headline: with Qobuz risk eliminated by Track D, the stack rebalances toward Python + Textual + `aiomusiccast` (rather than Rust, which had been pulled up by Qobuz crate availability). All three stack hello-world skeletons remain in the file for reference.

## Decision Gate (after research)

Before any implementation work begins, write `DECISION.md` in this folder
that:

1. Cites evidence from each research track.
2. Picks one of: **(i)** wrap an existing OSS controller, **(ii)** wrap
   Home Assistant via REST, **(iii)** build new on a chosen stack, or
   **(iv)** chain qobuz-player → DLNA → MusicCast and skip a custom UI.
3. Lays out the next-phase task list.

## Tasks

### Phase 1: Research (parallel) ✅

- [x] ✅ **Track A**: YXC API surface → `research-yxc-api.md`
- [x] ✅ **Track B**: OSS landscape → `research-oss-landscape.md`
- [x] ✅ **Track C**: HA integration → `research-home-assistant.md`
- [x] ✅ **Track D**: Qobuz on MusicCast → `research-qobuz-integration.md`
- [x] ✅ **Track E**: Stack options → `research-stack-options.md`

### Phase 2: Decision Gate

- [x] ✅ Synthesise findings into [`DECISION.md`](DECISION.md)
- [ ] 🔄 User reviews decision gate
- [ ] ⬜ Approve path forward (answers to the four open questions in DECISION.md)

### Phase 3: Implementation (TBD post-decision)

Concrete sub-tasks will be added once Phase 2 lands. The DECISION.md "next phase" sketch lists nine candidate steps spanning verification probes, project scaffolding, vertical-slice screens, UDP push wiring, and Ansible packaging.

## Success Criteria

- All 5 research tracks produce a research file with concrete findings.
- DECISION.md cites evidence and picks a path with clear rationale.
- Implementation phase ships a controller satisfying the goals above.

## Notes & Updates

### 2026-05-02

- Plan created. Receivers confirmed at 192.0.2.44 / 192.0.2.74.
- Research agents dispatched in parallel.
- All 5 research tracks complete. **Live confirmation that Qobuz is native and playing on the WXA-50** (Track A captured `input: "qobuz"` mid-playback). Decision gate written; awaiting user review and answers to the four open questions in DECISION.md.
