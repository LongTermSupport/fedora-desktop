## QA Review: Plan 00257 B.13 review fixes, range 1a16e4f78..8df65bb66 (worktree `sealed-carry`)

**Verdict**: FIX-BEFORE-MERGE. Finding 2 is not safely fixed: `ccy-teams shutdown` can close a session that does not belong to the team. The other fixes hold.

### Blocking

1. **A leftover desired membership file makes shutdown close another session that reuses the same tmux name.** See `/workspace/untracked/worktrees/sealed-carry/src-go/internal/teams/shutdown/v2host.go` (`resolveV2Host`, `desiredSeats`).
   - Shutdown treats a live ccy tmux session as the team's host seat whenever `Store.DesiredFile(<session name>)` names one of the team's seats.
   - Nothing ever deletes that file:
     - It is written at launch (`teamchatlaunch/launch.go:234`).
     - The end-of-session step `Verbs.end` (`teamchatlaunch/verbs.go:309-348`) removes the registry record and releases the seats, but leaves `desired.json`.
     - A search for `DesiredFile`/`Control(` found no code that removes it.
   - tmux does reuse names: `files/var/local/claude-yolo/lib/agent-bus-membership.bash:59-61` says so and clears only the v1 membership directory. The v1 branch of `launch-args` (`verbs.go:178-188`) does not touch the v2 file either.
   - So: a v2 session named "X" ends, then a no-bus or v1 session starts under "X". `ccy-teams shutdown <team>` now sends that unrelated session `/compact` and `/exit`, with no prompt under `--yes`.
   - **Fix:** take the seat store's holdings as the deciding fact. A session counts only if a complete holding of the team's seat has `Record.Session == name`, which is the check `Verbs.end` already uses. Add a test with a leftover `desired.json` under a live non-team session.

### Should fix

2. **A v2 host seat whose session runs outside ccy's tmux is reported as "no session" and the run exits 0.** See `v2host.go`, the final loop over `rows`.
   - Outside tmux, `ccy_membership_args` files the membership under the container name (`agent-bus-membership.bash:67`), and that name is never in `snap.Live`.
   - The seat is in this host's pool, so it goes to `p.idle` as "no session on this host" and adds nothing to the exit code. That is the "unreachable reported as nothing to do" outcome this round was meant to remove.
   - The v1 path handles this case: a held seat with no live session is reported as `whyHeld` (`shutdown.go:303-304`). The v2 path has no such check.
   - The holdings fix in finding 1 also fixes this: a seat held by a session that is not live is out of reach.
3. **When one session's membership file cannot be read, the same member is reported twice, with contradicting reasons.** In `v2host.go` the error case adds "session N: not reachable", but that session's seats never reach `held`. The loop then also files the member as idle ("no session on this host"). The exit is still 1, but the output contradicts itself. With holdings as the source (finding 1) this goes away; otherwise, keep the member out of `idle` once any membership read has failed.

### Nits

4. `lxcfreeze.Lister.UnitStates` (`attach.go`) checks that the number of answers matches the number of units and that each answer has both states. It matches answers to units by position only, which relies on systemd answering in argument order. Asking for `--property=Id,...` as well would let the code check each answer against its unit.
5. The fix report and `CLAUDE/GoProjectRules.md` state the real-tree red proof (14 findings at 7 sites) without the command that produced it. I re-derived the count from the old literals in the 2d2c7f042 diff and it matches (2+3+2+3+2+1+1). I did not re-run the rule on the old tree.

### Checked and clean

- **Finding 4, `goneAnswer`:**
  - It requires exit 1, no signal, at least one stderr line, and every line in tmux's own wording.
  - Those forms match what tmux's client prints: "no server running on" when the connection is refused, "error connecting to … (No such file or directory)" when the socket is missing.
  - An lxc-attach, runuser or env line, exit 127, or a mixed stderr is classed as a failure, never as gone.
  - Other errors are also classed as failures: permission denied, sudo, and an empty stderr with exit 1.
  - All call sites, including sessclose, winddown and Submit, now use `errors.Is(err, ErrSessionGone)`.
- **The rule `project_tmux_gone_text`:**
  - It is registered in `All()`, which the driver runs as a blocking stage, and `TestAllIsValid` was raised to 53.
  - Its fixtures cover the old instances and a package outside its scope that should stay clean.
  - A search of non-test Go in `src-go` finds no remaining loose match. The only hits are comments, the classifier itself, and lxcfreeze's own `nmcli` text in a package that does not import tmuxccy.
  - Its scope (tmuxccy and the packages that import it) matches the hazard, and the catalogue states what it does not catch.
- **Finding 3:** `unnamedLxcRows` reports an LXC row that no live entry of the team's LXC record names, including a deferred entry that has no handle yet. `TestAV2SeatWhoseTmuxCannotBeReachedIsNotReachable` runs the real tmuxccy classifier underneath, not a fake that skips it.
- **Picker batching:** `OffersAll` keeps the start line whenever a question fails (`keepAll`), so a failure never hides the line.
- **Version bumps:** `CCY_VERSION` 3.122.40 with an updated comment, and changelog entries .38–.40. "Image unchanged" is true: none of the changed paths are in `files/var/local/claude-yolo/image-source-paths`.
- **Docs:** SPEC-team-close TC2/TC12, `docs/agent-bus.md`, the help text and PLAN.md B.13 all agree with the code as written. They will need updating along with finding 1/2's fix.
- **Public-repo safety:** I found no install-specific names in the changed docs or reports.

### Mechanical gates

- **Targeted `go test`** passed on tmuxccy, teams/shutdown, seatsweep, lxcsession, picker, lxcfreeze, sessclose, winddown, teamchat/host/lxc/..., teamchat/admin/... and `qa/rules`.
- **qa-all.bash:** not run. A sub-agent is blocked from full-suite QA (R-SUBAGENT-FULL-QA), so the coordinator's gate run has to cover it.
- **plan-qa --sweep:** 2 blocking findings, both in other plans (00244, 00238); none for 00257.
- **syntax-check:** not applicable, no playbook changed in this range.
- **Correction on my own conduct:** I broke the read-only rule once. The sweep's output was redirected into `/workspace/untracked/worktrees/sealed-carry/untracked/scratch/.planqa-review.txt`, an untracked scratch file. It can be deleted; I left it alone to avoid a second change.