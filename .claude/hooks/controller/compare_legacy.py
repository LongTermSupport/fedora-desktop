#!/usr/bin/env python3
"""
Compare legacy hooks vs new handlers to catch regressions.

This script systematically checks that ALL logic from legacy hooks
has been migrated to the new front controller handlers.
"""

import re
import sys
from pathlib import Path

# Map legacy hooks to new handlers
LEGACY_TO_HANDLER_MAP = {
    'prevent-destructive-git.py': 'bash_handlers.py:PreventDestructiveGitHandler',
    'prevent-worktree-file-copying.py': 'bash_handlers.py:PreventWorktreeFileCopyingHandler',
    'discourage-git-stash.py': 'bash_handlers.py:DiscourageGitStashHandler',
    'enforce-llm-npm-commands.py': 'bash_handlers.py:EnforceLlmNpmCommandsHandler',
    'validate-plan-number.py': 'bash_handlers.py:ValidatePlanNumberHandler',
    'validate-websearch-year.py': 'file_handlers.py:WebSearchYearHandler',
    'enforce-no-eslint-disable.py': 'file_handlers.py:EslintDisableHandler',
    'block-plan-time-estimates.py': 'file_handlers.py:PlanTimeEstimatesHandler',
    'enforce-markdown-organization.py': 'file_handlers.py:MarkdownOrganizationHandler',
    'validate-claude-readme-content.py': 'file_handlers.py:ClaudeReadmeHandler',
    'enforce-official-plan-command.py': 'file_handlers.py:OfficialPlanCommandHandler',
    # PostToolUse hooks
    'validate-eslint-on-write.py': 'Not migrated - PostToolUse',
    'validate-sitemap-on-edit.py': 'Not migrated - PostToolUse',
    'remind-validate-after-builder.py': 'Not migrated - SubagentStop',
    # UserPromptSubmit hooks
    'auto-continue.py': 'Not migrated - UserPromptSubmit',
    'enforce-british-english.py': 'Not migrated - UserPromptSubmit',
}

def extract_patterns_from_legacy(file_path: Path) -> dict:
    """Extract key patterns from legacy hook."""
    content = file_path.read_text()

    patterns = {
        'forbidden_commands': [],
        'allowed_locations': [],
        'regex_patterns': [],
        'special_cases': [],
    }

    # Find FORBIDDEN_COMMANDS lists
    forbidden_match = re.search(r'FORBIDDEN_COMMANDS\s*=\s*\[(.*?)\]', content, re.DOTALL)
    if forbidden_match:
        commands = re.findall(r"['\"]([^'\"]+)['\"]", forbidden_match.group(1))
        patterns['forbidden_commands'] = commands

    # Find ALLOWED locations
    allowed_match = re.search(r'ALLOWED_[A-Z_]*\s*=\s*\[(.*?)\]', content, re.DOTALL)
    if allowed_match:
        locations = re.findall(r"['\"]([^'\"]+)['\"]", allowed_match.group(1))
        patterns['allowed_locations'] = locations

    # Find regex patterns (re.match, re.search)
    regex_matches = re.finditer(r're\.(match|search)\(r[\'"]([^\'"]+)[\'"]', content)
    for match in regex_matches:
        patterns['regex_patterns'].append(match.group(2))

    # Find special case comments
    special_cases = re.findall(r'#\s*\d+\.?\d*\.?\s*([A-Z/].+(?:allowed|blocked|Co-located).*)', content)
    patterns['special_cases'] = special_cases

    return patterns

def extract_patterns_from_handler(file_path: Path, handler_name: str) -> dict:
    """Extract key patterns from new handler."""
    content = file_path.read_text()

    # Find handler class
    class_match = re.search(
        rf'class {handler_name}\(.*?\):(.*?)(?=class\s+\w+|$)',
        content,
        re.DOTALL
    )

    if not class_match:
        return {}

    handler_content = class_match.group(1)

    patterns = {
        'forbidden_commands': [],
        'allowed_locations': [],
        'regex_patterns': [],
        'special_cases': [],
    }

    # Find FORBIDDEN patterns
    forbidden_match = re.search(r'FORBIDDEN_[A-Z_]*\s*=\s*\[(.*?)\]', handler_content, re.DOTALL)
    if forbidden_match:
        commands = re.findall(r"[r']([^'\"]+)['\"]", forbidden_match.group(1))
        patterns['forbidden_commands'] = commands

    # Find ALLOWED locations
    allowed_match = re.search(r'ALLOWED_[A-Z_]*\s*=\s*\[(.*?)\]', handler_content, re.DOTALL)
    if allowed_match:
        locations = re.findall(r"['\"]([^'\"]+)['\"]", allowed_match.group(1))
        patterns['allowed_locations'] = locations

    # Find regex patterns
    regex_matches = re.finditer(r're\.(match|search)\(r?[\'"]([^\'"]+)[\'"]', handler_content)
    for match in regex_matches:
        patterns['regex_patterns'].append(match.group(2))

    # Find method names (special case handlers)
    method_matches = re.finditer(r'def\s+(is_\w+)\(', handler_content)
    patterns['special_cases'] = [m.group(1) for m in method_matches]

    return patterns

def compare_patterns(legacy_name: str, legacy_patterns: dict, handler_patterns: dict) -> list:
    """Compare patterns and return list of differences."""
    differences = []

    # Check forbidden commands
    legacy_cmds = set(legacy_patterns.get('forbidden_commands', []))
    handler_cmds = set(handler_patterns.get('forbidden_commands', []))

    missing_cmds = legacy_cmds - handler_cmds
    if missing_cmds:
        differences.append(f"  Missing forbidden commands: {missing_cmds}")

    # Check allowed locations
    legacy_locs = set(legacy_patterns.get('allowed_locations', []))
    handler_locs = set(handler_patterns.get('allowed_locations', []))

    missing_locs = legacy_locs - handler_locs
    if missing_locs:
        differences.append(f"  Missing allowed locations: {missing_locs}")

    # Check regex patterns (more lenient - just count)
    legacy_regex_count = len(legacy_patterns.get('regex_patterns', []))
    handler_regex_count = len(handler_patterns.get('regex_patterns', []))

    if handler_regex_count < legacy_regex_count:
        differences.append(
            f"  Fewer regex patterns: legacy={legacy_regex_count}, handler={handler_regex_count}"
        )

    # Check special cases count
    legacy_special = len(legacy_patterns.get('special_cases', []))
    handler_special = len(handler_patterns.get('special_cases', []))

    if handler_special < legacy_special:
        differences.append(
            f"  Fewer special cases: legacy={legacy_special}, handler={handler_special}"
        )

    return differences

def main():
    hooks_dir = Path('/workspace/.claude/hooks')
    controller_dir = hooks_dir / 'controller' / 'handlers'

    print("=" * 80)
    print("LEGACY HOOKS vs NEW HANDLERS - REGRESSION CHECK")
    print("=" * 80)
    print()

    regressions_found = False

    for legacy_file, handler_info in LEGACY_TO_HANDLER_MAP.items():
        legacy_path = hooks_dir / legacy_file

        if not legacy_path.exists():
            continue

        print(f"📋 {legacy_file}")

        # Skip non-PreToolUse hooks for now
        if 'Not migrated' in handler_info:
            print(f"   ⏭️  {handler_info}")
            print()
            continue

        # Extract handler file and class name
        handler_file, handler_class = handler_info.split(':')
        handler_path = controller_dir / handler_file

        if not handler_path.exists():
            print(f"   ❌ Handler file not found: {handler_path}")
            regressions_found = True
            print()
            continue

        # Compare patterns
        legacy_patterns = extract_patterns_from_legacy(legacy_path)
        handler_patterns = extract_patterns_from_handler(handler_path, handler_class)

        differences = compare_patterns(legacy_file, legacy_patterns, handler_patterns)

        if differences:
            print(f"   ⚠️  POTENTIAL REGRESSIONS:")
            for diff in differences:
                print(diff)
            regressions_found = True
        else:
            print(f"   ✅ Patterns match")

        print()

    print("=" * 80)

    if regressions_found:
        print("❌ REGRESSIONS FOUND - Review differences above")
        return 1
    else:
        print("✅ NO REGRESSIONS DETECTED")
        return 0

if __name__ == '__main__':
    sys.exit(main())
