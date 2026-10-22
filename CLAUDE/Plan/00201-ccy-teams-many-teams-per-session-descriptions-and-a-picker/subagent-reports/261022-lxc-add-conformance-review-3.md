# Conformance re-review 3: project_refusal_kind (Plan 00201 Task 2.20)

Reviewer: DBF conformance reviewer (run as a general agent; the dedicated type could not be
launched). Specification: method 1.1.0 (cached copy), sections 3, 4, 7 and 8. Branch
`fix/lxc-add-already-declared`, tip 1b919c135. Second tightening: red 7e40c23a9, green c47ad86d1.
Re-review of `261022-lxc-add-conformance-review-2.md` (DOES NOT CONFORM). Nothing in the branch
was changed apart from this file. The scratch worktrees, the probe copy and the built driver
were removed.

## Verdict: DOES NOT CONFORM

Both findings of review 2 are resolved. Review 2's informational finding 3 is resolved too: the
red now survives as a commit of its own. The new clearing condition brings in one new shape that
carries the hazard. A recovering `if` whose condition narrows the recovery to one kind or one
code (`ok && errors.Is(err, declare.ErrInterrupted)`, `ok && de.Code == 130`) hands the code on
for that kind only, then re-asks on every other refusal. It passes the rule. It is not in "Not
caught", and it is not referred (new finding 1).

## Reproduction

| Run                                                                                                                              | Result                                                                                                |
| -------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Tightening red, single-rule harness: `go test ./rules -run TestRefusalKind` in a detached worktree at 7e40c23a9                  | FAIL, "bad.go:91: no diagnostic was reported" (`stopsOnOneKindThenReasks`), as the report says        |
| Entry point at 7e40c23a9: `scripts/qa-go-qa.bash -t analyzers`                                                                   | exit 0, `analyzers passed`. Expected: this red is in the fixture, and the real tree was already fixed |
| Green, entry point at tip 1b919c135: `scripts/qa-go-qa.bash -t analyzers`                                                        | exit 0, `analyzers passed`                                                                            |
| Green, harness at tip: `go test ./rules` (src-go/qa)                                                                             | ok. No rule source changed between c47ad86d1 and the tip                                              |
| Rule active at green: the driver built from the tip, `go vet -vettool=` over the 0ca99358c `src-go`                              | exit 1, the same 4 findings: create/wizard.go:430, :600, :709, lxcadd/adder.go:548                    |
| Related green: `go test ./internal/teams/... ./internal/declare/...`; `python3 -m unittest tests.helpers.agent_bus.test_declare` | all ok                                                                                                |
| Probes: a scratch copy of src-go/qa with an extra fixture file `probe.go`, removed afterwards                                    | all 3 in-hazard shapes below give "no diagnostic was reported"                                        |

The full `qa-go-qa.bash` (test, coverage, golangci) and `qa-changed.bash` were not run.

## Review 2's findings

| #   | Finding                                                  | Status                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --- | -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | A code handed on in one branch clears the whole function | Resolved. `codeHandedOn` now clears a recovery only where the return sits as a direct statement of the recovering block. The probe is `bad.go` `stopsOnOneKindThenReasks`. I reproduced it red at 7e40c23a9 (a commit of its own) and green at c47ad86d1. The tip rule still reports the 4 defence-commit findings. Its own new gap is new finding 1.                                                                   |
| 2   | Not-caught shapes neither referred nor reported as gaps  | Resolved for the shapes it names. GoProjectRules.md "Not caught" lists four shapes, each with its reason. That includes the widened "re-asked on together with another kind" and the new "re-ask in an earlier branch". The report's "Toolchain and detector gaps" table gives the same four shapes, with what would close each. "Decisions referred to the owner" refers them as a rule left narrower than the hazard. |
| 3   | Tightening red not a commit of its own (informational)   | Resolved: 7e40c23a9 holds only the fixture case (and the review files).                                                                                                                                                                                                                                                                                                                                                 |

## New findings, by severity

### 1. A recovering `if` whose condition narrows to one kind clears the recovery (3.1 bounds, 3.3 Part B, section 4) - blocking

`codeHandedOn` adds an `IfStmt`'s whole `Init` and `Cond` to `handedOn` whenever its body
returns the code directly. Any conjunct in `Cond` therefore clears, including one that limits
the code handoff to a single kind. The hazard is then the one review 2 finding 1 described,
written as a single `if`. These probes pass the tip rule, and each re-asks on a 78:

```go
if de, ok := errors.AsType[*declare.Error](err); ok && errors.Is(err, declare.ErrInterrupted) {
	return false, &probeStop{code: de.Code}
}
fmt.Printf("  %s. Try again.\n", err)
return false, nil
```

The same result comes with `ok && de.Code == 130` in place of the `errors.Is`. It also comes with
`var de *declare.Error; if errors.As(err, &de) && errors.Is(err, declare.ErrInterrupted) {...}`.
The clearance was built for `good.go` `exitsOnAnyCode` (`ok && de.Code != 0`, the chooser's
shape), but nothing tells that conjunct apart from one that names a kind. None of the four "Not
caught" entries covers this. The closest, "a re-ask in an earlier branch before the
unconditional code return", is a different shape: there the re-ask comes before the return; here
the narrowing sits in the recovering condition itself. Under 3.3 Part B, an exclusion with no
hazard sentence is an unrecorded suppression. Under section 4, a doubtful exclusion goes to the
Owner.

Resolve (either):

- Tighten. Let an `if` clear its recovery only when `Cond` is the bare recovery (`ok`, the
  `errors.As` call, or `ok && <conjunct>`). The conjunct may read only `.Code` against zero and
  names no kind and no other code value. Otherwise treat the return as conditional. Commit the
  three probes into `bad.go` first and show them red, then change the rule and show them green.
  Keep `exitsOnAnyCode` green, and rerun the entry point and the real-tree driver.
- Or list it. Add "a recovering `if` whose condition also names a kind or a code value" to
  GoProjectRules.md "Not caught" and to the report's gaps table, with its reason and what would
  close it, and so to the owner referral.

Tightening is cheap here: the check is syntactic, and the real tree's clearing `if`s
(`create.Wizard.run`, `lxcadd.Adder.Run`/`summary`, `chooser.failed`, `ops.childExit`) should
still clear. Check that against the driver.

### 2. The report does not say the rule is held unmerged while the referral is open (section 4, "What to do whilst waiting") - low

The report and PLAN.md Task 2.20 refer the narrower rule to the owner ("The owner decides
whether to keep the rule as it stands"). Neither says that the branch, and with it the rule as
narrowed, waits for that decision rather than merging. Section 4 says the Practitioner "leaves
the Rule unmerged rather than merged in a weakened form". The branch is not merged today, so
nothing has gone wrong yet. But nothing on the record stops the coordinator merging it before
the owner answers.

Resolve: add one sentence to "Decisions referred to the owner", and to Task 2.20: the branch is
not merged until the owner decides on the listed gaps. Or record that the owner has already
accepted merging at this width.

## What passed

- 3.3 Part A: the second tightening's red is a commit of its own (7e40c23a9) and fails the
  single-rule harness, reproduced. The defence's own red stays at 0ca99358c, and the tip rule
  still reports its 4 findings there.
- 3.3 Part B: the three new `good.go` shapes (an assignment then a direct return, `ok && de.Code != 0`, a type switch case) are hazard-free as written. The journal records the probe
  that was checked, then removed, and listed.
- 3.2 / section 4: the four listed gaps have a reason each and a means of closing each, and they
  are referred as a rule narrower than the hazard. The wording "re-asked on together with another
  kind" covers review 2's `ErrUsage || ErrRefused` probe.
- 3.5: the rule is in `rules.All()` and blocking in the analyzers stage. The entry point is green
  at the tip.
- 3.6 / 8.x: the analyzer `Doc` and GoProjectRules.md "project_refusal_kind" describe the
  tightened clearing condition consistently with `codeHandedOn`.
- Original defect and A4: `test_declare` and the teams and declare Go packages are green at the
  tip. The `set` refinement (`_REFUSAL_READS`, 326d7e9b8) has its red recorded in the journal.
- The report template is filled. Its commits (7e40c23a9, c47ad86d1, 326d7e9b8) match the history.
