# GNOME Shell / mutter wallpaper handling on multi-monitor Wayland — decode and scaling cost

Research target: correct or confirm a claim made to a user about per-monitor wallpaper
decode/scaling cost.

Host under discussion: Fedora 44, GNOME on Wayland, **mutter 50.4 / gnome-shell 50.4**
(`rpm -q mutter gnome-shell` → `mutter-50.4-1.fc44.x86_64`, `gnome-shell-50.4-1.fc44.x86_64`).
GPUs on the host: `card0` → `nvidia`, `card1` → `i915` (Arc / Meteor Lake iGPU),
`card2`–`card5` → `evdi` (DisplayLink).
`GL_MAX_TEXTURE_SIZE = 16384` (from `glxinfo -l`).

All source citations below are the **`gnome-50` branch** of mutter and gnome-shell, i.e. the
code actually running on this host, fetched from the GitHub mirror
(`raw.githubusercontent.com/GNOME/{mutter,gnome-shell}/gnome-50/...`) because
`gitlab.gnome.org` was not reachable from this sandbox. The `main` branch is cited separately
where GNOME 51 changes the answer.

---

## TL;DR — the verdict on the claim

| Question                                   | Answer                                                                                                                                                                                                            |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Decoded once or per monitor?               | **Once.** One process-global `MetaBackgroundImageCache`, keyed on `GFile`; one `CoglTexture` shared by every monitor.                                                                                             |
| Per-monitor scaling on GPU or CPU?         | **GPU only.** No CPU rescale exists anywhere in the path.                                                                                                                                                         |
| Mipmaps for the downscale?                 | **Yes**, `glGenerateMipmap` on the GPU, and mutter deliberately caps the chain depth.                                                                                                                             |
| Cost paid once or ×4?                      | The **expensive part (decode + full-res texture) is paid once**. What *is* per-monitor is a small offscreen framebuffer at *monitor* resolution — ~49 MiB total for these four monitors, not 4× the source image. |
| Is the big file still a real problem?      | **Yes, but for different reasons**: ~350 ms decode latency on every (re)load, ~94 MiB transient CPU buffer, ~125–156 MiB permanently resident VRAM to feed a 48.9 MiB output.                                     |
| Known black-background bugs?               | **Yes** — `mutter#4767` / `gnome-shell#3206` (empty redraw clip, open), plus a second, unreported sticky-NULL-texture path the source shows. See §5.                                                              |
| Does a tool already pre-scale per monitor? | **No third-party tool does** — but GNOME's own background XML `<size>` mechanism does, and is shipped and unused. See §6.                                                                                         |

So: "it decodes it four times / costs four times the VRAM" is **wrong**. "A 27 MB 7008×4672
JPEG is a bad wallpaper on this machine" is **right**, for latency and for the resident
full-res texture.

---

## 1. Decode once, or per monitor?

### 1a. gnome-shell only ever creates ONE `Background` for a static image

`js/ui/background.js`, `BackgroundSource.getBackground()`:

```js
// Animated backgrounds are (potentially) per-monitor, since
// they can have variants that depend on the aspect ratio and
// size of the monitor; for other backgrounds we can use the
// same background object for all monitors.
if (file == null || !file.get_basename().endsWith('.xml'))
    monitorIndex = 0;
```

For a `.jpg`/`.png` the monitor index is **forced to 0**, so all four `BackgroundManager`s
(one per monitor, created by `LayoutManager._updateBackgrounds()` in `js/ui/layout.js`) look up
the *same* `Background` object, which *is* a `MetaBackground` (`class Background extends Meta.Background`). Per-monitor `Background` objects only happen for XML slideshow wallpapers,
where the XML can name different files for different monitor geometries.

The file's own header comment states the architecture explicitly
(`js/ui/background.js` lines 46–63):

```
// A static image, background color or gradient is relatively straightforward. The
// calling code creates a separate BackgroundManager for each monitor. Since they
// are created for the same GSettings schema, they will use the same BackgroundSource
// object, which provides a single Background and correspondingly a single
// MetaBackground object.
//
// BackgroundManager               BackgroundManager
//        |        \               /        |
//        |         BackgroundSource        |        looked up in BackgroundCache
//        |                |                |
//        |            Background           |
//        |                |                |
//   MetaBackgroundActor   |    MetaBackgroundActor
//         \               |               /
//          `------- MetaBackground ------'
//                         |
//                MetaBackgroundImage            looked up in MetaBackgroundImageCache
```

### 1b. And even multiple `MetaBackground`s would share one decoded texture

`src/compositor/meta-background-image.c`, `meta_background_image_cache_load()`:

```c
image = g_hash_table_lookup (cache->images, file);
if (image != NULL)
  return g_object_ref (image);
...
task = g_task_new (image, NULL, file_loaded, NULL);
g_task_run_in_thread (task, (GTaskThreadFunc) load_file);
```

with `meta_background_image_cache_get_default()` returning a **process-wide singleton**
(`static MetaBackgroundImageCache *cache;`), and the hash table keyed on
`g_file_hash`/`g_file_equal`. So the same URI always resolves to one `MetaBackgroundImage`
holding one `CoglTexture`.

### 1c. All the other consumers share it too

Every `BackgroundManager` in gnome-shell 50 uses the default
`settingsSchema: BACKGROUND_SCHEMA` (`org.gnome.desktop.background`), therefore the same
`BackgroundSource` from `BackgroundCache._backgroundSources` and therefore the same
`MetaBackground`:

- `js/ui/layout.js:500` `_createBackgroundManager()` — the desktop, one per monitor.
- `js/ui/workspace.js:978` — the Overview's per-workspace, per-monitor background.
- `js/ui/unlockDialog.js:715` — the lock screen (blurred via `Shell.BlurEffect`).

`js/ui/screenShield.js` references `org.gnome.desktop.screensaver` only for *settings*
(line 24/109), not for a `BackgroundManager`. So there is **no second decoded copy** for
the lock screen either.

### 1d. Caveat: the cache holds a WEAK reference

`meta_background_image_cache_load()` inserts into the hash table **without taking a ref**
(`g_hash_table_new (g_file_hash, (GEqualFunc) g_file_equal)` — no value destroy function),
and `meta_background_image_finalize()` removes itself:

```c
if (image->in_cache)
  g_hash_table_remove (image->cache->images, image->file);
```

So the decoded texture lives exactly as long as some `MetaBackground` holds a ref. The
"cache" is a *dedup* mechanism, not a retention mechanism. This matters in §4.

**GNOME 51 / mutter main changes this**: `627a46b504` ("background/image: No longer load from
files", 2026-03-16) removed `meta-background-image.c` entirely; the public API is now
`meta_background_set_texture()` / `meta_background_set_blend_textures()`
(`src/meta/meta-background.h` on `main`), and gnome-shell's `js/ui/background.js` grew a
`BackgroundTextureCache` that calls glycin from JS and holds textures in a
`Map` (`this._textures`) with **strong** references, purged only on file change or GL video
memory purge. That fixes 1d, but moves the decode call (`loader.load()` / `image.next_frame()`)
out of a GTask worker thread and into the JS main loop — I did **not** verify whether glycin's
sync API blocks the main context there, so treat that as an open question, not a finding.

---

## 2. GPU or CPU scaling? Mipmaps?

**Entirely GPU.** There is no CPU rescale in the path at all. The chain is:

1. `load_file()` (worker thread) → glycin gives raw pixels.
2. `file_loaded()` (main thread) → `meta_create_texture()` + `cogl_texture_set_data()`:
   a straight upload, no resampling.
3. `meta_background_get_texture()` (main thread, per monitor) → renders the source texture into
   a **per-monitor `CoglOffscreen`/FBO at monitor resolution** with
   `cogl_framebuffer_draw_textured_rectangle()`. This is the scale, and it is a GPU draw.
4. `MetaBackgroundContent` then samples *that* per-monitor texture in its own pipeline
   (vignette / brightness / rounded-clip shader) —
   `meta-background-content.c:428` `meta_background_get_texture (self->background, self->monitor, ...)`.

### Mipmaps: yes, generated on the GPU, and deliberately capped

`meta-background.c:631` `create_pipeline()` sets:

```c
cogl_pipeline_set_layer_filters (templates[type], 0,
                                 COGL_PIPELINE_FILTER_LINEAR_MIPMAP_LINEAR,
                                 COGL_PIPELINE_FILTER_LINEAR);
```

Cogl generates them lazily on first paint (`cogl-texture-2d.c:206` `_cogl_texture_2d_pre_paint`,
guarded by `auto_mipmap && mipmaps_dirty`), and the GL driver call is a plain
`glGenerateMipmap` (`cogl/cogl/driver/gl/cogl-texture-driver-gl.c:477`).

mutter then **limits how deep the chain goes**, via `get_best_mipmap_level()`
(`meta-background.c:739`, introduced by commit `7a0bc5af7f`, "background: Limit mipmap levels to
avoid loss of visible detail", 2020-02-10):

```c
static int
get_best_mipmap_level (CoglTexture *texture, int visible_width, int visible_height)
{
  int mipmap_width  = cogl_texture_get_width (texture);
  int mipmap_height = cogl_texture_get_height (texture);
  int halves = 0;

  while (mipmap_width >= visible_width && mipmap_height >= visible_height)
    { halves++; mipmap_width /= 2; mipmap_height /= 2; }

  return MAX (0, halves - 1);
}
```

and applies it with `cogl_pipeline_set_layer_max_mipmap_level (pipeline, 0, mipmap_level)`
(`meta-background.c:884` and `:916`). That call sets `priv->max_level_requested` on the
*texture* (`cogl-pipeline-layer-state.c:1301` → `cogl_texture_set_max_level()`), which feeds
`_cogl_texture_get_n_levels()`:

```c
return MIN (n_levels, priv->max_level_requested + 1);   // cogl-texture.c:328
```

and `generate_mipmap` sets `GL_TEXTURE_MAX_LEVEL` to `n_levels - 1` immediately before
`glGenerateMipmap` (`cogl-texture-driver-gl.c:470-477`). So the cap bounds both what is
*sampled* and what is *allocated*.

**Subtlety worth knowing (inference from the code, not from a maintainer statement):**
`max_level_requested` is per-*texture*, and all four monitors share one texture, so the last
`set_layer_max_mipmap_level()` before the first paint wins, and `mipmaps_dirty` is cleared
after the first generation. In practice all four of this host's monitors compute the *same*
level for a 7008×4672 source (see §3), so it does not bite here.

---

## 3. Actual memory cost for a 7008×4672 JPEG on this 4-monitor host

Measured on this machine (`gjs` + `Gly-2` typelib, replicating mutter's `load_file()` exactly —
`g_file_read` → `gly_loader_new_for_stream` → `gly_loader_load` → `gly_image_next_frame`),
with a synthetic 7008×4672 JPEG:

| Source                  | Decoded buffer               | `stride`         | loader.load | next_frame | **total**      |
| ----------------------- | ---------------------------- | ---------------- | ----------- | ---------- | -------------- |
| 7008×4672, 21.6 MiB q88 | 98,224,128 B = **93.68 MiB** | 21024 (= 7008×3) | 56–66 ms    | 285–302 ms | **342–366 ms** |
| 7008×4672, 37.8 MiB q95 | 93.68 MiB                    | 21024            | 77–82 ms    | 379–389 ms | **461–466 ms** |
| 3072×1920, 3.1 MiB q90  | 16.88 MiB                    | 9216             | 22–23 ms    | 50–53 ms   | **73–75 ms**   |
| 3840×2400, 5.0 MiB q90  | 26.37 MiB                    | 11520            | 23–27 ms    | 71 ms      | **94–98 ms**   |

`stride = width × 3` confirms glycin returns **`R8G8B8`** for a JPEG (no alpha), so
`meta_create_texture()` is called with `COGL_TEXTURE_COMPONENTS_RGB`
(`meta-background-image.c:264-268`).

### Shared-once costs

| Item                                         | Size                                                                            | Lifetime                                          |
| -------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------- |
| glycin CPU pixel buffer, `R8G8B8`            | **93.68 MiB**                                                                   | transient — freed after `cogl_texture_set_data()` |
| level-0 `CoglTexture`, `GL_RGB8`             | **93.68 MiB** if the driver stores 24 bpp; **124.91 MiB** if promoted to 32 bpp | resident for the session                          |
| mipmap level 1 (3504×2336)                   | 23.4 / 31.2 MiB                                                                 | resident                                          |
| **total resident, shared by all 4 monitors** | **~117–156 MiB**                                                                |                                                   |

On Mesa/Intel, `GL_RGB8` is in practice stored as a 32-bpp `X8B8G8R8`-style format because ISL
does not support 24-bpp tiled texture surfaces, so **124.91 MiB is the realistic figure**. I did
not instrument the driver to prove it on this host — treat the 3 vs 4 byte/pixel question as
unverified, and use the 4-byte number as the budget.

`get_best_mipmap_level(7008×4672, …)` for this host's four monitors:

| Monitor                        | FBO size  | halves               | level |
| ------------------------------ | --------- | -------------------- | ----- |
| eDP-1 3072×1920 (scale 1.7534) | 3072×1920 | 7008→3504→1752 stops | **1** |
| DVI-I-1 2560×1080              | 2560×1080 | 7008→3504→1752 stops | **1** |
| HDMI-1 1920×1080               | 1920×1080 | 7008→3504→1752 stops | **1** |
| DVI-I-2 1920×1080              | 1920×1080 | 7008→3504→1752 stops | **1** |

so only mip level 1 is generated — **not** the full 14-level chain (which would add ~1/3, i.e.
+41.6 MiB). mutter's cap is doing real work here.

(Monitor geometry read from `~/.config/monitors.xml` configuration #11. Because
`meta_backend_is_stage_views_scaled()` is true on Wayland, `texture_width = monitor_area.width × monitor_scale` — `meta-background.c:817-826` — which for eDP-1's logical 1752×1095 at scale
1.7534 is back to the physical 3072×1920.)

### Per-monitor costs — this is the only thing paid ×4

`struct _MetaBackgroundMonitor { gboolean dirty; CoglTexture *texture; CoglFramebuffer *fbo; }`
(`meta-background.c:46-51`), one per monitor, allocated in
`invalidate_monitor_backgrounds()` and filled in `meta_background_get_texture()`:

```c
monitor->texture = meta_create_texture (texture_width, texture_height, cogl_context,
                                        COGL_TEXTURE_COMPONENTS_RGBA,
                                        META_TEXTURE_FLAGS_NONE);
offscreen = cogl_offscreen_new_with_texture (monitor->texture);
monitor->fbo = COGL_FRAMEBUFFER (offscreen);
```

RGBA (4 B/px — note commit `5dc92aa134`, 2023-12-18, *reverted* an attempt to make this RGB):

| Monitor           | Pixels    | Bytes         |
| ----------------- | --------- | ------------- |
| eDP-1 3072×1920   | 5,898,240 | 22.50 MiB     |
| DVI-I-1 2560×1080 | 2,764,800 | 10.55 MiB     |
| HDMI-1 1920×1080  | 2,073,600 | 7.91 MiB      |
| DVI-I-2 1920×1080 | 2,073,600 | 7.91 MiB      |
| **total**         |           | **48.87 MiB** |

Allocated **once**, because there is one shared `MetaBackground` (§1a/§1c) — the desktop, the
Overview and the unlock dialog all reuse it.

### Grand total and the counterfactual

- Actual: **≈ 166–205 MiB** resident, of which ~117–156 MiB is the shared source texture.
- If it really were per-monitor decode: ~500–625 MiB, plus 4× ~350 ms decode.

**The real waste is not multiplication, it is resolution.** ~125–156 MiB of VRAM is held
permanently to feed 48.87 MiB of output, whose largest consumer is 3072×1920 — about 5.5× more
source pixels than the biggest monitor can show. Pre-scaling the wallpaper to 3072×1920 would
cut the level-0 texture to 22.5 MiB (+5.6 MiB mip) and the decode from ~350 ms to ~75 ms,
measured above.

### Multi-GPU: the cost is not multiplied per GPU either

mutter has one Cogl/EGL context on the chosen primary GPU (`choose_primary_gpu()`,
`meta-renderer-native.c:2438`). Secondary GPUs — the four `evdi` nodes here — get finished
frames via `MetaSharedFramebufferCopyMode` (`META_SHARED_FRAMEBUFFER_COPY_MODE_PRIMARY` /
`_SECONDARY_GPU` / `_ZERO`, `meta-renderer-native.c:1916-2055`, overridable with
`MUTTER_DEBUG_MULTI_GPU_FORCE_COPY_MODE`). The wallpaper texture therefore lives once, on the
primary GPU; what crosses to the evdi devices is the already-composited scanout buffer, which is
the normal per-frame path and not background-specific.

---

## 4. When is the background RE-DECODED?

Distinguish three different costs:

- **(A) full re-decode from disk** — ~350 ms for this file;
- **(B) per-monitor FBO re-render** — a few GPU draws, cheap;
- **(C) nothing**.

| Event                                                                                            | What happens                                                                                                                                                                                                                                                                                                                                             | Cost                                                                                                                     |
| ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Session start                                                                                    | `LayoutManager._updateBackgrounds()` → `cache.load()` → `g_task_run_in_thread(load_file)`                                                                                                                                                                                                                                                                | **A**                                                                                                                    |
| Any key changes in `org.gnome.desktop.background`, or `org.gnome.desktop.interface color-scheme` | `Background._emitChangedSignal` → `bg-changed` → old `Background` dropped, new one built, `cache.load()` again                                                                                                                                                                                                                                           | **A** if the old image was already finalised, otherwise a cache hit — see the caveat below                               |
| Wallpaper **file contents** change on disk (`GFileMonitor`)                                      | `background.js:372` `imageCache.purge(changedFile)` then `_emitChangedSignal()`                                                                                                                                                                                                                                                                          | **A**, forced                                                                                                            |
| **Monitor hotplug / reconfigure** (`monitors-changed`)                                           | Two independent things: (i) `MetaBackground::on_monitors_changed` → `invalidate_monitor_backgrounds()` frees *all* per-monitor FBOs and re-marks them dirty (`meta-background.c:121-144`); (ii) gnome-shell's `LayoutManager._monitorsChanged` → `_updateBackgrounds()` **destroys every `BackgroundManager` then recreates them** (`layout.js:543-579`) | **B** normally; **A** if the teardown transiently drops the last ref — see caveat                                        |
| Resume from suspend                                                                              | `loginManager` `prepare-for-sleep` handler → `this._refreshAnimation()`, which early-returns unless `this._animation` is set, i.e. **only for XML slideshows** (`background.js:269-275`, `327-333`)                                                                                                                                                      | **C** for a static JPEG — but resume normally also fires `monitors-changed`, so in practice you get that row's behaviour |
| Screen unlock                                                                                    | `unlockDialog`'s `BackgroundManager`s are destroyed; the shared `BackgroundSource` use-count stays > 0 because the four desktop managers still hold it                                                                                                                                                                                                   | **C**                                                                                                                    |
| **GL video memory purge**                                                                        | `on_gl_video_memory_purged()` → `meta_background_image_cache_purge()` + `set_file(..., force_reload = TRUE)` for *both* slots, then `mark_changed()` (`meta-background.c:270-294`)                                                                                                                                                                       | **A**, unconditionally                                                                                                   |

### The `monitors-changed` re-decode caveat — this is the one that can lose a race

`LayoutManager._updateBackgrounds()` destroys **all** `BackgroundManager`s *before* creating the
new ones:

```js
for (let i = 0; i < this._bgManagers.length; i++)
    this._bgManagers[i].destroy();          // layout.js:547-548
this._bgManagers = [];
...
for (let i = 0; i < this.monitors.length; i++)
    this._bgManagers.push(this._createBackgroundManager(i));
```

`BackgroundManager.destroy()` → `cache.releaseBackgroundSource()`; on the 4th call the use count
hits 0, the `BackgroundSource` is destroyed and all its `Background` objects are dropped. Since
`MetaBackgroundImageCache` holds only a weak reference (§1d), the decoded texture survives that
window **only because GJS has not collected the `MetaBackground` wrappers yet**. That is timing,
not design. If a GC lands in that window, the immediately-following `cache.load()` misses and you
eat a fresh ~350 ms decode — during which `meta_background_image_get_texture()` returns `NULL`
and `meta_background_get_texture()` returns `self->color_texture`, i.e. the flat
`primary-color` (dark grey `#282828` by default).

This is a genuine mechanism by which a very large wallpaper file makes a hotplug visibly worse,
and it is exactly the kind of thing that would be masked on a small wallpaper. GNOME 51's
strong-ref `BackgroundTextureCache` removes this specific hazard.

### `gl-video-memory-purged` — relevant because of the NVIDIA card

Emitted from `src/compositor/compositor.c:980-987`:

```c
status = cogl_driver_get_graphics_reset_status (cogl_driver);
switch (status)
  {
  case COGL_GRAPHICS_RESET_STATUS_PURGED_CONTEXT_RESET:
    g_signal_emit_by_name (priv->display, "gl-video-memory-purged");
```

`PURGED_CONTEXT_RESET` maps to NVIDIA's `GL_PURGED_CONTEXT_RESET_NV`
(`NV_robustness_video_memory_purge`), raised after suspend/resume and VT switches on the
proprietary NVIDIA driver. The handler's own comment explains why this is a full disk reload:

```c
/* The GPU memory that just got invalidated is the texture inside
 * self->background_image1,2 and/or its mipmaps. However, to save memory the
 * original pixbuf isn't kept in RAM so we can't do a simple re-upload. The
 * only copy of the image was the one in texture memory that got invalidated.
 * So we need to do a full reload from disk. */
```

(commit `a5265365dd`, "background: Reload when GPU memory is invalidated", 2019-05-23.)

Whether this fires depends on which GPU `choose_primary_gpu()` picks for the compositor context.
**On this host it does not fire.** The running `gnome-shell` (pid 6310 at time of writing) holds
`/dev/dri/card1` (`i915`) as its KMS device plus `card2`/`card3` (`evdi`); it does **not** hold
`card0` (`nvidia`). It has both render nodes open (`renderD128` → `i915`,
`renderD129` → `nvidia`), but the compositor's Cogl context is on the Intel iGPU, where
`NV_robustness_video_memory_purge` is not exposed, so `COGL_GRAPHICS_RESET_STATUS_PURGED_CONTEXT_RESET`
never occurs. This path is dormant here — but it would become live for any session that ends up
rendering on the NVIDIA card, where every resume would cost a full ~350 ms re-decode.

Connected outputs at time of writing, for reference: `card1-eDP-1`, `card1-HDMI-A-1`,
`card2-DVI-I-1`, `card3-DVI-I-2`.

### Threading note

The decode itself is off the main thread (`g_task_run_in_thread (task, load_file)`), so it does
not block the compositor. But `file_loaded()` runs on the **main thread** and does the
`cogl_texture_set_data()` of ~94 MiB in one go — that upload is a main-loop stall. I did not
measure it; it is a smaller number than the decode but it is not free, and it scales linearly
with source pixels.

---

## 5. "Black background on some monitors" — two mechanisms

There are **two independent** code paths that produce this symptom. One has an upstream issue;
the other does not. Full issue-tracker research is in the companion report (§7).

### 5a. Empty redraw clip — `mutter#4767`, the documented one

This is the one with an upstream diagnosis.
[gnome-shell#3206](https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/3206) (open since 2020,
GNOME 3.38+) was root-caused in
[mutter#4767](https://gitlab.gnome.org/GNOME/mutter/-/issues/4767) (open, 2026-04-24):
`meta_background_content_paint_content()` intersects the paint context's redraw clip with the
actor's stage rectangle, gets an empty region, and returns without painting anything.

I verified the code is present verbatim in the 50.4 tree
(`meta-background-content.c:717-724` and `:748-749`):

```c
const MtkRegion *redraw_clip;

redraw_clip = clutter_paint_context_get_redraw_clip (paint_context);
if (redraw_clip)
  {
    region = mtk_region_copy (redraw_clip);
    mtk_region_intersect_rectangle (region, &rect_within_stage);
  }
...
/* region is now in actor space */
if (mtk_region_is_empty (region))
  return;
```

One observation from reading it, offered as observation and not as a derivation of the bug:
this `redraw_clip` branch is only reached when `untransformed` is true, i.e. when
`rect_within_actor == rect_within_stage` (`:701-705`). `clutter_actor_get_content_box()` returns
actor-local coordinates starting at (0,0), so `untransformed` holds **only for the background
actor whose stage position is (0,0)**. Every other monitor's actor takes the `else` branch
(`:731-742`), which ignores `redraw_clip` entirely and uses `rect_within_actor`. That is
consistent with the reported pattern of a *specific* monitor going black rather than a random
one — but I have not independently reproduced the empty intersection, and mutter#4767 is the
authority on why it becomes empty.

A 6-line fallback patch is proposed in the issue; no merge request is attached to it. A separate
MR, `mutter!5307`, appears to target this but its status could not be read (the GitLab endpoint
returned HTTP 500 on every route the research agent tried). **Treat the fix status as unknown.**

Related open reports with the same shape:

- [gnome-shell#8034](https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/8034) — GNOME 43 & 47,
  primary lock-screen background blacks out on keypress/scroll with a secondary HDMI monitor.
- [mutter#4935](https://gitlab.gnome.org/GNOME/mutter/-/issues/4935) — **Fedora 44, mutter 50.3,
  GNOME Shell 50.3** (AMD): background turns black on unmaximise/close, recovers only by changing
  the wallpaper or restarting the session. This is the closest live match to the host in question
  by version.
- [Launchpad #2013042](https://bugs.launchpad.net/ubuntu/+source/gnome-shell/+bug/2013042)
  (In Progress) and duplicate #2128888 — lock wallpaper vanishes on second-monitor plug/unplug,
  Ubuntu 23.04 → 25.10.

`mutter#3232` (fixed via `mutter!3477`) is the one *confirmed* `cogl_framebuffer_allocate()`
failure producing a black background — but it is the **wallpaper-texture** call site
(`ensure_wallpaper_texture()`, for tiled `wallpaper` style, where the texture exceeded
`GL_MAX_TEXTURE_SIZE`), not the per-monitor FBO site in §5b.

### 5b. Sticky NULL texture after an FBO allocation failure — no upstream issue

*(This is what the source shows. There is **no** filed bug matching it: the research agent found
zero hits for `cogl_framebuffer_allocate` in either tracker, and the path logs nothing, so the
absence of journal messages is not evidence against it.)*

`meta_background_get_texture()` can return `NULL`:

```c
if (!cogl_framebuffer_allocate (monitor->fbo, &catch_error))
  {
    /* Texture or framebuffer allocation failed; it's unclear why this happened;
     * we'll try again the next time this is called. (MetaBackgroundActor
     * caches the result, so user might be left without a background.)
     */
    g_clear_object (&monitor->texture);
    g_clear_object (&monitor->fbo);
    g_error_free (catch_error);
    return NULL;
  }
```

— `meta-background.c:849-860`. The comment names the failure mode out loud.

The caching it warns about is in `meta-background-content.c:425-448`:

```c
if (self->changed & CHANGED_BACKGROUND)
  {
    CoglTexture *texture = meta_background_get_texture (self->background, self->monitor,
                                                        &self->texture_area, &wrap_mode);
    if (texture) { ... } else { self->texture_width = 0; self->texture_height = 0; }

    cogl_pipeline_set_layer_texture (self->pipeline, 0, texture);
    cogl_pipeline_set_layer_wrap_mode (self->pipeline, 0, wrap_mode);

    self->changed &= ~CHANGED_BACKGROUND;     /* cleared even when texture == NULL */
  }
```

`CHANGED_BACKGROUND` is cleared **unconditionally**, including on the `texture == NULL` path. So
a single transient FBO-allocation failure on one monitor leaves that monitor's pipeline with a
NULL layer texture and nothing queued to retry. It stays black until something else invalidates —
`MetaBackground::changed` (`mark_changed()`) or actor recreation.

Two further observations that make this reachable on a hotplug-heavy multi-GPU box:

1. **`on_monitors_changed()` does not emit `changed`.** `meta-background.c:140-144` calls only
   `invalidate_monitor_backgrounds()`, which frees the FBOs and sets `dirty = TRUE`, but does
   *not* `mark_changed()`. Any `MetaBackgroundContent` that survives a `monitors-changed` event
   is therefore never told to re-fetch. gnome-shell normally papers over this by destroying and
   recreating every background actor (`layout.js:543`), so it does not bite the stock shell —
   but an extension holding a `MetaBackgroundActor` across a hotplug would see exactly this.
2. `MetaBackgroundContent` connects to `MetaBackground::changed` and nothing else
   (`meta-background-content.c:1070-1071`); it has no `monitors-changed` handler of its own.

Consistent with the reported symptom: the Overview and window dragging keep working on the
affected monitor because those are ordinary stage-view compositing, entirely independent of
`MetaBackgroundContent`'s cached pipeline. Only the background actor is stuck.

This `g_error_free` form of the code is live in mutter **45 through 50**; only `main` has been
converted to `g_autoptr`.

### 5c. It is NOT the evdi/DisplayLink copy path

mutter's own `doc/multi-gpu.md` states that all compositing happens on the primary GPU and
secondary outputs receive copies. The per-monitor background FBO is therefore never allocated on
an evdi device. Combined with the premise that the Overview and window dragging render correctly
on the affected monitor, this argues *against* a secondary-GPU fault and *for* a background-actor
fault. The research agent found no evdi/DisplayLink issue matching "wallpaper black, shell UI
fine" — every evdi blackout report loses the shell UI too.

### 5d. Practical implication regardless of which mechanism

Anything that emits `MetaBackground::changed` un-sticks §5b, which is why the folk workaround of
toggling `gsettings set org.gnome.desktop.background picture-uri ...` works (it runs the §4 row-2
path). `Alt+F2 r` is not available on Wayland; restarting gnome-shell means logging out.

Note that the two mechanisms are distinguishable by this test: **§5a is transient** (it recovers
on the next full-surface repaint, e.g. opening the Overview), whereas **§5b is sticky** (it
survives repaints and needs a `picture-uri` change or a session restart). Which one a user is
hitting can be told apart by whether opening and closing the Overview restores the wallpaper.

---

## 6. Does anything already solve "pre-scale and cache per monitor resolution"?

**No third-party tool does.** Every GNOME-capable wallpaper tool composites *one spanned image*
across the whole virtual desktop and points `picture-uri` at it with `picture-options='spanned'`.
Details and maintenance dates are in the companion report (§8); the short version:

| Tool                              | Mechanism                                                                                           | Wayland/GNOME 50                                                                          | Maintained                                        |
| --------------------------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------- |
| HydraPaper                        | composite one spanned image                                                                         | yes                                                                                       | **dormant** — last real commit 2023-09-23         |
| Superpaper                        | GNOME path sets one spanned file                                                                    | yes                                                                                       | **stalled** — last release 2022-12-20             |
| Variety                           | no per-monitor support at all                                                                       | yes                                                                                       | releases active, README declares maintenance mode |
| azwallpaper / Wallpaper Slideshow | single `picture-uri`                                                                                | yes                                                                                       | **healthy** — v14.2, 2026-04-21                   |
| Wallshuffle (extension)           | composites a spanned JPEG **synchronously** with `GdkPixbuf.new_from_file` in the shell's main loop | yes                                                                                       | active, 2026-07-24                                |
| swww / hyprpaper / wpaperd        | `wlr_layer_shell`                                                                                   | **impossible** — mutter does not implement layer-shell (mutter#973, closed unimplemented) | —                                                 |

Superpaper *does* implement per-monitor pre-scaled crops
(`special_image_cropper` / `set_wallpaper_piecewise`), but its own docstring restricts that path
to KDE/XFCE/macOS — "systems where the wallpapers are set on a per display basis" — and GNOME is
not one of them.

Wallshuffle is worth calling out as a **negative**: compositing a spanned canvas for this host's
layout is roughly 9472×1920 ≈ 54 MB decoded, done with a synchronous GdkPixbuf load on the
shell's main loop. It makes the decode cost strictly worse, not better.

### But GNOME already ships the capability, unused

The background XML `<size>` mechanism *is* "hand mutter a small pre-scaled file per monitor",
and it needs no patching. Two things have to line up, and both do:

1. `BackgroundSource.getBackground()` only gives each monitor its own `Background` when the
   filename ends in `.xml` (§1a).

2. `Animation.update(monitor)` then passes the monitor's dimensions to gnome-desktop's
   selector (`js/ui/background.js:664-671`):

   ```js
   update(monitor) {
       this.keyFrameFiles = [];
       if (this.get_num_slides() < 1) return;
       const [progress, duration, isFixed_, filename1, filename2] =
           this.get_current_slide(monitor.width, monitor.height);
   ```

I verified the selector's behaviour empirically on this host with `gjs` against the installed
`GnomeBG-4.0` typelib, using an XML of the form:

```xml
<background>
  <static>
    <duration>31536000.0</duration>
    <file>
      <size width="1920" height="1080">/path/wall-1920x1080.jpg</size>
      <size width="2560" height="1080">/path/wall-2560x1080.jpg</size>
      <size width="3072" height="1920">/path/wall-3072x1920.jpg</size>
    </file>
  </static>
</background>
```

Results — it really does hand back a different file per requested geometry:

```
1920x1080 -> wall-1920x1080.jpg
2560x1080 -> wall-2560x1080.jpg
3072x1920 -> wall-3072x1920.jpg
1752x1095 -> wall-3072x1920.jpg      # same 1.6 aspect ratio
 800x600  -> wall-3072x1920.jpg      # nearest aspect ratio
```

A second test with two variants at the **same** aspect ratio (960×540 and 3840×2160) pins down
the tie-break:

```
 640x360  -> same-960x540.jpg
 960x540  -> same-960x540.jpg
1280x720  -> same-3840x2160.jpg
1920x1080 -> same-3840x2160.jpg
3840x2160 -> same-3840x2160.jpg
7680x4320 -> same-3840x2160.jpg
```

This matches gnome-desktop's `find_best_size()` exactly: **aspect ratio first, ties broken by
width proximity, in two passes where the first pass requires the candidate to be ≥ the monitor in
both dimensions**. (960×540 wins for a 960-wide monitor but loses for a 1280-wide one, because it
fails pass 0 there.)

**Three important caveats:**

- **The selector is keyed on resolution, not monitor identity.** This host's 3072×1920,
  2560×1080 and 1920×1080 panels are separable; the **two 1920×1080 panels cannot be given
  different images**.
- **gnome-shell passes *logical* size**, not physical — `this._layoutManager.monitors[...]`.
  For eDP-1 at scale 1.7534 that is 1752×1095, not 3072×1920. Because matching is aspect-ratio
  driven this still resolves correctly to a 1.6-ratio file, but the `<size>` numbers should be
  chosen with that in mind rather than assumed to be physical pixels.
- **Each distinct file is a separate decode and a separate resident texture.** Per-monitor
  pre-scaled files trade one 125 MiB texture for three small ones (≈22.5 + 10.5 + 7.9 MiB at
  4 B/px) — a large net win here, but it is not free, and it is not one texture.

The only thing that could separate the two identical 1080p panels is an unmerged proof of concept
(`github.com/laverdone/gnome-shell`, covered by Phoronix 2026-06-26) that patches `background.js`
with an `a{ss}` connector-name → URI map. It requires building gnome-shell from source, so it is
not a deployable option.

For reference, this host's current setting is `picture-uri = file:///home/<user>/.config/background`
— not an `.xml`, so all four monitors share one texture today, exactly as analysed in §1–§3.

---

## 7. Version-specific summary

| GNOME                       | Behaviour                                                                                                                                                                                                                                                                               |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ≤ 47                        | `MetaBackgroundImageCache` loads via **GdkPixbuf**, in-process.                                                                                                                                                                                                                         |
| 48–50 (this host: **50.4**) | `MetaBackgroundImageCache` loads via **glycin** (`gly_loader_*`), i.e. an out-of-process sandboxed Rust loader. Decode on a GTask worker thread, upload on the main thread. Cache holds a **weak** ref. `meta_background_set_file()` still exists.                                      |
| 51 / `main`                 | `meta-background-image.c` **deleted** (`3073248c2d`, `627a46b504`). API is now `meta_background_set_texture()` / `set_blend_textures()`. gnome-shell owns a `BackgroundTextureCache` (`js/ui/background.js`, `main`) that calls glycin from JS, keys on URI, and holds **strong** refs. |

Fedora 44's glycin on this host: `glycin-libs-2.1.5-1.fc44`, `glycin-loaders-2.1.5-1.fc44`.
JPEG is handled by `/usr/libexec/glycin-loaders/2+/glycin-image-rs` per
`/usr/share/glycin-loaders/2+/conf.d/glycin-image-rs.conf` — a separate sandboxed process, which
is why ~20–80 ms of the measured decode is fixed spawn/IPC overhead independent of image size.

---

## 8. Companion reports

Two parallel research agents covered the parts of the question that need the issue trackers and
the wider tool ecosystem rather than the source:

- Known black-background / hotplug bugs: `untracked/agent-reports/260911-gnome-black-background-bugs-research.md`
- Existing per-monitor wallpaper tooling: `untracked/agent-reports/260911-per-monitor-wallpaper-tools-research.md`

### Known gaps in this research

- `gitlab.gnome.org` was unreachable from this sandbox; all mutter/gnome-shell source was read
  from the GitHub mirror, and issue content came from search snippets. GitLab issue *comments*
  require auth (HTTP 401), so maintainer discussion on every cited issue is **unread**.
- `mutter!5307`'s status could not be determined (HTTP 500 on every route).
- Whether Mesa stores this `GL_RGB8` texture at 24 or 32 bpp was **not** instrumented on this
  host; the 4 B/px figure is the conservative budget, not a measurement.
- Whether GNOME 51's move of the glycin call into the JS main loop actually blocks the shell
  was **not** verified.

---

## Appendix — reproducing the measurements

The benchmark replicates mutter's `load_file()` call sequence exactly:

```js
// /tmp/wptest/bench.js — run with: gjs -m bench.js <image>
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Gly from 'gi://Gly?version=2';

const file = Gio.File.new_for_path(ARGV[0]);
const t0 = GLib.get_monotonic_time();
const stream = file.read(null);
const loader = Gly.Loader.new_for_stream(stream);
const image = loader.load();
const tLoad = GLib.get_monotonic_time();
const frame = image.next_frame();
const tFrame = GLib.get_monotonic_time();
print(JSON.stringify({
    width: frame.get_width(), height: frame.get_height(),
    stride: frame.get_stride(), buf_bytes: frame.get_buf_bytes().get_size(),
    ms_loader_load: (tLoad - t0) / 1000,
    ms_next_frame: (tFrame - tLoad) / 1000,
    ms_total: (tFrame - t0) / 1000,
}));
```

Source files consulted (all `gnome-50` branch unless noted):

- `mutter:src/compositor/meta-background-image.c`
- `mutter:src/compositor/meta-background.c`
- `mutter:src/compositor/meta-background-content.c`
- `mutter:src/compositor/meta-background-actor.c`
- `mutter:src/compositor/cogl-utils.c`
- `mutter:src/compositor/compositor.c`
- `mutter:src/meta/meta-background.h` (and the `main` variant)
- `mutter:cogl/cogl/cogl-texture.c`, `cogl-texture-2d.c`, `cogl-pipeline-layer-state.c`
- `mutter:cogl/cogl/driver/gl/cogl-texture-driver-gl.c`
- `mutter:src/backends/native/meta-renderer-native.c`
- `gnome-shell:js/ui/background.js` (and the `main` variant)
- `gnome-shell:js/ui/layout.js`, `js/ui/workspace.js`, `js/ui/unlockDialog.js`, `js/ui/screenShield.js`
