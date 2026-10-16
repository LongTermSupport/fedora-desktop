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

## Re-check

Branch `feat/hot-control-m6` at 43a7e79f: 71644ded (B1 in Go), 6f9375e3 (ccy `--no-bus`
removes its name's directory), 0ccf32d2 (`PINGBUS_SEATS` gone), 43a7e79f (journal). Read-only;
no qa-all.

### Verdict: MERGE

B1 is closed by a different mechanism than the one prescribed, and the mechanism is sound. One
edge (R1) can still judge a real bus cc session not-bus; it needs a `--` in cc's argv, is
pre-dated by the same reading in `TeamsList`, and does not let a seat be taken: a follow-up
commit, not a merge blocker.

### What was run

- `go test ./internal/teams/... ./internal/registry/...`: pass.
- `python3 -I -m pytest -q tests/helpers/pingbus/test_seat.py`: 101 passed.
- `bash scripts/test-cc-teams.bash`: 80 passed. `bash scripts/test-ccy-teams.bash`: 189 passed.

### Is "the record carries the bus" a single source of truth?

Yes, because of where the record is written and removed, which I traced rather than took from
the commit message:

- `ccy_tmux_insulate` (`tmux-session.bash:505-527`) picks the first free name and writes the
  record **before** `ccy_tmux_start_detached`; the pane then writes the membership. So at the
  moment any membership is written, the record of its name is this launch's. No launch-mid-way
  window exists in which a membership is present and the record is an earlier session's.
- The record is removed only by the trampoline when the launcher returns
  (`session-registry.bash:610-615`), by a failed start (`tmux-session.bash:523`), and by two Go
  paths that first kill the tmux session (`sessions/picker.go:218-227`,
  `teams/shutdown/stop.go:217-230`). Nothing removes a live session's record, so a live bus
  session cannot lose its record while it runs.
- A reused name: tmux refuses a duplicate name on one server and `ccy_tmux_next_name` takes
  only a free one, so "a non-bus launch rewrites the record while the old bus session under the
  same name still runs" cannot occur; the rewrite happens only once the old session has ended.
- A crash or a reboot: the record stays (that is the registry's design), the membership is
  tmpfs and gone. `ccy-sessions restore` relaunches from the record without rewriting it, and
  the relaunch writes a fresh membership under the same name: record and membership agree.
  The restored launcher runs inside tmux, so `ccy_tmux_insulate` returns at once
  (`tmux-session.bash:440-447`) and writes nothing.
- The hot-control rewrite (`registry.go:228-268`, `SetTeams`/`SetCCTeams`) keeps a cc record on
  `--teams` or `--bus` and never adds `--no-bus` to a ccy one, so a record a hot add just
  changed still reads as bus.
- The non-zero-exit pane (`read -r` holds the window): record removed, pane live, membership
  present. Judged not-bus, correctly: the session has ended. Before the fix it showed as an
  "unguarded" cc row.
- `ccy_membership_args` (`agent-bus-membership.bash:45-50`) removes only
  `ccy/membership/<session>` where `<session>` is the pane's own name
  (`ccy_tmux_current_session` answers only when the pane's first process is this launcher or
  its trampoline, `tmux-session.bash:574-581`), so a `ccy --no-bus` typed inside another
  session's pane cannot remove that session's membership.
- The board already required a live tmux session of the membership's name
  (`board/snapshot.go:196-199`), so container-named memberships of plain-terminal or headless
  `ccy --teams` launches were never attributed; `Current` takes nothing from them.

### Could a real live bus session be judged not-bus?

- **R1 (edge, non-blocking).** `LaunchedWithBus` (`discover.go:265-276`) cuts the arguments at
  the first `--` for cc as well as ccy, but cc has no option terminator:
  `cc_teams_split_args` (`cc-teams.bash:56-78`) scans every word, and
  `ccy_registry_replay_args` for prefix `cc` records every word including `--`. Two ways in:
  `cc -- --teams 1@alpha` is a bus session whose record reads not-bus (the test `cc-after`
  in `discover_test.go:305` fixes this reading in); and `cc --teams 1@alpha -- <prompt>`, after
  a hot add, has its record rewritten by `SetTeams` with `--teams` appended **after** the `--`
  (`registry.go:246-248`), so a session hot control just put on a team is then not listed, in
  no team (`SessionsIn`), not reserving its seats in `claimsElsewhere`, and every command on
  it exits 78 "started without --teams or --bus", which is false. Its seat is not lost: the
  keeper holds the lock and the token exists, so another add meets the held/exists checks, not
  a free seat. Fix: for `rec.Prefix == membership.KindCC` do not cut at `--` (match cc's own
  parser), and make `SetTeams` insert `--teams` before the first `--` rather than append; the
  latter also stops a ccy record `ccy -- <claude args>` from handing `--teams` to claude on
  restore (pre-existing, from M3). Add the `cc --teams X -- p` case to `TestCurrent...` and a
  `SetTeams` case with a `--` in `registry_test.go`.
- **R2 (minor).** `hot.launchedWithBus` (`list.go:596-610`), `runningCC` (`cc.go:167-176`) and
  `claimsElsewhere` (`add.go:238-245`) read `listing.Records` and ignore `listing.Unreadable`,
  so a session whose record cannot be read is now silently not-bus: hidden from the list and
  the picker, its seats unreserved. The board reports it (`collect.go:216-219`); the hot paths
  should fail on `Unreadable` rather than drop it (fail fast). Records are written whole and
  moved into place, so this needs a corrupt file or a key an older Go reader refuses.

### Is removing `PINGBUS_SEATS` safe for every launcher path?

Yes. The only places that build a `seat exec` command are `cc_teams_command`
(`cc-teams.bash:138-145`, `PINGBUS_MEMBERSHIP=$dir` always) and the entrypoint's AGENT-BUS
block, fed by `ccy_membership_args` (`PINGBUS_MEMBERSHIP` as a mount or `off`; the entrypoint
refuses a missing marker and a leftover `PINGBUS_SEATS`, `entrypoint.sh:555-578`,
`test-ccy-agent-bus.bash:251-268`). LXC members are made by `play-agent-bus.yml` and have no
launcher in this repository that runs `seat exec` (`docs/agent-bus-lxc-members.md` names
neither variable). No file under `files/`, `playbooks/` or `helpers/` sets `PINGBUS_SEATS` any
longer; the remaining mentions are the entrypoint's refusal, its tests, and history.
`desired_source` now raises 78 with the three launchers named (`seat.py:327-338`), and
`test_seat.py:439-442` and `:847` prove it.

### Do the tests prove the fix?

Yes for what the fix claims: `TestCurrentKeepsOnlyTheMembershipsTheirRecordsLaunchedWithTheBus`
(the rule), `TestAPlainCCUnderTheNameOfAnEndedOneOwnsNoMembership` (list, `SessionsIn`, add 78,
no `agent-bus seat` and no `sudo` called), `TestAddRefusesASeatAnotherLiveSessionWants` (the
75 reservation goes once the record says `--no-bus`), `TestAddToACcyNoBusSessionUnderAReusedNameIsRefused`,
`TestListReadsNoMembershipForACcyNoBusUnderAReusedName`,
`TestCollectSessionsSkipsAMembershipLeftUnderAReusedName`; test-ccy-teams covers the
`--no-bus` removal against a runtime directory of its own. Acceptance (d)
(`_acceptance-m6.inc.bash:375-391`) is the production path: a real membership under D's name,
a plain record written by `ccy_registry_write` as cc writes it, a live pane; it judges `session list` output, exit 78 with the exact sentence, and that B's add of the same seat succeeds
without "named by session D". The gap is R1's `--` case, which no test exercises.

### Merge note: hard-coded versions

Beyond `claude-yolo` (`CCY_VERSION`, `REQUIRED_CONTAINER_VERSION`) and the Dockerfile
`LABEL`, the numbers 3.99.12.x / 2.77.x appear only in prose: `docs/ccy-changelog.md` (the
3.99.12.3 heading and its text), `ACCEPTANCE.md` (one line), `PLAN.md` (one line),
`CLAUDE/Plan/meta-deploy.list` (the play's description), the 26-10-16 journal (two entries,
append-only, leave as written) and `subagent-reports/261016-m6-opus.md`. No script, test or
Go file carries them, so the renumber at merge touches the two version files, the changelog
heading, the ACCEPTANCE/PLAN sentence and the meta-deploy description.
