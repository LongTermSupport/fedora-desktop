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

---

## 7. Message events and kinds

### 7.1 Rules common to every agent event

An agent event is an `m.room.message` sent by a member. Element shows `m.room.message` and hides
other types, so every event a human should see is one; the meaning is in exactly one typed key.

| Field           | Rule                                                                                                                                                                                        |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| typed key       | exactly one of `<ns>.ping`, `<ns>.text`, `<ns>.notice` (main room); `<ns>.report` (reports); `<ns>.question`, `<ns>.thread` (questions). None, two, or one of another room's: drop `schema` |
| `v`, `features` | checked first, before size or shape (P26's version-first rule), section 3                                                                                                                   |
| `msgtype`       | `m.text` when the event addresses a human, else `m.notice`; display policy, never read by a receiver                                                                                        |
| `body`          | a string; display text for Element (section 12), never read by a receiver                                                                                                                   |
| `m.mentions`    | `{"user_ids": <the addressees, sorted>}` so Element notifies the right humans; never read by a receiver (the typed `to` is the addressing)                                                  |
| `m.relates_to`  | absent, or exactly `{"m.in_reply_to": {"event_id": <event ID>}}`, or (questions room) the thread form of 10.3                                                                               |
| edits           | `m.new_content` anywhere, or `rel_type` `m.replace`: drop `edit`. Agents never edit                                                                                                         |
| size            | content at most 16384 bytes; each typed object has its own cap below                                                                                                                        |
| `format`        | absent (OD-A9 keeps `formatted_body` out for now)                                                                                                                                           |

**Why `body` is not checked.** v1 dropped an event whose `body` differed from a render of its typed
object, so the Element text could never change without a protocol version, and every field lived
twice. The check protected nothing a member could not do anyway: any member may post free text
saying whatever it likes. In v2 the sender's renderer is display, may improve in any release, and
receivers read the typed object only.

### 7.2 Ping

```json
{
  "msgtype": "m.notice",
  "body": "review pr:example-org/myrepo#12@0123456789abcdef0123456789abcdef01234567 -> myrepo.2+desk.podman",
  "m.mentions": {"user_ids": ["@myrepo.2+desk.podman:<server_name>"]},
  "<ns>.ping": {
    "v": 1,
    "verb": "review",
    "to": ["@myrepo.2+desk.podman:<server_name>"],
    "ref": "pr:example-org/myrepo#12@0123456789abcdef0123456789abcdef01234567"
  }
}
```

- `verb` from the closed table (8.1); `to` 1-32 distinct user IDs, sorted, each addressable (8.1);
  `ref` and `branch` as 8.1 and section 9; the typed object at most 4096 bytes.
- The answered ping, for verbs that take one, is the reply relation's `event_id`: `re` is no longer a
  field (AR22). Element then shows an answer under the ping it answers.

### 7.3 Agent text

```json
{
  "msgtype": "m.text",
  "body": "to alice, myrepo.1: Branch pushed; the failing test is fixed in 0123abc.",
  "m.mentions": {"user_ids": ["@alice:<server_name>", "@myrepo.1+desk.podman:<server_name>"]},
  "m.relates_to": {"m.in_reply_to": {"event_id": "$<43 chars>"}},
  "<ns>.text": {
    "v": 1,
    "to": ["@alice:<server_name>", "@myrepo.1+desk.podman:<server_name>"],
    "text": "Branch pushed; the failing test is fixed in 0123abc."
  }
}
```

- `to` 1-16 distinct, sorted: role-holding non-parked members and listed humans; `text` 1-4096
  bytes; never `@room`.
- A worker writing to a human adds every coordinator not already in `to` (P21); a receiving
  coordinator that was added sees the humans as `copy:` (13.3).
- The sender refuses text matching a secret shape (the twelve patterns of v1's protocol section 7,
  carried unchanged, with Go's `\b` and `\s` defined on Unicode scalar values over the pinned
  tables, P33) or holding its own token (R43). Receivers do not run the secret check.

### 7.4 Notice

A typed lifecycle fact. It replaces v1's fixed-text goodbye, farewell and disconnected messages,
which were PEER texts a receiver could only take as text.

```json
{
  "msgtype": "m.notice",
  "body": "myrepo.2+desk.podman left the team: session ended (exit)",
  "m.mentions": {"user_ids": ["@alice:<server_name>", "@myrepo.1+desk.podman:<server_name>"]},
  "<ns>.notice": {
    "v": 1,
    "event": "left",
    "subject": "@myrepo.2+desk.podman:<server_name>",
    "reason": "exit",
    "to": ["@alice:<server_name>", "@myrepo.1+desk.podman:<server_name>"]
  }
}
```

| `event`        | Sender             | Extra fields                        | Meaning                                               | Source           |
| -------------- | ------------------ | ----------------------------------- | ----------------------------------------------------- | ---------------- |
| `left`         | the subject itself | `reason`: `exit`, `logout`, `other` | the subject's session ended with a goodbye            | R28              |
| `disconnected` | admin              | -                                   | the subject's session ended without one (posted by C) | R29              |
| `added`        | admin              | `role`                              | the subject joined the team                           | R30              |
| `removed`      | admin              | -                                   | the owner removed the subject                         | R30, v1 farewell |
| `parked`       | admin              | -                                   | the owner parked the subject                          | R30              |
| `role`         | admin              | `role`                              | the subject's role is now `role`                      | R30              |

- `to` is the team's humans and coordinators, at most 17, never the subject (R28: "to humans and
  coordinators only"; each agent addressed costs a model turn).
- A notice whose `event` is unknown is dropped `schema`; a new event is a feature.
- `removed`, `parked`, `role` and `added` come from the admin because the admin made the change:
  the notice no longer depends on the subject's session being alive to post its own farewell.
- Exactly one of `left` and `disconnected` per claim, never twice: C's and B's records (R28, R29)
  keep that; A only fixes who may send which.

### 7.5 Human messages

A human writes in Element; nothing of `<ns>` is on the event. The member reads:

| Field     | Rule                                                                                                                                                           |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `msgtype` | `m.text`; anything else (`m.emote`, files, images): ignored `type` and counted as unheard                                                                      |
| `body`    | a string of at most 16384 bytes, else drop `size`; `format`/`formatted_body` ignored                                                                           |
| reply     | `m.relates_to.m.in_reply_to.event_id`; leading `>` quote lines and one blank line after them are removed when present (P37); empty after that: ignored `empty` |
| thread    | `rel_type` `m.thread` in main is read as a reply to the thread root                                                                                            |
| edit      | `rel_type` `m.replace` with `m.new_content` (`msgtype` `m.text`, `body` string): delivered as a revision (below)                                               |

**Addressing.** A human message in main is addressed to this member when any holds:

1. `m.mentions.user_ids` contains this member;
2. `m.mentions.room` is `true` (humans hold the `@room` power);
3. it replies to an event this member sent (not a thread fallback reply);
4. it is in a thread whose root this member sent.

Rules 3 and 4 are new. Element adds the replied-to sender to `m.mentions` for a plain reply, but a
"Reply in thread" is not a mention, so in v1 a human answering an agent inside a thread was unheard.
Checking the replied-to event's sender takes one event fetch, which B caches. A human message
addressed to no agent is counted as unheard, as v1 (P41).

**Edits.** A human's edit is delivered as a new HUMAN entry with `revises: <original event ID>`, in
main and in questions alike (00211 D6 had questions only; OD-A8). It is accepted only when the
original exists in the same room and has the same sender (the server does not enforce that); it is
addressed to whoever the original was addressed to, plus any member named in
`m.new_content.m.mentions`. Its age is the edit's own `origin_server_ts`. Why: an owner correcting
an instruction expects the correction to arrive; v1 dropped it silently in main.

A human message gets no `<ns>` key and is never read for anything but the above.

### 7.6 How the kind is derived

The receiver judges each event once, in this order; the first rule that decides wins. The outcome is
**deliver** with a kind, **ignore** (silent, not logged except where noted), or **drop** (logged with
a reason from the closed set below).

01. `type` is not `m.room.message`: ignore `type` (state is the trust path's).
02. `event_id` fails its grammar, `state_key` present, `unsigned.redacted_because` present, `content`
    not an object, or `origin_server_ts` not a non-negative integer: drop `schema`.
03. Already judged: ignore `seen`. Sent by this member: ignore `self` (B records its own ping's
    `origin_server_ts`, 8.2).
04. Sender class (6.7) none: drop `sender`.
05. Room:
    - reports: no member reads it (00211); a member that does is a fault.
    - questions: this member not the coordinator: ignore `not-coordinator`. A human: the human path,
      every message addressed; kind HUMAN with the room lines (13.3). The coordinator's own posts
      feed the question index (10.4). Any other agent: drop `sender`.
    - main: continue.
06. Admin: `<ns>.notice` other than `left`: the notice path, kind NOTICE. Anything else: ignore
    `admin` (the admin's display posts, if any, are for humans).
07. Human: the human path (7.5), kind HUMAN. A member whose roster entry says `human_text: false`
    drops it `sender` (R21 per-member mitigation, v1 `protocol.py:823`).
08. Agent: the typed-key rule (7.1), then by key: `<ns>.ping` the ping path (8.1), kind PING;
    `<ns>.text` kind PEER (the same `human_text` rule applies, as v1); `<ns>.notice` only `left` with
    `subject` equal to `sender`, kind NOTICE.
09. Addressing: a ping, text or notice whose `to` does not hold this member: ignore `not-addressed`.
10. Local checks, in order: age (`stale`), receive flood (`rate`), forge check for a ping with a
    reference (section 9). Then deliver.

TIMEOUT and MEMBERSHIP never come from the room: TIMEOUT from the member's own ping records (8.3)
and question reminders (10.5); MEMBERSHIP from the host's desired-membership file (B, 00203 H12).

**Closed sets.** Drop reasons: `version schema size edit sender role target verb ref allowlist excluded stale rate forge-rate unresolved provenance`. Ignore reasons: `type seen self admin not-addressed empty not-coordinator`. Send refusals add `secret`, `body` (a report
or question form error, 10.2) and `duplicate`. v1's `re` drop becomes `schema` (the relation is
shape) and `agent-text` becomes `not-addressed`. A new reason is a feature.

---

## 8. Pings, answers and timeouts

### 8.1 The verb table

Kept from v1 (P19, `protocol.py:252-262`), with one change: an answer's `re` is the reply relation.

| Verb      | `ref`     | Forms            | Reply relation | Answer expected | Senders             | External ref |
| --------- | --------- | ---------------- | -------------- | --------------- | ------------------- | ------------ |
| `fetch`   | required  | path, commit     | forbidden      | yes             | coordinator         | yes          |
| `sync`    | required  | path, commit     | forbidden      | yes             | coordinator         | no           |
| `review`  | required  | path, commit, pr | forbidden      | yes             | coordinator, worker | yes          |
| `run-qa`  | required  | commit, pr       | forbidden      | yes             | coordinator, worker | no           |
| `halt`    | forbidden | -                | forbidden      | yes             | coordinator         | no           |
| `ack`     | forbidden | -                | required       | no              | any role            | no           |
| `nack`    | optional  | any              | required       | no              | any role            | yes          |
| `done`    | required  | any              | optional       | no              | coordinator, worker | yes          |
| `blocked` | required  | any              | optional       | no              | coordinator, worker | yes          |

- Addressable: a non-parked role holder; for `ack`, `nack`, `done` and `blocked` also a listed human
  (P20, P22). A worker's ping that names a human copies the coordinators (P21).
- `branch` only beside a `path` or `commit` reference (P28).
- A sender outside the verb's senders: drop `role` (P23). An unknown verb: drop `verb`.
- The reply relation where forbidden, or missing where required: drop `schema`.

Why a closed verb set stays: a ping's authority is "the verb only" (13.4). Free text from an agent is
PEER and has no authority; the verbs are the only way one agent asks another to act, and each is
something a receiving agent can do safely on a document it reads.

### 8.2 Answers

An **answer** is an `ack`, `nack`, `done` or `blocked` whose reply relation names a ping this
member sent, from a sender that ping addressed. v1 counted only `ack` and `nack`; `done` and
`blocked` with a reply relation now count too, because an agent that finishes at once should not
have to send an `ack` first only to stop a TIMEOUT.

The **deadline** of a ping is its own `origin_server_ts` plus its timeout: `halt_ack_timeout_s` for
`halt`, `ack_timeout_s` for the other verbs that expect an answer. An answer counts when its
`origin_server_ts` is at or before the deadline, however late the member reads it (R42). Both times
are the server's: v1 took the deadline from the sender's local clock and compared it with the
answer's server time, so a skewed member clock moved deadlines. The sender learns its ping's
`origin_server_ts` when its own event comes back in sync (rule 3 of 7.6); until then B uses its
local send time.

An answer from someone the ping did not address counts for nothing; it is still delivered when it
addresses this member.

### 8.3 Timeouts

When the member's clock passes a ping's deadline, each addressee that has not answered gets exactly
one TIMEOUT, keyed `<ping event ID>~<addressee localpart>` and marked reported once delivered (R42;
v1 `inbox.py:840-860`). A TIMEOUT is never retried and spends no flood budget. Its authority is "a
fact about your own ping" (13.4).

### 8.4 What is delivered

| Arrives                       | Delivered as                                                                                                                                              |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| a ping that expects an answer | a PING entry                                                                                                                                              |
| `nack`, `done`, `blocked`     | a PING entry                                                                                                                                              |
| `ack`                         | no entry of its own: recorded, and the next delivery for another reason carries an `answered:` line for it (13.2); `history` and `status` show it (OD-A2) |
| a late answer                 | as above, marked late in `history`                                                                                                                        |

Why `ack` is not its own delivery: each entry costs the receiving session a model turn, and an
`ack` changes nothing the session must do: it waits for `done` or `blocked`, and a missing answer
already produces a TIMEOUT. Plan 00211 added the reports room partly to cut the same kind of
noise.

### 8.5 Staleness

A ping older than its verb's timeout when first judged is dropped `stale` (`ack_timeout_s` for a
verb that expects no answer); an answer that is stale still counts for 8.2 and is not delivered. A
human message or agent text older than `human_max_age_s` is dropped `stale` (P60).

---

## 9. References and the forge check

### 9.1 Grammar

Kept from v1 (P13-P15, `protocol.py:279-284,553-594`):

| Form     | Shape                      |
| -------- | -------------------------- |
| `path`   | `path:OWNER/REPO@SHA:PATH` |
| `commit` | `commit:OWNER/REPO@SHA`    |
| `pr`     | `pr:OWNER/REPO#NUM@SHA`    |
| `issue`  | `issue:OWNER/REPO#NUM`     |

`OWNER` `[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?`; `REPO` `[a-z0-9._-]{1,100}`, not `.`, `..` or ending
`.git`; `SHA` 40 lowercase hex; `NUM` `[1-9][0-9]{0,9}`; `PATH` 1-8 segments of
`[A-Za-z0-9._-]{1,64}`, none `.` or `..`; the whole at most 700 characters. A sender lowercases
`OWNER/REPO` before sending (ASCII lowercase: the grammar is ASCII, so v1's Python full-lowercase
case D6 cannot arise). A `path` reference, when the roster sets `path_prefixes`, must match one: an
entry ending `/` is a prefix, any other is exact (P15).

### 9.2 Trust class of a reference

From the roster's `forge` section (P27, `protocol.py:528-544,692-722`):

1. The repository is in `exclude`: drop `excluded`.
2. It is in `repos`: **listed**. Its `branches`, when given, are the only branches that prove it.
3. Its owner is `org`: **org**.
4. Otherwise: **external**. A verb that takes no external reference: drop `allowlist`. Otherwise the
   ping is accepted with trust `external`.

Listed wins over org.

### 9.3 The check (R16)

Who checks: the sender before it sends (a failure refuses the send), and every receiver before it
delivers (a failure drops the ping). Each uses its own forge login (`gh` for github.com), so a
member never trusts another member's word for a forge fact. Never in a hook (v1 `forge.py:4-5`).
The credential is re-read when a check needs it and the held copy is older than B's bound, and at
once after a 401; it is sent only as an `Authorization` header to the roster's `forge.api` origin
and never written or logged (R44).

What proves a reference, by class (v1 `forge.py:323-405`):

| Class                    | Repository facts                            | Branch                                                                                                          |
| ------------------------ | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| listed with branches     | owner and name as listed; `fork` is `false` | the ping's `branch` must be listed; the commit is reached from it, or from any listed branch when none is given |
| listed, no branches; org | owner login equals `org`; `fork` is `false` | the ping's `branch` (which must exist) or the default branch                                                    |
| external                 | `private` is `false`; `fork` is `false`     | when given, it exists and reaches the commit                                                                    |

| Form     | Proof                                                                                                                                                                          |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `commit` | `compare/BASE...SHA` is `identical` or `behind` for a proving branch                                                                                                           |
| `path`   | the commit proof, and `contents/PATH?ref=SHA` is a file                                                                                                                        |
| `pr`     | the pull request's head repository is the reference's; `author_association` is `OWNER`, `MEMBER` or `COLLABORATOR` (not judged for external); the head is at or ahead of `SHA` |
| `issue`  | the issue exists and is not a pull request                                                                                                                                     |

The rule under all of it, from v1's own comment: **a commit the API returns proves nothing**, because
a fork's commits are served through its parent's API; only reachability from a branch of a trusted
repository proves provenance (`forge.py:343-345`).

Results: proven (deliver, with the class as `trust`); `provenance` (found but not proven); `unresolved`
(not found, wrong kind, forge unreachable, forge refused the credential); `forge-rate` (the forge
rate-limited the check, which says nothing about the reference, so it is not the bus's `rate`).

Caching (B stores it): positive results only; repository metadata, reachability, pull requests and
branches for 3600 s, so a repository that leaves the organisation or becomes a fork stops being
trusted within that time however long the worker runs (R44); file and commit content at a SHA for
30 days, since content at a SHA cannot change. HTTP: 15 s timeout, 8 MiB response cap, redirects
only to the same origin, 429 handled as 11.3.

The receiver never fetches the referenced artefact for the session; the envelope's authority line
says it is a document to read, never a command (13.4).

---

## 10. Reports, questions and threads

Plan 00211's behaviour, on v2's schema. Its decisions D2-D16 hold unless a line below says why not.

### 10.1 Reports

```json
{
  "msgtype": "m.notice",
  "body": "Merged the parser fix\nPR 12 is in main; the nightly QA passed.",
  "m.mentions": {},
  "<ns>.report": {"v": 1}
}
```

- Any active member; humans cannot post (power 0). The text is the `body`: the first line is the
  headline (1-120 code points after trimming), the whole at most 1500 bytes; over the cap it is
  refused `body`, never cut. Secret shapes and the member's own token are refused (P51).
- No mentions and `m.notice`, so Element never notifies (00211 D14); no agent reads the room, so
  nobody is woken.
- Rate: the send bucket and duplicate window, and `report_per_hour` (11.1).
- Content guidance (milestones, merges, deploys queued, decisions for the owner, risks, blocked
  items; never a question or narrative) belongs to the skill, which B ships.

Here the `body` is the content, unlike other agent events: reports are written for humans and no
member reads them, so there is nothing to duplicate into a typed object.

### 10.2 Questions

The coordinator asks; workers never post in questions (they are not members) and ask the
coordinator in main (00211 D7).

```json
{
  "msgtype": "m.text",
  "body": "Q3 Which licence for the new docs site?\nContext: ...\nQuestion: ...\nOptions:\n1. MIT\n2. CC-BY-4.0",
  "m.mentions": {"user_ids": ["@alice:<server_name>"]},
  "<ns>.question": {
    "v": 1,
    "n": 3,
    "asked_by": ["@myrepo.2+desk.podman:<server_name>"],
    "re": ["$<43 chars>"],
    "remind_after_s": 14400
  }
}
```

- Form (P54): headline 1-120 code points, then `Context:` and `Question:` sections (required) and
  `Options:` (optional, lines `1. ` to at most `9. ` in order); the whole at most 8000 bytes; `body`
  starts `Q<n> `. A form error is refused `body`, naming the section.
- `n` is 1 plus the highest `n` among the coordinators' questions in the room's history, so a
  replacement coordinator numbers the same way with no session state (00211).
- `asked_by` 0-16 user IDs of members; `re` 0-16 main-room event IDs the question came from.
- `remind_after_s` optional, 600-604800 (10.5).
- An open question whose `Question:` section equals the new one (whitespace collapsed) refuses the
  new one `duplicate`, naming the open `Q<n>`.
- `m.text` mentioning every human, so Element notifies (00211 D14). Each question is a thread root,
  so the room's timeline shows one line per question (00211 D5).

### 10.3 Thread posts

The coordinator's follow-ups inside a question's thread:

```json
{
  "msgtype": "m.notice",
  "body": "Thanks: relayed to the worker. Closing.",
  "m.mentions": {},
  "m.relates_to": {
    "rel_type": "m.thread",
    "event_id": "$<question>",
    "is_falling_back": true,
    "m.in_reply_to": {"event_id": "$<question>"}
  },
  "<ns>.thread": {"v": 1, "n": 3, "close": true}
}
```

- With an answer to a specific human post, `m.in_reply_to` names that post and `is_falling_back` is
  `false` (the Matrix thread form).
- `close: true` closes the question; text may then be empty and the body is `Q<n> closed.`.
- A post that asks the humans something again is `m.text` and mentions them; otherwise `m.notice`
  and no mentions (00211 `--ask`).
- At most 4000 bytes. `<ns>.thread` replaces v1's `agent_bus.question_state`, so every coordinator
  post in questions is typed.

### 10.4 Matching a human's answer to a question

Every human message in questions reaches the coordinator (7.6 rule 5). It is attached to a question
by the first rule that holds (00211 section 8.3):

1. it is in the thread of a question (`rel_type` `m.thread`, root a question);
2. it replies to a question or to a post in a question's thread;
3. its body starts with `Q<n>` (any case) then a space, `:` or `.`;
4. it is an edit: the original's attachment, or its new body's `Q<n>`;
5. otherwise unattached: delivered with `question: none`, and the coordinator decides.

A question's state is `closed` once a valid closing post exists, else `answered` once any human
post is attached, else `open`; its unanswered count is the human posts attached after the
coordinator's last post in its thread (P58).

### 10.5 Reminders

A question with `remind_after_s` and no attached human post by its `origin_server_ts` plus that
bound produces one TIMEOUT for the coordinator, keyed `<question event ID>~humans`: `verb: ask-human`, `question: Q<n>`. It is computed from the room's history (like the numbering), so a
replacement coordinator gets the same reminder once. Why: in 00211 an unanswered question had no
expiry or reminder, so it could wait unseen while the asking worker was blocked (00211 open
question).

### 10.6 The whole flow

1. A worker needs a human decision: `say --to-coordinator` in main.
2. The coordinator answers itself if it can; otherwise it runs `ask-human` (10.2) with
   `--asked-by` and `--re` naming the worker and the worker's message.
3. Humans are notified; a human answers in the thread in Element (or replies, or writes `Q3: ...`).
4. The coordinator receives a HUMAN entry with `room: questions` and `question: Q3 "<headline>"`;
   the reply line is the `thread` command.
5. The coordinator passes the answer to each asker in main (`say --to <asker> --re <their event>`)
   and closes the question (`thread Q3 --close`). A later edit arrives with `revises:`; the
   coordinator corrects what it passed on.

The asker gets the answer from the coordinator, not from the room, on purpose: the coordinator owns
the work's direction and may combine answers, and workers stay out of the humans' notification
stream (00211 D7).

---

## 11. Limits and rates

### 11.1 Team limits

Set in the roster's `limits`, the same for every member (AR25). Values from v1 (P59,
`limits.py:26-47`); a value outside its bounds makes the roster invalid.

| Key                      | Default | Bounds      | Enforced by             | What                                                                                                                                       |
| ------------------------ | ------- | ----------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `send_per_minute`        | 20      | 1-60        | sender                  | token bucket refill per minute (P61)                                                                                                       |
| `send_burst`             | 10      | 1-30        | sender                  | bucket capacity                                                                                                                            |
| `recv_per_sender_minute` | 60      | 10-600      | receiver                | events admitted per sender per sliding minute; refusals not counted (P65)                                                                  |
| `ack_timeout_s`          | 900     | 60-86400    | sender and receiver     | answer deadline; ping staleness                                                                                                            |
| `halt_ack_timeout_s`     | 300     | 30-3600     | sender and receiver     | the same for `halt`                                                                                                                        |
| `human_max_age_s`        | 86400   | 3600-604800 | receiver                | human and agent text staleness                                                                                                             |
| `report_per_hour`        | 6       | 1-60        | sender                  | reports in any 3600 s                                                                                                                      |
| `deliver_per_sender`     | 10      | 1-60        | receiver (B's delivery) | PEER, PING and member-sent NOTICE entries per (team, sender) per rolling 600 s (E19); HUMAN, TIMEOUT and admin notices are never held back |

`wait_timeout_s` is a property of B's CLI, not of the team, and leaves this table.

### 11.2 Fixed limits

| What                            | Limit                                                                                                             | Source  |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------- |
| any agent event content         | 16384 bytes                                                                                                       | new     |
| ping object                     | 4096 bytes; `to` 1-32                                                                                             | P26     |
| agent text                      | 1-4096 bytes; `to` 1-16                                                                                           | P32     |
| notice `to`                     | 1-17                                                                                                              | R28     |
| human body                      | 16384 bytes                                                                                                       | P36     |
| report / question / thread post | 1500 / 8000 / 4000 bytes; headline 1-120 code points                                                              | P51-P54 |
| duplicate window                | 60 s on `[verb or kind, ref, re, sorted to]`; notices exempt (v1 lesson: it can only lose a second, true goodbye) | P62     |
| reference                       | 700 characters                                                                                                    | P13     |
| roster lists                    | humans 1-16; members 0-64; repos 0-32; branches 1-8 per repo; exclude 1-32; path prefixes 1-32                    | P59     |
| presence `status_msg`           | 200 bytes printable ASCII                                                                                         | new     |

### 11.3 Server waits

Tuwunel has no per-user send rate limit (only login tables), so all flood control is the members'
(Matrix digest section 8). A 429 is honoured as v1 (P66): `Retry-After` in seconds, else a reset
header, else `retry_after_ms`, else 5 s; three tries; a wait over 60 s is not slept but reported. A
retried send reuses its transaction ID (R19).

### 11.4 The worst case

A compromised member holding its token can post at whatever rate the server allows. What reaches
each session is bounded by the receive limit (60 per minute admitted), then the delivery budget (10
entries per 600 s, the rest counted as held back), then the envelope caps (13.2): about 20 KiB of
envelopes and a few held-back lines per 10 minutes per compromised sender, as v1 (SPEC-inject T6).
