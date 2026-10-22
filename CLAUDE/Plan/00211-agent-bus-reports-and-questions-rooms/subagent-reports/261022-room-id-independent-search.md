# Independent search: homeserver answers checked with a project-narrow identifier grammar

The dispatch named the narrow grammar (is_room_id etc.), so I searched from the class description plus those names. I did not open .semgrep/ or test_identifier_grammar.py. I did not run any detector.

## Instances (file:line, source, scope, can a legitimate homeserver be refused)

1. helpers/agent_bus/admin.py:417 joined_rooms(): `all(protocol.is_room_id(r) for r in rooms)`.
   Source: homeserver GET /joined_rooms. WHOLE-COLLECTION refusal (AdminError).
   A legitimate server is refused: Tuwunel's v11 admin room is in the list. CONFIDENT, the originating defect, still present in this worktree.
   Found by: text search, then reading. Reading showed the consumer at admin.py:758-763 (_adopt side room): the refusal blocks adoption of any side room whenever the admin is in a non-v12 room.
2. helpers/pingbus/syncer.py:659 _take_invites: `for room_id in rooms.invite: if not is_room_id: ignore`.
   Source: homeserver sync `rooms.invite` dict keys. SKIPPED per item (logged, continue). A legit v1-11 invite is silently ignored. This is not wholesale, but a v11 room invite is dropped instead of rejected/left. UNSURE: probably acceptable, since bus invites are v12.
3. helpers/pingbus/matrix.py:118 _room() (and :124 _event()), used on request paths, e.g. messages() at :321.
   Source: the caller's room ID. In the syncer it is self.member.room, a bus-written config value. Not a homeserver list. Raises ValueError for a non-v12 room, but the only callers use bus rooms. Low risk, UNSURE. Check the other callers of _room: client.join(room_id) and leave(room_id) in _take_invites are already filtered by is_room_id at syncer:659, so any v11 invite never reaches them.
4. helpers/pingbus/matrix.py:326 _event_id(answer): the event_id of a homeserver send/put answer must be `$`+43 chars. Source: homeserver answer. Raises Unreachable. Event IDs of room v4+ are always 43 chars, but v1-v3 rooms use `$opaque:server`. Bus rooms are v12, so it is safe only by the room-version invariant. Single value, whole operation fails. UNSURE (latent).
5. helpers/pingbus/syncer.py:149 _envelope_ok and :761 `if is_event_id(event_id): seen.add`; protocol.py:935 and :1433 validate_event. Source: homeserver sync/messages events. Per event: an event with a non-v4+ ID is dropped or ignored, never the whole batch. A legit server cannot produce one in a v12 room. Low risk.
6. helpers/pingbus/syncer.py:733 _read_membership: `inbox.is_user_id_text(state_key)`; inbox.py:299 and :119 _LOG_USER_RE `@[\x21-\x7e]{1,254}` plus a ":" check.
   Source: homeserver sync state events. Per event: skipped with a log line. Looser than the protocol (printable ASCII only). A legitimate non-ASCII or historical user ID is skipped, not fatal. UNSURE, low.
7. helpers/pingbus/inbox.py:747 and :750 _check_room_view: `all(is_user_id_text(uid) ...)` over room.json members/statuses keys.
   Source: a bus-written file, populated from homeserver membership state only after item 6 filtered it. Whole-file StateError on one bad key, but the filter at 6 makes it consistent. Low.
8. helpers/pingbus/protocol.py:1265 parse_rooms_index: all rooms in the `agent_bus.rooms` state event must be is_room_id, else Untrusted for the whole index. Source: a state event the bus admin wrote (bus content, v12 rooms). Whole-collection by design. Low.
9. helpers/agent_bus/admin.py:604 and :807: the createRoom answer's room_id must be is_room_id, else AdminError. Source: homeserver answer. The room is created with room_version=ROOM_VERSION (12), so it is correct unless the server ignores the version. Fine, low.
10. helpers/agent_bus/admin.py:244 and :708: the room ID read from a bus-written private file. Not homeserver data. Not a hazard.
11. helpers/pingbus/config.py:520 (a configured room), envelope.py:216/240/263, sentlog.py:66/82, inbox.py:193/288/652/869, protocol.py:740/899/1162/1181/1452, relay.py:91, cli.py:177/1090/1189/1254: bus-written protocol or file content, or CLI arguments. Event IDs `re` and `revises` that cross-reference the team room (v12). Not homeserver lists. cli.py:1189 checks user-supplied --re values before the homeserver get_event.
12. Go src-go/internal/bus/seat/member.go:61 (used at :194) and src-go/internal/pingbus/statusline/member.go:43 (used at :92): `^![A-Za-z0-9_-]{43}$` on `room` of the bus-written member.json. Not homeserver data. Not a hazard.
13. CLAUDE/Plan/00161-agent-team-bus-matrix/acceptance_check.py:160/166 (checks event/room IDs from homeserver answers on the bus rooms, per value), test_triage_probe.py:39-40/242-243 (test asserts that created room/event IDs have the v12 shape), triage_probe.py:77 (_ID_RE scans text for IDs). A scan or assertion over created bus rooms only. Low.

No hand-written patterns for room aliases or server names were applied to homeserver data. teamfile.check_server_name applies to the teamfile only. members()/room_state() in admin.py:421-425 and :350-357 do no per-ID validation (state_key only needs to be a str), so they are not affected.

## Attribution
- Text search found everything above containing the named functions or the `{43}` literal (sites 1-13).
- Reading found: that site 1 is wholesale where site 2 is per item; the downstream effect of site 1 at admin.py:758; that site 4 depends on the room-version invariant (a text hit alone does not show a homeserver answer is involved); that room_state()/members() have no such check; that sites 7, 10, 11 and 12 are bus-written, not homeserver data, which the pattern alone cannot show.
- Text search could not check whether the data reaches the check from a homeserver or from a file; that took reading.

## Searches run
grep -E "is_room_id|ROOM_ID_PATTERN|is_event_id|EVENT_ID_PATTERN|is_user_id_text|USER_ID_PATTERN|\{43\}|\^!|\^@" over helpers/ and CLAUDE/Plan; the Go regexp.MustCompile family under src-go (excluding vendor and tests) filtered for room/event/user/43; grep for roomIDRE/roomIDPattern; grep for joined_rooms; grep for user-id, alias and server-name regexes under helpers/.
Forms looked for: `![A-Za-z0-9_-]{43}`, `\$...{43}`, @user:server patterns, alias `#`, server_name checks, dict-key checks in `all(...)`, and sync keys.
