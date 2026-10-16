# Review: watcher survives a homeserver restart and runs supervised (Plan 00194 Task 2.0d, Plan 00203 M0)

Reviewed: `git diff F44...origin/fix/watcher-survives-homeserver-restart` (tip `28ec8b1d`,
F44 at `2e164edf`). Read-only; no `qa-all`. The developer's report:
`subagent-reports/261016-watcher-restart-opus.md`; the audit it answers:
`CLAUDE/Plan/00203-teams-hot-control-and-robustness/subagent-reports/261016-robustness-audit-opus.md`
(gaps 1, 2, 5).

## Verdict: FIX-FIRST

Two blocking findings, both small. The rest of the design holds: delivery stays exactly-once
across a retry, the session watch lock makes two watchers impossible, the fault classes are
right, and the tests go red without the fix.

### Blocking

**B1. The supervisor makes an orphaned watcher immortal, and it holds the team's sync locks.**
`_supervise` (`helpers/pingbus/cli.py`) judges the session alive with `notify.socket_present`,
a `stat`. The child, by contrast, learns the session is gone from `ECONNREFUSED` on a
delivery (`notify._GONE_ERRNOS` → `SocketGone` → exit 0). When a session dies without
unlinking its socket file (a SIGKILLed `cc --teams` claude, an OOM kill: audit gap 7), the
child exits 0 at the first message, the supervisor sees the file still there, and since 0 is
not in `SUPERVISE_FINAL` it restarts the child. The child syncs, takes the team locks again,
waits for the next message, exits 0 again, and so on, with a 1 to 60 s backoff, for ever.
On F44 that orphan died at its first message; on this branch it never dies, and while it
lives the seat's new session is refused the team lock ("held") and gets no delivery. The
report lists "stat, not connect" under Not done, but misses that the supervisor turns a
one-message orphan into a permanent one.

Fix, one line: add `EXIT_OK` to `SUPERVISE_FINAL`. `_watch` returns 0 only when the socket is
gone (missing before start, `socket_present` false at a tick, or `SocketGone` from a write),
so an exit 0 is always "the session ended" and never worth a restart. Keep the
socket-present check as well for the case where the child was killed by signal. A test:
child code `0` with the socket file still present ends the supervisor with exit 0 and starts
no second child (today `test_a_socket_that_is_gone_ends_it_whatever_the_child_said` covers
only the file-gone case).

**B2. Merge conflict with F44 in `CLAUDE/Plan/meta-deploy.list`.** `git merge-tree F44 <tip>` conflicts: F44 (Plan 00201 team shutdown) and this branch both appended a
`play-claude-yolo.yml` entry after the same line. `docs/agent-bus.md` auto-merges. Merge F44
into the branch and resolve (keep both entries; the F44 one says "no image rebuild", this one
needs the rebuild, so order them so the owner reads the rebuild line). Then the F44 version
gate runs on the merge commit, which it never has on this branch (its commits predate the
gate, `443c29b7`).

### Checks, in the order asked

**1. Exactly-once.**

- Retry: `inbox.TeamState.commit_batch` writes items (skipping `known_event_ids`, which
  includes `consumed/`), fsyncs, then saves the token. A fault before the token save replays
  the events on the next sync from the old token and the known set drops them. A fault after
  it loses nothing. `test_a_homeserver_restart_drops_no_team_and_delivers_once` exercises
  this with 502/503 injected on `/sync` and asserts exactly two payloads for two messages.
  Sound.
- Supervisor restart: the new child resumes from the stored token and the inbox on disk, so
  no loss. The one duplicate window is the one SPEC-inject E11 already has: payload written
  in full, process killed before `consume`. The skill text already tells the agent "a
  repeated `id:` is the same message again (after a restart)". Not new, not blocking.
- Watcher killed mid-delivery: same window, same at-least-once answer. No test covers it on
  either side of this change.
- New duplicate path, non-blocking (M1): `_watch` now contains any exception from
  `_inject_pass` and tries again next tick. If `_send_delivery` wrote the payload in full and
  then `consume` (a rename plus two directory fsyncs) raised, the item stays pending and is
  written to the socket again every pacing interval (2 s) until the disk recovers. Rename in
  one filesystem rarely needs space, so this is an edge, but the shape is "a disk fault
  floods the conversation with the same message". Remembering "written, not yet consumed"
  event IDs in the `_Injector` across ticks would close it.

**2. No two watchers.** The session watch lock is `flock` on `inject/<key>.watch`, never
removed, created `0600` with `O_NOFOLLOW`. The kernel releases a `flock` when the last open
file description closes, so a killed supervisor or child leaves no stale lock; a stale
*file* holds nothing (`probe_lock_at` takes `LOCK_SH|LOCK_NB` and reads the holder kind only
when that fails). The supervisor marks the fd inheritable and runs the child with
`close_fds=False`, so the child shares the open file description: a supervisor killed on its
own leaves the child counted, and the hooks' `_revive` sees the lock held and does nothing.
A second `watch --supervise`, or a plain `watch`, exits 75 before touching a team. Two hooks
spawning at once (prompt and Stop in quick succession) are arbitrated by the lock: the
second supervisor exits 75 with one `watch.log` line. `--child` run by hand skips the lock
(it is `SUPPRESS`ed in help); acceptable. Verified by `test_it_holds_the_sessions_watch_lock_while_the_child_runs`,
`test_a_second_supervisor_is_busy_and_starts_nothing`, and the hooks tests. Sound.

**3. Dies with the session.** The supervisor runs in its own session
(`Popen(start_new_session=True)`), so it is not in Claude Code's process group and nothing
signals it; its only tie to the session is the socket file. On a clean exit Claude Code
removes the socket, the child exits 0, the supervisor sees the file gone and exits 0, and
the lock is released. On an unclean exit with the file left behind: B1. The child is in the
supervisor's process group; `subprocess.run` does not forward a SIGTERM to it, which is the
documented "the watcher stays counted" behaviour and is fine.

**4. Fault classification.** `MatrixError.transient` is true for a refused or dropped
connection, a read failure, any 5xx (a proxy's HTML 502 included, since the status decides),
and a 429 past the client's own retries; false for 401, 403, 404, a refused redirect, an
oversized or non-JSON answer. `_permanent` returns `not transient` for a `MatrixError` and
`isinstance(exc, FAILURES)` otherwise, so `ConfigError`, `StateError`, `Untrusted`,
`Refusal` and `Busy` drop the team, and everything else (`OSError` ENOSPC, a `ValueError`)
is retried. Right for the stated classes. Two notes:

- No login happens anywhere in pingbus: the Matrix client uses the bundle's access token;
  `/sync` with a revoked token is a 401 on the request, not a login attempt, so no retry can
  trip Tuwunel's login rate limit. Confirmed by grep: the only "login" in `helpers/pingbus`
  is `gh auth login` in `forge.py`.
- Non-blocking (M2): "any other error is retried" means a programming error in the sync path
  (a `KeyError`, a `TypeError` from a malformed event) is reported once as
  "this team's sync failed (TypeError)" and then retried every 30 s for the session's life,
  indistinguishable in `watch.log` from an outage. The audit's F1 asked for "re-raising
  truly unexpected exceptions only after logging them, and let F2 restart". Acceptable for M0
  since the outage line names the exception type, but a counter (give up on a team after N
  consecutive non-Matrix, non-OSError failures) would make a bug visible.
- Non-blocking (M3): after a permanent refusal drops the only team, the child exits 8 and the
  supervisor exits 8 (final). The hooks then see a free watch lock and a team with no waker,
  and after `WATCHER_RESPAWN_EVERY_S` (300 s) respawn a supervisor that hits the same 401 and
  exits again, telling the agent "watcher ... was started again" on every fifth-minute prompt
  and blocking Stop with `watcher_failed` once per 600 s (which tells it to run `pingbus wait`,
  which exits 8 too). One `/sync` per 5 min with a bad token is harmless to the server, but
  the agent is told the wrong thing in a loop and nobody is told "401". Audit gap 8 (F7)
  owns the fix; a `.refused` mark that `_revive` honours would stop the loop now.

**5. Disk full and log flooding.**

- A per-team outage writes exactly two lines (start, "the team syncs again"); the backoff
  resets only on success, so a long outage is silent in between. A flapping homeserver
  (success then fault each second) writes two lines per flap, bounded by the 1 s floor.
  Acceptable.
- The supervisor writes one line per restart; the backoff resets only when the child ran
  `supervise_retry_max_s` (60 s) or more, so a child that dies at once settles at one line
  per minute, 1440 a day, for as long as the cause lasts. Exit 75 (every team busy, e.g. a
  hand-run `pingbus wait` holding the seat) is retried this way; reasonable, since a waiter
  ends. Tolerable; a `restarts` counter and a longer cap (F6) would be better.
- A delivery pass failure is logged once per streak and once on recovery. Good.
- ENOSPC: `_watch_poll` retries a team's state write; `consume` is a rename, which works on a
  full disk; `marks.set_spawned` on a full disk makes the hook report `watcher_failed` even
  though the `Popen` succeeded (the `except OSError` spans both), a cosmetic misreport (M4).
  `Marks.ensure_dir` failing in `_session_watch_lock` exits the supervisor 1, and the hooks
  respawn after 300 s; fine.
- `_describe` prints an `OSError`'s `strerror` or the exception type only, never a message
  body. Good.

**6. Versions.** `files/var/local/claude-yolo/Dockerfile` LABEL 2.66 → 2.67,
`REQUIRED_CONTAINER_VERSION` 2.66 → 2.67, `CCY_VERSION` 3.98.11 → 3.98.12, changelog entry
present, meta-deploy entries present. F44 is still at 2.66 / 3.98.11, so 2.67 is the next
number. The F44 gate (`scripts/git-hooks/pre-commit`, `CCY_IMAGE_PATHS` covers
`helpers/pingbus` and `helpers/agent_bus`, plus every `COPY` source under the two context
roots) would pass: staged LABEL equals staged REQUIRED and differs from HEAD's. No file
under `files/opt/claude-yolo` changed, so the plugin's `1.2.3` stays; the SKILL.md wake text
("Without the socket ... run `pingbus wait`") still describes the socketless case, which is
the only case the nag now covers, so no drift. The docs table rows in
`docs/agent-bus-protocol.md` match the code (75 for a second watcher; final codes 8, 10,
64, 78; 1 to 30 s and 1 to 60 s).

**7. Merge.** B2. Nothing else on F44 since the merge base `0e53ac69` touched
`helpers/pingbus`, `files/opt/claude-yolo` or `files/var/local/claude-yolo`
(`git diff --stat 0e53ac69 F44 -- ...` is empty), so no semantic conflict with team
shutdown. Plan 00203's spec H11 and section 7 take this branch "as it is" (per-team
backoff, first sync in the thread, `--supervise`, the watch lock under `PINGBUS_HOME/inject/`,
the hooks' revive) and build M1's keeper on top; B1's one-line change does not disturb that.
The spec's one pre-merge ask, "check its SPEC-inject E-numbers", is met: E25 and E26 are new
and do not clash.

**8. Tests.** The branch's `tests/helpers/pingbus` passes (1352 tests). With the branch's
tests over F44's `helpers/pingbus` in a scratch worktree: `TransientTest` 5/5 red,
`WatchOutageTest` 6/7 red (`test_a_rejected_token_still_drops_the_team` passes on F44 by
design), `WatchSuperviseTest` 7/7 red, `WatcherRestartTest` 7/10 red (the three that pass
on F44 assert "nothing happens", which F44 also does). The developer's "27 of 29" count
matches. One caveat (M5): `WatchRefusalTest::test_a_second_watcher_of_the_same_session_is_busy_even_on_a_free_team`
does not fail on F44, it hangs: `run_cli("watch")` there becomes a real watcher in the test
thread and never returns, so `pytest -x` stalls instead of going red. The suite has no
per-test timeout, so a regression of the session lock would hang CI rather than fail it.
Running that `watch` in a thread with a bounded join (as `start_watch` does) would make it a
real red. Not covered anywhere: a watcher killed between the socket write and `consume`, and
a supervisor-restarted *real* child resuming delivery (the supervise tests use a fake
`run_child`); `test_the_child_runs_under_its_parents_lock` is the nearest.

### Minor (not blocking)

- M1 to M5 above.
- `_Backoff.next` applies `min(wait, cap_s)` to a value already capped by construction except
  for the first call when `first_s > cap_s`; harmless.
- `watch_argv` raising `FileNotFoundError` is mapped to `ConfigError` (78) in `_supervise`
  only after the lock is taken; fine, the lock is released in `cmd_watch`'s `finally`.

### What to do

1. Add `EXIT_OK` to `SUPERVISE_FINAL` with the test described in B1.
2. Merge F44 into the branch, resolve `meta-deploy.list`, let the gate run.
3. Optionally fold M5 (bounded join) into the same fix.
4. Re-review is not needed after 1 and 2; M1 to M4 go to Plan 00203 (F6, F7).
