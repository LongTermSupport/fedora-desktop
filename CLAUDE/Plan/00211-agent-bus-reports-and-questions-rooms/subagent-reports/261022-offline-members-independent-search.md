# Independent search: a declared but offline team member treated as a failure or a precondition
The dispatch did not contain the rule or its pattern. Not opened: detector config, src-go/qa/, .semgrep/. Detector not run. Paths are relative to the worktree root. T = text search, R = reading.

## Forms expected before searching
- lxc-info / lxc-ls state checks (!= RUNNING, --running); lxc-attach into every declared container.
- Words "start it", "start the container", "is down", "not running", "is stopped" in error text, followed by exit 1 / return 1 / raise / die.
- Loops over declared members (agent_bus_lxc_members, rosters, coordinators) whose body fails on one offline member.
- podman ps / tmux has-session on ccy seats; wait loops with a deadline.
- Python helpers raising on a stopped container; Go commands mapping "not reachable" to a failing exit; plan acceptance legs that refuse.
- SSH members: no code exists (00205 is plan prose only). Searched ssh.?member, kindSSH, agent_bus_ssh: nothing in code.

## Instances (INSTANCE = a declared offline member blocks, fails, refuses, exits non-zero, or tells the owner to start it before carrying on)

### Product code

1. src-go/internal/teams/winddown/winddown.go:355 - notReached("its container "+it.Container+" is not running; start it, then run wind-down again", row). Also :58 and :250 (whyLxcDeferred: "start the container, run ccy-teams apply, then wind-down again"). Exit: :452-453 `if failedN > 0 || len(p.unreached) > 0 { return term.ExitFailure }`; usage :130 "1 ... a stopped container, another machine". INSTANCE, confident. Names the member but exits 1 and asks the owner to start it. Found by T and R.
2. src-go/internal/teams/shutdown/shutdown.go:213-214 and :332-333 - every kindLxc row goes to unreached; `if failedN > 0 || len(p.unreached) > 0 { return term.ExitFailure }`; doc :101. INSTANCE, moderate. It never tests container state: every LXC member, running or stopped, makes shutdown exit 1, so a stopped one is covered. Message whyLxc (:54) says "out of reach of this host tmux", never offline. Found by R only.
3. src-go/internal/teams/roles/roles.go:312, :367-371 - advice := "Start container " + m.Container ... "the entry of %s@%s blocks the change of %s: its checkout is read ... and that read failed". A role change reads every declared present LXC entry checkout; a stopped container makes that fail and the change is refused. INSTANCE, confident. T ("Start container") and R (loop :305-313).
4. helpers/agent_bus_lxc/derive.py:201-207 (running_root) - die("container ... keeps its root filesystem in LXC store ... mounted only inside the running container, and it is not running: start it, then try again"). Only for unmounted-store containers (UNMOUNTED_STORES). Reached via declare lxc-derive / lxc-identity / lxc-checkouts (item 3; src-go/internal/teams/lxcadd/adder.go:458, checkout.go:82). INSTANCE, unsure: directory-store containers are read from the host while stopped.
5. src-go/internal/teams/restart/jobs.go:265 - j.done(resultFailed, "its container is not running, so its supervisor cannot restart it; end it in ccy-sessions, then run restart again to start it"). Exit 1 at restart.go:238-239. INSTANCE, unsure: a wholly stopped session is started by restart (handled); this is the half-state of a seat with a tmux session but no container.
6. helpers/agent_bus/admin.py:930-931 (check_rooms; restart text :898) - problems.append(f"... {handle} has not joined {purpose} (...): {restart}"), restart = "restart that agent session (invite: it is not running ...)". An offline coordinator has not joined, so agent-bus check-rooms reports a PROBLEM and exits non-zero. Only caller: CLAUDE/Plan/00211.../_acceptance-stage2.inc.bash:255 (a12_check). INSTANCE, unsure (read-only checker).

### Plan scripts

7. CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage1.inc.bash:60-63 (s2_check) - printf container %s is %s, not RUNNING: start it, then run meta-deploy again ...; bad=1; return bad. Wired at acceptance.bash:280 ("... and none is down"); header acceptance.bash:9-10 "a container that is down is refused, named"; pre-flight deploy.bash:17 and :121 "no declared container down: containers can stop between deploys"; fake test acceptance.bash:199 (reject_leg "S2 (fake) a stopped container") pins the refusal as intended. INSTANCE, confident. T; R showed it gates deploy stage 2.
8. CLAUDE/Plan/00213-agent-bus-performance-go-carve-out/_acceptance.inc.bash:99-107 (c3_lxc) - "container %s is %s, not RUNNING: start it, then run meta-deploy again (the play copies its kit only while it runs)", bad=1. Header acceptance.bash:10, wired :99. INSTANCE, confident. Same shape as item 7.
9. CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash:131-148 (lxc_sessions) - home="$(guest_home "${container}" "${user}")" || return 1: lxc-attach into every present declared entry; a stopped container makes it fail and the leg stop. Comment :129-130 "refused a declared container that is down"; :144 "run play-agent-bus.yml with the container running". INSTANCE, confident. Found by R (no "stopped" text; the failure is an unguarded lxc-attach).
10. CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash:228-246 (rooms_joined) - waits 180 s for each coordinator to join questions, then "... its session is not running or started before deploy 1 ...: restart that session, then run meta-deploy again" and return 1. INSTANCE, confident. An offline coordinator (ccy or LXC) fails the acceptance and asks the owner to act.
11. CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash:164-175 (find_session, used by pick_coordinator :180) - "the coordinator %s runs in no session this host can drive ... start its session ..., then run meta-deploy again", return 1. INSTANCE, confident. An offline coordinator makes the test-question leg refuse; a12_check (:255, item 6) fails on the same coordinator.
12. CLAUDE/Plan/00161-agent-team-bus-matrix/triage_probe.py:1597-1599 - not_established.append("lxc: no running container; start one and re-run"), then raise ProbeError. NOT-AN-INSTANCE (borderline): needs any running container for a host-address probe; not a declared member.

## Handled correctly

13. helpers/agent_bus_lxc/members.py:1223-1231 (_hold_back), :1254-1264 (run_present), :1312-1328 (_absent_held_back), :1345-1346 - a container not RUNNING is never entered; the member is SKIPPED (already joined) or DEFERRED, the loop continues, other members are applied. The sweep at :1424 touches RUNNING containers only. HANDLED-OK.
14. members.py:736-739 (skipped_why): "LXC container X is stopped: <handle> is already a member of team T, so it is left as it is ...". :695-700 (summary): "N LXC member(s) not reached, their containers not running: ... Nothing failed; the first run with each container up brings it in line". HANDLED-OK; names the member plainly as not running.
15. members.py:742-746 (deferred_why): "LXC container X is stopped, and this host has no record that it joined team T: start it, then run the play again (ccy-teams apply T) to join it, or choose not to declare it yet". Also :1316-1328. HANDLED-BUT-UNCLEAR-MESSAGE: nothing is blocked, but the wording tells the owner to start the container and reads as a precondition.
16. playbooks/imports/play-agent-bus.yml:73-77, 261-277, 389-408 - agent_bus_lxc_held_re collects SKIPPED|DEFERRED|LXC-SUMMARY lines and a debug task prints them; comments :270 and :390 say "a deferral, not a failure". HANDLED-OK.
17. src-go/internal/teams/lxcadd/adder.go:299-314 (sayStopped) - "Declared in T, container not running: u@c. The play skips a member already joined and defers one not yet joined; neither blocks this add. Nothing is started." HANDLED-OK, plain.
18. src-go/internal/teams/lxcadd/adder.go:278-292 (listContainers) - with no running container, failed("there is no running LXC container on this host to add. Stopped, so not offered: ... Start one, then run this again."). NOT-AN-INSTANCE (the wizard choosing a new container; no declared member). With some running it prints "Not offered, as they are not running" and carries on.
19. src-go/internal/teams/restart/restart.go:51, :223-239 - LXC members are "left out"; report exits 1 only when failedN > 0 ("A member left out is not a failure"). whyLxc = "an LXC container agent: restart it inside its container". HANDLED-OK on exit code; HANDLED-BUT-UNCLEAR-MESSAGE (never says offline).
20. src-go/internal/teams/board/snapshot.go:17-30 (stoppedAtApply = "container stopped at the last apply"), board/collect.go:509 ("(stopped)"), board/table.go:43. HANDLED-OK; offline is shown.
21. src-go/internal/teams/hot/hot.go:489-490 - "no running container is labelled ...: it is not running, or it was started outside ccy tmux, so it cannot be changed while it runs". NOT-AN-INSTANCE (explicit command on a named session, not a declared member blocking something else).
22. CLAUDE/Plan/00203-teams-hot-control-and-robustness/_acceptance-robust.inc.bash:234-262 (check_lxc_members) - iterates lxc-ls --running -1 only, so stopped members are skipped silently. HANDLED-OK on blocking; HANDLED-BUT-UNCLEAR-MESSAGE: when all member containers are stopped, found=0 prints "R9: no agent_bus_lxc_members entry declares service: true on this host, so there is no member unit to prove" (:259), which is wrong and does not name the offline members.
23. docs/agent-bus-lxc-members.md:236-242, docs/agent-bus.md:447-452 - "never fails the play and is never started or entered ... DEFERRED ... (start the container and apply again)". HANDLED-OK as design; same "start the container" wording as item 15.

## Checked, not instances

24. members.py:1124-1130 (_check_service, lxc.start.auto): called only for RUNNING containers (:1270-1271). A correctness check on a running member.
25. members.py:988-994 (_existing_state, used :1255) and :1340-1344: "LXC container X does not exist: this play joins existing containers" raises. A missing container is a wrong entry (spec L5), not offline. Unsure.
26. members.py:1043-1050, :1334-1336; helpers/agent_bus/checkout.py:705-708; src-go/internal/teams/chooser/list.go:165; hot/add.go:108: a team homeserver not running, not a member.
27. CLAUDE/Plan/00055-container-process-watchdog/acceptance.bash:410-440: starts its own burner container with lxc-start; not a declared member.
28. CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-u23.inc.bash:327-336: the acceptance creates and starts its own container.
29. 00201.../_acceptance-m7.inc.bash, 00203.../_acceptance-m1/m2/m3/m4/m6, 00194.../_acceptance-goodbye.inc.bash, 00161.../_acceptance-u20.inc.bash: drive sessions the acceptance starts itself and waits for (m7_container_up, gb_starting, u20_container_up); no declared-member offline handling.
30. 00209-agent-bus-v2-in-go scripts: no LXC state handling. 00205-ssh-team-members: prose only.
31. helpers/containerwatch, .claude/ccy/claude-supervise.py: not about declared team members.

## Text search found, reading could not have checked

- The repeated "not RUNNING: start it, then run meta-deploy again" leg in two plans (items 7, 8); "Start container" in roles.go (item 3); die(... not running: start it ...) in derive.py (item 4); the "restart that agent session" text (items 6, 10). Reading cannot sweep about 100 files for every spelling.
- The HANDLED-OK sites (items 13-17, 20) by their skipped/deferred/stopped strings, and the docs.

## Reading found, text search could not have

- Items 2, 9, 11 carry no offline wording: shutdown exit 1 comes from len(p.unreached) > 0; lxc_sessions fails through an unguarded lxc-attach in guest_home; find_session fails on a coordinator with no session.
- Item 1: the failing exit lives in report(), away from the "start it" text.
- Items 6 and 10: offline shows up as "has not joined"; the link to offline is only in the restart string.
- Item 22: a silent skip with a misleading note when all are stopped.
- Item 19: LXC left out, no state named.
- Item 5: a stopped session is started, but a half-stopped seat fails.

## Searches run

- Whole project (excluding .git, src-go/qa, .semgrep, node_modules, the hooks-daemon copy), then narrowed by type and path, for: lxc-info, lxc-ls, lxc-start, lxc-attach, STOPPED, RUNNING, has-session, AUTOSTART, lxc.start.auto, offline, not running, is down, start it, start the container, Start container, container.*(not running|stopped), member.*(offline|not running), unreach, leftOut, held back, agent_bus_lxc_members, check-rooms, owner_needs, must be running, ssh.?member.
- Read: helpers/agent_bus/, helpers/agent_bus_lxc/, src-go/internal/teams/*, src-go/internal/lxcfreeze and src-go/internal/bus (grep), files/usr/local/sbin/agent-bus-install (grep), playbooks/imports/play-agent-bus.yml, docs/agent-bus*.md, plan scripts for 00055, 00057, 00161, 00177, 00194, 00196, 00201, 00203, 00205, 00209, 00211, 00213.
- Not read in full: helpers/pingbus/* and src-go/internal/pingbus (seat-side, not driven by a declared member list). Completed and Cancelled plans: grep only.

