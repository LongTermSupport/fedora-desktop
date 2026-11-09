# Plan 00194: the scripted test human and the delivery host checks

Branch `feat/00194-test-human-sender`, from F44. Built test first; not run on a host (this is a
container). Queued in `CLAUDE/Plan/meta-deploy.list`.

## What was built

- **`bus_human.py`**, the sender. Commands: `login`, `join`, `rooms`, `post`, `find`,
  `agent-say` and `logout`. The password goes on stdin only, from
  `sudo agent-bus human password TEAM NAME`. It is never an argument, never written and never
  printed; a test proves `--password` is a usage error. `login` writes a session file, mode
  0600, only ever as a new file; the other commands re-check it as pingbus checks a token
  file. Every request goes through pingbus's own `matrix.Client`, so the token travels only
  in the Authorization header.
- **How `post` shapes a message.** It builds the message as Element does:
  - `m.mentions` is always present. It is `{}` for text with no pill, so a typed `@room`
    mentions no one (Task 2.0f).
  - A pill puts the user ID in `body` and in `formatted_body`.
  - `--at-room` adds the @room pill.
  - `--thread`, `--reply-to` and `--edit` give a thread reply, a reply and an `m.replace`
    edit (Plan 00211's A5).
- **`agent-say`** posts the H3 peer's note as a seat's agent, using pingbus's
  `protocol.build_text`.
- **`rooms`** accepts every pending invite and lists the rooms by name. Plan 00211 needs it
  to find `<team> · questions`.
- **`test_bus_human.py`**: 28 tests against the fake Tuwunel client API, served over real
  HTTP.
- **`delivery_check.py`** and **`test_delivery_check.py`** (19 tests) give the verdicts:
  - It reads envelopes from the transcript. A delivery can be recorded as a user entry or as
    a `queued_command`, and both are found.
  - It checks the pane, no recv, the truncation, the eight hostile bodies, the `ignored=`
    count, and the busy turn's tool call.
  - The fixtures are rendered by the product's `envelope.py`, never copied.
- **`delivery-acceptance.bash`** and **`_acceptance-delivery.inc.bash`** are built on
  `_planlib.inc.bash`. They reuse the goodbye acceptance's legs and Plan 00201's team legs.
  `_acceptance-goodbye.inc.bash` changed in two ways: it now takes a caller's
  `GB_SESSIONS` and `GB_DESC_PREFIX`, and `gb_take_seats` takes an optional seat list. The
  goodbye acceptance itself is unchanged: its `--fake` still passes.
  `delivery-acceptance.bash --fake` passes in the container, with 18 legs accepting and
  rejecting cases.

## Where the sender lives, and why

It lives in Plan 00194's folder, and Plan 00211 refers to it there.

- It is plan-local acceptance scaffolding, so the "Tooling Is Go" rule exempts it.
- It is Python because it reuses pingbus's Matrix client, as the brief asked.
- The plan-tests gate discovers `test_*.py` only inside plan folders, not at the top of
  `CLAUDE/Plan/`. A shared top-level file would have lost its tests from QA.
- Plan 00194 needs it first and stays active the longest, because it waits on hooks-daemon
  #119.
- Plan 00211 finds it the way this plan finds Plan 00201's `m7_check.py`: by searching both
  `CLAUDE/Plan/` and `CLAUDE/Plan/Completed/`. That search keeps working after either plan
  is filed.

## How Plan 00211 would use it

1. It declares a test human in the team, for example `tester`, never the owner.
2. It runs `sudo -n agent-bus human password TEAM tester | bus_human.py login --bundle <a seat bundle of the team> --team TEAM --user tester --session F`.
3. It runs `bus_human.py rooms --session F` and picks the `<team> · questions` row.
4. It reads the root event ID from the QUESTION line, then runs
   `post --thread <root> --room <questions>` with the reply on stdin. It then runs
   `post --edit <reply id>`.
5. It finishes with `logout`.

The owner's Element-only checks remain the owner's: the disabled composer, the notification
and the absent react button.

## What the host run does, step by step

01. It checks the prerequisites. A missing saved token makes it exit 2 with nothing changed.
    It primes sudo, then opens the log.
02. It removes what an earlier run left, then installs `accept194c` and `accept194d` with their
    human `tester` and records their firewalld rules.
03. It reads `tester`'s password for each team with `agent-bus human password`, kept in memory
    only.
04. It makes a scratch checkout and takes `1@c,1@d`, plus `2@c` for the peer.
05. `tester` logs in on each team through `bus_human.py` and joins its room.
06. It starts a tmux server of its own and launches one idle ccy session, `accept194-deliver`.
07. **H1** (it stops the run if it fails): the test human posts a mention. The envelope must
    reach the transcript as HUMAN from `tester` on `accept194c`, closed, and the pane must show
    it. No Bash call may have run `pingbus recv` or `wait`.
08. **H6**: the agent's `ACK-<token>` answer must be in the room from the seat, read as
    `tester`.
09. **H5**: one message per team; both must arrive, each naming its own team.
10. **H4**: a 9216-byte body must arrive cut, with the `truncated:` line pointing at history.
    Then eight hostile bodies must each arrive as one closed HUMAN entry. How the watcher
    batched them is recorded as evidence.
11. **2.0f**: the test human posts a typed `@room`. The session's `pingbus status` must show
    `ignored=` one higher, and nothing may be delivered.
12. **H3**: the peer (seat 2) asks for `h3-peer-<token>.txt`. The entry must arrive as PEER and
    the file must not appear. The human's request for `h3-human-<token>.txt` must produce the
    file.
13. **H2**: a `sleep 60` prompt is typed into the pane. While that Bash call runs, the test
    human posts. The entry must arrive, and where it was recorded is kept.
14. **2.0d**: the run checks `sudo -n` is still valid, then restarts
    `agent-bus-hs@accept194c.service`. The watch log must say "the team syncs again", and the
    next message must arrive. If sudo has lapsed, this step is printed instead and not judged.
15. **E16**: the watch log must call no delivery of the run missing.
16. It logs the test human out, stops tmux, parks the seats, deletes the checkout, purges both
    teams, and checks C1 (nothing left).
17. The verdict: 1 if any check failed. Otherwise 2, with H7 (and 2.0d, if sudo lapsed)
    printed exactly. 0 is not reachable while H7 stays manual.

## What stays manual

- **H7**: no launch leaves out `crossSessionInbound: accept`. ccy's member kit writes it into
  every session's settings. Running H7 needs the owner's decision on how to launch such a
  session; the script prints the step.
- **Success criterion step 5**: the lifecycle restart baseline. It was not part of this task.
- **Element's own rendering**: the run proves the server-side path and the envelope, not what
  Element shows.

## Risks for the host run

- H3 and H6 depend on the model acting. A failure there is recorded, and the run goes on.
- The pane is captured with `-J` (wrapped lines joined) and matched on the delivery's first
  line. Claude Code may collapse a long message in the TUI. The full envelope is judged from
  the transcript.
- The hostile bodies carry no NUL, in case the homeserver refuses one.
