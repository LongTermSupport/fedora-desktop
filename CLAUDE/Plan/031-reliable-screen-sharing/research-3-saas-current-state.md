# Research Track 3 — Current SaaS / Commercial Screen-Sharing Landscape

**Scope:** Fedora 43 + GNOME Mutter (Wayland), residential-NAT WFH team, April 2026.
**Out of scope:** Self-hosted platforms (Track 2), native peer-to-peer tools (Track 4), and the two tools that are already known broken (Slack desktop screen share, Google Meet on mutter).

---

## Executive takeaway (read this first)

The Fedora 43 screen-sharing landscape bifurcates hard along two axes:

1. **Native Electron / Chromium app vs. browser.** On F43 Wayland, `xdg-desktop-portal-gnome` + PipeWire is the only sane capture path. Apps that honour the portal (Firefox, Chrome/Chromium native RPM, recent Discord stable, Zoom ≥ 6.0 with `enableWaylandShare=true`) work. Apps that ship their own capturer or rely on legacy X11 screengrab (Slack desktop, Teams native, older Discord, some Electron builds) fail — often with a black frame and no error.
2. **Native RPM vs. Flatpak.** The Fedora 43 community thread "Screen Sharing Broken in F43" identifies Flatpak sandboxing as the primary culprit for black-screen failures; native RPM installs of the same apps work on the same box.¹

Given this and the mutter ScreenCast instability that already killed Google Meet for the team, **the most reliable SaaS path on F43 today is a browser-based WebRTC room joined from native Firefox (RPM)**, *not* an Electron desktop client. The top two candidates to live-test are **Whereby** and **Jitsi Meet (hosted meet.jit.si)** as primary conferencing, plus **Discord stable (native RPM, not Flatpak)** as a dev-team daily-driver with the caveat that PipeWire-native app audio still doesn't get captured.²,³

---

## Candidate-by-candidate

### Discord (native RPM / RPM Fusion build)

- **What:** Voice/video/chat/screenshare SaaS, free tier covers everything a dev team needs (screenshare up to 1080p60 without Nitro since 2023).
- **Linux status:** Discord *stable* officially shipped Wayland screen sharing with audio in **January 2025** — confirmed by GamingOnLinux reporting on the 0.0.76 release. This is real, it works with GNOME Wayland via `xdg-desktop-portal`, and audio-from-shared-app is finally captured.²,⁴
- **Caveats:**
  - **Flathub build lags.** The Flathub Discord is known to still have portal issues; the plain .deb/.rpm tarball and Snap builds have the fix. The Fedora 43 thread confirms native RPM works where Flatpak doesn't.¹,²
  - **PipeWire-direct audio not captured.** If an app bypasses PulseAudio and writes straight to PipeWire (mpv with some configs, some recent GStreamer apps), Discord won't pick up its audio during screenshare.²
  - Global keybinds and some overlay features remain janky on Wayland.
- **Workflow:** Install native RPM (RPM Fusion has 0.0.118-1.fc43 as of search), sign in, press "Share Your Screen" inside a voice channel. No browser dance.
- **NAT:** Excellent — Discord's infra handles traversal transparently via their TURN fleet.
- **Pricing:** Free for our use case. Nitro ($10/mo/user) only needed for stream-quality bumps, HD file uploads, etc.
- **Verdict:** **Top candidate for daily dev collab.** Single persistent voice channel, drop-in/drop-out, screenshare-on-demand. Matches the "sit together in a room" pair-programming feel better than scheduled meetings.

### Zoom (native Linux client)

- **Linux status:** Zoom ≥ 5.11 added WebRTC/PipeWire Wayland screen sharing officially. In practice users on Fedora 42/43 report the **first screen share works, subsequent shares black-screen or crash GNOME Shell**.⁵,⁶ This matches the mutter ScreenCast bug pattern that already bit Google Meet.
- **Config knob:** `~/.config/zoomus.conf` → `enableWaylandShare=true`, ensure `pipewire` and `xdg-desktop-portal-gnome` are installed.
- **Web client fallback:** Zoom *does* have a browser client, and the EndeavourOS PSA confirms screenshare works reliably from Firefox when the native client fails.⁷ This is the safer path on F43.
- **Pricing:** Free tier = 40-min cap on 3+ people; Pro $15/mo/host.
- **NAT:** Excellent.
- **Verdict:** **Use the web client, not the desktop app.** The native client is exactly the class of bug that's burning the team on Google Meet. Keep as fallback only if a client mandates Zoom.

### Microsoft Teams (web client)

- Native Linux client is dead (deprecated December 2022).
- **Web client status on F43 + Firefox:** Works for audio/video/chat. Screen sharing is **inconsistent** — multiple 2025/2026 reports of black-screen during share on Wayland with both Firefox and Chromium.⁸ The Fedora 43 thread reports Teams in **Firefox** works but in **Chromium** is broken (black frame).¹
- PWA / Edge variant works slightly better (`ELECTRON_OZONE_PLATFORM_HINT=auto` sometimes required).
- **NAT:** Excellent.
- **Pricing:** Bundled in M365; standalone tier exists.
- **Verdict:** Unreliable. If a customer mandates Teams, join from Firefox; otherwise avoid.

### Whereby

- **What:** Pure-browser WebRTC rooms with permanent URLs (e.g. `whereby.com/yourteam`). No account needed to join, just to host.
- **Linux status:** Whereby's own docs explicitly warn: \*"Linux users on Wayland will experience issues with screen sharing — switch to Xorg."\*⁹ **However**, that doc hasn't been updated to reflect Firefox 106+ PipeWire support; in practice, Whereby screenshare from Firefox on F43 Wayland *does* work because the portal handles capture, not Whereby's code. The warning is defensive/outdated.¹⁰
- **Workflow:** Open a URL in Firefox. Zero install. Persistent room = very pair-programming-friendly.
- **Audio:** WebRTC audio; no system-audio-capture-during-screenshare on Linux (this is a browser/portal limitation, not Whereby's fault).
- **Pricing:** Free up to 100 meeting minutes/month; Pro $6.99/user/mo; Business $9.99/user/mo. 100-minute cap hurts active dev teams; Pro is essentially required.
- **NAT:** Excellent.
- **Verdict:** **Strong #1 candidate for live testing.** Browser-only kills the Electron-sandbox problem entirely, and persistent rooms fit pair-programming use.

### Jitsi Meet (meet.jit.si hosted)

- **What:** Open-source WebRTC conferencing, free public instance at meet.jit.si. No account needed.
- **Linux status:** Screen share works from Firefox on Wayland F43 via PipeWire+portal. Historical issues (2020–21) were resolved once Firefox shipped proper PipeWire support in v106.¹¹,¹⁰
- **Caveats:** meet.jit.si can be congested; the public instance has had quality issues at peak hours. Self-hosting is Track 2's domain.
- **Pricing:** Free.
- **NAT:** Excellent (JaaS/8x8 TURN infra).
- **Verdict:** **Strong #2 candidate.** Free, no-install, browser-based. Pair it with the Jitsi Electron app only if the browser version isn't enough — and **do not** install via Flathub (see the F43 thread).

### Around.co — DEAD

- Shut down **March 31, 2025**. Acquired by Miro Labs; features folded into Miro's "Video Calls".¹²
- If the team used Around previously, the successor is Miro Video Calls (browser-based, same WebRTC caveats as Whereby).

### Tuple

- **Linux support:** Linux **alpha/beta** exists as of 2025. Flagship platform is macOS.¹³ No public data on Wayland compatibility specifically for Fedora 43; the beta changelog is behind a paywall/login.
- **Pricing:** $25/user/month — expensive for a dev team relative to Whereby/Discord.
- **Features:** 5K low-latency, draw-on-screen, hand-off keyboard/mouse control — genuinely best-in-class for pair programming.
- **Verdict:** **Worth a beta trial** if the team wants true remote-driver pairing, but unknown Wayland stability and premium price mean it's a secondary investment, not a primary fix.

### Pop (formerly Screen, pop.com)

- Pop.com / Screen Inc. folded into a Pop subsidiary; Linux client never shipped stably. Multiple 2024–2025 tracker reports mark Linux as "planned, no ETA." Effectively **not viable for Fedora 43.**

### CoScreen (Mirantis)

- Official CoScreen help page: \*"CoScreen currently supports Microsoft Windows 10+ and macOS 13+. Sign up for the waitlist for Linux support."\*¹⁴ Waitlist is still open as of 2026. **Not viable.**

### Drovio (formerly Usetime)

- Official Drovio help page: \*"Wayland is not currently supported. Run your desktop under Xorg."\*¹⁵ The team's core requirement is Wayland. Fedora 43 default session is Wayland with no X11 fallback in the standard install. **Not viable** without regressing the whole desktop session.

### VS Code Live Share / JetBrains Code With Me

- **Live Share:** Works on Linux including Fedora/Wayland. Not a screen-share — it shares the *editor state*, terminals, servers. Excellent for code pairing but does not replace meetings/demos where you need to show browser behaviour, run a recording, or demo something outside the editor.
- **Code With Me:** JetBrains announced sunset in **March 2026**; 2026.1 is the final supported release, security updates until Q1 2027.¹⁶ Do not adopt as a new tool.
- **Verdict:** Live Share is an *adjunct* to real screen share, not a replacement. Recommend keeping it in the toolkit alongside whatever conferencing tool the team picks.

### Daily.co / LiveKit Cloud (WebRTC-as-a-Service)

- Developer infrastructure, not an end-user conferencing product. Not a SaaS the team "signs up for and uses." Context only: they power Whereby-like products. Out of scope.

### Adjacent (not screen share but useful)

- **Tailscale SSH / Taildrop:** For sharing code, running sessions via `tmate`, or exposing a local dev server to a teammate without cloud deploy. Recommended as part of the toolkit independent of whatever video solution wins.

---

## Pair-programming-specific feature matrix

| Tool                   | Browser-only? | Wayland screenshare reliable on F43? | Remote-control (keyboard/mouse handoff)? | Audio       | Free tier usable? | Price         |
| ---------------------- | ------------- | ------------------------------------ | ---------------------------------------- | ----------- | ----------------- | ------------- |
| **Whereby**            | Yes (Firefox) | Yes (via portal)                     | No                                       | WebRTC      | 100 min/mo cap    | $6.99/user/mo |
| **Jitsi (hosted)**     | Yes (Firefox) | Yes (via portal)                     | No                                       | WebRTC      | Unlimited         | Free          |
| **Discord (RPM)**      | No (desktop)  | Yes (stable ≥ 0.0.76)                | No                                       | Voice + app | Full features     | Free          |
| **Zoom (web)**         | Yes (Firefox) | Yes                                  | No                                       | WebRTC      | 40-min cap 3+     | $15/mo/host   |
| **Teams (Firefox)**    | Yes           | Partial (flaky)                      | No                                       | WebRTC      | Bundled M365      | —             |
| **Tuple**              | No (native)   | Unknown (Linux beta)                 | **Yes**                                  | Native      | 14-day trial      | $25/user/mo   |
| **VS Code Live Share** | No            | N/A (not screenshare)                | Yes (editor only)                        | External    | Free              | Free          |
| **CoScreen**           | —             | **Linux not supported**              | Yes                                      | —           | —                 | —             |
| **Drovio**             | —             | **Wayland not supported**            | Yes                                      | —           | —                 | —             |
| **Pop / pop.com**      | —             | **No Linux client**                  | Yes                                      | —           | —                 | —             |
| **Around**             | —             | **Shut down Mar 2025**               | No                                       | —           | —                 | —             |

---

## Top picks for live testing

### 1. Whereby (primary meetings) + Discord native RPM (daily dev co-working)

**Rationale:** Whereby gives the team a zero-install, zero-account, persistent-URL room that runs in Firefox — which on F43 + PipeWire + `xdg-desktop-portal-gnome` is the *only* screen-capture path that consistently works without the Electron-sandbox or mutter-crash failure modes. Discord native RPM fills the "hang out in a voice channel all day and drop a screenshare when needed" pair-programming gap that a scheduled-room product doesn't cover. Both are free-tier-capable at small-team scale (Whereby Pro at $6.99/user is cheap insurance for the 100-minute cap).

### 2. Jitsi Meet on meet.jit.si (meetings fallback)

Truly free, no account, browser-based, works on Firefox/Wayland. Keep as backup in case Whereby's free tier bites or Whereby has a regional outage. Do **not** use the Jitsi Electron Flatpak build — it has the same Flatpak sandbox issue as Slack/Teams-in-Chromium on F43.¹

### Explicitly rejected

- **Zoom native client:** same mutter ScreenCast bug class that killed Google Meet. Fail-fast rule applies — no point adopting another instance of the problem.
- **CoScreen, Drovio, Pop, Around:** no viable Linux/Wayland support or defunct.
- **Tuple:** keep on the radar for 2026-H2 if Linux beta stabilises, but not a reliable answer *today*.
- **Teams native:** Electron-dead, web client partial-broken on Chromium, workable only on Firefox for external customer calls.

---

## Critical operational rules derived from research

1. **Install conferencing-adjacent apps as native RPM, not Flatpak.** The F43 thread is unambiguous: Flatpak sandboxing breaks screen capture on Wayland for multiple Electron apps, and native RPM of the same app fixes it.¹
2. **Prefer Firefox over Chromium-family** for any web-based screenshare on F43 — Firefox 106+ has the best PipeWire/portal integration and works where Chromium-in-Flatpak fails.¹,¹⁰
3. **Verify** `xdg-desktop-portal-gnome` **and PipeWire are installed and running** before blaming any given app. A missing portal makes every symptom above indistinguishable.
4. **Do not adopt tools that require X11.** Drovio/CoScreen/some Electron builds push "just use Xorg" — on Fedora 43 that's a regression and the stated requirement is Wayland.

---

## Sources

01. Fedora Discussion — "Screen Sharing Broken in F43" — <https://discussion.fedoraproject.org/t/screen-sharing-broken-in-f43/179819> (identifies Flatpak as root cause, native RPM + Firefox works)
02. GamingOnLinux — "Discord screen-sharing with audio on Linux Wayland is officially here" (Jan 2025) — <https://www.gamingonlinux.com/2025/01/discord-screen-sharing-with-audio-on-linux-wayland-is-officially-here/>
03. Arch Wiki — Discord — <https://wiki.archlinux.org/title/Discord> (PipeWire-direct audio caveat)
04. GamingOnLinux — "Looks like Discord finally fixed Linux screen and audio sharing with Wayland" (Dec 2024) — <https://www.gamingonlinux.com/2024/12/looks-like-discord-finally-fixed-linux-screen-and-audio-sharing-with-wayland/>
05. Zoom Community — "Fedora 42, Gnome 47 - Wayland screen sharing does not work" — <https://community.zoom.com/t5/Zoom-Meetings/Fedora-42-Gnome-47-Wayland-screen-sharing-does-not-work/m-p/216734>
06. Fedora Discussion — "Zoom Works OK, Until I Ask It to Stop Screen Sharing" — <https://discussion.fedoraproject.org/t/zoom-works-ok-until-i-ask-it-to-stop-screen-sharing/135986>
07. EndeavourOS Forum — "PSA: Zoom screen sharing under non-Gnome wayland works when joining from your browser" — <https://forum.endeavouros.com/t/psa-zoom-screen-sharing-under-non-gnome-wayland-works-when-joining-from-your-browser/24712>
08. Microsoft Q&A — "Screen sharing do not works anymore on any modern linux" — <https://learn.microsoft.com/en-us/answers/questions/1118285/screen-sharing-do-not-wroks-anymore-on-any-modern>
09. Whereby Support — "Supported Browsers & Devices" — <https://whereby.helpscoutdocs.com/article/415-supported-devices>
10. Phoronix — "Firefox 106 Brings Improved WebRTC — Better Screen Sharing On Wayland" — <https://www.phoronix.com/news/Firefox-106-Available>
11. GitHub — jitsi/jitsi-meet Issue #6389 "Screen Sharing on Wayland not Possible" — <https://github.com/jitsi/jitsi-meet/issues/6389>
12. Around.co official site / Miro acquisition notice — <https://www.around.co/> (shutdown Mar 31 2025)
13. Tuple — <https://tuple.app/> (Linux beta status)
14. CoScreen Help — "Does CoScreen support Linux?" — <https://support.coscreen.co/hc/en-us/articles/360051499134>
15. Drovio Help — "[Linux only] Switching from Wayland to X.org" — <https://help.drovio.com/en/article/linux-only-switching-from-wayland-to-xorg-1b5b7im/>
16. JetBrains Platform Blog — "Sunsetting Code With Me" (Mar 2026) — <https://blog.jetbrains.com/platform/2026/03/sunsetting-code-with-me/>
