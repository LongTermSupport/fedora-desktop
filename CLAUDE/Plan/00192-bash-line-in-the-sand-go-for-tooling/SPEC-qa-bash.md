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
  that cannot be written: `ERROR: cannot write the report <path>: <why>`, exit 2. The report file has mode 0644, as the bash's
  umask gave it: another account reading it with `jq` must not be refused.
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
  shellcheck's defaults plus the severity bar of S11. **Stated limit:** these pins close every
  route *beside* a script. A directive written *inside* a script (`# shellcheck disable=`,
  `# shellcheck shell=`) is read by shellcheck whatever the flags say, and shellcheck has no switch
  to ignore it, so the gate scans for them itself (S16-S19). The time limits are in S8a. No `--severity` is passed: the info and
  style findings are kept for the report.

- S9. `--external-sources` is passed so a `source` directive is followed and a sourced library's
  definitions are seen. `qa-changed`'s shellcheck check is given the same `--norc` and
  `--external-sources` (with `--severity=warning`, its bar), so the same file has the same verdict in
  both. *(Bash passed neither flag and read a nested `.shellcheckrc`.)* **What the flag changes in
  the verdict:** a `source` target shellcheck cannot find stays SC1091 (info, advisory) as
  before; but a target it finds and cannot parse is SC1094 (`Parsing of sourced file failed. Ignoring it.`), level **warning**, which gates. The flag promotes the advisory SC1091 to a
  gating SC1094 on the sourcing script, so a vendored or daemon-owned tree excluded by
  `scripts/qa-trees.tsv` can fail this gate through any script that sources an unparseable file in
  it by a root-relative path or a `source=`/`source-path=` directive (shellcheck resolves a plain
  `source ./x` against the working directory, the root, not the script's directory). This is
  deliberate: a script that sources a file nothing can parse is broken. **Findings inside a
  followed file are not reported** by shellcheck (0.11.0): they appear only when that file is
  itself one of the files given.

- S10. Status 0 or 1 is data; 2 or more, no status, or a timeout is `ERROR: shellcheck invocation failed (status N):` with its stderr, exit 2. Its stdout must be a JSON array, else exit 2; empty
  stdout is not an array. Each diagnostic keeps every field shellcheck gave it, with `file` made
  relative to the root, and the list is the report's `shellcheck_diagnostics`. A diagnostic on a
  file that was not given is exit 2: shellcheck and the gate disagree about what was checked.
  **Not reachable with shellcheck 0.11.0**, which reports only on the files it was given even
  when it follows a source (S9). The check is kept, not removed: a shellcheck that did report
  inside a followed file would otherwise either drop those findings silently or fail a file the
  exclusion table promised not to judge, and exit 2 makes the disagreement loud. It is proven
  through a fake shellcheck (`foreign file` in `TestShellcheckStatusAndOutputAreChecked`), the only
  way to reach it. A timeout is S8a. Stdout that is empty, `null` or any JSON that is not an array
  is not an array (`null` decodes to an empty list without error, so the leading `[` is part of
  the check and not redundant).

- S11. Level `error` and `warning` gate; `info` and `style` do not, and are reported as an advisory.
  *(The measured reason: `SC2155` is warning-level and is this repository's own defect class,
  while the info/style findings are dominated by intentional patterns.)* A level that is none of
  the four is exit 2.

- S8a. Time limits. The two probes (S4) and each `bash -n` (S7) have one minute each; the one
  shellcheck process (S8) has **fifteen minutes** for all files together. A process still running
  at its limit is killed and is exit 2 naming the limit: `ERROR: shellcheck did not finish within 15m0s` (for `bash -n`, `bash -n <file>: bash did not finish within 1m0s`). Because every file
  shares the one process and its clock, one pathological file aborts the whole stage rather than
  failing as a file, and no report is written. A timeout is named as a timeout, not as a status.

### Output and report

- S12. A file is *failed* when it does not parse, has at least one gating finding, or has a
  directive finding (S16-S19). Its `error` is
  the syntax message, then each gating finding as `SC<code>:<line> (<level>) <message>`, joined by
  `; ` in line order, then code. Results are in file order (sorted by path).
- S13. Stdout, in order: each syntax failure (S7); when there is any diagnostic of any level,
  `⚠ shellcheck: <N> issues (see <merged> .checks.bash.shellcheck_diagnostics)`; when any gates,
  `✗ shellcheck: <G> error/warning-level finding(s) — gating` and one line each, sorted by file,
  line, column, code: `    <file>:<line> SC<code> (<level>) <message>`.
- S14. No failed file: `✓ bash: <N> files OK`, exit 0. Otherwise `✗ bash: <F>/<N> files failed → <report>`, exit 1, F being the number of failed files.
- S15. The report is `{type:"bash", status:"pass"|"fail", summary:{total, passed, failed}, results:[{file, type:"bash", status, error?}], failures:[the failed results], shellcheck_diagnostics:[...]}`, two-space JSON. It is written on exit 0 and 1, never on exit 2.

### Suppression directives

QA.md bans suppression. The hooks daemon enforces that only at `Write`/`Edit` time in a session; a
heredoc, a human editor or a merge reaches disk past it, and shellcheck honours a directive inside
a script whatever its flags. The gate therefore enforces the ban itself, with a ratchet, as
`bash-limits` does.

- S16. Every gated file is read as text and scanned for shellcheck directives. A directive is a `#`
  that begins a word (at line start, or after a blank, `;`, `&`, `|`, `(`, `)` or `{`), optional
  blanks, `shellcheck`, a blank, then words of the form `key=value`; it may stand on a line of its
  own or trail code, and one comment may hold several keys. A *disable directive* has a `disable`
  key (several `disable` keys in one comment are one directive); its codes are the
  comma-separated values, normalised (`2086` and `sc2086` are `SC2086`; `all`; a range such as
  `SC2000-SC2100` is kept as one token). Every disable directive found anywhere is counted. The
  scan reads text, not syntax: a directive-shaped string inside a heredoc or quote is also found,
  and is listed or reworded like any other. `disable=all` (alone or among codes) is a finding on
  every run, even when the file is listed: it hides every check and is never allowable.
- S17. The ratchet is `scripts/shellcheck-disable.allowlist` (in the scripts directory the gate was
  given; an absent file is an empty list). Comment lines start `#`; a record is
  `path<TAB>count<TAB>codes`: the count of disable directives in the file and the sorted,
  comma-separated codes they name, none of them `all`. A malformed line, a duplicate path or an
  `all` code is exit 2. For each file with *n* directives and codes *C*, against its record: no
  record and *n* > 0 is a finding (unlisted); *n* above the count, or a code outside the record's
  codes, is a finding (grew); *n* or *C* below the record is a finding (tighten: lower the
  record); a record with *n* = 0, or for a path that is no longer a gated file, is a finding
  (stale: remove it). Records only move down. Against the base: when the list holds any record, it
  is compared with the copy at the merge-base of HEAD and origin's default branch (git, through
  the seam); an entry the base lacks, a higher count, or a code the base's entry lacks is a
  finding (added, raised). An unresolvable origin default branch is exit 2 naming the fix; a base
  with no allowlist file is the commit that introduces it and has nothing to compare. A list with
  no record is not compared at all, since nothing can have been added to it.
- S18. A `shell=` key that contradicts the file's shebang fails the file. The shebang's dialect is
  the basename of its interpreter (for `env`, its first word that is not an option or an
  assignment); any `shell=` value other than that dialect is a finding (`shell=bash` on
  `#!/bin/sh` hides the POSIX warnings of a file that still runs under sh). A `shell=` that
  agrees, and any `shell=` on a file with no shebang (the dialect of a sourced library), is fine.
  This finding is never allowable.
- S19. Directive findings fail their file (S12): the file's `error` carries
  `directive:<line> <message>` after the syntax and shellcheck parts, and stdout carries one
  `✗ suppression: <file>:<line> <message>` per finding (`✗ suppression: <file>: <message>` when it
  has no line), after the shellcheck lines, in file then line order. A finding about a path that is
  not a gated file (a stale record) is its own entry in `results` and `failures`, so `total` and
  S14's file counts include it.
- S20. `--directives FILE...` runs only S16-S19 on the named files (root-relative; every argument
  after the flag is a file): no discovery, no `bash -n`, no shellcheck, no report, no base
  comparison, and each file judged as in S17 against its own record (unlisted, grew, tighten,
  stale). It is for `qa-changed`, whose `shell-directives` check calls it so a changed file gets
  the verdict the gate gives it. Stdout is the S19 lines; exit 0 none, 1 a finding, 2 the run could
  not complete (an unreadable file, a malformed allowlist). The flag with no file is a usage error.

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
| B13 | a disable directive or contradicting `shell=` hid any finding               | scanned, ratcheted by `shellcheck-disable.allowlist`          | S16-S20     |
| B14 | the report mode followed the umask                                          | mode 0644 stated and set                                      | S2          |

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
