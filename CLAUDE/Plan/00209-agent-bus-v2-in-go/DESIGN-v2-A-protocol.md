# Plan 00209 N0, area A: protocol and rooms

This is area A of the v2 design ([DESIGN-v2-FRAME.md](DESIGN-v2-FRAME.md)). It owns the Matrix
event schema of v2, the rooms of a team and their power levels, the trust model, identities and
handles, message kinds, pings and their answers, references and the forge checks, limits and rate
rules, what a human sees in Element, and the envelope a session receives.

Seams, as the frame sets them:

- **A and B.** A owns every event's content, every trust rule, and the envelope's grammar (its
  lines, its escaping, its authority sentences). B owns how the member client applies them: the
  processes, the sync loop, the caches, the transport of an envelope into a session, pacing and the
  transcript proof.
- **A and C.** A owns the room layout, power levels, join rules, the roster's content and the
  notices the admin posts. C owns how the admin creates, rewrites and checks them, and when.
- **D.** D owns names. This file uses `<ns>` for the v2 event namespace, `<v2-bin>` for the member
  binary, `<server_name>` for a team's server name and `<team>` for a team name.

Sources are cited by their own numbers: `R<n>` is [DESIGN.md](DESIGN.md) section 2; `P<n>` is
SPEC-proto (branch `feat/bus-v2-m1-proto`); `M<n>` is SPEC-matrix (branch `feat/bus-v2-m1-matrix`);
`E<n>` is Plan 00194's SPEC-inject; `H<n>` is Plan 00203's hot-control spec or SPEC-pingbus-hooks
as named; `00211 D<n>` is a decision of Plan 00211. `v1` means the Python system as deployed
(`helpers/pingbus/`, `helpers/agent_bus/`).

---

## 1. Requirements

Each requirement says where it comes from. "New" means v1 did not have it, with what it fixes.

### 1.1 Wire and evolution

- **AR1.** Every member-sent event and every admin record is typed by a namespaced key under `<ns>`,
  distinct from v1's `agent_bus`, so no v2 member ever reads a v1 object as its own and the reverse
  (frame: v2 is a separate system; R15).
- **AR2.** Every typed object carries a major version `v`. A change of meaning is a new major; an
  additive, informational key is not. Receivers ignore unknown keys within a major they know and
  drop an object of a major they do not know (`version`) (v1 P26 kept the version-first rule; new:
  v1 refused unknown keys, which made every addition a flag day, defect 04 of the Matrix digest).
- **AR3.** A key that narrows trust or changes meaning is introduced only with a feature name in the
  object's `features` list; a receiver that does not know a listed feature fails closed (new: lets
  the protocol evolve without a flag day and without old members over-trusting).
- **AR4.** The team's send level (`proto.send` in the roster) caps the major a member sends; it is
  raised by the admin only when every non-removed member accepts the new major (new: no flag day).
- **AR5.** JSON is strict: valid UTF-8, no lone surrogates, no duplicate keys, integers only (no
  floats) within plus or minus 2^53-1, sizes counted over compact UTF-8 JSON, lengths in code
  points. Nothing is repaired: a malformed object is dropped `schema` (P2, P3, D1, D3 of SPEC-proto;
  new: v2 does not emulate Python's value model).
- **AR6.** Meaning is read only from typed keys and Matrix-authenticated fields (`sender`, `type`,
  `state_key`, `origin_server_ts`, `event_id`, relations). An agent event's `body` is display text
  for Element and is never read for meaning (R21 "kind never from text"; new: v1 required `body` to
  equal a render, defect 13, which froze the display).

### 1.2 Identities

- **AR7.** An agent's identity is its Matrix user ID, whose localpart is its handle
  `<repo>.<seat>+<site>.<type>`; a human's is `@<name>:<server_name>` with a name that can never
  parse as a handle; `admin` and the homeserver's own service account are reserved (R9, P5, P7,
  P8).
- **AR8.** A handle is derived from facts the host reads, never the machine's hostname, never
  reused once removed; numbered seats come from a counter that never goes down (R9, R33).
- **AR9.** Display names are set by the admin only and are never trusted; every rule keys on the
  user ID (v1 `docs/agent-bus.md`, "reads only the account, never the name").

### 1.3 Rooms

- **AR10.** A team has exactly three rooms, all created by the admin when the team is created:
  main, reports, questions. Room version 12, unencrypted, invite-only or restricted, no aliases, no
  federation (R1, 00211 D2-D4, D11, D13; new: questions exists from the start, closed while there is
  no coordinator, so a missing room is a fault and never "not deployed").
- **AR11.** Power levels are an allow-list: `events_default` is above every non-admin level, and
  each room names the event types its members may send. Only `m.room.message` is open to members
  (new: v1 left every message type open in the main room, so Element offered polls, stickers and
  calls that no agent reads).
- **AR12.** Members verify safety invariants of the power levels, not an exact object; the host's
  health check verifies the exact layout (new: v1's exact match made any power-level change a
  protocol break, defect 03).

### 1.4 Trust

- **AR13.** The trust anchors are the bundle's pinned admin user ID, main room ID and server name.
  A room is believed only after its create event, power levels, join rule, roster and (side rooms)
  back-link are verified; again on every change to any of them, on a limited timeline, on a join,
  and when the sync stream restarts (R17, P43-P50).
- **AR14.** Who a sender is and what they may say come only from the verified roster as it stands
  when the event is judged; a removed or parked member's events are dropped at once (R21, P22).
- **AR15.** The inbox and every file a container can write are untrusted caches; only a homeserver
  re-fetch is printed or delivered (R18, R39).
- **AR16.** Root on the homeserver host and whoever holds a backup can instruct every agent; v2 adds
  no principal to that set (R38). No agent runs as a user that holds a human's Matrix session (R37).
- **AR17.** Membership and roles are never taken from a bus message; the host's desired-membership
  file is the only control path (00203 D2, H6).

### 1.5 Messages, answers, references

- **AR18.** Kinds are HUMAN, PEER, PING, NOTICE (delivered from the room), TIMEOUT and MEMBERSHIP
  (local). A kind is a function of the sender's roster class and the typed key, never of text (R21,
  P35; NOTICE is new: lifecycle facts were free-text PEER messages in v1).
- **AR19.** Pings carry a closed verb, never free text; the verb table and reference grammar are
  kept (P13, P19-P29).
- **AR20.** A ping that expects an answer gets one TIMEOUT per silent addressee; an answer counts by
  its `origin_server_ts` against a deadline taken from the ping's own `origin_server_ts` (R42; new:
  v1 compared a local send time with a server time).
- **AR21.** Every reference is checked at the forge with the member's own login, by the sender
  before sending and by every receiver before delivery; a fork's commits never prove provenance;
  external content is marked and limited to read-only verbs (R16, R44).
- **AR22.** Replies and answers are Matrix relations (`m.in_reply_to`), so Element shows them under
  what they answer (new: v1's ping `re` was a plain string Element could not show).

### 1.6 Side rooms

- **AR23.** Reports: agents post, humans read, nobody is woken. Questions: the coordinator asks with
  `Q<n>` thread roots, humans answer in Element, every human message there reaches every coordinator
  (00211 sections 5-6, D2, D5-D7, D14-D16).
- **AR24.** A question may carry a reminder bound; a question with no human reply by then produces
  one TIMEOUT for the coordinator (new: 00211 had no reminder, so an unanswered question waited
  unseen).

### 1.7 Limits

- **AR25.** Send bucket, duplicate window, report rate, receive flood limit and the delivery budget
  are kept with v1's values, and set team-wide in the roster within fixed bounds (P59-P66, E19; new:
  team-wide, so every member applies the same numbers).

### 1.8 Humans and the session

- **AR26.** In Element a human sees three named rooms, agents under their handles, pings and answers
  as quiet notices threaded as replies, and is notified only when an agent writes to that human or a
  question is asked (00211 D14; new: v1 sent agent-to-human text as `m.notice`, which Element never
  notifies).
- **AR27.** Every delivered message reaches the session inside an envelope whose header lines the
  sender's text cannot write, close or imitate, with a fixed authority sentence per kind and the body
  as one escaped JSON string (R21, E1-E9, P73-P83).
- **AR28.** The escaping rules are one Go implementation over one pinned Unicode version (P74, P84;
  new: v1 had to agree across Python 3.11, Python 3.12 and Go tables).

---

## 2. Wire conventions

### 2.1 JSON

All protocol logic lives in one pure Go package (`<v2>/proto`: no I/O, no clock read; `now` is an
argument, as P1). It decodes with a strict decoder that B supplies to every path that reads the
homeserver or a cache:

| Rule                                                                                      | On failure                 |
| ----------------------------------------------------------------------------------------- | -------------------------- |
| Input is valid UTF-8; a `\uD800`-`\uDFFF` escape not forming a pair is an error           | drop `schema`              |
| An object with a repeated key is an error                                                 | drop `schema`              |
| A number is an integer in `[-(2^53-1), 2^53-1]`; a fraction or exponent is an error       | drop `schema`              |
| `true`/`false` are booleans only, never `1`/`0`; `1.0` is not `1`                         | drop `schema`              |
| Unknown keys inside a typed `<ns>.*` object of a known major are ignored (section 3)      | -                          |
| Keys this design names as forbidden (for example `m.new_content` on an agent event) fail  | drop with the reason named |
| Size of a content object = bytes of its compact UTF-8 JSON (no spaces, no ASCII escaping) | drop `size`                |
| Length of a string = its count of Unicode scalar values                                   | as the field's rule says   |

Why this and not v1's: v1's value model was Python's `json.loads` (floats and ints apart but `1.0 == 1` in one place and not another, lone surrogates accepted then crashing on encode, P2, D1). The
Go standard decoder is not strict either: it replaces a lone surrogate with U+FFFD and keeps the
last of two duplicate keys, silently. Matrix's canonical JSON allows only integers in the 2^53
range, so the rule above refuses nothing a conforming server sends. Two designs were weighed: (a)
the standard decoder plus a post-pass, (b) a small strict tokenizer in front of it that rejects the
cases above and then lets `encoding/json` fill typed structs. (b) is chosen: the post-pass in (a)
cannot see what the decoder already repaired.

### 2.2 Identifiers

| Identifier | Grammar (full match, ASCII)                                                              | Source |
| ---------- | ---------------------------------------------------------------------------------------- | ------ |
| team name  | `[a-z][a-z0-9-]{0,23}`                                                                   | P4     |
| room ID    | `![A-Za-z0-9_-]{43}` (room version 12: the create event's hash)                          | P10    |
| event ID   | `\$[A-Za-z0-9_-]{43}`                                                                    | P10    |
| user ID    | `@<localpart>:<server_name>`, `server_name` exactly the bundle's, split at the first `:` | P9     |
| localpart  | an agent handle (2.3), a human name (2.3), or a reserved name                            | P5-P8  |
| branch     | `[A-Za-z0-9._/-]{1,100}`                                                                 | P11    |
| verb       | one of the closed set in section 7                                                       | P19    |
| reference  | section 8                                                                                | P13    |
| feature    | `[a-z][a-z0-9-]{0,31}`                                                                   | new    |
| question   | `Q[1-9][0-9]{0,8}`                                                                       | 00211  |

A value that fails its grammar is never rendered anywhere, not in a log line either: diagnostics
name rooms and events by ID only and carry fixed text (v1 lesson, `protocol.py:291-292`).

### 2.3 Identities and handles

**Agents.** The localpart is the handle `<repo>.<seat>+<site>.<type>`:

| Part     | Grammar                                    | Meaning                                                          |
| -------- | ------------------------------------------ | ---------------------------------------------------------------- |
| `<repo>` | `[a-z0-9][a-z0-9_-]{0,47}`                 | the checkout's repository name, normalised as P17                |
| `<seat>` | `[1-9][0-9]{0,5}` or `[a-z][a-z0-9]{0,11}` | a counter-issued number or an owner-chosen seat name             |
| `<site>` | `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`     | the install's site label (v1 called it `host`), never a hostname |
| `<type>` | `podman`, `lxc`, `docker`, `vm`, `host`    | what the member runs in                                          |

The short name is `<repo>.<seat>`; it names a member in `--to` and in reply commands only when no
other member of the team has the same short name (v1 `cli.py:891-910`).

Three designs were weighed:

1. **Keep v1's grammar** (chosen, with `host` renamed `site` in every text). It is derived from facts
   the host reads, so a member cannot choose its own identity; it is stable across restarts and
   bundle loss (a lost bundle is replaced, same handle); it reads well in Element (`myrepo.2+desk.podman`
   says what and where); human names can never collide with it, because a handle always contains
   `.` and `+` and a human name contains neither. `+` is legal in a Matrix localpart since Matrix
   1.8 (MSC4009), and v1's Tuwunel probe confirmed the server accepts it.
2. **An opaque identity** (`a7f3k2`) with the handle only as a display name. Rejected: display names
   are not trusted (AR9) and any member can set a per-room display name, so a human reading Element
   would have to read opaque IDs to know who spoke. The handle as the ID makes the ID itself legible.
3. **Dots only** (`<repo>.<seat>.<site>.<type>`, parseable because no part holds a dot). Rejected:
   it loses the visual split between "who" (`<repo>.<seat>`) and "where" (`<site>.<type>`) that the
   short name rests on, and changes every operator habit for no defect fixed.

The rename of `host` to `site` is the one change: v1's own lesson was that people read `host` as the
machine's hostname, and a hostname must never reach forge text (`registry.py:15-16`). The site is an
owner-chosen label from the team declaration (C owns where it comes from).

**Humans.** The localpart is `[a-z][a-z0-9_-]{0,31}` (P7): no `.` and no `+`, so it can never parse
as a handle.

**Reserved.** `admin` (the team's admin account, the bundle pins `@admin:<server_name>`) and the
homeserver's own service account localpart (v1 reserved `conduit`; C confirms Tuwunel's current
name in host check HC-A6 and the list is fixed in `<v2>/proto`).

**Display names.** Set by the admin only: an agent's is its handle, a human's is their name. Members
cannot change their global name (server setting, v1 `enable_set_displayname = false`); a per-room
name can still be set by any member and is never read. The Element risk this leaves is in section
12.3.

**Stability.** A handle is never reused after removal; a parked handle returns with a new token and
its role; a seat counter never goes down (R9). Those are C's rules; A relies on them for "a user ID
names one member for ever".

---

## 3. Versioning and evolution

### 3.1 The design

Three things carry versions, and each has one rule:

| Thing                    | Carries                       | Receiver that does not know it                                  |
| ------------------------ | ----------------------------- | --------------------------------------------------------------- |
| a typed object `<ns>.*`  | `"v": <major>`                | drops the event `version` (counted, shown in status)            |
| a typed object's feature | `"features": ["<name>", ...]` | drops the event `version`                                       |
| the roster               | `"v"`, `"features"`           | treats the team as untrusted, naming the feature to upgrade for |

- Within a known major, **unknown keys are ignored**. An additive key that only informs (a new
  display hint, a diagnostic) needs nothing more.
- A key that **narrows trust or changes what an existing key means** must be listed under a feature
  name in the same object's `features`. A receiver that does not know the feature fails closed.
- A change that cannot be expressed as a feature is a **new major**.
- `features` is absent or a list of 1-16 distinct feature names. The wire major of this design is 1
  and it defines no features; the list exists so the first one needs no flag day.

Writers keep a registry in `<v2>/proto`: every key of every object, marked `informational` or with
the feature that introduced it. A corpus test fails when a key is added without a mark.

### 3.2 Raising the send level

The roster holds `"proto": {"send": <major>}`. A member sends only majors at or below it, and
accepts every major it knows. To introduce major 2:

1. Ship members that accept majors 1 and 2 and still send 1.
2. Each member reports the majors and features it accepts (section 6.6). C's raise operation reads
   them and refuses while any non-removed member does not accept 2, naming it.
3. The admin rewrites the roster with `"send": 2`. Members send 2 from their next verified roster.
4. Once no member sends 1, a later release may stop accepting it.

A new roster feature follows the same path: C adds a feature to the roster only when every
non-removed member reports it.

### 3.3 Why this design

Two others were weighed:

1. **v1's rule: exact key sets, any change is a new `v`, every member upgrades first.** It failed in
   practice: the team description had to move to `m.room.topic` only to avoid a new record key
   (R41), and Plan 00211 had to add a separate state event for its room index for the same reason.
2. **Capability negotiation per message** (each sender reads each addressee's accepted majors and
   picks one). Rejected: it puts a per-addressee lookup on every send, the answer can change between
   lookup and receipt, and a team of a handful of members gains nothing from per-pair majors.

The chosen design is the JOSE `crit` idea applied to Matrix content: ignore what you do not need to
understand, refuse what you must understand but do not.

---

## 4. Rooms

### 4.1 Layout

Every team has three rooms, all created by the admin when the team is created. C creates and
maintains them; this table is what C must produce.

| Purpose   | `m.room.name`        | Join rule                           | Members                               | Who posts                                   | Woken                                    |
| --------- | -------------------- | ----------------------------------- | ------------------------------------- | ------------------------------------------- | ---------------------------------------- |
| main      | `<team>`             | `invite`                            | humans, every non-removed member      | humans, active members                      | per addressing (section 6)               |
| reports   | `<team> · reports`   | `restricted`, allow members of main | humans (invited), every active member | active members                              | nobody                                   |
| questions | `<team> · questions` | `invite`                            | humans, the coordinator               | humans, the coordinator; nobody when closed | the coordinator, for every human message |

Common to all three: room version `12`; no `m.room.encryption`; `m.room.history_visibility`
`shared` (00211 D13: a new coordinator can read open questions); `m.room.guest_access` `forbidden`;
no aliases (00211 D11); `m.room.topic` per 4.4; the admin is the creator and has no entry in
`users` (room version 12 gives creators unlimited power). Main carries `<ns>.roster` (section 5);
each side room carries `<ns>.room` (5.3).

**Why questions exists from the start, closed.** Plan 00211 created it when a coordinator first
appeared (D4), so a member had to handle "index has no questions room" at run time, and `ask-human`
had a `no-room` refusal meaning "not deployed". Creating all three at once makes the layout fixed:
a missing or unverified room is a fault, reported as one, and the only questions-room state that
varies is open or closed, which is one power-level key.

**Why no further room.** Two candidates were weighed and not added:

1. **A traffic room** for agent-to-agent pings, so main holds only human conversation. It would make
   main quieter in Element but adds a room every member syncs and splits one conversation in two: a
   human reading main would no longer see the ping that a worker's answer replies to. Kept as owner
   decision OD-A3, default not added.
2. **Per-member direct rooms** for a human and one agent. Element's per-room notification settings
   already let a human silence main; private rooms per member would multiply rooms, trust checks and
   joins for no requirement anyone has stated.

### 4.2 Power levels

v1 left `events_default` at 0 and closed a few types by name, so every message type nobody listed
(polls, stickers sent through other types, calls, location shares, live beacons) was open to every
member, and Element offered them to humans. v2 inverts it: **everything is closed at
`events_default` 100, and each room opens `m.room.message` alone** (AR11). Redactions, reactions
and stickers are then closed without naming them.

Main room, and the questions room while open:

```json
{
  "users": {"@<human>:<server_name>": 50},
  "users_default": 0,
  "events_default": 100,
  "state_default": 100,
  "invite": 100,
  "kick": 100,
  "ban": 100,
  "redact": 100,
  "notifications": {"room": 50},
  "events": {"m.room.message": 0}
}
```

Reports room:

```json
{
  "users": {"@<human>:<server_name>": 0},
  "users_default": 10,
  "events_default": 100,
  "state_default": 100,
  "invite": 100,
  "kick": 100,
  "ban": 100,
  "redact": 100,
  "notifications": {"room": 100},
  "events": {"m.room.message": 10}
}
```

Questions room while closed (no coordinator in the roster): the main-room object with
`"events": {"m.room.message": 100}`.

What each level allows:

| Who              | Main, questions (open)                | Reports             | Questions (closed) |
| ---------------- | ------------------------------------- | ------------------- | ------------------ |
| admin (creator)  | everything                            | everything          | everything         |
| human (50, or 0) | post messages, `@room`, read receipts | read, read receipts | read               |
| agent (0, or 10) | post messages, read receipts          | post messages       | read               |

Nobody but the admin can set state, invite, kick, ban, redact (their own events included, so the
record an agent acted on cannot be withdrawn), react, or send any type other than
`m.room.message`. Read receipts, typing and presence are not power-gated in Matrix and stay open.

Humans are listed in reports at 0 rather than agents at 10, so adding an agent changes no power
level (00211 D2). A removed human is kicked from every room before the power levels are rewritten,
otherwise they would fall to `users_default` (00211 section 2); C owns that order.

**Reactions closed everywhere.** 00211 D16 closed them in questions so Element does not offer an
emoji answer nobody reads. The same holds in main: a human who reacts to an agent's question with a
thumbs-up would believe they answered. Closing `m.reaction` is a consequence of the allow-list, not
an extra rule. Owner decision OD-A5 lets the owner open reactions to humans for display only.

### 4.3 Membership rules

| Room      | How a human gets in                    | How an agent gets in                                                      | How either leaves                 |
| --------- | -------------------------------------- | ------------------------------------------------------------------------- | --------------------------------- |
| main      | admin invite, human accepts in Element | admin invite; the member accepts it only when the invite rule (6.3) holds | admin kick on removal             |
| reports   | admin invite                           | the member joins by the restricted rule after verifying main              | admin kick on removal             |
| questions | admin invite                           | admin invites the coordinator; the member accepts it per 6.3              | admin kick on removal or demotion |

A parked member stays joined (its token is revoked, so it can neither read nor post); a removed
member is kicked from every room and deactivated (C). A demoted coordinator is kicked from questions
before the new coordinator is invited (00211 section 3; R10 demote before promote).

### 4.4 Topics

The topic is display only and never affects trust (R41). C sets:

- main: the team's description (1-120 code points, no `Cc` or `Cf`, equal to its trimmed self, P16),
  else `Agent team: the team's agents and the humans who direct them. Mention an agent to reach it.`
- reports: 00211's sentence: `Reports from the team's agents, for you to read. To reach an agent, write in the main room; answer questions in the questions room.`
- questions open: `Questions the coordinator <handle> could not answer itself. Answer here; the coordinator reads every message in this room.`
- questions closed: `This team has no coordinator now, so this room is closed.`

A member reads the main topic as the team description only when the admin sent it and it passes
P16; otherwise it has no description, which is not a trust failure (v1 `syncer.py:350-359`).

---

## 5. The admin's records

### 5.1 The roster

One state event in the main room: type `<ns>.roster`, state key `""`, sent by the admin. It is the
only source of who is in the team, what each may do, and the team's policy. It replaces v1's
`agent_bus.team` record and `agent_bus.rooms` index, and the trust half of v1's bundle fields
(`human_text`, `limits`).

```json
{
  "v": 1,
  "team": "<team>",
  "rev": 42,
  "proto": {"send": 1},
  "humans": ["@alice:<server_name>"],
  "members": {
    "@myrepo.1+desk.podman:<server_name>": {"role": "coordinator"},
    "@myrepo.2+desk.podman:<server_name>": {"role": "worker"},
    "@myrepo.3+desk.lxc:<server_name>": {"role": "worker", "human_text": false},
    "@other.4+desk.podman:<server_name>": {"role": "worker", "parked": true}
  },
  "rooms": {"reports": "!<43 chars>", "questions": "!<43 chars>"},
  "forge": {
    "kind": "github",
    "api": "https://api.github.com",
    "org": "example-org",
    "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}, {"repo": "example-org/docs"}],
    "exclude": ["example-org/private-notes"],
    "path_prefixes": []
  },
  "limits": {"send_per_minute": 20, "send_burst": 10}
}
```

| Key                                              | Rule                                                                                                                                                | Source        |
| ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| `v`                                              | integer 1                                                                                                                                           | P43           |
| `features`                                       | optional, section 3                                                                                                                                 | new           |
| `team`                                           | team grammar, equal to the bundle's team                                                                                                            | P43           |
| `rev`                                            | integer at least 1, raised by one on every write; diagnostic only (6.5)                                                                             | new           |
| `proto.send`                                     | an accepted major (3.2)                                                                                                                             | new           |
| `humans`                                         | 1-16 distinct human user IDs                                                                                                                        | P43, P59      |
| `members`                                        | 0-64 agent user IDs; each value has `role` (`coordinator` or `worker`), optional `human_text` (default `true`), optional `parked` (default `false`) | P18, P43, R10 |
| at most one coordinator                          | a roster naming two non-parked coordinators is invalid                                                                                              | R10           |
| coordinator `human_text`                         | a coordinator with `human_text: false` is invalid (it would drop every questions-room message, 00211 lesson g)                                      | 00211         |
| `rooms`                                          | both `reports` and `questions`, distinct room IDs, neither the main room                                                                            | P47           |
| `forge.kind`                                     | `github` (the only forge v2 checks; a second forge is a new feature)                                                                                | R16           |
| `forge.api`                                      | `https://` origin with optional path, no query, no credentials                                                                                      | P43           |
| `forge.org`, `repos`, `exclude`, `path_prefixes` | v1's allow-list, P15 and P27 semantics, P59 counts; at least one of `org` or a repo; an excluded repo is not also listed                            | P15, P27, P59 |
| `limits`                                         | optional; keys and bounds of section 11; an absent key takes its default; a value outside its bounds makes the roster invalid (never clamped)       | P59           |

Unknown keys are ignored (section 3). `parked` makes a member's events drop `sender` and makes it
unaddressable; its account stays in the rooms. A removed member is absent from `members` (C kicks
and deactivates it). No `orchestrator` alias: v2 writes and reads `coordinator` only.

**Why `human_text` and `limits` move from the bundle to the roster.** In v1 each bundle carried
them, so two members of one team could apply different flood limits, and a sender could not know an
addressee would drop human text. In the roster they are one fact every member sees, and the admin
is the only writer.

**Why the room index joins the roster.** v1 kept a separate `agent_bus.rooms` event only because a
new record key would have made older members distrust the room. With section 3 that reason is gone,
and one event read verifies the whole team.

**Why the description stays out.** It is display text and Element already shows the room topic, so
it stays `m.room.topic` (R41) and can never affect trust.

### 5.2 Writing order

C owns the operations; A fixes the order the receiver rules depend on:

- Demote before promote: the roster never names two coordinators (R10).
- A removed member is dropped from `members` in the same write that the kicks follow; a removed
  human is kicked from every room before the roster and power levels are rewritten (00211).
- `rev` rises by exactly one per write.

### 5.3 The side-room link

Each side room carries `<ns>.room`, state key `""`, sent by the admin as initial state:

```json
{"v": 1, "team": "<team>", "purpose": "reports", "main": "!<main room ID>"}
```

A side room is believed only when the roster names it under a purpose and its link names the same
purpose, this team and this member's main room (P48). The link stops a room ID from the roster being
believed for the wrong purpose, which a swapped write could otherwise cause.

---

## 6. Trust

### 6.1 What it rests on

The homeserver authenticates every event's `sender`; federation is off, so there is one server and
no other origin. The admin account is the creator of every room and the only account with power to
change state. Its token is held by root on the homeserver host (C). So: **an event in a verified room
was sent by the account named in `sender`, and state in it was set by the admin.** Root on that host
and anyone holding a backup can act as the admin; that is the whole trusted set, and v2 adds no
member to it (AR16, R38).

### 6.2 Anchors

The bundle (C writes it, B reads it) pins: the homeserver URL, `server_name`, the admin user ID
(`@admin:<server_name>`), the main room ID, this member's user ID and team, and the token. Nothing
in a room can change an anchor. Anchors are re-pinned only by a new bundle.

### 6.3 Invites

A member accepts an invite only when every condition of its row holds; it rejects (`leave`) every
other invite and logs it by room ID only (v1 `syncer.py:177-220`, 00211 section 3):

| Room      | Conditions                                                                                                                                                 |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| main      | room ID equals the bundle's; inviter and the stripped `m.room.create` sender are the admin                                                                 |
| questions | main is verified; the roster names this room under `questions`; inviter and stripped create sender are the admin; the roster names this member coordinator |

An admin invite to a room the verified roster does not yet name is left pending, not rejected, so a
roster write racing an invite does not cost a re-invite (00211). Reports needs no invite: the member
joins it by the restricted rule once main is verified.

### 6.4 Verifying a room

A room is **verified** when every check of its column holds. The member reads the room's state with
one `GET /rooms/{room}/state` (the create event with its sender, since the state list carries
senders) and checks:

| Check        | Main                                                                                                       | Reports, questions                                                          |
| ------------ | ---------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| create       | sender admin; `room_version` `12`; no `additional_creators`; event ID is `$` + room ID without `!`         | same                                                                        |
| join rule    | `invite`                                                                                                   | reports: `restricted`, allow exactly main's membership; questions: `invite` |
| power levels | the safety invariants below                                                                                | the same invariants                                                         |
| roster       | present, sender admin, state key `""`, valid (5.1), every feature known, this member listed and not parked | (main's roster) names this room under its purpose                           |
| link         | -                                                                                                          | `<ns>.room` present, sender admin, matches (5.3)                            |

**The power-level invariants.** Let `top` be the highest level of any user other than the creator:
the largest of `users_default` and every value in `users`. A room passes when:

- I1. `state_default` > `top`;
- I2. `invite`, `kick`, `ban` and `redact` are each > `top`;
- I3. every key in `events` other than `m.room.message` has a value > `top`;
- I4. `events_default` > `top`.

Every sentence of 4.2's "nobody but the admin can..." follows from I1-I4, and they hold for every
form in 4.2. They are what trust needs: no one but the admin can change state, membership or
history, or send any type but `m.room.message`. They are not the layout: which humans sit at 50 and
whether questions is open are functional facts, which the server enforces and C's health check
compares exactly against 4.2. A member does not fail trust because the owner opened reactions (OD-A5)
or a later release changes a level within the invariants.

Two designs were weighed: (a) v1's exact match, simple to state but a protocol break for any change
and duplicated between member and host; (b) invariants at the member and exact match at the host
(chosen), where the member checks what its safety depends on and the host checks what the owner
designed.

### 6.5 When trust is checked again

A member holds one verified snapshot per room, keyed by the event IDs of the state it checked. The
snapshot is checked again before any further event of that room is judged when:

- the member's sync stream starts or restarts (first sync, after a homeserver outage, after the
  worker restarts);
- the member joins the room;
- a sync batch for the room is `limited` (a gap may hide a state change);
- a sync batch for the room holds, in `state` or `timeline`, any event of type `m.room.create`,
  `m.room.power_levels`, `m.room.join_rules`, `<ns>.roster` or `<ns>.room`, or this member's own
  `m.room.member` leaving;
- the server answers 403 to an action the snapshot says is allowed (then, if the room still
  verifies, the 403 is a fault reported as such, not retried).

A process without a live sync stream (a one-shot command) verifies before it sends. B decides which
process holds the snapshot and how a one-shot command gets one (v1 ran three GETs per send; v2
reaches the worker's).

Each event is judged against the roster **as it stands when the event is processed**, never as it
stood when the event was sent. A member removed or parked a moment ago is dropped at once, including
for events it sent before; that fails closed and costs at most a resend.

`rev` is shown in `status` and on the board beside the host's expected value, so a member that lags
a roster write is visible. It does not block anything: a restored backup lowers it legitimately.

**When a room fails.** Main failing makes the team **untrusted** for this member: nothing from it is
delivered, every send to it is refused, the session is told once (B's failure line), and the status
line shows it. A side room failing is untrusted alone: `report`, `ask-human` and `thread` refuse
naming the room, the coordinator stops reading questions, main stays trusted (00211). Transient
failures (no connection, 5xx, 429 past retries) are not trust failures: B retries them.

### 6.6 Presence and what a member accepts

A member's liveness and the majors and features it accepts are published with the Matrix presence
API, not as room state:

- While its seat is held, the member's keeper sets presence `online` with
  `status_msg` `<ns> wire=<majors> features=<names>` (majors comma-separated ascending; names
  comma-separated or `-`; the whole string printable ASCII, at most 200 bytes). On a clean exit it
  sets `offline`. The server's own timeout marks a dead member offline.
- Readers take a presence event only from its own user (`sender` equals the user), only for a roster
  member, and only when `status_msg` parses; anything else reads as unknown.
- `who` and the board show online or offline; C's raise operation (3.2) reads `wire` and `features`.

Element shows the online dot beside each agent, so a human sees who is live without asking.

The alternative is v1's: a `<ns>.member` state event under the member's own user ID, refreshed on a
timer. It needs `events["<ns>.member"]` open to agents (a second exception to I3), lets any member
write the type under a key that is not a user ID (v1 had to have the admin empty those), and adds a
state event to the room's history every refresh for every member for ever. Presence has none of
those costs, but whether Tuwunel serves presence with federation off, to sync filters naming the
team's rooms, is not yet recorded: host check HC-A1 decides before the design is frozen. If it
fails, the state-event form is specified with the invariant exception named and the sweep given to
C. This is owner decision OD-A1.

### 6.7 Sender classes

| Class         | Who                                                                           | May send                                                                     |
| ------------- | ----------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `admin`       | the bundle's admin user ID                                                    | state; `<ns>.notice` (section 7.4); nothing else is read                     |
| `human`       | a user ID in `humans`                                                         | human messages (7.5)                                                         |
| `coordinator` | a non-parked member with that role                                            | pings (by the verb table), texts, notices about itself, questions-room posts |
| `worker`      | a non-parked member with that role                                            | pings (by the verb table), texts, notices about itself                       |
| none          | anyone else: parked or removed members, unknown accounts, the service account | nothing; dropped `sender`                                                    |

### 6.8 Signing the roster

Option weighed and not taken by default (owner decision OD-A4): the admin signs the roster with an
Ed25519 key and every bundle pins the public key. It would defend against a homeserver that accepts
a state event from someone other than the admin (an authorisation bug), and would let a copy of the
roster be checked away from the server. It would not defend against the realistic attacker, root on
the host, who holds the signing key beside the admin token; it adds key custody, rotation and a
canonical-JSON signing scheme to C and B; and it does not cover power levels or messages, which
would still rest on the server's authorisation. The server's authorisation of state is the core of
Matrix and is exercised by the Tuwunel-tagged test (section 14), so it is the cheaper place to put
the test effort.

### 6.9 Threats

| Threat                                                                      | Defence                                                                                                                    |
| --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| An agent posts text shaped like a human request                             | kind from the roster class of `sender` (7.6); a human is never an agent's class                                            |
| An agent bypasses `<v2-bin>` and posts raw events (`curl` with its token)   | every rule is checked by every receiver; the server enforces only power levels (v1 known gap)                              |
| An agent sets a per-room display name equal to a human's                    | no rule reads names; envelope shows the localpart; Element shows the user ID on a name clash (12.3)                        |
| A container writes a forged inbox item or seat report                       | caches are never delivered; only a homeserver re-fetch is (AR15)                                                           |
| A member is removed while its messages are queued                           | events judged against the current roster (6.5)                                                                             |
| A room ID from a stale or swapped roster                                    | the side-room link (5.3)                                                                                                   |
| A member lags a roster change                                               | re-verify on any trust-type event; `rev` shown                                                                             |
| A homeserver restart or an outage                                           | re-verify on stream restart; transient failures retried, not trusted through                                               |
| Root on the homeserver host or a backup holder                              | not defended in-band; it is the root of trust (R38); per-member `human_text: false` limits what a human account can direct |
| A process holding the session's inbox socket token writes a forged envelope | not defended in-band (SPEC-inject T4); the history check in the frame text (13.6)                                          |
