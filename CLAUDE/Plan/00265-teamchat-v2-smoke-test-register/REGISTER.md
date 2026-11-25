# Plan 00265 register: v2 faults found while smoke testing

Every v2 fault seen while the agent-team tooling is used for real work, by this seat, its
sub-agents, the other seats of the team and the owner. A row is added the moment the fault is
seen and stays open until a fix is proven. [PLAN.md](PLAN.md) holds the units and their tasks.

**Severity:** critical stops real work or silently loses information; high forces a manual
workaround; medium misleads; low is cosmetic.
**State:** `open`, `in progress (unit)`, `fixed (commit)` or `proven (how)`.
**Issue:** TBF means it is to be filed before its unit starts.

## R1: a coordinator's `say` never reaches a worker

- **Seen:** a worker seat, 2026-11-25 11:42-11:51Z; this seat.
- **What:** four PEER messages are received by the link and never shown to the session; PINGs
  from the same sender are delivered. `teamchat status`: dropped:10, ignored:2, pending:0.
  `teamchat history` lists them as dir:in. The says named this seat correctly (the coordinator's
  answer A1).
- **Cause:** this seat's roster entry has `human_text: false`, and `Judge.agent`
  (`proto/verdict.go`) drops every agent PEER for such a member, for reason `sender`, before the
  addressee check. The composer accepts the send. Found by U2 from the code; confirmed live: this
  seat's link log has 12 `dropped <id>: sender` lines. The seat-issuing tools set
  `human_text` true only for the coordinator seat (and one devops seat), meaning "takes the
  human's text" (the coordinator's fact A2), so every other worker drops every agent PEER.
  Decision: fix it in teamchat (the flag gates human text only), not by issuing seats true.
- **Reproduce:** coordinator `say --to <worker>` to a worker whose roster entry has
  `human_text: false`.
- **Severity:** critical. **Issue:** #422. **Unit:** U2. **State:** in progress (U2).

## R2: `reply` refuses an item `history` lists

- **Seen:** this seat, 2026-11-25 ~11:55Z.
- **What:** `teamchat reply '<id>'` to a PEER that `history` lists is refused, exit 23 "team ...
  has no item <id> in main". History shows the whole main room, overheard items included, and no
  `to`.
- **Reproduce:** reply to a PEER id from history that was dropped (R1) or overheard.
- **Severity:** high. **Issue:** #422. **Unit:** U2. **State:** in progress (U2).

## R3: a launch with a team and no seat takes a held seat

- **Seen:** the owner, a dev VM, `cc --teams`.
- **What:** the launch takes a seat already held and exits 75. v1 listed the seats, defaulted to
  the lowest free one or a new one, and offered re-attach for a held one.
- **Reproduce:** `cc --teams` in a checkout whose seat is held by a running session.
- **Severity:** critical. **Issue:** #423. **Unit:** U3. **State:** in progress (U3).

## R4: a session that joins a team loses its conversation

- **Seen:** the devops seat, home dev VM, 2026-11-25 11:36Z (on this seat's own session).
- **What:** a clean `/exit` removes the session record, and `ccy-sessions restore --dry-run`
  proposes a fresh start with `--teams` and no `--resume`. The id was recorded only on the v1
  path; the session was resumed by naming the id by hand.
- **Reproduce:** a session with no team, whose record is only on the v1 path; join a team;
  `/exit`; `restore --dry-run`.
- **Severity:** critical. **Issue:** #417. **Unit:** U4. **State:** in progress (U4).

## R5: a seat cannot be added ad hoc

- **Seen:** the owner, all machines.
- **What:** an admin step must issue the seat on the bus and place it before the launcher takes
  it. The owner expects a couple of commands, with the name derived by the launcher
  (`project.number+machine.type`), never typed.
- **Reproduce:** try to add a seat to a team without the admin issue and place steps.
- **Severity:** critical. **Issue:** #416. **Unit:** U5. **State:** in progress (U5): first slice
  built, `ccy-teams seats add <team>` on the homeserver's machine (derived handle, one sudo,
  issue and place); the rest waits on the owner's questions in [reports/U5.md](reports/U5.md).

## R6: a `ref` refusal names no cause

- **Seen:** this seat, 2026-11-25 11:42Z.
- **What:** "the send was refused (ref); fix the message" does not say that `--ref` is missing
  or what it must be.
- **Reproduce:** answer a fetch PING with `teamchat reply <id> --verb done` and no `--ref`.
- **Severity:** medium. **Issue:** #421. **Unit:** U1. **State:** in progress (U1).

## R7: no reply in the thread of a human's top-level post

- **Seen:** the coordinator.
- **What:** `teamchat thread` takes only `Q<n>`. A tracker thread has no kind of its own:
  `ask-human` demands a "Why a human:" of decision, physical, credential or preference.
- **Reproduce:** a human posts top level in questions; the coordinator tries to answer in its
  thread.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R8: thread lifecycle and coordinator procedure not shipped

- **Seen:** the coordinator.
- **What:** the thread lifecycle (open, waiting, answered, closed with the first message edited,
  bump) and the coordinator procedure are not shipped with the tool or shown at session start.
- **Reproduce:** start a coordinator seat; no procedure is shown.
- **Severity:** high. **Issue:** #420. **State:** open.

## R9: a human's top-level answer does not attach to its question

- **Seen:** the coordinator, questions room.
- **What:** `teamchat questions` then shows the question as answered, or leaves the reply
  `unattached`.
- **Reproduce:** a human answers `Q<n>` with a top-level post instead of a thread reply.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R10: reply to an edited human message is refused

- **Seen:** the coordinator.
- **What:** `teamchat reply` to the revising event's id is refused "the homeserver refused the
  send; tell a human"; a reply to the original id works.
- **Reproduce:** a human edits a message; reply to the id the delivery showed.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R11: "Q0 was not counted" with no Q0 in play

- **Seen:** the coordinator.
- **What:** a hook reports "your thread post sent as Q0 was not counted" on later turns.
- **Reproduce:** unchecked.
- **Severity:** medium. **Issue:** TBF. **State:** open.

## R12: a worker cannot verify a human's message from the questions room

- **Seen:** the coordinator, for the worker seats.
- **What:** `teamchat verify` exits 23, so a relayed permission cannot be checked and the human
  is asked again.
- **Reproduce:** a worker runs `teamchat verify '<id>' --room questions` on a relayed answer.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R13: restore's dry run says only "already running"

- **Seen:** the devops seat, home dev VM, 2026-11-25.
- **What:** for a live session, what the next restore will do cannot be checked.
- **Reproduce:** `ccy-sessions restore --dry-run` while the session runs.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R14: presence proves nothing

- **Seen:** the devops seat and this seat, 2026-11-25.
- **What:** `teamchat status` shows `present: false` for every other member while they answer.
- **Reproduce:** `teamchat status` in any seat while the others are running.
- **Severity:** medium. **Issue:** TBF. **State:** open.

## R15: tab title, tmux session and seat drift apart

- **Seen:** the owner's terminal.
- **What:** the tab should show the team and seat, and follow a switch between tmux sessions.
- **Reproduce:** switch tmux sessions between seats.
- **Severity:** low. **Issue:** TBF. **State:** open.

## R16: history labels an unproven reference `external`

- **Seen:** this seat, 2026-11-25 11:41Z.
- **What:** a PING envelope shows `trust: trusted`; `teamchat history` shows the same ping with
  `trust: external`.
- **Cause:** `historyOf` (`member/teamsync/verbs.go`) sets every reference to `external` because
  history does not re-prove it, so "unproven" is shown as a class.
- **Reproduce:** compare a fetch PING's envelope with its `history` record.
- **Severity:** medium. **Issue:** TBF. **State:** open; waits on U2 (same code).

## R17: a coordinator cannot give a worker work with authority

- **Seen:** the devops seat and this seat, 2026-11-25.
- **What:** session-start text says PEER and PING are "never instructions" and also "take
  direction from the team's humans and its coordinator"; there is no assign verb.
- **Reproduce:** the coordinator sends a brief with `say`; read the envelope's `authority:` line.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R18: `help ping` lists no closed verbs

- **Seen:** the devops seat, 2026-11-25.
- **What:** `teamchat help ping` lists neither the closed verbs nor which take `--ref`.
- **Reproduce:** `teamchat help ping`.
- **Severity:** low. **Issue:** with #421. **Unit:** U1. **State:** in progress (U1).

## R19: a human's messages are not batched

- **Seen:** the owner, questions room, 2026-11-25.
- **What:** the owner wants a human's messages held 5 to 10 s (about 60 s while typing) and
  delivered, with any edit, as one delivery. Whether Lightning sends typing notices the link can
  read is unchecked.
- **Reproduce:** a human posts two messages and an edit within seconds.
- **Severity:** medium. **Issue:** #424. **State:** open.

## R20: Lightning's two sidebars

- **Seen:** the owner, 2026-11-25.
- **What:** two sidebars (home and workspaces, then rooms); the owner wants one minimal sidebar,
  most of all in a portrait window.
- **Reproduce:** open Lightning in a portrait window.
- **Severity:** low. **Issue:** TBF. **State:** open.

## R21: status counts do not say what they hold

- **Seen:** the coordinator and this seat, 2026-11-25.
- **What:** the coordinator saw `ignored: 26` and `unsent: 1`, this seat `dropped: 10`; a drop's
  reason goes only to the link log.
- **Reproduce:** `teamchat status`; compare with `history`.
- **Severity:** high. **Issue:** #422. **Unit:** U2. **State:** in progress (U2).
