# Ansible Lint Improvement Plan

**Date**: 2025-12-03
**Status**: Planning Phase
**Project**: fedora-desktop ansible automation

## Overview

This plan outlines a systematic approach to improve ansible-lint compliance across the fedora-desktop project while maintaining Infrastructure as Code principles. All changes will be performed through automated tooling and committed systematically.

## Objectives

1. **Create reusable lint tooling** - Build scripts for consistent linting across the project
2. **Automate quality checks** - Integrate linting into Claude Code workflow via hooks
3. **Systematically fix violations** - Address lint issues one rule at a time with individual commits
4. **Adopt FQCN best practice** - Use Fully Qualified Collection Names for all Ansible modules
5. **Maintain quality going forward** - Prevent regression through automated checks

## Project Scope

- **Total playbooks**: 37 YAML files
- **Current lint config**: `.ansible-lint` with cosmetic rules disabled
- **Major issues**: FQCN violations, command-instead-of-module warnings
- **Git branch**: main (F42)

## Phase 1: Tooling Infrastructure

### 1.1 Build `scripts/lint` Script

**Purpose**: Provide consistent linting interface with rich output

**Features**:
- Accept path argument (file, directory, or whole project)
- Generate detailed JSON output to `./untracked/lint/{timestamp}-{sanitized-path}.json`
- Auto-prune old JSON files (keep last 10)
- Produce terse summary output:
  - Group issues by file
  - If >10 files affected, show only totals
  - Include link to JSON file
  - Provide example `jq` queries for analysis

**Implementation**:
```bash
#!/usr/bin/env bash
# scripts/lint [path]
# - If no path: lint entire playbooks/ directory
# - Output: JSON to untracked/lint/ + terse summary to stdout
# - Auto-prune: keep only 10 most recent JSON files
```

**Output Example**:
```
Ansible Lint Summary
====================
Total: 42 violations across 15 files

Top Issues:
  fqcn[action-core]: 30 violations
  command-instead-of-module: 8 violations
  no-changed-when: 4 violations

Detailed results: ./untracked/lint/2025-12-03T17-30-45-playbooks.json

Example queries:
  # List files with most issues
  jq '[.[] | .location.path] | group_by(.) | map({file: .[0], count: length}) | sort_by(.count) | reverse' ./untracked/lint/2025-12-03T17-30-45-playbooks.json

  # Get all FQCN violations
  jq '[.[] | select(.check_name | startswith("fqcn"))]' ./untracked/lint/2025-12-03T17-30-45-playbooks.json
```

**Exit codes**:
- 0: No violations
- 1: Violations found
- 2: Lint execution error

### 1.2 Create Claude Code Hook

**Purpose**: Auto-lint Ansible files when edited, provide immediate feedback

**Hook file**: `.claude/hooks/on-edit.sh` (or appropriate hook type)

**Behavior**:
- Trigger: Any `.yml` file in `playbooks/` is edited
- Action: Run `scripts/lint {edited-file}`
- Output to agent:
  - If clean: "✓ No ansible-lint issues found in {file}"
  - If issues: Full summary + instruction to fix before proceeding

**Implementation approach**:
- Use Claude Code documentation to determine correct hook type
- Research hook API and file patterns
- Use Task agent with subagent_type=Explore to read Claude Code docs

**Questions to resolve**:
- What hook types does Claude Code support?
- How to scope to only Ansible YAML files?
- How to pass edited file path to hook script?

### 1.3 Update `.ansible-lint` Configuration

**Changes**:
- Remove `fqcn` from `skip_list` to enable FQCN checks
- Keep cosmetic rules disabled (key-order, yaml formatting)
- Document why each rule is skipped

**Timing**: After Phase 1 tooling complete, before Phase 2 fixes

## Phase 2: Systematic Issue Resolution

### 2.1 Pre-fix Preparation

**Before each rule fix**:
1. Ensure clean git working tree: `git status`
2. Create baseline: `scripts/lint playbooks/ > /tmp/baseline.txt`
3. Identify scope: Count violations for specific rule
4. Create focused commit message template

### 2.2 Fix FQCN Violations

**Target**: All `fqcn[action-core]` and `fqcn[action]` violations

**Scope estimate**: ~30-50 violations across 37 playbooks

**Approach**:
1. Run: `scripts/lint playbooks/` and filter for FQCN issues
2. For each playbook with FQCN violations:
   - Read file
   - Replace short names with FQCN:
     - `package` → `ansible.builtin.package`
     - `command` → `ansible.builtin.command`
     - `copy` → `ansible.builtin.copy`
     - `file` → `ansible.builtin.file`
     - `service` → `ansible.builtin.service`
     - `systemd` → `ansible.builtin.systemd`
     - etc.
   - Verify syntax: `ansible-playbook --syntax-check`
3. Re-run lint to confirm rule fully fixed
4. Single commit: "refactor: adopt FQCN for all Ansible builtin modules"

**Commit message**:
```
refactor: adopt FQCN for all Ansible builtin modules

Replace short module names with fully qualified collection names
across all playbooks to follow Ansible best practices and prevent
future namespace conflicts.

Changes:
- package → ansible.builtin.package
- command → ansible.builtin.command
- copy → ansible.builtin.copy
- [etc.]

Fixes all fqcn[action-core] ansible-lint violations.
```

### 2.3 Fix Other High-Priority Issues

**Target rules** (in order):
1. `no-changed-when` - Add `changed_when: false` to check commands
2. `risky-file-permissions` - Set explicit file permissions
3. `var-naming` - Fix variable naming conventions (if any)

**For each rule**:
1. Clean repo check
2. Identify all violations: `scripts/lint | jq 'select(.check_name == "rule-name")'`
3. Fix all instances of that rule
4. Verify fix: `scripts/lint` shows 0 violations for that rule
5. Commit with message: "fix(lint): resolve {rule-name} violations"
6. Move to next rule

### 2.4 Address Warning-Level Issues

**Target**: `command-instead-of-module` warnings

**Approach**:
- Review each instance individually
- Determine if module alternative exists and is appropriate
- If raw command is necessary: Add explanatory comment + keep as-is
- If module alternative better: Refactor to use module

**Commit separately** if changes made

## Phase 3: Validation & Documentation

### 3.1 Final Validation

```bash
# Run full project lint
scripts/lint playbooks/

# Verify exit code 0 (no violations)
echo $?

# Check git status - should show only intended commits
git log --oneline -10
```

### 3.2 Update Documentation

**Files to update**:
- `CLAUDE.md` - Add section on lint script usage
- `docs/containerization.md` - May need updates if relevant
- `README.md` - If it exists and references dev workflow

**New section for CLAUDE.md**:
```markdown
## Code Quality - Ansible Lint

### Running Lint Checks

Use the provided lint script for consistent checking:

\`\`\`bash
# Lint entire project
./scripts/lint

# Lint specific directory
./scripts/lint playbooks/imports/optional/

# Lint specific file
./scripts/lint playbooks/imports/play-basic-configs.yml
\`\`\`

Results are saved to `./untracked/lint/` with example jq queries provided.

### Claude Code Integration

Ansible playbooks are automatically linted when edited. Fix any reported
issues before committing changes.

### Lint Configuration

See `.ansible-lint` for rule configuration. FQCN (Fully Qualified Collection
Names) are enforced for all Ansible builtin modules.
```

## Abort Conditions & Risk Mitigation

### ABORT if any of these occur:

1. **Syntax errors after fixes** - If ansible-playbook --syntax-check fails
2. **Git conflicts** - If repo is not clean before starting a fix phase
3. **>100 violations for single rule** - Scope too large, need different approach
4. **Breaking changes required** - If fixes require functional changes to playbooks
5. **Hook system incompatibility** - If Claude Code doesn't support needed hooks

### Risk: Playbook Functionality

**Concern**: Syntax changes might alter playbook behavior

**Mitigation**:
- FQCN changes are syntax-only, functionally equivalent
- Run syntax checks after each file edit
- Test critical playbooks in isolated environment before commit

**Suggested test approach**:
```bash
# Run main playbook in check mode
ansible-playbook playbooks/playbook-main.yml --check

# Test specific changed playbooks
ansible-playbook playbooks/imports/play-basic-configs.yml --check
```

### Risk: Time/Scope Creep

**Concern**: 37 playbooks × multiple violations = large effort

**Mitigation**:
- Fix one rule at a time across all files
- Use automation (sed, awk, Edit tool) for repetitive changes
- Commit frequently to avoid losing progress
- Prioritize FQCN, defer low-priority issues if needed

### Alternative Strategies (if needed)

**If systematic fix becomes impractical**:

1. **Gradual adoption** - Fix files only when touched for other reasons
2. **Ignore files** - Add problematic files to `.ansible-lint-ignore`
3. **Split effort** - Fix core playbooks, leave optional playbooks
4. **Relax rules** - Move more rules to warn_list instead of skip_list

## Success Criteria

- [ ] `scripts/lint` tool created and functional
- [ ] Claude Code hook installed and triggering correctly
- [ ] `.ansible-lint` updated to enable FQCN checks
- [ ] All FQCN violations resolved (single commit)
- [ ] All high-priority issues resolved (individual commits per rule)
- [ ] Final `scripts/lint playbooks/` shows 0 violations
- [ ] Documentation updated with lint workflow
- [ ] All commits follow conventional commit format
- [ ] No syntax errors in any playbook
- [ ] Clean git history with descriptive commit messages

## Timeline Estimate

- Phase 1 (Tooling): 30-60 minutes
- Phase 2.2 (FQCN fixes): 60-90 minutes (37 files, mostly mechanical)
- Phase 2.3 (Other fixes): 30-60 minutes (depends on violation count)
- Phase 3 (Validation/docs): 15-30 minutes

**Total**: 2.5-4 hours

## Execution Approach

1. Commit this plan document first
2. Execute Phase 1 (tooling) completely
3. Test tooling thoroughly
4. Execute Phase 2 systematically (one rule at a time)
5. Validate and document

## Notes

- This is a PUBLIC repository - all commits will be visible
- Follow existing project patterns for script placement
- Maintain Infrastructure as Code principles - no manual changes
- Use ansible-lint --fix flag cautiously (may make unwanted changes)
- Keep commits atomic and well-described

---

**Plan Version**: 1.0
**Last Updated**: 2025-12-03
**Plan Status**: Ready for execution
