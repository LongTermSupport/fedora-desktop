# Claude Code Hooks - Project Configuration

**IMPORTANT**: Always check the latest online documentation before making changes to hooks. This file should be kept up-to-date with the official docs.

## Official Documentation Links

**PRIMARY REFERENCE - Always check these first:**
- **Hooks Guide**: https://code.claude.com/docs/en/hooks-guide.md
- **Hooks Reference**: https://code.claude.com/docs/en/hooks.md
- **Common Workflows**: https://code.claude.com/docs/en/common-workflows.md
- **Documentation Map**: https://code.claude.com/docs/en/claude_code_docs_map.md

## Overview

Claude Code provides a hook system that allows you to run custom commands at various points in the workflow. This project uses hooks to automatically lint Ansible playbooks when they are edited or created.

## Hook Types Available

Claude Code supports 10 hook event types:

1. **PreToolUse** - Runs before tool calls (can block them)
2. **PermissionRequest** - Executes when permission dialogs appear
3. **PostToolUse** - Runs after tool calls complete ⭐ **Used in this project**
4. **UserPromptSubmit** - Activates when users submit prompts
5. **Notification** - Runs when Claude Code sends notifications
6. **Stop** - Executes when Claude Code finishes responding
7. **SubagentStop** - Runs upon subagent task completion
8. **PreCompact** - Activates before compact operations
9. **SessionStart** - Runs when a new session starts or resumes
10. **SessionEnd** - Executes when a session terminates

## Project Hook Configuration

This project uses **PostToolUse** hooks to automatically lint Ansible playbooks after they are edited or created.

### Configuration Location

Hooks are configured in `.claude/settings.json` (project-level, version-controlled).

### Current Hooks

**Ansible Auto-Linting Hook**:
- **Event**: PostToolUse
- **Matcher**: `Edit|Write` (triggers on Edit or Write tool use)
- **Script**: `.claude/hooks/ansible-lint.sh`
- **Purpose**: Automatically run ansible-lint on playbook files after editing
- **Behavior**: Blocks operation if linting fails (exit code 2)

## Hook Input Format

Hooks receive JSON data via stdin containing information about the operation:

```json
{
  "session_id": "string",
  "transcript_path": "string",
  "cwd": "string",
  "permission_mode": "default|plan|acceptEdits|bypassPermissions",
  "hook_event_name": "PostToolUse",
  "tool_name": "Write|Edit",
  "tool_input": {
    "file_path": "/absolute/path/to/file",
    "content": "file content"
  },
  "tool_use_id": "toolu_...",
  "tool_response": {
    "filePath": "/absolute/path/to/file",
    "success": true
  }
}
```

### Extracting File Path

**Using jq in bash:**
```bash
FILE_PATH=$(cat | jq -r '.tool_input.file_path')
```

**Using Python:**
```python
import json
import sys
data = json.load(sys.stdin)
file_path = data['tool_input']['file_path']
```

## Hook Exit Codes

- **0** - Success, operation continues
- **2** - Blocking error, prevents the operation, shows stderr to user/Claude
- **Other** - Non-blocking error, shows stderr only in verbose mode

## Hook JSON Output (Optional)

For exit code 0, hooks can output JSON to provide feedback:

```json
{
  "continue": true,
  "stopReason": "optional reason string",
  "suppressOutput": true,
  "systemMessage": "Message shown to Claude and user"
}
```

## Environment Variables

All hooks have access to:
- `CLAUDE_PROJECT_DIR` - Absolute path to project root
- `CLAUDE_CODE_REMOTE` - "true" if running in web environment

## Creating New Hooks

### Method 1: Manual (Recommended for this project)

1. Create hook script in `.claude/hooks/`
2. Make executable: `chmod +x .claude/hooks/your-script.sh`
3. Add configuration to `.claude/settings.json`
4. Test with: `echo '{"tool_input":{"file_path":"/path/to/test.yml"}}' | .claude/hooks/your-script.sh`

### Method 2: Using /hooks Command

1. Run `/hooks` in Claude Code
2. Select event type
3. Add matcher pattern
4. Enter shell command
5. Choose storage location

## Security Best Practices

⚠️ **Always follow these security guidelines:**

- ✅ Validate and sanitize all inputs
- ✅ Always quote shell variables: `"$VAR"`
- ✅ Check for path traversal attacks (`..`)
- ✅ Use absolute paths
- ✅ Skip sensitive files (.env, .git, keys)
- ✅ Set reasonable timeouts (default 60s)
- ✅ Handle errors gracefully
- ✅ Don't expose sensitive data in output

## Debugging Hooks

1. **Verify registration**: Run `/hooks` to see active hooks
2. **Enable debug mode**: `claude --debug`
3. **Check stderr**: Hook execution status appears in stderr
4. **Manual testing**: Pipe test JSON to hook script
5. **Check timeout**: Default 60 seconds, configurable

Example manual test:
```bash
echo '{
  "tool_input": {
    "file_path": "/workspace/playbooks/imports/play-basic-configs.yml"
  }
}' | .claude/hooks/ansible-lint.sh
```

## Modifying Hooks

**IMPORTANT**: Before modifying hooks:

1. ✅ Check latest official documentation (links at top of this file)
2. ✅ Review current hook configuration in `.claude/settings.json`
3. ✅ Test changes manually before committing
4. ✅ Update this CLAUDE.md file with any changes
5. ✅ Document the reason for changes in git commit

**After modifying hooks:**
1. Test with sample files
2. Verify exit codes are correct
3. Check output format
4. Update project documentation if needed

## Matcher Patterns

Matchers are **case-sensitive** and support regex:

- `"Write"` - Exact match, only Write tool
- `"Edit|Write"` - Matches Edit OR Write
- `"Notebook.*"` - Matches tools starting with "Notebook"
- `"*"` or `""` - Matches all tools (use cautiously)

## Hook Performance Considerations

- Hooks have 60-second timeout by default
- Multiple matching hooks run in parallel
- Identical commands are automatically deduplicated
- Keep hooks fast - they run on every matching tool use
- Consider filtering files early to avoid unnecessary processing

## Project-Specific Guidelines

For this fedora-desktop project:

1. **Ansible playbook linting**: Use PostToolUse hook with `Edit|Write` matcher
2. **Filter files**: Only process `*.yml` files in `/playbooks/` directory
3. **Blocking behavior**: Exit with code 2 if linting fails
4. **Clear feedback**: Always output helpful messages to stderr
5. **Use project lint script**: Call `./scripts/lint` for consistent behavior
6. **Fast execution**: Ensure hooks complete within 30 seconds

## Troubleshooting

**Hook not triggering:**
- Check matcher pattern is correct
- Verify hook is registered with `/hooks` command
- Ensure script is executable
- Check file path matches your filters

**Hook failing unexpectedly:**
- Check stderr output for error messages
- Test hook manually with sample JSON
- Verify all dependencies are installed (ansible-lint, jq, etc.)
- Check timeout hasn't been exceeded

**Hook blocking when it shouldn't:**
- Verify exit codes (0 = success, 2 = block)
- Check filter logic for file paths
- Review error conditions in script

## Maintenance Requirements

⚠️ **This file MUST be kept up-to-date:**

1. When Claude Code releases new hook features
2. When hook behavior changes in documentation
3. When project adds/modifies/removes hooks
4. When troubleshooting discovers new issues
5. When security best practices are updated

**Check official docs quarterly** or when:
- Hooks stop working as expected
- New Claude Code version is released
- Adding new hooks to the project

## Additional Resources

- **Claude Code Release Notes**: Check for hook system updates
- **GitHub Issues**: Search for hook-related issues
- **Community Examples**: Look for hook recipes in the community

---

**Last Updated**: 2025-12-03
**Verified Against**: Claude Code hooks documentation (code.claude.com)
**Next Review Due**: 2026-03-03 (quarterly)
