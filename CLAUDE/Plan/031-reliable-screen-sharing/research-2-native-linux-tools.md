# Research 2 — Native Linux Peer-to-Peer & Remote-Desktop Tools for Screen Sharing

**Context**: Plan 031, reliable screen sharing for a team of WFH devs on Fedora 43 GNOME Wayland,
behind residential NAT, no shared LAN. Slack screen sharing is disabled; Google Meet freezes
within seconds (likely the Mutter xdg-desktop-portal screencast bug). This track avoids SaaS
conferencing and self-hosted multi-user platforms — it looks at tools where the pixels flow
(mostly) directly between host and viewer machines.

**Scope note**: "Works on F43 GNOME Wayland" means two things — (a) the host can actually capture
the screen without hitting the same Mutter portal bug that breaks Meet, and (b) input/audio also
work. Anything that only works on X11, wlroots, or headless GDM is scored down for pair-programming
use because devs need to share their *live*, already-logged-in GNOME session.

---

## The Mutter/portal problem (quick recap)

On Wayland/GNOME, screen capture is mediated by the compositor via the
`org.freedesktop.portal.ScreenCast` D-Bus interface and a PipeWire node exported by
`xdg-desktop-portal-gnome` (`xdp-gnome`) talking to Mutter. Any tool that takes this
"portal path" runs into whatever bug is freezing Meet — because Chromium, Electron, Firefox,
OBS, gnome-remote-desktop, RustDesk's Wayland mode, and basically everything else all share
this path. There are only two escape hatches:

1. **KMS / DRM direct capture** — bypasses Mutter entirely, reads framebuffer at the kernel level
   (requires `cap_sys_admin`). Used by Sunshine.
2. **wlroots-specific protocols** (`wlr-screencopy`, `ext-image-copy-capture-v1`) — GNOME/Mutter
   does *not* implement wlr-screencopy, so these tools do not work on GNOME at all.

Nothing else bypasses it. Tools advertising "native Wayland" almost always mean
"uses the portal" — which on Mutter today is the failing path.

---

## Candidate 1: gnome-remote-desktop (GRD)

- **What**: GNOME's first-party RDP/VNC server, shipped in Fedora via `gnome-remote-desktop`
  (v49.1-1 in F43 repos). Ships `grdctl` CLI and a Settings panel. Source of truth is
  gitlab.gnome.org/GNOME/gnome-remote-desktop.
- **License**: GPL-2.0+.
- **Latest release**: 49.x (GNOME 49 cycle), early 2026. Active upstream.
- **Wayland on F43 GNOME**: First-party — this is *the* remote-desktop implementation Mutter
  is designed around. F43 ships both user-session sharing and headless modes. **However**, since
  F43 removed X11 GNOME sessions, users on the Fedora forum report GRD over RDP is "unstable"
  post-F43 upgrade — consistent with a wider Mutter screencast regression in that release.
- **Portal path?** GRD talks to Mutter over the same D-Bus `ScreenCast`/`RemoteDesktop` APIs;
  it does *not* bypass the portal path. That means **if Meet is freezing because of a Mutter
  bug, GRD screen sharing is likely to hit the same bug**. This is the single most important
  caveat against defaulting to it.
- **Audio**: Yes — PipeWire stereo audio over RDP is supported (`grd-rdp-pipewire-stream.c`).
- **Setup**: Peer-to-peer only. No built-in relay, no NAT traversal, no signalling. Host must be
  reachable — which for residential NAT means either a Tailscale/WireGuard overlay, port-forward,
  or SSH tunnel. Plan this in.
- **Latency**: RDP over LAN is sub-50 ms; over WAN with a mesh VPN expect 80–200 ms — fine for
  pair programming, not great for gaming.
- **Viewer**: Any RDP client — `gnome-connections`, Windows mstsc, macOS Microsoft Remote Desktop,
  FreeRDP, Remmina. **This is the strongest compatibility story in the field** — viewers don't
  need Linux.
- **Multi-viewer**: Limited. A single user-session share serves one viewer at a time, but
  "screen share" vs "remote control" modes exist; multi-headless user sessions can run
  concurrently but that's different from "one host, many eyeballs".
- **Auth**: Username/password; TLS with user-provided cert (`grdctl rdp set-tls-key/cert`).
- **2026 status**: Known issues filed for F43 instability; package is current; active dev.

**Verdict**: Good default *if* the Mutter portal bug in F43 gets patched. Otherwise same failure
mode as Meet. Excellent cross-platform viewer story. Needs a VPN overlay for NAT.

---

## Candidate 2: RustDesk

- **What**: Rust+Flutter TeamViewer/AnyDesk clone. Self-hostable relay (hbbs + hbbr) or uses
  public free tier.
- **License**: AGPL-3.0; relay server (hbbr) also AGPL. "Pro" server is commercial.
- **Latest release**: 1.4.3 (Oct 2025) added Wayland multi-monitor + virtual mouse; 1.1.15
  (Jan 2026 bugfix) bumped default bandwidth settings. Very active.
- **Wayland on F43 GNOME**: Experimental since 1.2.0, much improved in 1.4.x. But — GitHub
  discussion #13112 documents "strange window flickering making it unusable" on Fedora 42 GNOME
  Wayland; workaround was reverting to KDE or installing XWayland/Xorg packages. Issue #12507
  reports pointer misalignment on F42 GNOME 48 with external monitors. **Not yet confirmed
  stable on F43 GNOME.**
- **Portal path?** Yes — RustDesk's Wayland mode uses xdg-desktop-portal `RemoteDesktop`/
  `ScreenCast`. This is the same path as Meet. Same risk.
- **Audio**: Yes, bidirectional. Works via PulseAudio/PipeWire.
- **Setup**: The big win — built-in NAT traversal via the hbbs ID server + hbbr relay fallback.
  Free public relay works out of the box; can self-host (one small VPS, TCP 21115–21119,
  `docker run rustdesk/rustdesk-server`). ~180 KB/s per relayed session, 1 CPU + 1 GB RAM
  handles ~1000 concurrent.
- **Latency**: Direct P2P is low (30–80 ms typical). Relayed is +20–50 ms depending on relay
  proximity. GeoDNS-routed relays in Pro.
- **Viewer**: Native clients for Linux, Windows, macOS, iOS, Android, web (WASM). Cross-platform
  is solved.
- **Multi-viewer**: One session per host by default.
- **Auth**: Per-session one-time code + optional permanent password; relay key-pair ed25519.
- **2026 status**: Strong momentum, but real-world F43 GNOME reports are mixed. Installable via
  Flatpak (`flathub com.rustdesk.RustDesk`) or RPM.

**Verdict**: Best NAT story of any candidate. Wayland story is improving but still flaky on
GNOME specifically, and still rides the same portal path that's failing for Meet. Worth a live
test but don't bet the team on it yet.

---

## Candidate 3: Sunshine + Moonlight

- **What**: Open-source reimplementation of NVIDIA GameStream, built for low-latency game
  streaming. Sunshine is the host; Moonlight is the viewer. Hardware-encoded
  H.264/HEVC/AV1 via VAAPI/NVENC/AMF.
- **License**: GPL-3.0.
- **Latest release**: Active 2025–2026 cadence; headless-monitor Wayland support added 2025.
- **Wayland on F43 GNOME**: **This is the interesting one.** Sunshine on GNOME/KDE (non-wlroots)
  uses **KMS capture**, which reads the DRM framebuffer directly — it **bypasses Mutter and the
  xdg-desktop-portal entirely**. That means the Meet-freezing bug cannot affect it.
  Requires `sudo setcap cap_sys_admin+p $(readlink -f $(which sunshine))`.
- **Fedora 43 reports (late 2025 / early 2026)**: Fedora forum threads
  ("Sunshine not working under Fedora 43 Gnome", "Sunshine No Longer Working on F43") show
  mixed results — GUI launch fails but terminal launch works; users report having to use the
  beta. VAAPI H.264/HEVC/AV1 encoders detect fine on AMD. Scaling quirks when streaming to
  non-PC viewers. **Workable, with friction.**
- **Portal path?** No — KMS capture path. This is the key differentiator against everything
  else in this list.
- **Audio**: Yes — captures default PulseAudio/PipeWire sink.
- **Setup**: Peer-to-peer only. Moonlight client pairs via PIN once, then reconnects directly.
  **No NAT traversal built-in** — this is the big gap. Options:
  - Tailscale/ZeroTier overlay (works fine, Moonlight sees it as "LAN")
  - Port-forward 47984/47989/47990/48010 TCP + 47998–48010 UDP (not realistic for residential)
  - [Moonlight Internet Hosting Tool](https://github.com/moonlight-stream/moonlight-internet-hosting-tool) (NAT-PMP/UPnP)
- **Latency**: Absolute best in class. Sub-frame encode+decode possible with NVENC/VAAPI;
  10–25 ms on LAN, 30–60 ms on well-routed WAN. Built for gaming.
- **Viewer**: Moonlight is Linux/Windows/macOS/iOS/Android/Chromebook/Apple TV/Raspberry Pi
  native. Unofficial WebRTC-based browser client exists (MrCreativ3001/moonlight-web-stream)
  but is a third-party project.
- **Multi-viewer**: Officially one client per session. Not designed for demos.
- **Auth**: Mutual pairing with a PIN; RSA cert pinning afterwards.
- **2026 status**: Active. F43 has real issues but the KMS path is the reason to try this first.

**Verdict**: **The only candidate in this track that meaningfully bypasses the Mutter portal
path.** That alone makes it the strongest technical answer to "Meet freezes". Pair it with
Tailscale for NAT traversal. Not great for 1-to-many demos, but ideal for pair programming.

---

## Candidate 4: wayvnc + NoVNC

- **What**: VNC server for wlroots-based Wayland compositors.
- **Wayland on F43 GNOME**: **Incompatible.** wayvnc depends on the `wlr-screencopy` protocol,
  which Mutter does not implement. README and multiple Fedora/Arch threads confirm: "GNOME and
  KDE are not supported". Dead end for this track.
- **Verdict**: Skip.

---

## Candidate 5: Parsec

- **What**: Proprietary low-latency remote-desktop / game streaming, relay-based.
- **Wayland on F43 GNOME**: "Wayland is in beta for some distros" but officially
  recommends X11 sessions; Flathub issue tracker has a pile of "Wayland issues" reports. Since
  F43 removed X11 GNOME, Parsec is effectively unsupported on a default F43 install.
- **License**: Proprietary. Relay hosted by Parsec (Unity).
- **Verdict**: Skip for F43 GNOME Wayland. Not a serious candidate.

---

## Candidate 6: Apache Guacamole

- **What**: Clientless HTML5 gateway that proxies VNC/RDP/SSH to a browser.
- **Role here**: Not a *capture* tool — it's a browser viewer. It still needs an RDP/VNC
  *source* on the host (i.e., gnome-remote-desktop). So Guacamole inherits any bug in GRD.
- **Wayland on F43**: Browser-viewer side is fine. Host side = GRD = same portal path.
- **Setup**: Java servlet (`guacd` + Tomcat) needs to run somewhere reachable by both viewer
  and host. Adds operational weight.
- **Verdict**: Useful *as a viewer* overlay on GRD if cross-platform browser-only clients matter
  more than installing a native RDP client. Doesn't solve the underlying capture problem.

---

## Candidate 7: waypipe

- **What**: Wayland equivalent of `ssh -X` — forwards individual Wayland surfaces over SSH.
- **Role here**: Per-application, not full-screen. Useful for "let me watch your editor"
  but *the editor actually runs on your machine and displays on mine* — that's the opposite
  of screen sharing. Doesn't show what's *on* the other dev's real screen.
- **Verdict**: Not a fit for the "watch my live session" use case. Tool exists, works on F43,
  wrong problem.

---

## Candidate 8: Raw PipeWire / GStreamer pipeline

- **What**: `gst-launch-1.0 pipewiresrc ! … ! rtph264pay ! udpsink` or similar, piping the
  portal-provided PipeWire node to RTP/WHIP/RTMP.
- **Wayland on F43 GNOME**: Uses `xdg-desktop-portal` to obtain the PipeWire node → **same portal
  path as Meet**. If Meet is broken, this will be broken the same way.
- **Verdict**: Not a differentiated solution. Useful only if a test of the portal path works
  reliably in isolation — in which case Meet would also work.

---

## Candidate 9: wf-recorder → ffmpeg → RTMP

- Same story as raw PipeWire. wf-recorder is wlroots-focused; on GNOME it falls back to the
  portal path. Skip.

---

## Candidate 10: Deskreen

- **What**: Electron app, host runs Deskreen, viewer opens a URL in any browser, sees the
  screen via WebRTC with a LAN discovery + QR-code pairing flow.
- **License**: AGPL-3.0.
- **Last release**: 2.0.x, quiet since 2023 — the project has slowed significantly. No 2026
  releases visible. Treat as "maintained-ish".
- **Wayland on F43 GNOME**: Electron-based → uses Chromium's getDisplayMedia → same portal path
  that's failing for Meet. This is almost certainly the same failure mode.
- **Setup**: Designed for LAN (phone/TV as second screen). Works over VPN/overlay networks too,
  but the host + viewer need to reach each other directly.
- **Verdict**: Skip — same portal bug, less active project.

---

## Comparison matrix

| Tool                  | Bypasses Mutter portal? | F43 GNOME Wayland in 2026                                                  | NAT/Relay                               | Audio        | Viewers                          | Latency                 | Multi-viewer      |
| --------------------- | ----------------------- | -------------------------------------------------------------------------- | --------------------------------------- | ------------ | -------------------------------- | ----------------------- | ----------------- |
| Sunshine + Moonlight  | **Yes (KMS)**           | Works with friction; KMS capture + cap_sys_admin; forum reports F43 quirks | No built-in — pair with Tailscale       | Yes          | Linux/Win/Mac/iOS/Android/Web    | **Best** (10–60 ms)     | No                |
| gnome-remote-desktop  | No (uses portal)        | First-party; F43 post-X11 removal reports of RDP instability               | None — needs VPN                        | Yes (stereo) | Any RDP client (best cross-plat) | Good (50–200 ms)        | Limited           |
| RustDesk              | No (uses portal)        | Experimental; flickering reported on F42 GNOME, F43 unverified             | **Built-in** (hbbs/hbbr, self-hostable) | Yes          | All major OS + web               | Good                    | No                |
| Apache Guacamole      | N/A — viewer only       | Inherits GRD issues                                                        | Gateway = fixed endpoint                | Via RDP      | Any browser                      | +20–40 ms vs native RDP | Yes (per-session) |
| wayvnc                | N/A                     | **Does not work on GNOME**                                                 | —                                       | —            | —                                | —                       | —                 |
| Parsec                | No                      | X11 only in practice                                                       | Parsec relay                            | Yes          | Win/Mac/Linux                    | Excellent               | No                |
| waypipe               | N/A (per-app)           | Works                                                                      | SSH                                     | No           | Linux only                       | Good                    | No                |
| PipeWire/gst pipeline | No                      | Same as portal bug                                                         | DIY                                     | DIY          | Any RTP/RTMP client              | Variable                | Via SFU           |
| wf-recorder           | No on GNOME             | Falls back to portal                                                       | DIY                                     | DIY          | Any                              | Variable                | Via relay         |
| Deskreen              | No (Electron/portal)    | Same portal bug likely                                                     | LAN-focused                             | Limited      | Any browser                      | Moderate                | Yes               |

---

## Top picks for live testing

### Pick 1: **Sunshine + Moonlight over Tailscale**

**Why**: It's the only candidate that structurally cannot hit the Mutter portal bug that is
breaking Meet. The KMS capture path reads the framebuffer below Mutter entirely. Latency is
best-in-class for pair programming. Moonlight clients exist for every platform the team uses.
Tailscale (already popular with dev teams) solves the NAT traversal gap cleanly — Moonlight
treats a Tailscale peer as LAN. Audio works. Install path is well-documented on Fedora,
even if the F43 GNOME experience has some rough edges (terminal-launch-only, scaling).

**Risks**: `cap_sys_admin+p` is a real elevation and security-conscious teams should note it.
Not designed for one-to-many demos. F43 forum reports suggest we'll need to diagnose a few
things (GUI launch failure, scaling), but the underlying capture works.

**Test plan**: Install via COPR or Flatpak, grant KMS cap, pair one Moonlight client over LAN,
then over Tailscale, measure latency, confirm audio, try a 30-min pair session.

### Pick 2: **gnome-remote-desktop over Tailscale** *(as a fallback / audit reference)*

**Why**: First-party, best cross-platform viewer story (any RDP client), stereo audio, zero
extra install on the viewer side for Windows/macOS users. If the Mutter portal bug turns out
to be fixed in a stable F43 update by test time, GRD becomes the simplest, most official
choice. Valuable as a reference point: **if GRD freezes the same way Meet does, that confirms
the bug is in Mutter/xdp-gnome and narrows the root-cause investigation**. If GRD *works*,
then the Meet freeze is likely Chromium-side and Sunshine becomes unnecessary.

**Test plan**: `grdctl rdp enable` + TLS cert + password; connect from a second machine over
Tailscale with `gnome-connections`. Compare freezing behaviour to Meet directly — this is a
diagnostic as much as a solution.

---

## Sources

- [RustDesk 1.4.3 Wayland multi-monitor (UbuntuHandbook, Oct 2025)](https://ubuntuhandbook.org/index.php/2025/10/rustdesk-released-1-4-3-with-multi-monitor-for-wayland-virtual-mouse/)
- [RustDesk Discussion #13112 — Fedora 42 KDE/GNOME Wayland flickering](https://github.com/rustdesk/rustdesk/discussions/13112)
- [RustDesk Issue #12507 — F42 GNOME 48 pointer misalignment](https://github.com/rustdesk/rustdesk/issues/12507)
- [RustDesk Linux docs](https://rustdesk.com/docs/en/client/linux/)
- [RustDesk self-host relay docs](https://rustdesk.com/docs/en/self-host/rustdesk-server-pro/relay/)
- [RustDesk PR #6675 — Wayland flatpak input via portal](https://github.com/rustdesk/rustdesk/pull/6675)
- [gnome-remote-desktop 49.1-1 in Fedora 43](https://packages.fedoraproject.org/pkgs/gnome-remote-desktop/gnome-remote-desktop/fedora-43.html)
- [GNOME/gnome-remote-desktop repo](https://github.com/GNOME/gnome-remote-desktop)
- [Setup Headless Multi-User Sessions — GNOME 48 (James North)](https://jamesnorth.net/post/grd-46-setup)
- [Fedora Discussion — Fedora 43 unusable in headless systems](https://discussion.fedoraproject.org/t/fedora-43-unusable-in-headless-systems/181096)
- [Fedora Discussion — Truly headless remote access and Wayland, Nov 2025](https://discussion.fedoraproject.org/t/truly-headless-remote-access-and-wayland-are-there-any-solutions-yet-nov-2025/173010)
- [Sunshine — GitHub repo](https://github.com/LizardByte/Sunshine)
- [Sunshine getting started — KMS capture requirements](https://docs.lizardbyte.dev/projects/sunshine/latest/md_docs_2getting__started.html)
- [Fedora Discussion — Sunshine not working under Fedora 43 GNOME](https://discussion.fedoraproject.org/t/sunshine-not-working-under-fedora-43-gnome/171746)
- [Fedora Discussion — Sunshine No Longer Working on F43](https://discussion.fedoraproject.org/t/sunshine-no-longer-working-on-f43/171049)
- [Moonlight web stream (WebRTC viewer)](https://github.com/MrCreativ3001/moonlight-web-stream)
- [wayvnc README — wlroots only](https://github.com/any1/wayvnc)
- [Parsec Flathub — Wayland issues tracker](https://github.com/flathub/com.parsecgaming.parsec/issues/6)
- [Parsec Linux support page](https://www.parsec.com/support/os-support/linux.php)
- [Apache Guacamole + gnome-remote-desktop RDP troubleshooting](https://discourse.gnome.org/t/gnome-remote-desktop-rdp-problem-with-apache-guacamole/20703)
- [waypipe man page (Arch)](https://man.archlinux.org/man/extra/waypipe/waypipe.1.en)
- [Deskreen homepage](https://deskreen.com/)
- [GNOME Wayland Remoting hackfest notes (PipeWire architecture)](<https://wiki.gnome.org/Hackfests(2f)WaylandRemoting.html>)
- [Jan Grulich — how to enable Wayland screen sharing with PipeWire + portal](https://jgrulich.cz/2018/07/04/how-to-enable-and-use-screen-sharing-on-wayland/)
