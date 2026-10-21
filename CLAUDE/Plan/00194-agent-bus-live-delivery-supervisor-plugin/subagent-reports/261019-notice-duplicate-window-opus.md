# Task 2.0i: pingbus's own notices and the duplicate window

## Finding

The host acceptance at F44 96454752 proved Task 2.0h: on the window close the launcher
survived the hangup, stopped the container and ran `pingbus seat disconnected`. pingbus then
refused each seat's notice: `disconnect.log` has `failed: rate limited: duplicate` for both
seats, and the launcher's stderr `no disconnect notice: rate limited: duplicate`, then exit 9.

The send gate (`helpers/pingbus/limits.py`, `SendGate.admit`) refuses a send whose key was
sent in the last `DUPLICATE_WINDOW_S` (60 s). An agent text is keyed by the pseudo-verb
`text`, a sha256 of the text and the set of addressees (`cli._send_text`). `DISCONNECT_TEXT`
is fixed for a handle, and the addressees are the team's humans and coordinators, so G3's
notice (19 s after G2's, same seat) had G2's key.

## Every automatic notice, checked

| Notice                                      | Where                            | Posted to the room?             | Defect    |
| ------------------------------------------- | -------------------------------- | ------------------------------- | --------- |
| `<handle> disconnected: ...`                | `cmd_seat_disconnected`          | yes, `_post_to_humans_and_...`  | yes       |
| `<handle> left the team: session ended (r)` | hook session-end, `post_goodbye` | yes, same poster                | yes       |
| `<handle> left the team: removed by ...`    | `cmd_seat_farewell`              | yes, same poster                | yes       |
| `joined`, `left`, `teams_now` lines         | `hooks.py` templates             | no: hook context in the session | not gated |
| MEMBERSHIP envelope                         | `envelope.py`                    | no: typed into the session      | not gated |

The goodbye's case: two sessions on the same seat both ending with `/exit` within a minute
(relaunch, end again). The second goodbye failed as a duplicate, so it was not recorded, and
the launcher's `seat disconnected` then posted `disconnected` in its place. The farewell's
case: a seat removed, added back and removed again within a minute.

## Choice: (a), exempt the notices from the duplicate window

The window exists to stop an agent repeating itself (a loop re-sending the same ping or text).
pingbus's notices are not an agent's words: each is told once by construction, the
disconnected notice per claim (the seat lock is held and emptied, and the claim is checked
against the launch and the goodbye record), the goodbye per session (`goodbye.json`), the
farewell per removal (the seat held throughout). With a fixed template the window can only
ever refuse a second, true notice.

(b), a claim time or session name in the text, was rejected: it puts a machine stamp into a
message a human reads, has to be repeated in each of the three templates, and leaves the same
trap for the next fixed-text notice. (a) is one change at the one poster the three share.

The token bucket still applies, so a broken caller looping on notices is still stopped at the
member's `send_burst` / `send_per_minute`. A notice does not enter the window either, so an
agent's own later `say` of the same words is not refused because of it.

## Change

- `SendGate.admit(..., deduplicate=True)`; with it false the window is neither checked nor
  recorded, the bucket is. Threaded through `TeamState.admit_send` and `cli._send_text`.
- `cli._post_to_humans_and_coordinators` passes `deduplicate=False`.
- `docs/agent-bus-protocol.md` §10 states the exemption.
- CCY 3.99.12.19, container 2.77.15 (the image carries pingbus); changelog entry.
- meta-deploy: `play-agent-bus.yml`, `play-claude-yolo.yml`, then the 00194 acceptance.

## Tests (written first, seen red)

- `test_seat_disconnected`: a seat that disconnects twice 19 s apart is told twice. Red with
  the host's message, `rate limited: duplicate`.
- `test_seat_retire`: `seat farewell` twice inside the window posts twice. Red, duplicate.
- `test_cli.GoodbyePostTest`: two goodbyes inside the window both post (red, duplicate); a
  goodbye still spends a token (`send_burst` 1: the second is `rate limited: rate`; red as
  `duplicate`).
- `test_limits.SendGateTest`: `deduplicate=False` passes the window but not the bucket, and
  leaves no window entry for a later deduplicated send. Red, unknown keyword.
