# Handoff 1: fix/lxc-add-already-declared (Plan 00201)

Copy of the first worker's handoff, kept with the plan.

Branch fix/lxc-add-already-declared, branched from F44 67033c224. Tip at handoff a0f763596.

## Commits so far

1. 64d5bc3b6: m7_check.py expect_list_row (acceptance D1) reads the TEAM/MEMBER/KIND/STATE
   offsets from the heading line, exact match; red then green against rows recorded from
   board.Layout(rows, 0) (test_m7_check.ListRowTest.MEASURED).
2. 0ca99358c: DBF defence RED: new analyzer project_refusal_kind
   (src-go/qa/rules/refusalkind.go, fixture testdata/src/fix/internal/{declare,badrefusal},
   TestRefusalKind, All() now 9 rules; catalogue row and section in CLAUDE/GoProjectRules.md).
   Real tree red: 4 findings (lxcadd Adder.checked; create wizard.go:430 isRefusal, :600
   askHumans, :709 Wizard.checked). Independent search report:
   subagent-reports/261022-lxc-add-independent-search.md.
3. a0f763596: the fix for lxc-add and declare.py, CCY 3.99.14.13, container 2.78.16,
   docs/ccy-changelog.md entry, meta-deploy.list descriptions updated.
   - declare.py: \_judge_lxc (alone -> 64, among file / file itself -> 78), lxc-check replaces
     the member's own entry, lxc-add identical -> UNCHANGED, differs -> 78 naming keys,
     --update (\_replace_lxc), new lxc-compare (DECLARED/DIFFERS lines), derive_lxc split the
     same way, add-repo already-trusted -> UNCHANGED, set_values: entry refused as it stands
     for an untouched key -> 78.
   - Go declare client: LxcCompare, LxcUpdate, LxcAddDryRun(ctx, e, update).
   - lxcadd: compare after questions; identical -> confirmApply then apply (stdout UNCHANGED);
     differs -> list keys, Choose update/stop; self coordinator gets keepChoice not takeover;
     checked() re-asks only on ErrUsage, else stops naming the host_vars file.
   - Tests: tests/helpers/agent_bus/test_declare.py (LxcAlreadyDeclaredTest etc.);
     src-go/internal/teams/lxcadd/declared_test.go (real declare.py via realDeclare).
   - RED proof of owner output (at 0ca99358c with the first test): stderr had
     "agent_bus_lxc_members entries 1 and 2 both declare agent@container-a in team alpha. Try
     again."

## Left to do

1. create wizard (src-go/internal/teams/create/wizard.go): Wizard.checked re-asks only on
   declare.ErrUsage, otherwise stops naming the host_vars file; isRefusal -> errors.Is(err,
   declare.ErrUsage); askHumans: a non-ErrUsage refusal ends the run. Tests first. Then the
   driver over both modules must be clean.
2. declare.py create "--team ... already declared": optionally add the way on ("ccy-teams apply
   <team>") to the message (near-miss).
3. create.checkOwner/checkRepo read any gh exit as "not found" (environment failure read as an
   input verdict; gh prints "(HTTP 404)" for not found). The coordinator later asked for this
   to be fixed, not only recorded.
4. PLAN.md and journal entry, with red/green evidence.
5. GoProjectRules.md project_refusal_kind section: check it is true after step 1.
6. ./scripts/qa-changed.bash, go tests; never qa-all.
7. Reviews: conformance-reviewer, then adversarial Opus loop over `git diff F44...HEAD` until
   MERGE; each round saved as subagent-reports/261022-lxc-add-review-roundN.md.
