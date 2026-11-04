# DBF conformance review: project_hidden_bindings over Bubble Tea (Plan 00204 Task 2.3)

Commits: red 727810c8, green 70c96773, fix-branch tip 90c02eed (fix/lxc-checkout-typed).
Each commit was checked out in its own detached worktree, since removed. Nothing in the branch was changed.

## Verdict

**Conforms, with no blocking finding.** The rule exists, was proven red before it was written and green after, and sweeps clean. The first real Bubble Tea screen, `internal/ui/pathinput` at 90c02eed, passes it.

Four gaps are medium or low severity (see Findings). Two things I could not check:

- **The method specification (sections 3, 4, 7, 8) was not given a path and I could not find it in the tree.** I judged against the checklist in my brief and the project's own rule text in `CLAUDE/GoProjectRules.md`. None of the findings cites a specification clause. A reviewer with the specification should confirm them against it.
- **`scripts/qa-go.bash` exits 1 on this host at the green commit for a reason unrelated to the rule** (see Reproduction).

## Reproduction

The rule is a `go/analysis` analyzer run by `scripts/qa-go.bash`. `scripts/qa-hidden-bindings.bash` covers shell fzf and Python Textual and has no Bubble Tea part.

- **Red, 727810c8, `scripts/qa-go.bash`: exit 1, as required.**
  - `TestHiddenBindings` fails with 10 unmet wants: `fix/internal/ui/teakeys.go` lines 12, 13, 18, 32, 34, 40; `fix/internal/badtea/bad.go` lines 14, 15, 19, 26.
  - The commit holds only fixtures, stubs of Bubble Tea and Bubbles key, the `rules_test.go` change and the search report. No rule code is in it.
  - The red proves the fixtures demand the rule. It cannot show the real tree carries a hazard, because the tree has none.
- **Green, 70c96773, `scripts/qa-go.bash`: exit 1, but not because of the rule.** The one failing test is `TestRealSemgrepScansTestsDirectoriesAndLargeScripts` in `qa/cmd/qa-patterns`: a semgrep Timeout on `scripts/big.bash`.
  - It fails identically at the red commit, and again when run alone (about 20 s).
  - It is a host or toolchain problem, not a defect of this work. Reported, not worked around.
  - `qa-go.bash` stops at its first failing step, so the "project rules" vet steps never ran inside that invocation. I ran them by hand, below.
- **Green steps run by hand at 70c96773:**
  - Driver build, then `go vet -vettool` over `src-go`: exit 0. The same over the `qa` module: exit 0.
  - `go test ./rules`: ok, so `TestHiddenBindings` passes.
  - `scripts/qa-hidden-bindings.bash`: exit 0, "HIDDEN-BINDINGS-OK 446 shell file(s), 340 Python file(s)".
- **Tip, 90c02eed, by hand:** the driver builds and `go vet -vettool` is clean over both modules. So the rule is active and passes on the real pathinput code. `qa-go.bash` itself was not re-run at the tip; the semgrep failure would recur.

Findings 1 and 5 rest on reproduction or on reading the rule code. Findings 2 to 4 rest on the commits and reports alone.

## What I checked

- **Rule exists, scope.** `qa/rules/teakeys.go`, called from `runHiddenBindings`.
  - Three clauses, as in the brief: `key.NewBinding` without a constant, non-empty `key.WithHelp`; a `KeyMsg`, `KeyPressMsg`, `KeyReleaseMsg` or `Key` named outside `internal/ui` and its subpackages; a hand-compare of `String`, `Keystroke`, `Code` or `Text` against a constant unless line editing or navigation.
  - Matching goes by type object, so an import alias is covered.
  - Tab is in `lineKeys` and `lineCodes`. The owner's decision and the Tab-needs-a-hint condition are recorded in the catalogue.
  - `pathinput` has a "Tab completes a directory name" binding in its hint line, and `pathinput_test.go:112` asserts "Tab completes" is in the view. The rule itself does not check that the hint exists; the catalogue says "ui's tests hold that", which is honest.
- **Red proof is its own commit, ahead of the green.** Yes: 727810c8 then 70c96773.
- **Reached the real tree.** `pathinput` matches through `key.Matches` against five bindings with help, and compares `k.String() == "up"` for navigation only. Judged by the rule and passes.
- **Permanent and blocking.** It runs in `qa-go.bash` as the "project rules" hard step over both modules. Stable id `project_hidden_bindings`; `go vet -vettool=<driver> help project_hidden_bindings` shows the extended doc string; the catalogue in `CLAUDE/GoProjectRules.md` ships in the repo. The catalogue, `src-go/CLAUDE.md` and Plan 00204 were updated in the green commit.
- **Authority and instances.** Zero instances, so no fixing, referral, suppression or baseline. The rule was not weakened and `table.go` is untouched.
- **Ordering.** The rule (727810c8, 70c96773) landed before the first Bubble Tea key handler (90c02eed), as Task 2.3 required. "Original defect fixed last" does not apply: the hazard had not occurred.
- **Toolchain gap.** The semgrep timeout is reported above, not worked around.

## Findings, most severe first

1. **The rule has evasions with no red test (medium).**
   - **Evidence:** `teakeys.go` judges only the `key.NewBinding` call. `isKeyRead` accepts only a direct call or field selector on a key-typed value.
   - **Not caught:** a hand-compare through an intermediate (`s := msg.String(); if s == "ctrl+x"`); a `key.Binding{...}` struct literal, or `SetHelp` and `SetKeys` after construction.
   - **Fixtures:** `testdata/src/fix/internal/ui/teakeys.go` and `badtea/bad.go` hold none of these. The catalogue's own opening rule (CLAUDE/GoProjectRules.md lines 11 to 13) says a rule is also red-tested against the ways round it. I read this from the code; I did not run a probe.
   - **Resolve:** add red fixtures for the indirection and struct-literal forms and close them, or list them under "Not caught". The catalogue lists only "a Bubble Tea key read through a type the rule does not list", which does not describe either case.
2. **Sweep count and scope not stated in one place; next wider rule not named (low to medium).**
   - **Evidence:** the catalogue says the sweep "over both modules found none". The search report gives its own scope (all `*.go` under the worktree, excluding `src-go/vendor/`, `src-go/qa/rules/`, and a non-existent `internal/fzfbind`). The two are not reconciled.
   - The exclusions are defensible: the rules directory holds the rule and fixtures only, and vendored code is not first-party.
   - **Resolve:** one sentence giving count and scope, and a named next wider rule not built with its reason. The obvious one is a check that the single help or menu component in `internal/ui` renders every `key.Binding`, which the plan task asked for and the rule leaves to ui's tests.
3. **The independent search is thin as a record (low).**
   - **Evidence:** `00204-go-tui-polish/subagent-reports/261016-dbf-bubbletea-keys-search-opus.md` says "text search found everything; reading only confirmed...". That is one technique plus a confirming read, not two techniques each recorded for what the other could not check.
   - **Ordering:** it is committed in the same commit as the red fixtures, so the history cannot show it was made before, or without sight of, the rule. Only the commit message asserts that.
   - **Reconciliation:** the sweep's 0 matches the search's 0, which is consistent but adds no evidence that the rule reaches what a search would find.
   - **Resolve:** record a second technique (for example a type-based listing of key-type uses) and what each could and could not see, or state that with zero instances neither could be tested.
4. **No filled report template (low).** There is no DBF report for this remediation, only the search report, a catalogue paragraph and the plan checkbox. Missing: the examined-versus-by-pattern statement (trivially none), the referral section (nothing to refer), and the toolchain-gap line for the semgrep timeout.
5. **`qa-go.bash` cannot go green on this host (informational).** The semgrep timeout fails it at red, green and presumably the tip. A reviewer on another host should confirm the full entry point passes.

## Not found

- No suppression, baseline or allowlist row added.
- The red commit does not contain the rule; the green commit does not touch the fixtures.
- Nothing outside `internal/ui` reads a Bubble Tea key at the tip (vet clean).
