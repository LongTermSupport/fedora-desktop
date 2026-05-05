# Home Assistant `yamaha_musiccast` Integration — Deep Dive

## TL;DR

HA's MusicCast integration is a **thin wrapper around `aiomusiccast`** — a separate, redistributable Python library that does all the YXC and UPnP/DLNA work. The integration **does implement `browse_media` and supports browsing Qobuz** (and other streaming services the receiver is logged into) via the receiver's own list-API, with menu-layer-based pagination, plus a tree of presets. Search is implemented in `aiomusiccast.pyamaha` (`setSearchString`), but **the HA integration's `browse_media` does not expose search to the user** — it's directory navigation only. So "wrap HA over REST" gets you 80% of a Qobuz controller for free (browse-and-play + transport + volume + presets), but search you'd have to add yourself by extending the library or talking to YXC directly. The `aiomusiccast` library is the better target — using HA buys nothing the library doesn't already give you.

## Integration source

Files saved to `untracked/00038-musiccast-controller/ha-musiccast/` (manifest, `__init__`, `media_player`, `select`, `number`, `switch`, `config_flow`, `coordinator`, `entity`, `const`, `strings.json`).
`aiomusiccast` cloned to `untracked/00038-musiccast-controller/aiomusiccast/`.

Notable absence: there is **no `services.yaml`** in the integration upstream (404 confirmed). The integration registers only HA's stock `media_player` services — there are no MusicCast-specific RPCs.

## Setup & discovery

`manifest.json` declares:

- `iot_class: local_push`
- `config_flow: true`
- `dependencies: ["ssdp"]`
- `ssdp[].manufacturer: "Yamaha Corporation"`
- `requirements: ["aiomusiccast==0.15.0"]`

Two paths to a configured device, both ending up in the same `async_setup_entry`:

1. **SSDP auto-discovery** (`config_flow.py:async_step_ssdp`). HA's SSDP component picks up any `manufacturer == "Yamaha Corporation"` device. The flow then GETs the `ssdp_location` URL and verifies the device exposes the YXC control URL marker:
   ```python
   "<yamaha:X_yxcControlURL>/YamahaExtendedControl/v1/</yamaha:X_yxcControlURL>" in text
   ```
   If yes, a confirm-dialog is shown to the user. Unique-ID is the SSDP `serial` UUID.
2. **Manual** (`async_step_user`): user supplies an IP. The flow calls `MusicCastDevice.get_device_info(host, session)` (a one-shot YXC `system/getDeviceInfo` GET) and reads `system_id` as the unique key.

Both paths persist three things in the config entry: `host` (IP), `serial`, `upnp_description` (the SSDP `ssdp_location` URL, or a default `http://{host}:49154/MediaRenderer/desc.xml`).

`async_setup_entry` then:

- builds a `MusicCastDevice` (the library object),
- creates a `MusicCastDataUpdateCoordinator` with a 60-second polling fallback,
- runs first refresh, calls `coordinator.musiccast.build_capabilities()` (introspect what the device supports),
- calls `device.enable_polling()` — **this opens a UDP listening socket and tells the receiver to push events to it** (see "Push vs poll" below),
- forwards entry to platforms `MEDIA_PLAYER`, `NUMBER`, `SELECT`, `SWITCH`.

## Entity model

For one receiver, HA spawns a **variable** number of entities driven by `aiomusiccast.capability_registry`. Per zone the receiver advertises in `system/getFeatures`, you get one entity per supported feature on each platform.

- **`media_player`** — one per zone. The receiver's main zone always present; zone2/zone3/zone4 added if `data.zones` reports them. So on a typical RX-V receiver: `media_player.<name>` (main) and `media_player.<name>_zone2`. (Both 192.0.2.44 and 192.0.2.74 in the user's setup are likely 1-zone or 2-zone — confirm in YXC.)
- **`select`** — one per `OptionSetter` capability the device advertises. From `capability_registry.py` and `strings.json` translation keys, candidates: dimmer, sleep timer (per zone), tone-control mode (per zone), surround-decoder type (per zone), equalizer mode (per zone), link-audio-quality, link-audio-delay, link-control.
- **`number`** — one per `NumberSetter` capability. Per zone: equalizer (high/mid/low — three numbers), tone-control (bass/treble — two), dialogue-level, dialogue-lift, DTS dialogue control, subwoofer-volume.
- **`switch`** — one per `BinarySetter` capability. Device-level: speaker A, speaker B, party mode. Zone-level: bass extension, extra bass, enhancer, pure direct, adaptive DRC, clear voice, surround 3D, mono, party mute, headphone, etc.

A typical mid-range MusicCast AVR therefore exposes **roughly 25–40 entities** for one device (one or two `media_player`, plus a long tail of selects/numbers/switches). All `media_player` capability is on the media_player entity itself; the others are config/diagnostic surface for the receiver's DSP/zone settings.

The `entity.py` base class wires every entity into a single `MusicCastDataUpdateCoordinator` per device, so they all refresh together when push events fire.

## media_player capabilities

### Standard surface

`MUSIC_PLAYER_BASE_SUPPORT` flags: `SHUFFLE_SET`, `REPEAT_SET`, `SELECT_SOUND_MODE`, `SELECT_SOURCE`, `GROUPING`, `PLAY_MEDIA`. Then bitwise-OR'd with feature-driven additions: `TURN_ON|TURN_OFF` (if `ZoneFeature.POWER`), `VOLUME_SET|VOLUME_STEP` (if `VOLUME`), `VOLUME_MUTE` (if `MUTE`), `PREVIOUS_TRACK|NEXT_TRACK` (if NetUSB or tuner source), `PAUSE|PLAY|STOP` (NetUSB only), `BROWSE_MEDIA` (whenever the player isn't OFF).

State machine: maps zone power + NetUSB playback to HA `MediaPlayerState`. Pause/stop/seek only work while a NetUSB source (Qobuz, Spotify, server, USB, net_radio, etc.) is selected — for HDMI/analog inputs those calls raise `HomeAssistantError`.

Source list comes from `data.zones[zone].input_list`, mapped to the user-friendly labels the receiver returns from `system/getNameText`.

### `browse_media` — implemented, with caveats

Code in `media_player.py:326`:

```python
async def async_browse_media(self, media_content_type=None, media_content_id=None):
    if media_content_id and media_source.is_media_source_id(media_content_id):
        return await media_source.async_browse_media(
            self.hass,
            media_content_id,
            content_filter=lambda item: item.media_content_type.startswith("audio/"),
        )

    if self.state == MediaPlayerState.OFF:
        raise HomeAssistantError(
            "The device has to be turned on to be able to browse media."
        )

    if media_content_id:
        media_content_path = media_content_id.split(":")
        media_content_provider = await MusicCastMediaContent.browse_media(
            self.coordinator.musiccast, self._zone_id, media_content_path, 24
        )
        add_media_source = False
    else:
        media_content_provider = MusicCastMediaContent.categories(
            self.coordinator.musiccast, self._zone_id
        )
        add_media_source = True
    ...
```

The interesting part is in `aiomusiccast/musiccast_media_content.py`. The browseable inputs whitelist:

```python
BROWSABLE_INPUTS: ClassVar[tuple[str, ...]] = (
    "usb", "server", "net_radio", "rhapsody", "napster", "pandora",
    "siriusxm", "juke", "radiko", "qobuz", "deezer", "amazon_music",
)
```

`MusicCastMediaContent.categories()` builds the root: a "Presets" folder plus one folder per `BROWSABLE_INPUTS ∩ data.zones[zone].input_list`. **So Qobuz only appears as a browseable category if (a) the receiver firmware supports it AND (b) you've signed in via the MusicCast mobile app.** Same applies to Deezer, Amazon Music, etc. — the integration cannot log you in; the official MusicCast app must.

Once you click into a streaming-service folder, browsing is implemented as iterative YXC `netusb/getListInfo` + `netusb/setListControl?type=select` calls, organized by **menu_layer** (the YXC list-navigation primitive, not a search). Each navigation step actually changes the receiver's state machine — there is one global "list cursor" per netusb source. Pagination is implemented by passing `<>N` index suffixes in the `media_content_id`. Each level builds 24 children at a time.

**Important: this is stateful navigation, not a stateless query.** The receiver's NetUSB cursor moves as you browse; if a second client is browsing simultaneously, you'll fight each other.

`content_id` shape encodes the path: `list:qobuz:3:42` (source, menu_layer, index), `presets:5`, `input:qobuz`. Navigation back is via repeated `netusb/setListControl?type=return` until the menu_layer matches the requested one.

### `play_media` — three URI schemes

```python
async def async_play_media(self, media_type, media_id, **kwargs):
    if media_source.is_media_source_id(media_id):
        play_item = await media_source.async_resolve_media(...)
        media_id = play_item.url

    if self.state == MediaPlayerState.OFF:
        await self.async_turn_on()

    if media_id:
        parts = media_id.split(":")
        if parts[0] == "list":
            # play the item at index parts[3] in the current netusb list
            if (index := parts[3]) == "-1":
                index = "0"
            await self.coordinator.musiccast.play_list_media(index, self._zone_id)
            return
        if parts[0] == "presets":
            index = parts[1]
            await self.coordinator.musiccast.recall_netusb_preset(self._zone_id, index)
            return
        if parts[0] in ("http", "https") or media_id.startswith("/"):
            media_id = async_process_play_media_url(self.hass, media_id)
            await self.coordinator.musiccast.play_url_media(
                self._zone_id, media_id, "HomeAssistant"
            )
            return

    raise HomeAssistantError(
        "Only presets, media from media browser and http URLs are supported"
    )
```

So three accepted forms:

1. **`list:<source>:<menu_layer>:<index>`** — play item at that index from the source's currently-displayed list (relies on the receiver's NetUSB cursor being where the browse left it).
2. **`presets:<n>`** — recall a NetUSB preset (1..40 typically). This is the most reliable "resume" path.
3. **`http(s)://...` or `/...`** — play an arbitrary HTTP audio URL. Implemented via UPnP DLNA AVTransport: `aiomusiccast` switches the zone source to `server` with `autoplay_disabled`, sends `Stop`, `SetAVTransportURI`, then `Play` to the receiver's MediaRenderer (`/MediaRenderer/desc.xml`). MIME type is sniffed; arbitrary content works as long as the receiver's DLNA renderer can decode it (it must be PCM/MP3/FLAC/AAC etc., not Qobuz-DRM).

## Browse / search story

**The critical question, answered:**

- **Browse Qobuz: yes, via the receiver's own NetUSB list API.** `qobuz` is in `BROWSABLE_INPUTS`. The integration walks the receiver's Qobuz menu (Discover, Playlists, Favourites, Albums, Tracks, Artists, etc. — whatever the receiver firmware exposes) by sending `netusb/getListInfo?input=qobuz&...` and tree-shaping the response into HA's `BrowseMedia`. Login is **out-of-band** — done in the official MusicCast Android/iOS app once, then the receiver remembers the credentials.
- **Search Qobuz from HA: no, not via `browse_media`.** The YXC API has `setSearchString` (and its glue is implemented in `aiomusiccast.pyamaha.NetUSB.set_search_string`), but `MusicCastMediaContent.browse_media` doesn't surface it as a node. The dispatch only knows three top-level path verbs: `input:`, `list:`, `presets:`. To search, you'd send `setSearchString` directly, then re-browse — not something `media_player.browse_media` does today.
- **Searching general Qobuz catalog from outside the receiver: no.** The receiver only exposes what its firmware Qobuz integration shows — so search is whatever the Qobuz-on-receiver UI shows (which on RX/CX models is "Search by track, album, artist" via on-screen keyboard, basically). It's not a Qobuz Web API call.

**Net effect for the desktop UX:** browsing predictable Qobuz hierarchy (Favourites, Playlists, recent) works. Free-text search of the Qobuz catalog is a separate problem we'd have to solve either by (a) adding `setSearchString` to our flow on top of YXC, or (b) calling Qobuz's own API for catalog search and then using `play_url_media` or list-select to actually start playback (this second path is what other research tracks should clarify).

## Push vs poll

**Hybrid: UDP push + 60-second polling fallback.**

`AsyncDevice.enable_polling()` (in `pyamaha.py`) opens an asyncio datagram endpoint on `0.0.0.0:0`, gets the assigned port, and adds two HTTP request headers used on every subsequent YXC HTTP call:

```python
self._headers.update({"X-AppName": "MusicCast/1.0", "X-AppPort": str(port)})
```

These headers tell the receiver *"send UDP push events to my IP at this port"*. The receiver then unicasts JSON datagrams to that port whenever state changes (volume, input, playback, group membership, etc.). `MusicCastUdpProtocol.datagram_received` parses each datagram and dispatches to `MusicCastDevice.handle()`, which selectively re-fetches the relevant subsystem (zone, netusb, tuner, dist, clock, system) via HTTP.

The 60-second `DataUpdateCoordinator` SCAN_INTERVAL is a safety net for missed UDP packets (UDP is unreliable, and the docs explicitly warn about garbled messages — "If you receive these errors frequently, try LAN cable instead of WiFi").

This means: **for our desktop UX, if we use `aiomusiccast` directly, we get sub-second state updates for free.** If we use HA's WebSocket API on top, we get the same data at one extra hop's latency.

## Limitations (from official docs + reading code)

From the official HA integration page:

1. **Grouping limits**: cannot put zones of the same device in distinct groups; if a non-main zone is the master of a group, other zones on the same device cannot join it.
2. **Streaming-service login is out-of-band**: "For services such as Deezer, you have to log in using the official MusicCast app." Same for Qobuz, Amazon Music, etc.
3. **UDP messages aren't error-corrected**: occasional "Received invalid message" / "non-UTF-8 compliant message" log spam, especially over WiFi.

From reading code (not in docs):

4. **`browse_media` requires zone power == ON**: raises `HomeAssistantError` if the player is OFF. So the UX must turn on the receiver before browsing — or work around it by short-circuiting at the library level.
5. **Stateful list cursor**: the YXC `netusb/getListInfo` model has *one cursor per netusb input* on the receiver. Two HA users browsing simultaneously will collide, and the integration doesn't synchronize that.
6. **No search**: `MusicCastMediaContent` ignores the `Capable of Search` bit (`attribute & 0b1000`) that `from_info` decodes — it stores `can_search` on the result but never produces a search-prompt node.
7. **`play_media` accepts only three URI shapes** — the `list:`, `presets:`, and `http://` forms above. Anything else raises `HomeAssistantError`. Notably no Qobuz track URI like `qobuz://track/12345` — you can't deep-link to a Qobuz item; you must navigate to it.
8. **NetUSB transport ops only on NetUSB sources**: pause/stop/shuffle/repeat raise `HomeAssistantError` if the active source is HDMI/analog/optical. Tuner sources only support next/prev (band tune). This is correct receiver behaviour but means the UX must hide controls when on a non-NetUSB source.

## "Wrap HA over REST" architectural evaluation

### REST/WebSocket call patterns we'd use

**REST API** (`/api/...`, `Authorization: Bearer <long-lived token>`):

- `GET /api/states/media_player.<name>` — current state, `attributes.media_title/artist/album/source/source_list/volume_level/...`
- `GET /api/services/media_player` — service catalog (these are HA-stock, not MusicCast-specific):
  - `media_player.turn_on/turn_off`, `volume_set`, `volume_mute`, `media_play/pause/stop`, `media_next_track/previous_track`, `select_source`, `select_sound_mode`, `play_media`, `shuffle_set`, `repeat_set`, `join`, `unjoin`.
- `POST /api/services/media_player/play_media` body `{"entity_id":"media_player.x","media_content_id":"presets:1","media_content_type":"music"}`.
- For browse, HA's REST surface is awkward — `browse_media` is **WebSocket-only** (`media_player/browse_media` and `media_player/play_media` ws commands), not REST.

**WebSocket API** (`ws://ha:8123/api/websocket`, JSON envelope, bearer auth):

- After auth handshake, `{"type":"subscribe_events","event_type":"state_changed"}` → real-time state diffs.
- `{"type":"media_player/browse_media","entity_id":"media_player.x","media_content_id":"input:qobuz","media_content_type":"directory"}` — returns a `BrowseMedia` tree node.
- `{"type":"call_service","domain":"media_player","service":"play_media",...}` — invoke playback.

### What we'd build (thin client over HA)

A Python or TypeScript desktop app that:

- holds an HA bearer token,
- maintains one WebSocket connection per HA instance,
- mirrors `media_player.<name>` state into our UI,
- renders Qobuz browse trees by recursively calling `media_player/browse_media` with `media_content_id` = `""`, `input:qobuz`, then drilling deeper,
- triggers playback by `call_service` on `play_media` with the returned `media_content_id`,
- handles transport (play/pause/next/prev/volume/mute) via standard `media_player` services.

### What we'd NOT have to build

- YXC client (HA does it).
- UPnP DLNA client for arbitrary URL playback (HA does it).
- UDP push listener (HA does it; our WebSocket subscription gives us state diffs derived from it).
- SSDP discovery (HA does it).
- Group/zone state management.
- The 50% of the integration that is config/diagnostic switches — we just don't expose them in the UX.

### What we'd LOSE by depending on HA

- **Operational dependency**: a Podman'd HA container running 24/7. Updates, breakage, config-flow re-auth on token expiry, image bloat (~1GB).
- **Search**: HA does *not* solve search. We have to add it ourselves either way — and adding it in HA's media-browser model means writing an HA custom component, which is more painful than calling YXC directly.
- **Latency**: every action goes desktop → HA → receiver. UDP push events round-trip desktop ← HA ← receiver. Adds tens of ms (negligible on LAN, but pointless overhead).
- **Tied to HA's `browse_media` model**: it's tree-only. Our UX may want a flatter "recent / favourites / search" layout that doesn't map cleanly to HA's recursive node model. We'd be fighting it.
- **No catalog enrichment**: HA's browse only exposes what the receiver returns. Album art, tracklists, artist bios — not available unless we go to Qobuz Web API anyway.
- **Stale data risk**: HA's coordinator polling is 60s, push handles transient state — if the WS connection drops, we get a stutter.

### Verdict: **partially viable, but not the obvious shortcut**

If the user's goal is a *quick* "see what's playing + transport + browse Favourites/Playlists" UX with minimal effort, **wrap HA**. You get browse, transport, presets, volume, source-select, and grouping for free over a single WebSocket. Total work: a few hundred lines of UI glue.

If the goal is full-fidelity Qobuz including **catalog search**, HA gives you nothing extra — you'd still need either the receiver's `setSearchString` (not exposed by HA's browse_media) or Qobuz's own Web API. At that point HA is dead weight: an extra service to keep running, an extra abstraction layer to debug, and zero help on the hard problem.

**The smart path is to use `aiomusiccast` directly**, because:

- It's MIT-licensed, pip-installable, ~5,000 LOC.
- It exposes everything HA exposes (it *is* what HA calls under the hood) plus things HA doesn't expose (`set_search_string`, raw `get_list_info` with arbitrary `list_id`).
- It already speaks YXC + UPnP DLNA + UDP push correctly.
- No HA server, no token management, no WebSocket dance.
- Same async-Python idiom we'd use for HA's WS API anyway.

`aiomusiccast` is the integration's value — HA wraps it, but adds nothing on top that helps us.

## aiomusiccast library notes

### API surface (relevant subset)

`MusicCastDevice(ip, aiohttp.ClientSession, upnp_description=None)`:

- **Lifecycle**: `await device.fetch()` (full state pull), `await device.device.enable_polling()` (start UDP push receiver), `device.device.disable_polling()`, `device.register_callback(cb)`, `device.register_group_update_callback(async_cb)`.
- **Discovery helpers** (classmethods): `MusicCastDevice.check_yamaha_ssdp(location, session)`, `MusicCastDevice.get_device_info(ip, session)`.
- **Power/volume per zone**: `turn_on(zone_id)`, `turn_off`, `mute_volume(zone_id, bool)`, `set_volume_level(zone_id, 0..1)`, `volume_up/down`.
- **Source**: `select_source(zone_id, source, mode="")`, `select_sound_mode(zone_id, mode)`.
- **NetUSB transport**: `netusb_play/pause/stop/previous_track/next_track`, `netusb_shuffle(bool)`, `netusb_repeat(mode)`.
- **Tuner**: `tuner_previous_station/next_station`.
- **Browse (matches what HA uses)**: `get_list_info(source, start_index)`, `select_list_item(item, zone_id)`, `return_in_list(zone_id)`, `play_list_media(item, zone_id)`.
- **Presets**: `recall_netusb_preset(zone_id, n)`, `store_netusb_preset(n)`.
- **Arbitrary HTTP URL via DLNA**: `play_url_media(zone_id, url, title, mime_type=None)`.
- **MusicCast group/distribution**: `mc_server_group_extend/reduce/close`, `mc_client_join/unjoin`, `zone_join/unjoin` — full client/server group lifecycle.
- **Properties**: `data.netusb_track`, `data.netusb_artist`, `data.netusb_album`, `data.netusb_albumart_url` (relative; prepend `http://{ip}` — `media_image_url` does this), `data.netusb_play_time/total_time`, `data.zones[zone].input/current_volume/mute/power/sound_program/...`, `data.input_names` (friendly labels).
- **MusicCastMediaContent** (re-export at package root): the same browse helper HA uses; can be called standalone if we want HA's tree shape.

### What's there that HA doesn't expose

- `pyamaha.NetUSB.set_search_string(text, list_id="main", index=None)` — write a YXC `POST /netusb/setSearchString` payload. Combined with `get_list_info(source, 0)` after, this is the **search primitive** the HA integration drops on the floor.
- `pyamaha.NetUSB.get_service_info(input, type, timeout)` and `get_account_status` — for streaming-service login state.
- `pyamaha.NetUSB.switch_account(input, index, timeout)` — change which logged-in account is active.
- `pyamaha.System.get_features()` — full feature catalog (input list, sound programs per zone, repeat/shuffle modes, zone caps).
- `pyamaha.Tuner.set_freq` / DAB methods — tuner control.
- `pyamaha.Clock` — alarm scheduling.

### Could we use this library directly?

Yes — trivially. `pip install aiomusiccast` and:

```python
import aiohttp, asyncio
from aiomusiccast import MusicCastDevice

async def main():
    async with aiohttp.ClientSession(cookie_jar=aiohttp.DummyCookieJar()) as session:
        dev = MusicCastDevice("192.0.2.44", session)
        await dev.fetch()
        await dev.device.enable_polling()
        dev.register_callback(lambda: print("state changed"))
        # browse Qobuz top level
        info = await dev.get_list_info("qobuz", 0)
        print(info)
        # ... drive the receiver
        await asyncio.Future()  # park
```

That's it. No HA, no WebSocket, no token. Everything HA's media_player.py does at line 326–396 is reachable directly via `MusicCastMediaContent.browse_media(dev, "main", path, 24)` if we want HA's tree shape, or via the lower-level `dev.get_list_info` + `dev.select_list_item` if we want our own.

The library is the right dependency. HA is not.

## Sources

- HA integration source (snapshot from `home-assistant/core` `dev` branch): `untracked/00038-musiccast-controller/ha-musiccast/`
  - `manifest.json`, `__init__.py`, `config_flow.py`, `coordinator.py`, `entity.py`, `const.py`
  - `media_player.py` (931 lines — the meat)
  - `select.py`, `number.py`, `switch.py` (capability-driven, ~60 lines each)
  - `strings.json` (translation keys reveal the entity catalog)
  - `services.yaml` HTTP 404 upstream — confirms no integration-specific services are registered (all `media_player.*` services are HA-stock)
- aiomusiccast library (cloned, depth=1, MIT-licensed): `untracked/00038-musiccast-controller/aiomusiccast/`
  - `aiomusiccast/__init__.py` — public API surface
  - `aiomusiccast/musiccast_device.py` (1132 lines — core device wrapper)
  - `aiomusiccast/musiccast_media_content.py` (217 lines — browse tree builder)
  - `aiomusiccast/pyamaha.py` (2319 lines — YXC URL templates, UDP protocol, UPnP DLNA AVT)
  - `aiomusiccast/capabilities.py` + `capability_registry.py` — capability/EntityType model that drives select/number/switch entities
  - `aiomusiccast/features.py` — `DeviceFeature` and `ZoneFeature` flag enums
- Official HA integration docs: `https://www.home-assistant.io/integrations/yamaha_musiccast` (saved to `/tmp/ha_doc.md`, 133 lines)
  - Grouping limitations section
  - "Play media functionality" section: presets via `presets:N`, HTTP URL via `media_content_id`, login-via-mobile-app caveat
  - "Errors on handling UDP messages" troubleshooting
- HA WebSocket API reference (general): `https://developers.home-assistant.io/docs/api/websocket/` — for `media_player/browse_media` and `media_player/play_media` ws commands, and `subscribe_events`
- HA REST API reference (general): `https://developers.home-assistant.io/docs/api/rest/` — for `/api/states`, `/api/services/<domain>/<service>`
