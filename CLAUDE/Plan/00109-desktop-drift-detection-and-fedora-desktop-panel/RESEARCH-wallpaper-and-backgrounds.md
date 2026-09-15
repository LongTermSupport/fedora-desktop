# Research: wallpaper decode cost and the black-background symptom

Supporting document for Plan 00109 Phase 5. Extracted from `PLAN.md` to keep the
plan lean — `PLAN.md` carries the conclusions and the open decisions; this file
carries the evidence behind them.

Raw agent output, including source citations and the benchmark method, is in
[`subagent-reports/`](subagent-reports/):

- `260911-wallpaper-scaling-research-opus.md` — decode/scaling/memory
- `260911-gnome-black-background-bugs-research.md` — the upstream bugs

The third report, on prior-art tools, is **deliberately not committed**: it lists
a dozen GNOME extension UUIDs, and a UUID such as `azwallpaper@azwallpaper…` is
indistinguishable from an email address to this repo's secret scanner. Replacing
them with reserved placeholders would turn correct reference data into wrong
reference data, so it stays in `untracked/agent-reports/` on the machine that
generated it. Its conclusion is summarised under "Prior art" below, which is the
part that matters.

---

## The symptom

After a DisplayLink dock cycle, some monitors show a working desktop — windows
can be dragged onto them, the Overview works — but the **desktop background is
black** on a subset of them. On 2026-09-11 the first two monitors were correct
and the third plus the laptop panel were not.

## A claim made during triage, and its correction

During the session it was asserted that the oversized wallpaper cost ~131 MB of
texture **per monitor**, ~520 MB across four. **That is wrong**, and the
correction matters because it changes which fix is worth building.

**Decode happens once, not per monitor.** `MetaBackgroundImageCache` is a
process-global singleton keyed on `GFile`, yielding one `CoglTexture` shared by
every consumer. On top of that, gnome-shell's `BackgroundSource.getBackground()`
forces `monitorIndex = 0` for any wallpaper whose filename does not end `.xml` —
so for a plain JPEG there is literally one `MetaBackground`, shared by the
desktop, the Overview and the unlock dialog.

**Scaling is GPU-only.** No CPU rescale exists in the path. mutter renders the
shared texture into a per-monitor FBO via
`cogl_framebuffer_draw_textured_rectangle()`, with `glGenerateMipmap` and a
deliberate depth cap in `get_best_mipmap_level()` bounding both what is sampled
and what is allocated.

### Measured cost on this host's layout

Layout: eDP-1 3072x1920 (scale 1.7534), DVI-I-1 2560x1080, HDMI-A-1 1920x1080,
DVI-I-2 1920x1080.

| Item                                          | Cost                                   | Paid              |
| --------------------------------------------- | -------------------------------------- | ----------------- |
| Level-0 texture                               | ~125 MiB                               | once, shared      |
| Mip level 1                                   | ~31 MiB                                | once, shared      |
| Per-monitor RGBA FBOs                         | 22.5 + 10.5 + 7.9 + 7.9 = **48.9 MiB** | once each         |
| *(what a per-monitor decode would have cost)* | *~500–625 MiB*                         | *does not happen* |

All four monitors compute mip level 1, so the full 14-level chain is not built.

### The real lever is decode latency

Benchmarked with glycin on this machine (gjs + `Gly-2`, replicating `load_file()`):

| Image                                  | Decode         | CPU buffer |
| -------------------------------------- | -------------- | ---------- |
| 7008x4672 (the camera original)        | **342–466 ms** | 93.7 MiB   |
| 3072x1920 (sized to the largest panel) | **73–75 ms**   | —          |

So ~156 MiB is held permanently to feed a 48.9 MiB output whose largest consumer
is 3072x1920.

## How often is that decode actually paid?

Rarely, and **not on the events one would assume**. This matters because it rules
out the "ongoing waste" framing entirely.

| Event                                                                        | Full re-decode?                                                                                                                                              |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Session start                                                                | **Yes** — always, once per login                                                                                                                             |
| Any `org.gnome.desktop.background` key, or `interface color-scheme`, changes | Yes, *if* the old image was already finalised; otherwise a cache hit                                                                                         |
| Wallpaper **file contents** change on disk (`GFileMonitor`)                  | Yes, forced (`imageCache.purge`)                                                                                                                             |
| **Monitor hotplug / reconfigure**                                            | **Normally no** — per-monitor FBOs are freed and re-rendered, a few GPU draws. Decode only if teardown transiently drops the last ref                        |
| Resume from suspend                                                          | No for a static JPEG — `_refreshAnimation()` early-returns unless an XML slideshow is set. In practice resume also fires `monitors-changed`, so see that row |
| Screen unlock                                                                | No — the shared `BackgroundSource` use-count stays above zero                                                                                                |
| GL video memory purge                                                        | Yes, unconditionally — but **dormant here**: gnome-shell holds `card1` (i915), not `card0` (nvidia)                                                          |

So on this host the realistic frequency is **once per login, plus the occasional
light/dark switch**. A dock cycle normally costs GPU draws, not a decode.

**Therefore the decode cost is not a performance problem and must not be sold as
one.** ~350 ms once a day is irrelevant. The only thing that makes image size
matter is the race described next — scaling shrinks the window in which a GC can
blank the backgrounds, roughly 350 ms → 73 ms.

**Not established:** how often a GC actually lands in that window on this
machine. That is the number that would justify building anything, and it has not
been measured. The only evidence to date is the user's report that the symptom
occurs *often* when monitors are moved around.

## Why that latency causes black backgrounds

`_updateBackgrounds()` **destroys every background manager before rebuilding
them**, and the image cache holds only a **weak** reference. Whether the decoded
texture survives a monitor reconfiguration therefore comes down to GJS GC timing.
During a miss, the affected monitors paint flat `primary-color`.

A 342–466 ms decode is exactly what loses that race. A 73 ms one is far likelier
to win. This is why scaling helps at all — not memory pressure.

Resume-from-suspend is a no-op for static images. The NVIDIA
`gl-video-memory-purged` full-reload path is dormant here: gnome-shell holds
`card1` (i915), not `card0` (nvidia).

## Two upstream bugs produce the same symptom independently

Scaling cannot fix these, which is why Phase 5 is a **mitigation, not a fix**:

- **`mutter#4767`** — empty redraw clip. Open; root-causes `gnome-shell#3206`
  (2020). Present verbatim in 50.4.
- **`mutter#4935`** — open, reported against Fedora 44 / mutter 50.3.
- **An apparently unreported sticky variant** — `meta_background_get_texture()`
  can return NULL on FBO allocation failure, while `meta-background-content.c`
  clears `CHANGED_BACKGROUND` *unconditionally* and nothing re-sets it, since
  `on_monitors_changed()` never emits `changed`.

**Distinguishing test:** the clip bug recovers on any repaint — open the Overview
and the background returns. The sticky variant does not.

**And the attribution was never confirmed on the axis that would have settled it.**
`mutter#4767` paints the empty clip's colour and the sticky NULL-texture variant paints
nothing, so *what colour a missed monitor shows* — flat blue-grey `primary-color` versus
black — discriminates between the two candidates directly. That question was put upstream
and **closed by the owner as resolved without a recorded answer**. So the `mutter#4767`
attribution here rests on the rendering evidence alone: it is the better-supported of the
two, not a confirmed one, and a future reader should not read it as settled. Neither
candidate is fixable in this repo either way, which is why the phase ships a recovery
action rather than waiting on the answer.

## Prior art: GNOME already ships per-monitor pre-scaling, unused

No third-party tool does per-monitor correct scaling — variety, hydrapaper,
superpaper and the rest all composite one spanned image.

But a GNOME background `.xml` with multiple `<size>` entries **is** per-monitor
pre-scaling. `GnomeBG` selects aspect-ratio-first with a width-proximity
tie-break, keyed on *logical* size. Verified empirically against the installed
`GnomeBG-4.0` typelib.

Two caveats before adopting it:

- It sets `monitorIndex` per monitor, which **leaves the shared single-background
  fast path** described above.
- Two identical 1920x1080 panels cannot be given different images — `<size>` keys
  on resolution, and theirs is equal.

## Caveats on this research

- `gitlab.gnome.org` was unreachable, so issue **comments** are unread throughout.
  Conclusions rest on issue bodies and source.
- Mesa was not instrumented to confirm 24 vs 32 bpp for `GL_RGB8`; 4 B/px is a
  conservative budget, not a measurement.

## Recovery: what was built, and the half of it that does not fire

Task 5.4's reasoning, kept here so `PLAN.md` carries task state and this carries the detail.

**What it does.** `Action.REFRESH_BACKGROUND` in `helpers/displaylink_recovery/`, fired by the
existing dock udev rule and the suspend service, strictly after the wedge ladder and never while
the session is locked. The mechanism is a `picture-uri` toggle, which is the **only** signal that
re-sets `CHANGED_BACKGROUND`: another `monitors-changed` does not recover it, and
`updateResolution()` refreshes only the animation. Restarting gnome-shell also works, and is not
available under Wayland.

**Why it never toggles while locked.** `gnome-shell#9188` reports a ~57 MB per-monitor leak on
that path, so the recovery reads lock state and defers instead.

**Three faults found while adding it, which together meant Plan 00056's recovery had never run
on this host at all:**

1. a deploy step that always failed on a missing parent directory, so the code was never in place;
2. a wedge signature that was always true — `getsize()` on a sysfs file returns 0, so the
   comparison it fed could not distinguish anything;
3. dconf writes silently discarded, because `sudo` strips the bus address the write needs.

Each was verified on the HOST and fixed in `9a79dd7`. The combination is the part worth naming:
three independent faults, each individually plausible as "written but never exercised", adding up
to a recovery mechanism that had never once executed while appearing to be deployed.

**Known limitation — the resume path is effectively inert.** Measured on this host:
`lock-enabled true` and `lock-delay 0`, so the screen is already locked by the time
`displaylink-suspend.service` runs. The lock check then correctly refuses and the run prints
`action=none` — indistinguishable from "nothing needed", and that ambiguity is why this is
written down rather than left to be rediscovered. The **dock/udev path still works**, since
somebody moving monitors around is present and unlocked. The close-lid → reopen → unlock case is
not covered, and covering it needs something in the *user* session that reacts to unlock rather
than a root oneshot.
