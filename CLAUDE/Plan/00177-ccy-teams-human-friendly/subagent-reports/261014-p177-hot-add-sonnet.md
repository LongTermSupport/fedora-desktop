# Hot-adding a running ccy session to a team: findings and the plan written

No code was built. The first brief (restart-based add) was withdrawn by the owner: a
restart is not wanted. This is what was verified before that, and the no-restart design that
replaces it, written as Plan 00177 Phase 8 and Plan 00194 Phase 3 / DESIGN section 3.

## Verified (read from the code, nothing run)

- The in-place restart does not replay the registry record. The supervisor plugin
  (`ccy_lifecycle`, loaded only for `--max-age/--run-for/--until`) asks the supervisor to
  exit 75; the host launcher then relaunches with its own in-memory `CCY_ORIG_ARGS`
  (`ccy_handle_restart_exit`). Only a reboot restore replays the record. Nothing on the host
  can ask a session to restart, so a restart-based add was never available for sessions
  without that plugin.
- A restart is a new container (`--rm`), so `PINGBUS_*` are fixed at container start.
- `pingbus` is in every image, but linked onto PATH and the Claude plugin loaded only when
  `PINGBUS_SEATS` was passed. `PINGBUS_HOME` and `PINGBUS_TEAMS` are made by
  `pingbus seat exec`, which also holds the seat lock across the exec.

## Gaps listed as tasks

Plan 00177 Tasks 8.1 to 8.8 and Plan 00194 Tasks 3.1 to 3.3: plugin loaded in every
supervised session with a heartbeat; telling containers apart in a shared checkout (join
marker naming the container hostname, to verify on the host); `pingbus join` giving a
running container a session home, a held seat lock and an env file; the Go "Running
sessions" view with Add/Remove and `add-session`/`remove-session`; the age restart keeping a
joined team (record read on the relaunch line, without growing the ratchet files); docs.

## Owner must decide

- Loading the `ccy-agent-bus` plugin in every supervised session (not only `--teams`) is a
  launch-behaviour change.
- Sessions started before that ships, `--no-supervise` ones and old supervisors cannot be
  joined; the design refuses them with a reason and has no restart fallback.
- The Claude plugin extras (skill, sent echo) arrive at the joined session's next start.
