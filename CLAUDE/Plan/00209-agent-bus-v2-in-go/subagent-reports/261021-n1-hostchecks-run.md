# N1 Task 1.4 (hostchecks): the container run of hca_probe.py

The committed evidence for Task 1.4's server checks. `hca_probe.py` was run in the CCY container
against the pinned Tuwunel release asset, unpacked by hand:

- asset `v1.9.4-release-all-x86_64-v1-linux-gnu-tuwunel.zst`, sha256
  `78628e6d13deda9e4832aeb93f2d10332e3c4e70a8815b45e6ef773404edd5a3`, the
  `tuwunel_sha256_x86_64` of `files/usr/local/share/agent-bus/tuwunel.pin` (v1.9.4);
- the binary unpacked from it, sha256 `8e9a5873d8a2cea62c808106426fbeae16bf49333089f1eb2adec9fd3d6e0b2b`,
  `--version` `tuwunel 1.9.4`;
- the command, from the worktree root (`<scratch>` is an untracked scratch directory):
  `python3 CLAUDE/Plan/00209-agent-bus-v2-in-go/hca_probe.py . <scratch>/work <scratch>/report.md --binary <scratch>/tuwunel-1.9.4 --extra db_pool_workers=4`;
- exit 1 (two checks fail on a fact; every server check judged).

The report follows as the probe wrote it, with the binary's absolute path and the scratch path
replaced by placeholders. Room and event IDs are the throwaway server's own.

## Plan 00209 host checks (Task 1.4)

- binary: <scratch>/tuwunel-1.9.4, `--version` tuwunel 1.9.4, given with --binary, so its asset record is not checked against files/usr/local/share/agent-bus/tuwunel.pin
- config: a team's key set (C 3.2.4), server name hca.invalid, on a loopback port
- config keys added for this run away from the host: {"db_pool_workers": 4}
- the server says {"name": "Tuwunel", "version": "1.9.4"}
- the server's `show-config` answer: <scratch>/show-config.txt (not committed)
- not in this run: HC-A2 (Element's display), HC-A3 and HC-A5, the owner's Element checks after gate A

| Check | Verdict | Question |
| --- | --- | --- |
| HC-A7 | PASS | GET /rooms/{room}/state returns the create event with its sender, so one read verifies a room |
| HC-A4 | PASS | Tuwunel accepts 4.2's power levels exactly as written and refuses polls, reactions, stickers, redactions, state and invites to non-admins |
| H-C12 | PASS | a restricted join to reports, a power-0 post refused, pending invites left pending |
| HC-A2 (server) | PASS | the human's push rules notify an agent's m.text that mentions them and not an m.notice |
| HC-A6 | PASS | Tuwunel's own service account localpart, to reserve |
| HC-A1 | FAIL | presence with federation off: online by the link's sync, set_presence=offline, PUT offline, the timeout |
| H-C13 | FAIL | DC16 (c'): one device per seat, logout, what the token alone can do, the admin read, the fallback |
| H-C14 | PASS | after a restore: /sync with a since ahead of the database, and a direct roster read |

### HC-A7: PASS

- holds: the state list holds one create event: one
- holds: the create event carries its sender, the admin: @admin:hca.invalid
- holds: its event ID is $ and the room ID without !: $zTPX4gOKMWiov0vcqos47BLKYFZrPXpP9uBHil3GgoQ for !zTPX4gOKMWiov0vcqos47BLKYFZrPXpP9uBHil3GgoQ
- holds: room_version 12 and no additional_creators: {"room_version": "12"}

### HC-A4: PASS

- holds: main: the power levels read back unchanged: exact
- holds: reports: the power levels read back unchanged: exact
- holds: questions (open): the power levels read back unchanged: exact
- holds: questions (closed): the power levels read back unchanged: exact
- holds: main: an agent (0) posts m.room.message: HTTP 200
- holds: main: a human (50) posts m.room.message: HTTP 200
- holds: main: a human's reaction is refused: HTTP 403 M_FORBIDDEN
- holds: main: a human's sticker is refused: HTTP 403 M_FORBIDDEN
- holds: main: a human's poll (m.poll.start) is refused: HTTP 403 M_FORBIDDEN
- holds: main: a human's poll (org.matrix.msc3381.poll.start) is refused: HTTP 403 M_FORBIDDEN
- holds: main: an agent's reaction is refused: HTTP 403 M_FORBIDDEN
- holds: main: a human's redaction of their own message is refused: HTTP 403 M_FORBIDDEN
- holds: main: a human's state event (m.room.topic) is refused: HTTP 403 M_FORBIDDEN
- holds: main: an agent's state event (teamchat.roster) is refused: HTTP 403 M_FORBIDDEN
- holds: main: a human's invite is refused: HTTP 403 M_FORBIDDEN
- holds: main: an agent's invite is refused: HTTP 403 M_FORBIDDEN
- holds: questions (closed): a human's message is refused: HTTP 403 M_FORBIDDEN

### H-C12: PASS

- holds: an agent in main joins reports by the restricted rule: HTTP 200
- holds: an account outside main cannot join reports: HTTP 403 M_FORBIDDEN
- holds: the joined agent posts in reports (10): HTTP 200
- holds: a power-0 human is refused a post in reports: HTTP 403 M_FORBIDDEN
- holds: a pending invite stays pending across filtered syncs: 3 syncs, membership invite

### HC-A2 (server): PASS

- holds: .m.rule.suppress_notices comes before .m.rule.is_user_mention: .m.rule.master, .m.rule.suppress_notices, .m.rule.invite_for_me, .m.rule.member_event, .im.nheko.msc3664.reply, .m.rule.is_user_mention, .m.rule.is_room_mention, .m.rule.tombstone, .m.rule.reaction, .m.rule.room.server_acl, .m.rule.suppress_edits, .org.matrix.msc3930.rule.poll_response
- holds: an agent's m.notice mentioning the human does not notify: (notifications, highlights) (0, 0) before, (0, 0) after the notice
- holds: an agent's m.text mentioning the human notifies and highlights: (1, 1) after the text

### HC-A6: PASS

- holds: the service account is the admin room's creator: @conduit (v1 reserved conduit: the same)
- holds: a member can look the account up: HTTP 200

### HC-A1: FAIL

- holds: a link's default sync shows it online in another member's filtered sync: [{"currently_active": true, "displayname": "hca.1+lab.host", "last_active_ago": 6, "presence": "online"}]
- FAILS: a sync with set_presence=offline leaves presence unchanged: presence after it: offline
- holds: a link's PUT /presence of offline takes effect: presence after it: offline
- holds: the timers a team's config leaves at Tuwunel's defaults: presence_idle_timeout_s 300, presence_offline_timeout_s 1800
- holds: a member that stops syncing goes offline by the server's timers: with presence_idle_timeout_s 10, presence_offline_timeout_s 10: online at 0 s, then unavailable at 10 s, then offline at 20 s

### H-C13: FAIL

- FAILS: a second login with the same device_id makes the first token answer M_UNKNOWN_TOKEN: the first token then answers HTTP 200
- holds: POST /logout kills the token: it then answers HTTP 401 M_UNKNOWN_TOKEN; the first token answers HTTP 401 M_UNKNOWN_TOKEN
- holds: the token alone cannot change the password: HTTP 401, stages ['m.login.password']
- holds: the token alone cannot deactivate the account: HTTP 401, stages ['m.login.password']
- holds: the token alone cannot mint a login token (so cannot add a device): HTTP 403 M_FORBIDDEN
- holds: a config key refuses a member's own password change and deactivation: no such key, so the config sets none (the health tick is the watch); the keys that match do other things: login_with_password: turns password login off for every account, which DC16 (c') needs; turn_password: the TURN server's credential; auto_deactivate_banned_room_attempts: deactivates a user who joins a banned room; emergency_password: the service account's own password
- holds: an admin read lists a member's devices and its deactivated flag: devices ['0hzmORhQId', 'HCASEAT'], deactivated False
- holds: the admin read shows a deactivated account as deactivated: deactivated True; its token answers HTTP 401 M_UNKNOWN_TOKEN
- holds: the fallback: DELETE /devices/<id> with UIA by password kills that device's token: without auth 401, with it HTTP 200; the device's token then answers HTTP 401 M_UNKNOWN_TOKEN, the deleting one HTTP 200

### H-C14: PASS

- holds: what /sync answers to a since ahead of the database: a wait for the timeout (3.0 s of a 3 s timeout): since 344, next_batch 335, errcode -
- holds: a direct state read of the roster answers at once, as the backup holds it: HTTP 200 in 0.00 s: {"hca": "before the backup"}
- holds: (recorded) a post after the restore reaches a member still holding the old since: NOT delivered (next_batch 338): a member must drop its since after a restore

Verdict: FAIL (exit 1)
