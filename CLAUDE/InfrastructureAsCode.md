# Infrastructure as Code — Ansible-Only Workflow

## Core Rule

**ALL system changes MUST go through Ansible playbooks. Manual operations are PROHIBITED.**

This is a hard rule with NO exceptions.

## Prohibited Manual Actions

- ❌ **Manual file copies** — `sudo cp file /path/` or `cp file dest`
- ❌ **Manual installations** — `sudo dnf install`, `npm install -g`, `pip install`
- ❌ **Manual configuration edits** — Direct editing of system files
- ❌ **Manual service management** — `systemctl enable/start` commands
- ❌ **Manual downloads** — `curl` or `wget` scripts piped to shell
- ❌ **Manual symlinks** — `ln -s` operations
- ❌ **Manual testing** — `pip install package` to "test if it works"
- ❌ **Direct file creation** — Creating files directly in `~/.local/bin/` or `/usr/local/bin/`

## Required Workflow

**Edit → Playbook → Deploy → Test** (in this order, always):

1. **Edit source files** in the repository (e.g., `extensions/`, `files/`)
2. **Update/create playbook** in `playbooks/imports/`
3. **Run the playbook** to deploy to the system
4. **Test the deployed result** on the system

```bash
# 1. Edit files in repo
vim extensions/my-extension/script.sh

# 2. Update playbook to deploy
vim playbooks/imports/optional/common/play-my-feature.yml

# 3. Deploy via Ansible
ansible-playbook playbooks/imports/optional/common/play-my-feature.yml

# 4. Test the deployed result
~/.local/bin/script.sh --test
```

## Why Manual Operations Are Always Wrong

- **Creates drift** between repo and system state
- **Breaks auditability** — changes not tracked in version control
- **Defeats reproducibility** — system can't be rebuilt from git
- **"Quick test" trap** — manual state often becomes permanent
- **Debugging confusion** — is the bug in the playbook or the manual config?

## Correct Ansible Patterns

**Always use Ansible modules:**
- `copy`, `template` — for file deployment
- `package`, `dnf` — for installations
- `service`, `systemd` — for service management
- `file` — for permissions, ownership, symlinks
- `get_url` — for downloads

**Ensure idempotency:**
- Playbooks must be safe to run multiple times
- Use `creates` parameter for shell commands that should run once
- Use declarative state (`state: present`, `state: started`)
- Check before change with conditionals

```yaml
# GOOD: Idempotent with creates
- name: Install from URL
  shell: wget https://example.com/install.sh && bash install.sh
  args:
    creates: /usr/bin/installed_binary

# BAD: Will fail on second run
- name: Install from URL
  shell: wget https://example.com/install.sh && bash install.sh
```

**Test playbook changes:**
- Verify with `--check` or `--diff` flags before applying

## For Package Availability Checks

```bash
# ✅ GOOD - Just query, don't install
dnf info package-name
pip index versions package-name

# ❌ BAD - Actually installs
pip install package-name
dnf install package-name
```

## If Something Doesn't Work After Deployment

1. Fix the **PLAYBOOK**, not the system
2. Re-run the playbook
3. Test again
4. Repeat until working

**The playbook IS the source of truth. The system state is just a reflection of it.**

## If You Catch Manual Steps Being Suggested

1. STOP immediately
2. Identify the Ansible playbook that should handle this
3. Verify the playbook will correctly deploy the changes
4. If playbook is missing/incomplete, UPDATE THE PLAYBOOK FIRST
5. Then recommend running the playbook
