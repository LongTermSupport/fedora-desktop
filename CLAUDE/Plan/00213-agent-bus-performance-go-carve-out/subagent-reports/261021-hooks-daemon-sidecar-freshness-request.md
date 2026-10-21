# Draft hooks-daemon request: context-sidecar freshness tied to the status-line refresh

Plan 00213 Task 1.3. This is a draft only; the coordinator routes it through
`hooks-daemon issue-report`. It is not filed from here.

## Problem

`claude-supervise.py` treats a context sidecar as stale once `now - ts` passes
`CompactPolicy.freshness_seconds`, which defaults to `_DEFAULT_FRESHNESS_SECONDS = 30.0`. A
stale reading is a NOOP ("sidecar stale"), so the supervisor does not compact. The value comes
from the dataclass default. No environment variable, config key or flag sets it.

In an idle session, Claude Code renders the status line only on its `refreshInterval` timer.
Its docs say the event-driven triggers "can go quiet when the main session is idle". Only a
render writes the sidecar. So any project whose status line refreshes more slowly than every
30 s loses automatic compaction for part of each cycle. The `_DEFAULT_FOREGROUND_MARGIN_SECONDS = 10.0` ambiguity margin is also sized at "roughly one status refresh interval".

This ties the status-line pace to the supervisor. A client cannot slow its status line (each
refresh starts processes and costs CPU) without also delaying compaction.

## Request

One of these:

1. Make `freshness_seconds` (and the foreground margin) settable from
   `.claude/hooks-daemon.yaml`, under `ccy:` or the supervisor's own section.
2. Derive both from the session's effective `statusLine.refreshInterval`, for example freshness
   = max(30, 2 × interval + slack), with the margin scaled to match.

## Client impact today

The client has set its seated bus sessions' `refreshInterval` to 25 s, not the 60 s it wanted,
so that the sidecar stays inside the 30 s window.
