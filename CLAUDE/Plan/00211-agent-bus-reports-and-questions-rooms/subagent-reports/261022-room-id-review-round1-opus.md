# Review round 1 (Opus): fix/agent-bus-room-id-shapes (f2c59399..5f86e2700)

## Verdict: MERGE

No defect blocks the host's run. The fix is correct for the path that hit rc 70. Finding 1
is a one-line documentation fix that can go in before or after the merge.

Tests run: `test_admin_side_rooms`, `test_protocol`, `test_identifier_grammar`, `test_fakes`:
195 tests, OK. I did not run `test_admin.py` and the rest of the suite, which the fake's new
admin-room join also reaches. qa-all covers those.

## What was checked and holds

- **`is_any_room_id` against the spec.** The localpart is `[^:\x00]+` and must not be empty.
  The server name follows the spec: IPv4, or an IPv6 literal of 2-45 characters in brackets,
  or a DNS name of 1-255 characters, with an optional port of 1-5 digits. `fullmatch` means a
  trailing newline does not match. The 255-byte limit is measured in UTF-8, and a lone
  surrogate is refused. The check is no narrower than the spec and does not accept garbage.

- **The other paths in the host's run.** `joined_rooms` is the only place where the bus's
  version-12 grammar is applied to a list the homeserver chose. The places that check one ID
  with `is_room_id` are all bus-owned, and accepting only version 12 there is correct:

  - the ID files, through `_room_id` and `_side_room_id`
  - the room IDs returned by `createRoom`
  - `config.py:520`, `inbox.py:288` and `matrix._room`
  - the rooms index (`protocol.py:1289`)
  - the syncer's invites, which are member-side. Members are not server admins, so they are
    never in the admin room.

  The bootstrap, add_member, set_role, remove_member, check_rooms and list paths, and adding
  an LXC member, all reach the homeserver's list only through `_find_side_room`, which is
  fixed.

- **Where the note goes.** It goes to stderr, not into `changes`, so it does not affect
  Ansible's `changed_when`.

- **The 403 handling.** On the ID-file path, a 403 still raises `AdminError` (the message
  says "not a … room admin created", which is acceptable). In `check_rooms` it is still
  reported as a problem. A joined room is always readable, so the discovery path cannot hide
  a real 403. Nothing is hidden.

- **The fake.** It models the admin room faithfully: room version 11, the `!hex:server` ID,
  created by `@conduit`, with the name and alias, and it is joined at registration as a
  server admin.

## Findings

1. **A stale version claim.** In `CLAUDE/Plan/meta-deploy.list` line 121, inside the
   play-claude-yolo.yml entry, the text still says "Container 2.78.14 (the guide in the image
   is corrected)". Line 91 of the same entry now says 2.78.15, so the entry contradicts
   itself. Change it to 2.78.15.
2. **A gap in the rule.** `homeserver-list-narrow-grammar` only treats `.call`, `.transport`
   and `._request` as homeserver calls. The syncer reaches the homeserver through
   `self.client.sync`, `.messages` and `.get_state`, so the rule never examines it. The Sweep
   test "finds the homeserver clients" asserts that `syncer.py` is scanned, which implies the
   rule covers it, but it does not. The rule also misses the equivalent
   `for r in rooms: if not is_room_id(r): raise` form. No instance exists today (I searched
   by hand), so this does not block the merge. Either widen the rule or narrow the claim in
   CLAUDE/QA.md.
3. **A repeated note (not a defect).** A team with no coordinator has no questions ID file,
   so every run calls `joined_rooms` twice for questions. The stderr note therefore repeats
   on every run, twice. That is acceptable, because it is on stderr and the changes list does
   not see it.
