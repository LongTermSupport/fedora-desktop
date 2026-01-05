"""BritishEnglishHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class BritishEnglishHandler(Handler):
    """Warn about American English spellings in content files (non-blocking)."""

    # Common American -> British spelling patterns
    SPELLING_CHECKS = {
        r'\bcolor\b': 'colour',
        r'\bfavor\b': 'favour',
        r'\bbehavior\b': 'behaviour',
        r'\borganize\b': 'organise',
        r'\brecognize\b': 'recognise',
        r'\banalyze\b': 'analyse',
        r'\bcenter\b': 'centre',
        r'\bmeter\b': 'metre',
        r'\bliter\b': 'litre',
    }

    CHECK_EXTENSIONS = ['.md', '.ejs', '.html', '.txt']
    CHECK_DIRECTORIES = ['private_html', 'docs', 'CLAUDE']

    def __init__(self):
        super().__init__(name="enforce-british-english", priority=60)

    def matches(self, hook_input: dict) -> bool:
        """Check if writing content files with potential American spellings."""
        tool_name = hook_input.get("tool_name")
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Only check content files
        if not any(file_path.endswith(ext) for ext in self.CHECK_EXTENSIONS):
            return False

        # Only check certain directories
        if not any(dir in file_path for dir in self.CHECK_DIRECTORIES):
            return False

        content = get_file_content(hook_input)
        if tool_name == "Edit":
            content = hook_input.get("tool_input", {}).get("new_string", "")

        if not content:
            return False

        # Check for American spellings (skip code blocks in markdown)
        issues = self._check_british_english(content)
        return len(issues) > 0

    def handle(self, hook_input: dict) -> HookResult:
        """Warn about American spellings but allow operation."""
        file_path = get_file_path(hook_input)
        content = get_file_content(hook_input)
        tool_name = hook_input.get("tool_name")

        if tool_name == "Edit":
            content = hook_input.get("tool_input", {}).get("new_string", "")

        issues = self._check_british_english(content)

        # WARNING: We allow the operation but print a warning
        warning_parts = [
            f"⚠️  American English detected in {file_path}:\n"
        ]

        for issue in issues[:5]:  # Show max 5 issues
            warning_parts.append(
                f"  Line {issue['line']}: '{issue['american']}' → use '{issue['british']}'\n"
                f"    {issue['text']}\n"
            )

        if len(issues) > 5:
            warning_parts.append(f"  ... and {len(issues) - 5} more issue(s)\n")

        warning_parts.append(
            "\n✅ CORRECT SPELLING: Please use British English.\n"
            "If this is intentional (e.g., in a quote), you can ignore this warning.\n"
        )

        # Return allow with warning message
        return HookResult(
            decision="allow",
            reason="".join(warning_parts)
        )

    def _check_british_english(self, content: str) -> list:
        """Check content for American spellings, skipping code blocks."""
        issues = []
        lines = content.split('\n')
        in_code_block = False

        for line_num, line in enumerate(lines, 1):
            # Toggle code block state
            if line.strip().startswith('```'):
                in_code_block = not in_code_block
                continue

            # Skip lines in code blocks
            if in_code_block:
                continue

            # Check for American spellings
            for american_pattern, british in self.SPELLING_CHECKS.items():
                match = re.search(american_pattern, line, re.IGNORECASE)
                if match:
                    issues.append({
                        'line': line_num,
                        'american': match.group(),
                        'british': british,
                        'text': line.strip()[:80]  # Truncate long lines
                    })

        return issues

