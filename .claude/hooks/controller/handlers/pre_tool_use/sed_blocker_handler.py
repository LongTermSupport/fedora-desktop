"""SedBlockerHandler - blocks ALL sed command usage."""

import sys
import os
import re

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class SedBlockerHandler(Handler):
    """Block ALL sed command usage - Claude gets sed wrong and causes file destruction.

    Blocks:
    1. Bash tool with sed command (direct execution)
    2. Write tool creating .sh/.bash files containing sed commands

    Allows:
    1. Markdown files (.md) - documentation can mention sed
    2. Git commands - commit messages can mention sed
    3. Read operations - already allowed (doesn't match Write/Bash)

    sed causes large-scale file corruption when:
    - Syntax errors destroy hundreds of files with find -exec
    - In-place editing is irreversible
    - Regular expressions are error-prone
    """

    def __init__(self):
        super().__init__(name="block-sed-command", priority=10)
        # Word boundary pattern: \bsed\b matches "sed" as whole word
        self._sed_pattern = re.compile(r'\bsed\b', re.IGNORECASE)

    def matches(self, hook_input: dict) -> bool:
        """Check if sed appears anywhere in bash commands or shell scripts."""
        tool_name = hook_input.get("tool_name")

        # Case 1: Bash tool - block ANY command containing sed
        if tool_name == "Bash":
            command = get_bash_command(hook_input)
            if command and self._sed_pattern.search(command):
                return True

        # Case 2: Write tool - block shell scripts containing sed, allow markdown
        if tool_name == "Write":
            file_path = get_file_path(hook_input)
            if not file_path:
                return False

            # ALLOW: Markdown files (documentation for humans)
            if file_path.endswith('.md'):
                return False

            # BLOCK: Shell scripts with sed
            if file_path.endswith('.sh') or file_path.endswith('.bash'):
                content = get_file_content(hook_input)
                if content and self._sed_pattern.search(content):
                    return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block the operation with clear explanation."""
        tool_name = hook_input.get("tool_name")

        # Extract the problematic command/content
        if tool_name == "Bash":
            blocked_content = get_bash_command(hook_input)
            context_type = "command"
        else:  # Write tool
            blocked_content = get_file_path(hook_input)
            context_type = "script"

        return HookResult(
            decision="deny",
            reason=(
                f"🚫 BLOCKED: sed command detected\n\n"
                f"sed is FORBIDDEN - causes large-scale file corruption.\n\n"
                f"BLOCKED {context_type}: {blocked_content}\n\n"
                f"WHY BANNED:\n"
                f"  • Claude gets sed syntax wrong regularly\n"
                f"  • Single error destroys hundreds of files\n"
                f"  • In-place editing is irreversible\n\n"
                f"✅ USE PARALLEL HAIKU AGENTS:\n"
                f"  1. List files to update\n"
                f"  2. Dispatch haiku agents (one per file)\n"
                f"  3. Use Edit tool (safe, atomic, git-trackable)\n\n"
                f"EXAMPLE:\n"
                f"  Bad:  find . -name \"*.ts\" -exec sed -i 's/foo/bar/g' {{}} \\;\n"
                f"  Good: Dispatch 10 haiku agents with Edit tool"
            )
        )
