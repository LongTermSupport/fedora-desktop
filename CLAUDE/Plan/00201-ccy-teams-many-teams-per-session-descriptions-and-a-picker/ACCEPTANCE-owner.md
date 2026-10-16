# Plan 00201 M7: the owner's part of the acceptance

## Morning start: agent teams with you in the loop

Do these in order, once. Each step names what you should see.

1. **Nothing to deploy.** The host already runs F44 at 8ce5e95d (ccy 3.98.11, image 2.66,
   pingbus plugin 1.2.3, Tuwunel 1.9.4, ccy-teams with the sign-in fixes); those deploys also
   applied every team. The deploy queue is empty, so do not run `meta-deploy.bash`.
2. **Look at the teams.** In a terminal, `cd ~` first, then `ccy-teams`. Run it from your home
   folder: inside a checkout it may offer to start a session there (default Yes), or to create
   a team for it (default No). Every team row should read **up** and **up to date**. If it opens
   with "Not as declared: ..." and "Update them now (play-agent-bus.yml)?", answer Yes. If it
   says "Team ... has no description.", choose "Describe <team> now" and write one line, or
   "Not now (ask again next time)" (Esc is the same); only "Never ask about <team>" stops it
   asking, and the team row's menu has "Ask me to describe <team> again" to undo that. Note every
   **held** seat: each is a running session to relaunch in step 3. Leave with `q`.
3. **Relaunch every team session.** In every session on a team (ccy, `cc --teams`, an LXC
   container's agent), let it finish its turn, then `/exit` and start it again. Why: a
   session loads the bus software (pingbus, its plugin and hooks, the ccy image) only when it
   starts, so an old one keeps the old delivery. And an old session silently drops a threaded
   answer from a new one, reading the reply as an edit (seen live). So relaunch every one of
   them, not just some. A ccy session: in its checkout run `ccy`; under **Quick Launch
   Available** the **Team:** line shows its seats; press Enter at "Use same configuration?
   [Y/n]" and they are taken again. If that line says `none`, press Ctrl-C and run `ccy --teams` instead (step 5). A `cc --teams` session: start it again as `ccy --teams` (step 5), not `cc`. An LXC
   agent: inside the container, as its user, in its checkout, run `ccy --teams <team>`. The
   first ccy launch may rebuild the image to 2.66, which takes a few minutes.
4. **Sign in to Element, once per team.** In `ccy-teams`, press Enter on your name under a
   team (KIND `human`), then choose **Sign in: set a password (shown once)**. If you have
   signed in before, that line is not there: choose **Open `<team>` in Element** and skip the
   password. Sudo may ask for your password. It prints the Homeserver, Username and Password
   once. With the Clipboard Indicator extension on (this repo's default), it says the password
   is not offered for the clipboard: **type the password into Element; do not paste it.**
   Answer Yes to "Open `<team>` in Element now?". The Element window already knows the
   homeserver, so type the Username and Password and sign in. A new password later signs
   out all your Element sessions of that team, so keep this one. Each team is **its own
   Element window**, because each team is its own Matrix server and one Element window holds
   one account. Why, and the options: [RESEARCH-one-element-window.md](RESEARCH-one-element-window.md).
   Next time, press Enter on any team row and choose **Open every team in Element, a window each**.
5. **Start the agents with `ccy`, not `cc --teams`.** `cc` runs as you on the desktop, beside
   your Element sign-in, so a misbehaving agent there could post as you. A ccy session runs
   in a container. In each project checkout run `ccy --teams`. If only one team covers the
   checkout, it says "Team ... works on ..." and takes it without a list. Otherwise, Tab marks
   teams and Enter takes them. For each team, "Team `<team>`: which seat does this session
   take?": take the default. For a seat that already exists you are asked its role: the first
   line, **Keep as is: ...**, or **Make coordinator** for the agent that should take your
   requests (one per team; sudo asks). A **new** seat is not asked: it joins as a worker. To
   make it the coordinator later, in `ccy-teams` press Enter on its seat, then **Make
   coordinator**, then Yes.
6. **Check the session.** The launch prints "This session is on 1 team:" (or "on N teams:"),
   one line per seat with its role and the team's description. The status line ends
   `bus: <team>` (several teams: `bus: <team>,<team>`); a red `bus: no record ...` is a
   fault (below). Next time in that checkout, plain `ccy` shows the seats on the **Team:**
   line under **Quick Launch Available**, and Enter takes them again.
7. **Talk to an agent.** In the team's Element window type `@`, **pick the agent from the
   list** (its handle, like `myrepo.1+local.podman`), write, send. A name you typed without
   picking it from the list reaches nobody. Edits are ignored: send a new message instead.
   Within seconds the agent's avatar appears under your message, which means it was
   delivered. Its answer comes as a reply to your message, starting `[agent-bus] text -> `.
   In the agent's terminal your message shows under "Another Claude session sent a message"
   (Claude Code's own label), then a block starting "agent-bus delivery" with `kind: HUMAN` and
   `from: <you>`. More:
   [Talking to agents from Element](../../../docs/agent-bus.md#talking-to-agents-from-element).

If something is off:

- No avatar under your message: you did not pick the agent from the `@` list, or its session is
  not running (`ccy-teams` shows the seat as held when it is), or it was not relaunched.
- Status line `bus: no record ...` in red, or the agent says a delivery is "missing" or
  "stalled": type `! pingbus status` in that session and send the output.
- "is built on this host but its homeserver is not running": `ccy-teams apply <team>`. "there
  is no team ...": the message lists the teams here; check the name.
- "seat ... is held by another session": that seat's session still runs. Find it with
  `ccy-sessions`, or take another seat.

## The checks

`acceptance.bash` covers what runs unattended (descriptions, a headless session on two teams,
team inference, SessionEnd, a plain session). These steps need a terminal and your answers.

**Before you start:** the Morning start above is done (every team up to date, every team
session relaunched on image 2.66). You need two of your own teams, here A and B,
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
11. One real Ctrl-P paste (a new password signs out your Element sessions of A, so do this
    last and keep the new password). In `ccy-teams`, Ctrl-P on your row under A, answer Yes.
    With the Clipboard Indicator extension on (this repo's default), no copy is offered: one
    line says the extension would use up the paste and keep the password, and the password is
    typed from the screen; its menu in the top bar must not list the password. Then, in the
    Extensions app, switch Clipboard Indicator off, Ctrl-P again and answer "Yes, copy the
    password": paste it into Element's sign-in (Ctrl-V) and sign in; a second Ctrl-V anywhere
    pastes nothing. Switch Clipboard Indicator back on.
12. Shut a team's sessions down (Task 2.14, [SPEC-shutdown.md](SPEC-shutdown.md)). Use a
    throwaway team, or a real one whose sessions may end now, with a ccy session in a seat (two
    is better: leave one idle and one working on a long task). Keep one attached in a
    terminal you can see. In `ccy-teams`, press Enter on the team's row and choose **Shut down
    every session of `<team>`**. It lists each session (and any LXC agent as not reachable),
    then asks, with No highlighted: choose Yes. In the attached terminal you see `/exit` typed
    and the session end; the working one is interrupted first, then ends the same way. Each
    line reads `exited cleanly; seat N@<team> released`. The list then shows those seats
    **free**, and the team row still **up**. A line saying `stopped` or `failed` fails this
    step: send it.

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
