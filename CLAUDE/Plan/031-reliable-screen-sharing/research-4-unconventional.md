# Research 4: Unconventional / Outside-the-Box Screen Sharing

**Track**: Research agent 4 of 4 — creative / lateral approaches
**Scope**: Fedora 43, GNOME Wayland, WFH devs behind residential NAT
**Constraint summary**: Slack's screen share path is hard-disabled in `app.asar`;
Google Meet freezes within seconds due to the mutter `ScreenCast` bug; no shared
LAN available.

The brief: sidestep the broken Wayland portal stack entirely. Assume the
"obvious" answers (Jitsi self-host, Zoom/Teams, rustdesk, direct VNC) are covered
by the other three research agents.

---

## Guiding Insight

The single most important observation: **the Wayland ScreenCast portal is broken,
but the PipeWire *camera* path and V4L2 userspace APIs are completely fine.**
Slack hard-disables the screencapture path in `app.asar`, but Slack still happily
opens a webcam via the standard browser/WebRTC `getUserMedia()` flow — and no
amount of Electron lockdown can reasonably disable that without breaking
video calls. Several of the creative options below exploit exactly this
asymmetry.

---

## Category A — Hardware-Based Bypass

### A1. HDMI → USB UVC capture card (laptop loop-through)

**Idea**: Route the laptop's HDMI output into a UVC-compliant USB capture card
plugged into the same laptop. The desktop image now appears as `/dev/video1`, a
plain webcam. Slack, Meet, Teams, anything — they all see a camera, which works.

- **Hardware**: Cheap generic HDMI→USB UVC dongles (MacroSilicon MS2109/MS2130
  chipset) are USD 10–25 on Amazon/AliExpress. Mid-tier (ATEN CAMLIVE UC3020,
  StarTech UVCHDCAP, AJA U-TAP HDMI) are USD 100–250 and properly 1080p60/4K30.
  Elgato HD60 X / S+ is USD 150–200.
- **Linux compat**: UVC is natively supported by the Linux kernel `uvcvideo`
  driver — truly plug-and-play on Fedora 43. Confirmed working on
  macOS/Windows/Linux for the ATEN, StarTech, AJA, and MOKOSE lines.
- **Setup complexity**: Plug HDMI-out → dongle → USB port. Enable mirror display
  (or extended). Done. No kernel modules, no Secure Boot headaches.
- **Good for**: Synchronous sharing in *any* videoconf app. Zero dependency on
  Wayland portals. Audio goes via whatever the app already uses.
- **Bad for**: Requires a laptop with working HDMI out, a free USB port, and USD
  10+ per developer. Not useful for audio-only or text collab. Adds 50–200 ms
  latency typical for UVC capture.
- **Sidesteps the Wayland bug?** Completely — never touches xdg-desktop-portal
  or mutter's ScreenCast D-Bus.
- **2026 state**: Mature. NearStream, ATEN, Elgato all ship 4K60 UVC cards in
  2026; driver-free on Linux.

This is the **"pretend the screen is a webcam"** approach — silly-sounding,
physically real, and immune to every software-layer bug in the stack.

### A2. Raspberry Pi as loopback capture device

Feed laptop HDMI into a Pi, have the Pi re-emit over USB gadget mode or
network. Works but adds Pi + HDMI cable + USB-C gadget configuration. Strictly
worse than A1 unless a Pi is already sitting on the desk. Skip.

---

## Category B — Broadcast / Virtual-Webcam Bypass

### B1. OBS Studio + v4l2loopback → virtual webcam (TOP PICK)

**Idea**: OBS captures the screen via PipeWire (same broken portal — but OBS
reconnects cleanly after the mutter freeze, unlike Meet which just dies). OBS
outputs that content into a `/dev/video10` virtual camera created by
`v4l2loopback`. Slack/Meet/Teams then *select that "webcam"* as their camera
input. You share your screen while every app thinks you're showing your face.

- **Hardware**: None.
- **Software**: OBS Studio (Fedora RPMFusion), `kmod-v4l2loopback` +
  `akmod-v4l2loopback` from RPMFusion. Load the module with
  `modprobe v4l2loopback devices=1 exclusive_caps=1 card_label="OBS Cam"`.
- **Setup complexity**: Medium. Fedora 43 enforces kernel module signing under
  Secure Boot — either sign the akmod manually (`mokutil --import`) or disable
  Secure Boot. There is a known-good community fix
  (AnthonyLloydDotNet/obs-fedora-virtualcam-fix) for Fedora 41–43.
- **Good for**: Synchronous sharing in *any* WebRTC-based app including Slack
  huddles, Meet, Teams, Discord. Works where the screencast path is blocked,
  because the app is using the camera path. Audio untouched. Can overlay
  webcam, swap scenes, hide sensitive info.
- **Bad for**: Image is presented as a "webcam", so Meet/Slack UIs mirror and
  cap at 720p/1080p depending on app negotiation. Meeting participants see it
  in the camera tile, not as a shared-screen pin — slightly awkward social UX.
  Also, OBS itself still uses PipeWire ScreenCast under the hood, so if mutter
  is the root cause of freezes, OBS may also freeze. *However*, OBS's PipeWire
  source is widely reported as much more robust than Chromium's — it reconnects
  on error rather than silently deadlocking.
- **Sidesteps the Wayland bug?** Partially. It reuses the portal but
  encapsulates it; recovery from portal hiccups is far better than in-app.
- **2026 state**: OBS 30/31 ship first-class PipeWire and WHIP support.
  `v4l2loopback` is actively maintained; `exclusive_caps=1` is the magic flag
  that lets Chromium/Electron see the device.

**This is the highest leverage unconventional pick** because it weaponises the
working camera path against the broken screen-share path, costs zero dollars,
and works today on stock Fedora 43 + RPMFusion.

### B2. OBS → WHIP/WHEP → self-hosted MediaMTX → browser viewer

**Idea**: OBS ingests screen, pushes via WHIP (WebRTC HTTP Ingest Protocol) to
a self-hosted MediaMTX or Broadcast-Box instance. Viewers open an HTTPS URL in
any browser — WHEP egress delivers sub-second WebRTC video.

- **Infra needed**: A small VPS (USD 5/mo Hetzner/DO) running MediaMTX in Docker.
  TLS via Caddy or direct. Or Tailscale Funnel if you want zero public infra.
- **Setup**: MediaMTX config is ~20 lines YAML. OBS 30+ has native WHIP output
  under Service → WHIP.
- **Latency**: Reported \<1 s end-to-end with WebRTC transport.
- **Good for**: One-to-many viewing, async stragglers, mixed-timezone standups.
  No viewer install required.
- **Bad for**: One-way only — no voice back, no screen-control. Solves viewing
  but not conversation; pair with existing Slack voice huddle for audio.
- **Sidesteps Wayland bug?** Same caveat as B1 — OBS still uses PipeWire
  capture, but the transport is ours.
- **2026 state**: Mature. OBS WHIP is stable; MediaMTX is a flagship
  zero-dependency Go server with active WHIP/WHEP support.

### B3. OBS → RTMP → Owncast

Same shape as B2 but RTMP ingest with HLS egress. Higher latency (5–20 s), but
Owncast is a turnkey binary with chat and built-in web player. Good for
broadcast-style "I'll show you my refactor" sessions, bad for real-time back
and forth.

### B4. Unlisted YouTube / Twitch live stream as transport

Surprisingly viable for quick demos: OBS → RTMP → Twitch unlisted/YouTube
unlisted. Zero infra. 15–30 s latency rules out pair programming but perfectly
fine for "watch me reproduce this bug" handoffs. Free. Silly, but ships. Only
caveat: TOS around professional/internal use can be fuzzy, and your content
transits a third-party platform.

### B5. Peertube live streaming

Federated, self-hostable, but heavyweight for this use case. Skip unless you
already run a Peertube instance.

---

## Category C — Code Collaboration (No Screen Share at All)

### C1. VS Code Live Share

- Real-time co-editing, shared terminal, shared debug sessions, follow-mode.
- Cross-OS (Linux ↔ Mac ↔ Windows).
- Free (requires Microsoft/GitHub account).
- **No built-in audio / screen share** — pairs with Slack voice huddle.
- Sidesteps Wayland entirely; no screen pixels ever leave the host.
- 2026 state: Stable, actively maintained; works on VSCodium too.

### C2. JetBrains Code With Me — DEPRECATED

JetBrains officially announced March 2026 that Code With Me is being sunset.
2026.1 is the final release; public relays shut down Q1 2027. **Do not
standardise on this.** CodeTogether (commercial) is the likely
cross-IDE successor.

### C3. tmate

SSH-less shared tmux over the public tmate relay (or self-hosted).
Terminal-only, read-only or read-write. Ideal for CLI pair debugging,
awful for anything GUI. Free, instant, works behind any NAT. For teams
doing a lot of ssh/ops/kubectl debugging this is *genuinely* a screen
share replacement 80% of the time.

### C4. Sourcegraph / mob.sh / git-pair

Async pair-programming via rapid-fire commits. Works but requires
discipline. Worth mentioning as an option when latency is pathological
(e.g., devs on opposite poles of the planet).

---

## Category D — Network-Level Tricks

### D1. Tailscale + "LAN-only" screen share tool

Huge win. Many screen-share tools (Deskreen, SparkleShare, local HTTP servers,
even raw `gst-launch` RTP) assume LAN trust and are brittle on public internet.
Tailscale makes every laptop a peer on a zero-config mesh VPN; every LAN-only
tool now "just works" over the internet, encrypted, NAT-traversed.

- **Setup**: `dnf install tailscale; tailscale up`. Done.
- **Cost**: Free for personal / USD 6/user/mo business.
- **Good for**: Enabling every tool from the other research agents' lists.
- **Sidesteps Wayland bug?** Not directly — it's a transport. But it turns
  your 4-dev WFH team into a single LAN, which is the missing prerequisite for
  a dozen unconventional approaches (Deskreen, OBS → VLC RTSP, plain NDI,
  Barrier, etc.).

### D2. Tailscale Funnel exposing local OBS/MediaMTX

Publish a local Owncast/MediaMTX/RTMP to the public internet without any port
forwarding. HTTPS, automatic cert, zero config. Funnel is HTTP/HTTPS-only, so
WebRTC signalling works; UDP media still needs the tailnet itself, not Funnel.

### D3. ZeroTier

Same shape as Tailscale. Marginally more fiddly. If one is already standardised,
stick with it.

---

## Category E — Asymmetric / Low-Tech

### E1. Point a phone at the laptop screen — literally

Sounds absurd. Works. Use the phone as a WebRTC camera (via "NDI HX Camera",
"Camo", "DroidCam", or `scrcpy`-reverse). The phone joins the Slack huddle as a
second participant whose "face" is the laptop screen. Zero Wayland involvement.

- Good for: emergency demos, 5-min unblocker calls.
- Bad for: anything >10 min, shaky hands, poor OCR-ability of code.
- Cost: zero (everyone has a phone).

### E2. Second device as the sharer

Share screen from a secondary device that *isn't* on broken Wayland — e.g.
a personal MacBook mirroring via USB-C from the Linux laptop into Zoom. Ugly,
but a reliable fallback for the one exec call a quarter where it absolutely
must work.

### E3. Async screen recording: Cap.so / Loom / asciinema

For 70%+ of "can you show me…?" requests, a 90-second recording is *better*
than a live share — searchable, rewatchable, timezone-free.

- **Cap.so** (2026): open-source Loom alt, cross-platform, custom S3 bucket
  support. Active development.
- **asciinema**: terminal sessions only, tiny `.cast` files, embeddable.
  Perfect for CLI walkthroughs.
- **Loom**: commercial, works but macOS/Windows only on desktop; browser
  recording works on Linux.

Encourage a team norm of "record first, share live only if needed." Huge
hidden productivity win even when screen-share works.

---

## Comparison Matrix

Sorted roughly by creative-cost vs payoff (best first):

| #   | Approach                            | Cost       | Setup   | Sync? | Sidesteps Wayland? | Payoff |
| --- | ----------------------------------- | ---------- | ------- | ----- | ------------------ | ------ |
| B1  | OBS + v4l2loopback → virtual webcam | Free       | Medium  | Yes   | Mostly             | HIGH   |
| C1  | VS Code Live Share                  | Free       | Easy    | Yes   | Fully              | HIGH   |
| D1  | Tailscale + LAN-only tools          | Free       | Easy    | Yes   | Enabler            | HIGH   |
| E3  | Cap.so / asciinema async recording  | Free       | Easy    | No    | Fully              | HIGH   |
| C3  | tmate (CLI pair)                    | Free       | Easy    | Yes   | Fully              | HIGH\* |
| A1  | HDMI → USB UVC capture card         | USD 10–200 | Easy    | Yes   | Fully              | MED    |
| B2  | OBS → WHIP → MediaMTX               | USD 5/mo   | Medium  | Yes   | Mostly             | MED    |
| B3  | OBS → RTMP → Owncast                | USD 5/mo   | Medium  | ~     | Mostly             | MED    |
| E1  | Phone pointed at laptop screen      | Free       | Trivial | Yes   | Fully              | LOW    |
| B4  | OBS → unlisted YouTube/Twitch live  | Free       | Easy    | ~     | Mostly             | LOW    |
| C2  | JetBrains Code With Me              | Free       | Easy    | Yes   | Fully              | DEAD   |

\* HIGH for teams doing CLI/ops work; LOW for GUI-heavy work.

---

## Top Picks to Test Seriously

### 🥇 Pick #1 — OBS + v4l2loopback virtual webcam (B1)

**The lateral insight**: Slack's webcam path is fully operational because
disabling it would kill video calls. We hijack that path. Screen content
becomes "camera content" and every videoconf tool on the market works
unmodified. Free, one-time Ansible setup, works on Fedora 43 once the
akmod-v4l2loopback secure-boot story is handled (or disabled).

**Test plan**:

1. Ansible: install `obs-studio`, `akmod-v4l2loopback`, add modprobe config.
2. Document Secure Boot caveat in team README.
3. Create an OBS scene: single "Screen Capture (PipeWire)" source + optional
   webcam overlay in corner.
4. Verify Slack huddle / Google Meet / Teams all see "OBS Virtual Camera" as
   selectable webcam and do not freeze after 5 minutes.
5. If OBS's PipeWire source also suffers the mutter bug: switch to
   `xcomposite` window capture under XWayland, or feed OBS from A1
   (HDMI capture card) as the ultimate fallback.

### 🥈 Pick #2 — VS Code Live Share + Tailscale-enabled Slack huddle for voice (C1 + D1)

For 80% of this team's actual day-to-day ("can you pair on this?"), Live
Share is strictly better than screen sharing: both devs can type, the
remote dev sees syntax highlighting at their font size, and latency is
measured in keystrokes not frames. Audio continues to ride Slack
huddles, which work fine (huddles without video/screen-share work).

**Why this pair**: Live Share solves the sync-collab use case cleanly and
eliminates the screen-share requirement from the most common workflow.
Tailscale is added because it unlocks *the other agents'* self-host
options as a fallback when Live Share doesn't fit (e.g. showing a GUI
app, a browser DevTools session, a GIMP/Figma flow).

### Honourable mentions worth piloting cheaply

- **HDMI UVC dongle (A1)** — USD 15 per dev, stocked as "break glass" kit for
  the one exec demo where nothing else is allowed to fail.
- **asciinema (E3)** — mandate it for any "look at this terminal bug" share.
  10× better than Slack-huddle screenshare for CLI content; asynchronous.
- **OBS → Tailscale Funnel → MediaMTX (B2 + D2)** — if the team grows or
  needs many-viewer internal demos without inviting external participants.

---

## Lateral Thought Bonus

The reason **B1 (virtual webcam)** is the genuine outside-the-box winner:
every vendor who locks down screen share (Slack, corporate-hardened Teams,
paranoid browser extensions) does so because screen content contains
secrets. None of them lock down the webcam path because doing so would
break video calls. By making the screen *be* the webcam, we exploit a
policy gap that is load-bearing for every one of these products'
business models, and thus will never be closed. This trick will outlive
the mutter bug, the next portal redesign, and probably Slack itself.

---

## Sources

- [OBS virtual cam on Linux — OBS Forums](https://obsproject.com/forum/threads/obs-virtual-cam-on-linux.134849/)
- [OBS and Virtual Cam (Linux) — Mathieu Acher blog](https://blog.mathieuacher.com/OBSVirtualCam/)
- [Enable virtual camera in OBS Studio on Fedora — wains.be](https://blog.wains.be/2021/2021-10-03-obs-studio-virtual-camera-fedora/)
- [AnthonyLloydDotNet/obs-fedora-virtualcam-fix](https://github.com/AnthonyLloydDotNet/obs-fedora-virtualcam-fix)
- [Trying to install v4l2loopback on Fedora 42 — Fedora Discussion](https://discussion.fedoraproject.org/t/trying-to-install-v4l2loopback-on-fedora-42/152376)
- [OBS Virtual Camera Enable — Fedora Discussion](https://discussion.fedoraproject.org/t/obs-virtual-camera-enable/31095)
- [screen-share-sway — hw0lff/screen-share-sway on GitHub](https://github.com/hw0lff/screen-share-sway)
- [OBS screen capture freezes after screen lock/unlock (Wayland/PipeWire)](https://obsproject.com/forum/threads/obs-screen-capture-freezes-after-screen-lock-unlock-wayland-pipewire.194546/)
- [OBS Studio + WebRTC: Building and Testing Ultra-Low Latency Streaming](https://telecom.altanai.com/2026/03/17/obs-studio-webrtc-building-and-testing-ultra-low-latency-streaming/)
- [MediaMTX on GitHub](https://github.com/bluenviron/mediamtx)
- [OBS WHIP Streaming Guide](https://obsproject.com/kb/whip-streaming-guide)
- [Ultra Low Latency Streaming with OBS, WHIP, WHEP and Broadcast Box — Medium](https://medium.com/@contact_45426/ultra-low-latency-streaming-with-obs-whip-whep-and-broadcast-box-fa649bf87fbe)
- [VDO.Ninja WHIP/WHEP client](https://vdo.ninja/whip)
- [Owncast — self-hosted live streaming](https://owncast.online/)
- [VS Code Live Share — Microsoft](https://visualstudio.microsoft.com/services/live-share/)
- [Sunsetting Code With Me — JetBrains Platform Blog, March 2026](https://blog.jetbrains.com/platform/2026/03/sunsetting-code-with-me/)
- [CodeTogether Cloud — Eclipse Marketplace](https://marketplace.eclipse.org/content/codetogether-cloud)
- [tmate — instant terminal sharing](https://tmate.io/)
- [Cap — open source Loom alternative](https://cap.so/)
- [CapSoftware/Cap on GitHub](https://github.com/CapSoftware/Cap)
- [asciinema](https://asciinema.org/)
- [Tailscale Funnel docs](https://tailscale.com/docs/features/tailscale-funnel)
- [Tailscale Funnel examples](https://tailscale.com/kb/1247/funnel-examples)
- [How to Use Tailscale Funnel to Share Local Apps Securely (2025 Guide) — subnetsavy](https://subnetsavy.com/wp-content/uploads/articles/tailscale-funnel-guide.html)
- [RTCwebcam — use any device's camera as a webcam on Linux](https://github.com/nagi1999a/RTCwebcam)
- [Best HDMI Capture Cards for Streamers (2025) — NearStream](https://www.nearstream.us/blog/best-video-capture-hdmi-card)
- [U-TAP HDMI — AJA Video Systems (UVC on Linux)](https://www.aja.com/products/u-tap-hdmi)
- [ATEN CAMLIVE UC3020 HDMI→USB-C UVC Capture](https://www.amazon.com/UC3020-ATEN-webcasting-conferencing-Compatible/dp/B07N1HQ41X)
- [StarTech UVCHDCAP — HDMI to USB-C UVC 1080p60 capture](https://www.startech.com/en-us/audio-video-products/uvchdcap)
- [MOKOSE USB 3.0 HDMI/SDI Video Capture (Linux-compatible UVC)](https://www.mokose.com/products/mokose-usb3-0-hdmi-sdi-video-capture-card-for-windows-linux-os-x-mac-hd-loop-thru-game-dongle-grabber-device-1080p-60fps-uvc-free-driver-box)
- [How to use OBS video output for video conferences on Linux — Stanford SCS](https://www.scs.stanford.edu/~dm/blog/hide-webcam.html)
- [Make virtual meetings better with this OBS trick — opensource.com](https://opensource.com/article/20/8/obs-virtual-webcam)
