# Session Modes

> **This file is DAEMON-OWNED.** It is deployed to `CLAUDE/core/SessionModes.core.md`
> and overwritten wholesale on every install and upgrade, so a local edit is
> discarded the next time the daemon deploys — never hand-edit it. Your own
> `CLAUDE/SessionModes.md` is seeded once, is never touched again, and opens with
> a markdown link to this file. Put anything this project does differently there,
> not here.
>
> Wrapper paths below are written as `hooks-daemon`, short for
> `.claude/hooks-daemon/bin/hooks-daemon` in a standard install. A self-install
> checkout, where the daemon root IS the project root, uses `bin/hooks-daemon`.

A **session mode** is a named, per-session condition that switches the daemon's
work-driving machinery off for ONE session: the crons, goal injection and
supervisor prompts that keep an unattended worker busy. A plain worker session
has no mode and shows nothing. Modes compose: their suppressions union, and a
mode that halts work beats one that only narrows it.

**Guards are never part of any mode.** Every blocking and safety handler behaves
identically in every mode; a mode only withholds things that DRIVE work.

## The modes

| Label (status line) | Mode, reason       | Entered by                                                            | Ends on                                                                                            |
| ------------------- | ------------------ | --------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `usage-paused`      | usage pause        | the usage-pause gate, when the session reaches its usage ceiling      | the usage window resetting (or a human typing `! hooks-daemon usage-pause clear`)                  |
| `goal-blocked`      | BLOCKED, goal-loop | `goal_loop_detector` in `act` mode                                    | a genuine human prompt, a repository change, the `blocked_hours` timer, `session-mode unblock`     |
| `awaiting-human`    | BLOCKED, declared  | a stop declaring `STOPPING BECAUSE: [awaiting-human] ...`             | a genuine human prompt in the SAME session, or its fixed 24-hour cap                               |
| `manual-goals`      | manual goals       | a human `/goal clear`, `hooks-daemon goals off`, `/mode goals manual` | `hooks-daemon goals on` (or `/mode goals auto`), or a new session                                  |
| `focus`             | focus              | a human prompt starting `hooks-daemon focus <task>`                   | `hooks-daemon focus off`, a new session, or its 12-hour TTL (renewed by each genuine human prompt) |
| `unattended`        | daemon unattended  | the daemon-wide unattended mode (`/mode`), not a session mode         | that daemon mode being switched off                                                                |

**Precedence** (which label the status line shows first):
`usage-paused > goal-blocked > awaiting-human > manual-goals > focus > unattended`.

### BLOCKED: one family, two reasons

`awaiting-human` and `goal-blocked` are the same mode, `BLOCKED`, with reason
`declared` or `goal-loop`. They share one suppression set and show different
labels.

- **Declared (`awaiting-human`).** Only a GENUINE human prompt in the SAME
  session ends it. A cron tick (any prompt led by a `[tick:...]` line), a line
  the ccy supervisor typed, a notification, or a prompt in ANOTHER session
  leaves it in place. Otherwise it lasts a fixed 24 hours, so a mistaken
  declaration costs at most a day of ticks. `session-mode unblock` refuses it:
  the session asked for a human, so only a human ends it.
- **Goal-loop (`goal-blocked`).** Entered by the detector (below) and ended by
  a genuine human prompt, a repository change a blocked input could have
  produced, the `blocked_hours` timer, or `hooks-daemon session-mode unblock`.

### Focus

A human prompt that STARTS with `hooks-daemon focus <task>` (any case; the
speech-to-text spellings `hooks daemon` and `hooks demon` also work) puts that
session, and only that session, into focus. `hooks-daemon focus off` (or
`done`, `end`, `disable`) ends it. The prompt still reaches the model with the
task attached. A supervisor line, a cron tick or a notification never toggles
focus. Entering focus clears an existing goal ONCE: the ccy supervisor types
`/goal clear` at its next idle; without the supervisor, a human must type it.
Focus is a working mode, not a halt.

### Manual goals

`manual-goals` hands this session's goal to the human. It is entered when a
human clears a goal (a `/goal clear` the Stop path attributes to the human, not
to the supervisor), by `hooks-daemon goals off` (or `manual`) typed as the WHOLE
prompt or run from the CLI, or by `/mode goals manual`. It ends only at an
explicit switch-back, `hooks-daemon goals on` (or `auto`) or `/mode goals auto`,
or with the session: its stored expiry is a liveness bound that every prompt
renews, so a live session never times out of it.

While it holds, the daemon makes no AUTOMATED goal change for the session: no
`.goal-intent` is written, so the ccy supervisor types no `/goal`, and a plan
retiring does not clear the goal. A `/goal <condition>` the human types still
works and does not end the mode. Not suppressed: the goal-loop detector's clear,
the clear on entering focus, `hooks-daemon clear-goal`, `continue`, cron ticks
and every guard. A goals command that cannot be recorded is stated as
`MANUAL GOALS NOT CHANGED` with the error.

## What each mode suppresses

| Suppressed                                                              | BLOCKED (either reason) | focus                                          |
| ----------------------------------------------------------------------- | ----------------------- | ---------------------------------------------- |
| Declared `persistent_crons` ticks (`R-DECLARED-CRON-SUPPRESSED`)        | dropped (\*)            | dropped, even with `runs_while_awaiting_human` |
| Failsafe-cron tick                                                      | dropped                 | DELIVERED, with a leading line naming the task |
| Watchdog tick                                                           | delivered               | delivered                                      |
| Goal injection (`.goal-intent`) and every `drives_autonomy` handler     | withheld                | withheld                                       |
| Goal-ledger stop challenge                                              | suppressed              | replaced by a one-line focus reminder          |
| ccy supervisor `/goal` and session-actions directive                    | not typed               | not typed (see below)                          |
| ccy supervisor post-compact `continue`, standing-authorisation reminder | not typed               | typed as normal                                |
| Unattended stop-block interceptor                                       | stands down             | unchanged                                      |

(\*) A declared job that sets `runs_while_awaiting_human: true` still runs while
BLOCKED. `usage-paused` has its own, stricter contract (only the usage-resume
cron may exist); see `R-USAGE-PAUSE-*` via `hooks-daemon explain-rule`.

The focus failsafe line reads: `focus mode: the active task is <task>; other plans are out of scope`.

### What the ccy supervisor does

- **Under focus** the supervisor types no goal and no session-actions
  directive, because the daemon writes neither signal: the goal is cleared once
  on entry and no `.goal-intent` or `.session-actions` file is written while
  focused. The supervisor itself reads no focus state. The post-compact
  `continue` and the standing-authorisation reminder are unaffected, because
  focus is a working mode, not a halt.
- **While BLOCKED** the supervisor reads the daemon's per-session
  `<session>.session-modes` file and skips the post-compact `continue`, any
  `/goal`, the standing-authorisation reminder and the session-actions
  directive, each as a named `noop:` line in its `decision.log` (for example
  `noop: post-compact continue skipped: session BLOCKED (awaiting-human)`).
  `/compact` itself still runs. A pending goal CLEAR still goes through.
- **An unreadable session-modes file closes the same gate** rather than being
  read as "no modes": the prompts are withheld, `decision.log` names the file
  and error, and the status line shows the WARNING
  `supervisor: session-modes file unreadable -- goal, continue and reminder prompts withheld; see decision.log`.
  The fix is NOT a restart: the daemon renames an unreadable file `*.corrupt`
  the next time it loads its registry, after which the gate reopens. Let the
  daemon load the registry, then inspect the `.corrupt` file for the cause.

## The goal-loop detector

`goal_loop_detector` (Stop, main thread) catches a session that keeps stopping
with `STOPPING BECAUSE:` while the goal evaluator keeps answering "not met",
with no human prompt in between. It counts cycles inside a window and resets on
a human prompt, a met or cleared goal, or a repository change (HEAD plus
tracked changes; plan journals and `untracked/` excluded). A stop declaring
`[awaiting-human]` counts toward a lower threshold. Stop-text similarity is
logged as evidence only, never a firing condition.

- **`observe` (default)** only logs `near-miss`, `would-enter` and
  `loop-reset` records to the transition log. It never blocks and never enters
  a mode.
- **`act` (opt-in, `mode: act`)** at the threshold enters `goal-blocked` and
  clears the goal, so the supervisor types `/goal clear` and stops driving the
  loop. The session is told once at its next prompt.
- **Without ccy** nothing types the clear. The only backstop is Claude Code's
  own cap of 8 consecutive stop-hook continuations (reset by each tool call,
  raised by `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP`), after which it pauses the goal
  until the next message. A loop that makes tool calls between stops never
  reaches that cap, which is why the detector exists.

Options and defaults: `hooks-daemon explain-handler goal_loop_detector`.

## Being told

- **On change.** Entering, leaving and expiry are each stated to the agent
  once, in one line. A focus change or renewal that cannot be
  recorded is stated as `FOCUS MODE NOT CHANGED` or `FOCUS MODE NOT RENEWED`
  with the error.
- **After a daemon restart** the next prompt in each session gets one
  `SESSION MODE:` line naming the modes that survived the restart and any it
  cleared, and whether the daemon is in unattended mode. A usage-paused session
  hears this on the resume tick that finds usage still over the ceiling.
- **On the status line.** The `session_mode_indicator` segment shows
  `⚑ <top mode> +N` (with the task when focus leads), and nothing when no mode
  is active. A red `⚑ modes unreadable` or `⚑ usage-pause record unreadable`
  means the daemon could not read the mode state; see the daemon log.
  `hooks-daemon status-line-explained` describes every segment.

## Inspecting and changing modes

| Command                                                         | Does                                                             |
| --------------------------------------------------------------- | ---------------------------------------------------------------- |
| `hooks-daemon session-mode` / `get [--json]`                    | this session's modes                                             |
| `hooks-daemon session-mode focus <task>`                        | enter focus (same as `/mode focus <task>`)                       |
| `hooks-daemon session-mode focus off`                           | end focus (same as `/mode focus off`)                            |
| `hooks-daemon session-mode unblock`                             | end a goal-loop block; REFUSES a declared `awaiting-human` block |
| `hooks-daemon goals off` / `goals on` (`manual` / `auto`)       | enter / leave manual-goals (same as `/mode goals manual\|auto`)  |
| `hooks-daemon mode-history [--session ID] [--limit N] [--json]` | every session's live modes and the recent transitions            |

`session-mode` acts on `$CLAUDE_CODE_SESSION_ID` (set inside a Bash tool call)
unless `--session <id>` is given; so does `goals`. The IPC actions are
`get_session_modes`, `enter_focus`, `exit_focus`, `enter_manual_goals` and
`exit_manual_goals`.

**The transition log.** Every mode entered, exited, expired or cleared by a
restart, with what triggered it, plus the detector's observe records, is
appended to `session-mode-transitions.jsonl` under the daemon's untracked
directory (newest 500 kept). `mode-history` reads it; an unreadable log is an
error, never an empty history. A log that does not parse is moved aside as
`session-mode-transitions.jsonl.<time>.<token>.corrupt` and a fresh one
started.
