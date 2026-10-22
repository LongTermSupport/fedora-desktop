**Verdict: NOT YET CONFORMING.** The rule for 00211 (fixture-plain-member-bundle) conforms apart from findings 5 to 7. The rule for 00201 (checker-copies-cited-constant) is a conforming Defence (SPEC 7 allows that with the sweep incomplete), but its remediation does not conform, because instances the search found were left unfixed.

**Reproduction (SPEC 7).** All runs were mine, on `git archive` trees under `untracked/scratch/dbfrev-*`.
- **Red, 00211 rule at bd3cf6661:** `qa-changed.bash --root <tree> --files <rule module>` gave rc=1. The fixtures pass and the sweep reports exactly `_acceptance-stage2.inc.bash:358`.
- **Red, 00201 rule at 2722a74c6:** same command, rc=1, with 4 findings: m7_check.py:67 and :438, and goodbye_check.py:35 and :39.
- The red runs also showed "hidden-bindings could not run", because an exported tree has no git. That comes from the setup, not the rules.
- **Green at HEAD a0b04b9d7:** `qa-changed.bash --base 22e599b3d` gave rc=0, with unit-tests running both modules. Each module run directly: 12 tests OK. The 00211 rule is also green at fe0ca54d2.
- **Original symptoms:**
  - M3: HEAD's `test_m7_check.py` run against the pre-fix `m7_check.py` fails (`test_expect_session_settings` FAIL, `test_the_seated_guard_runs_the_kit_command` ERROR). It passes at HEAD (49 tests). `test_goodbye_check` passes (34 tests).
  - A3: `acceptance.bash --stage 2 --fake` gave rc=0. Its new legs call the real `a3_take` through the real `config.load_bundle`: accepted gives PASS, unaccepted is refused.

**Findings**

1. **(Major, 3.4 and 4: "The Instances the search found are fixed regardless") Unfixed instances, 00201 rule.** Task 2.21 refers 11 copied values, 16 copied inline texts and A15 to the owner. One of them is drifted today: Plan 00161 `triage_probe.py` pins Tuwunel v1.9.3, but the product is at v1.9.4.
   - The reasons given are work, not decisions only the owner can make. One is "the product must name each text as a constant first". The other is "A6 runs in a container with no checkout", and QA.md's own remedy covers that case (the script that starts the checker hands it the value).
   - The referral also gives no per-instance plan for what to try next.
   - To resolve: fix them, or get a recorded owner decision for each. The allowance for a single known instance does not stretch to 28.
2. **(Medium, 3.3B and 3.4 "Entire codebase") Sweep scope, 00201 rule.** The rule and the independent search cover only checkers in active plan folders. The class was named "a checker hard-coding a value its subject owns", and `tests/` and `scripts/test-*.bash` can carry that hazard. No sentence explains why they cannot, and no search confirms one. To resolve: write and confirm that sentence, or widen the scope.
3. **(Medium, 3.1 behaviour pin; 6) No QA runner executes the checks that pin the reported behaviour.** For M3 that is `test_m7_check.py`; for A3 it is the `--fake` legs. qa-changed did not route `test_m7_check.py` even though it changed; qa-routing.tsv only routes `CLAUDE/Plan/*.bash` to plan-script-logging. Neither report says whether a runner pins the behaviour. To resolve: state it, or wire those checks into a gate.
4. **(Medium, 3.5 and 3.2 mechanism gap) Both rules.** qa-changed reruns a rule only when its own module changes. A commit that adds an instance to a scanned plan, script or helper file passes qa-changed and is caught only at qa-all or CI. Both rules do block there, so 3.5 holds. The reports mention this under Permanence but not as a toolchain gap. To resolve: record it as a gap, or route the scanned paths to these modules.
5. **(Low, 3.6 "versioned as one") Both rules.** The `CLAUDE/QA.md` sections arrived in the fix commits, not the defence commits. At bd3cf6661 and 2722a74c6 the printed identifier `CLAUDE/QA.md "<id>"` resolves to nothing. Note it in the reports; the commits cannot be rewritten without a force-push.
6. **(Low, 3.1 saturation and 3.4 corroboration) 00211 rule.** The independent search left Go fixtures under `src-go/internal` (seat/, teams/, statusline/) marked "UNREAD, not cleared". The DBF report does not mention this. To resolve: read them, or give the reason they cannot carry the hazard.
7. **(Info, 3.2)** Both detectors read code, using text parsing and `ast`, and use unittest only as the runner. That is an acceptable bespoke detector, not a test acting as the detector.

**Checked and found compliant:** each red is a commit of its own, reachable, and reproduces from an exported tree. The fixtures are kept as each rule's own tests, including near misses. Each rule is shown to be loaded by firing on a fixture in the same run. The messages are terse and carry a stable identifier. QA.md states what each rule is, why it exists and how to fix a finding (8.4), and the rule sections can be listed (8.5).
- The next wider rule is named for both, with reasons based on the hazard or on false positives.
- Each narrowing sentence is recorded: `-e`/`--setenv`/`--out=-`, archived plans, and expansions or indented lines for 00201.
- Each independent search was dispatched before its rule, records what text search and reading each found, and agrees with its rule (00211) or is reconciled in the search's favour (00201).
- The fixes address the hazard: m7_check reads `seat.session_settings` and the kit settings, and `config.py` is unchanged.
- The originating defects are fixed last, with reproducing tests.

I changed nothing in the branch. The scratch trees and run logs are under `/workspace/.claude/worktrees/agent-ae34f0a86f279431d-2cff53e5/untracked/scratch/` (`dbfrev-*`, `bd3.qa.*`, `272.qa.*`).