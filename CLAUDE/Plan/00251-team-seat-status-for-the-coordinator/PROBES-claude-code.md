# Plan 00251: what Claude Code exposes for a seat's state (Task 1.4, documentation half)

From Claude Code's documentation (https://code.claude.com/docs/en/hooks.md,
https://code.claude.com/docs/en/statusline.md, https://code.claude.com/docs/en/errors.md).
Each fact is what the docs say; "not stated" marks a gap the live probes must close.

## Working and idle

- A turn starts at `UserPromptSubmit`, tools are bracketed by `PreToolUse` and `PostToolUse`
  (and `PostToolUseFailure`), and the turn ends at `Stop`. No hook or field says "idle, waiting
  for input" as a state: idle is the time after `Stop` with no new `UserPromptSubmit`.
- `SessionStart` carries `source`: `startup`, `resume`, `clear`, `compact`, `fork`.
  `SessionEnd` carries a reason (`clear`, `resume`, `logout`, `prompt_input_exit`, `other`) and
  has a budget of about 1.5 s, so "gone" cannot rest on it alone.

## Blocked on a dialog

- `PermissionRequest` fires at once when a tool call needs a permission decision, and
  `Elicitation` at once when an MCP server asks for input. These are the blocked signals.
- The `Notification` hook's `permission_prompt`, `elicitation_dialog` and
  `elicitation_url_dialog` fire only after about 6 s without a keystroke, and `idle_prompt` about
  60 s after a reply with nothing typed and no background agent. They are late confirmations,
  not the signal.

## Rate-limited

- The session shows `You've hit your session limit · resets 3:45pm` (weekly and per-model
  limits read the same way) and makes no request until then.
- A subscription session may wait and continue by itself after the reset. The `Notification`
  types `quota_auto_resume_fired`, `quota_auto_resume_stale` (the reset passed while the machine
  slept, so it waits for Enter) and `quota_auto_resume_disabled` report how the wait ended;
  `idle_prompt` is not sent during it.
- `StopFailure` runs instead of `Stop` when a turn ends on an API error. Its `error` is one of
  `rate_limit`, `overloaded`, `authentication_failed`, `oauth_org_not_allowed`,
  `account_on_hold`, `billing_error`, `invalid_request`, `model_not_found`, `server_error`,
  `max_output_tokens`, `cloud_credential_error`, `unknown`, with optional `error_details` and
  `last_assistant_message`. It carries no reset time. Not stated: whether a subscription usage
  limit ends the turn through `StopFailure` with `rate_limit`, or through `Stop`.
- The only machine-readable reset time is the status line's input:
  `rate_limits.five_hour` and `rate_limits.seven_day`, each `used_percentage` (0-100) and
  `resets_at` (Unix seconds). It is present only for claude.ai subscriptions and only after the
  first API response, and a window is dropped once its reset has passed.

## Context

- The status line's `context_window` has `used_percentage`, `remaining_percentage` (either may be
  null early), `context_window_size`, `current_usage` and the token totals.

## What the hooks daemon already knows (from its seat)

- Its files under `.claude/hooks-daemon/untracked/` are internal layout and may change; the
  supported interface is its CLI. `hooks-daemon mode-history --json [--session ID]` gives every
  session's live modes (awaiting-human, focus, goal-blocked, usage pause) and recent mode
  changes; `hooks-daemon session-mode get` gives one session's modes.
- Records carry `session_id`, so a host where several sessions share a checkout filters on it.
- A main-thread `rate_limit` stop is recorded, as a `StopFailure` (session, error, when it
  happened and when it cleared), with no reset time. The reset times are held apart, from the
  status line's `rate_limits`, and must be joined to the stop; whether that join is reliable is
  the first task of the daemon's own plan.
- A single `session-state --json` read (modes, the last turn edge with its StopFailure error,
  the usage windows, the usage pause), ideally streamed on change, does not exist yet; it is the
  daemon's Plan 00601 (session state contract and schemas), with a versioned JSON Schema the
  daemon validates its own output against. Until it lands, a seat reads `mode-history --json --session` and
  never the files.

## What the live probes must still settle

- Which hook ends a turn that hits a subscription usage limit, and its payload.
- The usage banner's exact text at several terminal widths (fixtures for the pane reading).
- Whether Tuwunel keeps a presence `status_msg` across syncs that set no presence, and across a
  server restart.
