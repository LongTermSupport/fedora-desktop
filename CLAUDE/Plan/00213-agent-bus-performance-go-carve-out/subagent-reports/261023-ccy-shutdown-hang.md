# ccy session shutdown: what runs between /exit and the container being gone

Triage only. Nothing was edited or committed. Session, team and seat names are anonymised
(session A, session B, team T). Everything measured here was read-only. The only commands run
were the hooks-daemon SessionEnd forwarder (with a synthetic `reason: clear` payload, which no
daemon handler acts on), `pingbus hook session-end` (`reason: clear`, with a scratch
`PINGBUS_HOME`, so it posted no goodbye), `pingbus --help`, `pingbus --team T reach` and
`pingbus --team T history --limit 1`.

Host load during the measurements: load average 96 on 22 CPUs, from several parallel Go QA runs.
The absolute numbers are therefore pessimistic.

## 1. Evidence from this checkout's own logs (the strongest data)

Three files are written at three different points of the teardown, and all three are readable
from inside the container:

- `<seat>/said-goodbye.json` is written in-container at the moment the SessionEnd goodbye is
  posted (its mtime is when that happened).
- `untracked/supervise/decision.log` gets a `supervisor exiting` line when claude has exited and
  the PTY supervisor (PID 2, under tini) returns. The container ends right after that.
- `.claude/ccy/pingbus/disconnect.log` is written on the host by the launcher's EXIT-trap step
  `ccy_bus_disconnected`, after `podman run --rm` has returned.

| exit             | first Ctrl-C | goodbye recorded | supervisor exits | host disconnect line | supervisor exit to host line       |
| ---------------- | ------------ | ---------------- | ---------------- | -------------------- | ---------------------------------- |
| session A, today | 10:10:26.87  | 10:10:28.89      | 10:10:29.24      | 10:10:30             | **0.8 s**                          |
| session B, today | 10:19:59.68  | 10:20:02.09      | 10:20:02.70      | 10:20:59             | **56.3 s**                         |
| earlier exits    |              |                  |                  |                      | 2.1, 3.4, 12.0, 12.0, 15.1, 41.7 s |

What this shows:

- **Inside the container the teardown is fast and bounded: about 2.5 to 3 s from Ctrl-C to the
  supervisor exiting, team goodbye included.** The goodbye is posted about 0.5 s before claude
  exits.
- **The long, variable wait comes after the container's PID 1 has exited, on the host:** 0.8 s
  at best, 12 to 56 s at worst. Every `disconnect.log` entry says `not told: goodbye already posted`. So the host's `pingbus seat disconnected` posted nothing in any of these runs, and the
  host-side network path did not cause the delay.
- During session B's wait the owner pressed Ctrl-C four more times (10:19:59 to 10:20:00.9). That
  is the 2 to 3 s in-container phase being felt as slow. The Ctrl-C guard swallows every other
  press.

## 2. The chain, step by step

| #   | step                                                                                                                                                                                                                                               | waits on                                                                                                                                                     | bound                                                                                                                                                                                                                                                                                                                     | what makes it slow                                                                                                                                                                                                                                                                                                       |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Claude Code runs the SessionEnd hooks in parallel: the project's `.claude/hooks/session-end` (hooks-daemon forwarder) and the kit plugin's `pingbus hook session-end` (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json`) | both hook processes                                                                                                                                          | Default budget 1.5 s (vendored `hooks.md`, SessionEnd). The kit's settings set `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000` (`seat.py` `SESSION_END_BUDGET_MS`), which is the total budget and also each untimed hook's timeout. The plugin hook's own `timeout: 6` applies to that hook but does not raise the budget. | Measured: forwarder 0.26 to 0.30 s. `pingbus` zipapp startup alone is 1.0 to 1.2 s under this load.                                                                                                                                                                                                                      |
| 1a  | pingbus goodbye (`hooks.py` `_post_all`, through `cli.post_goodbye`): `verify_room`, then a send per seat, one thread each                                                                                                                         | the homeserver                                                                                                                                               | `GOODBYE_TIMEOUT_S = 4.0` for all seats together (daemon threads, so they are abandoned at the bound). Each request has `matrix.REQUEST_TIMEOUT_S = 15`, but the 4 s cap governs.                                                                                                                                         | A slow or blackholed homeserver costs the full 4 s plus startup, about 5 s, which is under the 6 s per-hook timeout. A server that is down refuses at once (fast). Measured `reach` 1.0 to 1.6 s and `history` 1.3 to 1.7 s, mostly process startup. `_first_goodbye` makes the goodbye once per session, with no retry. |
| 1b  | hooks-daemon SessionEnd                                                                                                                                                                                                                            | the daemon socket; `ensure_daemon` starts the daemon if it is down                                                                                           | 8 s (untimed, so it gets the env value)                                                                                                                                                                                                                                                                                   | Nothing here: no daemon handler subscribes to SessionEnd (`handlers/session_end/` holds only `__init__.py`). It is slow only on a cold daemon start.                                                                                                                                                                     |
| 2   | Claude Code's own exit: background shells, LSP servers, transcript flush                                                                                                                                                                           | its children                                                                                                                                                 | Background commands are force-killed 1.5 s after being asked to stop (vendored changelog)                                                                                                                                                                                                                                 | Seen within the 2.5 to 3 s total. No child of claude holds the PTY (checked: `/proc/*/fd`), so the supervisor's master gets EOF at once.                                                                                                                                                                                 |
| 3   | Supervisor: `waitpid`, `restart.finish`, `worker.close()`                                                                                                                                                                                          | the worker                                                                                                                                                   | `terminate` then `wait(1.0)` then `kill`; all its threads are daemon threads                                                                                                                                                                                                                                              | At most about 1 s.                                                                                                                                                                                                                                                                                                       |
| 4   | tini (PID 1) exits when its child (the supervisor, exec'd by `entrypoint.sh` through `pingbus seat exec`) exits. The kernel then SIGKILLs everything left in the PID namespace (pingbus watcher, hooks daemon, keeper, any go/QA processes).       | the kernel reaping                                                                                                                                           | none                                                                                                                                                                                                                                                                                                                      | Large or IO-bound leftovers (for example a running `go test -race`) take a while to reap under load.                                                                                                                                                                                                                     |
| 5   | `podman run --rm` (attached, `-it`) returns: conmon reports the exit, then removal (overlay unmount, network teardown, container record)                                                                                                           | podman's locks, storage, netns                                                                                                                               | none set by ccy                                                                                                                                                                                                                                                                                                           | **Prime suspect for the 12 to 56 s.** It is not observable from inside the container. The exit code is passed through.                                                                                                                                                                                                   |
| 6   | Launcher after the container (`claude-yolo` lines 3923 to 3988): an exit code of 75 goes to `ccy_handle_restart_exit` (image update, then `cleanup`, then exec); the compose stop prompt runs if compose was started                               | `update_claude_inplace`; a person, for the compose prompt                                                                                                    | none                                                                                                                                                                                                                                                                                                                      | A supervisor restart includes an image update before `cleanup`, so it is slow by design. No restart line appears in the decision log for these exits, so this probably does not explain them.                                                                                                                            |
| 7   | EXIT trap `cleanup` (`claude-yolo` line 2378): stty, rm, `_probe_agent_stop`, `ccy_agent_filter_stop`, then `ccy_bus_disconnected`                                                                                                                 | `ccy_agent_filter_stop` does `kill` then an **unbounded** `wait`                                                                                             | none, although the filter handles SIGTERM                                                                                                                                                                                                                                                                                 | Normally fast.                                                                                                                                                                                                                                                                                                           |
| 7a  | `ccy_bus_disconnected` (`lib/agent-bus-membership.bash`): `podman ps -q --filter name=`; if the container still runs (hangup or TERM), `podman stop -t 10`; then `timeout 60 pingbus seat disconnected`                                            | podman; seat locks (poll every 0.5 s for up to `disconnect_wait_s` = 15 s **per seat, one seat after another**); the homeserver when no goodbye was recorded | 60 s overall                                                                                                                                                                                                                                                                                                              | Normally under 1 s (the session A row). It is 10 s or more only after a hangup or SIGTERM, or when a seat lock is still held. Up to 60 s when the goodbye failed and the homeserver is slow. None of these happened in the logged runs.                                                                                  |
| 8   | tmux trampoline (`session-registry.bash` `ccy_registry_trampoline`): removes the record; **if the exit code is not 0, holds the pane at "Press Enter to close this session."**                                                                     | a person                                                                                                                                                     | none                                                                                                                                                                                                                                                                                                                      | Reads as a hang if the pane is not watched. Check what exit code a Ctrl-C/Ctrl-C exit leaves.                                                                                                                                                                                                                            |
| 9   | Only with `ccy-teams shutdown` / wind-down (`src-go/internal/teams/shutdown`, `winddown`): types /exit and polls tmux                                                                                                                              | panes, seats                                                                                                                                                 | Prompt 30 s, Exit 60 s, Release 30 s (up to about 120 s per run). Wind-down Idle is **300 s**, and a busy session is never interrupted.                                                                                                                                                                                   | Slow by design when a session is busy, or its screen is misjudged (NotDrawn). `checkSeats` runs a host `pingbus seat list` (Python startup) every second.                                                                                                                                                                |

On `podman stop` (a hangup, ccy-sessions End, or a host shutdown): tini forwards SIGTERM to the
supervisor. The supervisor installs no SIGTERM handler (only TSTP, QUIT, TTIN and TTOU are
guarded), so it dies at once. tini exits, and the kernel kills claude **without running
SessionEnd**. The 10 s grace is therefore rarely used. The goodbye is never sent, and the host
posts `disconnected` instead: a network post, bounded by 60 s.

## 3. Likely causes, ranked

1. **Host-side container teardown (steps 4 and 5): `podman run --rm` returning late.** Evidence:
   12 to 56 s gaps between `supervisor exiting` and the host's disconnect line, while the host's
   own disconnect step did nothing. Contributors to suspect: podman lock or storage contention
   with the other running ccy containers and host `podman` callers, network/netns teardown, and
   reaping heavy leftover processes in the container (QA runs that the session left running in
   the background) at a load of 96. The gaps vary with host activity, not with the bus.
2. **The bus's in-container cost (step 1a), about 1.5 to 2.5 s per exit, up to about 5 s with a
   slow homeserver.** It is real but bounded. Most of it is the Python zipapp starting under
   load. This is the part that "the teams stuff" adds. It is noticeable (the owner pressed Ctrl-C
   repeatedly) but it does not explain waits of tens of seconds.
3. **The window held at "Press Enter" (step 8)** if the exit code is not 0, and the compose
   prompt (step 6). Both look like hangs and both need a person.
4. **`ccy-teams shutdown` and wind-down bounds (step 9):** up to about 120 s and 300 s by
   design.
5. A hangup or `podman stop` path (`stop -t 10` plus a `disconnected` post, at most 60 s).
   Possible but not seen in the logs.

## 4. What the host agent should measure (read-only)

Run this around a throwaway ccy session in this checkout. Use either `--no-bus`, or a seat on a
scratch test team, so that no real team is told anything.

```bash
# Terminal 1: container lifecycle events with nanosecond times, bounded to 5 minutes
timeout 300 podman events --filter label=ccy=true --format '{{.Time}} {{.Status}} {{.Name}}' | cat

# Terminal 2: what the launcher and podman are doing, every 0.5 s, bounded to 3 minutes
timeout 180 bash -c 'while sleep 0.5; do date +%T.%N; pgrep -af "[c]laude-yolo|[p]odman (run|ps|stop|rm)|[p]ingbus seat|[c]onmon" | cut -c1-160; done' | cat

# Terminal 3: the in-container markers, live
timeout 300 tail -F untracked/supervise/decision.log .claude/ccy/pingbus/disconnect.log | cat
```

Then, in the throwaway session, note `date +%T.%N` and exit the way the owner does (Ctrl-C
twice, or /exit). Wait for the pane to close or show "Press Enter". Afterwards:

```bash
uptime
stat -c '%n %y' .claude/ccy/pingbus/seats/*/*/said-goodbye.json
journalctl --user --since '-5 min' --no-pager -o short-precise | grep -E 'conmon|podman|libpod' | cat
podman ps -a --filter label=ccy=true --format '{{.Names}} {{.Status}}' | cat
```

Read the run as follows:

- From `supervisor exiting` to the `died` event is container exit (step 4).
- From `died` to `remove`, and to the moment `podman run` vanishes from the samples, is podman
  teardown (step 5).
- From then to the `launcher ... exited 0` line in disconnect.log is the launcher's cleanup
  (step 7).
- Repeat once with the host idle and once while other ccy sessions run QA, to confirm that load
  and contention drive step 5.
- Repeat with `--no-bus` to put a number on the bus's share.
- Note whether the pane says "exited with status N. Press Enter".

## 5. Proposed fixes, and where each would go

1. **Do not make the person wait for the host's post-exit work.** In `ccy_bus_disconnected`
   (`files/var/local/claude-yolo/lib/agent-bus-membership.bash`), run `pingbus seat disconnected`
   detached (`setsid`, its own process group, output to disconnect.log). Keep the 60 s
   `timeout`. The terminal-gone handling already exists. This needs a CCY version bump.
2. **If step 5 is confirmed:** look at what holds podman's teardown. For example, have the
   session's long-running background jobs stopped by Claude Code before exit. Have
   `podman run --rm` not wait on removal: run without `--rm`, then `podman rm` in the background
   after the exit code is read. `docker-health.bash` already sweeps leftovers. Both changes are in
   `claude-yolo` around line 3881, with a CCY version bump.
3. **Make SessionEnd network-free.** The in-container hook writes its reason locally, the host
   step (fix 1, detached) posts the goodbye, and `disconnected` covers a missing record. Changes
   go in `helpers/pingbus/hooks.py` `session_end` and `cli.py` `cmd_seat_disconnected`. This
   saves about 0.5 to 4 s per exit and removes the homeserver from the critical path. Short of
   that: lower `GOODBYE_TIMEOUT_S` (4 s), the plugin hook `timeout` (6 s) and
   `SESSION_END_BUDGET_MS` (8 s), keeping the test that pins them together. A Go `hook session-end` (in the line of Plan 00213's status line) would remove the 1 s Python startup.
4. **Bound the waits that have no bound:** the `wait` in `ccy_agent_filter_stop`
   (`lib/ssh-handling.bash`) should kill -9 after a few seconds.
5. **`ccy-teams shutdown`:** the Exit bound (60 s) is far above the observed 3 s. Show progress
   per session, or lower it. Wind-down's 300 s Idle wait should be shown as a countdown.
6. **SIGTERM path:** the supervisor could forward SIGTERM to claude, so that SessionEnd (and the
   goodbye) runs within podman's 10 s grace. `claude-supervise.py` is daemon-owned, so this
   would be an upstream hooks-daemon issue, not a local edit.

No Go SessionEnd hook exists on F44: Plan 00209 Task 3.1 is not built, and the kit's hooks are
the Python `pingbus`.
