# Plan 00215 rework review, round 1 (`feat/opencode`, `git diff F44...HEAD`)

Reviewer only; nothing changed. Checks run in the worktree, all green:
`scripts/test-ccy-agent-choice.bash` (52/0), `go test -count=1 ./internal/ccylaunch/difftest/`
(ok), `test-ccy-session-registry.bash` (199/0), `test-ccy-token-handoff`, `-teams`,
`-git-signing`, `-restart-request`, `-agent-bus`, `-project-env` (all 0 failed),
`qa-bash-limits.bash` (rc 0). Every existing difftest golden is unchanged except the three
`--help` ones, so a claude launch's command, banner and run argv really are unchanged.

What holds up (no finding): `${PROMPT_TEXT:+--prompt "$PROMPT_TEXT"}` gives exactly two words,
or none, with no splitting or globbing of the prompt (I checked it with `a  b ?`). The prefix
assignments plus `local -x` inside `ccy_agent_container` keep the key in that one call's
environment: `container_cmd` is just `"$CONTAINER_ENGINE" "$@"`, and the unit test and the
difftest both show no other child gets it. The key goes in as `-e NAME` only. The bare `"$dir"/*`
glob is handled: a missing directory falls to the "holds none" error. The entrypoint with
`PINGBUS_MEMBERSHIP=off` and `CCY_NO_SUPERVISOR=1` unsets any wrapper and ends in
`exec "$@"`, so opencode runs. The Dockerfile step checks the sha256 before it extracts, asserts
the version, and validates both JSON files with `jq`.

## Findings

1. **BLOCKING: an opencode session is not restored after a reboot, though the docs and the lib
   say it is.** `docs/ccy.md` (new "opencode" section, "the session record, which keeps the flag,
   so a restored session runs opencode again") and `lib/agent-choice.bash:4-5` both claim it.
   The record holds the launch's original arguments. Those have `--opencode` but not `--no-bus`,
   because `NO_BUS_MODE` is set inside the launcher, not in the argv. So
   `discover.LaunchedWithBus` (`src-go/internal/teams/discover/discover.go:187`) judges the
   record as one that carried the bus. `ccy-sessions restore` then calls `conversation.For`
   (`src-go/internal/sessions/restore.go:215`), which needs the pingbus hook's
   `.claude/ccy/pingbus/sessions/<name>/conversation.json`. An opencode session never writes it,
   so one of two things happens:

   - **No such file.** The restore fails for that session, and its message is about a
     conversation the hook did not record.
   - **A stale file from an earlier Claude bus session under the same reused tmux name.** tmux
     reuses names, and that file is not removed on the no-bus path
     (`lib/agent-bus-membership.bash:55-60` removes only the runtime membership directory).
     `registry.ResumeArgs` (`src-go/internal/registry/restore.go:107`) then appends `--supervise`
     and `--resume <claude-id>`. The launcher refuses that with 64 ("--opencode cannot be used
     with --supervise"), in a held pane.

   `registry.Supervised` also reports an opencode record as supervised. After a restore,
   set-going (`src-go/internal/sessions/setgoing.go`) waits for Claude's framed `❯` input box,
   which opencode never draws, and reports a fault after `SetGoingWait`. ccy-teams hot control
   also treats such a session as a bus session.

   **Fix:** pick one of two.

   - (a) Make restore understand opencode. `discover.LaunchedWithBus` is false and
     `registry.Supervised` is false for a record with `--opencode`/`-oc` before `--`.
     `sessions/restore.go` skips `conversation.For`/`ResumeArgs` for such a record and replays
     its arguments as they are, adding no `--supervise` and no `--resume`. set-going leaves the
     entry alone as expected. Add tests for each.
   - (b) If restore is out of scope for Phase 1, the launcher marks an opencode session's record
     no-restore, and the docs and the lib comment say an opencode session is not brought back.

   Either way, add a difftest or sessions test that proves the behaviour you choose.

2. **BLOCKING: `ccy --opencode` in a checkout whose Quick Launch saved a team fails late, with a
   message about the wrong flag.** With no `--token`, `--ssh-key` or `--network`, Quick Launch
   runs (`claude-yolo:1229`). The default answer (Enter, "y") sets `TEAMS_LIST="$LAST_TEAMS"`
   and runs `ccy_launch_teams`, which may open the seat chooser, then appends `--teams` to
   `CCY_ORIG_ARGS` and writes the seats into the session record with `ccy-teams record-seats`
   (`claude-yolo:1291-1296`). The launch carries on through the image build and every prompt.
   Only at `ccy_seats_take` (`claude-yolo:3892`) does `ccy_no_bus_check` refuse it with 64:
   "--no-bus leaves the agent team bus out ...", a flag the owner never gave. This is the
   ordinary path in a team checkout, this repository included, so the Task 1.4 host check is
   likely to hit it.

   **Fix:** in the Quick Launch block, handle an opencode launch as a restore or a headless
   launch is handled for the team. Set `quick_team=""` when `CCY_AGENT=opencode`, guard the
   `ccy_launch_teams` branch at 1291 with `"$NO_BUS_MODE" != true` (which also helps
   `--no-bus`), and change the line printed at 1272 to say the saved team is not taken
   (opencode is on no team). Add a difftest scenario: a world with `.claude/ccy/.last-launch.conf`
   holding `LAST_TEAMS`, plus `-oc` and a piped "y", whose golden shows opencode starting with
   `PINGBUS_MEMBERSHIP=off` and no `agent-bus`/`ccy-teams` seat calls.

3. **SHOULD-FIX: the "every launcher flag has a replay decision" gate does not see the new flag.**
   `scripts/test-ccy-session-registry.bash:166-167` finds the parser's flags by grepping only
   `$LAUNCHER` for `arg" == "--...`. `--opencode | -oc` is parsed in `lib/agent-choice.bash`'s
   `case`, so it is in none of the groups (`lib/session-registry.bash:122-127`, Go mirror
   `src-go/internal/registry/restore.go:43-46`), and the gate stays green. The flag is now
   classed as `unknown` (value_slot), so a bare word after it is replayed as its value.

   **Fix:** add `--opencode` and `-oc` to `CCY_REGISTRY_KEEP_FLAGS` and to Go's `keepFlags`. Make
   the derivation also read the `case` patterns in `lib/agent-choice.bash`, or move the flag
   test back into the launcher's `elif` chain so the grep sees it.

4. **SHOULD-FIX: a bare first word, the usual way to give claude a first message, becomes
   opencode's project argument.** `ccy -oc "fix the bug"` runs `opencode --auto "fix the bug"`
   (`lib/agent-choice.bash:79`, and `scripts/test-ccy-agent-choice.bash:71-73` asserts exactly
   this). opencode's TUI reads a positional argument as the project directory, so it fails or
   opens the wrong directory.

   **Fix:** in `ccy_agent_options`, refuse (64) a CLAUDE_ARG before the first flag that does not
   start with `-`, naming `--prompt`, or map it to `--prompt`. Document the choice in
   docs/ccy.md and test it.

5. **NIT: a key file that becomes unreadable after the check is handed on empty.**
   `local -x "$name=$(<"$CCY_ROOT/provider-keys/$name")"` (`lib/agent-choice.bash:86`) carries
   on with an empty value if the file has gone since `ccy_agent_options` checked it. Set -e is
   off there, because the call sits inside `|| container_rc=$?`. A file holding only a newline
   also passes the checks (`-s`, one line) and gives an empty key.

   **Fix:** read the value into a local first. Return 1 with `print_error` when the read fails or
   the value is empty, and reject a whitespace-only key in `ccy_agent_options`.

6. **NIT: session limits set by the project's `ccy.env` are not caught on the host.** The host
   check covers `--max-age`, `--run-for`, `--until` and `CCY_MAX_AGE`. A project's `ccy.env` may
   also set `CCY_LIFECYCLE_*` (entrypoint.sh:668), and an opencode session then stops in the
   container with "Install the hooks daemon in this project", which is misleading. It does fail
   fast, so this is a wording nit.

   **Fix:** in entrypoint.sh's no-wrapper branch, name `--opencode` when `$1` is `opencode`.

7. **NIT: a drive-by docs change in `.claude/skills/update-versions/SKILL.md`.** The
   manifest-format rewrite (to `vars/version-pins.yml`) is correct, since the file exists. It is
   an unrelated fix bundled into a feature commit, so mention it in the commit or plan journal.

## Not findings

- **The meta-deploy entry is right.** It folds into the existing `play-claude-yolo.yml` entry,
  names CCY 3.100.1 and container 2.79.1, and says what the new flag does and that a key file is
  needed. The versions match between the Dockerfile LABEL, `REQUIRED_CONTAINER_VERSION`,
  `CCY_VERSION`, the changelog and PLAN.md.
- **The rest of the wiring is in place.** The play's lib list, `CCY_LIBS`, the qa-gates row, the
  `addedgates_test.go` row, `help.txt` and the three `--help` goldens are all updated.
- **The bash limits pass.** `agent-choice.bash` has 4 functions, and the launcher did not grow
  past its allowlist entry.

VERDICT: FIX-FIRST
