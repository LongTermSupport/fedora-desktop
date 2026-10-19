# Goodbye on Ctrl-C, and a disconnect notice (Task 2.0b follow-up)

Branch `fix/goodbye-ctrl-c` from origin/F44. The owner asked: does mashing Ctrl-C send
the goodbye, or at least a disconnect notice? Before this branch it sent neither.

## Facts

1. **`pingbus history` shows a goodbye.** The goodbye is an agent `text` addressed to the
   team's humans and its coordinator (`cli.py` `_post_to_humans_and_coordinators`). The
   coordinator's history keeps it as a `PEER` line carrying the text: `syncer.py`
   `_history_item` calls `protocol.validate_event`, and `_agent_text_outcome` accepts a text
   whose `to` names the reader. So zero "left the team" lines means no goodbye was posted.

2. **Why none was posted: Claude Code's SessionEnd bound.** From the 2.1.303 binary:
   `getSessionEndHookTimeoutMs` (`Axe`) returns `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`
   when it is set. Otherwise it returns `max(1500, the largest timeout of a SessionEnd hook in initialHooksConfig)`. That snapshot holds the settings-file hooks only, so plugin hooks
   are not in it. `shutdown()` runs every SessionEnd hook under
   `AbortSignal.timeout(that bound)`, so the kit's `"timeout": 6` never counted. Probed here
   with a plugin SessionEnd hook that sleeps 4 s, under `claude -p`:

   - default: the hook started at +2.0 s and was cancelled ("Hook cancelled"); its process
     was killed and never finished;
   - with `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000`, set either in the environment or in
     a `--settings` file's `env`: the hook finished at +4.0 s.

   `pingbus --help` alone takes about 1 s in this container. The goodbye also needs config, a
   sync, the room check and a send on top of that, so it was always cut off.

3. **What mashing Ctrl-C does.**

   - The supervisor's Ctrl-C gate passes every second press through
     (`claude-supervise.py` `CtrlCGate`, lines 597-633). Its input is the container tty in raw
     mode; Claude Code runs on a pty of its own.
   - Claude Code's double press exits through `shutdown(0, "prompt_input_exit")`, reason
     `exit`.
   - `shutdown` first runs `cleanupTerminalModes`, which calls `detachForShutdown` and turns
     raw mode off. Presses after that become SIGINTs to Claude Code's process group. Its
     handler calls `shutdown`, which returns early once a shutdown is under way.
   - Hooks are spawned with `detached: true` on Linux (`yo = !gt`), in a session of their
     own, so terminal signals never reach the hook.

   Mashing therefore did no harm. The 1.5 s bound did.

4. **Other endings.**

   - `podman stop`, or closing the terminal or tmux window: tini (PID 1) forwards the signal to
     the supervisor, which has no SIGTERM or SIGHUP handler and dies. tini then exits, and the
     kernel kills every process in the container. Claude Code never reaches SessionEnd.
   - A crash or a SIGKILL also ends without SessionEnd.

   So the acceptance line's assumption, that SIGTERM reaches the hook, does not hold in ccy.
   Only `/exit`, logout and Ctrl-C ever run the hook.

5. **No fallback existed.** The watcher and the keeper die with the container, and nothing
   runs on the host at the seat's release. One thing could be used: a keeper's explicit
   release empties `seat.lock` (`inbox.Lock.release`), while a keeper killed with its
   container leaves `seat <claim ms>` in it.

## Fix (spec in `docs/agent-bus-protocol.md` §12 and §13, tests first)

- **(a) The goodbye survives Ctrl-C.**
  - `seat.session_settings` (the `--settings` file) and the member kit's `settings.json` set
    `env.CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000`. That covers the 4 s cap plus
    pingbus's start.
  - The hook ignores SIGINT and SIGHUP while it posts and puts the handlers back afterwards.
    SIGTERM is left alone, so Claude Code can still end the hook when the bound runs out.
  - Each goodbye that is posted is recorded in its seat as `said-goodbye.json`
    (`{"v":1,"claimed_ms":N}`).
  - The entrypoint is untouched (it is ratcheted), and so is the container stop grace, which
    does not matter because no hook runs on a stop.
- **(b) Disconnect notice.**
  - New host command `pingbus seat disconnected --root --session --since`. It reads the
    session's report (`sessions/<name>/applied.json`) and waits up to 15 s for each seat to
    be free. For each seat whose lock still names a claim of this launch, with no goodbye
    recorded for that claim, it posts `<handle> disconnected: session ended without a goodbye` to the humans and coordinators. It holds the seat lock while it does this and
    empties the claim afterwards, so each claim is told once.
  - The ccy launcher runs it as the last step of `cleanup` (`ccy_bus_disconnected` in
    `lib/agent-bus-membership.bash`). Where it runs:
    - **Why the launcher:** it is the only thing that outlives the container on every path
      the launcher survives. Bash runs the EXIT trap on SIGHUP as well. It also runs in the
      checkout, where the seat bundles are, and it knows its session's name and start time.
    - **Why not the watcher or the keeper:** both die with the container.
    - **Why not the seat release:** nothing runs on the host at the release.
  - Double messages are prevented by the record plus the claim being emptied.
  - The launcher is ratcheted, so the one added call line is offset by folding three `echo`
    lines of the compose notice into one `printf`. The output is the same.
- Versions: CCY 3.99.12.11 and container 2.77.8 (placeholders), with a changelog section.
  `meta-deploy.list` gets `play-agent-bus.yml` (the host's pingbus) and then
  `play-claude-yolo.yml`.

## Before and now, for the owner's Ctrl-C

**Before:**

1. Claude Code exited.
2. SessionEnd ran.
3. The goodbye hook was killed at 1.5 s, before it had posted.
4. No message was sent, and nothing else said the session had gone.

**Now:**

1. The same exit gives the hook 8 s, and the goodbye `left the team: session ended (exit)`
   is posted once.
2. Pressing Ctrl-C again changes nothing.
3. If the goodbye still fails, for example because the homeserver is down, the launcher's
   exit tries `disconnected` instead.

`podman stop` or a closed window posts `disconnected` once.

## Tests (seen failing, then passing)

- RED: 23 failures in `test_seat_disconnected` (new), `test_hooks.GoodbyeTest` (record and
  signals), `test_seat` and `test_plugin_contract` (the settings env); 9 in
  `scripts/test-ccy-teams.bash`.
- GREEN: the whole pingbus suite, 1560 tests. Two `test_relay_cli.WatchRelayTest` timing tests
  failed once under load average 30. The same class is also flaky on origin/F44, and it
  passed three times in a row on this branch. `scripts/test-ccy-teams.bash`: 205 passed.

## Only provable on the host (acceptance)

1. After meta-deploy, end a `--teams` session with Ctrl-C. The team gets one `left the team: session ended (exit)`. Then `pingbus --team T history` as the coordinator shows it
   as a PEER line.
2. `podman stop` the session's container: exactly one `disconnected: session ended without a goodbye`.
3. Close its tmux window: exactly one `disconnected`. This also checks that bash runs the
   EXIT trap after SIGHUP, and that the 15 s wait covers the container stopping.
4. Neither case posts both messages.

## Not covered / follow-ups

- A host reboot, or a launcher killed with SIGKILL, posts nothing.
- A hook killed between its post and its record posts both messages.
- A lifecycle restart (an `/exit` by the supervisor) now really says goodbye before the
  session comes back.
- `cc` and LXC members have no launcher fallback; their keeper releases cleanly and empties
  the claim.
