# Plan 00265 register: v2 faults found while smoke testing

Every v2 fault seen while the agent-team tooling is used for real work, by this seat, its
sub-agents, the other seats of the team and the owner. A row is added the moment the fault is
seen and stays open until a fix is proven. [PLAN.md](PLAN.md) holds the units and their tasks.

**Severity:** critical stops real work or silently loses information; high forces a manual
workaround; medium misleads; low is cosmetic.
**State:** `open`, `in progress (unit)`, `fixed (commit)` or `proven (how)`. "On F44 (X)"
means F44 commit X has the tree of a v2-smoke commit that holds the cited fixes. F44 gets each
batch as one commit-tree copy, so the v2-smoke hashes are never F44's ancestors.
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
- **Severity:** critical. **Issue:** #422. **Unit:** U2. **State:** fixed (U2), merged into
  `v2-smoke` and on F44 at c1b20004f (the home pin), but in no image until container 2.84.78
  (`09c383f5b`; R54). The live check is in `reports/U2.md`.
  The owner confirmed the new meaning (lts-ops Q11, A12): `human_text` gates a human's words
  only. Plan 00209 A 7.6 rule 08 and Plan 00161 D63 now say so.

## R2: `reply` refuses an item `history` lists

- **Seen:** this seat, 2026-11-25 ~11:55Z.
- **What:** `teamchat reply '<id>'` to a PEER that `history` lists is refused, exit 23 "team ...
  has no item <id> in main". History shows the whole main room, overheard items included, and no
  `to`.
- **Reproduce:** reply to a PEER id from history that was dropped (R1) or overheard.
- **Severity:** high. **Issue:** #422. **Unit:** U2. **State:** fixed (U2). A dropped PEER is
  now delivered, and `reply` to an overheard or judged-out event says why it refuses. The live
  check is in `reports/U2.md`.

## R3: a launch with a team and no seat takes a held seat

- **Seen:** the owner, a dev VM, `cc --teams`.
- **What:** the launch takes a seat already held and exits 75. v1 listed the seats, defaulted to
  the lowest free one or a new one, and offered re-attach for a held one.
- **Reproduce:** `cc --teams` in a checkout whose seat is held by a running session.
- **Severity:** critical. **Issue:** #423. **Unit:** U3. **State:** fixed (U3, ccy 3.124.44),
  merged into `v2-smoke`: a team named with no seat never takes a held seat; the seats are
  listed with their holders, the lowest free one the default, and a held one re-attaches its
  session. The no-free-seat outcome is one seam (`Teamchat.NewSeat`), which the follow-on wires to
  U5's seat creation. Live proof is owed (`reports/U3.md`).

## R4: a session that joins a team loses its conversation

- **Seen:** the devops seat, home dev VM, 2026-11-25 11:36Z (on this seat's own session).
- **What:** a clean `/exit` removes the session record, and `ccy-sessions restore --dry-run`
  proposes a fresh start with `--teams` and no `--resume`. The id was recorded only on the v1
  path; the session was resumed by naming the id by hand.
- **Reproduce:** a session with no team, whose record is only on the v1 path; join a team;
  `/exit`; `restore --dry-run`.
- **Severity:** critical. **Issue:** #417. **Unit:** U4. **State:** fixed (U4), merged into
  `v2-smoke` (15fbc90cd, ccy 3.124.45; U3 and U4 had both taken 3.124.44). A plain
  `ccy --teams <seats>` resumes the conversation recorded per seat
  (`.claude/ccy/seat-conversations/`); with none it exits 64 naming the repair and `--fresh`.
  The seat 4 and seat 5 difference (A6) is not caused by the resume or startup source; seat 4's
  record or transcript was missing, which only the host's files can show. Live proof is owed
  (`reports/U4.md`). The interplay with U3's chooser (a never-used free seat) is being checked.

## R5: a seat cannot be added ad hoc

- **Seen:** the owner, all machines.
- **What:** an admin step must issue the seat on the bus and place it before the launcher takes
  it. The owner expects a couple of commands, with the name derived by the launcher
  (`project.number+machine.type`), never typed.
- **Reproduce:** try to add a seat to a team without the admin issue and place steps.
- **Severity:** critical. **Issue:** #416. **Unit:** U5. **State:** first slice merged (751bdeab3),
  `ccy-teams seats add <team>` on the homeserver's machine (derived handle, one sudo, issue and
  place; worker seats take no human text by default); seat creation in the launch follows U3;
  the rest waits on the owner (Q6, [reports/U5.md](reports/U5.md)). **Fixed on F44 (batch
  7, `800d9eed7`)** (ccy 3.124.51): a bare `ccy --teams <team>` / `cc --teams <team>` with no free seat of its
  kind (and the chooser's new-seat row) runs the same add in process (`seatadd.Adder.NewSeat`,
  wired as the chooser's `NewSeat`) and launches the new seat instead of ending 78; a team served
  on another machine is still 78, naming the route ([reports/R5.md](reports/R5.md)). Live proof
  owed.
- **Owner's answer to Q6 (12:42Z, via the coordinator, A9):** a human adding a seat, even with
  sudo, is fine for now. On the home and dc server VMs, seats are managed through IaC first.
  Seats are numbered only, and named seats are dropped. So `seats add` serves a machine where the
  human has sudo (the laptop). Two later ideas, not now: a route that adds a seat over ssh to the
  bus host, and seats an agent requests.
- **Owner, 26 Nov (via the coordinator):** typing a seat number is "a dead UX" (they had been
  given `cc --teams 4@ballicom`). Checked in the code at ccy 3.124.57 and 3.124.72: no number is
  needed. A teamchat team named alone takes the checkout's one free seat of the launcher's kind,
  adds one when there is none, and lists several with the lowest free as the default
  (`chooser/teamchat.go` `teamchatSeat`). The help said only "a team alone asks the seat", so it
  is reworded on v2-smoke for batch 23 (ccy 3.124.80; `docs/ccy.md` too). Not built, waiting
  for the owner: a plain `cc` with no `--teams` joining the checkout's one team by itself.

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
- **Owner's input (F9, 12:39Z):** messages to a human are walls of text. The procedure must say a
  message to a human is short markdown: the point first in bold, then bullets, a status emoji, and
  the ask last. Questions are closed when done and bumped while waiting. lts-infra's wording
  ("The owner, and how they want to be spoken to" in its coordinator doc) is input for the
  generic text.
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
- **Seen again (26 Nov):** the coordinator gets the notice on every prompt. Its posts that day
  were `reply` to the owner's top-level posts, `thread Q<n>`, `thread Q<n> --note` and
  `ask-human`; none named Q0. The reply was sent and placed in the owner's thread, so nothing
  was lost; only the notice is wrong. Why it repeats on every turn is not traced yet.
- **Severity:** medium. **Issue:** TBF. **State:** open (with R7); batch 24.

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
- **What:** `./scripts/qa-changed.bash` fails its Go step before any check on arm64, so no Go
  change passes targeted QA locally there. The units reported ast-grep 0.50.0 pinned for
  linux/amd64 only as the cause; no such pin is in this tree (it would be inside go-qa itself),
  so the cause is unchecked here and is for #426 to establish. Also:
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

## R31: the estate's pin may refuse a commit signed in a container

- **Seen:** this seat, 2026-11-25, while preparing the second batch for the coordinator's re-pin
  (A8).
- **What:** the estate builds teamchat from one pinned fedora-desktop commit and refuses an
  unsigned commit or one signed by an unknown key. Commits made in this container are SSH-signed
  with the container's own key (GitHub verifies them). The commit at today's pin, and almost
  every F44 tip, were signed by a different key. Whether the estate's allowed-signers list holds
  the container key cannot be seen from here.
- **Reproduce:** compare `git log --format=%GF` on the batch commit and on the pinned commit.
- **Severity:** high (blocks every container-made fix from reaching the estate).
  **Issue:** TBF. **State:** confirmed by the coordinator (A9). The estate accepts four keys, and
  this container's key is not one of them, so the pin check and the dev VMs' self-update refuse
  these commits. The owner decides (lts-ops Q10): countersign each batch, trust the key, or trust
  it at home only. Who signs is not changed here. **Resolved for home (A10, the owner, 12:47Z):**
  no countersigning. The home environment trusts this key (the home dev VM's shared key), and
  the datacentre does not, so it stays on its old pin and skips these commits.

## R32: `close Q<n>` waits for stdin it does not need

- **Seen:** the coordinator, 2026-11-25 (F9).
- **What:** `teamchat close Q<n>` run with no stdin waits 60 s, then fails with a usage error.
- **Reproduce:** `teamchat close Q<n>` at a terminal or with an open, silent stdin.
- **Severity:** medium. **Issue:** TBF. **State:** open.

## R33: `ask-human`'s form does not say markdown is rendered

- **Seen:** the coordinator, 2026-11-25 (F9).
- **What:** `ask-human --skeleton` and its checks do not say the questions room renders markdown,
  so questions are sent as plain walls of text.
- **Reproduce:** `teamchat ask-human --skeleton`.
- **Severity:** low. **Issue:** #420 (with R8). **State:** open.

## R34: a human's in-thread answer is delivered with no question

- **Seen:** the coordinator and the owner, all of 2026-11-25 (F12, 12:56Z).
- **What:** the delivery envelope of a human's reply made inside a question's thread says
  `question: none` and `re: -`. `teamchat verify '<id>'` on the same message prints the question
  (for example `"question": "Q8"`) and its headline. Of 17 of the owner's messages checked,
  verify named a question for 15, and every one was delivered as `question: none`. So the
  coordinator treated in-thread answers as unattached: it asked the owner which question a
  reply belonged to, and answered one in the wrong thread. `teamchat questions` also lists two
  of the owner's messages as `unattached`. The owner: "this = the thread im replying to - the fact
  you regard this as ambiguous is a concern that needs logging somewhere".
- **Wanted:** the envelope carries the question number and headline whenever the event is in a
  question's thread, and `reply` answers in that thread.
- **Reproduce:** a human replies in the thread of `Q<n>`; compare the envelope with
  `teamchat verify '<id>' --room questions`.
- **Severity:** high (it misled the coordinator all day). **Issue:** TBF. **State:** fixed by U9,
  merged into `v2-smoke` (b3044e711); live proof owed. One resolver
  (`proto.QuestionIndex.QuestionIn`) now names the question for the delivery, recv, history and
  verify; rule `project_question_unresolved` holds it ([reports/U9.md](reports/U9.md)). The
  `unattached` listing was not reproduced offline: if a live check still shows an in-thread reply
  there, record its id. Same family as R9.

## R37: U7 spelled a liveness word outside its owner

- **Seen:** U9's run of the project rules, 2026-11-25.
- **What:** U7's presence constant spelled `"online"` in the member engine, which
  `project_liveness_word` refuses. Batch 3 would have failed CI on it.
- **State:** fixed on `v2-smoke` (9d521ef27): `matrix.PresenceOnline` is the one spelling.

## R38: the Go lint hook denies a new project rule's fixture

- **Seen:** U9, 2026-11-25 (dogfood).
- **What:** the `lint_on_edit` hook vets `src-go/qa/rules/testdata/src/**` as module packages, so
  every write of an analysistest GOPATH fixture is denied ("package … is not in std").
- **Severity:** low. **Issue:** TBF. **State:** open.

## R41: a seat cannot be added to a running v2 session (regression against v1)

- **Seen:** this seat, reading the code for F18 and F19 (the owner: Ballicom's coordinator also
  becomes worker seat 7 on lts-ops), 2026-11-25.
- **What:** v1's `ccy-teams session add <session> <team>[/<seat>]` adds a seat to a running
  session. In v2 the keeper already re-reads the session's `desired.json` and the engine can add
  a team while running (`member/keeper/seats.go`, `engine/teams.go`). But `session add`
  (`teams/hot`) writes its next generation to v1's `membership.json`, which the v2 keeper never
  reads, and nothing writes a new generation of `desired.json` (the launch always writes
  generation 1). So a v2 session must be relaunched to gain a team.
- **Multi-team itself works** (from the code, not yet run end to end): one link holds every team,
  each delivery names its team, role and `human_text` are per seat, and the questions verbs check
  coordinatorship per team.
- **Interim:** relaunch keeping the conversation: `ccy-teams seats conversation 1@ballicom` while
  the session runs, `/exit`, then `ccy --resume <id> --teams 1@ballicom,7@lts-ops`.
- **Fix:** `ccy-teams session add` writes the next generation of a teamchat session's
  `desired.json` (after the seat is in the pool).
- **Cause, checked (U10):** worse than the row said: a v2 launch makes no v1 membership
  directory, so every `session` verb stopped at `resolve` with exit 78 "launched without the
  agent team bus kit".
- **Fixed (U10):** `session add` and `remove` on a v2 session take or release the seat the way a
  launch does and write the next generation of `desired.json` (`teamchatlaunch/hotseats.go`); add
  waits for the keeper's seat lock and `report.json` at that generation. `--role`, `--park`,
  `--coordinator`, `move`, `role` and `repair` refuse by name for v2. Not yet run live.
  Detail, the class guard and the sibling readers left open: `reports/U10.md`.
- **Review fixes (batch 4):** the desired generation is read under the session's change lock
  (two adds no longer both write N+1); a join whose generation cannot be written releases the
  seat it took; a remove whose seat the store does not hold says so instead of "free"; a
  running cc session on teamchat is refused by name; an unreadable v2 desired is 78; a stale
  refused report is named as an older generation. ccy 3.124.47, container 2.84.78 (with R49).
- **Live check (batch 7 launcher, the devops seat and the coordinator, 25 Nov 18:51-18:57Z):**
  - **Run 1:** an add of a purged team's seat failed at the take (that homeserver is not reached
    from the VM) and put the record back: R59.
  - **Run 2 (team B):** the host side held. Add exited 0, wrote generation 2, and the keeper in the
    container held the seat ("the desired membership generation 2 applied", "seat .../1 held").
    Remove exited 0, wrote generation 3, and released it.
  - **The session never had the team.** The container's network policy admits only its first team's
    bus port, so every request of the link to team B got no answer. `teamchat status` in the session
    did not list team B, and no join notice arrived: R60.
  - So the hot path works, but only to a team the container can reach. It has to be re-run on one.
- **Severity:** high (no-degrade, F19). **Issue:** TBF. **Unit:** U10. **State:** on F44 (batch
  4); host side proven live; session side open behind R60.

## R51: host verbs that read only v1 membership do not see v2 sessions

- **Seen:** U10, sweeping R41's class ("a host verb acting on one bus generation's file for a
  session of the other"), 2026-11-25.
- **What:** these still read only v1's per-session membership, so a v2 session is invisible to
  them: `ccy-teams session list` (`teams/hot/list.go`); `ops.SessionsIn` and `TakeOut` (taking
  a team away leaves running v2 sessions in it); `teams/restart/jobs.go`; the board's
  `addDesired` (`teams/board/collect.go`, `snapshot.go`: v1's desired memberships; the board
  has a `v2.go`, so U11 first checks what it already shows); `internal/conversation`. A host `cc`
  session on teamchat is refused by name by `session` verbs (batch 4 review), not yet changed.
- **Fix:** each reads a v2 session's `desired.json` (and its seats' reports) as R41's add does,
  under the same class guard, `TestEveryVerbOnAV2SessionActsOrRefusesByName`, widened to these
  readers.
- **Seen live (R58, 25 Nov 18:51Z):** `session list` showed a running v2 session as "old or
  --no-bus: relaunch it to change it while it runs", while `session add` on the same session
  said "running, can be hot-controlled". `ownMembership` (`hot/list.go:213-219`) reads only v1's
  `membership.json`; `add` falls through to v2's `desired.json` (`hot.go:480-487`). The picker's
  running-sessions view has the same gap. Fix: one lookup, v1 else v2, shared by list, add,
  `SessionsIn` and the picker.
- **Fixed (batch 10):** one lookup, `hot.Lookup.Running` (v1's membership if the session has a
  v1 directory, else its teamchat `desired.json`), used by `session add`/`remove` (resolve),
  `session list` and the picker's view (`Sessions`), `SessionsIn`, `ccy-teams restart` and the
  cc-on-teamchat listing. A v2 row shows teams, seats and duties and a state from
  `HotSeats.Applied`, never `keeperLine`. `SessionsIn` counts v1 sessions only, since its take-down
  is of a v1 team. Restart refuses a v2 session by name, and so does the picker's add (it offers
  remove). The board and `conversation` were already v2-aware. The class guard
  `TestEveryVerbOnAV2SessionActsOrRefusesByName` now also drives the readers and holds an
  inventory of every v1-membership read outside the lookup, so a new one fails it. ccy 3.124.56.
  Detail: `reports/R51.md`.
- **Severity:** high (no-degrade, F19: v1's tools saw every session). **Issue:** TBF.
  **Unit:** U11. **State:** on F44 (batch 10, `356506aee`, ccy 3.124.56); not yet run live.

## R42: `say --to-coordinator` is ambiguous in a session that coordinates one of its teams

- **Seen:** the same reading, 2026-11-25.
- **What:** with two teams and no `--team`, `--to-coordinator` takes each team's coordinator and
  never leaves out the sender (`engine/send.go`, `resolve`), so a session that coordinates team A
  and works on team B always gets the ambiguous-team refusal, though only B's coordinator is
  someone else. `--team <B>` works.
- **Fix:** a team whose coordinator is this session is not a candidate for `--to-coordinator`;
  with `--team` naming that team, the refusal (64) says this member is its coordinator and to
  use `--to`. `reply --to-coordinator` already left the session out.
- **Severity:** medium. **Issue:** TBF. **Unit:** U10. **State:** fixed on `v2-smoke`
  (cdac15584, red then green); lands with batch 4.

## R40: a `ci/<topic>` push of a commit GitHub already has runs no CI

- **Seen:** this seat, batch 3, 2026-11-25 (dogfood: the CI recipe in `CLAUDE.md`).
- **What:** three `ci/<topic>` branches made at the `v2-smoke` tip, which was already pushed,
  started no QA run and said nothing. A new branch whose commits GitHub already holds carries no
  new commits, and the QA workflow's `paths` filter then sees no changed file and skips it. A
  fast-forward of an existing `ci/<topic>` branch (`abc019487..1d9f38cb2`) started run
  45625876657 at once.
- **Hazard:** the recipe (`git push origin HEAD:refs/heads/ci/<topic>`, then wait for green) waits
  for ever on a run that will never exist, and an agent may read the silence as GitHub trouble.
- **Fix direction:** the push gate or the recipe names this case: push the `ci/<topic>` branch
  before the work branch, or create it at the protected tip and fast-forward it.
- **Severity:** medium. **Issue:** TBF. **State:** open; workaround known.

## R43: one back-merge without a version bump stops a work branch landing by ancestry

- **Seen:** this seat, batch 3's CI run 45625876657 ("QA (diff, rest)"), 2026-11-25 (dogfood).
- **What:** `scripts/qa-ccy-merge-commits.bash` judges every merge commit in a run's range
  against its parent on the protected line. `8a3b4af01` (F44 merged into `v2-smoke` for R23)
  carried `v2-smoke`'s teamchat changes at the same `CCY_VERSION` as that F44 parent, so it is
  refused. The commit is history and cannot be fixed without a force-push, so every later push of
  `v2-smoke`'s ancestry fails the same way, although the tip's version (3.124.46) is above F44's.
- **Workaround used:** batch 3 lands as one signed commit on F44 with `v2-smoke`'s tree
  (`732effcb3`), as batches 1 and 2 did. The cost is ancestry: the unit branches do not show as
  merged into F44.
- **Fix direction:** bump `CCY_VERSION` in the work branch before merging the protected line into
  it, or have the gate judge only the merges a push adds to the protected line.
- **Severity:** medium. **Issue:** TBF. **State:** open; workaround known.

## R44: the standard coordinator procedure ships with the team tooling

- **Seen:** the owner, lts-ops questions room 13:55Z (F20): "this needs to be standardised and
  the ballicom team coordinator needs the same instructions".
- **What:** a team's coordinator learns its procedure from a hand-over note in its own
  repository; another team's coordinator has none. The infrastructure repository has written one
  standard procedure with no project facts in it, plus a project-layer template.
- **Where it will live:** a second skill in the teamchat plugin the kit already ships,
  `files/opt/claude-yolo/optional/teamchat/plugin/teamchat/skills/coordinator/SKILL.md`, beside
  the `teamchat` skill.
- **How a coordinator will find it:** the SessionStart frame's coordinator line
  (`member/hooktext/hooktext.go`, `tmplRoleCoordinator`; today it names no skill) will name the
  skill, so it reaches only the seats that coordinate a team, at every launch. A project adds its layer in its own repository.
- **Upkeep:** every `teamchat` verb and flag the skill names is checked against the code when
  it lands. A test will hold the skill's verbs to the CLI's verb table, so a renamed verb fails
  QA.
- **Severity:** medium. **Issue:** TBF. **State:** on F44 (batch 6, `b58317073`; CCY 3.124.49,
  container 2.84.80 with the review fixes): the kit ships `teamchat:coordinator` (source `member/kit/coordinator.txt`, image copy
  held equal by test), the coordinator frame line names it, and a test parses every command it
  names. Ten command corrections against the source are listed in
  [reports/R44.md](reports/R44.md). Live once the image is rebuilt on the host; the delivery
  note's U2, U5 and U6 (project-layer path, whoami listing, membership notice) stay open.

## R39: a seat starts on the account's default model

- **Seen:** the owner (laptop host seat) and the hooks-daemon seat, 2026-11-25 (F14).
- **What:** a seat comes up on the default model (Sonnet) and the owner switches it by hand.
  Neither launcher passes a model: no `--model` or `ANTHROPIC_MODEL` in `ccy`, `cc` or the Go
  launch code, so claude takes the account's default on every route.
- **Wanted:** a default model per team with a per-seat override, given to the seat at launch
  with no human step. A model the person names on the command line wins. It sets the model a
  seat starts on, not a minimum: it neither compares models nor refuses a launch (the deferred
  model-class refusal stays in Non-Goals).
- **Where it lives:** the per-machine seat store, `<seat store>/models.json` (0600, on no
  mount), not the team's declaration: a model set on one machine does not follow the team to
  another. Read by `Launch.Take` (`internal/teams/teamchatlaunch`) for both launchers, as
  `ANTHROPIC_MODEL`, not `--model`, and not in `ccy-start-claude` or `cc-claude`: departure in
  [reports/R39.md](reports/R39.md).
- **Built:** `ccy-teams seats model <team> <model>|--clear` (team default) and
  `ccy-teams seats model [--host] <seat>@<team> <model>|--clear` (a seat's own, which wins; the
  seat found in the checkout's pool by kind, kept by handle); no argument lists them.
  `launch-args` adds `-e ANTHROPIC_MODEL=<model>` to podman's arguments, so a host-shell
  `ANTHROPIC_MODEL` does not reach a ccy container, only the seat's recorded model; `cc-run`
  puts it in the session's environment unless the person's has one. Both say it on stderr.
  claude is taken to rank a `--model` above `ANTHROPIC_MODEL`, so the person's wins with no
  argument parsing, and a project `ccy.env` to win too (sourced after the image ENV; that it
  also comes after `podman run -e` is unchecked): both are the live check after deploy. Child claudes the session starts
  inherit `ANTHROPIC_MODEL`. With several seats, the first seat with a model decides: its own,
  else its team's (`seatmodel.Settings.For`). A hot-seat join (`ccy-teams session add`) launches
  nothing, so the seat's model is not used; `HotSeats.Join` says so on stderr before the take.
  Not the image (`internal/teams` is no image source) and not the bash launcher.
- **Restore (checked):** a restore replays the person's recorded launcher arguments
  (`lib/session-registry.bash`; a `--model opus` is kept as an unknown flag's value), never the
  model a launch adds, and the replayed launch takes its seats again, so it re-reads the same
  setting and starts on the same model.
- **Severity:** medium. **Issue:** TBF. **State:** on F44 (batch 8, `10a63a7f0`, CCY 3.124.52,
  container 2.84.80 unchanged); live after `play-claude-yolo.yml` rebuilds `ccy-teams`, already
  queued. Live check: a seat with a model starts on it; `--model` on the command line wins.
  The coordinator reads the `ANTHROPIC_MODEL` half at a restore on their dev VM (25 Nov). The
  `--model` half has no route there but a launch by hand: `declare` records no extra `--model`.

## R35: the Lightning client setup for a team, with presets

- **Seen:** the owner, lts-ops Q2 and 12:53Z (F13).
- **What:** the owner's boundary ruling puts setting up Lightning for a team in fedora-desktop.
  Today that setup is a script in the team's own infrastructure repository. The owner asks
  whether Lightning can be preconfigured when it is set up for a team. The first preset is the
  spaces rail hidden (R20, F11).
- **Next:** move that setup into `agent-bus-lightning` (`internal/lightning`) with a per-team
  preset of client settings. What Lightning lets us set is to be read from a real client config
  by the team's host-level seat on the owner's laptop.
- **What we write (F15, read from `internal/lightning`):** one instance per team under the
  Lightning Flatpak's data (`~/.var/app/<app_id>/teams/<team>/{data,config,cache}`), one
  settings file per instance, `config/MatrixClient/matrix-client.conf` (an INI file:
  `[homeserver]` `url=`), written only when absent, after which Lightning rewrites and owns it
  (L7); and a launcher per team. The team script in the infrastructure repository writes no
  Lightning settings. No spaces-rail key is in what we write.
- **The key (F16, from upstream source, `lightning-matrix/lightning` at 05d0be9c568b):** the rail is
  `shell/spacesRailVisible` (`src/app/SettingsManager.cpp`, `kSpacesRailVisible`; a bool, default
  true, in `src/settings/SettingsSchemaTable.inc`). Lightning's settings are a default `QSettings`
  with organisation `MatrixClient` and application `matrix-client` (`src/main.cpp`), so on Linux
  it is this same file, section `[shell]`, line `spacesRailVisible=false`. Unchecked: that the
  installed Flatpak's version uses the same key. A preset therefore means merging that one key
  into each team's file, which changes L7 ("only when absent"). The laptop cannot read the file
  (its secret guard covers the Flatpak's data, by design), so no live comparison is needed.
- **The rail preset and settings (F21):** it ships with the workaround stated; it does not wait for
  R45. With the rail hidden, the settings cog and account menu move to the room list's full-width
  header (upstream #45); only a list also collapsed to its narrow width loses them, and Ctrl+,
  (or Ctrl+K, "Open Settings") opens Settings in every layout. The preset's note says so.
- **The live file (F29, the laptop host seat, from a key-names-only listing; no values seen):**
  confirms F16 on the installed client. `spacesRailVisible` sits in the top-level `[shell]`
  section, not per account, and only in the two instances where the owner unticked the rail;
  the default instance has `[shell] spacesRailWidth` and an untouched team instance has neither.
  An untouched team instance's file holds only `[homeserver] url`, the line our setup writes
  before first launch, so the preset is one more key in a file we already write.
- **Unchecked:** the value's spelling (the name says boolean), and whether Lightning keeps a
  preset value at first launch rather than resetting it. One test settles both: preset it in a
  throwaway instance, launch, look at the rail.
- **Severity:** low (after the container work). **Issue:** TBF. **State:** open.

## R36: an earlier #417 implementation sits on unmerged branches

- **Seen:** this seat, 2026-11-25, while clearing the branch pool.
- **What:** three branches from an earlier session (all on origin; only `agent-a1aeb314…` is
  also local) (2026-11-24; `agent-a2be5458…`,
  `agent-adff76a1…`, `agent-a1aeb314…`) hold another implementation of #417 (ccy 3.125.0 to
  3.125.5) that never reached F44. U4 built #417 again on `v2-smoke`. Those branches also hold
  work U4 may not have: "a team taken down forgets its seats' conversation pointers", a
  first-launch `--fresh` hint, and a project rule `project_temp_not_exclusive` (temporaries
  created without O_EXCL) with its sweep and the `fsx.WriteAtomic` fix.
- **Next:** compare them with U4, carry over what U4 lacks (the rule and its sweep first), then
  retire the branches. They are not deleted until that is done.
- **Compared (25-26 Nov, untracked/scratch/r36-compare.md):** a2be… is inside adff…, which is
  inside a1ae… (9 commits; before this carry-over none was in F44 or v2-smoke by `git cherry`). U4 covers the resume,
  `--fresh`/`--rebind`, `--set` and the selector reader. Missing: (c) the rule, its sweep and the
  `fsx` fix; (a) `ccy-teams remove` keeps a removed team's seat records; (b) the `--fresh` hints
  (`declare.py`, `queries.go`, `chooser/list.go`, `StartCcy`) and a named new seat still exit 64.
  U4's records have another layout, so (a) and (b) are rewritten, not picked. Owner's call: the
  old branch's lock on seat-record writes (4001e4789), which U4 lacks.
- **Carried over (c), on v2-smoke for the next batch:** the rule (fc14af2ed; red: the same 5
  sites on today's tree: untagged, busfailpoints, tuwunel, ccyteamstest, difftest), the sweep (c91c9f125) and the `fsx` fix (18bfa02d3;
  `TestWriteAtomicFollowsNoPlantedLink` red on the old code, green on the fix); ccy 3.124.68,
  container 2.84.88.
- **(a) rewritten for U4's layout, on v2-smoke:** `ccy-teams remove`, once the team is down,
  removes `.claude/ccy/seat-conversations/<team>/` in every checkout of the seat-checkouts list
  and of the running sessions (`conversation.ForgetTeam`; a link in its place is refused), and
  fails naming what it could not remove; ccy 3.124.69. Unchecked gap: a teamchat checkout whose
  session has ended and that never entered the seat-checkouts list keeps its records.
- **(b) rewritten for U4's layout, on v2-smoke:** ccy-teams' Start a ccy session
  (`Seats.StartCcy`) adds `--fresh` when `conversation.NeverUsed` says the seats were never used
  in the checkout (ForSeats' no-session case: no seat record, no session that held them), and
  starts nothing on a record it cannot read; Plan 00203's M3 and M4 acceptance launches (new
  seats of teams the run creates) pass `--fresh`; docs/agent-bus.md and docs/ccy.md say a named
  seat's first launch says `--fresh`; ccy 3.124.70. Not changed, on purpose: the next steps of
  `declare.py` and `queries.go`, the chooser's two "name it" lines and Plan 00232's advice give a
  team name or a bare `--teams`, whose picked never-used seat already starts fresh (#423,
  `--given`); a `--fresh` there would drop a picked seat's recorded conversation.
- **Independent review of the R36 commits (26 Nov 01:37Z), FIX-BEFORE-MERGE; its findings:**
  (2) `ForgetTeam` followed a link above the team's directory (`.claude`, `.claude/ccy` or
  `seat-conversations`), so `ccy-teams remove` could delete outside the checkout: fixed, the
  removal goes through an `os.Root` on the checkout and every directory from `.claude` down must
  be a real one (red on all three links, green after); ccy 3.124.71. (4) the wiring was untested:
  a test now holds that `Wire` sets `Ops.ForgetTeam` and that it removes a record. (5) Plan
  00232's and Plan 00161 U20's named-seat acceptance launches pass `--fresh` (a sweep of the
  active plan scripts finds no other). (6) docs/agent-bus.md's take-down step names the removal.
  (1) the class `project_temp_not_exclusive` names, outside Go: rule `temp-not-exclusive`
  (`.semgrep/temp-exclusive.yml`; shell: a pid-named temporary written or moved by mv; Python:
  a non-exclusive create the same function renames), red on 13 lines in 11 files plus
  `ccy_lifecycle.py` and `pingbus/notify.py`, each fixed with `mktemp`/`tempfile.mkstemp` (or
  `O_EXCL`); gate `scripts/qa-temp-exclusive.bash` (ghscopes engine, `TempExclusive` spec),
  blocking in `qa-all` (`qa-gates.tsv` row `temp-not-exclusive`) and `qa-changed` (check
  `temp-exclusive`, SPEC-qa-changed B69), CLAUDE/QA.md "temp-not-exclusive"; ccy 3.124.72,
  container 2.84.89. Fixed: commits 6633dc113, 4bb754029, 94a6dd0b2 and the wiring commit after
  them. (a) widening the shell rule to fixed suffixes (`"$f.tmp"` then `mv`): decided and
  done, below; (b) the daemon-deployed
  `.claude/ccy/claude-supervise.py` (6 pid-named temporaries and 1 with a fixed suffix) and `.claude/provision.sh` (2), which every gate
  excludes, to report upstream. (3) checked in the code, not yet run: when none of the named
  seats has a seat record, `ForSeats` falls back to the sessions: on an agent-bus checkout each
  session's keeper report (`v1Sessions`), on teamchat each session's desired membership
  (`v2Sessions`, `conversation/seats.go`). `ccy-teams remove` takes down an agent-bus team and
  takes only its running sessions off it (`sessionsOff`, H29; `session remove` refuses a stopped
  session). A stopped session's own record still lists the team, and its keeper report still
  names the seat. So after `remove` and a new team of the same name, a plain
  `ccy --teams <seat>@<team>` can resume the old team's conversation through such a session,
  and restoring that session rejoins the new team.
- **Decided by the coordinator (26 Nov):** (3) is fixed by (a): remove also takes stopped
  sessions off the team, with a red-then-green test of remove, a new team of the same name and a
  plain launch. The fixed-suffix widening is done under the same rule. The old branch's lock is
  kept, not an owner question. All three go in the batch after 13.
- **Lock ported (on v2-smoke, after batch 13):** a launch writes its seats' records under
  `flock` on `.claude/ccy/seat-conversations/.lock`, and `ForgetTeam` takes the same lock
  through its `os.Root`. The tests were red, because neither took the lock, and are green now
  (ccy 3.124.73). **Corrected (batch review, finding 4):** the commit says the lock is never
  reached through a link. Only the lock file itself is opened `O_NOFOLLOW`. The launch reaches
  the directories above it, and writes each seat record, by plain path, as before the port. So a
  container that links `.claude/ccy/seat-conversations` elsewhere makes the host's launch write
  records there. **Fixed (the coordinator's call, this batch):** the launch now reaches the lock
  and every record through the checkout as an `os.Root`. Each record is written to an
  exclusively created temporary renamed into place, so a linked seat-records or team directory
  is refused. The test (both links) was red, records written outside, and is green now (ccy
  3.124.76).
- **Re-reviewed (qa-reviewer, the five fixes):** pass with one should-fix. When `remove` read
  the team's sessions again after taking them off and the read failed, the error was dropped,
  so the person saw no session and no cause. The error is now printed. The test was red and is
  green now (ccy 3.124.77).
- **Reviewed (qa-reviewer, batch 22 `c5f27341`): fix before merge**
  ([report](subagent-reports/261126-qa-reviewer-batch22.md)). All four findings are fixed (ccy
  3.124.78):
  - (1) The coordinator's test of (3) was missing. A test now runs the plain launch's seat
    lookup (`ForSeats`). It finds the stopped session's conversation before the session is taken
    off, and no session after. It was written after the fix, so it guards the fix; it was never
    seen red.
  - (2) The lock wait is now bounded: after 30 s it fails naming the lock (red: a hung `flock`).
  - (3) Widened. An `os.Root` ignores `O_NOFOLLOW` on the last name and follows a relative link
    that stays inside the checkout, so neither writer refused a linked lock, the launch
    included. Finding 4's "the lock uses `O_NOFOLLOW`" held for absolute links only. Both
    writers now open the lock through one opener, which refuses any lock that is not the plain
    file at that name. The test was red on both writers.
  - (4) A stopped session running again when the sessions are taken off is left as it is and
    named (red: it was taken off).
- **Re-reviewed (qa-reviewer, batch 22b `842294f2`): fix before merge**
  ([report](subagent-reports/261126-qa-reviewer-batch22b.md)). Findings (1) to (4) are closed.
  The new wait broke `project_realtime_bound`, because `realLockClock.Sleep` had no `realClocks`
  row. The row is added, as the coordinator chose, and this is the batch's last change. Nit for
  the next batch: a dangling relative link at the lock gets its target created inside the
  checkout before the open is refused.
- **Batch 22c `bf9ec613` (ci/v2-batch13): ready, held off F44.** It adds the `realClocks` row.
  The reviewer's spot-check of that row passed
  ([report](subagent-reports/261126-qa-reviewer-batch22c-spotcheck.md)), and CI run 45773214021 is green. It
  fast-forwards from F44 and stays off it until the coordinator says.
- **The dangling-link nit, fixed as a class (on v2-smoke, for batch 23):** rule
  `project_root_create_follows_link` (an `os.Root` create without `O_EXCL` follows a link at the
  last name, `O_NOFOLLOW` or not; both forms checked on Go 1.26 first). Red: the fixture (6
  findings, all missed with the analyzer reporting nothing) and the tree, 3 findings. Swept:
  `bashlimits`' allowlist rewrite (it wrote through a linked allowlist: red, now an exclusive
  temporary renamed over the name) and `semgrepreport`'s self-test copy (an `os.MkdirTemp`
  directory, now `O_EXCL`). Fixed: the lock is created exclusively and an existing one opened
  without create (red: the planted target was made; ccy 3.124.79).
- **(3) fixed (on v2-smoke, after batch 13):** `remove` asks, while the team is still up and
  once its running sessions are off it, which stopped sessions name it on the agent bus (each
  record's system read as a restore reads it, `conversation.SystemOfRecord`). Once the team is
  down it takes the team out of those records, keeping their other teams, and drops their
  keeper reports, before it forgets the seat records. A teamchat team of the same name is left
  alone. Tests: the choice, the taking off, the order and refusals in `remove`, and the wiring.
  Each was red first and is green now (ccy 3.124.74).
- **Widened (on v2-smoke, after batch 13):** the shell rule also finds a fixed-suffix temporary
  (`.tmp`, `.new`, `.part`, `.temp`) that a redirect writes and an `mv` within 40 lines moves,
  written straight or held in a variable. The fixture's new cases were red on the old rule (5
  missed lines) and are green now. The sweep found 12 lines in 9 files: plan, test and vmtest
  scripts, a test's fake clipboard, and the UniFi play, whose temporary takes the properties
  file's mode. Each now uses `mktemp` beside its target, and the gate is clean.
- **Batch review (qa-reviewer, 26 Nov, fix before merge), on v2-smoke:**
  - (1) remove deleted a stopped session's keeper report by a path that follows links. It now
    deletes inside the checkout only (`os.Root`), before the record is rewritten.
  - (2) a stopped session that could not be taken off skipped the forgetting, and a second
    remove is refused. Now every session is tried, the seat records are always forgotten, and
    the failure names each session left, saying what a restore of it would do.
  - (3) a running session whose membership could not be read was skipped, then taken for a
    stopped one. remove now refuses, naming it.
  - (4) the lock's link claim did not hold; the launch's writes are now confined (above).
  - (5) two suites without `set -e` now stop when `mktemp` fails.
  - Each code fix was red first and is green now.
- **Severity:** medium (duplicate work, and a QA rule not landed). **Issue:** #417. **State:**
  the parts through ccy 3.124.72 (fsx's exclusive temporary, remove forgetting seat records and
  never following a link above them, `--fresh` for an unused seat, rule `temp-not-exclusive`)
  are on F44 (`e67e28d72`, 26 Nov). The lock port, (3), the widening and the review fixes
  (ccy 3.124.73 to 3.124.77) are on v2-smoke for the next batch; (b) is for the daemon's upstream.
  The owner answered Q14: delete ci/v2-batch3, 3b and 3c (and 3d, which has the same tip) and
  the three old agent branches, through the guard. The coordinator's logged verdict (26 Nov):
  `agent-a1aeb314…` has no value, since every feature it holds was rebuilt on v2-smoke. The
  other two are ancestors of it. No open pull request or review comment holds any of them
  (`gh pr list`, checked). The guard proves neither path yet: v2-smoke is not a declared
  integration line, and batches reach F44 as new commits. So the deletion waits for the hooks
  seat's two guard paths (contained, and the logged no-value retire), dry run first. The same
  verdict covers v2-smoke-u1, u2, u4, u5 and u7 (each differs from v2-smoke only by an empty
  park commit), u3, u6, u8 and u9 (ancestors of v2-smoke), and private-names-no-first-word
  (on F44 as c219e89ce). The clean detached `untracked/wt-b6` waits too: the guard refuses a
  HEAD no ref holds, though its patch is in v2-smoke. One named hold stays:
  `lightning-default-client` (Plan 00254 Task 7.1, Lightning as the default Matrix client),
  un-parked when v1 is retired, because Lightning is then the only human surface. #325
  (`photography-idempotent`) and the end of #324 (`restore-drill`) go in batch 24.

## R49: a worker's failed questions join was reported as "event not found"

- **Seen:** the devops seat, Task 4.11 (lts-ops), 14:40Z, holding a write on owner words it
  could not verify (F25).
- **What:** a worker's `verify` of a questions post joins questions by its invite when refused
  (`teamsync/verbs.go`, `getQuestionsEvent`). When that join failed, the cause went only to
  `link.log` and the verb answered 23 not-found "not in the team's main room, nor in a questions
  room this member reads", which reads as "workers cannot verify".
- **The live 23 is R12, not this:** this seat, on the same code (digest `2f5c69f0…` is the src-go
  git tree of ffa5e86c8), reproduced it on both owner ids (F26), with and without `--room questions`. At ffa5e86c8 `getQuestionsEvent` joins only on a 403, and Tuwunel answers a reader
  who is not joined 404 M_NOT_FOUND (`src/api/client/room/event.rs:68-75` at tuwunel f7dc0770), so
  no join was ever tried (hence no log line) and a worker holding only an invite cannot see the
  event under `shared` (`user_can.rs:402-437`). U6 (5309c8de1, R12) joins on 404 too and made the
  fake answer as Tuwunel does; it reaches sessions with batch 3 and their restart. Report:
  `untracked/scratch/r49-rootcause.md` (this seat).
- **Fix:** a failed join is now refused forbidden (exit 22, tell a human), naming the join and
  `link.log`; not-found again means only that no readable event has the id. **Severity:** medium.
  **Issue:** TBF. **State:** fixed on `v2-smoke`; batch 4. `teamsync` is an image source, so it
  ships in container 2.84.78 (ccy 3.124.47), which batch 4's first commits lacked (review 1).

## R48: the off-host route demanded a dedicated agent user (uid and groups)

- **Seen:** the owner, lts-ops Q8, 14:31Z (F22): "we're still blocked - the whole uid thing is
  misguided".
- **What:** K15 (a guest uid that is a host account's is refused) is enforced only by the
  homeserver host's own `lxc apply` and its absent phase (`guestfacts.go:157`, given that host's
  passwd by `apply.go:829`, `903` and `absent.go:371`). Off-host nothing reads any host's accounts, yet our doc
  (`agent-bus-lxc-members.md`, off-host section) and route-to-live L7 asked for "a uid no
  account on the container's host has". What `seat place-lxc` did enforce off-host was GU1's
  caller check, which also refused any supplementary group (`identity.go`, `ownGroupOnly`): a
  container's usual login (wheel, sudo, docker) was refused 78 identity, protecting nothing,
  since the seat files are created 0600 and 0700 under a home only the user can write.
- **Fix:** `guest.EstablishOffHost` (GU1 without the group rule) for `seat place-lxc` only; the
  on-host guest ops keep `Establish`. The doc's off-host requirement is "not root, a home only it
  can write"; L7 dropped. On-host K15 is unchanged (it does not touch this route).
- **Severity:** high (blocked the container route, P1). **Issue:** #425 (A1). **State:** fixed
  on F44 (batch 3, 9d37999ce).

## R52: the git hooks run on one checkout only

- **Seen:** U10's review fixes, 2026-11-25: batch 4 changed an image source with no CCY bump,
  and no pre-commit gate stopped it.
- **What:** `play-git-hooks-security.yml` sets `core.hooksPath` on the `repo_path` checkout
  alone. This seat's checkout has it unset and only git's `*.sample` files in `.git/hooks`, so pre-commit (secret,
  private-name and version gates), commit-msg and pre-push (the protected-branch gate) never
  ran here. `SecurityRules.md`'s "Enforced by Ansible on every main playbook run" holds for that
  one checkout.
- **Interim:** the review caught the missing bump, CI caught the coverage; this seat runs
  `scripts/git-hooks/pre-commit` by hand before committing. No manual `git config` (IaC only).
- **Severity:** high (every gate a commit is meant to pass locally is silently absent).
  **Issue:** #427. **State:** open, outside v2.

## R68: a coordinator's `reply` to an older questions-room post is refused "no item in main"

- **Seen:** a coordinator seat (team A), 25 Nov, after a relaunch: `teamchat reply '<id>'` on two
  older owner posts in questions ended 23, "team <t> has no item <id> in main"; the seat asked
  afresh with `ask-human`. The same refusal earlier that day drove a `thread Q<n> --note`
  fallback. The documented procedure answers a person with `reply`.
- **What (read in code, red test):** `reply` took the room from the seat's store; for an id the
  store did not hold, `fetched` re-fetched it from main only, so a questions post read as a 404.
  The store is persisted, not per launch; why these posts were not in it is unchecked (the
  1024-item ledger, or ids taken from `questions`/`history`). The code departed from SPEC-n3f
  QR1, which says the coordinator looks in questions after main.
- **Fix:** the coordinator of a team with a questions room looks there when main answers 404,
  and answers in the post's thread; an id in neither room is 23 naming both; a worker never
  reads questions. The engine's fake `Refetch` now keeps each fetch's room. No rule: the
  sweep found one instance, and the wrong and right sites share a shape
  ([R68](reports/R68.md)).
- **Severity:** high (breaks the documented answer path). **Issue:** TBF. **State:** on F44
  (`e67e28d72`, 26 Nov; batch 20, `f59504a18`); not yet run live. Seen again on 26 Nov
  ~08:30Z: the coordinator, relaunched, got exit 23 "no item <id> in main" replying to two
  questions-room posts. That is the pre-fix behaviour, so its session runs a kit older than
  `e67e28d72`. The live proof waits on its image being rebuilt there.

## R67: the status-line golden test reports a process that died as it was killed as outliving the render

- **Seen:** CI run 45703604400 ("QA (diff, go)"), 25 Nov 21:32Z: `TestStatusLineGolden`'s
  `cancel-sighup` and `cancel-sigint` failed with `kill <pid>: no such process` and
  `grandchild.pid: process <pid> outlived the render`; a re-run passed.
- **What:** `goneOne` polled the grandchild (already sent SIGKILL by the render's group kill) for
  250 checks, then sent SIGKILL itself and reported "outlived" whatever the kill answered. The
  process ended between the last check and the kill: ESRCH, so it never outlived anything, yet
  the step drew two failures. The 5 s bound had also run out on a loaded runner.
- **Fix:** `outlives` takes its probes (ended, kill, pause) as fields; a kill answered ESRCH is no
  problem, any other refused kill is still an error. The bound is 1000 checks (at least 20 s,
  well short of the grandchild's 60 s sleep); a passing step waits only until the process ends.
  `TestOutlivesDecision` was red on the old decision. The sweep found no other instance
  ([R67](reports/R67.md)).
- **Severity:** low (a false red on an unrelated batch). **Issue:** TBF. **State:** on F44
  (`e67e28d72`, 26 Nov; batch 18, 6b300c2a0).

## R66: the edit-time lint hook lints the project-rule fixtures, which carry deliberate findings

- **Seen:** the R62 agent, 25 Nov.
- **What:** the hooks-daemon `lint_on_edit` handler runs golangci-lint on
  `src-go/qa/rules/testdata/**` and blocks every edit to those fixtures, which can never pass by
  design (each carries the findings its rule must report).
- **Fix:** exclude the analysistest testdata tree from that handler in the project's hooks-daemon
  configuration, or have the handler skip analysistest testdata.
- **Severity:** low. **Issue:** TBF. **State:** open, tooling (dogfood).

## R65: a leftover empty v1 membership hides a teamchat session from the host lookup

- **Seen:** the team's `ccy-teams session list` on its dev VM after batch 11, 25 Nov 21:26Z: a
  running ccy session launched on teamchat (`--teams 4@<team>`) listed with teams `-` and state
  `unguarded: ccy-teams session repair <session>`.
- **What:** the session had first been restored at boot with no `--teams`. A no-team launch
  writes an empty v1 membership (`$XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json`,
  `seats:[]`), which a no-team session needs to stay addable. Relaunched on teamchat in the same
  boot, the v2 launch (`teamchatlaunch.launchArgs`) never removed it, and `Lookup.Running`
  (`hot/running.go`) read v1 whenever that directory existed, never v2. So list, add, remove,
  `SessionsIn`, restart and the picker all read the session as an empty v1 one, and the printed
  `repair` would have started a v1 seat keeper (`pingbus seat keep`) in a v2 session.
- **Fix:** a teamchat launch (ccy `launch-args` and cc `cc-run`) removes the name's v1
  membership directory once its take has succeeded (a refused take keeps it; a no-team launch
  still writes its empty one). `Lookup.Running`, with both files on disk, takes the one written
  later (v1 `written_ms` against v2 `written_ms`), which fixes a session already running without
  a relaunch. Repair on such a session is now refused by name as a v2 verb. Report:
  [reports/R65.md](reports/R65.md).
- **Severity:** high (the printed repair hint does harm). **Issue:** TBF. **State:** on F44
  (`e67e28d72`, 26 Nov; batch 17, `76d9bf5f0`); not yet run live. Live check: the team confirms after its reboot
  that the directory does not come back and the session lists its teams.

## R64: a link restart re-judges the questions room and rate-drops the owner's posts

- **Seen:** the coordinator's `teamchat status` after its 18:48Z relaunch, 25 Nov:
  `"dropped_by":{"rate":45}`, and link.log lines "teamsync lts-ops: dropped $<id>: rate".
  The coordinator re-fetched all 45 with `teamchat verify`. Every one was a HUMAN post by the
  owner in the questions room, sent 12:42-16:47Z, all already shown before the relaunch. So
  nothing was lost this time.
- **What (read in code):**
  - On a link start the questions room is read from its start or its stored token, by design
    (`teamsync/builder.go:17, 96-103`).
  - Every event is judged before the floor check that skips one already delivered
    (`qjudge`, `teamsync/questions.go:518-548`, calls `judgeRoom` at 533, then `qtake`, 566-595,
    whose floor skip is at 579). That check skips only a Deliver verdict, so
    a `rate` drop inside the floor is still taken and counted (`session.go:928-929, 958-964`).
  - The judge applies the per-sender receive limit (`recv_per_sender_minute`, 60 a minute) to a
    human, with no stale test in questions (`proto/verdict.go:368-372, 402`). The window counts
    the judging time, not the sending time (`session.go:871`, `proto/limits.go:94, 225-235`), so
    the whole replay lands in one minute.
  - "Seen" holds only ids still pending or in the ledger (`member/engine/storewrite.go:155-163`;
    the ledger, not Pending, is capped at 1024, `capLedger`, `member/state/store.go:30`), so posts
    delivered hours ago are judged again.
  - A dropped event is not fetched again: the position is saved past it (`session.go:595`), and
    the questions floor is raised to the newest judged event (`questions.go:588-592`). A NEW
    owner post in that burst would be lost for good.
- **Fix:** an event that is history the link reads again (the questions floor covers it; in main,
  the marker covers it or it is restored) is judged against a window of its own, thrown away, so
  replayed history never counts toward the flood window (`judgeRoom`'s `replayed`). Main has the
  same order after a seat's release (`resume` keeps events past the release time under the
  marker) and is fixed the same way. Tests in `member/teamsync/replay_flood_test.go`.
- **Decided (the team coordinator, 25 Nov 21:21Z, on the owner's standing rule; reversible):** a
  team human is exempt from `recv_per_sender_minute` in main and in questions (A 11.1 now says
  so). A post past the limit is delivered, counted in `status`'s `counts.human_over_limit` and
  logged with one line naming it. Report: [reports/R64.md](reports/R64.md).
- **Severity:** high (the owner's words can be dropped for good). **Issue:** TBF. **State:**
  on F44 (`e67e28d72`, 26 Nov): batch 13 (544ebda3d) and the human exemption, batch 16
  (`d9a4fcc80`); not yet run live.

## R63: `declare` fails a converge while a declared session runs with a record not its own

- **Seen:** the coordinator's question on batch 11 (25 Nov 20:36Z), answered from the code.
- **What:** `ccy-sessions declare` (`sessions/declare.go`, `declareInto`) answers UNCHANGED (rc 0)
  only when the record under the name equals the declared one. When it differs and a tmux session
  of that name runs, it ends rc 1 ("is running, and its record is its launch's"). A provisioning
  play that declares every session on every converge then fails, or must match stderr. Two ways
  in: the inventory changes a running session's `--teams`, which has failed since `d5e536a44`;
  and, since R61 (batch 11), the owner starts a stopped declared session from a terminal, so its
  record becomes the launch's.
- **Fix (the coordinator's choice, option c):** in that case `declare` answers rc 0 with a
  verdict of its own, `<name> DEFERRED`. The line says the record is written when `declare` runs
  again after the session stops. Nothing is written while the session runs. A record that differs
  while the session is stopped is still refused without `--replace`.
- **Severity:** medium (a play's converge). **Issue:** TBF. **State:** on F44 (`e67e28d72`, 26 Nov;
  batch 14, `f1e50d031`); not yet run live. Report: [reports/R63.md](reports/R63.md).

## R62: `project_walk_open` does not take a regular-file check written as a switch case

- **Seen:** this seat, batch 10's CI (25 Nov 20:30Z) and a by-hand run of the project rules
  (`go vet -vettool` with the `qa/driver` build).
- **What:** in a `filepath.WalkDir` callback, a guard written as a switch case,
  `case !d.Type().IsRegular() || …: return nil`, followed by the open, is still reported as an
  unguarded open. The rule's doc (`qa/rules/walkopen.go:35-40`) accepts a check only in an `if`
  (`if check { open }`, or `if !check { continue/break/return/panic }` before it). The switch form
  leaves the callback on a failed check exactly as the `if` does, so this is a false positive. It
  took two CI-length rounds to find, because the message names the open, not the form it wants.
- **Interim:** batch 10's guard is the `if` form (`a607fca45`).
- **Fix:** the rule accepts a switch case whose condition is the failed check (alone or joined by
  `||`) and whose body leaves, with a fixture in `rules/testdata`. Its message names the forms it
  takes.
- **Severity:** low (a correct guard refused; a by-hand run finds the accepted form). **Issue:**
  TBF. **State:** on F44 (`e67e28d72`, 26 Nov; batch 15, `0db93cad0`; reports/R62.md): a tagless switch
  clause that leaves on a failed check (alone, in `||`, or listed) guards what follows when
  every earlier clause leaves or holds the check; `break`/`fallthrough` in a clause and `&&`
  are no guard; the message names the forms taken.

## R61: a terminal launch beside a declared session makes a second record under a new name

- **Seen:** the coordinator's question before taking batch 9 (R15), 25 Nov 19:45Z, read in the
  code at `d5e9eda6d`.
- **What:** a restore, a team restart and a declared session keep the recorded name
  (`sessions/restore.go:561`, `teams/restart/jobs.go:298`, `sessions/declare.go`). A fresh
  `ccy --teams …` from a terminal names a new session `<launcher>-<team>-<project>-<seat>`
  (`tmux-session.bash` `ccy_tmux_next_name`). A record is the file `<registry>/<session name>`
  (`registry/registry.go:202`), so in a checkout whose session is declared under a fixed name, that
  launch writes a second record beside the declared one, and a later restore would try to start
  both. Unchecked: what the second start does with the seats the first holds.
- **Fix:** a terminal launch in a checkout with a declared record for the same seats takes that
  record's name.
- **Severity:** medium (an inventory addressed by fixed names gains a stray session). **Issue:**
  TBF. **State:** on F44 (batch 11, `6b480e66f`; ccy 3.124.57; reports/R61.md):
  `ccy-teams internal session-name` takes the checkout and reads the registry; exactly one
  stopped record of the same checkout, launcher and seats (canonical, any order; none matches
  none), in the file its name names, is taken over, its `fresh-if-no-conversation` mark kept;
  two are neither taken, said on stderr. Live proof owed.

## R60: a hot join to a team the container cannot reach reports success, and status hides it

- **Seen:** R41's second live run, 25 Nov 18:57Z (the coordinator read the container's keeper and
  link logs).
- **What:**
  - The host reaches team B's homeserver, the container does not: its network policy admits only
    the first team's bus port. The link's log has "no roster cache; sends wait for the first
    verification", then "teamsync ...: down: the homeserver did not answer usably", and every
    later request got no answer.
  - Even so, `session add` printed "seat 1 held, applied" and "The session is told it joined",
    and the seat's `report.json` said the link was running with no reason.
  - `teamchat status` in the session did not list team B: **unproven**. The coordinator's status
    sampler matched only team records with `"role":"<word>"`, and a team whose roster never
    loaded has `"role":""`, so the sampler's filter would drop it.
  - The leave notice left "owed": **withdrawn**. The coordinator's keeper.log has "its leave
    notice is owed" and "seat ... released" in the same second; nothing stayed owed.
- **Fix:**
  - `HotSeats.Applied` reports a seat joined only once its `report.json` shows an answer from
    the team's homeserver (`last_sync_ok_ms`) and the team not down, else names what is
    missing, or "the session cannot reach team X's homeserver" with the link's cause.
  - `report.json` (and the link's state message) carry `down`: null, or the link's cause.
  - `status`: a test of a held team that never answered is green on the old code (listed, down,
    name = team id, `"role":""`, trust unverified). No status change.
  - The leave notice: no change (withdrawn).
- **Not ours:** the session network policy has to admit a hot-joined team's port. That is the
  owner's host configuration.
- **Severity:** high (a join reported done that never happened). **Issue:** TBF. **State:** on
  F44 (`2e057a895`, 26 Nov, whose tree is v2-smoke `d8c38c8d8`'s; batch 12, `b239a00a6`, `2a0b1854c`: the host reads a running older image's
  report.json, which has no `down`, as down null; `8d93dd3d7`: container 2.84.82, ccy
  3.124.60); not yet run live. Detail: `reports/R60.md`.

## R59: `session add` offers a purged team's dead seat as free

- **Seen:** R41's first live run, 25 Nov 18:51Z.
- **What:** the team had been removed with purge, but its seat was still in the member pool
  (#386: no verb releases such seats). `session add` called it "free" and failed only at the
  login. `HotSeats.Choose` judges a seat free from the pool folder and the seat holdings alone,
  and nothing on that path reads the team's removal.
- **Fix:** with #386, a seat of a removed team is never offered, and a named one is refused,
  naming the removal.
- **Fixed (batch 19):** `system.Life` reads the status record, then `teamchat_teams`: declared
  absent is removed, declared present is not installed (both refused, 78); no record and no
  entry is a team served elsewhere, offered as before. Every chooser (session add, launches,
  the `--teams` chooser, seats add) consults it. Residual: an `absent` entry later deleted
  leaves no removal record. Detail: `reports/R59.md`.
- **Severity:** medium. **Issue:** #386 (related). **State:** on F44 (`e67e28d72`, 26 Nov;
  batch 19, 058938264); not yet run live.

## R57: `teamchat version` names all of `src-go`, so equal teamchat builds disagree

- **Seen:** the coordinator, 25 Nov 18:48Z. The container's `teamchat version` printed digest
  `85c2df54`, the host and the bus printed `f39fc426`.
- **What:** the digest is the git tree id of the whole `src-go` at the build commit (the
  Dockerfile's and `play-teamchat.yml`'s `-X main.SourceDigest`). Batches 7 and 8 changed only
  `internal/teams`, which is no image source, so the container kept batch 6's build, which has the
  same teamchat code. The two digests are the `src-go` trees of batch 6 and batch 7. A reader
  cannot tell a stale image from a harmless difference.
- **Also (the image label, 22:35Z):** a container seat relaunched on batch 11's launcher got a
  per-session image created that minute, labelled ccy 3.124.57 and container 2.84.80, whose
  `ccy-source-commit` label is batch 6's commit, while `claude-yolo:latest` on the same machine
  reads batch 11's. Consistent with the cause above (no image source changed since batch 6, so the
  cached builder layer keeps its label; unchecked on the machine), but an image built today names
  a commit five batches old. The fix should cover the label too: it names the image-source commit
  and says so, or a second label carries the build's checkout commit.
- **Corrected:** the keeper does compare digests at run time (the kit binary's `--version` against
  the kit directory's name and its own, `keeper/prepare.go`), and the digest names the host's kit
  directory; a digest may cover more than a build's source, never less.
- **Fix:** one script, `files/var/local/claude-yolo/image-source-digest`, digests the files
  `image-source-paths` matches at a commit; the image's builder stage runs it at the commit it
  clones and `play-teamchat.yml` at HEAD, for teamchat only (its kit is named by it), so one
  teamchat source prints one digest. teamchat-admin keeps the `src-go` tree id. `help version`
  says what the digest names. The label is renamed `ccy-image-source-commit` (the image-source
  commit, never HEAD; a HEAD label would change the image every commit). No rule: the class is
  semantic; the sweep found the two instances ([R57](reports/R57.md)).
- **Severity:** low (misleading, harmless). **Issue:** TBF. **State:** on F44 (`e67e28d72`, 26 Nov;
  batch 21, `9114b2b70`, and the label note; ccy 3.124.66, container 2.84.86); not yet run live.

## R56: a Lightning instance on a busy team's rooms grows to 75 GB resident

- **Seen:** the laptop host seat via the coordinator, 25 Nov 17:55Z (F30), read-only (`ps`,
  `free`, `podman stats`).
- **What:** the Lightning instance of the team with the busy questions room reached RSS 75.1 GB
  after 7h open, still growing (78,869,176 KiB at 17:55:08Z, about 80.9 GB at 17:57Z; over one
  measured minute after that it fell by about 200 MB, and the first "a minute later" pair had no
  real interval, so no rate stands). The host had 86 of 93 GiB in use and zram swap at 6.6 of 8.0 GiB, and
  two deploys were killed. A second instance, on a quieter team and open as long, held 0.3 GB, so
  the growth follows the traffic. Cause unverified. Upstream #98 (4 GB on Windows) had
  search-indexer memory fixed in 0.10.7; the installed version is not yet read.
- **Capture (17:58Z, `/proc` of the live process):** RssAnon 80,535,588 kB of VmRSS
  80,596,944 kB; RssFile 61 MB, RssShmem 0; Private_Dirty 80.7 GB, of it Referenced 40.8 GB;
  **153,465 `memfd:unknown-usage:QtQml` mappings** (then 3,795 JSGCHeap, 548 JITCode, 219
  `i915.gem`); 152 fds. So the memory is the client's QML engine allocations, not GPU, files or
  shared memory. `flatpak info`/`ps` were denied on the laptop, so the version is unread.
- **Next:** file upstream with those numbers once the owner agrees (public tracker; no names;
  draft `untracked/scratch/lightning-f30-issue.md`). Until a fix: quit and reopen the instance.
- **Severity:** high (it starves the owner's laptop). **Issue:** upstream, to file. **State:**
  open.

## R55: the v2 seat pool is not filtered by member type

- **Seen:** this seat, 2026-11-25, answering the owner's host-seat question (F28).
- **What:** `chooser.Teamchat.Pool(team)` is every pool seat of the team in this checkout, and
  `teamchatSeat` (`chooser/teamchat.go:80-93`) orders and offers them with no look at the seat's
  kind, though it is given `memberType`. So a bare `cc --teams <team>` (host) can be offered or
  given a free podman seat, and a `ccy` launch a host seat. A named `<N>@<team>` takes the seat
  whose seat part is N (`teamchatlaunch.ChooseSeat`), also with no kind check.
- **Fix:** with R5: the pool offered and the seat named are those of the launcher's kind; a
  named seat of the other kind is refused, naming the kind. **Severity:** medium (a host agent
  can start on a podman seat's identity). **Issue:** TBF. **State:** fixed on F44 (batch 7,
  `800d9eed7`, ccy 3.124.51). Every pool read a launch chooses from takes the kind (the handle's type, a
  plain string) as a parameter (`system.PoolState`, `Cmd.Handles`/`SeatCount`/`PoolState`, the
  chooser's `Teamchat` fields, `teamchatlaunch.ChooseSeat`), so every caller passes one, though
  nothing at compile time checks its value. `Launch.Kind` is a plain field, so `Launch.Run` and
  `OpenHotSeats` refuse an empty or unknown one (`ErrKind`, tested) before reading anything; the
  chooser, list, ways on, launch-args (podman), cc-run (host) and a running
  session's join (podman) offer and take that kind only, and a named seat of the other kind is
  refused naming its kind and launcher (`seatchoice.WrongKind`) ([reports/R5.md](reports/R5.md)).

## R54: fixes landed on F44 without reaching any image (no container bump, no CI gate)

- **Seen:** F27 (coordinator, 15:47Z): seat 7, relaunched on ccy 3.124.43, still drops every
  coordinator `say`. This seat traced it, 2026-11-25.
- **What:** ccy rebuilds its image only when the container version or the Dockerfile hash
  changes (`validate_container_version`, `lib/common.bash`). Container 2.84.77 came in at
  `ffa5e86c8` (Nov 24 12:31), whose src-go tree is the `2f5c69f0…` digest every session
  reports. U2 (R1, landed as `dc6b81aac..81b89b14b`), the source-list fix `c1b20004f` and
  batch 3 (`9d37999ce`) changed teamchat image sources and
  kept 2.84.77, so no session ran R1's, R12's or any batch 3 fix. The version gate for image
  sources lives only in `scripts/git-hooks/pre-commit`, which this checkout does not run (R52),
  and no QA script or CI job runs it.
- **Fix:** batch 4 part 2 (`09c383f5b`, container 2.84.78) is the first bump since, so a deploy
  and a relaunch bring every fix so far. The class: `scripts/qa-ccy-version-bump.bash BASE [REV]`
  runs the pre-commit hook itself (`--ccy-version-only`, so the same libraries and
  `image-source-paths`) over the change from BASE in a shared clone, as one commit.
  `qa-changed.bash` runs it as the `ccy-version-bump` check (so CI's diff mode does), and CI's
  full run judges the pushed range. Red on U2's landing, on `c1b20004f` and on batch 3; green on
  batch 4 (`reports/R54.md`).
- **Severity:** critical (hid R1's fix for a day; the owner's second team deadlocked, F27).
  **Issue:** #428. **State:** fixed on v2-smoke; it reaches F44 with the next batch.

## R53: the by-hand Go QA cannot see go-qa's module coverage floor

- **Seen:** batch 4's first CI run (45643448881): "statement coverage 89.7% is below
  fail_under_statements = 90". Per-package `go test -cover` on arm64 read above 86% per package
  and gave no module figure in go-qa's terms (`-coverpkg`, diff mode).
- **What:** the third gap of R26's class after R47: go-qa cannot run on arm64 (#426), and each
  hand-picked substitute misses one of its checks.
- **Also (batch 6 review):** `go test` over the changed packages alone misses a test in a package
  that only reads a changed one: `installplay`'s kit-part test failed on R44's new skill
  directory and no by-hand run reached it. Until #426, a by-hand run tests the whole tree the
  change sits in (`./internal/teamchat/...`, `./internal/teams/...`), not the changed packages.
- **Fix:** R26's (#426). Until then, a unit's by-hand QA reports "module coverage unchecked", and
  CI is the coverage gate. **Severity:** low (CI catches it). **Issue:** #426. **State:** open.

## R47: the by-hand Go QA missed the launcher's parity oracle

- **Seen:** this seat, batch 3 CI run 45628379212 (QA diff, go), 2026-11-25.
- **What:** U3 added `--given=<seats>` to the bash launcher's `ccy-teams internal seat-resume`
  call on purpose. `internal/ccylaunch/difftest` replays the bash launcher against recorded
  goldens, so four `launch-teams-*` scenarios (TestLauncherParity and TestFrontParity) failed in
  CI. The units' Go QA was run by hand (go-qa cannot run on arm64, #426) as `go test` over the
  changed Go packages only, which never reaches a test whose subject is a bash file. CI's diff
  routing did run it.
- **Fix:** goldens re-recorded from the launcher with the harness's `-update` (the four
  scenarios, `--given` the only difference). Every unit brief's by-hand QA now adds
  `go test ./internal/ccylaunch/difftest` when the bash launcher changes.
- **Severity:** medium (cost one CI round). **Issue:** #426 (the cause of the by-hand run).
  **State:** fixed on `v2-smoke`.

## R45: Lightning shows no settings button in a collapsed room list with the rail hidden

- **Seen:** the owner, lts-ops questions room, 14:16Z (F21).
- **What:** with the spaces rail hidden and the room list collapsed to its narrow column, no
  settings or account button is on screen; the owner widened the list by hand to reach them.
  Upstream source at 05d0be9c: the full-width header that carries them is hidden when compact,
  and the compact column (home, search, filter, archived, Activity) has none. Ctrl+, works.
- **Issue:** upstream [lightning-matrix/lightning#107](https://github.com/lightning-matrix/lightning/issues/107)
  (follows #45 and #103). **Severity:** low. **State:** filed upstream; watched.

## R46: Lightning has no quick toggle to collapse and expand the room list

- **Seen:** the owner, lts-ops questions room, 14:16Z (F21).
- **What:** collapsing or expanding the room list takes a drag, click or double-click on a 1 px
  divider; Ctrl+B hides the list entirely and no shortcut or button switches its width.
- **Issue:** upstream [lightning-matrix/lightning#108](https://github.com/lightning-matrix/lightning/issues/108).
  **Severity:** low. **State:** filed upstream; watched.

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
  On F44 (`engine/status.go:406-410` there, checked 26 Nov). On 26 Nov at 08:05Z the devops
  seat still showed present=false for everyone but itself, which is the old code's output, so
  that seat is very likely on a teamchat older than the rollout (its version has been asked
  for). One gap remains: `present` is true only for "online", so an idle ("unavailable")
  member reads as absent.

## R15: tab title, tmux session and seat drift apart

- **Seen:** the owner's terminal.
- **What:** the tab should show the team and seat, and follow a switch between tmux sessions.
- **Owner's spec (F10, 12:47Z):** the tab, the tmux session and the seat all show team, project
  and seat number, and the seat type, which is the launcher used (`ccy` for a container seat,
  `cc` for a seat on the machine itself). Example: `lts-ops · fedora-desktop.4 · ccy`. The owner
  may still change the order.
- **More (F15, the laptop host seat, 13:36Z):** that seat's tmux session is `cc-lts-lts-infra`,
  which does not name its seat. The tab title inside tmux is tmux's own: `files/etc/tmux.conf`
  sets `set-titles on` with `set-titles-string "tmux: #S  (F12 then Detach leaves it running)"`,
  so the tab shows the session name, and ccy's own title (`CCY: <project>`) is overwritten while
  tmux runs. So the fix is the session name: name it for the seat, and the tab follows.
- **Reading the title from inside a seat:** for a seat on the machine itself, inside tmux,
  `tmux display-message -p "$(tmux show-options -gv set-titles-string)"` prints the title the tab
  is given, and reads only. A container seat has no tmux client inside, so it cannot.
  Unchecked on the laptop.
- **Reproduce:** switch tmux sessions between seats.
- **Severity:** low. **Issue:** TBF. **State:** on F44 (batch 9, `d5e9eda6d`, commits in
  reports/R15.md; ccy 3.124.53 to 3.124.55; live tab check after the deploy): a `ccy`/`cc` launch with `--teams` names its tmux
  session `<launcher>-<team>-<project>-<seat>` (seats joined with `_`), its list read as the
  seat check reads it so cc and ccy name the same seats alike, built in one place,
  `ccy-teams internal session-name`; the tab follows. The name is fixed at launch. The launcher leads, not trails, and no `·`
  or `.`: see reports/R15.md. Open: a `cc --teams <team>` whose seat is chosen inside the session,
  and sessions already running, keep `<launcher>-<project>`; that restored and restarted
  sessions keep their recorded names is unchecked. Live proof owed.

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
- **Workaround (the owner, F11, 12:52Z):** untick *Show spaces rail* in Lightning's settings,
  which hides one sidebar. This is now in `docs/agent-bus.md`. No issue was filed anywhere.
- **Severity:** low. **Issue:** none. **State:** workaround known; an optional improvement is
  that each new team's Lightning instance starts with the rail off (`internal/lightning`), or
  with a collapsed option.

## R21: status counts do not say what they hold

- **Seen:** the coordinator and this seat, 2026-11-25.
- **What:** the coordinator saw `ignored: 26` and `unsent: 1`, this seat `dropped: 10`; a drop's
  reason goes only to the link log.
- **Reproduce:** `teamchat status`; compare with `history`.
- **Severity:** high. **Issue:** #422. **Unit:** U2. **State:** partly fixed (U2): drops now carry
  their reason (`counts.dropped_by`), and history records carry `to` and `for_me`. `ignored`
  is still not split by reason, and `unsent` is untouched.

## R22: LXC containers on another host cannot join a v2 team

- **Seen:** the owner and a container team, a dev VM, 2026-11-25 ("not working at all").
- **What:** the v2 LXC route admits only containers on the homeserver's own host (lxc-attach,
  the bridge address); here the containers run on another guest. Also asked: how a host-started
  LXC session signs in (A2), a v2 contract for a container repository's own launcher (A3), a v2
  `lxc-add` (A4), two doc points (A5), and the uid rule K15 in the user docs.
- **Reproduce:** a team whose homeserver is on host X; a container on host Y tries to join.
- **Severity:** critical. **Issue:** #425. **Unit:** U8 (Plan 00257 Task B.4).
  **State:** L2 to L4 fixed (U8) and merged into `v2-smoke`: a seat's url follows who places it;
  `teamchat seat place-lxc`; `team rotate-ca` lists an off-host LXC seat. On F44 at
  c1b20004f, the home pin. The re-pin of the team's estate (L5) and a live join are owed.
- **Route (the owner, Q8, 12:56Z, via A11):** the other-host route. The team's homeserver stays
  on the bus guest. A container seat's token may travel once in the ssh pipe, as a pool seat's
  password does. Only this route is built and proven.

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

## R69: a host `cc` seat gets no PEER message, only pings

- **Seen:** the laptop host seat (a host `cc` worker, delivery "socket"), 26 Nov. It reported
  this after the owner found it idle. The coordinator's job sent at 07:54Z with `say` printed
  `queued:false` and no error.
- **What (the seat's evidence, its own check):** the coordinator's PEER messages appear in the
  seat's `teamchat history` (direction "in", kind "peer"), but none reached the session.
  `teamchat recv` returns nothing. Status shows pending 0, ignored 63, dropped 420. Every
  delivery the session did get from 25 Nov on was a PING (fetch).
- **Correction (08:14Z, from the seat's link log):** my first read said the loss was between
  the store and the session. That was wrong. The link drops the messages at receive.
  - Its `link.log` holds 433 lines `teamsync lts-ops: dropped <id>: sender`, covering every
    PEER from every sender. The coordinator's 07:54Z job is among them.
  - The 14 "in"/"peer" history items are the same drops: that binary's history records a
    dropped item too.
  - Pings are not in the log, and both were delivered.
- **Cause (read in code, matches all the evidence):** this is issue #422.
  - That seat's member is `human_text: false`, and its kit is ccy 3.124.41 (teamchat digest
    90f502ac…).
  - Before #422 was fixed, the judge's `human_text` gate dropped every agent text with
    `sender`, not only a relayed human's words. A ping is not text, so it passed.
  - F44 has the fix: dc6b81aac, "an agent say reaches a human_text false worker". The gate is
    now `c.Text.Relay != nil && !HumanText(self)` (`proto/verdict.go:461-464`).
  - F44 also has c8a011430 ("a drop is counted under its reason; history, sent and reply say
    who an item is for"), which covers most of R71.
- **Live proof owed after the rollout to that machine:** a `say` to the host seat is delivered,
  and its `link.log` has no new `sender` drop.
- **Severity:** high (a host worker could not be given work by message). **Issue:** #422.
  **State:** fixed on F44 (`e67e28d72`); that machine runs an older kit, so it waits on the
  deploy there.

## R70: a sender is not told its message was dropped

- **Seen:** the coordinator, 26 Nov, on R69's lost job.
- **What (checked in code):** `say` and `reply` print `sent` once the homeserver accepts the
  event. Nothing comes back when the receiving link drops or ignores it, so the sender sees
  success either way.
- **Proposed (design, not built):** a NOTICE back to the sender for a drop of a message
  addressed to the dropping member, naming the reason. For batch 24, after R69.
- **Severity:** medium. **Issue:** TBF. **State:** open.

## R71: status counts carry no reason, and history no recipient

- **Seen:** the laptop host seat, 26 Nov.
- **What:** `dropped 420` and `ignored 63` are not defined in `teamchat help status`, and no
  command lists what was dropped or ignored. The status record has `dropped_by` (reason counts)
  and the link log names each drop (`teamsync/session.go:959-968`), but neither is named in the
  help (`kit/skill.txt:82` names `counts.dropped_by`; the help does not). On that seat's
  kit, `teamchat history` items had no "to" field, so a member could not tell which inbound
  PEERs were addressed to it; F44 adds it (c8a011430). Two different messages that open with the same paragraph look
  like a duplicate (the seat's "T4").
- **Fix (batch 24):** the help defines each count and names the log. History's "to" is done on
  F44 (c8a011430) and reaches that seat with its kit update.
- **Severity:** low. **Issue:** TBF. **State:** open.

## R72: the `git` wrapper fails in Claude Code sessions in an aliased repository

- **Seen:** a host `cc` seat on another dev machine, 26 Nov. Plain `git` fails because
  `_gh_account_git` is not defined; `command git` works.
- **Cause (checked):** both functions are defined together in
  `playbooks/imports/play-github-cli-multi.yml` (`_gh_account_git` at line 1416; `git()` calls
  it at 1445). They are written to `~/.bashrc-includes/gh-aliases.inc.bash`, which `.bashrc`
  sources. Claude Code's Bash tool replays a snapshot of the login shell, and that snapshot
  leaves out every function whose name starts with a single underscore. The filter
  `[[ "$func" == _[!_]* ]] ||` was found in the installed `claude` binary. So `git()` survives
  and its helper does not. It breaks only where the remote is `github.com-<alias>`, the one
  case in which `git()` calls the helper.
- **Also (read by a sub-agent, unchecked):** the snapshot drops variables, so the
  `_GH_ACCOUNT_USERS` array (line 1396) is lost too. `git-<alias>`, `clone-<alias>`,
  `gh-<alias>` and `git-branch-default` are likely broken the same way.
- **Fix (batch 24, Defence Before Fix):** first a rule that no function in a deployed shell
  include calls a single-underscore function or reads a global array; then the fix; then a
  test that applies the snapshot's filter and calls `git` in an aliased repo.
- **Severity:** medium. **Issue:** TBF. **State:** open.

## R73: no hot swap of a session's sign-in, and a plan that would fail over automatically

- **Seen:** the owner's question, 26 Nov, after the coordinator's session stopped three times
  on a five-hour limit.
- **What (read in code; line numbers in the reply to the coordinator, 08:1xZ):**
  - A session's token is `--token NAME`, otherwise the checkout's Quick Launch choice,
    otherwise the menu.
  - The name is kept in the container label `ccy-token`, in `.claude/ccy/.last-launch.conf`,
    and in the ccy-sessions record, but only when `--token` was typed.
  - Nothing moves a running session to another token. Plan 00146 (switching through the
    supervisor) is Not Started.
  - A user unit `ccy-session-token-failover` seen on a dev machine is not in this repository.
  - ⚠️ Plan 00146's decision 3 records an automatic restart on the next declared token, which
    contradicts the owner's rule that switching is a hot swap the owner triggers.
- **Severity:** medium. **Issue:** TBF. **State:** open; decision 3 to be settled before 00146
  starts.

## R74: owner requirement — ccy as its own installable package

- **Seen:** the owner, 26 Nov 08:19Z, on starting Claude seats inside LXC containers: the
  commands inside a container may differ from the host's, so ccy should be extracted from
  fedora-desktop into its own package that can be installed natively inside containers that
  might not run Fedora.
- **State:** recorded as an owner requirement. There is no plan yet and none is started.
  Plan 00266 records each LXC seat's own start command for now.

## R75: the team picker offers teams that were removed

- **Seen:** the owner's terminal, 26 Nov 08:26Z. A bare `ccy --teams` in a checkout offered
  four teams, each "on teamchat, 1 podman seat here". Most are retired, and one is not on the
  bus at all.
- **Cause (read in code):**
  - The list is every team with a seat in the checkout's pool
    (`teams/chooser/teamchat.go:209-233`, `teams/system/cmd.go:196` `PoolTeams`).
  - R59's filter drops a team only when it has no status record **and** this machine's
    fedora-desktop host_vars declare it `absent` (`teams/system/lifecycle.go:47-73`).
  - "No record and no entry" counts as served on another machine, so it is still offered
    (`lifecycle.go:33-35`). Another repository's declaration of the removal is never read.
  - Nothing deletes pool seats: `team remove --purge` leaves the owners' seat stores
    (`lifecycle.go:26-28`).
- **Fix (batch 24, agreed with the coordinator):**
  - (a) A pool seat issued by this machine's own homeserver, whose team has no status record
    here, is gone, whatever the host_vars say.
  - (b) A `ccy-teams seats forget <team>` verb deletes a retired team's pool seats on the
    owner's word, and refuses a held seat or a live team. The owner's retire decision is
    their Q17.
- **Severity:** medium. **Issue:** #386 (related). **State:** open.

## R76: "teamchat is starting or restarting" from a link that is running

- **Seen:** the coordinator, 26 Nov 08:11Z. It got a prompt notice "teamchat is starting or
  restarting; team messages wait on the server", while its status showed link restarts 0 and
  uptime 13750 s.
- **Cause (read in code):** when a verb or hook gets no answer from the link, it asks
  `ReadDown` why (`member/client/client.go:223`, `member/client/nolink.go:43-69`).
  - `ReadDown` starts from `DownStarting`. It changes that only for a failed link, or for a
    restarting one with a next-try time.
  - So a link whose `keeper.json` says `running` (`state/values.go:22`), but which did not
    answer this one call in time (busy, or the host loaded), is reported as starting or
    restarting.
  - The words are wrong, and the state it implies (messages waiting) may be wrong too.
- **Fix (batch 24, built test-first):**
  - `ReadDown` also probes `link.lock`. A held keeper.lock, with keeper.json saying `running`
    and link.lock held, is the new state `busy`. Its text: "teamchat is running but did not
    answer in time (busy, or the machine is loaded); team messages wait on the server until it
    does".
  - With link.lock free, it stays "starting": the link has gone, and the keeper has not yet said
    so (the e2e row that kills the link holds that).
  - The status line draws `teamchat busy` if it ever meets this state.
  - SPEC-n3 LN3 says the same.
  - The bound was not put in the words: it differs per verb (hooks 300 ms to `wait`'s timeout).
- **Severity:** low (misleading, not lossy). **Issue:** TBF. **State:** fixed on v2-smoke, waits
  on batch 24.

## R77: an LXC seat unit cannot find a claude installed under nvm

- **Seen:** a team seat, 26 Nov, checking one project's LXC containers, where claude is an npm
  package under nvm. `~/.bashrc` loads nvm only in an interactive shell.
- **Cause (checked in code):** the seat unit's PATH is fixed:
  `~/.local/bin:/usr/local/bin:/usr/bin:/bin` (`teamchat/host/lxc/unit/unit.go:92`). Its
  command is the bare word `claude` (`unit.go:72`). So `teamchat-admin lxc start` in such a
  container finds neither claude nor the node it runs on. Not yet seen failing live.
- **Fix (with Plan 00266 C.2, before it):**
  - At every start, the boot replay included, resolve `claude` and `node` through the
    container user's login shell, and put both directories first in that start's PATH.
  - Freeze nothing in the unit or the record: a node bump moves the nvm directory.
  - Refuse loudly only when the lookup finds nothing.
  - The decision is in Plan 00266 under "Finding the claude command". Test first.
- **Severity:** high where it applies (no seat starts). **Issue:** TBF. **State:** open.

## R78: run.bash runs from a dirty checkout, or one behind its upstream

- **Seen:** the owner, lts-ops Q18, 26 Nov 08:58Z (verbatim): "run.bash should block with force
  escape hatch if the repo is behind remote or uncommited".
- **Today (checked in run.bash):**
  - A clean tree is required only before an update or a pin (`assert_clean_worktree`, line
    1331, called from `fd_checkout` at 823).
  - The play runs (`--changed`, `--rerun`) only warn on uncommitted changes
    (`play_batch_warn_dirty`, 2079-2090).
  - Nothing checks whether the checkout is behind its upstream.
- **Fix (Plan 00265 Task 4.1, the batch after 24):**
  - Before any play, run.bash refuses when the checkout has uncommitted changes, or is behind
    its upstream after a `git fetch`. The message says which it is, names the paths or the
    commit count, and gives the fix (commit or stash; `git pull`).
  - A fetch that fails is a refusal too, never a pass.
  - A detached pin (`RUN_BASH_GIT_REF`) has no upstream, so only the dirty check applies.
  - `--force` runs anyway, and says on stderr which check it overrode.
  - The check is a small Go command that run.bash calls in place of
    `play_batch_warn_dirty`, because run.bash is on the bash-limits ratchet and may not grow.
  - Tests: dirty, behind, fetch failed, clean and current, detached pin, and `--force`
    over each refusal. The Go uses a real temporary repository with a bare upstream, and
    `scripts/test-run-bash-*` drives the wiring.
- **Severity:** medium (a play deployed from code that is not on the remote). **Issue:** TBF.
  **State:** open.
