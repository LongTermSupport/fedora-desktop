# Plan 00203 acceptance

The owner's acceptance steps for each milestone of [SPEC-hot-control.md](SPEC-hot-control.md)
(section 6). Each milestone's automated part is in `acceptance.bash`, which
`CLAUDE/Plan/meta-deploy.bash` runs after the plays the milestone needs.

## M2: the bus kit in every ccy session

On an SELinux **Enforcing** host (`getenforce` says `Enforcing`), after `meta-deploy.bash` has
run `play-claude-yolo.yml` for this plan. The first launch after the deploy rebuilds the image
once (container 2.67).

### Before acceptance.bash runs

Start three sessions inside ccy's tmux (a terminal ccy opens, or Ctrl-N in `ccy-teams`, so that
`ccy-sessions` lists each one). Leave all three running and idle:

1. `ccy --teams 1@<team>`, in a checkout of a team on this machine.
2. A plain `ccy`, in any checkout.
3. `ccy --no-bus`, in any checkout.

### Steps

| Step | What to do                                                                                          | What passes                                                                                                                                   | Automated by acceptance.bash                                                       |
| ---- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| (a)  | In session 1, send a message to it from Element, and ask it to `pingbus say` something to the team. | It works as before: the launch block names the team, the status line shows `bus: <team>`, the message arrives, the reply is posted.           | the seat is held (`agent-bus seat list`), the `active` flag is up                  |
| (b)  | Look at session 2.                                                                                  | No `bus:` segment: its status line is the checkout's own (the hooks daemon's, here).                                                          | the membership reads; `touch`, a write and a remount in the mount fail (H3)        |
| (c)  | Type a short prompt in session 2.                                                                   | It answers as any session does.                                                                                                               | its hook ends in the shell (no `python3`); at most one `pingbus` process; idle CPU |
| (d)  | Nothing to do.                                                                                      | The first launch after the deploy rebuilt the image.                                                                                          | the image is the launcher's `REQUIRED_CONTAINER_VERSION`, with the M2 entrypoint   |
| (e)  | Look at session 3.                                                                                  | Its status line is the checkout's own; it never shows `bus:`.                                                                                 | `PINGBUS_MEMBERSHIP=off`, no mount, no `pingbus`, no plugin, no settings           |
| (f)  | Nothing to do.                                                                                      | The spike's result is recorded: Claude Code does not re-read a `--settings` file mid-session, so a session is launched with inbound accepted. | the result is in the spec, H34                                                     |

`acceptance.bash` exits 2 (could not establish) and names what to start when one of the three
sessions is not running. While it measures idle CPU (one minute), leave sessions 2 and 3 alone.

### What is not in M2

A plain session runs no `pingbus` process at all until M1's seat keeper ships; from then it runs
exactly one, the keeper. Adding a running session to a team is M3.
