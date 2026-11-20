# Usage and observability tools: study

Plan 00258, Task 2.1, group "usage and observability". Seven repositories, read only, nothing
built or run. Clone paths are under `untracked/starred-repos/`. Commit counts are from the local
clones, whose history may still have been arriving while this was written; treat activity
figures as lower bounds.

What we compare against, read for this study:

- **ccy token usage**: `docs/ccy.md` (sections "Seeing usage limits before you pick", "No session
  on a token with no usage left"), `src-go/internal/ccylaunch/tokenusage.go`,
  `src-go/cmd/ccy-token-usable`. ccy reads usage by sending one billed Haiku request per token and
  reading the `anthropic-ratelimit-unified-*` response headers, cached 15 minutes per token.
  `/api/oauth/usage` is closed to ccy's setup-tokens (they lack the `user:profile` scope):
  `CLAUDE/Plan/Completed/00101-ccy-token-usage-via-ratelimit-headers/FINDINGS.md` F9, F22.
- **Hooks daemon**: the status line's usage chips (`handlers/status_line/usage_indicator.py`,
  fed from Claude Code's own `rate_limits` payload), prompt-cache chips
  (`prompt_cache_indicator.py`), the usage-pause gate and its supervisor record
  (`utils/usage_pause.py`), `stop_failure_recorder`/`limit_rebrief`, the resume guard
  (R-RESUME-COLD-LARGE), and the `optimal_config_checker` idle-compaction check.
- **Plans**: 00146 (token switch through a local forwarder), 00181 (session limits), 00256
  (idle stand-down), 00251 (seat status; its research already cites hotseat and agtop).
- **Watch list**: six of the seven repositories are already rows in `CLAUDE/Watchlist.tsv`;
  `inevolin/claude-code-usage-analysis` is not.

---

## Maciek-roboblog/Claude-Code-Usage-Monitor

Studied: baseline at c59a83bf943f329f0e61f1a29c760353ee1860a5

**What it is.** A Python Rich terminal monitor (`claude-monitor`) for one person's Claude Code
usage: live bars, burn rate, forecasts, a statusline mode, JSON/one-shot output and an opt-in
local "warehouse" of history. For a single user on one machine.

**Architecture.**

- *Source of data, three tiers with provenance* (`src/claude_monitor/output/snapshots.py`,
  `official.py`, `api_usage.py`):
  1. Official: run as a Claude Code `statusLine` command (`--statusline`), it saves the
     `rate_limits.{five_hour,seven_day}` block from the status payload with a capture time and
     flags it stale after a TTL (`official.py` `capture_statusline`, `read_official_limits`).
  2. Experimental: an opt-in GET of `https://api.anthropic.com/api/oauth/usage` with
     `anthropic-beta: oauth-2025-04-20`, using the token in `~/.claude/.credentials.json`;
     180-second cache, `Retry-After` honoured on 429 and persisted (`api_usage.py`
     `read_api_limits`). Labelled `confidence="experimental"`, never outranks a fresh official
     capture.
  3. Local estimate: reads every `*.jsonl` under `~/.claude/projects`, dedupes on
     `(message.id, requestId)` (`data/reader.py` `_create_unique_hash`), groups entries into
     5-hour blocks starting at the hour (`data/analyzer.py`), and estimates a limit.
- *Limit prediction.* The legacy "custom plan" takes the 90th percentile of past blocks' token
  totals, preferring blocks that look like they hit a limit (`core/p90_calculator.py`). Fixed
  per-plan token limits (Pro ~44k, Max5 ~88k, Max20 ~220k) are hard-coded guesses. The v4 README
  itself now says to prefer the official statusline data.
- *Pace.* For the 5-hour window only, it compares percent used with percent of the window
  elapsed and labels the gap "slow down" / "on track" / "speed up" with a 10-point tolerance
  (`snapshots.py` `_pace_from_window`).
- *Automation contract.* `--once` exits 0 ok, 10 near limit, 11 limit hit, 20 indeterminate,
  30 no data (README; `snapshots.py` `_status`).
- *Limit detection from transcripts*: scans system messages and tool results for limit text
  (`data/analyzer.py` `_detect_single_limit`).
- *Warehouse*: versioned JSON with retention, so history survives Claude Code's own transcript
  clean-up (`data/warehouse.py`).
- Multi-account: none. Per agent/session: per 5-hour block and per model. Remote: none.

**Maturity.** 170 commits, 10 authors, v4.0.0. Commits cluster in June-July 2025 and one burst in
June 2026 (v4); nothing since. Large test suite (`src/tests/`). Popular, but now quiet.

**Licence.** MIT (`LICENSE`, "Copyright (c) 2025 Maciej"). No per-file headers. Reuse: lift.

**Compared with us.** Behind on the essentials. The hooks daemon already reads `rate_limits`
from the status payload on every render (usage_indicator), shows reset countdowns and a
ceiling, and pauses a session at it. ccy reads per-token figures from response headers, which is
more exact than any token-count estimate. The P90/fixed-limit machinery is obsolete now that
Claude Code ships real percentages. Where it is ahead: the **provenance and freshness label** on
every figure (official / experimental / estimate, stale or not), the **pace** label, and the
**exit-code contract** for scripts.

**Ideas.**

| Idea                                                                                                                                                                    | Where it lands                                                                  | Kind                           |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------ |
| Pace label (used % against elapsed % of the window) beside the 5-hour and weekly bars in the ccy token menu and `ccy --token-usage`; the daemon chip could carry it too | `src-go/internal/ccylaunch/tokenusage.go`; daemon `usage_indicator.py` upstream | issue                          |
| Provenance on every usage figure (probe, status line, cache age) once more than one source feeds ccy's cache (see takeaway 2)                                           | ccy usage cache                                                                 | part of the plan in takeaway 2 |

**Code worth lifting.** Nothing worth the notice: the pace function is a few lines and easier
to write in Go than to port.

**Risks / avoid.** Hard-coded plan token limits and percentile "limit discovery" give
confident wrong numbers. `oauth/usage` with an interactive-login token is undocumented and
reads the desktop login's credential file, which ccy deliberately never touches.

---

## inevolin/claude-code-usage-analysis

Studied: baseline at e9451a4334172cc5c1f0efa84c50981b814cd0ea

**What it is.** A measured study, with an analyser, of what actually drives Claude Code usage:
prompt-cache misses, not typing. Python analyser (`src/cc_usage_study/`), a simulator, live runs,
and a self-critical review (`FINDINGS.md`). For individual users tuning habits.

**Architecture.** Reads `~/.claude/projects/**.jsonl`, dedupes streamed chunks on
`(message.id, requestId)`, includes sub-agent and sidechain files, and prices calls in
"cache-weighted units" (input 1, 5-minute write 1.25, 1-hour write 2, read 0.05, output 5)
(`README.md` "Methods in brief"; `FINDINGS.md` "Where the study is right"). It reports
`ttl_expired_rebuild` counts per session and splits 5-minute and 1-hour cache writes. No live
monitor, no accounts, no remote.

**Findings (all on Claude Code 2.1.325, Opus 5.5; most n=1-3, marked preliminary by the author):**

01. **A break longer than the cache TTL costs about 10x** the next small task against a fresh
    session (97k-token history, 5-minute cache, 10-13 minute breaks, n=3). On a subscription the
    TTL is 1 hour, so the cliff moves to the hour and each rebuilt token is a 2x write.
02. **`/compact` before a break: ~2.5x; after the break: ~10x** (no better than just continuing).
    A cold compaction sends the history as plain input.
03. **`/clear` plus a three-line note (goal, done, next) after a break is ~1x**. Staying warm for
    a run of small related fixes is ~28% cheaper than `/clear` between them (n=1).
04. **Scheduled `claude -p --resume` across a gap is ~3x** a fresh session each run (modelled,
    not measured live).
05. **Idle compaction** (on by default since v2.1.299) summarises a session at ~90% of the
    1-hour TTL, but only on the 1-hour cache, only while the process is alive, and only above
    `CLAUDE_CODE_IDLE_COMPACT_MIN_TOKENS` (200,000 default, 100,000 minimum). Advice: set it to
    100,000.
06. **Where auto-compaction fires** is about 33,000 tokens below the number given, and the check
    runs between requests, so one big tool result can overshoot it by up to ~55%. Never stack two
    compaction knobs; `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=25` on a 200k window thrashes.
07. **Compaction is partly invisible**: summary calls were 12-26% of usage in compacting runs and
    appear only in Claude Code's own cost line, not the transcript. Lower triggers did not save
    usage live (`--autocompact 100000` was +25%).
08. **A `CLAUDE.md` edit or a new MCP server (with tool search on) did not break the cache**;
    switching a tool off or switching model mid-task cost ~22x a normal turn (n=2).
09. **Sub-agents** have their own 5-minute cache unless `CLAUDE_CODE_SUBAGENT_PROMPT_CACHE_TTL=1h`
    (untested by the study).
10. **Raw tokens mislead**: one setting sent 63% fewer raw tokens and cost 25% more.

The author's own `FINDINGS.md` lists the caveats: small n, a simulator that failed back-tests
for long-session compaction, API list price not subscription weights.

**What it implies for how we run sessions.** These are inferences, not measurements of ours.

- **The failsafe cron is hourly** (`FAILSAFE_CRON_SCHEDULE_HINT = "47 * * * *"`,
  `.claude/hooks-daemon/src/claude_code_hooks_daemon/utils/cron_enforcement.py`). An otherwise
  idle session's previous request was roughly 59-60 minutes before the next tick, which is the
  edge of the 1-hour TTL. *Guess:* many ticks land just after expiry and pay a full cache
  rebuild for a turn that usually finds nothing. The daemon's backed-off tick
  (R-FAILSAFE-CRON-BACKED-OFF) reduces the count but, if each tick is still past the TTL, not
  the cost per tick. The daemon's own prompt-cache chip already knows `expires_at` and
  `miss_causes`, so this is measurable from data we have.
- **Restores, `--max-age` restarts and the usage-pause resume** bring a session back after a
  gap of hours. The study says resuming a large stale history costs several times a fresh
  start with a handoff. The daemon already guards cold resumes of large sub-agents
  (R-RESUME-COLD-LARGE); the session-level equivalent (ccy restore and restart) does not
  appear to weigh it.
- **Plan 00256 (idle stand-down) and the usage pause's "compact once"**: compaction should
  happen while the cache is still warm (before the stand-down or before the hour runs out),
  not after the wake. That is a design point for 00256.
- **ccy containers**: `CLAUDE_CODE_IDLE_COMPACT_MIN_TOKENS=100000` and
  `CLAUDE_CODE_SUBAGENT_PROMPT_CACHE_TTL=1h` are candidates for the ccy container environment.
  The daemon's `optimal_config_checker` already checks that idle compaction is not disabled.
  Neither variable is set anywhere in this repository today.
- **Our heavy hook output** (large advisory blocks on many tool calls) lands in context and is
  re-read every turn. Finding 10 says raw size is not the cost, cache misses are; growth is
  cheap while warm. This argues *against* spending effort shrinking hook output for cost
  reasons, and *for* spending it on avoiding cold turns.

**Maturity.** 47 commits, 5 authors, 2026-11-13 to 2026-11-17: new and research-shaped. Tests
and a back-test pipeline exist (`tests/`, `outputs/validation.json`).

**Licence.** MIT (`LICENSE`, "Copyright (c) 2026 Ilja Nevolin"). Reuse: lift.

**Compared with us.** Different kind of thing: a study, not a tool. We are ahead on live
instrumentation (the daemon's cache chip reads Claude Code's own `prompt_cache` payload; the
analyser reconstructs it from transcripts after the fact). We are behind on having measured
what our own session-running policies cost.

**Ideas.**

| Idea                                                                                                                                                                                                                                 | Where it lands                                                       | Kind                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Measure what an hourly failsafe tick costs on an idle ccy session (cold or warm, from the prompt-cache payload), then choose: a cadence inside the TTL, a much longer one, or skip a tick when the cache is cold and nothing is owed | hooks daemon (upstream) and ccy supervisor                           | plan                                                              |
| Cache-aware session return: a ccy restore/restart of a large, long-idle session starts fresh with a handoff instead of `--resume`, or compacts first while warm                                                                      | `ccy-sessions restore`, `lib/session-lifecycle.bash`, Plan 00256     | plan (fold into 00256)                                            |
| Set `CLAUDE_CODE_IDLE_COMPACT_MIN_TOKENS=100000` and `CLAUDE_CODE_SUBAGENT_PROMPT_CACHE_TTL=1h` in ccy containers, after a measurement on one session                                                                                | ccy container env (`files/var/local/claude-yolo/`), CCY version bump | needs discussion (the first is from a binary read at one version) |
| Add this repository to the watch list (it is the only one of the seven not on it; it will keep publishing measurements)                                                                                                              | `CLAUDE/Watchlist.tsv`                                               | issue                                                             |

**Code worth lifting.** None needed. The dedupe key `(message.id, requestId)` and the
cache-weighted unit table are facts, not code.

**Risks.** Small samples, one model, API list price. The author says so. Treat every multiple
as a direction, and measure on our own sessions before changing a policy.

---

## DevDock-AI/claude-unlimited

Studied: baseline at b2869e150cab91ab0fe8e0e20be8fdc08d3a5183

**What it is.** A local daemon (`cu`) on `127.0.0.1:4317` that Claude Code reaches through
`ANTHROPIC_BASE_URL`. It pools several Claude subscriptions, API keys and ChatGPT/Codex
accounts and swaps the credential per request, so a session moves to another account without
restarting. Web dashboard, macOS HUD, notifications. Python standard library only, ~22k lines
plus 99 test files. For one person with several accounts. **It is the most complete working
example of Plan 00146's design.**

**Architecture.**

- *Proxy* (`claude_unlimited/proxy.py`): strips inbound `Authorization`/`x-api-key` and hop
  headers, adds the account's credential, drops `accept-encoding` so the response SSE can be read
  for usage, and passes only an allowlist of response headers to the classifier
  (`RESPONSE_HEADER_ALLOWLIST`).
- *Observation* (`observation.py`): pure classification of status code plus rate-limit headers
  into usage snapshot / quota exhausted / short rate limit / auth failure. **Only a snapshot at
  or above the switch threshold, or real exhaustion, rotates; a bare 429 never does** (it gets a
  cooldown, escalating when there is no `Retry-After`).
- *Router* (`router.py`): per-account states ELIGIBLE, DRAINING (threshold crossed, finish
  in-flight, take nothing new), COOLDOWN, EXHAUSTED, re-auth. Sticky: stays on an account until
  its threshold. Optional per-model rule: when an account's Fable weekly window is spent, the
  whole session moves (`fable_spent`, `must_leave`).
- *Distribute mode* (`router.py` `_branch_weights`, `_weekly_spend_rate`, `_five_hour_room`;
  README "Balancing sessions and subagents"): each new agent (main or sub-agent) is placed on an
  account in proportion to unused weekly quota per hour left before that week resets, capped at
  3x the pool median and scaled down as the 5-hour window fills; once placed, an agent stays put
  so its prompt cache stays warm, moving only at a threshold, a limit, an outage or after an hour
  idle (`gateway.py` `BRANCH_PIN_TTL_SECONDS = 3600`).
- *Per-agent identity*: Claude Code labels requests with `x-claude-code-session-id`,
  `x-claude-code-agent-id` and `x-claude-code-parent-agent-id`
  (`project_attribution.py`), which the daemon uses for pinning and for per-project and
  per-agent usage attribution.
- *Usage probe* (`usage_probe.py`, ADR `docs/adr/0001-...md` amendment): reads each account's
  `/api/oauth/usage` only while the user is active, at most every 5-10 minutes with jitter,
  with persisted escalating backoff (429: at least 15 minutes, doubling to 6 hours, pausing the
  whole provider; 401/403: hours, and never marks the account broken). Per-model weekly windows
  come only from this endpoint.
- *1M context through a proxy* (ADR `docs/adr/0008-1m-context-through-the-gateway.md`): measured
  with Claude Code 2.1.278, **any non-`api.anthropic.com` base URL makes Claude Code budget every
  native-1M model at 200K**. The cause, from the client binary, is a hostname check; the fix the
  project ships is `_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1` for the launched client
  (`cli.py` `_apply_one_million_context`). The ADR verifies it at zero cost by reading
  `context_window.context_window_size` from a status-line payload.
- *Credentials*: imports Claude Code's own login (keychain or `~/.claude/.credentials.json`) and
  refreshes it with Claude Code's public OAuth client id, mimicking Claude Code's refresh request
  shape to avoid a throttled bucket (`oauth_login.py`). Setup-tokens are accepted but cannot read
  `oauth/profile`/`oauth/usage` (`anthropic_oauth.py` `scope_limited`), which matches our F9.
- *ECO* (`eco.py`): optional rewriting of tool output in the request before it leaves (dedupe
  lines, summarise logs, truncate diffs). *Speech* (`speech.py`): injects a terse-reply
  instruction into the system prompt.
- *OpenAI bridge*: translates Anthropic Messages to Codex/ChatGPT and back.
- Remote/mobile: none (loopback only).

**Maturity.** 124 commits, 3 authors, August-November 2026, steady. Eight ADRs, CI, a large test
suite. Fast-moving; README version claims track Claude Code releases.

**Licence.** MIT (`LICENSE`, "Copyright (c) 2026 Claude Unlimited contributors"). Vendored
`claude_unlimited/static/vendor/` (charts.css) is MIT, Leon Sorokin. No per-file headers in
`claude_unlimited/*.py`. Reuse: lift.

**Compared with us.** Ahead of Plan 00146 in every respect that has been built: rotation
policy, draining, per-model limits, per-agent pinning, persisted backoff. ccy is ahead on
containment: 00146 keeps the real token on the host and out of the container, whereas
claude-unlimited holds every credential in one loopback daemon that any local process can call
with its local token. ccy's setup-tokens also avoid the refresh-token race that importing a
desktop login brings. We have nothing like the OpenAI bridge, and should not want it.

**Ideas.**

| Idea                                                                                                                                                                                                                                                              | Where it lands                                                                       | Kind                                                                     |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| **The 1M-context cap behind a base URL**: add it to Plan 00146 as a known consequence, with the zero-cost status-line check of `context_window.context_window_size` as an acceptance test, and decide whether to rely on an underscore-prefixed internal variable | Plan 00146 (PLAN.md, research, Phase 0 spike)                                        | issue against the plan (a fact the plan lacks), decision inside the plan |
| Router semantics for 00146 phase 2 (rotation): sticky until a threshold, DRAINING, never rotate on a bare 429, a per-model weekly rule, escalating cooldown when no `Retry-After`                                                                                 | Plan 00146 forwarder (Go)                                                            | plan (already exists; add as design input)                               |
| Per-agent pinning by `x-claude-code-agent-id` so a rotation never breaks a warm sub-agent cache, and per-agent usage attribution for free once the forwarder sees every request                                                                                   | Plan 00146 forwarder; Plan 00251 (requests in flight per seat is a "working" signal) | plan                                                                     |
| Persisted, escalating backoff for any usage read, shared across processes (see also hotseat)                                                                                                                                                                      | `ccylaunch/tokenusage.go`                                                            | issue                                                                    |

**Code worth lifting.** As a reference to port to Go, with the MIT notice kept in the ported
file's header:

- `claude_unlimited/router.py`: the state machine and `_branch_weights` /
  `_weekly_spend_rate` / `_five_hour_room` placement maths.
- `claude_unlimited/observation.py`: the classification table (which header and status
  combination means what).
- `claude_unlimited/usage_probe.py`: the `Scheduler` backoff rules.

**Risks / avoid.**

- **Terms.** It routes requests through subscription credentials and refreshes them while
  posing as Claude Code's OAuth client and User-Agent (`oauth_login.py`). Plan 00146's research
  already quotes the consumer terms on intermediating credentials; this tool sits further into
  that grey area than 00146's single-owner, own-tokens design. Do not copy the credential
  import or the refresh impersonation.
- ECO and the speech injection change what the model sees. Off by default there, and
  correctly so. *Guess:* rewriting earlier tool output is cache-safe only if the rewrite is
  byte-stable across turns; a non-deterministic rewrite would break the cached prefix on every
  turn.
- The 1M fix depends on an internal variable read from one Claude Code version's binary. It
  may disappear in any release.

---

## Maxteabag/hotseat

Studied: baseline at ccecd72c4524f237be52c97d0968e1a67eb12823

**What it is.** A Go terminal dashboard and CLI for the Claude Code, Codex and Muse Code accounts
on one machine: quota left per account and per window (5-hour, 7-day, 7-day Fable), reset times,
which account each session uses, and a "Work" screen of sessions that a usage limit stopped,
which it can resume on an account with room. Single static binary, Bubble Tea UI. Same author
as clarp, with an integration package for it (`internal/clarp/`).

**Architecture.**

- *Quota*: the providers' own usage endpoints, no inference requests (README; `internal/quota/`).
  Successful reads are cached 60 seconds; 429 backs off exponentially from 60 seconds and
  honours `Retry-After`. **The cooldown is shared across processes**: per-token cache files named
  by the token's SHA-256 (so the token never reaches disk) and a per-token OS lock, re-reading
  the cache after taking the lock, so concurrent readers never fire duplicate requests
  (`internal/quota/cache.go`, `read_lock_unix.go`; `docs/quota-data.md`). "Not reported" is never
  shown as 0%, and a 429 is a failed lookup, not an exhausted account.
- *Accounts*: `hotseat use <alias>` gives a session its own config dir holding only identity and
  credentials, with settings, skills and history symlinked back; `hotseat switch` rewrites the
  shared credential (asks first, counts affected sessions). Token refresh is delegated to the
  real `claude` CLI in a throwaway config dir with one capped Haiku request, never reimplemented
  (README "Expired tokens").
- *Stopped work* (`internal/work/scan.go`, `resume.go`): reads each recent transcript backwards
  in growing blocks (8 KiB to 256 KiB) only until the last user/assistant turn; the session is
  "stopped on a limit" when that last turn is an `isApiErrorMessage` record whose text matches
  `hit your (session|usage) limit`. Verdicts are cached by size and mtime. Resume uses a fixed
  continuation prompt that tells the agent to re-check the interrupted step and not repeat
  finished work. Readiness checks block an account whose weekly or Fable window is spent.
- Per session: title, cwd, last activity, last message. Remote: none. Status line:
  `hotseat statusline` names the account.

**Maturity.** 26 commits, 2 authors, September-November 2026, releases with deb/rpm/Homebrew.
Recent commits fix real-use problems (stop reading gigabytes per listing; share the usage
cooldown across processes).

**Licence.** MIT (`LICENSE`, "Copyright (c) 2026 Maxteabag"). No per-file headers. Reuse: lift.
Go, so directly reusable in `src-go/`.

**Compared with us.** ccy's token menu shows the same bars per token, but by a billed probe;
hotseat cannot read setup-tokens' usage either (same endpoint, same scope wall). Our
`src-go/internal/transcript` cache (incremental, inode- and head-checked) is more capable than
hotseat's tail scanner. The daemon already records a turn that ended on a usage limit
(`stop_failure_recorder`, `limit_rebrief`). Ahead of us: a **host-wide list of sessions stopped
by a limit, with which token could resume them**, and the **cross-process usage cooldown**.

**Ideas.**

| Idea                                                                                                                                                                                                                                                                          | Where it lands                                                                                                                                         | Kind                                              |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| A "stopped on usage limit" state in `ccy-sessions` and the ccy-teams board, from the transcript's last turn (the `isApiErrorMessage` + limit-text signal) or the daemon's stop-failure record, with the reset time                                                            | `src-go/internal/sessions`, `internal/teams/board`; Plan 00251 (its "rate-limited" state, and its open question about which signal marks a limit stop) | plan (00251)                                      |
| Cross-process lock and shared backoff for ccy's per-token usage cache. *To verify:* parallel launches on one token (a boot restore) may each find the entry stale and each send a billed probe, and a 429 is recorded as "never fresh", so the next launch asks again at once | `src-go/internal/ccylaunch/tokenusage.go` (`prime`, `fresh`)                                                                                           | issue (after reading the restore path to confirm) |

**Code worth lifting.**

- `internal/quota/cache.go`, `internal/quota/read_lock_unix.go`: per-token hashed cache file and
  lock-then-recheck pattern.
- `internal/work/scan.go` `endsOnLimit`/`judge` and `internal/work/resume.go` `limitText`:
  small, but the exact detection rule is what 00251 needs. Keep the MIT notice.

**Risks.** `hotseat switch` rewrites a credential every running session reads, mid-conversation;
ccy's per-container token is the safer model. The limit text is an English UI string and will
change; pair it with the structured stop-failure hook.

---

## ldegio/agtop

Studied: baseline at dc04da34565a6032aca76ce797aa967bb7d18c56

GPL-2.0: clean room, ideas only.

**What it is.** A `top`-style Node.js terminal dashboard (one ~9,500-line `index.js`) of every
Claude Code and Codex session on the machine: spend, tokens, context fullness, CPU and memory,
tool activity, sub-agents, configuration in force, with list and JSON modes.

**Behaviour (described, not transcribed).**

- Finds sessions from the transcript directories of both tools and computes cost from token
  counts with a public model price table fetched and cached for a day (README "How It Works").
- Maps running OS processes to sessions: first by a session id visible in the process arguments,
  then by which transcript file the process holds open and its working directory; it walks the
  process tree to attribute child processes (builds, test runs) to the session, and avoids
  double-counting a wrapper process and the real binary it starts.
- Shows context pressure as a percentage of the window per session.
- Lists the sub-agents a session dispatched with type, model, cost, tools and duration, and
  rebuilds rows from the parent transcript when Claude Code has already deleted the sub-agent's
  own transcript ("ghost" rows).
- A configuration panel shows, per session, which instruction files, memories, skills, MCP
  servers and permission files apply.
- Recent work is about cost of polling: not re-reading unchanged files, not pinning a core on a
  growing transcript.

**Maturity.** 131 commits, 6 authors, March-October 2026; busiest in March-May, quiet since
early October. npm package 0.6.1.

**Licence.** GPL-2.0-only (`LICENSE`, `package.json` `"license": "GPL-2.0-only"`). Reuse: clean
room. No code, paths only.

**Compared with us.** `ccy-sessions` already shows a CPU column (container plus host side), the
token and SSH key per session; the daemon's status line shows context and cache per session from
inside. Ahead of us: one **host-wide** table of all sessions with context %, cost and tool
activity, and **sub-agent visibility after purge**. 00251's research already took the "sort by
actionability" idea from it.

**Ideas.**

| Idea                                                                                                                                     | Where it lands                       | Kind                                                              |
| ---------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ | ----------------------------------------------------------------- |
| Context % and last-tool per session in `ccy-sessions`, read from the daemon's per-session snapshot rather than by re-parsing transcripts | `src-go/internal/sessions`           | plan (pairs with takeaway 2's host-side read of daemon snapshots) |
| Sub-agent rows reconstructed from the parent transcript when the child's file is gone                                                    | hooks daemon spend ledger (upstream) | needs discussion, low priority                                    |

**Risks.** GPL: anyone implementing from this section must not open the source. Process
attribution by open files needs `lsof`-style access that rootless podman containers make
awkward from the host.

---

## sodiumsun/agenttrail

Studied: baseline at 3785d4ea62ca58b9ded853b2e4c3b99f12c02c97

**What it is.** "Local observability for AI coding agents": a dependency-free Node daemon on
`127.0.0.1:5330` and a browser map of one repository, showing where each Claude Code or Codex
session is working, its tool activity, its task list, and progress against a `PLAN.md`
convention. Optional Claude Code hooks; a Herdr desktop mod. Explicitly no token billing.

**Architecture** (`docs/OBSERVABILITY.md`, `docs/INGEST.md`, `bin/lib/claude.mjs`,
`bin/lib/logtail.mjs`).

- Tails transcripts touched in the last two hours, at most the last 2 MiB of each, with saved
  read positions so a restart neither repeats nor loses events. Follows `CLAUDE_CONFIG_DIR`.
- Optional hooks post the same events live; **hook and log evidence merge into one run per
  session id**, a log row counting only if newer than the last hook heard.
- **Every fact carries an evidence class**: reported (plan status, task lists, lifecycle
  events), observed (file changes, tool calls), inferred (component from file globs), unknown
  (missing history; "an empty list that was reported is different from a session that never
  reported one"). A partial log says `history: partial`.
- An ingest endpoint (`POST /hook`, one JSON event schema: SessionStart, PreToolUse,
  PostToolUse, TodoUpdate, Stop, SessionEnd, SubagentStop) lets any agent report itself; a
  versioned read-only `GET /panel` summary feeds other front-ends.
- Local-only hardening: refuses non-local `Host`, rejects foreign `Origin`, per-board token for
  setup actions.
- Session-start hook reminds the agent when the plan has fallen behind the code.

**Maturity.** 147 commits, 2 authors, August-November 2026 (a big August, active again in
November). npm 0.4.1, tests in `test/`.

**Licence.** MIT (`LICENSE`, "Copyright (c) 2026 Kelly Sun"; README: "MIT for the project code
and original assets"). Reuse: lift.

**Compared with us.** Different centre of gravity: it maps work to a plan's components; we
track session health and usage. Our plan workflow and hooks daemon already do the
"plan behind code" nagging (plan QA, Plan Commit Rule) much more strictly. Ahead of us: the
**evidence vocabulary** and the **hook-plus-log merge** for one session.

**Ideas.**

| Idea                                                                                                                                                                   | Where it lands       | Kind         |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------- | ------------ |
| Label each seat-state fact in 00251 by evidence class (reported by the seat, observed from pane/transcript, inferred, unknown), and show "unknown" rather than a guess | Plan 00251 design    | plan (00251) |
| Hook events and transcript tail merged per session id, transcript filling gaps while the receiver was down                                                             | Plan 00251 seat link | plan (00251) |

**Code worth lifting.** None needed; the value is in the documented contract
(`docs/OBSERVABILITY.md`, `docs/INGEST.md`), which can be cited.

**Risks.** Its read positions and stored command snippets under `~/.agenttrail` are a second
copy of sensitive transcript text; anything similar of ours must keep to the
untracked/`0600` rules.

---

## Maxteabag/clarp

Studied: baseline at 59da071013fdfc79532b0df4e9d526ad0c9ba96f

PolyForm Shield 1.0.0: clean room, ideas only.

**What it is.** A self-hosted server plus PWA and native iOS client for talking to Claude Code
and Codex from a phone over Tailscale or a relay, with voice in and out, teams of agents,
"Janitor" maintenance agents, an attention inbox, durable goals, and Claude account failover.
Very large (3,800+ files) and extremely active: 4,582 commits from 2 authors since September
2026\. A separate commercial licence is offered (`COMMERCIAL_LICENSE.md`); contributions need a
CLA (`CLA.md`).

**Licence.** PolyForm Shield 1.0.0 (`LICENSE.md`). It forbids using the software to provide a
product that competes with it, "agent-control" products named in the README. Reuse: clean room.
Since ccy-teams and the agent bus are arguably in the same space, keep this repository strictly
to ideas: no code, no transcribed rules, no copied prompts. File paths below are for reference
only.

**Behaviour relevant to usage and observability (in our own words).**

- *Account failover* (`docs/claude-account-failover.md`): when a turn meets a classified usage
  limit, the runtime pauses all its Claude turns behind one barrier, runs a trusted local
  "selector" program that activates and verifies an account, and resumes each unfinished turn on
  its original conversation with a continuation note. The selector can answer that the account
  serves only some models; turns for the other models stay parked separately (weekly limits are
  per model), for a bounded time, after which they are released to fail visibly. A short-lived
  429 or a warning-level limit event does not trigger recovery; a "plan does not offer this
  model" failure is treated as a fact about one turn, not the account. **Relapses** (a new limit
  within ten minutes of a recovery) back off with doubling waits and, after a fixed count, end
  the relapsing turns visibly. A recovery is only considered done when the resumed agent shows
  real progress.
- *Background versus conversation calls* (`docs/model-fallbacks.md`): a conversation agent never
  moves to another provider or model behind the user's back, because it would lose its history;
  only one-shot background calls retry down a model chain, and each retry is recorded once so a
  restart cannot repeat it.
- *Restarts* (`docs/runtime-restarts.md`): a restart restores bindings and visibly marks
  interrupted turns but sends **no** generic "carry on" prompts; startup never spends quota
  asking idle conversations whether they have work. Whoever planned the restart re-prompts only
  the genuinely stranded targets, then checks for a new assistant message or tool call, because
  a launched process or a "busy" badge is not evidence of progress.
- *Attention* (`docs/attention-inbox.md`, `docs/janitor-attention.md`): one inbox sorted into
  blocking, other decisions, ready to review, and working; urgency kept separate from answer
  effort; an agent can withdraw its own stale question; an ordinary chat message does not
  silently close a pending decision; a maintenance agent raises one deterministic alert per
  failure episode (after two consecutive failures), not one per failure.
- *Banked reset redemption*: optional automation that redeems an account's usage-reset credits
  through a broker host holding web sessions. Out of scope for us, and not something to
  emulate.

**Compared with us.** clarp is ahead on remote/mobile control, on a fully worked failover for a
pool of turns, and on attention UX. We are ahead on containment (per-session containers,
network policy) and on the deterministic enforcement layer. The daemon's usage pause (pause at a
ceiling, one resume cron at the reset, supervisor told to stay quiet) is the same philosophy as
clarp's "no generic continuity prompts", reached independently.

**Ideas.**

| Idea                                                                                                                                                                                         | Where it lands                                     | Kind                                      |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- | ----------------------------------------- |
| Rotation in 00146 should pause in-flight turns behind a barrier, back off on relapse, and treat per-model weekly exhaustion as parking only that model's work                                | Plan 00146 rotation phase                          | plan (design input)                       |
| After a ccy restore, restart or supervisor nudge, verify progress by a new assistant record in the transcript, not by "the process is up"; never prompt idle sessions at startup just to ask | `ccy-sessions restore`, ccy supervisor, Plan 00256 | plan (fold into 00256 or 00135 follow-up) |
| One alert per failure episode, deterministic id, cleared by success or config change                                                                                                         | Plan 00251/00224 notifications                     | plan (00251)                              |

**Risks.** Licence (above). Scale: 4,500 commits in three months means the documents describe a
moving target; re-read before relying on any detail.

---

## Group section: the strongest takeaways, ranked

1. **Plan 00146's forwarder will silently cap 1M-context models at 200K.** claude-unlimited
   measured that any `ANTHROPIC_BASE_URL` other than `api.anthropic.com` makes Claude Code
   budget native-1M models at 200K, and the fix it ships rests on an underscore-prefixed
   internal variable (`docs/adr/0008-1m-context-through-the-gateway.md`). Plan 00146 does not
   mention context size at all. For long autonomous ccy sessions that is a major regression
   (constant compaction, thrash). It can be checked at no cost by reading
   `context_window.context_window_size` from the status payload. **Issue against Plan 00146**,
   with the decision (rely on the variable, or accept the cap) made in the plan.

2. **Usage figures without billed requests: reuse what running sessions already see.** Every
   running ccy session's status line already receives Claude Code's own `rate_limits`, and the
   daemon stores it. A host-side reader keyed by the container's `ccy-token` label could keep
   ccy's per-token usage cache fresh with no Haiku probe, labelled by source and age (Usage
   Monitor's provenance model, hotseat's "not reported is not 0%"). Add a cross-process lock and
   shared, persisted 429 backoff for the probe that remains (hotseat `internal/quota/`,
   claude-unlimited `usage_probe.py`). **Plan** (it touches ccy, the daemon's snapshot location
   and the menu). The lock and backoff part on its own is an **issue**, after confirming the
   duplicate-probe guess against the restore path.

3. **Measure what our session-running policies cost in cache rebuilds.** The usage-analysis
   study shows cold returns cost several times a warm or fresh start. Our hourly failsafe tick
   sits at the edge of the 1-hour TTL; restores, `--max-age` restarts and usage-pause resumes
   return to large histories after long gaps; Plan 00256's stand-down should compact while warm
   or start fresh with a handoff. The daemon's prompt-cache chip already records the data needed.
   **Plan**: measure first (cold ticks, restore cost), then change cadence and return policy;
   `CLAUDE_CODE_IDLE_COMPACT_MIN_TOKENS=100000` and `CLAUDE_CODE_SUBAGENT_PROMPT_CACHE_TTL=1h` are
   candidates for the ccy environment once measured.

4. **Rotation semantics for 00146 are already worked out elsewhere; adopt them as design
   input.** From claude-unlimited (MIT, liftable as a reference to port): sticky until a
   threshold, DRAINING, never rotate on a bare 429, escalating cooldown without `Retry-After`,
   per-model weekly exhaustion, per-agent pinning by `x-claude-code-agent-id` to keep caches warm.
   From clarp (ideas only): pause in-flight turns behind a barrier, relapse backoff, park per
   model, confirm recovery by real progress. **Add to Plan 00146** as a research note.

5. **Seat status (00251) gets two missing pieces.** The usage-limit stop signal: the
   transcript's last turn is an `isApiErrorMessage` record with the limit text (hotseat,
   `internal/work/scan.go`, MIT), alongside the StopFailure hook the daemon already records; and
   an evidence vocabulary (reported, observed, inferred, unknown) with hook and transcript merged
   per session (agenttrail). Once the 00146 forwarder exists, requests carrying a seat's session
   id are a free "working" signal. **Plan 00251** design input.

Repositories to keep on the watch list: all six already listed. Add
`inevolin/claude-code-usage-analysis` (new measurements will follow). Highest value to watch
closely: claude-unlimited (it is building 00146 ahead of us) and hotseat (Go, MIT, same problem
space as ccy's token menu).
