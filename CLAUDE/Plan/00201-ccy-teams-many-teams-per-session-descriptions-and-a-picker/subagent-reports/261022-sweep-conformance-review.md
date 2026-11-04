# Conformance review: checker-copies-cited-constant, clause 2 (Task 2.22 sweep)

Tip 9001c6a3a (base F44 b0eb800fa) against SPEC.md sections 3, 4, 7. Reviewed from the reviewer's own worktree with detached scratch worktrees of b0dd7c9e1 and 9001c6a3a (isolation forbids using the named worktree); this file is at the same relative path in the reviewer's worktree.

## Verdict: NOT YET CONFORMING

## Reproduction

- Red, b0dd7c9e1: `python3 -m unittest tests.helpers.plan_acceptance.test_cited_constant_copy_rule` rc 1. Nine fixture tests pass; `Sweep.test_no_finding` fails with 39 findings (00161 u20, 00203 M4/M6/triage-hs/m2-check, 00211 stage2). Matches the handoff's 39.
- Green, 9001c6a3a: rc 0, 10 tests OK; the fixture tests run in the same module, so the rule was loaded.
- `./scripts/qa-changed.bash --base F44` at the tip: UNVERIFIED. The gates that ran first passed (bash-syntax, shellcheck, shell-directives, semgrep, ruff, unit-tests, docs); the go gate reported one failure, `src-go/internal/ui/pathinput` TestTheRenderedScreenAtANarrowWidthLosesNothing (a width test in code this branch does not touch; not investigated); the 36 suite failures came from the reviewer removing the scratch worktree while the run was still going (getcwd errors), so they say nothing about the branch. The report's rc 0 is neither confirmed nor contradicted.
- The report's 83 count (`red_at.py`) and the 23-row wrong-product table (`proofs.py`) were not reproduced: both scripts are untracked scratch in the other worktree (see F3).

## Findings

### F1 (high). Search-found instances left unfixed on grounds of cost; rule narrower than the search. Clauses 3.1, 3.4; section 4.
The search lists 34 value copies and 34 text copies. The report fixes the named-constant rows and refers, uncounted: hot-package texts (b1-b7, b9-b19, b22, b23), pingbus/agent-bus inline texts (A4, A5, b25, b26, b31, b33, B10), inline values (`.config/pingbus`, `agent-bus-state-`, `/usr/local/lib/agent-bus/tuwunel`, unit names, `ccy-session=`, `LAST_TOKEN=`, mktemp, rows 22, 25-27). Verified live: 00203 `_acceptance-m4.inc.bash` still holds `set to coordinator`. Reasons given: a format constant per message in deployed Go; "another container bump" and play reruns. These are costs, not decisions. Section 4 gives the practitioner "what each fix should be"; "a hundred Instances, or a thousand, is work"; the instances the search found "are fixed regardless". Handoff 1 step 3 planned the product-text reader and product naming and it was not finished. Only the narrower item is the Owner's: leaving the rule narrower than the search where the wider check risks false positives (section 4, last bullet). The report's reason for not building the fragment rule is of that kind but is stated in the Rule section, not put to the Owner as a decision.
Resolve: fix the confident remaining rows, or refer each with its section 4 trigger and a stated sentence; put the rule-width decision explicitly.

### F2 (high). Referral lacks the counts 3.4 requires; unsure items carry no stated doubt. Clause 3.4, 3.3 Part B, section 4.
Required: count fixed, count remaining, what stopped, what next. Given: "83 findings" plus unnumbered by-hand fixes; remaining is row ranges, never a number (roughly 50 of 68 search rows by my reading); stopper is cost (F1). Rows 17, 21, 29, 30, b17, b34 are "unsure": routing doubt upward is right, but the doubt must be stated; the search gives sentences for 21, 29, 30 that the report drops. Row 21 (`ccy-seats` label with no product counterpart: a negative check that cannot fail) may be a live defect and gets one clause.
Resolve: a table of every search row: fixed (by hand/pattern), referred (trigger and doubt), or not an instance (confirming search).

### F3 (high). Widening has no red commit of its own; the 83 count is retrospective from an uncommitted script. Clauses 3.3 Part A, 3.4.
b0dd7c9e1 is a valid red for the first OWNED list. c389a79b6 widens OWNED by 12 entries and adds the PATH kind in the same commit as 17 files of fixes (rule test +60/-22 beside the checker edits). 04f68d188 likewise combines the clause-1 pin-parser widening with its fix; "red at the parent" is a red of the rule before it was widened. The 83 exists only as output of untracked `red_at.py`, and the report concedes the commit's own count was 39.
Resolve: a rule-and-fixtures-only commit before the fixes (reconstruct by applying the rule hunk onto 626447e33), or commit the counting script with the report; restate counts per rule width.

### F4 (medium). By-pattern versus individual is asserted. Clause 3.4.
Report: "Each examined individually; none by pattern". Yet batch 1 (44 findings) went through shared readers and about 30 mark sites in M4/M6 were converted to `\Q<mark>\E` greps; search row 12 is one value at about 12 sites. That is change by pattern. State which, and the sample examined. The wrong-product table exercises the reader per constant, not each site.

### F5 (medium). Both gaps are reported, not worked around, but recorded where nobody looks, and (b) is a defendable class. Clauses 3.2, 8.7, section 4.
(a) Plan-folder `test_*.py` run by no gate: real consequence (00161 `test_acceptance_check` red since a5486643d, negatives passing for the wrong reason), fixed for that file, given to the Owner. Right in kind; but not recorded in CLAUDE/QA.md or plan open items, only in a subagent report. The `test_*.py` exclusion's sentence ("fake input") is now shown unsafe to rely on.
(b) Multiple command substitutions in one bash assignment: a real instance (00211 stage 2 `--fake` accepted a missing INBOX_DIR) found by the proofs, fixed with red/green. The report says a sound rule needs the syntax tree that `src-go/qa/bashlimits` already builds, so a bespoke detector is practical; this is not a toolchain gap but a class with an achievable rule, and not defending it is the Owner's decision (section 4). It is referred only in prose; the 7 text-sweep lines are not tabulated and the 6 dismissed as "harmless" carry no per-line sentence. A separate DBF remediation or a recorded Owner decision is owed.
Resolve: record both in QA.md or the plan's recorded decisions with the lines and sentences.

### F6 (medium). Blocking rule merged over a codebase that still carries search instances. Clause 3.5, section 4 (whilst waiting).
Green because OWNED lists only what was fixed. Section 4: leave the rule unmerged rather than weakened, except a single recorded known instance. The report asserts "merged at its width; the excess is a referral, not an exclusion" rather than asking. Not a suppression or baseline: no noqa/skip lists or exclusions in the diff.

### F7 (low).
- Identifier `checker-copies-cited-constant` resolves to a CLAUDE/QA.md heading, versioned with the rule in the same commits; message terse. 3.6 conforms (PATH kind documentation not exhaustively read).
- Decisions (next wider rule, excluded values) live in the report only; 8.7 wants them discoverable via the defences' own record.

## Referrals: legitimately outside authority?
- Rule-width (fragment rule, false-positive reason), the multi-substitution class, the plan-test gate, rows 17/21/29/30/b17/b34 (doubt): yes, Owner's, once stated as decisions with counts.
- Deployed-code naming, image/digest bump, replays: no. Cost, not authority (F1).

## What conforms
Rule exists, reads code, red proven on real code at a commit of its own (b0dd7c9e1), fixtures kept, green at tip with rule loaded; no suppression, baseline or exclusion of an instance; scope exclusions (completed plans, JOURNAL, subagent-reports, fixtures) carry runnable-checker-free sentences; independent search predates the clause, names what each technique found and not-examined areas; permanent and blocking via qa-helper-tests in qa-all/CI; the Task 2.22 sites and the second pin parser fixed with an empty-version refusal; wrong-product proofs disclosed their own first-run failures honestly.

## To reach CONFORMS
1. Fix remaining in-authority instances or refer each with stated sentence and counts (F1, F2).
2. Give the widening a red commit; commit the counting script (F3).
3. Record pattern versus individual with a sample (F4).
4. Record both gaps in the project record; put multi-substitution class and rule width to the Owner (F5, F6).
