# Conformance re-review 4: project_refusal_kind (Plan 00201 Task 2.20)

Reviewer: DBF conformance reviewer (run as a general agent; the dedicated type could not be
launched). Specification: method 1.1.0 (cached copy), sections 3, 4, 7 and 8. Branch
`fix/lxc-add-already-declared`, tip 92054fb44. Third tightening: red e5ec1bab1, green 92054fb44.
Scope: narrow re-review of `261022-lxc-add-conformance-review-3.md` (DOES NOT CONFORM) against
`git diff 1b919c135 92054fb44`. Nothing in the branch was changed apart from this file. The
scratch worktrees and the probe fixture were removed.

## Verdict: DOES NOT CONFORM

Review 3 finding 1 is resolved for the shapes it named: a kind or a code tested in the recovering
`if`'s Cond is now a finding. One shape that carries the hazard still passes the rule. When the
filter moves into the `if`'s Init (`if ok := errors.As(err, &de) && errors.Is(err, declare.ErrInterrupted); ok {...}`), the Cond is again a single identifier. It passes, and
"Not caught" does not list it (new finding 1). Review 3 finding 2 (low) is not resolved
(finding 2).

## Reproduction

| Run                                                                                        | Result                                                                                                                                                       |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Red, single-rule harness at e5ec1bab1: `go test ./rules -run TestRefusalKind` (src-go/qa)  | FAIL: bad.go:119, :128, :138 "no diagnostic was reported"; good.go:97 "unexpected diagnostic". This matches the report                                       |
| Green, harness at the tip 92054fb44                                                        | ok                                                                                                                                                           |
| Green, entry point at the tip: `scripts/qa-go-qa.bash -t analyzers`                        | exit 0, `analyzers passed`                                                                                                                                   |
| Probes: an extra fixture file `probe.go` in a scratch copy at the tip, run with `-count=1` | 3 of 5 in-hazard probes report nothing (lines 12, 30, 47). The 2 that fire: `ok && intr` in the Cond, and `ok && de.Code != 0 && errors.Is(...)` in the Cond |

The full `qa-go-qa.bash` and `qa-changed.bash` were not run. The real-tree driver was not rerun
separately, because the analyzers stage of the entry point runs the rule over the real tree.

## Review 3's findings

| #   | Finding                                                           | Status                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --- | ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | A recovering `if` whose Cond narrows to one kind clears           | Resolved for the Cond. `unfiltered` accepts only an identifier, the `errors.As` call, or either `&& .Code != 0`. The three probes from review 3, plus two bool-variable variants, are in `bad.go`. They were red in their own commit, e5ec1bab1, and are green at the tip. `guardsThenExits` (a negated guard) was added to `good.go`. The docs and the analyzer `Doc` describe the new condition. The gap still left is new finding 1. |
| 2   | Rule not stated as held unmerged while the referral is open (low) | Not resolved. The report now says it "does not decide whether the branch merges before that answer, which is the coordinator's and the owner's call". Section 4 is the reverse: the Practitioner leaves the Rule unmerged rather than merged in a weakened form. PLAN.md Task 2.20 still says nothing about merging.                                                                                                                    |

## New findings, by severity

### 1. A kind filter in the `if`'s Init, read through a bare identifier in the Cond, clears the recovery (3.1 bounds, 3.3 Part B, section 4): blocking

`unfiltered`'s `*ast.Ident` case accepts any identifier the Init declares. It does not check that
the identifier is the `ok` result of the recovering call. So the review-3 hazard comes back when
the filter is moved into the Init. Both of these pass the tip rule, and each re-asks on a 78:

```go
var de *declare.Error
if ok := errors.As(err, &de) && errors.Is(err, declare.ErrInterrupted); ok {
	return false, &stopCode{code: de.Code}
}
fmt.Println("Try again.")
return false, nil
```

```go
var de *declare.Error
if _, intr := errors.As(err, &de), errors.Is(err, declare.ErrInterrupted); intr {
	return false, &stopCode{code: de.Code}
}
```

The code also differs from its documentation. The analyzer `Doc` and GoProjectRules.md both say
"the ok its Init declares", which reads as the recovery's `ok`, but the code accepts any
identifier the Init declares. "Not caught" lists neither shape.

A third probe also passes: the same Init filter with a negated Cond (`...; !ok { re-ask }; return de.Code`). That one is the listed "re-ask in an earlier branch before the unconditional code
return", so it is referred to the owner and does not block.

Resolve (either):

- Tighten. Let the identifier clear only when the Init is an assignment whose single right-hand
  side is the recovering call (`recoversRefusal`), and the identifier is that call's `ok` (the
  second left-hand name of `errors.AsType`, the only one of `errors.As`). Commit the two probes
  into `bad.go` as red first, then make the change and show them green. Keep `good.go` and the
  real tree green through the entry point.
- Or list it. Add "a kind filter in the recovering `if`'s Init, read through a variable in the
  Cond" to "Not caught" and to the report's gaps table, with its reason and what would close it,
  and so to the owner referral.

Tightening is a small syntactic check, and every real-tree clearing `if` uses
`de, ok := errors.AsType[...](err); ok`.

### 2. The referral still leaves merging open (section 4, "What to do whilst waiting"): low

This carries over from review 3 finding 2. See the table above. Resolve with one sentence in
the report's "Decisions referred to the owner" and in PLAN.md Task 2.20: the branch is not merged
until the owner decides on the listed gaps. Or record the owner's acceptance of merging at this
width.

## What passed

- 3.3 Part A: the third tightening's red is a commit of its own (e5ec1bab1) and fails the
  single-rule harness, as reproduced above.
- 3.3 Part B: `guardsThenExits` is free of the hazard. A non-declare error returns 1; every
  declare refusal hands its code on.
- 3.5: the rule is blocking in the analyzers stage, and the entry point is green at the tip.
- 3.6: the report's Proof section and the journal entry give the same red lines (119, 128, 138;
  good.go:97) and commits as the history.
