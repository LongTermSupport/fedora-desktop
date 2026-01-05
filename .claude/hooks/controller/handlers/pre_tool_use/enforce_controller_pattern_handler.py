"""EnforceControllerPatternHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class EnforceControllerPatternHandler(Handler):
    """Prevent creation of standalone hook files - enforce front controller pattern."""

    # Whitelisted entry point scripts - ALL 10 official Claude Code hook events
    # Official docs: https://code.claude.com/docs/en/hooks.md
    ALLOWED_ENTRY_POINTS = [
        # Currently implemented (4)
        '/.claude/hooks/pre-tool-use',
        '/.claude/hooks/post-tool-use',
        '/.claude/hooks/user-prompt-submit',
        '/.claude/hooks/subagent-stop',
        # Infrastructure ready (6) - implement handlers as needed
        '/.claude/hooks/permission-request',
        '/.claude/hooks/stop',
        '/.claude/hooks/notification',
        '/.claude/hooks/pre-compact',
        '/.claude/hooks/session-start',
        '/.claude/hooks/session-end',
        # Python package file
        '/.claude/hooks/__init__.py',
    ]

    def __init__(self):
        super().__init__(name="enforce-controller-pattern", priority=5)

    def matches(self, hook_input: dict) -> bool:
        """Check if creating standalone hook file (ANY file type)."""
        tool_name = hook_input.get("tool_name")
        if tool_name != "Write":
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Writing to .claude/hooks/ directory?
        if '/.claude/hooks/' not in file_path:
            return False

        # Allow controller/ subdirectory (handlers, tests, etc.)
        if '/controller/' in file_path:
            return False

        # Allow legacy backup files
        if '.bak.' in file_path:
            return False

        # Allow ONLY whitelisted entry point scripts
        if any(file_path.endswith(entry) or file_path == entry for entry in self.ALLOWED_ENTRY_POINTS):
            return False

        # Block EVERYTHING else (ANY extension: .py, .sh, .bash, etc.)
        return True

    def handle(self, hook_input: dict) -> HookResult:
        """Block standalone hook creation."""
        file_path = get_file_path(hook_input)
        file_ext = file_path.split('.')[-1] if '.' in file_path else 'unknown'

        return HookResult(
            decision="deny",
            reason=(
                "🚫 BLOCKED: Standalone hook files are NOT allowed\n\n"
                f"Attempted to create: {file_path}\n"
                f"File type: .{file_ext}\n\n"
                "WHY: ALL hooks must use the front controller pattern.\n"
                "This applies to ALL file types: .py, .sh, .bash, executables, etc.\n\n"
                "✅ CORRECT APPROACH:\n"
                "1. Create a Handler class in controller/handlers/\n"
                "2. Register it in the appropriate dispatcher (pre_tool_use.py, etc.)\n"
                "3. Write tests in controller/tests/\n\n"
                "📖 DOCUMENTATION:\n"
                "  File: .claude/hooks/CLAUDE.md (architecture overview)\n"
                "  Examples: controller/handlers/*.py\n\n"
                "⚠️  ONLY these 10 entry point scripts are allowed (covering ALL official Claude hook events):\n"
                + "\n".join(f"  - {entry}" for entry in self.ALLOWED_ENTRY_POINTS if '__init__' not in entry) + "\n\n"
                "NO other hook files will ever be needed. All hooks use the front controller pattern."
            )
        )


