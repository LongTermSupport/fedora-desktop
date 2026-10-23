# Plan 00219 design: desktop checks over D-Bus

The interface, the test shell, the client tool, the security model and the decisions. The
plan's tasks are in [PLAN.md](PLAN.md); this file is the reference they build to.

## 1. Why the host agent cannot see the screen today

GNOME Shell 50 closes every route an agent on the host would use (read from the extracted
50.5 source, `untracked/gnome-shell/50.5/js-extracted/org/gnome/shell/`):

| Route                                         | Why it fails                                                               | Source                               |
| --------------------------------------------- | -------------------------------------------------------------------------- | ------------------------------------ |
| `org.gnome.Shell.Eval`                        | returns `(false, '')` unless `global.context.unsafe_mode`                  | `ui/shellDBus.js` `EvalAsync`        |
| `org.gnome.Shell.Introspect.GetWindows`       | `DBusSenderChecker`: only allowlisted well-known names, unless unsafe mode | `misc/introspect.js`, `misc/util.js` |
| `org.gnome.Shell.Screenshot.ScreenshotWindow` | the same sender check                                                      | `ui/screenshot.js`                   |
| the screenshot portal                         | asks the owner on their screen                                             | xdg-desktop-portal                   |
| `wmctrl`, `xdotool`                           | X11 only; native Wayland windows are invisible to them                     | -                                    |

`DBusSenderChecker.checkInvocation` returns early when `global.context.unsafe_mode` is set.
That one fact shapes the design: a throwaway shell this repo starts in unsafe mode gives
Eval, Introspect and Screenshot for free, while the owner's real shell stays locked.

The repo already exports an object on the shell's own bus connection that outside processes
call: the speech-to-text panel's `org.fedoradesktop.SpeechToText.Panel.PasteKey` at
`/org/fedoradesktop/SpeechToText/Panel` (`extensions/speech-to-text@fedora-desktop/extension.js`,
`_exportPanelDBus`), called by `wsi` through `gdbus`. An extension-exported object is not
behind the sender check, so the same shape works for a read-only facts object.

## 2. Two parts

1. **A read-only facts object in the `fedora-desktop` panel extension**, live in the
   owner's real session and in the test shell. It changes nothing on the screen.
2. **A test shell**: a headless GNOME Shell on a private session bus, in unsafe mode, with
   its own config, data and cache directories. Everything that changes a screen (opening
   windows, switching workspace, taking a screenshot) happens there, never on the owner's.

The extension gets **no mutating method and no screenshot method** (D1). The test shell's
mutations use the shell's own unsafe-mode APIs, which the real session refuses by itself:
the guard is GNOME's, not a switch of ours that a session process could flip.

## 3. The facts interface

- Bus: the session bus. Exported with `Gio.DBusExportedObject.wrapJSObject(...).export(Gio.DBus.session, PATH)`
  on the shell's connection, so the destination is `org.gnome.Shell`. No well-known name of
  its own is owned.
- Object path: `/org/fedoradesktop/DesktopFacts`
- Interface: `org.fedoradesktop.DesktopFacts`
- One method, no properties, no signals:

```xml
<node>
  <interface name="org.fedoradesktop.DesktopFacts">
    <method name="GetWindows">
      <arg type="s" direction="out" name="facts_json"/>
    </method>
  </interface>
</node>
```

`GetWindows` returns one JSON document, a snapshot taken in one main-loop turn, so the
workspace index and the window list cannot disagree (two calls could straddle a switch):

```json
{
  "schema": 1,
  "headless": false,
  "active_workspace": 0,
  "workspaces": 4,
  "windows": [
    {
      "id": "2147483655",
      "wm_class": "org.gnome.TextEditor",
      "app_id": "org.gnome.TextEditor.desktop",
      "workspace": 0,
      "monitor": 0,
      "focused": true,
      "minimized": false
    }
  ]
}
```

| Field              | Meaning                                                                              | Meta/Shell call (verified against the 50.5 source in Task 2.1)       |
| ------------------ | ------------------------------------------------------------------------------------ | -------------------------------------------------------------------- |
| `schema`           | `1`. Bumped on any incompatible change; the client refuses a schema it does not know | -                                                                    |
| `headless`         | true in the test shell; lets a reader prove which shell it asked                     | `global.backend.is_headless()` (used in `ui/main.js`)                |
| `active_workspace` | 0-based index                                                                        | `global.workspace_manager.get_active_workspace_index()`              |
| `workspaces`       | count                                                                                | `global.workspace_manager.n_workspaces`                              |
| `id`               | the window's stable id, as a **string** (a uint64 does not survive a JSON double)    | `window.get_id()`                                                    |
| `wm_class`         | `""` when unset                                                                      | `window.get_wm_class()`                                              |
| `app_id`           | the shell's app id for the window, `""` when it has none                             | `Shell.WindowTracker.get_default().get_window_app(window)?.get_id()` |
| `workspace`        | 0-based index, or `null` for a window on all workspaces                              | `window.is_on_all_workspaces()`, `window.get_workspace().index()`    |
| `monitor`          | monitor index                                                                        | `window.get_monitor()`                                               |
| `focused`          | it has keyboard focus                                                                | `window.has_focus()`                                                 |
| `minimized`        |                                                                                      | `window.minimized`                                                   |

Windows listed: the same eligibility as the shell's own Introspect (`_isEligibleWindow`): not
override-redirect, and type NORMAL, DIALOG, MODAL_DIALOG or UTILITY. Sorted by `id`, so two
snapshots diff cleanly.

**No titles** (D2). A title carries content (document names, mail subjects, page titles);
`wm_class` and `app_id` name the program, which `ps` already tells any same-user process.

**No state, no cost.** The call walks the window list once and returns. It is synchronous
in the compositor, which is fine for a list of tens of windows and no I/O; no timer, no
subscription, nothing to clean up but the export itself.

### Code shape (thin extension, logic testable)

- `extensions/fedora-desktop@fedora-desktop/windowFacts.js`: a pure function
  `windowFacts({windows, activeWorkspace, workspaces, headless, appIdOf})` that builds the
  document from plain values. No GNOME import, so the Node harness tests it directly.
- `extensions/fedora-desktop@fedora-desktop/desktopFacts.js`: `exportDesktopFacts()` /
  `unexportDesktopFacts()`; reads the Meta/Shell objects, applies the eligibility filter,
  calls `windowFacts`, and is the only file that touches `global`.
- `extension.js`: one call in `enable()`, one in `disable()`, and a paragraph in the header
  saying the panel also answers this object. The panel still runs no check and applies no fix.
- A call that throws inside the handler returns a D-Bus error with the message, never a
  partial document.

## 4. The test shell

A Go command, `desktop-check` (`src-go/cmd/desktop-check`, D7), built and installed by the
panel play. Subcommands:

| Subcommand                                                          | Does                                                                                                                                             | On the real session                |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------- |
| `windows`                                                           | calls `GetWindows` on whatever bus `DBUS_SESSION_BUS_ADDRESS` names, checks `schema`, prints the JSON to stdout                                  | works (read-only)                  |
| `wait-window --app-id A --not-in F`                                 | polls `windows` (bounded, default 15 s) until a window of `A` appears whose id is not in snapshot file `F`; prints it; exits non-zero on timeout | works (read-only)                  |
| `shell [--size WxH] [--workspaces N] [--link SRC=REL]... -- CMD...` | starts a test shell, runs `CMD` inside it, tears it down, exits with `CMD`'s status                                                              | n/a: it never touches the real one |
| `activate-workspace N`                                              | Eval `global.workspace_manager.get_workspace_by_index(N).activate(global.get_current_time())`                                                    | **refused**: see below             |
| `screenshot-window FILE`                                            | `org.gnome.Shell.Screenshot.ScreenshotWindow(true, false, false, FILE)` on the focused window                                                    | **refused**: see below             |

**The mutation guard.** `activate-workspace` and `screenshot-window` first Eval
`global.backend.is_headless() && global.context.unsafe_mode` and run only on `(true, 'true')`.
The real session answers Eval with `(false, '')`, so they refuse with exit 3 and a message
naming the real session, and never reach the call. The guard is checked by the tool **and**
enforced by GNOME: even a tool bug cannot switch the owner's workspace or capture the owner's
screen, because their shell refuses Eval and the Screenshot call for any sender not on its list.
The JavaScript is a fixed template; `N` is parsed as an integer by Go before it is formatted in.

**What `shell` does**, in order (each step fails loudly, with the shell's log, on error):

1. A private directory `$XDG_RUNTIME_DIR/desktop-check/<pid>/` (tmpfs, under the user's 0700
   runtime dir, D6) with `config/`, `data/`, `cache/`, `state/`.
2. A private session bus: `dbus-daemon --session --nofork --print-address` with that
   environment, so activated services (dconf, the apps) inherit the private `XDG_*` dirs.
3. dconf seeded on that bus (written to `config/dconf/user`, never the owner's database):
   `org.gnome.shell enabled-extensions ['fedora-desktop@fedora-desktop']`,
   `org.gnome.mutter dynamic-workspaces false`, `org.gnome.desktop.wm.preferences num-workspaces N` (default 2), and the welcome-tour key set so no dialog takes focus.
4. `data/gnome-shell/extensions/fedora-desktop@fedora-desktop` symlinked to the **deployed**
   copy under `~/.local/share/gnome-shell/extensions/`, so the test shell runs what the play
   installed. Each `--link SRC=REL` adds `data/REL -> SRC` (00198 links its desktop file).
5. `gnome-shell --headless --unsafe-mode --virtual-monitor WxH --wayland-display desktop-check-<pid> --no-x11`
   (flags established by triage, Task 1.1), `DISPLAY` and `WAYLAND_DISPLAY` unset.
6. Ready when `GetWindows` answers with `headless: true` (bounded wait, default 30 s). This
   doubles as proof that the deployed extension loads.
7. `CMD` runs with `DBUS_SESSION_BUS_ADDRESS`, `WAYLAND_DISPLAY`, `XDG_CONFIG_HOME`,
   `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME` pointing at the test shell, and
   `DISPLAY` unset. Inside it, `desktop-check windows` and `gio open` reach the test shell.
8. On exit, or SIGINT/SIGTERM: the shell is sent SIGTERM (then SIGKILL after a bound), the
   bus stopped, the directory removed. A run that cannot clean up says so and exits non-zero.

A headless shell needs no logout and no seat, and it loads the deployed extension files
fresh, so **the test shell sees a new extension deploy at once**; only the real-session
`GetWindows` waits for the owner's next login.

Go rules: `src-go/CLAUDE.md` (execx seams for every process, fakes in unit tests, the
`project_realtime_bound` rule for every wait). Process control follows the existing
child-process patterns in `src-go/internal/` rather than new ones.

## 5. Security

| Concern                                                      | Answer                                                                                                                                                                                                                              |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Who can call `GetWindows` on the real session                | Any unsandboxed process of the same user on the session bus. Flatpak apps cannot: xdg-dbus-proxy does not let them talk to `org.gnome.Shell` without an explicit `--talk-name`. Not the system bus, not other users.                |
| What it reveals                                              | Which programs have windows, on which workspace and monitor, which is focused. No titles, no contents, no geometry, no pixels. A same-user process can already list the programs (`/proc`), so the new fact is placement and focus. |
| Can it change anything                                       | No. One method, read-only, no arguments.                                                                                                                                                                                            |
| Screenshots of the owner's screen                            | Never. Not in the extension; the client's screenshot is refused on the real session by the tool and by GNOME's own sender check.                                                                                                    |
| Workspace switching on the owner's screen                    | Never. Same double guard.                                                                                                                                                                                                           |
| The test shell's Eval                                        | Reachable only on its private bus, whose socket address is in the child environment and under the user's 0700 runtime dir. A same-user process that went looking could reach it, and could equally run its own shell: no new power. |
| The test shell writing the owner's settings                  | It has its own `XDG_CONFIG_HOME`, so its dconf database is a throwaway file. Task 1.1 proves the owner's `~/.config/dconf/user` is unchanged by a run.                                                                              |
| The owner's other extensions running in the test shell       | They do not: `enabled-extensions` lists only this one, in the throwaway database.                                                                                                                                                   |
| Apps launched in the test shell reaching the owner's session | They get the private bus and the test shell's Wayland socket, and `DISPLAY` is unset. Task 1.1 checks a launched app's window appears in the test shell and nowhere else.                                                           |

**Why no owner-enabled gate on a mutating method.** A gate held in gsettings, a file or an
environment variable is writable by the same processes it would keep out, so it would be a
switch rather than a guard. Putting mutation where GNOME already allows it (a shell this tool
owns, in unsafe mode) needs no gate.

## 6. How a plan's acceptance calls it

From the host agent's shell (raw form, for a human):

```bash
gdbus call --session --dest org.gnome.Shell \
  --object-path /org/fedoradesktop/DesktopFacts \
  --method org.fedoradesktop.DesktopFacts.GetWindows
```

From a script (the JSON on stdout, diagnostics on stderr):

```bash
desktop-check windows | jq '.active_workspace'
desktop-check shell -- "${PLAN_SCRIPT_DIR}/in-test-shell.bash"
```

A plan's acceptance puts its in-shell steps in one plan-local script and runs it through
`desktop-check shell`, so set-up and teardown are owned by the tool, not by each plan.

A real-session call before the owner has logged out and in since the deploy fails with
`UnknownObject`; `desktop-check windows` turns that into "the running shell has not loaded
the facts object yet: log out and in", and an acceptance treats it as not yet established,
never as a pass.

## 7. Use by Plan 00198 (a file opened as Nautilus does opens a window on the active workspace)

In-shell script, run twice by 00198's acceptance:

1. `desktop-check windows > before.json`; assert `active_workspace` 0 and `gio mime text/plain`
   names `org.gnome.TextEditor.desktop`.
2. `gio open a.txt` (GIO's default-handler launch, the path Nautilus takes for an unsandboxed
   app); `wait-window --app-id org.gnome.TextEditor.desktop --not-in before.json` gives W1;
   assert W1 is on workspace 0.
3. `desktop-check activate-workspace 1`; `windows > mid.json`; assert `active_workspace` 1.
4. `gio open b.txt`; `wait-window ... --not-in mid.json` gives W2; assert W2 is on workspace 1,
   `active_workspace` is still 1, W1 is still on workspace 0.

- **Green run**: `desktop-check shell --link "$HOME/.local/share/applications/org.gnome.TextEditor.desktop=applications/org.gnome.TextEditor.desktop" -- ...`
  (the deployed override). All asserts hold.
- **Red run**: the same without `--link`, so the system desktop file (`DBusActivatable=true`)
  is used. Step 4 must fail: no new window (b.txt became a tab in W1), or the active workspace
  went back to 0. A red run that passes means the check does not test the fix, and the
  acceptance fails.

This proves the fix with no owner and no change to the owner's screen. Whether the owner's
own Nautilus double-click stays a gate for 00198 is 00198's call (D8). If the owner does it,
`desktop-check windows` afterwards records the new window's workspace against the active one
as evidence rather than a remembered answer.

**Premise to watch** (Task 1.1): `gio open` from a script carries no activation token, so in
the red run the shell may not raise W1 and switch workspace. The "no new window" assert still
catches the red case, so the check stays sound either way; the plan records which red
symptom appeared.

## 8. Use by Plan 00185 (emoji glyphs)

In the test shell: start the terminal under test (Ptyxis first) running the issue's
`printf '\U0001F4C8 \U0001F9F5 \U0001F4E6\n'` and then sleeping; `wait-window` for it; it
has focus as the only window; `screenshot-window` to a PNG under the run log directory.

- The host agent reads the PNG with its own image reading and states which glyphs it shows;
  that is the "fresh window" check of 00185's Task 1.2 done without the owner. A mechanical
  comparison against reference glyphs (`hb-view` with the packaged Noto Color Emoji) is
  00185's choice, not built here.
- It decides between 00185's hypotheses only for a **fresh** process: a correct fresh Ptyxis
  points at H1 (a long-running terminal's stale glyph table), a wrong one at H5 (the COLRv1
  table read wrongly). It cannot observe the owner's long-running terminal, which stays the
  owner's check.
- The screenshot is of the test shell only; the owner's screen is never captured.

## 9. Decisions (coordinator-defaultable; recommended default first)

| #   | Decision                                                                 | Recommended default                                                                                           | Alternative                                                                                                            |
| --- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| D1  | Mutating and screenshot methods in the extension                         | **None.** Mutation and screenshots only in the test shell, through GNOME's own unsafe-mode APIs               | `ActivateWorkspace`/`ScreenshotWindow` in the extension behind an owner switch (rejected: a switch is not a guard, §5) |
| D2  | Window titles in `GetWindows`                                            | **Omitted**                                                                                                   | Included (leaks content to any same-user process)                                                                      |
| D3  | Return shape                                                             | **One JSON string** with `schema`, ids as strings; scripts read it with one `jq`, Go with `encoding/json`     | A typed `a(...)` GVariant (nothing here parses gdbus's GVariant text)                                                  |
| D4  | Where the object lives                                                   | **The `fedora-desktop` panel extension**; its play already copies the whole directory and declares it enabled | A new extension and play                                                                                               |
| D5  | Owner opt-in for the read-only object                                    | **Always on** with the panel                                                                                  | A gsettings switch (adds a key and protects nothing, §5)                                                               |
| D6  | Test shell scratch location                                              | **`$XDG_RUNTIME_DIR/desktop-check/<pid>/`**, removed on exit                                                  | `untracked/` in the checkout (survives crashes, but is on disk and per-checkout)                                       |
| D7  | Client name                                                              | **`desktop-check`**, installed to `~/.local/bin/` by the panel play                                           | -                                                                                                                      |
| D8  | 00198's owner Nautilus check once the test-shell legs pass red and green | **Optional confirmation**, recorded with `desktop-check windows` if done                                      | Kept as a gate                                                                                                         |
