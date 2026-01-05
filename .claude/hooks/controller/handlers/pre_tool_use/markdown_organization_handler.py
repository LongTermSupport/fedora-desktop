"""MarkdownOrganizationHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class MarkdownOrganizationHandler(Handler):
    """Enforce markdown file organization rules.

    CRITICAL: This handler must match legacy hook behavior EXACTLY.
    Cannot use simple 'in' checks - must use precise pattern matching.
    """

    def __init__(self):
        super().__init__(name="enforce-markdown-organization", priority=35)

    def is_adhoc_instruction_file(self, file_path: str) -> bool:
        """Check if this is CLAUDE.md, README.md, SKILL.md, or agent file (allowed anywhere)."""
        import os
        filename = os.path.basename(file_path).lower()

        # CLAUDE.md and README.md allowed anywhere
        if filename in ['claude.md', 'readme.md']:
            return True

        # Normalize path for pattern matching - handle both absolute and relative paths
        # Strip leading slashes and any workspace directory prefix
        normalized = file_path.lstrip('/')
        workspace_patterns = ['workspace/', 'workspace\\']
        for pattern in workspace_patterns:
            if normalized.startswith(pattern):
                normalized = normalized[len(pattern):]

        # SKILL.md files in .claude/skills/*/ are allowed
        if filename == 'skill.md' and '.claude/skills/' in normalized:
            return True

        # Agent definitions in .claude/agents/ are allowed
        if '.claude/agents/' in normalized and file_path.endswith('.md'):
            return True

        return False

    def is_page_colocated_file(self, file_path: str) -> bool:
        """Check if this is a *-research.md or *-rules.md file co-located with pages."""
        import re

        # Normalize path - handle both absolute and relative paths
        # Strip leading slashes and any workspace directory prefix
        normalized = file_path.lstrip('/')

        # Remove common workspace directory prefix patterns
        workspace_patterns = ['workspace/', 'workspace\\']
        for pattern in workspace_patterns:
            if normalized.startswith(pattern):
                normalized = normalized[len(pattern):]

        # Check for page research files: src/pages/**/*-research.md
        if re.match(r'^src/pages/.*-research\.md$', normalized, re.IGNORECASE):
            return True

        # Check for page rules files: src/pages/**/*-rules.md
        if re.match(r'^src/pages/.*-rules\.md$', normalized, re.IGNORECASE):
            return True

        return False

    def matches(self, hook_input: dict) -> bool:
        """Check if writing markdown to wrong location.

        IMPORTANT: Must match legacy hook behavior exactly using precise patterns.
        """
        import re

        tool_name = hook_input.get("tool_name")
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path or not file_path.endswith('.md'):
            return False

        # Normalize path - handle both absolute and relative paths
        normalized = file_path.lstrip('/')
        workspace_patterns = ['workspace/', 'workspace\\']
        for pattern in workspace_patterns:
            if normalized.startswith(pattern):
                normalized = normalized[len(pattern):]

        # CRITICAL: CLAUDE.md and README.md are allowed ANYWHERE
        if self.is_adhoc_instruction_file(file_path):
            return False

        # Page co-located files (*-research.md, *-rules.md) are allowed
        if self.is_page_colocated_file(file_path):
            return False

        # Check allowed locations with PRECISE pattern matching (not simple 'in' checks)

        # 1. CLAUDE/Plan/NNN-*/ - Requires numbered subdirectory
        if re.match(r'^CLAUDE/Plan/\d{3}-[^/]+/.+\.md$', normalized, re.IGNORECASE):
            return False  # Allow

        # 2. CLAUDE/ root level ONLY (no subdirs except known ones)
        if normalized.lower().startswith('claude/') and '/' not in normalized[7:]:
            return False  # Allow

        # 3. CLAUDE/research/ - Structured research data
        if re.match(r'^CLAUDE/research/', normalized, re.IGNORECASE):
            return False  # Allow

        # 4. CLAUDE/Sitemap/ - Site architecture
        if re.match(r'^CLAUDE/Sitemap/', normalized, re.IGNORECASE):
            return False  # Allow

        # 5. docs/ - Human-facing documentation
        if normalized.lower().startswith('docs/'):
            return False  # Allow

        # 6. untracked/ - Temporary docs
        if normalized.lower().startswith('untracked/'):
            return False  # Allow

        # 7. eslint-rules/ - ESLint rule docs
        if re.match(r'^eslint-rules/.*\.md$', normalized, re.IGNORECASE):
            return False  # Allow

        # Not in allowed location - block
        return True

    def handle(self, hook_input: dict) -> HookResult:
        """Block markdown in wrong location."""
        file_path = get_file_path(hook_input)

        return HookResult(
            decision="deny",
            reason=(
                "MARKDOWN FILE IN WRONG LOCATION\n\n"
                "Markdown files must follow project organization rules.\n\n"
                f"Attempted to write: {file_path}\n\n"
                "This location is NOT allowed. Markdown files can only be written to:\n\n"
                "1. ./CLAUDE/Plan/XXX-plan-name/ - Docs for current plan\n"
                "2. ./CLAUDE/ (root only) - Generic LLM docs\n"
                "3. ./CLAUDE/research/ - Structured research data\n"
                "4. ./docs/ - Human-facing documentation\n"
                "5. ./eslint-rules/ - ESLint rule documentation\n"
                "6. ./untracked/ - Ad-hoc temporary docs\n\n"
                "CHOOSE THE RIGHT LOCATION:\n"
                "- Is this for the current plan? -> CLAUDE/Plan/{plan-number}-*/\n"
                "- Is this temporary/ad-hoc? -> untracked/\n"
                "- Is this for humans? -> docs/\n"
                "- Is this generic LLM context? -> CLAUDE/ (very rare!)"
            )
        )


