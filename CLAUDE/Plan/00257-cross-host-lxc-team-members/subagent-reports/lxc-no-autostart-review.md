**Verdict: FIX-BEFORE-MERGE** (range `70bd36893..d7dc8881e`)

The design holds in both generations. I swept `playbooks/`, `tasks/`, `files/`, `helpers/` and `src-go/` and found no remaining enable, start, `WantedBy`, wants link or restart-on-change path for a member session unit. Apply stops a session only when it migrates away from an old link or enablement (v1 `members.py:1252`, v2 `apply.go` `placeUnit` stops only when `unlinked`). A session started on demand survives a re-apply, and tests pin that in both generations. The new tests would fail on the old code.

### Blocking
1. **The `offline-member-blocks` gate fails at d7dc8881e.** I ran `python3 -m unittest tests.helpers.agent_bus_lxc.test_offline_member_rule` and `Sweep.test_no_finding` fails with three findings:
   - `lxc-no-autostart-acceptance.bash:79` and `:82`, detector A: the script reads `agent_bus_lxc_members` and runs `lxc-attach -n` / `lxc-info -s` itself, and nothing in the plan folder calls `plan_lxc_online`.
   - `helpers/agent_bus_lxc/members.py:28`, detector B: the new docstring wording.

   The script also breaks the rule's substance. A stopped container calls `unsure`, so the run exits 2, where it should name the member offline and leave it unjudged (PlanScriptStandards R3, QA.md `offline-member-blocks`). The report says `qa-changed` passed (report lines 180–183), so either it does not trigger helper tests for these paths or it was not run on these files. Either way that needs logging as a dogfood fault. Fix: use `plan_lxc_online_named` and reword `members.py:26-28`.

### Should fix
2. **v2 migration can lose the stop.** In `apply.go` `placeUnit`, the wants link is removed before `s.Stop`. If Stop fails, or the guest's systemd cannot be reached, the seat is marked offline, not failed. The next apply then finds no link, so `unlinked=false` and it never stops the session again. The boot-started session keeps running until the container next reboots. Fix: stop first and remove the link only after the stop succeeds, or retry the stop. v1 is safe here because a failed `disable --now` raises.
3. **Stale docs.** `environment/localhost/host_vars/localhost.yml.dist:180-182` still says `service: true` is "restarted after a crash, a container restart and a host reboot". The v2 section of `docs/agent-bus-lxc-members.md` (around lines 396–410) still says "a supervised session unit" and never says a v2 seat starts only via `teamchat-admin lxc restart --member`.
4. **`lxc.start.auto = 1` is still required for `service: true`, with no reason left.** That requirement only existed so the session came back after a reboot. v1 `members.py:1220` was reworded to "which service: true requires of it", which gives no reason. v2 still checks start.auto (`apply.go:956`), and the docs still require it. Drop the check or state why it is still needed.
5. **The acceptance script can report "absent" or "clean" when it actually could not see:**
   - `checkUnit` (lines 87 and 101): if `lxc-attach` itself fails, `test -f` reports the unit "not present" (exit 1) or "no wants link" (pass). Both should be exit 2.
   - Line 195: a `find` failure inside `$(...)` under `set -e` aborts with find's exit code, not 2.
   - A v2 `service: true` member whose `find` returns no `teamchat-session-*` unit is silently zero units checked. There is no `COVERAGE n of m` line.
   - The reboot proof has no positive control. If claude's process name is not `claude` (an npm install shows as `node`), `pgrep -x claude` always finds nothing and the proof passes blind. Add `systemctl is-active <unit>` = `inactive` after boot.
6. **R7 mismatch:** the script declares `plan_mode gather` but reboots containers. The `askReboot` deviation from R5/`PLAN_ASSUME_YES` is documented (lines 115–119) and sound, but the mode declaration is wrong.

### Nits
- The "Start its session in its LXC container" line (`picker/menu.go:205`) is offered on every LXC row, including members on another host and members with `service: false`. Those then hit the "no record" or "not found" message.
- The script never calls `plan_finish`.

### Checked and clean
- **v1 argv** (`attach.go:28`): `sudo -n lxc-attach -P … --clear-env -- env -i PATH=… systemctl start -- <unit>`, with no secrets in argv.
- **Rule table:** `systemctl` added to `lxcfreezePkg` only, with a fixture case. Minimal and justified.
- **Versions:** `CCY_VERSION` 3.122.32, container label and `REQUIRED_CONTAINER_VERSION` 2.84.38 agree, with one changelog entry.
- **meta-deploy.list:** order is teamchat, then claude-yolo, then agent-bus, then acceptance, with Q15 last. The descriptions are accurate.
- **Script:** exec bit set, waits for `is-system-running` (running or degraded) with a 120 s cap before `pgrep`.
- No public-repo leaks found.

### Mechanical gates
- `test_offline_member_rule`: **FAIL** (above). `test_members`: OK. Go tests pass for `teams/lxcsession`, `teams/picker`, `lxcfreeze` and `lxc/unit`.
- **Not run:** `qa-all.bash` (sub-agents are barred from full QA runs), Go tests for `teamchat/host/lxc` (the worktree holds another agent's uncommitted edits to those files), and `plan-qa`/syntax-check (no playbook changed).

Files: `/workspace/untracked/worktrees/sealed-carry/CLAUDE/Plan/00257-cross-host-lxc-team-members/lxc-no-autostart-acceptance.bash`, `/workspace/untracked/worktrees/sealed-carry/src-go/internal/teamchat/host/lxc/apply.go`, `/workspace/untracked/worktrees/sealed-carry/helpers/agent_bus_lxc/members.py`, `/workspace/untracked/worktrees/sealed-carry/environment/localhost/host_vars/localhost.yml.dist`, `/workspace/untracked/worktrees/sealed-carry/docs/agent-bus-lxc-members.md`