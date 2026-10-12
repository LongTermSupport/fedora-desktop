# Research: one recording mode (Phase 11) and a thin extension over a reloadable service (Phase 12)

Supporting document for [PLAN.md](PLAN.md) Phases 11 and 12. It records what the code
does today (read at the branch that holds Plan 00171's merge), so the tasks name real
files and functions. Owner decisions are listed in the plan as open tasks; this note
gives the reasoning behind each recommendation.

## 1. Phase 11: one recording mode

### 1.1 What exists today

Four recorders, chosen by two GSettings keys and one more switch:

| Mode                  | Chosen by                                                            | Code                                                    | Needs                                      |
| --------------------- | -------------------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------ |
| batch                 | `streaming-mode` false (the schema default)                          | `files/home/.local/bin/wsi` (1,430 lines of bash)       | `faster-whisper-transcribe` (play-written) |
| streaming, standard   | `streaming-mode` true, `streaming-startup-mode` `standard` (default) | `wsi-stream` `run_streaming` → `run_standard_streaming` | RealtimeSTT (and PyTorch through it)       |
| streaming, pre-buffer | `streaming-startup-mode` `pre-buffer`                                | `wsi-stream` `run_prebuffered_streaming`                | RealtimeSTT                                |
| streaming, server     | `streaming-startup-mode` `server`                                    | `wsi-stream` `run_server_mode` → `wsi-stream-server`    | faster-whisper only                        |

Inside server mode, `continuous-dictation` picks between the VAD loop-and-buffer (Phase 2)
and the old whole-clip path (`WholeClip` in `wsi-stream-server`, capped by `wsi-stream`'s
`STREAMING_MAX_SECONDS = 120` passed as `--timeout`). The owner runs server mode with
continuous dictation on. Article mode (`wsi-article`) already rides on the server's
continuous session and loads `wsi-stream` as a module for its socket helpers.

The one mode kept is therefore: `wsi-stream` server mode with continuous dictation, for
Insert, Ctrl+Insert, Alt+Insert and article mode alike.

### 1.2 Every caller of the modes to remove

Scripts (`files/home/.local/bin/`):

- `wsi`: the whole file goes. Its unique callers: the extension's `_launchFor()`
  (`script: 'wsi'` when `streaming-mode` is false), `scripts/test-wsi-stop-grace.bash` (drives
  it end to end) and `scripts/qa-stt-limits.bash` (its `MAX_RECORDING_SECONDS=30`).
- `faster-whisper-transcribe` and `faster-whisper-transcribe.py`: written by two tasks in
  `play-speech-to-text.yml` ("Create faster-whisper Shell Wrapper", "Create faster-whisper
  Python Script") and used only by `wsi` (`WHISPERFILE`). With them goes the play var
  `faster_whisper_model` and its `stt_model` default (no host var sets it here).
- `wsi-stream`: `run_streaming`, `run_standard_streaming`, `run_prebuffered_streaming`,
  `check_dependencies` (imports RealtimeSTT), `on_realtime_update`,
  `wait_for_final_realtime_passes`, `read_remaining_audio`, `FINAL_REALTIME_*`,
  `report_preview_fallback` (the tiny-preview fallback exists only in standard mode),
  `STREAMING_MAX_SECONDS` and the `--timeout` option, the `--pre-buffer` option,
  `dictation_limits()` (the `continuous-dictation` branch; `continuous_limits()` stays and
  becomes the only source). `main()` routes straight to server mode.
  `server_wanted_at_login()` stops reading `streaming-mode` and `streaming-startup-mode`
  and reads only `server-start-at-login`.
- `wsi-stream-server`: `WholeClip`, the `continuous` START flag and the
  "VAD required only with continuous" checks in the session constructor; a START is
  always continuous. Its idle `--timeout` stays (a different thing: the idle shutdown).
- `wsi-resolve-model`: `--mode batch|streaming` and `AUTO_WITHOUT_GPU` (small for batch,
  base for streaming) collapse to one choice, the server's (`base` without a GPU; with a
  GPU both modes already pick `distil-large-v3.5` / `large-v3-turbo`). Its comments about
  RealtimeSTT 0.3.104 pinning faster-whisper 1.1.1 go.
- `wsi-model-manager`: `AUTO_MODES = ('batch', 'streaming')` becomes one pick.
- `wsi-setting`: only its usage comment names `streaming-startup-mode`.

Extension (`extensions/speech-to-text@fedora-desktop/`):

- `extension.js`: `_streamingMode`, `_streamingStartupMode` and their load/changed
  handlers; `_launchFor()`'s `script` and `serverMode`; `_recorderCommand()`'s
  `--pre-buffer` and `--server-mode` arguments; `_continuousDictation()` (the countdown
  limit then always comes from `max-recording-minutes`); `_checkServerStatus()` and
  `_updateStatusLabel()`'s "only in server mode" gates (the warm dot shows whenever the
  server is up); `_discardRecording()`'s non-server branch and `RECORDER_CMDLINE`'s bare
  `wsi`; the dead `_claudeEnabled` (loaded, never consulted).
- `recorderLaunch.js`: `discardSignal()` always answers USR1 (the server's ABORT); the
  `KILL`-the-group branch goes.
- `panelSession.js`: `CAPTURING` (PREPARING meaning "already capturing" was true only of
  batch and pre-buffer; server mode never sends PREPARING, article mode does but Insert in
  an article goes to `_stopArticle`).
- `prefs.js`: the streaming group's `streaming-mode` switch and `streaming-startup-mode`
  combo, the `continuous-dictation` switch, the model subtitle "Small (batch) or Base
  (streaming)", the installed-model check's `mode` (line 256), and the `claude-enabled`
  switch if the owner retires it.
- Schema: `streaming-mode`, `streaming-startup-mode`, `continuous-dictation` (and
  `claude-enabled` if retired). See 1.4 for when they leave the schema.

Play (`playbooks/imports/optional/common/play-speech-to-text.yml`):

- "Install System Dependencies for RealtimeSTT" (`portaudio-devel`, `python3-devel`) and
  "Install RealtimeSTT for Streaming Mode" (`RealtimeSTT==1.0.0`): replaced by an
  uninstall (`state: absent`), kept until every host has run it once, then deleted.
- The faster-whisper pin comment ("Pinned to what RealtimeSTT 1.0.0 declares") is
  reworded: the pin now exists for the Silero VAD the server loads.
- "Copy WSI Script", the two faster-whisper-transcribe tasks: replaced by `state: absent`
  for the deployed files, so no stale `wsi` stays on `PATH`.
- "Verify The Auto Model Resolves": its `batch`/`streaming` loop becomes one call.
- The header comment and the "RealtimeSTT Streaming Mode" banner.

Tests and gates:

- `scripts/test-wsi-stop-grace.bash`: most of it drives batch `wsi`; those cases go. The
  rest (wsi-setting, keep-warm schema keys, "the play deploys every helper", the Python
  suite) stays, and the file is renamed for what it then tests.
- `tests/speech_to_text/test_prebuffer_stop.py`: deleted.
  `test_stop_grace.py`: `FinalPassesTest` and `DrainTest` (RealtimeSTT and pre-buffer
  units) go; `GracefulStop`, signal handler and grace-reader tests stay.
  `test_keep_warm.py`: the `server_wanted_at_login` cases on the two retired keys.
  `test_resolve_model.py`: the per-mode cases. `test_continuous_session.py`:
  `WholeClipTest` and `continuous=False` starts. `test_server_client.py`: the
  `continuous-dictation` false case (a START with `continuous: False` and the 120 s cap);
  the server's START protocol tests in `test_continuous_session.py` and
  `test_article_client.py` drop the `continuous` field.
- `tests/extensions/test-stt-recorder-launch.mjs` (`discardSignal` KILL case),
  `test-stt-panel-session.mjs` (CAPTURING cases), `test-stt-prefs-installed-model.mjs`
  (the mode).
- `scripts/qa-stt-limits.bash`: the rules for `wsi`'s 30 and `wsi-stream`'s 120 go (a
  control run against the old files must still fail the remaining rules).
- `.semgrep/speech-to-text.js` names `streaming-startup-mode` only as fixture text for a
  label rule; it is not a caller and can stay.

Docs: `docs/features/speech-to-text.md` (Processing Modes, Streaming Mode Setup, Recording
Limits, Continuous Dictation, Batch Mode (Default), Streaming Mode (Real-Time),
Command-Line Usage, Streaming Mode Issues, Architecture, Dependencies),
`docs/playbooks.md` (RealtimeSTT line), `CLAUDE/GnomeShell.md` (the wsi/wsi-stream
architecture paragraph), `CLAUDE/QA.md` (the two gate descriptions and the RealtimeSTT
example), `extensions/CLAUDE.md` (example architecture). Completed plans are history and
are not edited.

Not touched: Claude post-processing (`--claude-process` already works in server mode),
paste settings, `dictation-paste-interval-seconds`, the stop grace, keep-warm keys.

### 1.3 What the owner gives up

- No second recorder if the warm server breaks: a server that cannot load its model
  (GPU memory, a missing model) means no dictation until it is fixed. Every failure is
  already loud (Phase 2, Plan 00156), so this is a visible outage, never a silent one.
- The warm server holds the model in memory while it runs; its idle shutdown stays, so
  the first Insert after an idle shutdown waits for a model load (Phase 8's keep-warm
  switches remain the answer).
- Without a GPU, `auto` loads `base` (server mode's pick today), not batch's `small`.
- RealtimeSTT's live preview of text while speaking is gone. Server mode never had it.

### 1.4 Migration for Phase 11

The extension's JavaScript is loaded only at login, while the scripts change at the play
run. Between the play run and the logout, the old extension still launches
`wsi-stream --server-mode`. So `wsi-stream` keeps accepting `--server-mode` as an
ignored flag until Phase 12 replaces the launcher (then it is removed). The retired
GSettings keys stay in the schema, unread, until the next deploy that is followed by a
logout anyway (Phase 12's switch-over), and that deploy resets their stored values
(`community.general.dconf` `state: absent` on `/org/gnome/shell/extensions/speech-to-text/<key>`),
so no orphan value is left in dconf. Removing a key from the compiled schema under a
running Shell that still reads it is the one ordering to avoid.

RealtimeSTT pulled in PyTorch and other large packages; `pip uninstall` removes only the
package named. Whether the dependencies go too is an owner decision, after a read-only
probe (`pip show` `Required-by` for each) shows nothing else installed for the user needs
them.

## 2. Phase 12: the extension as a thin layer over a reloadable service

### 2.1 Why

GNOME Shell loads extension JavaScript only at login (Wayland; `extensions/CLAUDE.md`).
About 2,000 lines run inside it: `extension.js` (1,449), `panelSession.js` (490, Plan
00171's state machine), `panelLook.js`, `recorderLaunch.js`, `pasteTarget.js`,
`focusOutline.js`. Every logic fix there costs a logout before it can be tried, and the
extension's own rule ("keep extension code thin") is not met.

### 2.2 What needs the Shell, and what can move

| Duty today                                                                                                       | Needs the Shell?                                                | After Phase 12                                                              |
| ---------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------------------- |
| Key grabs: Insert, Ctrl+Insert, Alt+Insert (`Main.wm.addKeybinding`), Escape added only while the mic is open    | yes                                                             | extension; each press is a D-Bus call to the service                        |
| Panel indicator, icon, elapsed/backlog label, flashing, article spinner, menu                                    | yes (St, PanelMenu)                                             | extension draws what the service publishes                                  |
| What each state looks like (`panelLook.js`), the label text (`_countdownText`, `_secondsLeft`)                   | no                                                              | service; it publishes the look, so a new state or colour needs no logout    |
| State machine: Insert/Escape per state, queued start, cancel, unowned recorder, exit-settle (`panelSession.js`)  | no                                                              | service                                                                     |
| Recorder launch (argv from settings, setsid, child exit watch, signals; `recorderLaunch.js`, `_recorderCommand`) | no                                                              | service                                                                     |
| Session tokens; taking `StateChanged`/`Error`/`Progress` only from the followed recorder                         | no                                                              | service subscribes to the recorders' signals (recorders unchanged)          |
| Live-recorder PID file check (`_readLiveRecorderPid`)                                                            | no                                                              | service                                                                     |
| Warm-server dot (5 s poll of PID file and socket)                                                                | no                                                              | service publishes `ServerWarm`                                              |
| Panel notifications (`Main.notify`: errors, "Ready", "middle-click to paste", busy article)                      | no (freedesktop Notifications reach the same Shell)             | service                                                                     |
| Paste target pin at the press (`pasteTarget.js`, `MetaWindow` and its `unmanaged` signal)                        | yes                                                             | extension                                                                   |
| `PasteKey` D-Bus answer (window WM class, terminal by desktop category via `Shell.WindowTracker`, Ctrl+V lists)  | yes                                                             | extension, unchanged interface (the recorders call it on `org.gnome.Shell`) |
| Giving the pinned window focus back (`Main.activateWindow`)                                                      | yes                                                             | extension                                                                   |
| Focus outline on the pinned window (`focusOutline.js`)                                                           | yes                                                             | extension; shown while the service says a paste can come                    |
| Menu actions: copy last transcription, open log, model manager, server manager                                   | no                                                              | service methods (the menu item only calls)                                  |
| Settings window (`openPreferences`, `prefs.js`)                                                                  | `prefs.js` runs in its own process, started when Settings opens | stays as is                                                                 |
| Microphone shortfall report (`microphone_shortfall`) and the recorders' own notifications                        | no                                                              | stay in `wsi-stream` for now; may move into the service later               |

The "mic-health reporting" the Phase 12 draft gave the service is in `wsi-stream`, not the
extension; it moves only if `wsi-stream`'s duties later move into the service.

What stays in the extension is key forwarding, drawing, the paste pin and its D-Bus
answer, the outline, window activation, and watching the service's name. That is small
and stable: it changes only when the D-Bus contract changes.

### 2.3 D-Bus contract (sketch)

The service owns the well-known name `org.fedoradesktop.SpeechToText` and exports
`/org/fedoradesktop/SpeechToText/Session`. The XML is kept in the repo as the one copy
both sides are checked against.

```xml
<node>
  <interface name="org.fedoradesktop.SpeechToText.Session">
    <!-- kind: dictation | claude; style: '' | corporate | natural.
         press_id: chosen by the extension, which remembers the window focused at this
         press. with_shift: the paste key for that window, the recorder's fallback if
         PasteKey cannot answer. action: started | stopping | cancelling | queued |
         unqueued | stopping-unowned | refused. -->
    <method name="Press">
      <arg type="s" direction="in" name="kind"/>
      <arg type="s" direction="in" name="style"/>
      <arg type="u" direction="in" name="press_id"/>
      <arg type="b" direction="in" name="with_shift"/>
      <arg type="s" direction="out" name="action"/>
    </method>
    <method name="Escape"><arg type="s" direction="out" name="action"/></method>
    <method name="CreateArticle"><arg type="s" direction="out" name="action"/></method>
    <!-- copy-last-transcription | open-log | model-manager | server-manager -->
    <method name="MenuAction"><arg type="s" direction="in" name="name"/></method>

    <property name="InterfaceVersion" type="u" access="read"/>
    <property name="State" type="s" access="read"/>       <!-- the panelLook.js vocabulary, plus SERVICE states -->
    <property name="Kind" type="s" access="read"/>        <!-- '' | dictation | claude | article -->
    <property name="PressId" type="u" access="read"/>     <!-- the press whose session is live; 0 none -->
    <property name="MicOpen" type="b" access="read"/>     <!-- Escape grabbed while true -->
    <property name="PasteCanCome" type="b" access="read"/><!-- outline shown while true -->
    <property name="Icon" type="s" access="read"/>        <!-- theme icon name, or an absolute file path -->
    <property name="IconColour" type="s" access="read"/>
    <property name="Label" type="s" access="read"/>       <!-- '' shows the icon; else the badge text -->
    <property name="LabelColour" type="s" access="read"/>
    <property name="LabelFlash" type="b" access="read"/>
    <property name="LabelSpinner" type="b" access="read"/><!-- the article's turning spinner -->
    <property name="StatusText" type="s" access="read"/>  <!-- the menu's status line -->
    <property name="ServerWarm" type="b" access="read"/>
  </interface>
</node>
```

All property changes go out as `org.freedesktop.DBus.Properties.PropertiesChanged`.
Elapsed time is a label change once a second while recording (the recorder's `Progress`
signal already arrives at that rate); the spinner is animated by the extension from the
flag, so nothing is sent twice a second.

Unchanged: the recorders still broadcast `StateChanged`, `Error` and `Progress` (with the
session token) on `/org/fedoradesktop/SpeechToText`; the service, not the extension,
subscribes. The recorders still call `PasteKey` on the extension. So Phase 12 changes no
recorder.

**The press id.** The extension cannot know whether a press will start, stop or queue
(the service decides). So each press carries an id, and the extension remembers the
window focused at that press. The service publishes `PressId` when the session that
press asked for starts (a queued start can be seconds later); the extension then pins
that press's window. A press answered with anything but `started` or `queued` forgets
its window. This keeps Plan 00171's rule that a queued start goes to the window
focused at the press, not at the start.

**Service down is a state of its own.** The extension watches the name
(`Gio.bus_watch_name`). On name lost it draws a "service down" look (not the ready
microphone), drops Escape, hides the outline, and an Insert shows a notification that
the speech-to-text service is not running, naming
`journalctl --user -u <unit> --no-pager`. On name appeared it reads every property
(`GetAll`) and draws them. A `Press` call that gets no reply within a short timeout is
drawn as an error ("service not answering"), never ready.

**Version skew.** After a play run the service is new while the extension is the one
loaded at login. `InterfaceVersion` is bumped only by a change the loaded extension cannot
use; the extension draws "panel out of date: log out to load it" when the number is not
its own. Additive changes (a new state, a new look, a new menu action name the extension
already passes through) do not bump it. A new published state is already safe: an
unknown state is drawn as please-wait, never ready (`panelLook.js`'s rule, kept).

### 2.4 Process supervision and cgroups

Today a recorder is a child of GNOME Shell, so it lives in the Shell's cgroup, and the
server that `wsi-stream` starts (`start_new_session=True`: a new session, the same
cgroup) does too. If the service spawned recorders directly, `systemctl --user restart`
would kill every process in the service's cgroup (the default `KillMode=control-group`),
including a dictation in progress and a warm server it started.

So each recorder is started in a transient scope of its own:
`systemd-run --user --scope --collect --unit=<name>-<token> -- setsid <recorder> ...`.
`systemd-run --scope` registers the scope for its own PID and then runs the command in
that process, so the recorder stays the service's child (its exit is seen directly, as
`Gio.Subprocess.wait_async` sees it today) but outside the service's cgroup. That this
holds (same PID, child of the service, survives a service restart) is the first
integration test to write, not an assumption to build on. Rejected: `KillMode=process`
on the service (systemd discourages it, and it leaves children in a cgroup a restart
then reuses).

A dictation still running across a service restart is found by the new instance through
the recorders' PID file, as a recorder the panel did not start (Plan 00171's `UNOWNED`:
red icon, Insert stops it with INT, the text is kept). Re-adopting it as the panel's own
(persisting the session token, watching the PID with `pidfd_open`) is possible later;
owner decision in the plan.

### 2.5 The recorders' environment

A recorder started by the service gets the systemd user manager's environment, not GNOME
Shell's. `wsi-stream` reads `XDG_SESSION_TYPE` (clipboard path), `LANG` (language
`system`), `WHISPER_MODEL`; `wl-copy` and the GTK4 `wsi-article-window` need
`WAYLAND_DISPLAY`; `ydotool` needs its socket; `wsi-claude-process` needs `claude` on
`PATH`. GNOME imports some of these into the user manager at login, but which ones is a
fact to establish on the host before building: a read-only triage leg comparing
`systemctl --user show-environment` with the Shell's `/proc/<pid>/environ` for exactly
those names. Any gap is fixed in IaC (the unit's `PassEnvironment=`/`Environment=`, or
the service setting it on the launch), never by hand.

### 2.6 Settings

The service reads the extension's GSettings at each press, as the extension does now:
fresh values, no change handlers. The schema is compiled in the extension's own
`schemas/` directory, not system-wide, so the reader needs a schema source from that
directory (`gio`'s `SettingsSchemaSource::from_directory` in Rust; `wsi-setting` does the
same with `gsettings --schemadir`).

### 2.7 Language: Rust (recommended) or Python

Rust fits the owner's direction for long-running services (Plan 00174: "where efficiency
matters (a long-running service, a root daemon), write it in Rust"), the state machine is
new code in either language (it moves out of JavaScript), and Rust's types make the
state × event table checkable by the compiler. Crates: `zbus` (serving the interface,
watching signals, calling Notifications), `tokio` (child processes, timers, signals),
`gio` only to read GSettings, and `rustix` or `nix` to send signals without a `kill`
subprocess. Build needs `glib2-devel` on the host for `gio`.

Costs: it waits for Plan 00174's Phase 2 (`tooling-rust/`, `rust-qa-ci` in `qa-all.bash`,
the build-on-host deploy pattern), which Plan 00155's key mapper proves first; every
logic change is a `cargo build --release` in the play (incremental after the first); the
48 Node cases of `test-stt-panel-session.mjs` are rewritten, not moved.

Alternative: Python with PyGObject (`Gio.DBus`, `GLib` timeouts, `Gio.Subprocess`). The
JavaScript ports nearly line for line, the tests port to `unittest` beside the existing
`tests/speech_to_text/`, no build step, no wait on Plan 00174, and the same language as
`wsi-stream`, whose duties may later move into the service. Against it: not the owner's
stated direction, and a Python interpreter stays resident.

Either way `wsi-stream-server` stays Python (faster-whisper and Silero are Python; its
time is in the model) and `wsi-stream` is not rewritten in the same step.

### 2.8 Deployment by Ansible (`play-speech-to-text.yml`)

- Build and install the binary by Plan 00174's pattern (release build on the host with
  the toolchain from `play-rust-dev.yml`, installed root-owned under `/usr/local/bin`,
  rebuilt only when the source changes). If Python: deployed to `~/.local/bin` like the
  other scripts.
- A user unit in `files/home/.config/systemd/user/`, deployed and enabled like
  `wsi-stream-server-at-login.service`:
  `Type=dbus`, `BusName=org.fedoradesktop.SpeechToText`, `PartOf=` and
  `WantedBy=graphical-session.target`, `Restart=on-failure` with a start limit (a crash
  loop stops and shows as "service down").
- A handler, notified by the binary and unit tasks: `daemon_reload`, then restart the
  unit (with `XDG_RUNTIME_DIR` set as the existing user-unit tasks do), then read back
  that it is active and owns its name. A failed restart fails the play.
- Extension files removed from the source are also removed from the deployed directory
  (`state: absent`): the "Copy Extension Files" loop only adds, so a deleted
  `panelSession.js` would otherwise stay on disk.
- The retired Phase 11 keys are reset in dconf in the same deploy (1.4).

### 2.9 Migration and rollback

1. Phase 11 first: one launch path to port instead of four.
2. Contract and service, deployed while the old extension is still loaded. Harmless: it
   subscribes to the recorders' signals and publishes, and nothing presses it. This
   proves at login, restart and name ownership on the host before anything depends on it.
3. The thin extension, in one deploy and one logout. No in-Shell fallback state machine:
   two paths is what the owner wants gone, and "service down" is shown as such.
4. Prove the goal: a service-only change (a notification's wording, say) shipped with a
   play run and a restart, seen with no logout.

Rollback: revert the extension's switch-over commit, run the play, log out. The service
can stay deployed (the old extension ignores it). Before step 3, rollback is just
disabling the unit by reverting its commit.

Considered and rejected: a "shadow" period where the service runs its state machine
beside the old extension to compare decisions. The keys are grabbed by the old
extension, so the service never sees the presses that drive most transitions.

### 2.10 Testing

- Pure state machine, no I/O: events in (press, escape, article, recorder state with
  token, recorder exit, timer fired, a live recorder found at start) and actions out
  (spawn, signal, publish, notify, start or cancel a timer), with a fake clock. Every
  case of `tests/extensions/test-stt-panel-session.mjs` (48) is carried over, Plan 00171's
  race cases first, and each is run red before it passes.
- The look table (`test-stt-panel-look.mjs`, 9) moves with `panelLook.js`.
- Service over D-Bus: a private `dbus-daemon --session` per test, the real service
  binary, stub recorders that emit the real signals; assert the sequence of
  `PropertiesChanged`. Includes the cgroup case (2.4): a stub recorder survives a
  restart of the service under a user manager (host-only if CI has none; named
  NOT ESTABLISHABLE by the plan's acceptance otherwise).
- Contract: the XML file in the repo is compared with the service's `Introspect` reply
  in QA, and the extension's proxy is built from the same file.
- Extension (Node, existing gi stubs): drawing from properties, key forwarding, press id
  to pin, name lost (service down), no reply (not answering), version mismatch.
  `test-stt-paste-target.mjs` and `test-stt-focus-outline.mjs` stay as they are.
- Host acceptance: every Insert, Escape, Claude and article path; `systemctl --user stop`
  shows service down and Insert says so; a crash restarts it and the icon recovers; a
  restart during a dictation behaves as the owner chose.

### 2.11 Risks

| Risk                                                                                                    | Mitigation                                                                                     |
| ------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| A service restart kills a dictation or the warm server                                                  | recorders in their own scopes (2.4); a test restarts the service under a live stub recorder    |
| Recorders lose an environment variable the Shell gave them (clipboard, paste, article window, `claude`) | triage leg first (2.5); fix in the unit, never by hand                                         |
| A reloaded service and the login-time extension disagree on the contract                                | `InterfaceVersion`; additive changes only; "panel out of date" state                           |
| Insert feels ignored because the service is slow or hung                                                | `Press` answers at once (the state machine never blocks); a reply timeout is drawn as an error |
| The icon claims ready while nothing is listening                                                        | name watcher; service down is its own look                                                     |
| The Rust prerequisites (Plans 00174, 00155) delay Phase 12                                              | owner may choose Python (2.7); Phases 11 and 10 do not wait on it                              |
| One mode means no fallback recorder                                                                     | owner decision; every server failure is already loud                                           |
| Stale deployed extension files after the move                                                           | `state: absent` for each removed file                                                          |
