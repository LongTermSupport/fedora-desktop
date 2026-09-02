# Plan 00039: `ftp-camera` Viewer Mode + Launcher TUI

**Status**: Dormant (blocked on Phase 5 manual test matrix and the idempotent playbook re-run, both HOST-only; all code shipped)
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

- [x] ✅ User reviews this plan and answers the questions above (gate
  resolved 2026-05-20, DECISIONS.md "Decision gate")
- [x] ✅ Update plan with chosen flag name, viewer, and TUI tool (`--view` /
  `--view-jpg`, geeqie, `gum choose`; DECISIONS.md)
- [x] ✅ Update plan with the answers to the closed-viewer and
  replace-vs-queue policy questions (both moot under geeqie single-instance;
  DECISIONS.md "Decision gate")

### Phase 2: Viewer modifier (`--view` / `--view-jpg`)

State variables: `VIEWER_MODE=true|false`, `VIEWER_CLASS=RAW|JPG`. The
flags are mutually exclusive with each other and with non-server modes.

All shipped in e6048c51; line references are to
`files/home/.local/bin/ftp-camera` at d1f0529.

- [x] ✅ **Arg parsing**: turn the single-arg `case` into a loop over `$@`
  so `--view` / `--view-jpg` add to the chosen primary mode; reject
  `--view-jpg --view`, `--view --sort`, etc. (e6048c51; `while` loop at
  line 339, mutual exclusion at 366-380, non-server rejection at 423-428)
- [x] ✅ **`display_in_viewer()` helper** next to `copy_one_file_to_mount()`
  (primitive in DECISIONS.md, research summary). (e6048c51, refresh fix
  669fe51f; line 1571)
- [x] ✅ **Class filter helper**: predicate matching `$VIEWER_CLASS`
  (`arw` for RAW, `jpg`/`jpeg` for JPG). (`viewer_class_matches`, line 1535)
- [x] ✅ **Hook for async modes**: in `process_async_upload`, after
  `sort_one_file` succeeds, call the viewer only if `VIEWER_MODE=true` and
  the class matches. (line 1928)
- [x] ✅ **Hook for default mode**: new `viewer_monitor_loop()` tailing the
  vsftpd OK UPLOAD log, alongside the existing `inotifywait` printer.
  (line 1589, started at 2480-2482)
- [x] ✅ **Source folder is the local upload tree**, never the rclone mount;
  call it out in a code comment. (`--help` text line 148 and hook comments)
- [x] ✅ **Pre-warm (async modes only)**: create today's class folder and open
  geeqie against it before `wait $MONITOR_PID` (Decision 4). (line 2431-2434)
- [x] ✅ **Startup probe**: fail fast if `geeqie` is not on `$PATH`.
  (line 2292-2295)
- [x] ✅ **Headless check**: exit with a clear message if both
  `$WAYLAND_DISPLAY` and `$DISPLAY` are unset. (line 2297-2301)
- [x] ✅ **Startup confirmation banner** (Decision 6) before the monitor loop.
  (`print_startup_banner`, line 1775, called at 2419)
- [x] ✅ Update `--help` and the trailing workflow examples for the modifier
  flags. (lines 111-116, 138-148, 202-203)

### Phase 3: Launcher TUI

Two-step `gum choose` flow; step 2 is skipped for non-server modes.

Shipped in e6048c51, refined in d81d34c (self-discoverable two-step flow)
and 0100f38 (no re-fire under the systemd-inhibit re-exec). A third
hotspot step was added later by 5a1d7bc, outside this plan.

- [x] ✅ Detect "no arguments" and route to the launcher. (line 462-475)
- [x] ✅ **Step 1, mode picker**: default / async / async-copy / sort / push /
  copy / prune / pass, each with a one-line description. (line 225-260)
- [x] ✅ **Step 2, viewer picker** (server modes only): "no viewer" first and
  pre-selected via `--selected`, then "view RAW", "view JPG". (line 264-296)
- [x] ✅ The launcher exec-replaces itself with `$0 <flags>` so the banner
  prints uniformly. (`run_launcher_tui`, exec at the end of the function)
- [x] ✅ Add `--no-tui` (or equivalent) escape hatch for scripted callers.
  (line 388-391; also used as the launcher's own silent opt-out, 327-332)
- [x] ✅ Fall back to `fzf` if `gum` is missing; if both are missing, note it
  and proceed with default-mode FTP server. (line 467-475)

### Phase 4: Playbook integration

- [x] ✅ Add the chosen TUI package (`gum` or `fzf`) to a playbook install
  step (`play-ftp-camera.yml`, or a general tools play if shared).
  (e6048c51; `play-ftp-camera.yml` line 99-104 installs `gum`)
- [x] ✅ No new viewer install; the Phase 2 probe checks `command -v geeqie`.
  (669fe51f confirms no playbook change; probe at script line 2292)
- [x] ✅ Update the `play-ftp-camera.yml` installation summary to mention
  the new mode and the launcher. (`play-ftp-camera.yml` line 446-470)

### Phase 5: QA

- [x] ✅ `./scripts/qa-all.bash` clean. (re-run 2026-09-02 at d1f0529: QA
  passed, 646 files checked)
- [ ] 🔄 Manual test plan run on host. Each row of the matrix:
  - [ ] ⬜ **Launcher**: no args, pick each option, verify routing and banner.
  - [ ] 🔄 **`--view` + `--async-copy`**: 5+ upload burst, one geeqie window
    cycles through, only ARW fires. (a 3-frame ARW burst over the hotspot
    ran on 2026-05-20 and exposed the refresh bug fixed by 669fe51f; no
    post-fix burst run is recorded)
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

- [ ] ⬜ New mode displays each upload in a single window, in real time,
  during a 20-frame burst sequence with no extra windows spawned. (HOST
  test; only the pre-fix 3-frame run is on record)
- [ ] ⬜ Existing modes (`--async-copy`, `--push`, `--copy`, `--prune`,
  `--sort`, `--pass`, default) are unaffected. (HOST test; the modes have
  been used and patched since, e.g. 7986dfa, but no explicit regression
  pass is recorded)
- [x] ✅ `ftp-camera` with no arguments opens the launcher TUI; choosing
  any option routes correctly. (e6048c51, d81d34c, 0100f38; the re-fire
  fix in 0100f38 came out of live use of the launcher)
- [x] ✅ Headless invocation of the viewer mode fails fast with a clear
  message rather than half-starting. (probe runs before vsftpd starts,
  script line 2288-2302)
- [x] ✅ `./scripts/qa-all.bash` passes. (2026-09-02 at d1f0529)
- [ ] ⬜ Playbook re-run on the host is idempotent. (HOST action)

## Delivery & Milestones

- e6048c51: `--view` / `--view-jpg` modifiers and the launcher TUI landed in
  `files/home/.local/bin/ftp-camera`.
- d81d34c, 0100f38: launcher made self-discoverable; no double-fire under
  the systemd-inhibit re-exec.
- 669fe51f: geeqie refresh fix (`--file=` under `setsid -f`).
- Plan slimmed; full history in [PLAN_archive.md](PLAN_archive.md), activity
  log in `JOURNAL/`.
- Remaining: the Phase 5 host test matrix and an idempotent playbook re-run,
  both HOST-only. Closing them moves the plan to Complete.
