Verdict: FIX-FIRST

# Adversarial review, round 2: DESIGN-v2-B-member.md (Plan 00209 N0, area B)

Reviewed at tip 5377e2c3 against DESIGN-v2-FRAME.md and the N0 common rules. I checked each
round-1 fix in the text itself, not in the fix log, and then read the whole file again from the
start. The round-1 fixes are real and most of them hold. Four new defects break central claims:
B21/B9 (no loss), B33 (a removed team's history is not delivered), B43/B35 (the first-join
relaunch resumes the conversation) and B20 (recv and socket never both show an item). A fifth
defect makes B35's inbound rule unbuildable as written. Each one has a small, local fix.

## Round-1 findings

| #   | State                                                                                                                                                 |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01  | resolved (SCM_RIGHTS shared description, kill-then-close, locks never unlinked). The remove timeout opens a new hole: see N2                          |
| 02  | resolved (`awaiting` list, the idle-started quiet clock, `missed` and the late find, one rule on restart). Its interaction with the ledger is new: N1 |
| 03  | resolved (no team runs the keeper alone; no bus means no `run`; cost and bars given in 2.14)                                                          |
| 04  | resolved (per-digest absolute paths, plugin under `<kit>`). The inbound setting in the immutable settings file is new: N5                             |
| 05  | resolved in substance (peer descent check, socket form, SessionEnd defers to pidfd). The residual overclaim is N7                                     |
| 06  | resolved (`--list`/`--runtime`, start ticks, `--adopt`, the leftover sweep). The adopted worker's socket token is N8                                  |
| 07  | resolved for SessionStart. Not resolved for the first-join relaunch it was raised for: N3                                                             |
| 08  | resolved (cap, `more`, ack). The pre-ack state is unspecified: N4                                                                                     |
| 09  | resolved (BD14, D2)                                                                                                                                   |
| 10  | resolved (B11 restated, the next worker closes the run, R39 openat walk, fchown/fchmod, EIO/EPIPE)                                                    |
| 11  | resolved (caps sized from the schema, a 2 MiB sum, the cross-store order, per-store restart)                                                          |
| 12  | resolved (`floor_ms`, `restored`, the residual risk stated under C7)                                                                                  |
| 13  | resolved (invites, filter rebuild, demotion stop, `revises:`, `human_text`, A7)                                                                       |
| 14  | resolved (the carried-rules list in section 5)                                                                                                        |
| 15  | resolved (tmux in B36; the host compares `sha256sum` before running the binary)                                                                       |
| 16  | resolved (heartbeat from the state goroutine's timer)                                                                                                 |
| 17  | resolved (retry on a bundle change or a generation change; the store is kept)                                                                         |
| 18  | resolved (`unusable` counts, HUMAN render NOTICE, `desired.state`, BD15)                                                                              |
| 19  | resolved, all of (a)-(g)                                                                                                                              |
| 20  | resolved (alternatives per part; a tests-by-piece table)                                                                                              |

## New findings

N1. **MAJOR. 2.3.2, 2.7.3 and B44: after a restart, items declared missing are dropped as
already delivered, with no notice.**
The full write puts the ids into the ledger ("full write: persist written "yes"; the ids go to the
ledger"). `missing` moves them back to `pending` and marked `repeat`, and nothing takes them out of
the ledger. On a worker restart, every pending item is a restored item. The restore rule (2.3.2
bullet 1, 2.7.3 "Content", B44) then drops "an id the ledger already holds". So a delivery that was
declared missing, followed by any worker restart (a crash, a remove timeout, or the backoff after
a 78 elsewhere), loses items the agent was told to `recv`. The only trace is a `worker.log` line.
That breaks B21 ("its items return to recv") and the property "no accepted item is lost". The
same path catches a "yes" entry that is still in `awaiting` at the restart and is declared missing
after it: its ids are in the ledger as well.
**Fix:** remove the ids from the ledger when `missing` returns them to `pending`, in the same
store write. Or make the replay check "in the ledger and not in `pending`/`awaiting`". Add a
failpoint property: declare missing, SIGKILL the worker, restart, and the items are still pending.

N2. **MAJOR. 2.5 and 2.10: a remove that times out leaves the store behind, which breaks B33.
The 10 s bound is routinely too short.**
On timeout, the keeper kills the worker and closes the seat. The worker never reached "deletes
the store". The next take of that seat, by this session on a re-add or by another session,
therefore loads the old store and syncs from its stored position. Everything posted while the team
was out is then delivered, against B33 ("nothing posted while it was out is delivered when it
returns; a re-added team starts from a fresh sync"). The timeout is also not rare. The remove
step posts a farewell, and a send has a 30 s deadline with three tries (2.4.4). So removing a team
whose homeserver is down always passes 10 s. That SIGKILLs the worker for every other team, with
their in-flight deliveries and sends.
**Fix:** bound the farewell send inside `remove` (for example 3 s, then `"farewell": "owed"`), so
that the keeper's 10 s covers it. After the kill, the keeper, still holding the seat lock, renames
the store aside (`store.json.removed-<ms>`) before it closes the lock. That is a file rename, not a
store write, so B14 holds; state it as the one exception to "the keeper never writes a store". Or
have the worker write a `removed` marker first, which the next add treats as "no store". Add the
crash test: a remove with an unreachable homeserver, then a re-add, and no item from the gap is
delivered.

N3. **MAJOR. 2.8, 2.3.3 and 2.10: the first-join relaunch has no conversation record for the
session it relaunches.**
`conversation.json` is written only by the worker, only at SessionStart, and only "for each held
seat". A session launched with no team has no worker at SessionStart, and its hooks answer `{}`
(2.4.1) and pass nothing on. When the first seat arrives, the worker starts after SessionStart.
UserPromptSubmit only fills in socket facts and the transcript path, so `conversation.json` is never
written for this session. The relaunch that B35, B43 and C9 resume "by the seat's
`conversation.json`" then finds no file, or finds one left by an earlier session on that seat and
resumes the wrong conversation (`claude --resume <old id>`). This is exactly the flow R46 was
carried for.
**Fix:** in a session with no team, the keeper keeps SessionStart's `session_id`, `source` and
transcript path in memory. Either it serves a minimal `hook.session-start` on its control path, or
the hook writes them into `<v2-run>` (0600; they are not secret). The keeper hands them to the
worker with the first `add`, and the worker writes `conversation.json` on `add` as well as on
SessionStart. A record whose `run` differs from the current run must never be used for a
relaunch. Test: launch with no team, hot add, and the relaunch resumes this session's id.

N4. **MAJOR. 2.4.4 and 2.7.3: the state of an item between `recv` sending it and the CLI's ack is
not defined, so B20 can fail.**
The worker marks items delivered "on that line and on nothing else". If they stay `pending` until
the ack, the delivery goroutine can pick them for a socket delivery in that window (pacing is 2 s;
a CLI under load, or a model slow to read the tool output, takes longer). The agent then sees the
item in the `recv` output and again in the socket delivery. The claim "`recv` takes `pending`
only; an item awaiting proof is not pending" does not cover this window.
**Fix:** add an in-memory state, `reserved by recv <ack>`. The delivery goroutine skips it. The
ack moves it to delivered (and into the ledger, which the diagram also leaves unsaid, see N9).
A closed connection, a missing ack or a worker crash returns it to `pending`. It is never
persisted, so a crash leaves it pending and recv shows it again. Add the state to the 2.7.3
diagram and an interleaving property.

N5. **MAJOR. 2.7.1, 2.12 and B35/BD10: the inbound setting cannot live in an immutable kit
settings file.**
`<kit>/settings.json` is one file per digest, never changed (2.4.1), and "holds ... the inbound
setting of B35" (2.12). Yet a launch with a team must have `crossSessionInbound: accept` and a
launch with no team must keep it refused (2.7.1, 2.10). With one fixed file, either every session
accepts, which breaks BD10's least privilege, or none does, and socket delivery never works. `run`
has no step that chooses between them, although the desired file carries "the inbound decision
made at launch".
**Fix:** in `run` step 2, read the desired file's `inbound` and pass a second `--settings` that
holds only that key. `run` writes this file into `<v2-run>` (0600). Alternatively the kit carries
two fixed files (`settings-accept.json`, `settings-refuse.json`) and `run` picks one. State which,
and test both launches.

N6. **MINOR. 2.7.3: a crash between two stores' awaiting writes is not "harmless".**
Store A has the entry and store B does not, so the user line was never written. Yet A's entry
waits on the quiet clock (up to 1 h when busy), then comes back held from the socket, marked
`repeat`, and can only be taken by `recv`. That delays a HUMAN message and labels as a repeat an
item that was never sent, against B20 ("a repeat is possible only for a delivery whose write may
have begun").
**Fix:** each awaiting entry lists every team in its delivery. On restart, if any listed store
lacks the nonce, nothing was written: return the items to `pending` at once, with no `repeat`.

N7. **MINOR. 2.4.4 and B42: the transcript check binds to a `session_id` the request itself
supplies, and `~/.claude` is hardcoded.**
A forged `hook.session-start` from the agent's own Bash tool (a descendant, so it passes) names
its own `session_id`. Any `<id>.jsonl` under `~/.claude/projects/`, including another project's
session, then passes. So "a transcript of this session" is not enforced. Claude Code also honours
`CLAUDE_CONFIG_DIR`, which ccy may set.
**Fix:** derive the projects directory from `CLAUDE_CONFIG_DIR`, defaulting to `~/.claude`. Bind
the transcript to the session's cwd-derived project directory. Accept a new `session_id` only on
SessionStart with `source` `clear` or `resume`, or soften B42's wording to what is actually
enforced.

N8. **MINOR. 2.4.5 and 2.8: an adopted session cannot be woken until a human types.**
The inbox socket token lives only in the dead keeper's memory and the exited worker's memory.
After `repair --runtime`, the new worker gets the token only from `hook.prompt` ("only if none are
set"). Stop and SessionStart do not set it. So an idle session, the one a delivery must wake,
stays unreachable until its next prompt. The keeper-crash row also leaves `run.ended` `null`. The
next worker would then post "disconnected: the session that ended at <time>" while the session
still lives.
**Fix:** let `hook.stop` also fill in missing socket facts, under the same checks, or state that
an adopted session stays in `wait` mode until the next prompt. On a keeper crash, have the worker
write a distinct `ended` reason (`keeper-lost`) that the next worker renders correctly.

N9. **MINOR. Contradictions between requirements and design.**
(a) B26 says Stop "never [blocks] while socket delivery is live", but the Stop row blocks for
items held as missing even then. Restate B26.
(b) B21 says missing is declared only after "a Stop hook since the write", but 2.7.4 also starts
the clock at the write when the session was idle. Align B21.
(c) It is not said whether items taken by `recv` go into the ledger. Line 291 says "every item it
delivered", but the diagram puts only socket deliveries into the ledger.
(d) "A cap reached is a loud failure of that team (exit 78 on the next verb)". If that includes
`recv`, a full `pending` list can never be drained. State that a full `pending` stops only the
sync (the position is held) and that `recv` still drains.
(e) 2.4.2 says the keeper's desired-file failures reach the owner "through the worker's `status`",
but a session with no team has no worker, so `status` exits 69. Name `report.json` as the channel
there (it is per held seat, so with no seat, say where the keeper reports instead).

N10. **MINOR. C3: kit garbage collection cannot see every runtime.**
"An old `<digest>/` is removed only when no runtime's `session.pid` names it", but the runtimes sit
in container-private `/tmp` (ccy) and in each guest's `XDG_RUNTIME_DIR` (LXC). The host cannot list
them directly. For ccy the kit is in the image, so this is moot there. For LXC it needs a
`guest kit-in-use` op, or `install-self` must sweep inside the guest. State which, in C3 and 2.13.

N11. **MINOR. 2.4.1 step 2: the re-exec resolves `current` a second time.**
The re-exec'd `run` follows `current` again, so an install landing between the two reads gives a
session a different kit from the one checked. It also checks the digest by running the binary's
own `--version`, the self-vouching that 2.13 rejects for guests. Pass the resolved `<kit>` to the
re-exec as a flag. Since the kit is host-placed and never changed, it is fine to say that
`--version` is a consistency check, not an integrity check.

The frame's form is met: requirements with sources, two or more designs per part, owner decisions
with defaults (BD1-BD15), risks, drops with reasons and the carried list, assumptions per area, and
tests per piece. The seams are respected: content and trust are cited to A, the bundle and host
side to C, and names to D through placeholders. No install-specific names were found.
