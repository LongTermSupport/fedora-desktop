# Review: Plan 00194 Task 2.0c build, `feat/bus-inject-message` (92143c1e) — security and design

Fable 5.1, 2026-10-15. Read-only. Diff `F44...origin/feat/bus-inject-message` (merge base
77eb6de5 = origin/F44 head fd5403b8's parent chain; merge-tree against F44 is clean). Read in
full: `SPEC-inject.md` (amended), `envelope.py`, `inject.py`, the `cli.py`, `hooks.py`,
`notify.py`, `relay.py` diffs, `test_envelope.py`, `test_inject.py`, `test_notify.py`,
`test_hooks.py`, `test_plugin_contract.py`, `test_relay_cli.py`, Plan 00161 `u20_check.py`
and `_acceptance-u20.inc.bash`, docs, skill, journal, PLAN.md, `meta-deploy.list`, versions.
Tests run on the tip in a scratch worktree (removed afterwards):
`python3 -I -m pytest tests/helpers/pingbus CLAUDE/Plan/00161-agent-team-bus-matrix/test_u20_check.py`
→ **1368 passed**, 3m37s. Plus my own probes against the branch's renderer (below).

## Verdict: FIX FIRST (two small code fixes; everything else merges as is)

The build is faithful to the amended spec and every spec-review finding is genuinely in the
code, with tests that drive the production path (`pingbus watch` against a real Unix socket
in `test_notify.py`, not just the renderer). The renderer resisted everything I threw at it.
Two narrow but real exactly-once / fail-fast gaps remain in the watcher's error paths (M1,
M2 below); each is a few lines plus a test. The "missing" detection should be **kept**.

## Findings

### MEDIUM

**M1. A persistent re-fetch failure never stalls, so the hooks stay quiet and nothing
announces the pending item.** `_inject_pass` (`cli.py`) catches `FAILURES` from `_choose`
and sets `retry_at`, but leaves `inj.waiting` as it was (0 after a clean run). `Stall.observe(waiting=0, …)` can never report a stall, so `.stalled` is never set, `Survey.delivery_active`
stays true (the watcher still holds every lock), the prompt hook prints no count and Stop
does not block. A human's message sits in the inbox with only a `watch.log` line. Today's
count notice needed no re-fetch, so this is a regression for that case. It is narrow (a
homeserver outage also fails the sync thread, which drops the team and frees the lock, so the
hooks revert), but it is the exact silent-hold the stall mark exists to prevent. Relay mode
does not have it: `_relay_pass` measures `spool.waiting()` independently of the failure.
*Fix:* in the `except FAILURES` branch set `inj.waiting` from the pending count (count the
candidates before re-fetching, or `max(inj.waiting, len(_candidates(seats, rt)))`), and a
watcher test: fetcher raising `matrix.MatrixError` → `.stalled` within `stall_after_s`, item
still in the inbox, `recv` prints it.

**M2. After a complete `sendall`, only a close *timeout* counts as sent; any other error on
the close path makes the delivery "not written" and it is retried with a new nonce.**
`notify.send_notice` catches `TimeoutError` around `shutdown`/`recv` but lets other
`OSError`s (ECONNRESET when the peer closes with unread bytes, EPIPE from `shutdown`)
propagate; `_send_delivery` then treats it as a write failure, consumes nothing, and retries
after 10 s with a fresh nonce — the duplicate-in-thread case E11 was amended to rule out
(spec review M1). Low probability (Claude Code reads the line before closing), but the
spec's rule is "written in full is sent" and the code only honours it for one exception
type. *Fix:* in `send_notice`, once `sendall` returned, catch `OSError` (not only
`TimeoutError`) on the close path and return `False`; test with a `FakeInbox` variant that
closes without reading.

### LOW

**L1. Stale marks on the host.** The socket path form is `/run/user/<uid>/cc-socks/<N>.sock`
with a numeric `<N>` (checked in this session; it looks pid-sized). On the host
`PINGBUS_HOME` is per user, nothing removes `inject/<key>.*` at session end, and the watcher
clears `.missing` only when a later delivery is found. A reused `<N>` hands an old
`.missing` to a new session: its Stop blocks every 600 s and every prompt carries the
"missing" line until a delivery lands. *Fix:* the watcher clears `.missing` and `.stalled`
at start (nothing is outstanding yet), and the session-end hook removes the three files.

**L2. E12 / `Spool.exclusive` docstring say the socket write under the lock is bounded by
"the 5 s timeout".** `connect`, `sendall` and the close wait each carry the 5 s timeout, so
the lock can be held ~15 s. `recv`/`wait` only block that long; no deadlock (see below).
Doc fix.

**L3. Held-back counts live in memory and are consumed before the send.** `_settle_aside`
consumes flood-limited items under the lock before `send_notice`; `inj.held` is cleared
only after a successful send. A watcher crash before the next successful delivery loses the
count, so those messages are never mentioned (they are in `history`). Acceptable; worth a
line under R3 in the spec.

**L4. (Record, not a defect.)** `_sent(True)` renders the epoch because `bool` passes the
range check; unreachable from `entry_from_line`, which parses digits only.

## What I attacked and what held

- **Forged headers / envelope close / terminal and bidi bytes.** Every header value passes
  `_check` (`[\x21-\x7e]`, or `[\x20-\x7e]` for `reply:`); a hostile `team`, `sender`,
  `handle`, `event_id`, `reply` or TIMEOUT `silent` target raises before anything is
  rendered (probed all six). The first line uses raw `sender`/`team`, but the same values are
  `_check`ed in the entry lines of the same `render` call, so a bad value never leaves the
  function. Bodies with `\r\n`, NUL, U+2028, U+202E, ESC, `ESC[201~`, DEL, `"`, `\`, a lone
  surrogate and a forged `<<end agent-bus …>>` render as one `body:` line, pure printable
  ASCII (`json.dumps(…, ensure_ascii=True)` escapes DEL too: `\u007f`), exactly two `<<`
  lines per entry. Room names, topics, display names, `formatted_body` are never rendered
  (§15 unchanged).
- **Escape then truncate.** `cap_body` cuts the *text* at a code-point boundary by binary
  search on the rendered length, then renders: it can split neither an escape nor a
  surrogate pair. Fuzzed 300 random mixes of NUL / lone surrogate / emoji / `é` / `\` / `"`
  up to 3000 code points: every rendering ≤ 2048, is a prefix of the text, and one more code
  point would exceed. 2048 NULs → 340 shown; 512 emoji → never split. The 12 KiB delivery cap
  is absolute (`fit` refuses to exceed it; 8 full NUL bodies → 4 per delivery); the largest
  entry test renders the longest handle, team, 700-char ref, 32 copy humans and a 5000-NUL
  body under 5 KiB.
- **Nonce.** Drawn after the bodies are fetched (`secrets.token_hex(4)` in `_send_delivery`);
  a body cannot start a line anyway. Not called a secret anywhere.
- **Flood budget (E19).** Keyed `(team, localpart)` from the re-fetched event's sender; a
  Matrix account has one localpart, so "several handles" means several compromised accounts.
  PEER and PING share the budget; HUMAN and TIMEOUT never spend. Starvation of a human: the
  budget removes the flooder's items from the queue (consumed, counted) after 10 entries, so
  a human's message waits at most two or three paced deliveries (≈6 s). Watcher test:
  13 notes → 10 bodies across two deliveries, `5 more pending` on the first, one `held back: 3 more` line. Budget is in-memory per watcher; a peer cannot restart the watcher.
- **Exactly-once under the shared lock.** Lock order is the same in every process:
  team `inbox.Lock` (watch/wait hold it from start; `recv` fails Busy and reads the cache,
  never waits on it while holding the spool) → `relay/lock` → outbox `flock`. `recv`'s
  `_deliver` re-fetches outside the lock and re-checks `in_inbox` inside it; the watcher's
  `_send_delivery` re-checks `in_inbox` / `due_timeouts` under the lock, writes, consumes.
  No cycle. A stalled socket holds the lock at most ~15 s (L2). `SocketGone` propagates and
  consumes nothing; a write error consumes nothing and retries; a full write consumes (M2 is
  the one hole). Crash between write and consume: R3, documented in the spec and skill.
  Tests: `recv` after a delivery prints nothing; a write error consumes nothing and is
  retried; stalled after failures with `recv` still reading the item; a late close is one
  delivery; a TIMEOUT is delivered once; a forged inbox file is never delivered.
- **Pacing.** One delivery per 2 s; identical bodies get distinct nonces (test).

## The M2 "missing" detection: KEEP

- **Evidence for the assumption.** It is not an assumption any more: Plan 00161's
  `u20_check.py` parses a socket message as a `type: user` entry with `origin.kind == "peer"`
  and, mid-turn, as a `type: attachment` / `queued_command` with origin `peer`; the
  mid-turn parser was added 2026-10-09 (ae8b320b) and the host run of that day "passed …
  M2.0 to M2.2, the last with the checker counting a notice absorbed mid-turn" (00161 journal
  26-10-09). The watcher's check is weaker than that parser: a byte substring
  (`agent-bus delivery <nonce>:`, no JSON-escapable characters) anywhere in what the
  transcript gained after the send, so it survives transcript-format drift that keeps the
  content at all.
- **Failure mode if wrong.** Every delivery is reported missing ~3 min after the session goes
  quiet; prompt line each turn, Stop blocks once per 600 s. Loud, never silent; H2/H7 settle
  it on the host before anyone relies on it.
- **Worth it?** Yes: R2 (bypass mode without `crossSessionInbound: accept`) is a genuine
  silent-loss path (the write succeeds, the item is consumed, nothing stalls) and nothing
  else can detect it: the socket answers nothing, and the setting is not knowable from the
  client. `inject.py` is 196 pure lines with 17 unit tests plus two watcher tests; the hooks
  add one record-and-read. Edge cases handled: path change after `/clear` (rescan from 0),
  truncation, the needle split across writes, an unreadable transcript, no recorded path (→
  missing). Nothing to cut.

## Plan 00161 acceptance still tests the real property

`NOTICE_RE` matches the delivery's first line with the nonce as the "number" (hex); M2.2's
same-counts-two-numbers check now proves the nonce defeats the socket's duplicate filter,
within the measured window (`expect-within-window` unchanged). M2.1 accepts a `pingbus send`
after the delivery ("delivery, then the turn that answered it"), with `pingbus recv` still
accepted; rule 0 of the session orders tells the seat an envelope needs no `recv`. The
emptied-inbox poll now relies on the watcher's consume, which is the behaviour under test.
`test_u20_check` passes.

## Spec-review findings, verified in code

H1 cut (no ledger, no attestation; `inject_frame` only adds trust; no hook text calls an
envelope untrusted). H2 cap after escaping and absolute 12 KiB (above). M1 full write counts
as sent — for the timeout case; see M2 above for the gap. M2 missing detection (above). M3
budget (above). M4 one short first line; frame explained once at SessionStart (test). M5 facts
corrected in SPEC §1 and §8. L1 nonce wording. L2 `TS_MAX_MS` (`_sent` renders `-` for
out-of-range, test). L3 sort key `(0, ts, idx)` then `(1, 0, idx)` for TIMEOUTs. L4 socket-mode
seat built `reads=True`, `_spool_lock` in every mode, `Spool.exclusive` docstring amended.
L5 R4 stated. L6 surrogate-safe cut.

## Versions, deploy, docs, journal, names, PLAN.md

- CCY 3.98.8 / container 2.63 (`claude-yolo`, `Dockerfile` label), plugin 1.2.0,
  `docs/ccy-changelog.md` entry. ✓
- `meta-deploy.list`: `git merge-tree F44 origin/feat/bus-inject-message` is clean; the
  merged entry set equals F44's head exactly (play-claude-yolo, play-agent-bus, 00201
  acceptance, md-viewer, watermark, speech-to-text), the branch only adds the Task 2.0c text
  to the two Plan 00201 entries. Nothing emptied on F44 is re-queued. ✓
- Docs: protocol §12 (`inject/`), §13 (`watch`, hooks), §15 (envelope replaces the notice);
  `agent-bus.md`, `agent-bus-lxc-members.md`, launcher comment; skill with plugin-contract
  tests pinning the renderer's own lines; no `NOTICE_TEMPLATE`/`Notifier` reference left in
  `helpers/`, `docs/`, `tests/` or `test_protocol_doc.py`. ✓
- Journal day-file: the owner's 17:10 entry is intact (context in the diff) and the branch's
  17:15, 17:50, 18:02 entries follow it in time order. (17:15 still describes the attestation
  as written then; append-only, correct.) ✓
- No install-specific names in the diff: `<home>/...` placeholders, `workstation` is the
  role literal already on F44 in that file. ✓
- PLAN.md Task 2.0c claims (fact-check diff): branch exists; spec amended; `envelope.py`,
  `inject.py`, watcher, hooks, skill, plugin 1.2.0, container 2.63, CCY 3.98.8 all true on
  the tip (the daemon's diff caught an intermediate 2.62/3.98.7 which the merge commit
  corrected); M2 checks read deliveries; both plays queued. "Built test first": tests and
  source land in the same commits (346f93e8, ff53f51e), so the order is not provable from
  history; the tests do exercise the production path. DESIGN.md decision-B amendment and
  00161 D16 amendment are dated and point at the spec. ✓

## Residual notes (no action)

- `_inject_pass` takes the spool lock and the outbox `flock` once per tick even with nothing
  pending; trivial contention.
- `held back:` lines ride on any delivery with live entries regardless of the one-a-minute
  rule; the rule only paces held-only deliveries. Matches E19.
- The test rule in `helpers/CLAUDE.md` is `python3 -m unittest`; I used pytest as the runner
  for the same unittest classes. Same tests, same result.
