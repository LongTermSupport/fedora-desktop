---
paths:
  - "helpers/**"
  - "tests/helpers/**"
  - "**/*.py"
---

# You are editing Python — helpers are stdlib-only and test-first

Full rules: [helpers/CLAUDE.md](../../helpers/CLAUDE.md) ·
QA: [CLAUDE/QA.md](../../CLAUDE/QA.md)

## In stone

- **Stdlib only.** No pip, no venv, no pytest — `unittest` + `unittest.mock`. A helper that
  genuinely needs a third-party library is a **deliberate, flagged decision**: stop and ask.
- **Test first.** `helpers/pyenv/resolver.py` → `tests/helpers/pyenv/test_resolver.py`. The
  TDD handler blocks creating a source file before its test exists.
- **Namespace packages, no `__init__.py`.** Split pure logic (unit-tested, no I/O) from a
  thin side-effecting executor.
- **Invoke with `command:` + `argv:`** and `chdir: "{{ root_dir }}"` — the list form bypasses
  the 2.19 `split_args` parser.
- **`subprocess` uses an explicit `check=`.** `check=True` normally; `check=False` **only**
  where the returncode is checked on the following lines and a non-zero exit is a *result*
  rather than an error. No bare `except`, no `|| true`, no `2>/dev/null`.
- **Marker lines to stdout, diagnostics to stderr** (`PYENV-CHANGED`, `PYENV-DONE`), so the
  play derives `changed_when` without parsing free-form output.

## Running the tests

```bash
./scripts/qa-helper-tests.bash
```

**Do NOT use `python3 -m unittest discover -s tests`** — it reports `Ran 0 tests … OK` and
exits **0**. These are namespace packages, so `discover` cannot import the start dir and
silently collects nothing. A gate reporting zero is a failure, not a pass.

## ruff is version-pinned

The enforced ruleset is ruff's **default** set, which grows every release, so the verdict is
a function of the version. `/.ruff-version` is the single source of truth and
`scripts/qa-python.bash` asserts the installed ruff matches it. If it fails, match the pin —
do not "fix" the findings a different ruff invented. Suppression comments (`# noqa`,
`# type: ignore`) are **blocked**: fix the code, or exempt in `ruff.toml` with a stated reason.
