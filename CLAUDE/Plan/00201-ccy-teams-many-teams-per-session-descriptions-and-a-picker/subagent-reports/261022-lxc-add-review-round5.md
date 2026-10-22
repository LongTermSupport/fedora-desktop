# Review round 5: fix/lxc-add-already-declared (Plan 00201 Task 2.20)

Scope: `git diff 92054fb44 093f9bc94` (`src-go/qa/rules/refusalkind.go`, `badrefusal/bad.go`
and `good.go`, `CLAUDE/GoProjectRules.md`, plan reports), checked against round 4's F1' and nit A.

Runs: `go test ./rules` (src-go/qa) passed. The driver built to
`untracked/scratch/bin/rules-driver`; `go vet -vettool` with it reported nothing over `./...` in
`src-go` and in `src-go/qa`. Probes ran in a scratch copy of the qa module under
`untracked/scratch/review-r5/` (since removed); the probe file is kept as
`untracked/scratch/review-r5/probe.go.txt`. The tracked tree is clean.

## Verdict: MERGE

## Round-4 findings, checked

- **F1'. Resolved, as fix (a).** The `*ast.Ident` case of `unfiltered` now calls `recoveryOk`.
  That accepts the identifier only when the Init is an `AssignStmt` with one right-hand side,
  that right-hand side is itself a recovery (`refusalsAt` on the unparenthesised node, so not a
  call that wraps one), and the identifier is the same object as the last left-hand name (`Defs`,
  or `Uses` for `=`). Round 4's P1 (kind folded into the Init's `ok`) and P3 (a second name
  declared beside the recovery) are now `want` cases in `bad.go` (`filtersInTheInit`,
  `filtersBesideTheInit`, `filtersByTheSecondName`). `good.go` gains `asInTheInit`.
- **DBF red proven.** The round-3 rule (92054fb44) run against the new fixtures fails at
  `bad.go:168` and `bad.go:188` (P1 and the second-name case). `filtersBesideTheInit` was already
  red through the `&&` path. The red commit 74d67de9d comes before the green commit 093f9bc94.
- **Nit A. Resolved, by documenting it.** "Not caught, hazard-free as far as found:
  `ok && other.Code != 0`" has been added to GoProjectRules.md. It is the same error-identity
  limit as the existing bullet.

## Probes (`probe.go.txt`, current rule)

| Probe                                                                  | Result  | Correct? |
| ---------------------------------------------------------------------- | ------- | -------- |
| p6 `if de, ok := err.(*declare.Error); ok { return de.Code }`          | clean   | Yes      |
| p11 `if ok := errors.As(..); ok && de.Code != 0 { return de.Code }`    | clean   | Yes      |
| p13 `if ok := (errors.As(..)); (ok) {...}`                             | clean   | Yes      |
| p14 `ok` declared earlier, `if ok = errors.As(..); ok {...}`           | clean   | Yes      |
| p15 `if de, ok := errors.AsType[*declare.Error](err); ok {...}`        | clean   | Yes      |
| p16 Init `found := errors.As(..)`, Cond an `ok` parameter, then re-ask | flagged | Yes      |
| p17 `de, _ := errors.AsType(..)`, Cond `errors.Is(.., ErrInterrupted)` | flagged | Yes      |

No shape was found that clears a filtered recovery and is not listed under "Not caught".

## Nits (do not block)

- **N1.** The `good.go` list in the GoProjectRules.md fixture bullet does not name the new
  `asInTheInit` ("`errors.As` in the Init, its `ok` alone"). The round-4 check that the list
  matches case for case no longer holds exactly. One phrase would fix it.
- **N2.** The Doc string and the GoProjectRules.md line that changed now run past the wrapping of
  the text around them. This is cosmetic.

## Checked and found sound

- `recoveryOk` cannot be reached with a nil Init: the type assertion on a nil `ast.Stmt` fails
  and returns false.
- A blank `_` as the last name has no object, so it never matches.
- Public repo: the new text has no install-specific names.
- Versions: the earlier note still applies. Re-check CCY 3.99.14.15 / 2.78.18 at merge time
  against roomid-fix.
