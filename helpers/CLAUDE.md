# Helpers Directory

Home for non-trivial logic extracted out of Ansible playbooks (and other glue),
written in Python and driven by tests.

**Cross-references:** [playbooks/CLAUDE.md](../playbooks/CLAUDE.md) · [CLAUDE/AnsibleStyle.md](../CLAUDE/AnsibleStyle.md) · [CLAUDE/QA.md](../CLAUDE/QA.md) · [CLAUDE/StderrHygiene.md](../CLAUDE/StderrHygiene.md)

## The Rule (in stone)

> **Ansible may run trivial bash / commands inline. Anything complex MUST be
> extracted into a TDD'd helper here.**

- **Trivial** = a single command, or a couple of straight-line commands with no
  branching, loops, arrays, `case`, process substitution, or data-munging.
- **Complex** = anything with loops, conditionals, arrays, `case`, pipelines that
  carry logic, version math, or text parsing.

**Why:** Ansible 2.19's `shell:`/`command:` free-form parser (`split_args`) is
**not bash-aware**. A non-trivial inline block fails `ansible-playbook --syntax-check` with `failed at splitting arguments, either an unbalanced jinja2 block or quotes` even when the bash is valid. Constructs that trip it include
`${arr[@]}`, `case … ;;`, `< <(...)`, and `${v:-}`. Keeping logic in a tested
Python helper sidesteps that entire class of breakage.

## Zero dependencies — stdlib only (in stone)

> **Helpers and their tests import ONLY the Python standard library. No pip, no
> venv, no pytest.**

- Tests use `unittest` + `unittest.mock` (both stdlib), run with
  `python3 -m unittest`.
- This is deliberate: it avoids desktop-vs-container venv clashes entirely. A
  venv built against the host python breaks when reused inside the CCY
  container (different python, different paths). See `.claude/hooks-daemon/`
  for the elaborate fingerprinted/namespaced-venv machinery you do **not** want
  to have to replicate.
- If a helper ever genuinely needs a third-party library, that is a
  **deliberate, flagged decision** — stop and discuss it first. You would be
  signing up for namespaced-venv management, so it must never happen by accident.

## Structure & invocation

- Namespace package, **no `__init__.py`** (e.g. `helpers/pyenv/resolver.py` +
  `helpers/pyenv/install_pyenv_versions.py`).
- Split **pure logic** (fully unit-tested, no I/O) from a **thin side-effecting
  executor** that shells out / touches the filesystem.
- Call it from a play with `command:` + `argv:` (the list form bypasses
  `split_args`), with `chdir:` to the repo root so the package imports:

```yaml
- name: Install/upgrade Pyenv Versions
  ansible.builtin.command:
    argv:
      - python3
      - -m
      - helpers.pyenv.install_pyenv_versions
      - --minor-count
      - "{{ pyenv_minor_count }}"
    chdir: "{{ root_dir }}" # repo root on sys.path so the helpers package imports
  register: pyenv_install
  changed_when: "'PYENV-CHANGED' in pyenv_install.stdout"
```

- Executors should print **stable marker lines** (e.g. `PYENV-CHANGED`,
  `PYENV-WARN`, `PYENV-DONE`) so the play derives `changed_when`/`failed_when`
  without parsing free-form output.
- Drive external tools directly via environment (e.g. `PYENV_ROOT`) rather than
  `source ~/.bash_profile` — that also avoids the Fedora `BASHRCSOURCED` nounset
  trap (see [CLAUDE/AnsibleStyle.md](../CLAUDE/AnsibleStyle.md)).

## Tests (TDD, required)

- Tests **mirror the helper path**: `helpers/pyenv/resolver.py` →
  `tests/helpers/pyenv/test_resolver.py`. Write the **test first** — the hooks
  daemon's TDD handler blocks creating a source file before its test exists.
- Put the repo root on `sys.path`, then import the namespace package (ruff
  ignores `E402` for `tests/**`).
- Run: **`./scripts/qa-helper-tests.bash`** (or a specific module, e.g.
  `python3 -m unittest tests.helpers.pyenv.test_resolver`).

> **Do NOT use `python3 -m unittest discover -s tests`.** It reports
> `Ran 0 tests … OK` and exits **0** — a false pass. These are namespace packages
> with no `__init__.py`, so `discover` cannot import the start dir and silently
> collects nothing. `qa-helper-tests.bash` enumerates `tests/helpers/**/test_*.py`
> and runs them by explicit module name, and fails when it finds none. This
> document recommended `discover` until Plan 00070 measured it.

## Fail fast

- `subprocess` calls always pass an explicit `check=`. `check=True` is the norm;
  `check=False` is allowed **only** where the returncode is inspected on the
  following lines and a non-zero exit is a *result* rather than an error. No
  `|| true`, no `2>/dev/null` swallowing, no bare `except`. Errors propagate and
  stop the play — this repo's #1 rule.
- ruff's ruleset and version are both pinned; suppression comments are blocked.
  See [CLAUDE/QA.md → ruff](../CLAUDE/QA.md#ruff-the-ruleset-is-explicit-and-the-version-is-pinned).
