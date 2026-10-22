# Handoff: offline members never block (DBF, branch fix/offline-members-never-block)

Worktree: `.claude/worktrees/agent-af3a57dd7e8a643f0-eb38bd6c`, branch
`fix/offline-members-never-block` off F44 c159e6626. Not pushed, not merged.

## Done

- `2e0c8aa6e` defence red: `tests/helpers/agent_bus_lxc/test_offline_member_rule.py`, rule
  `offline-member-blocks` (QA.md section; AgentNotes.md "An offline team member never blocks
  anything", the owner's rule, current-state wording). Detectors A (member-reading bash: own
  LXC state read, or lxc-attach with no `plan_lxc_online` in its plan folder), B (team-member
  code telling the owner to start/restart a member to go on), C (teams Go flow failing on a
  non-empty list of LXC rows). Independent search: `261022-offline-members-independent-search.md`
  (11 instances; the rule covers all of them but restart/jobs.go:265, judged not an instance:
  a live tmux session whose container is gone is broken, not offline).
- `978d02f45` `CLAUDE/Plan/_members.inc.bash` (`plan_lxc_online`; own file because
  `_planlib.inc.bash` is on the bash-limits ratchet), tests in `scripts/test-planlib.bash`;
  00211 acceptance stage 1 and 2 and deploy.bash text; 00213 C3; meta-deploy.list descriptions
  (00213 entry and 00211 deploy-stage-2 entry; the RUNNING precondition is gone). `--fake` for
  both 00211 stages passes; test-planlib passes.

## Left (the rule is still red on these 8 findings)

01. `helpers/agent_bus/admin.py:898,930` check_rooms: a coordinator with membership `invite` is
    offline (its pingbus has not synced): emit it as an OFFLINE line (cli.py prints
    `OFFLINE\t...`, exit 0 when only those), keep `leave` and `-` as PROBLEMs. Tests in
    tests/helpers/agent_bus/. The 00211 a12_check already prints rc-0 output lines. **This
    change needs the ccy container bump** (helpers/agent_bus is in the image: Dockerfile LABEL
    claude-yolo-version + REQUIRED_CONTAINER_VERSION, so CCY_VERSION too, with a changelog
    entry), and then the meta-deploy.list text that names CCY 3.99.14.12 / container 2.78.15
    changes. Without it A1/A2 of 00211 stage 2 still fails on an offline coordinator.
02. `helpers/agent_bus_lxc/members.py:745` deferred_why: drop "start it, then run the play
    again"; say it is offline and joins on the first run with its container online. Also the
    summary line (:699) could say "offline". Check tests/helpers/agent_bus_lxc/test_members.py.
03. `helpers/agent_bus_lxc/derive.py:206`: reword without "start it, then try again" (it is
    offline, so its checkout in an unmounted store cannot be read from here).
04. `src-go/internal/teams/roles/roles.go:367`: an entry's checkout read fails for a stopped
    container. Use the LXC member record (lxcrecord, needs AppliedDir on Setter, wired in New
    from h.AppliedDir): an item with a handle is judged by that handle, no read; an item
    deferred with no handle never joined, so it is not the handle (skip it, named offline).
    Drop "Start container". Tests roles_test.go:181-205 pin the old text.
05. `src-go/internal/teams/winddown/winddown.go:58,250,355` + report: a stopped container, a
    deferred member, and a record status stopped are offline: put them in `p.idle` with
    "offline (container X stopped)", not `p.unreached`; tests in winddown/lxc_test.go:42-70.
06. `src-go/internal/teams/shutdown/shutdown.go:213,332`: every LXC row is unreached and the
    run exits 1. At least the record's held-back items (lxcrecord status stopped/deferred) are
    offline: not unreached. Ideally also the live container list as winddown reads it.
07. `CLAUDE/Plan/00203-teams-hot-control-and-robustness/_acceptance-robust.inc.bash:236-262`
    check_lxc_members: list containers with `lxc-ls -1` and `plan_lxc_online` (source
    `_members.inc.bash` in its acceptance.bash), name the offline ones, and fix the found=0
    note, which is wrong when every member container is stopped.
08. Then: rule green; `python3 -m unittest tests.helpers.agent_bus_lxc.test_offline_member_rule`,
    the agent_bus/agent_bus_lxc suites, `go test ./internal/teams/...` (from src-go), the 00211
    `--fake` stages, `scripts/test-planlib.bash`, `./scripts/qa-changed.bash`.
09. Visibility: board shows a ccy seat's stopped session as "(stopped)" and an LXC member as
    "container stopped at the last apply" (record-based, not live). Not changed, on purpose:
    Plan 00209's queued acceptance-s1 compares `ccy-teams --list` bytes against the pre-stage-1
    build and would REJECT any wording change. Record that as a follow-up in 00209's PLAN.md
    (a live offline column for v2, which seat 3's v2 design must follow anyway).
10. Plan records: PLAN.md of 00211 (owner's rule and the fix), 00213 and 00203 notes; a
    journal entry via `CLAUDE/Plan/mkplan.bash --journal 00211 <category> <body-file>`.
    00201 and 00161 have no instance (searcher items 29/12).
11. Review: dbf conformance-reviewer, then the Opus adversarial loop, rounds saved as
    `subagent-reports/261022-offline-members-review-roundN.md`.

No `.claude/rules/` file covers agent-bus paths, so none was pointed at the AgentNotes section.
