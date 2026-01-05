"""PlanTimeEstimatesHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class PlanTimeEstimatesHandler(Handler):
    """Block time estimates in plan documents."""

    ESTIMATE_PATTERNS = [
        r'\*\*Estimated\s+Effort\*\*:\s*[^\n]*(?:hours?|minutes?|days?|weeks?)',
        r'Estimated\s+Effort:\s*[^\n]*(?:hours?|minutes?|days?|weeks?)',
        r'(?:Time\s+)?[Ee]stimated\s+(?:time)?:\s*[^\n]*(?:hours?|minutes?|days?|weeks?)',
        r'\*\*Total\s+Estimated\s+Time\*\*:\s*[^\n]*(?:hours?|minutes?|days?|weeks?)',
        r'Total\s+Estimated\s+Time:\s*[^\n]*(?:hours?|minutes?|days?|weeks?)',
        r'\*\*Target\s+Completion\*\*:\s*\d{4}-\d{2}-\d{2}',
        r'Target\s+Completion:\s*\d{4}-\d{2}-\d{2}',
        r'\*\*Completion\*\*:\s*\d{4}-\d{2}-\d{2}',
        r'Completion:\s*\d{4}-\d{2}-\d{2}',
        r'\b\d+\s*(hour|hr|minute|min|day|week|month)s?\b',
        r'\b(ETA|timeline|deadline|due date):\s*\d',
    ]

    def __init__(self):
        super().__init__(name="block-plan-time-estimates", priority=40)

    def matches(self, hook_input: dict) -> bool:
        """Check if writing time estimates to plan files."""
        tool_name = hook_input.get("tool_name")
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path or '/Plan/' not in file_path or not file_path.endswith('.md'):
            return False

        content = get_file_content(hook_input)
        if tool_name == "Edit":
            content = hook_input.get("tool_input", {}).get("new_string", "")

        if not content:
            return False

        # Check for time estimate patterns
        for pattern in self.ESTIMATE_PATTERNS:
            if re.search(pattern, content, re.IGNORECASE):
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block time estimates."""
        file_path = get_file_path(hook_input)

        return HookResult(
            decision="deny",
            reason=(
                "🚫 BLOCKED: Time estimates not allowed in plan documents\n\n"
                f"File: {file_path}\n\n"
                "Plans should focus on WHAT needs to be done, not WHEN.\n\n"
                "WHY: Time estimates in plans create false expectations and pressure.\n\n"
                "✅ CORRECT APPROACH:\n"
                "  - Break work into concrete tasks\n"
                "  - Describe implementation steps\n"
                "  - Let user decide scheduling\n"
                "  - Focus on actionable work, not timelines"
            )
        )


