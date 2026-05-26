# OSS MusicCast Controller Landscape

## TL;DR

The MusicCast OSS landscape is **dominated by integrations** (Home Assistant, Homebridge, ioBroker, OpenHAB, MQTT bridges, Streamdeck plugins) rather than standalone end-user controllers. Of ~80 repos surfaced, almost none ship a usable end-user UI for browse + search + select-and-play. The single standout is **KsanStone/MusicCastDesktop** — a Tauri+Vue+Rust desktop app with NetUSB browse, search, multi-zone, and now-playing already implemented; the rest of the desktop/web/TUI controllers are either dormant Windows-only forks (pulpul-s lineage), archived web toys (axelo), or library-only. The strongest **library** for building on top of is **vigonotion/aiomusiccast** (Python, used by HA, supports browse + search via NetUSB list IDs `auto_complete`/`search_artist`/`search_track`); a strong runner-up is **foxthefox/yamaha-yxc-nodejs** (Node, actively maintained May 2026).

## Already evaluated and discounted

- **atamanroman/ymc** (Go TUI) — panics on empty list, dormant, no fork has fixed it
- **balinbob/yrc** (gyrc, GTK) — dormant since 2020
- **FrankEberle/MCcontrol** (CGI/Haserl OpenWRT) — wrong target
- See `PLAN.md` for context.

## Survey table

Maintenance signal: 🟢 = pushed within 6 months; 🟡 = within 2 years; 🔴 = older or archived.

| Project                                | Lang             | UI form                      | Last push          | Stars | License     | Now-playing                       | Browse                                                  | Search                                                                             | Qobuz-aware                                                 | Notes                                                                                                                                                                  |
| -------------------------------------- | ---------------- | ---------------------------- | ------------------ | ----- | ----------- | --------------------------------- | ------------------------------------------------------- | ---------------------------------------------------------------------------------- | ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 🟢 KsanStone/MusicCastDesktop          | Rust+Vue (Tauri) | desktop GUI                  | 2026-01            | 0     | none stated | yes (PlaybackCard, art, progress) | yes (NetUsbListBrowser w/ paging)                       | yes (`set_search_string`, search dialog UI)                                        | yes (input icon + struct field; uses generic NetUSB search) | Top adoption candidate. README is template-only — actual code is real. Sibling crate `KsanStone/yamaha_rs`.                                                            |
| 🟢 foxthefox/yamaha-yxc-nodejs         | JS (Node)        | library                      | 2026-05            | 27    | MIT         | (lib API)                         | (lib API)                                               | (lib API)                                                                          | input id only                                               | Most-active YXC library on GitHub. Comprehensive method coverage (zone, NetUSB, list control, presets, distribution, CD, tuner). Has CI, fixture data for many models. |
| 🟢 foxthefox/ioBroker.musiccast        | JS               | ioBroker adapter             | 2026-05            | 13    | MIT         | -                                 | -                                                       | -                                                                                  | -                                                           | Wrong target (smart-home integration platform). Not a desktop controller.                                                                                              |
| 🟡 vigonotion/aiomusiccast             | Python           | library                      | 2025-11            | 13    | MIT         | (lib API)                         | yes (`MusicCastMediaContent.browse_media`, `load_list`) | yes (LIST_IDs `auto_complete`, `search_artist`, `search_track`; `can_search` flag) | input id only                                               | Backbone of the HA integration. Async, typed, well-structured, has tests. **Best Python lib to build on top of.**                                                      |
| 🟡 ha-musiccast (already cloned)       | Python           | HA media_player              | n/a                | n/a   | (HA core)   | yes                               | yes                                                     | yes (via aiomusiccast)                                                             | -                                                           | The HA integration itself — already in the cloned dir. Useful as reference for browse/search patterns.                                                                 |
| 🟡 redrabbit/musiccast                 | Elixir           | library                      | 2018-10 (archived) | 7     | MIT         | yes (Entity playback struct)      | minimal                                                 | no                                                                                 | input id only                                               | Cleanly modeled (GenServer per device, UPnP+YXC). Archived. Good API design but can't adopt as-is — would need maintainer to revive or fork.                           |
| 🟡 karlentwistle/music_cast            | Ruby             | library                      | 2024-04 (archived) | 6     | MIT         | partial (GetStatus)               | no                                                      | no                                                                                 | -                                                           | Power/Volume/Mute/Status only. No NetUSB. Archived. Not viable for our UX.                                                                                             |
| 🔴 axelo/opinionated-music-cast-remote | Elm+Node         | web (mobile-style)           | 2018-09 (archived) | 2     | none        | partial (volume + power)          | no                                                      | no                                                                                 | no                                                          | YAS-306 personal toy. 356-line backend, hardcoded IPs. Skip.                                                                                                           |
| 🔴 ppanagiotis/pymusiccast             | Python           | HA group helper              | 2021-07 (archived) | 38    | MIT         | -                                 | -                                                       | -                                                                                  | -                                                           | Pre-aiomusiccast HA helper. Archived. Superseded.                                                                                                                      |
| 🔴 pulpul-s/MusicCast-Control          | C# WPF           | Windows GUI                  | 2022-12            | 2     | none        | no                                | no                                                      | no                                                                                 | input id list (qobuz string)                                | Power/Volume/Mute/Inputs only. No now-playing, no browse. Windows-only.                                                                                                |
| 🔴 CatAnnaDev/MusicCast-Control-WPF    | C# WPF           | Windows GUI fork             | 2022-12            | 1     | none        | no                                | no                                                      | no                                                                                 | input id list                                               | Fork of pulpul-s, same scope.                                                                                                                                          |
| 🔴 youtoofan/UWP-Musiccast             | C#               | UWP                          | 2025-06            | 0     | AGPL-3.0    | unknown (no README)               | unknown                                                 | unknown                                                                            | unknown                                                     | Empty README — would need reverse engineering to assess. UWP is Windows-only. Skip.                                                                                    |
| 🔴 vi7/yamaterm                        | Bash + Go (WIP)  | CLI                          | 2021-12            | 1     | none        | no (status print)                 | no                                                      | no                                                                                 | -                                                           | "Terminal app" is a misnomer — it's a single bash script wrapping HTTP calls. Go client never finished. Dormant.                                                       |
| 🔴 jonaseickhoff/musiccast2mqtt        | TS               | MQTT bridge                  | 2022-09            | 5     | MIT         | -                                 | -                                                       | -                                                                                  | -                                                           | Headless. Wrong target. Inspired by yxc-nodejs.                                                                                                                        |
| 🔴 Iture/MusicCastControl              | Python           | MQTT bridge (archived)       | 2020-05            | 14    | none        | -                                 | -                                                       | -                                                                                  | -                                                           | Archived MQTT bridge. Wrong target.                                                                                                                                    |
| 🔴 jac459/yamaha-musiccast             | (json)           | NEEO Remote driver           | 2026-02            | 2     | MIT         | yes (in NEEO)                     | yes (NEEO directory)                                    | no                                                                                 | no                                                          | NEEO is a discontinued proprietary remote ecosystem. Wrong target.                                                                                                     |
| 🔴 nicolabs/musiccast-repairkit        | JS               | scenario runner (CLI/Docker) | 2023-09            | 5     | NOASSERTION | -                                 | -                                                       | -                                                                                  | -                                                           | Headless scenarios (volume sync, source-program mapping). Not a UI controller.                                                                                         |
| 🔴 adorobis/yamaha_musiccast           | (docs)           | API docs + sniff logs        | 2023-03            | 5     | none        | -                                 | -                                                       | -                                                                                  | -                                                           | Reference material — playlist-management endpoint sniffing logs. Useful as **documentation source**, not adoption.                                                     |
| 🔴 samvdb/php-musiccast-api            | PHP              | library                      | 2020-09            | 9     | none        | (lib API)                         | (lib API)                                               | (lib API)                                                                          | -                                                           | Dormant. PHP wrong target for a Linux desktop app.                                                                                                                     |
| 🔴 aangert/homebridge-musiccast-tv     | JS               | Homebridge plugin            | 2021-11            | 17    | GPL-3.0     | -                                 | -                                                       | -                                                                                  | -                                                           | HomeKit accessory plugin. Wrong target.                                                                                                                                |
| 🔴 cgierke/homebridge-musiccast        | TS               | Homebridge plugin            | 2025-09            | 4     | none        | -                                 | -                                                       | -                                                                                  | -                                                           | Same — wrong target.                                                                                                                                                   |
| 🔴 Pingviinituutti/musiccast-mqtt      | Rust             | MQTT bridge                  | 2023-09            | 0     | MIT         | -                                 | -                                                       | -                                                                                  | -                                                           | Tiny experimental Rust MQTT bridge. Skip.                                                                                                                              |

(Several other smart-home/automation glue repos surfaced in the search — Jeedom plugins, OpenHAB binding, audiostreamerscrobbler, Streamdeck, ESP-based IR bridges, MagicMirror modules — none relevant for a Linux desktop end-user controller.)

## Top candidates (deeper review)

### Candidate 1: KsanStone/MusicCastDesktop

- URL: <https://github.com/KsanStone/MusicCastDesktop>
- License: not specified (treat as all-rights-reserved unless we ask the author to add one)
- Status: actively developed Jan 2026; **0 stars / 0 forks / 0 issues** — almost certainly a personal project no one has discovered
- Cloned to: `untracked/00038-musiccast-controller/MusicCastDesktop` (and sibling lib `untracked/00038-musiccast-controller/yamaha_rs`)

**Architecture**

- Tauri 2 shell (Rust backend, Vue 3 frontend, single binary, GTK on Linux)
- Frontend: Vue 3 + Vuetify 3 + Pinia + vue-router + auto-imports + file-based routing (`unplugin-vue-router`). Modern, idiomatic.
- Backend: Rust commands in `src-tauri/src/lib.rs` that thin-wrap a sibling crate `KsanStone/yamaha_rs` (also cloned). 80+ Tauri commands cover: discover, device info, zone status, signal info, sound program, NetUSB play/list/search/recent, presets, YPAO config, multi-zone volume/mute/EQ/tone, dialogue lift/level, subwoofer, sound program list.
- `yamaha_rs/src/lib.rs` posts directly to `/v1/netusb/setSearchString`, `/v1/netusb/setListControl`, etc. — pure YXC HTTP client, no UPnP, no MusicCast-app reverse engineering needed.
- Models include `NetUsbQobuz { login_type }` (so the receiver's Qobuz auth state is observable in the UI).

**What we'd inherit**

- Working device discovery via SSDP
- A real-time now-playing card with album art (served from `http://<receiver_ip>/<albumart_url>`), progress slider, transport controls, repeat/shuffle, signal-info (sample rate, bit depth, format)
- A paged infinite-scroll NetUSB list browser with menu-layer breadcrumbs, return-to-parent, item type icons (folder/playable/searchable)
- Search dialog that fires when a `searchable` list item is tapped
- Multi-zone awareness baked into every command (`zone: main` is the default but already parameterized)
- Input selector + sound-program selector
- Recent-items list (Qobuz/streaming history surfaces here)

**What we'd need to change to fit our UX requirements**

- **License**: ask author to add an explicit license (MIT-ish) before adopting, or fork and use under whatever the author later sets. Currently a hard blocker for public adoption.
- **Qobuz-specific UX**: the existing browser is generic NetUSB. Our spec wants Qobuz **search** as a first-class operation. Implementation path: when input is `qobuz`, drop into the NetUSB list, fire `set_search_string` directly without requiring the user to navigate the menu — the search endpoint accepts the same call shape regardless of where in the list tree you are. Add a "Search Qobuz" entry point that sets input → opens browser → triggers search dialog with the user's query.
- **Linux packaging**: Tauri builds an AppImage / .deb / .rpm out of the box; we'd add a `play-musiccast-controller.yml` Ansible playbook to install via downloaded artifact (no Flatpak yet from this project).
- **Vuetify look**: very Material-ey. Cosmetic, easy to retheme.
- **Polling vs push**: today the code looks polling-based (status fetched on demand). The receiver supports YXC UDP event push on port 41100 — `axelo/opinionated-music-cast-remote` shows the pattern. Worth wiring in for snappier now-playing updates, but optional.
- **Tests**: there are no tests in either the desktop repo or `yamaha_rs`. We'd want at minimum smoke tests for the IPC layer.

**Honest verdict**

**Adopt-as-fork is the strongest path.** The code is genuinely capable, modern, and matches our feature checklist (now-playing + browse + search + Qobuz-aware + multi-zone). The blockers are administrative (license, packaging), not architectural. Build-from-scratch alternatives can't match what exists here in a few days of work. Risk: solo maintainer, no traction. Mitigation: fork, add license, contribute upstream where possible.

### Candidate 2: vigonotion/aiomusiccast (library)

- URL: <https://github.com/vigonotion/aiomusiccast>
- License: MIT
- Status: actively maintained (Nov 2025), 13 stars, 16 open issues, 10 forks. Backbone of HA integration.
- Cloned to: `untracked/00038-musiccast-controller/aiomusiccast`

**Architecture**

- `pyamaha.py`: low-level YXC HTTP + UDP client (request, request_json, post, dlna_avt_request)
- `musiccast_device.py`: device-level orchestrator (capabilities, polling, event handling)
- `musiccast_media_content.py`: high-level browse abstraction with `MusicCastMediaContent` dataclass (`is_playable`, `is_browsable`, `can_search`, `from_info`, `browse_media`, `load_list`, `return_in_list_info`)
- `capabilities.py`, `capability_registry.py`, `features.py`: typed capability model derived from `getFeatures`
- `LIST_ID = ["main", "auto_complete", "search_artist", "search_track"]` — explicit support for streaming-service search list IDs (which is what Qobuz needs)
- `pyproject.toml`, `pytest.ini`, `mypy.ini`, `.pre-commit-config.yaml`, `uv.lock` — proper Python project hygiene

**Five-line example (constructed from the API surface)**:

```python
from aiomusiccast import MusicCastDevice
async with aiohttp.ClientSession() as session:
    dev = MusicCastDevice("192.0.2.42", session)
    await dev.fetch()
    content = await dev.media_content("main")  # browse root
    await content.play_item(index=3)
```

**What we'd need to change to fit our UX**

- Build a UI from scratch on top (TUI via Textual, GUI via PyQt/GTK4, or web via FastAPI+HTMX) — aiomusiccast is library-only.
- Wire a now-playing event loop using its UDP listener (`pyamaha.py` already implements UDP datagram reception).
- Qobuz search: use `load_list("search_track")` with `setSearchString` — the lib already exposes the LIST_ID enum, would just need a thin `search_qobuz(query)` helper.

**Honest verdict**

**Best library if we choose to build our own UI.** Tooling is excellent, types are sharp, async is clean. Pick this if we want a Python TUI/GUI we fully control. Cost: weeks of UI work that KsanStone has already done.

### Candidate 3: foxthefox/yamaha-yxc-nodejs (library)

- URL: <https://github.com/foxthefox/yamaha-yxc-nodejs>
- License: MIT
- Status: actively maintained (May 2026), 27 stars, 10 forks, has CI, has mock-server fixtures for many real Yamaha models (RX-V481, RX-A2070, WX-010, WX-21, WX-51, YAS-306, YAS-408, ISX-18D, RX-V685, RX-V781, CD-NT670D)
- Cloned to: `untracked/00038-musiccast-controller/yamaha-yxc-nodejs`

**Architecture**

- Single class `YamahaYXC` with method-per-API-endpoint
- Methods cover everything: zone (power/volume/mute/input/sound-program/EQ/tone/balance/sub/bass-extension), NetUSB (presets, recent, playback, repeat/shuffle, list info, list control, search), CD, tuner, distribution (multi-zone), MC playlist, settings.
- `lib/data/*.json` — large set of `getFeatures` fixtures from real devices, useful for test/dev without hardware.

**Honest verdict**

Strong, actively-maintained Node.js library. If we wanted to build a controller in Electron / web frontend in JS, this would be the pick over reimplementing. **But** it's a layer below KsanStone — KsanStone has already built the UX on top of an equivalent (Rust) library. Verdict: **good library, not directly adopt-able as a controller**. Only worth picking if we deliberately want a Node/Electron stack.

### Candidate 4: redrabbit/musiccast (Elixir, archived)

- URL: <https://github.com/redrabbit/musiccast>
- License: MIT (archived 2018)
- Cloned to: `untracked/00038-musiccast-controller/redrabbit-musiccast`

**Architecture**

- Per-device GenServer (`MusicCast.Network.Entity`) holding live device state
- Pub/sub via `MusicCast.subscribe(device_id)` — deliveries arrive as Erlang messages (`{:musiccast, :update, ...}`)
- UPnP A/V transport stack (`AVTransport`, `AVMusicTrack`, SSDP client)
- YXC client + UDP event dispatcher

**Honest verdict**

**Elegant model**, clearest event-driven design of any candidate, but **archived 2018 and Elixir/BEAM is a hard ask** for a Linux desktop user shell — would need Phoenix LiveView or similar to render UI, plus an Elixir runtime. Only worth a look if we end up wanting reactive event semantics and don't want to roll our own. Hard pass for a desktop adoption candidate.

### Candidate 5: karlentwistle/music_cast (Ruby, archived)

- URL: <https://github.com/karlentwistle/music_cast>
- License: MIT (archived April 2024)
- Cloned to: `untracked/00038-musiccast-controller/karlentwistle-music_cast`

Power, volume, mute, status only. No NetUSB. Stale. **Hard pass.**

## Libraries (no UI, but solid for building on top)

### vigonotion/aiomusiccast

Covered above as Candidate 2. Best Python option. Used by HA core.

### foxthefox/yamaha-yxc-nodejs

Covered above as Candidate 3. Best JS option, most active overall.

### KsanStone/yamaha_rs

Sibling Rust crate to MusicCastDesktop. Covers everything the desktop app needs but is an unpublished crate (git-only). If we adopt KsanStone, we get this for free; if we pick another stack, this is the only Rust option but unpublished and undocumented — would not pick standalone.

## Recommendations for the decision gate

- **Best "adopt as-is" (or near-as-is)**: **KsanStone/MusicCastDesktop** — fork it, get the maintainer to add a license (MIT/Apache-2.0), wire a Qobuz-search shortcut entry-point, package as AppImage via Ansible. Estimated 2–3 days to a usable build vs. ~2 weeks build-from-scratch.
- **Best "library to build on top of"**: **vigonotion/aiomusiccast** if we want Python; **foxthefox/yamaha-yxc-nodejs** if we want JS/Electron. Both are actively maintained with real test coverage and capability models. aiomusiccast is the more elegant one and the LIST_ID search constants are exactly what Qobuz search wants.
- **"Build from scratch" still on the table because**: license uncertainty on KsanStone, plus we may want a TUI rather than a Tauri GUI for keyboard-first use. If TUI is the requirement, none of the existing controllers work — we'd build atop aiomusiccast (Python+Textual) or foxthefox/yamaha-yxc-nodejs (Node + Ink/Blessed). Aiomusiccast wins on stack ergonomics.

## Sources

- GitHub topic search `topic:musiccast` (50 results)
- GitHub topic search `topic:yamaha-musiccast` (10 results)
- GitHub keyword search `musiccast` (sorted by updated; ~80 results)
- Each candidate's repository metadata via `gh api repos/<owner>/<repo>`
- Direct README fetch via `gh api repos/<owner>/<repo>/readme`
- Source-tree inspection in cloned repos under `untracked/00038-musiccast-controller/`
- Confirmed `foxxyz/musiccast-cli` and `PSeitz/yamaha-controller` (mentioned in the brief) do **not** exist as named on GitHub — `foxxyz` has no MusicCast project, `PSeitz/yamaha-nodejs` covers older non-MusicCast Yamaha AVRs.
