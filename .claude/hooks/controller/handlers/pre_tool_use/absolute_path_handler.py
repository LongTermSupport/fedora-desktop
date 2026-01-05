"""AbsolutePathHandler - prevents /workspace/ absolute paths in code content."""

import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_file_content


class AbsolutePathHandler(Handler):
    """Block Write/Edit operations with /workspace/ absolute paths in file CONTENT.

    Container-specific absolute paths (/workspace/) should not appear in code.
    Tool parameters (like file_path) CAN use absolute paths - that's fine.
    We only block when /workspace/ appears INSIDE the code content being written.
    """

    def __init__(self):
        super().__init__(name="prevent-absolute-workspace-paths", priority=12)

    def matches(self, hook_input: dict) -> bool:
        """Check if file CONTENT contains /workspace/ absolute paths."""
        tool_name = hook_input.get("tool_name")

        # Only check Write and Edit tools
        if tool_name not in ["Write", "Edit"]:
            return False

        tool_input = hook_input.get("tool_input", {})

        # For Write: check content parameter
        if tool_name == "Write":
            content = tool_input.get("content", "")
            if content and "/workspace/" in content:
                return True

        # For Edit: check new_string and old_string parameters
        if tool_name == "Edit":
            new_string = tool_input.get("new_string", "")
            old_string = tool_input.get("old_string", "")

            # Check both new_string (what we're writing) and old_string (what exists)
            if "/workspace/" in new_string or "/workspace/" in old_string:
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block the operation with clear explanation showing the problematic content."""
        tool_name = hook_input.get("tool_name")
        tool_input = hook_input.get("tool_input", {})

        # Extract the problematic content snippet
        if tool_name == "Write":
            content = tool_input.get("content", "")
            snippet = content[:200] if content else ""
        else:  # Edit
            new_string = tool_input.get("new_string", "")
            snippet = new_string[:200] if new_string else ""

        return HookResult(
            decision="deny",
            reason=(
                f"BLOCKED: File content contains /workspace/ absolute path\n\n"
                f"Content snippet:\n{snippet}...\n\n"
                "Don't hardcode /workspace/ paths in your code.\n"
                "Use relative paths from the repository root instead.\n\n"
                "Why this matters:\n"
                "  - /workspace/ only exists in specific container environments\n"
                "  - Hardcoded absolute paths break portability\n"
                "  - Relative paths work everywhere (local, CI, production)\n\n"
                "Note: Tool parameters like file_path CAN be absolute - that's fine.\n"
                "This hook only blocks /workspace/ paths INSIDE your code content."
            )
        )
