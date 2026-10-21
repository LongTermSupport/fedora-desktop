# SPEC: a reports room and a questions room for every team

Design only. Nothing here is built. [PLAN.md](PLAN.md) holds the milestones and the decisions
the owner may want to change; this document is what they build.

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

Terms, as in the protocol: *main room* is today's team room (protocol §8, the bundle's
`room`); *record* is the admin-sent `agent_bus.team` state; *admin* is the team's
`@admin:<server_name>` account; *coordinator* is the agent the record's `roles` names
`coordinator`; *human* is a user ID in the record's `humans`.

## 1. What exists today (research)

| Fact                                                                                                                                                                                                                                                                   | Where                                                                                                                                                                                               |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| One room per team, created once by `admin` in `bootstrap` (`preset: private_chat`, room version 12, name = team, topic, power levels, the record as initial state). Its ID is kept in `/var/lib/agent-bus/<team>/room_id`; a second run re-checks the create event.    | `helpers/agent_bus/admin.py:559-581`, `:10`, `:68`                                                                                                                                                  |
| `bootstrap` is idempotent: humans' accounts, the room, removals of humans no longer listed, power levels, record and topic (each written only when it differs), and the humans' invites.                                                                               | `helpers/agent_bus/admin.py:584-600`, `:629-651`                                                                                                                                                    |
| A member is invited to the main room by `add_member`; `set_role` and `remove_member` re-sync the record; `remove_member` kicks and deactivates.                                                                                                                        | `helpers/agent_bus/admin.py:724-773`, `:798-827`, `:618-626`                                                                                                                                        |
| The play is a thin caller of `agent-bus-install`; `software` builds the zipapps and the member kit, then `team` runs `bootstrap` per team; LXC members are joined after the teams are installed, and their kit (with `pingbus`) is copied into each running container. | `playbooks/imports/play-agent-bus.yml:182-195`, `:272-333`, `:373-384`; `files/usr/local/sbin/agent-bus-install:387-395`, `:493-498`, `:831`; `helpers/agent_bus_lxc/members.py:1008-1015`, `:1230` |
| A ccy session's `pingbus` and the plugin come from the image build context, staged by the ccy play.                                                                                                                                                                    | `playbooks/imports/play-claude-yolo.yml:342-360`; `files/var/local/claude-yolo/claude-yolo:2096-2098`                                                                                               |
| The bundle names exactly one `room`; every member-side read and write uses `member.room`.                                                                                                                                                                              | `helpers/pingbus/config.py:53`, `:103`, `:519-541`; `helpers/agent_bus/admin.py:685-702`                                                                                                            |
| The syncer's `/sync` filter names only that room; an invite is accepted only to it, from `admin`, and every other invite is rejected with `POST /leave`.                                                                                                               | `helpers/pingbus/syncer.py:119-134`, `:137-160`, `:367-379`; protocol §8                                                                                                                            |
| Room trust: create event by `admin`, version 12, event ID derived from the room ID, power levels exactly `expected_power_levels(humans)`, a valid admin-sent record. A change to create, power levels or the record re-verifies.                                       | `helpers/pingbus/syncer.py:46-48`, `:295-313`; `helpers/pingbus/protocol.py:1021-1031`; protocol §8                                                                                                 |
| Power levels: humans 50, agents 0, `state_default` 100, `invite`/`kick` 100, `notifications.room` 50, `m.reaction` 50. `admin` holds creator power (not listed).                                                                                                       | `helpers/pingbus/protocol.py:161`, `:1021-1031`; protocol §8                                                                                                                                        |
| Humans and agents are told apart by the record alone: a sender in `humans` is `human`, an agent's class is its role; anyone else is dropped (`sender`). Human localparts have their own grammar; `admin` and `conduit` are reserved.                                   | `helpers/pingbus/protocol.py:587-594`, `:465`, `:60`, `:84`                                                                                                                                         |
| Two roles, `coordinator` and `worker`; `coordinators()` lists the holders; at most one is kept by the writers.                                                                                                                                                         | `helpers/pingbus/protocol.py:48-53`, `:564-567`; Plan 00196                                                                                                                                         |
| A human's message is delivered only when it mentions this agent (or `@room`); edits are dropped (`edit`); reply fallbacks are stripped. A human message no agent receives is logged as unheard.                                                                        | `helpers/pingbus/protocol.py:779-804`, `:889-903`; protocol §9                                                                                                                                      |
| A worker's text or ping to a human copies the coordinator automatically.                                                                                                                                                                                               | `helpers/pingbus/protocol.py:570-584`                                                                                                                                                               |
| `say` builds an agent text (`m.notice`, `agent_bus.text`), checks it, charges the send gate (token bucket, 60 s duplicate window), and posts to `member.room`.                                                                                                         | `helpers/pingbus/cli.py:1060-1096`; `helpers/pingbus/limits.py:26-47`; `helpers/pingbus/protocol.py:43-45`                                                                                          |
| Delivery into the session: the watcher syncs and writes each accepted item into the conversation through the inbox socket as an envelope with `kind`, `authority`, `reply:`.                                                                                           | `helpers/pingbus/cli.py:1926`; `helpers/pingbus/envelope.py:30`, `:54-71`; Plan 00194                                                                                                               |
| Session-start role lines tell each agent its role and its coordinator.                                                                                                                                                                                                 | `helpers/pingbus/hooks.py:134-142`, `:285-291`                                                                                                                                                      |
| The skill says the team is one room and teaches HUMAN, PEER, PING, `say --to`, `--to-coordinator`, and the coordinator and worker roles.                                                                                                                               | `files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md:8-12`, `:135-150`, `:206-230`                                                                                      |
| Output lines only ever gain fields at the end; exit codes are stable (4 refused, 9 rate limited or duplicate, 10 untrusted).                                                                                                                                           | protocol §14, §15                                                                                                                                                                                   |
| The homeserver allows room creation only by `admin` (Plan 00161 D63: "the only room the instance allows"), so all agent traffic stays auditable by the humans.                                                                                                         | `CLAUDE/Plan/00161-agent-team-bus-matrix/DESIGN.md:57`, `:378`                                                                                                                                      |
| ccy-teams shows each human's main-room membership on the board (`in room`, `not in room`).                                                                                                                                                                             | `src-go/internal/teams/board/snapshot.go:268`; `src-go/internal/teams/board/table.go:243-250`                                                                                                       |
| Element: each human's profile is per homeserver, so a new room on the same server needs no new sign-in; humans accept an invite in Element.                                                                                                                            | `helpers/agent_bus/element.py:1-23`                                                                                                                                                                 |

## 2. Relation to other plans

- **Plan 00207 (team roles), SPEC-roles §11 and §12-13.** It designs a coordinator-and-humans
  *management room* (terse capped briefs and `Q<n>` questions) and invite-only *channel rooms*
  kept by a reconciler. It is Not Started and waits on 25 owner decisions. This request
  differs from §11 in two ways: reports come from **any** agent, not only the coordinator,
  and questions are **long-form and self-contained**, not capped at 300 characters. This plan
  therefore **supersedes 00207 §11** and is the smaller, earlier step 00207 builds on:
  - 00207's `management` room becomes this plan's `questions` room, and its briefs become
    the coordinator's reports;
  - 00207's multi-room bundle and reconciler (M6a) take this plan's `agent_bus.rooms` index
    (section 4.1) as their starting point, not a second mechanism;
  - the `Q<n>` numbering read from room history, with no session state, is 00207 §11.2's idea,
    kept.
    A note in 00207's PLAN.md says so.
- **Plan 00209 (v2 in Go), unmerged, read at `1447d536`.** It keeps protocol 1 and every state
  file unchanged and ports v1 stage by stage (DESIGN §10.2). This plan extends v1, so the new
  event types, content keys, output columns and caps must enter 00209's protocol corpus (its
  §5.4) as requirements. PLAN.md records that for 00209's owner; 00209's branch is not edited
  here.
- **Plan 00161 D63** (one room, all auditable). Kept: every human of the team is in every room,
  and only `admin` creates rooms.
- **Plan 00194** (live delivery): the questions room reaches the coordinator through the same
  watcher and inbox socket. **Plan 00196** (coordinator): the role moves the questions-room
  membership. **Plan 00197** (seat broker) and **Plan 00205** (SSH members) add members
  through the same admin calls, so they inherit this.

## 3. The rooms

### 3.1 Shape

| Room      | Purpose key | Name (Element)       | Members                                  | Join rule                                    | Who may post                | Created         |
| --------- | ----------- | -------------------- | ---------------------------------------- | -------------------------------------------- | --------------------------- | --------------- |
| main      | (`room`)    | `<team>`             | humans, every agent (as today)           | invite (as today)                            | as today                    | exists          |
| reports   | `reports`   | `<team> · reports`   | humans (invited), any agent that reports | restricted: members of the main room join    | agents only (enforced, 3.2) | every team      |
| questions | `questions` | `<team> · questions` | humans, the coordinator                  | invite; `admin` invites the coordinator only | humans and the coordinator  | every team (D4) |

- **Purpose keys are fixed protocol strings**, `reports` and `questions`. Each team is its own
  homeserver, so the names need no team prefix inside the protocol; the Element name carries
  the team so the owner's room list reads well across teams.
- **No room aliases.** Members find the rooms by ID through the index (4.1), as they find the
  main room through the bundle today. An alias is one more thing to keep in step and to verify,
  and nothing needs it (D11).
- **Topics** (set by `admin`, informational, never trust):
  - reports: "Reports from the team's agents, for you to read. Nobody reads replies here: to
    reach an agent, write in the main room."
  - questions, with a coordinator: "Questions the coordinator `<handle>` could not answer
    itself. Answer here; the coordinator reads every message in this room."
  - questions, with none: "This team has no coordinator, so nobody reads this room now."
- **History visibility `shared`** for both (the `private_chat` default, set explicitly): a new
  coordinator must read the open questions (D13).
- **Encryption off, federation off**, as for the main room.

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
human's message there would reach no one and say nothing about it. That is a silent failure,
which the project's first rule forbids; making it impossible is the fail-fast answer. Agents
get their power from `users_default`, so the content depends on `humans` only, like the main
room's, and an agent joining changes nothing. `@room` and reactions are closed to everyone;
read receipts are not power-gated, so the owner's "seen" marks still work.

**Questions: exactly `expected_power_levels(humans)`** (protocol §8). Humans 50, the
coordinator 0 posting at `events_default` 0, `@room` for humans only. The homeserver keeps
workers out by membership, not power: they are never invited, the join rule is `invite`, and
an agent cannot invite (`invite` 100).

### 3.3 The questions room is a room, not a direct message

The owner asked that the coordinator-to-human channel be a real room so other agents can be
added later. It is an ordinary invite-only team room created by `admin`: adding another
account later is one invite and one membership rule in `rooms_for` (Plan 00207 §13.1), not a
new channel. A direct message between two accounts could not take a third, and the server
refuses agents' room creation anyway (Plan 00161 D63).

## 4. How members find and trust the rooms

### 4.1 The index in the main room

`admin` writes a new state event in the **main room**:

- type `agent_bus.rooms`, state key `""`, content `{"v": 1, "reports": "!…", "questions": "!…"}`
  (each key present once its room exists; no other key).
- `state_default` 100 already means only `admin` can write it.
- A receiver reads it only when sent by `admin`, `v` is 1, the keys are a subset of the two
  purpose keys, and each value is a room ID other than the main room's.

Why a new event and not a record key or a bundle field:

- An unknown **record** key makes an older pingbus distrust the whole main room (protocol §8;
  Plan 00209 R41). A new state **type** is outside the older syncer's filter
  (`syncer.py:48-49`), so an older member never reads it and loses nothing.
- A **bundle** field would mean re-issuing every member's bundle. The index reaches every
  member, LXC and ccy included, on its next sync, with no re-issue.

### 4.2 The back-link in each side room

Each side room carries `agent_bus.room`, state key `""`, sent by `admin`, content
`{"v": 1, "team": "<team>", "purpose": "reports"|"questions", "main": "<main room ID>"}`, as
initial state at creation. A side room is trusted only when the index names it **and** its
back-link names the same purpose and this member's main room, so neither room can be swapped
for another `admin` room on its own.

### 4.3 Side-room trust (checked on join and on every change to a trust type)

- The create event: sender `admin`, version 12, no `additional_creators`, event ID derived
  from the room ID (as protocol §8 for the main room).
- The back-link (4.2).
- Power levels exactly as section 3.2 for its purpose, against the **main room's** record's
  `humans`.
- `m.room.join_rules` exactly: reports
  `{"join_rule": "restricted", "allow": [{"type": "m.room_membership", "room_id": "<main>"}]}`,
  questions `{"join_rule": "invite"}`.
- The trust types of a side room are the main room's plus `m.room.join_rules` and
  `agent_bus.room`. A side room that fails is untrusted on its own: `report` or `ask-human`
  exits 10 naming it, the coordinator stops syncing it and `pingbus status` shows why. The main
  room stays trusted.

### 4.4 Joining

- **Reports.** A member joins on its first `pingbus report` (`POST /join/<id>`), allowed by the
  restricted join rule because it is in the main room. No invite, so no per-agent membership to
  keep in step, and an older pingbus never sees an invite it would reject. Humans are invited
  by `admin` so the room shows up in Element.
- **Questions.** `admin` invites the coordinator. The coordinator's syncer accepts an invite to
  a room when the inviter and stripped create sender are `admin`, the trusted index names it as
  `questions`, and the record names this member `coordinator`.
- **An admin invite the index does not (yet) name is left pending, logged by room ID, never
  joined and never rejected.** Rejecting would need a re-invite. Every other invite is still
  rejected, as today.
- **Demotion.** `admin` kicks the old coordinator from the questions room (reason "no longer the
  coordinator") before it invites the new one, the order Plan 00196's LXC handover uses. A
  member whose record no longer names it coordinator stops syncing the questions room at once,
  before the kick arrives.

## 5. Creation and migration (IaC)

### 5.1 The admin side

- `bootstrap` gains `_ensure_side_rooms` after `_ensure_room`: for each purpose, create the room
  if its ID file (`room_id.reports`, `room_id.questions`, beside `room_id`) is absent, else
  re-check its create event and back-link; write the index in the main room when it differs;
  sync each side room's power levels, join rules and topic when they differ; invite every human
  to both rooms when not joined or invited.
- **The coordinator's questions membership** is synced by one function,
  `_sync_questions_members`, called from `bootstrap`, `add_member`, `set_role`, `park_member`
  and `remove_member`: kick every agent account in the room that the registry does not name
  coordinator, then invite the coordinator if it is neither joined nor invited. Kicks before
  invites.
- `remove_member` also kicks from the reports room. Parking keeps the account and membership,
  and revokes the token, as today.
- Each change appends to `changes`, so the installer prints `CHANGED` only on change and the
  play's `changed_when` is unchanged.
- A failed homeserver call stops `bootstrap` with the step named (`AdminError`), as today. A
  re-run converges because every step compares first.

### 5.2 Existing teams

Nothing to migrate by hand. The next `play-agent-bus.yml` run (through `ccy-teams apply` or
`meta-deploy.bash`) creates both rooms for every team present, writes the index, invites the
humans and the coordinator. The main room keeps its ID, history and members.

### 5.3 Upgrade first (D12)

A member's pingbus must understand the index before an invite reaches it, or an older
coordinator rejects the questions invite with `POST /leave`. So this ships in **two deploys**,
the rule Plan 00196 followed for the coordinator role:

1. **Deploy 1, member side only.** The new pingbus (zipapp, LXC kit, ccy build context, the
   plugin's skill text). No room is created: the admin code is not yet merged. The owner then
   restarts the team's sessions (ccy containers pick up the new image, LXC containers take the
   kit on the play run, host sessions on relaunch).
2. **Deploy 2, admin side.** Rooms, index, invites.

A coordinator that is still old at deploy 2 rejects its invite. `bootstrap` then invites it
again on every apply, which prints `CHANGED invited <handle> to questions` each time, and the
health check names it ("the coordinator has not joined the questions room; its pingbus may be
older than the rooms change"). That repeat is the drift signal, not noise to suppress.

### 5.4 Member kinds

- **LXC members (top priority).** Their `pingbus` arrives in the kit copied by
  `members.py` `_sync_kit` during the play run, so deploy 1 reaches every running container.
  A container that is down is skipped and said so (`play-agent-bus.yml:384`); it is upgraded on
  the next run with it up, and deploy 2 waits for that (the acceptance checks it, section 11.2).
  An LXC coordinator syncs from inside its container like any member: no host-side change.
- **ccy members.** The new zipapp and plugin go into the image build context. Task 1.9 confirms
  how a new zipapp reaches running and new ccy images, and makes the version bump that
  `CLAUDE/ContainerRules.md` requires if one is needed.
- **Host `cc` sessions** use the host's `/usr/local/bin/pingbus` and take it on relaunch.

## 6. Who is woken by what

| Event                                   | Who is woken                                           | Delivered as                                  |
| --------------------------------------- | ------------------------------------------------------ | --------------------------------------------- |
| Any message in the reports room         | nobody (no agent syncs the reports room)               | nothing                                       |
| A human's message in the questions room | the coordinator, mention or not                        | `HUMAN`, `room: questions`, with its question |
| The coordinator's own post in questions | nobody (self)                                          | nothing                                       |
| Any other agent's post in questions     | nobody: dropped `sender` (cannot happen without drift) | nothing                                       |
| Main-room traffic                       | as today                                               | as today                                      |

- The coordinator's `/sync` filter names the main room and, while it is coordinator and the
  room is trusted, the questions room. One sync token covers both.
- A human's message in the questions room is a `HUMAN` item with the human's full authority,
  verified as today (`history` shows it, the consequential-request rule applies).
- Edits there are dropped as `edit`, as in the main room (D6). The skill tells the coordinator
  to ask the human to send a new message if an edit is reported in its drop counts.

## 7. Reports: `pingbus report`

`pingbus [--team T] report` posts the text on stdin to the team's reports room.

- **Who:** any agent holding a role. A human cannot (power levels).
- **Form:** first line a headline of at most 120 characters, then an optional body; at most
  1500 bytes in all (D10). Over the cap is refused (exit 4, `body`), never truncated. The
  secret-shape check and the member's own token check of `say` apply.
- **Wire:** `m.room.message`, `msgtype` `m.notice`, `body` the text, content key
  `agent_bus.report` `{"v": 1}`, **no mentions**. Matrix's default push rule
  `.m.rule.suppress_notices` means Element never notifies a notice, so reports are quiet by
  construction.
- **Limits (reuse):** charged to the member's existing send gate (`limits` token bucket,
  `admit_send` with a `report` gate verb), plus `report_per_hour` (default 6, bounds 1-60,
  per member, kept in the outbox's `gate`); a report equal to the member's previous report
  within 24 hours is refused as a duplicate (exit 9). The bucket and window are the ones `say`
  uses, so no new rate machinery.
- **Output:** a `SENT` line as `say` prints.
- **What belongs there**, from the skill: a milestone reached, a merge, a deploy queued, a
  decision taken on the owner's behalf, a risk found, something blocked that the human may
  want to know about. **Never a question**, never a request, never a narrative of routine
  steps.
- **The coordinator** is the usual reporter (a summary when a human-visible state changes),
  but any agent may report. A worker's report does not copy the coordinator: nobody is woken.

## 8. Questions

### 8.1 The flow

1. A worker with a question asks the coordinator on the main room, as now:
   `pingbus say --to-coordinator`.
2. The coordinator answers what it can from the plan, the repository, the team's history and
   its own judgement, with `say --to <worker> --re <event>`.
3. Only what it cannot answer, and what genuinely needs a human, it asks with
   `pingbus ask-human`: one question per message, long-form and self-contained.
4. The owner answers in the questions room. The coordinator is woken for every message there.
5. The coordinator dispatches the answer to each asking agent on the main room
   (`say --to <worker> --re <the worker's original event>`), then closes the question in its
   thread (`pingbus thread Q<n> --close`).

### 8.2 `pingbus ask-human`

`pingbus [--team T] ask-human [--asked-by NAME[,NAME…]] [--re EVENT_ID]` posts the question on
stdin.

- **Who:** the coordinator only (exit 4, `role`, for anyone else).
- **Form, checked:** first line a headline of at most 120 characters; then sections that start
  with `Context:` and `Question:` (both required) and `Options:` (optional, numbered `1.` to
  `9.`); at most 8000 bytes (D10). A missing section is refused (exit 4, `body`) naming it.
  This makes "self-contained" a format, not a hope: the reader needs no other room.
- **Numbering:** pingbus prefixes the headline with `Q<n>`, `n` one more than the highest `Q<n>`
  among the coordinator account's `ask-human` posts in the room's history (Plan 00207 §11.2's
  rule: no session state, so a relaunched or replacement coordinator numbers the same way).
- **Open cap:** refused (exit 9) while `max_open_questions` (default 5, bounds 1-20) questions
  have no human reply, naming them. One-at-a-time pressure is the point.
- **Duplicates:** a question whose `Question:` section equals an open question's is refused
  (exit 9) naming that `Q<n>`.
- **Wire:** `msgtype` `m.text` (a notice would be silenced by `.m.rule.suppress_notices`),
  `m.mentions.user_ids` = every listed human, so Element notifies. Content key
  `agent_bus.question` `{"v": 1, "n": <int>, "asked_by": [<user IDs>], "re": [<event IDs>]}`.
  `asked_by` and `re` are the coordinator's record of whom to answer; each `--asked-by` name is
  resolved as `say --to` resolves it, and each `--re` must be an event in the main room.
- **Output:** `SENT` plus a `QUESTION 1 TEAM Q<n> EVENT_ID` line.

### 8.3 Threads, replies and attaching an answer (D5)

Each question is a **thread root**. Threads keep the room's main timeline at one line per
question, which is the quiet the owner asked for, and keep each question's back-and-forth
together.

A human message is attached to a question by the first rule that holds:

1. its `m.relates_to` is `rel_type: m.thread` with `event_id` a question (Element's
   "Reply in thread");
2. its `m.in_reply_to` names a question or a post in a question's thread (Element's "Reply");
3. its body starts `Q<n>` (any case, then a space, `:` or `.`) naming a question;
4. otherwise it is **unattached**, still delivered, and shown with `question: none` so the
   coordinator decides. The owner may start a topic here unprompted (D15).

The reply fallback is stripped as today. The delivery envelope gains two lines after `team:`:
`room: questions` and `question: Q<n> "<headline>"` (or `question: none`). `recv` and `wait`
append one field to the `HUMAN` line, `ROOM` = `-` (main room) or `questions:Q<n>` or
`questions:-` (protocol §15: fields are only appended). The `REPLY` command for a questions-room
item is `pingbus --team T thread Q<n>` (or `thread --re 'EVENT_ID'` when unattached).

### 8.4 `pingbus thread` and `pingbus questions`

- `pingbus thread Q<n> [--close] [--re EVENT_ID]` (coordinator only): the text on stdin into the
  question's thread, e.g. a follow-up ("I need the cost limit as a number") or the dispatch note
  ("Passed to myrepo.2 and myrepo.4; closing."). It mentions the humans only when it asks
  something (`--ask`), so a plain note does not notify. `--close` marks the question closed with
  content `agent_bus.question_state` `{"v": 1, "n": <int>, "state": "closed"}` in the same
  message; the text may be empty with `--close`. At most 4000 bytes; the send gate applies.
- `pingbus questions [--all]` (coordinator; read-only): one line per question from the room's
  history, `QUESTION 1 TEAM Q<n> STATE EVENT_ID ASKED_BY`, `STATE` one of `open` (no human
  reply), `answered` (a human reply attached, not closed) and `closed`. Without `--all`, open and
  answered only. It is how a new coordinator picks up its predecessor's questions.
- `pingbus history --room questions` shows the room as `history` shows the main room.

## 9. What stays in the main room

Everything that is there today: pings, agent texts (`PEER`), workers' questions to the
coordinator, the coordinator's answers and dispatches, humans' direct messages to any agent,
membership notices, goodbye, farewell and disconnected notices (D8). The main room is the
team's working room. The owner can mute it in Element and follow reports and questions only;
the skill and `docs/agent-bus.md` say so.

## 10. Agent-facing text

### 10.1 The skill (`SKILL.md`)

- The opening paragraph: a team has a **main room** for all agent traffic, a **reports room**
  for the humans to read, and a **questions room** between the coordinator and the humans.
- A new section, **Reports and questions**, which says, in these terms:
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
    seen nothing else must be able to answer it), with `Context:` (what is going on and why it
    matters), `Question:` (one question) and, where there are choices, `Options:` with your
    recommendation first and its trade-off. Name who asked with `--asked-by` and `--re`. Never
    paste a worker's question unedited, never bundle several, and never ask what you can find
    out."
  - For the coordinator: "**Every human message in the questions room is for you.** Answer it,
    then pass the answer to each agent that asked (`say --to <asker> --re <their question>`),
    then close the question (`pingbus thread Q<n> --close`)."
- Exit codes: 10 now also names a side room.

### 10.2 Session-start lines (`hooks.py`)

- `role_coordinator` gains: "Questions only a human can answer go to the questions room with
  `pingbus ask-human`; every human message there is delivered to you. Report what the humans
  should know with `pingbus report`."
- `role_worker` and `role_worker_many` gain: "Ask the coordinator, never a human, when you need
  a decision. Report what the humans should know with `pingbus report`."
- `role_worker_alone` gains only the report sentence.

### 10.3 Docs

- `docs/agent-bus-protocol.md`: §2 the three new types and three content keys; §8 the index,
  side rooms, their power levels and join rules, the invite rule; §9 the questions-room receive
  path (every human message delivered to the coordinator, attachment rules); §10 the new limits
  and bounds; §13 the new commands; §15 the `HUMAN` line's appended field and the `QUESTION`
  line. Protocol version stays 1 (new types and appended fields only).
- `docs/agent-bus.md` (the owner's guide): the three rooms in Element, muting the main room,
  answering in a thread.

## 11. Tests and acceptance (TDD order)

### 11.1 Unit tests, each committed failing before its code

Python, `tests/helpers/pingbus/` and `tests/helpers/agent_bus/`, against the existing fakes
(`fake_client_api.py`, `fake_admin_api.py`), stdlib `unittest`, run by
`./scripts/qa-helper-tests.bash`:

1. `protocol`: `parse_rooms_index` (admin sender, keys, IDs, not the main room);
   `parse_room_link`; `expected_reports_power_levels(humans)`; join-rule checks; the
   `agent_bus.report`, `agent_bus.question` and `agent_bus.question_state` shapes; the report
   and question format checks and caps (each section missing, each cap ±1 byte).
2. `syncer`: side-room trust for each failure (create, link, power levels, join rules), each
   leaving the main room trusted; the invite rule (questions invite accepted only as
   coordinator; an unindexed admin invite left pending; others rejected); the filter with and
   without the questions room; a demoted coordinator stops syncing it at once; every human
   message in questions delivered unaddressed; each attachment rule and `none`; an agent post in
   questions dropped `sender`; edits dropped.
3. `cli`: `report` (join on first use, caps, gate, hourly cap, 24 h duplicate, a human refused
   is impossible but a missing index is exit 10); `ask-human` (role, numbering from fixture
   history, open cap, duplicate, mentions, `asked_by` resolution); `thread` and `--close`;
   `questions` states; `history --room`; the `HUMAN` line's new field and `REPLY` command.
4. `envelope`: `room:` and `question:` lines; `_FIELDS` for the longer `HUMAN` line.
5. `limits`: the two new bounds, refused outside them, never clamped.
6. `hooks`: the role lines.
7. `admin`: `_ensure_side_rooms` from nothing, re-run with no change (no `changes`), each drift
   corrected; `_sync_questions_members` for every role transition with kicks before invites;
   `remove_member` kicking from both; a failure naming the step and a re-run converging.
8. `health`: a read-only report of each side-room drift.

Go (`src-go/internal/teams/board`), test-first: the human row shows membership of each room
(`in all rooms`, or the rooms not joined, e.g. `questions: invited`), from a fixture
`agent-bus list` output.

### 11.2 `acceptance.bash` (PlanScriptStandards R1-R14)

`plan_mode gather`, `plan_require_host`, a run report in the run directory. Legs:

- **`--fake`** (runs anywhere, changes nothing): the verdict functions over fixture room states
  it writes, each with a negative fixture the verdict must refuse.
- **A1** every installed team has both rooms, the index and back-links agree, power levels and
  join rules are exact (read as `admin` through the admin tool's read path).
- **A2** every human is joined or invited to both rooms; the coordinator is joined to questions;
  no other agent is.
- **A3** with a worker's token (from a seat the acceptance takes for the run and releases):
  `/messages` on questions is refused (`M_FORBIDDEN`), a join is refused, a post is refused; a
  `pingbus report` lands in reports and no session's inbox grows.
- **A4** every declared LXC member's in-container `pingbus version` is the new one, and one LXC
  member's `pingbus report`, run through the guest path `members.py` uses, lands in reports.
- **A5** after the owner's Element steps (ACCEPTANCE-owner.md), `--after-owner`: the
  coordinator's `pingbus history --room questions` shows the owner's thread reply as an `in HUMAN` line attached to the test question, and the coordinator's session received it as an
  envelope.

### 11.3 The owner's steps (ACCEPTANCE-owner.md)

In Element: accept both invites; see the reports composer disabled; see a test report and get
no notification for it; get a notification for the coordinator's test question; answer it with
"Reply in thread"; then run `acceptance.bash --after-owner` through meta-deploy.

## 12. Security notes

- No new account and no new credential. Only `admin` creates rooms and writes the index, back-
  links and power levels; agents cannot invite, change state or `@room`.
- The homeserver keeps workers out of the questions room by membership. Where several seats
  share a checkout a worker could read the coordinator's bundle (Plan 00203 D6), so this is a
  quiet channel, not a confidentiality wall; Plan 00207 §12.2 owns making it one.
- A human's authority is unchanged: only a `HUMAN` item, verified in `history`, carries it.
- Nothing install-specific appears in any tracked file; examples use `alpha`, `alice`,
  `myrepo`.
