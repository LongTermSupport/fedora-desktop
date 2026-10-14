# qa-reviewer findings on Plan 00192: fixes

Review: [261014-qa-reviewer-plan.md](261014-qa-reviewer-plan.md). Work is on `go-port/qa-all`,
the one integration branch: F44, `fix/meta-deploy-list-data` and `docs/p194-owner-decisions`
are merged into it (`--no-ff`). The only conflict was one record of
`scripts/bash-limits.allowlist`; the markers were removed and the file rewritten with
`scripts/qa-bash-limits.bash --update`.

| Finding               | Result                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 PLAN.md drift       | Tasks 3.1, 3.2, 3.3, 4.1 and 4.2 restated to match the tree, each with the right mark (3.1 freeze merged and pushed `f0284848`, host acceptance pending with the sudo fix; 3.2 built, reviewed, fixed, unmerged until the owner's root test; 3.3 qa-changed merged and pushed `de85afbc`, qa-all merging through this branch). Task 1.3 paths are `src-go/qa/cmd/qa-bash-limits` and `src-go/qa/bashlimits`.                                                                                   |
| 2 qa-all numbers      | Counted from the tree: 142 golden scenarios, 32 entries in `deviations.json`, bugs B1-B6, B12, B13, B20-B23, B26.                                                                                                                                                                                                                                                                                                                                                                              |
| 7 Go defect classes   | B25 (capture dropped on `ErrWaitDelay`): new rule `project_waitdelay` (`src-go/qa/rules/waitdelay.go`, fixture `testdata/src/fix/internal/badwait` red and green, `TestWaitDelay`, catalogue entry). On the real tree it flagged `execx.OS.Run` (it set the delay, never named the error), which now has its own branch. The `sudo -n -v` probe: recorded in `CLAUDE/GoProjectRules.md` ("Defect classes that cannot be a rule") with what holds it instead, and the known `sudo -n true` gap. |
| 8 Success criterion 2 | Recorded in PLAN.md: freeze and qa-all bash suites replaced by the golden replay and Go tests, and why that meets the criterion. Criterion 3 now points at the rule and the recorded reason.                                                                                                                                                                                                                                                                                                   |
| 9 qa-gates rule paths | `.claude/rules/qa-gates.md` gains `scripts/qa-gates.tsv`, `qa-routing.tsv`, `qa-trees.tsv` and `src-go/qa/{all,gates,changed}/**`; the `CLAUDE.md` table row follows.                                                                                                                                                                                                                                                                                                                          |
| Nit: rule count       | PLAN.md Task 2.2 and `src-go/CLAUDE.md` now name all six rules.                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Nit: 90 statements    | Task 1.2 and `README.md` state it.                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| 10 conflicts          | The F44 conflict is resolved here. `go-port/agent-bus-install` still conflicts with F44 in `meta-deploy.bash`, `QA.md` and the allowlist and merges separately after the owner's test.                                                                                                                                                                                                                                                                                                         |

Also done, as instructed: `test-meta-deploy.bash` has its own row (`meta-deploy-queue`) in
`scripts/qa-gates.tsv` and a row in QA.md; the nesting inside `test-planlib.bash` is removed.
The suite prints `passed: N` for the count reader. The golden replay is a replay of the legacy
bash, which has no such gate, so `capture.py` now splices gates the table gained after its
revision into the legacy text (`SPLICED`), and the goldens and pinned deviations were recaptured
(`go test ./all -update-deviations` for the pins).

Not fixed here, by design: findings 3, 4, 5 and 6 are on the agent-bus-install branch; 11 needs
the coordinator's full qa-all run; the owner-name nit belongs in `mkplan.bash`.

Checks run: `scripts/qa-go.bash` (GO-OK, project rules clean, six rules), `go test ./...` in
`src-go/qa`, `scripts/qa-bash-limits.bash`, `scripts/test-qa-changed.bash`,
`scripts/test-meta-deploy.bash`, `scripts/test-planlib.bash`, `scripts/qa-docs.bash`, shellcheck
on the changed bash. `qa-all.bash` and Ansible were not run.
