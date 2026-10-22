VERDICT: FIX-FIRST

I checked tip 26c5bfd4 in its worktree and changed nothing there; my scratch output is in `/workspace/untracked/scratch/r3-*`. All of these passed:

- `scripts/test-ccy-token-handoff.bash`: exit 0, "failed: 0". It covers a front launch, a direct run, bad values and the tmux environment (lines 228-288).
- `go test ./internal/ccylaunch/... ./cmd/claude-yolo/...`: all ok.
- `qa-bash-limits.bash`: exit 0.
- `ansible-playbook --syntax-check` on play-claude-yolo.yml: clean.

## Blocking

**1. Any executable at all is accepted as the front, not just the real one.**

- `lib/launcher-path.bash:15` takes any absolute path to an executable regular file.
- I sourced it with `CCY_LAUNCHER_FRONT=/usr/bin/env` and no front involved. The result was `CCY_LAUNCHER_PATH=/usr/bin/env`.
- So a direct run of `claude-yolo.bash` with the variable set will:
  - run that program in the tmux pane (`claude-yolo:1182`), the restart relaunch (`:3848`) and the custom-Dockerfile modes (`:1160`, `:1166`);
  - write it into the session record, which `sessions/restore.go` and `teams/restart/jobs.go` replay later with no check.
- It is same-user, but it turns one stray variable into a program that every later restore runs.
- The real front is always the sibling. `front.go:122` builds the bash path as `Dir(Self)/claude-yolo.bash`, so `SCRIPT_DIR` in the bash equals `Dir(Self)` and the front is `$SCRIPT_DIR/claude-yolo`. Restricting to that costs nothing.
- Fix: accept only `"$CCY_LAUNCHER_FRONT" == "$SCRIPT_DIR/claude-yolo"` (plus the `-f`/`-x` check) and warn otherwise. Add a case to the bad-values loop in the test with an executable that is not the sibling, such as `/usr/bin/env`.

## Not blocking

**2. "Unset before anything runs" is not literally true.**

- These children are started while the variable is still exported:
  - `$(dirname …)` at `claude-yolo:26`;
  - in `ccy_token_handoff_take`: `command -p id`, the `cd` subshells, `find`, `rm` and `rmdir` (`token-handoff.bash:35-58`). These run only when a hand-off is present.
- All of them are system tools that never read the variable. Nothing that matters (tmux, the engine, claude) starts before the unset; tmux runs only after line 1182.
- `token-handoff.bash` is sourced first, before `common.bash` (line 39 vs 41), and the test pins that order.
- The comments at `launcher-path.bash:10` and `docs/ccy.md` should say "before it starts any program that could keep it".

**3. The rollback note in meta-deploy is now out of date.**

- `CLAUDE/Plan/meta-deploy.list:108-110` says "session records and re-runs name claude-yolo, so nothing runs" the stale `.bash`.
- That stopped being true with this fix: a session started by running the `.bash` directly records `claude-yolo.bash`, and after a rollback it would restore onto the stale file.
- Add that case beside the existing 3.99.14.0 caveat.

**4. Checks that hold:**

- **docs/ccy.md recovery advice** (1691-1697): now true.
- **Version and changelog:** the 3.99.14.3 entry is accurate. No container bump is needed, since the image is unchanged.
- **Allowlist:** round 2 does not touch it. It still stands at 2732 from round 1, and the gate passes.
- **Play:** `play-claude-yolo.yml:485` installs `lib/launcher-path.bash` in the lib loop at owner root, mode 0755, the same as its siblings.
- **Front side:**
  - The hand-over passes `Env` to `ChildEnv`, which adds the variable on top of the inherited environment and drops any earlier copy of the same name (`execx/secret.go:85-93`).
  - `extraLibs` includes `launcher-path`.

**5. The `root_claude_projects` failure was already there before this branch.**

- The handler files on F44 (/workspace) are identical to the base d6b508bf: `git diff --stat d6b508bf F44 -- .claude/hooks/handlers/pre_tool_use/` is empty.
- I ran the file with system `python3 -m pytest`, `PYTHONPATH=` the daemon's `src` plus its py311 venv's site-packages (that venv has no pytest).
- Base: 1 failed, 41 passed. Tip: 1 failed, 43 passed.
- The same test fails in both: `test_matches_write_to_root_claude_projects_returns_false` (line 167). The two new mapping tests pass. The author's "unrelated" claim is verified.
