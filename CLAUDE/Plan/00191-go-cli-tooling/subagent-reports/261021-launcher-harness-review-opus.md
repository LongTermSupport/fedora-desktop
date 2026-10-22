VERDICT: FIX-FIRST

The harness works and is well built: `go test -race -shuffle=on ./internal/ccylaunch/...` passed in 53 s, a plain run in 6 s, and `src-go/qa/changed` tests and `scripts/test-ccy-gpu-device.bash` (9/9) pass. One defect in the goldens' shape should be fixed before this becomes the port's gate. The rest is non-blocking.

**Blocking**

1. **The goldens pin bash's leaked environment, tokens included, on every child.** `recordCall` (`fake_test.go:262-271`) records every env name not in `baseEnv`. Bash's exported globals therefore show up on harmless calls. Counted over the goldens: `CCY_RESTART_*`, `CCY_LIFECYCLE_PLUGIN_PATH` and `CONTAINER_ENGINE` appear on 181 calls, `CLAUDE_CODE_OAUTH_TOKEN` on 72 and `GH_TOKEN` on 47.
   - Evidence from `golden/launch-gh-token-env.txt`: `getenforce` (line 61) and `curl -fsSL https://registry.npmjs.org/...` (line 83, "as above") get `GH_TOKEN`. `timedatectl show` (line 97) gets `CLAUDE_CODE_OAUTH_TOKEN`.
   - A Go port that gives the token only to `podman run` would fail many scenarios. It would have to copy a bash quirk that also leaks a credential, which is what item 2 of the brief asks to avoid.
   - The PLAN's list "Bash behaviour the goldens now pin" leaves this out.
   - Fix: record env names only for engine `run`/`build`/`exec`, tmux and `setup-token` calls, or only for an allowlist of names that matter (the tokens, `CCY_*` passed on purpose). Otherwise, at minimum, list the leak in PLAN.md Task 4.4 as pinned bash behaviour and as a defect the port must not keep.
   - Related: the doc says "the names the launcher added" (`parity_test.go:15`, `fake_test.go:97`). In fact scenario-supplied env is recorded too, because `Base` holds only the `baseEnv` keys (`sandbox_test.go:473`).

**Non-blocking**

2. **Security of the two env overrides** (`claude-yolo:135`, `:3485`, `:3585`).
   - Unset or empty, the defaults are byte-identical (`:-`).
   - Anyone who controls the environment already controls `PATH`, so this is no new trust boundary. It does widen what a stray `.envrc` or tmux global env can do:
     - `CCY_DOCKERFILE_DIR` rebuilds the shared `claude-yolo:latest` image from another directory, and the `--append-system-prompt` text is read from there too.
     - `CCY_GPU_DEVICE_DIR=/dev` becomes `--device /dev:/dev` (`gpu_device_flags`, `common-pure.bash:263-268`), handing every device the user can open to a `--dangerously-skip-permissions` container.
   - There is no fail-fast on a set-but-missing directory. It fails late (Dockerfile or startup file "not found"), and a missing GPU directory silently means no GPU.
   - A production seam is justified, since a Go port needs the same hook. Suggest a guard that the directory exists and that the GPU directory is not `/dev` or `/`.
3. **The seam only partly redirects.** `lib/session-lifecycle.bash:43` (`CCY_LIFECYCLE_PLUGIN_PATH`) and `lib/dockerfile-custom.bash:16` still hardcode `/opt/claude-yolo`. The goldens record the real `/opt` path in env, and `--custom` will reach the real host when it is covered.
4. **Hermeticity gaps.**
   - No absolute-path program calls bypass `PATH`; `sudo`, `systemd-run`, `loginctl` and the engines are all fakes.
   - The run still reads the host in two places: `/usr/share/zoneinfo/Europe/London` must exist (`claude-yolo:3728`; `scenarios.json:37`), and `/etc/localtime` is read.
   - `mktemp /tmp/claude-yolo-XXXXXX` (`claude-yolo:2366`) and the other `/tmp/ccy-*` templates write to the real `/tmp`, outside `snapshotDirs` (`sandbox_test.go:54`). A port that leaked its config temp there would not be caught.
   - My runs left nothing new in `/tmp`. The 11 `ccy-token-setup-*` files there predate them and could not be attributed.
5. **Possible flake: the pid mark** (`sandbox_test.go:514`). It replaces the pid wherever it appears as a whole word. In a fresh pid namespace (CI containers) a small pid could hit other numbers in the output. Anchoring it to known contexts (`-{pid}-`, `ccy-claude-update-`) would remove the risk.
6. **`-update` hazards.**
   - `-update` with `LAUNCHER` set re-records the goldens from a non-bash program (`parity_test.go:70`). It should refuse, as the 00213 oracle's purpose implies.
   - Under `-update`, `TestOracleCanFail` runs in parallel and reads goldens while they are being rewritten (`:179`).
7. **`TestOracleCanFail` is real, not a tautology.** It edits a scratch copy, requires the text to occur exactly once and the edit to change something, and asserts a difference. It is weak only in that it does not check which line differs: a crash would pass it too. Optionally assert that the diff contains the mutated token.
8. **`.golangci.yml:101-107`** adds a path-scoped gosec exclusion (`G302|G304|G306|G401|G501|G703`) with a stated reason. It follows the statusline precedent; md5 is required to match the launcher's hash. No `//nolint` was added and nothing else was loosened.
9. **Routing, docs, version and plan.**
   - The `qa-routing.tsv:35` glob is correct, and the QA.md row matches it. It only fires for launcher, `lib/*.bash` or Dockerfile edits, so unrelated commits do not pay. The cost is the whole Go gate, as for the existing common-pure row, and is acceptable.
   - Most of the 53 s is race-instrumented fakes, since every fake runs the race-built test binary. A non-race fake binary would cut it.
   - CCY 3.99.13.13 is bumped with a changelog entry; the container version is correctly unchanged. `bash-limits.allowlist` is untouched.
   - PLAN counts check out: 74 = 36 standalone + 10 engine + 9 tokens + 19 launch.
   - The routing and QA.md rows accurately describe the gate. No install-specific names appear in added lines or goldens.