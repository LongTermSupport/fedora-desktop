# Plan 00215 rework review, round 4 (`feat/opencode`, `git diff F44...HEAD`)

Review only; nothing in the branch was changed. Checks run in the worktree, all green:
`scripts/test-ccy-agent-choice.bash`, `test-ccy-teams.bash`, `test-ccy-session-registry.bash`
(exit 0, no FAIL lines), `go test -count=1 ./internal/ccylaunch/... ./internal/registry/... ./internal/sessions/... ./internal/teams/...` (all ok), `scripts/qa-bash-limits.bash` (exit 0),
`shellcheck -x` on `lib/agent-choice.bash` and its test (clean).

## The round 3 answers hold

- **Finding 1 (no-bus Quick Launch only grepped).** `test-ccy-teams.bash` now runs the cut-out
  block with `NO_BUS_MODE="${QUICK_NO_BUS:-false}"`, writes `quick_kept_team` to `TEAMS_KEPT`,
  and checks, for both `y` and `n`: no team launched (`ran=`), nothing recorded, no Team line,
  the saved team kept; `y` still takes token and keys; a bus launch keeps nothing. The two
  block greps in `test-ccy-agent-choice.bash` are gone, and only the save-line text check stays
  (it is outside the block). Holds.
- **Finding 2 (`quick_kept_team` unset).** Starts empty at `claude-yolo:633`, with the parse
  defaults, so it covers every launch path. Holds.
- **Finding 3 (PLAN.md Task 1.3 wording).** Says "kept for the next launch". Holds.
- Re-checked: CCY 3.100.4 and container 2.79.1 match in the launcher, the Dockerfile LABEL,
  the changelog, PLAN.md and meta-deploy. No reference to the removed desktop play is left.
  The key goes only by `-e NAME` (golden `launch-opencode.txt:61`, plus `checkKeyOnlyTo` and its
  mutation). The entrypoint unsets `CCY_CLAUDE_WRAPPER` under `CCY_NO_SUPERVISOR=1`, and with
  `PINGBUS_MEMBERSHIP=off` it reaches the plain `exec "$@"`, so `opencode` really is the
  command that runs. A `-oc` after `--` is claude's word in bash (`SAW_DOUBLE_DASH` branch) and
  in Go.

## Findings

1. **SHOULD-FIX: `--debug` writes the provider key, unmasked, into the debug log.**

   - `lib/agent-choice.bash:363` (`[[ "$value" =~ ... ]]`) and `:367`
     (`local -x "$name=$value"`) expand the key's value in lines that `set -x` traces.
   - The debug chooser runs before the tmux re-run. The re-run gets `--debug` again and sets
     `set -x` (`claude-yolo:1081`), so the process that runs `ccy_agent_container` is the one
     being traced.
   - The log's only defence is the prefix mask at `claude-yolo:1080`:
     `(gh[opsur]_|github_pat_|sk-ant-)`. An `ANTHROPIC_API_KEY` (`sk-ant-api03-`) is caught. The
     key the docs use as their example, `OPENROUTER_API_KEY` (`sk-or-v1-...`), is not caught,
     and nor is an OpenAI `sk-proj-` key or any other provider's.
   - The launcher's own comment (`claude-yolo:1077-1079`) and the token-handoff suite ("the
     log holds no token") show that the project rule is: no secret is expanded in a traced line.

   **Fix:** stop tracing inside the function for the lines that touch the value, and trace the
   engine call again. For example, at the top of `ccy_agent_container`:
   `local - traced=; [[ $- == *x* ]] && traced=1; { set +x; } 2>/dev/null`. Then, before
   `container_cmd "$@"`, add `[ -z "$traced" ] || set -x`. `local -` restores the options when
   the function returns.

   Add a case to `test-ccy-agent-choice.bash`: run `ccy_agent_container` under `set -x`, with
   `2>` sent to a file and a fake `container_cmd`, then assert that the key's value is not in
   that file and that the fake still saw the key.

2. **SHOULD-FIX: the branch no longer merges cleanly into F44.** F44 has moved to 90a873e85;
   the merge base is 67033c224. `git merge-tree F44 HEAD` reports two conflicts:

   - **`CLAUDE/Plan/meta-deploy.list`, the 00211 `deploy-stage-2.bash` entry (around line 64).**
     c159e6626 on F44 changed "created by the entry above" to "created when play-agent-bus.yml
     passed at 67033c224". This branch changed "container 2.78.15" to "2.79.1" in the next line.
   - **`CLAUDE/Plan/README.md`.** Both sides added an Active Plans line at the same place.

   **Fix:** merge F44 into `feat/opencode`.

   - In meta-deploy, keep F44's "(created when play-agent-bus.yml passed at 67033c224)" wording
     together with this branch's "container 2.79.1". meta-deploy's list must stay exact
     (AgentNotes), so the resolution should be checked by reading the result, not by taking
     one side.
   - In README, keep both entries.
   - Re-run the targeted suites after the merge. F44 also changed `play-claude-yolo.yml`
     (the CCY_VERSION regex read), and that file auto-merges.

3. **NIT: a provider key file with Windows line endings passes the check and hands opencode a
   key ending in `\r`.** `lib/agent-choice.bash:327` accepts `key\r\n` (one line, not blank),
   and `$(<file)` at `:363` keeps the `\r`. The provider then refuses the key with an auth error
   that does not point at the file. **Fix:** add `|| grep -q $'\r' "$file"` to the refusal
   condition at `:327`, and add a matching case to the test.

VERDICT: FIX-FIRST
