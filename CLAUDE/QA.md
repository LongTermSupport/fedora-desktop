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

| Script                   | Checks                                                                                                                                                   | Files                                                                                              |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `qa-bash.bash`           | `bash -n` + shellcheck. **shellcheck `error`-level findings GATE** (fail QA); `warning`/`info`/`style` are advisory.                                     | Repo-owned bash (excludes `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`) |
| `qa-python.bash`         | `python3 -m py_compile` + ruff (ruff exit ≥ 2 = hard fail; no `--fix` mutation in the check path)                                                        | Repo-owned Python files                                                                            |
| `qa-patterns.bash`       | Semgrep rules from `.semgrep/bash-conventions.yml` (`\|\| echo` and other error-hiding patterns)                                                         | Repo-owned bash                                                                                    |
| `qa-ansible.bash`        | Fail-fast grep (`failed_when: false`/`ignore_errors` without same-line `# FAIL-FAST-OK:`, case-insensitive) **plus** playbook shebang + exec-bit hygiene | `playbooks/ tasks/ vars/ environment/ roles/` (excludes `roles/vendor`), `*.yml`/`*.yaml`          |
| `qa-ansible-syntax.bash` | `ansible-playbook --syntax-check` on every playbook (files with a top-level `- hosts:`). Parse-only — safe in the CCY container                          | `playbooks/playbook-main.yml` + standalone `playbooks/imports/**`                                  |
| `qa-js.bash`             | `node --check` on repo JS + `eslint .` in `extensions/`                                                                                                  | Repo-owned `.js` (excludes vendor/node_modules) + `extensions/`                                    |

---

## GNOME Shell Extension JavaScript

Run ESLint via the binary directly (NOT `npm run lint` — blocked by hooks):

```bash
cd /workspace/extensions && node_modules/.bin/eslint speech-to-text@fedora-desktop/extension.js
```

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
- ✅ shellcheck `error`-level findings (these now **gate** — `warning`/`info`/`style` stay advisory)
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` if installed; a ruff crash, exit ≥ 2, is a hard failure)
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
