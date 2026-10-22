# Rework handoff (owner's O1-O7 answers)

Branch `feat/opencode`, worktree `.claude/worktrees/agent-a21adf48f5b634ba2-cad42346`.

## Done

- Desktop play removed; opencode 1.19.3 in `files/var/local/claude-yolo/Dockerfile`
  (container 2.79.0), managed config, `tui.json` unbinding ctrl+z.
- `ccy --opencode` / `-oc` in `files/var/local/claude-yolo/lib/agent-choice.bash` (CCY 3.100.0);
  launcher calls it at the parse's catch-all, after the parse, for the banner and the run.
- Tests: `scripts/test-ccy-agent-choice.bash` (gate `ccy-agent-choice`, row in
  `src-go/qa/all/addedgates_test.go`); difftest scenarios `launch-opencode*`, mutation, and
  `TestOpencodeLaunchHandsKeyOn`. Three suites' run-line grep moved to `ccy_agent_container run`.
- Plan, design section 6, docs, changelog, meta-deploy entry updated.

## Left

- Merge F44 into the branch (F44 moved to CCY 3.99.14.12 / container 2.78.15: keep 3.100.0 and
  2.79.0, changelog "Follows 3.99.14.12", meta-deploy entry text).
- Review rounds (`subagent-reports/261022-rework-review-roundN.md`) until MERGE, then qa-reviewer.
