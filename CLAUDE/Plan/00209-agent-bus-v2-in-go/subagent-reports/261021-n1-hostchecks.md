# N1 Task 1.4 (hostchecks): report

Plan 00209 Task 1.4, A's host checks (A 16) and C's H-C12 to H-C14 (C 8). Branch
`feat/bus-v2-n1-hostchecks`. The run's own report is [261021-n1-hostchecks-run.md](261021-n1-hostchecks-run.md);
the earlier agent's handoff is [261021-n1-hostchecks-handoff.md](261021-n1-hostchecks-handoff.md).

## What was built

Three plan-folder probes, in the 00211 spike's shape (plan scaffolding, not repo tooling):

- `hca_server.py`: a throwaway Tuwunel run as the invoking user on a free loopback port, with a
  team's config key set (C 3.2.4) and A 4.1-4.2's rooms as data (main, reports restricted to main,
  questions open and closed). Server name `hca.invalid`.
- `hca_probe.py <repo> <work> <report> [--binary P] [--extra K=JSON]`: judges every server check
  and writes the report. Exit 0 / 1 / 2 (pass / a fail or a missing dependency / not judged).
- `test_hca.py`: the pure parts (pin parsing, config, power levels, the registration MAC, verdicts
  and the report, the since classifier, the push-rule order, the config dump). Run from the plan
  folder with `python3 -m unittest test_hca`: all pass. `ruff check` and `ruff format --check`
  pass on the three files.

## The checks

Run in the CCY container against the pinned Tuwunel 1.9.4: the release asset's sha256
`78628e6d13deda9e4832aeb93f2d10332e3c4e70a8815b45e6ef773404edd5a3` is the pin's
`tuwunel_sha256_x86_64`; the unpacked binary's sha256 is
`8e9a5873d8a2cea62c808106426fbeae16bf49333089f1eb2adec9fd3d6e0b2b`. The container needs
`db_pool_workers=4` (the default pool hits EAGAIN under its pids limit); nothing else differs
from a team's config.

| Check           | Verdict    | Fact                                                                                                                                                                                                                                                                 |
| --------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HC-A7           | PASS       | `GET /rooms/{room}/state` holds one create event, with its `sender`; the room ID is the create event's ID with `!` for `$` (room version 12), so one read verifies a room                                                                                            |
| HC-A4           | PASS       | 4.2's power levels for main, reports and questions (open and closed) read back exactly; a human's reaction, sticker, both poll types, redaction, state event and invite are refused, an agent's state event, reaction and invite too, a post in closed questions too |
| H-C12           | PASS       | an agent in main joins reports by the restricted rule, an outsider cannot; the agent posts in reports at 10, a power-0 human is refused; a pending invite stays pending across filtered syncs                                                                        |
| HC-A2 (server)  | PASS       | the human's push rules put `.m.rule.suppress_notices` before `.m.rule.is_user_mention`; an agent's `m.notice` mentioning the human leaves the counts at (0, 0), its `m.text` makes them (1, 1)                                                                       |
| HC-A2 (display) | NOT JUDGED | Element's display: an owner step after gate A (below)                                                                                                                                                                                                                |
| HC-A3           | NOT JUDGED | Element's reply, thread and edit shapes: an owner step after gate A                                                                                                                                                                                                  |
| HC-A5           | NOT JUDGED | Element hiding the reports composer and the react button: an owner step after gate A                                                                                                                                                                                 |
| HC-A6           | PASS       | the service account is `@conduit`, the admin room's creator; a member can look it up                                                                                                                                                                                 |
| HC-A1           | FAIL       | online by the link's default sync, `PUT /presence` offline takes effect, the timers hold; but a sync with `set_presence=offline` SETS offline (finding 1)                                                                                                            |
| H-C13           | FAIL       | logout, UIA on password change and deactivation, `get_token` refused, the admin read and the fallback all hold; but a second login with the same `device_id` leaves the first token valid (finding 2)                                                                |
| H-C14           | PASS       | a `since` ahead of the database waits for the timeout and answers the server's lower `next_batch`; a roster state read answers at once; a post after the restore is not delivered to the old `since` (finding 3)                                                     |

Every server check is judged: seven pass, two fail on one fact each. The three Element checks are
not this task's any more (decision 1).

## Findings on Tuwunel 1.9.4, with the design-text fixes proposed

1. **HC-A1: `set_presence=offline` on a sync sets offline.** A 6.6 (and B's matching step) has
   every syncing request other than the link's long poll pass `set_presence=offline`, assuming "no
   change". On Tuwunel it marks the seat offline, so any second sync would flip a live seat's dot.
   The other facts hold: the default sync marks online, `PUT` offline takes effect, a member that
   stops syncing goes unavailable after `presence_idle_timeout_s` and offline after a further
   `presence_offline_timeout_s` (defaults 300 and 1800: a dead seat shows online up to 5 min and
   is offline after 35 min). Proposed: A 6.6 says no request other than the link's long poll
   syncs at all (the others read by direct state and message requests), and C 3.2.4 sets both
   timers explicitly. With that, OD-A1 (a) stays possible; the decision is the design owner's,
   since the check as written fails.
2. **H-C13: a same-device re-login does not revoke the first token.** Both tokens stay valid
   until a logout, and one logout ends both (they share the device). No config key refuses a
   member's own password change or deactivation (only `login_with_password`, which would break
   DC16), so the config sets none and the health tick stays the watch. Proposed: DC16 (c') takes
   a seat by logout-before-login (log out the previous holder's token, then log in), or takes the
   fallback (`DELETE /devices/<id>` with UIA by password), which holds.
3. **H-C14: a `since` ahead after a restore.** The server waits for the timeout and answers its own
   lower position, and a member still holding the old `since` does not receive a post made after
   the restore. The design already covers it: `restore_epoch` (A 2's roster table, C 3.12.2, B 2.6
   step 08) makes a member drop its sync position and read the roster by a direct state request.
   No fix proposed; the regression test should assert the non-delivery, so a later Tuwunel that
   changes it is noticed.
4. **HC-A6: the service account is `@conduit`.** v1's reserved name holds; A's reserved list keeps
   `conduit`.

## Decisions, with their reasons

1. **The Element mode is dropped.** No `hca_element.py`; its tests are removed from `test_hca.py`,
   so the pure-part tests run and pass. HC-A2's display, HC-A3 and HC-A5 become owner steps in A
   14's Element row, done after gate A. The mode would have made an Element profile on the host,
   and a profile named for `teamchat` there is exactly what gate A forbids before the owner
   confirms. Making A 14's Element row carry these three checks is a design fix for the design
   agent, not this task.
2. **No host script and no meta-deploy entry.** The server facts were established in the container
   against the exact pinned binary (the asset's sha256 is the pin's), so a host run of the same
   binary would repeat them. The permanent regression form is the Go test behind the `tuwunel`
   build tag once the matrix client merges (A 14, the server-behaviour row); its presence clause
   needs finding 1's correction.
3. **The probes stay Python, in the plan folder.** They follow the 00211 spike's shape and are
   plan-local scaffolding, not repo tooling; the Go test above is the delivered form. The server
   name is `hca.invalid`, carrying no product name. The downloaded asset and the decompression
   helper stay untracked.
