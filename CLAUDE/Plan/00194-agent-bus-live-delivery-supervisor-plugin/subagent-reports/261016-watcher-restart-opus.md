# Watcher survives a homeserver restart, and runs supervised (Task 2.0d)

Branch `fix/watcher-survives-homeserver-restart`, from F44. SPEC-inject E25 and E26 (the
first draft cited E20 and E21, which clash with existing expectations; renumbered after the
Plan 00203 robustness audit).

## First check: did current F44 still drop a team?

Yes. `_watch_poll` handed every exception to the main thread, which reported the team as "no
longer watched" and exited once no team was left. `matrix.Client.call` mapped a refused
connection and a 5xx to `MatrixError` with nothing to tell them from a 401. The first
`sync_once(0)` and `publish_status` also ran in `cmd_watch`'s main thread before any retry, and
any non-`FAILURES` exception (a full disk) was re-raised there, ending every team.

## What changed

- `matrix.py`: `MatrixError.transient`, true for no connection or a read failure, any 5xx (a
  proxy's HTML 502 too) and a 429 past its retries; false for 401, 403, 404 and a malformed
  or oversized answer.
- `cli.py`:
  - `_watch_poll` runs each team's first sync and status publish itself, so a homeserver
    that is not answering at start is waited for.
  - A transient fault, or any other error in one team (`OSError` ENOSPC included), is
    retried with `_Backoff` (1 s doubling to 30 s, `Runtime.outage_retry_*`) from the stored
    sync token. Each item is stored once, because the token commits only with its batch.
  - `watch.log` gets one line when the fault starts and one ("the team syncs again") when a
    full iteration succeeds.
  - Only `_permanent` failures are handed to the main thread and drop the team: non-transient
    `MatrixError`s and the other `FAILURES` classes.
  - A failing delivery or relay pass in the main loop is logged once and retried on the next
    tick.
- One watcher per session: `inject/<key>.watch` is the session's watch lock, taken by
  `watch` and by `watch --supervise`. A second one exits 75 even on a free team.
- `watch --supervise` holds that lock, makes it inheritable, and runs `watch --child` (which
  skips the lock). It restarts the child after any exit except the socket going or 8, 10, 64
  and 78, with a backoff of 1 s doubling to 60 s (reset after a long run). If the supervisor
  is killed on its own, the child still holds the lock, so no duplicate starts.
- `hooks.py`:
  - `spawn_watcher` starts `watch --supervise`.
  - UserPromptSubmit and Stop call `_revive`: when there is a socket, some team has no waker
    and the session's watch lock is free, they start it again and say `watcher_restarted`
    once (in additionalContext, or as Stop's systemMessage).
  - A start is given 30 s. One that died again within 300 s is reported as `watcher_failed`
    (Stop blocks once per window), not restarted in a loop.
  - The `no_waker` "run `pingbus wait`" block is kept only for sessions with no socket.
  - The spawn time is kept in `inject/<key>.spawned`; SessionStart writes it too, and
    `forget` removes it.

## Tests (red on F44 first)

Of the new tests, 27 of 29 failed or errored against F44's helpers (checked in a scratch copy).
New test classes:

- `test_matrix.TransientTest`
- `test_notify.WatchOutageTest`: restart mid-session with exactly-once delivery, a homeserver
  down at start, ENOSPC in one team, a failing pass, the backoff sequence, and a 401 still
  dropping the team
- `test_notify.WatchSuperviseTest`
- `test_hooks.WatcherRestartTest`

Existing tests changed only where the behaviour is meant to change: the no-waker Stop tests
and two relay off-mode tests now run with no socket, and PromptTest holds the session's watch
lock. Every pingbus test module passes; ruff is clean.

## Not done (left for Plan 00203)

- `pingbus wait` (`_long_poll`) still ends on a transient fault.
- The `watch-state.json` restart count (F6) is not written.
- The supervisor checks the socket with `stat`, as before, not a connect, so the
  orphan-on-host case (gap 7) is not closed.
- Plugin version is not bumped: no plugin file changed. The code ships in the image's
  zipapp (container 2.67, CCY 3.98.12) and the kit.

## Deploy

Queued in `CLAUDE/Plan/meta-deploy.list`: `play-claude-yolo.yml` (image) and
`play-agent-bus.yml` (kit).

Owner's check, after the deploy:

1. With a team session idle, restart its homeserver (`systemctl restart agent-bus-hs@<team>`).
2. Post from Element: the session is woken with no keypress, and `watch.log` shows one outage
   line and one recovery line.
3. Kill only the `pingbus watch --child` process: it comes back within about a minute.
