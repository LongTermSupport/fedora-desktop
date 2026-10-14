# qa-bash: specification of the Go port

Task 3.3 of [PLAN.md](PLAN.md), after [SPEC-qa-python.md](SPEC-qa-python.md), whose shape it follows.
**The legacy bash is not an oracle.** This states the *intended* behaviour as numbered, testable
expectations; `scripts/qa-bash.bash` (110 code lines, 232 with comments, an associative array, five
`jq` calls) was read only to find the requirements. Where it was wrong, the expectation says what
is right and section 3 lists the departure. Proof is the Go tests in `src-go/qa/bash` and
`src-go/qa/cmd/qa-bash` (cited by number) and a run of the Go tool over the live tree.

## 1. Role

The shell gate: every shell script git would publish must parse (`bash -n`) and must have no
`shellcheck` finding of level error or warning. `bash` and `shellcheck` stay the engines; the Go
tool owns what is checked, the proof that nothing tracked was left out, how the engines are
invoked (so no file beside the script can change the rules), and the judgement. Callers: `qa-all`
(`scripts/qa-gates.tsv`, stage `bash`: run with `QA_JSON_OUT`, reads `.summary.{total,passed,failed}`
and `.failures`; exit 2 aborts the run) and people. `qa-changed` runs `bash -n` and shellcheck per
file itself, with the same options (S9), and runs this gate whole when the tool or its wrapper
change.

## 2. Expectations

### Command line and environment

- S1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. The root is made absolute and cleaned once, so a relative `--root`, a relative
  `--scripts` and a trailing slash behave as the absolute path does. `-h`/`--help` prints the usage
  to stdout, exit 0. Any other argument, or a flag missing its value, is exit 2 with `ERROR:` and
  the usage on stderr. *(Bash ignored arguments.)*
- S2. `QA_JSON_OUT` is the report path; unset or empty is `/tmp/qa-bash-results.json`. A report
  that cannot be written: `ERROR: cannot write the report <path>: <why>`, exit 2.
  `QA_RESULTS_JSON` (unset or empty: `/tmp/qa-results.json`) is only named in the advisory line
  (S13): it is where qa-all keeps the merged diagnostics.
- S3. Exit codes: 0 no finding, 1 at least one, 2 the run could not complete. Every exit-2
  message is on stderr and starts `ERROR:`; no report is written on exit 2. A failed write to
  stdout or stderr is exit 2, and so is a closed stdout (`>&-`).

### The tools

- S4. shellcheck must run: `shellcheck --version` exits 0 and prints a `version: <v>` line, else
  `ERROR: shellcheck not installed` naming `playbooks/imports/play-devtools.yml` and saying not to
  install it by hand, exit 2. `bash --version` must exit 0, else `ERROR: bash not installed`,
  exit 2. **Stated limit:** shellcheck's version is not judged here; `scripts/qa-toolchain.bash`
  asserts it against `.qa-versions` (`SHELLCHECK=`) in the same `qa-all` run, and its verdict is
  the place a drift is reported.

### Which files: the shared discovery

- S5. Which files are checked is defined once, by [SPEC-qa-discovery.md](SPEC-qa-discovery.md): the
  gate asks `qa/discovery` for its `shell` list (what git would publish, outside the `shell` rows of
  `scripts/qa-trees.tsv`, a file being shell by its `.sh`/`.bash` name or a shell shebang). It
  adds nothing to that definition. A failure of the discovery (a bad trees table, git failing, an
  unfollowable candidate, no shell file at all) is exit 2 with its message after
  `ERROR: discovery of <list> failed:`; nothing is checked and no report is written.
- S6. Coverage guard: every path of the `tracked-shell` list must be in the `shell` list, else
  `ERROR: discovery missed <N> tracked shell script(s):`, the paths (sorted, one per line) and
  that a pass would vouch for scripts nothing read, exit 2. Both lists come from one predicate; the
  guard is the yardstick that notices a tracked script git no longer publishes (deleted from the
  tree) or a discovery change that drops files.

### Syntax

- S7. Each file is given to `bash -n -- <path>` as its own process, run from the root with the path
  relative to it, stdout and stderr merged. Status 0: the file passes. Status 2 (bash's status
  for a syntax error): the file fails, and the merged text, trimmed, is its message; the message
  therefore names the file by its path relative to the root and never carries the checkout's path.
  Any other status (a script bash could not open or run), or a process that cannot start or
  times out, is exit 2 naming the file. A failure is printed `✗ bash: <rel>: <message>`.

### shellcheck

- S8. One shellcheck process is given every file: `shellcheck --norc --external-sources --format json -- <relative paths>`, run from the root with `SHELLCHECK_OPTS=` set empty. `--norc` and the
  empty `SHELLCHECK_OPTS` mean no `.shellcheckrc` (the root's, one beside a script, the user's) and
  no environment option can disable a check or change the rules for a subtree; the gate's rules are
  shellcheck's defaults plus the severity bar of S11. No `--severity` is passed: the info and
  style findings are kept for the report.
- S9. `--external-sources` is passed so a `source` directive is followed and a sourced library's
  definitions are seen. `qa-changed`'s shellcheck check is given the same `--norc` and
  `--external-sources` (with `--severity=warning`, its bar), so the same file has the same verdict in
  both. *(Bash passed neither flag and read a nested `.shellcheckrc`.)*
- S10. Status 0 or 1 is data; 2 or more, no status, or a timeout is `ERROR: shellcheck invocation failed (status N):` with its stderr, exit 2. Its stdout must be a JSON array, else exit 2; empty
  stdout is not an array. Each diagnostic keeps every field shellcheck gave it, with `file` made
  relative to the root, and the list is the report's `shellcheck_diagnostics`. A diagnostic on a
  file that was not given (a sourced file outside the checked set) is exit 2: shellcheck and the
  gate disagree about what was checked.
- S11. Level `error` and `warning` gate; `info` and `style` do not, and are reported as an advisory.
  *(The measured reason: `SC2155` is warning-level and is this repository's own defect class,
  while the info/style findings are dominated by intentional patterns.)* A level that is none of
  the four is exit 2.

### Output and report

- S12. A file is *failed* when it does not parse or has at least one gating finding. Its `error` is
  the syntax message, then each gating finding as `SC<code>:<line> (<level>) <message>`, joined by
  `; ` in line order, then code. Results are in file order (sorted by path).
- S13. Stdout, in order: each syntax failure (S7); when there is any diagnostic of any level,
  `⚠ shellcheck: <N> issues (see <merged> .checks.bash.shellcheck_diagnostics)`; when any gates,
  `✗ shellcheck: <G> error/warning-level finding(s) — gating` and one line each, sorted by file,
  line, column, code: `    <file>:<line> SC<code> (<level>) <message>`.
- S14. No failed file: `✓ bash: <N> files OK`, exit 0. Otherwise `✗ bash: <F>/<N> files failed → <report>`, exit 1, F being the number of failed files.
- S15. The report is `{type:"bash", status:"pass"|"fail", summary:{total, passed, failed}, results:[{file, type:"bash", status, error?}], failures:[the failed results], shellcheck_diagnostics:[...]}`, two-space JSON. It is written on exit 0 and 1, never on exit 2.

## 3. Where the Go departs from the bash

| #   | bash behaviour                                                              | Go                                                            | expectation |
| --- | --------------------------------------------------------------------------- | ------------------------------------------------------------- | ----------- |
| B1  | arguments ignored                                                           | usage / exit 2                                                | S1          |
| B2  | shellcheck read any `.shellcheckrc` above a script and `SHELLCHECK_OPTS`    | `--norc` and an empty `SHELLCHECK_OPTS`                       | S8          |
| B3  | no `--external-sources`, unlike `qa-changed`                                | passed in both                                                | S9          |
| B4  | scope and yardstick by a sourced bash library through `jq` and arrays       | the `qa/discovery` package, in process                        | S5, S6      |
| B5  | `bash -n` messages and diagnostics carried the checkout's absolute paths    | paths relative to the root                                    | S7, S10     |
| B6  | a `bash -n` status other than a syntax error (unreadable file) was a "fail" | exit 2                                                        | S7          |
| B7  | shellcheck findings failed the stage with `failures: []`                    | the files with gating findings are failures, with their codes | S12, S15    |
| B8  | the failure count was syntax failures plus one for all of shellcheck        | the number of failed files                                    | S14         |
| B9  | unwritable report: shell error, exit 1                                      | message, exit 2                                               | S2          |
| B10 | failed stdout write unchecked                                               | exit 2                                                        | S3          |
| B11 | shellcheck output that was not JSON: a `jq` error                           | exit 2 with a message                                         | S10         |
| B12 | an unknown finding level was ignored                                        | exit 2                                                        | S11         |

## 4. Go design and cut-over

`src-go/qa/bash` (`run.go` command line and flow, `discover.go` the calls to the shared discovery
and the coverage guard, `syntax.go` the `bash -n` step, `shellcheck.go` the invocation and its
output, `report.go`) and a thin `src-go/qa/cmd/qa-bash`. Every process (git, bash, shellcheck) goes
through the injected `proc.Runner`, so the unit tests substitute a fake that records argv, directory
and environment; the real shellcheck and bash are exercised by `cmd/qa-bash`'s tests over a real
work tree (a nested `.shellcheckrc` and a `SHELLCHECK_OPTS` that disable a check cannot hide it)
and by the live-tree run in the journal. `scripts/qa-bash.bash` becomes a build-and-run wrapper over
`lib/go-tool.bash`; its allowlist row is removed. Routing: the gate row (`stage bash`) keeps naming
the script; `qa-changed` runs the gate whole when the tool or its wrapper change.
