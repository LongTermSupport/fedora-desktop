# Known bugs: GNOME Shell / mutter black desktop background on some monitors (Wayland)

Research target: wallpaper stays BLACK on *some* monitors in a multi-monitor Wayland
setup after hotplug / DisplayLink-evdi reconnect / resume / unlock, while the Overview,
window dragging and other shell UI render fine on those same monitors.

Sources reached: `gitlab.gnome.org` GitLab REST API (via `curl` — worked), GitLab HTML
(via WebFetch — worked intermittently), GitHub (`GNOME/mutter` + `GNOME/gnome-shell`
raw source, `DisplayLink/evdi` via `gh`), Launchpad, Red Hat Bugzilla, GNOME Discourse,
WebSearch.

Sources NOT reached — see "Negative results and unreachable sources" at the end.

---

## Part 1 — Confirmed upstream issues

### 1. mutter#4767 — `meta_background_content_paint_content` drops paints when `redraw_clip` doesn't overlap `rect_within_stage`

- URL: https://gitlab.gnome.org/GNOME/mutter/-/issues/4767
- State: **open**, created 2026-04-24, no labels, no milestone.
- Self-described as "the root cause behind the long-standing gnome-shell#3206".

Symptom as filed: on multi-monitor setups where the primary monitor is at `(0, 0)` and
monitors are aligned on the Y axis, the lock screen wallpaper goes solid black on the
primary monitor as soon as the clock transitions to the unlock prompt.

Root cause given in the report, in `src/compositor/meta-background-content.c`, in the
`untransformed` branch of `meta_background_content_paint_content()`:

```c
redraw_clip = clutter_paint_context_get_redraw_clip (paint_context);
if (redraw_clip)
  {
    region = mtk_region_copy (redraw_clip);
    mtk_region_intersect_rectangle (region, &rect_within_stage);
  }
else
  {
    region = mtk_region_create_rectangle (&rect_within_stage);
  }
/* ...later... */
if (mtk_region_is_empty (region))
  return;
```

When upstream damage calculation produces a `redraw_clip` that does not overlap
`rect_within_stage`, `region` ends up empty and `paint_content` returns without
rendering. The CSS `background: black` of `.screen-shield-background` is then what the
user sees.

Proposed fix (6 net new lines): when the intersection produces an empty region, fall
back to `region = mtk_region_create_rectangle (&rect_within_stage)` — the same fallback
the "no redraw_clip" branch already uses.

Reporter tested a patched `mutter-49.5-1.fc43.x86_64` on Fedora 43 / GNOME Shell 49.5 /
Wayland and reports no regressions over ~24 h including lock/unlock, monitor hot-plug
and resume from suspend. The reporter could not open the MR themselves (new
gitlab.gnome.org account, namespace limit), so **no merge request is attached to this
issue** as of this research.

**Relevance to the target symptom: partial.** This is a *paint-time* drop, not a
texture-allocation failure. It explains black background where the shell UI still
renders (only the background actor's paint is skipped), which matches the target
symptom shape. But the filed reproducer is the lock screen specifically.

### 2. gnome-shell#3206 — Main monitor wallpaper replaced with complete black on the lock screen after some time

- URL: https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/3206
- State: **open since 2020-09-30**. Label: `5. Background`.
- Affected version as filed: Fedora 33, Wayland, gnome-shell 3.38.0, mutter 3.38.0,
  AMD RX 580, 2560×1440@144 + 1920×1440@144. Reporter notes it worked on Fedora 32 /
  GNOME 3.36, i.e. a 3.38 regression.
- Steps: lock the screen, wait, move the mouse to show the lock screen → black instead
  of wallpaper on the main monitor.
- Root cause: see mutter#4767 above. A 2023 investigation by user `taoky` preceded it.
- Known workaround from the taoky fork: set `z_position: 1` on the `UnlockDialog`
  background widget, which forces the actor into the `transformed` branch where clip
  calculation uses `rect_within_actor` instead.
  Commit: https://gitlab.gnome.org/taoky/gnome-shell/-/commit/3a9a0e1e13215ef1e4f157daa89d39b883a6ce33

### 3. mutter!5307 — in-progress fix for the lock-screen black wallpaper

- URL: https://gitlab.gnome.org/GNOME/mutter/-/merge_requests/5307
- **Could not fetch.** The GitLab MR endpoint returned HTTP 500 on every attempt
  (REST API by path, REST API by numeric project id 547, list endpoint with `iids[]`,
  and WebFetch of the HTML page). Details below are second-hand from Launchpad.
- Per Launchpad bug #2013042 this MR is the in-progress upstream fix, assigned to
  Chris Bainbridge, targeted at the Ubuntu 26.10 milestone. Merge status unknown.

### 4. gnome-shell#8034 — Lock screen background turns black

- URL: https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/8034
- State: **open**, created 2024-11-04. Labels: `5. Background`, `5. Lock screen`, `6. Display`.
- Affected: Debian 12 + Fedora 41, GNOME **43 and 47**, Wayland.
- Symptom: laptop + HDMI secondary. The *primary* monitor's lockscreen background turns
  black when pressing Enter/Escape, scrolling the mouse wheel, or using trackpad
  gestures — then goes back to the blurred wallpaper. The secondary monitor is normal.
- No root cause recorded in the issue body. Same family as #3206 / mutter#4767.

### 5. gnome-shell#7052 — Lock screen background glitch on primary display in multi-monitor setup

- URL: https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/7052
- State: **closed** 2023-09-28 (closed one day after filing; almost certainly closed as
  a duplicate of #3206 — I could not read the closing comment, see unreachable sources).
- Affected: Fedora 39 Beta, gnome-shell built from `6f7f0f36`, Wayland, no extensions.
  Reporter says "I could remember that this bug has been there at least from GNOME 43".
- Reproducer is notable because it works in a VM: libvirt virtio video card with
  `heads="2"`, connect with `virt-viewer -a -v -c qemu:///system`, lock, wait for black,
  click then press ESC, or swipe repeatedly.

### 6. gnome-shell#1431 — Blank/corrupted background after resume from suspend

- URL: https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/1431
- State: **open since 2019-07-06**. Label: `5. Background`.
- Affected: Arch, GNOME/GDM 3.32.2, Wayland, Intel Haswell-ULT i915 (no discrete GPU).
- Symptom: after resume the wallpaper remains black; the mouse cursor and top bar still
  work, but the screen is not refreshed and launched programs do not appear on screen
  (they do start).
- Workaround reported: switch to VT1 (`Ctrl+Alt+F1`), GDM appears, unlock, and the
  session resumes correctly with previously launched programs now visible.
- **Caveat:** this one is more than a background-actor bug — the whole screen stops
  refreshing, so it is NOT a clean match for "only the background actor is black".

### 7. gnome-shell#7515 — Background wallpaper does not show up under llvmpipe

- URL: https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/7515
- State: **open**, created 2024-03-25. Labels: `1. Bug`, `5. Background`.
- Affected: GNOME 46 running on llvmpipe (software rendering).
- Symptom: pure black solid background instead of the wallpaper; the rest of the shell
  renders. Settings still lets you pick a wallpaper with no warning.
- **This is the closest filed match in shape** to "the monitor is alive, only the
  background actor is black", and it points at the GL/texture path rather than at
  damage/clip. No root cause is recorded in the issue.

### 8. mutter#3232 — Background wallpaper is blank on Intel Gen3 (a *confirmed* allocation-failure case)

- URL: https://gitlab.gnome.org/GNOME/mutter/-/issues/3232
- State: **closed** 2024-07-27. Label: `1. Bug`.
- Affected: mutter 45.2, mesa 23.3.1, Intel 82Q35 (Gen3) with `GL_MAX_TEXTURE_SIZE = 2048`.
- Symptom: default background image not rendered, black only. Occurs on both Wayland and
  Xorg. Does **not** happen with `picture-options` set to `centered`; happens with all
  other values. Only reproduces with image sizes that are multiples of 2048
  (2048², 4096², 8192², …).
- Root cause: mutter tries to create a wallpaper texture larger than the driver's
  maximum texture size, so `cogl_framebuffer_allocate()` fails.
- Fixed by mutter MR !3477 (referenced in the issue body). Mesa side:
  https://gitlab.freedesktop.org/mesa/mesa/-/issues/10410
- **This is the only confirmed upstream bug I found that is actually caused by the
  `cogl_framebuffer_allocate()` failure path.** Note it hits the *wallpaper texture*
  path (`ensure_wallpaper_texture`), which is the sibling allocation site, not the
  per-monitor FBO site the question asked about.

### 9. Ubuntu Launchpad #2013042 — Lock screen wallpaper vanishes (goes black) if a second monitor is plugged in/out

- URL: https://bugs.launchpad.net/ubuntu/+source/gnome-shell/+bug/2013042
- State: **In Progress**, assigned to Chris Bainbridge, milestone ubuntu-26.10.
- Confirmed across Ubuntu 23.04 (lunar), mantic, noble and questing.
- Symptom: connecting or disconnecting a second display makes the blurred lock-screen
  wallpaper vanish on the **primary** monitor, leaving lock-screen text on black. One
  reporter has an external 2560×1440 as primary; the background only shows on the
  secondary laptop panel.
- Upstream links recorded on the bug: gnome-shell#3206 and mutter!5307.
- No workaround documented on the bug.
- Duplicate: Launchpad #2128888 "Wallpaper is not shown correctly on lock screen with
  multiple monitors" — Ubuntu 24.04, gnome-shell 46.0-0ubuntu6~24.04.15, kernel
  6.8.0-85, Dell XPS 13 9300, Intel Iris Plus G7, Wayland.
  https://bugs.launchpad.net/ubuntu/+source/gnome-shell/+bug/2128888
- Related, older, same family: Launchpad #1922340 "GNOME lock screen wallpaper turns
  black on main display". https://bugs.launchpad.net/bugs/1922340

### 10. GNOME Discourse — "Black wallpaper when session locked"

- URL: https://discourse.gnome.org/t/black-wallpaper-when-session-locked/35809
- Posted 2026-06-25. Dual-screen; on lock one screen shows the wallpaper and the other
  is black, usually the main screen. Poster explicitly says they "didn't find any bugs
  related to that". No maintainer reply, no workaround, no version given. Listed for
  completeness — it adds a data point but no new technical information.

---

## Part 2 — The `meta_background_get_texture()` FBO-allocation path

This was the specific candidate cause to investigate. Findings below are **my reading
of the current source**, verified against the shipping release branches — they are NOT
a filed upstream bug (see the negative result at the end of this section).

### The code is still present, in every shipping branch

`src/compositor/meta-background.c`, in `meta_background_get_texture()`
(current `main` around line 745):

```c
if (!cogl_framebuffer_allocate (monitor->fbo, &catch_error))
  {
    /* Texture or framebuffer allocation failed; it's unclear why this happened;
     * we'll try again the next time this is called. (MetaBackgroundActor
     * caches the result, so user might be left without a background.)
     */
    g_clear_object (&monitor->texture);
    g_clear_object (&monitor->fbo);

    return NULL;
  }
```

I checked out the file on each release branch. The exact form quoted in the task —
with the explicit `g_error_free (catch_error)` call — is present in **gnome-45,
gnome-46, gnome-47, gnome-48, gnome-49 and gnome-50**. Only `main` has been refactored
to `g_autoptr (GError)`. So the quoted snippet matches any shipped mutter 45 through 50,
and the code path is live in Fedora 44's mutter 50.x.

### The comment understates the problem: the failure is sticky, not retried

The comment claims "we'll try again the next time this is called". On the mutter side
that is true — `monitor->dirty` is only cleared on the success path
(`monitor->dirty = FALSE;` further down), so a subsequent call would retry.

But the **caller does not call again**. In
`src/compositor/meta-background-content.c`, `set_texture()` /
`meta_background_content_paint_content()` does (gnome-50 branch, lines 425–447 —
identical on `main`):

```c
if (self->changed & CHANGED_BACKGROUND)
  {
    CoglPipelineWrapMode wrap_mode;
    CoglTexture *texture = meta_background_get_texture (self->background,
                                                        self->monitor,
                                                        &self->texture_area,
                                                        &wrap_mode);
    if (texture)
      { self->texture_width = ...; self->texture_height = ...; }
    else
      { self->texture_width = 0; self->texture_height = 0; }

    cogl_pipeline_set_layer_texture (self->pipeline, 0, texture);
    cogl_pipeline_set_layer_wrap_mode (self->pipeline, 0, wrap_mode);

    self->changed &= ~CHANGED_BACKGROUND;   /* <-- cleared unconditionally */
  }
```

`self->changed &= ~CHANGED_BACKGROUND` runs **even when `texture` is NULL**. A NULL
texture is installed on pipeline layer 0 and the dirty flag is cleared. Nothing retries.

`CHANGED_BACKGROUND` is only ever re-set in two places in that file:

- `on_background_changed()` (line ~302) — the `MetaBackground::changed` signal handler
- `meta_background_content_set_background()` (line ~1074)

### `monitors-changed` does NOT emit `MetaBackground::changed`

In `meta-background.c`:

```c
static void
on_monitors_changed (MetaBackground *self)
{
  invalidate_monitor_backgrounds (self);   /* frees FBOs, marks all monitors dirty */
}
```

`invalidate_monitor_backgrounds()` frees the FBOs, reallocates the `monitors` array and
sets `dirty = TRUE` on each — but it **does not** emit `signals[CHANGED]`. Only
`mark_changed()` emits that signal, and `on_monitors_changed()` does not call it.

### What actually saves the user today, and why a hotplug can slip through

On the gnome-shell side, `js/ui/layout.js` `_monitorsChanged()` calls
`_updateBackgrounds()`, which destroys every `BackgroundManager` and creates fresh ones
(lines ~534–551), so each monitor gets a brand-new `MetaBackgroundContent` with
`changed = CHANGED_ALL`. That is the normal recovery path on hotplug.

However `_updateBackgrounds()` is **async** and awaits `_waitLoaded()`. If the very
first paint of that freshly-created content actor is the one where
`cogl_framebuffer_allocate()` fails, the actor caches NULL and clears
`CHANGED_BACKGROUND`, and from then on:

- another `monitors-changed` does **not** recover it (no `::changed` signal, and if
  layout.js has already settled it won't necessarily rebuild again),
- `js/ui/background.js` `BackgroundSource._onMonitorsChanged()` calls
  `background.updateResolution()` for monitors that still exist — and
  `updateResolution()` only refreshes the *animation* (`_refreshAnimation()`), it does
  not invalidate the texture,
- only a genuine `bg-changed` (a `picture-uri` / `picture-options` / `color-scheme`
  gsettings change) or restarting gnome-shell recovers it.

**That is exactly the observed workaround profile**: toggling
`gsettings set org.gnome.desktop.background picture-uri` fixes it; `Alt+F2 r` (X11) or
restarting gnome-shell fixes it; toggling the monitor may or may not.

It also explains why the Overview and window dragging still render on the affected
monitor: only the `MetaBackgroundContent` pipeline has a NULL layer texture. The stage
view, the shell UI actors, and window actors are all unaffected.

**This is a plausible and code-supported mechanism for the target symptom, and it is
per-monitor, which matches "some monitors but not others".** It is not proof that it is
*the* cause in any given case — proving that requires catching the failure in the log.

### How to confirm or rule it out on a live system

The failure path calls `g_clear_object()` and returns NULL **without logging anything**.
`catch_error` is freed/auto-freed and never printed. So there is no journal message to
grep for on the mutter side. Confirmation requires either:

- a Cogl/Mesa-level error surfacing separately in the journal
  (`journalctl --no-pager -b -u gnome-shell | grep -iE 'cogl|framebuffer|texture|GL error'`), or
- a debug build / patched mutter that logs in that branch.

Absence of a log line is therefore NOT evidence against this hypothesis.

---

## Part 3 — Secondary GPU / evdi / DisplayLink

### Mutter renders everything on the primary GPU

Confirmed from `doc/multi-gpu.md`
(https://github.com/GNOME/mutter/blob/main/doc/multi-gpu.md):

- All compositing happens on the **primary GPU**, regardless of which GPU a display is
  attached to. Rendered content is then transferred to the secondary GPU for scan-out.
- Three copy modes, tried in this fallback order:
  1. **secondary GPU copy** (default) — the secondary GPU performs the transfer
  2. **zero-copy** — primary GPU exports a framebuffer, secondary GPU imports it
  3. **primary GPU copy** — primary copies into a dumb buffer (GPU-accelerated first,
     CPU fallback second)
- Forceable with `MUTTER_DEBUG_MULTI_GPU_FORCE_COPY_MODE` set to `zero-copy`,
  `primary-gpu-gpu` or `primary-gpu-cpu`.
- The primary GPU can be overridden with a udev rule on vendor/device ID.

**Implication for the FBO hypothesis:** the per-monitor background offscreen FBO is
allocated in the primary GPU's Cogl context. It is *not* allocated on the evdi device.
So an evdi-specific GL/GBM weakness is unlikely to be the direct cause of
`cogl_framebuffer_allocate()` failing for one monitor. A more likely trigger would be a
transiently bogus or oversized monitor geometry at hotplug time (the texture is sized
`monitor_area.width/height * monitor_scale`), or genuine primary-GPU memory pressure.

Useful diagnostic consequence: if the black background were an evdi *scan-out/copy*
problem you would expect the shell UI on that monitor to be broken too. The task's
premise (Overview and window dragging render fine on the affected monitor) argues
**against** the secondary-GPU copy path and **for** a background-actor-specific fault.

### DisplayLink / evdi issues found

- **evdi#484** — "Monitors connected through the dock are blank on Wayland but work fine
  on X11 (Ubuntu 24.04)". https://github.com/DisplayLink/evdi/issues/484
  Closed 2024-08-10. GNOME 46.0, Wayland 1.22.0, Dell D6000 dock, RTX 3070 +
  nvidia-550, kernel 6.8.0-40. With the proprietary driver the monitor gets no signal
  at all; with nouveau the monitor gets a signal but shows **only the cursor on a black
  background — no shell UI, no window content**. Kernel log shows
  `evdi_user_framebuffer_destroy`, `evdi_painter_close`, `evdi_driver_close` right after
  the EDID property is set, which does not happen on X11. No workaround documented.
  **Not a match** — the shell UI does not render either, so this is scan-out, not the
  background actor.

- **evdi#489** — "Graphical artifacts on GNOME Wayland, Linux 6.10.3, and EVDI >= 1.14.3".
  https://github.com/DisplayLink/evdi/issues/489
  Open, filed 2024-09. EVDI 1.14.3/1.14.4/1.14.5/1.14.6 all affected; EVDI 1.14.2 with
  kernel 6.9.12 is clean. Dell D3100 dock. Artifacts are worst when moving windows
  around. Root cause not identified. **Workaround: downgrade to EVDI 1.14.2.**
  **Not a match** — artifacts, not a black background actor. Worth knowing as an evdi
  version-sensitivity data point.

- **evdi#470 / gnome-shell#7697** — "GNOME + EVDI causes GNOME to crash and GDM to
  restart using XORG instead of Wayland". https://github.com/DisplayLink/evdi/issues/470
  and https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/7697 (closed 2024-06-17,
  labelled `3. Not GNOME`). Arch, gnome-shell 46.2. Connecting a DisplayLink device
  drops the Wayland session back to GDM, after which only the X11 session is offered.
  **Not a match.**

- **gnome-shell#5248** — "2nd External Monitor turns black, showing only cursor".
  https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/5248
  **Open** since 2022-03-23, never triaged. Ubuntu 21.10, GNOME Shell 40.5, mutter 40.5,
  Wayland, ThinkPad X1 + Thunderbolt 4 dock, two 4K HDMI monitors, `evdi(OE)` present in
  the tainted module list. The right monitor goes black at random when the mouse crosses
  onto it, and starts showing content again when the mouse moves back. dmesg is spammed
  with `adding CRTC not allowed without modesets: requested 0x2, affected 0xf` and a
  `WARNING at drivers/gpu/drm/drm_atomic.c:1377 drm_atomic_check_only`.
  **Partial match at best** — "showing only cursor" means the shell UI is gone too, so
  this reads as an atomic-KMS/CRTC problem, not a background-actor problem. But it is
  the only open GNOME issue that combines evdi, a dock and a per-monitor blackout.

- **gnome-shell#2695** — "DisplayLink: Window manager warning: HW cursor for format
  875713089 not supported". https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/2695
  Closed 2020-04-27. Cosmetic/cursor only. Not related.

### Secondary-GPU issues that are adjacent but not the target bug

- **gnome-shell#6855** — "Monitors connected to secondary GPU randomly go black".
  https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/6855 — **closed**, filed
  2023-07-28. GNOME 44.3+. AMD RENOIR iGPU + RX 6800M dGPU, three monitors. Monitors on
  the secondary GPU black out for a few seconds and recover; monitors on the primary GPU
  are unaffected. Journal shows repeated
  `Cursor update failed: drmModeAtomicCommit: Invalid argument` and
  `Page flip failed: drmModeAtomicCommit: Invalid argument`. Reporter suspects the
  GNOME 44 async-KMS/PRIME changes. Workaround: reconnect the monitor or sleep/wake.
  The report does **not** distinguish "wallpaper black" from "whole output black", so it
  cannot be used as a match either way.

- **mutter#2182** — "Hotplugging a monitor on a secondary (nvidia GBM) GPU crashes with
  SIGTRAP and *Failed to create fallback offscreen framebuffer: Failed to create texture
  2d due to size/format constraints*".
  https://gitlab.gnome.org/GNOME/mutter/-/issues/2182 — closed, 2022-06-06.
  Directly relevant as *precedent*: hotplug on a secondary GPU can make offscreen
  texture/framebuffer creation fail in mutter. There it crashed; in the background path
  it would silently return NULL.

- **mutter#4983** — "Compositor hangs on external monitor hotplug: drmModeAddFB2 fails
  with ABGR2101010 on secondary-GPU (Optimus) setup". Closed 2026-08-17.

- **mutter#4714** — "Secondary GPU (NVIDIA) buffers fail KMS registration:
  gbm_bo_create() returns DRM_FORMAT_MOD_INVALID, drmModeAddFB2 rejected (regression from
  MR !4908)". Closed 2026-04-05.

- **mutter#3918** — "With Nvidia as primary, screen attached to secondary (integrated)
  GPU does not light up". Closed 2025-02-18.

- **mutter#3680** — "Some rendering is missing on Xilinx Mali (Failed to create offscreen
  effect framebuffer)". Closed 2024-09-18. Another confirmed case of offscreen framebuffer
  creation failing and producing missing rendering rather than a crash.

None of these four is the target bug; they are listed as evidence that the
"offscreen/FBO allocation fails on hotplug" family is real and recurrent.

---

## Part 4 — Other mutter background issues seen while searching (for completeness)

- **mutter#4935** — "Mutter loses wallpaper reference when resizing or closing maximized
  windows". https://gitlab.gnome.org/GNOME/mutter/-/issues/4935 — **open**, filed
  2026-07-26, label `2. Needs Information`. Fedora 44 Workstation, **mutter 50.3 /
  mutter-50.3-3.fc44.x86_64, GNOME Shell 50.3, kernel 7.1.4-204.fc44**, AMD RX 7600,
  Wayland, GNOME and GNOME Classic. Closing or unmaximizing any maximized window turns
  the whole background solid black. **The wallpaper only returns after changing it again
  or restarting the session** — i.e. exactly the sticky-cache recovery profile described
  in Part 2. This is the single most interesting issue for a Fedora 44 / mutter 50 host,
  and worth watching / adding data to.
- **mutter#1882** — "Wallpaper appears light grey during loading". Open since 2021-07-13.
- **mutter#1911** — "GNOME background color always black" (area outside a `scaled` image
  ignores primary/secondary colour). Closed 2023-12-19.
- **gnome-shell#5774** — "Some JPEG files are not handled and wallpaper is black".
  Closed 2022-08-18. Decoder-side, not compositor-side.
- **gnome-shell#7778** — "Backgrounds selected from Pictures become completely black".
  Closed 2024-07-13.
- **gnome-shell#9188** — "Lock-screen background retains wallpaper buffers when
  `org.gnome.desktop.background picture-uri` changes during shield-active state, leaking
  ~57 MB per monitor per change". Open, 2026-04-26. A memory-leak issue, but relevant as
  a caution: the `picture-uri` toggle workaround leaks buffers if used while locked.

---

## Negative results and unreachable sources

Reporting these explicitly, as requested.

- **No upstream issue exists for the `meta_background_get_texture()` per-monitor FBO
  allocation failure.** I searched both `GNOME/mutter` and `GNOME/gnome-shell` GitLab
  issue trackers for `cogl_framebuffer_allocate` (zero results in both projects),
  `framebuffer allocation`, `Failed to allocate`, `offscreen`, and `gbm_surface`.
  Nothing filed describes the NULL-texture caching behaviour of
  `MetaBackgroundContent`. mutter#3232 is the closest, and it is the sibling
  *wallpaper-texture* site, not the per-monitor FBO site.
- **No bug report, MR or commit quotes the comment "user might be left without a
  background".** WebSearch on the exact phrase returned only Cogl API documentation.
- **No evdi/DisplayLink issue matches "wallpaper black but shell UI renders".** I
  searched `DisplayLink/evdi` via `gh search issues` for `background`, `wallpaper` and
  `gnome black`. Every evdi black-screen report has the shell UI missing too.
- **No Red Hat / Fedora Bugzilla match.** Searching bugzilla.redhat.com for gnome-shell
  Wayland black desktop background on a second monitor returned only unrelated
  multi-monitor bugs (1714378, 1387832, 1134727, 2021904, 1433497, 1375342 …). None
  describes a black background actor with working shell UI.
- **No Ask Ubuntu or Reddit thread citing a specific issue number** for this symptom was
  found. Searches returned generic Wayland/NVIDIA external-monitor performance threads.

Unreachable sources:

- **mutter merge request !5307** — HTTP 500 on the GitLab REST API (by project path and
  by numeric id 547), on the `merge_requests?iids[]=` list endpoint, and on WebFetch of
  the HTML page. Its existence, assignee (Chris Bainbridge) and ubuntu-26.10 milestone
  come from Launchpad #2013042; its merge status and diff are unverified.
- **GitLab issue comments** — `/issues/<iid>/notes` returns `401 Unauthorized` without a
  token for both projects, so maintainer discussion on every GitLab issue above is
  unread. Issue titles, states, labels, dates and body text are all first-hand from the
  API. In particular I could not read why gnome-shell#7052 was closed after one day, or
  whether mutter#4767 has had any maintainer response.
- **ArchWiki DisplayLink page** — blocked by Anubis anti-bot; returned an access-denied
  page. A WebSearch snippet attributed to it claims "When DisplayLink devices are first
  plugged in, two displays connected to a displaylink adapter display a black
  background" and a performance tip about setting a "simpler" background image, but I
  could not verify either against the page itself. **Treat both as unconfirmed.**
- **GitHub blame view of `meta-background.c`** — truncated before the relevant lines, so
  I could not identify which commit introduced the "user might be left without a
  background" comment. The GitLab repository commits API for that file path returned
  HTTP 500.

---

## Practical takeaways

1. The best-documented, actively-tracked bug family is
   **gnome-shell#3206 → mutter#4767 → mutter!5307**, which is a *paint-clip* bug rather
   than an allocation bug. It is real, it is per-monitor, it produces exactly "black
   where the wallpaper should be while everything else renders", and it is unfixed
   upstream. Its filed reproducer is the lock screen, but the defective code
   (`meta_background_content_paint_content`) serves the **desktop background too**, and
   the mutter#4767 author explicitly argues the bug affects any `MetaBackgroundContent`
   consumer, not just the lock screen.
2. The **`cogl_framebuffer_allocate()` NULL-caching path is a genuine latent defect**
   present in mutter 45 through 50, confirmed by reading the shipping source, but it is
   **not filed upstream** and there is no logging to confirm it fires in the field.
3. For a Fedora 44 / mutter 50 host, **mutter#4935** is the closest live report and has
   the matching recovery signature (wallpaper returns only after changing it again or
   restarting the session).
4. Reported workarounds across all of the above:
   - toggle `gsettings set org.gnome.desktop.background picture-uri` (forces
     `MetaBackground::changed`, which is the only signal that re-sets `CHANGED_BACKGROUND`)
     — note gnome-shell#9188 says doing this while locked leaks ~57 MB per monitor
   - restart gnome-shell (`Alt+F2 r` on X11; log out / in on Wayland)
   - switch VT and back (gnome-shell#1431)
   - reconnect the monitor or sleep/wake the output (gnome-shell#6855)
   - for evdi artifacting specifically, downgrade to EVDI 1.14.2 (evdi#489)
   - `MUTTER_DEBUG_MULTI_GPU_FORCE_COPY_MODE` to change the secondary-GPU copy path, if
     a multi-GPU copy problem is suspected
