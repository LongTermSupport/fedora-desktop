# SPEC: a reports room and a questions room for every team

Phase 1 (the member side) is built and not yet deployed; Phase 2 (the admin side) is not
built. [PLAN.md](PLAN.md) holds the milestones and the decisions
the owner may want to change; this document is what they build. It was revised after the
design review (`FIX-FIRST`; the review's findings and their dispositions are journalled).

## 0. The request

The owner's request, paraphrased faithfully:

- Each team has one homeserver and **one room** today. When a team is busy that room is far
  too busy to follow.
- Add **two rooms per team**:
  1. **Reports** ("management info / reporting"). Read-only from the owner's point of view.
     Any agent may post what the human should be aware of. Not for questions.
  2. **Questions** (Q&A), human-level questions only. An agent with a question asks the
     coordinator, as now, on the main room. The coordinator tries to answer it. Only what it
     cannot answer goes up, as **one long-form, self-contained question message** in the
     questions room. The owner answers there. The coordinator is woken for **every** message
     in that room and dispatches the answers to the agents that asked.
- The coordinator-to-human channel should be a real room, not a direct message, so other
  agents can be added later. The questions room is that room (section 3.3).
- The goal is a much quieter interface for the owner when a team is busy.

"Asks the coordinator, as now" is read as: workers ask on the **main** room and never post in
the questions room. That keeps the questions room to the owner's own questions (PLAN.md D7).

Terms, as in the protocol: *main room* is today's team room (protocol §8, the bundle's
`room`); *record* is the admin-sent `agent_bus.team` state; *admin* is the team's
`@admin:<server_name>` account; *coordinator* is an agent the record's `roles` names
`coordinator` (the writers keep one; a receiver treats every one listed as a coordinator);
*human* is a user ID in the record's `humans`; *purpose* is `reports` or `questions`.

## 1. What exists today (research)

| Fact                                                                                                                                                                                                                                                                        | Where                                                                                                                                                                                                    |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| One room per team, created once by `admin` in `bootstrap` (`preset: private_chat`, room version 12, name = team, topic, power levels, the record as initial state). Its ID is kept in `/var/lib/agent-bus/<team>/room_id`; a second run re-checks the create event.         | `helpers/agent_bus/admin.py:559-581`, `:10`, `:68`                                                                                                                                                       |
| `bootstrap` is idempotent: humans' accounts, the room, removals of humans no longer listed (kicked from the main room only), power levels, record and topic (each written only when it differs), and the humans' invites.                                                   | `helpers/agent_bus/admin.py:584-600`, `:629-651`, `:642-644`                                                                                                                                             |
| A member is invited to the main room by `add_member`; `set_role` and `remove_member` re-sync the record; `remove_member` kicks and deactivates.                                                                                                                             | `helpers/agent_bus/admin.py:724-773`, `:798-827`, `:618-626`                                                                                                                                             |
| The play is a thin caller of `agent-bus-install`; `software` builds the zipapps and the member kit, then `team` runs `bootstrap` per team. The LXC kit (with `pingbus`) is copied into each running container **only** by the "Join the LXC members" task, after the teams. | `playbooks/imports/play-agent-bus.yml:182-195`, `:272-333`, `:373-384`; `files/usr/local/sbin/agent-bus-install:387-395`, `:493-498`, `:831`; `helpers/agent_bus_lxc/members.py:1008-1015`, `:1228-1231` |
| An LXC member may be declared with `human_text: false` (`--no-human-text`).                                                                                                                                                                                                 | `helpers/agent_bus_lxc/members.py:456`                                                                                                                                                                   |
| A ccy session's `pingbus` and the plugin come from the image build context; the launcher compares the base image ID. A change to what the image bakes in (`helpers/pingbus/` included) still needs the container version bump: the pre-commit gate requires it (5.6).       | `playbooks/imports/play-claude-yolo.yml:342-360`; `files/var/local/claude-yolo/claude-yolo:2096-2098`                                                                                                    |
| The bundle names exactly one `room`; every member-side read and write uses `member.room`.                                                                                                                                                                                   | `helpers/pingbus/config.py:53`, `:103`, `:519-541`; `helpers/agent_bus/admin.py:685-702`                                                                                                                 |
| The syncer's `/sync` filter names only that room; an invite is accepted only to it, from `admin`, and every other invite is rejected with `POST /leave`. After a join the room's history is skipped.                                                                        | `helpers/pingbus/syncer.py:119-134`, `:137-160`, `:367-379`, `:381-390`; protocol §8                                                                                                                     |
| Room trust: create event by `admin`, version 12, event ID derived from the room ID, power levels exactly `expected_power_levels(humans)`, a valid admin-sent record. A change to create, power levels or the record re-verifies.                                            | `helpers/pingbus/syncer.py:46-48`, `:295-313`; `helpers/pingbus/protocol.py:1021-1031`; protocol §8                                                                                                      |
| Power levels: humans 50, agents 0, `state_default` 100, `invite`/`kick` 100, `notifications.room` 50, `m.reaction` 50. `admin` holds creator power (not listed).                                                                                                            | `helpers/pingbus/protocol.py:161`, `:1021-1031`; protocol §8                                                                                                                                             |
| Humans and agents are told apart by the record alone: a sender in `humans` is `human`, an agent's class is its role; anyone else is dropped (`sender`). Human localparts have their own grammar; `admin` and `conduit` are reserved.                                        | `helpers/pingbus/protocol.py:587-594`, `:465`, `:60`, `:84`                                                                                                                                              |
| Two roles, `coordinator` and `worker`; `coordinators()` lists the holders.                                                                                                                                                                                                  | `helpers/pingbus/protocol.py:48-53`, `:564-567`; Plan 00196                                                                                                                                              |
| A human's message is delivered only when it mentions this agent (or `@room`) and the bundle's `human_text` is true; edits are dropped (`edit`); reply fallbacks are stripped. A human message no agent receives is logged as unheard.                                       | `helpers/pingbus/protocol.py:779-804`, `:889-903`; protocol §9                                                                                                                                           |
| The inbox stores accepted events and **re-validates each one** with `validate_event` whenever it is read (`recv`, `wait`, the hooks, the watcher). A stored event carries no room.                                                                                          | `helpers/pingbus/inbox.py:380-421`; `helpers/pingbus/hooks.py:563`; `helpers/pingbus/cli.py:1169`, `:1686`, `:2269`                                                                                      |
| `history` re-validates each room message the same way.                                                                                                                                                                                                                      | `helpers/pingbus/syncer.py:519-575`                                                                                                                                                                      |
| A worker's text or ping to a human copies the coordinator automatically.                                                                                                                                                                                                    | `helpers/pingbus/protocol.py:570-584`                                                                                                                                                                    |
| `say` builds an agent text (`m.notice`, `agent_bus.text`), checks it, charges the send gate (token bucket, 60 s duplicate window), and posts to `member.room`.                                                                                                              | `helpers/pingbus/cli.py:1060-1096`; `helpers/pingbus/limits.py:26-47`; `helpers/pingbus/protocol.py:43-45`                                                                                               |
| Delivery into the session: the watcher (long-running, so its code is what was loaded when it started) syncs and writes each accepted item into the conversation through the inbox socket as an envelope.                                                                    | `helpers/pingbus/cli.py:1926`; `helpers/pingbus/envelope.py:30`, `:54-71`; Plan 00194                                                                                                                    |
| Session-start role lines tell each agent its role and its coordinator.                                                                                                                                                                                                      | `helpers/pingbus/hooks.py:134-142`, `:285-291`                                                                                                                                                           |
| The skill says the team is one room and teaches HUMAN, PEER, PING, `say --to`, `--to-coordinator`, and the roles.                                                                                                                                                           | `files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md:8-12`, `:135-150`, `:206-230`                                                                                           |
| Output lines only ever gain fields at the end (protocol §15), but **ccy-teams' parser of `agent-bus list` takes an exact field count** per line kind and errors on any other; lines of an unknown kind are skipped.                                                         | protocol §14, §15; `src-go/internal/agentbus/parse.go:167-180`; `src-go/internal/marker/marker.go:22`                                                                                                    |
| The homeserver allows room creation only by `admin` (Plan 00161 D63), so all agent traffic stays auditable by the humans.                                                                                                                                                   | `CLAUDE/Plan/00161-agent-team-bus-matrix/DESIGN.md:57`, `:378`                                                                                                                                           |
| ccy-teams shows each human's main-room membership on the board.                                                                                                                                                                                                             | `src-go/internal/teams/board/snapshot.go:268`; `src-go/internal/teams/board/table.go:243-250`                                                                                                            |
| Element: each human's profile is per homeserver, so a new room on the same server needs no new sign-in; humans accept an invite in Element.                                                                                                                                 | `helpers/agent_bus/element.py:1-23`                                                                                                                                                                      |

## 2. Relation to other plans

- **Plan 00207 (team roles), SPEC-roles §11 and §12-13.** It designs a coordinator-and-humans
  *management room* (terse, capped briefs and `Q<n>` questions) and invite-only *channel rooms*
  kept in step by a reconciler. It is Not Started and waits on 25 owner decisions. This request
  differs from its §11: reports come from **any** agent, and questions are **long-form and
  self-contained**, not capped at 300 characters. This plan therefore **supersedes 00207 §11**
  and is the smaller, earlier step that 00207 builds on:
  - 00207's `management` room becomes this plan's `questions` room, and its briefs become
    reports;
  - 00207's multi-room bundle and reconciler (M6a) start from this plan's `agent_bus.rooms`
    index (4.1), not a second mechanism;
  - this plan keeps 00207 §11.2's idea of `Q<n>` numbering read from room history, with no
    session state.
  - The reports room is joined through a **restricted** join (3.1). That ends 00207 SPEC-roles
    §12.2's invariant that "no room uses restricted joins". The note filed in 00207's PLAN.md
    says both things.
- **Plan 00209 (v2 in Go), unmerged, read at `1447d536`.** It keeps protocol 1 and every state
  file unchanged, and ports v1 stage by stage (DESIGN §10.2). This plan extends v1 (PLAN.md D9,
  for the owner's sign-off), so the new event types, content keys, output field, line kinds
  and inbox key must be added to 00209's protocol corpus (its §5.4) as requirements.
- **Plan 00161 D63** (one room, all auditable). Kept: every human of the team is in every room,
  and only `admin` creates rooms.
- **Plan 00194** (live delivery): the questions room reaches the coordinator through the same
  watcher and inbox socket. **Plan 00196** (coordinator): the role moves the questions-room
  membership. **Plan 00197** (seat broker) and **Plan 00205** (SSH members) add members
  through the same admin calls, so they inherit this. **Plan 00178** (history export) reads
  only the main room and is told about the new rooms.

## 3. The rooms

### 3.1 Shape

| Room      | Purpose key | Name (Element)       | Members                                         | Join rule                                 | Who may post                                    | Created                                    |
| --------- | ----------- | -------------------- | ----------------------------------------------- | ----------------------------------------- | ----------------------------------------------- | ------------------------------------------ |
| main      | (`room`)    | `<team>`             | humans, every agent (as today)                  | invite (as today)                         | as today                                        | exists                                     |
| reports   | `reports`   | `<team> · reports`   | humans (invited), every agent running this code | restricted: main-room members may join    | agents only (enforced, 3.2)                     | every team                                 |
| questions | `questions` | `<team> · questions` | humans, every coordinator                       | invite; `admin` invites coordinators only | humans and coordinators; nobody when none (3.2) | when the team first has a coordinator (D4) |

- **Purpose keys are fixed protocol strings**, `reports` and `questions`. Each team is its own
  homeserver, so the names need no team prefix inside the protocol. The Element name carries
  the team, so the owner's room list reads well across teams.
- **No room aliases.** Members find the rooms by ID through the index (4.1), as they find the
  main room through the bundle today (D11).
- **Topics** (set by `admin`, informational, never trust):
  - reports: "Reports from the team's agents, for you to read. To reach an agent, write in the
    main room; answer questions in the questions room."
  - questions, open: "Questions the coordinator `<handle>` could not answer itself. Answer
    here; the coordinator reads every message in this room."
  - questions, closed: "This team has no coordinator now, so this room is closed."
- **History visibility `shared`** for both, set explicitly: a new coordinator must read the open
  questions (D13).
- **Encryption off, federation off**, as for the main room.
- A member's syncer joins the reports room as soon as it reads the index, so it can post
  there. The join is not used as proof of anything: the deploy checks the questions-room join
  itself (5.3).

### 3.2 Power levels

**Reports: read-only for humans, enforced by the homeserver (D2).** Agents post at 10;
humans are listed at 0, so Element shows the composer as disabled.

```json
{"users": {"<each human user ID>": 0},
 "users_default": 10, "events_default": 10, "state_default": 100,
 "invite": 100, "kick": 100, "ban": 100, "redact": 100,
 "notifications": {"room": 100},
 "events": {"m.room.power_levels": 100, "m.room.tombstone": 150,
            "m.room.redaction": 100, "m.reaction": 100, "m.sticker": 100,
            "agent_bus.status": 100}}
```

Why enforce rather than leave it to convention: nobody is woken by the reports room, so a
human's message there would reach no one, silently. The project's first rule forbids that, and
making it impossible is the fail-fast answer. Agents get their power from `users_default`, so
the content depends on `humans` only, as the main room's does, and an agent joining changes
nothing. A human removed from the team is kicked from every room (5.1) before the power levels
are rewritten, so a removed human never falls back to `users_default` 10. `@room` and
reactions are closed to everyone; read receipts are not power-gated, so the owner's "seen"
marks still work.

**Questions, open** (the record names a coordinator): `expected_power_levels(humans)` with
**`m.reaction` at 100** (D16). Humans 50, coordinators 0 posting at `events_default` 0,
`@room` for humans only. Reactions are closed because an emoji is an ambiguous answer to a
decision, and closing them means Element does not offer them. Every answer is then a message,
which the coordinator receives.

**Questions, closed** (no coordinator): the same, with the humans listed at **0** and
`events_default` 10, so nobody can post. A message nobody would read is impossible rather than
silently unheard (D4). The admin switches between the two forms when the record's coordinators
change, and a receiver accepts exactly the form that matches the record.

The homeserver keeps workers out of the questions room by membership, not power: they are never
invited, the join rule is `invite`, and an agent cannot invite (`invite` 100).

### 3.3 The questions room is a room, not a direct message

The owner asked that the coordinator-to-human channel be a real room, so other agents can be
added later. It is an ordinary invite-only team room created by `admin`. Adding another account
later is one invite and one membership rule in `rooms_for` (Plan 00207 §13.1), not a new
channel. A direct message between two accounts could not take a third, and the server refuses
agents' room creation anyway (Plan 00161 D63).

## 4. How members find and trust the rooms

### 4.1 The index in the main room

`admin` writes a new state event in the **main room**:

- type `agent_bus.rooms`, state key `""`, content `{"v": 1, "reports": "!…", "questions": "!…"}`;
- each key is present once its room exists, and there is no other key;
- `state_default` 100 already means only `admin` can write it.

A receiver reads it only when it is sent by `admin`, `v` is 1, its keys are a subset of the
purpose keys, and each value is a room ID other than the main room's. It is added to the
syncer's state and timeline types and to its trust types.

Why a new event, not a record key or a bundle field:

- An unknown **record** key makes an older pingbus distrust the whole main room (protocol §8).
  A new state **type** is outside the older syncer's filter (`syncer.py:48-49`), so an older
  member never reads it and loses nothing.
- A **bundle** field would mean re-issuing every member's bundle. The index reaches every
  member, LXC and ccy included, on its next sync, with no re-issue.

### 4.2 The back-link in each side room

Each side room carries `agent_bus.room`, state key `""`, sent by `admin`, content
`{"v": 1, "team": "<team>", "purpose": "reports"|"questions", "main": "<main room ID>"}`, as
initial state at creation. A side room is trusted only when the index names it **and** its
back-link names the same purpose and this member's main room. Neither room can then be swapped
for another `admin` room on its own.

### 4.3 Side-room trust (checked on join and on every change to a trust type)

- The create event: sender `admin`, version 12, no `additional_creators`, and an event ID
  derived from the room ID (as protocol §8 for the main room).
- The back-link (4.2).
- Power levels exactly as section 3.2 for the purpose and, for questions, the open or closed
  form the record calls for, against the **main room's** record's `humans`.
- `m.room.join_rules` exactly:
  - reports:
    `{"join_rule": "restricted", "allow": [{"type": "m.room_membership", "room_id": "<main>"}]}`;
  - questions: `{"join_rule": "invite"}`.
- A side room's trust types are the main room's plus `m.room.join_rules` and `agent_bus.room`.
- A side room that fails is untrusted on its own:
  - `report`, `ask-human` and `thread` exit 10, naming the room;
  - the coordinator stops syncing the room, and `pingbus status` shows why;
  - the main room stays trusted.
- A non-transient homeserver failure in a side room (a refused restricted join, which Synapse
  answers 400, or no usable answer) is that room's alone, as a failed check is: it is logged
  in full, recorded with its status and errcode as the room's reason (`refused` for a reports
  join, `pending` for a questions join, `untrusted` for a state read), and every main-room
  command goes on. A failed question read on receive stores the items without their question.
- A transient one (no connection, a 5xx, a 429 past its retries: what a homeserver restart
  looks like) is raised, as a main-room failure is, so the watcher's retry and backoff handle
  it: the room is never marked for it, and the sync token is not committed. A questions gap
  fill is never contained, since the token covers both rooms and committing past a failed
  fill would lose the messages.

### 4.4 Joining and leaving

- **Reports.** A member's syncer joins the reports room (`POST /join/<id>`) when it first reads
  the index. The restricted join rule allows it because the member is in the main room. The
  member never syncs the room afterwards: the filter does not name it. No invite is sent to an
  agent, so an older pingbus never sees one to reject. Humans are invited by `admin`, so the
  room shows up in Element.
- **Questions.** `admin` invites every coordinator. The coordinator's syncer accepts an invite
  to a room when all of these hold:
  - the inviter and the stripped create sender are `admin`;
  - the trusted index names the room as `questions`;
  - the record names this member `coordinator`;
  - the bundle's `human_text` is true (4.5).
- **An admin invite the index does not (yet) name is left pending**, logged by room ID, never
  joined and never rejected: rejecting would need a re-invite. A questions invite to a member
  that is not a coordinator with human text is left pending the same way, and `pingbus status`
  and the hook context name the reason (4.5). Every other invite is still rejected, as today.
- **An older pingbus** rejects the questions invite (`POST /leave`), as it rejects every invite
  but the main room's. That is loud, not silent: the deploy-2 wait leg names the coordinator
  (5.3), and the next play run invites it again, because its membership is then `leave`.
- **Demotion.** `admin` kicks an agent that is no longer a coordinator from the questions room
  (reason "no longer the coordinator") before it invites the new one, the order Plan 00196's
  LXC handover uses. A member whose record no longer names it coordinator stops syncing the
  questions room at once, before the kick arrives.
- **Handover.** A newly joined room's history is skipped (`syncer.py:381-390`), so a new
  coordinator would miss answers given during a handover. Two things cover that:
  - `role_coordinator` tells it to run `pingbus questions` first (10.2);
  - `questions` lists every human reply that no coordinator has answered or closed (8.4).

### 4.5 A coordinator must take human text

A coordinator whose bundle has `human_text: false` would drop every message in the questions
room as `sender`, silently. That is refused **where the role is assigned**, each with a test.
A sync never refuses it: failing every sync would take the member off the bus entirely.

- `members.py` refuses an LXC entry with `role: coordinator` and `human_text: false`
  (`MemberError`, naming the entry). An LXC member's role comes only from its entry, so this
  covers both its `add-member` and its `set-role`.
- `admin.add_member` refuses `role` coordinator with `human_text` false (`AdminError`: "a
  coordinator must take human text: drop `--no-human-text`"). ccy-teams never issues a bundle
  with `--no-human-text`.
- `set-role` cannot see the bundle's flag (the registry does not record it). A bundle issued by
  hand with `--no-human-text` whose member is later made coordinator is the one case left, and
  it is loud: the member leaves the questions invite pending (4.4), `pingbus status` shows
  `questions: refused (human_text is false; re-issue the bundle without --no-human-text)`, the
  hook context says the same, and the deploy-2 wait leg and health name it as not joined.

## 5. Creation and migration (IaC)

### 5.1 The admin side

`bootstrap` gains `_ensure_side_rooms` after `_ensure_room`. For the reports room on every
team, and for the questions room once the record has ever named a coordinator, it:

1. **Finds or creates the room.** Its ID file is `room_id.reports` or `room_id.questions`,
   beside `room_id`. When the file is present, the room's create event and back-link are
   re-checked. When it is absent (a crash between `createRoom` and the file's write), the admin
   **adopts** the room before creating one:
   - it looks first at the room the index names;
   - else at the rooms `admin` has joined (`/joined_rooms`) whose admin-created back-link names
     this purpose and this main room.
     Exactly one match is adopted, and its ID file is written. More than one is an `AdminError`
     naming them. Only none leads to `createRoom`.
2. Writes the index in the main room when it differs.
3. Syncs each side room's power levels (for questions, the open or closed form), join rules and
   topic, each only when it differs.
4. Invites every human to each side room who has neither joined nor been invited.
5. Removes a human who is no longer listed from **every** room (main, reports and questions),
   before any power levels are rewritten. This extends `admin.py:642-644`, which kicks from the
   main room only.

**Questions membership** is synced by one function, `_sync_questions_members`. It is called
from `bootstrap`, `add_member`, `set_role` and `remove_member` (parking keeps the role, so
`park_member` does not call it). It:

1. creates the questions room through step 1 above, when the record names a coordinator and the
   room does not exist yet;
2. kicks every agent account in the room that the registry does not name coordinator;
3. invites **every** coordinator that has neither joined nor been invited to questions (a
   coordinator whose membership is `leave`, because an older pingbus rejected the invite, is
   invited again);
4. switches the power levels and topic between the open and closed forms when the record's
   coordinators change.

Kicks come before invites. `remove_member` also kicks from reports. Each change is appended to
`changes`, so the installer prints `CHANGED` only on change and the play's `changed_when` is
unchanged. A failed homeserver call stops `bootstrap` with the step named (`AdminError`), as
today. A re-run converges because every step compares first.

### 5.2 Existing teams

There is nothing to migrate by hand. The deploy-2 play run creates the reports room for every
team present, and the questions room for every team with a coordinator. It also writes the
index and invites the humans and the coordinators. The main room keeps its ID, history and
members.

### 5.3 Upgrade first, and the deploy-2 legs (D12)

A coordinator's running watcher must understand the index before an invite reaches it. An
older one rejects the questions invite with `POST /leave`. The file on disk proves nothing:
the watcher is long-running and keeps the code it started with, and short-lived commands
(`recv`, `wait`) sync too. The only proof that the running code is new is the event the hazard
is about: the coordinator **joining the questions room**. So:

- **Deploy 1, member side only.**
  - Phase 1 is merged to F44 **before any Phase 2 code is**, and F44 still has the old admin
    code.
  - Deploy 1 runs the **whole** `play-agent-bus.yml`, because the LXC kit is copied only by its
    "Join the LXC members" task (`members.py:1228-1231`). It also runs the ccy play's staging of
    the pingbus build context.
  - No room is created, because the admin code that would create one is not on F44.
  - The deploy-1 acceptance (`acceptance.bash --stage 1`) checks two versions: the host
    `pingbus version`, and the in-container `pingbus version` of every declared LXC member. It
    refuses a declared container that is down.
  - The closing message asks the owner to restart the teams' sessions.
- **Deploy 2, admin side.** `deploy.bash --stage 2` runs these `plan_deploy_leg`s, failing fast:
  1. **Pre-flight.** Every declared LXC container is up and its kit's pingbus is the new one;
     a container that is down is refused, named. Containers can stop between deploys.
  2. **The play**, the whole `play-agent-bus.yml`. It creates the rooms and the index, and
     invites the humans and every coordinator.
  3. **Questions joins.** Waits, up to 180 s, for each team's coordinator to have **joined** the
     questions room, read through the new `ROOM` lines of `agent-bus list` (5.5). It refuses
     naming each one that has not, with its membership: "`<handle>` has not joined questions
     (`invite`: its session is not running; `leave`: its running pingbus is older than deploy
     1): restart that session, then run meta-deploy again". The re-run's play invites a `leave`
     coordinator again.
  4. **`acceptance.bash --stage 2`** (11.2).

A coordinator that becomes old later (a session relaunched from a stale image) and rejects a
new invite is `leave` in the questions room. Health names it (5.4), and the next play run
invites it again.

### 5.4 Health

The admin tool gains a read-only side-room check, `agent-bus check-rooms <team>`
(`admin.check_rooms`). It is not in `health.py`: that is root's homeserver probe behind a
timer, holds no Matrix credential and checks no room, so the first version of this section,
which put the check there "as the main-room checks do", named checks that do not exist. The
check reads, as `admin`:

- the rooms exist and their state is exact;
- every human is in each room;
- no worker is in questions;
- each coordinator is joined to questions, and to reports (a coordinator running 0.2.0
  joins both).

Each fault is a `PROBLEM` line with its repair (run the agent-bus play, or restart the
coordinator's session and run the play), and any fault exits 1. Stage 2's acceptance (A1,
A2) runs it for every team.

### 5.5 `agent-bus list`: a new line kind

`agent-bus list` gains one line per account per side room:
`ROOM\t<user ID>\t<purpose>\t<membership>`, where membership is `join`, `invite`, `leave`
or `-`. **HUMAN and MEMBER lines are unchanged.** ccy-teams' parser takes an exact field
count per kind and skips kinds it does not know (`parse.go:167-180`, `marker.go:22`), so a
ccy-teams binary from before Phase 3 keeps working through deploy 2.

### 5.6 Member kinds

- **LXC members (top priority).** Covered by deploy 1's whole-play run and the deploy-2
  pre-flight (5.3). An LXC coordinator syncs from inside its container like any member; nothing
  changes on the host side.
- **ccy members.** The new zipapp and plugin go into the image build context. Each commit that
  changes `helpers/pingbus/` (or anything else the image bakes in) bumps the container version
  and `CCY_VERSION`: the pre-commit gate requires it (CLAUDE/ContainerRules.md), so the first
  version of this spec, which said no bump was needed, was wrong (Task 1.9). A running
  container keeps its old pingbus until its session restarts, and the deploy-2 questions-join
  leg (5.3) covers that.
- **Host `cc` sessions** use the host's `/usr/local/bin/pingbus` and pick it up on relaunch.

## 6. Who is woken by what

| Event                                             | Who is woken                                           | Delivered as                                  |
| ------------------------------------------------- | ------------------------------------------------------ | --------------------------------------------- |
| Any message in the reports room                   | nobody (no agent syncs the reports room)               | nothing                                       |
| A human's message in the questions room           | every coordinator, mention or not                      | `HUMAN`, `room: questions`, with its question |
| A human's edit of a message in the questions room | every coordinator                                      | `HUMAN`, `revises: <event>` (D6)              |
| A coordinator's own post in questions             | nobody (self)                                          | nothing                                       |
| Another coordinator's post in questions           | nobody: ignored (`coordinator-post`), never a drop     | nothing                                       |
| Any other agent's post in questions               | nobody: dropped as `sender` (impossible without drift) | nothing                                       |
| Main-room traffic                                 | as today                                               | as today                                      |

- A coordinator's `/sync` filter names the main room and, while it is a coordinator and the
  room is trusted, the questions room. One sync token covers both, and a limited timeline in
  either is gap-filled by the same `/messages` read (`GAP_FILTER`).
- **Every stored inbox item records its room.** A questions-room item is stored with one more
  **top-level** key, `room`, whose value is `questions`, beside the event's stored keys. A
  main-room item gets no such key, so it stays byte-identical to what older code writes and
  reads (an older reader runs during deploy 1's in-place upgrades, and old code never produces
  or reads a questions-room item). `inbox.pending()` passes the room (`main` when the key is
  absent) to `validate_event(…, room=…)` when it re-validates (`inbox.py:380-421`), and
  `history --room` does the same (`syncer.py:519-575`). Without this, re-validation would
  ignore every unaddressed questions-room message as not addressed, and the coordinator would
  never see the owner's answer.
- `validate_event` with `room="questions"` treats every human message as addressed to this
  member, and requires the receiver to be a coordinator.
- A human's message in the questions room is a `HUMAN` item with the human's full authority,
  verified as today: `history --room questions` shows it, and the consequential-request rule
  applies.
- **Edits (D6).** In the questions room only, a human's `m.replace` edit is delivered as a new
  `HUMAN` item. Its body is the edit's `m.new_content.body`, and it carries `revises: <original event ID>`. The item is a revision the coordinator weighs against the original, never a silent
  replacement. In the main room, edits are still dropped as `edit`.

## 7. Reports: `pingbus report`

`pingbus [--team T] report` posts the text on stdin to the team's reports room.

- **Who:** any agent holding a role. A human cannot (power levels).
- **Form:** a first line (the headline) of at most 120 characters, then an optional body, at
  most 1500 bytes in all (D10). Over the cap is refused (exit 4, `body`), never truncated. The
  secret-shape check and the member's own token check of `say` apply.
- **Wire:** `m.room.message` with `msgtype` `m.notice`, `body` the text, content key
  `agent_bus.report` `{"v": 1}`, and **no mentions**. Matrix's default push rule
  `.m.rule.suppress_notices` means Element never notifies a notice, so reports are quiet by
  construction.
- **Before the rooms exist** (no index yet) it is refused with exit 4, reason `no-room`: "team T
  has no reports room yet; it is created by the agent-bus play". That is "not deployed", not
  "untrusted".
- **Limits (reused):**
  - a report is charged to the member's existing send gate (the `limits` token bucket,
    `admit_send` with a `report` gate verb);
  - `report_per_hour` (default 6, bounds 1-60, per member). Its send times are kept in their own
    file, `state/reports.json`, not in the outbox's `gate`: an older reader checks that the
    gate's keys are exactly `{bucket, recent}`, and would refuse the outbox during deploy 1's
    in-place upgrades. (A departure from the first version of this spec, accepted in Phase 1.)
  - a report equal to the member's previous report within the duplicate window is refused
    (exit 9).
    These are the bucket and window `say` already uses; there is no new rate machinery.
- **Output:** a `SENT` line, as `say` prints.
- **What belongs there**, from the skill: a milestone reached, a merge, a deploy queued, a
  decision taken on the owner's behalf, a risk found, or something blocked that the human may
  want to know about. **Never a question**, never a request, never a narrative of routine
  steps.
- The coordinator is the usual reporter, but any agent may report. A worker's report does not
  copy the coordinator: nobody is woken.

## 8. Questions

### 8.1 The flow

1. A worker with a question asks the coordinator on the main room, as now:
   `pingbus say --to-coordinator`.
2. The coordinator answers what it can, from the plan, the repository, the team's history and
   its own judgement, with `say --to <worker> --re <event>`.
3. What it cannot answer, and what genuinely needs a human, it asks with `pingbus ask-human`:
   one question per message, long-form and self-contained.
4. The owner answers in the questions room. The coordinator is woken for every message there.
5. The coordinator passes the answer to each agent that asked, on the main room
   (`say --to <worker> --re <the worker's original event>`), then closes the question in its
   thread (`pingbus thread Q<n> --close`).

### 8.2 `pingbus ask-human`

`pingbus [--team T] ask-human [--asked-by NAME[,NAME…]] [--re EVENT_ID]` posts the question on
stdin.

- **Who:** a coordinator only (exit 4, `role`, for anyone else).
- **Form, checked:**
  - a first line (the headline) of at most 120 characters;
  - sections starting `Context:` and `Question:`, both required;
  - an optional `Options:` section, numbered `1.` to `9.`;
  - at most 8000 bytes in all (D10).
    A missing section is refused (exit 4, `body`), naming it. This makes "self-contained" a
    format, not a hope: the reader needs no other room.
- **Numbering:** pingbus prefixes the headline with `Q<n>`. `n` is one more than the highest
  `Q<n>` among the coordinators' `ask-human` posts in the room's history. This is Plan 00207
  §11.2's rule: with no session state, a relaunched or replacement coordinator numbers the same
  way.
- **No open-question cap.** One was considered and dropped: it would block an urgent question
  whenever the owner leaves several unanswered. The skill's "one question per message, only
  what you cannot answer" and the duplicate refusal are what keep the room quiet.
- **Duplicates:** a question whose `Question:` section equals an open question's is refused
  (exit 9), naming that `Q<n>`.
- **Wire:**
  - `msgtype` `m.text`, because a notice would be silenced by `.m.rule.suppress_notices`;
  - `m.mentions.user_ids` lists every human, so Element notifies;
  - content key `agent_bus.question`, `{"v": 1, "n": <int>, "asked_by": [<user IDs>], "re": [<event IDs>]}`.
    `asked_by` and `re` are the coordinator's record of whom to answer. Each `--asked-by` name is
    resolved as `say --to` resolves it, and each `--re` must be an event in the main room.
- **Output:** `SENT`, plus one `QUESTION` line in the one layout §8.4 gives it,
  `QUESTION 1 TEAM Q<n> open EVENT_ID ASKED_BY 0`. (The first version of this spec gave a
  shorter form here that conflicted with §8.4; one layout was accepted in Phase 1.)

### 8.3 Threads, replies and attaching an answer (D5)

Each question is a **thread root**. Threads keep the room's main timeline at one line per
question, which is the quiet the owner asked for, and keep each question's back-and-forth
together.

A human message (or edit) is attached to a question by the first rule that holds:

1. its `m.relates_to` is `rel_type: m.thread` with `event_id` a question (Element's
   "Reply in thread");
2. its `m.in_reply_to` names a question or a post in a question's thread (Element's "Reply");
3. its body starts with `Q<n>` (any case, then a space, `:` or `.`) naming a question;
4. for an edit, the original's attachment;
5. otherwise it is **unattached**. It is still delivered, shown with `question: none`, and the
   coordinator decides. The owner may start a topic here unprompted (D15).

The reply fallback is stripped, as today. The delivery envelope gains lines after `team:`:
`room: questions`, then `question: Q<n> "<headline>"` (or `question: none`), then `revises: <event ID>` for an edit. `recv` and `wait` append one field to the `HUMAN` line: `ROOM`, which
is `-` (main room), `questions:Q<n>` or `questions:-` (protocol §15: fields are only
appended). The `REPLY` command for a questions-room item is `pingbus --team T thread Q<n>`, or
`thread --re 'EVENT_ID'` when the item is unattached.

The exact `m.relates_to` shapes come from the Matrix specification's threads and replies
modules. The owner's real reply in A5 is recorded as a fixture, and the tests are re-run against
it (Task 4.2).

### 8.4 `pingbus thread` and `pingbus questions`

- `pingbus thread Q<n> [--ask] [--close] [--re EVENT_ID]` (coordinators only) posts the text on
  stdin into the question's thread. Examples are a follow-up ("I need the cost limit as a
  number") or the dispatch note ("Passed to myrepo.2 and myrepo.4; closing.").
  - `--ask` mentions the humans, so Element notifies; without it a note does not notify.
  - `--close` marks the question closed with the content `agent_bus.question_state`
    `{"v": 1, "n": <int>, "state": "closed"}` in the same message. The text may be empty with
    `--close`.
  - At most 4000 bytes; the send gate applies.
- `pingbus questions [--all]` (coordinators; read-only) prints one line per question from the
  room's history: `QUESTION 1 TEAM Q<n> STATE EVENT_ID ASKED_BY UNANSWERED`.
  - `STATE` is `open` (no human reply), `answered` (a human reply attached, not closed) or
    `closed`.
  - `UNANSWERED` counts the human replies (and edits) after the last coordinator post in that
    thread, and their event IDs follow as `REPLY_TO 1 TEAM Q<n> EVENT_ID` lines.
  - Without `--all`, closed questions are left out.
  - This is how a new coordinator picks up its predecessor's questions and answers (4.4).
- `pingbus history --room questions` shows the room as `history` shows the main room, through
  the room-aware validation of section 6.

## 9. What stays in the main room

Everything that is there today stays:

- pings and agent texts (`PEER`);
- workers' questions to the coordinator, and its answers and dispatches;
- humans' direct messages to any agent;
- membership notices, and the goodbye, farewell and disconnected notices (D8).

The main room is the team's working room. The owner can mute it in Element and follow only
reports and questions; the skill and `docs/agent-bus.md` say so. A reply the owner posts in the
main room without a mention is still logged as unheard there. The owner's guide therefore says
"answer questions in the questions room".

## 10. Agent-facing text

### 10.1 The skill (`SKILL.md`)

- The opening paragraph says a team has a **main room** for all agent traffic, a **reports
  room** for the humans to read, and a **questions room** between the coordinator and the
  humans.
- A new section, **Reports and questions**, says, in these terms:
  - "**Questions go to the coordinator, never to a human.** If you are a worker and need a
    decision, a fact or a permission, ask the coordinator with `pingbus say --to-coordinator`.
    Write to a human only to answer a human who wrote to you."
  - "**Reports are for the humans to read, not to answer.** `pingbus report` when something a
    human would want to know has happened: a milestone, a merge, a deploy queued, a decision
    you took on their behalf, a risk. Not routine progress, not a question, not a request.
    Nobody is woken by a report and nobody replies to one."
  - For the coordinator: "**Bubble up only what you cannot answer.** First answer from the
    plan, the repository, the history and your own judgement. When only a human can decide,
    ask with `pingbus ask-human`: one question per message, self-contained (a reader who has
    seen nothing else must be able to answer it). Give `Context:` (what is going on and why it
    matters), `Question:` (one question) and, where there are choices, `Options:` with your
    recommendation first and its trade-off. Name who asked with `--asked-by` and `--re`. Never
    paste a worker's question unedited, never bundle several, and never ask what you can find
    out."
  - For the coordinator: "**Every human message in the questions room is for you.** Answer it,
    then pass the answer to each agent that asked (`say --to <asker> --re <their question>`),
    then close the question (`pingbus thread Q<n> --close`). A `revises:` line means the human
    corrected an earlier answer: act on the revision, and tell the askers if you already passed
    the original on."
- Exit codes: 10 now also names a side room, and 4 `no-room` means the rooms are not deployed
  yet: carry on in the main room as before (skip the report; the coordinator asks the human in
  the main room), never "tell a human".

### 10.2 Session-start lines (`hooks.py`)

- `role_worker` and `role_worker_many` gain: "Ask the coordinator, never a human, when you need
  a decision."
- Two lines of their own, each said only once this member has joined the room it names
  (`rooms.json` state `join`), so deploy 1, which ships the member code before deploy 2 makes
  the rooms, sends no agent to a room it cannot use:
  - `role_questions`, for the coordinator: "in team T, questions only a human can answer go to
    the questions room with `pingbus ask-human`; every human message there is delivered to you.
    Run `pingbus questions` now and pass on any answer not yet passed on."
  - `role_reports`, for every role: "in team T, report what the humans should know with
    `pingbus report`."
- `questions_refused` is said only for the `human_text` refusal reason, not for any other
  refused or pending questions room.

### 10.3 Docs

- `docs/agent-bus-protocol.md`:
  - §2: the three new types and three content keys;
  - §8: the index, side rooms, their power levels (open and closed questions forms), join rules
    and the invite rule;
  - §9: the questions-room receive path, room-aware re-validation, edits and the attachment
    rules;
  - §10: the new limit and its bounds;
  - §12: the coordinator `human_text` rule;
  - §13: the new commands;
  - §15: the `HUMAN` line's appended field, and the `QUESTION`, `REPLY_TO` and `agent-bus list`
    `ROOM` lines.
    The protocol version stays 1: only new types, new line kinds and appended fields are added.
- `docs/agent-bus.md` (the owner's guide): the three rooms in Element, muting the main room,
  and answering in a thread, in the questions room.

## 11. Tests, plan scripts and acceptance (TDD order)

### 11.1 Unit tests, each committed failing before its code

Python tests go in `tests/helpers/pingbus/`, `tests/helpers/agent_bus/` and
`tests/helpers/agent_bus_lxc/`. They use the existing fakes (`fake_client_api.py`,
`fake_admin_api.py`) and stdlib `unittest`, and run under `./scripts/qa-helper-tests.bash`.

01. **`protocol`:**
    - `parse_rooms_index` (admin sender, keys, IDs, not the main room) and `parse_room_link`;
    - the reports power levels, and the open and closed questions forms;
    - the join-rule checks;
    - the `agent_bus.report`, `agent_bus.question` and `agent_bus.question_state` shapes;
    - the report and question format checks and caps (each section missing, each cap ±1 byte);
    - `validate_event(room="questions")`: unaddressed human messages accepted for a coordinator,
      refused for a worker; edits delivered with `revises`; an agent sender dropped.
02. **`inbox`:** a stored, unaddressed questions-room item survives `pending()` re-validation and
    is delivered. An item without the room key reads as `main`. A main-room item is written
    byte-identical to the old format (no `room` key), so an older reader accepts it.
03. **`syncer`:**
    - side-room trust for each failure (create, link, power levels, join rules), each leaving
      the main room trusted;
    - the reports join on reading the index;
    - the invite rule: a questions invite is accepted only by a coordinator with human text; an
      unindexed admin invite is left pending; other invites are rejected;
    - the filter with and without the questions room;
    - a demoted coordinator stops syncing the room at once;
    - each attachment rule, and `none`;
    - a gap fill of a limited questions-room timeline;
    - `history --room questions`.
04. **`status` and `hooks`:** a coordinator with `human_text: false` leaves the questions invite
    pending and both name the reason; its syncs still run.
05. **`cli`:**
    - `report`: caps, the gate, the hourly cap, duplicates, `no-room`;
    - `ask-human`: the role, numbering from fixture history, duplicates, mentions, `asked_by`
      resolution;
    - `thread` with `--ask` and `--close`;
    - `questions`: the states, `UNANSWERED` and `REPLY_TO`;
    - the `HUMAN` line's new field and `REPLY` command.
06. **`envelope`:** the `room:`, `question:` and `revises:` lines; `_FIELDS` for the longer
    `HUMAN` line.
07. **`limits`:** `report_per_hour` refused outside its bounds, never clamped.
08. **`hooks`:** the role lines.
09. **`admin`:**
    - `_ensure_side_rooms` from nothing, and a re-run with no change (no `changes`);
    - adoption after a crash between `createRoom` and the ID file, both through the index and
      through `/joined_rooms`; two candidates is an `AdminError`;
    - removed humans kicked from every room before the power levels change;
    - `_sync_questions_members` for every role transition: kicks before invites, every
      coordinator, a `leave` coordinator invited again, the open and closed forms, and the
      room created when the first coordinator appears;
    - `add_member` refusing a coordinator with `human_text` false (Phase 1);
    - `remove_member` kicking from both rooms;
    - a failure naming the step, and a re-run converging.
10. **`health`:** a read-only report of each side-room drift and of each coordinator not joined
    to questions.
11. **`cli` of the admin tool:** `agent-bus list` `ROOM` lines, with the HUMAN and MEMBER lines
    byte-identical to before.
12. **`members`:** an LXC coordinator entry with `human_text: false` is refused.

Go (`src-go/internal/agentbus`, `src-go/internal/teams/board`), test-first:

- the parser skips `ROOM` lines today, pinned by a test before Phase 2 ships;
- then the parser reads them, and each human's board row shows their membership of each room
  (`in all rooms`, or for example `questions: invited`).

### 11.2 Plan scripts (PlanScriptStandards R1-R14)

- **`spike.bash`** (Task 0.1): `plan_mode deploy`, `plan_require_host`.
  - It starts the installed, pinned Tuwunel binary as the invoking user. It listens on a
    loopback port, with a temporary config and data directory under the run directory, a
    generated registration secret and throwaway accounts.
  - It changes no system state: no unit, no team file, no play.
  - Its `plan_deploy_leg`s probe the restricted join, the power-0 refusal and pending invites.
    A report in the run directory records the facts.
  - `plan_on_cleanup` stops the process and removes the directory.
  - It is a spike, not triage: it starts a process, so it cannot be `gather` (R7).
- **`deploy.bash --stage 1|2`**: `plan_mode deploy`, `plan_require_host`, the legs of 5.3. Each
  stage ends by running `acceptance.bash --stage N` as its last leg (R9). `--check` rehearses the
  plays and skips acceptance, and says so.
- **`acceptance.bash`**: `plan_mode deploy`, because A3 and A4 take a seat and post a report,
  which are state changes (R7). `plan_require_host` applies except under `--fake`
  (`STANDARD-EXCEPTION(R2)`, as Plan 00194's acceptance does): `--fake` reads only the fixtures
  it writes. It is written and committed in Phase 1 (stage 1 legs and `--fake`) and extended in
  Phase 2 **before** the stage-2 `deploy.bash` that runs it.
  - **`--fake`** runs anywhere and changes nothing. It runs the verdict functions over fixture
    room states it writes, each with a negative fixture the verdict must refuse.
  - **Stage 1:** S1, the host pingbus is the new version; S2, every declared LXC member's
    in-container pingbus is the new version, and a container that is down is refused.
  - **Stage 2:**
    - **A1:** every installed team has a reports room, and every team with a coordinator has a
      questions room. The index and back-links agree. Power levels and join rules are exact,
      read as `admin` through the admin tool.
    - **A2:** every human is joined or invited to each side room. Every coordinator is joined to
      questions, and no other agent is. Every running member is joined to reports.
    - **A3:** with a worker's token, from a seat taken for the run and released through
      `plan_on_cleanup`:
      - `/messages` on questions, a join and a post are each refused (`M_FORBIDDEN`);
      - a `pingbus report` lands in reports, and no session's inbox grows.
    - **A4:** one LXC member's `pingbus report`, run through the guest path `members.py` uses,
      lands in reports.
  - **`--after-owner`** is the R9 exception: a human step lies between it and the deploy. After
    the owner's Element steps it runs **A5**: the coordinator's `pingbus history --room questions` shows the owner's thread reply and an edit as `in HUMAN` lines attached to the
    test question, and the coordinator's session received both as envelopes. A5 saves the raw
    events under `untracked/` for Task 4.2 to turn into anonymised fixtures.

### 11.3 The owner's steps (ACCEPTANCE-owner.md)

In Element:

1. Accept both invites.
2. See that the reports composer is disabled.
3. See a test report, and get no notification for it.
4. Get a notification for the coordinator's test question.
5. Answer it with "Reply in thread", then edit that answer.
6. Confirm that no react button is offered in the questions room.

Then `acceptance.bash --after-owner` runs through meta-deploy.

## 12. Security notes

- No new account and no new credential. Only `admin` creates rooms and writes the index,
  back-links and power levels. Agents cannot invite, change state or use `@room`.
- The homeserver keeps workers out of the questions room by membership. Where several seats
  share a checkout, a worker could read the coordinator's bundle (Plan 00203 D6), so this is a
  quiet channel, not a confidentiality wall; Plan 00207 §12.2 owns making it one.
- The restricted join grants the reports room to exactly the main room's members, which is the
  intent. A removed member is kicked from every room and deactivated.
- A human's authority is unchanged: only a `HUMAN` item, verified in `history`, carries it. An
  edit is delivered as a revision beside its original, never as a silent replacement.
- Nothing install-specific appears in any tracked file. Examples use `alpha`, `alice` and
  `myrepo`.
