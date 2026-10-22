# Conformance review round 3: offline-member-blocks (spec sections 3, 4, 7, 8)

Reviewed: branch fix/offline-members-never-block, tip 4958cfba9. My own worktree was on another branch, so I read the branch through git and ran the test in a detached scratch worktree, since removed. The branch was not changed. I did not run qa-all.bash.

## Reproduction

- **Red commits (2e0c8aa6e, 1d9951f0e):** not re-run this round. Commits are immutable, and round 2 reproduced both. The first failed with 19 findings. The second failed on exactly one, `restart/jobs.go:266`.
- **Final, rule unchanged:** `git diff 9525d8a67 4958cfba9 -- tests/helpers/agent_bus_lxc` is empty. The rule file is also unchanged since 95dbd8fc1, which round 2 reviewed. `CLAUDE/QA.md` changed between 9525d8a67 and 4958cfba9 only in the `test-meta-deploy.bash` row and a new `test-ccy-image-build-lock.bash` row, both from F44. Neither touches the rule section.
- **Green at 4958cfba9:** `pytest tests/helpers/agent_bus_lxc/test_offline_member_rule.py` gives 7 passed. The fixtures fire the detectors in the same run, so the rule is active. Verified.
- **Project entry point at the tip:** not verified by me.
  - The report states `./scripts/qa-changed.bash --base F44` returned rc 0 at 8cb85c27c. It was not re-run at 4958cfba9, which also changed versions.
  - The report says plainly that qa-all.bash is not run by sub-agents and that the coordinator's full run is the entry-point green. That is honest.
  - The report also discloses that qa-changed does not rerun this rule when a scanned Go or bash file changes. Only qa-all covers that.
- **Originating symptom (stage 1 fake offline leg):** needs LXC tooling and was not run, so it is unverified.

## Round 2 findings

1. **Second Narrowing in a fix commit (3.3): resolved.**
   - The report now has a "A second Narrowing, recorded" paragraph. It names `HELPER_CALL = \bplan_lxc_online(?:_named)?\b` and what it lets through.
   - It says the wrapper calls `plan_lxc_online`, cites the fixture and the `\b` guard, and records that no commit holds the red, so history is not claimed. The narrowing is also written into `CLAUDE/QA.md` detector A.
   - Residual nit: the report's opening list still says "(`1959e982d` ... and the rule counting `plan_lxc_online_named`, red first in its fixture)". The later paragraph correctly says the red is not in history. The two disagree, so drop "red first in its fixture" from the opening line.

2. **Remaining instance, allowance not cited (3.5, section 4): resolved.**
   - Referral 1 cites the single-known-instance allowance. The count is exactly one, with what stopped the fixing and what to try next (PLAN Tasks 2.8 and 2.9).
   - `CLAUDE/QA.md` has the section "The one known instance it does not flag", naming `roles.go` `isMember` and the same allowance.
   - Referral 2 is a separate referral (Plan 00209 Task 7.4).

3. **Entry-point green: resolved as a record, still pending as a run.**
   - The record states honestly that qa-changed passed and qa-all is the coordinator's run.
   - The coordinator must quote the qa-all result before push. This is not a defect in the work.

4. **Counts: resolved.**
   - The report now reads "19 findings in 10 files at the first red, 1 more (one more file) at the second", and "the 20 rule findings are in 11 files".
   - It says the `check_rooms` and `roles` changes were over-corrections found by review, not rule findings. This matches round 2's reproduction.

5. **Search order and exclusions: mostly resolved.**
   - Exclusions are now stated one by one: completed plans, `JOURNAL/` and `subagent-reports/`, `tests/`, `testdata/` and fixtures, vendored Go, and non-member Go and Python. The next wider rule not built is named with its reason.
   - The search order is now stated honestly. The report says the order rests on the practitioner's statement alone, because the search and the rule landed in one commit and no dated dispatch record exists.
   - That is a disclosure, not evidence. The independence claim in 3.1 stays unprovable from history, and the record no longer pretends otherwise.

## Findings now, by severity

- **LOW.** The opening paragraph's "red first in its fixture" contradicts the second-Narrowing paragraph (finding 1 residual). Remove those words.
- **LOW.** The independent search's pre-rule order is not provable from the repository (finding 5). It is disclosed, so I accept it. Future runs should commit the search report before the rule.
- **INFO.** The entry-point green at the tip rests on the coordinator's qa-all run, which is not yet quoted.

## Checked and conforming

- Rule is a failing test, with no warn mode, baseline, skip or suppression.
- Red commits stand alone.
- Message carries the stable id and resolves to the shipped `CLAUDE/QA.md` section.
- Search reconciled in the rule's favour, with the two techniques' unique finds recorded.
- Instance count reported with sweep scope.
- Examined versus by-pattern split recorded: all examined individually, none by pattern.
- Referrals carry count fixed, count remaining, blocker and next step.
- Toolchain gaps (no resolver or listing command, qa-changed routing) reported, not worked around.
- Original defect fix is recorded as not last, a recorded departure.
- Later commits (8cb85c27c, 4958cfba9) did not change the rule.

VERDICT: CONFORMS