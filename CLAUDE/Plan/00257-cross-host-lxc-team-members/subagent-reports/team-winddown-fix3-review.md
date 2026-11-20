## QA Review: commit 7b45baa3e (Plan 00257 B.13 review 3, v2 host seats decided by holdings)

**Verdict**: FIX-BEFORE-MERGE

The fix closes the reported blocker for the leftover `desired.json`. The same failure still happens through a leftover **holding**, which the seat store deliberately keeps after a crash.

### Should fix (residual of the original blocker)

1. **Shutdown can still close an unrelated session when a crashed holder's name is reused.** `src-go/internal/teams/shutdown/v2host.go:87-109` (`closeHolders`) closes any live ccy tmux session that a complete `held.json` names. It never asks whether that session's run is still alive. The chain, each step grounded:
   - **Names are reused.** `lib/tmux-session.bash:75-84` (`ccy_tmux_next_name`) gives each new session the first free `ccy-<project>[-n]` name among live tmux sessions.
   - **The holding survives a crash.** A registry record is written when a session starts and removed only when its command returns (`internal/registry/registry.go:1-5`), so a crash or reboot leaves it. `ccy_registry_write` rewrites the record whole under the name (`lib/session-registry.bash:247`).
   - **The sweep keeps it.** It releases a holding only when `!Restorable` (`internal/teamchat/sweep/sweep.go:75-81`). `Restorable` just means "a registry record exists under this name" (`teamchatlaunch/adapters.go:28-41`). Its `Desires` reads the old session's `desired.json`, which nothing removes, so the holding comes back `Kept` (`seatsweep.go:191-204`).
   - **The result:** after a crash, a fresh `ccy` in that project (no restore, no bus, or another team) takes the same name and refreshes the record. The team's holding then names it, and `ccy-teams shutdown <team>` sends it `/compact` and `/exit`. That is the original defect, now carried by a different record.
   - **The store already tells these cases apart.** It judges a live keeper by the session seat lock: `sessionHolds` probes `SessionSeat(session, team)` for `lockHeld` (`seat/take.go:229-243`). `Announce` is the "crashed-session path (SD3)" for a holding whose lock is free (`seat/release.go:274-294`). Shutdown should do the same: close only when `HeldBy` matches **and** the session's seat lock for that team is held. A complete holding with a free lock should be reported as a crashed run, not closed.
   - **No test covers it.** `TestALeftoverDesiredMembershipDoesNotMakeASessionTheTeams` (`v2host_test.go:667`) plants a leftover `desired.json` but no leftover holding. A red test is needed: a complete holding naming `ccy-x`, a live unrelated `ccy-x`, and a free seat lock.

2. **A crashed holder gets the wrong diagnosis and exit 1.** The same stale holding, when no session is live under that name (after a reboot with no restore yet), falls to `v2host.go:65-70`. It is reported as `held by session X, which is not a session on this host's ccy tmux (a ccy session started outside tmux ...); type /exit in it where it runs` (`v2.go:22-23`), and the run exits 1. The session is dead, so there is nowhere to type `/exit`. The lock probe from finding 1 would separate "crashed, kept for restore" from "live outside tmux". Spec TC12 (`SPEC-team-close.md:84-89`) should then name the crashed case.

### Nits

3. **`seatsweep.Desired.Read` is now a dead export.** It was exported in 5cb21944e for shutdown, and since this commit its only callers are inside `seatsweep` (`seatsweep.go:196` and the tests at `seatsweep_test.go:382-392`). Unexport it (YAGNI). The dead-code gate does not catch exported methods.
4. **`checkouts()` silently skips pool entries it cannot parse.** `v2host.go:142-146` drops any entry that is not a valid checkout key or not a directory. The sweep reports the same entry as "not a checkout's pool" (`seatsweep.go:149-151`), and `Holdings` treats a strange name as `ErrInvalid`. The skip goes the safe way (the member ends up "elsewhere", exit 1), but it is a fail-fast inconsistency in new code that inherited it from `inPool`.

### Checked and clean

- **Holdings of another team:** `teamHoldings` filters on `h.Seat.Team == team` (`v2host.go:123-127`).
- **Incomplete or partly written holdings:** `openHeld` reports any `held.json` that is missing or does not decode as `Complete:false` (`seat/release.go:226-240`). `HeldBy` never matches it, and shutdown reports it once as unreachable. A take caught between `mkdirFresh` and `create` (`take.go:259-271`) therefore gives a cautious exit 1, never a wrong close.
- **ReadHoldings without the lock:** it reads through directory file descriptors. A `held/` removed mid-read is ENOENT and skipped; any other error propagates and makes every member unreachable once (`TestUnreadableHoldingsAreNotReachableOnce`). It creates nothing when the store is missing (tested). Fail-fast holds.
- **Two hosts sharing a store:** not checked. The store sits under the owner's `XDG_STATE_HOME`, and I found nothing that shares it.
- **Verbs.end and the launcher's take keep their behaviour:** `HeldBy` has the same predicate as the removed loops (`Complete && Session==`), and the take adds the same seat equality as before. Filter order is kept. `TestBusDisconnectedOnTeamchatEndsTheSessionsSeats` (`verbs_test.go:333+`) covers another session's holding, an incomplete one, and the order.
- **UnitStates matching by Id:** the block count is checked, and a missing Id, an Id not asked or an Id named twice is refused. A missing unit leads to empty states and an error. The only units asked are `teamchat-session-<[a-z0-9_-]>-…-<hex>.service` (`paths.go:184-198`, `lxcsession/v2.go:34`), so no `.service` suffix or escaping can differ. An alias unit would fail the call, which is fail-fast. The rows of one container cannot repeat a unit, since each seat key is distinct.
- **Container 2.84.41 bump:** it is required, because `image-source-paths:51` lists `src-go/internal/teamchat/seat/*`. The LABEL and `REQUIRED_CONTAINER_VERSION` agree, `CCY_VERSION` is 3.122.41 with an updated comment, and the changelog entry is accurate.
- **Docs:** SPEC TC2/TC12, `docs/agent-bus.md`, the help text (`shutdown.go:131-139`), PLAN B.13 and the GoProjectRules red-proof command all match the code (apart from the crashed-run case in finding 2).
- **Install-specific names:** none in the plan, docs or tests; the tests use placeholders (`ccy-container-a`, `repo.1+site.podman`, `/srv/proj`).

### Mechanical gates

- **Go tests:** `go test ./internal/teamchat/seat/... ./internal/teams/... ./internal/lxcfreeze/...` exited 0, with 34 packages ok.
- **plan-qa --sweep:** no findings for 00257. Two block findings exist in other plans (00244, 00238); this commit did not cause them.
- **qa-all.bash:** not run. The hooks block a full QA run inside a sub-agent (R-SUBAGENT-FULL-QA), so the coordinator owns it.
- **qa-changed.bash:** not run in this review either.
- **syntax-check:** not applicable, no playbooks changed.

Key files:
- /workspace/untracked/worktrees/sealed-carry/src-go/internal/teams/shutdown/v2host.go
- /workspace/untracked/worktrees/sealed-carry/src-go/internal/teamchat/sweep/sweep.go
- /workspace/untracked/worktrees/sealed-carry/src-go/internal/teamchat/seat/take.go
- /workspace/untracked/worktrees/sealed-carry/src-go/internal/teamchat/seat/release.go
- /workspace/untracked/worktrees/sealed-carry/src-go/internal/teams/seatsweep/seatsweep.go