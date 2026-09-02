# Plan 00039 — Decisions, research summary and risks

Supporting document for [PLAN.md](PLAN.md). Extracted verbatim in substance
from the original plan (see [PLAN_archive.md](PLAN_archive.md)); the viewer
and TUI candidate survey is in [research.md](research.md) and the refresh
root-cause analysis in [geeqie-refresh-research.md](geeqie-refresh-research.md).

## Research summary

**Viewer: `geeqie` (primary, already installed by `play-photography.yml`).**

The original research claimed a `geeqie --remote` flag. The host probe
(Fedora 43, geeqie 2.6.1) showed no such flag, but a manual test proved
geeqie 2.x is **implicitly single-instance**: a second `geeqie /path/B.jpg`
invocation loads B into the existing window. Zero new packages, no PID file,
no IPC. Later live testing showed the bare positional form does not reliably
refresh from inside a pipeline; the fixed primitive is:

```sh
display_in_viewer() {
    local file="$1"
    # --file= is the documented remote-control entry point. setsid -f detaches
    # from the inotifywait / tail|awk|while pipeline so the D-Bus handshake
    # with the running primary is not raced. GQ_NEW_INSTANCE scrubbed so an
    # inherited value cannot force G_APPLICATION_NON_UNIQUE.
    GQ_NEW_INSTANCE= setsid -f geeqie --file="$file" >/dev/null 2>&1
}
```

Why geeqie beats the alternatives: already installed, no IPC plumbing,
implicit respawn if the user closes it, implicit arrow-key history from the
directory listing, native ARW via libraw, Wayland-native GTK3, in-app trash.

**Contingency:** `imv` 4.5.0 (`fedora` repo, `imv-msg open <file>` Unix-socket
IPC) at the cost of one package and a small PID-tracking helper. Details in
geeqie-refresh-research.md section 6.

**TUI: `gum choose`** (`gum` 0.16.2 in the `fedora` repo). `fzf` 0.70.0
(`updates` repo) is the fallback. `whiptail` is not in Fedora 43 repos at
all.

## Decision gate (all resolved 2026-05-20)

- **Viewer**: geeqie, via implicit single-instance CLI.
- **Single-image vs add-to-stack**: moot; geeqie shows the current file and
  gives arrow-key history from the directory for free.
- **Viewer closed mid-session**: moot; the next invocation respawns it.
- **`--view` is an orthogonal modifier, not a mode** (Decision 5).
- **Default class is RAW**; `--view-jpg` opts in to JPG and is mutually
  exclusive with `--view` (Decision 2).
- **Source folder is always the local FTP tree**, never the rclone mount.
- **Folder selection depends on sort**: with `--async` / `--async-copy` watch
  `$UPLOAD_DIR/photos/$(date +%Y/%m/%d)/RAW/` (or `JPG/`); without sort
  watch `$UPLOAD_DIR` filtered by extension, no pre-warm (Decision 4).
- **TUI tool**: `gum choose`.
- **Startup confirmation banner** on every invocation (Decision 6).

## Technical decisions

### Decision 1: Where the viewer call hooks in

**Context**: need to know when "the file is ready to view".
**Options**: (1) right after `sort_one_file` succeeds; (2) after the per-file
mount copy succeeds.
**Decision**: Option 1. Remote-upload status is irrelevant to the preview and
the cp-into-mount latency would defeat the point. Copy failure still logs
separately.
**Date**: 2026-05-20

### Decision 2: JPG vs ARW — explicit class filter, RAW default

**Context**: the camera sends both classes in RAW+JPG bursts; firing on both
flickers between formats.
**Options**: (1) fire on every photo, last-write-wins; (2) fire only on the
user-chosen class, RAW by default, JPG via a separate flag.
**Decision**: Option 2. `--view` watches ARW, `--view-jpg` watches JPG,
mutually exclusive. `VIEWER_CLASS=RAW|JPG` is set once at startup and every
event in `process_async_upload` and the default-mode viewer loop is filtered
against it.
**Date**: 2026-05-20 (user confirmed in revision)

### Decision 3: Viewer process lifecycle

**Context**: should `ftp-camera` own and kill the viewer on exit?
**Options**: (1) spawn and tear down on Ctrl+C; (2) viewer is the user's
process, never killed by the script.
**Decision**: Option 2. The post-session review must not lose its window.
**Date**: 2026-05-20 (user confirmed)

### Decision 4: Pre-warm strategy

**Context**: geeqie pays a 1-2 s GTK startup; pre-warming in the wrong place
surfaces old or mixed-class files.
**Options**: (1) never pre-warm; (2) always pre-warm against the upload root;
(3) conditional: pre-warm against today's class subfolder in async modes only.
**Decision**: Option 3.

```sh
TODAY_DIR="$UPLOAD_DIR/photos/$(date +%Y/%m/%d)/$VIEWER_CLASS"
sudo mkdir -p "$TODAY_DIR"
geeqie "$TODAY_DIR" >/dev/null 2>&1 &
```

Default mode cold-starts geeqie on the first matching arrival because the
upload root is not guaranteed clean. Midnight rollover leaves the pre-warm
folder stale; accepted, since explicit per-file calls still display and the
user can restart the script.
**Date**: 2026-05-20 (user proposed; refined in revision)

### Decision 5: `--view` as orthogonal modifier

**Context**: the first design made `--view` a combined mode equal to
`--async-copy` plus viewer. The user wanted it combinable with any server
mode.
**Options**: (1) self-contained mode with extra flag names per combination;
(2) modifier flag added to whatever server mode was chosen.
**Decision**: Option 2. The arg parser becomes a `while` loop over `$@`;
modes set `MODE=...`, modifiers set `VIEWER_MODE=true`; a rejection list
catches `--view` with non-server modes and `--view --view-jpg`. Benefits:
`ftp-camera --view` alone is useful, and `--async-copy --view` composes
without surprise.
**Date**: 2026-05-20 (user revision)

### Decision 6: Startup confirmation banner

**Context**: orthogonal modifiers give twelve distinct configurations; the
user must see which one is running.
**Options**: (1) only the existing "FTP server ready" line; (2) a structured
summary block before the monitor loop.
**Decision**: Option 2. Sketch, styled with the script's existing palette:

```
══════════════════════════════════════════════════════════
  ftp-camera ready
──────────────────────────────────────────────────────────
  Mode:         async-copy (sort + per-file rclone copy)
  Viewer:       on   (filter: ARW)
  Watching:     /srv/ftp-camera/photos/2026/05/20/RAW/
  Pre-warm:     yes (geeqie opened against the watch folder)
  Rclone:       LTS-G-Drive:/PHOTO/LIBRARY
  Mount:        ~/mnt/lts-photo/PHOTO/LIBRARY
  FTP URL:      ftp://camera@&lt;lan-ip&gt;/
══════════════════════════════════════════════════════════
```

Rows depend on active flags: `Rclone` and `Mount` are omitted without a
remote; `Watching` reports the upload root in default mode. Same banner
whether launched from the TUI or by flag.
**Date**: 2026-05-20 (user revision)

## Risks and mitigations

| Risk                                                                           | Impact | Probability | Mitigation                                                                                         |
| ------------------------------------------------------------------------------ | ------ | ----------- | -------------------------------------------------------------------------------------------------- |
| Geeqie startup latency stalls the first frame of a burst                       | Med    | Med         | Pre-warm in async modes (Decision 4). Fallback: switch to `imv`.                                   |
| `gum` not in Fedora 43 repos                                                   | Low    | Low         | Verified present (0.16.2). Fall back to `fzf`.                                                     |
| Viewer mode runs over SSH without a display and burns silently                 | Med    | Low         | Fail-fast headless probe at startup (Phase 2).                                                     |
| User closes geeqie mid-session and is confused when the next photo respawns it | Low    | Med         | One-line notice in the script output the first time geeqie is respawned in a session.              |
| RAW decode path triggers libraw bugs on unusual A7V files                      | Med    | Low         | Class filter defaults to RAW by user choice; `--view-jpg` avoids the libraw path if it misbehaves. |
| Geeqie window does not refresh when called from inside a pipeline              | High   | Seen        | `--file=` plus `setsid -f` plus `GQ_NEW_INSTANCE` scrub (geeqie-refresh-research.md section 5).    |
