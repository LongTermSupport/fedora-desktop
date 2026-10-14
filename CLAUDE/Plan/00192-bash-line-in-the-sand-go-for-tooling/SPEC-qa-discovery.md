# qa-discovery: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md), after [SPEC-qa-patterns.md](SPEC-qa-patterns.md).
**The legacy bash is not an oracle.** This states the *intended* behaviour as numbered, testable
expectations; `scripts/qa-discovery.bash` (158 code lines, 14 functions) was read only to find the
requirements. Where it was wrong, the expectation says what is right and section 3 lists the
departure. Proof is the Go tests in `src-go/qa/discovery` (cited by number) and a run of the Go
tool over the live tree, compared with what the bash listed.

## 1. Role

The one definition of "which files does a QA gate read": a repo-owned shell script, a repo-owned
Python file, a Python Jinja template, and the three yardsticks a gate measures its own coverage
against (every tracked shell script, Python file, playbook and helper test module). It was a
sourced bash library so that `qa-bash`, `qa-python` and the others could not disagree about what a
shell script is (Plan 00076, Plan 00081). It is now a Go command, `qa-discovery`, that prints one
list; the bash library is a thin wrapper that calls it, so the callers keep one mechanism.
Callers: `qa-bash`, `qa-python`, `qa-ansible-syntax`, `qa-helper-tests`, `qa-ready-wait-rules`
(lists), and `qa-js`, `qa-ansible-syntax` (the exclusion test, which stays in the wrapper over the
same data file).

## 2. Expectations

### Command line and output

- D1. `qa-discovery [--root DIR] [--scripts DIR] LIST`. `LIST` is exactly one of `shell`, `python`,
  `python-templates`, `tracked-shell`, `tracked-python`, `tracked-playbooks`,
  `tracked-helper-tests`. `--root` names the repository root; `--scripts` the scripts directory
  (the root is its parent); neither: the working directory. `-h`/`--help` prints the usage to
  stdout, exit 0. A missing or extra `LIST`, an unknown list, an unknown flag or a flag without its
  value is exit 2 with `ERROR:` and the usage on stderr.
  The root is resolved by the one shared helper `qa/internal/rootpath` (absolute, clean, symlink-free; a root that cannot be resolved is exit 2), so the absolute paths printed are under the real root, which a gate strips from what an engine prints.
- D2. stdout is the payload and nothing else: the paths, each ended by NUL, sorted, no duplicates.
  `shell`, `python` and `python-templates` print absolute paths (the root as given, then `/`, then
  the repository-relative path); the four `tracked-` lists print repository-relative paths. A list
  with no members prints nothing. Every diagnostic is on stderr, starting `ERROR:`.
- D3. Exit 0 with the list, exit 2 when the list could not be made. Nothing is printed on exit 2.
  A failed write to stdout is exit 2.

### Scope: what git would publish

- D4. `shell`, `python` and `python-templates` read the files in
  `git ls-files -z --cached --others --exclude-standard` from the root: tracked, or untracked and
  not ignored. A gitignored file is never read, counted or listed. The `tracked-` lists read
  `git ls-files -z` (tracked only). Both leave out files `git ls-files --deleted` names (listed
  by git, gone from the tree). `git` failing, or the root not a work tree, is
  `ERROR: cannot list the files git would publish under <root>: <why>`.
- D5. The excluded trees are the rows of `scripts/qa-trees.tsv` (`<list><TAB><dir>`, list `shell`
  or `python`; `#` and blank lines skipped). A path is excluded from a list when `/dir/` occurs as
  whole segments anywhere in `/<path>/`, tested on the repository-relative path (never on the
  absolute path, whose ancestors are not ours). `shell`, `tracked-shell` use the `shell` rows;
  `python`, `python-templates`, `tracked-python` the `python` rows; `tracked-playbooks` and
  `tracked-helper-tests` exclude nothing. An unreadable file, a malformed row, an unknown list
  name, or no row for `shell` or none for `python` is exit 2: an empty exclusion list would gate
  trees that are not ours.

### Which files

- D6. A shell script is a file whose name ends `.sh` or `.bash`, or, with any other name, whose
  first line is a shebang whose interpreter is `bash` or `sh` (directly, or after `env` and its
  options and `NAME=value` words), without a `-c` argument, a combined flag group such as `-ec` or `-xc` counting as
  containing one (a `bash -c` shebang is an inline one-liner in another kind of file, the
  playbooks'). A `*.j2` file is never one. Mode bits play
  no part.
- D7. A Python file is a file whose name ends `.py`, or, with any other name that is not `*.j2`,
  whose first line is a shebang whose interpreter (after `env` and its options) has a name
  beginning `python`. Mode bits play no part.
- D8. `python-templates` lists the `*.j2` files in the published set, outside the Python
  exclusions, whose first line is a Python shebang (D7's test). It may be empty.
- D9. One predicate per language serves discovery and its yardstick, so the yardstick can never be
  a second opinion about what counts; they differ only in source (D4). The shell predicate is one
  function, exported by `qa/discovery` and used by `qa/patterns`; no other package keeps a copy.
- D10. A file whose first line has to be read and cannot be is exit 2 naming it
  (`ERROR: cannot read <path>`): an unreadable file is never quietly classified as not a script. An
  empty file, or one with no newline, has the first line it has.
- D11. A symlink is judged by its target and listed under its own path. A link whose target is not
  a regular file (a directory, or one that does not resolve) is not a script, except one named
  `*.sh`, `*.bash` or `*.py` that cannot be followed, which is exit 2 naming it. Any other entry
  that is not a regular file (a submodule, a directory) is not a script.
- D12. `tracked-playbooks` is the tracked `*.yml`/`*.yaml` files under `playbooks/`.
  `tracked-helper-tests` is the tracked files under `tests/helpers/` whose base name is
  `test_*.py`. Neither looks inside a file.
- D13. An empty `shell`, `python`, `tracked-shell`, `tracked-python`, `tracked-playbooks` or
  `tracked-helper-tests` list is exit 2 (`ERROR: ... found 0 ...: this repository always has some, so discovery is broken`): a pass over nothing would vouch for code nothing had read.
- D16. `js` lists the `.js` and `.mjs` files in the published set (D4), outside the `shell` rows of
  `scripts/qa-trees.tsv` (D5; that list has always meant shell, YAML and JavaScript discovery), by
  name only, absolute. A symlink is judged by its target (D11). An empty list is exit 2 (D13):
  `ERROR: found 0 JavaScript files under <root>: ...`. The `qa-js` gate asks for it (SPEC-qa-js.md J5).
- D15. `git ls-files --others` does not look inside an untracked nested repository: it prints the
  one entry, ending in `/`. Such an entry that is not in an excluded tree (D5) is exit 2 naming
  it (`ERROR: <path> is a nested repository git will not look into ...`): a file the gates
  cannot see must stop QA. It is either listed in `qa-trees.tsv` on purpose, or a mistake.

### The bash wrapper

- D14. `scripts/qa-discovery.bash` is sourced, as before. `qa_discover LIST ROOT` runs the tool
  (built by `lib/go-tool.bash`) and fills `QA_FILES` with the list; the tool failing exits the
  caller 2, so a gate cannot go on with a list it did not get. `qa_is_excluded REL` and
  `qa_py_is_excluded REL` keep their contract over `QA_EXCLUDE_DIRS` and `QA_PY_EXCLUDE_DIRS`,
  which `lib/qa-trees.bash` reads from the same table (a missing table, an unknown list, or no row for `shell` or none for `python` is exit 2, as in
  D5, so both readers refuse the same tables).
  Sourcing needs neither go nor git; only `qa_discover` does.

## 3. Where the Go departs from the bash

| #   | bash behaviour                                                                                          | Go                                               | expectation |
| --- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------ | ----------- |
| B1  | discovery by `find`: gitignored files, build output and other checkouts were listed                     | what git would publish                           | D4          |
| B2  | `#!/bin/sh -e` and `#!/usr/bin/env sh` missed; `#!/…/bashful` and a `bash` anywhere in the line matched | interpreter parsed                               | D6          |
| B3  | any `python` in the first line matched (`#!/bin/bash # python`)                                         | the interpreter must be python                   | D7          |
| B4  | symlinks: `find -type f` skipped them, the yardstick counted them                                       | judged by target, listed by own path, both sides | D11         |
| B5  | python templates read from tracked files only                                                           | the published set                                | D8          |
| B6  | order was `find`'s                                                                                      | sorted                                           | D2          |
| B7  | arguments ignored, no usage                                                                             | usage, exit 2 on misuse                          | D1          |
| B8  | a missing `python` row in the table scanned everything                                                  | exit 2                                           | D5          |
| B9  | only an empty shell/python list was checked, by each caller                                             | every list but templates, by the tool            | D13         |
| B10 | seven `qa_*` list functions and seven arrays                                                            | one `qa_discover LIST ROOT` and `QA_FILES`       | D14         |
| B11 | an excluded name in an ancestor of the root: handled for the list, not for `qa_is_excluded` callers     | the relative path only, everywhere               | D5          |

B10 changes the callers: `qa-bash`, `qa-python`, `qa-ansible-syntax`, `qa-helper-tests` and
`qa-ready-wait-rules` call `qa_discover` in place of the old functions.

## 4. Go design and cut-over

`src-go/qa/discovery` (`run.go` command line and lists, `list.go` the git sets and classification,
`trees.go`, `shebang.go`) and a thin `src-go/qa/cmd/qa-discovery`. Every process goes through
`proc.Runner`; tests substitute a fake git over a real temporary tree. The wrapper keeps the
exclusion test in bash because `qa-js` and `qa-ansible-syntax` ask it once per file, where a process
per file would cost minutes; both readers use `scripts/qa-trees.tsv` and a test asserts they agree.
Its allowlist row is removed. Routing: `scripts/qa-discovery.bash` stays in `perfile-gate`; a change
to the wrapper, `lib/qa-trees.bash` or the Go package runs the Go gate, whose tests run the wrapper
over a real work tree.
