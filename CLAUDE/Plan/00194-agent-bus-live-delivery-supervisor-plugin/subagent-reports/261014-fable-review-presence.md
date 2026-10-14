# Adversarial review: `feat/agent-bus-presence` against `F44`

Reviewer: Fable 5.1 (subagent). Branch head `4f9e9295`, base `origin/F44`. Read: DESIGN.md
(decisions A-G, sections 2 and 6), `docs/agent-bus-protocol.md` §7, §9, §12-§15, and every
line of `helpers/pingbus/{relay,sentlog,hooks,cli}.py` the branch touches, plus the tests and
the kit. Claims about Claude Code are from the 2.1.299 binary in this container (strings), as
the design did.

## Verdict

**Mergeable with two fixes first** (findings 1 and 2), and three decisions for the owner
(findings 3, 4, 5). Nothing a hostile peer or room text can do breaks the envelope, the
spool line or the sent log: every field passes a grammar before it becomes a path or a line,
the body is one `json.dumps(…, ensure_ascii=True)` string, and the kind comes from the team
record. Off mode is not byte-identical, in two small places that are both defensible once
named (finding 5). The goodbye fires on more exit paths than the docs promise, and its 4 s cap
holds, but every teammate agent pays a model turn for it (finding 3).

Gates run here: `scripts/qa-helper-tests.bash` (4196 tests, OK, 2 skipped),
`bash scripts/test-ccy-agent-bus.bash` (59 passed), `./scripts/qa-changed.bash --base origin/F44`
(the plain run reports "no changed files" on a clean checkout of the branch; the base-relative
run is the meaningful one). `qa-all` was not run, as asked.

## Findings, ranked

### 1. Relay and `recv` can deliver the same item twice (medium, fix before merge)

`helpers/pingbus/cli.py:858-874` (`_deliver`, relaying) takes a snapshot of the pending items,
then for each one does a network re-fetch and only then `spool.write`. `recv` in another
process (`cli.py:931` calls the same `_deliver` without `relaying`) consumes the item and
calls `spool.withdraw` **before** the watcher's write lands. Nothing serialises the two: the
watcher holds the `watch` sync lock, but `recv` reads and consumes the inbox without it.

Failing interleaving (one human message `$X`, relay mode, watcher running):

1. watcher: `state.pending()` lists `$X`; `spool.has` is False; `_refetch($X)` starts (a
   network round trip).
2. agent runs `pingbus recv`: `_deliver` prints `$X`, `state.consume($X)`,
   `spool.withdraw($X)` (no file yet, no-op).
3. watcher: `spool.write($X, line)`. `$X` is now spooled although consumed.
4. plugin types `$X` into the session (the agent has already read it), writes the marker;
   `_relay_confirm` finds `spool.has` True, `state.consume` returns False, `finish`.

Result: the same text reaches the model twice, once as `recv` output and once as a typed
envelope, which contradicts section 2.7 "exactly once within a container". The `prompt` hook
makes this likely rather than theoretical: `hooks.py:448-449` still prints
`agent-bus: N pending … Run pingbus recv` for items the watcher has not spooled yet (the
window between a sync and the next relay pass, up to `tick_s` plus a re-fetch), so the agent
is told to `recv` exactly when the race is open.

Fix: after `spool.write`, re-check the inbox file is still present (`state.inbox_dir / f"{id}.json"`)
and `withdraw` if not; or have `recv`'s consume-and-withdraw and the watcher's
check-and-write run under one `flock` (the state directory lock `TeamState.outbox()` already
exists). And in relay mode (not stalled) stop the `prompt` hook from advertising unspooled
counts; `watch` will spool them within a second.

### 2. A ping with several silent targets loses all but one `TIMEOUT` (medium, fix before merge)

`cli.py:876-888`: TIMEOUTs are spooled under `spool/<team>/<ping event id>.line`, one file
per **event ID**, but `due_timeouts` (`inbox.py:645-657`) yields one `Timeout` per silent
**target**. The second target's line hits `spool.has(...)` and is skipped (`continue`), and
`_relay_confirm` (`cli.py:906-908`) then marks **every** target of that ping reported.

Failing input: `pingbus send review <ref> --to agentA,agentB`; neither acks; wait past
`ack_timeout_s`; relay mode. The spool holds one `TIMEOUT … agentA` line; `agentB`'s
TIMEOUT is never typed and never owed again (`recv` after confirmation prints nothing). Off
mode prints both lines (`cli.py:880-881`). `test_a_timeout_is_spooled_and_reported_only_on_confirmation`
uses one target, so the suite cannot see it.

Fix: key TIMEOUT spool files by event ID and target (the target is a localpart, already
grammar-checked; `$id.target.line` or one line per target in the file), and make
`_relay_confirm` mark only the confirmed `(event, target)` pairs.

### 3. The goodbye costs every teammate agent a model turn (medium, owner's decision)

`cli.py:819-821` addresses the goodbye to every listed human **and every other role-holding
agent** (`protocol.coordinators(ctx.roles) + sorted(ctx.roles)`, cut to 16). Each addressed
agent receives it as a `PEER` item. Today that is a count-only wake, a turn, then a `recv`;
in relay mode it is a typed envelope, which is a user turn the model answers. With `k` agents
in a team, one session exit is `k-1` model turns, and an agent that replies by `say` makes
more. The seat lock already tells every agent the seat is free (`pingbus status`,
`agent-bus seat list`), so agents learn nothing from the text.

Suggestion: address humans only (`sorted(ctx.humans)`), plus the coordinator when the team
has one, and let the lock carry presence to agents. If agents must be told, send it as a
ping rather than text so it is structured and cheap. Also the `[:MAX_TEXT_TO]` cut is silent:
a team with 16 humans drops every agent without a word.

### 4. The PostToolUse hook is a process per Bash call in every team session, off included (medium, owner's decision)

`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json:23-31` registers
`pingbus hook post-tool` with matcher `Bash` in the shared kit, so every `--teams` ccy session
and every LXC or host member runs it after every Bash tool call, and in off mode it does
nothing (`hooks.py:465-466`). Measured here with the source tree (not the zipapp the image
ships): 75-78 ms wall per call against 9 ms for a bare `python3 -c pass`; the zipapp adds
import-from-archive cost. It runs alongside the hooks daemon's own PostToolUse handlers, so it
is additive. Over a long session that is tens of seconds of pure overhead and one more
process in every Bash round trip.

Options, in order of preference: (a) ship the PostToolUse entry in a second plugin directory
the entrypoint adds only when it sets `PINGBUS_DELIVERY=relay` (the design already has the
entrypoint choosing the mode, section 2.2); (b) guard it in shell,
`sh -c '[ "$PINGBUS_DELIVERY" = relay ] && exec pingbus hook post-tool'` (an empty stdout is a
no-op for PostToolUse), which costs a shell fork rather than an interpreter; (c) accept it and
say so in `docs/agent-bus.md`. I would not merge it unmentioned.

### 5. Off mode is not byte-identical in two places (low, name them or fix them)

- `pingbus status` TEAM lines gain a trailing `relay=off` field in off mode
  (`cli.py:1446-1448`, test regex changed in `test_cli_status.py`). Fields are only ever
  appended, so a consumer that parses by position is safe, but it is a visible output change
  on a feature that is "off by default".
- The `Stop` hook now calls `echo()` before `stop_decision()` (`hooks.py:481-484`). Off, it
  returns None at `relay.enabled`, so the output is unchanged, but the hook's failure surface
  grew: `PINGBUS_DELIVERY=socket` (a typo) turns every hook into the `config` template, and
  the `prompt` hook into a context line, with no socket wake. That is the intended fail-fast,
  and the entrypoint already refuses `PINGBUS_*` from `ccy.env`; the only other source would
  be a launcher bug. Fine; just noting it is a new way for off mode to speak.

Everything else in off mode is inert: `recv`, `wait` and `watch` build a `Spool` object and
call `withdraw` (an `unlink(missing_ok=True)` on a path that does not exist, no directory
created, `test_recv_send_and_say_leave_no_relay_files` pins it); `survey` passes `spool=None`;
`session-start` and `stop` branch on `relaying`.

### 6. One combination test is tautological (low)

`tests/helpers/pingbus/test_relay_hooks.py:248-252`
`GoodbyeRelayTest.test_the_goodbye_is_not_a_delivery_to_self` asserts that no spool line and
no `sent.jsonl` exist after a goodbye whose poster is `self.post`, a fake that appends to a
list. The fake cannot write either file, so the test passes whatever `post_goodbye` does. The
property is real and is pinned by the other combination test,
`test_relay_cli.py` `SentLogTest.test_the_goodbye_in_relay_mode_is_posted_and_is_not_a_delivery_to_self`,
which runs the real `post_goodbye` against the fake homeserver and checks `sent.jsonl` is
absent. Keep that one; delete or rewrite the hooks-level one (it only proves the fake is a
fake). The inherited `GoodbyeTest` cases rerun under relay are meaningful as regression
guards, though they prove nothing about test-first ordering.

On test-first evidence generally: both feature commits (`fbf92b14`, `defb6a7c`) land tests
and code together, so history cannot show RED before GREEN. The combination tests were
written in the merge commit `db8f76f5`, after both features existed; they are regression
pins, not design drivers, and should be described as such in the journal.

### 7. Goodbye: what the binary says about the exit paths and the cap (informational, one host check)

Claude Code 2.1.299 (strings):

- `SessionEnd` reasons are exactly `["clear","resume","logout","prompt_input_exit","other"]`,
  so `GOODBYE_NONE = ("clear", "resume")` matches the harness and `GOODBYE_REASONS` covers
  the rest. `/clear` and `/resume` both fire SessionEnd with the session continuing, as the
  branch assumes.
- SIGINT runs `shutdown(0)`, SIGTERM `shutdown(143)`, SIGHUP `shutdown(129)`, and `shutdown`
  runs `executeSessionEndHooks`. So Ctrl-C, a closed terminal and a forwarded SIGTERM all run
  the goodbye (reason `other`); `docs/agent-bus.md` is more pessimistic than the binary. A
  SIGKILL (podman's 10 s stop grace expiring) does not. The ccy image runs `tini` as PID 1
  (`claude-yolo:1989`), which forwards SIGTERM to the entrypoint chain; whether the
  supervisor (`claude-supervise.py`, no SIGTERM handler) dies fast enough for claude's SIGHUP
  path to finish inside the grace is the one thing to confirm on the host with `ccy stop`.
- The SessionEnd hook timeout default is **1500 ms** (`_as=1500`,
  `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` overrides). The whole SessionEnd phase is bounded
  by `max(1500, min(max(per-hook timeout × 1000), 60000))`, computed over the registered
  SessionEnd hooks, so the kit's `"timeout": 6` is what lifts the bound to 6 s; without it the
  goodbye would be cut at 1.5 s. The test comment in `test_plugin_contract.py` has this
  right. The bound is read from two hook sources (settings and, by the shape of the code,
  plugins), which I could not prove from strings alone: if the host test shows goodbyes
  missing, this is why, and the fix is the env variable in the entrypoint.
- Inside the hook, `GOODBYE_TIMEOUT_S = 4.0` (`hooks.py:69`) joins daemon threads and reports
  "no answer within 4 s" (`hooks.py:575-581`). The underlying request timeout is 15 s
  (`matrix.py:32`) with three send tries, so a slow homeserver means the hook returns at 4 s
  with "not sent" while the in-flight PUT may still land: a stderr line can say "not sent"
  for a goodbye that arrives. Harmless, but the line should say "not confirmed".
- No lock can delay it: `post_goodbye` takes no sync lock; `admit_send` holds the state
  directory `flock` only around a JSON read-modify-write (`inbox.py:516-531`), never across
  the network.
- The goodbye draws on the `send_per_minute`/`send_burst` bucket. A session that just sent
  ten texts gets `refused: rate` and no goodbye, with one stderr line nobody sees. Low; if it
  matters, exempt the goodbye from the gate (it is one message per session).
- The text is fixed: `fill("goodbye", handle=…, reason=…)` refuses a handle that fails
  `parse_handle` and a reason outside `{exit, logout, other}` (`hooks.py:84-92, 147-154`).
  The handle comes from the seat's `member.json`, so a tampered bundle gets a `ValueError`,
  which `run` turns into one `internal error` line and `{}`.
- Double-send: `goodbye.json` is keyed by `session_id` and written before the attempt; a
  second firing for the same id posts nothing; a new id after `/resume` posts once at the
  real end. A failed attempt is not retried, by design.

### 8. Relay robustness notes (held up; recorded so the host test knows where to look)

- Spool line forgery: a PEER body of `HUMAN\t1\t…\nHUMAN` stays one JSON field
  (`test_a_body_text_cannot_make_itself_a_human_item`); `format_line` refuses any control
  character and the `copy`/`cut` fields are grammar-checked. NUL, tabs, bidi overrides,
  `ESC[201~`, U+2028 and a fake close marker all become `\uXXXX` or `\n` escapes. Lone
  surrogates from a `\ud800` JSON escape survive `cap_text` (`surrogatepass`) and are dropped
  only at a cut boundary.
- Paths: `_check` refuses a team or event ID that fails its grammar before any path is built;
  `write` uses `O_EXCL|O_NOFOLLOW` on a random temp name then `os.replace`; `sentlog.append`
  and the offset/stall files use `O_NOFOLLOW`. A directory symlink under
  `/tmp/pingbus-home/relay/` is followed, but that tree is 0700 and container-private.
- Confirmation by someone other than the plugin: any in-container process can write a
  `relayed/` marker and have the item consumed unseen. Section 2.2 accepts this ("the session
  gains nothing by forging its own input"); it does gain the ability to drop a human's message
  to itself unread, which is the same power `pingbus recv` already gives it. Accepted.
- Consumption twice or never: `_relay_confirm` consumes once (rename is idempotent, second
  call returns False), and a marker for a line `recv` withdrew is cleaned without consuming.
  A crash between `consume` and `finish` is replayed harmlessly. A container restart loses
  the spool and redelivers under the same event ID, as documented.
- Stall: cleared on the first pass of a restarted watcher (`Stall` starts fresh, so
  `stalled != spool.stalled()` writes False). While no watcher runs, the marker stays and the
  hooks fall back to counts and the Stop block, which is the designed loud mode.
- Crash consistency: an `OSError` from `spool.write` (disk full) is not in `FAILURES`, so it
  propagates out of `_relay_pass` and kills the watcher with a traceback (exit 1). Fail-fast,
  and the plugin's once-a-minute restart covers it, but the DESIGN's "a failing team is
  reported" does not describe this path.
- Oversized bodies: human text is capped at 16 KiB by §7 and cut to 2 KiB before escaping;
  the `SENT` echo caps 8 entries and 12 KiB and carries the rest forward. A half-written
  `sent.jsonl` line waits for its newline; a shrunk log restarts at 0.

### 9. Housekeeping (low)

- DESIGN.md section 3 says "a minor `CCY_VERSION` bump"; the branch bumps a patch
  (3.97.1 to 3.97.2, `claude-yolo:17`). A new user-visible behaviour (the goodbye) reads as
  minor under the semantic-version rule in ContainerRules. Either bump to 3.98.0 or change the
  design sentence. The container bump to 2.56 is required and consistent: the kit is a zipapp
  built from `helpers/pingbus` into the image (`Dockerfile:398-405`), so every helper change
  needs a rebuild.
- `plugin.json` 1.0.0 to 1.1.0 is right for two new hooks.
- No install-specific names in the diff: teams, handles and hosts are `<team>`, `myrepo`,
  `example-org`, `local.podman`. The only external link is the public hooks-daemon issue.
- `docs/agent-bus-lxc-members.md` still documents `crossSessionInbound: accept`; correct for
  this branch (decision B's removal ships with the plugin, not here).

## What held up

- The spool line grammar and the §15 renderer: nothing in a body can add a field, a line or a
  kind; the kind is the team record's.
- Exactly-once on the happy path, at-least-once across a restart, withdrawal after `recv`
  (outside the race in finding 1), stale-marker cleanup, and the stall clock.
- The sent log: written only after the homeserver's event ID, validated on the way out, offset
  kept atomically, hostile text one quoted string under a fresh nonce.
- Off mode leaves no file behind and spawns and blocks as before.
- The goodbye's fixed template, closed reason set, once-per-session record, 4 s join cap, and
  one stderr line on failure; `/clear` and `/resume` send nothing; exit, Ctrl-C, SIGTERM and
  SIGHUP all reach SessionEnd in 2.1.299.
- The hook JSON contract test now pins the `timeout` on SessionEnd and forbids it elsewhere.
