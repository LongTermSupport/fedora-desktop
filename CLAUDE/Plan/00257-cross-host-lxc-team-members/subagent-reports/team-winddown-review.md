## QA review: Plan 00257 B.13 (team wind-down), range 9ccd9f9c5..4d146d09f

**Verdict: FIX-BEFORE-MERGE**

Nothing in this range can kill or stop an LXC seat during a shutdown. The two problems that matter are the v2 "Start its session" action, which can still restart a running session in a narrow window, and Task B.13 being ticked done while the spec is not fully met.

### Should fix

1. **The "refuse if running" check before `lxc restart` can be beaten by timing.** `src-go/internal/teams/lxcsession/v2.go`, from the `UnitState` call to the `Sudo.Capture(... "lxc","restart","--member" ...)` call in `startV2`.
   - The check reads the unit's state, then a separate sudo call runs `teamchat-admin lxc restart`. That command does a reset and a real restart (`src-go/internal/teamchat/host/lxc/restart.go:154-158`), so if the unit becomes active between the two calls, its brand-new session is ended.
   - The window is short, but the guarantee depends on timing.
   - If the check cannot be made, it is safe: every error path returns "Nothing was started", and `offersV2` only keeps the menu line visible when it cannot check.
   - Fix: add a start-only verb or flag to `teamchat-admin lxc` that uses systemd's start, which does nothing to a unit that is already running. ccy-teams should call that, never `restart`.

2. **v2 host seats are not closed, but B.13 is marked done.** `src-go/internal/teams/shutdown/v2.go` (`planForV2`) and `PLAN.md` (the B.13 line, `[x] ✅`).
   - SPEC TC2 covers host seats found from "a session record or a desired membership", and the owner asked for ALL active seats. The code instead reports every v2 host member as "not reachable" (`whyV2Host`), so any v2 team with a host member always exits 1.
   - How they could be found: a v2 ccy session's membership lives in the owner's seat store under the ccy session's name (`Store.Session`, `Store.Control`, `Store.DesiredFile(SessionName)` in `src-go/internal/teamchat/paths/paths.go:693-699`; `SessionName` "names a ccy session", `ids.go:45`). That file is what `ccystart` mounts as `TEAMCHAT_MEMBERSHIP` (`ccystart.go:12-17`).
   - So: read each control `desired.json` that names the team and match its session name against the live `tmux -L ccy` sessions. That is the host path that already exists.
   - Either build this, or untick B.13 and record it as an open gap.

3. **A missing v2 LXC record looks the same as "no seats".**
   - `lxcstate.ReadRecord` returns an empty record when the file is missing (`lxcstate.go:254-255`). `resolveV2Lxc` ignores the found flag, and `planForV2` skips every status row of kind `lxc`.
   - Result: a status record listing LXC members with no LXC record gives "Team T has no running session.", exit 0.
   - Fix: any `kind: lxc` status member with no matching non-left LXC record entry should be listed as not reachable.

4. **"No such file or directory" through lxc-attach is read as "no session".** `src-go/internal/tmuxccy/tmuxccy.go:130`
   - `List` treats any stderr containing "No such file or directory" as an empty server. Inside lxc-attach, that text can also come from a missing `runuser`/`tmux`/`env` or from lxc-attach itself.
   - `addGuest` (`shutdown/lxc.go`) would then report the seat as "no session running in container C" (exit 0) when it was never actually looked at.
   - Fix: for guest servers, accept only tmux's own no-server or socket-path wording, and report anything else as unreachable.

### Nits

- **TC15 wording does not match the code.** The spec says a v2 LXC row shows "whether its session runs". The code (correctly) says "on/off the bus at the last health check" (`board/v2.go`, `wordOnBus`/`wordOffBus`). Change the spec to match the code.
- **Extra sudo calls on every board refresh.** `setSnap` (`picker/picker.go:126-130`) calls `Offers` for every v2 LXC row. For v2 rows that is a passwordless-sudo `lxc-ls`, the LXC path lookup and an `lxc-attach systemctl show`, per row.

### Checked and clean

- **Nothing kills or stops an LXC seat.**
  - All three `KillSession` routes (`closeWindow`, `hardStop`, and the launcher-question kill) are reached only for host jobs: `notClaude` returns early for a guest, and `timedOut` handles a guest before `hardStop`.
  - No `systemctl stop` or `lxc-stop` exists in the changed code.
  - A guest server that cannot be listed fails only its own sessions (`run` keeps a separate failure per server).
- **Command-line safety.** Everything is passed as an argument list through `execx`, with no shell. `tmux -S <path>` comes from `paths.GuestTmuxSocket`. lxc-attach uses `--clear-env` and `env -i`. No secrets appear in any command line, and tmux targets go through the existing `Submit`/`SendEnter` paths.
- **v1 flows never act on a v2 team.**
  - `V2Teams` is kept separate from `Teams`.
  - apply, remove, backup, restore, login and `OpenEveryTeam` all check against or loop over `snap.Teams` only.
  - restart and wind-down refuse a v2 team by name.
  - The picker offers only `v2Actions` on v2 rows.
  - Loops over `Rows` either filter by team or need `Held`/`Dir`, which v2 rows never have (`seats/role.go:109`).
- **Confirmation** defaults to No (`shutdown.go:215`), with crumbs `<team> › Shut down`, a context line naming the team and the session count, and the seats listed in the preview before the question. With no terminal and no `--yes` it refuses with exit 64.
- **Tests.**
  - TC2-TC9, TC12 and TC15-TC17 each have named tests that run through teamtest fakes, and sessclose has guest tests G1-G4.
  - There is no test for the check-then-restart race in finding 1, and none for a guest whose compaction runs past 600 s.
- **Version.** `CCY_VERSION` is 3.122.36 and the changelog has entries for .35 and .36. No container bump is needed: no changed path matches `files/var/local/claude-yolo/image-source-paths`.
- **Public-repo safety.** The only IP added is 192.0.2.10 (an RFC 5737 example address). No usernames, home paths or hostnames were found.
- **Docs.** `docs/agent-bus.md` and `usage.txt` match the behaviour.

### Mechanical gates

- `go test -race -count=1` on sessclose, tmuxccy, lxcfreeze, systemd and `./internal/teams/...`: passed (exit 0).
- `scripts/test-ccy-teams-command.bash`: 397 passed, 0 failed.
- I did not run `qa-all.bash`, `plan-qa --sweep` or `--syntax-check`. Full QA is not allowed inside a sub-agent, and no playbooks changed, so the coordinator needs to run the full gate.