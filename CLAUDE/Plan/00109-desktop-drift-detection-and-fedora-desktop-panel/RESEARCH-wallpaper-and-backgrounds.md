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
