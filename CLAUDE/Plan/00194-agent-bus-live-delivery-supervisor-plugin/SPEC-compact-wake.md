# Plan 00194 spec: compact before a cold wake (Task 2.0e)

**Status**: facts established. Stage 1 (the measurement, observe only) is built on branch
`feat/pingbus-compact-before-wake`. Stage 2 (the compaction itself) is specified here. It
cannot be built inside pingbus, and it waits on the owner's decisions (section 6) and an
upstream supervisor change (U1-U3).

**The owner's request** (condensed): a bus message that wakes an agent which has sat idle with a
large context hits a cold prompt cache, so the wake is expensive. If the context is over about
30 % and the agent is idle, compact first (`/compact` carrying the message, or compact then the
message, as one turn), make sure the work continues afterwards (the supervisor's `continue`, or
pingbus watching and injecting its own), as a defence at pingbus level.

## 1. Facts

Each fact is backed by a file and line, or by a measurement taken from this container on
2026-10-19.

- **F1. Delivery is the inbox socket.** `pingbus watch` writes each delivery as one connection
  carrying `{"type":"auth",…}` and then `{"type":"user","message":{"role":"user","content":<envelope>}}`
  (`helpers/pingbus/notify.py`; `tests/helpers/pingbus/test_notify.py` `WireTest`), from
  `_send_delivery` (`helpers/pingbus/cli.py`, the function after `_live`). The envelope always
  starts `agent-bus delivery <nonce>:` (SPEC-inject E7).

- **F2. A socket message cannot run a slash command.** Claude Code 2.1.303 puts its own frame
  in front of every socket message: `Another Claude session sent a message:` (or `… while you were working:`), followed by its peer caveat. These are the constants in the binary's module
  `YIe`/`p`/`u`/`o` (`claude.exe`, offset about 238.7 MB, "Version: 2.1.303"), and the owner's host
  test showed them (DESIGN.md §1.2 step 2). The text never starts the prompt, so a `/compact`
  in it is only text. pingbus has no other path into the session.

- **F3. The supervisor cannot type for pingbus.** The ccy supervisor's plugin API is 1.0
  (`.claude/ccy/claude-supervise.py:3493-3494`): plugins never type text, and only fixed
  supervisor templates reach the chat (DESIGN.md §2.0). API 1.1
  ([hooks-daemon#119](https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/119))
  has not shipped. Only the supervisor's built-in families type, and they type `/compact <fixed body>`: `claude-supervise.py:6526-6531`, bodies at `:6226-6262`.

- **F4. The supervisor's `continue`.** When its own session compacts, it types
  `🤖 [ccy-supervisor …] continue` once per compaction, whoever started it (human, supervisor
  or auto). It waits until the input box is empty and the child's output has settled
  (`claude-supervise.py:5949-5987`, the body at `:6266-6271`). It learns of the compaction from
  the daemon's PreCompact signal `<session>.compacting`, filtered to its own session
  (`:2374-2427`, `:1200-1312`), with a 600 s TTL (`:1150-1160`). So after any compaction pingbus
  could cause, the supervisor would type `continue`, and nothing lets pingbus stop it. It
  exists only under the ccy supervisor; `cc --teams` on the host has none (PLAN.md Non-Goals).

- **F5. `/compact <args>` and what follows it.** The arguments are the summary's custom
  instructions (`claude-supervise.py:6214-6216`). In three sampled transcripts a `/compact` is
  recorded as a `<command-name>/compact</command-name>` user entry, an `isCompactSummary` user
  entry, a `compact_boundary` system entry, and then the
  `<local-command-stdout>Compacted …` line, with no assistant turn after it. Compaction ends
  the turn. That is why the supervisor types `continue` (`:1150-1160`, "compact injected,
  continue lost").

- **F6. What a compaction costs in size and time.** `compactMetadata` from sampled
  transcripts: 470 653 → 13 489 tokens in 88 s; 413 838 → 10 613 in 66 s; 396 771 → 12 846 in
  49 s; 161 315 → 17 551 in 59 s. The summary context is 11k-18k tokens and takes about a
  minute to make.

- **F7. Where the context size is.** The last main-thread (`isSidechain: false`) assistant
  entry's `message.usage`: `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` is the prompt of the session's last model call. The watcher already
  has the transcript path (`inject.Marks.transcript`, recorded by the hooks). The other sources
  fall short:

  - The status line's stdin carries `context_window.used_percentage` and
    `context_window_size` (the daemon's `context_sidecar.py:127-155`), but `pingbus status-line` records none of it (`statusline.py`).
  - The daemon's sidecar is opt-in (`default_enabled = False`, `context_sidecar.py:89-90`) and
    is keyed by session id, not by the socket.
  - The supervisor deletes every sidecar but the newest once it is 30 minutes old
    (`claude-supervise.py:1187-1196`, `:3440-3461`), so it is gone just when a session has been
    idle long enough to matter.

  The transcript is the source. It does not carry the window's size, so the threshold is in
  tokens.

- **F8. The cache's TTL is in the transcript.** Each `usage.cache_creation` splits the write
  into `ephemeral_1h_input_tokens` and `ephemeral_5m_input_tokens`. The sampled sessions of this
  deployment write one-hour entries. A cache hit refreshes the TTL, so the cache is cold
  once the TTL has passed since the last call.

- **F9. Idle or mid-turn.** `message.stop_reason` is `tool_use` while a turn is calling
  tools, and `end_turn` when it ends: 3 638 against 452 in one sampled transcript, with no null.
  A session whose last call asked for a tool is in a long tool, not idle. Its next call will
  be cold whatever pingbus does, and it cannot be compacted mid-turn.

- **F10. Authority survives a compaction already.** The pingbus SessionStart hook has no
  matcher (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json:3-11`), so
  it runs again with source `compact` and E14's line comes back. An envelope sent *after*
  the compaction never passes through a summary.

## 2. The cost, honestly

Units are one uncached input token. The ratios are the published ones: a one-hour cache write
costs 2, a five-minute write 1.25, a cache read 0.1, and an output token 5 (Opus). `C` is the
context, `S` the context after compaction (about 15k, F6), `O` the summary's output tokens
(not measured; taken as 5k), and `n` the turns the agent runs after the wake.

- **A, wake as today:** `2C` (the cold rewrite on a one-hour TTL), then about `0.1C` per turn.
- **B, compact, then deliver:** the compaction call reads `C` cold. That costs `kC`, with `k`
  between 1 (the call does not write the cache) and 2 (it does), which is not measured. Add
  `5·O` for the summary, `2S` for the message turn's write, and about `0.1S` per turn after.
- **The saving of B over A:** `(2 − k)C + 0.1·n·(C − S) − 5·O − 2S`, where the overhead
  `5·O + 2S` is about 55k.

| C       | k = 2: break-even turns `n` | k = 1: saving at n = 1 |
| ------- | --------------------------- | ---------------------- |
| 60 000  | about 12                    | about 9 500            |
| 150 000 | about 4                     | about 108 500          |
| 400 000 | about 2                     | about 383 500          |

What this means:

- **Compaction never makes the wake itself cheap.** The compaction call is the same cold read
  of `C`. What B saves is in the turns after the wake. Where `k = 1`, it also saves the
  difference between a cache write and a plain read.
- **It saves most where the session would have compacted later anyway.** A context that keeps
  growing is compacted by the supervisor when it turns red, or by Claude Code automatically.
  Then A pays the cold `2C` now *and* a compaction of a larger context later, and B pays one
  compaction. This is the "one wake rather than a cold full-context turn plus a compact later"
  case, and it is a real saving of about `2C`.
- **At 60 000 tokens, a wake answered in one turn costs more with B when `k = 2`**, for
  example a `PEER` note the agent acknowledges and goes idle.
- **Not counted in tokens:** compaction loses detail of the work in progress, and it delays
  the message by the compaction's minute (F6). A human waits that much longer for an answer.

So the trigger needs a size, the owner's 30 %, and real numbers before it acts. Stage 1
collects them (D1, D5).

## 3. Can it work as asked? Not from pingbus

- **"`/compact <message>`" is rejected twice.** The socket cannot run it (F2). Even typed by
  the supervisor, the message would become the summary's custom instructions (F5). The
  sender's text would then steer what the session remembers, which is exactly what the owner
  ruled out. No message text ever goes into `/compact`.
- **"Compact, then the message" cannot be started by pingbus.** Only the supervisor can type
  `/compact` (F2, F3), and nothing lets pingbus ask it to.
- **The supervisor's `continue` gets in the way (F4).** After a compaction made for a
  delivery it types `continue` first. That is a second turn, and the agent may resume stale
  work before it reads the message.

So the compaction must be done by the supervisor, through an upstream change, and pingbus is
the sensor and the holder. A pingbus-only `/compact` would be the half-working version the
owner asked not to build.

## 4. Expectations

### Stage 1: the measurement (built, observe only)

- **C1. The reading.** From the recorded transcript, the newest 8 MiB at most, newest entry
  first, sidechain entries skipped:
  - The size is the last assistant entry with a `usage`: input + cache writes + cache reads.
    Its timestamp is the last call, and its `stop_reason` says whether the turn ended.
  - The TTL comes from the newest such entry that wrote the cache: one hour if it wrote any
    one-hour tokens, otherwise five minutes if it wrote any five-minute tokens. If no entry
    in the tail wrote the cache, the TTL is unknown.
  - A `compact_boundary` newer than that entry is the context instead: its `postTokens`, from
    its timestamp.
- **C2. The trigger.** A context of at least `coldwake.THRESHOLD_TOKENS` tokens (default 60 000,
  30 % of a 200k window, set in that one place; D1), a known TTL, a last call that ended its
  turn, and at least the TTL since that call. If anything is unknown (no transcript, no call,
  an unknown TTL, a bad count or timestamp), there is no trigger and today's delivery stands.
- **C3. One line, numbers only.** After a delivery that met the trigger was written,
  `watch.log` gets exactly one line: `pingbus: cold wake: delivery <nonce> woke <N> tokens of context <I> s after its last model call, past its <T> s prompt cache; compact-before-wake would apply (observe only)`. It carries no text, sender or team. The delivery is unchanged.
- **C4. It cannot fail a delivery.** An unreadable or malformed transcript gives no reading.
- **C5. Socket delivery only.** Relay mode (Task 2.1's plugin path) is not measured.
- **C6. Bounded cost.** At most 8 MiB is read per delivery, and deliveries are at least 2 s
  apart (E13).

### Stage 2: the compaction (specified; waits on D3-D5 and U1-U3)

- **C7. Hold and ask.** When C2 holds, the watcher holds the batch and consumes nothing. It
  writes `inject/<key>.compact-wake` (a timestamp and the nonce, no text) as its request to the
  supervisor.
- **C8. A fixed compaction.** The supervisor (U1) types `/compact <fixed body>` when the box is
  empty and the session idle. The body is the supervisor's own text: "Summarise this session
  for continuation. Agent-bus messages are waiting and arrive as the next turn; do nothing
  after compacting." No message text reaches it.
- **C9. No `continue` for it.** The daemon attributes the compaction to the origin `bus` (U2),
  and the supervisor types no `continue` for that origin (U3). The held delivery is the
  continuation.
- **C10. Deliver on the summary.** The watcher finds a `compact_boundary` in the transcript
  after the request, then writes the held delivery to the socket, with everything that arrived
  meanwhile under the E9 caps. That delivery is the turn, run on the small context. E16 confirms
  it reached the conversation, or marks it missing.
- **C11. Bounded.** If there is no `/compact` command entry within 60 s of the request, or no
  boundary within 600 s (the supervisor's own TTL, F4; the longest measured compaction is
  88 s), the watcher removes the request, delivers as today, and logs `compact-wake abandoned`. A delivery is held at most that long.
- **C12. Never twice, never a pingbus `continue`.** Items are consumed only on the one
  delivery write (E10, E11), whichever way it goes. pingbus never types or sends `continue`:
  the delivery is the turn. If an older supervisor types `continue` anyway, Claude Code queues
  the delivery behind it (decision C). The cost is one small extra turn, never a lost or
  doubled message.
- **C13. The session gets busy.** If the transcript grows with anything but the compaction
  after the request (a human typed, a cron fired), the cache is warm again. The watcher
  withdraws the request and delivers as today.
- **C14. The compaction fails** (an error, no boundary): C11.
- **C15. Several messages, one compaction.** One request per session at a time. Arrivals join
  the held batch, and the batch goes as one delivery after the compaction.
- **C16. Authority.** The envelope is rendered and sent after the compaction, unchanged (E1-E9).
  E14's SessionStart line returns with source `compact` (F10). The summary instructions never
  carry message data.
- **C17. Logs.** One line each for requested, compacted (pre and post tokens and the duration,
  from `compactMetadata`), and delivered or abandoned (the reason from a closed set). No
  text.
- **C18. Unsupervised sessions.** `cc --teams` on the host has no supervisor to compact
  (F4). It keeps stage 1's line only.

### Upstream (hooks daemon, filed through `hooks-daemon issue-report`; D4)

- **U1.** A built-in supervisor family that reads `inject/<key>.compact-wake` (or the
  equivalent the daemon prefers) and types the fixed `/compact` of C8 when the session is
  idle.
- **U2.** PreCompact attributes that compaction to the origin `bus`.
- **U3.** No `continue` for the origin `bus`.

## 5. Tests

- `tests/helpers/pingbus/test_coldwake.py` covers C1-C3 on transcript lines shaped as F5-F9
  show:
  - the size, the sidechain, both TTLs and an unknown one;
  - the TTL taken from an older write, and a turn still running;
  - a compaction before and after the last call;
  - the tail cut, lines that are not entries, and bad counts and timestamps;
  - the inclusive bounds and every non-trigger;
  - the log line's exact template, and its refusal of a bad nonce.
- `tests/helpers/pingbus/test_notify.py` `WatchDeliveryTest` runs a real watcher against the
  fake homeserver and socket:
  - a cold 180k context gives exactly one `cold wake` line with the delivery's nonce, no body
    text, and the delivery unchanged;
  - a warm context and a small context give none.

## 6. Decisions for the owner

- **D1. The threshold's unit and default.** Tokens, because the cost is in tokens and the
  transcript has no window size. The default is 60 000, the owner's 30 % of 200k. Section 2
  recommends 150 000 for stage 2. *Default: 60 000 for stage 1, revisited with its data.*
- **D2. An unknown TTL.** No trigger, so a wrong guess never compacts a warm cache.
  *Default: as built.*
- **D3. A human's message and the compaction's minute.** Stage 2 would hold every kind, a
  `HUMAN` request included, for up to 600 s (C11), usually about one minute (F6). *Default:
  every kind; alternative: `HUMAN` is never held.*
- **D4. Ask upstream for U1-U3.** It is a public tracker, so the coordinator or the owner files
  it with `hooks-daemon issue-report`. *Default: file it once D1 and D3 are settled.*
- **D5. Stage 2 only after stage 1's data.** A week of `cold wake` lines shows how often it
  applies and on what sizes. *Default: yes.*
- **D6. A cheaper defence, not adopted.** While a session is large and cold, a `PEER`, `PING`
  or `TIMEOUT` would wait, without waking it, until the session wakes for another reason or a
  `HUMAN` message comes. That costs no cold read at all, and pingbus can build it alone today.
  It reverses decision C ("fire immediately") for those kinds. *Default: not adopted; offered.*
- **D7. Where the stage 1 line goes.** `watch.log` only: no hook text and no status line.
  *Default: as built.*

## 7. Host checks

- **HC1.** Stage 1: leave a `ccy --teams` session with more than 60k context idle for more than
  an hour, then post to it from Element. `watch.log` has one `cold wake:` line with its numbers
  and no text, and the envelope arrives as before.
- **HC2.** The same within the hour: no line.
- **HC3.** Before stage 2, measure `k`: the cost of one `/compact` on a cold large session (the
  account's usage before and after). Section 2's table turns on it.
- **HC4.** Stage 2, once U1-U3 ship: a cold large session gets one compaction and then the
  delivery as one turn on the summary, with no `continue`. A busy session gets no compaction.
  A compaction that never starts is abandoned within 60 s, and the delivery goes as before.
