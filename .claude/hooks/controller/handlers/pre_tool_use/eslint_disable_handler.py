"""EslintDisableHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class EslintDisableHandler(Handler):
    """Block ESLint disable comments in code."""

    FORBIDDEN_PATTERNS = [
        r'eslint-disable',
        r'@ts-ignore',
        r'@ts-nocheck',
        r'@ts-expect-error',
    ]

    CHECK_EXTENSIONS = ['.ts', '.tsx', '.js', '.jsx']

    def __init__(self):
        super().__init__(name="enforce-no-eslint-disable", priority=30)

    def matches(self, hook_input: dict) -> bool:
        """Check if writing ESLint disable comments."""
        tool_name = hook_input.get("tool_name")
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Case-insensitive extension check
        file_path_lower = file_path.lower()
        if not any(file_path_lower.endswith(ext) for ext in self.CHECK_EXTENSIONS):
            return False

        # Skip node_modules, dist, build artifacts
        if any(skip in file_path for skip in ['node_modules', 'dist', '.build', 'coverage']):
            return False

        content = get_file_content(hook_input)
        if tool_name == "Edit":
            content = hook_input.get("tool_input", {}).get("new_string", "")

        if not content:
            return False

        # Check for forbidden patterns
        for pattern in self.FORBIDDEN_PATTERNS:
            if re.search(pattern, content, re.IGNORECASE):
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block ESLint suppressions."""
        file_path = get_file_path(hook_input)
        content = get_file_content(hook_input)
        if hook_input.get("tool_name") == "Edit":
            content = hook_input.get("tool_input", {}).get("new_string", "")

        # Find which pattern matched
        issues = []
        for pattern in self.FORBIDDEN_PATTERNS:
            for match in re.finditer(pattern, content, re.IGNORECASE):
                issues.append(match.group(0))

        return HookResult(
            decision="deny",
            reason=(
                "🚫 BLOCKED: ESLint suppression comments are not allowed\n\n"
                f"File: {file_path}\n\n"
                f"Found {len(issues)} suppression comment(s):\n"
                + "\n".join(f"  - {issue}" for issue in issues[:5]) + "\n\n"
                "WHY: Suppression comments hide real problems and create technical debt.\n\n"
                "✅ CORRECT APPROACH:\n"
                "  1. Fix the underlying issue (don't suppress)\n"
                "  2. Refactor code to meet ESLint rules\n"
                "  3. If rule is genuinely wrong, update .eslintrc.json project-wide\n\n"
                "ESLint rules exist for good reason. Fix the code, don't silence the tool."
            )
        )


