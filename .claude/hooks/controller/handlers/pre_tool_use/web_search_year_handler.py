"""WebSearchYearHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class WebSearchYearHandler(Handler):
    """Validate WebSearch queries don't use outdated years."""

    CURRENT_YEAR = 2025

    def __init__(self):
        super().__init__(name="validate-websearch-year", priority=55)

    def matches(self, hook_input: dict) -> bool:
        """Check if WebSearch query uses old year."""
        tool_name = hook_input.get("tool_name")
        if tool_name != "WebSearch":
            return False

        query = hook_input.get("tool_input", {}).get("query", "")
        if not query:
            return False

        # Check for years 2020-2024 in query
        for year in range(2020, self.CURRENT_YEAR):
            if str(year) in query:
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block outdated year in WebSearch."""
        query = hook_input.get("tool_input", {}).get("query", "")

        return HookResult(
            decision="deny",
            reason=(
                f"🚫 BLOCKED: WebSearch query contains outdated year\n\n"
                f"Query: {query}\n\n"
                f"Current year is {self.CURRENT_YEAR}. Don't search for old years.\n\n"
                "✅ CORRECT APPROACH:\n"
                f"  - Use {self.CURRENT_YEAR} for current information\n"
                "  - Remove year if searching general topics\n"
                "  - Only use old years if specifically researching history"
            )
        )


