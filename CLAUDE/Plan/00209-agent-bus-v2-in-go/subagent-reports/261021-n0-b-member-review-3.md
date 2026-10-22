Verdict: FIX-FIRST

# Adversarial review, round 3: DESIGN-v2-B-member.md (Plan 00209 N0, area B)

Reviewed at tip 3d6ec120 against DESIGN-v2-FRAME.md and the N0 common rules. I checked each
round-2 fix in the text itself and not only in the fix log. Then I read the parts the fixes
touched (2.1, 2.3, 2.4, 2.5, 2.7, 2.8, 2.10, 2.11, 2.15, C6, C9) against the central claims. All
eleven round-2 findings are resolved. Two new defects break central claims: R1 (fail fast and
exit 69, introduced by the socket hand-off) and R2 (repeat marking, introduced by `handed`). Both
fixes are small and local. The rest are MINOR.

## Round-2 findings

| #   | State                                                                                                                                                                                           |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| N1  | resolved. The ledger is exactly awaiting-`yes`, proven or recv-acked, and `missing` removes the ids in the same write (B44, 2.3.2, 2.7.3). There is a property and a replay test                |
| N2  | resolved. Removal is local; the farewell comes later with its own bound; on a timeout the keeper deletes the store under the lock (2.5, 2.10). New edges: R3, R4, R5                            |
| N3  | resolved. The keeper's reduced server writes the conversation record before any team, used only on a matching `run` (2.3.3, 2.4.2, C9). The hand-off it adds brings R1 and R6                   |
| N4  | resolved for the live path (`handed`, exclusive, 10 s return marked `repeat`). The crash path is not marked: R2                                                                                 |
| N5  | resolved. `run` step 5 writes `<v2-run>/settings.json` with an exact append rule, and the kit settings drop inbound. New edges: R7, R8, R9                                                      |
| N6  | resolved. `teams` is in each awaiting entry, and an `unknown` entry missing from a held store returns at once with no `repeat`; a seat no longer held is skipped (2.7.3)                        |
| N7  | resolved. `CLAUDE_CONFIG_DIR` comes from `run`'s environment, the transcript is bound to the project directory, a new id is taken only on `clear`/`resume`, and B42 states the residual (2.4.4) |
| N8  | resolved. `hook.stop` fills in the socket facts, and `keeper-lost` is rendered by run id (2.4.4, 2.4.5, 2.11). Adopt has gaps: R10                                                              |
| N9  | resolved (a)-(e). (e) has a sibling gap: R11                                                                                                                                                    |
| N10 | resolved. GC happens where runtimes are visible: never for ccy, `install-self` for LXC, the host install for `cc` (2.13, C3)                                                                    |
| N11 | resolved. `--kit` is passed to the re-exec, and `--version` is named a consistency check (2.4.1 step 2)                                                                                         |

## New findings

R1. **MAJOR. 2.4.2, 2.4.4, 2.2.4 and 2.15: the keeper keeps the listening `worker.sock` open, so
"cannot connect, exit 69" never happens. Requests the CLI gave up on run later.**
The keeper "keeps its listening descriptor for the session's life" (2.4.2). It does not serve the
socket while a held seat's worker is restarting (backoff 1 s to 60 s), or after a final exit 78.
While a listener exists, `connect` succeeds and the request lands in the backlog. So:

- the CLI never gets `ECONNREFUSED`, and the 69 `no-worker` rule of 2.4.4 cannot fire;
- after a final 78, every verb hangs until a client deadline the design never states, because
  deadlines are "set by the worker per operation";
- the 2.15 `wait` test, "no worker for the whole timeout: exit 69", contradicts the mechanism: the
  test would see `wait` time out with 4, or hang;
- a request whose CLI was killed (a Bash tool timeout) is still in the socket buffer, so the next
  worker reads it and acts on it. A `say`, `reply` or `ping` the agent saw fail is then posted, and
  the agent's retry posts it a second time. The duplicate gate catches only identical text inside
  its window.

Hook ops queued the same way are applied out of order, after the hook has already printed its own
"not running" answer.
**Fix:** whenever no worker is serving (restart backoff, failed 78), the keeper serves its reduced
server. That is not only the case where no seat is held. It answers hook ops with its fixed line,
records their facts as it already does with no seat, and answers every other op 69 `no-worker` at
once. Add a `deadline_ms` to every request (the CLI's own deadline). A server that accepts a
request past its deadline discards it unexecuted. Hand the descriptor over only to a worker that
has reported ready, and take serving back when its pidfd fires. Test: a worker in backoff gives 69
in under 300 ms for `say`, a `say` whose CLI died is never posted, and `wait` still reconnects.

R2. **MAJOR. 2.4.4, 2.7.3 and B20: an item a `recv` printed is shown again with no `repeat` mark
after a worker crash.**
`handed` is memory only: "a worker crash leaves them in the store's `pending`, and they come back
`restored`". The CLI may already have written the records to stdout when the worker dies (the
records are sent, then the ack is lost). The next `recv` or delivery then shows them marked
`restored`, not `repeat`. That breaks B20 ("a `recv` whose acknowledgement never came ... is marked
as a repeat") and the 2.15 property "no item reaches the fake transcript twice unless the second
copy carries `repeat`". A `recv`'s output is a tool result in the transcript, and 2.15 lists the
failpoint "the worker while items are `handed` to a `recv`", so the property fails in its own test.
**Fix:** before the records are sent, set `repeat: true` on those `pending` entries in one store
write. The ack removes them from `pending` anyway, so a clean `recv` never shows the flag. This
costs one store write per `recv`, which 2.3.2's write cost and 2.14 should count. Or persist a
`handed` list with the recv id. Fix the diagram's last line and the `repeat:` wording to match.

R3. **MINOR. 2.10, 2.3.3, C6: `"farewell": "owed"` is written into a seat's `report.json` after
the seat is released.**
The worker releases the seat before it posts the farewell, so another session's keeper can take
the seat inside the 30 s and rewrite `report.json`. Two keepers then write one file, and the
`owed` that C6 depends on is lost. The same happens after session end, when the marker is "left in
`report.json`" of a free seat. **Fix:** the keeper keeps its seat-lock descriptor until the farewell
is posted or the session ends. The worker has already closed its own and deleted the store, so
B33 and the single writer of the store are unaffected. Only then does the keeper write the final
`report.json` and release. Say that a new taker never sees an `owed` it did not write.

R4. **MINOR. 2.10 and 2.5: a timed-out removal of the last held seat has no worker left to post the
owed farewell.**
"The restarted worker is passed the owed farewell", but with no other seat the keeper restarts no
worker (2.5 restarts it "for the other held seats"). The farewell stays owed until the session
ends, which can be days, and the bundle stays placed that long (C6). **Fix:** the keeper starts a
worker that holds no seat and only posts owed farewells (bounded at 30 s, then it ends), or C posts
the farewell once `report.json` says `owed` and the seat is free.

R5. **MINOR. 2.5 and 2.15: a keeper crash between the SIGKILL and the store deletion frees the seat
with the store intact.**
A re-add then syncs from the old position and delivers the gap, against B33. This window is a
keeper bug, not an expected path, but the 2.15 property "a removed team's items never reach the
transcript" claims every failpoint. **Fix:** name it as a residual in 2.5 and in the risks, or add
the failpoint and accept the residual explicitly.

R6. **MINOR. 2.4.2: the hand-off can overlap.**
At the first `add` the keeper may still be serving a connection it accepted, such as a
`hook.session-start` that writes the conversation record. The worker serves at the same moment,
which breaks the "one writer at a time" claim of 2.3.3. **Fix:** the keeper stops accepting,
finishes the connections in flight, snapshots the facts, and only then passes the descriptor and
the facts. Do the reverse when it takes serving back.

R7. **MINOR. 2.4.1 step 5 and 2.7.1: "left refused" by leaving the key out does not refuse.**
If the user's or the project's settings set `crossSessionInbound: accept`, a launch with no team
takes cross-session messages, against BD10. **Fix:** `run` always writes the key, with the refusing
value when `inbound` is `refuse`, and relies on flag settings outranking user and project settings.
Add that precedence to the host check. Also say what step 5 does when `run` has no `--desired`
(the `run -- bash` case): no settings are written, since no `claude` is launched, or refuse.

R8. **MINOR. 2.4.1 step 5: guessing Claude Code from the base name `claude` is a hidden mode
switch.**
A launcher whose Claude Code entry is a wrapper or has another name still takes seats, holds
presence and syncs. But it gets no plugin, so it has no hooks, no SessionStart frame and no socket
facts. The agent is never told that the bus exists, and items queue unseen while the team sees a
present member. **Fix:** the launcher states the kind, for example `run --claude -- CMD`, which
appends the flags (exit 64 rules as now), and `run --plain -- CMD`, which appends nothing. A
launcher that holds seats without `--claude` is told so in `status` and `report.json`
(`"delivery": "none"`). Nothing is inferred from a file name.

R9. **MINOR. 2.10 and 2.4.1: the keeper and the worker are never told the launch-time inbound
decision.**
Only `run` reads `inbound`, and it writes it into the settings file. The keeper is started with
`--desired` and re-reads the desired file on every change. At the first join, C must rewrite
`inbound` to `accept` for the relaunch. A worker that reads the current file would then believe
that this session accepts, push socket messages Claude Code holds, and never set
`needs_relaunch: true`, so C's relaunch never comes. **Fix:** pass `--inbound accept|refuse` from
`run` to the keeper (or have the keeper read `<v2-run>/settings.json`), and on to the worker. State
that the launch value, never a later read, decides socket delivery and `needs_relaunch`. Test: a
desired file rewritten to `accept` under a refused session still gives `wait` mode and
`needs_relaunch: true`.

R10. **MINOR. 2.4.5 and 2.11: adopt has two unstated steps.**
(a) The worker resumes silently only on "this runtime's own run id", but `session.pid` holds no run
id, and in ccy `<v2-run>` is a `/tmp` path whose form is not given. Add `"run"` to `session.pid`.
(b) The dead keeper and worker leave a stale `worker.sock`, so the adopting keeper's `bind` fails
with `EADDRINUSE`. State that, under `keeper.lock`, it unlinks the stale socket path (a socket, not
a lock file, so 2.5's never-unlink rule does not apply) and binds again.

R11. **MINOR. 2.8, B25 and N9(e): with no seat held, the counts of unusable hook stdin have no
reporter.**
B25 says the counts are "reported through `status`", but `status` exits 69 with no worker, and
`session.json` is the worker's. **Fix:** the keeper's reduced server counts them in memory, puts a
count in `status.json` (shown by the status line like `desired`), and hands the count to the worker
with the session facts.

R12. **MINOR. 2.10 and 2.4.4: a removed team's `handed` and `awaiting` items are unspecified.**
A `recv` ack that arrives after its team's store was deleted must not recreate the store. An
awaiting HUMAN item of a removed team loses its read receipt. State that the ack is ignored for a
team no longer held, and that its receipts are dropped with the store, so Element shows those
HUMAN items as unread.

Seams hold (A, C1-C9, D placeholders); no install-specific names; BD1-BD15 not re-raised.
