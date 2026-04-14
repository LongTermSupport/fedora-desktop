# Research Track 1 — Self-Hosted Real-Time Collaboration Platforms

**Plan**: 031 — Reliable Screen Sharing for Fedora 43 GNOME Wayland
**Agent**: Research Agent 1 of 4
**Scope**: Self-hostable open-source platforms suitable for a WFH dev team (1:1 and ≤4-person calls) sitting behind residential NAT
**Date**: April 2026

## Executive framing

Every candidate in this track ultimately delivers screen sharing through the browser's `getDisplayMedia()` WebRTC API, which means the actual capture on Fedora 43 GNOME Wayland flows through the same pipeline that's failing for Google Meet: Firefox/Chromium → `xdg-desktop-portal-gnome` → `mutter`'s ScreenCast D-Bus interface → PipeWire. If that pipeline is broken at the compositor layer, **no self-hosted server magically fixes it**. What a self-hosted platform *can* do is (a) give us a codec/bitrate profile that doesn't trigger the mutter freeze as aggressively as Meet's default, (b) fall back to a native Electron/desktop client that bypasses the browser's portal quirks, or (c) let us A/B test whether the freeze is client-side (likely) or server-side (unlikely — Google's infra is fine). Keep this in mind throughout.

The mutter screencast freeze affecting Meet is a known family of bugs tracked against GNOME 48+; there is no landed fix in April 2026 that I could confirm ([Clear Linux Forum — Mutter bug in GNOME 48](https://community.clearlinux.org/t/mutter-bug-in-gnome-48/10372), [Ubuntu Bug #2045772](https://bugs.launchpad.net/bugs/2045772)). Firefox on Wayland screen sharing via xdg-desktop-portal is broadly working now but still reports regressions after GNOME 48 portal changes ([Arch bbs 283835](https://bbs.archlinux.org/viewtopic.php?id=283835), [Mozilla bz 1746771](https://bugzilla.mozilla.org/show_bug.cgi?id=1746771)).

## Candidates

### Jitsi Meet (self-hosted)

- **What / maintainer**: WebRTC SFU + Prosody XMPP signaling, maintained by 8x8. Latest stable **2.0.10888 released 2026‑02‑02** with a prior 2.0.10741 on 2026‑01‑14 ([jitsi-meet releases](https://github.com/jitsi/jitsi-meet/releases)). Very active.
- **Delivery**: Browser-first (Chromium and Firefox), plus an Electron app and mobile. Self-hosted web client uses `getDisplayMedia`, so on Fedora 43 Wayland it goes through the native system picker. Handbook notes the release that added proper PipeWire/Wayland picker support ([Jitsi releases handbook](https://jitsi.github.io/handbook/docs/releases/)).
- **Wayland screen share status**: Browser-side screen sharing on GNOME Wayland works when xdg-desktop-portal-gnome is installed and PipeWire is running — the stock Fedora 43 setup satisfies this. The Electron client has a **long tail of Wayland bugs**: "select application/screen twice" ([issue 963](https://github.com/jitsi/jitsi-meet-electron/issues/963)), can't share entire screen ([issue 200](https://github.com/jitsi/jitsi-meet-electron/issues/200)), and usability complaints ([issue 829](https://github.com/jitsi/jitsi-meet-electron/issues/829)). The Flathub build had additional issues forcing pipewire socket access ([flathub issue 31](https://github.com/flathub/org.jitsi.jitsi-meet/issues/31)). The meta-issue for Wayland support ([jitsi-meet 6389](https://github.com/jitsi/jitsi-meet/issues/6389)) is still cited in 2025 Fedora threads ([Fedora Discussion 66446](https://discussion.fedoraproject.org/t/not-able-to-share-screen-with-jitsi-meet-on-wayland-with-flatpaks/66446)). **Recommendation: use it via Firefox/Chromium, not Electron**.
- **Audio**: Mic yes. "Share tab audio" only works in Chromium browsers (upstream browser limitation, not Jitsi). Firefox on Linux cannot capture system/monitor audio during screen share — this is consistent across every WebRTC platform in this track.
- **Infra**: Single VM is fine for ≤10 participants. Components: Jicofo, Jitsi Videobridge (JVB), Prosody, nginx. Bundled TURN (coturn) is part of the Debian/Ubuntu quickstart ([devops-guide-quickstart](https://jitsi.github.io/handbook/docs/devops-guide/devops-guide-quickstart/)).
- **Bandwidth / CPU**: ~500 kbps–2 Mbps per participant; JVB is Java, moderately hungry. 2 vCPU / 4 GB VPS handles a 4-person call comfortably.
- **Install effort**: Low — the `jitsi-meet` deb metapackage on Ubuntu 22.04 is ~15 minutes plus DNS/Let's Encrypt.
- **Multi-user**: Works fine for 4. Scales to dozens per JVB.
- **NAT traversal**: Coturn installed by the quickstart. Residential NAT users will work via TURN/443 relay when direct UDP fails.
- **Verdict**: **Viable** via browser. The server side is mature. The Wayland risk is entirely client-side and shared with Meet — meaning **Jitsi could inherit the exact same mutter freeze**. Worth live testing *precisely because* it isolates whether the bug is Meet-specific or pipeline-wide.

### BigBlueButton

- **What / maintainer**: Full classroom platform (slides, whiteboard, polls, breakout rooms) built on Kurento/mediasoup. BigBlueButton Inc. / Blindside Networks. BBB **3.0.23 released March 2026**; 3.1 is in `-dev` only ([endoflife.date — BigBlueButton](https://endoflife.date/bigbluebutton), [release notes](https://docs.bigbluebutton.org/release-notes/)).
- **Delivery**: Browser-only HTML5 client (Chrome, Firefox, Chromium-Edge). No native desktop app.
- **Wayland status**: Reports in October 2025 of **black-screen screen sharing on Ubuntu 22.04 + BBB 3.0.16** when no audio device was redirected ([bbb-install issue 802](https://github.com/bigbluebutton/bbb-install/issues/802)). The original Wayland screen-share issue ([bigbluebutton 9338](https://github.com/bigbluebutton/bigbluebutton/issues/9338)) is closed but Wayland regressions recur with each browser or portal change.
- **Audio**: Mic yes. BBB uses FreeSWITCH audio bridge independently of the video pipeline, which is actually nicer — audio keeps flowing if screen share dies. No built-in desktop-audio capture.
- **Infra**: **Heavy**. BBB mandates Ubuntu 22.04 LTS specifically ([docs.bigbluebutton.org install](https://docs.bigbluebutton.org/administration/install/)), and runs FreeSWITCH, Kurento, mediasoup, Redis, MongoDB, NGINX, Node, Greenlight. 8 GB RAM minimum, 4+ vCPU recommended, plus a TURN server for strict NATs.
- **Bandwidth / CPU**: Heaviest in this list. Designed for 100-student classrooms, overkill for 4 devs.
- **Install effort**: `bbb-install.sh` takes ~30 minutes on a compliant server, but it *will fight you* if the server isn't a dedicated clean Ubuntu 22.04 box.
- **NAT traversal**: Coturn bundled.
- **Verdict**: **Skip.** Feature set is wildly over-spec for a 4-dev standup, install complexity is disproportionate, and Wayland screen-share has recent (late-2025) regression reports. Not worth testing for this use case.

### Nextcloud Talk

- **What / maintainer**: First-party Nextcloud app (`spreed`). Active, ships with every Nextcloud release. Maintained by Nextcloud GmbH.
- **Delivery**: Browser + native desktop/mobile clients.
- **Wayland status**: Same browser-side `getDisplayMedia` stack as Jitsi. No unique issues reported against it for Fedora 43.
- **Audio**: Mic yes. Screen sharing *with* system audio requires Chromium ("Chrome tab" option) — explicitly called out in Nextcloud forums as a browser-side limitation ([Nextcloud — Talk screensharing with audio](https://help.nextcloud.com/t/talk-screensharing-with-audio-possible/96964)). Firefox users will not get desktop audio.
- **Infra**: Nextcloud itself (PHP + DB) + **required "High Performance Backend"** (HPB = Nextcloud Talk signaling server, based on Janus) for anything beyond 2‑3 participants. Nextcloud explicitly warns the built-in mesh MCU degrades above that ([Nextcloud Talk on Coturn + HPB](https://arnowelzel.de/en/nextcloud-talk-with-coturn-and-self-hosted-signaling-server-high-performance-backend)). Plus coturn.
- **Bandwidth / CPU**: HPB needs its own server or at least its own ports; coturn recommended on its own IP.
- **Install effort**: Medium‑high. Simple Nextcloud is trivial; adding HPB + coturn + Talk signaling requires 3 services correctly wired. `community.nethserver.org` threads walk through it.
- **Multi-user**: Without HPB it's basically 1:1. With HPB it scales fine for 4.
- **NAT traversal**: STUN works out of the box; coturn is the standard add-on.
- **Verdict**: **Viable for teams already running Nextcloud.** If the team isn't, it's a lot of yak-shaving for what is ultimately the same browser-side pipeline as Jitsi. Medium priority.

### Element Call / MatrixRTC (LiveKit-backed)

- **What / maintainer**: Element Hq's native-Matrix video conferencing, migrated to LiveKit SFU via MatrixRTC (MSC4143, MSC4195). FOSDEM 2026 presentation covered current architecture ([GIGAZINE — MatrixRTC at FOSDEM 2026](https://gigazine.net/gsc_news/en/20260220-matrixrtc/)). Since **April 2025 Element stopped providing hosted LiveKit** as a courtesy service ([Element blog — E2EE voice/video for self-hosted](https://element.io/blog/end-to-end-encrypted-voice-and-video-for-self-hosted-community-users/)), so self-hosters must deploy their own.
- **Delivery**: Browser (Element Call web) + Element X / Element Desktop (Electron-ish). E2E-encrypted streams.
- **Wayland status**: Same browser pipeline. No Element-specific Wayland pathologies beyond the client layer.
- **Audio**: Mic yes. Desktop audio again gated on the browser.
- **Infra**: **The most complex stack in this track.** Synapse (or Dendrite) homeserver + `lk-jwt-service` (Go, auth tokens) + LiveKit SFU + coturn + nginx reverse proxy + correct `.well-known/matrix/client` entries for `org.matrix.msc4143.rtc_foci` ([element-call self-hosting.md](https://github.com/element-hq/element-call/blob/livekit/docs/self-hosting.md), [lk-jwt-service](https://github.com/element-hq/lk-jwt-service), [willlewis.co.uk deployment walkthrough](https://willlewis.co.uk/blog/posts/deploy-element-call-backend-with-synapse-and-docker-compose/), [Spaetzblog MatrixRTC setup](https://sspaeth.de/2024/11/sfu/)).
- **Bandwidth / CPU**: LiveKit is Go and efficient, Synapse is Python and heavier. 4 GB VPS is a realistic floor for the Matrix side.
- **Install effort**: **High.** Expect a half-day even with docker-compose, more if starting from zero Matrix experience. Worth it only if the team already wants Matrix chat.
- **NAT traversal**: LiveKit + coturn; well-documented.
- **Verdict**: **Overkill** unless the team adopts Matrix as its primary chat. Not a priority for short-term screen-sharing pain relief.

### Mattermost Calls

- **What / maintainer**: First-party Mattermost plugin; ships with all self-hosted Mattermost installs since v7+ ([mattermost-plugin-calls](https://github.com/mattermost/mattermost-plugin-calls)).
- **Delivery**: Browser + Mattermost desktop (Electron).
- **Audio**: **Screen-share-with-audio landed in calls plugin v1.9.0** ([Mattermost audio/screensharing docs](https://docs.mattermost.com/end-user-guide/collaborate/audio-and-screensharing.html)). Enabled per-user via Settings → Plugin Preferences → Calls.
- **Wayland status**: No Mattermost-specific Wayland screen-share regressions surfaced in search. Desktop client is Electron — will inherit Electron/Wayland quirks the same way Jitsi Electron does.
- **Infra**: Mattermost server + built-in calls plugin. For group calls >2 on free edition, you need the RTCD server component separately ([calls-deployment docs](https://docs.mattermost.com/administration-guide/configure/calls-deployment.html)). **1:1 with screen-share is free**; group calls up to 50 require Enterprise/Professional licensing — a real gotcha.
- **Install effort**: Medium. Mattermost itself is easy; RTCD adds one more service.
- **NAT traversal**: RTCD includes TURN-ish handling; external coturn still recommended for symmetric NAT.
- **Verdict**: **Good for 1:1**, licensing issue for 4-person calls unless the team runs Mattermost Professional. Demote unless 1:1 is the dominant use case.

### Rocket.Chat + Jitsi bridge

- **What / maintainer**: Rocket.Chat ships a Jitsi app/slash-command (`/jitsi`) that embeds a Jitsi room per Rocket.Chat channel ([Rocket.Chat Jitsi app docs](https://docs.rocket.chat/docs/jitsi-app)).
- **Delivery**: Inherits Jitsi (browser + optional Electron).
- **Wayland / audio / NAT**: Identical to Jitsi. All screen-share behaviour is whatever Jitsi does.
- **Infra**: Rocket.Chat server + self-hosted Jitsi (two stacks). Historic forum reports of "screen share doesn't work embedded but works in a separate window" ([Rocket.Chat forum 13258](https://forums.rocket.chat/t/screen-share-not-work-in-rocketchat-with-jitsi-in-chat-or-app-but-work-in-web-with-windowed-jitsi/13258)) — iframe sandboxing issue.
- **Verdict**: Only interesting if Rocket.Chat is already the team chat. The screen-share tech is just Jitsi.

### Galène (galene.org)

- **What / maintainer**: Minimalist Go/Pion WebRTC SFU by Juliusz Chroboczek (of Polybus / IRIF, Paris). Designed for lectures. Single ~10 MB binary. Active but low-velocity project.
- **Delivery**: Browser (vanilla JS client). **Also a native desktop client** that supports screen sharing ([galene.org feature list](https://galene.org)) — this is one of the few platforms in this track with a non-Electron native client.
- **Wayland status**: No Galène-specific Wayland issues found. Native client behaviour on Wayland isn't well-documented in Fedora threads — this is a probe opportunity.
- **Audio**: Mic + support for "numerous audio and video streams". Audio-with-screen-share works in Chromium tab-share mode; Firefox limitation applies. There's an active lists.galene.org thread on "how to share audio when screen sharing" indicating it's a known user question but solvable on Chromium.
- **Infra**: **Single Go binary**. Built-in TURN server. Runs on "1/4 of a CPU core" for a 100-person lecture per the docs. Setup is: `go build`, drop in a `groups/*.json`, point Let's Encrypt cert at `data/`, systemd unit, done ([dev.to — Galene on VPS](https://dev.to/sledov/galene-a-simple-videoconferencing-server-installation-on-vps-g4n), [galene.org docs](https://galene.org/galene.html)).
- **Bandwidth / CPU**: Lowest in this list. A €5 Hetzner VM runs it fine.
- **Install effort**: **Lowest.** 30 minutes including DNS.
- **Multi-user**: Practically unlimited for the team's scale.
- **NAT traversal**: Built-in TURN — no separate coturn. Listed explicitly as a feature ([galene.org](https://galene.org)).
- **Verdict**: **Strong contender** on simplicity and NAT handling. The LWN article comparing BBB vs Galène ([LWN 908960](https://lwn.net/Articles/908960/)) treats Galène seriously. Risk: smaller user base means less Fedora-43-specific tribal knowledge.

### Mediasoup / Janus (raw SFUs)

- **What**: Neither is a product; both are SFU libraries you write an app around. Mediasoup is Node + C++ (vertical scale), Janus is C plugin-based (horizontal scale) ([trembit comparison](https://trembit.com/blog/choosing-the-right-sfu-janus-vs-mediasoup-vs-livekit-for-telemedicine-platforms/)).
- **Turnkey option**: **MiroTalk SFU** — a mediasoup-based turnkey conference app with screen share, chat, recording, self-hosted ([mirotalksfu github](https://github.com/miroslavpejic85/mirotalksfu), [docs.mirotalk.com self-hosting](https://docs.mirotalk.com/mirotalk-sfu/self-hosting/)). Active, Node 22.x, install is `npm install` + a coturn setup. Browser-only client.
- **Wayland**: Same `getDisplayMedia` story.
- **Verdict**: **MiroTalk SFU is a viable plug-and-play option**; raw mediasoup/Janus is not — don't build custom for a team-of-four.

### Daily.co self-hosted

Not offered. Daily is SaaS-only. Skip.

## Comparison matrix

| Platform                 | Last release     | Install effort     | Infra footprint               | Native non-Electron client    | Built-in TURN     | 4-user fit        | Wayland risk                           | Priority                   |
| ------------------------ | ---------------- | ------------------ | ----------------------------- | ----------------------------- | ----------------- | ----------------- | -------------------------------------- | -------------------------- |
| Jitsi Meet               | 2026‑02‑02       | Low                | 1 VM, ~4 GB                   | No (Electron only)            | Yes (coturn)      | Excellent         | Medium (same browser pipeline as Meet) | **High — testbed**         |
| BigBlueButton            | 2026‑03 (3.0.23) | High               | Dedicated Ubuntu 22.04, 8 GB+ | No                            | Yes               | Overkill          | Medium, recent black-screen reports    | Skip                       |
| Nextcloud Talk           | Rolling          | Medium-high (+HPB) | 2‑3 services                  | Desktop app yes               | No, add coturn    | OK with HPB       | Low‑Medium                             | Medium                     |
| Element Call / MatrixRTC | Rolling          | High               | 4‑5 services                  | Element Desktop               | No, add coturn    | Good              | Low‑Medium                             | Low (unless Matrix-native) |
| Mattermost Calls         | Rolling          | Medium             | 2 services + licensing        | Mattermost desktop (Electron) | Partial           | 1:1 free, >2 paid | Low‑Medium                             | Medium for 1:1             |
| Rocket.Chat + Jitsi      | Rolling          | Medium             | 2 stacks                      | Via Jitsi                     | Via Jitsi         | Same as Jitsi     | Same as Jitsi                          | Low                        |
| **Galène**               | Active Go repo   | **Low**            | **1 Go binary**               | **Yes, native client**        | **Yes, built-in** | Excellent         | **Unknown — probe opportunity**        | **High — testbed**         |
| MiroTalk SFU             | Active           | Low-medium         | Node + coturn                 | No                            | No                | Good              | Same browser pipeline                  | Medium                     |

## Top picks for live testing

### Pick 1 — Galène

Rationale:

- Ten-minute install on any €5 VPS; single Go binary, built-in TURN solves the residential-NAT problem without an extra coturn.
- **It's the only candidate with a non-Electron native desktop client** that does screen sharing. If the mutter ScreenCast bug is truly in the GNOME compositor + browser interaction, a native Pion/WebRTC client might capture via a different path and survive where Meet freezes. This is the most *physically different* test we can run against the underlying bug.
- Cheap to throw away if it fails.

### Pick 2 — Jitsi Meet (self-hosted, browser client)

Rationale:

- The reference self-hosted WebRTC platform. If Jitsi-over-Firefox *also* freezes with the mutter bug, we have strong evidence the issue is in the Fedora 43 portal/mutter stack itself and no self-hosted server in this track will save us — which is crucial negative information that narrows Plan 031's solution space toward peer-to-peer (Agent 2's track) or SaaS alternatives (Agent 3's).
- If it *doesn't* freeze, we have a drop-in replacement for Meet with the same UX for basically free.
- Releases are active and recent (Feb 2026), install is 15 minutes on Ubuntu 22.04.

### What I am explicitly not recommending for first testing

- **BigBlueButton**: install complexity and feature sprawl vastly exceed the requirement; recent black-screen Wayland reports add risk without upside.
- **Element Call / MatrixRTC**: the infrastructure cost is only justified if the team is also migrating chat to Matrix.
- **Nextcloud Talk / Mattermost / Rocket.Chat bridges**: these are chat-platform add-ons; they only make sense if the chat platform is already the team's tool, and even then they don't introduce a *new* screen-share pipeline — Nextcloud and Rocket+Jitsi reuse the same browser-side `getDisplayMedia` path that Meet uses.

## Caveats the live-test plan should account for

1. **None of these tools fix a mutter screencast bug**. If mutter is the culprit, server choice is irrelevant and the solution is a Fedora compositor patch, a Wayland-portal workaround, an X11 session fallback, or a native-capture peer-to-peer tool (Agent 2's track).
2. **Desktop-audio capture during screen share is a browser limitation on Linux**, not a server feature. Firefox on Linux simply cannot deliver it; Chromium delivers it only for tab-share, not full-screen share. This constraint is identical across every platform in this track and shouldn't be used as a differentiator.
3. **Residential NAT + TURN is the easy part**. Every platform here handles it; whichever we choose, plan on a TURN server listening on UDP 3478 and TLS 443.

## Sources

- [Jitsi Meet releases (GitHub)](https://github.com/jitsi/jitsi-meet/releases)
- [Jitsi handbook — releases](https://jitsi.github.io/handbook/docs/releases/)
- [Jitsi self-hosting quickstart](https://jitsi.github.io/handbook/docs/devops-guide/devops-guide-quickstart/)
- [jitsi-meet issue 6389 — Wayland screen sharing](https://github.com/jitsi/jitsi-meet/issues/6389)
- [jitsi-meet-electron 200 — Cannot share entire screen on Wayland](https://github.com/jitsi/jitsi-meet-electron/issues/200)
- [jitsi-meet-electron 963 — select twice on Wayland](https://github.com/jitsi/jitsi-meet-electron/issues/963)
- [jitsi-meet-electron 829 — Wayland screensharing UX](https://github.com/jitsi/jitsi-meet-electron/issues/829)
- [jitsi-meet-electron 567 — Pipewire support](https://github.com/jitsi/jitsi-meet-electron/issues/567)
- [flathub jitsi-meet issue 31 — drop pipewire filesystem](https://github.com/flathub/org.jitsi.jitsi-meet/issues/31)
- [Fedora Discussion — Jitsi Wayland screen share](https://discussion.fedoraproject.org/t/not-able-to-share-screen-with-jitsi-meet-on-wayland-with-flatpaks/66446)
- [BigBlueButton release notes](https://docs.bigbluebutton.org/release-notes/)
- [BigBlueButton install docs](https://docs.bigbluebutton.org/administration/install/)
- [bbb-install issue 802 — black-screen screen share Oct 2025](https://github.com/bigbluebutton/bbb-install/issues/802)
- [bigbluebutton 9338 — Wayland screen sharing not working](https://github.com/bigbluebutton/bigbluebutton/issues/9338)
- [endoflife.date — BigBlueButton](https://endoflife.date/bigbluebutton)
- [Nextcloud Talk — screensharing with audio thread](https://help.nextcloud.com/t/talk-screensharing-with-audio-possible/96964)
- [Nextcloud Talk + Coturn + HPB walkthrough (Arno Welzel)](https://arnowelzel.de/en/nextcloud-talk-with-coturn-and-self-hosted-signaling-server-high-performance-backend)
- [nextcloud/spreed on GitHub](https://github.com/nextcloud/spreed)
- [Element blog — E2EE voice/video for self-hosted](https://element.io/blog/end-to-end-encrypted-voice-and-video-for-self-hosted-community-users/)
- [element-call self-hosting docs](https://github.com/element-hq/element-call/blob/livekit/docs/self-hosting.md)
- [lk-jwt-service](https://github.com/element-hq/lk-jwt-service)
- [Spaetzblog — MatrixRTC setup](https://sspaeth.de/2024/11/sfu/)
- [willlewis.co.uk — Element Call backend with Synapse + Docker](https://willlewis.co.uk/blog/posts/deploy-element-call-backend-with-synapse-and-docker-compose/)
- [GIGAZINE — MatrixRTC at FOSDEM 2026](https://gigazine.net/gsc_news/en/20260220-matrixrtc/)
- [Mattermost calls deployment](https://docs.mattermost.com/administration-guide/configure/calls-deployment.html)
- [Mattermost audio + screensharing docs](https://docs.mattermost.com/end-user-guide/collaborate/audio-and-screensharing.html)
- [mattermost-plugin-calls](https://github.com/mattermost/mattermost-plugin-calls)
- [Rocket.Chat Jitsi app docs](https://docs.rocket.chat/docs/jitsi-app)
- [Rocket.Chat forum 13258 — iframe screen share](https://forums.rocket.chat/t/screen-share-not-work-in-rocketchat-with-jitsi-in-chat-or-app-but-work-in-web-with-windowed-jitsi/13258)
- [Galène homepage](https://galene.org)
- [Galène docs](https://galene.org/galene.html)
- [jech/galene on GitHub](https://github.com/jech/galene)
- [dev.to — Galene on a VPS](https://dev.to/sledov/galene-a-simple-videoconferencing-server-installation-on-vps-g4n)
- [LWN — bbb vs galene](https://lwn.net/Articles/908960/)
- [Simon Vandevelde — Host your own videoconferencing with Galene](https://simonvandevelde.be/posts/Host_Your_Own_Videoconferencing_Server_Using_Galene.html)
- [mirotalksfu on GitHub](https://github.com/miroslavpejic85/mirotalksfu)
- [MiroTalk SFU self-hosting docs](https://docs.mirotalk.com/mirotalk-sfu/self-hosting/)
- [medevel — MiroTalk SFU overview](https://medevel.com/mirotalk-sfu/)
- [Trembit — Janus vs Mediasoup vs LiveKit](https://trembit.com/blog/choosing-the-right-sfu-janus-vs-mediasoup-vs-livekit-for-telemedicine-platforms/)
- [Arch bbs 283835 — Wayland/GNOME/Firefox/Chromium screen sharing](https://bbs.archlinux.org/viewtopic.php?id=283835)
- [Mozilla bz 1746771 — Firefox xdg-desktop-portal direct display](https://bugzilla.mozilla.org/show_bug.cgi?id=1746771)
- [Clear Linux — Mutter bug in GNOME 48](https://community.clearlinux.org/t/mutter-bug-in-gnome-48/10372)
- [Ubuntu Bug #2045772 — GNOME session freezes after starting share](https://bugs.launchpad.net/bugs/2045772)
- [GNOME Discourse — Webcam freezes in Google Meet](https://discourse.gnome.org/t/webcam-image-freezes-in-google-meet-in-gnome/25567)
