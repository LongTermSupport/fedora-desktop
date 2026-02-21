# Claude Code Project Configuration

This directory contains Claude Code configuration and hooks for this project.

## Files

- **`settings.local.json`** - Personal settings (gitignored, not shared)
- **`hooks/`** - Pre/Post tool use hooks (shared with team)

## Hooks

### validate-system-paths.sh

**Purpose:** Prevent direct editing of deployed system files.

**Enforces:** "Edit project files, deploy with Ansible" workflow.

**Blocked Paths:**
- `/etc/` - System configuration
- `/usr/` - System binaries and libraries
- `/var/` - Variable data (including our deployed scripts)
- `/opt/` - Optional packages
- `/root/` - Root user home
- `/home/` - User home directories (except project workspace)

**When Triggered:**
- Before any `Edit(*)` tool use
- Before any `Write(*)` tool use

**Behavior:**
- ✓ **Allows:** Editing files within the project repository
- ❌ **Blocks:** Editing deployed system files
- 📝 **Suggests:** Edit project files in `files/` directory, then deploy with Ansible

**Example:**

```
❌ Blocked: /var/local/claude-yolo/claude-yolo
✓ Correct: files/var/local/claude-yolo/claude-yolo
           + ansible-playbook playbooks/imports/optional/common/...
```

## Why This Matters

### Without This Hook:
1. Claude edits `/var/local/claude-yolo/script` directly
2. Change works immediately but is NOT in git
3. Next Ansible run overwrites the change
4. Change is lost, configuration drift occurs

### With This Hook:
1. Claude is blocked from editing deployed files
2. Claude edits `files/var/local/claude-yolo/script` instead
3. Change is committed to git
4. Ansible deploys the change consistently
5. Change persists and is version controlled

## Development Principles Enforced

- **Version Control:** All changes tracked in git
- **Reproducibility:** Changes can be deployed to any system
- **Fail Fast:** Catch mistakes early, not after deployment
- **Idempotency:** Ansible ensures consistent state
- **Auditability:** Git history shows all configuration changes

## Configuration

Hooks are configured in `settings.local.json` (or team-shared `settings.json`):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit(*)",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/validate-system-paths.sh"
          }
        ]
      }
    ]
  }
}
```

## Requirements

- **jq** - JSON parsing (installed via Ansible in base system setup)

## Testing

To test the hook manually:

```bash
# Create test tool use JSON
echo '{"tool": "Edit", "params": {"file_path": "/var/local/test.sh"}}' | \
  .claude/hooks/validate-system-paths.sh

# Should exit with code 2 and block message
echo $?  # Should be 2
```

## See Also

- [CLAUDE.md](../CLAUDE.md) - Full project documentation
- [Claude Code Hooks Docs](https://code.claude.com/docs/en/hooks)
