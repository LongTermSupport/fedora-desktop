# Handoff 2: offline members never block (DBF, branch fix/offline-members-never-block)

Worktree `.claude/worktrees/agent-af3a57dd7e8a643f0-eb38bd6c`. Not pushed, not merged into F44.
F44 merged in (no change since c159e6626). The first handoff is `261022-offline-members-handoff.md`.

## Done since handoff 1

- `a51a29e17` check-rooms: `RoomCheck(problems, offline)`, OFFLINE lines, exit 0; CCY 3.99.14.13,
  container 2.78.16, changelog, meta-deploy.list text, 00211 stage-2 fake leg `t1=offline`.
- `a14af5445` members.py deferred/summary and derive.py wording say offline.
- `f84831b0e` 00203 R9 via `plan_lxc_online`.
- `1e00176bd` merge of the Go sub-agent (roles, winddown, shutdown; its report
  `261022-offline-members-go-report.md`).
- `b37ea0269` plan notes (00211 Task 2.8, 00213, 00203 Task 2.4, 00209 Task 7.4), `2f3dd9e3a` journal.
- Conformance round 1 (`261022-offline-members-conformance-round1.md`, NOT YET CONFORMING):
  answered by `1d9951f0e` (rule widened, red on restart/jobs.go; A's exemption needs a bash code
  line), `96a8fdb6c` (restart fix, SPEC-team-add-and-restart B4a), `8ecda7660` (DBF report
  `261022-offline-members-dbf-report.md` rewritten for the findings).
- `./scripts/qa-changed.bash --base F44` passed (rc 0) before `1d9951f0e`.
- Opus review round 1: `261022-offline-members-review-round1.md`, VERDICT FIX-FIRST.

## Round 1 findings answered so far

- 1, 2, 7, 4 (wording; A1/A2 judges joins), 9 (stage-2 wording): `521e97108`. check-rooms reads
  each coordinator's `agent_bus.status` in the team room (`protocol.read_status`, `now_ms`
  parameter): listening and not joined is a PROBLEM, no live status is OFFLINE, ban/other is a
  PROBLEM. This needed CCY 3.99.14.14 / container 2.78.17 (changelog entry, meta-deploy.list,
  00211 PLAN.md updated). The DBF report still says 3.99.14.13: fix it.
- 3: `0be44b47f` (winddown `byRecord`, shutdown `lxc.go`: record `stopped` goes through the live
  container list; tests first). `go test ./internal/teams/...` and golangci-lint green.

## Left (round 1 findings in `261022-offline-members-review-round1.md`)

1. Finding 5: rewrite 00211 SPEC.md (lines ~268-269, 340-355, 370-378, 742-744),
   ACCEPTANCE-owner.md:6-7, PLAN.md Tasks 2.5/2.6 to the offline behaviour; record the §11.2
   scope change (an offline coordinator leaves the test question and A5 NOT JUDGED).
2. Finding 6: 00213 `acceptance.bash:110` / `c3_lxc` and 00203 R9 print a NOT JUDGED verdict
   when no member was judged, not "installed everywhere" / a plain pass.
3. Finding 8: roles.go ~394-397 says "its checkout could not be read (...)" unless the container
   list says stopped. Finding 9b: restart/jobs.go give back the cleanup hint ("ccy-sessions ends
   the leftover tmux session") without "then run restart again to start it".
4. Finding 10: R9 lists only declared member containers (MEMBERS_JSON / host_vars), not `lxc-ls`.
5. Nits 11-12 (`plan_lxc_online` offline names on a fixed prefix the callers filter; ABORTING
   is a failure, FROZEN etc. named by raw state). Update scripts/test-planlib.bash.
6. Follow-up task in 00211 PLAN.md: roles decides an entry giving host+seat+repo by its fields
   (needs `Repo` on `declare.LxcMember`).
7. Update the DBF report, run `./scripts/qa-changed.bash --base F44`, conformance round 2, then
   Opus review round 2+ until MERGE (`261022-offline-members-review-roundN.md`).
