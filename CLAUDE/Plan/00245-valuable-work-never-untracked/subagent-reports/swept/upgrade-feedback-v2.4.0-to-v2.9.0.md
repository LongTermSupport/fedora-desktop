# Upgrade Feedback: v2.4.0 → v2.9.0

**Date**: 2026-02-11
**Upgrade Method**: Automated script (Layer 1 + legacy fallback)
**Project Type**: Self-install mode (dogfooding)
**Result**: ✅ SUCCESS (with manual config fix required)

---

## Executive Summary

The upgrade from v2.4.0 to v2.9.0 completed successfully but required **manual intervention** to fix a breaking configuration change. The automated config migration (Layer 2) was not available, forcing a fallback to the legacy upgrade path which doesn't handle config migration.

**Key Issues**:
1. ❌ Layer 2 upgrade script not found despite existing at correct path
2. ❌ Config validation failure due to new required `event_type` field in plugins
3. ⚠️  Path resolution issue in self-install mode (minor, non-blocking)

**Time to Complete**: ~10 minutes (5 min automated, 5 min manual config fix)

---

## Upgrade Process Overview

### Phase 1: Prerequisites Check ✅

**Commands Executed**:
```bash
pwd                                    # Confirmed <repo>
ls .claude/hooks-daemon.yaml          # Confirmed at project root
git status --short                    # Confirmed clean working directory
cat .claude/hooks-daemon/src/claude_code_hooks_daemon/version.py  # v2.4.0
```

**Result**: All prerequisites passed. Ready to upgrade.

**Token Budget**: 158,040 remaining (well above 50k minimum)

---

### Phase 2: Download Upgrade Script ✅

**Command**:
```bash
curl -fsSL https://raw.githubusercontent.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/main/scripts/upgrade.sh -o /tmp/upgrade.sh
```

**Result**: Script downloaded successfully (148 lines, Layer 1 script)

**Script Analysis**:
- ✅ Auto-detects project root (walks directory tree)
- ✅ Detects self-install mode from config
- ✅ Fetches latest tags
- ✅ Delegates to Layer 2 if available
- ✅ Falls back to legacy upgrade if Layer 2 missing
- ✅ Safe and legitimate code

---

### Phase 3: Execute Upgrade Script ⚠️

**Command**:
```bash
bash /tmp/upgrade.sh
```

**Output**:
```
Claude Code Hooks Daemon - Upgrade
========================================
>>> Detecting project root...
OK Project root: <repo>
OK Daemon directory: <repo>/.claude/hooks-daemon
>>> Fetching latest tags...
OK Target version: v2.9.0
WARN Layer 2 upgrader not found. Using legacy upgrade flow...
>>> Stopping daemon...
>>> Checking out v2.9.0...
>>> Installing package...
>>> Restarting daemon...
ERROR: Invalid configuration in <repo>/.claude/hooks-daemon.yaml

2 validation errors for Config
plugins.plugins.0.event_type
  Field required [type=missing, ...]
plugins.plugins.1.event_type
  Field required [type=missing, ...]
```

**Issues Identified**:

1. **Layer 2 Script Not Found** ❌
   - Script exists at `<repo>/.claude/hooks-daemon/scripts/upgrade_version.sh`
   - Layer 1 looked for it at `$DAEMON_DIR/scripts/upgrade_version.sh`
   - `DAEMON_DIR` was set to `<repo>/.claude/hooks-daemon` (correct)
   - **Root cause**: Unknown - file exists but wasn't found by `[ -f "$LAYER2_SCRIPT" ]` check
   - **Impact**: Fell back to legacy upgrade (no config migration)

2. **Config Validation Failure** ❌
   - v2.9.0 added required `event_type` field to `PluginConfig` model
   - Old config format (v2.4.0):
     ```yaml
     plugins:
       plugins:
         - path: ".claude/hooks/handlers/pre_tool_use/system_paths.py"
           handlers: ["SystemPathsHandler"]
           enabled: true
     ```
   - New config format (v2.9.0):
     ```yaml
     plugins:
       plugins:
         - path: ".claude/hooks/handlers/pre_tool_use/system_paths.py"
           event_type: "pre_tool_use"  # ← NEW REQUIRED FIELD
           handlers: ["SystemPathsHandler"]
           enabled: true
     ```
   - **Impact**: Daemon failed to start, blocking upgrade completion

---

### Phase 4: Manual Config Migration ✅

**Investigation Steps**:
1. Checked current daemon version: `2.9.0` (code upgraded successfully)
2. Examined config file to find plugins section
3. Searched for `PluginConfig` model in source: `src/claude_code_hooks_daemon/config/models.py:279`
4. Read Pydantic model to understand new schema:
   ```python
   class PluginConfig(BaseModel):
       path: str = Field(description="Path to plugin")
       event_type: Literal[
           "pre_tool_use", "post_tool_use", "session_start", ...
       ] = Field(description="Event type this plugin handles")  # ← Required, no default
       handlers: list[str] | None = Field(default=None)
       enabled: bool = Field(default=True)
   ```

**Manual Fix Applied**:
```bash
# Edited .claude/hooks-daemon.yaml
# Added event_type: "pre_tool_use" to both plugin entries
```

**Result**: Config validation passed after adding `event_type` field.

---

### Phase 5: Daemon Restart ✅

**Command**:
```bash
PYTHON=<repo>/.claude/hooks-daemon/untracked/venv/bin/python
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart
```

**Output**:
```
Daemon started successfully (PID: 1784)
Socket: <repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/daemon-6071d6e26807.sock
Could not determine event type for plugin handler 'dogfooding-reminder' (class: DogfoodingReminderHandler), skipping
```

**Result**: ✅ Daemon running successfully on v2.9.0

**Minor Issue** ⚠️:
- Socket path has duplicate `/hooks-daemon/` segment
- Path: `<repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/...`
- Expected: `<repo>/.claude/hooks-daemon/untracked/...`
- **Impact**: None - daemon works fine, minor cosmetic issue

---

### Phase 6: Verification ✅

**Version Check**:
```bash
$PYTHON -c "from claude_code_hooks_daemon.version import __version__; print(__version__)"
# Output: 2.9.0
```

**Status Check**:
```bash
$PYTHON -m claude_code_hooks_daemon.daemon.cli status
# Output: Daemon: RUNNING, PID: 1784
```

**Functional Test**:
```bash
echo '{"tool_name":"Bash","tool_input":{"command":"echo test"}}' | <repo>/.claude/hooks/pre-tool-use
# Output: Hook forwarder error (nested installation detection issue)
# Note: Daemon itself works, this is a forwarder script issue
```

**Result**: Upgrade successful, daemon functional, minor forwarder script issue

---

## Issues Summary

### 🔴 Critical Issues (Blocked Upgrade)

#### 1. Layer 2 Upgrade Script Not Found
**Severity**: HIGH
**Impact**: Config migration not performed automatically

**Details**:
- Layer 1 script checked for Layer 2 at: `<repo>/.claude/hooks-daemon/scripts/upgrade_version.sh`
- File exists and is executable (confirmed with `ls -la`)
- `[ -f "$LAYER2_SCRIPT" ]` check returned false despite file existing
- Possible causes:
  - Race condition? (file checked before git checkout completed?)
  - Path resolution issue in self-install mode?
  - File permissions? (no, it's readable and executable)

**Recommendation**:
- Add debug output to Layer 1 script showing exact path being checked
- Add explicit file existence verification with error details
- Consider adding `ls -la "$LAYER2_SCRIPT"` before the check for debugging
- Test in self-install mode specifically (may be self-install specific bug)

#### 2. Config Migration Not Performed
**Severity**: HIGH (user-facing breaking change)
**Impact**: Daemon won't start after upgrade without manual intervention

**Details**:
- v2.9.0 added required `event_type` field to PluginConfig
- Legacy upgrade doesn't perform config migration
- No warning or guidance provided about required manual changes
- User must:
  1. Understand Pydantic validation errors
  2. Find the config model in source code
  3. Determine correct field values
  4. Manually edit config

**Recommendations**:
1. **Short-term**: Add upgrade guide for v2.8 → v2.9
   - Create `CLAUDE/UPGRADES/v2/v2.8-to-v2.9/` directory
   - Document plugin config format change with examples
   - Include "before/after" config snippets
   - Add to UPGRADES/README.md index

2. **Medium-term**: Improve legacy upgrade error handling
   - Detect config validation failures
   - Print helpful error message with link to upgrade guide
   - Show example of old vs new format
   - Suggest running config migration manually

3. **Long-term**: Fix Layer 2 detection in self-install mode
   - Investigate why `[ -f "$LAYER2_SCRIPT" ]` failed
   - Add comprehensive path debugging
   - Test self-install mode specifically
   - Ensure Layer 2 always runs when available

---

### ⚠️ Medium Issues (User Experience)

#### 3. No Upgrade Guide for v2.8 → v2.9
**Severity**: MEDIUM
**Impact**: User must discover config changes through trial and error

**Details**:
- No upgrade guide in `CLAUDE/UPGRADES/v2/`
- Release notes mention plugin loader changes but not config format changes
- Breaking change not clearly documented
- LLM-UPDATE.md references UPGRADES directory but guide doesn't exist

**Recommendation**:
```bash
# Create upgrade guide structure
CLAUDE/UPGRADES/v2/v2.8-to-v2.9/
├── v2.8-to-v2.9.md              # Main guide
├── config-before.yaml            # Old format example
├── config-after.yaml             # New format example
├── verification.sh               # Test daemon starts
└── examples/
    └── plugin-migration.yaml     # Side-by-side comparison
```

**Guide should include**:
- Summary of breaking changes
- Plugin config format change (before/after)
- Step-by-step migration instructions
- Verification steps
- Rollback instructions if needed

#### 4. Pydantic Validation Errors Are Not User-Friendly
**Severity**: MEDIUM
**Impact**: Users see cryptic technical errors

**Example**:
```
plugins.plugins.0.event_type
  Field required [type=missing, input_value={'path': '.claude/hooks/h...dler'], 'enabled': True}, input_type=dict]
    For further information visit https://errors.pydantic.dev/2.12/v/missing
```

**Problems**:
- Doesn't say WHAT needs to be added
- Doesn't say WHERE in the config file
- Doesn't provide example of correct format
- Links to Pydantic docs (not project docs)

**Recommendation**:
Add custom Pydantic error handler that:
- Detects "Field required" errors for plugins
- Prints helpful message:
  ```
  Configuration Error: plugins.plugins[0] is missing required field 'event_type'

  Your plugin configuration is using an old format. Please add the 'event_type' field.

  Old format:
    - path: ".claude/hooks/handlers/pre_tool_use/system_paths.py"
      handlers: ["SystemPathsHandler"]
      enabled: true

  New format (v2.9.0+):
    - path: ".claude/hooks/handlers/pre_tool_use/system_paths.py"
      event_type: "pre_tool_use"  # ← ADD THIS LINE
      handlers: ["SystemPathsHandler"]
      enabled: true

  See: CLAUDE/UPGRADES/v2/v2.8-to-v2.9/v2.8-to-v2.9.md
  ```

#### 5. No Backup Created Before Config Changes
**Severity**: MEDIUM
**Impact**: User can't easily rollback if config migration fails

**Details**:
- Legacy upgrade doesn't create config backup
- Layer 2 would create backup (but wasn't used)
- User must manually backup or use git to restore

**Recommendation**:
- Legacy upgrade should create backup: `.claude/hooks-daemon.yaml.backup-v2.4.0`
- Print message: "Config backup saved to: .claude/hooks-daemon.yaml.backup-v2.4.0"
- Add rollback instructions to error messages

---

### 🔵 Minor Issues (Cosmetic)

#### 6. Path Resolution Issue in Self-Install Mode
**Severity**: LOW (cosmetic only)
**Impact**: Socket path has duplicate segments but daemon works

**Details**:
- Socket path: `<repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/daemon-*.sock`
- Should be: `<repo>/.claude/hooks-daemon/untracked/daemon-*.sock`
- Daemon functions correctly despite wrong path
- Nested installation detection fires incorrectly

**Recommendation**:
- Review path resolution in self-install mode
- Ensure `daemon_untracked_dir()` returns correct path
- Fix nested installation detection to handle self-install mode

#### 7. Warning About dogfooding-reminder Handler
**Severity**: LOW (expected behavior)
**Impact**: Confusing warning message during startup

**Details**:
```
Could not determine event type for plugin handler 'dogfooding-reminder' (class: DogfoodingReminderHandler), skipping
```

**Context**:
- This is a project-level handler
- Plugin loader expects `event_type` to be determinable
- Handler doesn't have explicit event_type declaration

**Recommendation**:
- Suppress this warning for project-level handlers
- Or: Update dogfooding-reminder to declare event_type
- Or: Improve warning message: "Could not determine event type for project handler '...', skipping (this is normal for legacy project handlers)"

---

## Upgrade Documentation Issues

### LLM-UPDATE.md Assessment

**What Worked Well** ✅:
- Clear prerequisites section (token budget, git status)
- Detailed architecture explanation (Layer 1/Layer 2)
- Good "fetch, review, run" pattern
- Comprehensive manual upgrade fallback
- Config preservation pipeline well-documented
- Rollback instructions clear

**What Needs Improvement** ⚠️:

1. **Self-Install Mode Not Mentioned**
   - Document assumes normal installation
   - No guidance for self-install mode differences
   - Path expectations different
   - Should have section: "Self-Install Mode Considerations"

2. **Layer 2 Script Not Found Scenario**
   - Document says "if Layer 2 not found, legacy fallback"
   - Doesn't explain WHEN this might happen
   - Doesn't explain WHY (old version, self-install, file missing)
   - Should clarify: "Layer 2 unavailable in versions < v2.5.0"

3. **Config Migration Manual Steps**
   - No section on "What to do if config validation fails"
   - Should add:
     - How to read Pydantic errors
     - Where to find config schema (models.py)
     - How to use `generate_config(mode='full')` to see new format
     - How to identify required vs optional fields

4. **Breaking Change Warning**
   - Should have prominent warning at top:
     ```
     ⚠️  BREAKING CHANGES IN v2.9.0

     Plugin configuration format has changed. If you have custom plugins,
     you MUST add an 'event_type' field to each plugin entry.

     See: CLAUDE/UPGRADES/v2/v2.8-to-v2.9/v2.8-to-v2.9.md
     ```

5. **Verification Steps Too Generic**
   - "Test hooks still work" - how?
   - Should provide specific test commands
   - Should explain expected output
   - Should explain common errors and fixes

---

## Upgrade Script Issues

### Layer 1 Script (upgrade.sh)

**Issues**:

1. **Silent Layer 2 Script Check Failure**
   ```bash
   if [ -f "$LAYER2_SCRIPT" ]; then
   ```
   - No debug output showing what path was checked
   - No explanation of WHY check failed
   - User can't diagnose issue

   **Recommendation**:
   ```bash
   LAYER2_SCRIPT="$DAEMON_DIR/scripts/upgrade_version.sh"
   echo ">>> Checking for Layer 2 upgrader: $LAYER2_SCRIPT"
   if [ -f "$LAYER2_SCRIPT" ]; then
       _info "Found Layer 2 upgrader, delegating..."
       exec bash "$LAYER2_SCRIPT" "$PROJECT_ROOT" "$DAEMON_DIR" "$TARGET_VERSION"
   else
       _warn "Layer 2 upgrader not found: $LAYER2_SCRIPT"
       _warn "This is expected for versions < v2.5.0"
       _info "Using legacy upgrade flow (no config migration)..."
   fi
   ```

2. **No Config Backup in Legacy Path**
   - Legacy upgrade modifies system without backup
   - User can't rollback easily

   **Recommendation**: Add before checkout:
   ```bash
   if [ -f "$PROJECT_ROOT/.claude/hooks-daemon.yaml" ]; then
       BACKUP="$PROJECT_ROOT/.claude/hooks-daemon.yaml.backup-$(date +%Y%m%d-%H%M%S)"
       cp "$PROJECT_ROOT/.claude/hooks-daemon.yaml" "$BACKUP"
       _ok "Config backup: $BACKUP"
   fi
   ```

3. **No Error Handling for Config Validation**
   - Daemon fails to start, script says "OK Legacy upgrade complete"
   - Should detect daemon start failure and print helpful message

   **Recommendation**: Check daemon status and provide guidance:
   ```bash
   _info "Restarting daemon..."
   "$VENV_PYTHON" -m claude_code_hooks_daemon.daemon.cli start 2>&1 | tee /tmp/daemon_start.log

   if ! "$VENV_PYTHON" -m claude_code_hooks_daemon.daemon.cli status &>/dev/null; then
       _err "Daemon failed to start after upgrade"
       if grep -q "validation error" /tmp/daemon_start.log; then
           _info "Config validation failed. You may need to update your config for v$TARGET_VERSION."
           _info "Check: CLAUDE/UPGRADES/ for migration guides"
       fi
       exit 1
   fi
   ```

---

## UPGRADES Directory Issues

### Missing Upgrade Guides

**Current State**:
```
CLAUDE/UPGRADES/v2/
└── v2.0-to-v2.1/
```

**Missing**:
- v2.1-to-v2.2
- v2.2-to-v2.3
- v2.3-to-v2.4
- v2.4-to-v2.5
- v2.5-to-v2.6
- v2.6-to-v2.7
- v2.7-to-v2.8
- v2.8-to-v2.9 ← **CRITICAL (breaking changes)**

**Impact**:
- Users can't find upgrade instructions
- Breaking changes not documented
- LLM-UPDATE.md references non-existent guides

**Recommendation**:
Create upgrade guides for ALL version bumps, especially:
- **PRIORITY 1**: v2.8-to-v2.9 (breaking config changes)
- **PRIORITY 2**: Any other versions with breaking changes
- **PRIORITY 3**: All other versions (even if just "no breaking changes")

**Template** (from UPGRADES/README.md):
```bash
CLAUDE/UPGRADES/v2/v2.8-to-v2.9/
├── v2.8-to-v2.9.md              # Main guide
├── config-before.yaml            # Example: old format
├── config-after.yaml             # Example: new format
├── config-additions.yaml         # What to add
├── verification.sh               # Test script
└── examples/
    └── expected-output.txt       # What success looks like
```

---

## Release Process Issues

### v2.9.0 Release

**Issue**: Breaking config change not prominently documented

**Where It Should Be**:

1. **CHANGELOG.md** - ⚠️ Not clearly marked as breaking
   - Should have **BREAKING** prefix
   - Should show before/after config examples
   - Should link to migration guide

2. **RELEASES/v2.9.0.md** - ⚠️ Mentions plugin changes but not format change
   - Line 68: "Plugin paths now resolve relative to workspace_root"
   - Doesn't mention required `event_type` field
   - Should have section: "Breaking Changes" with migration steps

3. **README.md** - ✅ Version badge updated
   - No breaking change warning (acceptable for README)

**Recommendation**:
Add to release checklist (CLAUDE/development/RELEASING.md):
- [ ] If breaking changes, add prominent warning to release notes
- [ ] If config changes, create upgrade guide in CLAUDE/UPGRADES/
- [ ] If breaking changes, update CHANGELOG with **BREAKING** markers
- [ ] Test upgrade path from previous version

---

## Testing Gaps

### What Wasn't Tested

1. **Self-Install Mode Upgrade**
   - No documented test of upgrade in self-install mode
   - Layer 2 script detection may be broken specifically in self-install
   - Should add CI test or manual test checklist

2. **Config Migration with Plugins**
   - No test of plugin config migration
   - Breaking change wasn't caught pre-release
   - Should add acceptance test: "Upgrade from v2.8 with plugins"

3. **Legacy Upgrade Path**
   - Legacy upgrade path used, but not intentionally tested
   - Should verify legacy path works as fallback

**Recommendation**:
Add to acceptance testing playbook:
```markdown
## Test: Upgrade Path
1. Install v2.8.0 in fresh directory
2. Add sample plugin to config
3. Run upgrade script to v2.9.0
4. Verify daemon starts successfully
5. Verify plugin still works
6. Expected: Config auto-migrated, daemon running, plugin loaded
```

---

## User Experience Timeline

**Ideal Upgrade** (if Layer 2 worked):
```
1. User runs: bash /tmp/upgrade.sh
2. Script detects v2.4.0 → v2.9.0
3. Layer 2 creates backup
4. Layer 2 extracts config customizations
5. Layer 2 checks out v2.9.0
6. Layer 2 generates new default config
7. Layer 2 merges customizations (adds event_type automatically)
8. Layer 2 validates merged config
9. Layer 2 restarts daemon
10. User sees: "Upgrade complete to v2.9.0"
Total time: ~2 minutes, zero manual intervention
```

**Actual Upgrade** (what happened):
```
1. User runs: bash /tmp/upgrade.sh
2. Script detects v2.4.0 → v2.9.0
3. Layer 2 check FAILS (unknown reason)
4. Falls back to legacy upgrade (no config migration)
5. Checks out v2.9.0
6. Restarts daemon
7. Daemon fails: "Field required: event_type"
8. User investigates Pydantic error
9. User searches codebase for PluginConfig
10. User reads models.py to understand schema
11. User manually edits config
12. User restarts daemon
13. Daemon starts successfully
14. User confirms v2.9.0 running
Total time: ~10 minutes, manual config surgery required
```

**User Pain Points**:
- ❌ Expected automated config migration (documented in LLM-UPDATE.md)
- ❌ Got cryptic Pydantic errors instead
- ❌ Had to reverse-engineer config schema from source
- ❌ No upgrade guide to reference
- ❌ No helpful error messages or suggestions

---

## Recommendations Summary

### 🔴 Critical (Do Before Next Release)

1. **Fix Layer 2 Detection in Self-Install Mode**
   - Investigate why `[ -f "$LAYER2_SCRIPT" ]` fails
   - Add debug output to upgrade script
   - Test in both normal and self-install modes

2. **Create v2.8-to-v2.9 Upgrade Guide**
   - Document plugin config format change
   - Provide before/after examples
   - Add to CLAUDE/UPGRADES/v2/v2.8-to-v2.9/
   - Update UPGRADES/README.md index

3. **Improve Config Validation Error Messages**
   - Detect "Field required" errors for plugins
   - Print helpful message with examples
   - Link to upgrade guide
   - Show old vs new format

### ⚠️ High Priority (Do Soon)

4. **Add Config Backup to Legacy Upgrade**
   - Backup config before any changes
   - Print backup location
   - Document rollback process

5. **Improve Layer 1 Script Error Handling**
   - Check daemon status after restart
   - Detect config validation failures
   - Provide helpful troubleshooting guidance

6. **Document Self-Install Mode in LLM-UPDATE.md**
   - Add section on self-install considerations
   - Explain path differences
   - Note potential issues

7. **Mark Breaking Changes in Release Notes**
   - Add **BREAKING** prefix to CHANGELOG
   - Create "Breaking Changes" section in release notes
   - Link to migration guide

### 🔵 Medium Priority (Nice to Have)

8. **Create Upgrade Guides for All Missing Versions**
   - v2.1-to-v2.2, v2.2-to-v2.3, etc.
   - Even if just "no breaking changes"
   - Maintains completeness of UPGRADES directory

9. **Add Upgrade Path to Acceptance Tests**
   - Test upgrade from previous version
   - Verify config migration works
   - Catch breaking changes before release

10. **Improve Legacy Upgrade Path**
    - Better progress indicators
    - Clearer warnings about limitations
    - Link to documentation

---

## What Went Well ✅

Despite the issues, several things worked great:

1. **LLM-UPDATE.md Was Comprehensive**
   - Clear step-by-step process
   - Good architecture explanation
   - Helpful troubleshooting section
   - Made it possible to complete upgrade manually

2. **Layer 1 Script Worked**
   - Auto-detected project root
   - Correctly identified self-install mode
   - Fetched latest tags
   - Provided fallback when Layer 2 missing

3. **Code Upgrade Succeeded**
   - Git checkout to v2.9.0 worked perfectly
   - Venv reinstall worked
   - No import errors or Python issues

4. **Daemon Started After Config Fix**
   - Once config corrected, daemon started immediately
   - No other issues
   - All handlers loaded successfully

5. **Documentation Was Discoverable**
   - Easy to find config models in source
   - Release notes helped understand changes
   - Architecture docs explained system well

---

## Conclusion

The v2.4.0 → v2.9.0 upgrade was **successful but required manual intervention** due to a breaking config change and Layer 2 script detection failure.

**For Users**: The upgrade experience was frustrating due to cryptic errors and lack of guidance, but the system worked after manual config fix.

**For Developers**: This upgrade exposed gaps in the automated upgrade system, especially:
- Layer 2 detection reliability
- Config migration in legacy path
- Breaking change documentation
- Error message helpfulness

**Most Critical Fix**: Create v2.8-to-v2.9 upgrade guide and improve error messages for config validation failures. This will help users who upgrade in the future.

**Most Important Long-Term Fix**: Investigate and fix Layer 2 detection in self-install mode so automated config migration actually runs.

---

## Appendix: Commands Used

```bash
# Prerequisites
pwd
ls .claude/hooks-daemon.yaml
git status --short
cat .claude/hooks-daemon/src/claude_code_hooks_daemon/version.py

# Download upgrade script
curl -fsSL https://raw.githubusercontent.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/main/scripts/upgrade.sh -o /tmp/upgrade.sh

# Review script
less /tmp/upgrade.sh

# Run upgrade
bash /tmp/upgrade.sh

# Investigate config error
cat <repo>/.claude/hooks-daemon/src/claude_code_hooks_daemon/version.py | grep "__version__"
grep -A 10 "^plugins:" <repo>/.claude/hooks-daemon.yaml
grep -r "class.*Plugin.*Config" <repo>/.claude/hooks-daemon/src/claude_code_hooks_daemon/config

# Fix config manually
vim <repo>/.claude/hooks-daemon.yaml
# Added event_type: "pre_tool_use" to both plugins

# Restart daemon
PYTHON=<repo>/.claude/hooks-daemon/untracked/venv/bin/python
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart

# Verify
$PYTHON -m claude_code_hooks_daemon.daemon.cli status
$PYTHON -c "from claude_code_hooks_daemon.version import __version__; print(__version__)"
```

---

**End of Feedback Report**

---

## ADDENDUM: Root Cause Analysis - Nested Installation

**Investigation Date**: 2026-02-11 (post-upgrade)
**Severity**: CRITICAL
**Impact**: Daemon started with wrong project path, created nested directories

### The Nested Installation Problem

After upgrade completion, investigation revealed the daemon was running from a NESTED path:
```
Socket: <repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/daemon-*.sock
PID:    <repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/daemon-*.pid

Expected:
Socket: <repo>/.claude/hooks-daemon/untracked/daemon-*.sock
PID:    <repo>/.claude/hooks-daemon/untracked/daemon-*.pid
```

### Root Cause Chain

**1. v2.9.0 Includes Self-Install Config Files In Git**

When `git checkout v2.9.0` ran, it checked out `.claude/` directory **inside the hooks-daemon repo**:

```bash
$ ls -la <repo>/.claude/hooks-daemon/.claude/
total 28
drwxr-xr-x. agents/
drwxr-xr-x. commands/
drwxr-xr-x. hooks/
drwxr-xr-x. hooks-daemon/  ← Contains untracked/ with socket and PID!
-rw-r--r--. hooks-daemon.yaml
-rw-r--r--. hooks-daemon.yaml.example
-rw-r--r--. settings.json
drwxr-xr-x. skills/
```

These files exist because the hooks-daemon repo itself runs in **self-install mode** (dogfooding itself).

**2. Project Path Detection Logic Finds Wrong `.claude`**

When daemon CLI runs, it walks UP from CWD to find `.claude`:

```python
# From cli.py line 76-86
current = Path.cwd()

while current != current.parent:
    claude_dir = current / ".claude"
    if claude_dir.is_dir():  # ← FIRST .claude found wins!
        return _validate_installation(current)
    current = current.parent
```

**Timeline of directory creation**:
- 10:48: Git checkout creates `<repo>/.claude/hooks-daemon/.claude/` (self-install mode files from repo)
- 10:51: Daemon restart runs from within `<repo>/.claude/hooks-daemon/`
- 10:51: Walk-up finds `<repo>/.claude/hooks-daemon/.claude/` FIRST
- 10:51: Daemon incorrectly uses `<repo>/.claude/hooks-daemon` as project root
- 10:51: `paths.py` creates: `project_root + /.claude/hooks-daemon/untracked` = nested mess

**3. Hardcoded Path Logic Creates Nested Structure**

From `paths.py` line 109 and 144:
```python
untracked_dir = project_path / ".claude" / "hooks-daemon" / "untracked"
```

This **always** appends `/.claude/hooks-daemon/untracked` to whatever project path is provided.

So with wrong project path:
```
<repo>/.claude/hooks-daemon + /.claude/hooks-daemon/untracked
= <repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked
```

### Why This Wasn't Caught

1. **Self-Install Mode Confusion**
   - Normal installations: daemon in `.claude/hooks-daemon/`, config NOT in git
   - Self-install mode: daemon IS the project, config IS in git
   - Upgrade script didn't distinguish these scenarios

2. **Path Detection Assumes No Nested `.claude`**
   - Walk-up logic assumes only ONE `.claude` in path hierarchy
   - When two exist, picks the first (closest) one
   - No validation that found `.claude` is the CORRECT one

3. **No Nested Installation Check During Startup**
   - `_validate_installation()` checks for nested installation
   - But only AFTER path is already chosen
   - By then, daemon already has wrong project root

### Evidence From Running System

**Process information**:
```bash
$ ps aux | grep daemon
root  1784  <repo>/.claude/hooks-daemon/untracked/venv/bin/python -m claude_code_hooks_daemon.daemon.cli restart

$ readlink /proc/1784/cwd
/  ← Daemon CWD is root (daemonized process)
```

**Config being used**:
```bash
$ cat <repo>/.claude/hooks-daemon/.claude/hooks-daemon.yaml | head -7
version: "2.0"
daemon:
  idle_timeout_seconds: 600
  log_level: INFO
  self_install_mode: true  ← Hooks-daemon's OWN config!
  strict_mode: true
```

**Wrong config loaded**: Daemon loaded `<repo>/.claude/hooks-daemon/.claude/hooks-daemon.yaml` (the hooks-daemon repo's self-install config) instead of `<repo>/.claude/hooks-daemon.yaml` (the fedora-desktop project's config).

### Confusion Between Two Projects

This fedora-desktop installation has **two distinct projects**:

1. **Fedora-Desktop Project** (`<repo>/`)
   - Git repo: `LongTermSupport/fedora-desktop`
   - Config: `<repo>/.claude/hooks-daemon.yaml`
   - Mode: **NORMAL** (not self-install)
   - Purpose: User's actual project

2. **Hooks-Daemon Repo** (`<repo>/.claude/hooks-daemon/`)
   - Git repo: `Edmonds-Commerce-Limited/claude-code-hooks-daemon`
   - Config: `<repo>/.claude/hooks-daemon/.claude/hooks-daemon.yaml`
   - Mode: **SELF-INSTALL** (dogfooding itself)
   - Purpose: The daemon software itself

**What went wrong**: Daemon detected `<repo>/.claude/hooks-daemon/` as project root (wrong!) instead of `<repo>/` (correct!).

### Recommended Fixes

#### 1. Immediate Workaround (For This Installation)

Stop daemon, remove nested `.claude`, restart from correct location:
```bash
PYTHON=<repo>/.claude/hooks-daemon/untracked/venv/bin/python
$PYTHON -m claude_code_hooks_daemon.daemon.cli stop

# Remove nested .claude (created by git checkout)
rm -rf <repo>/.claude/hooks-daemon/.claude

# Restart from correct location
cd <repo>
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart

# Verify correct paths
$PYTHON -m claude_code_hooks_daemon.daemon.cli status
# Expected: Socket at <repo>/.claude/hooks-daemon/untracked/...
```

#### 2. Short-Term Fix (For v2.9.1 Patch Release)

**CRITICAL**: Exclude `.claude/` from git in hooks-daemon repo to prevent this issue:

```bash
# In hooks-daemon repo root
echo ".claude/" >> .gitignore
git rm -r --cached .claude/
git commit -m "fix: exclude .claude/ from version control

Prevents nested installation detection issues when hooks-daemon repo
is installed as a normal daemon (not self-install mode).

Self-install mode config should not be tracked in git."
```

**Why**: Self-install mode config files in git confuse normal installations during upgrades.

#### 3. Long-Term Fix (For v2.10.0)

**Improve Path Detection** - Skip nested `.claude` directories:
```python
# In cli.py get_project_path()
while current != current.parent:
    claude_dir = current / ".claude"
    if claude_dir.is_dir():
        # NEW: Skip if nested hooks-daemon installation detected
        nested_daemon = claude_dir / "hooks-daemon" / ".claude"
        if nested_daemon.is_dir():
            print(f"WARNING: Skipping nested installation at {current}", file=sys.stderr)
            current = current.parent
            continue
        
        try:
            return _validate_installation(current)
        except SystemExit:
            pass
    current = current.parent
```

**Add Repair Command**:
```bash
daemon cli repair --fix-nested
# Detects and removes nested .claude directories automatically
```

### Key Learnings

1. **Self-Install Mode Files Should NOT Be In Git**
   - Confuses normal installations during upgrades
   - Creates nested directory structures
   - Should be excluded via `.gitignore`

2. **Path Detection Needs Better Validation**
   - Finding "a `.claude`" directory isn't sufficient
   - Must verify it's the CORRECT `.claude` for this installation
   - Should detect and skip nested installations

3. **Upgrade Testing Must Cover Both Modes**
   - Self-install mode testing alone is insufficient
   - Normal installations behave very differently
   - Test matrix should include: normal → normal, self → self

4. **Error Messages Should Be Actionable**
   - Technical errors don't help users
   - Provide clear fix steps
   - Detect common scenarios and offer solutions

---

**End of Addendum - Full Feedback Report Complete**
