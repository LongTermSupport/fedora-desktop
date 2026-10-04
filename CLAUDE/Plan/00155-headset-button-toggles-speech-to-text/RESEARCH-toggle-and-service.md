# Plan 00155 research: the toggle entry point and the user-service pattern

Read from the repository when the plan was filed. Linked from `PLAN.md` (F7, F8, F9, D4,
Task 4.2).

## The toggle entry point (F8, F9)

wsi has no toggle of its own. A second `wsi` started while one records refuses with
exit 1 (the EXT-03 PID-file guard in `files/home/.local/bin/wsi`); it does not stop the
first.

The toggle is `_launchWSI()` in `extensions/speech-to-text@fedora-desktop/extension.js`,
the callback of the `toggle-recording` keybinding (Insert by default). It does the whole
toggle: it ignores a press while a launch is pending (the debounce), stops the recording
when the panel state is `RECORDING`, `STOPPING`, `PREPARING` or `TRANSCRIBING`
(`_stopRecording()` sends SIGTERM to the PID in `/dev/shm/stt-recording-$USER.pid`,
which wsi's `on_term` turns into the stop grace), and otherwise spawns `wsi` or
`wsi-stream` with flags built from the extension's settings (auto-paste, streaming mode
and startup mode, language, model, notifications).

The extension exports **no D-Bus method**: on `org.fedoradesktop.SpeechToText` it only
subscribes to the `StateChanged`, `Error` and progress signals that the scripts emit. So
"call the toggle" needs a decision (D4) before the listener can be written.

## The user-service pattern to follow (F7)

`play-speech-to-text.yml` already deploys one systemd user service,
`wsi-stream-server-at-login.service`, from `files/home/.config/systemd/user/`. It is
`PartOf=` and `WantedBy=graphical-session.target`, and the play then resolves the session
user's uid with `getent` (never defaulted), enables the unit with `XDG_RUNTIME_DIR` set,
runs a separate `daemon_reload`, and reads the live manager back with
`systemctl --user list-dependencies graphical-session.target`, failing if the unit is not
in the graph. The new unit follows the same steps.

Helper modules are deployed the way `play-container-watch.yml` deploys
`helpers.containerwatch`: an explicit module list into a library directory, plus a thin
`~/.local/bin` wrapper.
