# Plan 00203 M1: pingbus (Opus build notes)

Branch `feat/hot-control-m1-pingbus`, from F44 (with M0), with M2
(`origin/feat/hot-control-m2-launcher`, 235c8cc2) merged in.

## What was built

| Spec       | Where                                            | Notes                                                                                                                                                                                                     |
| ---------- | ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 3.1, 3.2   | `helpers/pingbus/membership.py`                  | The one reader of both files. M2's `seat.parse_membership` is folded in; the stricter checks of both are kept.                                                                                            |
| H4, H8     | `config.active_teams`                            | With `PINGBUS_MEMBERSHIP`: held seats in `applied.json` ∩ desired, desired order, every call. `select_teams` now says "not a member of `<team>`" (exit 64).                                               |
| D4, H9     | `seat.Keeper`, `seat.keep`, `pingbus seat keep`  | The only seat lock holder. Reconciles on a change of the desired file's inode, size or mtime (0.5 s tick); a refusal is recorded with its `why` and does not stop the others; a launch is all or nothing. |
| D4 LOCK_UN | `inbox.Lock.release`                             | Truncate, `LOCK_UN`, close. Test: a dup of the descriptor no longer keeps the lock.                                                                                                                       |
| H10        | `hooks._keeper_lines`, template `keeper_stopped` | SessionStart and prompt say so while `keeper.lock` is free. The `active` flag stays after a keeper death, so the guarded hooks still run and say it.                                                      |
| 3.3        | `Keeper._publish`                                | `<checkout>/.claude/ccy/pingbus/sessions/<session>/applied.json`, written at start and on each change; failures are logged, never fatal.                                                                  |
| H33 flag   | `Keeper._flag`                                   | The keeper owns `active`; `seat exec` writes M2's settings and inner status line after the keeper is ready.                                                                                               |
| empty      | `seat exec`                                      | Valid under the membership: home, keeper, settings, nothing held, gh not asked.                                                                                                                           |
| H23 step 5 | `pingbus seat retire [--root DIR] SEAT@TEAM`     | Holds the seat lock throughout; clears `sync.json`; publishes `until = now`. Homeserver down: the position is still cleared, the exit is the Matrix one.                                                  |
| H22 step 5 | `pingbus reach --team T [--root DIR]`            | Unauthenticated `GET /_matrix/client/versions`, 3 s, to the base URL of the team's first seat in the checkout.                                                                                            |

## How `seat exec` and the keeper meet

- `seat exec` reads the source (`desired_source`: membership or `PINGBUS_SEATS`, never
  both), checks the settings and the forge credential (when a seat is wanted), then spawns
  `pingbus seat keep --launch --parent <own pid> --root R --home H` in its own session.
- The keeper makes the home, takes `keeper.lock`, applies strictly, moves its stderr to
  `keeper.log`, prints `READY`, and loops. A refusal is its exit code and stderr, which
  `seat exec` passes on (75, 78) unchanged.
- The keeper dies with its parent through `PR_SET_PDEATHSIG` (ctypes `prctl`); a `getppid`
  poll is the backstop. So on the host a `cc --teams` seat is free the moment claude ends.
- How pingbus re-runs itself: the zipapp it was loaded from, else the source tree
  (`cli.pingbus_argv`), independent of `sys.argv[0]`.

## Interface notes for M2 and later

- `PINGBUS_TEAMS` is still set for a seated membership session (M2's behaviour), because
  `statusline.py` reads it until M3's H14. `active_teams` ignores it when
  `PINGBUS_MEMBERSHIP` is set.
- `session` in the desired file must match `[A-Za-z0-9][A-Za-z0-9._-]{0,127}` (it names
  the report directory). M2's Go writer should produce registry names inside that.
- `farewell.reason` is a closed set: `removed` only. M4 adds `moved` if a move needs its own words.
- Repair (`podman exec -d <c> pingbus seat keep`, no `--parent`) adopts the existing home
  and re-takes the desired seats, clearing links no seat backs.
- A seat refused as `held-elsewhere` is retried only when the desired file changes.
- `told` in `applied.json` is carried over by the keeper; M3 writes it. Two writers of one
  file then race; M3 should give `told` its own file or have the keeper own every write.

## Verification

- `test_membership`, `test_seat`, `test_seat_retire`, `test_inbox`, `test_hooks`, `test_config` green;
  `./scripts/qa-helper-tests.bash` run for the commit.
- `scripts/test-ccy-agent-bus.bash` 77/0 (it replays the image's real `seat exec`, so the
  keeper runs from the zipapp), `scripts/test-cc-teams.bash` 69/0,
  `scripts/test-ccy-teams.bash` 185/0.
