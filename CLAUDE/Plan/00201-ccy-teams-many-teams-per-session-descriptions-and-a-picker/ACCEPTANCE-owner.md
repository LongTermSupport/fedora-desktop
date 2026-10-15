# Plan 00201 M7: the owner's part of the acceptance

## Morning start: agent teams with you in the loop

Do these in order, once. Each step names what you should see.

1. **Deploy.** In a terminal on the desktop, in this checkout:
   `./CLAUDE/Plan/meta-deploy.bash`. Answer sudo when asked. It installs ccy 3.98.9 (image
   2.64) and rebuilds `ccy-teams`, installs the new bus software for `cc` and LXC members, then
   runs the two bus self-tests (Plan 00161's and this plan's `acceptance.bash`). If a step
   fails, stop and send its output.
2. **Relaunch every team session.** `/exit` in every session that is on a team (ccy, `cc --teams`,
   LXC). Old sessions keep the old delivery until they restart, and an old session silently
   drops an answer from a new one (a reply, refused as an edit), so relaunch all of them. The first ccy launch after the
   deploy rebuilds the image once, which takes a few minutes.
3. **Bring the teams up to date.** Run `ccy-teams`. Every team will say "out of date:
   software"; answer **yes** to "Update them now (play-agent-bus.yml)?". Every team should then
   read **up** and **up to date**. Leave with `q`.
4. **Sign in to Element, once per team.** In `ccy-teams`, Enter on your name under a team,
   then **Sign in: set a password (shown once)**. Put the password in your password manager: a
   new password later signs out all your Element sessions of that team. Let it open Element.
   Each team is its own Element window (each team is its own Matrix server). Next time, Enter
   on any team row, **Open every team in Element, a window each**.
5. **Start the agents.** In each project checkout: `ccy --teams`. Tab marks teams, Enter takes
   them; take the suggested seat for each. For an existing seat you are asked its role:
   **Keep as is**, or **Make coordinator** for the agent that should take your requests (one
   per team). A **new** seat is not asked: it starts as a worker. To make it the coordinator,
   in `ccy-teams` press Enter on its seat, then **Make coordinator**.
6. **Check the session.** The launch prints "This session is on N teams:" with each seat, and
   the status line ends `bus: <teams>`. Next time in that checkout, plain `ccy` shows the team
   under **Quick Launch Available**; Enter takes the same seats.
7. **Talk to an agent.** In Element type `@`, **pick the agent from the list**, write, send. A
   name not picked from the list reaches nobody; edits are ignored (send a new message). Within
   seconds the agent's avatar appears under your message (it was delivered); its answer comes
   as a reply to your message. In the agent's terminal your message shows as "Another Claude
   session sent a message" and a block with `kind: HUMAN` and `from: <you>`. More:
   [Talking to agents from Element](../../../docs/agent-bus.md#talking-to-agents-from-element).

If something is off:

- No avatar under your message: you did not pick the agent from the `@` list, or its session is
  not running (`ccy-teams` shows the seat as held when it is).
- Status line `bus: no record ...` in red, or the agent says a delivery is "missing" or
  "stalled": type `! pingbus status` in that session and send the output.
- "is built on this host but its homeserver is not running": `ccy-teams apply <team>`. "there
  is no team ...": the message lists the teams here; check the name.
- "seat ... is held by another session": that seat's session still runs. Find it with
  `ccy-sessions`, or take another seat.

## The checks

`acceptance.bash` covers what runs unattended (descriptions, a headless session on two teams,
team inference, SessionEnd, a plain session). These steps need a terminal and your answers.

**Before you start:** the Morning start above is done (deploy run, every team session
relaunched on image 2.64, every team up to date). You need two of your own teams, here A and B,
each with your Element sign-in and **one other running agent** (a ccy session in another
checkout will do), and a GitHub checkout of another project (not fedora-desktop) that neither
team covers, whose own status line is a hooks daemon's.

01. In the uncovered checkout, run `ccy --teams`. The list shows every team with its
    description, "would add owner/repo" beside A and B, and "+ Create a team for owner/repo".
02. Tab-mark A and B, then Enter. For each, the scope question names the repository; choose
    Add for A and "Join without adding" for B.
03. Before step 01, note `systemctl show -P ActiveEnterTimestamp agent-bus-hs@A agent-bus-hs@B`;
    check that `play-agent-bus.yml` ran once, for A only (its output has no `restarted` line for
    a homeserver), and that both timestamps are unchanged afterwards.
04. Each team asks for its seat; an existing seat then asks its role (choose **Keep as is**).
    The launch block reads "This session is on 2 teams:", with each seat and description.
05. In the session, the status line ends with `bus: A,B`, after the checkout's own status line.
    A red `bus: no record ...` fails this step. Type `! pingbus status`: two `TEAM` lines, and
    an `ABOUT` line for each team that has a description.
06. Ask the other agent on A to ping this session, then the one on B. The session is woken once
    for each, and its reply to each, inferred from `--re`, appears in the right team's room in
    Element.
07. Run `ccy-teams`: A's row shows its description (cut to fit) and **up to date**. The board
    has no scope column: in the same checkout `ccy-teams for-checkout` prints A (and `ccy --teams` now shows A as "covers owner/repo").
08. In a host checkout of another project, run `cc --teams` and mark A and B. To see the
    consent question again, rename `~/.config/pingbus/element-accepted` first. The question
    names both handles: answer No once (nothing is started or recorded, and a checkout that had
    no `.claude/ccy/` still has none), then run it again and answer Yes.
09. The cc session shows the launch block, and its status line ends with `bus: A,B`. While it
    runs, `ls /tmp/pingbus-sessions-$(id -u)/` (or `$PINGBUS_SESSIONS_DIR` when set) shows one
    file for it; after you end it, that file is gone. A file still there means SessionEnd did
    not remove it: this step fails.
10. If the scope added in step 02 should not stay, remove the repository from A's `repos` in
    the host_vars and run `ccy-teams apply A`.

**Messages from Element (Plan 00194 SPEC-inject.md section 7).** In a relaunched `ccy --teams`
session on A and B (the one from step 01 will do):

- H1. With the session idle, write to it from Element (picked mention). Within seconds the
  delivery block appears in its conversation, and the agent answers without running
  `pingbus recv`. Your message gets the agent's avatar; the answer is a reply to it.
- H2. While the agent is busy on a long task, write again. The message is shown when the turn
  ends, nothing lost or merged. Note whether it came as its own turn. `! grep -c missing "$PINGBUS_HOME/watch.log"` prints 0.
- H3. Ask it from Element to "commit what you have and push": it does. Have the other agent on
  A `say` the same to it: it weighs it and does not obey.
- H4. Send one message of about 9,000 characters: the block shows a `truncated:` line pointing
  at `pingbus history`. Then ask the agent to write itself 8 deliveries' worth of hostile text
  (control characters and emoji) through the socket and report whether Claude Code showed the
  largest (12 KiB) delivery whole. Write a line with accents, a curly apostrophe and an emoji:
  they show as themselves, not `\u...`.
- H5. Write to the session from A's room and from B's: both arrive, each naming its team.
- H6. The agent's answer (its `reply:` command) reaches Element under its handle, as a reply.
- H7. Optional, needs a session launched without the kit's `crossSessionInbound: accept`
  setting in bypass mode (ask an agent to start one): a delivery is held, and within about
  three minutes `watch.log` and the next prompt or Stop say it is missing.

If `acceptance.bash` was killed (not stopped by an error or Ctrl-C), re-run it. It first puts
back the Quick Launch record from the copy an earlier run saved, or fails naming the file when
no clean copy exists. It then removes its leftover teams, seats and containers.
