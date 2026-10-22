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

## Left

1. Round 1 findings 1-12 (in that file). In short: check-rooms must decide offline from the
   member's `agent_bus.status` liveness (listening, `until` in the future) not from a pending
   invite, and treat ban/knock/any other state as a PROBLEM; winddown `byRecord` and shutdown
   `lxc.go` must send record `stopped` items through the live container check; 00211 stage 2
   "not driven from here" wording and judging every coordinator's join; 00211 SPEC.md,
   ACCEPTANCE-owner.md, PLAN.md Tasks 2.5/2.6 rewritten; 00213 and 00203 R9 "NOT JUDGED"
   verdict text when nothing was judged; minors 7-12; a follow-up task in 00211 PLAN.md for
   roles deciding an entry with host+seat+repo by its fields.
2. Conformance round 2, then Opus review round 2+ until MERGE; rerun qa-changed --base F44.
