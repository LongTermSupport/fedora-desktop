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

| Script | Checks | Files |
|--------|--------|-------|
| `qa-bash.bash` | shellcheck + `bash -n` | All bash files |
| `qa-python.bash` | `python3 -m py_compile` + ruff | All Python files |
| `qa-patterns.bash` | Semgrep rules from `.semgrep/bash-conventions.yml` | Bash files (catches `\|\| echo` and other error-hiding patterns) |

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

| Changed files | QA command |
|---------------|------------|
| Bash or Python files | `./scripts/qa-all.bash` |
| Extension JavaScript | `cd /workspace/extensions && node_modules/.bin/eslint <file>` |
| `ccy-ctrl-z-patch.js` | `./scripts/qa-ctrl-z-patch.bash` |
| Ansible playbooks | `./scripts/qa-all.bash` (includes `qa-ansible.bash` via `qa-patterns.bash`) |

---

## What QA Catches

- ✅ Bash syntax errors (`bash -n` validation)
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` if installed)
- ✅ Error-hiding bash patterns (`|| echo` — Semgrep, `.semgrep/bash-conventions.yml`)
- ✅ Ansible fail-fast violations (`failed_when: false` without `# FAIL-FAST-OK:` annotation)

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
