## QA Review: Plan 00156 Task 1.3 and Task 2.1 (branch `agent-ab6c18f88848e2a35-e721b79e` vs F44, 3 commits)

**Verdict**: FIX-BEFORE-MERGE

### Blocking
None.

### Should fix
1. **`server_failure()` reports the wrong error when a server that gave way shares the log.** `/workspace/files/home/.local/bin/wsi-stream:482-484`
   It skips only lines containing "Server already running". But a server that loses the lock writes four ERROR lines, not one (`/workspace/files/home/.local/bin/wsi-stream-server:1339-1342`): "Server already running", "PID file: …", "To force restart…" and "  kill N". So when the winning server then dies of CUDA out-of-memory, `errors[0]` is `PID file: /run/user/…/wsi-stream-server.pid` and the real cause is hidden again. That is the defect Task 2.1 was meant to fix.
   The test `test_the_line_a_giving_way_server_appends_is_not_the_error` (`/workspace/tests/speech_to_text/test_server_start.py:107-113`) passes because its fixture holds only the first of the four lines, so it does not match what production writes.
   There is a second case. A server that exits before it truncates the log (lock refused, PID file unopenable, argparse error) leaves the previous run's log in place, so "first ERROR" can be a stale error from an earlier session.
   Fix: only read lines after the last `=== WSI-Stream Server Starting ===` marker, and drop the whole block the losing server writes. Then use the real four-line output in the fixture.
2. **`DIL_VERSION` was not bumped for a behaviour change.** `/workspace/files/var/local/docker-in-lxc:7` (change at :346-354)
   `wait_for_container_network` now aborts on a container that is not RUNNING or STARTING, and on an `lxc-info` that fails. The repo bumps this version for every behaviour change: commits `fb12242b` and `a72691ec`, plus the Plan 00075 journal entry "My own miss, corrected", which records the version going stale exactly like this. Bump to 1.2.1 or 1.3.0 with a comment. No CCY bump is needed: this file is not under `claude-yolo/`.
3. **The explicit `exit 3` in both new Ansible waits never runs.** `/workspace/playbooks/imports/optional/common/play-speech-to-text.yml:528` and `/workspace/playbooks/imports/play-lxc-install-config.yml:565`
   Under `set -euo pipefail`, the `{ systemctl status …; exit 3; }` group is the last command after `||`, so errexit still applies inside it. I probed it: `bash -c 'set -euo pipefail; false || { (exit 4); echo reached; exit 3; }'` gives rc=4.
   For an inactive or failed unit, `systemctl status` itself exits 3, so the task only works by coincidence. Any other status code (4 for no such unit, 1) falls outside `until: rc in [0, 3]`, so the task retries the full count instead of stopping at once, which contradicts the comments.
   Fix: capture the status and exit 3 explicitly, e.g. `{ systemctl status … || status_rc=$?; exit 3; }`.

### Nits
- `/workspace/CLAUDE/QA.md`: the Python fix example checks `is_server_running()` before `server.poll()`, while the page's next paragraph says to ask liveness before readiness. The bash example and `wsi-stream` itself do it the right way round.
- `/workspace/scripts/qa-ready-wait-rules.bash:734`: the pass line `passed: 592 failed: 0` gives one total. It should give the Python and shell counts separately, as AgentNotes asks of new gates.
- `scripts/qa-ready-wait-rules.bash` is committed as mode 100644. Its own usage line and the QA.md table say to run it directly; every sibling `qa-*.bash` gate is 100755.
- `recorderLaunch.js` `spawnWatched` (the real `Gio.Subprocess` path) is never run by `test-stt-recorder-launch.mjs`, which injects its own spawn. That is acceptable for GJS, but the review of that path is all there is.

### Checked and clean
- **Behaviour beyond the fix**:
  - tmate uses `tmate -F` (foreground), so `kill -0` on `$!` is valid.
  - vmtest reboots are guest-initiated with libvirt's default restart-on-reboot, so `domstate` stays `running` and `guest_must_be_up` will not falsely die.
  - Both `start_server` callers (`wsi-stream:1208`, `wsi-article:197`) catch `Exception`, so the narrowing to `except OSError` is safe.
  - Recorder failures that happen after the first PREPARING report are still ignored, which is correct.
- **Extension**:
  - `recorderLaunch.js` is deployed by the play (`play-speech-to-text.yml:342`) and checked by 00148's `acceptance.bash:97`. No other file list names the extension's files.
  - The generation guard and `settle()` on disable stop late callbacks.
  - `Gio.Subprocess.new` is the same synchronous spawn as before, and `wait_async` does not block. `_resetToIdle` clears the article spinner.
  - ESLint is clean on both files, and the 6/6 node tests pass.
- **Helper**: stdlib only, with no `__init__.py` as the convention requires; its test mirrors its path, 28 tests OK. The `.semgrep/` fixtures are excluded by `qa-discovery.bash:66`.
- **Gate**: `bash scripts/qa-ready-wait-rules.bash` passes with `passed: 592 failed: 0`. It guards both an empty population and a partial one (files semgrep was handed but did not scan). semgrep is provisioned by `play-python.yml` and `qa-toolchain`.
- **Tests on the production path**: `test_server_start.py` drives the real `start_server` (6/6 OK). `test-vmtest-reboot-dispatch.bash` extracts the real `wait_for_ssh` and `guest_must_be_up` (9/9).
- **Fail-fast**: no new `failed_when: false` or `ignore_errors`, and the 00055 fail path matches its siblings.
- **Plan**: plan-qa reports no findings for 00156, 00148 or 00055. The ✅ marks on Tasks 1.3 and 2.1 are backed by the gate and the tests. The host deploy goes through `run-changed.bash` in meta-deploy.
- **Public safety**: nothing install-specific in the added lines (no home paths, emails, private IPs or usernames).

### Mechanical gates
- qa-all.bash: not run, per your instruction.
- plan-qa --sweep: 0 block, 11 advise, none in the touched plans.
- syntax-check: both changed playbooks pass.
- qa-helper-tests: only the triggered module was run (28 OK).
- ESLint: run, clean. `check_extension_compat` was not needed: `metadata.json` is unchanged.

One disclosure: I redirected the gate's output to `/workspace/untracked/scratch/qa-rw.out`, an untracked scratch file. Nothing tracked was touched.