# Fedora 43 GNOME Screen Sharing — Research Report

Investigation date: 2026-04-07
Author: Claude (Opus 4.6)
Target system: Fedora 43 Workstation, GNOME 49 (Wayland-only), kernel 6.17.x

---

## 1. Executive Summary

- **Slack desktop (RPM and Flatpak) is broken by design.** Slack ships an Electron build that has the WebRTC PipeWire capturer hard-disabled in `app.asar`. Adding `--enable-features=WebRTCPipeWireCapturer` to the launcher does **not** work in current Slack 4.46–4.49 because the hardcoded disable wins over the user flag. The community fix is to binary-patch the string in `app.asar` (rename `WebRTCPipeWireCapturer` → `LebRTCPipeWireCapturer`) so the disable list can no longer find the feature. This is the only known fix for the native Slack client. The reliable alternative is the Slack web app in Chrome/Chromium (which has WebRTC PipeWire on by default since Chrome 110).
- **Google Meet's "freezes after a few seconds" symptom matches a known mutter ScreenCast bug** that was fixed upstream in mutter 49.4 (MR !4881 / !4798, "screen-cast-stream-src: Only specify framerate range if there is any") and a related damage-region bug fixed in mutter 49.3 ("Fix reporing damage region in pipewire streams"). **Fedora 43 stable shipped with mutter 49.0 → 49.1.1 (October 2025) → 49.3 (early February 2026) → 49.5 (mid-March 2026)**. If the user upgraded recently, `mutter-49.5-1.fc43` should already contain both fixes; if they have not pulled updates since early 2026, that is the proximate cause of the freeze. (`mutter-49.4` was skipped on Fedora — Fedora went straight from 49.3 to 49.5.)
- **There is no GNOME-on-X11 escape hatch on Fedora 43.** Per the WaylandOnlyGNOME change, the X11 GDM session and the entire `gnome-session-xsession` package are removed from F43 repos. The only X11 fallback is to switch desktop environment (Plasma X11, Cinnamon, MATE). This is a significant constraint on troubleshooting.
- **Recommended fixes, in order of cost:**
  1. `sudo dnf upgrade --refresh` to pull `mutter-49.5-1.fc43` and `gnome-shell-49.5-1.fc43` (March 2026) — fixes the Meet freeze for free if not yet applied.
  2. For Slack: switch to the **Slack web app in Chrome** (or Chromium native RPM). Pin the app via Chrome's "Install Slack" PWA action. This is the lowest-friction fix and works today.
  3. If the desktop Slack client is mandatory, apply the `app.asar` patch (post-install hook).
  4. Disable Chrome/Chromium **hardware acceleration** (`chrome://settings/system`) — known to fix PipeWire freezes on AMD/Mesa 25.x on Fedora 43, at the cost of higher CPU.
  5. Last resort: install KDE Plasma (Wayland) alongside GNOME, or move team to Jitsi/Element which work in Firefox without flags.
- **Known questions** (need user input): GPU vendor (Intel/AMD/NVIDIA), exact mutter version (`rpm -q mutter`), exact Chrome version, whether the user upgraded mutter past 49.3.

---

## 2. Current State of the Stack

### 2.1 Fedora 43 / GNOME 49 facts

- **Fedora 43** released **2025-10-28**. Ships GNOME 49 "Brescia", kernel 6.17, Anaconda WebUI, Python 3.14. ([Fedora Magazine](https://fedoramagazine.org/whats-new-fedora-workstation-43/))
- **GNOME X11 session removed.** Fedora 43 implements [Changes/WaylandOnlyGNOME](https://fedoraproject.org/wiki/Changes/WaylandOnlyGNOME): `gnome-session-xsession` and the GNOME-on-Xorg packages are no longer in the F43 repos. GDM no longer offers an X11 GNOME session. X11 *applications* still run via XWayland, but the *session* cannot be GNOME on X11. Users who need an X11 session must switch DE entirely (KDE Plasma X11, Cinnamon, MATE, etc.). ([Phoronix](https://www.phoronix.com/news/F43-Change-Wayland-Only-GNOME), [GNOME blog](https://blogs.gnome.org/alatiera/2025/06/23/x11-session-removal-faq/))
- **GNOME 49 / mutter version timeline in F43 (from bodhi):**
  - `mutter-49~alpha.1-3.fc43` (~July 2025, pre-release)
  - `mutter-49.0-3.fc43`, `49.0-4.fc43` (~September 2025)
  - `mutter-49.1.1-1.fc43` (~October 2025, GA)
  - `mutter-49.3-1.fc43` (~2026-02-05, FEDORA-2026-6b2e10bdb8) — includes "Fix reporing damage region in pipewire streams"
  - `mutter-49.5-1.fc43` (~2026-03-18, FEDORA-2026-d90ae90842) — includes the !4881 framerate-range backport from 49.4
  - **Fedora skipped mutter-49.4** on F43; the 49.4 fixes landed in F43 via the 49.5 update.
  - Source: [bodhi mutter F43](https://bodhi.fedoraproject.org/updates/?packages=mutter&releases=F43)
- **PipeWire on F43:** `pipewire-1.4.11-1.fc43`. (F44/rawhide has 1.6.x.) Note: the upstream MR !4798 description specifically mentions the framerate-range bug being triggered by **PipeWire 1.5.x** WebRTC negotiation behaviour, so on 1.4.11 the framerate-range bug may be partially mitigated, but the damage-region bug is independent. ([Fedora packages: pipewire](https://packages.fedoraproject.org/pkgs/pipewire/pipewire/index.html))
- `xdg-desktop-portal-gnome` is the active portal backend on F43 (replaces gtk for GNOME-specific actions; gtk portal still installed for fallback).
- Hardware video acceleration (VA-API) for Chrome/Chromium on Fedora 42/43 has been **regression-prone** since the **Mesa 25.x** transition. Multiple Fedora Discussion threads from October 2025 → January 2026 report Brave/Chromium/Chrome dropping to software rendering, lag, and visual corruption on Wayland. Workaround used by other Fedora users: pin Chromium 141.0.7390.54-1.fc42 or earlier. F43 does not allow downgrading Mesa to 24.x (no 24.x in repos). ([Fedora Discussion: Wayland and Chromium/Brave GPU acceleration is suddenly broken](https://discussion.fedoraproject.org/t/wayland-and-chromium-brave-gpu-acceleration-is-suddenly-broken-videos-hella-laggy/167520))

### 2.2 How screen sharing works on F43 GNOME (the layered model)

```
Application (Slack/Chrome/Firefox/Zoom/OBS)
        │
        │  D-Bus: org.freedesktop.portal.ScreenCast
        ▼
xdg-desktop-portal  ──────►  xdg-desktop-portal-gnome  (GNOME backend impl)
        │                            │
        │                            │  D-Bus: org.gnome.Mutter.ScreenCast
        ▼                            ▼
   PipeWire daemon  ◄──── mutter (capture + dmabuf into PipeWire stream)
        │
        │  PipeWire node (video stream w/ frames)
        ▼
Application reads frames via PipeWire client lib
```

- The **portal** is the user-facing security gate ("share which screen?"). Without a portal backend (xdg-desktop-portal-gnome), nothing can capture the screen on Wayland.
- **mutter** is the actual frame source. It uses `screen-cast-stream-src` (a GStreamer-style PipeWire source) to feed dmabuf frames into a PipeWire node.
- The application either uses **PipeWire directly** (Firefox, Chrome since 110, OBS, gnome-remote-desktop, Zoom 6.x), or it uses an **Electron build that bundles WebRTC's PipeWire capturer** (Slack, Discord, Element, Teams desktop, Vesktop). For Electron apps the feature is gated by the Chromium feature flag `WebRTCPipeWireCapturer`, which is controlled at compile time (Slack disables it) or via `--enable-features=WebRTCPipeWireCapturer`.
- **No more X11 fallback** on GNOME means there is no `xdpyinfo`-based capture path; everything must go through this stack. Apps that historically only worked under X11 (old Zoom, old Slack, Teams classic) are completely dead in GNOME-on-F43.

---

## 3. Slack-Specific Findings

### 3.1 The root cause: Slack hardcodes the WebRTCPipeWireCapturer disable

- Multiple sources (AUR `slack-desktop-wayland` PKGBUILD comments, the [PaulDance/patch-slack](https://github.com/PaulDance/patch-slack) repo, [Hacker News thread March 2024](https://news.ycombinator.com/item?id=39630249) with comment from `argulane`, and the [Arch Linux forum thread](https://bbs.archlinux.org/viewtopic.php?id=292904)) all converge on the same explanation:

  > "The Slack desktop client for Linux has native support for using PipeWire to capture and share screen contents under Wayland. However, the `WebRTCPipeWireCapturer` option for its `--enable-features` CLI flag that enables the feature is apparently ignored on purpose in the current version."
  > — [PaulDance/patch-slack README](https://github.com/PaulDance/patch-slack)

- The mechanism: Slack's bundled JS in `/usr/lib/slack/resources/app.asar` has a hardcoded list that **disables** `WebRTCPipeWireCapturer` regardless of any `--enable-features` flag the user passes. So both of these are no-ops on current Slack:
  ```
  /usr/bin/slack --enable-features=WebRTCPipeWireCapturer
  Exec=/usr/bin/slack --enable-features=WebRTCPipeWireCapturer %U
  ```
  These instructions appear in many old (2022) blog posts (Guy Rutenberg, raelcunha) and are **stale** for current Slack versions.

### 3.2 The community fix: binary-patch app.asar

The known working hack:

```bash
sudo sed -i \
  's/,"WebRTCPipeWireCapturer"/,"LebRTCPipeWireCapturer"/' \
  /usr/lib/slack/resources/app.asar
```

This renames the string in the disable list to `LebRTCPipeWireCapturer` (which doesn't exist as a feature), so the disable list misses, the default-on Chromium feature stays on, and PipeWire capture works. Source: [PaulDance/patch-slack](https://github.com/PaulDance/patch-slack), AskUbuntu answer 1492207 (referenced from the HN thread).

Caveats:
- Has to be re-applied every time Slack auto-updates (each `dnf update slack`).
- ASAR integrity checks have been tightened in Electron post-CVE-2025-55305, but as of slack-desktop-wayland AUR `4.47.69-1` (December 2025) the simple sed still works because Slack doesn't appear to enforce ASAR integrity for its own bundled assets.
- **This is unsupported by Slack.** It will be fragile across version bumps.

### 3.3 Flatpak Slack situation

- The flathub Slack manifest (`com.slack.Slack`) merged [PR #118 (vchernin)](https://github.com/flathub/com.slack.Slack/pull/118) in May 2021 to build the Flatpak runtime *with* PipeWire support, fixing the original "Issue #101: Screen sharing on Wayland" — but the same WebRTCPipeWireCapturer hard-disable in `app.asar` lives inside the Slack tarball that the Flatpak unpacks. So the runtime is correct, but the bundled Slack still self-disables the feature.
- [Issue #196](https://github.com/flathub/com.slack.Slack/issues/196) (April 2023, "Slack crashes in share screen attempt in Wayland — regression") was closed as "not planned"; the workaround there was to pin an older flatpak commit.
- The [Fedora Discussion thread "Screen Sharing Broken in F43"](https://discussion.fedoraproject.org/t/screen-sharing-broken-in-f43/179819) (kyzu0, January 2026) reports the same: Slack window-share works, full-screen-share is black. User confirmed that **uninstalling Flatpak versions and using native RPMs improved things**, but did not specifically resolve Slack's hardcoded disable.

### 3.4 Slack release notes (verified for 2025-2026)

From [slack.com/release-notes/linux](https://slack.com/release-notes/linux):
- **4.46.104** (2025-10-21) — "Minor fixes."
- **4.47.69** (2025-12-11) — "Small adjustments."
- **4.49.81** (2026-04-03) — "Minimal bug fixes."

**No Wayland or screen-sharing fixes** in any release notes from late 2024 onward. The 2024 announcement of Slack "fixing Wayland screen sharing" (4.38.115, [OMG! Ubuntu](https://www.omgubuntu.co.uk/2024/05/slack-linux-app-fixes-screen-sharing-under-wayland)) appears to have been *partial* — it added the ability for window sharing, but the WebRTCPipeWireCapturer hard-disable was reintroduced or never fully removed for full-screen capture. Multiple users in late-2025 / early-2026 threads confirm full-screen sharing is still broken.

### 3.5 Slack web app — the actually-working option

- The Slack web app at `https://app.slack.com` runs in any Chromium-based browser. Chrome 110+ has `enable-webrtc-pipewire-capturer` **on by default**, so screen sharing in Slack-via-Chrome on Fedora 43 GNOME Wayland Just Works (subject to the GNOME 49 framerate/damage bug below).
- Firefox can run Slack web too, but Slack disables some features in Firefox (the Slack help docs explicitly recommend Chromium-family browsers).
- You can install Slack as a Chrome PWA: Chrome menu → Cast, save, and share → Install page as app… → choose Slack. This gives a windowed, taskbar-pinnable Slack experience that uses Chrome's working portal pipeline.
- **This is the recommended fix for the user.**

---

## 4. Google Meet–Specific Findings

### 4.1 Symptom matches a known mutter regression

User report: "Google Meet screen sharing works briefly then stops updating — frame freezes."

This **exactly matches** the symptom described in two upstream mutter issues:

1. **MR !4798 (merged December 2025) → backported as !4881 → released in mutter 49.4 / Fedora's mutter 49.5**
   Title: "screen-cast-stream-src: Only specify framerate range if there is any"
   Reporter: Jonas Ådahl
   Description: "When recording monitors lacking a defined refresh rate, the framerate range is unconditionally specified. Including a zero-value framerate (0/1) alongside a defined rate causes the WebRTC negotiation in PipeWire 1.5.x to treat 0 as mathematically smaller than 60, breaking codec negotiation. Fix is to only emit the framerate range if there *is* a real framerate."
   Affects: virtual monitors, monitors without reported refresh rate, and (per the comments) some external displays.
   Source: GNOME GitLab MR [4798](https://gitlab.gnome.org/GNOME/mutter/-/merge_requests/4798) and [4881](https://gitlab.gnome.org/GNOME/mutter/-/merge_requests/4881)

2. **mutter 49.3 changelog: "Fix reporing damage region in pipewire streams"** [sic — typo "reporing" is in the actual upstream changelog]
   Releases page: [newreleases.io mutter 49.3](https://newreleases.io/project/gnome-gitlab/GNOME/mutter/release/49.3)
   This is the **smoking gun for the freeze symptom**. PipeWire screen-cast streams use *damage tracking* — the compositor only sends a new frame when it detects that some region of the screen has changed. If mutter computes the damage region wrong, the compositor stops sending frames until something forces a full repaint (e.g., the user moves the mouse or focuses a window). The receiver sees the last frame frozen.
   This precisely matches the [Fedora Discussion thread (Feb 2025)](https://discussion.fedoraproject.org/t/fedora-screen-share-remote-desktop-freezing-until-i-move-something/145313): "screen sharing in Discord, Vesktop, Zoom, Teams becomes very laggy until I move something on my screen … approximately one frame every 5 seconds until mouse movement."

### 4.2 What this means for the user

If they are running **`mutter-49.0` through `mutter-49.1.1`** (the F43 GA versions, October 2025 — January 2026), they have **both** bugs:
- The damage-region bug (causes freeze-until-movement on any screen).
- The framerate-range bug (causes freeze on monitors without a stable reported refresh rate, including some virtual displays).

Mutter 49.3 (Feb 2026) fixes the damage region bug. Mutter 49.5 (Mar 2026) adds the framerate fix. If the user has not run `dnf upgrade` since early February 2026, they're on the buggy version.

**Recommended first action:**
```bash
rpm -q mutter gnome-shell xdg-desktop-portal xdg-desktop-portal-gnome pipewire
sudo dnf upgrade --refresh mutter gnome-shell xdg-desktop-portal-gnome pipewire
# Then log out + log back in (mutter requires a session restart).
```

### 4.3 Other contributing factors for Google Meet freeze

- **Hardware video acceleration in Chrome on Mesa 25.x**: confirmed regression on F42/F43 ([Fedora Discussion 167520](https://discussion.fedoraproject.org/t/wayland-and-chromium-brave-gpu-acceleration-is-suddenly-broken-videos-hella-laggy/167520), thread runs from October 2025 to January 2026). Symptom: Chrome-based browsers report `chrome://gpu` SwANGLE software rendering on Wayland, despite working hardware acceleration on X11. Affects AMD GPUs primarily. **Workaround**: turn off hardware acceleration in `chrome://settings/system` → "Use graphics acceleration when available". Costs CPU but avoids the freeze and the broken VA-API path.
- **xdg-desktop-portal version mismatch**: if `xdg-desktop-portal` is newer than `xdg-desktop-portal-gnome`, the GNOME backend can fail to register the ScreenCast portal. Verify with `systemctl --user status xdg-desktop-portal-gnome`. Reinstall both: `sudo dnf reinstall xdg-desktop-portal xdg-desktop-portal-gnome`.
- **Multi-portal-backend conflict**: per [mylinuxforwork/dotfiles issue 1106](https://github.com/mylinuxforwork/dotfiles/issues/1106), having `xdg-desktop-portal-gtk` *and* `xdg-desktop-portal-gnome` *and* `xdg-desktop-portal-kde` simultaneously can cause Chrome to pick the wrong backend and produce a black screen. On a vanilla Fedora 43 Workstation this should not be the case, but worth checking: `dnf list installed 'xdg-desktop-portal*'`.

### 4.4 Browser pivot: Firefox

- Firefox has had stable PipeWire ScreenCast on Wayland since Firefox 84+ (default-on since ~Firefox 110, ~2023). On Fedora 43 with the official Firefox RPM, screen sharing in Google Meet via Firefox works without flags **provided the underlying mutter is not buggy** (i.e., post-49.3 fix).
- Firefox does not use the WebRTCPipeWireCapturer Chromium feature flag — it uses its own `media.webrtc.camera.allow-pipewire` (true by default on recent Firefox).
- Firefox is therefore a useful **A/B test**: if Google Meet freezes in Firefox too, the bug is in mutter/portal/PipeWire, not in Chrome. If it works in Firefox, the bug is in Chrome (likely Mesa/VA-API).

---

## 5. Tool Comparison Table

Status reflects Fedora 43 GNOME 49 Wayland-only with mutter ≥ 49.3.

| Tool | Install method | Wayland screen share status | Hardware accel | Sandbox | Recommendation |
|---|---|---|---|---|---|
| **Slack desktop (RPM)** | `slack-{ver}-0.1.fc21.x86_64.rpm` from slack.com | **Broken** (full screen). Window share partially works on some configs. WebRTCPipeWireCapturer hard-disabled in app.asar. | N/A (Electron) | None | **Avoid** unless app.asar patch is applied. Replace with web app. |
| **Slack Flatpak** | `flatpak install flathub com.slack.Slack` | **Broken** for full screen (same root cause as RPM). Window share inconsistent. | N/A | flatpak | **Avoid**. |
| **Slack web app** | Chrome → Install Slack as PWA | **Works.** Uses Chrome's PipeWire path. | Yes (subject to Mesa/Chrome bug) | Chrome | **Recommended.** Best ROI fix. |
| **Google Meet** | Chrome / Chromium / Firefox | **Works** post-mutter-49.3. Pre-49.3: freezes until movement. | Yes (Chrome — known regressions) | Browser | **Recommended** after mutter upgrade. |
| **Jitsi Meet (web)** | meet.jit.si in any browser | **Works** post-mutter-49.3. No Electron app needed. | Browser | Browser | Excellent fallback for ad-hoc calls. |
| **Jitsi Meet Electron** | Flatpak (`org.jitsi.jitsi-meet`) | Inconsistent. Same Electron disable issues as Slack. | N/A | flatpak | Use the web version instead. |
| **Zoom (RPM)** | `zoom_x86_64.rpm` from zoom.us | **First share works, second share black/crash.** Known issue across Zoom 6.x on Fedora 40–43 Wayland. Engineering acknowledged in 6.2.10.4645+ but not fully fixed as of early 2026. | Limited | None | Tolerable for one-shot meetings. Restart Zoom between shares. |
| **Zoom Flatpak** | `flathub us.zoom.Zoom` | Same issues as RPM, plus needs `--socket=wayland`. [Issue #520](https://github.com/flathub/us.zoom.Zoom/issues/520) confirms second-share black on F43. | Limited | flatpak | Avoid. |
| **Discord (RPM)** | discord.com .deb/.rpm | Originally needed xwaylandvideobridge. December 2024 Discord client added native Wayland audio + screen share via PipeWire. | N/A | None | Works with caveats. Use Vesktop for better support. |
| **Vesktop** | Flatpak (`dev.vencord.Vesktop`) | **Best Discord experience on Wayland.** Native PipeWire. | N/A | flatpak | Recommended Discord client. |
| **Microsoft Teams (PWA via Edge)** | Edge → Install Teams as app | **Works on Wayland** with caveats (full-screen only, sometimes window-share fails). The "Teams for Linux" Electron client (IsmaelMartinez) is community, not official. | Browser | Browser | Use Edge PWA if Teams is mandatory. |
| **Element / Element Call (Flatpak)** | `im.riot.Riot` | Needs `--enable-features=WebRTCPipeWireCapturer` flag (still works for Element, unlike Slack). Element Web in Firefox is the easier path. | N/A | flatpak | Element web in Firefox is simplest. |
| **Whereby (web)** | whereby.com | Whereby explicitly recommends X11 on Linux. Wayland support is fragile. | Browser | Browser | Avoid for Linux Wayland users. |
| **OBS Studio (RPM)** | `dnf install obs-studio` | **Works.** Uses Screen Capture (PipeWire) source via xdg-desktop-portal. Capture is rock solid on F43. | Yes (VA-API) | None | Use as the basis for the virtual-camera workaround (§6). |
| **gpu-screen-recorder (RPM)** | RPMFusion | Works. Low-latency record/replay. | Yes | None | For local recording, not live sharing. |
| **gnome-remote-desktop / RDP** | Built into GNOME 49 (`gnome-remote-desktop-49.1`) | Works. Settings → Sharing → Remote Desktop → enable RDP. Connect from another device with `xfreerdp` or Windows RDP. | N/A | systemd user service | Useful for "share my screen with one person via direct RDP", not for hosted meetings. |

---

## 6. The Virtual-Webcam Workaround

If Slack desktop must be used and the app.asar patch is unacceptable, OBS Studio + a virtual camera lets Slack "share screen" by sending a fake camera feed.

- **OBS Studio on Wayland**: install `obs-studio` and `obs-studio-plugin-pipewire-capture` from F43 repos. OBS uses `xdg-desktop-portal` to capture. This is rock solid on F43 (since OBS 30.x).
- **Virtual camera output**: OBS has a built-in `Start Virtual Camera` button (since OBS 28). On Linux this requires the kernel module `v4l2loopback`. On Fedora: `sudo dnf install kmod-v4l2loopback v4l2loopback` (RPMFusion), then `sudo modprobe v4l2loopback exclusive_caps=1 card_label="OBS Virtual Camera"`.
- **What apps see**: any app that lists webcams (Slack, Zoom, Meet, Discord) sees "OBS Virtual Camera". Select it as your camera. The audience sees your screen content as if it were a face cam.
- **Limitations**: aspect ratio is camera-typical (640×480 or 1280×720), not full screen. Quality is lower than a real screen share. Audio routing is separate (you'll need a PipeWire null sink for screen-share audio). And it's a clumsy UX — you give up your real webcam.
- **When to use**: emergency only, when (a) Slack desktop is mandatory and (b) the patch is unacceptable.

---

## 7. The X11 Fallback (and why it doesn't exist on F43)

Many older guides recommend "switch to GNOME on Xorg from the GDM cog wheel". **This is not possible on Fedora 43.**

- The [Fedora Change WaylandOnlyGNOME](https://fedoraproject.org/wiki/Changes/WaylandOnlyGNOME) removed the GNOME-Xorg session entirely. `gnome-session-xsession` is not in the F43 repos. The cog wheel on GDM only shows `GNOME` (Wayland). There is no "GNOME on Xorg" entry.
- GNOME 49 was *built* without X11 in Fedora's spec (`-Dx11=false`), so even if you installed an old gnome-session-xsession RPM, mutter would refuse to start an Xorg backend.
- **The only X11 escape options on F43:**
  1. Install KDE Plasma 6.4 — it still ships an X11 session (`sudo dnf install @kde-desktop-environment`).
  2. Install Cinnamon (`@cinnamon-desktop-environment`) or MATE (`@mate-desktop`) — both ship X11 sessions.
  3. Switch to a different login manager (`lightdm`) and a non-GNOME DE.
- This is significant overhead just to make Slack work, and is **not recommended** unless screen sharing is the user's only critical task and no other fix lands. The Chrome-PWA + mutter-upgrade path is much cheaper.

---

## 8. Hardware Considerations

| GPU | Wayland screen share state on F43 |
|---|---|
| **Intel** (i915, Xe) | Works well. Most reliable platform. No known F43-specific regressions. |
| **AMD** (radeonsi) | Works post-mutter-49.3. Pre-49.3 hits the damage-region freeze hardest because amdgpu's damage tracking interacts with the bug. Mesa 25.x lag bug also affects Chrome video accel. |
| **NVIDIA proprietary (570+)** | **Fragile.** [NVIDIA/open-gpu-kernel-modules issue #467](https://github.com/NVIDIA/open-gpu-kernel-modules/issues/467) — PipeWire screen sharing produces black canvas with cursors on NVIDIA + Wayland. Affects both proprietary and `nvidia-open` modules. Recommendation if NVIDIA: stick with the F43 nouveau/NVK Wayland session if the GPU is supported, or accept the brokenness on proprietary. |
| **NVIDIA hybrid laptops** | The discrete GPU rarely renders the GNOME compositor (it's Intel/AMD iGPU rendering mutter), so screen capture works as long as the iGPU path is intact. Slack/Chrome may try to use dGPU for video encode, which can re-trigger the issues. |

---

## 9. Recommended Fix Order

For the specific user (Slack broken, Meet freezes), in increasing cost:

### Step 1 — Diagnose (1 minute)
```bash
rpm -q mutter gnome-shell xdg-desktop-portal xdg-desktop-portal-gnome \
       pipewire wireplumber google-chrome-stable firefox slack
glxinfo -B | grep -E 'vendor|renderer'      # GPU info
journalctl --user -u xdg-desktop-portal-gnome -b --no-pager
```
Note the mutter version and the GPU vendor.

### Step 2 — Update everything (free)
```bash
sudo dnf upgrade --refresh
# Specifically: ensure mutter ≥ 49.5, pipewire latest, xdg-desktop-portal-gnome ≥ 49.x
# Then log out + log back in (NOT just restart the shell — full session restart)
```
Expected outcome: Google Meet stops freezing. Slack still broken.

### Step 3 — Switch Slack to the web app (5 minutes)
1. Open `https://app.slack.com` in Chrome (RPM `google-chrome-stable` from Google's Linux repo, or `chromium` from Fedora repos).
2. Log in.
3. Chrome menu (⋮) → Cast, save, and share → **Install Slack as app**.
4. The PWA pins to dock/Activities and runs in its own window. Screen share works (uses the same Chrome PipeWire path that Meet uses).
5. Uninstall the desktop Slack RPM/Flatpak to avoid confusion: `sudo dnf remove slack && flatpak uninstall com.slack.Slack`.

Expected outcome: Slack screen sharing works.

### Step 4 — If Chrome itself freezes screen share on Meet/Slack-PWA (after Step 2)
Disable Chrome hardware acceleration:
1. `chrome://settings/system`
2. Toggle off **"Use graphics acceleration when available"**.
3. Restart Chrome.
This is a workaround for the Mesa 25.x VA-API regression, not a real fix. Costs CPU but eliminates the freeze.

### Step 5 — Verify portal/Pipewire
```bash
systemctl --user restart xdg-desktop-portal xdg-desktop-portal-gnome pipewire wireplumber
gdbus introspect --session --dest org.freedesktop.portal.Desktop \
  --object-path /org/freedesktop/portal/desktop --recurse | grep ScreenCast
```
You should see `interface org.freedesktop.portal.ScreenCast`.

### Step 6 — Last resort: native Slack desktop with app.asar patch
Only if the user **must** have the desktop client. Create `/etc/dnf/post-transaction-actions.d/slack-pipewire.action` (requires `dnf-plugin-post-transaction-actions`) or run after each Slack update:
```bash
sudo sed -i 's/,"WebRTCPipeWireCapturer"/,"LebRTCPipeWireCapturer"/' \
  /usr/lib/slack/resources/app.asar
```
Test screen sharing immediately after each Slack update.

### Step 7 — Tool replacement (escalation)
If Steps 1–6 don't satisfy:
- For team meetings: move to **Jitsi Meet** (`meet.jit.si`) or **Google Meet** in Firefox.
- For Discord-style chat with screen share: **Vesktop** Flatpak.
- For Teams: Edge PWA.
- For 1-on-1 desktop sharing without a meeting platform: **gnome-remote-desktop** built-in, share RDP creds with the other party, they connect with `xfreerdp` or Microsoft Remote Desktop.

---

## 10. Open Questions (need user input)

The following details would change the recommendation:

1. **Exact mutter version**: `rpm -q mutter`. If pre-49.3, the freeze fix is "just upgrade". If already on 49.5, the freeze has another cause.
2. **GPU vendor**: Intel / AMD / NVIDIA. NVIDIA proprietary means a different recommendation set (KDE Wayland or X11 swap may be necessary).
3. **Did Slack screen share work on Fedora 42 (GNOME 48)?** If yes, this is a F43-specific regression — likely the mutter damage bug that already had a fix in 49.3. If no, this is the long-standing Slack `app.asar` hard-disable.
4. **Does the user have Flatpak Slack, RPM Slack, or both?** Removing one and testing the other isolates the runtime.
5. **Multi-monitor setup?** The framerate-range bug !4798 specifically affects monitors with no reported refresh rate (often DP MST hubs, USB-C dongles, virtual displays from `gnome-monitor-config`).
6. **Has the user tried Firefox as an A/B test for Meet?** If Meet freezes in Firefox too, the issue is in mutter/PipeWire/portal. If it works in Firefox, the issue is in Chrome (likely Mesa).
7. **Webcam in use?** The screen-share + camera path interacts with PipeWire's video buffering. Some users see freezes only when sharing screen *and* sending a camera feed.
8. **Acceptable to install Chromium/Chrome?** If the user is a Firefox-only person, Slack web is harder.

---

## 11. Sources

### Official release information
- [Fedora Magazine — What's New in Fedora Workstation 43](https://fedoramagazine.org/whats-new-fedora-workstation-43/) — F43 GA notes, GNOME 49 features.
- [Fedora Project Wiki — WaylandOnlyGNOME](https://fedoraproject.org/wiki/Changes/WaylandOnlyGNOME) — official change proposal removing GNOME-on-Xorg.
- [Fedora packages — pipewire](https://packages.fedoraproject.org/pkgs/pipewire/pipewire/index.html) — confirms F43 ships pipewire-1.4.11.
- [bodhi — mutter F43 updates](https://bodhi.fedoraproject.org/updates/?packages=mutter&releases=F43) — chronological mutter-49.x updates for F43.
- [bodhi — FEDORA-2026-d90ae90842](https://bodhi.fedoraproject.org/updates/FEDORA-2026-d90ae90842) — mutter-49.5-1.fc43, pushed stable mid-March 2026.
- [bodhi — FEDORA-2026-6b2e10bdb8](https://bodhi.fedoraproject.org/updates/FEDORA-2026-6b2e10bdb8) — mutter-49.3-1.fc43, pushed stable 2026-02-05.
- [Slack Linux release notes](https://slack.com/release-notes/linux) — Slack 4.46–4.49 official changelog.

### Mutter / GNOME upstream
- [GNOME GitLab — mutter MR !4798](https://gitlab.gnome.org/GNOME/mutter/-/merge_requests/4798) — Jonas Ådahl, Dec 2025, "screen-cast-stream-src: Only specify framerate range if there is any". Root cause + fix for the framerate-range freeze.
- [GNOME GitLab — mutter MR !4881](https://gitlab.gnome.org/GNOME/mutter/-/merge_requests/4881) — gnome-49 backport of !4798.
- [newreleases.io — mutter 49.3](https://newreleases.io/project/gnome-gitlab/GNOME/mutter/release/49.3) — changelog including "Fix reporing damage region in pipewire streams".
- [newreleases.io — mutter 49.4](https://newreleases.io/project/gnome-gitlab/GNOME/mutter/release/49.4) — includes "Fix screen sharing of monitors with no framerate".
- [newreleases.io — mutter 49.5](https://newreleases.io/project/gnome-gitlab/GNOME/mutter/release/49.5) — F43's current stable mutter; includes 49.4 fixes.
- [9to5Linux — GNOME 49.4 Released](https://9to5linux.com/gnome-49-4-released-with-improvements-for-nautilus-gnome-shell-and-mutter) — confirms screen sharing fix in 49.4 with attribution.
- [Linuxiac — GNOME 49.4 Released](https://linuxiac.com/gnome-49-4-released-with-shell-mutter-and-files-bug-fixes/) — confirmation, dated 2026-02-14.
- [GNOME blog — X11 Session Removal FAQ (alatiera, June 2025)](https://blogs.gnome.org/alatiera/2025/06/23/x11-session-removal-faq/) — explains the upstream rationale.

### Slack-specific
- [GitHub — flathub/com.slack.Slack issue #101 (Screen sharing on Wayland)](https://github.com/flathub/com.slack.Slack/issues/101) — original Wayland screen share request, closed 2021 with PR #118.
- [GitHub — flathub/com.slack.Slack PR #118 (vchernin)](https://github.com/flathub/com.slack.Slack/pull/118) — added PipeWire to Flatpak runtime.
- [GitHub — flathub/com.slack.Slack issue #196](https://github.com/flathub/com.slack.Slack/issues/196) — April 2023 regression report; closed not-planned.
- [GitHub — PaulDance/patch-slack](https://github.com/PaulDance/patch-slack) — the WebRTCPipeWireCapturer → LebRTCPipeWireCapturer sed patch as a Debian package.
- [AUR — slack-desktop-wayland](https://aur.archlinux.org/packages/slack-desktop-wayland) — version 4.47.69-1 with PKGBUILD comments confirming the disable still exists in current Slack.
- [Hacker News — "The only issue I have with wayland is that screen sharing from slack does not work"](https://news.ycombinator.com/item?id=39630249) — March 2024 thread, comment from `argulane` confirming Slack's intentional disable.
- [Arch BBS — Screen sharing in Slack under Wayland](https://bbs.archlinux.org/viewtopic.php?id=292904) — community working solutions.
- [Guy Rutenberg — Slack screen sharing under Wayland](https://www.guyrutenberg.com/2022/03/12/slack-screen-sharing-under-wayland/) — old (2022) `--enable-features` instructions, now stale.
- [OMG! Ubuntu — Slack Linux App Fixes Screen Sharing Under Wayland](https://www.omgubuntu.co.uk/2024/05/slack-linux-app-fixes-screen-sharing-under-wayland) — Slack 4.38.115 (May 2024) "fix" announcement, only partially worked.

### Google Meet / Chrome / freeze symptom
- [Fedora Discussion — Fedora screen share / remote desktop freezing until I move something](https://discussion.fedoraproject.org/t/fedora-screen-share-remote-desktop-freezing-until-i-move-something/145313) — exact symptom match, Feb 2025, AMD GPU, GNOME Wayland.
- [Fedora Discussion — Screen Sharing Broken in F43](https://discussion.fedoraproject.org/t/screen-sharing-broken-in-f43/179819) — January 2026 thread (kyzu0, decathorpe, py0xc3, rohankmr414) confirming F43 black screen + flatpak/RPM observation.
- [Fedora Discussion — Wayland and Chromium/Brave GPU acceleration is suddenly broken](https://discussion.fedoraproject.org/t/wayland-and-chromium-brave-gpu-acceleration-is-suddenly-broken-videos-hella-laggy/167520) — Mesa 25.x regression on Fedora, Oct 2025 → Jan 2026.
- [Fedora Discussion — Difficult to use GNOME in Fedora 43 at the moment (mutter)](https://discussion.fedoraproject.org/t/difficult-to-use-gnome-in-fedora-43-at-the-moment-mutter/172421) — November 2025, mutter 49.1.1 / 49.1 bugs documented.
- [Red Hat Customer Portal — How do I enable screensharing when I use Google Chrome or Firefox on Wayland?](https://access.redhat.com/solutions/6712111) — official Red Hat instructions (still valid for the prerequisites).
- [GitHub — mylinuxforwork/dotfiles issue 1106](https://github.com/mylinuxforwork/dotfiles/issues/1106) — multi-portal-backend conflict producing black screen.

### Zoom / Discord / Teams / Element / Jitsi
- [GitHub — flathub/us.zoom.Zoom issue #520](https://github.com/flathub/us.zoom.Zoom/issues/520) — Zoom 6.4.6 second-share black on Arch + GNOME 48; same root cause on F43.
- [Zoom Community — Sharing in Wayland Works Only The First Time](https://community.zoom.com/t5/Zoom-Meetings/Sharing-in-Wayland-Works-Only-The-First-Time/m-p/240802) — confirms second-share broken across distros.
- [GamingOnLinux — Discord finally fixed Linux screen and audio sharing with Wayland (Dec 2024)](https://www.gamingonlinux.com/2024/12/looks-like-discord-finally-fixed-linux-screen-and-audio-sharing-with-wayland/) — Discord native Wayland support added.
- [GitHub — element-hq/element-web issue #18607](https://github.com/element-hq/element-web/issues/18607) — Element Wayland screen sharing broken, fix is `--enable-features=WebRTCPipeWireCapturer`.
- [Microsoft TechCommunity — Microsoft Teams PWA on Linux](https://techcommunity.microsoft.com/t5/microsoft-teams-blog/microsoft-teams-progressive-web-app-now-available-on-linux/bc-p/3679463) — Teams Linux desktop client deprecated, PWA path is now official.

### XDG portals + GNOME RDP
- [Arch Wiki — XDG Desktop Portal](https://wiki.archlinux.org/title/XDG_Desktop_Portal) — architectural overview, kept current.
- [Flatpak — ScreenCast portal docs](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.impl.portal.ScreenCast.html) — protocol reference.
- [Fedora packages — gnome-remote-desktop 49.1 fc43](https://packages.fedoraproject.org/pkgs/gnome-remote-desktop/gnome-remote-desktop/fedora-43.html) — version on F43.
- [Fedora Magazine — Sharing the computer screen in GNOME](https://fedoramagazine.org/sharing-the-computer-screen-in-gnome/) — built-in RDP server walkthrough.

### NVIDIA-specific
- [GitHub — NVIDIA/open-gpu-kernel-modules issue #467](https://github.com/NVIDIA/open-gpu-kernel-modules/issues/467) — PipeWire screen sharing black canvas with cursors on NVIDIA + Wayland.
- [Fedora Discussion — Chromium share screen crash with NVIDIA proprietary driver on Wayland](https://discussion.fedoraproject.org/t/chromium-share-screen-crash-with-nvidia-proprietary-driver-on-wayland/75058) — confirms NVIDIA-specific brokenness.

---

## 12. Confidence Assessment

| Claim | Confidence | Evidence |
|---|---|---|
| Slack hardcodes WebRTCPipeWireCapturer disable in app.asar | **High** | Multiple independent sources (PaulDance/patch-slack, AUR PKGBUILD comments, HN comment from argulane, Arch BBS thread, NixOS Discourse). Workaround sed command is consistent across sources. |
| The Meet freeze symptom matches mutter damage-region bug | **High** | Symptom in Fedora Discussion 145313 ("freezing until I move something") is textbook damage-tracking failure. Mutter 49.3 changelog explicitly says "Fix reporing damage region in pipewire streams". |
| The framerate-range bug (!4798/!4881) affects F43 | **Medium-high** | Upstream MR description references PipeWire 1.5.x; F43 is on 1.4.11. The bug *may* be PipeWire-version-dependent. But mutter 49.5 backported it and Fedora pushed it, suggesting it does affect F43 users in some configurations (probably multi-monitor with USB-C dongles). |
| `--enable-features=WebRTCPipeWireCapturer` on Slack launcher does not work | **High** | Confirmed by 3+ sources from 2024-2026. The flag is *ignored* because Slack's internal disable list runs after Chromium parses CLI flags. |
| Slack web in Chrome PWA works on F43 | **High** | Chrome 110+ has WebRTCPipeWireCapturer on by default; this is the same path Google Meet uses, which is well-tested. The PWA wrapper does not change the screen-sharing pipeline. |
| Mesa 25.x VA-API regression affects F43 Chrome | **High** | Multiple Fedora Discussion threads from Oct 2025 → Jan 2026 with the same symptom; F43 cannot downgrade to Mesa 24.x. |
| Disabling Chrome hardware acceleration fixes Mesa-related freeze | **Medium** | Documented workaround in multiple threads, but it's also possible the freeze the user sees is mutter-side, not Chrome-side. Worth trying as Step 4. |
| F43 has no GNOME X11 fallback | **High** | Confirmed by Fedora Project Wiki, Phoronix, multiple Fedora Discussion threads. |
| Zoom second-share broken on F43 | **High** | Cross-distro confirmation (Arch, openSUSE, Fedora) with same root cause: Zoom doesn't reset PipeWire stream after first share ends. |
| Vesktop is the best Discord client on F43 | **High** | Community consensus across multiple threads in 2025-2026. |

---

## 13. TL;DR for the Implementation Plan

The implementation plan should:

1. **First action**: a diagnostic shell script that prints `mutter`, `pipewire`, `xdg-desktop-portal-gnome`, `google-chrome-stable`/`firefox`/`slack` versions; GPU vendor; and the output of `gdbus introspect` for the ScreenCast portal. This is the prerequisite to picking the right fix.
2. **If mutter < 49.3**: full system upgrade and session restart. This alone likely fixes the Meet freeze.
3. **For Slack**: install Chrome (or Chromium), open `app.slack.com`, install as PWA, uninstall the desktop Slack RPM/Flatpak. Document this as the canonical workflow. Optionally, add a script to `files/home/.local/bin/` that opens the Slack PWA, just to make the flow obvious.
4. **For Meet hardware accel**: a Chrome-flag toggle script (`chrome --disable-features=...`) or just documenting "if Meet still freezes, turn off Chrome hardware acceleration".
5. **As a backup**: an Ansible-managed `slack-asar-patch.sh` that applies the sed if the user explicitly opts in. Should be a separate, opt-in playbook with a clear warning.
6. **Long-term**: track upstream Slack and mutter, and revisit when Slack ships an Electron build that respects `--enable-features=WebRTCPipeWireCapturer`, or when Fedora 44 ships pipewire 1.6 + mutter 50 which should resolve the remaining edge cases.
