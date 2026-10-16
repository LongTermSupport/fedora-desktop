# Plan 00203 acceptance

The owner's acceptance steps for each milestone of [SPEC-hot-control.md](SPEC-hot-control.md)
(section 6). `acceptance.bash` is a dispatcher: each milestone's automated part is its own
`_acceptance-<milestone>.inc.bash`, and `CLAUDE/Plan/meta-deploy.bash` runs it after the plays
the milestone needs.

## M2: the bus kit in every ccy session

On an SELinux **Enforcing** host (`getenforce` says `Enforcing`), after `meta-deploy.bash` has
run `play-claude-yolo.yml` for this plan. The first launch after the deploy rebuilds the image
once (container 2.68). The automated part needs ccy launched interactively in this checkout
once, so a headless launch has Quick Launch's token and key.

### What runs by itself

`_acceptance-m2.inc.bash` starts two throwaway headless sessions in ccy's tmux, in this checkout:
a plain `ccy` and a `ccy --no-bus`. Their stdin is held open and no turn is sent, so they stay
idle. It removes them on the way out, with their containers, records, membership directories
and any transcript. Your own sessions are never touched. Then meta-deploy runs the Plan 00201
and Plan 00161 acceptances, ported to M2. They launch `ccy --teams` sessions on throwaway teams,
and that is check (a).

| Step | What passes                                                                                                                                                                                                                                                                | Where                         |
| ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| (a)  | `ccy --teams` works as before: the seats are held, the launch block, `pingbus status`, delivery and the goodbye.                                                                                                                                                           | Plan 00201 M7, Plan 00161 U20 |
| (b)  | The plain session reads `/run/ccy/membership/membership.json` (no seat). A `touch`, a write and a remount there all fail (H3). It has no `active` flag, and its status line is the guard around its own. It has no `crossSessionInbound` and no inbox socket (Decision 1). | `_acceptance-m2.inc.bash`     |
| (c)  | A prompt's hook ends in the shell (no `python3`, no `pingbus`), at most one `pingbus` process runs, and idle CPU over a minute matches the `--no-bus` session.                                                                                                             | `_acceptance-m2.inc.bash`     |
| (d)  | The session image is the launcher's `REQUIRED_CONTAINER_VERSION` (2.68), with the M2 entrypoint.                                                                                                                                                                           | `_acceptance-m2.inc.bash`     |
| (e)  | `--no-bus`: `PINGBUS_MEMBERSHIP=off`, no mount, no `pingbus`, no plugin, no settings.                                                                                                                                                                                      | `_acceptance-m2.inc.bash`     |
| (f)  | The spike's result is in the spec (H34). Claude Code does not re-read `--settings` mid-session, so a session on no team is launched with no inbound, and its first join relaunches it once.                                                                                | `_acceptance-m2.inc.bash`     |

### What to look at yourself

Start a plain `ccy` in ccy's tmux and look at it. Its status line is the checkout's own (the
hooks daemon's, here), with no `bus:` segment, and a prompt is answered as in any session. A
`ccy --no-bus` session looks the same. A `ccy --teams 1@<team>` session shows `bus: <team>`.

### What is not in M2

A plain session runs no `pingbus` process at all until M1's seat keeper ships; from then on it
runs exactly one, the keeper. Adding a running session to a team is M3. For a session that
started on no team, the first join relaunches it once.
