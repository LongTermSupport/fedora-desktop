# qa-patterns: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md), after [SPEC-qa-ansible.md](SPEC-qa-ansible.md).
**The legacy bash is not an oracle.** This states the *intended* behaviour as numbered, testable
expectations; `scripts/qa-patterns.bash` (196 code lines, 330 with comments, an associative array
and eight `jq` calls) was read only to find the requirements. Where it was wrong, the expectation
says what is right and section 3 lists the departure. Proof is the Go tests in
`src-go/qa/patterns` (cited by number) and a run of the Go tool and the bash over the live tree.

## 1. Role

The pattern gate for repository-owned shell scripts. The *rules* are data: semgrep rules in
`.semgrep/bash-conventions.yml` (all `pattern-regex`, with a `paths.include` list each), proven
against the annotated fixture `.semgrep/bash-conventions.bash`. semgrep stays the engine that
applies them (PCRE2 `pattern-regex` with `pattern-not-regex` filtering is semgrep's, and
re-implementing it would fork the rule semantics); the Go tool owns everything around it: what is
scanned, the proof the rules still work, running semgrep, and refusing a pass over code the
analyser did not read. Callers: `qa-all` (`scripts/qa-gates.tsv`, stage `patterns`: run with
`QA_JSON_OUT`, reads `.summary.{total,passed,failed}` and `.failures`; exit 2 aborts the run) and
people. `qa-changed` runs this gate whole when the rules, the fixture, the tool or its wrapper
change (section 4, B33), besides its own per-file semgrep check.

## 2. Expectations

### Command line and environment

- P1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument, or a flag
  missing its value, is exit 2 with `ERROR:` and the usage on stderr. *(Bash ignored arguments.)*
  The root is resolved by the one shared helper `qa/internal/rootpath` (absolute, clean, symlink-free; a root that cannot be resolved is exit 2), and a symlinked-root test pins that no checkout path reaches stdout or the report.
- P2. `QA_JSON_OUT` is the report path; unset or empty is `/tmp/qa-patterns-results.json`. The
  report cannot be written: `ERROR: cannot write the report <path>: <why>`, exit 2.
- P3. Exit codes: 0 no finding, 1 at least one, 2 the run could not complete. Every exit-2
  message is on stderr and starts `ERROR:`. A failed write to stdout or stderr is exit 2.

### The tools and the proof the rules work (exit 2, no report)

- P4. semgrep must run (`semgrep --version` exits 0), else
  `ERROR: semgrep not found. Install with: pipx install semgrep`.
- P5. The rules file and the fixture must both exist and be regular files, else exit 2 naming
  the missing one. Then `semgrep --test --config <rules> <fixture> --metrics=off` must exit 0;
  otherwise `ERROR: semgrep rule self-tests failed (see .semgrep/bash-conventions.bash)` followed
  by semgrep's own output, exit 2. A rule that stops matching is worse than a missing tool.

### Which files: what git would publish

- P6. The candidates are the files in `git ls-files -z --cached --others --exclude-standard`
  from the root; a gitignored file is never read, counted or reported. Tracked files deleted from
  the work tree (`git ls-files --deleted`) are not candidates. `git` failing, or the root not a
  work tree, is `ERROR: cannot list the files git would publish under <root>: <why>`, exit 2.
  *(Bash used `find`, so ignored files, build output and other checkouts' files were scanned.)*
- P7. Excluded trees are the `shell` rows of `scripts/qa-trees.tsv` (`shell<TAB>dir`): a path is
  excluded when `/dir/` occurs as whole segments anywhere in `/<path>/`. A malformed row, an
  unreadable file, or no `shell` row at all is exit 2: an empty exclusion list would scan trees
  that are not ours, and a reader that finds nothing proved nothing.
- P8. A candidate is a shell script when its name ends `.sh` or `.bash`, or, with any other name,
  its first line is a shebang whose interpreter is `bash` or `sh` (directly, or after `env` and
  its options and `NAME=value` words) without a `-c` argument (a `bash -c` shebang is an inline
  one-liner in another kind of file, the playbooks'). A `*.j2` template is never a script here (it
  is not shell until rendered). Mode bits play no part. A candidate that cannot be read is exit 2
  naming it. A directory or submodule entry is not a script. A symlink to a shell script is judged by
  where its target is: a target that is itself among the scripts to scan is scanned under its own
  name, and the link is counted in the coverage line (P16) as not scanned in its own right; a
  target that is not (gitignored, in an excluded tree, outside the root) is scanned under the
  link's name, and the coverage line (P16) says so by naming the link. The gate never claims a
  target is scanned unless it is. A symlink named `*.sh`/`*.bash` that cannot be followed is exit 2 naming it.
  A path git lists that the tree lacks and `--deleted` does not name is exit 2
  (`ERROR: cannot inspect <path>`). *(Bash's find matched `-type f` only and said nothing of symlinks.)*
- P9. No script found is exit 2: this repository always has some, so discovery is broken.

### The mirror

- P10. semgrep decides a file's language from its extension (and reads a shebang only when the
  owner-execute bit is set), so the scripts are copied into a temporary mirror at their own
  repo-relative path, `.bash` appended where there is no `.sh`/`.bash` extension. The relative
  path is kept so each rule's `paths.include` applies as on the real tree. Line endings are
  normalised to LF in the copy (`\r\n` becomes `\n`; line numbers are unchanged), so a `$`-anchored
  rule matches a CRLF script as it does an LF one. The mirror holds an empty `.semgrepignore`, so
  semgrep's built-in ignores (`tests/`, `vendor/`, `build/`, ...) do not drop a script the gate
  discovered. Two scripts claiming one
  mirror path (`dir/foo` and `dir/foo.bash`) are exit 2 naming both. The mirror is removed on every
  exit. Failure to build it is exit 2.

### Running semgrep and judging what came back

- P11. The rules file is copied beside the mirror (outside it, so it is never scanned), and semgrep
  runs in the mirror: `semgrep --config ../rules.yml --json-output <file> --metrics=off --quiet --max-target-bytes=0 .`
  (no size limit: a large script is scanned, not skipped). A rules file outside the scan root makes
  semgrep prefix every rule id with the dotted path of the file, which would put the checkout's
  path in the output, so the rule id of a finding is the bare id from the rules file. Any exit status but 0 is exit 2 with semgrep's stderr (semgrep without `--error`
  exits 0 on findings, so any other status is semgrep itself failing). No JSON file, an empty one,
  or one that is not the expected JSON is exit 2. *(Bash let status 1 through.)*
- P12. Every discovered script must come back in `paths.scanned`; otherwise
  `ERROR: semgrep did not scan <N> discovered shell script(s):` and the list, exit 2. A pass over
  files the analyser never opened is not a pass.
- P13. A semgrep error of type `Syntax error` on a file means no rule ran on it: exit 2 listing
  the files (`ERROR: semgrep could not parse <N> file(s), so NO rule ran on them:`). There is no
  list of tolerated files. *(Bash carried an always-empty `SEMGREP_CANNOT_PARSE` list that had to
  be kept in step in both directions; YAGNI, deleted. A file that cannot be parsed is fixed.)*
- P14. `PartialParsing` errors are reported, not gating (every rule is a regex over raw text, so
  skipped parse ranges cost nothing today): per file, the lines outside the parse tree as the
  union of the reported ranges, the file's total lines, and the first and last line of the gaps.
  Any other error type (a timeout, a rule error, ...) is exit 2 naming the type and file (`(no path)` for an error semgrep gives no file), because
  the file was not fully analysed. *(Bash counted such errors into the partial-parse note and never gated on them.)*

### Output and report

- P15. A finding is a rule match: path (mapped back from the mirror to the real path), line,
  rule id, message. Findings are ordered by file, then line, then rule id. A file is *failed* when
  it has at least one finding.
- P16. Every run, pass or fail, prints the coverage lines before the verdict: when
  symlinks were left out because their targets are scanned, `⚠ patterns: <N> symlink(s) not scanned in their own right (their targets are, under their own names)`; when a link's target is not among the scripts, `⚠ patterns: <N> symlink(s) scanned under their own name, their targets being outside the scanned set:` and one `    <link> -> <target>` line each; for P14, `⚠ patterns: <N> file(s) semgrep parsed only in part (regex rules unaffected):` and one line per file `    <path> — <skipped> of <total> lines outside the parse tree (first gap from <a>, last to <b>)`.
- P17. No finding: `✓ patterns: <N> files OK`, exit 0. Otherwise
  `✗ patterns: <F>/<N> files failed → <report path>` and per failed file `  ✗ <path>` then
  `    <rule>:<line> <message>; ...` (messages whitespace-trimmed and folded onto one line), exit 1.
  `N` is the number of scripts scanned.
- P18. The report is `{type:"patterns", status:"pass"|"fail", scanned:[...paths], summary:{total, passed, failed}, failures:[{file, type:"patterns", status:"fail", error}]}`, two-space JSON;
  `total` = N, `failed` = failed files, `passed` = total - failed. `error` is the file's findings
  as `<rule>:<line> <message>` joined by `; `. It is written on exit 0 and 1, never on exit 2.

## 3. Where the Go departs from the bash

| #   | bash behaviour                                                   | Go                             | expectation |
| --- | ---------------------------------------------------------------- | ------------------------------ | ----------- |
| B1  | arguments ignored                                                | usage / exit 2                 | P1          |
| B2  | scope by `find`: ignored files and build output scanned          | what git would publish         | P6          |
| B3  | a dangling `*.sh`/`*.bash` symlink invisible to `find -type f`   | exit 2 naming it               | P8          |
| B4  | `#!/usr/bin/env sh` missed; `#!/…/bashful` matched               | interpreter parsed             | P8          |
| B5  | symlinks skipped silently                                        | counted in the coverage line   | P8, P16     |
| B6  | semgrep exit 1 accepted                                          | any non-zero is exit 2         | P11         |
| B7  | non-`Syntax error` semgrep errors only counted, never gating     | other types exit 2             | P14         |
| B8  | `SEMGREP_CANNOT_PARSE` exemption list (empty, two-way assertion) | deleted; unparseable is exit 2 | P13         |
| B9  | deterministic only by semgrep's order                            | sorted                         | P15         |
| B10 | exclusion list read by `lib/qa-trees.bash`; empty list scans all | no `shell` row is exit 2       | P7          |
| B11 | unwritable report: shell error exit 1                            | message, exit 2                | P2          |
| B12 | failed stdout write unchecked                                    | exit 2                         | P3          |
| B13 | rule id printed with the checkout path; `tests/`, `vendor/` scripts and files over 1 MB dropped by semgrep, CRLF evading `$` rules | bare rule id; ignore list emptied, no size limit, LF in the mirror | P10, P11 |
| B14 | symlink coverage claimed a target scanned without checking       | target checked; otherwise the link is scanned and named | P8, P16 |

## 4. Go design and cut-over

`src-go/qa/patterns` (`run.go` command line, `discover.go` scope and classification, `mirror.go`,
`semgrep.go` run and judgement, `report.go`) and a thin `src-go/qa/cmd/qa-patterns`. Every process
(git, semgrep) goes through the injected `proc.Runner`, so the tests substitute a fake that records
argv and writes the JSON a real semgrep would; the real semgrep and git are exercised by the live
tree run recorded in the journal. `scripts/qa-patterns.bash` becomes a build-and-run wrapper over
`lib/go-tool.bash`; its allowlist row is removed. Routing: the gate row (`stage patterns`) and the
`perfile-gate` list keep naming the script; qa-changed gains a `patterns` check, run whole when the
tool, the rules, the fixture, the wrapper `scripts/qa-patterns.bash`, `scripts/lib/go-tool.bash` or
`scripts/qa-trees.tsv` change. The rules and fixture are covered by that check alone, and are not
also reported as skipped excluded-tree files.
