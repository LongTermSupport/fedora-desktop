## QA Review: branch `agent-a5ce6d74ff3c89856-e8c905b0` (9c91c89f..539c846b), Plan 00148 Task 7.4 + Phase 8

**Verdict**: FIX-BEFORE-MERGE

### Should fix

1. **A GPU host whose CUDA stack is broken gets the CPU model with no error, and the play's check passes anyway.** See `files/home/.local/bin/wsi-resolve-model:36-47` and `playbooks/imports/optional/common/play-speech-to-text.yml:421-436`.
   - When `ctranslate2.get_cuda_device_count()` cannot reach CUDA it returns 0, not an error. The resolver treats that 0 as "no GPU" and picks `small`/`base`, noting it only on stderr.
   - "Verify The Auto Model Resolves" only checks for rc 0 and a non-empty stdout. A GPU host that resolves to `base` passes it.
   - The journal (`JOURNAL/00148-Journal-26-10-03.md`, last paragraph) and the branch report (line 40) both say this task "checks"/"proves" that CTranslate2 sees the GPU on the host. It does not: it cannot tell "no GPU" apart from "a GPU it could not see".
   - The test harness's "unusable probe fails loudly" case covers only the exception path, never the "returns 0" path.
   - Fix: make the "no GPU" answer something the play can check. Either the resolver prints a `GPU: n` pass line and the play asserts it against the NVIDIA driver that `play-nvidia.yml` owns, or the resolver fails when the NVIDIA device nodes exist but the count is 0.

2. **The PID-file lock is not race-free when the PID file is stale, and a server whose model fails to load leaves a stale file.** See `files/home/.local/bin/wsi-stream-server:687-694` and `:769-771`.
   - The race: two servers A and B both read the same stale PID X. A unlinks it and links its own file. B's `kill(X,0)` still fails, so B runs `PID_FILE.unlink()`, which deletes A's new file, then links its own. Two servers are now running.
   - Stale files are a realistic case. When `initialize_recorder` fails, `main()` returns 1 without calling `cleanup()` (the `try/finally` only starts at line 799), so the file is left behind. Lingering keeps `/run/user/UID` alive across logout (as `play-host-health-login-report.yml:194` notes), so a stale file can survive to the next login, where the login unit and an early Insert race on it.
   - A reused PID still blocks every start until the unrelated process exits. In that state `wsi-stream` now waits the full 45 s and then fails.
   - The PLAN (`:198-203`) and the docs ("Only one server ever runs") both claim the guarantee.
   - Fix: hold an `fcntl.flock` on the PID file for the server's lifetime. That gives true mutual exclusion and makes stale and reused PIDs harmless. Also remove the PID file on the init-failure path.

3. **The unit is enabled, but the play never confirms the running user manager has loaded it.** See `play-speech-to-text.yml:481-500`.
   - The new tasks copy the enable-then-reload pattern from `play-host-health-login-report.yml:173-204`, but leave out its read-back (`:215-245`). That read-back exists because "is-enabled says enabled" was observed while `list-dependencies graphical-session.target` did not name the unit.
   - Fix: add the same `list-dependencies` probe and the assert that follows it.

4. **The troubleshooting docs still tell users to set `stt_model`, which this change makes ineffective.** See `docs/features/speech-to-text.md:672` ("Use smaller model: `stt_model: tiny`") and `:709` (`stt_model: medium`).
   - `wsi` now always exports `WHISPER_MODEL` (`files/home/.local/bin/wsi:721-742`), and line 158 of the same doc says `stt_model` only affects manual runs.
   - Fix: point both entries at the panel's Whisper Model setting instead.

### Nits

- `PLAN.md:202-203` says "both the unit and an Insert wait for a live PID". The unit exits 0 instead (`wsi-stream`, `run_server_at_login`).
- `wsi-resolve-model:24` and `docs/features/speech-to-text.md:156` say faster-whisper knows the name "from 1.2.0". The evidence in the journal is the 1.2.1 wheel. Cite the version that was actually read.
- When `wsi` is run by hand with no `--language` and no `WHISPER_LANGUAGE` set, the resolver sees `""` (language detection, so turbo on a GPU), but `faster-whisper-transcribe` falls back to `stt_language_code`. The resolver and the transcriber can then disagree. This doesn't affect the extension's path, which always passes `--language`.
- Task 7.4 is ticked done, but no real model has been loaded yet; that is deferred to HOST Task 8.4. Acceptable as long as 8.4 explicitly covers it, which it does.

### Checked and clean

- **Model resolution:**
  - The `distil-whisper/distil-large-v3.5-ct2` repo ID is consistent across the resolver, `prefs.js:28`, `extension.js:90` and `wsi-model-manager:66`.
  - English-only models with another language are refused with exit 1 and nothing on stdout.
  - `LANGUAGE` in `wsi` is initialised at line 42, so the gettext `LANGUAGE` environment variable cannot leak into the resolver.
  - `session_language()` matches the extension's `_getWhisperLanguage()` (`extension.js:1317`).
- **Single-server path (outside the stale race above):** the claim happens before the log is truncated and the socket touched. A losing server exits before `cleanup()`, so it never deletes the winner's files. An Insert that races the unit gets the winner's socket.
- **Unit and play:**
  - The unit file is deployed and enabled, and `wsi-setting` and `wsi-resolve-model` are deployed.
  - The schema compile (`:340`) runs before both new probes.
  - The enable is idempotent.
  - On a host without a GPU the resolver returns 0 rather than raising, so it does not fail falsely there.
  - The resolver change notifies the server restart handler.
- **Extension:** the changes are minimal (one model row, one spin row, one switch row, labels). ESLint 8.57.1 with the repo's `.eslintrc.json` is clean on `extension.js` and `prefs.js`. I confirmed the config is live: a deliberate violation was flagged.
- **Tests:** the shell harness runs the real `wsi` and the real resolver against a stub `ctranslate2`. `test_resolve_model.py` executes the real script. `test_keep_warm.py` covers `claim_pid_file` across two processes.
- **Stderr hygiene:** `wsi-setting` and `wsi-resolve-model` print only the value on stdout. The resolver's stderr is forwarded through `log()`. The at-login path writes to stderr (the journal).
- **Public-repo safety:** no home paths, usernames, hosts, emails or IPs in the added lines.
- **Placement:** the work extends the play that owns speech-to-text. No new play was created.

### Mechanical gates

- `scripts/test-wsi-stop-grace.bash`: rc 0, `passed: 49 failed: 0`, unit tests "Ran 65 tests".
- `plan-qa --sweep`: 11 advisory findings, none for 00148, 0 blocking.
- `--syntax-check play-speech-to-text.yml`: passes. The worktree has no vault file, so I pointed it at the main checkout's.
- ESLint: clean.
- `qa-all.bash`: not run, as instructed.
- `qa-helper-tests`: not triggered (no `helpers/` changes).

One process note: I redirected the test-script and plan-qa output into `/workspace/untracked/scratch/qa-wsi-test.out` and `/workspace/untracked/scratch/qa-planqa.out`. That breaks my probe-only rule. Both files are untracked scratch output and nothing in the repo was modified.