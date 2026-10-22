# Conformance review round 2: offline-member-blocks (spec 1.1.0, sections 3, 4, 7, 8)

Reviewed: branch fix/offline-members-never-block, tip 95dbd8fc1; red commits 2e0c8aa6e, 1d9951f0e.
The branch is checked out in another worktree, so I reproduced in detached scratch worktrees of the
commits under my own untracked/scratch/. The branch was not changed.

## Reproduction

- RED 1, 2e0c8aa6e: pytest on tests/helpers/agent_bus_lxc/test_offline_member_rule.py exits 1;
  Sweep.test_no_finding fails with 19 findings (A x6, B x12, C x1), each message carrying the id and
  CLAUDE/QA.md "offline-member-blocks"; six fixture tests pass. VERIFIED.
- RED 2, 1d9951f0e: exits 1; the sweep fails on exactly one finding, src-go/internal/teams/restart/jobs.go:266
  (B); six pass. VERIFIED.
- GREEN, 95dbd8fc1: 7 of 7 pass, rule active (fixtures fire the detectors in the same run). VERIFIED.
- Project entry point at final: NOT VERIFIED. ./scripts/qa-changed.bash --base F44 exits 2 with
  "eslint for extensions/ is not set up (cd extensions && npm ci)", a missing dependency in the checkout,
  before any suite ran; reported, not worked around. qa-all.bash not run (instructed). Clause 3.5's green
  through the entry point rests on the report ("rc 0" by the sub-agent, no output quoted) and the coordinator's
  full run; the report itself says qa-all was not run.
- Originating symptom (stage 1 fake offline leg): needs LXC tooling; UNVERIFIED.

## Round 1 points

1. restart/jobs.go left outside the rule: ANSWERED. B widened with a fixture, red alone in 1d9951f0e
   (reproduced), fixed in 96a8fdb6c.
2. Narrowing made in a fix commit: PARTLY. The 978d02f45 exemption is now tightened (code lines of bash files,
   fixture), recorded in the report with a search sentence. Resolved for that exemption; see finding 1 for a
   repeat.
3. Original defect not last: ANSWERED by recording the departure from the order (3.4). Fake-leg red/green not shown.
4. Exclusions and wider rule as confirmed sentences: PARTLY. Go outside src-go/internal/teams and Python outside
   the bus helpers now have a sentence. Still unstated: completed plans (only "excluded"), JOURNAL,
   subagent-reports, tests/, testdata, vendor. The next wider rule is still one sentence joining two widenings.
5. Test-form detector: ANSWERED (cross-file, three languages).
6. Identifier / enumeration gap: ANSWERED (recorded as toolchain owner's gap).
7. Examined / by-pattern split: ANSWERED in words; see finding 4 on counts.
8. Search order a claim: NOT ANSWERED. The report still says "dispatched before the rule, without its pattern"
   with no dated pointer (dispatch, journal entry or commit time).
9. Sweep scope: ANSWERED.
Also the qa-changed routing gap is now disclosed (Permanence section), not resolved.

## Findings, by severity

### 1. MEDIUM. The rule was changed again inside a fix commit (3.3 Parts A and B; round 1 finding 2 repeated)

1959e982d ("NOT JUDGED verdicts ... R9 ...") edits the rule: HELPER_CALL now accepts plan_lxc_online_named, so
a file that calls only it is exempt from detector A. That is a Narrowing (more is let through), made in a
commit that also holds nine files of fixes, with no red commit of its own. The report says the rule counts it
"red first in its fixture"; no commit shows that (regex and fixture arrive together with the fixes), so the
claim cannot be checked from history. Substance looks sound: plan_lxc_online_named calls plan_lxc_online
(_members.inc.bash line 50), the new fixture proves the exemption, the comment-only fixture still fires, and
the \b stops plan_lxc_online_foo matching. Resolve: a commit of its own (rule + fixture, shown failing against
the old regex), or a sentence in the report's Narrowing section recording this second Narrowing, what it lets
through, and that the wrapper calls the base helper, and drop the unverifiable "red first".

### 2. MEDIUM. One instance remains outside the fix and the rule; the allowance is not cited (3.5, section 4)

Referral 1 (role change with no LXC member record still stops, advising `ccy-teams apply`) leaves a hazard
instance in place. The referral gives the count (1 left), what stopped the fixing, and what to try next (name
the handle from the entry's host/seat/repo; PLAN Task 2.8/2.9): that part conforms. But the rule does not flag
it, so the rule is green with a known instance present, and the report does not say that the specification's
single-recorded-known-instance allowance is what permits this, nor record the instance in the rule's own
CLAUDE/QA.md section (it names PLAN.md only). Resolve: cite the allowance, state the count is exactly one,
record it in the QA.md section; or fix it. Referral 2 (board record, Plan 00209 byte comparison) is a
proper separate referral with a task named.

### 3. MEDIUM. Entry-point green not shown (3.5, 7)

See Reproduction. qa-changed also does not rerun the rule when a Go or bash file it scans changes (disclosed),
so the pre-commit blocking gate is partial and only qa-all is complete. Resolve: the coordinator's qa-all run,
quoted in the final report.

### 4. LOW. Counts disagree in the report (7)

"19 findings in 10 files at the first red, 1 more at the second" matches my runs. The Fixes section says "the 20
findings are in 13 files"; 10 files plus jobs.go is 11. The extra files (check_rooms, roles) were over-
corrections found by the adversarial review, not rule findings. Say so, or correct the number.

### 5. LOW. Search order and exclusions still asserted (3.1)

Round 1 points 8 and the rest of 4. Search content passes (two techniques, what each found alone, unsure item
marked and settled in the search's favour).

## Checked and conforming

- Red commits are commits of their own: 2e0c8aa6e (rule, docs, search), 1d9951f0e (rule widening, QA.md and the
  test only); each fails before its fixes and passes at final.
- Fixtures carry ruleid marks, near misses kept; the restart phrase has a fixture and fires.
- Rule is a failing test: no warn mode, baseline, skip or suppression in the rule or fix diffs.
- Message carries the stable id and a path to the shipped QA.md section; AgentNotes records the calibration.
- Search reconciled in the rule's favour; instance count with sweep scope reported; sweep narrowing excludes only
  non-member code, with a sentence.
- Toolchain gaps reported, not worked around.

## Verdict

Round 1's high findings are resolved and red/green reproduce. Not yet conforming on a narrow set: a second
unrecorded Narrowing in a fix commit (1), the remaining instance without a cited allowance or rule-side record
(2), and the entry-point green unproven (3). Findings 4 and 5 are record fixes. All resolve with report and
QA.md edits plus the coordinator's quoted qa-all run; no rule code change is required.

VERDICT: NOT YET CONFORMING
