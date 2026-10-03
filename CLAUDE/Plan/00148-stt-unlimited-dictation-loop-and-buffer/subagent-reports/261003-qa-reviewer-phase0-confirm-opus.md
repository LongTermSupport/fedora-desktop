## QA Review: Plan 00148 Phase 0 fixes (ccf38cb5, 0206c155)

**Verdict: PASS WITH NITS**

All five earlier findings are fixed, and the fixes add no new defects.

### Prior findings

1. **A stop during the model load is now honoured.** Fixed.
   - The buffering loop now also stops when the stop is due (`wsi-stream:1142`).
   - If it stopped while the model was still loading (`:1157-1163`), it closes and drains the microphone into the buffer, then waits for the model. `load_model_thread` sets `model_ready` on both success and error (`:1128`, `:1132`), so that wait always ends.
   - The live feed loop and the second drain are skipped in that case (`:1196`, `:1217-1218`). The state is TRANSCRIBING, not RECORDING.
   - The docs (`docs/features/speech-to-text.md:221-223`) and PLAN Task 0.3 now describe this.
2. **Real tests now exist, and the coverage claims are honest.** Fixed.
   - `tests/speech_to_text/test_prebuffer_stop.py` runs the real `run_prebuffered_streaming` with real SIGTERMs. It checks when the microphone closes against when the model became ready, and that the bytes fed equal the bytes written. The cases are a TERM during the load, a second TERM during the load, and a TERM after the load.
   - `test_server_stop_order.py` checks the server's stop order: the pipe is drained, at least the required number of realtime passes run after EOF, and only then is `recorder.stop()` called.
   - The QA.md row, the harness header (`scripts/test-wsi-stop-grace.bash:11-14`) and PLAN Task 0.4 all say that standard streaming and the server-mode client loop are **not** run end to end.
3. **The schema compiles every run, and the play checks the grace reads.** Fixed.
   - The compile has no `when:` any more, runs as `user_login`, and uses `changed_when: schema_copy.changed` (`play-speech-to-text.yml:339-347`). `extension_dest/schemas` is owned by the user (`:306-313`), so it can replace an older compiled schema that root owns.
   - A new task runs `wsi-stop-grace` as the user with `HOME` set, and fails the play unless the exit code is 0 and stdout is a whole number (`:368-379`).
   - `wsi-stop-grace` no longer mixes gsettings' stderr into the value it parses, which removes a way to fail falsely.
4. **The docstring is corrected.** Fixed: `wsi-stream-server:238-241` now describes the real order.
5. **No logging in the stop signal handlers.** Fixed. `on_term` and `on_now` only append to `stop.notes` (`wsi-stream:140-148`), and `announce_pending_stop` logs those notes from the main loop (`:162-163`). Every main loop calls it (`:671`, `:889`, `:1143`, `:1159`, `:1199`).

### Nits

1. **The host check does not cover the new stop-during-load path.** PLAN Task 0.5 (`PLAN.md:75`) only says "press Insert right on the last word in each mode".
   - In the stub, `feed_audio` produces a realtime update straight away (`test_prebuffer_stop.py:47-49`). So nothing tests whether the 1.5 s bound in `wait_for_final_realtime_passes` (`wsi-stream:55`, `:1219`) is enough for the real model to transcribe a whole load's worth of audio fed in one burst.
   - Fix: add a 0.5 sub-step: stop during a cold pre-buffer load, and check that all the words spoken before the stop are pasted.
2. **Two other signal handlers still log.** These are older code, not part of finding 5, but they have the same re-entrancy shape:
   - `wsi-stream:622-623` (`abort_startup`, from b3cfe1f4)
   - `wsi-stream-server:667-671` (`signal_handler`, from 1a3fa423)

   Both only matter in debug mode. Fix them now or note them for later.

### Checked and clean

- **New problems from the fixes:** none found.
- **Fail-fast:** `model_error` is still raised after a stop during the load (`:1166`). The verify task has no `failed_when: false`.
- **QA.md:** most of the 114 changed lines are the table being re-aligned. The only content change is the `test-wsi-stop-grace.bash` row.
- **Plan state:** Task 0.5 is still open, and the status header (`PLAN.md:3`) matches that.

### Mechanical gates

- **`scripts/test-wsi-stop-grace.bash`** (run in the worktree): `passed: 25 failed: 0`, unit tests `Ran 27 tests`, exit code 0.
- **`ansible-playbook --syntax-check`** on the worktree play: passes. I ran it from `/workspace`, because the worktree has no `vault-pass.secret`.
- **`ruff check tests/speech_to_text/`:** clean.
- **qa-all.bash:** not run, as instructed.
- **ESLint and the extension compat check:** not triggered, because no extension JS or `metadata.json` changed.

Files are under `/workspace/.claude/worktrees/agent-a778b888905519dc3-ec22c06e/`:
- `files/home/.local/bin/wsi-stream`
- `files/home/.local/bin/wsi-stream-server`
- `playbooks/imports/optional/common/play-speech-to-text.yml`
- `tests/speech_to_text/test_prebuffer_stop.py`
- `CLAUDE/Plan/00148-stt-unlimited-dictation-loop-and-buffer/PLAN.md`