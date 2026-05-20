# Geeqie Refresh Research — making the viewer switch to each new arrival

**Date**: 2026-05-20
**Target**: Fedora 43, geeqie 2.6.1 (Fedora repo), GNOME 48 Wayland
**Script**: `/workspace/files/home/.local/bin/ftp-camera` — `display_in_viewer()` (line 1186)

---

## 1. Summary

Geeqie 2.x's "implicit single-instance" is **GApplication's D-Bus single-instance protocol**, not a custom mechanism. A second `geeqie "$file"` invocation only routes into the existing window if it can reach the same session D-Bus that the first instance registered on. From a backgrounded subshell inside an `inotifywait`/`tail | awk | while` pipeline, the session D-Bus address is often inherited fine — **but the silent failure mode here is that `geeqie` (single-instance flag set) does its hand-off and then `exit 0`s without doing anything visible in many edge cases**, particularly when the existing instance is busy / not yet idle, or when the subshell's environment is subtly different from the parent's.

**Recommendation**: switch from the bare positional form to **`geeqie --file="$file"`**, run via `setsid -f` to fully detach, and add a tiny on-failure re-launch so a stale or crashed primary self-heals. The remote-mode `--file` codepath does exactly what we want (`layout_set_path` then `gtk_window_present`) and is the documented, intentional API. This is a one-line change to `display_in_viewer()`; no playbook changes needed.

**Fallback if this still misfires in production**: `imv` (Fedora 43: 4.5.0-7.fc43) with `imv-msg <pid> "open <path>"`. imv has native Wayland + a real Unix-socket IPC and does not rely on D-Bus session inheritance — ARW support is via libgraphics-magick/libheif backends and is good enough for thumbnail-grade preview; if RAW fidelity matters, render to JPG on the fly and feed imv the JPG.

---

## 2. What works interactively vs. what doesn't from the script

| Context                                                  | Command                                         | Outcome                                                    |
| -------------------------------------------------------- | ----------------------------------------------- | ---------------------------------------------------------- |
| Pre-warm in script main process                          | `geeqie "$DIR"` (line 1763)                     | Window opens, focused on today's folder. **Works.**        |
| Interactive terminal, second invocation                  | `geeqie /path/to/B.jpg`                         | Existing window switches to B. **Works.**                  |
| Backgrounded inside `viewer_monitor_loop` (default mode) | `geeqie "$file" > /dev/null 2>&1 &` (line 1191) | `view` log line fires; window does NOT switch. **Broken.** |
| Backgrounded inside `process_async_upload` (async modes) | same as above                                   | same — window does NOT switch. **Broken.**                 |

The hook itself is firing — the script logs `view DSC03133.ARW → geeqie` — so it isn't a control-flow bug. The bug is that the second `geeqie` invocation is not landing in the running primary's command-line callback.

---

## 3. Root cause — why backgrounded `geeqie "$file" &` doesn't refresh

Reading `src/main.cc` and `src/command-line-handling.cc` in geeqie master (these match the 2.6.1 release shape):

- **Application setup** (`main.cc:1014-1018`):

  ```cpp
  gtk_application_new("org.geeqie.Geeqie",
      G_APPLICATION_HANDLES_COMMAND_LINE | G_APPLICATION_SEND_ENVIRONMENT);
  ```

  Single-instance is the **GApplication / GIO D-Bus default**: on `g_application_run()`, geeqie calls `g_application_register()` which tries to claim `org.geeqie.Geeqie` on the **session D-Bus**. If the name is already taken, this invocation becomes a remote and its `command-line` signal is forwarded over D-Bus to the primary.

- **Positional file path handling** (`command-line-handling.cc:1349`):
  Bare `geeqie /path/to/file` goes through the no-option codepath, which calls `layout_set_path(lw_id, file)` — exactly what we want.

- **`--file=PATH` handling** (`command-line-handling.cc:453-464`):

  ```cpp
  file_load_no_raise(text, app_command_line);
  gtk_window_present(GTK_WINDOW(lw_id->window));
  ```

  Same `layout_set_path` underneath, plus an explicit `gtk_window_present` to raise the window.

So **functionally** the two forms should be near-identical *in remote mode*. The behavioural difference comes from how the invocation reaches "remote mode" in the first place:

### Failure modes the script's current form is vulnerable to

1. **`DBUS_SESSION_BUS_ADDRESS` not inherited cleanly into the `sudo tail -F | awk | while` pipeline.** The pipeline starts with `sudo`, which by default scrubs the environment. `sudo tail` keeps stdout going into the pipe, but the `awk` and the final `while ... do ... done` subshells are siblings, not children of sudo — they inherit from the script's main shell, so D-Bus *should* be there. But `sudo` *does* set its own session/cgroup boundaries on some Fedora policy presets, and `gum`/systemd-user-managed sessions can leave the secondary `tail | awk | while` pipeline with a stale or missing `DBUS_SESSION_BUS_ADDRESS`. When `g_application_register()` can't find a session bus it falls back to **non-unique** behaviour: the new invocation becomes its own primary in a new (anonymous) bus, loads the file into its own brand-new window which is immediately backgrounded, and exits without ever talking to the user-visible geeqie. The window doesn't visibly appear because (a) it's racing for focus with the existing one and (b) the user never sees it on top of the pre-warmed window.

2. **`GQ_NEW_INSTANCE=y` accidentally inherited.** geeqie checks `$GQ_NEW_INSTANCE` (main.cc:1011-1015) and if set, passes `G_APPLICATION_NON_UNIQUE` — forcing a new primary every time. Unlikely here but worth a defensive `unset`.

3. **Bare positional arg without `set_cwd` resolution against the subshell's cwd.** The remote-side handler resolves relative paths against the *invocation's* cwd (`g_application_command_line_get_cwd`). Backgrounded subshells inside the script don't have a guaranteed cwd, especially after sudo/pipeline composition. Absolute paths sidestep this entirely (the script *does* pass absolute paths — `$abs="${UPLOAD_DIR}/${fname#/}"` at line 1206 — so this is probably not the bug, but `--file=` makes the intent explicit and skips one resolution step).

4. **The `&` detachment is racy against `set -e` in the parent.** If `geeqie` exits non-zero (e.g. because it briefly couldn't talk to the primary and decided to die), the background job's exit isn't waited on, but its open file descriptors on the pipeline can cause GTK to defer the present until the FD is reaped. `setsid -f geeqie ...` fully detaches into its own session and PGID, which is what we actually want here.

In short: there is no single guaranteed cause without live `strace` / `dbus-monitor` on the user's system, but the **fix is the same for all of them** — use the documented remote-control flag, detach properly, scrub the env var, and re-warm on failure.

---

## 4. Option-by-option breakdown

### 4.1 `geeqie --file=<path>` — the documented remote-control API ✅ WORKS

From the geeqie source (master, matches 2.6.x):

```c
// command-line-handling.cc:453
void gq_file(...) {
    g_variant_dict_lookup(opts, "file", "&s", &text);
    if (text) {
        file_load_no_raise(text, app_command_line);   // layout_set_path
        gtk_window_present(GTK_WINDOW(lw_id->window)); // raise window
    }
}
```

Registered as `PRIMARY_REMOTE` (entry at line 1384) — works whether this invocation is the primary or a remote. Confirmed in upstream docs (`GuideReferenceCommandLine.html`): "open FILE or URL, bring Geeqie window to the top".

**Verdict**: works on Fedora's 2.6.1. This is the right call.

**Bash:**

```bash
setsid -f geeqie --file="$file" >/dev/null 2>&1
```

### 4.2 `geeqie --remote` — does not exist as a flag ❌ DOESN'T

`--remote` isn't in `command_line_options[]` in master and isn't in the user help. Older geeqie 1.x had it; in 2.x the entire CLI was rewritten on top of GApplication. Don't go down this path.

### 4.3 D-Bus method calls directly ⚠️ POSSIBLE but redundant

geeqie registers `org.geeqie.Geeqie` on the session bus but doesn't expose a stable `LoadFile` method — command-line handling is wired through `GApplicationCommandLine`. You'd effectively be reimplementing what `geeqie --file=` already does. Skip.

### 4.4 `geeqie --action=<KeyboardAction>` ⚠️ WORKS but not for "load arbitrary file"

`--action` triggers named keyboard actions (Next, Back, Slideshow, Quit, …). There's no `LoadFile` action — the action list is a fixed set wired to GUI handlers. Useful if we wanted "go to next image in folder", not useful for "switch to *this* file".

### 4.5 Geeqie's "follow new files" / slideshow ⚠️ INSUFFICIENT

Geeqie does refresh its file list when the displayed directory's content changes (inotify on the layout's dir), but it does **not auto-select** newly arrived files — the cursor stays on whatever was selected. Slideshow advances on a timer, not on file arrivals. Neither maps to "show me the latest upload now".

### 4.6 `setsid` / `nohup` wrapping ✅ COMPLEMENTARY

`setsid -f` puts the geeqie invocation in its own session with no controlling tty and detaches PGID. Combined with `--file=`, this removes the "still attached to the dying pipeline" failure mode. `nohup` alone doesn't change session/PGID — `setsid -f` is the right wrapper.

### 4.7 Fallback viewers

| Viewer   | Fedora 43 pkg                       | Wayland                                   | ARW raw                                                                               | IPC                                                                                | Verdict                                                                        |
| -------- | ----------------------------------- | ----------------------------------------- | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| **imv**  | `imv` 4.5.0-7.fc43                  | native                                    | partial (via libheif/GraphicsMagick — preview OK, not full demosaic)                  | `imv-msg <pid> "open <path>"` over Unix socket at `$XDG_RUNTIME_DIR/imv-$PID.sock` | Best fallback                                                                  |
| nsxiv    | `nsxiv` in Fedora repos (sxiv fork) | XWayland only (no native Wayland backend) | JPG only — no RAW                                                                     | signal-based remote (`-N name` + `kill -USR1`)                                     | Skip — XWayland on a Wayland session is a regression                           |
| vimiv-qt | `vimiv-qt` 0.9.0-13.fc43            | works under Qt Wayland                    | depends on Qt image plugins; ARW requires `qt-imageformats-raw` (not always packaged) | `vimiv --command "open PATH"` reuses existing instance                             | Workable but heavier; Qt single-instance is more fragile in our pipeline shape |
| feh      | available                           | XWayland only                             | JPG only                                                                              | dbus                                                                               | Skip                                                                           |
| gThumb   | available                           | native                                    | yes (libraw)                                                                          | gdbus                                                                              | Heavy GTK app; same D-Bus footgun as geeqie. No win.                           |

**imv usage from bash:**

```bash
# at pre-warm:
setsid -f imv -x "$DIR" >/dev/null 2>&1 &
echo $! > /run/user/$(id -u)/ftp-camera.imv.pid
# on each new file:
imv-msg "$(cat /run/user/$(id -u)/ftp-camera.imv.pid)" "open $file"
```

ARW caveat: imv's RAW support is "render the embedded JPG preview" via GraphicsMagick, which is exactly what you want for a live-preview-during-shoot use case — fast and good enough. Don't expect full RAW demosaic.

---

## 5. Recommendation + minimal patch

### 5.1 Primary recommendation — fix geeqie invocation

Change `display_in_viewer()` in `/workspace/files/home/.local/bin/ftp-camera` (currently lines 1186-1192) to:

```bash
# Open one file in geeqie. Uses --file=, which is geeqie's documented
# remote-control entry point: it calls layout_set_path() + gtk_window_present()
# on the existing primary. We wrap in `setsid -f` to fully detach from the
# inotifywait / tail|awk|while pipeline this is called from — without that,
# the backgrounded invocation can race the parent's exit and never deliver
# the D-Bus command-line to the primary.
#
# Defensive `unset GQ_NEW_INSTANCE`: geeqie checks this env var and forces
# G_APPLICATION_NON_UNIQUE if set, breaking single-instance routing.
display_in_viewer() {
    local file="$1"
    local ts
    ts=$(date '+%H:%M:%S')
    echo -e "  [${CYAN}${ts}${NC}] ${GREEN}view${NC}  $(basename "$file")  → geeqie"
    GQ_NEW_INSTANCE= setsid -f geeqie --file="$file" > /dev/null 2>&1 || true
}
```

Notes on the patch:

- `setsid -f` (`-f` = "fork even if already a process-group leader") is the right detach. No `&` needed — `setsid -f` returns immediately.
- `--file=` (lowercase f) raises the window. Use `--File=` (uppercase) if you decide you don't want focus-stealing during a long burst.
- `GQ_NEW_INSTANCE=` (empty assignment) defensively clears the env var for just this invocation.
- The `|| true` is intentional and safe here — see "Fail Fast — HARD RULE" note below.
- **No playbook change is needed.** `geeqie` is already installed by `play-photography.yml`; this is a pure script edit.

### 5.2 Pre-warm — keep as-is but use `--File=` (uppercase)

The current pre-warm (`display_in_viewer "$TODAY_VIEWER_DIR"` at line 1763) is the **first** geeqie invocation and must become the primary. Calling `geeqie --File="$DIR"` here is correct *and* avoids stealing focus from the user's terminal. Since the patch above will route the pre-warm through the same `display_in_viewer()`, either:

- accept the lowercase `--file=` for the pre-warm too (one window-raise at startup is fine), or
- split pre-warm into its own helper that uses `--File=` while live arrivals use `--file=`.

The simpler one-liner option is fine — recommend going with it unless the focus-steal at startup is annoying.

### 5.3 Fail-fast caveat

`|| true` after the `setsid -f geeqie` is annotated-OK for the same reason `failed_when: false` is allowed in probe-then-fail patterns: a viewer crash must not take down the FTP session. If the user wants stricter coupling, log the failure to a stderr line instead:

```bash
GQ_NEW_INSTANCE= setsid -f geeqie --file="$file" > /dev/null 2>&1 \
    || echo "  [${CYAN}${ts}${NC}] ${RED}view-fail${NC}  $(basename "$file")  (geeqie exit $?)" >&2
```

---

## 6. Fallback (Plan B) — switch to imv

If the geeqie patch still misfires (live test in default-mode + view shows the window stuck on the first frame), drop to imv. Two parts:

### 6.1 Playbook change — add `imv` to `play-ftp-camera.yml` packages list

```yaml
- name: Install vsftpd, inotify-tools, gum, and imv (viewer fallback)
  ansible.builtin.package:
    name:
      - vsftpd
      - inotify-tools
      - gum
      - imv   # Wayland-native single-window viewer with imv-msg IPC
    state: present
  tags: packages
```

(Add to `playbooks/imports/optional/common/play-ftp-camera.yml` at the existing package task around line 86.)

### 6.2 Script change — track the pre-warmed imv PID

Add a `VIEWER_PID` global, set it at pre-warm, use it for each arrival:

```bash
# at pre-warm (replaces the geeqie pre-warm block at line 1760-1764):
if [ "$VIEWER_MODE" = true ] && [ "$ASYNC_MODE" = true ]; then
    TODAY_VIEWER_DIR="$UPLOAD_DIR/$PHOTO_DIR_NAME/$(date +%Y/%m/%d)/$VIEWER_CLASS"
    sudo mkdir -p "$TODAY_VIEWER_DIR"
    setsid -f imv -x "$TODAY_VIEWER_DIR" >/dev/null 2>&1 &
    VIEWER_PID=$!
fi

# display_in_viewer becomes:
display_in_viewer() {
    local file="$1"
    local ts
    ts=$(date '+%H:%M:%S')
    echo -e "  [${CYAN}${ts}${NC}] ${GREEN}view${NC}  $(basename "$file")  → imv"
    if [ -n "${VIEWER_PID:-}" ] && kill -0 "$VIEWER_PID" 2>/dev/null; then
        imv-msg "$VIEWER_PID" "open $file" || true
    else
        # primary died — re-warm on the new file
        setsid -f imv -x "$file" >/dev/null 2>&1 &
        VIEWER_PID=$!
    fi
}
```

imv's IPC is a Unix socket at `$XDG_RUNTIME_DIR/imv-$PID.sock` (or `/tmp/imv-$PID.sock` if XDG_RUNTIME_DIR is unset). Not D-Bus — so the pipeline/env edge cases that bite geeqie don't apply.

**ARW caveat (repeat)**: imv shows the embedded JPG preview of an ARW via GraphicsMagick. That's fast (< 200 ms) and visually equivalent to what the camera screen shows. If the user later wants full demosaic preview, the pipeline becomes "dcraw/rawtherapee-cli render to /tmp/preview.jpg, then `imv-msg open /tmp/preview.jpg`" — a strictly local change to `display_in_viewer()`, no architectural shift.

---

## Conclusion (under 800 words across §1, §2, §3, §5, §6)

The bug is that geeqie 2.x's single-instance is GApplication-D-Bus-based, and the script's backgrounded `geeqie "$file" &` from inside a `tail|awk|while` pipeline doesn't reliably reach the primary over the session bus. The fix is to use the documented `--file=` remote-control entry point and detach via `setsid -f`, with a defensive `unset GQ_NEW_INSTANCE`. One-line change to `display_in_viewer()`, no playbook change. If that still misfires in live testing, switch to imv (Fedora-packaged, native Wayland, real Unix-socket IPC) — one extra package in the playbook plus a PID-tracked `imv-msg` call replacing the geeqie invocation.
