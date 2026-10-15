# Plan 00201 M7: the owner's part of the acceptance

`acceptance.bash` covers what runs unattended (descriptions, a headless session on two teams,
team inference, SessionEnd, a plain session). These steps need a terminal and your answers.
Use two of your own teams, here called A and B, and a GitHub checkout neither covers. Make it
a checkout of another project, not fedora-desktop: the status line must work there too.

01. In the uncovered checkout, run `ccy --teams`. The list shows every team with its
    description, "would add" beside A and B, and "+ Create a team".
02. Tab-mark A and B, then Enter. For each, the scope question names the repository; choose
    Add for A and "Join without adding" for B.
03. Confirm that `play-agent-bus.yml` runs once, for A only, and that no homeserver restarts.
04. Choose a seat and role for each team. The launch block reads "This session is on 2
    teams:", with each seat and description.
05. In the session, look at the status line: it ends with `bus: A,B`, after the checkout's own
    status line when it has one (a hooks daemon's, say). A red `bus: no record ...` fails this
    step. Run `pingbus status`: two `TEAM` lines and two `ABOUT` lines.
06. Ask a member of A to ping this session, then a member of B. The session is woken once for
    each, and its reply to each, inferred from `--re`, appears in the right team's room in
    Element.
07. Run `ccy-teams`. The board shows A's description, and A as up to date with the new
    repository in its scope.
08. In a host checkout of another project, run `cc --teams` and mark A and B. If
    `~/.config/pingbus/element-accepted` does not exist, the consent question names both
    handles: answer No once (nothing is started or recorded, and a checkout that had no
    `.claude/ccy/` still has none), then run it again and answer Yes.
09. The cc session shows the launch block, and its status line ends with `bus: A,B`. End it, then
    check that the per-session record is gone: `ls /tmp/pingbus-sessions-$(id -u)/` must not
    list the ended session's ID (the directory is `$PINGBUS_SESSIONS_DIR` when that is set).
    A file still there means SessionEnd did not remove it: this step fails.
10. If the scope added in step 2 should not stay, remove the repository from A's `repos` in
    the host_vars and run `ccy-teams apply A`.

If `acceptance.bash` was killed (not stopped by an error or Ctrl-C), re-run it. It first puts
back the Quick Launch record from the copy an earlier run saved, or fails naming the file when
no clean copy exists. It then removes its leftover teams, seats and containers.
