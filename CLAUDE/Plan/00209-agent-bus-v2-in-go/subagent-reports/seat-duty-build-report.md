# Seat duty build report (PROPOSAL-seat-kinds.md, owner Q66)

Branch `v2-seat-duty`, from `origin/plan-xhost-lxc-members` (93fd6761a). v2 teamchat only; v1
untouched. Each expectation was a failing test before its code (red seen per package, then green).
Local runs were limited to `go test` (no -race) and `go vet` of the packages changed; GitHub CI
is the gate.

## What was built

1. **`duties` on a `teamchat_teams` entry** (`internal/teamchat/declare/duties.go`, new package
   `internal/teamchat/duty`). A sparse map of member handle to duty, decoded strictly (object,
   at most 256 entries, each key an agent handle that is not the probe, each value in the
   closed set). The declaration reader (what `declare check` runs; the verb itself is not built
   yet in `teamchat-admin`) refuses an unknown duty, a key that is not an agent handle, a host
   member (`+<site>.host`) as `coordinator` or given the coordinator duty (OD1), an entry for
   the coordinator's handle, and the coordinator duty given to any other handle (C30: the
   coordinator's duty follows from `coordinator` alone, PROPOSAL section 3). "A handle that is
   not a member" needs the roster, which the unprivileged reader cannot see, so `team apply`
   refuses it (`apply.checkRoles`, before any change; exit 70 refused, step roles).
   `duties` is a gated field (DC18): in `Gated`, the AG3 difference lines
   (`SECURITY<TAB>team<TAB>duties<TAB>- -> <handle>=<duty>`), `team approve --only duties --add|--remove|--set <handle>=<duty>`, `approved.json` (written only when not empty, so an
   older file reads as approving none and every existing file stays byte for byte) and
   `team.json`.
2. **Defaults** (`duty.Of`, `declare.Team.DutyOf`): the coordinator handle is coordinator, a host
   member is host, every other member is worker; an explicit entry overrides, within the rules
   above.
3. **The duty reaches the seat** (OD2 (a), no Matrix wire change). `desired.json` seats take an
   optional `duty` (`member/state.DesiredSeat`); the keeper keeps it per seat and sends it in
   the link's `add` control message (`wire.Add.Duty`, optional); the engine holds it per team.
   Writers: the LXC apply (`m.team.DutyOf(handle)`) and the ccy launch (`teamchatlaunch`, from
   the team status record's row; none on a machine with no record). A farewell and the seat
   sweep now match a desired seat by team and seat (`Desired.Holds`), since struct equality
   would have stopped matching once a seat carries a duty: `seatsweep.Desires` was such an
   instance and is fixed (test added); `desired.check` was the other.
4. **`teamchat whoami --json`** (new package `internal/teamchat/member/whoami`, wired as an
   outside verb). Shape, documented in the package doc and docs/agent-bus-cross-network.md:
   `{"v":1,"teams":[{"team":"<id>","handle":"<repo>.<seat>+<site>.<type>","role":"coordinator"|"worker"|"probe"|null,"duty":"coordinator"|"worker"|"host"|null}]}`.
   One entry per held team whose handle is known; reads only the run's `status.json`. Exit 0
   (empty list for no seat), 69 `no-session`, 69 `no-link` (no `status.json` yet), 78 `runtime`
   (another run's file, or a strict-decode failure). status.json rows gained `handle` (full
   handle, null until known, its short name must equal `member`) to make this possible.
5. **`duty` on the status rows**: the member's `status.json` team rows (`duty`, nullable) and
   the host status record's member rows (`duty`, optional, never on the probe; written by the
   health view from `team.json`).
6. **The play**: `play-teamchat.yml` hands each team mapping through whole and its input checks
   do not list team fields, so it needs no change. Docs: DESIGN-v2-C-host.md 3.8 shape, a
   `duties` bullet, C46 and the DC18 paragraph; DESIGN-v2-B-member.md verb table row;
   docs/agent-bus-cross-network.md section on duty and whoami.

teamchat ships in the ccy image: container 2.84.45, CCY_VERSION 3.122.52 (2.84.44 / 3.122.51 was
an intermediate commit on this branch; the hook requires a bump per image-touching commit),
`image-source-paths` regenerated (two new packages), docs/ccy-changelog.md entry.

## Decisions to note

- The coordinator duty is never declared in `duties` (PROPOSAL section 3's rule), read as the
  OD1 exception to "an explicit entry overrides the default".
- The membership check lives in `team apply`, so a duty is declared after the member is added.
- A duty change reaches a running session at its next take of the seat (launch or re-add): the
  keeper does not re-send a held seat's changed duty. The LXC host copy does change (a
  re-written desired.json), so a running LXC session keeps the old duty until it relaunches.
- `whoami` leaves out a seat whose handle the link has not read yet.
- PLAN.md has no task row for this work; the coordinator owns adding one.

## Host run (not queued; host runs are paused)

A later meta-deploy.list entry would be the play-teamchat run (to build and install the new
teamchat-admin and teamchat) and the ccy image rebuild via play-claude-yolo.

## Dogfood fault met

`git push` of a branch with nothing new to push (after a pre-commit refusal) failed in the
pre-push hook with `qa-gitleaks: a ref line git did not write:` followed by an empty value,
which names no cause. Not fixed or filed here (load rule; the coordinator should log it).
