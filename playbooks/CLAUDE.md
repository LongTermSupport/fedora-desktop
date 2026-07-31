# Playbooks Directory

**Ansible style rules and patterns:** [CLAUDE/AnsibleStyle.md](../CLAUDE/AnsibleStyle.md)

**Deployment workflow (edit → playbook → deploy → test):** [CLAUDE/InfrastructureAsCode.md](../CLAUDE/InfrastructureAsCode.md)

**Complex logic belongs in a tested helper, not a shell block:** [helpers/CLAUDE.md](../helpers/CLAUDE.md)

## Complex Logic → TDD Helper (in stone)

> **Ansible may run trivial bash / commands inline. Anything complex MUST be
> extracted into a TDD'd Python helper under `helpers/`.**

- **Trivial** = a single command, or a couple of straight-line commands with no
  branching, loops, arrays, `case`, process substitution, or data-munging.
- **Complex** = loops, conditionals, arrays, `case`, logic-carrying pipelines,
  version math, text parsing — extract it.

**Why:** Ansible 2.19's `shell:`/`command:` free-form parser (`split_args`) is
**not bash-aware** and rejects non-trivial inline blocks at `--syntax-check`
time (`failed at splitting arguments, either an unbalanced jinja2 block or quotes`) even when the bash itself is valid. A tested Python helper avoids the
whole class of breakage, and is invoked with `command:` + `argv:` (which bypasses
`split_args`) plus `chdir: "{{ root_dir }}"`. Helpers are **stdlib-only** (no
venv, no pip) — full pattern, rationale, and test layout in
[helpers/CLAUDE.md](../helpers/CLAUDE.md). First example: `helpers/pyenv/` driving
`playbooks/imports/play-python.yml`.

## Executable Playbooks Requirement

**ALL playbooks in this directory MUST be directly executable.**

### Requirements for Every Playbook

1. **Shebang**: First line must be `#!/usr/bin/env ansible-playbook`
2. **Executable permission**: File must have execute bit set (`chmod +x`)
3. **Direct execution**: Must be runnable by path without `ansible-playbook` prefix

### Creating New Playbooks

When creating a new playbook file, ALWAYS:

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  name: Your Playbook Name
  # ... rest of playbook
```

Then make it executable:

```bash
chmod +x playbooks/path/to/new-playbook.yml
```

### Automated Script

To add shebangs to all playbooks and make them executable, run:

```bash
./scripts/make-playbooks-executable.bash
```

This script:

- Scans all `.yml` files in `playbooks/` directory
- Adds shebang if missing
- Sets executable permission
- Reports what was updated

### Why This Matters

- **Simpler execution**: `./playbooks/playbook-main.yml` instead of `ansible-playbook playbooks/playbook-main.yml`
- **Copy-paste friendly**: Users can copy playbook path and run directly
- **Consistent with Unix conventions**: Executable scripts should have shebangs
- **Better DX**: Reduces friction when running optional playbooks

### Verification

Test that a playbook is properly executable:

```bash
# Should show ansible-playbook version, not "permission denied"
./playbooks/imports/play-comms.yml --version
```

### Pre-commit Hook Recommendation

Consider adding a pre-commit check to enforce this:

```bash
# Check all .yml files in playbooks/ have shebang and are executable
find playbooks -name "*.yml" -type f | while read file; do
    if ! head -n1 "$file" | grep -q "^#!/usr/bin/env ansible-playbook"; then
        echo "ERROR: Missing shebang in $file"
        exit 1
    fi
    if [ ! -x "$file" ]; then
        echo "ERROR: Not executable: $file"
        exit 1
    fi
done
```
