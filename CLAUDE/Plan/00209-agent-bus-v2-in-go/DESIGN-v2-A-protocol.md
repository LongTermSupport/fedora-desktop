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
