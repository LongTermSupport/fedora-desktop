# Plan 00251 research: seat status for the coordinator

Research and brainstorm for Tasks 1.1 and 1.2. Read-only; nothing here is built. Citations are
repository paths at F44 `9d5ac90e0`. Claims about Claude Code behaviour that this research did
not verify on the pinned version are marked **(verify)**: each needs a probe before a spec relies
on it.

## 0. What v2 already has (the base to build on, not beside)

| Already there                                                                                                                                                                                                            | Where                                                                                 | What it gives seat status                                                                               |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| The link learns busy/idle from hooks: UserPromptSubmit marks the session busy, Stop and SessionStart mark it idle (`engine.SessionFacts.Idle`)                                                                           | `src-go/internal/teamchat/member/link/hooks.go` (`tellSession`), B 2.8                | The core working/idle signal, already deterministic and already in the right process                    |
| The kit registers only SessionStart, UserPromptSubmit, Stop, SessionEnd                                                                                                                                                  | `src-go/internal/teamchat/member/kit/kit.go`                                          | No Notification, PreCompact or tool hooks yet: blocked-on-dialog is invisible today                     |
| Every member syncs **presence of every team member** (filter section `presence`, senders = roster); the link's long-poll is what keeps the seat online; a dead link goes `unavailable` then `offline` within about 120 s | A 6.6, A 6.10, SPEC-matrix M50                                                        | A cross-network, history-free carrier that the coordinator's link already receives                      |
| `status_msg` is deliberately "never set, never read"; presence is labelled "display, not liveness"                                                                                                                       | A 6.6                                                                                 | Using it for seat state is a design change to A 6.6 (owner decision, section 7)                         |
| Agents cannot write any state event: `state_default` 100, only `m.room.message` open; v1's `listening` state event was removed on purpose (history growth, non-user-ID keys)                                             | A 4.2, A 6.6 "Why presence, and the alternative"                                      | A room-state carrier reopens a settled decision; it needs a power-level change                          |
| Lifecycle notices `left` / `disconnected`, posted by the link at the keeper's end, or by the owner's seat sweep / LXC health tick within a minute when nothing in the member runs on                                     | B 2.11, `teamchat/notice`, `teamchat/sweep`                                           | The `gone` state and its reason already exist on the wire                                               |
| Pings with answers, deadlines and exactly-one TIMEOUT per unanswered addressee; `outbox` records of open and answered pings                                                                                              | A 8.1-8.3, B 2.2.1 `status`                                                           | The **request graph**: who owes whom an answer. This is what turns "idle" into "idle-locked"            |
| The link's 1-minute heartbeat of `status.json`, the status line every 25 s (which receives Claude Code's status-line JSON on stdin)                                                                                      | B 2.9                                                                                 | A periodic local tick that already exists; a source of context and rate-limit facts **(verify fields)** |
| Host status record and ccy-teams board (`STATE` from v1 `agentbus.Liveness`: listening / stale / parked)                                                                                                                 | C 3.11.3, `src-go/internal/teams/board/table.go`, `src-go/internal/agentbus/parse.go` | Host-local only; `online` only for LXC; cannot see a remote seat except through the homeserver          |
| Pane classifier: ready / busy / typed / not-drawn, launcher prompts, conversation picker                                                                                                                                 | `src-go/internal/screen`, Plan 00135 Phase 10                                         | Host-side evidence for local ccy panes; no dialog or usage-limit recognition yet                        |
| Plan 00203's "idle with an empty box for 2 minutes" (shutdown-early, wind-down idle moments)                                                                                                                             | `CLAUDE/Plan/00203-teams-hot-control-and-robustness/PLAN.md` decision 16, Task 3.14   | The same pane idle test, to be reused (not re-implemented) for the pane input                           |

The key finding: **the seat's own link is the one process that already holds nearly every
signal** (hook-driven busy/idle, transcript path, pending deliveries, outbox, presence of the
others), and the coordinator's link already receives every seat's presence. Seat status is mostly
a reducer plus a publisher inside the link, not a new service.

## 1. Idle-lock failure modes

| #   | Mode                                                                                                                                          | What the coordinator sees today                                              | Deterministic evidence available                                                                                                                                          |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1  | **Finished but not reported.** Worker `ack`ed a ping, did the work, stopped without `done` or `blocked`.                                      | Nothing. The `ack` satisfied the deadline, so no TIMEOUT ever fires          | Worker idle (Stop, no prompt since) + an open request from the coordinator that has an `ack` but no `done`/`blocked`/`nack`                                               |
| F2  | **Blocked on a dialog**: permission prompt, AskUserQuestion, plan-mode approval, an MCP elicitation, a trust prompt                           | Nothing; the seat is "online"                                                | Notification hook with its type **(verify `notification_type`: `permission_prompt`, `idle_prompt`, `elicitation_dialog`)**; pane text                                     |
| F3  | **Blocked on a human**: the agent stopped with `STOPPING BECAUSE: [awaiting-human]`, or asked in questions and waits                          | Nothing                                                                      | Stop + the hooks daemon's session mode `awaiting-human` (where the daemon runs); an open question in the questions room (coordinator)                                     |
| F4  | **Mutual wait**: worker waits on the coordinator (sent a ping, or `blocked` naming the coordinator) while the coordinator waits on the worker | Each believes the other is busy                                              | Both idle + a cycle in the request graph (edges = open pings / `blocked` answers)                                                                                         |
| F5  | **Crashed or gone**: Claude Code exited, container killed, host lost power, remote machine partitioned                                        | A `disconnected` notice (crash, kill) within ~1 min; nothing for a partition | Presence offline/unavailable; `left`/`disconnected` notice; no notice + presence unavailable = **unreachable** (partition)                                                |
| F6  | **Rate-limited** (usage limit hit): the session cannot run a turn until the window resets                                                     | Indistinguishable from offline or idle                                       | Status-line `rate_limits` **(verify)**, a turn ending on a rate-limit API error (StopFailure hook **(verify)**, transcript error entry), pane text, ccy token-usage cache |
| F7  | **Usage-paused** by the hooks daemon (a self-imposed ceiling below the hard limit; only cron tools allowed, resumes at window reset)          | Looks idle                                                                   | The daemon's usage-pause state for the session (R-USAGE-PAUSE-\*); its resume cron's time                                                                                 |
| F8  | **Context exhausted**: autocompact failing, "prompt is too long", or a session that compacts in a loop                                        | Looks working or idle                                                        | Status-line `context_window` used share **(verify)**, PreCompact / `compact_boundary` frequency in the transcript, API error in transcript                                |
| F9  | **Hung in a turn**: a tool that never returns, a stuck subprocess, a wedged API stream                                                        | Looks working forever                                                        | Busy (no Stop) + no transcript write for N minutes; the last transcript entry a `tool_use` with no result says which tool                                                 |
| F10 | **Typed but not sent**: a human or the supervisor left text in the input box; live delivery waits on it (Plan 00194's one wait)               | Looks idle                                                                   | Pane `typed` state (`screen.Typed`)                                                                                                                                       |
| F11 | **Delivery lost**: a delivery the session never received (B 2.7.4 `missing`)                                                                  | Coordinator thinks the worker has the request                                | The worker link's `awaiting`/`missing` records                                                                                                                            |
| F12 | **At a launcher prompt or conversation picker** after a restart (Plan 00135 Phase 10)                                                         | Seat held, nothing happening                                                 | Pane: `screen.AtConversationPicker`, `KnownPrompts`; no SessionStart since the keeper started                                                                             |

F1 and F4 are the true idle-locks the owner describes; both are decided purely by the bus's own
records plus the seat's busy/idle bit. F2, F3, F6-F9 are "blocked" variants; F5 is "gone".

## 2. Signals per seat and their reliability

| Signal                                                                                        | Where it is read                                 | Cost                                       | Reliability                                                                                                                                                | Cross-network                    |
| --------------------------------------------------------------------------------------------- | ------------------------------------------------ | ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| UserPromptSubmit / Stop / SessionStart (busy / idle)                                          | seat link (already)                              | none new                                   | High. Gaps: a Stop blocked by a Stop hook (`stop_hook_active`) is not a real end; a hook missed while the link restarts is learned at the next one (B 2.8) | via the publisher                |
| Notification hook (dialog, idle prompt)                                                       | seat link (new kit hook)                         | one short process per notification         | High for "a dialog is open" **(verify types on the pinned version)**; the dialog's end is the next UserPromptSubmit/PostToolUse/Stop                       | via the publisher                |
| StopFailure hook (turn ended on an API error)                                                 | seat link (new kit hook)                         | rare                                       | **(verify existence and payload: error type rate_limit / auth / billing)**                                                                                 | via the publisher                |
| Status-line stdin JSON (`rate_limits.*.resets_at`, `context_window`)                          | `teamchat status-line` (already runs every 25 s) | none new; forward a few fields to the link | **(verify fields on the pinned version)**; only while Claude Code draws; account-wide facts                                                                | via the publisher                |
| Transcript (inotify on the path the link already knows)                                       | seat link                                        | one watch                                  | High for "progress happened" (mtime) and for the last entry's kind (tool_use without result = in a tool; API error entry = failed turn)                    | via the publisher                |
| Seat link liveness (presence via long-poll)                                                   | homeserver, every member's sync                  | none new                                   | High for "the link is alive"; says nothing about Claude Code inside                                                                                        | yes, native                      |
| Keeper / link / seat locks (`keeper.lock`, `seat.lock`)                                       | the seat's own machine                           | none                                       | Truth for liveness (R27) but local only                                                                                                                    | no                               |
| Lifecycle notices `left` / `disconnected`                                                     | main room                                        | none new                                   | High; `disconnected` for a remote seat needs that machine's own sweep (Plan 00246)                                                                         | yes                              |
| Pane snapshot (`screen` package, Plan 00203's idle test)                                      | host tooling with tmux access                    | a capture per tick                         | Medium: text-shape dependent, breaks across Claude Code versions; best for picker, launcher prompts, typed text, usage-limit banner                        | no (only the pane's own machine) |
| Hooks daemon session modes (`awaiting-human`, usage-pause)                                    | the daemon's state in the project                | file read                                  | High where the daemon runs; absent in projects without it, so enrichment only                                                                              | via the publisher                |
| ccy token-usage cache (`ccy --token-usage --json`, `src-go/internal/ccylaunch/tokenusage.go`) | host                                             | one HTTP read per account, cached          | High for the account's window and reset time; per token, so it predicts every seat on that token                                                           | no (host-side)                   |
| v1 board liveness (`agentbus.Liveness`)                                                       | host, v1                                         | -                                          | v1 only; out of scope (non-goal) but the board keeps it for v1 rows                                                                                        | no                               |

Rule from this: **the seat computes its own state** (it alone sees hooks, transcript and the
status line on its machine), and only the result crosses the network. Pane evidence is a
host-local enrichment that the host tooling feeds to a local seat's link, never a primary source.

### 2.1 The seat state machine (computed in the seat's link)

States, each published with `since_ms` and a `reason`:

| State          | Entered when                                                                                                                                                                                                                              | Left when                                                                                                                           |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `starting`     | the link runs, no SessionStart yet                                                                                                                                                                                                        | SessionStart (→ `idle`); 120 s without it → `blocked:launch` (picker, launcher prompt, F12)                                         |
| `working`      | UserPromptSubmit                                                                                                                                                                                                                          | Stop (→ idle candidate), Notification dialog, StopFailure                                                                           |
| `idle`         | Stop with `stop_hook_active` false, then **no UserPromptSubmit for `idle_dwell` (60 s)**; published only after the dwell, so a Stop-hook re-prompt never flaps                                                                            | UserPromptSubmit                                                                                                                    |
| `waiting`      | `idle` and this seat has an open request it sent (ping awaiting an answer, or a `blocked` it posted) — idle *on someone*, named in the reason                                                                                             | the answer arrives (it will be delivered and start a turn)                                                                          |
| `blocked`      | reason `dialog` (Notification permission/elicitation), `human` (awaiting-human mode or an open question), `launch` (picker / prompt), `typed` (pane)                                                                                      | the next UserPromptSubmit / PostToolUse / Stop                                                                                      |
| `rate-limited` | a turn failed on a rate-limit error, or the status line reports a window at its limit, or the pane shows the limit banner; `until_ms` from `resets_at` or the parsed banner; reason `usage-limit` or `usage-pause` (the daemon's ceiling) | `until_ms` passed **and** a successful turn (UserPromptSubmit then a transcript write without error); never left on the clock alone |
| `stalled`      | `working` and no transcript write for `stall_after` (20 min, longer while the last entry is a Bash `tool_use` with a declared long timeout)                                                                                               | any transcript write                                                                                                                |
| `gone`         | not published by the seat: derived by readers from presence `offline`/`unavailable` + lifecycle notice. Sub-reasons `left`, `disconnected`, `unreachable` (presence lost, no notice)                                                      | the seat's link syncs again                                                                                                         |

Flags beside the state (do not change it): `context_pct` (with `context-low` at ≥ 90 %),
`pending_in` (deliveries not yet taken), `missing` (B 2.7.4), `open_out` / `open_in` (request
graph counts).

Distinguishing the four the owner named:

| Looks like nothing is happening | Presence            | Last lifecycle notice | Seat-published state   | Verdict                                                         |
| ------------------------------- | ------------------- | --------------------- | ---------------------- | --------------------------------------------------------------- |
| idle                            | online              | `back` / none         | `idle` / `waiting`     | **idle**                                                        |
| rate-limited                    | online              | none                  | `rate-limited` until T | **rate-limited until HH:MM**                                    |
| crashed                         | offline             | `disconnected`        | (stale)                | **gone: disconnected**                                          |
| offline (clean)                 | offline             | `left`                | (stale)                | **gone: left**                                                  |
| partitioned remote seat         | unavailable/offline | none                  | (stale)                | **gone: unreachable** (may be working; do not reassign at once) |

### 2.2 Rate-limited, in full (owner priority)

- **Detection, in order of authority**: (1) a turn that ends on a rate-limit API error — StopFailure
  hook **(verify)** or the transcript's error entry, both seen by the seat link; (2) the status-line
  JSON's `rate_limits` window at 100 % with `resets_at` **(verify)**; (3) the pane's usage-limit
  banner, parsed by a new `screen` classifier with real captures as fixtures (Plan 00135's fixture
  style); (4) the hooks daemon's usage-pause state (reason `usage-pause`, resume at its cron's
  time). (5) Host-side, `ccy --token-usage --json` tells the account's windows and resets for every
  token: the board can show *which other seats share the exhausted token* and will hit it too.
- **Reset time**: `until_ms` from `resets_at` when present, else the banner's time parsed with its
  zone, else unknown (shown as `rate-limited (reset unknown)`). Report text: `rate-limited until 15:00 (usage limit)`.
- **Pending messages**: while `rate-limited`, the seat's link **holds** live delivery (an inbox
  write would start a turn that fails at once and may be recorded as delivered); held items stay
  pending and are listed. The coordinator's view lists each open request to that seat as
  `undeliverable until 15:00`.
- **What the coordinator should do** (the report says it; the coordinator decides): reset within
  `wait_ok` (default 30 min) → wait; longer and the seat holds open requests → reassign to an idle
  worker; every worker rate-limited on one token → tell the human (a token switch).
- **Token hot-swap hook point**: the `→ rate-limited` transition is published locally too (the
  link's `<v2-run>/status.json`, readable by host tooling for local seats). A future proxy or
  `ccy` token switch subscribes to that local edge, never to the bus, so no token name or account
  fact crosses the network. After a swap, the first successful turn clears the state.

## 3. Candidate architectures

### A. Seat publishes through presence; coordinator's link aggregates (recommended)

- **Computed**: in each seat's link (reducer of section 2.1).
- **Lives**: in the homeserver's presence for the seat's account: `presence` stays what the
  long-poll makes it; the link sets `status_msg` to a compact, versioned, strict string, e.g.
  `tc1 working since=1762858000000` / `tc1 rate-limited since=… until=…` / `tc1 blocked since=… why=dialog`.
  Set by `PUT /presence/{u}/status` on a debounced change only (at most one per 10 s).
- **Crosses networks**: natively; every member already syncs the team's presence (A 6.10).
- **Coordinator reads**: its link already receives every member's presence event; it keeps the
  aggregate in memory, joins it with its own outbox (the request graph) and the lifecycle notices,
  and serves `teamchat team-status` from it (no network call), writes it into its `status.json`
  (the status line shows a one-glyph team summary), and decides notifications (section 4).
- **Turn cost**: zero for publishing and reading; turns only for actionable notifications.
- **Failure modes**: a dead link leaves a stale `status_msg`, but presence itself goes
  `unavailable`/`offline` (the server's timers), which overrides it to `gone`; presence on Tuwunel
  must keep `status_msg` across the long-poll's implicit `online` **(host check, like HC-A1)**; a
  homeserver restart may drop presence (each link re-sets it after its first sync).
- **Adds to v2**: A 6.6 revision (status_msg grammar, still "display, not liveness" for R27);
  proto: the grammar's parser/printer with corpus tests; member: kit hooks (Notification,
  StopFailure if verified), the reducer, transcript inotify, status-line field forward, the
  publisher; coordinator link: aggregator, `team-status` verb, notifier. No room, power-level or
  host change.

### B. Seat publishes a room state event keyed by its own user ID

- **Lives**: `teamchat.seat_state` state event in main, `state_key` = the sender's own user ID
  (Matrix auth rules reject a `@`-prefixed state key that is not the sender, so no impersonation,
  which was v1's problem).
- **Pros**: current state is fetchable in one `GET /rooms/{id}/state` by any reader (a coordinator
  that restarts, the host board, Element with hidden events on), and it is durable.
- **Cons**: reopens A 4.2/6.6 (a state type opened at power 0 in a room whose model is "only
  `m.room.message`"); every change is a permanent state event (a busy team: hundreds a day);
  a dead seat's event says `working` forever, so it needs an expiry and a heartbeat refresh,
  which multiplies history; the filter must add the type; Element may show it.
- **Turn cost**: as A. **Adds**: the power-level change through C's apply, proto type, filter
  change, plus all of A's member work.

### C. Host-side aggregator (homeserver host)

- **Computed**: per seat by its link as in A, but aggregated by a host process (the health tick or
  the seat broker of Plan 00197) that reads presence and lifecycle notices through `ctl-child`,
  adds local pane and lock evidence, writes the host status record (C 3.11.3) and the board.
- **Pros**: one view for humans independent of any coordinator; sees a coordinator that is itself
  stuck; can feed Plan 00224's desktop notification for the owner.
- **Cons**: the homeserver host is one machine (today the laptop); a remote coordinator still
  needs the bus to be told, so it does not replace A, it duplicates its reader; the tick is
  minutely, so latency; root/ctl side grows.
- **Turn cost**: zero for humans; coordinator notification still goes over the bus.
- **Adds**: a presence read in the ctl child, new status-record fields, board column.

### D. Pane-scraping supervisor per machine (herdr-style)

- **Computed**: on each machine, a supervisor (the ccy supervisor of Plan 00194, or ccy-teams)
  classifies each pane with the `screen` package and manifests, and posts the result.
- **Pros**: sees what no hook reports (picker, launcher prompts, typed text, the usage banner).
- **Cons**: text-shape fragile across Claude Code releases; needs tmux access, so not for every
  member kind; posting it cross-network needs a seat's credentials or a bus path anyway.
- **Use**: an **input** to A (host tooling writes a pane verdict into a local seat's runtime, the
  link folds it in), not an architecture of its own.

### E. Pull-only report command / status line

- `teamchat team-status` and a status-line segment, read when someone looks.
- **Pros**: smallest; zero turns.
- **Cons**: does not fix idle-lock: an idle coordinator never looks, and a coordinator that polls
  spends a turn per poll. Falls out of A for free; not sufficient alone.

| Option | Cross-network        | History cost | Detects gone          | Wakes an idle coordinator | Decision it reopens       | Size |
| ------ | -------------------- | ------------ | --------------------- | ------------------------- | ------------------------- | ---- |
| A      | yes                  | none         | yes (presence timers) | yes, policy-gated         | A 6.6 `status_msg`        | M    |
| B      | yes                  | grows        | needs expiry          | yes                       | A 4.2 power levels, A 6.6 | M+   |
| C      | homeserver host only | none         | yes                   | via the bus only          | none (host grows)         | M    |
| D      | no                   | none         | no                    | no                        | none                      | S-M  |
| E      | n/a                  | none         | as source             | no                        | none                      | S    |

## 4. Notification policy (no wasted turns)

**Principle**: a notification wakes the coordinator only for a condition it can act on, that is
new or changed since it was last told, and that has held for its dwell. Everything else rides
as trailing lines on the next delivery that happens anyway (the pattern of A 8.4's `answered:`
lines), or stays in `team-status`.

**Where it runs**: in the coordinator's link (it has the aggregate and the request graph, and it
already delivers through the inbox socket, which queues for a busy session, B18). One delivery
per decision, kind `TEAM-STATUS`, carrying every current actionable condition at once.

| Condition (all deterministic)                                                                                       | Dwell   | Actionable for                                    | Wakes the coordinator                              |
| ------------------------------------------------------------------------------------------------------------------- | ------- | ------------------------------------------------- | -------------------------------------------------- |
| C1 Worker `idle` with an open request from the coordinator that has no `done`/`blocked`/`nack` (F1)                 | 2 min   | coordinator                                       | yes                                                |
| C2 Worker `waiting` on the coordinator (its open ping to the coordinator) while the coordinator is idle (F4)        | 1 min   | coordinator                                       | yes                                                |
| C3 Whole team idle (every live seat `idle`/`waiting`/`blocked`/`rate-limited`) and at least one open request exists | 3 min   | coordinator, then humans                          | yes, once                                          |
| C4 Worker `rate-limited` holding open requests, reset beyond `wait_ok`                                              | 0       | coordinator (reassign)                            | yes                                                |
| C5 Worker `gone` (`disconnected` or `unreachable` > 5 min) holding open requests                                    | as said | coordinator                                       | yes                                                |
| C6 Worker `blocked: dialog` or `blocked: human` > 5 min                                                             | 5 min   | **humans** (a coordinator cannot answer a dialog) | no: trailing line; humans via Element / Plan 00224 |
| C7 Worker `stalled`                                                                                                 | 20 min  | coordinator (inspect / halt)                      | yes, once                                          |
| working↔idle with no open request, `starting`, recoveries, `context-low`                                            | -       | nobody                                            | no: trailing line or report only                   |

Rules:

- **Edge-triggered with generations**: each condition has a key (`C1:<seat>:<ping id>`) and is
  told once; it is told again only when it changes (a new ping, a new state) or on a backoff
  reminder (30 min, then 2 h) while it persists. Told keys persist in the coordinator's
  `session.json` (the pattern of B's told generations), so a restarted link does not re-wake.
- **Coalesce**: conditions arising within 30 s of each other go in one delivery.
- **Busy coordinator**: never interrupted; the delivery queues and runs at its turn end, and the
  conditions are re-evaluated just before the write so a resolved one is dropped.
- **Global ceiling**: at most one `TEAM-STATUS` wake per 5 min per team, except C3 and a new C5.
- **When the coordinator is the stuck one** (gone, rate-limited, blocked): no agent can be woken
  usefully; C3/C5 about the coordinator go to the team's humans (an Element mention costs no
  agent turn) and to Plan 00224's owner notification when that lands.
- **Free-text requests are invisible**: the graph sees only pings. The coordinator skill should
  say "dispatch work with a ping (`review`, `fetch`, `sync`, `run-qa`), never only free text";
  free-text-only dispatch reduces C1 to "worker idle" (dwell 5 min, trailing line only).

**Watch command**: `teamchat team-status [--team T] [--json] [--watch [--actionable]]`. One-shot
prints the table (state, since, reason, open requests, until) from the link's memory. `--watch`
prints one line per transition (or per actionable condition) and blocks: for a human terminal,
for the board, or for a Claude Code Monitor tool if an owner wants that route. The agent
coordinator does not need it, because the link's own delivery is the edge-triggered watch.

## 5. Concepts lifted from the watch list

| Repository                         | Concept                                                                                                                                                                                                                               | Use here                                                                                                                                                                                     |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| herdrdev/herdr                     | Every pane marked working / blocked / idle / done; detection from a bottom-first screen snapshot with evidence-based manifests (AND/OR gates of invariant controls); "wait until another agent is genuinely blocked"                  | The state vocabulary; manifests as the shape of the new `screen` classifiers (dialog, usage banner); the review (Plan 00200 scan-20261111T0947Z) already pointed the blocked-dialog gap here |
| OrchestratorInc/agent-orchestrator | Durable facts (`activity_state`: active / idle / waiting_input / blocked / exited, reported by TUI hooks) with **display status computed at read time**; per-turn subagent tracking so a background subagent keeps a session "active" | The seat publishes facts, readers derive verdicts (gone, idle-locked); count SubagentStop / background work as working **(verify hook payloads)**                                            |
| awslabs/cli-agent-orchestrator     | Terminal status IDLE / PROCESSING / WAITING_USER_ANSWER / COMPLETED / ERROR; delivery gated on status and never answers an open dialog; evidence that the receiver began **this** delivery                                            | Hold delivery while `blocked: dialog` or `rate-limited`; the same "began processing" proof B 2.7.4 already does                                                                              |
| agent-of-empires/agent-of-empires  | Push only for Waiting (after holding 5 s), Idle (a long job settles) and Error; a 60 s per-session cooldown; suppressed while the human is already looking                                                                            | Dwell + cooldown + suppression-while-attended in section 4                                                                                                                                   |
| cdknorow/coral                     | One "Needs you" inbox of every agent waiting on a permission, question or decision; idle agents sleep and wake on a message                                                                                                           | A `needs-human` section in `team-status` (C6), routed to humans not the coordinator                                                                                                          |
| Ark0N/Codeman                      | Respawn controller: idle detected → continue / clear / resume; auto-resume when a subscription limit resets                                                                                                                           | Out of scope (non-goal), but `rate-limited until` is the trigger such a later plan needs                                                                                                     |
| Maxteabag/hotseat                  | Per-account quota status and a "Work" list of sessions a usage limit stopped, with a status-line segment                                                                                                                              | The rate-limited list and the "which seats share this token" view on the host board                                                                                                          |
| ldegio/agtop                       | A top-style view sortable by status                                                                                                                                                                                                   | `team-status` table sorted by actionability, then since                                                                                                                                      |
| mvschwarz/openrig                  | Teams with roles over herdr-classified panes                                                                                                                                                                                          | Confirms the role + pane-state split; nothing extra to lift                                                                                                                                  |

READMEs read as data only; nothing was run.

## 6. Relation to neighbouring plans and the board

- **Plan 00203**: its "idle with an empty box for 2 minutes" pane test is the pane input of the
  reducer (`blocked: typed`, idle confirmation for a local pane). Reuse its function; do not add a
  second idle detector.
- **Plan 00194**: live delivery already decides "idle since the write"; the reducer reads the
  same facts. The ccy supervisor could later write a pane verdict into the runtime for D's input.
- **Plan 00135 Phase 10**: the `screen` package grows the classifiers this needs (dialog, usage
  banner) next to `AtConversationPicker`, with real-capture fixtures.
- **Plan 00224**: carries the owner half (C3/C5 about the coordinator, C6); this plan supplies the
  edge, 00224 the desktop route. Its "remote members use the bus" seam is exactly this transport.
- **Plan 00221**: the tree board shows each member's seat state; with labels the line reads
  `2 (front) working 12m`.
- **Plan 00246**: cross-network membership; a remote seat's `unreachable` vs `disconnected`
  depends on that machine's own sweep, which 00246 Task 8.8 must provide.
- **Plan 00196 / 00197**: the coordinator role decides who gets C1-C5; the broker (if built) is a
  natural home for C's host aggregator.
- **The ccy-teams board's STATE column** (`board/table.go`, `Row.Live agentbus.Liveness`): v1,
  host-local "listening / stale / parked". For v2 rows the board gains the seat state read from the
  same presence (through the ctl child, or the owner's own read of the host status record), shown
  as `working 12m`, `idle 3m`, `blocked: dialog`, `rate-limited until 15:00`, `gone: unreachable`.
  Liveness becomes one input of the verdict (the `gone` row of section 2.1), not a separate column;
  `parked` stays the roster's word and wins over everything. v1 rows keep today's liveness.

## 7. Recommendation and open questions

**Recommended**: **A**, with D's pane evidence as a local input and C as a later, optional host
view.

1. Reducer in the seat's link (section 2.1), pure and table-tested with fake clocks; inputs:
   existing hook facts, new Notification (and StopFailure if verified) kit hooks, transcript
   inotify, status-line fields, optional local pane verdict and hooks-daemon mode.
2. Publish through presence `status_msg` (A 6.6 revised), debounced; also into the link's local
   `status.json` (for the status line, the board and a future token-swap proxy).
3. Coordinator's link aggregates presence + lifecycle notices + its outbox into `team-status` and
   decides `TEAM-STATUS` deliveries by section 4's table; humans get C6 and coordinator-stuck
   conditions.
4. Hold live delivery to a seat that is `rate-limited` or `blocked: dialog`; report its pending
   items as undeliverable until reset.

Owner decisions:

- **Q1** Carrier: presence `status_msg` (A, revises A 6.6, needs a Tuwunel host check that the
  long-poll keeps it) or a state event per user ID (B, revises A 4.2)? Default A.
- **Q2** Does the coordinator get woken for C3 "whole team idle with open requests", or only humans?
  Default both, once.
- **Q3** Dwell and threshold defaults (idle 60 s, C1 2 min, blocked 5 min, stall 20 min,
  `wait_ok` 30 min): team settings in the roster, or fixed constants? Default constants.
- **Q4** Hold delivery while rate-limited (default yes) — or deliver and let the turn fail?
- **Q5** Should the coordinator skill require pings for dispatch, so C1 is exact? Default yes.
- **Q6** Should workers see each other's state (all members sync presence already), or the
  coordinator only? Default visible to all, notified only to the coordinator.
- **Q7** C (host aggregator) now, or after 00224 and 00221 land? Default after.
- **Q8** Rate-limit trigger for the token hot-swap work: the local `→ rate-limited` edge in
  `<v2-run>/status.json` is the proposed hook point; confirm the proxy will be host-local.

Probes needed before a spec (verify list): Notification hook `notification_type` values; a
StopFailure hook and its rate-limit payload; status-line `rate_limits` and `context_window` fields;
Tuwunel presence keeping `status_msg` across `set_presence`-less syncs and a restart; the usage
banner's text at several widths (fixtures).
