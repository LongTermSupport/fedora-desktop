# Review round 3: fix/lxc-add-already-declared (Plan 00201 Task 2.20)

Scope: `git diff F44...HEAD` in full (tip 1b919c135), and `git diff 08a8804b7 HEAD` against
each round-2 finding.

Runs: `go test ./internal/teams/... ./internal/declare/... ./internal/bus/declare/...` ok;
`go test ./rules` (src-go/qa) ok; `python3 -m unittest tests.helpers.agent_bus.test_declare`
OK (171 tests). The `qa/driver` build used as `go vet -vettool` over `src-go/./...` reports
nothing on the real tree. The rule probe is kept as
`untracked/scratch/review-r3/probe.go.txt`. It was run as a temporary package,
`src-go/internal/zzprobe/`, which has been removed, and the tracked tree is clean.

## Verdict: FIX-FIRST

Round-2 finding 1 and nits A and B are resolved. The remaining issue is small: the tightened
`project_refusal_kind` misses a shape that its own Doc and GoProjectRules.md say it reports.
That shape is not in the "Not caught" list either.

## Round-2 findings, checked

1. **Resolved.** `_REFUSAL_READS` (`helpers/agent_bus/declare.py:170-178`) gives the keys each
   cross-key rule reads: `exclude` reads {exclude, org, repos}, `humans` reads
   {humans, removed_humans} and `org` reads {org, repos}. `set` gives 78 only when that set does
   not overlap `changes`. I checked the table against every `_fail` in `teamfile.py`. The only
   other rule that reads a second key is the default `server_name` (team + suffix), and `set`
   cannot change `team`, so 78 is right there. `with_removals` puts `removed_humans` into
   `changes` when `humans` drops someone, and the cases it creates still come out right.
   Parse-order masking also gives the right kind both ways:

   - The file is at fault first and the changed key is checked later: 78. The file must be
     fixed first in any case.
   - A changed key is checked earlier: 64, then 78 once a valid value is entered.

   The round-2 probe is now `SetTest.test_a_refusal_whose_rule_reads_a_changed_key_is_the_values_refusal`.
   It checks 64 for repos that still list the excluded repository, 78 for a key the rule does not
   read (humans), 0 for a passing value, and that the file is unchanged after each refusal.

- **Nit A. Resolved.** Task 2.20 says `ccy-teams apply` and `lxc-add` of an absent team keep
  their own 64 (`ops.go:160`, `adder.go:341`), because the team comes from a command-line
  argument and is never asked again. The cited lines are correct.
- **Nit B. Resolved.** Task 2.20 now records that `add-repo` of a repository the team already
  trusts is UNCHANGED.
- **Nit C (round-1 nit 7, no summary box).** Still left. It is not blocking.

## Rule change (`src-go/qa/rules/refusalkind.go`)

The probe file has four functions:

| Probe                                                                                                     | Result          | Correct?                        |
| --------------------------------------------------------------------------------------------------------- | --------------- | ------------------------------- |
| `condFilter`: `if de, ok := AsType(..); ok && de.Code == 130 { return &stop{de.Code} }`, then "Try again" | **not flagged** | No: a missed finding (F1)       |
| `asThenCode`: `var de; if !errors.As(err, &de) { return 1 }; return de.Code`                              | flagged         | No: a false finding (nit N1)    |
| `viaLocal`: `code := de.Code; return code`                                                                | flagged         | False finding, fails safe (N1)  |
| `earlyReask`: `if ok && de.Code != 130 { re-ask }; return &stop{de.Code}`                                 | not flagged     | Already listed under Not caught |

## New findings

### F1 (FIX): a kind filter in the recovering `if` Cond clears the recovery

`codeHandedOn` (`refusalkind.go`, the `*ast.IfStmt` case) clears an if's Init and its whole
Cond whenever the body returns the code directly. When the Cond adds a kind filter, such as
`ok && de.Code == 130` or `ok && errors.Is(err, declare.ErrInterrupted)`, the code is handed on
for that one kind only. Every other kind falls through to the re-ask.

This is the `stopsOnOneKindThenReasks` hazard, written in one statement instead of two. The
Doc says "A code handed on for one kind inside a branch of its own leaves the rest asked again".
GoProjectRules.md says the same shape "is a finding". Neither lists it under "Not caught".

`good.go`'s `exitsOnAnyCode` (`ok && de.Code != 0`) is the only conjunct form that has to stay
green. It is the shape the real tree's `chooser.failed` uses.

Concrete fix, either of these:

- **(a) Make it red.** Clear the Cond only when it is the recovery result alone (`ok`, the
  `errors.As(...)` call, or `!` of either), or `&&` of that with `<de>.Code != 0`. Add
  `condFilter` to `bad.go` as a `want` case, and keep `exitsOnAnyCode` green.
- **(b) Document it.** Add the shape to the "Not caught" list in GoProjectRules.md and to the
  Doc, as a third shape referred to the owner.

(a) is a few lines and matches what the docs already claim.

### Nits

- **N1.** Some valid code is flagged although it hands the code on. This is the safe
  direction, and the real tree has no case of it.

  - **The `errors.As` guard.** In `var de *declare.Error; if !errors.As(err, &de) { return ... }; return de.Code`
    the recovery is in an if Cond. `codeHandedOn` counts a later return only for
    assign, decl and expr statements, so the recovery is flagged. This is Go's usual
    `errors.As` form, and GoProjectRules.md names `errors.As` as a recovery the rule
    understands.
  - **A local copy.** `code := de.Code; return code` is flagged too.

  Fix: in the `*ast.IfStmt` case, also clear Init and Cond when
  `returnsCode(pass, stmts[i+1:])` holds. Add a `good.go` case for the `errors.As` guard.
  Alternatively, record both as known false positives.

- **N2.** `_REFUSAL_READS` reads more keys than it needs, and that fails safe. A `humans`
  pattern refusal ("must match ...") clears 78 when only `removed_humans` changes. An `org`
  format refusal clears 78 when only `repos` changes. No value for the changed key can fix
  either refusal, so each is asked again, but only up to the loop's tries. Nothing to change.
  If anything, add a comment saying the table is meant to read more keys than it needs.

## Checked and found sound

- **Versions.** CCY 3.99.14.15 and container 2.78.18 are in step across `CCY_VERSION`,
  `REQUIRED_CONTAINER_VERSION`, the Dockerfile label, the changelog heading, Task 2.20 and all
  four meta-deploy.list mentions. No 2.78.17 or 3.99.14.14 is left outside history files
  (journal and reports). The changelog explains the unreleased .13 / .14 and .16 / .17. F44 is at
  3.99.14.12 / 2.78.15. Another agent (roomid-fix) may also bump these, so the numbers are a
  likely merge conflict. Re-check them at merge time.
- **meta-deploy.list.** The wording matches the code. 00211's "finds the ccy image already at
  2.78.18" still follows from 00213's whole ccy play running earlier.
- **Changelog.** The new `set` sentence gives the read sets correctly.
- **The owner's rules.** The 78 for a refusal of the file's state says "fix it in the file
  first" and is not asked again. Round 2 checked how each caller handles it, and this round
  changes no caller.
- **Out of scope.** Nothing in `git diff 08a8804b7 HEAD` touches offline or stopped-member
  handling.
- **The red commit.** The DBF red commit 7e40c23a9 is an ancestor of HEAD.
- **Public repo.** No install-specific names: only example-org, placeholders and `<user>@<container>`.
