# Review: the agent-bus count notice versus injecting the message itself

Read-only design review (Opus, 2026-10-15). No file was edited. The question came from the
owner. Today a bus message wakes the session with "agent-bus: 1 pending (…), notice N. Run
`pingbus recv`.", which costs an extra turn and hides the words from the human watching the
terminal. Why not inject the message?

## 1. How it works today, end to end

1. `pingbus watch` is started detached by the plugin's SessionStart hook
   (`helpers/pingbus/hooks.py`). It holds every team's sync lock, long-polls each team in
   its own thread, validates each event and writes valid items to the inbox
   (`.claude/ccy/pingbus/seats/<team>/<seat>/…`, shared by every container of the checkout).
2. `notify.Notifier.observe` (`helpers/pingbus/notify.py:117`) fires whenever a pending
   event ID appears that was not pending at its last look. It sends `notice_text(...)`, a
   fixed template of counts plus a rising notice number `S`, at most once per 2 s. The
   wiring is `cli.py:1413` (`cmd_watch`).
3. `notify.send_notice` writes one auth line and one user line to Claude Code's
   per-session inbox socket (`CLAUDE_CODE_MESSAGING_SOCKET`). That needs
   `crossSessionInbound: accept`
   (`files/opt/claude-yolo/optional/agent-bus/settings.json:2`). Claude Code wraps the
   notice as "Another Claude session sent a message:" and adds its permission-laundering
   caveat (U01, Plan 00161 journal 26-10-06 16:12).
4. The agent runs `pingbus recv`. `_deliver` (`cli.py:980`) re-fetches each item from the
   homeserver (`_refetch`, `cli.py:954`), re-validates it, prints the §15
   `PING`/`HUMAN`/`PEER` line and a `REPLY` line, and consumes it. The UserPromptSubmit
   and Stop hooks add count-only reminders. Spec: `docs/agent-bus-protocol.md` §15.

## 2. Why the notice carries counts and not content: the record

- **The origin.** The wake research
  (`CLAUDE/Plan/00161-agent-team-bus-matrix/subagent-reports/261006-research-wake-opus-5-5.md`,
  lines 4, 131-132 and 183) worked to issue #59, which "forbids free text reaching an
  agent". Its rule for every wake path was "fixed templates plus counts and validated
  one-line pings only; never event bodies".
- **The design.** Plan 00161 `DESIGN.md` §6 (lines 1035-1041 and 1058-1064) and decision
  D16 adopted that rule: "Only counts, never content". The socket's "Another Claude session"
  frame was tolerable because "the text is a fixed template naming the bus, and the agent
  then reads validated items through `pingbus recv`". D20 adds that "`recv` re-fetches
  before printing": the inbox is a cache, never the authority.
- **The spec** freezes it: `docs/agent-bus-protocol.md` §15 says "The socket
  notification and every hook output are fixed templates carrying counts".

## 3. Do those reasons still hold?

| Reason                                                                     | Holds today? | Why                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| -------------------------------------------------------------------------- | ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Issue #59: no free text reaches an agent                                   | **No**       | D13 (human text) and D63 (agent text, 2026-10-12) reversed it. `recv` already puts the same human and peer text into the model's context, one tool call later. The notice protects nothing that `recv` does not hand over.                                                                                                                                                                                                                               |
| Only re-fetched, re-validated items reach the model (the inbox is a cache) | **Yes**      | A sibling seat's container can write the shared inbox and plant a "HUMAN" item (Plan 00194 DESIGN §2.2). Anything injected must be built from the watcher's own re-fetch, never from the inbox. That is already possible: `_deliver(..., relaying=True)` does it.                                                                                                                                                                                        |
| The socket frames every message as "Another Claude session"                | **Yes**      | It is the strongest reason. For a PEER line the frame is roughly right. For a HUMAN line it is wrong, and Claude Code's caveat tells the model to discount it, so a real request from a human can be under-weighted. The owner's host test on 2026-10-14 saw this (Plan 00194 DESIGN §1.2, "Provenance is wrong"). Today `recv`'s output arrives as a tool result, which carries no such frame. Content in the socket would make the frame load-bearing. |
| Read/ack state: delivered once                                             | Solvable     | The relay ledger already does it (spool, confirm, consume; `recv` withdraws from the spool; `relay/lock`).                                                                                                                                                                                                                                                                                                                                               |
| Size                                                                       | Solvable     | The relay already caps a body at 2 KiB before escaping (`relay.BODY_MAX_BYTES`), and the `cut` field points to `history` for the rest.                                                                                                                                                                                                                                                                                                                   |
| The dedupe window and `S`                                                  | Moot         | Distinct bodies are not deduped. A per-relay nonce keeps two identical bodies distinct. The rate bucket (30, refilling at 0.5/s) still needs batching.                                                                                                                                                                                                                                                                                                   |
| Several teams                                                              | Moot         | Every §15 line carries its team. Plan 00201 S-C7 leaves the wake unchanged.                                                                                                                                                                                                                                                                                                                                                                              |
| Matrix sync locking                                                        | Moot         | The watcher already holds the locks and re-fetches with no lock (`GET /event`).                                                                                                                                                                                                                                                                                                                                                                          |

**Note:** the owner has already answered this question. Plan 00194
(`CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/`, In Progress) exists to
inject the message itself.

- **Decision B:** the ccy supervisor types each re-fetched §15 item into the session in a
  nonce-marked envelope. Every header is set by pingbus, the body is a quoted
  `ensure_ascii` JSON string, and the authority line is fixed per kind.
- **Decision C:** it fires at once, busy or idle.
- **Decision E:** the agent's own sends are echoed as a `systemMessage`.
- **The socket is dropped,** and so is `crossSessionInbound`.
- **Section 4 rejects "Peers through the inbox socket, text instead of counts".** It would
  be a second path, it needs `crossSessionInbound: accept` (attack surface), and it frames a
  human's message wrongly.

The pingbus half shipped in image 2.56 (Task 2.0): the relay mode of `watch`, `relay.py`,
`sentlog.py`, the relay hooks and `status relay=`. It stays inert until a launch sets
`PINGBUS_DELIVERY=relay`. The only block is upstream: hooks-daemon issue #119, which is
their Plan 00526, needs supervisor plugin API 1.1 so that a plugin can type text. #119 is
open and waits on three owner rulings: build it, the trust boundary, and busy delivery
(Plan 00194 journal 26-10-15 09:28). Plan 00194 decisions B and C already answer all three
in the owner's words.

## 4. Recommendation

### 4.1 Primary: finish Plan 00194; do not put content into the socket

- The design already has what the lead's brief asks for:
  - recv's own framing: the kind, sender handle and team, plus the reply command
    (`pingbus say --to …`, the same command recv's `REPLY` line prints);
  - the body marked as data: an escaped JSON string between nonce markers that it cannot
    close;
  - a size bound, with a pointer to the rest;
  - delivered once, through the spool ledger, so `recv` does not repeat it;
  - the words visible to the human in the main thread, and no extra turn.
- It also fixes the HUMAN mis-framing that a socket-with-content design would make worse.
- **The action is the owner's, not an agent's:** confirm decisions B and C in the room for
  hooks-daemon Plan 00526, so that API 1.1 gets built. Then do Task 2.1: the plugin
  `files/var/local/claude-yolo/supervisor-plugins/ccy_agent_bus.py`, the entrypoint wiring
  and refusal, the removal of `crossSessionInbound`, and the skill.
- 00194 DESIGN §3 already lists the tests: hostile bodies stay inside one JSON string,
  nonce markers, truncation never splits an escape, a forged inbox file is never delivered,
  the kind comes from the team record, consume only after confirmation, and no socket
  write.

### 4.2 Interim, only if the owner wants relief before 00526 ships

The interim is a socket sink for the relay spool. It reverses decision B for an interim, so
it is the owner's call.

**What changes where:**

- **`helpers/pingbus/cli.py` (`cmd_watch`).** When the session has a socket and relay mode
  is off:
  - run `_deliver(..., relaying=True)` into an in-process spool;
  - render the batch with the shared envelope renderer and send it with `send_notice`;
  - consume the items only after `send_notice` returns cleanly;
  - on `SocketGone`, exit as today, so the items stay pending for `recv`.
  - `Notifier` keeps the 2 s pacing and the "owed" logic, and batches up to 8 items or
    12 KiB, with "N more pending: run `pingbus recv`".
- **`helpers/pingbus/relay.py`, or a new `envelope.py`.** One renderer for 00194's §2.4
  envelope, shared later with the supervisor plugin's contract test. A provenance line
  states that Claude Code labels the message as another session, and that the agent-bus
  watcher delivered it. The authority line is fixed per kind; only `kind: HUMAN` is a
  request.
- **`helpers/pingbus/notify.py`.** `NOTICE_TEMPLATE` stays for the case where only drops or
  failures are pending.
- **`helpers/pingbus/hooks.py`.** The relay-aware counts already exist; reuse them so that
  UserPromptSubmit and Stop do not prompt a `recv` for items already delivered.
- **Docs:**
  - `docs/agent-bus-protocol.md` §15 gains a second exception to "templates and counts
    only", and §13 the `watch` behaviour;
  - `docs/agent-bus.md`;
  - the skill (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md`):
    a socket-delivered item is the same as a `recv` line, and a repeated `id` is a repeat;
  - Plan 00161 `DESIGN.md` §6 and D16, and Plan 00194 decision B, each with a dated
    decision row.
- **Version bumps:** the container and `CCY_VERSION` bumps and `docs/ccy-changelog.md`,
  because pingbus ships in the image.

**Tests**, extending the existing ones in `tests/helpers/pingbus/`:

- `test_notify.py`: the envelope renders hostile bodies (`"`, a newline, ESC, `ESC[201~`, a
  fake close marker, bidi, U+2028) inside one JSON string; the cut never splits an escape;
  the caps and the "more pending" line.
- `test_relay_cli.py` or `test_cli.py`, using `fake_client_api.py` and a fake socket:
  - a forged inbox file is never sent;
  - the kind comes from the team record;
  - an item is consumed only after a clean send;
  - a send failure leaves it pending;
  - `recv` after a delivery prints nothing again;
  - two identical bodies in a row are both delivered (the nonce);
  - several teams appear in one batch.
- `test_relay_hooks.py`: no `recv` prompt for delivered items.
- `test_protocol_doc.py` holds §15 to the renderer.
- A host check through the plan's acceptance: the owner sees the words in the thread, the
  agent does not run `recv`, and a HUMAN request is acted on despite the socket caveat.

**Risks:**

- **HUMAN under-weighting.** A human's message is again wrapped in Claude Code's
  cross-session caveat, so the model may discount or refuse a legitimate request. That is
  the reason 00194 rejected this design. The provenance line reduces the risk but does not
  remove it.
- **Two delivery paths.** The interim keeps `crossSessionInbound: accept`, and once 00194
  lands there are two paths to retire, against the owner's "single code path".
- **Its own lifetime.** It is throwaway work that lives until 00526 ships.

My judgement: do 4.1. Do 4.2 only if 00526 stays unscheduled and the owner accepts the
HUMAN-framing risk in writing.
