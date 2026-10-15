# qa-all: functional specification for the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md), the second QA orchestrator. Method and shape:
[SPEC-qa-changed.md](SPEC-qa-changed.md) (read it first; this document does not repeat what the
two share). The spec states what the bash does today, numbers the latent bugs, and says which a Go
port fixes deliberately and which it keeps. It was written before any Go code.

Sources, all read at the branch point (`F44` after the qa-changed merge, `e4f6615c`). `L<n>` is a
line of the file named by its prefix:

| prefix | file                                        | lines    |
| ------ | ------------------------------------------- | -------- |
| `QA`   | `scripts/qa-all.bash`                       | 1446     |
| `HS`   | `scripts/lib/qa-helper-summary.bash`        | 286      |
| `TH`   | `scripts/test-qa-helper-summary.bash`       | 718      |
| `GC`   | `helpers/qa_environment/gate_call_sites.py` | 133      |
| `LC`   | `helpers/docs/link_check.py` (`qa_gates`)   | L119-214 |
| `VD`   | `helpers/qa_environment/verdicts.py`        | 280      |
| `WF`   | `.github/workflows/qa.yml`                  | L137-146 |

`QA` is on the bash-limits ratchet allowlist at 836 code lines, 2 functions, `data` (a measure the
rule leaves in bash only because it is "the most churned orchestrator"): the script every
contributor, agent and CI runs before every push.

"stdout" is the tool's stdout. Today's contract: stdout carries one stage line per gate and the
verdict; stderr carries the captured detail of a failed gate and the `ERROR:` aborts. There is no
colour; every glyph (`✓ ✗ ⚠`) is plain UTF-8.

---

## 1. What it is, who calls it, and what depends on its output

`./scripts/qa-all.bash` runs **every** QA gate in a fixed order, never stopping at the first
failure (`QA` L44-55), prints one stage line per gate, writes a machine report to
`/tmp/qa-results.json`, and prints one verdict. It takes no arguments (bug B1).

Callers (searched across the tree; the list is complete):

- **People and agents**, by documentation: `CLAUDE.md` (QA Mandatory Before Commits, Always
  Push), `CLAUDE/QA.md`, `docs/development.md`, the path rules in `.claude/rules/`. They read stdout,
  the verdict line and the exit status, and `jq '.failures[]' /tmp/qa-results.json`.
- **CI** (`WF` L137-146): `run: ./scripts/qa-all.bash`, then the artifact upload of
  `/tmp/qa-results.json` (`if: always()`, `if-no-files-found: ignore`). The job fails on a non-zero
  exit. Nothing else greps its output.
- **`verdicts.py`** (`VD`): parses a captured run, local or a CI log, for the stage lines. The
  contract it reads is section 7.
- **The hooks daemon**: the stop rule on QA failure keys on the tool's own output. The tokens
  `✗ QA FAILED:` and `✓ QA passed:` and the exit status are kept verbatim.
- **Readers of its text, which the port must replace** (each is a scan of `qa-all.bash` source for
  a fact, and the wrapper will have no such source):
  - `qa-changed`'s suite population (`SPEC-qa-changed.md` B9): every `$SCRIPT_DIR/(test|qa)-*.bash`
    mention.
  - `LC` `qa_gates` (the QA.md inventory gate, `qa-docs.bash`): every `$VAR/.../x.bash` and
    `-m helpers.x` mention.
  - `TH` and `GC`: the `qa_gate_detail` call sites, parsed by a Python helper, and two `grep -c` of
    `exit 1` and `qa_hard_gate_failed` (the "no gate aborts the suite" property).
  - `scripts/test-qa-docs-exit-codes.bash` L120: writes a fake `qa-all.bash` into a fixture tree for
    the docs gate.
  - `src-go/qa/bashlimits` tests: the name `scripts/qa-all.bash` as a fixture path (not a reader).

What `qa-all.bash` reads from the gates it runs: the exit status, the whole stdout+stderr of each
hard gate, and for seven stages a JSON file. Section 4 and 5.

---

## 2. The command line

```
qa-all.bash                  run every gate
```

No flags, no arguments. **Every argument is silently ignored** (B1): `qa-all.bash --help` and
`qa-all.bash --typo` run the whole suite. The Go command adds `-h/--help` and the two internal flags
`--root DIR` and `--scripts DIR` the wrapper sets (as `qa-changed` has); anything else is a usage
error, exit 2.

Environment read (new, section 6): `QA_ALL_TIMEOUT`, `QA_RESULTS_JSON`. Passed through unchanged
to the gates: everything else, notably `QA_BASH_LIMITS_BASE` (the ratchet base), `HOME` and
`XDG_CACHE_HOME`.

---

## 3. Order of operations (`QA` L14-1446)

1. `set -euo pipefail`; create ten temp files; one `EXIT` trap removes them (`QA` L28-41).
2. Run the gates of section 4 in the order of section 5, each recorded, none aborting the run,
   except the **missing-tool aborts** of the seven stages (section 4.2).
3. Merge the seven stage reports into one JSON document and write it (section 8).
4. Print the verdict (section 7.3), exit 0 or 1.

Every gate's command runs from the caller's working directory except `extension-compat` and
`panel-contract`, which `cd` to the repository root first (B11).

---

## 4. The four kinds of gate

### 4.1 Hard gate (83 of the 92 rows): run, capture, require status 0, read a summary

`QA` pattern, one per gate:

```
out=""
if ! out="$(bash "$SCRIPT_DIR/<script>" 2>&1)"; then          # or: cd root && python3 -m <module>
    qa_hard_gate_failed <name> "<reason>" "$out"
fi
summary=$(<reader> "$out" ...)
qa_pass_line <name> "$summary"
```

- stdout and stderr of the gate are merged into one capture, so their order is true.
- **Failed** (any non-zero status, 1 and 2 alike): the capture, trailing newlines removed and one
  newline added, goes to **stderr** (nothing if empty), then the stage line
  `✗ <name>: <reason>` to **stdout**, and the name is appended to the failed list. The pass line is
  suppressed (`qa_pass_line` looks the name up in the failed list).
- **Passed**: `✓ <name>: <summary>` to stdout, where `<summary>` is the reader's answer
  (section 4.5). A passing gate's capture is discarded (B-kept K1).
- A gate script that does not exist is `bash` exit 127: a failure like any other.

### 4.2 Stage (7 rows): the jq-merged reports

`bash`, `python`, `patterns`, `ansible`, `ansible-syntax`, `js`, `docs`:

```
rc=0
QA_JSON_OUT="$TMP_<STAGE>" "$SCRIPT_DIR/qa-<stage>.bash" || rc=$?
if [[ $rc -eq 2 ]]; then echo "ERROR: <message>" >&2; exit 2
elif [[ $rc -ne 0 ]]; then FAILED=$((FAILED + 1)); fi
```

- The stage's stdout and stderr are **not captured**: they go straight to this process's streams,
  live, in the true order. The stage prints its own stage line (`✓ bash: 91 files OK`,
  `✗ docs: ...`) and its details; this tool prints nothing for it. Those lines are the stage's
  census entry in `VD`.
- Status 0 passes; **2 aborts the whole run** immediately (the one deliberate abort: a suite that
  cannot run its tools has nothing to accumulate, `QA` L52-54): the message below to stderr, exit 2,
  no JSON, no verdict. Any other non-zero adds one to `FAILED`.
- `VD` knows the abort (`TOOL_ABORT`: `^ERROR: (?:Missing required tools|[a-z-]+ gate could not produce|qa-all: )`, the last for the Go runner's own aborts, B26),
  so the message texts are a contract. They are data in `qa-gates.tsv`:

| stage            | exit 2 message                                                                             |
| ---------------- | ------------------------------------------------------------------------------------------ |
| `bash`           | `ERROR: bash gate could not produce a result (a required tool is missing or the tool failed: see the ERROR line above).` (B28) |
| `python`         | the same, naming `python` (B29)                                                            |
| `patterns`       | the same, naming `patterns` (B30)                                                          |
| `ansible`        | `ERROR: Missing required tools for ansible check. Install them and re-run.`                |
| `ansible-syntax` | `ERROR: ansible-syntax gate could not produce a result (prerequisites missing or the tool failed: see the ERROR line above).` (B27) |
| `js`             | the same, naming `js` (B31)                                                                |
| `docs`           | `ERROR: docs gate could not produce a result (zero-file scan or checker crash).`           |

- The report each stage writes to `$QA_JSON_OUT` is `{type, status, summary:{total,passed,failed}, failures:[...], ...}` (extra keys are the stage's own).

### 4.3 Drift gate (1 row): `deployed-drift`

Like a hard gate, except that on **success** its own output, trailing newlines removed, is printed
to stdout with `echo` as the stage line(s) (`QA` L235-243): the gate's output is the line
(`⚠ deployed-drift: skipped (CCY container ...)` or `✓ ...`), not a summary of it. If it prints
nothing, a blank line is printed and there is no stage line (B12). Failure as 4.1, reason
`repo-owned scripts differ from their deployed copies`.

### 4.4 Helper-tests gate (1 row): the counts file

`QA` L285-330. `scripts/qa-helper-tests.bash --counts-file <tmp> --counts-token <token>`, token
`qa-all-<pid>-<epoch ns>`, with **stdout and stderr captured separately** (`QA` L287-288), because
the counts do not travel in text. Three sequential ways to fail, one gate, recorded once:

1. status non-zero: `✗ helper-tests: the helper unit suite failed`; the capture (stdout then
   stderr, concatenated) to stderr.
2. status 0 but **stdout non-empty**: `✗ helper-tests: wrote to stdout, which is this suite's verdict stream`; the capture is a four-line explanation (verbatim, `QA` L305-309) followed by the stdout.
   A test printing there could forge or split a stage line.
3. status 0, stdout empty, but the counts file cannot be read: the reader (4.6) writes its reason
   to stderr, then `✗ helper-tests: ran, but its counts could not be read` (empty capture).

Pass line: `✓ helper-tests: Ran 1464 tests in 64 modules (64 tracked), 1 skipped`.

### 4.5 Summary readers (`HS`)

A hard gate's `<summary>` comes from one or more readers over its capture:

- **case count** (`qa_gate_case_count`, 72 gates, 73 before `helper-counts-reader` is removed): the last line holding `passed:[[:space:]]+[0-9]+`,
  then the last such match on that line, printed `passed: N`; **no match prints the bare word
  `passed`** (B2).
- **detail** (`qa_gate_detail <capture> <ERE>`, 11 gates and 13 patterns): the matched text of the last line that
  matches the pattern; **no match prints `summary unreadable`** (B3). The patterns are POSIX ERE
  (`awk` then `bash =~`, leftmost-longest). `vmtest-manifest` reads three patterns and joins them
  `a; b; c`.

The patterns (data, section 11): `toolchain` `TOOLCHAIN-OK .+`; `go` `GO-OK [0-9]+ package[(]s[)]`;
`bash-limits` `BASH-LIMITS-OK [0-9]+ bash files: .+`; `nokill-containerwatch`
`[0-9]+ container-watch file[(]s[)] clean`; `planlib-tests` `PASSED [(]library version [0-9.]+[)]`;
`ssh-suspend-guard` `VERDICT: PASS`; `extension-compat`
`All [0-9]+ extension[(]s[)] cover the GNOME Shell that Fedora [0-9]+ ships`; `panel-contract`
`[0-9]+ constant[(]s[)] agree between .+`; `plan-script-logging`
`[0-9]+ plan script[(]s[)] examined, no offences`; `version-pins`
`vars/[a-z-]+[.]yml: VERSION-PINS-OK .+`; `vmtest-manifest` three, listed in the table.

### 4.6 The counts reader (`helper_counts_summary`, `HS` L78-202)

Reads the counts file (`key=value` lines, split at the first `=`; an empty key line is skipped; a
last line without a newline is kept). Refuses, returning non-zero with a one-line reason on stderr
and **nothing on stdout**: no file; an empty file (its own message, "the runner did not write it");
an unknown key; a repeated key (tracked by a seen-flag, not by emptiness: `tests=` then `tests=5`
is a repeat); a token that is not the expected one (checked before the digits); a missing or
non-`^[0-9]+$` value for `tests`, `skipped`, `modules`, `tracked`. Prints
`Ran <tests> test[s] in <modules> module[s] (<tracked> tracked), <skipped> skipped`, singular when
the count is exactly 1. The Go port reproduces every refusal and message substring.

---

## 5. The gates and their order

93 gates today, 92 rows after the removal below; the order is the contract (the census and the
test of "every gate runs" both depend on it). It is `scripts/qa-gates.tsv` (section 11), extracted
from the bash by a script, and reads:

1. `toolchain`, `go`, `bash-limits` (hard, detail). `toolchain` deliberately does not abort: a
   drifted toolchain makes the verdicts unreliable, not unrunnable (`QA` L91-100).
2. The seven stages.
3. `nokill-containerwatch` (hard, detail), `deployed-drift` (drift), `helper-tests` (helpers).
4. The `test-*.bash` suites, `qa-gh-scopes`, `qa-speech-to-text-rules`, `qa-ready-wait-rules`,
   `qa-ansible-pause`, `qa-stt-limits` (hard, case count), interleaved as in the file; `planlib-tests`
   and `ssh-suspend-guard` (hard, detail).
5. `helper-counts-reader` (`test-qa-helper-summary.bash`), `docs-exit-codes`, `qa-changed-mapping`
   (hard, case count). **`helper-counts-reader` is removed** by the port (section 9); the other
   two stay.
6. `extension-compat`, `panel-contract` (hard, python module), `vmtest-manifest` (three details),
   `plan-script-logging`, `version-pins` (hard, detail).

The 92 rows after that removal are what the Go runs; the file lists them in this order with the
reason text of each `✗` line (`QA` L104-1389), which is a column of the data.

---

## 6. Environment and files

| name                     | read by               | meaning                                                                               |
| ------------------------ | --------------------- | ------------------------------------------------------------------------------------- |
| `QA_JSON_OUT`            | set by `QA` per stage | A `mktemp` file the stage writes its report to. Required by those scripts.            |
| `QA_BASH_LIMITS_BASE`    | `qa-bash-limits.bash` | The ratchet base (CI sets it); inherited unchanged.                                   |
| `HOME`, `XDG_CACHE_HOME` | gates (inherited)     | Where the Go-built gates keep their binaries.                                         |
| `QA_ALL_TIMEOUT`         | new (B8)              | Per-gate time limit as a Go duration; empty is 30m, `0` disables.                     |
| `QA_RESULTS_JSON`        | new (B7)              | The path of the machine report; empty is `/tmp/qa-results.json`, the path CI uploads. |

Files written: the ten temps (removed on exit) and `/tmp/qa-results.json`. Files read: the gate
scripts. It never writes to the repository.

---

## 7. Output contract

### 7.1 Stage lines (stdout), in the order of section 5

```
✓ <name>: <summary>                    a passing hard, drift or helpers gate
✗ <name>: <reason>                     a failed one (capture on stderr first)
```

plus whatever the seven stages print themselves. `VD` matches `^[✓✗⚠] ([a-z0-9][a-z0-9-]*): (.*)$`
for a stage and keeps every line in order, so **the name charset, the colon-space and one line per
gate are a contract**; a `⚠` is "ran but incomplete" (`deployed-drift` skipped).

### 7.2 stderr

The capture of each failed hard gate (before its `✗` line), the stages' own stderr (live), the
counts reader's reason, and the `ERROR:` abort. Nothing else.

### 7.3 The verdict (stdout)

Pass (no stage non-zero, no hard gate failed):

```
✓ QA passed: <TOTAL> files checked
```

Fail:

```
✗ QA FAILED: <E> errors in <TOTAL> files
  <K> gate(s) failed: <names space-joined>          only when K > 0, names in run order
  Details: jq '.failures[]' /tmp/qa-results.json
```

`<TOTAL>` and `<E>` are `.summary.total` and `.summary.failed` of the merged report (the stages
only: hard gates contribute no files). Exit 0 / 1. `VD` counts `QA passed:`/`QA FAILED:` as the run
terminator, so a capture without one is read as truncated.

---

## 8. The machine report (`QA` L1394-1424)

`jq -s` over the seven temp files **positionally** (`.[0]`..`.[6]`):

```
{ status: "pass"|"fail",
  summary: {total, passed, failed},        # sums of .summary.* with `add // 0`
  failures: [ every .failures[] of every stage, in stage order ],
  checks: { bash, python, patterns, ansible, ansible_syntax, js, docs }   # each stage's whole document
}
```

`status` is `fail` when `FAILED > 0` or a hard gate failed. Written to `/tmp/qa-results.json`
(`>` redirection: not atomic, and left in place if the run ends before this step). Pretty-printed,
two-space indent, key order as above.

---

## 9. The oracles, and where each goes

| oracle                                                                                         | what it proves                                                           | disposition                                                                                                                                                                                                                                                                                                                    |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `TH` cases 1-35 (`helper_counts_summary`)                                                      | every counts-file refusal, the plural forms, the stderr diagnostics      | **ported, case for case, to Go tests** over the Go reader, plus the end-to-end case against the real `unittest_counts` runner                                                                                                                                                                                                  |
| `TH` cases for `qa_gate_case_count` (14) and `qa_gate_detail` (5)                              | the readers' rule (last line, last match, word fallback)                 | **ported to Go tests**; the fallbacks stop being fallbacks (B2, B3) and are tested as failures                                                                                                                                                                                                                                 |
| `TH` "every `qa_gate_detail` pattern is read from `qa-all.bash` and run against its real gate" | a pattern cannot drift from the wording of its gate                      | **moved to run time**: the pattern is data beside the gate, and a pattern that does not match the gate's real output is now a **failed gate** (B3), in every full run, rather than a suite that re-ran 11 gates. A Go test pins the failure; the real-tree test pins that every row's pattern compiles and every script exists |
| `TH` "registered in both directions" (`GATE_COMMAND` table)                                    | a registered gate has a call site and a call site is registered          | **disappears by construction**: one row carries the gate and its pattern; a row-loader test refuses a malformed one                                                                                                                                                                                                            |
| `TH` "exactly one `exit 1`" and "`qa_hard_gate_failed` call sites > 0"                         | no gate aborts the suite: a failing gate does not disable those after it | **Go test**: a table test fails each of the 92 gates in turn through the fake runner and asserts every later gate still ran, in order, and the failed name is in the verdict; also two failures, and the single abort of a stage exit 2                                                                                        |
| `GC` (`gate_call_sites.py`) and `tests/helpers/qa_environment/test_gate_call_sites.py`         | find the `qa_gate_detail` call sites                                     | **deleted**, with the library; nothing remains to parse                                                                                                                                                                                                                                                                        |
| `LC` `qa_gates` + its tests                                                                    | every gate has a row in QA.md and the reverse                            | **kept, reading `scripts/qa-gates.tsv`** instead of the text (script gates by filename, module gates by dotted path; the same `source`-line exemption becomes "no such row")                                                                                                                                                   |
| `scripts/test-qa-docs-exit-codes.bash` L120 fixture                                            | the docs gate exit codes                                                 | the fixture writes `scripts/qa-gates.tsv` (one comment line) instead of `qa-all.bash`                                                                                                                                                                                                                                          |
| `scripts/test-qa-changed.bash` L124-125                                                        | a suite picked by name, for a library file                               | names another real pair (the `lib/qa-helper-summary.bash` file is deleted)                                                                                                                                                                                                                                                     |
| `src-go/qa/changed/{suites,run,golden,realtree}_test.go`                                       | the population from `qa-all.bash` text                                   | read the data file; the realtree test asserts the population is the rows of `qa-gates.tsv`                                                                                                                                                                                                                                     |

**Never weaken what they prove**: the list above is the whole of `TH`'s 718 lines. The one
assertion that moves to run time (the pattern/gate coupling) becomes strictly stronger: it holds
on every full run on every machine, not only inside one suite that re-ran the gates.

---

## 10. Latent bugs

Numbered for the plan and the golden deviation file. **Fix** means the Go port changes behaviour on
purpose (a deviation pinned in `testdata/deviations.json` with the bug number); **Keep** means
reproduced. Items verified by running the bash are marked (reproduced).

| #   | where                                | defect                                                                                                                                                                                                                                                                                                                                                                           | disposition                                                                                                                                                                                                                                                     |
| --- | ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | `QA` whole file                      | Arguments are ignored: `qa-all.bash --help`, `--plan`, `--typo` each run the full suite (minutes) and exit with its verdict. (reproduced)                                                                                                                                                                                                                                        | **Fix**: `-h/--help` prints usage to stdout, exit 0; any other argument is exit 2 with the usage on stderr                                                                                                                                                      |
| B2  | `QA` L345 and 62 more; `HS` L239-253 | A suite that exits 0 without a `passed: <n>` line prints `✓ <name>: passed` (the bare word): a harness that died before its summary, or never printed one, is a green tick. (reproduced on the live tree: `test-ccy-info-flags.bash` prints `Summary: N passed, M failed`, so its tick has never carried a count.)                                                               | **Fix**: the gate fails, `✗ <name>: exited 0 but printed no 'passed: <n>' line, so it proved nothing`, capture on stderr; and `test-ccy-info-flags.bash` prints `passed: N failed: M` (the suite fixed in the same commit)                                      |
| B3  | `QA` 11 detail sites; `HS` L277-285  | A detail pattern that matches nothing prints `✓ <name>: summary unreadable`: a tick over a reader gone blind, the defect its own header says the fallback exists to expose (it exposed it as a word, not as a failure).                                                                                                                                                          | **Fix**: the gate fails, `✗ <name>: ran, but its summary line was not found`, with the pattern and the capture on stderr. The pattern/gate coupling `TH` tests becomes a property of every run                                                                  |
| B4  | `QA` L1405-1424                      | The merge is positional over seven files. `jq -s` **drops an empty input**, so a stage that exited without writing its report (a crash, a kill, `mktemp`'s empty file) shifts every later stage down one slot: `.checks.docs` holds `js`'s report. A malformed file aborts `jq` under `set -e` *after* every gate ran, so the run ends with no verdict, no JSON and jq's status. | **Fix**: each stage's report is read by name; an empty, malformed or wrong-shape report is that stage's failure, a stage line `✗ <stage>: its report is unusable (<why>)`, and its `checks` entry is `null`                                                     |
| B5  | `QA` L137-206                        | A stage that exits 0 while its own report says `summary.failed > 0` passes (the status is the exit code alone).                                                                                                                                                                                                                                                                  | **Fix**: that is a stage failure, `✗ <stage>: exited 0 but its report counts <n> failed`. (Checked against the real stages; none does it today.)                                                                                                                |
| B6  | `QA` L1405-1424                      | A run that ends before the merge (a stage's exit 2, a crash, Ctrl-C) leaves the previous run's `/tmp/qa-results.json` whose `.status` may say `pass`: `jq .status` then reports a verdict from a different run.                                                                                                                                                                  | **Fix**: the file is removed before the first gate (a failure to remove is exit 2) and written atomically (temp file, rename)                                                                                                                                   |
| B7  | `QA` L28                             | One fixed path for every run: agents in sibling worktrees, a user and a hook clobber each other's report, and the final line points at whichever wrote last.                                                                                                                                                                                                                     | **Fix**: `QA_RESULTS_JSON` overrides the path; the default stays `/tmp/qa-results.json` (CI uploads it)                                                                                                                                                         |
| B8  | whole file                           | No gate has a time limit: a hung suite, a prompt waiting on a terminal, a stuck network call holds the push gate for ever and cannot be told from a slow gate.                                                                                                                                                                                                                   | **Fix**: per-gate limit, default 30 minutes, `QA_ALL_TIMEOUT` overrides, `0` disables; the gate's process group is killed; a hard gate fails `✗ <name>: <reason> (timed out after 30m0s)`, a stage fails with the stage line `✗ <stage>: timed out after 30m0s` |
| B9  | whole file                           | Ctrl-C kills bash and leaves no word: no stage line, no verdict, children possibly orphaned; the exit is 130 by the shell's accident.                                                                                                                                                                                                                                            | **Fix**: SIGINT/SIGTERM end the running gate's process group; `✗ qa-all interrupted` on stdout; exit 130; no report                                                                                                                                             |
| B10 | `QA` as a source of facts            | `qa-changed`'s population (SPEC-qa-changed B9), the QA.md inventory (`LC`) and `TH` each re-derive the gate list by scanning the text, so a gate invoked another way, or named in a string or `echo`, is missed or counted.                                                                                                                                                      | **Fix**: the list is data, `scripts/qa-gates.tsv`, read by the runner, by `qa-changed` and by the inventory gate                                                                                                                                                |
| B11 | `QA` L103-1389                       | Gates run in the caller's directory (only two `cd` to the root): `cd /tmp && /path/scripts/qa-all.bash` runs `qa-bash.bash` and 80 more in `/tmp`; whether each copes is an accident of that script.                                                                                                                                                                             | **Fix**: every gate runs with the repository root as its directory                                                                                                                                                                                              |
| B12 | `QA` L235-243                        | `deployed-drift` passing with **empty output** prints a blank line and no stage line: a gate that stopped printing is indistinguishable from one that ran, the defect its own header is about.                                                                                                                                                                                   | **Fix**: a pass with no `deployed-drift:` stage line is a failure, `✗ deployed-drift: ran, but printed no stage line`                                                                                                                                           |
| B13 | `QA` L1432-1445                      | The `FAILED` block names failed *hard gates* only. A failed *stage* with an empty `.failures` (a crash) prints `✗ QA FAILED: 0 errors in N files` and no name at all.                                                                                                                                                                                                            | **Fix**: a second line `  <K> stage(s) failed: <names>` beside the gates line; the existing lines are unchanged                                                                                                                                                 |
| B14 | `QA` L1-14 (header), L18-23          | The header counts rot: "37 stage lines: 27 + 9 + 1" (the file has 63, 11 and 1), and `TH` carried "21 gates" in four places.                                                                                                                                                                                                                                                     | **Fix** by deletion: counts are not stated in prose; the data and the stage lines carry them                                                                                                                                                                    |
| B15 | `QA` L28, L1405                      | A host without `go` ran all but the `go` gate and `bash-limits`, which failed; the Go port needs `go` before it can run anything.                                                                                                                                                                                                                                                | **Fix**, as `qa-changed`: exit 2 from the wrapper with the play-golang hint. Fail fast, and `go` is already required by two gates                                                                                                                               |
| B16 | `QA` L103-1389                       | A gate that cannot be started at all (the interpreter is not on PATH) is `bash` exit 127 or a `cd`/`python3` failure inside `$(...)` and reads as an ordinary failed gate, with the shell's message as its capture.                                                                                                                                                              | **Keep** as a failure; in Go it is a failure whose capture is the start error, same shape                                                                                                                                                                       |
| B17 | `QA` L137-206                        | The seven stages run live and uncaptured, so a stage's `print` can forge a stage line of its own (the helper-tests gate captures its child for exactly this reason).                                                                                                                                                                                                             | **Keep**: the stage's own line is its verdict, there is no summary to read from it; the port streams them in the true order                                                                                                                                     |
| B18 | `QA` L138-141 etc.                   | A stage exit 2 aborts without listing the hard gates that already failed; the earlier `✗` lines are all the record.                                                                                                                                                                                                                                                              | **Keep**: the abort is the stated exception; `VD` reads it as a terminal state                                                                                                                                                                                  |
| B19 | `QA` L1405                           | The report is built by `jq`, a dependency of this script as well as of the stages.                                                                                                                                                                                                                                                                                               | **Fix**: built in Go (the stages still need `jq` themselves)                                                                                                                                                                                                    |

| B20 | `HS` L78-202 | The helper-tests counts reader's refusals print `helper_counts_summary: ...` (a function that no longer exists) with bash `%q` quoting, and a token mismatch over two lines. | **Fix** (cosmetic): `helper-tests counts: ...` with Go quoting on one line; every refusal and the failed stage line are unchanged, and every message substring `TH` asserted is kept |

Found by the review of the port (`subagent-reports/261014-fable-review-qa-all.md`):

| #   | Where                | What the bash did                                                                                                                                                                            | Decision                                                                                                                                                                                                                                          |
| --- | -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B21 | `QA` count gates     | A suite that exits 0 after `passed: 0 failed: 0` printed `✓ <name>: passed: 0`: case discovery that matched nothing, the same defect as the missing count line (B2), was a green tick.       | **Fix**: zero cases is "could not run", it proved nothing: `✗ <name>: exited 0 but counted 0 passed cases ('passed: 0'), so it proved nothing`. One reader, `gates.CaseCount`, shared with `qa-changed` (SPEC-qa-changed B31)                     |
| B22 | `QA` count gates     | A suite that exits 0 while its count line says `failed: 2` (a summary that forgot its `exit 1`) printed `✓ <name>: passed: 3`.                                                               | **Fix**: a `failed: <m>` above zero on the counted line fails the gate: `✗ <name>: exited 0 but its count line reports failed cases: 2 failed`. Same reader (SPEC-qa-changed B32)                                                                 |
| B23 | `QA` L137-206        | A stage's own output (or a failed gate's capture, or the drift gate's line) can contain a line that opens `✓ QA passed:`, which anything greping stdout for the verdict takes for the run's. | **Fix**: a line from a gate that opens like the run's verdict (`✓`/`✗`/`⚠` then `QA `) is indented two spaces; every other byte, stage lines included, is untouched, so `verdicts.py` still reads them. The closing verdict is the only such line |
| B24 | `QA` gate invocation | A gate script that is gone was one failed gate (`bash` status 127) and every other gate still ran.                                                                                           | **Fix**: the table is validated before the first gate: a script that is not a file is `ERROR: qa-all: ...`, exit 2, no gate run (section 11.3). A table that names a script that does not exist is a defect to fix, not a gate to report          |
| B25 | `QA` `$(...)`        | A gate that exited but left a background child holding its output pipe hung `$(...)` for ever, and no limit applied (B8 bounds it only in Go).                                               | **Fix**: after the grace period the output the gate wrote is kept, the child's process group is killed, and the gate fails: `✗ <name>: <text> (left a process running that held its output open)`                                                 |
| B26 | `QA` exit-2 aborts   | The Go runner's own aborts (`unknown argument`, `cannot write the report`, an unreadable table) printed bare `ERROR: <text>`, which `verdicts.py` did not read as the end of a run.          | **Fix**: each is `ERROR: qa-all: <text>`, and `TOOL_ABORT` matches the prefix                                                                                                                                                                     |
| B27 | `QA` exit-2 aborts   | The `ansible-syntax` stage text for exit 2 told the reader to install `ansible-playbook`, but the Go gate exits 2 for six causes (a missing engine, `ansible.cfg` or vault password file, an unusable config dump, a broken playbook discovery, an unwritable report) and prints the true `ERROR:` line first, so the last line contradicted it. | **Fix**: the row names every cause and points at the line above; it still opens `ERROR: ansible-syntax gate could not produce` so `TOOL_ABORT` reads it. Pinned in `testdata/deviations.json` (`exit2-ansible-syntax`) |
| B28 | `QA` exit-2 aborts   | The `bash` stage text for exit 2 said `Missing required tools. Install them and re-run.`, but `qa-bash` exits 2 for more than a missing tool (discovery missing a tracked script, shellcheck printing no JSON array, or the suppression ratchet finding no base) and prints the true `ERROR:` line first, so the last line contradicted it (the same defect as B27). | **Fix**: the row names the failure and points at the line above; it still opens `ERROR: bash gate could not produce` so `TOOL_ABORT` reads it. Pinned in `testdata/deviations.json` (`exit2-bash`) |
| B29 | `QA` exit-2 aborts   | The `python` stage text for exit 2 said `Missing required tools. Install them and re-run.`, but `qa-python` exits 2 for more than a missing tool (no `RUFF=` pin, ruff not listing a file, or a template holding Jinja statements) and prints the true `ERROR:` line first, so the last line contradicted it (the same defect as B27). | **Fix**: the row names the failure and points at the line above; it still opens `ERROR: python gate could not produce` so `TOOL_ABORT` reads it. Pinned in `testdata/deviations.json` (`exit2-python`) |
| B30 | `QA` exit-2 aborts   | The `patterns` stage text for exit 2 said `Missing required tools. Install them and re-run.`, but `qa-patterns` exits 2 for more than a missing tool (the rule self-test failing or a script not parsed) and prints the true `ERROR:` line first, so the last line contradicted it (the same defect as B27). | **Fix**: the row names the failure and points at the line above; it still opens `ERROR: patterns gate could not produce` so `TOOL_ABORT` reads it. Pinned in `testdata/deviations.json` (`exit2-patterns`) |
| B31 | `QA` exit-2 aborts   | The `js` stage text for exit 2 said `Missing required tools. Install them and re-run.`, but `qa-js` exits 2 for more than a missing tool (a missing node or `node_modules`, an ESLint run that could not start, or an unwritable report) and prints the true `ERROR:` line first, so the last line contradicted it (the same defect as B27). | **Fix**: the row names the failure and points at the line above; it still opens `ERROR: js gate could not produce` so `TOOL_ABORT` reads it. Pinned in `testdata/deviations.json` (`exit2-js`) |
| B32 | `QA` exit-2 aborts   | The `docs` stage text for exit 2 said `zero-file scan or checker crash`, but `qa-docs` is one program (no checker to crash) and exits 2 for an empty scope, an unread or unparsed file, a broken discovery, an unwritable report and more, printing the true `ERROR:` line first, so the last line named the wrong thing (the same defect as B27). | **Fix**: the row names the failure and points at the line above; it still opens `ERROR: docs gate could not produce` so `TOOL_ABORT` reads it. Pinned in `testdata/deviations.json` (`exit2-docs`) |
| B33 | `QA` the go gate     | The `go` gate read `GO-OK <n> package(s)` from the interim `qa-go.bash`. It runs go-qa-ci, which prints a summary table, so the gate reads the `coverage` step's `passed` and its failed text names go-qa. Every golden scenario shows the `go` stage line, so each is pinned as deviation B33.                                                    |

Which bugs the golden shows and which a unit test does: B1-B6, B12, B13, B20-B23 and B26-B33 differ in
the recorded output and are pinned in `testdata/deviations.json`. B7-B11 (the report path, the
timeout, the interrupt, the table as data, the working directory), B14-B19, B24 and B25 are not
visible in a recorded stub run, so each has its own Go test (`run_test.go`, `gate_test.go`,
`guard_test.go`, `config_test.go`, `report_test.go`, `proc_test.go`, the real-tree tests).
B7 reaches the stage that names the report: `qa-bash.bash` takes the path from
`QA_RESULTS_JSON`, as the runner does (`realtable_test.go` pins it).

K1 (**Keep**): a passing gate's capture is discarded, including its warnings. K2: hard-gate status
1 and 2 are the same failure (the repo's exit-2 convention is for the stages; `qa-changed`'s "could
not run" wording would change the `✗` line shape `VD` reads). K3: sequential, in file order; no
parallelism (decision D2 of SPEC-qa-changed, still open).

---

## 11. Go design

### 11.1 Where it lives

The nested `src-go/qa` module, as `qa/changed`:

```
src-go/qa/gates/            the data and the readers, shared (imported by changed and all)
    gates.go                scripts/qa-gates.tsv: load, strict validation, the population
    readers.go              case count, detail, the counts-file reader
src-go/qa/all/              the runner (package all)
    config.go               flags, Env, exit codes, the timeout
    run.go                  Main, the loop, the verdict
    gate.go                 the four kinds
    report.go               the machine report
src-go/qa/cmd/qa-all/main.go
scripts/qa-gates.tsv        the 92 rows
scripts/qa-all.bash         the wrapper
```

The process seam is `qa/internal/proc`. It gains two optional writers on `Cmd` (`StdoutTo`,
`StderrTo`): when set, the stream is written there as it is produced instead of captured. That is
how a stage's output reaches the terminal live and in order without a second way to start a process.

### 11.2 `scripts/qa-gates.tsv`

Tab-separated, `#` comments, strict (a short row, an unknown kind, a duplicate name, a bad
pattern, a missing script, a reader on the wrong kind is exit 2 with the line number):

```
kind  <TAB> name <TAB> command <TAB> text <TAB> reader [<TAB> reader ...]
```

- `command`: space-separated argv relative to the root; a first word ending `.bash` under
  `scripts/` runs through `bash`, anything else (`python3 -m helpers.x`) as written.
- `text`: for `gate`, `drift` and `helpers`, the reason on the `✗` line; for `stage`, the exit-2
  message.
- readers (`gate` rows only): `count`, or `detail:<ERE>`; several are joined `; `.

`qa-changed`'s population is the script rows of this file minus the per-file gates, in file order
(B10; SPEC-qa-changed B9 is closed).

### 11.3 Behaviour

- Flags as section 2. Loads the table, validates every script exists (B24: exit 2 before any gate
  runs), clears the report path (B6). A script row's extra words are the script's arguments and
  reach it, in order.
- Runs the rows in order; each under the per-gate timeout (B8); each recorded.
- Hard gate: section 4.1 with B2, B3, B11, B21, B22 and B25. The reader answer is a typed result,
  never a word.
- Drift: 4.3 with B12. Helpers: 4.4 verbatim. Stage: 4.2 with B4, B5, B8; the exit-2 abort cleans up.
- Verdict and report as sections 7.3 and 8, with B6, B7, B13, B19.
- Stdout and stderr stay separate streams end to end.

### 11.4 Tests

Go tests beside the code under `qa-go.bash`. A fake runner scripts each gate's status, output and
JSON. Cases: the loader (every refusal); each reader (`TH`'s corpus); the counts reader; one gate of
each kind, passing and failing, byte for byte; every gate failing in turn with all later gates
still run; two failures; the stage abort; timeouts and interrupt; the report (merge, B4, B5, atomic
write); the verdict text; flags. Two tests read the real tree: every row's script exists and its
patterns compile; the population `qa-changed` reads is the rows of the real file.

---

## 12. Parity and cut-over

### 12.1 Parity

1. **Golden capture of the bash from a git revision before it is deleted**
   (`qa-all-golden/capture.py`, the freeze and qa-changed method: record from a revision, never from a
   retained copy). A world is a directory of stub gates: one per row of the gate list (parsed from
   the legacy source at the revision), each logging its invocation to a file and printing a scripted
   output; a stub `python3` for the two module gates; a stub `qa-helper-tests.bash` that writes the
   counts file for the token it is handed. Scenarios: all pass; each of the 92 gates failing alone
   (and, for stages, exiting 1 and 2); two failures; every reader shape (count, detail, three
   details); the counts file in each way it can be wrong and stdout noise; drift pass, skip and
   fail; a detail that does not match; a suite with no count; a stage with no report; stage output
   passed through; arguments. A record is stdout, stderr, status, the invocation log and the merged
   report. The Go test replays the corpus through the real runner with the fake and compares bytes;
   a difference must be a numbered bug pinned in `testdata/deviations.json`.
2. **The real tree**: `scripts/test-qa-changed.bash` (population from the data), the Go real-tree
   tests, and a run of each stage and of the cheap hard gates through the new command in the
   worktree (never the full gate: that is the coordinator's).

### 12.2 Cut-over

One feature branch. (1) Spec, committed alone. (2) The Go port with its golden and tests, bash
untouched. (3) The cut-over commit: the data file, the wrapper, the deletions (`lib/qa-helper-summary.bash`,
`test-qa-helper-summary.bash`, `gate_call_sites.py` and its test), `qa-changed` reading the data,
`link_check` reading the data, the allowlist entries removed with `qa-bash-limits.bash --update`
(never folded to fit), `CLAUDE/QA.md`, `src-go/CLAUDE.md`, the plan text. Rollback is a `git revert`
of the cut-over commit; no bash fallback is kept.

### 12.3 Risks the port must hold

- The push gate becomes unavailable if the nested module does not build. The wrapper says so
  loudly (exit 2, the compiler output); `qa-go.bash` is a separate script and diagnoses it.
- Do not widen the contract: section 7 is what `VD`, CI and the daemon read.
- The gate list is now the one place that says what runs. Adding a gate is a row; the QA.md
  inventory gate and `qa-changed` follow it.

---

## 13. Decisions taken (the coordinator's defaults for the previous port, applied again)

- D1: 30 minute per-gate limit, `QA_ALL_TIMEOUT`.
- D2: sequential.
- D3: a hard gate's status 2 is a failure (K2); the stages keep exit 2 as the one abort.
- B2/B3/B5/B12 turn four silent passes into failures; each was checked against the live tree and,
  where the live tree trips one (B2), the offending suite is fixed in the same change.
