# Conformance re-review 5: project_refusal_kind (Plan 00201 Task 2.20)

Reviewer: DBF conformance reviewer (run as a general agent; the dedicated type could not be
launched). Specification: method 1.1.0 (cached copy), sections 3, 4, 7 and 8. Branch
`fix/lxc-add-already-declared`, tip 093f9bc94. Fourth tightening: red 74d67de9d, green 093f9bc94.
Scope: narrow re-review of `261022-lxc-add-conformance-review-4.md` (DOES NOT CONFORM) against
`git diff 92054fb44 093f9bc94`. Nothing in the branch was changed apart from this file. The
scratch worktrees and the probe fixture were removed.

## Verdict: CONFORMS

Both findings of review 4 are resolved. No further unlisted in-hazard shape was found by the
probes below. Two low notes do not block.

## Reproduction

| Run                                                                                       | Result                                                                          |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Red, single-rule harness at 74d67de9d: `go test ./rules -run TestRefusalKind` (src-go/qa) | FAIL: bad.go:168 and :188 "no diagnostic was reported". This matches the report |
| Green, harness at the tip 093f9bc94                                                       | ok                                                                              |
| Green, entry point at the tip: `scripts/qa-go-qa.bash -t analyzers`                       | exit 0, `analyzers passed`                                                      |
| Probes: an extra fixture file `probe.go` in a scratch copy at the tip, `-count=1`         | 4 of 4 in-hazard probes reported (listed below)                                 |

The full `qa-go-qa.bash`, `qa-changed.bash` and `qa-all.bash` were not run, as briefed. The
analyzers stage of the entry point runs the rule over the real tree, so the real-tree driver was
not rerun separately.

## Review 4's findings

| #   | Finding                                                    | Status                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| --- | ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | A kind filter in the `if`'s Init, read through a bare name | Resolved by tightening. The `*ast.Ident` case of `unfiltered` now calls `recoveryOk`. That function clears the name only when the Init is an assignment with a single right-hand side that `refusalsAt` recognises as a recovery (an `errors.AsType`/`errors.As` call or a type assertion, not an expression containing one), and only when the name is the same object as that assignment's last left-hand name. `filtersInTheInit` (bad.go:168) and `filtersByTheSecondName` (:188) were red in their own commit 74d67de9d and are green at the tip. `filtersBesideTheInit` (:178) is a regression guard, and it fired already at red. `asInTheInit` in good.go keeps the hazard-free `ok := errors.As(...); ok` clear. The analyzer `Doc` and GoProjectRules.md now describe the same condition as the code. |
| 2   | Referral left merging open (low)                           | Resolved in the report: "the merge of this branch is held for that answer ... only the owner can release it" (section 4, "What to do whilst waiting"). See low note B.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |

## Probes at the tip (all reported, so no new gap)

- A recovering `if` with `ok` alone in the Cond whose body hands the code on only under
  `errors.Is(err, declare.ErrInterrupted)` and re-asks otherwise (probe.go:12).
- A tagless `switch de, ok := errors.AsType[...](err); { case ok && errors.Is(...Interrupted): return de.Code; default: re-ask }` (probe.go:22).
- An Init `ok = ok && errors.As(err, &de)` assigning to an outer `ok` that already held the kind
  (probe.go:34).
- The review-4 Init filter wrapped in parentheses, `ok := (errors.As(...) && errors.Is(...))`
  (probe.go:43).

The "Not caught" entry added at this tip (`ok && other.Code != 0` counts any `*declare.Error`'s
`Code`) is recorded as hazard-free as far as found. It is listed and does not block.

## Low notes (non-blocking)

A. 3.6 record accuracy. The report says that `filtersInTheInit`, `filtersBesideTheInit`,
`filtersByTheSecondName` and `asInTheInit` were "red in 74d67de9d", but only bad.go:168 and :188
failed there. `filtersBesideTheInit` already fired, and `asInTheInit` is a good.go case that
passed. The line numbers it gives are correct. To resolve, one clause could name the other two
as guards added in the red commit.

B. PLAN.md Task 2.20 still does not say that the merge is held for the owner. Review 4 asked for
this in the report and in PLAN.md. The report is the referral of record, so this does not block.
A sentence in Task 2.20 would keep the plan in step with the report.

## What passed

- 3.3 Part A: the fourth tightening's red is a commit of its own (74d67de9d), and it fails the
  single-rule harness as reproduced above.
- 3.1 bounds: the clearing name is now tied to the recovery's own result, as review 4 asked. Every
  real-tree clearing `if` (`de, ok := errors.AsType[...](err); ok`) still clears, and the entry
  point is green.
- 3.5: the rule is blocking in the analyzers stage, and its documentation in
  CLAUDE/GoProjectRules.md ("project_refusal_kind") matches the code.
- Section 4: the listed gaps are referred, with the merge held for the owner.
