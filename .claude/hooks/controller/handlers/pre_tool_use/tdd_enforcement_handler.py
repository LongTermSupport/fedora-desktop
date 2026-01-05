"""TddEnforcementHandler - enforce test-first development for handler files."""

import re
import sys
import os
from pathlib import Path

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_file_path, get_file_content


class TddEnforcementHandler(Handler):
    """Enforce TDD by blocking handler file creation without corresponding test file."""

    def __init__(self):
        super().__init__(name="enforce-tdd", priority=15)

    def matches(self, hook_input: dict) -> bool:
        """Check if this is a Write operation to a handler file."""
        # Only match Write tool
        if hook_input.get("tool_name") != "Write":
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Must be a .py file
        if not file_path.endswith('.py'):
            return False

        # Must be in a handlers subdirectory
        if '/handlers/' not in file_path:
            return False

        # Exclude __init__.py files
        if file_path.endswith('__init__.py'):
            return False

        # Must be in one of the handler event directories
        handler_dirs = [
            '/handlers/pre_tool_use/',
            '/handlers/post_tool_use/',
            '/handlers/user_prompt_submit/',
            '/handlers/subagent_stop/',
        ]

        return any(handler_dir in file_path for handler_dir in handler_dirs)

    def handle(self, hook_input: dict) -> HookResult:
        """Check if test file exists, deny if not."""
        handler_path = get_file_path(hook_input)
        test_file_path = self._get_test_file_path(handler_path)

        # Check if test file exists
        if test_file_path.exists():
            return HookResult(decision="allow")

        # Test file doesn't exist - block with helpful message
        handler_filename = Path(handler_path).name
        test_filename = test_file_path.name

        return HookResult(
            decision="deny",
            reason=(
                f"🚫 TDD REQUIRED: Cannot create handler without test file\n\n"
                f"Handler file: {handler_filename}\n"
                f"Missing test: {test_filename}\n\n"
                f"PHILOSOPHY: Test-Driven Development\n"
                f"In TDD, we write the test first, then implement the handler.\n"
                f"This ensures:\n"
                f"  • Clear requirements before coding\n"
                f"  • 100% test coverage from the start\n"
                f"  • Design-focused implementation\n"
                f"  • Prevents untested code in production\n\n"
                f"REQUIRED ACTION:\n"
                f"1. Create the test file first:\n"
                f"   {test_file_path}\n\n"
                f"2. Write comprehensive tests for the handler\n"
                f"   - Test matches() logic with various inputs\n"
                f"   - Test handle() decision and reason\n"
                f"   - Test edge cases and error conditions\n\n"
                f"3. Run tests (they should fail - red)\n\n"
                f"4. THEN create the handler file:\n"
                f"   {handler_path}\n\n"
                f"5. Run tests again (they should pass - green)\n\n"
                f"REFERENCE:\n"
                f"  See existing test files in tests/ for examples\n"
                f"  File: .claude/hooks/controller/GUIDE-TESTING.md\n"
                f"  File: .claude/hooks/CLAUDE.md (TDD mandatory)"
            )
        )

    def _get_test_file_path(self, handler_path: str) -> Path:
        """Get the expected test file path for a handler file."""
        # Extract just the handler filename
        handler_filename = Path(handler_path).name

        # Convert handler filename to test filename
        # e.g., git_handler.py -> test_git_handler.py
        test_filename = f"test_{handler_filename}"

        # Get controller directory by finding 'controller' in path
        path_parts = Path(handler_path).parts
        try:
            controller_idx = path_parts.index('controller')
            # Reconstruct path from parts (properly handles leading /)
            controller_dir = Path(*path_parts[:controller_idx + 1])
        except ValueError:
            # Fallback: assume standard structure
            controller_dir = Path(handler_path).parent.parent.parent

        # Test file should be in tests/ directory
        test_file_path = controller_dir / "tests" / test_filename

        return test_file_path


