# DBF conformance review: gh-scope-outside-ssot

Reviewer: `defence-before-fix:conformance-reviewer` (Sonnet), in a worktree of its own. The
review was written there, and the coordinator copied it here unchanged in substance.

- Spec: Defence Before Fix method specification 1.0.1.
- Defence commit: 27aee075. Final code commit: b9475431. Report commit: 00aa7d6d.
- Nothing in the branch under review was changed. Every command below was a read-only
  checkout, a gate run, or a scratch mutant of the rule, built and thrown away under
  `untracked/scratch/` and never committed.

## Reproduction results (checked first, per section 7)

### Red run: confirmed

- At 27aee075, in the worktree, `./scripts/qa-gh-scopes.bash` (the gate `qa-all.bash`
  wires in) exits 1.
- Findings: 56 lines in 10 files. They are the same files, at the same line numbers, as the
  report lists: `run.bash`, `scripts/gh-account-setup.bash`,
  `playbooks/imports/play-github-cli-multi.yml`, `docs/configuration.md`,
  `docs/github-multi-account.md`, `docs/headless-provisioning.md`,
  `docs/headless-server-install.md`, `docs/vm-acceptance-testing.md`,
  `files/home/.local/bin/vmtest` and
  `CLAUDE/Plan/00139-commit-signing-everywhere/deploy.bash`.
- One discrepancy. The gate printed `passed: 797 failed: 10`, so 807 files were scanned; the
  report says 806 (Finding 3).

The full entry point, `./scripts/qa-all.bash`, did not reach the gate at 27aee075 in the
worktree. `ANSIBLE_VAULT_PASSWORD_FILE` was pointed at a dummy script, and
qa-ansible-syntax passed (82 playbooks). The run then stopped at `qa-js.bash` with exit 2,
`Missing required tools (node / extensions node_modules)`. That is a limit of the worktree,
not a verdict, so it is recorded here and not counted against the remediation. The
gate-level red run above stands as the reproduction of clause 3.3's proof.

### Green run: confirmed, through the real entry point

`/workspace/scripts/qa-all.bash`, run in the main checkout (clean at 00aa7d6d, nothing
changed or committed), exits 0:

```
✓ gh-scope-outside-ssot: passed: 813
✓ run-bash-gh-scopes: passed: 19
✓ ccy-git-signing: passed: 26
✓ bash-history-search: passed: 23
✓ QA passed: 1111 files checked
```

Every figure matches the report. It is a full, unmodified run through the project's own
entry point, with the rule active and enforced.

### Fixture proof: confirmed

- `semgrep --test` passes against the defence commit's fixture: 8 `ruleid` and 6 `ok`
  lines, in one file.
- Rebuilding the report's mutant (the `gh auth login|refresh --scopes/-s` pattern line
  deleted) and re-running gives the failure the report claims: `missed lines: [11]`.

### Rule stability: confirmed

- `.semgrep/gh-scopes.yml` is byte-identical at 27aee075 and b9475431 (the diff is empty).
- Its only commit in history is the defence commit.
- No exclude and no suppression was added during the fix.

**Overall:** the remediation conforms to the six clauses of section 3 for this defect. The
three findings below are low severity and concern the record. Nothing needed an owner
decision.

## Findings

### Finding 1 (low): one exclusion's confirming search is not recorded

Clause 3.3 Part B, and clause 3.1's search standard.

The rule excludes `.claude/hooks-daemon/**` and `.claude/skills/**`, as upstream files
replaced wholesale on every upgrade. That is a legitimate narrowing if true. Clause 3.3
Part B requires the excluded code to be searched to clause 3.1's standard, so that the
sentence is confirmed and not merely asserted.

Neither report records that search:

- the independent searcher's "Not counted" section lists neither path;
- its search commands do not name them;
- the DBF report says nothing about searching those trees.

I searched them at 27aee075. `.claude/hooks-daemon/**` has no tracked files in this
checkout. The only "scope" hits in `.claude/skills/**` are in `docs-qa/SKILL.md` and
`planning/SKILL.md`, and they mean plan or task scope, not a GitHub OAuth scope. So the
sentence is true, but the record does not show that anyone checked it.

**Resolve by:** one line in the report saying both trees were searched and found clean.

### Finding 2 (low): clause 3.1's "Runner check" statement is not with the rest of its record

Clause 3.1 says five items must be on the record before clause 3.2 begins:

1. the Class;
2. the Hazard sentence;
3. the two search techniques;
4. the next wider Rule;
5. where the Defect was reported as a behaviour, whether a check a Runner executes pins
   that behaviour, or why not.

"Defect and class" states the first four. The fifth appears only in the report's last
section, "The original defect". The content is present and correct.

**Resolve by:** a cross-reference in "Defect and class".

### Finding 3 (low): the red run's scanned total does not reproduce exactly

Clause 3.4's count, and section 7's reproduction.

The report says the red run found 56 lines in 10 files "of 806 scanned". Running
`./scripts/qa-gh-scopes.bash` at 27aee075 twice gives `passed: 797 failed: 10`, which is
807 scanned. The findings themselves reproduce exactly.

**Resolve by:** correcting 806 to 807, or noting how the figure was derived.

## What was checked and found sound

- **Rule width.** Two forms: a token that can only be a GitHub scope, and a literal
  `--scopes`/`-s` list. That covers the search's 15 live sightings, plus two the search
  missed: the `vmtest` message and a comment in the PhpStorm-token block. Checked against
  the red-run file list. The next wider rule is named, and the reason it was not built
  (false positives on ordinary words) is a practitioner-level reason under section 4.

- **Proof commit.** 27aee075 adds only:

  - the rule, fixture and gate;
  - the gate's `qa-all.bash` wiring;
  - its `CLAUDE/QA.md` documentation;
  - the search report.

  It fixes nothing, and it stays reachable in history through the `--no-ff` merges.

- **Independent search.** It ran before the rule existed and was blind to it. It used two
  techniques, and says in both directions what each found that the other could not. It
  was reconciled in the rule's favour.

- **Sweep and fixes.** The scope is recorded as the project's first calibration on the
  point. All 57 instances were fixed with real changes and no suppression. That is 56,
  plus one introduced mid-fix and caught by the rule. `gh_request_missing_scopes` was
  spot-checked.

- **Enforcement.** The gate is a hard stage of `scripts/qa-all.bash`, and it is shown
  through the real entry point.

- **Message and docs.** A terse message carrying the identifier, and a `CLAUDE/QA.md`
  section on what the rule is, why it exists and how to fix a finding. The toolchain gaps
  are reported, not worked around.

- **Original defect.** It was fixed last. Its reproducing test,
  `scripts/test-run-bash-gh-scopes.bash`, is wired into `qa-all.bash`, is green at the
  final commit, and asserts the one-pass behaviour.

## What cannot be reproduced after the fact

This review cannot re-run the claim that the independent search ran before the rule was
written and without sight of it. The rule and the search report were committed together.
The searcher's report says it did not see the rule, and the journal timestamps fit. The
claim rests on the practitioner's account, and it is not counted as a finding.
