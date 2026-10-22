# Review round 4: fix/lxc-add-already-declared (Plan 00201 Task 2.20)

Scope: `git diff 1b919c135 92054fb44` against the round-3 findings (`refusalkind.go`,
`badrefusal/bad.go` and `good.go`, `CLAUDE/GoProjectRules.md`), and a skim of
`git diff F44...HEAD`. Since round 3 the only changes are the rule, its fixtures, its docs and
the plan reports. No other file has changed.

Runs: `go test ./rules` (src-go/qa) passed. The driver built to `untracked/scratch/bin/rules-driver`.
`go vet -vettool` with it reported nothing over `./...` in both `src-go` and `src-go/qa`. The
probe is kept as `untracked/scratch/review-r4/probe.go.txt`. It was run as a temporary package,
`src-go/internal/zzprobe/`, which has been removed. The tracked tree is clean.

## Verdict: FIX-FIRST

F1 and N1 are resolved as round 3 asked. One finding is left. The new `unfiltered` accepts any
identifier that the if's Init declares as "the bare recovery result". A kind filter moved into
the Init therefore clears the recovery again. This is F1 again, one statement earlier. The Doc
and GoProjectRules.md say it is a finding, and "Not caught" does not list it.

## Round-3 findings, checked

- **F1. Resolved.** `codeHandedOn` now clears an if's Init and Cond only when `unfiltered` holds.
  `unfiltered` accepts an identifier declared in the Init, the recovering call, or that
  `&& .Code != 0` (in either operand order). `bad.go` gains five `want` cases: a kind filter, a
  code filter, a filter after `errors.As`, a filter through a bool variable, and the variable
  alone. `exitsOnAnyCode` stays green.
- **N1. Resolved (the `errors.As` guard).** The second `case` takes `!<bare result>` followed by
  a later direct code return. `good.go` gains `guardsThenExits`. The local-copy false positive
  (`code := de.Code; return code`) is now recorded under "Flagged though hazard-free", which is
  the documented alternative.
- **N2.** Nothing was to change, and nothing did.

## Probes (`probe.go.txt`)

| Probe                                                                                                                        | Result      | Correct?                        |
| ---------------------------------------------------------------------------------------------------------------------------- | ----------- | ------------------------------- |
| P1 `initFilter`: `if ok := errors.As(err, &de) && errors.Is(err, ErrInterrupted); ok { return &stop{de.Code} }`, then re-ask | not flagged | **No: missed finding (F1')**    |
| P3 `initBoolFilter`: `if found, intr := errors.As(err, &de), errors.Is(err, ErrInterrupted); intr {...}`, then re-ask        | not flagged | **No: missed finding (F1')**    |
| P4 `otherCode`: `ok && other.Code != 0` (`.Code` of a different `*declare.Error`)                                            | not flagged | No, but contrived (nit A)       |
| P5 `guardThenFilter`: `if !errors.As {return}; if de.Code == 130 {return code}`, then re-ask                                 | flagged     | Yes                             |
| P6 `guardReasksNonRefusal`: a negated guard whose body re-asks, then the code handed on                                      | clean       | Yes (a non-refusal re-asked)    |
| P7 `negConj`: `if !(errors.As(..) && de.Code == 64) { re-ask }; return code`                                                 | flagged     | Yes (78 re-asked)               |
| P8 `negInitOk`: `if ok := errors.As(..); !ok { return 1 }; return de.Code`                                                   | clean       | Yes                             |
| P9 `tripleConj`: `ok && de.Code != 0 && errors.Is(.., ErrInterrupted)`                                                       | flagged     | Yes                             |
| P10 `shadowOk`: the recovery in an assignment, then `if ok := errors.Is(..); ok { return code }`                             | flagged     | Yes                             |
| P11 `elseReask`: the body hands the code on, and the `else` re-asks                                                          | clean       | Yes (the else is a non-refusal) |

## New findings

### F1' (FIX): an identifier from the if's Init counts as the bare result, whatever its value

`unfiltered`'s `*ast.Ident` case (`refusalkind.go:138-140`) checks only that the identifier is
declared inside `s.Init`. It does not check that the identifier holds the recovery's result. So:

- **P1.** `ok := errors.As(err, &de) && errors.Is(err, declare.ErrInterrupted)` puts the kind
  filter on the Init's right-hand side, and the Cond `ok` passes.
- **P3.** A second variable that the Init declares (`found, intr := errors.As(..), errors.Is(..)`)
  passes as well.

In both, every kind other than an interrupt reaches the re-ask, a 78 included. This is
`filtersByAVariable` (red), with the variable declared in the Init instead of the line before.
The Doc and GoProjectRules.md both define the cleared Cond as "the ok its Init declares" and
"the bare recovery result". Neither listed probe is that.

Concrete fix, either of these:

- **(a) Make it red.** Accept the identifier only when the Init is an `*ast.AssignStmt` with one
  right-hand side, that right-hand side is a recovering call (`recoversRefusal`, through
  `ast.Unparen`), and the identifier is the call's result: `Lhs[1]` for `errors.AsType`, `Lhs[0]`
  for `errors.As`. In practice, match `TypesInfo.Defs` of that Lhs to the `Uses` object. Add P1
  to `bad.go` as a `want`. Keep `exitsOnAnyCode` and the `if de, ok := ...; ok` goods green.
- **(b) Document it.** List "a kind filter assigned in the if's Init" under "Not caught" in
  GoProjectRules.md and the Doc, referred to the owner.

(a) is a few lines.

### Nits

- **A.** `codeNonZero` accepts the `.Code` of any `*declare.Error`, not only the one
  recovered (P4). This is error identity, the same limit as the existing "kind read of some other
  error" Not-caught bullet. Widen that bullet to cover a code read too, or leave it as it is.
- **B.** `token` is imported and used in the new `case`, and `ast.Unparen` needs Go 1.22. The go
  directive covers it. Nothing to do.

## Checked and found sound

- The `F44...HEAD` skim found nothing new outside the rule. Every other file is as round 3
  reviewed it.
- The GoProjectRules.md fixture list matches `bad.go` and `good.go` case for case.
- The DBF red commit e5ec1bab1 comes before the green commit 92054fb44 on the branch.
- Public repo: the new text has no install-specific names.
- Versions: the round-3 note still applies. Re-check CCY 3.99.14.15 / 2.78.18 at merge time,
  because roomid-fix may bump them too.
