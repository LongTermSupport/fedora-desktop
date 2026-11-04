# Per-monitor wallpaper handling on GNOME/Wayland — existing tool survey

Research date: 2026-09-11. Verification host: Fedora 44, gnome-shell 50.4, mutter 50.4
(libmutter-18), gnome-desktop4 44.5. Session type: Wayland.

Where a claim is marked **VERIFIED LOCALLY**, it was checked against the binaries and
source actually installed on this machine, not against upstream docs.

---

## 0. Executive answer to the KEY QUESTION

> Does ANY existing tool "pre-scale and cache the wallpaper per monitor resolution and
> hand mutter the small pre-scaled file"?

**No tool does this on GNOME.** Every GNOME-capable tool surveyed falls into bucket (a):
composite one large spanned image covering the whole virtual desktop, write it to disk,
and point `org.gnome.desktop.background picture-uri` at it with
`picture-options='spanned'`.

The one tool that *does* implement per-monitor pre-scaled crops — **Superpaper**'s
`special_image_cropper()` / `set_wallpaper_piecewise()` — deliberately restricts that code
path to **KDE Plasma, XFCE (commented out) and macOS**, because those platforms expose a
per-screen wallpaper API. On GNOME it explicitly falls back to the single spanned file.
See §2 for the verbatim code.

**However**, the capability the question describes *does* exist in GNOME itself, unused by
any tool: the **gnome-desktop slideshow XML `<size>` variant mechanism** (§7). If
`picture-uri` points at a file whose basename ends in `.xml`, gnome-shell builds a
*separate* `Background` object per monitor, and gnome-desktop selects a different file per
monitor from a `<size width= height=>` list, matching on **aspect ratio first, then width
proximity**. That is precisely "hand mutter a small pre-scaled file per monitor" — and it
is a supported, shipped, non-patched code path. Its limitation is that selection is keyed
on **resolution/aspect, not monitor identity** (see §7.4).

---

## 1. HydraPaper

- Upstream (live): <https://gitlab.com/gabmus/hydrapaper>
- GitHub mirror (dead): <https://github.com/GabMus/HydraPaper> — README says
  "Development moved to <https://gitlab.com/gabmus/hydrapaper>"
- Flathub: <https://flathub.org/en/apps/org.gabmus.hydrapaper>

### Mechanism — CONFIRMED: composites one spanned image

Your belief is correct. HydraPaper uses Pillow to merge the per-monitor images into a
single canvas laid out to match the monitor arrangement, then sets that one file as the
wallpaper with `picture-options='spanned'`. It does not hand GNOME anything per-monitor.
Documented at <https://fedoramagazine.org/multi-monitor-wallpapers-with-hydrapaper/> and
<https://www.linuxuprising.com/2018/05/how-to-set-different-background-for.html>.
It does handle rotated monitors and mixed resolutions when building the canvas.

### Maintenance — DORMANT (~3 years)

| Signal                    | Value                                                                |
| ------------------------- | -------------------------------------------------------------------- |
| GitHub mirror last push   | 2020-06-02                                                           |
| GitHub latest release     | `1.2`, 2018-02-14                                                    |
| GitLab `last_activity_at` | 2026-08-14 (issue traffic, not code)                                 |
| GitLab last *commits*     | 2023-09-23 — "remove persist backgrounds", "bumped version to 3.3.2" |
| Flathub latest release    | **3.3.2, 2023-09-23**                                                |
| Archived?                 | No                                                                   |

So: last real code change 2023-09-23, ~2 years before this survey. Issue activity
continues but no releases. GitLab's `releases` API only lists 1.4/1.4.2 (2018/2019);
Flathub's appstream is the accurate release record.

### Wayland / GNOME 48–50

No explicit statement in the README about Wayland or a supported GNOME version range —
README says "GNOME 3, MATE and Budgie". Because the mechanism is purely
`gsettings`-based, it is display-server agnostic and should still function under GNOME
Wayland 48/49/50. **Unverified** — not installed or run on this host. The risk is not
Wayland but GTK3/libwnck3 bitrot and the `picture-uri-dark` key (added in GNOME 42), which
HydraPaper 3.3.2 predates by enough that dark-mode behaviour should be checked.

---

## 2. Superpaper

- <https://github.com/hhannine/superpaper>

### Mechanism on GNOME — CONFIRMED: single spanned image

Read from `superpaper/wallpaper_processing.py` at `master`.

`set_wallpaper_linux()` takes a **single** `outputfile` and for GNOME does:

```python
if desk_env in ["gnome", "gnome-wayland", "gnome-xorg",
                  "unity", "ubuntu",
                  "pantheon", "budgie-desktop",
                  "pop"]:
    subprocess.run(["/usr/bin/gsettings", "set",
                    "org.gnome.desktop.background", "picture-uri-dark",
                    file])
    subprocess.run(["/usr/bin/gsettings", "set",
                    "org.gnome.desktop.background", "picture-uri",
                    file])
```

Note `"gnome-wayland"` is explicitly in the list — **GNOME Wayland is an intended target**.
Note also it does **not** set `picture-options`; the user must set "Spanned" themselves.

The composite is built earlier in the same module: each monitor's region is cropped and
resized, then pasted into one canvas which is saved once:

```python
combined_image.paste(cropped_images[crp_id], DISPLAY_OFFSET_ARRAY[crp_id])
...
combined_image.save(outputfile, quality=95)
```

Its "caching" is only a double-buffer: it alternates `<profile>-a.<ext>` and
`<profile>-b.<ext>` in `TEMP_PATH` so the `picture-uri` string always changes and GNOME
notices. It is *not* a per-monitor pre-scaled cache.

### The per-monitor pre-scaled path EXISTS — but is not available to GNOME

This is the single most relevant finding in the whole survey:

```python
def set_wallpaper_piecewise(image_piece_list):
    """
    Wallpaper setter that takes already cropped images and sets them
    directly to corresponding monitors on systems where wallpapers
    are set on a monitor by monitor basis.
    ...
    Currently supported such systems are KDE Plasma and XFCE.
    """
    pltform = platform.system()
    if pltform == "Linux":
        if running_kde():
            kdeplasma_actions(None, image_piece_list)
        # desk_env = os.environ.get("DESKTOP_SESSION")
        # elif desk_env in ["xfce", "xubuntu", "ubuntustudio"]:
            # xfce_actions(None, image_piece_list)
    elif pltform == "Darwin":
        set_wallpaper_macos(None, image_piece_list=image_piece_list)
    else:
        pass
    return 0


def special_image_cropper(outputfile):
    """
    Crops input image into monitor specific pieces based on display offsets.

    This is needed on systems where the wallpapers are set on a per display basis.
    This means that the composed image needs to be re-cut into pieces which
    are saved separately.
    """
    img = Image.open(outputfile)
    outputname = os.path.splitext(outputfile)[0]
    img_names = []
    crop_id = 0
    for res, offset in zip(RESOLUTION_ARRAY, DISPLAY_OFFSET_ARRAY):
        left = offset[0]; top = offset[1]
        right = left + res[0]; bottom = top + res[1]
        crop_tuple = (left, top, right, bottom)
        cropped_img = img.crop(crop_tuple)
        fname = outputname + "-crop-" + str(crop_id) + ".png"
        img_names.append(fname)
        cropped_img.save(fname, "PNG")
        crop_id += 1
    return img_names
```

The docstring is the answer to the key question in the author's own words: per-monitor
files are produced only "on systems where the wallpapers are set on a per display basis",
and GNOME is not one of them. The GNOME path never calls `special_image_cropper`.
Note it also crops *from the already-composed full canvas*, so it does not avoid the cost
of building the big image — it adds a re-cut on top.

### Feature set

README claims "Set different image on every display" and "Span images on groups of
displays". On GNOME both are realised through the same single spanned file.
Advanced spanning offers pixel-density correction, bezel correction and perspective
correction — genuinely unique features, all of which only make sense on a spanned canvas.

### Maintenance — STALLED

| Signal         | Value                    |
| -------------- | ------------------------ |
| Last push      | 2025-03-27               |
| Latest release | `v2.2.1`, **2022-12-20** |
| Stars          | ~1.36k                   |
| Archived?      | No                       |

~18 months since the last commit, ~3.7 years since the last release.

### Wayland

README lists desktop environments (BSPWM, Budgie, Cinnamon, Gnome, i3, KDE, LXDE/LXQt,
Mate, Pantheon, XFCE) and makes **no statement about Wayland** either way. The code
recognises `DESKTOP_SESSION=gnome-wayland`, so GNOME Wayland is handled. Caveat: it is a
wxPython GUI app; display enumeration on Wayland is a known weak point for wx — it may
mis-detect monitor geometry under Wayland. **Unverified.**

---

## 3. Variety

- <https://github.com/varietywalls/variety>

### Mechanism — single wallpaper only; NO per-monitor support

Variety is a wallpaper *downloader and rotator* (Wallhaven, Unsplash, Bing, Reddit, …).
It sets one wallpaper. Per-monitor support has been an open request for ~8 years:

| Issue/PR                                                   | Title                                              | Opened     | State (2026-09-11)     |
| ---------------------------------------------------------- | -------------------------------------------------- | ---------- | ---------------------- |
| [#96](https://github.com/varietywalls/variety/issues/96)   | Multi monitor support                              | 2018-10-26 | **open**               |
| [#469](https://github.com/varietywalls/variety/issues/469) | Different wallpapers and settings for each monitor | 2021-11-14 | **open**               |
| [#574](https://github.com/varietywalls/variety/pull/574)   | show different picture for multiple monitors       | 2022-10-12 | **open** (unmerged PR) |

So Variety solves none of (a), (b) or (c).

### Maintenance — ACTIVE but in declared maintenance mode

| Signal         | Value                                    |
| -------------- | ---------------------------------------- |
| Last push      | **2026-09-11** (same day as this survey) |
| Latest release | `0.9.1`, **2026-08-30**                  |
| Stars          | ~1.7k                                    |

The README states the project is in **maintenance mode** with limited developer
availability for triaging issues or implementing major features — which is consistent with
the three multi-monitor requests sitting open. Do not expect per-monitor support to land.

---

## 4. azwallpaper ("Wallpaper Slideshow" by Andrew Zaech)

- <https://gitlab.com/AndrewZaech/azwallpaper>
- <https://extensions.gnome.org/extension/6281/wallpaper-slideshow/>

Not "Azan" — it is **A**ndrew **Z**aech's prefix, the same convention as his ArcMenu
(`arcmenu`) work.

### Mechanism — single URI; NO per-monitor support

Read from `src/slideshow.js` at `main`. It holds one
`Gio.Settings({schema: 'org.gnome.desktop.background'})` and does exactly:

```js
this._backgroundSettings.set_string('picture-uri', uri);
this._backgroundSettings.set_string('picture-uri-dark', uri);
```

Its only uses of "monitor" are unrelated to wallpapers:

- `display.get_monitor_in_fullscreen(i)` — to pause the slideshow when something is fullscreen
- `Gio.FileMonitor` — to watch the slideshow directory for changes

No compositing, no per-monitor anything. It is a directory rotator plus a Bing
image-of-the-day downloader.

### Maintenance — VERY ACTIVE, and current for GNOME 50

| Signal                       | Value                                                      |
| ---------------------------- | ---------------------------------------------------------- |
| Last commit                  | 2026-08-02                                                 |
| `last_activity_at`           | 2026-08-06                                                 |
| Tags                         | `v14.2` 2026-04-21, `v14.1` 2026-03-11, `v14.0` 2025-12-05 |
| EGO supported shell versions | **45, 46, 47, 48, 49, 50**                                 |
| EGO latest version           | 14.2                                                       |

This is the healthiest GNOME-native wallpaper extension found — it just does not do what
you need.

---

## 5. GNOME Shell extensions

Surveyed via the extensions.gnome.org query API
(`https://extensions.gnome.org/extension-query/?search=…&shell_version=50`) across the
search terms `wallpaper`, `background`, `monitor wallpaper`: **54 distinct extensions**
declare support for GNOME Shell 50.

### 5.1 Wallshuffle — the ONLY EGO extension doing per-monitor wallpapers

- <https://extensions.gnome.org/extension/10449/wallshuffle/>
- <https://github.com/cwittenberg/wallshuffle>
- UUID `wallshuffle@cwittenberg`, EGO version **7**, shell **46, 47, 48, 49, 50**
- Last push **2026-07-24**, not archived, ~7 stars, **no LICENSE file in the repo**

EGO description: *"GNOME applies a single background across every display. This extension
changes that. Set a unique wallpaper for each monitor, choose how every image is rendered,
and automatically shuffle backgrounds on a configurable timer."*

**Mechanism — bucket (a): it composites one big spanned JPEG, inside the shell process.**

From `extension.js`:

```js
const dest = GdkPixbuf.Pixbuf.new(GdkPixbuf.Colorspace.RGB, false, 8, globalBox.w, globalBox.h);
...
src = GdkPixbuf.Pixbuf.new_from_file(imgPath);
...
const monBox = { w, h, targetX: mon.geom.x - globalBox.minX, targetY: mon.geom.y - globalBox.minY };
renderStrategy.render(dest, src, monBox, globalBox);
...
const outDir = GLib.build_filenamev([GLib.get_user_cache_dir(), 'wallshuffle']);
const outFilename = `spanned-bg-${timestamp}.jpg`;
dest.savev(outPath, 'jpeg', ['quality'], ['100']);
...
this._bgSettings.set_string('picture-options', 'spanned');
this._bgSettings.set_string('picture-uri', `file://${outPath}`);
this._bgSettings.set_string('picture-uri-dark', `file://${outPath}`);
```

`rendering.js` provides per-monitor render strategies — `zoom`, `fit`, `fill`, `centre`,
`tile`, `span`, `stretch` — each of which scales/crops the source *for that monitor's box*
and then `copy_area`s the result into the **shared global canvas** at
`monBox.targetX/targetY`. So it gets per-monitor *cropping correctness* right, then throws
away the benefit by flattening into one spanned file.

Two consequences worth flagging for your decode-cost concern (c):

1. `GdkPixbuf.Pixbuf.new_from_file()` is **synchronous** and runs **in gnome-shell's main
   loop**. Decoding a large source image will stall the compositor.
2. The destination canvas is the full virtual desktop. For a 3072x1920 + 1920x1080 +
   1920x1080 + 2560x1080 layout arranged horizontally that is ~9472x1920 ≈ 18.2 MPx, i.e.
   ~54 MB of RGB888 held in the shell's address space, re-encoded to JPEG q100 on every
   shuffle tick. This makes the decode cost *worse*, not better.

So Wallshuffle solves (a) and (b) but actively worsens (c).

### 5.2 Random Wallpaper

- <https://extensions.gnome.org/extension/1040/random-wallpaper/>, UUID
  `user@example.com`, shell **48, 49, 50** among many older
- Multi-monitor support is **delegated to third-party tools**: the description states
  *"Support for multiple monitors using third party tools * Hydra Paper * Superpaper"* —
  i.e. it shells out, and inherits the spanned-image mechanism of whichever is installed.

### 5.3 Everything else — none do per-monitor

| Extension                         | UUID                                                                | Shell 50?             | What it does                                        |
| --------------------------------- | ------------------------------------------------------------------- | --------------------- | --------------------------------------------------- |
| Wallpaper Slideshow (azwallpaper) | `user@example.com`                                | yes (43–50)           | directory rotation + Bing; single URI (§4)          |
| Bing Wallpaper                    | `user@example.com`                                 | yes (3.18–50)         | single Bing image of the day                        |
| DeskChanger                       | `user@example.com`                                  | yes                   | single-wallpaper changer with profiles              |
| 2 Wallpapers                      | `2wallpapers@dudumaroja`                                            | yes (45–50)           | swaps wallpaper by *window presence*, not monitor   |
| NASA APOD Wallpaper Changer       | `user@example.com`                                         | yes                   | single image                                        |
| Wallpaper Carousel                | `user@example.com`                                      | yes (48–50)           | folder rotation, single image                       |
| Fresh Web Wallpaper               | `user@example.com`                                  | yes (45–50)           | Bing/NASA/Peapix, single image                      |
| Immich Wallpaper                  | `user@example.com`                               | yes (45–50)           | Immich server photos, single image                  |
| Workspace Wallpapers              | `workspace-wallpapers@ero`, `user@example.com` | yes (50)              | per-**workspace**, not per-monitor                  |
| Static Workspace Background       | `user@example.com`               | yes (48–50)           | freezes bg across workspace switches                |
| Blur wallpaper                    | `user@example.com`                                       | yes (46–50)           | pre-renders a blurred copy "to improve performance" |
| Gnome Live Wallpaper              | `user@example.com`                                    | yes (45–50)           | animated/live wallpapers                            |
| Walkpaper / Walkpaper2            | `walkpaper*`                                                        | —                     | per-**workspace**                                   |
| Wallpaper Switcher                | `WallpaperSwitcher@Rishu`                                           | **no** — max shell 44 | per-workspace; stale                                |
| Wallpaper Mode                    | ext 3324                                                            | —                     | rendering mode toggler                              |

**"Walltaker" is not on extensions.gnome.org** — a search for it returns only fuzzy
name-matches (WallPager, Wallhub, Wallpicker, Wallfred, Wallshuffle), and the closest
candidates (Wallhub shell 45–47, Wallpicker 46–48, Wallfred 45–47, WallPager 45–46) are
all **stale before GNOME 49/50** and none are per-monitor. If you meant the
walltaker.joi.how client, that is a fetch-and-set client, not a per-monitor tool.

"Desktop Wallpaper Switcher" as a distinct extension name does not exist for shell 50;
the matches are the per-workspace switchers above.

### 5.4 No extension overrides BackgroundManager for per-monitor

Nothing on EGO patches `Main.layoutManager._bgManagers` / `BackgroundSource.getBackground`
to give genuinely independent per-monitor `Background` objects. Wallshuffle — the only one
that even claims per-monitor — does not touch `background.js` at all; it only writes a
file and flips GSettings.

### 5.5 The out-of-tree PoC that DOES override background.js

Reported by Phoronix on **2026-06-26**:
<https://www.phoronix.com/news/PoC-Per-Background-GNOME>

- Author: Gianluca Masci
- Repo/design doc:
  <https://github.com/laverdone/gnome-shell/blob/per-monitor-backgroud/per-monitor-background.md>
  (note the typo `backgroud` in the branch name)
- Introduces a single GSettings key **`org.gnome.shell per-monitor-background`**, an
  `a{ss}` dict mapping **monitor connector name → `file://` URI** (e.g. `eDP-1`, `DP-1`)
- Patches `js/ui/background.js` to look up the current monitor in that dict, plus a
  GSettings listener to trigger redraws; patches `js/ui/backgroundMenu.js` to add a
  "Per-Monitor Backgrounds…" desktop context-menu entry; ships a GUI helper
- Status: **proof of concept, not upstreamed, no merge request filed.** Requires building
  gnome-shell from source. The author notes it was "AI supported in its creation".
- Target GNOME Shell version is **not stated** in the design doc.

This is architecturally the right answer (keyed on **connector name**, so it disambiguates
two identical 1920x1080 panels — the one thing the XML mechanism cannot do), but it is a
patched shell, not an extension, and is unusable as infrastructure today.

---

## 6. swww / awww / hyprpaper / wpaperd — all wlroots-only, all unusable under mutter

### Why they cannot work on GNOME

These are Wayland *clients* that paint the desktop by creating a surface on the background
layer via the **`wlr-layer-shell-unstable-v1`** protocol
(<https://wayland.app/protocols/wlr-layer-shell-unstable-v1>). That protocol is
implemented by wlroots (sway, Hyprland, Wayfire), Smithay (COSMIC) and KWin — but **not by
mutter**. Without it, there is no way for an external client to place a surface beneath
the desktop, so the tools have nothing to attach to.

**VERIFIED LOCALLY:** scanning every mutter 50.4 shared object on this host
(`/usr/lib64/libmutter-18.so.0`, `/usr/lib64/libmutter-18.so.0.0.0`, and all of
`/usr/lib64/mutter-18/*.so*`) for the byte string `wlr_layer_shell` returns **absent in
every file**. Mutter 50.4 does not implement the protocol.

Upstream request **[GNOME/mutter#973 "implement layer_shell protocol"](https://gitlab.gnome.org/GNOME/mutter/-/issues/973)**
was opened 2019-12-14 and is **closed** (last updated 2024-02-20) without the protocol
being implemented.

swww says so itself, verbatim in its README: it requires *"a compositor that implements the
wlr-layer-shell (typically wlroots based compositors)"* and *"Note that this means `swww`
will not run on Gnome, because it does not implement the `wlr-layer-shell` protocol."*

### Maintenance

| Tool                    | Repo                                    | Status               | Last push          | Latest release        |
| ----------------------- | --------------------------------------- | -------------------- | ------------------ | --------------------- |
| swww                    | <https://github.com/LGFae/swww>         | **ARCHIVED**         | 2025-10-31         | `v0.11.2`, 2025-09-20 |
| awww (swww's successor) | <https://codeberg.org/LGFae/awww>       | active, not archived | updated 2026-09-01 | — (433 stars)         |
| hyprpaper               | <https://github.com/hyprwm/hyprpaper>   | active               | 2026-08-13         | `v0.8.4`, 2026-04-29  |
| wpaperd                 | <https://github.com/danyspin97/wpaperd> | active               | 2026-08-10         | `1.3.0`, 2026-05-27   |

swww was renamed and moved: its README states *"This project has been renamed `awww` and
moved to Codeberg."* The rename changes nothing about GNOME compatibility — `awww` is
still a layer-shell client.

**Verdict: all four are non-starters on GNOME/Wayland. Not "degraded", not "needs a flag" —
structurally impossible.**

---

## 7. gnome-backgrounds XML slideshow format — the mechanism that actually exists

This is the most useful part of the survey. Everything below was read from the code
**actually installed on this host**.

### 7.1 Two different XML formats are routinely confused — do not mix them up

| Format                                   | Root element              | Lives in                                       | Purpose                                                                                            |
| ---------------------------------------- | ------------------------- | ---------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| **wallpaper list** (`gnome-wp-list.dtd`) | `<wallpapers><wallpaper>` | `/usr/share/gnome-background-properties/*.xml` | populates the *chooser* in Settings; `<filename>`, `<options>`, `<pcolor>` …                       |
| **slideshow / animation**                | `<background>`            | pointed at by `picture-uri`                    | what gnome-shell actually *renders*; `<starttime>`, `<static>`, `<transition>`, `<file>`, `<size>` |

Only the **second** is relevant. Most web guidance (including
<https://help.gnome.org/admin/system-admin-guide/stable/backgrounds-extra.html.en>)
describes the first.

**VERIFIED LOCALLY:** this host has 30 wp-list XMLs in
`/usr/share/gnome-background-properties/` and exactly one slideshow XML,
`/usr/share/backgrounds/f44/default/f44.xml` (the Fedora 44 day/night default). None of
them use `<size>` — the variant mechanism is shipped but unused by any distro content.

### 7.2 gnome-shell only creates per-monitor Backgrounds for `.xml` — CONFIRMED

**VERIFIED LOCALLY** by extracting the shipped JS from the installed 50.4 build:

```
gresource extract /usr/lib64/gnome-shell/libshell-18.so /org/gnome/shell/ui/background.js
```

(The shell's JS is embedded in `libshell-18.so`, not in any `/usr/share/gnome-shell/*.gresource`.)

`BackgroundSource.getBackground()`, line 579 of the shipped `background.js`:

```js
getBackground(monitorIndex) {
    let file = null;
    let style;
    ...
    // Animated backgrounds are (potentially) per-monitor, since
    // they can have variants that depend on the aspect ratio and
    // size of the monitor; for other backgrounds we can use the
    // same background object for all monitors.
    if (file == null || !file.get_basename().endsWith('.xml'))
        monitorIndex = 0;

    if (!(monitorIndex in this._backgrounds)) {
        ...
        this._backgrounds[monitorIndex] = background;
    }
    return this._backgrounds[monitorIndex];
}
```

Your hint is exactly right, and the upstream comment states the intent explicitly. A
plain `.jpg`/`.png` collapses every monitor to `monitorIndex = 0` — one shared `Background`
object, one `Meta.Background`, one texture, scaled/cropped independently per monitor by
the `BackgroundActor`. There is no per-monitor pre-scaling and no way to introduce any.

**Two conditions must both hold** for the animated path:

1. `getBackground()`: `file.get_basename().endsWith('.xml')` — a *basename* test, so the
   file must literally be named `*.xml`.
2. `Background._loadFile()` (line 491): the sniffed content type must be `application/xml`:

```js
const contentType = info.get_content_type();
if (contentType === 'application/xml')
    this._loadAnimation(file);
else
    this._loadImage(file);
```

### 7.3 How the per-monitor variant is selected

`Background._updateAnimation()` (line 387) passes the monitor geometry down:

```js
this._animation.update(this._layoutManager.monitors[this._monitorIndex]);
const files = this._animation.keyFrameFiles;
```

The `Animation` class (a GJS wrapper over `GnomeBG.BGSlideShow`) at line 664:

```js
update(monitor) {
    this.keyFrameFiles = [];
    if (this.get_num_slides() < 1)
        return;
    const [progress, duration, isFixed_, filename1, filename2] =
        this.get_current_slide(monitor.width, monitor.height);
    ...
}
```

**Only `monitor.width` and `monitor.height` cross the boundary.** No connector name, no
monitor index, no scale factor, no position. This is the hard constraint on the whole
approach.

`gnome-shell` imports `gi://GnomeBG` — **VERIFIED LOCALLY**: `GnomeBG-4.0.typelib` is
provided by `gnome-desktop4-44.5` and does contain `BGSlideShow`
(`GnomeDesktop-4.0.typelib` does not).

`gnome_bg_slide_show_get_current_slide()` in `libgnome-desktop/gnome-bg-slide-show.c`
delegates to `find_best_size()` for both keyframes:

```c
if (file1 && slide->file1)
    *file1 = find_best_size (slide->file1, width, height);
if (file2 && slide->file2)
    *file2 = find_best_size (slide->file2, width, height);
```

And `find_best_size()` — **this is the exact selection rule**:

```c
static const char *
find_best_size (GSList *sizes, gint width, gint height)
{
	GSList *s;
	gdouble a, d, distance;
	FileSize *best = NULL;
	gint pass;

	a = width/(gdouble)height;
	distance = 10000.0;

	for (pass = 0; pass < 2; pass++) {
		for (s = sizes; s; s = s->next) {
			FileSize *size = s->data;

			if (pass == 0 && (size->width < width || size->height < height))
				continue;

			d = fabs (a - size->width/(gdouble)size->height);
			if (d < distance) {
				distance = d;
				best = size;
                        }
			else if (d == distance) {
				if (abs (size->width - width) < abs (best->width - width)) {
					best = size;
				}
			}
		}

		if (best)
			break;
	}

	return best->file;
}
```

Answering the question precisely — **it selects on aspect ratio first, then width**:

- **Pass 0** considers only candidates that are `>=` the monitor in **both** width and
  height (so exact-size pre-scaled variants qualify; anything smaller in either dimension
  is skipped).
- Among those, it minimises `|monitor_aspect − candidate_aspect|`.
- Ties in aspect ratio are broken by minimal `|candidate.width − monitor.width|`.
- **Pass 1** repeats with the size filter dropped, if pass 0 found nothing.
- Not pixel count, not area, not an exact-match lookup.

Sharp edges:

- `return best->file` **dereferences `best` unconditionally** — an empty `<size>` list
  would crash. Always emit at least one variant.
- `distance` is not reset between passes; harmless only because pass 0 `continue`s before
  computing `d`.

### 7.4 CAN the XML specify different FILES for different monitors? — Yes, but keyed on resolution, not identity

The parser (`handle_start_element` / `handle_text`) builds `slide->file1` as a **list** of
`FileSize` structs. A `<file>` (or `<from>`/`<to>`) element may contain **multiple `<size>`
children**, each with its own `width`/`height` attributes and its own path as text content.
`stack_is(self, "size", "file", "static", "background", NULL)` enforces the exact nesting,
and `has_multiple_sizes` is set as soon as a second entry appears.

So the working shape is:

```xml
<background>
  <static>
    <duration>31536000.0</duration>
    <file>
      <size width="3072" height="1920">/path/pre-scaled-3072x1920.jpg</size>
      <size width="2560" height="1080">/path/pre-scaled-2560x1080.jpg</size>
      <size width="1920" height="1080">/path/pre-scaled-1920x1080.jpg</size>
    </file>
  </static>
</background>
```

`<starttime>` may be omitted; a single long-duration `<static>` slide gives a
non-animating background. `<transition>` variants use `<from>`/`<to>`, each of which takes
its own `<size>` list.

**This IS "hand mutter a small pre-scaled file per monitor".** gnome-shell will create four
separate `Background` objects (one per monitor index), each of which resolves a different
`<size>` entry and calls `set_file()` with a small, already-correct image. Nothing decodes
the huge original. It satisfies (b) and (c) directly.

**The limitation, stated precisely:** selection is a pure function of `(width, height)`.
For your layout —

| Monitor | Resolution | Aspect | Distinguishable?               |
| ------- | ---------- | ------ | ------------------------------ |
| A       | 3072x1920  | 1.600  | yes                            |
| B       | 1920x1080  | 1.778  | **no — B and C are identical** |
| C       | 1920x1080  | 1.778  | **no — B and C are identical** |
| D       | 2560x1080  | 2.370  | yes                            |

— three distinct variants are addressable, but **the two 1920x1080 panels will always
resolve to the same file**. Goal (b) (per-monitor correct crop/pre-scale) is fully
achievable. Goal (a) (a genuinely *different* image on each monitor) is achievable for
A/D/{B,C} but not between B and C. Only the §5.5 connector-name PoC can separate those.

Also note `monitor.width`/`monitor.height` from `layoutManager.monitors` are **logical**
pixels. Under fractional or 2x scaling the values handed to `find_best_size` will not be
the panel's native resolution, so variants must be authored against logical geometry.
**Unverified on this host** (no HiDPI monitor to test against) — worth confirming before
relying on it.

### 7.5 One shared Animation object across monitors

`BackgroundCache.getAnimation()` (line 170) caches **one** `Animation` per
`settingsSchema`, shared by every per-monitor `Background`:

```js
let animation = this._animations[params.settingsSchema];
if (animation && _fileEqual0(animation.file, params.file)) { ...return; }
animation = new Animation({file: params.file});
```

`update(monitor)` mutates that shared object's `keyFrameFiles`. It is safe because
`update()` assigns a **fresh array** and `_updateAnimation()` captures the reference
synchronously on the next line — but it does mean the object is stateful and shared, which
is worth knowing if you ever extend this path.

### 7.6 Current state of this host

**VERIFIED LOCALLY:**

```
org.gnome.desktop.background picture-uri      -> 'file://<home>/.config/background'
org.gnome.desktop.background picture-uri-dark -> 'file://<home>/.config/background'
org.gnome.desktop.background picture-options  -> 'zoom'
```

The basename is `background` — **no `.xml` suffix** — so `getBackground()` forces
`monitorIndex = 0` and all four monitors share a single `Background` object and a single
decoded texture, zoom-cropped independently per monitor. This is precisely the situation
described in the problem statement, confirmed on the live system.

---

## 8. Summary matrix

| Tool                   | Per-monitor?              | Mechanism                                                     | GNOME 48/49/50 Wayland                                         | Maintained (2026-09)                                                                             |
| ---------------------- | ------------------------- | ------------------------------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| HydraPaper             | yes (visually)            | (a) composite one spanned image, Pillow                       | likely yes, unverified                                         | **dormant** — last code 2023-09-23, rel 3.3.2                                                    |
| Superpaper             | yes (visually)            | (a) on GNOME; (b) exists but KDE/XFCE/macOS only              | yes (`gnome-wayland` handled); wx monitor detection unverified | **stalled** — push 2025-03-27, rel v2.2.1 2022-12                                                |
| Variety                | **no**                    | single wallpaper                                              | yes                                                            | active (rel 0.9.1 2026-08-30) but **maintenance mode**; 3 open multi-monitor requests since 2018 |
| azwallpaper            | **no**                    | single `picture-uri`                                          | yes, shell 43–50                                               | **very active** — v14.2 2026-04-21                                                               |
| Wallshuffle            | yes (visually)            | (a) composite spanned JPEG **inside gnome-shell**, GdkPixbuf  | yes, shell 46–50                                               | active — push 2026-07-24; 7 stars, no LICENSE                                                    |
| Random Wallpaper       | via HydraPaper/Superpaper | (a), delegated                                                | yes, shell 48–50                                               | active                                                                                           |
| swww / awww            | n/a                       | wlr-layer-shell                                               | **impossible**                                                 | swww archived; awww active 2026-09-01                                                            |
| hyprpaper              | n/a                       | wlr-layer-shell                                               | **impossible**                                                 | active, v0.8.4 2026-04-29                                                                        |
| wpaperd                | n/a                       | wlr-layer-shell                                               | **impossible**                                                 | active, 1.3.0 2026-05-27                                                                         |
| laverdone PoC          | **yes, truly**            | patches `background.js`, keyed on connector name              | requires building gnome-shell from source                      | PoC only, never submitted upstream                                                               |
| **GNOME XML `<size>`** | **yes, by resolution**    | **(b) pre-scaled file per monitor, selected by aspect+width** | **shipped and supported**                                      | part of gnome-desktop / gnome-shell                                                              |

## 9. What is unverified

- HydraPaper and Superpaper were **not installed or executed** on this host. Their GNOME
  Wayland behaviour is inferred from source reading, not observation. Superpaper's
  wxPython monitor enumeration under Wayland is the specific risk.
- The end-to-end XML `<size>` mechanism was verified by reading the shipped gnome-shell
  50.4 JS and gnome-desktop `master` C, but **not by authoring a test XML and observing
  four monitors** — no system changes were made.
- Whether `layoutManager.monitors[i].width/height` are logical or physical pixels under
  fractional scaling was **not** empirically confirmed.
- gnome-desktop C source was read from GitHub `master`, not from the Fedora
  `gnome-desktop4-44.5` source RPM; a distro patch could in principle differ, though the
  `GnomeBG-4.0.typelib` ABI matches.
- Wallshuffle's runtime behaviour (shell stall magnitude, memory) is reasoned from its
  source, not measured.
