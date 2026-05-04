# Qobuz to MusicCast: Architectural Options

## TL;DR

**Native Qobuz over YXC (Path 1) is the holy-grail option and almost certainly the right answer for our two receivers.** Yamaha shipped Qobuz as a native MusicCast streaming service in May 2019 via firmware update across the RX-A 80, RX-V 85, RX-S602, the R-N1000A/R-N2000A, MusicCast 20/50 speakers, and others; on supported models, `qobuz` becomes a netusb input that the receiver itself logs into and streams from Qobuz cloud at up to 24/192 FLAC, with browse/search/play exposed through the generic YXC `netusb` endpoints. If our two receivers (192.0.2.44, 192.0.2.74) report `qobuz` in `system/getFeatures.input_list`, our controller is a thin UI over YXC and we're done. If they do not, the realistic fallback is **Path 2 (DLNA renderer push from upmpdcli with the Qobuz plugin)**, which preserves lossless and gives us a browse tree but adds a daemon. AirPlay (Path 3) is lossy 16/44.1 and a poor fit for a Qobuz controller. Qobuz Connect (Path 4) is the future winner if Yamaha joins, but as of May 2026 they have not. MPD/mopidy-qobuz (Path 5) is technically possible but the maintained pieces overlap heavily with upmpdcli, so it just adds moving parts.

## Path 1: Native Qobuz on MusicCast (YXC)

**Receiver-model / firmware requirements**

Yamaha and Qobuz announced native Qobuz integration on May 14, 2019 as a free firmware update for the following MusicCast products: MusicCast 20, MusicCast 50, MusicCast BAR 400, RX-A x80 series AV receivers (RX-A480/A780/A880/A1080/A2080/A3080), RX-V x85 series AV receivers, RX-S602 slimline AVR, ATS-4080 sound bar, TSR-7850 AVR, CX-A5200 preamp, XDA-QS5400 streaming amp, MusicCast VINYL 500. Newer streaming-focused units (R-N803D, R-N1000A, R-N2000A, NP-S303) also support Qobuz natively. All quote up to 24-bit/192 kHz playback, but multi-room linked playback downsamples slaves to 48 kHz — only the master plays at native rate. ([Sound & Vision](https://www.soundandvision.com/content/yamaha-musiccast-products-can-now-stream-qobuz), [Qobuz Community 2019-06-03](https://community.qobuz.com/news-en/2019/06/03/qobuz-announces-integration-with-yamaha-musiccast-free-firmware-update), [Hi-Res Audio Online](https://www.hiresaudio.online/qobuz-integrates-with-yamaha-musiccast/))

Pre-2018 receivers (RX-V81 series and earlier) generally **do not** get Qobuz; the firmware update only targets units with the newer net-module hardware. ([Polk forum thread](https://forum.polkaudio.com/discussion/187111/yamaha-quobuz-support-for-models-made-before-2018))

**How to detect on our specific units**

Run a one-shot probe per receiver:

```bash
curl -s http://192.0.2.44/YamahaExtendedControl/v1/system/getFeatures \
  | jq '.system.input_list[] | select(.id == "qobuz")'
curl -s http://192.0.2.74/YamahaExtendedControl/v1/system/getFeatures \
  | jq '.system.input_list[] | select(.id == "qobuz")'
```

If the input is present, the response contains `{"id":"qobuz","distribution_enable":true,"rename_enable":false,"account_enable":true,"play_info_type":"netusb"}`. The `account_enable: true` flag means the receiver expects to log into Qobuz with stored credentials (entered via the MusicCast app once); `play_info_type: netusb` means we use the standard `/v1/netusb/*` endpoints. ([YXC Basic spec, sections 4.2 and 7](/workspace/CLAUDE/Plan/00038-musiccast-controller/yxc-api-spec-basic.pdf), and search results confirming `qobuz` is a recognised input ID.)

**YXC subtree shape (for confirmation, not duplication)**

YXC does not expose a Qobuz-specific endpoint subtree. Qobuz is an *input* under netusb, and browsing/searching/playing it goes through the generic netusb endpoints that Track A documents in detail:

- `setInput?input=qobuz&zone=main` — switch the zone to Qobuz (after which the receiver enters the Qobuz menu's root).
- `netusb/getListInfo?input=qobuz&index=0&size=8&lang=en` — page through the current browse list (8 items at a time per the spec). Returns titles, attributes (selectable/playable/searchable), and album art.
- `netusb/setListControl?type=select&index=N` — descend into a folder/category (list_id auto-advances).
- `netusb/setListControl?type=play&index=N` — play the selected track/album.
- `netusb/setSearchString` — POST `{"list_id":"auto_complete","string":"radiohead"}` to populate search; pair with `setListControl?type=select` against an item with the "Capable of Search" attribute.
- `netusb/getPlayInfo` — current track metadata, album art URL, position, duration, repeat/shuffle.
- `netusb/setPlayback?playback=play|pause|stop|next|previous` — transport control.
- `netusb/getAccountStatus` / `netusb/switchAccount` — confirm/swap the stored Qobuz account.
- Event subscription via UDP push (`X-AppName` header on first request) gives us live now-playing updates without polling.

(All from the YXC Basic spec sections 7.1–7.25; cross-referenced with [openHAB-hosted YXC PDF](https://community-openhab-org.s3-eu-central-1.amazonaws.com/original/2X/9/931ea88e30cf0f05fcdee79816eb4d3f12dd4d70.pdf) and [yamaha-commands README](https://github.com/honnel/yamaha-commands/blob/master/README.md).)

**UX completeness: full**

- Browse: full tree (Favourites, Playlists, My Albums, Editor's Picks, Genres, etc. — exactly what the MusicCast app shows for Qobuz, because the receiver is generating that menu locally).
- Search: full text search via `setSearchString` against the searchable nodes.
- Now playing: full metadata + album art, with event push.
- Multi-zone: streams to either receiver independently; group both with `dist/*` endpoints if desired (with the 48 kHz slave caveat).
- The receiver shows what's playing on its own front panel and via the Yamaha app — we don't have to drive that.

**Limitations**

- Single Qobuz session per receiver: while we're playing on .44, the .74's Qobuz session uses the *same Qobuz account* (Qobuz allows N concurrent streams depending on plan; Studio plan typically allows 1, Studio Sublime 1, Family up to 6). Same constraint as any other Qobuz client.
- Multi-room sync (MusicCast Link) downsamples to 48 kHz on slave zones.
- We're at the mercy of Yamaha's firmware: if they decide to stop renewing the Qobuz partnership tokens (as has happened to other vendors with other services), the input quietly stops working. There is no upstream maintenance we can do.
- The list-paging UX is pre-Qobuz-Connect-era: 8 items per page, a `list_id` cursor that gets stale on session change, and refreshes triggered by `setInput`. Track A will document the rough edges.

**Verdict: top-ranked.** If `getFeatures` confirms `qobuz` on both receivers, build on this and stop looking elsewhere.

## Path 2: DLNA renderer mode (audio push from desktop)

**MusicCast as a DLNA renderer — confirmed**

Yamaha MusicCast devices are bona fide UPnP/DLNA renderers (DMR class). They accept FLAC/WAV/AIFF up to 24/192 and ALAC up to 24/96 from any UPnP push controller. Many users run Plex/JRiver/BubbleUPnP into a MusicCast unit at full hi-res. ([Yamaha MusicCast FAQ](https://usa.yamaha.com/products/contents/audio_visual/musiccast/musiccast-faqs.html), [Audiophonics R-N402D specs](https://www.audiophonics.fr/en/integrated-amplifiers/yamaha-musiccast-r-n402d-amplifier-streamer-wifi-airplay-dlna-bluetooth-2x115w-4-ohm-24bit-192khz-dsd128-p-15758.html))

So the question is purely: **what desktop-side software gives us a Qobuz-aware UPnP push pipeline?**

**qobuz-player (SofusA) DLNA push — no**

The repo notice says it has been moved to [qobine](https://github.com/SofusA/qobine). Both qobuz-player and qobine use a GStreamer pipeline whose sink is local audio (ALSA/PulseAudio); the stated dependencies include `alsa-sys-devel` and the only documented outputs are local audio + a web UI on port 9888 + MPRIS for D-Bus control. No DLNA, no UPnP, no AirPlay output is documented. ([qobine README](https://github.com/SofusA/qobine), [qobuz-player README](https://github.com/SofusA/qobuz-player/blob/main/README.md))

In principle one could swap the GStreamer audio sink for a `rtpL16pay`/HTTP-streaming sink and feed it to a UPnP renderer, but that's a fork, not a feature.

**hifi-rs (iamdb) DLNA push — no**

Same architecture: GStreamer-backed local player ("GStreamer-backed player, SQLite database"). README emphasises gapless local playback and MPRIS; no networked-output support documented. ([hifi.rs README](https://github.com/iamdb/hifi.rs))

**upmpdcli + Qobuz plugin — yes, this is the realistic path**

upmpdcli is the canonical Linux UPnP renderer that wraps MPD, and it ships **media-server gateway plugins** for Tidal, **Qobuz**, HighResAudio, Subsonic. The plugin exposes Qobuz as a browsable UPnP MediaServer tree (Favorites, Playlists, search, etc.) and upmpdcli itself acts as an OpenHome-compatible MediaRenderer that any UPnP control point can drive. As of upmpdcli 1.9.17 (April 2026), Qobuz support has been refreshed: "1.9.17 : Qobuz support is back, you will need to update the connection." ([upmpdcli home](https://www.lesbonscomptes.com/upmpdcli/index.html), [GioF71/upmpdcli-docker](https://github.com/GioF71/upmpdcli-docker))

Architecture variants:

- **Variant A (renderer-only):** run upmpdcli on the desktop as a local DMR. Use a UPnP control point to push the desktop's MPD output to one of the MusicCast receivers. Doesn't get us Qobuz browsing.
- **Variant B (gateway):** run upmpdcli with the Qobuz media-server plugin. Our controller talks to upmpdcli's UPnP MediaServer (or just to MPD directly), browses Qobuz, and uses standard UPnP `AVTransport.SetAVTransportURI` against the MusicCast renderer to push the chosen track URL. **This is the variant that makes sense.** Lossless 24/192 FLAC carries cleanly over DLNA; the receiver decodes it natively.

There's a long tail of community recipes for this exact pattern (LMS/Squeezelite + qobuz plugin + UPnP bridge, BubbleUPnPServer + Qobuz, etc.). All work; all add 1–2 daemons. ([Lyrion forums on LMS+Qobuz+UPnP](https://forums.lyrion.org/forum/user-forums/logitech-media-server/110940-lms-qobuz-and-upnp), [Hifizine "Stream Qobuz to anything"](https://www.hifizine.com/2019/07/stream-qobuz-to-anything/), [Linux Audio Foundation MusicLounge guide](https://linuxaudiofoundation.org/musiclounge-upnp-renderer-local-qobuz-tidal-and-others/))

**Bitrate / lossless: yes**

DLNA over HTTP can carry FLAC up to 24/192 to a MusicCast renderer with no transcoding. Confirmed by the published format-support matrix. ([Yamaha FAQ](https://usa.yamaha.com/products/contents/audio_visual/musiccast/musiccast-faqs.html))

**UX completeness: full but two-sided**

- Browse/search: lives in our controller, fed by upmpdcli's Qobuz media-server gateway (or by talking to the Qobuz Web API directly and turning track IDs into stream URLs ourselves).
- Now-playing on the receiver: the receiver shows generic UPnP "server" track metadata. The Yamaha front panel and MusicCast app show track name/album/art, but the input shows as `server` (or `mc_link`), not `qobuz`. Functional, slightly less integrated.
- One Qobuz session caveat applies as in Path 1.

**Verdict: ranked second; the fallback if Path 1 fails.** Real, lossless, maintained, and we keep all the UX in our controller. Cost: extra daemon (upmpdcli + MPD), extra Qobuz-account-token rot risk in the upmpdcli plugin (the project has had two prior Qobuz-token outages, both resolved).

## Path 3: AirPlay sender from Linux

**Hardware support**

Most MusicCast receivers from RX-Vx85/RX-Ax80 onward, and the MusicCast 20/50/BAR-400, support AirPlay 2 via the same 2019 firmware wave. ([9to5Mac Yamaha AirPlay-2 list](https://9to5mac.com/2019/04/15/yamaha-airplay-2-updates/), [Yamaha hub article](https://hub.yamaha.com/audio/a-how-to/how-to-use-airplay-2-with-musiccast/))

**Linux as an AirPlay 2 sender**

This is the sticking point. shairport-sync — the canonical Linux project for AirPlay — is **receive-only**: "Shairport Sync only works as an audio player (target), and it's not possible to use Shairport Sync as a source." ([shairport-sync README](https://github.com/mikebrady/shairport-sync))

There is one credible sender option: [music-assistant/cliairplay](https://github.com/music-assistant/cliairplay), a CLI that wraps the owntone stack to push audio to AirPlay 2 devices. It is built and shipped as a binary by GitHub Actions, but has no tagged releases and is purpose-built to be invoked by the music-assistant project (i.e. it expects to be fed audio from Music Assistant, not from a Qobuz-aware sender we'd write).

The historical alternative — pulseaudio-raop2 — handles **AirPlay 1** only and is largely abandoned in the PipeWire era. ([pulseaudio-raop2](https://hfujita.github.io/pulseaudio-raop2/))

**Bitrate cap — the dealbreaker**

AirPlay 1 is 16-bit/44.1 kHz ALAC. AirPlay 2 nominally allows the same in audio-only mode (16/44.1 ALAC), with hi-res only between Apple-approved endpoints. From a Linux Qobuz sender, the practical cap is 16/44.1 — i.e. we'd be downsampling 24/192 Qobuz Studio content to CD quality just to use AirPlay. ([Qobuz "compatible devices" article](https://www.qobuz.com/us-en/magazine/story/Qobuz-Vous/Qobuz-Streaming-the-compatible179461/))

**UX completeness: middling**

- Browse/search lives entirely in our controller.
- Now-playing on the receiver: AirPlay 2 sends limited metadata; receiver shows track/artist if our sender forwards it; the input shows as `airplay`.
- Multi-room sync via AirPlay 2 works across receivers, but we lose the YXC `mc_link` group machinery.

**Verdict: ranked low.** The Linux-sender story is fragile (one half-maintained CLI), and the bitrate cap nullifies the whole point of subscribing to Qobuz Studio. Worth knowing as an emergency Plan C, not a primary path.

## Path 4: Qobuz Connect

**Does it exist?**

Yes. Qobuz Connect — the analogue of Spotify Connect, where the device pulls bit-perfect from Qobuz's cloud and the phone/desktop is a controller — launched in 2025. It supports up to 24/192 PCM, DSD up to 22.5 MHz, and DXD 24/352.8 natively. ([Qobuz Connect launch coverage on Darko.Audio Feb 2026](https://darko.audio/2026/02/cambridge-adds-qobuz-connect-to-streaming-hardware-dating-back-to-2014/), [Audfree explainer](https://www.audfree.com/streaming-music-tips/qobuz-connect.html), [What Hi-Fi guide](https://www.whathifi.com/streaming-entertainment/music-streaming/qobuz-connect-what-is-it-which-products-support-it))

**Does MusicCast implement it?**

**No, not as of May 2026.** The official Qobuz [list of integrated brands](https://help.qobuz.com/en/articles/314578-list-of-brands-integrated-into-qobuz-connect) names ~100 manufacturers including Marantz, Denon, HEOS, Onkyo, Cambridge Audio, NAD, Bluesound, McIntosh, dCS, Naim, KEF, Volumio, WiiM, Eversolo — but **Yamaha is not on it**. Confirming community evidence: a [Facebook MusicCast group thread](https://www.facebook.com/groups/156021117016/posts/10162862003962017/) titled "Hey Yamaha — please work on the Qobuz Connect firmware" reports Yamaha customer support has no information on if/when Qobuz Connect will reach MusicCast. The R-N800 has been singled out by users as not getting Connect despite recent firmware. ([What Hi-Fi forum thread](https://forums.whathifi.com/threads/qobuz-connect.137441/))

**If Yamaha adds it later**

The protocol is designed exactly like Spotify Connect: our controller talks to the Qobuz app/SDK; the receiver pulls bit-perfect; we have no audio responsibility on the desktop. UX would be perfect: full browse/search via Qobuz's own API (which we'd be hitting anyway for art and metadata), full hi-res, no daemons, no token rot. But none of that is available to us today on Yamaha hardware.

**Verdict: not viable in May 2026; flag as the migration target if Yamaha announces Connect.**

## Path 5: MPD + mopidy-qobuz bridge

**Components**

Mopidy (the MPD-compatible server) + a Qobuz backend extension + a UPnP gateway (e.g. upmpdcli pointed at Mopidy's MPD socket).

**Maintenance status of mopidy-qobuz: poor**

- [taschenb/mopidy-qobuz](https://github.com/taschenb/mopidy-qobuz) — unmaintained since ~2019, last commit pre-2020, open issues including "Invalid or missing app_id parameter".
- [vitiko98/mopidy-qobuz (Hi-Res fork, also published as Mopidy-Qobuz-Hires on PyPI)](https://github.com/vitiko98/mopidy-qobuz) — Snyk classifies it as **Inactive** (release cadence, repo activity).
- [secondwtq/mopidy-qobuz](https://github.com/secondwtq/mopidy-qobuz) — small fork, no recent activity.

In short: every Mopidy-Qobuz extension on offer is alpha-quality and effectively unmaintained.

**Why this path adds nothing over Path 2**

upmpdcli's Qobuz plugin is the *better-maintained* version of the same idea: it talks to Qobuz directly, exposes a UPnP browse tree, and renders to MPD. There is no scenario where adding Mopidy in front of MPD via an unmaintained Qobuz extension is preferable to upmpdcli's bundled plugin.

**Verdict: ranked lowest. Strictly dominated by Path 2.** No reason to pursue unless we want a non-UPnP/non-DLNA frontend (e.g. Iris web UI) — and even then the Qobuz extension is the weak link.

## Ranking summary

| Path                              | Feasibility (May 2026)                                               | UX completeness                                                               | Lossless?                   | Complexity                                 | Rank  |
| --------------------------------- | -------------------------------------------------------------------- | ----------------------------------------------------------------------------- | --------------------------- | ------------------------------------------ | ----- |
| 1. Native Qobuz / YXC             | High *if* `getFeatures` returns `qobuz` — needs probe on .44 and .74 | Full: browse, search, art, transport, events                                  | Yes (24/192 single-zone)    | Lowest: thin UI over YXC                   | **1** |
| 2. DLNA push via upmpdcli + Qobuz | High; upmpdcli 1.9.17 (Apr 2026) confirms Qobuz plugin is alive      | Full but two-sided (controller browses, receiver shows generic UPnP metadata) | Yes (24/192 FLAC over DLNA) | Medium: upmpdcli + MPD daemons; auth setup | **2** |
| 3. AirPlay sender from Linux      | Low: only one half-maintained CLI sender (cliairplay)                | Browse in controller; minimal receiver metadata                               | **No (16/44.1 cap)**        | Medium-high (fragile sender stack)         | 4     |
| 4. Qobuz Connect                  | **Not viable** — Yamaha not on Qobuz partner list as of May 2026     | Would be full if available                                                    | Yes                         | Lowest *if* it ever lands on MusicCast     | 5     |
| 5. Mopidy-qobuz + UPnP bridge     | Medium (extension unmaintained)                                      | Full                                                                          | Yes (in theory)             | Highest: 3 daemons + dead extension        | 3     |

## Recommendation for the decision gate

**Pursue Path 1 conditional on a one-shot probe; have Path 2 ready as the fallback.**

The very first task in implementation should be running `getFeatures` against both receivers and grepping for `qobuz` and `account_enable`. If both return positive: we build a thin YXC controller, leverage Track A's endpoint reference, and we're done. The whole architecture collapses to "MusicCast app, but better".

**If the probe says one or both receivers do NOT have native Qobuz** (likely if the units turn out to be older than 2018, or non-MusicCast-2.0 hardware), we pivot to Path 2: install `upmpdcli` with the Qobuz plugin via a new playbook, point our controller at upmpdcli's MPD socket for browse/transport and at the receiver's UPnP `AVTransport` for routing. This is more code and one more daemon, but stays lossless and fully featured. **If Track A's findings show that the YXC qobuz subtree on these models is missing search or browse** (some older firmware versions exposed Qobuz only via Favorites and Recents, not full browse), we likewise pivot to Path 2 — the receiver's audio path is fine, but the YXC menu surface is too thin to build a real browser on.

A secondary watch-item: monitor Yamaha firmware notes for Qobuz Connect support. If/when it lands, we migrate from YXC-Qobuz to Connect for free metadata, cleaner session model, and avoidance of any future YXC-Qobuz token rot.

## Sources

- [Sound & Vision — Yamaha MusicCast Products Can Now Stream Qobuz](https://www.soundandvision.com/content/yamaha-musiccast-products-can-now-stream-qobuz)
- [Qobuz Community — Yamaha integration announcement, June 2019](https://community.qobuz.com/news-en/2019/06/03/qobuz-announces-integration-with-yamaha-musiccast-free-firmware-update)
- [Hi-Res Audio Online — Qobuz integrates with Yamaha MusicCast](https://www.hiresaudio.online/qobuz-integrates-with-yamaha-musiccast/)
- [Yamaha MusicCast FAQ (US)](https://usa.yamaha.com/products/contents/audio_visual/musiccast/musiccast-faqs.html)
- [Polk Audio forum — Qobuz support for pre-2018 Yamaha](https://forum.polkaudio.com/discussion/187111/yamaha-quobuz-support-for-models-made-before-2018)
- [Yamaha Extended Control API Specification (Basic), Rev 1.10](/workspace/CLAUDE/Plan/00038-musiccast-controller/yxc-api-spec-basic.pdf) — sections 4.2 (`getFeatures`), 7 (Network/USB), 13 (List Control application notes)
- [openHAB-hosted YXC Basic spec PDF](https://community-openhab-org.s3-eu-central-1.amazonaws.com/original/2X/9/931ea88e30cf0f05fcdee79816eb4d3f12dd4d70.pdf)
- [yamaha-commands README — community-extracted YXC reference](https://github.com/honnel/yamaha-commands/blob/master/README.md)
- [foxthefox/yamaha-yxc-nodejs library](https://github.com/foxthefox/yamaha-yxc-nodejs)
- [9to5Mac — Yamaha AirPlay 2 product list](https://9to5mac.com/2019/04/15/yamaha-airplay-2-updates/)
- [Yamaha Hub — How to Use AirPlay 2 with MusicCast](https://hub.yamaha.com/audio/a-how-to/how-to-use-airplay-2-with-musiccast/)
- [SofusA/qobuz-player README](https://github.com/SofusA/qobuz-player) — moved to qobine; GStreamer backend, local outputs only
- [SofusA/qobine README](https://github.com/SofusA/qobine) — successor; alsa-sys-devel dep, no DLNA/AirPlay output
- [iamdb/hifi.rs README](https://github.com/iamdb/hifi.rs) — GStreamer-backed local player, no networked output
- [upmpdcli home page](https://www.lesbonscomptes.com/upmpdcli/index.html) — 1.9.17 restored Qobuz, gateway media-server architecture
- [GioF71/upmpdcli-docker](https://github.com/GioF71/upmpdcli-docker) — Tidal/Qobuz/Subsonic streaming via upmpdcli
- [Linux Audio Foundation — UPnP renderer with Qobuz/Tidal](https://linuxaudiofoundation.org/musiclounge-upnp-renderer-local-qobuz-tidal-and-others/)
- [Hifizine — Stream Qobuz to almost anything](https://www.hifizine.com/2019/07/stream-qobuz-to-anything/) (background reading; live fetch returned 403, content available via cached/search excerpts)
- [Lyrion forums — LMS, Qobuz and UPnP](https://forums.lyrion.org/forum/user-forums/logitech-media-server/110940-lms-qobuz-and-upnp)
- [mikebrady/shairport-sync README — receive-only](https://github.com/mikebrady/shairport-sync)
- [music-assistant/cliairplay](https://github.com/music-assistant/cliairplay) — sole credible Linux AirPlay-2 sender CLI
- [pulseaudio-raop2 (legacy AirPlay 1)](https://hfujita.github.io/pulseaudio-raop2/)
- [Qobuz — "compatible devices and protocols" article](https://www.qobuz.com/us-en/magazine/story/Qobuz-Vous/Qobuz-Streaming-the-compatible179461/) — AirPlay capped at 16/44.1
- [Qobuz Help Centre — list of brands integrated into Qobuz Connect](https://help.qobuz.com/en/articles/314578-list-of-brands-integrated-into-qobuz-connect) — Yamaha **not** present (verified May 2026)
- [Qobuz Help Centre — which devices are compatible with Qobuz Connect](https://help.qobuz.com/en/articles/313013-which-devices-are-compatible-with-qobuz-connect)
- [Darko.Audio — Cambridge adds Qobuz Connect (Feb 2026)](https://darko.audio/2026/02/cambridge-adds-qobuz-connect-to-streaming-hardware-dating-back-to-2014/)
- [Audfree — Qobuz Connect explainer](https://www.audfree.com/streaming-music-tips/qobuz-connect.html)
- [What Hi-Fi — Qobuz Connect guide and supported product list](https://www.whathifi.com/streaming-entertainment/music-streaming/qobuz-connect-what-is-it-which-products-support-it)
- [What Hi-Fi forum — Qobuz Connect / Yamaha discussion](https://forums.whathifi.com/threads/qobuz-connect.137441/)
- [Facebook MusicCast group — "Hey Yamaha — please work on the Qobuz Connect firmware"](https://www.facebook.com/groups/156021117016/posts/10162862003962017/)
- [taschenb/mopidy-qobuz — unmaintained](https://github.com/taschenb/mopidy-qobuz)
- [vitiko98/mopidy-qobuz (Hi-Res, Snyk: Inactive)](https://github.com/vitiko98/mopidy-qobuz)
- [Mopidy-Qobuz-Hires on PyPI](https://pypi.org/project/Mopidy-Qobuz-Hires/)
- [Audiophonics — Yamaha R-N402D specs (DLNA + 24/192)](https://www.audiophonics.fr/en/integrated-amplifiers/yamaha-musiccast-r-n402d-amplifier-streamer-wifi-airplay-dlna-bluetooth-2x115w-4-ohm-24bit-192khz-dsd128-p-15758.html)
- [Symfonium support — UPnP track-progress on MusicCast](https://support.symfonium.app/t/track-progress-not-updated-if-using-upnp-dlna-with-yamaha-musiccast-devices/2225) (corroborates DLNA renderer behaviour)
