# qa-speech-to-text-rules: specification of the Go port

Same method as [SPEC-qa-gh-scopes.md](SPEC-qa-gh-scopes.md): **the legacy bash is not an oracle.**
This states the *intended* behaviour as numbered, testable expectations;
`scripts/qa-speech-to-text-rules.bash` (105 code lines, a `python3 -c` YAML split and three `jq` calls)
was read only to find the requirements. Where it was wrong the expectation says what is right and
section 3 lists the departure. The proof is the Go tests in `src-go/qa/sttrules` (cited by number),
real-semgrep tests in `src-go/qa/cmd/qa-speech-to-text-rules`, and a run of both tools over the live
tree at cut-over.

## 1. Role

The gate for the rules in `.semgrep/speech-to-text.yml` (Plan 00156, CLAUDE/QA.md):
`model-present-without-weights` (a model judged present from its cache snapshot) and
`dropdown-label-carries-explanation` (a dropdown option that explains itself). The rules are
semgrep data in two languages, proven against `.semgrep/speech-to-text.js` and
`.semgrep/speech-to-text.py`. semgrep stays the engine; the Go tool owns what is scanned, the proof
that every rule still matches, and the refusal to pass over a file the analyser did not read.
Callers: `qa-all` (`scripts/qa-gates.tsv`, gate `speech-to-text-rules`, kind `count`, read from
`passed: N failed: M`), `qa-changed` and people. Processes started: `git` (through `qa/discovery`)
and `semgrep`, both through `qa/internal/proc`.

## 2. Expectations

### Command line, streams, exit codes

- S1. `qa-speech-to-text-rules [--root DIR] [--scripts DIR] [PATH...]`. `--root` names the
  repository root; `--scripts` names the scripts directory and the root is its parent (the wrapper
  passes it); neither: the working directory. `-h`/`--help` prints the usage to stdout, exit 0. An
  unknown option, or an option missing its value, is exit 2 with `ERROR:` and the usage on stderr.
  A bare word is a PATH.
- S2. Exit codes: 0 clean, 1 a finding or a rule failing its fixture, 2 the run could not complete.
  Every exit-2 message is on stderr and starts `ERROR:`. A failed write to stdout is exit 2,
  `ERROR: cannot write to stdout: <why>`. stdout holds only the payload (findings, the summary, the
  `passed:` line); nothing else is written there.
- S3. The root is resolved by `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable is
  exit 2). No checkout path reaches stdout or stderr when the root is a symlink, and semgrep is run
  with the resolved root as its working directory. semgrep's own words (a failed self-test, its
  stderr) name files by absolute path, so the checkout path is taken out of them before they are
  shown.

### Preconditions

- S4. semgrep not runnable (`semgrep --version` fails to start, times out, leaves a process behind
  or exits non-zero) is exit 2, `ERROR: semgrep not found. Install with: pipx install semgrep (<cause>)`, before anything else runs; the cause is the start error or the exit code and stderr.
- S5. The rule file `.semgrep/speech-to-text.yml` and the two fixtures `.semgrep/speech-to-text.js`
  and `.semgrep/speech-to-text.py` must be readable regular files; any other is exit 2 naming it.
- S6. The rule file is decoded as YAML. It must hold a `rules` list; every rule must carry a string
  `id` and a `languages` list of exactly one language, `javascript` or `python`. Anything else (does
  not decode, no rules, a rule with two languages or another language or no id) is exit 2 naming the
  rule. Each of the two languages must have at least one rule, so a rule file edited to hold only
  one language's rules cannot silently stop judging the other.

### The rules are proven on every run

- S7. Each language's fixture must mark, with a `ruleid: <id>` annotation on some line, **every rule
  of that language**. A fixture with no annotation, or one missing a rule of its language, is exit 2,
  `ERROR: <fixture> marks no ruleid line for <id>; the self-test proves nothing for it`, because
  `semgrep --test` exits 0 on a fixture with no annotations. The check runs before the self-test.
  Only `ruleid:` counts, at the start of a line or after a non-word character: `todoruleid:`
  (which semgrep does not fail when it does not match) certifies nothing. The annotation reader is
  `semgrepreport.Annotated`, shared with the gh-scopes and patterns gates.
- S8. Then `semgrep --test --metrics=off --disable-version-check --config <rules> <fixture>` runs
  for each of the two fixtures. A result of 1 (or a 2 that reports a "rule id mismatch": semgrep's
  words for a fixture annotating a rule the file does not hold) is exit 1, `ERROR: .semgrep/speech-to-text.yml failed its fixture <fixture>:` and semgrep's own output, on stderr,
  and no scan happens. Any other result of 2 or more is the rule file being unusable (a YAML or
  regex semgrep rejects): exit 2, `ERROR: semgrep could not run the self-test (exit N):` and its
  output. A self-test that cannot start or times out is exit 2.

### What is scanned

- S9. **Default scope.** The JavaScript files and the Python files of the lists `js` and `python` of
  `qa/discovery` (what git would publish, by name or shebang; the extensionless Python scripts
  under `files/` are found by shebang), as repository-relative paths, less any file under
  `.semgrep/` (the rules' own fixtures, the one exclusion this gate owns). The vendored trees
  (`.claude/hooks-daemon`, `.claude/skills`, `node_modules`) are the trees table's
  (`scripts/qa-trees.tsv`) and are already gone from both lists, so `qa-changed`, which reads the
  same table, never hands this gate a file the push gate leaves out. A discovery failure is exit 2 with its message. After the
  exclusions each language must keep at least one file, else exit 2, `ERROR: no <JavaScript|Python> files were found; nothing was checked`.
- S10. **Named scope.** With PATH arguments only those files are judged, whatever tree they are in
  (a named file wins over the exclusions of S9). Each is made relative to the root: a relative path
  is taken against the root's frame, an absolute one must lie inside the root once the symlinks of
  its directory are resolved (so an absolute path through a symlinked checkout is accepted), and one
  outside the root or climbing out of it is exit 2. A file that cannot be read, or is not a regular
  file, is exit 2 naming it. Duplicates are judged once. The language is by name: `.js`/`.mjs` is
  JavaScript, `.py` Python; with any other name (or none) a first line whose interpreter is
  Python (`discovery.PythonShebang`: `python`, `python3.12`, through `env -S`; not
  `sh -c 'exec python'`, not a mention in a comment) is Python. Any other file is exit 2, `ERROR: <path> is neither JavaScript nor Python`.
- S11. **Rules by language.** semgrep applies a rule to an extensionless file only under
  `--scan-unknown-extensions`, and then applies every rule whatever its language. So each language
  is its own scan with a rule file holding only that language's rules, written (mode 0600, with the
  rules re-encoded from the decoded YAML, nothing hand-assembled) in a private temporary directory
  that is removed when the run ends, whatever its outcome. A language with no file to judge is not
  scanned.
- S12. A scan hands the files to semgrep **by name**, never as a directory (a directory scan applies
  semgrep's default ignore list, which skips `tests/`), from the root:
  `semgrep --metrics=off --disable-version-check --config <rules> --no-rewrite-rule-ids --scan-unknown-extensions --json --quiet --max-target-bytes=0 <files...>`.

### Judging semgrep's answer (nothing unread is a pass)

- S13. semgrep exiting 2 or more, a timeout, a stdout that is not a JSON report, or a report with no
  `paths.scanned` is exit 2 with semgrep's stderr. Exit 0 and 1 are both a report.
- S14. Any entry in the report's `errors` is exit 2 listing path and message: a file semgrep could
  not parse or read fully is not a clean file.
- S15. **Coverage.** Every file handed over must be in `paths.scanned`; a file semgrep skipped, or
  never named, is exit 2 listing them, `A pass reported over files the analyser never opened is not a pass`. Zero scanned files in total is exit 2.

### Output

- S16. Findings: each distinct `path:line: <rule id>` of every result, sorted by path then numeric
  line, one per line on stdout; then `speech-to-text rules: <L> finding(s) in <F> file(s). See CLAUDE/QA.md under each identifier.`; then `passed: <scanned - F> failed: <F>`; exit 1.
- S17. Clean: `passed: <scanned> failed: 0` on stdout, exit 0. The qa-all `count` reader reads this
  line. `<scanned>` is the number of distinct files semgrep reports it read (`paths.scanned`) across both
  languages; S15 has already shown every handed file is among them.

## 3. Departures from the bash

| Id  | The bash                                                                                                             | Now                                                                                                                                              |
| --- | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| B1  | tracked files only (`git ls-files`)                                                                                  | what git would publish: a new unstaged file too (S9)                                                                                             |
| B2  | a fixture with no annotation passed its self-test                                                                    | exit 2 (S7); a rule with no `ruleid` in its language's fixture too                                                                               |
| B3  | semgrep's default 1 MB size limit skipped a big file, which was then reported lost                                   | no size limit (S12); anything still unread is exit 2 (S15)                                                                                       |
| B4  | a named file that was not `.js`/`.mjs` was scanned as Python, whatever it was                                        | exit 2 unless it is JavaScript or Python (S10)                                                                                                   |
| B5  | the rule split ran `python3 -c` with PyYAML                                                                          | decoded and re-encoded in Go (S6, S11); a rule file of one language fails                                                                        |
| B6  | findings sorted as text (`sort -u`), so line 10 came before line 9                                                   | sorted by path then numeric line (S16)                                                                                                           |
| B7  | arguments beginning with `-` were taken as paths                                                                     | an unknown option is exit 2 (S1)                                                                                                                 |
| B8  | no time limit on any process                                                                                         | every process is time-bounded (S4, S8, S13)                                                                                                      |
| B9  | the tool read the checkout path from `BASH_SOURCE` and printed it in errors                                          | root resolved once; no checkout path in output (S3)                                                                                              |
| B10 | `.semgrep/*`, `.claude/skills/*`, hooks-daemon, `*/node_modules/*` dropped only for the default scope, by shell glob | the vendored trees come from the trees table alone (`python .claude/skills` added), the gate owns only `.semgrep/` (S9); a named file wins (S10) |

## 4. Stated limits and decisions

- Scope reuses `qa/discovery` so the trees table and its refusal of an empty list apply; the
  exclusions the bash had on top (the fixtures, vendored trees) are applied here, after discovery.
- semgrep is not guaranteed to report a parse error for a broken file (a truncated Python `def` was
  scanned and reported clean by the engine), so S14 refuses what semgrep reports and no more.
- A file named by hand in an excluded tree (a fixture, say) is judged because it was named; the
  default scope would leave it out. Filtering in named mode was rejected: a run whose every file
  is excluded would print a vacuous `passed: 0` or abort a commit check. `qa-changed` does not
  depend on it: its routing skips an excluded tree before the gate is handed a file, and the
  trees table now excludes `.claude/skills` for Python as it did for shell and JavaScript.
- `qa-changed` runs this gate over exactly the changed JavaScript and Python files by name (check
  `stt-rules-files`), and whole when its rules, fixtures, script, Go package, command or the shared
  discovery change (check `stt-rules`, `whole`, so a deletion owes it too).
