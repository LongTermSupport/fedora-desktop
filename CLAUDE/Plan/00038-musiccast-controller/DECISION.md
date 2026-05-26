# Plan 00038 — Decision Gate

**Date**: 2026-05-02
**Status**: Awaiting user review
**Inputs**: research-yxc-api.md, research-oss-landscape.md, research-home-assistant.md, research-qobuz-integration.md, research-stack-options.md

## What changed during research (the load-bearing fact)

The single most important finding from all five tracks:

> **Qobuz is native on both your receivers, exposed through YXC's `netusb` endpoints, and was actively playing on the WXA-50 (192.0.2.74) during research probes.**

Agent A captured a live `netusb/getPlayInfo` returning `input: "qobuz"` with track / album / artist / progress / attribute bits including b[18] Add Track (Qobuz) and b[25] Add Playlist (Qobuz). Agent D confirmed both receiver-model lineages received the May 2019 Qobuz firmware. The RX-A3070's preset list already contains Qobuz items.

This eliminates the largest risk that Track E's recommendation (Rust) was hedging against — Qobuz auth reverse-engineering. **We don't write a Qobuz client at all.** The receiver handles Qobuz. We are a controller for the receiver, not a Qobuz player.

## What this collapses

- **No Qobuz Web API client needed.** Browse, search, play, now-playing all run through YXC's generic `netusb` endpoints with `input=qobuz`.
- **No DLNA/UPnP push needed.** Path 2 (upmpdcli + Qobuz plugin) was the fallback for "what if Qobuz isn't native". It is. Skip Path 2.
- **No AirPlay/MPD/Connect work needed.** Same reason.
- **Track E's "Rust because Qobuz crates exist" advantage evaporates.** With no Qobuz half, the stack decision rebalances toward "which stack has the best maintained YXC library + best TUI ergonomics" — and that's Python (aiomusiccast + Textual).

## What we actually need to build / adopt

The minimum surface area:

1. **YXC client**: HTTP polling + UDP push subscription (`X-AppName`/`X-AppPort` headers). Most of the work is done by `vigonotion/aiomusiccast` — actively maintained, MIT, used by Home Assistant, plus it exposes `setSearchString` which HA itself doesn't surface.
2. **UI**: now-playing pane, NetUSB browse tree (Qobuz/server/net_radio), search modal, transport / volume / source controls, multi-zone awareness for the RX-A3070's zone2/zone3.
3. **Packaging**: an Ansible playbook that drops the controller onto the desktop, similar to `play-qobuz-cli.yml`.

## Three viable paths

### Path A — Adopt `KsanStone/MusicCastDesktop` (fork)

- Tauri 2 + Vue 3 + Rust + sibling `yamaha_rs` crate.
- Already implements: SSDP discovery, multi-zone, now-playing card with art + progress, NetUSB list browser with paging + breadcrumbs, search dialog (`set_search_string`), repeat / shuffle / sound program, presets, recent items, signal info, YPAO config.
- Models include `NetUsbQobuz { login_type }` — Qobuz-aware out of the box.
- 0 stars, 0 forks, 0 issues. Solo personal repo. **No license.**
- Estimated 2–3 days to a usable build (mostly: fork, packaging via AppImage/.rpm, "Search Qobuz" entry-point shortcut so search isn't a generic-NetUSB navigation step).

**Risk**: license. Without an explicit license, the code is all-rights-reserved by default. Adopting requires either (a) the author adding an OSI license on request, (b) running it as a personal-use fork without redistribution, or (c) walking away.

**Form factor**: GUI desktop app (Tauri → GTK on Wayland Linux). Not a TUI.

### Path B — Build TUI on `aiomusiccast` + Textual (Python)

- Library: `vigonotion/aiomusiccast` (MIT, actively maintained, ~5k LOC). Already does everything: HTTP YXC client, UDP push receiver, browse, presets, group/distribution, `MusicCastMediaContent` browse helper.
- Bonus: it exposes `pyamaha.NetUSB.set_search_string` which HA's `media_player.browse_media` ignores — we'd be the *only* thing surfacing receiver-side search.
- TUI: Textual 5.x. Has DataTable, ProgressBar, Input, Modal screens, CSS theming, async-first.
- Estimated 1–2 weeks to feature parity with KsanStone. Distribution via venv + launcher script in `~/.local/bin/` (heavier than a single binary, lighter than a full Tauri install).

**Risk**: more weeks than Path A. Less polish. UI work has to actually be done, not inherited.

**Form factor**: TUI. Keyboard-first. Renders over SSH cleanly. Plays better with the rest of this repo's Linux-power-user aesthetic.

### Path C — Build Rust TUI on Ratatui (no aiomusiccast equivalent)

- Write the YXC client ourselves (~300 lines: `reqwest` + `tokio::net::UdpSocket`).
- Ratatui + crossterm + tokio. `spotify-tui` is the proven pattern.
- Estimated 2–3 weeks. Roughly Path B's effort plus writing the YXC client from scratch.

**Risk**: highest effort of the three and gains nothing concrete that Path B doesn't provide — the Rust Qobuz crates aren't relevant any more.

**Form factor**: TUI. Single static binary, smallest of all three.

## Discounted options

- **Wrap Home Assistant via REST/WebSocket** (Track C): partially viable but adds an HA server to maintain and HA's `browse_media` doesn't surface search anyway. We'd still need to call YXC directly for search. No-op shortcut, plus dead weight. Skip.
- **DLNA / upmpdcli + Qobuz plugin** (Track D Path 2): only relevant if YXC native Qobuz fails. It hasn't.
- **AirPlay / MPD-mopidy / Qobuz Connect**: covered in Track D, all ranked below Path 1 (native YXC). All discounted by the live confirmation.
- **Adoption of any other repo from Track B**: the survey of ~80 repos turned up no other viable controller. KsanStone is the only one that meets the spec.

## Recommendation

**Pursue Path B (Python + Textual + `aiomusiccast`)** as the primary path, with Path A (adopt KsanStone) as a **parallel low-cost experiment**.

Reasoning:

1. **Form factor matches your stated direction.** You started with ymc and gyrc — both keyboard-driven. The earlier conversation flagged a TUI as the goal ("see what's playing, browse, search, select, play" via terminal). KsanStone is a GUI; valuable, but not what you originally asked for.
2. **License risk on KsanStone is real.** A fork without an explicit license is technically infringement. Asking the maintainer is cheap and may resolve it; depending on it is not.
3. **`aiomusiccast` is a force multiplier.** It's already 80% of the work, MIT, has tests, is the same library HA uses. Building on it is cheap. Path B's "1–2 weeks to parity" is realistic, not optimistic.
4. **The repo already has the Python toolchain** (`play-python.yml`, pyenv, Textual would be a `pip install` away). Distribution path is consistent.
5. **The stack rebalance**: with Qobuz risk eliminated, the Rust advantage from Track E vanishes. Python is the right answer post-research.

The "parallel low-cost experiment" on KsanStone is: open an issue asking for an MIT/Apache-2.0 license, attach a one-line patch for it, and see if the maintainer responds within ~1 week. If yes — we have an excellent fork-able starting point that may eclipse Path B in polish for a fraction of the effort. If no — we proceed with Path B without delay. Cost of asking: 5 minutes.

## Pre-implementation gate (run on the desktop)

Before writing any controller code, run Track A's verification probes against both receivers to confirm everything we're banking on:

```bash
# Confirm Qobuz is a first-class input on each receiver
curl -s http://192.0.2.44/YamahaExtendedControl/v1/system/getFeatures \
  | jq '.system.input_list[] | select(.id == "qobuz")'
curl -s http://192.0.2.74/YamahaExtendedControl/v1/system/getFeatures \
  | jq '.system.input_list[] | select(.id == "qobuz")'

# Confirm Qobuz session is logged in
curl -s http://192.0.2.44/YamahaExtendedControl/v1/netusb/getAccountStatus \
  | jq '.service_list[] | select(.id == "qobuz")'

# Confirm searchable nodes exist on the Qobuz tree (after switching to it)
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/main/prepareInputChange?input=qobuz"
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/main/setInput?input=qobuz"
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/netusb/getListInfo?input=qobuz&index=0&size=8&lang=en" \
  | jq '.list_info[] | {text, attribute, searchable: (.attribute % 16 >= 8)}'

# Live test of the search primitive
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"list_id":"search_artist","string":"miles davis"}' \
  http://192.0.2.74/YamahaExtendedControl/v1/netusb/setSearchString \
  | jq .
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/netusb/getListInfo?input=qobuz&index=0&size=8&lang=en" \
  | jq '{menu_name, max_line, list_info: (.list_info | map(.text))}'
```

If those all return what we expect (Qobuz present, logged in, search produces results), Path B begins. If any fail, we pivot to upmpdcli (Track D Path 2) — no controller code wasted.

## If you accept this decision, the next phase looks like:

1. Run the verification probes above; capture output.
2. Open an issue at `https://github.com/KsanStone/MusicCastDesktop` requesting an OSI license. (Path A parallel experiment.)
3. Scaffold a new Python project `musiccast-tui/` (probably under this repo's `untracked/` first, or at a new top-level path — to be decided).
4. Pin `aiomusiccast` and Textual; write a smoke test that connects to both receivers and dumps `getPlayInfo`.
5. Build the now-playing screen first (smallest vertical slice that's useful on its own).
6. Build the browse tree screen with paging + breadcrumbs.
7. Build the search modal.
8. Wire UDP push events for live now-playing updates.
9. Package via `play-musiccast-controller.yml` (new playbook, hardware-specific or common — TBD).

Each is a sub-task in PLAN.md once you greenlight this decision.

## Open questions for you

1. **Form factor confirmation**: TUI (Path B) or Tauri GUI (Path A)? My recommendation is Path B; if you'd rather the GUI, say so and we'll go that direction.
2. **License experiment**: open the KsanStone issue, yes/no?
3. **Project home**: new top-level path in this repo (`musiccast-tui/`), or a separate repo published to your GitHub? Either works.
4. **Scope cut**: any feature in the spec you'd drop to ship faster — multi-zone, presets, search? My instinct: ship now-playing + browse + transport first, then add search and multi-zone in a second iteration.
