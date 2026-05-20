# Plan 00039: `ftp-camera` Viewer Mode + Launcher TUI

**Status**: Not Started (research complete, awaiting decision gate)
**Created**: 2026-05-20
**Owner**: joseph
**Priority**: Medium
**Type**: Feature Extension

## Overview

The Sony A7V supports "auto FTP transfer" — every photo is sent the moment
the shutter closes. The existing `ftp-camera --async-copy` mode already
sorts and ships each file the instant it lands. This plan adds a sibling
mode (`--async-copy-viewer`, working name) that **also displays each
sorted photo in a single image viewer window** so the user gets a live
"contact sheet of one" while shooting — without the viewer multiplying
into a window per frame.

Concurrently, the script's CLI surface has grown to nine modes
(`default | --sort | --pass | --push | --copy | --prune | --async | --async-copy` plus this new mode). The plan therefore also adds an
interactive launcher TUI when `ftp-camera` is invoked with no arguments,
so the user picks the mode from a menu rather than memorising flags.

Research is in [research.md](research.md). It looked at imv, feh,
nsxiv, geeqie, qview, swayimg, gthumb, eog/loupe, oculante, and vimiv-qt
for the viewer, and `gum choose`, `fzf`, `whiptail`, and Python `textual`
for the TUI. The headline finding: **geeqie's `--remote` flag is built
for exactly this use case, and geeqie is already deployed by
`play-photography.yml`** — no new viewer dependency required.

## Goals

- Live single-window preview of each sorted photo, updated in place as new
  uploads arrive — no window spawn-storm during burst sequences.
- The new mode is composable with the existing async-copy pipeline:
  rclone-mount write-through and remote upload behaviour are unchanged.
- Launcher TUI for argument-free `ftp-camera` invocation: arrow-key menu
  listing every mode with a one-line description.
- No regression to existing flags — every current invocation form keeps
  working unchanged. Power users skip the TUI by passing a flag.

## Non-Goals

- Editing/rating/colour-grading in the viewer. The viewer is just a
  preview surface; RapidRAW / darktable / ART remain the editing tools.
- Tethered-shooting workflows (USB cable, gphoto2). This is FTP-only.
- Viewing RAW files directly when both JPG and ARW are uploaded — the
  camera sends both, the JPG is the live-preview source, the ARW lives in
  the sort tree for later editing. Avoids slow libraw decode on the hot
  path.
- Network-transparent viewer (X forwarding, VNC). The user views on the
  same machine the FTP server runs on.

## Context & Background

- The host is Fedora 43 GNOME 48 on Wayland. XWayland is available.
- `ftp-camera` is a single bash script at
  `files/home/.local/bin/ftp-camera`, deployed by
  `playbooks/imports/optional/common/play-ftp-camera.yml`.
- The async pipeline already classifies each upload, sorts by EXIF date,
  and (in `--async-copy`) writes through the local rclone mount. The
  new mode hooks into the same `process_async_upload` codepath after a
  successful sort — the viewer is the only added side-effect.
- The existing modes the launcher needs to expose:
  | Flag           | Purpose                                                                 |
  | -------------- | ----------------------------------------------------------------------- |
  | (none)         | Start FTP server, sort at the end                                       |
  | `--sort`       | Sort already-uploaded files and exit                                    |
  | `--pass`       | Print FTP password and exit                                             |
  | `--push`       | Direct synchronous rclone push                                          |
  | `--copy`       | Copy via mount + verify                                                 |
  | `--prune`      | Delete orphan RAW files                                                 |
  | `--async`      | FTP server + sort-on-arrival                                            |
  | `--async-copy` | FTP server + sort + per-file mount copy (background upload)             |
  | **new**        | `--async-copy-viewer` (working name): all of `--async-copy` + live view |

## Research Summary (full detail in research.md)

> **NOTE (2026-05-20, second revision)**: the geeqie pick is back, but
> via a different mechanism than originally claimed. The host probe
> confirmed two things in sequence:
>
> 1. Geeqie 2.6.1 has no `--remote` flag (original research was wrong).
> 2. Geeqie 2.6.1 is nevertheless **implicitly single-instance** — a
>    second invocation `geeqie /path/B.jpg` while an instance is
>    already running loads B into the existing window with no flag
>    needed. Confirmed by manual test on the host.
>
> So we still get the zero-new-install outcome the original plan
> targeted, just with a plain `geeqie "$file" &` call rather than
> `geeqie --remote file:…`. `imv` falls back to a contingency.

**Viewer recommendation: `geeqie` (primary, already installed)**

Implementation primitive — three lines of bash:

```sh
display_in_viewer() {
    local file="$1"
    # Geeqie 2.x is implicitly single-instance: a second invocation
    # routes the file path to the running window. If no instance is
    # running, this starts one. No --remote, no IPC plumbing, no PID
    # file. Backgrounded so the second-invocation handshake doesn't
    # block the FTP server's monitor loop.
    geeqie "$file" >/dev/null 2>&1 &
}
```

Why this beats the alternatives:

- **Already installed** via `play-photography.yml:44`. Zero new
  packages.
- **No PID tracking, no socket, no IPC** — the OS-level
  single-instance handshake is geeqie's own concern.
- **Implicit respawn**: if the user closes geeqie mid-session, the
  next invocation just starts a fresh instance with the new file. The
  "closed mid-session" decision-gate question is answered automatically.
- **Implicit history**: geeqie auto-populates its image list from the
  directory of the file you opened, so arrow-keys flick through the
  session. The "replace vs queue" decision-gate question is also
  answered automatically.
- **Native ARW** via libraw if we ever want it.
- **Wayland-native** (GTK3).
- **In-app trash** on Ctrl+D.

Contingency (kept for the record, not on the active path): if
geeqie's startup latency ever becomes a problem during high-cadence
bursts, switch to `imv` 4.5.0 (`fedora` repo, `imv-msg open <file>`
IPC) at the cost of one new package and a small PID-tracking helper.

**TUI recommendation: `gum choose` (CONFIRMED available)**

`gum` 0.16.2 is in the `fedora` repo. One-line bash integration:

```sh
MODE=$(gum choose default async async-copy async-copy-viewer \
                   sort push copy prune pass)
```

`fzf` 0.70.0 (`updates` repo) is the universal fallback. `whiptail`
was dropped — not in Fedora 43 repos at all (`No matching packages to list`).

## Decision Gate

Resolved:

- [x] ✅ **Viewer choice**: `geeqie`. Host probe confirmed implicit
  single-instance via plain CLI (no `--remote` flag needed).
- [x] ✅ **Single-image vs add-to-stack**: irrelevant — geeqie auto-
  populates its list from the directory, so the user gets both
  behaviours (current file shown + arrow-key history) for free.
- [x] ✅ **Behaviour when viewer is closed mid-session**: irrelevant —
  the next `geeqie "$file" &` invocation transparently respawns.

All resolved (2026-05-20):

- [x] ✅ **`--view` is an ORTHOGONAL MODIFIER, not a mode.** It
  composes with whatever primary mode the user picked — default FTP
  server, `--async`, `--async-copy`. The user picks the FTP
  behaviour separately and adds `--view` to get the live preview.
- [x] ✅ **Default class is RAW.** Viewer fires only on ARW
  arrivals. JPG arrivals don't trigger geeqie. Opt-in to JPG via the
  separate `--view-jpg` flag (mutually exclusive with `--view`).
- [x] ✅ **Source folder is always the local FTP tree**, never the
  rclone mount destination. The user is reviewing what the camera
  just sent, not what's been shipped out.
- [x] ✅ **Folder selection depends on whether sorting is active**:
  - With `--async` or `--async-copy` → watch (and pre-warm against)
    `$UPLOAD_DIR/photos/$(date +%Y/%m/%d)/RAW/` (or `JPG/` for
    `--view-jpg`).
  - Without sort (default FTP server) → watch the upload root
    `$UPLOAD_DIR`, filter on extension. No pre-warm — geeqie
    cold-starts on the first matching arrival because the root may
    contain old/mixed files we don't want to surface.
- [x] ✅ **TUI tool**: `gum choose`.
- [x] ✅ **Startup confirmation banner**: every invocation prints a
  clear, human-friendly summary of the chosen behaviour (mode,
  watched folder, class filter, pre-warm status, geeqie behaviour) so
  the user can see exactly what's happening without reading the
  script. Same format whether launched from the TUI or directly via
  flag.

## Tasks

### Phase 1: Decision gate

- [ ] ⬜ User reviews this plan and answers the questions above
- [ ] ⬜ Update plan with chosen flag name, viewer, and TUI tool
- [ ] ⬜ Update plan with the answers to the closed-viewer and
  replace-vs-queue policy questions

### Phase 2: Viewer modifier (`--view` / `--view-jpg`)

`--view` and `--view-jpg` are orthogonal modifier flags. They can be
combined with any primary mode that runs the FTP server (default,
`--async`, `--async-copy`). They are mutually exclusive with each
other and with non-server modes (`--sort`, `--pass`, `--push`,
`--copy`, `--prune`) — those modes exit immediately and have nothing
to view.

State variables introduced:

- `VIEWER_MODE=true|false` — viewer is active
- `VIEWER_CLASS=RAW|JPG` — extension to filter on (RAW default, JPG
  when `--view-jpg`)

Tasks:

- [ ] ⬜ **Arg parsing**: extend the top-of-script `case` so that
  `--view` and `--view-jpg` are recognised AS ADDITIONS to whatever
  primary mode the user picked. The existing `case` only handles one
  arg; this becomes a loop over `$@`. Set `VIEWER_MODE=true` and
  `VIEWER_CLASS=RAW` (or `JPG`). Validate: reject `--view-jpg --view`, reject `--view --sort`, etc.
- [ ] ⬜ **`display_in_viewer()` helper** next to
  `copy_one_file_to_mount()`:
  ```bash
  display_in_viewer() {
      local file="$1"
      # Geeqie 2.x is implicitly single-instance via plain CLI:
      # second invocation routes the file path into the running
      # window. Backgrounded so the handshake doesn't block the
      # monitor loop.
      geeqie "$file" > /dev/null 2>&1 &
  }
  ```
- [ ] ⬜ **Class filter helper**: small predicate that returns 0 if
  the file extension matches `$VIEWER_CLASS` (`arw` for RAW, `jpg`
  or `jpeg` for JPG), 1 otherwise.
- [ ] ⬜ **Hook for async modes**: inside `process_async_upload`,
  after `sort_one_file` returns success (case `0`), call
  `display_in_viewer "$sorted_path"` ONLY IF `VIEWER_MODE=true` AND
  the file matches `$VIEWER_CLASS`. JPG arrivals do NOT fire the
  viewer when `VIEWER_CLASS=RAW`, and vice versa.
- [ ] ⬜ **Hook for default mode** (no sort): add a new
  `viewer_monitor_loop()` that tails the vsftpd OK UPLOAD log
  (same source `async_monitor_loop` uses, so partial transfers
  never fire the viewer). For each completed upload whose extension
  matches `$VIEWER_CLASS`, call `display_in_viewer "$UPLOAD_DIR/$fname"`. Run alongside (not instead of) the existing
  default-mode `inotifywait` pretty-printer.
- [ ] ⬜ **Source folder is the local upload tree**, never the rclone
  mount. The script's existing path discipline already guarantees
  this — just call it out in a code comment so future edits don't
  introduce a regression.
- [ ] ⬜ **Pre-warm (async modes only)**: at the bottom of startup
  (after the FTP banner, before `wait $MONITOR_PID`), if
  `VIEWER_MODE=true` AND a sort mode is active:
  ```bash
  TODAY_DIR="$UPLOAD_DIR/photos/$(date +%Y/%m/%d)/$VIEWER_CLASS"
  sudo mkdir -p "$TODAY_DIR"
  geeqie "$TODAY_DIR" > /dev/null 2>&1 &
  ```
  No pre-warm in default mode: the upload root may contain old or
  mixed-class files we don't want to surface; geeqie cold-starts on
  the first matching arrival instead.
- [ ] ⬜ **Startup probe**: fail fast if `geeqie` isn't on `$PATH`.
  Same pattern as `load_rclone_config()`.
- [ ] ⬜ **Headless check**: if `VIEWER_MODE=true` and BOTH
  `$WAYLAND_DISPLAY` and `$DISPLAY` are unset, exit with a clear
  message telling the user to drop `--view`/`--view-jpg`. Better to
  refuse than start vsftpd and then silently never show a window.
- [ ] ⬜ **Startup confirmation banner** (see Decision 6 below): emit
  a clear, human-friendly summary BEFORE entering the monitor loop.
  Same format whether the user came in via TUI or direct flag.
- [ ] ⬜ Update the script's `--help` output and the trailing workflow
  examples to document `--view` and `--view-jpg` as modifier flags
  that compose with the primary modes.

### Phase 3: Launcher TUI

The TUI is a two-step `gum choose` flow because mode and viewer are
orthogonal. Step 2 is skipped for non-server modes (sort/push/copy/
prune/pass/pass-only) which have nothing to view.

- [ ] ⬜ Detect "no arguments" at the top of arg parsing and route to
  the launcher.
- [ ] ⬜ **Step 1 — mode picker**: a single `gum choose` listing:
  ```
  default      Start FTP server, sort at end on Ctrl+C
  async        Sort each upload as it arrives
  async-copy   Sort + per-file rclone copy as uploads arrive
  sort         Sort already-uploaded files and exit
  push         Direct rclone push of the sort tree and exit
  copy         Mount-write-through copy with verify and exit
  prune        Delete orphan RAW files (interactive) and exit
  pass         Print FTP password and exit
  ```
- [ ] ⬜ **Step 2 — viewer picker** (only when Step 1 chose default /
  async / async-copy): second `gum choose` with three entries, **"no
  viewer" first AND set as the default-highlighted item via `gum choose --selected="no viewer"`** — the viewer is the rare case, not
  the norm:
  ```
  no viewer        Just receive uploads, no preview window   ← default
  view RAW         Show each ARW arrival in a single geeqie window
  view JPG         Show each JPG arrival in a single geeqie window
  ```
  Pressing Enter immediately at this step gives "no viewer" — the
  expected outcome for the typical workflow.
- [ ] ⬜ The launcher exec-replaces itself with `$0 <flags>` so the
  rest of the script proceeds normally and the confirmation banner
  prints uniformly. E.g. user picks `async-copy` + `view RAW` → exec
  `$0 --async-copy --view`.
- [ ] ⬜ Add `--no-tui` (or equivalent) as an escape hatch: behave
  like default-mode FTP server with no menu, for scripted/automated
  callers.
- [ ] ⬜ If `gum` is missing on the host, fall back to `fzf`. If
  both are missing, print a clear note and proceed with default-mode
  FTP server (the lowest-friction safe option).

### Phase 4: Playbook integration

- [ ] ⬜ Add the chosen TUI package (`gum` or `fzf`) to a playbook
  install step. `gum` likely belongs in `play-ftp-camera.yml` itself if
  it's only used here, or in a more general place (`play-bash-tools.yml`
  or similar) if other future tools want it. Decide during Phase 1.
- [ ] ⬜ No new viewer install needed — geeqie is already deployed by
  `play-photography.yml:44`. Phase 2's startup probe just needs to
  verify `command -v geeqie` and fail fast otherwise.
- [ ] ⬜ Update `play-ftp-camera.yml`'s "Display installation summary"
  debug message to mention the new mode and the launcher behaviour.

### Phase 5: QA

- [ ] ⬜ `./scripts/qa-all.bash` clean.
- [ ] ⬜ Manual test plan run on host. Each row of the matrix:
  - [ ] ⬜ **Launcher**: `ftp-camera` no args → TUI → pick each option →
    verify it routes correctly; verify the startup banner correctly
    summarises the chosen behaviour.
  - [ ] ⬜ **`--view` + `--async-copy`**: 5+ uploads burst → single
    geeqie window cycles through them, NO new windows spawn. Only ARW
    files trigger geeqie; JPG arrivals are silent in the viewer.
  - [ ] ⬜ **`--view` + `--async`**: same, without rclone in the path.
  - [ ] ⬜ **`--view` (default mode, no sort)**: upload root accumulates
    files; only ARW arrivals fire geeqie; the watched folder is
    `$UPLOAD_DIR` itself, not a sorted subtree.
  - [ ] ⬜ **`--view-jpg` + each of the three above**: identical
    behaviour but filtering on JPG instead of RAW.
  - [ ] ⬜ **Mutual-exclusion**: `--view --view-jpg` rejected with
    clear error.
  - [ ] ⬜ **Bad combinations**: `--view --sort` / `--view --push` etc.
    rejected — those modes exit immediately and have nothing to view.
  - [ ] ⬜ Close geeqie mid-session → next matching upload respawns it
    transparently.
  - [ ] ⬜ Kill `ftp-camera` (Ctrl+C) → geeqie keeps running as a normal
    user-launched window (not torn down).
  - [ ] ⬜ **Headless**: `ssh` without DISPLAY/WAYLAND, run `ftp-camera --view` → fail-fast error.
  - [ ] ⬜ **Pre-warm sanity** (async modes only): `ftp-camera --async --view` before any uploads today → geeqie window opens against
    today's RAW folder (empty), no error.
  - [ ] ⬜ **No pre-warm in default mode**: `ftp-camera --view` (no
    async) → geeqie does NOT start until the first matching upload
    arrives.
  - [ ] ⬜ Verify `--async`, `--async-copy`, `--push`, `--copy`,
    `--prune`, `--sort`, `--pass` STILL WORK without `--view` —
    nothing about the existing surface changed.

## Technical Decisions

### Decision 1: Where the viewer call hooks in

**Context**: We need to know when "the file is ready to view".
**Options**:

1. Right after `sort_one_file` returns success — earliest possible, file
   is on local disk in the sorted layout.
2. After the per-file mount copy succeeds — guarantees the file has also
   been queued for remote upload before we show it.
   **Decision**: Option 1. The viewer should fire as soon as the file is
   local; remote-upload status is irrelevant to the on-screen preview, and
   delaying the display by the cp-into-mount latency defeats the point.
   Copy failure logs separately as it already does today.
   **Date**: 2026-05-20 (proposed, subject to Phase 1 review)

### Decision 2: JPG vs ARW — explicit class filter, RAW default

**Context**: The camera sends both for RAW+JPG bursts. The viewer
should fire on exactly one class so there's no flicker between the
two formats arriving in quick succession.
**Options**:

1. Fire on every photo, last-write-wins.
2. Always fire on the user-chosen class only; default RAW, opt-in to
   JPG via a separate flag.

**Decision**: Option 2 (user override of the earlier Option 3 pick —
see Notes & Updates → 2026-05-20 third revision). `--view` watches
ARW; `--view-jpg` watches JPG. The two are mutually exclusive. The
class is set once at startup via `VIEWER_CLASS=RAW|JPG` and the hook
inside `process_async_upload` (and the default-mode viewer monitor
loop) filters every event against it. No flicker, no surprises.
**Date**: 2026-05-20 (user confirmed in revision)

### Decision 3: Viewer process lifecycle

**Context**: Should `ftp-camera` own the viewer process and kill it on
exit, or let it survive?
**Options**:

1. `ftp-camera` spawns the viewer and tears it down on Ctrl+C.
2. The viewer is the user's process; `ftp-camera` only talks to it via
   plain `geeqie "$file"` calls and never kills it.

**Decision**: Option 2. The session-summary review the user does after
Ctrl+C should not get its window yanked away. Geeqie keeps running,
the user closes it when they're done.
**Date**: 2026-05-20 (user confirmed)

### Decision 4: Pre-warm strategy

**Context**: Geeqie is a GTK app and pays a ~1-2 second startup cost.
On a high-cadence burst the first frame would otherwise wait on GTK
initialisation. But pre-warming in the wrong place can surface old or
mixed-class files we don't want the user to see.
**Options**:

1. No pre-warm in any mode — first arriving photo starts geeqie cold.
2. Always pre-warm against the upload root.
3. Conditional pre-warm: in async modes, precreate today's class
   subfolder and open geeqie against it. In default mode, no pre-warm
   (root may contain old files).

**Decision**: Option 3. In `--async` / `--async-copy` modes:

```sh
TODAY_DIR="$UPLOAD_DIR/photos/$(date +%Y/%m/%d)/$VIEWER_CLASS"
sudo mkdir -p "$TODAY_DIR"
geeqie "$TODAY_DIR" >/dev/null 2>&1 &
```

In default mode (no sort): no pre-warm. Geeqie cold-starts on the
first matching arrival.

Why conditional: the sorted-tree subfolder is guaranteed empty (or
contains only today's already-arrived photos of the chosen class) so
it's safe to open geeqie against. The upload root in default mode is
NOT guaranteed empty — old or mixed-class files would pollute the
user's view.

Edge case: if the script runs across a midnight boundary, the
pre-warm folder is stale by the time the date rolls. Acceptable —
geeqie will still display new files when called explicitly with
their post-rollover path, and the user can restart the script to
re-pre-warm against the new day.
**Date**: 2026-05-20 (user proposed; refined in revision)

### Decision 5: `--view` as orthogonal modifier

**Context**: Initial design treated `--view` as a single combined
mode (`--async-copy` + viewer). User pushed back: the viewer should
be combinable with any FTP-server mode, not locked to one.
**Options**:

1. `--view` is a self-contained mode that implies `--async-copy`
   - viewer. Other combinations require separate flag names
     (`--async-view`, `--view-only`, etc.).
2. `--view` is an orthogonal modifier flag that adds the viewer
   behaviour to whatever FTP-server mode the user chose
   (default / `--async` / `--async-copy`).

**Decision**: Option 2. The arg parser becomes a `while` loop over
`$@` instead of a single-arg `case`. Modes set `MODE=...`, modifiers
set `VIEWER_MODE=true`, etc. Rejection list at the end catches
incompatible pairs (`--view` with non-server modes like `--push`,
`--sort`, `--prune`; `--view --view-jpg`).

Two clear benefits:

- `ftp-camera --view` alone is a useful tool: plain FTP server with
  preview-on-arrival.
- `ftp-camera --async-copy --view` composes naturally; nothing
  surprising about how the flags combine.

**Date**: 2026-05-20 (user revision)

### Decision 6: Startup confirmation banner

**Context**: With orthogonal modifiers, mode combinations multiply
(2 × server-modes × 2 view-classes × {viewer on/off} = 12 distinct
configurations). The user must be able to tell at a glance which
combination is actually running.
**Options**:

1. Print only the existing FTP-banner ("FTP server ready").
2. Print a structured summary block before entering the monitor loop
   that lists every salient state variable.

**Decision**: Option 2. Format (sketch — colour/styling matches the
script's existing `BOLD/GREEN/CYAN/YELLOW` palette):

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

Fields shown depend on which flags are active — e.g. `Rclone` and
`Mount` rows are omitted when no rclone remote is configured;
`Watching` says `(monitoring upload root; only ARW arrivals fire geeqie)` in default mode. Same banner whether the script was
launched via TUI or direct flag.

**Date**: 2026-05-20 (user revision)

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

## Risks & Mitigations

| Risk                                                                           | Impact | Probability | Mitigation                                                                                                                                                   |
| ------------------------------------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Geeqie startup latency stalls the first frame of a burst                       | Med    | Med         | Pre-warm: launch geeqie once at FTP server startup with an empty/placeholder file; subsequent `--remote` calls are instantaneous. Fallback: switch to `imv`. |
| `gum` not in Fedora 43 repos                                                   | Low    | Low         | Verify with `dnf info gum` before committing the playbook change. Fall back to `fzf` (universal).                                                            |
| Viewer mode runs over SSH without a display and burns silently                 | Med    | Low         | Fail-fast probe at startup (Phase 2).                                                                                                                        |
| User closes geeqie mid-session and is confused when the next photo respawns it | Low    | Med         | Print a one-line notice in the script's output stream the first time geeqie is respawned in a session.                                                       |
| RAW decode path triggers libraw bugs on unusual A7V files                      | Med    | Low         | Avoided by Decision 2 (JPG-only preview).                                                                                                                    |

## Notes & Updates

### 2026-05-20

- Plan created. Research (see research.md) completed via web-knowledge
  agent; geeqie identified as the obvious primary because it's already
  deployed for the photography workflow and `--remote` matches the
  single-instance requirement exactly.
- Confirmed in repo: `playbooks/imports/optional/common/play-photography.yml`
  installs geeqie (line 44, line 298 in summary). No new viewer
  dependency required if we pick geeqie.
- `gum` / `imv` Fedora-package availability was not verifiable from the
  CCY container (Debian) — flagged for host verification in Phase 1.
- Awaiting decision-gate answers from the user.

### 2026-05-20 (revision after host probe)

Ran `untracked/command.bash` on the Fedora 43 / GNOME 49.6 / Wayland
host. Captured at `untracked/command.bash.output`. Material findings:

- **Geeqie 2.6.1 has no `--remote` flag.** The Fedora-shipped help
  output lists only navigation control flags: `--first --last --next --quit --raise --slideshow*` and `--action=<ACTION>`. No `file:`
  URL syntax, no documented "open this file in the running instance"
  primitive. The original recommendation was based on stale research
  about 1.x; 2.x removed or never had this CLI surface. **The single
  load-bearing assumption in the original plan turned out to be
  false.**
- Geeqie may still single-instance implicitly on a second invocation
  (`geeqie file.jpg` while another instance is running) — but this is
  unverified. A short manual test is now in the Decision Gate to
  settle it definitively before committing to a path.
- `imv` 4.5.0 IS in the `fedora` repo (verified). `imv-msg open <file>`
  IPC is the cleanest replacement primitive. Promoted to **primary
  viewer recommendation**.
- `vimiv-qt` 0.9.0 also in `updates` repo with `--command "open …"`
  IPC — held as alternative.
- `gum` 0.16.2 confirmed in `fedora` repo. TUI pick stands.
- `whiptail` is NOT in Fedora 43 repos at all (`No matching packages to list`). Removed from consideration.
- `fzf` 0.70.0 in `updates` repo — universal fallback remains valid.
- `swayimg` 4.7 in `updates` — possible alternative if `imv` ever
  proves problematic on Wayland.
- Other findings: rclone `lts-photo` mount confirmed active at
  `~/mnt/lts-photo`; sample sorted upload tree exists at
  `/srv/ftp-camera/photos/2026/05/14/{JPG,RAW}/`.
- Bug in the debug script (since fixed): probing `rawtherapee --version` opened the RawTherapee GUI because that build ignores
  `--version`. Script now splits binaries into safe-to-probe vs
  presence-only.

### 2026-05-20 (second revision after manual test)

User ran the manual geeqie test on the host. **Result: the second
`geeqie /path/B.jpg` invocation loaded B into the existing window —
same instance, same window, no new window spawned.** Geeqie 2.x is
implicitly single-instance via the CLI itself; no `--remote` flag or
socket plumbing is needed.

Consequence for the plan:

- Viewer pick reverts to **geeqie**. Original plan target reached, via
  a different mechanism than the original research described.
- Zero new package installs for the viewer. `play-ftp-camera.yml` only
  needs the `gum` (or `fzf`) install for the TUI.
- The implementation primitive simplifies to a single backgrounded
  `geeqie "$file" &` call — no PID file, no IPC, no respawn logic.
- Two decision-gate questions (replace-vs-queue, closed-mid-session)
  are answered automatically by geeqie's behaviour and have been
  struck through.
- `imv` retained as a contingency in the Risks table only.

### 2026-05-20 (third revision after user redesign)

User reshaped `--view` from a self-contained mode into an orthogonal
modifier and tightened the class-filtering and source-folder rules.
Material changes from the previous revision:

- `--view` and `--view-jpg` are now MODIFIER flags that compose with
  any FTP-server primary mode (default / `--async` / `--async-copy`).
  Mutually exclusive with each other and with non-server modes.
- Class filter is now strict, not last-write-wins: `--view` fires only
  on ARW arrivals; `--view-jpg` fires only on JPG arrivals. No flicker.
- Source folder is always the local FTP upload tree, never the rclone
  mount destination. Made explicit in code comments.
- Folder-to-watch depends on whether sort is active:
  - sort active → `$UPLOAD_DIR/photos/$(date +%Y/%m/%d)/$VIEWER_CLASS/`
  - default mode → `$UPLOAD_DIR/` (filter by extension).
- Pre-warm is now conditional: only in async modes (where today's
  class subfolder is guaranteed empty/clean). Default mode cold-starts
  geeqie on first matching arrival to avoid surfacing stale/mixed
  files in the upload root.
- TUI is now a two-step `gum choose` flow: mode picker then viewer
  picker. Skips step 2 for non-server modes.
- New startup confirmation banner (Decision 6) prints a structured
  summary of every salient state variable before the script enters
  its monitor loop, so the user can verify exactly what's running at
  a glance.
- Arg parser is no longer a single-arg `case`; becomes a `while` loop
  over `$@` to accept mode + modifier combinations.

Decisions added: 5 (orthogonal modifier), 6 (banner format).
Decision 2 (class filter) and 4 (pre-warm) rewritten.
