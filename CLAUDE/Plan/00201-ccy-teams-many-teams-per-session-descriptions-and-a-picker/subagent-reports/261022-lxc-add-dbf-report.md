# Defence Before Fix report: project_refusal_kind (Plan 00201 Task 2.20)

Branch `fix/lxc-add-already-declared`. Defence commit 0ca99358c (red). Fix commits a0f763596
(lxc-add, declare.py) and 2bf348eab (create wizard, gh lookups). F44 merged in at 11b48c687.
The rule was tightened, and the remaining instances fixed, after the conformance review
(`261022-lxc-add-conformance-review.md`, DOES NOT CONFORM) and Opus round 1 (FIX-FIRST), in the
commit after this report's first draft (9778b10d3).

## Specification followed

Method specification 1.1.0, cached copy (`/root/.cache/defence-before-fix/spec/SPEC.md`), as
printed by the skill's refresh script; detector specification 1.0.0 and toolchain specification
0.2.0, cached.

## Toolchain

Go: go-qa-ci's `go-qa`, pinned by the `tool` directive of `src-go/qa/go.mod`, configured by
`src-go/qa/go-qa.toml`, entry point `scripts/qa-go-qa.bash` (its `analyzers` stage runs the
project rules; in `qa-all.bash`, and in `qa-changed.bash` for a `src-go/` change). Project rules
are `go/analysis` analyzers in `src-go/qa/rules`, run through `src-go/qa/driver` over both
modules. No manifest declaration (`defenceBeforeFix` or equivalent) exists for a Go module here;
none was added. Listing: `rules.All()` in `src-go/qa/rules/table.go`, with the catalogue table in
`CLAUDE/GoProjectRules.md`. Identifier resolution: the analyzer name printed with each finding,
documented in its own `Doc` (`go vet -vettool=<driver> help`) and in the GoProjectRules.md
section of that name. Single-rule harness: `go test ./rules -run TestRefusalKind` in
`src-go/qa`. Calibration: no recorded exceptions; none assumed.

## Defect and class

Originating instance: `src-go/internal/teams/lxcadd/adder.go` `Adder.checked`, which read every
`*declare.Error` as a wrong answer, so `ccy-teams lxc-add` asked the user three times over
"entries 5 and 6 both declare <user>@<container>", entry 5 being the member an earlier run had
written. Class: a declare.py refusal recovered by its type and never told apart by its kind.
Hazard: a refusal of the host_vars file's state is asked again as if the answer were wrong;
retyping can never pass, the run burns its tries and the person is not told the file is at fault.

The independent search was briefed as "Class A, retry on config-file state": the same hazard,
named from the loop's side. `project_refusal_kind` is that class at the place the decision is
made, the function that recovers the refusal.

Two related classes, recorded under GoProjectRules.md "Defect classes that cannot be a rule"
with what holds them: **a declare.py refusal given the wrong kind** (declare.py's half: a
file-state clash raised as 64) and **an add that refuses what is already there** (class B of the
search: `lxc-add` of a member declared as given refused as a duplicate of itself). Semgrep runs
over Python here, but neither can be judged by shape: which refusals another answer can fix, and
whether a second add is "nothing to do", are facts about meaning. `test_declare.py` holds both.

## Rule

`project_refusal_kind`, `src-go/qa/rules/refusalkind.go`. Message: "a \*declare.Error is read
without its kind: re-ask only on declare.ErrUsage, stop on the rest naming the file".
Documentation: the analyzer's `Doc` and `CLAUDE/GoProjectRules.md` "project_refusal_kind".
Width: any function (each function literal judged alone) that recovers a `*declare.Error` of
`internal/declare` by `errors.AsType`, `errors.As`, a type assertion or a type-switch case, and
neither names `declare.ErrUsage` nor hands the exit code on whole (a returned `.Code`, converted
or not, alone or as a field of a returned composite literal or its address). Naming another kind
or printing `.Code` does not clear it. Test files are not judged. Excluded as hazard-free: a
function that tells `ErrUsage` apart, or one that ends with the code. Next wider rule
considered: every retry loop around any checker, whatever the error type. Not built: most such
loops judge a pure value (an empty name, a subnet) with no file behind it.

The first draft cleared a function on any `Err*` name or any `.Code`/`.Is`; the conformance
review proved two in-hazard shapes passed (cancel on `ErrInterrupted` then re-ask on all; print
`.Code` then re-ask). The second draft cleared a function on any return that handed `.Code` on;
conformance re-review 2 proved a code handed on for an interrupt only, every other kind re-asked,
passed. Now a code handed on clears only the recovery whose block returns it unconditionally: a
direct statement of the `if` body whose Init or Cond recovers, a later direct statement of the
block whose assignment recovers, a direct statement of the type switch case. `ErrUsage` named
anywhere still clears the function. Conformance re-review 3 and Opus round 3 proved a kind filter
in the recovering `if`'s Cond passed (`ok && errors.Is(err, ErrInterrupted)`, `ok && de.Code == 130`, `errors.As(...) && errors.Is(...)`): now an `if`'s Cond clears only when it is the bare
recovery result (the `ok` its Init declares, the `errors.As` call) or that `&& de.Code != 0`, and
an `if !<bare result> {...}` guard clears with a later direct code return. Not caught now: see
"Toolchain and detector gaps".

## Proof

Red at 0ca99358c, a commit of its own before any fix: `TestRefusalKind` over
`src-go/qa/rules/testdata/src/fix/internal/badrefusal` with the stub `fix/internal/declare`; the
real tree, 4 findings through the entry point (`scripts/qa-go-qa.bash -t analyzers`, exit 1:
create/wizard.go:430, :600, :709, lxcadd/adder.go:548), reproduced by the conformance reviewer
in a detached worktree. Tightening: three new `bad.go` shapes (cancels then re-asks, prints
`.Code` then re-asks, `.Is(declare.ErrRefused)`) were red ("no diagnostic was reported" at
bad.go:67, :76, :86) before the rule changed, green after. Second tightening: the probe
`stopsOnOneKindThenReasks` is red in a commit of its own, 7e40c23a9 (`TestRefusalKind`: "no
diagnostic was reported" at bad.go:91), green at c47ad86d1 with three new hazard-free shapes in
`good.go` (a code returned after an assignment, under `ok && de.Code != 0`, from a type switch
case); the driver over both modules reports nothing there. Third tightening: `filtersInTheCondition`,
`filtersByCode`, `filtersAfterAs` (bad.go) and `guardsThenExits` (good.go) red in their own
commit, e5ec1bab1 (bad.go:119, :128, :138 not reported; good.go:97 reported); two bool-variable
filters (bad.go:148, :158) added with the rule change were red under the e5ec1bab1 rule too
(checked by running it against the new fixture); all green at 92054fb44, driver clean. Fourth
tightening (re-review 4, Opus round 4: any name the Init declared passed as the found result):
`filtersInTheInit` and `filtersByTheSecondName` red in 74d67de9d (bad.go:168, :188 not
reported), added there with two guards that already held, `filtersBesideTheInit` (bad.go, red
through the `&&` path already) and `asInTheInit` (good.go, clean); the Cond's bare name now counts only as the last name
of an Init that is one recovery and nothing else; green at the commit after, driver clean.
Owner's symptom: red at 0ca99358c by
`TestAMemberDeclaredAlreadyIsRecognisedAndTheTeamApplied` (lxcadd/declared_test.go), whose
stderr held "entries 1 and 2 both declare agent@container-a in team alpha. Try again."; the
capture is local (untracked/scratch), the test is the record.

## Independent search

`subagent-reports/261022-lxc-add-independent-search.md`, briefed with the classes and the
originating instances only, kept out of `src-go/qa/rules` and `src-go/qa/driver`. It was
committed in the defence commit; the record of its running before the rule is the file's time
(12:29:55) against the commit (12:32:21), nothing more. Text search ("Try again", "Giving up",
maxTries, callers of every declare mutator, `ErrUsage`/`ErrRefused`) found the retry loops and
which tell the kind apart; reading found what text cannot: declare.py gave file-state clashes
exit 64, so even the loops that tell the kind apart would re-ask. Every outcome:

| Candidate                                       | Outcome                                                                                                                                                                         |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A1 lxcadd `checked` (originating)               | Fixed, a0f763596; rule finding                                                                                                                                                  |
| A2 lxcadd checkout loop via `LxcDerive`         | Fixed, a0f763596: `derive_lxc` gives a clash with the file's entries 78, and `checked` stops on it                                                                              |
| A3 create checkers                              | Fixed, 2bf348eab; rule findings `checked`, `isRefusal`, `askHumans`                                                                                                             |
| A4 settings `CheckSet` loop                     | Fixed in declare.py after the reviews: a team not declared present is 78; `set` gives 78 only when the refusing rule reads no key the change sets (`_REFUSAL_READS`, 326d7e9b8) |
| A5 picker `inputDescription`                    | Not the hazard: its `Check` judges the description alone (field rules, 64), and it stops on any other kind                                                                      |
| B1 `lxc-add` of a declared member (originating) | Fixed, a0f763596: UNCHANGED, or 78 naming the keys, `--update`                                                                                                                  |
| B2 `create` of a declared team                  | Not the hazard: as asked it is UNCHANGED already; the name question is 64 (another name passes) and now names `ccy-teams apply <team>` as the way on                            |
| B3 `add-repo` of a trusted repository           | Fixed, a0f763596: UNCHANGED                                                                                                                                                     |
| B4 `drop-limit` re-run                          | Not the hazard: a removal, its only caller offers limited teams only and stops on the error                                                                                     |
| B5 `admin.py add_member`                        | Not the hazard: registry and homeserver, no interactive retry                                                                                                                   |

The search's Go instances are all caught by the rule. The tightened rule found one more real-tree
recovery, lxc-add's dry-run refusal in `summary` (adder.go:665), which stops with
`&stop{term.ExitCode(de.Code)}`: hazard-free, so the rule was taught that a code handed on inside
a returned literal is the kind read; its fixture `stopsWithItsCode` holds that.

## Sweep and count

Scope: every first-party Go package of both modules (`src-go`, `src-go/qa`), vendored code
excluded by the driver. Count: 4 at the defence commit, 1 more (hazard-free, above) with the
tightened rule. The declare.py half is not a rule (above).

## Fixes

All 4 examined individually. `lxcadd.Adder.checked`: re-asks only on `ErrUsage`, cancels on
`ErrInterrupted`, stops on `ErrRefused` naming the host_vars file, and says any other failure as
"declare.py failed (exit N)". Create: `Wizard.unanswerable` does the same and `checked`,
`askHumans` and `checkRepo` go through it; `isRefusal` is gone. No suppression.

## Decisions referred to the owner

The rule is left narrower than the hazard: each shape under "Toolchain and detector gaps" still
re-asks on a 78 and passes the rule. Closing them needs data flow (which branch re-asks, which
error a kind is read of) or meaning (a re-ask told from another early return), which a
syntactic `go/analysis` rule does not do; widening it to every retry loop would flag loops over
values with no file behind them. What would close each is given with it below. The owner decides
whether to keep the rule as it stands; the merge of this branch is held for that answer: the hand-back
to the coordinator says so, and only the owner can release it.

## Permanence

In `rules.All()` (9 rules), run by go-qa's `analyzers` stage through `scripts/qa-go-qa.bash`,
blocking in `qa-all.bash` and `qa-changed.bash`. Green: the driver over both modules reports
nothing; `qa-changed.bash` passes.

## Toolchain and detector gaps

No manifest declaration for the Go toolchain. In the detector, each carrying the hazard (a 78
asked again), listed in GoProjectRules.md "Not caught":

| Shape                                                                                                      | Why the rule misses it                                             | What would close it                                                                  |
| ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `ErrUsage` named but branched the wrong way, or re-asked on with another kind (`ErrUsage \|\| ErrRefused`) | any use of `ErrUsage` clears; which branch re-asks needs data flow | an SSA pass: the re-ask's return reached only under `errors.Is(err, ErrUsage)` alone |
| a kind read of some other error than the one recovered                                                     | error identity needs data flow                                     | the same pass, with the `errors.Is` operand tied to the recovered error              |
| a re-ask in an earlier branch before the unconditional code return                                         | the return is direct, so it clears; a re-ask is meaning, not shape | a convention: re-asks only through one helper the rule can name                      |
| a declare refusal re-wrapped in another error type before it is read                                       | the type is hidden behind the wrapper                              | `internal/declare` the only package that may wrap a `*declare.Error` (a seam rule)   |

## The original defect

declare.py: an LXC member declared as given is UNCHANGED; declared otherwise it is refused (78)
naming each differing key, `--update` replaces it (refusing to lose a comment). lxc-add: says the
member is declared already and applies after one yes; declared otherwise it offers update or
stop, and Stop names `ccy-teams apply <team>`. Tests: `tests/helpers/agent_bus/test_declare.py`
(LxcAlreadyDeclaredTest) and `src-go/internal/teams/lxcadd/declared_test.go` (the real
declare.py).

Found alongside and fixed (a different class, "a probe read as a verdict", listed under
GoProjectRules.md "cannot be a rule"): the wizard read any failing `gh api` as "no such name".
Only gh's `(HTTP 404)` is not found now; tests run no network, 401, 403 and 502. The owner lookup
no longer runs under `gum spin` (which kept gh's stderr from the verdict): it prints "Looking up
<owner> on GitHub…", as the repository check prints "Checking".
