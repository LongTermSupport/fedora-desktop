# qa-python: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md), after [SPEC-qa-patterns.md](SPEC-qa-patterns.md).
**The legacy bash is not an oracle.** This states the *intended* behaviour as numbered, testable
expectations; `scripts/qa-python.bash` (155 code lines, 270 with comments, an associative array
and seven `jq` calls, an inline `python -c`) was read only to find the requirements. Where it was
wrong, the expectation says what is right and section 3 lists the departure. Proof is the Go tests
in `src-go/qa/python` (cited by number) and a run of the Go
tool and the bash over the live tree.

## 1. Role

The Python gate: every Python file git would publish must compile and pass ruff, and every Python
template must compile once its expressions are substituted. ruff stays the engine (the ruleset is
data, in `ruff.toml`; the version is pinned by `.qa-versions`); `python3` stays the compiler. The
Go tool owns everything around them: what is checked, the proof that ruff read what it was given,
and the judgement. Callers: `qa-all` (`scripts/qa-gates.tsv`, stage `python`: run with
`QA_JSON_OUT`, reads `.summary.{total,passed,failed}` and `.failures`; exit 2 aborts the run) and
people. `qa-changed` runs ruff per file itself, and runs this gate whole when the tool or its
wrapper change (section 4).

## 2. Expectations

### Command line and environment

- Y1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. The root is made absolute and cleaned once, so `--root .`, a relative `--scripts` and a
  trailing slash all behave as the absolute path does. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument, or a flag
  missing its value, is exit 2 with `ERROR:` and the usage on stderr. *(Bash ignored arguments.)*
  The root is resolved by the one shared helper `qa/internal/rootpath` (absolute, clean, symlink-free; a root that cannot be resolved is exit 2), and a symlinked-root test pins that no checkout path reaches stdout or the report.
- Y2. `QA_JSON_OUT` is the report path; unset or empty is `/tmp/qa-python-results.json`. A report
  that cannot be written: `ERROR: cannot write the report <path>: <why>`, exit 2.
- Y3. Exit codes: 0 no finding, 1 at least one, 2 the run could not complete. Every exit-2
  message is on stderr and starts `ERROR:`; no report is written on exit 2. A failed write to
  stdout or stderr is exit 2, and so is a closed stdout (`>&-`): the write fails with EBADF, it
  must not be lost silently.

### The tools and the pin

- Y4. ruff must run (`ruff --version` exits 0 and prints `ruff <version>`), else
  `ERROR: ruff not installed` with the play that installs it (`./playbooks/imports/play-python.yml`,
  and that Fedora's dnf package is the wrong one), exit 2. `python3` must run
  (`python3 --version` exits 0), else `ERROR: python3 not installed`, exit 2.
  **Stated limit:** only ruff is pinned. The compile verdict is the host's `python3`, so syntax newer
  than that interpreter (`type X = int` is a SyntaxError before 3.12) fails on an older host and
  passes on a newer one. `.qa-versions` holds tool pins (`RUFF=`), not interpreter minimums, and
  has no natural place for one; the interpreter is whatever the OS provides.
- Y5. `.qa-versions` under the root must exist and hold exactly one `RUFF=<version>` line with a
  non-empty value (surrounding whitespace ignored), else exit 2 naming the file: with no pin the
  gate cannot say which gate it is.
- Y6. A ruff whose version differs from the pin **fails the gate, it does not abort the run**: ruff
  is not asked to lint (its findings would come from a gate this repository has not agreed to), a
  report is written with `status: "fail"`, `summary {total:0, passed:0, failed:1}`, `results: []`,
  `ruff_diagnostics: []` and exactly one failure `{file: ".qa-versions", type: "python", status: "fail", error: "ruff version mismatch: expected <pin>, found <actual> — ruff was not run, so no python file was linted this run"}`, stdout is `✗ python: ruff version mismatch: expected <pin> (.qa-versions), found <actual> → <report> (no files linted)`, stderr says how to match the pin or
  bump it deliberately, exit 1. (A report with no document, or an exit 2, would shift the
  positional merge in qa-all and take every later gate offline.)

### Which files: the shared discovery

- Y7. Which files are checked is defined once, by [SPEC-qa-discovery.md](SPEC-qa-discovery.md),
  and this gate asks the `qa/discovery` package for its `python` and `python-templates` lists
  (D2-D13): what git would publish (never a gitignored file), outside the `python` rows of
  `scripts/qa-trees.tsv`, a file being Python by its `.py` name or a Python shebang whatever its
  mode, a symlink judged by its target. This gate adds nothing to that definition and does not
  re-derive it. *(Bash used `find`, so ignored files, build output and other checkouts' files were
  checked, and its coverage guard compared that with git.)*
- Y8-Y10. A failure of the discovery (a bad trees table, git failing, an unreadable or
  unfollowable candidate, no Python file at all) is exit 2 with its message after
  `ERROR: discovery of <list> failed:`. The tests here cover the consequences for this gate
  (nothing is checked, no report is written); the definitions are tested in `qa/discovery`.

### Templates

- Y11. A Python template is a `.j2` the discovery lists as `python-templates` (D8); the list may
  be empty.
- Y12. A template containing a Jinja statement (`{%`) is exit 2 naming it: its rendered Python
  cannot be reconstructed by substitution, and "this gate cannot check it" must not become "this
  gate passes it". Nothing else is checked first, so one such file stops the run. The check is a
  substring test, so a Python string holding `{%` in a template is exit 2 as well (fail-safe).
- Y13. Any other template has every `{{ … }}` (shortest match, across lines) replaced with `None`
  and the result is compiled as Python. A failure is the template's own: `None` is an expression
  wherever an expression may appear. A Jinja comment (`{# … #}`) is not substituted and fails the
  compile: a false failure, never a false pass (fail-safe, stated limit).

### Compiling, and the proof ruff read everything

- Y14. One `python3 -I` process compiles every file (bytes in, so an encoding declaration is
  honoured) without writing bytecode into the tree, and answers for each file given. A failure is
  `✗ python: <path>: <message>` on stdout (`<path> (rendered)` for a template), the message being
  Python's own text with the checkout's path never in it. `python3` failing, or not answering for a
  file it was given, or answering in anything but the expected form, is exit 2.
- Y15. ruff must have read every Python file: `ruff check --config <root>/ruff.toml --show-files --no-cache <files>` is run
  with the same arguments as the lint and each file must be listed, else `ERROR: ruff did not select <N> of the Python file(s) it was given:` and the list, exit 2. (ruff drops a file its
  own exclude rules name, and a gate that vouches for a file ruff skipped is lying.) Templates are
  compiled, never linted, and are not given to ruff.

### Ruff

- Y22. Every ruff call (lint and `--show-files`) is given `--config <root>/ruff.toml`, so ruff's
  hierarchical discovery is off: a `pyproject.toml [tool.ruff]`, `ruff.toml` or `.ruff.toml`
  nested under the root cannot replace the repository's ruleset for its subtree. qa-changed's
  per-file ruff is given the same flag and `--no-cache`.
- Y23. The lint is given `--ignore-noqa`: `# noqa` and `# ruff: noqa` cannot hide a finding
  (CLAUDE/QA.md bans suppression directives; the gate enforces it).

- Y16. `ruff check --config <root>/ruff.toml --no-cache --ignore-noqa --output-format json <absolute paths>` runs from the root. `--no-cache`
  keeps the check read-only (no `.ruff_cache` written). Status 0 or 1 is data; 2 or more, or no
  status, or a timeout, is `ERROR: ruff invocation failed (status N):` with its stderr, exit 2. Its
  stdout must be a JSON array, else exit 2; an empty stdout is not an array.
- Y17. Each diagnostic keeps every field ruff gave it, with `filename` made relative to the root,
  and the whole list is the report's `ruff_diagnostics`. A diagnostic on a file that was not
  given to ruff is exit 2 (ruff and the gate disagree on what was checked).

### Output and report

- Y18. A file is *failed* when it has a compile error or at least one ruff diagnostic. Its
  `error` is the compile message, then its diagnostics as `<code>:<line> <message>`, joined by `; `
  (a diagnostic without a code prints `syntax-error`; the pinned ruff gives syntax errors the code
  `invalid-syntax`, so this fallback is reached only by an older ruff).
- Y19. Order is files sorted by path, then templates sorted by path, in `results` and in stdout.
  The coverage and failure lines are, in order: each compile failure line (Y14); when ruff found
  anything, `✗ python: ruff: <N> issues in <F> file(s) (see <report> .ruff_diagnostics)`.
- Y20. No failure: `✓ python: <N> files OK; <T> rendered template(s) compile (syntax only, not linted)`, exit 0. The template count is stated always, including 0: a template discovery that
  silently finds nothing is the hole this line exists to make visible. Otherwise
  `✗ python: failed → <report>`, exit 1.
- Y21. The report is `{type:"python", status:"pass"|"fail", summary:{total, passed, failed}, results:[{file, type:"python"|"python-template", status, error?}], failures:[the failed results], ruff_diagnostics:[...]}`, two-space JSON; `total` is the number of results (files and templates),
  `failed` the failed ones. It is written on exit 0 and 1, never on exit 2.

## 3. Where the Go departs from the bash

| #   | bash behaviour                                                               | Go                                                                    | expectation |
| --- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------- | ----------- |
| B1  | arguments ignored                                                            | usage / exit 2                                                        | Y1          |
| B2  | scope by `find`: ignored files and build output checked                      | what git would publish                                                | Y7          |
| B3  | coverage guard compared the `find` result with the tracked list              | git is the scope; the independent proof is that ruff listed each file | Y7, Y15     |
| B4  | no `python3` made every file a "syntax error" with a shell message           | exit 2 `python3 not installed`                                        | Y4          |
| B5  | `py_compile` once per file, writing `__pycache__` into the tree              | one `python3 -I` process, nothing written                             | Y14         |
| B6  | a `.qa-versions` with no `RUFF=` line gave an empty pin and a "mismatch"     | exit 2 naming the file                                                | Y5          |
| B7  | a ruff that skipped a file by its own exclude rules was unnoticed            | exit 2 listing the files                                              | Y15         |
| B8  | ruff run with its cache, writing `.ruff_cache`                               | `--no-cache`                                                          | Y16         |
| B9  | ruff output not JSON: a `jq` error under `set -e`                            | exit 2 with a message                                                 | Y16         |
| B10 | ruff findings failed the stage with `failures: []`                           | the files with findings are failures with their codes                 | Y18, Y21    |
| B11 | `ruff_diagnostics` carried the checkout's absolute paths                     | paths relative to the root                                            | Y17         |
| B12 | unwritable report: shell error, exit 1                                       | message, exit 2                                                       | Y2          |
| B13 | failed stdout write unchecked                                                | exit 2                                                                | Y3          |
| B14 | a dangling `*.py` symlink invisible to `find -type f`                        | exit 2 naming it                                                      | Y9          |
| B15 | the error text of a failed compile carried a temporary file's path           | the template's own path                                               | Y14         |
| B17 | a nested ruff config could swap the ruleset; `# noqa` hid findings          | `--config` to the root file, `--ignore-noqa`                          | Y22, Y23    |
| B16 | exclusion list read by `lib/qa-trees.bash`; an empty list checked everything | no `python` row is exit 2                                             | Y8          |
| B18 | `--root` or `--scripts` given an empty value, or a value that begins with `-`, was taken as the value, and an empty root was read as "not given", so the gate judged the working directory | exit 2: `<flag> needs a directory`, with the usage | Y1 |

## 4. Go design and cut-over

`src-go/qa/python` (`run.go` command line and flow, `discover.go` the call to the shared discovery, `compile.go`
the compile step and templates, `ruff.go` the version, the coverage proof and the lint,
`report.go`) and a thin `src-go/qa/cmd/qa-python`. Discovery is the `qa/discovery` package
(SPEC-qa-discovery.md), called in-process with the injected runner. Every process (git, ruff, python3) goes through the injected `proc.Runner`, so the tests
substitute a fake that records argv and answers as the tool would; the real ruff and python3 are
exercised by `cmd/qa-python`'s tests over a real work tree and by the live-tree run in the journal.
`scripts/qa-python.bash` becomes a build-and-run wrapper over `lib/go-tool.bash`; its allowlist row
is removed. Routing: the gate row (`stage python`) and the `perfile-gate` list keep naming the
script; qa-changed gains a `python` check, run whole when the tool, its wrapper, `ruff.toml`,
`.qa-versions` or `scripts/qa-trees.tsv` change, or when a Python template (the `@pytemplate`
predicate: a `.j2` with a Python shebang) changes, ahead of the generic `*.j2` row. CI runs `qa-all`, which runs the wrapper, so it
needs `go` and `python3` and `ruff` (all already installed there).
