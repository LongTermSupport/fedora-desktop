**Verdict: FIX-BEFORE-MERGE**

## QA Review: commit 978b6a83, Plan 00148 Phase 0 (delayed stop)

The bash trap, the `GracefulStop` design and server mode are sound. One mode breaks the "second TERM stops at once" contract, and that mode is exactly the one the tests don't reach.

### Should fix (blocks merge)

1. **Pre-buffer mode ignores a due stop while the model loads.** The buffering loop at `files/home/.local/bin/wsi-stream:1129-1140` stops only when `model_ready` is set. It never checks `stop.due()`. Insert is honoured in `PREPARING` (`extension.js:817-821`), and the PID file is written at `:1052`. So any stop pressed during a model load keeps the microphone open until the model is ready: a first TERM whose grace has run out, a second TERM, or a SIGINT. All of that audio is then fed in at `:1159`. Before this commit the TERM killed the process outright. Now it records and transcribes whatever is said after the user stopped, which can be a 5–15 s cold load per the docs.
   - The comment at `:1156-1157` ("this is audio the user spoke before stopping") is wrong in this case.
   - `docs/features/speech-to-text.md` ("A second press during the grace stops at once") and PLAN Task 0.3 ✅ claim behaviour this mode doesn't have.
   - **Fix:** break out of the load loop on `stop.due()`. Then drain with `read_remaining_audio`, wait for `model_ready`, and feed only what was buffered.

2. **The per-mode wiring has no test, which is how item 1 got through.** `tests/speech_to_text/test_stop_grace.py` tests these pieces on their own: `GracefulStop`, `install_stop_handlers`, `read_stop_grace`, `read_remaining_audio` and `wait_for_final_realtime_passes`. It never runs the loops in `run_standard_streaming`, `run_prebuffered_streaming` or `run_server_mode`. The `wsi-stream-server` changes (`stop_recording_pipeline`, `:247-303`) have no test at all. The commit message and the QA.md row say the tests "cover the streaming modes", which overstates them.
   - **Fix:** add a test that runs `run_prebuffered_streaming` with a stub `RealtimeSTT` module and a stub `pw-record` on PATH, sending TERM during the load and again after it. The batch `wsi` harness already does the same thing for bash.
   - At minimum, the QA.md row and the commit claim should say which paths are covered and which are not.

3. **The schema compile only runs when the copy changes, and the recorders now refuse to start without the key.** `play-speech-to-text.yml:336-342` compiles only `when: schema_copy.changed`. Suppose one run copies the XML and then aborts before the compile finishes. Every later run skips the compile, and `gschemas.compiled` never gets `stop-grace-seconds`. From then on every recording fails (`wsi:717-721`, `wsi-stream:560-564` and `:768-773`). The error message says "re-run play-speech-to-text.yml", which would not fix it. This is the existence-guard defect class from AgentNotes (Plan 00067).
   - **Fix:** compile every run (it is idempotent and cheap). Add a task that runs `wsi-stop-grace` as `{{ user_login }}` and asserts a number on stdout, so the play proves the key can be read.

### Nits

4. `files/home/.local/bin/wsi-stream-server:237-240`: the `stop_recording_pipeline` docstring still says it "signals the feeding thread to stop" before stopping the recorder. The new order is: terminate, drain to EOF, wait for realtime passes, then stop.
5. `files/home/.local/bin/wsi-stream:131-137`: the signal handlers call `log()`, which in debug mode does `print(..., file=sys.stderr)`. If that interrupts a `log()` already running on the main thread, it can raise `RuntimeError: reentrant call`. In pre-buffer mode the broad `except Exception: break` at `:1138` and `:1181` would turn that into an early end to the recording. The pattern existed in the old handlers, it only happens in debug mode, and it is rare. Recording state in the handler and logging from the main loop would remove it.

### Checked and clean

- **wsi bash trap:** `on_term` returns `0` under `set -e`. The `wait` loop at `wsi:776-784` re-waits while the recorder is still alive. The timer subshell runs with TERM at its default disposition. The grace timer is cancelled in `stop_recording`, after the loop, and in `cleanup`. INT stays immediate. A TERM before the grace is read, or after `RECORDING_OVER`, stops at once. Escape in batch mode is a SIGKILL of the process tree (`extension.js:1142-1143`), so nothing waits.
- **Python handlers:** they only record times, with no sleep and no blocking. All stop actions happen in the main loops, and `recorder.stop()` has moved out of the standard-mode handler. In server mode, USR1 makes the stop due at once and sets the discard flag. `recording_active` is still cleared after the STOP is sent (`wsi-stream:681`), so atexit doesn't send it twice. The `wsi-article` and `wsi-article-window` stop paths don't use the changed code.
- **Deployment:** the play deploys `wsi-stop-grace` with mode 0755 (`:354-361`). The schema key is `i`, default 3, range 0–30, matching the docs and the test. `ansible-playbook --syntax-check` passes.
- **Fail-fast:**
  - Missing `gsettings` or an absent key makes `wsi-stop-grace` exit 1 with the reason on stderr, and both recorders refuse to start.
  - `wsi` checks this before the microphone opens; `test-wsi-stop-grace.bash:257-263` asserts it.
  - In Python, a timeout or a non-number from the reader raises an error and ends with `ERROR`.
- **Stderr hygiene:** `wsi-stop-grace` prints only the value on stdout. The `wsi` stdout check in the harness (`Hello world`) passes, so the new code adds nothing to stdout.
- **Gate wiring:** the `qa-all.bash` hard gate has a pass line, and `passed: N` parses through `qa_gate_case_count`. The unit tests are counted as one case, but the PASS line shows `Ran 23 tests`. `ruff check tests/speech_to_text/` is clean.
- **Public safety:** the diff has no home paths, private IPs or other identifying values.
- **Plan:** Task 0.5 (the host check) is correctly left open, and the status header matches it. The plan-qa sweep has no findings for 00148.

### Mechanical gates

- **qa-all.bash:** not run, as instructed (the coordinator is running it).
- **`scripts/test-wsi-stop-grace.bash`:** run directly; `passed: 25 failed: 0` (unit tests: `Ran 23 tests`).
- **plan-qa --sweep:** 0 block, 11 advise, none of them for 00148.
- **syntax-check `play-speech-to-text.yml`:** passes.
- **ESLint and the extension compat check:** not triggered. `extension.js` and `metadata.json` are unchanged; the only `extensions/` change is the schema XML.

Files: `/workspace/files/home/.local/bin/wsi-stream`, `/workspace/files/home/.local/bin/wsi-stream-server`, `/workspace/playbooks/imports/optional/common/play-speech-to-text.yml`, `/workspace/tests/speech_to_text/test_stop_grace.py`, `/workspace/docs/features/speech-to-text.md`