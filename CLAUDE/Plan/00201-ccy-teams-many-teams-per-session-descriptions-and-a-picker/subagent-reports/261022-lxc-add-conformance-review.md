# Conformance review: project_refusal_kind (Plan 00201 Task 2.20)

Reviewer: DBF conformance reviewer (run as a general agent; the dedicated type could not be
launched). Specification: method 1.1.0, sections 3, 4, 7 and 8. Branch
`fix/lxc-add-already-declared`, defence 0ca99358c, final 9778b10d3. Nothing in the branch was
changed apart from this file.

## Verdict: DOES NOT CONFORM

The defence fires, is blocking and resolves to documentation, and the four Go instances are
fixed. It does not conform because the rule is narrower than its own hazard in a way that is
neither recorded nor referred (finding 1), and because instances of the class's Python half
that the independent search reported are left unfixed and unaccounted for (finding 2).

## Reproduction

| Run                                                                                                                                                                  | Result                                                                                                                       |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Red, entry point: `scripts/qa-go-qa.bash -t analyzers` in a detached worktree at 0ca99358c                                                                           | exit 1; 4 x `error[project_refusal_kind]`: create/wizard.go:430, :600, :709, lxcadd/adder.go:548; summary `analyzers FAILED` |
| Red, driver direct at 0ca99358c (`go vet -vettool=` over `src-go`; `src-go/qa` clean)                                                                                | the same 4 findings, exit 1                                                                                                  |
| Red, fixture: `go test ./rules -run TestRefusalKind` at 0ca99358c                                                                                                    | ok (the analysistest `want` lines fire)                                                                                      |
| Green, entry point: `scripts/qa-go-qa.bash -t analyzers` at 9778b10d3                                                                                                | exit 0, `analyzers passed`                                                                                                   |
| Rule loaded at green: the 9778b10d3 driver run over the 0ca99358c tree                                                                                               | 4 findings, so the green run's rule is active; `RefusalKind` is in `rules.All()` (table.go:278)                              |
| Originating symptom, green: `go test ./internal/teams/lxcadd ./internal/teams/create ./internal/declare`; `python3 -m unittest tests.helpers.agent_bus.test_declare` | all ok                                                                                                                       |
| Originating symptom, red                                                                                                                                             | not rerun by me; rests on the handoff's record ("entries 1 and 2 both declare ... Try again.") - unverified                  |

The full `qa-go-qa.bash` (test, coverage, golangci) was not run; only its `analyzers` stage, which
is the stage that carries the rule.

## Findings, by severity

### 1. The rule is satisfied by naming any kind, so in-hazard code passes (3.1 bounds, 3.3 Part B, section 4) - blocking

`checkRefusalKind` clears a function when any `declare.Err*` variable is referenced anywhere in it
(`isKindVar`), or when `.Code` or `.Is` of a `declare.Error` is selected (`readsKind`). None of
these is the discrimination the hazard needs (re-ask only on `ErrUsage`). Probe, in a scratch copy
of `src-go/qa` with one extra fixture file (removed afterwards):

- a function that cancels on `errors.Is(err, declare.ErrInterrupted)` and then re-asks on every
  `*declare.Error`, a 78 included;
- a function that reads `de.Code` only to print it and re-asks on every kind.

Both carry the hazard; `TestRefusalKind` failed with "no diagnostic was reported" for both. The
first is one edit away from the fixed `lxcadd.Adder.checked`, which reads `ErrInterrupted` too.
The documentation's "Not caught" list names a kind read of some other error and a re-wrapped
refusal, not these, and no sentence saying the hazard cannot arise there can be written, so this is
an unrecorded narrowing that the specification treats as suppression.

Resolve: tighten the rule so the clearing read is the `ErrUsage` discrimination (`errors.Is(..., declare.ErrUsage)` or `.Is(declare.ErrUsage)`, or `.Code` handed on whole as an exit/returned
value, not merely selected), add the two probe cases to `bad.go`, and rerun red/green. If it is
left as is, refer it to the owner as "rule narrower than the hazard" under section 4 and list both
shapes under "Not caught".

### 2. Python-half instances from the independent search are unfixed and unreconciled (3.1, 3.4, section 4) - blocking

The class as documented has two halves; the second is "a refusal must carry the right kind" in
declare.py. The search reported (A4) that `_present` (declare.py:825, "team X is not declared
present, with a port") is a file-state refusal raised as `UsageError` (64). It still is.
`settings.go` `CheckSet` loop (settings.go:~285-292) asks for a setting value with the team taken
from the screen, re-asks on `ErrUsage`, and so re-asks up to three times on a refusal that no value
can pass, without naming the file: the hazard. `check_lxc`'s "team ... is not declared present"
(declare.py:1236) is the same shape for lxc-add, where the team is not the question being asked.
The docstring (declare.py:84-86) now records this as "a value refused alone ... 64", which
contradicts the rule's own definition of 78 ("the file, its entries, or the answer against them").

The report says "No widening was needed" and "Decisions referred to the owner: None", and does not
give a disposition for search candidates A2 to A5 or B2 to B4. The search is the authority over the
rule's reach (3.4), so each candidate needs a recorded outcome.

Resolve: decide the contract precisely (the useful test is "can another answer to this question
pass"), make `_present` and the lxc team-presence refusal 78 when the team is not the value being
asked (or have the callers stop on them), with tests; and add a table to the report giving each
search candidate (A1-A5, B1-B5) its outcome: fixed (commit), not the hazard (sentence), or referred.

### 3. The Python-side toolchain gap is stated with a reason that is not true (3.2)

The report and GoProjectRules.md say "no Python rule gate exists for `helpers/`". The project runs
semgrep over Python today (`qa-ready-wait-rules`, `qa-speech-to-text-rules`, `qa-gh-scopes`), so a
bespoke rule is available in principle. Clause 3.2 asks for the reason a detector was not
practical. The likely true reason is that which refusals are file-state is a fact about meaning,
not a shape in the syntax tree.

Resolve: restate the gap with that reason, and move this half into "Defect classes that cannot be
a rule" with what holds it (`test_declare.py`), as is done for the readiness-probe class. Or build
the semgrep rule.

### 4. The second defect, a non-idempotent add, is fixed but not attributed to a class (3.1, 3.2)

The originating defect has two causes: the retry, and `lxc-add` refusing a member that is already
declared instead of answering UNCHANGED. The search treated the second as its own class (B) and
found B2 (`create` of a declared team), B3 (`add-repo`, now UNCHANGED per the handoff) and B4
(`drop-limit`). The report folds the lxc-add half into "The original defect" with no class, no rule
and no "cannot be a rule" entry, and gives B2 and B4 no outcome.

Resolve: name class B and its hazard, record a rule or a "cannot be a rule" entry with what holds
it, and give each B candidate an outcome (fixed, not the hazard with its sentence, or referred).

### 5. Ordering of the search before the rule is asserted, not shown (3.1)

The search report was committed in the defence commit 0ca99358c. The file is dated 12:29:55
against the 12:32:21 commit, but nothing on record shows the search was chosen and run before the
rule was run on the real tree. The search was also briefed as "Class A, retry on config-file state",
which is not the rule's class wording, and the report does not map one onto the other.

Resolve: add a journal entry (via `mkplan.bash --journal`) with the dispatch time and brief, and
state in the report how class A maps onto `project_refusal_kind`.

### 6. Report completeness (template) - low

- The fix commit 2bf348eab also replaces the owner lookup spinner (`Gum.Spin`) with a printed
  line. The report mentions the gh 404 change but not this behaviour change.
- "Red at 0ca99358c ... the real tree, 4 findings": confirmed. "All 4 examined individually":
  consistent with the diffs (each site now goes through `checked`/`unanswerable`, `isRefusal`
  removed).
- The originating symptom red should cite the captured file (`untracked/scratch/lxcadd-red.txt` is
  local only) or a test name that reproduces it at 0ca99358c.

## What passed

- 3.2: a bespoke `go/analysis` rule that reads code; no test stands in for it.
- 3.3 Part A: the red run survives as its own commit before any fix, reproducible from a fresh
  worktree through the entry point; the fixture is kept as the rule's test.
- 3.4 (Go half): sweep scope recorded (both modules, vendor excluded); count 4, all fixed, no
  suppression, no baseline.
- 3.5: in `rules.All()`, run by go-qa's analyzers stage, blocking (`FAILED`, exit 1); green through
  `scripts/qa-go-qa.bash`.
- 3.6 / 8.3-8.5: the message is terse and carries `project_refusal_kind`; it resolves to the
  analyzer `Doc` (`go vet -vettool=<driver> help`) and the GoProjectRules.md section, both versioned
  with the rule, and both state the correct construction. The catalogue table lists the rule.
- 3.1 next wider rule named with a hazard-based reason (retry loops over pure values carry no file).
- 3.1 behaviour pin: `lxcadd/declared_test.go` runs the real declare.py against an already declared
  member.
