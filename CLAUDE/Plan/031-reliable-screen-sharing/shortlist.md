# Plan 031 — Phase 2 Shortlist & Test Ladder

**Created**: 2026-04-14
**Status**: Phase 2 (synthesis) complete; ready for Phase 3 (live testing)

This doc synthesises the 4 parallel research reports into a unified
comparison and a recommended **diagnostic-first test ladder**. The order is
deliberate: each test, in addition to potentially solving the problem, also
narrows down where the bug actually lives.

## The crystallised picture

Across all four research tracks, three converging insights:

1. **The bug is in the GNOME/Mutter portal stack** (`xdg-desktop-portal-gnome`
   → mutter ScreenCast → PipeWire). Every browser-based tool that does
   `getDisplayMedia()` ultimately routes through it, which is why Meet, Slack,
   Zoom-native, and most self-hosted-via-browser platforms all hit the same
   freeze class.
2. **Working solutions fall into three patterns** that *physically can't* hit
   the broken portal:
   - **Bypass the portal** (Sunshine = KMS/DRM framebuffer capture)
   - **Bypass the browser** (Discord native RPM, Galène native client)
   - **Bypass the screen-share path entirely** (OBS → v4l2loopback → use the
     unblockable webcam path; or VS Code Live Share = no pixels at all)
3. **Operational rules** that cut across every track:
   - **Native RPM > Flatpak** (Flatpak sandboxing breaks portal capture in
     several apps independent of the mutter bug)
   - **Firefox > Chromium** for any browser-based screenshare on F43
   - The team's `xdg-desktop-portal-gnome` + PipeWire stack must be verified
     working before debugging any individual app

## Unified comparison matrix

Sorted by the test ladder order below.

| Tool                                   | Track          | Bypass class             | Install effort                        | Audio                | NAT                | Self-host?   | Cost                                       | Diagnostic value                                                                                                  |
| -------------------------------------- | -------------- | ------------------------ | ------------------------------------- | -------------------- | ------------------ | ------------ | ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| Discord native RPM                     | SaaS           | Bypass browser           | 5 min (RPM Fusion)                    | ✅ desktop+mic       | ✅                 | ❌           | Free                                       | If works: confirms native non-Electron + portal works                                                             |
| OBS + v4l2loopback → Slack             | Unconventional | Bypass screen-share path | 30 min (akmod, Secure Boot caveat)    | ➖ separate channel  | ✅ (uses Slack)    | n/a          | Free                                       | If works: makes every videoconf tool work via webcam path                                                         |
| gnome-remote-desktop                   | Native Linux   | Same portal path         | 10 min (built-in)                     | ✅ stereo            | ❌ needs Tailscale | n/a          | Free                                       | **Key diagnostic**: if GRD freezes too → bug is in Mutter; if GRD works → bug is in Chromium-side getDisplayMedia |
| Sunshine + Moonlight                   | Native Linux   | Bypass portal (KMS/DRM)  | 1-2 hr (cap_sys_admin, Tailscale)     | ✅                   | ✅ via Tailscale   | n/a          | Free                                       | If this fails too → hardware/driver issue, not portal                                                             |
| Whereby                                | SaaS           | Browser via Firefox      | None (browser)                        | ✅                   | ✅                 | ❌           | Free 100min/mo, $7/user                    | Confirms whether *any* browser-based tool works on this user's setup                                              |
| Galène native client                   | Self-hosted    | Bypass browser           | 30 min server + native client install | ✅                   | ✅ via TURN        | ✅ Go binary | Same diagnostic as Discord but self-hosted |                                                                                                                   |
| VS Code Live Share + Tailscale + voice | Unconventional | No pixels at all         | 15 min                                | n/a (separate voice) | ✅                 | n/a          | Free                                       | Always-works fallback for code-only pairing                                                                       |

**Explicitly ruled out** (don't waste cycles testing):

- Around — shut down March 2025
- CoScreen, Pop, Tuple — no production Linux client
- JetBrains Code With Me — sunset announced March 2026
- Teams native — dead since Dec 2022
- Parsec — X11-only on F43
- wayvnc — wlroots-only, doesn't run on GNOME Mutter
- waypipe — per-app forwarding, wrong problem
- BigBlueButton — install complexity + 2025 black-screen regressions
- Element Call / MatrixRTC — 5-service stack only worth it if adopting Matrix chat
- Zoom native desktop — same bug class as Meet (use web client instead)
- RustDesk — open issues on F42 GNOME Wayland; F43 unverified; rides the portal

## The test ladder (Phase 3 plan)

Run in this order. **Stop at the first level that fully solves the problem**,
but always run Test 0 (diagnostic) regardless because it tells us where the
bug actually lives — useful for Plan 028 too.

### Test 0 — Diagnostic: gnome-remote-desktop (10 min)

**Why first**: Cheapest test that distinguishes "bug is in Mutter" from "bug
is in Chromium". If GRD freezes the same way Meet does, the bug is in the
GNOME portal stack itself, and any browser-based candidate is doomed. If
GRD works fine, the bug is Chromium-side and lots of browser solutions open
back up.

**Setup**: GRD is built into GNOME on F43. Enable in Settings → Sharing
→ Screen Sharing or via gsettings. Connect from second laptop with any RDP
client (Remmina is in Fedora repos).

**Pass**: 10+ minutes of active screen content with no freeze.

**Outcome routing**:

- If **pass** → bug is browser-side; favour Discord, Whereby, Firefox+Meet
- If **fail** → bug is portal-side; jump straight to Test 3 (Sunshine) and
  Test 2 (OBS virtual webcam)

### Test 1 — Quick win: Discord native RPM (30 min)

**Why second**: Discord native screen share with desktop audio works on
Wayland since v0.0.76 (Jan 2025). It's free, the team is likely already on
Discord for community servers, and it solves the daily pairing/voice/screen
combo in one tool.

**Setup**: `dnf install discord` (RPM Fusion). Test screen-with-audio share
in a voice channel between the two laptops.

**Pass criteria**:

- 30+ minutes of pair-programming style use
- Desktop audio carries through (e.g. share a YouTube video, far-end hears it)
- No freeze when content is changing rapidly
- Reconnect cleanly after laptop suspend/resume

**If passes**: Adopt for daily collaborative work. Document in
`docs/screen-sharing.md`. Plan 031 effectively closes here for the
common case.

### Test 2 — Universal solve: OBS + v4l2loopback → webcam path (90 min)

**Why third**: This is the lateral-thinking play. Every videoconf tool
(Slack, Meet, Teams, anything) blocks the screen-share path but **cannot**
block the webcam path without breaking video calls. Turning the screen into
a virtual webcam exploits a policy gap that vendors will never close.

This is the **universal solve** — once it works, every meeting tool the
team is forced to use (Slack for client calls, Meet for cross-org meetings)
becomes screen-share-capable.

**Setup**:

1. Install `v4l2loopback` (akmod from RPM Fusion — note Secure Boot caveat,
   may need to sign the module).
2. Install OBS Studio.
3. Configure OBS scene with screen capture source.
4. Click "Start Virtual Camera" in OBS.
5. In Slack/Meet, pick "OBS Virtual Camera" as the camera input instead of
   sharing screen.
6. Audio handled separately via the meeting tool's own mic/system-audio.

**Pass criteria**:

- 30+ minutes in a real Slack call with another participant
- Other side sees the screen content as if it were a webcam feed
- Reasonable framerate (target ≥15 fps for code reading)

**If passes**: This is the highest-leverage solution because it's
tool-agnostic. Worth Ansible-ising even if Test 1 also passed.

### Test 3 — Escape hatch: Sunshine + Moonlight + Tailscale (3 hr)

**Why last in main ladder**: Highest setup cost, but the only solution that
*structurally cannot* be affected by the Mutter portal bug (uses KMS/DRM
framebuffer capture). This is the team's escape hatch for when nothing
else works.

**Setup**:

1. Install Tailscale on both laptops (likely already in repo, check).
2. Install Sunshine (RPM or Flatpak; needs `cap_sys_admin`, document the
   security trade-off).
3. Install Moonlight on the viewer laptop.
4. Pair, test screen capture between the two over Tailscale.

**Pass criteria**:

- Works for full pair-programming session
- Latency feels responsive (\<150ms for keystrokes)
- Audio carries (Sunshine supports it)
- Reconnects cleanly

**If passes**: Document but probably keep as escape-hatch / power-user tool
rather than daily driver — install complexity is too high for ad-hoc use.

## Recommended Phase 3 picks (the actual shortlist)

The plan asked for 2–3 candidates. **Pick all three of Test 1, Test 2, and
Test 3 plus the Test 0 diagnostic**, in that order:

1. **Discord native RPM** — daily driver candidate. Highest probability of
   "just works" with lowest install cost.
2. **OBS + v4l2loopback** — universal solve. Once it works it solves *every*
   broken tool, not just one.
3. **Sunshine + Moonlight + Tailscale** — escape hatch. Only structurally
   guaranteed bypass of the broken portal.

**Run gnome-remote-desktop diagnostic first** to inform sequencing — if it
fails, skip Discord and go straight to OBS + Sunshine since those are the
only portal-bypass options.

## Cross-cutting operational changes (to apply regardless of choice)

These are universal wins surfaced by the research, worth Ansible-ising even
before Phase 3 testing:

1. **Add Chrome RPM** — already covered by Plan 028 Phase 3.2 (don't
   duplicate). Useful for Slack PWA fallback and Whereby reliability.
2. **Add Tailscale** — check if already installed; if not, add a playbook.
   Unlocks Sunshine-over-internet, GRD-over-internet, and many other
   LAN-only-tool options as fallbacks.
3. **Document the F43 Wayland gotchas** in `docs/screen-sharing.md`:
   - Native RPM > Flatpak for any conferencing app
   - Firefox > Chromium for in-browser screenshare
   - How to verify the portal stack is healthy
   - When to reach for the OBS virtual-webcam workaround

## What this plan does NOT need to fix

Plan 028 already handles:

- Diagnosing the actual mutter version on the user's machines
- Upgrading mutter past the known-buggy versions
- Adding Chrome and trying the Slack PWA path

Plan 031 should not duplicate any of that. Plan 031 lands on a *parallel*
solution path so the team is no longer dependent on Slack/Meet getting fixed.
