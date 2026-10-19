# Plan 00194 PLAN.md diff: fact check

Scope: claims added or changed in the diff (Task 2.0e added; the host acceptance bullet rewritten).

## Task 2.0e

1. CONFIRMED: "A lifecycle restart posts `left the team: session ended (exit)` and the session re-joins." `helpers/pingbus/hooks.py:85,128,1025` shows `prompt_input_exit` maps to shown reason `exit`, and the goodbye text is `{handle} left the team: session ended ({reason})`. `/exit` typed by the supervisor gives that same SessionEnd reason (upstream #127). `docs/agent-bus.md:502-504` states it.
2. CONFIRMED: both lifecycle triggers return `ExitForRestart`. `files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py` lines 346-348 (relaunch after a first team join) and 388 (maximum age).
3. CONFIRMED: the supervisor writes `restart-request.json` only after the child exits. `.claude/ccy/claude-supervise.py:8545-8548`; `finish()` (line 8729) writes it, while `request()` (8656-8706) keeps state only in memory (`self._pending`) after typing `/exit`.
4. CONFIRMED: the supervisor may refuse when there is no single session id (`len(session_ids) != 1`, line 8684), and abandons after 30 s (`_RESTART_EXIT_WAIT_SECONDS = 30.0`, line 8559; `abandon()`, line 8708). Refusals also include dry-run, cooldown and the abandon cap. A plugin-written marker would therefore be unreliable, as the plan says.
5. CONFIRMED: filed as hooks-daemon #127. `gh api .../issues/127` returns the title "ccy supervisor: an exit for restart is indistinguishable from a real exit at SessionEnd (the request is recorded only after the child exits)", state open, created 2026-10-19. The proposed fix matches the plan's "record before typing /exit, clear on abandon". Zero comments, so nothing there contradicts the plan. (`gh issue view 127 --comments` printed nothing in this container; `gh api` was used instead.)
6. CONFIRMED: "Fable's review of `fix/goodbye-ctrl-c`, 3b". `subagent-reports/261019-goodbye-ctrl-c-review-fable.md` has section "3b. The lifecycle restart" with the same conclusion (restarts should post nothing; supervisor needs a marker before `/exit`).
7. CONFIRMED: `seat.record_goodbye` exists (`helpers/pingbus/seat.py:262`, used in `hooks.py:1100`), and the launcher's `seat disconnected` (`helpers/pingbus/cli.py:2599`, `docs/agent-bus-protocol.md:825`) reads the recorded goodbye.
8. CONFIRMED: "`docs/agent-bus.md` states today's behaviour until then" (`docs/agent-bus.md:502-504`).
9. CONFIRMED: `fix/goodbye-ctrl-c` is an ancestor of the current branch (merged into F44).

## Host acceptance bullet (rewritten)

10. CONFIRMED: before the fix no goodbye was sent because of Claude Code's 1.5 s SessionEnd bound, which a plugin hook's `timeout` does not raise. Journal `JOURNAL/00194-Journal-26-10-19.md` 09:16 finding (`getSessionEndHookTimeoutMs`, probe cancelled by default and finished with the variable at 8000, plugin hooks not in the snapshot). `docs/agent-bus-protocol.md:715` agrees.
11. CONFIRMED: a stop or a closed terminal ends the container at once, so no hook runs. Same journal entry (tini forwards the signal, the supervisor has no SIGTERM/SIGHUP handler); `docs/agent-bus.md:505-507`.
12. CONFIRMED: step 1, mashed Ctrl-C posts one `left the team` and the launcher prints no traceback or `pingbus seat disconnected failed`. The launcher message string exists (`lib/agent-bus-membership.bash:92`); `docs/agent-bus.md:499-500` says pressing more changes nothing; the journal 10:20 entry covers the SIGINT handling. (The behaviour is what the host check tests, so it is intentionally unverified here.)
13. CONFIRMED: step 2, the text is `disconnected: session ended without a goodbye`, posted by the launcher (`cli.py:2599`, `docs/agent-bus.md:508`).
14. CONFIRMED: step 3, two seats get one `disconnected` each (`docs/agent-bus.md:508-509`, "for each seat it held").
15. CONFIRMED: step 4, never both messages for one session (`docs/agent-bus.md:510`).
16. CONFIRMED: step 5, `CCY_LIFECYCLE_MAX_AGE_SECONDS` has a minimum (1800 s, `lib/session-lifecycle.bash:33`, `ccy_lifecycle.py:38`), and the Fable review (line 205) asked for this baseline.
17. CONFIRMED: the two plays exist (`playbooks/imports/play-agent-bus.yml`, `play-claude-yolo.yml`), branch `fix/goodbye-ctrl-c` exists, and the journal day-file 26-10-19 exists. The diff does not claim they are queued; `CLAUDE/Plan/meta-deploy.list` currently has no active entries.
18. NOTE (not a refutation): the removed bullet said "set `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` in the entrypoint if needed". It is now set in the session settings (`files/opt/claude-yolo/optional/agent-bus/settings.json:9`), consistent with the rewrite.

## Style nit

The `podman stop` step line (step 2) and the issue sentence in Task 2.0e run past the surrounding wrap width ("filed as #127 (waits on its release); then the hook skips the goodbye for that session and records"). This is cosmetic only.

REFUTED: none.
