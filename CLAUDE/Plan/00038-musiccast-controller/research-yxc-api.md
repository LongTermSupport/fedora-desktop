# YXC API Research

## TL;DR

Yes — YXC gives us everything we need: now-playing (`netusb/getPlayInfo`), tree-style browsing (`netusb/getListInfo` + `setListControl`), service-side search (`netusb/setSearchString`), and Qobuz exposed as a first-class `netusb` input on Yamaha hardware that has Qobuz licensed. Push events arrive as plain UDP-unicast JSON to a port the controller declares in an `X-AppPort` request header — no polling required for status changes. The protocol is unauthenticated HTTP on port 80, every endpoint is `GET` (one exception: `setSearchString` is documented as `POST`), and the spec we'll work against is YXC API Specification (Basic) Rev. 1.10, Yamaha 2016, copy saved at `yxc-api-spec-basic.pdf` in this folder.

## URL convention & auth

- Base URL: `http://{host}/YamahaExtendedControl/v1/`
- Pattern: `/YamahaExtendedControl/v1/<service>/<command>`
- Services in scope for a controller: `system`, `main` / `zone2` / `zone3` / `zone4`, `netusb`, `tuner`, `cd`, `dist` (link distribution).
- Versioning: the path segment `v1` corresponds to the "API Version" returned by `system/getDeviceInfo`. The user's two receivers both report `api_version: 2.08` — backward compat is asserted by the spec, so v1 paths still work on v2 firmware.
- HTTP verb: **GET for everything**, including all `set*` operations. The single exception in the Basic spec is `netusb/setSearchString`, which is documented as POST with a JSON body (in practice many community libraries use the GET form with query params and it works — but stick to POST for spec-compliance).
- Auth: **none.** No headers, no cookies, no API key. Anyone on the LAN can control any MusicCast device. (No HTTPS either — port 80 plaintext.)
- Discovery: SSDP `urn:schemas-upnp-org:device:MediaRenderer:1`, then read the device description XML and look for `<yamaha:X_yxcControlURL>` and `<manufacturer>Yamaha Corporation</manufacturer>`.
- Two custom request headers if you want push events:
  - `X-AppName: MusicCast/<version>` (the spec example is `MusicCast/1.40(iOS)`)
  - `X-AppPort: <udp_port>` (e.g. `41100`) — the receiver will then UDP-unicast events to your IP on that port for 10 minutes after each request, refreshed by every subsequent request from the same IP.

## Endpoint reference (only the ones a controller cares about)

### `system/getDeviceInfo`

- Verb: GET
- Returns: `model_name`, `destination`, `device_id` (12-char ASCII, unique per device), `system_id`, `system_version`, `api_version`, `netmodule_version`, `netmodule_checksum`, `operation_mode`, `update_error_code`.
- Why a controller cares: identity, capability gating (api_version ≥ 1.17 unlocks `device_id`, jpg/png/bmp album art).
- Verified live response from 192.0.2.44 (RX-A3070):

```json
{"response_code":0,"model_name":"RX-A3070","destination":"BG","device_id":"AC44F243C7AD","system_id":"0A2DEC03","system_version":2.87,"api_version":2.08,"netmodule_generation":1,"netmodule_version":"1923    ","netmodule_checksum":"A7E6476F","operation_mode":"normal","update_error_code":"00000000"}
```

- Verified live response from 192.0.2.74 (WXA-50):

```json
{"response_code":0,"model_name":"WXA-50","destination":"BG","device_id":"AC44F24FC141","system_id":"0511EC93","system_version":2.86,"api_version":2.08,"netmodule_generation":1,"netmodule_version":"1925    ","netmodule_checksum":"F73506B6","operation_mode":"normal","update_error_code":"00000000"}
```

### `system/getFeatures`

- Verb: GET
- Returns: a fat tree describing every capability the device exposes:
  - `system.func_list` — wired_lan, bluetooth_standby, etc.
  - `system.zone_num` — number of independently controllable zones (RX-A3070 has 3+ zones; WXA-50 returns 1).
  - `system.input_list[]` — every selectable input ID with three flags per input: `distribution_enable`, `rename_enable`, `account_enable`. **This is the canonical "is Qobuz here?" check.**
  - `zone[]` — per-zone func_list (power, volume, mute, sound_program, tone_control, link_control, …), input_list, sound_program_list, link_control_list, range_step (volume min/max/step).
  - `tuner`, `netusb`, `clock`, `distribution` subtrees with each one's preset count, recent_info count, valid functions, etc.
- Why a controller cares: this is the **one-shot capability probe** to drive UI feature toggling. Don't hard-code volume ranges, sound programs, or input lists — read them from here.
- One-shot Qobuz availability probe (recommended by Track D):
  ```bash
  curl -s http://{host}/YamahaExtendedControl/v1/system/getFeatures \
    | jq '.system.input_list[] | select(.id == "qobuz")'
  ```
  If you get an object back, Qobuz is a first-class input on that device. If empty, Qobuz isn't licensed/available on that hardware.

### `system/getStatus`

- Verb: GET
- Spec section: 4.x. Returns power/sleep/distribution status at the device level.
- **Caveat:** this returned `{"response_code":3}` (unsupported / wrong path) on the RX-A3070 in our probe. Most controllers use `<zone>/getStatus` instead (see below); `system/getStatus` per the spec exists but its actual surface is small. For a controller we generally don't need it — `<zone>/getStatus` carries the interesting data.

### `<zone>/getStatus`  (zone in {`main`, `zone2`, `zone3`, `zone4`})

- Verb: GET
- Returns: `power`, `sleep`, `volume`, `mute`, `max_volume`, `input`, `distribution_enable`, `sound_program`, `surr_decoder_type`, `pure_direct`, `enhancer`, `tone_control{}`, `dialogue_level`, `link_control`, `link_audio_delay`, `link_audio_quality`, `disable_flags`, `actual_volume{mode,value,unit}`, `contents_display`, `audio_select`, `party_enable`.
- Why a controller cares: the always-on "what's this zone doing right now" snapshot.
- Verified live response from 192.0.2.44 / `main`:

```json
{"response_code":0,"power":"standby","sleep":0,"volume":109,"mute":false,"max_volume":161,"input":"av1","distribution_enable":true,"sound_program":"surr_decoder","surr_decoder_type":"auto","pure_direct":false,"enhancer":false,"tone_control":{"mode":"auto","bass":0,"treble":0},"dialogue_level":0,"link_control":"standard","link_audio_delay":"audio_sync","link_audio_quality":"uncompressed","disable_flags":0,"actual_volume":{"mode":"db","value":-26.0,"unit":"dB"},"contents_display":false,"audio_select":"auto","party_enable":false}
```

### `<zone>/setPower`, `setVolume`, `setMute`, `setInput`

- All `GET` with query params:
  - `setPower?power={on|standby|toggle}`
  - `setVolume?volume={0..max}` or `?volume={up|down}` (with optional `&step=N`)
  - `setMute?enable={true|false}`
  - `setInput?input=<input_id>` (use the IDs returned by `getFeatures` — e.g. `qobuz`, `net_radio`, `airplay`, `spotify`, `usb`, `server`, `cd`, `tuner`, `hdmi1`, `bluetooth`, `mc_link`, `main_sync`)
  - Optional `&mode=autoplay_disabled` on `setInput` lets you change source without auto-resuming playback.
- `<zone>/prepareInputChange?input=<id>` — call this **before** `setInput` so the device tears down the previous source cleanly. The spec's worked example (Sec. 13.1.1) demonstrates this.

### `netusb/getPlayInfo`

- Verb: GET
- Returns the now-playing snapshot for whichever Net/USB-class input is current (server, net_radio, qobuz, spotify, airplay, bluetooth, usb, …).
- Fields:
  - `input` — current input ID
  - `playback` — `play` / `stop` / `pause` / `fast_reverse` / `fast_forward`
  - `repeat` — `off` / `one` / `all`
  - `shuffle` — `off` / `on` / `songs` / `albums`
  - `play_time` (sec, `-60000` = invalid), `total_time` (sec, `0` if N/A)
  - `artist`, `album`, `track`
  - `albumart_url` — relative path. Absolute = `http://{host}{albumart_url}`. ymf format is Yamaha-encrypted; on api_version ≥ 1.17 you also get jpg/png/bmp.
  - `albumart_id` — bumps when art changes; use to invalidate caches
  - `usb_devicetype` — `msc` / `ipod` / `unknown`
  - `auto_stopped` — true if Pandora/SiriusXM auto-stopped due to inactivity
  - `attribute` — bitfield of capabilities. Key bits for our build:
    - b[0] Playable, b[1] Stop, b[2] Pause, b[3] Prev, b[4] Next, b[5] FastRev, b[6] FastFwd, b[7] Repeat, b[8] Shuffle
    - b[18] Capable of Add Track (Qobuz/Pandora/Napster/JUKE)
    - b[19] Capable of Add Album (Qobuz/Napster/JUKE)
    - b[24] Capable of Link Distribution (multi-room)
    - b[25] Capable of Add Playlist (**Qobuz**)
  - `repeat_available[]`, `shuffle_available[]` — which values are valid for the current source (lets the UI grey out invalid options).
- Verified live response from 192.0.2.74 (WXA-50, Qobuz playing):

```json
{
  "response_code": 0,
  "input": "qobuz",
  "play_queue_type": "system",
  "playback": "stop",
  "repeat": "off",
  "shuffle": "on",
  "play_time": 0,
  "total_time": 1293,
  "artist": "Adam Schatz",
  "album": "Civil Engineering Vol. 1",
  "track": "A Pox On Your Upstairs Neighbors",
  "albumart_url": "",
  "albumart_id": 6904,
  "usb_devicetype": "unknown",
  "auto_stopped": false,
  "attribute": 84148639,
  "repeat_available": ["off", "one", "all"],
  "shuffle_available": ["off", "on"]
}
```

  Note `input: "qobuz"` — that confirms Qobuz is a first-class `netusb` input on the WXA-50, not behind a generic `streaming_service` wrapper. The `attribute` value `84148639` decodes to bits including b[25] Add Playlist (Qobuz) and b[18] Add Track (Qobuz).

### `netusb/setPlayback`

- `setPlayback?playback=<v>` where v ∈ `play` / `stop` / `pause` / `play_pause` / `previous` / `next` / `fast_reverse_start` / `fast_reverse_end` / `fast_forward_start` / `fast_forward_end`.

### `netusb/setPlayPosition?position=<seconds>`

- Server input only (DLNA). Not for Qobuz.

### `netusb/toggleRepeat`, `netusb/toggleShuffle`

- No discrete-set; the device cycles through valid values. Read back via `getPlayInfo`.

### `netusb/getListInfo`  ← **the browse endpoint**

- Verb: GET
- Params:
  - `input` (required) — the input ID whose tree you're browsing (e.g. `qobuz`, `server`, `net_radio`).
  - `index` (optional) — offset into list, must be a multiple of 8 (0, 8, 16, …, max 64992).
  - `size` (required) — how many rows to fetch this call, 1..8.
  - `lang` (optional) — `en` / `ja` / `fr` / `de` / `es` / `ru` / `it` / `zh`.
  - `list_id` (optional) — defaults to `main`. For Pandora's auto-complete/search the spec lists `auto_complete`, `search_artist`, `search_track`. **In practice the same `search_artist` / `search_track` / `search_album` list_ids are also used for Qobuz, JUKE, Napster, Rhapsody** — see the Search section below; the Basic spec only enumerates them for Pandora but the Qobuz capability bits in section 7.7 (b[20] Capable of Add Artist (Qobuz), b[15] Playlist (JUKE/Qobuz), etc.) plus aiomusiccast's wrappers confirm wider applicability. (uncertain on exact label set — verify with on-device probes, see verification probes section.)
- Response:
  - `menu_layer` (0..15) — depth in the tree
  - `max_line` (0..65000) — total rows in the current menu
  - `index` — echo of request offset
  - `playing_index` — row index of the now-playing element (-1 if nothing in this list is playing)
  - `menu_name` — display name of the current menu
  - `list_info[]` — array (≤ 8) of:
    - `text` — label
    - `thumbnail` — URL or "" (relative; resolve against `http://{host}` if not absolute)
    - `attribute` — bitfield. Bits a controller cares about:
      - b[0] Name exceeds max byte limit
      - b[1] Capable of Select (i.e. drillable into deeper layer)
      - b[2] Capable of Play (i.e. invocable via `setListControl?type=play`)
      - b[3] Capable of Search
      - b[4] Album Art available
      - b[5] Now Playing (Pandora)
      - b[15] Playlist (JUKE/Qobuz)
      - b[20]-[22] Capable of Add Artist/Remove Artist/Add Playlist (**Qobuz**)
      - b[23]-[26] Play Now / Play Next / Add Play Queue / Add MusicCast Playlist
- **Pagination quirk**: `size` is capped at 8. To page through a 40-row menu you call with `index=0&size=8`, then `index=8&size=8`, etc. up to `max_line`.
- **Blocking quirk** (Application Notes Sec. 13.1.6 footnote): `getListInfo` can take **up to 30 seconds** to populate after a layer change, and **blocks every other command on the device** while loading. Strategy: poll `getListInfo` until `response_code == 0` and `list_info` is populated, with a generous timeout. The official MusicCast app does exactly this.
- Spec error codes specific to list_info:
  - 100: Access Error (any Net/USB)
  - 112: Access Denied (Server)

### `netusb/setListControl`

- Verb: GET
- Params:
  - `list_id` (default `main`)
  - `type` (required) — `select` (descend into the indexed item), `play` (start playback of the indexed item), `return` (go back up one layer)
  - `index` — required for `select`/`play`. Offset within the current list (0..64999).
  - `zone` — optional, defaults to `main`. The zone whose input gets switched to the source as part of `play`.
- Notes:
  - For an item with `attribute & 0b110 == 0b110` (b[1] AND b[2] both set) you may either `select` it (drill in) or `play` it (start at this row) — useful for albums where you can either browse tracks or start playback at the album level.
  - To `select` an item with `attribute & b[3]` (Capable of Search), call `setSearchString` first to provide the search text. (See next section.)

### `netusb/setSearchString`

- Verb: **POST** (the only POST in the Basic spec).
- Body: JSON.
- Body params:
  - `list_id` (default `main`) — values per spec are `main`, `auto_complete`, `search_artist`, `search_track`. (uncertain) — based on aiomusiccast usage and Qobuz capability bits, additional `search_album` and possibly per-service variants exist on Qobuz-capable hardware. Confirm via probes.
  - `string` (required) — the search text.
  - `index` (optional, valid only when `list_id == "main"`) — element offset to drive the search at; behaves like a `setListControl?type=select` with the search text attached. If omitted, the call only stores the search text without changing layers.
- Example body (verbatim from spec):

```json
{ "list_id": "auto_complete", "string": "michael" }
```

- **How results come back**: `setSearchString` itself returns only `{"response_code":0}`. The actual search result is the new contents of the list — fetch it via a follow-up `netusb/getListInfo` call with the same `list_id`. Pattern:
  1. Navigate the Qobuz tree until you land on a "Search" node (attribute bit b[3] set).
  2. `POST setSearchString` with `{list_id: "main", string: "<query>", index: <search_node_index>}` — this both sets the text and selects the search node, descending one layer.
  3. `GET getListInfo?input=qobuz&index=0&size=8` to read the results.
  4. To narrow further (e.g. Qobuz's separate "by artist" / "by track" tabs) repeat the POST with `list_id: "search_artist"` or `list_id: "search_track"` and re-`getListInfo`.
- **aiomusiccast** wraps this exact dance in `MusicCastDevice.netusb_set_search()` — that's the canonical reference implementation. Home Assistant's `yamaha_musiccast` integration imports it but doesn't surface search to the user; we will.
- **Quirk**: `setSearchString` only operates against an input that's currently selected (the spec implies this and the community confirms it). So before searching Qobuz: call `main/prepareInputChange?input=qobuz` then `main/setInput?input=qobuz`, *then* navigate to the search node, *then* POST.

### `netusb/getPresetInfo` / `recallPreset` / `storePreset` / `clearPreset` / `movePreset`

- Presets are common across all Net/USB inputs; the count comes from `system/getFeatures` → `netusb.preset.num` (40 on both our receivers).
- `recallPreset?zone=main&num=N` — instantly switch to preset N in the named zone.
- `storePreset?num=N` stores whatever's currently playing into slot N.
- Verified preset list on 192.0.2.44 (excerpt — first 6 slots are populated, mostly with Qobuz items):

```json
{"response_code":0,"preset_info":[
  {"input":"qobuz","text":"Época","attribute":30},
  {"input":"server","text":"Super Magic 2000","attribute":30},
  {"input":"server","text":"Down the Road","attribute":30},
  {"input":"qobuz","text":"Y' a pas d'arrangement","attribute":30},
  {"input":"qobuz","text":"L'homme pressé","attribute":30},
  {"input":"qobuz","text":"Grace Kelly","attribute":30},
  {"input":"unknown","text":""}, ... 33 more empty slots ...
],"func_list":["clear","move"]}
```

  This is strong evidence that Qobuz-on-MusicCast is preset-friendly: a controller that just exposes "play preset N in zone Z" already covers a real-world use case for many users.

### `netusb/getRecentInfo` / `recallRecentItem` / `clearRecentInfo`

- Last-N playback history across all Net/USB inputs (40 slots on our hardware). Each entry has `input`, `text`, `albumart_url`, `play_count`, `attribute`. `recallRecentItem?zone=main&num=N` jumps right back to that item.

### `netusb/getPlayDescription`

- Pandora-only `why_this_song` explainer in the Basic spec; not useful for Qobuz today.

### `netusb/getSettings` / `setQuality`

- For Qobuz specifically: `getSettings` returns `qobuz.quality.value` (current) and `qobuz.quality.value_list[]` (selectable: `hr_192_24` / `hr_96_24` / `cd_44_16` / `mp3_320`). `setQuality?input=qobuz&value=hr_192_24` chooses one. Worth surfacing in the controller — it's a per-account streaming-quality toggle that the official app exposes.

### `netusb/getAccountStatus` / `switchAccount` / `getServiceInfo`

- Lists which streaming services have stored credentials, their login_status (`logged_in`, `account_expired`, `invalid_account`, …), and `type` (`formal`, `trial`, `unpaid`, `expired`).
- Useful for the controller's settings/diagnostics screen ("Qobuz session: logged_in"). We **cannot** log in or change credentials over YXC — that's done through the MusicCast iOS/Android app or the device's web UI.

### `tuner/*`

- AM, FM, RDS, DAB, HD-Radio. Endpoints: `getPresetInfo`, `getPlayInfo`, `setBand`, `setFreq`, `recallPreset`, `switchPreset`, `storePreset`, `clearPreset`, `startAutoPreset`. Out of scope for our Qobuz-focused build, but trivial to wire up later.

### `cd/*`

- For receivers with a CD slot. `getPlayInfo`, `setPlayback`, `toggleTray`, `toggleRepeat`, `toggleShuffle`. RX-A3070 has no CD; WXA-50 has no CD; ignore.

## Now-playing data shape

Concrete real example from 192.0.2.74 with Qobuz mid-playlist (annotated):

```json
{
  "response_code": 0,
  "input": "qobuz",                                  // current source
  "play_queue_type": "system",                       // reserved/internal
  "playback": "stop",                                // play | stop | pause | fast_reverse | fast_forward
  "repeat": "off",                                   // off | one | all
  "shuffle": "on",                                   // off | on | songs | albums
  "play_time": 0,                                    // seconds elapsed (-60000 == invalid)
  "total_time": 1293,                                // seconds total (0 if N/A — e.g. radio)
  "artist": "Adam Schatz",
  "album": "Civil Engineering Vol. 1",
  "track": "A Pox On Your Upstairs Neighbors",
  "albumart_url": "",                                // relative; "" when none. Prefix http://{host} for absolute.
  "albumart_id": 6904,                               // bumps on art change; cache-buster
  "usb_devicetype": "unknown",                       // msc | ipod | unknown
  "auto_stopped": false,                             // true if Pandora/SiriusXM auto-stopped on inactivity
  "attribute": 84148639,                             // bitfield; bits documented under getPlayInfo above
  "repeat_available": ["off", "one", "all"],         // valid values for the repeat enum on this source
  "shuffle_available": ["off", "on"]                 // valid values for the shuffle enum on this source
}
```

For the controller's "currently playing" panel we need: `track`, `artist`, `album`, `albumart_url` (resolved absolute), `play_time` / `total_time` (for the progress bar), `playback` (transport state), `repeat` / `shuffle` plus their `*_available` companions (UI grey-out), and the `attribute` bitfield (which control buttons to enable). For the **progress bar** we should consume the `play_time` event push (issued every second by the receiver while `playback == "play"`) rather than polling — see Push events below.

## Browsing NetUSB / streaming services

The list model is a tree-of-menus. Layer 0 is the service's root menu; each `select` descends one layer; `return` ascends one. The controller carries no client-side state — the **server** holds the cursor (current layer + position) on a per-input basis.

End-to-end walkthrough: "user navigates from root to a Qobuz playlist's tracks and plays one":

1. Switch to Qobuz on main zone (preflight + commit):
   ```
   GET /YamahaExtendedControl/v1/main/prepareInputChange?input=qobuz
   GET /YamahaExtendedControl/v1/main/setInput?input=qobuz
   ```
2. Read the root menu of Qobuz (max 8 rows at a time):
   ```
   GET /YamahaExtendedControl/v1/netusb/getListInfo?input=qobuz&index=0&size=8&lang=en
   ```
   Response will look like (typical Qobuz root):
   ```json
   {"response_code":0,"menu_layer":0,"max_line":7,"index":0,"playing_index":-1,
    "menu_name":"Qobuz",
    "list_info":[
      {"text":"My playlists","thumbnail":"","attribute":2},
      {"text":"Favorite albums","thumbnail":"","attribute":2},
      {"text":"Favorite tracks","thumbnail":"","attribute":2},
      {"text":"Favorite artists","thumbnail":"","attribute":2},
      {"text":"New releases","thumbnail":"","attribute":2},
      {"text":"Editor's picks","thumbnail":"","attribute":2},
      {"text":"Search","thumbnail":"","attribute":10}
    ]}
   ```
   `attribute: 2` = b[1] Selectable. `attribute: 10` = b[1] Selectable + b[3] Capable of Search.

3. Drill into "My playlists" (index 0):
   ```
   GET /YamahaExtendedControl/v1/netusb/setListControl?list_id=main&type=select&index=0
   ```
   The server moves its cursor; the response is `{"response_code":0}`.
4. Read the new layer (your playlists):
   ```
   GET /YamahaExtendedControl/v1/netusb/getListInfo?input=qobuz&index=0&size=8
   ```
   Returns `menu_layer:1`, `menu_name:"My playlists"`, `max_line: <count>`, list of playlists each with `attribute: 6` (b[1] Select + b[2] Play — you can either drill in to see tracks or hit play on the playlist itself).
5. Drill into a specific playlist (say index 3): `setListControl?type=select&index=3`. Then `getListInfo` again — `menu_layer:2`, `menu_name:"<playlist name>"`, list of tracks each with `attribute: 4` (b[2] Play only).
6. Play track at index 5: `setListControl?type=play&index=5&zone=main`. The receiver starts playback; subsequent `getPlayInfo` returns the live track metadata.
7. Back out one layer: `setListControl?type=return`.

**Pagination**: any layer with `max_line > 8` requires multiple `getListInfo` calls bumping `index` by 8 each time (8, 16, 24, …). The `index` parameter MUST be a multiple of 8.

**Concurrency caveat** (spec Sec. 13.1.6 footnote): `getListInfo` can take up to 30 seconds and **blocks every other command on the device** while loading. Treat list-loading as a serialised operation in the controller — don't fire other commands at the same receiver until it returns.

## Search

Search is a real first-class feature, expressed through three coordinated endpoints:

1. `setSearchString` (POST `{list_id, string, index?}`) sets the search text for a list_id and optionally selects an indexed search node simultaneously.
2. `getListInfo` (GET) reads back the populated results.
3. `setListControl` then drives drilldown / playback as normal.

**list_id values for search**:
- `main` (with `index` set to a search node) — the generic case, equivalent to "I'm at a layer, I want to type into the search box at row N and drill in".
- `auto_complete` — type-ahead suggestions while the user is still typing (Pandora; aiomusiccast also exposes for other services).
- `search_artist` / `search_track` — the spec explicitly lists these for Pandora; aiomusiccast (the lib HA uses) wraps them generically. (uncertain) Whether `search_album`, `search_playlist` etc. exist on Qobuz-capable hardware is not in the Basic spec but is consistent with the Qobuz-specific capability bits in section 7.7. Verify on-device with the probes below.

**Quirks**:
- Search only operates against the currently-selected input. Switch to Qobuz first.
- `setSearchString` body is JSON. It is the only POST endpoint in the Basic spec — every other "set" is GET-with-querystring.
- Results don't come back in the POST response; you must follow up with `getListInfo`. Watch out for the 30-second block window.
- aiomusiccast's `MusicCastDevice.netusb_set_search(input, list_id, string, index=None)` is the cleanest reference (Track C found this).
- Home Assistant's `yamaha_musiccast` integration imports aiomusiccast but doesn't expose search to the user — so we'd be filling a real gap.

## Push events / subscriptions

Receivers UDP-unicast event JSON to a controller-declared port. There is no separate "subscribe" call — you simply add two custom HTTP request headers to any YXC request and the receiver remembers your IP+port for 10 minutes:

```
X-AppName: MusicCast/1.0(linux)
X-AppPort: 41100
```

- Transport: UDP unicast to the IP the request came from, on the port declared in `X-AppPort`.
- Subscription lifetime: 10 minutes after the last request from that IP. Any subsequent request from the same IP refreshes it. To stay subscribed permanently, send a no-op request (e.g. `system/getDeviceInfo`) every few minutes.
- A different `X-AppPort` from the same IP overwrites the previous registration.
- Payload: a JSON object with only the deltas. Top-level keys correspond to services/zones (`system`, `main`, `zone2`, `tuner`, `netusb`, `cd`, `dist`, `clock`) plus a top-level `device_id`. Each delta is either a literal new value (e.g. `power: "on"`, `volume: 30`) or a `<thing>_updated: true` flag instructing the controller to re-fetch via the named GET (e.g. `name_text_updated: true` → call `system/getNameText`).
- Spec event example:

```json
{
  "system": { "name_text_updated": true },
  "main": {
    "power": "on", "input": "siriusxm", "volume": 30, "mute": false,
    "status_updated": true
  },
  "zone2": { "power": "on", "input": "cd", "volume": 50, "mute": false, "enhancer": false },
  "tuner": { "play_info_updated": false },
  "netusb": {
    "play_error": 0,
    "account_updated": true,
    "play_time": 50,
    "trial_status": { "input":"siriusxm", "enable":false },
    "trial_time_left": { "input":"siriusxm", "time": 5 },
    "play_info_updated": false,
    "list_info_updated": false
  },
  "cd": { "tray_status":"ready", "play_time":100, "play_info_updated":false },
  "device_id": "AC44F243C7AD"
}
```

- Key `*_updated` flags a controller will care about:
  - `main.status_updated` → re-fetch `main/getStatus`
  - `main.signal_info_updated` → re-fetch `main/getSignalInfo`
  - `netusb.play_info_updated` → re-fetch `netusb/getPlayInfo`
  - `netusb.list_info_updated` → re-fetch `netusb/getListInfo` (lets the UI auto-refresh a browse pane when the user navigates from the device's IR remote or another controller)
  - `netusb.preset_info_updated`, `netusb.recent_info_updated` → re-fetch the matching list
  - `netusb.account_updated` → re-fetch `netusb/getAccountStatus`
  - `tuner.play_info_updated`, `tuner.preset_info_updated`
  - `system.func_status_updated`, `system.bluetooth_info_updated`, `system.name_text_updated`, `system.location_info_updated`
  - `cd.play_info_updated`
  - `dist.dist_info_updated`
  - `clock.settings_updated`
- Direct values pushed without an `_updated` flag (no follow-up fetch needed):
  - `<zone>.power`, `<zone>.input`, `<zone>.volume`, `<zone>.mute`
  - `netusb.play_time` (pushed every second while playing — drives the progress bar)
  - `netusb.play_error`, `netusb.multiple_play_errors`, `netusb.play_message`
  - `cd.tray_status`, `cd.play_time`
- `play_error` codes worth toasting in the UI (Qobuz-specific in italics):
  - 1: Access Error / 2: Playback Unavailable / 3: Skip Limit Reached / 4: Invalid Session / 5: High-Res Not Playable at MusicCast Leaf
  - *6: User Uncredentialed (Qobuz)*
  - *7: Track Restricted by Right Holders (Qobuz)*
  - *8: Sample Restricted (Qobuz)*
  - *9: Genre Restricted (Qobuz)*
  - *10: Application Restricted (Qobuz)*
  - *11: Intent Restricted (Qobuz)*

## Qobuz on YXC

**Qobuz is a first-class `netusb` input.** Not a separate service, not under a generic `streaming_service` wrapper. Confirmed three ways:

1. **`system/getFeatures`** lists `qobuz` in `system.input_list[]` on Qobuz-capable hardware (RX-A3070 destination "BG", WXA-50 destination "BG"; both have it — Qobuz availability is region-gated by the `destination` field at the firmware level).
2. **`netusb/getPlayInfo`** returns `"input": "qobuz"` when Qobuz is the current source (verified live on 192.0.2.74).
3. **`netusb/getSettings`** returns a `qobuz` object with a `quality` enum specific to Qobuz (`hr_192_24` / `hr_96_24` / `cd_44_16` / `mp3_320`). No other input has its own settings sub-object in the spec.

What this means for the build:
- All of `getPlayInfo`, `getListInfo`, `setListControl`, `setSearchString`, `getRecentInfo`, `recallPreset`, `storePreset`, `manageList` (with `add_track` / `add_album` / `add_artist` / `add_playlist` / `remove_*`), and `manageDist` (link distribution / multi-room) work with `input=qobuz` like any other Net/USB source.
- Qobuz-specific affordances surfaced through capability bits: `getPlayInfo.attribute b[18]` = Add Track to Qobuz favourites, `b[19]` = Add Album, `b[25]` = Add Playlist; `getListInfo.list_info[].attribute b[20]/[21]/[22]` = Add Artist / Remove Artist / Add Playlist.
- Streaming-quality control: `setQuality?input=qobuz&value=hr_192_24`.
- Account: `getAccountStatus` reports `qobuz.login_status: logged_in/logged_out/access_error/invalid_account/account_expired`.
- **Cannot do via YXC**: log in / log out of Qobuz, change Qobuz credentials. That stays in the official MusicCast app.

## Verification probes (run on the desktop with the receivers reachable)

These confirm the user's specific RX-A3070 (192.0.2.44) and WXA-50 (192.0.2.74) capabilities. Each command writes to a temp file and reports JSON via `jq`.

```bash
# Sanity check: both receivers respond
curl -s --max-time 5 http://192.0.2.44/YamahaExtendedControl/v1/system/getDeviceInfo | jq .model_name
curl -s --max-time 5 http://192.0.2.74/YamahaExtendedControl/v1/system/getDeviceInfo | jq .model_name

# Is Qobuz a first-class input?  (Track D's canonical probe)
curl -s http://192.0.2.44/YamahaExtendedControl/v1/system/getFeatures \
  | jq '.system.input_list[] | select(.id == "qobuz")'
curl -s http://192.0.2.74/YamahaExtendedControl/v1/system/getFeatures \
  | jq '.system.input_list[] | select(.id == "qobuz")'

# What's playing right now?
curl -s http://192.0.2.44/YamahaExtendedControl/v1/netusb/getPlayInfo | jq .
curl -s http://192.0.2.74/YamahaExtendedControl/v1/netusb/getPlayInfo | jq .

# Per-zone status (use main; RX-A3070 also has zone2/zone3)
curl -s http://192.0.2.44/YamahaExtendedControl/v1/main/getStatus | jq .
curl -s http://192.0.2.74/YamahaExtendedControl/v1/main/getStatus | jq .

# Qobuz account status (only meaningful if Qobuz is in input_list)
curl -s http://192.0.2.44/YamahaExtendedControl/v1/netusb/getAccountStatus \
  | jq '.service_list[] | select(.id == "qobuz")'

# Streaming-quality currently selected for Qobuz
curl -s http://192.0.2.44/YamahaExtendedControl/v1/netusb/getSettings | jq .qobuz

# List 40 presets, show only the populated ones with their input source
curl -s http://192.0.2.44/YamahaExtendedControl/v1/netusb/getPresetInfo \
  | jq '.preset_info | to_entries[] | select(.value.input != "unknown") | {slot: (.key+1), input: .value.input, text: .value.text}'

# Switch main zone to Qobuz, then read the Qobuz root menu (8 rows)
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/main/prepareInputChange?input=qobuz" | jq .
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/main/setInput?input=qobuz" | jq .
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/netusb/getListInfo?input=qobuz&index=0&size=8&lang=en" | jq .

# Probe search: search for "miles davis" against the Qobuz tree
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"list_id":"search_artist","string":"miles davis"}' \
  http://192.0.2.74/YamahaExtendedControl/v1/netusb/setSearchString | jq .
curl -s "http://192.0.2.74/YamahaExtendedControl/v1/netusb/getListInfo?input=qobuz&index=0&size=8&lang=en" | jq .

# Subscribe to push events: open a UDP listener on :41100, then send any request with the headers
( command -v nc >/dev/null && nc -u -l 41100 ) &
curl -s -H 'X-AppName: MusicCast/1.0(linux)' -H 'X-AppPort: 41100' \
  http://192.0.2.74/YamahaExtendedControl/v1/system/getDeviceInfo > /dev/null
# Now nudge the device (e.g. press a button on the front panel or via the app) and watch nc.
```

The third probe (`netusb/getPlayInfo` on .74) was already run during research and returned `input: "qobuz"`, confirming Qobuz is live on the WXA-50. The first probe on .44 returned `input: "av1"` (AV receiver was watching its physical AV input at probe time) — that doesn't tell us whether Qobuz is licensed there, hence the `getFeatures` check.

## Gaps / unknowns

- **`search_album` / per-service search list_ids**: the Basic spec only documents `search_artist`, `search_track`, `auto_complete` (and lists them as Pandora-only). Qobuz capability bits hint at richer search list_ids but they're not enumerated. **Probe via the verification commands above and capture which list_ids the receiver accepts without 4xx-equivalent response codes.**
- **Browse semantics under Qobuz vs Spotify**: Spotify on YXC is largely a thin "see status, send transport commands" wrapper (Spotify Connect handles browsing in the Spotify app). Qobuz is fully browseable through `getListInfo`. Other services (Tidal, Deezer, Amazon Music) — not in scope for this project, behaviour likely varies per service.
- **Advanced spec coverage**: the Yamaha "Advanced" spec PDF (saved as `yxc-api-spec-advanced.pdf` in this folder, also Rev 1.x from Yamaha 2016) covers MusicCast Link / link distribution (`/dist/*`), stereo pairing, and a few other advanced features. We didn't dig into it for this research because it's out of scope for "see what's playing + browse Qobuz + play". When we want multi-room sync, that's the next read.
- **Rate limits / DoS thresholds**: spec is silent. Community wisdom: don't poll faster than 1 Hz when not subscribed; with push events, no polling needed.
- **Qobuz region/destination gating**: `destination: "BG"` (Europe) on both user receivers. Whether `destination: "U"` (US) hardware exposes Qobuz is uncertain; not relevant for this user.
- **HTTP/2 or keepalive**: spec mandates nothing. Treat each request as standalone HTTP/1.1.
- **Concurrent zone control**: not explicitly forbidden, but the `getListInfo` 30-second block applies device-wide, not per-zone.

## Sources

- **YXC API Specification (Basic) Rev. 1.10** — Yamaha 2016, 104 pages. Saved locally at `/workspace/CLAUDE/Plan/00038-musiccast-controller/yxc-api-spec-basic.pdf`. Source: <https://community-openhab-org.s3-eu-central-1.amazonaws.com/original/2X/9/931ea88e30cf0f05fcdee79816eb4d3f12dd4d70.pdf>
- **YXC API Specification (Advanced)** — Yamaha. Saved locally at `/workspace/CLAUDE/Plan/00038-musiccast-controller/yxc-api-spec-advanced.pdf`. Source: <https://community.symcon.de/uploads/short-url/vRXaJXAn6vI2DSQYMHF0aqLbdir.pdf>
- **MusicCast HTTP simplified API for Control Systems V1.1** (June 20, 2017): <https://forum.smartapfel.de/attachment/4358-yamaha-musiccast-http-simplified-api-for-controlsystems-pdf/> — shorter integrator-targeted version of the same protocol.
- **pyamaha** — Python implementation of the YXC API. Useful as a "what subset is actually wired up": <https://github.com/rsc-dev/pyamaha>
- **aiomusiccast** — async Python wrapper used by Home Assistant. Canonical reference for `setSearchString` usage and the event-loop pattern: <https://github.com/vigonotion/aiomusiccast>
- **Home Assistant `yamaha_musiccast` integration** — shows which endpoints HA actually calls (and which it ignores, like search): <https://github.com/home-assistant/core/tree/dev/homeassistant/components/yamaha_musiccast>
- **honnel/yamaha-commands** — list of empirically-discovered commands: <https://github.com/honnel/yamaha-commands>
- **karlentwistle/music_cast** (Ruby): <https://github.com/karlentwistle/music_cast>
- **samvdb/php-musiccast-api** (PHP): <https://github.com/samvdb/php-musiccast-api>
- **Live probe data from user's receivers**: 192.0.2.44 (RX-A3070, BG, system_version 2.87, api_version 2.08), 192.0.2.74 (WXA-50, BG, system_version 2.86, api_version 2.08) — captured during this research and embedded throughout this document.
