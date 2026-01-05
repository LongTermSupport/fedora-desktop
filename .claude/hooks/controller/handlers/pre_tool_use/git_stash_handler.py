"""GitStashHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class GitStashHandler(Handler):
    """Discourage git stash (with escape hatch for necessary cases)."""

    ESCAPE_HATCH = "I HAVE ABSOLUTELY CONFIRMED THAT STASH IS THE ONLY OPTION"

    def __init__(self):
        super().__init__(name="discourage-git-stash", priority=20)

    def matches(self, hook_input: dict) -> bool:
        """Check if this is a git stash creation command."""
        command = get_bash_command(hook_input)
        if not command:
            return False

        # Only match stash creation (not list/show/apply which are safe)
        return bool(re.search(r'git\s+stash\s+(?:push|save)', command, re.IGNORECASE))

    def handle(self, hook_input: dict) -> HookResult:
        """Block unless escape hatch phrase is present."""
        command = get_bash_command(hook_input)

        # Check for escape hatch
        if self.ESCAPE_HATCH in command:
            return HookResult("allow")

        return HookResult(
            decision="deny",
            reason=(
                "BLOCKED: git stash is dangerous\n\n"
                "Reason: Stashes can be lost, forgotten, or accidentally dropped.\n"
                "git stash is especially problematic in worktree-based workflows.\n\n"
                "SAFE alternatives:\n"
                "  - git commit -m 'WIP: description'  (proper version control)\n"
                "  - git checkout -b experiment/name   (new branch for experiments)\n"
                "  - git worktree add ../worktree-name (parallel work)\n"
                "  - git add -p                        (stage specific changes)\n\n"
                f"ESCAPE HATCH (if truly necessary):\n"
                f"  git stash  # {self.ESCAPE_HATCH}"
            )
        )


