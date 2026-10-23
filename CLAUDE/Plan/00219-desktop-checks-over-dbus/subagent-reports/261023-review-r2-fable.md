# Plan 00219 review, round 2: security and feasibility

Reviewed: `PLAN.md` and `DESIGN.md` at b333ac7e4 (the round-1 fix commit), against
`subagent-reports/261023-review-r1-fable.md`. GNOME paths are under
`untracked/gnome-shell/50.5/js-extracted/org/gnome/shell/`. One empirical probe was run in
this container (a real `dbus-daemon`, `untracked/scratch/dbus-probe.bash`); its results are
in section 2.

## 1. Round-1 fixes: applied?

| Round-1 item                                                | Status                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ----------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| BLOCKING: bus daemon in the full private environment        | Applied. §4 step 2 builds the environment once (WAYLAND_DISPLAY chosen first, GDK_BACKEND=wayland, XDG\_\* private, DISPLAY/WAYLAND_SOCKET/DBUS_SESSION_BUS_ADDRESS removed) and step 3 starts the daemon in it; step 8 gives CMD the same plus the bus address. §5 "apps reaching the owner's session" row and §7 red run both cite it. Task 1.1 proves it with a D-Bus-activated editor via `/proc/<pid>/environ`. Correct. |
| S1 Looking Glass unsafe-mode condition; second tool guard   | Applied: §4 "The one condition on GNOME's half"; guard 1 (socket under `$XDG_RUNTIME_DIR/desktop-check/`) and guard 2 (Eval `is_headless && unsafe_mode`). Soundness of guard 1: section 2.                                                                                                                                                                                                                                   |
| S2 Flatpak row reworded                                     | Applied, accurate (`--talk-name=org.gnome.Shell` or `--socket=session-bus`).                                                                                                                                                                                                                                                                                                                                                  |
| S3 window source named                                      | Applied: `global.get_window_actors()`, with `_isEligibleWindow`'s filter.                                                                                                                                                                                                                                                                                                                                                     |
| S4 absolute screenshot path; shell log on readiness timeout | Applied (§4 table row; step 7; Task 3.1 tests a relative path refused).                                                                                                                                                                                                                                                                                                                                                       |
| S5 drop `--link`                                            | Applied; §4 step 5 and §7 green run have the in-shell script symlink into the private `XDG_DATA_HOME/applications/`.                                                                                                                                                                                                                                                                                                          |
| S6 wider triage                                             | Applied: Task 1.1 lists activated names, diffs `$XDG_RUNTIME_DIR`, checks focus, measures start-up, dconf checksum, default handler, `--wayland` fallback (step 6).                                                                                                                                                                                                                                                           |
| S7 `wait-window` bound as a flag                            | Applied: `--timeout`, default 60 s.                                                                                                                                                                                                                                                                                                                                                                                           |
| S8 SPEC, docs line, stale sweep, header mentions            | Applied: §4 end + Task 3.1 (SPEC-desktop-check.md); Task 4.2 (docs); step 1 (sweep); Task 3.2 (headers). `headless` field marked informational (§3).                                                                                                                                                                                                                                                                          |
| Seeding commands in the private env                         | Applied (step 2 lists "the seeding commands").                                                                                                                                                                                                                                                                                                                                                                                |

Nothing omitted.

## 2. Did the fixes introduce a defect? The bus-socket-path guard, probed

Run in this container with the real `dbus-daemon` (1.16-era, Fedora): a daemon started as
`dbus-daemon --session --nofork --print-address=3 --address=unix:path=<dir>/desktop-check/1/bus`
with `WAYLAND_DISPLAY=desktop-check-1 GDK_BACKEND=wayland XDG_*=<private>` and a test
service activated by `StartServiceByName`:

- **`--address=unix:path=...` works with `--session`** (the CLI address overrides
  `session.conf`'s `unix:tmpdir=/tmp` listen). The socket appears at the given path.
- **The printed address is `unix:path=<dir>/desktop-check/1/bus,guid=<32 hex>`.** So
  `DBUS_SESSION_BUS_ADDRESS` in CMD (and so in `desktop-check activate-workspace`) carries the
  path, with a `,guid=` suffix: the guard must parse the address (split `;` then `,`, take the
  `path` key, D-Bus-unescape), not compare the whole string or substring-match.
- **The activated service inherits the daemon's environment exactly, plus
  `DBUS_SESSION_BUS_ADDRESS`, `DBUS_STARTER_ADDRESS` and `DBUS_STARTER_BUS_TYPE=session` set
  by the daemon.** It saw `WAYLAND_DISPLAY=desktop-check-1`, `GDK_BACKEND=wayland` and the
  private `XDG_*`. The round-1 fix is therefore the right fix, and the daemon needs no bus
  address of its own (step 2's "removed" is right).
- **Socket path length.** My first attempt failed with `Failed to start message bus: Socket name too long`: `sun_path` is 108 bytes. `/run/user/<uid>/desktop-check/<pid>/bus` is ~40,
  so the design is fine on a normal host, but the tool must check the length and fail loudly
  (R-fail-fast) rather than letting the daemon die with an empty printed address.
- **The socket mode is 0777**; the 0700 runtime dir is the protection, as §5 says. Fine.
- **A set-but-empty `DISPLAY` is not "removed".** My probe passed `DISPLAY=''` and the child
  saw `DISPLAY=` present. GTK treats an empty `DISPLAY` as unset in practice, but the design
  says removed; the Go code must delete the key (`os.Environ()` filtered), and the Task 1.1
  environ check should assert the key is **absent**, not empty. Wording only; the design's
  intent is correct.
- Everything else in the parent's environment is inherited (the probe child saw this
  container's whole environment). That is intended (PATH, HOME, LANG, XDG_RUNTIME_DIR), but the
  SPEC should state the environment as inherit-then-override with an explicit removal list,
  so a future `SSH_AUTH_SOCK`/`GNOME_SETUP_DISPLAY`/`XAUTHORITY`-style variable is a decision,
  not an accident. `XAUTHORITY` and `GNOME_SETUP_DISPLAY` should join the removal list now
  (both are X11 routes; harmless with `GDK_BACKEND=wayland`, cheap to remove).

Guard 1 soundness beyond the probe:

- Guard 1 reads `$XDG_RUNTIME_DIR` from the tool's own environment and `DBUS_SESSION_BUS_ADDRESS`
  from CMD's; both are inherited unchanged through `shell`, so the check sees the path it
  created. On the real session the address is `unix:path=/run/user/<uid>/bus`, which is not
  under `desktop-check/`, so it refuses. Correct.
- A same-user process could symlink `desktop-check/x/bus` to the real bus socket; guard 2
  then refuses (`is_headless()` false). The two guards are independent as claimed. Cheap
  hardening: `Lstat` the path and refuse a symlink.
- Also accept only a single address with a `unix:path=` key; refuse `abstract=`, `tmpdir=`,
  `tcp:` or an address list. The tool creates the address, so anything else is a bug.
- `dbus-daemon` also sets `DBUS_SESSION_BUS_ADDRESS` in the children it activates (probe
  above), so an activated app's own child processes (an editor spawning a helper) stay on the
  private bus. Good.

Triage ordering (Task 1.1 before the facts object exists): sound. The test shell is in unsafe
mode, so `org.gnome.Shell.Introspect.GetWindows` is open to the triage script
(`misc/util.js` `checkInvocation` returns early), which lists the apps' windows there; the
owner's shell is proved untouched by the launched apps' environment (`WAYLAND_DISPLAY`
private, `DISPLAY` absent, `GDK_BACKEND=wayland`), which needs no facts object, as the task
says. One gap in the task text: **Introspect does not return a pid** (no `pid` in
`misc/introspect.js`), and a D-Bus-activated editor's pid is the daemon's child, unknown to
the script; `pgrep gnome-text-editor` could pick up the owner's editor. Get the pid from the
private bus: `GetConnectionUnixProcessID('org.gnome.TextEditor')` on `org.freedesktop.DBus`.
State that in Task 1.1 so the triage script does not fall into the `pgrep` trap.

## 3. Fresh read of the whole design

Security

- Guards, Eval semantics, Screenshot signature and sender checks: as round 1 verified
  (`ui/shellDBus.js` `EvalAsync`, `misc/util.js` `checkInvocation`, `ui/screenshot.js`
  `ScreenshotWindowAsync`/`_createScreenshot`). The guard expression
  `global.backend.is_headless() && global.context.unsafe_mode` serialises to `'true'`; in an
  owner shell put in unsafe mode it yields `(true, 'false')`, still refused. Correct.
- `GetWindows` content and reach: unchanged from round 1, acceptable.
- Services activated on the private bus come from `/usr/share/dbus-1/services` plus the
  **private** `XDG_DATA_HOME/dbus-1/services`, so the owner's own `~/.local/share/dbus-1/services`
  are not reachable from the test shell. Good; worth one line in §5.
- Teardown (step 9) stops the shell and the bus but not the apps the daemon activated or CMD
  launched. On Wayland disconnect GTK apps exit, and dconf-service exits on bus loss, so in
  practice nothing survives, but a process that ignores the compositor loss (a terminal's
  `sleep` child from §8, a stuck app) stays behind with the private env, harmless but
  untracked. Suggest: start the daemon, the shell and CMD in one new process group (or
  session) and kill the group at the end of step 9, then report any survivor. Not blocking.
- `pgrep`-style pid reuse in the stale sweep (step 1): a dead pid's directory may be kept if
  the pid was reused; harmless (left in tmpfs), and the next matching run sweeps it.

Feasibility

- `dbus-daemon` exists here (`/usr/bin/dbus-daemon`, `dbus-run-session`) and on Fedora
  Workstation as the `dbus-daemon` package (at-spi2-core needs it for the a11y bus); Task 1.1
  and 3.2 cover the host. `gdbus` is **not** installed in this container; the triage runs on the
  host where glib2 ships it, but the Go client should speak D-Bus itself (`godbus`) rather than
  shell out to `gdbus`/`busctl`, which also keeps the execx seam to real processes only.
- Expect these extra activations on the private bus and let Task 1.1 record them: `org.a11y.Bus`
  (GTK asks for it; it spawns a second a11y `dbus-daemon`), `org.gtk.vfs.Daemon` (GIO; a second
  `gvfsd` will try to use the shared `$XDG_RUNTIME_DIR/gvfsd` and `gvfs` fuse mount, the one
  place a private-bus service touches a shared runtime path). Setting `GIO_USE_VFS=local` and
  `GTK_A11Y=none`/`NO_AT_BRIDGE=1` in the private environment removes both and makes the
  runtime-dir diff cleaner; a consumer that needs gvfs can unset them. Recommend adding them
  to step 2 unless triage shows they are not activated.
- gnome-shell with no `org.gnome.SessionManager`, no systemd user activation and no PipeWire:
  it logs and continues (GNOME's own test harness runs this way); `ScreenshotWindow` does not
  need PipeWire. libgnome-volume-control will connect to the owner's PipeWire-Pulse through
  the shared runtime dir, read-only unless a volume key is pressed; note in the §5 table.
- `--headless` without `--wayland`: in mutter the headless backend implies a Wayland
  compositor, so `--wayland` should be unnecessary; the design already keeps it as a triage
  fallback. Fine.
- `activate-workspace N` out of range: `get_workspace_by_index` returns null, Eval returns
  `(false, '<TypeError>')`; the tool must report that as failure, not success (Task 3.1 test).
- The 00198 red/green and 00185 legs: unchanged from round 1 and sound.

## Verdict

**READY.** No blocking fix remains; the round-1 blocker is correctly fixed and the second
guard is sound once the address is parsed rather than string-matched.

### Blocking fixes

None.

### Suggestions

1. Guard 1 implementation notes for SPEC/Task 3.1: parse `DBUS_SESSION_BUS_ADDRESS` (single
   address, `unix:path=` key, strip `,guid=`), `Lstat` and refuse a symlink, check the socket
   path is under 108 bytes before starting the daemon and fail loudly if not.
2. Step 2: state the environment as inherit-then-override with an explicit removal list;
   remove (delete, not blank) `DISPLAY`, `WAYLAND_SOCKET`, `DBUS_SESSION_BUS_ADDRESS`,
   `XAUTHORITY`, `GNOME_SETUP_DISPLAY`; consider `GIO_USE_VFS=local` and `GTK_A11Y=none`.
   Task 1.1's environ check asserts `DISPLAY` is absent.
3. Task 1.1: obtain the activated editor's pid with `GetConnectionUnixProcessID` on the private
   bus (Introspect carries no pid; `pgrep` can hit the owner's editor).
4. Step 9: run daemon, shell and CMD in one process group and kill the group at teardown;
   report survivors.
5. §5 table: one row each for "owner's user service files are not visible to the private bus"
   and "the shell's audio control reaches the owner's PipeWire-Pulse (read-only)".
6. Task 3.1: `activate-workspace` out-of-range index is reported as failure from Eval's
   `(false, msg)`; the client uses a Go D-Bus library rather than `gdbus`.
