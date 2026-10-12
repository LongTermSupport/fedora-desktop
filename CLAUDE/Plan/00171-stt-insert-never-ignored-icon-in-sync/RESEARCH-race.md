# Plan 00171 research: why Insert was swallowed

Read-only code research plus the host's own debug log, checked line by line before
filing. Line numbers are as of the commit that filed this plan.

## The incident (debug.log, server mode, continuous dictation, auto-paste)

Extension lines are UTC.

```
08:10:51.126Z [EXT] State: TRANSCRIBING      dictation A stopped (orange dots)
08:10:53.590Z [EXT] State: SUCCESS           A pasted (green mic)
08:10:53.966Z [EXT] INSERT KEY PRESSED       376 ms later: SUCCESS is not "active", so a new client B is spawned
08:10:54.304Z [EXT] State: RECORDING         B: START accepted by the server, mic open
08:10:54.587Z [EXT] State: IDLE              A: wsi-stream:1494, after time.sleep(1)
08:10:54.596Z [EXT] State: IDLE              A: atexit ensure_stop_sent (also unlinks the PID file)
08:10:54.603Z [EXT] State: IDLE              A: atexit cleanup_on_exit (also unlinks the PID file)
08:10:57.163Z [EXT] INSERT KEY PRESSED       extension thinks IDLE: spawns client C
08:10:57.518Z [EXT] State: RECORDING         C announces RECORDING before it sends START
08:10:57.534Z [EXT] State: ERROR             server: "Recording already active"
08:10:57.536Z [EXT] State: IDLE              C's atexit IDLEs wipe the red icon at once
```

The same happened at 08:11:04 and 08:11:49. B kept recording until it was discarded by
hand (SIGUSR1).

The press was **after** the orange dots, not during them. During STOPPING/TRANSCRIBING
an Insert goes to `_stopRecording()` and never spawns a client.

## Root causes, each confirmed in code

1. **State signals carry no identity.** `StateChanged(s)` holds only the state string.
   The subscription accepts any sender (`extension.js:478-496`), and each signal comes
   from a throwaway `gdbus emit` process (`wsi-stream:354-364`). The extension keeps
   one global `_currentState` (`extension.js:71`). So the IDLE signals that A sends as
   it finishes overwrite B's RECORDING.
2. **The old client keeps signalling after its terminal state.** After SUCCESS,
   `run_server_mode` sleeps 1 s and sends IDLE (`wsi-stream:1493-1494`). Both atexit
   handlers send IDLE again (`:1270-1274`, `:309`). Batch `wsi` does the same: a
   delayed `(sleep 2 && gdbus emit … IDLE) &` (`wsi:1375-1378`) and an IDLE in
   `cleanup`.
3. **One shared PID file, deleted without checking whose it is.** The file is
   `/dev/shm/stt-recording-$USER.pid` (`wsi-stream:42`). It is written at `:1286` and
   unlinked unconditionally at `:1263-1268`, `:317-322` and, on a rejected START,
   `:1331-1332`. `wsi:376` is the same. So A, then C, delete B's PID file, and the
   extension's stop path loses B's PID (`extension.js:1242-1264`). Escape needs that
   PID (`:1280-1283`).
4. **A rejected START is invisible.** The server replies
   `{"status":"error","message":"Recording already active"}`, or a "still finishing a
   transcription" message while the previous dictation drains
   (`wsi-stream-server:1016-1028`), and logs nothing. The client sends RECORDING
   *before* START, then ERROR, then IDLE twice, all within about 15 ms. The extension
   passes `--no-notify` while the `show-notifications` key is false (its default), and
   `notify()` sends no `Error` signal, so nothing shows for long enough to
   be seen.
5. **No liveness reconciliation.** The extension watches a child's exit only while a
   launch is pending (`recorderLaunch.js:72-77`). A recorder that is alive while the
   state says IDLE, or dead while it says RECORDING, is never noticed.

## Insert's behaviour today (`_launchWSI`, `extension.js:901-973`)

| Extension state                              | Insert does                                                                                                  |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| launch pending (no signal yet since a spawn) | ignored                                                                                                      |
| RECORDING / PREPARING                        | SIGTERM via the PID file: graced stop (Plan 00148 Phase 0)                                                   |
| STOPPING (orange)                            | SIGTERM again: stop now                                                                                      |
| TRANSCRIBING (orange)                        | SIGTERM: no visible effect (the mic is already closed; whether any path still acts on it is not established) |
| SUCCESS / ERROR / IDLE                       | spawns a new client, with no check of the PID file or the server                                             |

## Testability

- `tests/extensions/test-stt-recorder-launch.mjs` (via `scripts/test-panel-sections.bash`
  with `gjs-loader.mjs`/`gi-stubs.mjs`) is the pattern for a pure extension module.
  `extension.js`'s state machine has no test, because it imports St/Main directly.
- `tests/speech_to_text/test_server_client.py` drives `run_server_mode` against a stub
  server with `PID_FILE` and `emit_dbus_signal` patched, and replays atexit handlers.
  That is where the client-side regression tests go.
- Logs: `[EXT]` and `[STREAM]` lines in `~/.local/share/speech-to-text/debug.log` (debug
  mode only), `[SERVER]` lines in `server.log` (always).
