# Fable review: feat/pingbus-compact-before-wake at 6d3d61ab

Reviewed against local F44 (d57dab04). Developer's report:
[261019-compact-before-wake-opus.md](261019-compact-before-wake-opus.md); spec:
[../SPEC-compact-wake.md](../SPEC-compact-wake.md). Branch base is origin/F44 0ce52a7e, which
is three merges behind local F44.

## Verdict: FIX-FIRST

The code is sound and does what the spec says; the facts are right in substance. What blocks
the merge is mechanical: the version numbers and the deploy queue collide with what F44 has
since taken, and two spec statements are overstated in a way that would mislead the upstream
filing. Exact list at the end.

## 1. The four "can't" findings

| Finding                                              | Verified                                                                                                                                                                                                                                            | Holds?                                                                                                                                                                                                                                                                                      |
| ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (1) Socket frame neutralises `/compact`              | `helpers/pingbus/hooks.py:151` and DESIGN.md §1.2 (the owner's host test) both record the "Another Claude session sent a message:" frame. The binary constants are not in the repo; taken from the host test.                                       | Yes, and for a stronger reason than the spec gives: a `type: user` socket message enters the conversation as a user message, and slash commands are parsed only from the TUI input box. The frame is confirmation, not the mechanism. The conclusion is right either way.                   |
| (2) Plugin API 1.0 cannot type                       | `.claude/ccy/claude-supervise.py:3508` `_PLUGIN_API_VERSION = (_PLUGIN_API_MAJOR, 0)`; `:3490-3496` a plugin gets `on_start`/`on_idle` only; typing goes through `os.write(master_fd, …)` at `:9141` from the pty the supervisor forked at `:9297`. | Yes.                                                                                                                                                                                                                                                                                        |
| (3) `/compact <message>` steers the summary          | `:6210-6216` and `:6745`: the supervisor's own `/compact` payloads put their body after the command as custom instructions.                                                                                                                         | Yes.                                                                                                                                                                                                                                                                                        |
| (4) The supervisor's `continue` after any compaction | `:5963-5987`: `reading.compacting` from the daemon's `<session>.compacting` signal, filtered to own sessions (`:2397-2412`), then one `continue` once the TUI is idle and the stream has settled.                                                   | Yes for pingbus, **overstated for the supervisor**: Plan 00479's usage pause already types a `/compact` with a do-nothing body and suppresses the `continue` (`:6711-6718`, log text "compaction wait dropped, no continue"; `machine.pause_compacted_for`). A no-`continue` origin exists. |

**A simpler path the spec does not name.** ccy runs the session inside tmux on a private
socket (`files/var/local/claude-yolo/lib/tmux-session.bash:514`, `tmux -L "$CCY_TMUX_SOCKET"`).
From inside the container a watcher could `tmux send-keys` a `/compact` into the pane; the
supervisor would forward it from its stdin as if a human typed it. So F2's "pingbus has no
other path into the session" is false as written. The path is still the wrong one: it has no
idle gate (the supervisor's `idle` is exactly what stops a compaction from landing on a human
mid-sentence), the fixed-body rule would be pingbus's to enforce alone, and F4's `continue`
still fires. The spec must name it and reject it, or the next reader rediscovers it and
builds the half-working version the owner ruled out. No simpler *acceptable* path exists;
the supervisor-actuated design stands.

## 2. coldwake.py

Read `helpers/pingbus/coldwake.py` in full and probed it directly.

- **Tail only, O(max_bytes).** `_tail` seeks to `size - 8 MiB` and reads from there;
  `lines[1:]` drops the piece the cut lands in. On a real 142 MB transcript (longest line
  234 KB, 16 111 usage lines) `read()` took **8.6 ms** and returned a sane context (one-hour
  TTL, `end_turn`). The loop breaks at the first usage entry that wrote the cache, which is
  normally the newest, so JSON is parsed for one or two lines.
- **Robustness, probed:** a last line larger than the window gives `None`; a partial last
  line is skipped and the valid entry before it is read; an invalid-UTF-8 line is skipped
  (`UnicodeDecodeError` is a `ValueError`); a non-assistant last entry with no assistant in the
  tail gives `None`; a missing `stop_reason` gives `turn_ended=False` (no trigger); a directory
  or absent path gives `None` (`OSError`). Negative or non-int counts and a bad timestamp give
  `None`. I found no exception type that escapes `read()`: `fromisoformat` raises only
  `ValueError`, stamps end in `Z` so `.timestamp()` is arithmetic, every `.get` is on a
  checked dict.
- **Fails closed:** every unknown is `None`, `assess` returns `None` on `None`, and the
  log line is the only effect. C4 holds.
- **No text logged:** `log_line` is a fixed template of four numbers and the nonce, and the
  nonce is `secrets.token_hex(4)` so the `ValueError` guard cannot fire in production. The
  watcher test asserts the human's words are absent from stderr.
- **TTL source is right.** `usage.cache_creation.{ephemeral_1h,ephemeral_5m}_input_tokens`
  is where Claude Code records the split; the real transcript has 16 105 one-hour writes and
  no five-minute ones, matching F8. A cache hit refreshing the TTL is the documented
  behaviour, so "cold once TTL has passed since the last call" is correct.
- **One semantic gap, not a stage 1 defect.** `read()` ignores user entries, so a session
  whose human has just submitted a prompt (user entry written, assistant reply not yet) reads
  as idle and cold. For observe-only it only over-counts; for stage 2 it is exactly C13's
  case, which the spec already covers by watching the transcript grow.

## 3. The delivery path

`helpers/pingbus/cli.py:1775-1801`. The only additions are the `coldwake.assess(coldwake.read(before[0]), …)` line before `send_notice` and the `if cold is not None` write after
the lock is released. `before[0]` is the transcript path `prepare()` already fetched. `cold`
is bound before the `try`, so neither the `SocketGone` re-raise nor the `OSError` return can
reach an unbound name, and neither reaches the log. Nothing consumed, held, budgeted or
timed changes. The read sits inside the spool lock on the critical path and costs about
10 ms (measured above) against a 2 s minimum spacing; that is a delay in the strict sense
but not one a human or the ack deadline can see. Moving the read after `send_notice` would
remove even that, at the price of reading a transcript the delivery may already have grown;
`prepare()` has the pre-send size, so this is a judgement call, not a defect.

## 4. The cost analysis

Checked the arithmetic of spec §2 with the published ratios (1h write 2, 5m write 1.25, read
0.1, output 5 for Opus): all six table cells reproduce (k=2 break-even n = 12.2, 4.1, 1.4;
k=1 saving at n=1 = 9 500, 108 500, 383 500). The honesty is right: it says plainly that the
compaction call is the same cold read of `C`, that `k` is unmeasured, and that at 60k a
one-turn wake can cost more. One thing it leaves implicit: `A` assumes the cold turn
rewrites the whole prefix at the one-hour rate, which is what Claude Code's breakpoints do
on this deployment but is the reason `2C` and not `C`; worth one clause.

## 5. Versions and the merge

- **Container bump required: yes.** `play-claude-yolo.yml` builds the pingbus zipapp from
  `helpers/` into the image (`Dockerfile:398-407`), so a `helpers/pingbus` change is an image
  change.
- **CCY bump required: yes.** `claude-yolo` is touched (`REQUIRED_CONTAINER_VERSION` must
  move with the label), and the rule binds on any edit to that file.
- **The numbers are wrong for where this lands.** The branch takes 3.99.12.11 / 2.77.8
  from a base at 3.99.12.10 / 2.77.7. Local F44 is already **3.99.12.11 / 2.77.7**, and
  `fix/lxc-checkout-typed` takes **3.99.12.12 / 2.77.8**. This branch must take
  **CCY 3.99.12.13 / container 2.77.9**, with the changelog entry "Follows 3.99.12.12
  (container 2.77.8)". Six places: `claude-yolo` lines 17 and 81, the Dockerfile label,
  the changelog heading and its "Follows" line, the PLAN.md Task 2.0e text, and the
  meta-deploy comment.
- **`git merge-tree --write-tree F44 6d3d61ab`: three conflicts** —
  `CLAUDE/Plan/meta-deploy.list`, `docs/ccy-changelog.md`,
  `files/var/local/claude-yolo/claude-yolo`. All are the version and queue lines above.
- **The queue.** F44 already carries one `playbooks/imports/play-claude-yolo.yml` entry
  (compose question, Plans 00204 and 00201). The branch adds a second. The resolution is the
  existing entries, this plan's sentence added to the existing `play-claude-yolo.yml`
  comment, and one new `playbooks/imports/play-agent-bus.yml` entry. No duplicate play.

## 6. U1-U3 as upstream issues

They are nearly filable. U1 and U2 each leave one mechanism undefined; U3 should cite the
precedent. Drafts:

- **U1 — a `compact-wake` supervisor family.** The ccy supervisor gains a built-in family
  that watches the daemon's injection directory for a request file written by an outside
  sensor (pingbus), `inject/<key>.compact-wake`, holding a JSON `{"ts": <epoch>, "nonce": <8 hex>}` and no message text. When the file is present, younger than 60 s, the input box is
  empty and the session is idle at the end of a turn (the same gates as the existing
  compaction families), the family types exactly one fixed `/compact <body>` whose body is
  the supervisor's own constant ("Summarise this session for continuation. Agent-bus
  messages are waiting and arrive as the next turn; do nothing after compacting.") and
  deletes the request file. A request older than 60 s is deleted untyped. **Before filing,
  define `<key>`:** pingbus keys its marks by session socket, the supervisor by session id;
  the issue must say which, or the daemon cannot match the file to a session.
- **U2 — PreCompact attributes the origin.** The daemon's PreCompact handler already drops
  `<session>.compacting` with `session_id` and `ts`. Add an `origin` field with a closed set
  (`human`, `supervisor`, `auto`, `bus`). **Before filing, say how `bus` is recognised:**
  the two candidates are PreCompact's `custom_instructions` equalling U1's fixed body, or the
  presence of the U1 request file for that session at compaction start; the first is
  self-contained and is the one to propose.
- **U3 — no `continue` for origin `bus`.** The supervisor's post-compaction `continue`
  (`claude-supervise.py:5963-5987`) is skipped when the signal's `origin` is `bus`, because
  the held bus delivery is the continuation. This extends the mechanism Plan 00479 already
  uses for the usage pause (`drop_await_for_usage_pause`, "compaction wait dropped, no
  continue"), so the issue should ask for the existing suppression to be keyed on `origin`
  rather than for a new path.

## 7. Fail-fast, stderr, names

- **Fail-fast.** `except OSError: return None` and the `json.loads` skip are the
  probe-then-decide pattern: the result is a reading, `None` is a legitimate value, and C4
  says a delivery must not fail on it. Acceptable for a sensor; it would not be for stage 2's
  hold, where an unreadable transcript must be a logged abandon, which C11/C14 already say.
- **Stderr.** The line goes to `err`, the watcher's stderr (`watch.log`). Nothing on stdout.
- **Install names.** The diff and the journal carry none. The spec's "`claude.exe`, offset
  about 238.7 MB" is odd (the container binary is not a `.exe`) but names no install; it should
  read "the Claude Code 2.1.303 binary".
- **QA.** `ruff check` on the four Python files: clean. `python3 -m unittest tests.helpers.pingbus.test_coldwake tests.helpers.pingbus.test_notify` with every
  `PINGBUS_*` unset: 93 tests, OK, 50.7 s. `qa-all` not run (sub-agent).

## FIX-FIRST list

1. Versions to **CCY 3.99.12.13 / container 2.77.9** in `claude-yolo` (both lines, with
   the comment), the Dockerfile label, the changelog heading with "Follows 3.99.12.12
   (container 2.77.8)", PLAN.md Task 2.0e, and the meta-deploy comment; then merge local F44
   (after `fix/lxc-checkout-typed`) and resolve the three conflicts.
2. `meta-deploy.list`: one `play-claude-yolo.yml` entry (append this plan's sentence to the
   existing comment) plus the new `play-agent-bus.yml` entry.
3. Spec F2: replace "pingbus has no other path into the session" with the tmux `send-keys`
   path named and rejected (no idle gate, F4's `continue`, pingbus alone enforcing the fixed
   body). Spec F4: note the usage-pause precedent so U3 is an extension.
4. Spec U1: define `<key>` and the request file's schema and deletion. Spec U2: name the
   attribution mechanism (fixed body in `custom_instructions`).

Not blocking: the 10 ms read inside the lock (judgement call, documented above); the
"`claude.exe`" wording; one clause in §2 on why the cold turn is `2C`.
