# QA — Quality Assurance Scripts

## Primary Rule

**ALWAYS run QA before committing changes to Bash, Python, or Ansible files.**

**ALWAYS and ONLY use this single command:**

```bash
./scripts/qa-all.bash
```

**NEVER use individual scripts directly** (`qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`) — always use `qa-all.bash`.

---

## What qa-all.bash Runs

`qa-all.bash` runs six stages and merges their JSON into `/tmp/qa-results.json`. A
missing **required** tool makes a stage (and the whole run) exit `2`; a real
analyser crash (e.g. ruff/shellcheck exit ≥ 2) is a hard failure, never silently
treated as "0 issues".

| Script                   | Checks                                                                                                                                                                                                                                                                       | Files                                                                                              |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `qa-bash.bash`           | `bash -n` (always) + shellcheck (only when `shellcheck` is in PATH — silently skipped if absent). **shellcheck `error`-level findings GATE** (fail QA); `warning`/`info`/`style` are advisory.                                                                               | Repo-owned bash (excludes `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`) |
| `qa-python.bash`         | `python3 -m py_compile` + ruff (ruff exit ≥ 2 = hard fail; no `--fix` mutation in the check path)                                                                                                                                                                            | Repo-owned Python files                                                                            |
| `qa-patterns.bash`       | Semgrep rules from `.semgrep/bash-conventions.yml` (`\|\| echo` and other error-hiding patterns)                                                                                                                                                                             | Repo-owned bash                                                                                    |
| `qa-ansible.bash`        | Fail-fast grep (`failed_when: false`/`ignore_errors` without same-line `# FAIL-FAST-OK:`, case-insensitive), **self-default vars** (`x: "{{ x \| default(…) }}"` — the 2.19 recursive-loop footgun `--syntax-check` can't see), **plus** playbook shebang + exec-bit hygiene | `playbooks/ tasks/ vars/ environment/ roles/` (excludes `roles/vendor`), `*.yml`/`*.yaml`          |
| `qa-ansible-syntax.bash` | `ansible-playbook --syntax-check` on every playbook (files with a top-level `- hosts:`). Parse-only — safe in the CCY container                                                                                                                                              | `playbooks/playbook-main.yml` + standalone `playbooks/imports/**`                                  |
| `qa-js.bash`             | `node --check` on repo JS + `eslint .` in `extensions/`                                                                                                                                                                                                                      | Repo-owned `.js` (excludes vendor/node_modules) + `extensions/`                                    |

---

## GNOME Shell Extension JavaScript

Run ESLint via the binary directly (NOT `npm run lint` — blocked by hooks):

```bash
cd /workspace/extensions && node_modules/.bin/eslint speech-to-text@fedora-desktop/extension.js
```

---

## Helper Unit Tests + Extension Version Compatibility

Helper packages under `helpers/` are stdlib-only (`helpers/CLAUDE.md`). Their unit
tests are namespace-package modules, so `unittest discover` cannot collect them —
run them with the dedicated runner, which enumerates `tests/helpers/**/test_*.py`
and runs them by explicit module name:

```bash
./scripts/qa-helper-tests.bash
```

A separate **static** gate confirms every `extensions/<uuid>/metadata.json`
declares support for the GNOME Shell major that this branch's Fedora release ships
(`vars/fedora-version.yml`). It is session-free (unlike the runtime
`helpers.gnome.verify_extension`), so it runs in CI on the repo source:

```bash
python3 -m helpers.gnome.check_extension_compat
```

The Fedora→GNOME-Shell map lives in `helpers/gnome/fedora_compat.py`
(`FEDORA_TO_GNOME_MAJOR`). When cutting a new `F<N>` branch, add that release's
GNOME major there — an unmapped Fedora version fails the gate by design, forcing a
human to confirm the GNOME version. Both run automatically in the `helpers` CI job
(`.github/workflows/qa.yml`).

---

## CCY ctrl+z Patch

For changes to `ccy-ctrl-z-patch.js`, run the dedicated patch QA script:

```bash
# First run (installs latest Claude Code into scripts/qa-ccy/node_modules/):
./scripts/qa-ctrl-z-patch.bash --update

# Subsequent runs (uses cached install, fast):
./scripts/qa-ctrl-z-patch.bash

# After a Claude Code release, refresh and re-verify:
./scripts/qa-ctrl-z-patch.bash --update
```

---

## When to Run What

| Changed files         | QA command                                                                  |
| --------------------- | --------------------------------------------------------------------------- |
| Bash or Python files  | `./scripts/qa-all.bash`                                                     |
| Extension JavaScript  | `cd /workspace/extensions && node_modules/.bin/eslint <file>`               |
| `ccy-ctrl-z-patch.js` | `./scripts/qa-ctrl-z-patch.bash`                                            |
| Ansible playbooks     | `./scripts/qa-all.bash` (runs `qa-ansible.bash` + `qa-ansible-syntax.bash`) |

---

## What QA Catches

- ✅ Bash syntax errors (`bash -n` validation)
- ✅ shellcheck `error`-level findings, **when `shellcheck` is in PATH** (silently skipped if absent; `warning`/`info`/`style` are advisory even when it runs)
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` — **required**; `qa-python.bash` exits 2 with an error if ruff is absent; a ruff crash, exit ≥ 2, is also a hard failure)
- ✅ Error-hiding bash patterns (`|| echo` — Semgrep, `.semgrep/bash-conventions.yml`)
- ✅ Ansible fail-fast violations (`failed_when: false` without `# FAIL-FAST-OK:` annotation)
- ✅ Ansible playbook **syntax** errors (`ansible-playbook --syntax-check`, catches the 2.19 parse hazards)
- ✅ Playbook hygiene (every `- hosts:` playbook has the `ansible-playbook` shebang + exec bit)
- ✅ JavaScript syntax (`node --check`) + ESLint across `extensions/`

## What QA Does NOT Catch (Known Limitations)

- ❌ **Runtime API incompatibilities** — e.g., calling a library method with parameters it no longer accepts
- ❌ **Import errors** — missing dependencies only fail at runtime
- ❌ **Logic errors** — code that runs but produces wrong results

**For Python files that use external libraries** (like `wsi-stream` using RealtimeSTT):

- After editing, **manually test the script** to verify it works
- Library APIs can change between versions
- Syntax checking alone is not sufficient for integration code

---

## Example Workflow

```bash
# 1. Make changes
vim files/home/.local/bin/wsi-stream

# 2. Run QA
./scripts/qa-all.bash

# 3. If QA passes, deploy and TEST the actual script (on HOST, not in CCY container)
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
~/.local/bin/wsi-stream --help  # Verify it imports/runs

# 4. Only then commit
git add files/home/.local/bin/wsi-stream
git commit -m "fix: update wsi-stream"
```

## Rules Summary

1. **Run `./scripts/qa-all.bash` before EVERY commit** that touches Bash or Python files
2. **Run ESLint before EVERY commit** that touches extension JavaScript
3. **Run `./scripts/qa-ctrl-z-patch.bash` before EVERY commit** that touches `ccy-ctrl-z-patch.js`
4. **Fix all errors** before committing — QA failures indicate broken code
5. **Do not skip QA** — even for "small" changes
