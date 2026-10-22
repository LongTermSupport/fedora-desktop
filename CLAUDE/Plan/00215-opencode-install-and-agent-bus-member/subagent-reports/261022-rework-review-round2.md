# Plan 00215 rework review, round 2 (`feat/opencode`, `git diff F44...HEAD`)

This was a review only, and nothing was changed. I ran these checks in the worktree, and all
passed: `scripts/test-ccy-agent-choice.bash` (61 passed, 0 failed),
`scripts/test-ccy-session-registry.bash` (204 passed, 0 failed),
`go test -count=1 ./internal/ccylaunch/... ./internal/registry/... ./internal/sessions/... ./internal/teams/... ./internal/conversation/...` (all ok), and `scripts/qa-bash-limits.bash`
(exit status 0).

## The round 1 answers hold

1. **Restore.** `ccy_registry_wants_restore` returns false for `--opencode` or `-oc` before
   `--`, so the record is marked no-restore (`lib/tmux-session.bash:412`). `registry.Opencode`
   is checked in `sessions/restore.go:191` before `conversation.For` and `ResumeArgs`, so the
   session is skipped even when its mark is on. `discover.LaunchedWithBus` is false for it, which
   also covers `conversation.For`, ccy-teams hot control and the hot list. ccy-teams restart only
   acts on sessions that hold seats, and an opencode session never holds one, so its
   `Supervised`/`ResumeArgs` path cannot be reached. Tests cover each spelling and the case after
   `--`.
2. **Quick Launch team.** `quick_team` is cleared under `NO_BUS_MODE` and is now the only way the
   saved team is joined, so `TEAMS_LIST="$LAST_TEAMS"` is gone. The late, wrong-flag 64 from
   `ccy_no_bus_check` can no longer happen.
3. **The replay-decision gate.** It now reads the `case` patterns in `agent-choice.bash` and
   checks that at least one was found. `--opencode` and `-oc` are keep flags in both bash and Go.
4. **Bare first word.** A bare first word is refused (64), and the message names `--prompt`.
5. **Key read.** The key is read into a local. A key that has gone, or reads as blank, returns 1
   before the engine starts, and a whitespace-only file is refused at the check. Both are tested.

## Checked, no finding

- **A claude launch's container command and banner are unchanged.** `CCY_AGENT_CMD` for claude
  is `claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"`. The banner's printf gives
  the same bytes as the four old echos, trailing space included (tested). Every existing difftest
  golden is unchanged except the three `--help` ones. The debug-chooser printf has the same
  output.
- **`local -x` and `set -e`.** The function is called inside `|| container_rc=$?`, so set -e is
  off inside it, and every failure is handled explicitly. The prefix-assigned tokens and the
  `local -x` keys reach `container_cmd` only. The difftest `checkKeyOnlyTo` proves that no other
  child gets the key, and the mutation that drops `CCY_AGENT_RUN_ARGS` is caught.
- **Globbing.** `"$dir"/*` with a missing directory falls to the "holds none" error. A dangling
  symlink is seen through `-L` and refused. Key contents never reach output or argv: only `grep -q`, `wc -l` and `stat` read the file.
- **The container.** The golden run has `-e PINGBUS_MEMBERSHIP=off -e OPENROUTER_API_KEY ... -e CCY_NO_SUPERVISOR=1 ... opencode --auto ...`. In `entrypoint.sh` the bus branch (which
  needs `$1 = claude`) is skipped. `CCY_NO_SUPERVISOR=1` unsets any wrapper, and the script ends
  in `exec "$@"`.
- **Dockerfile.** The tarball's sha256 is checked before it is extracted, the version is
  asserted, and both JSON files are validated with `jq`. The LABEL 2.79.1 matches
  `REQUIRED_CONTAINER_VERSION`. CCY 3.100.2 matches across the launcher, the changelog, PLAN.md
  and meta-deploy.
- **Wiring.** The play's lib list, `CCY_LIBS`, the qa-gates row, the addedgates row, `help.txt`
  and the goldens are all updated. The meta-deploy entry is folded into the existing
  `play-claude-yolo.yml` entry and says what the flag does and that a key file is needed.

## Findings

1. **SHOULD-FIX: an opencode (or `--no-bus`) launch erases the saved team, so the next ordinary
   `ccy` Quick Launch no longer offers it.** `claude-yolo:3475` writes
   `LAST_TEAMS="${CCY_SEATS}"` on every launch that gets that far. An opencode launch now always
   has empty `CCY_SEATS`, so in a team checkout (this repository is one), one `ccy -oc` makes
   the next plain `ccy` show no "Team:" line and join no team. That is a change to a later claude
   launch, which the "a launch without the flag is unchanged" goal rules out. Before this branch,
   `--no-bus` with a saved team failed at seats-take after this file was written with the seats.
   It did not wipe the team, so this is new behaviour for both flags. **Fix:** when
   `NO_BUS_MODE` is true, keep the saved value. For example, before the heredoc:
   `saved_teams=$CCY_SEATS; [ "$NO_BUS_MODE" != true ] || saved_teams=${LAST_TEAMS:-}`, then
   write `LAST_TEAMS="${saved_teams}"`. Add a test: a text check in
   `test-ccy-agent-choice.bash`, or better, a difftest world with a `.last-launch.conf` holding
   `LAST_TEAMS` plus `-oc --token work --no-ssh`, whose golden shows the file still holding the
   team.

2. **NIT: the Quick Launch box drops the team without saying so, and that path is held only by
   text greps.** `claude-yolo:1261-1262`: with `quick_team=""`, `describe-seats` prints nothing,
   so the owner sees no team and no reason. Round 1 asked for the line to say the saved team is
   not taken. **Fix:** when `NO_BUS_MODE` is true and `LAST_TEAMS` is set, print
   `  Team:    <saved team> (not taken: --no-bus/--opencode is on no team)` to stderr, and do not
   call `describe-seats`. The behaviour itself is a two-line guard, so the greps
   (`test-ccy-agent-choice.bash:205-206`) are acceptable for now.

3. **NIT: only the first word is checked for being a bare word.** `lib/agent-choice.bash:44`:
   `ccy -oc --model openrouter/x "fix it"` passes the check, and opencode still reads `fix it`
   as its project directory. **Fix:** say in docs/ccy.md that the message must be given with
   `--prompt` wherever it comes. A fuller check needs opencode's value-taking flags, so the doc
   line is enough.

4. **NIT: three assignments are written as a loop.** `lib/agent-choice.bash:63-65`: a
   `for ... printf -v` loop sets `NO_BUS_MODE`, `NO_SUPERVISE_MODE` and
   `CLAUDE_ARGS_BEFORE_SEPARATOR`. **Fix:** write them as one statement:
   `NO_BUS_MODE=true NO_SUPERVISE_MODE=true CLAUDE_ARGS_BEFORE_SEPARATOR=0`. It is clearer and
   smaller for the limits.

5. **NIT: the docs do not say that opencode's history is lost.** `docs/ccy.md:888` says "opencode
   keeps its history inside the container". The container is `--rm`, and nothing mounts
   opencode's data directory, so the history ends with the session. **Fix:** "opencode keeps
   its history inside the container, so it is gone when the session ends".

6. **NIT: ccy-teams messages name `--no-bus` for an opencode session.**
   `src-go/internal/teams/hot/hot.go:497,509` and `src-go/internal/conversation/conversation.go:70`
   say "launched with --no-bus" or "relaunch ... without --no-bus" to an owner who used
   `--opencode`. The listing state in `hot/list.go:167` reads the same way. **Fix:** name
   `--opencode` beside `--no-bus` in those strings, or leave them for Phase 3 and note that in
   the plan.

7. **NIT: the "byte for byte" claims are wider than the change.** `PLAN.md:158` and
   `docs/ccy-changelog.md:34` say a launch without `--opencode` is byte for byte what it was. A
   `--no-bus` Quick Launch with a saved team did change (it is now accepted instead of failing
   late). The changelog bullet mentions `--no-bus`, but the closing sentence contradicts it.
   **Fix:** "A launch without either flag is ...", or name the `--no-bus` change in the success
   criterion.

8. **NIT: one test can never fail.** `scripts/test-ccy-agent-choice.bash:187` greps the test
   shell's own `env` for `_API_KEY=`, but nothing in the test ever exports one, so it cannot
   fail. Line 186 already covers the real property. **Fix:** delete the check, or make it run a
   child (`bash -c 'env'`) right after a `ccy_agent_container` call whose fake `container_cmd`
   starts a background child.

VERDICT: FIX-FIRST
