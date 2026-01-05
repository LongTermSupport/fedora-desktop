"""DestructiveGitHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class DestructiveGitHandler(Handler):
    """Block destructive git commands that permanently destroy data."""

    def __init__(self):
        super().__init__(name="prevent-destructive-git", priority=10)
        self.destructive_patterns = [
            r'\bgit\s+reset\s+.*--hard\b',
            r'\bgit\s+clean\s+.*-[a-z]*f',
            r'\bgit\s+checkout\s+\.\s*(?:$|;|&&|\|)',
            # SECURITY FIX: Match all variants of checkout with -- and file
            # Matches: git checkout -- file
            # Matches: git checkout HEAD -- file
            # Matches: git checkout main -- file
            # Matches: git checkout @{upstream} -- file
            r'\bgit\s+checkout\s+.*--\s+\S',
            r'\bgit\s+restore\s+.*--worktree\b',
            r'\bgit\s+stash\s+(?:drop|clear)\b',
        ]

    def matches(self, hook_input: dict) -> bool:
        """Check if this is a destructive git command."""
        command = get_bash_command(hook_input)
        if not command or 'git' not in command.lower():
            return False

        # Check for any destructive git pattern
        for pattern in self.destructive_patterns:
            if re.search(pattern, command, re.IGNORECASE):
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block the destructive command with explanation."""
        command = get_bash_command(hook_input)

        # Determine which pattern matched and provide specific reason
        if re.search(r'\bgit\s+reset\s+.*--hard\b', command, re.IGNORECASE):
            reason = "git reset --hard destroys all uncommitted changes permanently"
        elif re.search(r'\bgit\s+clean\s+.*-[a-z]*f', command, re.IGNORECASE):
            reason = "git clean -f permanently deletes untracked files"
        elif re.search(r'\bgit\s+stash\s+drop\b', command, re.IGNORECASE):
            reason = "git stash drop permanently destroys stashed changes"
        elif re.search(r'\bgit\s+stash\s+clear\b', command, re.IGNORECASE):
            reason = "git stash clear permanently destroys all stashed changes"
        elif re.search(r'\bgit\s+checkout\s+.*--\s+\S', command, re.IGNORECASE):
            reason = "git checkout [REF] -- file discards all local changes to that file permanently"
        else:
            reason = "This git command destroys uncommitted changes permanently"

        return HookResult(
            decision="deny",
            reason=(
                f"BLOCKED: Destructive git command detected\n\n"
                f"Reason: {reason}\n\n"
                f"Command: {command}\n\n"
                "This command PERMANENTLY DESTROYS uncommitted changes with NO recovery possible.\n\n"
                "If this operation is truly necessary, you must ask the human user to run it manually.\n\n"
                "SAFE alternatives:\n"
                "  - git stash        (save changes, can recover later)\n"
                "  - git diff         (review changes first)\n"
                "  - git status       (see what would be affected)\n"
                "  - git commit       (save changes permanently first)\n\n"
                "The LLM is NOT ALLOWED to run destructive git commands. Ask the user to do it."
            )
        )
