# Plan 00194: the live path of delivery-acceptance.bash, audited before the host runs it

Branch `fix/00194-delivery-live-path`, from F44 at e3777b225. Not run on a host (this is a
container). The `--fake` run and the plan's unit tests pass here. The meta-deploy.list entry for
`delivery-acceptance.bash` is unchanged.

## The second host fault, and the fix

The leg "take 1@accept194c,1@accept194d and the peer 2@accept194c" gave all three seats to one
`agent-bus seat take`. A take is one session's seats. `seat take` parses its list with
`seat.parse_seat_list(text)`, whose `one_per_team` defaults to true
(`helpers/agent_bus/checkout.py` `_parse_and_check_teams`). So it refused, correctly: "one seat
per team per session".

The fix uses the product as it stands. `dl_take_seats` now runs one take per list in
`DL_TAKES`: first `SEAT_LIST` (the session's own seats), then `2@accept194c` (the peer). The
peer is a seat of the scratch checkout that no session holds. It is never launched, and it posts
through its own bundle (`bus_human.py agent-say --bundle <seats>/accept194c/2`). Its handle is
`fedora-desktop.2+local.podman`, a sibling of the session's seat, and its note arrives as PEER.

- **The session's membership.** It is still exactly `SEAT_LIST`. `gb_launch` passes
  `--teams "${SEAT_LIST}"`, and `gb_seats_held` waits for those seats only.
- **Sudo.** Each take ends with `sudo -k`, and the peer's take makes its seat with `sudo -n`. So
  `dl_take_seats` runs `m7_sudo_alive` before each take and names a lapse.
  - The goodbye run at c3391ba9 already relied on `sudo -n` after a take (its clean-up), and it
    passed.
- **Clean-up.** `gb_park_seats` now parks every seat directory of the run's teams in the scratch
  checkout, as `m7_park_seats` does. That is the session's seats and the peer, in one
  `seat remove`, which accepts several seats of one team. `gb_teardown` calls it on an early stop
  too. For the goodbye acceptance nothing changes, because it only has seat 1 of each team.

## Every live leg, in order

| Leg                                       | Verified, and how                                                                                                                                                                                                                                                                                | Found                                                                                                                                                                                                                  |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| source time (m7 and goodbye includes)     | Ran each `m7_check product-value` reader here: CHECKOUT_SEATS, INSTALL_STATE_ROOT, STATE_ROOT, APPLIED_DIR, SESSIONS_DIR_TEMPLATE, GOODBYE_REASONS[prompt_input_exit]. Also ran `session-paths` (4 lines), `session-names` (watch.log is the third) and `membership-dirs`. All answer.           | ok                                                                                                                                                                                                                     |
| gb_paths, dl_paths (top level, errexit)   | Read against seat.py and membership.py. DL_CTR_HOME is SESSION_HOME and DL_WATCH_LOG is WATCH_LOG.                                                                                                                                                                                               | `gb_paths` now also calls `m7_session_paths`. It was in `gb_prerequisites` (133e60098). It is needed before `gb_leftovers`, and the rehearsal (below) runs this same function.                                         |
| gb_prerequisites                          | The tools, XDG_RUNTIME_DIR, the tracked .claude/ccy files, the owner's host_vars declaring neither team, the saved token.                                                                                                                                                                        | ok                                                                                                                                                                                                                     |
| gb_leftovers                              | `GB_SESSIONS` and `GB_DESC_PREFIX` are set before the include (the include reads them). `expect-own-team` matches on the prefix "Plan 00194 delivery acceptance". `m7_seat_listing` now has its paths. `m7_remove_teams leftover` ran the same way in the passing goodbye run.                   | ok after fault 1                                                                                                                                                                                                       |
| m7_resolve_bus_address .. m7_record_rules | Unchanged since the goodbye acceptance passed on them. The team files declare `HUMAN=tester`.                                                                                                                                                                                                    | ok                                                                                                                                                                                                                     |
| gb_human_passwords                        | Runs before any take, while sudo is fresh (`agent-bus human password TEAM NAME`, helpers/agent_bus/cli.py HUMAN_ACTIONS).                                                                                                                                                                        | ok                                                                                                                                                                                                                     |
| gb_make_checkout                          | The origin, signing off, the tracked ccy files, and the check that the pingbus tree is ignored (the same check `_check_ignored` makes in take).                                                                                                                                                  | ok                                                                                                                                                                                                                     |
| dl_take_seats                             | Checked against checkout.py `take`/`_take`: one take per list, a missing seat created through `sudo -n add-member --out=-`, `sudo -k` at the end. The peer's handle is the localpart of the bundle's user_id (goodbye_check `handle`), which is the PEER line's `from:` (envelope.py `_member`). | **fault 2**, fixed                                                                                                                                                                                                     |
| dl_human_logins                           | `bus_human login --bundle` (config.read_member on seat 1's bundle), then `join` the room from member.json. The password goes on stdin. DL_SEATS holds the seat user IDs used as mentions.                                                                                                        | ok                                                                                                                                                                                                                     |
| gb_tmux_start, gb_launch                  | As in the goodbye run. The launcher's own take finds both seats present, so it creates nothing and asks no sudo. The free peer seat changes nothing for `--teams SEAT_LIST`.                                                                                                                     | ok                                                                                                                                                                                                                     |
| dl_launch (transcript path)               | hooks.py `session_start` records `transcript_path` under `PINGBUS_HOME/inject/<key>.transcript` (inject.py Marks). The glob skips the `.tmp` file.                                                                                                                                               | ok                                                                                                                                                                                                                     |
| dl_quiet, dl_read_live                    | The recorded transcript **file** need not exist yet. Claude Code writes it once the conversation has an entry, and inject.py's DeliveryCheck counts a missing one as size 0. `stat` and `cat` of a missing file failed the check at once, and H1 is the hard check.                              | **fault 3**, fixed: in the container, a missing transcript reads as empty and its size as `absent`; any other error still fails                                                                                        |
| dl_read_live (status)                     | cli.py `cmd_status` exits non-zero when a team fails (FAILURES). Plan 00201's M7 treats a refusing `pingbus status` as "not yet" (`m7_status_ready`). Here it ended H1's poll with "the check itself failed".                                                                                    | **fault 4**, fixed: a refusing status is 3, and the readers pass it on, so a poll waits and then fails at its bound with pingbus's stderr above it. The watch log (made by `spawn_watcher` with O_CREAT) stays strict. |
| H1                                        | `post --mention <seat user ID>` (m.mentions.user_ids). HUMAN `from: tester` (`is_human_localpart`). Then the pane, then no-recv.                                                                                                                                                                 | ok after 3 and 4                                                                                                                                                                                                       |
| H6                                        | `find --sender <seat>` reads `TEXT_KEY.text` (protocol.build_text, which the agent's `say` posts), and exits 3 until it is there.                                                                                                                                                                | ok                                                                                                                                                                                                                     |
| H5                                        | Index 1 of DL_HUMAN_FILES, DL_ROOMS and DL_SEATS is set by dl_human_logins.                                                                                                                                                                                                                      | ok                                                                                                                                                                                                                     |
| H4                                        | The pill prefix (`<user>: `) comes before each token, inside the first bytes of the cut body. HUMAN entries are not flood-limited (E19 counts agents only). No NUL is sent.                                                                                                                      | ok                                                                                                                                                                                                                     |
| 2.0f                                      | The status TEAM line carries `ignored=` (cli.py `_status_team`). `dl_ignored_is` now passes the reader's 3 on.                                                                                                                                                                                   | ok                                                                                                                                                                                                                     |
| H3                                        | The peer's `agent-say --to <seat user ID>` arrives as PEER from its localpart. The human's file was waited for with `dl_poll ... test -e`, but `test` returns 1 while the file is missing, and dl_poll takes 1 as "the check itself failed" (only 3 is "not yet").                               | **fault 5**, fixed: `dl_exists` returns 3 until the file exists                                                                                                                                                        |
| H2                                        | `send-keys -l` then Enter into `=accept194-deliver:`. tool-ran looks only at Bash tool_use commands, so the prompt's own text does not match.                                                                                                                                                    | ok                                                                                                                                                                                                                     |
| 2.0d                                      | The watcher writes "pingbus: team accept194c: the team syncs again" (cli.py) to watch.log. `dl_syncs_again_above` now passes the reader's 3 on.                                                                                                                                                  | ok                                                                                                                                                                                                                     |
| E16                                       | cli.py writes "delivery {nonce} is missing{unread}:", so the substring matches.                                                                                                                                                                                                                  | ok                                                                                                                                                                                                                     |
| logouts, gb_cleanup, purge, C1            | dl_teardown runs before gb_teardown (plan_on_cleanup keeps the order of registration). The clean-up checks `m7_sudo_alive` before it parks. The rules file is in the same run directory (`gb_ev` = `m7_ev`).                                                                                     | The park missed the peer; now every seat is parked                                                                                                                                                                     |

## The rehearsal that would have caught both faults

`delivery-acceptance.bash --fake` now sources the live path's three includes too. Its first two
legs, run in the container with nothing changed, are:

- **set-up (rehearsed)** (`dl_fake_rehearse`). It runs `gb_paths` and `dl_paths` as the host run
  calls them. Then it runs `m7_seat_listing`, the first reader `gb_leftovers` reaches, which
  refuses while the session paths are unread. The fake mode's `podman`, in its own `fake-bin`, lists no
  container. Last, every `DL_TAKES` list goes through `delivery_check.py take-lists`, which parses
  each with the product's `seat.parse_seat_list` exactly as `seat take` does, refuses a seat in
  two takes, and checks that the first take is exactly SEAT_LIST.
- **must be rejected**: the old single take, `1@accept194c,1@accept194d,2@accept194c`. The
  rehearsal rejects it with agent-bus's own words.

Both are proven red. With `m7_session_paths` taken out of `gb_paths`, the rehearsal failed with
"the membership mount and file are not read yet", which is fault 1's message. The reject leg
shows fault 2's refusal. `test_delivery_check.py` has 4 new tests for `take-lists`, and its
fake-run test runs both legs.

Not rehearsed: what only the host has (teams, sudo, containers, Claude Code). Faults 3 to 5 were
found by reading the product, not by a test. They are timing and state conditions that only a
live session produces.

## Verification here

- `python3 test_delivery_check.py`: 23 OK. `test_goodbye_check.py`: 34 OK (its fake run included).
  `test_bus_human.py`: 28 OK.
- `delivery-acceptance.bash --fake`: FAKE ACCEPTED, with the two set-up legs.
- `shellcheck -x` is clean on all four bash files.

## Risks left for the host run

- If sudo does not survive the first take on this host, the peer's take stops at
  `m7_sudo_alive` by name, before anything is launched. The goodbye run's clean-up shows that it
  does survive.
- The peer's bundle (and its token) is in the scratch checkout, which the session can read. The
  session is told to weigh a PEER note, not to post as one, and the checkout is deleted at the
  end.
