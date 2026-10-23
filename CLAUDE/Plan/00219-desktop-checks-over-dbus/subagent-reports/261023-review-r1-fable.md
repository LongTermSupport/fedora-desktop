# Plan 00219 review, round 1: security and feasibility

Reviewed: `CLAUDE/Plan/00219-desktop-checks-over-dbus/PLAN.md` and `DESIGN.md` at 82a631413.
Evidence paths below are relative to the worktree; GNOME paths are under
`untracked/gnome-shell/50.5/js-extracted/org/gnome/shell/`.

## 1. Security

### 1.1 What the 50.5 source confirms

- `ui/shellDBus.js:71-92` `EvalAsync`: returns `(false, '')` when `!global.context.unsafe_mode`;
  otherwise `JSON.stringify(await eval(code))`, `undefined` mapped to `''`. The design's Eval
  semantics (JSON string; `(true, 'true')` for the guard expression) are right.
- `misc/util.js:399-408` `checkInvocation`: returns at once when `global.context.unsafe_mode`;
  otherwise the sender's unique name must be the current owner of an allowlisted well-known
  name, else `ACCESS_DENIED`. Allowlists: Introspect `misc/introspect.js:7-10` (the two portal
  backends); Screenshot `ui/screenshot.js:2489-2492` (MediaKeys, gnome portal backend).
- `ui/screenshot.js:2683-2704` `ScreenshotWindowAsync`: params `[includeFrame, includeCursor, flash, filename]`, goes through `_createScreenshot` (2500-2530) which runs the sender check
  (`restrictCallers` default true) and the lockdown check. Return `(b, s)`. The design's
  `ScreenshotWindow(true, false, false, FILE)` matches.
- `ui/main.js:139` uses `global.backend.is_headless()`; `ui/main.js:290-306` watches
  `notify::unsafe-mode`. Both API names exist.
- The precedent `extensions/speech-to-text@fedora-desktop/extension.js:1222-1237` exports on
  `Gio.DBus.session` with `wrapJSObject(...).export(...)`, so an extension object on
  `org.gnome.Shell` with no sender check is established practice here.

### 1.2 Is the "double guard" real?

Mostly, with one stated condition the design omits.

- On a real session in normal mode: the tool's Eval gets `(false, '')` and refuses (guard 1);
  even if the tool skipped that, Eval itself returns `(false, '')` without evaluating, and
  `ScreenshotWindow` denies an unallowlisted sender (guard 2). Real.
- **Condition:** the owner can put the real shell in unsafe mode from Looking Glass (the
  `notify::unsafe-mode` handler at `ui/main.js:290-306` exists precisely because this
  happens). In that state GNOME's guard is gone for Eval **and** for Screenshot
  (`util.js:400`), and only the tool's `is_headless()` half stands. The claim "even a tool bug
  cannot switch the owner's workspace" is therefore true only while the owner's shell is in
  safe mode. Not blocking (the owner opting into unsafe mode is the owner's call, and the
  tool's check still refuses), but the security table should say so, and a cheap second tool
  guard is worth adding: `activate-workspace`/`screenshot-window` also refuse unless the bus
  address they are talking to is one `desktop-check shell` created (e.g. the socket lives under
  `$XDG_RUNTIME_DIR/desktop-check/`). That makes a tool bug need two independent mistakes.

### 1.3 GetWindows exposure

- Content: wm_class, app id, workspace, monitor, focus, minimized. No titles, no geometry.
  Lower than what `Introspect.GetWindows` returns to allowlisted callers (`introspect.js:153-175`
  includes title, frame size, sandboxed app id). Acceptable for any same-user unsandboxed
  process, which `/proc` already tells which programs run.
- **The Flatpak claim is overstated.** "xdg-dbus-proxy does not let them talk to
  `org.gnome.Shell` without an explicit `--talk-name`" is true of the default filter, but a
  Flatpak with `--socket=session-bus` (unfiltered) or `--talk-name=org.gnome.Shell` (which
  extension-manager-type apps carry) can call it, and `org.gnome.Shell` is not a name a proxy
  would single out. Reword to "a Flatpak whose manifest grants `org.gnome.Shell` or the whole
  session bus can; that is a visible permission in the manifest, and the content is placement
  and focus only". The risk level is still acceptable given D2 (no titles).

### 1.4 Test-shell isolation

- **Private bus.** `dbus-daemon --session --nofork --print-address` gives a private bus; its
  socket under the user's 0700 runtime dir is as good as the real one. Correct. Verify
  `dbus-daemon` is installed (Fedora ships dbus-broker for the user bus; the `dbus-daemon`
  package is usually present but is not guaranteed) -- Task 1.1 does this. Good.
- **dconf.** `ca.desrt.dconf` is D-Bus-activated by the private daemon with the daemon's own
  environment, and `dconf-service` writes the `user-db:user` database at
  `$XDG_CONFIG_HOME/dconf/user` (`g_get_user_config_dir`). So with `XDG_CONFIG_HOME` set on the
  daemon **before** it starts, the throwaway database is where writes go. Confident, and
  Task 1.1's checksum before/after is the right proof. Add: the seeding `dconf`/`gsettings`
  commands themselves must run with the private env (both the bus address and
  `XDG_CONFIG_HOME`; the client reads the db file directly, the service writes it).
- **BLOCKING: D-Bus-activated apps inherit the daemon's environment, not CMD's.** DESIGN.md
  §4 step 2 starts `dbus-daemon` "with that environment" (the four `XDG_*` dirs), and only
  step 7 gives `WAYLAND_DISPLAY`, `DBUS_SESSION_BUS_ADDRESS` and an unset `DISPLAY` to `CMD`.
  Every activated service (dconf, `org.gnome.TextEditor` in 00198's **red run**, portals,
  keyring) is forked by `dbus-daemon` with `dbus-daemon`'s environment. If that environment
  still carries the host agent's `WAYLAND_DISPLAY=wayland-0` (or `DISPLAY`), the D-Bus-activated
  editor connects to the **owner's** compositor and its window lands on the **owner's**
  screen. This is the exact escape the plan promises cannot happen, and it is the red run,
  the one that deliberately uses D-Bus activation. Fix: choose the Wayland socket name before
  step 2 and start `dbus-daemon` (and later the shell) with the complete private environment:
  `WAYLAND_DISPLAY=desktop-check-<pid>`, `DISPLAY` unset, `DBUS_SESSION_BUS_ADDRESS` unset
  (the daemon has none; the shell gets the printed address), `XDG_*` private, and
  `GDK_BACKEND=wayland` so GTK cannot fall back to X11. State it in step 2 and prove it in
  Task 1.1 with a D-Bus-activated app (gnome-text-editor via `gio open` with the system desktop
  file), not only a directly launched `gtk4-demo`; check the owner's shell has gained no window
  (`desktop-check windows` on the real session before/after, once the object exists, or
  `Introspect` is unavailable so use the facts object or the owner's eye on the first run).
- **systemd --user activation.** A plain `dbus-daemon --session` without
  `--systemd-activation` ignores `SystemdService=` and forks `Exec=` itself, so nothing on the
  private bus reaches the user's systemd instance. GIO's "move the launched app into a
  transient scope" call goes to `org.freedesktop.systemd1` on the **session bus it is on**
  (the private one, where no such name exists) and fails quietly. No escape. Confident.
- **Portals.** A GTK app asking `org.freedesktop.portal.Desktop` on the private bus activates
  a **second** `xdg-desktop-portal` (and `xdg-desktop-portal-gnome`, which talks to the
  headless shell). Isolated from the owner, but noisy and slow. Unsandboxed GTK4 apps rarely
  touch portals; note it in triage rather than design around it.
- **Keyring (unsure).** `org.freedesktop.secrets` on the private bus activates
  `gnome-keyring-daemon` with the private `XDG_DATA_HOME` (so a fresh, empty `keyrings/`), but
  gnome-keyring's control socket is `$XDG_RUNTIME_DIR/keyring/control`, and the runtime dir
  is **shared**. Whether a daemon started with `--start` on the private bus attaches to the
  owner's running daemon through that socket I am not sure of. Neither 00198 nor 00185 should
  trigger it, but Task 1.1 should list every name activated on the private bus
  (`dbus-monitor --address <addr>` or `dbus-daemon`'s `--print-pid` with
  `DBUS_VERBOSE`) so this is a fact, not a guess.
- **Shared `$XDG_RUNTIME_DIR` in general.** `--wayland-display desktop-check-<pid>` avoids
  `wayland-0`; `--no-x11` avoids `/tmp/.X11-unix`. Mutter/gnome-shell may still create files
  under the runtime dir (e.g. `gnome-shell/`, `mutter/`). Triage: diff `ls $XDG_RUNTIME_DIR`
  before and after a run.
- **`HOME` is shared and unchanged.** Deliberate (the extension symlink target, the deployed
  binary). Anything that ignores XDG and writes `~/.something` directly would touch the owner.
  GTK4, GLib, dconf, fontconfig all honour XDG; `fc-cache` will rebuild into the private
  `XDG_CACHE_HOME` on first run (seconds; see the 15 s `wait-window` bound below).
- **The owner's other extensions do not run** (`enabled-extensions` is in the throwaway db,
  and the shell scans `XDG_DATA_HOME/gnome-shell/extensions` plus `XDG_DATA_DIRS`). Correct.
- **Eval on the private bus is arbitrary JS as the user.** The design's "no new power" answer
  is right: same user, same `HOME`.

## 2. Feasibility

- **Flags.** `--headless`, `--virtual-monitor WxH[@Hz]`, `--wayland-display NAME` and
  `--no-x11` are mutter options gnome-shell passes through; `--unsafe-mode` is gnome-shell's
  (since 41). GNOME's own test harness runs the shell as `dbus-run-session` plus
  `gnome-shell --headless --wayland --no-x11 --virtual-monitor ...`; headless uses no seat
  and no logind session. Plausible with high confidence; `--wayland` may need to be explicit
  in addition to `--headless`. Task 1.1 (`--help-all` plus a real start) is the right scope.
  Add to its probes: the shell's stderr with no `org.gnome.SessionManager` present (it logs
  and continues in CI, but prove it on 50.5), and start-up time, so the 30 s ready bound is
  measured rather than guessed.
- **Eval.** Correct (above).
- **ScreenshotWindow.** Signature correct. Two cautions. (a) `Shell.Screenshot.screenshot_window`
  shoots the **display's focus window**; with no input device, mutter still focuses a new
  mapped window under `focus-new-windows=smart`, but a headless run has never been proved to
  here -- Task 4.1's "focused" assertion covers it, and Task 1.1 should check it early.
  (b) `_createStream` treats a relative filename as relative to the Pictures special dir; the
  tool must pass an **absolute** path and refuse a relative one.
- **Meta/Shell calls.** `get_id`, `get_wm_class`, `is_on_all_workspaces`, `get_workspace().index()`,
  `get_monitor`, `has_focus`, `minimized`, `WindowTracker.get_window_app`,
  `workspace_manager.get_active_workspace_index/n_workspaces/get_workspace_by_index`,
  `get_current_time`, `backend.is_headless` all exist in mutter/shell 50. The design does not
  say **where the window list comes from**: Introspect walks `Shell.AppSystem.get_running()`
  per app (`introspect.js:137-149`), which drops windows the tracker gave no app. Use
  `global.get_window_actors().map(a => a.meta_window)` (or `global.display.list_all_windows()`)
  so an app-less window is still listed with `app_id: ""`, as the schema promises.
- **00198 red/green.** Sound. `gio open` goes through `g_app_info_launch_default_for_uri`, which
  for `DBusActivatable=true` uses `org.freedesktop.Application.Open` -- the same path Nautilus
  takes for an unsandboxed app, so it is equivalent enough. GApplication uniqueness is per bus,
  so the editor in the private bus is a fresh instance and the owner's editor is untouched.
  Red run: `Open` on the running instance adds a tab to the existing window, so "no new
  window" fails step 4 regardless of whether the shell honours the token-less present and
  switches workspace; the design already says this. Two premises to pin in triage: the
  default handler for `text/plain` with an **empty private `XDG_CONFIG_HOME`** (no owner
  `mimeapps.list`) is the editor on Fedora (via `/usr/share/applications/gnome-mimeapps.list`;
  step 1's assert catches it if not), and the green run's user desktop file placed in the
  private `applications/` shadows the system one by id without a `mimeinfo.cache` (it does;
  GIO resolves by id across data dirs).
- **00185.** Fresh Ptyxis in the headless shell renders with llvmpipe; glyph shaping and
  COLRv1 rasterisation are CPU paths shared with the real session, so a wrong glyph there is
  evidence for H5 and a right one for H1, as §8 says. The private `XDG_CACHE_HOME` means a
  fontconfig cache rebuild on the first app start, which can exceed the 15 s `wait-window`
  default; make the bound a flag with a larger default or pre-warm with `fc-cache` in the
  shell set-up.
- **Readiness probe doubles as extension-load proof.** Good; but a failed load leaves only
  "timed out" -- the tool should dump the shell's stderr (it owns the process) on that path.

## 3. Repo-rule fit

- Thin extension: `windowFacts.js` pure, `desktopFacts.js` the only `global` toucher, two
  lines in `extension.js`. Matches the harness (`tests/extensions/gjs-loader.mjs` answers
  `gi://`), and `scripts/test-panel-sections.bash` enforces listing the new suite. Good.
- Tool in Go under `src-go/cmd/desktop-check` with `execx` fakes: right. Note `src-go/CLAUDE.md`
  expects a spec with numbered requirements for a tool of this size (the others carry
  `SPEC-*.md`, E1-En); plan a short `SPEC-desktop-check.md` or put the numbered list in the
  package doc.
- Deploy: panel play imports `play-golang.yml` and `tasks/build-go-command.yml` (whose
  `become: false` handles the play's `become: true`); `meta-deploy.list` entry in Task 4.1.
  Good. The play header says the panel "runs no check and applies no fix"; the facts object
  keeps that true, but the header should mention the object.
- Plan-local scripts: `triage.bash`, `deploy.bash`, `acceptance.bash` and the consumers'
  in-shell scripts are plan-local and exempt from the bash limits. Good.
- YAGNI: `wait-window` earns its place (both consumers poll). `--link` does not: `CMD`
  already receives the private `XDG_DATA_HOME` and `data/` is writable, so 00198's in-shell
  script can `ln -s` its desktop file itself. Drop `--link`. `--size` and `--workspaces` are
  fine as defaults-with-override.
- Missing from the tasks: (1) README index entry exists already (`CLAUDE/Plan/README.md:37`);
  (2) no `docs/` mention of the new object or tool -- add to the panel's user doc in Task 4.2;
  (3) the `headless` field in `GetWindows` is informational while the guard uses Eval -- say
  so, or the two can be read as the same guard; (4) stale run directories from a crashed
  tool (`$XDG_RUNTIME_DIR/desktop-check/<pid>` with a dead pid) -- the tool should sweep them
  on start.

## Verdict

**NOT READY** (one blocking isolation gap; the rest is wording and triage scope).

### Required fixes (blocking)

1. §4 step 2: start `dbus-daemon` with the **complete** private environment -- the chosen
   `WAYLAND_DISPLAY=desktop-check-<pid>`, `DISPLAY` unset, `GDK_BACKEND=wayland`, private
   `XDG_*`, no inherited `DBUS_SESSION_BUS_ADDRESS` -- because D-Bus-activated apps (the whole
   point of 00198's red run) inherit the daemon's environment, not `CMD`'s. Task 1.1 must
   prove it with a D-Bus-activated app and check the owner's session gained no window.

### Suggestions

1. §5: state the unsafe-mode condition on the "double guard" (Looking Glass can put the
   real shell in unsafe mode); add the tool-side second check that the bus address is one the
   tool created.
2. §5: reword the Flatpak row (apps granted `org.gnome.Shell` or the session bus can call).
3. §3: name the window source (`global.get_window_actors()`), not the app-walk Introspect uses.
4. `screenshot-window`: absolute path required; dump shell stderr on readiness timeout.
5. Drop `--link`; the in-shell script symlinks into `$XDG_DATA_HOME/applications`.
6. Task 1.1: also list every name activated on the private bus, diff `$XDG_RUNTIME_DIR`
   before/after, confirm new-window focus headless, measure start-up time, test with
   `--wayland` if `--headless` alone refuses.
7. `wait-window` bound as a flag with a default that survives a fontconfig cache rebuild.
8. Spec file (or numbered package doc) for `desktop-check`; a `docs/` line for the object and
   tool; a stale-run sweep; mention the object in the play header and the extension header.
