# Fact check 3: Plan 00251 diff and the PROBES-claude-code.md daemon section

Installed daemon: `.claude/hooks-daemon/`, package version 3.73.1 (pyproject.toml:7). Doc claims were checked
against the vendored copies in `.claude/hooks-daemon/remote-docs/code.claude.com/docs/en/` (hooks.md,
statusline.md). errors.md is NOT vendored.

## REFUTED or unsupported (most consequential first)

1. "mode-history gives every session's live modes (awaiting-human, focus, goal-blocked, usage pause)".
   Ran `mode-history --json --limit 3`: the output has only `sessions` (live modes, here a `manual-goals`
   entry) and `transitions` (awaiting-human enter/exit, each with `evidence`). Usage pause is a separate
   record (`utils/usage_pause.py`, read by the `usage-pause` verb, cli.py `cmd_usage_pause`), and no
   usage-pause mode label turned up in `core/session_mode_registry.py`. A seat cannot get the pause from
   mode-history. Change: drop "usage pause" from that list, or name `usage-pause` as the interim read.
2. "Until that release is installed, a seat reads `mode-history --json --session`". That fallback carries
   modes and transitions only. It does not carry the StopFailure error or the usage windows, so the interim
   read cannot show "rate-limited" or a reset time. Change: say so in the PROBES text, so a reader does not
   think the fallback covers the rate-limited state.
3. "A main-thread `rate_limit` stop is recorded". Fields confirmed (`utils/stop_failure_records.py`:
   session_id, error, recorded_at, resolved_at), and only `rate_limit`, `authentication_failed` and
   `cloud_credential_error` are recorded (line 48). But "main-thread" is unsupported: the recorder
   (`handlers/stop_failure/stop_failure_recorder.py`) filters on nothing but session id. The records sit in
   the daemon's `stop-failures.json` and no CLI verb in 3.73.1 exposes them. Change: drop "main-thread",
   and state that in the install the record is file-only.

## session-state status

`hooks-daemon session-state` is ABSENT from the install: argparse answers "invalid choice", and the verb
list has no such verb. There is no `schemas/` directory under `src/claude_code_hooks_daemon/`, no
SESSION_STATE_CONTRACT file, and no Plan 00601 in the install's CLAUDE/Plan. This matches the doc's own
caveat (on main, reaching an install with the next release). Everything the doc says about Plan 00601
(flags, `--watch`, the three schema names, `$id`, schema_version 1.0.0, pin major 1, `resets_at`
optional, per-call cost) is UNVERIFIABLE-HERE; the daemon's main branch or its next release settles it.

## Verification table

| #   | Claim                                                                                                  | Verdict                              | Evidence                                                                                                |
| --- | ------------------------------------------------------------------------------------------------------ | ------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| 1   | `mode-history --json [--session ID]` exists                                                            | VERIFIED                             | `mode-history --help`: `--session`, `--limit`, `--json`                                                 |
| 2   | `session-mode get` gives one session's modes                                                           | VERIFIED                             | `session-mode --help`: get/focus/unblock, `--session`, `--json`                                         |
| 3   | mode-history includes recent mode changes                                                              | VERIFIED                             | JSON `transitions`: at, session_id, label, action, evidence                                             |
| 4   | modes include awaiting-human, focus, goal-blocked                                                      | VERIFIED (partly)                    | awaiting-human in transitions; focus and goal-loop block via session-mode verbs; usage pause see item 1 |
| 5   | usage pause is one of mode-history's modes                                                             | REFUTED                              | see item 1                                                                                              |
| 6   | records carry `session_id`                                                                             | VERIFIED                             | transitions and StopFailureRecord both carry it                                                         |
| 7   | StopFailure recorded with session, error, when happened, when cleared                                  | VERIFIED                             | stop_failure_records.py fields; no reset time field                                                     |
| 8   | the recorded stop is "main-thread"                                                                     | REFUTED (unsupported)                | see item 3                                                                                              |
| 9   | reset times held apart from the stop, from status line `rate_limits`                                   | VERIFIED                             | `core/usage_snapshot.py` (`usage_state_file`), `core/data_layer.py` UsageTracker                        |
| 10  | files under `untracked/` are internal, CLI is the interface                                            | UNVERIFIABLE-HERE                    | policy statement; no contract text in the install                                                       |
| 11  | session-state verb in the install                                                                      | REFUTED (absent), as the doc caveats | see section above                                                                                       |
| 12  | Plan 00601 details                                                                                     | UNVERIFIABLE-HERE                    | not in the install                                                                                      |
| 13  | fallback read `mode-history --json --session`                                                          | REFUTED as sufficient                | see item 2                                                                                              |
| 14  | PLAN: blocked is `PermissionRequest` and `Elicitation`, immediate                                      | VERIFIED                             | hooks.md:65 and :90                                                                                     |
| 15  | PLAN: reset time only in status line `rate_limits.*.resets_at`                                         | VERIFIED                             | statusline.md:208-209; StopFailure payload (hooks.md:2928) has no reset time                            |
| 16  | `SessionStart` source: startup, resume, clear, compact, fork                                           | VERIFIED                             | hooks.md:321                                                                                            |
| 17  | `SessionEnd` has a short default budget                                                                | VERIFIED                             | hooks.md:3666 (1.5 second default; raisable)                                                            |
| 18  | `idle_prompt` follows a reply after a minute of no typing                                              | VERIFIED                             | hooks.md:2544, 2569                                                                                     |
| 19  | permission_prompt and elicitation Notifications are late (about 6 s)                                   | UNVERIFIABLE-HERE                    | hooks.md:2566 says they share desktop-notification timing; the figure 6 was not found in the lines read |
| 20  | `quota_auto_resume_fired`, `_stale`, `_disabled` types                                                 | VERIFIED                             | hooks.md:2553-2555                                                                                      |
| 21  | `idle_prompt` is not sent during the quota wait                                                        | UNVERIFIABLE-HERE                    | not confirmed in lines read                                                                             |
| 22  | StopFailure `error` enum                                                                               | VERIFIED                             | hooks.md:333 and :2928                                                                                  |
| 23  | banner text "You've hit your session limit"                                                            | UNVERIFIABLE-HERE                    | errors.md not vendored; `hooks-daemon remote-docs add <url>` would settle it                            |
| 24  | `rate_limits` present only for claude.ai subscribers, after first response, window dropped after reset | VERIFIED (partly)                    | statusline.md:164 for the drop; the other two not individually read                                     |
| 25  | `context_window` fields                                                                                | UNVERIFIABLE-HERE                    | not read                                                                                                |
| 26  | PLAN diff: still-owed live probes (limit-ending hook, banner text, Tuwunel presence)                   | VERIFIED                             | no vendored page settles them                                                                           |

Counts: 26 claims: 13 verified, 4 refuted or unsupported (5, 8, 11 and 13), 9 unverifiable-here.
