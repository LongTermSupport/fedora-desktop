# Task 2.0e: the restart marker, filed upstream

Filed upstream as hooks-daemon issue #127:
<https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/127>

## Procedure followed

The steps of `.claude/skills/hooks-daemon/issue-report.md`, in order:

1. **Configuration ruled out.** The only supervisor option is `ccy.deploy_supervisor`,
   which turns the supervisor on or off. Dry-run refuses before typing `/exit`, so it
   cannot produce the gap.

2. **Source read** (`.claude/ccy/claude-supervise.py` at 3.70.0):

   - `RestartCoordinator.request` types `/exit` and keeps the request only in memory.
   - `finish` writes `restart-request.json` and `restarted.json` only after the child
     has exited, so neither exists while SessionEnd hooks run.
   - `request` can refuse without telling the plugin: dry-run, cooldown, the abandon
     cap, or no single own session id.
   - `abandon` gives up after 30 s, again without telling the plugin.

   The generator only accepts a citation under `src/claude_code_hooks_daemon/`, and the
   supervisor script is not there. So the citation is the deployer
   (`install/ccy_supervisor.py:41`), as for #125, and the supervisor line numbers are in
   the Observed text.

3. **Version currency.** The installed 3.70.0 is the latest release.

4. **Generated** with `hooks-daemon issue-report --fields untracked/scratch/supervisor-restart-marker-fields.json --latest 3.70.0`.

   - The first run was refused: `/exit` in the reproduction read as an absolute path.
     The reproduction was reworded to "the exit command".
   - One unclear sentence was then fixed in the fields file and the body regenerated.
     The generated file was never edited.
   - I read the body before filing. It contains no config, logs, absolute paths or
     install names.

5. **Filed** with `gh issue create --repo ... --body-file untracked/issue-reports/issue-report-20261019-132442.md`.

## Duplicate check

None of the existing issues covers this:

- #119: plugin API 1.1 Relay
- #124: dispatch pool saturation
- #125: injections go to the focused thread
- #126: wrapper interpreter prefix
- #71 (closed): the plugin system

Searches for "restart exit supervisor", "restart-request", "SessionEnd restart marker"
and "ExitForRestart" found nothing else relevant.

## What #127 proposes

1. Before typing `/exit`, once every refusal check has passed, the supervisor writes a
   private record of the pending restart (`restart-pending.json`: session_id, plugin,
   reason, requested_at, deadline).
2. It exports the record's path to the child, for example through
   `CCY_SUPERVISOR_RESTART_PENDING`.
3. It removes the record when it abandons the restart, or when typing `/exit` fails.
4. Readers trust the record only when it names their own session id and its deadline has
   not passed.

## What remains in Task 2.0e

This waits on an upstream release carrying #127. The project side then does the
following, test first:

- The SessionEnd hook reads the record and skips the goodbye for that session.
- It records the goodbye as said (`seat.record_goodbye`), so the launcher's
  `seat disconnected` stays quiet too.
- `docs/agent-bus.md` describes today's behaviour until that release.
