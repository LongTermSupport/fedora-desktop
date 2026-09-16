# QA Review — Plan 00109 re-run, branch F44 (commits `50849715`, `ddd833c9`, `4273ded1`)

**Verdict**: FIX-BEFORE-MERGE — 0 blocking, 3 should-fix, 4 minor, 5 nits.

**The blocking finding is genuinely fixed and the fix is falsifiable.** What stands between
this plan and its final criterion, other than the HOST/VM runs and the owner decisions, is
finding 1: the acceptance gate written to prove the pin axis compared something passes two
reachable states where it compared nothing.

Reviewer: `qa-reviewer` (Opus 5), 2026-09-16. Read-only. Nothing in the repo was changed;
scratch artefacts were written only under `untracked/scratch/`.

## Should fix

### 1. `acceptance.bash` check [4] passes a host that compared nothing — the zero-coverage defect one level up, in the gate that vouches for the fix

`CLAUDE/Plan/00109-…/acceptance.bash:437-455`. The check greps the status document for one
literal phrase:

```bash
elif zero_line="$(grep -m1 -e '^TEXT installed-vs-pinned .*compared 0 of ' "${STATUS_PROBE}")"; then
```

Two reachable states produce zero comparisons and no such phrase, so both land in the `else`
branch and print `PASS  the pin check has a real compared population`:

**(a) A pin whose probe raised.** `check_pins.check`'s coverage guard is
`if pins and not findings and …` (`helpers/version_pins/check_pins.py:313`), so an error
finding suppresses the coverage line. Measured against the real manifest,
`registry(present=True, modules=('evdi',))`, `dkms status` raising:

```
findings: [(False, 'evdi_version: could not be checked — dkms: cannot open /var/lib/dkms: permission denied')]
section state: unavailable
```

No `compared 0 of` line exists, so check [4] passes with `(section state: unavailable)` in
its own success message and the error demoted to a `NOTE`. The run reaches `ACCEPTED`.

**(b) Partial coverage.** The fix now emits *"the installed-vs-pinned check compared 1 of 2
tracked pins…"*. `compared 0 of ` does not match it, so the check that exists to catch an
idle axis does not catch the partial case either — the exact "guard the zero case, miss the
partial" shape this plan's own blocking finding was.

This sits under a file header that reads **"COVERAGE IS STATED, NOT INFERRED … Coverage
implied by a count is this repo's named recurring defect"** (`acceptance.bash:20-24`) — and
then infers the pin axis's coverage from the absence of a phrase. The round-2 review already
asked for the number (`260916-qa-reviewer-00109-fixes-opus-5.md:132`, *"a `COVERAGE: n of m`
line would settle it"*); it was not added, and this gate is the consumer that needed it.

**Fix**: have the pins section state its coverage as a number
(`COVERAGE: n of m tracked pins compared`) and make check [4] fail unless `n == m` and
`m > 0`. A grep for a sentence is not an assertion about a population.

Related, same check and check [5]: both key on prose generated in Python (`compared 0 of `,
`could not give an answer`) with nothing coupling the two.
`helpers/host_health/login_report.py:401` currently emits the phrase check [5] wants —
reword either and the gate stops detecting, silently, for ever. The marker text belongs in
the producer as a constant the probe can echo.

### 2. `scripts/test-panel-sections.bash` derives its floor from the suites but not its suites from the directory

`scripts/test-panel-sections.bash:31-34`. `TEST_FILES` is a hardcoded two-element list.
Nothing compares it with what is on disk:

```
$ ls tests/extensions/
gi-stubs.mjs  gjs-loader.mjs  test-panel-indicator.mjs  test-panel-sections.mjs
$ grep -rn "tests/extensions" scripts/*.bash        # only this gate names them
```

A third `tests/extensions/test-*.mjs` never runs, the gate still prints `passed: 35`, and
nothing says a suite was skipped. This is the discovery defect `qa-bash.bash` had and
`qa-python.bash` still had a fortnight later — the lesson the repo wrote down in AgentNotes
and which this very commit set applied to the *count* and not to the *population*. The
comment at `:48-50` explains why a directory **argument** to `node --test` is wrong; that is
a different question from whether the list is complete.

**Fix**: enumerate `tests/extensions/test-*.mjs`, fail if any file is not in `TEST_FILES`,
and keep passing the files explicitly.

### 3. `docs/playbooks.md:777` still says "the three checks", fifteen lines below the line the same commit corrected

`50849715` changed `docs/playbooks.md:762` to *"Merges four checks"* and rewrote the play's
own header to drop the number — and left `:777`:

> **One play, two deliveries.** Only the delivery was ever profile-specific; the three
> checks are not.

This is a live claim on the user-facing page for the play this plan ships, and it is the
tenth instance of the finding that was fixed in nine places one at a time. Everything else
in that cluster is now correct (`login_report.py:3`, `health.js:7,23,133`,
`statusDocument.js:269`, the `.j2`, the play header, `PLAN.md`, `DESIGN-server-route.md`) —
verified by grep.

## Minor

### 4. `check_pins.py:307-312` claims partial coverage is reported; `not findings` means it often is not

The guard is `if pins and not findings and (tracked == 0 or compared < tracked)`. Measured,
two tracked pins, the DKMS one skipped and the rpm one drifted:

```
drifted-rpm + skipped-dkms -> ['displaylink_version (behind): pinned v6.3.0-1, installed v0.0.1-wrong']
```

Nothing states that the other tracked pin was never compared. The comment's justification —
*"a pin whose probe raised already carries a line naming the error"* — is true of the
exception path and false of the DKMS skip, which emits nothing at all. Unreachable with
today's manifest, and `test_the_real_manifest_on_a_server_reports_its_zero_coverage:409-415`
does fail loudly if a non-DKMS tracked pin is added (verified), so this is a comment that
promises more than the code delivers rather than a live hole. Stating the number
unconditionally (finding 1's fix) removes both.

### 5. `PLAN.md:321` — "The eighteen HOST items in the task tree"

I count **11** unchecked HOST/VM leaf items — `PLAN.md:85, 90, 125, 156, 163, 193, 197, 203,
257, 269, 298` — or 13 including the two parent tasks (`:153`, `:161`). Nineteen is the
acceptance gate's check count (`EXPECTED_CHECKS=(0 … 18)`, verified), which is a different
population. This is the same stale-number class as the earlier review's finding 4,
reintroduced in the sentence written to close it. Either state the count the tree supports
or drop it.

### 6. `deploy.bash` fail-fast makes a KVM-less host skip the DisplayLink deploy, and only a subagent report says so

`deploy.bash:114-118`. `play-vm-test-lab.yml` is leg 3, `play-displaylink.yml` leg 4, and
`plan_deploy_leg` aborts the run on the first failure (R7). A host without `/dev/kvm`
therefore never deploys Task 5.4's recovery tree, udev rule and suspend service — acceptance
then fails check [18] by name, which is correct, but the operator is given no hint that leg 4
is unrelated to the leg that failed. `260916-scripts-00109-opus-5.md:165-170` records the
consequence; `deploy.bash`'s help text and its abort path do not. One sentence in
`PLAN_USAGE` fixes it. (The stated reason for DisplayLink being last is right and I am not
disputing it.)

### 7. `CLAUDE/QA.md:19-20` is one gate short of what `qa-all.bash` runs

```
gates parsed from qa-all.bash: 37       (helpers.docs.link_check.qa_gates)
run verdict lines: 8 merged names + 30 separate gates
CLAUDE/QA.md:19: "runs **thirty-six** gates … the other twenty-nine run separately"
```

The table immediately beneath that lead-in has **30** rows, and `ddd833c9` rewrote that table
(including the `test-panel-sections.bash` row) without touching the count above it. The
off-by-one predates this plan — the earlier review traced it to Plan 00125 — but the file is
now in 00109's diff and the paragraph carefully derives "37 stage names for 36 gates" from a
wrong base. The row inventory is gate-checked; the prose is not.

## Nits

8. `PLAN.md:336` — "929 files"; the run now prints `✓ QA passed: 943 files checked`. A
   hand-maintained number that ages on every commit; quoting the gate's shape rather than its
   value would end it.
9. `PLAN.md:19-21` — the "46 today outside `archived/`" edit left a ragged wrap
   (`plan) are run by` / `hand, once, and then forgotten`).
10. `helpers/host_health/handoff.py:48`, its two test twins
    (`tests/helpers/host_health/test_handoff.py:217`, `test_probe_results.py:286`) and
    `DESIGN-panel.md:66` all narrate *"seven of the messages the three checks emit"*. A past
    measurement in the present tense, now four checks. Consistent with each other, so low
    risk — but it is the same sentence in four places.
11. `statusDocument.read`'s `CANCELLED` branch (`statusDocument.js:131-133`) and `_render`'s
    post-`disable()` guard (`extension.js:130-132`) are still untested. `DEFERRED_READS` now
    makes both cheap: hold a read, call `disable()`, flush. Worth taking while the harness is
    warm.
12. `test-panel-sections.bash:84` counts `^test(` only. An indented, `await`ed or
    loop-generated declaration undercounts the floor (weaker gate, silent); a
    `test('…', {skip: true}, …)` at column 0 would overcount it (false failure, loud). Both
    fail in the safe direction today — `27 + 8 = 35` matches the run exactly — but the first
    direction is the one that erodes.

## Checked and clean

- **The blocking fix is real, and the new tests do fail against the old semantics.** Ran the
  suite against three in-memory mutants of `check_pins.py` (module source mutated and
  re-exec'd; no file touched):

  | mutant | failures |
  | --- | --- |
  | old semantics — guard is `tracked == 0` only | 3: `test_PARTIAL_coverage_is_reported_too_not_just_zero`, `test_a_host_with_no_dkms_subsystem_does_not_resolve_a_dkms_pin`, `test_the_real_manifest_on_a_server_reports_its_zero_coverage` |
  | guard always fires | 7, **including the control** `test_a_host_that_DOES_compare_its_pins_gets_no_coverage_finding` |
  | `not findings` dropped | 3 (the three probe-failure tests) |

  So the control is not decorative — it dies on the "fires whatever the coverage was" mutant,
  which is the only mutant it exists to catch. The old test that enshrined the defect
  (`check(… registry=NO_SUBSYSTEM) == []`) is gone, replaced at `test_check_pins.py:391-398`
  by one asserting `0 of 1` and `no DKMS subsystem`.
- **The fix reaches the production consumer.** `login_report.py:447-457` passes both
  `registry=probe.dkms_registry()` and `ran_plays=plays_run_here(base)`; the standalone
  `check_pins.main()` passes neither, which is correct — with no registry the DKMS skip never
  fires and an absent `dkms` becomes a reported `unchecked` finding rather than silence.
- **`test-panel-indicator.mjs` genuinely falsifies the shipped `extension.js`.** Six mutants
  injected through a chained ESM loader against the real file (nothing written to disk);
  **all six died**, one test each: initial icon `unavailable → ok`; drop `menu.removeAll()`;
  `if (document === null)` → `if (false)`; `unavailable` colour → `''`; `disable()` leaking
  the poll timer; `disable()` leaking the indicator; and the first `_render(null)` removed.
- **The `node --test` premise behind the derived floor is measured, not assumed**:
  `node --test /dev/null` (v24.21.0) reports `tests 1 / pass 1 / fail 0`. With the fix, an
  emptied suite is caught by the per-file `declared -eq 0` branch before the total is
  compared.
- **Stub faithfulness.** The stubs are recorders, not re-implementations, and the pass means
  something narrow and honestly scoped: the shipped `extension.js` and `statusDocument.js`
  are what is imported, `gjs-loader.mjs` throws on an unknown `gi://` specifier, and the one
  place the stub deliberately diverges — the synchronous `load_contents_async` — is now
  switchable via `DEFERRED_READS` and is documented in place (`gi-stubs.mjs:193-216,
  236-245`). `load_contents_finish` returning 2 elements where real GJS returns 3 is
  invisible to the only consumer (`statusDocument.js:121` destructures two).
  `PanelMenu.Button`, `Main.panel.addToStatusArea`, `Extension(metadata)` and `St.Icon`'s
  `icon_name` assignment all match the shapes the extension actually uses. What a pass does
  **not** establish — St layout, legibility, whether the icon reads correctly — is stated in
  the script header, in the suite header and in `PLAN.md:257-259`, and the `HOST` item is
  still open. No overclaim.
- **`DECISIONS.md` extraction is byte-faithful.** Diffed the removed `PLAN.md` block against
  the new file, normalising heading level and blank lines: Decisions 1 and 2 are identical,
  the only additions are the file preamble and Decision 3. Nothing lost or altered. Decision
  3 is recorded OPEN as the owner's, not re-raised.
- **`deploy.bash` against R1–R14.** R1 bootstrap verbatim and `.git`-bounded; R2
  `plan_require_host`; R3 `plan_prime_sudo` **before** `plan_start_log`; R4
  `plan_start_log auto`, no hand-rolled tee or EXIT trap; R5 no bare `read`; R6
  `plan_ansible_playbook` with repo-relative paths; R7 `plan_mode deploy` + four bare
  top-level `plan_deploy_leg`s; R8 one `plan_gate_change` before the first leg, naming every
  state change including the MOK/reboot risk; R9 renders no verdict; R12 exec bit set,
  `bash -n` and `shellcheck -x -S warning` clean. `--check` is refused with a measured reason
  rather than passed through.
- **`acceptance.bash` against R1–R14.** Same bootstrap; `plan_require_host`;
  `plan_mode gather`; no `plan_gate_change` (correct — R8 forbids gating a read-only run);
  reports in `PLAN_RUN_DIR` (R10); no `|| true`, no `set +e`, no silencing `2>/dev/null`
  (R11); `bash -n` and `shellcheck -x -S warning` clean. The two deviations are annotated in
  the file with their reasons: `plan_finish` is not the closer because the gate has a third
  answer, and `read_field` assigns rather than prints so that `abort`'s `exit 2` runs in the
  caller's shell rather than a command substitution. The one ansible invocation is through
  the wrapper (`:583`) and is `--list-tasks`, which applies nothing. `EXPECTED_CHECKS` is
  stated and a missing check is NAMED and REJECTED even at zero failures — correct on the
  axis that matters most, which is why finding 1 is worth fixing rather than a nit.
- **Every round-2 finding actioned**, verified in the current tree: the `count -gt 0` guard
  replaced by a derived floor; the `document === null` and `menu.removeAll()` paths now
  tested; the docstring's test twin corrected; the two `DECISIONS.md` cross-references;
  `store.clear_broken(base, *, at: str)` now required with a `ValueError` on empty
  (`store.py:101,117`); `CLAUDE/QA.md:62` now says "two suites"; `statusDocument.js:269`
  reworded; `PLAN.md:336` "every gate green" replaced by the true sentence naming the three
  standing advisories.
- **Journal ordering (earlier finding 9) is resolved as a decision, not left dangling.**
  `CLAUDE/PlanJournalling.md:80-88` now settles the contradiction explicitly — this repo's
  append-only rule beats the daemon's "move the entry" remediation, and the advisory is left
  standing deliberately. The sweep still reports the three files; that is now the documented,
  chosen state.
- **No CCY obligation.** No path under `files/var/local/claude-yolo/**` appears anywhere in
  the three commits, so no `CCY_VERSION`, Dockerfile LABEL or `REQUIRED_CONTAINER_VERSION`
  bump is owed.
- **Public-repo safety.** Scanned `deploy.bash`, `acceptance.bash`, `DECISIONS.md`,
  `PLAN.md`, `test-panel-indicator.mjs`, `gi-stubs.mjs`, `test-panel-sections.bash` and
  `check_pins.py` for home paths, emails, private IPs and `.local` hostnames: no matches.
  Every host path in the two plan scripts is resolved at runtime from `$HOME`,
  `PLAN_REPO_ROOT` or the production helpers; the only literals are FHS system paths.
- **Plan Commit Rule.** No 00109 file has changed since `4273ded1`
  (`git log 4273ded1..HEAD -- <plan and code paths>` is empty), and the plan file moved with
  the code in all three commits. The three modified files in `git status` belong to Plan
  00099, a concurrent session, not to this review.

## Mechanical gates

- `./scripts/qa-all.bash`: **PASS**, exit 0, 943 files.
  `helper-tests: Ran 1562 tests in 66 modules (66 tracked), 1 skipped`;
  `panel-sections: passed: 35`;
  `panel-contract: 9 constant(s) … 8 document key(s) … 4 section id(s)`;
  `version-pins: 9 pin(s), 1 tracked, 8 untracked — COVERAGE: 9 of 9`. Three standing
  advisories, exactly as `PLAN.md:336` now describes them: shellcheck informational, semgrep
  partial parses, `deployed-drift` skipped in the container.
- `hooks-daemon plan-qa --sweep`: **exit 1**, repo-wide. 00109's three entries are the
  journal-ordering advisories that `CLAUDE/PlanJournalling.md` now deliberately accepts. No
  00109 block.
- `ansible-playbook --syntax-check`: **PASS** on `play-host-health-login-report.yml`,
  `play-fedora-desktop-panel.yml`, `play-vm-test-lab.yml`, `play-displaylink.yml` and
  `playbook-main.yml`.
- Conditional gates, all triggered by this diff and all run: `qa-helper-tests.bash` **PASS**
  (1562/1 skipped); `python3 -m helpers.gnome.check_extension_compat` **PASS** (5/5);
  `cd extensions && node_modules/.bin/eslint .` **PASS**; `bash -n` +
  `shellcheck -x -S warning` on both new plan scripts **clean**.
- Not run, and not triggered: nothing. No Ansible playbook was executed — this is a CCY
  container.

## Answer to the closing question

**Yes — something other than the HOST/VM runs and the owner decisions stands between this
plan and its final criterion.** Finding 1 is container-actionable and is a defect in the
artefact that will be used to tick the HOST items: `acceptance.bash` check [4] can print
`PASS` and reach `ACCEPTED` on a host where the installed-vs-pinned axis compared nothing.
Findings 2 and 3 are also container-actionable and cheap. The blocking finding from the
full-plan-diff review is genuinely resolved, its fix is falsifiable in both directions, and
every other container-actionable finding from both prior rounds is closed. Everything still
marked HOST, VM, owner's-call, Decision 3, Task 0.3 and T5.4a was left alone, as instructed.
