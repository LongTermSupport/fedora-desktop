# Phase 6 readiness: can Tasks 6.4 (cut-over) and 6.5 (v1 removal) run unattended?

This was read-only research at F44 a1d2674a7 (worktree head 086d30456, with the same code). No
tracked file other than this one was changed. Nothing was run on a host. Teams are named by
placeholder: **Team A** moves first, **Team B** second, and **Team C** last. Team C is the one
in a production release. The coordinator holds the mapping to the real names. Line numbers are
from this tree.

Each claim is marked:

- **FACT**: read in code, a plan or docs.
- **INFERENCE**: reasoned from code, but not run.
- **UNKNOWN**: needs a host read or a proof run.

## Answer first

Not as the plan and code stand. Five things block a weekend with nobody at a terminal:

1. **Task 6.5 is not built.** No retirement play or `remove --software` verb exists. As
   written, step 4 of it deletes v1's state roots, which hold the only copy of every v1 room's
   history (Q5).
2. **The owner has gates in Task 6.4:**
   - approving each v2 team (DC18);
   - opening the CA to the v2 address in each checkout's network policy;
   - step 4's human post from Element or Lightning, and the Element sign-in it needs.
3. **Some members cannot move to v2:**
   - Host `cc` members cannot be on v2 (Task 5.1 is not built).
   - v1 LXC members whose sessions were started by hand can be neither reached nor relaunched
     by any tool.
   - A session that holds seats on two v1 teams cannot hold one v1 seat and one v2 seat. The
     launch is refused with exit 64 until Task 4.5 is built.
4. **Neither switch to v2 that keeps the conversation has been proven.** A v2 session's
   conversation record is never read by the host's restore, `ccy --stop`, shutdown or
   wind-down (Q2). So moving a session must be proven on a scratch session first, and closing
   one after it moves is suspect.
5. **Every host step needs the host seat** with passwordless sudo, through meta-deploy. A
   container seat can only prepare, message and commit.

Moving a team off v1 (`state: absent`) is **per team**. The v1 removal (6.5) is
**all-or-nothing** (see "Per team or all-or-nothing").

## Q1. Which 6.4 and 6.5 steps need a terminal or the owner

| Candidate                               | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                            | Evidence                                                                                                                               |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| meta-deploy's own confirmation          | **None.** Running the script is the consent. `PLAN_ASSUME_YES=1` and `RUN_BASH_OFFER_ISSUE=0` are exported.                                                                                                                                                                                                                                                                                                                                        | `CLAUDE/Plan/meta-deploy.bash:111-120`                                                                                                 |
| Reboot prompt                           | **Not on this path.** The "Ready to reboot now?" prompt is only in run.bash's full run. A single play exits before it.                                                                                                                                                                                                                                                                                                                             | `run.bash:2285-2357` (exit at 2356); `run.bash:3937-3954`                                                                              |
| Sudo for plays                          | **Unattended only with passwordless sudo.** With no terminal on stdin and a `RUN_BASH_*` variable set (meta-deploy sets one), run.bash goes headless. Headless needs NOPASSWD:ALL or `RUN_BASH_SUDO_PASSWORD_FILE`, or it stops. Attended runs prompt with `--ask-become-pass`. The list's last entry, `play-ZZZ-passwordless-sudo.yml`, **removes** passwordless sudo unless host_vars declares `passwordless_sudo_for_unattended_deploys: true`. | `run.bash:1278-1304`, `run.bash:411-450`, `run.bash:2332-2347`, `run.bash:1979-1988`; the meta-deploy.list comment above the ZZZ entry |
| Pauses or prompts in the plays          | **None** in play-teamchat.yml, play-agent-bus.yml or play-claude-yolo.yml (no `pause:` and no `vars_prompt`).                                                                                                                                                                                                                                                                                                                                      | grep over the three plays                                                                                                              |
| Prompt in the queued meta-deploy list   | `00257-.../lxc-no-autostart-acceptance.bash` asks per running LXC container, on /dev/tty, whether to reboot it. With no terminal the answer is "no", so it does not hang. In a tmux pane with a terminal it waits for an answer.                                                                                                                                                                                                                   | `lxc-no-autostart-acceptance.bash:147-170`                                                                                             |
| Seat launches                           | `ccy` itself is interactive (Quick Launch, choose-teams, launch-summary on a terminal only). Unattended starts exist only through `ccy-sessions declare` + `ccy-sessions restore`, or `ccy-teams restart --yes`. Each must be given what it would ask (`--token`, `--ssh-key` or `--no-ssh`, `--compose`). A key that needs a passphrase is refused unless ssh-agent holds it.                                                                     | `docs/ccy.md:330-343`; `src-go/internal/teams/usage.txt:171-184`; `docs/ccy.md:238-242`                                                |
| Pool seat (a v2 seat for a ccy session) | `sudo teamchat-admin member add ... --credential password \| teamchat seat place-issued --checkout <dir>`. It needs root on the homeserver host but no person: piped, it writes the plain line. Gate B is met (INT6, the owner's choice of 2026-10-30).                                                                                                                                                                                            | `docs/agent-bus-cross-network.md:104-135`; `PLAN.md:109`                                                                               |
| v2 LXC seat token                       | Placed by `lxc apply` itself; no person.                                                                                                                                                                                                                                                                                                                                                                                                           | `docs/agent-bus-cross-network.md:132-134`                                                                                              |
| Claude token                            | Unattended only while the named token is valid. An expired token forces a new one, which only the owner can make (`ccy --create-token` / `--update-token`).                                                                                                                                                                                                                                                                                        | `claude-yolo:211-218`, `claude-yolo:297-298`                                                                                           |
| v2 Element/Lightning sign-in            | **Owner.** `teamchat-admin human password` prints the password once, on the terminal's alternate screen. The owner then signs in. The Element play places the team CA (host, unattended once `agent_bus_element_teams` has an https entry).                                                                                                                                                                                                        | `docs/agent-bus-cross-network.md:165-215`                                                                                              |
| 6.4 step 4's human post                 | **Owner** (by definition).                                                                                                                                                                                                                                                                                                                                                                                                                         | `PLAN.md:476-478`                                                                                                                      |
| v2 team approval                        | **Owner's decision.** `teamchat_approve_teams` is "the owner's gate (DC18) done by IaC: ... the review of the inventory change is the approval".                                                                                                                                                                                                                                                                                                   | `playbooks/imports/play-teamchat.yml:51-58`                                                                                            |
| Network policy for the v2 address       | **The owner's, on the host.** A ccy session is refused private ranges and the host. Only the v1 bus address is always open. A v2 team on a private address needs a `CCY_NETWORK_ALLOW` line in each checkout's policy file.                                                                                                                                                                                                                        | `docs/ccy.md:1737-1756`                                                                                                                |
| Taking a v1 team down through ccy-teams | `ccy-teams remove` is "on a terminal only". The unattended route is to set host_vars to `state: absent`, then run play-agent-bus.yml (meta-deploy).                                                                                                                                                                                                                                                                                                | `src-go/internal/teams/usage.txt:146-149`; `play-agent-bus.yml:17-18`, `:298-307`                                                      |
| An ask in the v1 removal                | 6.5 is not built, so nothing can be said about its prompts. It is "plays that refuse to run while any v1 team is present", and removing the Element profiles is "if the owner chooses".                                                                                                                                                                                                                                                            | `PLAN.md:484-490`; `DESIGN-v2-D-migration.md:1254-1255`                                                                                |

## Q2. A seat on v2 with nobody at a terminal, and whether it resumes

**FACT: how a v2 launch works.**

- A `ccy --teams` list naming a team that `ccy-teams system` judges teamchat takes a pool seat in
  the checkout, writes `control/<session>/desired.json`, and starts claude under `teamchat run`
  with `TEAMCHAT_MEMBERSHIP` and `PINGBUS_MEMBERSHIP=off` (`LAUNCH-ccy.md:37-41`, `:61-103`).
- A team is v2 for a launch when its status record says so, or when the seat store holds a pool
  seat for it **in that checkout** (`LAUNCH-ccy.md:47-59`).
- A name that is both a declared v1 team and v2-live is refused with exit 64. **So the v2 team
  must have a different id from the v1 team it replaces.**
- A host `cc` launch is v1 only (`src-go/internal/teams/chooser/teamchat.go:15-35`;
  `PLAN.md:409-410`, `:447-449`).
- A list mixing v1 and v2 teams is refused with exit 64 until Task 4.5(a)'s dual launch is built
  (`PLAN.md:409-410`, `:425-429`).

**FACT: what can start a session with no terminal.**

- `ccy-sessions restore` starts every recorded session that is not live. Each starts detached in
  CCY's tmux, with its recorded arguments plus `--resume <id>` (`docs/ccy.md:297-321`).
- `ccy-sessions declare --name <n> --replace DIR -- <ccy args>` writes such a record
  (`docs/ccy.md:330-343`).
- `ccy-teams restart <team> --yes`:
  - restarts a running ccy session in place at its next idle moment, through its supervisor;
  - starts a stopped one as restore does;
  - resumes by id in both cases (`src-go/internal/teams/restart/restart.go:1-15`).
- `ccy-teams internal restart-args` makes a supervisor restart replay the record's current
  `--teams` (`usage.txt:292-296`).
- The only units are the seat sweep timer (releases seats, starts nothing) and
  `ccy-sessions-restore.service` (at boot, opt-in, `ccy_restore_sessions: true`)
  (`LAUNCH-ccy.md:105-136`; `docs/ccy.md:285-300`).
- Restore, restart and `--stop` are all **host** commands.

**FACT: resume is keyed on v1's file.**

- Restore, restart, `ccy --stop`, shutdown and wind-down find a session's conversation only
  at `<checkout>/.claude/ccy/pingbus/sessions/<session>/conversation.json`
  (`src-go/internal/conversation/conversation.go:89-118`, `start.go:33-50`).
- That file is written only by the v1 pingbus SessionStart hook, and not when
  `PINGBUS_MEMBERSHIP` is `off` (`helpers/pingbus/hooks.py:246-253`), which is what every v2
  ccy session runs with.
- A v2 session writes its record into the seat mount instead (`teamchat/paths/paths.go:435-445`,
  `teamchat/member/link/facts.go:407-414`). No host reader reads it.
- The v2 keeper resumes from its own record only under `--resume-recorded`
  (`teamchat/member/agent/agent.go:124-127`), and no launcher passes that flag (grep: no caller).

**INFERENCE: what that means for the coordinator (a ccy podman seat).**

1. **Stopping it on v1 works.** It has a v1 record, so `ccy --stop <session>` compacts, waits
   for `/exit`, and the conversation id stays in the v1 file.
   - It waits for an idle prompt for up to 300 s.
   - After that bound it **kills** the session (`docs/ccy.md:201-208`). So "never mid-turn" is
     only true if the session is idle first.
2. **Restarting it on v2 with its context.** Two possible paths, neither designed or tested
   for this move:
   - **(i)** `ccy-sessions declare --name <same session name> --replace <checkout> -- --teams <seat>@<v2 team> --token <t> --ssh-key <k>|--no-ssh [--compose skip]`,
     then `ccy-sessions restore --dry-run`, then `ccy-sessions restore`.
     - StartPlan reads the v1 `conversation.json` the v1 run left (nothing removes it on exit,
       as far as this read found) and appends `--resume <id>`.
     - **Caution:** restore starts every record that is not live, not only this one.
       `--dry-run` shows which.
   - **(ii)** `ccy-teams record-seats <session> <v2 list>`, then `ccy-teams restart <v1 team> --yes`.
     - This relaunches in place with the record's new `--teams` and resumes.
     - It is off-label: record-seats is documented for ccy's own use, and the v1 seat stays
       held through an exit 75.
   - Either path is what Task 4.5(d) says must be "proven on a scratch session before the
     coordinator moves" (`PLAN.md:437-439`). It has not been run.
3. **After the move, the host cannot follow the v2 conversation.** That v1 file stays frozen at
   the id the move resumed. So, after the move:
   - A reboot restore resumes that **cutover-time** id. It does not follow later /clear or
     compactions.
   - `ccy --stop`, `ccy-teams shutdown` and wind-down judge compaction against that stale
     transcript, or fail with "its transcript could not be found" when there is none
     (`src-go/internal/sessclose/sessclose.go:470-486`).
   - The shutdown tests for v2 host seats use a fake closer, so this path has no test
     (`teams/shutdown/v2host_test.go:164`).
   - This is a **defect class to fix (Defence Before Fix) before the cut-over relies on close
     or restore of v2 sessions**: host readers that ignore the v2 conversation record.
4. **Can another agent or a unit do it with no human?** Only an agent **on the host**: the host
   seat with the user's tmux, podman and registry. A container seat cannot. The membership
   mount is read-only (`LAUNCH-ccy.md:37-41`), and `ccy`, `ccy-sessions` and `ccy-teams` are
   host binaries. No unit does it on demand.

**UNKNOWN:**

- whether `claude --resume <id>` keeps writing to the same transcript or forks a new id (this
  decides whether the frozen id is stale or only old);
- whether `ccy_restore_sessions` is on for this laptop;
- whether the coordinator's checkout has a pool seat yet.

## Q3. Issue #299

- **What it is.** "Element profile of a team moved to Lightning and then dropped is kept and
  never reported" (OPEN). The profile holds the human's Matrix session token.
- **Code fix, FACT.** ba6b7a434, merged at 1e93be889, is an ancestor of this tree.
  - `element.py` writes a marker `agent-bus-team` into each profile it makes
    (`helpers/agent_bus/element.py:61`, `:377-394`).
  - The play names a kept profile on a `KEPT<TAB><path>` line and never deletes it.
  - The issue was left open "until the queued meta-deploy run of play-agent-bus-element.yml
    has written the markers on the host" (issue comment, 2026-11-16).
- **Deploy status, UNKNOWN.** ead4b448a queued it, and 49a7ee685 (2026-11-17) says "both
  passes ran on the host", which suggests it ran.
- **Host check (read-only):** `ls -l ~/.var/app/im.riot.Riot/config/Element-*/agent-bus-team`.
  A marker in every Element profile the play manages means the fix is deployed and #299 can be
  closed.
- 6.5 lists it as a precondition (`PLAN.md:489-490`).

## Q4. Which teams exist

**What the repo says without host_vars:**

- Only the shapes: `agent_bus_teams`, `agent_bus_lxc_members`, `agent_bus_element_teams`
  (`environment/localhost/host_vars/localhost.yml.dist:110-236`) and v2's `teamchat_teams`,
  `teamchat_lxc_members` and `teamchat_approve_teams` (`play-teamchat.yml:43-68`;
  `docs/agent-bus-lxc-members.md:382-439`).
- The anonymised v1 wind-down triage records that this host has 8 `agent_bus_lxc_members`
  entries, **none with `service: true`**, and that a v1 team spans several checkouts
  (`00257-.../subagent-reports/winddown-v1-triage.md`, section 1).
- The coordinator's host read says there are three v1 teams (A, B, C). The repo cannot confirm
  this.

**Transport:**

- v1 is plain HTTP to the bus address on `agentbus0` (`declare.py:1712-1718` builds the url
  `http://...`; `agent-bus-install:120-122`).
- v2 is TLS with the team's CA, `https`, with plain HTTP on loopback only
  (`PLAN.md:56-58`; `docs/agent-bus-cross-network.md:179-191`).

**Read-only host commands (as the login user, from the checkout root unless noted):**

1. `ccy-teams --list`: both systems' teams, members and sessions. No terminal is needed.
   Members show "need sudo" without the read-only rule (`usage.txt:76-82`).
2. `python3 -m helpers.agent_bus.declare declared --file environment/localhost/host_vars/localhost.yml`:
   one line per v1 entry, `TEAM<TAB>team<TAB>port<TAB>state<TAB>server_name<TAB>url<TAB>description`.
   The url column is the transport. Add `declare lxc-members --file <same>` for the
   `LXCMEMBER` lines. The output goes into no tracked file.
3. In each moving checkout: `ccy-teams system <team>...` prints `<given> <id> agent-bus|teamchat`.
   Exit 64 means a name on both systems (`usage.txt:260-266`).
4. `ls -l /var/lib/teamchat-status/teams/` and `cat /var/lib/teamchat-status/teams/<id>/status.json`
   (readable without sudo, `teamchat/paths/paths.go:106`, `:328-338`). Also `teamchat-admin status --help`.
5. `systemctl list-units --all --no-pager 'agent-bus-hs@*' 'agent-bus-backup@*' 'teamchat*' | cat`
6. `ccy-sessions --list`: every session, its directory and its network.
7. `sudo -n -k true && echo NOPASSWD-ok`: whether meta-deploy can run unattended.
8. `./CLAUDE/Plan/meta-deploy.bash --list`: what a run would do (it changes nothing).
9. `sudo -n /usr/local/sbin/agent-bus-install backups --team <team>` and
   `sudo -n du -sh /var/lib/agent-bus/<team> /var/lib/agent-bus-install/<team>`: what v1 history
   and backups each team holds.

## Q5. Does 6.5 delete v1 rooms, history or data?

**Where v1 history lives (FACT):**

- `/var/lib/agent-bus/<team>/` contains `db/` (Tuwunel's RocksDB, every room event), `backups/`,
  `secrets/`, `team.json`, `registry.json` and `room_id`.
- `/var/lib/agent-bus-install/<team>/backups/agent-bus-state-<n>.tar` holds the state tars.
- References: `agent-bus-install:88-91`, `:805-807`, `:908-910`;
  `RESEARCH-v1-team-state.md` section 1.1.
- Partial copies:
  - each member's pingbus state;
  - each human's Element cache in `~/.var/app/im.riot.Riot/config/Element-<team>/`.

**What removes what:**

| Action                                      | What it removes                                                                                         | History                                                                                                                                                                                                                    |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 6.4 step 6, `state: absent` without `purge` | Stops and disables the homeserver and its timers; removes the firewall rules, the drop-in and the stamp | Data kept. Re-declaring `present` brings it back (`agent-bus-install:858-882`)                                                                                                                                             |
| `purge: true`                               | Also `rm -rf /var/lib/agent-bus/<team>` and `/var/lib/agent-bus-install/<team>`                         | **Destroyed, backups included**, because both backup homes are inside those directories (`agent-bus-install:883-891`)                                                                                                      |
| 6.5, which is D 7.2 steps 4-7               | Includes "v1's state roots (once the purges are done)"                                                  | **Destroyed.** 6.5 takes only steps 4-7 and so skips D 7.2 step 2's rule that "a purge is offered only for a team whose export verifies against its digest.json" (`DESIGN-v2-D-migration.md:1244-1253`; `PLAN.md:484-488`) |

**Is there an export tool? No.**

- v2's history export (M3, `/var/lib/teamchat-admin/<team>/migration/v1-export/`, as
  `events-<room>.jsonl`, `history.md` and `digest.json`) is part of Task 6.1, which is not built
  (`DESIGN-v2-D-migration.md:949-959`).
- Plan 00178's `agent-bus export` is "Not Started" (`CLAUDE/Plan/00178-agent-team-history-export/PLAN.md:3`).
- `teamchat/carry` is v2-to-v2.
- `pingbus history` reads one member's view, at most 500 items, from the main or questions room
  (`helpers/pingbus/cli.py:3347-3353`; `helpers/pingbus/syncer.py:75-76`).

**A safe export would be, in order:**

1. Before any `absent`: `agent-bus-install backup-now --team <team>` for each team.
   - This is a consistent Tuwunel backup plus a state tar, restorable by
     `agent-bus-install restore`. It needs v1's software to read it.
2. After `absent` without purge (the homeserver stopped): a plan-local script run through
   meta-deploy that copies each team's two directories to a root-only archive **outside** the
   v1 roots, records the sha256s, and fails if they do not match.
   - The archive must be outside the v1 roots because 6.5 deletes them.
3. A readable export, before 6.5 runs. This means building Plan 00178 / the D 5 M3 export:
   page every room with the team's admin token (`/messages`, `dir=f`), and write
   `events.jsonl`, `history.md` and `digest.json`. That is a build task, not an unattended step.
4. 6.5 is built so that it refuses to remove a state root unless a verified copy or export
   exists, as D 7.2 step 2 intends.

## Q6. Does the cut-over depend on the v1 wind-down fix? Is the proposed order right?

**The wind-down fix is not a hard dependency, INFERENCE.**

- The plan's steps never call wind-down (`PLAN.md:468-483`).
- Its defects matter only if the runbook uses wind-down to quiet sessions:
  - LXC members are judged ✗ when they have no service unit;
  - a line Claude never received can read as ✓ (the "typed but never taken" weakness);
  - free seats with the same number are listed with identical labels.
- References: `winddown-v1-triage.md`, ranked defects 1 to 3.
- So the runbook should not use wind-down unattended. It should ask each session over pingbus
  to commit, push and go idle, and confirm from the reply and the board.
- Shutdown works for v1 host seats, because their transcripts are recorded
  (`winddown-v1-triage.md` section 2).

**Checked against `PLAN.md:468-483` and the code, the coordinator's order is right in shape. It
has these gaps:**

01. **Step 2 is missing:** `play-teamchat.yml --tags member` on each moving machine; TLS reach
    with the team CA; the bus in each checkout's network policy (the owner's file).
02. **The v2 id must differ from the v1 team's id,** or `ccy-teams system` exits 64 while v1
    declares it (`LAUNCH-ccy.md:56-57`).
03. **The owner is missing from the pilot.** 6.4 step 4 needs a human post from Element or
    Lightning, plus the Element play's https entry and CA for the v2 team.
04. **Closing v1 questions and threads is missing,** before any relaunch. v2 has no questions
    verbs (`PLAN.md:479-480`). Use `pingbus questions --all`, then `pingbus thread Q<n> --close`.
05. **The move must be proven on a scratch session** before the coordinator moves (Task 4.5(d)),
    including whether `ccy --stop` and restore work on a v2 session afterwards (Q2.3).
06. **Members that cannot move:**
    - host `cc` seats (Task 5.1);
    - sessions in two v1 teams (exit 64, Task 4.5);
    - hand-started v1 LXC members (no service unit; v2 needs a `teamchat_lxc_members` entry, and
      `service: true` to start unattended).
    - Plan text `PLAN.md:482-483` says LXC members cannot move "until Phase 5". That is stale:
      v2 LXC members are built (`docs/agent-bus-lxc-members.md:382-439`).
07. **A pre-absent backup of v1 data is missing** (Q5).
08. **6.5 is not runnable:**
    - It is not built.
    - #299 is not closed.
    - It needs every `agent_bus_teams` entry absent, including Team C.
    - It would delete the v1 history.
09. **The coordinator cannot relaunch itself.** Its stop and restart must be done by the host
    seat.
10. **The meta-deploy queue already holds runs that must finish first** (the teamchat B.5 fixes,
    the ccy image, the v1 play's B.12 change).
    - The last entry, the ZZZ passwordless-sudo play, ends unattended runs unless host_vars
      declares the flag.
    - Check `meta-deploy.bash --list` before adding cut-over entries.

## Per team or all-or-nothing

- **Leaving v1 is per team (FACT).**
  - Each `agent_bus_teams` entry is `present` or `absent` on its own.
  - The play removes only the absent ones and can be scoped with `agent_bus_only_teams`
    (`play-agent-bus.yml:17-18`, `:62-67`, `:298-307`).
  - So Team A can be `absent` while Teams B and C run on v1.
  - Task 6.4 is explicitly "a team at a time" (`PLAN.md:468-470`).
- **Removing v1 (6.5) is all-or-nothing (FACT).**
  - It runs "once every `agent_bus_teams` entry is absent".
  - It removes shared objects: `agent-bus*` binaries, the sudoers file, `agentbus0`, the
    `agent-bus` user, the state roots, and the v1 kit from the ccy image.
  - It then refuses any `agent_bus_*` key (`PLAN.md:484-488`; `DESIGN-v2-D-migration.md:1250-1260`).
  - It must wait for Team C, and for every member that cannot move (Q6 gap 6).

## Runbook for an unattended run

The "Who" column uses these codes:

- **(a)** the host seat or the owner on the host (meta-deploy, plays, systemd, ccy/cc
  launches, `ccy --stop`);
- **(b)** any container seat alone (repo edits, commits, pingbus and teamchat messages);
- **(c)** the owner.

**UA** = can run unattended; **NOT UA** = cannot. Steps 4 to 13 are repeated per team: Team A,
then Team B, then Team C when its coordinator clears it.

| #   | Step                                                                                                                                                                                                                                        | Who                                                             | Needs                                                                                                                 | Check                                                                                                                              | Rollback                                                                                | UA?                                                                                      |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| 0   | Preflight reads (Q4 commands 1-9)                                                                                                                                                                                                           | a (read-only)                                                   | host seat                                                                                                             | Output anonymised into the plan, not tracked raw                                                                                   | none                                                                                    | UA                                                                                       |
| 1   | Run the meta-deploy queue already pending                                                                                                                                                                                                   | a                                                               | NOPASSWD sudo; ZZZ flag declared or the ZZZ entry left out; no terminal attached (else the LXC reboot question waits) | meta-deploy summary all PASS; `teamchat-admin version`; `ccy --version` at the queued CCY                                          | fix forward                                                                             | UA only if NOPASSWD holds                                                                |
| 2   | Back up each v1 team now (`agent-bus-install backup-now --team <t>`), as a plan script entry in meta-deploy.list                                                                                                                            | b writes it, a runs it                                          | step 1                                                                                                                | `agent-bus-install backups --team <t>` shows a new id                                                                              | none needed                                                                             | UA                                                                                       |
| 3   | Prove the move on a scratch session (Task 4.5(d)): v1 → stop → declare/restore on a scratch v2 team → resumes; then `ccy --stop` and `ccy-sessions restore --check` on it                                                                   | a; the session answers on b                                     | scratch v2 team (needs 5)                                                                                             | the resumed session recalls a fact from before; the close succeeds                                                                 | stop it; drop its record                                                                | NOT UA until proven. If it fails, stop: fix Q2.3 first (b builds it, a deploys it)       |
| 4   | Declare the v2 team (new id ≠ v1 id) in `teamchat_teams` with its roster; list it in `teamchat_approve_teams`                                                                                                                               | c decides and approves; a or c edits host_vars                  | owner's approval                                                                                                      | `play-teamchat.yml --tags teams` passes; `/var/lib/teamchat-status/teams/<id>/status.json`                                         | `state: absent` and the play                                                            | NOT UA (owner gate DC18)                                                                 |
| 5   | Member program and reach: `play-teamchat.yml --tags member`; the Element play with an https `agent_bus_element_teams` entry; each checkout's network policy allows the v2 address                                                           | a; c for the policy widening                                    | 4                                                                                                                     | `ccy-network-policy where` shows the allow line; the Element play's handshake check passes                                         | remove the lines and re-run                                                             | NOT UA (policy is the owner's)                                                           |
| 6   | Issue and place one pool seat per moving ccy checkout: `sudo teamchat-admin member add ... --credential password \| teamchat seat place-issued --checkout <dir>`; LXC members through `teamchat_lxc_members` (`service: true`) and the play | a                                                               | 4-5                                                                                                                   | in each checkout, `ccy-teams system <v2 id>` prints `teamchat`                                                                     | `teamchat-admin member retire`                                                          | UA                                                                                       |
| 7   | Pilot: one session per machine round-trips with every other seat both ways; `teamchat history --team <id>` on each side                                                                                                                     | a launches; b sessions exchange                                 | 6                                                                                                                     | both sides' history shows both directions                                                                                          | stop the pilot sessions                                                                 | UA, once 3 is proven                                                                     |
| 8   | A human posts from Element or Lightning and a seat sees it                                                                                                                                                                                  | c (password from `teamchat-admin human password` at a terminal) | 5, 7                                                                                                                  | the seat's history shows the post                                                                                                  | none                                                                                    | NOT UA                                                                                   |
| 9   | Close every v1 question and thread on the team                                                                                                                                                                                              | b (coordinator); c where an answer is the owner's               | none                                                                                                                  | `pingbus questions --all` shows none open                                                                                          | none                                                                                    | UA for closing; NOT UA where the owner must answer                                       |
| 10  | Each worker, one at a time: told over pingbus to commit, push and go idle, and confirms; then stop and restart on v2 by the step 3 method                                                                                                   | b tells; a stops and relaunches                                 | 3, 6, 7                                                                                                               | `ccy-sessions --list` shows it detached or running; `teamchat history` shows it posting on v2; the board shows the v1 seat free    | `ccy --stop`, then declare/restore with the v1 `--teams` (the v1 seat is kept until 12) | UA after 3; a session over 300 s busy is killed by `--stop`, so confirm it is idle first |
| 11  | The coordinator last, same as 10                                                                                                                                                                                                            | a                                                               | 10 done                                                                                                               | same                                                                                                                               | same                                                                                    | UA after 3                                                                               |
| 12  | v1 team `state: absent` without purge (host_vars, then `play-agent-bus.yml` with `agent_bus_only_teams` set to the team, through meta-deploy)                                                                                               | a; c if host_vars edits are the owner's                         | 10-11, and no cc or LXC member left on that team                                                                      | `systemctl is-active agent-bus-hs@<t>` is inactive; `ccy-teams --list` shows the team down; `/var/lib/agent-bus/<t>` still present | `state: present` and the play; data intact                                              | UA                                                                                       |
| 13  | Copy the stopped team's two directories to a root-only archive outside the v1 roots, with sha256s (plan script)                                                                                                                             | b writes it, a runs it                                          | 12                                                                                                                    | the script compares the digests                                                                                                    | none                                                                                    | UA                                                                                       |
| 14  | After all three teams: close #299 (marker check, Q3); build 6.5 test-first with a guard requiring 13's verified copy (or the readable export); owner decides purges and the Element profiles                                                | b builds; c decides; a deploys                                  | every entry absent; cc and LXC members resolved                                                                       | the 6.5 plays' own refusals; `teamchat-admin footprint`                                                                            | before purge: re-declare v1 present. After purge: only the archive from 13              | NOT UA (not built; owner decisions)                                                      |

**Steps container seats can do alone (b):**

- writing and committing the plan scripts for 2 and 13, and the meta-deploy.list entries;
- step 9 (closing v1 questions and threads);
- the pingbus requests to commit and go idle in step 10;
- the message exchange inside the pilot sessions (7) once they run;
- building the Q2.3 fix and 6.5 (code, tests, CCY bumps, CI);
- updating PLAN.md, including the stale LXC sentence at 482-483.

Nothing that changes the host can run from a container seat.

**Cannot run unattended at all:**

- 3, until it is proven;
- 4 and 5 (owner gates);
- 8 (a human post);
- 14 (6.5 not built; owner decisions).

**Practical weekend reading:** the host seat can do 0, 1, 2 and 6 to 13 for Team A. That is
only once the owner has pre-approved:

- the Team A v2 declaration (4);
- the network-policy lines (5);
- accepting step 8 for later;
- and once step 3 has passed.

Team C waits for its coordinator in any case.
