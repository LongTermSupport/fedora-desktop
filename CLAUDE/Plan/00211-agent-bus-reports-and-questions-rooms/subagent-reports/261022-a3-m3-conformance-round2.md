**Verdict: NOT YET CONFORMING.** The 00211 rule (`fixture-plain-member-bundle`) now conforms as a remediation, apart from finding 5. The 00201 rule (`checker-copies-cited-constant`) is still a conforming Defence (SPEC 7). Its remediation does not conform, because the referral of the 16 inline texts is mostly not within section 4.

**Reproduction (SPEC 7).** I ran everything myself on `git archive` trees in `untracked/scratch/dbfrev2/`.
- **Red, 00211, at bd3cf6661:** `qa-changed --root … --files <module>` gave rc=1, with one finding, `_acceptance-stage2.inc.bash:358`.
- **Red, 00201, at 2722a74c6:** rc=1, with 4 findings: m7_check.py:67 and :438, and goodbye_check.py:35 and :39.
- **Green at 7a07fa2fb:** `qa-changed --base 22e599b3d` gave rc=0, and unit-tests ran both rule modules.
- **Fixes:** the checker tests pass at the tip: `test_m7_check` 50 OK; `test_triage_probe`, `test_deploy_check` and `test_u20_check` 160 OK. I did not rerun the A3 `--fake` legs this round.

**Findings**

1. **Major (3.4 "Fix them all"; section 4 lists the owner's decisions): the referral of the 16 inline texts rests on work, not on a decision.**
   - Section 4 gives the practitioner "what each fix should be". Changing product code, bumping versions and queueing host deploys is this project's normal agent workflow (CLAUDE.md: edit, commit, add to meta-deploy.list). `meta-deploy.list` already queues pingbus and ccy rebuilds on this branch's plans.
   - Section 4 also says "Where the work is entirely Agent-driven, do the work". None of the owner decisions it lists applies.
   - The stated reason is also wrong for some rows. "The product holds each as text inside a call" is false for three of them:
     - B6: `membership.FAREWELL_REASONS["removed"]`, a named Python value that can be imported.
     - B13: `login_message._RUNS_CHANGED`, also a named Python value.
     - B3: `var headings` in `board/table.go`, which can be read with the same narrow Go regex used for A3 and A13.
   - The other rows can largely be fixed the same way (an `ast` parse or narrow regex of the product's literal), or by having the product name the text.
   - To resolve: fix them all, or record an owner decision for each one that genuinely needs one. Until then, 3.4 is not met: "No Instance may be knowingly left unfixed at the end without the Owner's decision".
2. **Medium (3.4 corroboration; 3.1 "A clean run of the Rule is not evidence"): the widened scope has no independent search.** The scope now includes `tests/` and `scripts/`, but the only evidence for them is the rule finding nothing there. The report itself says "The independent search read the plan folders only". To resolve: search those trees independently, or write a sentence on why the hazard cannot arise there and confirm it by search.
3. **Medium (section 4 "Uncertainty is itself an Escalation trigger"; 3.1 saturation): two loose ends from the search.**
   - Its 6 UNSURE rows (00198, 00144, 00109, deploy_check REMEDY, kit paths, 00201 M7 paths) have no disposition in the DBF report or in Task 2.21.
   - It lists files it did not examine in depth and says so ("not claimed clean"): the deploy.bash files of 7 plans, the 00170 scripts, the 00128 census, 00054 update.bash, the bodies of 00203 m4 and m6, and most of u01_probe and triage_probe. That is not saturation.
   - To resolve: give each UNSURE row a ruling or refer it, and read the unread files.
4. **Low (3.3B, the narrowing sentence): A15 is dropped without a sentence.** The only reason given is "subject is a plan script, not product". The hazard (drift between deploy.bash:104 and `spike_probe.VERDICT_PASS`) is still present. To resolve: fix it (it is one plan-local import or read), or write a sentence on why it cannot drift.
5. **Low (3.1 and 3.4; carried from round 1, finding 6): the 00211 Go fixtures.** The report now gives a hazard sentence for them (in-memory Go tests that run no host pingbus) but still marks them "unread, not cleared". To resolve: read them to confirm the sentence, then mark them cleared.

**Resolved since round 1:**
- The 11 named or computed copies are fixed one by one with tests, including the drifted Tuwunel pin (now v1.9.4, read from `tuwunel.pin`).
- The scope is widened (but see finding 2).
- The gap where qa-changed reruns a rule only when its own module changes, and the gap where no gate runs the plan checker tests or `--fake` legs, are both recorded as toolchain gaps.
- Both reports note that the QA.md docs arrived after the red commits.
- The meta-deploy entries exist.

**Checked and still compliant:**
- Each red is a commit of its own and reproduces from a fresh export.
- The fixtures are kept as each rule's own tests, and each rule is seen to fire on them in the same run.
- The messages are blocking and carry a stable identifier that resolves in CLAUDE/QA.md.
- The next wider rules are named with reasons.
- The originating defects are fixed last, with reproducing tests.

I changed nothing in the branch. The scratch trees and run logs are in `/workspace/.claude/worktrees/agent-ae34f0a86f279431d-2cff53e5/untracked/scratch/dbfrev2/`.