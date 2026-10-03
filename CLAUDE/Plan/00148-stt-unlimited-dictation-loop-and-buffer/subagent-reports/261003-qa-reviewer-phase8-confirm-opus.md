**Verdict: FIX-BEFORE-MERGE.** The five earlier findings are fixed, but the fix brings in one new lock race and leaves one doc paragraph saying the opposite of the new behaviour. Both fixes are small.

### Earlier findings: all fixed

1. **A GPU CUDA cannot see now fails loudly. Fixed.**
   - `wsi-resolve-model` raises `GpuNotVisible` when `/dev/nvidia[0-9]*` nodes exist but CTranslate2 counts 0. The pattern deliberately skips `nvidiactl`.
   - Both auto resolution and `--gpu-count` go through `checked_gpu_count()`.
   - The play (`play-speech-to-text.yml:420-444`) finds the nodes, then fails if the count isn't a number, or if it is 0 while nodes exist. A host without a GPU passes with 0.
   - I read the installed Ansible `find` module (`find.py:581`): with `file_type: any` it does return character devices.
   - The real script is tested against both node layouts in `test_resolve_model.py`.
2. **The PID file lock. Fixed.**
   - The server holds an exclusive `flock` (`LOCK_EX|LOCK_NB`) for its lifetime, and the fd is `pid_lock_fd`.
   - The inode re-check after locking is correct (path `stat` against `fstat` of the fd, retried 3 times).
   - `release_pid_file()` unlinks the file while still holding the lock and only then closes it, which is the right order.
   - Python opens the fd with close-on-exec, so `pw-record` children cannot inherit the lock.
   - The model-load failure exit (`wsi-stream-server:792`) and both socket failure exits (`:801`, `:813`) remove the file.
   - A stale file and a reused PID are both tested, and the model-load failure is tested through the real `main()`.
3. **The play reads back the login unit. Fixed.** `play-speech-to-text.yml:529-566` asks the live user manager what `graphical-session.target` pulls in, after the daemon-reload, then asserts the unit is listed.
4. **The troubleshooting docs no longer point at `stt_model`. Fixed.** They now name the Settings Whisper Model and language. `stt_model` appears only at `:161-167`, which correctly describes it as the default for running the transcriber by hand.
5. **Each nit. Fixed.**
   - `PLAN.md:204-206` now says the unit exits 0.
   - Version claims now cite 1.2.1 and 1.1.1 as read from the wheels (`wsi-resolve-model:34-37`, docs `:157`).
   - `wsi:729-750` gives the resolver and the transcriber the same language and exports it.

### Should fix (new)

1. **`wsi-stream`'s lock probe can make a starting server refuse to start.** `files/home/.local/bin/wsi-stream:528-545`, against `files/home/.local/bin/wsi-stream-server:684-694`.
   - `server_pid_alive()` briefly takes a shared lock (`LOCK_SH|LOCK_NB`).
   - If a server reaches its exclusive `LOCK_EX|LOCK_NB` inside that window, it gets `BlockingIOError` and exits 1. It logs "Server already running (PID …)" and `kill <held>`, where `<held>` is whatever the file contains: `unknown`, or a dead server's PID that may since belong to an unrelated process.
   - The probe runs at `wsi-stream:575` (an Insert) and `:637` (the login unit), which is exactly the login-plus-Insert race this lock exists for.
   - It does end with one server, because the probe got its lock, reports "no server" and its caller starts one. But the login unit can show failed, and the log can tell the user to kill the wrong process.
   - The window is microseconds, and no test covers it.
   - Fix: in `claim_pid_file()`, retry `LOCK_EX|LOCK_NB` a few times over about 100 ms before deciding another server holds the lock. A probe holds it for microseconds, a server for its whole life. Add a test where a shared lock is held briefly during a claim.

2. **The docs still say a broken GPU falls back to CPU.** `docs/features/speech-to-text.md:559` says "If GPU still unavailable, extension falls back to CPU (slower but functional)."
   - With the default `auto` model, it now stops with the `CTranslate2 counts 0 CUDA devices` error. Line 153 of the same doc already says so.
   - The "CUDA / GPU Issues" section (`:532-559`) also doesn't name the new error, which is now the main symptom.
   - Fix: state that `auto` stops with that error, and that only a model chosen explicitly runs on the CPU.

### Checked and clean
- **Placement:** all the work stays in `play-speech-to-text.yml`; no new play.
- **NVIDIA hosts:** the play has no host variable to check instead of `/dev`. `play-nvidia.yml` is optional and declares none, so probing the device nodes is the only signal there is.
- **Fail-fast:** the one `failed_when: false` (`:546`) is annotated, and the next task asserts on its result.
- **Stderr hygiene:** `--gpu-count` prints only the number on stdout.
- **Public-repo safety:** no identifiers in the added lines.

### Mechanical gates
- `scripts/test-wsi-stop-grace.bash`: rc 0, `passed: 54 failed: 0`, unit tests "Ran 73 tests".
- `ansible-playbook --syntax-check play-speech-to-text.yml`: passes.
- `plan-qa --sweep`: 11 findings, all advisory, 0 blocking. I filtered the output for "00148" and nothing matched.
- `qa-all.bash`: not run, as instructed.
- `qa-helper-tests`: not needed, because nothing under `helpers/` changed.