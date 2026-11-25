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
- **Severity:** high (no-degrade, F19). **Issue:** TBF. **Unit:** U10. **State:** fixed on
  `v2-smoke` (U10); lands with batch 4; the live check is an add on a running v2 session.

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
- **Severity:** high (no-degrade, F19: v1's tools saw every session). **Issue:** TBF.
  **Unit:** U11. **State:** open; next after batch 4 is in CI.

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
  claude ranks a `--model` above `ANTHROPIC_MODEL`, so the person's wins with no argument
  parsing; a project `ccy.env` (sourced after) wins too. Child claudes the session starts
  inherit `ANTHROPIC_MODEL`. With several seats, the first seat with a model decides: its own,
  else its team's (`seatmodel.Settings.For`). A hot-seat join (`ccy-teams session add`) launches
  nothing, so the seat's model is not used; `HotSeats.Join` says so on stderr before the take.
  Not the image (`internal/teams` is no image source) and not the bash launcher.
- **Restore (checked):** a restore replays the person's recorded launcher arguments
  (`lib/session-registry.bash`; a `--model opus` is kept as an unknown flag's value), never the
  model a launch adds, and the replayed launch takes its seats again, so it re-reads the same
  setting and starts on the same model.
- **Severity:** medium. **Issue:** TBF. **State:** fixed on v2-smoke; batch 8 (CCY 3.124.52,
  container 2.84.80 unchanged; live after `play-claude-yolo.yml` rebuilds `ccy-teams`, already
  queued).

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
- **What:** three remote branches from an earlier session (2026-11-24; `agent-a2be5458…`,
  `agent-adff76a1…`, `agent-a1aeb314…`) hold another implementation of #417 (ccy 3.125.0 to
  3.125.5) that never reached F44. U4 built #417 again on `v2-smoke`. Those branches also hold
  work U4 may not have: "a team taken down forgets its seats' conversation pointers", a
  first-launch `--fresh` hint, and a project rule `project_temp_not_exclusive` (temporaries
  created without O_EXCL) with its sweep and the `fsx.WriteAtomic` fix.
- **Next:** compare them with U4, carry over what U4 lacks (the rule and its sweep first), then
  retire the branches. They are not deleted until that is done.
- **Severity:** medium (duplicate work, and a QA rule not landed). **Issue:** #417. **State:**
  open.

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
