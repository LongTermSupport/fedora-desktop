# QA Review — Plan 00125, `cedc9426..5ac5f57f` (+ plan state at `41b42564`)

**Verdict**: FIX-BEFORE-MERGE

`d4826f90` and `41b42564` landed during this review. Reviewed: the four requested commits,
plus the current on-disk `PLAN.md` / `FINDINGS.md`.

## Blocking

### 1. `helper-tests` reports the same verdict line whether the DisplayLink pair ran or skipped

`scripts/qa-all.bash:155`

Measured, at the same commit, against CI run `35038606426`:

```
helper-tests here: Verdict(symbol='+', detail='Ran 1446 tests')
helper-tests ci  : Verdict(symbol='+', detail='Ran 1446 tests')
```

Byte-identical, so `verdicts.compare` returns `AGREE`. But here `card1-eDP-1` is connected, so
`TestEdidByteCountAgainstRealSysfs` ran its assertion against real sysfs (local
`OK (skipped=1)`, the root/chmod case); on the runner the only connector is `Virtual`, now
denylisted, so both sysfs tests skipped and the root/chmod case ran instead. Two different
populations, one identical line.

The cause is `qa-all.bash:155`: `helper_summary=$(... grep -oE 'Ran [0-9]+ tests?')`.
`unittest` counts skips inside `testsRun`, and the `OK (skipped=N)` line is discarded.
Measured on the CI log: `grep -ic skipped` = **1**, and that one is `deployed-drift`'s.

Live consequences:

- `PLAN.md:203` Success Criterion *"Local `qa-all.bash` and the CI run agree on every stage"*
  is ticked on a measurement that cannot see the divergence it exists to catch.
- `41b42564`'s *"helper-tests now agree exactly"* is not supported — they agree on the line,
  not on what ran.
- This is the plan's own subject reproduced inside the plan's own instrument: a check whose
  clean result is indistinguishable from a blind one.

**Fix**: emit the skip count in the stage line — `+ helper-tests: Ran 1446 tests, 1 skipped`
(or a `COVERAGE:` line from `qa-helper-tests.bash`). The container then reports `1 skipped`,
the runner `2 skipped`, and `verdicts.py` flags `differs` unprompted.

## Should fix

### 2. `verdicts.py` guards the empty case and is silent on the partial one

`helpers/qa_environment/verdicts.py:219-229`

The zero-stage guard is present and well argued. Nothing guards a *partial* parse. Measured,
by truncating the CI capture:

```
CI capture truncated: parsed 4 stages; 32 rows reported as only-here
['ansible', 'ansible-syntax', 'ccy-gpu-device', ... 'vmtest-reboot-dispatch']
```

`triage.bash:92-95` then instructs the reader: *"only-there ... this machine did not run that
stage AT ALL ... usually an earlier gate failing here and hiding every gate declared after
it."* A broken capture therefore renders as a confident, plausible, wrong finding about gate
coverage — the exact class this plan exists to remove, and the
`AgentNotes` "guard the empty case, miss the partial" shape.

**Fix**: print `COVERAGE: parsed n of m symbol-prefixed lines` per side — the idiom
`qa-version-pins` already uses (`COVERAGE: 9 of 9`).

Two concrete under-matches in the same parser, both measured:

- `CI_LOG_PREFIX`'s greedy `(?:[^\t]*\t)+` does not distinguish a harness prefix from a tab in
  the *payload*: `verdicts.parse("+ vmtest-manifest: scenarios=8\trunnable=8")` returns `{}`.
  A stage line containing a tab is silently deleted.
- `patterns` is emitted twice locally (`~ patterns: 15 file(s) ... parsed only in part`, then
  `+ patterns: 260 files OK`). "Last wins" discards the advisory. A machine where semgrep
  parses everything and one where it parses 15 files in part both collapse to the pass line
  and read as `agree`.

### 3. `assert_on_host`'s default marker paths are now exercised by nothing

`files/home/.local/lib/freeze/freeze-common.bash:172-173`,
`scripts/test-freezelib.bash:917,926,933,943`

All four new cases set both `FREEZE_CONTAINERENV_PATH` and `FREEZE_DOCKERENV_PATH`, so
`${FREEZE_CONTAINERENV_PATH:-/run/.containerenv}` never evaluates its default. The test it
replaced did drive the real defaults — that was the whole "the suite happens to be in a
container" mechanism. Net trade: the predicate gained coverage in both directions, the
production default values lost all of it. A typo in either path ships green and the guard
stops refusing containers. The falsification table in the journal (23:52) mutates the
*condition*, never the defaults, so it does not cover this.

**Fix**: add a machine-independent case pinning the two literals, e.g.
`contains "the podman default marker" "$(declare -f assert_on_host)" "/run/.containerenv"`.

### 4. The seam is an ambient environment variable on a safety guard — and the journal claims the repo's existing answer was adopted

`CLAUDE/Plan/00125-.../JOURNAL/00125-Journal-26-09-15.md:475-480`

The journal's 00:02 entry says: *"`assert_on_host` hardcoded its container markers, while
`_plan_in_container` ... already takes them as arguments and is tested in both directions ...
in both cases the fix was to adopt the existing answer."* For `qa-js.bash` that is true. For
`assert_on_host` it is not — `_plan_in_container` (`CLAUDE/Plan/_planlib.inc.bash:252-265`,
*"Markers are arguments rather than hardcoded so BOTH branches are testable"*) takes
arguments; the new code takes exported variables.

The difference is load-bearing: an argument cannot arrive from a parent shell, an exported
`FREEZE_DOCKERENV_PATH` can, and it silently loosens a guard whose entire job is to refuse.
`podfreeze:879` and `lxcfreeze:735` both call it with no arguments, so
`assert_on_host "${@:-...}"` with the defaults inside the function is a drop-in.

**Fix**: adopt arguments, or correct the journal claim in a new (append-only) entry.

### 5. "26 gates are declared after that point" is 25

`PLAN.md:168`, `FINDINGS.md:82`, journal 23:38 and 23:58, commit messages `05cf9b53` and
`5ac5f57f`

`grep -n "QA FAILED" scripts/qa-all.bash` after the helper-tests abort at line 152 gives
aborts at 168, 185, 203, 226, 244, 260, 273, 288, 302, 316, 331, 345, 360, 372, 387, 402,
420, 437, 455, 470, 486, 496, 514, 530, 544 — **25**. The 26th match is line 581,
`QA FAILED: $NERRORS errors in $TOTAL files`, which is the run's closing structural summary
over the jq-merged stages that run *before* line 152, not a gate declared after it. This looks
like a `grep -c` past line 152 that swept in the summary.
`AgentNotes` -> *"Measured is a claim with a scope, and the scope is usually wrong."*
The journal is append-only, so correct it in a new entry.

### 6. The skip reason is named in code and invisible everywhere the gate is read

`tests/helpers/displaylink_recovery/test_run_recovery.py:480-485`, `CLAUDE/QA.md` new table row 4

The helper's docstring says *"naming what was excluded so the skip is never silent"* and
`CLAUDE/QA.md` says *"it skips where no connector with a physical display link is present,
naming what it ignored"*. `unittest` prints skip reasons only at verbosity >= 2;
`qa-helper-tests.bash` runs at the default. Measured here:
`python3 -m unittest tests.helpers.displaylink_recovery.test_run_recovery` prints
`OK (skipped=1)` and no reason; `qa-all.bash` then discards even that. The string is reachable
only by someone who already suspects and re-runs with `-v`.

**Fix**: make it reachable (run the suite `-v`, or emit a marker line), or drop the claim from
`QA.md`.

### 7. On the HOST, `qa-all.bash` is now red and truncated until the freeze play runs

`40e3a26d` message, `PLAN.md:161-162`

`qa-deployed-drift.bash:219` covers `files/home/.local/lib/freeze/*`, and its abort is
`qa-all.bash:129` — before helper-tests. So on the host the suite now aborts 27 gates earlier
than it did, which is precisely the Task 4.1 mechanism this plan documents. The deploy
instruction is present and correct (`tasks/deploy-freeze-lib.yml` exists); the framing
("behaviour is identical, so nothing is broken meanwhile") understates it, and `CLAUDE.md`
names local `qa-all.bash` the pre-commit requirement.

## Nits

- **`.mjs` is outside `qa-js.bash`'s population.** `scripts/qa-js.bash:60` is `-name "*.js"`;
  `tests/extensions/{gi-stubs,gjs-loader,test-panel-sections}.mjs` get no `node --check` and
  sit outside the `extensions/` ESLint project. They are executed by the `panel-sections`
  gate, so they are not unchecked — but `+ js: 8 files OK` is narrower than "repo-owned
  JavaScript", the same under-match shape the commit just fixed six lines above.
- **`CLAUDE/Plan/README.md:37`** still describes 00125 as "five helper tests"; it is six.
- **R2 deviation.** `PlanScriptStandards.md:101` says *"Pick exactly one of the two"* and
  carves out scripts *"whose findings do not depend on where it ran"*. `triage.bash`'s
  findings very much depend on where it ran — a genuine third case, not the carve-out. The
  reasoning is sound and stated in the header; the paraphrase in `PLAN.md:77-79` just reads R2
  as softer than it is.

## Checked and clean

- **Was any test weakened? (the plan's stated risk)** — No, for the DisplayLink pair.
  `connector_type` is correct against `card10-DP-1` -> `DP`, `card0-HDMI-A-1` -> `HDMI-A`,
  `card1-eDP-1` -> `eDP`, and a test pins that production's own connectors (`card*-DVI-I-*`)
  are not denylisted. The pair is not vacuous here: `card1-eDP-1` is connected and the
  assertion ran. It goes vacuous only on a `Virtual`/`Writeback`-only VM — a real coverage
  loss on the runner, but declared, and denylist-not-allowlist is the right call. The
  *visibility* of that loss is findings 1 and 6, not the denylist itself.
- **Did the dbus injection lose coverage?** — No. `apply_enabled_extensions.py:113` calls
  `session_bus.current()`, so patching it is the production seam, not a bypass. The
  `dbus-run-session` branch is covered at both levels:
  `test_falls_back_to_dbus_run_session_with_no_bus_at_all` (resolver) and the new
  `test_the_uid_derived_runtime_dir_is_consulted_with_a_bare_environment` (`current()`,
  checking `source == "dbus-run-session"`). Your claim checks out. The new
  `assertNotIn("DBUS_SESSION_BUS_ADDRESS", seen["env"])` is environment-independent because
  `_run` patches `os.environ` with `clear=True` and `os` is one module object.
- **`qa-js.bash` widening is a pure subtraction.** Set-diffed rather than size-compared, per
  the `AgentNotes` rule: exactly two files leave, both under
  `.ansible/collections/ansible_collections/community/general/tests/integration/targets/keycloak_authz_custom_policy/policy/`.
  No repo-owned file lost. 8 on both machines, confirmed against the CI log.
- **The `/workspace` fix was generalised.** Swept every tracked non-doc file: the survivors are
  the CCY container's own mount point (`entrypoint.sh`, `common-pure.bash`, Dockerfile
  examples) and hook-handler test fixtures. No other gate-executed file hardcodes it.
- **Parser coverage on a real capture.** 38 symbol-prefixed lines -> 37 matched -> 36 unique
  stages; the single non-match is the intended `QA passed:` run summary. The
  uppercase-exclusion reasoning is correct.
- **The CI claims are true, independently verified.** Runs `35037675348` and `35038606426`
  both reach the closing summary; all 36 stages present; `docs` is the only failure;
  `freezelib: passed: 234` and `panel-sections: passed: 17` on the runner.
- **Scope vs Non-Goals.** None of the three oversteps. `qa-js.bash` changed *where* it looks,
  not *what* it checks. `CLAUDE/QA.md` is Task 4.2 verbatim. `helpers/qa_environment/` is Task
  1.1, and `helpers/CLAUDE.md` mandates extracting text parsing — not optional. The new
  table's four rows match what I measured: only `docs` and `deployed-drift` genuinely differ
  machine-to-machine.
- **`verdicts.py` reads no host state** — both captures arrive as paths. Its 25 tests exercise
  behaviour against real observed input shapes (CI prefix, BOM, ANSI, run-summary exclusion,
  indented continuation), not the implementation.
- **Public-repo safety.** Clean. The only path-shaped strings are `/home/runner/work/.../`
  (generic, already elided) and `/workspace`; the Anthropic noreply address is the mandated
  attribution. No hostnames, usernames, container names or project names in the diff, the plan
  tree, or the five commit messages.
- **Version bumps.** No `files/var/local/claude-yolo/` file touched -> no `CCY_VERSION` owed.
  No playbook, Dockerfile, entrypoint or skill touched -> no LABEL /
  `REQUIRED_CONTAINER_VERSION` owed. `freeze-common.bash` carries no version constant.
- **Plan Commit Rule.** Was dangling mid-review (`PLAN.md` modified, `FINDINGS.md` untracked);
  `d4826f90` and `41b42564` closed it. Tree clean and level with `origin/F44`.
  `README.md:37` has the 00125 row.
- **Task ticks.** 1.1, 1.2, 1.3, 3.1-3.5, 4.1, 4.2 are all earned by code I read and, where
  claimed, by CI runs I fetched. The one overclaim is the Success Criterion in finding 1.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** — `QA passed: 911 files checked`; 36 stages; the `shellcheck`
  169 and `patterns` 15-partial advisories are standing and unchanged.
- `hooks-daemon plan-qa --sweep`: **PASS** — 2 findings, 0 block, both advisory and both
  pre-existing (a 00046 path reference, journal-freshness on 13 unrelated plans). Nothing
  against 00125.
- `ansible-playbook --syntax-check`: **not triggered** — the diff touches no playbook.
  `qa-ansible-syntax` still ran repo-wide inside `qa-all`:
  `82 playbooks OK (79 under playbooks/imports/, 3 elsewhere)`.
- `scripts/qa-helper-tests.bash` (required — `helpers/` and `tests/helpers/` changed):
  **PASS** — `Ran 1446 tests`, `OK (skipped=1)`.
- `scripts/test-freezelib.bash` (required — freeze library changed): **PASS** —
  `passed: 234  failed: 0`.
- `helpers.gnome.check_extension_compat`: **not triggered** — no `extensions/**/metadata.json`
  change. Ran inside `qa-all` anyway: `All 5 extension(s)`.
- `extensions` ESLint: **not triggered** — no extension JS changed (the edited file is
  `tests/extensions/*.mjs`, outside that project). Ran inside `qa-all`'s `js` stage anyway.
