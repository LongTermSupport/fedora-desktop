# qa-changed.bash — report

## Built

- `scripts/qa-changed.bash`: diff-scoped QA. Changed files are `git diff --name-only HEAD`
  (or `--base <ref>`) plus untracked-not-ignored, deleted dropped. `--plan` prints the chosen
  checks without running; `--files F...` names files explicitly. Exit 0 pass or nothing
  changed, 1 a check failed, 2 missing tool.
- `scripts/test-qa-changed.bash`: 31 cases through `--plan` (real repo paths for the mapping,
  a scratch repository via `QA_CHANGED_ROOT` for the git collection). Wired into
  `qa-all.bash` as `qa-changed-mapping`; header stage-line counts bumped 36/26 to 37/27.
- Docs aligned: `CLAUDE/QA.md` (primary rule, new section, table row, when-to-run, rules
  summary), `/CLAUDE.md` (QA section and Always Push), `docs/development.md`, and the
  `.claude/rules/` lines that said qa-all before every commit (bash-scripts, ansible-editing,
  ccy-version-bump, python-helpers). `qa-gates.md` had no such line.

## How each gate was scoped

| Gate                   | Scoping                                                                                                                                                                 |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| toolchain              | run whole (instant) when a shell or Python file changed                                                                                                                 |
| bash/shellcheck        | no file list in the gate; `bash -n` and `shellcheck --severity=warning` on the files; classification from `qa-discovery.bash`                                           |
| semgrep                | no file list; the same temp-mirror idea on the changed files                                                                                                            |
| python                 | ruff on the files; mirrored unit test module run with plain `unittest` (the `qa-helper-tests.bash` runner takes no module names, only `--counts-file`/`--counts-token`) |
| ansible-syntax         | `ansible-playbook --syntax-check` on the affected playbooks; a task/vars file maps to every playbook naming its basename (one level only)                               |
| ansible patterns, docs | not scopable; run whole (about 0.3 s each)                                                                                                                              |
| js                     | `node --check` per file; extensions eslint on the changed files only                                                                                                    |
| suites                 | every `test-*`/hard `qa-*` script qa-all invokes whose text names a changed non-markdown file by path or basename, or that itself changed; reason printed               |

## Left to qa-all (stated in the header and QA.md)

Python-template compile, semgrep parse-coverage assertions, helper-test count assertions,
extension-compat, panel-contract, deployed-drift, and any suite no changed file is named in.
`.j2` files are covered only by a naming suite (qa-all does not lint them either).

## Known limits

- Suite selection is textual: a suite that reaches a file through a library it sources is not
  picked unless the library itself changed and is named.
- Task/vars to playbook mapping is one level deep.
- A suite that names a changed file covers it, including a suite naming itself, so a test
  fixture meant to be "uncovered" must not appear literally in the test's own text.
