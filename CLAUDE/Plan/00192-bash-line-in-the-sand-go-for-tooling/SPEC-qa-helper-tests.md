# qa-helper-tests: specification of the Go port

Same method as [SPEC-qa-toolchain.md](SPEC-qa-toolchain.md): **the legacy bash is not an
oracle.** This states the *intended* behaviour as testable expectations;
`scripts/qa-helper-tests.bash` (90 statements, an associative array) was read only to discover the
requirements. Where it was wrong the expectation says the right behaviour and a departure (B) says
what the bash did. The proof is the Go tests in `src-go/qa/helpertests` and
`src-go/qa/cmd/qa-helper-tests` (one or more per expectation, cited by number) and a run over the
live tree at cut-over.

## 1. Role

The helper packages under `helpers/` are namespace packages with no `__init__.py`, so
`python3 -m unittest discover` collects nothing and says `Ran 0 tests`. This gate enumerates
`tests/helpers/**/test_*.py`, turns each path into a dotted module name and hands the whole list to
`helpers.qa_environment.unittest_counts` (stdlib `unittest`; no pytest, no virtual environment:
helper tests are stdlib-only by rule). That Python runner stays Python (Ansible-adjacent QA
helper, `helpers/CLAUDE.md`); what moves to Go is everything around it: discovery, the coverage
cross-check, the command line, the environment, and judging that the run really happened.

Callers: `qa-all` (`scripts/qa-gates.tsv`, kind `helpers`, gate `helper-tests`) passes
`--counts-file` and `--counts-token` and reads the file with `gates.HelperCounts`; CI and humans run
the wrapper bare. `qa-changed` does not run this gate (it runs the `unit-tests` check over the
mirrored module of each changed helper); that stays as it is (B9).

The counts file format and the runner are unchanged: five keys, `token`, `tests`, `skipped`,
`modules`, `tracked`, written after the suite by the runner. The reader in `qa/gates` gains an
integer-valued sibling (G1) and the old `HelperCounts` keeps its output byte for byte.

## 2. Expectations

Numbers are cited by the tests.

### Command line, streams, exit codes

- H1. `--root DIR` names the repository root; `--scripts DIR` names the scripts directory and the
  root is its parent (the wrapper passes this); neither: the working directory. `--counts-file PATH`
  and `--counts-token VALUE` are described in H9. `-h`/`--help` prints the usage to stdout, exit 0.
  Any other argument beginning `-`, and any positional argument, is exit 2 with
  `ERROR: unknown argument '<arg>'` and the usage on stderr; an option missing its value (or whose
  value begins `-` or is empty) is exit 2, and so is an option given twice. The root is resolved by `qa/internal/rootpath`.
- H2. Exit codes: 0 every collected test passed; 1 the suite ran and at least one test failed or
  errored (the runner's own verdict); 2 the run could not complete (H1, H4-H8, H10-H13). Every exit-2
  message is on stderr and starts `ERROR:`.
- H3. Stdout stays empty on a run, whatever the outcome: `qa-all` parses its stage lines from the
  stdout of a sibling and refuses a helper-suite run that wrote to it. Everything a human reads
  (coverage, the runner's tracebacks and summary, the final line) is stderr. A failed write to
  stdout or stderr is exit 2 (`ERROR: cannot write to stdout: <why>` when stderr still works).

### Scope

- H4. The tests are found by walking `<root>/tests/helpers`: every entry whose name begins `test_`
  and ends `.py` that is a regular file, or a symlink to one. A directory is entered, a symlinked
  directory is not. A missing `tests/helpers`, an unreadable directory, a symlink named like a test
  that does not resolve, and **no test found at all** are exit 2. An empty scope never passes.
- H5. The tracked tests come from the shared `qa/discovery` list `tracked-helper-tests` (git's index,
  less deleted files). Its failure is exit 2 carrying discovery's message. Every tracked test the walk
  did not find is a lost test: exit 2 naming each, before anything runs. The yardstick comes from git,
  not from the walk being checked.
- H6. The other direction: a found test that git does not track still runs, and is named on stderr
  after a `COVERAGE: <found minus untracked> of <tracked> tracked helper test modules` line, with the
  reason it matters (the counts of this machine are no longer comparable to another's). It is not a
  failure.
- H7. A test path becomes a module name by dropping `.py` and turning `/` into `.`. Every segment of
  the result must be an ASCII Python identifier (`[A-Za-z_][A-Za-z0-9_]*`); a path with another
  character in any segment (a dash, a dot, a space) cannot be imported by name and is exit 2 naming
  the path, never a silent skip and never an argument for the runner to misread.

### The run

- H8. The runner is started as
  `env -u GIT_CONFIG_COUNT -u GIT_CONFIG_PARAMETERS GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 python3 -m helpers.qa_environment.unittest_counts --counts-file <abs path> --tracked-modules <N> [--counts-token <value>] <module>...` in the repository root: the machine's git configuration (a
  signing key) stays out of every fixture commit. `<N>` is the number of tracked tests (H5), the
  modules are in path order.
- H9. The counts file is `--counts-file`'s path made absolute against the caller's directory; the
  file is emptied before the run, so a runner that dies without writing leaves nothing a stale run
  could have left behind. With no `--counts-file` the gate uses a private temporary file and removes
  it afterwards. `--counts-token` is forwarded only when given and is required to come back
  (G1). A file that cannot be emptied or created is exit 2.
- H10. The runner's stdout and stderr are passed through as they are written: its stdout to this
  command's stdout, its stderr to stderr. (The runner writes everything to stderr; if a test makes
  the runner write to stdout, `qa-all` is where that is refused.)
- H11. The runner's status decides the verdict: 0 passed, 1 failed. Any other status (killed by a
  signal, `python3` or `env` missing so the shell-style 127, an argument error 2, an unhandled
  exception) is exit 2 saying the status; a runner that cannot be started at all is exit 2 saying
  `python3` is installed by `playbooks/imports/play-python.yml` and not to install it by hand. A
  runner that ran longer than 30 minutes is killed with its process group and is exit 2 (`timed out`). A process a test left holding the output open after the runner ended is exit 2.
- H12. After the runner ends with 0 or 1, the counts file must be read (G1) and must agree with what
  was asked: it carries the given token, `tests` is at least 1, `modules` equals the number of
  modules passed, `tracked` equals the tracked count. A missing, empty, repeated, non-numeric,
  wrong-token or disagreeing file is exit 2 saying which: the numbers `qa-all` compares two machines
  on must come from this run or the run is not trusted. The verdict of a red suite is not softened:
  this applies to 1 as well, because a runner that reports failure and no counts is a runner that
  did not finish.
- H13. On exit 0, one stderr line `HELPER-TESTS-OK: Ran <tests> tests in <modules> modules (<tracked> tracked), <skipped> skipped`. On exit 1, `ERROR: helper tests failed (<tests> run, <skipped> skipped)`.

### The reader (qa/gates)

- G1. `gates.HelperRun(path, token)` returns the four numbers as integers under the same refusals as
  `HelperCounts` (missing file, empty file, unknown key, repeated key, token other than asked,
  missing or non-numeric count; numbers too large for an `int` are refused). `HelperCounts` formats
  the same values and its output and error texts are unchanged.

## 3. Departures from the bash (B)

- B1. No test found: the bash printed `ERROR: no helper tests found` and exited 1; exit 2 here (H4),
  the code `CLAUDE/QA.md` reserves for "coverage cannot be verified".
- B2. The bash found tests with `find -type f`, which skips a symlink, then reported a tracked
  symlinked test as "missed". The walk follows a symlink to a regular file (H4), as discovery does,
  so the two lists agree.
- B3. The bash exited with whatever the runner exited with. Only 0 and 1 are verdicts here (H11).
- B4. The bash never looked at the counts file; `qa-all` was the only reader. A standalone run could
  pass over a runner that wrote nothing, or a suite that collected zero tests. H12 checks.
- B5. A test path that is not a dotted identifier was passed to the runner as a module name and
  surfaced as an import error inside the suite (or, with a leading dash, as an argument); H7 names it.
- B6. The bash left the counts file as the caller gave it; H9 empties it first.
- B7. The bash had no time limit (the caller's was the only one); H11 bounds the run.
- B8. The bash let a repeated option win silently (the last); a repeated option is exit 2 here
  (H1).
- B9. `qa-changed` still runs no helper-test gate: no check, no golden scenario changed (so no
  SPEC-qa-changed B-row). A change under `src-go/` routes to the `go` check, which runs these
  tests; a change to the wrapper is a shell file and routes to the shell checks. Tests pin both
  (`classify_test.go`, `realtree_test.go`).
- B10. The bash cross-checked tracked-versus-found with an associative array; that is the one
  reason it was over the bash limits. The check is a slice difference in Go.

## 4. Stated limits

- The tests' own behaviour is the runner's: a test that overwrites the counts file after the
  runner's write (an `atexit` handler) is not detected here; the token and write ordering are the
  defence, as `unittest_counts.py` documents.
- `tests/` outside `tests/helpers/` is not run here (some of it needs numpy); that is deliberate and
  unchanged.
