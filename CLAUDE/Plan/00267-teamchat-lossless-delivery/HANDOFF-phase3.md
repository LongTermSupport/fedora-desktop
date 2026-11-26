# Handoff: Plan 00267 Phase 3, Task 3.2 (awaiting the coordinator's ruling)

Both halves of Task 3.2 are built on `v2-smoke`: the replay half (late, expired) and the forge
hold (PLAN.md Task 3.2; report `subagent-reports/261126-phase3-part3.md`). The task stays
open on one question for the coordinator: must Task 3.2a, which makes the hold durable, land
before 3.2 is ticked?

- If it need not: tick Task 3.2 in PLAN.md, drop its "stays open" sentence and delete this file.
- If it must: build Task 3.2a as PLAN.md describes it, then do the same.

Why the hold is not durable: `session.start` resumes from the stored `next_batch` whenever
one is stored, so main's floor below a held ping (`heldFloor`) is read only by a resume with
no position. The hold is on the Team (`Team.forgeHeld`), so it survives a session restart. A
link restart loses a held ping with no bounce.

## Tooling faults met (reported to the coordinator, not yet filed)

- The `lint_on_edit` hook lints analysistest fixtures under `qa/rules/testdata/src/`, which no
  gate lints: every fixture function is reported `unused`. The existing `storeadd` fixture would
  be reported the same way.
- The `lint_on_edit` hook reports a failure when another agent's golangci-lint is running
  ("parallel golangci-lint is running"), which is a busy lock, not a lint finding.
