# Defence Before Fix report: project_refusal_kind (Plan 00201 Task 2.20)

Branch `fix/lxc-add-already-declared`. Defence commit 0ca99358c (red). Fix commits a0f763596
(lxc-add, declare.py) and 2bf348eab (create wizard, gh lookups). F44 merged in at 11b48c687.

## Specification followed

Method specification 1.1.0, cached copy (`/root/.cache/defence-before-fix/spec/SPEC.md`), as
printed by the skill's refresh script; detector specification 1.0.0 and toolchain specification
0.2.0, cached.

## Toolchain

Go: go-qa-ci's `go-qa`, pinned by the `tool` directive of `src-go/qa/go.mod`, configured by
`src-go/qa/go-qa.toml`, entry point `scripts/qa-go-qa.bash` (in `qa-all.bash`, and in
`qa-changed.bash` for a `src-go/` change). Project rules are `go/analysis` analyzers in
`src-go/qa/rules`, run by go-qa's vet step through `src-go/qa/driver` over both modules. No
manifest declaration (`defenceBeforeFix` or equivalent) exists for a Go module here; none was
added. Listing: `rules.All()` in `src-go/qa/rules/table.go`, with the catalogue table in
`CLAUDE/GoProjectRules.md`. Identifier resolution: the analyzer name printed with each finding,
documented in its own `Doc` and in the GoProjectRules.md section of that name. Single-rule
harness: `go test ./rules -run TestRefusalKind` in `src-go/qa` (analysistest over the
fixture). Calibration: no recorded exceptions; none assumed.

## Defect and class

Originating instance: `src-go/internal/teams/lxcadd/adder.go` `Adder.checked`, which read every
`*declare.Error` as a wrong answer, so `ccy-teams lxc-add` asked the user three times over
"entries 5 and 6 both declare <user>@<container>", entry 5 being the member an earlier run had
written. Class: a declare.py refusal recovered by its type and never by its kind. Hazard: a
refusal of the host_vars file's state is asked again as if the answer were wrong; retyping can
never pass, the run burns its tries and the person is not told the file is at fault.

The other half of the class is declare.py's: a refusal must carry the right kind. `check_lxc` and
`derive_lxc` re-raised a clash with an existing entry as a usage error (64). That half is held by
`tests/helpers/agent_bus/test_declare.py` (no Python rule gate covers `helpers/`).

## Rule

`project_refusal_kind`, `src-go/qa/rules/refusalkind.go`. Message: "a \*declare.Error is read
without its kind: re-ask only on declare.ErrUsage, stop on the rest naming the file".
Documentation: the analyzer's `Doc` and `CLAUDE/GoProjectRules.md` "project_refusal_kind".
Width: any function (each function literal judged alone) that recovers a `*declare.Error` of
`internal/declare` by `errors.AsType`, `errors.As`, a type assertion or a type-switch case, and
never reads its kind (no `errors.Is` against a `declare.Err*` variable, no `.Is` call, no
`.Code`). Test files are not judged. Excluded as hazard-free: a function that reads the kind.
Next wider rule considered: every retry loop around any checker, whatever the error type. Not
built: most such loops judge a pure value (an empty name, a subnet) with no file behind it, and
the rule would match code without the hazard.

Not caught (recorded in the docs): a kind read of some other error in the same function, and a
declare refusal re-wrapped in another error type before it is read.

## Proof

Red at 0ca99358c, a commit of its own before any fix: `TestRefusalKind` over the fixture
`src-go/qa/rules/testdata/src/fix/internal/badrefusal` (bad.go: lxc-add's original `checked`,
the wizard's `isRefusal`, a type assertion, a type switch, a checker literal, a kind read of a
different error; good.go: clean forms) with the stub `fix/internal/declare`; and the real tree,
4 findings: `lxcadd.Adder.checked`, create `Wizard.checked`, `isRefusal`, `askHumans`.

## Independent search

`subagent-reports/261022-lxc-add-independent-search.md`, dispatched before the rule with the
class and hazard only, kept out of `src-go/qa/rules` and `src-go/qa/driver`. Text search
("Try again", "Giving up", maxTries, callers of every declare mutator, `ErrUsage`/`ErrRefused`)
found the retry loops and which discriminate the kind; reading found what text cannot: that
declare.py gave file-state clashes exit 64, so even the discriminating loops (`picker/offers.go`,
`settings/settings.go`) would re-ask; that fixed in declare.py, not in the rule. The search's Go
instances (lxcadd checked, create checked) are all caught by the rule; the rule also found
create's `isRefusal` and `askHumans`, which the search listed as lesser. No widening was needed.

## Sweep and count

Scope: every first-party Go package of both modules (`src-go`, `src-go/qa`), vendored code
excluded by the driver; the class is Go-only on the reading side (the Python side is held by
tests). Count: 4.

## Fixes

All 4 examined individually. `lxcadd.Adder.checked` (a0f763596): re-asks only on `ErrUsage`,
cancels on `ErrInterrupted`, otherwise stops naming the host_vars file. Create (2bf348eab): one
`Wizard.unanswerable` does the same and `checked`, `askHumans` and `checkRepo` go through it;
`isRefusal` is gone. No suppression; the rule is unchanged.

## Decisions referred to the owner

None.

## Permanence

In `rules.All()` (9 rules), run by go-qa's vet step through `scripts/qa-go-qa.bash`, which is
blocking in `qa-all.bash` and `qa-changed.bash`. Green: the driver over both modules reports
nothing at 2bf348eab; `qa-changed.bash` passed (go-qa both modules, the ccy-teams suite).

## Toolchain and detector gaps

No manifest declaration for the Go toolchain (the toolchain spec's clause 9.2 form is not used
by this project for Go). No Python rule gate over `helpers/`, so declare.py's half of the class
is held by tests, not a rule.

## The original defect

declare.py: an LXC member declared as given is UNCHANGED; declared otherwise it is refused (78)
naming each differing key, `--update` replaces it. lxc-add: says the member is declared already
and applies after one yes, offers update or stop when it differs. Tests:
`tests/helpers/agent_bus/test_declare.py` (LxcAlreadyDeclaredTest) and
`src-go/internal/teams/lxcadd/declared_test.go` (the real declare.py). The owner's output was
reproduced red at 0ca99358c ("entries 1 and 2 both declare agent@container-a in team alpha. Try
again.") and is green after.

Found alongside and fixed (a different class, "a probe read as a verdict", listed under
GoProjectRules.md "cannot be a rule"): the wizard read any failing `gh api` as "no such name".
Only gh's `(HTTP 404)` is not found now; tests run no network, 401, 403 and 502.
