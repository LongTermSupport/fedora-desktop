# Conformance re-review: project_refusal_kind (Plan 00201 Task 2.20)

Reviewer: DBF conformance reviewer (run as a general agent; the dedicated type could not be
launched). Specification: method 1.1.0 (cached copy), sections 3, 4, 7 and 8. Branch
`fix/lxc-add-already-declared`, defence 0ca99358c, final 1c93f845b (the answer to the first
review is 2bf348eab and 1c93f845b). Re-review of `261022-lxc-add-conformance-review.md` (DOES NOT
CONFORM). Nothing in the branch was changed apart from this file; the scratch worktree and probe
copies were removed.

## Verdict: DOES NOT CONFORM

Five of the six findings from the first review are resolved, and finding 5 is resolved as far as
it can be. The tightening that answered finding 1 still lets through one shape that carries the
hazard: a function that hands the exit code on in one branch and re-asks on every other kind.
This is finding 1's first probe again, with the cancel written as `&stop{code}` instead of
`return err`. It is not in "Not caught", and nobody referred it (new finding 1). Separately, the
report lists hazard-carrying shapes the rule does not catch, yet says "Decisions referred to the
owner: None" and "None in the detector" (new finding 2).

## Reproduction

| Run                                                                                                                                                                        | Result                                                                                                               |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Red, entry point: `scripts/qa-go-qa.bash -t analyzers` in a detached worktree at 0ca99358c                                                                                 | exit 1; 4 x `error[project_refusal_kind]`: create/wizard.go:430, :600, :709, lxcadd/adder.go:548; `analyzers FAILED` |
| Green, entry point: `scripts/qa-go-qa.bash -t analyzers` at 1c93f845b (clean tree)                                                                                         | exit 0, `analyzers passed`                                                                                           |
| Rule active at green: the driver built from 1c93f845b, `go vet -vettool=` over the 0ca99358c `src-go`                                                                      | the same 4 findings, exit 1; `RefusalKind` is in `rules.All()` (9 analyzers)                                         |
| Fixture at green: `go test ./rules -run TestRefusalKind` (src-go/qa)                                                                                                       | ok                                                                                                                   |
| Tightening red: the 9778b10d3 `refusalkind.go` against the 1c93f845b fixture                                                                                               | FAIL, "no diagnostic was reported" at bad.go:67, :76, :86, as the report and journal say                             |
| Owner's symptom red: `TestAMemberDeclaredAlreadyIsRecognisedAndTheTeamApplied` (only that test and its helpers, from a0f763596) run in the 0ca99358c tree, real declare.py | FAIL; stderr "agent_bus_lxc_members entries 1 and 2 both declare agent@container-a in team alpha. Try again."        |
| Green, related: `go test ./internal/teams/... ./internal/declare/... ./internal/bus/declare/...`; `python3 -m unittest tests.helpers.agent_bus.test_declare`               | all ok                                                                                                               |
| Probes (scratch copy of src-go/qa, extra fixture file, removed afterwards)                                                                                                 | both in-hazard shapes below give "no diagnostic was reported"                                                        |

The full `qa-go-qa.bash` (test, coverage, golangci) and `qa-changed.bash` were not run. The
report's "qa-changed.bash passes" rests on the report.

## The first review's findings

| #   | Finding                                      | Status                                                                                                                                                                                                                                                                                                                                              |
| --- | -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Rule cleared by naming any kind              | Resolved for both probed shapes: `cancelsThenReasks` (bad.go:67) and `printsCodeThenReasks` (bad.go:76) now fire, plus `.Is(declare.ErrRefused)` (:86). Each was red under the old rule (reproduced). The new clearing condition (`handsCodeOn`) brings in a shape of its own (new finding 1).                                                      |
| 2   | Python half unfixed, candidates unreconciled | Resolved. `_present` and `check_lxc`'s team-presence refusal are `DeclareError` (78). `set` gives 78 only for a key the change leaves alone (`_REFUSED_KEY_RE`). The docstring matches. `settings.go:291` stops on anything but `ErrUsage`. The report's table gives A1-A5 and B1-B5 an outcome each, and the outcomes match the code I read.       |
| 3   | Python gap stated with an untrue reason      | Resolved. The report and GoProjectRules.md now say semgrep runs over Python but the class is a fact about meaning. The half is under "Defect classes that cannot be a rule", held by `test_declare.py`.                                                                                                                                             |
| 4   | Second defect not attributed to a class      | Resolved. "An add that refuses what is already there" is named, with the reason it cannot be a rule and what holds it (`LxcAlreadyDeclaredTest`, `lxcadd/declared_test.go`). B2-B5 have outcomes.                                                                                                                                                   |
| 5   | Search-before-rule ordering asserted         | Resolved as far as it can be. The report maps class A onto `project_refusal_kind` and says plainly that the only record of the ordering is the file time against the commit time. No journal entry was added. The search file says it did not open `src-go/qa/rules` or `src-go/qa/driver`. The gap is disclosed, not hidden, so it does not block. |
| 6   | Report completeness                          | Resolved. The `gum spin` change is described. The symptom red cites a test by name, and I reproduced it at 0ca99358c (above).                                                                                                                                                                                                                       |

## New findings, by severity

### 1. A code handed on in one branch clears a function that re-asks on every other kind (3.1 bounds, 3.3 Part B, section 4) - blocking

`checkRefusalKind` sets `kindRead` when any `return` in the function hands `.Code` on
(`handsCodeOn`), wherever that return sits. Probe, passes the rule:

```go
func probeStopsOnInterruptThenReasks(err error) (bool, error) {
	de, ok := errors.AsType[*declare.Error](err)
	if !ok {
		return false, err
	}
	if errors.Is(err, declare.ErrInterrupted) {
		return false, &stop{code: exitCode(de.Code)}
	}
	fmt.Printf("  %s. Try again.\n", de.Error())
	return false, nil
}
```

This re-asks on a 78, which is the hazard. It is the first review's finding 1, probe 1, with the
cancel written in the `&stop{term.ExitCode(de.Code)}` form that `lxcadd.Adder.checked` and
`create.Wizard.unanswerable` really use. So it is one edit away from real code. It is not under
"Not caught", and no sentence saying the hazard cannot arise there can be written. Under 3.3 Part B
that makes it an unrecorded suppression.

Resolve: let a code handoff clear the function only when it is an unconditional return in the
block that recovers the refusal (the `if de, ok := ...; ok { ... return code }` body, or the
statements after a `de, ok :=` / `if !ok` guard), not when it sits under a further condition.
Every real-tree clearing by code (adder.go:213, :671; wizard.go:174; chooser.go:254; ops.go:246)
has that shape. The two `checked`/`unanswerable` functions clear on `ErrUsage` anyway. Add the
probe to `bad.go`, show it red under the current rule, then green, and rerun the entry point.
Otherwise, list it under "Not caught" and refer it (new finding 2).

### 2. Hazard-carrying gaps are listed but neither referred nor reported as detector gaps (section 4, 3.2) - blocking

The docs' and report's "Not caught" list holds three shapes: `ErrUsage` named but branched the
wrong way round, a kind read of some other error, and a refusal re-wrapped before it is read.
Each carries the hazard: the function still re-asks on a 78. A second probe also passes:
`errors.Is(err, declare.ErrUsage) || errors.Is(err, declare.ErrRefused)` followed by a re-ask.
That is "wrong way round" only on a generous reading. The report still says "Decisions referred
to the owner: None" and "Toolchain and detector gaps: ... None in the detector". Section 4 puts
"leaving a Rule narrower than ... because the wider check is harder to build" with the Owner, and
3.2 asks for detector limits to be reported. The first review's resolution for finding 1 named
this same route ("refer it to the owner as 'rule narrower than the hazard' and list the shapes
under 'Not caught'").

Resolve: add the not-caught shapes to "Toolchain and detector gaps", with the reason for each
(branch direction and error identity need data flow the syntactic rule does not do; re-wrapping
hides the type). Refer them under "Decisions referred to the owner" as a rule left narrower than
the hazard, with what would close each. Widen the "wrong way round" wording to cover re-asking on
`ErrUsage` together with another kind. New finding 1, if it is not tightened, joins this list.

### 3. The tightening's red is not a commit of its own (3.3 Part A) - low, informational

The three new `bad.go` shapes and the rule change share 1c93f845b, so their red rests on the
journal (12:37 entry). I reproduced it by running the 9778b10d3 rule against the new fixture, so
it is verified. 3.3 Part A's commit requirement is met by 0ca99358c for the defence itself. If
new finding 1 is tightened, commit the new `bad.go` case before the rule change, so that red
survives as a commit.

## What passed

- 3.2: a bespoke `go/analysis` rule; both non-rule halves are recorded with the reason and what
  holds them.
- 3.3 Part A: 0ca99358c reproduces red through the entry point from a fresh worktree. The fixture
  stays as the rule's test, and the tightening red is reproducible.
- 3.3 Part B: the one narrowing the tightened rule added (a code handed on inside a returned
  literal, adder.go:665 `summary`) has its sentence ("stops with its code") and fixture
  (`stopsWithItsCode`). It is hazard-free as written in the real tree.
- 3.4: sweep scope is recorded (both modules, vendor excluded). 4 instances at the defence, all
  examined and fixed, with no suppression and no baseline. Every search candidate has an outcome,
  and A4 is really fixed (declare.py 78, settings stops on non-usage).
- 3.5: the rule is in `rules.All()` and blocking in the analyzers stage. It is green through
  `scripts/qa-go-qa.bash`, and the green-run rule is shown active against the red tree.
- 3.6 / 8.x: the message carries `project_refusal_kind`. It resolves to the analyzer `Doc` and the
  GoProjectRules.md section, both of which describe the tightened rule correctly.
- Original defect last, with a reproducing test: `TestAMemberDeclaredAlreadyIsRecognisedAndTheTeamApplied`
  is red at 0ca99358c (reproduced) and green at 1c93f845b. `LxcAlreadyDeclaredTest` pins the
  declare.py side.
