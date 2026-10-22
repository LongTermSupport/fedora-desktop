# Review round 2: fix/lxc-add-already-declared (Plan 00201 Task 2.20)

Scope: `git diff F44...HEAD` in full, and `git diff 9778b10d3 HEAD` against each round-1 finding.

Run: `go test ./internal/teams/... ./internal/declare/... ./internal/bus/declare/...` ok;
`go test ./rules/` (src-go/qa) ok; `python3 -m unittest tests.helpers.agent_bus.test_declare`
OK (170). The project's analyzers (`qa/driver` as `go vet -vettool`) over `./...` gave no
`project_refusal_kind` finding on the real tree. Probe files are in `untracked/scratch/review-r2/`.

## Verdict: FIX-FIRST

Round-1 findings 2 to 6 are resolved, and nit 7 was left as stated. Round-1 finding 1 is fixed
for the case it named: the same key, retyped wrong the same way, is now 64. A cross-key form
of the same misclassification is still reachable, and it has a one-line probe.

## Round-1 findings, checked

1. **Partly resolved.** See finding 1 below.
2. **Resolved.** The Stop on a member declared otherwise now says "Nothing was changed. ccy-teams
   apply <team> joins it as declared." (`adder.go:321`), and `declared_test.go` pins it.
3. **Resolved.** `lxcadd.checked` (`adder.go:646`) and `create.unanswerable` (`wizard.go:747`)
   say anything that is not ErrUsage, ErrInterrupted or ErrRefused as "declare.py failed (exit N)"
   with its stderr, and stop with that code. There is a test for each, and each asserts that the
   question is asked only once. `run` builds every `*declare.Error` from a non-zero ExitError,
   so a code of 0 cannot turn a failure into a success.
4. **Resolved.** The changelog 3.99.14.14 entry now records `add-repo` UNCHANGED, the 78 for an
   absent team, the narrowed `set` 78 and `create`'s apply hint. Task 2.20 records all of them
   except `add-repo` (nit B).
5. **Resolved.** `_replace_lxc` refuses to rewrite or remove a line that carries a comment
   (DeclareError, 78). Because `lxc-add --dry-run` also goes through `add_lxc`, and from it
   `_replace_lxc`, the refusal comes in lxc-add's summary, before any yes and before a takeover
   demotes anyone. A test covers it.
6. **Resolved.** The rule now clears a function only on a use of `declare.ErrUsage`, or on a
   `.Code` handed on whole: returned bare, converted, or as a field of a returned literal or its
   address. `.Code` inside a Printf and `.Is(ErrRefused)` no longer count. The new fixture shapes
   are red. The Doc and GoProjectRules.md state what the rule still does not catch.
7. **Left, as stated.** `confirmApply` has no summary box. This is a nit and does not block.

## The 64-to-78 change for a team not declared present: callers checked

- **The settings screen.** `inputField` (`settings.go:291`) asks again only on ErrUsage. On 78 it
  reports the error and leaves, which is what the meta-deploy wording says. `save` treats Usage
  and Refused alike, as a notice. `Edit` and `prepareSave` treat any read error as a notice.
- **The picker.** `inputDescription` uses `check` with no team, so the change cannot reach it.
  On a Describe error the picker reports it and leaves, whatever the code.
- **Login.** On an Element error it reports the error and fails, whatever the code.
- **Chooser widen and failed().** These pass declare's code on. `ccy-teams` choose of an absent
  team would now exit 78, and no bash caller tests for 64 or 78. `InPicker` falls through to
  "failed, exit N" for 78.
- **ops and lxc-add.** Both check presence in Go before calling declare.py, so they still give
  their own 64 (`ops.go:160`, `adder.go:341`). `test-ccy-teams-command.bash:1458` (apply of an
  absent team, 64) is ops-side and unaffected (nit A).
- **The board.** It does not branch on the kind.
- **Tests.** The Go mirror (`bus/declare.present` refusedf) and its tests, `reads_test.go`, and the
  wizard's `--team --yes` case are updated. No bash suite asserts declare.py's 64 for an absent
  team. Plays and scripts call declare.py only for `element-teams`, which the change does not
  touch.

## `_REFUSED_KEY_RE`

`teamfile._fail` always writes `team file: <key>: <rule>`. Every key it names is
`[a-z_]+`, except "team file", which a dict entry cannot produce. "not valid JSON" cannot arise
here either. A refusal that names no key falls to 64, which is bounded by each loop's tries.
That is the safe direction.

## Findings

### 1. (FIX) `set` still gives 78 for a value another answer passes when the refused key's rule reads a changed key

`helpers/agent_bus/declare.py:1148-1149`. The new test is whether the key named in the refusal
is among `changes`. Three teamfile rules name one key and read others:

- `exclude` reads `org` and `repos`: "'x' is also listed in repos", and "needs an org".
- `humans` reads `removed_humans`: "was removed from the team earlier".
- `org` (required) reads `repos`.

When the change sets one of the keys that is read, but not to a value that fixes the refusal,
`after` is refused with the same message as `before`, the named key is not in `changes`, and
the result is 78, "fix it in the file first". Yet another value for the very key being set would
pass.

Probe (`untracked/scratch/review-r2/hv.yml`: alpha with org example-org, repos
`["example-org/foo"]`, exclude `["foo"]`):

- `set --team alpha --value repos '["example-org/foo","example-org/bar"]' --check` gives
  **rc 78**: "refused as it stands (team file: exclude: 'foo' is also listed in repos ...); fix it
  in the file first".
- `set --team alpha --value repos '["example-org/bar"]' --check` gives **rc 0**.

So the value could be retyped. In the settings screen, `inputField` (`settings.go:291`) leaves
with ExitFailure on that 78 when it should ask again. This is round-1 finding 1's class, a
recoverable input mistake reported as the file's state, through a cross-key rule.

Fix: give 78 only when no changed key is read by the refused key's rule. For example, keep a
small table next to `_REFUSED_KEY_RE` of the keys each rule reads:
`{"exclude": {"exclude","org","repos"}, "humans": {"humans","removed_humans"}, "org": {"org","repos"}}`,
defaulting to `{key}`. Give 78 only if that set and `changes` are disjoint. Add a test that sets
`repos` while it still lists an excluded repository: 64, and the file unchanged. A probe that
cannot be made to agree with the teamfile rules is what this table guards against, so a comment
there should name `teamfile._exclude`, `_removed_humans` and `parse_team_file`.

### Nits (not blocking)

- **A.** A team that is not declared present now has two kinds. declare.py gives it 78. `ccy-teams apply <absent team>` (`ops.go:160`) and `lxc-add` (`adder.go:341`) still give their own 64
  "See --help". For a CLI argument naming a missing team, 64 is defensible, and none of these
  callers re-asks, so the split does no harm. One sentence in Task 2.20 or GoProjectRules.md
  would stop the next reader thinking it is an oversight.
- **B.** Task 2.20 does not mention that `add-repo` of a repository the team already trusts is now
  UNCHANGED. The changelog and GoProjectRules.md do mention it.
- **C.** Round-1 nit 7 (no summary box on the write-nothing path) is left.

## Checked and found sound

- **Versions and meta-deploy.** CCY 3.99.14.14 and container 2.78.17 are in step across
  `CCY_VERSION`, `REQUIRED_CONTAINER_VERSION`, the Dockerfile label, the changelog heading, Task
  2.20 and every meta-deploy mention. F44 is at 3.99.14.12 / 2.78.15, so the skipped .13 / .16
  are explained in the changelog. The meta-deploy order works: 00213's deploy runs the whole ccy
  play first, so 00211's "finds the image already at 2.78.17" holds. The added sentence ("the
  settings screen stops, instead of asking again, on a team the host_vars no longer declare")
  matches `inputField`.
- **Offline handling.** Nothing in `git diff 9778b10d3 HEAD` touches offline or stopped-member
  handling.
- **Public repo.** No install-specific names: placeholders and example-org only.
