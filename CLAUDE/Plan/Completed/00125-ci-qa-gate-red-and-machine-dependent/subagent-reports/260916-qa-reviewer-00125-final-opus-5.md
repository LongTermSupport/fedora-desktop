# QA Review — Plan 00125, `551a65d7` + `ac706483` on `F44` (Task 5.3 final)

> **Provenance**: written by the `qa-reviewer` subagent (Opus 5) on 2026-09-16 via a Bash
> heredoc, because `Write` and `Edit` were withheld from this reviewer session for the
> seventh consecutive round. The dispatch pre-authorised this fallback. The reviewer made
> no other write of any kind; the working tree was clean at `ac706483` before and after.

**Verdict**: FIX-BEFORE-MERGE

The mechanism in both commits is correct and was verified independently. Everything below is
a claim that outruns what was measured, a doc that did not move with the behaviour, or a
sweep that stopped at the pattern text instead of the defect. Nothing here breaks a user,
loses data or leaks anything.

## Blocking

None.

## Should fix

### 1. The library header now states, in capitals, the opposite of what the file does

`scripts/lib/qa-helper-summary.bash:6`, `:8`, `:35`

`ac706483` appended `qa_gate_case_count` and did not touch lines 1-36. Three statements are
now false of the file they head:

- `:6` — "Sourced, never executed — **it defines one function** and runs nothing." It defines two.
- `:8` — "**THIS FILE PARSES NO OUTPUT STREAM**, and that is the point of it."
  `qa_gate_case_count` (`:216`) parses an output-stream capture. This is the file's headline
  claim and it is the first thing a reader meets on the way to the function that contradicts it.
- `:35` — "It is also **ONE function rather than two**. The previous pair drifted into using
  opposite match rules and disagreed with each other about the same run; a single reader
  cannot." The file is two functions again, and they *do* use opposite rules (one reads a
  file, one is positional).

Fix: scope lines 1-36 to `helper_counts_summary` explicitly, or rewrite the header for a file
that now holds two stage-line readers.

Related, same cause: `scripts/qa-all.bash:18` still says "**The** reader for the helper-tests
stage line" (singular), and `CLAUDE/QA.md:73`'s catalogue row still reads "**the reader**
behind this suite's own `helper-tests` line". That row was correctly singularised by
`551a65d7` and made wrong again by `ac706483`, which did not touch `CLAUDE/QA.md`. The gate
now also covers the reader shared by 21 other gates, which the row does not say.

### 2. The seventh over-claim — "a gate prints its summary last" is false for 8 of the 21

`scripts/lib/qa-helper-summary.bash:206`

    # rule is positional: a gate prints its summary last, which is true because it is a summary.

Measured, by running all 21 gates and comparing the summary's line number against the
capture's line count:

    test-secret-scan              SUMMARY NOT LAST (+1)
    test-ccy-rootless-guard       SUMMARY NOT LAST (+1)
    test-ccy-token-mode           SUMMARY NOT LAST (+1)
    test-ccy-ssh-handling         SUMMARY NOT LAST (+2)
    test-freezelib                SUMMARY NOT LAST (+1)
    test-lxcfreeze                SUMMARY NOT LAST (+1)
    test-podfreeze                SUMMARY NOT LAST (+1)
    test-qa-ansible-failfast      SUMMARY NOT LAST (+2)
    === gates whose summary is NOT the last line: 8 of 21 ===

They print `OK`, `VERDICT: PASS`, `test-secret-scan: PASSED`, or two explanatory echoes after
it. The justification is also circular — "true because it is a summary" is a tautology, not a
measurement, sitting two lines under a sentence that correctly says "Measured across them".

The property that actually holds, and the one worth writing down, is narrower and was
measured: **no gate emits a second `passed: <digits>` line, and every count-bearing line in
all 21 is on stdout from the single parent process** — so the merged `2>&1` cannot reorder
them, and there is no child, no background job and no printing `EXIT` trap that could (all 16
traps are `rm -rf`; zero backgrounded commands).

### 3. The eighth over-claim, in the next sentence: the degrade-to-word path has no live caller

`scripts/lib/qa-helper-summary.bash:209-210`, mirrored at `FINDINGS.md:178`

    # count — it prints `PASSED (library version 1.2.0)` — so this path is live, not defensive.

`planlib-tests` does not call `qa_gate_case_count`. It has its own inline reader at
`scripts/qa-all.bash:246`:

    planlib_summary=$(printf '%s' "$planlib_out" | grep -oE 'PASSED \(library version [0-9.]+\)') ||
        planlib_summary="passed"

All 21 actual callers emit a `passed: N` line (measured — see "Checked and clean"), so the
branch is reached only by the two synthetic unit tests. It is defensive, which is fine;
claiming it is live in order to justify it is not. `FINDINGS.md:178` puts planlib's format in
the table of "shapes the shared reader handles" for the same reason, and it is not one of them.

Same paragraph, `:208`: "Degrading to a WORD ... **matches the other reader**" is contradicted
at `:212` by "**Unlike** `helper_counts_summary` this does NOT hard-fail". `helper_counts_summary`
refuses; it does not degrade to a word. Same two-lines-apart contradiction shape as the sixth
over-claim.

### 4. Task 4.5's sweep was scoped to the pattern text, not the defect — two inline readers left, one dead since the day it landed

`scripts/qa-all.bash:129`, `nokill-containerwatch`:

    nokill_summary=$(printf '%s' "$nokill_out" | grep -oE '[0-9]+ call site[s]? checked') ||
        nokill_summary="no forbidden kill call sites"

The gate has never printed that string. Measured:

    $ bash scripts/qa-nokill-containerwatch.bash 2>&1
    ✓ no-kill gate: 3 container-watch file(s) clean — reporting-only confirmed
    $ ... | grep -cE '[0-9]+ call site[s]? checked'
    0

`grep -n "call site" scripts/qa-nokill-containerwatch.bash` confirms no such wording exists
anywhere in it, and `git log -- scripts/qa-nokill-containerwatch.bash` shows one commit — the
wording has never changed. So the reader has matched zero times in its whole life, the `||`
fallback has hidden that on every run, and the coverage number (3 files) never reaches the
stage line. **A blind reader whose blind output is indistinguishable from a real answer, in
`qa-all.bash`, is the exact defect this plan exists to remove** — and it sits six lines under
a comment complaining that a rule was "written down beside the drift gate and never applied to
this one, six lines above it".

`scripts/qa-all.bash:246`, `planlib-tests`: same unscoped `-o`-over-whole-capture shape,
currently one match, untested. Latent exactly as the 21 were.

Both belong in Task 4.5's scope. `CLAUDE/AgentNotes.md:897` ("Generalise a fix past the file
you were reading") is the rule; `FINDINGS.md:162`'s "**Every other hard gate** in `qa-all.bash`
read its case count with an unscoped `grep -oE 'passed: [0-9]+'`" is the over-claim that let
them through — it was 21 of the 29 non-merged gates, and 2 of the other 8 have the same defect
with a different regex.

### 5. `--tracked-modules` is not exercised by any test

`tests/helpers/qa_environment/test_unittest_counts.py`

`grep -c "tracked-modules"` on the test file returns **0**. `run_module_in_subprocess` (`:262`)
never passes it. Every `main()` end-to-end test produces `modules=1 tracked=1`, which is
precisely what the `default=None -> len(args.modules)` fallback produces. **If `main()` ignored
the flag entirely, every test would still pass**, `tracked` would always equal `modules`, and
the untracked-file divergence — the entire reason `tracked=` exists — would be permanently
invisible.

The flag does work; checked directly rather than assumed:

    $ python3 -m helpers.qa_environment.unittest_counts --counts-file $cf --counts-token PROBE \
        --tracked-modules 99 tests.helpers.qa_environment.test_unittest_counts
    token=PROBE / tests=19 / skipped=0 / modules=1 / tracked=99

but nothing in the tree would catch it regressing. This is the "only the primary path is
asserted" shape inverted — only the *fallback* is asserted. One test through `main()` with
`--tracked-modules` differing from the module count closes it.

### 6. Stale line citation — `PLAN.md:164` and `FINDINGS.md:298` cite `qa-all.bash:138`

At HEAD, `:138` is `drift_out=""`; the gate invocation is `:139` and the abort is `:142`.
Tracked across the plan's commits:

    27d7b342 -> 137   (citation said 137 — correct)
    9b366751 -> 138   (citation bumped to 138 — correct)
    0b1623fa -> 139   (citation NOT bumped — wrong from here on)
    551a65d7 -> 139
    ac706483 -> 139

Hand-corrected once, stale one commit later.

On the open question: **this is the evidence for switching to anchor text**, and
`CLAUDE/AgentNotes.md:920` already says so ("prefer a citation that survives an edit ...
Launcher line numbers in this repo have gone stale within the hour of being verified"). The
general conversion should **not** block 5.3 — but fix this one instance now, because it is
currently wrong.

## Nits

- `scripts/qa-all.bash:189` and `CLAUDE/QA.md:146` both say "one `print()` in any of **1,482**
  tests". The run says 1483.
- `scripts/qa-helper-tests.bash:128` — a bare `#` line opens the new comment block, an editing
  artefact.
- `qa_gate_case_count`'s two patterns disagree and neither disagreement is documented:
  `awk '/passed: [0-9]+/'` requires exactly one space, `[[ =~ passed:[[:space:]]+([0-9]+) ]]`
  allows many. Consequence, measured: `passed:  29` (two spaces) degrades to the word — safe.
  But the *within-line* rule is FIRST match while the *across-line* rule is LAST line, in one
  function, and only the second is stated or tested:

      qa_gate_case_count 'suite passed: 3 of them; passed: 29 failed: 0'  ->  passed: 3

  Unreachable across the current 21, but it emits a wrong count — the "reads as a measurement"
  failure the comment says the design avoids. One sentence and one case.

## Checked and clean

- **The positional rule (attack 1)**: holds, and for a stronger reason than the comment gives.
  All 21 emit exactly one `passed: N` line; all count-bearing lines are stdout from the parent
  bash process, so `2>&1` cannot reorder; no gate spawns a background job; all 16 `EXIT` traps
  are `rm -rf` with no output; no gate prints anything count-shaped after its summary on the
  success path; the `failed: %d` stderr writes in the three vmtest gates are failure-path only,
  and `qa-all.bash` exits before the reader on failure.
- **Pure refactor (attack 2)**: verified independently, not from the commit message. Sourced
  the lib, ran each of the 21 gates, compared the *exact old expression* against the new
  function on the same capture. **21 of 21 SAME**, one match line each — so no gate previously
  produced a two-line output that the function now silently normalises. `helper-counts-reader`
  34 -> 43 is the nine new cases, and 34+9=43.
- **`tracked=` end to end (attack 3)**: `qa-helper-tests.bash --counts-file` produces
  `tracked=65` alongside `modules=65`; the gate's stdout is 0 bytes; `helper_counts_summary`
  renders `Ran 1483 tests in 65 modules (65 tracked), 1 skipped`; a file missing `tracked=` is
  refused (test present and passing). The two numbers come from genuinely independent sources —
  `tracked` from `git ls-files` via `qa_tracked_helper_tests`, `modules` from the `find` walk —
  and the earlier exit-2 check makes `modules > tracked` the only reachable divergence.
  The untracked-file control was **not** reproduced end to end, because it requires creating a
  file in the tree and the reviewer's rules of engagement forbid any mutation; finding 5 is the
  part of that leg nothing else covers.
  On the wording question: **`N modules (M tracked)` was the right call.** "66 of 65" is worse,
  the number is now in the delivered stream rather than the discarded one, and that satisfies
  round 8's actual request. **"Report, not fatal" is also right** — nothing is skipped, the
  tracked-but-missing direction still exits 2, and failing on an uncommitted test would break
  ordinary TDD.
- **Fail-fast**: no new `failed_when`/`ignore_errors`; no skip-and-warn. `qa_gate_case_count`'s
  soft degrade is correct — the gate's pass/fail is already carried by the `if ! ...; then exit 1`
  above every call site, so no failure signal is discarded, and a missing `awk` still aborts the
  suite under `set -e` rather than reporting `passed`.
- **Stderr hygiene**: `qa_gate_case_count`'s stdout is its captured payload; the `COVERAGE:`
  line and file names stay on stderr; `qa-all.bash`'s empty-stdout gate on the helper suite
  still holds (0 bytes measured).
- **IaC placement**: no playbook, `files/`, or `files/var/local/claude-yolo/` change — **no CCY
  version bump required, correctly absent**. No Ansible run (CCY container).
- **Naming**: `qa_gate_case_count` says what it does. The only naming issue is structural and
  already in finding 1 — a reader shared by 21 gates living in a file called
  `qa-helper-summary.bash` whose header disclaims the behaviour. Fixing the header is
  sufficient; renaming to something like `qa-stage-lines.bash` is the cleaner option and is the
  owner's call.
- **Public-repo safety**: scanned every added line across both commits for home paths, emails,
  hostnames, IPs, tokens and key blocks — no matches.
- **Plan Commit Rule**: tree clean, plan committed with its code,
  `JOURNAL/00125-Journal-26-09-16.md` **336 added / 0 deleted** (strictly append-only),
  `CLAUDE/Plan/README.md:37` row present. The journal repeats findings 2 and 3 verbatim —
  append a correction entry, do not rewrite.
- **Counts given in the dispatch**: 43 reader cases OK, 19 runner tests OK, 36 gates / 37 stage
  names / 38 symbol-prefixed lines OK (9 lines from the 8 merged-stage names, `patterns`
  printing twice, plus 29).
- **Modes and lint**: git modes correct (`qa-all`/`qa-helper-tests`/`test-qa-helper-summary`
  100755, libs and Python 100644). `shellcheck -x` clean on all five changed bash files.
  `helpers/CLAUDE.md`: stdlib-only imports, explicit `check=False` with a reason on the one
  `subprocess.run`.

## Mechanical gates

| Gate | Result |
| ---- | ------ |
| `./scripts/qa-all.bash` | **exit 0** — `✓ QA passed: 920 files checked`; `✓ helper-tests: Ran 1483 tests in 65 modules (65 tracked), 1 skipped`; `✓ helper-counts-reader: passed: 43` |
| `hooks-daemon plan-qa --sweep` | exit 1, **0 block / 2 advise**; neither finding touches Plan 00125 (00046 path-existence, journal-freshness across 13 other plans) |
| `scripts/test-qa-helper-summary.bash` | exit 0, `passed: 43 failed: 0` |
| `python3 -m unittest tests.helpers.qa_environment.test_unittest_counts` | exit 0, `Ran 19 tests ... OK` |
| `scripts/qa-helper-tests.bash` standalone | exit 0, `COVERAGE: 65 of 65`, `Ran 1483 tests ... OK (skipped=1)` |
| `scripts/qa-helper-tests.bash --counts-file --counts-token` | exit 0, counts file complete, gate stdout 0 bytes |
| `shellcheck -x` on the 5 changed bash files | clean |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook changed. qa-all's `ansible-syntax` stage covered all 82 anyway |
| `qa-helper-tests.bash` conditional gate | **triggered** (helpers/ + tests/helpers/ changed) — run both ways, above |
| `check_extension_compat` / extension ESLint | **not triggered** — no `extensions/` change |
| CI, run `35057905951` @ `ac706483` | fails on **`docs` alone**, 8 findings, all `.claude/rules/*.md -> ../hooks-daemon/CLAUDE/DirectoryRoles.md target does not exist` (Task 2.1). Every other stage green, `920 files` matching local exactly, and `✓ helper-tests: ... 2 skipped` against local `1` — the divergence is still produced, with `(65 tracked)` agreeing on both machines |

## Does Task 5.3 discharge?

**Not yet — but the remainder is small, bounded, and none of it blocks anything else.**

The engineering in both commits is sound and was confirmed rather than taken on trust: the
positional rule holds, the refactor is byte-identical across all 21 gates, `tracked=` travels
end to end and renders, and CI behaves exactly as predicted. If the code were the only
question, this would pass.

It does not discharge because the plan's own subject is claims that outrun their evidence, and
`ac706483` shipped three more of them (findings 1-3), plus a sweep that stopped at the regex
instead of the defect and left a reader that has been blind since the day it was written
(finding 4). Finding 4 in particular is the plan's charter aimed back at `qa-all.bash` itself.

Required before 5.3:

1. Header and doc-comment corrections — findings 1, 2, 3 (comment text only; `CLAUDE/QA.md:73`
   and `qa-all.bash:18` go with them).
2. Finish Task 4.5's sweep — `qa-all.bash:129` and `:246`, and fix `FINDINGS.md:162`'s "every
   other hard gate". The nokill one is a genuine repair, not a refactor.
3. One test through `main()` with `--tracked-modules` differing from the module count — finding 5.
4. Correct `qa-all.bash:138` -> `:139` in the two plan docs — finding 6.

Follow-up, not 5.3's business: the general line-number-to-anchor-text conversion; the
within-line match rule (nit 3); the `1,482` drift. Tasks 2.1, 2.2, 4.3 and 5.2 remain the
owner's decisions, correctly out of scope here.

Nine rounds in, the honest summary is that the code has been right for a while and the comments
keep outrunning it. Finding 4 is the one worth the owner's time regardless of the verdict.

## Review conditions

- Target: `ac706483` (HEAD), clean working tree at start **and** at end — the tree did not move
  during the review.
- Reviewed: `551a65d7` and `ac706483`, plus the round-8 re-check report.
- Container: CCY (`/workspace`) — no Ansible run, no deploy.
