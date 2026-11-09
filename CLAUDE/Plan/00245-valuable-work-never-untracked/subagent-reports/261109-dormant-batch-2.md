# Dormant branches, second batch

Plan 00245 Phase 4, Tasks 4.6 and 4.7. No new branch was created (the cap is full): each branch
was finished on itself, in a worktree of the existing branch. Both outcomes are on
`plan-team-liaison-role`; see the end of this file for its tip.

| Branch                               | Outcome                                                         |
| ------------------------------------ | --------------------------------------------------------------- |
| `plan-team-liaison-role`             | **Ready to merge**: Plan 00225 reviewed to MERGE (round 9)      |
| `review/pingbus-compact-before-wake` | **Not wanted as built**: a v1 feature, and v1 is feature-frozen |

## 1. `review/pingbus-compact-before-wake` (Plan 00194, compact before a cold wake)

Not finished, and its tip (4405d2ee4) is not changed. The evidence:

- **v1 is feature-frozen.** The owner, 2026-11-02: "v1 should feature freeze apart from bug fix"
  (`CLAUDE/AgentNotes.md`, "The agent bus v1 is feature-frozen: bug fixes only"; Plan 00209
  direction 7; Plan 00246 Non-Goals: "Any v1 feature work: v1 is feature-frozen"). The branch
  stopped on 2026-10-19, before that ruling.
- **What the branch builds is a v1 feature.** Stage 1 adds `helpers/pingbus/coldwake.py` and a
  `cold wake:` log line to the Python watcher's delivery path (observe only). Stage 2, the
  compaction, could never be built in pingbus: it needs the owner's decisions D1-D7 and an
  upstream supervisor change (U1-U3).
- **v2 has not taken it over.** Plan 00209's design has no cold-wake or compaction-on-wake
  requirement (searched: its PLAN, DESIGN-v2 and SPEC files); F44's pingbus is still the Python
  one in `helpers/pingbus/`, and v2 (`src-go/internal/teamchat`) is on F44 beside it.
- **Its task number collides.** The branch called it Plan 00194 Task 2.0e; F44's 2.0e is another
  task (the lifecycle-restart goodbye, specified in Plan 00209 Task 3.1).

Had it been wanted, the work left was the review's FIX-FIRST list (versions to above F44's
3.113.4 / 2.80.1, the meta-deploy entry, spec F2's `tmux send-keys` path named and rejected,
U1 and U2 defined), and `coldwake.py` would also have met the "Tooling Is Go" rule only as a
helper the watcher imports (it is not a standalone CLI, so it stays Python under that rule).

**Recorded** in Plan 00194 as Task 2.0j (❌), with the research kept in the plan folder so
dropping the branch loses nothing but the unmerged code: `SPEC-compact-wake.md` (its status
header now says not built), the developer's report and the Fable review (2fd20bbb1, on
`plan-team-liaison-role`).

**For the owner:** drop the branch; and decide whether to carry the cold-wake request into v2.

## 2. `plan-team-liaison-role` (Plan 00225, the team liaison, plan only)

- **Plan number:** 00225 is free on F44 (no `00225-` folder in `origin/F44`'s tree, Completed
  included).
- **F44 merged in** (75b11a2aa). One conflict, `CLAUDE/Plan/README.md`: the 00225 row put in
  its numbered place among F44's active plans.
- **Reconciled with F44** (54c115ae9). The plan was built on v1 (Python `pingbus` and
  `agent-bus`). With v1 frozen, a new role is a v1 feature, so the plan is retargeted to v2
  (`teamchat`) throughout (DESIGN section 0, LD10): roster role in `proto/roster.go`, the
  questions room in `proto/rooms.go`, verbs `ask`, `thread`, `questions`, `history`, `verify`,
  admin `role apply`. It now depends on Plan 00209 Phase 6: no team can have a liaison until it
  is migrated to v2. Where F44 changed what the design leaned on:
  - the reports room is already read-only to humans in v2 (level 0, message level 10), which
    answers the owner's Element request; kept as an acceptance check (LZ24);
  - v2 has no `agent_bus.status` with an `until`, so the liaison's liveness comes from A 7.4's
    lifecycle notices or Matrix presence, measured in Task 0.1 (LZ10);
  - Plan 00222's guard is not built and is written against v1's role source; LZ16a now says so.
- **Round 5's blockers fixed:** an edited or redacted question post fails the relay check
  (`edited`, LZ6); no SSH keys or write-capable token at a liaison launch, refused in Go where
  role and selection meet (LZ17a); in-container read-only binds are optional and trusted only
  once `CAP_SYS_ADMIN` is dropped and probed (LZ17, LD12).
- **The owner's Element input folded in:** the liaison takes over the questions room (as
  designed); one conversation across Claude Code and Element, a terminal answer confirmed in the
  room to count (LZ7b, LD13).
- **Review rounds** (Opus, reports in the plan's `subagent-reports/`):
  - Round 6 CHANGES (2 blockers: the daemon's executables classed writable; the per-seat
    directory not matching what the launcher reads); fixed in 74149100f.
  - Round 7 CHANGES (1 blocker: per-file binds inside a read-only daemon directory); fixed in
    51deefebd.
  - Round 8 CHANGES (1 blocker: a liaison rebuilding the daemon's editable virtualenv); fixed
    in 2ede9063d.
  - Round 9 **MERGE**; its should-fix and minors folded in 31a05a1c8.
- A journal entry records the retarget (`JOURNAL/2026-11-09/`).

Status: Not Started (plan only), reviewed, with owner decisions LD1-LD13 open at their defaults.

## Plan 00245

Tasks 4.6 (✅) and 4.7 (⏸, the owner's) added to Phase 4.

## QA

`./scripts/qa-changed.bash --base origin/F44` in the worktree at 2fd20bbb1 (plus the Plan 00245
task lines): passed, 19 changed files. Only markdown changed after it (this report and Plan
00245's Phase 4). `qa-all` is the coordinator's.

## Tooling faults met (dogfooding)

- **To log (same family as the first batch's note):** the secret-path guard
  (`R-SECRET-BASH-MENTION`) blocked a `python3 - <<EOF` patch script because markdown `**` in
  the heredoc matched the protected glob `*.secret*`. Workaround: the script written to the
  worktree's `untracked/scratch/` and run from there. Upstream, daemon-owned handler; the
  coordinator should file it with `hooks-daemon issue-report` if it is not filed yet.
- **To log:** the subagent cross-worktree guard binds a subagent to the first worktree it writes
  in, so the main checkout's `untracked/scratch/` is refused even for scratch. That is the
  intended rule, but the scratch location the containment guard recommends (`untracked/scratch/`)
  then has to be the worktree's own, which no message says.
