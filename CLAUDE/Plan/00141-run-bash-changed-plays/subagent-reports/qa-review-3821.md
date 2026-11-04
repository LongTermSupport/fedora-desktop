**Verdict: fix before merge.** The code change is correct and its tests cover it. What needs fixing is the changelog and docs, which describe the hooks daemon's behaviour wrongly for anyone on the current daemon release.

### Blockers
None.

### Should fix

1. **The changelog says something that isn't true for the current daemon release.** `docs/ccy-changelog.md:22-27` (and the commit message) say the daemon's deployed `ccy.env` arms the supervisor through `.claude/ccy/claude-supervise`, "in every such project".
   - That launcher only exists on the daemon's unreleased `main`. It was added in commit `4747db1c0` on 2026-10-05.
   - The latest release, v3.68.0 (2026-10-03, the version installed here), still writes `claude-supervise.py --arm --`. That's in its `src/claude_code_hooks_daemon/install/ccy_supervisor.py:41,73-77`, and in this repo's own `.claude/ccy/ccy.env`.
   - Fix: say the launcher form arrives with the next daemon release, and that the `.py` form still works.

2. **`docs/ccy.md` will be out of date as soon as that daemon release ships.**
   - `docs/ccy.md:1049` and `:1072` say `deploy_supervisor: true` writes `claude-supervise.py --arm --`. Daemon `main` now writes `.../claude-supervise --arm --` (line 37 of its `ccy.env`).
   - `:1032-1034` says the supervisor "runs under the container's system `python3`". Through the launcher, it runs under whichever interpreter the launcher picks: `$CCY_PYTHON`, then `python3.14` down to `python3.11`, then `python3`.
   - `:1420-1424` (session limits) should say that both wrapper forms are accepted.

3. **The case "the launcher is named but `claude-supervise.py` is missing" has no test.**
   - The code handles it: `entrypoint.sh:622-625` refuses with "not there", which is right. Without that refusal, the daemon launcher (lines 100-104 of its script) would run `claude` with no supervisor and silently drop `--plugin`.
   - So that refusal is the only thing keeping a session limit from being skipped silently, and nothing guards it.
   - Fix: add one case to `scripts/test-ccy-lifecycle.bash` next to line 436, e.g. `wrapper_case "$work/nowhere/claude-supervise --arm --" 7200 '' claude` expecting `rc=1`.

### Nits

- **The launcher has a second way to run without the supervisor.** If no Python 3.11 or later is found, the daemon launcher (lines 108-109) prints a banner and runs `claude` without the supervisor, so `--plugin` and the session limit are dropped. The entrypoint only checks the `.py`, so it can't see this. The image's Python (Debian bookworm, 3.11) meets the minimum, so in practice only a bad `CCY_PYTHON` triggers it. It deserves one sentence in the comment at `entrypoint.sh:592-594`, which currently says the launcher passes "every argument unchanged" without mentioning this exception.
- **The "not there" error names a path the user never typed.** At `entrypoint.sh:623`, a launcher-form wrapper reports `<launcher>.py`. Naming the launcher too ("…named via the claude-supervise launcher") would make it clearer.
- **The deploy list comment is stale.** `CLAUDE/Plan/meta-deploy.bash:55-57` still says "ccy 3.82.0". The `00141 run-changed` entry will pick up 3.82.1 as well, so only the comment needs updating when this merges.

### Checked and clean
- **Matching:**
  - `*/claude-supervise` needs a slash in front, so a bare command name on `PATH` is refused.
  - Any other word ending in `/claude-supervise` (such as the old `scripts/claude-supervise` shim, commit `b7bf80d3d`) only passes if a `.py` beside it exists and declares a matching `_PLUGIN_API_MAJOR`. Otherwise it is refused.
  - `supervisor` can no longer be empty at line 622, so dropping `${supervisor:-(none)}` is safe.
- **Does the launcher pass `--plugin` through?** Yes. I read the launcher on daemon `main`: `exec "$interpreter" "$supervisor" "$@"` (line 101), with `supervisor="${0%/*}/claude-supervise.py"`. That is the same sibling path the entrypoint builds, and every argument goes through unchanged.
- **The test runs the production code:** it lifts the entrypoint's own code from `ccy_lifecycle_wanted()` to the end of the file (test line 362). I ran `scripts/test-ccy-lifecycle.bash`: `passed: 211  failed: 0`, including the three new cases.
- **Version bumps:** `CCY_VERSION` 3.82.1, Dockerfile label 2.44 and `REQUIRED_CONTAINER_VERSION` 2.44 all agree (the test checks this too). The commit sits directly on F44 HEAD `6108c00c`, so the version numbers don't collide with anything.
- **Host-side check:** `lib/session-lifecycle.bash:205` reads `$PWD/.claude/ccy/claude-supervise.py`, which is the same file the launcher runs, so the host and the entrypoint check the same supervisor.
- **Public-repo safety:** nothing install-specific in the diff.

### Mechanical gates
- `qa-all.bash`: not run, as you asked.
- `plan-qa --sweep`: not run; this isn't plan work.
- `--syntax-check`: not applicable; no playbooks changed.