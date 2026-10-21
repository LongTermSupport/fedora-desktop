# SPEC: `bus/proto`, the wire protocol v1 in Go (Plan 00209 milestone M1)

The one home of every protocol-1 grammar both sides of the bus use (DESIGN 3.2, R15, R21, R41).
Authority: [docs/agent-bus-protocol.md](../../../docs/agent-bus-protocol.md) (cited `§n`). The
Python is read for requirements only (`protocol.py`, `envelope.py`, `limits.py`, `cli.py` lines,
`agent_bus/registry.py` handle building), cited `file:line`; where it is wrong the departure is
numbered (D1...) and pinned in the corpus. Plan 00211's SPEC (side rooms, reports, questions) and
Plan 00194's SPEC-inject (E1-E26, the envelope) are carried.

Package: `src-go/internal/bus/proto`. Corpus: `tests/fixtures/agent-bus-protocol/` (DESIGN 5.4),
walked by `tests/helpers/pingbus/test_protocol_corpus.py` against the Python and by
`src-go/internal/bus/proto/corpus_test.go` against the Go. Neither is the other's oracle: every
case is written from this spec or a found bug.

## 0. Scope

In: §3 identifiers and handle building; §4 ping; §5 verbs; §6 reference grammar (offline part);
§7 human and agent text, secret shapes; §8 team record, status, power levels, §8.1 side-room
state; §9 offline receive steps and their verdicts; §10 limits as pure functions over a given
`now`; §11 allowlist trust (offline); Plan 00211 §7-§8 report, question and thread content and
the question index; §15 item lines (`PING`, `HUMAN`, `PEER`, `REPLY`, `TIMEOUT`, `SENT`,
`DROPPED`, `HISTORY`, `QUESTION`, `REPLY_TO`); the delivery envelope (SPEC-inject §2 E1-E9,
E19-E21, §3) and the membership notice (Plan 00203 H12).

Out (other packages, not created here): the forge check and its cache (`bus/forge`); member.json,
the inbox, outbox and every file (`bus/state`); Matrix calls (`bus/matrix`); the socket, the
transcript check and the inject marks (`bus/member`, Python `inject.py`); the report lines of
`status` (`TEAM`, `MEMBER`, `ROOM`, `SEAT`, `ABOUT`, `UNTRUSTED`, `UNEXPECTED`) and `PENDING`,
which are CLI report formats (`bus/member`); relay spool lines (D4 of DESIGN: relay not built).

## 1. Values (P1-P3)

- **P1. Pure.** No I/O, no clock read, no package state written after init. Every function that
  needs a time takes it (`now_ms`, or a monotonic `time.Duration` for the sender budget).
- **P2. The value model is Python's `json.loads`.** Inputs are values decoded by
  `internal/pingbus/pyjson` (reused, not copied): `pyjson.Number` keeps int and float apart, a
  bool is never an int (`type(v) is int`), a string may hold a lone surrogate (WTF-8, as Python
  keeps it). `isinstance(v, dict)` is `pyjson.Object`, a list is `[]any`. Python's `v == 1` (where
  the Python uses `==`, not `type(v) is int`) is `pyjson.EqualsInt`: `true` and `1.0` equal `1`
  (`protocol.py:1448` `parse_question`, `:1484` `_closes`). JSON equality by
  `json.dumps(sort_keys=True)` is `pyjson.Canonical` (`protocol.py:1118`).
- **P3. Sizes and lengths.** A length in characters counts code points, a lone surrogate as one
  (`len(str)`). A size in UTF-8 bytes is `len(s.encode("utf-8"))`, which Python raises on for a
  lone surrogate (D1). `content_size` (`protocol.py:636`) is the byte length of
  `json.dumps(content, ensure_ascii=False, separators=(",", ":"))` in UTF-8: strings escape only
  `"`, `\`, `\b \f \n \r \t` and other C0 as `\u00XX`; an int is its Python decimal (`-0` is `0`);
  a float is Python's `repr` (`1.0`, `1e+16`, `1.5e-05`, `NaN`, `Infinity`, `-Infinity`); key
  order does not change the size. A lone surrogate anywhere makes it unencodable: an error, which
  the ping path reads as `schema` (`protocol.py:753-756`).

## 2. Identifiers (§3, §6, §11; P4-P17)

Python `re.fullmatch` over the whole value; every class below is ASCII unless said. A non-string
never matches.

- **P4. Team name** `[a-z][a-z0-9-]{0,23}` (`protocol.py:83`).
- **P5. Agent handle** `<repo>.<seat>+<host>.<type>`: repo `[a-z0-9][a-z0-9_-]{0,47}`, seat
  `[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}`, host `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`, type one of
  `podman lxc docker vm host` (`protocol.py:63-82`). Parsed into its four parts; a seat of digits
  is numbered (its value), else named.
- **P6. Seat** alone: the seat pattern of P5.
- **P7. Human localpart** `[a-z][a-z0-9_-]{0,31}` and not `admin` or `conduit` (P8).
- **P8. Reserved localparts**: `admin`, `conduit`.
- **P9. User ID**: `@<localpart>:<server_name>` with the server exactly the given one, split at
  the first `:`; the localpart is a handle, a reserved localpart or a human localpart, else no
  user ID (`protocol.py:487-497`). An agent user ID's localpart is a handle; a human user ID's is
  a human localpart.
- **P10. Room ID** `![A-Za-z0-9_-]{43}`; **event ID** `\$[A-Za-z0-9_-]{43}`.
- **P11. Branch** `[A-Za-z0-9._/-]{1,100}`.
- **P12. Owner** `[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?`; **repository name**
  `[a-z0-9._-]{1,100}`, not `.`, `..` or ending `.git`; **OWNER/REPO** both.
- **P13. Reference** (§6): `path:OWNER/REPO@SHA:PATH`, `commit:OWNER/REPO@SHA`,
  `pr:OWNER/REPO#NUM@SHA`, `issue:OWNER/REPO#NUM`; SHA `[0-9a-f]{40}`; NUM `[1-9][0-9]{0,9}`;
  PATH 1 to 8 segments `[A-Za-z0-9._-]{1,64}`, none `.` or `..`; the whole at most 700
  characters. Parsed into text, form, owner, repo, sha, num, path (`protocol.py:553-571`).
- **P14. Lowercasing for send**: only the `OWNER/REPO` part of anything shaped
  `(path|commit|pr|issue):<no @ or #>*<rest>` is lowercased; anything else is unchanged
  (`protocol.py:574-580`). Python `str.lower()` is the full lowercase (D6).
- **P15. Path prefix** entry: a PATH ending in `/`, or an exact PATH; `path_allowed` is a plain
  prefix test for a `/` entry, equality otherwise (`protocol.py:583-594`).
- **P16. Description** (§8): a string of 1 to 120 code points, equal to its Python `strip()`
  (Unicode whitespace: `str.isspace`), with no character of category `Cc` or `Cf`
  (`protocol.py:464-468`). Categories are each implementation's own Unicode tables (D4).
- **P17. Building a handle** (§3, `agent_bus/registry.py:70-122`): `normalise_repo` lowercases,
  turns every code point outside `[a-z0-9_-]` into `-`, strips leading `-` and `_`, keeps the
  first 48, refuses empty; `repo_from_remote(remote, dir)` takes the last component split on `/`
  and `:` of the remote (`rstrip("/")` first), removes `.git` when it came from a remote, else
  uses the directory; `resolve_host(explicit, role)` takes the first non-empty, lowercases,
  turns outside `[a-z0-9-]` into `-` and must then match the host pattern, refusing with
  neither; `build_handle(repo, seat, host, type)` takes a number 1 to 999999 or a seat name and
  must parse as a handle; `prefix_of(handle)` is `<repo>+<host>.<type>`. A refusal is an error
  (the Python's message text is not part of the contract).

## 3. Roles, verbs, addressing (§5, §7, §8; P18-P23)

- **P18. Roles** `coordinator`, `worker`; a record's `orchestrator` reads as `coordinator`
  (`protocol.py:48-53,597-600`).
- **P19. Verbs**, in spec order, each with its `ref` (required/optional/forbidden), allowed ref
  forms, `re`, ack expected, senders, external allowed (`protocol.py:252-262`, §5's table):
  `fetch` (req path commit; re forbidden; ack; coordinator; external), `sync` (req path commit;
  forbidden; ack; coordinator; no), `review` (req path commit pr; forbidden; ack; coordinator
  worker; external), `run-qa` (req commit pr; forbidden; ack; coordinator worker; no), `halt`
  (forbidden; forbidden; ack; coordinator; no), `ack` (forbidden; re required; no ack; anyone
  addressed; no), `nack` (optional, any form; required; no; addressed; external), `done`
  (required, any form; optional; no; coordinator worker; external), `blocked` (as `done`).
- **P20.** Only `ack`, `nack`, `done`, `blocked` may name a listed human in `to`.
- **P21. Coordinators** are the user IDs whose role is `coordinator`, sorted. **The
  coordinator's copy**: when the sender's role is `worker` and `to` names a listed human, the
  coordinators not already in `to` are appended in that order; otherwise `to` is unchanged
  (`protocol.py:603-616`). **On copy**: a receiver whose role is `coordinator` gets the listed
  humans of `to`, sorted; anyone else gets none (`:619-623`).
- **P22. Sender class**: a listed human is `human`; an agent holding a role is that role; any
  other value (non-string included) is none (`protocol.py:626-633`). **Addressable**: an agent
  user ID holding a role; or, for agent text or a P20 verb, a listed human user ID (`:683-689`).
- **P23. Role check** (`protocol.py:783-792`): an unknown verb is `verb`; a sender that is not a
  role holder is `role`; a role not among the verb's senders (unless the verb is sent by "anyone
  addressed") is `role`. Agent text needs only a role holder (`:1238-1241`).

## 4. Ping (§4, §6, §11; P24-P29)

- **P24. Build**: `{"msgtype": "m.notice", "body": render, "m.mentions": {"user_ids": to sorted}, "agent_bus.ping": {"v": 1, "verb", "to" sorted, then "ref", "re", "branch" when given}}` (`protocol.py:1129-1142`).
- **P25. Render**: `[agent-bus] <verb> <ref or ->[ on <branch>] -> <to sorted, space-joined>[ re <re>]` (`protocol.py:641-651`).
- **P26. Validate content**, first failure wins (`protocol.py:745-780`): not an object: `schema`;
  `agent_bus.ping` an object whose `v` is an int other than 1: `version`; `m.relates_to` or
  `m.new_content` present: `edit`; content size (P3) unencodable: `schema`, over 4096: `size`;
  keys not exactly `msgtype body m.mentions agent_bus.ping` or `msgtype` not `"m.notice"`:
  `schema`; the ping not an object, or a key outside `v verb to ref re branch`, or a required key
  (`v verb to`) missing: `schema`; `v` not an int, `to` not a list of strings, any other key not a
  string: `schema`; unknown verb: `verb`; `to` empty, over 32, duplicated, or naming anyone not
  addressable for the verb (P22): `target`; the reference (P27); the branch (P28); `re` (P29);
  `m.mentions` not exactly `{"user_ids": <to sorted>}`: `schema`; `body` not a string: `schema`;
  `body` not the render: `body`. The parsed ping carries verb, `to` sorted, ref, re, trust,
  branch.
- **P27. Reference and trust** (`protocol.py:692-722`): absent where required, present where
  forbidden, failing P13, or of a form the verb does not allow: `ref`; a repository of the org
  named in `exclude`: `excluded`; a repository neither listed nor the org's is external: `allowlist`
  for a verb that takes no external ref, else accepted with trust `external`; otherwise trust
  `trusted`, and a `path` reference must pass `path_prefixes` when the team sets them
  (`allowlist`). Listed wins over org; `trusts_any_branch` is an unlisted org repository or one
  listed with no branches (`:528-544`).
- **P28. Branch**: absent, or a P11 branch beside a `path` or `commit` reference; else `ref`.
- **P29. `re`**: absent unless required (`re`); present where forbidden or not an event ID: `re`.

## 5. Agent text and secret shapes (§7; P30-P34)

- **P30. Build** (`protocol.py:1150-1165`): `{"msgtype": "m.notice", "body": render_text, "m.mentions": {"user_ids": to sorted}, "agent_bus.text": {"v": 1, "to" sorted, "text"}}`, and
  with `re` (an event ID, else an error) `"m.relates_to": {"m.in_reply_to": {"event_id": re}}`.
- **P31. Render**: `[agent-bus] text -> <to sorted>\n<text>`.
- **P32. Validate** (`protocol.py:1202-1235`), first failure wins: not an object: `schema`; text
  object with an int `v` other than 1: `version`; `m.new_content` present: `edit`; an
  `m.relates_to` with `rel_type` `m.replace`: `edit`; any other `m.relates_to` than exactly
  `{"m.in_reply_to": {"event_id": <event ID>}}`: `schema`; keys other than the four (plus that
  one): `schema`; `msgtype` not `m.notice`: `schema`; text object keys not exactly `v to text`,
  `v` not an int, `to` not a list of strings, `text` not a non-empty string: `schema`; `text`
  over 4096 UTF-8 bytes: `size`; `to` empty, over 16, duplicated, or not addressable: `target`;
  on send only, a secret shape: `secret`; mentions not `{"user_ids": <to sorted>}`: `schema`;
  body not a string: `schema`; body not the render: `body`.
- **P33. Secret shapes** (§7 list, `protocol.py:127-142,1185-1190`): the first of the twelve, in
  order, found anywhere with Python `re.search` semantics. Python's `\b` is a Unicode word
  boundary (a word character is a letter, a number or `_`), its `\s` is `str.isspace`, and
  `(?i)` folds `ı` and `İ` onto `i`, `ſ` onto `s` and `K` (U+212A) onto `k`. Go's RE2 has none of
  these, so the Go spells them out (`\s` as the explicit class, `\b` checked by hand against the
  code point before and after the match, the case-folded text normalised for `ı`/`İ` first).
- **P34.** `report`, `ask-human` and `thread` refuse secret shapes too (P51-P54).

## 6. Receive (§9, Plan 00211 SPEC §6; P35-P42)

- **P35. `validate_event(event, ctx, self, seen, human_text, room)`** (`protocol.py:914-970`): a
  room other than `main` or `questions` is a caller error. Then, first match wins: not an
  object: drop `schema`; `type` not `m.room.message`: ignore `type`; event ID failing P10: drop
  `schema`; in `seen`: ignore `seen`; sender equal to `self`: ignore `self`; `state_key` present,
  `unsigned.redacted_because` present, `content` not an object, or `origin_server_ts` not a
  non-negative int: drop `schema`. In the questions room, P40. In the main room: sender class
  none: drop `sender`; human: P36; content holding `agent_bus.text`: with `agent_bus.ping` too,
  drop `schema`, else P38; else P39.
- **P36. Human path** 04h-09h (`protocol.py:818-843`): `human_text` false: drop `sender`;
  `msgtype` not `m.text`: drop `schema`; `m.relates_to` present and not an object: drop
  `schema`; `m.new_content` present or `rel_type` `m.replace`: drop `edit`; `body` not a string:
  drop `schema`; body over 16384 UTF-8 bytes: drop `size`; not addressed (mentions `room` exactly
  `true`, or `user_ids` a list containing self): ignore `not-addressed`; with `m.in_reply_to` in
  `m.relates_to`, the reply fallback removed (P37), empty: ignore `empty`. Accepted with event
  ID, sender, ts, text.
- **P37. Reply fallback** (`protocol.py:795-805`): split on `\n`; the leading lines starting with
  `>` and then one empty line are removed; with no leading `>` line the text is unchanged.
- **P38. Agent text received** (04p, `protocol.py:846-872`): neither the text object's `to` nor
  `m.mentions.user_ids` (each a list) contains self: ignore `agent-text`; `human_text` false:
  drop `sender`; P32 without the secret check: drop with its reason. Accepted as a peer text with
  its copy (P21).
- **P39. Ping received** (05p-07p): P26 then P23: drop with the reason; self not in `to`: ignore
  `not-addressed`. Accepted with sender, event ID, ts and copy.
- **P40. Questions room** (`protocol.py:875-911`): self not the record's coordinator: ignore
  `not-coordinator`; sender a coordinator: ignore `coordinator-post`; sender not a listed human
  or `human_text` false: drop `sender`; `msgtype` not `m.text`: drop `schema`; `m.relates_to`
  present and not an object: drop `schema`; an edit (`m.new_content` present or `rel_type`
  `m.replace`) must be exactly `rel_type` `m.replace`, an event-ID `event_id`, and an
  `m.new_content` object with `msgtype` `m.text`, else drop `schema`, and its body is the new
  content's; body not a string: drop `schema`; over 16384 bytes: drop `size`; not an edit and a
  reply: fallback removed; empty: ignore `empty`. Accepted as a human message with `revises`
  (the edited event) for an edit.
- **P41. Unheard** (`protocol.py:973-987`): only an outcome ignored `not-addressed` of a listed
  human's event whose content is an object; no `user_ids` list: unheard; else unheard when no
  string entry is a role-holding agent user ID.
- **P42. Closed sets** (`protocol.py:192-214`): drop reasons `version schema size edit sender role target verb ref allowlist excluded re body stale rate forge-rate unresolved provenance`;
  send refusals `secret no-room`; ignore reasons `type seen self not-addressed empty agent-text not-coordinator coordinator-post`; unheard `not-addressed`. `stale`, `rate`, `forge-rate`,
  `unresolved` and `provenance` are never produced here.

## 7. Room state (§8, §8.1; P43-P50)

- **P43. Team record** (`protocol.py:1035-1069`), the first failure raising "untrusted" with
  exactly this reason text: keys missing a required one or holding an unknown one: `team record keys`; `v` not the int 1: `team record version`; team not a team name or not this team: `team name`; humans not a list of 1-16 distinct values (distinct by P2's canonical form; `list size or duplicates`) or not all human user IDs: `humans`; roles not an object of at most 64, or any
  role (after P18) not a role or key not an agent user ID: `roles`; path_prefixes present and not
  1-32 distinct P15 entries: the list rule's `list size or duplicates`, else `path_prefixes`;
  forge_api not `https://[^\s/]+(/\S*)?` (Python `\s`): `forge_api`; org present and not an
  owner: `org`; repos (0-32 distinct; `list size or duplicates`) entries: not an object of
  exactly `repo` or `repo branches`, repo not OWNER/REPO, or a repeat: `repos entry`; branches
  not 1-8 distinct (`list size or duplicates`) or not branch names: `branch name`; no org and no
  repos: `neither an org nor a repository is trusted`; exclude present: not 1-32 distinct (`list size or duplicates`), no org, or not repository names: `exclude`; one listed in repos as
  `org/name`: `exclude repository is also listed`. Parsed: team, humans, roles, repos with their
  branches, path_prefixes, forge_api, org, exclude.
- **P44. Team event**: an object, `type` `agent_bus.team`, `state_key` `""`, sender the admin,
  else `team record event`; then P43 on its content.
- **P45. Status** (`protocol.py:1084-1102`): read only when the type is `agent_bus.status`, the
  state key equals the sender, the sender holds a role, the content's keys are exactly `v state until`, its size (P3) at most 256, `v` the int 1, `state` `listening`, `until` a non-negative
  int; the value is `until`. Anything else is ignored.
- **P46. Power levels** exactly (§8, `protocol.py:1105-1126`), types included (canonical, P2).
- **P47. Rooms index** (§8.1, `protocol.py:1247-1268`): an admin state event of type
  `agent_bus.rooms`, state key `""`, content an object with `v` the int 1 (`the agent_bus.rooms event` / `the agent_bus.rooms content`); other keys a subset of `reports questions` (`the rooms index keys`), each a room ID other than the main room, no two equal (`the rooms index room IDs`).
- **P48. Back-link**: `agent_bus.room` admin state event as P47; content exactly
  `{"v": 1, "team", "purpose", "main"}` with the expected values (`the room's back-link`).
- **P49. Side power levels** (`protocol.py:1279-1306`): reports and questions (open, closed) as
  §8.1; any other purpose is a caller error.
- **P50. Join rules**: reports restricted to the main room's members, questions invite
  (`join rules`).

## 8. Reports, questions, threads (Plan 00211 SPEC §7-§8; P51-P58)

- **P51. Report text** (`protocol.py:1321-1337`): over 1500 UTF-8 bytes: `body`; first line
  (before the first `\n`) blank after `strip()` or over 120 code points: `body`; secret shape:
  `secret`. Returns the headline.
- **P52. Thread text**: over 4000 bytes: `body`; empty unless closing: `body`; secret: `secret`.
- **P53. Report content**: `{"msgtype": "m.notice", "body", "m.mentions": {}, "agent_bus.report": {"v": 1}}` built; checked exactly (`schema`).
- **P54. Question text** (`protocol.py:1369-1409`): over 8000 bytes: `body`; headline as P51;
  a headline starting with a section name: `body`; sections `Context:`, `Question:`,
  `Options:` read from line 2 on by line prefix, a repeat: `body`; `Context:` or `Question:`
  missing or blank once stripped: `body`; `Options:` lines (non-blank) must start `1. `, `2. `...
  in order, at most 9: `body`; secret: `secret`. Returns headline and the stripped `Question:`
  section.
- **P55. Question content**: `m.text`, body `Q<n> <text>`, mentions every human sorted,
  `agent_bus.question` `{"v": 1, "n", "asked_by", "re"}`.
- **P56. Parse question** (`protocol.py:1441-1459`): an `m.room.message` with an event ID, an
  object content and a string sender that is an agent user ID; `agent_bus.question` an object of
  exactly `v n asked_by re`, `v == 1` by P2's Python equality, `n` an int at least 1, body a
  string starting `Q<n> `, `msgtype` `m.text`, `asked_by` a list of strings, `re` a list of event
  IDs, and the text after the prefix passing P54. Else no question.
- **P57. Thread post** (`protocol.py:1462-1479`): body the text, or `Q<n> closed.` for an empty
  closing post; `m.text` and mentions of the humans with `ask`, else `m.notice` and `{}`; with no
  root a reply (`m.in_reply_to` to `re`), else a thread relation (`rel_type` `m.thread`,
  `event_id` root, `is_falling_back` true without `re`, `m.in_reply_to` `re` or the root); with
  `close`, `agent_bus.question_state` `{"v": 1, "n", "state": "closed"}`. Neither root nor `re`
  is a caller error.
- **P58. The question index** (`protocol.py:1501-1611`): messages oldest first; the first
  question of a number counts; agent posts in a question's thread move its "last agent post"
  and close it when they carry a valid closing state for its number (`v == 1` by P2, `n` an int,
  `state` `closed`); a human message (sender in humans) is attached by the first rule: an edit
  (`rel_type` `m.replace`) by its new body's `Q<n>` prefix, else its original's question; a
  thread whose root is a question; a reply to a question or to a post in a question's thread;
  the body's (reply-stripped) prefix `[Qq]<1-6 digits>` then space, `:` or `.`. Views per
  question in number order: `closed` when closed, else `answered` with any reply, else `open`;
  `unanswered` are the replies after the last agent post. `next_number` is one more than the
  highest; `open_duplicate` compares `Question:` sections with whitespace collapsed
  (`str.split()`), skipping closed ones.

## 9. Limits (§10; P59-P66)

- **P59. Defaults and bounds** (`limits.py:26-47`): `send_per_minute` 20 (1-60), `send_burst` 10
  (1-30), `recv_per_sender_minute` 60 (10-600), `ack_timeout_s` 900 (60-86400),
  `halt_ack_timeout_s` 300 (30-3600), `human_max_age_s` 86400 (3600-604800), `wait_timeout_s`
  1500 (1-1790), `report_per_hour` 6 (1-60). `parse_limits`: absent is the defaults; not an
  object, an unknown key, or a value that is not an int (bool excluded) in bounds: a config
  error, never clamped. `check_wait_timeout` holds `wait --timeout` to its bounds.
- **P60. Ack and age** (`limits.py:143-177`): an ack-expected verb's timeout is
  `halt_ack_timeout_s` for `halt`, else `ack_timeout_s`; none for the others; deadline `sent + timeout*1000`; overdue when `now > deadline`. A ping is stale when `now - origin >` its
  timeout (`ack_timeout_s` for a verb that expects none); a human message (and an agent text)
  when `now - origin > human_max_age_s*1000`. An unknown verb is a caller error.
- **P61. Token bucket** in integer credit (`limits.py:180-217`): capacity `burst*60000`; refill
  `elapsed_ms * send_per_minute`, elapsed never negative and the clock never moved back; a take
  needs 60000. State `{credit, at_ms}` exactly, ints, credit non-negative, capped at capacity on
  load.
- **P62. Duplicate window**: key is compact JSON of `[verb, ref, re, sorted(set(to))]`
  (`limits.py:220-222`, Python's `json.dumps`, so non-ASCII escaped as `\uXXXX`); seen within 60 s
  (`now - at < 60000`); recording drops expired keys. State: an object of string to int.
- **P63. Send gate**: duplicate first (`duplicate`), then the bucket (`rate`), recording nothing
  when refused; `deduplicate` false skips the window both ways. State `{bucket, recent}`.
- **P64. Report window**: sends in the last 3600 s (`now - at < 3600000`); at `per_hour` or more:
  `rate`; state a list of ints.
- **P65. Receive flood**: per sender at most `recv_per_sender_minute` admitted in any sliding
  minute (`now - t >= 60000` expires); refusals not counted.
- **P66. Server waits** (`limits.py:120-134`): `Retry-After` of digits (after `strip()`), then a
  named reset header of digits as Unix seconds minus `now` (never negative), then the body's
  `retry_after_ms` as a non-negative int in seconds, then 5. Fixed: 60 s duplicate window, 30 s
  long poll, 3 tries, a wait over 60 s not slept.

## 10. Item lines (§15; P67-P72)

- **P67. A line** is `KIND`, `1`, then the kind's fields, tab-joined; an absent field is `-`; a
  field must be a non-empty string with no C0 or DEL (`cli.py:149-161`). Field counts: PING 9,
  HUMAN 6, PEER 6, REPLY 3, ABOUT 2, TIMEOUT 5, SENT 2, DROPPED 2, HISTORY 4, QUESTION 6,
  REPLY_TO 3 (`cli.py:104-116`).
- **P68.** `PING`: team, event ID, sender localpart, verb, ref, re, trust, branch, copy (`copy:`
  and the humans' localparts joined by `,`, or `-`); trust present exactly when ref is. `HUMAN`:
  team, event ID, sender (a human), ts, text as `json.dumps(text, ensure_ascii=True)`, room
  field (`-`, `questions:Q<n>`, `questions:-`). `PEER`: as HUMAN with an agent sender and copy.
  `TIMEOUT`: team, event ID, silent target, verb, ref. `SENT`: team, event ID (`cli.py:224-323`).
- **P69. The reply command** (`cli.py:287-310`): none for a ping; in the questions room `pingbus --team <team> thread Q<n>` or `thread --re '<id>'`; else `pingbus --team <team> say --to <name> --re '<id>'`, the name a human's localpart, or for a peer its short name (handle before
  `+`) when exactly one agent of the record has it, else its handle.
- **P70. `DROPPED`**: total, then `reason=count` in P42 order for counts above 0; any key not a
  drop reason, a count not a non-negative int, or a total of 0 is a caller error.
- **P71. `HISTORY`**: `HISTORY`, `1`, direction `in`/`out`, ts, then the inner line's kind and
  fields after its version.
- **P72. `QUESTION`**: team, `Q<n>`, state, event ID, the askers' localparts joined by `,` (those
  that parse) or `-`, the unanswered count. `REPLY_TO`: team, `Q<n>`, event ID.

## 11. The delivery envelope (§15, SPEC-inject §2-§3; P73-P84)

- **P73. Entry from a line** (`envelope.py:228-272`): a `HUMAN`, `PEER`, `PING` or `TIMEOUT` line
  of version 1 with its exact field count (8, 8, 11, 7 including kind and version); team and event
  ID by grammar; this session's handle a handle; sender a handle or human localpart; HUMAN only
  from a human localpart and PEER only from a handle, each with its reply command; ts
  `[0-9]{1,32}`; the text a JSON string (`json.loads`); copy `-` or `copy:` and human localparts;
  room field `-|questions:(Q[1-9][0-9]{0,8}|-)` with the attached question's headline (1-120
  code points) exactly for `Q<n>`, `revises` only in the questions room and an event ID; PING and
  TIMEOUT: a known verb, ref absent or P13; PING: re absent or an event ID, trust absent,
  `trusted` or `external`, branch absent or P11, and its ts from the caller. Anything else is an
  error, never rendered.
- **P74. The body render** (`envelope.py:279-346`, E2): one JSON string; printable ASCII but `"`
  and `\` as itself; a non-ASCII code point as itself only when: U+FE0E/U+FE0F after a shown
  `So`/`Sk`; a mark (`Mn Mc Me`) after a shown ASCII alphanumeric or shown non-ASCII, at most 4
  in a row; a letter or number (`L* N*`); punctuation or a symbol inside the allow-listed blocks;
  never one of the escaped set; never one whose NFKC form holds `"`, `\`, `<`, `>` or a
  non-printable (for Unicode 14 that is exactly U+FF02, U+FF1C, U+FF1E, U+FF3C, pinned in the
  corpus); a ZWJ only between a shown pictograph run (pictograph, then its U+FE0F or skin tones)
  and a pictograph; U+FE0F on a keycap base only before U+20E3. Everything else is the escape
  `json.dumps(ch, ensure_ascii=True)` writes (`\n \r \t \b \f \" \\`, else `\uxxxx` lowercase, an
  astral code point as its surrogate pair, a lone surrogate as itself escaped).
- **P75. Body cap** (E8, `envelope.py:355-372`): the rendered string, quotes included, at most
  2048 UTF-8 bytes (WTF-8 for a lone surrogate); otherwise the longest code-point prefix whose
  render fits, with shown and total UTF-8 byte counts.
- **P76. Wire bytes** (E9): the length of `json.dumps(text, ensure_ascii=True)`.
- **P77. Entry lines** per kind (`envelope.py:397-430`, SPEC-inject §3): the kind line, `from:`
  (not for TIMEOUT), `team:`, the questions-room lines (`room:`, `question: Q<n> <headline rendered>` or `question: none`, `revises:`), `to: this session, as <handle>`, `sent:` (not for
  TIMEOUT; E21: `-` outside 0 to 253402300799999, else UTC `YYYY-MM-DDTHH:MM:SSZ` of the whole
  second), `id:`; TIMEOUT: `silent:`, `verb:`, `ref:`, its authority; PING: `verb ref trust branch re`, the copy line, its authority (external or not), `reply: pingbus --team <team> send <VERB> --to <sender> --re '<id>'`; HUMAN/PEER: copy line, authority, `reply:`, `body:`, and
  `truncated: <shown> of <total> bytes shown; the whole text is in pingbus --team <team> history (id <id>)` when cut. Every header value is `[\x21-\x7e]{1,1200}` (or `-` when absent); a reply
  command `[\x20-\x7e]{1,400}`; anything else is an error.
- **P78. First line** (E7): `agent-bus delivery <nonce>: <N> message(s) (<H> from humans, <A> from agents, <P> pings, <T> timeouts): <who>`, `who` the first three distinct `<sender> (<KIND>, <team>)` labels, then ` and <k> more`; `flood limit notes only` with no entries.
- **P79. Held back** (E19): one line per (team, sender) with a positive count, in the caller's
  order, at most 8, then `held back: <n> more from other senders` for the rest.
- **P80. A delivery**: the nonce `[0-9a-f]{8}`; at least one entry or held-back line; `more` a
  non-negative int; lines joined by `\n`, each entry between `<<agent-bus N i/n>>` and
  `<<end agent-bus N i/n>>`, then held-back lines, then `<k> more pending: they follow in the next delivery`.
- **P81. Membership notice** (Plan 00203 H12, `envelope.py:481-496`): its own first line, one
  entry `kind: MEMBERSHIP ...`, `from: pingbus`, `to: this session`, the caller's lines, the
  authority; a line that is empty, over 600 characters, holds a C0, DEL, U+2028 or U+2029, or a
  `<<` or `>>` is refused.
- **P82. Fit** (E9): how many entries, oldest first, the first delivery takes: at most 8, and at
  most 12 KiB in wire bytes with the rest counted as more pending; none fitting is an error.
- **P83. Sender budget** (E19): an agent kind (`PEER`, `PING`) at most 10 entries per (team,
  sender) in a rolling 600 s, with `chosen` counted; other kinds always allowed and spend
  nothing.
- **P84. Unicode tables** (DESIGN 8.3): Go's `unicode` is 15.0, the container's Python 3.11 is
  14.0 and CI's Python 3.12 is 15.0. The Go escapes every code point Unicode 15.0 assigned that
  14.0 left unassigned (49 ranges, pinned in the corpus; a test holds the Go table equal to them
  and the Python test holds them unassigned under Unicode 14), treats them as no word character
  for P33's `\b`, and otherwise uses its own tables, which agree with 14.0 on every code point
  14.0 assigns (checked when the table was built). A corpus case that depends on the version
  carries the Python's expectation for Unicode 14 and for 15 or later; the Go's is one.

## 12. Departures and findings

- **D1. Lone surrogate in a body or text.** `_human_outcome` and `validate_text_content`
  (`protocol.py:835,1224`) call `.encode("utf-8")` on a string that can hold a lone surrogate
  (an inbox file, which is untrusted, or a homeserver that passes one), and raise
  `UnicodeEncodeError` out of `validate_event`. A string that is not encodable is not "a string
  of at most N UTF-8 bytes": the Go drops it `schema`. The Python is pinned as raising; reported,
  not fixed in this branch.
- **D2. A non-string sender in the questions room.** `ctx.roles.get(sender)`
  (`protocol.py:885`) raises `TypeError` for an unhashable sender (a list or an object). The Go
  drops `sender`, as the main room does for any non-member. Pinned, reported.
- **D3. `origin_server_ts` past int64.** Python accepts any non-negative int; the Go accepts up to
  2^63-1 and drops a larger one `schema` (Matrix's canonical JSON allows at most 2^53-1). Pinned.
- **D4. Unicode version.** P16 and P74 classify by each side's tables, with P84's table on the Go
  side. Pinned per version.
- **D5. Secret `\b` beside a Unicode 15 letter.** Python 3.11 sees a boundary, the Go too (P84),
  Python 3.12 none. Pinned per version.
- **D6. `İ` (U+0130) in a repository or host name.** Python's `str.lower()` gives `i` and a
  combining dot, so `-` follows; Go's `strings.ToLower` gives `i` alone. The Go uses Python's
  full lowercase for that code point, so both give `i-`. No departure in behaviour; pinned.
