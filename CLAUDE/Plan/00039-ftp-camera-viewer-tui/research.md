# Research: single-window image viewer + launcher TUI for `ftp-camera`

Captured 2026-05-20. Backing document for Plan 00039. Headline
recommendation: **geeqie for the viewer (already deployed), `gum choose`
for the TUI (verify Fedora packaging on host)**.

> **2026-05-20 — TWO revisions after host probe + manual test:**
>
> **R1 (after probe):** Geeqie 2.6.1 has NO `--remote` flag — the
> claim below was based on stale 1.x docs. Pick temporarily switched
> to `imv`.
>
> **R2 (after manual test):** Geeqie 2.x is nevertheless **implicitly
> single-instance** — a plain second `geeqie /path/B.jpg` invocation
> loads B into the existing window. Confirmed live on the host. The
> mechanism described below is wrong, but the headline (geeqie wins,
> zero new install) is right.
>
> **Final pick: `geeqie` (primary) + `gum` (TUI).** The right command
> is just `geeqie "$file" &`, not `geeqie --remote file:…`. See Plan
> 00039 _Notes & Updates → 2026-05-20 (second revision)_ for the full
> account. The body of this document is preserved as the original
> research input — its description of HOW geeqie achieves single-
> instance is wrong, but everything else (the imv/vimiv-qt/swayimg
> comparisons, the TUI analysis) stands.

## Problem statement

`ftp-camera` already sorts and copies uploads in real time
(`--async-copy`). We need a new mode that ALSO displays each new photo
in **one** image viewer window — subsequent photos update the same
window, not spawn a new one. Constraints: Fedora 43 + GNOME 48 Wayland,
bash-drivable, low latency, doesn't block the FTP server, ideally
supports flag/delete-from-viewer.

## 1. Image viewer candidates

### `imv` — Wayland-native, IPC via Unix socket

- **Single-instance / remote**: Yes. Auto-creates a socket at
  `$XDG_RUNTIME_DIR/imv-$PID.sock`. Drive with `imv-msg <PID> <command>`.
- **Replace-current-image pattern**:
  ```sh
  imv-msg "$PID" close all
  imv-msg "$PID" open "$NEW"
  ```
  or `open <file>` plus `goto -1`.
- **RAW (.arw)**: No native RAW. Would need a wrapper (`dcraw_emu -e` or
  `exiftool -b -PreviewImage`) — but the camera ships JPG alongside, so
  just feed the JPG.
- **Wayland**: Native (wlroots-style; works on GNOME/Mutter).
- **Fedora package**: `imv` — availability on Fedora 43 to verify on
  host (`dnf info imv`).
- **Delete-from-viewer**: configurable bindings in `~/.config/imv/config`,
  e.g. `D = exec rm "$imv_current_file"; next; close 1`.
- **Cost**: PID-file plumbing. We need to track `$PID` between the start
  call and every subsequent message — not hard, but extra state.

### `feh` — X11 only (XWayland)

- **Single-instance / remote**: No native IPC. The classic workaround
  (`pkill feh && feh file &`) is racy and spawns a new window each time.
  Disqualified.

### `nsxiv` / `sxiv` — X11

- No built-in IPC. The `--read-from-stdin` mode (`nsxiv -i < /tmp/fifo`)
  lets you feed paths but APPENDS only — no "show this one now"
  primitive. Marginal at best.

### `geeqie` — GTK, `--remote`

- **Single-instance / remote**: First-class. `geeqie --remote file:/path/x.arw`
  opens the file in the already-running instance (and starts one if
  none). Also supports `--remote --next`, `--first`, `--quit`,
  `--slideshow-start`, `--tools-show`, etc.
- **RAW (.arw)**: Yes — libraw-backed, handles Sony ARW natively. Reads
  embedded previews for speed.
- **Wayland**: GTK3 — runs natively on Wayland (no XWayland).
- **Fedora package**: `geeqie` — **already in `play-photography.yml`**
  (line 44, summarised at line 298). Zero install cost.
- **Delete-from-viewer**: built-in `Ctrl+D` trash, configurable.
- **Cost**: GTK app startup is heavier than `imv`. First-frame latency
  is the main risk — mitigated by pre-warming the window at server
  start.

### `qview`

- Qt-based. Has a `--single-instance` flag but it just refocuses an
  existing window; passing a new file from CLI to the existing window
  doesn't appear to be supported via documented flags. Skip.

### `swayimg` — Wayland-native

- Newer versions expose an IPC API. Older builds lack it. Less proven
  than imv for this use case, and Fedora packaging is patchy. Held as a
  distant alternative.

### `gthumb` — GNOME

- Image browser, not designed for "drop in one file" workflows. No
  documented `--remote`-style flag to push a file into an existing
  window. Each invocation spawns its own window. Skip.

### `eog` / `loupe`

- **eog**: No reliable CLI IPC. `--single-window` behaviour drifts
  across GNOME versions.
- **loupe** (GNOME 45+ default, Rust): D-Bus activation
  (`org.gnome.Loupe`) but `Application.Open` calls open new windows in
  current builds. Not suitable.

### Modern Rust/Go options

- **oculante**: cross-platform, RAW-aware via libraw bindings — no
  daemon/IPC. Skip.
- **vimiv-qt**: socket-driven (`vimiv --command open …`). Works, but
  Fedora packaging is third-party COPR. Not worth it when geeqie covers
  the case.
- **RapidRAW**: full editor, wrong tool here.

## 2. Recommendation

| Criterion       | geeqie (primary)                           | imv (secondary)                |
| --------------- | ------------------------------------------ | ------------------------------ |
| Single-instance | `--remote file:…` — designed for it        | `imv-msg $PID open …`          |
| Wayland         | GTK3 native                                | wlroots-native, works on GNOME |
| RAW (.arw)      | Yes (libraw)                               | No — feed it the JPG sibling   |
| Performance     | Heavier startup; pre-warm to mitigate      | Very light                     |
| Fedora package  | `geeqie` (already in play-photography.yml) | `imv` (verify on host)         |
| Bash scripting  | One-liner; no PID tracking                 | Needs PID file                 |
| Delete bonus    | Ctrl+D to trash                            | Custom binding                 |

**Pick geeqie.** Already deployed, ARW-native, stable `--remote` API,
trash built-in. Switch to `imv` only if first-frame latency during
bursts proves disruptive — in which case we'd feed `imv` the JPG sibling.

Wrapper sketch (the only new logic needed inside `ftp-camera`):

```bash
display_in_viewer() {
    local file="$1"
    # Geeqie's --remote starts the instance if none exists,
    # else swaps the displayed file in-place. Fire-and-forget; viewer
    # is the user's process, not ours.
    geeqie --remote "$file" >/dev/null 2>&1 &
}
```

Pre-warm variant (recommended once we measure latency): launch
`geeqie &` once during the server-startup banner before the first
upload arrives, so the window is already up by the time the camera
sends a frame.

## 3. TUI menu

| Tool         | Bash integration                                      | Dep cost                           | UX                                     |
| ------------ | ----------------------------------------------------- | ---------------------------------- | -------------------------------------- |
| `gum choose` | `MODE=$(gum choose default async async-copy …)`       | Verify `dnf info gum` on host      | Best: arrows, filter, colours, clean   |
| `fzf`        | `MODE=$(printf '%s\n' default … \| fzf --height 40%)` | Universally available (`dnf`)      | Powerful but overkill for a fixed menu |
| `whiptail`   | `whiptail --menu … 3>&1 1>&2 2>&3` — fd juggling      | Preinstalled                       | Looks dated, but always works          |
| Textual (py) | Dedicated `.py` script, mode via stdout/exit          | Already used (`wsi-model-manager`) | Overkill for a one-shot launcher       |

**Pick `gum choose`** if available on Fedora 43; one-line bash
integration, idiomatic, looks good. Otherwise `fzf` — same one-line
pattern with a slightly different UX. `whiptail` is the
non-aesthetic fallback. `textual` is the wrong tool for a single-pick
launcher even though the project uses it elsewhere.

Suggested entrypoint shape:

```bash
if [[ $# -eq 0 ]]; then
    MODE=$(gum choose --header 'ftp-camera mode' \
        default async async-copy async-copy-viewer \
        sort push copy prune pass) || exit 0
    [[ $MODE == default ]] || set -- "--$MODE"
fi
```

## 4. Open questions for the decision gate

1. Geeqie vs imv vs other?
2. Mode flag name (`--async-copy-viewer`? `--live`? `--watch`?).
3. TUI: `gum` (preferred), `fzf` (universal), or skip?
4. Show JPG only (fast) vs JPG-then-ARW (slow on RAW decode)?
5. If user closes the viewer mid-session, respawn on next upload or
   leave closed?
6. Pre-warm geeqie at server start (recommended) or lazy-start on first
   upload?

## 5. Sources / verification status

- `play-photography.yml:44` — confirmed `geeqie` is installed by the
  existing photography playbook in this repo.
- Geeqie `--remote` API — long-standing feature (`man geeqie`,
  `geeqie --help-all`).
- Imv socket API — `imv-msg(1)` man page; `XDG_RUNTIME_DIR/imv-<pid>.sock`.
- `gum` / `imv` Fedora package availability — not verifiable from the
  CCY container (Debian-based). Verify on host before Phase 4 with
  `dnf info <pkg>`.
