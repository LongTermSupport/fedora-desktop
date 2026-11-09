# DBF conformance review: project_child_exit and project_row_kind (spec 1.1.0)

Reviewer used a detached checkout in a sibling worktree; the branch under review was not changed.

## Reproduction (section 7)

- Red, 1824f6ed5: `./scripts/qa-changed.bash --base 1824f6ed5~1` (plain qa-changed diffs against HEAD and says "no changed files" on a clean commit) exits 1, `go: detectors analyzers FAILED`: 14 project_child_exit (reboot.go:208, choose.go:224, chooser.go:255, wizard.go:174/749/755, adder.go:215/648/654/671, ops.go:231/250/253/256) and 1 project_row_kind (collect.go:516). Matches the commit message. The identifier is printed.
- Green, a86557067, same command and base: exit 0, "qa-changed passed: 56 changed file(s)", go-qa both modules passed. The green output does not enumerate rules; "rule active" rests on both analyzers being registered in src-go/qa/rules/table.go and the same driver firing in the red run (3.3 Part C). I did not run the analysistest fixture tests separately.
- Originating symptom (ccy --teams, unknown team, exit 78) not reproduced end to end (needs agent-bus and a host); only the presence of unknown.go and its tests in a86557067 was checked. Unverified beyond that.
- Draft report: only untracked/agent-reports/261023-ccy-teams-unknown-team-and-kind.md exists. It is stale (tip 37cbe9ae7, "PARTIAL", fix not yet done). Report-based checks are unverified against a final report.

## Findings

### High

1. Search and rule disagree and it is not reconciled or escalated (3.1 saturation, 3.4 "the search is the authority", section 4). The independent search found child-code pass-throughs the rule does not catch: vmtest member.go:272 (ssh status; 124 and 255 undocumented), verify.go:39-40, open/launch.go:146 (128 + signal of a handler), and Class B readers (roles.parseHandle, seats.nextSeat, lxcadd HasSuffix). GoProjectRules.md records only "vmtest helperExitCode"; member.go, verify.go and open/launch.go:146 are not named. Recording a gap as "Not caught" is not the Owner decision that section 4 requires when the reason is that the wider check is harder to build. Resolve: widen project_child_exit (an int helper that returns an error's Code), or write the per-site sentence that the hazard cannot arise, confirmed by search; refer the undecided sites (member.go 124/255, launch.go:146) to the Owner with counts.
2. No final report; the template is not filled (3.1 record, 3.4, section 4). Missing: both techniques with what each found that the other could not have checked, reconciliation, sweep scope, examined-versus-pattern record, and whether a Runner-executed check pins the reported behaviour. The search record lives in the ignored untracked/ tree, so it is neither versioned nor discoverable (8.7). Resolve: commit the search record beside the rule docs and fill the template to match the commits.

### Medium

3. Search order is not demonstrable (3.1 step 2). Search file mtime 11:47, red commit 11:53; no earlier rule commit exists, and the searcher says it did not open the detector. Plausible, but the commits cannot show it. State it in the report.
4. Next wider rules (3.1 step 4, section 4). Class A: "every runtime term.ExitCode conversion", rejected as most are the tool's own codes; partly a "harder to build" reason, not routed to the Owner. Class B: rejected by one-line sentences (parseHandle checks grammar, nextSeat numbers podman seats, lxcadd matches the member it added) with no recorded confirming search. seats.nextSeat matches only .podman, so host seats are not counted; no sentence addresses whether that is a hazard.
5. FromChild keeps 64/75/77/78/130, so the shell still receives 78; the fix is the term.Explain last line and usage text. Say so explicitly as the chosen behaviour (3.4 "write down what the default means"). The reboot change to exit 1 is a recorded departure (SPEC-ccy-sessions 1.3): acceptable.

### Low

6. The hand fix of 13 Kind == KindCcy sites to Kind.IsSession is guarded by no rule, and the record does not say whether each was examined or changed by pattern (3.4). No Kind == KindCcy remains at the tip.
7. Sweep scope (src-go and nested qa, tests and fixtures excluded) is in the rule docs but not stated as the recorded sweep decision (3.4).

## Checked and satisfied

- Permanent and blocking: runs in go-qa analyzers via qa-changed; fails exit 1; no nolint, baseline or suppression in the diff.
- Red is a commit of its own (rules and fixtures only, no instance fixed); fix follows (37cbe9ae7); original defect fixed last (a86557067, with tests).
- Fixtures versioned with the rule (badexit, badrowkind, good/bad pairs); docs in CLAUDE/GoProjectRules.md added with the fix commit, stating what, why, fix, and not-caught; identifier resolves via the driver help doc string and the catalogue (8.3); docs show the correct construction (term.FromChild, board.KindOf) (8.4).
- All 15 sweep instances are fixed rather than suppressed; ops.playExit became playStatus (an int that never was the exit), a real fix.
- No toolchain gap was claimed; the vmtest miss is a rule gap (finding 1).

## Verdict

Red and green reproduce through the entry point. The remediation does not yet conform: findings 1 and 2 must be resolved first.
