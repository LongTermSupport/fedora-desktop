# QA Review — Plan 00125 round 2, `070c4507..fa3cfe8e`

**Verdict**: FIX-BEFORE-MERGE

Reviewed `17307240` (the response to round 1) against
`subagent-reports/260916-qa-reviewer-00125-opus-5.md`. `d31b2d81` and `fa3cfe8e` landed
mid-review and are included; HEAD is `fa3cfe8e`, tree clean, level with `origin/F44`.

## Per round-1 finding

| # | Finding | Fix |
| - | ------- | --- |
| 1 | helper-tests line blind to skips | **Real and load-bearing — but half-closed.** Measured: local `Ran 1453 tests, 1 skipped` vs CI run `35040618271` `Ran 1453 tests, 2 skipped`; `compare` returns `differs` where round 1 measured byte-identical `agree`. Mechanism genuine. The overclaim it was cashed into is still on the page — New 1. |
| 2 | coverage + two under-matches | **Real.** COVERAGE line per side, `CI_LOG_PREFIX` requires the timestamp, stages keep every line in order. Verified on the real CI log: 37 of 38, 0 unrecognised, 0 symbol-bearing lines lost to prefix stripping; `patterns` keeps both lines. One over-correction — New 2. |
| 3 | `assert_on_host` defaults untested | **Real.** Two `declare -f` pins, both run and pass (`scripts/test-freezelib.bash:952-954`). |
| 4 | env-var seam / false journal claim | **Real** — arguments adopted *and* journal corrected. Side effect: New 3. |
| 5 | "26 gates" is 25 | **Real and correct.** Listed independently: 25 `exit 1` gates after `scripts/qa-all.bash:152`, 26 after the drift abort at `:129` — so the surviving "26" at `PLAN.md:165` and journal 00:41 is right in its own context, not a missed edit. |
| 6 | skip reason unreachable | **Real.** Claim dropped; `python3 -m unittest -v tests.helpers.displaylink_recovery.test_run_recovery` does print `skipped 'running as root: chmod 000 does not block the read'` — measured. |
| 7 | host red until deploy | **Real.** `scripts/qa-deployed-drift.bash` EXTRA_PAIRS does cover `files/home/.local/lib/freeze/*`; `PLAN.md:159-167` and journal 00:41 say so. |
| nit `.mjs` | **Real, no over-reach.** 11 files, all tracked and repo-owned; the 3 new ones are ESM and `node --check` parses `.mjs` as a module (node v24 refuses unknown extensions outright, so it is not a no-op). |
| nit README | **Real.** "six tests that read host state". |
| nit R2 | **Real.** Now quotes *"pick exactly one of the two"* and claims a third case explicitly. |

No finding was silently dropped. Nothing is theatre.

## Should fix

### 1. `PLAN.md:212-216` still says `helper-tests` agrees exactly, and it is now measurably false

The ticked Success Criterion reads *"the only stages that differ are `docs` … and
`deployed-drift` … `js`, `bash`, `patterns`, `python` and `helper-tests` now agree
exactly."* Measured at HEAD against run `35040618271`:

```
deployed-drift  differs   here: skipped (CCY container …)   ci: skipped (… .local/bin does not exist)
docs            differs   here: 71 files OK                 ci: 8 finding(s) across 71 files
helper-tests    differs   here: Ran 1453 tests, 1 skipped   ci: Ran 1453 tests, 2 skipped
TOTAL DIFFERENCES: 3
```

`fa3cfe8e`'s own journal entry says `verdicts.py` will flag it. This is the exact sentence
round 1's blocking finding said had been cashed prematurely: the instrument was fixed and
the claim was left. Rewrite it — the divergence is now the *declared* signal, satisfying
the criterion's own *"or the disagreement is declared in `CLAUDE/QA.md`"* branch.

### 2. `capture_problem`'s `no-run-summary` premise is false for 7 exit paths, and it refuses the plan's own headline scenario

`scripts/qa-all.bash:34,43,52,62,71,80,97` are `exit 2` missing-tool aborts printing only
`ERROR: Missing required tools …` — no `QA FAILED`. Measured:

```
capture: "✓ bash: 300 files OK" + "ERROR: Missing required tools (semgrep)…"
  -> QA-VERDICTS-COVERAGE side 1 of 1 … -> 1 stage(s)
  -> capture_problem: 'no-run-summary'   (main returns 1; triage aborts under pipefail)
```

The stderr text then misdiagnoses it: *"the run did not reach its end or the capture is
truncated. Every 'did not run' row below it would be an artefact of the truncation, not a
fact about that machine"* — when a missing tool on one machine **is** a fact about that
machine, and is precisely this plan's subject. Live, not theoretical:
`.github/workflows/qa.yml:62,66,74` (galaxy install, `npm ci`, vault placeholder) exist
only to keep CI off those `exit 2` paths.
`test_an_aborted_run_is_comparable_because_it_still_names_its_own_failure` asserts the
claim over the `exit 1` population only — the guard-the-case-you-thought-of shape.

**Fix**: treat a recognised missing-tool abort as a terminal state too, or downgrade
`no-run-summary` to a loud banner above the table rather than a refusal to compare.

### 3. The positional-args change added two shellcheck advisories whose suggested remedy would disable the guard

`⚠ shellcheck` moved 169 → 171; both new entries are

```
files/home/.local/bin/podfreeze:879 SC2119 Use assert_on_host "$@" if function's $1 should mean script's $1.
files/home/.local/bin/lxcfreeze:735 SC2119 …
```

Attribution is certain by construction — SC2119 cannot fire on a function with no
positional parameters. Taking the advice would pass the *tool's* argv into the marker slots
(`podfreeze <container>` → `[ -f "<container>" ]`), leaving only `$container` as a signal
and silently loosening a guard whose whole job is to refuse. Suppression directives are
banned (R-QA-SUPPRESSION), so the remedy is a comment at both call sites saying why the
advisory must stand.

## Nits

- **The skip-count extraction under-matches two ways.** `scripts/qa-all.bash:163` requires
  the closing paren, so `OK (skipped=1, expected failures=1)` — unittest appends
  `expected failures=`/`unexpected successes=` *after* `skipped=` — reports **0 skipped**,
  silently restoring the defect this line removes. Latent: censused every skip mechanism in
  `tests/helpers/` (case-insensitive `skiptest|unittest.skip|@skip|expectedfailure`) → 3
  sites, no expected failures, which also independently confirms `d31b2d81`'s
  four-shapes-four-counts bijection. Second: two matches concatenate
  (`(skipped=2)` + `(skipped=10)` → `210`). Both go away with
  `[[ $helper_out =~ skipped=([0-9]+) ]]`. The 0-skip path, `set -e` and `helper_skips`
  scoping are fine — replayed.
- **Coverage cannot see its own blind spot.** `symbol_lines` is counted *after*
  `CI_LOG_PREFIX` stripping, so a CI line whose prefix no longer matches the stricter regex
  leaves both numerator and denominator and coverage reads 100%. Measured 0 such lines in
  run `35040618271`, so latent; counting the denominator on the raw line closes it.
- **The deleted "last wins" comment was removed, not answered.** It documented that
  `qa-all.bash` echoes some failures to both streams and a CI log interleaves them; with
  sequence comparison a duplicate or a reordering on one side now reads `differs`.
  Measured: no duplicates in the current CI log (only `patterns`, same two lines, same
  order both sides). One sentence saying the premise was checked would close it.
- **Journal timestamps run ahead of the commits carrying them.** `17307240` (authored
  00:35:01) contains entries stamped 00:37/00:39/00:41; `d31b2d81` (00:38:51) contains
  00:48; `fa3cfe8e` (00:39:16) contains 00:54 — the clock read 00:39 at that moment. On a
  log whose 00:54 entry argues that *predicted-then-measured order* is the point, invented
  times undercut the argument.

## Checked and clean

- **Append-only discipline** — both corrections are NEW entries under a 00:35 "Corrections
  to earlier entries" heading naming the 23:38/23:58/00:02/23:52 entries; the journal diff
  is additions only, no earlier line edited.
- **`assert_on_host` call sites** — `podfreeze:879`, `lxcfreeze:735`, both bare;
  `${1:-…}` is nounset-safe; positional parameters are not inherited by a called function.
  No `FREEZE_CONTAINERENV_PATH`/`FREEZE_DOCKERENV_PATH` reference survives anywhere in the
  repo, docs included. No deployed tree exists in this container to drift.
- **`verdicts.py` signature change** — no caller outside `probe-qa-verdicts.bash` (CLI) and
  its own tests, so `Parsed`/`list[Verdict]` breaks nothing. The probe captures `2>&1`, so
  `QA FAILED` reaches the local capture.
- **The `.mjs` widening pulled in nothing vendored or generated** — all 11 files
  `git ls-files`-tracked; the 3 new ones are `tests/extensions/{gi-stubs,gjs-loader,test-panel-sections}.mjs`,
  all top-level `import`/`export`, all parsed clean.
- **Plan Commit Rule** — tree clean at `fa3cfe8e`; the `CLAUDE/QA.md` edit found dangling
  mid-review was committed by `d31b2d81`.
- **Public-repo safety** — only `/home/runner/…` and `/workspace` path-shaped strings; no
  names, hosts, container names or secrets in the three commits or the plan tree.
- **Version bumps** — no `files/var/local/claude-yolo/**`, no playbook, Dockerfile,
  entrypoint or skill touched; none owed.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** — `✓ QA passed: 914 files checked`, 36 stages;
  `⚠ shellcheck: 171` (see Should-fix 3) and `⚠ patterns: 15 partial` standing.
- `hooks-daemon plan-qa --sweep`: **PASS for 00125** — 2 findings, 0 block, both
  pre-existing (00046 path reference, journal-freshness on 13 unrelated plans).
- `ansible-playbook --syntax-check`: **not triggered** — no playbook in the diff;
  `qa-ansible-syntax` ran repo-wide inside `qa-all` (82 playbooks OK).
- `scripts/qa-helper-tests.bash` (required — `helpers/`, `tests/helpers/` changed):
  **PASS** — `Ran 1453 tests`, `OK (skipped=1)`.
- `scripts/test-freezelib.bash` (required — freeze library changed): **PASS** —
  `passed: 236  failed: 0`, including the two new default pins.
- `helpers.gnome.check_extension_compat` / `extensions` ESLint: **not triggered** — no
  `metadata.json` and no extension JS change; both ran inside `qa-all` anyway.
