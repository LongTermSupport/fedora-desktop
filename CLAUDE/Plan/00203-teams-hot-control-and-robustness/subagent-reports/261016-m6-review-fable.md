# Plan 00203 M6 review (Fable): cc hot control and `cc --bus`

Branch `feat/hot-control-m6` at f3cecd8e (F44 741191c3 merged). Read-only review against
SPEC-hot-control.md H31/H32 "As built in M6", PLAN.md Decision 7, ACCEPTANCE.md M6 and
`subagent-reports/261016-m6-opus.md`.

## Verdict: FIX-FIRST

One blocker (B1). Everything else holds.

## What was run

- `go test ./internal/teams/... ./internal/registry/...`: pass.
- `bash scripts/test-cc-teams.bash`: 80 passed, 0 failed. (The file is mode 100644 at F44 and
  at HEAD; the qa gate runs it through bash, so this is not a regression.)
- `python3 -m pytest -q tests/helpers/pingbus`: 1524 passed.
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK. `cc-teams.bash` allowlist entry went 108/8/159
  to 87/7/159 (lines/functions/statements): it shrank.

## Blocking

### B1. A stale cc membership survives under a reused tmux session name

`files/var/local/claude-code/cc-teams.bash:285` writes
`$XDG_RUNTIME_DIR/pingbus/membership/<session>/` through `membership-init --kind=cc`, and nothing
ever removes it: not `cc_cleanup` (`files/var/local/claude-code/cc:182` removes only
`CC_TEAMS_TMP`), not the keeper, not a plain `cc` launch (`cc:193` skips the whole teams path when
neither `--teams` nor `--bus` is given). ccy's session names are reused as soon as the previous one
ends (`files/var/local/claude-yolo/lib/tmux-session.bash:78`, `ccy_tmux_next_name` takes the first
free `cc-<project>`), so the common sequence is:

1. `cc --teams 1@alpha` in a repo, as `cc-<project>`; /exit. The directory stays, kind `cc`,
   seats `[1@alpha]`.
2. `cc` (no bus) in the same repo. It is named `cc-<project>` again, its pane is live.

Now:

- `discover.RunningCC` (`src-go/internal/teams/discover/discover.go:377`) counts it: the pane is
  live. `ccy-teams session list` and the M5 picker show the plain cc as a controllable `cc` session
  on `alpha/1`, state "seat not held" or `unguarded: end it and start it again from ccy-sessions`.
  Restarting it does not help; the row is a ghost.
- `claimsElsewhere` (`src-go/internal/teams/hot/add.go:223-245`) treats `1@alpha` as "named by
  session cc-<project>, which has not taken it yet", so a hot add of `1@alpha` to any other session
  of that checkout exits 75 for as long as any plain cc runs there.
- `session add cc-<project> beta` on the ghost resolves (`hot.go:996`, `isCC` is true and the pane
  is live), takes a seat (sudo), writes the membership, then waits for a keeper that does not exist
  and rolls back after the wait, leaving the new seat token in the checkout.

The acceptance cannot see it: every M6 session name carries `$$` and the teardown removes the
directories by hand (`_acceptance-m6.inc.bash:387`), which is itself a hint that production has no
owner for them.

**Fix.** Make the cc launch own its directory the way a ccy launch owns its label: in `cc` (or
`cc_teams_split_args`' caller), when the bus is not wanted and `ccy_tmux_current_session` names the
session, remove `$XDG_RUNTIME_DIR/pingbus/membership/<session>/` before starting claude; and have
`cc_cleanup` remove the session's own directory at exit (the keeper has ended with claude by then,
so no seat is held). With both, a live cc pane has a membership only while that cc wants the bus.
Add a `scripts/test-cc-teams.bash` case (a plain launch clears a stale directory) and an acceptance
step (start a plain cc under a name that has a stale membership; `session list` must not show it).
Note the same class exists for `ccy --no-bus` (`agent-bus-membership.bash:46` sets
`PINGBUS_MEMBERSHIP=off` and leaves any old `ccy/membership/<session>/` in place); it is older than
M6 but worth the same fix in the ccy launcher, in its own commit.

## Judged and holding

1. **Spec conformance.** H31 and H32 "As built in M6" match the code: kind `cc` under
   `pingbus/membership/<session>`, `membership-init --kind=cc` writing the record whole
   (`membership.go:161`, `recordSeats`), readers refusing a kind that does not match its directory
   (`discover.go:201`, `hot.go:418`), liveness by pane, keeper by process tree
   (`cc.go:596-617`), host-side `pingbus reach --root` (`cc.go:626`), repair refused 78
   (`list.go:332`, `cc.go:651`), `SetCCTeams` keeping `--bus` on no seats, `cc` refusing
   `--bus` with `--teams` (64), `describe-seats --sentence` gone, prompt naming no team. The
   "Not in M6" gaps (agent-bus H21 liveness not counting cc; `PINGBUS_SEATS` still read by
   nobody's launcher) are stated honestly. `PINGBUS_SEATS` is now dead code in pingbus: a
   YAGNI follow-up, not a blocker.
2. **Security.**
   - *Escalation past Decision 3.* A cc claude runs as the owner's uid on the host, so it can
     already write any file under `$XDG_RUNTIME_DIR` (a ccy session's `ccy/membership/` too,
     which is mounted read-only only from the container's side), run `ccy-teams session add`
     itself, and read any seat token in a checkout it can see. M6 adds no capability that the
     uid did not have when `cc --teams` shipped; the membership route only reaches seats whose
     tokens already exist in the checkout (creating one needs sudo). The authority boundary is
     the uid, as Decision 3 and the consent text say. No new escalation.
   - *Refuse in force before the join.* `cc_teams_settings` writes `crossSessionInbound: refuse`
     into the `--settings` file (`cc-teams.bash:259`; test-cc-teams checks it). `seat exec` writes
     `home/settings.json` but passes the command argv through untouched (`cli.py:2376-2416`), so
     claude sees exactly one `--settings`, cc's. Claude Code's precedence is policy, then flag,
     then project/local, then user: only a root-written managed policy file could override it,
     and the session cannot write that. The keeper's report is independent: at a launch
     `_inbound = bool(self._held)` (`seat.py:603`), so a `--bus` keeper reports `inbound: false`
     whatever the settings say; `ccRefusing` reads that. Consistent.
   - *`--accepts-inbound`.* No reference remains in helpers, files, src-go, scripts or tests
     (one test name and the changelog sentence only). The ccy entrypoint and `cc` both derive
     inbound from "launched with seats"; the Repair path reads the home's settings
     (`seat.py:669`). Nothing needs the flag.
3. **The exit-1 first join.** `apply` (`add.go:293-325`) reaches `ccNoInbound` only after
   `commit` (membership and record written) and `waitHeld` (the keeper holds the seat and has
   reported). State left: seat token in the checkout, membership names it, keeper holds its
   lock, record `--teams <seat>`, claude refusing inbound. The re-run goes `alreadyIn` ->
   `ccRefusing` -> exit 1 with the same words (`add.go:451`), never "Nothing to do", until a
   relaunch's keeper reports `inbound: true`. Crash windows: after `makeSeat` and before
   `commit`, a free seat token exists and the next add finds it `exists`; after `commit` and
   before the record write, the keeper holds a seat the record does not name, which a restart
   then frees. No window leaves a lock held by nobody: only a keeper holds locks and it dies
   with claude. The restart replays `--teams <seat>`, a fully named list, so no terminal is
   needed and the consent record answers (chooser). Sound.
4. **The merge.** `git diff 741191c3..HEAD --stat` touches only M6's files; the claude-yolo diff
   is the version pair, the seat.py diff is comment-only (the `--accepts-inbound` add and
   removal cancel), `_acceptance-m2`, the U20 fix and the launcher identity fix are untouched,
   and `list.go` is additive on M5's `Sessions`. M5, the fourth fix, the refuse-inbound change
   and the launcher identity fix are all present.
5. **Bash.** Shrank (above); gate green. `cc_teams_command` uses `${session:-cc.$$}`: `$$` is
   cc's pid, fine for a sourced library.
6. **The acceptance.** Throwaway only: teams `acc-m6a`/`acc-m6b` declared in a run-dir
   host_vars file, sessions named with `$$`, records `restore=no`, `m6_prepare` refuses to run
   if either team or seat directory exists. Teardown is registered first (`plan_on_cleanup`)
   and removes tmux sessions, stand-ins, the detached sleep, records, membership directories,
   reports, teams (purge) and seat directories. Checks judge exit codes, exact stderr lines,
   the membership JSON, the record, `agent-bus seat list` and `ps`, never a model; (b) proves
   the sleep is still alive when the seat reads free; (c) checks settings and report together,
   both `--bus` adds exit 1 with the restart sentence and no `Nothing restarted`, the restart
   accepts, repair is 78, the record ends on `--bus` with no `--teams`. No false pass found.
   Minor: `tmux kill-session` ends the driver before its `rm -rf "${tmp}"`, so
   `$XDG_RUNTIME_DIR/cc-teams.XXXXXX` homes are left until logout (0700, tmpfs).
7. **The queue.** `play-claude-yolo.yml` then `00203`, each with a description. Correct.
   ACCEPTANCE.md M6 says `play-agent-bus.yml` must have run too; nothing in the kit changed
   against the deployed F44, so the queue is right and the sentence is slightly over-stated.
8. **No install-specific names.** Grep of the added lines for usernames, home paths, LAN
   hosts and addresses: none. The journal has only `<project>` placeholders.

## Non-blocking notes

- N1. `cc.<pid>` directories (cc run outside ccy's tmux) accumulate until logout; they never
  match a tmux name so `claimsElsewhere` and the board ignore them, but `ReadDesired` reads
  each one every time. The B1 fix (remove at exit) covers them.
- N2. The journal diff reorders existing entries into clock order (01:41, 01:57, 02:03, ...)
  with no text change; the day file is meant to be append-only. Harmless, but the next
  `--journal` run should not re-sort.
- N3. The 07:49 journal entry says Decision 7's interim default is "accepts from launch"; the
  08:22 entry corrects it. Fine as an append-only record.
