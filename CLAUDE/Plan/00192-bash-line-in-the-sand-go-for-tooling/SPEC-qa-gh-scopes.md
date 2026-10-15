# qa-gh-scopes: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md). Same method as [SPEC-qa-patterns.md](SPEC-qa-patterns.md):
**the legacy bash is not an oracle.** This states the *intended* behaviour as numbered, testable
expectations; `scripts/qa-gh-scopes.bash` (67 code lines, a `python3 -c` YAML read and three `jq`
calls) was read only to find the requirements. Where it was wrong the expectation says what is right
and section 3 lists the departure. Proof is the Go tests in `src-go/qa/ghscopes` (cited by number)
and a run of both tools over the live tree at cut-over.

## 1. Role

The `gh-scope-outside-ssot` gate. `vars/github-required-scopes.yml` is the one statement of which
GitHub OAuth scopes a token needs and `helpers/github_scopes/` the one implementation of how scopes
imply one another; the semgrep rule `.semgrep/gh-scopes.yml` (data), proven against
`.semgrep/gh-scopes.fixture`, reports every other place a scope is named. semgrep stays the engine;
the Go tool owns what is scanned, the proof the rule still works, and the refusal to pass over a file
the analyser did not read. The rule reads every file in every language, so scope is not a language
list. Callers: `qa-all` (`scripts/qa-gates.tsv`, gate `gh-scope-outside-ssot`, `count` reader of the
`passed: N failed: M` line) and people. The tool starts two programs, `git` and `semgrep`, both
through `qa/internal/proc`.

## 2. Expectations

### Command line, streams, exit codes

- G1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument, or a flag
  missing its value, is exit 2 with `ERROR:` and the usage on stderr.
- G2. Exit codes: 0 clean, 1 a finding or the rule failing its fixture, 2 the run could not complete.
  Every exit-2 message is on stderr and starts `ERROR:`. A failed write to stdout is exit 2 with
  `ERROR: cannot write to stdout: <why>`.
- G3. The root is resolved by `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable is
  exit 2 `cannot resolve the repository root`). No checkout path reaches stdout or stderr when the
  root is a symlink.

### Preconditions

- G4. semgrep not runnable (`semgrep --version` fails to start or exits non-zero) is exit 2,
  `ERROR: semgrep not found. Install with: pipx install semgrep (<cause>)`, before anything else
  runs; the cause is the start error or the exit code and stderr of `--version`.
- G5. The rule file `.semgrep/gh-scopes.yml` and the fixture `.semgrep/gh-scopes.fixture` must be
  regular files; either missing is exit 2 naming it.
- G6. The rule is proven on every run: `semgrep --test --config <rule> <fixture> --metrics=off`. A
  non-zero result is exit 1, `ERROR: gh-scope-outside-ssot failed its fixture (.semgrep/gh-scopes.fixture):`
  and semgrep's own output, on stderr, and no scan happens. A rule edited into matching nothing fails
  here and never reports a clean repository. `semgrep --test` exits 0 on a fixture with no
  annotations, so the fixture must also carry at least one `ruleid:` line; none is exit 2,
  `ERROR: .semgrep/gh-scopes.fixture marks no ruleid line; the self-test proves nothing`, before
  the test runs. A self-test that cannot start or times out is exit 2.

### What is scanned

- G7. **Scope: what git would publish.** The candidates are the files listed by
  `git ls-files -z --cached --others --exclude-standard` in the root. git failing is exit 2,
  `ERROR: cannot list the files git would publish under .: <why>`. Of those, only regular files are
  handed to semgrep: a symlink is left out (semgrep refuses it; its target is scanned in its own
  right or is vendored) and a listed file that is gone from the disk (a deleted, unstaged file) is
  left out. Any other failure to look at a file is exit 2 naming it.
- G8. No file left to hand over is exit 2, `ERROR: git named no files to scan; nothing was checked`.
- G9. The files are handed to semgrep **by name**, never as a directory (a directory scan applies
  semgrep's default ignore list, which skips `tests/` and would hide a copy there), run from the root
  so paths are relative: `semgrep --config <rule> --json --quiet --metrics=off --max-target-bytes=0 <files...>`.

### Judging semgrep's answer (nothing unread is a pass)

- G10. semgrep exiting 2 or more, a timeout, or a stdout that is not a JSON report with `paths` is
  exit 2 with semgrep's stderr. Exit 0 and 1 are both a report.
- G11. Any entry in the report's `errors` is exit 2 listing path and type: a file semgrep could not
  fully read is not a clean file.
- G12. **Coverage.** The files expected to be read are the handed files that no `paths.exclude` glob
  of the rule matches (semgrep drops those without saying so). The globs are read by decoding the rule
  file as YAML (`rules[].paths.exclude`, every rule's list together). A rule file that does not
  decode, or whose `paths.exclude` is not a list of strings, is exit 2 naming it. A rule with no `paths.exclude` is exit 2 (the gate is built around the rule's own excludes;
  an unparseable or emptied list must not silently widen what is expected). Every expected file must
  be in `paths.scanned` or `paths.skipped`; any other is exit 2 listing them, `A pass reported over files the analyser never opened is not a pass`. A glob uses semgrep's meaning, which is a gitignore
  pattern's: it matches at any depth (`vars/x.yml` also drops `sub/vars/x.yml`) unless it starts
  with `/`, which anchors it to the root; `*` stays within one path segment, `**` crosses segments,
  `**/` may match no directory at all.
- G13. Files semgrep names as skipped are accounted for, not hidden: a note
  `⚠ gh-scopes: N file(s) skipped by semgrep` and each path indented is printed to stdout.
- G14. Zero files in `paths.scanned` is exit 2 (`semgrep scanned no file`): a count of nothing proves
  nothing.

### Output

- G15. Findings: each distinct `path:line` of a result, sorted, one per line on stdout as
  `<path>:<line>  gh-scope-outside-ssot`; then
  `gh-scope-outside-ssot: <L> line(s) in <F> file(s) name a GitHub scope outside vars/github-required-scopes.yml. See CLAUDE/QA.md "gh-scope-outside-ssot".`;
  then `passed: <scanned - F> failed: <F>`; exit 1.
- G16. Clean: `passed: <scanned> failed: 0` on stdout, exit 0. The qa-all `count` reader reads this line.

## 3. Departures from the bash

| Id  | The bash                                                                  | Now                                                         |
| --- | ------------------------------------------------------------------------- | ----------------------------------------------------------- |
| B1  | tracked files only (`git ls-files`)                                       | what git would publish: an unstaged new file too (G7)       |
| B2  | ignored the report's `errors`                                             | exit 2 (G11)                                                |
| B3  | semgrep's default 1 MB size limit skipped big files, accounted as skipped | no size limit (G9); anything still skipped is printed (G13) |
| B4  | `passed: 0` on a run that scanned nothing                                 | exit 2 (G14)                                                |
| B5  | excludes matched with `fnmatch`, where `*` crosses `/`                    | semgrep's glob meaning (G12)                                |
| B6  | self-test sent metrics; no timeout on any process                         | `--metrics=off`; every process is time-bounded (G6, G10)    |
| B7  | arguments ignored                                                         | unknown argument is exit 2 (G1)                             |
| B8  | a missing semgrep and a missing rule file were different failures         | both exit 2 (G4, G5)                                        |
| B9 | `--root` or `--scripts` given an empty value, or a value that begins with `-`, was taken as the value, and an empty root was read as "not given", so the gate judged the working directory | exit 2: `<flag> needs a directory`, with the usage (G1) |

## 4. Stated limits and decisions

- Scope is this tool's own `git ls-files` walk, not a `qa/discovery` list. Discovery judges a
  symlink by its target and fails on a nested repository; this gate must leave symlinks out
  (semgrep refuses them) and has no trees table, so reusing the list would need a second filter
  and a changed contract for every other caller. Both walks ask git for the same set.
- `qa-changed` runs this gate (check `gh-scopes`, whole) only when the rule, its fixture, the
  wrapper or the tool change or are deleted. Every published file is an input to the gate, but a
  per-commit semgrep over the whole tree is too slow to want; `qa-all` runs it for every push.
