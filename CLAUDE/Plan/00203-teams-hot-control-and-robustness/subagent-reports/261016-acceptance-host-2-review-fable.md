# Review: fix/acceptance-host-2 at 5bd673f6 (based on F44)

Adversarial, read-only review of the two host acceptance fixes. Verdict: **MERGE**. No
blocking finding; three low findings below, none a false pass.

## (1) 00203 M2 and 00161 M2.11: the launch leg

**The SIGHUP diagnosis is right.** Reproduced in the container with tmux 3.3a (pane shell
bash, `/bin/sh` is dash), a script that traps HUP, a fifo held open by the harness:

| pane command                                   | result                      |
| ---------------------------------------------- | --------------------------- |
| `exec prog <fifo >out 2>err`                   | SIGHUP before the 2 s sleep |
| `exec prog >out 2>err` (stdin left on the pty) | runs to its exit            |
| `prog <fifo >out 2>err; echo $? >status`       | runs, status file holds `7` |

With no process holding the pty, tmux reads EOF, treats the pane as exited and hangs the
process group up. Running ccy as the pane shell's child keeps the pty open.

**What M2 / M2.11 test is preserved.** `ccy_tmux_current_session`
(`files/var/local/claude-yolo/lib/tmux-session.bash:574`) names the session when
`pane_pid` is `$$` or `$PPID`; the launcher is `/var/local/claude-yolo/claude-yolo`
itself (`_acceptance-m2.inc.bash:35`, `_acceptance-u20.inc.bash:38`), so under the pane
shell its `$PPID` is the pane pid: the `ccy-session` label, the membership mount written
by `record-seats` and the fifo on ccy's stdin are as before. The trailing
`; echo "$?" >…status` records ccy's exit for `m2_ended_early` / `u20_named_up`.

- **Low, comment only** (`_acceptance-m2.inc.bash:68-73`, `_acceptance-u20.inc.bash:1077-1081`):
  the comments say "never `exec`", but a one-command `sh -c` is exec'd implicitly by dash
  (and by bash in some configurations) regardless; it is the trailing `; echo "$?"` that
  forces the fork. Say so, or a later edit that drops the status file for brevity brings
  the SIGHUP back. The owner's `$SHELL` decides tmux's pane shell.

## (2) 00161 M2.3: the supervised watcher

**Stop logic matches pingbus.** `hooks.spawn_watcher` starts `pingbus watch --supervise`
(`hooks.py:400`), which takes the session watch lock, makes it inheritable and runs
`watch --child` (`cli.py:1810-1834`). Neither process installs a signal handler (the only
`signal.` use is the keeper's PDEATHSIG, `cli.py:736`), so one `kill -TERM supervisor child` ends both: the supervisor's TERM is delivered before its backoff (`supervise_retry_s`
1 s) could restart anything, the child's death closes the last copy of the watch-lock fd and
the team lock, so `pingbus status` reads `wake=none`, which the harness waits for before
the idle turn. `supervised_watcher` (u20_check.py) refuses anything but exactly one
supervisor and its one child (parent checked), so a supervisor in backoff, a stray plain
watcher or a second supervisor is a FAIL with the listing, not a guess. The `/proc/stat`
ppid parse (`rsplit(b")")…split()[1]`) is correct. `test_u20_check`: 79 tests pass.

**Hook logic and the 300 s guard.** `_revive` (`hooks.py:596-616`): watch lock held →
nothing; since-last-spawn `< 30 s` → nothing; `< 300 s` → `failed` (UserPromptSubmit
context, Stop block once per 600 s via `no_waker_ms`); else spawn → `restarted`. Against the
harness (`_acceptance-u20.inc.bash:789-822`):

- First idle input, since ≥ 300 s (the common case: accc's mark dates from its launch,
  before M2.0, M2.0b, orders, M2.1, M2.2): `restarted`; harness sees `wake=watcher`,
  requires the `watcher_restarted` text, sleeps 40 s (> `WATCHER_START_GRACE_S` 30, so the
  next revive is not swallowed by the grace), stops again, polls `wake=none`, second idle
  input at ~T0+50 s → `failed` → `pingbus wait` → `wake=waiter`. Correct.
- First idle input, 30 ≤ since < 300: `failed` straight away; `wake=waiter`; no
  `watcher_restarted` required. Correct.
- since < 30: cannot happen (several turns and two legs separate the mark from M2.3).

No false pass: `wake=waiter` is only a `pingbus wait` holding the team lock; the kill is
proved by `wake=none` first; `expect-no-notices any` excludes a watcher delivery; the ack
to the review is found in the room and the stdin line count is exact (2 or 3).

- **Low, flakiness on a loaded host** (`_acceptance-u20.inc.bash:796-806`): in the
  restarted branch the second idle input lands at T0 + (idle turn 1) + ~45 s, and idle
  turn 1 is allowed up to `U20_TURN_S` = 240 s. If that turn takes more than ~255 s the
  second revive is again `restarted`, `wake` is `watcher`, and the final waiter poll fails
  after 240 s. A false fail, not a false pass; an idle haiku turn is normally seconds. Fix
  if wanted: loop the restart branch, bounded (two or three rounds), rather than one fixed
  second round.
- **Info**: if accc ends the idle turn before its background `pingbus wait` takes the team
  lock, Stop's `_revive` says `failed` and blocks once with `watcher_failed`
  (`hooks.py:714`); the session is told to run `wait` again and the second one exits busy.
  Harmless to the check (the turn count only rises; the ack still arrives) and the
  `expect-hook-text watcher_failed` is satisfied either way.

## meta-deploy.list

Exact: no play; `00203-teams-hot-control-and-robustness` (the plan has only
`acceptance.bash`, so nothing deploys); `00201-…/acceptance.bash`;
`00161-agent-team-bus-matrix/acceptance.bash` (not the plan folder, so 00161's
`deploy.bash` does not run). Each entry has its description comment.

## Install-specific names

None in the diff: no hostnames, usernames, home paths, container IDs or non-example
addresses. Plan docs name only `fedora-desktop` artefacts and plan/seat placeholders.

## Docs

DESIGN.md M2.3 and D59, acceptance.bash's leg list, PLAN.md and the journal entry describe
the shipped behaviour (supervisor then child, a second stop inside 300 s, 2 or 3 stdin
lines). `u20_check.py`'s command table updated for `expect-hook-text` and
`supervised-watcher`.
