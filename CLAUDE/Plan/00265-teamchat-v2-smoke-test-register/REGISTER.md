# Plan 00265 register: v2 faults found while smoke testing

Every v2 fault seen while the agent-team tooling is used for real work, by this seat, its
sub-agents, the other seats of the team and the owner. A row is added the moment the fault is
seen and stays open until a fix is proven. [PLAN.md](PLAN.md) holds the units and their tasks.

**Severity:** critical stops real work or silently loses information; high forces a manual
workaround; medium misleads; low is cosmetic.
**State:** `open`, `in progress (unit)`, `fixed (commit)` or `proven (how)`.
**Issue:** TBF means it is to be filed before its unit starts.
**Cause class:** kept for the owner's retrospective in [Cause classes](#cause-classes) at the end,
filled in as each cause is known.

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
- **Severity:** critical. **Issue:** #416. **Unit:** U5. **State:** first slice merged (751bdeab3),
  `ccy-teams seats add <team>` on the homeserver's machine (derived handle, one sudo, issue and
  place; worker seats take no human text by default); seat creation in the launch follows U3;
  the rest waits on the owner (Q6, [reports/U5.md](reports/U5.md)). Live proof owed.

## R6: a `ref` refusal names no cause

- **Seen:** this seat, 2026-11-25 11:42Z.
- **What:** "the send was refused (ref); fix the message" does not say that `--ref` is missing
  or what it must be.
- **Reproduce:** answer a fetch PING with `teamchat reply <id> --verb done` and no `--ref`.
- **Severity:** medium. **Issue:** #421. **Unit:** U1.
  **State:** fixed (merge ea576bbbf): each refusal reason has its own message naming the cause
  and the next step; a ref refusal names the verb, `--ref`, the forms it takes and the pinged ref.
  Defence: project rule `project_refusal_reason_text` (red on 3 real sites, green after).

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
- **Cause (from Tuwunel's source, U6):** a reply in questions to an edit's id starts a thread
  rooted at the edit, which Tuwunel refuses (400); the fake homeserver accepted it.
- **Severity:** high. **Unit:** U6. **State:** fixed (merge c3c336cd4): a reply to an edit
  answers the original; the fake homeserver refuses a thread on an edit. Live proof owed.

## R11: "Q0 was not counted" with no Q0 in play

- **Seen:** the coordinator.
- **What:** a hook reports "your thread post sent as Q0 was not counted" on later turns.
- **Cause (from code, not proven live):** a coordinator's thread post that replies to a human
  post with no question number is placed with `PlacedThread.N == 0`
  (`proto/questions_prov.go` `PlaceThread`). When its echo folds as not counted, `echoed` records
  `Dropped{N: 0}`, and `hooktext.Uncounted` renders "sent as Q0 was not counted: Q0 is another
  question", a reason that cannot be true for N 0. It belongs with R7.
- **Reproduce:** the coordinator replies with `teamchat reply` to a human's top-level post in
  questions; read the next turns' hook lines.
- **Severity:** medium. **Issue:** TBF. **State:** open (with R7).

## R12: a worker cannot verify a human's message from the questions room

- **Seen:** the coordinator, for the worker seats.
- **What:** `teamchat verify` exits 23, so a relayed permission cannot be checked and the human
  is asked again.
- **Reproduce:** a worker runs `teamchat verify '<id>' --room questions` on a relayed answer.
- **Cause (from Tuwunel's source, U6):** Tuwunel answers a non-member's event fetch 404, not
  403; the worker's link joined questions only on 403, so it never joined, and every relayed
  answer to a worker was dropped too. The fake homeserver answered 403.
- **Severity:** high. **Unit:** U6. **State:** fixed (merge c3c336cd4): the link joins on 403
  or 404; the fake homeserver answers as Tuwunel. Live proof owed.

## R26: qa-changed's Go step cannot run on arm64

- **Seen:** U6 and the other units, this seat's arm64 container, 2026-11-25.
- **What:** go-qa pins ast-grep 0.50.0 for linux/amd64 only, so `./scripts/qa-changed.bash`
  fails its Go step before any check; no Go change passes targeted QA locally on arm64. Also:
  `lint_on_edit` denies Go edits with "parallel golangci-lint is running" when agents edit at once.
- **Reproduce:** `./scripts/qa-changed.bash` with a Go change on an arm64 host.
- **Severity:** high. **Issue:** #426. **State:** open.

## R27: "tell a human" for a server refusal the agent caused

- **Seen:** U6, 2026-11-25.
- **What:** a homeserver refusal caused by the agent's own input (as in R10) is told as "the
  homeserver refused the send; tell a human", sending an agent-fixable fault to a human.
- **Severity:** medium. **Issue:** TBF. **State:** open (the refusal-cause class of U1).

## R28: an edit of an unanswered post stays listed after the reply

- **Seen:** U6, from the code, 2026-11-25.
- **What:** after a reply to an edited human post, the edit stays in the unanswered list.
- **Severity:** medium. **Issue:** TBF. **State:** open.

## R29: repository tooling that slowed the units (not v2 itself)

- **Seen:** U1, U6 and this seat, 2026-11-25, with eight units running at once.
- **What:**
  - the QA lock lets a later run overtake one already waiting, so a unit's `qa-changed` never
    got the lock;
  - `lint_on_edit` denies Go edits with "parallel golangci-lint is running" (a lock clash, not a
    finding);
  - lint runs over the `analysistest` fixtures, which are meant to hold defects;
  - error-hiding and secret guards fired false positives on the units' code;
  - the branch pool counts unit branches merged into the integration branch as open, so the
    owner's one-integration-branch pattern exceeds the cap and the stop guard recorded the active
    `v2-smoke` as abandoned.
- **Severity:** medium. **Issue:** TBF (each to its owner: this repository's QA, the hooks
  daemon). **State:** open.

## R30: no released teamchat kit archive

- **Seen:** the coordinator's decision on O1, 2026-11-25.
- **What:** a container on another host gets the member kit only by its own setup copying it
  from a fedora-desktop host; a released static kit archive would let it fetch one.
- **Severity:** low. **Issue:** TBF. **State:** open (later improvement).

## Cause classes

For the owner's retrospective: each fault's cause class, and the check that would have caught it
before the move to v2. Filled in as causes are found.

| Row      | Cause class                                                       | Check that would have caught it                                                                  |
| -------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| R1       | one flag with two meanings (`human_text` also gating agent PEERs) | a delivery matrix test: every sender role to every member kind, with and without the flag        |
| R2, R21  | a decision recorded only in a log                                 | a rule that every drop or ignore reaches the session or `status` with its reason                 |
| R3       | a v1 capability not carried over (seat choice at launch)          | a v1-parity checklist of the launch behaviours, run against v2 before the switch                 |
| R4       | a v1 capability not carried over (resume by record)               | the same parity checklist; a restore dry run in the cut-over acceptance                          |
| R6, R18  | an error that names a code, not a cause                           | the project rule `project_refusal_reason_text` (now built)                                       |
| R10, R12 | a test double that differed from the real server                  | the fake homeserver's answers pinned against real Tuwunel (the `tuwunel`-tagged suite run in CI) |
| R11      | a sentinel value rendered as data (N 0 shown as "Q0")             | a test of every hook line over its edge values                                                   |
| R14      | an output field filled from a placeholder, never its source       | a review rule: each status field names its source; a test that another member can be `present`   |
| R16      | "not checked" shown as a class (`external`)                       | the same review rule                                                                             |
| R22      | a path never run on a real machine (other-host LXC)               | a live acceptance on the real topology before the switch                                         |
| R23      | a flag the grammar accepts and production never reads             | the `TestProduction` table (now holds the keyed kind); a live start of one LXC seat              |
| R26      | an untested architecture (arm64)                                  | a CI job on arm64, or a pin check for every architecture the hosts run                           |

## R13: restore's dry run says only "already running"

- **Seen:** the devops seat, home dev VM, 2026-11-25.
- **What:** for a live session, what the next restore will do cannot be checked.
- **Reproduce:** `ccy-sessions restore --dry-run` while the session runs.
- **Severity:** high. **Issue:** TBF. **State:** open.

## R14: presence proves nothing

- **Seen:** the devops seat and this seat, 2026-11-25.
- **What:** `teamchat status` shows `present: false` for every other member while they answer.
- **Reproduce:** `teamchat status` in any seat while the others are running.
- **Cause:** `memberRecords` set `present` to `id == self`; nothing ever read presence.
- **Severity:** medium. **Issue:** TBF. **Unit:** U7.
  **State:** fixed (U7, merged b1d62a0e0): status reads Matrix presence per agent
  (`presence`: online, unavailable, offline, unknown). Live proof owed (reports/U7.md).

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
- **Severity:** low. **Issue:** with #421. **Unit:** U1.
  **State:** fixed (merge ea576bbbf): `help ping` and `help reply` list the verbs and which take a
  reference, built from `proto.Verbs`.

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

## R22: LXC containers on another host cannot join a v2 team

- **Seen:** the owner and a container team, a dev VM, 2026-11-25 ("not working at all").
- **What:** the v2 LXC route admits only containers on the homeserver's own host (lxc-attach,
  the bridge address); here the containers run on another guest. Also asked: how a host-started
  LXC session signs in (A2), a v2 contract for a container repository's own launcher (A3), a v2
  `lxc-add` (A4), two doc points (A5), and the uid rule K15 in the user docs.
- **Reproduce:** a team whose homeserver is on host X; a container on host Y tries to join.
- **Severity:** critical. **Issue:** #425. **Unit:** U8 (Plan 00257 Task B.4).
  **State:** in progress (U8).

## R23: no v2 LXC session can start

- **Seen:** U8, from the code, 2026-11-25.
- **What:** `teamchat run --kind lxc --key K` is refused 78 "a lxc launch needs --desired": the
  keeper's production wiring never read `--key` (home layout, no desired file). So every v2 LXC
  session unit, and any hand or container-launcher run, cannot start, on the homeserver's own
  host too.
- **Reproduce:** `teamchat run --kind lxc --key <key> -- claude` in a member container.
- **Severity:** critical. **Issue:** #425. **Unit:** U8.
  **State:** fixed, on F44 at 5fea8d97f (CI green), with `play-teamchat.yml` queued in
  `meta-deploy.list`; deploy (L5) and live proof owed.

## R24: a ping straight after its push is refused as "provenance failed"

- **Seen:** the coordinator, 2026-11-25 12:14Z, once.
- **What:** `teamchat ping fetch commit:<repo>@<sha>` in the same command as the `git push` of
  that commit was refused, exit 13 "the reference's provenance failed at the forge"; the same
  ping seconds later was sent. The forge had not yet indexed the commit; the message names the
  wrong cause.
- **Reproduce:** `git push ... && teamchat ping fetch commit:<repo>@<new sha> --to <member>`.
- **Severity:** low. **Issue:** TBF. **State:** open (the refusal-cause class of U1).

## R25: a seat started on the fallback image joins the team silently

- **Seen:** a hooks-daemon seat's first launch on arm64, 2026-11-25 12:15Z.
- **What:** the project's `.claude/ccy/Dockerfile` failed to build; ccy started the session on
  the base image with its build-failure prompt, and the seat joined the team in that state,
  without the project's toolchain. It reports that `origin` over an SSH host alias did not work
  in that container (unchecked). Wanted: what a fallback-image seat has and lacks, and whether a
  team seat says so in its first message.
- **Reproduce:** launch a team seat whose project Dockerfile fails to build.
- **Severity:** medium. **Issue:** TBF. **State:** open.
