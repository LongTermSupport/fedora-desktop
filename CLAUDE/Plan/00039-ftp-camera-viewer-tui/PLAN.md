# Plan 00039: `ftp-camera` Viewer Mode + Launcher TUI

**Status**: Not Started (research complete, awaiting decision gate)
**Created**: 2026-05-20
**Owner**: joseph
**Priority**: Medium
**Type**: Feature Extension

## Overview

The Sony A7V supports auto FTP transfer: every photo is sent the moment the
shutter closes, and `ftp-camera --async-copy` already sorts and ships each
file as it lands. This plan adds `--view` / `--view-jpg` modifier flags that
also display each matching arrival in a single geeqie window, giving the user
a live "contact sheet of one" while shooting without a window per frame.

Because the script now has nine modes, the plan also adds a `gum choose`
launcher TUI when `ftp-camera` is invoked with no arguments.

The full original plan, including the dated revision narrative, is kept
verbatim in [PLAN_archive.md](PLAN_archive.md). Durable material lives in:

- [DECISIONS.md](DECISIONS.md): research summary, resolved decision gate,
  Technical Decisions 1-6, risks table.
- [research.md](research.md): viewer and TUI candidate survey.
- [geeqie-refresh-research.md](geeqie-refresh-research.md): root cause and
  fix for the window-not-refreshing bug.

## Goals

- Live single-window preview of each sorted photo, updated in place as new
  uploads arrive, with no window spawn-storm during bursts.
- The viewer composes with the existing async-copy pipeline; rclone-mount
  write-through and remote upload behaviour are unchanged.
- Launcher TUI for argument-free invocation: arrow-key menu listing every
  mode with a one-line description.
- No regression to existing flags; power users skip the TUI by passing a
  flag.

## Non-Goals

- Editing, rating or colour-grading in the viewer. RapidRAW, darktable and
  ART remain the editing tools.
- Tethered shooting (USB, gphoto2). This is FTP-only.
- Network-transparent viewing (X forwarding, VNC).

## Context & Background

- Host is Fedora GNOME on Wayland; XWayland available.
- `ftp-camera` is a single bash script at `files/home/.local/bin/ftp-camera`,
  deployed by `playbooks/imports/optional/common/play-ftp-camera.yml`.
- The viewer hooks into `process_async_upload` after a successful sort; in
  default mode a separate loop tails the vsftpd OK UPLOAD log.
- Geeqie is already installed by `play-photography.yml`; only `gum` (or
  `fzf`) is a new package.
- Key design rules (see DECISIONS.md): `--view` is an orthogonal modifier,
  RAW is the default class, the source folder is always the local FTP tree,
  pre-warm only in async modes, and a startup banner summarises the chosen
  configuration.

## Tasks

### Phase 1: Decision gate

- [ ] ⬜ User reviews this plan and answers the questions above
- [ ] ⬜ Update plan with chosen flag name, viewer, and TUI tool
- [ ] ⬜ Update plan with the answers to the closed-viewer and
  replace-vs-queue policy questions

### Phase 2: Viewer modifier (`--view` / `--view-jpg`)

State variables: `VIEWER_MODE=true|false`, `VIEWER_CLASS=RAW|JPG`. The
flags are mutually exclusive with each other and with non-server modes.

- [ ] ⬜ **Arg parsing**: turn the single-arg `case` into a loop over `$@`
  so `--view` / `--view-jpg` add to the chosen primary mode; reject
  `--view-jpg --view`, `--view --sort`, etc.
- [ ] ⬜ **`display_in_viewer()` helper** next to `copy_one_file_to_mount()`
  (primitive in DECISIONS.md, research summary).
- [ ] ⬜ **Class filter helper**: predicate matching `$VIEWER_CLASS`
  (`arw` for RAW, `jpg`/`jpeg` for JPG).
- [ ] ⬜ **Hook for async modes**: in `process_async_upload`, after
  `sort_one_file` succeeds, call the viewer only if `VIEWER_MODE=true` and
  the class matches.
- [ ] ⬜ **Hook for default mode**: new `viewer_monitor_loop()` tailing the
  vsftpd OK UPLOAD log, alongside the existing `inotifywait` printer.
- [ ] ⬜ **Source folder is the local upload tree**, never the rclone mount;
  call it out in a code comment.
- [ ] ⬜ **Pre-warm (async modes only)**: create today's class folder and open
  geeqie against it before `wait $MONITOR_PID` (Decision 4).
- [ ] ⬜ **Startup probe**: fail fast if `geeqie` is not on `$PATH`.
- [ ] ⬜ **Headless check**: exit with a clear message if both
  `$WAYLAND_DISPLAY` and `$DISPLAY` are unset.
- [ ] ⬜ **Startup confirmation banner** (Decision 6) before the monitor loop.
- [ ] ⬜ Update `--help` and the trailing workflow examples for the modifier
  flags.

### Phase 3: Launcher TUI

Two-step `gum choose` flow; step 2 is skipped for non-server modes.

- [ ] ⬜ Detect "no arguments" and route to the launcher.
- [ ] ⬜ **Step 1, mode picker**: default / async / async-copy / sort / push /
  copy / prune / pass, each with a one-line description.
- [ ] ⬜ **Step 2, viewer picker** (server modes only): "no viewer" first and
  pre-selected via `--selected`, then "view RAW", "view JPG".
- [ ] ⬜ The launcher exec-replaces itself with `$0 <flags>` so the banner
  prints uniformly.
- [ ] ⬜ Add `--no-tui` (or equivalent) escape hatch for scripted callers.
- [ ] ⬜ Fall back to `fzf` if `gum` is missing; if both are missing, note it
  and proceed with default-mode FTP server.

### Phase 4: Playbook integration

- [ ] ⬜ Add the chosen TUI package (`gum` or `fzf`) to a playbook install
  step (`play-ftp-camera.yml`, or a general tools play if shared).
- [ ] ⬜ No new viewer install; the Phase 2 probe checks `command -v geeqie`.
- [ ] ⬜ Update the `play-ftp-camera.yml` installation summary to mention
  the new mode and the launcher.

### Phase 5: QA

- [ ] ⬜ `./scripts/qa-all.bash` clean.
- [ ] ⬜ Manual test plan run on host. Each row of the matrix:
  - [ ] ⬜ **Launcher**: no args, pick each option, verify routing and banner.
  - [ ] ⬜ **`--view` + `--async-copy`**: 5+ upload burst, one geeqie window
    cycles through, only ARW fires.
  - [ ] ⬜ **`--view` + `--async`**: same, without rclone.
  - [ ] ⬜ **`--view` (default mode)**: watches `$UPLOAD_DIR` itself, only
    ARW fires.
  - [ ] ⬜ **`--view-jpg` + each of the three above**: JPG filter instead.
  - [ ] ⬜ **Mutual-exclusion**: `--view --view-jpg` rejected.
  - [ ] ⬜ **Bad combinations**: `--view --sort` / `--view --push` rejected.
  - [ ] ⬜ Close geeqie mid-session, next matching upload respawns it.
  - [ ] ⬜ Ctrl+C `ftp-camera`, geeqie keeps running.
  - [ ] ⬜ **Headless**: `ftp-camera --view` over ssh fails fast.
  - [ ] ⬜ **Pre-warm sanity**: `--async --view` before any uploads opens
    today's empty RAW folder without error.
  - [ ] ⬜ **No pre-warm in default mode**: geeqie starts only on first match.
  - [ ] ⬜ All existing modes still work without `--view`.

## Success Criteria

- [ ] New mode displays each upload in a single window, in real time,
  during a 20-frame burst sequence with no extra windows spawned.
- [ ] Existing modes (`--async-copy`, `--push`, `--copy`, `--prune`,
  `--sort`, `--pass`, default) are unaffected.
- [ ] `ftp-camera` with no arguments opens the launcher TUI; choosing
  any option routes correctly.
- [ ] Headless invocation of the viewer mode fails fast with a clear
  message rather than half-starting.
- [ ] `./scripts/qa-all.bash` passes.
- [ ] Playbook re-run on the host is idempotent.

## Delivery & Milestones

- e6048c51: `--view` / `--view-jpg` modifiers and the launcher TUI landed in
  `files/home/.local/bin/ftp-camera`.
- 669fe51f: geeqie refresh fix (`--file=` under `setsid -f`).
- Plan slimmed; full history in [PLAN_archive.md](PLAN_archive.md), activity
  log in `JOURNAL/`.
