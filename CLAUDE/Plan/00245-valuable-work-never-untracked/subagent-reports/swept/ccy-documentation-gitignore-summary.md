# CCY Custom Dockerfile Documentation and Gitignore Detection

## Summary

Enhanced CCY (Claude Code YOLO mode) to provide comprehensive documentation about custom Dockerfiles and detect when custom Dockerfiles are gitignored.

## Problem Addressed

1. **Missing Documentation**: Users with custom CCY Dockerfiles in other projects were confused about how to properly create them. No comprehensive documentation existed inside the containers.

2. **Gitignore Problem**: When users gitignore `.claude/ccy/` completely, their custom Dockerfile gets ignored too, which means:
   - Team members don't get the custom container configuration
   - CI/CD doesn't have the right environment
   - Container setup is lost when cloning repository

## Solution Implemented

### 1. Created Comprehensive Documentation

**File**: `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt`

This documentation covers:
- Quick start guide
- Understanding the container environment
- When to use custom Dockerfiles (and when not to)
- Creating custom Dockerfiles (3 methods: AI-guided, template-based, manual)
- Best practices (cache mounts, layer organization, comments, verification)
- Common examples (Python+PostgreSQL, Go+AWS CLI, TypeScript+Docker-in-Docker)
- Troubleshooting
- **Git tracking requirements** (the critical section)

Key features:
- Plain text format (not markdown) to avoid hook conflicts in deployment locations
- Self-contained and comprehensive
- Includes exact paths and commands
- Explains what should and shouldn't be tracked in git

### 2. Added Dockerfile Gitignore Detection

**Files Modified**:
- `files/var/local/claude-yolo/lib/common.bash`

**New Function**: `check_dockerfile_gitignored()`

This function:
- Checks if `.claude/ccy/Dockerfile` exists but is gitignored
- **Warns** (doesn't fail) if Dockerfile is being ignored
- Provides clear instructions on how to fix
- Explains why it matters (team won't get config, CI/CD issues)
- Shows safe files to track vs dangerous files to never track

**Integration**: Called from existing `check_ccy_gitignore_safety()` function which runs on every CCY startup.

### 3. Container Integration

**Files Modified**:
- `files/var/local/claude-yolo/Dockerfile` - Copy documentation into container
- `files/opt/claude-yolo/ccy-startup-info.txt` - Reference documentation in startup banner
- `playbooks/imports/optional/common/play-install-claude-yolo.yml` - Deploy documentation

The documentation is now:
- Injected into every CCY container at `/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt`
- Referenced in the startup banner
- Always available via `cat /opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt`

## Version Bumps

Following project requirements for version tracking:

1. **Dockerfile**: `1.4` → `1.5` (added documentation directory and file)
2. **CCY Wrapper**: `3.2.0` → `3.3.0` (added Dockerfile gitignore detection)
3. **Common Library**: `1.3.0` → `1.4.0` (new `check_dockerfile_gitignored()` function)
4. **REQUIRED_CONTAINER_VERSION**: `1.4` → `1.5` (kept in sync with Dockerfile)

## What Should Be Tracked in Git

The enhanced detection now clearly explains:

### ✅ Safe to Track:
- `.claude/ccy/Dockerfile` - Custom container configuration (SHOULD be tracked!)
- `.claude/ccy/.gitignore` - Protection rules

### ❌ NEVER Track:
- `.claude/ccy/.last-launch.conf` - Contains token names and SSH key paths
- `.claude/ccy/*` - Session data and conversation history

### Recommended .gitignore Pattern:

```gitignore
# Claude Code state (but keep custom configs)
.claude/*
!.claude/ccy/Dockerfile
!.claude/ccb/Dockerfile
!.claude/agents/
!.claude/hooks/
!.claude/commands/

# Claude tokens (CRITICAL - never track!)
.claude-tokens/
```

## Detection Behavior

When CCY starts, it now:

1. **Enforces** `.claude/ccy/.gitignore` exists with correct content (protects session data)
2. **FAILS** if dangerous files (like `.last-launch.conf`) are tracked
3. **WARNS** if `Dockerfile` exists but is gitignored (won't be shared)

The warning is informative but non-blocking - the project will still work, but users are informed that their team won't get the custom container setup.

## User Experience

### Before:
- Users confused about how to create custom Dockerfiles
- No documentation available inside containers
- Custom Dockerfiles silently gitignored with no warning
- Team members clone repo and get base container instead of custom setup

### After:
- Comprehensive documentation always available in containers
- Clear warning when Dockerfile is gitignored
- Instructions on how to fix gitignore issues
- Examples and best practices for common use cases

## Testing

All code validated:
- ✅ Bash syntax: `./scripts/qa-bash.bash` - 188 files checked, all passed
- ✅ Python syntax: `./scripts/qa-python.bash` - 94 files checked, all passed
- ✅ Combined QA: `./scripts/qa-all.bash` - All checks passed

## Deployment

To deploy these changes:

```bash
# On host system
cd ~/Projects/fedora-desktop

# Deploy updates
ansible-playbook playbooks/imports/optional/common/play-install-claude-yolo.yml

# The playbook will:
# 1. Create /opt/claude-yolo/docs/ directory
# 2. Copy CUSTOM-DOCKERFILES.txt to container build location
# 3. Rebuild claude-yolo:latest with version 1.5
# 4. Update wrapper scripts with new detection

# Verify
podman image inspect claude-yolo:latest --format '{{index .Config.Labels "claude-yolo-version"}}'
# Should show: 1.5

# Test in a project
cd ~/Projects/some-project
ccy
# Should see new startup banner mentioning custom Dockerfile documentation
# If .claude/ccy/Dockerfile exists and is gitignored, will see warning
```

## Files Modified

1. `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt` - **NEW** - Comprehensive documentation
2. `files/var/local/claude-yolo/lib/common.bash` - Added `check_dockerfile_gitignored()`
3. `files/var/local/claude-yolo/Dockerfile` - Version bump + documentation injection
4. `files/var/local/claude-yolo/claude-yolo` - Version bumps
5. `files/opt/claude-yolo/ccy-startup-info.txt` - Reference to documentation
6. `playbooks/imports/optional/common/play-install-claude-yolo.yml` - Deploy documentation

## Impact

- **Positive**: Users get clear guidance on custom Dockerfiles
- **Positive**: Detection prevents team members from missing custom container configs
- **Positive**: Better documentation improves CCY adoption
- **No Breaking Changes**: All changes are additive or non-blocking warnings
- **Minimal Performance Impact**: Detection runs once at startup (milliseconds)

## Future Enhancements

Potential improvements for future iterations:

1. Add `--check-gitignore` flag to explicitly audit git tracking status
2. Offer to automatically fix gitignore when Dockerfile is ignored
3. Add similar detection for CCB (Claude Code Browser mode)
4. Create a `ccy --doctor` command that runs all health checks
